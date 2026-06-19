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

package results

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const enableForeignKeysSQL = `PRAGMA foreign_keys = ON`

const minSearchSuggestionQueryLength = 2

const createResultSetsTableSQL = `
CREATE TABLE IF NOT EXISTS result_sets (
	id                  VARCHAR(64)  NOT NULL PRIMARY KEY,
	pipeline_identifier VARCHAR(512) NOT NULL,
	run_key             VARCHAR(512) NOT NULL,
	requester           VARCHAR(255) NOT NULL,
	operator            VARCHAR(255) NOT NULL,
	command             TEXT         NOT NULL,
	pipeline_name       VARCHAR(255) NOT NULL,
	pipeline_version    VARCHAR(255) NOT NULL,
	output_directory    TEXT         NOT NULL,
	output_directory_gid BIGINT      NULL,
	created_at          VARCHAR(30)  NOT NULL,
	updated_at          VARCHAR(30)  NOT NULL
);`

const addResultSetsOutputDirectoryGIDColumnSQL = `
ALTER TABLE result_sets ADD COLUMN output_directory_gid BIGINT NULL;`

const createResultFilesTableSQL = `
CREATE TABLE IF NOT EXISTS result_files (
	result_id VARCHAR(64)  NOT NULL,
	path      TEXT         NOT NULL,
	mtime     VARCHAR(30)  NOT NULL,
	size      BIGINT       NOT NULL,
	kind      VARCHAR(10)  NOT NULL,
	FOREIGN KEY (result_id)
		REFERENCES result_sets(id) ON DELETE CASCADE
);`

const createResultMetadataTableSQL = `
CREATE TABLE IF NOT EXISTS result_metadata (
	result_id      VARCHAR(64)  NOT NULL,
	meta_key       VARCHAR(255) NOT NULL,
	value_ordinal  INTEGER      NOT NULL DEFAULT 0,
	value          TEXT         NOT NULL,
	FOREIGN KEY (result_id)
		REFERENCES result_sets(id) ON DELETE CASCADE
);`

const dropResultMetadataPrimaryKeySQL = `
ALTER TABLE result_metadata DROP PRIMARY KEY;`

const addResultMetadataValueOrdinalColumnSQL = `
ALTER TABLE result_metadata ADD COLUMN value_ordinal INTEGER NOT NULL DEFAULT 0;`

const createResultMetadataResultIDIndexSQL = `
CREATE INDEX idx_result_metadata_result_id
	ON result_metadata(result_id);`

const createResultMetadataMetaKeyValueIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_result_metadata_meta_key_value
	ON result_metadata(meta_key, value);`

const createResultMetadataMetaKeyValueIndexMySQLSQL = `
CREATE INDEX idx_result_metadata_meta_key_value
	ON result_metadata(meta_key, value(255));`

type searchSuggestionSource struct {
	fieldKey string
	column   string
	order    int
}

var searchSuggestionSources = []searchSuggestionSource{
	{fieldKey: "pipeline_name", column: "pipeline_name", order: 10},
	{fieldKey: "run_key", column: "run_key", order: 20},
	{fieldKey: "user", column: "requester", order: 30},
	{fieldKey: "operator", column: "operator", order: 40},
	{fieldKey: "pipeline_version", column: "pipeline_version", order: 50},
	{fieldKey: "pipeline_identifier", column: "pipeline_identifier", order: 60},
	{fieldKey: "output_directory", column: "output_directory", order: 70},
}

// OutputDirectoryGID returns the Unix group ID for an output directory.
func OutputDirectoryGID(path string) (*int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("determine output directory gid: %w", err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("determine output directory gid: stat data unavailable")
	}

	gid := int64(stat.Gid)

	return &gid, nil
}

type loadedResultMetadata struct {
	single map[string]string
	values map[string][]string
}

func appendMetadataValue(metadata map[string][]string, key string, value string) {
	for _, existingValue := range metadata[key] {
		if existingValue == value {
			return
		}
	}

	metadata[key] = append(metadata[key], value)
}

func singleMetadataFromValues(metadata map[string][]string) map[string]string {
	if len(metadata) == 0 {
		return map[string]string{}
	}

	single := make(map[string]string, len(metadata))
	for key, values := range metadata {
		if len(values) == 0 {
			continue
		}

		single[key] = values[0]
	}

	return single
}

type sqliteResultMetadataSchema struct {
	primaryKeyColumns []string
	hasValueOrdinal   bool
}

func sqliteResultMetadataSchemaInfo(db *sql.DB) (sqliteResultMetadataSchema, bool, error) {
	rows, err := db.Query(`PRAGMA table_info(result_metadata)`)
	if err != nil {
		if isIgnorablePragmaError(err) {
			return sqliteResultMetadataSchema{}, false, nil
		}

		return sqliteResultMetadataSchema{}, false, fmt.Errorf("inspect result_metadata schema: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	type primaryKeyColumn struct {
		name     string
		position int
	}

	columns := []primaryKeyColumn{}
	hasValueOrdinal := false
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue any
		var primaryKeyPosition int

		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKeyPosition); err != nil {
			return sqliteResultMetadataSchema{}, true, fmt.Errorf("scan result_metadata schema: %w", err)
		}
		if name == "value_ordinal" {
			hasValueOrdinal = true
		}
		if primaryKeyPosition > 0 {
			columns = append(columns, primaryKeyColumn{name: name, position: primaryKeyPosition})
		}
	}
	if err := rows.Err(); err != nil {
		return sqliteResultMetadataSchema{}, true, fmt.Errorf("iterate result_metadata schema: %w", err)
	}

	sort.Slice(columns, func(i, j int) bool {
		return columns[i].position < columns[j].position
	})

	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = column.name
	}

	return sqliteResultMetadataSchema{
		primaryKeyColumns: names,
		hasValueOrdinal:   hasValueOrdinal,
	}, true, nil
}

func ensureResultMetadataSchema(db *sql.DB) error {
	schema, inspectedSQLite, err := sqliteResultMetadataSchemaInfo(db)
	if err != nil {
		return err
	}
	if inspectedSQLite {
		if len(schema.primaryKeyColumns) == 0 && schema.hasValueOrdinal {
			return ensureResultMetadataResultIDIndex(db)
		}

		if err := migrateSQLiteResultMetadataSchema(db, schema.hasValueOrdinal); err != nil {
			return err
		}

		return ensureResultMetadataResultIDIndex(db)
	}

	if err := ensureResultMetadataResultIDIndex(db); err != nil {
		return err
	}

	if _, err := db.Exec(dropResultMetadataPrimaryKeySQL); err != nil && !isMissingPrimaryKeyError(err) {
		return fmt.Errorf("drop result_metadata primary key: %w", err)
	}

	if _, err := db.Exec(addResultMetadataValueOrdinalColumnSQL); err != nil && !isDuplicateColumnError(err) {
		return fmt.Errorf("add result_metadata value_ordinal column: %w", err)
	}

	return nil
}

func migrateSQLiteResultMetadataSchema(db *sql.DB, hasValueOrdinal bool) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin result_metadata migration: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, statement := range []string{
		`DROP INDEX IF EXISTS idx_result_metadata_meta_key_value`,
		`ALTER TABLE result_metadata RENAME TO result_metadata_old`,
		createResultMetadataTableSQL,
		sqliteResultMetadataMigrationInsertSQL(hasValueOrdinal),
		`DROP TABLE result_metadata_old`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("migrate result_metadata schema: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit result_metadata migration: %w", err)
	}

	committed = true

	return nil
}

func sqliteResultMetadataMigrationInsertSQL(hasValueOrdinal bool) string {
	if hasValueOrdinal {
		return `INSERT INTO result_metadata(result_id, meta_key, value_ordinal, value)
			SELECT result_id, meta_key, value_ordinal, value
			FROM result_metadata_old
			ORDER BY result_id, meta_key, value_ordinal, rowid`
	}

	return `INSERT INTO result_metadata(result_id, meta_key, value_ordinal, value)
		SELECT result_id, meta_key,
		       ROW_NUMBER() OVER (PARTITION BY result_id, meta_key ORDER BY rowid) - 1,
		       value
		FROM result_metadata_old
		ORDER BY rowid`
}

func ensureResultMetadataResultIDIndex(db *sql.DB) error {
	if _, err := db.Exec(createResultMetadataResultIDIndexSQL); err != nil && !isDuplicateIndexError(err) {
		return fmt.Errorf("create result_metadata result_id index: %w", err)
	}

	return nil
}

func isMissingPrimaryKeyError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "1091") ||
		strings.Contains(message, "can't drop") ||
		strings.Contains(message, "check that column/key exists") ||
		strings.Contains(message, "no such index") ||
		strings.Contains(message, "does not exist")
}

func appendSearchSuggestionQueryPart(parts []string, args []any, source searchSuggestionSource, term string) ([]string, []any) {
	parts = append(parts, fmt.Sprintf(
		`SELECT %d AS field_order, 0 AS is_metadata, ? AS field_key, %s AS match_value
		 FROM result_sets
		 WHERE instr(lower(%s), lower(?)) > 0`,
		source.order,
		source.column,
		source.column,
	))
	args = append(args, source.fieldKey, term)

	return parts, args
}

// NewStore enables foreign keys and creates the SQL schema on demand.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: nil db", ErrInvalidInput)
	}

	for _, statement := range []string{
		enableForeignKeysSQL,
		createResultSetsTableSQL,
		createResultFilesTableSQL,
		createResultMetadataTableSQL,
	} {
		if _, err := db.Exec(statement); err != nil {
			if statement == enableForeignKeysSQL && isIgnorablePragmaError(err) {
				continue
			}

			return nil, fmt.Errorf("initialise results store: %w", err)
		}
	}

	if err := ensureResultMetadataSchema(db); err != nil {
		return nil, fmt.Errorf("initialise results store: %w", err)
	}

	if err := ensureResultMetadataMetaKeyValueIndex(db); err != nil {
		return nil, fmt.Errorf("initialise results store: %w", err)
	}

	if err := ensureResultSetsOutputDirectoryGIDColumn(db); err != nil {
		return nil, fmt.Errorf("initialise results store: %w", err)
	}

	return &Store{db: db}, nil
}

func ensureResultMetadataMetaKeyValueIndex(db *sql.DB) error {
	if _, err := db.Exec(createResultMetadataMetaKeyValueIndexSQL); err == nil {
		return nil
	}

	if _, err := db.Exec(createResultMetadataMetaKeyValueIndexMySQLSQL); err != nil && !isDuplicateIndexError(err) {
		return fmt.Errorf("create result metadata meta_key/value index: %w", err)
	}

	return nil
}

func enableForeignKeysOnExecutor(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}) error {
	if _, err := executor.ExecContext(ctx, enableForeignKeysSQL); err != nil && !isIgnorablePragmaError(err) {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	return nil
}

func beginTxWithForeignKeys(ctx context.Context, db *sql.DB) (*sql.Tx, *sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire transaction connection: %w", err)
	}

	if err := enableForeignKeysOnExecutor(ctx, conn); err != nil {
		_ = conn.Close()

		return nil, nil, err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = conn.Close()

		return nil, nil, fmt.Errorf("begin transaction: %w", err)
	}

	return tx, conn, nil
}

func isIgnorablePragmaError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "pragma") && (strings.Contains(message, "syntax") || strings.Contains(message, "1064"))
}

func ensureResultSetsOutputDirectoryGIDColumn(db *sql.DB) error {
	if _, err := db.Exec(addResultSetsOutputDirectoryGIDColumnSQL); err != nil && !isDuplicateColumnError(err) {
		return fmt.Errorf("add result_sets output_directory_gid column: %w", err)
	}

	return nil
}

func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "duplicate column") || strings.Contains(message, "already exists")
}

func normalizedSearchSuggestionQuery(query string) (string, bool) {
	term := strings.TrimSpace(query)

	return term, utf8.RuneCountInString(term) >= minSearchSuggestionQueryLength
}

func upsertResultSetRow(ctx context.Context, tx *sql.Tx, id string, reg *Registration) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	createdAt := now

	var storedCreatedAt string
	err := tx.QueryRowContext(
		ctx,
		`SELECT created_at FROM result_sets WHERE id = ?`,
		id,
	).Scan(&storedCreatedAt)
	if err != nil && err != sql.ErrNoRows {
		return time.Time{}, time.Time{}, fmt.Errorf("query existing result set: %w", err)
	}

	if err == nil {
		createdAt, err = time.Parse(time.RFC3339Nano, storedCreatedAt)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse created_at: %w", err)
		}

		now = ensureUpdatedAtAfterCreatedAt(createdAt, now)

		_, err = updateResultSetRow(ctx, tx, id, reg, now)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("update result set: %w", err)
		}

		return createdAt, now, nil
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO result_sets (
			id, pipeline_identifier, run_key, requester, operator, command,
			pipeline_name, pipeline_version, output_directory, output_directory_gid,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		reg.PipelineIdentifier,
		reg.RunKey,
		reg.Requester,
		reg.Operator,
		reg.Command,
		reg.PipelineName,
		reg.PipelineVersion,
		reg.OutputDirectory,
		nullableInt64Value(reg.OutputDirectoryGID),
		createdAt.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			createdAt, err = existingCreatedAt(ctx, tx, id)
			if err != nil {
				return time.Time{}, time.Time{}, err
			}

			now = ensureUpdatedAtAfterCreatedAt(createdAt, now)

			_, err = updateResultSetRow(ctx, tx, id, reg, now)
			if err != nil {
				return time.Time{}, time.Time{}, fmt.Errorf("update result set after duplicate insert: %w", err)
			}

			return createdAt, now, nil
		}

		return time.Time{}, time.Time{}, fmt.Errorf("insert result set: %w", err)
	}

	return createdAt, now, nil
}

func nullableInt64Value(value *int64) any {
	if value == nil {
		return nil
	}

	return *value
}

func existingCreatedAt(ctx context.Context, tx *sql.Tx, id string) (time.Time, error) {
	var storedCreatedAt string

	err := tx.QueryRowContext(ctx, `SELECT created_at FROM result_sets WHERE id = ?`, id).Scan(&storedCreatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("query existing result set after duplicate insert: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, storedCreatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse created_at after duplicate insert: %w", err)
	}

	return createdAt, nil
}

func updateResultSetRow(ctx context.Context, tx *sql.Tx, id string, reg *Registration, now time.Time) (sql.Result, error) {
	return tx.ExecContext(
		ctx,
		`UPDATE result_sets
		SET requester = ?, operator = ?, command = ?, pipeline_name = ?,
		    pipeline_version = ?, output_directory = ?, output_directory_gid = ?, updated_at = ?
		WHERE id = ?`,
		reg.Requester,
		reg.Operator,
		reg.Command,
		reg.PipelineName,
		reg.PipelineVersion,
		reg.OutputDirectory,
		nullableInt64Value(reg.OutputDirectoryGID),
		now.Format(time.RFC3339Nano),
		id,
	)
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "duplicate") || strings.Contains(message, "unique constraint")
}

func isDuplicateIndexError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "duplicate") || strings.Contains(message, "already exists")
}

func ensureUpdatedAtAfterCreatedAt(createdAt, now time.Time) time.Time {
	if now.After(createdAt) {
		return now
	}

	return createdAt.Add(time.Nanosecond)
}

func normalizedRegistrationMetadataValues(reg *Registration) map[string][]string {
	if reg == nil {
		return map[string][]string{}
	}

	values := cloneMetadataValues(reg.MetadataValues)

	for key, value := range reg.Metadata {
		if existingValues, exists := values[key]; exists && len(existingValues) > 0 {
			continue
		}

		appendMetadataValue(values, key, value)
	}

	return values
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}

	intValue := value.Int64

	return &intValue
}

func responseMetadataValues(metadata map[string][]string) map[string][]string {
	for _, values := range metadata {
		if len(values) > 1 {
			return cloneMetadataValues(metadata)
		}
	}

	return nil
}

func cloneMetadataValues(metadata map[string][]string) map[string][]string {
	if len(metadata) == 0 {
		return map[string][]string{}
	}

	cloned := make(map[string][]string, len(metadata))
	for key, value := range metadata {
		for _, singleValue := range value {
			appendMetadataValue(cloned, key, singleValue)
		}
	}

	return cloned
}

func copyInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}

	copied := *value

	return &copied
}

func replaceResultFiles(ctx context.Context, tx *sql.Tx, resultID string, files []FileEntry) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM result_files WHERE result_id = ?`, resultID); err != nil {
		return fmt.Errorf("delete existing result files: %w", err)
	}

	for _, file := range files {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO result_files (result_id, path, mtime, size, kind) VALUES (?, ?, ?, ?, ?)`,
			resultID,
			file.Path,
			file.Mtime.UTC().Format(time.RFC3339Nano),
			file.Size,
			file.Kind,
		)
		if err != nil {
			return fmt.Errorf("insert result file: %w", err)
		}
	}

	return nil
}

func replaceResultMetadata(ctx context.Context, tx *sql.Tx, resultID string, metadata map[string][]string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM result_metadata WHERE result_id = ?`, resultID); err != nil {
		return fmt.Errorf("delete existing result metadata: %w", err)
	}

	for _, key := range sortedMultiMetadataKeys(metadata) {
		for valueOrdinal, value := range metadata[key] {
			_, err := tx.ExecContext(
				ctx,
				`INSERT INTO result_metadata (result_id, meta_key, value_ordinal, value) VALUES (?, ?, ?, ?)`,
				resultID,
				key,
				valueOrdinal,
				value,
			)
			if err != nil {
				return fmt.Errorf("insert result metadata: %w", err)
			}
		}
	}

	return nil
}

func metadataValuesFromMap(metadata map[string]string) map[string][]string {
	values := make(map[string][]string, len(metadata))

	for key, value := range metadata {
		appendMetadataValue(values, key, value)
	}

	return values
}

func loadDailyStatsCounts(ctx context.Context, conn *sql.Conn, now time.Time, days int) ([]DailyCount, error) {
	daily := zeroFilledDailyCounts(now, days)
	if days <= 0 {
		return daily, nil
	}

	start := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))
	rows, err := conn.QueryContext(
		ctx,
		`SELECT substr(created_at, 1, 10) AS day, COUNT(*)
		 FROM result_sets
		 WHERE created_at >= ?
		 GROUP BY day
		 ORDER BY day`,
		start.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("query stats daily counts: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	countsByDay := make(map[string]int, len(daily))

	for rows.Next() {
		var day string
		var count int

		if err := rows.Scan(&day, &count); err != nil {
			return nil, fmt.Errorf("scan stats daily count: %w", err)
		}

		countsByDay[day] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stats daily counts: %w", err)
	}

	for i := range daily {
		daily[i].Count = countsByDay[daily[i].Date]
	}

	return daily, nil
}

func zeroFilledDailyCounts(now time.Time, days int) []DailyCount {
	if days <= 0 {
		return []DailyCount{}
	}

	daily := make([]DailyCount, days)
	start := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))

	for i := range days {
		daily[i] = DailyCount{Date: start.AddDate(0, 0, i).Format("2006-01-02")}
	}

	return daily
}

func loadRecentStatsResults(ctx context.Context, conn *sql.Conn, recent int) ([]ResultSet, error) {
	if recent <= 0 {
		return []ResultSet{}, nil
	}

	rows, err := conn.QueryContext(
		ctx,
		`SELECT id, pipeline_identifier, run_key, requester, operator, command,
		        pipeline_name, pipeline_version, output_directory, output_directory_gid,
		        created_at, updated_at
		 FROM result_sets
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`,
		recent,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent stats result sets: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	results := make([]ResultSet, 0, recent)
	resultIDs := make([]string, 0, recent)

	for rows.Next() {
		result, err := scanResultSet(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recent stats result set: %w", err)
		}

		results = append(results, result)
		resultIDs = append(resultIDs, result.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent stats result sets: %w", err)
	}

	metadataByID, err := loadResultMetadataByIDs(ctx, conn, resultIDs)
	if err != nil {
		return nil, err
	}

	for i := range results {
		metadata := metadataByID[results[i].ID]
		results[i].Metadata = metadata.single
		results[i].MetadataValues = responseMetadataValues(metadata.values)
	}

	return results, nil
}

func querySearchResults(ctx context.Context, conn *sql.Conn, filters []string, args []any) ([]ResultSet, error) {
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString(`SELECT id, pipeline_identifier, run_key, requester, operator, command, pipeline_name, pipeline_version, output_directory, output_directory_gid, created_at, updated_at FROM result_sets`)

	if len(filters) > 0 {
		queryBuilder.WriteString(" WHERE ")
		queryBuilder.WriteString(strings.Join(filters, " AND "))
	}

	queryBuilder.WriteString(" ORDER BY created_at, id")

	rows, err := conn.QueryContext(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("search result sets: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	results := make([]ResultSet, 0)
	resultIDs := make([]string, 0)

	for rows.Next() {
		result, err := scanResultSet(rows)
		if err != nil {
			return nil, fmt.Errorf("scan result set: %w", err)
		}

		results = append(results, result)
		resultIDs = append(resultIDs, result.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate result set search: %w", err)
	}

	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close result set search rows: %w", err)
	}

	metadataByID, err := loadResultMetadataByIDs(ctx, conn, resultIDs)
	if err != nil {
		return nil, err
	}

	for i := range results {
		metadata := metadataByID[results[i].ID]
		results[i].Metadata = metadata.single
		results[i].MetadataValues = responseMetadataValues(metadata.values)
	}

	return results, nil
}

func scanResultSet(rowScanner interface {
	Scan(dest ...any) error
}) (ResultSet, error) {
	var result ResultSet
	var createdAt string
	var outputDirectoryGID sql.NullInt64
	var updatedAt string

	err := rowScanner.Scan(
		&result.ID,
		&result.PipelineIdentifier,
		&result.RunKey,
		&result.Requester,
		&result.Operator,
		&result.Command,
		&result.PipelineName,
		&result.PipelineVersion,
		&result.OutputDirectory,
		&outputDirectoryGID,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return ResultSet{}, err
	}

	result.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ResultSet{}, fmt.Errorf("parse created_at: %w", err)
	}

	result.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return ResultSet{}, fmt.Errorf("parse updated_at: %w", err)
	}

	result.OutputDirectoryGID = nullableInt64Pointer(outputDirectoryGID)
	result.Metadata = map[string]string{}

	return result, nil
}

func loadPipelineStatsCounts(ctx context.Context, conn *sql.Conn) ([]PipelineCount, error) {
	rows, err := conn.QueryContext(
		ctx,
		`SELECT pipeline_name, COUNT(*)
		 FROM result_sets
		 GROUP BY pipeline_name
		 ORDER BY COUNT(*) DESC, pipeline_name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query stats pipeline counts: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	pipelines := make([]PipelineCount, 0)

	for rows.Next() {
		var pipeline PipelineCount

		if err := rows.Scan(&pipeline.PipelineName, &pipeline.Count); err != nil {
			return nil, fmt.Errorf("scan stats pipeline count: %w", err)
		}

		pipelines = append(pipelines, pipeline)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stats pipeline counts: %w", err)
	}

	return pipelines, nil
}

func loadResultMetadata(ctx context.Context, querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, resultID string) (loadedResultMetadata, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT meta_key, value FROM result_metadata WHERE result_id = ? ORDER BY meta_key, value_ordinal, value`,
		resultID,
	)
	if err != nil {
		return loadedResultMetadata{}, fmt.Errorf("query result metadata: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	values := map[string][]string{}

	for rows.Next() {
		var key string
		var value string

		if err := rows.Scan(&key, &value); err != nil {
			return loadedResultMetadata{}, fmt.Errorf("scan result metadata: %w", err)
		}

		appendMetadataValue(values, key, value)
	}

	if err := rows.Err(); err != nil {
		return loadedResultMetadata{}, fmt.Errorf("iterate result metadata: %w", err)
	}

	return loadedResultMetadata{single: singleMetadataFromValues(values), values: values}, nil
}

func loadResultMetadataByIDs(ctx context.Context, querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, resultIDs []string) (map[string]loadedResultMetadata, error) {
	metadataByID := make(map[string]loadedResultMetadata, len(resultIDs))
	if len(resultIDs) == 0 {
		return metadataByID, nil
	}

	args := make([]any, 0, len(resultIDs))
	placeholders := make([]string, 0, len(resultIDs))

	for _, resultID := range resultIDs {
		metadataByID[resultID] = loadedResultMetadata{single: map[string]string{}, values: map[string][]string{}}
		placeholders = append(placeholders, "?")
		args = append(args, resultID)
	}

	query := fmt.Sprintf(
		`SELECT result_id, meta_key, value
		 FROM result_metadata
		 WHERE result_id IN (%s)
		 ORDER BY result_id, meta_key, value_ordinal, value`,
		strings.Join(placeholders, ", "),
	)

	rows, err := querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query result metadata: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var resultID string
		var key string
		var value string

		if err := rows.Scan(&resultID, &key, &value); err != nil {
			return nil, fmt.Errorf("scan result metadata: %w", err)
		}

		metadata := metadataByID[resultID]
		if metadata.values == nil {
			metadata.values = map[string][]string{}
		}
		appendMetadataValue(metadata.values, key, value)
		metadataByID[resultID] = metadata
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate result metadata: %w", err)
	}

	for resultID, metadata := range metadataByID {
		metadata.single = singleMetadataFromValues(metadata.values)
		metadataByID[resultID] = metadata
	}

	return metadataByID, nil
}

func appendMultiValueSearchFilter(filters []string, args []any, field string, values []string) ([]string, []any) {
	values = nonEmptySearchValues(values)
	if len(values) == 0 {
		return filters, args
	}

	clauses := make([]string, 0, len(values))

	for _, value := range values {
		clauses = append(clauses, fmt.Sprintf("instr(lower(%s), lower(?)) > 0", field))
		args = append(args, value)
	}

	if len(clauses) == 1 {
		return append(filters, clauses[0]), args
	}

	return append(filters, "("+strings.Join(clauses, " OR ")+")"), args
}

func appendMultiMetadataSearchFilters(filters []string, args []any, metadata map[string][]string) ([]string, []any) {
	for _, key := range sortedMultiMetadataKeys(metadata) {
		values := nonEmptySearchValues(metadata[key])
		if len(values) == 0 {
			continue
		}

		filterArgs := []any{key}
		valueClauses := make([]string, 0, len(values))
		for _, value := range values {
			valueClauses = append(valueClauses, "instr(lower(rm.value), lower(?)) > 0")
			filterArgs = append(filterArgs, value)
		}

		filters = append(filters, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM result_metadata rm
			WHERE rm.result_id = result_sets.id AND rm.meta_key = ? AND (%s)
		)`, strings.Join(valueClauses, " OR ")))
		args = append(args, filterArgs...)
	}

	return filters, args
}

func sortedMultiMetadataKeys(metadata map[string][]string) []string {
	keys := make([]string, 0, len(metadata))

	for key := range metadata {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func appendOrMetaSearchFilter(filters []string, args []any, orMeta []map[string][]string) ([]string, []any) {
	if len(orMeta) == 0 {
		return filters, args
	}

	clauses := make([]string, 0, len(orMeta))

	for _, condition := range orMeta {
		for key, values := range condition {
			values = nonEmptySearchValues(values)
			if len(values) == 0 {
				continue
			}

			valueClauses := make([]string, 0, len(values))
			args = append(args, key)
			for _, value := range values {
				valueClauses = append(valueClauses, "instr(lower(rm.value), lower(?)) > 0")
				args = append(args, value)
			}
			clauses = append(clauses, fmt.Sprintf(`EXISTS (SELECT 1 FROM result_metadata rm WHERE rm.result_id = result_sets.id AND rm.meta_key = ? AND (%s))`, strings.Join(valueClauses, " OR ")))
		}
	}

	if len(clauses) == 0 {
		return filters, args
	}

	if len(clauses) == 1 {
		return append(filters, clauses[0]), args
	}

	return append(filters, "("+strings.Join(clauses, " OR ")+")"), args
}

func nonEmptySearchValues(values []string) []string {
	filtered := make([]string, 0, len(values))

	for _, value := range values {
		if value == "" {
			continue
		}

		filtered = append(filtered, value)
	}

	return filtered
}

func expandRunKeySearchValues(values []string) []string {
	expanded := make([]string, 0, len(values)*3)
	seen := map[string]struct{}{}

	add := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}

		seen[trimmed] = struct{}{}
		expanded = append(expanded, trimmed)
	}

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		add(trimmed)
		add(BuildRunKey(trimmed, ""))

		primary, additional, hasDisplaySeparator := strings.Cut(trimmed, " / ")
		if hasDisplaySeparator {
			add(BuildRunKey(strings.TrimSpace(primary), strings.TrimSpace(additional)))

			continue
		}

		if !strings.Contains(trimmed, "=") {
			add(BuildRunKey("", trimmed))
		}
	}

	return expanded
}

func searchSuggestionFieldKey(rawFieldKey string, isMetadata bool) string {
	if !isMetadata {
		for _, source := range searchSuggestionSources {
			if rawFieldKey == source.fieldKey {
				return rawFieldKey
			}
		}
	}

	switch {
	case slices.Contains(combinedStudyMetaKeys, rawFieldKey):
		return "study"
	case slices.Contains(combinedSampleMetaKeys, rawFieldKey):
		return "sample"
	case slices.Contains(libraryTypeMetaKeys, rawFieldKey):
		return "library"
	case slices.Contains(libraryIDMetaKeys, rawFieldKey):
		return SeqmetaLibraryIDKey
	case slices.Contains(libraryLimsMetaKeys, rawFieldKey):
		return SeqmetaIDLibraryLimsKey
	case slices.Contains(combinedRunMetaKeys, rawFieldKey):
		return "run"
	case slices.Contains(combinedLaneMetaKeys, rawFieldKey):
		return rawFieldKey
	case strings.HasPrefix(rawFieldKey, "seqmeta_"):
		return rawFieldKey
	default:
		return "meta_" + rawFieldKey
	}
}

func multiSearchParamsFromSingle(params SearchParams) MultiSearchParams {
	multi := MultiSearchParams{
		Meta: map[string][]string{},
	}

	if params.Requester != "" {
		multi.Requester = []string{params.Requester}
	}

	if params.Operator != "" {
		multi.Operator = []string{params.Operator}
	}

	if params.PipelineName != "" {
		multi.PipelineName = []string{params.PipelineName}
	}

	if params.PipelineVersion != "" {
		multi.PipelineVersion = []string{params.PipelineVersion}
	}

	if params.PipelineIdentifier != "" {
		multi.PipelineIdentifier = []string{params.PipelineIdentifier}
	}

	if params.RunKey != "" {
		multi.RunKey = []string{params.RunKey}
	}

	if params.OutputDirectory != "" {
		multi.OutputDirectory = []string{params.OutputDirectory}
	}

	for key, value := range params.Meta {
		if value == "" {
			continue
		}

		multi.Meta[key] = []string{value}
	}

	return multi
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	err := s.db.Close()
	s.db = nil

	return err
}

// Upsert inserts or updates a result set and replaces its tracked files and metadata.
func (s *Store) Upsert(ctx context.Context, reg *Registration) (*ResultSet, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	if reg == nil {
		return nil, fmt.Errorf("%w: nil registration", ErrInvalidInput)
	}

	if err := ValidateRegistration(reg); err != nil {
		return nil, err
	}

	id := CompositeKeyID(reg.PipelineIdentifier, reg.RunKey)
	tx, conn, err := beginTxWithForeignKeys(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("begin result upsert: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}

		_ = conn.Close()
	}()

	createdAt, updatedAt, err := upsertResultSetRow(ctx, tx, id, reg)
	if err != nil {
		return nil, err
	}

	if err := replaceResultFiles(ctx, tx, id, reg.Files); err != nil {
		return nil, err
	}

	metadataValues := normalizedRegistrationMetadataValues(reg)
	if err := replaceResultMetadata(ctx, tx, id, metadataValues); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit result upsert: %w", err)
	}

	committed = true

	return &ResultSet{
		ID:                 id,
		PipelineIdentifier: reg.PipelineIdentifier,
		RunKey:             reg.RunKey,
		Requester:          reg.Requester,
		Operator:           reg.Operator,
		Command:            reg.Command,
		PipelineName:       reg.PipelineName,
		PipelineVersion:    reg.PipelineVersion,
		OutputDirectory:    reg.OutputDirectory,
		OutputDirectoryGID: copyInt64Pointer(reg.OutputDirectoryGID),
		Metadata:           singleMetadataFromValues(metadataValues),
		MetadataValues:     responseMetadataValues(metadataValues),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}, nil
}

// Search returns all stored result sets matching the supplied filters.
func (s *Store) Search(ctx context.Context, params SearchParams) ([]ResultSet, error) {
	return s.SearchMulti(ctx, multiSearchParamsFromSingle(params))
}

// SearchMulti returns all stored result sets matching multi-value filters.
func (s *Store) SearchMulti(ctx context.Context, params MultiSearchParams) ([]ResultSet, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire search connection: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	filters := make([]string, 0, 7+len(params.Meta))
	args := make([]any, 0)

	filters, args = appendMultiValueSearchFilter(filters, args, "requester", params.Requester)
	filters, args = appendMultiValueSearchFilter(filters, args, "operator", params.Operator)
	filters, args = appendMultiValueSearchFilter(filters, args, "pipeline_name", params.PipelineName)
	filters, args = appendMultiValueSearchFilter(filters, args, "pipeline_version", params.PipelineVersion)
	filters, args = appendMultiValueSearchFilter(filters, args, "pipeline_identifier", params.PipelineIdentifier)
	filters, args = appendMultiValueSearchFilter(filters, args, "run_key", expandRunKeySearchValues(params.RunKey))
	filters, args = appendMultiValueSearchFilter(filters, args, "output_directory", params.OutputDirectory)
	filters, args = appendMultiMetadataSearchFilters(filters, args, params.Meta)
	filters, args = appendOrMetaSearchFilter(filters, args, params.OrMeta)

	return querySearchResults(ctx, conn, filters, args)
}

// SearchSuggestions returns field/value substring matches from registered result data.
func (s *Store) SearchSuggestions(ctx context.Context, query string, limit int) ([]SearchSuggestion, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	term, ok := normalizedSearchSuggestionQuery(query)
	if !ok || limit <= 0 {
		return []SearchSuggestion{}, nil
	}

	queryParts := make([]string, 0, len(searchSuggestionSources)+1)
	args := make([]any, 0, len(searchSuggestionSources)*2+2)
	for _, source := range searchSuggestionSources {
		queryParts, args = appendSearchSuggestionQueryPart(queryParts, args, source, term)
	}

	queryParts = append(queryParts, `SELECT 100 AS field_order, 1 AS is_metadata, meta_key AS field_key, value AS match_value
		FROM result_metadata
		WHERE instr(lower(value), lower(?)) > 0`)
	args = append(args, term, limit*4)

	rows, err := s.db.QueryContext(
		ctx,
		fmt.Sprintf(`SELECT field_key, is_metadata, match_value
			FROM (%s) matches
			WHERE match_value <> ''
			GROUP BY field_key, is_metadata, match_value
			ORDER BY MIN(field_order), lower(match_value)
			LIMIT ?`, strings.Join(queryParts, " UNION ALL ")),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query search suggestions: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	suggestions := []SearchSuggestion{}
	seen := map[SearchSuggestion]struct{}{}
	for rows.Next() {
		var rawFieldKey string
		var isMetadata int
		var value string
		if err := rows.Scan(&rawFieldKey, &isMetadata, &value); err != nil {
			return nil, fmt.Errorf("scan search suggestion: %w", err)
		}

		suggestion := SearchSuggestion{
			FieldKey: searchSuggestionFieldKey(rawFieldKey, isMetadata != 0),
			Value:    value,
		}
		if _, ok := seen[suggestion]; ok {
			continue
		}

		seen[suggestion] = struct{}{}
		suggestions = append(suggestions, suggestion)
		if len(suggestions) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search suggestions: %w", err)
	}

	return suggestions, nil
}

func (s *Store) hasExactMetadataValue(ctx context.Context, keys []string, values []string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	keys = nonEmptySearchValues(keys)
	values = nonEmptySearchValues(values)
	if len(keys) == 0 || len(values) == 0 {
		return false, nil
	}

	keyPlaceholders := strings.TrimSuffix(strings.Repeat("?, ", len(keys)), ", ")
	args := make([]any, 0, len(keys)+len(values))
	for _, key := range keys {
		args = append(args, key)
	}

	valueClauses := make([]string, 0, len(values))
	for _, value := range values {
		valueClauses = append(valueClauses, "lower(value) = lower(?)")
		args = append(args, value)
	}

	var exists int
	err := s.db.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT 1 FROM result_metadata WHERE meta_key IN (%s) AND (%s) LIMIT 1`, keyPlaceholders, strings.Join(valueClauses, " OR ")),
		args...,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query exact metadata value: %w", err)
	}

	return true, nil
}

// DistinctMetadataValues returns sorted distinct metadata values for any of the supplied keys.
func (s *Store) DistinctMetadataValues(ctx context.Context, keys []string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	keys = nonEmptySearchValues(keys)
	if len(keys) == 0 {
		return []string{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(keys)), ", ")
	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}

	rows, err := s.db.QueryContext(
		ctx,
		fmt.Sprintf(`SELECT DISTINCT value FROM result_metadata WHERE meta_key IN (%s)`, placeholders),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query distinct metadata values: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	values := []string{}
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan distinct metadata value: %w", err)
		}

		values = append(values, value)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate distinct metadata values: %w", err)
	}

	sort.Strings(values)

	return values, nil
}

// Stats returns aggregate counts and recent result sets for dashboard loading.
func (s *Store) Stats(ctx context.Context, recent, days int) (*StatsResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	if recent < 0 || days < 0 {
		return nil, fmt.Errorf("%w: stats parameters must be non-negative", ErrInvalidInput)
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire stats connection: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	stats := &StatsResult{}

	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM result_sets`).Scan(&stats.Total); err != nil {
		return nil, fmt.Errorf("query stats total: %w", err)
	}

	stats.Recent, err = loadRecentStatsResults(ctx, conn, recent)
	if err != nil {
		return nil, err
	}

	stats.Daily, err = loadDailyStatsCounts(ctx, conn, time.Now().UTC(), days)
	if err != nil {
		return nil, err
	}

	stats.Pipelines, err = loadPipelineStatsCounts(ctx, conn)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

// MetaKeys returns sorted distinct metadata keys used by stored result sets.
func (s *Store) MetaKeys(ctx context.Context) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT meta_key FROM result_metadata ORDER BY meta_key`)
	if err != nil {
		return nil, fmt.Errorf("query metadata keys: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	keys := make([]string, 0)

	for rows.Next() {
		var key string

		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan metadata key: %w", err)
		}

		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate metadata keys: %w", err)
	}

	return keys, nil
}

// Get retrieves a single result set by ID, including metadata but excluding files.
func (s *Store) Get(ctx context.Context, id string) (*ResultSet, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	var result ResultSet
	var createdAt string
	var outputDirectoryGID sql.NullInt64
	var updatedAt string

	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, pipeline_identifier, run_key, requester, operator, command,
		        pipeline_name, pipeline_version, output_directory, output_directory_gid,
		        created_at, updated_at
		 FROM result_sets WHERE id = ?`,
		id,
	).Scan(
		&result.ID,
		&result.PipelineIdentifier,
		&result.RunKey,
		&result.Requester,
		&result.Operator,
		&result.Command,
		&result.PipelineName,
		&result.PipelineVersion,
		&result.OutputDirectory,
		&outputDirectoryGID,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: result set %q", ErrNotFound, id)
		}

		return nil, fmt.Errorf("query result set: %w", err)
	}

	result.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}

	result.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	result.OutputDirectoryGID = nullableInt64Pointer(outputDirectoryGID)
	metadata, err := loadResultMetadata(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	result.Metadata = metadata.single
	result.MetadataValues = responseMetadataValues(metadata.values)

	return &result, nil
}

// Delete permanently removes a result set and relies on cascading deletes for associated rows.
func (s *Store) Delete(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	tx, conn, err := beginTxWithForeignKeys(ctx, s.db)
	if err != nil {
		return fmt.Errorf("begin delete result set: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}

		_ = conn.Close()
	}()

	result, err := tx.ExecContext(ctx, `DELETE FROM result_sets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete result set: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete result set rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("delete result set %q: %w", id, ErrNotFound)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete result set: %w", err)
	}

	committed = true

	return nil
}

// GetFiles retrieves all tracked files for a result set.
func (s *Store) GetFiles(ctx context.Context, resultID string) ([]FileEntry, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT path, mtime, size, kind
		 FROM result_files
		 WHERE result_id = ?
		 ORDER BY path`,
		resultID,
	)
	if err != nil {
		return nil, fmt.Errorf("query result files: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	files := []FileEntry{}

	for rows.Next() {
		var file FileEntry
		var mtime string

		if err := rows.Scan(&file.Path, &mtime, &file.Size, &file.Kind); err != nil {
			return nil, fmt.Errorf("scan result file: %w", err)
		}

		file.Mtime, err = time.Parse(time.RFC3339Nano, mtime)
		if err != nil {
			return nil, fmt.Errorf("parse file mtime: %w", err)
		}

		files = append(files, file)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate result files: %w", err)
	}

	return files, nil
}

// ReplaceOutputFiles replaces output files for a result set while preserving input and pipeline files.
func (s *Store) ReplaceOutputFiles(ctx context.Context, resultID string, files []FileEntry) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	if err := validateTrackedFiles(files); err != nil {
		return err
	}

	for _, file := range files {
		if file.Kind != "output" {
			return fmt.Errorf("%w: output file kind required", ErrInvalidInput)
		}
	}

	outputDirectory, err := storedOutputDirectory(ctx, s.db, resultID)
	if err != nil {
		return err
	}

	if err := validateOutputFilesWithinDirectory(outputDirectory, files); err != nil {
		return err
	}

	tx, conn, err := beginTxWithForeignKeys(ctx, s.db)
	if err != nil {
		return fmt.Errorf("begin replace output files: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}

		_ = conn.Close()
	}()

	result, err := tx.ExecContext(
		ctx,
		`UPDATE result_sets SET updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano),
		resultID,
	)
	if err != nil {
		return fmt.Errorf("update result set timestamp: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update result set timestamp rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("replace output files for result set %q: %w", resultID, ErrNotFound)
	}

	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM result_files WHERE result_id = ? AND kind = ?`,
		resultID,
		"output",
	); err != nil {
		return fmt.Errorf("delete existing output files: %w", err)
	}

	for _, file := range files {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO result_files (result_id, path, mtime, size, kind) VALUES (?, ?, ?, ?, ?)`,
			resultID,
			file.Path,
			file.Mtime.UTC().Format(time.RFC3339Nano),
			file.Size,
			file.Kind,
		)
		if err != nil {
			return fmt.Errorf("insert replacement output file: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace output files: %w", err)
	}

	committed = true

	return nil
}

func storedOutputDirectory(ctx context.Context, db *sql.DB, resultID string) (string, error) {
	var outputDirectory string

	err := db.QueryRowContext(ctx, `SELECT output_directory FROM result_sets WHERE id = ?`, resultID).Scan(&outputDirectory)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("replace output files for result set %q: %w", resultID, ErrNotFound)
		}

		return "", fmt.Errorf("query result set output directory: %w", err)
	}

	return outputDirectory, nil
}
