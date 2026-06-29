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
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"

	"github.com/go-sql-driver/mysql"
)

const (
	cacheMySQLPathEnv     = "WA_MLWH_CACHE_PATH"
	cacheMySQLPasswordEnv = "WA_MLWH_CACHE_PASSWORD"
)

// mysqlExplainRow is the subset of an EXPLAIN row this test asserts on: the chosen
// index (key) and the access type (a full table scan reports type "ALL").
type mysqlExplainRow struct {
	scanType string
	key      string
}

// explainRunsForStudy runs EXPLAIN on the RunsForStudy cache query (the same SQL
// the read path uses) and returns the access plan for the iseq_product_metrics
// mirror, proving the id_study_lims-scoped query is index-served rather than
// full-scanning the mirror.
func explainRunsForStudy(t *testing.T, db *sql.DB, studyLimsID string) mysqlExplainRow {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), "EXPLAIN "+runsForStudyCacheSQL, studyLimsID, 100, 0)
	if err != nil {
		t.Fatalf("EXPLAIN RunsForStudy: %v", err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("EXPLAIN columns: %v", err)
	}

	if !rows.Next() {
		t.Fatal("EXPLAIN RunsForStudy returned no rows")
	}

	cells := make([]any, len(cols))
	for i := range cells {
		cells[i] = new(sql.NullString)
	}
	if err = rows.Scan(cells...); err != nil {
		t.Fatalf("scan EXPLAIN row: %v", err)
	}

	plan := mysqlExplainRow{}
	for i, name := range cols {
		value := cells[i].(*sql.NullString).String
		switch name {
		case "type":
			plan.scanType = value
		case "key":
			plan.key = value
		}
	}

	return plan
}

// TestRealMySQLCacheReadQueriesExecuteAndIndexesApplied is a runtime-skipped (NOT
// build-tagged) integration test against the REAL MySQL cache server configured
// in .env.development.local (WA_MLWH_CACHE_PATH / WA_MLWH_CACHE_PASSWORD). It is
// the durable guard for the MySQL-only class of cache-query bugs that the
// SQLite-backed hermetic tests miss:
//
//   - The study-overview / availability / run-for-study read+aggregate queries
//     must actually EXECUTE on MySQL. The missing-alias bug (a derived table with
//     no alias, MySQL Error 1248) is invisible on SQLite -- which permits an
//     unaliased subquery -- but fatal on MySQL, so StudyOverview (the libraries
//     count) failing here catches that whole class.
//   - A freshly built cache schema must carry every DECLARED index, in particular
//     iseq_product_metrics_mirror's (id_study_lims, id_run, position) index, so
//     the id_study_lims-scoped study queries are index-served (sub-second) rather
//     than full-scanning ~9M rows (the ~52s study overview).
//
// It uses a UNIQUE throwaway database derived from the configured cache db name
// (the last _<segment> replaced with _it<random>), builds the cache schema in it,
// runs the assertions, and DROPs it in t.Cleanup so it is removed on success AND
// on failure; it never touches the real configured cache db. It SKIPS cleanly
// (CI safety) when the cache env vars are absent or the server is unreachable.
func TestRealMySQLCacheReadQueriesExecuteAndIndexesApplied(t *testing.T) {
	baseDSN, password := realMySQLCacheDSNOrSkip(t)

	throwawayDSN := createThrowawayMySQLCacheDBOrSkip(t, baseDSN, password)

	ctx := context.Background()
	cache, err := OpenCacheOnly(ctx, CacheConfig{Path: throwawayDSN, Password: password})
	if err != nil {
		t.Fatalf("OpenCacheOnly() against throwaway MySQL cache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	if cache.cache.Dialect() != "mysql" {
		t.Fatalf("throwaway cache dialect = %q, want mysql", cache.cache.Dialect())
	}

	writeDB := cache.cache.DB()
	seedB1OverviewScenario(t, writeDB)
	cache.now = func() time.Time { return b1NowFixed }

	convey.Convey("Given the cache schema freshly built in a throwaway MySQL database", t, func() {
		convey.Convey("the study read and aggregate queries execute on MySQL (catching the missing-alias class)", func() {
			overview, err := cache.StudyOverview(ctx, "S1")
			convey.So(err, convey.ShouldBeNil)
			convey.So(overview.IDStudyLims, convey.ShouldEqual, "S1")
			convey.So(overview.SamplesTotal, convey.ShouldEqual, 5)
			convey.So(overview.Libraries, convey.ShouldEqual, 2)
			convey.So(overview.Runs, convey.ShouldEqual, 2)

			runs, err := cache.RunsForStudy(ctx, "S1", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(runs), convey.ShouldEqual, 2)

			runCount, err := cache.CountRunsForStudy(ctx, "S1")
			convey.So(err, convey.ShouldBeNil)
			convey.So(runCount.Count, convey.ShouldEqual, 2)

			libCount, err := cache.CountLibrariesForStudy(ctx, "S1")
			convey.So(err, convey.ShouldBeNil)
			convey.So(libCount.Count, convey.ShouldEqual, 2)

			withData, err := cache.SamplesWithData(ctx, "S1", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(withData), convey.ShouldEqual, 3)

			withoutData, err := cache.SamplesWithoutData(ctx, "S1", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(withoutData), convey.ShouldEqual, 2)
		})

		convey.Convey("the iseq_product_metrics_mirror id_study_lims index is applied after a fresh build", func() {
			indexes, _, err := readMySQLTableIndexes(ctx, writeDB, "iseq_product_metrics_mirror")
			convey.So(err, convey.ShouldBeNil)
			convey.So(slices.Contains(indexes, "id_study_lims,id_run,position"), convey.ShouldBeTrue)

			expected, err := expectedCacheSchemaShape("mysql")
			convey.So(err, convey.ShouldBeNil)
			actual, err := readMySQLCacheSchemaShape(ctx, writeDB)
			convey.So(err, convey.ShouldBeNil)
			convey.So(stringSlicesEqual(expected.Index["iseq_product_metrics_mirror"], actual.Index["iseq_product_metrics_mirror"]), convey.ShouldBeTrue)
		})

		convey.Convey("the RunsForStudy query is served by the id_study_lims index, not a full scan", func() {
			plan := explainRunsForStudy(t, writeDB, "S1")
			convey.So(plan.key, convey.ShouldEqual, "iseq_product_metrics_mirror_id_study_lims_id_run_position_idx")
			convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
		})
	})
}

// realMySQLCacheDSNOrSkip reads the real MySQL cache DSN and password from the
// environment (the WA_MLWH_CACHE_PATH / WA_MLWH_CACHE_PASSWORD the cache command
// uses), skipping cleanly when the path is absent or is not a MySQL DSN (so the
// SQLite-cache CI configuration stays green). The returned DSN is normalised (the
// `;?parseTime=true` form unwrapped) and carries no embedded password.
func realMySQLCacheDSNOrSkip(t *testing.T) (string, string) {
	t.Helper()

	path := strings.TrimSpace(os.Getenv(cacheMySQLPathEnv))
	if path == "" {
		t.Skipf("%s not set; skipping real MySQL cache integration test", cacheMySQLPathEnv)
	}

	normalized := normalizeMySQLDSNInput(path)
	if !looksLikeMySQLDSN(normalized) {
		t.Skipf("%s is not a MySQL DSN; skipping real MySQL cache integration test", cacheMySQLPathEnv)
	}

	return normalized, strings.TrimSpace(os.Getenv(cacheMySQLPasswordEnv))
}

// createThrowawayMySQLCacheDBOrSkip derives a unique throwaway database name from
// the configured cache db (its last _<segment> replaced with _it<random>),
// CREATEs it on the same server, and registers a t.Cleanup that DROPs it so it is
// removed on success AND on failure. It also defensively DROPs any pre-existing
// same-named db before creating. It never touches the configured cache db. It
// returns the DSN (in the cache config's normalised form) pointing at the
// throwaway db, and skips cleanly when the server is unreachable.
func createThrowawayMySQLCacheDBOrSkip(t *testing.T, baseDSN, password string) string {
	t.Helper()

	parsed, err := mysql.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse cache DSN: %v", err)
	}

	configuredDB := parsed.DBName
	throwawayDB := throwawayCacheDBName(configuredDB)
	if throwawayDB == configuredDB {
		t.Fatalf("derived throwaway db name %q equals configured db name; refusing to touch the real cache", throwawayDB)
	}

	serverParsed := *parsed
	serverParsed.DBName = ""
	serverParsed.Passwd = password
	serverDSN := serverParsed.FormatDSN()

	server, err := sql.Open("mysql", serverDSN)
	if err != nil {
		t.Skipf("could not open MySQL cache server (%v); skipping real MySQL cache integration test", err)
	}
	defer func() { _ = server.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err = server.PingContext(ctx); err != nil {
		t.Skipf("could not ping MySQL cache server (%v); skipping real MySQL cache integration test", err)
	}

	dropThrowawayMySQLCacheDB(t, serverDSN, throwawayDB)
	if _, err = server.ExecContext(ctx, "CREATE DATABASE `"+throwawayDB+"`"); err != nil {
		t.Fatalf("create throwaway db %q: %v", throwawayDB, err)
	}
	t.Cleanup(func() { dropThrowawayMySQLCacheDB(t, serverDSN, throwawayDB) })

	throwaway := *parsed
	throwaway.DBName = throwawayDB
	throwaway.Passwd = ""

	return throwaway.FormatDSN()
}

// throwawayCacheDBName derives a unique throwaway db name from the configured
// cache db name by replacing its last _<segment> with _it<random>, e.g.
// workflow_automation_mlwh_sb10 -> workflow_automation_mlwh_it<random>. When the
// name has no _<segment>, the suffix is appended instead, so the result is always
// distinct from the configured name.
func throwawayCacheDBName(configured string) string {
	suffix := "it" + randomHexToken()

	idx := strings.LastIndex(configured, "_")
	if idx < 0 {
		return configured + "_" + suffix
	}

	return configured[:idx+1] + suffix
}

// randomHexToken returns a short random hex token unique per test run so
// concurrent runs never collide on the throwaway db name.
func randomHexToken() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}

	return hex.EncodeToString(buf)
}

// dropThrowawayMySQLCacheDB drops the throwaway database, opening a short-lived
// server connection of its own so it works both before creation (defensive) and
// from t.Cleanup after the test (success or failure).
func dropThrowawayMySQLCacheDB(t *testing.T, serverDSN, throwawayDB string) {
	t.Helper()

	server, err := sql.Open("mysql", serverDSN)
	if err != nil {
		t.Logf("drop throwaway db %q: open server: %v", throwawayDB, err)

		return
	}
	defer func() { _ = server.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err = server.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+throwawayDB+"`"); err != nil {
		t.Logf("drop throwaway db %q: %v", throwawayDB, err)
	}
}
