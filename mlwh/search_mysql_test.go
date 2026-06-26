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
// into every substring-search SQL string against a regression that breaks the
// MySQL backend. On MySQL under the default sql_mode (NO_BACKSLASH_ESCAPES off),
// the string literal `'\'` is a backslash escaping the closing quote, so an
// `ESCAPE '\'` clause is an unterminated literal and MySQL rejects the whole
// statement with a syntax error (Error 1064). The sqlmock matchers below assert
// only the MATCH/LIKE skeleton and never the ESCAPE fragment, so they cannot
// catch this; this test asserts the SQL contract directly: every search SQL
// string must carry a single, valid `ESCAPE '!'` clause per searchable field
// and must never contain the broken lone-backslash form, so the clause is valid
// on both MySQL and SQLite.
func TestSearchSQLEscapeClauseIsDialectSafe(t *testing.T) {
	convey.Convey("Given the substring-search SQL strings built for both dialects", t, func() {
		studyFieldCount := len(studySearchFields)
		sampleFieldCount := len(sampleSearchFields)

		cases := []struct {
			name           string
			sql            string
			expectedClause int
		}{
			{"studySearchSQL", studySearchSQL, studyFieldCount},
			{"studySearchCountSQL", studySearchCountSQL, studyFieldCount},
			{"sampleSearchSQL (SQLite)", sampleSearchSQL, sampleFieldCount},
			{"sampleSearchCountSQL (SQLite)", sampleSearchCountSQL, sampleFieldCount},
			{"sampleSearchMySQLSQL", sampleSearchMySQLSQL, sampleFieldCount},
			{"sampleSearchMySQLCountSQL", sampleSearchMySQLCountSQL, sampleFieldCount},
		}

		for _, testCase := range cases {
			convey.Convey("the "+testCase.name+" clause is dialect-safe", func() {
				convey.Convey("then it never renders the unterminated MySQL form ESCAPE '\\'", func() {
					convey.So(strings.Contains(testCase.sql, `ESCAPE '\'`), convey.ShouldBeFalse)
				})

				convey.Convey("then it renders a valid ESCAPE '!' clause once per searchable field", func() {
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

func TestSearchSamplesDialectDispatchRoutesByBackend(t *testing.T) {
	convey.Convey("Given the SearchSamples dialect dispatch", t, func() {
		convey.Convey("when SearchSamples runs on a MySQL Client, then the ngram AGAINST path is emitted", func() {
			roDB, roMock, err := sqlmock.New()
			convey.So(err, convey.ShouldBeNil)
			defer func() {
				roMock.ExpectClose()
				convey.So(roDB.Close(), convey.ShouldBeNil)
				convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
			}()

			roMock.ExpectQuery(`AGAINST\(\? IN BOOLEAN MODE\)`).
				WithArgs(`"acme"`, "%acme%", "%acme%", "%acme%", "%acme%", 100, 0).
				WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))

			// A never-synced sqlmock cache: with no rows the never-synced sync
			// state probe runs, so satisfy it (the SQL it ran is the assertion).
			roMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
				WithArgs(syncTableSample).
				WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow("2026-01-02T03:04:05Z", nil, 0))

			client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

			samples, err := client.SearchSamples(context.Background(), "acme", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []Sample{})
		})

		convey.Convey("when SearchSamples runs on a SQLite Client, then the FTS5 sample_search MATCH path is emitted (no AGAINST)", func() {
			roDB, roMock, err := sqlmock.New()
			convey.So(err, convey.ShouldBeNil)
			defer func() {
				roMock.ExpectClose()
				convey.So(roDB.Close(), convey.ShouldBeNil)
				convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
			}()

			roMock.ExpectQuery(`sample_search MATCH \?`).
				WithArgs(`"acme"`, "%acme%", "%acme%", "%acme%", "%acme%", 100, 0).
				WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))

			roMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
				WithArgs(syncTableSample).
				WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow("2026-01-02T03:04:05Z", nil, 0))

			// Dialect() drives the dispatch (sqliteCache -> SQLite path); the
			// sqlmock cacheReader captures the SQL the SQLite path emits.
			client := &Client{cache: &sqliteCache{}, cacheReader: roDB}

			samples, err := client.SearchSamples(context.Background(), "acme", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []Sample{})
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

func TestCountSampleSearchDialectDispatchRoutesByBackend(t *testing.T) {
	convey.Convey("Given the CountSampleSearch dialect dispatch", t, func() {
		convey.Convey("when CountSampleSearch runs on a MySQL Client, then the ngram AGAINST COUNT path is emitted", func() {
			roDB, roMock, err := sqlmock.New()
			convey.So(err, convey.ShouldBeNil)
			defer func() {
				roMock.ExpectClose()
				convey.So(roDB.Close(), convey.ShouldBeNil)
				convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
			}()

			roMock.ExpectQuery(`SELECT COUNT\(\*\).*AGAINST\(\? IN BOOLEAN MODE\)`).
				WithArgs(`"acme"`, "%acme%", "%acme%", "%acme%", "%acme%").
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

			// A zero count triggers the never-synced sync state probe; satisfy it
			// (the count SQL it ran is the assertion).
			roMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
				WithArgs(syncTableSample).
				WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow("2026-01-02T03:04:05Z", nil, 0))

			client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

			count, err := client.CountSampleSearch(context.Background(), "acme")
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 0})
		})

		convey.Convey("when CountSampleSearch runs on a SQLite Client, then the FTS5 sample_search MATCH COUNT path is emitted (no AGAINST)", func() {
			roDB, roMock, err := sqlmock.New()
			convey.So(err, convey.ShouldBeNil)
			defer func() {
				roMock.ExpectClose()
				convey.So(roDB.Close(), convey.ShouldBeNil)
				convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
			}()

			roMock.ExpectQuery(`SELECT COUNT\(\*\).*sample_search MATCH \?`).
				WithArgs(`"acme"`, "%acme%", "%acme%", "%acme%", "%acme%").
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

			roMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
				WithArgs(syncTableSample).
				WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow("2026-01-02T03:04:05Z", nil, 0))

			// Dialect() drives the dispatch (sqliteCache -> SQLite path); the
			// sqlmock cacheReader captures the SQL the SQLite path emits.
			client := &Client{cache: &sqliteCache{}, cacheReader: roDB}

			count, err := client.CountSampleSearch(context.Background(), "acme")
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 0})
		})
	})
}

func TestCountSampleSearchMySQLBuildsNgramBooleanMatchCountWithLIKEPostFilterNoLimit(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		// The captured COUNT SQL must reuse SearchSamples' MySQL WHERE verbatim:
		// the boolean-mode ngram FULLTEXT MATCH(...) AGAINST(...) over the four
		// sample fields, the case-insensitive LIKE post-filter over the same four
		// fields, and id_lims = 'SQSCP'; it must carry no LIMIT/OFFSET so the
		// count equals the unpaginated search length.
		countSQL := `(?s)SELECT COUNT\(\*\).*MATCH\(name, supplier_name, common_name, donor_id\)` +
			` AGAINST\(\? IN BOOLEAN MODE\).*` +
			`id_lims = 'SQSCP'.*` +
			`sample_mirror\.name LIKE \?.*` +
			`sample_mirror\.supplier_name LIKE \?.*` +
			`sample_mirror\.common_name LIKE \?.*` +
			`sample_mirror\.donor_id LIKE \?`

		roMock.ExpectQuery(countSQL).
			WithArgs(`"acme"`, "%acme%", "%acme%", "%acme%", "%acme%").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		count, err := client.CountSampleSearch(context.Background(), "acme")

		convey.Convey("when CountSampleSearch runs, then the ngram boolean MATCH/AGAINST COUNT and LIKE post-filter SQL is built with the term args and no pagination", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 2})

			// The matched SQL must not paginate: a LIMIT/OFFSET would have
			// required two extra bound args beyond the five term args.
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestSearchSamplesMySQLBuildsNgramBooleanMatchWithLIKEPostFilter(t *testing.T) {
	convey.Convey("Given a sqlmock MySQL Client", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		// The captured SQL must narrow via a boolean-mode ngram FULLTEXT
		// MATCH(...) AGAINST(...) over the four sample fields and then confirm
		// via a case-insensitive LIKE post-filter over the same four fields,
		// honour id_lims = 'SQSCP', order by id_sample_tmp, and paginate with
		// LIMIT ? OFFSET ?.
		searchSQL := `(?s)MATCH\(name, supplier_name, common_name, donor_id\)` +
			` AGAINST\(\? IN BOOLEAN MODE\).*` +
			`id_lims = 'SQSCP'.*` +
			`sample_mirror\.name LIKE \?.*` +
			`sample_mirror\.supplier_name LIKE \?.*` +
			`sample_mirror\.common_name LIKE \?.*` +
			`sample_mirror\.donor_id LIKE \?.*` +
			`ORDER BY sample_mirror\.id_sample_tmp.*` +
			`LIMIT \? OFFSET \?`

		roMock.ExpectQuery(searchSQL).
			WithArgs(`"acme"`, "%acme%", "%acme%", "%acme%", "%acme%", 100, 0).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).
				AddRow(sampleResolverRow(1, "uuid-1", "lims-1", "ACME-001", "sanger-1", "ACME-supplier-1", "accession-1", "donor-1")...).
				AddRow(sampleResolverRow(2, "uuid-2", "lims-2", "ACME-002", "sanger-2", "ACME-supplier-2", "accession-2", "donor-2")...))

		// hydrateSampleFanOut issues one fan-out query for the returned rows.
		roMock.ExpectQuery(regexp.QuoteMeta(`FROM library_samples`)).
			WithArgs(int64(1), int64(2)).
			WillReturnRows(sqlmock.NewRows(sampleFanOutColumnsForTest()))

		client := &Client{cache: &mysqlCache{roDB: roDB}, cacheReader: roDB}

		samples, err := client.SearchSamples(context.Background(), "acme", 100, 0)

		convey.Convey("when SearchSamples runs, then the ngram boolean MATCH/AGAINST and LIKE post-filter SQL is built with the term and pagination args", func() {
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
