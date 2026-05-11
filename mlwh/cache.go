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
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

const (
	defaultMySQLLockTimeoutSeconds = 30
	mysqlSyncLockName              = "wa_mlwh_sync"

	// CacheSchemaVersion is the embedded cache schema version supported by OpenCache.
	CacheSchemaVersion = 1
)

var (
	// ErrPasswordInDSN reports that a MySQL DSN embeds a password directly.
	ErrPasswordInDSN = errors.New("mlwh: password must not appear in DSN")
	// ErrUpstreamImpaired reports that a required upstream cache/database operation failed.
	ErrUpstreamImpaired = errors.New("mlwh: upstream database impaired")

	sqlOpenFunc = sql.Open
)

// Cache exposes the opened cache database handle.
type Cache interface {
	DB() *sql.DB
	Dialect() string
	Close() error
}

// OpenCache opens the configured cache backend and ensures the embedded schema exists.
func OpenCache(ctx context.Context, cfg CacheConfig) (Cache, error) {
	if looksLikeMySQLDSN(cfg.Path) {
		return openMySQLCache(ctx, cfg)
	}

	return openSQLiteCache(ctx, cfg)
}

func openSQLiteCache(ctx context.Context, cfg CacheConfig) (Cache, error) {
	rwDSN := sqliteWritableDSN(cfg.Path)
	rwDB, err := sqlOpenFunc("sqlite", rwDSN)
	if err != nil {
		return nil, fmt.Errorf("mlwh: open sqlite cache: %w", err)
	}

	if err = rwDB.PingContext(ctx); err != nil {
		_ = rwDB.Close()

		return nil, fmt.Errorf("mlwh: ping sqlite cache: %w", err)
	}

	if err = prepareCacheSchema(ctx, rwDB, "sqlite"); err != nil {
		_ = rwDB.Close()

		return nil, err
	}

	roDB := rwDB
	if cfg.Path != ":memory:" {
		roDB, err = sqlOpenFunc("sqlite", sqliteReadOnlyDSN(cfg.Path))
		if err != nil {
			_ = rwDB.Close()

			return nil, fmt.Errorf("mlwh: open sqlite read-only cache: %w", err)
		}

		if err = roDB.PingContext(ctx); err != nil {
			_ = roDB.Close()
			_ = rwDB.Close()

			return nil, fmt.Errorf("mlwh: ping sqlite read-only cache: %w", err)
		}
	}

	return &sqliteCache{rwDB: rwDB, roDB: roDB}, nil
}

func openMySQLCache(ctx context.Context, cfg CacheConfig) (Cache, error) {
	resolvedDSN, err := resolveMySQLDSN(cfg)
	if err != nil {
		return nil, err
	}

	rwDB, err := sqlOpenFunc("mysql", resolvedDSN)
	if err != nil {
		return nil, fmt.Errorf("mlwh: open mysql cache: %w", err)
	}

	if err = rwDB.PingContext(ctx); err != nil {
		_ = rwDB.Close()

		return nil, fmt.Errorf("mlwh: ping mysql cache: %w", err)
	}

	if err = prepareCacheSchema(ctx, rwDB, "mysql"); err != nil {
		_ = rwDB.Close()

		return nil, err
	}

	roDSN, err := mysqlReadOnlyDSN(resolvedDSN)
	if err != nil {
		_ = rwDB.Close()

		return nil, err
	}

	roDB, err := sqlOpenFunc("mysql", roDSN)
	if err != nil {
		_ = rwDB.Close()

		return nil, fmt.Errorf("mlwh: open mysql read-only cache: %w", err)
	}

	if err = roDB.PingContext(ctx); err != nil {
		_ = roDB.Close()
		_ = rwDB.Close()

		return nil, fmt.Errorf("mlwh: ping mysql read-only cache: %w", err)
	}

	return &mysqlCache{rwDB: rwDB, roDB: roDB}, nil
}

// CacheConfig describes the cache connection to open.
type CacheConfig struct {
	Path     string
	Password string
}

func resolveMySQLDSN(cfg CacheConfig) (string, error) {
	parsed, err := mysql.ParseDSN(cfg.Path)
	if err != nil {
		return "", fmt.Errorf("mlwh: parse mysql cache dsn: %w", err)
	}

	if parsed.Passwd != "" {
		return "", ErrPasswordInDSN
	}

	if cfg.Password != "" {
		parsed.Passwd = cfg.Password
	}

	return parsed.FormatDSN(), nil
}

// Client owns the cache connections used by sync and read paths.
type Client struct {
	cache                   Cache
	cacheReader             *sql.DB
	syncSource              Querier
	sourceDB                *sql.DB
	syncMu                  *sync.Mutex
	expandCacheMu           *sync.RWMutex
	expandCache             map[expandIdentifierCacheKey]expandIdentifierCacheEntry
	now                     func() time.Time
	syncRunner              func(context.Context, *sql.Tx, []string) error
	mySQLLockTimeoutSeconds int
}

// ReadDB exposes the client's read-only cache handle.
func (c *Client) ReadDB() *sql.DB {
	if c == nil {
		return nil
	}

	return c.cacheReader
}

func (c *Client) acquireSyncLock(ctx context.Context) (func() error, error) {
	if c.cache.Dialect() != "mysql" {
		return nil, nil
	}

	timeout := c.mySQLLockTimeoutSeconds
	if timeout <= 0 {
		timeout = defaultMySQLLockTimeoutSeconds
	}

	var gotLock int
	err := c.cache.DB().QueryRowContext(
		ctx,
		"SELECT GET_LOCK('wa_mlwh_sync', ?)",
		timeout,
	).Scan(&gotLock)
	if err != nil {
		return nil, fmt.Errorf("%w: acquire mysql cache lock: %w", ErrUpstreamImpaired, err)
	}
	if gotLock != 1 {
		return nil, fmt.Errorf("%w: acquire mysql cache lock", ErrUpstreamImpaired)
	}

	return func() error {
		var released int

		err := c.cache.DB().QueryRowContext(
			ctx,
			"SELECT RELEASE_LOCK('wa_mlwh_sync')",
		).Scan(&released)
		if err != nil {
			return fmt.Errorf("%w: release mysql cache lock: %w", ErrUpstreamImpaired, err)
		}
		if released != 1 {
			return fmt.Errorf("%w: release mysql cache lock", ErrUpstreamImpaired)
		}

		return nil
	}, nil
}

type sqliteCache struct {
	rwDB *sql.DB
	roDB *sql.DB
}

func (c *sqliteCache) DB() *sql.DB {
	return c.rwDB
}

func (c *sqliteCache) ReadDB() *sql.DB {
	return c.roDB
}

func (c *sqliteCache) Dialect() string {
	return "sqlite"
}

func (c *sqliteCache) Close() error {
	if c.roDB != nil && c.roDB != c.rwDB {
		if err := c.roDB.Close(); err != nil {
			_ = c.rwDB.Close()

			return err
		}
	}

	if c.rwDB != nil {
		return c.rwDB.Close()
	}

	return nil
}

type mysqlCache struct {
	rwDB *sql.DB
	roDB *sql.DB
}

func (c *mysqlCache) DB() *sql.DB {
	return c.rwDB
}

func (c *mysqlCache) ReadDB() *sql.DB {
	return c.roDB
}

func (c *mysqlCache) Dialect() string {
	return "mysql"
}

func (c *mysqlCache) Close() error {
	if c.roDB != nil && c.roDB != c.rwDB {
		if err := c.roDB.Close(); err != nil {
			_ = c.rwDB.Close()

			return err
		}
	}

	if c.rwDB != nil {
		return c.rwDB.Close()
	}

	return nil
}

func mysqlReadOnlyDSN(dsn string) (string, error) {
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("mlwh: parse mysql read-only cache dsn: %w", err)
	}

	if parsed.Params == nil {
		parsed.Params = map[string]string{}
	}

	parsed.Params["transaction_read_only"] = "1"

	return parsed.FormatDSN(), nil
}

func applySQLiteSchema(ctx context.Context, db *sql.DB) error {
	return applySchema(ctx, db, "sqlite")
}

func prepareCacheSchema(ctx context.Context, db *sql.DB, dialect string) error {
	needsReset, err := cacheSchemaNeedsReset(ctx, db, dialect)
	if err != nil {
		return err
	}
	if needsReset {
		if err = resetSchema(ctx, db, dialect); err != nil {
			return err
		}
	}

	if err = ensureSchemaVersion(ctx, db, dialect); err != nil {
		return err
	}

	needsReset, err = cacheSchemaNeedsReset(ctx, db, dialect)
	if err != nil {
		return err
	}
	if !needsReset {
		return nil
	}

	if err = resetSchema(ctx, db, dialect); err != nil {
		return err
	}

	return writeSchemaVersion(ctx, db)
}

func cacheSchemaNeedsReset(ctx context.Context, db *sql.DB, dialect string) (bool, error) {
	expectedStatements, err := loadSchema(dialect)
	if err != nil {
		return false, err
	}

	expectedShape, err := parseSchemaShape(expectedStatements)
	if err != nil {
		return false, err
	}

	actualShape, err := inspectCacheSchema(ctx, db, dialect, expectedShape)
	if err != nil {
		return false, err
	}

	return !schemaShapeMatches(expectedShape, actualShape), nil
}

func inspectCacheSchema(ctx context.Context, db *sql.DB, dialect string, expected schemaShape) (schemaShape, error) {
	shape := schemaShape{
		Tables: make(map[string]map[string]string, len(expected.Tables)),
		Index:  make(map[string][]string, len(expected.Index)),
	}

	for _, table := range schemaStatementOrder {
		columns, err := inspectCacheTableColumns(ctx, db, dialect, table)
		if err != nil {
			return schemaShape{}, err
		}
		shape.Tables[table] = columns

		indexes, err := inspectCacheTableIndexes(ctx, db, dialect, table)
		if err != nil {
			return schemaShape{}, err
		}
		shape.Index[table] = indexes
	}

	return shape, nil
}

func inspectCacheTableColumns(ctx context.Context, db *sql.DB, dialect, table string) (map[string]string, error) {
	if dialect == "sqlite" {
		return inspectSQLiteTableColumns(ctx, db, table)
	}

	return inspectMySQLTableColumns(ctx, db, table)
}

func inspectCacheTableIndexes(ctx context.Context, db *sql.DB, dialect, table string) ([]string, error) {
	if dialect == "sqlite" {
		return inspectSQLiteTableIndexes(ctx, db, table)
	}

	return inspectMySQLTableIndexes(ctx, db, table)
}

func inspectSQLiteTableColumns(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("mlwh: inspect sqlite table %s columns: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string]string)
	for rows.Next() {
		var (
			cid        int
			name       string
			typeName   string
			notNull    int
			defaultVal any
			pk         int
		)
		if err = rows.Scan(&cid, &name, &typeName, &notNull, &defaultVal, &pk); err != nil {
			return nil, fmt.Errorf("mlwh: scan sqlite table %s columns: %w", table, err)
		}
		columns[name] = normaliseTypeFamily(typeName)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read sqlite table %s columns: %w", table, err)
	}

	return columns, nil
}

func inspectSQLiteTableIndexes(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_list(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("mlwh: inspect sqlite table %s indexes: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	var indexes []string
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err = rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, fmt.Errorf("mlwh: scan sqlite table %s indexes: %w", table, err)
		}
		if origin != "c" {
			continue
		}

		columns, indexErr := inspectSQLiteIndexColumns(ctx, db, name)
		if indexErr != nil {
			return nil, indexErr
		}
		indexes = append(indexes, strings.Join(columns, ","))
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read sqlite table %s indexes: %w", table, err)
	}

	sort.Strings(indexes)

	return indexes, nil
}

func inspectSQLiteIndexColumns(ctx context.Context, db *sql.DB, indexName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_info(%s)", indexName))
	if err != nil {
		return nil, fmt.Errorf("mlwh: inspect sqlite index %s columns: %w", indexName, err)
	}
	defer func() { _ = rows.Close() }()

	var columns []string
	for rows.Next() {
		var seqno, cid int
		var name string
		if err = rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, fmt.Errorf("mlwh: scan sqlite index %s columns: %w", indexName, err)
		}
		columns = append(columns, name)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read sqlite index %s columns: %w", indexName, err)
	}

	return columns, nil
}

func inspectMySQLTableColumns(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT COLUMN_NAME, DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table)
	if err != nil {
		return nil, fmt.Errorf("mlwh: inspect mysql table %s columns: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string]string)
	for rows.Next() {
		var name, typeName string
		if err = rows.Scan(&name, &typeName); err != nil {
			return nil, fmt.Errorf("mlwh: scan mysql table %s columns: %w", table, err)
		}
		columns[name] = normaliseTypeFamily(typeName)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read mysql table %s columns: %w", table, err)
	}

	return columns, nil
}

func inspectMySQLTableIndexes(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT INDEX_NAME, COLUMN_NAME FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX`, table)
	if err != nil {
		return nil, fmt.Errorf("mlwh: inspect mysql table %s indexes: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	grouped := make(map[string][]string)
	for rows.Next() {
		var indexName, columnName string
		if err = rows.Scan(&indexName, &columnName); err != nil {
			return nil, fmt.Errorf("mlwh: scan mysql table %s indexes: %w", table, err)
		}
		if indexName == "PRIMARY" {
			continue
		}
		grouped[indexName] = append(grouped[indexName], columnName)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read mysql table %s indexes: %w", table, err)
	}

	indexes := make([]string, 0, len(grouped))
	for _, columns := range grouped {
		indexes = append(indexes, strings.Join(columns, ","))
	}
	sort.Strings(indexes)

	return indexes, nil
}

func schemaShapeMatches(expected, actual schemaShape) bool {
	for table, expectedColumns := range expected.Tables {
		actualColumns, ok := actual.Tables[table]
		if !ok || len(actualColumns) != len(expectedColumns) {
			return false
		}
		for column, expectedType := range expectedColumns {
			if actualColumns[column] != expectedType {
				return false
			}
		}

		expectedIndexes := append([]string(nil), expected.Index[table]...)
		actualIndexes := append([]string(nil), actual.Index[table]...)
		sort.Strings(expectedIndexes)
		sort.Strings(actualIndexes)
		if len(expectedIndexes) != len(actualIndexes) {
			return false
		}
		for idx := range expectedIndexes {
			if expectedIndexes[idx] != actualIndexes[idx] {
				return false
			}
		}
	}

	return true
}

func ensureSchemaVersion(ctx context.Context, db *sql.DB, dialect string) error {
	var (
		rowCount int
		version  int
	)

	err := db.QueryRowContext(
		ctx,
		"SELECT COUNT(*), COALESCE(MAX(version), 0) FROM schema_version",
	).Scan(&rowCount, &version)
	if err != nil {
		return fmt.Errorf("mlwh: query schema version: %w", err)
	}

	switch {
	case rowCount == 1 && version == CacheSchemaVersion:
		return nil
	case rowCount == 0:
		return writeSchemaVersion(ctx, db)
	default:
		if err = resetSchema(ctx, db, dialect); err != nil {
			return err
		}

		return writeSchemaVersion(ctx, db)
	}
}

func writeSchemaVersion(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "DELETE FROM schema_version"); err != nil {
		return fmt.Errorf("mlwh: clear schema version: %w", err)
	}

	if _, err := db.ExecContext(
		ctx,
		"INSERT INTO schema_version(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)",
		CacheSchemaVersion,
	); err != nil {
		return fmt.Errorf("mlwh: write schema version: %w", err)
	}

	return nil
}

func resetSchema(ctx context.Context, db *sql.DB, dialect string) error {
	for idx := len(schemaStatementOrder) - 1; idx >= 0; idx-- {
		stmt := fmt.Sprintf("DROP TABLE IF EXISTS %s", schemaStatementOrder[idx])
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("mlwh: drop %s cache table %s: %w", dialect, schemaStatementOrder[idx], err)
		}
	}

	if err := applySchema(ctx, db, dialect); err != nil {
		return err
	}

	return nil
}

func applySchema(ctx context.Context, db *sql.DB, dialect string) error {
	stmts, err := loadSchema(dialect)
	if err != nil {
		return err
	}

	for _, group := range stmts {
		for _, stmt := range splitSQLStatements(group) {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("mlwh: apply %s schema: %w", dialect, err)
			}
		}
	}

	return nil
}

func looksLikeMySQLDSN(path string) bool {
	if path == "" || path == ":memory:" {
		return false
	}

	if filepath.IsAbs(path) || strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") || strings.HasPrefix(path, "file:") {
		return false
	}

	parsed, err := mysql.ParseDSN(path)
	if err != nil {
		return false
	}

	return parsed.DBName != "" && (parsed.User != "" || parsed.Net != "" || parsed.Addr != "" || strings.Contains(path, "@"))
}

func sqliteWritableDSN(path string) string {
	if path == ":memory:" {
		return path
	}

	return fmt.Sprintf("file:%s?mode=rwc&_pragma=journal_mode(WAL)", filepath.ToSlash(path))
}

func sqliteReadOnlyDSN(path string) string {
	if path == ":memory:" {
		return path
	}

	return fmt.Sprintf("file:%s?mode=ro", filepath.ToSlash(path))
}
