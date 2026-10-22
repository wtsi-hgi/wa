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
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

const (
	multiSampleSupplier = "PD42487a"
	multiSampleDonor    = "PD42487"
)

var multiSampleCRAMRows = [][]string{
	{multiSampleSupplier, "5534STDY0000102", qcPass, "/seq/29284/29284_6#6.cram"},
	{multiSampleSupplier, "5534STDY0000104", qcPass, "/seq/31360/31360_1#9.cram"},
	{multiSampleSupplier, "5534STDY0000104", qcPass, "/seq/31360/31360_2#9.cram"},
	{multiSampleSupplier, "5534STDY0000104", qcPass, "/seq/31360/31360_3#9.cram"},
	{multiSampleSupplier, "5534STDY0000104", qcPass, "/seq/31360/31360_4#9.cram"},
}

var multiSampleLaneRows = [][]string{
	{multiSampleSupplier, "5534STDY0000102", "29284", "6", "6"},
	{multiSampleSupplier, "5534STDY0000104", "31360", "1", "9"},
	{multiSampleSupplier, "5534STDY0000104", "31360", "2", "9"},
	{multiSampleSupplier, "5534STDY0000104", "31360", "3", "9"},
	{multiSampleSupplier, "5534STDY0000104", "31360", "4", "9"},
	{multiSampleSupplier, "5534STDY0000105", "31400", "2", "3"},
}

const (
	multiSampleSharedLaneSupplier = "POOLED-SUP"
	multiSampleSharedLaneDonor    = "POOLED-DONOR"
)

func TestRemoteClientExportMultiSampleOnOldServer(t *testing.T) {
	convey.Convey("Given a server that predates multi_sample and reports ambiguity", t, func() {
		var exportQuery atomic.Value
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == mlwhOpenAPIPath {
				_, _ = w.Write([]byte(`{"info":{"version":"1.10.0"},"paths":{"/export/{children}/{parent_kind}/{parent_id}":{"get":{"parameters":[{"name":"columns"},{"name":"all"}]}}}}`))

				return
			}
			exportQuery.Store(r.URL.RawQuery)
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"ambiguous","message":"mlwh: identifier matches multiple records: \"PD42478a\" matches samples 4018284 and 4032041"}`))
		}))
		defer server.Close()

		remote, err := NewRemoteClient(RemoteConfig{BaseURL: server.URL, Timeout: 5 * time.Second})
		convey.So(err, convey.ShouldBeNil)
		defer closeRemoteClientForTest(t, remote)

		convey.Convey("when a multi-sample export is requested, then the error says the server is too old", func() {
			_, err := remote.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "sample"}, "PD42478a", ExportOptions{All: true, MultiSample: true})

			convey.So(errors.Is(err, ErrMultiSampleUnsupported), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring, "1.10.0")
			convey.So(exportQuery.Load(), convey.ShouldContainSubstring, "multi_sample=true")
		})

		convey.Convey("when the export does not ask for multi-sample, then the plain ambiguity is returned", func() {
			_, err := remote.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "sample"}, "PD42478a", ExportOptions{All: true})

			convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrMultiSampleUnsupported), convey.ShouldBeFalse)
			convey.So(exportQuery.Load(), convey.ShouldNotContainSubstring, "multi_sample")
		})
	})
}

func TestRemoteClientExportMultiSampleProbeFallbacks(t *testing.T) {
	convey.Convey("Given a server that reports a multi-sample export as ambiguous", t, func() {
		export := func(server *httptest.Server, token string) error {
			remote, err := NewRemoteClient(RemoteConfig{BaseURL: server.URL, Token: token, Timeout: 5 * time.Second})
			convey.So(err, convey.ShouldBeNil)
			defer closeRemoteClientForTest(t, remote)

			_, err = remote.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "sample"}, "PD42478a", ExportOptions{All: true, MultiSample: true})

			return err
		}

		convey.Convey("when its OpenAPI document fails with a server error, then the original ambiguity is returned", func() {
			server := newAmbiguousExportServerForTest(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})
			defer server.Close()

			err := export(server, "")

			convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrMultiSampleUnsupported), convey.ShouldBeFalse)
		})

		convey.Convey("when its OpenAPI document is not JSON, then the original ambiguity is returned", func() {
			server := newAmbiguousExportServerForTest(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not json"))
			})
			defer server.Close()

			err := export(server, "")

			convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrMultiSampleUnsupported), convey.ShouldBeFalse)
		})

		convey.Convey("when the client has a token, then the OpenAPI probe sends it as a bearer token", func() {
			var authorization atomic.Value
			server := newAmbiguousExportServerForTest(func(w http.ResponseWriter, r *http.Request) {
				authorization.Store(r.Header.Get("Authorization"))
				if r.Header.Get("Authorization") != "Bearer probe-token" {
					w.WriteHeader(http.StatusUnauthorized)

					return
				}
				_, _ = w.Write([]byte(`{"info":{"version":"1.10.0"},"paths":{"/export/{children}/{parent_kind}/{parent_id}":{"get":{"parameters":[{"name":"all"}]}}}}`))
			})
			defer server.Close()

			err := export(server, "probe-token")

			convey.So(authorization.Load(), convey.ShouldEqual, "Bearer probe-token")
			convey.So(errors.Is(err, ErrMultiSampleUnsupported), convey.ShouldBeTrue)
		})
	})
}

// newAmbiguousExportServerForTest answers every Export with 409 ambiguous and
// serves the OpenAPI document with openAPI.
func newAmbiguousExportServerForTest(openAPI http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == mlwhOpenAPIPath {
			openAPI(w, r)

			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"ambiguous","message":"mlwh: identifier matches multiple records: \"PD42478a\" matches samples 4018284 and 4032041"}`))
	}))
}

func TestExportSampleParentMultiSampleSupplierName(t *testing.T) {
	convey.Convey("Given a supplier name shared by five samples of which only some have data", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedMultiSampleSupplierScenario(t, client.cache.DB())

		assertMultiSampleSupplierNameExports(client)

		convey.Convey("when runs are exported, then the runs of every matching sample are returned once each", func() {
			_, rows, err := multiSampleExport(client, "runs", multiSampleSupplier, ExportOptions{All: true})

			convey.So(err, convey.ShouldBeNil)
			convey.So(rows, convey.ShouldResemble, [][]string{{"29284"}, {"31360"}, {"31400"}})
		})
	})
}

func TestExportSampleParentMultiSampleKeepsUniqueIdentifierAmbiguity(t *testing.T) {
	convey.Convey("Given identifiers that must be unique but match several samples", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedMultiSampleSupplierScenario(t, client.cache.DB())
		seedMultiSampleUniqueIdentifierClashes(t, client.cache.DB())

		assertMultiSampleKeepsUniqueIdentifierAmbiguity(client)
	})
}

func TestExportSampleLanesDefaultColumnsIdentifySample(t *testing.T) {
	convey.Convey("Given a single sample with lanes", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedMultiSampleSupplierScenario(t, client.cache.DB())

		assertSampleLanesDefaultColumnsIdentifySample(client)
	})
}

func TestServerExportMultiSampleOptIn(t *testing.T) {
	convey.Convey("Given a server over a cache with a shared supplier name", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedMultiSampleSupplierScenario(t, client.cache.DB())

		convey.Convey("when the export omits multi_sample, then the existing 409 ambiguous contract is unchanged", func() {
			response := performMLWHRequestForTest(t, client, http.MethodGet, "/export/irods/sample/"+multiSampleSupplier+"?all=true")

			convey.So(response.Code, convey.ShouldEqual, http.StatusConflict)
			convey.So(response.Body.String(), convey.ShouldContainSubstring, `"code":"ambiguous"`)
		})

		convey.Convey("when multi_sample=true is sent, then every matching sample's rows are returned", func() {
			response := performMLWHRequestForTest(t, client, http.MethodGet, "/export/lanes/sample/"+multiSampleSupplier+"?all=true&multi_sample=true")
			var result ExportResult
			decodeMLWHJSONResponseForTest(t, response, &result)

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(result.Rows, convey.ShouldResemble, multiSampleLaneRows)
		})

		convey.Convey("when multi_sample is not a boolean, then the request is rejected", func() {
			response := performMLWHRequestForTest(t, client, http.MethodGet, "/export/lanes/sample/"+multiSampleSupplier+"?multi_sample=maybe")

			convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		})

		convey.Convey("when the OpenAPI document is read, then the export operation advertises multi_sample", func() {
			convey.So(exportOperationAdvertisesMultiSample(t, client), convey.ShouldBeTrue)
		})
	})
}

func TestRemoteClientExportMultiSample(t *testing.T) {
	convey.Convey("Given a RemoteClient against an in-process server with a shared supplier name", t, func() {
		local, cleanup := newExportTestClient(t)
		defer cleanup()
		seedMultiSampleSupplierScenario(t, local.cache.DB())

		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		convey.Convey("when all rows are exported with multi-sample, then the remote rows match the local rows", func() {
			opts := ExportOptions{All: true, FileType: "cram", MultiSample: true}
			result, err := remote.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "sample"}, multiSampleSupplier, opts)
			convey.So(err, convey.ShouldBeNil)

			rows := make([][]string, 0)
			_, err = result.ForEachRow(context.Background(), func(row []string) error {
				rows = append(rows, row)

				return nil
			})
			convey.So(err, convey.ShouldBeNil)
			convey.So(rows, convey.ShouldResemble, multiSampleCRAMRows)
		})

		convey.Convey("when a unique identifier is ambiguous on a server that supports multi-sample, then ErrAmbiguous is returned", func() {
			_, err := local.cache.DB().Exec(`UPDATE sample_mirror SET accession_number = 'EGANDUP' WHERE id_sample_tmp IN (102, 104)`)
			convey.So(err, convey.ShouldBeNil)

			_, err = remote.Export(context.Background(), ExportRelationship{Children: "lanes", ParentKind: "sample"}, "EGANDUP", ExportOptions{All: true, MultiSample: true})

			convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrMultiSampleUnsupported), convey.ShouldBeFalse)
		})
	})
}

// TestRealMySQLExportSampleParentMultiSample runs the multi-sample export
// behaviour against a throwaway database on the real MySQL cache server, where
// the IN (subquery) scope, the lanes derived table and the multi-sample study
// listing must execute under MySQL's dialect. It skips when no MySQL cache is
// configured.
func TestRealMySQLExportSampleParentMultiSample(t *testing.T) {
	client := openThrowawayMySQLExportClientForTest(t)
	seedMultiSampleSupplierScenario(t, client.cache.DB())

	convey.Convey("Given a supplier name shared by five samples in a throwaway MySQL cache", t, func() {
		assertMultiSampleSupplierNameExports(client)
		assertSampleLanesDefaultColumnsIdentifySample(client)

		convey.Convey("when EXPLAIN runs the scoped iRODS and lanes queries, then the sample lookups are index-served", func() {
			assertMultiSampleScopeIndexServed(t, client)
		})
	})
}

func TestRealMySQLExportSampleParentMultiSampleKeepsUniqueIdentifierAmbiguity(t *testing.T) {
	client := openThrowawayMySQLExportClientForTest(t)
	seedMultiSampleSupplierScenario(t, client.cache.DB())
	seedMultiSampleUniqueIdentifierClashes(t, client.cache.DB())

	convey.Convey("Given identifiers that must be unique but match several samples in a throwaway MySQL cache", t, func() {
		assertMultiSampleKeepsUniqueIdentifierAmbiguity(client)
	})
}

func TestExportSampleParentMultiSampleSharedLane(t *testing.T) {
	convey.Convey("Given six samples sharing a supplier name, a donor id and one run/lane/tag product", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedMultiSampleSharedLaneScenario(t, client.cache.DB())

		assertMultiSampleSharedLaneExports(client)
	})
}

func TestRealMySQLExportSampleParentMultiSampleSharedLane(t *testing.T) {
	client := openThrowawayMySQLExportClientForTest(t)
	seedMultiSampleSharedLaneScenario(t, client.cache.DB())

	convey.Convey("Given six samples sharing a supplier name, a donor id and one run/lane/tag product, in a throwaway MySQL cache", t, func() {
		assertMultiSampleSharedLaneExports(client)
	})
}

// openThrowawayMySQLExportClientForTest opens a cache-only Client on a fresh
// throwaway MySQL database that t.Cleanup drops, or skips the test when no
// MySQL cache is configured or reachable.
func openThrowawayMySQLExportClientForTest(t *testing.T) *Client {
	t.Helper()

	baseDSN, password := realMySQLCacheDSNOrSkip(t)
	throwawayDSN := createThrowawayMySQLCacheDBOrSkip(t, baseDSN, password)

	client, err := OpenCacheOnly(context.Background(), CacheConfig{Path: throwawayDSN, Password: password})
	if err != nil {
		t.Fatalf("OpenCacheOnly() against throwaway MySQL cache: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if client.cache.Dialect() != "mysql" {
		t.Fatalf("throwaway cache dialect = %q, want mysql", client.cache.Dialect())
	}

	return client
}

// seedMultiSampleSharedLaneScenario models six samples, 401 to 406, that share
// supplier name POOLED-SUP and donor id POOLED-DONOR and were all sequenced in
// run 32000 lane 1 under tag 5, so only the sample breaks the run/lane/tag tie.
// Samples and their product rows are written in descending sample order, and
// sample 401 has two product rows for the same run/lane/tag, which must
// collapse into one exported lane.
func seedMultiSampleSharedLaneScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 900, "5534")
	for id := int64(406); id >= 401; id-- {
		name := "5534STDY0000" + formatInt(id)
		seedManifestSampleRow(t, db, id, name, multiSampleSharedLaneSupplier, "EGAN0000"+formatInt(id), name)
		seedLibrarySample(t, db, "Standard", id, "5534")
		setSampleDonorForTest(t, db, id, multiSampleSharedLaneDonor)
		seedIseqProductMetricsMirrorRow(t, db, id*10, id, 32000, 1, 5, "5534")
	}
	seedIseqProductMetricsMirrorRow(t, db, 4019, 401, 32000, 1, 5, "5534")

	seedExportSyncState(t, db)
	seedExportRunAggregationSyncState(t, db)
}

func assertMultiSampleSharedLaneExports(client *Client) {
	want := make([][]string, 0, 6)
	for id := int64(401); id <= 406; id++ {
		want = append(want, []string{multiSampleSharedLaneSupplier, "5534STDY0000" + formatInt(id), "32000", "1", "5"})
	}

	for _, identifier := range []string{multiSampleSharedLaneSupplier, multiSampleSharedLaneDonor} {
		convey.Convey("when lanes of "+identifier+" are exported with multi-sample, then each sample's lane appears once, ordered by sample", func() {
			_, rows, err := multiSampleExport(client, "lanes", identifier, ExportOptions{All: true})

			convey.So(err, convey.ShouldBeNil)
			convey.So(rows, convey.ShouldResemble, want)
		})

		for _, limit := range []int{1, 2, 4} {
			convey.Convey("when lanes of "+identifier+" are paged "+formatInt(int64(limit))+" at a time, then the pages concatenate to the same ordered rows", func() {
				rows := make([][]string, 0, len(want))
				for offset := 0; offset < len(want); offset += limit {
					page, _, err := multiSampleExport(client, "lanes", identifier, ExportOptions{Limit: limit, Offset: offset})
					convey.So(err, convey.ShouldBeNil)
					convey.So(page.Total, convey.ShouldEqual, len(want))
					rows = append(rows, page.Rows...)
				}

				convey.So(rows, convey.ShouldResemble, want)
			})
		}
	}
}

// seedMultiSampleSupplierScenario models a supplier name shared by five Sanger
// samples where only some were sequenced: 101 and 103 are registered only, 102
// has one Illumina product with a CRAM, 104 is a four-lane top-up with CRAMs and
// a second study, and 105 has a product but no iRODS data yet. Sample 200 has
// data under a different supplier name, and sample 106 shares the supplier name
// but belongs to another LIMS; neither may leak into the scope.
func seedMultiSampleSupplierScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 900, "5534")
	seedHierarchyStudy(t, db, 901, "7000")
	seedHierarchyStudy(t, db, 902, "7001")

	for _, id := range []int64{101, 102, 103, 104, 105} {
		name := "5534STDY0000" + formatInt(id)
		seedManifestSampleRow(t, db, id, name, multiSampleSupplier, "EGAN0000"+formatInt(id), name)
		seedLibrarySample(t, db, "Standard", id, "5534")
		setSampleDonorForTest(t, db, id, multiSampleDonor)
	}
	seedLibrarySample(t, db, "Standard", 104, "7000")
	seedManifestSampleRow(t, db, 200, "5534STDY0000200", "OTHER", "EGAN0000200", "5534STDY0000200")
	seedLibrarySample(t, db, "Standard", 200, "5534")
	seedManifestSampleRow(t, db, 106, "C_GCLP0000106", multiSampleSupplier, "EGAN0000106", "C_GCLP0000106")
	if _, err := db.Exec(`UPDATE sample_mirror SET id_lims = 'C_GCLP' WHERE id_sample_tmp = 106`); err != nil {
		t.Fatalf("seedMultiSampleSupplierScenario(): %v", err)
	}
	seedLibrarySample(t, db, "Standard", 106, "7001")

	seedIseqProductMetricsMirrorRow(t, db, 1021, 102, 29284, 6, 6, "5534")
	for lane := 1; lane <= 4; lane++ {
		seedIseqProductMetricsMirrorRow(t, db, int64(1040+lane), 104, 31360, lane, 9, "5534")
	}
	seedIseqProductMetricsMirrorRow(t, db, 1051, 105, 31400, 2, 3, "5534")
	seedIseqProductMetricsMirrorRow(t, db, 2001, 200, 29284, 6, 7, "5534")
	seedIseqProductMetricsMirrorRow(t, db, 1061, 106, 31500, 1, 1, "7001")

	seedMultiSampleCRAM(t, db, 1, 102, 29284, 6, 6)
	for lane := 1; lane <= 4; lane++ {
		seedMultiSampleCRAM(t, db, int64(1+lane), 104, 31360, lane, 9)
	}
	seedMultiSampleCRAM(t, db, 9, 200, 29284, 6, 7)
	seedMultiSampleCRAM(t, db, 10, 106, 31500, 1, 1)

	seedExportSyncState(t, db)
	seedExportRunAggregationSyncState(t, db)
}

func setSampleDonorForTest(t *testing.T, db *sql.DB, id int64, donor string) {
	t.Helper()

	if _, err := db.Exec(`UPDATE sample_mirror SET donor_id = ? WHERE id_sample_tmp = ?`, donor, id); err != nil {
		t.Fatalf("setSampleDonorForTest(): %v", err)
	}
	seedDonorSampleRow(t, db, donor, id, "5534")
}

func seedMultiSampleCRAM(t *testing.T, db *sql.DB, idLocation, idSample int64, idRun, position, tagIndex int) {
	t.Helper()

	product := formatInt(int64(idRun)) + "_" + formatInt(int64(position)) + "#" + formatInt(int64(tagIndex))
	seedExportIRODSRow(t, db, exportIRODSSeedRow{
		IDSeqProductLocation: idLocation,
		IDProduct:            "product-" + product,
		Collection:           "/seq/" + formatInt(int64(idRun)),
		FileName:             product + ".cram",
		IDSampleTmp:          idSample,
		StudyID:              "5534",
		IDRun:                idRun,
		Position:             position,
		TagIndex:             tagIndex,
		QC:                   sql.NullInt64{Int64: 1, Valid: true},
		IsDeliverable:        sql.NullInt64{Int64: 1, Valid: true},
	})
}

// seedMultiSampleUniqueIdentifierClashes adds, on top of
// seedMultiSampleSupplierScenario, a duplicated accession (EGANDUP) and four
// values that are one sample's must-be-unique identifier and two other
// samples' shared supplier name: a name (CLASH), a Sanger sample id
// (SSID-CLASH), an accession (SHARED-ACC) and a non-numeric LIMS id
// (LIMS-CLASH). The sample holding each clash value holds it under exactly one
// must-be-unique column, so each column's guard is exercised on its own.
func seedMultiSampleUniqueIdentifierClashes(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`UPDATE sample_mirror SET accession_number = 'EGANDUP' WHERE id_sample_tmp IN (102, 104)`); err != nil {
		t.Fatalf("seedMultiSampleUniqueIdentifierClashes(): %v", err)
	}
	seedManifestSampleRow(t, db, 300, "CLASH", "CLASH-supplier", "EGAN0000300", "SSID0000300")
	seedManifestSampleRow(t, db, 306, "sample-306", "supplier-306", "EGAN0000306", "SSID-CLASH")
	seedManifestSampleRow(t, db, 307, "sample-307", "SSID-CLASH", "EGAN0000307", "sample-307")
	seedManifestSampleRow(t, db, 308, "sample-308", "SSID-CLASH", "EGAN0000308", "sample-308")
	seedManifestSampleRow(t, db, 301, "sample-301", "CLASH", "EGAN0000301", "sample-301")
	seedManifestSampleRow(t, db, 302, "sample-302", "CLASH", "EGAN0000302", "sample-302")
	seedManifestSampleRow(t, db, 303, "sample-303", "SHARED-ACC", "SHARED-ACC-X", "sample-303")
	seedManifestSampleRow(t, db, 304, "sample-304", "SHARED-ACC", "EGAN0000304", "sample-304")
	seedManifestSampleRow(t, db, 305, "sample-305", "supplier-305", "SHARED-ACC", "sample-305")
	seedManifestSampleRow(t, db, 309, "sample-309", "supplier-309", "EGAN0000309", "sample-309")
	seedManifestSampleRow(t, db, 310, "sample-310", "LIMS-CLASH", "EGAN0000310", "sample-310")
	seedManifestSampleRow(t, db, 311, "sample-311", "LIMS-CLASH", "EGAN0000311", "sample-311")

	if _, err := db.Exec(`UPDATE sample_mirror SET id_sample_lims = 'LIMS-CLASH' WHERE id_sample_tmp = 309`); err != nil {
		t.Fatalf("seedMultiSampleUniqueIdentifierClashes(): %v", err)
	}
}

func assertMultiSampleKeepsUniqueIdentifierAmbiguity(client *Client) {
	convey.Convey("when a duplicated accession is exported with multi-sample, then ambiguity is reported", func() {
		_, _, err := multiSampleExport(client, "lanes", "EGANDUP", ExportOptions{All: true})

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
	})

	convey.Convey("when a value is one sample's name and other samples' shared supplier name, then ambiguity is reported", func() {
		_, _, err := multiSampleExport(client, "lanes", "CLASH", ExportOptions{All: true})

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
	})

	convey.Convey("when a value is only one sample's Sanger sample id and other samples' shared supplier name, then ambiguity is reported", func() {
		_, _, err := multiSampleExport(client, "lanes", "SSID-CLASH", ExportOptions{All: true})

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
	})

	convey.Convey("when a value is one sample's accession and other samples' shared supplier name, then ambiguity is reported", func() {
		_, _, err := multiSampleExport(client, "lanes", "SHARED-ACC", ExportOptions{All: true})

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
	})

	convey.Convey("when a value is only one sample's non-numeric LIMS id and other samples' shared supplier name, then lanes and irods report ambiguity", func() {
		_, _, err := multiSampleExport(client, "lanes", "LIMS-CLASH", ExportOptions{All: true})

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)

		_, _, err = multiSampleExport(client, "irods", "LIMS-CLASH", ExportOptions{All: true, FileType: "cram"})

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
	})
}

// assertMultiSampleSupplierNameExports holds the multi-sample export behaviour
// over seedMultiSampleSupplierScenario, shared by the SQLite and MySQL tests.
func assertMultiSampleSupplierNameExports(client *Client) {
	convey.Convey("when irods is exported without opting in, then the ambiguity is still reported", func() {
		_, _, err := exportRowsForTest(client, "irods", multiSampleSupplier, ExportOptions{All: true, FileType: "cram"})

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
	})

	convey.Convey("when irods is exported with multi-sample, then every CRAM of every matching sample is returned in run/lane/tag order", func() {
		result, rows, err := multiSampleExport(client, "irods", multiSampleSupplier, ExportOptions{All: true, FileType: "cram"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Columns, convey.ShouldResemble, []string{"supplier_name", "sanger_sample_id", "manual_qc", "irods_path"})
		convey.So(rows, convey.ShouldResemble, multiSampleCRAMRows)
	})

	convey.Convey("when irods is paged with a cursor, then the pages concatenate to the complete ordered set", func() {
		first, _, err := multiSampleExport(client, "irods", multiSampleSupplier, ExportOptions{Limit: 2, FileType: "cram"})
		convey.So(err, convey.ShouldBeNil)
		convey.So(first.Total, convey.ShouldEqual, 5)
		convey.So(first.NextCursor, convey.ShouldNotBeEmpty)

		rows := append([][]string{}, first.Rows...)
		cursor := first.NextCursor
		for cursor != "" {
			page, _, pageErr := multiSampleExport(client, "irods", multiSampleSupplier, ExportOptions{Limit: 2, FileType: "cram", Cursor: cursor})
			convey.So(pageErr, convey.ShouldBeNil)
			rows = append(rows, page.Rows...)
			cursor = page.NextCursor
		}

		convey.So(rows, convey.ShouldResemble, multiSampleCRAMRows)
	})

	convey.Convey("when lanes are exported, then each run/lane/tag row names its sample and samples without products drop out", func() {
		result, rows, err := multiSampleExport(client, "lanes", multiSampleSupplier, ExportOptions{All: true})

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Columns, convey.ShouldResemble, []string{"supplier_name", "sanger_sample_id", "id_run", "lane", "tag_index"})
		convey.So(rows, convey.ShouldResemble, multiSampleLaneRows)
	})

	convey.Convey("when lanes are paged with limit/offset, then the pages are deterministic and Total counts every sample's lanes", func() {
		first, _, err := multiSampleExport(client, "lanes", multiSampleSupplier, ExportOptions{Limit: 4})
		convey.So(err, convey.ShouldBeNil)
		second, _, err := multiSampleExport(client, "lanes", multiSampleSupplier, ExportOptions{Limit: 4, Offset: 4})
		convey.So(err, convey.ShouldBeNil)

		convey.So(first.Total, convey.ShouldEqual, 6)
		convey.So(first.Complete, convey.ShouldBeFalse)
		convey.So(second.Complete, convey.ShouldBeTrue)
		convey.So(append(first.Rows, second.Rows...), convey.ShouldResemble, multiSampleLaneRows)
	})

	convey.Convey("when studies are exported, then the studies of every matching sample are returned once each", func() {
		result, rows, err := multiSampleExport(client, "studies", multiSampleSupplier, ExportOptions{All: true})

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Columns, convey.ShouldResemble, []string{"id_study_lims", "name", "accession_number"})
		convey.So(rows, convey.ShouldResemble, [][]string{
			{"5534", "Study 5534", "EGAS00005534"},
			{"7000", "Study 7000", "EGAS00007000"},
		})

		page, _, pageErr := multiSampleExport(client, "studies", multiSampleSupplier, ExportOptions{Limit: 1, Offset: 1})
		convey.So(pageErr, convey.ShouldBeNil)
		convey.So(page.Total, convey.ShouldEqual, 2)
		convey.So(page.Rows, convey.ShouldResemble, [][]string{{"7000", "Study 7000", "EGAS00007000"}})
	})

	convey.Convey("when the shared donor id is exported, then it scopes to the same samples", func() {
		_, rows, err := multiSampleExport(client, "irods", multiSampleDonor, ExportOptions{All: true, FileType: "cram"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(rows, convey.ShouldResemble, multiSampleCRAMRows)
	})

	convey.Convey("when one sample's Sanger name is exported with multi-sample, then only that sample's rows are returned", func() {
		_, rows, err := multiSampleExport(client, "lanes", "5534STDY0000102", ExportOptions{All: true})

		convey.So(err, convey.ShouldBeNil)
		convey.So(rows, convey.ShouldResemble, multiSampleLaneRows[:1])
	})

	convey.Convey("when multi-sample is requested for a non-sample parent, then it is rejected", func() {
		_, err := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "5534", ExportOptions{All: true, MultiSample: true})

		convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
	})
}

func assertSampleLanesDefaultColumnsIdentifySample(client *Client) {
	convey.Convey("when its lanes are exported without multi-sample, then the default columns still identify the sample", func() {
		result, rows, err := exportRowsForTest(client, "lanes", "5534STDY0000104", ExportOptions{All: true})

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Columns, convey.ShouldResemble, []string{"supplier_name", "sanger_sample_id", "id_run", "lane", "tag_index"})
		convey.So(rows, convey.ShouldResemble, multiSampleLaneRows[1:5])
	})

	convey.Convey("when the lane-only columns are requested, then the historic projection is available", func() {
		_, rows, err := exportRowsForTest(client, "lanes", "5534STDY0000102", ExportOptions{All: true, Columns: []string{"id_run", "lane", "tag_index"}})

		convey.So(err, convey.ShouldBeNil)
		convey.So(rows, convey.ShouldResemble, [][]string{{"29284", "6", "6"}})
	})
}

// assertMultiSampleScopeIndexServed asserts the supplier-name scope subquery is
// answered from the supplier_name index and the scoped iRODS and lanes scans
// look samples up by their id_sample_tmp indexes rather than scanning.
func assertMultiSampleScopeIndexServed(t *testing.T, client *Client) {
	t.Helper()

	db := client.cache.DB()
	for _, table := range []string{"sample_mirror", "seq_product_irods_locations_mirror", "iseq_product_metrics_mirror"} {
		if _, err := db.Exec("ANALYZE TABLE " + table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}

	scope, ok, err := client.manyToOneSampleScope(context.Background(), multiSampleSupplier)
	convey.So(err, convey.ShouldBeNil)
	convey.So(ok, convey.ShouldBeTrue)

	plan, err := newExportPlan(ExportRelationship{Children: "irods", ParentKind: "sample"}, multiSampleSupplier, ExportOptions{FileType: "cram", MultiSample: true})
	convey.So(err, convey.ShouldBeNil)
	query, args, err := exportIRODSPageQuery(exportIRODSQueryInput{
		parentKind:     "sample",
		sampleScope:    scope,
		columns:        plan.columns,
		normalisedFile: plan.normalisedFile,
		limit:          plan.limit,
	})
	convey.So(err, convey.ShouldBeNil)
	plans := explainPlanRows(t, db, query, args...)
	assertExplainUsesKey(plans, "sample_mirror", "sample_mirror_supplier_name_idx")
	assertExplainUsesKey(plans, "spi", "spi_mirror_sample_tmp_iseq_product_idx", "seq_product_irods_locations_mirror_id_sample_tmp_idx")

	from, laneArgs := exportLanesFrom(scope, false)
	plans = explainPlanRows(t, db, `SELECT lanes.id_run`+from, laneArgs...)
	assertExplainUsesKey(plans, "sample_mirror", "sample_mirror_supplier_name_idx")
	assertExplainUsesKey(plans, "ipm", "ipm_mirror_sample_run_position_tag_idx")
}

// assertExplainUsesKey asserts the alias is read through one of keys, not
// a full scan.
func assertExplainUsesKey(plans []mysqlExplainPlanRow, alias string, keys ...string) {
	plan, ok := findExplainPlanRow(plans, alias)
	convey.So(ok, convey.ShouldBeTrue)
	convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
	convey.So(slices.Contains(keys, plan.key), convey.ShouldBeTrue)
}

func exportOperationAdvertisesMultiSample(t *testing.T, client *Client) bool {
	t.Helper()

	response := performMLWHRequestForTest(t, client, http.MethodGet, mlwhOpenAPIPath)

	return strings.Contains(response.Body.String(), `"name":"multi_sample"`)
}

func multiSampleExport(client *Client, children, parentID string, opts ExportOptions) (ExportResult, [][]string, error) {
	opts.MultiSample = true

	return exportRowsForTest(client, children, parentID, opts)
}

func exportRowsForTest(client *Client, children, parentID string, opts ExportOptions) (ExportResult, [][]string, error) {
	result, err := client.Export(context.Background(), ExportRelationship{Children: children, ParentKind: "sample"}, parentID, opts)
	if err != nil {
		return result, nil, err
	}

	rows := make([][]string, 0)
	_, err = result.ForEachRow(context.Background(), func(row []string) error {
		rows = append(rows, row)

		return nil
	})

	return result, rows, err
}
