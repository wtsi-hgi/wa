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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

func TestServerHandlersDoNotOwnCachesB2(t *testing.T) {
	convey.Convey("B2.8: Given server handler source, when audited, then handlers do not own cache or map state", t, func() {
		source := readPackageFile(t, "server.go")
		lowerSource := strings.ToLower(source)

		convey.So(source, convey.ShouldContainSubstring, "queryer Queryer")
		convey.So(source, convey.ShouldContainSubstring, "func mlwhEndpointHandler")
		convey.So(lowerSource, convey.ShouldNotContainSubstring, "cache")
		convey.So(lowerSource, convey.ShouldNotContainSubstring, "map[")
		convey.So(lowerSource, convey.ShouldNotContainSubstring, "mutex")
		convey.So(lowerSource, convey.ShouldNotContainSubstring, "sync.")
	})
}

type serverFakeQueryer struct {
	classifyIdentifierFunc func(context.Context, string) (Match, error)
	resolveStudyFunc       func(context.Context, string) (Match, error)
	samplesForStudyFunc    func(context.Context, string, int, int) ([]Sample, error)
	enrichFunc             func(context.Context, string) (EnrichmentResult, error)

	samplesForStudyCall struct {
		studyLimsID string
		limit       int
		offset      int
	}
}

func (q *serverFakeQueryer) ClassifyIdentifier(ctx context.Context, raw string) (Match, error) {
	return q.classifyIdentifierFunc(ctx, raw)
}

func (q *serverFakeQueryer) ResolveSample(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveSample call")
}

func (q *serverFakeQueryer) ResolveSampleName(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveSampleName call")
}

func (q *serverFakeQueryer) ResolveStudy(ctx context.Context, raw string) (Match, error) {
	return q.resolveStudyFunc(ctx, raw)
}

func (q *serverFakeQueryer) ResolveRun(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveRun call")
}

func (q *serverFakeQueryer) ResolveLibrary(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveLibrary call")
}

func (q *serverFakeQueryer) ResolveLibraryIdentifier(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveLibraryIdentifier call")
}

func (q *serverFakeQueryer) AllStudies(_ context.Context, _ int, _ int) ([]Study, error) {
	panic("unexpected AllStudies call")
}

func (q *serverFakeQueryer) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Sample, error) {
	q.samplesForStudyCall.studyLimsID = studyLimsID
	q.samplesForStudyCall.limit = limit
	q.samplesForStudyCall.offset = offset

	return q.samplesForStudyFunc(ctx, studyLimsID, limit, offset)
}

func (q *serverFakeQueryer) SamplesForRun(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForRun call")
}

func (q *serverFakeQueryer) SamplesForLibrary(_ context.Context, _ string, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForLibrary call")
}

func (q *serverFakeQueryer) SamplesForLibraryID(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForLibraryID call")
}

func (q *serverFakeQueryer) SamplesForLibraryLimsID(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForLibraryLimsID call")
}

func (q *serverFakeQueryer) SamplesForLibraryType(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForLibraryType call")
}

func (q *serverFakeQueryer) LibrariesForStudy(_ context.Context, _ string, _ int, _ int) ([]Library, error) {
	panic("unexpected LibrariesForStudy call")
}

func (q *serverFakeQueryer) RunsForStudy(_ context.Context, _ string, _ int, _ int) ([]Run, error) {
	panic("unexpected RunsForStudy call")
}

func (q *serverFakeQueryer) LanesForSample(_ context.Context, _ string, _ int, _ int) ([]Lane, error) {
	panic("unexpected LanesForSample call")
}

func (q *serverFakeQueryer) IRODSPathsForSample(_ context.Context, _ string, _ int, _ int) ([]IRODSPath, error) {
	panic("unexpected IRODSPathsForSample call")
}

func (q *serverFakeQueryer) IRODSPathsForStudy(_ context.Context, _ string, _ int, _ int) ([]IRODSPath, error) {
	panic("unexpected IRODSPathsForStudy call")
}

func (q *serverFakeQueryer) StudiesForSample(_ context.Context, _ string) ([]Study, error) {
	panic("unexpected StudiesForSample call")
}

func (q *serverFakeQueryer) FindSamplesBySangerID(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesBySangerID call")
}

func (q *serverFakeQueryer) FindSamplesByIDSampleLims(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesByIDSampleLims call")
}

func (q *serverFakeQueryer) FindSamplesByAccessionNumber(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesByAccessionNumber call")
}

func (q *serverFakeQueryer) FindSamplesBySupplierName(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesBySupplierName call")
}

func (q *serverFakeQueryer) FindSamplesByLibraryType(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesByLibraryType call")
}

func (q *serverFakeQueryer) ExpandIdentifier(_ context.Context, _ IdentifierKind, _ string) ([]TaggedID, error) {
	panic("unexpected ExpandIdentifier call")
}

func (q *serverFakeQueryer) ExpandSearchValues(_ context.Context, _ IdentifierKind, _ string) (SearchValues, error) {
	panic("unexpected ExpandSearchValues call")
}

func (q *serverFakeQueryer) ExpandSampleSearchValues(_ context.Context, _ IdentifierKind, _ string) ([]string, error) {
	panic("unexpected ExpandSampleSearchValues call")
}

func (q *serverFakeQueryer) Enrich(ctx context.Context, identifier string) (EnrichmentResult, error) {
	return q.enrichFunc(ctx, identifier)
}

func (q *serverFakeQueryer) SampleDetail(_ context.Context, _ string) (SampleDetail, error) {
	panic("unexpected SampleDetail call")
}

func (q *serverFakeQueryer) StudyDetail(_ context.Context, _ string) (StudyDetail, error) {
	panic("unexpected StudyDetail call")
}

func (q *serverFakeQueryer) RunDetail(_ context.Context, _ string) (RunDetail, error) {
	panic("unexpected RunDetail call")
}

func (q *serverFakeQueryer) LibraryDetail(_ context.Context, _ string, _ string) (LibraryDetail, error) {
	panic("unexpected LibraryDetail call")
}

func TestServerSamplesForStudyB2(t *testing.T) {
	convey.Convey("B2.1: Given a server over a fake Queryer, when GET /study/6568/samples is served, then status is 200 and body is a 2-element Sample array", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, studyLimsID string, limit, offset int) ([]Sample, error) {
				return []Sample{
					{IDSampleTmp: 1, Name: studyLimsID + "-A"},
					{IDSampleTmp: 2, Name: studyLimsID + "-B"},
				}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.samplesForStudyCall.studyLimsID, convey.ShouldEqual, "6568")
		convey.So(queryer.samplesForStudyCall.limit, convey.ShouldEqual, mlwhServerFetchAllLimit)
		convey.So(queryer.samplesForStudyCall.offset, convey.ShouldEqual, 0)

		var samples []Sample
		decodeMLWHJSONResponseForTest(t, response, &samples)
		convey.So(samples, convey.ShouldResemble, []Sample{
			{IDSampleTmp: 1, Name: "6568-A"},
			{IDSampleTmp: 2, Name: "6568-B"},
		})
	})

	convey.Convey("B2.2: Given limit and offset query params, when GET /study/6568/samples is served, then the queryer receives those values", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
				return []Sample{}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples?limit=2&offset=1")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.samplesForStudyCall.limit, convey.ShouldEqual, 2)
		convey.So(queryer.samplesForStudyCall.offset, convey.ShouldEqual, 1)
	})
}

func performMLWHRequestForTest(t *testing.T, queryer Queryer, method, path string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewServer(queryer).RegisterRoutes(router, nil)

	request := httptest.NewRequest(method, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return response
}

func decodeMLWHJSONResponseForTest(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.NewDecoder(bytes.NewReader(response.Body.Bytes())).Decode(target); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}

func TestServerErrorEnvelopeB2(t *testing.T) {
	convey.Convey("B2.3: Given the queryer returns ErrNotFound, when GET /study/6568/samples is served, then status is 404 and code is not_found", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
				return nil, ErrNotFound
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusNotFound, "not_found")
	})

	convey.Convey("B2.4: Given the queryer returns ErrCacheNeverSynced, when GET /study/6568/samples is served, then status is 503 and code is cache_never_synced", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
				return nil, ErrCacheNeverSynced
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusServiceUnavailable, "cache_never_synced")
	})

	convey.Convey("B2.5: Given ResolveStudy returns ErrAmbiguous, when GET /resolve/study/x is served, then status is 409 and code is ambiguous", t, func() {
		queryer := &serverFakeQueryer{
			resolveStudyFunc: func(_ context.Context, _ string) (Match, error) {
				return Match{}, ErrAmbiguous
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/resolve/study/x")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusConflict, "ambiguous")
	})

	convey.Convey("B2.6: Given ClassifyIdentifier returns ErrUnsupportedIdentifier, when GET /classify/SQSCP is served, then status is 422 and code is unsupported_identifier", t, func() {
		queryer := &serverFakeQueryer{
			classifyIdentifierFunc: func(_ context.Context, _ string) (Match, error) {
				return Match{}, ErrUnsupportedIdentifier
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/classify/SQSCP")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusUnprocessableEntity, "unsupported_identifier")
	})
}

func assertMLWHErrorEnvelopeForTest(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	convey.So(response.Code, convey.ShouldEqual, status)

	var payload httpErrorEnvelope
	decodeMLWHJSONResponseForTest(t, response, &payload)
	convey.So(payload.Code, convey.ShouldEqual, code)
	convey.So(payload.Message, convey.ShouldNotBeBlank)
}

func TestServerEnrichB2(t *testing.T) {
	convey.Convey("B2.7: Given GET /enrich/6568 and an EnrichmentResult, then status is 200 and graph has no project or users keys", t, func() {
		queryer := &serverFakeQueryer{
			enrichFunc: func(_ context.Context, identifier string) (EnrichmentResult, error) {
				return EnrichmentResult{
					Identifier: identifier,
					Type:       KindStudyLimsID,
					Graph: EnrichmentGraph{
						Study: &Study{IDStudyLims: identifier},
					},
					Partial: false,
				}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/enrich/6568")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var payload map[string]any
		decodeMLWHJSONResponseForTest(t, response, &payload)
		graph, ok := payload["graph"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(payload["partial"], convey.ShouldEqual, false)
		convey.So(graph, convey.ShouldContainKey, "study")
		convey.So(graph, convey.ShouldNotContainKey, "project")
		convey.So(graph, convey.ShouldNotContainKey, "users")
	})
}
