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
	"path/filepath"
	"strconv"
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
	CacheSchemaVersion = 2
)

var (
	// ErrPasswordInDSN reports that a MySQL DSN embeds a password directly.
	ErrPasswordInDSN = errors.New("mlwh: password must not appear in DSN")
	// ErrUpstreamImpaired reports that a required upstream cache/database operation failed.
	ErrUpstreamImpaired = errors.New("mlwh: upstream database impaired")

	sqlOpenFunc                    = sql.Open
	cacheMigrationStderr io.Writer = os.Stderr
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
	rowCount, version, err := querySchemaVersion(ctx, db)
	if err != nil {
		if isMissingSchemaVersionError(dialect, err) {
			if err = applySchema(ctx, db, dialect); err != nil {
				return err
			}

			return writeSchemaVersion(ctx, db)
		}

		return fmt.Errorf("mlwh: query schema version: %w", err)
	}

	switch {
	case rowCount == 0:
		if err = applySchema(ctx, db, dialect); err != nil {
			return err
		}

		return writeSchemaVersion(ctx, db)
	case rowCount == 1 && version == CacheSchemaVersion:
		return nil
	default:
		return migrateCacheSchema(ctx, db, dialect, version)
	}
}

func querySchemaVersion(ctx context.Context, db *sql.DB) (int, int, error) {
	var (
		rowCount int
		version  int
	)

	err := db.QueryRowContext(
		ctx,
		"SELECT COUNT(*), COALESCE(MAX(version), 0) FROM schema_version",
	).Scan(&rowCount, &version)

	return rowCount, version, err
}

func isMissingSchemaVersionError(dialect string, err error) bool {
	if err != nil {
		if dialect == "sqlite" {
			return strings.Contains(err.Error(), "no such table: schema_version")
		}

		if dialect == "mysql" {
			var mysqlErr *mysql.MySQLError
			return errors.As(err, &mysqlErr) && mysqlErr.Number == 1146
		}
	}

	return false
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

func migrateCacheSchema(ctx context.Context, db *sql.DB, dialect string, fromVersion int) error {
	if err := dropCacheTables(ctx, db, dialect, cacheMigrationDropTables); err != nil {
		return err
	}

	if err := applySchema(ctx, db, dialect); err != nil {
		return err
	}

	if err := ensureSyncStateSchema(ctx, db, dialect); err != nil {
		return err
	}

	if err := deleteSyncStateRows(ctx, db, cacheMigrationSyncStateTables); err != nil {
		return err
	}

	if err := writeSchemaVersion(ctx, db); err != nil {
		return err
	}

	return logCacheMigration(fromVersion, CacheSchemaVersion)
}

func dropCacheTables(ctx context.Context, db *sql.DB, dialect string, tables []string) error {
	for idx := len(tables) - 1; idx >= 0; idx-- {
		stmt := fmt.Sprintf("DROP TABLE IF EXISTS %s", tables[idx])
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("mlwh: drop %s cache table %s: %w", dialect, tables[idx], err)
		}
	}

	return nil
}

func ensureSyncStateSchema(ctx context.Context, db *sql.DB, dialect string) error {
	rows, err := db.QueryContext(ctx, `SELECT * FROM sync_state LIMIT 0`)
	if err != nil {
		return fmt.Errorf("mlwh: query sync_state columns: %w", err)
	}

	columns, err := rows.Columns()
	closeErr := rows.Close()
	if err != nil {
		return fmt.Errorf("mlwh: read sync_state columns: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("mlwh: close sync_state columns: %w", closeErr)
	}

	available := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		available[column] = struct{}{}
	}

	missing := make([]string, 0, 2)
	if _, ok := available["resume_cursor"]; !ok {
		missing = append(missing, syncStateResumeCursorStatement(dialect))
	}
	if _, ok := available["indexes_dropped"]; !ok {
		missing = append(missing, syncStateIndexesDroppedStatement(dialect))
	}

	for _, stmt := range missing {
		if _, err = db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("mlwh: update sync_state schema: %w", err)
		}
	}

	return nil
}

func syncStateResumeCursorStatement(dialect string) string {
	if dialect == "mysql" {
		return `ALTER TABLE sync_state ADD COLUMN resume_cursor TEXT NULL`
	}

	return `ALTER TABLE sync_state ADD COLUMN resume_cursor TEXT`
}

func syncStateIndexesDroppedStatement(dialect string) string {
	if dialect == "mysql" {
		return `ALTER TABLE sync_state ADD COLUMN indexes_dropped INT NOT NULL DEFAULT 0`
	}

	return `ALTER TABLE sync_state ADD COLUMN indexes_dropped INTEGER NOT NULL DEFAULT 0`
}

func deleteSyncStateRows(ctx context.Context, db *sql.DB, tables []string) error {
	if len(tables) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(tables)), ", ")
	args := make([]any, len(tables))
	for index, table := range tables {
		args[index] = table
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM sync_state WHERE table_name IN (`+placeholders+`)`, args...); err != nil {
		return fmt.Errorf("mlwh: clear sync_state rows for recreated tables: %w", err)
	}

	return nil
}

func logCacheMigration(fromVersion, toVersion int) error {
	tables := append([]string(nil), cacheMigrationRecreateTables...)
	sort.Strings(tables)

	if _, err := fmt.Fprintf(
		cacheMigrationStderr,
		"mlwh cache: schema v%d->v%d, recreated tables: [%s]\n",
		fromVersion,
		toVersion,
		strings.Join(tables, ", "),
	); err != nil {
		return fmt.Errorf("mlwh: write cache migration log: %w", err)
	}

	return nil
}

func applySchema(ctx context.Context, db *sql.DB, dialect string) error {
	stmts, err := schemaStatementsForDialect(ctx, db, dialect)
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

func schemaStatementsForDialect(ctx context.Context, db *sql.DB, dialect string) ([]string, error) {
	if dialect != "mysql" {
		return loadSchema(dialect)
	}

	collation, err := mySQLSchemaTextCollation(ctx, db)
	if err != nil {
		return nil, err
	}

	return loadSchemaWithMySQLCollation(dialect, collation)
}

func mySQLSchemaTextCollation(ctx context.Context, db *sql.DB) (string, error) {
	version, err := mySQLServerVersion(ctx, db)
	if err != nil {
		return "", fmt.Errorf("mlwh: query mysql schema collation: %w", err)
	}

	if strings.Contains(strings.ToLower(version), "mariadb") {
		return mySQLLegacyTextCollation, nil
	}

	major, err := mySQLMajorVersion(version)
	if err != nil {
		return "", fmt.Errorf("mlwh: parse mysql version %q: %w", version, err)
	}

	if major < 8 {
		return mySQLLegacyTextCollation, nil
	}

	return mySQLPreferredTextCollation, nil
}

func mySQLServerVersion(ctx context.Context, db *sql.DB) (string, error) {
	var version string

	err := db.QueryRowContext(ctx, `SELECT VERSION()`).Scan(&version)

	return version, err
}

func mySQLMajorVersion(version string) (int, error) {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return 0, errors.New("empty version")
	}

	majorField, _, _ := strings.Cut(trimmed, ".")
	majorField = strings.TrimLeft(majorField, "vV")
	if majorField == "" {
		return 0, errors.New("missing major version")
	}

	major, err := strconv.Atoi(majorField)
	if err != nil {
		return 0, err
	}

	return major, nil
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
