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
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/smartystreets/goconvey/convey"
)

func TestOpenCacheSQLiteFreshSetsSchemaVersion(t *testing.T) {
	convey.Convey("Given a fresh SQLite cache path", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(cache.Close(), convey.ShouldBeNil) })

		var version int
		err = cache.DB().QueryRow(`SELECT version FROM schema_version`).Scan(&version)

		var syncStateRows int
		convey.So(cache.DB().QueryRow(`SELECT COUNT(*) FROM sync_state`).Scan(&syncStateRows), convey.ShouldBeNil)

		convey.Convey("when OpenCache runs, then it uses sqlite, records schema version 2, and leaves sync_state empty", func() {
			convey.So(cache.Dialect(), convey.ShouldEqual, "sqlite")
			convey.So(err, convey.ShouldBeNil)
			convey.So(version, convey.ShouldEqual, CacheSchemaVersion)
			convey.So(syncStateRows, convey.ShouldEqual, 0)

			var rows int
			convey.So(cache.DB().QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&rows), convey.ShouldBeNil)
			convey.So(rows, convey.ShouldEqual, 1)
		})
	})
}

func TestOpenCacheSQLiteReopenPreservesExistingData(t *testing.T) {
	convey.Convey("Given a SQLite cache already opened at the current schema version", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`INSERT INTO sample_mirror(
			id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name,
			sanger_sample_id, supplier_name, accession_number, donor_id,
			taxon_id, common_name, description, last_updated
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			1, "SQSCP", "sample-1", "uuid-1", "sample-name",
			"ssid-1", "supplier-1", "ENA1", "donor-1",
			9606, "human", "desc", "2026-05-06T12:00:00Z",
		)
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)

		output := captureCacheMigrationOutput(t, func() {
			reopened, openErr := OpenCache(context.Background(), CacheConfig{Path: cachePath})
			convey.So(openErr, convey.ShouldBeNil)
			convey.Reset(func() { convey.So(reopened.Close(), convey.ShouldBeNil) })

			var count int
			err = reopened.DB().QueryRow(`SELECT COUNT(*) FROM sample_mirror`).Scan(&count)
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldEqual, 1)
		})

		convey.Convey("when re-opened at v2, then the cache is not reset and no migration line is emitted", func() {
			convey.So(output, convey.ShouldEqual, "")
		})
	})
}

func TestOpenCacheSQLiteSchemaMismatchResetsSchema(t *testing.T) {
	convey.Convey("Given a SQLite cache with a v1 schema_version row and stale cache tables", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")
		seedSQLiteV1Cache(t, cachePath)

		var (
			version               int
			sampleRows            int
			syncStateRows         int
			remainingSyncState    []string
			resumeCursorColumns   int
			indexesDroppedColumns int
		)

		output := captureCacheMigrationOutput(t, func() {
			cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
			convey.So(err, convey.ShouldBeNil)
			convey.Reset(func() { convey.So(cache.Close(), convey.ShouldBeNil) })

			convey.So(cache.DB().QueryRow(`SELECT version FROM schema_version`).Scan(&version), convey.ShouldBeNil)
			convey.So(cache.DB().QueryRow(`SELECT COUNT(*) FROM sample_mirror`).Scan(&sampleRows), convey.ShouldBeNil)
			convey.So(cache.DB().QueryRow(`SELECT COUNT(*) FROM sync_state`).Scan(&syncStateRows), convey.ShouldBeNil)

			rows, queryErr := cache.DB().Query(`SELECT table_name FROM sync_state ORDER BY table_name`)
			convey.So(queryErr, convey.ShouldBeNil)
			for rows.Next() {
				var tableName string
				convey.So(rows.Scan(&tableName), convey.ShouldBeNil)
				remainingSyncState = append(remainingSyncState, tableName)
			}
			convey.So(rows.Err(), convey.ShouldBeNil)
			convey.So(rows.Close(), convey.ShouldBeNil)

			convey.So(cache.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sync_state') WHERE name = 'resume_cursor'`).Scan(&resumeCursorColumns), convey.ShouldBeNil)
			convey.So(cache.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sync_state') WHERE name = 'indexes_dropped'`).Scan(&indexesDroppedColumns), convey.ShouldBeNil)
		})

		convey.Convey("when OpenCache runs, then it migrates to v2, recreates the affected tables, and clears sync_state", func() {
			convey.So(output, convey.ShouldEqual, "mlwh cache: schema v1->v2, recreated tables: [donor_samples, iseq_product_metrics_mirror, library_samples, sample_mirror, seq_product_irods_locations_mirror, study_mirror]\n")
			convey.So(version, convey.ShouldEqual, CacheSchemaVersion)
			convey.So(sampleRows, convey.ShouldEqual, 0)
			convey.So(syncStateRows, convey.ShouldEqual, 1)
			convey.So(remainingSyncState, convey.ShouldResemble, []string{"unrelated"})
			convey.So(resumeCursorColumns, convey.ShouldEqual, 1)
			convey.So(indexesDroppedColumns, convey.ShouldEqual, 1)
		})
	})
}

func TestOpenCacheSQLiteCurrentVersionEmitsNoMigrationLine(t *testing.T) {
	convey.Convey("Given a SQLite cache already at schema version 2", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)

		output := captureCacheMigrationOutput(t, func() {
			reopened, openErr := OpenCache(context.Background(), CacheConfig{Path: cachePath})
			convey.So(openErr, convey.ShouldBeNil)
			convey.Reset(func() { convey.So(reopened.Close(), convey.ShouldBeNil) })
		})

		convey.Convey("when OpenCache runs again, then it emits no migration line", func() {
			convey.So(output, convey.ShouldEqual, "")
		})
	})
}

func TestOpenCacheMySQLMigratesV1Cache(t *testing.T) {
	convey.Convey("Given a MySQL cache backend whose schema_version row is still v1", t, func() {
		originalOpen := sqlOpenFunc
		defer func() { sqlOpenFunc = originalOpen }()

		rwDB, rwMock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		convey.So(err, convey.ShouldBeNil)
		roDB, roMock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		convey.So(err, convey.ShouldBeNil)

		rwMock.ExpectPing()
		rwMock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*), COALESCE(MAX(version), 0) FROM schema_version`)).WillReturnRows(
			sqlmock.NewRows([]string{"count", "version"}).AddRow(1, 1),
		)
		expectMySQLSchemaMigration(rwMock, 1, CacheSchemaVersion)
		rwMock.ExpectClose()

		roMock.ExpectPing()
		roMock.ExpectClose()

		openCount := 0
		sqlOpenFunc = func(driverName, dataSourceName string) (*sql.DB, error) {
			convey.So(driverName, convey.ShouldEqual, "mysql")
			openCount++

			switch openCount {
			case 1:
				return rwDB, nil
			case 2:
				return roDB, nil
			default:
				return nil, fmt.Errorf("unexpected sql.Open call %d", openCount)
			}
		}

		output := captureCacheMigrationOutput(t, func() {
			cache, openErr := OpenCache(context.Background(), CacheConfig{Path: "cache_user@tcp(localhost:3306)/wa_cache"})
			convey.So(openErr, convey.ShouldBeNil)
			convey.So(cache.Close(), convey.ShouldBeNil)
		})

		convey.Convey("when OpenCache runs, then it applies the v2 migration and emits the same single stderr line", func() {
			convey.So(output, convey.ShouldEqual, "mlwh cache: schema v1->v2, recreated tables: [donor_samples, iseq_product_metrics_mirror, library_samples, sample_mirror, seq_product_irods_locations_mirror, study_mirror]\n")
			convey.So(rwMock.ExpectationsWereMet(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestOpenCacheRejectsMySQLDSNWithPassword(t *testing.T) {
	convey.Convey("Given a MySQL DSN that embeds a password", t, func() {
		_, err := OpenCache(context.Background(), CacheConfig{Path: "user:secret@tcp(localhost:3306)/wa_cache"})

		convey.Convey("when OpenCache runs, then it returns ErrPasswordInDSN", func() {
			convey.So(errors.Is(err, ErrPasswordInDSN), convey.ShouldBeTrue)
		})
	})
}

func TestOpenCacheSQLiteEnablesWAL(t *testing.T) {
	convey.Convey("Given a fresh SQLite cache", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(cache.Close(), convey.ShouldBeNil) })

		var mode string
		err = cache.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode)

		convey.Convey("when journal_mode is queried, then it is WAL", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(strings.ToLower(mode), convey.ShouldEqual, "wal")
		})
	})
}

func TestClientSyncSerializesSQLiteWithMutex(t *testing.T) {
	convey.Convey("Given a client with a SQLite cache on a single mocked connection", t, func() {
		rwDB, rwMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		rwDB.SetMaxOpenConns(1)

		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		roMock.ExpectClose()

		releaseFirst := make(chan struct{})
		firstEntered := make(chan struct{}, 1)
		secondEntered := make(chan struct{}, 1)
		var enteredCount int32

		rwMock.MatchExpectationsInOrder(true)
		rwMock.ExpectBegin()
		rwMock.ExpectCommit()
		rwMock.ExpectBegin()
		rwMock.ExpectCommit()
		rwMock.ExpectClose()

		client := &Client{
			cache:       &sqliteCache{rwDB: rwDB, roDB: roDB},
			cacheReader: roDB,
			syncMu:      &sync.Mutex{},
			syncRunner: func(ctx context.Context, tx *sql.Tx, tables []string) error {
				switch atomic.AddInt32(&enteredCount, 1) {
				case 1:
					firstEntered <- struct{}{}
					<-releaseFirst
				default:
					secondEntered <- struct{}{}
				}

				return nil
			},
		}

		var wg sync.WaitGroup
		wg.Add(2)

		var firstErr error
		var secondErr error
		go func() {
			defer wg.Done()
			_, firstErr = client.Sync(context.Background(), "sample")
		}()

		<-firstEntered

		secondDone := make(chan error, 1)
		go func() {
			defer wg.Done()
			_, secondErr = client.Sync(context.Background(), "sample")
			secondDone <- secondErr
		}()

		select {
		case <-secondEntered:
			convey.So("second sync entered early", convey.ShouldEqual, "")
		case <-time.After(50 * time.Millisecond):
		}

		close(releaseFirst)
		wg.Wait()

		convey.Convey("when Sync is called concurrently, then the second transaction starts only after the first commits", func() {
			convey.So(firstErr, convey.ShouldBeNil)
			convey.So(<-secondDone, convey.ShouldBeNil)
			convey.So(secondErr, convey.ShouldBeNil)
			convey.So(len(secondEntered), convey.ShouldEqual, 1)
			convey.So(rwDB.Close(), convey.ShouldBeNil)
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(rwMock.ExpectationsWereMet(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncSerializesMySQLWithGetLock(t *testing.T) {
	convey.Convey("Given a client with a MySQL cache and ordered GET_LOCK expectations", t, func() {
		rwDB, rwMock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		convey.So(err, convey.ShouldBeNil)

		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)

		releaseFirst := make(chan struct{})
		firstLock := make(chan struct{}, 1)
		secondLock := make(chan struct{}, 1)
		var enteredCount int32

		rwMock.MatchExpectationsInOrder(true)
		rwMock.ExpectQuery("SELECT GET_LOCK\\('wa_mlwh_sync', \\?\\)").WithArgs(defaultMySQLLockTimeoutSeconds).WillReturnRows(sqlmock.NewRows([]string{"got_lock"}).AddRow(1)).WillDelayFor(5 * time.Millisecond)
		rwMock.ExpectBegin()
		rwMock.ExpectCommit()
		rwMock.ExpectQuery("SELECT RELEASE_LOCK\\('wa_mlwh_sync'\\)").WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1)).WillDelayFor(5 * time.Millisecond)
		rwMock.ExpectQuery("SELECT GET_LOCK\\('wa_mlwh_sync', \\?\\)").WithArgs(defaultMySQLLockTimeoutSeconds).WillReturnRows(sqlmock.NewRows([]string{"got_lock"}).AddRow(1)).WillDelayFor(5 * time.Millisecond)
		rwMock.ExpectBegin()
		rwMock.ExpectCommit()
		rwMock.ExpectQuery("SELECT RELEASE_LOCK\\('wa_mlwh_sync'\\)").WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))

		cache := &mysqlCache{rwDB: rwDB, roDB: roDB}
		client := &Client{
			cache:       cache,
			cacheReader: roDB,
			syncMu:      &sync.Mutex{},
			syncRunner: func(ctx context.Context, tx *sql.Tx, tables []string) error {
				switch atomic.AddInt32(&enteredCount, 1) {
				case 1:
					firstLock <- struct{}{}
					<-releaseFirst
				default:
					secondLock <- struct{}{}
				}

				return nil
			},
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			_, _ = client.Sync(context.Background(), "sample")
		}()

		<-firstLock

		go func() {
			defer wg.Done()
			_, _ = client.Sync(context.Background(), "sample")
		}()

		select {
		case <-secondLock:
			convey.So("second mysql sync entered early", convey.ShouldEqual, "")
		case <-time.After(50 * time.Millisecond):
		}

		close(releaseFirst)
		wg.Wait()

		convey.Convey("when Sync is called concurrently, then GET_LOCK and RELEASE_LOCK serialize the calls", func() {
			convey.So(len(secondLock), convey.ShouldEqual, 1)
			convey.So(rwMock.ExpectationsWereMet(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestOpenCacheInjectsMySQLPasswordIntoResolvedDSN(t *testing.T) {
	convey.Convey("Given a MySQL DSN without a password and a provided CacheConfig password", t, func() {
		originalOpen := sqlOpenFunc
		defer func() { sqlOpenFunc = originalOpen }()

		rwDB, rwMock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		convey.So(err, convey.ShouldBeNil)
		roDB, roMock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		convey.So(err, convey.ShouldBeNil)

		expectSchemaBootstrap(rwMock, "mysql")
		roMock.ExpectPing()

		var opened []string
		openCount := 0

		sqlOpenFunc = func(driverName, dataSourceName string) (*sql.DB, error) {
			convey.So(driverName, convey.ShouldEqual, "mysql")
			opened = append(opened, dataSourceName)
			openCount++

			switch openCount {
			case 1:
				return rwDB, nil
			case 2:
				return roDB, nil
			default:
				return nil, fmt.Errorf("unexpected sql.Open call %d", openCount)
			}
		}

		_, err = OpenCache(context.Background(), CacheConfig{Path: "cache_user@tcp(localhost:3306)/wa_cache", Password: "secret"})

		convey.Convey("when OpenCache runs, then the write DSN gets the injected password and the read DSN is transaction-read-only", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(opened, convey.ShouldHaveLength, 2)

			rwCfg, parseErr := mysql.ParseDSN(opened[0])
			convey.So(parseErr, convey.ShouldBeNil)
			roCfg, parseErr := mysql.ParseDSN(opened[1])
			convey.So(parseErr, convey.ShouldBeNil)

			convey.So(rwCfg.Passwd, convey.ShouldEqual, "secret")
			convey.So(strings.Contains(opened[0], "secret"), convey.ShouldBeTrue)
			convey.So(rwCfg.Params["transaction_read_only"], convey.ShouldEqual, "")

			convey.So(roCfg.Passwd, convey.ShouldEqual, "secret")
			convey.So(strings.Contains(opened[1], "secret"), convey.ShouldBeTrue)
			convey.So(roCfg.Params["transaction_read_only"], convey.ShouldEqual, "1")

			convey.So(rwMock.ExpectationsWereMet(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestApplySchemaMySQLPre8FallsBackToGeneralCaseInsensitiveCollation(t *testing.T) {
	convey.Convey("Given schema application against a pre-8 MySQL server", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			convey.So(db.Close(), convey.ShouldBeNil)
			convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		stmts, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT VERSION()`)).WillReturnRows(
			sqlmock.NewRows([]string{"version"}).AddRow("5.7.44"),
		)

		for _, group := range stmts {
			legacyGroup := strings.ReplaceAll(group, "utf8mb4_0900_ai_ci", "utf8mb4_general_ci")
			for _, stmt := range splitSQLStatements(legacyGroup) {
				mock.ExpectExec(regexp.QuoteMeta(stmt)).WillReturnResult(sqlmock.NewResult(0, 0))
			}
		}
		mock.ExpectClose()

		err = applySchema(context.Background(), db, "mysql")

		convey.Convey("when applySchema runs, then it substitutes utf8mb4_general_ci into every DDL statement", func() {
			convey.So(err, convey.ShouldBeNil)
		})
	})
}

func expectSchemaBootstrap(mock sqlmock.Sqlmock, dialect string) {
	stmts, err := loadSchema(dialect)
	if err != nil {
		panic(err)
	}

	mock.ExpectPing()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*), COALESCE(MAX(version), 0) FROM schema_version`)).WillReturnError(
		&mysql.MySQLError{Number: 1146, Message: "Table 'wa_cache.schema_version' doesn't exist"},
	)
	if dialect == "mysql" {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT VERSION()`)).WillReturnRows(
			sqlmock.NewRows([]string{"version"}).AddRow("8.0.36"),
		)
	}

	for _, group := range stmts {
		for _, stmt := range splitSQLStatements(group) {
			mock.ExpectExec(regexp.QuoteMeta(stmt)).WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM schema_version`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO schema_version(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`)).WithArgs(CacheSchemaVersion).WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestClientReadOnlyHandleRejectsWrites(t *testing.T) {
	convey.Convey("Given a client opened with a warm SQLite cache", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")
		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(cache.Close(), convey.ShouldBeNil) })

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}
		_, err = client.ReadDB().Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "lib", 1, "study")

		convey.Convey("when a write is attempted on the read-only handle, then it fails", func() {
			convey.So(err, convey.ShouldNotBeNil)
		})
	})

	convey.Convey("Given a client with a MySQL read-only cache handle", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)

		readOnlyErr := &mysql.MySQLError{Number: 1792, Message: "Cannot execute statement in a READ ONLY transaction"}
		roMock.ExpectExec(regexp.QuoteMeta(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`)).
			WithArgs("lib", 1, "study").
			WillReturnError(readOnlyErr)
		roMock.ExpectClose()

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}
		_, err = client.ReadDB().Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "lib", 1, "study")

		convey.Convey("when a write is attempted on the exposed read-only handle, then MySQL returns a read-only error", func() {
			convey.So(err, convey.ShouldEqual, readOnlyErr)
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func cacheReadDB(cache Cache) *sql.DB {
	reader, ok := cache.(interface{ ReadDB() *sql.DB })
	if !ok {
		return nil
	}

	return reader.ReadDB()
}

func captureCacheMigrationOutput(t *testing.T, run func()) string {
	t.Helper()

	original := cacheMigrationStderr
	var buffer bytes.Buffer
	cacheMigrationStderr = &buffer
	t.Cleanup(func() {
		cacheMigrationStderr = original
	})

	run()

	return buffer.String()
}

func seedSQLiteV1Cache(t *testing.T, cachePath string) {
	t.Helper()

	db, err := sql.Open("sqlite", cachePath)
	convey.So(err, convey.ShouldBeNil)
	t.Cleanup(func() { _ = db.Close() })

	v1Statements := []string{
		`CREATE TABLE schema_version(version INTEGER NOT NULL PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE TABLE sync_state(table_name TEXT NOT NULL PRIMARY KEY, high_water TEXT NOT NULL, last_run TEXT NOT NULL)`,
		`CREATE TABLE sample_mirror (
			id_sample_tmp INTEGER NOT NULL PRIMARY KEY,
			id_lims TEXT NOT NULL,
			id_sample_lims TEXT NOT NULL,
			uuid_sample_lims TEXT NOT NULL,
			id_study_lims TEXT NOT NULL,
			name TEXT NOT NULL,
			sanger_id TEXT NOT NULL,
			sanger_sample_id TEXT NOT NULL,
			supplier_name TEXT NOT NULL,
			accession_number TEXT NOT NULL,
			donor_id TEXT NOT NULL,
			library_type TEXT NOT NULL,
			taxon_id INTEGER NOT NULL,
			common_name TEXT NOT NULL,
			description TEXT NOT NULL,
			last_updated TEXT NOT NULL
		)`,
		`CREATE TABLE study_mirror(id_study_tmp INTEGER NOT NULL PRIMARY KEY, id_lims TEXT NOT NULL, id_study_lims TEXT NOT NULL, uuid_study_lims TEXT NOT NULL, name TEXT NOT NULL, accession_number TEXT NOT NULL, study_title TEXT NOT NULL, faculty_sponsor TEXT NOT NULL, state TEXT NOT NULL, abstract TEXT NOT NULL, abbreviation TEXT NOT NULL, description TEXT NOT NULL, data_release_strategy TEXT NOT NULL, data_access_group TEXT NOT NULL, hmdmc_number TEXT NOT NULL, programme TEXT NOT NULL, created TEXT NOT NULL, reference_genome TEXT NOT NULL, ethically_approved INTEGER NOT NULL DEFAULT 0, study_type TEXT NOT NULL, contains_human_dna INTEGER NOT NULL DEFAULT 0, contaminated_human_dna INTEGER NOT NULL DEFAULT 0, study_visibility TEXT NOT NULL, egadac_accession_number TEXT NOT NULL, ega_policy_accession_number TEXT NOT NULL, data_release_timing TEXT NOT NULL, last_updated TEXT NOT NULL)`,
		`CREATE TABLE library_samples(pipeline_id_lims TEXT NOT NULL, id_sample_tmp INTEGER NOT NULL, id_study_lims TEXT NOT NULL)`,
		`CREATE TABLE donor_samples(donor_id TEXT NOT NULL, id_sample_tmp INTEGER NOT NULL, id_study_lims TEXT NOT NULL)`,
		`CREATE TABLE negative_cache(raw TEXT PRIMARY KEY, reason TEXT, fetched_at TEXT, ttl_seconds INTEGER)`,
		`CREATE TABLE watermarks(name TEXT PRIMARY KEY, updated_at TEXT NOT NULL)`,
		`CREATE TABLE enrich_cache(cache_key TEXT PRIMARY KEY, payload TEXT NOT NULL, fetched_at TEXT NOT NULL)`,
	}

	for _, stmt := range v1Statements {
		_, err = db.Exec(stmt)
		convey.So(err, convey.ShouldBeNil)
	}

	_, err = db.Exec(`INSERT INTO schema_version(version, applied_at) VALUES (?, ?)`, 1, "2026-05-10T09:00:00Z")
	convey.So(err, convey.ShouldBeNil)

	staleSyncStateTables := []string{
		"iseq_product_metrics",
		"seq_product_irods_locations",
		syncTableIseqFlowcell,
		syncTableSample,
		syncTableStudy,
		"unrelated",
	}
	for _, tableName := range staleSyncStateTables {
		_, err = db.Exec(`INSERT INTO sync_state(table_name, high_water, last_run) VALUES (?, ?, ?)`, tableName, "2026-05-10T09:00:00Z", "2026-05-10T09:00:00Z")
		convey.So(err, convey.ShouldBeNil)
	}

	_, err = db.Exec(`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, id_study_lims, name, sanger_id, sanger_sample_id, supplier_name, accession_number, donor_id, library_type, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, 1, "SQSCP", "sample-1", "uuid-1", "study-1", "sample-name", "sample-name", "ssid-1", "supplier-1", "ENA1", "donor-1", "WGS", 9606, "human", "desc", "2026-05-10T09:00:00Z")
	convey.So(err, convey.ShouldBeNil)
}

func expectMySQLSchemaMigration(mock sqlmock.Sqlmock, fromVersion, toVersion int) {
	for idx := len(cacheMigrationDropTables) - 1; idx >= 0; idx-- {
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS ` + cacheMigrationDropTables[idx])).WillReturnResult(sqlmock.NewResult(0, 0))
	}

	stmts, err := loadSchema("mysql")
	if err != nil {
		panic(err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT VERSION()`)).WillReturnRows(
		sqlmock.NewRows([]string{"version"}).AddRow("8.0.36"),
	)

	for _, group := range stmts {
		for _, stmt := range splitSQLStatements(group) {
			mock.ExpectExec(regexp.QuoteMeta(stmt)).WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM sync_state LIMIT 0`)).WillReturnRows(
		sqlmock.NewRows([]string{"table_name", "high_water", "last_run"}),
	)
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE sync_state ADD COLUMN resume_cursor TEXT NULL`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE sync_state ADD COLUMN indexes_dropped INT NOT NULL DEFAULT 0`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM sync_state WHERE table_name IN (?, ?, ?, ?, ?)`)).
		WithArgs(
			"iseq_product_metrics",
			"seq_product_irods_locations",
			syncTableIseqFlowcell,
			syncTableSample,
			syncTableStudy,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM schema_version`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO schema_version(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`)).WithArgs(toVersion).WillReturnResult(sqlmock.NewResult(1, 1))
}
