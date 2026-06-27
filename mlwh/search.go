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
	"fmt"
	"strings"
)

// searchTermMinLength is the minimum effective length of a search term. Shorter
// terms short-circuit to an empty result without querying.
const searchTermMinLength = 3

// sampleSearchCountCap bounds the exact CountSampleSearch scan. The count is
// exact up to the cap; a term whose distinct matching samples reach the cap
// reports the cap as a floor (e.g. "10000+"), so a mega-term matching ~1.9M
// rows stays fast (~80ms) instead of scanning every matching token row.
const sampleSearchCountCap = 10000

// sampleSearchTokenPageMultiplier bounds the per-fetch over-fetch when paging
// distinct samples out of the (token, id_sample_tmp) index. A single sample can
// own several tokens sharing the query prefix, so a page of N distinct samples
// may span more than N token rows; each index-order fetch reads
// (need * multiplier + margin) token rows, looping only if duplicates exhaust
// the over-fetch, which keeps the common case to one bounded, index-ordered
// read.
const (
	sampleSearchTokenPageMultiplier = 4
	sampleSearchTokenPageMargin     = 64
)

// searchLIKEEscapeChar is the escape character bound via an explicit LIKE
// ESCAPE clause so that user-supplied '%' and '_' are matched literally rather
// than acting as wildcards. It is '!' rather than the conventional backslash
// because the shared `ESCAPE '<char>'` clause must be valid SQL on both
// backends: on MySQL under the default sql_mode (NO_BACKSLASH_ESCAPES off) the
// literal `'\'` is a backslash escaping the closing quote, making `ESCAPE '\'`
// an unterminated string literal (a syntax error), while SQLite rejects the
// doubled `'\\'` form as not a single character. '!' needs no string-literal
// escaping in either dialect, so the one clause works for both.
const searchLIKEEscapeChar = `!`

// studySearchFields are the study_mirror columns OR'd together by the study
// substring search (and its count sibling).
var studySearchFields = []string{"name", "study_title", "programme", "faculty_sponsor"}

// sampleTokenPrefixRangeClause is the half-open index range that selects every
// token starting with the lowercased search term: `token >= ? AND token < ?`,
// bound to [term, prefix-successor(term)). Unlike `token LIKE 'term%' ESCAPE
// '!'`, this is a true index RANGE seek on (token, id_sample_tmp) in BOTH
// dialects (verified via EXPLAIN QUERY PLAN: "SEARCH ... USING COVERING INDEX
// ... (token>? AND token<?)"). On SQLite the default LIKE is case-insensitive
// while the index uses BINARY collation, so the LIKE-prefix index optimisation
// does not apply and `token LIKE 'term%'` scans the whole covering index
// (~700-825ms on a 6M-token cache); the explicit range restores the index
// SEARCH (~60µs) without any schema change.
const sampleTokenPrefixRangeClause = `token >= ? AND token < ?`

// sampleTokenPrefixLowerClause is the open-ended degenerate fallback used only
// when the term has no finite prefix successor (every byte is 0xFF, or the term
// is empty - practically impossible for real `[a-z0-9]` tokens): `token >= ?`
// alone still seeks the index from the lower bound rather than producing a wrong
// finite range.
const sampleTokenPrefixLowerClause = `token >= ?`

// sampleSearchTokenPageSQL pages the (token, id_sample_tmp) prefix index in
// index order: the half-open token range (sampleTokenPrefixRangeClause) streamed
// in (token, id_sample_tmp) order with LIMIT/OFFSET. Because the index covers
// exactly these columns, the range is a covering-index SEARCH and the order
// matches it, so the page is served from the index with no global sort or table
// touch - measured 48-62ms at any cardinality. Ids are de-duplicated app-side (a
// sample may own several prefix-matching tokens).
var sampleSearchTokenPageSQL = `SELECT token, id_sample_tmp FROM ` + sampleSearchTokenTable +
	` WHERE ` + sampleTokenPrefixRangeClause + ` ORDER BY token, id_sample_tmp LIMIT ? OFFSET ?`

// sampleSearchTokenPageOpenSQL is the open-ended-range variant of
// sampleSearchTokenPageSQL for the degenerate no-upper-bound term (see
// sampleTokenPrefixLowerClause).
var sampleSearchTokenPageOpenSQL = `SELECT token, id_sample_tmp FROM ` + sampleSearchTokenTable +
	` WHERE ` + sampleTokenPrefixLowerClause + ` ORDER BY token, id_sample_tmp LIMIT ? OFFSET ?`

// sampleSearchByIDsSQLPrefix selects full sample rows by id_sample_tmp (the
// matching samples a prefix page resolved to), scoped to SQSCP and ordered by
// id_sample_tmp; the id placeholders and ORDER BY are appended per call.
var sampleSearchByIDsSQLPrefix = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE id_lims = 'SQSCP' AND id_sample_tmp IN `

// sampleSearchCountSQL counts the distinct samples matching the token prefix,
// bounded by sampleSearchCountCap: the inner SELECT DISTINCT over the half-open
// token range (sampleTokenPrefixRangeClause, a covering-index SEARCH) is itself
// capped with LIMIT so a mega-term stops scanning once the cap is reached, then
// the outer COUNT(*) reports that bounded distinct count.
var sampleSearchCountSQL = `SELECT COUNT(*) FROM (SELECT DISTINCT id_sample_tmp FROM ` + sampleSearchTokenTable +
	` WHERE ` + sampleTokenPrefixRangeClause + ` LIMIT ?) AS bounded_sample_search`

// sampleSearchCountOpenSQL is the open-ended-range variant of sampleSearchCountSQL
// for the degenerate no-upper-bound term (see sampleTokenPrefixLowerClause).
var sampleSearchCountOpenSQL = `SELECT COUNT(*) FROM (SELECT DISTINCT id_sample_tmp FROM ` + sampleSearchTokenTable +
	` WHERE ` + sampleTokenPrefixLowerClause + ` LIMIT ?) AS bounded_sample_search`

// studySearchWhereClause is the WHERE body shared by SearchStudies and its
// count sibling: SQSCP rows whose searchable fields contain the term.
var studySearchWhereClause = `id_lims = 'SQSCP' AND ` + likeContainsClause(studySearchFields)

// studySearchFromWhere is the FROM/WHERE body shared by SearchStudies and its
// count sibling: study_mirror filtered by studySearchWhereClause. Both the row
// SELECT and the COUNT(*) reuse it so the count equals the search length.
var studySearchFromWhere = ` FROM study_mirror WHERE ` + studySearchWhereClause

// studySearchSQL selects full study rows matching the term, ordered for stable
// pagination (id_study_lims with an id_study_tmp tie-breaker).
var studySearchSQL = `SELECT ` + studyMirrorSelectColumns + studySearchFromWhere +
	` ORDER BY id_study_lims, id_study_tmp LIMIT ? OFFSET ?`

// studySearchCountSQL counts the study rows SearchStudies would return for the
// term: the same id_lims filter and LIKE OR-set, with no LIMIT, so the count
// equals len(SearchStudies(...)) for the term.
var studySearchCountSQL = `SELECT COUNT(*)` + studySearchFromWhere

// likeContainsClause renders an OR'd, parenthesised set of
// `column LIKE ? ESCAPE '!'` predicates, one per field (see searchLIKEEscapeChar
// for why '!' is the escape character). Callers bind the same escaped pattern
// once per field, in field order.
func likeContainsClause(fields []string) string {
	predicates := make([]string, len(fields))
	for index, field := range fields {
		predicates[index] = fmt.Sprintf(`%s LIKE ? ESCAPE '%s'`, field, searchLIKEEscapeChar)
	}

	return "(" + strings.Join(predicates, " OR ") + ")"
}

// escapeLIKEPattern wraps term in '%...%' for a substring (contains) LIKE,
// escaping the LIKE wildcards ('%', '_') and the escape character itself so the
// term is matched literally. The returned pattern is bound as a parameter and
// paired with an explicit `ESCAPE '!'` clause (see searchLIKEEscapeChar). The
// escape character is replaced first so an already-escaped occurrence is not
// reprocessed by the wildcard rules.
func escapeLIKEPattern(term string) string {
	replacer := strings.NewReplacer(
		searchLIKEEscapeChar, searchLIKEEscapeChar+searchLIKEEscapeChar,
		"%", searchLIKEEscapeChar+"%",
		"_", searchLIKEEscapeChar+"_",
	)

	return "%" + replacer.Replace(term) + "%"
}

// likeContainsArgs repeats the escaped pattern once per searchable field, in
// field order, to bind against likeContainsClause.
func likeContainsArgs(pattern string, fields []string) []any {
	args := make([]any, len(fields))
	for index := range fields {
		args[index] = pattern
	}

	return args
}

// sampleTokenPrefixQuery picks the token-prefix range SQL and the leading bound
// arguments for term: the half-open `token >= ? AND token < ?` form bound to
// [lower, upper) for any real token, or the open-ended `token >= ?` form bound to
// [lower) for the degenerate no-upper-bound term (see sampleTokenPrefixBounds).
// Callers append their pagination or cap arguments after the returned bounds.
func sampleTokenPrefixQuery(term, rangeSQL, openSQL string) (string, []any) {
	lower, upper, hasUpper := sampleTokenPrefixBounds(term)
	if !hasUpper {
		return openSQL, []any{lower}
	}

	return rangeSQL, []any{lower, upper}
}

// sampleTokenPrefixBounds returns the half-open byte range [lower, upper) that
// selects exactly the tokens starting with term, for the (token, id_sample_tmp)
// index range seek (sampleTokenPrefixRangeClause). Tokens are stored lowercased
// (sampleSearchTokens lowercases ASCII letters), so lower is the lowercased term
// - the same lowercasing the previous LIKE prefix used - and upper is its prefix
// successor (bytePrefixSuccessor). For real `[a-z0-9]` tokens hasUpper is always
// true; only a term that is empty or all 0xFF after lowercasing has no finite
// successor, in which case hasUpper is false and the caller falls back to the
// open-ended `token >= ?` range (sampleTokenPrefixLowerClause) rather than
// fabricating a wrong upper bound.
//
// Matching a literal byte prefix - not a LIKE pattern - means a term carrying a
// character that cannot appear in a token ('%', '_', '-', a space, etc.) yields
// a range no token falls in, so it matches nothing, exactly as the escaped
// `LIKE 'term%'` predicate did before.
func sampleTokenPrefixBounds(term string) (lower, upper string, hasUpper bool) {
	lower = strings.ToLower(term)
	upper, hasUpper = bytePrefixSuccessor(lower)

	return lower, upper, hasUpper
}

// bytePrefixSuccessor returns the smallest byte string strictly greater than
// every string that has prefix as a prefix: prefix with its last byte that is
// < 0xFF incremented and all bytes after it dropped. Trailing 0xFF bytes have no
// in-place successor, so they are dropped and the increment carries to the last
// byte below 0xFF. If prefix is empty or every byte is 0xFF there is no finite
// successor (ok is false): the prefix range is open above and the caller must use
// a lower-bound-only predicate. ok is true for any prefix with at least one byte
// below 0xFF.
func bytePrefixSuccessor(prefix string) (successor string, ok bool) {
	bytes := []byte(prefix)
	for len(bytes) > 0 {
		last := len(bytes) - 1
		if bytes[last] < 0xFF {
			bytes[last]++

			return string(bytes), true
		}

		bytes = bytes[:last]
	}

	return "", false
}

// SearchStudies returns studies whose name, study_title, programme, or
// faculty_sponsor contains term (case-insensitive substring), ordered by
// id_study_lims for stable pagination. Terms shorter than searchTermMinLength
// return an empty slice without querying. A never-synced cache returns an empty
// slice joined with ErrCacheNeverSynced and ErrNotFound.
func (c *Client) SearchStudies(ctx context.Context, term string, limit, offset int) ([]Study, error) {
	if len(term) < searchTermMinLength {
		return []Study{}, nil
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	args := append(likeContainsArgs(escapeLIKEPattern(term), studySearchFields), limit, offset)

	studies, err := c.queryStudySearch(ctx, db, studySearchSQL, args...)
	if err != nil {
		return nil, err
	}
	if len(studies) > 0 {
		return studies, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return []Study{}, err
	}

	return []Study{}, nil
}

// SearchSamples returns samples having a word in name, supplier_name,
// common_name, or donor_id that starts with term (case-insensitive word-prefix
// match), ordered by id_sample_tmp for stable pagination, with their
// library/study fan-out populated as the Find* sample methods do. So "musculus"
// and "mus" both match "Mus Musculus"; a substring inside a single word (e.g.
// "usculus") does not. Terms shorter than searchTermMinLength return an empty
// slice without querying. A never-synced cache returns an empty slice joined
// with ErrCacheNeverSynced and ErrNotFound.
//
// The matching id_sample_tmps are paged out of the (token, id_sample_tmp) prefix
// index in index order (no global DISTINCT+id sort), then the sample rows are
// fetched by id and hydrated. The same path serves both dialects; the prefix
// index has no FTS dependency, so it works on MariaDB and MySQL < 8 too.
func (c *Client) SearchSamples(ctx context.Context, term string, limit, offset int) ([]Sample, error) {
	if len(term) < searchTermMinLength {
		return []Sample{}, nil
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	ids, err := c.sampleSearchTokenPage(ctx, db, term, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		if err = c.requireAnySyncState(ctx, syncTableSample); err != nil {
			return []Sample{}, err
		}

		return []Sample{}, nil
	}

	samples, err := c.fetchSamplesByID(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
		return nil, err
	}

	return samples, nil
}

// sampleSearchTokenPage returns the distinct id_sample_tmps for the page
// [offset, offset+limit) of samples matching the token prefix, in
// (token, id_sample_tmp) index order. A sample can own several prefix-matching
// tokens, so the index-ordered token stream is de-duplicated app-side; it
// over-fetches token rows in bounded pages and loops only when duplicates
// exhaust an over-fetch, keeping the common case to one index-ordered read.
func (c *Client) sampleSearchTokenPage(ctx context.Context, db *sql.DB, term string, limit, offset int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}

	query, bounds := sampleTokenPrefixQuery(term, sampleSearchTokenPageSQL, sampleSearchTokenPageOpenSQL)

	need := offset + limit
	seen := make(map[int64]struct{}, need)
	ordered := make([]int64, 0, need)
	tokenOffset := 0
	fetch := need*sampleSearchTokenPageMultiplier + sampleSearchTokenPageMargin

	for len(ordered) < need {
		args := append(append([]any(nil), bounds...), fetch, tokenOffset)

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("%w: query sample search: %w", ErrUpstreamImpaired, err)
		}

		scanned := 0
		for rows.Next() {
			var (
				token string
				id    int64
			)
			if scanErr := rows.Scan(&token, &id); scanErr != nil {
				_ = rows.Close()

				return nil, fmt.Errorf("%w: scan sample search: %w", ErrUpstreamImpaired, scanErr)
			}

			scanned++
			if _, ok := seen[id]; ok {
				continue
			}

			seen[id] = struct{}{}
			ordered = append(ordered, id)
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()

			return nil, fmt.Errorf("%w: query sample search: %w", ErrUpstreamImpaired, err)
		}
		if err = rows.Close(); err != nil {
			return nil, fmt.Errorf("%w: query sample search: %w", ErrUpstreamImpaired, err)
		}

		if scanned < fetch {
			break
		}

		tokenOffset += fetch
	}

	if offset >= len(ordered) {
		return []int64{}, nil
	}

	return ordered[offset:min(offset+limit, len(ordered))], nil
}

// fetchSamplesByID loads the full sample rows for the given id_sample_tmps
// (SQSCP-scoped), ordered by id_sample_tmp so a page is returned in stable id
// order. It reuses the Find* sample row shape via querySamples.
func (c *Client) fetchSamplesByID(ctx context.Context, db *sql.DB, ids []int64) ([]Sample, error) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for index, id := range ids {
		placeholders[index] = "?"
		args[index] = id
	}

	query := sampleSearchByIDsSQLPrefix + "(" + strings.Join(placeholders, ", ") + ") ORDER BY id_sample_tmp"

	return querySamples(ctx, db, query, "query sample search", args...)
}

// CountStudySearch counts the studies SearchStudies would return for term: a
// SELECT COUNT(*) over the identical study_mirror WHERE clause
// (studySearchWhereClause: id_lims = 'SQSCP' and the OR'd LIKE post-filter) with
// no LIMIT, so CountStudySearch(term) equals len(SearchStudies(term, all)) for
// any term. Terms shorter than searchTermMinLength return Count{Count: 0}
// without querying. A never-synced cache returns Count{} with an error
// satisfying both ErrCacheNeverSynced and ErrNotFound, mirroring SearchStudies.
func (c *Client) CountStudySearch(ctx context.Context, term string) (Count, error) {
	if len(term) < searchTermMinLength {
		return Count{Count: 0}, nil
	}

	args := likeContainsArgs(escapeLIKEPattern(term), studySearchFields)

	count, err := c.queryCount(ctx, studySearchCountSQL, "count study search", args...)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// CountSampleSearch counts the distinct samples SearchSamples would match for
// term: an exact COUNT(DISTINCT id_sample_tmp) over the token-prefix range,
// bounded by sampleSearchCountCap. For normal result sets the count is exact and
// equals len(SearchSamples(term, all)); for a very common token the scan stops
// at the cap and reports the cap as a floor (e.g. a token matching ~1.9M rows
// counts to the cap in ~80ms instead of scanning every row). Terms shorter than
// searchTermMinLength return Count{Count: 0} without querying. A never-synced
// cache returns Count{} with an error satisfying both ErrCacheNeverSynced and
// ErrNotFound, mirroring SearchSamples.
func (c *Client) CountSampleSearch(ctx context.Context, term string) (Count, error) {
	if len(term) < searchTermMinLength {
		return Count{Count: 0}, nil
	}

	query, bounds := sampleTokenPrefixQuery(term, sampleSearchCountSQL, sampleSearchCountOpenSQL)
	args := append(append([]any(nil), bounds...), sampleSearchCountCap)

	count, err := c.queryCount(ctx, query, "count sample search", args...)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableSample); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

func (c *Client) queryStudySearch(ctx context.Context, db *sql.DB, query string, args ...any) ([]Study, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query study search: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	studies := make([]Study, 0)
	for rows.Next() {
		study, scanErr := scanStudyRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan study search: %w", ErrUpstreamImpaired, scanErr)
		}

		studies = append(studies, study)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query study search: %w", ErrUpstreamImpaired, err)
	}

	return studies, nil
}
