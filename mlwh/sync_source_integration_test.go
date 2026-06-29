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
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"

	_ "github.com/go-sql-driver/mysql"
)

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
