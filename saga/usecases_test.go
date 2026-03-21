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

func TestSampleAllMetadata(t *testing.T) {
	Convey("Given MLWH returns two rows and iRODS returns one file with duplicate AVUs", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","taxon_id":9606,"common_name":"human","library_type":"Chromium","id_run":101,"lane":1,"tag_index":10,"irods_path":"/irods/S1/run1"},{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","taxon_id":9606,"common_name":"human","library_type":"Chromium","id_run":102,"lane":2,"tag_index":10,"irods_path":"/irods/S1/run2"}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"collection":"/seq/sample/S1","metadata":[{"name":"study_id","value":"100"},{"name":"study_id","value":"100"},{"name":"library_type","value":"Chromium"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		metadata, err := client.SampleAllMetadata(context.Background(), "S1")

		Convey("when SampleAllMetadata is called, then it merges MLWH rows with deduplicated iRODS AVUs", func() {
			So(err, ShouldBeNil)
			So(metadata, ShouldNotBeNil)
			So(metadata.SangerID, ShouldEqual, "S1")
			So(metadata.SampleName, ShouldEqual, "Sample 1")
			So(metadata.TaxonID, ShouldEqual, 9606)
			So(metadata.CommonName, ShouldEqual, "human")
			So(metadata.MLWH, ShouldHaveLength, 2)
			So(metadata.IRODSFiles, ShouldHaveLength, 1)
			So(metadata.AVUs["study_id"], ShouldResemble, []string{"100"})
			So(metadata.AVUs["library_type"], ShouldResemble, []string{"Chromium"})
		})
	})

	Convey("Given MLWH returns sample rows and iRODS returns 404", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","taxon_id":9606,"common_name":"human","library_type":"Chromium","id_run":101,"lane":1,"tag_index":10}],"total":1,"offset":0,"limit":100}`))
			case "/integrations/irods/samples/S1":
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		metadata, err := client.SampleAllMetadata(context.Background(), "S1")

		Convey("when SampleAllMetadata is called, then it returns MLWH data with empty iRODS fields", func() {
			So(err, ShouldBeNil)
			So(metadata, ShouldNotBeNil)
			So(metadata.SangerID, ShouldEqual, "S1")
			So(metadata.MLWH, ShouldHaveLength, 1)
			So(metadata.IRODSFiles, ShouldNotBeNil)
			So(metadata.IRODSFiles, ShouldHaveLength, 0)
			So(metadata.AVUs, ShouldNotBeNil)
			So(len(metadata.AVUs), ShouldEqual, 0)
		})
	})

	Convey("Given iRODS returns study metadata and MLWH only serves the matching study through filters", t, func() {
		var requestedFilters []string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"collection":"/seq/sample/S1","metadata":[{"name":"study_id","value":"100"},{"name":"library_type","value":"Chromium"}]}],"total":1}`))
			case "/integrations/mlwh/samples":
				requestedFilters = append(requestedFilters, r.URL.Query().Get("filters"))

				if r.URL.Query().Get("filters") != `{"study_id":"100"}` {
					http.Error(w, "expected filtered request", http.StatusInternalServerError)

					return
				}

				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","taxon_id":9606,"common_name":"human","library_type":"Chromium","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"100","id_sample_lims":"S2-LIMS","sanger_id":"S2","sample_name":"Sample 2","taxon_id":9606,"common_name":"human","library_type":"Chromium","id_run":102,"lane":2,"tag_index":11}],"total":2,"offset":0,"limit":100}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		metadata, err := client.SampleAllMetadata(context.Background(), "S1")

		Convey("when SampleAllMetadata is called, then it uses study-scoped MLWH requests instead of a full scan", func() {
			So(err, ShouldBeNil)
			So(metadata, ShouldNotBeNil)
			So(metadata.SangerID, ShouldEqual, "S1")
			So(metadata.MLWH, ShouldHaveLength, 1)
			So(metadata.MLWH[0].SangerID, ShouldEqual, "S1")
			So(requestedFilters, ShouldResemble, []string{"{\"study_id\":\"100\"}"})
		})
	})

	Convey("Given neither MLWH nor iRODS has the sample", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
			case "/integrations/irods/samples/S1":
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		metadata, err := client.SampleAllMetadata(context.Background(), "S1")

		Convey("when SampleAllMetadata is called, then it returns ErrNotFound", func() {
			So(metadata, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrNotFound), ShouldBeTrue)
		})
	})
}

func TestStudyAllSamples(t *testing.T) {
	Convey("Given MLWH returns five sample rows across two pages where three belong to study 100", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("page") {
			case "", "1":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1","sanger_id":"sample-1","sample_name":"Sample 1","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"200","id_sample_lims":"S2","sanger_id":"sample-2","sample_name":"Sample 2","id_run":102,"lane":2,"tag_index":11},{"id_study_lims":"100","id_sample_lims":"S3","sanger_id":"sample-3","sample_name":"Sample 3","id_run":103,"lane":3,"tag_index":12}],"total":5,"offset":0,"limit":3}`))
			case "2":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"300","id_sample_lims":"S4","sanger_id":"sample-4","sample_name":"Sample 4","id_run":104,"lane":4,"tag_index":13},{"id_study_lims":"100","id_sample_lims":"S5","sanger_id":"sample-5","sample_name":"Sample 5","id_run":105,"lane":5,"tag_index":14}],"total":5,"offset":3,"limit":3}`))
			default:
				t.Fatalf("unexpected page request: %s", r.URL.Query().Get("page"))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studySamples, err := client.StudyAllSamples(context.Background(), "100")

		Convey("when StudyAllSamples is called, then it returns the three matching rows", func() {
			So(err, ShouldBeNil)
			So(studySamples, ShouldNotBeNil)
			So(studySamples.StudyID, ShouldEqual, "100")
			So(studySamples.Samples, ShouldHaveLength, 3)
			So(studySamples.Samples[0].IDSampleLims, ShouldEqual, "S1")
			So(studySamples.Samples[1].IDSampleLims, ShouldEqual, "S3")
			So(studySamples.Samples[2].IDSampleLims, ShouldEqual, "S5")
		})
	})

	Convey("Given MLWH only returns study samples when the study filter is supplied", t, func() {
		var requestedFilters []string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedFilters = append(requestedFilters, r.URL.Query().Get("filters"))

			if r.URL.Query().Get("filters") != `{"study_id":"100"}` {
				http.Error(w, "expected filtered request", http.StatusInternalServerError)

				return
			}

			switch r.URL.Query().Get("page") {
			case "", "1":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1","sanger_id":"sample-1","sample_name":"Sample 1","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"100","id_sample_lims":"S3","sanger_id":"sample-3","sample_name":"Sample 3","id_run":103,"lane":3,"tag_index":12}],"total":3,"offset":0,"limit":2}`))
			case "2":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S5","sanger_id":"sample-5","sample_name":"Sample 5","id_run":105,"lane":5,"tag_index":14}],"total":3,"offset":2,"limit":2}`))
			default:
				t.Fatalf("unexpected page request: %s", r.URL.Query().Get("page"))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studySamples, err := client.StudyAllSamples(context.Background(), "100")

		Convey("when StudyAllSamples is called, then it paginates the filtered study view instead of the full catalogue", func() {
			So(err, ShouldBeNil)
			So(studySamples, ShouldNotBeNil)
			So(studySamples.Samples, ShouldHaveLength, 3)
			So(requestedFilters, ShouldResemble, []string{"{\"study_id\":\"100\"}", "{\"study_id\":\"100\"}"})
		})
	})

	Convey("Given MLWH returns sample rows but none belong to study 999", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("page") {
			case "", "1":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1","sanger_id":"sample-1","sample_name":"Sample 1","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"200","id_sample_lims":"S2","sanger_id":"sample-2","sample_name":"Sample 2","id_run":102,"lane":2,"tag_index":11}],"total":2,"offset":0,"limit":2}`))
			default:
				t.Fatalf("unexpected page request: %s", r.URL.Query().Get("page"))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studySamples, err := client.StudyAllSamples(context.Background(), "999")

		Convey("when StudyAllSamples is called, then it returns an empty non-nil slice", func() {
			So(err, ShouldBeNil)
			So(studySamples, ShouldNotBeNil)
			So(studySamples.StudyID, ShouldEqual, "999")
			So(studySamples.Samples, ShouldNotBeNil)
			So(studySamples.Samples, ShouldHaveLength, 0)
		})
	})

	Convey("Given MLWH pagination fails on page two after page one returns matches for study 100", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("page") {
			case "", "1":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1","sanger_id":"sample-1","sample_name":"Sample 1","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"200","id_sample_lims":"S2","sanger_id":"sample-2","sample_name":"Sample 2","id_run":102,"lane":2,"tag_index":11},{"id_study_lims":"100","id_sample_lims":"S3","sanger_id":"sample-3","sample_name":"Sample 3","id_run":103,"lane":3,"tag_index":12}],"total":5,"offset":0,"limit":3}`))
			case "2":
				http.Error(w, "boom", http.StatusInternalServerError)
			default:
				t.Fatalf("unexpected page request: %s", r.URL.Query().Get("page"))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studySamples, err := client.StudyAllSamples(context.Background(), "100")

		Convey("when StudyAllSamples is called, then it returns the matching page one rows alongside the error", func() {
			So(err, ShouldNotBeNil)
			So(studySamples, ShouldNotBeNil)
			So(studySamples.StudyID, ShouldEqual, "100")
			So(studySamples.Samples, ShouldHaveLength, 2)
			So(studySamples.Samples[0].IDSampleLims, ShouldEqual, "S1")
			So(studySamples.Samples[1].IDSampleLims, ShouldEqual, "S3")
			So(errors.Is(err, ErrServerError), ShouldBeTrue)
		})
	})
}

func TestSampleIRODSFiles(t *testing.T) {
	Convey("Given iRODS returns three files where two have analysis_type cellranger count", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"collection":"/seq/sample-1","metadata":[{"name":"analysis_type","value":"cellranger count"},{"name":"library_type","value":"Chromium single cell 3 prime v3"}]},{"id":2,"collection":"/seq/sample-2","metadata":[{"name":"analysis_type","value":"spaceranger count"},{"name":"library_type","value":"Visium"}]},{"id":3,"collection":"/seq/sample-3","metadata":[{"name":"analysis_type","value":"cellranger count"},{"name":"library_type","value":"Chromium single cell 5 prime v2"}]}],"total":3}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		sampleFiles, err := client.SampleIRODSFiles(context.Background(), "S1", &FilterOptions{
			AnalysisType: AnalysisCellrangerCount,
		})

		Convey("when SampleIRODSFiles is called, then it returns only files matching the analysis type", func() {
			So(err, ShouldBeNil)
			So(sampleFiles, ShouldNotBeNil)
			So(sampleFiles.SangerID, ShouldEqual, "S1")
			So(sampleFiles.Files, ShouldHaveLength, 2)
			So(sampleFiles.Files[0].ID, ShouldEqual, 1)
			So(sampleFiles.Files[1].ID, ShouldEqual, 3)
		})
	})

	Convey("Given iRODS returns three files where one has the requested library_type metadata", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"collection":"/seq/sample-1","metadata":[{"name":"analysis_type","value":"cellranger count"},{"name":"library_type","value":"Chromium single cell 3 prime v2"}]},{"id":2,"collection":"/seq/sample-2","metadata":[{"name":"analysis_type","value":"cellranger count"},{"name":"library_type","value":"Chromium single cell 3 prime v3"}]},{"id":3,"collection":"/seq/sample-3","metadata":[{"name":"analysis_type","value":"cellranger count"},{"name":"library_type","value":"Visium"}]}],"total":3}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		sampleFiles, err := client.SampleIRODSFiles(context.Background(), "S1", &FilterOptions{
			Metadata: map[string][]string{
				"library_type": {"Chromium single cell 3 prime v3"},
			},
		})

		Convey("when SampleIRODSFiles is called, then it returns only files matching the metadata filter", func() {
			So(err, ShouldBeNil)
			So(sampleFiles, ShouldNotBeNil)
			So(sampleFiles.SangerID, ShouldEqual, "S1")
			So(sampleFiles.Files, ShouldHaveLength, 1)
			So(sampleFiles.Files[0].ID, ShouldEqual, 2)
		})
	})

	Convey("Given iRODS returns three files and no filter options are provided", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"collection":"/seq/sample-1","metadata":[{"name":"analysis_type","value":"cellranger count"}]},{"id":2,"collection":"/seq/sample-2","metadata":[{"name":"analysis_type","value":"spaceranger count"}]},{"id":3,"collection":"/seq/sample-3","metadata":[{"name":"library_type","value":"Chromium single cell 3 prime v3"}]}],"total":3}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		sampleFiles, err := client.SampleIRODSFiles(context.Background(), "S1", nil)

		Convey("when SampleIRODSFiles is called, then it returns all files unfiltered", func() {
			So(err, ShouldBeNil)
			So(sampleFiles, ShouldNotBeNil)
			So(sampleFiles.SangerID, ShouldEqual, "S1")
			So(sampleFiles.Files, ShouldHaveLength, 3)
		})
	})

	Convey("Given iRODS returns no files for the sample", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[],"total":0}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		sampleFiles, err := client.SampleIRODSFiles(context.Background(), "S1", nil)

		Convey("when SampleIRODSFiles is called, then it returns an empty non-nil Files slice", func() {
			So(err, ShouldBeNil)
			So(sampleFiles, ShouldNotBeNil)
			So(sampleFiles.SangerID, ShouldEqual, "S1")
			So(sampleFiles.Files, ShouldNotBeNil)
			So(sampleFiles.Files, ShouldHaveLength, 0)
		})
	})
}

func TestStudyIRODSFiles(t *testing.T) {
	Convey("Given iRODS AllSamples returns three samples where two match avu:study_id 100", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"source":"IRODS","source_id":"626137912","data":{"avu:study_id":["100"],"avu:sample":["S1"]}},{"id":2,"source":"IRODS","source_id":"626137913","data":{"avu:study_id":["200"],"avu:sample":["S2"]}},{"id":3,"source":"IRODS","source_id":"626137914","data":{"avu:study_id":["100"],"avu:sample":["S3"]}}],"total":3,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","common_name":"Homo Sapien","library_type":"Chromium","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"100","id_sample_lims":"S3-LIMS","sanger_id":"S3","sample_name":"Sample 3","common_name":"Homo Sapien","library_type":"Chromium","id_run":103,"lane":3,"tag_index":12}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":11,"collection":"/seq/S1","metadata":[{"name":"study_id","value":"100"}]}],"total":1}`))
			case "/integrations/irods/samples/S3":
				_, _ = w.Write([]byte(`{"items":[{"id":13,"collection":"/seq/S3","metadata":[{"name":"study_id","value":"100"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", nil)

		Convey("when StudyIRODSFiles is called, then it returns files from the matching iRODS study samples", func() {
			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.StudyID, ShouldEqual, "100")
			So(studyFiles.Files, ShouldHaveLength, 2)
			So(studyFiles.Files[0].ID, ShouldEqual, 11)
			So(studyFiles.Files[1].ID, ShouldEqual, 13)
		})
	})

	Convey("Given iRODS study metadata has no matches but MLWH returns two study samples", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"source":"IRODS","source_id":"626137912","data":{"avu:study_id":["200"],"avu:sample":["X1"]}},{"id":2,"source":"IRODS","source_id":"626137913","data":{"avu:study_id":["300"],"avu:sample":["X2"]}}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","common_name":"Homo Sapien","library_type":"Chromium","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"100","id_sample_lims":"S2-LIMS","sanger_id":"S2","sample_name":"Sample 2","common_name":"Homo Sapien","library_type":"Chromium","id_run":102,"lane":2,"tag_index":11}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":21,"collection":"/seq/S1","metadata":[{"name":"study_id","value":"100"}]}],"total":1}`))
			case "/integrations/irods/samples/S2":
				_, _ = w.Write([]byte(`{"items":[{"id":22,"collection":"/seq/S2","metadata":[{"name":"study_id","value":"100"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", nil)

		Convey("when StudyIRODSFiles is called, then it falls back to MLWH sanger IDs", func() {
			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.StudyID, ShouldEqual, "100")
			So(studyFiles.Files, ShouldHaveLength, 2)
			So(studyFiles.Files[0].ID, ShouldEqual, 21)
			So(studyFiles.Files[1].ID, ShouldEqual, 22)
		})
	})

	Convey("Given iRODS study metadata finds one sample but MLWH reveals an additional study sample", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"source":"IRODS","source_id":"626137912","data":{"avu:study_id":["100"],"avu:sample":["S1"]}},{"id":2,"source":"IRODS","source_id":"626137913","data":{"avu:study_id":["200"],"avu:sample":["X2"]}}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","common_name":"Homo Sapien","library_type":"Chromium","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"100","id_sample_lims":"S2-LIMS","sanger_id":"S2","sample_name":"Sample 2","common_name":"Homo Sapien","library_type":"Chromium","id_run":102,"lane":2,"tag_index":11}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":51,"collection":"/seq/S1","metadata":[{"name":"study_id","value":"100"}]}],"total":1}`))
			case "/integrations/irods/samples/S2":
				_, _ = w.Write([]byte(`{"items":[{"id":52,"collection":"/seq/S2","metadata":[{"name":"study_id","value":"100"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", nil)

		Convey("when StudyIRODSFiles is called, then MLWH augments the direct iRODS matches", func() {
			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.StudyID, ShouldEqual, "100")
			So(studyFiles.Files, ShouldHaveLength, 2)
			So(studyFiles.Files[0].ID, ShouldEqual, 51)
			So(studyFiles.Files[1].ID, ShouldEqual, 52)
		})
	})

	Convey("Given iRODS study metadata finds direct matches but MLWH is unavailable", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"source":"IRODS","source_id":"626137912","data":{"avu:study_id":["100"],"avu:sample":["S1"]}},{"id":2,"source":"IRODS","source_id":"626137913","data":{"avu:study_id":["200"],"avu:sample":["X2"]}}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				http.Error(w, "mlwh unavailable", http.StatusInternalServerError)
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":71,"collection":"/seq/S1","metadata":[{"name":"study_id","value":"100"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", nil)

		Convey("when StudyIRODSFiles is called, then it still returns the direct iRODS files", func() {
			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.StudyID, ShouldEqual, "100")
			So(studyFiles.Files, ShouldHaveLength, 1)
			So(studyFiles.Files[0].ID, ShouldEqual, 71)
		})
	})

	Convey("Given direct iRODS study matches satisfy the metadata filter but MLWH is unavailable", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"source":"IRODS","source_id":"626137912","data":{"avu:study_id":["100"],"avu:sample":["S1"]}}],"total":1,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				http.Error(w, "mlwh unavailable", http.StatusInternalServerError)
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":72,"collection":"/seq/S1","metadata":[{"name":"study_id","value":"100"},{"name":"library_type","value":"Chromium"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", &FilterOptions{
			Metadata: map[string][]string{
				"study_id": {"100"},
			},
		})

		Convey("when StudyIRODSFiles is called, then direct iRODS matches are still returned", func() {
			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.StudyID, ShouldEqual, "100")
			So(studyFiles.Files, ShouldHaveLength, 1)
			So(studyFiles.Files[0].ID, ShouldEqual, 72)
		})
	})

	Convey("Given StudyIRODSFiles needs MLWH-only library_type context and MLWH is unavailable", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"source":"IRODS","source_id":"626137912","data":{"avu:study_id":["100"],"avu:sample":["S1"]}}],"total":1,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				http.Error(w, "mlwh unavailable", http.StatusInternalServerError)
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":73,"collection":"/seq/S1","metadata":[{"name":"study_id","value":"100"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", &FilterOptions{
			Metadata: map[string][]string{
				"library_type": {"Chromium"},
			},
		})

		Convey("when StudyIRODSFiles is called, then it returns the MLWH outage instead of silently dropping all direct matches", func() {
			So(studyFiles, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrServerError), ShouldBeTrue)
		})
	})

	Convey("Given StudyIRODSFiles is called with an analysis type filter", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"source":"IRODS","source_id":"626137912","data":{"avu:study_id":["100"],"avu:sample":["S1"]}},{"id":2,"source":"IRODS","source_id":"626137914","data":{"avu:study_id":["100"],"avu:sample":["S2"]}}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","common_name":"Homo Sapien","library_type":"Chromium","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"100","id_sample_lims":"S2-LIMS","sanger_id":"S2","sample_name":"Sample 2","common_name":"Homo Sapien","library_type":"Chromium","id_run":102,"lane":2,"tag_index":11}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":31,"collection":"/seq/S1","metadata":[{"name":"analysis_type","value":"spaceranger count"}]}],"total":1}`))
			case "/integrations/irods/samples/S2":
				_, _ = w.Write([]byte(`{"items":[{"id":32,"collection":"/seq/S2","metadata":[{"name":"analysis_type","value":"cellranger count"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", &FilterOptions{
			AnalysisType: AnalysisSpacerangerCount,
		})

		Convey("when StudyIRODSFiles is called, then only files matching the analysis type are returned", func() {
			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.Files, ShouldHaveLength, 1)
			So(studyFiles.Files[0].ID, ShouldEqual, 31)
		})
	})

	Convey("Given direct iRODS study matches are augmented by MLWH and filtering needs an MLWH-only field", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"source":"IRODS","source_id":"626137912","data":{"avu:study_id":["100"],"avu:sample":["S1"]}},{"id":2,"source":"IRODS","source_id":"626137913","data":{"avu:study_id":["200"],"avu:sample":["X2"]}}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","common_name":"Homo Sapien","library_type":"Chromium","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"100","id_sample_lims":"S2-LIMS","sanger_id":"S2","sample_name":"Sample 2","common_name":"Mus musculus","library_type":"Chromium","id_run":102,"lane":2,"tag_index":11}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":61,"collection":"/seq/S1","metadata":[{"name":"analysis_type","value":"cellranger count"}]}],"total":1}`))
			case "/integrations/irods/samples/S2":
				_, _ = w.Write([]byte(`{"items":[{"id":62,"collection":"/seq/S2","metadata":[{"name":"analysis_type","value":"cellranger count"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", &FilterOptions{
			Metadata: map[string][]string{
				"common_name": {"Homo Sapien"},
			},
		})

		Convey("when StudyIRODSFiles is called, then direct and fallback matches are both filtered with MLWH sample context", func() {
			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.Files, ShouldHaveLength, 1)
			So(studyFiles.Files[0].ID, ShouldEqual, 61)
		})
	})

	Convey("Given neither iRODS nor MLWH can find study samples", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", nil)

		Convey("when StudyIRODSFiles is called, then it returns an empty non-nil file slice", func() {
			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.StudyID, ShouldEqual, "100")
			So(studyFiles.Files, ShouldNotBeNil)
			So(studyFiles.Files, ShouldHaveLength, 0)
		})
	})

	Convey("Given MLWH cross-reference is used with a common_name metadata filter", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/irods/samples":
				_, _ = w.Write([]byte(`{"items":[{"id":1,"source":"IRODS","source_id":"626137912","data":{"avu:study_id":["200"],"avu:sample":["X1"]}}],"total":1,"offset":0,"limit":100}`))
			case "/integrations/mlwh/samples":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1-LIMS","sanger_id":"S1","sample_name":"Sample 1","common_name":"Homo Sapien","library_type":"Chromium","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"100","id_sample_lims":"S2-LIMS","sanger_id":"S2","sample_name":"Sample 2","common_name":"Mus musculus","library_type":"Chromium","id_run":102,"lane":2,"tag_index":11}],"total":2,"offset":0,"limit":100}`))
			case "/integrations/irods/samples/S1":
				_, _ = w.Write([]byte(`{"items":[{"id":41,"collection":"/seq/S1","metadata":[{"name":"analysis_type","value":"cellranger count"}]}],"total":1}`))
			case "/integrations/irods/samples/S2":
				_, _ = w.Write([]byte(`{"items":[{"id":42,"collection":"/seq/S2","metadata":[{"name":"analysis_type","value":"cellranger count"}]}],"total":1}`))
			default:
				http.NotFound(w, r)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studyFiles, err := client.StudyIRODSFiles(context.Background(), "100", &FilterOptions{
			Metadata: map[string][]string{
				"common_name": {"Homo Sapien"},
			},
		})

		Convey("when StudyIRODSFiles is called, then it applies metadata filtering using MLWH sample fields on the fallback path", func() {
			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.Files, ShouldHaveLength, 1)
			So(studyFiles.Files[0].ID, ShouldEqual, 41)
		})
	})
}
