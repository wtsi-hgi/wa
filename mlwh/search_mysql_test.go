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
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/smartystreets/goconvey/convey"
)

// TestStudySearchSQLEscapeClauseIsDialectSafe guards the LIKE ESCAPE clause baked
// into the study substring-search SQL strings against a regression that breaks
// the MySQL backend. On MySQL under the default sql_mode (NO_BACKSLASH_ESCAPES
// off), the string literal `'\'` is a backslash escaping the closing quote, so
// an `ESCAPE '\'` clause is an unterminated literal and MySQL rejects the whole
// statement with a syntax error (Error 1064). This asserts the SQL contract
// directly: each study search SQL string carries valid `ESCAPE '!'` clauses (one
// per study field) and never the broken lone-backslash form, so the clauses are
// valid on both backends. (Study search is a substring LIKE scan and is
// unchanged by the sample token-range fix.)
func TestStudySearchSQLEscapeClauseIsDialectSafe(t *testing.T) {
	convey.Convey("Given the study search SQL strings built for both dialects", t, func() {
		studyFieldCount := len(studySearchFields)

		cases := []struct {
			name           string
			sql            string
			expectedClause int
		}{
			{"studySearchSQL", studySearchSQL, studyFieldCount},
			{"studySearchCountSQL", studySearchCountSQL, studyFieldCount},
		}

		for _, testCase := range cases {
			convey.Convey("the "+testCase.name+" clause is dialect-safe", func() {
				convey.Convey("then it never renders the unterminated MySQL form ESCAPE '\\'", func() {
					convey.So(strings.Contains(testCase.sql, `ESCAPE '\'`), convey.ShouldBeFalse)
				})

				convey.Convey("then it renders a valid ESCAPE '!' clause the expected number of times", func() {
					convey.So(strings.Contains(testCase.sql, `ESCAPE '!'`), convey.ShouldBeTrue)
					convey.So(strings.Count(testCase.sql, "ESCAPE "), convey.ShouldEqual, testCase.expectedClause)
					convey.So(strings.Count(testCase.sql, `ESCAPE '!'`), convey.ShouldEqual, testCase.expectedClause)
				})
			})
		}
	})
}

// TestSampleSearchSQLUsesDialectSafeTokenRange asserts the sample token-prefix
// SQL queries the (token, id_sample_tmp) index via a half-open byte range
// (`token >= ? AND token < ?`) rather than a `token LIKE 'prefix%'` pattern. The
// range is what makes the query a true index SEARCH on SQLite (whose default
// case-insensitive LIKE cannot use the BINARY-collated index, forcing a full
// covering-index scan), and it is one shared SQL form valid and index-using on
// both backends. Asserting the SQL carries no LIKE/ESCAPE clause and does carry
// the range predicate locks that contract in at the string level (the EXPLAIN
// plan test proves the index is actually used).
func TestSampleSearchSQLUsesDialectSafeTokenRange(t *testing.T) {
	convey.Convey("Given the sample token-prefix search SQL strings", t, func() {
		cases := []struct {
			name string
			sql  string
		}{
			{"sampleSearchTokenPageSQL", sampleSearchTokenPageSQL},
			{"sampleSearchCountSQL", sampleSearchCountSQL},
		}

		for _, testCase := range cases {
			convey.Convey("the "+testCase.name+" predicate is a dialect-safe token range", func() {
				convey.Convey("then it carries the half-open `token >= ? AND token < ?` range", func() {
					convey.So(strings.Contains(testCase.sql, `token >= ? AND token < ?`), convey.ShouldBeTrue)
				})

				convey.Convey("then it uses no LIKE/ESCAPE clause (which would defeat the SQLite index)", func() {
					convey.So(strings.Contains(testCase.sql, "LIKE"), convey.ShouldBeFalse)
					convey.So(strings.Contains(testCase.sql, "ESCAPE"), convey.ShouldBeFalse)
				})
			})
		}
	})
}

// TestSampleFullPrefixSQLIsDialectSafeAnchoredPrefix locks in the full-value prefix
// SQL (Part A): a sample matches when any of name, supplier_name, common_name, or
// donor_id has the term as a literal PREFIX. The match must be anchored (one trailing
// '%', no leading '%') so it stays an index range seek on the NOCASE/ci columns, must
// carry a valid `ESCAPE '!'` clause per field (never the unterminated MySQL `ESCAPE
// '\'`), and must not be the removed multi-token GROUP BY form.
func TestSampleFullPrefixSQLIsDialectSafeAnchoredPrefix(t *testing.T) {
	convey.Convey("Given the full-value prefix search SQL strings", t, func() {
		fieldCount := len(sampleFullPrefixFields)

		cases := []struct {
			name string
			sql  string
		}{
			{"sampleFullPrefixPageSQL", sampleFullPrefixPageSQL},
			{"sampleFullPrefixCountSQL", sampleFullPrefixCountSQL},
		}

		for _, testCase := range cases {
			convey.Convey("the "+testCase.name+" clause is a dialect-safe anchored prefix", func() {
				convey.Convey("then it never renders the unterminated MySQL form ESCAPE '\\'", func() {
					convey.So(strings.Contains(testCase.sql, `ESCAPE '\'`), convey.ShouldBeFalse)
				})

				convey.Convey("then it renders a valid ESCAPE '!' clause once per searchable field", func() {
					convey.So(strings.Count(testCase.sql, `ESCAPE '!'`), convey.ShouldEqual, fieldCount)
					convey.So(strings.Count(testCase.sql, "LIKE ? ESCAPE '!'"), convey.ShouldEqual, fieldCount)
				})

				convey.Convey("then it is SQSCP-scoped and not the removed multi-token GROUP BY form", func() {
					convey.So(strings.Contains(testCase.sql, "id_lims = 'SQSCP'"), convey.ShouldBeTrue)
					convey.So(strings.Contains(testCase.sql, "GROUP BY"), convey.ShouldBeFalse)
					convey.So(strings.Contains(testCase.sql, "HAVING"), convey.ShouldBeFalse)
				})
			})
		}

		convey.Convey("then escapeLIKEPrefixPattern anchors the term with one trailing '%' and escapes user wildcards", func() {
			convey.So(escapeLIKEPrefixPattern("Hek_R1"), convey.ShouldEqual, `Hek!_R1%`)
			convey.So(escapeLIKEPrefixPattern("homo sapiens"), convey.ShouldEqual, `homo sapiens%`)
			convey.So(escapeLIKEPrefixPattern("a%b"), convey.ShouldEqual, `a!%b%`)
			convey.So(strings.HasPrefix(escapeLIKEPrefixPattern("homo"), "%"), convey.ShouldBeFalse)
		})
	})
}

func TestSearchSamplesMySQLShortTermIssuesNoQuery(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		samples, err := client.SearchSamples(context.Background(), "ab", 100, 0)

		convey.Convey("when SearchSamples runs with a length-2 term, then no query is sent and the result is empty", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []Sample{})
		})
	})
}

func TestCountSampleSearchMySQLMultiTokenUnionBoundedAndCount(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client and a two-word term", t, func() {
		roDB, roMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		convey.So(err, convey.ShouldBeNil)
		roMock.MatchExpectationsInOrder(false)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		// "Hek_R1" tokenises to "hek","r1"; only "hek" is searchable (>= 3 chars), so
		// it is the anchor. The multi-token count is len(union of the full-value
		// prefix ids and the word-token AND ids), each gathered bottom-cap.

		// Part A: full-value prefix page, anchored prefix "Hek!_R1%" on all four
		// fields, ordered by id, bounded by the cap. Returns id 1.
		roMock.ExpectQuery(`SELECT id_sample_tmp FROM sample_mirror WHERE id_lims = 'SQSCP' AND .*LIKE \? ESCAPE '!'.*ORDER BY id_sample_tmp LIMIT \?`).
			WithArgs(`Hek!_R1%`, `Hek!_R1%`, `Hek!_R1%`, `Hek!_R1%`, sampleSearchCountCap).
			WillReturnRows(sqlmock.NewRows([]string{"id_sample_tmp"}).AddRow(int64(1)))

		// Part C anchor selection: the bounded distinct count for the anchor token
		// "hek" over its half-open range [hek, hel), capped. Below the cap, so "hek"
		// is the anchor.
		roMock.ExpectQuery(`SELECT COUNT\(\*\) FROM \(SELECT DISTINCT id_sample_tmp FROM sample_search_token WHERE token >= \? AND token < \? LIMIT \?\)`).
			WithArgs("hek", "hel", sampleSearchCountCap).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		// Part C anchor page: the (token, id_sample_tmp) range for "hek", bounded.
		roMock.ExpectQuery(`SELECT token, id_sample_tmp FROM sample_search_token WHERE token >= \? AND token < \? ORDER BY token, id_sample_tmp LIMIT \? OFFSET \?`).
			WithArgs("hek", "hel", sampleSearchCountCap*sampleSearchTokenPageMultiplier+sampleSearchTokenPageMargin, 0).
			WillReturnRows(sqlmock.NewRows([]string{"token", "id_sample_tmp"}).
				AddRow("hek", int64(1)).
				AddRow("hek", int64(2)))

		// Part C in-memory verification: the anchor-matched samples' searchable fields
		// are fetched by id. id 1 has supplier "Hek_R1" (token "r1" matches the other
		// token); id 2 "HEK293" has no "r1*" word, so only id 1 survives the AND.
		roMock.ExpectQuery(`SELECT id_sample_tmp, name, supplier_name, common_name, donor_id FROM sample_mirror WHERE id_lims = 'SQSCP' AND id_sample_tmp IN \(\?, \?\) ORDER BY id_sample_tmp`).
			WithArgs(int64(1), int64(2)).
			WillReturnRows(sqlmock.NewRows([]string{"id_sample_tmp", "name", "supplier_name", "common_name", "donor_id"}).
				AddRow(int64(1), "7607STDY", "Hek_R1", "Homo sapiens", "donor-1").
				AddRow(int64(2), "name-2", "HEK293", "Homo sapiens", "donor-2"))

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		count, err := client.CountSampleSearch(context.Background(), "Hek_R1")

		convey.Convey("when CountSampleSearch runs, then it counts the de-duplicated union (full-prefix id 1 and word-AND id 1) as one", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 1})
		})
	})
}

func TestSearchSamplesMySQLPagesTokenIndexThenFetchesByID(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		// The page SQL must seek the (token, id_sample_tmp) index by a half-open
		// token range in index order with LIMIT/OFFSET (a bounded over-fetch of
		// token rows), and must not be a global SELECT DISTINCT ... ORDER BY
		// id_sample_tmp. The bound args are [lower, upper) for the prefix "acme"
		// (upper increments the last byte: "acme" -> "acmf").
		pageSQL := `SELECT token, id_sample_tmp FROM sample_search_token` +
			` WHERE token >= \? AND token < \? ORDER BY token, id_sample_tmp LIMIT \? OFFSET \?`
		roMock.ExpectQuery(pageSQL).
			WithArgs("acme", "acmf", 100*sampleSearchTokenPageMultiplier+sampleSearchTokenPageMargin, 0).
			WillReturnRows(sqlmock.NewRows([]string{"token", "id_sample_tmp"}).
				AddRow("acme", int64(1)).
				AddRow("acme", int64(2)))

		// The matching samples are then fetched by id, SQSCP-scoped, ordered by
		// id_sample_tmp.
		roMock.ExpectQuery(`SELECT .* FROM sample_mirror WHERE id_lims = 'SQSCP' AND id_sample_tmp IN \(\?, \?\) ORDER BY id_sample_tmp`).
			WithArgs(int64(1), int64(2)).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "uuid-1", "lims-1", "ACME-001", "sanger-1", "ACME-supplier-1", "accession-1", "donor-1")...).
				AddRow(sampleResolverRow(2, "uuid-2", "lims-2", "ACME-002", "sanger-2", "ACME-supplier-2", "accession-2", "donor-2")...))

		// hydrateSampleFanOut issues one fan-out query for the returned rows.
		roMock.ExpectQuery(regexp.QuoteMeta(`FROM library_samples`)).
			WithArgs(int64(1), int64(2)).
			WillReturnRows(sqlmock.NewRows(sampleFanOutColumnsForTest()))

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		samples, err := client.SearchSamples(context.Background(), "acme", 100, 0)

		convey.Convey("when SearchSamples runs, then the token-prefix page SQL and the by-id fetch SQL are built with the prefix and pagination args", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1, 2})
		})
	})
}

func TestSearchSamplesDeDuplicatesIdsSharingThePrefix(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client whose token page repeats an id across prefix-matching tokens", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		// id 1 owns two prefix-matching tokens ("mus", "musculus"); the page must
		// de-duplicate it to a single sample id before the by-id fetch. The prefix
		// "mus" seeks the half-open range [mus, mut) (upper increments the last
		// byte: "mus" -> "mut").
		roMock.ExpectQuery(`SELECT token, id_sample_tmp FROM sample_search_token`).
			WithArgs("mus", "mut", 100*sampleSearchTokenPageMultiplier+sampleSearchTokenPageMargin, 0).
			WillReturnRows(sqlmock.NewRows([]string{"token", "id_sample_tmp"}).
				AddRow("mus", int64(1)).
				AddRow("musculus", int64(1)).
				AddRow("muscle", int64(2)))

		roMock.ExpectQuery(`FROM sample_mirror WHERE id_lims = 'SQSCP' AND id_sample_tmp IN \(\?, \?\) ORDER BY id_sample_tmp`).
			WithArgs(int64(1), int64(2)).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "uuid-1", "lims-1", "mus-1", "sanger-1", "supplier-1", "accession-1", "donor-1")...).
				AddRow(sampleResolverRow(2, "uuid-2", "lims-2", "muscle-2", "sanger-2", "supplier-2", "accession-2", "donor-2")...))

		roMock.ExpectQuery(regexp.QuoteMeta(`FROM library_samples`)).
			WithArgs(int64(1), int64(2)).
			WillReturnRows(sqlmock.NewRows(sampleFanOutColumnsForTest()))

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		samples, err := client.SearchSamples(context.Background(), "mus", 100, 0)

		convey.Convey("when SearchSamples runs, then the duplicated id is fetched once and two distinct samples are returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1, 2})
		})
	})
}

func TestCountSampleSearchShortTermIssuesNoQuery(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		count, err := client.CountSampleSearch(context.Background(), "ab")

		convey.Convey("when CountSampleSearch runs with a length-2 term, then no query is sent and the count is zero", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 0})
		})
	})
}

func TestCountSampleSearchMySQLBuildsBoundedDistinctCount(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		// The count is an exact COUNT over a bounded inner SELECT DISTINCT
		// id_sample_tmp of the half-open token range, capped with LIMIT so a
		// mega-term stops at the cap. The bound args are the range [lower, upper)
		// for the prefix "acme" ("acme" -> "acmf") followed by the cap.
		countSQL := `(?s)SELECT COUNT\(\*\) FROM \(SELECT DISTINCT id_sample_tmp FROM sample_search_token` +
			` WHERE token >= \? AND token < \? LIMIT \?\)`
		roMock.ExpectQuery(countSQL).
			WithArgs("acme", "acmf", sampleSearchCountCap).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		count, err := client.CountSampleSearch(context.Background(), "acme")

		convey.Convey("when CountSampleSearch runs, then the bounded DISTINCT-count SQL is built with the prefix and the cap", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 2})
		})
	})
}

func TestSearchSamplesMySQLMultiTokenUnionsPrefixAndAnchor(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client and a two-word term", t, func() {
		roDB, roMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		convey.So(err, convey.ShouldBeNil)
		roMock.MatchExpectationsInOrder(false)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		// "Hek_R1" -> tokens "hek","r1"; the page is the union of the full-value
		// prefix match and the word-token AND anchored on "hek". The page need is
		// offset+limit = 100.

		// Part A: full-value prefix page, anchored prefix "Hek!_R1%", ordered by id.
		// Matches the literal supplier_name "Hek_R1" sample (id 1).
		roMock.ExpectQuery(`SELECT id_sample_tmp FROM sample_mirror WHERE id_lims = 'SQSCP' AND .*LIKE \? ESCAPE '!'.*ORDER BY id_sample_tmp LIMIT \?`).
			WithArgs(`Hek!_R1%`, `Hek!_R1%`, `Hek!_R1%`, `Hek!_R1%`, 100).
			WillReturnRows(sqlmock.NewRows([]string{"id_sample_tmp"}).AddRow(int64(1)))

		// Part C anchor selection: bounded distinct count of token "hek", below the cap.
		roMock.ExpectQuery(`SELECT COUNT\(\*\) FROM \(SELECT DISTINCT id_sample_tmp FROM sample_search_token WHERE token >= \? AND token < \? LIMIT \?\)`).
			WithArgs("hek", "hel", sampleSearchCountCap).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		// Part C anchor page: the "hek" range, in token-index order, bounded by the cap.
		roMock.ExpectQuery(`SELECT token, id_sample_tmp FROM sample_search_token WHERE token >= \? AND token < \? ORDER BY token, id_sample_tmp LIMIT \? OFFSET \?`).
			WithArgs("hek", "hel", sampleSearchCountCap*sampleSearchTokenPageMultiplier+sampleSearchTokenPageMargin, 0).
			WillReturnRows(sqlmock.NewRows([]string{"token", "id_sample_tmp"}).
				AddRow("hek", int64(1)).
				AddRow("hek", int64(2)))

		// Part C verification: both anchor ids' fields; both supplier names start
		// "Hek_R1", so both have an "r1*" word and survive the AND with "r1".
		roMock.ExpectQuery(`SELECT id_sample_tmp, name, supplier_name, common_name, donor_id FROM sample_mirror WHERE id_lims = 'SQSCP' AND id_sample_tmp IN \(\?, \?\) ORDER BY id_sample_tmp`).
			WithArgs(int64(1), int64(2)).
			WillReturnRows(sqlmock.NewRows([]string{"id_sample_tmp", "name", "supplier_name", "common_name", "donor_id"}).
				AddRow(int64(1), "7607STDY", "Hek_R1", "Homo sapiens", "donor-1").
				AddRow(int64(2), "name-2", "Hek_R1_a", "Homo sapiens", "donor-2"))

		// The union {1} ∪ {1,2} = {1,2} is fetched by id, SQSCP-scoped, id-ordered.
		// The resolver selects qualified sample_mirror.* columns, distinguishing it
		// from the unqualified field-verification fetch above.
		roMock.ExpectQuery(`SELECT sample_mirror\.id_sample_tmp.* FROM sample_mirror WHERE id_lims = 'SQSCP' AND id_sample_tmp IN \(\?, \?\) ORDER BY id_sample_tmp`).
			WithArgs(int64(1), int64(2)).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "uuid-1", "lims-1", "7607STDY", "sanger-1", "Hek_R1", "accession-1", "donor-1")...).
				AddRow(sampleResolverRow(2, "uuid-2", "lims-2", "name-2", "sanger-2", "Hek_R1_a", "accession-2", "donor-2")...))

		roMock.ExpectQuery(regexp.QuoteMeta(`FROM library_samples`)).
			WithArgs(int64(1), int64(2)).
			WillReturnRows(sqlmock.NewRows(sampleFanOutColumnsForTest()))

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		samples, err := client.SearchSamples(context.Background(), "Hek_R1", 100, 0)

		convey.Convey("when SearchSamples runs, then the full-prefix page and the anchored word-AND union resolve the samples in id order", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1, 2})
		})
	})
}

// sampleFanOutColumnsForTest is the column set hydrateSampleFanOut's fan-out
// query scans: the library identity columns followed by the qualified
// study_mirror columns. Returned empty so the matched samples carry no fan-out
// (the construction of the search SQL/args, not the fan-out, is under test).
func sampleFanOutColumnsForTest() []string {
	return []string{
		"id_sample_tmp", "pipeline_id_lims", "library_id", "id_library_lims",
		"study_mirror.id_study_tmp", "study_mirror.id_lims", "study_mirror.id_study_lims",
		"study_mirror.uuid_study_lims", "study_mirror.name", "study_mirror.accession_number",
		"study_mirror.study_title", "study_mirror.faculty_sponsor", "study_mirror.state",
		"study_mirror.data_release_strategy", "study_mirror.data_access_group", "study_mirror.programme",
		"study_mirror.reference_genome", "study_mirror.ethically_approved", "study_mirror.study_type",
		"study_mirror.contains_human_dna", "study_mirror.contaminated_human_dna", "study_mirror.study_visibility",
		"study_mirror.ega_dac_accession_number", "study_mirror.ega_policy_accession_number", "study_mirror.data_release_timing",
	}
}
