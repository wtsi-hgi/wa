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
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
)

type seqmetaStudySamplesResponseForTest struct {
	status  int
	samples []saga.MLWHSample
	body    string
}

func newSeqmetaStudySamplesServerForTest(responses map[string]seqmetaStudySamplesResponseForTest) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response, ok := responses[r.PathValue("id")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.status)

		if response.body != "" {
			_, _ = fmt.Fprint(w, response.body)

			return
		}

		_ = json.NewEncoder(w).Encode(response.samples)
	})

	mux := http.NewServeMux()
	mux.Handle("GET /study/{id}/samples", handler)

	return httptest.NewServer(mux)
}

func TestServerPostResults(t *testing.T) {
	convey.Convey("E1.1: Given an empty store and valid Registration JSON, when POST /results is called, then status is 201 with JSON result fields and application/json content type", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil, nil)

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
		server := NewServer(store, nil, nil)
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

		response := performResultsRequestForTest(t, NewServer(store, validator, nil).Handler(), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

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

		response := performResultsRequestForTest(t, NewServer(store, validator, nil).Handler(), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusUnprocessableEntity)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldNotBeBlank)
	})

	convey.Convey("E1.5: Given seqmeta is unreachable, then status is 502", t, func() {
		store := newSQLiteStoreForTest(t)
		validator := NewSeqmetaValidator("http://127.0.0.1:1", 50*time.Millisecond)
		reg := testRegistration()
		reg.Metadata = map[string]string{"seqmeta_runid": "48522"}

		response := performResultsRequestForTest(t, NewServer(store, validator, nil).Handler(), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadGateway)
	})

	convey.Convey("E1.6: Given Registration missing pipeline_identifier, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.PipelineIdentifier = ""

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
	})

	convey.Convey("E1.7: Given a malformed JSON body, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodPost, "/results", []byte(`{"pipeline_identifier":`))

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

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?user=alice", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].Requester, convey.ShouldEqual, "alice")
	})

	convey.Convey("Bug 3: Given GET /results?study=6568, then metadata aliases like seqmeta_studyid are matched by the combined Study field", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-alias-match", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_studyid": "6568"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-alias-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"study": "other"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?study=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-study-alias-match")
	})

	convey.Convey("Bug 3: Given GET /results?sample=SMP1001, then metadata aliases like seqmeta_sample_lims are matched by the combined Sample field", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-alias-match", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sample_lims": "SMP1001"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-alias-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1002"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?sample=SMP1001", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-sample-alias-match")
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

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?meta_library=exon", nil)

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

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?output_dir_prefix=/lustre/scratch", nil)

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

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 2)
	})

	convey.Convey("E2.5: Given no matches, then status 200 and the body is an empty JSON array", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-1", func(reg *Registration) {}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?user=nobody", nil)

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

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?seqmeta_runid=48522", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].Metadata["seqmeta_runid"], convey.ShouldEqual, "48522")
	})

	convey.Convey("D1.1: Given requesters alice, bob, and carol, when GET /results?user=alice&user=bob, then 2 matching results are returned", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-alice", func(reg *Registration) {}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-bob", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Requester = "bob"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-carol", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Requester = "carol"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?user=alice&user=bob", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So([]string{results[0].Requester, results[1].Requester}, convey.ShouldResemble, []string{"alice", "bob"})
	})

	convey.Convey("D1.2: Given user and pipeline_name filters, when GET /results is called, then different keys are ANDed", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-match", func(reg *Registration) {
			reg.Requester = "alice"
			reg.PipelineName = "nf"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-user-only", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Requester = "alice"
			reg.PipelineName = "other"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-pipeline-only", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Requester = "bob"
			reg.PipelineName = "nf"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?user=alice&pipeline_name=nf", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-match")
	})

	convey.Convey("D1.4: Given a single user filter, when GET /results is called, then behaviour matches the original single-value search", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-alice", func(reg *Registration) {}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-bob", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Requester = "bob"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?user=alice", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].Requester, convey.ShouldEqual, "alice")
	})

	convey.Convey("D1.5: Given no query params, when GET /results is called, then all result sets are returned", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-1", func(reg *Registration) {}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-2", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-3", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 3)
	})

	convey.Convey("E1.1: Given study_id=6568, when seqmeta resolves SANG1 and SANG2, then matching results are wrapped as SearchResult with matched_samples", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sang1", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sang3", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG3"}
		}))

		seqmeta := newSeqmetaStudySamplesServerForTest(map[string]seqmetaStudySamplesResponseForTest{
			"6568": {status: http.StatusOK, samples: []saga.MLWHSample{{SangerID: "SANG1"}, {SangerID: "SANG2"}}},
		})
		defer seqmeta.Close()

		resolver := NewSeqmetaSampleResolver(seqmeta.URL, time.Second)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study_id=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].ResultSet.Metadata["seqmeta_sampleid"], convey.ShouldEqual, "SANG1")
		convey.So(results[0].MatchedSamples, convey.ShouldResemble, []string{"SANG1"})
	})

	convey.Convey("E1.2: Given study_id combined with user=alice, then result sets must satisfy both filters", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-alice", func(reg *Registration) {
			reg.Requester = "alice"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-bob", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Requester = "bob"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1"}
		}))

		seqmeta := newSeqmetaStudySamplesServerForTest(map[string]seqmetaStudySamplesResponseForTest{
			"6568": {status: http.StatusOK, samples: []saga.MLWHSample{{SangerID: "SANG1"}}},
		})
		defer seqmeta.Close()

		resolver := NewSeqmetaSampleResolver(seqmeta.URL, time.Second)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study_id=6568&user=alice", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].ResultSet.Requester, convey.ShouldEqual, "alice")
	})

	convey.Convey("E1.3: Given seqmeta returns no samples for a study, then GET /results?study_id=6568 returns an empty array", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sang1", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1"}
		}))

		seqmeta := newSeqmetaStudySamplesServerForTest(map[string]seqmetaStudySamplesResponseForTest{
			"6568": {status: http.StatusOK, samples: []saga.MLWHSample{}},
		})
		defer seqmeta.Close()

		resolver := NewSeqmetaSampleResolver(seqmeta.URL, time.Second)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study_id=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, "[]\n")
	})

	convey.Convey("E1.4: Given study_id is requested without seqmeta configured, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?study_id=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldEqual, "seqmeta not configured")
	})

	convey.Convey("E1.5: Given seqmeta returns an error for the study lookup, then status is 502", t, func() {
		store := newSQLiteStoreForTest(t)
		seqmeta := newSeqmetaStudySamplesServerForTest(map[string]seqmetaStudySamplesResponseForTest{
			"6568": {status: http.StatusBadGateway, body: `{"error":"upstream failed"}`},
		})
		defer seqmeta.Close()

		resolver := NewSeqmetaSampleResolver(seqmeta.URL, time.Second)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study_id=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadGateway)
	})

	convey.Convey("E1.6: Given study_id combined with explicit seqmeta_sampleid, then the sample IDs are merged as a union", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sang1", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sang9", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG9"}
		}))

		seqmeta := newSeqmetaStudySamplesServerForTest(map[string]seqmetaStudySamplesResponseForTest{
			"6568": {status: http.StatusOK, samples: []saga.MLWHSample{{SangerID: "SANG1"}, {SangerID: "SANG2"}}},
		})
		defer seqmeta.Close()

		resolver := NewSeqmetaSampleResolver(seqmeta.URL, time.Second)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study_id=6568&seqmeta_sampleid=SANG9", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So([]string{results[0].ResultSet.Metadata["seqmeta_sampleid"], results[1].ResultSet.Metadata["seqmeta_sampleid"]}, convey.ShouldResemble, []string{"SANG1", "SANG9"})
	})

	convey.Convey("E1.7: Given seqmeta resolves three samples, then matched_samples contains only the identifiers that matched that result", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sang1", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1"}
		}))

		seqmeta := newSeqmetaStudySamplesServerForTest(map[string]seqmetaStudySamplesResponseForTest{
			"6568": {status: http.StatusOK, samples: []saga.MLWHSample{{SangerID: "SANG1"}, {SangerID: "SANG2"}, {SangerID: "SANG3"}}},
		})
		defer seqmeta.Close()

		resolver := NewSeqmetaSampleResolver(seqmeta.URL, time.Second)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study_id=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].MatchedSamples, convey.ShouldResemble, []string{"SANG1"})
	})

	convey.Convey("E1.8: Given GET /results?user=alice without study_id, then the response remains a plain []ResultSet payload", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-alice", func(reg *Registration) {
			reg.Requester = "alice"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?user=alice", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []map[string]any
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0]["id"], convey.ShouldNotBeNil)
		_, hasWrapper := results[0]["result_set"]
		_, hasMatchedSamples := results[0]["matched_samples"]
		convey.So(hasWrapper, convey.ShouldBeFalse)
		convey.So(hasMatchedSamples, convey.ShouldBeFalse)
	})

	convey.Convey("Bug 3: Given study=EGAS00001005445 with resolver returning samples, then results with those sample IDs are found", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-acc-match", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "ACC1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-acc-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "OTHER1"}
		}))

		seqmeta := newSeqmetaStudySamplesServerForTest(map[string]seqmetaStudySamplesResponseForTest{
			"EGAS00001005445": {status: http.StatusOK, samples: []saga.MLWHSample{{SangerID: "ACC1"}, {SangerID: "ACC2"}}},
		})
		defer seqmeta.Close()

		resolver := NewSeqmetaSampleResolver(seqmeta.URL, time.Second)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study=EGAS00001005445", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].ResultSet.Metadata["seqmeta_sampleid"], convey.ShouldEqual, "ACC1")
	})

	convey.Convey("Bug 3: Given study=6568 with resolver, both study-alias and resolver-matched results are found via OR logic", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-alias", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_studyid": "6568"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-resolved", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-unrelated", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG99"}
		}))

		seqmeta := newSeqmetaStudySamplesServerForTest(map[string]seqmetaStudySamplesResponseForTest{
			"6568": {status: http.StatusOK, samples: []saga.MLWHSample{{SangerID: "SANG1"}, {SangerID: "SANG2"}}},
		})
		defer seqmeta.Close()

		resolver := NewSeqmetaSampleResolver(seqmeta.URL, time.Second)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		runKeys := make([]string, len(results))
		for i, r := range results {
			runKeys[i] = r.ResultSet.RunKey
		}

		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(runKeys, convey.ShouldContain, "run-study-alias")
		convey.So(runKeys, convey.ShouldContain, "run-sample-resolved")
	})

	convey.Convey("Bug 3: Given seqmeta_studyid=6568 as direct param with resolver configured, hierarchical resolution is triggered and sarek-like results are found", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sarek-match", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sarek-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG99"}
		}))

		seqmeta := newSeqmetaStudySamplesServerForTest(map[string]seqmetaStudySamplesResponseForTest{
			"6568": {status: http.StatusOK, samples: []saga.MLWHSample{{SangerID: "SANG1"}, {SangerID: "SANG2"}}},
		})
		defer seqmeta.Close()

		resolver := NewSeqmetaSampleResolver(seqmeta.URL, time.Second)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_studyid=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].ResultSet.Metadata["seqmeta_sampleid"], convey.ShouldEqual, "SANG1")
	})
}

func TestServerGetStats(t *testing.T) {
	convey.Convey("B1.1/B1.5: Given stored and empty stats data, when GET /results/stats is called, then chi routes to the stats handler and returns aggregated JSON", t, func() {
		store := newSQLiteStoreForTest(t)
		now := time.Now().UTC()

		seedStatsResultSetForTest(t, store, "run-server-stats-1", now.Add(-time.Hour), func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-server-stats-1"
			reg.PipelineName = "nf-core/rnaseq"
			reg.Metadata = map[string]string{"library": "exon"}
		})
		seedStatsResultSetForTest(t, store, "run-server-stats-2", now.Add(-2*time.Hour), func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-server-stats-2"
			reg.PipelineName = "nf-core/sarek"
			reg.Metadata = map[string]string{"library": "intron"}
		})

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/stats", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var stats StatsResult
		decodeJSONResponseForTest(t, response, &stats)

		convey.So(stats.Total, convey.ShouldEqual, 2)
		convey.So(stats.Recent, convey.ShouldHaveLength, 2)
		convey.So(stats.Recent[0].RunKey, convey.ShouldEqual, "run-server-stats-1")
		convey.So(stats.Recent[0].Metadata, convey.ShouldResemble, map[string]string{"library": "exon"})
		convey.So(stats.Daily, convey.ShouldHaveLength, 30)
		convey.So(stats.Pipelines, convey.ShouldResemble, []PipelineCount{
			{PipelineName: "nf-core/rnaseq", Count: 1},
			{PipelineName: "nf-core/sarek", Count: 1},
		})

		emptyResponse := performResultsRequestForTest(t, NewServer(newSQLiteStoreForTest(t), nil, nil).Handler(), http.MethodGet, "/results/stats", nil)

		convey.So(emptyResponse.Code, convey.ShouldEqual, http.StatusOK)

		var emptyStats StatsResult
		decodeJSONResponseForTest(t, emptyResponse, &emptyStats)

		convey.So(emptyStats.Total, convey.ShouldEqual, 0)
		convey.So(emptyStats.Recent, convey.ShouldResemble, []ResultSet{})
		convey.So(emptyStats.Pipelines, convey.ShouldResemble, []PipelineCount{})
		convey.So(emptyStats.Daily, convey.ShouldHaveLength, 30)
	})

	convey.Convey("B1.2: Given 15 result sets, when GET /results/stats?recent=3, then only 3 recent results are returned while total remains 15", t, func() {
		store := newSQLiteStoreForTest(t)
		now := time.Now().UTC()

		for i := range 15 {
			seedStatsResultSetForTest(t, store, fmt.Sprintf("run-server-recent-%02d", i), now.Add(-time.Duration(i)*time.Hour), func(reg *Registration) {
				reg.PipelineIdentifier = fmt.Sprintf("pipe-server-recent-%02d", i)
			})
		}

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/stats?recent=3", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var stats StatsResult
		decodeJSONResponseForTest(t, response, &stats)

		convey.So(stats.Total, convey.ShouldEqual, 15)
		convey.So(stats.Recent, convey.ShouldHaveLength, 3)
		convey.So(stats.Recent[0].RunKey, convey.ShouldEqual, "run-server-recent-00")
	})

	convey.Convey("B1.7: Given invalid recent or days query params, when GET /results/stats is called, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil, nil)

		negativeRecent := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/stats?recent=-1", nil)
		invalidDays := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/stats?days=abc", nil)

		convey.So(negativeRecent.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, negativeRecent), convey.ShouldEqual, "invalid recent query parameter")
		convey.So(invalidDays.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, invalidDays), convey.ShouldEqual, "invalid days query parameter")
	})
}

func TestServerGetMetaKeys(t *testing.T) {
	convey.Convey("C1.1: Given result sets with metadata keys library, seqmeta_runid, and seqmeta_sampleid, when GET /results/meta-keys, then status 200 and the sorted JSON array is returned", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-meta-1", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_runid": "48522", "library": "exon"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-meta-2", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"library": "intron", "seqmeta_sampleid": "sample-1"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/meta-keys", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var keys []string
		decodeJSONResponseForTest(t, response, &keys)
		convey.So(keys, convey.ShouldResemble, []string{"library", "seqmeta_runid", "seqmeta_sampleid"})
	})

	convey.Convey("C1.2: Given no result sets, when GET /results/meta-keys, then status 200 and body is an empty JSON array", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/meta-keys", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, "[]\n")
	})

	convey.Convey("C1.3: Given two result sets both having key library, when GET /results/meta-keys, then library appears once", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-meta-1", func(reg *Registration) {
			reg.Metadata = map[string]string{"library": "exon"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-meta-2", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"library": "intron"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/meta-keys", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var keys []string
		decodeJSONResponseForTest(t, response, &keys)
		convey.So(keys, convey.ShouldResemble, []string{"library"})
	})
}

func TestServerDeleteResult(t *testing.T) {
	convey.Convey("E6.1: Given a stored result set, when DELETE /results/{id} is called, then status is 204 and subsequent GET /results/{id} returns 404", t, func() {
		store := newSQLiteStoreForTest(t)
		result, err := store.Upsert(t.Context(), testRegistration())
		convey.So(err, convey.ShouldBeNil)

		server := NewServer(store, nil, nil)

		deleteResponse := performResultsRequestForTest(t, server.Handler(), http.MethodDelete, "/results/"+result.ID, nil)
		getResponse := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+result.ID, nil)

		convey.So(deleteResponse.Code, convey.ShouldEqual, http.StatusNoContent)
		convey.So(deleteResponse.Body.Len(), convey.ShouldEqual, 0)
		convey.So(getResponse.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(errorResponseBodyForTest(t, getResponse), convey.ShouldNotBeBlank)
	})

	convey.Convey("E6.2: Given a non-existent ID, when DELETE /results/{id} is called, then status is 404", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil, nil)

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

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/"+stored.ID, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)

		convey.So(result, convey.ShouldResemble, *stored)
	})

	convey.Convey("E3.2: Given a non-existent ID, when GET /results/<id> is called, then status is 404 with an error key", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/missing-id", nil)

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

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/"+result.ID+"/files", nil)

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

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/missing-id/files", nil)

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

		server := NewServer(store, nil, nil)

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
			NewServer(store, nil, nil).Handler(),
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
			NewServer(store, nil, nil).Handler(),
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
