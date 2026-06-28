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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// TestClientSyncPacBioProductMetricsIncrementalFollowsLastChanged covers the
// per-platform product-metrics incremental strategy (the iseq_product_metrics
// last_changed precedent): a NULL qc is preserved as pending, and the sync_state
// high_water tracks the latest source last_changed.
func TestClientSyncPacBioProductMetricsIncrementalFollowsLastChanged(t *testing.T) {
	convey.Convey("A5: Given PacBio product-metrics source rows with a NULL qc and ascending last_changed", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 13, 9, 0, 0, 0, time.UTC)
		latest := base.Add(2 * time.Hour)
		seedRealMLWHStudyRow(t, source, 73, "SQSCP", "7301", "uuid-study-73", "Study PacBio Metrics", "acc-st-73", base)
		seedRealMLWHPacBioRunRow(t, source, 9300, 931, 73)
		seedRealMLWHPacBioProductMetricRow(t, source, 93001, 9300, "pacbio-metric-1", base)
		seedRealMLWHPacBioProductMetricRow(t, source, 93002, 9300, "pacbio-metric-2", latest)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTablePacBioProductMetrics)

		convey.Convey("when synced, then both rows mirror with sample/study, NULL qc stays pending, and high_water is the latest last_changed", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 2)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM pac_bio_product_metrics_mirror WHERE id_study_lims = ?`, "7301"), convey.ShouldEqual, 2)

			var idSampleTmp int64
			var idStudyLims string
			var qc sql.NullInt64
			convey.So(cache.DB().QueryRow(`SELECT id_sample_tmp, id_study_lims, qc FROM pac_bio_product_metrics_mirror WHERE id_pac_bio_product = ?`, "pacbio-metric-1").Scan(&idSampleTmp, &idStudyLims, &qc), convey.ShouldBeNil)
			convey.So(idSampleTmp, convey.ShouldEqual, 931)
			convey.So(idStudyLims, convey.ShouldEqual, "7301")
			convey.So(qc.Valid, convey.ShouldBeFalse)
			convey.So(qcString(qc), convey.ShouldEqual, "pending")

			convey.So(readSyncHighWater(t, cache.DB(), syncTablePacBioProductMetrics), convey.ShouldEqual, latest)
		})
	})
}

// TestClientSyncIseqRunStatusDictWholesaleReplace covers the wholesale-replace
// strategy for the dict table: each run fully replaces the small table.
func TestClientSyncIseqRunStatusDictWholesaleReplace(t *testing.T) {
	convey.Convey("A5: Given an iseq_run_status_dict source and a stale mirror row", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHIseqRunStatusDictRow(t, source, 1, "qc review pending", 23)
		seedRealMLWHIseqRunStatusDictRow(t, source, 2, "run pending", 1)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// A stale dict row not present in the source must be removed by the
		// wholesale replace.
		_, err := cache.DB().Exec(`INSERT INTO iseq_run_status_dict_mirror(id_run_status_dict, description, temporal_index) VALUES (?, ?, ?)`, 99, "stale", 99)
		convey.So(err, convey.ShouldBeNil)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatusDict)

		convey.Convey("when synced, then the mirror equals the source (stale row gone)", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_run_status_dict_mirror`), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_run_status_dict_mirror WHERE id_run_status_dict = ?`, 99), convey.ShouldEqual, 0)

			var description string
			var temporal int
			convey.So(cache.DB().QueryRow(`SELECT description, temporal_index FROM iseq_run_status_dict_mirror WHERE id_run_status_dict = ?`, 1).Scan(&description, &temporal), convey.ShouldBeNil)
			convey.So(description, convey.ShouldEqual, "qc review pending")
			convey.So(temporal, convey.ShouldEqual, 23)
		})
	})
}

// TestClientSyncOseqFlowcellWholesaleReplace covers the wholesale-replace
// strategy for ONT identity (oseq_flowcell), recovering id_study_lims via study.
func TestClientSyncOseqFlowcellWholesaleReplace(t *testing.T) {
	convey.Convey("A5: Given an oseq_flowcell source linked to an SQSCP study", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 14, 9, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 74, "SQSCP", "7401", "uuid-study-74", "Study ONT", "acc-st-74", base)
		seedRealMLWHOseqFlowcellRow(t, source, 9400, 941, 74)
		seedRealMLWHOseqFlowcellRow(t, source, 9401, 942, 74)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableOseqFlowcell)

		convey.Convey("when synced, then the ONT identity rows mirror with their recovered id_study_lims", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 2)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM oseq_flowcell_mirror WHERE id_study_lims = ?`, "7401"), convey.ShouldEqual, 2)

			var idSampleTmp int64
			convey.So(cache.DB().QueryRow(`SELECT id_sample_tmp FROM oseq_flowcell_mirror WHERE id_oseq_flowcell_tmp = ?`, 9400).Scan(&idSampleTmp), convey.ShouldBeNil)
			convey.So(idSampleTmp, convey.ShouldEqual, 941)
		})
	})
}

// TestClientSyncPacBioRunWellMetricsWholesaleReplace covers the wholesale-replace
// strategy for a per-run status/date table: the nullable status/date columns
// mirror faithfully (NULL stays NULL), and a NULL source last_changed normalizes
// to the zero RFC3339 time for the NOT NULL mirror last_updated column.
func TestClientSyncPacBioRunWellMetricsWholesaleReplace(t *testing.T) {
	convey.Convey("A5: Given a pac_bio_run_well_metrics source with status/date columns and a NULL last_changed", t, func() {
		source := openRealMLWHSchemaSource(t)
		runComplete := time.Date(2026, time.June, 16, 12, 0, 0, 0, time.UTC)
		if _, err := source.Exec(
			`INSERT INTO pac_bio_run_well_metrics(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_start, run_complete, well_complete, qc_seq_date, run_status, well_status, last_changed) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			9600, "r64-run", "A01", 1, nil, formatSyncTime(runComplete), nil, nil, "Complete", "Passed", nil,
		); err != nil {
			t.Fatalf("seed pac_bio_run_well_metrics: %v", err)
		}

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTablePacBioRunWellMetrics)

		convey.Convey("when synced, then the well row mirrors with its status preserved and NULL last_changed as zero time", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)

			var runStatus, wellStatus, runStartCol, lastUpdated string
			var runComp sql.NullString
			convey.So(cache.DB().QueryRow(`SELECT run_status, well_status, COALESCE(run_start, '<null>'), run_complete, last_updated FROM pac_bio_run_well_metrics_mirror WHERE id_pac_bio_rw_metrics_tmp = ?`, 9600).Scan(&runStatus, &wellStatus, &runStartCol, &runComp, &lastUpdated), convey.ShouldBeNil)
			convey.So(runStatus, convey.ShouldEqual, "Complete")
			convey.So(wellStatus, convey.ShouldEqual, "Passed")
			convey.So(runStartCol, convey.ShouldEqual, "<null>")
			convey.So(runComp.String, convey.ShouldEqual, formatSyncTime(runComplete))
			convey.So(lastUpdated, convey.ShouldEqual, formatSyncTime(time.Time{}))
		})
	})
}

// recordingOrderSource wraps a real SQLite source and reports the ascending-id
// paging cursor (the value bound to "id_run_status > ?") of each iseq_run_status
// source query, so a test can assert the ascending read order from the cursor
// progression without consuming the result rows the sync needs.
type recordingOrderSource struct {
	db      *sql.DB
	observe func(int64)
}

func (s recordingOrderSource) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "FROM iseq_run_status ") && len(args) == 1 {
		if cursor, ok := args[0].(int64); ok {
			s.observe(cursor)
		}
	}

	return s.db.QueryContext(ctx, query, args...)
}

// TestClientSyncIseqRunStatusReadsRowsInAscendingIDOrder covers A5.1: the
// iseq_run_status mirror is synced in ascending id_run_status order (the
// ascending-id strategy, no last_changed) and contains every source row.
func TestClientSyncIseqRunStatusReadsRowsInAscendingIDOrder(t *testing.T) {
	convey.Convey("A5.1: Given a mocked iseq_run_status source", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 10, 9, 0, 0, 0, time.UTC)

		// Insert rows out of id order so a correct ascending-id read must reorder
		// them; ids 5,1,3,2,4 with ascending dates so the per-run timeline is sane.
		seedRealMLWHIseqRunStatusRow(t, source, 5, 52553, 4, base.Add(4*time.Hour), 0)
		seedRealMLWHIseqRunStatusRow(t, source, 1, 52553, 1, base, 0)
		seedRealMLWHIseqRunStatusRow(t, source, 3, 52553, 3, base.Add(2*time.Hour), 0)
		seedRealMLWHIseqRunStatusRow(t, source, 2, 52553, 2, base.Add(time.Hour), 0)
		seedRealMLWHIseqRunStatusRow(t, source, 4, 52553, 5, base.Add(3*time.Hour), 1)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// With a batch size of 1 the ascending-id sync pages forward one row at a
		// time, so the cursor it carries in each successive source query (the value
		// in "id_run_status > ?") reveals the read order directly.
		withSyncColdBatchSizeForTest(t, 1)

		var cursors []int64
		client := &Client{
			cache:           cache,
			cacheReader:     cacheReadDB(cache),
			syncSource:      recordingOrderSource{db: source, observe: func(cursor int64) { cursors = append(cursors, cursor) }},
			disableSyncLock: true,
		}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatus)

		convey.Convey("when synced, then rows are read in ascending id_run_status order and the mirror contains all of them", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 5)

			// The ascending-id paging cursor advanced strictly 0 -> 1 -> 2 -> 3 -> 4
			// -> 5 (one row per page after each id), proving rows are read in
			// ascending id_run_status order rather than by date or insertion order.
			convey.So(cursors, convey.ShouldResemble, []int64{0, 1, 2, 3, 4, 5})

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_run_status_mirror`), convey.ShouldEqual, 5)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_run_status_mirror WHERE id_run = ?`, 52553), convey.ShouldEqual, 5)

			// The mirror stores every row faithfully, including the source iscurrent
			// flag (read-time "current" derivation is a later phase's concern).
			var idRunStatusDict, iscurrent int
			var date string
			convey.So(cache.DB().QueryRow(`SELECT id_run_status_dict, date, iscurrent FROM iseq_run_status_mirror WHERE id_run_status = ?`, 4).Scan(&idRunStatusDict, &date, &iscurrent), convey.ShouldBeNil)
			convey.So(idRunStatusDict, convey.ShouldEqual, 5)
			convey.So(iscurrent, convey.ShouldEqual, 1)
			convey.So(date, convey.ShouldEqual, formatSyncTime(base.Add(3*time.Hour)))
		})
	})
}

func seedRealMLWHIseqRunStatusRow(t *testing.T, db *sql.DB, idRunStatus, idRun, idRunStatusDict int64, date time.Time, iscurrent int) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO iseq_run_status(id_run_status, id_run, date, id_run_status_dict, iscurrent) VALUES (?, ?, ?, ?, ?)`,
		idRunStatus, idRun, formatSyncTime(date), idRunStatusDict, iscurrent,
	); err != nil {
		t.Fatalf("seedRealMLWHIseqRunStatusRow: %v", err)
	}
}

// TestClientSyncEseqProductMetricsIncrementalAdvancesOnSecondSync covers the
// per-platform product-metrics incremental strategy for a three-QC-column table
// (eseq): the qc/qc_seq/qc_lib columns mirror NULL-preservingly, and a second
// sync only picks up rows newer than the stored high_water (the last_changed
// incremental window), exactly like the iseq_product_metrics precedent.
func TestClientSyncEseqProductMetricsIncrementalAdvancesOnSecondSync(t *testing.T) {
	convey.Convey("A5: Given an Elembio product-metrics source synced once, then given a newer row", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 15, 9, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 75, "SQSCP", "7501", "uuid-study-75", "Study Elembio", "acc-st-75", base)
		seedRealMLWHEseqFlowcellRow(t, source, 9500, 951, 75)
		seedRealMLWHEseqProductMetricRow(t, source, "eseq-metric-1", 9500, sql.NullInt64{Int64: 1, Valid: true}, sql.NullInt64{}, sql.NullInt64{Int64: 0, Valid: true}, base)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		firstReports, err := syncSelectedTablesForTest(context.Background(), client, syncTableEseqProductMetrics)
		convey.So(err, convey.ShouldBeNil)
		convey.So(firstReports[0].Inserted, convey.ShouldEqual, 1)

		// A new row arrives after the first sync's high_water.
		newer := base.Add(time.Hour)
		seedRealMLWHEseqProductMetricRow(t, source, "eseq-metric-2", 9500, sql.NullInt64{Int64: 0, Valid: true}, sql.NullInt64{Int64: 1, Valid: true}, sql.NullInt64{Int64: 1, Valid: true}, newer)

		secondReports, err := syncSelectedTablesForTest(context.Background(), client, syncTableEseqProductMetrics)

		convey.Convey("when synced again, then only the newer row inserts, the QC columns map per-value, and high_water advances", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(secondReports, convey.ShouldHaveLength, 1)
			convey.So(secondReports[0].Inserted, convey.ShouldEqual, 1)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM eseq_product_metrics_mirror`), convey.ShouldEqual, 2)

			// Row 1: qc=1 (pass), qc_seq NULL (pending), qc_lib=0 (fail) preserved.
			var qc, qcSeq, qcLib sql.NullInt64
			convey.So(cache.DB().QueryRow(`SELECT qc, qc_seq, qc_lib FROM eseq_product_metrics_mirror WHERE id_eseq_product = ?`, "eseq-metric-1").Scan(&qc, &qcSeq, &qcLib), convey.ShouldBeNil)
			convey.So(qcString(qc), convey.ShouldEqual, "pass")
			convey.So(qcSeq.Valid, convey.ShouldBeFalse)
			convey.So(qcString(qcSeq), convey.ShouldEqual, "pending")
			convey.So(qcString(qcLib), convey.ShouldEqual, "fail")

			convey.So(readSyncHighWater(t, cache.DB(), syncTableEseqProductMetrics), convey.ShouldEqual, newer)
		})
	})
}

func seedRealMLWHEseqFlowcellRow(t *testing.T, db *sql.DB, idEseqFlowcellTmp, idSampleTmp, idStudyTmp int64) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO eseq_flowcell(id_eseq_flowcell_tmp, id_sample_tmp, id_study_tmp) VALUES (?, ?, ?)`,
		idEseqFlowcellTmp, idSampleTmp, idStudyTmp,
	); err != nil {
		t.Fatalf("seedRealMLWHEseqFlowcellRow: %v", err)
	}
}

func seedRealMLWHEseqProductMetricRow(t *testing.T, db *sql.DB, idProduct string, idFlowcellTmp int64, qc, qcSeq, qcLib sql.NullInt64, lastChanged time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO eseq_product_metrics(id_eseq_pr_metrics_tmp, id_eseq_flowcell_tmp, id_eseq_product, qc, qc_seq, qc_lib, last_changed) VALUES ((SELECT COALESCE(MAX(id_eseq_pr_metrics_tmp), 0) + 1 FROM eseq_product_metrics), ?, ?, ?, ?, ?, ?)`,
		idFlowcellTmp, idProduct, qc, qcSeq, qcLib, formatSyncTime(lastChanged),
	); err != nil {
		t.Fatalf("seedRealMLWHEseqProductMetricRow: %v", err)
	}
}

// TestClientSyncSeqOpsTrackingPerSampleFullRefreshReplacesSnapshot covers A5.2:
// the tracking-table full-refresh sync replaces the whole mirror with the new
// source snapshot (old rows gone), and the swap is atomic.
func TestClientSyncSeqOpsTrackingPerSampleFullRefreshReplacesSnapshot(t *testing.T) {
	convey.Convey("A5.2: Given an existing populated seq_ops_tracking_per_sample_mirror and a fresh source snapshot", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 11, 9, 0, 0, 0, time.UTC)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// Pre-populate the mirror with stale rows that must be gone after refresh.
		seedTrackingMirrorRowForTest(t, cache.DB(), "OLD-1", "S-OLD", base)
		seedTrackingMirrorRowForTest(t, cache.DB(), "OLD-2", "S-OLD", base)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror`), convey.ShouldEqual, 2)

		// The fresh source snapshot has a disjoint set of samples.
		seedRealMLWHTrackingRow(t, source, "NEW-1", "S-NEW", base.Add(time.Hour))
		seedRealMLWHTrackingRow(t, source, "NEW-2", "S-NEW", base.Add(time.Hour))
		seedRealMLWHTrackingRow(t, source, "NEW-3", "S-NEW", base.Add(time.Hour))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when the full-refresh sync runs, then the mirror equals the new snapshot (old rows gone)", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror`), convey.ShouldEqual, 3)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror WHERE id_sample_lims LIKE 'OLD-%'`), convey.ShouldEqual, 0)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror WHERE id_sample_lims LIKE 'NEW-%'`), convey.ShouldEqual, 3)
		})
	})
}

// TestClientSyncSeqOpsTrackingPerSampleSwapIsAtomic covers A5.2's atomicity
// clause: the build-and-swap happens inside a single transaction, so a reader can
// never observe a partial table (a row count between the old and new snapshots).
//
// It proves this two ways. First, deterministically via WAL snapshot isolation: a
// read transaction opened on the independent read-only connection BEFORE the swap
// keeps seeing the whole old snapshot (2) for its entire life even after the swap
// commits, while a fresh read afterwards sees the whole new snapshot (3) -- a
// reader is therefore never exposed to an in-between (DELETE-but-not-yet-inserted)
// state, which is only possible if the swap is one transaction. Second, a
// concurrent poller running across the swap must only ever observe 2 or 3.
func TestClientSyncSeqOpsTrackingPerSampleSwapIsAtomic(t *testing.T) {
	convey.Convey("A5.2 (atomicity): Given a populated tracking mirror being replaced by a 3-row snapshot", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.June, 11, 9, 0, 0, 0, time.UTC)
		seedTrackingMirrorRowForTest(t, cache.DB(), "OLD-1", "S-OLD", base)
		seedTrackingMirrorRowForTest(t, cache.DB(), "OLD-2", "S-OLD", base)

		newRows := []seqOpsTrackingPerSampleSyncRow{
			newTrackingSyncRowForTest("NEW-1", "S-NEW", base),
			newTrackingSyncRowForTest("NEW-2", "S-NEW", base),
			newTrackingSyncRowForTest("NEW-3", "S-NEW", base),
		}

		// Open a read transaction on the independent reader BEFORE the swap. Under
		// WAL it pins the pre-swap snapshot for the whole transaction.
		reader := cacheReadDB(cache)
		readTx, err := reader.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = readTx.Rollback() }()

		var beforeSwap int
		convey.So(readTx.QueryRow(`SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror`).Scan(&beforeSwap), convey.ShouldBeNil)
		convey.So(beforeSwap, convey.ShouldEqual, 2)

		// Concurrently poll the count from a fresh connection across the swap.
		stop := make(chan struct{})
		var mu sync.Mutex
		observedCounts := map[int]struct{}{}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				var count int
				if pollErr := reader.QueryRow(`SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror`).Scan(&count); pollErr == nil {
					mu.Lock()
					observedCounts[count] = struct{}{}
					mu.Unlock()
				}
			}
		}()

		refreshTime := time.Now().UTC()
		swapErr := writeSeqOpsTrackingPerSampleFullRefresh(context.Background(), cache, newRows, refreshTime)
		close(stop)
		wg.Wait()

		convey.Convey("when the swap runs, then a reader never observes a partial table (only 2 or 3, never 0/1/5)", func() {
			convey.So(swapErr, convey.ShouldBeNil)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror`), convey.ShouldEqual, 3)

			// The read transaction opened before the swap still sees the full old
			// snapshot, never a mid-swap partial table.
			var duringTx int
			convey.So(readTx.QueryRow(`SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror`).Scan(&duringTx), convey.ShouldBeNil)
			convey.So(duringTx, convey.ShouldEqual, 2)

			mu.Lock()
			defer mu.Unlock()
			for count := range observedCounts {
				// The poller must only ever see the full old snapshot (2) or the
				// full new snapshot (3): never an empty/partial mid-swap table.
				convey.So(count == 2 || count == 3, convey.ShouldBeTrue)
			}
		})
	})
}

func seedTrackingMirrorRowForTest(t *testing.T, db *sql.DB, idSampleLims, studyID string, manifestCreated time.Time) {
	t.Helper()

	row := newTrackingSyncRowForTest(idSampleLims, studyID, manifestCreated)
	stmt := buildBulkInsertStatement("seq_ops_tracking_per_sample_mirror", seqOpsTrackingPerSampleMirrorColumns, 1)
	if _, err := db.Exec(stmt, seqOpsTrackingPerSampleMirrorRowArgs(row)...); err != nil {
		t.Fatalf("seedTrackingMirrorRowForTest: %v", err)
	}
}

func newTrackingSyncRowForTest(idSampleLims, studyID string, manifestCreated time.Time) seqOpsTrackingPerSampleSyncRow {
	return seqOpsTrackingPerSampleSyncRow{
		IDSampleLims:     idSampleLims,
		SangerSampleID:   idSampleLims,
		SangerSampleName: idSampleLims + "-name",
		StudyID:          studyID,
		Programme:        "DNA Pipelines",
		FacultySponsor:   "Sponsor",
		LibraryType:      "Standard",
		Platform:         "Illumina",
		ManifestCreated:  sql.NullString{String: formatSyncTime(manifestCreated), Valid: true},
	}
}

// TestClientSyncSeqOpsTrackingPerSampleSetsRefreshAndSyncTimes covers A5.3: the
// tracking sync_state high_water is the refresh time and last_run is the sync
// time.
func TestClientSyncSeqOpsTrackingPerSampleSetsRefreshAndSyncTimes(t *testing.T) {
	convey.Convey("A5.3: Given a tracking-table sync", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 12, 9, 0, 0, 0, time.UTC)
		seedRealMLWHTrackingRow(t, source, "TRK-1", "S-TRK", base)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		before := time.Now().UTC().Add(-time.Second)
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)
		after := time.Now().UTC().Add(time.Second)

		convey.Convey("when it completes, then high_water is the refresh time and last_run is the sync time", func() {
			convey.So(err, convey.ShouldBeNil)

			highWater := readSyncStateColumnTime(t, cache.DB(), syncTableSeqOpsTrackingPerSample, "high_water")
			lastRun := readSyncStateColumnTime(t, cache.DB(), syncTableSeqOpsTrackingPerSample, "last_run")

			// high_water (refresh time) and last_run (sync time) are both populated
			// with a real timestamp captured during this run, not the zero time.
			convey.So(highWater.IsZero(), convey.ShouldBeFalse)
			convey.So(highWater.After(before), convey.ShouldBeTrue)
			convey.So(highWater.Before(after), convey.ShouldBeTrue)
			convey.So(lastRun.IsZero(), convey.ShouldBeFalse)
			convey.So(lastRun.After(before), convey.ShouldBeTrue)
			convey.So(lastRun.Before(after), convey.ShouldBeTrue)
		})
	})
}

func readSyncStateColumnTime(t *testing.T, db *sql.DB, table, column string) time.Time {
	t.Helper()

	var raw string
	if err := db.QueryRow(`SELECT `+column+` FROM sync_state WHERE table_name = ?`, table).Scan(&raw); err != nil {
		t.Fatalf("readSyncStateColumnTime(%s,%s): %v", table, column, err)
	}

	parsed, err := parseSyncTimeString(raw)
	if err != nil {
		t.Fatalf("parse %s %s %q: %v", table, column, raw, err)
	}

	return parsed
}
