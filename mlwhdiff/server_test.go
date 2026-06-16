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

package mlwhdiff

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestServerEndpoints(t *testing.T) {
	convey.Convey("D3: current-state routes are absent", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		server := NewServer(&MockProvider{}, store)
		noGraphPath := "/en" + "rich/x"
		requests := []*http.Request{
			httptest.NewRequest(http.MethodGet, "/studies", nil),
			httptest.NewRequest(http.MethodGet, "/study/6568/samples", nil),
			httptest.NewRequest(http.MethodGet, "/validate/x", nil),
			httptest.NewRequest(http.MethodGet, noGraphPath, nil),
			httptest.NewRequest(http.MethodDelete, noGraphPath, nil),
		}

		notFoundCount := 0
		for _, request := range requests {
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code == http.StatusNotFound {
				notFoundCount++
			}
		}

		convey.So(notFoundCount, convey.ShouldEqual, len(requests))
	})

	convey.Convey("study diff endpoint returns added samples", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			SamplesForStudyFunc: func(_ context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "6568")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.Sample{{Name: "S1"}, {Name: "S2"}}, nil
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/diff/study/6568", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var diff DiffResult[mlwh.Sample]
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &diff), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(diff.Added, convey.ShouldHaveLength, 2)
		convey.So(diff.Removed, convey.ShouldBeEmpty)
	})

	convey.Convey("D3: study diff all routes through AllStudies and preserves tombstones", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		poll := 0
		provider := &MockProvider{
			AllStudiesFunc: func(_ context.Context, limit, offset int) ([]mlwh.Study, error) {
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				poll++
				switch poll {
				case 1:
					return []mlwh.Study{{IDStudyLims: "6568", Name: "Study One"}, {IDStudyLims: "7777", Name: "Study Two"}}, nil
				case 2:
					return []mlwh.Study{{IDStudyLims: "6568", Name: "Study One Updated"}, {IDStudyLims: "7777", Name: "Study Two"}}, nil
				default:
					return []mlwh.Study{{IDStudyLims: "7777", Name: "Study Two"}}, nil
				}
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/diff/study/all", nil)

		firstRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(firstRecorder, request)
		var firstDiff DiffResult[mlwh.Study]
		convey.So(json.Unmarshal(firstRecorder.Body.Bytes(), &firstDiff), convey.ShouldBeNil)
		convey.So(firstRecorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(firstDiff.Added, convey.ShouldHaveLength, 2)
		convey.So(firstDiff.Modified, convey.ShouldBeEmpty)
		convey.So(firstDiff.Removed, convey.ShouldBeEmpty)

		secondRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(secondRecorder, request)
		var secondDiff DiffResult[mlwh.Study]
		convey.So(json.Unmarshal(secondRecorder.Body.Bytes(), &secondDiff), convey.ShouldBeNil)
		convey.So(secondRecorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(secondDiff.Added, convey.ShouldBeEmpty)
		convey.So(secondDiff.Modified, convey.ShouldResemble, []mlwh.Study{{IDStudyLims: "6568", Name: "Study One Updated"}})
		convey.So(secondDiff.Removed, convey.ShouldBeEmpty)

		thirdRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(thirdRecorder, request)
		var thirdDiff DiffResult[mlwh.Study]
		convey.So(json.Unmarshal(thirdRecorder.Body.Bytes(), &thirdDiff), convey.ShouldBeNil)
		convey.So(thirdRecorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(thirdDiff.Added, convey.ShouldBeEmpty)
		convey.So(thirdDiff.Modified, convey.ShouldBeEmpty)
		convey.So(thirdDiff.Removed, convey.ShouldResemble, []string{"6568"})

		entries, err := store.LoadEntries("studies:all")
		convey.So(err, convey.ShouldBeNil)
		convey.So(entries["6568"].Tombstone, convey.ShouldBeTrue)
	})

	convey.Convey("sample diff endpoint returns added paths", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			IRODSPathsForSampleFunc: func(_ context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
				convey.So(sangerName, convey.ShouldEqual, "S1")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.IRODSPath{{IDProduct: "product-1", Collection: "/a", DataObject: "a.cram", IRODSPath: "/a/a.cram"}}, nil
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/diff/sample/S1", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var diff DiffResult[mlwh.IRODSPath]
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &diff), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(diff.Added, convey.ShouldHaveLength, 1)
		convey.So(diff.Added[0].IDProduct, convey.ShouldEqual, "product-1")
		convey.So(diff.Added[0].IRODSPath, convey.ShouldEqual, "/a/a.cram")
		convey.So(recorder.Body.String(), convey.ShouldNotContainSubstring, "checksum")
		convey.So(recorder.Body.String(), convey.ShouldNotContainSubstring, "avu")
		convey.So(recorder.Body.String(), convey.ShouldNotContainSubstring, "size")
	})

	convey.Convey("sample diff returns 404 when the sample is missing", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			IRODSPathsForSampleFunc: func(_ context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
				convey.So(sangerName, convey.ShouldEqual, "missing")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return nil, mlwh.ErrNotFound
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/diff/sample/missing", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var body map[string]string
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(body["error"], convey.ShouldContainSubstring, "missing")
	})

	convey.Convey("D3: server source imports gin and not chi", t, func() {
		source, err := os.ReadFile("server.go")
		convey.So(err, convey.ShouldBeNil)

		body := string(source)
		convey.So(body, convey.ShouldContainSubstring, `"github.com/gin-gonic/gin"`)
		convey.So(body, convey.ShouldNotContainSubstring, `"github.com/go-chi/chi/v5"`)
	})
}
