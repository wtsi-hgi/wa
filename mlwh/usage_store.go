/*******************************************************************************
 * Copyright (c) 2026 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

package mlwh

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const (
	usageQueueSize       = 4096
	usageWriteBatchSize  = 256
	usagePruneInterval   = time.Hour
	usageDropLogInterval = time.Minute
	// usageMaxMethodBytes caps the stored method: unmatched routes accept any
	// client-sent method, and every row passes through insert.
	usageMaxMethodBytes = 32

	usageDroppedLogMessage     = "mlwh usage event dropped"
	usageWriteFailedLogMessage = "mlwh usage write failed"
	usagePruneFailedLogMessage = "mlwh usage prune failed"
)

const usageSchema = `
CREATE TABLE IF NOT EXISTS usage_requests (
    id             INTEGER PRIMARY KEY,
    ts_ms          INTEGER NOT NULL,
    category       TEXT NOT NULL,
    method         TEXT NOT NULL,
    route          TEXT NOT NULL DEFAULT '',
    path           TEXT NOT NULL DEFAULT '',
    client_id      TEXT NOT NULL DEFAULT '',
    username       TEXT NOT NULL DEFAULT '',
    remote_addr    TEXT NOT NULL DEFAULT '',
    client_version TEXT NOT NULL DEFAULT '',
    user_agent     TEXT NOT NULL DEFAULT '',
    mcp_transport  TEXT NOT NULL DEFAULT '',
    mcp_agent      TEXT NOT NULL DEFAULT '',
    mcp_tool       TEXT NOT NULL DEFAULT '',
    status         INTEGER NOT NULL,
    latency_us     INTEGER NOT NULL,
    query          TEXT,
    error_code     TEXT,
    error_kind     TEXT,
    error_message  TEXT
);
CREATE INDEX IF NOT EXISTS usage_requests_ts ON usage_requests (ts_ms);
CREATE INDEX IF NOT EXISTS usage_requests_route_ts
    ON usage_requests (method, route, ts_ms);
CREATE INDEX IF NOT EXISTS usage_requests_route_latency
    ON usage_requests (method, route, latency_us, ts_ms);
`

// usageErrorColumns are the nullable columns added after the first release,
// in schema order. migrateUsageErrorColumns adds any an existing DB lacks; rows
// written before then keep NULL in them.
var usageErrorColumns = []string{"query", "error_code", "error_kind", "error_message"}

// usageErrorIndexSQL creates the partial index the error summary reads. It
// runs after migrateUsageErrorColumns, since an old table lacks error_code.
const usageErrorIndexSQL = `CREATE INDEX IF NOT EXISTS usage_requests_errors
    ON usage_requests (ts_ms, method, route, status, error_code) WHERE status >= 400`

const usageInsertSQL = `INSERT INTO usage_requests (ts_ms, category, method, route, path,
    client_id, username, remote_addr, client_version, user_agent, mcp_transport, mcp_agent,
    mcp_tool, status, latency_us, query, error_code, error_kind, error_message)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

var errUsageRetention = errors.New("mlwh: usage retention must be at least 1 day")

// usageQueueItem is either an event to write or, when flushed is non-nil, a
// Flush marker closed once every earlier item has been handled.
type usageQueueItem struct {
	event   UsageEvent
	flushed chan struct{}
}

// UsageStore persists counted requests in their own SQLite database. Events
// are queued by Record and written by a single background writer, so callers
// never wait on SQLite.
type UsageStore struct {
	db            *sql.DB
	now           func() time.Time
	retentionDays int
	queue         chan usageQueueItem
	stop          chan struct{}
	done          chan struct{}
	closed        atomic.Bool
	dropped       atomic.Int64
	lastDropLog   atomic.Int64
}

// OpenUsageStore opens, creating if needed, the usage DB at path, prunes
// rows older than retentionDays, and starts the writer. retentionDays < 1
// is an error.
func OpenUsageStore(ctx context.Context, path string, retentionDays int) (*UsageStore, error) {
	return openUsageStore(ctx, path, retentionDays, usageQueueSize, time.Now)
}

func openUsageStore(ctx context.Context, path string, retentionDays, queueSize int,
	now func() time.Time,
) (*UsageStore, error) {
	if retentionDays < 1 {
		return nil, fmt.Errorf("%w: got %d", errUsageRetention, retentionDays)
	}

	if err := createStoreFile(path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", sqliteWritablePathDSN(path))
	if err != nil {
		return nil, fmt.Errorf("mlwh: open usage store: %w", err)
	}

	s := &UsageStore{
		db:            db,
		now:           now,
		retentionDays: retentionDays,
		queue:         make(chan usageQueueItem, queueSize),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}

	if err = createUsageSchema(ctx, db); err != nil {
		_ = db.Close()

		return nil, err
	}

	if _, err = s.Prune(ctx); err != nil {
		_ = db.Close()

		return nil, err
	}

	go s.run()

	return s, nil
}

// Record queues ev without blocking; it drops ev when the queue is full.
// Calling Record after Close is a no-op.
func (s *UsageStore) Record(ev UsageEvent) {
	if s.closed.Load() {
		return
	}

	select {
	case s.queue <- usageQueueItem{event: ev}:
	default:
		s.dropped.Add(1)
		s.logDropped()
	}
}

// logDropped warns about dropped events at most once per usageDropLogInterval.
func (s *UsageStore) logDropped() {
	now := time.Now().UnixNano()
	last := s.lastDropLog.Load()

	if last != 0 && now-last < int64(usageDropLogInterval) {
		return
	}

	if !s.lastDropLog.CompareAndSwap(last, now) {
		return
	}

	slog.Default().Warn(usageDroppedLogMessage, "dropped", s.dropped.Load())
}

// Flush blocks until every event queued before the call is written or
// dropped by a write failure, or ctx ends. After Close it returns nil at
// once.
func (s *UsageStore) Flush(ctx context.Context) error {
	if s.closed.Load() {
		return nil
	}

	flushed := make(chan struct{})

	select {
	case s.queue <- usageQueueItem{flushed: flushed}:
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case <-flushed:
		return nil
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Prune deletes rows with ts_ms older than now - retentionDays*24h and
// returns the count deleted.
func (s *UsageStore) Prune(ctx context.Context) (int64, error) {
	cutoff := usageCutoffMS(s.now().UnixMilli(), s.retentionDays)

	result, err := s.db.ExecContext(ctx, "DELETE FROM usage_requests WHERE ts_ms < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("mlwh: prune usage: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mlwh: prune usage: %w", err)
	}

	return n, nil
}

// usageCutoffMS returns nowMS - retentionDays*24h in milliseconds, saturating
// at math.MinInt64 (nothing pruned) when the retention reaches further back
// than an int64 millisecond timestamp can.
func usageCutoffMS(nowMS int64, retentionDays int) int64 {
	days := int64(retentionDays)
	if days > math.MaxInt64/usageMSPerDay {
		return math.MinInt64
	}

	span := days * usageMSPerDay
	if nowMS < math.MinInt64+span {
		return math.MinInt64
	}

	return nowMS - span
}

// Dropped returns events dropped on a full queue since open.
func (s *UsageStore) Dropped() int64 {
	return s.dropped.Load()
}

// RetentionDays returns the configured retention.
func (s *UsageStore) RetentionDays() int {
	return s.retentionDays
}

// Close flushes queued events, stops the writer, and closes the DB.
func (s *UsageStore) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	close(s.stop)
	<-s.done

	return s.db.Close()
}

// run is the single writer: it writes queued events in batches, prunes every
// usagePruneInterval, and on stop writes whatever is still queued.
func (s *UsageStore) run() {
	defer close(s.done)

	ticker := time.NewTicker(usagePruneInterval)
	defer ticker.Stop()

	for {
		select {
		case item := <-s.queue:
			s.writeBatch(item)
		case <-ticker.C:
			if _, err := s.Prune(context.Background()); err != nil {
				slog.Default().Warn(usagePruneFailedLogMessage, "err", err)
			}
		case <-s.stop:
			s.drain()

			return
		}
	}
}

// drain writes every item already queued.
func (s *UsageStore) drain() {
	for {
		select {
		case item := <-s.queue:
			s.writeBatch(item)
		default:
			return
		}
	}
}

// writeBatch writes first and any further queued events, up to
// usageWriteBatchSize events in total, in one transaction, then releases any
// Flush markers taken from the queue with them. A failed write is logged and
// the batch dropped.
func (s *UsageStore) writeBatch(first usageQueueItem) {
	events := make([]UsageEvent, 0, usageWriteBatchSize)

	var flushes []chan struct{}

	item := first

collect:
	for {
		if item.flushed != nil {
			flushes = append(flushes, item.flushed)
		} else {
			events = append(events, item.event)
		}

		if len(events) == usageWriteBatchSize {
			break
		}

		select {
		case item = <-s.queue:
		default:
			break collect
		}
	}

	if err := s.insert(events); err != nil {
		slog.Default().Warn(usageWriteFailedLogMessage, "err", err, "events", len(events))
	}

	for _, flushed := range flushes {
		close(flushed)
	}
}

func (s *UsageStore) insert(events []UsageEvent) error {
	if len(events) == 0 {
		return nil
	}

	ctx := context.Background()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mlwh: begin usage write: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, usageInsertSQL)
	if err != nil {
		return fmt.Errorf("mlwh: prepare usage insert: %w", err)
	}

	defer func() { _ = stmt.Close() }()

	for _, ev := range events {
		if _, err = stmt.ExecContext(ctx, usageInsertArgs(ev)...); err != nil {
			return fmt.Errorf("mlwh: insert usage: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("mlwh: commit usage write: %w", err)
	}

	return nil
}

// usageInsertArgs returns the usageInsertSQL arguments for ev, with each
// caller-supplied string cut to its cap.
func usageInsertArgs(ev UsageEvent) []any {
	return []any{
		ev.Time.UnixMilli(),
		string(ev.Category),
		truncateUTF8(ev.Method, usageMaxMethodBytes),
		ev.Route,
		truncateUTF8(ev.Path, UsageMaxPathBytes),
		truncateUTF8(ev.ClientID, UsageMaxClientIDBytes),
		truncateUTF8(ev.Username, UsageMaxUsernameBytes),
		ev.RemoteAddr,
		truncateUTF8(ev.ClientVersion, UsageMaxClientVersionBytes),
		truncateUTF8(ev.UserAgent, UsageMaxUserAgentBytes),
		truncateUTF8(ev.MCPTransport, UsageMaxMCPTransportBytes),
		truncateUTF8(ev.MCPAgent, UsageMaxMCPAgentBytes),
		truncateUTF8(ev.MCPTool, UsageMaxMCPToolBytes),
		ev.Status,
		ev.Latency.Microseconds(),
		usageNullString(truncateUTF8(ev.Query, UsageMaxQueryBytes)),
		usageNullString(ev.ErrorCode),
		usageNullString(ev.ErrorKind),
		usageNullString(truncateUTF8(ev.ErrorMessage, UsageMaxErrorMessageBytes)),
	}
}

// migrateUsageErrorColumns adds any usageErrorColumns the table lacks. Two
// processes opening one old DB would otherwise both see a column missing and
// both ALTER, so the check and the ALTERs share one BEGIN IMMEDIATE
// transaction on a single connection: a later opener waits on busy_timeout,
// then finds the columns present.
func migrateUsageErrorColumns(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("mlwh: usage migration connection: %w", err)
	}

	defer func() { _ = conn.Close() }()

	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("mlwh: begin usage migration: %w", err)
	}

	if err = addMissingUsageErrorColumns(ctx, conn); err != nil {
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")

		return err
	}

	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")

		return fmt.Errorf("mlwh: commit usage migration: %w", err)
	}

	return nil
}

func addMissingUsageErrorColumns(ctx context.Context, conn *sql.Conn) error {
	columns, err := readSQLiteTableColumns(ctx, conn, "usage_requests")
	if err != nil {
		return err
	}

	for _, column := range usageErrorColumns {
		if _, ok := columns[column]; ok {
			continue
		}

		if _, err = conn.ExecContext(ctx, "ALTER TABLE usage_requests ADD COLUMN "+column+" TEXT"); err != nil {
			return fmt.Errorf("mlwh: add usage column %s: %w", column, err)
		}
	}

	return nil
}

// createUsageSchema creates the table and indexes, adding the error columns
// to a table created before they existed.
func createUsageSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, usageSchema); err != nil {
		return fmt.Errorf("mlwh: create usage schema: %w", err)
	}

	if err := migrateUsageErrorColumns(ctx, db); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, usageErrorIndexSQL); err != nil {
		return fmt.Errorf("mlwh: create usage error index: %w", err)
	}

	return nil
}

// usageNullString returns NULL for "", so rows without error details read
// the same as rows written before those columns existed.
func usageNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
