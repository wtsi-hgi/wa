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
	"net/http"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// manifestAllRows is the fetch-all limit used by the StudyManifest acceptance
// tests so a single call returns every product row.
const manifestAllRows = 1000

func TestStudyManifestNeverSyncedReturnsJoinedSentinelC1(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "S1", "", false, manifestAllRows, 0)

		convey.Convey("when StudyManifest runs, then it returns both sentinels and a zero-value envelope", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(manifest, convey.ShouldResemble, StudyManifest{})
		})
	})
}

func TestRemoteClientStudyManifestRoundTripsC1(t *testing.T) {
	convey.Convey("Given a RemoteClient over a server returning a StudyManifest envelope", t, func() {
		requestURIs := make(chan string, 1)
		expected := StudyManifest{
			IDStudyLims:     "S1",
			Name:            "Study S1",
			AccessionNumber: "EGAS0000S1",
			FacultySponsor:  "Faculty sponsor 211",
			DataAccessGroup: "group",
			Rows: []ManifestRow{
				{Name: "S1-sample-alpha", SupplierName: "supplier-alpha", AccessionNumber: "EGAN-alpha", SangerSampleID: "sanger-alpha", IDRun: 52553, Position: 1, TagIndex: 1},
			},
			CacheSyncedAt: "2026-06-27T06:00:00Z",
		}
		server := newRemoteClientJSONServerForTest(requestURIs, expected)
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when StudyManifest runs, then the path is /study/S1/manifest and it returns the server's envelope", func() {
			manifest, err := client.StudyManifest(context.Background(), "S1", "", false, manifestAllRows, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(manifest, convey.ShouldResemble, expected)
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldStartWith, "/study/S1/manifest")
		})
	})
}

func TestStudyManifestListsOneRowPerProductWithStudyMetadataC1(t *testing.T) {
	convey.Convey("Given study S1 with 3 Illumina products across 2 samples", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedManifestS1Scenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "S1", "", false, manifestAllRows, 0)

		convey.Convey("when StudyManifest is called, then the envelope carries study metadata once and 3 product rows", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(manifest.IDStudyLims, convey.ShouldEqual, "S1")
			convey.So(manifest.Name, convey.ShouldEqual, "Study S1")
			convey.So(manifest.AccessionNumber, convey.ShouldEqual, "EGAS0000S1")
			convey.So(manifest.FacultySponsor, convey.ShouldEqual, "Faculty sponsor 211")
			convey.So(manifest.DataAccessGroup, convey.ShouldEqual, "group")
			convey.So(manifest.CacheSyncedAt, convey.ShouldEqual, "2026-06-27T06:00:00Z")
			convey.So(manifest.Rows, convey.ShouldHaveLength, 3)

			convey.Convey("and the rows carry the correct per-product sample identity, ordered by (id_run, position, tag_index, name), no irods_path", func() {
				first := manifest.Rows[0]
				convey.So(first.Name, convey.ShouldEqual, "S1-sample-alpha")
				convey.So(first.SupplierName, convey.ShouldEqual, "supplier-alpha")
				convey.So(first.AccessionNumber, convey.ShouldEqual, "EGAN-alpha")
				convey.So(first.SangerSampleID, convey.ShouldEqual, "sanger-alpha")
				convey.So(first.IDRun, convey.ShouldEqual, 52553)
				convey.So(first.Position, convey.ShouldEqual, 1)
				convey.So(first.TagIndex, convey.ShouldEqual, 1)
				convey.So(first.IRODSPath, convey.ShouldEqual, "")

				second := manifest.Rows[1]
				convey.So(second.IDRun, convey.ShouldEqual, 52553)
				convey.So(second.Position, convey.ShouldEqual, 1)
				convey.So(second.TagIndex, convey.ShouldEqual, 2)
				convey.So(second.Name, convey.ShouldEqual, "S1-sample-alpha")

				third := manifest.Rows[2]
				convey.So(third.IDRun, convey.ShouldEqual, 52554)
				convey.So(third.Position, convey.ShouldEqual, 2)
				convey.So(third.TagIndex, convey.ShouldEqual, 3)
				convey.So(third.Name, convey.ShouldEqual, "S1-sample-beta")
				convey.So(third.SupplierName, convey.ShouldEqual, "supplier-beta")
				convey.So(third.AccessionNumber, convey.ShouldEqual, "EGAN-beta")
				convey.So(third.SangerSampleID, convey.ShouldEqual, "sanger-beta")
				convey.So(third.IRODSPath, convey.ShouldEqual, "")
			})
		})
	})
}

func TestStudyManifestWithIRODSCramAddsPathPerProductC1(t *testing.T) {
	convey.Convey("Given study S1 with 3 products and .cram iRODS objects for 2 of them", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedManifestS1Scenario(t, cache.DB())
		// Two of the three products have .cram iRODS objects; the third (2203)
		// has none. Product 2101 also carries a non-cram object so the file_type
		// filter is meaningful (it picks the .cram, not the .crai).
		seedIRODSLocationMirrorRow(t, cache.DB(), "2101", "/seq/52553", "52553_1#1.cram", 21, "S1")
		seedIRODSLocationMirrorRow(t, cache.DB(), "2101", "/seq/52553", "52553_1#1.cram.crai", 21, "S1")
		seedIRODSLocationMirrorRow(t, cache.DB(), "2102", "/seq/52553", "52553_1#2.cram", 21, "S1")
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "S1", "cram", true, manifestAllRows, 0)

		convey.Convey("when StudyManifest(\"S1\",\"cram\",true,all) is called, then 2 rows carry their .cram path and the uncovered row is empty, count still 3", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(manifest.Rows, convey.ShouldHaveLength, 3)

			convey.So(manifest.Rows[0].TagIndex, convey.ShouldEqual, 1)
			convey.So(manifest.Rows[0].IRODSPath, convey.ShouldEqual, "/seq/52553/52553_1#1.cram")
			convey.So(manifest.Rows[1].TagIndex, convey.ShouldEqual, 2)
			convey.So(manifest.Rows[1].IRODSPath, convey.ShouldEqual, "/seq/52553/52553_1#2.cram")
			convey.So(manifest.Rows[2].IDRun, convey.ShouldEqual, 52554)
			convey.So(manifest.Rows[2].IRODSPath, convey.ShouldEqual, "")
		})
	})
}

func TestStudyManifestWithIRODSNoFileTypeDoesNotDefaultToCramC1(t *testing.T) {
	convey.Convey("Given study S1 with a product whose only iRODS object is a .bam (no .cram)", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedManifestS1Scenario(t, cache.DB())
		seedIRODSLocationMirrorRow(t, cache.DB(), "2101", "/seq/52553", "52553_1#1.bam", 21, "S1")
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "S1", "", true, manifestAllRows, 0)

		convey.Convey("when with_irods is set with no file_type, then the .bam path is returned (no implicit cram default)", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(manifest.Rows, convey.ShouldHaveLength, 3)
			convey.So(manifest.Rows[0].TagIndex, convey.ShouldEqual, 1)
			convey.So(manifest.Rows[0].IRODSPath, convey.ShouldEqual, "/seq/52553/52553_1#1.bam")
		})
	})
}

func TestStudyManifestWithIRODSPicksOneCoherentRealObjectC1(t *testing.T) {
	convey.Convey("Given study S1 where one product has two .cram iRODS objects in DIFFERENT collections", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedManifestS1Scenario(t, cache.DB())
		// Product 2101 has TWO matching iRODS objects whose collections and file
		// names sort oppositely: /seqB + aaa.cram and /seqA + zzz.cram. Two
		// independent MINs would pick MIN(collection)=/seqA with
		// MIN(file_name)=aaa.cram, fabricating /seqA/aaa.cram, which is neither
		// real object. The path must be exactly one of the two REAL pairs.
		seedIRODSLocationMirrorRow(t, cache.DB(), "2101", "/seqB", "aaa.cram", 21, "S1")
		seedIRODSLocationMirrorRow(t, cache.DB(), "2101", "/seqA", "zzz.cram", 21, "S1")
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "S1", "cram", true, manifestAllRows, 0)

		convey.Convey("when StudyManifest(\"S1\",\"cram\",true,all) is called, then the path is one real object, never a fabricated collection/file pair, and the count is unchanged", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(manifest.Rows, convey.ShouldHaveLength, 3)

			convey.So(manifest.Rows[0].TagIndex, convey.ShouldEqual, 1)
			convey.So(manifest.Rows[0].IRODSPath, convey.ShouldBeIn, "/seqB/aaa.cram", "/seqA/zzz.cram")
			convey.So(manifest.Rows[0].IRODSPath, convey.ShouldNotEqual, "/seqA/aaa.cram")
			convey.So(manifest.Rows[0].IRODSPath, convey.ShouldNotEqual, "/seqB/zzz.cram")
			convey.So(manifest.Rows[1].IRODSPath, convey.ShouldEqual, "")
			convey.So(manifest.Rows[2].IRODSPath, convey.ShouldEqual, "")
		})
	})
}

func TestStudyManifestHTTPPaginationHeadersC1(t *testing.T) {
	convey.Convey("Given study S1 with 3 products served over HTTP with limit=2&offset=0", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedManifestS1Scenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/study/S1/manifest?limit=2&offset=0")

		convey.Convey("when GET /study/S1/manifest?limit=2&offset=0 is served, then rows has 2, X-Total-Count 3, X-Next-Offset 2", func() {
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var manifest StudyManifest
			decodeMLWHJSONResponseForTest(t, response, &manifest)
			convey.So(manifest.Rows, convey.ShouldHaveLength, 2)
			convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "3")
			convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "2")
		})
	})
}

func TestStudyManifestUnknownStudyReturnsNotFoundC1(t *testing.T) {
	convey.Convey("Given a synced cache with no such study", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedManifestSyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "NOPE", "", false, manifestAllRows, 0)

		convey.Convey("when StudyManifest runs for an unknown study, then it returns ErrNotFound and a zero-value envelope", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
			convey.So(manifest, convey.ShouldResemble, StudyManifest{})
		})
	})
}

func TestStudyManifestSyncedStudyWithNoProductsReturnsEnvelopeC1(t *testing.T) {
	convey.Convey("Given a synced study S1 with metadata but no products", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 211, "S1")
		seedManifestSyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "S1", "", false, manifestAllRows, 0)

		convey.Convey("when StudyManifest runs, then it returns an envelope with metadata, empty rows and a populated cache_synced_at, no error", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(manifest.IDStudyLims, convey.ShouldEqual, "S1")
			convey.So(manifest.Name, convey.ShouldEqual, "Study S1")
			convey.So(manifest.AccessionNumber, convey.ShouldEqual, "EGAS0000S1")
			convey.So(manifest.FacultySponsor, convey.ShouldEqual, "Faculty sponsor 211")
			convey.So(manifest.DataAccessGroup, convey.ShouldEqual, "group")
			convey.So(manifest.Rows, convey.ShouldBeEmpty)
			convey.So(manifest.CacheSyncedAt, convey.ShouldEqual, "2026-06-27T06:00:00Z")
		})
	})
}

func TestStudyManifestEmptyStudyRequiresProductMetricsSyncC1(t *testing.T) {
	convey.Convey("Given study S1 with metadata and study/sample/flowcell sync but product metrics never synced", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 211, "S1")
		seedManifestIdentitySyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "S1", "", false, manifestAllRows, 0)

		convey.Convey("when StudyManifest sees no product rows, then it reports the manifest source as never synced instead of a synced empty envelope", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(manifest, convey.ShouldResemble, StudyManifest{})
		})
	})
}

func TestStudyManifestWithIRODSEmptyStudyRequiresIRODSSyncC1(t *testing.T) {
	convey.Convey("Given study S1 with product metrics synced but iRODS locations never synced", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 211, "S1")
		seedManifestSyncStateWithoutIRODS(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "S1", "cram", true, manifestAllRows, 0)

		convey.Convey("when StudyManifest with_irods sees no product rows, then it reports iRODS freshness as never synced instead of claiming synced empty paths", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(manifest, convey.ShouldResemble, StudyManifest{})
		})
	})
}

func TestStudyManifestWithIRODSRowsRequireIRODSSyncC1(t *testing.T) {
	convey.Convey("Given study S1 with product rows but iRODS locations never synced", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 211, "S1")
		seedManifestSampleRow(t, cache.DB(), 21, "S1-sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha")
		seedIseqProductMetricsMirrorRow(t, cache.DB(), 2101, 21, 52553, 1, 1, "S1")
		seedManifestSyncStateWithoutIRODS(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		manifest, err := client.StudyManifest(context.Background(), "S1", "cram", true, manifestAllRows, 0)

		convey.Convey("when StudyManifest with_irods would otherwise return rows with empty paths, then it reports iRODS freshness as never synced", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(manifest, convey.ShouldResemble, StudyManifest{})
		})
	})
}

func TestStudyManifestInvalidFileTypeRejectedC1(t *testing.T) {
	convey.Convey("Given a synced study S1", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedManifestS1Scenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, err := client.StudyManifest(context.Background(), "S1", "cr%am", true, manifestAllRows, 0)

		convey.Convey("when StudyManifest is given an invalid file_type, then it returns ErrUnsupportedIdentifier", func() {
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		})
	})
}

// seedManifestS1Scenario seeds study S1 with 3 Illumina products across 2
// samples (run/lane/tag distinct): sample 21 carries products on
// (52553,1,1) and (52553,1,2); sample 22 carries product on (52554,2,3). The
// study metadata (name/accession/faculty_sponsor/data_access_group) comes from
// seedHierarchyStudy. Sync state is seeded so cache_synced_at is populated.
func seedManifestS1Scenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 211, "S1")
	seedManifestSampleRow(t, db, 21, "S1-sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha")
	seedManifestSampleRow(t, db, 22, "S1-sample-beta", "supplier-beta", "EGAN-beta", "sanger-beta")

	seedIseqProductMetricsMirrorRow(t, db, 2101, 21, 52553, 1, 1, "S1")
	seedIseqProductMetricsMirrorRow(t, db, 2102, 21, 52553, 1, 2, "S1")
	seedIseqProductMetricsMirrorRow(t, db, 2203, 22, 52554, 2, 3, "S1")

	seedManifestSyncState(t, db)
}

// seedManifestSampleRow inserts a sample_mirror row with caller-controlled
// identity fields (name / supplier_name / accession_number / sanger_sample_id),
// so a manifest row's per-product sample identity can be asserted exactly. The
// other columns get deterministic placeholder values.
func seedManifestSampleRow(t *testing.T, db *sql.DB, id int64, name, supplierName, accession, sangerSampleID string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		"SQSCP",
		formatInt(id+100),
		"manifest-sample-uuid-"+formatInt(id),
		name,
		sangerSampleID,
		supplierName,
		accession,
		"manifest-donor-"+formatInt(id),
		9606,
		"human",
		"manifest-description",
		formatSyncTime(time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedManifestSampleRow(): %v", err)
	}
}

// seedManifestSyncState marks the manifest's feeding tables (and iseq_flowcell,
// which feeds the empty-study cascade) as synced so cache_synced_at is populated
// and the never-synced cascade is not triggered. The oldest last_run is on the
// study table.
func seedManifestSyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	highWater := time.Date(2026, time.June, 27, 0, 0, 0, 0, time.UTC)
	oldest := time.Date(2026, time.June, 27, 6, 0, 0, 0, time.UTC)
	seedSyncStateRun(t, db, syncTableStudy, highWater, oldest)
	seedSyncStateRun(t, db, syncTableSample, highWater, oldest.Add(1*time.Hour))
	seedSyncStateRun(t, db, syncTableIseqFlowcell, highWater, oldest.Add(2*time.Hour))
	seedSyncStateRun(t, db, syncTableIseqProductMetrics, highWater, oldest.Add(2*time.Hour))
	seedSyncStateRun(t, db, syncTableSeqProductIRODSLocations, highWater, oldest.Add(3*time.Hour))
}

func seedManifestSyncStateWithoutIRODS(t *testing.T, db *sql.DB) {
	t.Helper()

	seedManifestIdentitySyncState(t, db)
	seedSyncStateRun(
		t,
		db,
		syncTableIseqProductMetrics,
		time.Date(2026, time.June, 27, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
	)
}

func seedManifestIdentitySyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	highWater := time.Date(2026, time.June, 27, 0, 0, 0, 0, time.UTC)
	oldest := time.Date(2026, time.June, 27, 6, 0, 0, 0, time.UTC)
	seedSyncStateRun(t, db, syncTableStudy, highWater, oldest)
	seedSyncStateRun(t, db, syncTableSample, highWater, oldest.Add(1*time.Hour))
	seedSyncStateRun(t, db, syncTableIseqFlowcell, highWater, oldest.Add(2*time.Hour))
}
