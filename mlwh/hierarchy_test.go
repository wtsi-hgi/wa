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
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/smartystreets/goconvey/convey"
)

var (
	samplesForStudyCacheQuery         = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_study_lims = ? ORDER BY sample_mirror.name LIMIT ? OFFSET ?`
	samplesForStudyParentQuery        = `SELECT 1 FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`
	samplesForStudySourceParentQuery  = `SELECT ` + studyMirrorSelectColumns + ` FROM study WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`
	samplesForStudySourceQuery        = `SELECT DISTINCT iseq_flowcell.pipeline_id_lims, sample.id_sample_tmp, sample.id_lims, sample.id_sample_lims, sample.uuid_sample_lims, iseq_flowcell.id_study_lims, sample.name, sample.name AS sanger_id, sample.sanger_sample_id, sample.supplier_name, sample.accession_number, sample.donor_id, iseq_flowcell.pipeline_id_lims AS library_type, sample.taxon_id, sample.common_name, sample.description, sample.last_updated FROM iseq_flowcell INNER JOIN sample ON sample.id_sample_tmp = iseq_flowcell.id_sample_tmp WHERE iseq_flowcell.id_study_lims = ? AND sample.id_lims = 'SQSCP' ORDER BY sample.name LIMIT ? OFFSET ?`
	sampleByNameCacheQuery            = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`
	samplesForRunQuery                = `SELECT DISTINCT sample.id_sample_tmp, sample.id_lims, sample.id_sample_lims, sample.uuid_sample_lims, iseq_flowcell.id_study_lims, sample.name, sample.name AS sanger_id, sample.sanger_sample_id, sample.supplier_name, sample.accession_number, sample.donor_id, iseq_flowcell.pipeline_id_lims AS library_type, sample.taxon_id, sample.common_name, sample.description FROM iseq_product_metrics INNER JOIN iseq_flowcell ON iseq_flowcell.id_iseq_flowcell_tmp = iseq_product_metrics.id_iseq_flowcell_tmp INNER JOIN sample ON sample.id_sample_tmp = iseq_flowcell.id_sample_tmp WHERE iseq_product_metrics.id_run = ? AND sample.id_lims = 'SQSCP' ORDER BY sample.name LIMIT ? OFFSET ?`
	samplesForRunParentQuery          = `SELECT id_run FROM iseq_product_metrics WHERE id_run = ? LIMIT 1`
	samplesForLibraryTypeSourceQuery  = `SELECT DISTINCT iseq_flowcell.pipeline_id_lims, sample.id_sample_tmp, sample.id_lims, sample.id_sample_lims, sample.uuid_sample_lims, COALESCE(study.id_study_lims, ''), sample.name, sample.name AS sanger_id, sample.sanger_sample_id, sample.supplier_name, sample.accession_number, sample.donor_id, iseq_flowcell.pipeline_id_lims AS library_type, sample.taxon_id, sample.common_name, sample.description, sample.last_updated FROM iseq_flowcell LEFT JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp INNER JOIN sample ON sample.id_sample_tmp = iseq_flowcell.id_sample_tmp WHERE iseq_flowcell.pipeline_id_lims = ? AND (study.id_lims = 'SQSCP' OR study.id_lims IS NULL) AND sample.id_lims = 'SQSCP' ORDER BY sample.name LIMIT ? OFFSET ?`
	samplesForLibraryCacheQuery       = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND library_samples.id_study_lims = ? ORDER BY sample_mirror.name LIMIT ? OFFSET ?`
	samplesForLibraryStudyParentQuery = `SELECT 1 FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`
)

const (
	runsForStudyQuery        = `SELECT DISTINCT ipm.id_run FROM iseq_flowcell ifc INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_flowcell_tmp = ifc.id_iseq_flowcell_tmp WHERE ifc.id_study_tmp = ? ORDER BY ipm.id_run LIMIT ? OFFSET ?`
	lanesForSampleQuery      = `SELECT DISTINCT ipm.id_run, ipm.position, ipm.tag_index FROM iseq_flowcell ifc INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_flowcell_tmp = ifc.id_iseq_flowcell_tmp WHERE ifc.id_sample_tmp = ? ORDER BY ipm.id_run, ipm.position, ipm.tag_index LIMIT ? OFFSET ?`
	irodsPathsForSampleQuery = `SELECT spi.id_product, spi.collection, spi.data_object FROM iseq_flowcell ifc INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_flowcell_tmp = ifc.id_iseq_flowcell_tmp INNER JOIN seq_product_irods_locations spi ON spi.id_product = ipm.id_iseq_product WHERE ifc.id_sample_tmp = ? ORDER BY spi.id_product LIMIT ? OFFSET ?`
	irodsPathsForStudyQuery  = `SELECT spi.id_product, spi.collection, spi.data_object FROM iseq_flowcell ifc INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_flowcell_tmp = ifc.id_iseq_flowcell_tmp INNER JOIN seq_product_irods_locations spi ON spi.id_product = ipm.id_iseq_product WHERE ifc.id_study_tmp = ? ORDER BY spi.id_product LIMIT ? OFFSET ?`
)

func TestSamplesForStudyWarmCacheUsesJoinOnly(t *testing.T) {
	convey.Convey("Given a warm cache for a study with three linked samples", t, func() {
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()
		_ = sourceMock

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

func TestSamplesForStudyColdCacheMissingParentReturnsErrNotFound(t *testing.T) {
	convey.Convey("Given a cold cache and no upstream study row", t, func() {
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).
			WithArgs("6568", 100, 0).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableStudy).
			WillReturnRows(sqlmock.NewRows([]string{"found"}))
		sourceMock.ExpectQuery(regexp.QuoteMeta(samplesForStudySourceParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows(studyResolverColumns()))

		samples, err := client.SamplesForStudy(context.Background(), "6568", 100, 0)

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
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
		client, sourceMock, cleanup := newResolverSampleTestClient(t)
		defer cleanup()

		sourceMock.ExpectQuery(regexp.QuoteMeta(samplesForRunQuery)).
			WithArgs(12345, 100, 0).
			WillReturnRows(
				sqlmock.NewRows(sampleResolverColumns()).
					AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "Alpha", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...).
					AddRow(sampleResolverRow(2, "sample-uuid-2", "502", "Beta", "sanger-id-2", "supplier-2", "accession-2", "donor-2")...),
			)

		samples, err := client.SamplesForRun(context.Background(), "12345", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 2)
		convey.So(samples[0].Name, convey.ShouldEqual, "Alpha")
		convey.So(samples[1].Name, convey.ShouldEqual, "Beta")
	})
}

func TestSamplesForRunHonoursLimitOffset(t *testing.T) {
	convey.Convey("Given a five-row run source queried with limit and offset", t, func() {
		client, sourceMock, cleanup := newResolverSampleTestClient(t)
		defer cleanup()

		sourceMock.ExpectQuery(regexp.QuoteMeta(samplesForRunQuery)).
			WithArgs(12345, 2, 1).
			WillReturnRows(
				sqlmock.NewRows(sampleResolverColumns()).
					AddRow(sampleResolverRow(2, "sample-uuid-2", "502", "Beta", "sanger-id-2", "supplier-2", "accession-2", "donor-2")...).
					AddRow(sampleResolverRow(3, "sample-uuid-3", "503", "Gamma", "sanger-id-3", "supplier-3", "accession-3", "donor-3")...),
			)

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

func TestSamplesForStudyAndLibraryReadThroughWriteBack(t *testing.T) {
	convey.Convey("Given a cold cache with upstream rows for study and library traversals", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			sourceMock.ExpectClose()
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		studyRows := sqlmock.NewRows(studyResolverColumns()).AddRow(studyResolverRow(11, "6568", "study-uuid-11", "Study 11", "EGAS00001001111")...)
		sourceMock.ExpectQuery(regexp.QuoteMeta(samplesForStudySourceParentQuery)).WithArgs("6568").WillReturnRows(studyRows)
		sourceMock.ExpectQuery(regexp.QuoteMeta(samplesForStudySourceQuery)).
			WithArgs("6568", 2, 1).
			WillReturnRows(
				sqlmock.NewRows([]string{"pipeline_id_lims", "id_sample_tmp", "id_lims", "id_sample_lims", "uuid_sample_lims", "id_study_lims", "name", "sanger_id", "sanger_sample_id", "supplier_name", "accession_number", "donor_id", "library_type", "taxon_id", "common_name", "description", "last_updated"}).
					AddRow("Standard", int64(1), "SQSCP", "501", "sample-uuid-1", "6568", "Alpha", "Alpha", "sanger-id-1", "supplier-1", "accession-1", "donor-1", "Standard", 9606, "human", "description", formatSyncTime(time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))).
					AddRow("Standard", int64(2), "SQSCP", "502", "sample-uuid-2", "6568", "Beta", "Beta", "sanger-id-2", "supplier-2", "accession-2", "donor-2", "Standard", 9606, "human", "description", formatSyncTime(time.Date(2026, time.May, 6, 17, 1, 0, 0, time.UTC))),
			)

		studySamples, err := client.SamplesForStudy(context.Background(), "6568", 2, 1)

		convey.So(err, convey.ShouldBeNil)
		convey.So(studySamples, convey.ShouldHaveLength, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror WHERE id_sample_tmp IN (1, 2)`), convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples WHERE pipeline_id_lims = ? AND id_study_lims = ?`, "Standard", "6568"), convey.ShouldEqual, 2)

		librarySamples, err := client.SamplesForLibrary(context.Background(), "Standard", "6568", 2, 1)

		convey.So(err, convey.ShouldBeNil)
		convey.So(librarySamples, convey.ShouldHaveLength, 1)
		convey.So(librarySamples[0].Name, convey.ShouldEqual, "Beta")
	})
}

func TestSamplesForLibraryColdCacheExistingStudyWithoutMatchingLibraryReturnsEmptySlice(t *testing.T) {
	convey.Convey("Given a cold cache and an upstream study with no samples for the requested library", t, func() {
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryCacheQuery)).
			WithArgs("Standard", "6568", 2, 1).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryStudyParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows([]string{"found"}))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableStudy).
			WillReturnRows(sqlmock.NewRows([]string{"found"}))

		sourceMock.ExpectQuery(regexp.QuoteMeta(samplesForStudySourceParentQuery)).
			WithArgs("6568").
			WillReturnRows(sqlmock.NewRows(studyResolverColumns()).AddRow(studyResolverRow(11, "6568", "study-uuid-11", "Study 11", "EGAS00001001111")...))
		sourceMock.ExpectQuery(regexp.QuoteMeta(samplesForLibrarySourceSQL)).
			WithArgs("Standard", "6568", 2, 1).
			WillReturnRows(sqlmock.NewRows([]string{"pipeline_id_lims", "id_sample_tmp", "id_lims", "id_sample_lims", "uuid_sample_lims", "id_study_lims", "name", "sanger_id", "sanger_sample_id", "supplier_name", "accession_number", "donor_id", "library_type", "taxon_id", "common_name", "description", "last_updated"}))

		samples, err := client.SamplesForLibrary(context.Background(), "Standard", "6568", 2, 1)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldResemble, []Sample{})
	})
}

func TestSamplesForLibraryTypeColdCacheReadsThroughWithoutResolverSync(t *testing.T) {
	convey.Convey("Given a cold cache and upstream library-type rows, then SamplesForLibraryType reads through directly without a resolver sync gate", t, func() {
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		sourceMock.ExpectQuery(regexp.QuoteMeta(samplesForLibraryTypeSourceQuery)).
			WithArgs("Chromium single cell 3 prime v3", 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{
				"pipeline_id_lims",
				"id_sample_tmp",
				"id_lims",
				"id_sample_lims",
				"uuid_sample_lims",
				"id_study_lims",
				"name",
				"sanger_id",
				"sanger_sample_id",
				"supplier_name",
				"accession_number",
				"donor_id",
				"library_type",
				"taxon_id",
				"common_name",
				"description",
				"last_updated",
			}).AddRow(
				"Chromium single cell 3 prime v3",
				int64(11),
				"SQSCP",
				"501",
				"sample-uuid-11",
				"6568",
				"sample-a",
				"sample-a",
				"sanger-id-a",
				"supplier-a",
				"accession-a",
				"donor-a",
				"Chromium single cell 3 prime v3",
				9606,
				"human",
				"desc-a",
				formatSyncTime(time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)),
			))

		samples, err := client.SamplesForLibraryType(context.Background(), "Chromium single cell 3 prime v3", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].Name, convey.ShouldEqual, "sample-a")
		convey.So(samples[0].IDStudyLims, convey.ShouldEqual, "6568")
	})
}

func TestSamplesForLibraryTypeColdCacheUsesJoinedStudyLimsSourceQuery(t *testing.T) {
	convey.Convey("Given a cold cache and a library-type read-through", t, func() {
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		query := `SELECT DISTINCT iseq_flowcell.pipeline_id_lims, sample.id_sample_tmp, sample.id_lims, sample.id_sample_lims, sample.uuid_sample_lims, COALESCE(study.id_study_lims, ''), sample.name, sample.name AS sanger_id, sample.sanger_sample_id, sample.supplier_name, sample.accession_number, sample.donor_id, iseq_flowcell.pipeline_id_lims AS library_type, sample.taxon_id, sample.common_name, sample.description, sample.last_updated FROM iseq_flowcell LEFT JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp INNER JOIN sample ON sample.id_sample_tmp = iseq_flowcell.id_sample_tmp WHERE iseq_flowcell.pipeline_id_lims = ? AND (study.id_lims = 'SQSCP' OR study.id_lims IS NULL) AND sample.id_lims = 'SQSCP' ORDER BY sample.name LIMIT ? OFFSET ?`

		sourceMock.ExpectQuery(regexp.QuoteMeta(query)).
			WithArgs("Chromium single cell 3 prime v3", 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{
				"pipeline_id_lims",
				"id_sample_tmp",
				"id_lims",
				"id_sample_lims",
				"uuid_sample_lims",
				"id_study_lims",
				"name",
				"sanger_id",
				"sanger_sample_id",
				"supplier_name",
				"accession_number",
				"donor_id",
				"library_type",
				"taxon_id",
				"common_name",
				"description",
				"last_updated",
			}).AddRow(
				"Chromium single cell 3 prime v3",
				int64(11),
				"SQSCP",
				"501",
				"sample-uuid-11",
				"6568",
				"sample-a",
				"sample-a",
				"sanger-id-a",
				"supplier-a",
				"accession-a",
				"donor-a",
				"Chromium single cell 3 prime v3",
				9606,
				"human",
				"desc-a",
				formatSyncTime(time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)),
			))

		samples, err := client.SamplesForLibraryType(context.Background(), "Chromium single cell 3 prime v3", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].IDStudyLims, convey.ShouldEqual, "6568")
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

		runClient, runSourceMock, runCleanup := newResolverSampleTestClient(t)
		defer runCleanup()
		runSourceMock.ExpectQuery(regexp.QuoteMeta(samplesForRunQuery)).WithArgs(12345, 100, 0).WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		runSourceMock.ExpectQuery(regexp.QuoteMeta(samplesForRunParentQuery)).WithArgs(12345).WillReturnRows(sqlmock.NewRows([]string{"id_run"}))

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

		runClient, runSourceMock, runCleanup := newResolverSampleTestClient(t)
		defer runCleanup()
		runSourceMock.ExpectQuery(regexp.QuoteMeta(samplesForRunQuery)).WithArgs(12345, 100, 0).WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		runSourceMock.ExpectQuery(regexp.QuoteMeta(samplesForRunParentQuery)).WithArgs(12345).WillReturnRows(sqlmock.NewRows([]string{"id_run"}).AddRow(12345))

		runSamples, runErr := runClient.SamplesForRun(context.Background(), "12345", 100, 0)

		convey.So(runErr, convey.ShouldBeNil)
		convey.So(runSamples, convey.ShouldHaveLength, 0)

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
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
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
		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
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
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		roMock.ExpectQuery(regexp.QuoteMeta(samplesForStudyCacheQuery)).
			WithArgs("6568", 1000, 0).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "A", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...))
		roMock.ExpectQuery(regexp.QuoteMeta(sampleByNameCacheQuery)).
			WithArgs("A").
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "A", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...))
		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
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

		seedLibrarySample(t, client.cache.DB(), "Standard", 1, "6568")

		libraries, err := client.LibrariesForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(libraries, convey.ShouldResemble, []Library{
			{PipelineIDLims: "Bespoke", SampleCount: 3},
			{PipelineIDLims: "Standard", SampleCount: 10},
		})
	})
}

func TestRunsForStudyReturnsDistinctRunIDs(t *testing.T) {
	convey.Convey("Given a cached study with metrics rows spanning two runs", t, func() {
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")

		sourceMock.ExpectQuery(regexp.QuoteMeta(runsForStudyQuery)).
			WithArgs(int64(1), 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run"}).AddRow(100).AddRow(101))

		runs, err := client.RunsForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(runs, convey.ShouldResemble, []Run{{IDRun: 100}, {IDRun: 101}})
	})
}

func TestLanesForSampleReturnsOrderedLaneTriples(t *testing.T) {
	convey.Convey("Given a cached sample with three product-metrics rows", t, func() {
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 21, "6568", "7607STDY14643771")

		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(21), 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).
				AddRow(100, 1, 0).
				AddRow(100, 2, 0).
				AddRow(101, 1, 5))

		lanes, err := client.LanesForSample(context.Background(), "7607STDY14643771", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(lanes, convey.ShouldResemble, []Lane{{IDRun: 100, Position: 1, TagIndex: 0}, {IDRun: 100, Position: 2, TagIndex: 0}, {IDRun: 101, Position: 1, TagIndex: 5}})
	})
}

func TestIRODSPathsForSampleReturnsJoinedPaths(t *testing.T) {
	convey.Convey("Given a cached sample with two seq_product_irods_locations rows", t, func() {
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 31, "6568", "7607STDY14643771")

		sourceMock.ExpectQuery(regexp.QuoteMeta(irodsPathsForSampleQuery)).
			WithArgs(int64(31), 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_product", "collection", "data_object"}).
				AddRow("1234_1#1", "/seq/1234", "1234_1#1.cram").
				AddRow("1234_1#2", "/seq/1234", "1234_1#2.cram"))

		paths, err := client.IRODSPathsForSample(context.Background(), "7607STDY14643771", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(paths, convey.ShouldResemble, []IRODSPath{
			{IDProduct: "1234_1#1", Collection: "/seq/1234", DataObject: "1234_1#1.cram", IRODSPath: "/seq/1234/1234_1#1.cram"},
			{IDProduct: "1234_1#2", Collection: "/seq/1234", DataObject: "1234_1#2.cram", IRODSPath: "/seq/1234/1234_1#2.cram"},
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
		study, studyErr := client.StudyForSample(context.Background(), "7607STDY14643771")

		convey.So(errors.Is(librariesErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(runsErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(lanesErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(samplePathsErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(studyPathsErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(studyErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(libraries, convey.ShouldBeNil)
		convey.So(runs, convey.ShouldBeNil)
		convey.So(lanes, convey.ShouldBeNil)
		convey.So(samplePaths, convey.ShouldBeNil)
		convey.So(studyPaths, convey.ShouldBeNil)
		convey.So(study, convey.ShouldBeNil)
	})
}

func TestHierarchyMethodsReturnEmptySlicesForParentsWithoutChildren(t *testing.T) {
	convey.Convey("Given existing study and sample parents without child rows", t, func() {
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 41, "6568", "7607STDY14643771")

		sourceMock.ExpectQuery(regexp.QuoteMeta(runsForStudyQuery)).
			WithArgs(int64(1), 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run"}))
		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(41), 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}))
		sourceMock.ExpectQuery(regexp.QuoteMeta(irodsPathsForSampleQuery)).
			WithArgs(int64(41), 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_product", "collection", "data_object"}))
		sourceMock.ExpectQuery(regexp.QuoteMeta(irodsPathsForStudyQuery)).
			WithArgs(int64(1), 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_product", "collection", "data_object"}))

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
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 71, "6568")

		sourceMock.ExpectQuery(regexp.QuoteMeta(irodsPathsForStudyQuery)).
			WithArgs(int64(71), 100, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_product", "collection", "data_object"}).
				AddRow("5678_1#1", "/seq/5678", "5678_1#1.cram").
				AddRow("5678_1#2", "/seq/5678", "5678_1#2.cram"))

		paths, err := client.IRODSPathsForStudy(context.Background(), "6568", 100, 0)

		convey.So(err, convey.ShouldBeNil)
		convey.So(paths, convey.ShouldResemble, []IRODSPath{
			{IDProduct: "5678_1#1", Collection: "/seq/5678", DataObject: "5678_1#1.cram", IRODSPath: "/seq/5678/5678_1#1.cram"},
			{IDProduct: "5678_1#2", Collection: "/seq/5678", DataObject: "5678_1#2.cram", IRODSPath: "/seq/5678/5678_1#2.cram"},
		})
	})
}

func TestStudyForSampleReturnsLinkedStudy(t *testing.T) {
	convey.Convey("Given a cached sample linked to a cached study", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 81, "6568")
		seedHierarchySample(t, client.cache.DB(), 82, "6568", "7607STDY14643771")

		study, err := client.StudyForSample(context.Background(), "7607STDY14643771")

		convey.So(err, convey.ShouldBeNil)
		convey.So(study, convey.ShouldNotBeNil)
		convey.So(study.IDStudyLims, convey.ShouldEqual, "6568")
	})
}

func TestExpandIdentifierStudyReturnsSortedTaggedIDs(t *testing.T) {
	convey.Convey("Given a study with two samples and three distinct lanes", t, func() {
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 11, "6568", "B")
		seedHierarchySample(t, client.cache.DB(), 12, "6568", "A")
		seedLibrarySample(t, client.cache.DB(), "Standard", 11, "6568")
		seedLibrarySample(t, client.cache.DB(), "Standard", 12, "6568")

		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(12), 1000, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).AddRow(101, 1, 0).AddRow(100, 2, 0))
		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(11), 1000, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).AddRow(100, 1, 0))

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

func TestExpandIdentifierLibraryReturnsOriginalSamplesAndRuns(t *testing.T) {
	convey.Convey("Given a synced library spanning two studies", t, func() {
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchyStudy(t, client.cache.DB(), 2, "7777")
		seedHierarchySample(t, client.cache.DB(), 21, "6568", "B")
		seedHierarchySample(t, client.cache.DB(), 22, "7777", "A")
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "6568")
		seedLibrarySample(t, client.cache.DB(), "Standard", 22, "7777")
		seedSyncState(t, client.cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 18, 30, 0, 0, time.UTC))

		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(22), 1000, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).AddRow(100, 1, 0))
		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(21), 1000, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).AddRow(101, 1, 0).AddRow(100, 2, 0))

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
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchySample(t, client.cache.DB(), 31, "6568", "A")

		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(31), 1000, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).AddRow(101, 1, 0).AddRow(100, 1, 0).AddRow(100, 2, 0))

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
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		sourceMock.ExpectQuery(regexp.QuoteMeta(samplesForRunQuery)).
			WithArgs(100, 1000, 0).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "sample-uuid-1", "501", "B", "sanger-id-1", "supplier-1", "accession-1", "donor-1")...).
				AddRow(sampleResolverRow(2, "sample-uuid-2", "502", "A", "sanger-id-2", "supplier-2", "accession-2", "donor-2")...).
				AddRow(sampleResolverRow(2, "sample-uuid-2", "502", "A", "sanger-id-2", "supplier-2", "accession-2", "donor-2")...))

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
		client, sourceMock, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 1, "6568", "A")
		seedLibrarySample(t, client.cache.DB(), "Standard", 1, "6568")

		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(1), 1000, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).AddRow(100, 1, 0))

		first, err := client.ExpandIdentifier(context.Background(), KindStudyLimsID, "6568")
		convey.So(err, convey.ShouldBeNil)
		convey.So(first, convey.ShouldResemble, []TaggedID{
			{Kind: KindStudyLimsID, Canonical: "6568"},
			{Kind: KindSangerSampleName, Canonical: "A"},
			{Kind: KindRunID, Canonical: "100"},
		})

		var syncCalls atomic.Int32
		client.syncRunner = func(ctx context.Context, tx *sql.Tx, tables []string) error {
			syncCalls.Add(1)
			_, err := tx.ExecContext(ctx, `INSERT INTO sync_state(table_name, high_water, last_run) VALUES (?, ?, ?)`, "sample", formatSyncTime(time.Date(2026, time.May, 6, 19, 0, 0, 0, time.UTC)), formatSyncTime(time.Date(2026, time.May, 6, 19, 0, 0, 0, time.UTC)))
			return err
		}

		_, err = client.Sync(context.Background(), "sample")
		convey.So(err, convey.ShouldBeNil)
		convey.So(syncCalls.Load(), convey.ShouldEqual, 1)

		sourceMock.ExpectQuery(regexp.QuoteMeta(lanesForSampleQuery)).
			WithArgs(int64(1), 1000, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id_run", "position", "tag_index"}).AddRow(100, 1, 0))

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
		`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, egadac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
