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
	"slices"
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

// sampleAnchorFieldFetchChunk bounds how many anchor-matched samples' searchable
// fields are read per query during the in-memory word-AND verification (Part C). The
// anchor is the most-selective token, so the candidate set is small; fetching its
// rows' fields in id-ordered chunks lets the verification stop at the requested page
// size without a single huge IN list.
const sampleAnchorFieldFetchChunk = 1000

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

// sampleFullPrefixFields are the four sample_mirror columns whose case-insensitive
// NOCASE/ci indexes back the full-value PREFIX match: a sample matches when ANY of
// these columns has the search term as a literal prefix. Unlike the BINARY-collated
// (token, id_sample_tmp) table, a case-insensitive `col LIKE 'term%'` on these
// NOCASE/ci-indexed columns is an index range seek in both dialects (verified via
// EXPLAIN), so a multi-word term like "homo sapiens" or a partial like "hek_r"
// (matching supplier_name "Hek_R1") is fast without the slow multi-token word-AND.
var sampleFullPrefixFields = []string{"name", "supplier_name", "common_name", "donor_id"}

// sampleFullPrefixWhere is the WHERE body shared by the full-value prefix page and
// count: SQSCP rows where any searchable column has the term as a literal prefix
// (likeContainsClause renders one `col LIKE ? ESCAPE '!'` per field; the bound
// pattern is the escaped term followed by a single '%', see escapeLIKEPrefixPattern).
var sampleFullPrefixWhere = `id_lims = 'SQSCP' AND ` + likeContainsClause(sampleFullPrefixFields)

// sampleFullPrefixPageSQL returns the id_sample_tmps of samples whose name,
// supplier_name, common_name, or donor_id has the term as a literal prefix, ordered
// by id_sample_tmp for stable union pagination and bounded by LIMIT. It is only
// issued for multi-token terms (a single-token term is fully covered by the faster
// word-token range, whose word-prefix superset already contains every full-value
// prefix match), so the moderate-cardinality ORDER BY cost is confined to multi-word
// terms, where the realistic match sets are either sparse (a small index-merge sort)
// or dense (a primary-key-ordered scan that stops at LIMIT).
var sampleFullPrefixPageSQL = `SELECT id_sample_tmp FROM sample_mirror WHERE ` + sampleFullPrefixWhere +
	` ORDER BY id_sample_tmp LIMIT ?`

// sampleFullPrefixCountSQL counts the distinct full-value-prefix samples bounded by
// sampleSearchCountCap: the inner SELECT over the OR'd prefix predicates is capped
// with LIMIT (no ORDER BY, so a dense prefix stops as soon as the cap is reached),
// then the outer COUNT(*) reports that bounded count. id_sample_tmp is the primary
// key, so each row is already a distinct sample and no DISTINCT is needed.
var sampleFullPrefixCountSQL = `SELECT COUNT(*) FROM (SELECT id_sample_tmp FROM sample_mirror WHERE ` + sampleFullPrefixWhere +
	` LIMIT ?) AS bounded_full_prefix`

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

// sampleSearchTokenBound is the half-open byte range [Lower, Upper) of one query
// token: the lowercased token and its prefix successor (bytePrefixSuccessor).
type sampleSearchTokenBound struct {
	Lower string
	Upper string
}

// sampleSearchQueryBounds tokenises term exactly as stored search values are
// tokenised (sampleSearchTokens: maximal runs of ASCII [a-z0-9], lowercased),
// drops any query token subsumed by a more-specific one (dropPrefixSubsumedTokens),
// and returns the distinct per-token half-open byte ranges to AND together. A term
// whose runes are all separators or non-ASCII (e.g. "___", "ÿ") yields no tokens
// and so an empty slice (nothing to query); duplicate tokens collapse to one
// range, and a token that is a prefix of another retained token is dropped (so
// "mus musculus" collapses to the single "musculus" bound and takes the fast
// single-range path). Because every query token is [a-z0-9] (all bytes < 0x80),
// bytePrefixSuccessor always yields a finite, valid-UTF-8 upper bound, so the
// invalid-UTF-8 concern that motivated gating non-ASCII terms out (resolved PR
// #23 Copilot thread) cannot arise: a non-ASCII term simply searches its [a-z0-9]
// tokens instead of being rejected.
func sampleSearchQueryBounds(term string) []sampleSearchTokenBound {
	tokens := dropPrefixSubsumedTokens(sampleSearchTokens(term))
	if len(tokens) == 0 {
		return nil
	}

	bounds := make([]sampleSearchTokenBound, 0, len(tokens))
	for _, token := range tokens {
		// sampleSearchTokens lowercases and yields only [a-z0-9], so an upper bound
		// always exists; sampleTokenPrefixBounds returns it for the single-token
		// path's symmetry.
		lower, upper, _ := sampleTokenPrefixBounds(token)
		bounds = append(bounds, sampleSearchTokenBound{Lower: lower, Upper: upper})
	}

	return bounds
}

// dropPrefixSubsumedTokens removes every query token that is a prefix of another
// retained query token, keeping the longer/more-specific one (tokens equal to
// each other have already been collapsed by sampleSearchTokens, so only proper
// prefixes are dropped here). This is AND-correctness preserving: if query token
// A is a prefix of query token B (A != B), then "a word with prefix A" AND "a
// word with prefix B" is equivalent to just "a word with prefix B", because any
// word matching B's prefix also matches A's prefix (the SAME word satisfies
// both). So removing A does not change the result set. Running this BEFORE the
// single-vs-multi-token branch lets a term like "mus musculus" (tokens
// "mus","musculus", and "mus" is a prefix of "musculus") collapse to the single
// token "musculus", which then takes the fast single-range index seek instead of
// the multi-token GROUP BY over the OR-union (whose cost is O(sum of per-token
// range sizes) and cannot stop at the page limit). Chains ("mus","musc",
// "musculus" -> "musculus") and order ("musculus","mus") are handled; genuinely
// unrelated tokens (e.g. "homo","sapiens" or "hek","r1") are all retained so
// their AND still applies. Order of the retained tokens is preserved.
func dropPrefixSubsumedTokens(tokens []string) []string {
	if len(tokens) <= 1 {
		return tokens
	}

	kept := make([]string, 0, len(tokens))
	for i, token := range tokens {
		subsumed := false
		for j, other := range tokens {
			// token is subsumed if a different, strictly longer token has it as a
			// prefix. The length guard makes the relation antisymmetric, so two
			// distinct tokens never drop each other (already-equal tokens cannot
			// occur after sampleSearchTokens' dedup).
			if i != j && len(other) > len(token) && strings.HasPrefix(other, token) {
				subsumed = true

				break
			}
		}

		if !subsumed {
			kept = append(kept, token)
		}
	}

	return kept
}

// sampleSearchFieldRow holds one sample's id and its four searchable field values,
// read for the in-memory word-AND verification (Part C).
type sampleSearchFieldRow struct {
	id           int64
	name         string
	supplierName string
	commonName   string
	donorID      string
}

// fetchSampleSearchFields loads the searchable field values for the given
// id_sample_tmps (SQSCP-scoped), ordered by id_sample_tmp so the caller can scan and
// stop at a page boundary in id order. It selects only the four searchable columns
// the word-AND verification re-tokenises.
func fetchSampleSearchFields(ctx context.Context, db *sql.DB, ids []int64) ([]sampleSearchFieldRow, error) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for index, id := range ids {
		placeholders[index] = "?"
		args[index] = id
	}

	query := `SELECT id_sample_tmp, name, supplier_name, common_name, donor_id FROM sample_mirror WHERE id_lims = 'SQSCP' AND id_sample_tmp IN (` +
		strings.Join(placeholders, ", ") + ") ORDER BY id_sample_tmp"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample search: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	fields := make([]sampleSearchFieldRow, 0, len(ids))
	for rows.Next() {
		var row sampleSearchFieldRow
		if scanErr := rows.Scan(&row.id, &row.name, &row.supplierName, &row.commonName, &row.donorID); scanErr != nil {
			return nil, fmt.Errorf("%w: scan sample search: %w", ErrUpstreamImpaired, scanErr)
		}

		fields = append(fields, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query sample search: %w", ErrUpstreamImpaired, err)
	}

	return fields, nil
}

// sampleRowMatchesOtherTokens reports whether every token in others is a word-prefix
// of some token of the sample row (its name/supplier_name/common_name/donor_id
// tokenised the same way stored values are). The anchor token already matched by
// construction, so checking the remaining tokens completes the word-AND.
func sampleRowMatchesOtherTokens(row sampleSearchFieldRow, others []string) bool {
	if len(others) == 0 {
		return true
	}

	rowTokens := sampleSearchTokens(row.name, row.supplierName, row.commonName, row.donorID)
	for _, other := range others {
		if !anyTokenHasPrefix(rowTokens, other) {
			return false
		}
	}

	return true
}

// anyTokenHasPrefix reports whether any token in tokens has prefix as a prefix.
func anyTokenHasPrefix(tokens []string, prefix string) bool {
	for _, token := range tokens {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}

	return false
}

// sampleSearchTokensForTerm tokenises term the way stored searchable values are
// tokenised (sampleSearchTokens: maximal [a-z0-9] runs, lowercased) and drops any
// query token subsumed by a more-specific one (dropPrefixSubsumedTokens), so
// "mus musculus" collapses to the single token "musculus". The result drives the
// single-vs-multi-token branch in SearchSamples/CountSampleSearch.
func sampleSearchTokensForTerm(term string) []string {
	return dropPrefixSubsumedTokens(sampleSearchTokens(term))
}

// moreSelectiveAnchorTie breaks an equal-count anchor tie deterministically: prefer
// the longer (more specific) token, then the lexicographically smaller one.
func moreSelectiveAnchorTie(candidate, current string) bool {
	if len(candidate) != len(current) {
		return len(candidate) > len(current)
	}

	return candidate < current
}

// tokensExcept returns tokens with one occurrence of exclude removed (tokens are
// already de-duplicated, so this drops the single anchor entry and keeps every other
// query token, including ones shorter than searchTermMinLength that still constrain
// the word-AND).
func tokensExcept(tokens []string, exclude string) []string {
	others := make([]string, 0, len(tokens))
	dropped := false
	for _, token := range tokens {
		if !dropped && token == exclude {
			dropped = true

			continue
		}

		others = append(others, token)
	}

	return others
}

// mergeBottomIDs returns up to need of the smallest distinct ids drawn from a and b,
// in ascending order. Both inputs are already the bottom-need of their source, so
// their merged, de-duplicated, sorted prefix is the bottom-need of the union.
func mergeBottomIDs(a, b []int64, need int) []int64 {
	if need <= 0 {
		return nil
	}

	seen := make(map[int64]struct{}, len(a)+len(b))
	merged := make([]int64, 0, len(a)+len(b))
	for _, ids := range [][]int64{a, b} {
		for _, id := range ids {
			if _, ok := seen[id]; ok {
				continue
			}

			seen[id] = struct{}{}
			merged = append(merged, id)
		}
	}

	slices.Sort(merged)
	if len(merged) > need {
		merged = merged[:need]
	}

	return merged
}

// escapeLIKEPattern wraps term in '%...%' for a substring (contains) LIKE,
// escaping the LIKE wildcards ('%', '_') and the escape character itself so the
// term is matched literally. The returned pattern is bound as a parameter and
// paired with an explicit `ESCAPE '!'` clause (see searchLIKEEscapeChar). The
// escape character is replaced first so an already-escaped occurrence is not
// reprocessed by the wildcard rules.
func escapeLIKEPattern(term string) string {
	return "%" + escapeLIKELiteral(term) + "%"
}

// escapeLIKEPrefixPattern appends a single trailing '%' to term for a PREFIX LIKE
// (the full-value prefix match, Part A), escaping the LIKE wildcards ('%', '_') and
// the escape character so the term is matched literally. With no leading '%' the
// pattern stays anchored, so on a NOCASE/ci-indexed column the LIKE is an index range
// seek; the term keeps its literal separators, so "hek_r" matches "Hek_R1".
func escapeLIKEPrefixPattern(term string) string {
	return escapeLIKELiteral(term) + "%"
}

// escapeLIKELiteral escapes the LIKE wildcards ('%', '_') and the escape character
// itself in term so it is matched literally under an `ESCAPE '!'` clause (see
// searchLIKEEscapeChar). The escape character is replaced first so an already-escaped
// occurrence is not reprocessed by the wildcard rules.
func escapeLIKELiteral(term string) string {
	replacer := strings.NewReplacer(
		searchLIKEEscapeChar, searchLIKEEscapeChar+searchLIKEEscapeChar,
		"%", searchLIKEEscapeChar+"%",
		"_", searchLIKEEscapeChar+"_",
	)

	return replacer.Replace(term)
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
// [lower, upper) for any matchable term, or the open-ended `token >= ?` form
// bound to [lower) for the degenerate no-upper-bound case (see
// sampleTokenPrefixBounds). Callers append their pagination or cap arguments
// after the returned bounds.
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
// and upper is its prefix successor (bytePrefixSuccessor).
//
// The search entry points only reach this with a query token produced by
// sampleSearchTokens (via sampleSearchQueryBounds), i.e. a non-empty all-`[a-z0-9]`
// string (every byte < 0x80), for which a finite successor always exists, so
// hasUpper is true on every search path. The open-ended fallback (hasUpper false,
// caller uses sampleTokenPrefixLowerClause) is retained only as a defence for the
// degenerate empty/all-0xFF prefix and is unreachable from
// SearchSamples/CountSampleSearch, which tokenise the term first and never form a
// bound for a non-token term (it yields no query tokens at all).
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
//
// From the search entry points prefix is always a query token emitted by
// sampleSearchTokens (an all-`[a-z0-9]` lowercased word; non-token terms yield no
// query tokens and never reach here), so every byte is well below 0xFF: the
// successor increments one ASCII byte and is always valid UTF-8. Incrementing the
// last raw byte of a multi-byte (non-ASCII) rune could otherwise yield invalid
// UTF-8 (e.g. "ÿ" -> C3 BF -> C3 C0), which is why such terms are tokenised away
// before any bound is formed.
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

// queryIDColumn runs query (whose first column is id_sample_tmp) and returns the
// scanned ids in result order.
func queryIDColumn(ctx context.Context, db *sql.DB, query string, args ...any) ([]int64, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample search: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("%w: scan sample search: %w", ErrUpstreamImpaired, scanErr)
		}

		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query sample search: %w", ErrUpstreamImpaired, err)
	}

	return ids, nil
}

// queryCountRow runs a single-row COUNT(...) query against db and returns the scalar
// result. It is the db-scoped sibling of (*Client).queryCount used inside the
// multi-token gather, which already holds the cache reader handle.
func queryCountRow(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("%w: count sample search: %w", ErrUpstreamImpaired, err)
	}

	return count, nil
}

// sampleSearchUnionIDs returns up to need of the smallest distinct id_sample_tmps
// matching a multi-token term, in ascending id order: the de-duplicated union of
// the full-value prefix match (Part A, sampleFullPrefixPage) and the word-token
// AND match (Part C, sampleWordTokenAnchorPage). Both sources are fetched bottom-up
// in id order, so merging their bottom-need slices yields the bottom-need of the
// union (any id among the union's smallest need lies among each source's smallest
// need). Callers slice [offset:offset+limit] for a page, or pass need=cap to count.
func (c *Client) sampleSearchUnionIDs(ctx context.Context, db *sql.DB, term string, tokens []string, need int) ([]int64, error) {
	if need <= 0 {
		return nil, nil
	}

	prefixIDs, err := c.sampleFullPrefixPage(ctx, db, term, need)
	if err != nil {
		return nil, err
	}

	wordIDs, err := c.sampleWordTokenAnchorPage(ctx, db, tokens, need)
	if err != nil {
		return nil, err
	}

	return mergeBottomIDs(prefixIDs, wordIDs, need), nil
}

// sampleFullPrefixPage returns up to need of the smallest id_sample_tmps whose
// name, supplier_name, common_name, or donor_id has term as a literal prefix, in
// ascending id order (sampleFullPrefixPageSQL). The bound pattern is the escaped
// term plus a single '%', so user wildcards are literal and the match is a prefix,
// not a substring; '%' alone (no leading '%') keeps the NOCASE/ci index range seek.
func (c *Client) sampleFullPrefixPage(ctx context.Context, db *sql.DB, term string, need int) ([]int64, error) {
	if need <= 0 {
		return nil, nil
	}

	pattern := escapeLIKEPrefixPattern(term)
	args := append(likeContainsArgs(pattern, sampleFullPrefixFields), need)

	return queryIDColumn(ctx, db, sampleFullPrefixPageSQL, args...)
}

// sampleWordTokenAnchorPage returns up to need of the smallest id_sample_tmps that
// have, for EVERY token, a word-prefix match in some searchable field (the logical
// word-AND), in ascending id order. It drives off an anchor: the most-selective
// searchable token (>= searchTermMinLength). It fetches the anchor's matching
// samples (bounded by sampleSearchCountCap), then keeps those whose OTHER tokens are
// each a word-prefix of one of the sample's field tokens (checked in memory). If no
// token is searchable, or the anchor itself is too broad (its bounded distinct count
// reaches the cap, i.e. every token is broad), it contributes nothing and the
// full-value prefix (Part A) carries the term. This replaces the former GROUP BY
// over the OR-union of every per-token range, which scanned the whole union when one
// token was a very broad prefix.
func (c *Client) sampleWordTokenAnchorPage(ctx context.Context, db *sql.DB, tokens []string, need int) ([]int64, error) {
	if need <= 0 || len(tokens) == 0 {
		return nil, nil
	}

	searchable := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if len(token) >= searchTermMinLength {
			searchable = append(searchable, token)
		}
	}
	if len(searchable) == 0 {
		return nil, nil
	}

	anchor, anchorCount, err := c.pickMostSelectiveAnchor(ctx, db, searchable)
	if err != nil {
		return nil, err
	}
	if anchorCount >= sampleSearchCountCap {
		return nil, nil
	}

	anchorIDs, err := c.sampleSearchTokenPage(ctx, db, anchor, sampleSearchCountCap, 0)
	if err != nil {
		return nil, err
	}

	others := tokensExcept(tokens, anchor)

	return c.sampleAnchorMatchedIDs(ctx, db, anchorIDs, others, need)
}

// pickMostSelectiveAnchor returns the searchable token with the fewest matching
// samples and that bounded distinct count, so the anchor drives off the smallest
// candidate set. Each token's count is bounded by sampleSearchCountCap (the
// single-token count, ~0.2s even for a very broad token), so a returned count equal
// to the cap means "this token is broad"; when the most-selective token is itself at
// the cap, every token is broad and the caller skips the word-token contribution.
// Ties prefer the longer (more specific) token, then the lexicographically smaller,
// for determinism.
func (c *Client) pickMostSelectiveAnchor(ctx context.Context, db *sql.DB, searchable []string) (string, int, error) {
	bestToken := ""
	bestCount := -1
	for _, token := range searchable {
		query, args := sampleTokenPrefixQuery(token, sampleSearchCountSQL, sampleSearchCountOpenSQL)

		count, err := queryCountRow(ctx, db, query, append(args, sampleSearchCountCap)...)
		if err != nil {
			return "", 0, err
		}

		if bestCount == -1 || count < bestCount ||
			(count == bestCount && moreSelectiveAnchorTie(token, bestToken)) {
			bestToken = token
			bestCount = count
		}
	}

	return bestToken, bestCount, nil
}

// sampleAnchorMatchedIDs returns up to need of the smallest anchor-matched ids whose
// OTHER tokens are each a word-prefix of some token of the sample, in ascending id
// order. It sorts the anchor ids (the single-token page yields them in token-index
// order, not id order), then fetches their searchable fields in id-ordered chunks,
// re-tokenises each row, and keeps a sample only if every other token prefixes one of
// its field tokens. It stops as soon as need matches are collected, so a small page
// reads only the smallest matching ids.
func (c *Client) sampleAnchorMatchedIDs(ctx context.Context, db *sql.DB, anchorIDs []int64, others []string, need int) ([]int64, error) {
	if need <= 0 || len(anchorIDs) == 0 {
		return nil, nil
	}

	sorted := append([]int64(nil), anchorIDs...)
	slices.Sort(sorted)

	matched := make([]int64, 0, min(need, len(sorted)))
	for start := 0; start < len(sorted) && len(matched) < need; start += sampleAnchorFieldFetchChunk {
		end := min(start+sampleAnchorFieldFetchChunk, len(sorted))

		rows, err := fetchSampleSearchFields(ctx, db, sorted[start:end])
		if err != nil {
			return nil, err
		}

		for _, row := range rows {
			if sampleRowMatchesOtherTokens(row, others) {
				matched = append(matched, row.id)
				if len(matched) == need {
					break
				}
			}
		}
	}

	return matched, nil
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

// SearchSamples returns samples that match term, ordered by id_sample_tmp for
// stable pagination, with their library/study fan-out populated as the Find* sample
// methods do. A sample matches when EITHER any of name, supplier_name, common_name,
// or donor_id has term as a literal case-insensitive prefix (the full-value prefix,
// so "hek_r" matches supplier_name "Hek_R1" and "homo sapiens" matches common_name
// "Homo sapiens"), OR, for EVERY word of term, some field has a word starting with
// it (the case-insensitive word-prefix AND, so "mus" matches "Mus musculus",
// "Mus muscu" matches it via both word-prefixes, and the separator-insensitive
// "hek r1" matches "Hek_R1"); a substring inside a single word (e.g. "usculus") does
// not. term is tokenised exactly as stored values are (sampleSearchTokens: maximal
// [a-z0-9] runs). A term shorter than searchTermMinLength, or one that tokenises to
// nothing (e.g. "___" or a non-ASCII-only term), returns an empty slice without
// querying. A never-synced cache returns an empty slice joined with
// ErrCacheNeverSynced and ErrNotFound.
//
// A single query token takes the fast single-range seek of the (token,
// id_sample_tmp) prefix index alone (its word-prefix superset already contains every
// full-value prefix match, so the prefix scan is skipped). A multi-word term unions
// the full-value prefix match (an index range on the NOCASE/ci-collated columns)
// with the word-token AND (anchored on the most-selective token), de-duplicated and
// paged in id order. The same path serves both dialects; neither uses FTS, so it
// works on MariaDB and MySQL < 8 too.
func (c *Client) SearchSamples(ctx context.Context, term string, limit, offset int) ([]Sample, error) {
	if len(term) < searchTermMinLength {
		return []Sample{}, nil
	}

	tokens := sampleSearchTokensForTerm(term)
	if len(tokens) == 0 {
		return []Sample{}, nil
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	ids, err := c.sampleSearchIDs(ctx, db, term, tokens, limit, offset)
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

// sampleSearchIDs returns the id_sample_tmps for the page [offset, offset+limit) of
// samples matching term. A single token keeps the fast single-range word-prefix page
// (sampleSearchTokenPage) unchanged, since its word-prefix superset already covers
// every full-value prefix match. A multi-word term pages the de-duplicated union of
// the full-value prefix match and the word-token AND in id order
// (sampleSearchUnionIDs), slicing the requested window.
func (c *Client) sampleSearchIDs(ctx context.Context, db *sql.DB, term string, tokens []string, limit, offset int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}

	if len(tokens) == 1 {
		return c.sampleSearchTokenPage(ctx, db, tokens[0], limit, offset)
	}

	unionIDs, err := c.sampleSearchUnionIDs(ctx, db, term, tokens, offset+limit)
	if err != nil {
		return nil, err
	}
	if offset >= len(unionIDs) {
		return []int64{}, nil
	}

	return unionIDs[offset:min(offset+limit, len(unionIDs))], nil
}

// sampleSearchTokenPage returns the distinct id_sample_tmps for the page
// [offset, offset+limit) of samples matching the single token prefix, in
// (token, id_sample_tmp) index order. A sample can own several prefix-matching
// tokens, so the index-ordered token stream is de-duplicated app-side; it
// over-fetches token rows in bounded pages and loops only when duplicates
// exhaust an over-fetch, keeping the common case to one index-ordered read.
func (c *Client) sampleSearchTokenPage(ctx context.Context, db *sql.DB, token string, limit, offset int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}

	query, bounds := sampleTokenPrefixQuery(token, sampleSearchTokenPageSQL, sampleSearchTokenPageOpenSQL)

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

// CountSampleSearch counts the distinct samples SearchSamples would match for term,
// bounded by sampleSearchCountCap. For normal result sets the count is exact and
// equals len(SearchSamples(term, all)); for a very common match set the scan stops at
// the cap and reports the cap as a floor (e.g. "homo sapiens" matching millions of
// rows counts to the cap quickly instead of scanning every row). A single token uses
// the fast single-range distinct count of the (token, id_sample_tmp) index. A
// multi-word term counts the de-duplicated union of the full-value prefix match and
// the word-token AND, bounded by the cap. A term shorter than searchTermMinLength, or
// one that tokenises to nothing, returns Count{Count: 0} without querying. A
// never-synced cache returns Count{} with an error satisfying both
// ErrCacheNeverSynced and ErrNotFound, mirroring SearchSamples.
func (c *Client) CountSampleSearch(ctx context.Context, term string) (Count, error) {
	if len(term) < searchTermMinLength {
		return Count{Count: 0}, nil
	}

	tokens := sampleSearchTokensForTerm(term)
	if len(tokens) == 0 {
		return Count{Count: 0}, nil
	}

	count, err := c.countSampleSearch(ctx, term, tokens)
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

// countSampleSearch returns the bounded match count for term. A single token uses
// the fast single-range distinct count SQL directly. A multi-word term counts the
// bottom-cap union ids (sampleSearchUnionIDs with need=cap), so the count equals
// len(SearchSamples(term, all)) below the cap and reports the cap as a floor at or
// above it.
func (c *Client) countSampleSearch(ctx context.Context, term string, tokens []string) (int, error) {
	if len(tokens) == 1 {
		query, args := sampleTokenPrefixQuery(tokens[0], sampleSearchCountSQL, sampleSearchCountOpenSQL)

		return c.queryCount(ctx, query, "count sample search", append(args, sampleSearchCountCap)...)
	}

	db := c.readCacheDB()
	if db == nil {
		return 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	unionIDs, err := c.sampleSearchUnionIDs(ctx, db, term, tokens, sampleSearchCountCap)
	if err != nil {
		return 0, err
	}

	return len(unionIDs), nil
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
