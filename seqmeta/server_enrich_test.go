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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestServerEnrichEndpoint(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	study := &mlwh.Study{IDStudyLims: "6568", Name: "Study 6568"}
	samples := []mlwh.Sample{
		{SangerID: "S1", Name: "Sample 1", IDStudyLims: "6568", LibraryType: "RNA PolyA"},
		{SangerID: "S2", Name: "Sample 2", IDStudyLims: "6568", LibraryType: "RNA PolyA"},
		{SangerID: "S3", Name: "Sample 3", IDStudyLims: "6568", LibraryType: "PCR free"},
	}
	received := ""

	provider := &MockProvider{
		GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
			received = identifier
			if identifier == "6568" || identifier == "foo/bar" {
				return &mlwh.Study{IDStudyLims: identifier, Name: "Study " + identifier}, nil
			}
			return nil, mlwh.ErrNotFound
		},
		AllSamplesForStudyFunc: func(_ context.Context, studyID string) ([]mlwh.Sample, error) {
			if studyID == "6568" {
				return samples, nil
			}
			return []mlwh.Sample{}, nil
		},
	}
	server := NewServer(provider, store)

	convey.Convey("enrich endpoint returns a study graph envelope", t, func() {
		provider.GetStudyFunc = func(_ context.Context, identifier string) (*mlwh.Study, error) {
			received = identifier
			if identifier == "6568" {
				return study, nil
			}
			if identifier == "foo/bar" {
				return &mlwh.Study{IDStudyLims: identifier}, nil
			}
			return nil, mlwh.ErrNotFound
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(result.Graph.Study, convey.ShouldResemble, study)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 3)
		convey.So(result.Graph.Libraries, convey.ShouldHaveLength, 2)
		convey.So(result.Partial, convey.ShouldBeFalse)
	})

	convey.Convey("enrich endpoint passes URL-decoded identifiers to Enrich", t, func() {
		request := httptest.NewRequest(http.MethodGet, "/enrich/foo%2Fbar", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(received, convey.ShouldEqual, "foo/bar")
	})

	convey.Convey("enrich endpoint returns partial results when a downstream hop fails", t, func() {
		convey.So(store.DeleteEnrichCache("6568"), convey.ShouldBeNil)

		provider.GetStudyFunc = func(_ context.Context, _ string) (*mlwh.Study, error) {
			return study, nil
		}
		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]mlwh.Sample, error) {
			return nil, context.DeadlineExceeded
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var body map[string]string
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusBadGateway)
		convey.So(body["error"], convey.ShouldContainSubstring, context.DeadlineExceeded.Error())
	})

	convey.Convey("enrich endpoint ignores stale negative cache entries for library-type identifiers", t, func() {
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "RNA PolyA",
			Body:       []byte(`{"error":"seqmeta: unknown identifier"}`),
			FetchedAt:  time.Now(),
			TTL:        time.Hour,
			Negative:   true,
		}), convey.ShouldBeNil)

		provider.FindSamplesByLibraryTypeFn = func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
			convey.So(libraryType, convey.ShouldEqual, "RNA PolyA")

			return []mlwh.Sample{
				{SangerID: "S1", Name: "Sample 1", IDStudyLims: "6568", LibraryType: libraryType},
			}, nil
		}
		provider.GetStudyFunc = func(_ context.Context, identifier string) (*mlwh.Study, error) {
			if identifier != "6568" {
				return nil, mlwh.ErrNotFound
			}

			return study, nil
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/RNA%20PolyA", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Graph.Studies, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 1)
	})
}
