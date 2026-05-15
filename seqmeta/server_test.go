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
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestServerEndpoints(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	convey.Convey("list studies endpoint uses the mlwh-backed provider surface", t, func() {
		provider := &MockProvider{
			AllStudiesFunc: func(_ context.Context, limit, offset int) ([]mlwh.Study, error) {
				convey.So(limit, convey.ShouldBeGreaterThan, 0)
				convey.So(offset, convey.ShouldEqual, 0)
				return []mlwh.Study{{IDStudyLims: "6568", Name: "Study 6568"}}, nil
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/studies", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var studies []mlwh.Study
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &studies), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(studies, convey.ShouldHaveLength, 1)
		convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "6568")
	})

	convey.Convey("study samples endpoint can filter by library_type", t, func() {
		provider := &MockProvider{
			AllSamplesForStudyFunc: func(_ context.Context, studyID string) ([]mlwh.Sample, error) {
				convey.So(studyID, convey.ShouldEqual, "6568")
				return []mlwh.Sample{
					serverStudySample("6568", "S1", "RNA PolyA"),
					serverStudySample("6568", "S2", "PCR free"),
				}, nil
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/study/6568/samples?library_type=RNA+PolyA", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var samples []mlwh.Sample
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &samples), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].Libraries, convey.ShouldResemble, []mlwh.Library{{PipelineIDLims: "RNA PolyA", IDStudyLims: "6568"}})
	})

	convey.Convey("Bug 1: study samples endpoint filters library_type within the requested study", t, func() {
		provider := &MockProvider{
			AllSamplesForStudyFunc: func(_ context.Context, studyID string) ([]mlwh.Sample, error) {
				convey.So(studyID, convey.ShouldEqual, "6568")
				return []mlwh.Sample{
					{
						Name:    "cross-study-library",
						Studies: []mlwh.Study{{IDStudyLims: "6568"}},
						Libraries: []mlwh.Library{{
							PipelineIDLims: "RNA PolyA",
							IDStudyLims:    "9999",
						}},
					},
					{
						Name:    "in-study-library",
						Studies: []mlwh.Study{{IDStudyLims: "6568"}},
						Libraries: []mlwh.Library{{
							PipelineIDLims: "RNA PolyA",
							IDStudyLims:    "6568",
						}},
					},
				}, nil
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/study/6568/samples?library_type=RNA+PolyA", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var samples []mlwh.Sample
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &samples), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].Name, convey.ShouldEqual, "in-study-library")
	})

	convey.Convey("study diff endpoint returns added samples", t, func() {
		provider := &MockProvider{
			SamplesForStudyFunc: func(_ context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "6568")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)
				return []mlwh.Sample{{Name: "S1"}, {Name: "S2"}}, nil
			},
			AllSamplesForStudyFunc: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return nil, mlwh.ErrUpstreamImpaired
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

	convey.Convey("D4/C1-C3: study diff all routes through AllStudies and preserves tombstones", t, func() {
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
			SamplesForStudyFunc: func(_ context.Context, _ string, _ int, _ int) ([]mlwh.Sample, error) {
				return nil, mlwh.ErrUpstreamImpaired
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

	convey.Convey("sample diff endpoint returns added irods paths", t, func() {
		provider := &MockProvider{
			IRODSPathsForSampleFunc: func(_ context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
				convey.So(sangerName, convey.ShouldEqual, "S1")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)
				return []mlwh.IRODSPath{{IDProduct: "product-1", Collection: "/a", DataObject: "a.cram", IRODSPath: "/a/a.cram"}}, nil
			},
			GetSampleFilesFunc: func(_ context.Context, _ string) ([]mlwh.IRODSPath, error) {
				return nil, mlwh.ErrUpstreamImpaired
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

	convey.Convey("D4/C7: sample diff returns 404 when the sample is missing", t, func() {
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

	convey.Convey("validate endpoint uses ClassifyIdentifier", t, func() {
		provider := &MockProvider{
			ClassifyIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "6568")
				study := &mlwh.Study{IDStudyLims: "6568"}
				return mlwh.Match{Kind: mlwh.KindStudyLimsID, Canonical: "6568", Study: study}, nil
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/validate/6568", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var body map[string]any
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(body["type"], convey.ShouldEqual, string(IdentifierStudyLimsID))
		convey.So(body["identifier"], convey.ShouldEqual, "6568")
	})

	convey.Convey("validate endpoint maps upstream impairment to bad gateway", t, func() {
		provider := &MockProvider{
			ClassifyIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "6568")
				return mlwh.Match{}, mlwh.ErrUpstreamImpaired
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/validate/6568", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusBadGateway)
		convey.So(recorder.Body.String(), convey.ShouldContainSubstring, mlwh.ErrUpstreamImpaired.Error())
	})

	convey.Convey("validate endpoint returns 404 with the never-synced cache hint", t, func() {
		provider := &MockProvider{
			ClassifyIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "6568")

				return mlwh.Match{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
		}
		server := NewServer(provider, store)

		request := httptest.NewRequest(http.MethodGet, "/validate/6568", nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)

		var body map[string]string
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(body["error"], convey.ShouldContainSubstring, mlwh.ErrCacheNeverSynced.Error())
	})
}

func serverStudySample(studyID, name, libraryType string) mlwh.Sample {
	return mlwh.Sample{
		Name:      name,
		Studies:   []mlwh.Study{{IDStudyLims: studyID}},
		Libraries: []mlwh.Library{{PipelineIDLims: libraryType, IDStudyLims: studyID}},
	}
}
