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
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

const (
	defaultMySQLLockTimeoutSeconds = 30
	mysqlSyncLockNamePrefix        = "wa_mlwh_sync_"

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
	rwDB.SetMaxOpenConns(1)
	rwDB.SetMaxIdleConns(1)

	if err = rwDB.PingContext(ctx); err != nil {
		_ = rwDB.Close()

		return nil, fmt.Errorf("mlwh: ping sqlite cache: %w", err)
	}

	if err = prepareCacheSchema(ctx, rwDB, "sqlite"); err != nil {
		_ = rwDB.Close()

		return nil, err
	}
	if err = repairDroppedMirrorIndexes(ctx, rwDB, "sqlite"); err != nil {
		_ = rwDB.Close()

		return nil, err
	}

	lockDSN := sqliteLockDSN(cfg.Path)

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

	return &sqliteCache{rwDB: rwDB, roDB: roDB, lockDSN: lockDSN, writeMu: &sync.Mutex{}}, nil
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
	if err = repairDroppedMirrorIndexes(ctx, rwDB, "mysql"); err != nil {
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

	return &mysqlCache{rwDB: rwDB, roDB: roDB, lockDSN: resolvedDSN}, nil
}

// CacheConfig describes the cache connection to open.
type CacheConfig struct {
	Path     string
	Password string
}

func resolveMySQLDSN(cfg CacheConfig) (string, error) {
	parsed, err := mysql.ParseDSN(normalizeMySQLDSNInput(cfg.Path))
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
	cache            Cache
	cacheReader      *sql.DB
	syncSource       Querier
	sourceDB         *sql.DB
	syncMu           *sync.Mutex
	expandCacheMu    *sync.RWMutex
	expandCache      map[expandIdentifierCacheKey]expandIdentifierCacheEntry
	now              func() time.Time
	syncRunner       func(context.Context, *sql.Tx, []string) error
	syncReportWriter io.Writer
	syncRetryWriter  io.Writer
	syncRetrySleep   func(context.Context, time.Duration) error
	disableSyncLock  bool
}

// ReadDB exposes the client's read-only cache handle.
func (c *Client) ReadDB() *sql.DB {
	if c == nil {
		return nil
	}

	return c.cacheReader
}

// SetSyncReportWriter configures an optional writer for per-table sync lines.
func (c *Client) SetSyncReportWriter(writer io.Writer) {
	if c == nil {
		return
	}

	c.syncReportWriter = writer
}

func (c *Client) acquireSyncLock(ctx context.Context) (func() error, error) {
	if c == nil || c.cache == nil {
		return nil, fmt.Errorf("mlwh: cache client not configured")
	}
	if c.disableSyncLock {
		return nil, nil
	}

	switch cache := c.cache.(type) {
	case *sqliteCache:
		return cache.acquireSyncLock(ctx)
	case *mysqlCache:
		return cache.acquireSyncLock(ctx)
	default:
		return nil, nil
	}
}

type sqliteCache struct {
	rwDB    *sql.DB
	roDB    *sql.DB
	lockDSN string
	writeMu *sync.Mutex
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

func (c *sqliteCache) acquireSyncLock(ctx context.Context) (func() error, error) {
	if c == nil || c.lockDSN == "" {
		return nil, nil
	}

	lockDB, err := sqlOpenFunc("sqlite", c.lockDSN)
	if err != nil {
		return nil, fmt.Errorf("mlwh: open sqlite sync lock connection: %w", err)
	}
	lockDB.SetMaxOpenConns(1)
	lockDB.SetMaxIdleConns(1)
	if err = ensureSQLiteSyncLockSchema(ctx, lockDB); err != nil {
		_ = lockDB.Close()
		if isSQLiteSyncLockBusy(err) {
			return nil, ErrSyncAlreadyRunning
		}

		return nil, err
	}

	lockConn, err := lockDB.Conn(ctx)
	if err != nil {
		_ = lockDB.Close()

		return nil, fmt.Errorf("mlwh: acquire sqlite sync lock connection: %w", err)
	}

	if _, err = lockConn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		_ = lockConn.Close()
		_ = lockDB.Close()
		if isSQLiteSyncLockBusy(err) {
			return nil, ErrSyncAlreadyRunning
		}

		return nil, fmt.Errorf("mlwh: acquire sqlite sync lock: %w", err)
	}

	if err = ensureSQLiteSyncLockRow(ctx, lockConn); err != nil {
		_, _ = lockConn.ExecContext(context.Background(), `ROLLBACK`)
		_ = lockConn.Close()
		_ = lockDB.Close()

		return nil, err
	}

	return func() error {
		_, rollbackErr := lockConn.ExecContext(context.Background(), `ROLLBACK`)
		closeConnErr := lockConn.Close()
		closeDBErr := lockDB.Close()
		if rollbackErr != nil {
			return fmt.Errorf("mlwh: release sqlite sync lock: %w", rollbackErr)
		}
		if closeConnErr != nil {
			return fmt.Errorf("mlwh: close sqlite sync lock connection: %w", closeConnErr)
		}
		if closeDBErr != nil {
			return fmt.Errorf("mlwh: close sqlite sync lock database: %w", closeDBErr)
		}

		return nil
	}, nil
}

type mysqlCache struct {
	rwDB    *sql.DB
	roDB    *sql.DB
	lockDB  *sql.DB
	lockDSN string
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
	if c.lockDB != nil && c.lockDB != c.rwDB && c.lockDB != c.roDB {
		if err := c.lockDB.Close(); err != nil {
			if c.roDB != nil && c.roDB != c.rwDB {
				_ = c.roDB.Close()
			}
			if c.rwDB != nil {
				_ = c.rwDB.Close()
			}

			return err
		}
	}

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

func (c *mysqlCache) acquireSyncLock(ctx context.Context) (func() error, error) {
	if c == nil || c.rwDB == nil {
		return nil, fmt.Errorf("mlwh: mysql cache not configured")
	}

	lockDB := c.lockDB
	if lockDB == nil {
		lockDB = c.rwDB
	}

	lockConn, err := lockDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: acquire mysql cache lock connection: %w", ErrUpstreamImpaired, err)
	}

	lockName := mysqlSyncLockName(c.lockDSN)
	var gotLock int
	err = lockConn.QueryRowContext(ctx, `SELECT GET_LOCK(?, 0)`, lockName).Scan(&gotLock)
	if err != nil {
		_ = lockConn.Close()

		return nil, fmt.Errorf("%w: acquire mysql cache lock: %w", ErrUpstreamImpaired, err)
	}
	if gotLock != 1 {
		_ = lockConn.Close()

		return nil, ErrSyncAlreadyRunning
	}

	return func() error {
		var released int

		releaseErr := lockConn.QueryRowContext(context.Background(), `SELECT RELEASE_LOCK(?)`, lockName).Scan(&released)
		closeErr := lockConn.Close()
		if releaseErr != nil {
			return fmt.Errorf("%w: release mysql cache lock: %w", ErrUpstreamImpaired, releaseErr)
		}
		if released != 1 {
			return fmt.Errorf("%w: release mysql cache lock", ErrUpstreamImpaired)
		}
		if closeErr != nil {
			return fmt.Errorf("%w: close mysql cache lock connection: %w", ErrUpstreamImpaired, closeErr)
		}

		return nil
	}, nil
}

func mysqlSyncLockName(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if parsed, err := mysql.ParseDSN(trimmed); err == nil {
		trimmed = normalizedMySQLLockScope(parsed)
	}

	sum := sha1.Sum([]byte(trimmed))

	return mysqlSyncLockNamePrefix + hex.EncodeToString(sum[:])[:16]
}

func normalizedMySQLLockScope(parsed *mysql.Config) string {
	if parsed == nil {
		return ""
	}

	return parsed.Net + "|" + parsed.Addr + "|" + parsed.DBName
}

func ensureSQLiteSyncLockRow(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}) error {
	if _, err := execer.ExecContext(ctx, `INSERT OR IGNORE INTO sync_lock(id) VALUES (1)`); err != nil {
		return fmt.Errorf("mlwh: seed sqlite sync_lock row: %w", err)
	}

	return nil
}

func ensureSQLiteSyncLockSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sync_lock(id INTEGER PRIMARY KEY CHECK(id = 1))`); err != nil {
		return fmt.Errorf("mlwh: ensure sqlite sync_lock schema: %w", err)
	}

	return nil
}

func isSQLiteSyncLockBusy(err error) bool {
	message := strings.ToLower(err.Error())

	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
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
		if err = validateCurrentCacheSchema(ctx, db, dialect); err != nil {
			return migrateCacheSchema(ctx, db, dialect, version)
		}

		return nil
	default:
		return migrateCacheSchema(ctx, db, dialect, version)
	}
}

func validateCurrentCacheSchema(ctx context.Context, db *sql.DB, dialect string) error {
	expected, err := expectedCacheSchemaShape(dialect)
	if err != nil {
		return err
	}

	actual, err := readCacheSchemaShape(ctx, db, dialect)
	if err != nil {
		return err
	}
	if allowMissingSampleMirrorIndexesForRecovery(ctx, db, expected, actual) {
		actual.Index["sample_mirror"] = append([]string(nil), expected.Index["sample_mirror"]...)
	}
	if dialect == "mysql" {
		allowLargeMySQLColdLoadIndexShape(ctx, db, expected, actual)
	}

	if err := compareCacheSchemaShapes(expected, actual); err != nil {
		return err
	}

	return nil
}

func expectedCacheSchemaShape(dialect string) (schemaShape, error) {
	stmts, err := loadSchema(dialect)
	if err != nil {
		return schemaShape{}, err
	}

	shape, err := parseSchemaShape(stmts)
	if err != nil {
		return schemaShape{}, err
	}

	return shape, nil
}

func readCacheSchemaShape(ctx context.Context, db *sql.DB, dialect string) (schemaShape, error) {
	switch dialect {
	case "sqlite":
		return readSQLiteCacheSchemaShape(ctx, db)
	case "mysql":
		return readMySQLCacheSchemaShape(ctx, db)
	default:
		return schemaShape{}, fmt.Errorf("mlwh: unsupported cache schema dialect %q", dialect)
	}
}

func compareCacheSchemaShapes(expected, actual schemaShape) error {
	for table, expectedColumns := range expected.Tables {
		actualColumns, ok := actual.Tables[table]
		if !ok {
			return fmt.Errorf("mlwh: cache schema missing table %s", table)
		}
		if len(expectedColumns) != len(actualColumns) {
			return fmt.Errorf("mlwh: cache schema column mismatch for %s", table)
		}
		for column, expectedType := range expectedColumns {
			actualType, ok := actualColumns[column]
			if !ok || actualType != expectedType {
				return fmt.Errorf("mlwh: cache schema column mismatch for %s.%s", table, column)
			}
		}
		if !stringSlicesEqual(expected.Index[table], actual.Index[table]) {
			return fmt.Errorf("mlwh: cache schema index mismatch for %s", table)
		}
		if !stringSlicesEqual(expected.Unique[table], actual.Unique[table]) {
			return fmt.Errorf("mlwh: cache schema unique constraint mismatch for %s", table)
		}
	}

	return nil
}

func allowLargeMySQLColdLoadIndexShape(ctx context.Context, db *sql.DB, expected, actual schemaShape) {
	for _, table := range []string{"iseq_product_metrics_mirror", "seq_product_irods_locations_mirror"} {
		if !mysqlTableExceedsInlineIndexLimit(ctx, db, table, mysqlInlineMirrorIndexRowLimit) {
			continue
		}

		actual.Index[table] = append([]string(nil), expected.Index[table]...)
	}

	if mysqlTableExceedsInlineIndexLimit(ctx, db, "sample_mirror", mysqlInlineSampleIndexRowLimit) {
		actual.Index["sample_mirror"] = append([]string(nil), expected.Index["sample_mirror"]...)
	}
}

func mysqlTableExceedsInlineIndexLimit(ctx context.Context, db *sql.DB, table string, limit int) bool {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		return false
	}

	return count > limit
}

func stringSlicesEqual(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return false
		}
	}

	return true
}

func allowMissingSampleMirrorIndexesForRecovery(ctx context.Context, db *sql.DB, expected, actual schemaShape) bool {
	if stringSlicesEqual(expected.Index["sample_mirror"], actual.Index["sample_mirror"]) {
		return false
	}

	var (
		highWaterRaw   string
		indexesDropped int
	)
	err := db.QueryRowContext(
		ctx,
		`SELECT high_water, indexes_dropped FROM sync_state WHERE table_name = ?`,
		syncTableSample,
	).Scan(&highWaterRaw, &indexesDropped)
	if err != nil || indexesDropped != 1 {
		return false
	}

	highWater, err := parseSyncTimeString(highWaterRaw)
	if err != nil {
		return false
	}

	return !highWater.IsZero() && len(actual.Index["sample_mirror"]) == 0
}

func readSQLiteCacheSchemaShape(ctx context.Context, db *sql.DB) (schemaShape, error) {
	shape := schemaShape{
		Tables: make(map[string]map[string]string, len(schemaStatementOrder)),
		Index:  make(map[string][]string, len(schemaStatementOrder)),
		Unique: make(map[string][]string, len(schemaStatementOrder)),
	}

	for _, table := range schemaStatementOrder {
		columns, err := readSQLiteTableColumns(ctx, db, table)
		if err != nil {
			return schemaShape{}, err
		}
		shape.Tables[table] = columns

		indexes, uniques, err := readSQLiteTableIndexes(ctx, db, table)
		if err != nil {
			return schemaShape{}, err
		}
		if len(indexes) > 0 {
			shape.Index[table] = indexes
		}
		if len(uniques) > 0 {
			shape.Unique[table] = uniques
		}
	}

	return shape, nil
}

func readSQLiteTableColumns(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info('%s')", table))
	if err != nil {
		return nil, fmt.Errorf("mlwh: read sqlite table info for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string]string)
	for rows.Next() {
		var (
			cid        int
			name       string
			typeName   string
			notNull    int
			defaultV   any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultV, &primaryKey); err != nil {
			return nil, fmt.Errorf("mlwh: scan sqlite table info for %s: %w", table, err)
		}
		columns[name] = normaliseTypeFamily(typeName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read sqlite table info for %s: %w", table, err)
	}

	return columns, nil
}

func readSQLiteTableIndexes(ctx context.Context, db *sql.DB, table string) ([]string, []string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_list('%s')", table))
	if err != nil {
		return nil, nil, fmt.Errorf("mlwh: read sqlite index list for %s: %w", table, err)
	}

	type sqliteIndexListEntry struct {
		name   string
		unique int
		origin string
	}
	entries := []sqliteIndexListEntry{}

	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("mlwh: scan sqlite index list for %s: %w", table, err)
		}
		entries = append(entries, sqliteIndexListEntry{name: name, unique: unique, origin: origin})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, nil, fmt.Errorf("mlwh: read sqlite index list for %s: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, fmt.Errorf("mlwh: close sqlite index list for %s: %w", table, err)
	}

	indexes := []string{}
	uniques := []string{}
	for _, entry := range entries {
		if entry.origin == "pk" {
			continue
		}
		columns, err := readSQLiteIndexColumns(ctx, db, entry.name)
		if err != nil {
			return nil, nil, err
		}
		joined := strings.Join(columns, ",")
		if entry.unique == 1 {
			uniques = append(uniques, joined)
			continue
		}
		if entry.origin == "c" {
			indexes = append(indexes, joined)
		}
	}

	sort.Strings(indexes)
	sort.Strings(uniques)

	return indexes, uniques, nil
}

func readSQLiteIndexColumns(ctx context.Context, db *sql.DB, indexName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_info('%s')", indexName))
	if err != nil {
		return nil, fmt.Errorf("mlwh: read sqlite index info for %s: %w", indexName, err)
	}
	defer func() { _ = rows.Close() }()

	columns := []string{}
	for rows.Next() {
		var seqno, cid int
		var name string
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, fmt.Errorf("mlwh: scan sqlite index info for %s: %w", indexName, err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read sqlite index info for %s: %w", indexName, err)
	}

	return columns, nil
}

func readMySQLCacheSchemaShape(ctx context.Context, db *sql.DB) (schemaShape, error) {
	shape := schemaShape{
		Tables: make(map[string]map[string]string, len(schemaStatementOrder)),
		Index:  make(map[string][]string, len(schemaStatementOrder)),
		Unique: make(map[string][]string, len(schemaStatementOrder)),
	}

	for _, table := range schemaStatementOrder {
		columns, err := readMySQLTableColumns(ctx, db, table)
		if err != nil {
			return schemaShape{}, err
		}
		shape.Tables[table] = columns

		indexes, uniques, err := readMySQLTableIndexes(ctx, db, table)
		if err != nil {
			return schemaShape{}, err
		}
		if len(indexes) > 0 {
			shape.Index[table] = indexes
		}
		if len(uniques) > 0 {
			shape.Unique[table] = uniques
		}
	}

	return shape, nil
}

func readMySQLTableColumns(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT COLUMN_NAME, DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, table)
	if err != nil {
		return nil, fmt.Errorf("mlwh: read mysql columns for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string]string)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, fmt.Errorf("mlwh: scan mysql columns for %s: %w", table, err)
		}
		columns[name] = normaliseTypeFamily(dataType)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read mysql columns for %s: %w", table, err)
	}

	return columns, nil
}

func readMySQLTableIndexes(ctx context.Context, db *sql.DB, table string) ([]string, []string, error) {
	rows, err := db.QueryContext(ctx, `SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME <> 'PRIMARY' ORDER BY INDEX_NAME, SEQ_IN_INDEX`, table)
	if err != nil {
		return nil, nil, fmt.Errorf("mlwh: read mysql indexes for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	indexesByName := map[string][]string{}
	uniquesByName := map[string][]string{}
	for rows.Next() {
		var indexName, columnName string
		var nonUnique int
		if err := rows.Scan(&indexName, &nonUnique, &columnName); err != nil {
			return nil, nil, fmt.Errorf("mlwh: scan mysql indexes for %s: %w", table, err)
		}
		if nonUnique == 0 {
			uniquesByName[indexName] = append(uniquesByName[indexName], columnName)
			continue
		}
		indexesByName[indexName] = append(indexesByName[indexName], columnName)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("mlwh: read mysql indexes for %s: %w", table, err)
	}

	indexes := make([]string, 0, len(indexesByName))
	for _, columns := range indexesByName {
		indexes = append(indexes, strings.Join(columns, ","))
	}
	uniques := make([]string, 0, len(uniquesByName))
	for _, columns := range uniquesByName {
		uniques = append(uniques, strings.Join(columns, ","))
	}
	if len(indexes) > 0 {
		sort.Strings(indexes)
	}
	if len(uniques) > 0 {
		sort.Strings(uniques)
	}

	return indexes, uniques, nil
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

func repairDroppedMirrorIndexes(ctx context.Context, db *sql.DB, dialect string) error {
	for _, indexSet := range syncMirrorIndexSets {
		if err := repairDroppedMirrorIndexSet(ctx, db, dialect, indexSet); err != nil {
			return err
		}
	}

	return nil
}

func repairDroppedMirrorIndexSet(ctx context.Context, db *sql.DB, dialect string, indexSet syncMirrorIndexSet) error {
	var (
		highWaterRaw   string
		indexesDropped int
	)

	err := db.QueryRowContext(
		ctx,
		`SELECT high_water, indexes_dropped FROM sync_state WHERE table_name = ?`,
		indexSet.SyncTable,
	).Scan(&highWaterRaw, &indexesDropped)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mlwh: query %s dropped-index recovery state: %w", indexSet.Table, err)
	}
	if indexesDropped != 1 {
		return nil
	}

	highWater, err := parseSyncTimeString(highWaterRaw)
	if err != nil {
		return fmt.Errorf("mlwh: parse %s dropped-index recovery high_water: %w", indexSet.Table, err)
	}
	if highWater.IsZero() {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mlwh: begin %s dropped-index recovery: %w", indexSet.Table, err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if dialect == "sqlite" {
		if _, err = tx.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
			return fmt.Errorf("mlwh: configure sqlite dropped-index recovery: %w", err)
		}
	}
	repaired := true
	if indexSet.Table == "sample_mirror" {
		if err = rebuildSampleMirrorColdLoadIndexes(ctx, tx, dialect); err != nil {
			return err
		}
	} else {
		repaired, err = createMirrorDroppedIndexes(ctx, tx, dialect, indexSet)
		if err != nil {
			return err
		}
	}
	if repaired {
		if _, err = tx.ExecContext(ctx, `UPDATE sync_state SET indexes_dropped = 0 WHERE table_name = ?`, indexSet.SyncTable); err != nil {
			return fmt.Errorf("mlwh: clear %s dropped-index recovery flag: %w", indexSet.Table, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("mlwh: commit %s dropped-index recovery: %w", indexSet.Table, err)
	}

	committed = true

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

	return fmt.Sprintf("file:%s?mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", filepath.ToSlash(path))
}

func sqliteLockDSN(path string) string {
	if path == ":memory:" {
		return ""
	}

	return fmt.Sprintf("file:%s?mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(0)", filepath.ToSlash(path+".sync-lock"))
}

func sqliteReadOnlyDSN(path string) string {
	if path == ":memory:" {
		return path
	}

	return fmt.Sprintf("file:%s?mode=ro", filepath.ToSlash(path))
}
