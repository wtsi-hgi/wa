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
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// usageSchemaV1ForTest is the usage_requests schema before error details
// were stored, as existing production DBs hold it.
const usageSchemaV1ForTest = `
CREATE TABLE usage_requests (
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
    latency_us     INTEGER NOT NULL
);
CREATE INDEX usage_requests_ts ON usage_requests (ts_ms);
INSERT INTO usage_requests (ts_ms, category, method, route, status, latency_us)
VALUES (1791633600000, 'cli', 'GET', '/classify/:id', 404, 5000);
`

// usageRowForTest is one usage_requests row as stored.
type usageRowForTest struct {
	TSMS          int64
	Category      string
	Method        string
	Route         string
	Path          string
	ClientID      string
	Username      string
	RemoteAddr    string
	ClientVersion string
	UserAgent     string
	MCPTransport  string
	MCPAgent      string
	MCPTool       string
	Status        int
	LatencyUS     int64
	Query         sql.NullString
	ErrorCode     sql.NullString
	ErrorKind     sql.NullString
	ErrorMessage  sql.NullString
	Lookup        sql.NullString
}

func TestUsageStoreA2(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	convey.Convey("A2.1: Given a new path, when opened, then the file exists with mode 0600 and no rows", t, func() {
		path := filepath.Join(t.TempDir(), "u.sqlite")
		openTestUsageStore(t, path, 30, usageQueueSize, clock)

		info, err := os.Stat(path)
		convey.So(err, convey.ShouldBeNil)
		convey.So(info.Mode().Perm(), convey.ShouldEqual, os.FileMode(0o600))
		convey.So(usageRowsForTest(path), convey.ShouldBeEmpty)
	})

	convey.Convey("A2.2: Given one full event, when Record then Flush run, then one row holds every field", t, func() {
		path := filepath.Join(t.TempDir(), "u.sqlite")
		store := openTestUsageStore(t, path, 30, usageQueueSize, clock)

		at := time.Date(2026, 10, 10, 12, 0, 0, 123_000_000, time.UTC)
		store.Record(fullUsageEventForTest(at))
		convey.So(store.Flush(ctx), convey.ShouldBeNil)

		convey.So(usageRowsForTest(path), convey.ShouldResemble, []usageRowForTest{{
			TSMS:          1791633600123,
			Category:      "mcp",
			Method:        "GET",
			Route:         "/classify/:id",
			Path:          "/nowhere",
			ClientID:      "session-1",
			Username:      "alice",
			RemoteAddr:    "10.0.0.5",
			ClientVersion: "0.4.0",
			UserAgent:     "mlwh-mcp-server/0.4.0",
			MCPTransport:  "stdio",
			MCPAgent:      "claude-code/2.1.0",
			MCPTool:       "mlwh_classify",
			Status:        200,
			LatencyUS:     1500,
		}})
	})

	convey.Convey("A2.3: Given oversized string fields, when written, then each row field is cut to its cap", t, func() {
		path := filepath.Join(t.TempDir(), "u.sqlite")
		store := openTestUsageStore(t, path, 30, usageQueueSize, clock)

		ev := fullUsageEventForTest(clock)
		ev.ClientID = strings.Repeat("c", 200)
		ev.UserAgent = strings.Repeat("u", 600)
		ev.Path = strings.Repeat("p", 300)
		ev.Username = strings.Repeat("n", 200)
		ev.ClientVersion = strings.Repeat("v", 100)
		ev.MCPTransport = strings.Repeat("t", 50)
		ev.MCPAgent = strings.Repeat("a", 300)
		ev.MCPTool = strings.Repeat("o", 200)
		ev.Method = strings.Repeat("M", 1<<20)
		ev.Query = strings.Repeat("q", 600)
		ev.ErrorMessage = strings.Repeat("m", 600)

		store.Record(ev)
		convey.So(store.Flush(ctx), convey.ShouldBeNil)

		rows := usageRowsForTest(path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].ClientID, convey.ShouldEqual, ev.ClientID[:128])
		convey.So(rows[0].UserAgent, convey.ShouldEqual, ev.UserAgent[:512])
		convey.So(rows[0].Path, convey.ShouldEqual, ev.Path[:UsageMaxPathBytes])
		convey.So(rows[0].Username, convey.ShouldEqual, ev.Username[:UsageMaxUsernameBytes])
		convey.So(rows[0].ClientVersion, convey.ShouldEqual, ev.ClientVersion[:UsageMaxClientVersionBytes])
		convey.So(rows[0].MCPTransport, convey.ShouldEqual, ev.MCPTransport[:UsageMaxMCPTransportBytes])
		convey.So(rows[0].MCPAgent, convey.ShouldEqual, ev.MCPAgent[:UsageMaxMCPAgentBytes])
		convey.So(rows[0].MCPTool, convey.ShouldEqual, ev.MCPTool[:UsageMaxMCPToolBytes])
		convey.So(rows[0].Method, convey.ShouldEqual, ev.Method[:32])
		convey.So(rows[0].Query.String, convey.ShouldEqual, ev.Query[:UsageMaxQueryBytes])
		convey.So(rows[0].ErrorMessage.String, convey.ShouldEqual, ev.ErrorMessage[:UsageMaxErrorMessageBytes])
	})

	convey.Convey("A2.4: Given 3 events recorded and the store closed without Flush, when reopened, then 3 rows exist", t, func() {
		path := filepath.Join(t.TempDir(), "u.sqlite")
		store, err := OpenUsageStore(ctx, path, 30)
		convey.So(err, convey.ShouldBeNil)

		for range 3 {
			store.Record(fullUsageEventForTest(time.Now()))
		}

		convey.So(store.Close(), convey.ShouldBeNil)

		reopened, err := OpenUsageStore(ctx, path, 30)
		convey.So(err, convey.ShouldBeNil)
		t.Cleanup(func() { _ = reopened.Close() })

		convey.So(usageRowsForTest(path), convey.ShouldHaveLength, 3)
	})

	convey.Convey("A2.4: Given events still queued behind a busy writer, when the store is closed, then every event is written", t, func() {
		// The writer picks between queued events and the stop signal at
		// random, so repeat to catch a Close that skips the queue.
		const rounds = 8

		rows := 0

		for range rounds {
			path := filepath.Join(t.TempDir(), "u.sqlite")
			store, err := openUsageStore(ctx, path, 30, 4, func() time.Time { return clock })
			convey.So(err, convey.ShouldBeNil)

			conn := lockUsageDBForTest(path)

			store.Record(fullUsageEventForTest(clock))
			time.Sleep(20 * time.Millisecond)

			for range 3 {
				store.Record(fullUsageEventForTest(clock))
			}

			go func() {
				time.Sleep(50 * time.Millisecond)

				_, _ = conn.ExecContext(ctx, "COMMIT")
				_ = conn.Close()
			}()

			convey.So(store.Close(), convey.ShouldBeNil)

			rows += len(usageRowsForTest(path))
		}

		convey.So(rows, convey.ShouldEqual, 4*rounds)
	})

	convey.Convey("A2.5: Given rows at now-31d, now-30d+1s, and now-1h with retention 30", t, func() {
		path := filepath.Join(t.TempDir(), "u.sqlite")
		times := []time.Time{
			clock.Add(-31 * 24 * time.Hour),
			clock.Add(-30*24*time.Hour + time.Second),
			clock.Add(-time.Hour),
		}

		wantKept := []int64{times[1].UnixMilli(), times[2].UnixMilli()}

		convey.Convey("when Prune runs, then it returns 1 and two rows remain", func() {
			store := openTestUsageStore(t, path, 30, usageQueueSize, clock)
			recordUsageAtForTest(store, times)

			n, err := store.Prune(ctx)
			convey.So(err, convey.ShouldBeNil)
			convey.So(n, convey.ShouldEqual, 1)
			convey.So(usageRowTimesForTest(path), convey.ShouldResemble, wantKept)
		})

		convey.Convey("when a row sits exactly at now-30d, then Prune keeps it", func() {
			store := openTestUsageStore(t, path, 30, usageQueueSize, clock)
			boundary := clock.Add(-30 * 24 * time.Hour)
			recordUsageAtForTest(store, []time.Time{boundary})

			n, err := store.Prune(ctx)
			convey.So(err, convey.ShouldBeNil)
			convey.So(n, convey.ShouldEqual, 0)
			convey.So(usageRowTimesForTest(path), convey.ShouldResemble, []int64{boundary.UnixMilli()})
		})

		convey.Convey("when written with retention 365 and reopened with retention 30, then two rows remain", func() {
			store, err := openUsageStore(ctx, path, 365, usageQueueSize, func() time.Time { return clock })
			convey.So(err, convey.ShouldBeNil)
			recordUsageAtForTest(store, times)
			convey.So(store.Close(), convey.ShouldBeNil)
			convey.So(usageRowsForTest(path), convey.ShouldHaveLength, 3)

			reopened := openTestUsageStore(t, path, 30, usageQueueSize, clock)
			convey.So(reopened.RetentionDays(), convey.ShouldEqual, 30)
			convey.So(usageRowTimesForTest(path), convey.ShouldResemble, wantKept)
		})

		convey.Convey("when retention is too large for a time.Duration, then Prune keeps every row", func() {
			for _, days := range []int{1_000_000, math.MaxInt} {
				dayPath := filepath.Join(t.TempDir(), "u.sqlite")
				store := openTestUsageStore(t, dayPath, days, usageQueueSize, clock)
				recordUsageAtForTest(store, times)

				n, err := store.Prune(ctx)
				convey.So(err, convey.ShouldBeNil)
				convey.So(n, convey.ShouldEqual, 0)
				convey.So(usageRowTimesForTest(dayPath), convey.ShouldHaveLength, len(times))
			}
		})
	})

	convey.Convey("A2.6: Given retentionDays 0 or -1, then OpenUsageStore returns a retention error and creates nothing", t, func() {
		for _, days := range []int{0, -1} {
			path := filepath.Join(t.TempDir(), "u.sqlite")

			store, err := OpenUsageStore(ctx, path, days)
			convey.So(store, convey.ShouldBeNil)
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldContainSubstring, "retention")

			_, statErr := os.Stat(path)
			convey.So(os.IsNotExist(statErr), convey.ShouldBeTrue)
		}
	})

	convey.Convey("A2.7: Given queue size 4 and another connection holding an exclusive lock, "+
		"when Record is called 100 times, then it never blocks and drops the overflow", t, func() {
		logs := captureDefaultSlogForTest(t)
		path := filepath.Join(t.TempDir(), "u.sqlite")
		store := openTestUsageStore(t, path, 30, 4, clock)

		conn := lockUsageDBForTest(path)

		defer func() { _ = conn.Close() }()

		start := time.Now()

		for range 100 {
			store.Record(fullUsageEventForTest(clock))
		}

		convey.So(time.Since(start), convey.ShouldBeLessThan, 100*time.Millisecond)
		convey.So(store.Dropped(), convey.ShouldBeGreaterThanOrEqualTo, 50)

		time.Sleep(500 * time.Millisecond)

		_, err := conn.ExecContext(ctx, "COMMIT")
		convey.So(err, convey.ShouldBeNil)

		convey.So(store.Flush(ctx), convey.ShouldBeNil)
		convey.So(int64(len(usageRowsForTest(path)))+store.Dropped(), convey.ShouldEqual, 100)

		dropLogs := logs.recordsWithMessage("mlwh usage event dropped")
		convey.So(dropLogs, convey.ShouldHaveLength, 1)
		convey.So(dropLogs[0].Level, convey.ShouldEqual, slog.LevelWarn)
		convey.So(logs.recordsWithMessage("mlwh usage write failed"), convey.ShouldBeEmpty)
	})

	convey.Convey("A2.8: Given a store whose db is closed under it, when 5 events are recorded and Flush runs, "+
		"then Flush returns nil promptly and a write failure is logged", t, func() {
		logs := captureDefaultSlogForTest(t)
		store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 30, usageQueueSize, clock)
		convey.So(store.db.Close(), convey.ShouldBeNil)

		convey.So(func() {
			for range 5 {
				store.Record(fullUsageEventForTest(clock))
			}
		}, convey.ShouldNotPanic)

		flushCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()

		convey.So(store.Flush(flushCtx), convey.ShouldBeNil)

		failures := logs.recordsWithMessage("mlwh usage write failed")
		convey.So(failures, convey.ShouldNotBeEmpty)
		convey.So(failures[0].Level, convey.ShouldEqual, slog.LevelWarn)
		convey.So(slogAttrsForTest(failures[0])["err"], convey.ShouldNotBeEmpty)
	})

	convey.Convey("A2.9: Given a closed store, then Record does not panic and Close again returns nil", t, func() {
		store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 30, usageQueueSize, clock)
		convey.So(store.Close(), convey.ShouldBeNil)

		convey.So(func() { store.Record(fullUsageEventForTest(clock)) }, convey.ShouldNotPanic)
		convey.So(store.Close(), convey.ShouldBeNil)
	})

	convey.Convey("A2.10: Given a closed store, then Flush returns nil within 1 s and Summary errors", t, func() {
		store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 30, usageQueueSize, clock)
		convey.So(store.Close(), convey.ShouldBeNil)

		start := time.Now()
		convey.So(store.Flush(context.Background()), convey.ShouldBeNil)
		convey.So(time.Since(start), convey.ShouldBeLessThan, time.Second)

		_, err := store.Summary(context.Background(), UsageWindow7d)
		convey.So(err, convey.ShouldNotBeNil)
	})
}

// openTestUsageStore opens a store at path with the given retention, queue
// size, and fixed clock, closing it when the test ends.
func openTestUsageStore(t *testing.T, path string, retentionDays, queueSize int, now time.Time) *UsageStore {
	t.Helper()

	store, err := openUsageStore(context.Background(), path, retentionDays, queueSize,
		func() time.Time { return now })
	convey.So(err, convey.ShouldBeNil)
	t.Cleanup(func() { _ = store.Close() })

	return store
}

func fullUsageEventForTest(at time.Time) UsageEvent {
	return UsageEvent{
		Time:          at,
		Category:      UsageCategoryMCP,
		Method:        "GET",
		Route:         "/classify/:id",
		Path:          "/nowhere",
		ClientID:      "session-1",
		Username:      "alice",
		RemoteAddr:    "10.0.0.5",
		ClientVersion: "0.4.0",
		UserAgent:     "mlwh-mcp-server/0.4.0",
		MCPTransport:  "stdio",
		MCPAgent:      "claude-code/2.1.0",
		MCPTool:       "mlwh_classify",
		Status:        200,
		Latency:       1500 * time.Microsecond,
	}
}

// lockUsageDBForTest opens a second connection to the usage DB at path and
// holds BEGIN EXCLUSIVE on it until the caller commits. Closing the returned
// connection also closes its pool.
func lockUsageDBForTest(path string) *usageLockConnForTest {
	db, err := sql.Open("sqlite", sqliteWritableDSN(path))
	convey.So(err, convey.ShouldBeNil)

	conn, err := db.Conn(context.Background())
	convey.So(err, convey.ShouldBeNil)

	_, err = conn.ExecContext(context.Background(), "BEGIN EXCLUSIVE")
	convey.So(err, convey.ShouldBeNil)

	return &usageLockConnForTest{Conn: conn, db: db}
}

func recordUsageAtForTest(store *UsageStore, times []time.Time) {
	for _, at := range times {
		store.Record(fullUsageEventForTest(at))
	}

	convey.So(store.Flush(context.Background()), convey.ShouldBeNil)
}

func usageRowTimesForTest(path string) []int64 {
	rows := usageRowsForTest(path)
	times := make([]int64, 0, len(rows))

	for _, row := range rows {
		times = append(times, row.TSMS)
	}

	return times
}

// usageRowsForTest reads every usage_requests row from the file at path
// through its own connection, ordered by id.
func usageRowsForTest(path string) []usageRowForTest {
	db, err := sql.Open("sqlite", sqliteWritableDSN(path))
	convey.So(err, convey.ShouldBeNil)

	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(context.Background(), `SELECT ts_ms, category, method, route, path,
    client_id, username, remote_addr, client_version, user_agent, mcp_transport, mcp_agent,
    mcp_tool, status, latency_us, query, error_code, error_kind, error_message, lookup
    FROM usage_requests ORDER BY id`)
	convey.So(err, convey.ShouldBeNil)

	defer func() { _ = rows.Close() }()

	got := []usageRowForTest{}

	for rows.Next() {
		var r usageRowForTest

		convey.So(rows.Scan(&r.TSMS, &r.Category, &r.Method, &r.Route, &r.Path, &r.ClientID,
			&r.Username, &r.RemoteAddr, &r.ClientVersion, &r.UserAgent, &r.MCPTransport,
			&r.MCPAgent, &r.MCPTool, &r.Status, &r.LatencyUS, &r.Query, &r.ErrorCode, &r.ErrorKind,
			&r.ErrorMessage, &r.Lookup), convey.ShouldBeNil)

		got = append(got, r)
	}

	convey.So(rows.Err(), convey.ShouldBeNil)

	return got
}

type usageLockConnForTest struct {
	*sql.Conn
	db *sql.DB
}

func (c *usageLockConnForTest) Close() error {
	_ = c.Conn.Close()

	return c.db.Close()
}

func TestUsageStoreErrorColumnsMigration(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	convey.Convey("Given a DB written before error details existed", t, func() {
		path := filepath.Join(t.TempDir(), "u.sqlite")

		db, err := sql.Open("sqlite", sqliteWritableDSN(path))
		convey.So(err, convey.ShouldBeNil)
		_, err = db.ExecContext(ctx, usageSchemaV1ForTest)
		convey.So(err, convey.ShouldBeNil)
		convey.So(db.Close(), convey.ShouldBeNil)

		convey.Convey("when opened twice and an error event is recorded, then the old row keeps NULL details "+
			"and the new row holds them", func() {
			first, err := openUsageStore(ctx, path, 30, usageQueueSize, func() time.Time { return clock })
			convey.So(err, convey.ShouldBeNil)
			convey.So(first.Close(), convey.ShouldBeNil)

			store := openTestUsageStore(t, path, 30, usageQueueSize, clock)

			ev := fullUsageEventForTest(clock)
			ev.Status, ev.Path, ev.Query = 404, "/classify/X", "a=1"
			ev.ErrorCode, ev.ErrorKind, ev.ErrorMessage = "not_found", "timeout", "no X"
			store.Record(ev)
			convey.So(store.Flush(ctx), convey.ShouldBeNil)

			rows := usageRowsForTest(path)
			convey.So(rows, convey.ShouldHaveLength, 2)
			convey.So(rows[0].Status, convey.ShouldEqual, 404)
			convey.So(rows[0].ErrorCode.Valid, convey.ShouldBeFalse)
			convey.So(rows[0].ErrorMessage.Valid, convey.ShouldBeFalse)
			convey.So(rows[0].Query.Valid, convey.ShouldBeFalse)
			convey.So(rows[1].Query.String, convey.ShouldEqual, "a=1")
			convey.So(rows[1].ErrorCode.String, convey.ShouldEqual, "not_found")
			convey.So(rows[1].ErrorKind.String, convey.ShouldEqual, "timeout")
			convey.So(rows[1].ErrorMessage.String, convey.ShouldEqual, "no X")

			summary, err := store.Summary(ctx, UsageWindowAll)
			convey.So(err, convey.ShouldBeNil)
			convey.So(summary.Errors, convey.ShouldHaveLength, 2)
		})
	})

	convey.Convey("Given old DBs opened by 4 stores at once, then every open succeeds "+
		"and each DB gains the error columns", t, func() {
		const (
			trials  = 10
			openers = 4
		)

		failures := 0

		for range trials {
			path := filepath.Join(t.TempDir(), "u.sqlite")

			db, err := sql.Open("sqlite", sqliteWritableDSN(path))
			convey.So(err, convey.ShouldBeNil)
			_, err = db.ExecContext(ctx, usageSchemaV1ForTest)
			convey.So(err, convey.ShouldBeNil)
			convey.So(db.Close(), convey.ShouldBeNil)

			errs := make(chan error, openers)
			start := make(chan struct{})

			for range openers {
				go func() {
					<-start

					store, openErr := openUsageStore(ctx, path, 30, usageQueueSize,
						func() time.Time { return clock })
					if openErr == nil {
						openErr = store.Close()
					}

					errs <- openErr
				}()
			}

			close(start)

			for range openers {
				if openErr := <-errs; openErr != nil {
					t.Logf("concurrent open: %v", openErr)

					failures++
				}
			}

			db, err = sql.Open("sqlite", sqliteWritableDSN(path))
			convey.So(err, convey.ShouldBeNil)
			columns, err := readSQLiteTableColumns(ctx, db, "usage_requests")
			convey.So(err, convey.ShouldBeNil)
			convey.So(db.Close(), convey.ShouldBeNil)

			for _, column := range usageErrorColumns {
				if _, ok := columns[column]; !ok {
					failures++
				}
			}
		}

		convey.So(failures, convey.ShouldEqual, 0)
	})
}

func TestUsageStoreSQLitePathWithURISpecialCharacters(t *testing.T) {
	clock := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	convey.Convey("Given a new relative usage store path starting file:, when an event is recorded and flushed, "+
		"then the data lands in that exact file readable only by the owner", t, func() {
		dir := t.TempDir()
		t.Chdir(dir)

		store := openTestUsageStore(t, "file:u.db", 30, usageQueueSize, clock)

		store.Record(fullUsageEventForTest(clock))
		convey.So(store.Flush(context.Background()), convey.ShouldBeNil)

		assertStoreFileUsed(dir, "file:u.db")
	})

	for _, name := range []string{"u#1.sqlite", "u?1.sqlite", "u%231.sqlite"} {
		convey.Convey("Given a new usage store path named "+name+", when opened and an event is recorded and flushed, "+
			"then the database and its WAL land at that exact path readable only by the owner", t, func() {
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			store := openTestUsageStore(t, path, 30, usageQueueSize, clock)

			store.Record(fullUsageEventForTest(clock))
			convey.So(store.Flush(context.Background()), convey.ShouldBeNil)

			info, err := os.Stat(path)
			convey.So(err, convey.ShouldBeNil)
			convey.So(info.Size(), convey.ShouldBeGreaterThan, 0)
			convey.So(info.Mode().Perm(), convey.ShouldEqual, os.FileMode(0o600))

			info, err = os.Stat(path + "-wal")
			convey.So(err, convey.ShouldBeNil)
			convey.So(info.Mode().Perm()&0o077, convey.ShouldEqual, 0)

			entries, err := os.ReadDir(dir)
			convey.So(err, convey.ShouldBeNil)

			for _, entry := range entries {
				convey.So(entry.Name(), convey.ShouldBeIn, []string{name, name + "-wal", name + "-shm"})
			}
		})
	}
}
