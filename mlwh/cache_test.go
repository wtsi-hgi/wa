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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
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

		convey.Convey("when OpenCache runs, then it uses sqlite, records the current schema version, and leaves sync_state empty", func() {
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

func TestOpenCacheSQLiteCreatesSampleSearchTokenTable(t *testing.T) {
	convey.Convey("Given an empty SQLite cache opened through OpenCache", t, func() {
		cache, err := OpenCache(context.Background(), CacheConfig{Path: filepath.Join(t.TempDir(), "cache.sqlite")})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(cache.Close(), convey.ShouldBeNil) })

		convey.Convey("when the schema is applied, then a sample_search_token table over (token, id_sample_tmp) exists with a covering index and is queryable by token prefix", func() {
			var sql string
			convey.So(cache.DB().QueryRow(
				`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'sample_search_token'`,
			).Scan(&sql), convey.ShouldBeNil)

			upper := strings.ToUpper(sql)
			convey.So(upper, convey.ShouldNotContainSubstring, "VIRTUAL TABLE")
			convey.So(upper, convey.ShouldNotContainSubstring, "FTS5")
			convey.So(sql, convey.ShouldContainSubstring, "token")
			convey.So(sql, convey.ShouldContainSubstring, "id_sample_tmp")

			var indexSQL string
			convey.So(cache.DB().QueryRow(
				`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'sample_search_token_idx'`,
			).Scan(&indexSQL), convey.ShouldBeNil)
			convey.So(indexSQL, convey.ShouldContainSubstring, "token, id_sample_tmp")

			_, err = cache.DB().Exec(`SELECT id_sample_tmp FROM sample_search_token WHERE token LIKE ? ESCAPE '!' LIMIT 1`, "abc%")
			convey.So(err, convey.ShouldBeNil)
		})
	})
}

func TestAllowLargeMySQLColdLoadIndexShapeAllowsLegacySampleNameOnlyIndex(t *testing.T) {
	convey.Convey("Given a large MySQL sample mirror with the legacy name-only read index shape", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		expected := schemaShape{Index: map[string][]string{
			"iseq_product_metrics_mirror":        {"id_sample_tmp,id_run,position,tag_index"},
			"seq_product_irods_locations_mirror": {"id_sample_tmp", "id_study_lims,id_sample_tmp"},
			"sample_mirror":                      {"accession_number", "donor_id", "id_sample_lims", "last_updated", "name", "sanger_sample_id", "supplier_name", "uuid_sample_lims"},
		}}
		actual := schemaShape{Index: map[string][]string{
			"iseq_product_metrics_mirror":        {"id_sample_tmp,id_run,position,tag_index"},
			"seq_product_irods_locations_mirror": {"id_sample_tmp", "id_study_lims,id_sample_tmp"},
			"sample_mirror":                      {"name"},
		}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT TABLE_ROWS FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`)).
			WithArgs("sample_mirror").
			WillReturnRows(sqlmock.NewRows([]string{"TABLE_ROWS"}).AddRow(mysqlInlineSampleIndexRowLimit + 1))

		allowLargeMySQLColdLoadIndexShape(context.Background(), db, expected, actual)

		convey.So(actual.Index["sample_mirror"], convey.ShouldResemble, expected.Index["sample_mirror"])
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestRepairCompletedSampleMirrorCreatesMissingLookupIndexes(t *testing.T) {
	convey.Convey("Given a completed MySQL sample mirror with only the legacy name index", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		highWater := time.Date(2026, time.June, 5, 9, 45, 0, 0, time.UTC)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow(formatSyncTime(highWater), nil, 0))
		mock.ExpectQuery(regexp.QuoteMeta(mirrorIndexInventoryQuery("mysql", sampleMirrorIndexSet.Table))).
			WillReturnRows(sqlmock.NewRows([]string{"INDEX_NAME"}).AddRow("sample_mirror_name_idx"))
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(mirrorIndexInventoryQuery("mysql", sampleMirrorIndexSet.Table))).
			WillReturnRows(sqlmock.NewRows([]string{"INDEX_NAME"}).AddRow("sample_mirror_name_idx"))

		missing := []syncIndexSpec{
			{Name: "sample_mirror_id_sample_lims_idx", Column: "id_sample_lims"},
			{Name: "sample_mirror_uuid_sample_lims_idx", Column: "uuid_sample_lims"},
			{Name: "sample_mirror_sanger_sample_id_idx", Column: "sanger_sample_id"},
			{Name: "sample_mirror_supplier_name_idx", Column: "supplier_name"},
			{Name: "sample_mirror_accession_number_idx", Column: "accession_number"},
			{Name: "sample_mirror_donor_id_idx", Column: "donor_id"},
			{Name: "sample_mirror_last_updated_idx", Column: "last_updated"},
		}
		mock.ExpectExec(regexp.QuoteMeta(buildMySQLCreateMirrorSecondaryIndexesStatement("sample_mirror", missing))).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE sync_state SET indexes_dropped = 0 WHERE table_name = ?`)).
			WithArgs(syncTableSample).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err = repairDroppedMirrorIndexSet(context.Background(), db, "mysql", sampleMirrorIndexSet)

		convey.So(err, convey.ShouldBeNil)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestOpenCacheSQLitePreviousVersionRecreatesSampleSearchTokenTable(t *testing.T) {
	convey.Convey("Given a SQLite cache at the previous schema version", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`DROP TABLE IF EXISTS sample_search_token`)
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`UPDATE schema_version SET version = ?`, CacheSchemaVersion-1)
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)

		var (
			searchTables  int
			searchUsable  error
			migrationLine string
		)

		migrationLine = captureCacheMigrationOutput(t, func() {
			reopened, openErr := OpenCache(context.Background(), CacheConfig{Path: cachePath})
			convey.So(openErr, convey.ShouldBeNil)
			convey.Reset(func() { convey.So(reopened.Close(), convey.ShouldBeNil) })

			convey.So(reopened.DB().QueryRow(
				`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'sample_search_token'`,
			).Scan(&searchTables), convey.ShouldBeNil)
			_, searchUsable = reopened.DB().Exec(`SELECT id_sample_tmp FROM sample_search_token WHERE token LIKE ? ESCAPE '!' LIMIT 1`, "abc%")
		})

		convey.Convey("when OpenCache runs at the new version, then it prints exactly one migration line (now including sample_search_token) and the token table is present and usable", func() {
			convey.So(migrationLine, convey.ShouldEqual, fmt.Sprintf(
				"mlwh cache: schema v%d->v%d, recreated tables: [donor_samples, iseq_product_metrics_mirror, library_samples, sample_mirror, sample_search_token, seq_product_irods_locations_mirror, study_mirror]\n",
				CacheSchemaVersion-1, CacheSchemaVersion,
			))
			convey.So(searchTables, convey.ShouldEqual, 1)
			convey.So(searchUsable, convey.ShouldBeNil)
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

		convey.Convey("when OpenCache runs, then it migrates to the current version, recreates the affected tables, and clears sync_state", func() {
			convey.So(output, convey.ShouldEqual, fmt.Sprintf("mlwh cache: schema v1->v%d, recreated tables: [donor_samples, iseq_product_metrics_mirror, library_samples, sample_mirror, sample_search_token, seq_product_irods_locations_mirror, study_mirror]\n", CacheSchemaVersion))
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
	convey.Convey("Given a SQLite cache already at the current schema version", t, func() {
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

func TestOpenCacheSQLiteCurrentVersionShapeMismatchResetsSchema(t *testing.T) {
	convey.Convey("Given a SQLite cache at the current schema version with a drifted table set", t, func() {
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
		_, err = cache.DB().Exec(`DROP TABLE study_mirror`)
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)

		output := captureCacheMigrationOutput(t, func() {
			reopened, openErr := OpenCache(context.Background(), CacheConfig{Path: cachePath})
			convey.So(openErr, convey.ShouldBeNil)
			convey.Reset(func() { convey.So(reopened.Close(), convey.ShouldBeNil) })

			var sampleRows int
			convey.So(reopened.DB().QueryRow(`SELECT COUNT(*) FROM sample_mirror`).Scan(&sampleRows), convey.ShouldBeNil)
			convey.So(sampleRows, convey.ShouldEqual, 0)
		})

		convey.So(output, convey.ShouldEqual, fmt.Sprintf("mlwh cache: schema v%d->v%d, recreated tables: [donor_samples, iseq_product_metrics_mirror, library_samples, sample_mirror, sample_search_token, seq_product_irods_locations_mirror, study_mirror]\n", CacheSchemaVersion, CacheSchemaVersion))
	})
}

func TestOpenCacheSQLiteCurrentVersionWrongShapeResetsSchema(t *testing.T) {
	convey.Convey("Given a SQLite cache at the current schema version with a same-name table of the wrong shape", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)

		db, err := sql.Open("sqlite", sqliteWritableDSN(cachePath))
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(db.Close(), convey.ShouldBeNil) })

		_, err = db.Exec(`DROP TABLE study_mirror`)
		convey.So(err, convey.ShouldBeNil)
		_, err = db.Exec(`CREATE TABLE study_mirror(id_study_lims TEXT PRIMARY KEY)`)
		convey.So(err, convey.ShouldBeNil)

		output := captureCacheMigrationOutput(t, func() {
			reopened, openErr := OpenCache(context.Background(), CacheConfig{Path: cachePath})
			convey.So(openErr, convey.ShouldBeNil)
			convey.Reset(func() { convey.So(reopened.Close(), convey.ShouldBeNil) })

			var columnCount int
			convey.So(reopened.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('study_mirror')`).Scan(&columnCount), convey.ShouldBeNil)
			convey.So(columnCount, convey.ShouldBeGreaterThan, 1)
		})

		convey.So(output, convey.ShouldEqual, fmt.Sprintf("mlwh cache: schema v%d->v%d, recreated tables: [donor_samples, iseq_product_metrics_mirror, library_samples, sample_mirror, sample_search_token, seq_product_irods_locations_mirror, study_mirror]\n", CacheSchemaVersion, CacheSchemaVersion))
	})
}

func TestOpenCacheSQLiteRepairsDroppedSampleIndexesSilently(t *testing.T) {
	convey.Convey("B4.4: Given a SQLite cache with sample_mirror indexes missing and sync_state.indexes_dropped=1 plus non-zero high_water, when OpenCache runs again, then it recreates the indexes, clears the flag, and emits no migration line", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)

		db, err := sql.Open("sqlite", sqliteWritableDSN(cachePath))
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(db.Close(), convey.ShouldBeNil) })

		for _, indexName := range sampleMirrorSecondaryIndexNames() {
			_, err = db.Exec(`DROP INDEX IF EXISTS ` + indexName)
			convey.So(err, convey.ShouldBeNil)
		}

		_, err = db.Exec(
			`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, ?, ?) ON CONFLICT(table_name) DO UPDATE SET high_water = excluded.high_water, last_run = excluded.last_run, resume_cursor = excluded.resume_cursor, indexes_dropped = excluded.indexes_dropped`,
			syncTableSample,
			"2026-05-11T19:00:00Z",
			"2026-05-11T19:00:00Z",
			nil,
			1,
		)
		convey.So(err, convey.ShouldBeNil)

		preRepairIndexes := sampleMirrorIndexNames(t, db, "sqlite")
		convey.So(preRepairIndexes, convey.ShouldHaveLength, 0)

		output := captureCacheMigrationOutput(t, func() {
			reopened, openErr := OpenCache(context.Background(), CacheConfig{Path: cachePath})
			convey.So(openErr, convey.ShouldBeNil)
			convey.Reset(func() { convey.So(reopened.Close(), convey.ShouldBeNil) })
		})

		convey.So(output, convey.ShouldEqual, "")
		convey.So(sampleMirrorIndexNames(t, db, "sqlite"), convey.ShouldResemble, sampleMirrorSecondaryIndexNames())
		convey.So(readSyncStateRow(t, db, syncTableSample).IndexesDropped, convey.ShouldEqual, 0)
	})
}

func TestOpenCacheSQLiteSkipsDroppedIndexRepairDuringColdResume(t *testing.T) {
	convey.Convey("Given a SQLite product mirror with dropped indexes and a cold resume cursor", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)

		db, err := sql.Open("sqlite", sqliteWritableDSN(cachePath))
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(db.Close(), convey.ShouldBeNil) })

		for _, indexName := range iseqProductMetricsMirrorSecondaryIndexNames() {
			_, err = db.Exec(`DROP INDEX IF EXISTS ` + indexName)
			convey.So(err, convey.ShouldBeNil)
		}

		_, err = db.Exec(
			`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, ?, ?) ON CONFLICT(table_name) DO UPDATE SET high_water = excluded.high_water, last_run = excluded.last_run, resume_cursor = excluded.resume_cursor, indexes_dropped = excluded.indexes_dropped`,
			syncTableIseqProductMetrics,
			"2026-05-13T10:47:57Z",
			"2026-05-13T10:48:00Z",
			iseqProductMetricsIDResumeMode+"\t2110183853",
			1,
		)
		convey.So(err, convey.ShouldBeNil)
		convey.So(mirrorIndexNames(t, db, "sqlite", iseqProductMetricsMirrorIndexSet), convey.ShouldBeEmpty)

		output := captureCacheMigrationOutput(t, func() {
			reopened, openErr := OpenCache(context.Background(), CacheConfig{Path: cachePath})
			convey.So(openErr, convey.ShouldBeNil)
			convey.Reset(func() { convey.So(reopened.Close(), convey.ShouldBeNil) })
		})

		convey.So(output, convey.ShouldEqual, "")
		convey.So(mirrorIndexNames(t, db, "sqlite", iseqProductMetricsMirrorIndexSet), convey.ShouldBeEmpty)
		convey.So(readSyncStateRow(t, db, syncTableIseqProductMetrics).IndexesDropped, convey.ShouldEqual, 1)
	})
}

func TestOpenCacheSQLiteRepairsSparseSampleReadIndexes(t *testing.T) {
	convey.Convey("Given a completed sparse SQLite sample cache with only the name read index", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)

		seedSampleMirrorRow(t, cache.DB(), 1, "7607STDY14643771", "Hek_R1", "donor-1", time.Date(2026, time.May, 13, 11, 24, 59, 0, time.UTC))
		convey.So(cache.Close(), convey.ShouldBeNil)

		db, err := sql.Open("sqlite", sqliteWritableDSN(cachePath))
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(db.Close(), convey.ShouldBeNil) })

		for _, indexName := range sampleMirrorSecondaryIndexNames() {
			if indexName == "sample_mirror_name_idx" {
				continue
			}
			_, err = db.Exec(`DROP INDEX IF EXISTS ` + indexName)
			convey.So(err, convey.ShouldBeNil)
		}
		_, err = db.Exec(
			`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, ?, ?) ON CONFLICT(table_name) DO UPDATE SET high_water = excluded.high_water, last_run = excluded.last_run, resume_cursor = excluded.resume_cursor, indexes_dropped = excluded.indexes_dropped`,
			syncTableSample,
			"2026-05-13T11:24:59Z",
			"2026-05-13T11:25:00Z",
			nil,
			1,
		)
		convey.So(err, convey.ShouldBeNil)

		output := captureCacheMigrationOutput(t, func() {
			reopened, openErr := OpenCache(context.Background(), CacheConfig{Path: cachePath})
			convey.So(openErr, convey.ShouldBeNil)
			convey.Reset(func() { convey.So(reopened.Close(), convey.ShouldBeNil) })

			client := &Client{cache: reopened, cacheReader: readDBFromCache(reopened)}
			match, resolveErr := client.ResolveSample(context.Background(), "Hek_R1")
			convey.So(resolveErr, convey.ShouldBeNil)
			convey.So(match.Kind, convey.ShouldEqual, KindSupplierName)
			convey.So(match.Canonical, convey.ShouldEqual, "7607STDY14643771")
		})

		convey.So(output, convey.ShouldEqual, "")
		convey.So(countRows(t, db, `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 1)
		convey.So(sampleMirrorIndexNames(t, db, "sqlite"), convey.ShouldResemble, sampleMirrorSecondaryIndexNames())
		convey.So(readSyncStateRow(t, db, syncTableSample).IndexesDropped, convey.ShouldEqual, 0)
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

		convey.Convey("when OpenCache runs, then it applies the migration and emits the same single stderr line", func() {
			convey.So(output, convey.ShouldEqual, fmt.Sprintf("mlwh cache: schema v1->v%d, recreated tables: [donor_samples, iseq_product_metrics_mirror, library_samples, sample_mirror, sample_search_token, seq_product_irods_locations_mirror, study_mirror]\n", CacheSchemaVersion))
			convey.So(rwMock.ExpectationsWereMet(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestAllowLargeMySQLColdLoadIndexShapeUsesDroppedSyncState(t *testing.T) {
	convey.Convey("Given a sparse large MySQL product mirror with dropped indexes recorded in sync_state", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		highWater := time.Date(2026, time.May, 13, 9, 0, 0, 0, time.UTC)
		expected := schemaShape{Index: map[string][]string{
			"iseq_product_metrics_mirror":        {"id_run,position,tag_index", "id_sample_tmp,id_run,position,tag_index", "id_iseq_flowcell_tmp", "id_study_lims,id_run,position"},
			"seq_product_irods_locations_mirror": {"id_sample_tmp", "id_study_lims,id_sample_tmp"},
			"sample_mirror":                      {"name"},
		}}
		actual := schemaShape{Index: map[string][]string{
			"iseq_product_metrics_mirror":        {},
			"seq_product_irods_locations_mirror": {"id_sample_tmp", "id_study_lims,id_sample_tmp"},
			"sample_mirror":                      {"name"},
		}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqProductMetrics).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "indexes_dropped"}).AddRow(formatSyncTime(highWater), 1))

		allowLargeMySQLColdLoadIndexShape(context.Background(), db, expected, actual)

		convey.So(actual.Index["iseq_product_metrics_mirror"], convey.ShouldResemble, expected.Index["iseq_product_metrics_mirror"])
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestAllowLargeMySQLColdLoadIndexShapeUsesMetadataEstimateWithoutCountingRows(t *testing.T) {
	convey.Convey("Given an existing sparse MySQL product mirror whose dropped-index flag is absent", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		expected := schemaShape{Index: map[string][]string{
			"iseq_product_metrics_mirror":        {"id_run,position,tag_index", "id_sample_tmp,id_run,position,tag_index", "id_iseq_flowcell_tmp", "id_study_lims,id_run,position"},
			"seq_product_irods_locations_mirror": {"id_sample_tmp", "id_study_lims,id_sample_tmp"},
		}}
		actual := schemaShape{Index: map[string][]string{
			"iseq_product_metrics_mirror":        {},
			"seq_product_irods_locations_mirror": {"id_sample_tmp", "id_study_lims,id_sample_tmp"},
		}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqProductMetrics).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT TABLE_ROWS FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`)).
			WithArgs("iseq_product_metrics_mirror").
			WillReturnRows(sqlmock.NewRows([]string{"TABLE_ROWS"}).AddRow(mysqlInlineMirrorIndexRowLimit + 1))

		allowLargeMySQLColdLoadIndexShape(context.Background(), db, expected, actual)

		convey.So(actual.Index["iseq_product_metrics_mirror"], convey.ShouldResemble, expected.Index["iseq_product_metrics_mirror"])
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
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

func TestClientSyncRejectsConcurrentSQLiteSyncForSameCache(t *testing.T) {
	convey.Convey("B6.1: Given a SQLite cache and two concurrent syncs against it, when both try to acquire the advisory lock, then the second fails immediately with ErrSyncAlreadyRunning", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")

		firstCache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(firstCache.Close(), convey.ShouldBeNil) })

		secondCache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(secondCache.Close(), convey.ShouldBeNil) })

		firstEntered := make(chan struct{}, 1)
		secondEntered := make(chan struct{}, 1)
		releaseFirst := make(chan struct{})

		firstClient := &Client{
			cache:       firstCache,
			cacheReader: readDBFromCache(firstCache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				firstEntered <- struct{}{}
				<-releaseFirst

				return nil
			},
		}
		secondClient := &Client{
			cache:       secondCache,
			cacheReader: readDBFromCache(secondCache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				secondEntered <- struct{}{}

				return nil
			},
		}

		firstErrCh := make(chan error, 1)
		go func() {
			_, syncErr := firstClient.Sync(context.Background())
			firstErrCh <- syncErr
		}()

		select {
		case <-firstEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for first sqlite sync to hold the lock")
		}

		secondCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()

		secondErrCh := make(chan error, 1)
		go func() {
			_, syncErr := secondClient.Sync(secondCtx)
			secondErrCh <- syncErr
		}()

		var secondErr error
		select {
		case secondErr = <-secondErrCh:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for second sqlite sync to fail")
		}

		convey.So(errors.Is(secondErr, ErrSyncAlreadyRunning), convey.ShouldBeTrue)
		convey.So(len(secondEntered), convey.ShouldEqual, 0)

		close(releaseFirst)
		convey.So(<-firstErrCh, convey.ShouldBeNil)
	})
}

func TestMySQLSyncLockNameUsesHostAndDatabaseOnly(t *testing.T) {
	convey.Convey("Given MySQL DSN variants that point at the same host and database", t, func() {
		lockNames := []string{
			mysqlSyncLockName("cache_user@tcp(localhost:3306)/wa_mlwh_cache"),
			mysqlSyncLockName("other_user@tcp(localhost:3306)/wa_mlwh_cache?parseTime=true"),
			mysqlSyncLockName("third_user@tcp(localhost:3306)/wa_mlwh_cache?multiStatements=true&readTimeout=1s"),
		}

		convey.Convey("when the sync lock name is derived, then username and query parameters do not change it", func() {
			convey.So(lockNames[1], convey.ShouldEqual, lockNames[0])
			convey.So(lockNames[2], convey.ShouldEqual, lockNames[0])
		})
	})
}

func TestClientSyncRejectsConcurrentMySQLSyncForSameCache(t *testing.T) {
	convey.Convey("B6.2: Given a MySQL cache and two concurrent syncs against the same DSN, when both try GET_LOCK with the derived per-cache name, then the second fails immediately with ErrSyncAlreadyRunning", t, func() {
		const cacheDSN = "cache_user@tcp(localhost:3306)/wa_mlwh_cache"
		lockName := mysqlSyncLockName(cacheDSN)

		firstRW, firstRWMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		secondRW, secondRWMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		firstLockDB, firstLockMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		secondLockDB, secondLockMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		firstRO, _, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		secondRO, _, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)

		convey.Reset(func() {
			_ = firstRW.Close()
			_ = secondRW.Close()
			_ = firstLockDB.Close()
			_ = secondLockDB.Close()
			_ = firstRO.Close()
			_ = secondRO.Close()
		})

		firstLockMock.ExpectQuery(regexp.QuoteMeta(`SELECT GET_LOCK(?, 0)`)).WithArgs(lockName).WillReturnRows(sqlmock.NewRows([]string{"got_lock"}).AddRow(1))
		firstLockMock.ExpectQuery(regexp.QuoteMeta(`SELECT RELEASE_LOCK(?)`)).WithArgs(lockName).WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))

		secondLockMock.ExpectQuery(regexp.QuoteMeta(`SELECT GET_LOCK(?, 0)`)).WithArgs(lockName).WillReturnRows(sqlmock.NewRows([]string{"got_lock"}).AddRow(0))

		firstClient := &Client{
			cache:       &mysqlCache{rwDB: firstRW, roDB: firstRO, lockDB: firstLockDB, lockDSN: cacheDSN},
			cacheReader: firstRO,
		}
		secondClient := &Client{
			cache:       &mysqlCache{rwDB: secondRW, roDB: secondRO, lockDB: secondLockDB, lockDSN: cacheDSN},
			cacheReader: secondRO,
		}

		firstRelease, err := firstClient.acquireSyncLock(context.Background())
		convey.So(err, convey.ShouldBeNil)
		convey.So(firstRelease, convey.ShouldNotBeNil)

		secondRelease, secondErr := secondClient.acquireSyncLock(context.Background())
		convey.So(secondRelease, convey.ShouldBeNil)
		convey.So(errors.Is(secondErr, ErrSyncAlreadyRunning), convey.ShouldBeTrue)

		convey.So(firstRelease(), convey.ShouldBeNil)
		convey.So(firstRWMock.ExpectationsWereMet(), convey.ShouldBeNil)
		convey.So(secondRWMock.ExpectationsWereMet(), convey.ShouldBeNil)
		convey.So(firstLockMock.ExpectationsWereMet(), convey.ShouldBeNil)
		convey.So(secondLockMock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestSQLiteSyncLockIsReleasedAfterKilledProcess(t *testing.T) {
	if os.Getenv("WA_TEST_SQLITE_SYNC_LOCK_HELPER") == "1" {
		cachePath := os.Getenv("WA_TEST_SQLITE_SYNC_LOCK_CACHE")
		readyPath := os.Getenv("WA_TEST_SQLITE_SYNC_LOCK_READY")

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		if err != nil {
			os.Exit(2)
		}

		client := &Client{
			cache:       cache,
			cacheReader: readDBFromCache(cache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				_ = os.WriteFile(readyPath, []byte("ready"), 0o600)

				select {}
			},
		}

		_, _ = client.Sync(context.Background())
		os.Exit(3)
	}

	convey.Convey("B6.3: Given a held SQLite sync lock and a killed lock-holder process, when a new sync starts, then it acquires the lock successfully", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")
		readyPath := filepath.Join(t.TempDir(), "ready")

		command := exec.Command(os.Args[0], "-test.run=^TestSQLiteSyncLockIsReleasedAfterKilledProcess$")
		command.Env = append(
			os.Environ(),
			"WA_TEST_SQLITE_SYNC_LOCK_HELPER=1",
			"WA_TEST_SQLITE_SYNC_LOCK_CACHE="+cachePath,
			"WA_TEST_SQLITE_SYNC_LOCK_READY="+readyPath,
		)

		convey.So(command.Start(), convey.ShouldBeNil)
		convey.Reset(func() {
			if command.Process != nil {
				_ = command.Process.Kill()
				_, _ = command.Process.Wait()
			}
		})

		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(readyPath); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for helper process to hold the sqlite sync lock")
			}

			time.Sleep(10 * time.Millisecond)
		}

		convey.So(command.Process.Signal(syscall.SIGKILL), convey.ShouldBeNil)
		_, _ = command.Process.Wait()

		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(cache.Close(), convey.ShouldBeNil) })

		client := &Client{
			cache:       cache,
			cacheReader: readDBFromCache(cache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				return nil
			},
		}

		_, err = client.Sync(context.Background())

		convey.So(err, convey.ShouldBeNil)
	})
}

func TestResolveSampleSucceedsWhileSQLiteSyncLockIsHeld(t *testing.T) {
	convey.Convey("B6.4: Given a read-only resolver call while a SQLite sync lock is held, when ResolveSample runs, then it succeeds and does not take the advisory lock", t, func() {
		cachePath := filepath.Join(t.TempDir(), "cache.sqlite")
		seedCache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.So(writeSyncState(context.Background(), seedCache.DB(), "sqlite", syncTableSample, time.Date(2026, time.May, 11, 20, 0, 0, 0, time.UTC), nil, false), convey.ShouldBeNil)
		_, err = seedCache.DB().Exec(`INSERT INTO sample_mirror(
			id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name,
			sanger_sample_id, supplier_name, accession_number, donor_id,
			taxon_id, common_name, description, last_updated
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			1, "SQSCP", "101", "sample-uuid-1", "DN1234",
			"DN1234", "supplier-1", "acc-1", "donor-1",
			9606, "human", "desc-1", formatSyncTime(time.Date(2026, time.May, 11, 20, 0, 0, 0, time.UTC)),
		)
		convey.So(err, convey.ShouldBeNil)
		convey.So(seedCache.Close(), convey.ShouldBeNil)

		lockCache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(lockCache.Close(), convey.ShouldBeNil) })

		readCache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(readCache.Close(), convey.ShouldBeNil) })

		lockEntered := make(chan struct{}, 1)
		releaseLock := make(chan struct{})
		lockClient := &Client{
			cache:       lockCache,
			cacheReader: readDBFromCache(lockCache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				lockEntered <- struct{}{}
				<-releaseLock

				return nil
			},
		}

		errCh := make(chan error, 1)
		go func() {
			_, syncErr := lockClient.Sync(context.Background())
			errCh <- syncErr
		}()

		select {
		case <-lockEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for sqlite sync lock to be acquired")
		}

		readClient := &Client{cache: readCache, cacheReader: readDBFromCache(readCache)}
		match, resolveErr := readClient.ResolveSample(context.Background(), "DN1234")

		convey.So(resolveErr, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindSangerSampleName)
		convey.So(match.Canonical, convey.ShouldEqual, "DN1234")
		convey.So(match.Sample, convey.ShouldNotBeNil)
		convey.So(match.Sample.Name, convey.ShouldEqual, "DN1234")

		close(releaseLock)
		convey.So(<-errCh, convey.ShouldBeNil)
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
		rwMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))
		expectNoDroppedMirrorRepairRows(rwMock, syncTableIseqProductMetrics)
		expectNoDroppedMirrorRepairRows(rwMock, syncTableSeqProductIRODSLocations)
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

func TestOpenCacheTrimsTrailingEnvSemicolonFromMySQLDSN(t *testing.T) {
	convey.Convey("Given a MySQL cache DSN copied from a dotenv line with a trailing semicolon", t, func() {
		originalOpen := sqlOpenFunc
		defer func() { sqlOpenFunc = originalOpen }()

		rwDB, rwMock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		convey.So(err, convey.ShouldBeNil)
		roDB, roMock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		convey.So(err, convey.ShouldBeNil)

		expectSchemaBootstrap(rwMock, "mysql")
		rwMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))
		expectNoDroppedMirrorRepairRows(rwMock, syncTableIseqProductMetrics)
		expectNoDroppedMirrorRepairRows(rwMock, syncTableSeqProductIRODSLocations)
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

		_, err = OpenCache(context.Background(), CacheConfig{Path: "cache_user@tcp(localhost:3306)/wa_cache;?parseTime=true", Password: "secret"})

		convey.Convey("when OpenCache runs, then both MySQL connections use the normalized database name", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(opened, convey.ShouldHaveLength, 2)

			rwCfg, parseErr := mysql.ParseDSN(opened[0])
			convey.So(parseErr, convey.ShouldBeNil)
			roCfg, parseErr := mysql.ParseDSN(opened[1])
			convey.So(parseErr, convey.ShouldBeNil)

			convey.So(rwCfg.DBName, convey.ShouldEqual, "wa_cache")
			convey.So(roCfg.DBName, convey.ShouldEqual, "wa_cache")
			convey.So(rwCfg.ParseTime, convey.ShouldBeTrue)
			convey.So(roCfg.ParseTime, convey.ShouldBeTrue)
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

func TestSampleSyncPopulatesSQLiteSampleSearchTokenIndex(t *testing.T) {
	convey.Convey("Given a populated SQLite sample_mirror and a cold-load sync that builds the token index", t, func() {
		cache, _ := openCountingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		rows := sampleSyncRowsForRange(1, 3, time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC), nil)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {columns: sampleSyncSourceColumns, rows: rows},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		report, sawRows, err := client.syncTableData(context.Background(), syncTableSample, syncStateRecord{})
		convey.So(err, convey.ShouldBeNil)
		convey.So(sawRows, convey.ShouldBeTrue)
		convey.So(report.Inserted, convey.ShouldEqual, 3)

		convey.Convey("when a sample's name token is searched by prefix, then its id is returned (the token index is populated, not empty)", func() {
			var name string
			convey.So(cache.DB().QueryRow(`SELECT name FROM sample_mirror WHERE id_sample_tmp = 1`).Scan(&name), convey.ShouldBeNil)
			convey.So(len(name), convey.ShouldBeGreaterThanOrEqualTo, 3)

			tokens := sampleSearchTokens(name)
			convey.So(len(tokens), convey.ShouldBeGreaterThanOrEqualTo, 1)

			var id int64
			convey.So(cache.DB().QueryRow(
				`SELECT id_sample_tmp FROM sample_search_token WHERE token = ? AND id_sample_tmp = 1`,
				tokens[0],
			).Scan(&id), convey.ShouldBeNil)
			convey.So(id, convey.ShouldEqual, 1)

			var distinctSamples int
			convey.So(cache.DB().QueryRow(`SELECT COUNT(DISTINCT id_sample_tmp) FROM sample_search_token`).Scan(&distinctSamples), convey.ShouldBeNil)
			convey.So(distinctSamples, convey.ShouldEqual, 3)
		})
	})
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
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
		WithArgs(syncTableSample).
		WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))
	expectNoDroppedMirrorRepairRows(mock, syncTableIseqProductMetrics)
	expectNoDroppedMirrorRepairRows(mock, syncTableSeqProductIRODSLocations)
}

func expectNoDroppedMirrorRepairRows(mock sqlmock.Sqlmock, table string) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
		WithArgs(table).
		WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))
}
