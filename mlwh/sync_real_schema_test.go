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
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	_ "modernc.org/sqlite"
)

type warmSyncVariant int

const (
	warmSyncIncremental warmSyncVariant = iota
	warmSyncFromCursor
)

func TestE2WarmReshapedTablesUseLegacyQueriesWhenJSONTableIsUnsupported(t *testing.T) {
	convey.Convey("E2.2: Given changed iRODS rows and a changed multi-component metrics candidate on a source without JSON_TABLE", t, func() {
		sourceDB := openRealMLWHSchemaSource(t)
		seedB1IseqProductMetricsParityFixture(t, sourceDB)
		base := time.Date(2026, time.July, 2, 10, 0, 0, 0, time.UTC)
		seedRealMLWHIRODSLocationProductRow(t, sourceDB, 3010010, "product-301001", "/b1/direct", "direct.cram", base)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableIseqProductMetrics, warmSyncIncremental)
		seedWarmParitySyncState(t, cache.DB(), syncTableSeqProductIRODSLocations, warmSyncIncremental)
		source := &recordingSource{
			source: sqliteJSONTableSource{db: sourceDB},
			queryError: func(query string) error {
				if strings.Contains(query, "JSON_TABLE") {
					return fmt.Errorf("JSON_TABLE is unsupported")
				}

				return nil
			},
		}
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(
			context.Background(), client, syncTableIseqProductMetrics, syncTableSeqProductIRODSLocations,
		)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 2)
		convey.So([]string{reports[0].Table, reports[1].Table}, convey.ShouldResemble, []string{
			syncTableIseqProductMetrics,
			syncTableSeqProductIRODSLocations,
		})
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "product-301001"), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "product-301001"), convey.ShouldEqual, 1)

		queries := source.Queries()
		convey.So(queries, convey.ShouldHaveLength, 5)
		convey.So(queries[0].Query, convey.ShouldEqual, iseqProductMetricsSyncSourceQuery())
		convey.So(queries[1].Query, convey.ShouldContainSubstring, "JSON_TABLE(path_ipm.iseq_composition_tmp")
		convey.So(queries[1].Args, convey.ShouldContain, "b1-merged")
		convey.So(queries[2].Query, convey.ShouldEqual, iseqProductMetricsLegacySyncSourceQuery())
		convey.So(queries[3].Query, convey.ShouldEqual, seqProductIRODSLocationsSyncSourceQuery())
		convey.So(queries[4].Query, convey.ShouldEqual, seqProductIRODSLocationsLegacySyncSourceQuery())
	})
}

func TestE2WarmClientSyncPopulatesEveryReshapedMirror(t *testing.T) {
	convey.Convey("E2.1: Given one real-schema source with changed rows for every reshaped table and warm sample/study state", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC)
		seedRealMLWHSampleRow(t, source, 401001, "SQSCP", "E2-SAMPLE", "e2-sample-uuid", "e2-sample", "e2-ssid", "e2-supplier", "e2-accession", "e2-donor", 9606, "human", "E2 sample", base)
		seedRealMLWHStudyRow(t, source, 401, "SQSCP", "E2-STUDY", "e2-study-uuid", "E2 Study", "E2-ACCESSION", base)
		seedRealMLWHFlowcellRow(t, source, 40101, "E2-LIBRARY", 401001, 401, base.Add(time.Minute))
		seedRealMLWHProductMetricRow(t, source, 401001, 40101, 64001, 1, 1, 1, 1, 1, base.Add(2*time.Minute))
		seedRealMLWHIRODSLocationProductRow(t, source, 50101, "product-401001", "/e2", "64001.cram", base.Add(3*time.Minute))
		seedRealMLWHIseqRunStatusRow(t, source, 2, 64001, 1, base.Add(4*time.Minute), 1)
		seedRealMLWHIseqRunStatusDictRow(t, source, 1, "run complete", 1)
		seedRealMLWHTrackingRow(t, source, "E2-SAMPLE", "E2-STUDY", base.Add(5*time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		warmHighWater := base.Add(-24 * time.Hour)
		for _, table := range []string{
			syncTableSample,
			syncTableStudy,
			syncTableIseqFlowcell,
			syncTableIseqProductMetrics,
			syncTableSeqProductIRODSLocations,
			syncTableSeqOpsTrackingPerSample,
		} {
			seedSyncState(t, cache.DB(), table, warmHighWater)
		}
		runStatusCursor := encodeAscendingIDResumeCursor(iseqRunStatusIDResumeMode, 1)
		seedSyncStateWithCursor(t, cache.DB(), syncTableIseqRunStatus, time.Time{}, runStatusCursor)

		client := &Client{
			cache:           cache,
			cacheReader:     cacheReadDB(cache),
			syncSource:      sqliteJSONTableSource{db: source},
			disableSyncLock: true,
		}
		reports, err := client.Sync(context.Background())

		convey.So(err, convey.ShouldBeNil)
		reportCounts := make(map[string]int, len(reports))
		for _, report := range reports {
			reportCounts[report.Table]++
		}
		expectedReportCounts := make(map[string]int, len(supportedSyncTables))
		for _, table := range supportedSyncTables {
			expectedReportCounts[table] = 1
		}
		convey.So(reportCounts, convey.ShouldResemble, expectedReportCounts)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_run_status_mirror`), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_ops_tracking_per_sample_mirror`), convey.ShouldEqual, 1)
	})
}

func seedRealMLWHIseqRunStatusDictRow(t *testing.T, db *sql.DB, idRunStatusDict int64, description string, temporalIndex int) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO iseq_run_status_dict(id_run_status_dict, description, temporal_index) VALUES (?, ?, ?)`,
		idRunStatusDict, description, temporalIndex,
	); err != nil {
		t.Fatalf("seedRealMLWHIseqRunStatusDictRow: %v", err)
	}
}

func assertWarmMirrorMatchesCold(t *testing.T, table string, seed func(*sql.DB), variant warmSyncVariant) {
	t.Helper()

	source := openRealMLWHSchemaSource(t)
	seed(source)
	coldCache := openSQLiteSyncTestCache(t)
	defer func() { convey.So(coldCache.Close(), convey.ShouldBeNil) }()
	warmCache := openSQLiteSyncTestCache(t)
	defer func() { convey.So(warmCache.Close(), convey.ShouldBeNil) }()

	coldSource := &recordingSource{source: sqliteJSONTableSource{db: source}}
	coldClient := &Client{cache: coldCache, cacheReader: cacheReadDB(coldCache), syncSource: coldSource, disableSyncLock: true}
	coldReports, err := syncSelectedTablesForTest(context.Background(), coldClient, table)
	convey.So(err, convey.ShouldBeNil)
	convey.So(coldReports, convey.ShouldHaveLength, 1)

	seedWarmParitySyncState(t, warmCache.DB(), table, variant)
	warmSource := &recordingSource{source: sqliteJSONTableSource{db: source}}
	warmClient := &Client{cache: warmCache, cacheReader: cacheReadDB(warmCache), syncSource: warmSource, disableSyncLock: true}
	warmReports, err := syncSelectedTablesForTest(context.Background(), warmClient, table)
	convey.So(err, convey.ShouldBeNil)
	convey.So(warmReports, convey.ShouldHaveLength, 1)
	convey.So(orderedMirrorDump(t, warmCache.DB(), table), convey.ShouldResemble, orderedMirrorDump(t, coldCache.DB(), table))
}

func orderedMirrorDump(t *testing.T, db *sql.DB, table string) []byte {
	t.Helper()

	mirrorTable, orderBy := mirrorDumpQueryParts(t, table)
	rows, err := db.Query(`SELECT * FROM ` + mirrorTable + ` ORDER BY ` + orderBy)
	if err != nil {
		t.Fatalf("ordered mirror dump for %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("ordered mirror dump columns for %s: %v", table, err)
	}
	dumpRows := make([][]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err = rows.Scan(destinations...); err != nil {
			t.Fatalf("ordered mirror dump scan for %s: %v", table, err)
		}
		for i, value := range values {
			if bytes, ok := value.([]byte); ok {
				values[i] = string(bytes)
			}
		}
		dumpRows = append(dumpRows, values)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("ordered mirror dump rows for %s: %v", table, err)
	}

	dump, err := json.Marshal(struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}{Columns: columns, Rows: dumpRows})
	if err != nil {
		t.Fatalf("marshal ordered mirror dump for %s: %v", table, err)
	}

	return dump
}

func mirrorDumpQueryParts(t *testing.T, table string) (string, string) {
	t.Helper()

	switch table {
	case syncTableSeqProductIRODSLocations:
		return "seq_product_irods_locations_mirror", "id_seq_product_irods_locations_tmp, id_sample_tmp, id_study_lims, id_iseq_product"
	case syncTableIseqProductMetrics:
		return "iseq_product_metrics_mirror", "id_iseq_product"
	default:
		t.Fatalf("no ordered mirror dump definition for %q", table)

		return "", ""
	}
}

func seedWarmParitySyncState(t *testing.T, db *sql.DB, table string, variant warmSyncVariant) {
	t.Helper()

	highWater := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	if variant == warmSyncFromCursor {
		seedSyncStateWithCursor(t, db, table, highWater, formatSyncTime(highWater)+"\t0")

		return
	}

	seedSyncState(t, db, table, highWater)
}

type recordedSourceQuery struct {
	Query string
	Args  []any
}

func filterCompositeRecoverySourceQueries(queries []recordedSourceQuery) []recordedSourceQuery {
	filtered := make([]recordedSourceQuery, 0, len(queries))
	for _, query := range queries {
		if strings.Contains(query.Query, "JSON_TABLE(path_ipm.iseq_composition_tmp") {
			filtered = append(filtered, query)
		}
	}

	return filtered
}

func compositeRecoveryCandidateIDs(queries []recordedSourceQuery) map[string]bool {
	candidateIDs := map[string]bool{}
	for _, query := range queries {
		for _, arg := range query.Args {
			candidateIDs[fmt.Sprint(arg)] = true
		}
	}

	return candidateIDs
}

func TestB1WarmIseqProductMetricsRecoversMergedAndFiltersDirectRows(t *testing.T) {
	convey.Convey("B1.3: Given a merged product and direct products without an SQSCP own flowcell", t, func() {
		sourceDB := openRealMLWHSchemaSource(t)
		seedB1IseqProductMetricsParityFixture(t, sourceDB)
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableIseqProductMetrics, warmSyncIncremental)
		source := &recordingSource{source: sqliteJSONTableSource{db: sourceDB}}
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		var position, tagIndex int
		var idSampleTmp int64
		var idStudyLims string
		err = cache.DB().QueryRow(
			`SELECT position, tag_index, id_sample_tmp, id_study_lims FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`,
			"b1-merged",
		).Scan(&position, &tagIndex, &idSampleTmp, &idStudyLims)
		convey.So(err, convey.ShouldBeNil)
		convey.So(position, convey.ShouldEqual, 0)
		convey.So(tagIndex, convey.ShouldEqual, 0)
		convey.So(idSampleTmp, convey.ShouldEqual, 301001)
		convey.So(idStudyLims, convey.ShouldEqual, "b1-sqscp")
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "b1-null-flowcell"), convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "product-301003"), convey.ShouldEqual, 0)
	})
}

func seedB1IseqProductMetricsParityFixture(t *testing.T, db *sql.DB) {
	t.Helper()

	base := time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC)
	seedRealMLWHStudyRow(t, db, 301, "SQSCP", "b1-sqscp", "b1-sqscp-uuid", "B1 SQSCP", "B1-SQSCP", base)
	seedRealMLWHStudyRow(t, db, 302, "OTHER", "b1-other", "b1-other-uuid", "B1 Other", "B1-OTHER", base)
	seedRealMLWHFlowcellRow(t, db, 30101, "B1-1", 301001, 301, base)
	seedRealMLWHFlowcellRow(t, db, 30102, "B1-2", 301002, 301, base)
	seedRealMLWHFlowcellRow(t, db, 30103, "B1-OTHER", 302001, 302, base)
	seedRealMLWHProductMetricRow(t, db, 301001, 30101, 63001, 1, 1, 1, 1, 1, base.Add(time.Minute))
	seedRealMLWHProductMetricRow(t, db, 301002, 30102, 63001, 2, 1, 1, 1, 1, base.Add(2*time.Minute))
	seedRealMLWHProductMetricRow(t, db, 301003, 30103, 63002, 1, 1, 1, 1, 1, base.Add(3*time.Minute))
	seedRealMLWHProductMetricRow(t, db, 301004, 30101, 63003, 1, 1, 1, 1, 1, base.Add(4*time.Minute))
	_, err := db.Exec(
		`UPDATE iseq_product_metrics SET id_iseq_product = ?, id_iseq_flowcell_tmp = NULL WHERE id_iseq_pr_metrics_tmp = ?`,
		"b1-null-flowcell", 301004,
	)
	convey.So(err, convey.ShouldBeNil)
	seedRealMLWHCompositeProductMetricRow(t, db, 301101, "b1-merged", `{"components":[{"id_run":63001,"position":1,"tag_index":1},{"id_run":63001,"position":2,"tag_index":1}]}`, base.Add(5*time.Minute))
	seedRealMLWHCompositeProductMetricRow(t, db, 301102, "b1-no-spi", `{"components":[{"id_run":63001,"position":1,"tag_index":1},{"id_run":63001,"position":2,"tag_index":1}]}`, base.Add(6*time.Minute))
	seedRealMLWHCompositeProductMetricRow(t, db, 301103, "b1-failing-having", `{"components":[{"id_run":63001,"position":1,"tag_index":1},{"id_run":63999,"position":9,"tag_index":9}]}`, base.Add(7*time.Minute))
	seedRealMLWHIRODSLocationProductRow(t, db, 3011010, "b1-merged", "/b1", "merged.cram", base.Add(8*time.Minute))
	seedRealMLWHIRODSLocationProductRow(t, db, 3011030, "b1-failing-having", "/b1", "failing.cram", base.Add(9*time.Minute))
}

func TestB2WarmIseqProductMetricsScopesCompositeRecoveryToChangedCandidates(t *testing.T) {
	convey.Convey("B2.1: Given changed direct rows and multi-component candidates within one recovery chunk", t, func() {
		withSyncCompositeRecoveryChunkSizeForTest(t, 4)
		sourceDB := openRealMLWHSchemaSource(t)
		seedB1IseqProductMetricsParityFixture(t, sourceDB)
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableIseqProductMetrics, warmSyncIncremental)
		source := &recordingSource{source: sqliteJSONTableSource{db: sourceDB}}
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldBeNil)
		recoveryQueries := filterCompositeRecoverySourceQueries(source.Queries())
		convey.So(recoveryQueries, convey.ShouldHaveLength, 1)
		for _, query := range recoveryQueries {
			convey.So(query.Query, convey.ShouldContainSubstring, "path_ipm.id_iseq_product IN ("+sqlPlaceholders(len(query.Args))+")")
			convey.So(query.Query, convey.ShouldNotContainSubstring, "id_iseq_product IN (SELECT")
		}
		convey.So(compositeRecoveryCandidateIDs(recoveryQueries), convey.ShouldResemble, map[string]bool{
			"b1-merged":         true,
			"b1-no-spi":         true,
			"b1-failing-having": true,
		})
	})
}

func TestB2WarmIseqProductMetricsChunksCompositeRecoveryCandidates(t *testing.T) {
	convey.Convey("B2.2: Given more multi-component candidates than the forced recovery chunk size", t, func() {
		const chunkSize = 2
		withSyncCompositeRecoveryChunkSizeForTest(t, chunkSize)
		sourceDB := openRealMLWHSchemaSource(t)
		seedB1IseqProductMetricsParityFixture(t, sourceDB)
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableIseqProductMetrics, warmSyncIncremental)
		source := &recordingSource{source: sqliteJSONTableSource{db: sourceDB}}
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldBeNil)
		recoveryQueries := filterCompositeRecoverySourceQueries(source.Queries())
		convey.So(len(recoveryQueries), convey.ShouldBeGreaterThan, 1)
		for _, query := range recoveryQueries {
			convey.So(len(query.Args), convey.ShouldBeLessThanOrEqualTo, chunkSize)
			convey.So(query.Query, convey.ShouldContainSubstring, "path_ipm.id_iseq_product IN ("+sqlPlaceholders(len(query.Args))+")")
			convey.So(query.Query, convey.ShouldNotContainSubstring, "id_iseq_product IN (SELECT")
		}
		convey.So(compositeRecoveryCandidateIDs(recoveryQueries), convey.ShouldResemble, map[string]bool{
			"b1-merged":         true,
			"b1-no-spi":         true,
			"b1-failing-having": true,
		})
	})
}

func TestB2WarmIseqProductMetricsSkipsCompositeRecoveryWithoutCandidates(t *testing.T) {
	convey.Convey("B2.3: Given changed rows with no multi-component candidates", t, func() {
		sourceDB := openRealMLWHSchemaSource(t)
		seedB1IseqProductMetricsParityFixture(t, sourceDB)
		removeB2CompositeCandidatesForTest(t, sourceDB)
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableIseqProductMetrics, warmSyncIncremental)
		source := &recordingSource{source: sqliteJSONTableSource{db: sourceDB}}
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldBeNil)
		convey.So(filterCompositeRecoverySourceQueries(source.Queries()), convey.ShouldBeEmpty)
	})
}

func removeB2CompositeCandidatesForTest(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`DELETE FROM iseq_product_metrics WHERE id_iseq_product IN (?, ?, ?)`, "b1-merged", "b1-no-spi", "b1-failing-having"); err != nil {
		t.Fatalf("remove B2 composite candidates: %v", err)
	}
}

func TestB1WarmIseqProductMetricsFallsBackWhenChangedRowQueryIsUnsupported(t *testing.T) {
	convey.Convey("B1 fallback: Given the warm changed-row SELECT cannot read iseq_composition_tmp", t, func() {
		sourceDB := openRealMLWHSchemaSource(t)
		seedB1IseqProductMetricsParityFixture(t, sourceDB)
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableIseqProductMetrics, warmSyncIncremental)
		source := &recordingSource{
			source: sqliteJSONTableSource{db: sourceDB},
			queryError: func(query string) error {
				if query == iseqProductMetricsSyncSourceQuery() {
					return fmt.Errorf("no such column: ipm.iseq_composition_tmp")
				}

				return nil
			},
		}
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "product-301001"), convey.ShouldEqual, 1)
		queries := source.Queries()
		convey.So(queries, convey.ShouldHaveLength, 2)
		convey.So(queries[0].Query, convey.ShouldEqual, iseqProductMetricsSyncSourceQuery())
		convey.So(queries[1].Query, convey.ShouldEqual, iseqProductMetricsLegacySyncSourceQuery())
	})
}

func TestB1WarmIseqProductMetricsPropagatesUnrelatedChangedRowQueryErrors(t *testing.T) {
	convey.Convey("B1 fallback: Given the warm changed-row SELECT fails for an unrelated reason", t, func() {
		sourceDB := openRealMLWHSchemaSource(t)
		seedB1IseqProductMetricsParityFixture(t, sourceDB)
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableIseqProductMetrics, warmSyncIncremental)
		source := &recordingSource{
			source: sqliteJSONTableSource{db: sourceDB},
			queryError: func(query string) error {
				if query == iseqProductMetricsSyncSourceQuery() {
					return fmt.Errorf("permission denied")
				}

				return nil
			},
		}
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "permission denied")
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 0)
		convey.So(source.Queries(), convey.ShouldHaveLength, 1)
	})
}

func TestB1WarmIseqProductMetricsFallsBackWhenCompositeRecoveryIsUnsupported(t *testing.T) {
	convey.Convey("B1 fallback: Given changed multi-component candidates and unsupported composite recovery", t, func() {
		sourceDB := openRealMLWHSchemaSource(t)
		seedB1IseqProductMetricsParityFixture(t, sourceDB)
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableIseqProductMetrics, warmSyncIncremental)
		source := &recordingSource{
			source: sqliteJSONTableSource{db: sourceDB},
			queryError: func(query string) error {
				if strings.Contains(query, "JSON_TABLE(path_ipm.iseq_composition_tmp") {
					return fmt.Errorf("JSON_TABLE is unsupported")
				}

				return nil
			},
		}
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "product-301001"), convey.ShouldEqual, 1)
		queries := source.Queries()
		convey.So(queries, convey.ShouldHaveLength, 3)
		convey.So(queries[0].Query, convey.ShouldEqual, iseqProductMetricsSyncSourceQuery())
		convey.So(queries[1].Query, convey.ShouldContainSubstring, "JSON_TABLE(path_ipm.iseq_composition_tmp")
		convey.So(queries[2].Query, convey.ShouldEqual, iseqProductMetricsLegacySyncSourceQuery())
	})
}

func TestA1WarmSeqProductIRODSLocationsKeepsExpansionPlatformPerSourceRow(t *testing.T) {
	convey.Convey("A1.3: Given a multi-row Illumina expansion and a sibling changed row with different source platforms", t, func() {
		assertWarmMirrorMatchesCold(t, syncTableSeqProductIRODSLocations, func(db *sql.DB) {
			seedA1IRODSParityFixture(t, db)
		}, warmSyncIncremental)

		source := openRealMLWHSchemaSource(t)
		seedA1IRODSParityFixture(t, source)
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableSeqProductIRODSLocations, warmSyncIncremental)
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "a1-illumina-merged"), convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ? AND platform = ?`, "a1-illumina-merged", "illumina-merged"), convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ? AND platform = ?`, "product-201003", "illumina-single"), convey.ShouldEqual, 1)
	})
}

func seedA1IRODSParityFixture(t *testing.T, db *sql.DB) {
	t.Helper()

	base := time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC)
	created := base.Add(12 * time.Hour)

	seedRealMLWHStudyRow(t, db, 201, "SQSCP", "a1-study-illumina", "a1-study-illumina-uuid", "A1 Illumina", "A1-ILL", base)
	seedRealMLWHFlowcellRow(t, db, 20101, "A1-IL-1", 201001, 201, base)
	seedRealMLWHFlowcellRow(t, db, 20102, "A1-IL-2", 201002, 201, base)
	seedRealMLWHFlowcellRow(t, db, 20103, "A1-IL-3", 201003, 201, base)
	seedRealMLWHProductMetricRow(t, db, 201001, 20101, 62001, 1, 1, 1, 1, 1, base)
	seedRealMLWHProductMetricRow(t, db, 201002, 20102, 62001, 2, 1, 1, 1, 1, base)
	seedRealMLWHCompositeProductMetricRow(t, db, 201099, "a1-illumina-merged", `{"components":[{"id_run":62001,"position":1,"tag_index":1},{"id_run":62001,"position":2,"tag_index":1}]}`, base)
	seedRealMLWHProductMetricRow(t, db, 201003, 20103, 62002, 1, 1, 1, 1, 1, base)
	seedRealMLWHIRODSLocationPlatformRow(t, db, 30101, "a1-illumina-merged", "illumina-merged", "/a1/illumina/merged", "merged.cram", created, base.Add(time.Minute))
	seedRealMLWHIRODSLocationPlatformRow(t, db, 30102, "product-201003", "illumina-single", "/a1/illumina/single", "single.cram", created, base.Add(2*time.Minute))

	seedRealMLWHStudyRow(t, db, 202, "SQSCP", "a1-study-pacbio", "a1-study-pacbio-uuid", "A1 PacBio", "A1-PB", base)
	seedRealMLWHPacBioRunRow(t, db, 20201, 202001, 202)
	seedRealMLWHPacBioProductMetricRow(t, db, 202001, 20201, "a1-pacbio", base)
	seedRealMLWHIRODSLocationPlatformRow(t, db, 30201, "a1-pacbio", "pacbio", "/a1/pacbio", "pacbio.bam", created, base.Add(3*time.Minute))

	seedRealMLWHStudyRow(t, db, 203, "SQSCP", "a1-study-elembio", "a1-study-elembio-uuid", "A1 Elembio", "A1-EL", base)
	seedRealMLWHEseqFlowcellRow(t, db, 20301, 203001, 203)
	seedRealMLWHEseqProductMetricRow(t, db, "a1-elembio", 20301, 63001, sql.NullInt64{Int64: 1, Valid: true}, sql.NullInt64{Int64: 1, Valid: true}, sql.NullInt64{Int64: 1, Valid: true}, base)
	seedRealMLWHIRODSLocationPlatformRow(t, db, 30301, "a1-elembio", "elembio", "/a1/elembio", "elembio.cram", created, base.Add(4*time.Minute))

	seedRealMLWHStudyRow(t, db, 204, "SQSCP", "a1-study-ultimagen", "a1-study-ultimagen-uuid", "A1 Ultimagen", "A1-UL", base)
	seedRealMLWHUseqWaferRow(t, db, 20401, 204001, 204)
	seedRealMLWHUseqProductMetricRow(t, db, "a1-ultimagen", 20401, 64001, base)
	seedRealMLWHIRODSLocationPlatformRow(t, db, 30401, "a1-ultimagen", "ultimagen", "/a1/ultimagen", "ultimagen.cram", created, base.Add(5*time.Minute))

	seedRealMLWHStudyRow(t, db, 205, "SQSCP", "a1-study-ont", "a1-study-ont-uuid", "A1 ONT", "A1-ONT", base)
	seedRealMLWHOseqFlowcellRow(t, db, 20501, 205001, 205)
	seedRealMLWHIRODSLocationPlatformRow(t, db, 30501, "20501", "ont", "/a1/ont", "ont.fastq.gz", created, base.Add(6*time.Minute))
}

func TestA1WarmSeqProductIRODSLocationsIssuesOneChangedFirstQuery(t *testing.T) {
	convey.Convey("A1.4: Given changed and unchanged Illumina products in one source fixture", t, func() {
		sourceDB := openRealMLWHSchemaSource(t)
		seedA1IRODSQueryShapeFixture(t, sourceDB)
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedWarmParitySyncState(t, cache.DB(), syncTableSeqProductIRODSLocations, warmSyncIncremental)
		source := &recordingSource{source: sqliteJSONTableSource{db: sourceDB}}
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)
		queries := source.Queries()

		convey.So(err, convey.ShouldBeNil)
		convey.So(queries, convey.ShouldHaveLength, 1)
		query := queries[0].Query
		convey.So(query, convey.ShouldContainSubstring, `WITH changed_spi AS (SELECT spi.* FROM seq_product_irods_locations spi WHERE spi.last_changed >= ?)`)
		convey.So(query, convey.ShouldContainSubstring, `FROM changed_spi spi LEFT JOIN`)
		convey.So(query, convey.ShouldContainSubstring, `path_ipm.id_iseq_product IN (SELECT id_product FROM changed_spi)`)
		convey.So(query, convey.ShouldContainSubstring, `pbm.id_pac_bio_product IN (SELECT id_product FROM changed_spi)`)
		convey.So(query, convey.ShouldContainSubstring, `epm.id_eseq_product IN (SELECT id_product FROM changed_spi)`)
		convey.So(query, convey.ShouldContainSubstring, `upm.id_useq_product IN (SELECT id_product FROM changed_spi)`)
		convey.So(query, convey.ShouldContainSubstring, `CAST(ofc.id_oseq_flowcell_tmp AS CHAR) IN (SELECT id_product FROM changed_spi)`)
		convey.So(strings.Count(query, `IN (SELECT id_product FROM changed_spi)`), convey.ShouldEqual, 5)
		convey.So(strings.Count(query, "?"), convey.ShouldEqual, 1)
		convey.So(queries[0].Args, convey.ShouldHaveLength, 1)
	})
}

func seedA1IRODSQueryShapeFixture(t *testing.T, db *sql.DB) {
	t.Helper()

	changed := time.Date(2026, time.July, 2, 10, 0, 0, 0, time.UTC)
	unchanged := time.Date(2026, time.June, 30, 10, 0, 0, 0, time.UTC)
	seedRealMLWHStudyRow(t, db, 211, "SQSCP", "a1-study-shape", "a1-study-shape-uuid", "A1 Shape", "A1-SHAPE", unchanged)
	seedRealMLWHFlowcellRow(t, db, 21101, "A1-SHAPE-CHANGED", 211001, 211, changed)
	seedRealMLWHFlowcellRow(t, db, 21102, "A1-SHAPE-UNCHANGED", 211002, 211, unchanged)
	seedRealMLWHProductMetricRow(t, db, 211001, 21101, 62101, 1, 1, 1, 1, 1, changed)
	seedRealMLWHProductMetricRow(t, db, 211002, 21102, 62101, 2, 1, 1, 1, 1, unchanged)
	seedRealMLWHIRODSLocationPlatformRow(t, db, 31101, "product-211001", "illumina", "/a1/shape", "changed.cram", changed, changed)
	seedRealMLWHIRODSLocationPlatformRow(t, db, 31102, "product-211002", "illumina", "/a1/shape", "unchanged.cram", unchanged, unchanged)
}

func TestClientSyncSeqProductIRODSLocationsRecoversONTIdentityWithNullA4Fields(t *testing.T) {
	convey.Convey("A4.4: Given an ONT iRODS row whose id_product matches oseq_flowcell identity", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC)
		created := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 172, "SQSCP", "17201", "uuid-study-172", "Study ONT", "acc-st-172", base)
		seedRealMLWHOseqFlowcellRow(t, source, 9400, 941, 172)
		seedRealMLWHIRODSLocationPlatformRow(t, source, 94001, "9400", "ont", "/seq/ont/runs/run-9400", "sample.fastq.gz", created, base.Add(time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.Convey("when syncing, then the ONT row is retained with sample/study identity and NULL qc/deliverable", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)

			row := readIRODSLocationMirrorRowForTest(t, cache.DB(), "9400")
			convey.So(row.idSampleTmp, convey.ShouldEqual, 941)
			convey.So(row.idStudyLims, convey.ShouldEqual, "17201")
			convey.So(row.platform, convey.ShouldEqual, "ont")
			convey.So(row.created, convey.ShouldEqual, formatSyncTime(created))

			fields := readA4IRODSExportFieldsForTest(t, cache.DB(), "9400")
			convey.So(fields.qc.Valid, convey.ShouldBeFalse)
			convey.So(fields.isDeliverable.Valid, convey.ShouldBeFalse)
		})
	})
}

func seedRealMLWHOseqFlowcellRow(t *testing.T, db *sql.DB, idOseqFlowcellTmp, idSampleTmp, idStudyTmp int64) {
	t.Helper()

	seedRealMLWHOseqFlowcellRunRow(
		t,
		db,
		idOseqFlowcellTmp,
		idSampleTmp,
		idStudyTmp,
		fmt.Sprintf("ONT-%d", idOseqFlowcellTmp),
		nil,
		fmt.Sprintf("ont-run-uuid-%d", idOseqFlowcellTmp),
		time.Time{},
	)
}

// seedRealMLWHStudyUsersRow inserts a study_users role assignment linked to a
// study via id_study_tmp. login/email/name are passed as *string so a nil
// pointer seeds an upstream NULL (which the wholesale scan COALESCEs to empty string).
func seedRealMLWHStudyUsersRow(t *testing.T, db *sql.DB, idStudyUsersTmp, idStudyTmp int64, role string, login, email, name *string, lastUpdated time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO study_users(id_study_users_tmp, id_study_tmp, last_updated, role, login, email, name) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		idStudyUsersTmp, idStudyTmp, formatSyncTime(lastUpdated), role, login, email, name,
	); err != nil {
		t.Fatalf("seedRealMLWHStudyUsersRow: %v", err)
	}
}

func seedRealMLWHTrackingRow(t *testing.T, db *sql.DB, idSampleLims, studyID string, manifestCreated time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO seq_ops_tracking_per_sample(id_sample_lims, sanger_sample_id, sanger_sample_name, study_id, programme, faculty_sponsor, library_type, platform, manifest_created) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idSampleLims, idSampleLims, idSampleLims+"-name", studyID, "DNA Pipelines", "Sponsor", "Standard", "Illumina", formatSyncTime(manifestCreated),
	); err != nil {
		t.Fatalf("seedRealMLWHTrackingRow: %v", err)
	}
}

type recordingSource struct {
	source     sqliteJSONTableSource
	queryError func(string) error
	queries    []recordedSourceQuery
}

func (source *recordingSource) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	source.queries = append(source.queries, recordedSourceQuery{Query: query, Args: append([]any(nil), args...)})
	if source.queryError != nil {
		if err := source.queryError(query); err != nil {
			return nil, err
		}
	}

	return source.source.QueryContext(ctx, query, args...)
}

func (source *recordingSource) Queries() []recordedSourceQuery {
	return append([]recordedSourceQuery(nil), source.queries...)
}

type irodsLocationMirrorRow struct {
	idSampleTmp int64
	idStudyLims string
	platform    string
	created     string
}

func readIRODSLocationMirrorRowForTest(t *testing.T, db *sql.DB, productID string) irodsLocationMirrorRow {
	t.Helper()

	var row irodsLocationMirrorRow
	if err := db.QueryRow(
		`SELECT id_sample_tmp, id_study_lims, platform, created FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`,
		productID,
	).Scan(&row.idSampleTmp, &row.idStudyLims, &row.platform, &row.created); err != nil {
		t.Fatalf("readIRODSLocationMirrorRowForTest(%q): %v", productID, err)
	}

	return row
}

func seedRealMLWHOseqFlowcellRunRow(t *testing.T, db *sql.DB, idOseqFlowcellTmp, idSampleTmp, idStudyTmp int64, experimentName string, runID any, runUUID string, lastUpdated time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO oseq_flowcell(id_oseq_flowcell_tmp, id_sample_tmp, id_study_tmp, experiment_name, run_id, run_uuid, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		idOseqFlowcellTmp, idSampleTmp, idStudyTmp, experimentName, runID, runUUID, formatSyncTime(lastUpdated),
	); err != nil {
		t.Fatalf("seedRealMLWHOseqFlowcellRow: %v", err)
	}
}

func TestClientSyncSeqProductIRODSLocationsStoresCreatedAndPlatformForIllumina(t *testing.T) {
	convey.Convey("A2.1: Given a source Illumina iRODS row with created and seq_platform_name=illumina", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)
		created := time.Date(2026, time.June, 25, 12, 30, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 70, "SQSCP", "7001", "uuid-study-70", "Study Seventy", "acc-st-70", base)
		seedRealMLWHFlowcellRow(t, source, 7001, "Standard", 701, 70, base.Add(time.Minute))
		seedRealMLWHProductMetricRow(t, source, 70001, 7001, 48000, 1, 1, 1, 1, 1, base.Add(2*time.Minute))
		seedRealMLWHIRODSLocationPlatformRow(t, source, 80001, "product-70001", "illumina", "/seq/illumina/runs/48/48000", "plex1/48000#1.cram", created, base.Add(3*time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.Convey("when the iRODS table syncs, then the mirror row stores the supplied created and platform", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)

			row := readIRODSLocationMirrorRowForTest(t, cache.DB(), "product-70001")
			convey.So(row.idSampleTmp, convey.ShouldEqual, 701)
			convey.So(row.idStudyLims, convey.ShouldEqual, "7001")
			convey.So(row.platform, convey.ShouldEqual, "illumina")
			convey.So(row.created, convey.ShouldEqual, formatSyncTime(created))
		})
	})
}

// seedRealMLWHIRODSLocationPlatformRow seeds a seq_product_irods_locations row
// with an explicit seq_platform_name and created time, so per-platform recovery
// and the carried-through created/platform can be exercised end-to-end.
func seedRealMLWHIRODSLocationPlatformRow(t *testing.T, db *sql.DB, idTmp int64, idProduct, platform, rootCollection, relativePath string, created, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO seq_product_irods_locations(id_seq_product_irods_locations_tmp, created, last_changed, id_product, seq_platform_name, irods_root_collection, irods_data_relative_path, irods_secondary_data_relative_path) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		idTmp,
		formatSyncTime(created),
		formatSyncTime(lastUpdated),
		idProduct,
		platform,
		rootCollection,
		relativePath,
		nil,
	)
	if err != nil {
		t.Fatalf("seedRealMLWHIRODSLocationPlatformRow: %v", err)
	}
}

func TestClientSyncSeqProductIRODSLocationsRecoversPacBioRowFromProductMetrics(t *testing.T) {
	convey.Convey("A2.2: Given a PacBio iRODS row whose id_product matches pac_bio_product_metrics and no Illumina product", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 2, 9, 0, 0, 0, time.UTC)
		created := time.Date(2026, time.June, 26, 8, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 71, "SQSCP", "7101", "uuid-study-71", "Study PacBio", "acc-st-71", base)
		seedRealMLWHPacBioRunRow(t, source, 9100, 911, 71)
		seedRealMLWHPacBioProductMetricRow(t, source, 91001, 9100, "pacbio-product-1", base.Add(time.Minute))
		seedRealMLWHIRODSLocationPlatformRow(t, source, 81001, "pacbio-product-1", "pacbio", "/seq/pacbio/r64/runfolder", "demux/m64.hifi_reads.bam", created, base.Add(2*time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.Convey("when syncing, then a mirror row is written with the PacBio sample/study and platform from seq_platform_name, not dropped", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "pacbio-product-1"), convey.ShouldEqual, 1)

			row := readIRODSLocationMirrorRowForTest(t, cache.DB(), "pacbio-product-1")
			convey.So(row.idSampleTmp, convey.ShouldEqual, 911)
			convey.So(row.idStudyLims, convey.ShouldEqual, "7101")
			convey.So(row.platform, convey.ShouldEqual, "pacbio")
			convey.So(row.created, convey.ShouldEqual, formatSyncTime(created))
		})
	})
}

// TestClientSyncSeqProductIRODSLocationsToleratesNullRelativePath is the hermetic
// regression guard for the real-source bug where seq_product_irods_locations rows
// (e.g. the Ultimagen iRODS rows) carry a NULL irods_data_relative_path, which made
// the sync fail with "converting NULL to string is unsupported". The fixture's
// irods_data_relative_path column is nullable (matching reality) and this row's
// value is NULL, so a sync that did not tolerate it would fail here without a real
// database.
func TestClientSyncSeqProductIRODSLocationsToleratesNullRelativePath(t *testing.T) {
	convey.Convey("Given an Ultimagen iRODS row whose irods_data_relative_path is NULL", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC)
		created := time.Date(2026, time.June, 28, 6, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 73, "SQSCP", "7301", "uuid-study-73", "Study Ultimagen", "acc-st-73", base)
		seedRealMLWHUseqWaferRow(t, source, 9300, 931, 73)
		seedRealMLWHUseqProductMetricRow(t, source, "useq-product-1", 9300, 73001, base.Add(time.Minute))
		seedRealMLWHIRODSLocationNullRelativePathRow(t, source, 83001, "useq-product-1", "ultimagen", "/seq/ultimagen/runs/r1", created, base.Add(2*time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.Convey("when the iRODS table syncs, then the NULL relative path syncs cleanly as an empty path", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)

			var relativePath string
			convey.So(cache.DB().QueryRow(
				`SELECT irods_data_relative_path FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`,
				"useq-product-1",
			).Scan(&relativePath), convey.ShouldBeNil)
			convey.So(relativePath, convey.ShouldEqual, "")

			row := readIRODSLocationMirrorRowForTest(t, cache.DB(), "useq-product-1")
			convey.So(row.idSampleTmp, convey.ShouldEqual, 931)
			convey.So(row.idStudyLims, convey.ShouldEqual, "7301")
			convey.So(row.platform, convey.ShouldEqual, "ultimagen")
		})
	})
}

func TestClientSyncSeqProductIRODSLocationsKeepsDuplicatePathsWithDifferentSourceRows(t *testing.T) {
	convey.Convey("Given two source iRODS rows with the same product path and different source row IDs", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 5, 9, 0, 0, 0, time.UTC)
		created := time.Date(2026, time.June, 29, 8, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 74, "SQSCP", "7401", "uuid-study-74", "Study Duplicate Path", "acc-st-74", base)
		seedRealMLWHFlowcellRow(t, source, 7401, "Standard", 741, 74, base.Add(time.Minute))
		seedRealMLWHProductMetricRow(t, source, 74001, 7401, 48100, 1, 1, 1, 1, 1, base.Add(2*time.Minute))
		seedRealMLWHIRODSLocationPlatformRow(t, source, 84001, "product-74001", "illumina", "/seq/illumina/runs/48/48100", "plex1/48100#1.cram", created, base.Add(3*time.Minute))
		seedRealMLWHIRODSLocationPlatformRow(t, source, 84002, "product-74001", "illumina", "/seq/illumina/runs/48/48100", "plex1/48100#1.cram", created.Add(time.Minute), base.Add(4*time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.Convey("when the iRODS table syncs, then both source rows survive even though the path identity matches", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ? AND id_sample_tmp = ? AND id_study_lims = ? AND irods_collection = ? AND irods_file_name = ?`, "product-74001", 741, "7401", "/seq/illumina/runs/48/48100/plex1", "48100#1.cram"), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_seq_product_irods_locations_tmp IN (?, ?)`, 84001, 84002), convey.ShouldEqual, 2)
		})
	})
}

// TestClientSyncSeqOpsTrackingPerSampleToleratesNullContextColumns is the hermetic
// regression guard for the real-source bug where the mlwh_reporting tracking
// table's nullable context columns (library_type, platform, etc.) are NULL, which
// made the full-refresh sync fail with "converting NULL to string is unsupported".
func TestClientSyncSeqOpsTrackingPerSampleToleratesNullContextColumns(t *testing.T) {
	convey.Convey("Given a tracking source row whose context columns are NULL", t, func() {
		source := openRealMLWHSchemaSource(t)
		if _, err := source.Exec(
			`INSERT INTO seq_ops_tracking_per_sample(id_sample_lims, sanger_sample_id, sanger_sample_name, study_id, programme, faculty_sponsor, library_type, platform, manifest_created) VALUES (?, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`,
			"NULLY-1",
		); err != nil {
			t.Fatalf("seed null tracking row: %v", err)
		}

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqOpsTrackingPerSample)

		convey.Convey("when the full-refresh sync runs, then the NULL context columns sync cleanly as empty strings", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)

			var libraryType, platform string
			convey.So(cache.DB().QueryRow(
				`SELECT library_type, platform FROM seq_ops_tracking_per_sample_mirror WHERE id_sample_lims = ?`,
				"NULLY-1",
			).Scan(&libraryType, &platform), convey.ShouldBeNil)
			convey.So(libraryType, convey.ShouldEqual, "")
			convey.So(platform, convey.ShouldEqual, "")
		})
	})
}

func seedRealMLWHUseqWaferRow(t *testing.T, db *sql.DB, idUseqWaferTmp, idSampleTmp, idStudyTmp int64) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO useq_wafer(id_useq_wafer_tmp, id_sample_tmp, id_study_tmp) VALUES (?, ?, ?)`,
		idUseqWaferTmp, idSampleTmp, idStudyTmp,
	); err != nil {
		t.Fatalf("seedRealMLWHUseqWaferRow: %v", err)
	}
}

func seedRealMLWHUseqProductMetricRow(t *testing.T, db *sql.DB, idProduct string, idWaferTmp, idRun int64, lastChanged time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO useq_product_metrics(id_useq_pr_metrics_tmp, id_useq_wafer_tmp, id_run, id_useq_product, qc, qc_seq, qc_lib, last_changed) VALUES ((SELECT COALESCE(MAX(id_useq_pr_metrics_tmp), 0) + 1 FROM useq_product_metrics), ?, ?, ?, ?, ?, ?, ?)`,
		idWaferTmp, idRun, idProduct, nil, nil, nil, formatSyncTime(lastChanged),
	); err != nil {
		t.Fatalf("seedRealMLWHUseqProductMetricRow: %v", err)
	}
}

// seedRealMLWHIRODSLocationNullRelativePathRow seeds an iRODS row whose
// irods_data_relative_path is NULL, exactly like the real Ultimagen rows.
func seedRealMLWHIRODSLocationNullRelativePathRow(t *testing.T, db *sql.DB, idTmp int64, idProduct, platform, rootCollection string, created, lastUpdated time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO seq_product_irods_locations(id_seq_product_irods_locations_tmp, created, last_changed, id_product, seq_platform_name, irods_root_collection, irods_data_relative_path, irods_secondary_data_relative_path) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		idTmp,
		formatSyncTime(created),
		formatSyncTime(lastUpdated),
		idProduct,
		platform,
		rootCollection,
		nil,
		nil,
	); err != nil {
		t.Fatalf("seedRealMLWHIRODSLocationNullRelativePathRow: %v", err)
	}
}

func seedRealMLWHPacBioRunRow(t *testing.T, db *sql.DB, idPacBioTmp, idSampleTmp, idStudyTmp int64) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO pac_bio_run(id_pac_bio_tmp, id_sample_tmp, id_study_tmp) VALUES (?, ?, ?)`,
		idPacBioTmp, idSampleTmp, idStudyTmp,
	); err != nil {
		t.Fatalf("seedRealMLWHPacBioRunRow: %v", err)
	}
}

func seedRealMLWHPacBioProductMetricRow(t *testing.T, db *sql.DB, idTmp, idPacBioTmp int64, idProduct string, lastChanged time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO pac_bio_product_metrics(id_pac_bio_pr_metrics_tmp, id_pac_bio_rw_metrics_tmp, id_pac_bio_tmp, id_pac_bio_product, qc, last_changed) VALUES (?, ?, ?, ?, ?, ?)`,
		idTmp, idTmp, idPacBioTmp, idProduct, nil, formatSyncTime(lastChanged),
	); err != nil {
		t.Fatalf("seedRealMLWHPacBioProductMetricRow: %v", err)
	}
}

func TestClientSyncSeqProductIRODSLocationsPlatformComesFromSeqPlatformNameNotMatchedTable(t *testing.T) {
	convey.Convey("A2.3: Given a row whose seq_platform_name is pacbio but that also matches an Illumina product", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.June, 3, 9, 0, 0, 0, time.UTC)
		created := time.Date(2026, time.June, 27, 7, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 72, "SQSCP", "7201", "uuid-study-72", "Study Shared", "acc-st-72", base)
		seedRealMLWHFlowcellRow(t, source, 7200, "Standard", 721, 72, base.Add(time.Minute))
		// An Illumina product whose id_iseq_product equals the iRODS row's id_product.
		seedRealMLWHProductMetricRow(t, source, 72001, 7200, 48100, 1, 1, 1, 1, 1, base.Add(2*time.Minute))
		seedRealMLWHIRODSLocationPlatformRow(t, source, 82001, "product-72001", "pacbio", "/seq/illumina/runs/48/48100", "plex1/48100#1.cram", created, base.Add(3*time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.Convey("when syncing, then platform is pacbio from seq_platform_name, proving platform is not derived from the matched table", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)

			row := readIRODSLocationMirrorRowForTest(t, cache.DB(), "product-72001")
			convey.So(row.platform, convey.ShouldEqual, "pacbio")
			// The Illumina recovery branch still supplies sample/study (it is the
			// table that matched), proving platform and linkage are independent.
			convey.So(row.idSampleTmp, convey.ShouldEqual, 721)
			convey.So(row.idStudyLims, convey.ShouldEqual, "7201")
		})
	})
}

// TestSyncAgainstRealMLWHSchema exercises the full Client.Sync path against an
// upstream "MLWH" SQLite database whose table shapes faithfully match the real
// Sanger MLWH columns (in particular: the upstream `sample` table has NO
// `id_study_lims` column; `iseq_flowcell` links to `study` via
// `id_study_tmp` rather than carrying `id_study_lims`; product metric
// watermarks are `last_changed`; iRODS locations key products through
// `id_product`; and the study table uses the current
// `ega_dac_accession_number` spelling).
// This regression test would have caught the production bug where
// `wa mlwh sync --env development` failed with
// `Unknown column 'id_study_lims' in 'field list'` because the previous sync
// query selected `id_study_lims` from `sample` directly.
func TestSyncAgainstRealMLWHSchema(t *testing.T) {
	convey.Convey("Given an upstream database with the real MLWH schema (no id_study_lims on sample)", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHSampleRow(t, source, 1, "SQSCP", "1001", "uuid-sample-1", "sample-a", "ssid-a", "supplier-a", "acc-sa", "donor-a", 9606, "human", "desc-a", time.Date(2026, time.May, 7, 9, 0, 0, 0, time.UTC))
		seedRealMLWHSampleRow(t, source, 2, "SQSCP", "1002", "uuid-sample-2", "sample-b", "ssid-b", "supplier-b", "acc-sb", "donor-b", 9606, "human", "desc-b", time.Date(2026, time.May, 7, 9, 5, 0, 0, time.UTC))
		seedRealMLWHStudyRow(t, source, 10, "SQSCP", "5001", "uuid-study-1", "Study One", "acc-st-1", time.Date(2026, time.May, 7, 8, 0, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 100, "Standard", 1, 10, time.Date(2026, time.May, 7, 9, 10, 0, 0, time.UTC))
		seedRealMLWHProductMetricRow(t, source, 1001, 100, 9001, 1, 1, 1, 1, 1, time.Date(2026, time.May, 7, 9, 15, 0, 0, time.UTC))
		seedRealMLWHIRODSLocationRow(t, source, 1001, "/seq", "run/1", "/seq/run", "file.cram", time.Date(2026, time.May, 7, 9, 20, 0, 0, time.UTC))
		// sample 2 deliberately has no flowcell entry, so it produces no
		// library_samples study mapping while the rest of the sync still succeeds.

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := client.Sync(context.Background())

		convey.Convey("when Sync runs without restricting tables, then every table is synced and the study mapping is stored via library_samples", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, len(supportedSyncTables))

			byTable := make(map[string]SyncReport, len(reports))
			for _, report := range reports {
				byTable[report.Table] = report
			}
			convey.So(byTable["sample"].Inserted, convey.ShouldEqual, 2)
			convey.So(byTable["study"].Inserted, convey.ShouldEqual, 1)
			convey.So(byTable["iseq_flowcell"].Inserted, convey.ShouldEqual, 1)
			convey.So(byTable["iseq_product_metrics"].Inserted, convey.ShouldEqual, 1)
			convey.So(byTable["seq_product_irods_locations"].Inserted, convey.ShouldEqual, 1)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 1)

			paths, pathErr := client.IRODSPathsForSample(context.Background(), "sample-a", 100, 0)
			convey.So(pathErr, convey.ShouldBeNil)
			convey.So(paths, convey.ShouldResemble, []IRODSPath{{
				IDProduct:            "product-1001",
				Collection:           "/seq/run",
				DataObject:           "1",
				IRODSPath:            "/seq/run/1",
				IDSampleTmp:          1,
				Name:                 "sample-a",
				SupplierName:         "supplier-a",
				SangerSampleID:       "ssid-a",
				AccessionNumber:      "acc-sa",
				IDStudyLims:          "5001",
				StudyAccessionNumber: "acc-st-1",
				Created:              "2026-05-07T09:20:00Z",
				IDRun:                9001,
				Position:             1,
				TagIndex:             1,
				Platform:             "Illumina",
				ManualQC:             "pass",
				Deliverable:          boolPtr(true),
			}})

			var studyLimsForSample1 string
			convey.So(cache.DB().QueryRow(`SELECT id_study_lims FROM library_samples WHERE id_sample_tmp = 1`).Scan(&studyLimsForSample1), convey.ShouldBeNil)
			convey.So(studyLimsForSample1, convey.ShouldEqual, "5001")
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples WHERE id_sample_tmp = 2`), convey.ShouldEqual, 0)
		})
	})
}

func TestClientSyncSeqProductIRODSLocationsRealSourceExpandsCompositeProducts(t *testing.T) {
	convey.Convey("Given an upstream composite product iRODS location whose component products link to two samples", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.May, 13, 9, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 50, "SQSCP", "7607", "uuid-study-50", "Study Fifty", "acc-st-50", base)
		seedRealMLWHFlowcellRow(t, source, 5001, "Custom", 9419243, 50, base.Add(time.Minute))
		seedRealMLWHFlowcellRow(t, source, 5002, "Custom", 9419244, 50, base.Add(2*time.Minute))
		seedRealMLWHProductMetricRow(t, source, 6001, 5001, 48522, 1, 1, 1, 1, 1, base.Add(3*time.Minute))
		seedRealMLWHProductMetricRow(t, source, 6002, 5002, 48522, 2, 1, 1, 1, 1, base.Add(4*time.Minute))
		seedRealMLWHCompositeProductMetricRow(t, source, 7001, "composite-product", `{"components":[{"id_run":48522,"position":1,"tag_index":1},{"id_run":48522,"position":2,"tag_index":1}]}`, base.Add(5*time.Minute))
		seedRealMLWHIRODSLocationProductRow(t, source, 8001, "composite-product", "/seq/illumina/runs/48/48522", "plex1/48522#1.cram", base.Add(6*time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "composite-product"), convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ? AND id_sample_tmp = ? AND id_study_lims = ?`, "composite-product", 9419243, "7607"), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ? AND id_sample_tmp = ? AND id_study_lims = ?`, "composite-product", 9419244, "7607"), convey.ShouldEqual, 1)
		convey.So(locationMirrorFileForTest(t, cache.DB(), "composite-product"), convey.ShouldEqual, "48522#1.cram")
	})
}

func TestB1WarmIseqProductMetricsIncrementalMatchesCold(t *testing.T) {
	convey.Convey("B1.1: Given direct, filtered, merged, and unrecoverable changed product metrics", t, func() {
		assertWarmMirrorMatchesCold(t, syncTableIseqProductMetrics, func(db *sql.DB) {
			seedB1IseqProductMetricsParityFixture(t, db)
		}, warmSyncIncremental)
	})
}

func TestB1WarmIseqProductMetricsFromCursorMatchesCold(t *testing.T) {
	convey.Convey("B1.2: Given the mixed changed product-metrics fixture and a resume cursor", t, func() {
		assertWarmMirrorMatchesCold(t, syncTableIseqProductMetrics, func(db *sql.DB) {
			seedB1IseqProductMetricsParityFixture(t, db)
		}, warmSyncFromCursor)
	})
}

func TestA1WarmSeqProductIRODSLocationsIncrementalMatchesCold(t *testing.T) {
	convey.Convey("A1.1: Given every recoverable iRODS platform branch after an early watermark", t, func() {
		assertWarmMirrorMatchesCold(t, syncTableSeqProductIRODSLocations, func(db *sql.DB) {
			seedA1IRODSParityFixture(t, db)
		}, warmSyncIncremental)
	})
}

func TestA1WarmSeqProductIRODSLocationsFromCursorMatchesCold(t *testing.T) {
	convey.Convey("A1.2: Given every recoverable iRODS platform branch and a forced warm resume cursor", t, func() {
		assertWarmMirrorMatchesCold(t, syncTableSeqProductIRODSLocations, func(db *sql.DB) {
			seedA1IRODSParityFixture(t, db)
		}, warmSyncFromCursor)
	})
}

// TestAllStudiesAgainstRealMLWHSchema verifies that the synced-cache study list
// path tolerates the current upstream study column spelling used by MLWH.
func TestAllStudiesAgainstRealMLWHSchema(t *testing.T) {
	convey.Convey("Given a synced cache and an upstream database with the current MLWH study schema", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHStudyRow(t, source, 10, "SQSCP", "5001", "uuid-study-1", "Study One", "acc-st-1", time.Date(2026, time.May, 7, 8, 0, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableStudy)
		convey.So(err, convey.ShouldBeNil)

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then it returns the synced study from study_mirror", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 1)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "5001")
			convey.So(studies[0].EGADACAccessionNumber, convey.ShouldEqual, "")
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 1)
		})
	})
}

func TestAllStudiesAgainstRealMLWHSchemaAllowsNullStudyStrings(t *testing.T) {
	convey.Convey("Given a synced cache and an upstream study row with nullable text fields", t, func() {
		source := openRealMLWHSchemaSource(t)
		_, err := source.Exec(
			`INSERT INTO study(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			11,
			"SQSCP",
			"5002",
			"uuid-study-2",
			"Study Two",
			nil,
			"Study title 2",
			nil,
			"active",
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			true,
			nil,
			false,
			false,
			nil,
			nil,
			nil,
			nil,
			formatSyncTime(time.Date(2026, time.May, 7, 8, 10, 0, 0, time.UTC)),
		)
		convey.So(err, convey.ShouldBeNil)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}
		_, err = syncSelectedTablesForTest(context.Background(), client, syncTableStudy)
		convey.So(err, convey.ShouldBeNil)

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then nullable upstream strings are normalized to empty strings after sync", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 1)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "5002")
			convey.So(studies[0].AccessionNumber, convey.ShouldEqual, "")
			convey.So(studies[0].FacultySponsor, convey.ShouldEqual, "")
			convey.So(studies[0].EGADACAccessionNumber, convey.ShouldEqual, "")
		})
	})
}

// TestSampleResolverAgainstRealMLWHSchema verifies that syncing sample_mirror
// from the real MLWH schema supports later cache-only resolution, even though
// the upstream `sample` table has no `id_study_lims` column.
func TestSampleResolverAgainstRealMLWHSchema(t *testing.T) {
	convey.Convey("Given a synced cache and an upstream with the real MLWH schema", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHSampleRow(t, source, 5, "SQSCP", "5005", "b7daafb8-c59f-11ee-8fba-024224dd57f4", "name-5", "ssid-5", "supplier-5", "acc-5", "donor-5", 9606, "human", "desc-5", time.Date(2026, time.May, 7, 10, 0, 0, 0, time.UTC))
		seedRealMLWHStudyRow(t, source, 99, "SQSCP", "9999", "uuid-study-99", "Study Ninety Nine", "acc-st-99", time.Date(2026, time.May, 7, 8, 30, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 500, "Standard", 5, 99, time.Date(2026, time.May, 7, 10, 5, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)
		convey.So(err, convey.ShouldBeNil)

		match, err := client.ResolveSample(context.Background(), "b7daafb8-c59f-11ee-8fba-024224dd57f4")

		convey.Convey("when ResolveSample runs against the synced cache, then it returns the sample without relying on a removed sample.id_study_lims column", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(match.Kind, convey.ShouldEqual, KindSampleUUID)
			convey.So(match.Sample, convey.ShouldNotBeNil)
			convey.So(match.Sample.UUIDSampleLims, convey.ShouldEqual, "b7daafb8-c59f-11ee-8fba-024224dd57f4")
			convey.So(match.Sample.Studies, convey.ShouldBeEmpty)
			convey.So(match.Sample.Libraries, convey.ShouldBeEmpty)
		})
	})
}

func TestClientSyncIseqFlowcellRealSourceSkipsNonSQSCPStudyRows(t *testing.T) {
	convey.Convey("B7.1: Given a real-source iseq_flowcell row linked to a non-SQSCP study", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHStudyRow(t, source, 20, "GCLP", "6001", "uuid-study-20", "Non SQSCP Study", "acc-st-20", time.Date(2026, time.May, 7, 8, 0, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 200, "Standard", 21, 20, time.Date(2026, time.May, 7, 9, 0, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqFlowcell)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
		convey.So(reports[0].Updated, convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 0)
	})
}

func TestClientSyncProductTablesRealSourceSkipRowsWithoutSQSCPStudy(t *testing.T) {
	convey.Convey("B7.2: Given real-source product rows linked through a non-SQSCP study", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHStudyRow(t, source, 30, "GCLP", "7001", "uuid-study-30", "Non SQSCP Study", "acc-st-30", time.Date(2026, time.May, 7, 8, 5, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 300, "Standard", 31, 30, time.Date(2026, time.May, 7, 9, 5, 0, 0, time.UTC))
		seedRealMLWHProductMetricRow(t, source, 3001, 300, 9001, 1, 1, 1, 1, 1, time.Date(2026, time.May, 7, 9, 10, 0, 0, time.UTC))
		seedRealMLWHIRODSLocationRow(t, source, 3001, "/seq", "run/3001", "/seq/run/3001", "3001.cram", time.Date(2026, time.May, 7, 9, 15, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 2)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
		convey.So(reports[1].Inserted, convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 0)
	})
}

func TestClientSyncProductMetricsRealSourceNormalizesNullableNumericFields(t *testing.T) {
	convey.Convey("B7.3: Given a real-source product metric row with nullable lane and QC fields", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHStudyRow(t, source, 40, "SQSCP", "8001", "uuid-study-40", "Study Forty", "acc-st-40", time.Date(2026, time.May, 7, 8, 5, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 400, "Standard", 41, 40, time.Date(2026, time.May, 7, 9, 5, 0, 0, time.UTC))
		_, err := source.Exec(
			`INSERT INTO iseq_product_metrics(id_iseq_pr_metrics_tmp, id_iseq_product, last_changed, id_iseq_flowcell_tmp, id_run, position, tag_index, qc, qc_lib, qc_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			4001,
			"product-4001",
			formatSyncTime(time.Date(2026, time.May, 7, 9, 10, 0, 0, time.UTC)),
			400,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
		)
		convey.So(err, convey.ShouldBeNil)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 1)

		// id_run/position/tag_index are NOT NULL mirror columns, so a NULL source
		// value normalizes to 0. The QC columns are NULL-preserving: a NULL source
		// qc stays SQL NULL in the mirror (never coerced to 0) so a downstream read
		// maps it to "pending" rather than a 0 "fail".
		var idRun, position, tagIndex int
		var qc, qcLib, qcSeq sql.NullInt64
		convey.So(cache.DB().QueryRow(`SELECT id_run, position, tag_index, qc, qc_lib, qc_seq FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "product-4001").Scan(&idRun, &position, &tagIndex, &qc, &qcLib, &qcSeq), convey.ShouldBeNil)
		convey.So([]int{idRun, position, tagIndex}, convey.ShouldResemble, []int{0, 0, 0})
		convey.So(qc.Valid, convey.ShouldBeFalse)
		convey.So(qcLib.Valid, convey.ShouldBeFalse)
		convey.So(qcSeq.Valid, convey.ShouldBeFalse)
		convey.So(qcString(qc), convey.ShouldEqual, "pending")
	})
}

func openRealMLWHSchemaSource(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "real-mlwh.sqlite"))
	if err != nil {
		t.Fatalf("open real MLWH source sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// `sample` deliberately has NO id_study_lims column to mirror the real
	// Sanger MLWH schema: studies link to samples via iseq_flowcell (or
	// stock_resource for PacBio etc.), not via a column on sample itself.
	mustExec(t, db, `CREATE TABLE sample (
		id_sample_tmp    INTEGER PRIMARY KEY,
		id_lims          TEXT NOT NULL,
		id_sample_lims   TEXT NOT NULL,
		uuid_sample_lims TEXT NOT NULL,
		name             TEXT NOT NULL,
		sanger_sample_id TEXT NOT NULL,
		supplier_name    TEXT NOT NULL,
		accession_number TEXT NOT NULL,
		donor_id         TEXT NOT NULL,
		taxon_id         INTEGER NOT NULL,
		common_name      TEXT NOT NULL,
		description      TEXT NOT NULL,
		last_updated     TEXT NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE study (
		id_study_tmp                INTEGER PRIMARY KEY,
		id_lims                     TEXT NOT NULL,
		id_study_lims               TEXT NOT NULL,
		uuid_study_lims             TEXT NOT NULL,
		name                        TEXT NOT NULL,
		accession_number            TEXT,
		study_title                 TEXT,
		faculty_sponsor             TEXT,
		state                       TEXT NOT NULL,
		abstract                    TEXT,
		abbreviation                TEXT,
		description                 TEXT,
		data_release_strategy       TEXT,
		data_access_group           TEXT,
		hmdmc_number                TEXT,
		programme                   TEXT,
		created                     TEXT,
		reference_genome            TEXT,
		ethically_approved          INTEGER NOT NULL,
		study_type                  TEXT,
		contains_human_dna          INTEGER NOT NULL,
		contaminated_human_dna      INTEGER NOT NULL,
		study_visibility            TEXT,
		ega_dac_accession_number    TEXT,
		ega_policy_accession_number TEXT,
		data_release_timing         TEXT,
		last_updated                TEXT NOT NULL
	)`)

	// study_users carries per-study role assignments and links to study via
	// id_study_tmp; it is mirrored wholesale. login/email/name are nullable
	// upstream (the wholesale scan COALESCEs NULL to '').
	mustExec(t, db, `CREATE TABLE study_users (
		id_study_users_tmp INTEGER PRIMARY KEY,
		id_study_tmp       INTEGER NOT NULL,
		last_updated       TEXT NOT NULL,
		role               TEXT,
		login              TEXT,
		email              TEXT,
		name               TEXT
	)`)

	mustExec(t, db, `CREATE TABLE iseq_flowcell (
		id_iseq_flowcell_tmp INTEGER PRIMARY KEY,
		entity_type           TEXT NOT NULL,
		pipeline_id_lims     TEXT NOT NULL,
		id_sample_tmp        INTEGER NOT NULL,
		id_study_tmp         INTEGER NOT NULL,
		legacy_library_id    INTEGER,
		id_library_lims      TEXT,
		last_updated         TEXT NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE iseq_product_metrics (
		id_iseq_pr_metrics_tmp INTEGER PRIMARY KEY,
		id_iseq_product        TEXT NOT NULL,
		last_changed           TEXT NOT NULL,
		id_iseq_flowcell_tmp INTEGER,
		id_run               INTEGER,
		position             INTEGER,
		tag_index            INTEGER,
		iseq_composition_tmp TEXT,
		qc                   INTEGER,
		qc_lib               INTEGER,
		qc_seq               INTEGER
	)`)

	// irods_data_relative_path is nullable to match the real MLWH schema: the
	// Ultimagen iRODS rows store NULL there, so a sync that scanned it into a
	// plain string (rather than COALESCEing / NullString) would fail.
	mustExec(t, db, `CREATE TABLE seq_product_irods_locations (
		id_seq_product_irods_locations_tmp INTEGER PRIMARY KEY,
		created                 TEXT,
		last_changed            TEXT NOT NULL,
		id_product              TEXT NOT NULL,
		seq_platform_name       TEXT NOT NULL,
		irods_root_collection    TEXT NOT NULL,
		irods_data_relative_path TEXT,
		irods_secondary_data_relative_path TEXT
	)`)

	// Per-platform linkage tables (faithful subsets of the real MLWH schema)
	// that the iRODS recovery UNION joins through to recover sample/study for
	// PacBio, Elembio and Ultimagen products. Illumina links through
	// iseq_product_metrics/iseq_flowcell above.
	mustExec(t, db, `CREATE TABLE pac_bio_run (
		id_pac_bio_tmp INTEGER PRIMARY KEY,
		id_sample_tmp  INTEGER NOT NULL,
		id_study_tmp   INTEGER NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE pac_bio_product_metrics (
		id_pac_bio_pr_metrics_tmp INTEGER PRIMARY KEY,
		id_pac_bio_rw_metrics_tmp INTEGER,
		id_pac_bio_tmp            INTEGER,
		id_pac_bio_product        TEXT NOT NULL,
		qc                        INTEGER,
		last_changed              TEXT NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE eseq_flowcell (
		id_eseq_flowcell_tmp INTEGER PRIMARY KEY,
		id_sample_tmp        INTEGER NOT NULL,
		id_study_tmp         INTEGER NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE eseq_product_metrics (
		id_eseq_pr_metrics_tmp INTEGER PRIMARY KEY,
		id_eseq_flowcell_tmp   INTEGER,
		id_run                 INTEGER NOT NULL,
		id_eseq_product        TEXT NOT NULL,
		is_sequencing_control  INTEGER,
		qc                     INTEGER,
		qc_seq                 INTEGER,
		qc_lib                 INTEGER,
		last_changed           TEXT NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE useq_wafer (
		id_useq_wafer_tmp INTEGER PRIMARY KEY,
		id_sample_tmp     INTEGER NOT NULL,
		id_study_tmp      INTEGER NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE useq_product_metrics (
		id_useq_pr_metrics_tmp INTEGER PRIMARY KEY,
		id_useq_wafer_tmp      INTEGER,
		id_run                 INTEGER NOT NULL,
		id_useq_product        TEXT NOT NULL,
		is_sequencing_control  INTEGER,
		qc                     INTEGER,
		qc_seq                 INTEGER,
		qc_lib                 INTEGER,
		last_changed           TEXT NOT NULL
	)`)

	// iseq_run_status carries the Illumina NPG run-status transitions keyed on the
	// id_run_status primary key (no last_changed), synced in ascending-id mode.
	mustExec(t, db, `CREATE TABLE iseq_run_status (
		id_run_status      INTEGER PRIMARY KEY,
		id_run             INTEGER NOT NULL,
		date               TEXT    NOT NULL,
		id_run_status_dict INTEGER NOT NULL,
		iscurrent          INTEGER NOT NULL
	)`)

	// iseq_run_status_dict is a small lookup mirrored wholesale (no last_changed).
	mustExec(t, db, `CREATE TABLE iseq_run_status_dict (
		id_run_status_dict INTEGER PRIMARY KEY,
		description        TEXT NOT NULL,
		temporal_index     INTEGER
	)`)

	// oseq_flowcell carries ONT identity and links to study via id_study_tmp; it
	// is mirrored wholesale.
	mustExec(t, db, `CREATE TABLE oseq_flowcell (
		id_oseq_flowcell_tmp INTEGER PRIMARY KEY,
		id_sample_tmp        INTEGER NOT NULL,
		id_study_tmp         INTEGER NOT NULL,
		experiment_name      TEXT NOT NULL,
		run_id               INTEGER,
		run_uuid             TEXT NOT NULL,
		last_updated         TEXT NOT NULL
	)`)

	// Per-run status/date tables mirrored wholesale (small per-platform tables).
	// Their nullable status/date columns and last_changed are faithful subsets of
	// the real MLWH schema.
	mustExec(t, db, `CREATE TABLE pac_bio_run_well_metrics (
		id_pac_bio_rw_metrics_tmp INTEGER PRIMARY KEY,
		pac_bio_run_name          TEXT NOT NULL,
		well_label                TEXT NOT NULL,
		plate_number              INTEGER,
		run_start                 TEXT,
		run_complete              TEXT,
		well_complete             TEXT,
		qc_seq_date               TEXT,
		run_status                TEXT,
		well_status               TEXT,
		last_changed              TEXT
	)`)

	// eseq_run faithfully matches the real MLWH schema: it has NO run_status /
	// run_start / run_complete columns; the run-level lifecycle is run_type,
	// date_started, date_completed and a free-text outcome (and there is no
	// last_changed). A sync query referencing the old names fails here.
	mustExec(t, db, `CREATE TABLE eseq_run (
		id_eseq_run_tmp INTEGER PRIMARY KEY,
		folder_name     TEXT NOT NULL,
		run_name        TEXT NOT NULL,
		flowcell_id     TEXT NOT NULL,
		run_type        TEXT,
		date_started    TEXT,
		date_completed  TEXT,
		run_parameters  TEXT NOT NULL,
		run_manifest    TEXT,
		run_stats       TEXT,
		outcome         TEXT
	)`)

	// eseq_run_lane_metrics faithfully matches the real MLWH schema: its primary
	// key is the composite (id_run, lane) -- there is NO id_eseq_rlm_tmp -- and it
	// has NO run_name; its timeline is the dated run_started / run_complete columns.
	mustExec(t, db, `CREATE TABLE eseq_run_lane_metrics (
		id_run          INTEGER NOT NULL,
		lane            INTEGER NOT NULL,
		run_folder_name TEXT NOT NULL,
		run_started     TEXT,
		run_complete    TEXT,
		last_changed    TEXT,
		PRIMARY KEY (id_run, lane)
	)`)

	// useq_run_metrics faithfully matches the real MLWH schema: its primary key is
	// id_run -- there is NO id_useq_run_metrics_tmp -- and it has NO run_name /
	// run_status / run_start / run_complete columns; the run-level lifecycle is the
	// dated run_in_progress (start) and run_archived columns.
	mustExec(t, db, `CREATE TABLE useq_run_metrics (
		id_run          INTEGER PRIMARY KEY,
		run_folder_name TEXT NOT NULL,
		run_in_progress TEXT,
		run_archived    TEXT,
		last_changed    TEXT
	)`)

	// seq_ops_tracking_per_sample mutates in place and has no last_changed, so it
	// is mirrored by full-table refresh with an atomic swap.
	// Only id_sample_lims is NOT NULL upstream; the other context/lookup string
	// columns are nullable in the real mlwh_reporting table (e.g. library_type and
	// platform are frequently NULL), so the fixture leaves them nullable to match.
	mustExec(t, db, `CREATE TABLE seq_ops_tracking_per_sample (
		id_sample_lims         TEXT NOT NULL,
		sanger_sample_id       TEXT,
		sanger_sample_name     TEXT,
		study_id               TEXT,
		programme              TEXT,
		faculty_sponsor        TEXT,
		library_type           TEXT,
		platform               TEXT,
		manifest_created       TEXT,
		manifest_uploaded      TEXT,
		labware_received       TEXT,
		order_made             TEXT,
		working_dilution       TEXT,
		library_start          TEXT,
		library_complete       TEXT,
		sequencing_run_start   TEXT,
		sequencing_qc_complete  TEXT
	)`)

	return db
}

func seedRealMLWHSampleRow(t *testing.T, db *sql.DB, idTmp int64, idLims, idSampleLims, uuidLims, name, sangerSampleID, supplier, accession, donor string, taxon int, commonName, description string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idTmp, idLims, idSampleLims, uuidLims, name, sangerSampleID, supplier, accession, donor, taxon, commonName, description, formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedRealMLWHSampleRow: %v", err)
	}
}

func seedRealMLWHStudyRow(t *testing.T, db *sql.DB, idTmp int64, idLims, idStudyLims, uuidLims, name, accession string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO study(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, '', '', 'active', '', '', '', '', '', '', '', '', '', 1, '', 0, 0, '', '', '', '', ?)`,
		idTmp, idLims, idStudyLims, uuidLims, name, accession, formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedRealMLWHStudyRow: %v", err)
	}
}

func seedRealMLWHFlowcellRow(t *testing.T, db *sql.DB, idTmp int64, pipelineIDLims string, idSampleTmp int64, idStudyTmp int64, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_flowcell(id_iseq_flowcell_tmp, entity_type, pipeline_id_lims, id_sample_tmp, id_study_tmp, legacy_library_id, id_library_lims, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		idTmp, "library", pipelineIDLims, idSampleTmp, idStudyTmp, nil, nil, formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedRealMLWHFlowcellRow: %v", err)
	}
}

func seedRealMLWHProductMetricRow(t *testing.T, db *sql.DB, idProduct int64, idFlowcellTmp int64, idRun, position, tagIndex, qc, qcLib, qcSeq int, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics(id_iseq_pr_metrics_tmp, id_iseq_product, last_changed, id_iseq_flowcell_tmp, id_run, position, tag_index, iseq_composition_tmp, qc, qc_lib, qc_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idProduct,
		fmt.Sprintf("product-%d", idProduct),
		formatSyncTime(lastUpdated),
		idFlowcellTmp,
		idRun,
		position,
		tagIndex,
		fmt.Sprintf(`{"components":[{"id_run":%d,"position":%d,"tag_index":%d}]}`, idRun, position, tagIndex),
		qc,
		qcLib,
		qcSeq,
	)
	if err != nil {
		t.Fatalf("seedRealMLWHProductMetricRow: %v", err)
	}
}

func seedRealMLWHCompositeProductMetricRow(t *testing.T, db *sql.DB, idTmp int64, idProduct string, composition string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics(id_iseq_pr_metrics_tmp, id_iseq_product, last_changed, id_iseq_flowcell_tmp, id_run, position, tag_index, iseq_composition_tmp, qc, qc_lib, qc_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idTmp,
		idProduct,
		formatSyncTime(lastUpdated),
		nil,
		nil,
		nil,
		nil,
		composition,
		1,
		1,
		1,
	)
	if err != nil {
		t.Fatalf("seedRealMLWHCompositeProductMetricRow: %v", err)
	}
}

func seedRealMLWHIRODSLocationRow(t *testing.T, db *sql.DB, idProduct int64, rootCollection, relativePath, collection, fileName string, lastUpdated time.Time) {
	t.Helper()
	_ = collection
	_ = fileName

	seedRealMLWHIRODSLocationProductRow(t, db, idProduct, fmt.Sprintf("product-%d", idProduct), rootCollection, relativePath, lastUpdated)
}

func seedRealMLWHIRODSLocationProductRow(t *testing.T, db *sql.DB, idTmp int64, idProduct, rootCollection, relativePath string, lastUpdated time.Time) {
	t.Helper()

	seedRealMLWHIRODSLocationPlatformRow(t, db, idTmp, idProduct, "Illumina", rootCollection, relativePath, lastUpdated, lastUpdated)
}

type sqliteJSONTableSource struct {
	db *sql.DB
}

func (source sqliteJSONTableSource) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return source.db.QueryContext(ctx, rewriteJSONTableQueryForSQLite(query), args...)
}

func rewriteJSONTableQueryForSQLite(query string) string {
	// SQLite has no schemas, so the schema-qualified tracking table name resolves
	// to the unqualified fixture table.
	query = strings.ReplaceAll(query, "mlwh_reporting.seq_ops_tracking_per_sample", "seq_ops_tracking_per_sample")
	query = strings.ReplaceAll(query, `JSON_LENGTH(COALESCE(ipm.iseq_composition_tmp, '{"components":[]}'), '$.components')`, `json_array_length(COALESCE(ipm.iseq_composition_tmp, '{"components":[]}'), '$.components')`)
	query = strings.Replace(query,
		`JSON_TABLE(COALESCE(ipm.iseq_composition_tmp, '{"components":[]}'), '$.components[1]' COLUMNS(component_run INT PATH '$.id_run')) direct_component`,
		`json_each(COALESCE(ipm.iseq_composition_tmp, '{"components":[]}'), '$.components[1]') direct_component`,
		1,
	)
	query = strings.Replace(query,
		`INNER JOIN JSON_TABLE(path_ipm.iseq_composition_tmp, '$.components[*]' COLUMNS(component_run INT PATH '$.id_run', component_position INT PATH '$.position', component_tag_index INT PATH '$.tag_index')) component ON TRUE`,
		`INNER JOIN json_each(path_ipm.iseq_composition_tmp, '$.components') component ON TRUE`,
		1,
	)
	query = strings.Replace(query,
		`ipm.id_run = component.component_run AND ipm.position = component.component_position AND ipm.tag_index = component.component_tag_index`,
		`ipm.id_run = CAST(json_extract(component.value, '$.id_run') AS INTEGER) AND ipm.position = CAST(json_extract(component.value, '$.position') AS INTEGER) AND ipm.tag_index = CAST(json_extract(component.value, '$.tag_index') AS INTEGER)`,
		1,
	)
	query = strings.ReplaceAll(query, `component.component_run`, `CAST(json_extract(component.value, '$.id_run') AS INTEGER)`)
	query = strings.ReplaceAll(query, `component.component_position`, `CAST(json_extract(component.value, '$.position') AS INTEGER)`)
	query = strings.ReplaceAll(query, `component.component_tag_index`, `CAST(json_extract(component.value, '$.tag_index') AS INTEGER)`)

	return query
}

func mustExec(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()

	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}
