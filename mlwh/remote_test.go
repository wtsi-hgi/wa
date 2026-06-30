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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

var _ Queryer = (*RemoteClient)(nil)

func TestRemoteClientSamplesForStudyRoundTrips(t *testing.T) {
	convey.Convey("Given a RemoteClient pointed at a server returning samples", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, []Sample{
			{IDSampleTmp: 1, Name: "Alpha"},
			{IDSampleTmp: 2, Name: "Beta"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when SamplesForStudy runs, then it sends the registry path and returns decoded samples", func() {
			samples, err := client.SamplesForStudy(context.Background(), "6568", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []Sample{
				{IDSampleTmp: 1, Name: "Alpha"},
				{IDSampleTmp: 2, Name: "Beta"},
			})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/study/6568/samples?limit=100&offset=0")
		})
	})
}

func TestRemoteClientMapsNotFoundEnvelope(t *testing.T) {
	convey.Convey("Given a server returning a not_found envelope", t, func() {
		server := newRemoteClientErrorServerForTest(http.StatusNotFound, "not_found")
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when ResolveStudy runs, then the error wraps ErrNotFound", func() {
			_, err := client.ResolveStudy(context.Background(), "missing")

			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})
}

func TestRemoteClientMapsCacheNeverSyncedListEnvelope(t *testing.T) {
	convey.Convey("Given a list endpoint returns a cache_never_synced envelope", t, func() {
		server := newRemoteClientErrorServerForTest(http.StatusServiceUnavailable, "cache_never_synced")
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when SamplesForStudy runs, then the error wraps ErrCacheNeverSynced and ErrNotFound", func() {
			_, err := client.SamplesForStudy(context.Background(), "6568", 100, 0)

			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})
}

func TestRemoteClientMapsAmbiguousEnvelope(t *testing.T) {
	convey.Convey("Given a server returning an ambiguous envelope", t, func() {
		server := newRemoteClientErrorServerForTest(http.StatusConflict, "ambiguous")
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when ResolveStudy runs, then the error wraps ErrAmbiguous", func() {
			_, err := client.ResolveStudy(context.Background(), "ambiguous")

			convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
		})
	})
}

func TestRemoteClientMapsUnsupportedIdentifierEnvelope(t *testing.T) {
	convey.Convey("Given a server returning an unsupported_identifier envelope", t, func() {
		server := newRemoteClientErrorServerForTest(http.StatusUnprocessableEntity, "unsupported_identifier")
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when ClassifyIdentifier runs, then the error wraps ErrUnsupportedIdentifier", func() {
			_, err := client.ClassifyIdentifier(context.Background(), "SQSCP")

			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		})
	})
}

func TestRemoteClientMapsUpstreamImpairedEnvelope(t *testing.T) {
	convey.Convey("Given a server returning an upstream_impaired envelope", t, func() {
		server := newRemoteClientErrorServerForTest(http.StatusBadGateway, "upstream_impaired")
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when ResolveStudy runs, then the error wraps ErrUpstreamImpaired", func() {
			_, err := client.ResolveStudy(context.Background(), "x")

			convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		})
	})
}

func newRemoteClientErrorServerForTest(status int, code string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		writeRemoteClientJSONForTest(w, map[string]string{
			"code":    code,
			"message": fmt.Sprintf("server returned %s", code),
		})
	}))
}

func TestRemoteClientEscapesPathIdentifiers(t *testing.T) {
	convey.Convey("Given a path identifier containing slashes and spaces", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, SampleDetail{Sample: Sample{Name: "S/A B"}})
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when SampleDetail runs, then the outbound path segment is URL-escaped", func() {
			detail, err := client.SampleDetail(context.Background(), "S/A B")

			convey.So(err, convey.ShouldBeNil)
			convey.So(detail.Sample.Name, convey.ShouldEqual, "S/A B")
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/sample/S%2FA%20B/detail")
		})
	})
}

func TestRemoteClientSearchSamplesRoundTripsA4(t *testing.T) {
	convey.Convey("A4.5: Given a RemoteClient pointed at a server returning two samples", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, []Sample{
			{IDSampleTmp: 1, Name: "Alpha"},
			{IDSampleTmp: 2, Name: "Beta"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when SearchSamples runs, then the request path is /search/sample/acme?limit=100&offset=0 and it returns the two samples", func() {
			samples, err := client.SearchSamples(context.Background(), "acme", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []Sample{
				{IDSampleTmp: 1, Name: "Alpha"},
				{IDSampleTmp: 2, Name: "Beta"},
			})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/search/sample/acme?limit=100&offset=0")
		})
	})

	convey.Convey("A4.5 (studies): Given a RemoteClient pointed at a server returning two studies", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, []Study{
			{IDStudyLims: "1", Name: "Malaria genomics"},
			{IDStudyLims: "2", Name: "Malaria vaccine"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when SearchStudies runs, then the request path carries the term and pagination and it returns the two studies", func() {
			studies, err := client.SearchStudies(context.Background(), "malar", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldResemble, []Study{
				{IDStudyLims: "1", Name: "Malaria genomics"},
				{IDStudyLims: "2", Name: "Malaria vaccine"},
			})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/search/study/malar?limit=100&offset=0")
		})
	})
}

func TestRemoteClientSearchEscapesTermSegmentA4(t *testing.T) {
	convey.Convey("A4.6: Given a search term containing a slash and spaces", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, []Sample{})
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when SearchSamples runs, then the term path segment is URL-escaped", func() {
			_, err := client.SearchSamples(context.Background(), "a/c me", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/search/sample/a%2Fc%20me?limit=100&offset=0")
		})
	})
}

func TestRemoteClientCountSampleSearchRoundTripsF3(t *testing.T) {
	convey.Convey("F3.4: Given a RemoteClient over a server returning a Count", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, Count{Count: 3})
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when CountSampleSearch runs, then the path is /search/sample/acme/count and it returns the server's Count", func() {
			count, err := client.CountSampleSearch(context.Background(), "acme")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 3})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/search/sample/acme/count")
		})
	})
}

func TestRemoteClientCountStudiesRoundTripsF3(t *testing.T) {
	convey.Convey("F3 (studies count): Given a RemoteClient over a server returning a Count", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, Count{Count: 7})
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when CountStudies runs, then the path is /studies/count and it returns the server's Count", func() {
			count, err := client.CountStudies(context.Background())

			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 7})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/studies/count")
		})
	})
}

func TestRemoteClientCountSamplesWithDataRoundTrips(t *testing.T) {
	convey.Convey("Given a RemoteClient over a server returning a Count", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, Count{Count: 9})
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when CountSamplesWithData runs, then the path is /study/S1/samples-with-data/count and it returns the server's Count", func() {
			count, err := client.CountSamplesWithData(context.Background(), "S1")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 9})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/study/S1/samples-with-data/count")
		})
	})
}

// G1 acceptance test 3 (StatusBreakdown): the new endpoint round-trips through the
// RemoteClient to the same typed StatusBreakdown the local Client returns, over the
// path /study/:id/status-breakdown.
func TestRemoteClientStatusBreakdownRoundTrips(t *testing.T) {
	convey.Convey("Given a RemoteClient over a server returning a StatusBreakdown", t, func() {
		requestURIs := make(chan string, 1)
		expected := StatusBreakdown{
			IDStudyLims:          "S4",
			Distinct:             PhaseLadder{WithData: 3, SequencedNoData: 1, Registered: 1},
			PerPlatform:          []PlatformPhaseLadder{{Platform: "Illumina", Ladder: PhaseLadder{WithData: 3, SequencedNoData: 1}}},
			WithDetailedTimeline: 2,
			CacheSyncedAt:        "2026-06-27T07:00:00Z",
		}
		server := newRemoteClientJSONServerForTest(requestURIs, expected)
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when StatusBreakdown runs, then the path is /study/S4/status-breakdown and it returns the server's StatusBreakdown", func() {
			breakdown, err := client.StatusBreakdown(context.Background(), "S4")

			convey.So(err, convey.ShouldBeNil)
			convey.So(breakdown, convey.ShouldResemble, expected)
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/study/S4/status-breakdown")
		})
	})
}

func TestRemoteClientCallMatchesTypedResolveSample(t *testing.T) {
	convey.Convey("Given a stub MLWH server returning a sample Match", t, func() {
		requestURIs := make(chan string, 2)
		match := Match{Kind: KindSangerSampleName, Canonical: "SANGER1", Sample: &Sample{IDSampleTmp: 1, Name: "Alpha"}}
		server := newRemoteClientJSONServerForTest(requestURIs, match)
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when Call dispatches ResolveSample dynamically, then it yields the same result as the typed method", func() {
			result, err := client.Call(context.Background(), "ResolveSample", []string{"SANGER1"}, nil)
			convey.So(err, convey.ShouldBeNil)

			typed, ok := result.(*Match)
			convey.So(ok, convey.ShouldBeTrue)

			expected, err := client.ResolveSample(context.Background(), "SANGER1")
			convey.So(err, convey.ShouldBeNil)
			convey.So(*typed, convey.ShouldResemble, expected)

			convey.So(receiveRemoteClientTestValue(t, requestURIs, "Call request URI"), convey.ShouldEqual, "/resolve/sample/SANGER1")
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "typed request URI"), convey.ShouldEqual, "/resolve/sample/SANGER1")
		})
	})
}

func TestRemoteClientCallPaginationPassthrough(t *testing.T) {
	convey.Convey("Given a stub MLWH server returning a page of studies", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, []Study{
			{IDStudyLims: "1", Name: "Malaria genomics"},
			{IDStudyLims: "2", Name: "Malaria vaccine"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when Call drives AllStudies with limit/offset query values, then it returns the decoded rows and forwards the pagination", func() {
			result, err := client.Call(context.Background(), "AllStudies", nil, url.Values{"limit": {"2"}, "offset": {"0"}})
			convey.So(err, convey.ShouldBeNil)

			studies, ok := result.(*[]Study)
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(*studies, convey.ShouldResemble, []Study{
				{IDStudyLims: "1", Name: "Malaria genomics"},
				{IDStudyLims: "2", Name: "Malaria vaccine"},
			})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/studies?limit=2&offset=0")
		})
	})
}

func TestRemoteClientCallUnknownMethod(t *testing.T) {
	convey.Convey("Given a RemoteClient", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, Match{})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when Call is given an unknown Registry method, then it returns an error", func() {
			result, err := client.Call(context.Background(), "NoSuchMethod", nil, nil)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(result, convey.ShouldBeNil)
		})
	})
}

func TestRemoteClientCallPathParamArityMismatch(t *testing.T) {
	convey.Convey("Given a RemoteClient", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONServerForTest(requestURIs, Match{})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when Call dispatches a path-param endpoint with no path params, then it returns an error", func() {
			result, err := client.Call(context.Background(), "ResolveSample", nil, nil)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(result, convey.ShouldBeNil)
		})
	})
}

func newRemoteClientJSONServerForTest[T any](requestURIs chan<- string, result T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURIs <- r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		writeRemoteClientJSONForTest(w, result)
	}))
}

func TestRemoteClientSamplesForStudyPageReadsSizingHeadersE2(t *testing.T) {
	convey.Convey("E2.3: Given a RemoteClient Page variant against a server returning the sizing headers", t, func() {
		local := newListSizingClientForTest(t, "SZ", 25)
		defer closeParityClientForTest(t, local)
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		convey.Convey("when SamplesForStudyPage runs with limit=10&offset=0, then Total==25, NextOffset==10, and Items equals the bare-slice SamplesForStudy result for the same args", func() {
			page, err := remote.SamplesForStudyPage(context.Background(), "SZ", 10, 0)
			convey.So(err, convey.ShouldBeNil)

			convey.So(page.Total, convey.ShouldEqual, 25)
			convey.So(page.NextOffset, convey.ShouldEqual, 10)
			convey.So(page.Items, convey.ShouldHaveLength, 10)

			bare, err := remote.SamplesForStudy(context.Background(), "SZ", 10, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(page.Items, convey.ShouldResemble, bare)
		})

		convey.Convey("when SamplesForStudyPage runs on the last page (limit=10&offset=20), then Total==25 and NextOffset==-1", func() {
			page, err := remote.SamplesForStudyPage(context.Background(), "SZ", 10, 20)
			convey.So(err, convey.ShouldBeNil)

			convey.So(page.Total, convey.ShouldEqual, 25)
			convey.So(page.NextOffset, convey.ShouldEqual, -1)
			convey.So(page.Items, convey.ShouldHaveLength, 5)
		})
	})
}

func TestRemoteClientIRODSPathsForStudyPageReadsSizingHeadersE2(t *testing.T) {
	convey.Convey("E2.3 (irods): Given a RemoteClient Page variant against a server returning the sizing headers", t, func() {
		local := newListSizingClientForTest(t, "SZ", 25)
		defer closeParityClientForTest(t, local)
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		convey.Convey("when IRODSPathsForStudyPage runs with limit=10&offset=0, then Total==25, NextOffset==10, and Items equals the bare-slice IRODSPathsForStudy result", func() {
			page, err := remote.IRODSPathsForStudyPage(context.Background(), "SZ", 10, 0)
			convey.So(err, convey.ShouldBeNil)

			convey.So(page.Total, convey.ShouldEqual, 25)
			convey.So(page.NextOffset, convey.ShouldEqual, 10)
			convey.So(page.Items, convey.ShouldHaveLength, 10)

			bare, err := remote.IRODSPathsForStudy(context.Background(), "SZ", 10, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(page.Items, convey.ShouldResemble, bare)
		})
	})
}

// B3: the RemoteClient run-scope Page variant reads the X-Total-Count /
// X-Next-Offset sizing headers into Page.Total / Page.NextOffset and its Items
// equal the bare-slice IRODSPathsForRun result, exactly like the study Page
// variant. newListSizingClientForTest seeds 25 iRODS objects on run 99000.
func TestRemoteClientIRODSPathsForRunPageReadsSizingHeadersB3(t *testing.T) {
	convey.Convey("B3 (irods-run): Given a RemoteClient Page variant against a server returning the sizing headers", t, func() {
		local := newListSizingClientForTest(t, "SZ", 25)
		defer closeParityClientForTest(t, local)
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		convey.Convey("when IRODSPathsForRunPage runs with limit=10&offset=0, then Total==25, NextOffset==10, and Items equals the bare-slice IRODSPathsForRun result", func() {
			page, err := remote.IRODSPathsForRunPage(context.Background(), "99000", 10, 0)
			convey.So(err, convey.ShouldBeNil)

			convey.So(page.Total, convey.ShouldEqual, 25)
			convey.So(page.NextOffset, convey.ShouldEqual, 10)
			convey.So(page.Items, convey.ShouldHaveLength, 10)

			bare, err := remote.IRODSPathsForRun(context.Background(), "99000", "", 10, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(page.Items, convey.ShouldResemble, bare)
		})
	})
}

func TestRemoteClientSamplesWithDataPageReadsSizingHeadersE2(t *testing.T) {
	convey.Convey("E2.3 (samples-with-data): Given a RemoteClient Page variant over the feature's new paginated list", t, func() {
		local := newListSizingClientForTest(t, "SZ", 25)
		defer closeParityClientForTest(t, local)
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		convey.Convey("when SamplesWithDataPage runs with limit=10&offset=0, then Total==25, NextOffset==10, and Items equals the bare-slice SamplesWithData result", func() {
			page, err := remote.SamplesWithDataPage(context.Background(), "SZ", 10, 0)
			convey.So(err, convey.ShouldBeNil)

			convey.So(page.Total, convey.ShouldEqual, 25)
			convey.So(page.NextOffset, convey.ShouldEqual, 10)
			convey.So(page.Items, convey.ShouldHaveLength, 10)

			bare, err := remote.SamplesWithData(context.Background(), "SZ", 10, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(page.Items, convey.ShouldResemble, bare)
		})
	})
}

func TestRemoteClientSamplesWithoutDataPageReadsSizingHeadersE2(t *testing.T) {
	convey.Convey("E2.3 (samples-without-data): Given a study where every sample has data, when SamplesWithoutDataPage runs", t, func() {
		local := newListSizingClientForTest(t, "SZ", 25)
		defer closeParityClientForTest(t, local)
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		convey.Convey("then Total is the without-data total (0 here: all 25 have data) and Items matches the bare-slice result", func() {
			page, err := remote.SamplesWithoutDataPage(context.Background(), "SZ", 10, 0)
			convey.So(err, convey.ShouldBeNil)

			convey.So(page.Total, convey.ShouldEqual, 0)
			convey.So(page.NextOffset, convey.ShouldEqual, -1)
			convey.So(page.Items, convey.ShouldHaveLength, 0)

			bare, err := remote.SamplesWithoutData(context.Background(), "SZ", 10, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(page.Items, convey.ShouldResemble, bare)
		})
	})
}

// G1 acceptance test 3 (the three new people Page variants): each
// RemoteClient Page variant reads the X-Total-Count / X-Next-Offset sizing
// headers into Page.Total / Page.NextOffset and its Items equal the bare-slice
// result of the matching list method. seedCarlResolveFixture seeds 91 SQSCP
// studies whose faculty_sponsor is "Carl Anderson", with a study_users owner row
// (login ca3) on 59 of them, so the sponsor list totals 91, the user list totals
// 59 and resolve-person totals 2 candidates -- each total exceeds its page, so
// NextOffset is a real next page, not the -1 end-of-list marker.
func TestRemoteClientPeoplePageReadsSizingHeadersG1(t *testing.T) {
	convey.Convey("G1 (people): Given a RemoteClient over a Client whose cache has the Carl people fixture", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedCarlResolveFixture(t, cache)

		local := &Client{cache: cache, cacheReader: cacheReadDB(cache)}
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		convey.Convey("when StudiesForFacultySponsorPage runs with limit=10&offset=0, then Total==91, NextOffset==10, and Items equals the bare-slice result", func() {
			page, err := remote.StudiesForFacultySponsorPage(context.Background(), "Carl", 10, 0)
			convey.So(err, convey.ShouldBeNil)

			convey.So(page.Total, convey.ShouldEqual, 91)
			convey.So(page.NextOffset, convey.ShouldEqual, 10)
			convey.So(page.Items, convey.ShouldHaveLength, 10)

			bare, err := remote.StudiesForFacultySponsor(context.Background(), "Carl", 10, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(page.Items, convey.ShouldResemble, bare)
		})

		convey.Convey("when StudiesForUserPage runs with limit=10&offset=0, then Total==59, NextOffset==10, and Items equals the bare-slice result", func() {
			page, err := remote.StudiesForUserPage(context.Background(), "ca3", "", 10, 0)
			convey.So(err, convey.ShouldBeNil)

			convey.So(page.Total, convey.ShouldEqual, 59)
			convey.So(page.NextOffset, convey.ShouldEqual, 10)
			convey.So(page.Items, convey.ShouldHaveLength, 10)

			bare, err := remote.StudiesForUser(context.Background(), "ca3", "", 10, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(page.Items, convey.ShouldResemble, bare)
		})

		convey.Convey("when ResolvePersonPage runs with limit=1&offset=0, then Total==2, NextOffset==1, and Items equals the bare-slice result", func() {
			page, err := remote.ResolvePersonPage(context.Background(), "carl", 1, 0)
			convey.So(err, convey.ShouldBeNil)

			convey.So(page.Total, convey.ShouldEqual, 2)
			convey.So(page.NextOffset, convey.ShouldEqual, 1)
			convey.So(page.Items, convey.ShouldHaveLength, 1)

			bare, err := remote.ResolvePerson(context.Background(), "carl", 1, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(page.Items, convey.ShouldResemble, bare)
		})
	})
}

func TestRemoteClientAddsBearerToken(t *testing.T) {
	convey.Convey("Given RemoteConfig.Token is set", t, func() {
		authHeaders := make(chan string, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeaders <- r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			writeRemoteClientJSONForTest(w, Match{Kind: KindStudyLimsID, Canonical: "6568"})
		}))
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "test-token")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when ClassifyIdentifier runs, then the request carries a Bearer header", func() {
			_, err := client.ClassifyIdentifier(context.Background(), "6568")

			convey.So(err, convey.ShouldBeNil)
			convey.So(receiveRemoteClientTestValue(t, authHeaders, "auth header"), convey.ShouldEqual, "Bearer test-token")
		})
	})
}

func writeRemoteClientJSONForTest(w http.ResponseWriter, value any) {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(fmt.Sprintf("encode remote client test JSON: %v", err))
	}
}

func newRemoteClientForTest(t *testing.T, baseURL string, token string) *RemoteClient {
	t.Helper()

	client, err := NewRemoteClient(RemoteConfig{BaseURL: baseURL, Token: token})
	convey.So(err, convey.ShouldBeNil)

	return client
}

func closeRemoteClientForTest(t *testing.T, client *RemoteClient) {
	t.Helper()

	convey.So(client.Close(), convey.ShouldBeNil)
}

func receiveRemoteClientTestValue(t *testing.T, values <-chan string, name string) string {
	t.Helper()

	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}

	return ""
}
