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

// searchTermMinLength is the minimum effective length of a substring search
// term. Shorter terms short-circuit to an empty result without querying,
// matching what the trigram/ngram indexes can serve.
const searchTermMinLength = 3

// searchLIKEEscapeChar is the escape character bound via an explicit LIKE
// ESCAPE clause so that user-supplied '%' and '_' are matched literally rather
// than acting as wildcards.
const searchLIKEEscapeChar = `\`

// studySearchFields are the study_mirror columns OR'd together by the study
// substring search (and its count sibling).
var studySearchFields = []string{"name", "study_title", "programme", "faculty_sponsor"}

// sampleSearchFields are the sample_mirror columns OR'd together by the sample
// substring search LIKE post-filter (and its count sibling), qualified so they
// are unambiguous when sample_mirror is joined to the sample_search FTS5 table
// (whose virtual columns share these names).
var sampleSearchFields = []string{
	"sample_mirror.name",
	"sample_mirror.supplier_name",
	"sample_mirror.common_name",
	"sample_mirror.donor_id",
}

// sampleSearchWhereClause is the WHERE body shared by SearchSamples (SQLite
// path) and its count sibling: an FTS5 MATCH narrows candidates, then SQSCP
// rows whose searchable fields actually contain the term are kept by the LIKE
// post-filter that guarantees exact substring semantics.
var sampleSearchWhereClause = sampleSearchTable + ` MATCH ? AND sample_mirror.id_lims = 'SQSCP' AND ` +
	likeContainsClause(sampleSearchFields)

// sampleSearchFromWhere is the FROM/JOIN/WHERE body shared by the SQLite path
// of SearchSamples and its count sibling: the FTS5 trigram virtual table joined
// back to sample_mirror, filtered by sampleSearchWhereClause. Both the row
// SELECT and the COUNT(*) reuse it so the count equals the search length.
var sampleSearchFromWhere = ` FROM ` + sampleSearchTable +
	` INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = ` + sampleSearchTable + `.rowid` +
	` WHERE ` + sampleSearchWhereClause

// sampleSearchSQL selects full sample rows matching the term via the FTS5
// trigram index joined back to sample_mirror, ordered by id_sample_tmp for
// stable pagination.
var sampleSearchSQL = `SELECT ` + sampleMirrorSelectColumns + sampleSearchFromWhere +
	` ORDER BY sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`

// sampleSearchCountSQL counts the sample rows SearchSamples (SQLite path) would
// return for the term: the same FTS5 MATCH narrowing and LIKE post-filter, with
// no LIMIT, so the count equals len(SearchSamples(...)) for the term.
var sampleSearchCountSQL = `SELECT COUNT(*)` + sampleSearchFromWhere

// sampleSearchMySQLMatchClause is the boolean-mode ngram FULLTEXT predicate
// that narrows candidate sample_mirror rows on the MySQL backend, matching the
// columns covered by the sample_mirror_search_ftx FULLTEXT index. The term is
// bound as a quoted phrase (see fts5MatchPhrase) so arbitrary boolean-mode
// operators (+, -, *, ", (, ), ~, <) are treated literally and cannot error or
// change semantics; the LIKE post-filter still guarantees exact substring.
const sampleSearchMySQLMatchClause = `MATCH(name, supplier_name, common_name, donor_id) AGAINST(? IN BOOLEAN MODE)`

// sampleSearchMySQLWhereClause is the WHERE body shared by the MySQL path of
// SearchSamples and its count sibling: the ngram FULLTEXT MATCH narrows
// candidates, then SQSCP rows whose searchable fields actually contain the term
// are kept by the same LIKE post-filter used by the SQLite path, guaranteeing
// identical exact-substring semantics across dialects.
var sampleSearchMySQLWhereClause = sampleSearchMySQLMatchClause + ` AND sample_mirror.id_lims = 'SQSCP' AND ` +
	likeContainsClause(sampleSearchFields)

// sampleSearchMySQLFromWhere is the FROM/WHERE body shared by the MySQL path of
// SearchSamples and its count sibling: sample_mirror filtered by
// sampleSearchMySQLWhereClause (ngram FULLTEXT MATCH plus the LIKE post-filter).
// Both the row SELECT and the COUNT(*) reuse it so the count equals the search
// length.
var sampleSearchMySQLFromWhere = ` FROM sample_mirror WHERE ` + sampleSearchMySQLWhereClause

// sampleSearchMySQLSQL selects full sample rows matching the term via the ngram
// FULLTEXT index on sample_mirror, ordered by id_sample_tmp for stable
// pagination.
var sampleSearchMySQLSQL = `SELECT ` + sampleMirrorSelectColumns + sampleSearchMySQLFromWhere +
	` ORDER BY sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`

// sampleSearchMySQLCountSQL counts the sample rows SearchSamples (MySQL path)
// would return for the term: the same ngram FULLTEXT MATCH narrowing and LIKE
// post-filter, with no LIMIT, so the count equals len(SearchSamples(...)).
var sampleSearchMySQLCountSQL = `SELECT COUNT(*)` + sampleSearchMySQLFromWhere

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
// `column LIKE ? ESCAPE '\'` predicates, one per field. Callers bind the same
// escaped pattern once per field, in field order.
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
// paired with an explicit `ESCAPE '\'` clause.
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

// fts5MatchPhrase renders term as a single FTS5 string literal (a quoted
// phrase): the term is wrapped in double quotes with any embedded double quotes
// doubled. This makes arbitrary user input a safe MATCH query for the trigram
// tokenizer - FTS5 operators (OR, NEAR, *, :, -, parentheses) and quotes are
// treated as literal characters rather than query syntax, so terms cannot error
// or inject. Exactness is still guaranteed by the LIKE post-filter.
func fts5MatchPhrase(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
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

// SearchSamples returns samples whose name, supplier_name, common_name, or
// donor_id contains term (case-insensitive substring), ordered by id_sample_tmp
// for stable pagination, with their library/study fan-out populated as the
// Find* sample methods do. Terms shorter than searchTermMinLength return an
// empty slice without querying. A never-synced cache returns an empty slice
// joined with ErrCacheNeverSynced and ErrNotFound.
//
// The query is dialect-branched via the cache backend: SQLite uses an FTS5
// trigram MATCH to narrow candidates, MySQL an ngram FULLTEXT match; both apply
// the same LIKE post-filter for exact substring semantics.
func (c *Client) SearchSamples(ctx context.Context, term string, limit, offset int) ([]Sample, error) {
	if len(term) < searchTermMinLength {
		return []Sample{}, nil
	}

	switch c.cache.Dialect() {
	case "mysql":
		return c.searchSamplesMySQL(ctx, term, limit, offset)
	default:
		return c.searchSamplesSQLite(ctx, term, limit, offset)
	}
}

// searchSamplesSQLite is the SQLite FTS5 path of SearchSamples: an FTS5 trigram
// MATCH narrows candidate id_sample_tmps, joined back to sample_mirror, with a
// LIKE post-filter guaranteeing exact substring semantics.
func (c *Client) searchSamplesSQLite(ctx context.Context, term string, limit, offset int) ([]Sample, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	args := append([]any{fts5MatchPhrase(term)}, likeContainsArgs(escapeLIKEPattern(term), sampleSearchFields)...)
	args = append(args, limit, offset)

	samples, err := querySamples(ctx, db, sampleSearchSQL, "query sample search", args...)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
			return nil, err
		}

		return samples, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableSample); err != nil {
		return []Sample{}, err
	}

	return []Sample{}, nil
}

// searchSamplesMySQL is the MySQL ngram path of SearchSamples: a boolean-mode
// FULLTEXT MATCH ... AGAINST over the ngram index narrows candidate
// sample_mirror rows (id_lims = 'SQSCP'), with the same LIKE post-filter as the
// SQLite path guaranteeing exact substring semantics, ordered by id_sample_tmp.
func (c *Client) searchSamplesMySQL(ctx context.Context, term string, limit, offset int) ([]Sample, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	args := append([]any{fts5MatchPhrase(term)}, likeContainsArgs(escapeLIKEPattern(term), sampleSearchFields)...)
	args = append(args, limit, offset)

	samples, err := querySamples(ctx, db, sampleSearchMySQLSQL, "query sample search", args...)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
			return nil, err
		}

		return samples, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableSample); err != nil {
		return []Sample{}, err
	}

	return []Sample{}, nil
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

// CountSampleSearch counts the samples SearchSamples would return for term: a
// SELECT COUNT(*) over the identical sample WHERE clause (the FTS5 trigram MATCH
// on SQLite or the ngram FULLTEXT MATCH on MySQL, plus the shared LIKE
// post-filter and id_lims = 'SQSCP') with no LIMIT, so CountSampleSearch(term)
// equals len(SearchSamples(term, all)) for any term. The query is
// dialect-branched via the cache backend exactly as SearchSamples is. Terms
// shorter than searchTermMinLength return Count{Count: 0} without querying. A
// never-synced cache returns Count{} with an error satisfying both
// ErrCacheNeverSynced and ErrNotFound, mirroring SearchSamples.
func (c *Client) CountSampleSearch(ctx context.Context, term string) (Count, error) {
	if len(term) < searchTermMinLength {
		return Count{Count: 0}, nil
	}

	switch c.cache.Dialect() {
	case "mysql":
		return c.countSampleSearch(ctx, term, sampleSearchMySQLCountSQL)
	default:
		return c.countSampleSearch(ctx, term, sampleSearchCountSQL)
	}
}

// countSampleSearch runs the given dialect-specific sample-search COUNT(*)
// query, binding the FTS5/ngram MATCH phrase followed by the LIKE post-filter
// args (the same arg shape as SearchSamples minus the pagination args), and
// resolves a zero count against the sample sync state so the count and the
// search agree on a never-synced cache.
func (c *Client) countSampleSearch(ctx context.Context, term, query string) (Count, error) {
	args := append([]any{fts5MatchPhrase(term)}, likeContainsArgs(escapeLIKEPattern(term), sampleSearchFields)...)

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

// SupportsFullTextSearch reports whether the cache backend can serve the
// index-backed sample substring search (the SQLite FTS5 trigram table or the
// MySQL ngram FULLTEXT index). SQLite always qualifies. A MySQL backend
// qualifies only when it is genuine MySQL >= 8: it does not qualify when the
// reported VERSION() string contains "mariadb" (case-insensitive) or when its
// major version is below 8, because neither can provide the ngram full-text
// search the sample search relies on. The version check reuses the same
// VERSION() logic (mySQLServerVersion/mySQLMajorVersion) the schema collation
// selection uses.
func (c *Client) SupportsFullTextSearch(ctx context.Context) (bool, error) {
	if c == nil || c.cache == nil {
		return false, fmt.Errorf("mlwh: cache client not configured")
	}

	if c.cache.Dialect() != "mysql" {
		return true, nil
	}

	db := c.readCacheDB()
	if db == nil {
		return false, fmt.Errorf("mlwh: cache reader not configured")
	}

	version, err := mySQLServerVersion(ctx, db)
	if err != nil {
		return false, fmt.Errorf("mlwh: query mysql version for full-text search support: %w", err)
	}

	if strings.Contains(strings.ToLower(version), "mariadb") {
		return false, nil
	}

	major, err := mySQLMajorVersion(version)
	if err != nil {
		return false, fmt.Errorf("mlwh: parse mysql version %q: %w", version, err)
	}

	return major >= 8, nil
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
