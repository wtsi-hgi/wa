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
	syncTablePacBioRunWellMetrics,
	syncTableEseqRun,
	syncTableEseqRunLaneMetrics,
	syncTableUseqRunMetrics,
	syncTableSeqOpsTrackingPerSample,
	syncTablePacBioProductMetrics,
	syncTableEseqProductMetrics,
	syncTableUseqProductMetrics,
}

// TableFreshness is the per-table freshness reported by Freshness (and served by
// GET /freshness). HighWater and LastRun are UTC RFC3339 strings, empty when the
// table has never synced; EverSynced is false when no sync_state row exists.
type TableFreshness struct {
	Table      string `json:"table" doc:"mirrored MLWH table name"`
	HighWater  string `json:"high_water" doc:"latest synced last_updated (UTC RFC3339), empty if never synced"`
	LastRun    string `json:"last_run" doc:"timestamp of the last sync run (UTC RFC3339), empty if never synced"`
	EverSynced bool   `json:"ever_synced" doc:"false when no sync_state row exists for the table"`
}

// readTableFreshness reads the sync_state high_water and last_run for one table,
// returning ever_synced=false with empty timestamps when the row is absent. This is
// the sync_state read that also selects last_run, which readSyncStateFromDB omits.
func readTableFreshness(ctx context.Context, db *sql.DB, table string) (TableFreshness, error) {
	var highWaterRaw, lastRunRaw any
	err := db.QueryRowContext(ctx, `SELECT high_water, last_run FROM sync_state WHERE table_name = ?`, table).
		Scan(&highWaterRaw, &lastRunRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TableFreshness{Table: table}, nil
		}

		return TableFreshness{}, fmt.Errorf("%w: query sync state for %s: %w", ErrUpstreamImpaired, table, err)
	}

	highWater, err := formatFreshnessTime(highWaterRaw)
	if err != nil {
		return TableFreshness{}, fmt.Errorf("mlwh: parse sync state high_water for %s: %w", table, err)
	}

	lastRun, err := formatFreshnessTime(lastRunRaw)
	if err != nil {
		return TableFreshness{}, fmt.Errorf("mlwh: parse sync state last_run for %s: %w", table, err)
	}

	return TableFreshness{Table: table, HighWater: highWater, LastRun: lastRun, EverSynced: true}, nil
}

// Freshness reports the freshness of every mirrored sync table, returned by
// GET /freshness.
type Freshness struct {
	Tables []TableFreshness `json:"tables" doc:"freshness per mirrored sync table"`
}

// Freshness reports, for each mirrored sync table in freshnessSyncTables, its
// high_water and last_run (UTC RFC3339) and whether it has ever synced. It reads
// sync_state directly and must succeed even on a never-synced cache (every table
// then reports ever_synced=false with empty timestamps), so the MCP layer can
// degrade gracefully rather than seeing ErrCacheNeverSynced.
func (c *Client) Freshness(ctx context.Context) (Freshness, error) {
	db := c.readCacheDB()
	if db == nil {
		return Freshness{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	tables := make([]TableFreshness, 0, len(freshnessSyncTables))
	for _, table := range freshnessSyncTables {
		freshness, err := readTableFreshness(ctx, db, table)
		if err != nil {
			return Freshness{}, err
		}

		tables = append(tables, freshness)
	}

	return Freshness{Tables: tables}, nil
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
