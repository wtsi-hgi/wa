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

// j1MySQL groups the LIMS study ids of the J1 scenario seeded into the throwaway
// MySQL cache. Each new query path reads a self-contained study so the assertions
// never interfere: the manifest / file-type study scope, the run scope, the
// id_run=0 (non-Illumina) scope kept OFF the QC study, the QC-count study, and the
// people studies.
const (
	j1ManifestStudyLims = "S1"  // manifest + file-type-filtered study iRODS scope
	j1RunStudyLims      = "Sr"  // run-scoped iRODS scope (run 52553)
	j1IDRun0StudyLims   = "S0"  // a non-Illumina iRODS row -> id_run=0 (kept off the QC study)
	j1QCStudyLims       = "S1q" // D1q QC-count study (strict-equality fixture)
)

const (
	j1Run                = 52553 // the run whose iRODS objects IRODSPathsForRun lists
	j1DecoyRun           = 52554 // a different run; the run-scope query must exclude it
	j1FacultyMatches     = 3     // distinct "carl" faculty-sponsor studies (E1)
	j1UserDefault        = 3     // ua3 owner/owner/manager studies under the default roles (E2)
	j1UserDualRole       = 2     // dz9 owner + data_access_contact of one study (E2)
	j1ResolveBothSources = 2     // ResolvePerson("rosa") faculty_sponsor + study_users candidates (E3)
	j1ResolveLoginOnly   = 1     // ResolvePerson("rk9") study_users candidate via login fragment (E3)
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
	irodsPlans           []mysqlExplainRow
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

// mysqlExplainPlanRow is the subset of an EXPLAIN row the new-query-path tests
// assert on per table: the query alias (table), the select_type (to spot a
// per-row DEPENDENT SUBQUERY), the access type (a full scan reports "ALL") and the
// chosen / candidate indexes. It carries the table alias too (unlike the
// study-overview-only mysqlExplainRow) so a multi-table join plan can be checked
// table by table.
type mysqlExplainPlanRow struct {
	table        string
	selectType   string
	scanType     string
	key          string
	possibleKeys string
}

// findExplainPlanRow returns the EXPLAIN plan row for the given query alias (the
// table name EXPLAIN reports), so a test can assert the access path for a specific
// joined mirror. It returns ok=false when no plan row touches that alias (e.g. a
// near-empty table the optimizer eliminated, which the filler rows + ANALYZE in
// the seed prevent for the mirrors under test).
func findExplainPlanRow(plans []mysqlExplainPlanRow, alias string) (mysqlExplainPlanRow, bool) {
	for _, plan := range plans {
		if plan.table == alias {
			return plan, true
		}
	}

	return mysqlExplainPlanRow{}, false
}

// assertJ1StudiesForUserIndexServed asserts EXPLAIN of the /studies/user query
// (the default-role list query the read path uses) is served by a
// study_users_mirror lookup index (login/email/name/id_study_tmp/role), not a full
// scan of the study_users_mirror table.
func assertJ1StudiesForUserIndexServed(t *testing.T, db *sql.DB) {
	t.Helper()

	roles := studyUsersDefaultRoles
	query := studyUsersPageSQL(roles)
	args := append(studyUsersArgs("ca3", roles), availabilityFetchAll, 0)
	plans := explainPlanRows(t, db, query, args...)

	plan, ok := findExplainPlanRow(plans, "study_users_mirror")
	convey.So(ok, convey.ShouldBeTrue)
	convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
	convey.So(plan.key, convey.ShouldNotBeBlank)
	convey.So(plan.key, convey.ShouldBeIn,
		"study_users_mirror_login_idx",
		"study_users_mirror_email_idx",
		"study_users_mirror_name_idx",
		"study_users_mirror_id_study_tmp_idx",
		"study_users_mirror_role_idx",
	)
}

// explainPlanRows runs EXPLAIN on an arbitrary cache read query (the same SQL +
// args the read path uses) and returns every plan row, so a test can locate the
// access path for each joined mirror by its query alias and prove it is
// index-served rather than a full scan of a 9M-row mirror. It generalises
// explainRunsForStudy / explainPerPlatformBreakdown for the new D1/D2/E query
// paths, which join several aliased mirrors in one statement.
func explainPlanRows(t *testing.T, db *sql.DB, query string, args ...any) []mysqlExplainPlanRow {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), "EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("EXPLAIN columns: %v", err)
	}

	var plans []mysqlExplainPlanRow
	for rows.Next() {
		cells := make([]any, len(cols))
		for i := range cells {
			cells[i] = new(sql.NullString)
		}
		if err = rows.Scan(cells...); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}

		var plan mysqlExplainPlanRow
		for i, name := range cols {
			value := cells[i].(*sql.NullString).String
			switch name {
			case "table":
				plan.table = value
			case "select_type":
				plan.selectType = value
			case "type":
				plan.scanType = value
			case "key":
				plan.key = value
			case "possible_keys":
				plan.possibleKeys = value
			}
		}

		plans = append(plans, plan)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("EXPLAIN rows: %v", err)
	}

	return plans
}

// assertMirrorIndexServed asserts the EXPLAIN plan row for the given query alias
// exists, is served by a non-empty real index (key) and is not a full table scan
// (type != ALL) -- i.e. the id-scoped join into a multi-million-row mirror is
// index-served, never a full scan. It is the shared assertion behind I1 acceptance
// tests 2 and 3.
func assertMirrorIndexServed(plans []mysqlExplainPlanRow, alias string) {
	plan, ok := findExplainPlanRow(plans, alias)
	convey.So(ok, convey.ShouldBeTrue)
	convey.So(plan.key, convey.ShouldNotBeBlank)
	convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
}

// TestRealMySQLNewQueryPathsExecuteAndIndexesApplied is a runtime-skipped (NOT
// build-tagged) integration test against the REAL MySQL cache server configured in
// .env.development.local (WA_MLWH_CACHE_PATH / WA_MLWH_CACHE_PASSWORD). It is the
// durable MySQL-only guard for the new query paths (D1 run-scoped iRODS + file-type
// filter, D2 study manifest, D3 status-breakdown QC, E faculty-sponsor / user /
// resolve-person) added since the original cache read paths: it builds the cache
// schema in a UNIQUE throwaway database, seeds the shared J1 scenario, asserts each
// new path returns the SAME counts/rows the SQLite-backed hermetic tests pin (with
// the count == len(list) cross-check), and asserts via EXPLAIN that the run-scoped
// iRODS query, the manifest query and the file-type-filtered study iRODS query are
// index-served (a real key, type != ALL, no full scan of the ~9M-row iRODS or
// product-metrics mirrors) and that the /studies/user query is served by a
// study_users_mirror lookup index, not a full scan. The throwaway db is dropped in
// t.Cleanup on success AND failure and never touches the configured cache db; the
// test SKIPS cleanly when the cache env vars are absent or the server unreachable.
func TestRealMySQLNewQueryPathsExecuteAndIndexesApplied(t *testing.T) {
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
	seedJ1ScenarioMySQL(t, writeDB)

	convey.Convey("Given the J1 scenario in a throwaway MySQL database", t, func() {
		convey.Convey("I1.1: each new query path returns the SQLite counts/rows on MySQL", func() {
			assertJ1RunIRODSOnMySQL(ctx, t, cache)
			assertJ1ManifestOnMySQL(ctx, t, cache)
			assertJ1FileTypeStudyIRODSOnMySQL(ctx, t, cache)
			assertJ1IDRun0OnMySQL(ctx, t, cache)
			assertJ1StatusBreakdownQCOnMySQL(ctx, t, cache)
			assertJ1PeopleOnMySQL(ctx, t, cache)
		})

		convey.Convey("I1.2: the run-scoped iRODS, manifest and file-type study iRODS queries are index-served (real key, not a full scan)", func() {
			assertJ1RunIRODSIndexServed(t, writeDB)
			assertJ1ManifestIndexServed(t, writeDB)
			assertJ1FileTypeStudyIRODSIndexServed(t, writeDB)
		})

		convey.Convey("I1.3: the /studies/user query is served by a study_users_mirror index, not a full scan", func() {
			assertJ1StudiesForUserIndexServed(t, writeDB)
		})
	})
}

// seedJ1ScenarioMySQL seeds the shared J1 GoConvey scenario (spec J1) into a
// throwaway MySQL cache so the new query paths can be asserted on MySQL exactly as
// the SQLite tests assert them. It reuses the dialect-neutral plain-INSERT data
// helpers (seedHierarchyStudy / seedManifestSampleRow / seedIseqProductMetricsMirrorRow
// [WithQC] / seedIRODSLocationMirrorRowWithCreatedPlatform / seedOseqFlowcellMirrorRow /
// seedStudyMirrorSearchRow / seedStudyUsersMirrorRow), stamping sync_state with the
// dialect-neutral seedSyncStateRun (NOT the SQLite-only ON CONFLICT seedSyncState),
// and finishes with filler rows + ANALYZE TABLE so the optimizer reports a real
// per-table access path for each mirror under EXPLAIN.
//
// SEED FOOTGUN (D3 Note 1): the QC-count study j1QCStudyLims must keep the strict
// equality qc_pass + qc_fail + qc_pending == samples_total - distinct.registered,
// which holds ONLY because every sequenced (with-data) sample there has a
// product-metrics row. So in that study the registered-only and ONT samples have
// NO product-metrics row AND NO iRODS row, and the artificial non-Illumina /
// id_run=0 iRODS row (needed for the D1 id_run=0 assertion) lives on a SEPARATE
// study (j1IDRun0StudyLims), never attached to a QC-counted sample.
func seedJ1ScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedJ1ManifestStudyMySQL(t, db)
	seedJ1RunStudyMySQL(t, db)
	seedJ1IDRun0StudyMySQL(t, db)
	seedJ1QCStudyMySQL(t, db)
	seedJ1PeopleStudiesMySQL(t, db)
	seedJ1FillerAndSyncStateMySQL(t, db)
}

// seedJ1ManifestStudyMySQL seeds the manifest + file-type study-iRODS scope
// (j1ManifestStudyLims): 3 Illumina products across 2 samples on distinct
// (id_run, position, tag_index) triples (so StudyManifest lists 3 product rows
// with full study metadata), plus .cram iRODS objects for 2 of the 3 products and
// a .crai on one of them (so file_type=cram returns exactly the 2 .cram products
// and the manifest with_irods+cram path is exercised). It mirrors
// seedManifestS1Scenario + TestStudyManifestWithIRODSCramAddsPathPerProductC1.
func seedJ1ManifestStudyMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 211, j1ManifestStudyLims)
	seedManifestSampleRow(t, db, 21, "S1-sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha")
	seedManifestSampleRow(t, db, 22, "S1-sample-beta", "supplier-beta", "EGAN-beta", "sanger-beta")

	seedIseqProductMetricsMirrorRow(t, db, 2101, 21, 52553, 1, 1, j1ManifestStudyLims)
	seedIseqProductMetricsMirrorRow(t, db, 2102, 21, 52553, 1, 2, j1ManifestStudyLims)
	seedIseqProductMetricsMirrorRow(t, db, 2203, 22, 52554, 2, 3, j1ManifestStudyLims)

	// .cram objects for two of the three products (one also carries a .crai, so the
	// file-type filter picks the .cram, not the .crai); product 2203 has none.
	seedIRODSLocationMirrorRow(t, db, "2101", "/seq/52553", "52553_1#1.cram", 21, j1ManifestStudyLims)
	seedIRODSLocationMirrorRow(t, db, "2101", "/seq/52553", "52553_1#1.cram.crai", 21, j1ManifestStudyLims)
	seedIRODSLocationMirrorRow(t, db, "2102", "/seq/52553", "52553_1#2.cram", 21, j1ManifestStudyLims)
}

// seedJ1RunStudyMySQL seeds the run-scoped iRODS scope (j1RunStudyLims): run
// j1Run with six iRODS data objects across its products (four .cram, two .bai),
// each iRODS object matching an Illumina product-metrics row on the run (so id_run
// is derivable), plus a decoy product + iRODS object on j1DecoyRun the run-scope
// query must exclude. It mirrors seedB3RunIRODSScenario's data rows.
func seedJ1RunStudyMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 301, j1RunStudyLims)
	seedHierarchySample(t, db, 301, j1RunStudyLims, "Sr-STDY1")
	seedHierarchySample(t, db, 302, j1RunStudyLims, "Sr-STDY2")

	runProducts := []struct {
		idIseqProduct int64
		idSampleTmp   int64
		position      int
		fileName      string
	}{
		{30001, 301, 1, "52553_1#1.cram"},
		{30002, 301, 1, "52553_1#1.bai"},
		{30003, 301, 2, "52553_1#2.cram"},
		{30004, 302, 3, "52553_2#1.cram"},
		{30005, 302, 3, "52553_2#1.bai"},
		{30006, 302, 4, "52553_2#2.cram"},
	}
	for _, product := range runProducts {
		seedIseqProductMetricsMirrorRow(t, db, product.idIseqProduct, product.idSampleTmp, j1Run, product.position, 1, j1RunStudyLims)
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, formatInt(product.idIseqProduct), "/seq/52553", product.fileName, product.idSampleTmp, j1RunStudyLims, f4DeliveredCreated, "illumina")
	}

	// A decoy product + iRODS object on a different run; the run-scope query must
	// exclude it.
	seedIseqProductMetricsMirrorRow(t, db, 39999, 301, j1DecoyRun, 1, 1, j1RunStudyLims)
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "39999", "/seq/52554", "52554_1#1.cram", 301, j1RunStudyLims, f4DeliveredCreated, "illumina")
}

// seedJ1IDRun0StudyMySQL seeds the id_run=0 scope (j1IDRun0StudyLims): a single
// non-Illumina (ont) iRODS data object whose id_iseq_product matches NO
// product-metrics row, so the study iRODS LEFT JOIN yields id_run=0 while keeping
// the synced platform (B1.2). It is deliberately a SEPARATE study from the QC
// study so this iRODS-only-without-product-metrics row never breaks the QC
// strict-equality (the SEED FOOTGUN).
func seedJ1IDRun0StudyMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 201, j1IDRun0StudyLims)
	seedHierarchySample(t, db, 2001, j1IDRun0StudyLims, "S0-STDY1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "ont-2001", "/seq/ont", "ont_run.fast5", 2001, j1IDRun0StudyLims, f4DeliveredCreated, "ont")
}

// seedJ1QCStudyMySQL seeds the D1q QC-count study (j1QCStudyLims) per spec D1q
// A-E: A delivered (Illumina qc=1 + iRODS) -> qc_pass; B sequenced (two products
// qc=1 and qc=0, no iRODS) -> qc_fail; C sequenced (one product qc NULL, no iRODS)
// -> qc_pending; D registered-only (library link, NO products, NO iRODS) and E ONT
// (oseq_flowcell only, NO products, NO iRODS) -> distinct.registered, excluded
// from QC. It mirrors seedD1qQCScenario's data rows and HEEDS the SEED FOOTGUN:
// the not-sequenced samples carry no product-metrics row and no iRODS row, so
// qc_pass + qc_fail + qc_pending == samples_total - distinct.registered holds on
// MySQL.
func seedJ1QCStudyMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, d1qStudyTmp, j1QCStudyLims)

	for _, id := range []int64{d1qPass, d1qFail, d1qPending, d1qRegistered, d1qONT} {
		seedHierarchySample(t, db, id, j1QCStudyLims, "sample-"+formatInt(id))
		seedLibrarySample(t, db, "Standard", id, j1QCStudyLims)
	}

	// Sample A: delivered Illumina product qc=1 + an iRODS row linked to it -> pass.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 41101, d1qPass, 54410, 1, 1, j1QCStudyLims, sql.NullInt64{Int64: 1, Valid: true})
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "41101", "/seq/54410", "54410_1#1.cram", d1qPass, j1QCStudyLims, f4DeliveredCreated, "illumina")

	// Sample B: two Illumina products qc=1 and qc=0, NO iRODS -> fail (MIN(qc)=0).
	seedIseqProductMetricsMirrorRowWithQC(t, db, 41201, d1qFail, 54410, 2, 1, j1QCStudyLims, sql.NullInt64{Int64: 1, Valid: true})
	seedIseqProductMetricsMirrorRowWithQC(t, db, 41202, d1qFail, 54410, 2, 2, j1QCStudyLims, sql.NullInt64{Int64: 0, Valid: true})

	// Sample C: one Illumina product qc NULL, NO iRODS -> pending.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 41301, d1qPending, 54410, 3, 1, j1QCStudyLims, sql.NullInt64{})

	// Sample E: ONT identity only -- NO products, NO iRODS -> registered. Sample D
	// is registered-only (library link, seeded above), also NO products/iRODS.
	seedOseqFlowcellMirrorRow(t, db, 41501, d1qONT, j1QCStudyLims)
}

// seedJ1PeopleStudiesMySQL seeds the D4 person scenarios across three disjoint
// sub-fixtures whose match terms never cross over, so each people query path is
// asserted on MySQL exactly as its SQLite counterpart pins it:
//
//   - the faculty-sponsor sub-fixture (term "carl"): three SQSCP studies whose
//     faculty_sponsor contains "Carl" (two "Carl Anderson", one lower-case "carl
//     anderson") plus a "Jane Doe" non-match, with NO study_users rows -- so
//     StudiesForFacultySponsor("carl") returns the 3 Carl studies (mirrors E1);
//   - the user sub-fixture (term "ua3"): person login "ua3" / email
//     "ua3@sanger.ac.uk" / name "Ursula Andrews", owner of two studies, manager of
//     a third, follower of a fourth, plus a SEPARATE person "dz9" / "Dora Zane" who
//     is BOTH owner and data_access_contact of one study -- so StudiesForUser("ua3")
//     returns the 3 owner/owner/manager studies and StudiesForUser("dz9") returns
//     the one study twice (mirrors E2's default-roles and same-study-multiple-roles
//     tests, covering the email/login/name and dual-role D4 scenarios);
//   - the resolve sub-fixture (terms "rosa" / "rk9"): faculty_sponsor "Rosa King"
//     on three studies and a study_users owner "rk9" / "Rosa King" on two of them,
//     with distinct study-count bases (3 vs 2) -- so ResolvePerson("rosa") returns
//     both a faculty_sponsor and a study_users candidate and ResolvePerson("rk9")
//     returns the study_users candidate via a login fragment (mirrors E3).
//
// The names are disjoint ("Carl Anderson" vs "Ursula Andrews"/"Dora Zane" vs "Rosa
// King") so a term for one sub-fixture never matches another. It reuses the shared
// dialect-neutral seedStudyMirrorSearchRow / seedStudyUsersMirrorRow plain-INSERT
// helpers.
func seedJ1PeopleStudiesMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedJ1FacultySponsorStudiesMySQL(t, db)
	seedJ1UserStudiesMySQL(t, db)
	seedJ1ResolveStudiesMySQL(t, db)
}

// seedJ1FacultySponsorStudiesMySQL seeds the faculty-sponsor sub-fixture (E1
// mirror): three SQSCP studies whose faculty_sponsor contains "Carl" (two "Carl
// Anderson", one lower-case "carl anderson") plus a "Jane Doe" non-match, with NO
// study_users rows (so the "carl" term resolves to only the faculty_sponsor
// source).
func seedJ1FacultySponsorStudiesMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedStudyMirrorSearchRow(t, db, 5001, "5001", "study-a", "Title A", "Programme A", "Carl Anderson")
	seedStudyMirrorSearchRow(t, db, 5002, "5002", "study-b", "Title B", "Programme B", "Carl Anderson")
	seedStudyMirrorSearchRow(t, db, 5003, "5003", "study-c", "Title C", "Programme C", "carl anderson")
	seedStudyMirrorSearchRow(t, db, 5004, "5004", "study-d", "Title D", "Programme D", "Jane Doe")
}

// seedJ1UserStudiesMySQL seeds the user sub-fixture (E2 mirror): person "ua3"
// (login/email/name Ursula Andrews) owner of 5101,5102, manager of 5103, follower
// of 5104; plus a SEPARATE person "dz9" (Dora Zane) who is BOTH owner and
// data_access_contact of study 5105 (with a duplicate owner row that must collapse
// to one). The studies carry an empty faculty_sponsor so they never match a
// faculty-sponsor or resolve term.
func seedJ1UserStudiesMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedStudyMirrorSearchRow(t, db, 5101, "5101", "study-ua-a", "Title UA A", "Programme", "")
	seedStudyMirrorSearchRow(t, db, 5102, "5102", "study-ua-b", "Title UA B", "Programme", "")
	seedStudyMirrorSearchRow(t, db, 5103, "5103", "study-ua-c", "Title UA C", "Programme", "")
	seedStudyMirrorSearchRow(t, db, 5104, "5104", "study-ua-d", "Title UA D", "Programme", "")
	seedStudyMirrorSearchRow(t, db, 5105, "5105", "study-dz-a", "Title DZ A", "Programme", "")

	seedStudyUsersMirrorRow(t, db, 9101, 5101, "owner", "ua3", "ua3@sanger.ac.uk", "Ursula Andrews")
	seedStudyUsersMirrorRow(t, db, 9102, 5102, "owner", "ua3", "ua3@sanger.ac.uk", "Ursula Andrews")
	seedStudyUsersMirrorRow(t, db, 9103, 5103, "manager", "ua3", "ua3@sanger.ac.uk", "Ursula Andrews")
	seedStudyUsersMirrorRow(t, db, 9104, 5104, "follower", "ua3", "ua3@sanger.ac.uk", "Ursula Andrews")

	seedStudyUsersMirrorRow(t, db, 9105, 5105, "owner", "dz9", "dz9@sanger.ac.uk", "Dora Zane")
	seedStudyUsersMirrorRow(t, db, 9106, 5105, "data_access_contact", "dz9", "dz9@sanger.ac.uk", "Dora Zane")
	seedStudyUsersMirrorRow(t, db, 9107, 5105, "owner", "dz9", "dz9@sanger.ac.uk", "Dora Zane")
}

// seedJ1ResolveStudiesMySQL seeds the resolve sub-fixture (E3 mirror):
// faculty_sponsor "Rosa King" on three studies and a study_users owner "rk9" /
// "Rosa King" on two of them, so ResolvePerson("rosa") returns a faculty_sponsor
// candidate (study_count 3) AND a study_users candidate (study_count 2) -- the two
// bases differ by design -- and ResolvePerson("rk9") returns just the study_users
// candidate (login-fragment match).
func seedJ1ResolveStudiesMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedStudyMirrorSearchRow(t, db, 6001, "6001", "study-rk-a", "Title RK A", "Programme", "Rosa King")
	seedStudyMirrorSearchRow(t, db, 6002, "6002", "study-rk-b", "Title RK B", "Programme", "Rosa King")
	seedStudyMirrorSearchRow(t, db, 6003, "6003", "study-rk-c", "Title RK C", "Programme", "Rosa King")

	seedStudyUsersMirrorRow(t, db, 9601, 6001, "owner", "rk9", "rk9@sanger.ac.uk", "Rosa King")
	seedStudyUsersMirrorRow(t, db, 9602, 6002, "owner", "rk9", "rk9@sanger.ac.uk", "Rosa King")
}

// seedJ1FillerAndSyncStateMySQL stamps the feeding sync tables (and study_users)
// synced with the dialect-neutral seedSyncStateRun and adds filler rows (an
// unrelated study, its iRODS objects, and study_users rows for other logins) so
// each mirror is large enough that the MySQL optimizer reports a real per-table
// access path for the index-served joins (a near-empty table is optimized away and
// never appears in EXPLAIN). The filler never matches any asserted scope/term, so
// it changes no count. It runs ANALYZE TABLE so InnoDB statistics reflect the
// filler.
func seedJ1FillerAndSyncStateMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	// Filler iRODS + product-metrics rows for an unrelated study so the iRODS and
	// product-metrics mirrors are real tables under EXPLAIN. They never match the
	// asserted run/study scopes, so they change no count.
	for i := range 400 {
		product := formatInt(int64(900000 + i))
		seedIseqProductMetricsMirrorRow(t, db, int64(900000+i), int64(900000+i), 99999, 1, 1, "filler-study")
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, product, "/seq/99999", "99999_1#"+product+".cram", int64(900000+i), "filler-study", f4DeliveredCreated, "illumina")
	}

	// Filler study_users rows for unrelated logins so study_users_mirror is a real
	// table under EXPLAIN. None contains "carl"/"ca3"/"anderson", so they never
	// match the asserted person terms.
	for i := range 400 {
		login := "filler" + formatInt(int64(i))
		seedStudyUsersMirrorRow(t, db, int64(800000+i), 5001, "viewer", login, login+"@example.com", "Filler Person "+formatInt(int64(i)))
	}

	base := time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC)
	for i, table := range []string{
		syncTableStudy, syncTableSample, syncTableIseqFlowcell, syncTableIseqProductMetrics,
		syncTablePacBioProductMetrics, syncTableEseqProductMetrics, syncTableUseqProductMetrics,
		syncTableSeqProductIRODSLocations, syncTableSeqOpsTrackingPerSample, syncTableStudyUsers,
	} {
		seedSyncStateRun(t, db, table, base.Add(time.Duration(i)*time.Minute), base.Add(time.Duration(i)*time.Minute))
	}

	for _, table := range []string{
		"seq_product_irods_locations_mirror", "iseq_product_metrics_mirror", "study_users_mirror", "study_mirror",
	} {
		if _, err := db.Exec("ANALYZE TABLE " + table); err != nil {
			t.Fatalf("ANALYZE %s: %v", table, err)
		}
	}
}

// assertJ1RunIRODSOnMySQL asserts IRODSPathsForRun / CountIRODSPathsForRun on
// MySQL, with and without file_type, return the same rows/counts the SQLite B3
// tests pin (six data objects on the run, four of them .cram), with the
// count == len(list) cross-check.
func assertJ1RunIRODSOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	all, err := cache.IRODSPathsForRun(ctx, formatInt(j1Run), "", availabilityFetchAll, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(len(all), convey.ShouldEqual, 6)

	wrongRun := 0
	for _, path := range all {
		if path.IDRun != j1Run {
			wrongRun++
		}
	}
	convey.So(wrongRun, convey.ShouldEqual, 0)

	allCount, err := cache.CountIRODSPathsForRun(ctx, formatInt(j1Run), "")
	convey.So(err, convey.ShouldBeNil)
	convey.So(allCount.Count, convey.ShouldEqual, len(all))

	cram, err := cache.IRODSPathsForRun(ctx, formatInt(j1Run), "cram", availabilityFetchAll, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(len(cram), convey.ShouldEqual, 4)

	cramCount, err := cache.CountIRODSPathsForRun(ctx, formatInt(j1Run), "cram")
	convey.So(err, convey.ShouldBeNil)
	convey.So(cramCount.Count, convey.ShouldEqual, len(cram))
}

// assertJ1ManifestOnMySQL asserts StudyManifest / CountStudyManifest on MySQL,
// with and without with_irods+file_type, return the same rows/counts the SQLite C1
// tests pin (one row per product, study metadata once, the .cram path on the two
// covered products), with the count == len(list) cross-check.
func assertJ1ManifestOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	manifest, err := cache.StudyManifest(ctx, j1ManifestStudyLims, "", false, manifestAllRows, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(manifest.IDStudyLims, convey.ShouldEqual, j1ManifestStudyLims)
	convey.So(manifest.Name, convey.ShouldEqual, "Study "+j1ManifestStudyLims)
	convey.So(manifest.Rows, convey.ShouldHaveLength, 3)
	convey.So(manifest.Rows[0].IRODSPath, convey.ShouldEqual, "")

	count, err := cache.CountStudyManifest(ctx, j1ManifestStudyLims)
	convey.So(err, convey.ShouldBeNil)
	convey.So(count.Count, convey.ShouldEqual, len(manifest.Rows))

	withIRODS, err := cache.StudyManifest(ctx, j1ManifestStudyLims, "cram", true, manifestAllRows, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(withIRODS.Rows, convey.ShouldHaveLength, 3)
	convey.So(withIRODS.Rows[0].IRODSPath, convey.ShouldEqual, "/seq/52553/52553_1#1.cram")
	convey.So(withIRODS.Rows[1].IRODSPath, convey.ShouldEqual, "/seq/52553/52553_1#2.cram")
	convey.So(withIRODS.Rows[2].IRODSPath, convey.ShouldEqual, "")

	withIRODSCount, err := cache.CountStudyManifest(ctx, j1ManifestStudyLims)
	convey.So(err, convey.ShouldBeNil)
	convey.So(withIRODSCount.Count, convey.ShouldEqual, len(withIRODS.Rows))
}

// assertJ1FileTypeStudyIRODSOnMySQL asserts the file-type-filtered study iRODS
// list / count on MySQL return the two .cram products of the manifest study (the
// .crai on one of them does not add a row), with the count == len(list)
// cross-check, mirroring the SQLite B2 study-by-file-type test.
func assertJ1FileTypeStudyIRODSOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	cram, err := cache.IRODSPathsForStudyByFileType(ctx, j1ManifestStudyLims, "cram", availabilityFetchAll, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(len(cram), convey.ShouldEqual, 2)

	nonCram := 0
	for _, path := range cram {
		if !strings.HasSuffix(path.DataObject, ".cram") {
			nonCram++
		}
	}
	convey.So(nonCram, convey.ShouldEqual, 0)

	cramCount, err := cache.CountIRODSPathsForStudyByFileType(ctx, j1ManifestStudyLims, "cram")
	convey.So(err, convey.ShouldBeNil)
	convey.So(cramCount.Count, convey.ShouldEqual, len(cram))
}

// assertJ1IDRun0OnMySQL asserts the non-Illumina iRODS row on the id_run=0 scope
// gets id_run=0 from the study iRODS LEFT JOIN (no matching product-metrics row)
// and keeps its synced platform, mirroring the SQLite B1.2 test. This row lives on
// a SEPARATE study from the QC study, heeding the SEED FOOTGUN.
func assertJ1IDRun0OnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	paths, err := cache.IRODSPathsForStudy(ctx, j1IDRun0StudyLims, availabilityFetchAll, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(paths, convey.ShouldHaveLength, 1)
	convey.So(paths[0].IDRun, convey.ShouldEqual, 0)
	convey.So(paths[0].Platform, convey.ShouldEqual, "ont")
}

// assertJ1StatusBreakdownQCOnMySQL asserts StatusBreakdown.QC on MySQL is the
// pinned {qc_pass:1, qc_fail:1, qc_pending:1} and the D1q strict equality
// qc_pass + qc_fail + qc_pending == samples_total - distinct.registered holds,
// mirroring the SQLite D1q test.
func assertJ1StatusBreakdownQCOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	breakdown, err := cache.StatusBreakdown(ctx, j1QCStudyLims)
	convey.So(err, convey.ShouldBeNil)
	convey.So(breakdown.Distinct, convey.ShouldResemble, PhaseLadder{WithData: 1, SequencedNoData: 2, Registered: 2})
	convey.So(breakdown.QC, convey.ShouldResemble, StudyQCBreakdown{QCPass: 1, QCFail: 1, QCPending: 1})

	samplesTotal := breakdown.Distinct.WithData + breakdown.Distinct.SequencedNoData + breakdown.Distinct.Registered
	qcSum := breakdown.QC.QCPass + breakdown.QC.QCFail + breakdown.QC.QCPending
	convey.So(qcSum, convey.ShouldEqual, samplesTotal-breakdown.Distinct.Registered)
}

// assertJ1PeopleOnMySQL asserts StudiesForFacultySponsor / StudiesForUser /
// ResolvePerson and their counts on MySQL return the same rows/counts the SQLite E
// tests pin, each with the count == len(list) cross-check: the 3 "carl" sponsor
// studies; ua3's 3 owner/owner/manager default-role studies; the dz9 dual-role
// study returned twice; the "rosa" two-source resolve candidates with distinct
// study-count bases; and the "rk9" login-fragment resolve candidate.
func assertJ1PeopleOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	assertJ1FacultySponsorOnMySQL(ctx, t, cache)
	assertJ1StudiesForUserOnMySQL(ctx, t, cache)
	assertJ1ResolvePersonOnMySQL(ctx, t, cache)
}

// assertJ1FacultySponsorOnMySQL asserts StudiesForFacultySponsor("carl") returns
// the 3 Carl studies (case-insensitive substring) with the count == len(list)
// cross-check, mirroring E1.
func assertJ1FacultySponsorOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	sponsor, err := cache.StudiesForFacultySponsor(ctx, "carl", 100, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(peopleStudyLimsIDs(sponsor), convey.ShouldResemble, []string{"5001", "5002", "5003"})
	convey.So(sponsor[0].Study.FacultySponsor, convey.ShouldEqual, "Carl Anderson")
	for _, row := range sponsor {
		convey.So(row.Role, convey.ShouldBeEmpty)
	}

	sponsorCount, err := cache.CountStudiesForFacultySponsor(ctx, "carl")
	convey.So(err, convey.ShouldBeNil)
	convey.So(sponsorCount.Count, convey.ShouldEqual, len(sponsor))
	convey.So(sponsorCount.Count, convey.ShouldEqual, j1FacultyMatches)
}

// assertJ1StudiesForUserOnMySQL asserts StudiesForUser on MySQL: ua3's default
// roles return the 3 owner/owner/manager studies (excluding the follower), and the
// dual-role person dz9 returns the one study twice (owner + data_access_contact),
// each with the count == len(list) cross-check, mirroring E2.
func assertJ1StudiesForUserOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	user, err := cache.StudiesForUser(ctx, "ua3", "", 100, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(peopleStudyLimsIDs(user), convey.ShouldResemble, []string{"5101", "5102", "5103"})
	convey.So(peopleStudyRoles(user), convey.ShouldResemble, []string{"owner", "owner", "manager"})
	userCount, err := cache.CountStudiesForUser(ctx, "ua3", "")
	convey.So(err, convey.ShouldBeNil)
	convey.So(userCount.Count, convey.ShouldEqual, len(user))
	convey.So(userCount.Count, convey.ShouldEqual, j1UserDefault)

	dual, err := cache.StudiesForUser(ctx, "dz9", "", 100, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(peopleStudyLimsIDs(dual), convey.ShouldResemble, []string{"5105", "5105"})
	convey.So(peopleStudyRoles(dual), convey.ShouldResemble, []string{"data_access_contact", "owner"})
	dualCount, err := cache.CountStudiesForUser(ctx, "dz9", "")
	convey.So(err, convey.ShouldBeNil)
	convey.So(dualCount.Count, convey.ShouldEqual, len(dual))
	convey.So(dualCount.Count, convey.ShouldEqual, j1UserDualRole)
}

// assertJ1ResolvePersonOnMySQL asserts ResolvePerson on MySQL: "rosa" returns both
// a faculty_sponsor candidate (study_count 3) and a study_users candidate
// (study_count 2) -- the two bases differ -- and "rk9" returns just the study_users
// candidate via a login fragment, each with the count == len(list) cross-check,
// mirroring E3.
func assertJ1ResolvePersonOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	bothSources, err := cache.ResolvePerson(ctx, "rosa", 100, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(bothSources, convey.ShouldResemble, []PersonCandidate{
		{Source: "faculty_sponsor", Name: "Rosa King", StudyCount: 3},
		{
			Source: "study_users", Name: "Rosa King", Login: "rk9",
			Email: "rk9@sanger.ac.uk", Role: "owner", StudyCount: 2,
		},
	})
	bothCount, err := cache.CountResolvePerson(ctx, "rosa")
	convey.So(err, convey.ShouldBeNil)
	convey.So(bothCount.Count, convey.ShouldEqual, len(bothSources))
	convey.So(bothCount.Count, convey.ShouldEqual, j1ResolveBothSources)

	loginOnly, err := cache.ResolvePerson(ctx, "rk9", 100, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(loginOnly, convey.ShouldResemble, []PersonCandidate{
		{
			Source: "study_users", Name: "Rosa King", Login: "rk9",
			Email: "rk9@sanger.ac.uk", Role: "owner", StudyCount: 2,
		},
	})
	loginCount, err := cache.CountResolvePerson(ctx, "rk9")
	convey.So(err, convey.ShouldBeNil)
	convey.So(loginCount.Count, convey.ShouldEqual, len(loginOnly))
	convey.So(loginCount.Count, convey.ShouldEqual, j1ResolveLoginOnly)
}

// assertJ1RunIRODSIndexServed asserts EXPLAIN of the run-scoped iRODS query (the
// exact SQL the read path uses) serves both joined mirrors -- the product-metrics
// mirror (alias ipm, scoped by id_run) and the iRODS-locations mirror (alias spi,
// joined on id_iseq_product) -- by a real index with no full scan.
func assertJ1RunIRODSIndexServed(t *testing.T, db *sql.DB) {
	t.Helper()

	query, args := irodsRunFileTypeQuery("", j1Run, availabilityFetchAll, 0)
	plans := explainPlanRows(t, db, query, args...)
	assertMirrorIndexServed(plans, "ipm")
	assertMirrorIndexServed(plans, "spi")
}

// assertJ1ManifestIndexServed asserts EXPLAIN of the with_irods+file_type manifest
// query serves both the product-metrics mirror (alias ipm, scoped by
// id_study_lims) and the iRODS-locations mirror (alias spi, the set-at-once LEFT
// JOIN on id_iseq_product + id_study_lims) by a real index with no full scan and
// no per-row dependent subquery.
func assertJ1ManifestIndexServed(t *testing.T, db *sql.DB) {
	t.Helper()

	query, args := manifestListQuery(j1ManifestStudyLims, true, "cram", manifestAllRows, 0)
	plans := explainPlanRows(t, db, query, args...)
	for _, plan := range plans {
		convey.So(strings.ToUpper(plan.selectType), convey.ShouldNotContainSubstring, "DEPENDENT SUBQUERY")
	}
	assertMirrorIndexServed(plans, "ipm")
	assertMirrorIndexServed(plans, "spi")
}

// assertJ1FileTypeStudyIRODSIndexServed asserts EXPLAIN of the file-type-filtered
// study iRODS query serves the iRODS-locations mirror (alias spi, scoped by
// id_study_lims) by a real index with no full scan of the ~9M-row mirror.
func assertJ1FileTypeStudyIRODSIndexServed(t *testing.T, db *sql.DB) {
	t.Helper()

	query, args := irodsFileTypeQuery(irodsPathsForStudyCacheSQLPrefix, irodsPathsForStudyCacheSQLSuffix, "cram", j1ManifestStudyLims, availabilityFetchAll, 0)
	plans := explainPlanRows(t, db, query, args...)
	assertMirrorIndexServed(plans, "spi")
}
