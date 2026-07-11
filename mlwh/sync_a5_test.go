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
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
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

func TestClientSyncSeqOpsTrackingPerSampleDeletesReusePreparedStatement(t *testing.T) {
	convey.Convey("Given multiple tracking rows to delete", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		query := `DELETE FROM seq_ops_tracking_per_sample_mirror WHERE id_sample_lims = ? AND id_sample_lims COLLATE BINARY = ?`
		mock.ExpectBegin()
		prepared := mock.ExpectPrepare(regexp.QuoteMeta(query))
		prepared.ExpectExec().WithArgs("CaseProbe", "CaseProbe").WillReturnResult(sqlmock.NewResult(0, 1))
		prepared.ExpectExec().WithArgs("caseprobe", "caseprobe").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		tx, err := db.BeginTx(context.Background(), nil)
		convey.So(err, convey.ShouldBeNil)
		convey.So(deleteSeqOpsTrackingPerSampleRows(context.Background(), tx, "sqlite", []string{"CaseProbe", "caseprobe"}), convey.ShouldBeNil)
		convey.So(tx.Commit(), convey.ShouldBeNil)

		convey.Convey("when the deletion is applied, then both exact-match writes reuse one prepared statement", func() {
			convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func isTrackingMirrorMutationForTest(statement recordedSQLStatement) bool {
	normalized := normalizeSQL(statement.Query)

	return strings.HasPrefix(normalized, "INSERT INTO seq_ops_tracking_per_sample_mirror") ||
		strings.HasPrefix(normalized, "UPDATE seq_ops_tracking_per_sample_mirror") ||
		strings.HasPrefix(normalized, "DELETE FROM seq_ops_tracking_per_sample_mirror")
}

func isSyncStateWriteForTest(statement recordedSQLStatement) bool {
	return strings.HasPrefix(normalizeSQL(statement.Query), "INSERT INTO sync_state")
}

func isTrackingMirrorReadForTest(statement recordedSQLStatement) bool {
	normalized := normalizeSQL(statement.Query)

	return strings.HasPrefix(normalized, "SELECT ") && strings.Contains(normalized, "FROM seq_ops_tracking_per_sample_mirror")
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

func TestClientSyncIseqRunStatusWarmSyncStartsAtRetainedWatermark(t *testing.T) {
	convey.Convey("C1: Given iseq_run_status rows cold-synced through id M and P newer source rows", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
		seedIseqRunStatusSourceRange(t, source, 1, 3, base)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		var cursors []int64
		client := &Client{
			cache:           cache,
			cacheReader:     cacheReadDB(cache),
			syncSource:      recordingOrderSource{db: source, observe: func(cursor int64) { cursors = append(cursors, cursor) }},
			disableSyncLock: true,
		}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatus)
		convey.So(err, convey.ShouldBeNil)

		seedIseqRunStatusSourceRange(t, source, 4, 5, base)
		cursors = nil
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatus)

		convey.Convey("when warm-synced, then paging starts at M and only the P new rows are inserted", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(cursors, convey.ShouldNotBeEmpty)
			convey.So(cursors[0], convey.ShouldEqual, int64(3))
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 2)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_run_status_mirror`), convey.ShouldEqual, 5)

			resumeID, valid := readIseqRunStatusResumeID(t, cache.DB())
			convey.So(valid, convey.ShouldBeTrue)
			convey.So(resumeID, convey.ShouldEqual, int64(5))
		})
	})
}

func seedIseqRunStatusSourceRange(t *testing.T, db *sql.DB, startID, endID int64, base time.Time) {
	t.Helper()

	for offset := range endID - startID + 1 {
		id := startID + offset
		seedRealMLWHIseqRunStatusRow(t, db, id, 52553, id, base.Add(time.Duration(id-1)*time.Hour), 0)
	}
}

func readIseqRunStatusResumeID(t *testing.T, db *sql.DB) (int64, bool) {
	t.Helper()

	var raw sql.NullString
	if err := db.QueryRow(`SELECT resume_cursor FROM sync_state WHERE table_name = ?`, syncTableIseqRunStatus).Scan(&raw); err != nil {
		t.Fatalf("read iseq_run_status resume cursor: %v", err)
	}
	if !raw.Valid {
		return 0, false
	}

	id, ok, err := parseAscendingIDResumeCursor(raw.String, iseqRunStatusIDResumeMode)
	if err != nil {
		t.Fatalf("parse iseq_run_status resume cursor: %v", err)
	}

	return id, ok
}

func TestClientSyncIseqRunStatusNullCursorUpgradeRereadsOnce(t *testing.T) {
	convey.Convey("C2: Given an existing iseq_run_status mirror with a NULL resume cursor", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.July, 3, 9, 0, 0, 0, time.UTC)
		seedIseqRunStatusSourceRange(t, source, 1, 3, base)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedIseqRunStatusMirrorRange(t, cache.DB(), 1, 3, base)
		seedNullCursorIseqRunStatusState(t, cache.DB(), base)

		var cursors []int64
		client := &Client{
			cache:           cache,
			cacheReader:     cacheReadDB(cache),
			syncSource:      recordingOrderSource{db: source, observe: func(cursor int64) { cursors = append(cursors, cursor) }},
			disableSyncLock: true,
		}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatus)

		convey.Convey("when warm-synced, then it rereads from zero once, updates existing rows, and establishes the watermark", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(cursors, convey.ShouldNotBeEmpty)
			convey.So(cursors[0], convey.ShouldEqual, int64(0))
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
			convey.So(reports[0].Updated, convey.ShouldEqual, 3)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_run_status_mirror`), convey.ShouldEqual, 3)

			resumeID, valid := readIseqRunStatusResumeID(t, cache.DB())
			convey.So(valid, convey.ShouldBeTrue)
			convey.So(resumeID, convey.ShouldEqual, int64(3))
		})
	})
}

func seedIseqRunStatusMirrorRange(t *testing.T, db *sql.DB, startID, endID int64, base time.Time) {
	t.Helper()

	for offset := range endID - startID + 1 {
		id := startID + offset
		date := base.Add(time.Duration(id-1) * time.Hour)
		if _, err := db.Exec(
			`INSERT INTO iseq_run_status_mirror(id_run_status, id_run, date, id_run_status_dict, iscurrent, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
			id, 52553, formatSyncTime(date), id, 0, formatSyncDate(date),
		); err != nil {
			t.Fatalf("seed iseq_run_status_mirror row %d: %v", id, err)
		}
	}
}

func seedNullCursorIseqRunStatusState(t *testing.T, db *sql.DB, lastRun time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, NULL, 0)`,
		syncTableIseqRunStatus, formatSyncTime(time.Time{}), formatSyncTime(lastRun),
	); err != nil {
		t.Fatalf("seed NULL-cursor iseq_run_status sync state: %v", err)
	}
}

func TestClientSyncIseqRunStatusAfterNullCursorUpgradeUsesRetainedWatermark(t *testing.T) {
	convey.Convey("C2: Given an upgraded iseq_run_status mirror after its one-time full reread", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.July, 4, 9, 0, 0, 0, time.UTC)
		seedIseqRunStatusSourceRange(t, source, 1, 3, base)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedIseqRunStatusMirrorRange(t, cache.DB(), 1, 3, base)
		seedNullCursorIseqRunStatusState(t, cache.DB(), base)

		var cursors []int64
		client := &Client{
			cache:           cache,
			cacheReader:     cacheReadDB(cache),
			syncSource:      recordingOrderSource{db: source, observe: func(cursor int64) { cursors = append(cursors, cursor) }},
			disableSyncLock: true,
		}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatus)
		convey.So(err, convey.ShouldBeNil)
		cursors = nil

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatus)

		convey.Convey("when warm-synced again, then it pages from M and reads no rows", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(cursors, convey.ShouldResemble, []int64{3})
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
		})
	})
}

func TestClientSyncIseqRunStatusNoopPreservesRetainedWatermark(t *testing.T) {
	convey.Convey("C1: Given an iseq_run_status mirror whose retained watermark is M+P", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC)
		seedIseqRunStatusSourceRange(t, source, 1, 3, base)

		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatus)
		convey.So(err, convey.ShouldBeNil)

		seedIseqRunStatusSourceRange(t, source, 4, 5, base)
		_, err = syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatus)
		convey.So(err, convey.ShouldBeNil)
		observer.Reset()

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqRunStatus)

		convey.Convey("when warm-synced with no newer rows, then no mirror row is written and the watermark remains non-NULL", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)

			mirrorWrites := filterRecordedStatements(observer.Statements(), func(statement recordedSQLStatement) bool {
				return strings.Contains(statement.Query, "iseq_run_status_mirror")
			})
			convey.So(mirrorWrites, convey.ShouldBeEmpty)

			resumeID, valid := readIseqRunStatusResumeID(t, cache.DB())
			convey.So(valid, convey.ShouldBeTrue)
			convey.So(resumeID, convey.ShouldEqual, int64(5))
			convey.So(readSyncHighWater(t, cache.DB(), syncTableIseqRunStatus).IsZero(), convey.ShouldBeTrue)
		})
	})
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
		seedRealMLWHEseqProductMetricRow(t, source, "eseq-metric-1", 9500, 7501, sql.NullInt64{Int64: 1, Valid: true}, sql.NullInt64{}, sql.NullInt64{Int64: 0, Valid: true}, base)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		firstReports, err := syncSelectedTablesForTest(context.Background(), client, syncTableEseqProductMetrics)
		convey.So(err, convey.ShouldBeNil)
		convey.So(firstReports[0].Inserted, convey.ShouldEqual, 1)

		// A new row arrives after the first sync's high_water.
		newer := base.Add(time.Hour)
		seedRealMLWHEseqProductMetricRow(t, source, "eseq-metric-2", 9500, 7502, sql.NullInt64{Int64: 0, Valid: true}, sql.NullInt64{Int64: 1, Valid: true}, sql.NullInt64{Int64: 1, Valid: true}, newer)

		secondReports, err := syncSelectedTablesForTest(context.Background(), client, syncTableEseqProductMetrics)

		convey.Convey("when synced again, then only the newer row inserts, the QC columns map per-value, and high_water advances", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(secondReports, convey.ShouldHaveLength, 1)
			convey.So(secondReports[0].Inserted, convey.ShouldEqual, 1)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM eseq_product_metrics_mirror`), convey.ShouldEqual, 2)

			// Row 1: qc=1 (pass), qc_seq NULL (pending), qc_lib=0 (fail) preserved, and
			// the source id_run (the eseq_run_lane_metrics join key) mirrors faithfully.
			var idRun int64
			var qc, qcSeq, qcLib sql.NullInt64
			convey.So(cache.DB().QueryRow(`SELECT id_run, qc, qc_seq, qc_lib FROM eseq_product_metrics_mirror WHERE id_eseq_product = ?`, "eseq-metric-1").Scan(&idRun, &qc, &qcSeq, &qcLib), convey.ShouldBeNil)
			convey.So(idRun, convey.ShouldEqual, 7501)
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

func seedRealMLWHEseqProductMetricRow(t *testing.T, db *sql.DB, idProduct string, idFlowcellTmp, idRun int64, qc, qcSeq, qcLib sql.NullInt64, lastChanged time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO eseq_product_metrics(id_eseq_pr_metrics_tmp, id_eseq_flowcell_tmp, id_run, id_eseq_product, qc, qc_seq, qc_lib, last_changed) VALUES ((SELECT COALESCE(MAX(id_eseq_pr_metrics_tmp), 0) + 1 FROM eseq_product_metrics), ?, ?, ?, ?, ?, ?, ?)`,
		idFlowcellTmp, idRun, idProduct, qc, qcSeq, qcLib, formatSyncTime(lastChanged),
	); err != nil {
		t.Fatalf("seedRealMLWHEseqProductMetricRow: %v", err)
	}
}

// TestClientSyncSeqOpsTrackingPerSampleDiffApplyReplacesSnapshot covers A5.2:
// the tracking-table diff/apply makes the mirror equal the new source snapshot
// by deleting absent rows and inserting new rows atomically.
func TestClientSyncSeqOpsTrackingPerSampleDiffApplyReplacesSnapshot(t *testing.T) {
	convey.Convey("A5.2: Given an existing populated seq_ops_tracking_per_sample_mirror and a fresh source snapshot", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 11, 9, 0, 0, 0, time.UTC)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// Pre-populate the mirror with stale rows that the diff must delete.
		seedTrackingMirrorRowForTest(t, cache.DB(), "OLD-1", "S-OLD", base)
		seedTrackingMirrorRowForTest(t, cache.DB(), "OLD-2", "S-OLD", base)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror`), convey.ShouldEqual, 2)

		// The fresh source snapshot has a disjoint set of samples.
		seedRealMLWHTrackingRow(t, source, "NEW-1", "S-NEW", base.Add(time.Hour))
		seedRealMLWHTrackingRow(t, source, "NEW-2", "S-NEW", base.Add(time.Hour))
		seedRealMLWHTrackingRow(t, source, "NEW-3", "S-NEW", base.Add(time.Hour))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when the diff/apply sync runs, then the mirror equals the new snapshot (old rows gone)", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror`), convey.ShouldEqual, 3)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror WHERE id_sample_lims LIKE 'OLD-%'`), convey.ShouldEqual, 0)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror WHERE id_sample_lims LIKE 'NEW-%'`), convey.ShouldEqual, 3)
		})
	})
}

// TestClientSyncSeqOpsTrackingPerSampleSwapIsAtomic covers A5.2's atomicity
// clause: the diff/apply happens inside a single transaction, so a reader can
// never observe a partial table (a row count between the old and new snapshots).
//
// It proves this two ways. First, deterministically via WAL snapshot isolation: a
// read transaction opened on the independent read-only connection BEFORE apply
// keeps seeing the whole old snapshot (2) for its entire life even after apply
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
		_, swapErr := writeSeqOpsTrackingPerSampleDiffApply(context.Background(), cache, newRows, refreshTime)
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
	seedTrackingMirrorRowsForTest(t, db, []seqOpsTrackingPerSampleSyncRow{row})
}

func TestClientSyncSeqOpsTrackingPerSampleDiffApplyMixedChanges(t *testing.T) {
	convey.Convey("D1.1: Given tracking mirror rows A/B/C/D and a source snapshot with unchanged A, changed B, and new E", t, func() {
		base := time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
		rowA := newTrackingSyncRowForTest("A", "study-A", base)
		rowB := newTrackingSyncRowForTest("B", "study-B", base)
		rowC := newTrackingSyncRowForTest("C", "study-C", base)
		rowD := newTrackingSyncRowForTest("D", "study-D", base)
		changedB := newTrackingSyncRowForTest("B", "study-B", base.Add(time.Hour))
		rowE := newTrackingSyncRowForTest("E", "study-E", base.Add(2*time.Hour))

		source := openRealMLWHSchemaSource(t)
		seedTrackingSourceRowsForTest(t, source, []seqOpsTrackingPerSampleSyncRow{rowE, changedB, rowA})
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedTrackingMirrorRowsForTest(t, cache.DB(), []seqOpsTrackingPerSampleSyncRow{rowA, rowB, rowC, rowD})
		seedSyncState(t, cache.DB(), syncTableSeqOpsTrackingPerSample, base.Add(-time.Hour))
		observer.Reset()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when synced, then only E inserts and B updates while C/D disappear and every surviving column matches the snapshot", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)
			convey.So(reports[0].Updated, convey.ShouldEqual, 1)
			convey.So(readTrackingMirrorRowsForTest(t, cacheReadDB(cache)), convey.ShouldResemble, []seqOpsTrackingPerSampleSyncRow{rowA, changedB, rowE})

			mutations := filterRecordedStatements(observer.Statements(), isTrackingMirrorMutationForTest)
			convey.So(mutations, convey.ShouldHaveLength, 4)
		})
	})
}

func TestClientSyncSeqOpsTrackingPerSampleDiffApplyUsesOneTransaction(t *testing.T) {
	convey.Convey("D1.2: Given a tracking diff containing an insert, update, and deletes", t, func() {
		base := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
		rowA := newTrackingSyncRowForTest("A", "study-A", base)
		rowB := newTrackingSyncRowForTest("B", "study-B", base)
		changedB := newTrackingSyncRowForTest("B", "study-B", base.Add(time.Hour))

		source := openRealMLWHSchemaSource(t)
		seedTrackingSourceRowsForTest(t, source, []seqOpsTrackingPerSampleSyncRow{rowA, changedB, newTrackingSyncRowForTest("E", "study-E", base)})
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedTrackingMirrorRowsForTest(t, cache.DB(), []seqOpsTrackingPerSampleSyncRow{
			rowA,
			rowB,
			newTrackingSyncRowForTest("C", "study-C", base),
			newTrackingSyncRowForTest("D", "study-D", base),
		})
		observer.Reset()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when synced, then the ordered diff-read is fully drained before all changes and sync_state commit in exactly one transaction", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(observer.BeginCount(), convey.ShouldEqual, 1)
			convey.So(observer.CommitCount(), convey.ShouldEqual, 1)
			convey.So(observer.WriteWhileTrackingReadCount(), convey.ShouldEqual, 0)
			convey.So(filterRecordedStatements(observer.Statements(), isTrackingMirrorMutationForTest), convey.ShouldHaveLength, 4)
		})
	})
}

func TestClientSyncSeqOpsTrackingPerSampleNullMilestonesCompareEqual(t *testing.T) {
	convey.Convey("D1.3: Given equal tracking rows whose manifest and other milestones are NULL on both sides", t, func() {
		row := newTrackingSyncRowForTest("NULL-MILESTONES", "study-null", time.Time{})
		row.ManifestCreated = sql.NullString{}

		source := openRealMLWHSchemaSource(t)
		seedTrackingSourceRowsForTest(t, source, []seqOpsTrackingPerSampleSyncRow{row})
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedTrackingMirrorRowsForTest(t, cache.DB(), []seqOpsTrackingPerSampleSyncRow{row})
		observer.Reset()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when synced, then NULL-vs-NULL equality produces no update", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(filterRecordedStatements(observer.Statements(), isTrackingMirrorMutationForTest), convey.ShouldBeEmpty)
		})
	})
}

func TestClientSyncSeqOpsTrackingPerSampleCaseVariantsUseByteOrder(t *testing.T) {
	convey.Convey("D1.4: Given equal tracking rows with case-variant ids whose NOCASE and byte-wise orders differ", t, func() {
		base := time.Date(2026, time.July, 1, 11, 0, 0, 0, time.UTC)
		rows := []seqOpsTrackingPerSampleSyncRow{
			newTrackingSyncRowForTest("A", "study-A", base),
			newTrackingSyncRowForTest("a", "study-a", base),
			newTrackingSyncRowForTest("B", "study-B", base),
			newTrackingSyncRowForTest("b", "study-b", base),
		}

		source := openRealMLWHSchemaSource(t)
		seedTrackingSourceRowsForTest(t, source, rows)
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedTrackingMirrorRowsForTest(t, cache.DB(), rows)
		observer.Reset()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when synced, then the equal byte-ordered sets need no writes and remain unchanged", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(filterRecordedStatements(observer.Statements(), isTrackingMirrorMutationForTest), convey.ShouldBeEmpty)
			convey.So(readTrackingMirrorRowsForTest(t, cacheReadDB(cache)), convey.ShouldResemble, []seqOpsTrackingPerSampleSyncRow{rows[0], rows[2], rows[1], rows[3]})
		})
	})
}

func TestClientSyncSeqOpsTrackingPerSampleCaseVariantWritesUseIndexedExactMatch(t *testing.T) {
	convey.Convey("Given tracking rows whose ids differ only by case, with one update and one deletion", t, func() {
		base := time.Date(2026, time.July, 11, 9, 0, 0, 0, time.UTC)
		upper := newTrackingSyncRowForTest("CASEPROBE", "study-upper", base)
		mixed := newTrackingSyncRowForTest("CaseProbe", "study-mixed", base)
		lower := newTrackingSyncRowForTest("caseprobe", "study-lower", base)
		changedMixed := newTrackingSyncRowForTest("CaseProbe", "study-mixed", base.Add(time.Hour))

		source := openRealMLWHSchemaSource(t)
		seedTrackingSourceRowsForTest(t, source, []seqOpsTrackingPerSampleSyncRow{changedMixed, lower})
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedTrackingMirrorRowsForTest(t, cache.DB(), []seqOpsTrackingPerSampleSyncRow{upper, mixed, lower})
		observer.Reset()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when synced, then each write narrows by indexed equality before exact binary matching and does not touch the other case variant", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Updated, convey.ShouldEqual, 1)
			convey.So(readTrackingMirrorRowsForTest(t, cacheReadDB(cache)), convey.ShouldResemble, []seqOpsTrackingPerSampleSyncRow{changedMixed, lower})

			mutations := filterRecordedStatements(observer.Statements(), isTrackingMirrorMutationForTest)
			convey.So(mutations, convey.ShouldHaveLength, 2)
			convey.So(normalizeSQL(mutations[0].Query), convey.ShouldContainSubstring,
				"WHERE id_sample_lims = ? AND id_sample_lims COLLATE BINARY = ?")
			convey.So(namedValueOrdinals(mutations[0].Args[len(mutations[0].Args)-2:]), convey.ShouldResemble,
				[]any{"CaseProbe", "CaseProbe"})
			convey.So(normalizeSQL(mutations[1].Query), convey.ShouldEqual, normalizeSQL(
				"DELETE FROM seq_ops_tracking_per_sample_mirror WHERE id_sample_lims = ? AND id_sample_lims COLLATE BINARY = ?"))
			convey.So(namedValueOrdinals(mutations[1].Args), convey.ShouldResemble, []any{"CASEPROBE", "CASEPROBE"})
		})
	})
}

func TestClientSyncSeqOpsTrackingPerSampleUnchangedWritesOnlySyncState(t *testing.T) {
	convey.Convey("D2.1: Given an unchanged tracking mirror/source snapshot and an old refresh watermark", t, func() {
		oldRefresh := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)
		rows := []seqOpsTrackingPerSampleSyncRow{
			newTrackingSyncRowForTest("same-1", "study-1", oldRefresh),
			newTrackingSyncRowForTest("same-2", "study-2", oldRefresh),
		}

		source := openRealMLWHSchemaSource(t)
		seedTrackingSourceRowsForTest(t, source, rows)
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedTrackingMirrorRowsForTest(t, cache.DB(), rows)
		seedSyncState(t, cache.DB(), syncTableSeqOpsTrackingPerSample, oldRefresh)
		observer.Reset()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when synced, then only sync_state is written in one commit while high_water and last_run advance", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(filterRecordedStatements(observer.Statements(), isTrackingMirrorMutationForTest), convey.ShouldBeEmpty)
			convey.So(filterRecordedStatements(observer.Statements(), isSyncStateWriteForTest), convey.ShouldHaveLength, 1)
			convey.So(observer.BeginCount(), convey.ShouldEqual, 1)
			convey.So(observer.CommitCount(), convey.ShouldEqual, 1)
			convey.So(readTrackingMirrorRowsForTest(t, cacheReadDB(cache)), convey.ShouldResemble, rows)
			convey.So(readSyncStateColumnTime(t, cacheReadDB(cache), syncTableSeqOpsTrackingPerSample, "high_water").After(oldRefresh), convey.ShouldBeTrue)
			convey.So(readSyncStateColumnTime(t, cacheReadDB(cache), syncTableSeqOpsTrackingPerSample, "last_run").After(oldRefresh), convey.ShouldBeTrue)
		})
	})
}

func TestClientSyncSeqOpsTrackingPerSampleRecordsOrderedDiffRead(t *testing.T) {
	convey.Convey("D2.2: Given an unchanged tracking snapshot on the recording cache", t, func() {
		row := newTrackingSyncRowForTest("ordered", "study-ordered", time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC))
		source := openRealMLWHSchemaSource(t)
		seedTrackingSourceRowsForTest(t, source, []seqOpsTrackingPerSampleSyncRow{row})
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedTrackingMirrorRowsForTest(t, cache.DB(), []seqOpsTrackingPerSampleSyncRow{row})
		observer.Reset()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)
		mirrorReads := filterRecordedStatements(observer.Statements(), isTrackingMirrorReadForTest)

		convey.Convey("when synced, then exactly one real mirror SELECT is recorded and it explicitly orders by SQLite BINARY collation", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(mirrorReads, convey.ShouldHaveLength, 1)
			convey.So(normalizeSQL(mirrorReads[0].Query), convey.ShouldContainSubstring, "ORDER BY id_sample_lims COLLATE BINARY")
			convey.So(observer.RecordedUnorderedTrackingReadCount(), convey.ShouldEqual, 0)
		})
	})
}

func TestClientSyncSeqOpsTrackingPerSampleStreamsMirrorWithConstantResidency(t *testing.T) {
	convey.Convey("D2.3: Given 10000 equal tracking rows in the source and mirror", t, func() {
		const rowCount = 10000
		base := time.Date(2026, time.July, 1, 13, 0, 0, 0, time.UTC)
		rows := make([]seqOpsTrackingPerSampleSyncRow, 0, rowCount)
		for id := range rowCount {
			rows = append(rows, newTrackingSyncRowForTest(fmt.Sprintf("%05d", id), "study-stream", base))
		}

		source := openRealMLWHSchemaSource(t)
		seedTrackingSourceRowsForTest(t, source, rows)
		cache, observer := openRecordingSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedTrackingMirrorRowsForTest(t, cache.DB(), rows)
		observer.Reset()

		peak := 0
		currentResidency := 0
		acquiredRows := 0
		releasedRows := 0
		withSyncTrackingResidencyHookForTest(t, func(residentMirrorRows int) {
			if residentMirrorRows > peak {
				peak = residentMirrorRows
			}
			if residentMirrorRows > currentResidency {
				acquiredRows += residentMirrorRows - currentResidency
			} else {
				releasedRows += currentResidency - residentMirrorRows
			}
			currentResidency = residentMirrorRows
		})
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when the diff traverses the mirror, then every acquired row is released and its true mirror-side resident peak is exactly one row", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(acquiredRows, convey.ShouldEqual, rowCount)
			convey.So(releasedRows, convey.ShouldEqual, rowCount)
			convey.So(currentResidency, convey.ShouldEqual, 0)
			convey.So(peak, convey.ShouldEqual, 1)
		})
	})
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

func seedTrackingSourceRowsForTest(t *testing.T, db *sql.DB, rows []seqOpsTrackingPerSampleSyncRow) {
	t.Helper()

	seedTrackingRowsIntoTableForTest(t, db, "seq_ops_tracking_per_sample", rows)
}

func seedTrackingMirrorRowsForTest(t *testing.T, db *sql.DB, rows []seqOpsTrackingPerSampleSyncRow) {
	t.Helper()

	seedTrackingRowsIntoTableForTest(t, db, "seq_ops_tracking_per_sample_mirror", rows)
}

func seedTrackingRowsIntoTableForTest(t *testing.T, db *sql.DB, table string, rows []seqOpsTrackingPerSampleSyncRow) {
	t.Helper()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("seedTrackingRowsIntoTableForTest begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(buildBulkInsertStatement(table, seqOpsTrackingPerSampleMirrorColumns, 1))
	if err != nil {
		t.Fatalf("seedTrackingRowsIntoTableForTest prepare: %v", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, row := range rows {
		if _, err = stmt.Exec(seqOpsTrackingPerSampleMirrorRowArgs(row)...); err != nil {
			t.Fatalf("seedTrackingRowsIntoTableForTest exec: %v", err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("seedTrackingRowsIntoTableForTest commit: %v", err)
	}
}

func withSyncTrackingResidencyHookForTest(t *testing.T, hook func(int)) {
	t.Helper()

	original := syncTrackingMirrorResidencyHook
	syncTrackingMirrorResidencyHook = hook
	t.Cleanup(func() { syncTrackingMirrorResidencyHook = original })
}

func readTrackingMirrorRowsForTest(t *testing.T, db *sql.DB) []seqOpsTrackingPerSampleSyncRow {
	t.Helper()

	rows, err := db.Query(`SELECT ` + strings.Join(seqOpsTrackingPerSampleMirrorColumns, ", ") + ` FROM seq_ops_tracking_per_sample_mirror ORDER BY id_sample_lims COLLATE BINARY`)
	if err != nil {
		t.Fatalf("readTrackingMirrorRowsForTest query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]seqOpsTrackingPerSampleSyncRow, 0)
	for rows.Next() {
		row, scanErr := scanSeqOpsTrackingPerSampleSyncRow(rows)
		if scanErr != nil {
			t.Fatalf("readTrackingMirrorRowsForTest scan: %v", scanErr)
		}

		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("readTrackingMirrorRowsForTest rows: %v", err)
	}

	return result
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
