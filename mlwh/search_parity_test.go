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
	"slices"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// searchParityTerm is the ASCII-only substring term the parity tests search
// for. ASCII is deliberate: accent handling is a documented backend divergence
// (MySQL utf8mb4_0900_ai_ci is accent-insensitive, SQLite trigram is
// accent-sensitive), so cross-dialect set-equality is only guaranteed for ASCII
// fixtures.
const searchParityTerm = "acme"

// searchParityStudyTerm is the ASCII-only study substring term; the seeded
// study fixtures contain two studies whose title contains it and one that does
// not.
const searchParityStudyTerm = "malar"

// searchParityDeadline bounds the per-backend cache work so a misconfigured or
// unreachable MySQL host (reached via the writable-cache gate) cannot hang the
// test beyond the outer `go test` timeout.
const searchParityDeadline = 60 * time.Second

// B3.1 / B3.3: identical ASCII sample fixtures in SQLite and (gated) MySQL
// return equal id_sample_tmp sets for the same term; the SQLite assertions run
// regardless, and the MySQL half is skipped when no writable MySQL cache is
// configured.
func TestSearchParitySampleIDSetsEqualAcrossDialects(t *testing.T) {
	convey.Convey("Given identical ASCII sample fixtures in a SQLite cache", t, func() {
		ctx, cancel := context.WithTimeout(context.Background(), searchParityDeadline)
		defer cancel()

		sqliteClient := newSeededSearchParitySQLiteClient(t)

		sqliteSamples, err := sqliteClient.SearchSamples(ctx, searchParityTerm, 1000, 0)
		convey.So(err, convey.ShouldBeNil)

		sqliteSampleIDs := sortedInt64s(sampleTmpIDs(sqliteSamples))

		convey.Convey("then SQLite returns exactly the two ACME samples by id_sample_tmp", func() {
			convey.So(sqliteSampleIDs, convey.ShouldResemble, []int64{1, 2})
		})

		convey.Convey("and when the same fixtures are searched in a writable MySQL cache", func() {
			mysqlClient, skip := newSeededSearchParityMySQLClient(t)
			if skip != "" {
				t.Skip(skip)
			}

			mysqlSamples, mysqlErr := mysqlClient.SearchSamples(ctx, searchParityTerm, 1000, 0)
			convey.So(mysqlErr, convey.ShouldBeNil)

			convey.Convey("then the returned id_sample_tmp sets are equal across backends", func() {
				convey.So(sortedInt64s(sampleTmpIDs(mysqlSamples)), convey.ShouldResemble, sqliteSampleIDs)
			})
		})
	})
}

// B3.2 / B3.3: CountSampleSearch returns equal Count.Count across SQLite and
// (gated) MySQL, and each backend's count equals its own returned row-set size;
// the SQLite assertions run regardless and the MySQL half skips when no writable
// MySQL cache is configured.
func TestSearchParitySampleCountsEqualAcrossDialects(t *testing.T) {
	convey.Convey("Given identical ASCII sample fixtures in a SQLite cache", t, func() {
		ctx, cancel := context.WithTimeout(context.Background(), searchParityDeadline)
		defer cancel()

		sqliteClient := newSeededSearchParitySQLiteClient(t)

		sqliteCount, err := sqliteClient.CountSampleSearch(ctx, searchParityTerm)
		convey.So(err, convey.ShouldBeNil)

		sqliteSamples, err := sqliteClient.SearchSamples(ctx, searchParityTerm, 1000, 0)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("then the SQLite count equals its row-set size", func() {
			convey.So(sqliteCount, convey.ShouldResemble, Count{Count: 2})
			convey.So(sqliteCount.Count, convey.ShouldEqual, len(sqliteSamples))
		})

		convey.Convey("and when the same fixtures are counted in a writable MySQL cache", func() {
			mysqlClient, skip := newSeededSearchParityMySQLClient(t)
			if skip != "" {
				t.Skip(skip)
			}

			mysqlCount, mysqlErr := mysqlClient.CountSampleSearch(ctx, searchParityTerm)
			convey.So(mysqlErr, convey.ShouldBeNil)

			mysqlSamples, mysqlErr := mysqlClient.SearchSamples(ctx, searchParityTerm, 1000, 0)
			convey.So(mysqlErr, convey.ShouldBeNil)

			convey.Convey("then both counts are equal and equal to the row-set size", func() {
				convey.So(mysqlCount.Count, convey.ShouldEqual, sqliteCount.Count)
				convey.So(mysqlCount.Count, convey.ShouldEqual, len(mysqlSamples))
			})
		})
	})
}

// B3.1 / B3.2 / B3.3 for studies: identical ASCII study fixtures return equal
// id_study_tmp sets and equal CountStudySearch counts across SQLite and (gated)
// MySQL. Study search shares the same exact-substring LIKE contract as sample
// search, so the same set-equality holds; the MySQL half skips when no writable
// MySQL cache is configured.
func TestSearchParityStudyIDSetsAndCountsEqualAcrossDialects(t *testing.T) {
	convey.Convey("Given identical ASCII study fixtures in a SQLite cache", t, func() {
		ctx, cancel := context.WithTimeout(context.Background(), searchParityDeadline)
		defer cancel()

		sqliteClient := newSeededSearchParitySQLiteClient(t)

		sqliteStudies, err := sqliteClient.SearchStudies(ctx, searchParityStudyTerm, 1000, 0)
		convey.So(err, convey.ShouldBeNil)

		sqliteStudyIDs := sortedInt64s(studyTmpIDs(sqliteStudies))

		sqliteCount, err := sqliteClient.CountStudySearch(ctx, searchParityStudyTerm)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("then SQLite returns the two malaria studies and a matching count", func() {
			convey.So(sqliteStudyIDs, convey.ShouldResemble, []int64{1, 2})
			convey.So(sqliteCount, convey.ShouldResemble, Count{Count: 2})
			convey.So(sqliteCount.Count, convey.ShouldEqual, len(sqliteStudies))
		})

		convey.Convey("and when the same fixtures are searched in a writable MySQL cache", func() {
			mysqlClient, skip := newSeededSearchParityMySQLClient(t)
			if skip != "" {
				t.Skip(skip)
			}

			mysqlStudies, mysqlErr := mysqlClient.SearchStudies(ctx, searchParityStudyTerm, 1000, 0)
			convey.So(mysqlErr, convey.ShouldBeNil)

			mysqlCount, mysqlErr := mysqlClient.CountStudySearch(ctx, searchParityStudyTerm)
			convey.So(mysqlErr, convey.ShouldBeNil)

			convey.Convey("then the id_study_tmp sets and the counts are equal across backends", func() {
				convey.So(sortedInt64s(studyTmpIDs(mysqlStudies)), convey.ShouldResemble, sqliteStudyIDs)
				convey.So(mysqlCount.Count, convey.ShouldEqual, sqliteCount.Count)
				convey.So(mysqlCount.Count, convey.ShouldEqual, len(mysqlStudies))
			})
		})
	})
}

// newSeededSearchParitySQLiteClient opens a SQLite cache, seeds the shared ASCII
// study/sample parity fixtures into it, and returns a Client reading from it.
func newSeededSearchParitySQLiteClient(t *testing.T) *Client {
	t.Helper()

	cache := openSQLiteSyncTestCache(t)
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Errorf("cache.Close(): %v", err)
		}
	})

	seedSearchParityFixtures(t, cache)

	return &Client{cache: cache, cacheReader: cacheReadDB(cache)}
}

// sortedInt64s returns a sorted copy of ids so set-equality is asserted
// independently of row order.
func sortedInt64s(ids []int64) []int64 {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)

	return sorted
}

// studyTmpIDs projects the id_study_tmp of each study, the stable per-row
// identifier the cross-dialect study set-equality is asserted on.
func studyTmpIDs(studies []Study) []int64 {
	ids := make([]int64, len(studies))
	for index, study := range studies {
		ids[index] = study.IDStudyTmp
	}

	return ids
}

// newSeededSearchParityMySQLClient opens a fresh, writable MySQL cache via the
// package's WA_MLWH_CACHE_PATH gate (never the read-only WA_MLWH_DSN upstream
// warehouse), seeds the same ASCII fixtures into it, and returns a Client
// reading from it. When no writable MySQL cache is configured it returns a
// non-empty skip reason and a nil Client so the caller can t.Skip the MySQL
// half while the SQLite assertions still run.
func newSeededSearchParityMySQLClient(t *testing.T) (*Client, string) {
	t.Helper()

	cfg, skipReason := loadMySQLCacheConfigForTest(t)
	if skipReason != "" {
		return nil, skipReason
	}

	cache := openMySQLCacheForTest(t, cfg)

	seedSearchParityFixtures(t, cache)

	return &Client{cache: cache, cacheReader: cacheReadDB(cache)}, ""
}

// seedSearchParityFixtures seeds identical ASCII-only study and sample rows into
// the given cache and marks the study/sample sync state present, so the same
// fixtures back both the SQLite and MySQL halves of the parity assertions. The
// SQLite external-content FTS5 table is rebuilt from sample_mirror (the MySQL
// FULLTEXT index is maintained automatically by the inserts), and the sync
// state is written in the cache's own dialect.
func seedSearchParityFixtures(t *testing.T, cache Cache) {
	t.Helper()

	db := cache.DB()

	// Two studies whose title contains the ASCII term "malar" and one that does
	// not, so the study match set is non-trivial across dialects.
	seedStudyMirrorSearchRow(t, db, 1, "6568", "study-a", "Malaria genomics", "Genomics", "Sponsor A")
	seedStudyMirrorSearchRow(t, db, 2, "6566", "study-b", "Malaria vaccine", "Vaccines", "Sponsor B")
	seedStudyMirrorSearchRow(t, db, 3, "6567", "study-c", "Cancer atlas", "Oncology", "Sponsor C")

	// Two samples whose supplier_name contains the ASCII term "acme", one that
	// matches only via a different searchable field, and two that do not match
	// at all, so the sample match set is non-trivial and exercises the LIKE
	// post-filter shared by both dialects.
	seedSampleMirrorSearchRow(t, db, 1, "name-a", "ACME-001", "common-a", "donor-a")
	seedSampleMirrorSearchRow(t, db, 2, "name-b", "ACME-002", "common-b", "donor-b")
	seedSampleMirrorSearchRow(t, db, 3, "name-c", "OTHER-1", "common-c", "donor-c")
	seedSampleMirrorSearchRow(t, db, 4, "name-d", "supplier-d", "common-d", "donor-d")

	if cache.Dialect() != "mysql" {
		rebuildSampleSearchIndexForTest(t, db)
	}

	seedSearchParitySyncState(t, db, cache.Dialect(), syncTableStudy)
	seedSearchParitySyncState(t, db, cache.Dialect(), syncTableSample)
}

// seedSearchParitySyncState marks a sync table as synced in the given dialect so
// the search/count methods do not short-circuit to their never-synced error on a
// populated cache. It writes through the dialect-aware sync-state writer (the
// shared seedSyncState helper is SQLite-only).
func seedSearchParitySyncState(t *testing.T, db *sql.DB, dialect, table string) {
	t.Helper()

	highWater := time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC)
	if err := writeSyncState(context.Background(), db, dialect, table, highWater, nil, false); err != nil {
		t.Fatalf("seedSearchParitySyncState(%s, %s): %v", dialect, table, err)
	}
}
