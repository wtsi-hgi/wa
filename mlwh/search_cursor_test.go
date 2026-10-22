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

package mlwh

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

var (
	searchCursorFirstSync  = time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	searchCursorSecondSync = searchCursorFirstSync.Add(30 * time.Minute)
)

// seedSearchCursorScenario seeds the studies and samples the cursor tests page
// through. Under words=true the term "acme" streams the token index as
// (acme,11) (acme,14) (acme,17) (acmeb,12) (acmeb,15) (acmeb,16) (acmec,16)
// (acmez,11) (acmez,12) (acmez,13): samples 11, 12 and 16 own a later
// matching token too, so a cursor page must not return them again. Sample 18
// matches nothing. Studies 9003 appear twice, so the (id_study_lims,
// id_study_tmp) key needs its tie-breaker.
func seedSearchCursorScenario(t *testing.T, db *sql.DB, dialect string) {
	t.Helper()

	for _, study := range []struct {
		id   int64
		lims string
	}{{46, "9005"}, {41, "9001"}, {44, "9003"}, {43, "9004"}, {40, "9003"}, {45, "9002"}, {42, "9006"}} {
		seedStudyMirrorSearchRow(t, db, study.id, study.lims, "name "+study.lims, "A cursor study", "Genomics", "Sponsor")
	}
	seedStudyMirrorSearchRow(t, db, 47, "9000", "other", "Unrelated", "Genomics", "Sponsor")

	for _, sample := range []struct {
		id                          int64
		name, supplier, common, don string
	}{
		{11, "ACME-011", "acmez", "Homo sapiens", "d11"},
		{12, "ZED-012", "acmeb", "Mus musculus", "acmez"},
		{13, "ZED-013", "acmez", "Mus musculus", "d13"},
		{14, "ACME-014", "x14", "Homo sapiens", "d14"},
		{15, "ZED-015", "acmeb-1", "Mus musculus", "d15"},
		{16, "ZED-016", "acmec", "Homo sapiens", "acmeb"},
		{17, "ACME-017", "x17", "Homo sapiens", "d17"},
		{18, "ZED-018", "nomatch", "Homo sapiens", "d18"},
	} {
		seedSampleMirrorSearchRow(t, db, sample.id, sample.name, sample.supplier, sample.common, sample.don)
	}
	for _, id := range []int64{12, 14, 15, 17} {
		seedLibrarySample(t, db, "Standard", id, "9001")
	}

	rebuildSampleSearchIndexForTestDialect(t, db, dialect)
	rebuildCommonNameWordMirrorForTest(t, db)
	seedSyncStateRun(t, db, syncTableStudy, searchCursorFirstSync, searchCursorFirstSync)
	seedSyncStateRun(t, db, syncTableSample, searchCursorFirstSync, searchCursorFirstSync)
}

// simulateSearchCursorSync applies a sync between two cursor pages: sample 10
// (sorting before any cursor) and sample 30 newly match "acme", and the sample
// table's last_run advances.
func simulateSearchCursorSync(t *testing.T, db *sql.DB, dialect string) {
	t.Helper()

	seedSampleMirrorSearchRow(t, db, 10, "ACME-010", "x10", "Homo sapiens", "d10")
	seedSampleMirrorSearchRow(t, db, 30, "ACME-030", "x30", "Homo sapiens", "d30")
	rebuildSampleSearchIndexForTestDialect(t, db, dialect)
	if _, err := db.Exec(`UPDATE sync_state SET last_run = ? WHERE table_name = ?`, formatSyncTime(searchCursorSecondSync), syncTableSample); err != nil {
		t.Fatalf("advance sample last_run: %v", err)
	}
}

func sampleCursorPages(client *Client, term string, opts SampleSearchOptions, limit int) ([][]int64, []bool, error) {
	var (
		pages  [][]int64
		stales []bool
		cursor string
	)
	for range 50 {
		page, err := client.SearchSamplesCursor(context.Background(), term, opts, SearchPageOptions{Limit: limit, Cursor: cursor})
		if err != nil {
			return nil, nil, err
		}
		pages = append(pages, sampleTmpIDs(page.Items))
		stales = append(stales, page.Stale)
		if page.NextCursor == "" {
			return pages, stales, nil
		}
		cursor = page.NextCursor
	}

	return nil, nil, errors.New("cursor paging did not end")
}

func sampleOffsetPages(client *Client, term string, opts SampleSearchOptions, limit int) ([][]int64, error) {
	var pages [][]int64
	for offset := 0; ; offset += limit {
		page, err := client.SearchSamplesCursor(context.Background(), term, opts, SearchPageOptions{Limit: limit, Offset: offset})
		if err != nil {
			return nil, err
		}
		if len(page.Items) == 0 {
			return pages, nil
		}
		pages = append(pages, sampleTmpIDs(page.Items))
	}
}

func studyCursorPages(client *Client, term string, limit int) ([][]int64, error) {
	var (
		pages  [][]int64
		cursor string
	)
	for range 50 {
		page, err := client.SearchStudiesCursor(context.Background(), term, SearchPageOptions{Limit: limit, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		ids := make([]int64, len(page.Items))
		for index, study := range page.Items {
			ids[index] = study.IDStudyTmp
		}
		pages = append(pages, ids)
		if page.NextCursor == "" {
			return pages, nil
		}
		cursor = page.NextCursor
	}

	return nil, errors.New("cursor paging did not end")
}

func flattenIDs(pages [][]int64) []int64 {
	var all []int64
	for _, page := range pages {
		all = append(all, page...)
	}

	return all
}

func TestSearchCursorPaging(t *testing.T) {
	convey.Convey("Given a SQLite cache with matching studies and samples", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedSearchCursorScenario(t, cache.DB(), "sqlite")
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		assertSearchCursorPaging(t, client)

		convey.Convey("when the cache syncs between cursor pages", func() {
			assertSearchCursorAcrossSync(t, client, cache.DB(), "sqlite")
		})
	})
}

func TestRealMySQLSearchCursorPaging(t *testing.T) {
	client := openThrowawayMySQLExportClientForTest(t)
	seedSearchCursorScenario(t, client.cache.DB(), "mysql")

	convey.Convey("Given matching studies and samples in a throwaway MySQL cache", t, func() {
		assertSearchCursorPaging(t, client)

		convey.Convey("when the cache syncs between cursor pages", func() {
			assertSearchCursorAcrossSync(t, client, client.cache.DB(), "mysql")
		})
	})
}

func assertSearchCursorPaging(t *testing.T, client *Client) {
	t.Helper()

	convey.Convey("studies paged by cursor equal one big page, in (id_study_lims, id_study_tmp) order", func() {
		big, err := client.SearchStudiesCursor(context.Background(), "cursor study", SearchPageOptions{Limit: 100})
		convey.So(err, convey.ShouldBeNil)
		convey.So(big.NextCursor, convey.ShouldBeEmpty)

		for _, limit := range []int{1, 2, 3} {
			pages, pageErr := studyCursorPages(client, "cursor study", limit)
			convey.So(pageErr, convey.ShouldBeNil)
			convey.So(flattenIDs(pages), convey.ShouldResemble, []int64{41, 45, 40, 44, 43, 46, 42})
		}
	})

	for _, mode := range []struct {
		name string
		term string
		opts SampleSearchOptions
		want []int64
	}{
		{"the default literal prefix", "acme", SampleSearchOptions{}, []int64{11, 12, 13, 14, 15, 16, 17}},
		{"a multi-word words search", "acme zed", SampleSearchOptions{Words: true}, []int64{12, 13, 15, 16}},
		{"an organism filter with no term", "", SampleSearchOptions{Organism: "homo sapiens"}, []int64{11, 14, 16, 17, 18}},
		{"an organism filter with a term", "acme", SampleSearchOptions{Organism: "homo sapiens"}, []int64{11, 14, 16, 17}},
		{"a library type filter with no term", "", SampleSearchOptions{LibraryType: "Standard"}, []int64{12, 14, 15, 17}},
		{"a library type filter with a words term", "acme", SampleSearchOptions{Words: true, LibraryType: "Standard"}, []int64{14, 17, 12, 15}},
	} {
		convey.Convey("samples of "+mode.name+" paged by cursor equal the offset pages and one big page", func() {
			big, err := client.SearchSamplesCursor(context.Background(), mode.term, mode.opts, SearchPageOptions{Limit: 100})
			convey.So(err, convey.ShouldBeNil)
			convey.So(big.NextCursor, convey.ShouldBeEmpty)
			convey.So(sampleTmpIDs(big.Items), convey.ShouldResemble, sortedIDs(mode.want))

			for _, limit := range []int{1, 2, 3} {
				pages, stales, pageErr := sampleCursorPages(client, mode.term, mode.opts, limit)
				convey.So(pageErr, convey.ShouldBeNil)
				offsetPages, offsetErr := sampleOffsetPages(client, mode.term, mode.opts, limit)
				convey.So(offsetErr, convey.ShouldBeNil)
				convey.So(pages, convey.ShouldResemble, offsetPages)
				convey.So(sortedIDs(flattenIDs(pages)), convey.ShouldResemble, sortedIDs(mode.want))
				convey.So(stales, convey.ShouldNotContain, true)
			}
		})
	}

	convey.Convey("a single-word words search pages its token index without repeating samples that own several matching tokens", func() {
		pages, _, err := sampleCursorPages(client, "acme", SampleSearchOptions{Words: true}, 2)
		convey.So(err, convey.ShouldBeNil)
		convey.So(pages, convey.ShouldResemble, [][]int64{{11, 14}, {12, 17}, {15, 16}, {13}})

		offsetPages, offsetErr := sampleOffsetPages(client, "acme", SampleSearchOptions{Words: true}, 2)
		convey.So(offsetErr, convey.ShouldBeNil)
		convey.So(pages, convey.ShouldResemble, offsetPages)
	})

	convey.Convey("an exact final page carries no next cursor", func() {
		page, err := client.SearchSamplesCursor(context.Background(), "acme", SampleSearchOptions{}, SearchPageOptions{Limit: 7})
		convey.So(err, convey.ShouldBeNil)
		convey.So(page.Items, convey.ShouldHaveLength, 7)
		convey.So(page.NextCursor, convey.ShouldBeEmpty)
	})

	convey.Convey("a bad cursor is a bad request", func() {
		first, err := client.SearchSamplesCursor(context.Background(), "acme", SampleSearchOptions{}, SearchPageOptions{Limit: 2})
		convey.So(err, convey.ShouldBeNil)
		studyFirst, err := client.SearchStudiesCursor(context.Background(), "cursor study", SearchPageOptions{Limit: 2})
		convey.So(err, convey.ShouldBeNil)

		for _, tc := range []struct {
			term string
			opts SampleSearchOptions
			page SearchPageOptions
			want error
		}{
			{"acme", SampleSearchOptions{}, SearchPageOptions{Limit: 2, Cursor: "not a cursor!"}, errInvalidSearchCursor},
			{"acme", SampleSearchOptions{}, SearchPageOptions{Limit: 2, Cursor: "MQ"}, errInvalidSearchCursor},
			{"acme", SampleSearchOptions{}, SearchPageOptions{Limit: 2, Offset: 2, Cursor: first.NextCursor}, errSearchCursorWithOffset},
			{"acmez", SampleSearchOptions{}, SearchPageOptions{Limit: 2, Cursor: first.NextCursor}, errSearchCursorMismatch},
			{"acme", SampleSearchOptions{Words: true}, SearchPageOptions{Limit: 2, Cursor: first.NextCursor}, errSearchCursorMismatch},
			{"acme", SampleSearchOptions{}, SearchPageOptions{Limit: 2, Cursor: studyFirst.NextCursor}, errSearchCursorMismatch},
		} {
			_, searchErr := client.SearchSamplesCursor(context.Background(), tc.term, tc.opts, tc.page)
			convey.So(errors.Is(searchErr, ErrBadRequest), convey.ShouldBeTrue)
			convey.So(errors.Is(searchErr, tc.want), convey.ShouldBeTrue)
		}
	})

	convey.Convey("the search routes page by cursor and reject bad cursors with 400", func() {
		assertSearchCursorRoutes(t, client)
	})
}

func sortedIDs(ids []int64) []int64 {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)

	return sorted
}

func assertSearchCursorAcrossSync(t *testing.T, client *Client, db *sql.DB, dialect string) {
	t.Helper()

	first, err := client.SearchSamplesCursor(context.Background(), "acme", SampleSearchOptions{}, SearchPageOptions{Limit: 3})
	convey.So(err, convey.ShouldBeNil)
	convey.So(sampleTmpIDs(first.Items), convey.ShouldResemble, []int64{11, 12, 13})
	convey.So(first.Stale, convey.ShouldBeFalse)

	studyFirst, err := client.SearchStudiesCursor(context.Background(), "cursor study", SearchPageOptions{Limit: 3})
	convey.So(err, convey.ShouldBeNil)

	// The sync runs once, so the checks below share one Convey leaf: nested
	// leaves would re-run it against the shared MySQL cache.
	simulateSearchCursorSync(t, db, dialect)

	// Later pages keep every row after the cursor exactly once, gain the new
	// row after it, miss the new row before it, and warn.
	var (
		got    []int64
		cursor = first.NextCursor
	)
	for range 20 {
		if cursor == "" {
			break
		}
		page, pageErr := client.SearchSamplesCursor(context.Background(), "acme", SampleSearchOptions{}, SearchPageOptions{Limit: 3, Cursor: cursor})
		convey.So(pageErr, convey.ShouldBeNil)
		convey.So(page.Stale, convey.ShouldBeTrue)
		convey.So(page.Warnings(), convey.ShouldResemble, []string{SearchCursorStaleWarning})
		got = append(got, sampleTmpIDs(page.Items)...)
		cursor = page.NextCursor
	}
	convey.So(got, convey.ShouldResemble, []int64{14, 15, 16, 17, 30})

	// An offset page shifts instead: the insert before it repeats sample 13.
	offsetPage, offsetErr := client.SearchSamplesCursor(context.Background(), "acme", SampleSearchOptions{}, SearchPageOptions{Limit: 3, Offset: 3})
	convey.So(offsetErr, convey.ShouldBeNil)
	convey.So(sampleTmpIDs(offsetPage.Items), convey.ShouldResemble, []int64{13, 14, 15})

	// A study search whose own table did not sync is not stale.
	studyPage, studyErr := client.SearchStudiesCursor(context.Background(), "cursor study", SearchPageOptions{Limit: 3, Cursor: studyFirst.NextCursor})
	convey.So(studyErr, convey.ShouldBeNil)
	convey.So(studyPage.Stale, convey.ShouldBeFalse)

	// The search route sets X-Search-Warning on a stale cursor page only.
	response := performMLWHRequestForTest(t, client, http.MethodGet, "/search/sample/acme?limit=3&cursor="+first.NextCursor)
	convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
	convey.So(response.Header().Get(SearchWarningHeader), convey.ShouldEqual, SearchCursorStaleWarning)

	fresh := performMLWHRequestForTest(t, client, http.MethodGet, "/search/sample/acme?limit=3")
	convey.So(fresh.Header().Get(SearchWarningHeader), convey.ShouldBeEmpty)
}

func assertSearchCursorRoutes(t *testing.T, client *Client) {
	t.Helper()

	get := func(path string) *httptest.ResponseRecorder {
		return performMLWHRequestForTest(t, client, http.MethodGet, path)
	}

	first := get("/search/sample/acme?limit=3")
	convey.So(first.Code, convey.ShouldEqual, http.StatusOK)
	convey.So(first.Header().Get("X-Total-Count"), convey.ShouldEqual, "7")
	convey.So(first.Header().Get("X-Next-Offset"), convey.ShouldEqual, "3")
	cursor := first.Header().Get(SearchNextCursorHeader)
	convey.So(cursor, convey.ShouldNotBeEmpty)

	var ids []int64
	for range 20 {
		if cursor == "" {
			break
		}
		response := get("/search/sample/acme?limit=3&cursor=" + url.QueryEscape(cursor))
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "7")
		convey.So(response.Header().Values("X-Next-Offset"), convey.ShouldBeEmpty)
		convey.So(response.Header().Get(SearchWarningHeader), convey.ShouldBeEmpty)

		var samples []Sample
		decodeMLWHJSONResponseForTest(t, response, &samples)
		ids = append(ids, sampleTmpIDs(samples)...)
		cursor = response.Header().Get(SearchNextCursorHeader)
	}
	convey.So(ids, convey.ShouldResemble, []int64{14, 15, 16, 17})

	studies := get("/search/study/cursor%20study?limit=2")
	convey.So(studies.Code, convey.ShouldEqual, http.StatusOK)
	studyCursor := studies.Header().Get(SearchNextCursorHeader)
	convey.So(studyCursor, convey.ShouldNotBeEmpty)
	convey.So(get("/search/study/cursor%20study?limit=2&cursor="+studyCursor).Code, convey.ShouldEqual, http.StatusOK)

	sampleCursor := first.Header().Get(SearchNextCursorHeader)
	for _, path := range []string{
		"/search/sample/acme?cursor=not-a-cursor",
		"/search/sample/acme?offset=3&cursor=" + sampleCursor,
		"/search/sample/acmez?cursor=" + sampleCursor,
		"/search/sample/acme?words=true&cursor=" + sampleCursor,
		"/search/sample/acme?cursor=" + studyCursor,
		"/search/study/cursor%20study?offset=2&cursor=" + studyCursor,
		"/search/study/cursor%20study?cursor=" + sampleCursor,
	} {
		response := get(path)
		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(response.Body.String(), convey.ShouldContainSubstring, `"code":"bad_request"`)
	}
}

func TestRemoteClientSearchCursorPages(t *testing.T) {
	convey.Convey("Given a RemoteClient over a server backed by a SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedSearchCursorScenario(t, cache.DB(), "sqlite")
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		gin.SetMode(gin.TestMode)
		router := gin.New()
		NewServer(client).RegisterRoutes(router, nil)
		server := httptest.NewServer(router)
		defer server.Close()

		remote, err := NewRemoteClient(RemoteConfig{BaseURL: server.URL})
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when it follows sample and study cursors, then it reads every page once", func() {
			var (
				ids    []int64
				cursor string
			)
			for range 20 {
				page, pageErr := remote.SearchSamplesCursor(context.Background(), "acme", SampleSearchOptions{Words: true}, SearchPageOptions{Limit: 2, Cursor: cursor})
				convey.So(pageErr, convey.ShouldBeNil)
				ids = append(ids, sampleTmpIDs(page.Items)...)
				if page.NextCursor == "" {
					break
				}
				cursor = page.NextCursor
			}
			convey.So(ids, convey.ShouldResemble, []int64{11, 14, 12, 17, 15, 16, 13})

			studyPage, studyErr := remote.SearchStudiesCursor(context.Background(), "cursor study", SearchPageOptions{Limit: 6})
			convey.So(studyErr, convey.ShouldBeNil)
			convey.So(studyPage.Items, convey.ShouldHaveLength, 6)
			last, lastErr := remote.SearchStudiesCursor(context.Background(), "cursor study", SearchPageOptions{Limit: 6, Cursor: studyPage.NextCursor})
			convey.So(lastErr, convey.ShouldBeNil)
			convey.So(last.Items, convey.ShouldHaveLength, 1)
			convey.So(last.NextCursor, convey.ShouldBeEmpty)
		})

		convey.Convey("when it sends a cursor with an offset, then the server's 400 is a bad request", func() {
			first, firstErr := remote.SearchSamplesCursor(context.Background(), "acme", SampleSearchOptions{}, SearchPageOptions{Limit: 2})
			convey.So(firstErr, convey.ShouldBeNil)

			_, err = remote.SearchSamplesCursor(context.Background(), "acme", SampleSearchOptions{}, SearchPageOptions{Limit: 2, Offset: 1, Cursor: first.NextCursor})
			convey.So(errors.Is(err, ErrBadRequest), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring, "cursor and offset cannot be combined")
		})
	})

	convey.Convey("Given a server too old for cursors, which answers with an offset page", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Total-Count", "9")
			w.Header().Set("X-Next-Offset", "2")
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[]`)
		}))
		defer server.Close()

		remote, err := NewRemoteClient(RemoteConfig{BaseURL: server.URL})
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when a cursor page is requested, then it reports the unsupported cursor instead of the first page", func() {
			_, err = remote.SearchStudiesCursor(context.Background(), "cursor study", SearchPageOptions{Limit: 2, Cursor: "abc"})
			convey.So(errors.Is(err, ErrSearchCursorUnsupported), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring, SearchCursorAPIVersion)

			page, firstErr := remote.SearchStudiesCursor(context.Background(), "cursor study", SearchPageOptions{Limit: 2})
			convey.So(firstErr, convey.ShouldBeNil)
			convey.So(page.NextCursor, convey.ShouldBeEmpty)
		})
	})
}

func TestSampleSearchTokenAfterQuerySeeksTheTokenIndexRange(t *testing.T) {
	convey.Convey("Given the SQLite cache, when the keyset token page is planned, then it seeks the token index range", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		query, bounds := sampleTokenKeysetQuery("acme", sampleSearchAfter{key: sampleSearchKey{token: "acmeb", id: 12}, set: true})
		convey.So(query, convey.ShouldEqual, sampleSearchTokenPageAfterSQL)
		convey.So(bounds, convey.ShouldResemble, []any{"acmeb", "acmf", "acmeb", "acmeb", int64(12)})

		details := explainQueryPlanDetails(t, cache.DB(), query, append(bounds, 10, 0)...)
		convey.So(planUsesIndexRangeSearch(details), convey.ShouldBeTrue)
		convey.So(planHasFullTokenScan(details), convey.ShouldBeFalse)
	})
}

func TestRealMySQLSearchCursorQueriesUseIndexes(t *testing.T) {
	client := openThrowawayMySQLExportClientForTest(t)
	db := client.cache.DB()
	seedSearchCursorScenario(t, db, "mysql")
	seedSearchCursorExplainFiller(t, db)

	convey.Convey("Given a throwaway MySQL cache with filler samples, when the keyset search queries are explained, then each keeps an index range", t, func() {
		tokenQuery, tokenBounds := sampleTokenKeysetQuery("acme", sampleSearchAfter{key: sampleSearchKey{token: "acmeb", id: 12}, set: true})
		plans := explainPlanRows(t, db, tokenQuery, append(tokenBounds, 10, 0)...)
		assertExplainUsesKey(plans, sampleSearchTokenTable, sampleSearchTokenIndex.Name)
		token, _ := findExplainPlanRow(plans, sampleSearchTokenTable)
		convey.So(strings.ToLower(token.scanType), convey.ShouldEqual, "range")
		convey.So(strings.ToLower(token.extra), convey.ShouldNotContainSubstring, "filesort")

		plans = explainPlanRows(t, db, sampleOrganismCommonNamePageAfterSQL, "Homo sapiens", int64(12), 10)
		assertExplainUsesKey(plans, "sample_mirror", "sample_mirror_common_name_idx")
		organism, _ := findExplainPlanRow(plans, "sample_mirror")
		convey.So(strings.ToLower(organism.scanType), convey.ShouldEqual, "range")
		convey.So(strings.ToLower(organism.extra), convey.ShouldNotContainSubstring, "filesort")

		plans = explainPlanRows(t, db, sampleSQSCPPageAfterSQL, int64(12), 10, 0)
		assertExplainUsesKey(plans, "sample_mirror", "PRIMARY")

		prefixArgs := append(likeContainsArgs(escapeLIKEPrefixPattern("acme"), sampleFullPrefixFields), int64(12), 10)
		plans = explainPlanRows(t, db, sampleFullPrefixPageAfterSQL, prefixArgs...)
		prefix, ok := findExplainPlanRow(plans, "sample_mirror")
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(strings.ToLower(prefix.scanType), convey.ShouldNotEqual, "all")
	})
}

// seedSearchCursorExplainFiller adds non-matching samples so the optimizer
// costs index ranges against a table bigger than the scenario's handful.
func seedSearchCursorExplainFiller(t *testing.T, db *sql.DB) {
	t.Helper()

	for i := range 400 {
		id := int64(1000 + i)
		seedSampleMirrorSearchRow(t, db, id, fmt.Sprintf("FILL-%04d", i), fmt.Sprintf("filler%04d", i), fmt.Sprintf("Species %d", i%40), fmt.Sprintf("fd%04d", i))
	}
	rebuildSampleSearchIndexForTestDialect(t, db, "mysql")

	for _, table := range []string{"sample_mirror", sampleSearchTokenTable} {
		if _, err := db.Exec("ANALYZE TABLE " + table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
}
