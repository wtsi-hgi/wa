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
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

var _ Queryer = (*RemoteClient)(nil)

type remotePageMethodCase struct {
	name               string
	response           any
	expectedURI        string
	wantWithHeaders    any
	wantWithoutHeaders any
	emptyPage          any
	call               func(context.Context, *RemoteClient) (any, error)
}

func b1RemotePageMethodCases() []remotePageMethodCase {
	studies := []Study{{IDStudyLims: "1", Name: "Alpha"}, {IDStudyLims: "2", Name: "Beta"}}
	samples := []Sample{{IDSampleTmp: 1, Name: "Alpha"}, {IDSampleTmp: 2, Name: "Beta"}}
	libraries := []Library{
		{PipelineIDLims: "Standard", IDStudyLims: "6568", LibraryID: "L1"},
		{PipelineIDLims: "Custom", IDStudyLims: "6568", LibraryID: "L2"},
	}
	runs := []Run{{IDRun: 12345}, {IDRun: 12346}}
	lanes := []Lane{{IDRun: 12345, Position: 1, TagIndex: 0}, {IDRun: 12346, Position: 2, TagIndex: 1}}

	return []remotePageMethodCase{
		{
			name:               "AllStudiesPage",
			response:           studies,
			expectedURI:        "/studies?limit=2&offset=2",
			wantWithHeaders:    Page[Study]{Items: studies, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Study]{Items: studies, Total: 0, NextOffset: -1},
			emptyPage:          Page[Study]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.AllStudiesPage(ctx, 2, 2)
			},
		},
		{
			name:               "SamplesForRunPage",
			response:           samples,
			expectedURI:        "/run/12345/samples?limit=2&offset=2",
			wantWithHeaders:    Page[Sample]{Items: samples, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Sample]{Items: samples, Total: 0, NextOffset: -1},
			emptyPage:          Page[Sample]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForRunPage(ctx, "12345", 2, 2)
			},
		},
		{
			name:               "SamplesForLibraryPage",
			response:           samples,
			expectedURI:        "/library/Standard/study/6568/samples?limit=2&offset=2",
			wantWithHeaders:    Page[Sample]{Items: samples, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Sample]{Items: samples, Total: 0, NextOffset: -1},
			emptyPage:          Page[Sample]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForLibraryPage(ctx, "Standard", "6568", 2, 2)
			},
		},
		{
			name:               "SamplesForLibraryIDPage",
			response:           samples,
			expectedURI:        "/library-id/LIB123/samples?limit=2&offset=2",
			wantWithHeaders:    Page[Sample]{Items: samples, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Sample]{Items: samples, Total: 0, NextOffset: -1},
			emptyPage:          Page[Sample]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForLibraryIDPage(ctx, "LIB123", 2, 2)
			},
		},
		{
			name:               "SamplesForLibraryLimsIDPage",
			response:           samples,
			expectedURI:        "/library-lims-id/LIBLIMS123/samples?limit=2&offset=2",
			wantWithHeaders:    Page[Sample]{Items: samples, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Sample]{Items: samples, Total: 0, NextOffset: -1},
			emptyPage:          Page[Sample]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForLibraryLimsIDPage(ctx, "LIBLIMS123", 2, 2)
			},
		},
		{
			name:               "SamplesForLibraryTypePage",
			response:           samples,
			expectedURI:        "/library-type/Standard/samples?limit=2&offset=2",
			wantWithHeaders:    Page[Sample]{Items: samples, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Sample]{Items: samples, Total: 0, NextOffset: -1},
			emptyPage:          Page[Sample]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForLibraryTypePage(ctx, "Standard", 2, 2)
			},
		},
		{
			name:               "LibrariesForStudyPage",
			response:           libraries,
			expectedURI:        "/study/6568/libraries?limit=2&offset=2",
			wantWithHeaders:    Page[Library]{Items: libraries, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Library]{Items: libraries, Total: 0, NextOffset: -1},
			emptyPage:          Page[Library]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.LibrariesForStudyPage(ctx, "6568", 2, 2)
			},
		},
		{
			name:               "RunsForStudyPage",
			response:           runs,
			expectedURI:        "/study/6568/runs?limit=2&offset=2",
			wantWithHeaders:    Page[Run]{Items: runs, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Run]{Items: runs, Total: 0, NextOffset: -1},
			emptyPage:          Page[Run]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.RunsForStudyPage(ctx, "6568", 2, 2)
			},
		},
		{
			name:               "LanesForSamplePage",
			response:           lanes,
			expectedURI:        "/sample/S1/lanes?limit=2&offset=2",
			wantWithHeaders:    Page[Lane]{Items: lanes, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Lane]{Items: lanes, Total: 0, NextOffset: -1},
			emptyPage:          Page[Lane]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.LanesForSamplePage(ctx, "S1", 2, 2)
			},
		},
		{
			name:               "SearchStudiesPage",
			response:           studies,
			expectedURI:        "/search/study/malar?limit=2&offset=2",
			wantWithHeaders:    Page[Study]{Items: studies, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Study]{Items: studies, Total: 0, NextOffset: -1},
			emptyPage:          Page[Study]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SearchStudiesPage(ctx, "malar", 2, 2)
			},
		},
		{
			name:               "SearchSamplesPage",
			response:           samples,
			expectedURI:        "/search/sample/acme?limit=2&offset=2",
			wantWithHeaders:    Page[Sample]{Items: samples, Total: 5, NextOffset: 4},
			wantWithoutHeaders: Page[Sample]{Items: samples, Total: 0, NextOffset: -1},
			emptyPage:          Page[Sample]{},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SearchSamplesPage(ctx, "acme", 2, 2)
			},
		},
	}
}

type remotePageParityCase struct {
	name     string
	pageRows func(context.Context, *RemoteClient) (any, error)
	bareRows func(context.Context, *RemoteClient) (any, error)
}

func b1RemotePageParityCases() []remotePageParityCase {
	return []remotePageParityCase{
		{
			name: "AllStudiesPage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.AllStudiesPage(ctx, 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.AllStudies(ctx, 10, 0)
			},
		},
		{
			name: "SamplesForRunPage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.SamplesForRunPage(ctx, "99000", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForRun(ctx, "99000", 10, 0)
			},
		},
		{
			name: "SamplesForLibraryPage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.SamplesForLibraryPage(ctx, "Standard", "SZ", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForLibrary(ctx, "Standard", "SZ", 10, 0)
			},
		},
		{
			name: "SamplesForLibraryIDPage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.SamplesForLibraryIDPage(ctx, "LIB-SZ", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForLibraryID(ctx, "LIB-SZ", 10, 0)
			},
		},
		{
			name: "SamplesForLibraryLimsIDPage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.SamplesForLibraryLimsIDPage(ctx, "LIMS-SZ", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForLibraryLimsID(ctx, "LIMS-SZ", 10, 0)
			},
		},
		{
			name: "SamplesForLibraryTypePage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.SamplesForLibraryTypePage(ctx, "Standard", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SamplesForLibraryType(ctx, "Standard", 10, 0)
			},
		},
		{
			name: "LibrariesForStudyPage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.LibrariesForStudyPage(ctx, "SZ", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.LibrariesForStudy(ctx, "SZ", 10, 0)
			},
		},
		{
			name: "RunsForStudyPage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.RunsForStudyPage(ctx, "SZ", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.RunsForStudy(ctx, "SZ", 10, 0)
			},
		},
		{
			name: "LanesForSamplePage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.LanesForSamplePage(ctx, "sizing-900000", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.LanesForSample(ctx, "sizing-900000", 10, 0)
			},
		},
		{
			name: "SearchStudiesPage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.SearchStudiesPage(ctx, "Study", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SearchStudies(ctx, "Study", 10, 0)
			},
		},
		{
			name: "SearchSamplesPage",
			pageRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				page, err := client.SearchSamplesPage(ctx, "sizing", 10, 0)

				return page.Items, err
			},
			bareRows: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.SearchSamples(ctx, "sizing", 10, 0)
			},
		},
	}
}

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

func TestRemoteClientCallWithHeadersReturnsBodyAndHeadersA1(t *testing.T) {
	convey.Convey("A1.1: Given a stub MLWH server returning one study and sizing headers", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONHeaderServerForTest(requestURIs, []Study{
			{IDStudyLims: "1"},
		}, http.Header{
			"X-Total-Count": {"7"},
			"X-Next-Offset": {"2"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when CallWithHeaders drives AllStudies with limit/offset query values, then it returns the decoded rows and exposes the response headers", func() {
			result, headers, err := client.CallWithHeaders(context.Background(), "AllStudies", nil, url.Values{"limit": {"1"}, "offset": {"1"}})

			convey.So(err, convey.ShouldBeNil)
			studies, ok := result.(*[]Study)
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(*studies, convey.ShouldResemble, []Study{{IDStudyLims: "1"}})
			convey.So(headers.Get("X-Total-Count"), convey.ShouldEqual, "7")
			convey.So(headers.Get("X-Next-Offset"), convey.ShouldEqual, "2")
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/studies?limit=1&offset=1")
		})
	})
}

func TestRemoteClientCallWrapsCallWithHeadersA1(t *testing.T) {
	convey.Convey("A1.2: Given the same stub MLWH server returning one study and sizing headers", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONHeaderServerForTest(requestURIs, []Study{
			{IDStudyLims: "1"},
		}, http.Header{
			"X-Total-Count": {"7"},
			"X-Next-Offset": {"2"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when Call drives AllStudies with the same query, then it returns only the decoded body", func() {
			result, err := client.Call(context.Background(), "AllStudies", nil, url.Values{"limit": {"1"}, "offset": {"1"}})

			convey.So(err, convey.ShouldBeNil)
			studies, ok := result.(*[]Study)
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(*studies, convey.ShouldResemble, []Study{{IDStudyLims: "1"}})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/studies?limit=1&offset=1")
		})
	})
}

func TestRemoteClientSimpleBareListPageMethodsReadHeadersB1(t *testing.T) {
	for _, tc := range b1RemotePageMethodCases() {
		tc := tc
		convey.Convey("B1.1: Given "+tc.name+" receives two bare-list rows and sizing headers", t, func() {
			requestURIs := make(chan string, 1)
			server := newRemoteClientJSONHeaderServerForTest(requestURIs, tc.response, http.Header{
				"X-Total-Count": {"5"},
				"X-Next-Offset": {"4"},
			})
			defer server.Close()

			client := newRemoteClientForTest(t, server.URL, "")
			defer closeRemoteClientForTest(t, client)

			convey.Convey("when the page method runs with limit=2 and offset=2, then it returns rows, headers, and the body-only endpoint URI", func() {
				page, err := tc.call(context.Background(), client)

				convey.So(err, convey.ShouldBeNil)
				convey.So(page, convey.ShouldResemble, tc.wantWithHeaders)
				convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, tc.expectedURI)
			})
		})
	}
}

func TestRemoteClientSimpleBareListPageMethodsUseHeaderFallbacksB1(t *testing.T) {
	for _, tc := range b1RemotePageMethodCases() {
		tc := tc
		convey.Convey("B1.3: Given "+tc.name+" receives no sizing headers", t, func() {
			requestURIs := make(chan string, 1)
			server := newRemoteClientJSONHeaderServerForTest(requestURIs, tc.response, nil)
			defer server.Close()

			client := newRemoteClientForTest(t, server.URL, "")
			defer closeRemoteClientForTest(t, client)

			convey.Convey("when the page method succeeds, then Total is 0 and NextOffset is -1", func() {
				page, err := tc.call(context.Background(), client)

				convey.So(err, convey.ShouldBeNil)
				convey.So(page, convey.ShouldResemble, tc.wantWithoutHeaders)
			})
		})
	}
}

func TestRemoteClientSimpleBareListPageMethodsPreserveSentinelsB1(t *testing.T) {
	for _, tc := range b1RemotePageMethodCases() {
		tc := tc
		convey.Convey("B1.4: Given "+tc.name+" receives a cache_never_synced envelope", t, func() {
			server := newRemoteClientErrorServerForTest(http.StatusServiceUnavailable, "cache_never_synced")
			defer server.Close()

			client := newRemoteClientForTest(t, server.URL, "")
			defer closeRemoteClientForTest(t, client)

			convey.Convey("when the page method runs, then it returns an empty Page and the same sentinels as the body-only list", func() {
				page, err := tc.call(context.Background(), client)

				convey.So(err, convey.ShouldNotBeNil)
				convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
				convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
				convey.So(page, convey.ShouldResemble, tc.emptyPage)
			})
		})
	}
}

func TestRemoteClientSimpleBareListPageMethodsMatchBodyOnlyB1(t *testing.T) {
	convey.Convey("B1.2: Given a parity server seeded by newListSizingClientForTest", t, func() {
		local := newListSizingClientForTest(t, "SZ", 25)
		defer closeParityClientForTest(t, local)
		seedRemoteB1ListSizingExtras(t, local)

		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		for _, tc := range b1RemotePageParityCases() {
			tc := tc
			convey.Convey("when "+tc.name+" runs against its matching endpoint, then Page.Items equals the body-only result", func() {
				pageRows, err := tc.pageRows(context.Background(), remote)
				convey.So(err, convey.ShouldBeNil)

				bareRows, err := tc.bareRows(context.Background(), remote)
				convey.So(err, convey.ShouldBeNil)
				convey.So(pageRows, convey.ShouldResemble, bareRows)
			})
		}
	})
}

func seedRemoteB1ListSizingExtras(t *testing.T, client *Client) {
	t.Helper()

	_, err := client.cache.DB().Exec(`UPDATE library_samples SET library_id = ?, id_library_lims = ?`, "LIB-SZ", "LIMS-SZ")
	if err != nil {
		t.Fatalf("seed B1 library identifiers: %v", err)
	}

	rebuildSampleSearchIndexForTest(t, client.cache.DB())
}

func TestRemoteClientIRODSPathsForSampleByFileTypePageC2(t *testing.T) {
	convey.Convey("C2.1: Given a stub server returning one sample-scoped IRODS path and sizing headers", t, func() {
		requestURIs := make(chan string, 1)
		paths := []IRODSPath{{IDProduct: "p1", Collection: "/seq", DataObject: "S1.cram", IRODSPath: "/seq/S1.cram"}}
		server := newRemoteClientJSONHeaderServerForTest(requestURIs, paths, http.Header{
			"X-Total-Count": {"4"},
			"X-Next-Offset": {"2"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when IRODSPathsForSampleByFileTypePage runs with cram, then it sends file_type with pagination and reads the sizing headers", func() {
			page, err := client.IRODSPathsForSampleByFileTypePage(context.Background(), "S1", "cram", 1, 1)

			convey.So(err, convey.ShouldBeNil)
			convey.So(page, convey.ShouldResemble, Page[IRODSPath]{Items: paths, Total: 4, NextOffset: 2})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "sample request URI"), convey.ShouldEqual, "/sample/S1/irods?file_type=cram&limit=1&offset=1")
		})
	})
}

func TestRemoteClientIRODSPathsForStudyAndRunByFileTypePageC2(t *testing.T) {
	convey.Convey("C2.2: Given a stub server returning one IRODS path and sizing headers", t, func() {
		paths := []IRODSPath{{IDProduct: "p1", Collection: "/seq", DataObject: "52553.cram", IRODSPath: "/seq/52553.cram"}}
		cases := []struct {
			name        string
			expectedURI string
			call        func(context.Context, *RemoteClient) (Page[IRODSPath], error)
		}{
			{
				name:        "study",
				expectedURI: "/study/ST1/irods?file_type=cram&limit=1&offset=1",
				call: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForStudyByFileTypePage(ctx, "ST1", "cram", 1, 1)
				},
			},
			{
				name:        "run",
				expectedURI: "/run/52553/irods?file_type=cram&limit=1&offset=1",
				call: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForRunByFileTypePage(ctx, "52553", "cram", 1, 1)
				},
			},
		}

		for _, tc := range cases {
			tc := tc
			convey.Convey("when the "+tc.name+" filtered page method runs with cram, then it sends file_type with pagination and reads the sizing headers", func() {
				requestURIs := make(chan string, 1)
				server := newRemoteClientJSONHeaderServerForTest(requestURIs, paths, http.Header{
					"X-Total-Count": {"4"},
					"X-Next-Offset": {"2"},
				})
				defer server.Close()

				client := newRemoteClientForTest(t, server.URL, "")
				defer closeRemoteClientForTest(t, client)

				page, err := tc.call(context.Background(), client)

				convey.So(err, convey.ShouldBeNil)
				convey.So(page, convey.ShouldResemble, Page[IRODSPath]{Items: paths, Total: 4, NextOffset: 2})
				convey.So(receiveRemoteClientTestValue(t, requestURIs, tc.name+" request URI"), convey.ShouldEqual, tc.expectedURI)
			})
		}
	})
}

func TestRemoteClientIRODSPathsByFileTypePageOmitsEmptyFileTypeC2(t *testing.T) {
	convey.Convey("C2.3: Given a stub server returning one IRODS path and sizing headers", t, func() {
		paths := []IRODSPath{{IDProduct: "p1", Collection: "/seq", DataObject: "all.bam", IRODSPath: "/seq/all.bam"}}
		cases := []struct {
			name           string
			expectedURI    string
			filteredPage   func(context.Context, *RemoteClient) (Page[IRODSPath], error)
			unfilteredPage func(context.Context, *RemoteClient) (Page[IRODSPath], error)
		}{
			{
				name:        "sample",
				expectedURI: "/sample/S1/irods?limit=1&offset=1",
				filteredPage: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForSampleByFileTypePage(ctx, "S1", "", 1, 1)
				},
				unfilteredPage: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForSamplePage(ctx, "S1", 1, 1)
				},
			},
			{
				name:        "study",
				expectedURI: "/study/ST1/irods?limit=1&offset=1",
				filteredPage: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForStudyByFileTypePage(ctx, "ST1", "", 1, 1)
				},
				unfilteredPage: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForStudyPage(ctx, "ST1", 1, 1)
				},
			},
			{
				name:        "run",
				expectedURI: "/run/52553/irods?limit=1&offset=1",
				filteredPage: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForRunByFileTypePage(ctx, "52553", "", 1, 1)
				},
				unfilteredPage: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForRunPage(ctx, "52553", 1, 1)
				},
			},
		}

		for _, tc := range cases {
			tc := tc
			convey.Convey("when the "+tc.name+" filtered page method receives an empty fileType, then file_type is omitted and the result equals the unfiltered page method", func() {
				requestURIs := make(chan string, 2)
				server := newRemoteClientJSONHeaderServerForTest(requestURIs, paths, http.Header{
					"X-Total-Count": {"4"},
					"X-Next-Offset": {"2"},
				})
				defer server.Close()

				client := newRemoteClientForTest(t, server.URL, "")
				defer closeRemoteClientForTest(t, client)

				filteredPage, filteredErr := tc.filteredPage(context.Background(), client)
				unfilteredPage, unfilteredErr := tc.unfilteredPage(context.Background(), client)

				convey.So(filteredErr, convey.ShouldBeNil)
				convey.So(unfilteredErr, convey.ShouldBeNil)
				convey.So(filteredPage, convey.ShouldResemble, unfilteredPage)
				convey.So(receiveRemoteClientTestValue(t, requestURIs, tc.name+" filtered request URI"), convey.ShouldEqual, tc.expectedURI)
				convey.So(receiveRemoteClientTestValue(t, requestURIs, tc.name+" unfiltered request URI"), convey.ShouldEqual, tc.expectedURI)
			})
		}
	})
}

func TestRemoteClientIRODSPathsByFileTypePageInvalidFileTypeErrorC2(t *testing.T) {
	convey.Convey("C2.4: Given the upstream server rejects an invalid file_type", t, func() {
		cases := []struct {
			name        string
			expectedURI string
			call        func(context.Context, *RemoteClient) (Page[IRODSPath], error)
		}{
			{
				name:        "sample",
				expectedURI: "/sample/S1/irods?file_type=bad%2Ftype&limit=1&offset=1",
				call: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForSampleByFileTypePage(ctx, "S1", "bad/type", 1, 1)
				},
			},
			{
				name:        "study",
				expectedURI: "/study/ST1/irods?file_type=bad%2Ftype&limit=1&offset=1",
				call: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForStudyByFileTypePage(ctx, "ST1", "bad/type", 1, 1)
				},
			},
			{
				name:        "run",
				expectedURI: "/run/52553/irods?file_type=bad%2Ftype&limit=1&offset=1",
				call: func(ctx context.Context, client *RemoteClient) (Page[IRODSPath], error) {
					return client.IRODSPathsForRunByFileTypePage(ctx, "52553", "bad/type", 1, 1)
				},
			},
		}

		for _, tc := range cases {
			tc := tc
			convey.Convey("when the "+tc.name+" filtered page method receives the 400 response, then it returns the remote error and an empty page", func() {
				requestURIs := make(chan string, 1)
				server := newRemoteClientRecordingErrorServerForTest(requestURIs, http.StatusBadRequest, "bad_request")
				defer server.Close()

				client := newRemoteClientForTest(t, server.URL, "")
				defer closeRemoteClientForTest(t, client)

				page, err := tc.call(context.Background(), client)

				convey.So(page, convey.ShouldResemble, Page[IRODSPath]{})
				convey.So(err, convey.ShouldNotBeNil)
				convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
				convey.So(receiveRemoteClientTestValue(t, requestURIs, tc.name+" invalid request URI"), convey.ShouldEqual, tc.expectedURI)
			})
		}
	})
}

func newRemoteClientRecordingErrorServerForTest(requestURIs chan<- string, status int, code string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURIs <- r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		writeRemoteClientJSONForTest(w, map[string]string{
			"code":    code,
			"message": fmt.Sprintf("server returned %s", code),
		})
	}))
}

func TestRemoteClientSearchPageMethodsSendExplicitZeroPaginationB1(t *testing.T) {
	convey.Convey("B1.5: Given stub search servers", t, func() {
		cases := []struct {
			name        string
			response    any
			expectedURI string
			call        func(context.Context, *RemoteClient) (any, error)
		}{
			{
				name:        "SearchStudiesPage",
				response:    []Study{{IDStudyLims: "1", Name: "Malaria genomics"}},
				expectedURI: "/search/study/malar?limit=0&offset=0",
				call: func(ctx context.Context, client *RemoteClient) (any, error) {
					return client.SearchStudiesPage(ctx, "malar", 0, 0)
				},
			},
			{
				name:        "SearchSamplesPage",
				response:    []Sample{{IDSampleTmp: 1, Name: "acme-1"}},
				expectedURI: "/search/sample/acme?limit=0&offset=0",
				call: func(ctx context.Context, client *RemoteClient) (any, error) {
					return client.SearchSamplesPage(ctx, "acme", 0, 0)
				},
			},
		}

		for _, tc := range cases {
			tc := tc
			convey.Convey("when "+tc.name+" runs with limit=0 and offset=0, then it still sends both query params", func() {
				requestURIs := make(chan string, 1)
				server := newRemoteClientJSONHeaderServerForTest(requestURIs, tc.response, http.Header{
					"X-Total-Count": {"1"},
					"X-Next-Offset": {"-1"},
				})
				defer server.Close()

				client := newRemoteClientForTest(t, server.URL, "")
				defer closeRemoteClientForTest(t, client)

				page, err := tc.call(context.Background(), client)

				convey.So(err, convey.ShouldBeNil)
				convey.So(page, convey.ShouldNotBeNil)
				convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, tc.expectedURI)
			})
		}
	})
}

func TestRemoteClientSamplesWithDataSincePageReadsHeadersAndWindowQueryC1(t *testing.T) {
	convey.Convey("C1.1: Given a stub server returning one windowed SampleWithData row and sizing headers", t, func() {
		requestURIs := make(chan string, 1)
		rows := []SampleWithData{
			{Sample: Sample{IDSampleTmp: 1, Name: "Alpha"}, Platforms: []string{"Illumina"}},
		}
		server := newRemoteClientJSONHeaderServerForTest(requestURIs, rows, http.Header{
			"X-Total-Count": {"3"},
			"X-Next-Offset": {"2"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when SamplesWithDataSincePage runs, then it sends the windowed query and exposes header metadata", func() {
			page, err := client.SamplesWithDataSincePage(
				context.Background(),
				"S1",
				"2026-06-01T00:00:00Z",
				"2026-06-02T00:00:00Z",
				1,
				1,
			)

			convey.So(err, convey.ShouldBeNil)
			convey.So(page, convey.ShouldResemble, Page[SampleWithData]{
				Items:      rows,
				Total:      3,
				NextOffset: 2,
			})

			uri, err := url.ParseRequestURI(receiveRemoteClientTestValue(t, requestURIs, "request URI"))
			convey.So(err, convey.ShouldBeNil)
			convey.So(uri.Path, convey.ShouldEqual, "/study/S1/samples-with-data")
			convey.So(uri.Query(), convey.ShouldResemble, url.Values{
				"limit":  {"1"},
				"offset": {"1"},
				"since":  {"2026-06-01T00:00:00Z"},
				"until":  {"2026-06-02T00:00:00Z"},
			})
		})
	})
}

func TestRemoteClientSamplesWithDataSincePageOmitsEmptyWindowQueryC1(t *testing.T) {
	convey.Convey("C1.2: Given a stub server returning an empty samples-with-data page", t, func() {
		requestURIs := make(chan string, 1)
		server := newRemoteClientJSONHeaderServerForTest(requestURIs, []SampleWithData{}, nil)
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when SamplesWithDataSincePage runs with empty since and until, then only limit and offset are sent", func() {
			page, err := client.SamplesWithDataSincePage(context.Background(), "S1", "", "", 5, 10)

			convey.So(err, convey.ShouldBeNil)
			convey.So(page, convey.ShouldResemble, Page[SampleWithData]{
				Items:      []SampleWithData{},
				Total:      0,
				NextOffset: -1,
			})

			uri, err := url.ParseRequestURI(receiveRemoteClientTestValue(t, requestURIs, "request URI"))
			convey.So(err, convey.ShouldBeNil)
			convey.So(uri.Path, convey.ShouldEqual, "/study/S1/samples-with-data")
			convey.So(uri.Query(), convey.ShouldResemble, url.Values{
				"limit":  {"5"},
				"offset": {"10"},
			})
		})
	})
}

func TestRemoteClientSamplesWithDataSincePagePreservesBadRequestC1(t *testing.T) {
	convey.Convey("C1.3: Given upstream bad_request responses that also carry sizing headers", t, func() {
		requestURIs := make(chan string, 2)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestURIs <- r.URL.RequestURI()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Total-Count", "99")
			w.Header().Set("X-Next-Offset", "50")
			w.WriteHeader(http.StatusBadRequest)
			writeRemoteClientJSONForTest(w, map[string]string{
				"code":    "bad_request",
				"message": "invalid window",
			})
		}))
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when invalid timestamps or until without since are sent, then the empty Page and existing remote bad-request error are returned", func() {
			invalidPage, invalidErr := client.SamplesWithDataSincePage(context.Background(), "S1", "not-a-date", "", 1, 0)

			convey.So(invalidPage, convey.ShouldResemble, Page[SampleWithData]{})
			convey.So(invalidErr, convey.ShouldNotBeNil)
			convey.So(errors.Is(invalidErr, ErrUpstreamImpaired), convey.ShouldBeTrue)

			invalidURI, err := url.ParseRequestURI(receiveRemoteClientTestValue(t, requestURIs, "invalid timestamp request URI"))
			convey.So(err, convey.ShouldBeNil)
			convey.So(invalidURI.Query(), convey.ShouldResemble, url.Values{
				"limit":  {"1"},
				"offset": {"0"},
				"since":  {"not-a-date"},
			})

			untilOnlyPage, untilOnlyErr := client.SamplesWithDataSincePage(context.Background(), "S1", "", "2026-06-02T00:00:00Z", 1, 0)

			convey.So(untilOnlyPage, convey.ShouldResemble, Page[SampleWithData]{})
			convey.So(untilOnlyErr, convey.ShouldNotBeNil)
			convey.So(errors.Is(untilOnlyErr, ErrUpstreamImpaired), convey.ShouldBeTrue)

			untilOnlyURI, err := url.ParseRequestURI(receiveRemoteClientTestValue(t, requestURIs, "until-only request URI"))
			convey.So(err, convey.ShouldBeNil)
			convey.So(untilOnlyURI.Query(), convey.ShouldResemble, url.Values{
				"limit":  {"1"},
				"offset": {"0"},
				"until":  {"2026-06-02T00:00:00Z"},
			})
		})
	})
}

func TestRemoteClientPeoplePageMethodsReadHeadersC3(t *testing.T) {
	cases := []struct {
		name        string
		response    any
		headers     http.Header
		expectedURI string
		expected    any
		call        func(context.Context, *RemoteClient) (any, error)
	}{
		{
			name: "StudiesForFacultySponsorPage",
			response: []PersonStudy{
				{Study: Study{IDStudyLims: "S1", Name: "Alpha"}},
			},
			headers: http.Header{
				"X-Total-Count": {"3"},
				"X-Next-Offset": {"1"},
			},
			expectedURI: "/studies/faculty-sponsor/carl?limit=1&offset=0",
			expected: Page[PersonStudy]{
				Items:      []PersonStudy{{Study: Study{IDStudyLims: "S1", Name: "Alpha"}}},
				Total:      3,
				NextOffset: 1,
			},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.StudiesForFacultySponsorPage(ctx, "carl", 1, 0)
			},
		},
		{
			name: "StudiesForUserPage",
			response: []PersonStudy{
				{Study: Study{IDStudyLims: "S2"}, Role: "follower"},
			},
			headers: http.Header{
				"X-Total-Count": {"1"},
				"X-Next-Offset": {"-1"},
			},
			expectedURI: "/studies/user/ca3?limit=1&offset=0&role=follower",
			expected: Page[PersonStudy]{
				Items:      []PersonStudy{{Study: Study{IDStudyLims: "S2"}, Role: "follower"}},
				Total:      1,
				NextOffset: -1,
			},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.StudiesForUserPage(ctx, "ca3", "follower", 1, 0)
			},
		},
		{
			name: "ResolvePersonPage",
			response: []PersonCandidate{
				{Source: "faculty_sponsor", Name: "Rosa King", StudyCount: 2},
			},
			headers: http.Header{
				"X-Total-Count": {"2"},
				"X-Next-Offset": {"1"},
			},
			expectedURI: "/resolve-person/rosa?limit=1&offset=0",
			expected: Page[PersonCandidate]{
				Items:      []PersonCandidate{{Source: "faculty_sponsor", Name: "Rosa King", StudyCount: 2}},
				Total:      2,
				NextOffset: 1,
			},
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.ResolvePersonPage(ctx, "rosa", 1, 0)
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		convey.Convey("C3: Given "+tc.name+" receives people rows and sizing headers from a stub server", t, func() {
			requestURIs := make(chan string, 1)
			server := newRemoteClientJSONHeaderServerForTest(requestURIs, tc.response, tc.headers)
			defer server.Close()

			client := newRemoteClientForTest(t, server.URL, "")
			defer closeRemoteClientForTest(t, client)

			convey.Convey("when the page method runs, then it returns the decoded rows, headers, and expected filtered URI", func() {
				page, err := tc.call(context.Background(), client)

				convey.So(err, convey.ShouldBeNil)
				convey.So(page, convey.ShouldResemble, tc.expected)
				convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, tc.expectedURI)
			})
		})
	}
}

func TestRemoteClientStudyDetailWithOptionsReadsHeadersD2(t *testing.T) {
	convey.Convey("D2.1: Given a RemoteClient over a server returning non-lean StudyDetail and sizing headers", t, func() {
		requestURIs := make(chan string, 1)
		detail := StudyDetail{
			Study:     Study{IDStudyLims: "S1"},
			Libraries: []LibraryDetail{{Library: Library{LibraryID: "L1"}}},
		}
		server := newRemoteClientJSONHeaderServerForTest(requestURIs, detail, http.Header{
			"X-Total-Count": {"12"},
			"X-Next-Offset": {"5"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when StudyDetailWithOptions runs without lean, then it sends limit/offset and returns the body plus header totals", func() {
			page, err := client.StudyDetailWithOptions(context.Background(), "S1", DetailOptions{Limit: 5, Offset: 0})

			convey.So(err, convey.ShouldBeNil)
			convey.So(page, convey.ShouldResemble, PagedStudyDetail{
				StudyDetail: detail,
				Total:       12,
				NextOffset:  5,
			})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/study/S1/detail?limit=5&offset=0")
		})
	})
}

func TestRemoteClientStudyDetailWithOptionsLeanQueryD2(t *testing.T) {
	convey.Convey("D2.2: Given a RemoteClient over a server returning lean StudyDetail and sizing headers", t, func() {
		requestURIs := make(chan string, 1)
		detail := StudyDetail{
			Study:     Study{IDStudyLims: "S1"},
			SampleIDs: []string{"A"},
			Lean:      true,
		}
		server := newRemoteClientJSONHeaderServerForTest(requestURIs, detail, http.Header{
			"X-Total-Count": {"12"},
			"X-Next-Offset": {"-1"},
		})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when StudyDetailWithOptions runs with lean, then it sends lean=true with limit/offset and returns the lean body plus header totals", func() {
			page, err := client.StudyDetailWithOptions(context.Background(), "S1", DetailOptions{Limit: 5, Offset: 0, Lean: true})

			convey.So(err, convey.ShouldBeNil)
			convey.So(page.StudyDetail.Lean, convey.ShouldBeTrue)
			convey.So(page, convey.ShouldResemble, PagedStudyDetail{
				StudyDetail: detail,
				Total:       12,
				NextOffset:  -1,
			})
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/study/S1/detail?lean=true&limit=5&offset=0")
		})
	})
}

func TestRemoteClientRunDetailWithOptionsQueriesD2(t *testing.T) {
	cases := []struct {
		name        string
		detail      RunDetail
		options     DetailOptions
		expectedURI string
		nextOffset  int
	}{
		{
			name: "non-lean",
			detail: RunDetail{
				Run:     Run{IDRun: 52553},
				Samples: []Sample{{Name: "A"}},
			},
			options:     DetailOptions{Limit: 5, Offset: 0},
			expectedURI: "/run/52553/detail?limit=5&offset=0",
			nextOffset:  5,
		},
		{
			name: "lean",
			detail: RunDetail{
				Run:       Run{IDRun: 52553},
				SampleIDs: []string{"A"},
				StudyIDs:  []string{"S1"},
				Lean:      true,
			},
			options:     DetailOptions{Limit: 5, Offset: 0, Lean: true},
			expectedURI: "/run/52553/detail?lean=true&limit=5&offset=0",
			nextOffset:  -1,
		},
	}

	for _, tc := range cases {
		tc := tc
		convey.Convey("D2.3: Given a RemoteClient over a server returning "+tc.name+" RunDetail and sizing headers", t, func() {
			requestURIs := make(chan string, 1)
			server := newRemoteClientJSONHeaderServerForTest(requestURIs, tc.detail, http.Header{
				"X-Total-Count": {"12"},
				"X-Next-Offset": {strconv.Itoa(tc.nextOffset)},
			})
			defer server.Close()

			client := newRemoteClientForTest(t, server.URL, "")
			defer closeRemoteClientForTest(t, client)

			convey.Convey("when RunDetailWithOptions runs, then it sends the expected query and returns the body plus header totals", func() {
				page, err := client.RunDetailWithOptions(context.Background(), "52553", tc.options)

				convey.So(err, convey.ShouldBeNil)
				convey.So(page, convey.ShouldResemble, PagedRunDetail{
					RunDetail:  tc.detail,
					Total:      12,
					NextOffset: tc.nextOffset,
				})
				convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, tc.expectedURI)
			})
		})
	}
}

func TestRemoteClientDetailBodyOnlyMethodsKeepURIsD2(t *testing.T) {
	cases := []struct {
		name        string
		response    any
		expectedURI string
		call        func(context.Context, *RemoteClient) (any, error)
	}{
		{
			name:        "StudyDetail",
			response:    StudyDetail{Study: Study{IDStudyLims: "S1"}},
			expectedURI: "/study/S1/detail",
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.StudyDetail(ctx, "S1")
			},
		},
		{
			name:        "RunDetail",
			response:    RunDetail{Run: Run{IDRun: 52553}},
			expectedURI: "/run/52553/detail",
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.RunDetail(ctx, "52553")
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		convey.Convey("D2.4: Given a RemoteClient over a server returning "+tc.name, t, func() {
			requestURIs := make(chan string, 1)
			server := newRemoteClientJSONServerForTest(requestURIs, tc.response)
			defer server.Close()

			client := newRemoteClientForTest(t, server.URL, "")
			defer closeRemoteClientForTest(t, client)

			convey.Convey("when the existing body-only method runs, then it sends no query params and returns the unchanged body type", func() {
				result, err := tc.call(context.Background(), client)

				convey.So(err, convey.ShouldBeNil)
				convey.So(result, convey.ShouldResemble, tc.response)
				convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, tc.expectedURI)
			})
		})
	}
}

func TestRemoteClientDetailWithOptionsUsesHeaderFallbacksD2(t *testing.T) {
	convey.Convey("D2.5: Given RemoteClients over servers returning detail bodies without sizing headers", t, func() {
		studyURIs := make(chan string, 1)
		studyDetail := StudyDetail{Study: Study{IDStudyLims: "S1"}}
		studyServer := newRemoteClientJSONHeaderServerForTest(studyURIs, studyDetail, nil)
		defer studyServer.Close()

		runURIs := make(chan string, 1)
		runDetail := RunDetail{Run: Run{IDRun: 52553}}
		runServer := newRemoteClientJSONHeaderServerForTest(runURIs, runDetail, nil)
		defer runServer.Close()

		studyClient := newRemoteClientForTest(t, studyServer.URL, "")
		defer closeRemoteClientForTest(t, studyClient)
		runClient := newRemoteClientForTest(t, runServer.URL, "")
		defer closeRemoteClientForTest(t, runClient)

		convey.Convey("when the options methods run, then the bodies decode and missing headers fall back to Total 0 and NextOffset -1", func() {
			studyPage, studyErr := studyClient.StudyDetailWithOptions(context.Background(), "S1", DetailOptions{Limit: 5, Offset: 0})
			runPage, runErr := runClient.RunDetailWithOptions(context.Background(), "52553", DetailOptions{Limit: 5, Offset: 0})

			convey.So(studyErr, convey.ShouldBeNil)
			convey.So(studyPage, convey.ShouldResemble, PagedStudyDetail{
				StudyDetail: studyDetail,
				Total:       0,
				NextOffset:  -1,
			})
			convey.So(receiveRemoteClientTestValue(t, studyURIs, "study request URI"), convey.ShouldEqual, "/study/S1/detail?limit=5&offset=0")

			convey.So(runErr, convey.ShouldBeNil)
			convey.So(runPage, convey.ShouldResemble, PagedRunDetail{
				RunDetail:  runDetail,
				Total:      0,
				NextOffset: -1,
			})
			convey.So(receiveRemoteClientTestValue(t, runURIs, "run request URI"), convey.ShouldEqual, "/run/52553/detail?limit=5&offset=0")
		})
	})
}

func newRemoteClientJSONHeaderServerForTest[T any](requestURIs chan<- string, result T, headers http.Header) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURIs <- r.URL.RequestURI()
		for name, values := range headers {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		writeRemoteClientJSONForTest(w, result)
	}))
}

func TestRemoteClientCallWithHeadersErrorsMatchCallA1(t *testing.T) {
	convey.Convey("A1.3: Given a RemoteClient", t, func() {
		server := newRemoteClientJSONServerForTest(make(chan string, 1), Match{})
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when CallWithHeaders is given an unknown Registry method, then it returns nil result and the existing sentinel error", func() {
			result, _, err := client.CallWithHeaders(context.Background(), "NoSuchMethod", nil, nil)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
			convey.So(result, convey.ShouldBeNil)
		})

		convey.Convey("when CallWithHeaders is given too few path params, then it returns nil result and the existing sentinel error", func() {
			result, _, err := client.CallWithHeaders(context.Background(), "ResolveSample", nil, nil)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
			convey.So(result, convey.ShouldBeNil)
		})
	})

	convey.Convey("A1.3: Given a remote server returning a not_found envelope", t, func() {
		server := newRemoteClientErrorServerForTest(http.StatusNotFound, "not_found")
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when CallWithHeaders receives the envelope, then it returns nil result and preserves ErrNotFound", func() {
			result, _, err := client.CallWithHeaders(context.Background(), "ResolveSample", []string{"missing"}, nil)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(result, convey.ShouldBeNil)
		})
	})
}

func TestRemoteClientCallWithHeadersSearchUsesServerDefaultsB1(t *testing.T) {
	convey.Convey("B1.6: Given a real MLWH server over a fake search queryer", t, func() {
		requestURIs := make(chan string, 1)
		queryer := &serverFakeQueryer{
			searchStudiesFunc: func(_ context.Context, term string, _, _ int) ([]Study, error) {
				return []Study{{IDStudyLims: "1", Name: term + "-study"}}, nil
			},
			countStudySearchFunc: func(_ context.Context, _ string) (Count, error) {
				return Count{Count: 7}, nil
			},
		}
		server := newRecordingMLWHServerForRemoteTest(queryer, requestURIs)
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when CallWithHeaders omits SearchStudies query params, then the server applies search defaults and exposes headers", func() {
			result, headers, err := client.CallWithHeaders(context.Background(), "SearchStudies", []string{"malar"}, nil)

			convey.So(err, convey.ShouldBeNil)
			studies, ok := result.(*[]Study)
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(*studies, convey.ShouldResemble, []Study{{IDStudyLims: "1", Name: "malar-study"}})
			convey.So(headers.Get("X-Total-Count"), convey.ShouldEqual, "7")
			convey.So(headers.Get("X-Next-Offset"), convey.ShouldEqual, "1")
			convey.So(queryer.searchCall.term, convey.ShouldEqual, "malar")
			convey.So(queryer.searchCall.limit, convey.ShouldEqual, 100)
			convey.So(queryer.searchCall.offset, convey.ShouldEqual, 0)
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "request URI"), convey.ShouldEqual, "/search/study/malar")
		})
	})
}

func TestRemoteClientSearchPageMethodsReturnBadRequestForTooLargeLimitB1(t *testing.T) {
	convey.Convey("B1.7: Given a real MLWH server whose fake search queryer panics if called", t, func() {
		requestURIs := make(chan string, 2)
		queryer := &serverFakeQueryer{}
		server := newRecordingMLWHServerForRemoteTest(queryer, requestURIs)
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when SearchStudiesPage and SearchSamplesPage exceed SearchMaxLimit, then each request returns the existing remote bad_request handling", func() {
			studyPage, studyErr := client.SearchStudiesPage(context.Background(), "malar", SearchMaxLimit+1, 0)
			samplePage, sampleErr := client.SearchSamplesPage(context.Background(), "acme", SearchMaxLimit+1, 0)

			convey.So(studyPage, convey.ShouldResemble, Page[Study]{})
			convey.So(studyErr, convey.ShouldNotBeNil)
			convey.So(errors.Is(studyErr, ErrUpstreamImpaired), convey.ShouldBeTrue)
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "study request URI"), convey.ShouldEqual, "/search/study/malar?limit=1001&offset=0")

			convey.So(samplePage, convey.ShouldResemble, Page[Sample]{})
			convey.So(sampleErr, convey.ShouldNotBeNil)
			convey.So(errors.Is(sampleErr, ErrUpstreamImpaired), convey.ShouldBeTrue)
			convey.So(receiveRemoteClientTestValue(t, requestURIs, "sample request URI"), convey.ShouldEqual, "/search/sample/acme?limit=1001&offset=0")
		})
	})
}

func newRecordingMLWHServerForRemoteTest(queryer Queryer, requestURIs chan<- string) *httptest.Server {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewServer(queryer).RegisterRoutes(router, nil)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURIs <- r.URL.RequestURI()
		router.ServeHTTP(w, r)
	}))
}

func TestRemoteClientPeoplePageMethodsMatchBodyOnlyC3(t *testing.T) {
	convey.Convey("C3: Given a RemoteClient over a Client whose cache has the Carl people fixture", t, func() {
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

func TestRemoteClientPeoplePageMethodsPreserveSentinelsC3(t *testing.T) {
	cases := []struct {
		name      string
		emptyPage any
		emptyBare any
		page      func(context.Context, *RemoteClient) (any, error)
		bare      func(context.Context, *RemoteClient) (any, error)
	}{
		{
			name:      "StudiesForFacultySponsorPage",
			emptyPage: Page[PersonStudy]{},
			emptyBare: []PersonStudy(nil),
			page: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.StudiesForFacultySponsorPage(ctx, "carl", 1, 0)
			},
			bare: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.StudiesForFacultySponsor(ctx, "carl", 1, 0)
			},
		},
		{
			name:      "StudiesForUserPage",
			emptyPage: Page[PersonStudy]{},
			emptyBare: []PersonStudy(nil),
			page: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.StudiesForUserPage(ctx, "ca3", "follower", 1, 0)
			},
			bare: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.StudiesForUser(ctx, "ca3", "follower", 1, 0)
			},
		},
		{
			name:      "ResolvePersonPage",
			emptyPage: Page[PersonCandidate]{},
			emptyBare: []PersonCandidate(nil),
			page: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.ResolvePersonPage(ctx, "rosa", 1, 0)
			},
			bare: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.ResolvePerson(ctx, "rosa", 1, 0)
			},
		},
	}

	envelopes := []struct {
		name   string
		status int
		code   string
		check  func(error)
	}{
		{
			name:   "bad_request",
			status: http.StatusBadRequest,
			code:   "bad_request",
			check: func(err error) {
				convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
				convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
				convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeFalse)
			},
		},
		{
			name:   "cache_never_synced",
			status: http.StatusServiceUnavailable,
			code:   "cache_never_synced",
			check: func(err error) {
				convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
				convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		for _, envelope := range envelopes {
			envelope := envelope
			convey.Convey("C3: Given "+tc.name+" receives a remote "+envelope.name+" envelope", t, func() {
				server := newRemoteClientErrorServerForTest(envelope.status, envelope.code)
				defer server.Close()

				client := newRemoteClientForTest(t, server.URL, "")
				defer closeRemoteClientForTest(t, client)

				convey.Convey("when the page method runs, then it returns an empty Page and the same sentinel behavior as the body-only method", func() {
					page, pageErr := tc.page(context.Background(), client)
					bare, bareErr := tc.bare(context.Background(), client)

					convey.So(page, convey.ShouldResemble, tc.emptyPage)
					convey.So(bare, convey.ShouldResemble, tc.emptyBare)
					convey.So(pageErr, convey.ShouldNotBeNil)
					convey.So(bareErr, convey.ShouldNotBeNil)
					envelope.check(pageErr)
					envelope.check(bareErr)
				})
			})
		}
	}
}

func TestRemoteClientDetailWithOptionsErrorsD2(t *testing.T) {
	cases := []struct {
		name        string
		empty       any
		expectedURI string
		call        func(context.Context, *RemoteClient) (any, error)
	}{
		{
			name:        "StudyDetailWithOptions",
			empty:       PagedStudyDetail{},
			expectedURI: "/study/S1/detail?limit=-1&offset=0",
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.StudyDetailWithOptions(ctx, "S1", DetailOptions{Limit: -1, Offset: 0})
			},
		},
		{
			name:        "RunDetailWithOptions",
			empty:       PagedRunDetail{},
			expectedURI: "/run/52553/detail?limit=5&offset=-1",
			call: func(ctx context.Context, client *RemoteClient) (any, error) {
				return client.RunDetailWithOptions(ctx, "52553", DetailOptions{Limit: 5, Offset: -1})
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		convey.Convey("D2.6: Given "+tc.name+" receives an upstream bad_request envelope with sizing headers", t, func() {
			requestURIs := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestURIs <- r.URL.RequestURI()
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Total-Count", "12")
				w.Header().Set("X-Next-Offset", "5")
				w.WriteHeader(http.StatusBadRequest)
				writeRemoteClientJSONForTest(w, map[string]string{
					"code":    "bad_request",
					"message": "server returned bad_request",
				})
			}))
			defer server.Close()

			client := newRemoteClientForTest(t, server.URL, "")
			defer closeRemoteClientForTest(t, client)

			convey.Convey("when the options method runs, then it returns the existing remote error and no fabricated header metadata", func() {
				page, err := tc.call(context.Background(), client)

				convey.So(page, convey.ShouldResemble, tc.empty)
				convey.So(err, convey.ShouldNotBeNil)
				convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
				convey.So(receiveRemoteClientTestValue(t, requestURIs, tc.name+" request URI"), convey.ShouldEqual, tc.expectedURI)
			})
		})
	}
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
