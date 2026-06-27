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

// TestSearchSQLEscapeClauseIsDialectSafe guards the LIKE ESCAPE clause baked
// into the substring/prefix-search SQL strings against a regression that breaks
// the MySQL backend. On MySQL under the default sql_mode (NO_BACKSLASH_ESCAPES
// off), the string literal `'\'` is a backslash escaping the closing quote, so
// an `ESCAPE '\'` clause is an unterminated literal and MySQL rejects the whole
// statement with a syntax error (Error 1064). This asserts the SQL contract
// directly: each search SQL string carries valid `ESCAPE '!'` clauses (one per
// study field; one for the sample token prefix and one for its count) and never
// the broken lone-backslash form, so the clauses are valid on both backends.
func TestSearchSQLEscapeClauseIsDialectSafe(t *testing.T) {
	convey.Convey("Given the search SQL strings built for both dialects", t, func() {
		studyFieldCount := len(studySearchFields)

		cases := []struct {
			name           string
			sql            string
			expectedClause int
		}{
			{"studySearchSQL", studySearchSQL, studyFieldCount},
			{"studySearchCountSQL", studySearchCountSQL, studyFieldCount},
			{"sampleSearchTokenPageSQL", sampleSearchTokenPageSQL, 1},
			{"sampleSearchCountSQL", sampleSearchCountSQL, 1},
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

func TestSearchSamplesMySQLPagesTokenIndexThenFetchesByID(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		// The page SQL must scan the (token, id_sample_tmp) index by prefix in
		// index order with LIMIT/OFFSET (a bounded over-fetch of token rows), and
		// must not be a global SELECT DISTINCT ... ORDER BY id_sample_tmp.
		pageSQL := `SELECT token, id_sample_tmp FROM sample_search_token` +
			` WHERE token LIKE \? ESCAPE '!' ORDER BY token, id_sample_tmp LIMIT \? OFFSET \?`
		roMock.ExpectQuery(pageSQL).
			WithArgs("acme%", 100*sampleSearchTokenPageMultiplier+sampleSearchTokenPageMargin, 0).
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
		// de-duplicate it to a single sample id before the by-id fetch.
		roMock.ExpectQuery(`SELECT token, id_sample_tmp FROM sample_search_token`).
			WithArgs("mus%", 100*sampleSearchTokenPageMultiplier+sampleSearchTokenPageMargin, 0).
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
		// id_sample_tmp of the token prefix range, capped with LIMIT so a
		// mega-term stops at the cap. The bound args are the escaped prefix and
		// the cap.
		countSQL := `(?s)SELECT COUNT\(\*\) FROM \(SELECT DISTINCT id_sample_tmp FROM sample_search_token` +
			` WHERE token LIKE \? ESCAPE '!' LIMIT \?\)`
		roMock.ExpectQuery(countSQL).
			WithArgs("acme%", sampleSearchCountCap).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		count, err := client.CountSampleSearch(context.Background(), "acme")

		convey.Convey("when CountSampleSearch runs, then the bounded DISTINCT-count SQL is built with the prefix and the cap", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 2})
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
