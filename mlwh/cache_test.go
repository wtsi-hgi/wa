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

		convey.Convey("when OpenCache runs, then it uses sqlite and records schema version 1", func() {
			convey.So(cache.Dialect(), convey.ShouldEqual, "sqlite")
			convey.So(err, convey.ShouldBeNil)
			convey.So(version, convey.ShouldEqual, CacheSchemaVersion)

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
		_, err = cache.DB().Exec(`INSERT INTO negative_cache(raw, reason, fetched_at, ttl_seconds) VALUES (?, ?, ?, ?)`, "sample", "miss", "2026-05-06T12:00:00Z", 900)
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)

		reopened, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(reopened.Close(), convey.ShouldBeNil) })

		var count int
		err = reopened.DB().QueryRow(`SELECT COUNT(*) FROM negative_cache`).Scan(&count)

		convey.Convey("when re-opened, then the schema is not reset", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldEqual, 1)
		})
	})
}

func TestOpenCacheSQLiteSchemaMismatchResetsSchema(t *testing.T) {
	convey.Convey("Given a SQLite cache with an old schema version", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		db, err := sql.Open("sqlite", cachePath)
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = db.Close() })

		convey.So(applySQLiteSchema(context.Background(), db), convey.ShouldBeNil)
		_, err = db.Exec(`INSERT INTO schema_version(version, applied_at) VALUES (?, ?)`, 0, "2026-05-06T12:00:00Z")
		convey.So(err, convey.ShouldBeNil)
		_, err = db.Exec(`INSERT INTO negative_cache(raw, reason, fetched_at, ttl_seconds) VALUES (?, ?, ?, ?)`, "stale", "old", "2026-05-06T12:00:00Z", 900)
		convey.So(err, convey.ShouldBeNil)
		convey.So(db.Close(), convey.ShouldBeNil)

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(cache.Close(), convey.ShouldBeNil) })

		var version int
		convey.So(cache.DB().QueryRow(`SELECT version FROM schema_version`).Scan(&version), convey.ShouldBeNil)

		var count int
		err = cache.DB().QueryRow(`SELECT COUNT(*) FROM negative_cache`).Scan(&count)

		convey.Convey("when OpenCache runs, then it recreates all tables and updates schema_version", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(version, convey.ShouldEqual, CacheSchemaVersion)
			convey.So(count, convey.ShouldEqual, 0)
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

func expectSchemaBootstrap(mock sqlmock.Sqlmock, dialect string) {
	stmts, err := loadSchema(dialect)
	if err != nil {
		panic(err)
	}

	mock.ExpectPing()

	for _, group := range stmts {
		for _, stmt := range splitSQLStatements(group) {
			mock.ExpectExec(regexp.QuoteMeta(stmt)).WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*), COALESCE(MAX(version), 0) FROM schema_version`)).WillReturnRows(
		sqlmock.NewRows([]string{"count", "version"}).AddRow(0, 0),
	)
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
