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

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
)

func TestClientAdapterGetStudy(t *testing.T) {
	convey.Convey("Given a saga.Client backed by a mock HTTP server returning study JSON for 100", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/integrations/mlwh/studies/100" {
				http.Error(w, "unexpected path", http.StatusBadRequest)

				return
			}

			_, _ = w.Write([]byte(`{"id_study_tmp":100,"id_lims":"SQSCP","id_study_lims":"100","name":"Study 100"}`))
		}))
		defer server.Close()

		client, err := saga.NewClient("test-key", saga.WithBaseURL(server.URL))
		convey.So(err, convey.ShouldBeNil)
		defer client.Close()

		study, err := NewClientAdapter(client).GetStudy(context.Background(), "100")

		convey.Convey("when GetStudy is called, then the returned study has IDStudyLims == 100", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(study, convey.ShouldNotBeNil)
			convey.So(study.IDStudyLims, convey.ShouldEqual, "100")
		})
	})
}

func TestClientAdapterAllSamplesForStudy(t *testing.T) {
	convey.Convey("Given a mock server returning 2 MLWH samples for study 100", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/integrations/mlwh/samples" {
				http.Error(w, "unexpected path", http.StatusBadRequest)

				return
			}

			if r.URL.Query().Get("filters") != `{"study_id":"100"}` {
				http.Error(w, "unexpected filters", http.StatusBadRequest)

				return
			}

			_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"100","id_sample_lims":"S1","sanger_id":"SANG1","sample_name":"Sample 1","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"100","id_sample_lims":"S2","sanger_id":"SANG2","sample_name":"Sample 2","id_run":102,"lane":2,"tag_index":11}],"total":2,"offset":0,"limit":100}`))
		}))
		defer server.Close()

		client, err := saga.NewClient("test-key", saga.WithBaseURL(server.URL))
		convey.So(err, convey.ShouldBeNil)
		defer client.Close()

		samples, err := NewClientAdapter(client).AllSamplesForStudy(context.Background(), "100")

		convey.Convey("when AllSamplesForStudy is called, then 2 samples are returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldHaveLength, 2)
			convey.So(samples[0].SangerID, convey.ShouldEqual, "SANG1")
			convey.So(samples[1].SangerID, convey.ShouldEqual, "SANG2")
		})
	})
}

func TestClientAdapterGetSampleFiles(t *testing.T) {
	convey.Convey("Given a mock server returning 1 iRODS file for SANG1", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/integrations/irods/samples/SANG1" {
				http.Error(w, "unexpected path", http.StatusBadRequest)

				return
			}

			_, _ = w.Write([]byte(`{"items":[{"id":1,"collection":"/irods/SANG1","metadata":[]}],"total":1}`))
		}))
		defer server.Close()

		client, err := saga.NewClient("test-key", saga.WithBaseURL(server.URL))
		convey.So(err, convey.ShouldBeNil)
		defer client.Close()

		files, err := NewClientAdapter(client).GetSampleFiles(context.Background(), "SANG1")

		convey.Convey("when GetSampleFiles is called, then 1 file is returned with the correct Collection", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(files, convey.ShouldHaveLength, 1)
			convey.So(files[0].Collection, convey.ShouldEqual, "/irods/SANG1")
		})
	})
}

func TestClientAdapterListProjects(t *testing.T) {
	convey.Convey("Given a mock server returning 2 projects", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/projects/" {
				http.Error(w, "unexpected path", http.StatusBadRequest)

				return
			}

			_, _ = w.Write([]byte(`[{"id":1,"name":"Cell Atlas"},{"id":2,"name":"Spatial Pilot"}]`))
		}))
		defer server.Close()

		client, err := saga.NewClient("test-key", saga.WithBaseURL(server.URL))
		convey.So(err, convey.ShouldBeNil)
		defer client.Close()

		projects, err := NewClientAdapter(client).ListProjects(context.Background())

		convey.Convey("when ListProjects is called, then 2 projects are returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(projects, convey.ShouldHaveLength, 2)
			convey.So(projects[0].Name, convey.ShouldEqual, "Cell Atlas")
			convey.So(projects[1].Name, convey.ShouldEqual, "Spatial Pilot")
		})
	})
}

func TestClientAdapterFindSamplesBySangerID(t *testing.T) {
	convey.Convey("Given a mock server returning 1 MLWH sample for SANG1", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/integrations/mlwh/samples" {
				http.Error(w, "unexpected path", http.StatusBadRequest)

				return
			}

			if r.URL.Query().Get("filters") != `{"sanger_id":"SANG1"}` {
				http.Error(w, "unexpected filters", http.StatusBadRequest)

				return
			}

			_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"6568","id_sample_lims":"S1","sanger_id":"SANG1","sample_name":"Sample 1"}],"total":1,"offset":0,"limit":100}`))
		}))
		defer server.Close()

		client, err := saga.NewClient("test-key", saga.WithBaseURL(server.URL))
		convey.So(err, convey.ShouldBeNil)
		defer client.Close()

		samples, err := NewClientAdapter(client).FindSamplesBySangerID(context.Background(), "SANG1")

		convey.Convey("when FindSamplesBySangerID is called, then the result has length 1", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldHaveLength, 1)
			convey.So(samples[0].SangerID, convey.ShouldEqual, "SANG1")
		})
	})
}

func TestClientAdapterExtendedDelegates(t *testing.T) {
	convey.Convey("Given a mock server covering the remaining extended adapter delegates", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/integrations/mlwh/samples":
				filters := map[string]string{}
				if err := json.Unmarshal([]byte(r.URL.Query().Get("filters")), &filters); err != nil {
					http.Error(w, "invalid filters", http.StatusBadRequest)

					return
				}

				switch {
				case filters["id_sample_lims"] == "S2":
					_, _ = w.Write([]byte(`{"items":[{"id_sample_lims":"S2","sanger_id":"SANG2"}],"total":1,"offset":0,"limit":100}`))
				case filters["id_run"] == "34134":
					_, _ = w.Write([]byte(`{"items":[{"id_run":34134,"sanger_id":"SANG3"}],"total":1,"offset":0,"limit":100}`))
				case filters["library_type"] == "RNA PolyA":
					_, _ = w.Write([]byte(`{"items":[{"library_type":"RNA PolyA","sanger_id":"SANG4"}],"total":1,"offset":0,"limit":100}`))
				case filters["accession_number"] == "SAM123":
					_, _ = w.Write([]byte(`{"items":[{"accession_number":"SAM123","sanger_id":"SANG5"}],"total":1,"offset":0,"limit":100}`))
				default:
					http.Error(w, "unexpected filters", http.StatusBadRequest)
				}
			case "/integrations/mlwh/studies/6568":
				_, _ = w.Write([]byte(`{"id_study_tmp":1,"id_lims":"SQSCP","id_study_lims":"6568","name":"Study 6568"}`))
			case "/projects/7/studies/":
				_, _ = w.Write([]byte(`[{"id":71,"id_study_lims":"6568"}]`))
			case "/projects/7/samples/":
				_, _ = w.Write([]byte(`[{"id":72,"sanger_id":"SANG7"}]`))
			case "/projects/7/users/":
				_, _ = w.Write([]byte(`[{"id":73,"username":"user7"}]`))
			default:
				http.Error(w, "unexpected path", http.StatusBadRequest)
			}
		}))
		defer server.Close()

		client, err := saga.NewClient("test-key", saga.WithBaseURL(server.URL))
		convey.So(err, convey.ShouldBeNil)
		defer client.Close()

		adapter := NewClientAdapter(client)
		studySample := saga.MLWHSample{IDStudyLims: "6568"}

		idSampleLimsSamples, errIDSampleLims := adapter.FindSamplesByIDSampleLims(context.Background(), "S2")
		runSamples, errRunID := adapter.FindSamplesByRunID(context.Background(), 34134)
		libraryTypeSamples, errLibraryType := adapter.FindSamplesByLibraryType(context.Background(), "RNA PolyA")
		accessionSamples, errAccession := adapter.FindSamplesByAccessionNumber(context.Background(), "SAM123")
		study, errStudy := adapter.StudyForSample(context.Background(), studySample)
		projectStudies, errProjectStudies := adapter.ListProjectStudies(context.Background(), 7)
		projectSamples, errProjectSamples := adapter.ListProjectSamples(context.Background(), 7)
		projectUsers, errProjectUsers := adapter.ListProjectUsers(context.Background(), 7)

		convey.Convey("when each extended adapter method is called, then the expected result is returned", func() {
			convey.So(errIDSampleLims, convey.ShouldBeNil)
			convey.So(idSampleLimsSamples, convey.ShouldHaveLength, 1)
			convey.So(idSampleLimsSamples[0].IDSampleLims, convey.ShouldEqual, "S2")

			convey.So(errRunID, convey.ShouldBeNil)
			convey.So(runSamples, convey.ShouldHaveLength, 1)
			convey.So(runSamples[0].IDRun, convey.ShouldEqual, 34134)

			convey.So(errLibraryType, convey.ShouldBeNil)
			convey.So(libraryTypeSamples, convey.ShouldHaveLength, 1)
			convey.So(libraryTypeSamples[0].LibraryType, convey.ShouldEqual, "RNA PolyA")

			convey.So(errAccession, convey.ShouldBeNil)
			convey.So(accessionSamples, convey.ShouldHaveLength, 1)
			convey.So(accessionSamples[0].AccessionNumber, convey.ShouldEqual, "SAM123")

			convey.So(errStudy, convey.ShouldBeNil)
			convey.So(study, convey.ShouldNotBeNil)
			convey.So(study.IDStudyLims, convey.ShouldEqual, "6568")

			convey.So(errProjectStudies, convey.ShouldBeNil)
			convey.So(projectStudies, convey.ShouldHaveLength, 1)
			convey.So(projectStudies[0].IDStudyLims, convey.ShouldEqual, "6568")

			convey.So(errProjectSamples, convey.ShouldBeNil)
			convey.So(projectSamples, convey.ShouldHaveLength, 1)
			convey.So(projectSamples[0].SangerID, convey.ShouldEqual, "SANG7")

			convey.So(errProjectUsers, convey.ShouldBeNil)
			convey.So(projectUsers, convey.ShouldHaveLength, 1)
			convey.So(projectUsers[0].Username, convey.ShouldEqual, "user7")
		})
	})
}

func TestNewClientAdapterSatisfiesSAGAProvider(t *testing.T) {
	convey.Convey("Given NewClientAdapter(client)", t, func() {
		provider := SAGAProvider(NewClientAdapter(&saga.Client{}))

		convey.Convey("when assigned to a variable of type SAGAProvider, then it compiles", func() {
			convey.So(provider, convey.ShouldHaveSameTypeAs, &ClientAdapter{})
		})
	})
}
