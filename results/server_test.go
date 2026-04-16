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

package results

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestServerPostResults(t *testing.T) {
	convey.Convey("E1.1: Given an empty store and valid Registration JSON, when POST /results is called, then status is 201 with JSON result fields and application/json content type", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil)

		response := performResultsRequestForTest(t, server.Handler(), http.MethodPost, "/results", mustJSONBodyForTest(t, testRegistration()))

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)

		convey.So(result.Requester, convey.ShouldEqual, "alice")
		convey.So(result.CreatedAt.IsZero(), convey.ShouldBeFalse)
		convey.So(regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(result.ID), convey.ShouldBeTrue)
	})

	convey.Convey("E1.2: Given the same Registration POSTed twice, then the second response status is 200 and created_at matches the first", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil)
		body := mustJSONBodyForTest(t, testRegistration())

		firstResponse := performResultsRequestForTest(t, server.Handler(), http.MethodPost, "/results", body)
		secondResponse := performResultsRequestForTest(t, server.Handler(), http.MethodPost, "/results", body)

		var firstResult ResultSet
		var secondResult ResultSet
		decodeJSONResponseForTest(t, firstResponse, &firstResult)
		decodeJSONResponseForTest(t, secondResponse, &secondResult)

		convey.So(firstResponse.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(secondResponse.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(secondResult.CreatedAt, convey.ShouldEqual, firstResult.CreatedAt)
	})

	convey.Convey("E1.3: Given seqmeta_runid metadata and a validator returning the correct type, when POSTed, then status is 201", t, func() {
		store := newSQLiteStoreForTest(t)
		seqmeta := newSeqmetaServerForTest(map[string]seqmetaResponseForTest{
			"48522": {status: http.StatusOK, body: `{"identifier":"48522","type":"run_id","object":{}}`},
		})
		defer seqmeta.Close()

		validator := NewSeqmetaValidator(seqmeta.URL, time.Second)
		reg := testRegistration()
		reg.Metadata = map[string]string{"seqmeta_runid": "48522"}

		response := performResultsRequestForTest(t, NewServer(store, validator).Handler(), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
	})

	convey.Convey("E1.4: Given seqmeta returns the wrong type, then status is 422 with an error body", t, func() {
		store := newSQLiteStoreForTest(t)
		seqmeta := newSeqmetaServerForTest(map[string]seqmetaResponseForTest{
			"48522": {status: http.StatusOK, body: `{"identifier":"48522","type":"study_id","object":{}}`},
		})
		defer seqmeta.Close()

		validator := NewSeqmetaValidator(seqmeta.URL, time.Second)
		reg := testRegistration()
		reg.Metadata = map[string]string{"seqmeta_runid": "48522"}

		response := performResultsRequestForTest(t, NewServer(store, validator).Handler(), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusUnprocessableEntity)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldNotBeBlank)
	})

	convey.Convey("E1.5: Given seqmeta is unreachable, then status is 502", t, func() {
		store := newSQLiteStoreForTest(t)
		validator := NewSeqmetaValidator("http://127.0.0.1:1", 50*time.Millisecond)
		reg := testRegistration()
		reg.Metadata = map[string]string{"seqmeta_runid": "48522"}

		response := performResultsRequestForTest(t, NewServer(store, validator).Handler(), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadGateway)
	})

	convey.Convey("E1.6: Given Registration missing pipeline_identifier, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.PipelineIdentifier = ""

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
	})

	convey.Convey("E1.7: Given a malformed JSON body, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodPost, "/results", []byte(`{"pipeline_identifier":`))

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
	})
}

func TestServerGetResults(t *testing.T) {
	convey.Convey("E2.1: Given 2 stored result sets with requesters alice and bob, when GET /results?user=alice, then status 200 and the JSON array has 1 element with requester alice", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-alice", func(reg *Registration) {}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-bob", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Requester = "bob"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results?user=alice", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].Requester, convey.ShouldEqual, "alice")
	})

	convey.Convey("E2.2: Given GET /results?meta_library=exon, then only result sets with metadata key library equal to exon are returned", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-exon", func(reg *Registration) {
			reg.Metadata = map[string]string{"library": "exon", "study": "alpha"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-intron", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"library": "intron", "study": "alpha"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results?meta_library=exon", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].Metadata, convey.ShouldResemble, map[string]string{"library": "exon", "study": "alpha"})
	})

	convey.Convey("E2.3: Given GET /results?output_dir_prefix=/lustre/scratch, then only result sets whose output_directory starts with that prefix are returned", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-prefix-match", func(reg *Registration) {
			reg.OutputDirectory = "/lustre/scratch/project-a/run-1"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-prefix-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.OutputDirectory = "/lustre/archive/project-b/run-2"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results?output_dir_prefix=/lustre/scratch", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].OutputDirectory, convey.ShouldEqual, "/lustre/scratch/project-a/run-1")
	})

	convey.Convey("E2.4: Given GET /results with no params, then all result sets are returned", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-1", func(reg *Registration) {}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-2", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 2)
	})

	convey.Convey("E2.5: Given no matches, then status 200 and the body is an empty JSON array", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-1", func(reg *Registration) {}))

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results?user=nobody", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, "[]\n")
	})

	convey.Convey("GET /results parses seqmeta_X query parameters as exact metadata filters", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-seqmeta-match", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_runid": "48522", "library": "exon"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-seqmeta-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_runid": "99999", "library": "exon"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results?seqmeta_runid=48522", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].Metadata["seqmeta_runid"], convey.ShouldEqual, "48522")
	})
}

func TestServerDeleteResult(t *testing.T) {
	convey.Convey("E6.1: Given a stored result set, when DELETE /results/{id} is called, then status is 204 and subsequent GET /results/{id} returns 404", t, func() {
		store := newSQLiteStoreForTest(t)
		result, err := store.Upsert(t.Context(), testRegistration())
		convey.So(err, convey.ShouldBeNil)

		server := NewServer(store, nil)

		deleteResponse := performResultsRequestForTest(t, server.Handler(), http.MethodDelete, "/results/"+result.ID, nil)
		getResponse := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+result.ID, nil)

		convey.So(deleteResponse.Code, convey.ShouldEqual, http.StatusNoContent)
		convey.So(deleteResponse.Body.Len(), convey.ShouldEqual, 0)
		convey.So(getResponse.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(errorResponseBodyForTest(t, getResponse), convey.ShouldNotBeBlank)
	})

	convey.Convey("E6.2: Given a non-existent ID, when DELETE /results/{id} is called, then status is 404", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil)

		response := performResultsRequestForTest(t, server.Handler(), http.MethodDelete, "/results/missing-id", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldNotBeBlank)
	})
}

func TestServerGetResultByID(t *testing.T) {
	convey.Convey("E3.1: Given a stored result set, when GET /results/<valid-id> is called, then status is 200 and body matches the stored data including metadata", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := t.Context()
		reg := testRegistration()
		reg.Metadata = map[string]string{"library": "exon", "study": "alpha"}

		stored, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results/"+stored.ID, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)

		convey.So(result, convey.ShouldResemble, *stored)
	})

	convey.Convey("E3.2: Given a non-existent ID, when GET /results/<id> is called, then status is 404 with an error key", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results/missing-id", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldEqual, ErrNotFound.Error()+`: result set "missing-id"`)
	})
}

func TestServerGetResultFiles(t *testing.T) {
	convey.Convey("E4.1: Given a result set with 5 files (3 output, 1 input, 1 pipeline), when GET /results/{id}/files, then status 200 and JSON array has 5 entries with correct kind values", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.Files = []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "output"},
			{Path: "/tmp/results/run/out-3.txt", Mtime: time.Date(2026, time.April, 1, 12, 3, 0, 0, time.UTC), Size: 404, Kind: "output"},
			{Path: "/tmp/pipeline.nf", Mtime: time.Date(2026, time.April, 1, 11, 59, 0, 0, time.UTC), Size: 505, Kind: "pipeline"},
		}

		result, err := store.Upsert(t.Context(), reg)
		convey.So(err, convey.ShouldBeNil)

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results/"+result.ID+"/files", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var files []FileEntry
		decodeJSONResponseForTest(t, response, &files)

		convey.So(files, convey.ShouldResemble, []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "input"},
			{Path: "/tmp/pipeline.nf", Mtime: time.Date(2026, time.April, 1, 11, 59, 0, 0, time.UTC), Size: 505, Kind: "pipeline"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "output"},
			{Path: "/tmp/results/run/out-3.txt", Mtime: time.Date(2026, time.April, 1, 12, 3, 0, 0, time.UTC), Size: 404, Kind: "output"},
		})
	})

	convey.Convey("E4.2: Given non-existent ID, then status is 404", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil).Handler(), http.MethodGet, "/results/missing-id/files", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldEqual, ErrNotFound.Error()+`: result set "missing-id"`)
	})
}

func TestServerPutResultFiles(t *testing.T) {
	convey.Convey("E5.1: Given a result set with 2 output files and 1 input file, when PUT /results/{id}/files with 3 new output files, then status is 200 and stored files contain 4 entries with the input preserved", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.Files = []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "output"},
		}

		result, err := store.Upsert(context.Background(), reg)
		convey.So(err, convey.ShouldBeNil)

		server := NewServer(store, nil)

		replacement := []FileEntry{
			{Path: "/tmp/results/run/out-new-1.txt", Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC), Size: 404, Kind: "output"},
			{Path: "/tmp/results/run/out-new-2.txt", Mtime: time.Date(2026, time.April, 2, 12, 1, 0, 0, time.UTC), Size: 505, Kind: "output"},
			{Path: "/tmp/results/run/out-new-3.txt", Mtime: time.Date(2026, time.April, 2, 12, 2, 0, 0, time.UTC), Size: 606, Kind: "output"},
		}

		response := performResultsRequestForTest(
			t,
			server.Handler(),
			http.MethodPut,
			"/results/"+result.ID+"/files",
			mustJSONBodyForTest(t, replacement),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		getResponse := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+result.ID+"/files", nil)

		convey.So(getResponse.Code, convey.ShouldEqual, http.StatusOK)

		var files []FileEntry
		decodeJSONResponseForTest(t, getResponse, &files)
		convey.So(files, convey.ShouldResemble, []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "input"},
			{Path: "/tmp/results/run/out-new-1.txt", Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC), Size: 404, Kind: "output"},
			{Path: "/tmp/results/run/out-new-2.txt", Mtime: time.Date(2026, time.April, 2, 12, 1, 0, 0, time.UTC), Size: 505, Kind: "output"},
			{Path: "/tmp/results/run/out-new-3.txt", Mtime: time.Date(2026, time.April, 2, 12, 2, 0, 0, time.UTC), Size: 606, Kind: "output"},
		})
	})

	convey.Convey("E5.2: Given non-existent ID, when PUT /results/{id}/files is called, then status is 404", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(
			t,
			NewServer(store, nil).Handler(),
			http.MethodPut,
			"/results/missing-id/files",
			mustJSONBodyForTest(t, []FileEntry{{
				Path:  "/tmp/results/run/out-new-1.txt",
				Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC),
				Size:  505,
				Kind:  "output",
			}}),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldNotBeBlank)
	})

	convey.Convey("E5.3: Given malformed JSON, when PUT /results/{id}/files is called, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(
			t,
			NewServer(store, nil).Handler(),
			http.MethodPut,
			"/results/any-id/files",
			[]byte(`[{"path":`),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldNotBeBlank)
	})
}

func performResultsRequestForTest(t *testing.T, handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func mustJSONBodyForTest(t *testing.T, value any) []byte {
	t.Helper()

	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON body: %v", err)
	}

	return body
}

func errorResponseBodyForTest(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()

	var payload map[string]string
	decodeJSONResponseForTest(t, response, &payload)

	return payload["error"]
}

func decodeJSONResponseForTest(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.NewDecoder(bytes.NewReader(response.Body.Bytes())).Decode(target); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}
