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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

const (
	sharedFindSupplier    = "PD42487a"
	sharedFindLimsID      = "SHAREDLIMS"
	sharedFindLibraryType = "Shared library type"
	sharedFindFirstName   = "5534STDY8112792"
	sharedFindSecondName  = "5534STDY7888714"
	sharedFindOtherName   = "5534STDY9000001"
	sharedFindFirstTmpID  = 4018299
	sharedFindSecondTmpID = 4032054
	sharedFindOtherTmpID  = 4032099
)

// seedSharedFindSamples caches three samples: the first two share a supplier
// name, a LIMS id and a library type, and all three share the seeded Sanger
// sample id and accession.
func seedSharedFindSamples(t *testing.T, db *sql.DB) {
	t.Helper()

	synced := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	for _, table := range []string{syncTableStudy, syncTableSample, syncTableIseqFlowcell} {
		seedSyncStateRun(t, db, table, synced, synced)
	}
	seedHierarchyStudy(t, db, 1, "5534")

	for _, sample := range []struct {
		id       int64
		name     string
		supplier string
	}{
		{sharedFindFirstTmpID, sharedFindFirstName, sharedFindSupplier},
		{sharedFindSecondTmpID, sharedFindSecondName, sharedFindSupplier},
		{sharedFindOtherTmpID, sharedFindOtherName, "other supplier"},
	} {
		seedSampleMirrorRow(t, db, sample.id, sample.name, sample.supplier, "donor-"+sample.name, synced)
		seedDonorSampleRow(t, db, "donor-"+sample.name, sample.id, "5534")
	}

	if _, err := db.Exec(`UPDATE sample_mirror SET id_sample_lims = ? WHERE id_sample_tmp IN (?, ?)`,
		sharedFindLimsID, sharedFindFirstTmpID, sharedFindSecondTmpID); err != nil {
		t.Fatalf("share LIMS id: %v", err)
	}

	seedLibrarySample(t, db, sharedFindLibraryType, sharedFindFirstTmpID, "5534")
	seedLibrarySample(t, db, sharedFindLibraryType, sharedFindSecondTmpID, "5534")
}

// seedAmbiguityCandidates adds twelve samples sharing a supplier name and a
// donor id, a sample whose supplier name is another sample's Sanger name, and
// two studies sharing a name.
func seedAmbiguityCandidates(t *testing.T, db *sql.DB) {
	t.Helper()

	for index := range 12 {
		id := int64(5000000 + index)
		seedManifestSampleRow(t, db, id, fmt.Sprintf("MANY%02d", index), "MANYSUP", fmt.Sprintf("acc-%d", id), fmt.Sprintf("sanger-%d", id))
		setSampleDonorForTest(t, db, id, "MANYDONOR")
	}
	seedManifestSampleRow(t, db, 4032100, "5534STDY9000002", sharedFindOtherName, "acc-4032100", "sanger-4032100")
	seedStudyMirrorRow(t, db, 21, "7001", "study-uuid-7001", "Shared study name", "EGAS7001")
	seedStudyMirrorRow(t, db, 22, "7002", "study-uuid-7002", "Shared study name", "EGAS7002")
}

func sampleNamesForTest(samples []Sample) []string {
	names := make([]string, len(samples))
	for index, sample := range samples {
		names[index] = sample.Name
	}

	return names
}

func TestFindSampleEndpointsListEverySampleSharingTheValue(t *testing.T) {
	convey.Convey("Given cached samples that share identifier values", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()
		seedSharedFindSamples(t, client.cache.DB())

		assertFindSampleEndpointsList(t, client)
	})
}

func TestRealMySQLFindSampleEndpointsListEverySampleSharingTheValue(t *testing.T) {
	client := openThrowawayMySQLExportClientForTest(t)
	seedSharedFindSamples(t, client.cache.DB())

	convey.Convey("Given samples that share identifier values in a throwaway MySQL cache", t, func() {
		assertFindSampleEndpointsList(t, client)
	})
}

func assertFindSampleEndpointsList(t *testing.T, client *Client) {
	t.Helper()

	get := func(path string) ([]Sample, *httptest.ResponseRecorder) {
		response := performMLWHRequestForTest(t, client, http.MethodGet, path)
		if response.Code != http.StatusOK {
			return nil, response
		}

		var samples []Sample
		decodeMLWHJSONResponseForTest(t, response, &samples)

		return samples, response
	}

	convey.Convey("each find endpoint returns every match, ordered by Sanger name, sized by its count", func() {
		for _, tc := range []struct {
			path  string
			names []string
		}{
			{"/find/sample/supplier-name/" + sharedFindSupplier, []string{sharedFindSecondName, sharedFindFirstName}},
			{"/find/sample/lims-id/" + sharedFindLimsID, []string{sharedFindSecondName, sharedFindFirstName}},
			{"/find/sample/library-type/" + strings.ReplaceAll(sharedFindLibraryType, " ", "%20"), []string{sharedFindSecondName, sharedFindFirstName}},
			{"/find/sample/sanger-id/seed-sanger", []string{sharedFindSecondName, sharedFindFirstName, sharedFindOtherName}},
			{"/find/sample/accession/seed-accession", []string{sharedFindSecondName, sharedFindFirstName, sharedFindOtherName}},
		} {
			samples, response := get(tc.path)
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(sampleNamesForTest(samples), convey.ShouldResemble, tc.names)
			convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, fmt.Sprint(len(tc.names)))
			convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "-1")
		}
	})

	convey.Convey("limit and offset page the matches", func() {
		samples, response := get("/find/sample/supplier-name/" + sharedFindSupplier + "?limit=1")
		convey.So(sampleNamesForTest(samples), convey.ShouldResemble, []string{sharedFindSecondName})
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "2")
		convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "1")

		samples, response = get("/find/sample/supplier-name/" + sharedFindSupplier + "?limit=1&offset=1")
		convey.So(sampleNamesForTest(samples), convey.ShouldResemble, []string{sharedFindFirstName})
		convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "-1")

		for _, query := range []string{"?offset=5", "?limit=0"} {
			samples, response = get("/find/sample/supplier-name/" + sharedFindSupplier + query)
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(samples, convey.ShouldBeEmpty)
			convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "2")
		}
	})

	convey.Convey("a value no sample holds is still not_found, even past the first page", func() {
		for _, path := range []string{"/find/sample/supplier-name/nobody", "/find/sample/supplier-name/nobody?offset=5"} {
			_, response := get(path)
			convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		}
	})

	convey.Convey("the sample resolver still reports the shared supplier name as ambiguous", func() {
		_, response := get("/resolve/sample/" + sharedFindSupplier)
		convey.So(response.Code, convey.ShouldEqual, http.StatusConflict)
		convey.So(response.Body.String(), convey.ShouldContainSubstring, `"code":"ambiguous"`)
	})
}

func TestAmbiguityErrorsNameCandidateSamplesAndCount(t *testing.T) {
	convey.Convey("Given identifiers shared by several cached samples or studies", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()
		seedSharedFindSamples(t, client.cache.DB())
		seedAmbiguityCandidates(t, client.cache.DB())

		assertAmbiguityMessages(t, client)
	})
}

func TestRealMySQLAmbiguityErrorsNameCandidateSamplesAndCount(t *testing.T) {
	client := openThrowawayMySQLExportClientForTest(t)
	seedSharedFindSamples(t, client.cache.DB())
	seedAmbiguityCandidates(t, client.cache.DB())

	convey.Convey("Given identifiers shared by several samples or studies in a throwaway MySQL cache", t, func() {
		assertAmbiguityMessages(t, client)
	})
}

func assertAmbiguityMessages(t *testing.T, client *Client) {
	t.Helper()

	message := func(path string) string {
		response := performMLWHRequestForTest(t, client, http.MethodGet, path)
		convey.So(response.Code, convey.ShouldEqual, http.StatusConflict)

		var body struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		decodeMLWHJSONResponseForTest(t, response, &body)
		convey.So(body.Code, convey.ShouldEqual, "ambiguous")

		return body.Message
	}

	convey.Convey("a shared supplier name lists the Sanger names and count, not internal ids", func() {
		got := message("/resolve/sample/" + sharedFindSupplier)
		convey.So(got, convey.ShouldContainSubstring,
			fmt.Sprintf("%q matches 2 samples: %s, %s", sharedFindSupplier, sharedFindFirstName, sharedFindSecondName))
		convey.So(got, convey.ShouldNotContainSubstring, fmt.Sprint(sharedFindFirstTmpID))
		convey.So(got, convey.ShouldNotContainSubstring, fmt.Sprint(sharedFindSecondTmpID))
	})

	convey.Convey("more candidates than the cap list the first ten and how many more", func() {
		for _, path := range []string{"/resolve/sample/MANYSUP", "/resolve/sample/MANYDONOR"} {
			got := message(path)
			convey.So(got, convey.ShouldContainSubstring, "matches 12 samples: MANY00, MANY01, ")
			convey.So(got, convey.ShouldContainSubstring, "MANY09 and 2 more")
			convey.So(got, convey.ShouldNotContainSubstring, "MANY10")
			convey.So(got, convey.ShouldNotContainSubstring, "5000000")
		}
	})

	convey.Convey("a value naming different samples through different fields lists both by name", func() {
		got := message("/resolve/sample/" + sharedFindOtherName)
		convey.So(got, convey.ShouldContainSubstring,
			fmt.Sprintf("%q matches 2 samples: %s, 5534STDY9000002", sharedFindOtherName, sharedFindOtherName))
	})

	convey.Convey("a shared study name lists the study ids and count", func() {
		got := message("/resolve/study/Shared%20study%20name")
		convey.So(got, convey.ShouldContainSubstring, `"Shared study name" matches 2 studies: 7001, 7002`)
	})
}

func TestFindUniqueSampleReportsSharedValuesAsAmbiguous(t *testing.T) {
	convey.Convey("Given a Sanger sample id shared by three cached samples and a LIMS id held by one", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()
		seedSharedFindSamples(t, client.cache.DB())

		convey.Convey("the single-sample lookup enrichment uses names every candidate", func() {
			samples, err := client.findUniqueSample(context.Background(), sangerIDSampleFinder, "seed-sanger")

			convey.So(samples, convey.ShouldBeNil)
			convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring,
				fmt.Sprintf(`"seed-sanger" matches 3 samples: %s, %s, %s`, sharedFindSecondName, sharedFindFirstName, sharedFindOtherName))
		})

		convey.Convey("a value one sample holds returns that sample with its fan-out", func() {
			samples, err := client.findUniqueSample(context.Background(), limsIDSampleFinder, fmt.Sprint(sharedFindOtherTmpID+100))

			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleNamesForTest(samples), convey.ShouldResemble, []string{sharedFindOtherName})
		})
	})
}
