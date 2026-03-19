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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMLWHListStudies(t *testing.T) {
	Convey("Given a mock server returning a paginated studies response", t, func() {
		var requestedPath string
		var requestedPage string
		var requestedPageSize string
		var requestedSnakePageSize string
		var requestedSortField string
		var requestedSnakeSortField string
		var requestedSortOrder string
		var requestedSnakeSortOrder string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			requestedPage = r.URL.Query().Get("page")
			requestedPageSize = r.URL.Query().Get("pageSize")
			requestedSnakePageSize = r.URL.Query().Get("page_size")
			requestedSortField = r.URL.Query().Get("sortField")
			requestedSnakeSortField = r.URL.Query().Get("sort_field")
			requestedSortOrder = r.URL.Query().Get("sortOrder")
			requestedSnakeSortOrder = r.URL.Query().Get("sort_order")
			_, _ = w.Write([]byte(`{"items":[{"id_study_tmp":1,"id_lims":"SQSCP","id_study_lims":"3361","name":"Study A","faculty_sponsor":"Sponsor A"},{"id_study_tmp":2,"id_lims":"SQSCP","id_study_lims":"3362","name":"Study B","faculty_sponsor":"Sponsor B"}],"total":5,"offset":0,"limit":2}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		response, err := client.MLWH().ListStudies(context.Background(), PageOptions{
			Page:      1,
			PageSize:  2,
			SortField: "name",
			SortOrder: "desc",
		})

		Convey("when ListStudies is called, then it decodes the page and query options correctly", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/mlwh/studies")
			So(requestedPage, ShouldEqual, "1")
			So(requestedPageSize, ShouldEqual, "2")
			So(requestedSnakePageSize, ShouldBeBlank)
			So(requestedSortField, ShouldEqual, "name")
			So(requestedSnakeSortField, ShouldBeBlank)
			So(requestedSortOrder, ShouldEqual, "desc")
			So(requestedSnakeSortOrder, ShouldBeBlank)
			So(response, ShouldNotBeNil)
			So(response.Items, ShouldHaveLength, 2)
			So(response.Total, ShouldNotBeNil)
			So(*response.Total, ShouldEqual, 5)
		})
	})

	Convey("Given a mock server returning an empty studies page", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":50}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		response, err := client.MLWH().ListStudies(context.Background(), PageOptions{})

		Convey("when ListStudies is called, then it returns an empty non-nil slice and total zero", func() {
			So(err, ShouldBeNil)
			So(response, ShouldNotBeNil)
			So(response.Items, ShouldNotBeNil)
			So(response.Items, ShouldHaveLength, 0)
			So(response.Total, ShouldNotBeNil)
			So(*response.Total, ShouldEqual, 0)
		})
	})
}

func TestMLWHAllStudies(t *testing.T) {
	Convey("Given a mock server returning three study pages", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("page") {
			case "", "1":
				_, _ = w.Write([]byte(`{"items":[{"id_study_tmp":1,"id_lims":"SQSCP","id_study_lims":"3361","name":"Study A"},{"id_study_tmp":2,"id_lims":"SQSCP","id_study_lims":"3362","name":"Study B"}],"total":5,"offset":0,"limit":2}`))
			case "2":
				_, _ = w.Write([]byte(`{"items":[{"id_study_tmp":3,"id_lims":"SQSCP","id_study_lims":"3363","name":"Study C"},{"id_study_tmp":4,"id_lims":"SQSCP","id_study_lims":"3364","name":"Study D"}],"total":5,"offset":2,"limit":2}`))
			case "3":
				_, _ = w.Write([]byte(`{"items":[{"id_study_tmp":5,"id_lims":"SQSCP","id_study_lims":"3365","name":"Study E"}],"total":5,"offset":4,"limit":2}`))
			default:
				_, _ = w.Write([]byte(`{"items":[],"total":5,"offset":6,"limit":2}`))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studies, err := client.MLWH().AllStudies(context.Background())

		Convey("when AllStudies is called, then it collects all study pages", func() {
			So(err, ShouldBeNil)
			So(studies, ShouldHaveLength, 5)
			So(studies[0].IDStudyLims, ShouldEqual, "3361")
			So(studies[4].IDStudyLims, ShouldEqual, "3365")
		})
	})

	Convey("Given a mock server returning page one and failing on page two", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "2" {
				http.Error(w, "boom", http.StatusInternalServerError)

				return
			}

			_, _ = w.Write([]byte(`{"items":[{"id_study_tmp":1,"id_lims":"SQSCP","id_study_lims":"3361","name":"Study A"},{"id_study_tmp":2,"id_lims":"SQSCP","id_study_lims":"3362","name":"Study B"}],"total":5,"offset":0,"limit":2}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studies, err := client.MLWH().AllStudies(context.Background())

		Convey("when AllStudies is called, then it returns partial studies alongside the pagination error", func() {
			So(err, ShouldNotBeNil)
			So(studies, ShouldHaveLength, 2)
			So(studies[0].IDStudyLims, ShouldEqual, "3361")
			So(fmt.Sprintf("%v", err), ShouldContainSubstring, "boom")
		})
	})
}

func TestMLWHGetStudy(t *testing.T) {
	Convey("Given a mock server returning a study for ID 3361", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`{"id_study_tmp":1,"id_lims":"SQSCP","id_study_lims":"3361","name":"IHTP_ISC_IBDCA_Edinburgh","faculty_sponsor":"David Adams"}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		study, err := client.MLWH().GetStudy(context.Background(), "3361")

		Convey("when GetStudy is called, then it requests the study endpoint and decodes the study", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/mlwh/studies/3361")
			So(study, ShouldNotBeNil)
			So(study.Name, ShouldEqual, "IHTP_ISC_IBDCA_Edinburgh")
			So(study.FacultySponsor, ShouldEqual, "David Adams")
		})
	})

	Convey("Given a mock server returning 404 for a study", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		study, err := client.MLWH().GetStudy(context.Background(), "3361")

		Convey("when GetStudy is called, then it returns ErrNotFound", func() {
			So(study, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrNotFound), ShouldBeTrue)
		})
	})
}

func TestMLWHListSamples(t *testing.T) {
	Convey("Given a mock server returning a samples page with a null total", t, func() {
		var requestedPath string
		var requestedPage string
		var requestedPageSize string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			requestedPage = r.URL.Query().Get("page")
			requestedPageSize = r.URL.Query().Get("pageSize")
			_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"3361","id_sample_lims":"S1","sanger_id":"sample-1","sample_name":"Sample 1","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"3361","id_sample_lims":"S2","sanger_id":"sample-2","sample_name":"Sample 2","id_run":102,"lane":2,"tag_index":11}],"total":null,"offset":0,"limit":2}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		response, err := client.MLWH().ListSamples(context.Background(), PageOptions{Page: 1, PageSize: 2})

		Convey("when ListSamples is called, then it decodes the page and preserves a nil total", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/mlwh/samples")
			So(requestedPage, ShouldEqual, "1")
			So(requestedPageSize, ShouldEqual, "2")
			So(response, ShouldNotBeNil)
			So(response.Items, ShouldHaveLength, 2)
			So(response.Total, ShouldBeNil)
			So(response.Items[0].IDSampleLims, ShouldEqual, "S1")
		})
	})
}

func TestMLWHAllSamples(t *testing.T) {
	Convey("Given a mock server returning two sample rows and then an empty page with null totals", t, func() {
		requests := 0

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++

			switch r.URL.Query().Get("page") {
			case "", "1":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"3361","id_sample_lims":"S1","sanger_id":"sample-1","sample_name":"Sample 1","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"3361","id_sample_lims":"S2","sanger_id":"sample-2","sample_name":"Sample 2","id_run":102,"lane":2,"tag_index":11}],"total":null,"offset":0,"limit":2}`))
			case "2":
				_, _ = w.Write([]byte(`{"items":[],"total":null,"offset":2,"limit":2}`))
			default:
				t.Fatalf("unexpected page request: %s", r.URL.Query().Get("page"))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.MLWH().AllSamples(context.Background())

		Convey("when AllSamples is called, then it keeps fetching until an empty page is returned", func() {
			So(err, ShouldBeNil)
			So(requests, ShouldEqual, 2)
			So(samples, ShouldHaveLength, 2)
			So(samples[0].IDSampleLims, ShouldEqual, "S1")
			So(samples[1].IDSampleLims, ShouldEqual, "S2")
		})
	})

	Convey("Given a mock server returning the same sample twice for different runs", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("page") {
			case "", "1":
				_, _ = w.Write([]byte(`{"items":[{"id_study_lims":"3361","id_sample_lims":"S1","sanger_id":"sample-1","sample_name":"Sample 1","id_run":101,"lane":1,"tag_index":10},{"id_study_lims":"3361","id_sample_lims":"S1","sanger_id":"sample-1","sample_name":"Sample 1","id_run":202,"lane":2,"tag_index":20}],"total":null,"offset":0,"limit":2}`))
			case "2":
				_, _ = w.Write([]byte(`{"items":[],"total":null,"offset":2,"limit":2}`))
			default:
				t.Fatalf("unexpected page request: %s", r.URL.Query().Get("page"))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.MLWH().AllSamples(context.Background())

		Convey("when AllSamples is called, then it returns both rows without deduplicating", func() {
			So(err, ShouldBeNil)
			So(samples, ShouldHaveLength, 2)
			So(samples[0].IDRun, ShouldEqual, 101)
			So(samples[1].IDRun, ShouldEqual, 202)
			So(samples[0].IDSampleLims, ShouldEqual, samples[1].IDSampleLims)
		})
	})
}

func TestMLWHListFacultySponsors(t *testing.T) {
	Convey("Given a mock server returning two faculty sponsors", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`{"items":[{"name":"David Adams"},{"name":"Sarah Teichmann"}],"total":2,"offset":0,"limit":2}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		response, err := client.MLWH().ListFacultySponsors(context.Background(), PageOptions{})

		Convey("when ListFacultySponsors is called, then it returns two sponsor entries", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/mlwh/faculty_sponsors")
			So(response, ShouldNotBeNil)
			So(response.Items, ShouldHaveLength, 2)
			So(response.Items[0].Name, ShouldEqual, "David Adams")
			So(response.Items[1].Name, ShouldEqual, "Sarah Teichmann")
		})
	})
}

func TestMLWHListProgrammes(t *testing.T) {
	Convey("Given a mock server returning three programmes", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"name":"Cell Genetics"},{"name":"Human Cell Atlas"},{"name":"Tree of Life"}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		programmes, err := client.MLWH().ListProgrammes(context.Background())

		Convey("when ListProgrammes is called, then it returns all programme names from the array response", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/mlwh/programmes")
			So(programmes, ShouldHaveLength, 3)
			So(programmes[0].Name, ShouldEqual, "Cell Genetics")
			So(programmes[1].Name, ShouldEqual, "Human Cell Atlas")
			So(programmes[2].Name, ShouldEqual, "Tree of Life")
		})
	})
}

func TestMLWHListDataReleaseStrategies(t *testing.T) {
	Convey("Given a mock server returning two data release strategies", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"name":"managed"},{"name":"open"}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		strategies, err := client.MLWH().ListDataReleaseStrategies(context.Background())

		Convey("when ListDataReleaseStrategies is called, then it returns all strategy names from the array response", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/mlwh/data_release_strategies")
			So(strategies, ShouldHaveLength, 2)
			So(strategies[0].Name, ShouldEqual, "managed")
			So(strategies[1].Name, ShouldEqual, "open")
		})
	})
}
