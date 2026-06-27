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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/mlwh"
)

func (m *mockSearchExpander) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	m.classifyCalls = append(m.classifyCalls, raw)
	if m.classifyFunc != nil {
		return m.classifyFunc(ctx, raw)
	}

	return mlwh.Match{}, mlwh.ErrUnsupportedIdentifier
}

func newStaticMLWHSearchResolverForTest(responses map[mlwh.IdentifierKind]map[string]mlwh.SearchValues) *MLWHSearchResolver {
	return NewMLWHSearchResolver(&mockSearchExpander{
		searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
			if valuesByCanonical, ok := responses[kind]; ok {
				if values, ok := valuesByCanonical[canonical]; ok {
					return values, nil
				}
			}

			return mlwh.SearchValues{}, mlwh.ErrNotFound
		},
	})
}

func TestServerAnnotateAccessB3(t *testing.T) {
	forbiddenAccess := AccessState{
		CanView: false,
		Locked:  true,
		Reason:  "forbidden",
	}
	loginRequiredAccess := AccessState{
		CanView: false,
		Locked:  true,
		Reason:  "login_required",
	}

	convey.Convey("B3.1: Given two stored results, when anonymous GET /rest/v1/results is called, then both rows are returned with login-required access", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, accessRegistrationForTest("run-b3-public-one", "alice", "bob", 200))
		seedResultSetForTest(t, store, accessRegistrationForTest("run-b3-public-two", "carol", "dave", 300))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, gas.EndPointREST+"/results", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)
		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(resultAccessByRunKeyForTest(results)["run-b3-public-one"], convey.ShouldResemble, loginRequiredAccess)
		convey.So(resultAccessByRunKeyForTest(results)["run-b3-public-two"], convey.ShouldResemble, loginRequiredAccess)
	})

	convey.Convey("B3.2: Given user alice can access one of two results, when GET /rest/v1/auth/results is called, then both rows are returned with per-row access", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, accessRegistrationForTest("run-b3-auth-allowed", "alice", "bob", 200))
		seedResultSetForTest(t, store, accessRegistrationForTest("run-b3-auth-locked", "carol", "dave", 300))

		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})
		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)
		access := resultAccessByRunKeyForTest(results)
		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(access["run-b3-auth-allowed"], convey.ShouldResemble, AccessState{CanView: true})
		convey.So(access["run-b3-auth-locked"], convey.ShouldResemble, forbiddenAccess)
	})

	convey.Convey("B3.3: Given a study search that returns SearchResult, when the authenticated route is called, then nested result_set access is populated", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, studyAccessRegistrationForTest("run-b3-study-allowed", "SANG1", "alice", 200))
		seedResultSetForTest(t, store, studyAccessRegistrationForTest("run-b3-study-locked", "SANG2", "carol", 300))
		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {Samples: []string{"SANG1", "SANG2"}},
			},
		})

		router := newResultsGinHandlerForTest(t, NewServer(store, nil, resolver), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})
		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results?study_id=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)
		access := searchResultAccessByRunKeyForTest(results)
		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(access["run-b3-study-allowed"], convey.ShouldResemble, AccessState{CanView: true})
		convey.So(access["run-b3-study-locked"], convey.ShouldResemble, forbiddenAccess)
	})

	convey.Convey("B3.4: Given anonymous study search returns SearchResult, when GET /rest/v1/results is called, then all wrapped rows have login-required access", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, studyAccessRegistrationForTest("run-b3-public-study-one", "SANG1", "alice", 200))
		seedResultSetForTest(t, store, studyAccessRegistrationForTest("run-b3-public-study-two", "SANG2", "carol", 300))
		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {Samples: []string{"SANG1", "SANG2"}},
			},
		})

		response := performResultsRequestForTest(
			t,
			NewServer(store, nil, resolver).Handler(),
			http.MethodGet,
			gas.EndPointREST+"/results?study_id=6568",
			nil,
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)
		convey.So(results, convey.ShouldHaveLength, 2)
		for _, result := range results {
			convey.So(result.ResultSet.Access, convey.ShouldResemble, loginRequiredAccess)
		}
	})

	convey.Convey("B3.5: Given stats recent contains accessible and inaccessible rows, when GET /rest/v1/auth/results/stats is called, then recent access is correct and aggregates are unchanged", t, func() {
		store := newSQLiteStoreForTest(t)
		now := time.Now().UTC()
		seedStatsResultSetForTest(t, store, "run-b3-stats-allowed", now, func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-b3-stats-allowed"
			reg.Requester = "alice"
			reg.OutputDirectoryGID = gidForTest(200)
			reg.PipelineName = "nf-core/rnaseq"
		})
		seedStatsResultSetForTest(t, store, "run-b3-stats-locked", now.Add(-time.Hour), func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-b3-stats-locked"
			reg.Requester = "carol"
			reg.Operator = "dave"
			reg.OutputDirectoryGID = gidForTest(300)
			reg.PipelineName = "nf-core/sarek"
		})

		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})
		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results/stats?recent=2", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var stats StatsResult
		decodeJSONResponseForTest(t, response, &stats)
		access := resultAccessByRunKeyForTest(stats.Recent)
		convey.So(stats.Total, convey.ShouldEqual, 2)
		convey.So(stats.Recent, convey.ShouldHaveLength, 2)
		convey.So(stats.Pipelines, convey.ShouldResemble, []PipelineCount{
			{PipelineName: "nf-core/rnaseq", Count: 1},
			{PipelineName: "nf-core/sarek", Count: 1},
		})
		convey.So(access["run-b3-stats-allowed"], convey.ShouldResemble, AccessState{CanView: true})
		convey.So(access["run-b3-stats-locked"], convey.ShouldResemble, forbiddenAccess)
	})
}

func accessRegistrationForTest(runKey, requester, operator string, gid int64) *Registration {
	return searchRegistrationForTest(runKey, func(reg *Registration) {
		reg.PipelineIdentifier = "pipe-" + runKey
		reg.Requester = requester
		reg.Operator = operator
		reg.OutputDirectoryGID = gidForTest(gid)
	})
}

func normalizeResultsPathForTest(method, path string) string {
	if strings.HasPrefix(path, gas.EndPointREST) {
		return path
	}

	if !strings.HasPrefix(path, "/results") {
		return path
	}

	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete:
		return gas.EndPointAuth + path
	default:
		return gas.EndPointREST + path
	}
}

func resultAccessByRunKeyForTest(results []ResultSet) map[string]AccessState {
	access := make(map[string]AccessState, len(results))

	for _, result := range results {
		access[result.RunKey] = result.Access
	}

	return access
}

func studyAccessRegistrationForTest(runKey, sampleID, requester string, gid int64) *Registration {
	return searchRegistrationForTest(runKey, func(reg *Registration) {
		reg.PipelineIdentifier = "pipe-" + runKey
		reg.Requester = requester
		reg.Operator = "operator-" + requester
		reg.OutputDirectoryGID = gidForTest(gid)
		reg.Metadata = map[string]string{SeqmetaSampleNameKey: sampleID}
	})
}

func searchResultAccessByRunKeyForTest(results []SearchResult) map[string]AccessState {
	access := make(map[string]AccessState, len(results))

	for _, result := range results {
		access[result.ResultSet.RunKey] = result.ResultSet.Access
	}

	return access
}

type lockedResponseForTest struct {
	Error    string `json:"error"`
	Locked   bool   `json:"locked"`
	ResultID string `json:"result_id,omitempty"`
	Message  string `json:"message"`
}

func assertLockedResponseForTest(t *testing.T, response *httptest.ResponseRecorder, resultID string) {
	t.Helper()

	var locked lockedResponseForTest
	decodeJSONResponseForTest(t, response, &locked)
	convey.So(locked, convey.ShouldResemble, lockedResponseForTest{
		Error:    "locked",
		Locked:   true,
		ResultID: resultID,
		Message:  "You do not have access to this result set",
	})
}

type mockSearchExpander struct {
	expandCalls           int
	sampleOnlyCalls       int
	classifyCalls         []string
	classifyFunc          func(context.Context, string) (mlwh.Match, error)
	searchValuesFunc      func(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error)
	sampleNamesFunc       func(context.Context, mlwh.IdentifierKind, string) ([]string, error)
	sampleNameFunc        func(context.Context, string) (mlwh.Match, error)
	sampleFunc            func(context.Context, string) (mlwh.Match, error)
	resolveStudyFunc      func(context.Context, string) (mlwh.Match, error)
	resolveRunFunc        func(context.Context, string) (mlwh.Match, error)
	libraryFunc           func(context.Context, string) (mlwh.Match, error)
	libraryIdentifierFunc func(context.Context, string) (mlwh.Match, error)
	searchStudiesFunc     func(context.Context, string, int, int) ([]mlwh.Study, error)
	searchSamplesFunc     func(context.Context, string, int, int) ([]mlwh.Sample, error)
}

func (m *mockSearchExpander) ExpandSearchValues(ctx context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
	m.expandCalls++
	if m.searchValuesFunc == nil {
		return mlwh.SearchValues{}, nil
	}

	return m.searchValuesFunc(ctx, kind, canonical)
}

func (m *mockSearchExpander) ExpandSampleSearchValues(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]string, error) {
	m.sampleOnlyCalls++
	if m.sampleNamesFunc == nil {
		return nil, mlwh.ErrUnsupportedIdentifier
	}

	return m.sampleNamesFunc(ctx, kind, canonical)
}

func (m *mockSearchExpander) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	if m.resolveStudyFunc != nil {
		return m.resolveStudyFunc(ctx, raw)
	}

	return mlwh.Match{Kind: mlwh.KindStudyLimsID, Canonical: raw}, nil
}

func (m *mockSearchExpander) ResolveSampleName(ctx context.Context, raw string) (mlwh.Match, error) {
	if m.sampleNameFunc != nil {
		return m.sampleNameFunc(ctx, raw)
	}

	return mlwh.Match{}, mlwh.ErrUnsupportedIdentifier
}

func (m *mockSearchExpander) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	if m.sampleFunc != nil {
		return m.sampleFunc(ctx, raw)
	}

	return mlwh.Match{}, mlwh.ErrUnsupportedIdentifier
}

func (m *mockSearchExpander) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	if m.resolveRunFunc != nil {
		return m.resolveRunFunc(ctx, raw)
	}

	return mlwh.Match{Kind: mlwh.KindRunID, Canonical: raw}, nil
}

func (m *mockSearchExpander) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	if m.libraryFunc != nil {
		return m.libraryFunc(ctx, raw)
	}

	return mlwh.Match{Kind: mlwh.KindLibraryType, Canonical: raw}, nil
}

func (m *mockSearchExpander) ResolveLibraryIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	if m.libraryIdentifierFunc != nil {
		return m.libraryIdentifierFunc(ctx, raw)
	}

	return mlwh.Match{}, mlwh.ErrNotFound
}

func (m *mockSearchExpander) SearchStudies(ctx context.Context, term string, limit, offset int) ([]mlwh.Study, error) {
	if m.searchStudiesFunc != nil {
		return m.searchStudiesFunc(ctx, term, limit, offset)
	}

	return []mlwh.Study{}, nil
}

func (m *mockSearchExpander) SearchSamples(ctx context.Context, term string, limit, offset int) ([]mlwh.Sample, error) {
	if m.searchSamplesFunc != nil {
		return m.searchSamplesFunc(ctx, term, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func TestResolveRegistrationSample(t *testing.T) {
	convey.Convey("Given the sample-name fast path is unsupported, resolveRegistrationSample falls back to broad sample resolution", t, func() {
		sampleNameCalls := []string{}
		broadSampleCalls := []string{}
		resolver := &mockSearchExpander{
			sampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				sampleNameCalls = append(sampleNameCalls, raw)

				return mlwh.Match{}, mlwh.ErrUnsupportedIdentifier
			},
			sampleFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				broadSampleCalls = append(broadSampleCalls, raw)

				return mlwh.Match{
					Kind:      mlwh.KindSupplierName,
					Canonical: "7607STDY14643771",
					Sample: &mlwh.Sample{
						Name:         "7607STDY14643771",
						SupplierName: raw,
					},
				}, nil
			},
		}

		match, err := resolveRegistrationSample(context.Background(), resolver, "Hek_R1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, mlwh.KindSupplierName)
		convey.So(match.Canonical, convey.ShouldEqual, "7607STDY14643771")
		convey.So(sampleNameCalls, convey.ShouldResemble, []string{"Hek_R1"})
		convey.So(broadSampleCalls, convey.ShouldResemble, []string{"Hek_R1"})
	})

	convey.Convey("Given the sample-name fast path reports an unsynced cache, resolveRegistrationSample keeps it as a hard error", t, func() {
		broadSampleCalls := 0
		resolver := &mockSearchExpander{
			sampleNameFunc: func(context.Context, string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrCacheNeverSynced
			},
			sampleFunc: func(context.Context, string) (mlwh.Match, error) {
				broadSampleCalls++

				return mlwh.Match{}, errors.New("broad sample resolver should not run")
			},
		}

		_, err := resolveRegistrationSample(context.Background(), resolver, "Hek_R1")

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, mlwh.ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(broadSampleCalls, convey.ShouldEqual, 0)
	})
}

func mustRegistrationLookupBodyForTest(t *testing.T, reg *Registration, lookupValues map[string][]string) []byte {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(mustJSONBodyForTest(t, reg), &payload); err != nil {
		t.Fatalf("decode registration JSON body: %v", err)
	}

	payload["lookup_values"] = lookupValues

	return mustJSONBodyForTest(t, payload)
}

func resultSetCountForTest(t *testing.T, store *Store) int {
	t.Helper()

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM result_sets`).Scan(&count); err != nil {
		t.Fatalf("count result sets: %v", err)
	}

	return count
}

func TestServerProtectDetailAndFileListC1(t *testing.T) {
	convey.Convey("C1.1: Given no JWT and an existing result, when GET /rest/v1/results/<id> is called, then status is 403 with locked JSON", t, func() {
		store := newSQLiteStoreForTest(t)
		stored := seedResultSetForTest(t, store, accessRegistrationForTest("run-c1-public-detail", "alice", "carol", 200))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, gas.EndPointREST+"/results/"+stored.ID, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusForbidden)
		assertLockedResponseForTest(t, response, stored.ID)
	})

	convey.Convey("C1.2: Given no JWT and an existing result, when GET /rest/v1/results/<id>/files is called, then status is 403 and no file list is returned", t, func() {
		store := newSQLiteStoreForTest(t)
		stored := seedResultSetForTest(t, store, accessRegistrationForTest("run-c1-public-files", "alice", "carol", 200))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, gas.EndPointREST+"/results/"+stored.ID+"/files", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusForbidden)
		assertLockedResponseForTest(t, response, stored.ID)
	})

	convey.Convey("C1.4: Given no JWT and a missing result, when GET /rest/v1/results/missing is called, then status is 404", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, gas.EndPointREST+"/results/missing", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
	})

	convey.Convey("C1.5: Given user bob lacks access, when GET /rest/v1/auth/results/<id> is called, then status is 403 with locked JSON", t, func() {
		store := newSQLiteStoreForTest(t)
		stored := seedResultSetForTest(t, store, accessRegistrationForTest("run-c1-auth-detail-locked", "alice", "carol", 200))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "bob",
			User:     authUserForTest{gids: []string{"100"}},
		})

		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results/"+stored.ID, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusForbidden)
		assertLockedResponseForTest(t, response, stored.ID)
	})

	convey.Convey("C1.6: Given user bob lacks access, when GET /rest/v1/auth/results/<id>/files is called, then status is 403 and no file list is returned", t, func() {
		store := newSQLiteStoreForTest(t)
		stored := seedResultSetForTest(t, store, accessRegistrationForTest("run-c1-auth-files-locked", "alice", "carol", 200))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "bob",
			User:     authUserForTest{gids: []string{"100"}},
		})

		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results/"+stored.ID+"/files", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusForbidden)
		assertLockedResponseForTest(t, response, stored.ID)
	})

	convey.Convey("C1.8: Given user alice has access, when auth detail and file-list endpoints are called, then existing 200 behaviours are preserved", t, func() {
		store := newSQLiteStoreForTest(t)
		stored := seedResultSetForTest(t, store, accessRegistrationForTest("run-c1-auth-success", "alice", "carol", 200))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})

		detailResponse := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results/"+stored.ID, nil)
		filesResponse := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results/"+stored.ID+"/files", nil)

		convey.So(detailResponse.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(detailResponse.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")
		var result ResultSet
		decodeJSONResponseForTest(t, detailResponse, &result)
		convey.So(result.ID, convey.ShouldEqual, stored.ID)
		convey.So(result.Access, convey.ShouldResemble, AccessState{CanView: true})

		convey.So(filesResponse.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(filesResponse.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")
		var files []FileEntry
		decodeJSONResponseForTest(t, filesResponse, &files)
		convey.So(files, convey.ShouldHaveLength, 2)
	})
}

func TestServerRegisterRoutesA1(t *testing.T) {
	convey.Convey("A1.1: Given a Gin router with RegisterRoutes, when GET /rest/v1/results is called, then status 200 and body is a JSON array of all matching rows", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-a1-one", func(reg *Registration) {}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-a1-two", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-a1-two"
		}))

		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), nil)
		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointREST+"/results", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)
		convey.So(results, convey.ShouldHaveLength, 2)
	})

	convey.Convey("A1.2: Given a registered result, when GET /rest/v1/auth/results/<id> is called with a fake authorized user, then status 200 and the JSON id equals the stored ID", t, func() {
		store := newSQLiteStoreForTest(t)
		stored := seedResultSetForTest(t, store, accessRegistrationForTest("run-a1-auth-detail", "alice", "bob", 200))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})

		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results/"+stored.ID, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)
		convey.So(result.ID, convey.ShouldEqual, stored.ID)
	})

	convey.Convey("A1.3: Given a valid JWT for user alice, when GET /rest/v1/auth/session is called, then status is 200 with the current session JSON", t, func() {
		router := newResultsGinHandlerForTest(t, NewServer(newSQLiteStoreForTest(t), nil, nil), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})

		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/session", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var session SessionResponse
		decodeJSONResponseForTest(t, response, &session)
		convey.So(session, convey.ShouldResemble, SessionResponse{
			Authenticated: true,
			Username:      "alice",
			IsOwner:       false,
		})
	})

	convey.Convey("A1.4: Given the standalone handler, when an auth mutation route is called, then it is not registered without auth middleware", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodPost, gas.EndPointAuth+"/results", mustJSONBodyForTest(t, testServerRegistration(t)))

		convey.So(response.Code == http.StatusNotFound || response.Code == http.StatusMethodNotAllowed, convey.ShouldBeTrue)
	})
}

func newResultsGinHandlerForTest(t *testing.T, server *Server, user *CurrentUser) http.Handler {
	t.Helper()

	gin.SetMode(gin.TestMode)

	router := gin.New()
	auth := router.Group(gas.EndPointAuth)
	if user != nil {
		auth.Use(func(c *gin.Context) {
			c.Set(currentUserGinContextKey, user)
			c.Next()
		})
	}

	server.RegisterRoutes(router, auth)

	return router
}

func TestServerOwnerSessionsA4(t *testing.T) {
	convey.Convey("A4.1: Given server user svc and the server token, when POST /rest/v1/jwt logs in, then the returned JWT is marked owner and session reports owner", t, func() {
		handler, ownerStore := newOwnerSessionAuthHandlerForTest(t)
		token := loginJWTForTest(t, handler, "svc", "server-token")

		convey.So(ownerStore.IsOwner(token), convey.ShouldBeTrue)

		response := performResultsJWTRequestForTest(t, handler, http.MethodGet, gas.EndPointAuth+"/session", token, nil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var session SessionResponse
		decodeJSONResponseForTest(t, response, &session)
		convey.So(session, convey.ShouldResemble, SessionResponse{
			Authenticated: true,
			Username:      "svc",
			IsOwner:       true,
		})
	})

	convey.Convey("A4.2: Given server-starting username svc, when svc logs in with LDAP/password instead of the server token, then the returned JWT is not marked owner", t, func() {
		handler, ownerStore := newOwnerSessionAuthHandlerForTest(t)
		token := loginJWTForTest(t, handler, "svc", "ldap-password")

		convey.So(ownerStore.IsOwner(token), convey.ShouldBeFalse)

		response := performResultsJWTRequestForTest(t, handler, http.MethodGet, gas.EndPointAuth+"/session", token, nil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var session SessionResponse
		decodeJSONResponseForTest(t, response, &session)
		convey.So(session.IsOwner, convey.ShouldBeFalse)
	})

	convey.Convey("A4.3: Given an owner JWT, when GET /rest/v1/jwt refreshes it, then the refreshed JWT is marked owner", t, func() {
		handler, ownerStore := newOwnerSessionAuthHandlerForTest(t)
		token := loginJWTForTest(t, handler, "svc", "server-token")

		response := performResultsJWTRequestForTest(t, handler, http.MethodGet, gas.EndPointJWT, token, nil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var refreshed string
		decodeJSONResponseForTest(t, response, &refreshed)
		convey.So(ownerStore.IsOwner(refreshed), convey.ShouldBeTrue)

		sessionResponse := performResultsJWTRequestForTest(t, handler, http.MethodGet, gas.EndPointAuth+"/session", refreshed, nil)
		convey.So(sessionResponse.Code, convey.ShouldEqual, http.StatusOK)

		var session SessionResponse
		decodeJSONResponseForTest(t, sessionResponse, &session)
		convey.So(session.IsOwner, convey.ShouldBeTrue)
	})

	convey.Convey("A4.4: Given an LDAP/password JWT for server username svc, when it is refreshed, then the refreshed JWT is not marked owner", t, func() {
		handler, ownerStore := newOwnerSessionAuthHandlerForTest(t)
		token := loginJWTForTest(t, handler, "svc", "ldap-password")

		response := performResultsJWTRequestForTest(t, handler, http.MethodGet, gas.EndPointJWT, token, nil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var refreshed string
		decodeJSONResponseForTest(t, response, &refreshed)
		convey.So(ownerStore.IsOwner(refreshed), convey.ShouldBeFalse)

		sessionResponse := performResultsJWTRequestForTest(t, handler, http.MethodGet, gas.EndPointAuth+"/session", refreshed, nil)
		convey.So(sessionResponse.Code, convey.ShouldEqual, http.StatusOK)

		var session SessionResponse
		decodeJSONResponseForTest(t, sessionResponse, &session)
		convey.So(session.IsOwner, convey.ShouldBeFalse)
	})

	convey.Convey("A4.5: Given any authenticated JWT, when POST /rest/v1/auth/logout is called, then status is 204 and any owner marker is deleted", t, func() {
		handler, ownerStore := newOwnerSessionAuthHandlerForTest(t)
		token := loginJWTForTest(t, handler, "svc", "server-token")
		convey.So(ownerStore.IsOwner(token), convey.ShouldBeTrue)

		response := performResultsJWTRequestForTest(t, handler, http.MethodPost, gas.EndPointAuth+"/logout", token, nil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusNoContent)
		convey.So(ownerStore.IsOwner(token), convey.ShouldBeFalse)

		sessionResponse := performResultsJWTRequestForTest(t, handler, http.MethodGet, gas.EndPointAuth+"/session", token, nil)
		convey.So(sessionResponse.Code, convey.ShouldEqual, http.StatusOK)

		var session SessionResponse
		decodeJSONResponseForTest(t, sessionResponse, &session)
		convey.So(session.IsOwner, convey.ShouldBeFalse)
	})
}

func newOwnerSessionAuthHandlerForTest(t *testing.T) (http.Handler, OwnerSessionStore) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	ownerStore := NewOwnerSessionStore()
	authServer := gas.New(io.Discard)
	authServer.Router().Use(OwnerSessionMiddleware(OwnerSessionConfig{
		ServerUsername: "svc",
		ServerToken:    []byte("server-token"),
		Store:          ownerStore,
	}))

	keyPath := filepath.Join(t.TempDir(), "jwt.key")
	err := authServer.EnableAuth("", keyPath, func(username, password string) (bool, string) {
		switch {
		case username == "svc" && password == "server-token":
			return true, "1234"
		case username == "svc" && password == "ldap-password":
			return true, "1234"
		default:
			return false, ""
		}
	})
	if err != nil {
		t.Fatalf("enable auth: %v", err)
	}

	resultsServer := NewServer(
		newSQLiteStoreForTest(t),
		nil,
		nil,
		WithOwnerSessionStore(ownerStore),
	)
	resultsServer.RegisterRoutes(authServer.Router(), authServer.AuthRouter())

	return authServer.Router(), ownerStore
}

func loginJWTForTest(t *testing.T, handler http.Handler, username, password string) string {
	t.Helper()

	response := performResultsRequestForTest(t, handler, http.MethodPost, gas.EndPointJWT, mustJSONBodyForTest(t, map[string]string{
		"username": username,
		"password": password,
	}))
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}

	var token string
	decodeJSONResponseForTest(t, response, &token)

	return token
}

func TestServerRegistrationAuthorizationC2(t *testing.T) {
	convey.Convey("C2.1: Given a JWT marked owner by server-token login and registration requester alice, operator carol, when POST runs, then stored requester is alice and operator is carol", t, func() {
		store := newSQLiteStoreForTest(t)
		ownerStore := NewOwnerSessionStore()
		ownerToken := "owner-token-c2"
		ownerStore.MarkOwner(ownerToken, time.Now().Add(time.Hour))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil, WithOwnerSessionStore(ownerStore)), &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})
		reg := testServerRegistration(t)
		reg.RunKey = "run-c2-owner"
		reg.Requester = "alice"
		reg.Operator = "carol"

		response := performResultsJWTRequestForTest(t, router, http.MethodPost, gas.EndPointAuth+"/results", ownerToken, mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		assertStoredResultActorsForTest(t, store, response, "alice", "carol")
	})

	convey.Convey("C2.2: Given LDAP user bob and registration requester alice, operator carol, when POST runs, then stored requester is alice and operator is bob", t, func() {
		store := newSQLiteStoreForTest(t)
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "bob",
			User:     authUserForTest{},
		})
		reg := testServerRegistration(t)
		reg.RunKey = "run-c2-ldap-bob"
		reg.Requester = "alice"
		reg.Operator = "carol"

		response := performResultsJWTRequestForTest(t, router, http.MethodPost, gas.EndPointAuth+"/results", "ldap-token-bob", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		assertStoredResultActorsForTest(t, store, response, "alice", "bob")
	})

	convey.Convey("C2.3: Given the server-starting username is svc but the JWT came from LDAP/password login, when registration requester is alice and operator is carol, then stored requester is alice and operator is svc", t, func() {
		store := newSQLiteStoreForTest(t)
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})
		reg := testServerRegistration(t)
		reg.RunKey = "run-c2-ldap-svc"
		reg.Requester = "alice"
		reg.Operator = "carol"

		response := performResultsJWTRequestForTest(t, router, http.MethodPost, gas.EndPointAuth+"/results", "ldap-token-svc", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		assertStoredResultActorsForTest(t, store, response, "alice", "svc")
	})

	convey.Convey("C2.5: Given unauthenticated POST to /rest/v1/results, when called, then status is 404 or 405", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodPost, gas.EndPointREST+"/results", mustJSONBodyForTest(t, testServerRegistration(t)))

		convey.So(response.Code == http.StatusNotFound || response.Code == http.StatusMethodNotAllowed, convey.ShouldBeTrue)
	})
}

func TestServerOwnerOnlyMutationsC3(t *testing.T) {
	convey.Convey("C3.1: Given LDAP user alice with result access, when DELETE /rest/v1/auth/results/<id> is called, then status is 403 locked and the row still exists", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.OutputDirectoryGID = gidForTest(200)
		result := seedResultSetForTest(t, store, reg)
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})

		response := performResultsJWTRequestForTest(t, router, http.MethodDelete, gas.EndPointAuth+"/results/"+result.ID, "ldap-token-alice", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusForbidden)
		assertLockedResponseForTest(t, response, result.ID)

		stored, err := store.Get(t.Context(), result.ID)
		convey.So(err, convey.ShouldBeNil)
		convey.So(stored.ID, convey.ShouldEqual, result.ID)
	})

	convey.Convey("C3.2: Given server-owner-token user, when the same delete is called, then status is 204 and the row is removed", t, func() {
		store := newSQLiteStoreForTest(t)
		result := seedResultSetForTest(t, store, testRegistration())
		ownerToken := "owner-token-c3-delete"
		ownerStore := NewOwnerSessionStore()
		ownerStore.MarkOwner(ownerToken, time.Now().Add(time.Hour))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil, WithOwnerSessionStore(ownerStore)), &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})

		response := performResultsJWTRequestForTest(t, router, http.MethodDelete, gas.EndPointAuth+"/results/"+result.ID, ownerToken, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNoContent)
		convey.So(response.Body.Len(), convey.ShouldEqual, 0)

		_, err := store.Get(t.Context(), result.ID)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
	})

	convey.Convey("C3.3: Given LDAP user alice, when PUT /rest/v1/auth/results/<id>/files is called, then status is 403 and files are unchanged", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.OutputDirectoryGID = gidForTest(200)
		result := seedResultSetForTest(t, store, reg)
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})

		response := performResultsJWTRequestForTest(
			t,
			router,
			http.MethodPut,
			gas.EndPointAuth+"/results/"+result.ID+"/files",
			"ldap-token-alice",
			mustJSONBodyForTest(t, replacementOutputFilesForTest()),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusForbidden)
		assertLockedResponseForTest(t, response, result.ID)

		files, err := store.GetFiles(t.Context(), result.ID)
		convey.So(err, convey.ShouldBeNil)
		convey.So(files, convey.ShouldResemble, []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
		})
	})

	convey.Convey("C3.4: Given server-owner-token user, when rescan is called with valid output files, then status is 200 and files are replaced", t, func() {
		store := newSQLiteStoreForTest(t)
		result := seedResultSetForTest(t, store, testRegistration())
		ownerToken := "owner-token-c3-rescan"
		ownerStore := NewOwnerSessionStore()
		ownerStore.MarkOwner(ownerToken, time.Now().Add(time.Hour))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil, WithOwnerSessionStore(ownerStore)), &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})

		response := performResultsJWTRequestForTest(
			t,
			router,
			http.MethodPut,
			gas.EndPointAuth+"/results/"+result.ID+"/files",
			ownerToken,
			mustJSONBodyForTest(t, replacementOutputFilesForTest()),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var files []FileEntry
		decodeJSONResponseForTest(t, response, &files)
		convey.So(files, convey.ShouldResemble, []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "input"},
			{Path: "/tmp/results/run/out-new.txt", Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC), Size: 404, Kind: "output"},
		})
	})

	convey.Convey("C3.5: Given server-starting username svc logged in by LDAP password, when delete and rescan are called, then status is 403 and no mutation occurs", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.Requester = "svc"
		reg.OutputDirectoryGID = gidForTest(200)
		result := seedResultSetForTest(t, store, reg)
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})

		deleteResponse := performResultsJWTRequestForTest(t, router, http.MethodDelete, gas.EndPointAuth+"/results/"+result.ID, "ldap-token-svc", nil)
		rescanResponse := performResultsJWTRequestForTest(
			t,
			router,
			http.MethodPut,
			gas.EndPointAuth+"/results/"+result.ID+"/files",
			"ldap-token-svc",
			mustJSONBodyForTest(t, replacementOutputFilesForTest()),
		)

		convey.So(deleteResponse.Code, convey.ShouldEqual, http.StatusForbidden)
		assertLockedResponseForTest(t, deleteResponse, result.ID)
		convey.So(rescanResponse.Code, convey.ShouldEqual, http.StatusForbidden)
		assertLockedResponseForTest(t, rescanResponse, result.ID)

		stored, err := store.Get(t.Context(), result.ID)
		convey.So(err, convey.ShouldBeNil)
		convey.So(stored.ID, convey.ShouldEqual, result.ID)

		files, err := store.GetFiles(t.Context(), result.ID)
		convey.So(err, convey.ShouldBeNil)
		convey.So(files, convey.ShouldResemble, []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
		})
	})
}

func performResultsJWTRequestForTest(t *testing.T, handler http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	path = normalizeResultsPathForTest(method, path)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func performOwnerResultsRequestForTest(t *testing.T, server *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	token := "owner-token-" + strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	if server.ownerSessions == nil {
		server.ownerSessions = NewOwnerSessionStore()
	}
	server.ownerSessions.MarkOwner(token, time.Now().Add(time.Hour))

	handler := newResultsGinHandlerForTest(t, server, &CurrentUser{
		Username: "svc",
		User:     authUserForTest{},
	})

	return performResultsJWTRequestForTest(t, handler, method, path, token, body)
}

func TestServerPostResults(t *testing.T) {
	convey.Convey("E1.1: Given an empty store and valid Registration JSON, when POST /results is called, then status is 201 with JSON result fields and application/json content type", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil, nil)

		response := performOwnerResultsRequestForTest(t, server, http.MethodPost, "/results", mustJSONBodyForTest(t, testServerRegistration(t)))

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)

		convey.So(result.Requester, convey.ShouldEqual, "alice")
		convey.So(result.Access, convey.ShouldResemble, AccessState{CanView: true})
		convey.So(result.CreatedAt.IsZero(), convey.ShouldBeFalse)
		convey.So(regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(result.ID), convey.ShouldBeTrue)
	})

	convey.Convey("E1.2: Given the same Registration POSTed twice, then the second response status is 200 and created_at matches the first", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil, nil)
		body := mustJSONBodyForTest(t, testServerRegistration(t))

		firstResponse := performOwnerResultsRequestForTest(t, server, http.MethodPost, "/results", body)
		secondResponse := performOwnerResultsRequestForTest(t, server, http.MethodPost, "/results", body)

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
		validator := NewMLWHValidator(&mlwhValidationQueryerForTest{
			responses: map[string]mlwhValidationResponseForTest{
				"48522": {match: mlwh.Match{Kind: mlwh.KindRunID}},
			},
		})
		reg := testServerRegistration(t)
		reg.Metadata = map[string]string{"seqmeta_runid": "48522"}

		response := performOwnerResultsRequestForTest(t, NewServer(store, validator, nil), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
	})

	convey.Convey("Given repeated metadata values in a registration, then all values are stored and searchable", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil, nil)
		reg := testServerRegistration(t)
		reg.RunKey = "run-multi-values"
		reg.Metadata = map[string]string{
			SeqmetaSampleNameKey: "SANG1",
			"assay":              "RNA",
		}
		body := mustJSONBodyForTest(t, struct {
			*Registration
			MetadataValues map[string][]string `json:"metadata_values"`
		}{
			Registration: reg,
			MetadataValues: map[string][]string{
				SeqmetaSampleNameKey: {"SANG1", "SANG2"},
				"assay":              {"RNA", "WGS"},
			},
		})

		postResponse := performOwnerResultsRequestForTest(t, server, http.MethodPost, "/results", body)
		convey.So(postResponse.Code, convey.ShouldEqual, http.StatusCreated)

		var result ResultSet
		decodeJSONResponseForTest(t, postResponse, &result)
		convey.So(metadataValuesForResultForTest(t, store, result.ID, SeqmetaSampleNameKey), convey.ShouldResemble, []string{"SANG1", "SANG2"})
		convey.So(metadataValuesForResultForTest(t, store, result.ID, "assay"), convey.ShouldResemble, []string{"RNA", "WGS"})

		for _, path := range []string{
			"/results?sample=SANG1",
			"/results?sample=SANG2",
			"/results?meta_assay=RNA",
			"/results?meta_assay=WGS",
		} {
			response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, path, nil)
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var results []ResultSet
			decodeJSONResponseForTest(t, response, &results)
			convey.So(results, convey.ShouldHaveLength, 1)
			convey.So(results[0].ID, convey.ShouldEqual, result.ID)
		}
	})

	convey.Convey("Bug 260609-2: Given registration lookup values, the server resolves them with its configured MLWH resolver and stores repeated source metadata", t, func() {
		store := newSQLiteStoreForTest(t)
		sampleNameCalls := []string{}
		broadSampleCalls := []string{}
		expander := &mockSearchExpander{
			sampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				sampleNameCalls = append(sampleNameCalls, raw)
				if raw == "7607STDY14643771" {
					return mlwh.Match{
						Kind:      mlwh.KindSangerSampleName,
						Canonical: raw,
						Sample:    &mlwh.Sample{Name: raw},
					}, nil
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			sampleFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				broadSampleCalls = append(broadSampleCalls, raw)

				return mlwh.Match{
					Kind:      mlwh.KindSupplierName,
					Canonical: "7607STDY14643771",
					Sample: &mlwh.Sample{
						Name:         "7607STDY14643771",
						SupplierName: raw,
					},
				}, nil
			},
		}
		reg := testServerRegistration(t)
		reg.RunKey = "server-lookup-samples"
		reg.Metadata = map[string]string{"assay": "RNA"}
		reg.MetadataValues = map[string][]string{"assay": {"RNA", "WGS"}}

		body := mustRegistrationLookupBodyForTest(t, reg, map[string][]string{
			"sample": {"7607STDY14643771", "Hek_R1", "Hek_R2"},
		})
		response := performOwnerResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)), http.MethodPost, "/results", body)

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(sampleNameCalls, convey.ShouldResemble, []string{"7607STDY14643771", "Hek_R1", "Hek_R2"})
		convey.So(broadSampleCalls, convey.ShouldResemble, []string{"Hek_R1", "Hek_R2"})

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)
		convey.So(metadataValuesForResultForTest(t, store, result.ID, SeqmetaSampleNameKey), convey.ShouldResemble, []string{"7607STDY14643771"})
		convey.So(metadataValuesForResultForTest(t, store, result.ID, SeqmetaSupplierNameKey), convey.ShouldResemble, []string{"Hek_R1", "Hek_R2"})
		convey.So(metadataValuesForResultForTest(t, store, result.ID, "sample"), convey.ShouldResemble, []string{"Hek_R1", "Hek_R2"})
		convey.So(metadataValuesForResultForTest(t, store, result.ID, "assay"), convey.ShouldResemble, []string{"RNA", "WGS"})
	})

	convey.Convey("Bug 260609-2: Given an invalid MLWH lookup value, the server returns an error before storing a result", t, func() {
		store := newSQLiteStoreForTest(t)
		expander := &mockSearchExpander{
			sampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "missing-id")

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			sampleFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "missing-id")

				return mlwh.Match{}, mlwh.ErrNotFound
			},
		}
		reg := testServerRegistration(t)

		response := performOwnerResultsRequestForTest(
			t,
			NewServer(store, nil, NewMLWHSearchResolver(expander)),
			http.MethodPost,
			"/results",
			mustRegistrationLookupBodyForTest(t, reg, map[string][]string{"sample": {"missing-id"}}),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, `--sample "missing-id"`)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, "not found")
		convey.So(resultSetCountForTest(t, store), convey.ShouldEqual, 0)
	})

	convey.Convey("Bug 260609-2: Given a library ID lookup, the server uses the exact library identifier fast path", t, func() {
		store := newSQLiteStoreForTest(t)
		libraryIdentifierCalls := []string{}
		broadLibraryCalls := 0
		expander := &mockSearchExpander{
			libraryIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				libraryIdentifierCalls = append(libraryIdentifierCalls, raw)

				return mlwh.Match{
					Kind:      mlwh.KindLibraryID,
					Canonical: "71046409",
					Library: &mlwh.Library{
						PipelineIDLims: "Custom",
						LibraryID:      "71046409",
					},
				}, nil
			},
			libraryFunc: func(context.Context, string) (mlwh.Match, error) {
				broadLibraryCalls++

				return mlwh.Match{}, errors.New("broad library resolver should not run for library IDs")
			},
		}
		reg := testServerRegistration(t)
		reg.RunKey = "server-library-fast-path"

		response := performOwnerResultsRequestForTest(
			t,
			NewServer(store, nil, NewMLWHSearchResolver(expander)),
			http.MethodPost,
			"/results",
			mustRegistrationLookupBodyForTest(t, reg, map[string][]string{"library": {"71046409"}}),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(libraryIdentifierCalls, convey.ShouldResemble, []string{"71046409"})
		convey.So(broadLibraryCalls, convey.ShouldEqual, 0)

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)
		convey.So(metadataValuesForResultForTest(t, store, result.ID, SeqmetaLibraryIDKey), convey.ShouldResemble, []string{"71046409"})
		convey.So(metadataValuesForResultForTest(t, store, result.ID, SeqmetaPipelineIDLimsKey), convey.ShouldResemble, []string{"Custom"})
	})

	convey.Convey("Bug 260609-2: Given literal seqmeta metadata conflicts with lookup metadata, the server rejects it before storing", t, func() {
		store := newSQLiteStoreForTest(t)
		expander := &mockSearchExpander{
			sampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{
					Kind:      mlwh.KindSangerSampleName,
					Canonical: raw,
					Sample:    &mlwh.Sample{Name: raw},
				}, nil
			},
		}
		reg := testServerRegistration(t)
		reg.Metadata = map[string]string{SeqmetaSampleNameKey: "literal-sample"}
		reg.MetadataValues = map[string][]string{SeqmetaSampleNameKey: {"literal-sample"}}

		response := performOwnerResultsRequestForTest(
			t,
			NewServer(store, nil, NewMLWHSearchResolver(expander)),
			http.MethodPost,
			"/results",
			mustRegistrationLookupBodyForTest(t, reg, map[string][]string{"sample": {"7607STDY14643771"}}),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, `metadata key "seqmeta_name"`)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, "--sample")
		convey.So(resultSetCountForTest(t, store), convey.ShouldEqual, 0)
	})

	convey.Convey("E1.4: Given MLWH returns the wrong type, then status is 422 with an error body", t, func() {
		store := newSQLiteStoreForTest(t)
		validator := NewMLWHValidator(&mlwhValidationQueryerForTest{
			responses: map[string]mlwhValidationResponseForTest{
				"48522": {match: mlwh.Match{Kind: mlwh.KindStudyLimsID}},
			},
		})
		reg := testServerRegistration(t)
		reg.Metadata = map[string]string{"seqmeta_runid": "48522"}

		response := performOwnerResultsRequestForTest(t, NewServer(store, validator, nil), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusUnprocessableEntity)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldNotBeBlank)
	})

	convey.Convey("Given repeated seqmeta metadata values and one has the wrong type, then validation rejects the registration", t, func() {
		store := newSQLiteStoreForTest(t)
		validator := NewMLWHValidator(&mlwhValidationQueryerForTest{
			responses: map[string]mlwhValidationResponseForTest{
				"48522": {match: mlwh.Match{Kind: mlwh.KindRunID}},
				"SANG1": {match: mlwh.Match{Kind: mlwh.KindSangerSampleName}},
			},
		})
		reg := testServerRegistration(t)
		reg.Metadata = map[string]string{SeqmetaIDRunKey: "48522"}
		body := mustJSONBodyForTest(t, struct {
			*Registration
			MetadataValues map[string][]string `json:"metadata_values"`
		}{
			Registration: reg,
			MetadataValues: map[string][]string{
				SeqmetaIDRunKey: {"48522", "SANG1"},
			},
		})

		response := performOwnerResultsRequestForTest(t, NewServer(store, validator, nil), http.MethodPost, "/results", body)

		convey.So(response.Code, convey.ShouldEqual, http.StatusUnprocessableEntity)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, "seqmeta_id_run")
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, "SANG1")
	})

	convey.Convey("E1.5: Given MLWH is unavailable, then status is 502", t, func() {
		store := newSQLiteStoreForTest(t)
		validator := NewMLWHValidator(&mlwhValidationQueryerForTest{
			responses: map[string]mlwhValidationResponseForTest{
				"48522": {err: mlwh.ErrUpstreamImpaired},
			},
		})
		reg := testServerRegistration(t)
		reg.Metadata = map[string]string{"seqmeta_runid": "48522"}

		response := performOwnerResultsRequestForTest(t, NewServer(store, validator, nil), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadGateway)
	})

	convey.Convey("E1.6: Given Registration missing pipeline_identifier, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testServerRegistration(t)
		reg.PipelineIdentifier = ""

		response := performOwnerResultsRequestForTest(t, NewServer(store, nil, nil), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
	})

	convey.Convey("E1.7: Given a malformed JSON body, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performOwnerResultsRequestForTest(t, NewServer(store, nil, nil), http.MethodPost, "/results", []byte(`{"pipeline_identifier":`))

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
	})

	convey.Convey("B1.1: Given a directory with a Unix GID, when it is registered, then result_sets.output_directory_gid and JSON contain that GID", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testServerRegistration(t)
		expectedGID := statGIDForTest(t, reg.OutputDirectory)

		response := performOwnerResultsRequestForTest(t, NewServer(store, nil, nil), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(response.Body.String(), convey.ShouldContainSubstring, fmt.Sprintf(`"output_directory_gid":%d`, expectedGID))

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)
		convey.So(result.OutputDirectoryGID, convey.ShouldNotBeNil)
		convey.So(*result.OutputDirectoryGID, convey.ShouldEqual, expectedGID)

		storedGID := resultSetGIDForTest(t, store.db, result.ID)
		convey.So(storedGID.Valid, convey.ShouldBeTrue)
		convey.So(storedGID.Int64, convey.ShouldEqual, expectedGID)
	})

	convey.Convey("B1.2: Given registration JSON containing output_directory_gid, when the real directory has another GID, then the stored value is the server stat value", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testServerRegistration(t)
		expectedGID := statGIDForTest(t, reg.OutputDirectory)
		clientGID := expectedGID + 9999
		body := mustJSONBodyForTest(t, struct {
			*Registration
			OutputDirectoryGID int64 `json:"output_directory_gid"`
		}{
			Registration:       reg,
			OutputDirectoryGID: clientGID,
		})

		response := performOwnerResultsRequestForTest(t, NewServer(store, nil, nil), http.MethodPost, "/results", body)

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)
		convey.So(result.OutputDirectoryGID, convey.ShouldNotBeNil)
		convey.So(*result.OutputDirectoryGID, convey.ShouldEqual, expectedGID)
		convey.So(*result.OutputDirectoryGID, convey.ShouldNotEqual, clientGID)

		storedGID := resultSetGIDForTest(t, store.db, result.ID)
		convey.So(storedGID.Valid, convey.ShouldBeTrue)
		convey.So(storedGID.Int64, convey.ShouldEqual, expectedGID)
	})

	convey.Convey("B1.3: Given the output directory cannot be statted, when registration is called, then status is 400 and the body explains output directory GID determination", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.OutputDirectory = filepath.Join(t.TempDir(), "missing-output-directory")
		reg.Files[0].Path = filepath.Join(reg.OutputDirectory, "out-1.txt")

		response := performOwnerResultsRequestForTest(t, NewServer(store, nil, nil), http.MethodPost, "/results", mustJSONBodyForTest(t, reg))

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, "determine output directory gid")
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

	convey.Convey("Bug 3: Given GET /results?study=6568, then MLWH-named study metadata is matched by the combined Study field", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-lims-match", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_id_study_lims": "6568"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-lims-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_id_study_lims": "9999"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?study=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-study-lims-match")
	})

	convey.Convey("Bug 260605-5: Given GET /results?study=ERP7607, then source-specific study metadata is matched by the combined Study field", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-accession-match", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaStudyAccessionKey: "ERP7607"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-accession-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{SeqmetaStudyAccessionKey: "ERP7608"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?study=ERP7607", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-study-accession-match")
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

	convey.Convey("Bug 3: Given GET /results?sample=SANG1001, then MLWH-named sample metadata is matched by the combined Sample field", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-name-match", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_name": "SANG1001"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-name-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_name": "SANG1002"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?sample=SANG1001", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-sample-name-match")
	})

	convey.Convey("Bug 260605-5: Given GET /results?sample=Hek_R1, then source-specific sample metadata is matched by the combined Sample field", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-source-match", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaSupplierNameKey: "Hek_R1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-source-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{SeqmetaSupplierNameKey: "Hek_R2"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?sample=Hek_R1", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-supplier-source-match")
	})

	convey.Convey("Bug PR12 review: Given GET /results?sample uses a sample UUID or donor ID, then MLWH expansion can match registered sample names", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-uuid-expanded", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaSampleNameKey: "SANG-UUID"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-donor-expanded", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{SeqmetaSampleNameKey: "SANG-DONOR"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-unrelated-sample", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Metadata = map[string]string{SeqmetaSampleNameKey: "SANG-OTHER"}
		}))

		sampleExpansionCalls := map[mlwh.IdentifierKind][]string{}
		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				sampleExpansionCalls[kind] = append(sampleExpansionCalls[kind], canonical)
				switch {
				case kind == mlwh.KindSampleUUID && canonical == "sample-uuid-123":
					return mlwh.SearchValues{Samples: []string{"SANG-UUID"}}, nil
				case kind == mlwh.KindDonorID && canonical == "DONOR-42":
					return mlwh.SearchValues{Samples: []string{"SANG-DONOR"}}, nil
				default:
					return mlwh.SearchValues{}, nil
				}
			},
		}
		server := NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler()

		uuidResponse := performResultsRequestForTest(t, server, http.MethodGet, "/results?sample=sample-uuid-123", nil)
		convey.So(uuidResponse.Code, convey.ShouldEqual, http.StatusOK)
		var uuidResults []ResultSet
		decodeJSONResponseForTest(t, uuidResponse, &uuidResults)
		convey.So(uuidResults, convey.ShouldHaveLength, 1)
		convey.So(uuidResults[0].RunKey, convey.ShouldEqual, "run-sample-uuid-expanded")
		convey.So(sampleExpansionCalls[mlwh.KindSampleUUID], convey.ShouldContain, "sample-uuid-123")

		donorResponse := performResultsRequestForTest(t, server, http.MethodGet, "/results?sample=DONOR-42", nil)
		convey.So(donorResponse.Code, convey.ShouldEqual, http.StatusOK)
		var donorResults []ResultSet
		decodeJSONResponseForTest(t, donorResponse, &donorResults)
		convey.So(donorResults, convey.ShouldHaveLength, 1)
		convey.So(donorResults[0].RunKey, convey.ShouldEqual, "run-donor-expanded")
		convey.So(sampleExpansionCalls[mlwh.KindDonorID], convey.ShouldContain, "DONOR-42")
	})

	convey.Convey("Bug item 4: Given GET /results?sample=Hek_R1, then supplier-name sample aliases are resolved from registered sample candidates without slow live expansion", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-alias-match", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaSampleNameKey: "7607STDY14643771"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-alias-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{SeqmetaSampleNameKey: "7607STDY14643772"}
		}))

		sampleNameCalls := []string{}
		expander := &mockSearchExpander{
			sampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				sampleNameCalls = append(sampleNameCalls, raw)
				switch raw {
				case "7607STDY14643771":
					return mlwh.Match{Sample: &mlwh.Sample{Name: raw, SupplierName: "Hek_R1"}}, nil
				case "7607STDY14643772":
					return mlwh.Match{Sample: &mlwh.Sample{Name: raw, SupplierName: "Hek_R2"}}, nil
				default:
					return mlwh.Match{}, mlwh.ErrNotFound
				}
			},
			searchValuesFunc: func(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error) {
				return mlwh.SearchValues{}, errors.New("slow live expansion should not run for supplier-name sample search")
			},
		}

		response := performResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler(), http.MethodGet, "/results?sample=Hek_R1", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-supplier-alias-match")
		convey.So(mergeSearchValues(nil, sampleNameCalls), convey.ShouldResemble, []string{"7607STDY14643771", "7607STDY14643772"})
		convey.So(expander.expandCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("Bug 260519-2.2: Given GET /results?seqmeta_sample_name=SANG1001, then existing seqmeta_name metadata is matched", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-name-url-match", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_name": "SANG1001"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-name-url-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_name": "SANG1002"}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?seqmeta_sample_name=SANG1001", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-sample-name-url-match")
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

	convey.Convey("E2.3: Given GET /results?output_directory=project-a, then only result sets whose output_directory contains that substring are returned", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-output-directory-match", func(reg *Registration) {
			reg.OutputDirectory = "/lustre/scratch/project-a/run-1"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-output-directory-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.OutputDirectory = "/lustre/archive/project-b/run-2"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?output_directory=project-a", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].OutputDirectory, convey.ShouldEqual, "/lustre/scratch/project-a/run-1")
	})

	convey.Convey("Given GET /results?output_dir_prefix=project-a, then the legacy output directory query parameter remains supported", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-legacy-output-directory-match", func(reg *Registration) {
			reg.OutputDirectory = "/lustre/scratch/project-a/run-1"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-legacy-output-directory-miss", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.OutputDirectory = "/lustre/archive/project-b/run-2"
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?output_dir_prefix=project-a", nil)

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

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {Samples: []string{"SANG1", "SANG2"}},
			},
		})
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

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {Samples: []string{"SANG1"}},
			},
		})
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

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {},
			},
		})
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study_id=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, "[]\n")
	})

	convey.Convey("E1.4: Given study_id is requested without MLWH configured, then status is 400", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?study_id=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldEqual, "MLWH resolver not configured")
	})

	convey.Convey("E1.5: Given MLWH returns an error for the study lookup, then status is 502", t, func() {
		store := newSQLiteStoreForTest(t)
		resolver := NewMLWHSearchResolver(&mockSearchExpander{
			searchValuesFunc: func(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error) {
				return mlwh.SearchValues{}, mlwh.ErrUpstreamImpaired
			},
		})

		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study_id=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadGateway)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, "results: mlwh unavailable")
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldNotContainSubstring, "seqmeta unavailable")
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

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {Samples: []string{"SANG1", "SANG2"}},
			},
		})
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

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {Samples: []string{"SANG1", "SANG2", "SANG3"}},
			},
		})
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

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"EGAS00001005445": {Samples: []string{"ACC1", "ACC2"}},
			},
		})
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

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {Samples: []string{"SANG1", "SANG2"}},
			},
		})
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

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {Samples: []string{"SANG1", "SANG2"}},
			},
		})
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_studyid=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].ResultSet.Metadata["seqmeta_sampleid"], convey.ShouldEqual, "SANG1")
	})

	convey.Convey("Bug 4: Given study=6568 with resolver, nf-core/rnaseq tagged with seqmeta_studyid=6568 and nf-core/sarek tagged with resolved sample SANG42 are both found via SQL OR, excluding unrelated results", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-rnaseq", func(reg *Registration) {
			reg.PipelineName = "nf-core/rnaseq"
			reg.Metadata = map[string]string{"seqmeta_studyid": "6568"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sarek", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.PipelineName = "nf-core/sarek"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG42"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-unrelated", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Metadata = map[string]string{"seqmeta_studyid": "9999"}
		}))

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindStudyLimsID: {
				"6568": {Samples: []string{"SANG42"}},
			},
		})
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		runKeys := make([]string, len(results))
		for i, r := range results {
			runKeys[i] = r.ResultSet.RunKey
		}

		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(runKeys, convey.ShouldContain, "run-rnaseq")
		convey.So(runKeys, convey.ShouldContain, "run-sarek")
	})

	convey.Convey("Bug 3: Given library=RNA, direct library metadata, seqmeta_library metadata, resolver-derived samples, and resolver-derived lanes are all included", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-direct", func(reg *Registration) {
			reg.Metadata = map[string]string{"library": "RNA"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-seqmeta", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_library": "RNA"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-sample", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "LIBS1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-lane", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-4"
			reg.Metadata = map[string]string{"seqmeta_lane": "100_1#1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-unrelated", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-5"
			reg.Metadata = map[string]string{"library": "WGS"}
		}))

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindLibraryType: {
				"RNA": {Samples: []string{"LIBS1"}, Runs: []string{"100"}, Lanes: []string{"100_1#1"}},
			},
		})
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?library=RNA", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		runKeys := make([]string, len(results))
		for i, result := range results {
			runKeys[i] = result.RunKey
		}

		convey.So(results, convey.ShouldHaveLength, 4)
		convey.So(runKeys, convey.ShouldContain, "run-library-direct")
		convey.So(runKeys, convey.ShouldContain, "run-library-seqmeta")
		convey.So(runKeys, convey.ShouldContain, "run-library-sample")
		convey.So(runKeys, convey.ShouldContain, "run-library-lane")
	})

	convey.Convey("Bug 4: Given sample=SANGX with resolver-derived lanes, sample search includes lane-only result sets", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-direct", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANGX"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-lane", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_lane": "200_2#7"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-unrelated", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Metadata = map[string]string{"seqmeta_lane": "999_9#9"}
		}))

		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindSangerSampleName: {
				"SANGX": {Samples: []string{"SANGX"}, Runs: []string{"200"}, Lanes: []string{"200_2#7"}},
			},
		})
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?sample=SANGX", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		runKeys := make([]string, len(results))
		for i, result := range results {
			runKeys[i] = result.RunKey
		}

		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(runKeys, convey.ShouldContain, "run-sample-direct")
		convey.So(runKeys, convey.ShouldContain, "run-sample-lane")
	})

	convey.Convey("C2.3: Given sample fan-out across studies, sample resolver expansion returns both studies' lanes", t, func() {
		resolver := newStaticMLWHSearchResolverForTest(map[mlwh.IdentifierKind]map[string]mlwh.SearchValues{
			mlwh.KindSangerSampleName: {
				"S1": {Samples: []string{"S1"}, Runs: []string{"100", "101"}, Lanes: []string{"100_1#1", "101_2#2"}},
			},
		})
		samples, runs, lanes, err := resolver.Expand(context.Background(), mlwh.KindSangerSampleName, "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldResemble, []string{"S1"})
		convey.So(runs, convey.ShouldResemble, []string{"100", "101"})
		convey.So(lanes, convey.ShouldResemble, []string{"100_1#1", "101_2#2"})
	})

	convey.Convey("Bug 4: repeated study lookups reuse resolver cache to avoid duplicate upstream requests", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-cache", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG-CACHE"}
		}))

		studyHits := 0
		resolver := NewMLWHSearchResolver(&mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindStudyLimsID)
				convey.So(canonical, convey.ShouldEqual, "6568")
				studyHits++

				return mlwh.SearchValues{
					Samples: []string{"SANG-CACHE"},
					Runs:    []string{"300"},
					Lanes:   []string{"300_3#11"},
				}, nil
			},
		})

		first := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study=6568", nil)
		second := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study=6568", nil)

		convey.So(first.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(second.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(studyHits, convey.ShouldEqual, 1)
	})

	convey.Convey("G1.1/G1.2: Given study search expansion via mlwh, then direct study, sample, and lane-tagged results are all returned through OR groups", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-direct", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_studyid": "6568"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-sample", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "7607STDY14643771"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-lane", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Metadata = map[string]string{"seqmeta_lane": "12345_1#10"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-unrelated", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-4"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "OTHER-SAMPLE"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-sibling-lane", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-5"
			reg.Metadata = map[string]string{"seqmeta_lane": "67890_2#11"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindStudyLimsID)
				convey.So(canonical, convey.ShouldEqual, "6568")

				return mlwh.SearchValues{Samples: []string{"7607STDY14643771"}, Runs: []string{"12345"}, Lanes: []string{"12345_1#10"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study=6568", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		runKeys := make([]string, len(results))
		for i, result := range results {
			runKeys[i] = result.ResultSet.RunKey
		}

		convey.So(results, convey.ShouldHaveLength, 3)
		convey.So(runKeys, convey.ShouldContain, "run-study-direct")
		convey.So(runKeys, convey.ShouldContain, "run-study-sample")
		convey.So(runKeys, convey.ShouldContain, "run-study-lane")
		convey.So(runKeys, convey.ShouldNotContain, "run-study-sibling-lane")
	})

	convey.Convey("G1.3: Given repeated study search within 5 minutes, then mlwh ExpandIdentifier is called at most once", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-cache", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG-CACHE"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindStudyLimsID)
				convey.So(canonical, convey.ShouldEqual, "6568")

				return mlwh.SearchValues{Samples: []string{"SANG-CACHE"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		first := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study=6568", nil)
		second := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study=6568", nil)

		convey.So(first.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(second.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(expander.expandCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("Bug item 4: Given Study search uses accession EGAS00001005445 for study 6568, then direct study-id and related sample fixture results are both returned", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-rnaseq-accession", func(reg *Registration) {
			reg.PipelineName = "nf-core/rnaseq"
			reg.Metadata = map[string]string{"seqmeta_studyid": "6568"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sarek-accession", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.PipelineName = "nf-core/sarek"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "WTSI_wEMB10524782"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-unrelated-accession", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Metadata = map[string]string{"seqmeta_studyid": "9999"}
		}))

		expander := &mockSearchExpander{
			resolveStudyFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "EGAS00001005445")

				return mlwh.Match{
					Kind:      mlwh.KindStudyAccession,
					Canonical: "6568",
					Study: &mlwh.Study{
						IDStudyLims:     "6568",
						AccessionNumber: "EGAS00001005445",
					},
				}, nil
			},
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindStudyLimsID)
				convey.So(canonical, convey.ShouldEqual, "6568")

				return mlwh.SearchValues{Samples: []string{"WTSI_wEMB10524782"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?study=EGAS00001005445", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []SearchResult
		decodeJSONResponseForTest(t, response, &results)

		runKeys := make([]string, len(results))
		for i, result := range results {
			runKeys[i] = result.ResultSet.RunKey
		}

		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(runKeys, convey.ShouldContain, "run-rnaseq-accession")
		convey.So(runKeys, convey.ShouldContain, "run-sarek-accession")
		convey.So(runKeys, convey.ShouldNotContain, "run-unrelated-accession")
	})

	convey.Convey("G1.4: Given library search via mlwh and an in-memory fixture, then matching results return within 1 second", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-sample", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "LIB-S1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-direct", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_librarytype": "Standard"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindLibraryType)
				convey.So(canonical, convey.ShouldEqual, "Standard")

				return mlwh.SearchValues{Samples: []string{"LIB-S1"}, Runs: []string{"100"}, Lanes: []string{"100_1#1"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		started := time.Now()
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?library=Standard", nil)
		elapsed := time.Since(started)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(elapsed < time.Second, convey.ShouldBeTrue)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)
		convey.So(results, convey.ShouldHaveLength, 2)
	})

	convey.Convey("G1.4: Given seqmeta_libraryid search via mlwh, then it uses library ID expansion and exact library ID metadata", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-id-direct", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_libraryid": "71046409"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-id-expanded-sample", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "LIB-ID-S1"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-type-lookalike", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Metadata = map[string]string{"seqmeta_librarytype": "71046409"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindLibraryID)
				convey.So(canonical, convey.ShouldEqual, "71046409")

				return mlwh.SearchValues{Samples: []string{"LIB-ID-S1"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_libraryid=71046409", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		runKeys := make([]string, len(results))
		for i, result := range results {
			runKeys[i] = result.RunKey
		}

		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(runKeys, convey.ShouldContain, "run-library-id-direct")
		convey.So(runKeys, convey.ShouldContain, "run-library-id-expanded-sample")
		convey.So(runKeys, convey.ShouldNotContain, "run-library-type-lookalike")
	})

	convey.Convey("Bug 260519-1: Given seqmeta_id_sample_lims search via mlwh, then it uses sample LIMS expansion and finds sample-name metadata", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-lims-clicked", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_name": "SANG-LIMS"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-lims-unrelated", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_name": "OTHER"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindSampleLimsID)
				convey.So(canonical, convey.ShouldEqual, "12345")

				return mlwh.SearchValues{Samples: []string{"SANG-LIMS"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_id_sample_lims=12345", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(expander.expandCalls, convey.ShouldEqual, 1)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-sample-lims-clicked")
	})

	convey.Convey("Bug 260519-1: Given seqmeta_supplier_name search via mlwh, then it expands supplier metadata to the related sample result", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-clicked", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_name": "SANG-SUPPLIER"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-unrelated", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_name": "OTHER"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindSupplierName)
				convey.So(canonical, convey.ShouldEqual, "Supplier_Sample_Name")

				return mlwh.SearchValues{Samples: []string{"SANG-SUPPLIER"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_supplier_name=Supplier_Sample_Name", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(expander.expandCalls, convey.ShouldEqual, 1)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-supplier-clicked")
	})

	convey.Convey("Bug 260519-2: Given seqmeta_supplier_name search via mlwh, then direct sample metadata fallback uses ExpandSearchValues", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-fast", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_name": "7607STDY14643771"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindSupplierName)
				convey.So(canonical, convey.ShouldEqual, "Hek_R1")

				return mlwh.SearchValues{Samples: []string{"7607STDY14643771"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_supplier_name=Hek_R1", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(expander.expandCalls, convey.ShouldEqual, 1)
		convey.So(expander.sampleOnlyCalls, convey.ShouldEqual, 0)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-supplier-fast")
	})

	convey.Convey("Bug 260519-2: Given seqmeta_supplier_name search through the real results path, then registered sample candidates avoid a cold global supplier scan", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-candidate", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_name": "7607STDY14643771"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error) {
				return mlwh.SearchValues{}, fmt.Errorf("full expansion must not run for direct sample metadata")
			},
			sampleNamesFunc: func(context.Context, mlwh.IdentifierKind, string) ([]string, error) {
				return nil, fmt.Errorf("global direct sample metadata expansion must not run")
			},
			sampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "7607STDY14643771")

				return mlwh.Match{
					Kind:      mlwh.KindSangerSampleName,
					Canonical: raw,
					Sample: &mlwh.Sample{
						Name:         raw,
						SupplierName: "Hek_R1",
					},
				}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_supplier_name=Hek_R1", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(expander.expandCalls, convey.ShouldEqual, 0)
		convey.So(expander.sampleOnlyCalls, convey.ShouldEqual, 0)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-supplier-candidate")
	})

	convey.Convey("Bug PR8 SQL review: Given direct sample metadata search, then candidate resolution only reads canonical sample-name metadata values", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-candidate", func(reg *Registration) {
			reg.Metadata = map[string]string{
				"seqmeta_name":             "7607STDY14643771",
				"seqmeta_id_sample_lims":   "12345",
				"seqmeta_sanger_sample_id": "SS-12345",
			}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-direct", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-direct"
			reg.Metadata = map[string]string{"seqmeta_supplier_name": "Hek_R1"}
		}))

		resolvedCandidates := []string{}
		expander := &mockSearchExpander{
			searchValuesFunc: func(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error) {
				return mlwh.SearchValues{}, fmt.Errorf("full expansion must not run for direct sample metadata")
			},
			sampleNamesFunc: func(context.Context, mlwh.IdentifierKind, string) ([]string, error) {
				return nil, fmt.Errorf("global direct sample metadata expansion must not run")
			},
			sampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				resolvedCandidates = append(resolvedCandidates, raw)
				if raw != "7607STDY14643771" {
					return mlwh.Match{}, mlwh.ErrNotFound
				}

				return mlwh.Match{
					Kind:      mlwh.KindSangerSampleName,
					Canonical: raw,
					Sample: &mlwh.Sample{
						Name:         raw,
						SupplierName: "Hek_R1",
					},
				}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_supplier_name=Hek_R1", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(expander.expandCalls, convey.ShouldEqual, 0)
		convey.So(expander.sampleOnlyCalls, convey.ShouldEqual, 0)
		convey.So(resolvedCandidates, convey.ShouldResemble, []string{"7607STDY14643771"})
		convey.So(resolvedCandidates, convey.ShouldNotContain, "12345")
		convey.So(resolvedCandidates, convey.ShouldNotContain, "SS-12345")

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		runKeys := make([]string, len(results))
		for i, result := range results {
			runKeys[i] = result.RunKey
		}

		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(runKeys, convey.ShouldContain, "run-supplier-candidate")
		convey.So(runKeys, convey.ShouldContain, "run-supplier-direct")
	})

	convey.Convey("Bug 260519-1: Given seqmeta_supplier_name search with no mlwh relation, then a lookalike sample name is not matched", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-direct", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_supplier_name": "Supplier-Lookalike"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-lookalike", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_name": "Supplier-Lookalike"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindSupplierName)
				convey.So(canonical, convey.ShouldEqual, "Supplier-Lookalike")

				return mlwh.SearchValues{}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_supplier_name=Supplier-Lookalike", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(expander.expandCalls, convey.ShouldEqual, 1)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-supplier-direct")
	})

	convey.Convey("Bug 260519-1: Given seqmeta_id_sample_lims search with no mlwh relation, then a lookalike direct sample field is not matched", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-lims-direct", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_id_sample_lims": "12345"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sanger-id-lookalike", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sanger_sample_id": "12345"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindSampleLimsID)
				convey.So(canonical, convey.ShouldEqual, "12345")

				return mlwh.SearchValues{}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?seqmeta_id_sample_lims=12345", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(expander.expandCalls, convey.ShouldEqual, 1)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)

		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-sample-lims-direct")
	})

	convey.Convey("G1.4: Given library=Custom search via mlwh, then existing library type support still expands by type", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-type-direct", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_librarytype": "Custom"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-library-type-expanded-sample", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "LIB-TYPE-S1"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindLibraryType)
				convey.So(canonical, convey.ShouldEqual, "Custom")

				return mlwh.SearchValues{Samples: []string{"LIB-TYPE-S1"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?library=Custom", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)
		convey.So(results, convey.ShouldHaveLength, 2)
	})

	convey.Convey("G1.5: Given run search via mlwh, then expanded sample matches are returned via OR logic", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-direct-run", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_runid": "100"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-expanded-sample", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"seqmeta_sampleid": "RUN-S1"}
		}))

		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindRunID)
				convey.So(canonical, convey.ShouldEqual, "100")

				return mlwh.SearchValues{Samples: []string{"RUN-S1"}, Runs: []string{"100"}}, nil
			},
		}

		resolver := NewMLWHSearchResolver(expander)
		response := performResultsRequestForTest(t, NewServer(store, nil, resolver).Handler(), http.MethodGet, "/results?run=100", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var results []ResultSet
		decodeJSONResponseForTest(t, response, &results)
		convey.So(results, convey.ShouldHaveLength, 2)
	})
}

func testServerRegistration(t *testing.T) *Registration {
	t.Helper()

	reg := testRegistration()
	outputDirectory := t.TempDir()
	reg.OutputDirectory = outputDirectory

	for i := range reg.Files {
		if reg.Files[i].Kind == "output" {
			reg.Files[i].Path = filepath.Join(outputDirectory, filepath.Base(reg.Files[i].Path))
		}
	}

	return reg
}

func metadataValuesForResultForTest(t *testing.T, store *Store, resultID string, key string) []string {
	t.Helper()

	rows, err := store.db.Query(
		`SELECT value FROM result_metadata WHERE result_id = ? AND meta_key = ? ORDER BY value_ordinal`,
		resultID,
		key,
	)
	if err != nil {
		t.Fatalf("query result metadata values: %v", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatalf("scan result metadata value: %v", err)
		}

		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate result metadata values: %v", err)
	}

	return values
}

func statGIDForTest(t *testing.T, path string) int64 {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat output directory: %v", err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat output directory: missing syscall stat data")
	}

	return int64(stat.Gid)
}

func replacementOutputFilesForTest() []FileEntry {
	return []FileEntry{{
		Path:  "/tmp/results/run/out-new.txt",
		Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC),
		Size:  404,
		Kind:  "output",
	}}
}

func assertStoredResultActorsForTest(t *testing.T, store *Store, response *httptest.ResponseRecorder, requester, operator string) {
	t.Helper()

	var result ResultSet
	decodeJSONResponseForTest(t, response, &result)

	stored, err := store.Get(context.Background(), result.ID)
	convey.So(err, convey.ShouldBeNil)
	convey.So(stored.Requester, convey.ShouldEqual, requester)
	convey.So(stored.Operator, convey.ShouldEqual, operator)
	convey.So(result.Requester, convey.ShouldEqual, requester)
	convey.So(result.Operator, convey.ShouldEqual, operator)
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

func TestServerGetSearchSuggestions(t *testing.T) {
	convey.Convey("Given registered values, GET /results/search-suggestions returns typed substring matches", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-suggestion", func(reg *Registration) {
			reg.Requester = "requester-needle-260618"
			reg.Metadata = map[string]string{
				"assay_tag":        "alpha-needle-260618-omega",
				"seqmeta_sampleid": "SAMPLE-needle-260618",
			}
		}))

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/search-suggestions?q=needle-260618", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var suggestions []SearchSuggestion
		decodeJSONResponseForTest(t, response, &suggestions)
		convey.So(suggestionValuesByFieldForTest(suggestions), convey.ShouldResemble, map[string][]string{
			"meta_assay_tag": {"alpha-needle-260618-omega"},
			"sample":         {"SAMPLE-needle-260618"},
			"user":           {"requester-needle-260618"},
		})
	})

	convey.Convey("Bug 260618-5 item 2: Given a registered sample and an MLWH supplier-name lookup, GET /results/search-suggestions offers a Sample filter for the supplier name", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-supplier-generic-suggestion", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaSampleNameKey: "7607STDY14643771"}
		}))

		expander := &mockSearchExpander{
			classifyFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw == "Hek_R1" {
					return mlwh.Match{
						Kind:      mlwh.KindSupplierName,
						Canonical: "7607STDY14643771",
						Sample: &mlwh.Sample{
							Name:         "7607STDY14643771",
							SupplierName: raw,
						},
					}, nil
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			sampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw == "7607STDY14643771" {
					return mlwh.Match{
						Kind:      mlwh.KindSangerSampleName,
						Canonical: raw,
						Sample: &mlwh.Sample{
							Name:         raw,
							SupplierName: "Hek_R1",
						},
					}, nil
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			sampleFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw == "Hek_R1" {
					return mlwh.Match{
						Kind:      mlwh.KindSupplierName,
						Canonical: "7607STDY14643771",
						Sample: &mlwh.Sample{
							Name:         "7607STDY14643771",
							SupplierName: raw,
						},
					}, nil
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				if kind == mlwh.KindSupplierName && canonical == "Hek_R1" {
					return mlwh.SearchValues{Samples: []string{"7607STDY14643771"}}, nil
				}

				return mlwh.SearchValues{}, mlwh.ErrNotFound
			},
		}

		response := performResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler(), http.MethodGet, "/results/search-suggestions?q=Hek_R1", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var suggestions []SearchSuggestion
		decodeJSONResponseForTest(t, response, &suggestions)
		convey.So(expander.classifyCalls, convey.ShouldResemble, []string{"Hek_R1"})
		convey.So(suggestionValuesByFieldForTest(suggestions)["sample"], convey.ShouldContain, "Hek_R1")
	})

	convey.Convey("Bug 260627-4: Given a study whose title contains the typed word and is registered by its LIMS id, GET /results/search-suggestions offers a Study filter for the LIMS id labelled with the title", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-title-word", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaIDStudyLimsKey: "6568"}
		}))

		expander := &mockSearchExpander{
			searchStudiesFunc: func(_ context.Context, term string, _, _ int) ([]mlwh.Study, error) {
				if term == "diversity" {
					return []mlwh.Study{
						{IDStudyLims: "6568", Name: "DIV", StudyTitle: "Microbial diversity of soil"},
					}, nil
				}

				return []mlwh.Study{}, nil
			},
		}

		response := performResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler(), http.MethodGet, "/results/search-suggestions?q=diversity", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var suggestions []SearchSuggestion
		decodeJSONResponseForTest(t, response, &suggestions)
		studySuggestion := suggestionByFieldValueForTest(suggestions, "study", "6568")
		convey.So(studySuggestion, convey.ShouldNotBeNil)
		convey.So(studySuggestion.Label, convey.ShouldContainSubstring, "Microbial diversity of soil")
	})

	convey.Convey("Bug 260627-4: Given a study whose title contains the typed word but is NOT registered, GET /results/search-suggestions offers no Study filter", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-study-title-unregistered", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaIDStudyLimsKey: "9999"}
		}))

		expander := &mockSearchExpander{
			searchStudiesFunc: func(_ context.Context, term string, _, _ int) ([]mlwh.Study, error) {
				if term == "diversity" {
					return []mlwh.Study{
						{IDStudyLims: "6568", Name: "DIV", StudyTitle: "Microbial diversity of soil"},
					}, nil
				}

				return []mlwh.Study{}, nil
			},
		}

		response := performResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler(), http.MethodGet, "/results/search-suggestions?q=diversity", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var suggestions []SearchSuggestion
		decodeJSONResponseForTest(t, response, &suggestions)
		convey.So(suggestionByFieldValueForTest(suggestions, "study", "6568"), convey.ShouldBeNil)
	})

	convey.Convey("Bug 260627-4: Given a sample matched by word-prefix on its supplier name and registered, GET /results/search-suggestions offers a Sample filter for the canonical name", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-sample-word-prefix", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaSampleNameKey: "SANG1"}
		}))

		expander := &mockSearchExpander{
			searchSamplesFunc: func(_ context.Context, term string, _, _ int) ([]mlwh.Sample, error) {
				if term == "musculus" {
					return []mlwh.Sample{
						{Name: "SANG1", SupplierName: "Mus musculus liver", CommonName: "house mouse"},
					}, nil
				}

				return []mlwh.Sample{}, nil
			},
		}

		response := performResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler(), http.MethodGet, "/results/search-suggestions?q=musculus", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var suggestions []SearchSuggestion
		decodeJSONResponseForTest(t, response, &suggestions)
		convey.So(suggestionByFieldValueForTest(suggestions, "sample", "SANG1"), convey.ShouldNotBeNil)
	})

	convey.Convey("Bug 260627-4: Given both an exact-classify match and a title-word study match, exact-classify suggestions still come first", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-exact-and-substring", func(reg *Registration) {
			reg.Metadata = map[string]string{
				SeqmetaIDStudyLimsKey: "6568",
				SeqmetaSampleNameKey:  "diversitySAMPLE",
			}
		}))

		expander := &mockSearchExpander{
			classifyFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw == "diversitySAMPLE" {
					return mlwh.Match{
						Kind:      mlwh.KindSangerSampleName,
						Canonical: "diversitySAMPLE",
						Sample:    &mlwh.Sample{Name: "diversitySAMPLE"},
					}, nil
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			searchStudiesFunc: func(_ context.Context, term string, _, _ int) ([]mlwh.Study, error) {
				if strings.HasPrefix("diversitySAMPLE", term) || term == "diversitySAMPLE" {
					return []mlwh.Study{
						{IDStudyLims: "6568", StudyTitle: "diversitySAMPLE study"},
					}, nil
				}

				return []mlwh.Study{}, nil
			},
		}

		response := performResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler(), http.MethodGet, "/results/search-suggestions?q=diversitySAMPLE", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var suggestions []SearchSuggestion
		decodeJSONResponseForTest(t, response, &suggestions)
		convey.So(len(suggestions), convey.ShouldBeGreaterThanOrEqualTo, 2)
		exactIndex := suggestionIndexForFieldValueForTest(suggestions, "sample", "diversitySAMPLE")
		substringIndex := suggestionIndexForFieldValueForTest(suggestions, "study", "6568")
		convey.So(exactIndex, convey.ShouldBeGreaterThanOrEqualTo, 0)
		convey.So(substringIndex, convey.ShouldBeGreaterThanOrEqualTo, 0)
		convey.So(exactIndex, convey.ShouldBeLessThan, substringIndex)
	})

	convey.Convey("Bug 260627-4: Given the MLWH cache has never been synced, substring search degrades to no MLWH suggestions without a 502", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-never-synced", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaIDStudyLimsKey: "6568"}
		}))

		expander := &mockSearchExpander{
			searchStudiesFunc: func(context.Context, string, int, int) ([]mlwh.Study, error) {
				return nil, fmt.Errorf("%w: %w", mlwh.ErrCacheNeverSynced, mlwh.ErrNotFound)
			},
			searchSamplesFunc: func(context.Context, string, int, int) ([]mlwh.Sample, error) {
				return nil, fmt.Errorf("%w: %w", mlwh.ErrCacheNeverSynced, mlwh.ErrNotFound)
			},
		}

		response := performResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler(), http.MethodGet, "/results/search-suggestions?q=diversity", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var suggestions []SearchSuggestion
		decodeJSONResponseForTest(t, response, &suggestions)
		convey.So(suggestionByFieldValueForTest(suggestions, "study", "6568"), convey.ShouldBeNil)
	})

	convey.Convey("Bug 260627-4: Given a genuine upstream MLWH failure during substring search, GET /results/search-suggestions surfaces a 502", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-upstream-failure", func(reg *Registration) {
			reg.Metadata = map[string]string{SeqmetaIDStudyLimsKey: "6568"}
		}))

		expander := &mockSearchExpander{
			searchStudiesFunc: func(context.Context, string, int, int) ([]mlwh.Study, error) {
				return nil, errors.New("connection refused")
			},
		}

		response := performResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler(), http.MethodGet, "/results/search-suggestions?q=diversity", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadGateway)
	})

	convey.Convey("GET /results/search-suggestions skips store and MLWH suggestions for one-character generic queries", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-short-suggestion", func(reg *Registration) {
			reg.Requester = "alice"
		}))
		expander := &mockSearchExpander{}

		response := performResultsRequestForTest(t, NewServer(store, nil, NewMLWHSearchResolver(expander)).Handler(), http.MethodGet, "/results/search-suggestions?q=a", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, "[]\n")
		convey.So(expander.classifyCalls, convey.ShouldBeEmpty)
	})

	convey.Convey("GET /results/search-suggestions rejects invalid limits", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/search-suggestions?q=needle&limit=-1", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldEqual, "invalid limit query parameter")
	})
}

func TestServerDeleteResult(t *testing.T) {
	convey.Convey("E6.1: Given a stored result set, when DELETE /results/{id} is called, then status is 204 and subsequent GET /results/{id} returns 404", t, func() {
		store := newSQLiteStoreForTest(t)
		result, err := store.Upsert(t.Context(), testRegistration())
		convey.So(err, convey.ShouldBeNil)

		ownerToken := "owner-token-delete-e6"
		ownerStore := NewOwnerSessionStore()
		ownerStore.MarkOwner(ownerToken, time.Now().Add(time.Hour))
		server := NewServer(store, nil, nil, WithOwnerSessionStore(ownerStore))
		router := newResultsGinHandlerForTest(t, server, &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})

		deleteResponse := performResultsJWTRequestForTest(t, router, http.MethodDelete, gas.EndPointAuth+"/results/"+result.ID, ownerToken, nil)
		getResponse := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+result.ID, nil)

		convey.So(deleteResponse.Code, convey.ShouldEqual, http.StatusNoContent)
		convey.So(deleteResponse.Body.Len(), convey.ShouldEqual, 0)
		convey.So(getResponse.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(errorResponseBodyForTest(t, getResponse), convey.ShouldNotBeBlank)
	})

	convey.Convey("E6.2: Given a non-existent ID, when DELETE /results/{id} is called, then status is 404", t, func() {
		store := newSQLiteStoreForTest(t)
		ownerToken := "owner-token-delete-missing-e6"
		ownerStore := NewOwnerSessionStore()
		ownerStore.MarkOwner(ownerToken, time.Now().Add(time.Hour))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil, WithOwnerSessionStore(ownerStore)), &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})

		response := performResultsJWTRequestForTest(t, router, http.MethodDelete, gas.EndPointAuth+"/results/missing-id", ownerToken, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldNotBeBlank)
	})
}

func TestServerGetResultByID(t *testing.T) {
	convey.Convey("E3.1: Given a stored result set, when authenticated GET /results/<valid-id> is called, then status is 200 and body matches the stored data including metadata", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := t.Context()
		reg := testRegistration()
		reg.Metadata = map[string]string{"library": "exon", "study": "alpha"}
		reg.OutputDirectoryGID = gidForTest(200)

		stored, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})
		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results/"+stored.ID, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		var result ResultSet
		decodeJSONResponseForTest(t, response, &result)

		expected := *stored
		expected.Access = AccessState{CanView: true}
		convey.So(result, convey.ShouldResemble, expected)
	})

	convey.Convey("E3.2: Given a non-existent ID, when GET /results/<id> is called, then status is 404 with an error key", t, func() {
		store := newSQLiteStoreForTest(t)

		response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results/missing-id", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldEqual, ErrNotFound.Error()+`: result set "missing-id"`)
	})
}

func TestServerGetResultFiles(t *testing.T) {
	convey.Convey("E4.1: Given a result set with 5 files (3 output, 1 input, 1 pipeline), when authenticated GET /results/{id}/files, then status 200 and JSON array has 5 entries with correct kind values", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.OutputDirectoryGID = gidForTest(200)
		reg.Files = []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "output"},
			{Path: "/tmp/results/run/out-3.txt", Mtime: time.Date(2026, time.April, 1, 12, 3, 0, 0, time.UTC), Size: 404, Kind: "output"},
			{Path: "/tmp/pipeline.nf", Mtime: time.Date(2026, time.April, 1, 11, 59, 0, 0, time.UTC), Size: 505, Kind: "pipeline"},
		}

		result, err := store.Upsert(t.Context(), reg)
		convey.So(err, convey.ShouldBeNil)

		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil), &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})
		response := performResultsRequestForTest(t, router, http.MethodGet, gas.EndPointAuth+"/results/"+result.ID+"/files", nil)

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
		reg.OutputDirectoryGID = gidForTest(200)
		reg.Files = []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "output"},
		}

		result, err := store.Upsert(context.Background(), reg)
		convey.So(err, convey.ShouldBeNil)

		ownerToken := "owner-token-put-files-e5"
		ownerStore := NewOwnerSessionStore()
		ownerStore.MarkOwner(ownerToken, time.Now().Add(time.Hour))
		server := NewServer(store, nil, nil, WithOwnerSessionStore(ownerStore))
		ownerHandler := newResultsGinHandlerForTest(t, server, &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})

		replacement := []FileEntry{
			{Path: "/tmp/results/run/out-new-1.txt", Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC), Size: 404, Kind: "output"},
			{Path: "/tmp/results/run/out-new-2.txt", Mtime: time.Date(2026, time.April, 2, 12, 1, 0, 0, time.UTC), Size: 505, Kind: "output"},
			{Path: "/tmp/results/run/out-new-3.txt", Mtime: time.Date(2026, time.April, 2, 12, 2, 0, 0, time.UTC), Size: 606, Kind: "output"},
		}

		response := performResultsJWTRequestForTest(
			t,
			ownerHandler,
			http.MethodPut,
			gas.EndPointAuth+"/results/"+result.ID+"/files",
			ownerToken,
			mustJSONBodyForTest(t, replacement),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/json")

		authHandler := newResultsGinHandlerForTest(t, server, &CurrentUser{
			Username: "alice",
			User:     authUserForTest{},
		})
		getResponse := performResultsRequestForTest(t, authHandler, http.MethodGet, gas.EndPointAuth+"/results/"+result.ID+"/files", nil)

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
		ownerToken := "owner-token-put-files-missing-e5"
		ownerStore := NewOwnerSessionStore()
		ownerStore.MarkOwner(ownerToken, time.Now().Add(time.Hour))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil, WithOwnerSessionStore(ownerStore)), &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})

		response := performResultsJWTRequestForTest(
			t,
			router,
			http.MethodPut,
			gas.EndPointAuth+"/results/missing-id/files",
			ownerToken,
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
		ownerToken := "owner-token-put-files-malformed-e5"
		ownerStore := NewOwnerSessionStore()
		ownerStore.MarkOwner(ownerToken, time.Now().Add(time.Hour))
		router := newResultsGinHandlerForTest(t, NewServer(store, nil, nil, WithOwnerSessionStore(ownerStore)), &CurrentUser{
			Username: "svc",
			User:     authUserForTest{},
		})

		response := performResultsJWTRequestForTest(
			t,
			router,
			http.MethodPut,
			gas.EndPointAuth+"/results/any-id/files",
			ownerToken,
			[]byte(`[{"path":`),
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldNotBeBlank)
	})
}

func performResultsRequestForTest(t *testing.T, handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	path = normalizeResultsPathForTest(method, path)
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
