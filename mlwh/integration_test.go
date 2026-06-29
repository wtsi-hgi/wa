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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/smartystreets/goconvey/convey"
)

const (
	liveMLWHTestsEnv    = "WA_LIVE_MLWH_TESTS"
	liveMLWHSyncPerfEnv = "MLWH_SYNC_PERF_TEST"
	mlwhDSNEnv          = "WA_MLWH_DSN"
	mlwhPasswordEnv     = "WA_MLWH_PASSWORD"
)

func TestLiveMLWHSyncQueriesMatchDevelopmentSchema(t *testing.T) {
	ctx := context.Background()
	config, skipReason := loadLiveMLWHConfigForTest(t)
	if skipReason != "" {
		t.Skip(skipReason)
	}

	resolvedDSN, err := ResolveDSN(config.DSN, config.Password)
	if err != nil {
		t.Fatalf("ResolveDSN(): %v", err)
	}

	sourceDB, err := sql.Open("mysql", resolvedDSN)
	if err != nil {
		t.Fatalf("sql.Open(): %v", err)
	}
	t.Cleanup(func() { _ = sourceDB.Close() })

	err = sourceDB.PingContext(ctx)
	if err != nil {
		t.Fatalf("PingContext(): %v", err)
	}

	convey.Convey("Given live MLWH credentials from the development env path", t, func() {
		queries, queryErr := liveMLWHColdSyncPerfQueries()
		convey.So(queryErr, convey.ShouldBeNil)

		for _, query := range queries {
			err = explainLiveQuery(ctx, sourceDB, `EXPLAIN `+query.query, query.args...)
			convey.So(err, convey.ShouldBeNil)
		}

		err = explainLiveQuery(ctx, sourceDB, `EXPLAIN SELECT `+sampleSelectColumns+` FROM sample WHERE uuid_sample_lims = ? LIMIT 1`, "00000000-0000-0000-0000-000000000000")

		convey.Convey("when every production sync source query and the cold-cache sample query are explained against the live source, then they compile without schema errors", func() {
			convey.So(err, convey.ShouldBeNil)
		})
	})
}

func TestLiveMLWHConfigSkipsWithoutLiveGate(t *testing.T) {
	convey.Convey("Given live MLWH credentials but no live-test opt-in, when loading the live test config, then the test is skipped before MLWH is touched", t, func() {
		t.Setenv(liveMLWHTestsEnv, "")
		t.Setenv(mlwhDSNEnv, "mlwh_user@tcp(127.0.0.1:1)/mlwarehouse")
		t.Setenv(mlwhPasswordEnv, "secret")

		_, skipReason := loadLiveMLWHConfigForTest(t)

		convey.So(skipReason, convey.ShouldContainSubstring, liveMLWHTestsEnv)
	})
}

func TestLiveMLWHPerfConfigSkipsWithoutLiveGate(t *testing.T) {
	convey.Convey("Given cold-sync perf opts and credentials but no live-test opt-in, when loading the perf config, then the test is skipped before Sync can run", t, func() {
		t.Setenv(liveMLWHTestsEnv, "")
		t.Setenv(liveMLWHSyncPerfEnv, "1")
		t.Setenv(mlwhDSNEnv, "mlwh_user@tcp(127.0.0.1:1)/mlwarehouse")
		t.Setenv(mlwhPasswordEnv, "secret")

		_, skipReason := loadLiveMLWHPerfConfigForTest(t)

		convey.So(skipReason, convey.ShouldContainSubstring, liveMLWHTestsEnv)
	})
}

func TestLiveMLWHSyncPerTableColdSyncBudgetSkipsWithoutGate(t *testing.T) {
	t.Setenv(liveMLWHTestsEnv, "1")
	t.Setenv(liveMLWHSyncPerfEnv, "")
	t.Setenv(mlwhDSNEnv, "")
	t.Setenv(mlwhPasswordEnv, "")

	_, skipReason := loadLiveMLWHPerfConfigForTest(t)
	if skipReason != "MLWH_SYNC_PERF_TEST not set" {
		t.Fatalf("loadLiveMLWHPerfConfigForTest() skip = %q, want %q", skipReason, "MLWH_SYNC_PERF_TEST not set")
	}

	t.Skip(skipReason)
}

func TestLiveMLWHSyncPerTableColdSyncBudgetSkipsWithoutDSN(t *testing.T) {
	t.Setenv(liveMLWHTestsEnv, "1")
	t.Setenv(liveMLWHSyncPerfEnv, "1")
	t.Setenv(mlwhDSNEnv, "")
	t.Setenv(mlwhPasswordEnv, "")

	_, skipReason := loadLiveMLWHPerfConfigForTest(t)
	if skipReason != "WA_MLWH_DSN not set" {
		t.Fatalf("loadLiveMLWHPerfConfigForTest() skip = %q, want %q", skipReason, "WA_MLWH_DSN not set")
	}

	t.Skip(skipReason)
}

func TestLiveMLWHSyncPerTableColdSyncBudget(t *testing.T) {
	ctx := context.Background()
	config, skipReason := loadLiveMLWHPerfConfigForTest(t)
	if skipReason != "" {
		t.Skip(skipReason)
	}

	streamDB, err := openLiveMLWHSourceDBForTest(ctx, config.DSN, config.Password)
	if err != nil {
		t.Fatalf("openLiveMLWHSourceDBForTest(): %v", err)
	}
	t.Cleanup(func() { _ = streamDB.Close() })

	queries, err := liveMLWHColdSyncPerfQueries()
	if err != nil {
		t.Fatalf("liveMLWHColdSyncPerfQueries(): %v", err)
	}

	streamDurations := make(map[string]time.Duration, len(queries))
	streamRows := make(map[string]int, len(queries))
	for _, query := range queries {
		duration, rows, measureErr := measureLiveQueryStreamDuration(ctx, streamDB, query.query, query.args...)
		if measureErr != nil {
			t.Fatalf("measureLiveQueryStreamDuration(%s): %v", query.table, measureErr)
		}

		streamDurations[query.table] = duration
		streamRows[query.table] = rows
		t.Logf("stream %s rows=%d duration=%s", query.table, rows, duration)
	}

	client, err := Open(ctx, config)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	reports, err := client.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync(): %v", err)
	}
	// Sync fans out over every supported table; this perf test only budgets the
	// heavy streaming tables it explicitly probes, so it requires a report for
	// each probed table rather than an exact 1:1 with the full fan-out.
	if len(reports) < len(queries) {
		t.Fatalf("Sync() returned %d reports, want at least %d", len(reports), len(queries))
	}

	syncDurations := make(map[string]time.Duration, len(reports))
	for _, report := range reports {
		syncDurations[report.Table] = report.Duration
		t.Logf("sync %s inserted=%d updated=%d duration=%s", report.Table, report.Inserted, report.Updated, report.Duration)
	}

	for _, query := range queries {
		if _, ok := syncDurations[query.table]; !ok {
			t.Fatalf("Sync() returned no report for probed table %s", query.table)
		}
	}

	for _, query := range queries {
		streamDuration := streamDurations[query.table]
		syncDuration, ok := syncDurations[query.table]
		if !ok {
			t.Fatalf("Sync() missing report for table %s", query.table)
		}

		budget := 2 * streamDuration
		if syncDuration > budget {
			t.Fatalf("table %s exceeded cold-sync budget: sync=%s stream=%s budget=%s rows=%d", query.table, syncDuration, streamDuration, budget, streamRows[query.table])
		}
	}
}

func explainLiveQuery(ctx context.Context, db *sql.DB, query string, args ...any) error {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	return rows.Err()
}

type liveMLWHPerfQuery struct {
	table string
	query string
	args  []any
}

func liveMLWHColdSyncPerfQueries() ([]liveMLWHPerfQuery, error) {
	sampleQuery, sampleArgs, _, err := sampleSyncQuery(syncStateRecord{})
	if err != nil {
		return nil, err
	}

	studyQuery, studyArgs, err := studySyncQuery(syncStateRecord{})
	if err != nil {
		return nil, err
	}

	flowcellQuery, flowcellArgs, err := flowcellSyncQuery(syncStateRecord{})
	if err != nil {
		return nil, err
	}

	iseqProductMetricsQuery, iseqProductMetricsArgs, _, err := iseqProductMetricsSyncQuery(syncStateRecord{})
	if err != nil {
		return nil, err
	}

	seqProductIRODSLocationsQuery, seqProductIRODSLocationsArgs, _, err := seqProductIRODSLocationsSyncQuery(syncStateRecord{})
	if err != nil {
		return nil, err
	}

	return []liveMLWHPerfQuery{
		{table: syncTableSample, query: sampleQuery, args: sampleArgs},
		{table: syncTableStudy, query: studyQuery, args: studyArgs},
		{table: syncTableIseqFlowcell, query: flowcellQuery, args: flowcellArgs},
		{table: syncTableIseqProductMetrics, query: iseqProductMetricsQuery, args: iseqProductMetricsArgs},
		{table: syncTableSeqProductIRODSLocations, query: seqProductIRODSLocationsQuery, args: seqProductIRODSLocationsArgs},
	}, nil
}

func liveMLWHTestsSkipReason() string {
	if strings.TrimSpace(os.Getenv(liveMLWHTestsEnv)) == "1" {
		return ""
	}

	return fmt.Sprintf("%s not set; set %s=1 with %s/%s to run live MLWH integration tests", liveMLWHTestsEnv, liveMLWHTestsEnv, mlwhDSNEnv, mlwhPasswordEnv)
}

func measureLiveQueryStreamDuration(ctx context.Context, db *sql.DB, query string, args ...any) (time.Duration, int, error) {
	started := time.Now()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return 0, 0, err
	}

	count := 0
	rawValues := make([]sql.RawBytes, len(columns))
	dest := make([]any, len(columns))
	for i := range rawValues {
		dest[i] = &rawValues[i]
	}

	for rows.Next() {
		if err = rows.Scan(dest...); err != nil {
			return 0, count, err
		}

		count++
	}

	if err = rows.Err(); err != nil {
		return 0, count, err
	}

	return time.Since(started), count, nil
}

func loadLiveMLWHPerfConfigForTest(t *testing.T) (Config, string) {
	t.Helper()

	if skipReason := liveMLWHTestsSkipReason(); skipReason != "" {
		return Config{}, skipReason
	}

	if strings.TrimSpace(os.Getenv(liveMLWHSyncPerfEnv)) != "1" {
		return Config{}, fmt.Sprintf("%s not set", liveMLWHSyncPerfEnv)
	}

	dsn := strings.TrimSpace(os.Getenv(mlwhDSNEnv))
	if dsn == "" {
		return Config{}, fmt.Sprintf("%s not set", mlwhDSNEnv)
	}

	password := strings.TrimSpace(os.Getenv(mlwhPasswordEnv))
	if password == "" {
		return Config{}, fmt.Sprintf("%s not set", mlwhPasswordEnv)
	}

	return Config{
		DSN:      dsn,
		Password: password,
		Cache:    CacheConfig{Path: filepath.Join(t.TempDir(), "mlwh-sync-perf.sqlite")},
	}, ""
}

func openLiveMLWHSourceDBForTest(ctx context.Context, dsn string, password string) (*sql.DB, error) {
	resolvedDSN, err := ResolveDSN(dsn, password)
	if err != nil {
		return nil, err
	}

	sourceDB, err := sql.Open("mysql", resolvedDSN)
	if err != nil {
		return nil, err
	}

	if err = sourceDB.PingContext(ctx); err != nil {
		_ = sourceDB.Close()

		return nil, err
	}

	return sourceDB, nil
}

func loadLiveMLWHConfigForTest(t *testing.T) (Config, string) {
	t.Helper()

	if skipReason := liveMLWHTestsSkipReason(); skipReason != "" {
		return Config{}, skipReason
	}

	if dsn := strings.TrimSpace(os.Getenv(mlwhDSNEnv)); dsn != "" {
		return Config{
			DSN:      dsn,
			Password: strings.TrimSpace(os.Getenv(mlwhPasswordEnv)),
			Cache:    CacheConfig{Path: filepath.Join(t.TempDir(), "mlwh-live-cache.sqlite")},
		}, ""
	}

	repoRoot, err := findRepoRootForTest()
	if err != nil {
		return Config{}, "skipping live MLWH integration: could not locate repository root to load development env files"
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

	dsn := strings.TrimSpace(loaded[mlwhDSNEnv])
	if dsn == "" {
		return Config{}, "skipping live MLWH integration: WA_MLWH_DSN not set in environment and no development dotenv file with MLWH credentials was found"
	}

	return Config{
		DSN:      dsn,
		Password: strings.TrimSpace(loaded[mlwhPasswordEnv]),
		Cache:    CacheConfig{Path: filepath.Join(t.TempDir(), "mlwh-live-cache.sqlite")},
	}, ""
}

func findRepoRootForTest() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	current := wd
	for {
		if _, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}

		current = parent
	}
}
