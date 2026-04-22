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
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/results"
)

type resultSetWithFilesForTest struct {
	results.ResultSet
	Files []results.FileEntry `json:"files"`
}

func TestResultsGetCommand(t *testing.T) {
	convey.Convey("results get falls back to WA_RESULTS_BACKEND_URL when --server is unset", t, func() {
		result := testResultSetForCommand()
		server := newResultsGetServerForTest(result, nil)
		defer server.Close()

		t.Setenv("WA_SERVER_URL", "")
		t.Setenv("WA_RESULTS_BACKEND_URL", server.URL)

		output, err := executeRootCommandForTest(t, []string{"results", "get", result.ID})

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

func newResultsGetServerForTest(result results.ResultSet, files []results.FileEntry) *httptest.Server {
	handler := http.NewServeMux()
	handler.HandleFunc("/results/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/results/" + result.ID:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
		case "/results/" + result.ID + "/files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(files)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": `results: not found: result set "missing-id"`})
		}
	})

	return httptest.NewServer(handler)
}
