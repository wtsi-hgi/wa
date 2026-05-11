/*******************************************************************************
 * Copyright (c) 2026 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk> - Updated
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to - Updated
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, - Updated
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE. - Updated
 ******************************************************************************/

package mlwh

import (
	"context"
	"database/sql"
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
	samplesForStudyCacheQuery         = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_study_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForStudyParentQuery        = `SELECT 1 FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`
	sampleByNameCacheQuery            = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`
	samplesForRunQuery                = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM iseq_product_metrics_mirror INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = iseq_product_metrics_mirror.id_sample_tmp WHERE iseq_product_metrics_mirror.id_run = ? ORDER BY sample_mirror.name LIMIT ? OFFSET ?`
	samplesForLibraryCacheQuery       = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND library_samples.id_study_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForLibraryStudyParentQuery = `SELECT 1 FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`
)

const (
	runsForStudyQuery        = `SELECT DISTINCT id_run FROM iseq_product_metrics_mirror WHERE id_study_lims = ? ORDER BY id_run LIMIT ? OFFSET ?`
	lanesForSampleQuery      = `SELECT DISTINCT id_run, position, tag_index FROM iseq_product_metrics_mirror WHERE id_sample_tmp = ? ORDER BY id_run, position, tag_index LIMIT ? OFFSET ?`
	irodsPathsForSampleQuery = `SELECT CAST(id_iseq_product AS TEXT), irods_collection, irods_file_name FROM seq_product_irods_locations_mirror WHERE id_sample_tmp = ? ORDER BY id_iseq_product LIMIT ? OFFSET ?`
	irodsPathsForStudyQuery  = `SELECT CAST(id_iseq_product AS TEXT), irods_collection, irods_file_name FROM seq_product_irods_locations_mirror WHERE id_study_lims = ? ORDER BY id_iseq_product LIMIT ? OFFSET ?`
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
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableStudy).
			WillReturnRows(sqlmock.NewRows([]string{"found"}))

		samples, err := client.SamplesForStudy(context.Background(), "6568", 100, 0)

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(samples, convey.ShouldBeNil)
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
			WillReturnRows(sqlmock.NewRows([]string{"found"}))
		studyROMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableStudy).
			WillReturnRows(sqlmock.NewRows([]string{"found"}))

		studySamples, studyErr := studyClient.SamplesForStudy(context.Background(), "6568", 2, 1)

		convey.So(errors.Is(studyErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(studyErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(studySamples, convey.ShouldBeNil)

		libraryClient, libraryROMock, librarySourceMock, libraryCleanup := newMySQLResolverTestClient(t)
		defer libraryCleanup()
		_ = librarySourceMock

		libraryROMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryCacheQuery)).
			WithArgs("Standard", "6568", 2, 1).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}))
		libraryROMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableStudy).
			WillReturnRows(sqlmock.NewRows([]string{"found"}))

		librarySamples, libraryErr := libraryClient.SamplesForLibrary(context.Background(), "Standard", "6568", 2, 1)

		convey.So(errors.Is(libraryErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(libraryErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(librarySamples, convey.ShouldBeNil)
	})
}

func TestSamplesForLibraryColdCacheWithoutSyncReturnsErrCacheNeverSynced(t *testing.T) {
	convey.Convey("Given a cold cache and no study sync state for the requested library", t, func() {
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()
		_ = sourceMock

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryCacheQuery)).
			WithArgs("Standard", "6568", 2, 1).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableStudy).
			WillReturnRows(sqlmock.NewRows([]string{"found"}))

		samples, err := client.SamplesForLibrary(context.Background(), "Standard", "6568", 2, 1)

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(samples, convey.ShouldBeNil)
	})
}

func TestSamplesForLibraryTypeColdCacheReturnsErrCacheNeverSynced(t *testing.T) {
	convey.Convey("Given a cold cache and no flowcell sync state", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		samples, err := client.SamplesForLibraryType(context.Background(), "Chromium single cell 3 prime v3", 100, 0)

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(samples, convey.ShouldBeNil)
	})
}

func TestSamplesForMethodsMissingParentsReturnErrNotFound(t *testing.T) {
	convey.Convey("Given missing parents for study, run, and library traversals", t, func() {
		studyClient, studyROMock, studySourceMock, studyCleanup := newMySQLResolverTestClient(t)
		defer studyCleanup()
		studyROMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).WithArgs("6568", 100, 0).WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		studyROMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyParentQuery)).WithArgs("6568").WillReturnRows(sqlmock.NewRows([]string{"found"}))
		studyROMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).WithArgs(syncTableStudy).WillReturnRows(sqlmock.NewRows([]string{"found"}).AddRow(1))

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
		libraryROMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).WithArgs(syncTableStudy).WillReturnRows(sqlmock.NewRows([]string{"found"}).AddRow(1))

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
		studyClient := &Client{cache: studyCache, cacheReader: cacheReadDB(studyCache), syncSource: studyCache.DB()}

		studySamples, studyErr := studyClient.SamplesForStudy(context.Background(), "6568", 100, 0)

		convey.So(studyErr, convey.ShouldBeNil)
		convey.So(studySamples, convey.ShouldHaveLength, 0)

		runClient, _, runCleanup := newHierarchyTestClient(t)
		defer runCleanup()

		runSamples, runErr := runClient.SamplesForRun(context.Background(), "12345", 100, 0)

		convey.So(errors.Is(runErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(runSamples, convey.ShouldBeNil)

		libraryCache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(libraryCache.Close(), convey.ShouldBeNil) }()
		seedStudyMirrorRow(t, libraryCache.DB(), 12, "6568", "study-uuid-12", "Study 12", "EGAS00001001112")
		seedSyncState(t, libraryCache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 18, 1, 0, 0, time.UTC))
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
		roMock.ExpectQuery(regexp.QuoteMeta(sampleByNameCacheQuery)).
			WithArgs("A").
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "A", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...))
		roMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(1), 1000, 0).
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
		roMock.ExpectQuery(regexp.QuoteMeta(sampleByNameCacheQuery)).
			WithArgs("A").
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "A", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...))
		roMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(1), 1000, 0).
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
		seedIRODSLocationMirrorRow(t, client.cache.DB(), 4001, "/seq/1234", "1234_1#1.cram", 31, "6568")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), 4002, "/seq/1234", "1234_1#2.cram", 31, "6568")

		paths, err := client.IRODSPathsForSample(context.Background(), "7607STDY14643771", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(paths, convey.ShouldResemble, []IRODSPath{
			{IDProduct: "4001", Collection: "/seq/1234", DataObject: "1234_1#1.cram", IRODSPath: "/seq/1234/1234_1#1.cram"},
			{IDProduct: "4002", Collection: "/seq/1234", DataObject: "1234_1#2.cram", IRODSPath: "/seq/1234/1234_1#2.cram"},
		})
	})
}

func TestHierarchyMethodsReturnErrNotFoundForMissingParents(t *testing.T) {
	convey.Convey("Given missing study and sample parents", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

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
		seedIRODSLocationMirrorRow(t, client.cache.DB(), 5001, "/seq/5678", "5678_1#1.cram", 91, "6568")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), 5002, "/seq/5678", "5678_1#2.cram", 92, "6568")

		paths, err := client.IRODSPathsForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(paths, convey.ShouldResemble, []IRODSPath{
			{IDProduct: "5001", Collection: "/seq/5678", DataObject: "5678_1#1.cram", IRODSPath: "/seq/5678/5678_1#1.cram"},
			{IDProduct: "5002", Collection: "/seq/5678", DataObject: "5678_1#2.cram", IRODSPath: "/seq/5678/5678_1#2.cram"},
		})
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
		seedIRODSLocationMirrorRow(t, cache.DB(), 1001, "/seq/1234", "1234_1#1.cram", 1, "6568")

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
		seedSyncState(t, client.cache.DB(), syncTableStudy, time.Date(2026, time.May, 11, 16, 0, 0, 0, time.UTC))

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

func seedIRODSLocationMirrorRow(t *testing.T, db *sql.DB, idIseqProduct int64, collection, fileName string, idSampleTmp int64, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO seq_product_irods_locations_mirror(id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		idIseqProduct,
		"/seq",
		fileName,
		collection,
		fileName,
		idSampleTmp,
		idStudyLims,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 11, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedIRODSLocationMirrorRow(): %v", err)
	}
}
