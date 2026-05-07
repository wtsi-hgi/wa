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
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/smartystreets/goconvey/convey"
)

func TestAllStudiesWarmCacheReturnsOrderedStudiesWithoutMLWHQuery(t *testing.T) {
	convey.Convey("Given a warm cache mirror containing three SQSCP studies", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 31, "6568", "study-uuid-31", "Study 31", "EGAS00001003131")
		seedStudyMirrorRow(t, cache.DB(), 32, "6566", "study-uuid-32", "Study 32", "EGAS00001003232")
		seedStudyMirrorRow(t, cache.DB(), 33, "6567", "study-uuid-33", "Study 33", "EGAS00001003333")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			sourceMock.ExpectClose()
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then it returns the cache rows ordered by id_study_lims", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 3)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "6566")
			convey.So(studies[1].IDStudyLims, convey.ShouldEqual, "6567")
			convey.So(studies[2].IDStudyLims, convey.ShouldEqual, "6568")
		})
	})
}

func TestAllStudiesColdCacheReadThroughDoesNotAdvanceWatermark(t *testing.T) {
	convey.Convey("Given a cold cache and an MLWH source returning two studies", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			sourceMock.ExpectClose()
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		t1 := time.Date(2026, time.May, 6, 17, 10, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)
		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT `+studySelectColumns+`, last_updated FROM study WHERE id_lims = 'SQSCP' ORDER BY id_study_lims LIMIT ? OFFSET ?`)).
			WithArgs(100, 0).
			WillReturnRows(sqlmock.NewRows(studySyncSourceColumns).
				AddRow(studyRowValues(41, "SQSCP", "6566", "study-uuid-41", "Study 41", "EGAS00001004141", t1)...).
				AddRow(studyRowValues(42, "SQSCP", "6567", "study-uuid-42", "Study 42", "EGAS00001004242", t2)...))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then it reads through into study_mirror without writing sync_state", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 2)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "6566")
			convey.So(studies[1].IDStudyLims, convey.ShouldEqual, "6567")
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sync_state WHERE table_name = ?`, syncTableStudy), convey.ShouldEqual, 0)
		})
	})
}

func TestAllStudiesWarmCacheIncludesRowInsertedBySync(t *testing.T) {
	convey.Convey("Given a warm cache where Sync has just advanced the study watermark", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 6, 17, 20, 0, 0, time.UTC)
		t3 := t1.Add(20 * time.Minute)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			sourceMock.ExpectClose()
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, egadac_accession_number, ega_policy_accession_number, data_release_timing, last_updated FROM study WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_study_tmp`)).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows(studySyncSourceColumns).
				AddRow(studyRowValues(51, "SQSCP", "6566", "study-uuid-51", "Study 51", "EGAS00001005151", t1)...).
				AddRow(studyRowValues(52, "SQSCP", "6569", "study-uuid-52", "Study 52", "EGAS00001005252", t3)...))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		_, err = client.Sync(context.Background(), syncTableStudy)
		convey.So(err, convey.ShouldBeNil)

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then it includes the row committed by Sync", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 2)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "6566")
			convey.So(studies[1].IDStudyLims, convey.ShouldEqual, "6569")
			convey.So(readSyncHighWater(t, cache.DB(), syncTableStudy), convey.ShouldHappenOnOrBetween, t3, t3)
		})
	})
}

func TestAllStudiesWarmCacheHonoursLimitAndOffset(t *testing.T) {
	convey.Convey("Given a five-row warm cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 61, "6565", "study-uuid-61", "Study 61", "EGAS00001006161")
		seedStudyMirrorRow(t, cache.DB(), 62, "6566", "study-uuid-62", "Study 62", "EGAS00001006262")
		seedStudyMirrorRow(t, cache.DB(), 63, "6567", "study-uuid-63", "Study 63", "EGAS00001006363")
		seedStudyMirrorRow(t, cache.DB(), 64, "6568", "study-uuid-64", "Study 64", "EGAS00001006464")
		seedStudyMirrorRow(t, cache.DB(), 65, "6569", "study-uuid-65", "Study 65", "EGAS00001006565")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 45, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		studies, err := client.AllStudies(context.Background(), 2, 1)

		convey.Convey("when AllStudies runs, then it returns exactly the requested page", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 2)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "6566")
			convey.So(studies[1].IDStudyLims, convey.ShouldEqual, "6567")
		})
	})
}

func TestAllStudiesWarmCacheFiltersOutNonSQSCPRows(t *testing.T) {
	convey.Convey("Given a warm cache with SQSCP and non-SQSCP study rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 71, "6566", "study-uuid-71", "Study 71", "EGAS00001007171")
		seedStudyMirrorRow(t, cache.DB(), 72, "6567", "study-uuid-72", "Study 72", "EGAS00001007272")
		_, err := cache.DB().Exec(
			`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, egadac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			73,
			"GCLP",
			"9999",
			"study-uuid-73",
			"Study 73",
			"EGAS00001007373",
			"Study title 9999",
			"Faculty sponsor 9999",
			"active",
			"abstract",
			"abbr",
			"description",
			"strategy",
			"group",
			"hmdmc",
			"programme",
			"2026-05-06",
			"GRCh38",
			true,
			"study-type",
			false,
			false,
			"public",
			"EGAD0001",
			"EGAP0001",
			"immediate",
			formatSyncTime(time.Date(2026, time.May, 6, 18, 0, 0, 0, time.UTC)),
		)
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 18, 1, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then only SQSCP rows are returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 2)
			convey.So(studies[0].IDLims, convey.ShouldEqual, "SQSCP")
			convey.So(studies[1].IDLims, convey.ShouldEqual, "SQSCP")
		})
	})
}

func TestAllStudiesColdCacheSecondCallHitsStudyMirrorOnly(t *testing.T) {
	convey.Convey("Given a cold cache and a first read-through call", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			sourceMock.ExpectClose()
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		t1 := time.Date(2026, time.May, 6, 18, 10, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)
		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT `+studySelectColumns+`, last_updated FROM study WHERE id_lims = 'SQSCP' ORDER BY id_study_lims LIMIT ? OFFSET ?`)).
			WithArgs(100, 0).
			WillReturnRows(sqlmock.NewRows(studySyncSourceColumns).
				AddRow(studyRowValues(81, "SQSCP", "6566", "study-uuid-81", "Study 81", "EGAS00001008181", t1)...).
				AddRow(studyRowValues(82, "SQSCP", "6567", "study-uuid-82", "Study 82", "EGAS00001008282", t2)...))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		first, err := client.AllStudies(context.Background(), 100, 0)
		convey.So(err, convey.ShouldBeNil)

		second, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies repeats, then the second call is served from study_mirror without a second MLWH query", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(first, convey.ShouldHaveLength, 2)
			convey.So(second, convey.ShouldResemble, first)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sync_state WHERE table_name = ?`, syncTableStudy), convey.ShouldEqual, 0)
		})
	})
}
