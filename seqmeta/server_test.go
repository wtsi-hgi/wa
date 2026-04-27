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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
)

func TestServerLoadFreshEnrichCache(t *testing.T) {
	now := time.Date(2026, time.April, 24, 11, 30, 0, 0, time.UTC)

	convey.Convey("Given a server with cached enrich rows", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		server := NewServer(&MockProvider{}, store)

		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "fresh",
			Type:       IdentifierStudyID,
			Body:       []byte(`{"identifier":"fresh"}`),
			FetchedAt:  now,
			TTL:        time.Hour,
		}), convey.ShouldBeNil)
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "expired",
			Type:       IdentifierStudyID,
			Body:       []byte(`{"identifier":"expired"}`),
			FetchedAt:  now.Add(-time.Hour),
			TTL:        time.Minute,
		}), convey.ShouldBeNil)

		convey.Convey("when the cached row is still fresh, then the server returns it", func() {
			entry, err := server.loadFreshEnrichCache("fresh", now.Add(30*time.Minute))

			convey.So(err, convey.ShouldBeNil)
			convey.So(entry, convey.ShouldNotBeNil)
			if entry == nil {
				return
			}
			convey.So(entry.Identifier, convey.ShouldEqual, "fresh")
		})

		convey.Convey("when the cached row has expired, then the server treats it as a miss", func() {
			entry, err := server.loadFreshEnrichCache("expired", now)

			convey.So(entry, convey.ShouldBeNil)
			convey.So(errors.Is(err, sql.ErrNoRows), convey.ShouldBeTrue)
		})
	})
}

type failingResponseWriter struct {
	header http.Header
	code   int
	err    error
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}

	return w.header
}

func (w *failingResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

func (w *failingResponseWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func TestServerStudyDiffEndpoint(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	samples := []saga.MLWHSample{{SangerID: "S1"}, {SangerID: "S2"}}
	provider := &MockProvider{
		AllSamplesForStudyFunc: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return samples, nil
		},
	}
	server := NewServer(provider, store)

	convey.Convey("E1: study diff endpoint returns JSON changes", t, func() {
		request := httptest.NewRequest(http.MethodGet, "/diff/study/100", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result DiffResult[saga.MLWHSample]
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(recorder.Header().Get("Content-Type"), convey.ShouldContainSubstring, "application/json")
		convey.So(result.Added, convey.ShouldHaveLength, 2)
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)

		recorder = httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldBeEmpty)
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)

		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return nil, errors.New("upstream failed")
		}
		recorder = httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var body map[string]string
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusBadGateway)
		convey.So(body, convey.ShouldContainKey, "error")
	})

	convey.Convey("E5: study diff invalidates a cached enrich entry for the study identifier", t, func() {
		provider.AllSamplesForStudyFunc = func(_ context.Context, requestedStudyID string) ([]saga.MLWHSample, error) {
			convey.So(requestedStudyID, convey.ShouldEqual, "6568")

			return samples, nil
		}
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "6568",
			Type:       IdentifierStudyID,
			Body:       []byte(`{"identifier":"6568","type":"study_id","graph":{"study":{"id_study_lims":"6568"}}}`),
			FetchedAt:  time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC),
			TTL:        time.Hour,
		}), convey.ShouldBeNil)

		request := httptest.NewRequest(http.MethodGet, "/diff/study/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		loaded, err := store.LoadEnrichCache("6568")

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(loaded, convey.ShouldBeNil)
		convey.So(err, convey.ShouldEqual, sql.ErrNoRows)
	})

	convey.Convey("E5: study diff invalidates cached enrich entries that reference the study", t, func() {
		provider.AllSamplesForStudyFunc = func(_ context.Context, requestedStudyID string) ([]saga.MLWHSample, error) {
			convey.So(requestedStudyID, convey.ShouldEqual, "6568")

			return samples, nil
		}
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "SANG1",
			Type:       IdentifierSangerSampleID,
			Body:       []byte(`{"identifier":"SANG1","type":"sanger_sample_id","graph":{"sample":{"sanger_id":"SANG1","id_study_lims":"6568"},"study":{"id_study_lims":"6568"}}}`),
			FetchedAt:  time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC),
			TTL:        time.Hour,
		}), convey.ShouldBeNil)

		request := httptest.NewRequest(http.MethodGet, "/diff/study/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		loaded, err := store.LoadEnrichCache("SANG1")

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(loaded, convey.ShouldBeNil)
		convey.So(err, convey.ShouldEqual, sql.ErrNoRows)
	})
}

func TestServerSampleDiffEndpoint(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	files := []saga.IRODSFile{{Collection: "/one"}}
	provider := &MockProvider{
		GetSampleFilesFunc: func(_ context.Context, _ string) ([]saga.IRODSFile, error) {
			return files, nil
		},
	}
	server := NewServer(provider, store)

	convey.Convey("E2: sample diff endpoint returns JSON changes", t, func() {
		request := httptest.NewRequest(http.MethodGet, "/diff/sample/ABC", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result DiffResult[saga.IRODSFile]
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Added, convey.ShouldHaveLength, 1)
		convey.So(result.Added[0].Collection, convey.ShouldEqual, "/one")

		files = []saga.IRODSFile{{Collection: "/one"}, {Collection: "/two"}}
		recorder = httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 1)
		convey.So(result.Added[0].Collection, convey.ShouldEqual, "/two")

		provider.GetSampleFilesFunc = func(_ context.Context, _ string) ([]saga.IRODSFile, error) {
			return nil, saga.ErrNotFound
		}
		recorder = httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusNotFound)
	})

	convey.Convey("E5: sample diff invalidates the cached enrich entry for that sample identifier", t, func() {
		provider.GetSampleFilesFunc = func(_ context.Context, requestedSangerID string) ([]saga.IRODSFile, error) {
			convey.So(requestedSangerID, convey.ShouldEqual, "SANG1")

			return files, nil
		}
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "SANG1",
			Type:       IdentifierSangerSampleID,
			Body:       []byte(`{"identifier":"SANG1","type":"sanger_sample_id","graph":{"sample":{"sanger_id":"SANG1","id_study_lims":"6568"}}}`),
			FetchedAt:  time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC),
			TTL:        time.Hour,
		}), convey.ShouldBeNil)

		request := httptest.NewRequest(http.MethodGet, "/diff/sample/SANG1", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		loaded, err := store.LoadEnrichCache("SANG1")

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(loaded, convey.ShouldBeNil)
		convey.So(err, convey.ShouldEqual, sql.ErrNoRows)
	})
}

func TestServerValidateEndpoint(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	received := ""
	provider := &MockProvider{
		GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
			received = identifier
			if identifier == "6568" || identifier == "foo/bar" {
				return &saga.Study{IDStudyLims: identifier}, nil
			}

			return nil, saga.ErrNotFound
		},
		AllStudiesFunc:   func(_ context.Context) ([]saga.Study, error) { return nil, nil },
		AllSamplesFunc:   func(_ context.Context) ([]saga.MLWHSample, error) { return nil, nil },
		ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) { return nil, nil },
	}
	server := NewServer(provider, store)

	convey.Convey("E3: validate endpoint classifies identifiers", t, func() {
		request := httptest.NewRequest(http.MethodGet, "/validate/6568", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var result IdentifierResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)

		recorder = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodGet, "/validate/xyz", nil)
		server.Handler().ServeHTTP(recorder, request)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusNotFound)

		recorder = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodGet, "/validate/foo%2Fbar", nil)
		server.Handler().ServeHTTP(recorder, request)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(received, convey.ShouldEqual, "foo/bar")
	})

	convey.Convey("E6: validate returns a single matched object without an enrichment graph", t, func() {
		request := httptest.NewRequest(http.MethodGet, "/validate/6568", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var body map[string]any
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(body["type"], convey.ShouldEqual, string(IdentifierStudyID))

		object, ok := body["object"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(object["id_study_lims"], convey.ShouldEqual, "6568")
		_, hasGraph := object["graph"]
		convey.So(hasGraph, convey.ShouldBeFalse)
	})

	convey.Convey("E6: validate uses the targeted Sanger sample lookup and still returns the sample object", t, func() {
		allSamplesCalls := 0
		findSamplesBySangerIDCalls := 0
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.APIError{StatusCode: 422, Message: "Unprocessable Entity"}
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, nil
			},
			AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
				allSamplesCalls++

				return []saga.MLWHSample{{SangerID: "S1"}}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++
				if identifier != "S1" {
					return nil, saga.ErrNotFound
				}

				return []saga.MLWHSample{{SangerID: "S1", IDSampleLims: "L1"}}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				return nil, nil
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/validate/S1", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var body map[string]any
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(body["type"], convey.ShouldEqual, string(IdentifierSangerSampleID))

		object, ok := body["object"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(object["sanger_id"], convey.ShouldEqual, "S1")
		convey.So(object["id_sample_lims"], convey.ShouldEqual, "L1")
		_, hasGraph := object["graph"]
		convey.So(hasGraph, convey.ShouldBeFalse)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 1)
		convey.So(allSamplesCalls, convey.ShouldEqual, 0)
	})
}

func TestServerStudySamplesEndpoint(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	studyID := "6568"
	samples := []saga.MLWHSample{
		{SangerID: "S1"},
		{SangerID: "S2"},
		{SangerID: "S3"},
		{SangerID: "S4"},
		{SangerID: "S5"},
	}
	provider := &MockProvider{
		AllSamplesForStudyFunc: func(_ context.Context, requestedStudyID string) ([]saga.MLWHSample, error) {
			if requestedStudyID != studyID {
				return nil, errors.New("unexpected study id")
			}

			return samples, nil
		},
	}
	server := NewServer(provider, store)

	convey.Convey("F2: study samples endpoint returns study sample JSON", t, func() {
		request := httptest.NewRequest(http.MethodGet, "/study/6568/samples", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result []saga.MLWHSample
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(recorder.Header().Get("Content-Type"), convey.ShouldContainSubstring, "application/json")
		convey.So(result, convey.ShouldHaveLength, 5)
		convey.So(result[0].SangerID, convey.ShouldEqual, "S1")
		convey.So(result[4].SangerID, convey.ShouldEqual, "S5")

		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return nil, saga.ErrNotFound
		}
		recorder = httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusNotFound)

		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return nil, errors.New("upstream failed")
		}
		recorder = httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusBadGateway)

		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return []saga.MLWHSample{}, nil
		}
		recorder = httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(strings.TrimSpace(recorder.Body.String()), convey.ShouldEqual, "[]")
	})
}

func TestServerListStudiesEndpoint(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	studies := []saga.Study{
		{IDStudyLims: "100", Name: "Alpha"},
		{IDStudyLims: "200", Name: "Beta"},
		{IDStudyLims: "300", Name: "Gamma"},
	}
	provider := &MockProvider{
		AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
			return studies, nil
		},
	}
	server := NewServer(provider, store)

	convey.Convey("F1: list studies returns JSON studies from the provider", t, func() {
		request := httptest.NewRequest(http.MethodGet, "/studies", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result []map[string]any
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(recorder.Header().Get("Content-Type"), convey.ShouldContainSubstring, "application/json")
		convey.So(result, convey.ShouldHaveLength, 3)
		convey.So(result[0]["name"], convey.ShouldEqual, "Alpha")
		convey.So(result[0]["id_study_lims"], convey.ShouldEqual, "100")
		convey.So(result[1]["name"], convey.ShouldEqual, "Beta")
		convey.So(result[2]["id_study_lims"], convey.ShouldEqual, "300")
	})

	convey.Convey("F1: list studies returns an empty JSON array when no studies are available", t, func() {
		studies = []saga.Study{}

		request := httptest.NewRequest(http.MethodGet, "/studies", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(recorder.Header().Get("Content-Type"), convey.ShouldContainSubstring, "application/json")
		convey.So(recorder.Body.String(), convey.ShouldEqual, "[]\n")
	})

	convey.Convey("F1: list studies returns a JSON error when the provider fails", t, func() {
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			return nil, errors.New("upstream failed")
		}

		request := httptest.NewRequest(http.MethodGet, "/studies", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var body map[string]string
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusBadGateway)
		convey.So(recorder.Header().Get("Content-Type"), convey.ShouldContainSubstring, "application/json")
		convey.So(body, convey.ShouldContainKey, "error")
	})
}

func TestServerErrorResponses(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	provider := &MockProvider{
		AllSamplesForStudyFunc: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return []saga.MLWHSample{{SangerID: "S1"}}, nil
		},
	}
	server := NewServer(provider, store)

	convey.Convey("E4: error responses are consistent JSON", t, func() {
		recorder := httptest.NewRecorder()
		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return nil, errors.New("bad gateway")
		}
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/diff/study/100", nil))
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusBadGateway)
		convey.So(recorder.Header().Get("Content-Type"), convey.ShouldContainSubstring, "application/json")

		var body map[string]string
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(body, convey.ShouldContainKey, "error")

		convey.So(store.Close(), convey.ShouldBeNil)
		recorder = httptest.NewRecorder()
		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return []saga.MLWHSample{{SangerID: "S1"}}, nil
		}
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/diff/study/100", nil))
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusInternalServerError)
	})
}

func TestServerWriteFailureDoesNotAdvanceWatermark(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	provider := &MockProvider{
		AllSamplesForStudyFunc: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return []saga.MLWHSample{{SangerID: "S1"}, {SangerID: "S2"}}, nil
		},
	}
	server := NewServer(provider, store)

	convey.Convey("HTTP diff does not advance the watermark when writing the response fails", t, func() {
		writer := &failingResponseWriter{err: errors.New("client disconnected")}
		server.Handler().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/diff/study/100", nil))

		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/diff/study/100", nil))

		var result DiffResult[saga.MLWHSample]
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)
	})
}
