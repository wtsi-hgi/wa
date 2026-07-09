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
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"

	_ "github.com/go-sql-driver/mysql"
)

// studyUsersExpectedSourceColumns are the seven columns the study_users wholesale
// source SELECT must name, matching studyUsersWholesaleSpec().mirrorColumns. They
// are asserted explicitly (below) so a successful PREPARE of the study_users query
// against the real source proves study_users carries exactly these columns.
var studyUsersExpectedSourceColumns = []string{
	"id_study_users_tmp",
	"id_study_tmp",
	"role",
	"login",
	"email",
	"name",
	"last_updated",
}

type sourceColumnTypeExpectation struct {
	table     string
	column    string
	dataTypes []string
}

var a9SourceColumnTypeExpectations = []sourceColumnTypeExpectation{
	{table: "iseq_flowcell", column: "entity_type", dataTypes: []string{"char", "enum", "varchar"}},
	{table: "iseq_flowcell", column: "pipeline_id_lims", dataTypes: []string{"char", "varchar"}},
	{table: "eseq_product_metrics", column: "is_sequencing_control", dataTypes: []string{"tinyint"}},
	{table: "useq_product_metrics", column: "is_sequencing_control", dataTypes: []string{"tinyint"}},
	{table: "iseq_product_metrics", column: "qc", dataTypes: []string{"tinyint"}},
	{table: "eseq_product_metrics", column: "qc", dataTypes: []string{"tinyint"}},
	{table: "useq_product_metrics", column: "qc", dataTypes: []string{"tinyint"}},
	{table: "pac_bio_product_metrics", column: "qc", dataTypes: []string{"tinyint"}},
	{table: "iseq_run_status", column: "date", dataTypes: []string{"datetime", "timestamp"}},
	{table: "iseq_run_status_dict", column: "description", dataTypes: []string{"char", "varchar"}},
	{table: "oseq_flowcell", column: "last_updated", dataTypes: []string{"datetime", "timestamp"}},
	{table: "study_users", column: "role", dataTypes: []string{"char", "varchar"}},
	{table: "study", column: "programme", dataTypes: []string{"char", "varchar"}},
}

func assertSourceColumnHasExpectedType(t *testing.T, db *sql.DB, expectation sourceColumnTypeExpectation) {
	t.Helper()

	dataType, err := sourceColumnDataType(t, db, expectation.table, expectation.column)
	convey.So(err, convey.ShouldBeNil)
	if err != nil {
		return
	}

	convey.So(expectation.dataTypes, convey.ShouldContain, dataType)
}

func sourceColumnDataType(t *testing.T, db *sql.DB, table, column string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var dataType string
	err := db.QueryRowContext(ctx, `
SELECT LOWER(data_type)
FROM information_schema.columns
WHERE table_schema = DATABASE()
	AND table_name = ?
	AND column_name = ?`,
		table, column,
	).Scan(&dataType)
	if err != nil {
		return "", fmt.Errorf("%s.%s source column type: %w", table, column, err)
	}

	return dataType, nil
}

// TestSyncSourceSchemaMatchesRealMLWH is a runtime-skipped (NOT build-tagged)
// integration test: it validates that the source schema the rest of the suite
// assumes is the schema the REAL upstream MLWH actually has. It connects with the
// same WA_MLWH_DSN / WA_MLWH_PASSWORD the `wa mlwh sync` command uses and PREPAREs
// EVERY sync source query (from AllSyncSourceQueries, the single source of truth)
// against the live server. Preparing forces the server to validate every column,
// table and schema reference without reading rows, so a missing column, missing
// table or wrong schema fails the test generically -- this would have caught the
// real-source bugs (seq_ops_tracking_per_sample being in mlwh_reporting, the wrong
// useq/eseq primary keys and the dropped eseq_run.run_status column).
//
// It SKIPS cleanly (so CI without DB access stays green) when WA_MLWH_DSN is empty,
// or when opening / pinging the source fails.
func TestSyncSourceSchemaMatchesRealMLWH(t *testing.T) {
	db := openRealMLWHSourceOrSkip(t)

	convey.Convey("Given a live connection to the real upstream MLWH source", t, func() {
		queries := AllSyncSourceQueries()
		convey.So(len(queries), convey.ShouldBeGreaterThan, 0)

		// Every supported sync table must be represented among the validated
		// queries, so the coverage stays complete as tables are added.
		validateAllSupportedSyncTablesCovered(queries)

		convey.Convey("when every sync source query is prepared against it, then each one validates", func() {
			for _, query := range queries {
				prepareAndCloseSourceQuery(t, db, query)
			}
		})
	})
}

// TestRealMLWHSourceHasA9SchemaAssumptions is the A9 live-source guard for the
// source columns and rows the realworld3 features depend on. It stays runtime
// skipped without WA_MLWH_DSN, and it only reads/prepares against the source.
func TestRealMLWHSourceHasA9SchemaAssumptions(t *testing.T) {
	db := openRealMLWHSourceOrSkip(t)

	convey.Convey("Given a live connection to the real upstream MLWH source", t, func() {
		convey.Convey("when the A9 source columns are inspected, then each has the expected source type", func() {
			for _, expectation := range a9SourceColumnTypeExpectations {
				assertSourceColumnHasExpectedType(t, db, expectation)
			}
		})

		convey.Convey("when the A9 source data assumptions are probed, then the required rows are present", func() {
			assertSourceRowExists(
				t,
				db,
				"iseq_flowcell deliverable entity_type",
				`SELECT 1 FROM iseq_flowcell WHERE entity_type IN ('library', 'library_indexed') LIMIT 1`,
			)
			assertSourceRowExists(
				t,
				db,
				"eseq_product_metrics deliverable is_sequencing_control",
				`SELECT 1 FROM eseq_product_metrics WHERE is_sequencing_control = 0 LIMIT 1`,
			)
			assertSourceRowExists(
				t,
				db,
				"useq_product_metrics deliverable is_sequencing_control",
				`SELECT 1 FROM useq_product_metrics WHERE is_sequencing_control = 0 LIMIT 1`,
			)
			assertSourceRunStatusDateExists(t, db, "run complete")
			assertSourceRunStatusDateExists(t, db, "run archived")
			assertSourceRowExists(
				t,
				db,
				"oseq_flowcell last_updated",
				`SELECT 1 FROM oseq_flowcell WHERE last_updated IS NOT NULL LIMIT 1`,
			)
			assertSourceRowExists(
				t,
				db,
				"study_users SQSCP role rows",
				`SELECT 1 FROM study_users su INNER JOIN study ON study.id_study_tmp = su.id_study_tmp AND study.id_lims = 'SQSCP' WHERE su.role IS NOT NULL AND su.role <> '' LIMIT 1`,
			)
		})
	})
}

// TestRealMLWHSourcePreparesA3CompositeRecoveryQuery is the live-source guard for
// A3's real-world merged CRAM query shape. The current live source may have no
// composite CRAM rows, so the stable live contract is that the recovery SELECT
// still validates against the source schema; local real-source fixtures assert
// row-level composite attribution.
func TestRealMLWHSourcePreparesA3CompositeRecoveryQuery(t *testing.T) {
	db := openRealMLWHSourceOrSkip(t)

	convey.Convey("Given a live connection to the real upstream MLWH source", t, func() {
		prepareAndCloseSourceQuery(t, db, SyncSourceQuery{
			Name:  "A3 composite recovery probe",
			Query: `SELECT recovery.id_product, recovery.id_sample_tmp, recovery.id_study_lims FROM (` + seqProductIRODSLocationsIlluminaCompositionRecovery + `) recovery WHERE 1 = 0`,
		})
	})
}

// TestStudyUsersSyncSourceQueryCovered asserts -- WITHOUT a live source, so it
// always runs -- that the new study_users wholesale source SELECT is registered in
// AllSyncSourceQueries() and that study_users is in supportedSyncTables and counted
// by the supported-tables coverage check. This makes I2.1 genuinely tested rather
// than only incidentally covered: if study_users were dropped from
// wholesaleMirrorTables()/supportedSyncTables (so the generic
// TestSyncSourceSchemaMatchesRealMLWH stopped PREPAREing and covering it), this
// test fails. It also pins the seven columns the study_users source SELECT names,
// so the live PREPARE in TestSyncSourceSchemaMatchesRealMLWH proves study_users has
// exactly them.
func TestStudyUsersSyncSourceQueryCovered(t *testing.T) {
	convey.Convey("Given the registered sync source queries and supported tables", t, func() {
		convey.Convey("then study_users is a supported sync table", func() {
			convey.So(supportedSyncTables, convey.ShouldContain, "study_users")
		})

		convey.Convey("then AllSyncSourceQueries includes the study_users wholesale source SELECT", func() {
			queries := AllSyncSourceQueries()

			var studyUsersQuery *SyncSourceQuery

			for i := range queries {
				if queries[i].Name == "study_users" {
					studyUsersQuery = &queries[i]

					break
				}
			}

			convey.So(studyUsersQuery, convey.ShouldNotBeNil)
			convey.So(queryReferencesTable(studyUsersQuery.Query, "study_users"), convey.ShouldBeTrue)

			for _, column := range studyUsersExpectedSourceColumns {
				convey.So(studyUsersQuery.Query, convey.ShouldContainSubstring, column)
			}
		})

		convey.Convey("then the supported-tables coverage check counts study_users as covered", func() {
			queries := AllSyncSourceQueries()

			covered := false

			for _, query := range queries {
				if queryReferencesTable(query.Query, sourceTableForSyncTable("study_users")) {
					covered = true

					break
				}
			}

			convey.So(covered, convey.ShouldBeTrue)
		})
	})
}

// TestRealMLWHSourceHasNewColumns is a runtime-skipped (NOT build-tagged)
// integration test (skipping when WA_MLWH_DSN is absent, like
// TestSyncSourceSchemaMatchesRealMLWH) that PREPAREs a probe SELECT naming the new
// source columns the rest of the suite assumes -- study.faculty_sponsor,
// study.data_access_group, iseq_product_metrics.qc, and the ONT run-identity
// columns on oseq_flowcell. A successful PREPARE forces the server to validate
// every named column without reading rows, so it proves those columns exist on
// the real source.
func TestRealMLWHSourceHasNewColumns(t *testing.T) {
	db := openRealMLWHSourceOrSkip(t)

	convey.Convey("Given a live connection to the real upstream MLWH source", t, func() {
		convey.Convey("when a probe SELECT naming the new source columns is prepared, then it validates", func() {
			probe := SyncSourceQuery{
				Name:  "new-source-columns probe",
				Query: `SELECT study.faculty_sponsor, study.data_access_group, iseq_product_metrics.qc, oseq_flowcell.experiment_name, oseq_flowcell.run_id, oseq_flowcell.run_uuid, oseq_flowcell.last_updated FROM study, iseq_product_metrics, oseq_flowcell WHERE 1 = 0`,
			}

			prepareAndCloseSourceQuery(t, db, probe)
		})
	})
}

// openRealMLWHSourceOrSkip opens the upstream MLWH source the same way the sync
// command does, or skips the test when the source is unavailable (no DSN, or
// open/ping fails). The returned handle is closed via t.Cleanup.
func openRealMLWHSourceOrSkip(t *testing.T) *sql.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("WA_MLWH_DSN"))
	if dsn == "" {
		t.Skip("WA_MLWH_DSN not set; skipping real MLWH source schema integration test")
	}

	resolvedDSN, err := ResolveDSN(dsn, os.Getenv("WA_MLWH_PASSWORD"))
	if err != nil {
		t.Skipf("could not resolve WA_MLWH_DSN (%v); skipping real MLWH source schema integration test", err)
	}

	db, err := sql.Open("mysql", resolvedDSN)
	if err != nil {
		t.Skipf("could not open real MLWH source (%v); skipping real MLWH source schema integration test", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("could not ping real MLWH source (%v); skipping real MLWH source schema integration test", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

// prepareAndCloseSourceQuery prepares one sync source query against the live
// server (validating every column / table / schema it references) and closes it.
func prepareAndCloseSourceQuery(t *testing.T, db *sql.DB, query SyncSourceQuery) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stmt, err := db.PrepareContext(ctx, query.Query)
	if err != nil {
		t.Errorf("sync source query %q failed to prepare against the real MLWH source: %v\nquery: %s", query.Name, err, query.Query)

		return
	}

	convey.So(stmt.Close(), convey.ShouldBeNil)
}

// validateAllSupportedSyncTablesCovered asserts that every supported sync table
// (other than the cache-internal sample-search-token rebuild, which reads the
// cache mirror not the source) is represented by at least one validated source
// query, so the coverage is future-proof as new tables are added.
func validateAllSupportedSyncTablesCovered(queries []SyncSourceQuery) {
	covered := make(map[string]bool, len(queries))
	for _, query := range queries {
		for _, table := range supportedSyncTables {
			if queryReferencesTable(query.Query, sourceTableForSyncTable(table)) {
				covered[table] = true
			}
		}
	}

	for _, table := range supportedSyncTables {
		convey.So(covered[table], convey.ShouldBeTrue)
	}
}

// queryReferencesTable reports whether query references table as a whole SQL
// identifier, rather than merely containing it as a substring. The table name
// matches only when it is not immediately preceded or followed by an identifier
// character ([A-Za-z0-9_]); schema qualifiers, whitespace, newlines and SQL
// punctuation ('.', '(', ',', ';' ...) are all non-identifier characters and so
// count as boundaries. This is what stops iseq_run_status from being falsely
// reported as covered by a query that only mentions iseq_run_status_dict, or
// sample by a query that only mentions id_sample_lims. Matching is
// case-insensitive so the check is robust to SQL table refs varying in case.
func queryReferencesTable(query, table string) bool {
	return tableIdentifierRegexp(table).MatchString(query)
}

// tableIdentifierRegexp builds the boundary-aware, case-insensitive regexp used
// by queryReferencesTable. A leading/trailing identifier character is required
// to be absent, so the table name only matches as a whole identifier. Note that
// \b is unusable here because '_' is a word character, so it would not treat the
// boundary between iseq_run_status and _dict as a non-boundary.
func tableIdentifierRegexp(table string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(table) + `($|[^A-Za-z0-9_])`)
}

// sourceTableForSyncTable maps a sync table name to the upstream source table its
// query reads from, so coverage can be asserted via queryReferencesTable. They are
// identical except the tracking table, which lives in the mlwh_reporting schema.
func sourceTableForSyncTable(table string) string {
	if table == syncTableSeqOpsTrackingPerSample {
		return "mlwh_reporting.seq_ops_tracking_per_sample"
	}

	return table
}

func assertSourceRunStatusDateExists(t *testing.T, db *sql.DB, status string) {
	t.Helper()

	assertSourceRowExists(
		t,
		db,
		"iseq_run_status "+status+" date",
		`SELECT 1 FROM iseq_run_status irs INNER JOIN iseq_run_status_dict dict ON dict.id_run_status_dict = irs.id_run_status_dict WHERE LOWER(dict.description) = ? AND irs.date IS NOT NULL LIMIT 1`,
		status,
	)
}

func assertSourceRowExists(t *testing.T, db *sql.DB, name, query string, args ...any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var one int
	err := db.QueryRowContext(ctx, query, args...).Scan(&one)
	if err != nil {
		err = fmt.Errorf("%s: %w", name, err)
	}

	convey.So(err, convey.ShouldBeNil)
}
