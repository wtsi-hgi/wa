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
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	syncBatchSize                     = 1000
	maxSyncReconnectAttempts          = 5
	sqliteSyncPragmaCleanupTimeout    = 5 * time.Second
	syncTableSample                   = "sample"
	syncTableStudy                    = "study"
	syncTableIseqFlowcell             = "iseq_flowcell"
	syncTableIseqProductMetrics       = "iseq_product_metrics"
	syncTableSeqProductIRODSLocations = "seq_product_irods_locations"
	sqscpIDLims                       = "SQSCP"
)

var supportedSyncTables = []string{
	syncTableSample,
	syncTableStudy,
	syncTableIseqFlowcell,
	syncTableIseqProductMetrics,
	syncTableSeqProductIRODSLocations,
}

var sampleMirrorColumns = []string{
	"id_sample_tmp",
	"id_lims",
	"id_sample_lims",
	"uuid_sample_lims",
	"name",
	"sanger_sample_id",
	"supplier_name",
	"accession_number",
	"donor_id",
	"taxon_id",
	"common_name",
	"description",
	"last_updated",
}

var studyMirrorColumns = []string{
	"id_study_tmp",
	"id_lims",
	"id_study_lims",
	"uuid_study_lims",
	"name",
	"accession_number",
	"study_title",
	"faculty_sponsor",
	"state",
	"data_release_strategy",
	"data_access_group",
	"programme",
	"reference_genome",
	"ethically_approved",
	"study_type",
	"contains_human_dna",
	"contaminated_human_dna",
	"study_visibility",
	"ega_dac_accession_number",
	"ega_policy_accession_number",
	"data_release_timing",
	"last_updated",
}

var iseqProductMetricsMirrorColumns = []string{
	"id_iseq_product",
	"id_iseq_flowcell_tmp",
	"id_run",
	"position",
	"tag_index",
	"id_sample_tmp",
	"id_study_lims",
	"qc",
	"qc_lib",
	"qc_seq",
	"last_updated",
}

var seqProductIRODSLocationsMirrorColumns = []string{
	"id_iseq_product",
	"irods_root_collection",
	"irods_data_relative_path",
	"irods_collection",
	"irods_file_name",
	"id_sample_tmp",
	"id_study_lims",
	"last_updated",
}

type studySourceColumnSpec struct {
	canonical string
	aliases   []string
}

var studySourceColumnSpecs = []studySourceColumnSpec{
	{canonical: "id_study_tmp"},
	{canonical: "id_lims"},
	{canonical: "id_study_lims"},
	{canonical: "uuid_study_lims"},
	{canonical: "name"},
	{canonical: "accession_number"},
	{canonical: "study_title"},
	{canonical: "faculty_sponsor"},
	{canonical: "state"},
	{canonical: "data_release_strategy"},
	{canonical: "data_access_group"},
	{canonical: "programme"},
	{canonical: "reference_genome"},
	{canonical: "ethically_approved"},
	{canonical: "study_type"},
	{canonical: "contains_human_dna"},
	{canonical: "contaminated_human_dna"},
	{canonical: "study_visibility"},
	{canonical: "ega_dac_accession_number", aliases: []string{"egadac_accession_number"}},
	{canonical: "ega_policy_accession_number"},
	{canonical: "data_release_timing"},
}

var syncStateColumns = []string{"table_name", "high_water", "last_run", "resume_cursor", "indexes_dropped"}

type sampleMirrorIndexSpec struct {
	Name   string
	Column string
}

var sampleMirrorSecondaryIndexes = []sampleMirrorIndexSpec{
	{Name: "sample_mirror_id_sample_lims_idx", Column: "id_sample_lims"},
	{Name: "sample_mirror_uuid_sample_lims_idx", Column: "uuid_sample_lims"},
	{Name: "sample_mirror_name_idx", Column: "name"},
	{Name: "sample_mirror_sanger_sample_id_idx", Column: "sanger_sample_id"},
	{Name: "sample_mirror_supplier_name_idx", Column: "supplier_name"},
	{Name: "sample_mirror_accession_number_idx", Column: "accession_number"},
	{Name: "sample_mirror_donor_id_idx", Column: "donor_id"},
	{Name: "sample_mirror_last_updated_idx", Column: "last_updated"},
}

func sampleSyncSourceQuery() string {
	return `SELECT id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated FROM sample WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_sample_tmp`
}

func sampleSyncSourceQueryFromCursor() string {
	return `SELECT id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated FROM sample WHERE id_lims = 'SQSCP' AND ((last_updated > ?) OR (last_updated = ? AND id_sample_tmp > ?)) ORDER BY last_updated, id_sample_tmp`
}

func flowcellSyncSourceQuery() string {
	return `SELECT iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims, iseq_flowcell.last_updated FROM iseq_flowcell INNER JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp AND study.id_lims = 'SQSCP' WHERE iseq_flowcell.last_updated >= ? ORDER BY iseq_flowcell.last_updated, iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims`
}

func flowcellSyncSourceQueryFromCursor() string {
	return `SELECT iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims, iseq_flowcell.last_updated FROM iseq_flowcell INNER JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (iseq_flowcell.last_updated > ?) OR (iseq_flowcell.last_updated = ? AND (iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims) > (?, ?, ?)) ORDER BY iseq_flowcell.last_updated, iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims`
}

func iseqProductMetricsSyncSourceQuery() string {
	return `SELECT ipm.id_iseq_product, ipm.id_iseq_flowcell_tmp, ipm.id_run, ipm.position, ipm.tag_index, ifc.id_sample_tmp, study.id_study_lims, ipm.qc, ipm.qc_lib, ipm.qc_seq, ipm.last_updated FROM iseq_product_metrics ipm INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE ipm.last_updated >= ? ORDER BY ipm.last_updated, ipm.id_iseq_product`
}

func iseqProductMetricsSyncSourceQueryFromCursor() string {
	return `SELECT ipm.id_iseq_product, ipm.id_iseq_flowcell_tmp, ipm.id_run, ipm.position, ipm.tag_index, ifc.id_sample_tmp, study.id_study_lims, ipm.qc, ipm.qc_lib, ipm.qc_seq, ipm.last_updated FROM iseq_product_metrics ipm INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (ipm.last_updated > ?) OR (ipm.last_updated = ? AND ipm.id_iseq_product > ?) ORDER BY ipm.last_updated, ipm.id_iseq_product`
}

func seqProductIRODSLocationsSyncSourceQuery() string {
	return `SELECT spi.id_iseq_product, spi.irods_root_collection, spi.irods_data_relative_path, spi.irods_collection, spi.irods_file_name, ifc.id_sample_tmp, study.id_study_lims, spi.last_updated FROM seq_product_irods_locations spi INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_product = spi.id_iseq_product INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE spi.last_updated >= ? ORDER BY spi.last_updated, spi.id_iseq_product`
}

func seqProductIRODSLocationsSyncSourceQueryFromCursor() string {
	return `SELECT spi.id_iseq_product, spi.irods_root_collection, spi.irods_data_relative_path, spi.irods_collection, spi.irods_file_name, ifc.id_sample_tmp, study.id_study_lims, spi.last_updated FROM seq_product_irods_locations spi INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_product = spi.id_iseq_product INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (spi.last_updated > ?) OR (spi.last_updated = ? AND spi.id_iseq_product > ?) ORDER BY spi.last_updated, spi.id_iseq_product`
}

type syncStateRecord struct {
	HighWater      time.Time
	ResumeCursor   *string
	IndexesDropped bool
	Exists         bool
}

// Querier provides the upstream MLWH query surface used by sync.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// SyncReport describes the outcome of syncing one cache table.
type SyncReport struct {
	Table     string
	Inserted  int
	Updated   int
	Duration  time.Duration
	HighWater time.Time
}

func syncSampleTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, err := sampleSyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query sample sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if err = prepareSampleMirrorIndexesForSync(ctx, cache, &state); err != nil {
		return SyncReport{}, false, err
	}

	report := SyncReport{Table: syncTableSample, HighWater: state.HighWater}
	sawRows := false
	batch := make([]sampleSyncRow, 0, syncBatchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		batchHighWater := batch[len(batch)-1].LastUpdated
		resumeCursor := encodeSampleResumeCursor(batch[len(batch)-1])
		result, applyErr := writeSampleBatch(ctx, cache, batch, batchHighWater, &resumeCursor, state.IndexesDropped)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanSampleSyncRow(rows)
		if scanErr != nil {
			return report, false, scanErr
		}
		if row.Sample.IDLims != sqscpIDLims {
			continue
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
		return report, false, fmt.Errorf("mlwh: read sample sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeSampleSyncState(ctx, cache, report.HighWater, state.IndexesDropped); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func syncStudyTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, err := studySyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}

	rows, err := queryStudySourceContext(ctx, source, func(columns string) string {
		return strings.Replace(query, studySelectColumns, columns, 1)
	}, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query study sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableStudy, HighWater: state.HighWater}
	sawRows := false
	batch := make([]studySyncRow, 0, syncBatchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		batchHighWater := batch[len(batch)-1].LastUpdated
		resumeCursor := encodeStudyResumeCursor(batch[len(batch)-1])
		result, applyErr := writeStudyBatch(ctx, cache, batch, batchHighWater, &resumeCursor)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanStudySyncRow(rows)
		if scanErr != nil {
			return report, false, scanErr
		}
		if row.Study.IDLims != sqscpIDLims {
			continue
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
		return report, false, fmt.Errorf("mlwh: read study sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeSyncState(ctx, cache, syncTableStudy, report.HighWater); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func queryStudySourceContext(ctx context.Context, source Querier, queryForColumns func(string) string, args ...any) (*sql.Rows, error) {
	rows, err := source.QueryContext(ctx, queryForColumns(studySelectColumns), args...)
	if err == nil || !isUnknownStudyColumnError(err) {
		return rows, err
	}

	resolvedColumns, resolveErr := resolveStudySourceColumns(ctx, source)
	if resolveErr != nil {
		return nil, errors.Join(err, resolveErr)
	}

	return source.QueryContext(ctx, queryForColumns(resolvedColumns), args...)
}

func resolveStudySourceColumns(ctx context.Context, source Querier) (string, error) {
	rows, err := source.QueryContext(ctx, `SELECT * FROM study WHERE id_lims = 'SQSCP' LIMIT 0`)
	if err != nil {
		return "", fmt.Errorf("mlwh: probe study schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("mlwh: read study schema columns: %w", err)
	}

	available := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		available[column] = struct{}{}
	}

	resolved := make([]string, 0, len(studySourceColumnSpecs))
	for _, spec := range studySourceColumnSpecs {
		column, ok := resolveStudySourceColumn(spec, available)
		if !ok {
			return "", fmt.Errorf("mlwh: study source missing required column %q", spec.canonical)
		}

		if column == spec.canonical {
			resolved = append(resolved, column)

			continue
		}

		resolved = append(resolved, column+` AS `+spec.canonical)
	}

	return strings.Join(resolved, ", "), nil
}

func resolveStudySourceColumn(spec studySourceColumnSpec, available map[string]struct{}) (string, bool) {
	if _, ok := available[spec.canonical]; ok {
		return spec.canonical, true
	}

	for _, alias := range spec.aliases {
		if _, ok := available[alias]; ok {
			return alias, true
		}
	}

	return "", false
}

func isUnknownStudyColumnError(err error) bool {
	message := strings.ToLower(err.Error())

	return strings.Contains(message, "unknown column") || strings.Contains(message, "no such column")
}

func syncFlowcellTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, err := flowcellSyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query iseq_flowcell sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableIseqFlowcell, HighWater: state.HighWater}
	sawRows := false
	seen := make(map[string]struct{})
	batch := make([]flowcellSyncRow, 0, syncBatchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		batchHighWater := batch[len(batch)-1].LastUpdated
		resumeCursor := encodeFlowcellResumeCursor(batch[len(batch)-1])
		result, applyErr := writeFlowcellBatch(ctx, cache, batch, batchHighWater, &resumeCursor)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanFlowcellSyncRow(rows)
		if scanErr != nil {
			return report, false, scanErr
		}

		sawRows = true
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}

		if row.PipelineIDLims == "" {
			continue
		}

		key := flowcellKey(row)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		batch = append(batch, row)
		if len(batch) == syncBatchSize {
			if err = flushBatch(); err != nil {
				return report, false, err
			}
		}
	}

	if err = rows.Err(); err != nil {
		return report, false, fmt.Errorf("mlwh: read iseq_flowcell sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeSyncState(ctx, cache, syncTableIseqFlowcell, report.HighWater); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

// Sync syncs all supported cache tables in parallel.
func (c *Client) Sync(ctx context.Context) ([]SyncReport, error) {
	return c.syncTables(ctx)
}

type sqliteSyncWritePragmaState struct {
	Synchronous int
	CacheSize   int
	TempStore   int
}

func configureSQLiteSyncWritePragmas(ctx context.Context, cache Cache) (func() error, error) {
	if cache == nil || cache.Dialect() != "sqlite" {
		return nil, nil
	}

	state, err := readSQLiteSyncWritePragmaState(ctx, cache.DB())
	if err != nil {
		return nil, err
	}

	for _, statement := range []string{
		`PRAGMA synchronous = NORMAL`,
		`PRAGMA cache_size = -200000`,
		`PRAGMA temp_store = MEMORY`,
	} {
		if _, err = cache.DB().ExecContext(ctx, statement); err != nil {
			return nil, fmt.Errorf("mlwh: configure sqlite sync write pragma %q: %w", statement, err)
		}
	}

	return func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), sqliteSyncPragmaCleanupTimeout)
		defer cancel()

		if _, restoreErr := cache.DB().ExecContext(cleanupCtx, fmt.Sprintf(`PRAGMA synchronous = %d`, state.Synchronous)); restoreErr != nil {
			return fmt.Errorf("mlwh: restore sqlite sync write pragma synchronous: %w", restoreErr)
		}
		if _, restoreErr := cache.DB().ExecContext(cleanupCtx, fmt.Sprintf(`PRAGMA cache_size = %d`, state.CacheSize)); restoreErr != nil {
			return fmt.Errorf("mlwh: restore sqlite sync write pragma cache_size: %w", restoreErr)
		}
		if _, restoreErr := cache.DB().ExecContext(cleanupCtx, fmt.Sprintf(`PRAGMA temp_store = %d`, state.TempStore)); restoreErr != nil {
			return fmt.Errorf("mlwh: restore sqlite sync write pragma temp_store: %w", restoreErr)
		}

		return nil
	}, nil
}

func readSQLiteSyncWritePragmaState(ctx context.Context, db *sql.DB) (sqliteSyncWritePragmaState, error) {
	var state sqliteSyncWritePragmaState

	if err := db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&state.Synchronous); err != nil {
		return sqliteSyncWritePragmaState{}, fmt.Errorf("mlwh: read sqlite sync write pragma synchronous: %w", err)
	}
	if err := db.QueryRowContext(ctx, `PRAGMA cache_size`).Scan(&state.CacheSize); err != nil {
		return sqliteSyncWritePragmaState{}, fmt.Errorf("mlwh: read sqlite sync write pragma cache_size: %w", err)
	}
	if err := db.QueryRowContext(ctx, `PRAGMA temp_store`).Scan(&state.TempStore); err != nil {
		return sqliteSyncWritePragmaState{}, fmt.Errorf("mlwh: read sqlite sync write pragma temp_store: %w", err)
	}

	return state, nil
}

func (c *Client) syncTables(ctx context.Context) (reports []SyncReport, err error) {
	if c == nil || c.cache == nil {
		return nil, fmt.Errorf("mlwh: cache client not configured")
	}

	tables := append([]string(nil), supportedSyncTables...)

	mu := c.syncMu
	if mu == nil {
		mu = &sync.Mutex{}
		c.syncMu = mu
	}

	mu.Lock()
	defer mu.Unlock()

	releaseLock, err := c.acquireSyncLock(ctx)
	if err != nil {
		return nil, err
	}
	if releaseLock != nil {
		defer func() {
			releaseErr := releaseLock()
			if err == nil && releaseErr != nil {
				err = releaseErr
			}
		}()
	}

	restorePragmas, err := configureSQLiteSyncWritePragmas(ctx, c.cache)
	if err != nil {
		return nil, err
	}
	if restorePragmas != nil {
		defer func() {
			restoreErr := restorePragmas()
			if err == nil && restoreErr != nil {
				err = restoreErr
			}
		}()
	}

	if c.syncRunner != nil {
		return c.runSyncRunner(ctx, tables)
	}

	type syncResult struct {
		report SyncReport
		err    error
	}

	resultCh := make(chan syncResult, len(tables))
	var waitGroup sync.WaitGroup

	for _, table := range tables {
		waitGroup.Add(1)

		go func(table string) {
			defer waitGroup.Done()

			report, syncErr := c.syncTable(ctx, table)
			if syncErr != nil {
				resultCh <- syncResult{report: report, err: fmt.Errorf("%s: %w", table, syncErr)}
				return
			}

			c.emitSyncReport(report)
			resultCh <- syncResult{report: report}
		}(table)
	}

	waitGroup.Wait()
	close(resultCh)

	reports = make([]SyncReport, 0, len(tables))
	var errs []error
	for result := range resultCh {
		if result.report.Table != "" && (result.err == nil || syncReportHasObservedState(result.report)) {
			reports = append(reports, result.report)
		}

		if result.err != nil {
			errs = append(errs, result.err)
		}
	}

	return reports, errors.Join(errs...)
}

func readSyncStateFromDB(ctx context.Context, db *sql.DB, table string) (syncStateRecord, error) {
	var highWaterRaw any
	var resumeCursor sql.NullString
	var indexesDropped int
	if err := db.QueryRowContext(ctx, `SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`, table).Scan(&highWaterRaw, &resumeCursor, &indexesDropped); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return syncStateRecord{}, nil
		}

		return syncStateRecord{}, fmt.Errorf("mlwh: query sync state for %s: %w", table, err)
	}

	highWater, err := parseSyncTimeValue(highWaterRaw)
	if err != nil {
		return syncStateRecord{}, fmt.Errorf("mlwh: parse sync state for %s: %w", table, err)
	}

	state := syncStateRecord{HighWater: highWater, Exists: true}
	if resumeCursor.Valid {
		state.ResumeCursor = &resumeCursor.String
	}
	state.IndexesDropped = indexesDropped == 1

	return state, nil
}

func (c *Client) emitSyncReport(report SyncReport) {
	if c == nil || c.syncReportWriter == nil {
		return
	}

	_, _ = io.WriteString(
		c.syncReportWriter,
		fmt.Sprintf(
			"%s inserted=%d updated=%d high_water=%s\n",
			report.Table,
			report.Inserted,
			report.Updated,
			report.HighWater.UTC().Format("2006-01-02T15:04:05Z"),
		),
	)
}

func (c *Client) runSyncRunner(ctx context.Context, tables []string) (reports []SyncReport, err error) {
	tx, err := c.cache.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mlwh: begin cache sync: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err = c.syncRunner(ctx, tx, tables); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("mlwh: commit cache sync: %w", err)
	}

	committed = true
	c.clearExpandIdentifierCache()

	return nil, nil
}

func (c *Client) syncTable(ctx context.Context, table string) (report SyncReport, err error) {
	report = SyncReport{Table: table}
	started := time.Now()
	defer func() {
		report.Duration = time.Since(started)
	}()
	retryCount := 0

	for {
		state, err := readSyncStateFromDB(ctx, c.cache.DB(), table)
		if err != nil {
			return report, err
		}

		next, _, err := c.syncTableData(ctx, table, state)
		report = mergeSyncReport(report, next)
		if err == nil {
			c.clearExpandIdentifierCache()

			return report, nil
		}
		if !isTransientSyncSourceError(err) {
			return report, err
		}
		if syncReportCommittedProgress(next) {
			retryCount = 0
		}
		if retryCount == maxSyncReconnectAttempts {
			return report, fmt.Errorf("mlwh: sync %s: %w", table, err)
		}

		retryCount++
		backoff := syncReconnectBackoff(retryCount)
		c.emitSyncRetry(table, retryCount, err, backoff)
		if sleepErr := c.sleepSyncRetry(ctx, backoff); sleepErr != nil {
			return report, fmt.Errorf("mlwh: sync %s: %w", table, sleepErr)
		}
	}
}

func syncReportCommittedProgress(report SyncReport) bool {
	return report.Inserted > 0 || report.Updated > 0
}

func syncReportHasObservedState(report SyncReport) bool {
	return syncReportCommittedProgress(report) || !report.HighWater.IsZero()
}

func mergeSyncReport(total, next SyncReport) SyncReport {
	if total.Table == "" {
		total.Table = next.Table
	}
	total.Inserted += next.Inserted
	total.Updated += next.Updated
	total.Duration += next.Duration
	if next.HighWater.After(total.HighWater) {
		total.HighWater = next.HighWater
	}

	return total
}

func isTransientSyncSourceError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrConnDone) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	type transientError interface {
		Temporary() bool
		Timeout() bool
	}

	var netErr transientError
	if errors.As(err, &netErr) && (netErr.Temporary() || netErr.Timeout()) {
		return true
	}

	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"invalid connection",
		"unexpected eof",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"timeout awaiting response headers",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

func syncReconnectBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return time.Second
	}

	backoff := time.Second << (attempt - 1)
	if backoff > 30*time.Second {
		return 30 * time.Second
	}

	return backoff
}

func (c *Client) emitSyncRetry(table string, attempt int, retryErr error, backoff time.Duration) {
	var writer io.Writer = os.Stderr
	if c != nil && c.syncRetryWriter != nil {
		writer = c.syncRetryWriter
	}

	_, _ = fmt.Fprintf(writer, "mlwh sync: %s reconnecting attempt %d/%d after %v: backoff %s\n", table, attempt, maxSyncReconnectAttempts, retryErr, backoff)
}

func (c *Client) sleepSyncRetry(ctx context.Context, delay time.Duration) error {
	if c != nil && c.syncRetrySleep != nil {
		return c.syncRetrySleep(ctx, delay)
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withSyncWriteTx(ctx context.Context, cache Cache, apply func(*sql.Tx) error) error {
	if sqliteCache, ok := cache.(*sqliteCache); ok && sqliteCache.writeMu != nil {
		sqliteCache.writeMu.Lock()
		defer sqliteCache.writeMu.Unlock()
	}

	tx, err := cache.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mlwh: begin cache sync: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if cache.Dialect() == "sqlite" {
		if _, err = tx.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
			return fmt.Errorf("mlwh: configure sqlite sync busy timeout: %w", err)
		}
	}

	if err = apply(tx); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("mlwh: commit cache sync: %w", err)
	}

	committed = true

	return nil
}

func readSyncState(ctx context.Context, tx *sql.Tx, table string) (syncStateRecord, error) {
	var highWaterRaw any
	var resumeCursor sql.NullString
	var indexesDropped int
	if err := tx.QueryRowContext(ctx, `SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`, table).Scan(&highWaterRaw, &resumeCursor, &indexesDropped); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return syncStateRecord{}, nil
		}

		return syncStateRecord{}, fmt.Errorf("mlwh: query sync state for %s: %w", table, err)
	}

	highWater, err := parseSyncTimeValue(highWaterRaw)
	if err != nil {
		return syncStateRecord{}, fmt.Errorf("mlwh: parse sync state for %s: %w", table, err)
	}

	state := syncStateRecord{HighWater: highWater, Exists: true}
	if resumeCursor.Valid {
		state.ResumeCursor = &resumeCursor.String
	}
	state.IndexesDropped = indexesDropped == 1

	return state, nil
}

func parseSyncTimeValue(raw any) (time.Time, error) {
	switch value := raw.(type) {
	case time.Time:
		return value.UTC(), nil
	case string:
		return parseSyncTimeString(value)
	case []byte:
		return parseSyncTimeString(string(value))
	case nil:
		return time.Time{}, fmt.Errorf("nil time value")
	default:
		return time.Time{}, fmt.Errorf("unsupported time value %T", raw)
	}
}

func parseSyncTimeString(raw string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported time %q", raw)
}

func writeSyncState(ctx context.Context, db *sql.DB, dialect, table string, highWater time.Time, resumeCursor *string, indexesDropped bool) error {
	stmt := buildUpsertStatement(dialect, "sync_state", syncStateColumns, []string{"table_name"})
	_, err := db.ExecContext(ctx, stmt, syncStateArgs(table, highWater, resumeCursor, indexesDropped)...)
	if err != nil {
		return fmt.Errorf("mlwh: write sync state for %s: %w", table, err)
	}

	return nil
}

func writeSyncStateTx(ctx context.Context, tx *sql.Tx, dialect, table string, highWater time.Time, resumeCursor *string, indexesDropped bool) error {
	stmt := buildUpsertStatement(dialect, "sync_state", syncStateColumns, []string{"table_name"})
	_, err := tx.ExecContext(ctx, stmt, syncStateArgs(table, highWater, resumeCursor, indexesDropped)...)
	if err != nil {
		return fmt.Errorf("mlwh: write sync state for %s: %w", table, err)
	}

	return nil
}

func syncStateArgs(table string, highWater time.Time, resumeCursor *string, indexesDropped bool) []any {
	args := []any{table, formatSyncTime(highWater), formatSyncTime(time.Now().UTC())}
	if resumeCursor == nil {
		args = append(args, nil)
	} else {
		args = append(args, *resumeCursor)
	}
	if indexesDropped {
		args = append(args, 1)
	} else {
		args = append(args, 0)
	}

	return args
}

func buildUpsertStatement(dialect, table string, columns, keyColumns []string) string {
	return buildBulkUpsertStatement(dialect, table, columns, keyColumns, 1)
}

func buildBulkUpsertStatement(dialect, table string, columns, keyColumns []string, rowCount int) string {
	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ") + ")"
	values := strings.TrimSuffix(strings.Repeat(rowPlaceholder+", ", rowCount), ", ")
	updateColumns := make([]string, 0, len(columns))
	keySet := make(map[string]struct{}, len(keyColumns))
	for _, key := range keyColumns {
		keySet[key] = struct{}{}
	}
	for _, column := range columns {
		if _, ok := keySet[column]; ok {
			continue
		}
		if dialect == "mysql" {
			updateColumns = append(updateColumns, fmt.Sprintf("%s = VALUES(%s)", column, column))
			continue
		}
		updateColumns = append(updateColumns, fmt.Sprintf("%s = excluded.%s", column, column))
	}
	if len(updateColumns) == 0 {
		if dialect == "mysql" {
			updateColumns = append(updateColumns, fmt.Sprintf("%s = VALUES(%s)", keyColumns[0], keyColumns[0]))
		} else {
			updateColumns = append(updateColumns, fmt.Sprintf("%s = excluded.%s", keyColumns[0], keyColumns[0]))
		}
	}

	insertPrefix := fmt.Sprintf("INSERT INTO %s(%s) VALUES %s", table, strings.Join(columns, ", "), values)
	if dialect == "mysql" {
		return insertPrefix + " ON DUPLICATE KEY UPDATE " + strings.Join(updateColumns, ", ")
	}

	return insertPrefix + " ON CONFLICT(" + strings.Join(keyColumns, ", ") + ") DO UPDATE SET " + strings.Join(updateColumns, ", ")
}

func buildBulkInsertStatement(table string, columns []string, rowCount int) string {
	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ") + ")"
	values := strings.TrimSuffix(strings.Repeat(rowPlaceholder+", ", rowCount), ", ")

	return fmt.Sprintf("INSERT INTO %s(%s) VALUES %s", table, strings.Join(columns, ", "), values)
}

func formatSyncTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func sampleSyncQuery(state syncStateRecord) (string, []any, error) {
	if state.ResumeCursor == nil {
		return sampleSyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, nil
	}

	lastUpdated, idSampleTmp, err := parseTwoPartResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, fmt.Errorf("mlwh: parse sample resume cursor: %w", err)
	}

	return sampleSyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), idSampleTmp}, nil
}

func studySyncQuery(state syncStateRecord) (string, []any, error) {
	query := `SELECT ` + studySelectColumns + `, last_updated FROM study WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_study_tmp`
	queryFromCursor := `SELECT ` + studySelectColumns + `, last_updated FROM study WHERE id_lims = 'SQSCP' AND ((last_updated > ?) OR (last_updated = ? AND id_study_tmp > ?)) ORDER BY last_updated, id_study_tmp`
	if state.ResumeCursor == nil {
		return query, []any{formatSyncTime(state.HighWater)}, nil
	}

	lastUpdated, idStudyTmp, err := parseTwoPartResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, fmt.Errorf("mlwh: parse study resume cursor: %w", err)
	}

	return queryFromCursor, []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), idStudyTmp}, nil
}

func flowcellSyncQuery(state syncStateRecord) (string, []any, error) {
	if state.ResumeCursor == nil {
		return flowcellSyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, nil
	}

	lastUpdated, pipelineIDLims, idSampleTmp, idStudyLims, err := parseFlowcellResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, fmt.Errorf("mlwh: parse iseq_flowcell resume cursor: %w", err)
	}

	return flowcellSyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), pipelineIDLims, idSampleTmp, idStudyLims}, nil
}

func iseqProductMetricsSyncQuery(state syncStateRecord) (string, []any, error) {
	if state.ResumeCursor == nil {
		return iseqProductMetricsSyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, nil
	}

	lastUpdated, idIseqProduct, err := parseTwoPartResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, fmt.Errorf("mlwh: parse iseq_product_metrics resume cursor: %w", err)
	}

	return iseqProductMetricsSyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), idIseqProduct}, nil
}

func seqProductIRODSLocationsSyncQuery(state syncStateRecord) (string, []any, error) {
	if state.ResumeCursor == nil {
		return seqProductIRODSLocationsSyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, nil
	}

	lastUpdated, idIseqProduct, err := parseTwoPartResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, fmt.Errorf("mlwh: parse seq_product_irods_locations resume cursor: %w", err)
	}

	return seqProductIRODSLocationsSyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), idIseqProduct}, nil
}

func finalizeSyncState(ctx context.Context, cache Cache, table string, highWater time.Time) error {
	return withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		return writeSyncStateTx(ctx, tx, cache.Dialect(), table, highWater, nil, false)
	})
}

func finalizeSampleSyncState(ctx context.Context, cache Cache, highWater time.Time, indexesDropped bool) error {
	return withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		if indexesDropped {
			if err := createSampleMirrorSecondaryIndexes(ctx, tx, cache.Dialect()); err != nil {
				return err
			}
		}

		return writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSample, highWater, nil, false)
	})
}

func prepareSampleMirrorIndexesForSync(ctx context.Context, cache Cache, state *syncStateRecord) error {
	if state == nil {
		return fmt.Errorf("mlwh: sample sync state not configured")
	}
	if !shouldDropSampleMirrorIndexes(*state) {
		return nil
	}

	if err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		if err := dropSampleMirrorSecondaryIndexes(ctx, tx, cache.Dialect()); err != nil {
			return err
		}

		return writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSample, time.Time{}, nil, true)
	}); err != nil {
		return err
	}

	state.Exists = true
	state.HighWater = time.Time{}
	state.IndexesDropped = true

	return nil
}

func shouldDropSampleMirrorIndexes(state syncStateRecord) bool {
	if !state.Exists {
		return true
	}

	return state.HighWater.IsZero() && !state.IndexesDropped
}

func sampleMirrorIndexInventoryQuery(dialect string) string {
	if dialect == "mysql" {
		return `SELECT DISTINCT INDEX_NAME FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sample_mirror' AND INDEX_NAME <> 'PRIMARY'`
	}

	return `SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'sample_mirror'`
}

func sampleMirrorExistingIndexes(ctx context.Context, tx *sql.Tx, dialect string) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, sampleMirrorIndexInventoryQuery(dialect))
	if err != nil {
		return nil, fmt.Errorf("mlwh: query sample_mirror indexes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	indexes := make(map[string]struct{}, len(sampleMirrorSecondaryIndexes))
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("mlwh: scan sample_mirror index: %w", err)
		}

		indexes[name] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: iterate sample_mirror indexes: %w", err)
	}

	return indexes, nil
}

func dropSampleMirrorSecondaryIndexes(ctx context.Context, tx *sql.Tx, dialect string) error {
	existing, err := sampleMirrorExistingIndexes(ctx, tx, dialect)
	if err != nil {
		return err
	}

	for _, index := range sampleMirrorSecondaryIndexes {
		if _, ok := existing[index.Name]; !ok {
			continue
		}

		stmt := `DROP INDEX IF EXISTS ` + index.Name
		if dialect == "mysql" {
			stmt = `DROP INDEX ` + index.Name + ` ON sample_mirror`
		}

		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("mlwh: drop sample_mirror index %s: %w", index.Name, err)
		}
	}

	return nil
}

func createSampleMirrorSecondaryIndexes(ctx context.Context, tx *sql.Tx, dialect string) error {
	existing, err := sampleMirrorExistingIndexes(ctx, tx, dialect)
	if err != nil {
		return err
	}

	for _, index := range sampleMirrorSecondaryIndexes {
		if _, ok := existing[index.Name]; ok {
			continue
		}

		stmt := fmt.Sprintf(`CREATE INDEX %s ON sample_mirror(%s)`, index.Name, index.Column)
		if dialect == "sqlite" {
			stmt = fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON sample_mirror(%s)`, index.Name, index.Column)
		}

		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("mlwh: create sample_mirror index %s: %w", index.Name, err)
		}
	}

	return nil
}

func parseTwoPartResumeCursor(raw string) (time.Time, int64, error) {
	parts := strings.Split(raw, "\t")
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("expected 2 fields, got %d", len(parts))
	}

	lastUpdated, err := parseSyncTimeString(parts[0])
	if err != nil {
		return time.Time{}, 0, err
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("parse integer field %q: %w", parts[1], err)
	}

	return lastUpdated, id, nil
}

func parseFlowcellResumeCursor(raw string) (time.Time, string, int64, string, error) {
	parts := strings.Split(raw, "\t")
	if len(parts) != 4 {
		return time.Time{}, "", 0, "", fmt.Errorf("expected 4 fields, got %d", len(parts))
	}

	lastUpdated, err := parseSyncTimeString(parts[0])
	if err != nil {
		return time.Time{}, "", 0, "", err
	}

	idSampleTmp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return time.Time{}, "", 0, "", fmt.Errorf("parse integer field %q: %w", parts[2], err)
	}

	return lastUpdated, parts[1], idSampleTmp, parts[3], nil
}

func encodeSampleResumeCursor(row sampleSyncRow) string {
	return formatSyncTime(row.LastUpdated) + "\t" + strconv.FormatInt(row.Sample.IDSampleTmp, 10)
}

func encodeStudyResumeCursor(row studySyncRow) string {
	return formatSyncTime(row.LastUpdated) + "\t" + strconv.FormatInt(row.Study.IDStudyTmp, 10)
}

func encodeFlowcellResumeCursor(row flowcellSyncRow) string {
	return formatSyncTime(row.LastUpdated) + "\t" + row.PipelineIDLims + "\t" + strconv.FormatInt(row.IDSampleTmp, 10) + "\t" + row.IDStudyLims
}

func encodeIseqProductMetricsResumeCursor(row iseqProductMetricsSyncRow) string {
	return formatSyncTime(row.LastUpdated) + "\t" + strconv.FormatInt(row.IDIseqProduct, 10)
}

func encodeSeqProductIRODSLocationsResumeCursor(row seqProductIRODSLocationsSyncRow) string {
	return formatSyncTime(row.LastUpdated) + "\t" + strconv.FormatInt(row.IDIseqProduct, 10)
}

func (c *Client) syncTableData(ctx context.Context, table string, state syncStateRecord) (SyncReport, bool, error) {
	source := c.syncSource
	if source == nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: sync source not configured")
	}

	switch table {
	case syncTableSample:
		return syncSampleTable(ctx, c.cache, source, state)
	case syncTableStudy:
		return syncStudyTable(ctx, c.cache, source, state)
	case syncTableIseqFlowcell:
		return syncFlowcellTable(ctx, c.cache, source, state)
	case syncTableIseqProductMetrics:
		return syncIseqProductMetricsTable(ctx, c.cache, source, state)
	case syncTableSeqProductIRODSLocations:
		return syncSeqProductIRODSLocationsTable(ctx, c.cache, source, state)
	default:
		return SyncReport{}, false, fmt.Errorf("mlwh: unsupported sync table %q", table)
	}
}

type iseqProductMetricsSyncRow struct {
	IDIseqProduct     int64
	IDIseqFlowcellTmp int64
	IDRun             int
	Position          int
	TagIndex          int
	IDSampleTmp       int64
	IDStudyLims       string
	QC                int
	QCLib             int
	QCSeq             int
	LastUpdated       time.Time
}

func syncIseqProductMetricsTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, err := iseqProductMetricsSyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query iseq_product_metrics sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableIseqProductMetrics, HighWater: state.HighWater}
	sawRows := false
	batch := make([]iseqProductMetricsSyncRow, 0, syncBatchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		batchHighWater := batch[len(batch)-1].LastUpdated
		resumeCursor := encodeIseqProductMetricsResumeCursor(batch[len(batch)-1])
		result, applyErr := writeIseqProductMetricsBatch(ctx, cache, batch, batchHighWater, &resumeCursor)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanIseqProductMetricsSyncRow(rows)
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
		return report, false, fmt.Errorf("mlwh: read iseq_product_metrics sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeSyncState(ctx, cache, syncTableIseqProductMetrics, report.HighWater); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func scanIseqProductMetricsSyncRow(rows *sql.Rows) (iseqProductMetricsSyncRow, error) {
	var row iseqProductMetricsSyncRow
	var lastUpdated any
	if err := rows.Scan(
		&row.IDIseqProduct,
		&row.IDIseqFlowcellTmp,
		&row.IDRun,
		&row.Position,
		&row.TagIndex,
		&row.IDSampleTmp,
		&row.IDStudyLims,
		&row.QC,
		&row.QCLib,
		&row.QCSeq,
		&lastUpdated,
	); err != nil {
		return iseqProductMetricsSyncRow{}, fmt.Errorf("mlwh: scan iseq_product_metrics sync row: %w", err)
	}

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return iseqProductMetricsSyncRow{}, fmt.Errorf("mlwh: parse iseq_product_metrics last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func upsertIseqProductMetricsMirror(ctx context.Context, tx *sql.Tx, dialect string, row iseqProductMetricsSyncRow) error {
	return upsertIseqProductMetricsMirrorBatch(ctx, tx, dialect, []iseqProductMetricsSyncRow{row})
}

func upsertIseqProductMetricsMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []iseqProductMetricsSyncRow) error {
	stmt := buildBulkUpsertStatement(dialect, "iseq_product_metrics_mirror", iseqProductMetricsMirrorColumns, []string{"id_iseq_product"}, len(rows))
	args := make([]any, 0, len(rows)*len(iseqProductMetricsMirrorColumns))
	for _, row := range rows {
		args = append(args,
			row.IDIseqProduct,
			row.IDIseqFlowcellTmp,
			row.IDRun,
			row.Position,
			row.TagIndex,
			row.IDSampleTmp,
			row.IDStudyLims,
			row.QC,
			row.QCLib,
			row.QCSeq,
			formatSyncTime(row.LastUpdated),
		)
	}
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("mlwh: upsert iseq_product_metrics mirror batch: %w", err)
	}

	return nil
}

type seqProductIRODSLocationsSyncRow struct {
	IDIseqProduct         int64
	IRODSRootCollection   string
	IRODSDataRelativePath string
	IRODSCollection       string
	IRODSFileName         string
	IDSampleTmp           int64
	IDStudyLims           string
	LastUpdated           time.Time
}

func syncSeqProductIRODSLocationsTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, err := seqProductIRODSLocationsSyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query seq_product_irods_locations sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableSeqProductIRODSLocations, HighWater: state.HighWater}
	sawRows := false
	batch := make([]seqProductIRODSLocationsSyncRow, 0, syncBatchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		batchHighWater := batch[len(batch)-1].LastUpdated
		resumeCursor := encodeSeqProductIRODSLocationsResumeCursor(batch[len(batch)-1])
		result, applyErr := writeSeqProductIRODSLocationsBatch(ctx, cache, batch, batchHighWater, &resumeCursor)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanSeqProductIRODSLocationsSyncRow(rows)
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
		return report, false, fmt.Errorf("mlwh: read seq_product_irods_locations sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeSyncState(ctx, cache, syncTableSeqProductIRODSLocations, report.HighWater); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func scanSeqProductIRODSLocationsSyncRow(rows *sql.Rows) (seqProductIRODSLocationsSyncRow, error) {
	var row seqProductIRODSLocationsSyncRow
	var lastUpdated any
	if err := rows.Scan(
		&row.IDIseqProduct,
		&row.IRODSRootCollection,
		&row.IRODSDataRelativePath,
		&row.IRODSCollection,
		&row.IRODSFileName,
		&row.IDSampleTmp,
		&row.IDStudyLims,
		&lastUpdated,
	); err != nil {
		return seqProductIRODSLocationsSyncRow{}, fmt.Errorf("mlwh: scan seq_product_irods_locations sync row: %w", err)
	}

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return seqProductIRODSLocationsSyncRow{}, fmt.Errorf("mlwh: parse seq_product_irods_locations last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func upsertSeqProductIRODSLocationsMirror(ctx context.Context, tx *sql.Tx, dialect string, row seqProductIRODSLocationsSyncRow) error {
	return upsertSeqProductIRODSLocationsMirrorBatch(ctx, tx, dialect, []seqProductIRODSLocationsSyncRow{row})
}

func upsertSeqProductIRODSLocationsMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []seqProductIRODSLocationsSyncRow) error {
	stmt := buildBulkUpsertStatement(dialect, "seq_product_irods_locations_mirror", seqProductIRODSLocationsMirrorColumns, []string{"id_iseq_product"}, len(rows))
	args := make([]any, 0, len(rows)*len(seqProductIRODSLocationsMirrorColumns))
	for _, row := range rows {
		args = append(args,
			row.IDIseqProduct,
			row.IRODSRootCollection,
			row.IRODSDataRelativePath,
			row.IRODSCollection,
			row.IRODSFileName,
			row.IDSampleTmp,
			row.IDStudyLims,
			formatSyncTime(row.LastUpdated),
		)
	}
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("mlwh: upsert seq_product_irods_locations mirror batch: %w", err)
	}

	return nil
}

type sampleSyncRow struct {
	Sample      Sample
	LastUpdated time.Time
}

type nullableSampleSyncFields struct {
	idLims          sql.NullString
	idSampleLims    sql.NullString
	uuidSampleLims  sql.NullString
	name            sql.NullString
	sangerSampleID  sql.NullString
	supplierName    sql.NullString
	accessionNumber sql.NullString
	donorID         sql.NullString
	taxonID         sql.NullInt64
	commonName      sql.NullString
	description     sql.NullString
}

func scanSampleSyncRow(rows *sql.Rows) (sampleSyncRow, error) {
	var row sampleSyncRow
	var lastUpdated any
	nullable := &nullableSampleSyncFields{}
	if err := rows.Scan(
		&row.Sample.IDSampleTmp,
		&nullable.idLims,
		&nullable.idSampleLims,
		&nullable.uuidSampleLims,
		&nullable.name,
		&nullable.sangerSampleID,
		&nullable.supplierName,
		&nullable.accessionNumber,
		&nullable.donorID,
		&nullable.taxonID,
		&nullable.commonName,
		&nullable.description,
		&lastUpdated,
	); err != nil {
		return sampleSyncRow{}, fmt.Errorf("mlwh: scan sample sync row: %w", err)
	}
	row.Sample.IDLims = nullStringValue(nullable.idLims)
	row.Sample.IDSampleLims = nullStringValue(nullable.idSampleLims)
	row.Sample.UUIDSampleLims = nullStringValue(nullable.uuidSampleLims)
	row.Sample.Name = nullStringValue(nullable.name)
	row.Sample.SangerSampleID = nullStringValue(nullable.sangerSampleID)
	row.Sample.SupplierName = nullStringValue(nullable.supplierName)
	row.Sample.AccessionNumber = nullStringValue(nullable.accessionNumber)
	row.Sample.DonorID = nullStringValue(nullable.donorID)
	if nullable.taxonID.Valid {
		row.Sample.TaxonID = int(nullable.taxonID.Int64)
	}
	row.Sample.CommonName = nullStringValue(nullable.commonName)
	row.Sample.Description = nullStringValue(nullable.description)

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return sampleSyncRow{}, fmt.Errorf("mlwh: parse sample last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func upsertSampleMirror(ctx context.Context, tx *sql.Tx, dialect string, row sampleSyncRow) error {
	return upsertSampleMirrorBatch(ctx, tx, dialect, []sampleSyncRow{row})
}

func upsertSampleMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []sampleSyncRow) error {
	stmt := buildBulkUpsertStatement(dialect, "sample_mirror", sampleMirrorColumns, []string{"id_sample_tmp"}, len(rows))
	args := make([]any, 0, len(rows)*len(sampleMirrorColumns))
	for _, row := range rows {
		args = append(args,
			row.Sample.IDSampleTmp,
			row.Sample.IDLims,
			row.Sample.IDSampleLims,
			row.Sample.UUIDSampleLims,
			row.Sample.Name,
			row.Sample.SangerSampleID,
			row.Sample.SupplierName,
			row.Sample.AccessionNumber,
			row.Sample.DonorID,
			row.Sample.TaxonID,
			row.Sample.CommonName,
			row.Sample.Description,
			formatSyncTime(row.LastUpdated),
		)
	}
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("mlwh: upsert sample mirror batch: %w", err)
	}

	return nil
}

func replaceDonorSample(ctx context.Context, tx *sql.Tx, row sampleSyncRow) error {
	return replaceDonorSampleBatch(ctx, tx, []sampleSyncRow{row})
}

func replaceDonorSampleBatch(ctx context.Context, tx *sql.Tx, rows []sampleSyncRow) error {
	whereClause, whereArgs := buildKeyInClause([]string{"id_sample_tmp"}, sampleBatchKeys(rows))
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM donor_samples WHERE %s", whereClause), whereArgs...); err != nil {
		return fmt.Errorf("mlwh: clear donor sample batch: %w", err)
	}

	insert := buildBulkInsertStatement("donor_samples", []string{"donor_id", "id_sample_tmp"}, len(rows))
	args := make([]any, 0, len(rows)*2)
	for _, row := range rows {
		args = append(args, row.Sample.DonorID, row.Sample.IDSampleTmp)
	}
	if _, err := tx.ExecContext(ctx, insert, args...); err != nil {
		return fmt.Errorf("mlwh: insert donor sample batch: %w", err)
	}

	return nil
}

type studySyncRow struct {
	Study       Study
	LastUpdated time.Time
}

func scanStudySyncRow(rows *sql.Rows) (studySyncRow, error) {
	var row studySyncRow
	var lastUpdated any
	targets, apply := studyScanTargets(&row.Study)
	targets = append(targets, &lastUpdated)
	if err := rows.Scan(targets...); err != nil {
		return studySyncRow{}, fmt.Errorf("mlwh: scan study sync row: %w", err)
	}
	apply()

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return studySyncRow{}, fmt.Errorf("mlwh: parse study last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func upsertStudyMirror(ctx context.Context, tx *sql.Tx, dialect string, row studySyncRow) error {
	return upsertStudyMirrorBatch(ctx, tx, dialect, []studySyncRow{row})
}

func upsertStudyMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []studySyncRow) error {
	stmt := buildBulkUpsertStatement(dialect, "study_mirror", studyMirrorColumns, []string{"id_study_tmp"}, len(rows))
	args := make([]any, 0, len(rows)*len(studyMirrorColumns))
	for _, row := range rows {
		args = append(args,
			row.Study.IDStudyTmp,
			row.Study.IDLims,
			row.Study.IDStudyLims,
			row.Study.UUIDStudyLims,
			row.Study.Name,
			row.Study.AccessionNumber,
			row.Study.StudyTitle,
			row.Study.FacultySponsor,
			row.Study.State,
			row.Study.DataReleaseStrategy,
			row.Study.DataAccessGroup,
			row.Study.Programme,
			row.Study.ReferenceGenome,
			row.Study.EthicallyApproved,
			row.Study.StudyType,
			row.Study.ContainsHumanDNA,
			row.Study.ContaminatedHumanDNA,
			row.Study.StudyVisibility,
			row.Study.EGADACAccessionNumber,
			row.Study.EGAPolicyAccessionNumber,
			row.Study.DataReleaseTiming,
			formatSyncTime(row.LastUpdated),
		)
	}
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("mlwh: upsert study mirror batch: %w", err)
	}

	return nil
}

type flowcellSyncRow struct {
	PipelineIDLims string
	IDSampleTmp    int64
	IDStudyLims    string
	LastUpdated    time.Time
}

func scanFlowcellSyncRow(rows *sql.Rows) (flowcellSyncRow, error) {
	var row flowcellSyncRow
	var pipelineIDLims sql.NullString
	var studyLims sql.NullString
	var lastUpdated any
	if err := rows.Scan(&pipelineIDLims, &row.IDSampleTmp, &studyLims, &lastUpdated); err != nil {
		return flowcellSyncRow{}, fmt.Errorf("mlwh: scan iseq_flowcell sync row: %w", err)
	}
	row.PipelineIDLims = nullStringValue(pipelineIDLims)
	row.IDStudyLims = nullStringValue(studyLims)

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return flowcellSyncRow{}, fmt.Errorf("mlwh: parse iseq_flowcell last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func flowcellKey(row flowcellSyncRow) string {
	return fmt.Sprintf("%s\x00%d\x00%s", row.PipelineIDLims, row.IDSampleTmp, row.IDStudyLims)
}

func replaceLibrarySample(ctx context.Context, tx *sql.Tx, row flowcellSyncRow) error {
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM library_samples WHERE pipeline_id_lims = ? AND id_sample_tmp = ? AND id_study_lims = ?`,
		row.PipelineIDLims,
		row.IDSampleTmp,
		row.IDStudyLims,
	); err != nil {
		return fmt.Errorf("mlwh: clear library sample row %s/%d/%s: %w", row.PipelineIDLims, row.IDSampleTmp, row.IDStudyLims, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`,
		row.PipelineIDLims,
		row.IDSampleTmp,
		row.IDStudyLims,
	); err != nil {
		return fmt.Errorf("mlwh: insert library sample row %s/%d/%s: %w", row.PipelineIDLims, row.IDSampleTmp, row.IDStudyLims, err)
	}

	return nil
}

func upsertLibrarySampleBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []flowcellSyncRow) error {
	stmt := buildBulkUpsertStatement(dialect, "library_samples", []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims"}, []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims"}, len(rows))
	args := make([]any, 0, len(rows)*3)
	for _, row := range rows {
		args = append(args, row.PipelineIDLims, row.IDSampleTmp, row.IDStudyLims)
	}
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("mlwh: upsert library sample batch: %w", err)
	}

	return nil
}

type syncBatchResult struct {
	Inserted int
	Updated  int
}

func countExistingKeys(ctx context.Context, tx *sql.Tx, table string, keyColumns []string, keys [][]any) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	whereClause, args := buildKeyInClause(keyColumns, keys)
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", table, whereClause)
	var count int
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("mlwh: count existing %s batch rows: %w", table, err)
	}

	return count, nil
}

func buildKeyInClause(keyColumns []string, keys [][]any) (string, []any) {
	if len(keyColumns) == 1 {
		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(keys)), ", ")
		args := make([]any, 0, len(keys))
		for _, key := range keys {
			args = append(args, key[0])
		}

		return fmt.Sprintf("%s IN (%s)", keyColumns[0], placeholders), args
	}

	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(keyColumns)), ", ") + ")"
	placeholders := strings.TrimSuffix(strings.Repeat(rowPlaceholder+", ", len(keys)), ", ")
	args := make([]any, 0, len(keys)*len(keyColumns))
	for _, key := range keys {
		args = append(args, key...)
	}

	return "(" + strings.Join(keyColumns, ", ") + ") IN (" + placeholders + ")", args
}

func dedupeSampleBatch(rows []sampleSyncRow) []sampleSyncRow {
	indices := make(map[int64]int, len(rows))
	deduped := make([]sampleSyncRow, 0, len(rows))
	for _, row := range rows {
		index, ok := indices[row.Sample.IDSampleTmp]
		if ok {
			deduped[index] = row
			continue
		}

		indices[row.Sample.IDSampleTmp] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func dedupeStudyBatch(rows []studySyncRow) []studySyncRow {
	indices := make(map[int64]int, len(rows))
	deduped := make([]studySyncRow, 0, len(rows))
	for _, row := range rows {
		index, ok := indices[row.Study.IDStudyTmp]
		if ok {
			deduped[index] = row
			continue
		}

		indices[row.Study.IDStudyTmp] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func dedupeFlowcellBatch(rows []flowcellSyncRow) []flowcellSyncRow {
	indices := make(map[string]int, len(rows))
	deduped := make([]flowcellSyncRow, 0, len(rows))
	for _, row := range rows {
		key := flowcellKey(row)
		index, ok := indices[key]
		if ok {
			deduped[index] = row
			continue
		}

		indices[key] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func dedupeIseqProductMetricsBatch(rows []iseqProductMetricsSyncRow) []iseqProductMetricsSyncRow {
	indices := make(map[int64]int, len(rows))
	deduped := make([]iseqProductMetricsSyncRow, 0, len(rows))
	for _, row := range rows {
		index, ok := indices[row.IDIseqProduct]
		if ok {
			deduped[index] = row
			continue
		}

		indices[row.IDIseqProduct] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func dedupeSeqProductIRODSLocationsBatch(rows []seqProductIRODSLocationsSyncRow) []seqProductIRODSLocationsSyncRow {
	indices := make(map[int64]int, len(rows))
	deduped := make([]seqProductIRODSLocationsSyncRow, 0, len(rows))
	for _, row := range rows {
		index, ok := indices[row.IDIseqProduct]
		if ok {
			deduped[index] = row
			continue
		}

		indices[row.IDIseqProduct] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func sampleBatchKeys(rows []sampleSyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.Sample.IDSampleTmp})
	}

	return keys
}

func studyBatchKeys(rows []studySyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.Study.IDStudyTmp})
	}

	return keys
}

func flowcellBatchKeys(rows []flowcellSyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.PipelineIDLims, row.IDSampleTmp, row.IDStudyLims})
	}

	return keys
}

func iseqProductMetricsBatchKeys(rows []iseqProductMetricsSyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.IDIseqProduct})
	}

	return keys
}

func seqProductIRODSLocationsBatchKeys(rows []seqProductIRODSLocationsSyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.IDIseqProduct})
	}

	return keys
}

func validateFlowcellBatch(rows []flowcellSyncRow) error {
	for _, row := range rows {
		if row.IDStudyLims == "" {
			return fmt.Errorf("mlwh: library_samples row (%s, %d) violates constraint: id_study_lims must not be empty", row.PipelineIDLims, row.IDSampleTmp)
		}
	}

	return nil
}

func validateIseqProductMetricsBatch(rows []iseqProductMetricsSyncRow) error {
	for _, row := range rows {
		if row.IDStudyLims == "" {
			return fmt.Errorf("mlwh: iseq_product_metrics_mirror row %d violates constraint: id_study_lims must not be empty", row.IDIseqProduct)
		}
	}

	return nil
}

func validateSeqProductIRODSLocationsBatch(rows []seqProductIRODSLocationsSyncRow) error {
	for _, row := range rows {
		if row.IDStudyLims == "" {
			return fmt.Errorf("mlwh: seq_product_irods_locations_mirror row %d violates constraint: id_study_lims must not be empty", row.IDIseqProduct)
		}
	}

	return nil
}

func writeSampleBatch(ctx context.Context, cache Cache, rows []sampleSyncRow, highWater time.Time, resumeCursor *string, indexesDropped bool) (syncBatchResult, error) {
	deduped := dedupeSampleBatch(rows)
	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing, err := countExistingKeys(ctx, tx, "sample_mirror", []string{"id_sample_tmp"}, sampleBatchKeys(deduped))
		if err != nil {
			return err
		}
		if err = upsertSampleMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
			return err
		}
		if err = replaceDonorSampleBatch(ctx, tx, deduped); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err = writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSample, highWater, resumeCursor, indexesDropped); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func writeStudyBatch(ctx context.Context, cache Cache, rows []studySyncRow, highWater time.Time, resumeCursor *string) (syncBatchResult, error) {
	deduped := dedupeStudyBatch(rows)
	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing, err := countExistingKeys(ctx, tx, "study_mirror", []string{"id_study_tmp"}, studyBatchKeys(deduped))
		if err != nil {
			return err
		}
		if err = upsertStudyMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err = writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableStudy, highWater, resumeCursor, false); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func writeFlowcellBatch(ctx context.Context, cache Cache, rows []flowcellSyncRow, highWater time.Time, resumeCursor *string) (syncBatchResult, error) {
	deduped := dedupeFlowcellBatch(rows)
	if err := validateFlowcellBatch(deduped); err != nil {
		return syncBatchResult{}, err
	}

	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing, err := countExistingKeys(ctx, tx, "library_samples", []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims"}, flowcellBatchKeys(deduped))
		if err != nil {
			return err
		}
		if err = upsertLibrarySampleBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err = writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableIseqFlowcell, highWater, resumeCursor, false); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func writeIseqProductMetricsBatch(ctx context.Context, cache Cache, rows []iseqProductMetricsSyncRow, highWater time.Time, resumeCursor *string) (syncBatchResult, error) {
	deduped := dedupeIseqProductMetricsBatch(rows)
	if err := validateIseqProductMetricsBatch(deduped); err != nil {
		return syncBatchResult{}, err
	}

	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing, err := countExistingKeys(ctx, tx, "iseq_product_metrics_mirror", []string{"id_iseq_product"}, iseqProductMetricsBatchKeys(deduped))
		if err != nil {
			return err
		}
		if err = upsertIseqProductMetricsMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err = writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableIseqProductMetrics, highWater, resumeCursor, false); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func writeSeqProductIRODSLocationsBatch(ctx context.Context, cache Cache, rows []seqProductIRODSLocationsSyncRow, highWater time.Time, resumeCursor *string) (syncBatchResult, error) {
	deduped := dedupeSeqProductIRODSLocationsBatch(rows)
	if err := validateSeqProductIRODSLocationsBatch(deduped); err != nil {
		return syncBatchResult{}, err
	}

	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing, err := countExistingKeys(ctx, tx, "seq_product_irods_locations_mirror", []string{"id_iseq_product"}, seqProductIRODSLocationsBatchKeys(deduped))
		if err != nil {
			return err
		}
		if err = upsertSeqProductIRODSLocationsMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err = writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSeqProductIRODSLocations, highWater, resumeCursor, false); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func rowExists(ctx context.Context, tx *sql.Tx, query string, args ...any) (bool, error) {
	var found int
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, fmt.Errorf("mlwh: query cache row existence: %w", err)
	}

	return true, nil
}

func (c *Client) requireResolverSyncState(ctx context.Context, table string) error {
	return c.requireAnySyncState(ctx, table)
}

func neverSyncedReadErr() error {
	return fmt.Errorf("%w: %w", ErrNotFound, ErrCacheNeverSynced)
}

func (c *Client) requireAnySyncState(ctx context.Context, tables ...string) error {
	summary, err := c.requiredSyncStateSummary(ctx, tables...)
	if err != nil {
		return err
	}
	if !summary.allPresent {
		return neverSyncedReadErr()
	}

	return nil
}

type requiredSyncStateSummaryResult struct {
	allAbsent bool
	allPresent bool
}

func (c *Client) requiredSyncStateSummary(ctx context.Context, tables ...string) (requiredSyncStateSummaryResult, error) {
	if len(tables) == 0 {
		return requiredSyncStateSummaryResult{}, fmt.Errorf("mlwh: at least one sync table is required")
	}

	db := c.ReadDB()
	if db == nil {
		if c == nil || c.cache == nil {
			return requiredSyncStateSummaryResult{}, fmt.Errorf("mlwh: cache client not configured")
		}

		db = c.cache.DB()
	}

	seen := make(map[string]struct{}, len(tables))
	summary := requiredSyncStateSummaryResult{allAbsent: true, allPresent: true}

	for _, table := range tables {
		if table == "" {
			return requiredSyncStateSummaryResult{}, fmt.Errorf("mlwh: sync table name must not be empty")
		}
		if _, ok := seen[table]; ok {
			continue
		}
		seen[table] = struct{}{}

		state, err := readSyncStateFromDB(ctx, db, table)
		if err != nil {
			return requiredSyncStateSummaryResult{}, fmt.Errorf("%w: query sync state for %s: %w", ErrUpstreamImpaired, table, err)
		}
		if state.Exists {
			summary.allAbsent = false
		} else {
			summary.allPresent = false
		}
	}

	return summary, nil
}

func (c *Client) hasSyncState(ctx context.Context, table string) (bool, error) {
	if table == "" {
		return false, fmt.Errorf("mlwh: sync table name must not be empty")
	}

	summary, err := c.requiredSyncStateSummary(ctx, table)
	if err != nil {
		return false, err
	}

	return !summary.allAbsent, nil
}
