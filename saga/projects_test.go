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

func TestProjectsList(t *testing.T) {
	Convey("Given a mock server returning two projects", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"id":1,"name":"Cell Atlas"},{"id":2,"name":"Spatial Pilot"}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		projects, err := client.Projects().List(context.Background())

		Convey("when List is called, then it returns two projects with correct IDs and names", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/projects/")
			So(projects, ShouldHaveLength, 2)
			So(projects[0].ID, ShouldEqual, 1)
			So(projects[0].Name, ShouldEqual, "Cell Atlas")
			So(projects[1].ID, ShouldEqual, 2)
			So(projects[1].Name, ShouldEqual, "Spatial Pilot")
		})
	})
}

func TestProjectsAdd(t *testing.T) {
	Convey("Given a mock server accepting a project create request", t, func() {
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
			_, _ = w.Write([]byte(`{"id":1,"name":"proj"}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		project, err := client.Projects().Add(context.Background(), "proj")

		Convey("when Add is called, then it POSTs the project name and returns the created project", func() {
			So(err, ShouldBeNil)
			So(requestBodyErr, ShouldBeNil)
			So(requestedMethod, ShouldEqual, http.MethodPost)
			So(requestedPath, ShouldEqual, "/projects/")
			So(requestBody, ShouldEqual, `{"name":"proj"}`)
			So(project, ShouldNotBeNil)
			So(project.ID, ShouldEqual, 1)
			So(project.Name, ShouldEqual, "proj")
		})
	})
}

func TestProjectsAddInvalidatesListCache(t *testing.T) {
	Convey("Given a cached projects list", t, func() {
		var getRequests atomic.Int32
		var postRequests atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Path == "/projects/" {
					getRequests.Add(1)
				}

				_, _ = w.Write([]byte(`[{"id":1,"name":"proj"}]`))
			case http.MethodPost:
				postRequests.Add(1)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":2,"name":"new-proj"}`))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Projects().List(context.Background())
		So(err, ShouldBeNil)

		_, err = client.Projects().Add(context.Background(), "new-proj")
		So(err, ShouldBeNil)

		_, err = client.Projects().List(context.Background())

		Convey("when a project is added, then the cached projects list is invalidated", func() {
			So(err, ShouldBeNil)
			So(postRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}

func TestProjectsGet(t *testing.T) {
	Convey("Given a mock server returning project 1", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`{"id":1,"name":"Cell Atlas"}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		project, err := client.Projects().Get(context.Background(), 1)

		Convey("when Get is called, then it requests the project endpoint and decodes the project", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/projects/1")
			So(project, ShouldNotBeNil)
			So(project.ID, ShouldEqual, 1)
			So(project.Name, ShouldEqual, "Cell Atlas")
		})
	})

	Convey("Given a mock server returning 404 for a project", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		project, err := client.Projects().Get(context.Background(), 1)

		Convey("when Get is called, then it returns ErrNotFound", func() {
			So(project, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrNotFound), ShouldBeTrue)
		})
	})
}

func TestProjectsListSamples(t *testing.T) {
	Convey("Given a mock server returning three samples for a project", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"id":41,"sanger_id":"ABC123"},{"id":42,"sanger_id":"DEF456"},{"id":43,"sanger_id":"GHI789"}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.Projects().ListSamples(context.Background(), 1)

		Convey("when ListSamples is called, then it requests the project samples endpoint and returns three samples", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/projects/1/samples/")
			So(samples, ShouldHaveLength, 3)
			So(samples[0].ID, ShouldEqual, 41)
			So(samples[0].SangerID, ShouldEqual, "ABC123")
			So(samples[2].ID, ShouldEqual, 43)
			So(samples[2].SangerID, ShouldEqual, "GHI789")
		})
	})
}

func TestProjectsAddSampleInvalidatesSamplesCache(t *testing.T) {
	Convey("Given a cached project samples list", t, func() {
		var getRequests atomic.Int32
		var postRequests atomic.Int32
		var requestedPath string
		var requestedMethod string
		var requestBody string
		var requestBodyErr error

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Path == "/projects/1/samples/" {
					getRequests.Add(1)
				}

				_, _ = w.Write([]byte(`[{"id":41,"sanger_id":"ABC123"}]`))
			case http.MethodPost:
				postRequests.Add(1)
				requestedPath = r.URL.Path
				requestedMethod = r.Method

				body, err := io.ReadAll(r.Body)
				requestBodyErr = err
				requestBody = string(body)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":42,"sanger_id":"ABC123"}`))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Projects().ListSamples(context.Background(), 1)
		So(err, ShouldBeNil)

		sample, err := client.Projects().AddSample(context.Background(), 1, "ABC123")
		So(err, ShouldBeNil)

		_, err = client.Projects().ListSamples(context.Background(), 1)

		Convey("when AddSample is called, then it posts the sample ID, returns the added sample, and invalidates the project samples cache", func() {
			So(err, ShouldBeNil)
			So(requestBodyErr, ShouldBeNil)
			So(requestedMethod, ShouldEqual, http.MethodPost)
			So(requestedPath, ShouldEqual, "/projects/1/samples/")
			So(requestBody, ShouldEqual, `{"sanger_id":"ABC123"}`)
			So(sample, ShouldNotBeNil)
			So(sample.ID, ShouldEqual, 42)
			So(sample.SangerID, ShouldEqual, "ABC123")
			So(postRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}

func TestProjectsRemoveSampleInvalidatesSamplesCache(t *testing.T) {
	Convey("Given a cached project samples list", t, func() {
		var getRequests atomic.Int32
		var deleteRequests atomic.Int32
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Path == "/projects/1/samples/" {
					getRequests.Add(1)
				}

				_, _ = w.Write([]byte(`[{"id":42,"sanger_id":"ABC123"}]`))
			case http.MethodDelete:
				deleteRequests.Add(1)
				requestedPath = r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Projects().ListSamples(context.Background(), 1)
		So(err, ShouldBeNil)

		err = client.Projects().RemoveSample(context.Background(), 1, 42)
		So(err, ShouldBeNil)

		_, err = client.Projects().ListSamples(context.Background(), 1)

		Convey("when RemoveSample is called, then it deletes the project sample and invalidates the project samples cache", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/projects/1/samples/42")
			So(deleteRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}

func TestProjectsListStudies(t *testing.T) {
	Convey("Given a mock server returning two studies for a project", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"id":71,"id_study_lims":"study-a"},{"id":72,"id_study_lims":"study-b"}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		studies, err := client.Projects().ListStudies(context.Background(), 1)

		Convey("when ListStudies is called, then it requests the project studies endpoint and returns two studies", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/projects/1/studies/")
			So(studies, ShouldHaveLength, 2)
			So(studies[0].ID, ShouldEqual, 71)
			So(studies[0].IDStudyLims, ShouldEqual, "study-a")
			So(studies[1].ID, ShouldEqual, 72)
			So(studies[1].IDStudyLims, ShouldEqual, "study-b")
		})
	})
}

func TestProjectsAddStudyInvalidatesStudiesCache(t *testing.T) {
	Convey("Given a cached project studies list", t, func() {
		var getRequests atomic.Int32
		var postRequests atomic.Int32
		var requestedPath string
		var requestedMethod string
		var requestBody string
		var requestBodyErr error

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Path == "/projects/1/studies/" {
					getRequests.Add(1)
				}

				_, _ = w.Write([]byte(`[{"id":71,"id_study_lims":"study-a"}]`))
			case http.MethodPost:
				postRequests.Add(1)
				requestedPath = r.URL.Path
				requestedMethod = r.Method

				body, err := io.ReadAll(r.Body)
				requestBodyErr = err
				requestBody = string(body)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":72,"id_study_lims":"study-a"}`))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Projects().ListStudies(context.Background(), 1)
		So(err, ShouldBeNil)

		study, err := client.Projects().AddStudy(context.Background(), 1, "study-a")
		So(err, ShouldBeNil)

		_, err = client.Projects().ListStudies(context.Background(), 1)

		Convey("when AddStudy is called, then it posts the study ID, returns the added study, and invalidates the project studies cache", func() {
			So(err, ShouldBeNil)
			So(requestBodyErr, ShouldBeNil)
			So(requestedMethod, ShouldEqual, http.MethodPost)
			So(requestedPath, ShouldEqual, "/projects/1/studies/")
			So(requestBody, ShouldEqual, `{"id_study_lims":"study-a"}`)
			So(study, ShouldNotBeNil)
			So(study.ID, ShouldEqual, 72)
			So(study.IDStudyLims, ShouldEqual, "study-a")
			So(postRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}

func TestProjectsRemoveStudyInvalidatesStudiesCache(t *testing.T) {
	Convey("Given a cached project studies list", t, func() {
		var getRequests atomic.Int32
		var deleteRequests atomic.Int32
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Path == "/projects/1/studies/" {
					getRequests.Add(1)
				}

				_, _ = w.Write([]byte(`[{"id":72,"id_study_lims":"study-a"}]`))
			case http.MethodDelete:
				deleteRequests.Add(1)
				requestedPath = r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Projects().ListStudies(context.Background(), 1)
		So(err, ShouldBeNil)

		err = client.Projects().RemoveStudy(context.Background(), 1, 72)
		So(err, ShouldBeNil)

		_, err = client.Projects().ListStudies(context.Background(), 1)

		Convey("when RemoveStudy is called, then it deletes the project study and invalidates the project studies cache", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/projects/1/studies/72")
			So(deleteRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}

func TestProjectsListUsers(t *testing.T) {
	Convey("Given a mock server returning one user for a project", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"id":81,"username":"jdoe"}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		users, err := client.Projects().ListUsers(context.Background(), 1)

		Convey("when ListUsers is called, then it requests the project users endpoint and returns one user", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/projects/1/users/")
			So(users, ShouldHaveLength, 1)
			So(users[0].ID, ShouldEqual, 81)
			So(users[0].Username, ShouldEqual, "jdoe")
		})
	})
}

func TestProjectsAddUserInvalidatesUsersCache(t *testing.T) {
	Convey("Given a cached project users list", t, func() {
		var getRequests atomic.Int32
		var postRequests atomic.Int32
		var requestedPath string
		var requestedMethod string
		var requestBody string
		var requestBodyErr error

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Path == "/projects/1/users/" {
					getRequests.Add(1)
				}

				_, _ = w.Write([]byte(`[{"id":81,"username":"jdoe"}]`))
			case http.MethodPost:
				postRequests.Add(1)
				requestedPath = r.URL.Path
				requestedMethod = r.Method

				body, err := io.ReadAll(r.Body)
				requestBodyErr = err
				requestBody = string(body)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":82,"username":"asmith"}`))
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Projects().ListUsers(context.Background(), 1)
		So(err, ShouldBeNil)

		user, err := client.Projects().AddUser(context.Background(), 1, "asmith")
		So(err, ShouldBeNil)

		_, err = client.Projects().ListUsers(context.Background(), 1)

		Convey("when AddUser is called, then it posts the username, returns the added user, and invalidates the project users cache", func() {
			So(err, ShouldBeNil)
			So(requestBodyErr, ShouldBeNil)
			So(requestedMethod, ShouldEqual, http.MethodPost)
			So(requestedPath, ShouldEqual, "/projects/1/users/")
			So(requestBody, ShouldEqual, `{"username":"asmith"}`)
			So(user, ShouldNotBeNil)
			So(user.ID, ShouldEqual, 82)
			So(user.Username, ShouldEqual, "asmith")
			So(postRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}

func TestProjectsRemoveUserInvalidatesUsersCache(t *testing.T) {
	Convey("Given a cached project users list", t, func() {
		var getRequests atomic.Int32
		var deleteRequests atomic.Int32
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Path == "/projects/1/users/" {
					getRequests.Add(1)
				}

				_, _ = w.Write([]byte(`[{"id":82,"username":"asmith"}]`))
			case http.MethodDelete:
				deleteRequests.Add(1)
				requestedPath = r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.Projects().ListUsers(context.Background(), 1)
		So(err, ShouldBeNil)

		err = client.Projects().RemoveUser(context.Background(), 1, 82)
		So(err, ShouldBeNil)

		_, err = client.Projects().ListUsers(context.Background(), 1)

		Convey("when RemoveUser is called, then it deletes the project user and invalidates the project users cache", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/projects/1/users/82")
			So(deleteRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}
