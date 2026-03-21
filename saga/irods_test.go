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
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestIRODSListSamples(t *testing.T) {
	Convey("Given a mock server returning two iRODS samples with nested data maps", t, func() {
		var requestedPath string
		var requestedPage string
		var requestedPageSize string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			requestedPage = r.URL.Query().Get("page")
			requestedPageSize = r.URL.Query().Get("pageSize")
			_, _ = w.Write([]byte(`{"items":[{"id":1,"name":"sample-1","source":"IRODS","source_id":"sample-1","data":{"avu:study_id":["6568"],"nested":{"library_type":"scRNA-seq"}},"curated":{"status":"ready"},"parent":null},{"id":2,"name":"sample-2","source":"IRODS","source_id":"sample-2","data":{"avu:study_id":["7777"],"nested":{"library_type":"ATAC-seq"}},"curated":{},"parent":1}],"total":2,"offset":0,"limit":10}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		response, err := client.IRODS().ListSamples(context.Background(), PageOptions{Page: 1, PageSize: 10})

		Convey("when ListSamples is called, then it decodes the page and preserves nested data maps", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/irods/samples")
			So(requestedPage, ShouldEqual, "1")
			So(requestedPageSize, ShouldEqual, "10")
			So(response, ShouldNotBeNil)
			So(response.Items, ShouldHaveLength, 2)
			So(response.Items[0].Data, ShouldNotBeNil)
			So(response.Items[0].Data["nested"], ShouldHaveSameTypeAs, map[string]any{})
			So(response.Items[0].Curated["status"], ShouldEqual, "ready")
			So(response.Items[1].Parent, ShouldNotBeNil)
			So(*response.Items[1].Parent, ShouldEqual, 1)
		})

		Convey("when ListSamples is called, then array values in data are preserved", func() {
			So(err, ShouldBeNil)
			studyIDs, ok := response.Items[0].Data["avu:study_id"].([]any)
			So(ok, ShouldBeTrue)
			So(studyIDs, ShouldResemble, []any{"6568"})
		})
	})
}

func TestIRODSAllSamples(t *testing.T) {
	Convey("Given a mock server returning three iRODS sample pages", t, func() {
		requests := 0

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++

			switch r.URL.Query().Get("page") {
			case "", "1":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"name":"sample-1","source":"IRODS","source_id":"sample-1","data":{},"curated":{},"parent":null}],"total":3,"offset":0,"limit":1}`))
			case "2":
				_, _ = w.Write([]byte(`{"items":[{"id":2,"name":"sample-2","source":"IRODS","source_id":"sample-2","data":{},"curated":{},"parent":null}],"total":3,"offset":1,"limit":1}`))
			case "3":
				_, _ = w.Write([]byte(`{"items":[{"id":3,"name":"sample-3","source":"IRODS","source_id":"sample-3","data":{},"curated":{},"parent":null}],"total":3,"offset":2,"limit":1}`))
			default:
				t.Fatalf("unexpected page request: %s", r.URL.Query().Get("page"))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.IRODS().AllSamples(context.Background())

		Convey("when AllSamples is called, then it collects every page", func() {
			So(err, ShouldBeNil)
			So(requests, ShouldEqual, 3)
			So(samples, ShouldHaveLength, 3)
			So(samples[0].SourceID, ShouldEqual, "sample-1")
			So(samples[2].SourceID, ShouldEqual, "sample-3")
		})
	})
}

func TestIRODSPing(t *testing.T) {
	Convey("Given a mock server returning 200 from the iRODS root endpoint", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		err = client.IRODS().Ping(context.Background())

		Convey("when Ping is called, then it returns nil and targets the iRODS root", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/irods/")
		})
	})

	Convey("Given a mock server returning 500 from the iRODS root endpoint", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		err = client.IRODS().Ping(context.Background())

		Convey("when Ping is called, then the error wraps ErrServerError", func() {
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrServerError), ShouldBeTrue)
		})
	})
}

func TestIRODSGetSampleFiles(t *testing.T) {
	Convey("Given a mock server returning one iRODS file with metadata for a sample", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`{"items":[{"id":626137912,"collection":"/seq/illumina/cellranger-arc/sample","metadata":[{"name":"sample_common_name","value":"human"},{"name":"library_type","value":"Chromium single cell 3 prime v3"},{"name":"analysis_type","value":"cellranger-arc count"}]}],"total":1}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		files, err := client.IRODS().GetSampleFiles(context.Background(), "WTSI_wEMB10524782")

		Convey("when GetSampleFiles is called, then it requests the sample endpoint and decodes files with metadata", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/irods/samples/WTSI_wEMB10524782")
			So(files, ShouldHaveLength, 1)
			So(files[0].Collection, ShouldEqual, "/seq/illumina/cellranger-arc/sample")
			So(files[0].Metadata, ShouldResemble, []IRODSMetadata{
				{Name: "sample_common_name", Value: "human"},
				{Name: "library_type", Value: "Chromium single cell 3 prime v3"},
				{Name: "analysis_type", Value: "cellranger-arc count"},
			})
		})
	})

	Convey("Given a mock server returning no iRODS files for a sample", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"items":[],"total":0}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		files, err := client.IRODS().GetSampleFiles(context.Background(), "WTSI_wEMB10524782")

		Convey("when GetSampleFiles is called, then it returns an empty non-nil slice", func() {
			So(err, ShouldBeNil)
			So(files, ShouldNotBeNil)
			So(files, ShouldHaveLength, 0)
		})
	})

	Convey("Given a mock server returning 404 for a sample", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		files, err := client.IRODS().GetSampleFiles(context.Background(), "WTSI_wEMB10524782")

		Convey("when GetSampleFiles is called, then it returns ErrNotFound", func() {
			So(files, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrNotFound), ShouldBeTrue)
		})
	})
}

func TestIRODSGetWebSummary(t *testing.T) {
	Convey("Given a mock server returning an HTML web summary", t, func() {
		var requestedPath string
		expected := []byte("<html><body><h1>summary</h1></body></html>")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(expected)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		summary, err := client.IRODS().GetWebSummary(context.Background(), "collection-123")

		Convey("when GetWebSummary is called, then it returns the raw response bytes", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/irods/web-summary/collection-123")
			So(summary, ShouldResemble, expected)
		})
	})
}

func TestIRODSListAnalysisTypes(t *testing.T) {
	Convey("Given a mock server returning five iRODS analysis types", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"name":"cellranger multi"},{"name":"cellranger count"},{"name":"spaceranger count"},{"name":"cellranger-atac count"},{"name":"cellranger-arc count"}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		types, err := client.IRODS().ListAnalysisTypes(context.Background())

		Convey("when ListAnalysisTypes is called, then it returns all analysis types from the array response", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/irods/analysis-types")
			So(types, ShouldHaveLength, 5)
			So(types, ShouldContain, IRODSAnalysisType{Name: "cellranger count"})
			So(types, ShouldContain, IRODSAnalysisType{Name: "spaceranger count"})
		})
	})
}
