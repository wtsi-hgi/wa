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
	"strings"
	"sync"
	"time"
)

const (
	syncTableSample       = "sample"
	syncTableStudy        = "study"
	syncTableIseqFlowcell = "iseq_flowcell"
	sqscpIDLims           = "SQSCP"
)

var supportedSyncTables = []string{syncTableSample, syncTableStudy, syncTableIseqFlowcell}

var sampleMirrorColumns = []string{
	"id_sample_tmp",
	"id_lims",
	"id_sample_lims",
	"uuid_sample_lims",
	"id_study_lims",
	"name",
	"sanger_id",
	"sanger_sample_id",
	"supplier_name",
	"accession_number",
	"donor_id",
	"library_type",
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
	"abstract",
	"abbreviation",
	"description",
	"data_release_strategy",
	"data_access_group",
	"hmdmc_number",
	"programme",
	"created",
	"reference_genome",
	"ethically_approved",
	"study_type",
	"contains_human_dna",
	"contaminated_human_dna",
	"study_visibility",
	"egadac_accession_number",
	"ega_policy_accession_number",
	"data_release_timing",
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
	{canonical: "abstract"},
	{canonical: "abbreviation"},
	{canonical: "description"},
	{canonical: "data_release_strategy"},
	{canonical: "data_access_group"},
	{canonical: "hmdmc_number"},
	{canonical: "programme"},
	{canonical: "created"},
	{canonical: "reference_genome"},
	{canonical: "ethically_approved"},
	{canonical: "study_type"},
	{canonical: "contains_human_dna"},
	{canonical: "contaminated_human_dna"},
	{canonical: "study_visibility"},
	{canonical: "egadac_accession_number", aliases: []string{"ega_dac_accession_number"}},
	{canonical: "ega_policy_accession_number"},
	{canonical: "data_release_timing"},
}

var syncStateColumns = []string{"table_name", "high_water", "last_run"}

func sampleSyncSourceQuery() string {
	return `SELECT id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, ` + sampleStudyLimsSubquery + `, last_updated FROM sample WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_sample_tmp`
}

func flowcellSyncSourceQuery() string {
	return `SELECT iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, COALESCE(study.id_study_lims, ''), iseq_flowcell.last_updated FROM iseq_flowcell LEFT JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp WHERE iseq_flowcell.last_updated >= ? AND (study.id_lims = 'SQSCP' OR study.id_lims IS NULL) ORDER BY iseq_flowcell.last_updated, iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, COALESCE(study.id_study_lims, '')`
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
	HighWater time.Time
}

func syncSampleTable(ctx context.Context, tx *sql.Tx, source Querier, dialect string, highWater time.Time) (SyncReport, bool, error) {
	rows, err := source.QueryContext(
		ctx,
		sampleSyncSourceQuery(),
		formatSyncTime(highWater),
	)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query sample sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableSample, HighWater: highWater}
	sawRows := false

	for rows.Next() {
		row, scanErr := scanSampleSyncRow(rows)
		if scanErr != nil {
			return SyncReport{}, false, scanErr
		}
		if row.Sample.IDLims != sqscpIDLims {
			continue
		}

		sawRows = true

		exists, existsErr := rowExists(ctx, tx, `SELECT 1 FROM sample_mirror WHERE id_sample_tmp = ? LIMIT 1`, row.Sample.IDSampleTmp)
		if existsErr != nil {
			return SyncReport{}, false, existsErr
		}

		if upsertErr := upsertSampleMirror(ctx, tx, dialect, row); upsertErr != nil {
			return SyncReport{}, false, upsertErr
		}
		if replaceErr := replaceDonorSample(ctx, tx, row); replaceErr != nil {
			return SyncReport{}, false, replaceErr
		}

		if exists {
			report.Updated++
		} else {
			report.Inserted++
		}
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}
	}

	if err = rows.Err(); err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: read sample sync source: %w", err)
	}

	return report, sawRows, nil
}

func syncStudyTable(ctx context.Context, tx *sql.Tx, source Querier, dialect string, highWater time.Time) (SyncReport, bool, error) {
	rows, err := queryStudySourceContext(ctx, source, func(columns string) string {
		return `SELECT ` + columns + `, last_updated FROM study WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_study_tmp`
	}, formatSyncTime(highWater))
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query study sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableStudy, HighWater: highWater}
	sawRows := false

	for rows.Next() {
		row, scanErr := scanStudySyncRow(rows)
		if scanErr != nil {
			return SyncReport{}, false, scanErr
		}
		if row.Study.IDLims != sqscpIDLims {
			continue
		}

		sawRows = true

		exists, existsErr := rowExists(ctx, tx, `SELECT 1 FROM study_mirror WHERE id_study_tmp = ? LIMIT 1`, row.Study.IDStudyTmp)
		if existsErr != nil {
			return SyncReport{}, false, existsErr
		}

		if upsertErr := upsertStudyMirror(ctx, tx, dialect, row); upsertErr != nil {
			return SyncReport{}, false, upsertErr
		}

		if exists {
			report.Updated++
		} else {
			report.Inserted++
		}
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}
	}

	if err = rows.Err(); err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: read study sync source: %w", err)
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

func syncFlowcellTable(ctx context.Context, tx *sql.Tx, source Querier, highWater time.Time) (SyncReport, bool, error) {
	rows, err := source.QueryContext(
		ctx,
		flowcellSyncSourceQuery(),
		formatSyncTime(highWater),
	)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query iseq_flowcell sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableIseqFlowcell, HighWater: highWater}
	sawRows := false
	seen := make(map[string]struct{})

	for rows.Next() {
		row, scanErr := scanFlowcellSyncRow(rows)
		if scanErr != nil {
			return SyncReport{}, false, scanErr
		}

		sawRows = true

		key := flowcellKey(row)
		if _, ok := seen[key]; ok {
			if row.LastUpdated.After(report.HighWater) {
				report.HighWater = row.LastUpdated
			}
			continue
		}
		seen[key] = struct{}{}

		exists, existsErr := rowExists(
			ctx,
			tx,
			`SELECT 1 FROM library_samples WHERE pipeline_id_lims = ? AND id_sample_tmp = ? AND id_study_lims = ? LIMIT 1`,
			row.PipelineIDLims,
			row.IDSampleTmp,
			row.IDStudyLims,
		)
		if existsErr != nil {
			return SyncReport{}, false, existsErr
		}

		if replaceErr := replaceLibrarySample(ctx, tx, row); replaceErr != nil {
			return SyncReport{}, false, replaceErr
		}

		if exists {
			report.Updated++
		} else {
			report.Inserted++
		}
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}
	}

	if err = rows.Err(); err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: read iseq_flowcell sync source: %w", err)
	}

	return report, sawRows, nil
}

// Sync serializes cache sync transactions for the configured cache backend.
func (c *Client) Sync(ctx context.Context, tables ...string) (reports []SyncReport, err error) {
	if c == nil || c.cache == nil {
		return nil, fmt.Errorf("mlwh: cache client not configured")
	}

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

	if c.syncRunner != nil {
		return c.runSyncRunner(ctx, tables)
	}

	normalized, err := normalizeSyncTables(tables)
	if err != nil {
		return nil, err
	}

	reports = make([]SyncReport, 0, len(normalized))
	for _, table := range normalized {
		report, syncErr := c.syncTable(ctx, table)
		if syncErr != nil {
			return reports, syncErr
		}

		reports = append(reports, report)
	}

	return reports, nil
}

func normalizeSyncTables(tables []string) ([]string, error) {
	if len(tables) == 0 {
		return append([]string(nil), supportedSyncTables...), nil
	}

	normalized := make([]string, 0, len(tables))
	seen := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		name := strings.ToLower(strings.TrimSpace(table))
		if name == "" {
			return nil, fmt.Errorf("mlwh: sync table name must not be empty")
		}
		if !isSupportedSyncTable(name) {
			return nil, fmt.Errorf("mlwh: unsupported sync table %q", table)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}

	return normalized, nil
}

func isSupportedSyncTable(table string) bool {
	for _, supported := range supportedSyncTables {
		if table == supported {
			return true
		}
	}

	return false
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

func (c *Client) syncTable(ctx context.Context, table string) (SyncReport, error) {
	tx, err := c.cache.DB().BeginTx(ctx, nil)
	if err != nil {
		return SyncReport{}, fmt.Errorf("mlwh: begin cache sync: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	highWater, stateExists, err := readSyncState(ctx, tx, table)
	if err != nil {
		return SyncReport{}, err
	}

	report, sawRows, err := c.syncTableData(ctx, tx, table, highWater)
	if err != nil {
		return SyncReport{}, err
	}

	if err = tx.Commit(); err != nil {
		return SyncReport{}, fmt.Errorf("mlwh: commit cache sync: %w", err)
	}

	committed = true
	c.clearExpandIdentifierCache()

	if sawRows || stateExists {
		if err = writeSyncState(ctx, c.cache.DB(), c.cache.Dialect(), table, report.HighWater); err != nil {
			return SyncReport{}, err
		}
	}

	return report, nil
}

func readSyncState(ctx context.Context, tx *sql.Tx, table string) (time.Time, bool, error) {
	var highWaterRaw string
	if err := tx.QueryRowContext(ctx, `SELECT high_water FROM sync_state WHERE table_name = ?`, table).Scan(&highWaterRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}

		return time.Time{}, false, fmt.Errorf("mlwh: query sync state for %s: %w", table, err)
	}

	highWater, err := parseSyncTimeValue(highWaterRaw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("mlwh: parse sync state for %s: %w", table, err)
	}

	return highWater, true, nil
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

func writeSyncState(ctx context.Context, db *sql.DB, dialect, table string, highWater time.Time) error {
	stmt := buildUpsertStatement(dialect, "sync_state", syncStateColumns, []string{"table_name"})
	_, err := db.ExecContext(ctx, stmt, table, formatSyncTime(highWater), formatSyncTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("mlwh: write sync state for %s: %w", table, err)
	}

	return nil
}

func buildUpsertStatement(dialect, table string, columns, keyColumns []string) string {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
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

	insertPrefix := fmt.Sprintf("INSERT INTO %s(%s) VALUES (%s)", table, strings.Join(columns, ", "), placeholders)
	if dialect == "mysql" {
		return insertPrefix + " ON DUPLICATE KEY UPDATE " + strings.Join(updateColumns, ", ")
	}

	return insertPrefix + " ON CONFLICT(" + strings.Join(keyColumns, ", ") + ") DO UPDATE SET " + strings.Join(updateColumns, ", ")
}

func formatSyncTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func (c *Client) syncTableData(ctx context.Context, tx *sql.Tx, table string, highWater time.Time) (SyncReport, bool, error) {
	source := c.syncSource
	if source == nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: sync source not configured")
	}

	switch table {
	case syncTableSample:
		return syncSampleTable(ctx, tx, source, c.cache.Dialect(), highWater)
	case syncTableStudy:
		return syncStudyTable(ctx, tx, source, c.cache.Dialect(), highWater)
	case syncTableIseqFlowcell:
		return syncFlowcellTable(ctx, tx, source, highWater)
	default:
		return SyncReport{}, false, fmt.Errorf("mlwh: unsupported sync table %q", table)
	}
}

type sampleSyncRow struct {
	Sample      Sample
	IDStudyLims string
	LastUpdated time.Time
}

func scanSampleSyncRow(rows *sql.Rows) (sampleSyncRow, error) {
	var row sampleSyncRow
	var lastUpdated any
	if err := rows.Scan(
		&row.Sample.IDSampleTmp,
		&row.Sample.IDLims,
		&row.Sample.IDSampleLims,
		&row.Sample.UUIDSampleLims,
		&row.Sample.Name,
		&row.Sample.SangerSampleID,
		&row.Sample.SupplierName,
		&row.Sample.AccessionNumber,
		&row.Sample.DonorID,
		&row.Sample.TaxonID,
		&row.Sample.CommonName,
		&row.Sample.Description,
		&row.IDStudyLims,
		&lastUpdated,
	); err != nil {
		return sampleSyncRow{}, fmt.Errorf("mlwh: scan sample sync row: %w", err)
	}
	row.Sample.IDStudyLims = row.IDStudyLims
	row.Sample.SangerID = row.Sample.Name

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return sampleSyncRow{}, fmt.Errorf("mlwh: parse sample last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func upsertSampleMirror(ctx context.Context, tx *sql.Tx, dialect string, row sampleSyncRow) error {
	stmt := buildUpsertStatement(dialect, "sample_mirror", sampleMirrorColumns, []string{"id_sample_tmp"})
	args := []any{
		row.Sample.IDSampleTmp,
		row.Sample.IDLims,
		row.Sample.IDSampleLims,
		row.Sample.UUIDSampleLims,
		row.Sample.IDStudyLims,
		row.Sample.Name,
		row.Sample.SangerID,
		row.Sample.SangerSampleID,
		row.Sample.SupplierName,
		row.Sample.AccessionNumber,
		row.Sample.DonorID,
		row.Sample.LibraryType,
		row.Sample.TaxonID,
		row.Sample.CommonName,
		row.Sample.Description,
		formatSyncTime(row.LastUpdated),
	}
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("mlwh: upsert sample mirror row %d: %w", row.Sample.IDSampleTmp, err)
	}

	return nil
}

func replaceDonorSample(ctx context.Context, tx *sql.Tx, row sampleSyncRow) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM donor_samples WHERE id_sample_tmp = ?`, row.Sample.IDSampleTmp); err != nil {
		return fmt.Errorf("mlwh: clear donor sample row %d: %w", row.Sample.IDSampleTmp, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO donor_samples(donor_id, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`,
		row.Sample.DonorID,
		row.Sample.IDSampleTmp,
		row.IDStudyLims,
	); err != nil {
		return fmt.Errorf("mlwh: insert donor sample row %d: %w", row.Sample.IDSampleTmp, err)
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
	stmt := buildUpsertStatement(dialect, "study_mirror", studyMirrorColumns, []string{"id_study_tmp"})
	args := []any{
		row.Study.IDStudyTmp,
		row.Study.IDLims,
		row.Study.IDStudyLims,
		row.Study.UUIDStudyLims,
		row.Study.Name,
		row.Study.AccessionNumber,
		row.Study.StudyTitle,
		row.Study.FacultySponsor,
		row.Study.State,
		row.Study.Abstract,
		row.Study.Abbreviation,
		row.Study.Description,
		row.Study.DataReleaseStrategy,
		row.Study.DataAccessGroup,
		row.Study.HMDMCNumber,
		row.Study.Programme,
		row.Study.Created,
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
	}
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("mlwh: upsert study mirror row %d: %w", row.Study.IDStudyTmp, err)
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
	var lastUpdated any
	if err := rows.Scan(&row.PipelineIDLims, &row.IDSampleTmp, &row.IDStudyLims, &lastUpdated); err != nil {
		return flowcellSyncRow{}, fmt.Errorf("mlwh: scan iseq_flowcell sync row: %w", err)
	}

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

func (c *Client) ensureResolverTableSynced(ctx context.Context, table string) error {
	if table == "" {
		return fmt.Errorf("mlwh: sync table name must not be empty")
	}

	warm, err := c.hasResolverSyncState(ctx, table)
	if err != nil {
		return err
	}
	if warm {
		return nil
	}

	_, err = c.Sync(ctx, table)

	return err
}

func (c *Client) hasResolverSyncState(ctx context.Context, table string) (bool, error) {
	db := c.ReadDB()
	if db == nil {
		if c == nil || c.cache == nil {
			return false, fmt.Errorf("mlwh: cache client not configured")
		}

		db = c.cache.DB()
	}

	var found int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`, table).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: query sync state for %s: %w", ErrUpstreamImpaired, table, err)
	}

	return true, nil
}
