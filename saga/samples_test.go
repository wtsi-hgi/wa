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

package saga

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSamplesList(t *testing.T) {
	Convey("Given a mock server returning two saga samples with a total of five", t, func() {
		var requestedPath string
		var requestedPage string
		var requestedPageSize string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			requestedPage = r.URL.Query().Get("page")
			requestedPageSize = r.URL.Query().Get("pageSize")
			_, _ = w.Write([]byte(`{"items":[{"id":1,"name":"sample-1","source":"IRODS","source_id":"123","data":{"study_id":"3361"},"curated":{"status":"ready"},"parent":null},{"id":2,"name":"sample-2","source":"IRODS","source_id":"124","data":{},"curated":{},"parent":1}],"total":5,"offset":0,"limit":2}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		response, err := client.Samples().List(context.Background(), PageOptions{Page: 1, PageSize: 2})

		Convey("when List is called, then it decodes the page and total correctly", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/samples/")
			So(requestedPage, ShouldEqual, "1")
			So(requestedPageSize, ShouldEqual, "2")
			So(response, ShouldNotBeNil)
			So(response.Items, ShouldHaveLength, 2)
			So(response.Total, ShouldNotBeNil)
			So(*response.Total, ShouldEqual, 5)
			So(response.Items[0].SourceID, ShouldEqual, "123")
			So(response.Items[1].Parent, ShouldNotBeNil)
			So(*response.Items[1].Parent, ShouldEqual, 1)
		})
	})

	Convey("Given a mock server returning an empty saga samples page", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":50}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		response, err := client.Samples().List(context.Background(), PageOptions{})

		Convey("when List is called, then it returns an empty non-nil slice", func() {
			So(err, ShouldBeNil)
			So(response, ShouldNotBeNil)
			So(response.Items, ShouldNotBeNil)
			So(response.Items, ShouldHaveLength, 0)
			So(response.Total, ShouldNotBeNil)
			So(*response.Total, ShouldEqual, 0)
		})
	})
}

func TestSamplesAll(t *testing.T) {
	Convey("Given a mock server returning two saga sample pages with three items total", t, func() {
		requests := 0

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++

			switch r.URL.Query().Get("page") {
			case "", "1":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"name":"sample-1","source":"IRODS","source_id":"123","data":{},"curated":{},"parent":null},{"id":2,"name":"sample-2","source":"IRODS","source_id":"124","data":{},"curated":{},"parent":null}],"total":3,"offset":0,"limit":2}`))
			case "2":
				_, _ = w.Write([]byte(`{"items":[{"id":3,"name":"sample-3","source":"IRODS","source_id":"125","data":{},"curated":{},"parent":null}],"total":3,"offset":2,"limit":2}`))
			default:
				t.Fatalf("unexpected page request: %s", r.URL.Query().Get("page"))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.Samples().All(context.Background())

		Convey("when All is called, then it collects all sample pages", func() {
			So(err, ShouldBeNil)
			So(requests, ShouldEqual, 2)
			So(samples, ShouldHaveLength, 3)
			So(samples[0].SourceID, ShouldEqual, "123")
			So(samples[2].SourceID, ShouldEqual, "125")
		})
	})
}

func TestSamplesCreate(t *testing.T) {
	Convey("Given a mock server accepting a sample creation request", t, func() {
		var requestedPath string
		var requestedMethod string
		var requestBody string
		var requestBodyErr error

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			requestedMethod = r.Method

			body, err := io.ReadAll(r.Body)
			requestBodyErr = err
			requestBody = string(body)

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1,"name":"sample-1","source":"IRODS","source_id":"123","data":{},"curated":{},"parent":null}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		sample, err := client.Samples().Create(context.Background(), "IRODS", "123")

		Convey("when Create is called, then it posts the source fields and returns the created sample", func() {
			So(err, ShouldBeNil)
			So(requestBodyErr, ShouldBeNil)
			So(requestedMethod, ShouldEqual, http.MethodPost)
			So(requestedPath, ShouldEqual, "/samples/")
			So(requestBody, ShouldEqual, `{"source":"IRODS","source_id":"123"}`)
			So(sample, ShouldNotBeNil)
			So(sample.ID, ShouldEqual, 1)
			So(sample.Source, ShouldEqual, "IRODS")
			So(sample.SourceID, ShouldEqual, "123")
		})
	})
}

func TestSamplesCreateInvalidatesListCache(t *testing.T) {
	Convey("Given a cached saga samples list", t, func() {
		var getRequests atomic.Int32
		var postRequests atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Path == "/samples/" {
					getRequests.Add(1)
				}

				_, _ = w.Write([]byte(`{"items":[{"id":1,"name":"sample-1","source":"IRODS","source_id":"123","data":{},"curated":{},"parent":null}],"total":1,"offset":0,"limit":50}`))
			case http.MethodPost:
				postRequests.Add(1)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":2,"name":"sample-2","source":"IRODS","source_id":"124","data":{},"curated":{},"parent":null}`))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Samples().List(context.Background(), PageOptions{})
		So(err, ShouldBeNil)

		_, err = client.Samples().Create(context.Background(), "IRODS", "124")
		So(err, ShouldBeNil)

		_, err = client.Samples().List(context.Background(), PageOptions{})

		Convey("when Create is called, then the cached samples list is invalidated", func() {
			So(err, ShouldBeNil)
			So(postRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}

func TestSamplesGetBySource(t *testing.T) {
	Convey("Given a mock server returning a saga sample for a source and source ID", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`{"id":7,"name":"sample-7","source":"IRODS","source_id":"456","data":{"study_id":"6568"},"curated":{"status":"ready"},"parent":null}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		sample, err := client.Samples().GetBySource(context.Background(), "IRODS", "456")

		Convey("when GetBySource is called, then it requests the source endpoint and decodes the sample", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/samples/IRODS/456")
			So(sample, ShouldNotBeNil)
			So(sample.Source, ShouldEqual, "IRODS")
			So(sample.SourceID, ShouldEqual, "456")
		})
	})

	Convey("Given source path segments containing reserved characters", t, func() {
		var requestedURI string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedURI = r.RequestURI
			_, _ = w.Write([]byte(`{"id":7,"name":"sample-7","source":"IRODS/ARCHIVE","source_id":"folder/456","data":{},"curated":{},"parent":null}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Samples().GetBySource(context.Background(), "IRODS/ARCHIVE", "folder/456")

		Convey("when GetBySource is called, then it escapes each path segment", func() {
			So(err, ShouldBeNil)
			So(requestedURI, ShouldEqual, "/samples/IRODS%2FARCHIVE/folder%2F456")
		})
	})

	Convey("Given a mock server returning 404 for a source sample", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		sample, err := client.Samples().GetBySource(context.Background(), "IRODS", "456")

		Convey("when GetBySource is called, then it returns ErrNotFound", func() {
			So(sample, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrNotFound), ShouldBeTrue)
		})
	})
}

func TestSamplesGetStudySamples(t *testing.T) {
	Convey("Given a mock server returning three saga samples for a study source ID", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"id":7,"name":"sample-7","source":"IRODS","source_id":"456","data":{"study_id":"6568"},"curated":{"status":"ready"},"parent":null},{"id":8,"name":"sample-8","source":"IRODS","source_id":"457","data":{"study_id":"6568"},"curated":{},"parent":null},{"id":9,"name":"sample-9","source":"IRODS","source_id":"458","data":{"study_id":"6568"},"curated":{},"parent":7}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.Samples().GetStudySamples(context.Background(), "IRODS", "6568")

		Convey("when GetStudySamples is called, then it requests the study samples endpoint and decodes all samples", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/samples/IRODS/studies/6568")
			So(samples, ShouldHaveLength, 3)
			So(samples[0].Source, ShouldEqual, "IRODS")
			So(samples[0].SourceID, ShouldEqual, "456")
			So(samples[2].Parent, ShouldNotBeNil)
			So(*samples[2].Parent, ShouldEqual, 7)
		})
	})

	Convey("Given a study source ID containing reserved path characters", t, func() {
		var requestedURI string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedURI = r.RequestURI
			_, _ = w.Write([]byte(`[]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Samples().GetStudySamples(context.Background(), "IRODS/ARCHIVE", "study/6568")

		Convey("when GetStudySamples is called, then it escapes each path segment", func() {
			So(err, ShouldBeNil)
			So(requestedURI, ShouldEqual, "/samples/IRODS%2FARCHIVE/studies/study%2F6568")
		})
	})

	Convey("Given a mock server returning no saga samples for a study source ID", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.Samples().GetStudySamples(context.Background(), "IRODS", "6568")

		Convey("when GetStudySamples is called, then it returns an empty non-nil slice", func() {
			So(err, ShouldBeNil)
			So(samples, ShouldNotBeNil)
			So(samples, ShouldHaveLength, 0)
		})
	})

	Convey("Given a mock server returning 404 for study samples", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.Samples().GetStudySamples(context.Background(), "IRODS", "6568")

		Convey("when GetStudySamples is called, then it returns ErrNotFound", func() {
			So(samples, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrNotFound), ShouldBeTrue)
		})
	})
}

func TestSamplesListStudies(t *testing.T) {
	Convey("Given a mock server returning two saga studies", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"id":71,"id_study_lims":"study-a","name":"Cell Atlas"},{"id":72,"id_study_lims":"study-b","name":"Spatial Pilot"}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL+"/api"))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studies, err := client.Samples().ListStudies(context.Background())

		Convey("when ListStudies is called, then it requests the live studies endpoint and returns two studies", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/api/studies/")
			So(studies, ShouldHaveLength, 2)
			So(studies[0].ID, ShouldEqual, 71)
			So(studies[0].IDStudyLims, ShouldEqual, "study-a")
			So(studies[0].Name, ShouldEqual, "Cell Atlas")
			So(studies[1].ID, ShouldEqual, 72)
			So(studies[1].IDStudyLims, ShouldEqual, "study-b")
			So(studies[1].Name, ShouldEqual, "Spatial Pilot")
		})
	})

	Convey("Given a mock server returning no saga studies", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL+"/api"))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studies, err := client.Samples().ListStudies(context.Background())

		Convey("when ListStudies is called, then it returns an empty non-nil slice", func() {
			So(err, ShouldBeNil)
			So(studies, ShouldNotBeNil)
			So(studies, ShouldHaveLength, 0)
		})
	})

	Convey("Given the live studies endpoint returns only its namespace root payload", t, func() {
		var requestedPaths []string
		var mlwhPageQuery string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPaths = append(requestedPaths, r.URL.Path)

			switch r.URL.Path {
			case "/api/studies/":
				_, _ = w.Write([]byte(`{"studies":"Saga"}`))
			case "/api/integrations/mlwh/studies":
				mlwhPageQuery = r.URL.RawQuery
				_, _ = w.Write([]byte(`{"items":[{"id_study_tmp":71,"id_study_lims":"study-a","name":"Cell Atlas"},{"id_study_tmp":72,"id_study_lims":"study-b","name":"Spatial Pilot"}],"total":2,"offset":0,"limit":100}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL+"/api"))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studies, err := client.Samples().ListStudies(context.Background())

		Convey("when ListStudies is called, then it falls back to MLWH studies and maps them into SagaStudy values", func() {
			So(err, ShouldBeNil)
			So(requestedPaths, ShouldResemble, []string{"/api/studies/", "/api/integrations/mlwh/studies"})
			So(mlwhPageQuery, ShouldEqual, "page=1&pageSize=100")
			So(studies, ShouldHaveLength, 2)
			So(studies[0].ID, ShouldEqual, 71)
			So(studies[0].IDStudyLims, ShouldEqual, "study-a")
			So(studies[0].Name, ShouldEqual, "Cell Atlas")
			So(studies[1].ID, ShouldEqual, 72)
			So(studies[1].IDStudyLims, ShouldEqual, "study-b")
			So(studies[1].Name, ShouldEqual, "Spatial Pilot")
		})
	})
}
