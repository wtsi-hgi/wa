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

package seqmeta

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
)

func TestServerEnrichEndpoint(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	study := &saga.Study{IDStudyLims: "6568", Name: "Study 6568"}
	samples := []saga.MLWHSample{
		{SangerID: "S1", IDStudyLims: "6568", LibraryType: "RNA PolyA"},
		{SangerID: "S2", IDStudyLims: "6568", LibraryType: "RNA PolyA"},
		{SangerID: "S3", IDStudyLims: "6568", LibraryType: "PCR free"},
	}
	received := ""

	provider := &MockProvider{
		GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
			received = identifier
			if identifier == "6568" || identifier == "foo/bar" {
				return &saga.Study{IDStudyLims: identifier}, nil
			}

			return nil, saga.ErrNotFound
		},
		AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
			return []saga.Study{}, nil
		},
		AllSamplesForStudyFunc: func(_ context.Context, studyID string) ([]saga.MLWHSample, error) {
			if studyID == "6568" {
				return samples, nil
			}

			return []saga.MLWHSample{}, nil
		},
		AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
			return []saga.MLWHSample{}, nil
		},
		ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
			return []saga.Project{}, nil
		},
	}
	server := NewServer(provider, store)

	convey.Convey("E1: enrich endpoint returns a study graph envelope", t, func() {
		provider.GetStudyFunc = func(_ context.Context, identifier string) (*saga.Study, error) {
			received = identifier
			if identifier == "6568" {
				return study, nil
			}

			if identifier == "foo/bar" {
				return &saga.Study{IDStudyLims: identifier}, nil
			}

			return nil, saga.ErrNotFound
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(recorder.Header().Get("Content-Type"), convey.ShouldContainSubstring, "application/json")
		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(result.Graph.Study, convey.ShouldNotBeNil)
		if result.Graph.Study == nil {
			return
		}

		convey.So(result.Graph.Study.IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 3)
		convey.So(result.Graph.Libraries, convey.ShouldHaveLength, 2)
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeNil)
	})

	convey.Convey("E1: enrich endpoint passes URL-decoded identifiers to Enrich", t, func() {
		request := httptest.NewRequest(http.MethodGet, "/enrich/foo%2Fbar", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(received, convey.ShouldEqual, "foo/bar")
	})

	convey.Convey("E2: enrich endpoint returns partial results when a non-classification hop fails", t, func() {
		convey.So(store.DeleteEnrichCache("6568"), convey.ShouldBeNil)

		provider.GetStudyFunc = func(_ context.Context, identifier string) (*saga.Study, error) {
			convey.So(identifier, convey.ShouldEqual, "6568")

			return study, nil
		}
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			return []saga.Study{}, nil
		}
		provider.AllSamplesForStudyFunc = func(_ context.Context, studyID string) ([]saga.MLWHSample, error) {
			convey.So(studyID, convey.ShouldEqual, "6568")

			return nil, saga.ErrServerError
		}
		provider.FindSamplesBySangerIDFn = nil
		provider.FindSamplesByIDSampleLimsFn = nil
		provider.FindSamplesByAccessionNumberFn = nil
		provider.FindSamplesByRunIDFn = nil
		provider.FindSamplesByLibraryTypeFn = nil
		provider.ListProjectsFunc = func(_ context.Context) ([]saga.Project, error) {
			return []saga.Project{}, nil
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{{
			Hop:    HopSamples,
			Reason: ReasonUpstreamError,
			Status: http.StatusBadGateway,
		}})
	})

	convey.Convey("E2: enrich endpoint returns 502 with missing probes when every classification hop fails upstream", t, func() {
		convey.So(store.DeleteEnrichCache("xyz"), convey.ShouldBeNil)

		provider.GetStudyFunc = func(_ context.Context, identifier string) (*saga.Study, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return nil, saga.ErrServerError
		}
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			return nil, saga.ErrServerError
		}
		provider.FindSamplesBySangerIDFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return nil, saga.ErrServerError
		}
		provider.FindSamplesByIDSampleLimsFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return nil, saga.ErrServerError
		}
		provider.FindSamplesByAccessionNumberFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return nil, saga.ErrServerError
		}
		provider.FindSamplesByLibraryTypeFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return nil, saga.ErrServerError
		}
		provider.ListProjectsFunc = func(_ context.Context) ([]saga.Project, error) {
			return nil, saga.ErrServerError
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/xyz", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result struct {
			Error   string       `json:"error"`
			Missing []MissingHop `json:"missing"`
		}
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusBadGateway)
		convey.So(result.Error, convey.ShouldEqual, ErrAllHopsFailed.Error())
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{
			{Hop: HopClassify, Reason: ReasonUpstreamError, Status: http.StatusBadGateway},
			{Hop: HopClassify, Reason: ReasonUpstreamError, Status: http.StatusBadGateway},
			{Hop: HopClassify, Reason: ReasonUpstreamError, Status: http.StatusBadGateway},
			{Hop: HopClassify, Reason: ReasonUpstreamError, Status: http.StatusBadGateway},
			{Hop: HopClassify, Reason: ReasonUpstreamError, Status: http.StatusBadGateway},
			{Hop: HopClassify, Reason: ReasonUpstreamError, Status: http.StatusBadGateway},
			{Hop: HopClassify, Reason: ReasonUpstreamError, Status: http.StatusBadGateway},
		})
	})

	convey.Convey("E2: enrich endpoint returns 404 when every classification hop is empty or not found", t, func() {
		convey.So(store.DeleteEnrichCache("xyz"), convey.ShouldBeNil)

		provider.GetStudyFunc = func(_ context.Context, identifier string) (*saga.Study, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return nil, saga.ErrNotFound
		}
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			return []saga.Study{}, nil
		}
		provider.FindSamplesBySangerIDFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return []saga.MLWHSample{}, nil
		}
		provider.FindSamplesByIDSampleLimsFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return []saga.MLWHSample{}, nil
		}
		provider.FindSamplesByAccessionNumberFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return []saga.MLWHSample{}, nil
		}
		provider.FindSamplesByLibraryTypeFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return []saga.MLWHSample{}, nil
		}
		provider.ListProjectsFunc = func(_ context.Context) ([]saga.Project, error) {
			return []saga.Project{}, nil
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/xyz", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result map[string]any
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(result["error"], convey.ShouldEqual, ErrUnknownIdentifier.Error())
		_, hasMissing := result["missing"]
		convey.So(hasMissing, convey.ShouldBeFalse)
	})

	convey.Convey("E3: enrich endpoint serves the second request from cache within the success TTL", t, func() {
		getStudyCalls := 0
		convey.So(store.DeleteEnrichCache("6568"), convey.ShouldBeNil)

		provider.GetStudyFunc = func(_ context.Context, identifier string) (*saga.Study, error) {
			getStudyCalls++
			convey.So(identifier, convey.ShouldEqual, "6568")

			return study, nil
		}
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			return []saga.Study{}, nil
		}
		provider.AllSamplesForStudyFunc = func(_ context.Context, studyID string) ([]saga.MLWHSample, error) {
			convey.So(studyID, convey.ShouldEqual, "6568")

			return samples, nil
		}
		provider.FindSamplesBySangerIDFn = nil
		provider.FindSamplesByIDSampleLimsFn = nil
		provider.FindSamplesByAccessionNumberFn = nil
		provider.FindSamplesByRunIDFn = nil
		provider.FindSamplesByLibraryTypeFn = nil
		provider.ListProjectsFunc = func(_ context.Context) ([]saga.Project, error) {
			return []saga.Project{}, nil
		}

		server := NewServer(provider, store, WithEnrichTTL(time.Hour, 10*time.Minute))
		request := httptest.NewRequest(http.MethodGet, "/enrich/6568", nil)

		firstRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(firstRecorder, request)
		secondRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(secondRecorder, request)

		convey.So(firstRecorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(secondRecorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(getStudyCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("E3: enrich endpoint re-fetches after the success TTL expires", t, func() {
		getStudyCalls := 0
		convey.So(store.DeleteEnrichCache("6568"), convey.ShouldBeNil)

		provider.GetStudyFunc = func(_ context.Context, identifier string) (*saga.Study, error) {
			getStudyCalls++
			convey.So(identifier, convey.ShouldEqual, "6568")

			return study, nil
		}
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			return []saga.Study{}, nil
		}
		provider.AllSamplesForStudyFunc = func(_ context.Context, studyID string) ([]saga.MLWHSample, error) {
			convey.So(studyID, convey.ShouldEqual, "6568")

			return samples, nil
		}
		provider.FindSamplesBySangerIDFn = nil
		provider.FindSamplesByIDSampleLimsFn = nil
		provider.FindSamplesByAccessionNumberFn = nil
		provider.FindSamplesByRunIDFn = nil
		provider.FindSamplesByLibraryTypeFn = nil
		provider.ListProjectsFunc = func(_ context.Context) ([]saga.Project, error) {
			return []saga.Project{}, nil
		}

		server := NewServer(provider, store, WithEnrichTTL(10*time.Millisecond, time.Minute))
		request := httptest.NewRequest(http.MethodGet, "/enrich/6568", nil)

		firstRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(firstRecorder, request)
		time.Sleep(20 * time.Millisecond)
		secondRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(secondRecorder, request)

		convey.So(firstRecorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(secondRecorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(getStudyCalls, convey.ShouldEqual, 2)
	})

	convey.Convey("E3: enrich endpoint stores partial results with the negative TTL", t, func() {
		convey.So(store.DeleteEnrichCache("6568"), convey.ShouldBeNil)

		provider.GetStudyFunc = func(_ context.Context, identifier string) (*saga.Study, error) {
			convey.So(identifier, convey.ShouldEqual, "6568")

			return study, nil
		}
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			return []saga.Study{}, nil
		}
		provider.AllSamplesForStudyFunc = func(_ context.Context, studyID string) ([]saga.MLWHSample, error) {
			convey.So(studyID, convey.ShouldEqual, "6568")

			return nil, saga.ErrServerError
		}
		provider.FindSamplesBySangerIDFn = nil
		provider.FindSamplesByIDSampleLimsFn = nil
		provider.FindSamplesByAccessionNumberFn = nil
		provider.FindSamplesByRunIDFn = nil
		provider.FindSamplesByLibraryTypeFn = nil
		provider.ListProjectsFunc = func(_ context.Context) ([]saga.Project, error) {
			return []saga.Project{}, nil
		}

		server := NewServer(provider, store, WithEnrichTTL(time.Hour, 10*time.Minute))
		request := httptest.NewRequest(http.MethodGet, "/enrich/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		loaded, err := store.LoadEnrichCache("6568")

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(err, convey.ShouldBeNil)
		convey.So(loaded, convey.ShouldNotBeNil)
		if loaded == nil {
			return
		}

		convey.So(loaded.TTL, convey.ShouldEqual, 10*time.Minute)
		convey.So(loaded.Partial, convey.ShouldBeTrue)
		convey.So(loaded.Negative, convey.ShouldBeFalse)
	})

	convey.Convey("E3: enrich endpoint reuses a cached negative result within the negative TTL", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		findSamplesBySangerIDCalls := 0
		findSamplesByIDSampleLimsCalls := 0
		findSamplesByAccessionNumberCalls := 0
		findSamplesByLibraryTypeCalls := 0
		listProjectsCalls := 0
		convey.So(store.DeleteEnrichCache("xyz"), convey.ShouldBeNil)

		provider.GetStudyFunc = func(_ context.Context, identifier string) (*saga.Study, error) {
			getStudyCalls++
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return nil, saga.ErrNotFound
		}
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			allStudiesCalls++

			return []saga.Study{}, nil
		}
		provider.AllSamplesForStudyFunc = nil
		provider.FindSamplesBySangerIDFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			findSamplesBySangerIDCalls++
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return []saga.MLWHSample{}, nil
		}
		provider.FindSamplesByIDSampleLimsFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			findSamplesByIDSampleLimsCalls++
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return []saga.MLWHSample{}, nil
		}
		provider.FindSamplesByAccessionNumberFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			findSamplesByAccessionNumberCalls++
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return []saga.MLWHSample{}, nil
		}
		provider.FindSamplesByRunIDFn = nil
		provider.FindSamplesByLibraryTypeFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			findSamplesByLibraryTypeCalls++
			convey.So(identifier, convey.ShouldEqual, "xyz")

			return []saga.MLWHSample{}, nil
		}
		provider.ListProjectsFunc = func(_ context.Context) ([]saga.Project, error) {
			listProjectsCalls++

			return []saga.Project{}, nil
		}

		server := NewServer(provider, store, WithEnrichTTL(time.Hour, 10*time.Minute))
		request := httptest.NewRequest(http.MethodGet, "/enrich/xyz", nil)

		firstRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(firstRecorder, request)
		secondRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(secondRecorder, request)

		var loadedBody map[string]string
		loaded, err := store.LoadEnrichCache("xyz")

		convey.So(firstRecorder.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(secondRecorder.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(err, convey.ShouldBeNil)
		convey.So(loaded, convey.ShouldNotBeNil)
		if loaded == nil {
			return
		}

		convey.So(loaded.Negative, convey.ShouldBeTrue)
		convey.So(loaded.TTL, convey.ShouldEqual, 10*time.Minute)
		convey.So(json.Unmarshal(loaded.Body, &loadedBody), convey.ShouldBeNil)
		convey.So(loadedBody, convey.ShouldResemble, map[string]string{"error": ErrUnknownIdentifier.Error()})
		convey.So(getStudyCalls, convey.ShouldEqual, 1)
		convey.So(allStudiesCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByIDSampleLimsCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByAccessionNumberCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByLibraryTypeCalls, convey.ShouldEqual, 1)
		convey.So(listProjectsCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("E4: delete enrich endpoint removes an existing cache entry", t, func() {
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "6568",
			Type:       IdentifierStudyID,
			Body:       []byte(`{"identifier":"6568"}`),
			FetchedAt:  time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC),
			TTL:        time.Hour,
		}), convey.ShouldBeNil)

		request := httptest.NewRequest(http.MethodDelete, "/enrich/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var body map[string]string
		loaded, err := store.LoadEnrichCache("6568")

		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(body, convey.ShouldResemble, map[string]string{"identifier": "6568"})
		convey.So(loaded, convey.ShouldBeNil)
		convey.So(err, convey.ShouldEqual, sql.ErrNoRows)
	})

	convey.Convey("E4: delete enrich endpoint returns success when the cache entry is absent", t, func() {
		convey.So(store.DeleteEnrichCache("missing"), convey.ShouldBeNil)

		request := httptest.NewRequest(http.MethodDelete, "/enrich/missing", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
	})

	convey.Convey("E4: delete enrich endpoint returns 500 when the store is closed", t, func() {
		closedStore, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.So(closedStore.Close(), convey.ShouldBeNil)

		closedServer := NewServer(provider, closedStore)
		request := httptest.NewRequest(http.MethodDelete, "/enrich/x", nil)
		recorder := httptest.NewRecorder()

		closedServer.Handler().ServeHTTP(recorder, request)

		var body map[string]any
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusInternalServerError)
		convey.So(body["error"], convey.ShouldNotBeBlank)
	})
}
