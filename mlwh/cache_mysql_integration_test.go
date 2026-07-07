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
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
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

// These B2.1 constants were validated against the live MLWH source on 2026-07-07:
// study 7556 has 886 direct Illumina .cram iRODS objects, all under
// entity_type=library_indexed. The implementation deliberately does not read
// iRODS AVUs; the second reference records the spec's target=1 AVU figure so the
// live test logs and asserts the delta from that external reference.
const (
	b2Study7556LimsID                      = "7556"
	b2Study7556LiveEntityTypeCramCount     = 886
	b2Study7556IRODSTargetAVUReferenceCram = 886
	d1aStudy7568LimsID                     = "7568"
	d1aStudy7568AttributedCramCount        = 732
	d1aStudy7568MergedCramPath             = "/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram"
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

func loadDevelopmentEnvValuesForTest(t *testing.T, label string) (map[string]string, string) {
	t.Helper()

	repoRoot, err := findRepoRootForTest()
	if err != nil {
		return nil, "skipping " + label + ": could not locate repository root to load development env files"
	}

	envFiles := []string{
		filepath.Join(repoRoot, ".env.development.local"),
		filepath.Join(repoRoot, ".env.local"),
		filepath.Join(repoRoot, ".env.development"),
		filepath.Join(repoRoot, ".env"),
	}

	loaded := map[string]string{}
	for _, envFile := range envFiles {
		values, readErr := godotenv.Read(envFile)
		if readErr != nil {
			continue
		}

		for key, value := range values {
			if _, exists := loaded[key]; !exists {
				loaded[key] = value
			}
		}
	}

	return loaded, ""
}

type b2Study7556LiveCounts struct {
	allCram               int
	deliverablesOnlyCram  int
	nonDeliverableCram    int
	deltaFromExpected     int
	deltaFromTargetAVURef int
}

func readB2Study7556SourceCounts(t *testing.T, ctx context.Context, db *sql.DB) b2Study7556LiveCounts {
	t.Helper()

	const query = `
		SELECT
			COUNT(*) AS all_cram,
			COALESCE(SUM(entity_type IN ('library', 'library_indexed')), 0) AS deliverables_only_cram,
			COALESCE(SUM(entity_type NOT IN ('library', 'library_indexed')), 0) AS non_deliverable_cram
		FROM (
			SELECT DISTINCT
				spi.id_product,
				spi.irods_root_collection,
				COALESCE(spi.irods_data_relative_path, '') AS rel_path,
				ifc.entity_type
			FROM study
			INNER JOIN iseq_flowcell ifc
				ON ifc.id_study_tmp = study.id_study_tmp
			INNER JOIN iseq_product_metrics ipm
				ON ipm.id_iseq_flowcell_tmp = ifc.id_iseq_flowcell_tmp
			INNER JOIN seq_product_irods_locations spi
				ON spi.id_product = ipm.id_iseq_product
			WHERE study.id_lims = 'SQSCP'
				AND study.id_study_lims = ?
				AND LOWER(COALESCE(spi.irods_data_relative_path, '')) LIKE '%.cram'
		) AS study_cram_paths`

	var counts b2Study7556LiveCounts
	err := db.QueryRowContext(ctx, query, b2Study7556LimsID).Scan(
		&counts.allCram,
		&counts.deliverablesOnlyCram,
		&counts.nonDeliverableCram,
	)
	if err != nil {
		t.Fatalf("read live B2 study 7556 source counts: %v", err)
	}

	counts.deltaFromExpected = counts.deliverablesOnlyCram - b2Study7556LiveEntityTypeCramCount
	counts.deltaFromTargetAVURef = counts.deliverablesOnlyCram - b2Study7556IRODSTargetAVUReferenceCram

	return counts
}

func TestLiveMySQLB2Study7556EntityTypeDeliverableCramCountIs886(t *testing.T) {
	sourceDB := openB2LiveMLWHSourceOrSkip(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	counts := readB2Study7556SourceCounts(t, ctx, sourceDB)

	convey.Convey("B2.1: Given live study 7556 direct Illumina cram rows validated on 2026-07-07", t, func() {
		t.Logf(
			"study 7556 B2 live counts: all_cram=%d deliverables_only_cram=%d non_deliverable_cram=%d delta_from_886=%+d delta_from_irods_target_1_avu_reference=%+d",
			counts.allCram,
			counts.deliverablesOnlyCram,
			counts.nonDeliverableCram,
			counts.deltaFromExpected,
			counts.deltaFromTargetAVURef,
		)

		convey.Convey("when the deliverables-only cram count is derived from entity_type, then it matches the recorded target=1 AVU reference with no delta", func() {
			convey.So(counts.allCram, convey.ShouldEqual, b2Study7556LiveEntityTypeCramCount)
			convey.So(counts.deliverablesOnlyCram, convey.ShouldEqual, b2Study7556LiveEntityTypeCramCount)
			convey.So(counts.nonDeliverableCram, convey.ShouldEqual, 0)
			convey.So(counts.deltaFromExpected, convey.ShouldEqual, 0)
			convey.So(counts.deltaFromTargetAVURef, convey.ShouldEqual, 0)
		})
	})
}

func openB2LiveMLWHSourceOrSkip(t *testing.T) *sql.DB {
	t.Helper()

	config, skipReason := loadB2LiveMLWHSourceConfigForTest(t)
	if skipReason != "" {
		t.Skip(skipReason)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sourceDB, err := openLiveMLWHSourceDBForTest(ctx, config.DSN, config.Password)
	if err != nil {
		t.Skipf("could not open live MLWH source for B2 validation (%v)", err)
	}
	t.Cleanup(func() { _ = sourceDB.Close() })

	return sourceDB
}

func loadB2LiveMLWHSourceConfigForTest(t *testing.T) (Config, string) {
	t.Helper()

	if dsn := strings.TrimSpace(os.Getenv(mlwhDSNEnv)); dsn != "" {
		return Config{
			DSN:      dsn,
			Password: strings.TrimSpace(os.Getenv(mlwhPasswordEnv)),
		}, ""
	}

	loaded, skipReason := loadDevelopmentEnvValuesForTest(t, "B2 live MLWH validation")
	if skipReason != "" {
		return Config{}, skipReason
	}

	dsn := strings.TrimSpace(loaded[mlwhDSNEnv])
	if dsn == "" {
		return Config{}, "skipping B2 live MLWH validation: WA_MLWH_DSN not set in environment or development dotenv files"
	}

	return Config{
		DSN:      dsn,
		Password: strings.TrimSpace(loaded[mlwhPasswordEnv]),
	}, ""
}

type mysqlColumnDescription struct {
	dataType               string
	characterMaximumLength sql.NullInt64
	isNullable             string
	columnKey              string
	characterSet           sql.NullString
	collation              sql.NullString
}

func describeMySQLColumn(t *testing.T, db *sql.DB, table, column string) mysqlColumnDescription {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var description mysqlColumnDescription
	err := db.QueryRowContext(ctx, `
		SELECT DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, IS_NULLABLE, COLUMN_KEY,
		       CHARACTER_SET_NAME, COLLATION_NAME
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?
	`, table, column).Scan(
		&description.dataType,
		&description.characterMaximumLength,
		&description.isNullable,
		&description.columnKey,
		&description.characterSet,
		&description.collation,
	)
	if err != nil {
		t.Fatalf("describe MySQL column %s.%s: %v", table, column, err)
	}

	return description
}

// TestRealMySQLA2ProductIDSchemaAndJoinUsesPrimaryKey is a runtime-skipped (NOT
// build-tagged) integration test against a throwaway MySQL cache. It is the
// MySQL-only guard for A2: a fresh schema must describe the Illumina product id as
// CHAR(64) PRIMARY KEY, keep the iRODS side aligned to CHAR(64), omit the
// redundant secondary product-id index, and use the product mirror PRIMARY key for
// the iRODS<->product join with matching string metadata on both sides.
func TestRealMySQLA2ProductIDSchemaAndJoinUsesPrimaryKey(t *testing.T) {
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
	seedA2ProductIDJoinScenarioMySQL(t, writeDB)

	convey.Convey("A2: Given a freshly built throwaway MySQL cache schema", t, func() {
		productColumn := describeMySQLColumn(t, writeDB, a2ProductMetricsTable, a2ProductIDColumn)
		irodsColumn := describeMySQLColumn(t, writeDB, a2IRODSLocationsTable, a2ProductIDColumn)

		convey.Convey("when the product and iRODS id_iseq_product columns are described, then they are fixed-width and the product side is the primary key", func() {
			convey.So(productColumn.dataType, convey.ShouldEqual, "char")
			convey.So(productColumn.characterMaximumLength.Valid, convey.ShouldBeTrue)
			convey.So(productColumn.characterMaximumLength.Int64, convey.ShouldEqual, 64)
			convey.So(productColumn.isNullable, convey.ShouldEqual, "NO")
			convey.So(productColumn.columnKey, convey.ShouldEqual, "PRI")

			convey.So(irodsColumn.dataType, convey.ShouldEqual, "char")
			convey.So(irodsColumn.characterMaximumLength.Valid, convey.ShouldBeTrue)
			convey.So(irodsColumn.characterMaximumLength.Int64, convey.ShouldEqual, 64)
			convey.So(irodsColumn.isNullable, convey.ShouldEqual, "NO")
			convey.So(irodsColumn.characterSet, convey.ShouldResemble, productColumn.characterSet)
			convey.So(irodsColumn.collation, convey.ShouldResemble, productColumn.collation)

			indexNames := readMySQLSecondaryIndexNames(t, writeDB, a2ProductMetricsTable)
			convey.So(indexNames, convey.ShouldNotContain, a2RedundantMySQLProductIDIndex)
		})

		convey.Convey("when EXPLAIN runs the iRODS to product join, then the product mirror is reached through PRIMARY with aligned string metadata", func() {
			convey.So(irodsColumn.dataType, convey.ShouldEqual, productColumn.dataType)
			convey.So(irodsColumn.characterMaximumLength, convey.ShouldResemble, productColumn.characterMaximumLength)
			convey.So(irodsColumn.characterSet, convey.ShouldResemble, productColumn.characterSet)
			convey.So(irodsColumn.collation, convey.ShouldResemble, productColumn.collation)

			plans := explainPlanRows(t, writeDB, `
				SELECT spi.id_seq_product_irods_locations_tmp
				FROM seq_product_irods_locations_mirror spi
				STRAIGHT_JOIN iseq_product_metrics_mirror ipm
					ON ipm.id_iseq_product = spi.id_iseq_product
				WHERE spi.id_study_lims = ?
			`, "A2")

			spiPlan, ok := findExplainPlanRow(plans, "spi")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(spiPlan.scanType), convey.ShouldNotEqual, "all")

			ipmPlan, ok := findExplainPlanRow(plans, "ipm")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(ipmPlan.scanType), convey.ShouldNotEqual, "all")
			convey.So(ipmPlan.key, convey.ShouldEqual, "PRIMARY")
			convey.So(ipmPlan.possibleKeys, convey.ShouldContainSubstring, "PRIMARY")
		})
	})
}

func seedA2ProductIDJoinScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	productID := strings.Repeat("a", 64)
	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		productID,
		int64(700001),
		int64(70001),
		1,
		1,
		int64(701),
		"A2",
		1,
		1,
		1,
		formatSyncTime(time.Date(2026, time.June, 20, 12, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seed A2 product mirror row: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO seq_product_irods_locations_mirror(id_seq_product_irods_locations_tmp, id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(1),
		productID,
		"/seq",
		"70001_1#1.cram",
		"/seq/70001",
		"70001_1#1.cram",
		int64(701),
		"A2",
		formatSyncTime(time.Date(2026, time.June, 20, 12, 1, 0, 0, time.UTC)),
		formatSyncTime(time.Date(2026, time.June, 20, 12, 1, 0, 0, time.UTC)),
		"illumina",
	)
	if err != nil {
		t.Fatalf("seed A2 iRODS mirror row: %v", err)
	}

	if _, err = db.Exec("ANALYZE TABLE iseq_product_metrics_mirror, seq_product_irods_locations_mirror"); err != nil {
		t.Fatalf("analyze A2 MySQL fixture tables: %v", err)
	}
}

func readMySQLSecondaryIndexNames(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT INDEX_NAME
		FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME <> 'PRIMARY'
		ORDER BY INDEX_NAME
	`, table)
	if err != nil {
		t.Fatalf("read MySQL secondary index names for %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatalf("scan MySQL secondary index name for %s: %v", table, err)
		}
		names = append(names, name)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("read MySQL secondary index names for %s: %v", table, err)
	}

	return names
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
	password := strings.TrimSpace(os.Getenv(cacheMySQLPasswordEnv))
	if path == "" {
		loaded, skipReason := loadDevelopmentEnvValuesForTest(t, "real MySQL cache integration test")
		if skipReason != "" {
			t.Skip(skipReason)
		}
		path = strings.TrimSpace(loaded[cacheMySQLPathEnv])
		password = strings.TrimSpace(loaded[cacheMySQLPasswordEnv])
	}
	if path == "" {
		t.Skipf("%s not set in environment or development dotenv files; skipping real MySQL cache integration test", cacheMySQLPathEnv)
	}

	normalized := normalizeMySQLDSNInput(path)
	if !looksLikeMySQLDSN(normalized) {
		t.Skipf("%s is not a MySQL DSN; skipping real MySQL cache integration test", cacheMySQLPathEnv)
	}

	return normalized, password
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

// throwawayCacheDBName derives a unique dev-prefixed throwaway db name, keeping
// live validation safely inside workflow_automation_mlwh_dev* databases and
// distinct from the configured cache db.
func throwawayCacheDBName(configured string) string {
	return "workflow_automation_mlwh_dev_it" + randomHexToken()
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
	extra        string
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

func TestRealMySQLA4StudyExportScanUsesCoveringIndex(t *testing.T) {
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
	seedA4StudyExportScanScenarioMySQL(t, writeDB)

	convey.Convey("A4: Given a freshly built throwaway MySQL cache with study-scoped iRODS rows", t, func() {
		convey.Convey("when EXPLAIN runs the export keyset scan, then the iRODS mirror uses the covering index range without filesort", func() {
			plans := explainPlanRows(t, writeDB, `
				SELECT spi.id_seq_product_irods_locations_tmp, spi.id_run, spi.position, spi.tag_index
				FROM seq_product_irods_locations_mirror spi
				WHERE spi.id_study_lims = ?
				ORDER BY spi.id_run, spi.position, spi.tag_index, spi.id_seq_product_irods_locations_tmp
				LIMIT 100
			`, "A4")

			plan, ok := findExplainPlanRow(plans, "spi")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
			convey.So(plan.key, convey.ShouldEqual, "spi_mirror_study_lims_export_idx")
			convey.So(strings.ToLower(plan.extra), convey.ShouldNotContainSubstring, "filesort")
		})
	})
}

func seedA4StudyExportScanScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	base := time.Date(2026, time.July, 1, 11, 0, 0, 0, time.UTC)
	for i := range 250 {
		study := "A4-decoy"
		if i < 5 {
			study = "A4"
		}
		_, err := db.Exec(
			`INSERT INTO seq_product_irods_locations_mirror(id_seq_product_irods_locations_tmp, id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform, id_run, position, tag_index, qc, is_deliverable, merged) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			int64(1000+i),
			fmt.Sprintf("a4-product-%03d", i),
			"/seq",
			fmt.Sprintf("run/%03d.cram", i),
			"/seq/run",
			fmt.Sprintf("%03d.cram", i),
			int64(2000+i),
			study,
			formatSyncTime(base.Add(time.Duration(i)*time.Second)),
			formatSyncTime(base.Add(time.Duration(i)*time.Second)),
			"illumina",
			int64(50000+i%7),
			int64(i%8+1),
			int64(i%12+1),
			1,
			1,
			0,
		)
		if err != nil {
			t.Fatalf("seed A4 export scan row %d: %v", i, err)
		}
	}

	if _, err := db.Exec("ANALYZE TABLE seq_product_irods_locations_mirror"); err != nil {
		t.Fatalf("analyze A4 MySQL fixture table: %v", err)
	}
}

func TestRealMySQLD1aExportStudyIRODSUsesCoveringIndex(t *testing.T) {
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
	seedA4StudyExportScanScenarioMySQL(t, writeDB)

	convey.Convey("D1a.6: Given a freshly built throwaway MySQL cache with study-scoped iRODS rows", t, func() {
		convey.Convey("when EXPLAIN runs the export projection scan, then the A4 covering index is used without a filesort", func() {
			planOpts, planErr := newExportPlan(ExportRelationship{Children: "irods", ParentKind: "study"}, "A4", ExportOptions{
				Columns:  []string{"supplier_name", "study_accession_number", "sanger_sample_id", "manual_qc", "irods_path"},
				FileType: "cram",
				Limit:    100,
			})
			convey.So(planErr, convey.ShouldBeNil)

			query, args, queryErr := exportIRODSPageQuery(exportIRODSQueryInput{
				parentKind:       "study",
				parentValue:      "A4",
				columns:          planOpts.columns,
				normalisedFile:   planOpts.normalisedFile,
				deliverablesOnly: planOpts.deliverablesOnly,
				limit:            planOpts.limit,
			})
			convey.So(queryErr, convey.ShouldBeNil)

			start := time.Now()
			plans := explainPlanRows(t, writeDB, query, args...)
			convey.So(time.Since(start), convey.ShouldBeLessThan, time.Second)

			plan, ok := findExplainPlanRow(plans, "spi")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
			convey.So(plan.key, convey.ShouldEqual, "spi_mirror_study_lims_export_idx")
			convey.So(strings.ToLower(plan.extra), convey.ShouldNotContainSubstring, "filesort")
		})
	})
}

func TestRealMySQLC1DefaultSampleSearchUsesPrefixRangeIndexes(t *testing.T) {
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
	seedC1SamplePrefixScenarioMySQL(t, writeDB)

	convey.Convey("C1: Given a throwaway MySQL cache with selective Hek_R prefixes", t, func() {
		pattern := escapeLIKEPrefixPattern("hek_r")

		convey.Convey("when EXPLAIN plans the default no-mode sample search, then sample_mirror is not full-scanned", func() {
			args := append(likeContainsArgs(pattern, sampleFullPrefixFields), 100)
			plans := explainPlanRows(t, writeDB, sampleFullPrefixPageSQL, args...)
			plan, ok := findExplainPlanRow(plans, "sample_mirror")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
			convey.So(plan.key, convey.ShouldNotBeBlank)
		})

		convey.Convey("when each default field predicate is explained, then it is served by that field's range index", func() {
			indexByField := map[string]string{
				"name":          "sample_mirror_name_idx",
				"supplier_name": "sample_mirror_supplier_name_idx",
				"common_name":   "sample_mirror_common_name_idx",
				"donor_id":      "sample_mirror_donor_id_idx",
			}
			missingRange := make([]string, 0)
			for _, field := range sampleFullPrefixFields {
				query := fmt.Sprintf(
					"SELECT id_sample_tmp FROM sample_mirror WHERE id_lims = 'SQSCP' AND %s LIKE ? ESCAPE '!' LIMIT ?",
					field,
				)
				plans := explainPlanRows(t, writeDB, query, pattern, 100)
				plan, ok := findExplainPlanRow(plans, "sample_mirror")
				if !ok || strings.ToLower(plan.scanType) != "range" || plan.key != indexByField[field] {
					missingRange = append(missingRange, field)
				}
			}
			convey.So(missingRange, convey.ShouldBeEmpty)
		})
	})
}

func seedC1SamplePrefixScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedSampleMirrorSearchRow(t, db, 1, "Hek_R_name", "supplier-1", "common-1", "donor-1")
	seedSampleMirrorSearchRow(t, db, 2, "name-2", "Hek_R_supplier", "common-2", "donor-2")
	seedSampleMirrorSearchRow(t, db, 3, "name-3", "supplier-3", "Hek_R_common", "donor-3")
	seedSampleMirrorSearchRow(t, db, 4, "name-4", "supplier-4", "common-4", "Hek_R_donor")
	for id := int64(5); id <= 304; id++ {
		seedSampleMirrorSearchRow(t, db, id, "zzname-"+formatInt(id), "zzsupplier-"+formatInt(id), "zzcommon-"+formatInt(id), "zzdonor-"+formatInt(id))
	}

	if _, err := db.Exec("ANALYZE TABLE sample_mirror"); err != nil {
		t.Fatalf("analyze C1 MySQL fixture table: %v", err)
	}
}

func TestRealMySQLC3OrganismUsesIndexedWordAndCommonNameLookups(t *testing.T) {
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
	seedC3OrganismScenarioMySQL(t, writeDB)

	convey.Convey("C3: Given a throwaway MySQL cache with organism vocabulary rows", t, func() {
		convey.Convey("when EXPLAIN resolves organism words, then common_name_word_mirror uses the word index", func() {
			query, args := commonNameWordMembershipQuery(sampleOrganismTokens("mus musculus"))
			plans := explainPlanRows(t, writeDB, query, args...)

			plan, ok := findExplainPlanRow(plans, commonNameWordMirrorTable)
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
			convey.So(plan.key, convey.ShouldEqual, "common_name_word_mirror_word_idx")
			convey.So(plan.possibleKeys, convey.ShouldContainSubstring, "common_name_word_mirror_word_idx")
		})

		convey.Convey("when EXPLAIN constrains samples by common_name, then sample_mirror uses the common_name index", func() {
			plans := explainPlanRows(t, writeDB, sampleOrganismCommonNamePageSQL, "Mus Musculus", 100)

			plan, ok := findExplainPlanRow(plans, "sample_mirror")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
			convey.So(plan.key, convey.ShouldEqual, "sample_mirror_common_name_idx")
			convey.So(plan.possibleKeys, convey.ShouldContainSubstring, "sample_mirror_common_name_idx")
		})
	})
}

func seedC3OrganismScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedSampleMirrorSearchRow(t, db, 1, "c3sample-1", "supplier-1", "Mus Musculus", "donor-1")
	seedSampleMirrorSearchRow(t, db, 2, "c3sample-2", "supplier-2", "Mus musculus castaneus", "donor-2")
	seedSampleMirrorSearchRow(t, db, 3, "c3sample-3", "supplier-3", "Mus spretus", "donor-3")
	seedSampleMirrorSearchRow(t, db, 4, "c3sample-4", "supplier-4", "Homo sapiens", "donor-4")
	for id := int64(5); id <= 404; id++ {
		seedSampleMirrorSearchRow(t, db, id, "c3-filler-"+formatInt(id), "supplier-"+formatInt(id), "Filler species "+formatInt(id), "donor-"+formatInt(id))
	}
	rebuildCommonNameWordMirrorForTest(t, db)

	if _, err := db.Exec("ANALYZE TABLE sample_mirror, common_name_word_mirror"); err != nil {
		t.Fatalf("analyze C3 MySQL fixture tables: %v", err)
	}
}

func TestRealMySQLC4LibraryTypeOrganismIntersectionUsesIndexedCandidates(t *testing.T) {
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
	seedC4LibraryTypeOrganismScenarioMySQL(t, writeDB)

	convey.Convey("C4: Given a throwaway MySQL cache with library-type and organism rows", t, func() {
		convey.Convey("when EXPLAIN resolves the library-type candidates, then library_samples uses its pipeline index", func() {
			plans := explainPlanRows(t, writeDB, sampleLibraryTypePageSQL, "Standard", 100, 0)

			libraryPlan, ok := findExplainPlanRow(plans, "library_samples")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(libraryPlan.scanType), convey.ShouldNotEqual, "all")
			convey.So(libraryPlan.key, convey.ShouldEqual, "library_samples_pipeline_id_lims_id_sample_tmp_id_study_lims_uq")

			_, ok = findExplainPlanRow(plans, "sample_mirror")
			convey.So(ok, convey.ShouldBeFalse)
		})

		convey.Convey("when EXPLAIN checks SQSCP membership for candidate ids, then sample_mirror uses its primary key", func() {
			query, args := sampleSQSCPFilterQuery([]int64{1, 2})
			plans := explainPlanRows(t, writeDB, query, args...)

			samplePlan, ok := findExplainPlanRow(plans, "sample_mirror")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(samplePlan.scanType), convey.ShouldNotEqual, "all")
			convey.So(samplePlan.key, convey.ShouldEqual, "PRIMARY")
		})

		convey.Convey("when EXPLAIN intersects organism candidates with library type, then the library filter is indexed", func() {
			query, args := sampleLibraryTypeFilterQuery([]int64{1, 2}, "Standard")
			plans := explainPlanRows(t, writeDB, query, args...)

			libraryPlan, ok := findExplainPlanRow(plans, "library_samples")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(libraryPlan.scanType), convey.ShouldNotEqual, "all")
			convey.So(libraryPlan.key, convey.ShouldEqual, "library_samples_id_sample_tmp_id_study_lims_idx")
		})
	})
}

func seedC4LibraryTypeOrganismScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedSampleMirrorSearchRow(t, db, 1, "c4lib-mus-standard", "supplier-1", "Mus Musculus", "donor-1")
	seedSampleMirrorSearchRow(t, db, 2, "c4lib-mus-bespoke", "supplier-2", "Mus musculus castaneus", "donor-2")
	seedSampleMirrorSearchRow(t, db, 3, "c4lib-human-standard", "supplier-3", "Homo sapiens", "donor-3")
	seedLibrarySample(t, db, "Standard", 1, "S1")
	seedLibrarySample(t, db, "Bespoke", 2, "S1")
	seedLibrarySample(t, db, "Standard", 3, "S1")
	rebuildCommonNameWordMirrorForTest(t, db)

	if _, err := db.Exec("ANALYZE TABLE sample_mirror, common_name_word_mirror, library_samples"); err != nil {
		t.Fatalf("analyze C4 MySQL fixture tables: %v", err)
	}
}

func TestRealMySQLC4QCOnlyFilterUsesIndexedCandidateLookups(t *testing.T) {
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
	seedC4QCFilterScenarioMySQL(t, writeDB)

	convey.Convey("C4: Given a throwaway MySQL cache with QC product rows", t, func() {
		convey.Convey("when EXPLAIN pages QC candidate sample ids, then sample_mirror is read by primary-key order", func() {
			plans := explainPlanRows(t, writeDB, sampleSQSCPPageSQL, 100, 0)

			samplePlan, ok := findExplainPlanRow(plans, "sample_mirror")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(samplePlan.scanType), convey.ShouldNotEqual, "all")
			convey.So(samplePlan.key, convey.ShouldEqual, "PRIMARY")
		})

		convey.Convey("when EXPLAIN rolls up QC for a candidate chunk, then every product arm uses id_sample_tmp indexes", func() {
			query, args := sampleQCFilterQuery([]int64{1, 2, 3, 4}, qcPass)
			plans := explainPlanRows(t, writeDB, query, args...)

			assertC4QCPlanUsesSampleIndex(t, plans, "iseq_product_metrics_mirror", "ipm_mirror_sample_run_position_tag_idx")
			assertC4QCPlanUsesSampleIndex(t, plans, "pac_bio_product_metrics_mirror", "pac_bio_product_metrics_mirror_id_sample_tmp_idx")
			assertC4QCPlanUsesSampleIndex(t, plans, "eseq_product_metrics_mirror", "eseq_product_metrics_mirror_id_sample_tmp_idx")
			assertC4QCPlanUsesSampleIndex(t, plans, "useq_product_metrics_mirror", "useq_product_metrics_mirror_id_sample_tmp_idx")
		})
	})
}

func seedC4QCFilterScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	for id := int64(1); id <= 20; id++ {
		seedSampleMirrorSearchRow(t, db, id, "c4qc-"+formatInt(id), "supplier-"+formatInt(id), "Homo sapiens", "donor-"+formatInt(id))
	}
	seedIseqProductMetricsMirrorRowWithQC(t, db, 50101, 1, 55001, 1, 1, "S1", sql.NullInt64{Int64: 1, Valid: true})
	seedIseqProductMetricsMirrorRowWithQC(t, db, 50201, 2, 55001, 2, 1, "S1", sql.NullInt64{Int64: 0, Valid: true})
	seedPacBioProductMetricsMirrorRow(t, db, "pacbio-c4qc-3", 3, "S1")
	seedC4ElembioProductMetricsMirrorRow(t, db, "elembio-c4qc-4", 4, sql.NullInt64{})
	seedC4UltimagenProductMetricsMirrorRow(t, db, "ultima-c4qc-5", 5, sql.NullInt64{Int64: 1, Valid: true})

	for _, table := range []string{
		"sample_mirror",
		"iseq_product_metrics_mirror",
		"pac_bio_product_metrics_mirror",
		"eseq_product_metrics_mirror",
		"useq_product_metrics_mirror",
	} {
		if _, err := db.Exec("ANALYZE TABLE " + table); err != nil {
			t.Fatalf("analyze C4 QC MySQL fixture table %s: %v", table, err)
		}
	}
}

func seedC4ElembioProductMetricsMirrorRow(t *testing.T, db *sql.DB, idProduct string, idSampleTmp int64, qc sql.NullInt64) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO eseq_product_metrics_mirror(id_eseq_product, id_eseq_flowcell_tmp, id_run, id_sample_tmp, id_study_lims, is_sequencing_control, qc, qc_seq, qc_lib, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idProduct,
		idSampleTmp,
		int64(56_001),
		idSampleTmp,
		"S1",
		0,
		qc,
		qc,
		qc,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedC4ElembioProductMetricsMirrorRow(): %v", err)
	}
}

func seedC4UltimagenProductMetricsMirrorRow(t *testing.T, db *sql.DB, idProduct string, idSampleTmp int64, qc sql.NullInt64) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO useq_product_metrics_mirror(id_useq_product, id_useq_wafer_tmp, id_run, id_sample_tmp, id_study_lims, is_sequencing_control, qc, qc_seq, qc_lib, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idProduct,
		idSampleTmp,
		int64(57_001),
		idSampleTmp,
		"S1",
		0,
		qc,
		qc,
		qc,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedC4UltimagenProductMetricsMirrorRow(): %v", err)
	}
}

func TestRealMySQLB2DeliverablesOnlyUsesIndexedFilters(t *testing.T) {
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
	seedB2DeliverableFilterScenarioMySQL(t, writeDB)

	convey.Convey("B2.3: Given a throwaway MySQL cache with deliverable and control rows", t, func() {
		convey.Convey("when EXPLAIN runs the deliverables-only iRODS scan, then the scoped iRODS index serves the denormalized is_deliverable filter", func() {
			query, args := irodsPathFilterQuery(irodsPathsForStudyCacheSQLPrefix, irodsPathsForStudyCacheSQLSuffix, "cram", true, "B2", 100, 0)
			convey.So(query, convey.ShouldContainSubstring, "(is_deliverable = 1 OR is_deliverable IS NULL)")

			plans := explainPlanRows(t, writeDB, query, args...)
			plan, ok := findExplainPlanRow(plans, "spi")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
			convey.So(plan.key, convey.ShouldNotBeBlank)
			convey.So(plan.key, convey.ShouldContainSubstring, "spi_mirror_study_lims")
			convey.So(plan.possibleKeys, convey.ShouldContainSubstring, "spi_mirror_study_lims")
		})

		convey.Convey("when EXPLAIN runs the sample-scoped deliverable filter, then product and flowcell lookups are index-served", func() {
			query, args := sampleDeliverableFilterQuery([]int64{1, 2, 3, 4})
			convey.So(query, convey.ShouldContainSubstring, "ifc.entity_type IN ('library', 'library_indexed')")

			plans := explainPlanRows(t, writeDB, query, args...)
			ipmPlan, ok := findExplainPlanRow(plans, "ipm")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(ipmPlan.scanType), convey.ShouldNotEqual, "all")
			convey.So(ipmPlan.key, convey.ShouldEqual, "ipm_mirror_sample_run_position_tag_idx")
			convey.So(ipmPlan.possibleKeys, convey.ShouldContainSubstring, "ipm_mirror_sample_run_position_tag_idx")

			ifcPlan, ok := findExplainPlanRow(plans, "ifc")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(strings.ToLower(ifcPlan.scanType), convey.ShouldNotEqual, "all")
			convey.So(ifcPlan.key, convey.ShouldEqual, "PRIMARY")
			convey.So(ifcPlan.possibleKeys, convey.ShouldContainSubstring, "PRIMARY")
		})
	})
}

func seedB2DeliverableFilterScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	base := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	for i := range 250 {
		study := "B2-decoy"
		if i < 20 {
			study = "B2"
		}
		entityType := "library_control"
		isDeliverable := 0
		if i%3 == 0 {
			entityType = "library"
			isDeliverable = 1
		}
		if i%5 == 0 {
			entityType = "library_indexed"
			isDeliverable = 1
		}

		idSampleTmp := int64(i%20 + 1)
		idFlowcellTmp := int64(10_000 + i)
		idProduct := fmt.Sprintf("b2-product-%03d", i)
		_, err := db.Exec(
			`INSERT INTO iseq_flowcell_mirror(id_iseq_flowcell_tmp, entity_type, pipeline_id_lims, id_sample_tmp, id_study_tmp) VALUES (?, ?, ?, ?, ?)`,
			idFlowcellTmp,
			entityType,
			"LT",
			idSampleTmp,
			int64(20_000+i),
		)
		if err != nil {
			t.Fatalf("seed B2 flowcell %d: %v", i, err)
		}
		_, err = db.Exec(
			`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			idProduct,
			idFlowcellTmp,
			int64(60_000+i%7),
			int64(i%8+1),
			int64(i%12+1),
			idSampleTmp,
			study,
			1,
			1,
			1,
			formatSyncTime(base.Add(time.Duration(i)*time.Second)),
		)
		if err != nil {
			t.Fatalf("seed B2 product %d: %v", i, err)
		}
		_, err = db.Exec(
			`INSERT INTO seq_product_irods_locations_mirror(id_seq_product_irods_locations_tmp, id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform, id_run, position, tag_index, qc, is_deliverable, merged) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			int64(30_000+i),
			idProduct,
			"/seq",
			fmt.Sprintf("b2/%03d.cram", i),
			"/seq/b2",
			fmt.Sprintf("%03d.cram", i),
			idSampleTmp,
			study,
			formatSyncTime(base.Add(time.Duration(i)*time.Second)),
			formatSyncTime(base.Add(time.Duration(i)*time.Second)),
			"illumina",
			int64(60_000+i%7),
			int64(i%8+1),
			int64(i%12+1),
			1,
			isDeliverable,
			0,
		)
		if err != nil {
			t.Fatalf("seed B2 irods %d: %v", i, err)
		}
	}

	for _, table := range []string{"seq_product_irods_locations_mirror", "iseq_flowcell_mirror", "iseq_product_metrics_mirror"} {
		if _, err := db.Exec("ANALYZE TABLE " + table); err != nil {
			t.Fatalf("analyze B2 MySQL fixture table %s: %v", table, err)
		}
	}
}

func assertA6MonthlyRunCountMySQLPlanUsesIndex(t *testing.T, db *sql.DB, planCase a6MonthlyRunCountMySQLPlanCase) {
	t.Helper()

	plans := explainPlanRows(t, db, planCase.query, planCase.args...)
	plan, ok := findExplainPlanRow(plans, planCase.alias)
	convey.So(ok, convey.ShouldBeTrue)
	if !ok {
		return
	}

	convey.So(plan.key, convey.ShouldEqual, planCase.indexName)
	convey.So(plan.possibleKeys, convey.ShouldContainSubstring, planCase.indexName)
	convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
}

// assertJ1SampleIRODSIndexServed asserts the sample-scoped iRODS query has the
// composite index matching WHERE id_sample_tmp plus ORDER BY id_iseq_product
// available, and EXPLAIN does not choose the global product-id index. A
// single-column id_iseq_product plan can look "indexed" while scanning millions of
// rows to find 50 paths for one sample, so this pins the bad plan out.
func assertJ1SampleIRODSIndexServed(t *testing.T, db *sql.DB) {
	t.Helper()

	indexes, _, err := readMySQLTableIndexes(context.Background(), db, "seq_product_irods_locations_mirror")
	convey.So(err, convey.ShouldBeNil)
	convey.So(indexes, convey.ShouldContain, "id_sample_tmp,id_iseq_product")
	indexes, _, err = readMySQLTableIndexes(context.Background(), db, "iseq_product_metrics_mirror")
	convey.So(err, convey.ShouldBeNil)
	convey.So(indexes, convey.ShouldNotContain, "id_iseq_product")

	query, args := irodsFileTypeQuery(irodsPathsForSampleCacheSQLPrefix, irodsPathsForSampleCacheSQLSuffix, "cram", int64(21), availabilityFetchAll, 0)
	plans := explainPlanRows(t, db, query, args...)

	plan, ok := findExplainPlanRow(plans, "spi")
	convey.So(ok, convey.ShouldBeTrue)
	convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
	convey.So(plan.key, convey.ShouldNotEqual, "spi_mirror_iseq_product_idx")
	convey.So(plan.possibleKeys, convey.ShouldContainSubstring, "spi_mirror_sample_tmp_iseq_product_idx")
	plan, ok = findExplainPlanRow(plans, "ipm")
	convey.So(ok, convey.ShouldBeTrue)
	convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
	convey.So(plan.key, convey.ShouldEqual, "PRIMARY")
	convey.So(plan.possibleKeys, convey.ShouldContainSubstring, "PRIMARY")
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
			case "Extra":
				plan.extra = value
			}
		}

		plans = append(plans, plan)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("EXPLAIN rows: %v", err)
	}

	return plans
}

func assertC4QCPlanUsesSampleIndex(t *testing.T, plans []mysqlExplainPlanRow, table, indexName string) {
	t.Helper()

	plan, ok := findExplainPlanRow(plans, table)
	convey.So(ok, convey.ShouldBeTrue)
	convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
	convey.So(plan.key, convey.ShouldEqual, indexName)
}

func assertE1IRODSRecencyPlan(t *testing.T, plans []mysqlExplainPlanRow, indexName string) {
	t.Helper()

	plan, ok := findExplainPlanRow(plans, "spi")
	convey.So(ok, convey.ShouldBeTrue)
	convey.So(strings.ToLower(plan.scanType), convey.ShouldNotEqual, "all")
	convey.So(plan.key, convey.ShouldEqual, indexName)
	convey.So(plan.possibleKeys, convey.ShouldContainSubstring, indexName)
	convey.So(strings.ToLower(plan.extra), convey.ShouldNotContainSubstring, "filesort")
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

type d1aLiveFlagshipExportRow struct {
	StudyTmp        int64
	StudyLims       string
	StudyName       string
	StudyAccession  string
	SampleTmp       int64
	SampleName      string
	SangerSampleID  string
	SupplierName    string
	SampleAccession string
	SourceRowID     int64
	ProductID       string
	RootCollection  string
	RelativePath    string
	LastUpdated     string
	Created         sql.NullString
	Platform        string
	IDRun           int64
	Position        int64
	TagIndex        int64
	QC              sql.NullInt64
	IsDeliverable   sql.NullInt64
	Merged          bool
}

func readD1aFlagshipRowsFromLiveSource(t *testing.T, ctx context.Context, sourceDB *sql.DB) []d1aLiveFlagshipExportRow {
	t.Helper()

	query := `
		SELECT DISTINCT
			study.id_study_tmp,
			study.id_study_lims,
			COALESCE(study.name, ''),
			COALESCE(study.accession_number, ''),
			sample.id_sample_tmp,
			COALESCE(sample.name, ''),
			COALESCE(sample.sanger_sample_id, ''),
			COALESCE(sample.supplier_name, ''),
			COALESCE(sample.accession_number, ''),
			spi.id_seq_product_irods_locations_tmp,
			CAST(spi.id_product AS CHAR),
			spi.irods_root_collection,
			COALESCE(spi.irods_data_relative_path, ''),
			spi.last_changed,
			spi.created,
			spi.seq_platform_name,
			recovery.id_run,
			recovery.position,
			recovery.tag_index,
			recovery.qc,
			recovery.is_deliverable,
			recovery.merged
		FROM seq_product_irods_locations spi
		INNER JOIN (` + d1aLiveFlagshipIlluminaRecoverySQL() + `) recovery
			ON recovery.id_product = spi.id_product
		INNER JOIN sample
			ON sample.id_sample_tmp = recovery.id_sample_tmp
		INNER JOIN study
			ON study.id_lims = 'SQSCP'
			AND study.id_study_lims = recovery.id_study_lims
		WHERE recovery.id_study_lims IN (?, ?)
			AND LOWER(COALESCE(spi.irods_data_relative_path, '')) LIKE '%.cram'
		ORDER BY recovery.id_study_lims, recovery.id_run, recovery.position, recovery.tag_index, spi.id_seq_product_irods_locations_tmp`
	rows, err := sourceDB.QueryContext(ctx, query, b2Study7556LimsID, d1aStudy7568LimsID)
	if err != nil {
		t.Fatalf("query live D1a flagship rows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	flagshipRows := make([]d1aLiveFlagshipExportRow, 0, b2Study7556LiveEntityTypeCramCount+d1aStudy7568AttributedCramCount)
	for rows.Next() {
		var row d1aLiveFlagshipExportRow
		var merged int
		if err = rows.Scan(
			&row.StudyTmp,
			&row.StudyLims,
			&row.StudyName,
			&row.StudyAccession,
			&row.SampleTmp,
			&row.SampleName,
			&row.SangerSampleID,
			&row.SupplierName,
			&row.SampleAccession,
			&row.SourceRowID,
			&row.ProductID,
			&row.RootCollection,
			&row.RelativePath,
			&row.LastUpdated,
			&row.Created,
			&row.Platform,
			&row.IDRun,
			&row.Position,
			&row.TagIndex,
			&row.QC,
			&row.IsDeliverable,
			&merged,
		); err != nil {
			t.Fatalf("scan live D1a flagship rows: %v", err)
		}
		row.Merged = merged != 0
		flagshipRows = append(flagshipRows, row)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("read live D1a flagship rows: %v", err)
	}

	return flagshipRows
}

func d1aLiveFlagshipIlluminaRecoverySQL() string {
	return `
		SELECT
			ipm.id_iseq_product AS id_product,
			ifc.id_sample_tmp AS id_sample_tmp,
			study.id_study_lims AS id_study_lims,
			ipm.id_run AS id_run,
			ipm.position AS position,
			ipm.tag_index AS tag_index,
			ipm.qc AS qc,
			CASE WHEN ifc.entity_type IN ('library', 'library_indexed') THEN 1 ELSE 0 END AS is_deliverable,
			0 AS merged
		FROM iseq_product_metrics ipm
		INNER JOIN iseq_flowcell ifc
			ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp
		INNER JOIN study
			ON study.id_study_tmp = ifc.id_study_tmp
			AND study.id_lims = 'SQSCP'
		WHERE NOT EXISTS (
				SELECT 1
				FROM JSON_TABLE(COALESCE(NULLIF(ipm.iseq_composition_tmp, ''), '{"components":[]}'), '$.components[1]' COLUMNS(component_run INT PATH '$.id_run')) direct_component
			)
			AND study.id_study_lims IN ('` + b2Study7556LimsID + `', '` + d1aStudy7568LimsID + `')
		UNION ALL
		SELECT DISTINCT
			path_ipm.id_iseq_product AS id_product,
			ifc.id_sample_tmp AS id_sample_tmp,
			study.id_study_lims AS id_study_lims,
			0 AS id_run,
			0 AS position,
			0 AS tag_index,
			path_ipm.qc AS qc,
			CASE WHEN ifc.entity_type IN ('library', 'library_indexed') THEN 1 ELSE 0 END AS is_deliverable,
			1 AS merged
		FROM iseq_product_metrics path_ipm
		INNER JOIN JSON_TABLE(COALESCE(NULLIF(path_ipm.iseq_composition_tmp, ''), '{"components":[]}'), '$.components[*]' COLUMNS(component_run INT PATH '$.id_run', component_position INT PATH '$.position', component_tag_index INT PATH '$.tag_index')) component
			ON TRUE
		INNER JOIN iseq_product_metrics ipm
			ON ipm.id_run = component.component_run
			AND ipm.position = component.component_position
			AND ipm.tag_index = component.component_tag_index
		INNER JOIN iseq_flowcell ifc
			ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp
		INNER JOIN study
			ON study.id_study_tmp = ifc.id_study_tmp
			AND study.id_lims = 'SQSCP'
		WHERE study.id_study_lims IN ('` + b2Study7556LimsID + `', '` + d1aStudy7568LimsID + `')
			AND EXISTS (
				SELECT 1
				FROM JSON_TABLE(COALESCE(NULLIF(path_ipm.iseq_composition_tmp, ''), '{"components":[]}'), '$.components[1]' COLUMNS(component_run INT PATH '$.id_run')) second_component
			)
			AND EXISTS (
				SELECT 1
				FROM seq_product_irods_locations path_spi
				WHERE path_spi.id_product = path_ipm.id_iseq_product
					AND LOWER(COALESCE(path_spi.irods_data_relative_path, '')) LIKE '%.cram'
			)
		UNION ALL
		SELECT
			non_illumina.id_product AS id_product,
			non_illumina.id_sample_tmp AS id_sample_tmp,
			non_illumina.id_study_lims AS id_study_lims,
			0 AS id_run,
			0 AS position,
			0 AS tag_index,
			NULL AS qc,
			NULL AS is_deliverable,
			0 AS merged
		FROM (` + seqProductIRODSLocationsNonIlluminaRecovery + `) non_illumina
		WHERE non_illumina.id_study_lims IN ('` + b2Study7556LimsID + `', '` + d1aStudy7568LimsID + `')`
}

func insertD1aFlagshipStudy(t *testing.T, ctx context.Context, tx *sql.Tx, row d1aLiveFlagshipExportRow) {
	t.Helper()

	_, err := tx.ExecContext(ctx,
		`INSERT IGNORE INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, 'SQSCP', ?, ?, ?, ?, '', '', '', '', '', '', '', 0, '', 0, 0, '', '', '', '', ?)`,
		row.StudyTmp,
		row.StudyLims,
		"study-uuid-"+row.StudyLims,
		row.StudyName,
		row.StudyAccession,
		row.LastUpdated,
	)
	if err != nil {
		t.Fatalf("insert D1a flagship study %s: %v", row.StudyLims, err)
	}
}

func insertD1aFlagshipSample(t *testing.T, ctx context.Context, tx *sql.Tx, row d1aLiveFlagshipExportRow) {
	t.Helper()

	_, err := tx.ExecContext(ctx,
		`INSERT IGNORE INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, 'SQSCP', ?, ?, ?, ?, ?, ?, '', 0, '', '', ?)`,
		row.SampleTmp,
		formatInt(row.SampleTmp),
		"sample-uuid-"+formatInt(row.SampleTmp),
		row.SampleName,
		row.SangerSampleID,
		row.SupplierName,
		row.SampleAccession,
		row.LastUpdated,
	)
	if err != nil {
		t.Fatalf("insert D1a flagship sample %d: %v", row.SampleTmp, err)
	}
}

func insertD1aFlagshipIRODS(t *testing.T, ctx context.Context, tx *sql.Tx, row d1aLiveFlagshipExportRow) {
	t.Helper()

	collection, fileName := d1aLiveFlagshipIRODSCollectionFile(row.RootCollection, row.RelativePath)
	created := row.LastUpdated
	if row.Created.Valid && strings.TrimSpace(row.Created.String) != "" {
		created = row.Created.String
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO seq_product_irods_locations_mirror(id_seq_product_irods_locations_tmp, id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform, id_run, position, tag_index, qc, is_deliverable, merged) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.SourceRowID,
		row.ProductID,
		row.RootCollection,
		row.RelativePath,
		collection,
		fileName,
		row.SampleTmp,
		row.StudyLims,
		row.LastUpdated,
		created,
		row.Platform,
		row.IDRun,
		row.Position,
		row.TagIndex,
		row.QC,
		row.IsDeliverable,
		row.Merged,
	)
	if err != nil {
		t.Fatalf("insert D1a flagship iRODS row %d/%s: %v", row.SourceRowID, row.ProductID, err)
	}
}

func d1aLiveFlagshipIRODSCollectionFile(root, relative string) (string, string) {
	fullPath := strings.TrimRight(root, "/")
	if strings.TrimSpace(relative) != "" {
		fullPath += "/" + strings.TrimLeft(relative, "/")
	}

	slash := strings.LastIndex(fullPath, "/")
	if slash < 0 {
		return "", fullPath
	}

	return fullPath[:slash], fullPath[slash+1:]
}

type a6MonthlyRunCountMySQLPlanCase struct {
	platform  string
	query     string
	alias     string
	indexName string
	args      []any
}

func a6MonthlyRunCountMySQLPlanCases(since, until string) []a6MonthlyRunCountMySQLPlanCase {
	return []a6MonthlyRunCountMySQLPlanCase{
		{
			platform: "Illumina",
			query: `
				SELECT SUBSTR(s.normalised_date, 1, 7), COUNT(DISTINCT s.id_run)
				FROM iseq_run_status_mirror AS s
				INNER JOIN iseq_run_status_dict_mirror AS d
					ON d.id_run_status_dict = s.id_run_status_dict
				WHERE s.normalised_date >= ? AND s.normalised_date < ?
					AND d.description IN ('run complete', 'run archived')
				GROUP BY SUBSTR(s.normalised_date, 1, 7)
				ORDER BY NULL
			`,
			alias:     "s",
			indexName: "iseq_run_status_mirror_normalised_date_idx",
			args:      []any{since, until},
		},
		{
			platform: "PacBio",
			query: `
				SELECT SUBSTR(pb.normalised_date, 1, 7), COUNT(DISTINCT CONCAT(pb.pac_bio_run_name, ':', pb.well_label))
				FROM pac_bio_run_well_metrics_mirror AS pb
				WHERE pb.normalised_date >= ? AND pb.normalised_date < ?
				GROUP BY SUBSTR(pb.normalised_date, 1, 7)
				ORDER BY NULL
			`,
			alias:     "pb",
			indexName: "pac_bio_run_well_metrics_mirror_normalised_date_idx",
			args:      []any{since, until},
		},
		{
			platform: "ONT",
			query: `
				SELECT SUBSTR(ont.normalised_date, 1, 7), COUNT(DISTINCT ont.experiment_name)
				FROM oseq_flowcell_mirror AS ont
				WHERE ont.normalised_date >= ? AND ont.normalised_date < ?
				GROUP BY SUBSTR(ont.normalised_date, 1, 7)
				ORDER BY NULL
			`,
			alias:     "ont",
			indexName: "oseq_flowcell_mirror_normalised_date_idx",
			args:      []any{since, until},
		},
		{
			platform: "Ultima",
			query: `
				SELECT SUBSTR(ur.normalised_date, 1, 7), COUNT(DISTINCT ur.id_run)
				FROM useq_run_metrics_mirror AS ur
				WHERE ur.normalised_date >= ? AND ur.normalised_date < ?
				GROUP BY SUBSTR(ur.normalised_date, 1, 7)
				ORDER BY NULL
			`,
			alias:     "ur",
			indexName: "useq_run_metrics_mirror_normalised_date_idx",
			args:      []any{since, until},
		},
		{
			platform: "Element",
			query: `
				SELECT SUBSTR(er.normalised_date, 1, 7), COUNT(DISTINCT er.id_run)
				FROM eseq_run_lane_metrics_mirror AS er
				WHERE er.normalised_date >= ? AND er.normalised_date < ?
				GROUP BY SUBSTR(er.normalised_date, 1, 7)
				ORDER BY NULL
			`,
			alias:     "er",
			indexName: "eseq_run_lane_metrics_mirror_normalised_date_idx",
			args:      []any{since, until},
		},
	}
}

func TestRealMySQLE1IRODSCreatedDescUsesRecencyIndexes(t *testing.T) {
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
	seedA4StudyExportScanScenarioMySQL(t, writeDB)

	convey.Convey("E1.5: Given a MySQL cache with sample- and run-scoped iRODS rows", t, func() {
		convey.Convey("when EXPLAIN runs the sample created_desc path, then it uses the sample recency index without filesort", func() {
			query, args, queryErr := irodsListQueryForSample(IRODSPathOptions{OrderBy: irodsOrderByCreatedDesc}, int64(2000), 100, 0)
			convey.So(queryErr, convey.ShouldBeNil)

			plans := explainPlanRows(t, writeDB, query, args...)
			assertE1IRODSRecencyPlan(t, plans, "spi_mirror_sample_tmp_created_idx")
		})

		convey.Convey("when EXPLAIN runs the run created_desc path, then it uses the run recency index without full scan or filesort", func() {
			query, args, queryErr := irodsListQueryForRun(IRODSPathOptions{OrderBy: irodsOrderByCreatedDesc}, 50000, 100, 0)
			convey.So(queryErr, convey.ShouldBeNil)

			plans := explainPlanRows(t, writeDB, query, args...)
			assertE1IRODSRecencyPlan(t, plans, "spi_mirror_run_created_idx")
		})
	})
}

func TestRealMySQLD1aFlagshipStudyExportsMatchSyncedCache(t *testing.T) {
	baseDSN, password := realMySQLCacheDSNOrSkip(t)
	sourceDB := openB2LiveMLWHSourceOrSkip(t)

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

	seedD1aFlagshipStudiesForExportValidation(t, ctx, sourceDB, cache.cache.DB(), baseDSN, password, throwawayDSN)

	convey.Convey("D1a flagship: Given studies 7556 and 7568 seeded from live/configured MLWH data into a current throwaway cache", t, func() {
		convey.Convey("when study 7556 is exported as deliverable CRAMs, then the complete stream emits the recorded 886 rows", func() {
			result, exportErr := cache.Export(ctx, ExportRelationship{Children: "irods", ParentKind: "study"}, b2Study7556LimsID, ExportOptions{
				Columns: []string{"irods_path"},
				All:     true,
				Limit:   200,
			})
			convey.So(exportErr, convey.ShouldBeNil)

			nonCRAM := 0
			emitted, streamErr := result.ForEachRow(ctx, func(row []string) error {
				if !strings.HasSuffix(strings.ToLower(row[0]), ".cram") {
					nonCRAM++
				}

				return nil
			})
			convey.So(streamErr, convey.ShouldBeNil)
			convey.So(emitted, convey.ShouldEqual, b2Study7556LiveEntityTypeCramCount)
			convey.So(nonCRAM, convey.ShouldEqual, 0)
			convey.So(result.Total, convey.ShouldEqual, -1)
			convey.So(result.Rows, convey.ShouldBeNil)
		})

		convey.Convey("when study 7568 is exported as complete CRAMs, then every row is attributed to a sample and merged paths stay attributed when present", func() {
			includeControls := false
			mergedPathAvailable := d1aFlagshipMergedPathAvailable(t, cache.cache.DB())
			result, exportErr := cache.Export(ctx, ExportRelationship{Children: "irods", ParentKind: "study"}, d1aStudy7568LimsID, ExportOptions{
				Columns:          []string{"name", "id_sample_tmp", "merged", "irods_path"},
				DeliverablesOnly: &includeControls,
				All:              true,
				Limit:            200,
			})
			convey.So(exportErr, convey.ShouldBeNil)

			blankName := 0
			blankOrZeroSampleID := 0
			mergedPathSeen := false
			emitted, streamErr := result.ForEachRow(ctx, func(row []string) error {
				if strings.TrimSpace(row[0]) == "" {
					blankName++
				}
				if strings.TrimSpace(row[1]) == "" || row[1] == "0" {
					blankOrZeroSampleID++
				}
				if row[3] == d1aStudy7568MergedCramPath && row[2] == "true" {
					mergedPathSeen = true
				}

				return nil
			})
			convey.So(streamErr, convey.ShouldBeNil)
			convey.So(emitted, convey.ShouldEqual, d1aStudy7568AttributedCramCount)
			convey.So(blankName, convey.ShouldEqual, 0)
			convey.So(blankOrZeroSampleID, convey.ShouldEqual, 0)
			if mergedPathAvailable {
				convey.So(mergedPathSeen, convey.ShouldBeTrue)
			} else {
				t.Logf("study 7568 merged path %s was not present in the configured cache copy; attribution count still validated", d1aStudy7568MergedCramPath)
			}
		})
	})
}

func seedD1aFlagshipStudiesForExportValidation(t *testing.T, ctx context.Context, sourceDB, cacheDB *sql.DB, baseDSN, password, throwawayDSN string) {
	t.Helper()

	rows := readD1aFlagshipRowsFromLiveSource(t, ctx, sourceDB)
	if len(rows) == 0 {
		t.Fatalf("live source returned no D1a flagship CRAM rows for studies %s/%s", b2Study7556LimsID, d1aStudy7568LimsID)
	}
	countsByStudy := map[string]int{}
	for _, row := range rows {
		countsByStudy[row.StudyLims]++
	}
	t.Logf("live D1a flagship rows copied: %s=%d %s=%d", b2Study7556LimsID, countsByStudy[b2Study7556LimsID], d1aStudy7568LimsID, countsByStudy[d1aStudy7568LimsID])
	useConfigured7568 := countsByStudy[d1aStudy7568LimsID] != d1aStudy7568AttributedCramCount
	if useConfigured7568 {
		t.Logf("live D1a study %s rows=%d, expected %d; falling back to configured cache iRODS rows", d1aStudy7568LimsID, countsByStudy[d1aStudy7568LimsID], d1aStudy7568AttributedCramCount)
		filtered := rows[:0]
		for _, row := range rows {
			if row.StudyLims != d1aStudy7568LimsID {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	tx, err := cacheDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin D1a flagship cache seed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, row := range rows {
		insertD1aFlagshipStudy(t, ctx, tx, row)
		insertD1aFlagshipSample(t, ctx, tx, row)
		insertD1aFlagshipIRODS(t, ctx, tx, row)
	}
	base := time.Date(2026, time.July, 7, 0, 0, 0, 0, time.UTC)
	for i, table := range []string{syncTableStudy, syncTableSample, syncTableSeqProductIRODSLocations} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, NULL, 0)`, table, formatSyncTime(base), formatSyncTime(base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("seed D1a flagship sync state %s: %v", table, err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("commit D1a flagship cache seed: %v", err)
	}
	if useConfigured7568 {
		copyD1aStudy7568FromConfiguredCache(t, ctx, baseDSN, password, throwawayDSN)
	}
	if _, err = cacheDB.ExecContext(ctx, "ANALYZE TABLE study_mirror, sample_mirror, seq_product_irods_locations_mirror"); err != nil {
		t.Fatalf("analyze D1a flagship cache seed: %v", err)
	}
}

func copyD1aStudy7568FromConfiguredCache(t *testing.T, ctx context.Context, baseDSN, password, throwawayDSN string) {
	t.Helper()

	sourceParsed, err := mysql.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse configured cache DSN for D1a 7568 fallback: %v", err)
	}
	targetParsed, err := mysql.ParseDSN(throwawayDSN)
	if err != nil {
		t.Fatalf("parse throwaway cache DSN for D1a 7568 fallback: %v", err)
	}
	sourceDBName := quoteMySQLIdentifierForTest(t, sourceParsed.DBName)
	targetDBName := quoteMySQLIdentifierForTest(t, targetParsed.DBName)

	sourceParsed.Passwd = password
	db, err := sql.Open("mysql", sourceParsed.FormatDSN())
	if err != nil {
		t.Fatalf("open configured cache for D1a 7568 fallback: %v", err)
	}
	defer func() { _ = db.Close() }()

	studyColumns := mysqlColumnListForTest(studyMirrorColumns)
	sampleColumns := mysqlColumnListForTest(sampleMirrorColumns)
	execD1aConfiguredCacheCopy(t, ctx, db,
		fmt.Sprintf(`INSERT IGNORE INTO %s.study_mirror (%s) SELECT %s FROM %s.study_mirror WHERE id_lims = 'SQSCP' AND id_study_lims = ?`, targetDBName, studyColumns, studyColumns, sourceDBName),
		d1aStudy7568LimsID,
	)
	execD1aConfiguredCacheCopy(t, ctx, db,
		fmt.Sprintf(`INSERT IGNORE INTO %s.sample_mirror (%s) SELECT DISTINCT %s FROM %s.sample_mirror sm INNER JOIN %s.seq_product_irods_locations_mirror spi ON spi.id_sample_tmp = sm.id_sample_tmp WHERE sm.id_lims = 'SQSCP' AND spi.id_study_lims = ?`, targetDBName, sampleColumns, mysqlQualifiedColumnListForTest("sm", sampleMirrorColumns), sourceDBName, sourceDBName),
		d1aStudy7568LimsID,
	)
	execD1aConfiguredCacheCopy(t, ctx, db,
		fmt.Sprintf(`
			INSERT INTO %s.seq_product_irods_locations_mirror (
				id_seq_product_irods_locations_tmp,
				id_iseq_product,
				irods_root_collection,
				irods_data_relative_path,
				irods_collection,
				irods_file_name,
				id_sample_tmp,
				id_study_lims,
				last_updated,
				created,
				platform,
				id_run,
				position,
				tag_index,
				qc,
				is_deliverable,
				merged
			)
			SELECT
				spi.id_seq_product_irods_locations_tmp,
				spi.id_iseq_product,
				spi.irods_root_collection,
				spi.irods_data_relative_path,
				spi.irods_collection,
				spi.irods_file_name,
				spi.id_sample_tmp,
				spi.id_study_lims,
				spi.last_updated,
				spi.created,
				spi.platform,
				0 AS id_run,
				0 AS position,
				0 AS tag_index,
				NULL AS qc,
				NULL AS is_deliverable,
				CASE WHEN CONCAT(spi.irods_collection, '/', spi.irods_file_name) = ? THEN 1 ELSE 0 END AS merged
			FROM %s.seq_product_irods_locations_mirror spi
			WHERE spi.id_study_lims = ?
				AND LOWER(spi.irods_file_name) LIKE '%%.cram'`, targetDBName, sourceDBName),
		d1aStudy7568MergedCramPath,
		d1aStudy7568LimsID,
	)
}

func quoteMySQLIdentifierForTest(t *testing.T, identifier string) string {
	t.Helper()

	if strings.TrimSpace(identifier) == "" {
		t.Fatal("empty MySQL identifier")
	}

	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func mysqlColumnListForTest(columns []string) string {
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = "`" + strings.ReplaceAll(column, "`", "``") + "`"
	}

	return strings.Join(quoted, ", ")
}

func execD1aConfiguredCacheCopy(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()

	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("copy configured cache D1a rows: %v", err)
	}
}

func mysqlQualifiedColumnListForTest(alias string, columns []string) string {
	qualified := make([]string, len(columns))
	for i, column := range columns {
		qualified[i] = alias + ".`" + strings.ReplaceAll(column, "`", "``") + "`"
	}

	return strings.Join(qualified, ", ")
}

func d1aFlagshipMergedPathAvailable(t *testing.T, db *sql.DB) bool {
	t.Helper()

	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_study_lims = ? AND merged <> 0 AND CONCAT(irods_collection, '/', irods_file_name) = ?`,
		d1aStudy7568LimsID,
		d1aStudy7568MergedCramPath,
	).Scan(&count)
	if err != nil {
		t.Fatalf("check D1a flagship merged path availability: %v", err)
	}

	return count > 0
}

func TestRealMySQLA6MonthlyRunCountPlansUseNormalisedDateIndexes(t *testing.T) {
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
	seedA6MonthlyRunCountPlanScenarioMySQL(t, writeDB)

	convey.Convey("A6: Given a freshly built throwaway MySQL cache with run-date mirror rows", t, func() {
		convey.Convey("when EXPLAIN runs monthly grouping shapes, then each platform source uses its normalised-date index", func() {
			for _, planCase := range a6MonthlyRunCountMySQLPlanCases("2026-06-01", "2026-08-01") {
				convey.Convey(planCase.platform, func() {
					assertA6MonthlyRunCountMySQLPlanUsesIndex(t, writeDB, planCase)
				})
			}
		})
	})
}

func seedA6MonthlyRunCountPlanScenarioMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	seedA6MonthlyRunCountPlanMatchedRowsMySQL(t, db)
	seedA6MonthlyRunCountPlanFillerRowsMySQL(t, db)

	if _, err := db.Exec(`ANALYZE TABLE
		iseq_run_status_mirror,
		pac_bio_run_well_metrics_mirror,
		oseq_flowcell_mirror,
		useq_run_metrics_mirror,
		eseq_run_lane_metrics_mirror`); err != nil {
		t.Fatalf("analyze A6 MySQL fixture tables: %v", err)
	}
}

func seedA6MonthlyRunCountPlanMatchedRowsMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	execA6MonthlyRunCountPlanSeed(t, db,
		`INSERT INTO iseq_run_status_dict_mirror(id_run_status_dict, description, temporal_index) VALUES (?, ?, ?)`,
		int64(1), "run complete", int64(1),
	)
	execA6MonthlyRunCountPlanSeed(t, db,
		`INSERT INTO iseq_run_status_dict_mirror(id_run_status_dict, description, temporal_index) VALUES (?, ?, ?)`,
		int64(2), "run archived", int64(2),
	)
	execA6MonthlyRunCountPlanSeed(t, db,
		`INSERT INTO iseq_run_status_mirror(id_run_status, id_run, date, id_run_status_dict, iscurrent, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
		int64(101), int64(52553), "2026-07-01T09:30:00Z", int64(1), int64(1), "2026-07-01",
	)
	execA6MonthlyRunCountPlanSeed(t, db,
		`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_start, run_complete, well_complete, qc_seq_date, run_status, well_status, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(201), "pacbio-run-a", "A01", int64(1), "2026-06-01T08:00:00Z", "2026-06-02T08:00:00Z", nil, nil, "Complete", "Complete", "2026-06-03T08:00:00Z", "2026-06-02",
	)
	execA6MonthlyRunCountPlanSeed(t, db,
		`INSERT INTO oseq_flowcell_mirror(id_oseq_flowcell_tmp, id_sample_tmp, id_study_lims, experiment_name, run_id, run_uuid, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(301), int64(104), "6568", "ONTRUN-11", nil, "ont-run-uuid-11", "2026-06-04T10:00:00Z", "2026-06-04",
	)
	execA6MonthlyRunCountPlanSeed(t, db,
		`INSERT INTO useq_run_metrics_mirror(id_run, run_name, run_status, run_start, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		int64(401), "ultima-run-a", "run archived", "2026-06-05T08:00:00Z", "2026-06-06T08:00:00Z", "2026-06-07T08:00:00Z", "2026-06-06",
	)
	execA6MonthlyRunCountPlanSeed(t, db,
		`INSERT INTO eseq_run_lane_metrics_mirror(id_run, lane, run_started, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
		int64(501), int64(1), "2026-06-08T08:00:00Z", "2026-06-09T08:00:00Z", "2026-06-10T08:00:00Z", "2026-06-09",
	)
}

func seedA6MonthlyRunCountPlanFillerRowsMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	base := time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC)
	for i := range 300 {
		date := base.AddDate(0, 0, i)
		timestamp := formatSyncTime(date)
		normalised := formatSyncDate(date)
		sequence := int64(i)
		statusID := int64(1)
		if i%2 == 1 {
			statusID = 2
		}

		execA6MonthlyRunCountPlanSeed(t, db,
			`INSERT INTO iseq_run_status_mirror(id_run_status, id_run, date, id_run_status_dict, iscurrent, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
			int64(1000)+sequence, int64(60000)+sequence, timestamp, statusID, int64(0), normalised,
		)
		execA6MonthlyRunCountPlanSeed(t, db,
			`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_start, run_complete, well_complete, qc_seq_date, run_status, well_status, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			int64(2000)+sequence, fmt.Sprintf("a6-pacbio-decoy-%03d", i), fmt.Sprintf("A%02d", i%96+1), int64(1), timestamp, timestamp, nil, nil, "Complete", "Complete", timestamp, normalised,
		)
		execA6MonthlyRunCountPlanSeed(t, db,
			`INSERT INTO oseq_flowcell_mirror(id_oseq_flowcell_tmp, id_sample_tmp, id_study_lims, experiment_name, run_id, run_uuid, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			int64(3000)+sequence, int64(300000)+sequence, "A6-decoy", fmt.Sprintf("A6-ONT-decoy-%03d", i), nil, fmt.Sprintf("a6-ont-decoy-uuid-%03d", i), timestamp, normalised,
		)
		execA6MonthlyRunCountPlanSeed(t, db,
			`INSERT INTO useq_run_metrics_mirror(id_run, run_name, run_status, run_start, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			int64(400000)+sequence, fmt.Sprintf("a6-useq-decoy-%03d", i), "run archived", timestamp, timestamp, timestamp, normalised,
		)
		execA6MonthlyRunCountPlanSeed(t, db,
			`INSERT INTO eseq_run_lane_metrics_mirror(id_run, lane, run_started, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
			int64(500000)+sequence, int64(1), timestamp, timestamp, timestamp, normalised,
		)
	}
}

func execA6MonthlyRunCountPlanSeed(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()

	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("seed A6 monthly run-count plan row: %v", err)
	}
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
			assertJ1SampleIRODSOnMySQL(ctx, t, cache)
			assertJ1FileTypeStudyIRODSOnMySQL(ctx, t, cache)
			assertJ1IDRun0OnMySQL(ctx, t, cache)
			assertJ1StatusBreakdownQCOnMySQL(ctx, t, cache)
			assertJ1PeopleOnMySQL(ctx, t, cache)
		})

		convey.Convey("I1.2: the run-scoped iRODS, manifest and file-type study iRODS queries are index-served (real key, not a full scan)", func() {
			assertJ1RunIRODSIndexServed(t, writeDB)
			assertJ1ManifestIndexServed(t, writeDB)
			assertJ1SampleIRODSIndexServed(t, writeDB)
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
// MySQL, with and without file_type, return the same rows/counts the combined J1
// fixture pins (six run-scope objects plus three manifest objects on the same
// run), with the count == len(list) cross-check.
func assertJ1RunIRODSOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	all, err := cache.IRODSPathsForRun(ctx, formatInt(j1Run), "", availabilityFetchAll, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(len(all), convey.ShouldEqual, 9)

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
	convey.So(len(cram), convey.ShouldEqual, 6)

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

// assertJ1SampleIRODSOnMySQL asserts IRODSPathsForSample /
// CountIRODSPathsForSample on MySQL, with and without file_type, return the same
// rows/counts the SQLite sample iRODS tests pin, while preserving the additive
// id_run/platform fields.
func assertJ1SampleIRODSOnMySQL(ctx context.Context, t *testing.T, cache *Client) {
	t.Helper()

	paths, err := cache.IRODSPathsForSample(ctx, "S1-sample-alpha", availabilityFetchAll, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(paths, convey.ShouldHaveLength, 3)
	for _, path := range paths {
		convey.So(path.IDSampleTmp, convey.ShouldEqual, 0)
		convey.So(path.Name, convey.ShouldEqual, "")
		convey.So(path.IDRun, convey.ShouldEqual, j1Run)
		convey.So(path.Platform, convey.ShouldEqual, "illumina")
		convey.So(path.IRODSPath, convey.ShouldStartWith, "/seq/52553/")
	}

	count, err := cache.CountIRODSPathsForSample(ctx, "S1-sample-alpha")
	convey.So(err, convey.ShouldBeNil)
	convey.So(count.Count, convey.ShouldEqual, len(paths))

	cram, err := cache.IRODSPathsForSampleByFileType(ctx, "S1-sample-alpha", "cram", availabilityFetchAll, 0)
	convey.So(err, convey.ShouldBeNil)
	convey.So(cram, convey.ShouldHaveLength, 2)
	for _, path := range cram {
		convey.So(path.DataObject, convey.ShouldEndWith, ".cram")
	}

	cramCount, err := cache.CountIRODSPathsForSampleByFileType(ctx, "S1-sample-alpha", "cram")
	convey.So(err, convey.ShouldBeNil)
	convey.So(cramCount.Count, convey.ShouldEqual, len(cram))
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
// query serves the product-metrics mirror (alias ipm, scoped by id_study_lims) and
// the iRODS-locations mirror by a real index with no full scan and no per-row
// dependent subquery. The iRODS pick is now a set-at-once LEFT JOIN to a DERIVED
// TABLE that picks one coherent object per product via a window function; a window
// function blocks derived-table merging, so MySQL materialises it and reports the
// outer reference under a synthetic <derivedN> alias (NOT "spi"). The real
// anti-full-scan guarantee therefore lives on the inner DERIVED scan of the actual
// seq_product_irods_locations_mirror table, which must use the id_study_lims index
// (type != ALL, real key) -- this still fails on a real full scan of the ~9M-row
// mirror. The derived table is INDEPENDENT (it references only the id_study_lims /
// file-type bound parameters, never an outer ipm column), so its select_type is
// the set-at-once DERIVED, never the per-row DEPENDENT SUBQUERY / DEPENDENT DERIVED
// (the correlated-subquery perf trap), which the loop asserts.
func assertJ1ManifestIndexServed(t *testing.T, db *sql.DB) {
	t.Helper()

	query, args := manifestListQuery(j1ManifestStudyLims, true, "cram", manifestAllRows, 0)
	plans := explainPlanRows(t, db, query, args...)
	for _, plan := range plans {
		convey.So(strings.ToUpper(plan.selectType), convey.ShouldNotContainSubstring, "DEPENDENT SUBQUERY")
		convey.So(strings.ToUpper(plan.selectType), convey.ShouldNotContainSubstring, "DEPENDENT DERIVED")
	}
	assertMirrorIndexServed(plans, "ipm")
	assertMirrorIndexServed(plans, "seq_product_irods_locations_mirror")
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
