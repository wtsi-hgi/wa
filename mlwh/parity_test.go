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
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

const (
	parityStudyID         = "7607"
	parityStudyAccession  = "EGAS0000107607"
	parityStudyName       = "Parity Study 7607"
	paritySampleName      = "7607STDY14643771"
	paritySampleLimsID    = "9575305"
	paritySampleSangerID  = "SANGER-7607-1"
	paritySampleSupplier  = "supplier-7607-1"
	paritySampleAccession = "ERS7607001"
	paritySampleDonor     = "donor-7607-1"
	parityLibraryType     = "Custom"
	parityLibraryID       = "71046409"
	parityLibraryLimsID   = "SQPP-47463-G:B1"
	parityFindLibraryType = "Bespoke"
	parityRunID           = "48522"
	// parityStudySearchTerm matches both seeded studies via their name
	// ("Parity Study 7607"/"7608") and study_title ("Study title 7607"/"7608"),
	// so SearchStudies/CountStudySearch return a non-trivial, deterministic set
	// (two studies, ordered by id_study_lims) across local and remote clients.
	parityStudySearchTerm = "study"
	// paritySampleSearchTerm matches both seeded samples via their supplier_name
	// ("supplier-7607-1"/"supplier-7607-2"), so SearchSamples/CountSampleSearch
	// return a non-trivial, deterministic set (two samples, ordered by
	// id_sample_tmp) once the FTS5 sample_search index is rebuilt below.
	paritySampleSearchTerm = "supplier"
)

type parityQueryCase struct {
	name string
	call func(context.Context, Queryer) (any, error)
}

func parityQueryCases() []parityQueryCase {
	return []parityQueryCase{
		{name: "ClassifyIdentifier", call: func(ctx context.Context, q Queryer) (any, error) { return q.ClassifyIdentifier(ctx, parityStudyID) }},
		{name: "ResolveSample", call: func(ctx context.Context, q Queryer) (any, error) { return q.ResolveSample(ctx, paritySampleName) }},
		{name: "ResolveSampleName", call: func(ctx context.Context, q Queryer) (any, error) { return q.ResolveSampleName(ctx, paritySampleName) }},
		{name: "ResolveStudy", call: func(ctx context.Context, q Queryer) (any, error) { return q.ResolveStudy(ctx, parityStudyID) }},
		{name: "ResolveRun", call: func(ctx context.Context, q Queryer) (any, error) { return q.ResolveRun(ctx, parityRunID) }},
		{name: "ResolveLibrary", call: func(ctx context.Context, q Queryer) (any, error) { return q.ResolveLibrary(ctx, parityLibraryType) }},
		{name: "ResolveLibraryIdentifier", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.ResolveLibraryIdentifier(ctx, parityLibraryID)
		}},
		{name: "AllStudies", call: func(ctx context.Context, q Queryer) (any, error) { return q.AllStudies(ctx, 100, 0) }},
		{name: "SamplesForStudy", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SamplesForStudy(ctx, parityStudyID, 100, 0)
		}},
		{name: "SamplesForRun", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SamplesForRun(ctx, parityRunID, 100, 0)
		}},
		{name: "SamplesForLibrary", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SamplesForLibrary(ctx, parityLibraryType, parityStudyID, 100, 0)
		}},
		{name: "SamplesForLibraryID", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SamplesForLibraryID(ctx, parityLibraryID, 100, 0)
		}},
		{name: "SamplesForLibraryLimsID", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SamplesForLibraryLimsID(ctx, parityLibraryLimsID, 100, 0)
		}},
		{name: "SamplesForLibraryType", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SamplesForLibraryType(ctx, parityLibraryType, 100, 0)
		}},
		{name: "LibrariesForStudy", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.LibrariesForStudy(ctx, parityStudyID, 100, 0)
		}},
		{name: "RunsForStudy", call: func(ctx context.Context, q Queryer) (any, error) { return q.RunsForStudy(ctx, parityStudyID, 100, 0) }},
		{name: "StudyOverview", call: func(ctx context.Context, q Queryer) (any, error) { return q.StudyOverview(ctx, parityStudyID) }},
		{name: "SamplesWithData", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SamplesWithData(ctx, parityStudyID, 100, 0)
		}},
		{name: "SamplesWithoutData", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SamplesWithoutData(ctx, parityStudyID, 100, 0)
		}},
		{name: "LanesForSample", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.LanesForSample(ctx, paritySampleName, 100, 0)
		}},
		{name: "IRODSPathsForSample", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.IRODSPathsForSample(ctx, paritySampleName, 100, 0)
		}},
		{name: "IRODSPathsForStudy", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.IRODSPathsForStudy(ctx, parityStudyID, 100, 0)
		}},
		{name: "StudiesForSample", call: func(ctx context.Context, q Queryer) (any, error) { return q.StudiesForSample(ctx, paritySampleName) }},
		{name: "FindSamplesBySangerID", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.FindSamplesBySangerID(ctx, paritySampleSangerID)
		}},
		{name: "FindSamplesByIDSampleLims", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.FindSamplesByIDSampleLims(ctx, paritySampleLimsID)
		}},
		{name: "FindSamplesByAccessionNumber", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.FindSamplesByAccessionNumber(ctx, paritySampleAccession)
		}},
		{name: "FindSamplesBySupplierName", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.FindSamplesBySupplierName(ctx, paritySampleSupplier)
		}},
		{name: "FindSamplesByLibraryType", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.FindSamplesByLibraryType(ctx, parityFindLibraryType)
		}},
		{name: "ExpandIdentifier", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.ExpandIdentifier(ctx, KindStudyLimsID, parityStudyID)
		}},
		{name: "ExpandSearchValues", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.ExpandSearchValues(ctx, KindStudyLimsID, parityStudyID)
		}},
		{name: "ExpandSampleSearchValues", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.ExpandSampleSearchValues(ctx, KindSangerSampleID, paritySampleSangerID)
		}},
		{name: "Enrich", call: func(ctx context.Context, q Queryer) (any, error) { return q.Enrich(ctx, parityStudyID) }},
		{name: "SampleDetail", call: func(ctx context.Context, q Queryer) (any, error) { return q.SampleDetail(ctx, paritySampleName) }},
		{name: "StudyDetail", call: func(ctx context.Context, q Queryer) (any, error) { return q.StudyDetail(ctx, parityStudyID) }},
		{name: "RunDetail", call: func(ctx context.Context, q Queryer) (any, error) { return q.RunDetail(ctx, parityRunID) }},
		{name: "LibraryDetail", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.LibraryDetail(ctx, parityLibraryType, parityStudyID)
		}},
		{name: "SearchStudies", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SearchStudies(ctx, parityStudySearchTerm, 100, 0)
		}},
		{name: "SearchSamples", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.SearchSamples(ctx, paritySampleSearchTerm, 100, 0)
		}},
		{name: "CountStudySearch", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.CountStudySearch(ctx, parityStudySearchTerm)
		}},
		{name: "CountSampleSearch", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.CountSampleSearch(ctx, paritySampleSearchTerm)
		}},
		{name: "CountStudies", call: func(ctx context.Context, q Queryer) (any, error) { return q.CountStudies(ctx) }},
		{name: "CountSamplesForStudy", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.CountSamplesForStudy(ctx, parityStudyID)
		}},
		{name: "CountSamplesWithData", call: func(ctx context.Context, q Queryer) (any, error) {
			return q.CountSamplesWithData(ctx, parityStudyID)
		}},
		{name: "Freshness", call: func(ctx context.Context, q Queryer) (any, error) { return q.Freshness(ctx) }},
	}
}

type paritySample struct {
	IDSampleTmp     int64
	IDStudyLims     string
	IDSampleLims    string
	UUIDSampleLims  string
	Name            string
	SangerSampleID  string
	SupplierName    string
	AccessionNumber string
	DonorID         string
}

func seedParityCache(t *testing.T, db *sql.DB) {
	t.Helper()

	syncedAt := paritySyncedAt()
	seedSyncState(t, db, syncTableSample, syncedAt)
	seedSyncState(t, db, syncTableStudy, syncedAt)
	seedSyncState(t, db, syncTableIseqFlowcell, syncedAt)
	seedSyncState(t, db, syncTableIseqProductMetrics, syncedAt)
	seedSyncState(t, db, syncTableSeqProductIRODSLocations, syncedAt)

	seedStudyMirrorRow(t, db, 1, parityStudyID, "study-uuid-"+parityStudyID, parityStudyName, parityStudyAccession)
	seedStudyMirrorRow(t, db, 2, "7608", "study-uuid-7608", "Parity Study 7608", "EGAS0000107608")

	seedParitySample(t, db, paritySample{
		IDSampleTmp:     31,
		IDStudyLims:     parityStudyID,
		IDSampleLims:    paritySampleLimsID,
		UUIDSampleLims:  "sample-uuid-7607-1",
		Name:            paritySampleName,
		SangerSampleID:  paritySampleSangerID,
		SupplierName:    paritySampleSupplier,
		AccessionNumber: paritySampleAccession,
		DonorID:         paritySampleDonor,
	})
	seedParitySample(t, db, paritySample{
		IDSampleTmp:     32,
		IDStudyLims:     parityStudyID,
		IDSampleLims:    "9575306",
		UUIDSampleLims:  "sample-uuid-7607-2",
		Name:            "7607STDY14643772",
		SangerSampleID:  "SANGER-7607-2",
		SupplierName:    "supplier-7607-2",
		AccessionNumber: "ERS7607002",
		DonorID:         "donor-7607-2",
	})

	seedParityLibrarySample(t, db, parityLibraryType, 31, parityStudyID, parityLibraryID, parityLibraryLimsID)
	seedParityLibrarySample(t, db, parityLibraryType, 32, parityStudyID, "71046410", "SQPP-47464-G:C1")
	seedParityLibrarySample(t, db, parityFindLibraryType, 31, parityStudyID, "72000001", "SQPP-72000-G:A1")

	seedIseqProductMetricsMirrorRow(t, db, 9001, 31, 48522, 1, 1, parityStudyID)
	seedIseqProductMetricsMirrorRow(t, db, 9002, 32, 48522, 1, 2, parityStudyID)
	seedIseqProductMetricsMirrorRow(t, db, 9003, 31, 48523, 2, 1, parityStudyID)
	seedIRODSLocationMirrorRow(t, db, "9001", "/seq/illumina/runs/48/48522/plex1", "48522#1.cram", 31, parityStudyID)
	seedIRODSLocationMirrorRow(t, db, "9002", "/seq/illumina/runs/48/48522/plex1", "48522#2.cram", 32, parityStudyID)

	// sample_search is an external-content FTS5 table; raw sample_mirror inserts
	// do not populate it, so rebuild it (as the sample sync does) before the
	// parity table exercises SearchSamples/CountSampleSearch.
	rebuildSampleSearchIndexForTest(t, db)
}

func paritySyncedAt() time.Time {
	return time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
}

func seedParityLibrarySample(
	t *testing.T,
	db *sql.DB,
	pipelineIDLims string,
	idSampleTmp int64,
	idStudyLims string,
	libraryID string,
	idLibraryLims string,
) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims, library_id, id_library_lims) VALUES (?, ?, ?, ?, ?)`,
		pipelineIDLims,
		idSampleTmp,
		idStudyLims,
		libraryID,
		idLibraryLims,
	)
	if err != nil {
		t.Fatalf("seedParityLibrarySample(): %v", err)
	}
}

func seedParitySample(t *testing.T, db *sql.DB, sample paritySample) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sample.IDSampleTmp,
		sqscpIDLims,
		sample.IDSampleLims,
		sample.UUIDSampleLims,
		sample.Name,
		sample.SangerSampleID,
		sample.SupplierName,
		sample.AccessionNumber,
		sample.DonorID,
		9606,
		"human",
		"parity sample "+sample.Name,
		formatSyncTime(paritySyncedAt()),
	)
	if err != nil {
		t.Fatalf("seedParitySample(): %v", err)
	}

	seedDonorSampleRow(t, db, sample.DonorID, sample.IDSampleTmp, sample.IDStudyLims)
}

func TestRemoteClientClientParityB4(t *testing.T) {
	// E2.2 / B4.1: the full parity table is exercised over the HTTP round-trip
	// and its asserted method count is pinned to the live Queryer member count,
	// so a future Queryer method that is not added here (e.g. the Phase 4
	// search/count/freshness additions) fails this test until the table covers
	// it.
	convey.Convey("B4.1: Given a seeded OpenCacheOnly SQLite cache and an httptest server wrapping the same Client", t, func() {
		local := newParitySeededClient(t)
		defer closeParityClientForTest(t, local)
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		cases := parityQueryCases()
		failures := make([]string, 0)
		checked := 0

		for _, tc := range cases {
			localResult, localErr := tc.call(context.Background(), local)
			remoteResult, remoteErr := tc.call(context.Background(), remote)
			checked++

			if localErr != nil || remoteErr != nil {
				failures = append(failures, fmt.Sprintf("%s returned error: local=%v remote=%v", tc.name, localErr, remoteErr))

				continue
			}

			if !reflect.DeepEqual(localResult, remoteResult) {
				failures = append(failures, fmt.Sprintf("%s results differ:\nlocal=%#v\nremote=%#v", tc.name, localResult, remoteResult))
			}
		}

		convey.Convey("when each Queryer method is invoked on both clients, then the asserted count equals the Queryer member count and all JSON round-tripped results match", func() {
			convey.So(len(cases), convey.ShouldEqual, queryerMethodCount())
			convey.So(checked, convey.ShouldEqual, queryerMethodCount())
			convey.So(failures, convey.ShouldHaveLength, 0)
		})
	})
}

func TestRemoteClientClientParityCasingE2(t *testing.T) {
	// E2.1: the Match (ClassifyIdentifier, ResolveStudy) and TaggedID
	// (ExpandIdentifier) results carry the snake_case JSON tags added in Item
	// 3.1; this proves their round-trip through the gin server and RemoteClient
	// decode back to Go values identical to the local Client's.
	convey.Convey("E2.1: Given the seeded parity cache served over HTTP", t, func() {
		local := newParitySeededClient(t)
		defer closeParityClientForTest(t, local)
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		convey.Convey("when ClassifyIdentifier runs on both clients, then the snake_case Match round-trip is lossless", func() {
			localResult, localErr := local.ClassifyIdentifier(context.Background(), parityStudyID)
			remoteResult, remoteErr := remote.ClassifyIdentifier(context.Background(), parityStudyID)

			convey.So(localErr, convey.ShouldBeNil)
			convey.So(remoteErr, convey.ShouldBeNil)
			convey.So(localResult.Study, convey.ShouldNotBeNil)
			convey.So(reflect.DeepEqual(localResult, remoteResult), convey.ShouldBeTrue)
		})

		convey.Convey("when ResolveStudy runs on both clients, then the snake_case Match round-trip is lossless", func() {
			localResult, localErr := local.ResolveStudy(context.Background(), parityStudyID)
			remoteResult, remoteErr := remote.ResolveStudy(context.Background(), parityStudyID)

			convey.So(localErr, convey.ShouldBeNil)
			convey.So(remoteErr, convey.ShouldBeNil)
			convey.So(localResult.Study, convey.ShouldNotBeNil)
			convey.So(reflect.DeepEqual(localResult, remoteResult), convey.ShouldBeTrue)
		})

		convey.Convey("when ExpandIdentifier runs on both clients, then the snake_case TaggedID round-trip is lossless", func() {
			localResult, localErr := local.ExpandIdentifier(context.Background(), KindStudyLimsID, parityStudyID)
			remoteResult, remoteErr := remote.ExpandIdentifier(context.Background(), KindStudyLimsID, parityStudyID)

			convey.So(localErr, convey.ShouldBeNil)
			convey.So(remoteErr, convey.ShouldBeNil)
			convey.So(len(localResult), convey.ShouldBeGreaterThan, 0)
			convey.So(reflect.DeepEqual(localResult, remoteResult), convey.ShouldBeTrue)
		})
	})
}

func newParitySeededClient(t *testing.T) *Client {
	t.Helper()

	client := newParityClient(t)
	seedParityCache(t, client.cache.DB())

	return client
}

func TestRemoteClientClientParityNeverSyncedSentinelB4(t *testing.T) {
	convey.Convey("B4.2: Given a never-synced OpenCacheOnly SQLite cache served over HTTP", t, func() {
		local := newParityClient(t)
		defer closeParityClientForTest(t, local)
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		_, localErr := local.SamplesForStudy(context.Background(), parityStudyID, 100, 0)
		_, remoteErr := remote.SamplesForStudy(context.Background(), parityStudyID, 100, 0)

		convey.Convey("when SamplesForStudy runs locally and remotely, then both errors preserve ErrCacheNeverSynced and ErrNotFound", func() {
			convey.So(errors.Is(localErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(localErr, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(remoteErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(remoteErr, ErrNotFound), convey.ShouldBeTrue)
		})
	})
}

// TestServerOverNeverSyncedCacheStaysReadOnly locks in the guarantee that a user
// going via the MLWH server (the only "via the server" path is the RemoteClient
// over NewServer/RegisterRoutes) cannot trigger a sync: the read/query REST
// surface (Queryer) has no Sync operation, so driving the server can never
// populate a never-synced cache. The server surfaces the never-synced state on a
// read, and the cache remains never-synced afterwards — observed through both the
// remote freshness read and the local cache directly.
func TestServerOverNeverSyncedCacheStaysReadOnly(t *testing.T) {
	convey.Convey("Given a never-synced OpenCacheOnly SQLite cache served over HTTP and driven only through a RemoteClient", t, func() {
		local := newParityClient(t)
		defer closeParityClientForTest(t, local)
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		_, remoteErr := remote.SamplesForStudy(context.Background(), parityStudyID, 100, 0)

		convey.Convey("then the server surfaces the never-synced state on a read", func() {
			convey.So(errors.Is(remoteErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(remoteErr, ErrNotFound), convey.ShouldBeTrue)
		})

		convey.Convey("and the cache remains never-synced afterwards (the server never populated/synced it)", func() {
			remoteFreshness, remoteFreshErr := remote.Freshness(context.Background())
			localFreshness, localFreshErr := local.Freshness(context.Background())

			convey.So(remoteFreshErr, convey.ShouldBeNil)
			convey.So(localFreshErr, convey.ShouldBeNil)
			convey.So(everSyncedCount(remoteFreshness), convey.ShouldEqual, 0)
			convey.So(everSyncedCount(localFreshness), convey.ShouldEqual, 0)
		})
	})
}

func TestRemoteClientClientParityNotFoundSentinelB4(t *testing.T) {
	convey.Convey("B4.3: Given a synced OpenCacheOnly SQLite cache without a requested study", t, func() {
		local := newParityClient(t)
		defer closeParityClientForTest(t, local)
		seedSyncState(t, local.cache.DB(), syncTableStudy, paritySyncedAt())
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		_, localErr := local.ResolveStudy(context.Background(), "999999")
		_, remoteErr := remote.ResolveStudy(context.Background(), "999999")

		convey.Convey("when ResolveStudy runs locally and remotely, then both errors preserve ErrNotFound", func() {
			convey.So(errors.Is(localErr, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(remoteErr, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(localErr, ErrCacheNeverSynced), convey.ShouldBeFalse)
			convey.So(errors.Is(remoteErr, ErrCacheNeverSynced), convey.ShouldBeFalse)
		})
	})
}

func TestRemoteClientClientParityAmbiguousSentinelB4(t *testing.T) {
	convey.Convey("B4.4: Given a study name with two synced cache matches", t, func() {
		local := newParityClient(t)
		defer closeParityClientForTest(t, local)
		seedStudyMirrorRow(t, local.cache.DB(), 81, "7681", "study-uuid-7681", "Duplicated Study", "EGAS0000107681")
		seedStudyMirrorRow(t, local.cache.DB(), 82, "7682", "study-uuid-7682", "Duplicated Study", "EGAS0000107682")
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		_, localErr := local.ResolveStudy(context.Background(), "Duplicated Study")
		_, remoteErr := remote.ResolveStudy(context.Background(), "Duplicated Study")

		convey.Convey("when ResolveStudy runs locally and remotely, then both errors preserve ErrAmbiguous", func() {
			convey.So(errors.Is(localErr, ErrAmbiguous), convey.ShouldBeTrue)
			convey.So(errors.Is(remoteErr, ErrAmbiguous), convey.ShouldBeTrue)
		})
	})
}

func newParityClient(t *testing.T) *Client {
	t.Helper()

	client, err := OpenCacheOnly(context.Background(), CacheConfig{Path: filepath.Join(t.TempDir(), "parity.sqlite")})
	convey.So(err, convey.ShouldBeNil)

	return client
}

func closeParityClientForTest(t *testing.T, client *Client) {
	t.Helper()

	convey.So(client.Close(), convey.ShouldBeNil)
}

func newParityRemoteClientForTest(t *testing.T, local *Client) *RemoteClient {
	t.Helper()

	server := newParityHTTPServerForTest(local)
	t.Cleanup(server.Close)

	return newRemoteClientForTest(t, server.URL, "")
}

func newParityHTTPServerForTest(queryer Queryer) *httptest.Server {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewServer(queryer).RegisterRoutes(router, nil)

	return httptest.NewServer(router)
}

// everSyncedCount returns how many tables in freshness report ever_synced=true; it
// is 0 for a never-synced cache.
func everSyncedCount(freshness Freshness) int {
	count := 0
	for _, table := range freshness.Tables {
		if table.EverSynced {
			count++
		}
	}

	return count
}

// queryerMethodCount returns the number of methods declared on the Queryer
// interface, derived by reflection so the parity table stays pinned to the
// actual query surface rather than a hand-maintained literal.
func queryerMethodCount() int {
	return reflect.TypeOf((*Queryer)(nil)).Elem().NumMethod()
}
