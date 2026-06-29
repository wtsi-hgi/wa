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
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/smartystreets/goconvey/convey"
	modernsqlite "modernc.org/sqlite"
)

var sampleSyncSourceColumns = []string{
	"id_sample_tmp",
	"id_lims",
	"id_sample_lims",
	"uuid_sample_lims",
	"name",
	"sanger_sample_id",
	"supplier_name",
	"accession_number",
	"donor_id",
	"taxon_id",
	"common_name",
	"description",
	"last_updated",
}

var sampleColdSyncSourceQueryForTest = sampleColdSyncSourceQuery()

var sampleSyncSourceQueryForTest = sampleSyncSourceQuery()

var studySyncSourceColumns = []string{
	"id_study_tmp",
	"id_lims",
	"id_study_lims",
	"uuid_study_lims",
	"name",
	"accession_number",
	"study_title",
	"faculty_sponsor",
	"state",
	"data_release_strategy",
	"data_access_group",
	"programme",
	"reference_genome",
	"ethically_approved",
	"study_type",
	"contains_human_dna",
	"contaminated_human_dna",
	"study_visibility",
	"ega_dac_accession_number",
	"ega_policy_accession_number",
	"data_release_timing",
	"last_updated",
}

var studySyncSourceQueryForTest = `SELECT ` + studySelectColumns + `, last_updated FROM study WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_study_tmp`

var iseqProductMetricsSyncSourceColumns = []string{
	"id_iseq_product",
	"id_iseq_pr_metrics_tmp",
	"id_iseq_flowcell_tmp",
	"id_run",
	"position",
	"tag_index",
	"id_sample_tmp",
	"id_study_lims",
	"qc",
	"qc_lib",
	"qc_seq",
	"last_updated",
}

var seqProductIRODSLocationsSyncSourceColumns = []string{
	"id_seq_product_irods_locations_tmp",
	"id_product",
	"irods_root_collection",
	"irods_data_relative_path",
	"id_sample_tmp",
	"id_study_lims",
	"last_updated",
	"created",
	"seq_platform_name",
}

// newSyncTestSourceTables is the set of sync tables added by A4/A5 (everything in
// supportedSyncTables beyond the original five). A full-Sync test that seeds only
// the original five gets an empty source for these, so they sync zero rows rather
// than erroring on a missing plan.
var newSyncTestSourceTables = map[string]bool{
	syncTableIseqRunStatus:           true,
	syncTableIseqRunStatusDict:       true,
	syncTableOseqFlowcell:            true,
	syncTablePacBioRunWellMetrics:    true,
	syncTableEseqRun:                 true,
	syncTableEseqRunLaneMetrics:      true,
	syncTableUseqRunMetrics:          true,
	syncTableSeqOpsTrackingPerSample: true,
	syncTablePacBioProductMetrics:    true,
	syncTableEseqProductMetrics:      true,
	syncTableUseqProductMetrics:      true,
}

var (
	syncTestDriverOnce sync.Once
	syncTestDriverMu   sync.Mutex
	syncTestDriverSeq  int
	syncTestDrivers    = map[string]*syncTestDriverState{}
)

var (
	recordingSQLiteDriverOnce  sync.Once
	recordingSQLiteObserversMu sync.Mutex
	recordingSQLiteObservers   = map[string]*sqliteSyncSQLObserver{}
)

var (
	syncCountingSQLiteDriverOnce sync.Once
	syncCountingSQLiteCountersMu sync.Mutex
	syncCountingSQLiteCounters   = map[string]*syncCommitCounter{}
)

func withSampleSearchTokenReadPageSizeForTest(t *testing.T, size int) {
	t.Helper()

	original := sampleSearchTokenReadPageSize
	sampleSearchTokenReadPageSize = size
	t.Cleanup(func() {
		sampleSearchTokenReadPageSize = original
	})
}

func syncSelectedTablesForTest(ctx context.Context, client *Client, tables ...string) ([]SyncReport, error) {
	reports := make([]SyncReport, 0, len(tables))
	for _, table := range tables {
		report, err := client.syncTable(ctx, table)
		if err != nil {
			return nil, err
		}

		reports = append(reports, report)
	}

	return reports, nil
}

type syncStartRecord struct {
	At          time.Time
	GoroutineID int64
}

type syncTestQueryResult struct {
	rows            [][]driver.Value
	queryErr        error
	finalErr        error
	firstRowDelay   time.Duration
	blockBeforeRow2 <-chan struct{}
	afterFirstRow   func()
	startSink       func(syncStartRecord)
	querySink       func(string, []driver.NamedValue)
}

type syncTestSourcePlan struct {
	columns         []string
	rows            [][]driver.Value
	queryErr        error
	finalErr        error
	firstRowDelay   time.Duration
	blockBeforeRow2 <-chan struct{}
	afterFirstRow   func()
	startSink       func(syncStartRecord)
	querySink       func(string, []driver.NamedValue)
	queryResults    []syncTestQueryResult
}

type syncTestDriverState struct {
	mu         sync.Mutex
	plans      map[string]syncTestSourcePlan
	queryCount map[string]int
}

func TestClientSyncIseqProductMetricsPreservesNullQCAsPending(t *testing.T) {
	convey.Convey("A3.2: Given a source iseq_product_metrics row with NULL qc", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 14, 9, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows: [][]driver.Value{
					{"product-null-qc", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", nil, nil, nil, formatSyncTime(base)},
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)
		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)

		convey.Convey("when synced, then the mirror stores SQL NULL (not 0) and a downstream read maps it to pending", func() {
			qc := readIseqProductMetricsMirrorQCForTest(t, cache.DB(), "product-null-qc")
			convey.So(qc.Valid, convey.ShouldBeFalse)
			convey.So(qcString(qc), convey.ShouldEqual, "pending")
		})
	})
}

func readIseqProductMetricsMirrorQCForTest(t *testing.T, db *sql.DB, idIseqProduct string) sql.NullInt64 {
	t.Helper()

	var qc sql.NullInt64
	err := db.QueryRow(`SELECT qc FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, idIseqProduct).Scan(&qc)
	if err != nil {
		t.Fatalf("readIseqProductMetricsMirrorQCForTest(%q): %v", idIseqProduct, err)
	}

	return qc
}

func TestClientSyncIseqProductMetricsMapsQCOneZeroNullToPassFailPending(t *testing.T) {
	convey.Convey("A3.3: Given source iseq_product_metrics rows with qc of 1, 0 and NULL", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows: [][]driver.Value{
					{"product-qc-pass", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", int64(1), int64(1), int64(1), formatSyncTime(base)},
					{"product-qc-fail", int64(3002), int64(2001), int64(9001), int64(1), int64(2), int64(1), "5001", int64(0), int64(0), int64(0), formatSyncTime(base.Add(time.Minute))},
					{"product-qc-pending", int64(3003), int64(2001), int64(9001), int64(1), int64(3), int64(1), "5001", nil, nil, nil, formatSyncTime(base.Add(2 * time.Minute))},
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)
		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)

		convey.Convey("when synced, then they read back as pass, fail and pending respectively", func() {
			convey.So(qcString(readIseqProductMetricsMirrorQCForTest(t, cache.DB(), "product-qc-pass")), convey.ShouldEqual, "pass")
			convey.So(qcString(readIseqProductMetricsMirrorQCForTest(t, cache.DB(), "product-qc-fail")), convey.ShouldEqual, "fail")
			convey.So(qcString(readIseqProductMetricsMirrorQCForTest(t, cache.DB(), "product-qc-pending")), convey.ShouldEqual, "pending")
		})
	})
}

func TestFinalizeSampleSyncStateRebuildsLargeSQLiteSecondaryIndexes(t *testing.T) {
	convey.Convey("Given a completed large SQLite sample cold load with indexes still dropped", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		highWater := time.Date(2026, time.May, 13, 11, 0, 0, 0, time.UTC)
		cache := &sqliteCache{rwDB: db}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`PRAGMA busy_timeout = 5000`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM donor_samples`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT OR IGNORE INTO donor_samples(donor_id, id_sample_tmp) SELECT donor_id, id_sample_tmp FROM sample_mirror`)).WillReturnResult(sqlmock.NewResult(0, 10296551))
		// The token index is rebuilt with index-added-after discipline: drop the
		// covering index, clear the table, read sample_mirror in id-range pages to
		// tokenise (closing each page's result set before inserting it, so MySQL is
		// never asked to write while a SELECT result set is open), then recreate the
		// index. The first page read returns no rows here, terminating the paged
		// loop, so no further page read and no token INSERT is issued before the
		// index is recreated.
		mock.ExpectExec(regexp.QuoteMeta(`DROP INDEX IF EXISTS sample_search_token_idx`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM sample_search_token`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta(sampleSearchTokenPageQuery + strconv.Itoa(sampleSearchTokenReadPageSize))).
			WithArgs(int64(0)).
			WillReturnRows(sqlmock.NewRows([]string{"id_sample_tmp", "name", "supplier_name", "common_name", "donor_id"}))
		mock.ExpectExec(regexp.QuoteMeta(`CREATE INDEX IF NOT EXISTS sample_search_token_idx ON sample_search_token(token, id_sample_tmp)`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta(mirrorIndexInventoryQuery("sqlite", sampleMirrorIndexSet.Table))).WillReturnRows(sqlmock.NewRows([]string{"name"}))
		for _, index := range sampleMirrorSecondaryIndexes {
			mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON sample_mirror(%s)`, index.Name, index.Column))).
				WillReturnResult(sqlmock.NewResult(0, 0))
		}
		mock.ExpectExec(regexp.QuoteMeta(buildUpsertStatement("sqlite", "sync_state", syncStateColumns, []string{"table_name"}))).
			WithArgs(syncTableSample, formatSyncTime(highWater), sqlmock.AnyArg(), nil, 0).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err = finalizeSampleSyncState(context.Background(), cache, highWater, true)

		convey.So(err, convey.ShouldBeNil)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

type syncTestDriver struct{}

type syncTestConn struct{ state *syncTestDriverState }

type syncTestRows struct {
	columns []string
	plan    syncTestQueryResult
	index   int
	started bool
}

func TestClientSyncFiveTableParallelReports(t *testing.T) {
	convey.Convey("B1.1: Given deterministic rows for the original five tables, when Client.Sync runs, then it returns one report per supported table with the inserted counts for those five", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 11, 9, 0, 0, 0, time.UTC)
		t2 := t1.Add(time.Minute)

		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows: [][]driver.Value{
					{int64(1), "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", "supplier-a", "acc-a", "donor-a", int64(9606), "human", "desc-a", formatSyncTime(t1)},
					{int64(2), "SQSCP", "102", "sample-uuid-2", "sample-b", "sanger-b", "supplier-b", "acc-b", "donor-b", int64(9606), "human", "desc-b", formatSyncTime(t2)},
				},
			},
			syncTableStudy: {
				columns: studySyncSourceColumns,
				rows: [][]driver.Value{
					studyRowValues(10, "SQSCP", "5001", "study-uuid-1", "study-a", "acc-study-a", t1),
				},
			},
			syncTableIseqFlowcell: {
				columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:    [][]driver.Value{{"Standard", int64(1), "5001", formatSyncTime(t1)}},
			},
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows:    [][]driver.Value{{"product-1001", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", int64(1), int64(1), int64(1), formatSyncTime(t1)}},
			},
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows:    [][]driver.Value{{int64(7001), "product-1001", "/seq", "run/file.cram", int64(1), "5001", formatSyncTime(t2), formatSyncTime(t2), "illumina"}},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := client.Sync(context.Background())

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, len(supportedSyncTables))

		byTable := make(map[string]SyncReport, len(reports))
		for _, report := range reports {
			byTable[report.Table] = report
		}

		convey.So(byTable[syncTableSample].Inserted, convey.ShouldEqual, 2)
		convey.So(byTable[syncTableStudy].Inserted, convey.ShouldEqual, 1)
		convey.So(byTable[syncTableIseqFlowcell].Inserted, convey.ShouldEqual, 1)
		convey.So(byTable[syncTableIseqProductMetrics].Inserted, convey.ShouldEqual, 1)
		convey.So(byTable[syncTableSeqProductIRODSLocations].Inserted, convey.ShouldEqual, 1)

		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 1)
	})
}

func TestClientSyncSQLiteAdvisoryLockAllowsParallelTableWrites(t *testing.T) {
	convey.Convey("Given a file-backed SQLite cache and one source row per sync table", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 12, 9, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    [][]driver.Value{{int64(1), "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", "supplier-a", "acc-a", "donor-a", int64(9606), "human", "desc-a", formatSyncTime(t1)}},
			},
			syncTableStudy: {
				columns: studySyncSourceColumns,
				rows:    [][]driver.Value{studyRowValues(10, "SQSCP", "5001", "study-uuid-1", "study-a", "acc-study-a", t1)},
			},
			syncTableIseqFlowcell: {
				columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:    [][]driver.Value{{"Standard", int64(1), "5001", formatSyncTime(t1)}},
			},
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows:    [][]driver.Value{{"product-1001", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", int64(1), int64(1), int64(1), formatSyncTime(t1)}},
			},
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows:    [][]driver.Value{{int64(7001), "product-1001", "/seq", "run/file.cram", int64(1), "5001", formatSyncTime(t1), formatSyncTime(t1), "illumina"}},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		reports, err := client.Sync(context.Background())

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, len(supportedSyncTables))
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 1)
	})
}

func TestClientSyncReportsPerTableDuration(t *testing.T) {
	convey.Convey("E1 helper: Given a delayed sample source, when sync runs, then the returned report includes the per-table elapsed duration", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		delay := 25 * time.Millisecond
		now := formatSyncTime(time.Now().UTC())
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns:       sampleSyncSourceColumns,
				rows:          [][]driver.Value{{int64(1), "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", "supplier-a", "acc-a", "donor-a", int64(9606), "human", "desc-a", now}},
				firstRowDelay: delay,
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Table, convey.ShouldEqual, syncTableSample)
		convey.So(reports[0].Duration, convey.ShouldBeGreaterThanOrEqualTo, delay)
	})
}

func TestClientSyncFiveTableParallelJoinsErrors(t *testing.T) {
	convey.Convey("B1.2: Given one failing table source, when Client.Sync runs, then it returns a joined error and every other supported table still commits", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 11, 10, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    [][]driver.Value{{int64(1), "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", "supplier-a", "acc-a", "donor-a", int64(9606), "human", "desc-a", formatSyncTime(t1)}},
			},
			syncTableStudy: {
				queryErr: fmt.Errorf("forced study failure"),
			},
			syncTableIseqFlowcell: {
				columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:    [][]driver.Value{{"Standard", int64(1), "5001", formatSyncTime(t1)}},
			},
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows:    [][]driver.Value{{"product-1001", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", int64(1), int64(1), int64(1), formatSyncTime(t1)}},
			},
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows:    [][]driver.Value{{int64(7001), "product-1001", "/seq", "run/file.cram", int64(1), "5001", formatSyncTime(t1), formatSyncTime(t1), "illumina"}},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := client.Sync(context.Background())

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, syncTableStudy)
		convey.So(err.Error(), convey.ShouldContainSubstring, "forced study failure")
		convey.So(reports, convey.ShouldHaveLength, len(supportedSyncTables)-1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 1)
	})
}

func TestClientSyncStartsEachTableWithinOverlapWindow(t *testing.T) {
	convey.Convey("B1.3: Given an instrumented source, when Client.Sync runs, then each table starts within 100ms and on a distinct goroutine", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		starts := struct {
			mu      sync.Mutex
			records map[string]syncStartRecord
		}{records: make(map[string]syncStartRecord, len(supportedSyncTables))}

		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: syncSingleRowSourcePlan(sampleSyncSourceColumns, []driver.Value{int64(1), "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", "supplier-a", "acc-a", "donor-a", int64(9606), "human", "desc-a", formatSyncTime(time.Now().UTC())}, func(record syncStartRecord) {
				starts.mu.Lock()
				starts.records[syncTableSample] = record
				starts.mu.Unlock()
			}),
			syncTableStudy: syncSingleRowSourcePlan(studySyncSourceColumns, studyRowValues(10, "SQSCP", "5001", "study-uuid-1", "study-a", "acc-study-a", time.Now().UTC()), func(record syncStartRecord) {
				starts.mu.Lock()
				starts.records[syncTableStudy] = record
				starts.mu.Unlock()
			}),
			syncTableIseqFlowcell: syncSingleRowSourcePlan([]string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"}, []driver.Value{"Standard", int64(1), "5001", formatSyncTime(time.Now().UTC())}, func(record syncStartRecord) {
				starts.mu.Lock()
				starts.records[syncTableIseqFlowcell] = record
				starts.mu.Unlock()
			}),
			syncTableIseqProductMetrics: syncSingleRowSourcePlan(iseqProductMetricsSyncSourceColumns, []driver.Value{"product-1001", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", int64(1), int64(1), int64(1), formatSyncTime(time.Now().UTC())}, func(record syncStartRecord) {
				starts.mu.Lock()
				starts.records[syncTableIseqProductMetrics] = record
				starts.mu.Unlock()
			}),
			syncTableSeqProductIRODSLocations: syncSingleRowSourcePlan(seqProductIRODSLocationsSyncSourceColumns, []driver.Value{int64(7001), "product-1001", "/seq", "run/file.cram", int64(1), "5001", formatSyncTime(time.Now().UTC()), formatSyncTime(time.Now().UTC()), "illumina"}, func(record syncStartRecord) {
				starts.mu.Lock()
				starts.records[syncTableSeqProductIRODSLocations] = record
				starts.mu.Unlock()
			}),
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := client.Sync(context.Background())

		convey.So(err, convey.ShouldBeNil)
		starts.mu.Lock()
		defer starts.mu.Unlock()
		convey.So(starts.records, convey.ShouldHaveLength, 5)

		goroutines := make(map[int64]struct{}, len(starts.records))
		for _, record := range starts.records {
			goroutines[record.GoroutineID] = struct{}{}
		}

		var first time.Time
		for _, record := range starts.records {
			if first.IsZero() || record.At.Before(first) {
				first = record.At
			}
		}

		for _, record := range starts.records {
			convey.So(record.At.Sub(first) <= 100*time.Millisecond, convey.ShouldBeTrue)
		}
		convey.So(goroutines, convey.ShouldHaveLength, 5)
	})
}

func TestClientSyncSampleColdLoadUsesBulkTransactions(t *testing.T) {
	convey.Convey("Given 2500 sample rows in a cold cache, when sync runs, then it avoids per-1000-row commits and reports 2500 inserts", t, func() {
		cache, commitCounter := openCountingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		rows := sampleSyncRowsForRange(1, 2500, time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC), nil)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    rows,
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		report, sawRows, err := client.syncTableData(context.Background(), syncTableSample, syncStateRecord{})

		convey.So(err, convey.ShouldBeNil)
		convey.So(sawRows, convey.ShouldBeTrue)
		convey.So(report.Table, convey.ShouldEqual, syncTableSample)
		convey.So(report.Inserted, convey.ShouldEqual, 2500)
		convey.So(report.Updated, convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2500)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 2500)
		convey.So(commitCounter.Count(), convey.ShouldEqual, 3)
	})
}

func TestClientSyncSampleColdLoadRebuildsDonorsSetWise(t *testing.T) {
	convey.Convey("Given a cold sample cache with rows that cannot already exist, when sync runs, then donor samples are rebuilt set-wise from sample_mirror", t, func() {
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 12, 12, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    sampleSyncRowsForRange(1, 2, base, nil),
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(err, convey.ShouldBeNil)
		sawSetWiseDonorRebuild := false
		for _, statement := range observer.Statements() {
			normalized := normalizeSQL(statement.Query)
			convey.So(normalized, convey.ShouldNotContainSubstring, "WHERE id_sample_tmp IN")
			if strings.Contains(normalized, "INTO donor_samples") && strings.Contains(normalized, "SELECT donor_id, id_sample_tmp FROM sample_mirror") {
				sawSetWiseDonorRebuild = true
			}
		}
		convey.So(sawSetWiseDonorRebuild, convey.ShouldBeTrue)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 2)
	})
}

func TestInsertSampleMirrorBatchSQLiteUsesPreparedStatement(t *testing.T) {
	convey.Convey("Given a cold SQLite sample batch", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		base := time.Date(2026, time.May, 12, 12, 30, 0, 0, time.UTC)
		rows := []sampleSyncRow{
			{Sample: Sample{IDSampleTmp: 1, IDLims: "SQSCP", IDSampleLims: "101", UUIDSampleLims: "uuid-1", Name: "sample-1", SangerSampleID: "sanger-1", SupplierName: "supplier-1", AccessionNumber: "acc-1", DonorID: "donor-1", TaxonID: 9606, CommonName: "human", Description: "desc-1"}, LastUpdated: base},
			{Sample: Sample{IDSampleTmp: 2, IDLims: "SQSCP", IDSampleLims: "102", UUIDSampleLims: "uuid-2", Name: "sample-2", SangerSampleID: "sanger-2", SupplierName: "supplier-2", AccessionNumber: "acc-2", DonorID: "donor-2", TaxonID: 9606, CommonName: "human", Description: "desc-2"}, LastUpdated: base.Add(time.Second)},
		}
		insert := buildBulkInsertStatement("sample_mirror", sampleMirrorColumns, 1)

		mock.ExpectBegin()
		prepared := mock.ExpectPrepare(regexp.QuoteMeta(insert))
		for _, row := range rows {
			prepared.ExpectExec().WithArgs(driverValuesForTest(sampleMirrorRowArgs(row))...).WillReturnResult(sqlmock.NewResult(1, 1))
		}
		mock.ExpectCommit()

		tx, err := db.BeginTx(context.Background(), nil)
		convey.So(err, convey.ShouldBeNil)
		convey.So(insertSampleMirrorBatch(context.Background(), tx, "sqlite", rows), convey.ShouldBeNil)
		convey.So(tx.Commit(), convey.ShouldBeNil)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestClientSyncSampleReplayCountsRowsAsUpdates(t *testing.T) {
	convey.Convey("B2.2: Given the same 2500 sample rows replayed, when sync runs again, then it reports 2500 updates and no inserts", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		rows := sampleSyncRowsForRange(1, 2500, time.Date(2026, time.May, 11, 12, 30, 0, 0, time.UTC), nil)

		firstSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    rows,
			},
		})
		defer func() { _ = firstSource.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: firstSource}

		firstReport, sawRows, err := client.syncTableData(context.Background(), syncTableSample, syncStateRecord{})
		convey.So(err, convey.ShouldBeNil)
		convey.So(sawRows, convey.ShouldBeTrue)
		convey.So(firstReport.Inserted, convey.ShouldEqual, 2500)

		secondSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    rows,
			},
		})
		defer func() { _ = secondSource.Close() }()

		client.syncSource = secondSource
		secondState, stateErr := readSyncStateFromDB(context.Background(), cache.DB(), syncTableSample)
		convey.So(stateErr, convey.ShouldBeNil)

		secondReport, sawRows, err := client.syncTableData(context.Background(), syncTableSample, secondState)

		convey.So(err, convey.ShouldBeNil)
		convey.So(sawRows, convey.ShouldBeTrue)
		convey.So(secondReport.Inserted, convey.ShouldEqual, 0)
		convey.So(secondReport.Updated, convey.ShouldEqual, 2500)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2500)
	})
}

func TestClientSyncSampleUpsertIsLastWriteWinsWithinBatch(t *testing.T) {
	convey.Convey("B2.3: Given duplicate sample keys within one batch, when sync runs, then the last row wins", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 13, 0, 0, 0, time.UTC)
		rows := sampleSyncRowsForRange(1, 250, base, nil)
		duplicate := sampleSyncRowValues(100, base.Add(100*time.Second), sampleSyncRowOverride{
			IDSampleLims:   "Y",
			UUIDSampleLims: "sample-uuid-100-y",
			Name:           "sample-100-y",
			DonorID:        "donor-100-y",
		})
		rows = append(rows[:100], append([][]driver.Value{duplicate}, rows[100:]...)...)

		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    rows,
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, sawRows, err := client.syncTableData(context.Background(), syncTableSample, syncStateRecord{})

		convey.So(err, convey.ShouldBeNil)
		convey.So(sawRows, convey.ShouldBeTrue)

		var idSampleLims string
		convey.So(cache.DB().QueryRow(`SELECT id_sample_lims FROM sample_mirror WHERE id_sample_tmp = ?`, 100).Scan(&idSampleLims), convey.ShouldBeNil)
		convey.So(idSampleLims, convey.ShouldEqual, "Y")
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 250)
	})
}

func TestClientSyncLibrarySamplesUpsertIsIdempotentAcrossDuplicateTriples(t *testing.T) {
	convey.Convey("B2.4: Given duplicate library_samples triples, when sync runs twice, then only one row is stored and the replay does not raise a unique error", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 13, 30, 0, 0, time.UTC)
		rows := [][]driver.Value{
			{"Standard", int64(101), "study-101", formatSyncTime(base)},
			{"Standard", int64(101), "study-101", formatSyncTime(base.Add(time.Second))},
		}

		firstSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqFlowcell: {
				columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:    rows,
			},
		})
		defer func() { _ = firstSource.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: firstSource}

		firstReport, sawRows, err := client.syncTableData(context.Background(), syncTableIseqFlowcell, syncStateRecord{})
		convey.So(err, convey.ShouldBeNil)
		convey.So(sawRows, convey.ShouldBeTrue)
		convey.So(firstReport.Inserted, convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples WHERE pipeline_id_lims = ? AND id_sample_tmp = ? AND id_study_lims = ?`, "Standard", 101, "study-101"), convey.ShouldEqual, 1)

		secondSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqFlowcell: {
				columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:    rows,
			},
		})
		defer func() { _ = secondSource.Close() }()

		client.syncSource = secondSource
		secondState, stateErr := readSyncStateFromDB(context.Background(), cache.DB(), syncTableIseqFlowcell)
		convey.So(stateErr, convey.ShouldBeNil)

		secondReport, sawRows, err := client.syncTableData(context.Background(), syncTableIseqFlowcell, secondState)

		convey.So(err, convey.ShouldBeNil)
		convey.So(sawRows, convey.ShouldBeTrue)
		convey.So(secondReport.Inserted, convey.ShouldEqual, 0)
		convey.So(secondReport.Updated, convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples WHERE pipeline_id_lims = ? AND id_sample_tmp = ? AND id_study_lims = ?`, "Standard", 101, "study-101"), convey.ShouldEqual, 1)
	})
}

func TestClientSyncConsumesRowsInStreamingMode(t *testing.T) {
	convey.Convey("B1.6: Given sources that block before row two, when Client.Sync runs, then each table consumes row one before the producer finishes even though the partial batch is not committed yet", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// Only the original five tables are seeded with blocking two-row sources
		// for this streaming probe; the new fan-out tables have empty sources and
		// emit no first row, so the wait targets just these streaming tables.
		streamingTables := []string{
			syncTableSample,
			syncTableStudy,
			syncTableIseqFlowcell,
			syncTableIseqProductMetrics,
			syncTableSeqProductIRODSLocations,
		}
		releaseRow2 := make(chan struct{})
		firstRowSeen := make(chan string, len(streamingTables))
		now := formatSyncTime(time.Now().UTC())

		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns:         sampleSyncSourceColumns,
				rows:            [][]driver.Value{{int64(1), "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", "supplier-a", "acc-a", "donor-a", int64(9606), "human", "desc-a", now}, {int64(2), "SQSCP", "102", "sample-uuid-2", "sample-b", "sanger-b", "supplier-b", "acc-b", "donor-b", int64(9606), "human", "desc-b", now}},
				blockBeforeRow2: releaseRow2,
				afterFirstRow:   func() { firstRowSeen <- syncTableSample },
			},
			syncTableStudy: {
				columns:         studySyncSourceColumns,
				rows:            [][]driver.Value{studyRowValues(10, "SQSCP", "5001", "study-uuid-1", "study-a", "acc-study-a", time.Now().UTC()), studyRowValues(11, "SQSCP", "5002", "study-uuid-2", "study-b", "acc-study-b", time.Now().UTC())},
				blockBeforeRow2: releaseRow2,
				afterFirstRow:   func() { firstRowSeen <- syncTableStudy },
			},
			syncTableIseqFlowcell: {
				columns:         []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:            [][]driver.Value{{"Standard", int64(1), "5001", now}, {"Standard", int64(2), "5002", now}},
				blockBeforeRow2: releaseRow2,
				afterFirstRow:   func() { firstRowSeen <- syncTableIseqFlowcell },
			},
			syncTableIseqProductMetrics: {
				columns:         iseqProductMetricsSyncSourceColumns,
				rows:            [][]driver.Value{{"product-1001", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", int64(1), int64(1), int64(1), now}, {"product-1002", int64(3002), int64(2002), int64(9002), int64(2), int64(1), int64(2), "5002", int64(1), int64(1), int64(1), now}},
				blockBeforeRow2: releaseRow2,
				afterFirstRow:   func() { firstRowSeen <- syncTableIseqProductMetrics },
			},
			syncTableSeqProductIRODSLocations: {
				columns:         seqProductIRODSLocationsSyncSourceColumns,
				rows:            [][]driver.Value{{int64(7001), "product-1001", "/seq", "run/file-1.cram", int64(1), "5001", now, now, "illumina"}, {int64(7002), "product-1002", "/seq", "run/file-2.cram", int64(2), "5002", now, now, "illumina"}},
				blockBeforeRow2: releaseRow2,
				afterFirstRow:   func() { firstRowSeen <- syncTableSeqProductIRODSLocations },
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		errCh := make(chan error, 1)
		go func() {
			_, err := client.Sync(context.Background())
			errCh <- err
		}()

		seen := make(map[string]struct{}, len(streamingTables))
		deadline := time.After(2 * time.Second)
		for len(seen) < len(streamingTables) {
			select {
			case table := <-firstRowSeen:
				seen[table] = struct{}{}
			case <-deadline:
				t.Fatal("expected each table to consume its first row before producer release")
			}
		}

		select {
		case err := <-errCh:
			convey.So(err, convey.ShouldBeNil)
			convey.So("sync completed before second rows were released", convey.ShouldEqual, "")
		case <-time.After(50 * time.Millisecond):
		}

		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 0)

		close(releaseRow2)
		convey.So(<-errCh, convey.ShouldBeNil)
	})
}

func TestResolveDSNForSyncForcesStreamingSafeOptions(t *testing.T) {
	convey.Convey("B1.6: Given a source DSN with non-streaming options enabled, when ResolveDSN runs, then it forces explicit streaming-safe driver options", t, func() {
		resolved, err := ResolveDSN(
			"mlwh_user@tcp(mlwh-db-ro:3435)/mlwarehouse?interpolateParams=true&multiStatements=true",
			"secret",
		)

		convey.So(err, convey.ShouldBeNil)
		convey.So(strings.Contains(resolved, "multiStatements=false"), convey.ShouldBeTrue)
		convey.So(strings.Contains(resolved, "interpolateParams=false"), convey.ShouldBeTrue)
		convey.So(strings.Contains(resolved, "interpolateParams=true"), convey.ShouldBeFalse)

		cfg, parseErr := mysql.ParseDSN(resolved)
		convey.So(parseErr, convey.ShouldBeNil)
		convey.So(cfg.Passwd, convey.ShouldEqual, "secret")
		convey.So(cfg.MultiStatements, convey.ShouldBeFalse)
		convey.So(cfg.InterpolateParams, convey.ShouldBeFalse)
	})
}

func TestResolveDSNTrimsTrailingEnvSemicolon(t *testing.T) {
	convey.Convey("Given a source DSN copied from a dotenv line with a trailing semicolon, when ResolveDSN runs, then the semicolon is not treated as part of the database name", t, func() {
		resolved, err := ResolveDSN("mlwh_user@tcp(mlwh-db-ro:3435)/mlwarehouse;?parseTime=true", "secret")

		convey.So(err, convey.ShouldBeNil)

		cfg, parseErr := mysql.ParseDSN(resolved)
		convey.So(parseErr, convey.ShouldBeNil)
		convey.So(cfg.DBName, convey.ShouldEqual, "mlwarehouse")
		convey.So(cfg.Passwd, convey.ShouldEqual, "secret")
		convey.So(cfg.ParseTime, convey.ShouldBeTrue)
	})
}

func TestClientSyncSampleColdCachePopulatesMirrorsAndWatermark(t *testing.T) {
	convey.Convey("Given a cold cache and three SQSCP sample rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		t1 := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)
		t3 := t2.Add(10 * time.Minute)

		sourceMock.ExpectQuery(regexp.QuoteMeta(sampleColdSyncSourceQueryForTest)).
			WithArgs(sampleColdInitialID).
			WillReturnRows(sqlmock.NewRows(sampleSyncSourceColumns).
				AddRow(1, "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", "supplier-a", "acc-a", "donor-a", 9606, "human", "desc-a", formatSyncTime(t3)).
				AddRow(2, "SQSCP", "102", "sample-uuid-2", "sample-b", "sanger-b", "supplier-b", "acc-b", "donor-b", 9606, "human", "desc-b", formatSyncTime(t1)).
				AddRow(3, "SQSCP", "103", "sample-uuid-3", "sample-c", "sanger-c", "supplier-c", "acc-c", "donor-c", 9606, "human", "desc-c", formatSyncTime(t2)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := syncSelectedTablesForTest(context.Background(), client, "sample")

		convey.Convey("when Sync runs, then it mirrors the rows, populates donor_samples and advances the watermark", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Table, convey.ShouldEqual, "sample")
			convey.So(reports[0].Inserted, convey.ShouldEqual, 3)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(reports[0].HighWater, convey.ShouldHappenOnOrBetween, t3, t3)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 3)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 3)
			convey.So(readSyncHighWater(t, cache.DB(), "sample"), convey.ShouldHappenOnOrBetween, t3, t3)

			var sampleName, supplierName, donorID string
			err = cache.DB().QueryRow(`SELECT name, supplier_name, donor_id FROM sample_mirror WHERE id_sample_tmp = ?`, 3).Scan(&sampleName, &supplierName, &donorID)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleName, convey.ShouldEqual, "sample-c")
			convey.So(supplierName, convey.ShouldEqual, "supplier-c")
			convey.So(donorID, convey.ShouldEqual, "donor-c")

			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncSampleColdCacheToleratesNullableTextFields(t *testing.T) {
	convey.Convey("Given a cold cache and a sample row with nullable text fields", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		timestamp := time.Date(2026, time.May, 10, 9, 30, 0, 0, time.UTC)

		sourceMock.ExpectQuery(regexp.QuoteMeta(sampleColdSyncSourceQueryForTest)).
			WithArgs(sampleColdInitialID).
			WillReturnRows(sqlmock.NewRows(sampleSyncSourceColumns).
				AddRow(1, "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", nil, "acc-a", "donor-a", 9606, "human", "desc-a", formatSyncTime(timestamp)).
				AddRow(2, "SQSCP", "102", "sample-uuid-2", "sample-b", "sanger-b", "supplier-b", nil, nil, nil, nil, nil, formatSyncTime(timestamp.Add(time.Minute))))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := syncSelectedTablesForTest(context.Background(), client, "sample")

		convey.Convey("when Sync runs, then it stores empty strings for NULL text columns instead of failing", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2)

			var supplierName, accessionNumber, donorID, commonName, description string
			var taxonID int
			err = cache.DB().QueryRow(
				`SELECT supplier_name, accession_number, donor_id, taxon_id, common_name, description FROM sample_mirror WHERE id_sample_tmp = ?`,
				2,
			).Scan(&supplierName, &accessionNumber, &donorID, &taxonID, &commonName, &description)
			convey.So(err, convey.ShouldBeNil)
			convey.So(supplierName, convey.ShouldEqual, "supplier-b")
			convey.So(accessionNumber, convey.ShouldEqual, "")
			convey.So(donorID, convey.ShouldEqual, "")
			convey.So(taxonID, convey.ShouldEqual, 0)
			convey.So(commonName, convey.ShouldEqual, "")
			convey.So(description, convey.ShouldEqual, "")
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncSampleWarmCacheUsesHighWaterFilter(t *testing.T) {
	convey.Convey("Given a warm sample cache with an existing high water mark", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t2 := time.Date(2026, time.May, 6, 10, 10, 0, 0, time.UTC)
		t3 := t2.Add(10 * time.Minute)
		seedSampleMirrorRow(t, cache.DB(), 2, "sample-b", "supplier-b", "donor-b", t2)
		seedDonorSampleRow(t, cache.DB(), "donor-b", 2, "study-b")
		seedSyncState(t, cache.DB(), "sample", t2)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(sampleSyncSourceQueryForTest)).
			WithArgs(formatSyncTime(t2)).
			WillReturnRows(sqlmock.NewRows(sampleSyncSourceColumns).
				AddRow(3, "SQSCP", "103", "sample-uuid-3", "sample-c", "sanger-c", "supplier-c", "acc-c", "donor-c", 9606, "human", "desc-c", formatSyncTime(t3)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := syncSelectedTablesForTest(context.Background(), client, "sample")

		convey.Convey("when Sync runs, then it queries from the saved high water and upserts only the new row", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 2)
			convey.So(readSyncHighWater(t, cache.DB(), "sample"), convey.ShouldHappenOnOrBetween, t3, t3)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncSampleRollbackLeavesMirrorAndWatermarkUnchanged(t *testing.T) {
	convey.Convey("Given a sync failure after one new sample row is written", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)
		t3 := t2.Add(10 * time.Minute)

		seedSampleMirrorRow(t, cache.DB(), 1, "sample-a", "supplier-a", "donor-a", t1)
		seedDonorSampleRow(t, cache.DB(), "donor-a", 1, "study-a")
		seedSyncState(t, cache.DB(), "sample", t1)

		_, err := cache.DB().Exec(`CREATE TRIGGER fail_second_donor_insert BEFORE INSERT ON donor_samples WHEN NEW.id_sample_tmp = 3 BEGIN SELECT RAISE(FAIL, 'forced donor insert failure'); END;`)
		convey.So(err, convey.ShouldBeNil)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(sampleSyncSourceQueryForTest)).
			WithArgs(formatSyncTime(t1)).
			WillReturnRows(sqlmock.NewRows(sampleSyncSourceColumns).
				AddRow(2, "SQSCP", "102", "sample-uuid-2", "sample-b", "sanger-b", "supplier-b", "acc-b", "donor-b", 9606, "human", "desc-b", formatSyncTime(t2)).
				AddRow(3, "SQSCP", "103", "sample-uuid-3", "sample-c", "sanger-c", "supplier-c", "acc-c", "donor-c", 9606, "human", "desc-c", formatSyncTime(t3)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		_, err = syncSelectedTablesForTest(context.Background(), client, "sample")

		convey.Convey("when the transaction rolls back, then the prior mirror rows and watermark remain unchanged", func() {
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror WHERE id_sample_tmp IN (2, 3)`), convey.ShouldEqual, 0)
			convey.So(readSyncHighWater(t, cache.DB(), "sample"), convey.ShouldHappenOnOrBetween, t1, t1)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncIseqFlowcellPopulatesDistinctLibrarySamples(t *testing.T) {
	convey.Convey("Given a cold cache and duplicate iseq_flowcell triples in the source", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 6, 11, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(flowcellSyncSourceQuery())).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows([]string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"}).
				AddRow("lib-a", 11, "study-a", formatSyncTime(t1)).
				AddRow("lib-a", 11, "study-a", formatSyncTime(t2)).
				AddRow("lib-b", 12, "study-b", formatSyncTime(t2)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := syncSelectedTablesForTest(context.Background(), client, "iseq_flowcell")

		convey.Convey("when Sync runs, then library_samples stores one row per distinct triple", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 2)

			rows, queryErr := cache.DB().Query(`SELECT pipeline_id_lims, id_sample_tmp, id_study_lims FROM library_samples ORDER BY pipeline_id_lims, id_sample_tmp, id_study_lims`)
			convey.So(queryErr, convey.ShouldBeNil)
			defer func() { _ = rows.Close() }()

			var triples []string
			for rows.Next() {
				var pipelineID, studyID string
				var sampleID int64
				convey.So(rows.Scan(&pipelineID, &sampleID, &studyID), convey.ShouldBeNil)
				triples = append(triples, pipelineID+":"+studyID+":"+formatInt(sampleID))
			}

			convey.So(rows.Err(), convey.ShouldBeNil)
			convey.So(triples, convey.ShouldResemble, []string{"lib-a:study-a:11", "lib-b:study-b:12"})
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncIseqFlowcellSkipsNullLibraryTypes(t *testing.T) {
	convey.Convey("Given a cold cache and an iseq_flowcell row without pipeline_id_lims", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 10, 11, 40, 0, 0, time.UTC)
		t2 := t1.Add(time.Minute)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(flowcellSyncSourceQuery())).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows([]string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"}).
				AddRow(nil, 11, "study-a", formatSyncTime(t1)).
				AddRow("lib-b", 12, "study-b", formatSyncTime(t2)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := syncSelectedTablesForTest(context.Background(), client, "iseq_flowcell")

		convey.Convey("when Sync runs, then it skips the NULL library row and still advances the watermark", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(reports[0].HighWater, convey.ShouldHappenOnOrBetween, t2, t2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 1)
			convey.So(readSyncHighWater(t, cache.DB(), "iseq_flowcell"), convey.ShouldHappenOnOrBetween, t2, t2)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncConstraintViolationNamesOffendingLibrarySampleRow(t *testing.T) {
	convey.Convey("B7.4: Given a forced sync batch row with an empty id_study_lims", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 11, 20, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    nil,
			},
			syncTableStudy: {
				columns: studySyncSourceColumns,
				rows:    nil,
			},
			syncTableIseqFlowcell: {
				columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:    [][]driver.Value{{"Standard", int64(31), "", formatSyncTime(t1)}},
			},
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows:    nil,
			},
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows:    nil,
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := client.Sync(context.Background())

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, syncTableIseqFlowcell)
		convey.So(err.Error(), convey.ShouldContainSubstring, "(Standard, 31)")
		convey.So(err.Error(), convey.ShouldContainSubstring, "id_study_lims")
		convey.So(reports, convey.ShouldHaveLength, len(supportedSyncTables))
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 0)
	})
}

func TestClientSyncStudyColdCachePopulatesMirrorAndWatermark(t *testing.T) {
	convey.Convey("Given a cold cache and two SQSCP study rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(studySyncSourceQueryForTest)).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows(studySyncSourceColumns).
				AddRow(studyRowValues(1, "SQSCP", "201", "study-uuid-1", "study-a", "acc-a", t1)...).
				AddRow(studyRowValues(2, "SQSCP", "202", "study-uuid-2", "study-b", "acc-b", t2)...))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := syncSelectedTablesForTest(context.Background(), client, "study")

		convey.Convey("when Sync runs, then it mirrors the study rows and stores the latest watermark", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 2)
			convey.So(readSyncHighWater(t, cache.DB(), "study"), convey.ShouldHappenOnOrBetween, t2, t2)

			var title, sponsor string
			err = cache.DB().QueryRow(`SELECT study_title, faculty_sponsor FROM study_mirror WHERE id_study_tmp = ?`, 2).Scan(&title, &sponsor)
			convey.So(err, convey.ShouldBeNil)
			convey.So(title, convey.ShouldEqual, "Study title 2")
			convey.So(sponsor, convey.ShouldEqual, "Faculty sponsor 2")

			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncStudySkipsNonSQSCPSourceRows(t *testing.T) {
	convey.Convey("Given a study source that returns a non-SQSCP row", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(studySyncSourceQueryForTest)).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows(studySyncSourceColumns).
				AddRow(studyRowValues(1, "SQSCP", "201", "study-uuid-1", "study-a", "acc-a", t1)...).
				AddRow(studyRowValues(2, "GCLP", "202", "study-uuid-2", "study-b", "acc-b", t2)...))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		_, err = syncSelectedTablesForTest(context.Background(), client, "study")

		convey.Convey("when Sync runs, then only SQSCP rows are written to study_mirror", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 1)

			var idLims string
			err = cache.DB().QueryRow(`SELECT id_lims FROM study_mirror LIMIT 1`).Scan(&idLims)
			convey.So(err, convey.ShouldBeNil)
			convey.So(idLims, convey.ShouldEqual, "SQSCP")
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncWritesSyncStateAfterCommit(t *testing.T) {
	convey.Convey("Given mocked cache and study source handles", t, func() {
		cacheDB, cacheMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = cacheDB.Close() }()

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		t1 := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)
		cursor := formatSyncTime(t2) + "\t2"
		bulkArgs := append(studyMirrorArgs(1, "SQSCP", "201", "study-uuid-1", "study-a", "acc-a", t1), studyMirrorArgs(2, "SQSCP", "202", "study-uuid-2", "study-b", "acc-b", t2)...)

		cacheMock.MatchExpectationsInOrder(true)
		cacheMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).WithArgs("study").WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}))
		cacheMock.ExpectBegin()
		cacheMock.ExpectExec(regexp.QuoteMeta(`PRAGMA busy_timeout = 5000`)).WillReturnResult(sqlmock.NewResult(0, 0))
		cacheMock.ExpectExec(`INSERT INTO study_mirror`).WithArgs(bulkArgs...).WillReturnResult(sqlmock.NewResult(1, 2))
		cacheMock.ExpectExec(`INSERT INTO sync_state`).WithArgs("study", formatSyncTime(t2), sqlmock.AnyArg(), cursor, 0).WillReturnResult(sqlmock.NewResult(1, 1))
		cacheMock.ExpectCommit()
		cacheMock.ExpectBegin()
		cacheMock.ExpectExec(regexp.QuoteMeta(`PRAGMA busy_timeout = 5000`)).WillReturnResult(sqlmock.NewResult(0, 0))
		cacheMock.ExpectExec(`INSERT INTO sync_state`).WithArgs("study", formatSyncTime(t2), sqlmock.AnyArg(), nil, 0).WillReturnResult(sqlmock.NewResult(1, 1))
		cacheMock.ExpectCommit()

		sourceMock.ExpectQuery(regexp.QuoteMeta(studySyncSourceQueryForTest)).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows(studySyncSourceColumns).
				AddRow(studyRowValues(1, "SQSCP", "201", "study-uuid-1", "study-a", "acc-a", t1)...).
				AddRow(studyRowValues(2, "SQSCP", "202", "study-uuid-2", "study-b", "acc-b", t2)...))

		client := &Client{cache: &sqliteCache{rwDB: cacheDB, roDB: cacheDB}, syncSource: sourceDB}

		_, err = syncSelectedTablesForTest(context.Background(), client, "study")

		convey.Convey("when Sync succeeds, then each batch writes sync_state in the same transaction and clears the cursor at end-of-stream", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(cacheMock.ExpectationsWereMet(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncSampleClearsResumeCursorAtEndOfStream(t *testing.T) {
	convey.Convey("B3.1: Given 1500 sample rows and a clean end-of-stream", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    sampleSyncRowsForRange(1, 1500, base, nil),
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 1500)
		convey.So(readSyncResumeCursor(t, cache.DB(), syncTableSample), convey.ShouldBeNil)
		convey.So(readSyncHighWater(t, cache.DB(), syncTableSample), convey.ShouldHappenOnOrBetween, base.Add(1499*time.Second), base.Add(1499*time.Second))
	})
}

func TestClientSyncSamplePersistsResumeCursorForLastCommittedBatch(t *testing.T) {
	convey.Convey("B3.2: Given 1500 sample rows followed by driver.ErrBadConn", t, func() {
		withSyncColdBatchSizeForTest(t, syncBatchSize)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 13, 0, 0, 123456789, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns:  sampleSyncSourceColumns,
				rows:     sampleSyncRowsForRange(1, 1500, base, nil),
				finalErr: driver.ErrBadConn,
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		expectedCursor := "id_sample_tmp_desc\t1000"
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, driver.ErrBadConn), convey.ShouldBeTrue)
		convey.So(readSyncResumeCursor(t, cache.DB(), syncTableSample), convey.ShouldEqual, expectedCursor)
		convey.So(readSyncHighWater(t, cache.DB(), syncTableSample), convey.ShouldHappenOnOrBetween, base.Add(999*time.Second), base.Add(999*time.Second))
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 1000)
	})
}

func TestClientSyncSampleColdLoadUsesIDSampleTmpKeysetQuery(t *testing.T) {
	convey.Convey("Given an empty sample cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		var capturedQuery string
		var capturedArgs []driver.NamedValue
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    nil,
				querySink: func(query string, args []driver.NamedValue) {
					capturedQuery = query
					capturedArgs = append([]driver.NamedValue(nil), args...)
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(capturedQuery, convey.ShouldContainSubstring, `id_sample_tmp < ?`)
		convey.So(capturedQuery, convey.ShouldContainSubstring, `ORDER BY id_sample_tmp DESC`)
		convey.So(capturedQuery, convey.ShouldNotContainSubstring, `last_updated >= ?`)
		convey.So(capturedQuery, convey.ShouldNotContainSubstring, `ORDER BY last_updated`)
		convey.So(namedValueOrdinals(capturedArgs), convey.ShouldResemble, []any{sampleColdInitialID})
	})
}

func TestClientSyncSampleColdLoadRestartsLegacyLastUpdatedCursorWithIDKeyset(t *testing.T) {
	convey.Convey("Given a cold sample sync state left behind with a legacy last_updated resume cursor", t, func() {
		withSyncColdBatchSizeForTest(t, syncBatchSize)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		legacyHighWater := time.Date(2021, time.December, 1, 12, 0, 0, 0, time.UTC)
		legacyCursor := formatSyncTime(legacyHighWater) + "\t9419243"
		convey.So(writeSyncState(context.Background(), cache.DB(), "sqlite", syncTableSample, legacyHighWater, &legacyCursor, false), convey.ShouldBeNil)

		var capturedQuery string
		var capturedArgs []driver.NamedValue
		base := time.Date(2020, time.January, 1, 9, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns:  sampleSyncSourceColumns,
				rows:     sampleSyncRowsForRange(1, 1000, base, nil),
				finalErr: driver.ErrBadConn,
				querySink: func(query string, args []driver.NamedValue) {
					capturedQuery = query
					capturedArgs = append([]driver.NamedValue(nil), args...)
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, driver.ErrBadConn), convey.ShouldBeTrue)
		convey.So(capturedQuery, convey.ShouldContainSubstring, `id_sample_tmp < ?`)
		convey.So(capturedQuery, convey.ShouldContainSubstring, `ORDER BY id_sample_tmp DESC`)
		convey.So(capturedQuery, convey.ShouldNotContainSubstring, `ORDER BY last_updated`)
		convey.So(namedValueOrdinals(capturedArgs), convey.ShouldResemble, []any{sampleColdInitialID})
		convey.So(readSyncResumeCursor(t, cache.DB(), syncTableSample), convey.ShouldEqual, "id_sample_tmp_desc\t1000")
		convey.So(readSyncHighWater(t, cache.DB(), syncTableSample), convey.ShouldHappenOnOrBetween, legacyHighWater, legacyHighWater)
	})
}

func TestSampleSyncCompletedSparseIndexStateUsesIncrementalHighWater(t *testing.T) {
	convey.Convey("Given a completed sparse sample cache with indexes_dropped still recorded", t, func() {
		highWater := time.Date(2026, time.May, 13, 10, 37, 12, 0, time.UTC)
		query, args, mode, err := sampleSyncQuery(syncStateRecord{HighWater: highWater, Exists: true, IndexesDropped: true})

		convey.So(err, convey.ShouldBeNil)
		convey.So(mode, convey.ShouldEqual, sampleSyncModeIncremental)
		convey.So(query, convey.ShouldEqual, sampleSyncSourceQuery())
		convey.So(args, convey.ShouldResemble, []any{formatSyncTime(highWater)})
	})
}

func TestClientSyncSampleResumesFromStrictKeysetCursor(t *testing.T) {
	convey.Convey("B3.3: Given a saved sample resume cursor for row 1000", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 14, 0, 0, 987654321, time.UTC)
		seedRows := sampleSyncRowsForRange(1, 1000, base, nil)
		for _, row := range seedRows {
			seedSampleMirrorRow(
				t,
				cache.DB(),
				row[0].(int64),
				row[4].(string),
				row[6].(string),
				row[8].(string),
				mustParseSyncTime(t, row[12].(string)),
			)
			seedDonorSampleRow(t, cache.DB(), row[8].(string), row[0].(int64), "")
		}

		cursorTime := base.Add(999 * time.Second)
		seedSyncStateWithCursor(t, cache.DB(), syncTableSample, cursorTime, "last_updated\t"+formatSyncTime(cursorTime)+"\t1000")

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		resumeQuery := sampleSyncSourceQueryFromCursor()
		sourceMock.ExpectQuery(regexp.QuoteMeta(resumeQuery)).
			WithArgs(formatSyncTime(cursorTime), formatSyncTime(cursorTime), int64(1000)).
			WillReturnRows(sampleRowsFromDriverValues(sampleSyncSourceColumns, sampleSyncRowsForRange(1001, 1500, base.Add(1000*time.Second), nil)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, runErr := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(runErr, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 500)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 1500)
		convey.So(readSyncResumeCursor(t, cache.DB(), syncTableSample), convey.ShouldBeNil)
		convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestClientSyncIseqFlowcellUsesFourColumnResumeCursor(t *testing.T) {
	convey.Convey("B3.4: Given 2000 iseq_flowcell rows followed by driver.ErrBadConn", t, func() {
		withSyncColdBatchSizeForTest(t, syncBatchSize)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 15, 0, 0, 222333444, time.UTC)
		rows := flowcellSyncRowsForRange(1, 2000, base)
		firstSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqFlowcell: {
				columns:  []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:     rows,
				finalErr: driver.ErrBadConn,
			},
		})
		defer func() { _ = firstSource.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: firstSource}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqFlowcell)

		expectedTime := base.Add(1999 * time.Second)
		expectedCursor := formatSyncTime(expectedTime) + "\tpipe-2000\t2000\tstudy-2000"
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, driver.ErrBadConn), convey.ShouldBeTrue)
		convey.So(readSyncResumeCursor(t, cache.DB(), syncTableIseqFlowcell), convey.ShouldEqual, expectedCursor)

		var capturedQuery string
		var capturedArgs []driver.NamedValue
		secondSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqFlowcell: {
				columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:    nil,
				querySink: func(query string, args []driver.NamedValue) {
					capturedQuery = query
					capturedArgs = append([]driver.NamedValue(nil), args...)
				},
			},
		})
		defer func() { _ = secondSource.Close() }()

		client.syncSource = secondSource

		reports, resumeErr := syncSelectedTablesForTest(context.Background(), client, syncTableIseqFlowcell)

		convey.So(resumeErr, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(capturedQuery, convey.ShouldContainSubstring, `(iseq_flowcell.last_updated > ?) OR (iseq_flowcell.last_updated = ? AND (iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims) > (?, ?, ?))`)
		convey.So(namedValueOrdinals(capturedArgs), convey.ShouldResemble, []any{
			formatSyncTime(expectedTime),
			formatSyncTime(expectedTime),
			"pipe-2000",
			int64(2000),
			"study-2000",
		})
	})
}

func TestClientSyncIseqProductMetricsKeepsProductKeyAndUsesMetricsTmpCursor(t *testing.T) {
	convey.Convey("Given 1000 iseq_product_metrics rows with distinct product keys and tmp ids followed by driver.ErrBadConn", t, func() {
		withSyncColdBatchSizeForTest(t, syncBatchSize)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 12, 10, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqProductMetrics: {
				columns:  iseqProductMetricsSyncSourceColumns,
				rows:     iseqProductMetricsSyncRowsForRange(1, 1000, base, 9000),
				finalErr: driver.ErrBadConn,
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, driver.ErrBadConn), convey.ShouldBeTrue)
		convey.So(readSyncResumeCursor(t, cache.DB(), syncTableIseqProductMetrics), convey.ShouldEqual, iseqProductMetricsIDResumeMode+"\t1000")
		convey.So(readSyncStateRow(t, cache.DB(), syncTableIseqProductMetrics).IndexesDropped, convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "product-10000"), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "1000"), convey.ShouldEqual, 0)
	})
}

func TestClientSyncSeqProductIRODSLocationsUsesLocationTmpCursor(t *testing.T) {
	convey.Convey("Given 1001 seq_product_irods_locations rows with distinct product keys and location tmp ids followed by driver.ErrBadConn", t, func() {
		withSyncColdBatchSizeForTest(t, syncBatchSize)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 12, 10, 30, 0, 0, time.UTC)
		firstSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSeqProductIRODSLocations: {
				columns:  seqProductIRODSLocationsSyncSourceColumns,
				rows:     seqProductIRODSLocationsSyncRowsForRange(1, 1001, base, 9000),
				finalErr: driver.ErrBadConn,
			},
		})
		defer func() { _ = firstSource.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: firstSource, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, driver.ErrBadConn), convey.ShouldBeTrue)
		convey.So(readSyncResumeCursor(t, cache.DB(), syncTableSeqProductIRODSLocations), convey.ShouldEqual, seqProductIRODSLocationsIDMode+"\t1000")
		convey.So(readSyncStateRow(t, cache.DB(), syncTableSeqProductIRODSLocations).IndexesDropped, convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "product-10000"), convey.ShouldEqual, 1)

		var capturedQuery string
		var capturedArgs []driver.NamedValue
		secondSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows:    nil,
				querySink: func(query string, args []driver.NamedValue) {
					capturedQuery = query
					capturedArgs = append([]driver.NamedValue(nil), args...)
				},
			},
		})
		defer func() { _ = secondSource.Close() }()

		client.syncSource = secondSource

		reports, resumeErr := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.So(resumeErr, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(capturedQuery, convey.ShouldContainSubstring, `spi.id_seq_product_irods_locations_tmp > ?`)
		convey.So(namedValueOrdinals(capturedArgs), convey.ShouldResemble, []any{int64(1000)})
	})
}

func TestClientSyncSeqProductIRODSLocationsIncrementalResumeReplacesExistingRows(t *testing.T) {
	convey.Convey("Given an interrupted incremental seq_product_irods_locations sync resumes for an existing product", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 12, 11, 0, 0, 0, time.UTC)
		seedSeqProductIRODSLocationsMirrorRow(t, cache.DB(), "product-loc-1", "/seq/old", "old.cram", base)
		seedSyncStateWithCursor(t, cache.DB(), syncTableSeqProductIRODSLocations, base, formatSyncTime(base)+"\t9000")

		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows: [][]driver.Value{
					{int64(9001), "product-loc-1", "/seq", "new/new.cram", int64(8001), "study-new", formatSyncTime(base.Add(time.Minute)), formatSyncTime(base.Add(time.Minute)), "illumina"},
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "product-loc-1"), convey.ShouldEqual, 1)
		convey.So(locationMirrorFileForTest(t, cache.DB(), "product-loc-1"), convey.ShouldEqual, "new.cram")
	})
}

func TestClientSyncSeqProductIRODSLocationsResumeKeepsExpandedSourceRowsAtomic(t *testing.T) {
	convey.Convey("Given expanded composite iRODS rows share a source location id across a cold batch boundary", t, func() {
		withSyncColdBatchSizeForTest(t, syncBatchSize)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC)
		firstRows := seqProductIRODSLocationsSyncRowsForRange(1, 999, base, 9000)
		firstRows = append(firstRows,
			[]driver.Value{int64(1000), "composite-product", "/seq/illumina/runs/48/48522", "plex1/48522#1.cram", int64(9419243), "7607", formatSyncTime(base.Add(999 * time.Second)), formatSyncTime(base.Add(999 * time.Second)), "illumina"},
			[]driver.Value{int64(1000), "composite-product", "/seq/illumina/runs/48/48522", "plex1/48522#1.cram", int64(9419244), "7607", formatSyncTime(base.Add(1000 * time.Second)), formatSyncTime(base.Add(1000 * time.Second)), "illumina"},
			[]driver.Value{int64(1001), "product-after-composite", "/seq", "run/after.cram", int64(9419245), "7607", formatSyncTime(base.Add(1001 * time.Second)), formatSyncTime(base.Add(1001 * time.Second)), "illumina"},
		)

		firstSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSeqProductIRODSLocations: {
				columns:  seqProductIRODSLocationsSyncSourceColumns,
				rows:     firstRows,
				finalErr: driver.ErrBadConn,
			},
		})
		defer func() { _ = firstSource.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: firstSource, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, driver.ErrBadConn), convey.ShouldBeTrue)
		convey.So(readSyncResumeCursor(t, cache.DB(), syncTableSeqProductIRODSLocations), convey.ShouldEqual, seqProductIRODSLocationsIDMode+"\t1000")
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "composite-product"), convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 1001)

		var capturedQuery string
		var capturedArgs []driver.NamedValue
		resumeSource := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows: [][]driver.Value{
					{int64(1001), "product-after-composite", "/seq", "run/after.cram", int64(9419245), "7607", formatSyncTime(base.Add(1001 * time.Second)), formatSyncTime(base.Add(1001 * time.Second)), "illumina"},
				},
				querySink: func(query string, args []driver.NamedValue) {
					capturedQuery = query
					capturedArgs = append([]driver.NamedValue(nil), args...)
				},
			},
		})
		defer func() { _ = resumeSource.Close() }()

		client.syncSource = resumeSource
		reports, resumeErr := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.So(resumeErr, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(capturedQuery, convey.ShouldContainSubstring, `spi.id_seq_product_irods_locations_tmp > ?`)
		convey.So(namedValueOrdinals(capturedArgs), convey.ShouldResemble, []any{int64(1000)})
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "composite-product"), convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "product-after-composite"), convey.ShouldEqual, 1)
		convey.So(readSyncResumeCursor(t, cache.DB(), syncTableSeqProductIRODSLocations), convey.ShouldBeNil)
	})
}

func TestClientSyncSampleColdLoadSetsIndexesDroppedBeforeFirstBatchSQLite(t *testing.T) {
	convey.Convey("B4.1: Given an empty SQLite cache and a cold sample sync, when the source blocks after row one, then sample_mirror indexes are dropped and sync_state records indexes_dropped=1 with zero high_water", t, func() {
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		releaseRow2 := make(chan struct{})
		firstRowSeen := make(chan struct{}, 1)
		base := time.Date(2026, time.May, 11, 16, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns:         sampleSyncSourceColumns,
				rows:            sampleSyncRowsForRange(1, 2, base, nil),
				blockBeforeRow2: releaseRow2,
				afterFirstRow:   func() { firstRowSeen <- struct{}{} },
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}
		errCh := make(chan error, 1)
		go func() {
			_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)
			errCh <- err
		}()

		select {
		case <-firstRowSeen:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for sample sync to block after the first row")
		}

		statements := filterRecordedStatements(observer.Statements(), func(statement recordedSQLStatement) bool {
			return !strings.HasPrefix(normalizeSQL(statement.Query), "PRAGMA ")
		})
		// Cold-load prep drops only the sample_mirror secondary indexes; the
		// sample_search_token covering index is dropped (and rebuilt) during the
		// finalize token build, not here, so the prepared sample maintenance is
		// no longer trigger-based.
		dropCount := len(sampleMirrorSecondaryIndexes)
		convey.So(statements, convey.ShouldHaveLength, dropCount+1)

		expectedDrops := make([]string, 0, dropCount)
		for _, index := range sampleMirrorSecondaryIndexes {
			expectedDrops = append(expectedDrops, normalizeSQL(`DROP INDEX IF EXISTS `+index.Name))
		}

		actualDrops := make([]string, 0, dropCount)
		for _, statement := range statements[:dropCount] {
			actualDrops = append(actualDrops, normalizeSQL(statement.Query))
		}

		convey.So(actualDrops, convey.ShouldResemble, expectedDrops)
		convey.So(normalizeSQL(statements[len(statements)-1].Query), convey.ShouldEqual, normalizeSQL(buildUpsertStatement("sqlite", "sync_state", syncStateColumns, []string{"table_name"})))
		convey.So(namedValueOrdinals(statements[len(statements)-1].Args[:1]), convey.ShouldResemble, []any{syncTableSample})
		convey.So(namedValueOrdinals(statements[len(statements)-1].Args[1:2]), convey.ShouldResemble, []any{formatSyncTime(time.Time{})})
		convey.So(namedValueOrdinals(statements[len(statements)-1].Args[3:4]), convey.ShouldResemble, []any{nil})
		convey.So(statements[len(statements)-1].Args[4].Value, convey.ShouldEqual, 1)
		convey.So(namedValueOrdinals(statements[len(statements)-1].Args[2:3])[0], convey.ShouldHaveSameTypeAs, "")
		convey.So(namedValueOrdinals(statements[len(statements)-1].Args[2:3])[0], convey.ShouldNotEqual, "")
		convey.So(observer.CommitCount(), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 0)
		convey.So(sampleMirrorIndexNames(t, cache.DB(), cache.Dialect()), convey.ShouldHaveLength, 0)
		state := readSyncStateRow(t, cache.DB(), syncTableSample)
		convey.So(state.IndexesDropped, convey.ShouldEqual, 1)
		convey.So(state.HighWater, convey.ShouldEqual, formatSyncTime(time.Time{}))

		close(releaseRow2)
		convey.So(<-errCh, convey.ShouldBeNil)
	})
}

func TestClientSyncSampleColdLoadRecreatesIndexesAfterFinalBatchSQLite(t *testing.T) {
	convey.Convey("B4.2: Given an empty SQLite cache and a cold sample sync, when sync completes, then the eight sample_mirror indexes are recreated and indexes_dropped is cleared", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 16, 30, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    sampleSyncRowsForRange(1, 2, base, nil),
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(sampleMirrorIndexNames(t, cache.DB(), cache.Dialect()), convey.ShouldResemble, sampleMirrorSecondaryIndexNames())
		convey.So(readSyncStateRow(t, cache.DB(), syncTableSample).IndexesDropped, convey.ShouldEqual, 0)
	})
}

func TestCreateSampleMirrorSecondaryIndexesMySQLUsesSingleAlter(t *testing.T) {
	convey.Convey("Given all sample_mirror secondary indexes are missing for a MySQL cache", t, func() {
		statement := buildMySQLCreateSampleMirrorSecondaryIndexesStatement(sampleMirrorSecondaryIndexes)

		convey.Convey("when the rebuild statement is built, then it adds every index in one ALTER TABLE", func() {
			convey.So(statement, convey.ShouldStartWith, "ALTER TABLE sample_mirror ")
			convey.So(strings.Count(statement, "ADD INDEX"), convey.ShouldEqual, len(sampleMirrorSecondaryIndexes))
			for _, index := range sampleMirrorSecondaryIndexes {
				convey.So(statement, convey.ShouldContainSubstring, fmt.Sprintf("ADD INDEX %s(%s)", index.Name, index.Column))
			}
		})
	})
}

func TestFinalizeSampleSyncStateDefersMySQLIndexRebuild(t *testing.T) {
	convey.Convey("Given a MySQL cache whose cold sample indexes are still dropped", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		highWater := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)
		cache := &mysqlCache{rwDB: db}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(buildUpsertStatement("mysql", "sync_state", syncStateColumns, []string{"table_name"}))).
			WithArgs(syncTableSample, formatSyncTime(highWater), sqlmock.AnyArg(), nil, 1).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err = finalizeSampleSyncState(context.Background(), cache, highWater, true)

		convey.So(err, convey.ShouldBeNil)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestPrepareProductMirrorIndexesForColdSyncMySQLDropsPrimaryKey(t *testing.T) {
	convey.Convey("Given a MySQL product mirror with primary and secondary indexes", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		cache := &mysqlCache{rwDB: db}
		state := syncStateRecord{}
		indexRows := sqlmock.NewRows([]string{"INDEX_NAME"})
		for _, index := range iseqProductMetricsMirrorSecondaryIndexes {
			indexRows.AddRow(index.Name)
		}

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(mirrorIndexInventoryQuery("mysql", iseqProductMetricsMirrorIndexSet.Table))).WillReturnRows(indexRows)
		for _, index := range iseqProductMetricsMirrorSecondaryIndexes {
			mock.ExpectExec(regexp.QuoteMeta(`DROP INDEX ` + index.Name + ` ON ` + iseqProductMetricsMirrorIndexSet.Table)).WillReturnResult(sqlmock.NewResult(0, 0))
		}
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = 'PRIMARY'`)).
			WithArgs(iseqProductMetricsMirrorIndexSet.Table).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE ` + iseqProductMetricsMirrorIndexSet.Table + ` DROP PRIMARY KEY`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(buildUpsertStatement("mysql", "sync_state", syncStateColumns, []string{"table_name"}))).
			WithArgs(syncTableIseqProductMetrics, formatSyncTime(time.Time{}), sqlmock.AnyArg(), nil, 1).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err = prepareMirrorIndexesForColdSync(context.Background(), cache, &state, iseqProductMetricsMirrorIndexSet)

		convey.So(err, convey.ShouldBeNil)
		convey.So(state.Exists, convey.ShouldBeTrue)
		convey.So(state.IndexesDropped, convey.ShouldBeTrue)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestRepairDroppedProductMirrorIndexesCreatesRunLookupIndexWithoutPrimaryKeyRebuild(t *testing.T) {
	convey.Convey("Given a completed large MySQL product mirror with only the sample/run read index present", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		highWater := time.Date(2026, time.May, 12, 15, 0, 0, 0, time.UTC)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqProductMetrics).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow(formatSyncTime(highWater), nil, 1))
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(mirrorIndexInventoryQuery("mysql", iseqProductMetricsMirrorIndexSet.Table))).
			WillReturnRows(sqlmock.NewRows([]string{"INDEX_NAME"}).AddRow("ipm_mirror_sample_run_position_tag_idx"))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE iseq_product_metrics_mirror ADD INDEX iseq_product_metrics_mirror_id_run_position_tag_index_idx(id_run, position, tag_index)`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		err = repairDroppedMirrorIndexSet(context.Background(), db, "mysql", iseqProductMetricsMirrorIndexSet)

		convey.So(err, convey.ShouldBeNil)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestRepairDroppedProductMirrorIndexesCreatesRunAndSampleLookupIndexes(t *testing.T) {
	convey.Convey("Given a completed large MySQL product mirror with resolver-critical indexes missing", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		highWater := time.Date(2026, time.May, 13, 9, 15, 0, 0, time.UTC)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqProductMetrics).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow(formatSyncTime(highWater), nil, 1))
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(mirrorIndexInventoryQuery("mysql", iseqProductMetricsMirrorIndexSet.Table))).
			WillReturnRows(sqlmock.NewRows([]string{"INDEX_NAME"}))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE iseq_product_metrics_mirror ADD INDEX iseq_product_metrics_mirror_id_run_position_tag_index_idx(id_run, position, tag_index), ADD INDEX ipm_mirror_sample_run_position_tag_idx(id_sample_tmp, id_run, position, tag_index)`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		err = repairDroppedMirrorIndexSet(context.Background(), db, "mysql", iseqProductMetricsMirrorIndexSet)

		convey.So(err, convey.ShouldBeNil)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestRepairDroppedProductMirrorIndexesDefersLargeSQLiteSecondaryRebuild(t *testing.T) {
	convey.Convey("Given a completed large SQLite product mirror with cold-load indexes still dropped", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		highWater := time.Date(2026, time.May, 13, 11, 5, 0, 0, time.UTC)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
			WithArgs(syncTableIseqProductMetrics).
			WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow(formatSyncTime(highWater), nil, 1))
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`PRAGMA busy_timeout = 5000`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM iseq_product_metrics_mirror`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(mysqlInlineMirrorIndexRowLimit + 1))
		mock.ExpectQuery(regexp.QuoteMeta(mirrorIndexInventoryQuery("sqlite", iseqProductMetricsMirrorIndexSet.Table))).WillReturnRows(sqlmock.NewRows([]string{"name"}))
		mock.ExpectExec(regexp.QuoteMeta(`CREATE INDEX IF NOT EXISTS iseq_product_metrics_mirror_id_run_position_tag_index_idx ON iseq_product_metrics_mirror(id_run, position, tag_index)`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`CREATE INDEX IF NOT EXISTS ipm_mirror_sample_run_position_tag_idx ON iseq_product_metrics_mirror(id_sample_tmp, id_run, position, tag_index)`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		err = repairDroppedMirrorIndexSet(context.Background(), db, "sqlite", iseqProductMetricsMirrorIndexSet)

		convey.So(err, convey.ShouldBeNil)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestClientSyncSparseMySQLProductMirrorsReplaceExistingKeys(t *testing.T) {
	convey.Convey("Given completed sparse MySQL product mirrors with duplicate existing keys", t, func() {
		cfg, skip := loadMySQLCacheConfigForTest(t)
		if skip != "" {
			t.Skip(skip)
		}

		cache := openMySQLCacheForTest(t, cfg)
		db := cache.DB()
		base := time.Date(2026, time.May, 12, 16, 0, 0, 0, time.UTC)
		next := base.Add(time.Minute)

		dropMirrorPrimaryKeyForTest(t, db, iseqProductMetricsMirrorIndexSet)
		dropMirrorPrimaryKeyForTest(t, db, seqProductIRODSLocationsMirrorIndexSet)
		seedSparseIseqProductMetricsMirrorRow(t, db, "product-1", 1001, 1, base)
		seedSparseIseqProductMetricsMirrorRow(t, db, "product-1", 1002, 2, base)
		seedSeqProductIRODSLocationsMirrorRow(t, db, "product-loc-1", "/seq/old", "old-1.cram", base)
		seedSeqProductIRODSLocationsMirrorRow(t, db, "product-loc-1", "/seq/old", "old-2.cram", base)
		convey.So(writeSyncState(context.Background(), db, cache.Dialect(), syncTableIseqProductMetrics, base, nil, true), convey.ShouldBeNil)
		convey.So(writeSyncState(context.Background(), db, cache.Dialect(), syncTableSeqProductIRODSLocations, base, nil, true), convey.ShouldBeNil)

		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows: [][]driver.Value{
					{"product-1", int64(5001), int64(6001), int64(7001), int64(3), int64(4), int64(8001), "study-new", int64(1), int64(0), int64(1), formatSyncTime(next)},
				},
			},
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows: [][]driver.Value{
					{int64(9001), "product-loc-1", "/seq", "new/new-1.cram", int64(8001), "study-new", formatSyncTime(next), formatSyncTime(next), "illumina"},
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldBeNil)
		convey.So(countRows(t, db, `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "product-1"), convey.ShouldEqual, 1)
		convey.So(countRows(t, db, `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "product-loc-1"), convey.ShouldEqual, 1)
		convey.So(productMirrorRunForTest(t, db, "product-1"), convey.ShouldEqual, 7001)
		convey.So(locationMirrorFileForTest(t, db, "product-loc-1"), convey.ShouldEqual, "new-1.cram")
		convey.So(readSyncStateRow(t, db, syncTableIseqProductMetrics).IndexesDropped, convey.ShouldEqual, 1)
		convey.So(readSyncStateRow(t, db, syncTableSeqProductIRODSLocations).IndexesDropped, convey.ShouldEqual, 1)
	})
}

func TestSparseMySQLIseqProductMetricsIncrementalBatchUsesDeleteThenInsert(t *testing.T) {
	convey.Convey("Given a sparse MySQL iseq_product_metrics mirror and an incremental row for an existing key", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		cache := &mysqlCache{rwDB: db}
		highWater := time.Date(2026, time.May, 12, 16, 30, 0, 0, time.UTC)
		row := iseqProductMetricsSyncRow{IDIseqProduct: "product-1", SourceRowID: 5001, IDIseqFlowcellTmp: 6001, IDRun: 7001, Position: 3, TagIndex: 4, IDSampleTmp: 8001, IDStudyLims: "study-new", QC: sql.NullInt64{Int64: 1, Valid: true}, QCLib: sql.NullInt64{Int64: 0, Valid: true}, QCSeq: sql.NullInt64{Int64: 1, Valid: true}, LastUpdated: highWater}

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM (SELECT 1 FROM iseq_product_metrics_mirror WHERE id_iseq_product IN (?) GROUP BY id_iseq_product) AS existing_keys`)).
			WithArgs("product-1").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM iseq_product_metrics_mirror WHERE id_iseq_product IN (?)`)).
			WithArgs("product-1").
			WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectExec(regexp.QuoteMeta(buildBulkInsertStatement("iseq_product_metrics_mirror", iseqProductMetricsMirrorColumns, 1))).
			WithArgs(driverValuesForTest(iseqProductMetricsMirrorBatchArgs([]iseqProductMetricsSyncRow{row}))...).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(buildUpsertStatement("mysql", "sync_state", syncStateColumns, []string{"table_name"}))).
			WithArgs(syncTableIseqProductMetrics, formatSyncTime(highWater), sqlmock.AnyArg(), nil, 1).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		result, err := writeIseqProductMetricsBatch(context.Background(), cache, []iseqProductMetricsSyncRow{row}, highWater, nil, true, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Inserted, convey.ShouldEqual, 0)
		convey.So(result.Updated, convey.ShouldEqual, 1)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestSparseMySQLSeqProductIRODSLocationsIncrementalBatchUsesDeleteThenInsert(t *testing.T) {
	convey.Convey("Given a sparse MySQL seq_product_irods_locations mirror and an incremental row for an existing key", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		cache := &mysqlCache{rwDB: db}
		highWater := time.Date(2026, time.May, 12, 16, 35, 0, 0, time.UTC)
		row := seqProductIRODSLocationsSyncRow{SourceRowID: 9001, IDIseqProduct: "product-loc-1", IRODSRootCollection: "/seq", IRODSDataRelativePath: "new/new-1.cram", IRODSCollection: "/seq/new", IRODSFileName: "new-1.cram", IDSampleTmp: 8001, IDStudyLims: "study-new", LastUpdated: highWater}
		insertStatement := strings.Replace(buildBulkInsertStatement("seq_product_irods_locations_mirror", seqProductIRODSLocationsMirrorColumns, 1), "INSERT INTO", "INSERT IGNORE INTO", 1)

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM (SELECT 1 FROM seq_product_irods_locations_mirror WHERE id_iseq_product IN (?) GROUP BY id_iseq_product) AS existing_keys`)).
			WithArgs("product-loc-1").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM seq_product_irods_locations_mirror WHERE id_iseq_product IN (?)`)).
			WithArgs("product-loc-1").
			WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectExec(regexp.QuoteMeta(insertStatement)).
			WithArgs(driverValuesForTest(seqProductIRODSLocationsMirrorBatchArgs([]seqProductIRODSLocationsSyncRow{row}))...).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(regexp.QuoteMeta(buildUpsertStatement("mysql", "sync_state", syncStateColumns, []string{"table_name"}))).
			WithArgs(syncTableSeqProductIRODSLocations, formatSyncTime(highWater), sqlmock.AnyArg(), nil, 1).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		result, err := writeSeqProductIRODSLocationsBatch(context.Background(), cache, []seqProductIRODSLocationsSyncRow{row}, highWater, nil, true, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Inserted, convey.ShouldEqual, 0)
		convey.So(result.Updated, convey.ShouldEqual, 1)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestReplaceIseqProductMetricsMirrorBatchDeletesDuplicateSparseRows(t *testing.T) {
	convey.Convey("Given a sparse product metrics mirror with duplicate rows for a key", t, func() {
		db := openSparseProductMirrorReplacementDBForTest(t)
		base := time.Date(2026, time.May, 12, 16, 40, 0, 0, time.UTC)
		next := base.Add(time.Minute)
		seedSparseIseqProductMetricsMirrorRow(t, db, "product-1", 1001, 1, base)
		seedSparseIseqProductMetricsMirrorRow(t, db, "product-1", 1002, 2, base)
		row := iseqProductMetricsSyncRow{IDIseqProduct: "product-1", SourceRowID: 5001, IDIseqFlowcellTmp: 6001, IDRun: 7001, Position: 3, TagIndex: 4, IDSampleTmp: 8001, IDStudyLims: "study-new", QC: sql.NullInt64{Int64: 1, Valid: true}, QCLib: sql.NullInt64{Int64: 0, Valid: true}, QCSeq: sql.NullInt64{Int64: 1, Valid: true}, LastUpdated: next}

		tx, err := db.BeginTx(context.Background(), nil)
		convey.So(err, convey.ShouldBeNil)
		convey.So(replaceIseqProductMetricsMirrorBatch(context.Background(), tx, "sqlite", []iseqProductMetricsSyncRow{row}), convey.ShouldBeNil)
		convey.So(tx.Commit(), convey.ShouldBeNil)

		convey.So(countRows(t, db, `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "product-1"), convey.ShouldEqual, 1)
		convey.So(productMirrorRunForTest(t, db, "product-1"), convey.ShouldEqual, 7001)
	})
}

func TestReplaceSeqProductIRODSLocationsMirrorBatchDeletesDuplicateSparseRows(t *testing.T) {
	convey.Convey("Given a sparse product location mirror with duplicate rows for a key", t, func() {
		db := openSparseProductMirrorReplacementDBForTest(t)
		base := time.Date(2026, time.May, 12, 16, 45, 0, 0, time.UTC)
		next := base.Add(time.Minute)
		seedSeqProductIRODSLocationsMirrorRow(t, db, "product-loc-1", "/seq/old", "old-1.cram", base)
		seedSeqProductIRODSLocationsMirrorRow(t, db, "product-loc-1", "/seq/old", "old-2.cram", base)
		row := seqProductIRODSLocationsSyncRow{SourceRowID: 9001, IDIseqProduct: "product-loc-1", IRODSRootCollection: "/seq", IRODSDataRelativePath: "new/new-1.cram", IRODSCollection: "/seq/new", IRODSFileName: "new-1.cram", IDSampleTmp: 8001, IDStudyLims: "study-new", LastUpdated: next}

		tx, err := db.BeginTx(context.Background(), nil)
		convey.So(err, convey.ShouldBeNil)
		convey.So(replaceSeqProductIRODSLocationsMirrorBatch(context.Background(), tx, "sqlite", []seqProductIRODSLocationsSyncRow{row}), convey.ShouldBeNil)
		convey.So(tx.Commit(), convey.ShouldBeNil)

		convey.So(countRows(t, db, `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "product-loc-1"), convey.ShouldEqual, 1)
		convey.So(locationMirrorFileForTest(t, db, "product-loc-1"), convey.ShouldEqual, "new-1.cram")
	})
}

func TestIseqProductMetricsColdBatchUsesInsertOnly(t *testing.T) {
	convey.Convey("Given a cold iseq_product_metrics batch that cannot already exist", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()
		cache := &sqliteCache{rwDB: db}

		base := time.Date(2026, time.May, 12, 13, 0, 0, 0, time.UTC)
		rows := []iseqProductMetricsSyncRow{
			{IDIseqProduct: "product-1", SourceRowID: 1, IDIseqFlowcellTmp: 11, IDRun: 1001, Position: 1, TagIndex: 1, IDSampleTmp: 101, IDStudyLims: "5001", QC: sql.NullInt64{Int64: 1, Valid: true}, QCLib: sql.NullInt64{Int64: 1, Valid: true}, QCSeq: sql.NullInt64{Int64: 1, Valid: true}, LastUpdated: base},
			{IDIseqProduct: "product-2", SourceRowID: 2, IDIseqFlowcellTmp: 12, IDRun: 1001, Position: 1, TagIndex: 2, IDSampleTmp: 102, IDStudyLims: "5001", QC: sql.NullInt64{Int64: 1, Valid: true}, QCLib: sql.NullInt64{Int64: 1, Valid: true}, QCSeq: sql.NullInt64{Int64: 1, Valid: true}, LastUpdated: base.Add(time.Second)},
		}
		insert := buildBulkInsertStatement("iseq_product_metrics_mirror", iseqProductMetricsMirrorColumns, 1)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`PRAGMA busy_timeout = 5000`)).WillReturnResult(sqlmock.NewResult(0, 0))
		prepared := mock.ExpectPrepare(regexp.QuoteMeta(insert))
		for _, row := range rows {
			prepared.ExpectExec().WithArgs(driverValuesForTest(iseqProductMetricsMirrorRowArgs(row))...).WillReturnResult(sqlmock.NewResult(1, 1))
		}
		mock.ExpectExec(regexp.QuoteMeta(buildUpsertStatement("sqlite", "sync_state", syncStateColumns, []string{"table_name"}))).
			WithArgs(syncTableIseqProductMetrics, formatSyncTime(rows[len(rows)-1].LastUpdated), sqlmock.AnyArg(), nil, 0).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		result, err := writeIseqProductMetricsBatch(context.Background(), cache, rows, rows[len(rows)-1].LastUpdated, nil, false, true)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Inserted, convey.ShouldEqual, len(rows))
		convey.So(result.Updated, convey.ShouldEqual, 0)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestSeqProductIRODSLocationsColdBatchUsesInsertOnly(t *testing.T) {
	convey.Convey("Given a cold seq_product_irods_locations batch that cannot already exist", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()
		cache := &sqliteCache{rwDB: db}

		base := time.Date(2026, time.May, 12, 13, 10, 0, 0, time.UTC)
		rows := []seqProductIRODSLocationsSyncRow{
			{SourceRowID: 1, IDIseqProduct: "product-1", IRODSRootCollection: "/seq", IRODSDataRelativePath: "run/1.cram", IRODSCollection: "/seq/run", IRODSFileName: "1.cram", IDSampleTmp: 101, IDStudyLims: "5001", LastUpdated: base},
			{SourceRowID: 2, IDIseqProduct: "product-2", IRODSRootCollection: "/seq", IRODSDataRelativePath: "run/2.cram", IRODSCollection: "/seq/run", IRODSFileName: "2.cram", IDSampleTmp: 102, IDStudyLims: "5001", LastUpdated: base.Add(time.Second)},
		}
		insert := buildBulkInsertStatement("seq_product_irods_locations_mirror", seqProductIRODSLocationsMirrorColumns, 1)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`PRAGMA busy_timeout = 5000`)).WillReturnResult(sqlmock.NewResult(0, 0))
		prepared := mock.ExpectPrepare(regexp.QuoteMeta(insert))
		for _, row := range rows {
			prepared.ExpectExec().WithArgs(driverValuesForTest(seqProductIRODSLocationsMirrorRowArgs(row))...).WillReturnResult(sqlmock.NewResult(1, 1))
		}
		mock.ExpectExec(regexp.QuoteMeta(buildUpsertStatement("sqlite", "sync_state", syncStateColumns, []string{"table_name"}))).
			WithArgs(syncTableSeqProductIRODSLocations, formatSyncTime(rows[len(rows)-1].LastUpdated), sqlmock.AnyArg(), nil, 0).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		result, err := writeSeqProductIRODSLocationsBatch(context.Background(), cache, rows, rows[len(rows)-1].LastUpdated, nil, false, true)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Inserted, convey.ShouldEqual, len(rows))
		convey.So(result.Updated, convey.ShouldEqual, 0)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestClientSyncProductMirrorsColdLoadRebuildsSecondaryIndexes(t *testing.T) {
	convey.Convey("Given cold product mirror syncs", t, func() {
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 12, 13, 20, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows:    iseqProductMetricsSyncRowsForRange(1, 2, base, 9000),
			},
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows:    seqProductIRODSLocationsSyncRowsForRange(1, 2, base, 9000),
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldBeNil)
		for _, expected := range []struct {
			indexSet      syncMirrorIndexSet
			expectedNames []string
		}{
			{indexSet: iseqProductMetricsMirrorIndexSet, expectedNames: iseqProductMetricsMirrorSecondaryIndexNames()},
			{indexSet: seqProductIRODSLocationsMirrorIndexSet, expectedNames: seqProductIRODSLocationsMirrorSecondaryIndexNames()},
		} {
			droppedIndexes := make(map[string]struct{}, len(expected.indexSet.Indexes))
			createdIndexes := make(map[string]struct{}, len(expected.indexSet.Indexes))
			for _, statement := range observer.Statements() {
				normalized := normalizeSQL(statement.Query)
				for _, index := range expected.indexSet.Indexes {
					if strings.HasPrefix(normalized, "DROP INDEX IF EXISTS "+index.Name) {
						droppedIndexes[index.Name] = struct{}{}
					}
					if strings.HasPrefix(normalized, "CREATE INDEX IF NOT EXISTS "+index.Name+" ON "+expected.indexSet.Table+"(") {
						createdIndexes[index.Name] = struct{}{}
					}
				}
			}

			convey.So(droppedIndexes, convey.ShouldHaveLength, len(expected.indexSet.Indexes))
			convey.So(createdIndexes, convey.ShouldHaveLength, len(expected.indexSet.Indexes))
			convey.So(mirrorIndexNames(t, cache.DB(), cache.Dialect(), expected.indexSet), convey.ShouldResemble, expected.expectedNames)
		}
	})
}

func TestIseqProductMetricsColdSyncUsesDescendingSourceIDKeyset(t *testing.T) {
	convey.Convey("Given an empty iseq_product_metrics sync state", t, func() {
		query, args, coldIDSync, err := iseqProductMetricsSyncQuery(syncStateRecord{})

		convey.Convey("when the source query is built, then cold sync walks the source row id index instead of filesorting last_changed", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(coldIDSync, convey.ShouldBeTrue)
			convey.So(args, convey.ShouldResemble, []any{sampleColdInitialID})
			convey.So(query, convey.ShouldContainSubstring, "SELECT /*+ JOIN_FIXED_ORDER() */ ipm.id_iseq_product")
			convey.So(query, convey.ShouldContainSubstring, "ipm.id_iseq_pr_metrics_tmp < ?")
			convey.So(query, convey.ShouldContainSubstring, "ORDER BY ipm.id_iseq_pr_metrics_tmp DESC")
			convey.So(query, convey.ShouldNotContainSubstring, "ORDER BY ipm.last_changed")
		})
	})
}

func TestSeqProductIRODSLocationsColdSyncUsesSourceIDKeyset(t *testing.T) {
	convey.Convey("Given an empty seq_product_irods_locations sync state", t, func() {
		query, args, coldIDSync, err := seqProductIRODSLocationsSyncQuery(syncStateRecord{})

		convey.Convey("when the source query is built, then cold sync walks the source row id index instead of filesorting last_changed", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(coldIDSync, convey.ShouldBeTrue)
			convey.So(args, convey.ShouldResemble, []any{syncColdInitialAscendingID})
			convey.So(query, convey.ShouldContainSubstring, "spi.id_seq_product_irods_locations_tmp > ?")
			convey.So(query, convey.ShouldContainSubstring, "ORDER BY spi.id_seq_product_irods_locations_tmp")
			convey.So(query, convey.ShouldNotContainSubstring, "ORDER BY spi.last_changed")
		})
	})
}

func TestIseqProductMetricsColdIDSyncKeepsMaxHighWater(t *testing.T) {
	convey.Convey("Given cold product metrics rows ordered by source id but not by last_changed", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		later := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)
		earlier := later.Add(-time.Hour)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows: [][]driver.Value{
					{"product-1", int64(1), int64(11), int64(1001), int64(1), int64(1), int64(101), "5001", int64(1), int64(1), int64(1), formatSyncTime(later)},
					{"product-2", int64(2), int64(12), int64(1001), int64(1), int64(2), int64(102), "5001", int64(1), int64(1), int64(1), formatSyncTime(earlier)},
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldBeNil)
		convey.So(readSyncStateRow(t, cache.DB(), syncTableIseqProductMetrics).HighWater, convey.ShouldEqual, formatSyncTime(later))
	})
}

func TestClientSyncSampleIncrementalSyncKeepsIndexesInstalled(t *testing.T) {
	convey.Convey("B4.3: Given a warm sample cache with non-zero high_water, when incremental sync runs, then sample_mirror indexes are never dropped", t, func() {
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		warmHighWater := time.Date(2026, time.May, 11, 17, 0, 0, 0, time.UTC)
		seedSyncState(t, cache.DB(), syncTableSample, warmHighWater)
		observer.Reset()

		releaseRow2 := make(chan struct{})
		firstRowSeen := make(chan struct{}, 1)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns:         sampleSyncSourceColumns,
				rows:            sampleSyncRowsForRange(1, 2, warmHighWater.Add(time.Second), nil),
				blockBeforeRow2: releaseRow2,
				afterFirstRow:   func() { firstRowSeen <- struct{}{} },
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}
		errCh := make(chan error, 1)
		go func() {
			_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)
			errCh <- err
		}()

		select {
		case <-firstRowSeen:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for incremental sample sync to block after the first row")
		}

		convey.So(observer.Statements(), convey.ShouldHaveLength, 0)
		convey.So(observer.CommitCount(), convey.ShouldEqual, 0)
		convey.So(sampleMirrorIndexNames(t, cache.DB(), cache.Dialect()), convey.ShouldResemble, sampleMirrorSecondaryIndexNames())
		convey.So(readSyncStateRow(t, cache.DB(), syncTableSample).IndexesDropped, convey.ShouldEqual, 0)

		close(releaseRow2)
		convey.So(<-errCh, convey.ShouldBeNil)
	})
}

func TestClientSyncNonSampleTablesNeverSetIndexesDropped(t *testing.T) {
	convey.Convey("B4.5: Given cold syncs for the other four tables, when they complete, then indexes_dropped stays 0 for each table", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 17, 30, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableStudy: {
				columns: studySyncSourceColumns,
				rows:    [][]driver.Value{studyRowValues(10, "SQSCP", "5001", "study-uuid-1", "study-a", "acc-study-a", base)},
			},
			syncTableIseqFlowcell: {
				columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:    [][]driver.Value{{"Standard", int64(1), "5001", formatSyncTime(base.Add(time.Minute))}},
			},
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows:    [][]driver.Value{{"product-1001", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", int64(1), int64(1), int64(1), formatSyncTime(base.Add(2 * time.Minute))}},
			},
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows:    [][]driver.Value{{int64(7001), "product-1001", "/seq", "run/file.cram", int64(1), "5001", formatSyncTime(base.Add(3 * time.Minute)), formatSyncTime(base.Add(3 * time.Minute)), "illumina"}},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		_, err := syncSelectedTablesForTest(
			context.Background(),
			client,
			syncTableStudy,
			syncTableIseqFlowcell,
			syncTableIseqProductMetrics,
			syncTableSeqProductIRODSLocations,
		)

		convey.So(err, convey.ShouldBeNil)
		for _, table := range []string{syncTableStudy, syncTableIseqFlowcell, syncTableIseqProductMetrics, syncTableSeqProductIRODSLocations} {
			convey.So(readSyncStateRow(t, cache.DB(), table).IndexesDropped, convey.ShouldEqual, 0)
		}
	})
}

func TestClientSyncSampleColdLoadDropsAllSecondaryIndexesMySQL(t *testing.T) {
	convey.Convey("B4.6: Given a MySQL cache and a cold sample sync blocked after row one, then INFORMATION_SCHEMA reports zero sample_mirror secondary indexes mid-load", t, func() {
		cfg, skip := loadMySQLCacheConfigForTest(t)
		if skip != "" {
			t.Skip(skip)
		}

		cache := openMySQLCacheForTest(t, cfg)
		releaseRow2 := make(chan struct{})
		firstRowSeen := make(chan struct{}, 1)
		base := time.Date(2026, time.May, 11, 18, 0, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns:         sampleSyncSourceColumns,
				rows:            sampleSyncRowsForRange(1, 2, base, nil),
				blockBeforeRow2: releaseRow2,
				afterFirstRow:   func() { firstRowSeen <- struct{}{} },
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}
		errCh := make(chan error, 1)
		go func() {
			_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)
			errCh <- err
		}()

		select {
		case <-firstRowSeen:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for MySQL sample sync to block after the first row")
		}

		convey.So(sampleMirrorIndexColumns(t, cache.DB(), cache.Dialect()), convey.ShouldResemble, map[string][]string{})
		convey.So(readSyncStateRow(t, cache.DB(), syncTableSample).IndexesDropped, convey.ShouldEqual, 1)

		close(releaseRow2)
		convey.So(<-errCh, convey.ShouldBeNil)
	})
}

func TestClientSyncSampleColdLoadRecreatesIndexesMySQL(t *testing.T) {
	convey.Convey("B4.7: Given a MySQL cache and a cold sample sync, when sync completes, then exactly eight distinct secondary indexes exist on sample_mirror and indexes_dropped is 0", t, func() {
		cfg, skip := loadMySQLCacheConfigForTest(t)
		if skip != "" {
			t.Skip(skip)
		}

		cache := openMySQLCacheForTest(t, cfg)
		base := time.Date(2026, time.May, 11, 18, 30, 0, 0, time.UTC)
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				rows:    sampleSyncRowsForRange(1, 2, base, nil),
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(err, convey.ShouldBeNil)
		convey.So(sampleMirrorIndexColumns(t, cache.DB(), cache.Dialect()), convey.ShouldResemble, sampleMirrorSecondaryIndexColumns())
		convey.So(readSyncStateRow(t, cache.DB(), syncTableSample).IndexesDropped, convey.ShouldEqual, 0)
	})
}

func TestClientSyncSampleReconnectsAfterTransientFault(t *testing.T) {
	convey.Convey("B5.1: Given 1000 sample rows followed by invalid connection and then 500 more rows on reconnect, when sync runs, then it resumes from the persisted cursor and logs one retry line", t, func() {
		withSyncColdBatchSizeForTest(t, syncBatchSize)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 19, 0, 0, 0, time.UTC)
		var waits []time.Duration
		var stderr strings.Builder
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				queryResults: []syncTestQueryResult{
					{
						rows:     sampleSyncRowsForRange(1, 1000, base, nil),
						finalErr: fmt.Errorf("invalid connection"),
					},
					{
						rows: sampleSyncRowsForRange(1001, 1500, base.Add(1000*time.Second), nil),
					},
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true, syncRetryWriter: &stderr, syncRetrySleep: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		}}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 1500)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 1500)
		convey.So(nonEmptyLines(stderr.String()), convey.ShouldHaveLength, 1)
		convey.So(matchesPattern(nonEmptyLines(stderr.String())[0], `^mlwh sync: sample reconnecting attempt 1/5 after .*invalid connection.*: backoff 1s$`), convey.ShouldBeTrue)
		convey.So(waits, convey.ShouldResemble, []time.Duration{time.Second})
	})
}

func TestClientSyncSampleStopsAfterFiveReconnectAttempts(t *testing.T) {
	convey.Convey("B5.2: Given a sample source that fails with unexpected EOF on every attempt, when sync runs, then it logs five reconnect lines and returns an error naming sample", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		var waits []time.Duration
		var stderr strings.Builder
		results := make([]syncTestQueryResult, 0, 6)
		for range 6 {
			results = append(results, syncTestQueryResult{queryErr: io.ErrUnexpectedEOF})
		}
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns:      sampleSyncSourceColumns,
				queryResults: results,
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true, syncRetryWriter: &stderr, syncRetrySleep: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		}}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		lines := nonEmptyLines(stderr.String())
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, syncTableSample)
		convey.So(err.Error(), convey.ShouldContainSubstring, "unexpected EOF")
		convey.So(lines, convey.ShouldHaveLength, 5)
		for index, line := range lines {
			convey.So(matchesPattern(line, fmt.Sprintf(`^mlwh sync: sample reconnecting attempt %d/5 after .*unexpected EOF.*: backoff %s$`, index+1, []string{"1s", "2s", "4s", "8s", "16s"}[index])), convey.ShouldBeTrue)
		}
		convey.So(waits, convey.ShouldResemble, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second})
	})
}

func TestClientSyncSampleDoesNotRetryNonTransientSourceError(t *testing.T) {
	convey.Convey("B5.3: Given a sample source syntax error, when sync runs, then it fails without retry output", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		var waits []time.Duration
		var stderr strings.Builder
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns:      sampleSyncSourceColumns,
				queryResults: []syncTestQueryResult{{queryErr: fmt.Errorf("syntax error")}},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true, syncRetryWriter: &stderr, syncRetrySleep: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		}}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "syntax error")
		convey.So(nonEmptyLines(stderr.String()), convey.ShouldBeEmpty)
		convey.So(waits, convey.ShouldBeEmpty)
	})
}

func TestClientSyncSampleResetsReconnectBudgetAfterSuccessfulResume(t *testing.T) {
	convey.Convey("Given a sample sync with two separate transient faults split by a successful resumed batch, when sync runs, then each fault starts at attempt 1/5", t, func() {
		withSyncColdBatchSizeForTest(t, syncBatchSize)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 19, 20, 0, 0, time.UTC)
		var waits []time.Duration
		var stderr strings.Builder
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				queryResults: []syncTestQueryResult{
					{
						rows:     sampleSyncRowsForRange(1, 1000, base, nil),
						finalErr: fmt.Errorf("invalid connection"),
					},
					{
						rows:     sampleSyncRowsForRange(1001, 2000, base.Add(1000*time.Second), nil),
						finalErr: io.ErrUnexpectedEOF,
					},
					{
						rows: sampleSyncRowsForRange(2001, 2500, base.Add(2000*time.Second), nil),
					},
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true, syncRetryWriter: &stderr, syncRetrySleep: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		}}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)

		lines := nonEmptyLines(stderr.String())
		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 2500)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2500)
		convey.So(lines, convey.ShouldHaveLength, 2)
		convey.So(matchesPattern(lines[0], `^mlwh sync: sample reconnecting attempt 1/5 after .*invalid connection.*: backoff 1s$`), convey.ShouldBeTrue)
		convey.So(matchesPattern(lines[1], `^mlwh sync: sample reconnecting attempt 1/5 after .*unexpected EOF.*: backoff 1s$`), convey.ShouldBeTrue)
		convey.So(waits, convey.ShouldResemble, []time.Duration{time.Second, time.Second})
	})
}

func TestClientSyncTwoTablesReconnectIndependently(t *testing.T) {
	convey.Convey("B5.4: Given two tables that each fault once, when Client.Sync runs, then both reconnect once and the overall sync succeeds", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.May, 11, 19, 30, 0, 0, time.UTC)
		var waits []time.Duration
		var stderr strings.Builder
		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				queryResults: []syncTestQueryResult{
					{rows: sampleSyncRowsForRange(1, 1000, base, nil), finalErr: fmt.Errorf("invalid connection")},
					{rows: sampleSyncRowsForRange(1001, 1001, base.Add(1000*time.Second), nil)},
				},
			},
			syncTableStudy: {
				columns: studySyncSourceColumns,
				queryResults: []syncTestQueryResult{
					{rows: [][]driver.Value{studyRowValues(10, "SQSCP", "5001", "study-uuid-1", "study-a", "acc-study-a", base)}, finalErr: fmt.Errorf("unexpected EOF")},
					{rows: [][]driver.Value{studyRowValues(11, "SQSCP", "5002", "study-uuid-2", "study-b", "acc-study-b", base.Add(time.Second))}},
				},
			},
			syncTableIseqFlowcell: {
				columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
				rows:    [][]driver.Value{{"Standard", int64(1), "5001", formatSyncTime(base.Add(2 * time.Second))}},
			},
			syncTableIseqProductMetrics: {
				columns: iseqProductMetricsSyncSourceColumns,
				rows:    [][]driver.Value{{"product-1001", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", int64(1), int64(1), int64(1), formatSyncTime(base.Add(3 * time.Second))}},
			},
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows:    [][]driver.Value{{int64(7001), "product-1001", "/seq", "run/file.cram", int64(1), "5001", formatSyncTime(base.Add(4 * time.Second)), formatSyncTime(base.Add(4 * time.Second)), "illumina"}},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true, syncRetryWriter: &stderr, syncRetrySleep: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		}}

		reports, err := client.Sync(context.Background())

		lines := nonEmptyLines(stderr.String())
		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, len(supportedSyncTables))
		convey.So(lines, convey.ShouldHaveLength, 2)
		convey.So(strings.Join(lines, "\n"), convey.ShouldContainSubstring, "mlwh sync: sample reconnecting attempt 1/5")
		convey.So(strings.Join(lines, "\n"), convey.ShouldContainSubstring, "mlwh sync: study reconnecting attempt 1/5")
		convey.So(waits, convey.ShouldResemble, []time.Duration{time.Second, time.Second})
	})
}

func studyMirrorArgs(id int64, idLims, idStudyLims, uuidStudyLims, name, accession string, lastUpdated time.Time) []driver.Value {
	return studyRowValues(id, idLims, idStudyLims, uuidStudyLims, name, accession, lastUpdated)
}

func TestClientSyncSQLiteWritePragmasAppliedInSpecOrder(t *testing.T) {
	convey.Convey("B8.1: Given a SQLite cache, when sync starts, then the sync write connection applies the tuned pragmas in spec order", t, func() {
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				return nil
			},
		}

		_, err := client.Sync(context.Background())

		pragmaStatements := filterRecordedStatements(observer.Statements(), func(statement recordedSQLStatement) bool {
			return strings.HasPrefix(normalizeSQL(statement.Query), "PRAGMA ")
		})
		convey.So(err, convey.ShouldBeNil)
		convey.So(pragmaStatements, convey.ShouldNotBeEmpty)

		captured := make([]string, 0, len(pragmaStatements))
		for _, statement := range pragmaStatements {
			captured = append(captured, normalizeSQL(statement.Query))
		}

		convey.So(captured, convey.ShouldContain, normalizeSQL(`PRAGMA synchronous = OFF`))
		convey.So(captured, convey.ShouldContain, normalizeSQL(`PRAGMA cache_size = -200000`))
		convey.So(captured, convey.ShouldContain, normalizeSQL(`PRAGMA temp_store = MEMORY`))

		start := 0
		for index, statement := range captured {
			if statement == normalizeSQL(`PRAGMA synchronous = OFF`) {
				start = index
				break
			}
		}

		convey.So(captured[start:start+3], convey.ShouldResemble, []string{
			normalizeSQL(`PRAGMA synchronous = OFF`),
			normalizeSQL(`PRAGMA cache_size = -200000`),
			normalizeSQL(`PRAGMA temp_store = MEMORY`),
		})
	})
}

func TestClientSyncSQLiteWritePragmasRestoreOnError(t *testing.T) {
	convey.Convey("B8.2: Given a SQLite cache, when sync finishes with an error, then the sync write connection restores the pre-recorded pragma values", t, func() {
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		_, err := cache.DB().Exec(`PRAGMA synchronous = FULL`)
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`PRAGMA cache_size = -4096`)
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`PRAGMA temp_store = FILE`)
		convey.So(err, convey.ShouldBeNil)
		observer.Reset()

		forcedErr := errors.New("forced sync failure")
		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				return forcedErr
			},
		}

		_, err = client.Sync(context.Background())

		pragmaStatements := filterRecordedStatements(observer.Statements(), func(statement recordedSQLStatement) bool {
			return strings.HasPrefix(normalizeSQL(statement.Query), "PRAGMA ")
		})
		convey.So(err, convey.ShouldEqual, forcedErr)
		convey.So(pragmaStatements, convey.ShouldNotBeEmpty)

		captured := make([]string, 0, len(pragmaStatements))
		for _, statement := range pragmaStatements {
			captured = append(captured, normalizeSQL(statement.Query))
		}

		convey.So(captured[len(captured)-3:], convey.ShouldResemble, []string{
			normalizeSQL(`PRAGMA synchronous = 2`),
			normalizeSQL(`PRAGMA cache_size = -4096`),
			normalizeSQL(`PRAGMA temp_store = 1`),
		})
	})
}

func TestClientSyncSQLiteWritePragmasRestoreAfterCancellation(t *testing.T) {
	convey.Convey("B8.2: Given a canceled sync context, when sync exits, then the sync write connection still restores the pre-recorded pragma values", t, func() {
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		_, err := cache.DB().Exec(`PRAGMA synchronous = FULL`)
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`PRAGMA cache_size = -16384`)
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`PRAGMA temp_store = FILE`)
		convey.So(err, convey.ShouldBeNil)
		observer.Reset()

		ctx, cancel := context.WithCancel(context.Background())
		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				cancel()

				return ctx.Err()
			},
		}

		_, err = client.Sync(ctx)

		pragmaStatements := filterRecordedStatements(observer.Statements(), func(statement recordedSQLStatement) bool {
			return strings.HasPrefix(normalizeSQL(statement.Query), "PRAGMA ")
		})
		convey.So(err, convey.ShouldEqual, context.Canceled)
		convey.So(pragmaStatements, convey.ShouldNotBeEmpty)

		captured := make([]string, 0, len(pragmaStatements))
		for _, statement := range pragmaStatements {
			captured = append(captured, normalizeSQL(statement.Query))
		}

		convey.So(captured[len(captured)-3:], convey.ShouldResemble, []string{
			normalizeSQL(`PRAGMA synchronous = 2`),
			normalizeSQL(`PRAGMA cache_size = -16384`),
			normalizeSQL(`PRAGMA temp_store = 1`),
		})
	})
}

func TestClientSyncSQLiteWritePragmasRestoreOnSuccess(t *testing.T) {
	convey.Convey("B8.2: Given a SQLite cache, when sync finishes successfully, then the sync write connection restores the pre-recorded pragma values", t, func() {
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		_, err := cache.DB().Exec(`PRAGMA synchronous = FULL`)
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`PRAGMA cache_size = -8192`)
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`PRAGMA temp_store = FILE`)
		convey.So(err, convey.ShouldBeNil)
		observer.Reset()

		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				return nil
			},
		}

		_, err = client.Sync(context.Background())

		pragmaStatements := filterRecordedStatements(observer.Statements(), func(statement recordedSQLStatement) bool {
			return strings.HasPrefix(normalizeSQL(statement.Query), "PRAGMA ")
		})
		convey.So(err, convey.ShouldBeNil)
		convey.So(pragmaStatements, convey.ShouldNotBeEmpty)

		captured := make([]string, 0, len(pragmaStatements))
		for _, statement := range pragmaStatements {
			captured = append(captured, normalizeSQL(statement.Query))
		}

		convey.So(captured[len(captured)-3:], convey.ShouldResemble, []string{
			normalizeSQL(`PRAGMA synchronous = 2`),
			normalizeSQL(`PRAGMA cache_size = -8192`),
			normalizeSQL(`PRAGMA temp_store = 1`),
		})
	})
}

func TestClientSyncMySQLWritePragmasNoOp(t *testing.T) {
	convey.Convey("B8.3: Given a MySQL cache, when sync starts, then the sqlite pragma helper is a no-op", t, func() {
		rwDB, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = rwDB.Close() }()

		mock.MatchExpectationsInOrder(true)
		lockName := mysqlSyncLockName("")
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT GET_LOCK(?, 0)`)).
			WithArgs(lockName).
			WillReturnRows(sqlmock.NewRows([]string{"got_lock"}).AddRow(1))
		mock.ExpectBegin()
		mock.ExpectCommit()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT RELEASE_LOCK(?)`)).
			WithArgs(lockName).
			WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))

		client := &Client{
			cache: &mysqlCache{rwDB: rwDB, roDB: rwDB},
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				return nil
			},
		}

		_, err = client.Sync(context.Background())

		convey.So(err, convey.ShouldBeNil)
		convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

type syncStateRow struct {
	HighWater      string
	IndexesDropped int
}

func readSyncStateRow(t *testing.T, db *sql.DB, table string) syncStateRow {
	t.Helper()

	var state syncStateRow
	if err := db.QueryRow(`SELECT high_water, indexes_dropped FROM sync_state WHERE table_name = ?`, table).Scan(&state.HighWater, &state.IndexesDropped); err != nil {
		t.Fatalf("readSyncStateRow(%s): %v", table, err)
	}

	return state
}

func openSparseProductMirrorReplacementDBForTest(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open(sqlite): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	statements := []string{
		`CREATE TABLE iseq_product_metrics_mirror(id_iseq_product TEXT NOT NULL, id_iseq_flowcell_tmp INTEGER NOT NULL, id_run INTEGER NOT NULL, position INTEGER NOT NULL, tag_index INTEGER NOT NULL, id_sample_tmp INTEGER NOT NULL, id_study_lims TEXT NOT NULL, qc INTEGER NOT NULL, qc_lib INTEGER NOT NULL, qc_seq INTEGER NOT NULL, last_updated TEXT NOT NULL)`,
		`CREATE TABLE seq_product_irods_locations_mirror(id_iseq_product TEXT NOT NULL, irods_root_collection TEXT NOT NULL, irods_data_relative_path TEXT NOT NULL, irods_collection TEXT NOT NULL, irods_file_name TEXT NOT NULL, id_sample_tmp INTEGER NOT NULL, id_study_lims TEXT NOT NULL, last_updated TEXT NOT NULL, created TEXT NOT NULL, platform TEXT NOT NULL)`,
	}
	for _, statement := range statements {
		if _, err = db.Exec(statement); err != nil {
			t.Fatalf("create sparse replacement table: %v", err)
		}
	}

	return db
}

func dropMirrorPrimaryKeyForTest(t *testing.T, db *sql.DB, indexSet syncMirrorIndexSet) {
	t.Helper()

	_, err := db.Exec(`ALTER TABLE ` + indexSet.Table + ` DROP PRIMARY KEY`)
	if err != nil {
		t.Fatalf("dropMirrorPrimaryKeyForTest(%s): %v", indexSet.Table, err)
	}
}

func seedSparseIseqProductMetricsMirrorRow(t *testing.T, db *sql.DB, productID string, idRun, position int, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		productID,
		int64(2001),
		idRun,
		position,
		1,
		int64(3001),
		"study-old",
		1,
		1,
		1,
		formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedSparseIseqProductMetricsMirrorRow(): %v", err)
	}
}

func seedSeqProductIRODSLocationsMirrorRow(t *testing.T, db *sql.DB, productID, collection, fileName string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO seq_product_irods_locations_mirror(id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		productID,
		"/seq",
		fileName,
		collection,
		fileName,
		int64(3001),
		"study-old",
		formatSyncTime(lastUpdated),
		formatSyncTime(lastUpdated),
		"illumina",
	)
	if err != nil {
		t.Fatalf("seedSeqProductIRODSLocationsMirrorRow(): %v", err)
	}
}

func productMirrorRunForTest(t *testing.T, db *sql.DB, productID string) int {
	t.Helper()

	var idRun int
	if err := db.QueryRow(`SELECT id_run FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, productID).Scan(&idRun); err != nil {
		t.Fatalf("productMirrorRunForTest(%s): %v", productID, err)
	}

	return idRun
}

func locationMirrorFileForTest(t *testing.T, db *sql.DB, productID string) string {
	t.Helper()

	var fileName string
	if err := db.QueryRow(`SELECT irods_file_name FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, productID).Scan(&fileName); err != nil {
		t.Fatalf("locationMirrorFileForTest(%s): %v", productID, err)
	}

	return fileName
}

func driverValuesForTest(args []any) []driver.Value {
	values := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		values = append(values, arg)
	}

	return values
}

func sampleMirrorSecondaryIndexNames() []string {
	return []string{
		"sample_mirror_accession_number_idx",
		"sample_mirror_common_name_idx",
		"sample_mirror_donor_id_idx",
		"sample_mirror_id_sample_lims_idx",
		"sample_mirror_last_updated_idx",
		"sample_mirror_name_idx",
		"sample_mirror_sanger_sample_id_idx",
		"sample_mirror_supplier_name_idx",
		"sample_mirror_uuid_sample_lims_idx",
	}
}

func iseqProductMetricsMirrorSecondaryIndexNames() []string {
	return []string{
		"ipm_mirror_sample_run_position_tag_idx",
		"iseq_product_metrics_mirror_id_iseq_flowcell_tmp_idx",
		"iseq_product_metrics_mirror_id_run_position_tag_index_idx",
		"iseq_product_metrics_mirror_id_study_lims_id_run_position_idx",
	}
}

func seqProductIRODSLocationsMirrorSecondaryIndexNames() []string {
	// The cold load drops and recreates the mirror's rebuild index set, but the
	// table also carries the A1 (id_study_lims, created) recency index, which is
	// never dropped; mirrorIndexNames reads every physical index, sorted.
	return []string{
		"seq_product_irods_locations_mirror_id_sample_tmp_idx",
		"spi_mirror_study_lims_created_idx",
		"spi_mirror_study_lims_sample_tmp_idx",
	}
}

func sampleMirrorSecondaryIndexColumns() map[string][]string {
	return map[string][]string{
		"sample_mirror_accession_number_idx": {"accession_number"},
		"sample_mirror_common_name_idx":      {"common_name"},
		"sample_mirror_donor_id_idx":         {"donor_id"},
		"sample_mirror_id_sample_lims_idx":   {"id_sample_lims"},
		"sample_mirror_last_updated_idx":     {"last_updated"},
		"sample_mirror_name_idx":             {"name"},
		"sample_mirror_sanger_sample_id_idx": {"sanger_sample_id"},
		"sample_mirror_supplier_name_idx":    {"supplier_name"},
		"sample_mirror_uuid_sample_lims_idx": {"uuid_sample_lims"},
	}
}

func sampleMirrorIndexNames(t *testing.T, db *sql.DB, dialect string) []string {
	t.Helper()

	return mirrorIndexNames(t, db, dialect, sampleMirrorIndexSet)
}

func mirrorIndexNames(t *testing.T, db *sql.DB, dialect string, indexSet syncMirrorIndexSet) []string {
	t.Helper()

	query := fmt.Sprintf(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = '%s' AND name NOT LIKE 'sqlite_autoindex_%%' ORDER BY name`, indexSet.Table)
	if dialect == "mysql" {
		query = fmt.Sprintf(`SELECT DISTINCT INDEX_NAME FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = '%s' AND INDEX_NAME <> 'PRIMARY' ORDER BY INDEX_NAME`, indexSet.Table)
	}

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("mirrorIndexNames(%s) query: %v", indexSet.Table, err)
	}
	defer func() { _ = rows.Close() }()

	indexes := make([]string, 0, len(indexSet.Indexes))
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatalf("mirrorIndexNames(%s) scan: %v", indexSet.Table, err)
		}

		indexes = append(indexes, name)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("mirrorIndexNames(%s) rows: %v", indexSet.Table, err)
	}

	return indexes
}

func sampleMirrorIndexColumns(t *testing.T, db *sql.DB, dialect string) map[string][]string {
	t.Helper()

	if dialect != "mysql" {
		indexes := sampleMirrorIndexNames(t, db, dialect)
		columns := make(map[string][]string, len(indexes))
		for _, name := range indexes {
			columns[name] = sampleMirrorSecondaryIndexColumns()[name]
		}

		return columns
	}

	rows, err := db.Query(`SELECT INDEX_NAME, COLUMN_NAME FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sample_mirror' AND INDEX_NAME <> 'PRIMARY' ORDER BY INDEX_NAME, SEQ_IN_INDEX`)
	if err != nil {
		t.Fatalf("sampleMirrorIndexColumns query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string][]string, len(sampleMirrorSecondaryIndexes))
	for rows.Next() {
		var indexName string
		var columnName string
		if err = rows.Scan(&indexName, &columnName); err != nil {
			t.Fatalf("sampleMirrorIndexColumns scan: %v", err)
		}

		columns[indexName] = append(columns[indexName], columnName)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("sampleMirrorIndexColumns rows: %v", err)
	}

	return columns
}

type recordedSQLStatement struct {
	Query string
	Args  []driver.NamedValue
}

type sqliteSyncSQLObserver struct {
	mu         sync.Mutex
	begins     int
	statements []recordedSQLStatement
	commits    int
}

func (o *sqliteSyncSQLObserver) Record(query string, args []driver.NamedValue) {
	o.mu.Lock()
	defer o.mu.Unlock()

	copyArgs := make([]driver.NamedValue, len(args))
	copy(copyArgs, args)
	o.statements = append(o.statements, recordedSQLStatement{Query: query, Args: copyArgs})
}

func (o *sqliteSyncSQLObserver) IncrementCommit() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.commits++
}

func (o *sqliteSyncSQLObserver) IncrementBegin() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.begins++
}

func (o *sqliteSyncSQLObserver) Statements() []recordedSQLStatement {
	o.mu.Lock()
	defer o.mu.Unlock()

	statements := make([]recordedSQLStatement, len(o.statements))
	copy(statements, o.statements)

	return statements
}

func (o *sqliteSyncSQLObserver) CommitCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.commits
}

func (o *sqliteSyncSQLObserver) BeginCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.begins
}

func (o *sqliteSyncSQLObserver) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.begins = 0
	o.statements = nil
	o.commits = 0
}

type recordingSQLiteDriver struct {
	base modernsqlite.Driver
}

type recordingSQLiteConn struct {
	driver.Conn
	observer *sqliteSyncSQLObserver
}

type recordingSQLiteTx struct {
	driver.Tx
	observer *sqliteSyncSQLObserver
}

func (d *recordingSQLiteDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}

	recordingSQLiteObserversMu.Lock()
	observer := recordingSQLiteObservers[name]
	recordingSQLiteObserversMu.Unlock()

	return &recordingSQLiteConn{Conn: conn, observer: observer}, nil
}

func (c *recordingSQLiteConn) Begin() (driver.Tx, error) {
	//nolint:staticcheck // Required to satisfy the legacy driver.Conn interface in this test wrapper.
	tx, err := c.Conn.Begin()
	if err != nil || c.observer == nil {
		return tx, err
	}
	c.observer.IncrementBegin()

	return &recordingSQLiteTx{Tx: tx, observer: c.observer}, nil
}

func (c *recordingSQLiteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	beginner, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return c.Begin()
	}

	tx, err := beginner.BeginTx(ctx, opts)
	if err != nil || c.observer == nil {
		return tx, err
	}
	c.observer.IncrementBegin()

	return &recordingSQLiteTx{Tx: tx, observer: c.observer}, nil
}

func (c *recordingSQLiteConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	if c.observer != nil {
		c.observer.Record(query, args)
	}

	return execer.ExecContext(ctx, query, args)
}

func (c *recordingSQLiteConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	return queryer.QueryContext(ctx, query, args)
}

func (c *recordingSQLiteConn) Ping(ctx context.Context) error {
	pinger, ok := c.Conn.(driver.Pinger)
	if !ok {
		return nil
	}

	return pinger.Ping(ctx)
}

func (c *recordingSQLiteConn) CheckNamedValue(value *driver.NamedValue) error {
	checker, ok := c.Conn.(driver.NamedValueChecker)
	if !ok {
		return driver.ErrSkip
	}

	return checker.CheckNamedValue(value)
}

func (c *recordingSQLiteConn) ResetSession(ctx context.Context) error {
	resetter, ok := c.Conn.(driver.SessionResetter)
	if !ok {
		return nil
	}

	return resetter.ResetSession(ctx)
}

func (c *recordingSQLiteConn) IsValid() bool {
	validator, ok := c.Conn.(driver.Validator)
	if !ok {
		return true
	}

	return validator.IsValid()
}

func (tx *recordingSQLiteTx) Commit() error {
	err := tx.Tx.Commit()
	if err == nil && tx.observer != nil {
		tx.observer.IncrementCommit()
	}

	return err
}

func openRecordingSQLiteSyncTestCache(t *testing.T) (Cache, *sqliteSyncSQLObserver) {
	t.Helper()

	recordingSQLiteDriverOnce.Do(func() {
		sql.Register("wa-sync-recording-sqlite", &recordingSQLiteDriver{})
	})

	path := filepath.Join(t.TempDir(), "sync-recording.sqlite")
	rwDSN := sqliteWritableDSN(path)
	roDSN := sqliteReadOnlyDSN(path)
	observer := &sqliteSyncSQLObserver{}

	recordingSQLiteObserversMu.Lock()
	recordingSQLiteObservers[rwDSN] = observer
	recordingSQLiteObservers[roDSN] = nil
	recordingSQLiteObserversMu.Unlock()

	originalOpen := sqlOpenFunc
	sqlOpenFunc = func(driverName, dataSourceName string) (*sql.DB, error) {
		if driverName == "sqlite" {
			return sql.Open("wa-sync-recording-sqlite", dataSourceName)
		}

		return originalOpen(driverName, dataSourceName)
	}
	t.Cleanup(func() {
		sqlOpenFunc = originalOpen
		recordingSQLiteObserversMu.Lock()
		delete(recordingSQLiteObservers, rwDSN)
		delete(recordingSQLiteObservers, roDSN)
		recordingSQLiteObserversMu.Unlock()
	})

	cache, err := OpenCache(context.Background(), CacheConfig{Path: path})
	if err != nil {
		t.Fatalf("OpenCache(): %v", err)
	}

	observer.Reset()

	return cache, observer
}

func normalizeSQL(query string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
}

func filterRecordedStatements(statements []recordedSQLStatement, keep func(recordedSQLStatement) bool) []recordedSQLStatement {
	filtered := make([]recordedSQLStatement, 0, len(statements))
	for _, statement := range statements {
		if keep(statement) {
			filtered = append(filtered, statement)
		}
	}

	return filtered
}

func TestSyncResolverLibraryColdCacheReturnsNeverSynced(t *testing.T) {
	convey.Convey("Given a cold cache without flowcell sync state", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveLibrary(context.Background(), "Standard")

		convey.Convey("when ResolveLibrary executes, then it returns the never-synced sentinel instead of syncing", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(match, convey.ShouldResemble, Match{})
		})
	})
}

func TestResolveSampleColdCacheDonorStepReturnsNeverSynced(t *testing.T) {
	convey.Convey("Given a cold cache and a donor lookup that reaches the cache-backed donor step", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
		}

		match, err := client.ResolveSample(context.Background(), "DONOR-X")

		convey.Convey("when the donor_id step is reached, then it returns the never-synced sentinel instead of syncing", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(match, convey.ShouldResemble, Match{})
		})
	})
}

func TestResolveLibraryAndSampleWarmCacheSkipSync(t *testing.T) {
	convey.Convey("Given warm resolver-backed caches with matching rows already present", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorRow(t, cache.DB(), 31, "SANGER-31", "supplier-31", "DONOR-WARM", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		seedDonorSampleRow(t, cache.DB(), "DONOR-WARM", 31, "study-31")
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))

		_, err := cache.DB().Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "Standard", 31, "study-31")
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 14, 5, 0, 0, time.UTC))

		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				return fmt.Errorf("unexpected sync invocation")
			},
		}

		libraryMatch, libraryErr := client.ResolveLibrary(context.Background(), "Standard")
		sampleMatch, sampleErr := client.ResolveSample(context.Background(), "DONOR-WARM")

		convey.Convey("when the resolvers run against a warm cache, then they use cached rows without syncing again", func() {
			convey.So(libraryErr, convey.ShouldBeNil)
			convey.So(libraryMatch.Kind, convey.ShouldEqual, KindLibraryType)
			convey.So(libraryMatch.Canonical, convey.ShouldEqual, "Standard")

			convey.So(sampleErr, convey.ShouldBeNil)
			convey.So(sampleMatch.Kind, convey.ShouldEqual, KindDonorID)
			convey.So(sampleMatch.Canonical, convey.ShouldEqual, "SANGER-31")
		})
	})
}

func openSQLiteSyncTestCache(t *testing.T) Cache {
	t.Helper()

	cache, err := OpenCache(context.Background(), CacheConfig{Path: filepath.Join(t.TempDir(), "sync.sqlite")})
	if err != nil {
		t.Fatalf("OpenCache(): %v", err)
	}

	return cache
}

func withSyncColdBatchSizeForTest(t *testing.T, size int) {
	t.Helper()

	original := syncColdBatchSize
	syncColdBatchSize = size
	t.Cleanup(func() {
		syncColdBatchSize = original
	})
}

type syncCommitCounter struct {
	mu      sync.Mutex
	commits int
}

func (c *syncCommitCounter) Increment() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.commits++
}

func (c *syncCommitCounter) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.commits
}

func (c *syncCommitCounter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.commits = 0
}

func openCountingSQLiteSyncTestCache(t *testing.T) (Cache, *syncCommitCounter) {
	t.Helper()

	syncCountingSQLiteDriverOnce.Do(func() {
		var driver modernsqlite.Driver
		driver.RegisterConnectionHook(func(conn modernsqlite.ExecQuerierContext, dsn string) error {
			syncCountingSQLiteCountersMu.Lock()
			counter := syncCountingSQLiteCounters[dsn]
			syncCountingSQLiteCountersMu.Unlock()
			if counter == nil {
				return nil
			}

			hooker, ok := conn.(modernsqlite.HookRegisterer)
			if !ok {
				return nil
			}

			hooker.RegisterCommitHook(func() int32 {
				counter.Increment()

				return 0
			})

			return nil
		})

		sql.Register("wa-sync-counting-sqlite", &driver)
	})

	path := filepath.Join(t.TempDir(), "sync-counting.sqlite")
	rwDSN := sqliteWritableDSN(path)
	roDSN := sqliteReadOnlyDSN(path)
	counter := &syncCommitCounter{}

	syncCountingSQLiteCountersMu.Lock()
	syncCountingSQLiteCounters[rwDSN] = counter
	syncCountingSQLiteCounters[roDSN] = nil
	syncCountingSQLiteCountersMu.Unlock()

	originalOpen := sqlOpenFunc
	sqlOpenFunc = func(driverName, dataSourceName string) (*sql.DB, error) {
		if driverName == "sqlite" {
			return sql.Open("wa-sync-counting-sqlite", dataSourceName)
		}

		return originalOpen(driverName, dataSourceName)
	}
	t.Cleanup(func() {
		sqlOpenFunc = originalOpen
		syncCountingSQLiteCountersMu.Lock()
		delete(syncCountingSQLiteCounters, rwDSN)
		delete(syncCountingSQLiteCounters, roDSN)
		syncCountingSQLiteCountersMu.Unlock()
	})

	cache, err := OpenCache(context.Background(), CacheConfig{Path: path})
	if err != nil {
		t.Fatalf("OpenCache(): %v", err)
	}

	counter.Reset()

	return cache, counter
}

type sampleSyncRowOverride struct {
	IDSampleLims   string
	UUIDSampleLims string
	Name           string
	DonorID        string
}

func sampleSyncRowsForRange(startID, endID int64, base time.Time, overrides map[int64]sampleSyncRowOverride) [][]driver.Value {
	rows := make([][]driver.Value, 0, endID-startID+1)
	for id := startID; id <= endID; id++ {
		override := sampleSyncRowOverride{}
		if overrides != nil {
			override = overrides[id]
		}

		rows = append(rows, sampleSyncRowValues(id, base.Add(time.Duration(id-startID)*time.Second), override))
	}

	return rows
}

func sampleSyncRowValues(id int64, lastUpdated time.Time, override sampleSyncRowOverride) []driver.Value {
	idSampleLims := override.IDSampleLims
	if idSampleLims == "" {
		idSampleLims = formatInt(id)
	}
	uuidSampleLims := override.UUIDSampleLims
	if uuidSampleLims == "" {
		uuidSampleLims = fmt.Sprintf("sample-uuid-%d", id)
	}
	name := override.Name
	if name == "" {
		name = fmt.Sprintf("sample-%d", id)
	}
	donorID := override.DonorID
	if donorID == "" {
		donorID = fmt.Sprintf("donor-%d", id)
	}

	return []driver.Value{
		id,
		"SQSCP",
		idSampleLims,
		uuidSampleLims,
		name,
		fmt.Sprintf("sanger-%d", id),
		fmt.Sprintf("supplier-%d", id),
		fmt.Sprintf("acc-%d", id),
		donorID,
		int64(9606),
		"human",
		fmt.Sprintf("desc-%d", id),
		formatSyncTime(lastUpdated),
	}
}

func seedSampleMirrorRow(t *testing.T, db *sql.DB, id int64, name, supplierName, donorID string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		"SQSCP",
		formatInt(id+100),
		"seed-sample-uuid",
		name,
		"seed-sanger",
		supplierName,
		"seed-accession",
		donorID,
		9606,
		"human",
		"seed-description",
		formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedSampleMirrorRow(): %v", err)
	}
}

func seedDonorSampleRow(t *testing.T, db *sql.DB, donorID string, idSampleTmp int64, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(`INSERT INTO donor_samples(donor_id, id_sample_tmp) VALUES (?, ?)`, donorID, idSampleTmp)
	if err != nil {
		t.Fatalf("seedDonorSampleRow(): %v", err)
	}
}

func seedSyncState(t *testing.T, db *sql.DB, table string, highWater time.Time) {
	t.Helper()

	if err := writeSyncState(context.Background(), db, "sqlite", table, highWater, nil, false); err != nil {
		t.Fatalf("seedSyncState(): %v", err)
	}
}

func seedSyncStateWithCursor(t *testing.T, db *sql.DB, table string, highWater time.Time, resumeCursor string) {
	t.Helper()

	if err := writeSyncState(context.Background(), db, "sqlite", table, highWater, &resumeCursor, false); err != nil {
		t.Fatalf("seedSyncStateWithCursor(): %v", err)
	}
}

func studyRowValues(id int64, idLims, idStudyLims, uuidStudyLims, name, accession string, lastUpdated time.Time) []driver.Value {
	return []driver.Value{
		id,
		idLims,
		idStudyLims,
		uuidStudyLims,
		name,
		accession,
		"Study title " + formatInt(id),
		"Faculty sponsor " + formatInt(id),
		"active",
		"strategy",
		"group",
		"programme",
		"GRCh38",
		true,
		"study-type",
		false,
		false,
		"public",
		"EGAD0001",
		"EGAP0001",
		"immediate",
		formatSyncTime(lastUpdated),
	}
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()

	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("countRows(%q): %v", query, err)
	}

	return count
}

func formatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func readSyncHighWater(t *testing.T, db *sql.DB, table string) time.Time {
	t.Helper()

	var raw string
	if err := db.QueryRow(`SELECT high_water FROM sync_state WHERE table_name = ?`, table).Scan(&raw); err != nil {
		t.Fatalf("readSyncHighWater(%s): %v", table, err)
	}

	highWater, err := parseSyncTimeString(raw)
	if err != nil {
		t.Fatalf("parseSyncTimeString(%s): %v", raw, err)
	}

	return highWater
}

func readSyncResumeCursor(t *testing.T, db *sql.DB, table string) any {
	t.Helper()

	var raw sql.NullString
	if err := db.QueryRow(`SELECT resume_cursor FROM sync_state WHERE table_name = ?`, table).Scan(&raw); err != nil {
		t.Fatalf("readSyncResumeCursor(%s): %v", table, err)
	}
	if !raw.Valid {
		return nil
	}

	return raw.String
}

func mustParseSyncTime(t *testing.T, raw string) time.Time {
	t.Helper()

	parsed, err := parseSyncTimeString(raw)
	if err != nil {
		t.Fatalf("mustParseSyncTime(%s): %v", raw, err)
	}

	return parsed
}

func flowcellSyncRowsForRange(startID, endID int64, base time.Time) [][]driver.Value {
	rows := make([][]driver.Value, 0, endID-startID+1)
	for id := startID; id <= endID; id++ {
		rows = append(rows, []driver.Value{
			fmt.Sprintf("pipe-%d", id),
			id,
			fmt.Sprintf("study-%d", id),
			formatSyncTime(base.Add(time.Duration(id-startID) * time.Second)),
		})
	}

	return rows
}

func iseqProductMetricsSyncRowsForRange(startID, endID int64, base time.Time, productOffset int64) [][]driver.Value {
	rows := make([][]driver.Value, 0, endID-startID+1)
	for id := startID; id <= endID; id++ {
		rows = append(rows, []driver.Value{
			fmt.Sprintf("product-%d", id+productOffset),
			id,
			id + 2000,
			int64(9000 + id),
			int64(1),
			int64(1),
			id + 100,
			fmt.Sprintf("study-%d", id),
			int64(1),
			int64(1),
			int64(1),
			formatSyncTime(base.Add(time.Duration(id-startID) * time.Second)),
		})
	}

	return rows
}

func seqProductIRODSLocationsSyncRowsForRange(startID, endID int64, base time.Time, productOffset int64) [][]driver.Value {
	rows := make([][]driver.Value, 0, endID-startID+1)
	for id := startID; id <= endID; id++ {
		created := base.Add(time.Duration(id-startID) * time.Second)
		rows = append(rows, []driver.Value{
			id,
			fmt.Sprintf("product-%d", id+productOffset),
			"/seq",
			fmt.Sprintf("run/%d.cram", id),
			id + 100,
			fmt.Sprintf("study-%d", id),
			formatSyncTime(created),
			formatSyncTime(created),
			"illumina",
		})
	}

	return rows
}

func sampleRowsFromDriverValues(columns []string, values [][]driver.Value) *sqlmock.Rows {
	rows := sqlmock.NewRows(columns)
	for _, row := range values {
		rows.AddRow(row...)
	}

	return rows
}

func namedValueOrdinals(values []driver.NamedValue) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value.Value)
	}

	return args
}

func syncSingleRowSourcePlan(columns []string, row []driver.Value, startSink func(syncStartRecord)) syncTestSourcePlan {
	return syncTestSourcePlan{
		columns: columns,
		rows:    [][]driver.Value{row},
		startSink: func(record syncStartRecord) {
			if startSink != nil {
				startSink(record)
			}
		},
	}
}

func nonEmptyLines(value string) []string {
	parts := strings.Split(value, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		lines = append(lines, trimmed)
	}

	return lines
}

func matchesPattern(value, pattern string) bool {
	matched, err := regexp.MatchString(pattern, value)
	if err != nil {
		return false
	}

	return matched
}

func openSyncTestSourceDB(t *testing.T, plans map[string]syncTestSourcePlan) *sql.DB {
	t.Helper()

	syncTestDriverOnce.Do(func() {
		sql.Register("wa-sync-test-driver", syncTestDriver{})
	})

	syncTestDriverMu.Lock()
	syncTestDriverSeq++
	dsn := fmt.Sprintf("sync-test-%d", syncTestDriverSeq)
	syncTestDrivers[dsn] = &syncTestDriverState{plans: plans, queryCount: make(map[string]int, len(plans))}
	syncTestDriverMu.Unlock()

	db, err := sql.Open("wa-sync-test-driver", dsn)
	if err != nil {
		t.Fatalf("sql.Open(sync test source): %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		syncTestDriverMu.Lock()
		delete(syncTestDrivers, dsn)
		syncTestDriverMu.Unlock()
	})

	return db
}

func (syncTestDriver) Open(name string) (driver.Conn, error) {
	syncTestDriverMu.Lock()
	defer syncTestDriverMu.Unlock()

	state, ok := syncTestDrivers[name]
	if !ok {
		return nil, fmt.Errorf("unknown sync test dsn %q", name)
	}

	return &syncTestConn{state: state}, nil
}

func (c *syncTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare not supported")
}

func (c *syncTestConn) Close() error {
	return nil
}

func (c *syncTestConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("transactions not supported")
}

func (c *syncTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	table := syncTableForQuery(query)
	c.state.mu.Lock()
	plan, ok := c.state.plans[table]
	queryNumber := c.state.queryCount[table]
	c.state.queryCount[table] = queryNumber + 1
	c.state.mu.Unlock()
	if !ok {
		// Sync() now fans out over the new platform-coverage / run-status /
		// tracking tables too. A full-Sync test that only cares about the
		// original five need not seed those, so a recognised new table with no
		// plan serves an empty result set (it syncs zero rows). An unrecognised
		// or unseeded original table still errors, preserving the seed guard.
		if newSyncTestSourceTables[table] {
			return &syncTestRows{}, nil
		}

		return nil, fmt.Errorf("unexpected sync test query for %s: %s", table, query)
	}

	queryPlan := syncTestQueryResult{
		rows:            plan.rows,
		queryErr:        plan.queryErr,
		finalErr:        plan.finalErr,
		firstRowDelay:   plan.firstRowDelay,
		blockBeforeRow2: plan.blockBeforeRow2,
		afterFirstRow:   plan.afterFirstRow,
		startSink:       plan.startSink,
		querySink:       plan.querySink,
	}
	if len(plan.queryResults) > 0 {
		if queryNumber >= len(plan.queryResults) {
			queryPlan = plan.queryResults[len(plan.queryResults)-1]
		} else {
			queryPlan = plan.queryResults[queryNumber]
		}
		if len(queryPlan.rows) == 0 {
			queryPlan.rows = plan.rows
		}
		if queryPlan.startSink == nil {
			queryPlan.startSink = plan.startSink
		}
		if queryPlan.querySink == nil {
			queryPlan.querySink = plan.querySink
		}
	}
	if queryPlan.queryErr != nil {
		return nil, queryPlan.queryErr
	}
	if queryPlan.querySink != nil {
		queryPlan.querySink(query, args)
	}

	return &syncTestRows{columns: append([]string(nil), plan.columns...), plan: queryPlan}, nil
}

func (r *syncTestRows) Columns() []string {
	return append([]string(nil), r.columns...)
}

func (r *syncTestRows) Close() error {
	return nil
}

func (r *syncTestRows) Next(dest []driver.Value) error {
	if !r.started {
		r.started = true
		if r.plan.firstRowDelay > 0 {
			time.Sleep(r.plan.firstRowDelay)
		}
		if r.plan.startSink != nil {
			r.plan.startSink(syncStartRecord{At: time.Now(), GoroutineID: currentGoroutineID()})
		}
	}

	if r.index >= len(r.plan.rows) {
		if r.plan.finalErr != nil {
			return r.plan.finalErr
		}

		return io.EOF
	}

	if r.index == 1 && r.plan.blockBeforeRow2 != nil {
		<-r.plan.blockBeforeRow2
	}

	row := r.plan.rows[r.index]
	if len(row) != len(dest) {
		return fmt.Errorf("sync test row has %d values for %d columns", len(row), len(dest))
	}
	copy(dest, row)
	r.index++
	if r.index == 1 && r.plan.afterFirstRow != nil {
		r.plan.afterFirstRow()
	}

	return nil
}

func syncTableForQuery(query string) string {
	switch {
	case strings.Contains(query, " FROM sample "):
		return syncTableSample
	case strings.Contains(query, " FROM study "):
		return syncTableStudy
	case strings.Contains(query, " FROM iseq_flowcell "):
		return syncTableIseqFlowcell
	case strings.Contains(query, " FROM iseq_product_metrics ipm "):
		return syncTableIseqProductMetrics
	// The iRODS source query embeds the per-platform *_product_metrics tables in
	// its recovery UNION, so it must be matched (on its spi alias) before the
	// standalone product-metrics queries below, which share those table aliases.
	case strings.Contains(query, " FROM seq_product_irods_locations spi "):
		return syncTableSeqProductIRODSLocations
	case strings.Contains(query, " FROM pac_bio_product_metrics pbm "):
		return syncTablePacBioProductMetrics
	case strings.Contains(query, " FROM eseq_product_metrics epm "):
		return syncTableEseqProductMetrics
	case strings.Contains(query, " FROM useq_product_metrics upm "):
		return syncTableUseqProductMetrics
	case strings.Contains(query, " FROM iseq_run_status "):
		return syncTableIseqRunStatus
	case strings.Contains(query, " FROM iseq_run_status_dict "):
		return syncTableIseqRunStatusDict
	case strings.Contains(query, " FROM oseq_flowcell ofc "):
		return syncTableOseqFlowcell
	case strings.Contains(query, " FROM pac_bio_run_well_metrics "):
		return syncTablePacBioRunWellMetrics
	case strings.Contains(query, " FROM eseq_run_lane_metrics "):
		return syncTableEseqRunLaneMetrics
	case strings.Contains(query, " FROM eseq_run "):
		return syncTableEseqRun
	case strings.Contains(query, " FROM useq_run_metrics "):
		return syncTableUseqRunMetrics
	case strings.Contains(query, "seq_ops_tracking_per_sample"):
		return syncTableSeqOpsTrackingPerSample
	default:
		return "unknown"
	}
}

func currentGoroutineID() int64 {
	var buffer [64]byte
	count := runtime.Stack(buffer[:], false)
	fields := strings.Fields(strings.TrimPrefix(string(buffer[:count]), "goroutine "))
	if len(fields) == 0 {
		return 0
	}

	id, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}

	return id
}
