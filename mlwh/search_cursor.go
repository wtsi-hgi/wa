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
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

// SearchNextCursorHeader is the response header carrying the cursor for the
// next page of a study or sample search. It is absent on the last page.
const SearchNextCursorHeader = "X-Next-Cursor"

// SearchWarningHeader is the response header carrying a search page's warning:
// SearchCursorStaleWarning when the cache has synced since the cursor's first
// page.
const SearchWarningHeader = "X-Search-Warning"

// SearchCursorStaleWarning is the warning a cursor page carries when the cache
// has synced since the search's first page.
const SearchCursorStaleWarning = "cache synced since the first page; results may include newer matches"

const (
	searchCursorVersion    = "1"
	searchCursorKindStudy  = "study"
	searchCursorKindSample = "sample"
	searchCursorFieldCount = 6
)

// errSearchCursorWithOffset and errInvalidSearchCursor are the plain reasons a
// search cursor request is rejected; the Client wraps them in ErrBadRequest and
// the server returns them as 400 bad_request messages.
var (
	errSearchCursorWithOffset = errors.New("cursor and offset cannot be combined: continue a search with its cursor alone")
	errInvalidSearchCursor    = errors.New("invalid search cursor")
	errSearchCursorMismatch   = errors.New("search cursor belongs to a different search: reuse it with the same term, type and options")
)

// sampleSearchFilterFeedingTables are the tables, beyond sample, whose syncs
// can change which samples a filtered sample search matches.
var sampleSearchFilterFeedingTables = []string{
	syncTableIseqFlowcell,
	syncTableIseqProductMetrics,
	syncTablePacBioProductMetrics,
	syncTableEseqProductMetrics,
	syncTableUseqProductMetrics,
	syncTableOseqFlowcell,
}

// SearchPageOptions selects one page of a study or sample search: Limit rows,
// starting either Offset rows in or just after Cursor, the NextCursor of an
// earlier page of the same search. Cursor and a non-zero Offset cannot be
// combined.
type SearchPageOptions struct {
	Limit  int
	Offset int
	Cursor string
}

// SearchCursorPage is one page of a study or sample search. NextCursor
// continues the search just after the page's last row (keyset, like
// Elasticsearch's search_after) and is empty when no rows remain. Stale is set
// when the page continued a cursor and the cache has synced since the search's
// first page: rows whose sort key did not change are still neither repeated nor
// skipped, but matches that now sort before the cursor are missed and the page
// may include matches the first page could not have seen.
type SearchCursorPage[T any] struct {
	Items      []T
	NextCursor string
	Stale      bool
}

// Warnings returns the page's warnings: SearchCursorStaleWarning when Stale.
func (page SearchCursorPage[T]) Warnings() []string {
	if !page.Stale {
		return nil
	}

	return []string{SearchCursorStaleWarning}
}

// searchCursor is a decoded search cursor: which search it belongs to, the
// newest feeding-table sync time when the search's first page was read, and
// the sort key of the last row returned. For studies the key is
// (id_study_lims, id_study_tmp); for samples it is id_sample_tmp, plus the
// matched token on the single-token --words path.
type searchCursor struct {
	kind        string
	fingerprint string
	syncedAt    int64
	id          int64
	key         string
	set         bool
}

func encodeSearchCursor(cursor searchCursor) string {
	raw := strings.Join([]string{
		searchCursorVersion,
		cursor.kind,
		cursor.fingerprint,
		strconv.FormatInt(cursor.syncedAt, 10),
		strconv.FormatInt(cursor.id, 10),
		cursor.key,
	}, "\t")

	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeSearchCursor(raw string) (searchCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return searchCursor{}, errInvalidSearchCursor
	}

	parts := strings.SplitN(string(decoded), "\t", searchCursorFieldCount)
	if len(parts) != searchCursorFieldCount || parts[0] != searchCursorVersion {
		return searchCursor{}, errInvalidSearchCursor
	}

	syncedAt, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return searchCursor{}, errInvalidSearchCursor
	}

	id, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return searchCursor{}, errInvalidSearchCursor
	}

	return searchCursor{kind: parts[1], fingerprint: parts[2], syncedAt: syncedAt, id: id, key: parts[5], set: true}, nil
}

// studySearchFingerprint and sampleSearchFingerprint identify the search a
// cursor belongs to, so a cursor reused with a different term, kind or option
// set is rejected instead of silently paging the wrong order.
func studySearchFingerprint(term string) string {
	return searchFingerprint(searchCursorKindStudy, term)
}

func sampleSearchFingerprint(term string, opts SampleSearchOptions, filters sampleFilterFamily) string {
	return searchFingerprint(searchCursorKindSample, term, strconv.FormatBool(opts.Words), filters.Organism,
		filters.LibraryType, filters.QC, strconv.FormatBool(filters.DeliverablesOnly))
}

func searchFingerprint(parts ...string) string {
	hash := fnv.New64a()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}

	return strconv.FormatUint(hash.Sum64(), 16)
}

// parseSearchCursorRequest validates a page request against the search it
// targets and returns its decoded cursor (unset for an offset page). The
// errors are plain reasons fit for a 400 message.
func parseSearchCursorRequest(kind, fingerprint string, page SearchPageOptions) (searchCursor, error) {
	if strings.TrimSpace(page.Cursor) == "" {
		return searchCursor{}, nil
	}
	if page.Offset != 0 {
		return searchCursor{}, errSearchCursorWithOffset
	}

	cursor, err := decodeSearchCursor(page.Cursor)
	if err != nil {
		return searchCursor{}, err
	}
	if cursor.kind != kind || cursor.fingerprint != fingerprint {
		return searchCursor{}, errSearchCursorMismatch
	}
	if kind == searchCursorKindSample && !isSampleSearchCursorToken(cursor.key) {
		return searchCursor{}, errInvalidSearchCursor
	}

	return cursor, nil
}

// isSampleSearchCursorToken reports whether key is a valid sample cursor
// token: empty (id-ordered paths) or a stored search token ([a-z0-9]+).
func isSampleSearchCursorToken(key string) bool {
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}

	return true
}

// validateStudySearchCursor and validateSampleSearchCursor check a search
// page request's cursor before any query runs, returning a plain reason the
// server reports as a 400.
func validateStudySearchCursor(term string, page SearchPageOptions) error {
	_, err := parseSearchCursorRequest(searchCursorKindStudy, studySearchFingerprint(term), page)

	return err
}

func validateSampleSearchCursor(term string, opts SampleSearchOptions, page SearchPageOptions) error {
	if strings.TrimSpace(page.Cursor) == "" {
		return nil
	}

	filters, err := sampleFilterFamilyFromOptions(opts)
	if err != nil {
		return fmt.Errorf("invalid sample search options: %w", err)
	}

	_, err = parseSearchCursorRequest(searchCursorKindSample, sampleSearchFingerprint(term, opts, filters), page)

	return err
}

// searchSyncStamp returns the sync time a new cursor embeds and whether the
// cache has synced since the cursor's first page: the first page stamps the
// newest last_run of the search's feeding tables, and a continuation keeps its
// cursor's stamp and is stale when the current newest last_run differs.
func (c *Client) searchSyncStamp(ctx context.Context, cursor searchCursor, tables []string) (int64, bool, error) {
	newest, err := c.newestFeedingLastRun(ctx, tables)
	if err != nil {
		return 0, false, err
	}

	current := int64(0)
	if !newest.IsZero() {
		current = newest.UnixNano()
	}
	if !cursor.set {
		return current, false, nil
	}

	return cursor.syncedAt, current != cursor.syncedAt, nil
}

// SearchStudiesCursor returns one page of SearchStudies results with a keyset
// cursor for the next page. The page starts page.Offset rows in, or just after
// page.Cursor, which must be a NextCursor from the same term's study search.
// Studies are ordered by (id_study_lims, id_study_tmp), and a cursor page
// selects the studies after the cursor's key in that order, so it never
// re-scans the rows before it and, unlike an offset, does not shift when rows
// before it appear or disappear. A malformed or mismatched cursor, or a cursor
// combined with an offset, returns an error wrapping ErrBadRequest.
func (c *Client) SearchStudiesCursor(ctx context.Context, term string, page SearchPageOptions) (SearchCursorPage[Study], error) {
	fingerprint := studySearchFingerprint(term)

	cursor, err := parseSearchCursorRequest(searchCursorKindStudy, fingerprint, page)
	if err != nil {
		return SearchCursorPage[Study]{}, fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	if len(term) < searchTermMinLength {
		return SearchCursorPage[Study]{Items: []Study{}}, nil
	}

	db := c.readCacheDB()
	if db == nil {
		return SearchCursorPage[Study]{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	syncedAt, stale, err := c.searchSyncStamp(ctx, cursor, []string{syncTableStudy})
	if err != nil {
		return SearchCursorPage[Study]{}, err
	}

	studies, err := c.studySearchRows(ctx, db, term, searchFetchLimit(page.Limit), page.Offset, cursor)
	if err != nil {
		return SearchCursorPage[Study]{}, err
	}

	result := SearchCursorPage[Study]{Items: studies, Stale: stale}
	if page.Limit > 0 && len(studies) > page.Limit {
		result.Items = studies[:page.Limit]
		last := result.Items[page.Limit-1]
		result.NextCursor = encodeSearchCursor(searchCursor{
			kind: searchCursorKindStudy, fingerprint: fingerprint, syncedAt: syncedAt,
			id: last.IDStudyTmp, key: last.IDStudyLims,
		})
	}
	if len(result.Items) == 0 {
		result.Items = []Study{}
		if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
			return result, err
		}
	}

	return result, nil
}

// SearchSamplesCursor returns one page of SearchSamplesWithOptions results
// with a keyset cursor for the next page. The page starts page.Offset rows in,
// or just after page.Cursor, which must be a NextCursor from the same term,
// mode and filters. Samples are ordered by id_sample_tmp, except the
// single-token --words search, which streams its (token, id_sample_tmp) index
// and is keyed by both. A malformed or mismatched cursor, or a cursor combined
// with an offset, returns an error wrapping ErrBadRequest.
func (c *Client) SearchSamplesCursor(ctx context.Context, term string, opts SampleSearchOptions, page SearchPageOptions) (SearchCursorPage[Sample], error) {
	filters, err := sampleFilterFamilyFromOptions(opts)
	if err != nil {
		return SearchCursorPage[Sample]{}, err
	}

	fingerprint := sampleSearchFingerprint(term, opts, filters)

	cursor, err := parseSearchCursorRequest(searchCursorKindSample, fingerprint, page)
	if err != nil {
		return SearchCursorPage[Sample]{}, fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	if sampleSearchShouldShortCircuit(term, filters) {
		return SearchCursorPage[Sample]{Items: []Sample{}}, nil
	}

	db := c.readCacheDB()
	if db == nil {
		return SearchCursorPage[Sample]{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	tables := []string{syncTableSample}
	if !filters.empty() {
		tables = append(tables, sampleSearchFilterFeedingTables...)
	}

	syncedAt, stale, err := c.searchSyncStamp(ctx, cursor, tables)
	if err != nil {
		return SearchCursorPage[Sample]{}, err
	}

	after := sampleSearchAfter{key: sampleSearchKey{token: cursor.key, id: cursor.id}, set: cursor.set}

	keys, err := c.sampleSearchKeys(ctx, db, term, opts, filters, searchFetchLimit(page.Limit), page.Offset, after)
	if err != nil {
		return SearchCursorPage[Sample]{}, err
	}

	nextCursor := ""
	if page.Limit > 0 && len(keys) > page.Limit {
		keys = keys[:page.Limit]
		last := keys[page.Limit-1]
		nextCursor = encodeSearchCursor(searchCursor{
			kind: searchCursorKindSample, fingerprint: fingerprint, syncedAt: syncedAt,
			id: last.id, key: last.token,
		})
	}

	samples, err := c.samplesForSearchKeys(ctx, db, keys)
	if err != nil {
		return SearchCursorPage[Sample]{Items: samples}, err
	}

	return SearchCursorPage[Sample]{Items: samples, NextCursor: nextCursor, Stale: stale}, nil
}

// searchFetchLimit is the row count a cursor page fetches: one more than the
// page so a further row proves a next page exists. A non-positive limit
// fetches nothing and so returns no cursor.
func searchFetchLimit(limit int) int {
	if limit <= 0 {
		return 0
	}

	return limit + 1
}

// studySearchRows returns the matching studies for the page [offset,
// offset+limit), counted from just after cursor when it is set.
func (c *Client) studySearchRows(ctx context.Context, db *sql.DB, term string, limit, offset int, cursor searchCursor) ([]Study, error) {
	args := likeContainsArgs(escapeLIKEPattern(term), studySearchFields)
	if cursor.set {
		return c.queryStudySearch(ctx, db, studySearchAfterSQL, append(args, cursor.key, cursor.key, cursor.id, limit, offset)...)
	}

	return c.queryStudySearch(ctx, db, studySearchSQL, append(args, limit, offset)...)
}
