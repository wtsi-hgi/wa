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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
)

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
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusBadGateway)
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
