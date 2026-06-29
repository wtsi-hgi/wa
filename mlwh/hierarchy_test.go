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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/smartystreets/goconvey/convey"
)

var (
	samplesForStudyCacheQuery  = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_study_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForStudyParentQuery = `SELECT 1 FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`
	sampleFanOutOneIDQuery     = `SELECT library_samples.id_sample_tmp, library_samples.pipeline_id_lims, library_samples.library_id, library_samples.id_library_lims, ` + qualifiedStudyMirrorSelectSQL + `
		 FROM library_samples
		 INNER JOIN study_mirror ON study_mirror.id_study_lims = library_samples.id_study_lims
		 WHERE study_mirror.id_lims = 'SQSCP' AND library_samples.id_sample_tmp IN (?)
		 ORDER BY library_samples.id_sample_tmp, study_mirror.id_study_lims, library_samples.pipeline_id_lims`
	sampleFanOutThreeIDsQuery = `SELECT library_samples.id_sample_tmp, library_samples.pipeline_id_lims, library_samples.library_id, library_samples.id_library_lims, ` + qualifiedStudyMirrorSelectSQL + `
		 FROM library_samples
		 INNER JOIN study_mirror ON study_mirror.id_study_lims = library_samples.id_study_lims
		 WHERE study_mirror.id_lims = 'SQSCP' AND library_samples.id_sample_tmp IN (?,?,?)
		 ORDER BY library_samples.id_sample_tmp, study_mirror.id_study_lims, library_samples.pipeline_id_lims`
	samplesForLibraryCacheQuery       = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND library_samples.id_study_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForLibraryStudyParentQuery = `SELECT 1 FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`
)

const (
	runsForStudyQuery        = `SELECT DISTINCT id_run FROM iseq_product_metrics_mirror WHERE id_study_lims = ? ORDER BY id_run LIMIT ? OFFSET ?`
	lanesForSampleQuery      = `SELECT DISTINCT id_run, position, tag_index FROM iseq_product_metrics_mirror WHERE id_sample_tmp = ? ORDER BY id_run, position, tag_index LIMIT ? OFFSET ?`
	lanesForSampleStudyQuery = `SELECT DISTINCT id_run, position, tag_index FROM iseq_product_metrics_mirror WHERE id_sample_tmp = ? AND id_study_lims = ? ORDER BY id_run, position, tag_index LIMIT ? OFFSET ?`
	irodsPathsForSampleQuery = `SELECT DISTINCT id_iseq_product, irods_collection, irods_file_name FROM seq_product_irods_locations_mirror WHERE id_sample_tmp = ? ORDER BY id_iseq_product LIMIT ? OFFSET ?`
	irodsPathsForStudyQuery  = `SELECT DISTINCT id_iseq_product, irods_collection, irods_file_name FROM seq_product_irods_locations_mirror WHERE id_study_lims = ? ORDER BY id_iseq_product LIMIT ? OFFSET ?`
)

func TestSamplesForStudyWarmCacheUsesJoinOnly(t *testing.T) {
	convey.Convey("Given a warm cache for a study with three linked samples", t, func() {
		client, roMock, _, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).
			WithArgs("6568", 100, 0).
			WillReturnRows(
				sqlmock.NewRows(sampleResolverColumns()).
					AddRow(sampleResolverRow(3, "sample-uuid-3", "503", "Alpha", "sanger-id-3", "supplier-3", "accession-3", "donor-3")...).
					AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "Beta", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...).
					AddRow(sampleResolverRow(2, "sample-uuid-2", "502", "Gamma", "sanger-id-2", "supplier-2", "accession-2", "donor-2")...),
			)
		roMock.ExpectQuery(regexp.QuoteMeta(sampleFanOutThreeIDsQuery)).
			WithArgs(int64(3), int64(1), int64(2)).
			WillReturnRows(
				sqlmock.NewRows([]string{
					"id_sample_tmp", "pipeline_id_lims", "library_id", "id_library_lims",
					"study_mirror.id_study_tmp", "study_mirror.id_lims", "study_mirror.id_study_lims", "study_mirror.uuid_study_lims", "study_mirror.name", "study_mirror.accession_number", "study_mirror.study_title", "study_mirror.faculty_sponsor", "study_mirror.state", "study_mirror.data_release_strategy", "study_mirror.data_access_group", "study_mirror.programme", "study_mirror.reference_genome", "study_mirror.ethically_approved", "study_mirror.study_type", "study_mirror.contains_human_dna", "study_mirror.contaminated_human_dna", "study_mirror.study_visibility", "study_mirror.ega_dac_accession_number", "study_mirror.ega_policy_accession_number", "study_mirror.data_release_timing",
				}).
					AddRow(int64(1), "lib-1", "", "", int64(11), "SQSCP", "6568", "study-uuid-1", "Study A", "ERP1", "title-1", "sponsor-1", "active", "open", "group-1", "programme-1", "GRCh38", true, "type-1", true, false, "public", "DAC1", "POL1", "immediate").
					AddRow(int64(2), "lib-2", "", "", int64(12), "SQSCP", "6568", "study-uuid-1", "Study A", "ERP1", "title-1", "sponsor-1", "active", "open", "group-1", "programme-1", "GRCh38", true, "type-1", true, false, "public", "DAC1", "POL1", "immediate").
					AddRow(int64(3), "lib-3", "", "", int64(13), "SQSCP", "6568", "study-uuid-1", "Study A", "ERP1", "title-1", "sponsor-1", "active", "open", "group-1", "programme-1", "GRCh38", true, "type-1", true, false, "public", "DAC1", "POL1", "immediate"),
			)

		samples, err := client.SamplesForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 3)
		convey.So(samples[0].Name, convey.ShouldEqual, "Alpha")
		convey.So(samples[1].Name, convey.ShouldEqual, "Beta")
		convey.So(samples[2].Name, convey.ShouldEqual, "Gamma")
	})
}

func TestSamplesForStudyColdCacheReturnsErrCacheNeverSynced(t *testing.T) {
	convey.Convey("Given a cold cache with no study sync state", t, func() {
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()
		_ = sourceMock

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).
			WithArgs("6568", 100, 0).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableStudy).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))

		samples, err := client.SamplesForStudy(context.Background(), "6568", 100, 0)

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(samples, convey.ShouldResemble, []Sample{})
	})
}

func TestSamplesForStudyWarmCacheReturnsEmptySliceWhenStudyHasNoChildren(t *testing.T) {
	convey.Convey("Given a warm cache with a known study but no linked samples", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 11, "6568", "study-uuid-11", "Study 11", "EGAS00001001111")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 16, 30, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 16, 31, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 16, 32, 0, 0, time.UTC))

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			sourceMock.ExpectClose()
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		samples, err := client.SamplesForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 0)
	})
}

func TestSearchValuesAPIShape(t *testing.T) {
	convey.Convey("Given the ExpandSearchValues API", t, func() {
		var expand = (*Client).ExpandSearchValues

		encoded, err := json.Marshal(SearchValues{
			Samples: []string{"sample"},
			Runs:    []string{"100"},
			Lanes:   []string{"100_1#1"},
		})

		convey.So(expand, convey.ShouldNotBeNil)
		convey.So(err, convey.ShouldBeNil)
		convey.So(string(encoded), convey.ShouldEqual, `{"samples":["sample"],"runs":["100"],"lanes":["100_1#1"]}`)
	})
}

func TestSamplesForRunUsesDistinctMetricsJoin(t *testing.T) {
	convey.Convey("Given a run present in iseq_product_metrics", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedSyncState(t, client.cache.DB(), syncTableIseqProductMetrics, time.Date(2026, time.May, 6, 12, 30, 0, 0, time.UTC))
		seedHierarchySample(t, client.cache.DB(), 1, "6568", "Alpha")
		seedHierarchySample(t, client.cache.DB(), 2, "6568", "Beta")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 1001, 1, 12345, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 1002, 2, 12345, 2, 0, "6568")

		samples, err := client.SamplesForRun(context.Background(), "12345", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 2)
		convey.So(samples[0].Name, convey.ShouldEqual, "Alpha")
		convey.So(samples[1].Name, convey.ShouldEqual, "Beta")
	})
}

func TestSamplesForRunHonoursLimitOffset(t *testing.T) {
	convey.Convey("Given a five-row run source queried with limit and offset", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedSyncState(t, client.cache.DB(), syncTableIseqProductMetrics, time.Date(2026, time.May, 6, 12, 31, 0, 0, time.UTC))
		seedHierarchySample(t, client.cache.DB(), 1, "6568", "Alpha")
		seedHierarchySample(t, client.cache.DB(), 2, "6568", "Beta")
		seedHierarchySample(t, client.cache.DB(), 3, "6568", "Gamma")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 1101, 1, 12345, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 1102, 2, 12345, 2, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 1103, 3, 12345, 3, 0, "6568")

		samples, err := client.SamplesForRun(context.Background(), "12345", 2, 1)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 2)
		convey.So(samples[0].Name, convey.ShouldEqual, "Beta")
		convey.So(samples[1].Name, convey.ShouldEqual, "Gamma")
	})
}

func TestSamplesForLibraryWarmCacheUsesLibrarySamplesJoinOnly(t *testing.T) {
	convey.Convey("Given a warm cache for a study-scoped library", t, func() {
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryCacheQuery)).
			WithArgs("Standard", "6568", 1, 0).
			WillReturnRows(
				sqlmock.NewRows(sampleResolverColumns()).
					AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "Alpha", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...),
			)
		roMock.ExpectQuery(regexp.QuoteMeta(sampleFanOutOneIDQuery)).
			WithArgs(int64(1)).
			WillReturnRows(
				sqlmock.NewRows([]string{
					"id_sample_tmp", "pipeline_id_lims", "library_id", "id_library_lims",
					"study_mirror.id_study_tmp", "study_mirror.id_lims", "study_mirror.id_study_lims", "study_mirror.uuid_study_lims", "study_mirror.name", "study_mirror.accession_number", "study_mirror.study_title", "study_mirror.faculty_sponsor", "study_mirror.state", "study_mirror.data_release_strategy", "study_mirror.data_access_group", "study_mirror.programme", "study_mirror.reference_genome", "study_mirror.ethically_approved", "study_mirror.study_type", "study_mirror.contains_human_dna", "study_mirror.contaminated_human_dna", "study_mirror.study_visibility", "study_mirror.ega_dac_accession_number", "study_mirror.ega_policy_accession_number", "study_mirror.data_release_timing",
				}).
					AddRow(int64(1), "Standard", "", "", int64(11), "SQSCP", "6568", "study-uuid-1", "Study A", "ERP1", "title-1", "sponsor-1", "active", "open", "group-1", "programme-1", "GRCh38", true, "type-1", true, false, "public", "DAC1", "POL1", "immediate"),
			)

		samples, err := client.SamplesForLibrary(context.Background(), "Standard", "6568", 1, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].Name, convey.ShouldEqual, "Alpha")
		convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestSamplesForStudyAndLibraryColdCacheReturnErrCacheNeverSynced(t *testing.T) {
	convey.Convey("Given a cold cache without study sync state", t, func() {
		studyClient, studyROMock, studySourceMock, studyCleanup := newMySQLResolverTestClient(t)
		defer studyCleanup()
		_ = studySourceMock

		studyROMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).
			WithArgs("6568", 2, 1).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		studyROMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}).AddRow(1))
		studyROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))
		studyROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqFlowcell).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))

		studySamples, studyErr := studyClient.SamplesForStudy(context.Background(), "6568", 2, 1)

		convey.So(errors.Is(studyErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(studyErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(studySamples, convey.ShouldResemble, []Sample{})

		libraryClient, libraryROMock, librarySourceMock, libraryCleanup := newMySQLResolverTestClient(t)
		defer libraryCleanup()
		_ = librarySourceMock

		libraryROMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryCacheQuery)).
			WithArgs("Standard", "6568", 2, 1).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}).AddRow(1))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqFlowcell).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))

		librarySamples, libraryErr := libraryClient.SamplesForLibrary(context.Background(), "Standard", "6568", 2, 1)

		convey.So(errors.Is(libraryErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(libraryErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(librarySamples, convey.ShouldResemble, []Sample{})
	})
}

func TestSamplesForLibraryColdCacheWithoutDependentSyncStateReturnsErrCacheNeverSynced(t *testing.T) {
	convey.Convey("Given a known study but no dependent sample or flowcell sync state", t, func() {
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()
		_ = sourceMock

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryCacheQuery)).
			WithArgs("Standard", "6568", 2, 1).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}).AddRow(1))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqFlowcell).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))

		samples, err := client.SamplesForLibrary(context.Background(), "Standard", "6568", 2, 1)

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(samples, convey.ShouldResemble, []Sample{})
	})
}

func TestSamplesForLibraryTypeColdCacheReturnsErrCacheNeverSynced(t *testing.T) {
	convey.Convey("Given a cold cache and no flowcell sync state", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		samples, err := client.SamplesForLibraryType(context.Background(), "Chromium single cell 3 prime v3", 100, 0)

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(samples, convey.ShouldResemble, []Sample{})
	})
}

func TestSampleListMethodsReturnErrCacheNeverSyncedWhenDependentSyncStateIsPartial(t *testing.T) {
	convey.Convey("Given required sample fan-out tables where one sync_state row is present and another is absent", t, func() {
		studyClient, studyROMock, _, studyCleanup := newMySQLResolverTestClient(t)
		defer studyCleanup()

		studyROMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).
			WithArgs("6568", 2, 1).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		studyROMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}).AddRow(1))
		studyROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow("2026-05-06T12:32:00Z", nil, 0))
		studyROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqFlowcell).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))

		studySamples, studyErr := studyClient.SamplesForStudy(context.Background(), "6568", 2, 1)

		convey.So(errors.Is(studyErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(studySamples, convey.ShouldResemble, []Sample{})

		libraryClient, libraryROMock, _, libraryCleanup := newMySQLResolverTestClient(t)
		defer libraryCleanup()

		libraryROMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryCacheQuery)).
			WithArgs("Standard", "6568", 2, 1).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}).AddRow(1))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow("2026-05-06T12:32:00Z", nil, 0))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqFlowcell).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))

		librarySamples, libraryErr := libraryClient.SamplesForLibrary(context.Background(), "Standard", "6568", 2, 1)

		convey.So(errors.Is(libraryErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(librarySamples, convey.ShouldResemble, []Sample{})

		libraryTypeClient, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()
		seedSyncState(t, libraryTypeClient.cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 20, 0, 0, 0, time.UTC))

		libraryTypeSamples, libraryTypeErr := libraryTypeClient.SamplesForLibraryType(context.Background(), "Chromium single cell 3 prime v3", 100, 0)

		convey.So(errors.Is(libraryTypeErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(libraryTypeSamples, convey.ShouldResemble, []Sample{})
	})
}

func TestHierarchyListMethodsReturnEmptySlicesForNeverSyncedCaches(t *testing.T) {
	convey.Convey("Given a never-synced cache", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		runSamples, runErr := client.SamplesForRun(context.Background(), "12345", 100, 0)
		libraries, librariesErr := client.LibrariesForStudy(context.Background(), "6568", 100, 0)
		runs, runsErr := client.RunsForStudy(context.Background(), "6568", 100, 0)
		lanes, lanesErr := client.LanesForSample(context.Background(), "7607STDY14643771", 100, 0)
		samplePaths, samplePathsErr := client.IRODSPathsForSample(context.Background(), "7607STDY14643771", 100, 0)
		studyPaths, studyPathsErr := client.IRODSPathsForStudy(context.Background(), "6568", 100, 0)

		convey.So(errors.Is(runErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(errors.Is(librariesErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(errors.Is(runsErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(errors.Is(lanesErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(errors.Is(samplePathsErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(errors.Is(studyPathsErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(runSamples, convey.ShouldResemble, []Sample{})
		convey.So(libraries, convey.ShouldResemble, []Library{})
		convey.So(runs, convey.ShouldResemble, []Run{})
		convey.So(lanes, convey.ShouldResemble, []Lane{})
		convey.So(samplePaths, convey.ShouldResemble, []IRODSPath{})
		convey.So(studyPaths, convey.ShouldResemble, []IRODSPath{})
	})
}

func TestSamplesForMethodsMissingParentsReturnErrNotFound(t *testing.T) {
	convey.Convey("Given missing parents for study, run, and library traversals", t, func() {
		studyClient, studyROMock, studySourceMock, studyCleanup := newMySQLResolverTestClient(t)
		defer studyCleanup()
		studyROMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).WithArgs("6568", 100, 0).WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		studyROMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyParentQuery)).WithArgs("6568").WillReturnRows(sqlmock.NewRows([]string{"found"}))
		studyROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).WithArgs(syncTableStudy).WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow("2026-05-06T12:32:00Z", nil, 0))

		studySamples, studyErr := studyClient.SamplesForStudy(context.Background(), "6568", 100, 0)

		convey.So(errors.Is(studyErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(studySamples, convey.ShouldBeNil)
		convey.So(studySourceMock.ExpectationsWereMet(), convey.ShouldBeNil)

		runClient, _, runCleanup := newHierarchyTestClient(t)
		defer runCleanup()
		seedSyncState(t, runClient.cache.DB(), syncTableIseqProductMetrics, time.Date(2026, time.May, 6, 12, 32, 0, 0, time.UTC))

		runSamples, runErr := runClient.SamplesForRun(context.Background(), "12345", 100, 0)

		convey.So(errors.Is(runErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(runSamples, convey.ShouldBeNil)

		libraryClient, libraryROMock, librarySourceMock, libraryCleanup := newMySQLResolverTestClient(t)
		defer libraryCleanup()
		libraryROMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryCacheQuery)).WithArgs("Standard", "6568", 100, 0).WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryStudyParentQuery)).WithArgs("6568").WillReturnRows(sqlmock.NewRows([]string{"found"}))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).WithArgs(syncTableStudy).WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow("2026-05-06T12:32:00Z", nil, 0))

		librarySamples, libraryErr := libraryClient.SamplesForLibrary(context.Background(), "Standard", "6568", 100, 0)

		convey.So(errors.Is(libraryErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(librarySamples, convey.ShouldBeNil)
		convey.So(librarySourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestSamplesForMethodsKnownParentsWithoutChildrenReturnEmptySlices(t *testing.T) {
	convey.Convey("Given known parents with no children", t, func() {
		studyCache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(studyCache.Close(), convey.ShouldBeNil) }()
		seedStudyMirrorRow(t, studyCache.DB(), 11, "6568", "study-uuid-11", "Study 11", "EGAS00001001111")
		seedSyncState(t, studyCache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 18, 0, 0, 0, time.UTC))
		seedSyncState(t, studyCache.DB(), syncTableSample, time.Date(2026, time.May, 6, 18, 0, 0, 0, time.UTC))
		seedSyncState(t, studyCache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 18, 0, 0, 0, time.UTC))
		studyClient := &Client{cache: studyCache, cacheReader: cacheReadDB(studyCache), syncSource: studyCache.DB()}

		studySamples, studyErr := studyClient.SamplesForStudy(context.Background(), "6568", 100, 0)

		convey.So(studyErr, convey.ShouldBeNil)
		convey.So(studySamples, convey.ShouldHaveLength, 0)

		runClient, _, runCleanup := newHierarchyTestClient(t)
		defer runCleanup()
		seedSyncState(t, runClient.cache.DB(), syncTableIseqProductMetrics, time.Date(2026, time.May, 6, 18, 0, 0, 0, time.UTC))

		runSamples, runErr := runClient.SamplesForRun(context.Background(), "12345", 100, 0)

		convey.So(errors.Is(runErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(runSamples, convey.ShouldBeNil)

		libraryCache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(libraryCache.Close(), convey.ShouldBeNil) }()
		seedStudyMirrorRow(t, libraryCache.DB(), 12, "6568", "study-uuid-12", "Study 12", "EGAS00001001112")
		seedSyncState(t, libraryCache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 18, 1, 0, 0, time.UTC))
		seedSyncState(t, libraryCache.DB(), syncTableSample, time.Date(2026, time.May, 6, 18, 1, 0, 0, time.UTC))
		seedSyncState(t, libraryCache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 18, 1, 0, 0, time.UTC))
		libraryClient := &Client{cache: libraryCache, cacheReader: cacheReadDB(libraryCache), syncSource: libraryCache.DB()}

		librarySamples, libraryErr := libraryClient.SamplesForLibrary(context.Background(), "Standard", "6568", 100, 0)

		convey.So(libraryErr, convey.ShouldBeNil)
		convey.So(librarySamples, convey.ShouldHaveLength, 0)
	})
}

func TestExpandIdentifierStudyUsesAtMostFourQueries(t *testing.T) {
	convey.Convey("Given a study with one sample and one lane", t, func() {
		client, roMock, _, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		start := time.Now()

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).
			WithArgs("6568", 1000, 0).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "A", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...))
		roMock.ExpectQuery(regexp.QuoteMeta(sampleFanOutOneIDQuery)).
			WithArgs(int64(1)).
			WillReturnRows(
				sqlmock.NewRows([]string{
					"id_sample_tmp", "pipeline_id_lims", "library_id", "id_library_lims",
					"study_mirror.id_study_tmp", "study_mirror.id_lims", "study_mirror.id_study_lims", "study_mirror.uuid_study_lims", "study_mirror.name", "study_mirror.accession_number", "study_mirror.study_title", "study_mirror.faculty_sponsor", "study_mirror.state", "study_mirror.data_release_strategy", "study_mirror.data_access_group", "study_mirror.programme", "study_mirror.reference_genome", "study_mirror.ethically_approved", "study_mirror.study_type", "study_mirror.contains_human_dna", "study_mirror.contaminated_human_dna", "study_mirror.study_visibility", "study_mirror.ega_dac_accession_number", "study_mirror.ega_policy_accession_number", "study_mirror.data_release_timing",
				}).
					AddRow(int64(1), "Standard", "", "", int64(11), "SQSCP", "6568", "study-uuid-1", "Study A", "ERP1", "title-1", "sponsor-1", "active", "open", "group-1", "programme-1", "GRCh38", true, "type-1", true, false, "public", "DAC1", "POL1", "immediate"),
			)
		roMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleStudyQuery)).
			WithArgs(int64(1), "6568", 1000, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).AddRow(100, 1, 0))

		taggedIDs, err := client.ExpandIdentifier(context.Background(), KindStudyLimsID, "6568")
		elapsed := time.Since(start)

		convey.So(err, convey.ShouldBeNil)
		convey.So(taggedIDs, convey.ShouldResemble, []TaggedID{
			{Kind: KindStudyLimsID, Canonical: "6568"},
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindRunID, Canonical: "100"},
		})
		convey.So(elapsed, convey.ShouldBeLessThan, 100*time.Millisecond)
	})
}

func TestExpandIdentifierCachesResultsForTTL(t *testing.T) {
	convey.Convey("Given a cached expand result within the TTL", t, func() {
		client, roMock, _, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).
			WithArgs("6568", 1000, 0).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "A", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...))
		roMock.ExpectQuery(regexp.QuoteMeta(sampleFanOutOneIDQuery)).
			WithArgs(int64(1)).
			WillReturnRows(
				sqlmock.NewRows([]string{
					"id_sample_tmp", "pipeline_id_lims", "library_id", "id_library_lims",
					"study_mirror.id_study_tmp", "study_mirror.id_lims", "study_mirror.id_study_lims", "study_mirror.uuid_study_lims", "study_mirror.name", "study_mirror.accession_number", "study_mirror.study_title", "study_mirror.faculty_sponsor", "study_mirror.state", "study_mirror.data_release_strategy", "study_mirror.data_access_group", "study_mirror.programme", "study_mirror.reference_genome", "study_mirror.ethically_approved", "study_mirror.study_type", "study_mirror.contains_human_dna", "study_mirror.contaminated_human_dna", "study_mirror.study_visibility", "study_mirror.ega_dac_accession_number", "study_mirror.ega_policy_accession_number", "study_mirror.data_release_timing",
				}).
					AddRow(int64(1), "Standard", "", "", int64(11), "SQSCP", "6568", "study-uuid-1", "Study A", "ERP1", "title-1", "sponsor-1", "active", "open", "group-1", "programme-1", "GRCh38", true, "type-1", true, false, "public", "DAC1", "POL1", "immediate"),
			)
		roMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleStudyQuery)).
			WithArgs(int64(1), "6568", 1000, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).AddRow(100, 1, 0))

		first, err := client.ExpandIdentifier(context.Background(), KindStudyLimsID, "6568")
		convey.So(err, convey.ShouldBeNil)

		second, err := client.ExpandIdentifier(context.Background(), KindStudyLimsID, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(second, convey.ShouldResemble, first)
	})
}

func TestLibrariesForStudyReturnsDistinctCountsPerPipeline(t *testing.T) {
	convey.Convey("Given a study whose cache rows span two pipeline_id_lims values", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")

		for sampleID := range 10 {
			seedHierarchySample(t, client.cache.DB(), int64(sampleID+1), "6568", "sample-standard-"+formatInt(int64(sampleID+1)))
			seedLibrarySample(t, client.cache.DB(), "Standard", int64(sampleID+1), "6568")
		}

		for sampleID := range 3 {
			resolvedID := int64(sampleID + 11)
			seedHierarchySample(t, client.cache.DB(), resolvedID, "6568", "sample-bespoke-"+formatInt(resolvedID))
			seedLibrarySample(t, client.cache.DB(), "Bespoke", resolvedID, "6568")
		}

		seedLibrarySample(t, client.cache.DB(), "Standard", 1, "6569")

		libraries, err := client.LibrariesForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(libraries, convey.ShouldResemble, []Library{
			{PipelineIDLims: "Bespoke", IDStudyLims: "6568"},
			{PipelineIDLims: "Standard", IDStudyLims: "6568"},
		})
	})
}

func TestLibrariesForStudyKeepsSpecificLibraryIdentifiers(t *testing.T) {
	convey.Convey("Given a study with repeated library types but distinct library identifiers", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "7607")
		seedHierarchySample(t, client.cache.DB(), 31, "7607", "7607STDY14643771")
		seedHierarchySample(t, client.cache.DB(), 32, "7607", "7607STDY14643772")
		_, err := client.cache.DB().Exec(
			`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims, library_id, id_library_lims) VALUES (?, ?, ?, ?, ?)`,
			"Custom", 31, "7607", "71046409", "SQPP-47463-G:B1",
		)
		convey.So(err, convey.ShouldBeNil)
		_, err = client.cache.DB().Exec(
			`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims, library_id, id_library_lims) VALUES (?, ?, ?, ?, ?)`,
			"Custom", 32, "7607", "71046410", "SQPP-47464-G:C1",
		)
		convey.So(err, convey.ShouldBeNil)

		libraries, err := client.LibrariesForStudy(context.Background(), "7607", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(libraries, convey.ShouldResemble, []Library{
			{
				PipelineIDLims: "Custom",
				IDStudyLims:    "7607",
				LibraryID:      "71046409",
				IDLibraryLims:  "SQPP-47463-G:B1",
			},
			{
				PipelineIDLims: "Custom",
				IDStudyLims:    "7607",
				LibraryID:      "71046410",
				IDLibraryLims:  "SQPP-47464-G:C1",
			},
		})
	})
}

func TestRunsForStudyReturnsDistinctRunIDs(t *testing.T) {
	convey.Convey("Given a cached study with metrics rows spanning two runs", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 2001, 11, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 2002, 12, 101, 1, 0, "6568")

		runs, err := client.RunsForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(runs, convey.ShouldResemble, []Run{{IDRun: 100}, {IDRun: 101}})
	})
}

func TestLanesForSampleReturnsOrderedLaneTriples(t *testing.T) {
	convey.Convey("Given a cached sample with three product-metrics rows", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 21, "6568", "7607STDY14643771")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 3001, 21, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 3002, 21, 100, 2, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 3003, 21, 101, 1, 5, "6568")

		lanes, err := client.LanesForSample(context.Background(), "7607STDY14643771", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(lanes, convey.ShouldResemble, []Lane{{IDRun: 100, Position: 1, TagIndex: 0}, {IDRun: 100, Position: 2, TagIndex: 0}, {IDRun: 101, Position: 1, TagIndex: 5}})
	})
}

func TestIRODSPathsForSampleReturnsJoinedPaths(t *testing.T) {
	convey.Convey("Given a cached sample with two seq_product_irods_locations rows", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 31, "6568", "7607STDY14643771")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "4001", "/seq/1234", "1234_1#1.cram", 31, "6568")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "4002", "/seq/1234", "1234_1#2.cram", 31, "6568")

		paths, err := client.IRODSPathsForSample(context.Background(), "7607STDY14643771", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(paths, convey.ShouldResemble, []IRODSPath{
			{IDProduct: "4001", Collection: "/seq/1234", DataObject: "1234_1#1.cram", IRODSPath: "/seq/1234/1234_1#1.cram", Platform: "illumina"},
			{IDProduct: "4002", Collection: "/seq/1234", DataObject: "1234_1#2.cram", IRODSPath: "/seq/1234/1234_1#2.cram", Platform: "illumina"},
		})
	})
}

func TestIRODSPathsForSampleNormalisesTrailingCollectionSlash(t *testing.T) {
	convey.Convey("Given an iRODS collection already ending in a slash", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 34, "7607", "7607STDY14643771")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "5c7e2518e6e4b9f0bff053374d43a2b1f9bbb84625f035148db857b9bb01bfc0", "/seq/illumina/runs/48/48522/plex1/", "48522#1.cram", 34, "7607")

		paths, err := client.IRODSPathsForSample(context.Background(), "7607STDY14643771", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(paths, convey.ShouldHaveLength, 1)
		convey.So(paths[0].IRODSPath, convey.ShouldEqual, "/seq/illumina/runs/48/48522/plex1/48522#1.cram")
	})
}

func TestIRODSPathsForSampleReturnsCompositeProductPathForEveryLinkedSample(t *testing.T) {
	convey.Convey("Given one iRODS product path linked to multiple component samples", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 31, "7607", "7607STDY14643771")
		seedHierarchySample(t, client.cache.DB(), 32, "7607", "7607STDY14643772")
		seedHierarchySample(t, client.cache.DB(), 33, "7607", "NO_IRODS_SAMPLE")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "5c7e2518e6e4b9f0bff053374d43a2b1f9bbb84625f035148db857b9bb01bfc0", "/seq/illumina/runs/48/48522/plex1", "48522#1.cram", 31, "7607")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "5c7e2518e6e4b9f0bff053374d43a2b1f9bbb84625f035148db857b9bb01bfc0", "/seq/illumina/runs/48/48522/plex1", "48522#1.cram", 32, "7607")
		seedSyncState(t, client.cache.DB(), syncTableSeqProductIRODSLocations, time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC))

		firstPaths, firstErr := client.IRODSPathsForSample(context.Background(), "7607STDY14643771", 100, 0)
		secondPaths, secondErr := client.IRODSPathsForSample(context.Background(), "7607STDY14643772", 100, 0)
		missingPaths, missingErr := client.IRODSPathsForSample(context.Background(), "NO_IRODS_SAMPLE", 100, 0)

		convey.So(firstErr, convey.ShouldBeNil)
		convey.So(firstPaths, convey.ShouldResemble, []IRODSPath{{
			IDProduct:  "5c7e2518e6e4b9f0bff053374d43a2b1f9bbb84625f035148db857b9bb01bfc0",
			Collection: "/seq/illumina/runs/48/48522/plex1",
			DataObject: "48522#1.cram",
			IRODSPath:  "/seq/illumina/runs/48/48522/plex1/48522#1.cram",
			Platform:   "illumina",
		}})
		convey.So(secondErr, convey.ShouldBeNil)
		convey.So(secondPaths, convey.ShouldHaveLength, 1)
		convey.So(missingErr, convey.ShouldBeNil)
		convey.So(missingPaths, convey.ShouldResemble, []IRODSPath{})
	})
}

func TestFindSamplesBySangerIDHydratesLibraryIdentifiers(t *testing.T) {
	convey.Convey("Given a cached sample linked to a library row with MLWH library identifiers", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 13, 10, 1, 0, 0, time.UTC))
		seedHierarchyStudy(t, client.cache.DB(), 81, "7607")
		seedHierarchySample(t, client.cache.DB(), 31, "7607", "7607STDY14643771")
		_, err := client.cache.DB().Exec(`UPDATE sample_mirror SET sanger_sample_id = ? WHERE id_sample_tmp = ?`, "7607STDY14643771", 31)
		convey.So(err, convey.ShouldBeNil)
		_, err = client.cache.DB().Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims, library_id, id_library_lims) VALUES (?, ?, ?, ?, ?)`, "Custom", 31, "7607", "71046409", "SQPP-47463-G:B1")
		convey.So(err, convey.ShouldBeNil)

		samples, err := client.FindSamplesBySangerID(context.Background(), "7607STDY14643771")

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].Libraries, convey.ShouldResemble, []Library{{
			PipelineIDLims: "Custom",
			IDStudyLims:    "7607",
			LibraryID:      "71046409",
			IDLibraryLims:  "SQPP-47463-G:B1",
		}})
	})
}

func TestHierarchyMethodsReturnErrNotFoundForMissingParents(t *testing.T) {
	convey.Convey("Given missing study and sample parents", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()
		seedSyncState(t, client.cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 19, 0, 0, 0, time.UTC))
		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 19, 1, 0, 0, time.UTC))
		seedSyncState(t, client.cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 19, 2, 0, 0, time.UTC))
		seedSyncState(t, client.cache.DB(), syncTableIseqProductMetrics, time.Date(2026, time.May, 6, 19, 3, 0, 0, time.UTC))
		seedSyncState(t, client.cache.DB(), syncTableSeqProductIRODSLocations, time.Date(2026, time.May, 6, 19, 4, 0, 0, time.UTC))

		libraries, librariesErr := client.LibrariesForStudy(context.Background(), "6568", 100, 0)
		runs, runsErr := client.RunsForStudy(context.Background(), "6568", 100, 0)
		lanes, lanesErr := client.LanesForSample(context.Background(), "7607STDY14643771", 100, 0)
		samplePaths, samplePathsErr := client.IRODSPathsForSample(context.Background(), "7607STDY14643771", 100, 0)
		studyPaths, studyPathsErr := client.IRODSPathsForStudy(context.Background(), "6568", 100, 0)
		studies, studiesErr := client.StudiesForSample(context.Background(), "7607STDY14643771")

		convey.So(errors.Is(librariesErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(runsErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(lanesErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(samplePathsErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(studyPathsErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(studiesErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(libraries, convey.ShouldBeNil)
		convey.So(runs, convey.ShouldBeNil)
		convey.So(lanes, convey.ShouldBeNil)
		convey.So(samplePaths, convey.ShouldBeNil)
		convey.So(studyPaths, convey.ShouldBeNil)
		convey.So(studies, convey.ShouldBeNil)
	})
}

func TestHierarchyMethodsReturnEmptySlicesForParentsWithoutChildren(t *testing.T) {
	convey.Convey("Given existing study and sample parents without child rows", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 41, "6568", "7607STDY14643771")
		seedSyncState(t, client.cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 19, 3, 0, 0, time.UTC))
		seedSyncState(t, client.cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 19, 0, 0, 0, time.UTC))
		seedSyncState(t, client.cache.DB(), syncTableIseqProductMetrics, time.Date(2026, time.May, 6, 19, 1, 0, 0, time.UTC))
		seedSyncState(t, client.cache.DB(), syncTableSeqProductIRODSLocations, time.Date(2026, time.May, 6, 19, 2, 0, 0, time.UTC))

		libraries, librariesErr := client.LibrariesForStudy(context.Background(), "6568", 100, 0)
		runs, runsErr := client.RunsForStudy(context.Background(), "6568", 100, 0)
		lanes, lanesErr := client.LanesForSample(context.Background(), "7607STDY14643771", 100, 0)
		samplePaths, samplePathsErr := client.IRODSPathsForSample(context.Background(), "7607STDY14643771", 100, 0)
		studyPaths, studyPathsErr := client.IRODSPathsForStudy(context.Background(), "6568", 100, 0)

		convey.So(librariesErr, convey.ShouldBeNil)
		convey.So(runsErr, convey.ShouldBeNil)
		convey.So(lanesErr, convey.ShouldBeNil)
		convey.So(samplePathsErr, convey.ShouldBeNil)
		convey.So(studyPathsErr, convey.ShouldBeNil)
		convey.So(libraries, convey.ShouldResemble, []Library{})
		convey.So(runs, convey.ShouldResemble, []Run{})
		convey.So(lanes, convey.ShouldResemble, []Lane{})
		convey.So(samplePaths, convey.ShouldResemble, []IRODSPath{})
		convey.So(studyPaths, convey.ShouldResemble, []IRODSPath{})
	})
}

func TestIRODSPathsForStudyReturnsJoinedPaths(t *testing.T) {
	convey.Convey("Given a cached study with two seq_product_irods_locations rows", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 71, "6568")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "5001", "/seq/5678", "5678_1#1.cram", 91, "6568")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "5002", "/seq/5678", "5678_1#2.cram", 92, "6568")

		paths, err := client.IRODSPathsForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(paths, convey.ShouldResemble, []IRODSPath{
			{IDProduct: "5001", Collection: "/seq/5678", DataObject: "5678_1#1.cram", IRODSPath: "/seq/5678/5678_1#1.cram", IDSampleTmp: 91, Platform: "illumina"},
			{IDProduct: "5002", Collection: "/seq/5678", DataObject: "5678_1#2.cram", IRODSPath: "/seq/5678/5678_1#2.cram", IDSampleTmp: 92, Platform: "illumina"},
		})
	})
}

// B3 acceptance test 5: each /study/:id/irods row carries the correct
// id_sample_tmp and Sanger name, and grouping the rows by id_sample_tmp yields
// exactly the three samples-with-data.
func TestIRODSPathsForStudyCarrySampleIdentityGroupableBySample(t *testing.T) {
	convey.Convey("Given study S1 with study-scoped iRODS rows across three samples", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		paths, err := client.IRODSPathsForStudy(context.Background(), "S1", availabilityFetchAll, 0)

		convey.Convey("when the rows are fetched, then each carries its sample identity and they group to the 3 with-data samples", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(paths), convey.ShouldEqual, 6)

			bySample := make(map[int64]string)
			for _, path := range paths {
				convey.So(path.IDSampleTmp, convey.ShouldBeGreaterThan, 0)
				convey.So(path.Name, convey.ShouldNotBeEmpty)
				bySample[path.IDSampleTmp] = path.Name
			}

			convey.So(len(bySample), convey.ShouldEqual, 3)
			convey.So(bySample[b3IlluminaSampleA], convey.ShouldEqual, "sample-"+formatInt(b3IlluminaSampleA))
			convey.So(bySample[b3IlluminaSampleB], convey.ShouldEqual, "sample-"+formatInt(b3IlluminaSampleB))
			convey.So(bySample[b3PacBioSample], convey.ShouldEqual, "sample-"+formatInt(b3PacBioSample))

			// The S2-scoped sample's data object must not appear under S1.
			_, leaked := bySample[b3SharedWithS2]
			convey.So(leaked, convey.ShouldBeFalse)
		})
	})
}

// B1 acceptance test 1: a study Illumina iRODS row whose id_iseq_product matches
// an iseq_product_metrics_mirror row on a run carries that run id and the iRODS
// row's platform, derived by the LEFT JOIN on id_iseq_product.
func TestIRODSPathsForStudyCarryIDRunAndPlatformWhenProductMetricsMatch(t *testing.T) {
	convey.Convey("Given study S1 with an Illumina iRODS row matching a product-metrics run", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 101, "S1")
		seedHierarchySample(t, client.cache.DB(), 1, "S1", "S1STDY1")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 9001, 1, 52553, 1, 1, "S1")
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, client.cache.DB(), "9001", "/seq/52553", "52553_1#1.cram", 1, "S1", time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC), "illumina")

		paths, err := client.IRODSPathsForStudy(context.Background(), "S1", 100, 0)

		convey.Convey("when the study iRODS list is fetched, then the row carries id_run=52553 and platform=illumina", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(paths, convey.ShouldResemble, []IRODSPath{{
				IDProduct:   "9001",
				Collection:  "/seq/52553",
				DataObject:  "52553_1#1.cram",
				IRODSPath:   "/seq/52553/52553_1#1.cram",
				IDSampleTmp: 1,
				Name:        "S1STDY1",
				IDRun:       52553,
				Platform:    "illumina",
			}})
		})
	})
}

// B1 acceptance test 2: a study iRODS row (platform ont) whose id_iseq_product
// matches no iseq_product_metrics_mirror row gets id_run=0 (LEFT JOIN miss) and
// the platform it was synced with.
func TestIRODSPathsForStudyUnmatchedRowGetsZeroIDRunAndKeepsPlatform(t *testing.T) {
	convey.Convey("Given study S1 with an ont iRODS row matching no product-metrics row", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 101, "S1")
		seedHierarchySample(t, client.cache.DB(), 2, "S1", "S1STDY2")
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, client.cache.DB(), "ont-9002", "/seq/ont", "ont_run.fast5", 2, "S1", time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC), "ont")

		paths, err := client.IRODSPathsForStudy(context.Background(), "S1", 100, 0)

		convey.Convey("when the study iRODS list is fetched, then the row has id_run=0 and platform=ont", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(paths, convey.ShouldResemble, []IRODSPath{{
				IDProduct:   "ont-9002",
				Collection:  "/seq/ont",
				DataObject:  "ont_run.fast5",
				IRODSPath:   "/seq/ont/ont_run.fast5",
				IDSampleTmp: 2,
				Name:        "S1STDY2",
				IDRun:       0,
				Platform:    "ont",
			}})
		})
	})
}

// B1 acceptance test 3: the id_run/platform additions do not change the row
// grain, so CountIRODSPathsForStudy still equals len(IRODSPathsForStudy(all)).
func TestIRODSPathsForStudyCountEqualsListLenAfterIDRunPlatform(t *testing.T) {
	convey.Convey("Given study S1 with a mix of matched and unmatched iRODS rows", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 101, "S1")
		seedHierarchySample(t, client.cache.DB(), 1, "S1", "S1STDY1")
		seedHierarchySample(t, client.cache.DB(), 2, "S1", "S1STDY2")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 9001, 1, 52553, 1, 1, "S1")
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, client.cache.DB(), "9001", "/seq/52553", "52553_1#1.cram", 1, "S1", time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC), "illumina")
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, client.cache.DB(), "ont-9002", "/seq/ont", "ont_run.fast5", 2, "S1", time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC), "ont")

		count, countErr := client.CountIRODSPathsForStudy(context.Background(), "S1")
		paths, listErr := client.IRODSPathsForStudy(context.Background(), "S1", 100, 0)

		convey.Convey("when the count and the all-rows list are taken, then they are equal", func() {
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(paths))
			convey.So(count.Count, convey.ShouldEqual, 2)
		})
	})
}

// B2 acceptance test 1: a study iRODS list filtered by file_type=cram returns
// exactly the two .cram objects of three, and the matching count is 2.
func TestIRODSPathsForStudyByFileTypeFiltersToSuffix(t *testing.T) {
	convey.Convey("Given study S1 with a.cram, b.cram and c.bai iRODS objects", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 101, "S1")
		seedHierarchySample(t, client.cache.DB(), 1, "S1", "S1STDY1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-a", "/seq/s1", "a.cram", 1, "S1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-b", "/seq/s1", "b.cram", 1, "S1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-c", "/seq/s1", "c.bai", 1, "S1")

		paths, listErr := client.IRODSPathsForStudyByFileType(context.Background(), "S1", "cram", 100, 0)
		count, countErr := client.CountIRODSPathsForStudyByFileType(context.Background(), "S1", "cram")

		convey.Convey("when fetched with file_type=cram, then only the two .cram objects return and the count is 2", func() {
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(len(paths), convey.ShouldEqual, 2)
			objects := []string{paths[0].DataObject, paths[1].DataObject}
			convey.So(objects, convey.ShouldContain, "a.cram")
			convey.So(objects, convey.ShouldContain, "b.cram")
			convey.So(objects, convey.ShouldNotContain, "c.bai")
			convey.So(count.Count, convey.ShouldEqual, 2)
			convey.So(count.Count, convey.ShouldEqual, len(paths))
		})
	})
}

// B2 acceptance test 2: file_type=.CRAM (leading dot, mixed case) matches the
// same two .cram objects (leading dot stripped, case-insensitive).
func TestIRODSPathsForStudyByFileTypeStripsLeadingDotAndIsCaseInsensitive(t *testing.T) {
	convey.Convey("Given study S1 with two .cram objects and one .bai object", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 101, "S1")
		seedHierarchySample(t, client.cache.DB(), 1, "S1", "S1STDY1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-a", "/seq/s1", "a.cram", 1, "S1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-b", "/seq/s1", "b.cram", 1, "S1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-c", "/seq/s1", "c.bai", 1, "S1")

		paths, listErr := client.IRODSPathsForStudyByFileType(context.Background(), "S1", ".CRAM", 100, 0)
		count, countErr := client.CountIRODSPathsForStudyByFileType(context.Background(), "S1", ".CRAM")

		convey.Convey("when fetched with file_type=.CRAM, then it matches the same two .cram objects", func() {
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(len(paths), convey.ShouldEqual, 2)
			convey.So(count.Count, convey.ShouldEqual, 2)
			convey.So(count.Count, convey.ShouldEqual, len(paths))
		})
	})
}

// B2 acceptance test 3: file_type=bam (valid but unmatched) on a synced S1
// yields an empty list and a count of 0 with NO error.
func TestIRODSPathsForStudyByFileTypeUnmatchedSuffixIsEmptyNotError(t *testing.T) {
	convey.Convey("Given a synced study S1 with only .cram objects", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedSyncState(t, client.cache.DB(), syncTableSeqProductIRODSLocations, time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC))
		seedHierarchyStudy(t, client.cache.DB(), 101, "S1")
		seedHierarchySample(t, client.cache.DB(), 1, "S1", "S1STDY1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-a", "/seq/s1", "a.cram", 1, "S1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-b", "/seq/s1", "b.cram", 1, "S1")

		paths, listErr := client.IRODSPathsForStudyByFileType(context.Background(), "S1", "bam", 100, 0)
		count, countErr := client.CountIRODSPathsForStudyByFileType(context.Background(), "S1", "bam")

		convey.Convey("when fetched with file_type=bam, then the list is empty and the count is 0 with no error", func() {
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(paths, convey.ShouldBeEmpty)
			convey.So(count.Count, convey.ShouldEqual, 0)
			convey.So(count.Count, convey.ShouldEqual, len(paths))
		})
	})
}

// B2 acceptance test 5: a sample with a .cram and a .bai object filtered by
// file_type=cram returns only the .cram object and its count is 1.
func TestIRODSPathsForSampleByFileTypeFiltersToSuffix(t *testing.T) {
	convey.Convey("Given a cached sample with one .cram and one .bai iRODS object", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 31, "6568", "7607STDY14643771")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "4001", "/seq/1234", "1234_1#1.cram", 31, "6568")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "4002", "/seq/1234", "1234_1#1.bai", 31, "6568")

		paths, listErr := client.IRODSPathsForSampleByFileType(context.Background(), "7607STDY14643771", "cram", 100, 0)
		count, countErr := client.CountIRODSPathsForSampleByFileType(context.Background(), "7607STDY14643771", "cram")

		convey.Convey("when fetched with file_type=cram, then only the .cram object returns and the count is 1", func() {
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(len(paths), convey.ShouldEqual, 1)
			convey.So(paths[0].DataObject, convey.ShouldEqual, "1234_1#1.cram")
			convey.So(count.Count, convey.ShouldEqual, 1)
			convey.So(count.Count, convey.ShouldEqual, len(paths))
		})
	})
}

// seedB3RunIRODSScenario seeds run 52553 with six iRODS data objects across its
// products (four .cram, two .bai) plus a decoy product/iRODS object on a DIFFERENT
// run (52554), so the run-scope query (B3) must return exactly the run's six rows
// and never the decoy. The products span two samples to prove the run scope is not
// study- or sample-scoped: it follows id_iseq_product from the run's
// iseq_product_metrics rows to the iRODS mirror. It stamps the sync state the
// run-scope cascade consults so a synced cache reads data rather than the
// never-synced sentinel.
func seedB3RunIRODSScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedSyncState(t, db, syncTableIseqProductMetrics, time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableSeqProductIRODSLocations, time.Date(2026, time.June, 25, 9, 5, 0, 0, time.UTC))

	seedHierarchyStudy(t, db, 101, "S1")
	seedHierarchySample(t, db, 1, "S1", "S1STDY1")
	seedHierarchySample(t, db, 2, "S1", "S1STDY2")

	// Run 52553: six iRODS data objects across the run's products (4 .cram, 2 .bai).
	runProducts := []struct {
		idIseqProduct int64
		idSampleTmp   int64
		position      int
		fileName      string
	}{
		{9001, 1, 1, "52553_1#1.cram"},
		{9002, 1, 1, "52553_1#1.bai"},
		{9003, 1, 2, "52553_1#2.cram"},
		{9004, 2, 3, "52553_2#1.cram"},
		{9005, 2, 3, "52553_2#1.bai"},
		{9006, 2, 4, "52553_2#2.cram"},
	}
	for _, product := range runProducts {
		seedIseqProductMetricsMirrorRow(t, db, product.idIseqProduct, product.idSampleTmp, 52553, product.position, 1, "S1")
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, formatInt(product.idIseqProduct), "/seq/52553", product.fileName, product.idSampleTmp, "S1", time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC), "illumina")
	}

	// A decoy product + iRODS object on a different run; the run-scope query must
	// exclude it.
	seedIseqProductMetricsMirrorRow(t, db, 9999, 1, 52554, 1, 1, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "9999", "/seq/52554", "52554_1#1.cram", 1, "S1", time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC), "illumina")
}

// B3 acceptance test 1: the run iRODS list returns one row per iRODS data object
// on the run (six here), each carrying id_run = the run, derived by joining the
// run's iseq_product_metrics rows to the iRODS mirror on id_iseq_product.
func TestIRODSPathsForRunReturnsRunDataObjects(t *testing.T) {
	convey.Convey("Given run 52553 with six iRODS data objects across its products", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3RunIRODSScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		paths, err := client.IRODSPathsForRun(context.Background(), "52553", "", availabilityFetchAll, 0)

		convey.Convey("when the run iRODS list is fetched, then 6 rows return, each with id_run=52553", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(paths), convey.ShouldEqual, 6)

			wrongRun := 0
			for _, path := range paths {
				if path.IDRun != 52553 {
					wrongRun++
				}
				convey.So(path.IRODSPath, convey.ShouldStartWith, "/seq/52553/")
			}
			convey.So(wrongRun, convey.ShouldEqual, 0)
		})
	})
}

// B3 acceptance test 2 (list half): the run iRODS list filtered by file_type=cram
// returns exactly the four .cram objects of the six on the run.
func TestIRODSPathsForRunByFileTypeFiltersToSuffix(t *testing.T) {
	convey.Convey("Given run 52553 with four .cram and two .bai iRODS objects", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3RunIRODSScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		paths, err := client.IRODSPathsForRun(context.Background(), "52553", "cram", availabilityFetchAll, 0)

		convey.Convey("when fetched with file_type=cram, then exactly the 4 .cram objects return", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(paths), convey.ShouldEqual, 4)

			nonCram := 0
			for _, path := range paths {
				if !strings.HasSuffix(path.DataObject, ".cram") {
					nonCram++
				}
			}
			convey.So(nonCram, convey.ShouldEqual, 0)
		})
	})
}

// B3 acceptance test 4: a non-numeric run id is an unsupported identifier; a
// numeric run absent from a SYNCED cache is not_found; a never-synced cache yields
// an error satisfying BOTH ErrCacheNeverSynced AND ErrNotFound. This mirrors the
// run space's ResolveRun cascade exactly (the same as RunOverview / SamplesForRun).
func TestIRODSPathsForRunResolveCascade(t *testing.T) {
	ctx := context.Background()

	convey.Convey("Given a non-numeric run id on any cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, err := client.IRODSPathsForRun(ctx, "not-a-run", "", availabilityFetchAll, 0)

		convey.Convey("when called, then ErrUnsupportedIdentifier", func() {
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given a numeric run absent from a synced cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3RunIRODSScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, err := client.IRODSPathsForRun(ctx, "70000", "", availabilityFetchAll, 0)

		convey.Convey("when called, then ErrNotFound (and NOT the never-synced sentinel)", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
		})
	})

	convey.Convey("Given a never-synced cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, err := client.IRODSPathsForRun(ctx, "52553", "", availabilityFetchAll, 0)

		convey.Convey("when called, then the error satisfies both ErrCacheNeverSynced and ErrNotFound", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})
}

// B2: the bare IRODSPathsForStudy / IRODSPathsForSample methods must keep their
// signatures and behave exactly like the ByFileType variants with fileType="".
func TestIRODSPathsBareMethodsDelegateWithEmptyFileType(t *testing.T) {
	convey.Convey("Given study S1 and sample with mixed-suffix iRODS objects", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 101, "S1")
		seedHierarchySample(t, client.cache.DB(), 1, "S1", "S1STDY1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-a", "/seq/s1", "a.cram", 1, "S1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-c", "/seq/s1", "c.bai", 1, "S1")

		bareStudy, bareStudyErr := client.IRODSPathsForStudy(context.Background(), "S1", 100, 0)
		emptyFTStudy, emptyFTStudyErr := client.IRODSPathsForStudyByFileType(context.Background(), "S1", "", 100, 0)
		bareSample, bareSampleErr := client.IRODSPathsForSample(context.Background(), "S1STDY1", 100, 0)
		emptyFTSample, emptyFTSampleErr := client.IRODSPathsForSampleByFileType(context.Background(), "S1STDY1", "", 100, 0)

		convey.Convey("when the bare and empty-file-type variants are compared, then they return the same rows", func() {
			convey.So(bareStudyErr, convey.ShouldBeNil)
			convey.So(emptyFTStudyErr, convey.ShouldBeNil)
			convey.So(bareSampleErr, convey.ShouldBeNil)
			convey.So(emptyFTSampleErr, convey.ShouldBeNil)
			convey.So(bareStudy, convey.ShouldResemble, emptyFTStudy)
			convey.So(bareSample, convey.ShouldResemble, emptyFTSample)
			convey.So(len(bareStudy), convey.ShouldEqual, 2)
		})
	})
}

// B2: a normalised file_type that is empty after trimming (whitespace or a lone
// dot) or contains a LIKE wildcard or path separator is rejected with
// ErrUnsupportedIdentifier by the queryer too (defensive re-validation), so a
// direct Go caller is not silently wrong. The empty-string fileType is the
// no-filter sentinel (the bare methods delegate with it) and is NOT rejected
// here; rejecting a present-but-empty HTTP file_type is the handler's job.
func TestIRODSPathsByFileTypeRejectsInvalidTokenDefensively(t *testing.T) {
	convey.Convey("Given a study and sample with iRODS objects", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 101, "S1")
		seedHierarchySample(t, client.cache.DB(), 1, "S1", "S1STDY1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-a", "/seq/s1", "a.cram", 1, "S1")

		for _, invalid := range []string{"   ", ".", "%", "a%b", "a_b", "a/b"} {
			_, studyListErr := client.IRODSPathsForStudyByFileType(context.Background(), "S1", invalid, 100, 0)
			_, studyCountErr := client.CountIRODSPathsForStudyByFileType(context.Background(), "S1", invalid)
			_, sampleListErr := client.IRODSPathsForSampleByFileType(context.Background(), "S1STDY1", invalid, 100, 0)
			_, sampleCountErr := client.CountIRODSPathsForSampleByFileType(context.Background(), "S1STDY1", invalid)

			convey.So(errors.Is(studyListErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(errors.Is(studyCountErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(errors.Is(sampleListErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(errors.Is(sampleCountErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		}
	})
}

func TestHierarchyReadsNeverOpenWriteTransactions(t *testing.T) {
	convey.Convey("C7.1: Given 10 concurrent hierarchy reads against a populated cache, when they run in parallel, then each succeeds without opening a write transaction", t, func() {
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 11, 21, 0, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 11, 21, 1, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableIseqProductMetrics, time.Date(2026, time.May, 11, 21, 2, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableSeqProductIRODSLocations, time.Date(2026, time.May, 11, 21, 3, 0, 0, time.UTC))

		seedHierarchyStudy(t, cache.DB(), 1, "6568")
		seedHierarchySample(t, cache.DB(), 1, "6568", "S1")
		seedLibrarySample(t, cache.DB(), "Standard", 1, "6568")
		seedIseqProductMetricsMirrorRow(t, cache.DB(), 1001, 1, 12345, 1, 0, "6568")
		seedIRODSLocationMirrorRow(t, cache.DB(), "1001", "/seq/1234", "1234_1#1.cram", 1, "6568")

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}
		observer.Reset()

		reads := []func(context.Context) error{
			func(ctx context.Context) error { _, err := client.SamplesForStudy(ctx, "6568", 100, 0); return err },
			func(ctx context.Context) error { _, err := client.LibrariesForStudy(ctx, "6568", 100, 0); return err },
			func(ctx context.Context) error { _, err := client.RunsForStudy(ctx, "6568", 100, 0); return err },
			func(ctx context.Context) error { _, err := client.LanesForSample(ctx, "S1", 100, 0); return err },
			func(ctx context.Context) error { _, err := client.IRODSPathsForSample(ctx, "S1", 100, 0); return err },
			func(ctx context.Context) error { _, err := client.SamplesForStudy(ctx, "6568", 100, 0); return err },
			func(ctx context.Context) error { _, err := client.LibrariesForStudy(ctx, "6568", 100, 0); return err },
			func(ctx context.Context) error { _, err := client.RunsForStudy(ctx, "6568", 100, 0); return err },
			func(ctx context.Context) error { _, err := client.LanesForSample(ctx, "S1", 100, 0); return err },
			func(ctx context.Context) error { _, err := client.IRODSPathsForSample(ctx, "S1", 100, 0); return err },
		}

		start := make(chan struct{})
		errCh := make(chan error, len(reads))
		var wg sync.WaitGroup

		for _, read := range reads {
			wg.Add(1)
			go func(runRead func(context.Context) error) {
				defer wg.Done()
				<-start
				errCh <- runRead(context.Background())
			}(read)
		}

		close(start)
		wg.Wait()
		close(errCh)

		errorCount := 0
		for err := range errCh {
			if err != nil {
				errorCount++
			}
		}

		convey.So(errorCount, convey.ShouldEqual, 0)
		convey.So(observer.BeginCount(), convey.ShouldEqual, 0)
		convey.So(observer.CommitCount(), convey.ShouldEqual, 0)
	})
}

func TestHierarchyPackageDoesNotContainUpsertHierarchyReadThrough(t *testing.T) {
	convey.Convey("C7.2: Given the removed hierarchy read-through helper, when the package sources are scanned, then the symbol is absent", t, func() {
		entries, err := os.ReadDir(".")
		convey.So(err, convey.ShouldBeNil)

		removedSymbol := "upsertHierarchy" + "ReadThrough"
		matches := 0

		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
				continue
			}

			contents, readErr := os.ReadFile(entry.Name())
			convey.So(readErr, convey.ShouldBeNil)
			if regexp.MustCompile(regexp.QuoteMeta(removedSymbol)).Find(contents) != nil {
				matches++
			}
		}

		convey.So(matches, convey.ShouldEqual, 0)
	})
}

func TestStudiesForSampleReturnsOrderedStudyFanout(t *testing.T) {
	convey.Convey("Given a cached sample linked to two studies", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 82, "6569")
		seedHierarchyStudy(t, client.cache.DB(), 81, "6568")
		seedHierarchySample(t, client.cache.DB(), 1, "6568", "S1")
		seedLibrarySample(t, client.cache.DB(), "Standard", 1, "6568")
		seedLibrarySample(t, client.cache.DB(), "Chromium", 1, "6569")

		studies, err := client.StudiesForSample(context.Background(), "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(studies, convey.ShouldHaveLength, 2)
		convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(studies[1].IDStudyLims, convey.ShouldEqual, "6569")
	})
}

func TestStudiesForSampleReturnsErrNotFoundWhenSampleHasNoStudyLinks(t *testing.T) {
	convey.Convey("Given a cached sample with no library_samples rows", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 2, "6568", "S2")

		studies, err := client.StudiesForSample(context.Background(), "S2")

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(studies, convey.ShouldBeNil)
	})
}

func TestStudiesForSampleColdCacheReturnsErrCacheNeverSynced(t *testing.T) {
	convey.Convey("Given a never-synced cache", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		studies, err := client.StudiesForSample(context.Background(), "S1")

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(studies, convey.ShouldBeNil)
	})
}

func TestStudiesForSampleReturnsNeverSyncedWhenFanoutTableStateIsAbsent(t *testing.T) {
	convey.Convey("Given a partially-synced cache with only study sync state", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 101, "6568")
		seedHierarchySample(t, client.cache.DB(), 21, "6568", "S1")
		seedSyncState(t, client.cache.DB(), syncTableStudy, time.Date(2026, time.May, 11, 16, 0, 0, 0, time.UTC))
		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 11, 16, 1, 0, 0, time.UTC))

		match, err := client.ResolveStudy(context.Background(), "6568")
		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Study, convey.ShouldNotBeNil)
		convey.So(match.Study.IDStudyLims, convey.ShouldEqual, "6568")

		studies, err := client.StudiesForSample(context.Background(), "S1")

		convey.So(studies, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
	})
}

func TestFindSamplesByLibraryTypeReturnsNeverSyncedWhenDependentSyncStateIsPartial(t *testing.T) {
	convey.Convey("Given a partially-synced cache with sample sync state but no flowcell sync state", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()
		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 11, 17, 0, 0, 0, time.UTC))

		samples, err := client.FindSamplesByLibraryType(context.Background(), "Standard")

		convey.So(samples, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
	})
}

func TestStudiesForSampleExcludesNonSQSCPStudies(t *testing.T) {
	convey.Convey("Given a sample linked to SQSCP and non-SQSCP studies", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 91, "6568")
		_, err := client.cache.DB().Exec(`INSERT INTO study_mirror (
			id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title,
			faculty_sponsor, state, data_release_strategy, data_access_group, programme,
			reference_genome, ethically_approved, study_type, contains_human_dna,
			contaminated_human_dna, study_visibility, ega_dac_accession_number,
			ega_policy_accession_number, data_release_timing, last_updated
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			92, "CANSAMPLE", "9999", "study-uuid-92", "Other Study", "EGAS00001000092", "Other study title",
			"Faculty Sponsor", "active", "open", "group", "programme", "GRCh38", 1,
			"genomic sequencing", 0, 0, "open", "EGAC00001000092", "EGAP00001000092", "immediate",
			time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC),
		)
		convey.So(err, convey.ShouldBeNil)
		seedHierarchySample(t, client.cache.DB(), 3, "6568", "S3")
		seedLibrarySample(t, client.cache.DB(), "Standard", 3, "6568")
		seedLibrarySample(t, client.cache.DB(), "Chromium", 3, "9999")

		studies, err := client.StudiesForSample(context.Background(), "S3")

		convey.So(err, convey.ShouldBeNil)
		convey.So(studies, convey.ShouldHaveLength, 1)
		convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "6568")
	})
}

func TestFindSamplesByAccessionNumberReturnsErrAmbiguousWithBothCandidatePKs(t *testing.T) {
	convey.Convey("Given two cached samples sharing an accession number", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 11, 14, 0, 0, 0, time.UTC))
		seedHierarchySample(t, client.cache.DB(), 11, "6568", "S1")
		seedHierarchySample(t, client.cache.DB(), 22, "6568", "S2")
		_, err := client.cache.DB().Exec(`UPDATE sample_mirror SET accession_number = ? WHERE id_sample_tmp IN (?, ?)`, "DUP", 11, 22)
		convey.So(err, convey.ShouldBeNil)

		samples, err := client.FindSamplesByAccessionNumber(context.Background(), "DUP")

		convey.So(samples, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "11")
		convey.So(err.Error(), convey.ShouldContainSubstring, "22")
	})
}

func TestFindSamplesBySangerIDQueriesOnlySangerSampleIDColumn(t *testing.T) {
	convey.Convey("Given a cache where only accession_number matches the raw string", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 11, 14, 1, 0, 0, time.UTC))
		seedHierarchySample(t, client.cache.DB(), 33, "6568", "S3")
		_, err := client.cache.DB().Exec(`UPDATE sample_mirror SET accession_number = ?, sanger_sample_id = ? WHERE id_sample_tmp = ?`, "DUP", "different", 33)
		convey.So(err, convey.ShouldBeNil)

		samples, err := client.FindSamplesBySangerID(context.Background(), "DUP")

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(samples, convey.ShouldBeNil)
	})
}

func TestFindSamplesByLibraryTypeReturnsCanonicalSampleSlice(t *testing.T) {
	convey.Convey("Given a cache with one sample linked to a library type", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedSyncState(t, client.cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 11, 14, 2, 0, 0, time.UTC))
		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 11, 14, 3, 0, 0, time.UTC))
		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 1, "6568", "S1")
		seedLibrarySample(t, client.cache.DB(), "Standard", 1, "6568")

		samples, err := client.FindSamplesByLibraryType(context.Background(), "Standard")

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].IDSampleTmp, convey.ShouldEqual, int64(1))
		convey.So(samples[0].Name, convey.ShouldEqual, "S1")
		convey.So(samples[0].Libraries, convey.ShouldResemble, []Library{{PipelineIDLims: "Standard", IDStudyLims: "6568"}})
		convey.So(samples[0].Studies, convey.ShouldHaveLength, 1)
		convey.So(samples[0].Studies[0].IDStudyLims, convey.ShouldEqual, "6568")
	})
}

func TestSamplesForStudyHydratesPerPairingSampleFanOut(t *testing.T) {
	convey.Convey("Given a study sample linked to two library-study pairings", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 81, "6568")
		seedHierarchyStudy(t, client.cache.DB(), 82, "6569")
		seedHierarchySample(t, client.cache.DB(), 1, "6568", "S1")
		seedLibrarySample(t, client.cache.DB(), "Standard", 1, "6568")
		seedLibrarySample(t, client.cache.DB(), "Chromium", 1, "6569")

		samples, err := client.SamplesForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].Libraries, convey.ShouldResemble, []Library{{PipelineIDLims: "Standard", IDStudyLims: "6568"}, {PipelineIDLims: "Chromium", IDStudyLims: "6569"}})
		convey.So(samples[0].Studies, convey.ShouldHaveLength, 2)
		convey.So(samples[0].Studies[0].IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(samples[0].Studies[1].IDStudyLims, convey.ShouldEqual, "6569")
	})
}

func TestFindSamplesByMethodsColdCacheReturnErrCacheNeverSynced(t *testing.T) {
	convey.Convey("Given a never-synced cache", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		lookups := []struct {
			name string
			find func(context.Context) ([]Sample, error)
		}{
			{name: "sanger id", find: func(ctx context.Context) ([]Sample, error) { return client.FindSamplesBySangerID(ctx, "S1") }},
			{name: "sample lims id", find: func(ctx context.Context) ([]Sample, error) { return client.FindSamplesByIDSampleLims(ctx, "123") }},
			{name: "accession", find: func(ctx context.Context) ([]Sample, error) { return client.FindSamplesByAccessionNumber(ctx, "ACC") }},
			{name: "supplier", find: func(ctx context.Context) ([]Sample, error) { return client.FindSamplesBySupplierName(ctx, "SUP") }},
			{name: "library type", find: func(ctx context.Context) ([]Sample, error) { return client.FindSamplesByLibraryType(ctx, "Standard") }},
		}

		for _, lookup := range lookups {
			samples, err := lookup.find(context.Background())

			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(samples, convey.ShouldBeNil)
		}
	})
}

func TestExpandIdentifierStudyReturnsSortedTaggedIDs(t *testing.T) {
	convey.Convey("Given a study with two samples and three distinct lanes", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 11, "6568", "B")
		seedHierarchySample(t, client.cache.DB(), 12, "6568", "A")
		seedLibrarySample(t, client.cache.DB(), "Standard", 11, "6568")
		seedLibrarySample(t, client.cache.DB(), "Standard", 12, "6568")

		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6001, 12, 101, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6002, 12, 100, 2, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6003, 11, 100, 1, 0, "6568")

		taggedIDs, err := client.ExpandIdentifier(context.Background(), KindStudyLimsID, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(taggedIDs, convey.ShouldResemble, []TaggedID{
			{Kind: KindStudyLimsID, Canonical: "6568"},
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindSangerSampleName, Canonical: "B"},
			{Kind: KindRunID, Canonical: "100"},
			{Kind: KindRunID, Canonical: "101"},
		})
	})
}

func TestExpandSearchValuesStudyReturnsNamedSearchValues(t *testing.T) {
	convey.Convey("Given a warm cache for a study with two samples and three distinct lanes", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 11, "6568", "B")
		seedHierarchySample(t, client.cache.DB(), 12, "6568", "A")
		seedLibrarySample(t, client.cache.DB(), "Standard", 11, "6568")
		seedLibrarySample(t, client.cache.DB(), "Standard", 12, "6568")

		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6201, 12, 101, 1, 1, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6202, 12, 100, 2, 1, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6203, 11, 100, 1, 2, "6568")

		values, err := client.ExpandSearchValues(context.Background(), KindStudyLimsID, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(values.Samples, convey.ShouldHaveLength, 2)
		convey.So(values.Samples, convey.ShouldContain, "A")
		convey.So(values.Samples, convey.ShouldContain, "B")
		convey.So(values.Runs, convey.ShouldHaveLength, 2)
		convey.So(values.Runs, convey.ShouldContain, "100")
		convey.So(values.Runs, convey.ShouldContain, "101")
		convey.So(values.Lanes, convey.ShouldHaveLength, 3)
		convey.So(values.Lanes, convey.ShouldContain, "100_1#2")
		convey.So(values.Lanes, convey.ShouldContain, "100_2#1")
		convey.So(values.Lanes, convey.ShouldContain, "101_1#1")
	})
}

func TestExpandIdentifierStudyExcludesSiblingStudyRunsForSharedSample(t *testing.T) {
	convey.Convey("Given a shared sample sequenced in two studies", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchyStudy(t, client.cache.DB(), 2, "7777")
		seedSampleMirrorRow(t, client.cache.DB(), 21, "A", "supplier-21", "donor-21", time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC))
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "6568")
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "7777")

		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6101, 21, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6102, 21, 200, 1, 0, "7777")

		taggedIDs, err := client.ExpandIdentifier(context.Background(), KindStudyLimsID, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(taggedIDs, convey.ShouldResemble, []TaggedID{
			{Kind: KindStudyLimsID, Canonical: "6568"},
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindRunID, Canonical: "100"},
		})
	})
}

func TestSamplesForStudyPaginationIsDeterministicForTiedNames(t *testing.T) {
	convey.Convey("Given two study samples sharing the same name", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 11, "6568", "NAME")
		seedHierarchySample(t, client.cache.DB(), 12, "6568", "NAME")
		seedLibrarySample(t, client.cache.DB(), "Standard", 11, "6568")
		seedLibrarySample(t, client.cache.DB(), "Standard", 12, "6568")

		firstPage, firstErr := client.SamplesForStudy(context.Background(), "6568", 1, 0)
		secondPage, secondErr := client.SamplesForStudy(context.Background(), "6568", 1, 1)

		convey.So(firstErr, convey.ShouldBeNil)
		convey.So(secondErr, convey.ShouldBeNil)
		convey.So(firstPage, convey.ShouldHaveLength, 1)
		convey.So(secondPage, convey.ShouldHaveLength, 1)
		convey.So(firstPage[0].Name, convey.ShouldEqual, "NAME")
		convey.So(secondPage[0].Name, convey.ShouldEqual, "NAME")
		convey.So(firstPage[0].IDSampleTmp, convey.ShouldEqual, int64(11))
		convey.So(secondPage[0].IDSampleTmp, convey.ShouldEqual, int64(12))
	})
}

func TestSamplePaginationQueriesOrderByNameThenPrimaryKey(t *testing.T) {
	convey.Convey("Given the paginated sample hierarchy queries", t, func() {
		orderBySuffix := "ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?"

		convey.So(strings.Contains(samplesForStudyCacheSQL, orderBySuffix), convey.ShouldBeTrue)
		convey.So(strings.Contains(samplesForLibraryCacheSQL, orderBySuffix), convey.ShouldBeTrue)
		convey.So(strings.Contains(samplesForLibraryTypeCacheSQL, orderBySuffix), convey.ShouldBeTrue)
		convey.So(strings.Contains(samplesForRunCacheSQL, orderBySuffix), convey.ShouldBeTrue)
	})
}

func TestFindSamplesByLibraryTypeQueryOrdersByNameThenPrimaryKey(t *testing.T) {
	convey.Convey("Given the library-type finder query", t, func() {
		convey.So(
			strings.Contains(findSamplesByLibraryTypeSQL, "ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT 2"),
			convey.ShouldBeTrue,
		)
	})
}

func TestExpandIdentifierLibraryReturnsOriginalSamplesAndRuns(t *testing.T) {
	convey.Convey("Given a synced library spanning two studies", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchyStudy(t, client.cache.DB(), 2, "7777")
		seedHierarchySample(t, client.cache.DB(), 21, "6568", "B")
		seedHierarchySample(t, client.cache.DB(), 22, "7777", "A")
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "6568")
		seedLibrarySample(t, client.cache.DB(), "Standard", 22, "7777")
		seedSyncState(t, client.cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 18, 30, 0, 0, time.UTC))

		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 7001, 22, 100, 1, 0, "7777")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 7002, 21, 101, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 7003, 21, 100, 2, 0, "6568")

		taggedIDs, err := client.ExpandIdentifier(context.Background(), KindLibraryType, "Standard")

		convey.So(err, convey.ShouldBeNil)
		convey.So(taggedIDs, convey.ShouldResemble, []TaggedID{
			{Kind: KindLibraryType, Canonical: "Standard"},
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindSangerSampleName, Canonical: "B"},
			{Kind: KindRunID, Canonical: "100"},
			{Kind: KindRunID, Canonical: "101"},
		})
	})
}

func TestExpandIdentifierLibraryIDUsesExactIdentifierIndex(t *testing.T) {
	convey.Convey("Given synced library samples with different library IDs under the same type", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "7607")
		seedHierarchySample(t, client.cache.DB(), 31, "7607", "MATCH")
		seedHierarchySample(t, client.cache.DB(), 32, "7607", "OTHER")
		_, err := client.cache.DB().Exec(
			`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims, library_id, id_library_lims) VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)`,
			"Custom", 31, "7607", "71046409", "SQPP-47463-G:B1",
			"Custom", 32, "7607", "99999999", "SQPP-OTHER",
		)
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, client.cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 18, 30, 0, 0, time.UTC))

		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 7201, 31, 100, 1, 0, "7607")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 7202, 32, 200, 1, 0, "7607")

		values, err := client.ExpandSearchValues(context.Background(), KindLibraryID, "71046409")

		convey.So(err, convey.ShouldBeNil)
		convey.So(values, convey.ShouldResemble, SearchValues{
			Samples: []string{"MATCH"},
			Runs:    []string{"100"},
			Lanes:   []string{},
		})
	})
}

func TestExpandIdentifierLibraryPreservesSharedSampleStudyPairings(t *testing.T) {
	convey.Convey("Given one sample linked to the same library in two studies", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchyStudy(t, client.cache.DB(), 2, "7777")
		seedSampleMirrorRow(t, client.cache.DB(), 21, "A", "supplier-21", "donor-21", time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC))
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "6568")
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "7777")
		seedSyncState(t, client.cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 18, 30, 0, 0, time.UTC))

		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 7101, 21, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 7102, 21, 200, 1, 0, "7777")

		taggedIDs, err := client.ExpandIdentifier(context.Background(), KindLibraryType, "Standard")

		convey.So(err, convey.ShouldBeNil)
		convey.So(taggedIDs, convey.ShouldResemble, []TaggedID{
			{Kind: KindLibraryType, Canonical: "Standard"},
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindRunID, Canonical: "100"},
			{Kind: KindRunID, Canonical: "200"},
		})
	})
}

func TestExpandIdentifierSampleReturnsOriginalAndDistinctRuns(t *testing.T) {
	convey.Convey("Given a sample with duplicate lanes on one run", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 31, "6568", "A")

		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 8001, 31, 101, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 8002, 31, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 8003, 31, 100, 2, 0, "6568")

		taggedIDs, err := client.ExpandIdentifier(context.Background(), KindSangerSampleName, "A")

		convey.So(err, convey.ShouldBeNil)
		convey.So(taggedIDs, convey.ShouldResemble, []TaggedID{
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindRunID, Canonical: "100"},
			{Kind: KindRunID, Canonical: "101"},
		})
	})
}

func TestExpandSearchValuesDirectSampleMetadataResolvesCanonicalSample(t *testing.T) {
	convey.Convey("Given a sample with direct MLWH metadata", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedSampleMirrorRow(t, client.cache.DB(), 41, "SANG-DIRECT", "Supplier Direct", "donor-41", time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC))
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 8801, 41, 555, 1, 9, "6568")

		values, err := client.ExpandSearchValues(context.Background(), KindSampleLimsID, "141")

		convey.So(err, convey.ShouldBeNil)
		convey.So(values, convey.ShouldResemble, SearchValues{
			Samples: []string{"SANG-DIRECT"},
			Runs:    []string{"555"},
			Lanes:   []string{"555_1#9"},
		})

		values, err = client.ExpandSearchValues(context.Background(), KindSupplierName, "Supplier Direct")

		convey.So(err, convey.ShouldBeNil)
		convey.So(values, convey.ShouldResemble, SearchValues{
			Samples: []string{"SANG-DIRECT"},
			Runs:    []string{"555"},
			Lanes:   []string{"555_1#9"},
		})
	})
}

func TestExpandSampleSearchValuesDirectMetadataReturnsNamesWithoutLaneExpansion(t *testing.T) {
	convey.Convey("Given multiple samples sharing supplier metadata and no lane sync state", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 12, 34, 0, 0, time.UTC))
		seedSampleMirrorRow(t, client.cache.DB(), 51, "SANG-SUPPLIER-1", "Hek_R1", "donor-51", time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC))
		seedSampleMirrorRow(t, client.cache.DB(), 52, "SANG-SUPPLIER-2", "Hek_R1", "donor-52", time.Date(2026, time.May, 6, 12, 6, 0, 0, time.UTC))

		samples, err := client.ExpandSampleSearchValues(context.Background(), KindSupplierName, "Hek_R1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldResemble, []string{"SANG-SUPPLIER-1", "SANG-SUPPLIER-2"})

		samples, err = client.ExpandSampleSearchValues(context.Background(), KindSampleLimsID, "151")

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldResemble, []string{"SANG-SUPPLIER-1"})
	})
}

func TestExpandIdentifierRunReturnsOriginalAndDistinctSamples(t *testing.T) {
	convey.Convey("Given a run with duplicate product metrics for a sample", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()
		seedSyncState(t, client.cache.DB(), syncTableIseqProductMetrics, time.Date(2026, time.May, 6, 12, 33, 0, 0, time.UTC))
		seedHierarchySample(t, client.cache.DB(), 1, "6568", "B")
		seedHierarchySample(t, client.cache.DB(), 2, "6568", "A")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 9001, 1, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 9002, 2, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 9003, 2, 100, 2, 0, "6568")

		taggedIDs, err := client.ExpandIdentifier(context.Background(), KindRunID, "100")

		convey.So(err, convey.ShouldBeNil)
		convey.So(taggedIDs, convey.ShouldResemble, []TaggedID{
			{Kind: KindRunID, Canonical: "100"},
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindSangerSampleName, Canonical: "B"},
		})
	})
}

func TestExpandIdentifierCacheInvalidatedAfterSyncCommit(t *testing.T) {
	convey.Convey("Given a cached expand result and a successful sync commit", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 1, "6568", "A")
		seedLibrarySample(t, client.cache.DB(), "Standard", 1, "6568")

		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 10001, 1, 100, 1, 0, "6568")

		first, err := client.ExpandIdentifier(context.Background(), KindStudyLimsID, "6568")
		convey.So(err, convey.ShouldBeNil)
		convey.So(first, convey.ShouldResemble, []TaggedID{
			{Kind: KindStudyLimsID, Canonical: "6568"},
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindRunID, Canonical: "100"},
		})

		client.clearExpandIdentifierCache()

		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 10002, 1, 100, 1, 0, "6568")

		second, err := client.ExpandIdentifier(context.Background(), KindStudyLimsID, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(second, convey.ShouldResemble, first)
	})
}

func newHierarchyTestClient(t *testing.T) (*Client, sqlmock.Sqlmock, func()) {
	t.Helper()

	cache := openSQLiteSyncTestCache(t)
	sourceDB, sourceMock, err := sqlmock.New()
	if err != nil {
		_ = cache.Close()
		t.Fatalf("sqlmock.New(): %v", err)
	}

	client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

	cleanup := func() {
		sourceMock.ExpectClose()
		convey.So(sourceDB.Close(), convey.ShouldBeNil)
		convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)
	}

	return client, sourceMock, cleanup
}

func seedHierarchyStudy(t *testing.T, db *sql.DB, idStudyTmp int64, idStudyLims string) {
	t.Helper()

	rowValues := studyRowValues(idStudyTmp, sqscpIDLims, idStudyLims, "study-uuid-"+idStudyLims, "Study "+idStudyLims, "EGAS0000"+idStudyLims, time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC))
	args := make([]any, len(rowValues))
	for index, value := range rowValues {
		args[index] = value
	}

	_, err := db.Exec(
		`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		args...,
	)
	if err != nil {
		t.Fatalf("seedHierarchyStudy(): %v", err)
	}
}

func seedHierarchySample(t *testing.T, db *sql.DB, idSampleTmp int64, idStudyLims, name string) {
	t.Helper()

	seedSampleMirrorRow(t, db, idSampleTmp, name, "supplier-"+formatInt(idSampleTmp), "donor-"+formatInt(idSampleTmp), time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC))
	seedDonorSampleRow(t, db, "donor-"+formatInt(idSampleTmp), idSampleTmp, idStudyLims)
}

func seedLibrarySample(t *testing.T, db *sql.DB, pipelineIDLims string, idSampleTmp int64, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, pipelineIDLims, idSampleTmp, idStudyLims)
	if err != nil {
		t.Fatalf("seedLibrarySample(): %v", err)
	}
}

func seedIseqProductMetricsMirrorRow(t *testing.T, db *sql.DB, idIseqProduct, idSampleTmp int64, idRun, position, tagIndex int, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idIseqProduct,
		idSampleTmp,
		idRun,
		position,
		tagIndex,
		idSampleTmp,
		idStudyLims,
		1,
		1,
		1,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedIseqProductMetricsMirrorRow(): %v", err)
	}
}

func seedIRODSLocationMirrorRow(t *testing.T, db *sql.DB, idIseqProduct string, collection, fileName string, idSampleTmp int64, idStudyLims string) {
	t.Helper()

	created := time.Date(2026, time.May, 6, 12, 11, 0, 0, time.UTC)
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, idIseqProduct, collection, fileName, idSampleTmp, idStudyLims, created, "illumina")
}

// seedPacBioProductMetricsMirrorRow inserts a PacBio product-metrics mirror row
// linking a sample to a study, so the sample's canonical platform derives from
// pac_bio_product_metrics membership (PacBio) per the availability fan-out.
func seedPacBioProductMetricsMirrorRow(t *testing.T, db *sql.DB, idPacBioProduct string, idSampleTmp int64, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO pac_bio_product_metrics_mirror(id_pac_bio_product, id_pac_bio_rw_metrics_tmp, id_sample_tmp, id_study_lims, qc, last_updated) VALUES (?, ?, ?, ?, ?, ?)`,
		idPacBioProduct,
		idSampleTmp,
		idSampleTmp,
		idStudyLims,
		1,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedPacBioProductMetricsMirrorRow(): %v", err)
	}
}

// seedOseqFlowcellMirrorRow inserts an ONT identity row (oseq_flowcell_mirror)
// linking a sample to a study. ONT carries no products, iRODS, or QC, so the
// sample's platforms resolve to ["ONT"] from this membership alone.
func seedOseqFlowcellMirrorRow(t *testing.T, db *sql.DB, idOseqFlowcellTmp, idSampleTmp int64, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO oseq_flowcell_mirror(id_oseq_flowcell_tmp, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`,
		idOseqFlowcellTmp,
		idSampleTmp,
		idStudyLims,
	)
	if err != nil {
		t.Fatalf("seedOseqFlowcellMirrorRow(): %v", err)
	}
}

// seedIRODSLocationMirrorRowWithCreatedPlatform inserts an iRODS location mirror
// row with explicit created and platform values. seedIRODSLocationMirrorRow
// wraps it with sensible Illumina defaults so existing callers stay unchanged.
func seedIRODSLocationMirrorRowWithCreatedPlatform(t *testing.T, db *sql.DB, idIseqProduct string, collection, fileName string, idSampleTmp int64, idStudyLims string, created time.Time, platform string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO seq_product_irods_locations_mirror(id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idIseqProduct,
		"/seq",
		fileName,
		collection,
		fileName,
		idSampleTmp,
		idStudyLims,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 11, 0, 0, time.UTC)),
		formatSyncTime(created),
		platform,
	)
	if err != nil {
		t.Fatalf("seedIRODSLocationMirrorRow(): %v", err)
	}
}
