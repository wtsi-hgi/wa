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
// index (key), the candidate indexes (possibleKeys) and the access type (a full
// table scan reports type "ALL").
type mysqlExplainRow struct {
	scanType     string
	key          string
	possibleKeys string
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

// seedF4StatusBreakdownScenarioMySQL seeds the same multi-platform + ONT status
// breakdown fixture as seedF4StatusBreakdownScenario, but stamps sync_state with the
// dialect-neutral plain-INSERT seedSyncStateRun instead of the SQLite-only ON
// CONFLICT upsert seedSyncState, so the fixture builds against the real MySQL cache.
// The data-row helpers it reuses are all plain INSERTs already MySQL-compatible.
func seedF4StatusBreakdownScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, f4StudyTmp, f4StudyLims)

	for _, id := range []int64{f4Delivered1, f4Delivered2, f4MultiPlatform, f4SequencedNoData, f4ONT} {
		seedHierarchySample(t, db, id, f4StudyLims, "sample-"+formatInt(id))
		seedLibrarySample(t, db, "Standard", id, f4StudyLims)
	}

	seedIseqProductMetricsMirrorRowWithQC(t, db, 40101, f4Delivered1, 54401, 1, 1, f4StudyLims, sql.NullInt64{Int64: 1, Valid: true})
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "40101", "/seq/54401", "54401_1#1.cram", f4Delivered1, f4StudyLims, f4DeliveredCreated, "illumina")

	seedIseqProductMetricsMirrorRowWithQC(t, db, 40201, f4Delivered2, 54401, 2, 1, f4StudyLims, sql.NullInt64{Int64: 1, Valid: true})
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "40201", "/seq/54401", "54401_2#1.cram", f4Delivered2, f4StudyLims, f4DeliveredCreated, "illumina")

	seedIseqProductMetricsMirrorRowWithQC(t, db, 40301, f4MultiPlatform, 54401, 3, 1, f4StudyLims, sql.NullInt64{Int64: 1, Valid: true})
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "40301", "/seq/54401", "54401_3#1.cram", f4MultiPlatform, f4StudyLims, f4DeliveredCreated, "illumina")
	seedPacBioProductMetricsMirrorRow(t, db, "40302", f4MultiPlatform, f4StudyLims)

	seedIseqProductMetricsMirrorRowWithQC(t, db, 40401, f4SequencedNoData, 54401, 4, 1, f4StudyLims, sql.NullInt64{})

	seedOseqFlowcellMirrorRow(t, db, 40501, f4ONT, f4StudyLims)

	// Filler iRODS rows for an unrelated study, so the mirror is large enough that
	// the optimizer reports a real per-table access path for the linkage (a near-empty
	// table is optimized away and never appears in EXPLAIN). They never match the F4
	// study so they do not change the breakdown.
	for i := range 400 {
		product := formatInt(int64(900000 + i))
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, product, "/seq/99999", "99999_1#"+product+".cram", int64(900000+i), "other-study", f4DeliveredCreated, "illumina")
	}

	base := time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC)
	for i, table := range []string{
		syncTableStudy, syncTableSample, syncTableIseqFlowcell, syncTableIseqProductMetrics,
		syncTablePacBioProductMetrics, syncTableEseqProductMetrics, syncTableUseqProductMetrics,
		syncTableSeqProductIRODSLocations, syncTableSeqOpsTrackingPerSample,
	} {
		seedSyncStateRun(t, db, table, base.Add(time.Duration(i)*time.Minute), base.Add(time.Duration(i)*time.Minute))
	}

	// Refresh InnoDB statistics so the optimizer sees the iRODS mirror as a real
	// table and reports a per-table access path for the linkage (an unanalyzed tiny
	// table can be estimated at ~0 rows and eliminated from the plan).
	if _, err := db.Exec("ANALYZE TABLE seq_product_irods_locations_mirror"); err != nil {
		t.Fatalf("ANALYZE seq_product_irods_locations_mirror: %v", err)
	}
}

// perPlatformBreakdownExplain holds the EXPLAIN analysis of the per-platform
// status-breakdown query: the access-plan rows that touch the iRODS-locations mirror
// (one per product arm, each proving the linkage is index-served) and whether any
// row is a per-row DEPENDENT SUBQUERY (the slow correlated-subquery shape the fix
// removes).
type perPlatformBreakdownExplain struct {
	irodsPlans          []mysqlExplainRow
	hasDependentSubquery bool
}

// explainPerPlatformBreakdown runs EXPLAIN on the per-platform status-breakdown
// query (the exact SQL the read path uses) and extracts every plan row that touches
// the seq_product_irods_locations_mirror plus whether any row is a dependent
// subquery, so the test can prove the per-platform delivered linkage is index-served
// (no full scan of the ~7M-row mirror) and is no longer a per-row correlated
// subquery.
func explainPerPlatformBreakdown(t *testing.T, db *sql.DB, studyLimsID string) perPlatformBreakdownExplain {
	t.Helper()

	args := make([]any, len(statusBreakdownProductPlatformArms)+1)
	for i := range args {
		args[i] = studyLimsID
	}

	rows, err := db.QueryContext(context.Background(), "EXPLAIN "+statusBreakdownPerPlatformSQL(), args...)
	if err != nil {
		t.Fatalf("EXPLAIN per-platform breakdown: %v", err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("EXPLAIN columns: %v", err)
	}

	var result perPlatformBreakdownExplain
	for rows.Next() {
		cells := make([]any, len(cols))
		for i := range cells {
			cells[i] = new(sql.NullString)
		}
		if err = rows.Scan(cells...); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}

		var (
			plan       mysqlExplainRow
			table      string
			selectType string
		)
		for i, name := range cols {
			value := cells[i].(*sql.NullString).String
			switch name {
			case "type":
				plan.scanType = value
			case "key":
				plan.key = value
			case "possible_keys":
				plan.possibleKeys = value
			case "table":
				table = value
			case "select_type":
				selectType = value
			}
		}

		if strings.Contains(strings.ToUpper(selectType), "DEPENDENT SUBQUERY") {
			result.hasDependentSubquery = true
		}
		// EXPLAIN reports the iRODS mirror under its query alias "spi" (the table the
		// per-platform delivered linkage LEFT JOINs to in every product arm).
		if table == "spi" {
			result.irodsPlans = append(result.irodsPlans, plan)
		}
	}

	return result
}

// TestRealMySQLPerPlatformBreakdownIsIndexServed is a runtime-skipped (NOT
// build-tagged) integration test against the REAL MySQL cache server configured in
// .env.development.local. It is the durable guard that the per-platform status
// breakdown (the ~5s study page) stays index-served on MySQL: it builds the cache
// schema in a UNIQUE throwaway database, seeds the multi-platform + ONT status
// breakdown fixture, asserts the query EXECUTES and yields the same per-platform
// ladders the SQLite-backed hermetic tests pin, and asserts EXPLAIN shows the
// seq_product_irods_locations_mirror linkage served by the
// (id_study_lims, id_iseq_product) index with no full-table scan and no per-row
// dependent subquery over that ~7M-row mirror. The throwaway db is dropped in
// t.Cleanup on success AND failure and never touches the configured cache db; the
// test SKIPS cleanly when the cache env vars are absent or the server unreachable.
func TestRealMySQLPerPlatformBreakdownIsIndexServed(t *testing.T) {
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
	seedF4StatusBreakdownScenarioMySQL(t, writeDB)

	convey.Convey("Given the status-breakdown fixture in a throwaway MySQL database", t, func() {
		convey.Convey("the per-platform breakdown executes on MySQL with the pinned ladders", func() {
			breakdown, err := cache.StatusBreakdown(ctx, f4StudyLims)
			convey.So(err, convey.ShouldBeNil)

			ladders := map[string]PlatformPhaseLadder{}
			for _, ladder := range breakdown.PerPlatform {
				ladders[ladder.Platform] = ladder
			}

			convey.So(ladders[platformIllumina].Ladder.WithData, convey.ShouldEqual, 3)
			convey.So(ladders[platformIllumina].Ladder.SequencedNoData, convey.ShouldEqual, 1)
			convey.So(ladders[platformPacBio].Ladder.WithData, convey.ShouldEqual, 0)
			convey.So(ladders[platformPacBio].Ladder.SequencedNoData, convey.ShouldEqual, 1)
			convey.So(ladders[platformONT].Ladder.Registered, convey.ShouldEqual, 1)
		})

		convey.Convey("the per-platform breakdown iRODS linkage is index-served, not a full scan or per-row dependent subquery", func() {
			indexes, _, err := readMySQLTableIndexes(ctx, writeDB, "seq_product_irods_locations_mirror")
			convey.So(err, convey.ShouldBeNil)
			convey.So(slices.Contains(indexes, "id_study_lims,id_iseq_product"), convey.ShouldBeTrue)

			plan := explainPerPlatformBreakdown(t, writeDB, f4StudyLims)

			// The fix replaces the per-row correlated subquery with a set-at-once LEFT
			// JOIN: EXPLAIN must show NO dependent subquery, and every plan row that
			// touches the iRODS mirror must be served by the (id_study_lims,
			// id_iseq_product) index as a covering lookup (never a full "ALL" scan of the
			// ~7M-row mirror). There is one such row per product arm.
			convey.So(plan.hasDependentSubquery, convey.ShouldBeFalse)
			convey.So(len(plan.irodsPlans), convey.ShouldEqual, len(statusBreakdownProductPlatformArms))
			for _, irods := range plan.irodsPlans {
				convey.So(strings.ToLower(irods.scanType), convey.ShouldNotEqual, "all")
				convey.So(irods.key, convey.ShouldEqual, "spi_mirror_study_lims_iseq_product_idx")
				convey.So(irods.possibleKeys, convey.ShouldContainSubstring, "spi_mirror_study_lims_iseq_product_idx")
			}
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
