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
	"fmt"
	"sort"
	"strings"
	"time"
)

const enableForeignKeysSQL = `PRAGMA foreign_keys = ON`

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
	created_at          VARCHAR(30)  NOT NULL,
	updated_at          VARCHAR(30)  NOT NULL
);`

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
	result_id VARCHAR(64)  NOT NULL,
	meta_key  VARCHAR(255) NOT NULL,
	value     TEXT         NOT NULL,
	PRIMARY KEY (result_id, meta_key),
	FOREIGN KEY (result_id)
		REFERENCES result_sets(id) ON DELETE CASCADE
);`

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

	return &Store{db: db}, nil
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
			pipeline_name, pipeline_version, output_directory, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		reg.PipelineIdentifier,
		reg.RunKey,
		reg.Requester,
		reg.Operator,
		reg.Command,
		reg.PipelineName,
		reg.PipelineVersion,
		reg.OutputDirectory,
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
		    pipeline_version = ?, output_directory = ?, updated_at = ?
		WHERE id = ?`,
		reg.Requester,
		reg.Operator,
		reg.Command,
		reg.PipelineName,
		reg.PipelineVersion,
		reg.OutputDirectory,
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

func ensureUpdatedAtAfterCreatedAt(createdAt, now time.Time) time.Time {
	if now.After(createdAt) {
		return now
	}

	return createdAt.Add(time.Nanosecond)
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

func replaceResultMetadata(ctx context.Context, tx *sql.Tx, resultID string, metadata map[string]string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM result_metadata WHERE result_id = ?`, resultID); err != nil {
		return fmt.Errorf("delete existing result metadata: %w", err)
	}

	for key, value := range metadata {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO result_metadata (result_id, meta_key, value) VALUES (?, ?, ?)`,
			resultID,
			key,
			value,
		)
		if err != nil {
			return fmt.Errorf("insert result metadata: %w", err)
		}
	}

	return nil
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return map[string]string{}
	}

	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}

	return cloned
}

func scanResultSet(rowScanner interface {
	Scan(dest ...any) error
}) (ResultSet, error) {
	var result ResultSet
	var createdAt string
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

	result.Metadata = map[string]string{}

	return result, nil
}

func loadResultMetadata(ctx context.Context, querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, resultID string) (map[string]string, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT meta_key, value FROM result_metadata WHERE result_id = ? ORDER BY meta_key`,
		resultID,
	)
	if err != nil {
		return nil, fmt.Errorf("query result metadata: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	metadata := map[string]string{}

	for rows.Next() {
		var key string
		var value string

		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan result metadata: %w", err)
		}

		metadata[key] = value
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate result metadata: %w", err)
	}

	return metadata, nil
}

func loadResultMetadataByIDs(ctx context.Context, querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, resultIDs []string) (map[string]map[string]string, error) {
	metadataByID := make(map[string]map[string]string, len(resultIDs))
	if len(resultIDs) == 0 {
		return metadataByID, nil
	}

	args := make([]any, 0, len(resultIDs))
	placeholders := make([]string, 0, len(resultIDs))

	for _, resultID := range resultIDs {
		metadataByID[resultID] = map[string]string{}
		placeholders = append(placeholders, "?")
		args = append(args, resultID)
	}

	query := fmt.Sprintf(
		`SELECT result_id, meta_key, value
		 FROM result_metadata
		 WHERE result_id IN (%s)
		 ORDER BY result_id, meta_key`,
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

		metadataByID[resultID][key] = value
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate result metadata: %w", err)
	}

	return metadataByID, nil
}

func appendSearchFilter(filters []string, args []any, clause string, value string) ([]string, []any) {
	if value == "" {
		return filters, args
	}

	return append(filters, clause), append(args, value)
}

func sortedMetadataKeys(metadata map[string]string) []string {
	keys := make([]string, 0, len(metadata))

	for key := range metadata {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
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

	if err := replaceResultMetadata(ctx, tx, id, reg.Metadata); err != nil {
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
		Metadata:           cloneMetadata(reg.Metadata),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}, nil
}

// Search returns all stored result sets matching the supplied filters.
func (s *Store) Search(ctx context.Context, params SearchParams) ([]ResultSet, error) {
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
	args := make([]any, 0, 7+(2*len(params.Meta)))

	filters, args = appendSearchFilter(filters, args, "requester = ?", params.Requester)
	filters, args = appendSearchFilter(filters, args, "operator = ?", params.Operator)
	filters, args = appendSearchFilter(filters, args, "pipeline_name = ?", params.PipelineName)
	filters, args = appendSearchFilter(filters, args, "pipeline_version = ?", params.PipelineVersion)
	filters, args = appendSearchFilter(filters, args, "pipeline_identifier = ?", params.PipelineIdentifier)
	filters, args = appendSearchFilter(filters, args, "run_key = ?", params.RunKey)

	if params.OutputDirPrefix != "" {
		filters = append(filters, "substr(output_directory, 1, length(?)) = ?")
		args = append(args, params.OutputDirPrefix, params.OutputDirPrefix)
	}

	for _, key := range sortedMetadataKeys(params.Meta) {
		filters = append(filters, `EXISTS (
			SELECT 1 FROM result_metadata rm
			WHERE rm.result_id = result_sets.id AND rm.meta_key = ? AND rm.value = ?
		)`)
		args = append(args, key, params.Meta[key])
	}

	queryBuilder := strings.Builder{}
	queryBuilder.WriteString(`SELECT id, pipeline_identifier, run_key, requester, operator, command, pipeline_name, pipeline_version, output_directory, created_at, updated_at FROM result_sets`)

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
		results[i].Metadata = metadataByID[results[i].ID]
	}

	return results, nil
}

// Get retrieves a single result set by ID, including metadata but excluding files.
func (s *Store) Get(ctx context.Context, id string) (*ResultSet, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidInput)
	}

	var result ResultSet
	var createdAt string
	var updatedAt string

	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, pipeline_identifier, run_key, requester, operator, command,
		        pipeline_name, pipeline_version, output_directory, created_at,
		        updated_at
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

	result.Metadata, err = loadResultMetadata(ctx, s.db, id)
	if err != nil {
		return nil, err
	}

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
