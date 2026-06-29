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
	"fmt"
	"strconv"
	"time"
)

// This file gives each new platform-coverage / run-status / tracking mirror
// table (added in A4) its sync strategy (A5), reusing the established sync
// infrastructure:
//
//   - Per-platform *_product_metrics tables sync incrementally on the source
//     last_changed watermark, exactly like iseq_product_metrics, with
//     NULL-preserving QC.
//   - iseq_run_status syncs in ascending-id mode on its id_run_status primary
//     key (no last_changed), like the seq_product_irods_locations cold path;
//     high_water stays empty for ascending-id tables.
//   - The small lookup / identity / per-run status tables that lack a usable
//     incremental key (iseq_run_status_dict, oseq_flowcell, pac_bio_run_well_metrics,
//     eseq_run, eseq_run_lane_metrics, useq_run_metrics) are mirrored wholesale:
//     each run replaces the whole small table inside one transaction.
//   - seq_ops_tracking_per_sample mutates in place with no last_changed and is
//     mirrored by a full-table refresh that builds the new snapshot and swaps it
//     in atomically inside one transaction; its high_water is the refresh time
//     and last_run the sync time.

// ---------------------------------------------------------------------------
// Per-platform product-metrics incremental syncs (last_changed precedent).
// ---------------------------------------------------------------------------

// productMetricsMirrorSpec describes one per-platform product-metrics mirror so
// the shared incremental sync can read, scan and upsert it without per-table
// duplication. The source query recovers id_sample_tmp/id_study_lims through the
// platform's linkage table and projects the QC columns plus last_changed.
type productMetricsMirrorSpec struct {
	syncTable     string
	mirrorTable   string
	keyColumn     string
	mirrorColumns []string
	sourceQuery   func(state syncStateRecord) (string, []any)
	qcColumns     int
	// hasIDRun reports whether the mirror carries an id_run column (the NPG run
	// id), populated from the source product-metrics row. Elembio and Ultimagen
	// product-metrics carry id_run (it is their within-sequencing run-status join
	// key, eseq_product_metrics.id_run -> eseq_run_lane_metrics.id_run and
	// useq_product_metrics.id_run -> useq_run_metrics.id_run); PacBio does not (it
	// joins its run-well metrics via id_pac_bio_rw_metrics_tmp instead), so its
	// spec leaves this false.
	hasIDRun bool
}

var pacBioProductMetricsMirrorColumns = []string{
	"id_pac_bio_product",
	"id_pac_bio_rw_metrics_tmp",
	"id_sample_tmp",
	"id_study_lims",
	"qc",
	"last_updated",
}

var eseqProductMetricsMirrorColumns = []string{
	"id_eseq_product",
	"id_eseq_flowcell_tmp",
	"id_run",
	"id_sample_tmp",
	"id_study_lims",
	"qc",
	"qc_seq",
	"qc_lib",
	"last_updated",
}

var useqProductMetricsMirrorColumns = []string{
	"id_useq_product",
	"id_useq_wafer_tmp",
	"id_run",
	"id_sample_tmp",
	"id_study_lims",
	"qc",
	"qc_seq",
	"qc_lib",
	"last_updated",
}

// allProductMetricsMirrorSpecs returns every per-platform product-metrics spec so
// the source-schema validator can cover each one's source SELECT generically.
func allProductMetricsMirrorSpecs() []productMetricsMirrorSpec {
	return []productMetricsMirrorSpec{
		pacBioProductMetricsSpec(),
		eseqProductMetricsSpec(),
		useqProductMetricsSpec(),
	}
}

func pacBioProductMetricsSpec() productMetricsMirrorSpec {
	return productMetricsMirrorSpec{
		syncTable:     syncTablePacBioProductMetrics,
		mirrorTable:   "pac_bio_product_metrics_mirror",
		keyColumn:     "id_pac_bio_product",
		mirrorColumns: pacBioProductMetricsMirrorColumns,
		qcColumns:     1,
		sourceQuery: func(state syncStateRecord) (string, []any) {
			return `SELECT pbm.id_pac_bio_product, pbm.id_pac_bio_rw_metrics_tmp, pbr.id_sample_tmp, study.id_study_lims, pbm.qc, pbm.last_changed FROM pac_bio_product_metrics pbm INNER JOIN pac_bio_run pbr ON pbr.id_pac_bio_tmp = pbm.id_pac_bio_tmp INNER JOIN study ON study.id_study_tmp = pbr.id_study_tmp AND study.id_lims = 'SQSCP' WHERE pbm.last_changed >= ? ORDER BY pbm.last_changed, pbm.id_pac_bio_pr_metrics_tmp`,
				[]any{formatSyncTime(state.HighWater)}
		},
	}
}

func eseqProductMetricsSpec() productMetricsMirrorSpec {
	return productMetricsMirrorSpec{
		syncTable:     syncTableEseqProductMetrics,
		mirrorTable:   "eseq_product_metrics_mirror",
		keyColumn:     "id_eseq_product",
		mirrorColumns: eseqProductMetricsMirrorColumns,
		qcColumns:     3,
		hasIDRun:      true,
		sourceQuery: func(state syncStateRecord) (string, []any) {
			return `SELECT epm.id_eseq_product, epm.id_eseq_flowcell_tmp, epm.id_run, efc.id_sample_tmp, study.id_study_lims, epm.qc, epm.qc_seq, epm.qc_lib, epm.last_changed FROM eseq_product_metrics epm INNER JOIN eseq_flowcell efc ON efc.id_eseq_flowcell_tmp = epm.id_eseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = efc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE epm.last_changed >= ? ORDER BY epm.last_changed, epm.id_eseq_pr_metrics_tmp`,
				[]any{formatSyncTime(state.HighWater)}
		},
	}
}

func useqProductMetricsSpec() productMetricsMirrorSpec {
	return productMetricsMirrorSpec{
		syncTable:     syncTableUseqProductMetrics,
		mirrorTable:   "useq_product_metrics_mirror",
		keyColumn:     "id_useq_product",
		mirrorColumns: useqProductMetricsMirrorColumns,
		qcColumns:     3,
		hasIDRun:      true,
		sourceQuery: func(state syncStateRecord) (string, []any) {
			return `SELECT upm.id_useq_product, upm.id_useq_wafer_tmp, upm.id_run, uw.id_sample_tmp, study.id_study_lims, upm.qc, upm.qc_seq, upm.qc_lib, upm.last_changed FROM useq_product_metrics upm INNER JOIN useq_wafer uw ON uw.id_useq_wafer_tmp = upm.id_useq_wafer_tmp INNER JOIN study ON study.id_study_tmp = uw.id_study_tmp AND study.id_lims = 'SQSCP' WHERE upm.last_changed >= ? ORDER BY upm.last_changed, upm.id_useq_pr_metrics_tmp`,
				[]any{formatSyncTime(state.HighWater)}
		},
	}
}

// productMetricsMirrorSyncRow is one per-platform product-metrics row. QC fields
// are NULL-preserving (sql.NullInt64, never coerced to 0) so a downstream read
// maps NULL to "pending", distinct from a 0 "fail" (the A3 precedent).
type productMetricsMirrorSyncRow struct {
	ProductID      string
	LinkID         int64
	IDRun          int64
	IDSampleTmp    int64
	IDStudyLims    string
	QC             sql.NullInt64
	QCSeq          sql.NullInt64
	QCLib          sql.NullInt64
	LastUpdated    time.Time
	hasSecondaryQC bool
	hasIDRun       bool
}

func syncPacBioProductMetricsTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	return syncProductMetricsMirrorTable(ctx, cache, source, state, pacBioProductMetricsSpec())
}

func syncEseqProductMetricsTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	return syncProductMetricsMirrorTable(ctx, cache, source, state, eseqProductMetricsSpec())
}

func syncUseqProductMetricsTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	return syncProductMetricsMirrorTable(ctx, cache, source, state, useqProductMetricsSpec())
}

// syncProductMetricsMirrorTable incrementally syncs one per-platform
// product-metrics mirror, following the iseq_product_metrics last_changed
// precedent: it reads source rows whose last_changed is at or after the stored
// high_water, upserts them by primary key, and advances high_water to the latest
// last_changed seen. QC stays NULL-preserving.
func syncProductMetricsMirrorTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord, spec productMetricsMirrorSpec) (SyncReport, bool, error) {
	query, args := spec.sourceQuery(state)

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query %s sync source: %w", spec.syncTable, err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: spec.syncTable, HighWater: state.HighWater}
	sawRows := false
	batch := make([]productMetricsMirrorSyncRow, 0, syncBatchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		result, applyErr := writeProductMetricsMirrorBatch(ctx, cache, spec, batch, report.HighWater)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanProductMetricsMirrorSyncRow(rows, spec)
		if scanErr != nil {
			return report, false, scanErr
		}

		sawRows = true
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}

		batch = append(batch, row)
		if len(batch) == syncBatchSize {
			if err = flushBatch(); err != nil {
				return report, false, err
			}
		}
	}

	if err = rows.Err(); err != nil {
		return report, false, fmt.Errorf("mlwh: read %s sync source: %w", spec.syncTable, err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeSyncState(ctx, cache, spec.syncTable, report.HighWater); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func scanProductMetricsMirrorSyncRow(rows *sql.Rows, spec productMetricsMirrorSpec) (productMetricsMirrorSyncRow, error) {
	var row productMetricsMirrorSyncRow
	row.hasSecondaryQC = spec.qcColumns == 3
	row.hasIDRun = spec.hasIDRun
	var lastChanged any
	targets := []any{&row.ProductID, &row.LinkID}
	if row.hasIDRun {
		targets = append(targets, &row.IDRun)
	}
	targets = append(targets, &row.IDSampleTmp, &row.IDStudyLims, &row.QC)
	if row.hasSecondaryQC {
		targets = append(targets, &row.QCSeq, &row.QCLib)
	}
	targets = append(targets, &lastChanged)

	if err := rows.Scan(targets...); err != nil {
		return productMetricsMirrorSyncRow{}, fmt.Errorf("mlwh: scan %s sync row: %w", spec.syncTable, err)
	}

	parsed, err := parseSyncTimeValue(lastChanged)
	if err != nil {
		return productMetricsMirrorSyncRow{}, fmt.Errorf("mlwh: parse %s last_changed: %w", spec.syncTable, err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func productMetricsMirrorRowArgs(row productMetricsMirrorSyncRow) []any {
	args := []any{row.ProductID, row.LinkID}
	if row.hasIDRun {
		args = append(args, row.IDRun)
	}
	args = append(args, row.IDSampleTmp, row.IDStudyLims, row.QC)
	if row.hasSecondaryQC {
		args = append(args, row.QCSeq, row.QCLib)
	}
	args = append(args, formatSyncTime(row.LastUpdated))

	return args
}

func productMetricsMirrorBatchKeys(rows []productMetricsMirrorSyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.ProductID})
	}

	return keys
}

func validateProductMetricsMirrorBatch(syncTable string, rows []productMetricsMirrorSyncRow) error {
	for _, row := range rows {
		if row.IDStudyLims == "" {
			return fmt.Errorf("mlwh: %s_mirror row %q violates constraint: id_study_lims must not be empty", syncTable, row.ProductID)
		}
	}

	return nil
}

func writeProductMetricsMirrorBatch(ctx context.Context, cache Cache, spec productMetricsMirrorSpec, rows []productMetricsMirrorSyncRow, highWater time.Time) (syncBatchResult, error) {
	deduped := dedupeProductMetricsMirrorBatch(rows)
	if err := validateProductMetricsMirrorBatch(spec.syncTable, deduped); err != nil {
		return syncBatchResult{}, err
	}

	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing, err := countExistingKeys(ctx, tx, spec.mirrorTable, []string{spec.keyColumn}, productMetricsMirrorBatchKeys(deduped))
		if err != nil {
			return err
		}

		if err = upsertProductMetricsMirrorBatch(ctx, tx, cache.Dialect(), spec, deduped); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing

		return writeSyncStateTx(ctx, tx, cache.Dialect(), spec.syncTable, highWater, nil, false)
	})

	return result, err
}

func upsertProductMetricsMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, spec productMetricsMirrorSpec, rows []productMetricsMirrorSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(spec.mirrorColumns)), func(chunk []productMetricsMirrorSyncRow) error {
		stmt := buildBulkUpsertStatement(dialect, spec.mirrorTable, spec.mirrorColumns, []string{spec.keyColumn}, len(chunk))
		args := make([]any, 0, len(chunk)*len(spec.mirrorColumns))
		for _, row := range chunk {
			args = append(args, productMetricsMirrorRowArgs(row)...)
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("mlwh: upsert %s batch: %w", spec.mirrorTable, err)
		}

		return nil
	})
}

func dedupeProductMetricsMirrorBatch(rows []productMetricsMirrorSyncRow) []productMetricsMirrorSyncRow {
	indices := make(map[string]int, len(rows))
	deduped := make([]productMetricsMirrorSyncRow, 0, len(rows))
	for _, row := range rows {
		index, ok := indices[row.ProductID]
		if ok {
			deduped[index] = row

			continue
		}

		indices[row.ProductID] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

// ---------------------------------------------------------------------------
// iseq_run_status ascending-id sync (id_run_status PK, no last_changed).
// ---------------------------------------------------------------------------

var iseqRunStatusMirrorColumns = []string{
	"id_run_status",
	"id_run",
	"date",
	"id_run_status_dict",
	"iscurrent",
}

type iseqRunStatusSyncRow struct {
	IDRunStatus     int64
	IDRun           int64
	Date            time.Time
	IDRunStatusDict int64
	IsCurrent       int
}

// syncIseqRunStatusTable syncs iseq_run_status in ascending-id mode on its
// id_run_status primary key (cf. the seq_product_irods_locations ascending cold
// path). It pages forward by id_run_status, so rows are always read in ascending
// id order; there is no last_changed, so high_water stays empty (zero) and
// freshness for this table is reported via last_run only.
func syncIseqRunStatusTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	resumeID, err := iseqRunStatusResumeID(state)
	if err != nil {
		return SyncReport{}, false, err
	}

	report := SyncReport{Table: syncTableIseqRunStatus}
	sawRows := false
	lastID := resumeID

	for {
		batch, maxID, batchErr := readIseqRunStatusPage(ctx, source, lastID)
		if batchErr != nil {
			return report, false, batchErr
		}
		if len(batch) == 0 {
			break
		}

		sawRows = true
		result, writeErr := writeIseqRunStatusBatch(ctx, cache, batch, maxID)
		if writeErr != nil {
			return report, false, writeErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		lastID = maxID
	}

	if sawRows || state.Exists {
		// high_water stays empty for the ascending-id table; only last_run advances.
		if err = finalizeSyncState(ctx, cache, syncTableIseqRunStatus, time.Time{}); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func iseqRunStatusResumeID(state syncStateRecord) (int64, error) {
	if state.ResumeCursor == nil {
		return syncColdInitialAscendingID, nil
	}

	id, ok, err := parseAscendingIDResumeCursor(*state.ResumeCursor, iseqRunStatusIDResumeMode)
	if err != nil {
		return 0, err
	}
	if ok {
		return id, nil
	}

	return syncColdInitialAscendingID, nil
}

// iseqRunStatusPageQuery is the ascending-id page SELECT the iseq_run_status sync
// issues against the source. It is shared with the source-schema validator so the
// validated query is exactly the one sync runs.
func iseqRunStatusPageQuery() string {
	return `SELECT id_run_status, id_run, date, id_run_status_dict, iscurrent FROM iseq_run_status WHERE id_run_status > ? ORDER BY id_run_status LIMIT ` + strconv.Itoa(syncColdBatchSize)
}

func readIseqRunStatusPage(ctx context.Context, source Querier, lastID int64) ([]iseqRunStatusSyncRow, int64, error) {
	query := iseqRunStatusPageQuery()

	rows, err := source.QueryContext(ctx, query, lastID)
	if err != nil {
		return nil, 0, fmt.Errorf("mlwh: query iseq_run_status sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	batch := make([]iseqRunStatusSyncRow, 0, syncColdBatchSize)
	maxID := lastID
	for rows.Next() {
		row, scanErr := scanIseqRunStatusSyncRow(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}

		batch = append(batch, row)
		if row.IDRunStatus > maxID {
			maxID = row.IDRunStatus
		}
	}
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("mlwh: read iseq_run_status sync source: %w", err)
	}

	return batch, maxID, nil
}

func scanIseqRunStatusSyncRow(rows *sql.Rows) (iseqRunStatusSyncRow, error) {
	var row iseqRunStatusSyncRow
	var date any
	var iscurrent sql.NullInt64
	if err := rows.Scan(&row.IDRunStatus, &row.IDRun, &date, &row.IDRunStatusDict, &iscurrent); err != nil {
		return iseqRunStatusSyncRow{}, fmt.Errorf("mlwh: scan iseq_run_status sync row: %w", err)
	}

	parsed, err := parseSyncTimeValue(date)
	if err != nil {
		return iseqRunStatusSyncRow{}, fmt.Errorf("mlwh: parse iseq_run_status date: %w", err)
	}
	row.Date = parsed
	row.IsCurrent = nullIntValue(iscurrent)

	return row, nil
}

func iseqRunStatusMirrorRowArgs(row iseqRunStatusSyncRow) []any {
	return []any{row.IDRunStatus, row.IDRun, formatSyncTime(row.Date), row.IDRunStatusDict, row.IsCurrent}
}

func writeIseqRunStatusBatch(ctx context.Context, cache Cache, rows []iseqRunStatusSyncRow, maxID int64) (syncBatchResult, error) {
	var result syncBatchResult
	resumeCursor := encodeAscendingIDResumeCursor(iseqRunStatusIDResumeMode, maxID)
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		keys := make([][]any, 0, len(rows))
		for _, row := range rows {
			keys = append(keys, []any{row.IDRunStatus})
		}

		existing, err := countExistingKeys(ctx, tx, "iseq_run_status_mirror", []string{"id_run_status"}, keys)
		if err != nil {
			return err
		}

		if err = upsertIseqRunStatusBatch(ctx, tx, cache.Dialect(), rows); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(rows) - existing

		// high_water stays empty (zero) for the ascending-id table; the resume
		// cursor records the id paged to so far.
		return writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableIseqRunStatus, time.Time{}, &resumeCursor, false)
	})

	return result, err
}

func upsertIseqRunStatusBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []iseqRunStatusSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(iseqRunStatusMirrorColumns)), func(chunk []iseqRunStatusSyncRow) error {
		stmt := buildBulkUpsertStatement(dialect, "iseq_run_status_mirror", iseqRunStatusMirrorColumns, []string{"id_run_status"}, len(chunk))
		args := make([]any, 0, len(chunk)*len(iseqRunStatusMirrorColumns))
		for _, row := range chunk {
			args = append(args, iseqRunStatusMirrorRowArgs(row)...)
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("mlwh: upsert iseq_run_status_mirror batch: %w", err)
		}

		return nil
	})
}

// ---------------------------------------------------------------------------
// Wholesale full-replace syncs for small lookup / identity / per-run tables
// that lack a usable incremental key.
// ---------------------------------------------------------------------------

// wholesaleMirrorSpec describes a table mirrored wholesale: the whole mirror is
// cleared and rebuilt from the source snapshot inside one transaction each run.
type wholesaleMirrorSpec struct {
	syncTable     string
	mirrorTable   string
	mirrorColumns []string
	sourceQuery   string
	scan          func(*sql.Rows) ([]any, error)
}

// wholesaleMirrorTables lists every table mirrored wholesale, so the source-schema
// validator covers each one's source SELECT generically.
func wholesaleMirrorTables() []string {
	return []string{
		syncTableIseqRunStatusDict,
		syncTableOseqFlowcell,
		syncTableStudyUsers,
		syncTablePacBioRunWellMetrics,
		syncTableEseqRun,
		syncTableEseqRunLaneMetrics,
		syncTableUseqRunMetrics,
	}
}

func wholesaleMirrorSpecFor(table string) wholesaleMirrorSpec {
	switch table {
	case syncTableIseqRunStatusDict:
		return iseqRunStatusDictWholesaleSpec()
	case syncTableOseqFlowcell:
		return oseqFlowcellWholesaleSpec()
	case syncTableStudyUsers:
		return studyUsersWholesaleSpec()
	case syncTablePacBioRunWellMetrics:
		return pacBioRunWellMetricsWholesaleSpec()
	case syncTableEseqRun:
		return eseqRunWholesaleSpec()
	case syncTableEseqRunLaneMetrics:
		return eseqRunLaneMetricsWholesaleSpec()
	case syncTableUseqRunMetrics:
		return useqRunMetricsWholesaleSpec()
	default:
		return wholesaleMirrorSpec{}
	}
}

func iseqRunStatusDictWholesaleSpec() wholesaleMirrorSpec {
	return wholesaleMirrorSpec{
		syncTable:     syncTableIseqRunStatusDict,
		mirrorTable:   "iseq_run_status_dict_mirror",
		mirrorColumns: []string{"id_run_status_dict", "description", "temporal_index"},
		sourceQuery:   `SELECT id_run_status_dict, description, temporal_index FROM iseq_run_status_dict ORDER BY id_run_status_dict`,
		scan: func(rows *sql.Rows) ([]any, error) {
			var idRunStatusDict int64
			var description string
			var temporalIndex sql.NullInt64
			if err := rows.Scan(&idRunStatusDict, &description, &temporalIndex); err != nil {
				return nil, fmt.Errorf("mlwh: scan iseq_run_status_dict sync row: %w", err)
			}

			return []any{idRunStatusDict, description, temporalIndex}, nil
		},
	}
}

func oseqFlowcellWholesaleSpec() wholesaleMirrorSpec {
	return wholesaleMirrorSpec{
		syncTable:     syncTableOseqFlowcell,
		mirrorTable:   "oseq_flowcell_mirror",
		mirrorColumns: []string{"id_oseq_flowcell_tmp", "id_sample_tmp", "id_study_lims"},
		sourceQuery:   `SELECT ofc.id_oseq_flowcell_tmp, ofc.id_sample_tmp, study.id_study_lims FROM oseq_flowcell ofc INNER JOIN study ON study.id_study_tmp = ofc.id_study_tmp AND study.id_lims = 'SQSCP' ORDER BY ofc.id_oseq_flowcell_tmp`,
		scan: func(rows *sql.Rows) ([]any, error) {
			var idOseqFlowcellTmp, idSampleTmp int64
			var idStudyLims string
			if err := rows.Scan(&idOseqFlowcellTmp, &idSampleTmp, &idStudyLims); err != nil {
				return nil, fmt.Errorf("mlwh: scan oseq_flowcell sync row: %w", err)
			}
			if idStudyLims == "" {
				return nil, nil
			}

			return []any{idOseqFlowcellTmp, idSampleTmp, idStudyLims}, nil
		},
	}
}

// studyUsersWholesaleSpec mirrors study_users wholesale, INNER JOINing to study
// on id_study_tmp AND id_lims = 'SQSCP' so only rows whose study is an SQSCP
// study mirror (preserving the id_lims = 'SQSCP' invariant and ensuring the
// study_mirror.id_study_tmp link always resolves). The nullable login/email/name
// columns are scanned via sql.NullString and COALESCEd to empty string for the NOT NULL
// mirror columns; a row whose id_study_tmp is 0 is skipped.
func studyUsersWholesaleSpec() wholesaleMirrorSpec {
	return wholesaleMirrorSpec{
		syncTable:     syncTableStudyUsers,
		mirrorTable:   "study_users_mirror",
		mirrorColumns: []string{"id_study_users_tmp", "id_study_tmp", "role", "login", "email", "name", "last_updated"},
		sourceQuery:   `SELECT su.id_study_users_tmp, su.id_study_tmp, su.role, su.login, su.email, su.name, su.last_updated FROM study_users su INNER JOIN study ON study.id_study_tmp = su.id_study_tmp AND study.id_lims = 'SQSCP' ORDER BY su.id_study_users_tmp`,
		scan: func(rows *sql.Rows) ([]any, error) {
			var idStudyUsersTmp, idStudyTmp int64
			var role, login, email, name, lastUpdated sql.NullString
			if err := rows.Scan(&idStudyUsersTmp, &idStudyTmp, &role, &login, &email, &name, &lastUpdated); err != nil {
				return nil, fmt.Errorf("mlwh: scan study_users sync row: %w", err)
			}
			if idStudyTmp == 0 {
				return nil, nil
			}

			return []any{idStudyUsersTmp, idStudyTmp, role.String, login.String, email.String, name.String, normalizeWholesaleTime(lastUpdated)}, nil
		},
	}
}

func pacBioRunWellMetricsWholesaleSpec() wholesaleMirrorSpec {
	return wholesaleMirrorSpec{
		syncTable:   syncTablePacBioRunWellMetrics,
		mirrorTable: "pac_bio_run_well_metrics_mirror",
		mirrorColumns: []string{
			"id_pac_bio_rw_metrics_tmp", "pac_bio_run_name", "well_label", "plate_number",
			"run_start", "run_complete", "well_complete", "qc_seq_date", "run_status", "well_status", "last_updated",
		},
		sourceQuery: `SELECT id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_start, run_complete, well_complete, qc_seq_date, run_status, well_status, last_changed FROM pac_bio_run_well_metrics ORDER BY id_pac_bio_rw_metrics_tmp`,
		scan: func(rows *sql.Rows) ([]any, error) {
			var idTmp int64
			var runName, wellLabel string
			var plateNumber sql.NullInt64
			var runStart, runComplete, wellComplete, qcSeqDate, runStatus, wellStatus, lastChanged sql.NullString
			if err := rows.Scan(&idTmp, &runName, &wellLabel, &plateNumber, &runStart, &runComplete, &wellComplete, &qcSeqDate, &runStatus, &wellStatus, &lastChanged); err != nil {
				return nil, fmt.Errorf("mlwh: scan pac_bio_run_well_metrics sync row: %w", err)
			}

			return []any{idTmp, runName, wellLabel, plateNumber, runStart, runComplete, wellComplete, qcSeqDate, runStatus, wellStatus, normalizeWholesaleTime(lastChanged)}, nil
		},
	}
}

// eseqRunWholesaleSpec mirrors eseq_run using its REAL columns. The real table
// has NO run_status / run_start / run_complete columns: the run-level lifecycle is
// expressed by run_type, date_started, date_completed and a free-text outcome. The
// outcome maps to the mirror's run_status (the verbatim, open-vocabulary status
// string) and the two dates to run_start / run_complete. eseq_run has no
// last_changed; the mirror last_updated rides as the zero time.
func eseqRunWholesaleSpec() wholesaleMirrorSpec {
	return wholesaleMirrorSpec{
		syncTable:     syncTableEseqRun,
		mirrorTable:   "eseq_run_mirror",
		mirrorColumns: []string{"id_eseq_run_tmp", "run_name", "run_status", "run_start", "run_complete", "last_updated"},
		sourceQuery:   `SELECT id_eseq_run_tmp, run_name, outcome, date_started, date_completed FROM eseq_run ORDER BY id_eseq_run_tmp`,
		scan: func(rows *sql.Rows) ([]any, error) {
			var idTmp int64
			var runName string
			var outcome, dateStarted, dateCompleted sql.NullString
			if err := rows.Scan(&idTmp, &runName, &outcome, &dateStarted, &dateCompleted); err != nil {
				return nil, fmt.Errorf("mlwh: scan eseq_run sync row: %w", err)
			}

			return []any{idTmp, runName, outcome, dateStarted, dateCompleted, normalizeWholesaleTime(sql.NullString{})}, nil
		},
	}
}

// eseqRunLaneMetricsWholesaleSpec mirrors eseq_run_lane_metrics using its REAL
// columns. The real table has NO id_eseq_rlm_tmp (its primary key is the composite
// (id_run, lane)) and NO run_name; its within-sequencing timeline is the dated
// run_started / run_complete columns the Elembio progress path reads via id_run.
// last_changed feeds the mirror last_updated.
func eseqRunLaneMetricsWholesaleSpec() wholesaleMirrorSpec {
	return wholesaleMirrorSpec{
		syncTable:     syncTableEseqRunLaneMetrics,
		mirrorTable:   "eseq_run_lane_metrics_mirror",
		mirrorColumns: []string{"id_run", "lane", "run_started", "run_complete", "last_updated"},
		sourceQuery:   `SELECT id_run, lane, run_started, run_complete, last_changed FROM eseq_run_lane_metrics ORDER BY id_run, lane`,
		scan: func(rows *sql.Rows) ([]any, error) {
			var idRun, lane int64
			var runStarted, runComplete, lastChanged sql.NullString
			if err := rows.Scan(&idRun, &lane, &runStarted, &runComplete, &lastChanged); err != nil {
				return nil, fmt.Errorf("mlwh: scan eseq_run_lane_metrics sync row: %w", err)
			}

			return []any{idRun, lane, runStarted, runComplete, normalizeWholesaleTime(lastChanged)}, nil
		},
	}
}

// useqRunMetricsWholesaleSpec mirrors useq_run_metrics using its REAL columns. The
// real table has NO id_useq_run_metrics_tmp (its primary key is id_run), NO
// run_name, and NO run_status / run_start / run_complete columns: the run-level
// lifecycle is the dated run_in_progress (start) and run_archived (the only later
// run-level date) columns. run_folder_name supplies the mirror run_name; Ultimagen
// has no native run-status string, so the mirror run_status is empty. last_changed
// feeds the mirror last_updated.
func useqRunMetricsWholesaleSpec() wholesaleMirrorSpec {
	return wholesaleMirrorSpec{
		syncTable:     syncTableUseqRunMetrics,
		mirrorTable:   "useq_run_metrics_mirror",
		mirrorColumns: []string{"id_run", "run_name", "run_status", "run_start", "run_complete", "last_updated"},
		sourceQuery:   `SELECT id_run, run_folder_name, run_in_progress, run_archived, last_changed FROM useq_run_metrics ORDER BY id_run`,
		scan: func(rows *sql.Rows) ([]any, error) {
			var idRun int64
			var runFolderName string
			var runInProgress, runArchived, lastChanged sql.NullString
			if err := rows.Scan(&idRun, &runFolderName, &runInProgress, &runArchived, &lastChanged); err != nil {
				return nil, fmt.Errorf("mlwh: scan useq_run_metrics sync row: %w", err)
			}

			return []any{idRun, runFolderName, "", runInProgress, runArchived, normalizeWholesaleTime(lastChanged)}, nil
		},
	}
}

// normalizeWholesaleTime renders a nullable source timestamp as an RFC3339 string
// for a NOT NULL mirror last_updated column, using the zero time when the source
// value is NULL (the same NULL-rides-as-zero discipline used for iRODS created).
func normalizeWholesaleTime(value sql.NullString) string {
	if !value.Valid || value.String == "" {
		return formatSyncTime(time.Time{})
	}

	parsed, err := parseSyncTimeString(value.String)
	if err != nil {
		return formatSyncTime(time.Time{})
	}

	return formatSyncTime(parsed)
}

// syncWholesaleMirrorTable reads the whole source snapshot into memory, then
// clears and rebuilds the mirror inside one transaction so a concurrent reader
// sees the old snapshot until commit and the new one after (never a partial
// table). last_run advances; high_water stays empty (these tables have no
// meaningful watermark).
func syncWholesaleMirrorTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord, spec wholesaleMirrorSpec) (SyncReport, bool, error) {
	if spec.syncTable == "" {
		return SyncReport{}, false, fmt.Errorf("mlwh: unsupported wholesale mirror table")
	}

	snapshot, err := readWholesaleMirrorSnapshot(ctx, source, spec)
	if err != nil {
		return SyncReport{}, false, err
	}

	report := SyncReport{Table: spec.syncTable}
	if err = replaceWholesaleMirror(ctx, cache, spec, snapshot); err != nil {
		return report, false, err
	}

	report.Inserted = len(snapshot)

	return report, len(snapshot) > 0, nil
}

func readWholesaleMirrorSnapshot(ctx context.Context, source Querier, spec wholesaleMirrorSpec) ([][]any, error) {
	rows, err := source.QueryContext(ctx, spec.sourceQuery)
	if err != nil {
		return nil, fmt.Errorf("mlwh: query %s sync source: %w", spec.syncTable, err)
	}
	defer func() { _ = rows.Close() }()

	snapshot := make([][]any, 0)
	for rows.Next() {
		values, scanErr := spec.scan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if values == nil {
			continue
		}

		snapshot = append(snapshot, values)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read %s sync source: %w", spec.syncTable, err)
	}

	return snapshot, nil
}

func replaceWholesaleMirror(ctx context.Context, cache Cache, spec wholesaleMirrorSpec, snapshot [][]any) error {
	return withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+spec.mirrorTable); err != nil {
			return fmt.Errorf("mlwh: clear %s before wholesale replace: %w", spec.mirrorTable, err)
		}

		if err := insertWholesaleMirrorRows(ctx, tx, spec, snapshot); err != nil {
			return err
		}

		return writeSyncStateTx(ctx, tx, cache.Dialect(), spec.syncTable, time.Time{}, nil, false)
	})
}

func insertWholesaleMirrorRows(ctx context.Context, tx *sql.Tx, spec wholesaleMirrorSpec, snapshot [][]any) error {
	return forEachRowChunk(snapshot, syncStatementRowLimit(len(spec.mirrorColumns)), func(chunk [][]any) error {
		stmt := buildBulkInsertStatement(spec.mirrorTable, spec.mirrorColumns, len(chunk))
		args := make([]any, 0, len(chunk)*len(spec.mirrorColumns))
		for _, row := range chunk {
			args = append(args, row...)
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("mlwh: insert %s wholesale batch: %w", spec.mirrorTable, err)
		}

		return nil
	})
}

// ---------------------------------------------------------------------------
// seq_ops_tracking_per_sample full-table refresh with atomic swap.
// ---------------------------------------------------------------------------

var seqOpsTrackingPerSampleMirrorColumns = []string{
	"id_sample_lims",
	"sanger_sample_id",
	"sanger_sample_name",
	"study_id",
	"programme",
	"faculty_sponsor",
	"library_type",
	"platform",
	"manifest_created",
	"manifest_uploaded",
	"labware_received",
	"order_made",
	"working_dilution",
	"library_start",
	"library_complete",
	"sequencing_run_start",
	"sequencing_qc_complete",
}

// seqOpsTrackingPerSampleSyncRow is one tracking row: lookup/context columns plus
// the nine milestone datetimes, each nullable (a milestone is NULL until reached).
type seqOpsTrackingPerSampleSyncRow struct {
	IDSampleLims         string
	SangerSampleID       string
	SangerSampleName     string
	StudyID              string
	Programme            string
	FacultySponsor       string
	LibraryType          string
	Platform             string
	ManifestCreated      sql.NullString
	ManifestUploaded     sql.NullString
	LabwareReceived      sql.NullString
	OrderMade            sql.NullString
	WorkingDilution      sql.NullString
	LibraryStart         sql.NullString
	LibraryComplete      sql.NullString
	SequencingRunStart   sql.NullString
	SequencingQCComplete sql.NullString
}

func seqOpsTrackingPerSampleMirrorRowArgs(row seqOpsTrackingPerSampleSyncRow) []any {
	return []any{
		row.IDSampleLims,
		row.SangerSampleID,
		row.SangerSampleName,
		row.StudyID,
		row.Programme,
		row.FacultySponsor,
		row.LibraryType,
		row.Platform,
		row.ManifestCreated,
		row.ManifestUploaded,
		row.LabwareReceived,
		row.OrderMade,
		row.WorkingDilution,
		row.LibraryStart,
		row.LibraryComplete,
		row.SequencingRunStart,
		row.SequencingQCComplete,
	}
}

// The tracking table lives in the mlwh_reporting schema (NOT mlwarehouse, which
// the source connection defaults to), so the source name is schema-qualified.
// The read-only upstream user has access to mlwh_reporting; the hermetic SQLite
// source rewrites the qualifier away (it has no schemas).
// The context/lookup string columns (everything except the id_sample_lims primary
// key and the nullable milestone datetimes) are nullable in the real
// mlwh_reporting table, so they are COALESCEd to empty string to keep the plain-string scan
// targets and the NOT NULL mirror columns happy rather than failing with
// "converting NULL to string is unsupported".
const seqOpsTrackingPerSampleSourceQuery = `SELECT id_sample_lims, COALESCE(sanger_sample_id, '') AS sanger_sample_id, COALESCE(sanger_sample_name, '') AS sanger_sample_name, COALESCE(study_id, '') AS study_id, COALESCE(programme, '') AS programme, COALESCE(faculty_sponsor, '') AS faculty_sponsor, COALESCE(library_type, '') AS library_type, COALESCE(platform, '') AS platform, manifest_created, manifest_uploaded, labware_received, order_made, working_dilution, library_start, library_complete, sequencing_run_start, sequencing_qc_complete FROM mlwh_reporting.seq_ops_tracking_per_sample`

// syncSeqOpsTrackingPerSampleTable mirrors the tracking table by a full-table
// refresh: it captures the refresh time, reads the whole source snapshot, and
// builds-and-swaps it into the mirror atomically. The tracking table has no
// last_changed and mutates in place (and ~55% of rows have NULL manifest_created),
// so a GREATEST(milestones) watermark would miss in-place and backfilled fills; a
// full refresh is honest and its lag is surfaced via the freshness caveat. Its
// high_water is the refresh time and last_run the sync time.
func syncSeqOpsTrackingPerSampleTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	refreshTime := time.Now().UTC()

	snapshot, err := readSeqOpsTrackingPerSampleSnapshot(ctx, source)
	if err != nil {
		return SyncReport{}, false, err
	}

	report := SyncReport{Table: syncTableSeqOpsTrackingPerSample, HighWater: refreshTime}
	if err = writeSeqOpsTrackingPerSampleFullRefresh(ctx, cache, snapshot, refreshTime); err != nil {
		return report, false, err
	}

	report.Inserted = len(snapshot)

	return report, true, nil
}

func readSeqOpsTrackingPerSampleSnapshot(ctx context.Context, source Querier) ([]seqOpsTrackingPerSampleSyncRow, error) {
	rows, err := source.QueryContext(ctx, seqOpsTrackingPerSampleSourceQuery)
	if err != nil {
		return nil, fmt.Errorf("mlwh: query seq_ops_tracking_per_sample sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	snapshot := make([]seqOpsTrackingPerSampleSyncRow, 0)
	for rows.Next() {
		row, scanErr := scanSeqOpsTrackingPerSampleSyncRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}

		snapshot = append(snapshot, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read seq_ops_tracking_per_sample sync source: %w", err)
	}

	return snapshot, nil
}

func scanSeqOpsTrackingPerSampleSyncRow(rows *sql.Rows) (seqOpsTrackingPerSampleSyncRow, error) {
	var row seqOpsTrackingPerSampleSyncRow
	if err := rows.Scan(
		&row.IDSampleLims,
		&row.SangerSampleID,
		&row.SangerSampleName,
		&row.StudyID,
		&row.Programme,
		&row.FacultySponsor,
		&row.LibraryType,
		&row.Platform,
		&row.ManifestCreated,
		&row.ManifestUploaded,
		&row.LabwareReceived,
		&row.OrderMade,
		&row.WorkingDilution,
		&row.LibraryStart,
		&row.LibraryComplete,
		&row.SequencingRunStart,
		&row.SequencingQCComplete,
	); err != nil {
		return seqOpsTrackingPerSampleSyncRow{}, fmt.Errorf("mlwh: scan seq_ops_tracking_per_sample sync row: %w", err)
	}

	return row, nil
}

// writeSeqOpsTrackingPerSampleFullRefresh clears the tracking mirror and inserts
// the new snapshot inside a single transaction, so the build-and-swap is atomic:
// a concurrent reader on another connection sees either the whole old snapshot or
// the whole new one, never a partial table. It writes high_water = refreshTime and
// last_run = sync time.
func writeSeqOpsTrackingPerSampleFullRefresh(ctx context.Context, cache Cache, rows []seqOpsTrackingPerSampleSyncRow, refreshTime time.Time) error {
	return withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM seq_ops_tracking_per_sample_mirror`); err != nil {
			return fmt.Errorf("mlwh: clear seq_ops_tracking_per_sample_mirror before refresh: %w", err)
		}

		if err := insertSeqOpsTrackingPerSampleRows(ctx, tx, rows); err != nil {
			return err
		}

		return writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSeqOpsTrackingPerSample, refreshTime, nil, false)
	})
}

func insertSeqOpsTrackingPerSampleRows(ctx context.Context, tx *sql.Tx, rows []seqOpsTrackingPerSampleSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(seqOpsTrackingPerSampleMirrorColumns)), func(chunk []seqOpsTrackingPerSampleSyncRow) error {
		stmt := buildBulkInsertStatement("seq_ops_tracking_per_sample_mirror", seqOpsTrackingPerSampleMirrorColumns, len(chunk))
		args := make([]any, 0, len(chunk)*len(seqOpsTrackingPerSampleMirrorColumns))
		for _, row := range chunk {
			args = append(args, seqOpsTrackingPerSampleMirrorRowArgs(row)...)
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("mlwh: insert seq_ops_tracking_per_sample_mirror batch: %w", err)
		}

		return nil
	})
}
