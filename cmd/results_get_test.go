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

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/results"
)

type resultSetWithFilesForTest struct {
	results.ResultSet
	Files []results.FileEntry `json:"files"`
}

func TestResultsGetCommand(t *testing.T) {
	installPassthroughResultsAuthClientForTest(t)

	convey.Convey("defaultResultsServerURL derives the results server URL from the active dev port and ignores bind hosts", t, func() {
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_RESULTS_PORT", "3672")
		t.Setenv("WA_RESULTS_SERVER_URL", "")
		t.Setenv("WA_RESULTS_BACKEND_URL", "")

		convey.So(defaultResultsServerURL(), convey.ShouldEqual, "https://127.0.0.1:3672")
	})

	convey.Convey("defaultResultsServerURL uses WA_RESULTS_SERVER_URL as the full client URL override", t, func() {
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_RESULTS_PORT", "3672")
		t.Setenv("WA_RESULTS_SERVER_URL", "https://dev-host.example.org:3672/wa-api")
		t.Setenv("WA_RESULTS_BACKEND_URL", "https://frontend-backend.example.org:9443/ignored")

		convey.So(defaultResultsServerURL(), convey.ShouldEqual, "https://dev-host.example.org:3672/wa-api")
	})

	convey.Convey("defaultResultsServerURL keeps an origin-only WA_RESULTS_BACKEND_URL as a lower-precedence compatibility default", t, func() {
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_RESULTS_PORT", "3672")
		t.Setenv("WA_RESULTS_SERVER_URL", "")
		t.Setenv("WA_RESULTS_BACKEND_URL", "https://frontend-backend.example.org:9443")

		convey.So(defaultResultsServerURL(), convey.ShouldEqual, "https://frontend-backend.example.org:9443")
	})

	convey.Convey("defaultResultsServerURL normalises a path-prefixed WA_RESULTS_BACKEND_URL to an auth-safe origin", t, func() {
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_RESULTS_PORT", "3672")
		t.Setenv("WA_RESULTS_SERVER_URL", "")
		t.Setenv("WA_RESULTS_BACKEND_URL", "https://frontend-backend.example.org:9443/wa-api")

		serverURL := defaultResultsServerURL()
		authAddr, err := resultsAuthAddr(serverURL)

		convey.So(serverURL, convey.ShouldEqual, "https://frontend-backend.example.org:9443")
		convey.So(err, convey.ShouldBeNil)
		convey.So(authAddr, convey.ShouldEqual, "frontend-backend.example.org:9443")
	})

	convey.Convey("defaultResultsServerURL ignores malformed WA_RESULTS_BACKEND_URL values before falling back", t, func() {
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_RESULTS_PORT", "3672")
		t.Setenv("WA_RESULTS_SERVER_URL", "")
		t.Setenv("WA_RESULTS_BACKEND_URL", "://missing-scheme")

		convey.So(defaultResultsServerURL(), convey.ShouldEqual, "https://127.0.0.1:3672")
	})

	convey.Convey("defaultResultsServerURL ignores non-HTTPS WA_RESULTS_BACKEND_URL values before falling back", t, func() {
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_RESULTS_PORT", "3672")
		t.Setenv("WA_RESULTS_SERVER_URL", "")
		t.Setenv("WA_RESULTS_BACKEND_URL", "http://frontend-backend.example.org:9443/wa-api")

		convey.So(defaultResultsServerURL(), convey.ShouldEqual, "https://127.0.0.1:3672")
	})

	convey.Convey("defaultResultsServerURL derives the results server URL from the active prod port", t, func() {
		t.Setenv("WA_ENV", "production")
		t.Setenv("WA_PROD_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_PROD_RESULTS_PORT", "8090")
		t.Setenv("WA_RESULTS_SERVER_URL", "")
		t.Setenv("WA_RESULTS_BACKEND_URL", "")

		convey.So(defaultResultsServerURL(), convey.ShouldEqual, "https://127.0.0.1:8090")
	})

	convey.Convey("defaultResultsServerURL ignores unrelated server env vars and falls back to localhost when no active results port is set", t, func() {
		legacyServerEnvVar := "WA" + "_SERVER" + "_URL"
		t.Setenv(legacyServerEnvVar, "http://legacy.example:9999")
		t.Setenv("WA_ENV", "")
		t.Setenv("WA_TEST_RESULTS_PORT", "")
		t.Setenv("WA_DEV_RESULTS_PORT", "")
		t.Setenv("WA_PROD_RESULTS_PORT", "")
		t.Setenv("WA_RESULTS_SERVER_URL", "")
		t.Setenv("WA_RESULTS_BACKEND_URL", "")

		convey.So(defaultResultsServerURL(), convey.ShouldEqual, "https://localhost:8080")
	})

	convey.Convey("results get falls back to the active scenario results port when --server is unset", t, func() {
		result := testResultSetForCommand()
		server := newResultsGetTLSServerForTest(result, nil)
		defer server.Close()
		installResultsHTTPClientForTest(t, server.Client())

		serverURL, err := url.Parse(server.URL)
		convey.So(err, convey.ShouldBeNil)

		t.Setenv("WA_ENV", "test")
		t.Setenv("WA_TEST_RESULTS_PORT", serverURL.Port())
		t.Setenv("WA_RESULTS_SERVER_URL", "")
		t.Setenv("WA_RESULTS_BACKEND_URL", "")

		output, err := executeRootCommandForTest(t, []string{"results", "get", result.ID})

		convey.So(err, convey.ShouldBeNil)

		var got results.ResultSet
		err = json.Unmarshal([]byte(output), &got)
		convey.So(err, convey.ShouldBeNil)
		convey.So(got, convey.ShouldResemble, result)
	})

	convey.Convey("results get keeps explicit --server ahead of WA_RESULTS_SERVER_URL", t, func() {
		result := testResultSetForCommand()
		server := newResultsGetServerForTest(result, nil)
		defer server.Close()

		t.Setenv("WA_RESULTS_SERVER_URL", "https://unreachable.example.invalid:9443")
		t.Setenv("WA_RESULTS_BACKEND_URL", "")

		output, err := executeRootCommandForTest(t, []string{"results", "get", "--server", server.URL, result.ID})

		convey.So(err, convey.ShouldBeNil)

		var got results.ResultSet
		err = json.Unmarshal([]byte(output), &got)
		convey.So(err, convey.ShouldBeNil)
		convey.So(got, convey.ShouldResemble, result)
	})

	convey.Convey("G3.1: Given a valid ID, when get <id> is run, then stdout is valid JSON with the result set", t, func() {
		result := testResultSetForCommand()
		server := newResultsGetServerForTest(result, nil)
		defer server.Close()

		output, err := executeRootCommandForTest(t, []string{"results", "get", "--server", server.URL, result.ID})

		convey.So(err, convey.ShouldBeNil)

		var got results.ResultSet
		err = json.Unmarshal([]byte(output), &got)
		convey.So(err, convey.ShouldBeNil)
		convey.So(got, convey.ShouldResemble, result)
	})

	convey.Convey("G3.2: Given get <id> --files, then the JSON includes a files array", t, func() {
		result := testResultSetForCommand()
		files := []results.FileEntry{{
			Path:  "/tmp/results/run/out.txt",
			Mtime: time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC),
			Size:  123,
			Kind:  "output",
		}}
		server := newResultsGetServerForTest(result, files)
		defer server.Close()

		output, err := executeRootCommandForTest(t, []string{"results", "get", "--server", server.URL, "--files", result.ID})

		convey.So(err, convey.ShouldBeNil)

		var got resultSetWithFilesForTest
		err = json.Unmarshal([]byte(output), &got)
		convey.So(err, convey.ShouldBeNil)
		convey.So(got.ResultSet, convey.ShouldResemble, result)
		convey.So(got.Files, convey.ShouldResemble, files)
	})

	convey.Convey("G3.3: Given non-existent ID, then exit code is non-zero", t, func() {
		server := newResultsGetServerForTest(testResultSetForCommand(), nil)
		defer server.Close()

		_, err := executeRootCommandForTest(t, []string{"results", "get", "--server", server.URL, "missing-id"})

		convey.So(err, convey.ShouldNotBeNil)
	})
}

func testResultSetForCommand() results.ResultSet {
	timestamp := time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC)

	return results.ResultSet{
		ID:                 results.CompositeKeyID("pipe", "runid=48522"),
		PipelineIdentifier: "pipe",
		RunKey:             "runid=48522",
		Requester:          "alice",
		Operator:           "bob",
		Command:            "nextflow run pipe",
		PipelineName:       "nf-pipe",
		PipelineVersion:    "1.2.3",
		OutputDirectory:    "/tmp/results/run",
		Metadata:           map[string]string{"library": "exon"},
		CreatedAt:          timestamp,
		UpdatedAt:          timestamp,
	}
}

func newResultsGetTLSServerForTest(result results.ResultSet, files []results.FileEntry) *httptest.Server {
	return httptest.NewTLSServer(newResultsGetHandlerForTest(result, files))
}

func newResultsGetHandlerForTest(result results.ResultSet, files []results.FileEntry) http.Handler {
	handler := http.NewServeMux()
	handler.HandleFunc(gas.EndPointAuth+"/results/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case gas.EndPointAuth + "/results/" + result.ID:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
		case gas.EndPointAuth + "/results/" + result.ID + "/files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(files)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": `results: not found: result set "missing-id"`})
		}
	})

	return handler
}

func newResultsGetServerForTest(result results.ResultSet, files []results.FileEntry) *httptest.Server {
	return httptest.NewServer(newResultsGetHandlerForTest(result, files))
}

func TestResultsGetEndpointPermissions(t *testing.T) {
	convey.Convey("D2.2: Given no token and no terminal, get fails with authentication failed", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)
		installGasResultsClientCLIForTest(t, &resultsAuthPasswordHandler{terminal: false})

		result := testResultSetForCommand()
		server := newResultsGetTLSServerForTest(result, nil)
		defer server.Close()
		installResultsHTTPClientForTest(t, server.Client())

		_, err := executeRootCommandForTest(t, []string{"results", "get", "--server", server.URL, result.ID})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldEqual, "authentication failed")
	})

	convey.Convey("D2.3: Given an authenticated user lacking access, get preserves locked 403 messaging", t, func() {
		restore := installPassthroughResultsAuthClientForTest(t)
		defer restore()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != gas.EndPointAuth+"/results/result-locked" {
				http.NotFound(w, r)

				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"locked","locked":true,"result_id":"result-locked","message":"You do not have access to this result set"}`))
		}))
		defer server.Close()

		_, err := executeRootCommandForTest(t, []string{"results", "get", "--server", server.URL, "result-locked"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldEqual, "results server returned 403: locked")
	})

	convey.Convey("D2.4: Given access, get --files fetches authenticated detail and files endpoints", t, func() {
		restore := installPassthroughResultsAuthClientForTest(t)
		defer restore()

		result := testResultSetForCommand()
		files := []results.FileEntry{{
			Path:  "/tmp/results/run/out.txt",
			Mtime: time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC),
			Size:  123,
			Kind:  "output",
		}}
		pathsCh := make(chan string, 2)
		authHeaderCh := make(chan string, 2)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pathsCh <- r.URL.Path
			authHeaderCh <- r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")

			switch r.URL.Path {
			case gas.EndPointAuth + "/results/" + result.ID:
				_ = json.NewEncoder(w).Encode(result)
			case gas.EndPointAuth + "/results/" + result.ID + "/files":
				_ = json.NewEncoder(w).Encode(files)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		output, err := executeRootCommandForTest(t, []string{"results", "get", "--server", server.URL, "--files", result.ID})

		convey.So(err, convey.ShouldBeNil)

		var got resultSetWithFilesForTest
		err = json.Unmarshal([]byte(output), &got)
		convey.So(err, convey.ShouldBeNil)
		convey.So(got.ResultSet, convey.ShouldResemble, result)
		convey.So(got.Files, convey.ShouldResemble, files)
		convey.So(<-pathsCh, convey.ShouldEqual, gas.EndPointAuth+"/results/"+result.ID)
		convey.So(<-pathsCh, convey.ShouldEqual, gas.EndPointAuth+"/results/"+result.ID+"/files")
		convey.So(<-authHeaderCh, convey.ShouldEqual, "Bearer test-jwt")
		convey.So(<-authHeaderCh, convey.ShouldEqual, "Bearer test-jwt")
	})
}
