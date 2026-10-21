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
	"time"
)

// utcRFC3339Layout formats a UTC time as RFC3339 ending in a literal Z (the Z is a
// literal in the layout, so Format on a UTC time yields e.g. 2026-06-01T10:00:00Z).
const utcRFC3339Layout = "2006-01-02T15:04:05Z"

// freshnessSyncTables is the ordered set of mirrored sync tables reported by
// Freshness, matching the tables wa mlwh sync maintains (kept in lockstep with
// supportedSyncTables). The original five come first, then the platform-coverage,
// run-status and tracking mirrors added by A4/A5. HighWater is reported straight
// from each table's stored sync_state high_water, so its semantics follow the
// table's sync mode automatically: the full-refresh tracking table records its
// refresh time, the incremental product-metrics tables record their latest
// last_changed, and the ascending-id and wholesale-replace tables store no
// watermark (zero time), which formatFreshnessTime renders as empty.
var freshnessSyncTables = []string{
	syncTableStudy,
	syncTableSample,
	syncTableIseqFlowcell,
	syncTableIseqProductMetrics,
	syncTableSeqProductIRODSLocations,
	syncTableIseqRunStatus,
	syncTableIseqRunStatusDict,
	syncTableOseqFlowcell,
	syncTableStudyUsers,
	syncTablePacBioRunWellMetrics,
	syncTableEseqRun,
	syncTableEseqRunLaneMetrics,
	syncTableUseqRunMetrics,
	syncTableSeqOpsTrackingPerSample,
	syncTablePacBioProductMetrics,
	syncTableEseqProductMetrics,
	syncTableUseqProductMetrics,
}

// Freshness statuses, per table and overall.
const (
	// FreshnessStatusOK means the table's last sync succeeded within the stale
	// threshold and it has no newer failure.
	FreshnessStatusOK = "ok"
	// FreshnessStatusStale means the table's last successful sync is older than
	// the stale threshold, or it has synced but never been seen to succeed.
	FreshnessStatusStale = "stale"
	// FreshnessStatusFailing means the table's most recent sync attempt failed
	// or was skipped because a dependency failed.
	FreshnessStatusFailing = "failing"
	// FreshnessStatusNeverSynced means the table has no synced data.
	FreshnessStatusNeverSynced = "never_synced"
)

// DefaultStaleAfter is how long after its last successful sync a table is
// reported stale, unless SetStaleAfter configures otherwise. It allows several
// missed runs of the 30-minute production sync loop.
const DefaultStaleAfter = 2 * time.Hour

// freshnessStatusSeverity orders statuses for the overall status, which is the
// most severe table status. A never-synced table has no data at all, so it
// outranks a stale one.
var freshnessStatusSeverity = map[string]int{
	FreshnessStatusOK:          0,
	FreshnessStatusStale:       1,
	FreshnessStatusNeverSynced: 2,
	FreshnessStatusFailing:     3,
}

// TableFreshness is the per-table sync metadata reported by Freshness (and
// served by GET /freshness). HighWater is a sync-mode-specific source-progress
// watermark, while LastRun is the cache-refresh timestamp used to judge
// currency. Present timestamps are UTC RFC3339 strings; EverSynced is false and
// both timestamps are empty when no sync_state row exists.
type TableFreshness struct {
	Table         string   `json:"table" doc:"mirrored MLWH table name"`
	HighWater     string   `json:"high_water" doc:"sync-mode-specific source-progress watermark (UTC RFC3339): latest source-row change for incremental tables, refresh/snapshot time for full-refresh tables, or empty for unsynced tables and sync modes without a meaningful watermark; may remain old when source data is unchanged; do not use as cache refresh currency"`
	LastRun       string   `json:"last_run" doc:"last cache sync/refresh time for this table (UTC RFC3339), empty if never synced; use for per-table cache currency and as-of caveats; advances with every committed batch, so a sync that later failed also moves it"`
	EverSynced    bool     `json:"ever_synced" doc:"false when no sync_state row exists for the table"`
	LastSuccessAt string   `json:"last_success_at" doc:"when this table's sync last completed without error (UTC RFC3339), empty if never seen to complete; staleness is judged from it"`
	LastError     string   `json:"last_error" doc:"error from this table's most recent failed or skipped sync, credentials redacted and capped at 1000 bytes; empty when its latest sync succeeded"`
	LastErrorAt   string   `json:"last_error_at" doc:"when last_error was recorded (UTC RFC3339); empty when last_error is empty"`
	BlockedBy     []string `json:"blocked_by" doc:"failed dependency tables that made the most recent sync skip this table; empty otherwise"`
	Status        string   `json:"status" doc:"ok, stale (last success older than stale_after_seconds, or none seen although data was synced), failing (most recent sync failed or was skipped), or never_synced (no success and no synced data)"`
}

// readTableFreshness reads one table's sync_state high_water and last_run and
// its sync_outcome, returning ever_synced=false with empty timestamps when the
// sync_state row is absent. This is the sync_state read that also selects
// last_run, which readSyncStateFromDB omits.
func readTableFreshness(ctx context.Context, db *sql.DB, table string) (TableFreshness, syncOutcome, error) {
	outcome, err := readSyncOutcome(ctx, db, table)
	if err != nil {
		return TableFreshness{}, syncOutcome{}, err
	}

	freshness := TableFreshness{
		Table:         table,
		LastSuccessAt: formatOptionalFreshnessTime(outcome.LastSuccessAt),
		LastError:     outcome.LastError,
		LastErrorAt:   formatOptionalFreshnessTime(outcome.LastErrorAt),
		BlockedBy:     append([]string{}, outcome.BlockedBy...),
	}

	var highWaterRaw, lastRunRaw any
	err = db.QueryRowContext(ctx, `SELECT high_water, last_run FROM sync_state WHERE table_name = ?`, table).
		Scan(&highWaterRaw, &lastRunRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return freshness, outcome, nil
		}

		return TableFreshness{}, syncOutcome{}, fmt.Errorf("%w: query sync state for %s: %w", ErrUpstreamImpaired, table, err)
	}

	if freshness.HighWater, err = formatFreshnessTime(highWaterRaw); err != nil {
		return TableFreshness{}, syncOutcome{}, fmt.Errorf("mlwh: parse sync state high_water for %s: %w", table, err)
	}

	if freshness.LastRun, err = formatFreshnessTime(lastRunRaw); err != nil {
		return TableFreshness{}, syncOutcome{}, fmt.Errorf("mlwh: parse sync state last_run for %s: %w", table, err)
	}

	freshness.EverSynced = true

	return freshness, outcome, nil
}

// Freshness reports the freshness of every mirrored sync table, returned by
// GET /freshness.
type Freshness struct {
	Status            string           `json:"status" doc:"the most severe table status: failing, then never_synced, then stale, then ok"`
	StaleAfterSeconds int64            `json:"stale_after_seconds" doc:"how long after its last successful sync a table is reported stale, set on the server by WA_MLWH_STALE_AFTER (default 2h)"`
	Tables            []TableFreshness `json:"tables" doc:"sync metadata per mirrored table; use last_run, not high_water, for cache currency, and status for sync health"`
}

// Freshness reports, for each mirrored sync table in freshnessSyncTables, its
// sync-mode-specific source-progress high_water, cache-refresh last_run, last
// sync outcome and status, plus the overall status. It reads sync_state and
// sync_outcome directly and must succeed even on a never-synced cache (every
// table then reports ever_synced=false and never_synced), so the MCP layer can
// degrade gracefully rather than seeing ErrCacheNeverSynced.
func (c *Client) Freshness(ctx context.Context) (Freshness, error) {
	db := c.readCacheDB()
	if db == nil {
		return Freshness{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	now := c.outcomeNow()
	staleAfter := c.freshnessStaleAfter()
	result := Freshness{
		Status:            FreshnessStatusOK,
		StaleAfterSeconds: int64(staleAfter / time.Second),
		Tables:            make([]TableFreshness, 0, len(freshnessSyncTables)),
	}

	for _, table := range freshnessSyncTables {
		freshness, outcome, err := readTableFreshness(ctx, db, table)
		if err != nil {
			return Freshness{}, err
		}

		freshness.Status = freshnessTableStatus(freshness.EverSynced, outcome, now, staleAfter)
		if freshnessStatusSeverity[freshness.Status] > freshnessStatusSeverity[result.Status] {
			result.Status = freshness.Status
		}

		result.Tables = append(result.Tables, freshness)
	}

	return result, nil
}

// freshnessTableStatus applies the status rules in order: a recorded error is
// failing; no success and no synced data is never_synced; synced data with no
// recorded success, or a success older than staleAfter, is stale; otherwise
// ok. A table whose source is empty succeeds without writing sync_state, so a
// recorded success counts even when ever_synced is false.
func freshnessTableStatus(everSynced bool, outcome syncOutcome, now time.Time, staleAfter time.Duration) string {
	switch {
	case outcome.LastError != "":
		return FreshnessStatusFailing
	case outcome.LastSuccessAt.IsZero() && !everSynced:
		return FreshnessStatusNeverSynced
	case outcome.LastSuccessAt.IsZero() || now.Sub(outcome.LastSuccessAt) > staleAfter:
		return FreshnessStatusStale
	default:
		return FreshnessStatusOK
	}
}

func formatOptionalFreshnessTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}

	return value.UTC().Format(utcRFC3339Layout)
}

// formatFreshnessTime parses a stored timestamp and re-renders it as UTC RFC3339
// ending in Z, normalising any stored zone offset to UTC. An absent meaningful
// timestamp renders as the empty string rather than year 0001: a SQL NULL (raw
// is nil) reports empty, which is how a NULL MIN/MAX(created) aggregate (every
// matching iRODS row has an unknown creation time) flows through to an empty
// sequencing_date_range / newest_data_added / delivered_at; likewise a zero time,
// which an interrupted cold load can persist as the formatted zero time
// ("0001-01-01T00:00:00Z") in a sync_state high_water.
func formatFreshnessTime(raw any) (string, error) {
	if raw == nil {
		return "", nil
	}

	parsed, err := parseSyncTimeValue(raw)
	if err != nil {
		return "", err
	}

	if parsed.IsZero() {
		return "", nil
	}

	return parsed.UTC().Format(utcRFC3339Layout), nil
}

// SetStaleAfter sets how long after its last successful sync Freshness reports
// a table stale. A non-positive value restores DefaultStaleAfter.
func (c *Client) SetStaleAfter(staleAfter time.Duration) {
	if c == nil {
		return
	}

	c.staleAfter = staleAfter
}

func (c *Client) freshnessStaleAfter() time.Duration {
	if c.staleAfter > 0 {
		return c.staleAfter
	}

	return DefaultStaleAfter
}
