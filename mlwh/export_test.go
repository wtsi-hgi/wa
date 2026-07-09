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
	"errors"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/smartystreets/goconvey/convey"
)

func TestStreamExportProductsRowsUsesKeysetCursorWithoutOffsetD2(t *testing.T) {
	convey.Convey("D2.2: Given a complete products stream with several internal pages", t, func() {
		db, mock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		columns := []string{
			"id_run", "position", "tag_index", "name", "supplier_name", "accession_number",
			"sanger_sample_id", "product_count", "pending_qc", "min_qc",
		}
		firstQuery := manifestListSelectPrefix + manifestListBaseFrom + manifestListWhere +
			` GROUP BY ipm.id_run, ipm.position, ipm.tag_index` +
			` ORDER BY ipm.id_run, ipm.position, ipm.tag_index, MIN(sm.name) LIMIT ?`
		secondQuery := manifestListSelectPrefix + manifestListBaseFrom + manifestListWhere +
			` AND (ipm.id_run, ipm.position, ipm.tag_index) > (?, ?, ?)` +
			` GROUP BY ipm.id_run, ipm.position, ipm.tag_index` +
			` ORDER BY ipm.id_run, ipm.position, ipm.tag_index, MIN(sm.name) LIMIT ?`

		mock.ExpectQuery("^"+regexp.QuoteMeta(firstQuery)+"$").
			WithArgs("7799", 2).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(10, 1, 1, "product-a", "supplier-a", "EGAN-a", "sanger-a", 1, 0, 1).
				AddRow(10, 1, 2, "product-b", "supplier-b", "EGAN-b", "sanger-b", 1, 0, 1))
		mock.ExpectQuery("^"+regexp.QuoteMeta(secondQuery)+"$").
			WithArgs("7799", int64(10), int64(1), int64(2), 2).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(10, 1, 3, "product-c", "supplier-c", "EGAN-c", "sanger-c", 1, 0, 1))

		client := &Client{}
		plan := exportPlan{
			columns: []exportColumn{
				{Name: "name", Supported: true},
				{Name: "id_run", Supported: true},
				{Name: "lane", Supported: true},
				{Name: "tag_index", Supported: true},
			},
			limit: 2,
		}
		input := exportProductQueryInput{studyID: "7799"}
		parent := exportParent{Canonical: "7799"}

		var streamed [][]string
		count, streamErr := client.streamExportProductsRows(context.Background(), db, plan, input, parent, func(row []string) error {
			streamed = append(streamed, append([]string(nil), row...))

			return nil
		})

		convey.Convey("when a full page is followed by another page, then the next query advances by tuple cursor and never OFFSET", func() {
			convey.So(streamErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldEqual, 3)
			convey.So(streamed, convey.ShouldResemble, [][]string{
				{"product-a", "10", "1", "1"},
				{"product-b", "10", "1", "2"},
				{"product-c", "10", "1", "3"},
			})
			convey.So(mock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestExportStudyProductsRejectsCreatedDateOptionsD3(t *testing.T) {
	convey.Convey("D3.3: Given a products export", t, func() {
		client := &Client{}
		relationship := ExportRelationship{Children: "products", ParentKind: "study"}
		cases := []struct {
			name string
			opts ExportOptions
		}{
			{name: "created-desc sort", opts: ExportOptions{Sort: "created-desc"}},
			{name: "since window", opts: ExportOptions{Since: "2026-07-01T00:00:00Z"}},
			{name: "until window", opts: ExportOptions{Until: "2026-07-02T00:00:00Z"}},
		}

		for _, tc := range cases {
			tc := tc
			convey.Convey("when "+tc.name+" is supplied, then products keep created-date options iRODS-only", func() {
				result, err := client.Export(context.Background(), relationship, "7568", tc.opts)

				convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
				convey.So(err.Error(), convey.ShouldContainSubstring, "created-date export sorting/windows are supported only for iRODS exports")
				convey.So(result.Rows, convey.ShouldBeNil)
			})
		}
	})
}

type exportCountingWriter struct {
	newlines int
}

func (w *exportCountingWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' {
			w.newlines++
		}
	}

	return len(p), nil
}

func TestExportStudyIRODSAllUsesKeysetWithoutCappingRowsD1a(t *testing.T) {
	convey.Convey("D1a.4: Given a large study iRODS export", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		const rows = 120_000
		seedLargeExportScenario(t, client.cache.DB(), rows)

		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		result, err := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7699", ExportOptions{
			Columns: []string{"supplier_name", "study_accession_number", "sanger_sample_id", "manual_qc", "id_run", "lane", "tag_index", "platform", "created", "merged", "deliverable", "irods_path"},
			All:     true,
			Limit:   997,
		})
		counter := &exportCountingWriter{}
		emittedRows, renderErr := result.RenderTo(context.Background(), counter)

		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		var growth uint64
		if after.HeapInuse > before.HeapInuse {
			growth = after.HeapInuse - before.HeapInuse
		}

		convey.Convey("when --all is requested, then every row is emitted with streaming metadata and bounded heap growth", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(renderErr, convey.ShouldBeNil)
			convey.So(emittedRows, convey.ShouldEqual, rows)
			convey.So(counter.newlines, convey.ShouldEqual, rows+1)
			convey.So(result.Rows, convey.ShouldBeNil)
			convey.So(result.Total, convey.ShouldEqual, -1)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)
			convey.So(result.Complete, convey.ShouldBeTrue)
			convey.So(growth, convey.ShouldBeLessThan, uint64(20*1024*1024))
		})
	})
}

func newExportTestClient(t *testing.T) (*Client, func()) {
	t.Helper()

	cache := openSQLiteSyncTestCache(t)
	client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

	return client, func() { convey.So(cache.Close(), convey.ShouldBeNil) }
}

func seedLargeExportScenario(t *testing.T, db *sql.DB, rows int) {
	t.Helper()

	seedHierarchyStudy(t, db, 7699, "7699")
	seedManifestSampleRow(t, db, 1, "large-sample", "large-supplier", "EGAN-large", "sanger-large")
	seedLibrarySample(t, db, "Standard", 1, "7699")
	seedExportSyncState(t, db)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("seedLargeExportScenario begin: %v", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO seq_product_irods_locations_mirror(id_seq_product_irods_locations_tmp, id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform, id_run, position, tag_index, qc, is_deliverable, merged) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("seedLargeExportScenario prepare: %v", err)
	}
	defer func() { _ = stmt.Close() }()

	base := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)
	for i := range rows {
		_, err = stmt.Exec(
			int64(i+1),
			"large-product-"+formatInt(int64(i+1)),
			"/seq",
			"large/"+formatInt(int64(i+1))+".cram",
			"/seq/large",
			formatInt(int64(i+1))+".cram",
			int64(1),
			"7699",
			formatSyncTime(base),
			formatSyncTime(base),
			"illumina",
			int64(70000+i/96),
			int64(i%8+1),
			int64(i%12+1),
			1,
			1,
			0,
		)
		if err != nil {
			t.Fatalf("seedLargeExportScenario row %d: %v", i, err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("seedLargeExportScenario commit: %v", err)
	}
}

func seedExportSyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	highWater := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	seedSyncStateRun(t, db, syncTableStudy, highWater, highWater.Add(time.Hour))
	seedSyncStateRun(t, db, syncTableSample, highWater, highWater.Add(2*time.Hour))
	seedSyncStateRun(t, db, syncTableIseqFlowcell, highWater, highWater.Add(3*time.Hour))
	seedSyncStateRun(t, db, syncTableIseqProductMetrics, highWater, highWater.Add(4*time.Hour))
	seedSyncStateRun(t, db, syncTableSeqProductIRODSLocations, highWater, highWater.Add(5*time.Hour))
}

func TestExportStudyProductsAllStreamsCompleteSetMemoryBoundedD2(t *testing.T) {
	convey.Convey("D2.1: Given a large study products export", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		const rows = 120_000
		seedLargeProductExportScenario(t, client.cache.DB(), rows)

		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7799", ExportOptions{
			All:   true,
			Limit: 997,
		})
		counter := &exportCountingWriter{}
		emittedRows, renderErr := result.RenderTo(context.Background(), counter)

		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		var growth uint64
		if after.HeapInuse > before.HeapInuse {
			growth = after.HeapInuse - before.HeapInuse
		}

		convey.Convey("when --all is rendered, then every product row streams with bounded heap growth", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(renderErr, convey.ShouldBeNil)
			convey.So(emittedRows, convey.ShouldEqual, rows)
			convey.So(counter.newlines, convey.ShouldEqual, rows+1)
			convey.So(result.Rows, convey.ShouldBeNil)
			convey.So(result.Total, convey.ShouldEqual, -1)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)
			convey.So(result.Complete, convey.ShouldBeTrue)
			convey.So(growth, convey.ShouldBeLessThan, uint64(20*1024*1024))
		})
	})
}

func seedLargeProductExportScenario(t *testing.T, db *sql.DB, rows int) {
	t.Helper()

	seedHierarchyStudy(t, db, 7799, "7799")
	seedManifestSampleRow(t, db, 7799001, "large-product-sample", "large-product-supplier", "EGAN-large-products", "sanger-large-products")
	seedLibrarySample(t, db, "Standard", 7799001, "7799")
	seedExportSyncState(t, db)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("seedLargeProductExportScenario begin: %v", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO iseq_product_metrics_mirror` +
			`(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) ` +
			`VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		t.Fatalf("seedLargeProductExportScenario prepare: %v", err)
	}
	defer func() { _ = stmt.Close() }()

	lastUpdated := formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC))
	for i := range rows {
		_, err = stmt.Exec(
			int64(77_990_000+i),
			int64(7_799_001),
			90_000+i,
			1,
			1,
			int64(7_799_001),
			"7799",
			1,
			1,
			1,
			lastUpdated,
		)
		if err != nil {
			t.Fatalf("seedLargeProductExportScenario row %d: %v", i, err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("seedLargeProductExportScenario commit: %v", err)
	}
}

type exportIRODSSeedRow struct {
	IDSeqProductLocation int64
	IDProduct            string
	Collection           string
	FileName             string
	Created              time.Time
	IDSampleTmp          int64
	StudyID              string
	Platform             string
	IDRun                int
	Position             int
	TagIndex             int
	QC                   sql.NullInt64
	IsDeliverable        sql.NullInt64
	Merged               bool
}

func TestExportIRODSCreatedSortAndWindowE1(t *testing.T) {
	convey.Convey("E1: Given a study iRODS export with distinct created times", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()

		before := time.Date(2026, time.July, 5, 8, 0, 0, 0, time.UTC)
		since := before.Add(time.Hour)
		inside := before.Add(2 * time.Hour)
		until := before.Add(3 * time.Hour)
		seedHierarchyStudy(t, client.cache.DB(), 901, "E1")
		seedManifestSampleRow(t, client.cache.DB(), 21, "sample-e1", "supplier-e1", "EGAN-e1", "sanger-e1")
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "E1")
		seedExportSyncState(t, client.cache.DB())
		for _, row := range []exportIRODSSeedRow{
			{IDSeqProductLocation: 1, IDProduct: "before-window", FileName: "before.cram", Created: before, IDSampleTmp: 21, StudyID: "E1", IDRun: 52553, Position: 1, TagIndex: 1},
			{IDSeqProductLocation: 2, IDProduct: "at-since", FileName: "since.cram", Created: since, IDSampleTmp: 21, StudyID: "E1", IDRun: 52553, Position: 1, TagIndex: 2},
			{IDSeqProductLocation: 3, IDProduct: "inside-window", FileName: "inside.cram", Created: inside, IDSampleTmp: 21, StudyID: "E1", IDRun: 52553, Position: 1, TagIndex: 3},
			{IDSeqProductLocation: 4, IDProduct: "at-until", FileName: "until.cram", Created: until, IDSampleTmp: 21, StudyID: "E1", IDRun: 52553, Position: 1, TagIndex: 4},
		} {
			seedExportIRODSRow(t, client.cache.DB(), row)
		}

		result, err := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "E1", ExportOptions{
			Columns: []string{"id_product", "created"},
			Sort:    "created-desc",
			Since:   formatSyncTime(since),
			Until:   formatSyncTime(until),
			Limit:   100,
		})
		allResult, allErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "E1", ExportOptions{
			Columns: []string{"id_product", "created"},
			Sort:    "created-desc",
			Since:   formatSyncTime(since),
			Until:   formatSyncTime(until),
			All:     true,
			Limit:   1,
		})
		var allRows [][]string
		allCount, renderAllErr := allResult.ForEachRow(context.Background(), func(row []string) error {
			allRows = append(allRows, append([]string(nil), row...))

			return nil
		})

		convey.Convey("when created is selected with created-desc and a half-open window, then only in-window rows render newest-first", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"id_product", "created"})
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"inside-window", formatSyncTime(inside)},
				{"at-since", formatSyncTime(since)},
			})
			convey.So(result.Total, convey.ShouldEqual, 2)
		})

		convey.Convey("when the sorted date-window export requests all rows, then internal offset pages preserve newest-first order", func() {
			convey.So(allErr, convey.ShouldBeNil)
			convey.So(renderAllErr, convey.ShouldBeNil)
			convey.So(allCount, convey.ShouldEqual, 2)
			convey.So(allResult.Rows, convey.ShouldBeNil)
			convey.So(allResult.Total, convey.ShouldEqual, -1)
			convey.So(allResult.Complete, convey.ShouldBeTrue)
			convey.So(allRows, convey.ShouldResemble, [][]string{
				{"inside-window", formatSyncTime(inside)},
				{"at-since", formatSyncTime(since)},
			})
		})
	})
}

func TestExportIRODSCreatedSortUsesDeterministicOffsetPages(t *testing.T) {
	convey.Convey("D1a reviewer: Given a created-desc iRODS export with equal created timestamps", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()

		tieCreated := time.Date(2026, time.July, 5, 8, 0, 0, 0, time.UTC)
		newerCreated := tieCreated.Add(time.Hour)
		seedHierarchyStudy(t, client.cache.DB(), 902, "E1TIE")
		seedManifestSampleRow(t, client.cache.DB(), 22, "sample-e1-tie", "supplier-e1-tie", "EGAN-e1-tie", "sanger-e1-tie")
		seedLibrarySample(t, client.cache.DB(), "Standard", 22, "E1TIE")
		seedExportSyncState(t, client.cache.DB())
		for _, row := range []exportIRODSSeedRow{
			{IDSeqProductLocation: 30, IDProduct: "tie-run-2", FileName: "tie-run-2.cram", Created: tieCreated, IDSampleTmp: 22, StudyID: "E1TIE", IDRun: 52554, Position: 1, TagIndex: 1},
			{IDSeqProductLocation: 20, IDProduct: "newer", FileName: "newer.cram", Created: newerCreated, IDSampleTmp: 22, StudyID: "E1TIE", IDRun: 60000, Position: 1, TagIndex: 1},
			{IDSeqProductLocation: 10, IDProduct: "tie-lane-2", FileName: "tie-lane-2.cram", Created: tieCreated, IDSampleTmp: 22, StudyID: "E1TIE", IDRun: 52553, Position: 2, TagIndex: 1},
			{IDSeqProductLocation: 40, IDProduct: "tie-tag-2", FileName: "tie-tag-2.cram", Created: tieCreated, IDSampleTmp: 22, StudyID: "E1TIE", IDRun: 52553, Position: 1, TagIndex: 2},
			{IDSeqProductLocation: 50, IDProduct: "tie-id-50", FileName: "tie-id-50.cram", Created: tieCreated, IDSampleTmp: 22, StudyID: "E1TIE", IDRun: 52553, Position: 1, TagIndex: 1},
			{IDSeqProductLocation: 5, IDProduct: "tie-id-5", FileName: "tie-id-5.cram", Created: tieCreated, IDSampleTmp: 22, StudyID: "E1TIE", IDRun: 52553, Position: 1, TagIndex: 1},
		} {
			seedExportIRODSRow(t, client.cache.DB(), row)
		}

		first, firstErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "E1TIE", ExportOptions{
			Columns: []string{"id_product", "created"},
			Sort:    "created-desc",
			Limit:   3,
		})
		second, secondErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "E1TIE", ExportOptions{
			Columns: []string{"id_product", "created"},
			Sort:    "created-desc",
			Limit:   3,
			Offset:  3,
		})
		cursorResult, cursorErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "E1TIE", ExportOptions{
			Columns: []string{"id_product", "created"},
			Sort:    "created-desc",
			Cursor:  "cursor-2",
			Limit:   3,
		})

		convey.Convey("when pages are fetched, then equal-created ties use the canonical tuple order and no unusable cursor is advertised", func() {
			convey.So(firstErr, convey.ShouldBeNil)
			convey.So(secondErr, convey.ShouldBeNil)
			convey.So(first.Rows, convey.ShouldResemble, [][]string{
				{"newer", formatSyncTime(newerCreated)},
				{"tie-id-5", formatSyncTime(tieCreated)},
				{"tie-id-50", formatSyncTime(tieCreated)},
			})
			convey.So(second.Rows, convey.ShouldResemble, [][]string{
				{"tie-tag-2", formatSyncTime(tieCreated)},
				{"tie-lane-2", formatSyncTime(tieCreated)},
				{"tie-run-2", formatSyncTime(tieCreated)},
			})
			convey.So(first.Total, convey.ShouldEqual, 6)
			convey.So(second.Total, convey.ShouldEqual, 6)
			convey.So(first.Complete, convey.ShouldBeFalse)
			convey.So(second.Complete, convey.ShouldBeTrue)
			convey.So(first.NextCursor, convey.ShouldBeEmpty)
			convey.So(second.NextCursor, convey.ShouldBeEmpty)
		})

		convey.Convey("when a cursor is supplied with created-desc, then Export rejects the incompatible continuation mode", func() {
			convey.So(errors.Is(cursorErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(cursorErr.Error(), convey.ShouldContainSubstring, "created-date sorted iRODS exports require bounded limit/offset pages")
			convey.So(cursorResult.Rows, convey.ShouldBeNil)
		})
	})
}

func seedExport7556Scenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 7556, "7556")
	seedManifestSampleRow(t, db, 21, "sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha")
	seedManifestSampleRow(t, db, 22, "sample-beta", "supplier-beta", "EGAN-beta", "sanger-beta")
	seedLibrarySample(t, db, "Standard", 21, "7556")
	seedLibrarySample(t, db, "Standard", 22, "7556")
	seedExportSyncState(t, db)

	seedExportIRODSRow(t, db, exportIRODSSeedRow{
		IDSeqProductLocation: 1,
		IDProduct:            "merged-49348",
		Collection:           "/seq/illumina/runs/49/49348/lane1-2/plex1",
		FileName:             "49348_1-2#1.cram",
		IDSampleTmp:          22,
		StudyID:              "7556",
		IDRun:                0,
		Position:             0,
		TagIndex:             0,
		QC:                   sql.NullInt64{Int64: 1, Valid: true},
		IsDeliverable:        sql.NullInt64{Int64: 1, Valid: true},
		Merged:               true,
	})
	seedExportIRODSRow(t, db, exportIRODSSeedRow{
		IDSeqProductLocation: 2,
		IDProduct:            "deliverable-52553",
		Collection:           "/seq/illumina/runs/52/52553/lane1/plex1",
		FileName:             "52553_1#1.cram",
		IDSampleTmp:          21,
		StudyID:              "7556",
		IDRun:                52553,
		Position:             1,
		TagIndex:             1,
		QC:                   sql.NullInt64{Int64: 1, Valid: true},
		IsDeliverable:        sql.NullInt64{Int64: 1, Valid: true},
	})
	seedExportIRODSRow(t, db, exportIRODSSeedRow{
		IDSeqProductLocation: 3,
		IDProduct:            "control-52553",
		Collection:           "/seq/illumina/runs/52/52553/lane1/plex2",
		FileName:             "52553_1#2.cram",
		IDSampleTmp:          21,
		StudyID:              "7556",
		IDRun:                52553,
		Position:             1,
		TagIndex:             2,
		QC:                   sql.NullInt64{Int64: 0, Valid: true},
		IsDeliverable:        sql.NullInt64{Int64: 0, Valid: true},
	})
	seedExportIRODSRow(t, db, exportIRODSSeedRow{
		IDSeqProductLocation: 4,
		IDProduct:            "crai-52553",
		Collection:           "/seq/illumina/runs/52/52553/lane1/plex3",
		FileName:             "52553_1#3.cram.crai",
		IDSampleTmp:          21,
		StudyID:              "7556",
		IDRun:                52553,
		Position:             1,
		TagIndex:             3,
		QC:                   sql.NullInt64{Int64: 1, Valid: true},
		IsDeliverable:        sql.NullInt64{Int64: 1, Valid: true},
	})
	seedExportIRODSRow(t, db, exportIRODSSeedRow{
		IDSeqProductLocation: 5,
		IDProduct:            "pacbio-52554",
		Collection:           "/seq/pacbio/runs/52/52554",
		FileName:             "52554_2#1.cram",
		IDSampleTmp:          22,
		StudyID:              "7556",
		Platform:             "pacbio",
		IDRun:                52554,
		Position:             2,
		TagIndex:             1,
		QC:                   sql.NullInt64{},
		IsDeliverable:        sql.NullInt64{},
	})
}

func seedExportFilterScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 501, "FILTER")
	seedSampleMirrorSearchRow(t, db, 1, "filter-mus-standard-pass", "supplier-1", "Mus Musculus", "donor-1")
	seedSampleMirrorSearchRow(t, db, 2, "filter-mus-bespoke-pass", "supplier-2", "Mus musculus castaneus", "donor-2")
	seedSampleMirrorSearchRow(t, db, 3, "filter-human-standard-pass", "supplier-3", "Homo sapiens", "donor-3")
	seedSampleMirrorSearchRow(t, db, 4, "filter-mus-standard-fail", "supplier-4", "Mus Musculus", "donor-4")
	seedLibrarySample(t, db, "Standard", 1, "FILTER")
	seedLibrarySample(t, db, "Bespoke", 2, "FILTER")
	seedLibrarySample(t, db, "Standard", 3, "FILTER")
	seedLibrarySample(t, db, "Standard", 4, "FILTER")
	seedCommonNameWords(t, db, "musculus", "Mus Musculus", "Mus musculus castaneus")
	seedCommonNameWords(t, db, "mus", "Mus Musculus", "Mus musculus castaneus")
	seedCommonNameWords(t, db, "homo", "Homo sapiens")
	seedCommonNameWords(t, db, "sapiens", "Homo sapiens")
	for _, sampleID := range []int64{1, 2, 3, 4} {
		seedIseqFlowcellMirrorSearchRow(t, db, sampleID, sampleID, "library")
	}
	seedIseqProductMetricsMirrorRowWithQC(t, db, 500001, 1, 50001, 1, 1, "FILTER", sql.NullInt64{Int64: 1, Valid: true})
	seedIseqProductMetricsMirrorRowWithQC(t, db, 500002, 2, 50001, 1, 2, "FILTER", sql.NullInt64{Int64: 1, Valid: true})
	seedIseqProductMetricsMirrorRowWithQC(t, db, 500003, 3, 50001, 1, 3, "FILTER", sql.NullInt64{Int64: 1, Valid: true})
	seedIseqProductMetricsMirrorRowWithQC(t, db, 500004, 4, 50001, 1, 4, "FILTER", sql.NullInt64{Int64: 0, Valid: true})
	seedExportSyncState(t, db)

	rows := []exportIRODSSeedRow{
		{IDSeqProductLocation: 1, IDProduct: "filter-1", FileName: "50001_1#1.cram", IDSampleTmp: 1, StudyID: "FILTER", IDRun: 50001, Position: 1, TagIndex: 1, QC: sql.NullInt64{Int64: 1, Valid: true}, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 2, IDProduct: "filter-2", FileName: "50001_1#2.cram", IDSampleTmp: 2, StudyID: "FILTER", IDRun: 50001, Position: 1, TagIndex: 2, QC: sql.NullInt64{Int64: 1, Valid: true}, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 3, IDProduct: "filter-3", FileName: "50001_1#3.cram", IDSampleTmp: 3, StudyID: "FILTER", IDRun: 50001, Position: 1, TagIndex: 3, QC: sql.NullInt64{Int64: 1, Valid: true}, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 4, IDProduct: "filter-4", FileName: "50001_1#4.cram", IDSampleTmp: 4, StudyID: "FILTER", IDRun: 50001, Position: 1, TagIndex: 4, QC: sql.NullInt64{Int64: 0, Valid: true}, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
	}
	for _, row := range rows {
		row.Collection = "/seq/filter"
		seedExportIRODSRow(t, db, row)
	}
}

func seedCommonNameWords(t *testing.T, db *sql.DB, word string, commonNames ...string) {
	t.Helper()

	for _, commonName := range commonNames {
		_, err := db.Exec(`INSERT INTO common_name_word_mirror(word, common_name) VALUES (?, ?)`, word, commonName)
		if err != nil {
			t.Fatalf("seedCommonNameWords(): %v", err)
		}
	}
}

func seedExportSearchQCDivergenceScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 620, "QCX")
	seedSampleMirrorSearchRow(t, db, 41, "qcx-mixed", "qcx-supplier", "Homo sapiens", "qcx-donor")
	seedLibrarySample(t, db, "Standard", 41, "QCX")
	seedIseqProductMetricsMirrorRowWithQC(t, db, 62001, 41, 62010, 1, 1, "QCX", sql.NullInt64{Int64: 1, Valid: true})
	seedIseqProductMetricsMirrorRowWithQC(t, db, 62002, 41, 62010, 1, 2, "QCX", sql.NullInt64{Int64: 0, Valid: true})
	rebuildSampleSearchIndexForTest(t, db)
	seedExportSyncState(t, db)

	for _, row := range []exportIRODSSeedRow{
		{IDSeqProductLocation: 62001, IDProduct: "62001", Collection: "/seq/qcx", FileName: "62010_1#1.cram", IDSampleTmp: 41, StudyID: "QCX", IDRun: 62010, Position: 1, TagIndex: 1, QC: sql.NullInt64{Int64: 1, Valid: true}, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 62002, IDProduct: "62002", Collection: "/seq/qcx", FileName: "62010_1#2.cram", IDSampleTmp: 41, StudyID: "QCX", IDRun: 62010, Position: 1, TagIndex: 2, QC: sql.NullInt64{Int64: 0, Valid: true}, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
	} {
		seedExportIRODSRow(t, db, row)
	}
}

func seedExportSampleCRAMScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 770, "CRAMS")
	seedManifestSampleRow(t, db, 101, "cram-single", "supplier-single", "EGAN-single", "sanger-single")
	seedManifestSampleRow(t, db, 102, "cram-merged", "supplier-merged", "EGAN-merged", "sanger-merged")
	seedManifestSampleRow(t, db, 103, "cram-control-only", "supplier-control", "EGAN-control", "sanger-control")
	seedManifestSampleRow(t, db, 104, "cram-null-deliverable", "supplier-null", "EGAN-null", "sanger-null")
	for _, sampleID := range []int64{101, 102, 103, 104} {
		seedLibrarySample(t, db, "Standard", sampleID, "CRAMS")
	}
	seedExportSyncState(t, db)

	for _, row := range []exportIRODSSeedRow{
		{IDSeqProductLocation: 10, IDProduct: "single-bam", Collection: "/seq/crams/single", FileName: "52553_1#0.bam", IDSampleTmp: 101, StudyID: "CRAMS", IDRun: 52553, Position: 1, TagIndex: 0, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 11, IDProduct: "single-first", Collection: "/seq/crams/single", FileName: "52553_1#1.cram", IDSampleTmp: 101, StudyID: "CRAMS", IDRun: 52553, Position: 1, TagIndex: 1, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 12, IDProduct: "single-second", Collection: "/seq/crams/single", FileName: "52553_2#1.cram", IDSampleTmp: 101, StudyID: "CRAMS", IDRun: 52553, Position: 2, TagIndex: 1, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 20, IDProduct: "merged-lane1", Collection: "/seq/crams/merged", FileName: "49348_1#1.cram", IDSampleTmp: 102, StudyID: "CRAMS", IDRun: 49348, Position: 1, TagIndex: 1, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 21, IDProduct: "merged-lane2", Collection: "/seq/crams/merged", FileName: "49348_2#1.cram", IDSampleTmp: 102, StudyID: "CRAMS", IDRun: 49348, Position: 2, TagIndex: 1, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 22, IDProduct: "merged-composite", Collection: "/seq/crams/merged", FileName: "49348_1-2#1.cram", IDSampleTmp: 102, StudyID: "CRAMS", IDRun: 0, Position: 0, TagIndex: 0, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}, Merged: true},
		{IDSeqProductLocation: 30, IDProduct: "control-cram", Collection: "/seq/crams/control", FileName: "52554_1#1.cram", IDSampleTmp: 103, StudyID: "CRAMS", IDRun: 52554, Position: 1, TagIndex: 1, IsDeliverable: sql.NullInt64{Int64: 0, Valid: true}},
		{IDSeqProductLocation: 40, IDProduct: "null-cram", Collection: "/seq/crams/null", FileName: "pacbio.cram", IDSampleTmp: 104, StudyID: "CRAMS", Platform: "pacbio", IDRun: 0, Position: 0, TagIndex: 0, IsDeliverable: sql.NullInt64{}},
	} {
		seedExportIRODSRow(t, db, row)
	}
}

func seedExportRun49348MergedCompositeScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 7568, "7568")
	seedManifestSampleRow(t, db, 4934800, "sample-49348", "supplier-49348", "EGAN-49348", "sanger-49348")
	seedLibrarySample(t, db, "Standard", 4934800, "7568")
	seedExportSyncState(t, db)

	seedIseqProductMetricsMirrorRow(t, db, 4934801, 4934800, 49348, 1, 1, "7568")
	seedIseqProductMetricsMirrorRow(t, db, 4934802, 4934800, 49348, 2, 1, "7568")
	seedIseqProductMetricsMirrorRow(t, db, 4934812, 4934800, 49348, 0, 0, "7568")
	seedIseqProductMetricsMirrorRow(t, db, 4934899, 4934800, 0, 0, 0, "7568")

	for _, row := range []exportIRODSSeedRow{
		{IDSeqProductLocation: 1, IDProduct: "4934801", Collection: "/seq/illumina/runs/49/49348/lane1/plex1", FileName: "49348_1#1.cram", IDSampleTmp: 4934800, StudyID: "7568", IDRun: 49348, Position: 1, TagIndex: 1, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 2, IDProduct: "4934802", Collection: "/seq/illumina/runs/49/49348/lane2/plex1", FileName: "49348_2#1.cram", IDSampleTmp: 4934800, StudyID: "7568", IDRun: 49348, Position: 2, TagIndex: 1, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}},
		{IDSeqProductLocation: 3, IDProduct: "4934812", Collection: "/seq/illumina/runs/49/49348/lane1-2/plex1", FileName: "49348_1-2#1.cram", IDSampleTmp: 4934800, StudyID: "7568", IDRun: 49348, Position: 0, TagIndex: 0, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}, Merged: true},
		{IDSeqProductLocation: 4, IDProduct: "4934899", Collection: "/seq/illumina/runs/composite/multi-run", FileName: "multi-run#1.cram", IDSampleTmp: 4934800, StudyID: "7568", IDRun: 0, Position: 0, TagIndex: 0, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}, Merged: true},
	} {
		seedExportIRODSRow(t, db, row)
	}
}

func seedProductDeliverablesScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 601, "DELIV")
	seedManifestSampleRow(t, db, 31, "deliverable-with-nondeliverable-irods", "supplier-deliv-1", "EGAN-deliv-1", "sanger-deliv-1")
	seedManifestSampleRow(t, db, 32, "deliverable-objectless", "supplier-deliv-2", "EGAN-deliv-2", "sanger-deliv-2")
	seedManifestSampleRow(t, db, 33, "control-with-deliverable-irods", "supplier-control", "EGAN-control", "sanger-control")
	for _, sampleID := range []int64{31, 32, 33} {
		seedLibrarySample(t, db, "Standard", sampleID, "DELIV")
	}
	seedIseqFlowcellMirrorSearchRow(t, db, 31, 31, "library")
	seedIseqFlowcellMirrorSearchRow(t, db, 32, 32, "library_indexed")
	seedIseqFlowcellMirrorSearchRow(t, db, 33, 33, "library_control")
	seedIseqProductMetricsMirrorRow(t, db, 3101, 31, 61010, 1, 1, "DELIV")
	seedIseqProductMetricsMirrorRow(t, db, 3202, 32, 61010, 1, 2, "DELIV")
	seedIseqProductMetricsMirrorRow(t, db, 3303, 33, 61010, 1, 3, "DELIV")
	seedExportSyncState(t, db)

	seedExportIRODSRow(t, db, exportIRODSSeedRow{
		IDSeqProductLocation: 3101,
		IDProduct:            "3101",
		Collection:           "/seq/deliverables",
		FileName:             "61010_1#1.cram",
		IDSampleTmp:          31,
		StudyID:              "DELIV",
		IDRun:                61010,
		Position:             1,
		TagIndex:             1,
		IsDeliverable:        sql.NullInt64{Int64: 0, Valid: true},
	})
	seedExportIRODSRow(t, db, exportIRODSSeedRow{
		IDSeqProductLocation: 3303,
		IDProduct:            "3303",
		Collection:           "/seq/deliverables",
		FileName:             "61010_1#3.cram",
		IDSampleTmp:          33,
		StudyID:              "DELIV",
		IDRun:                61010,
		Position:             1,
		TagIndex:             3,
		IsDeliverable:        sql.NullInt64{Int64: 1, Valid: true},
	})
}

func seedExportIRODSRow(t *testing.T, db *sql.DB, row exportIRODSSeedRow) {
	t.Helper()

	if row.Collection == "" {
		row.Collection = "/seq/export"
	}
	if row.Platform == "" {
		row.Platform = "illumina"
	}

	created := row.Created
	if created.IsZero() {
		created = time.Date(2026, time.July, 1, 8, 30, 0, 0, time.UTC)
	}
	_, err := db.Exec(
		`INSERT INTO seq_product_irods_locations_mirror(id_seq_product_irods_locations_tmp, id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform, id_run, position, tag_index, qc, is_deliverable, merged) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.IDSeqProductLocation,
		row.IDProduct,
		"/seq",
		row.FileName,
		row.Collection,
		row.FileName,
		row.IDSampleTmp,
		row.StudyID,
		formatSyncTime(created),
		formatSyncTime(created),
		row.Platform,
		row.IDRun,
		row.Position,
		row.TagIndex,
		row.QC,
		row.IsDeliverable,
		row.Merged,
	)
	if err != nil {
		t.Fatalf("seedExportIRODSRow(): %v", err)
	}
}

func TestExportStudyProductsDefaultColumnsAndProductGrainA1(t *testing.T) {
	convey.Convey("A1: Given study S1 with 3 product-metrics rows and no iRODS objects", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestS1Scenario(t, client.cache.DB())
		setIseqProductMetricsMirrorQC(t, client.cache.DB(), 2102, sql.NullInt64{Int64: 0, Valid: true})
		setIseqProductMetricsMirrorQC(t, client.cache.DB(), 2203, sql.NullInt64{})

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{})
		aliasResult, aliasErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{
			Columns: []string{"position", "supplier_sample_name"},
		})
		unknownResult, unknownErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{
			Columns: []string{"not_a_column"},
		})

		convey.Convey("when products are exported with default columns, then rows are product-grained, objectless, ordered, and QC rolled up", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{
				"name",
				"supplier_name",
				"accession_number",
				"sanger_sample_id",
				"id_run",
				"lane",
				"tag_index",
				"manual_qc",
			})
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"S1-sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha", "52553", "1", "1", "pass"},
				{"S1-sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha", "52553", "1", "2", "fail"},
				{"S1-sample-beta", "supplier-beta", "EGAN-beta", "sanger-beta", "52554", "2", "3", "pending"},
			})
			convey.So(result.Total, convey.ShouldEqual, 3)
			convey.So(result.Complete, convey.ShouldBeTrue)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)
		})

		convey.Convey("when alias columns are requested, then the projection uses canonical column names", func() {
			convey.So(aliasErr, convey.ShouldBeNil)
			convey.So(aliasResult.Columns, convey.ShouldResemble, []string{"lane", "supplier_name"})
			convey.So(aliasResult.Rows, convey.ShouldResemble, [][]string{
				{"1", "supplier-alpha"},
				{"1", "supplier-alpha"},
				{"2", "supplier-beta"},
			})
		})

		convey.Convey("when an unknown product column is requested, then the error lists the valid product vocabulary", func() {
			convey.So(errors.Is(unknownErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(unknownErr.Error(), convey.ShouldContainSubstring, `unknown export column "not_a_column"`)
			convey.So(unknownErr.Error(), convey.ShouldContainSubstring, "valid columns:")
			convey.So(unknownErr.Error(), convey.ShouldContainSubstring, "irods_unmatched")
			convey.So(unknownResult.Rows, convey.ShouldBeNil)
		})
	})
}

func TestExportStudyProductsStudyConstantsA2(t *testing.T) {
	convey.Convey("A2.1: Given study S1 with product-metrics rows", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestS1Scenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{
			Columns: []string{"name", "id_study_lims", "study_accession_number"},
		})

		convey.Convey("when study-constant extras are selected, then every product row uses the resolved parent study values", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"name", "id_study_lims", "study_accession_number"})
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"S1-sample-alpha", "S1", "EGAS0000S1"},
				{"S1-sample-alpha", "S1", "EGAS0000S1"},
				{"S1-sample-beta", "S1", "EGAS0000S1"},
			})
			convey.So(result.Total, convey.ShouldEqual, 3)
		})
	})
}

func TestExportStudyProductsAttachesIRODSPathWithoutChangingProductGrainB1(t *testing.T) {
	convey.Convey("B1: Given study 7568 with 96 merged-gap products and 1 direct CRAM product", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestStudy7568MergedCRAMScenario(t, client.cache.DB())

		columns := []string{"name", "id_run", "lane", "tag_index", "irods_path"}
		cramResult, cramErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7568", ExportOptions{
			Columns:  columns,
			FileType: "cram",
			Limit:    manifestAllRows,
		})
		allTypesResult, allTypesErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7568", ExportOptions{
			Columns: columns,
			Limit:   manifestAllRows,
		})
		bamResult, bamErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7568", ExportOptions{
			Columns:  columns,
			FileType: "bam",
			Limit:    manifestAllRows,
		})

		convey.Convey("when file_type=cram is requested, then all product rows remain and only the direct CRAM path attaches", func() {
			nonEmpty, blank, directPath := countExportColumnValues(cramResult.Rows, 4, h4Study7568DirectCramPath)

			convey.So(cramErr, convey.ShouldBeNil)
			convey.So(cramResult.Columns, convey.ShouldResemble, columns)
			convey.So(cramResult.Rows, convey.ShouldHaveLength, h4Study7568MergedSingleLaneProducts+1)
			convey.So(cramResult.Total, convey.ShouldEqual, h4Study7568MergedSingleLaneProducts+1)
			convey.So(nonEmpty, convey.ShouldEqual, 1)
			convey.So(directPath, convey.ShouldEqual, 1)
			convey.So(blank, convey.ShouldEqual, h4Study7568MergedSingleLaneProducts)
		})

		convey.Convey("when file_type is unset, then product export still attaches the direct path without a CRAM default filter", func() {
			nonEmpty, _, directPath := countExportColumnValues(allTypesResult.Rows, 4, h4Study7568DirectCramPath)

			convey.So(allTypesErr, convey.ShouldBeNil)
			convey.So(allTypesResult.Rows, convey.ShouldHaveLength, h4Study7568MergedSingleLaneProducts+1)
			convey.So(allTypesResult.Total, convey.ShouldEqual, h4Study7568MergedSingleLaneProducts+1)
			convey.So(nonEmpty, convey.ShouldEqual, 1)
			convey.So(directPath, convey.ShouldEqual, 1)
		})

		convey.Convey("when file_type=bam is requested, then the product total is unchanged and no CRAM path attaches", func() {
			nonEmpty, blank, _ := countExportColumnValues(bamResult.Rows, 4, h4Study7568DirectCramPath)

			convey.So(bamErr, convey.ShouldBeNil)
			convey.So(bamResult.Rows, convey.ShouldHaveLength, h4Study7568MergedSingleLaneProducts+1)
			convey.So(bamResult.Total, convey.ShouldEqual, h4Study7568MergedSingleLaneProducts+1)
			convey.So(nonEmpty, convey.ShouldEqual, 0)
			convey.So(blank, convey.ShouldEqual, h4Study7568MergedSingleLaneProducts+1)
		})
	})
}

func countExportColumnValues(rows [][]string, columnIndex int, target string) (int, int, int) {
	nonEmpty := 0
	blank := 0
	targetMatches := 0
	for _, row := range rows {
		value := row[columnIndex]
		if value == "" {
			blank++
			continue
		}
		nonEmpty++
		if value == target {
			targetMatches++
		}
	}

	return nonEmpty, blank, targetMatches
}

func TestExportStudyProductsFlagsOnlyMergedMultilaneIRODSGapsB2(t *testing.T) {
	convey.Convey("B2.1: Given study 7568 with merged CRAM composites and one direct CRAM product", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestStudy7568MergedCRAMScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7568", ExportOptions{
			Columns:  []string{"irods_path", "irods_unmatched", "reason"},
			FileType: "cram",
			Limit:    manifestAllRows,
		})

		convey.Convey("when CRAM iRODS columns are exported, then only merged multi-lane gaps are flagged and no composite path is copied", func() {
			unmatchedRows := 0
			wrongReasonRows := 0
			unmatchedRowsWithPath := 0
			directRows := 0
			directRowsWithReason := 0
			rowsDuplicatingMergedPath := 0
			for _, row := range result.Rows {
				switch {
				case row[1] == "true":
					unmatchedRows++
					if row[0] != "" {
						unmatchedRowsWithPath++
					}
					if row[2] != h4MergedMultilaneReason {
						wrongReasonRows++
					}
				case row[0] != "":
					directRows++
					if row[2] != "" {
						directRowsWithReason++
					}
				}
				if row[0] == h4Study7568MergedCramPathForFixture {
					rowsDuplicatingMergedPath++
				}
			}

			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldHaveLength, h4Study7568MergedSingleLaneProducts+1)
			convey.So(result.Total, convey.ShouldEqual, h4Study7568MergedSingleLaneProducts+1)
			convey.So(unmatchedRows, convey.ShouldEqual, h4Study7568MergedSingleLaneProducts)
			convey.So(wrongReasonRows, convey.ShouldEqual, 0)
			convey.So(unmatchedRowsWithPath, convey.ShouldEqual, 0)
			convey.So(directRows, convey.ShouldEqual, 1)
			convey.So(directRowsWithReason, convey.ShouldEqual, 0)
			convey.So(rowsDuplicatingMergedPath, convey.ShouldEqual, 0)
		})
	})
}

func TestExportStudyProductsDoNotFlagObjectlessProductsB2(t *testing.T) {
	convey.Convey("B2.2: Given study S1 has objectless products and no merged CRAM composite", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestS1Scenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{
			Columns: []string{"irods_path", "irods_unmatched", "reason"},
		})

		convey.Convey("when iRODS columns are exported without a file type, then objectless products remain unflagged", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldHaveLength, 3)
			convey.So(result.Total, convey.ShouldEqual, 3)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"", "false", ""},
				{"", "false", ""},
				{"", "false", ""},
			})
		})
	})
}

func TestExportStudyProductsMergedGapDetectionIsCRAMOnlyB2(t *testing.T) {
	convey.Convey("B2.3: Given study 7568 has merged CRAM composites", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestStudy7568MergedCRAMScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7568", ExportOptions{
			Columns:  []string{"irods_unmatched"},
			FileType: "bam",
			Limit:    manifestAllRows,
		})

		convey.Convey("when file_type is not CRAM, then no merged-gap rows are flagged and total remains product-grained", func() {
			unmatchedRows := 0
			for _, row := range result.Rows {
				if row[0] == "true" {
					unmatchedRows++
				}
			}

			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldHaveLength, h4Study7568MergedSingleLaneProducts+1)
			convey.So(result.Total, convey.ShouldEqual, h4Study7568MergedSingleLaneProducts+1)
			convey.So(unmatchedRows, convey.ShouldEqual, 0)
		})
	})
}

func TestExportStudyProductsResultHasNoEnvelopeGapSummaryB2(t *testing.T) {
	convey.Convey("B2.4: Given a product export result", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestS1Scenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{})
		payload, marshalErr := json.Marshal(result)

		convey.Convey("when the result is marshalled, then product rows are the only gap-reporting surface", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(marshalErr, convey.ShouldBeNil)
			convey.So(string(payload), convey.ShouldNotContainSubstring, "products_without_irods")
			convey.So(string(payload), convey.ShouldNotContainSubstring, "ProductsWithoutIRODS")
		})
	})
}

func TestExportStudyProductsQCFilterReducesTotalC1(t *testing.T) {
	convey.Convey("C1.1-C1.2: Given study S1 has pass, fail, and pending products", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestS1Scenario(t, client.cache.DB())
		setIseqProductMetricsMirrorQC(t, client.cache.DB(), 2102, sql.NullInt64{Int64: 0, Valid: true})
		setIseqProductMetricsMirrorQC(t, client.cache.DB(), 2203, sql.NullInt64{})

		unfiltered, unfilteredErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{})
		qcCases := []struct {
			filter string
			row    []string
		}{
			{filter: qcFail, row: []string{"S1-sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha", "52553", "1", "2", "fail"}},
			{filter: qcPending, row: []string{"S1-sample-beta", "supplier-beta", "EGAN-beta", "sanger-beta", "52554", "2", "3", "pending"}},
			{filter: qcPass, row: []string{"S1-sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha", "52553", "1", "1", "pass"}},
		}
		filteredTotal := 0

		convey.Convey("when each QC verdict is exported, then each filter returns only its rolled-up product row", func() {
			convey.So(unfilteredErr, convey.ShouldBeNil)
			convey.So(unfiltered.Total, convey.ShouldEqual, 3)

			for _, testCase := range qcCases {
				result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{
					QC: testCase.filter,
				})

				convey.So(err, convey.ShouldBeNil)
				convey.So(result.Rows, convey.ShouldResemble, [][]string{testCase.row})
				convey.So(result.Total, convey.ShouldEqual, 1)
				filteredTotal += result.Total
			}
			convey.So(filteredTotal, convey.ShouldEqual, unfiltered.Total)
		})
	})
}

func TestExportStudyProductsOrganismFilterReducesTotalC1(t *testing.T) {
	convey.Convey("C1.3: Given study S1 products span two organisms and have no iRODS objects", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestS1Scenario(t, client.cache.DB())
		setManifestSampleCommonName(t, client.cache.DB(), 21, "Mus Musculus")
		setManifestSampleCommonName(t, client.cache.DB(), 22, "Homo sapiens")
		seedCommonNameWords(t, client.cache.DB(), "musculus", "Mus Musculus")

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{
			Columns:  []string{"name", "manual_qc", "irods_path"},
			Organism: "musculus",
		})

		convey.Convey("when one organism is exported, then Total and rows include only that organism's objectless products", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"S1-sample-alpha", "pass", ""},
				{"S1-sample-alpha", "pass", ""},
			})
			convey.So(result.Total, convey.ShouldEqual, 2)
			convey.So(result.Complete, convey.ShouldBeTrue)
		})
	})
}

func setManifestSampleCommonName(t *testing.T, db *sql.DB, idSampleTmp int64, commonName string) {
	t.Helper()

	_, err := db.Exec(`UPDATE sample_mirror SET common_name = ? WHERE id_sample_tmp = ?`, commonName, idSampleTmp)
	if err != nil {
		t.Fatalf("setManifestSampleCommonName(): %v", err)
	}
}

func TestExportStudyProductsLibraryTypeFilterReducesTotalC1(t *testing.T) {
	convey.Convey("C1.4: Given study S1 products span two library types", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestS1Scenario(t, client.cache.DB())
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "S1")
		seedLibrarySample(t, client.cache.DB(), "Bespoke", 22, "S1")

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{
			Columns:     []string{"name", "id_run", "lane", "tag_index"},
			LibraryType: "Standard",
		})

		convey.Convey("when one library type is exported, then Total and rows include only that type's products", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"S1-sample-alpha", "52553", "1", "1"},
				{"S1-sample-alpha", "52553", "1", "2"},
			})
			convey.So(result.Total, convey.ShouldEqual, 2)
		})
	})
}

func TestExportStudyProductsDeliverablesOnlyUsesFlowcellEntityTypeC2(t *testing.T) {
	convey.Convey("C2.1: Given study DELIV has deliverable and control products by flowcell entity_type", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedProductDeliverablesScenario(t, client.cache.DB())
		deliverablesOnly := true

		unfiltered, unfilteredErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "DELIV", ExportOptions{})
		filtered, filteredErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "DELIV", ExportOptions{
			Columns:          []string{"name", "lane", "tag_index", "irods_path"},
			DeliverablesOnly: &deliverablesOnly,
		})

		convey.Convey("when deliverables_only is nil then all products remain, and when true only entity_type deliverables remain", func() {
			convey.So(unfilteredErr, convey.ShouldBeNil)
			convey.So(unfiltered.Total, convey.ShouldEqual, 3)
			convey.So(filteredErr, convey.ShouldBeNil)
			convey.So(filtered.Rows, convey.ShouldResemble, [][]string{
				{"deliverable-with-nondeliverable-irods", "1", "1", "/seq/deliverables/61010_1#1.cram"},
				{"deliverable-objectless", "1", "2", ""},
			})
			convey.So(filtered.Total, convey.ShouldEqual, 2)
		})
	})
}

func TestExportStudyProductsDeliverablesOnlyWithFileTypeKeepsProductTotalC2(t *testing.T) {
	convey.Convey("C2.2: Given study DELIV has a deliverable product without an iRODS object", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedProductDeliverablesScenario(t, client.cache.DB())
		deliverablesOnly := true

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "DELIV", ExportOptions{
			Columns:          []string{"name", "lane", "tag_index", "irods_path"},
			DeliverablesOnly: &deliverablesOnly,
			FileType:         "cram",
		})

		convey.Convey("when file_type=cram is also set, then only attached paths change and Total still counts both deliverable products", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"deliverable-with-nondeliverable-irods", "1", "1", "/seq/deliverables/61010_1#1.cram"},
				{"deliverable-objectless", "1", "2", ""},
			})
			convey.So(result.Total, convey.ShouldEqual, 2)
		})
	})
}

func TestExportStudyProductsSyncedStudyWithNoProductsReturnsEmptyE1(t *testing.T) {
	convey.Convey("E1.1: Given a synced study S1 with metadata but no products", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedHierarchyStudy(t, client.cache.DB(), 211, "S1")
		seedManifestSyncState(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{})

		convey.Convey("when products are exported, then Export returns a complete empty result with no error", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldBeEmpty)
			convey.So(result.Total, convey.ShouldEqual, 0)
			convey.So(result.Complete, convey.ShouldBeTrue)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)
		})
	})
}

func TestExportStudyProductsNeverSyncedAndUnknownStudySentinelsE1(t *testing.T) {
	convey.Convey("E1.2: Given a never-synced cache", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{})

		convey.Convey("when products are exported for a name-like id, then parent resolution returns both sentinels", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(result, convey.ShouldResemble, ExportResult{})
		})
	})

	convey.Convey("E1.3: Given a synced cache with no matching study", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestSyncState(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "NOPE", ExportOptions{})

		convey.Convey("when products are exported for an unknown name-like id, then Export returns ErrNotFound only", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
			convey.So(result, convey.ShouldResemble, ExportResult{})
		})
	})
}

func TestExportStudyProductsEmptyStudyRequiresProductMetricsSyncE1(t *testing.T) {
	convey.Convey("E1.4: Given study S1 with metadata and identity sync but product metrics never synced", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedHierarchyStudy(t, client.cache.DB(), 211, "S1")
		seedManifestIdentitySyncState(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{})

		convey.Convey("when products are exported, then Export reports the product-metrics source as never synced", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(result, convey.ShouldResemble, ExportResult{})
		})
	})
}

func TestExportStudyProductsIRODSAttachmentRequiresIRODSSyncE1(t *testing.T) {
	convey.Convey("E1.5: Given study S1 has product metrics but the iRODS locations mirror never synced", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedHierarchyStudy(t, client.cache.DB(), 211, "S1")
		seedManifestSampleRow(t, client.cache.DB(), 21, "S1-sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 2101, 21, 52553, 1, 1, "S1")
		seedManifestSyncStateWithoutIRODS(t, client.cache.DB())

		plainResult, plainErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{})
		irodsResult, irodsErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{
			Columns: []string{"name", "irods_path"},
		})

		convey.Convey("when no iRODS attachment column is selected, then the product row still exports", func() {
			convey.So(plainErr, convey.ShouldBeNil)
			convey.So(plainResult.Rows, convey.ShouldHaveLength, 1)
			convey.So(plainResult.Total, convey.ShouldEqual, 1)
		})

		convey.Convey("when an iRODS attachment column is selected, then Export reports the iRODS source as never synced", func() {
			convey.So(errors.Is(irodsErr, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(irodsErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(irodsResult, convey.ShouldResemble, ExportResult{})
		})
	})
}

func TestExportStudyProductsEmptyIRODSAttachmentRequiresIRODSSyncE1(t *testing.T) {
	convey.Convey("E1.5: Given synced study S1 has no products and the iRODS locations mirror never synced", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedHierarchyStudy(t, client.cache.DB(), 211, "S1")
		seedManifestSyncStateWithoutIRODS(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "S1", ExportOptions{
			Columns: []string{"irods_path"},
		})

		convey.Convey("when the first product page is empty but needs iRODS, then Export reports the iRODS source as never synced", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(result, convey.ShouldResemble, ExportResult{})
		})
	})
}

func TestExportStudyIRODSCramColumnsAliasAndDeliverablesD1a(t *testing.T) {
	convey.Convey("D1a.1: Given study 7556 with mixed iRODS rows", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExport7556Scenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "files", ParentKind: "study"}, "EGAS00007556", ExportOptions{
			Columns:  []string{"supplier_sample_name", "accession_number", "study_accession_number", "sanger_sample_id", "manual_qc", "irods_path"},
			FileType: "cram",
		})

		convey.Convey("when the files alias export runs, then sample identity columns are canonicalised, controls/non-crams are dropped, and merged CRAMs sort first", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"supplier_name", "accession_number", "study_accession_number", "sanger_sample_id", "manual_qc", "irods_path"})
			convey.So(result.Rows, convey.ShouldHaveLength, 3)
			convey.So(result.Total, convey.ShouldEqual, 3)
			convey.So(result.Complete, convey.ShouldBeTrue)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)
			convey.So(result.Rows[0][0], convey.ShouldEqual, "supplier-beta")
			convey.So(result.Rows[0][1], convey.ShouldEqual, "EGAN-beta")
			convey.So(result.Rows[0][2], convey.ShouldEqual, "EGAS00007556")
			convey.So(result.Rows[0][3], convey.ShouldEqual, "sanger-beta")
			convey.So(result.Rows[0][4], convey.ShouldEqual, "pass")
			convey.So(result.Rows[0][5], convey.ShouldContainSubstring, "/lane1-2/plex1/49348_1-2#1.cram")

			nonCramPaths := 0
			for _, row := range result.Rows {
				if !strings.HasSuffix(row[5], ".cram") {
					nonCramPaths++
				}
			}
			convey.So(nonCramPaths, convey.ShouldEqual, 0)
		})
	})
}

func TestExportStudyIRODSDefaultColumnsExcludeStudyAccession(t *testing.T) {
	convey.Convey("Given a study iRODS export with default columns", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExport7556Scenario(t, client.cache.DB())

		defaultResult, defaultErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7556", ExportOptions{
			Limit: 10,
		})
		explicitResult, explicitErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7556", ExportOptions{
			Columns: []string{"supplier_name", "study_accession_number", "irods_path"},
			Limit:   10,
		})

		convey.Convey("when no projection is supplied, then study_accession_number is omitted while remaining explicitly selectable", func() {
			convey.So(defaultErr, convey.ShouldBeNil)
			convey.So(defaultResult.Columns, convey.ShouldResemble, []string{"supplier_name", "sanger_sample_id", "manual_qc", "irods_path"})
			convey.So(defaultResult.Rows, convey.ShouldHaveLength, 3)

			convey.So(explicitErr, convey.ShouldBeNil)
			convey.So(explicitResult.Columns, convey.ShouldResemble, []string{"supplier_name", "study_accession_number", "irods_path"})
			convey.So(explicitResult.Rows[0][1], convey.ShouldEqual, "EGAS00007556")
		})
	})
}

func TestExportStudyIRODSReportsMergedCompositeHonestRunH1(t *testing.T) {
	convey.Convey("H1: Given a study iRODS export with a merged lane1-2 object and a single-lane object", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExport7556Scenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7556", ExportOptions{
			Columns:  []string{"name", "supplier_name", "id_sample_tmp", "id_run", "merged", "irods_path"},
			FileType: "cram",
			Limit:    100,
		})

		convey.Convey("when iRODS rows are exported, then the composite row remains sample-attributed with merged=true/id_run=0 and the single-lane row keeps its run", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"name", "supplier_name", "id_sample_tmp", "id_run", "merged", "irods_path"})

			merged := exportRowContainingPath(t, result.Rows, 5, "/lane1-2/plex1/49348_1-2#1.cram")
			convey.So(merged, convey.ShouldResemble, []string{
				"sample-beta",
				"supplier-beta",
				"22",
				"0",
				"true",
				"/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram",
			})

			single := exportRowContainingPath(t, result.Rows, 5, "/lane1/plex1/52553_1#1.cram")
			convey.So(single[3], convey.ShouldEqual, "52553")
			convey.So(single[4], convey.ShouldEqual, "false")
		})
	})
}

func TestExportRunIRODSIncludesSingleRunMergedCompositeH2(t *testing.T) {
	convey.Convey("H2: Given run 49348 has single-lane rows and a single-run merged composite", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportRun49348MergedCompositeScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "run"}, "49348", ExportOptions{
			Columns:  []string{"name", "supplier_name", "id_sample_tmp", "id_product", "id_run", "lane", "tag_index", "merged", "irods_path"},
			FileType: "cram",
			Limit:    100,
		})

		convey.Convey("when run iRODS is exported, then the merged composite is included with sample attribution and public id_run=0 beside single-lane objects", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldHaveLength, 3)
			convey.So(result.Total, convey.ShouldEqual, 3)

			merged := exportRowContainingPath(t, result.Rows, 8, "/lane1-2/plex1/49348_1-2#1.cram")
			convey.So(merged, convey.ShouldResemble, []string{
				"sample-49348",
				"supplier-49348",
				"4934800",
				"4934812",
				"0",
				"0",
				"0",
				"true",
				"/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram",
			})

			firstLane := exportRowContainingPath(t, result.Rows, 8, "/lane1/plex1/49348_1#1.cram")
			convey.So(firstLane[0], convey.ShouldEqual, "sample-49348")
			convey.So(firstLane[3], convey.ShouldEqual, "4934801")
			convey.So(firstLane[4], convey.ShouldEqual, "49348")
			convey.So(firstLane[5], convey.ShouldEqual, "1")
			convey.So(firstLane[6], convey.ShouldEqual, "1")
			convey.So(firstLane[7], convey.ShouldEqual, "false")

			secondLane := exportRowContainingPath(t, result.Rows, 8, "/lane2/plex1/49348_2#1.cram")
			convey.So(secondLane[3], convey.ShouldEqual, "4934802")
			convey.So(secondLane[4], convey.ShouldEqual, "49348")
			convey.So(secondLane[5], convey.ShouldEqual, "2")
			convey.So(secondLane[6], convey.ShouldEqual, "1")
			convey.So(secondLane[7], convey.ShouldEqual, "false")
		})
	})
}

func exportRowContainingPath(t *testing.T, rows [][]string, pathColumn int, pathSubstring string) []string {
	t.Helper()

	for _, row := range rows {
		if strings.Contains(row[pathColumn], pathSubstring) {
			return row
		}
	}

	t.Fatalf("export row containing path %q not found in %#v", pathSubstring, rows)

	return nil
}

func TestExportUnknownColumnListsVocabularyD1a(t *testing.T) {
	convey.Convey("D1a.2: Given an unknown export column", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExport7556Scenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7556", ExportOptions{
			Columns: []string{"supplier_name", "not_a_column"},
		})

		convey.Convey("when Export validates columns, then it returns an actionable error before emitting rows", func() {
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring, `unknown export column "not_a_column"`)
			convey.So(err.Error(), convey.ShouldContainSubstring, "valid columns:")
			convey.So(err.Error(), convey.ShouldContainSubstring, "supplier_name")
			convey.So(err.Error(), convey.ShouldContainSubstring, "irods_path")
			convey.So(result.Rows, convey.ShouldBeNil)
		})
	})
}

func TestExportFileVocabulariesRejectProductOnlyColumnsJ1(t *testing.T) {
	convey.Convey("J1.3: Given iRODS and sample-crams exports", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExport7556Scenario(t, client.cache.DB())

		testCases := []struct {
			rel      ExportRelationship
			parentID string
			column   string
		}{
			{rel: ExportRelationship{Children: "irods", ParentKind: "study"}, parentID: "7556", column: "irods_unmatched"},
			{rel: ExportRelationship{Children: "irods", ParentKind: "study"}, parentID: "7556", column: "reason"},
			{rel: ExportRelationship{Children: "sample-crams", ParentKind: "study"}, parentID: "7556", column: "irods_unmatched"},
			{rel: ExportRelationship{Children: "sample-crams", ParentKind: "study"}, parentID: "7556", column: "reason"},
		}

		for _, testCase := range testCases {
			testCase := testCase
			convey.Convey("when "+testCase.rel.Children+" requests product-only column "+testCase.column, func() {
				result, err := client.Export(context.Background(), testCase.rel, testCase.parentID, ExportOptions{
					Columns: []string{testCase.column},
				})

				convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
				convey.So(err.Error(), convey.ShouldContainSubstring, "unknown export column")
				convey.So(err.Error(), convey.ShouldContainSubstring, `"`+testCase.column+`"`)
				convey.So(result.Columns, convey.ShouldBeNil)
				convey.So(result.Rows, convey.ShouldBeNil)
			})
		}
	})
}

func TestExportRenderFormatsD1a(t *testing.T) {
	convey.Convey("D1a.3: Given selected export rows", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExport7556Scenario(t, client.cache.DB())

		jsonResult, jsonErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7556", ExportOptions{
			Columns: []string{"name", "irods_path"},
			Format:  "json",
			Limit:   1,
		})
		jsonText, jsonRenderErr := jsonResult.Render()

		csvResult, csvErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7556", ExportOptions{
			Columns: []string{"name", "irods_path"},
			Format:  "csv",
			Limit:   1,
		})
		csvText, csvRenderErr := csvResult.Render()

		tsvResult, tsvErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7556", ExportOptions{
			Columns: []string{"name", "irods_path"},
			Limit:   1,
		})
		tsvText, tsvRenderErr := tsvResult.Render()

		convey.Convey("when each format renders, then JSON is keyed objects, CSV is comma-delimited, and default TSV is tab-delimited", func() {
			convey.So(jsonErr, convey.ShouldBeNil)
			convey.So(jsonRenderErr, convey.ShouldBeNil)
			var objects []map[string]string
			convey.So(json.Unmarshal([]byte(jsonText), &objects), convey.ShouldBeNil)
			convey.So(objects, convey.ShouldHaveLength, 1)
			convey.So(objects[0], convey.ShouldContainKey, "name")
			convey.So(objects[0], convey.ShouldContainKey, "irods_path")

			convey.So(csvErr, convey.ShouldBeNil)
			convey.So(csvRenderErr, convey.ShouldBeNil)
			convey.So(csvText, convey.ShouldContainSubstring, "name,irods_path\n")
			convey.So(csvText, convey.ShouldNotContainSubstring, "\t")

			convey.So(tsvErr, convey.ShouldBeNil)
			convey.So(tsvRenderErr, convey.ShouldBeNil)
			convey.So(tsvText, convey.ShouldContainSubstring, "name\tirods_path\n")
		})
	})
}

func TestExportStudyIRODSAppliesSharedFilterFamilyD1aC4(t *testing.T) {
	convey.Convey("C4/D1a: Given export rows spanning organism, library type, QC, and deliverable states", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportFilterScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "FILTER", ExportOptions{
			Columns:     []string{"name", "manual_qc", "irods_path"},
			FileType:    "cram",
			Organism:    "musculus",
			LibraryType: "Standard",
			QC:          "pass",
		})

		convey.Convey("when the shared filter family is applied on export, then only the intersecting product rows remain", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{{
				"filter-mus-standard-pass",
				"pass",
				"/seq/filter/50001_1#1.cram",
			}})
			convey.So(result.Total, convey.ShouldEqual, 1)
		})
	})
}

func TestExportStudySampleCRAMsAppliesSharedFilterFamilyD1aC4(t *testing.T) {
	convey.Convey("C4/D1a: Given sample CRAM rows spanning organism, library type, and QC states", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportFilterScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "sample-crams", ParentKind: "study"}, "FILTER", ExportOptions{
			Columns:     []string{"name", "accession_number", "irods_path", "merged"},
			Organism:    "musculus",
			LibraryType: "Standard",
			QC:          "pass",
		})

		convey.Convey("when the shared filter family is applied to sample-crams, then only the intersecting sample CRAM remains", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{{
				"filter-mus-standard-pass",
				"accession-1",
				"/seq/filter/50001_1#1.cram",
				"false",
			}})
			convey.So(result.Total, convey.ShouldEqual, 1)
		})
	})
}

func TestExportQCPassUsesPerProductGrainWhileSearchUsesSampleRollupD1aC4(t *testing.T) {
	convey.Convey("C4/D1a: Given one sample with a passing product and a failing product", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportSearchQCDivergenceScenario(t, client.cache.DB())

		search, searchErr := client.SearchSamplesWithOptions(context.Background(), "qcx", SampleSearchOptions{QC: qcPass}, 100, 0)
		exported, exportErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "QCX", ExportOptions{
			Columns: []string{"name", "manual_qc", "irods_path"},
			QC:      qcPass,
			Limit:   10,
		})

		convey.Convey("when --qc pass is applied to search and export, then search excludes the failed roll-up sample but export keeps its raw passing product", func() {
			convey.So(searchErr, convey.ShouldBeNil)
			convey.So(search, convey.ShouldBeEmpty)
			convey.So(exportErr, convey.ShouldBeNil)
			convey.So(exported.Rows, convey.ShouldResemble, [][]string{{
				"qcx-mixed",
				"pass",
				"/seq/qcx/62010_1#1.cram",
			}})
		})
	})
}

func TestExportRunsPopulatesAdvertisedColumnsD1a(t *testing.T) {
	convey.Convey("D1a/D1b: Given a sample with Illumina run status dates", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportRunScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "runs", ParentKind: "sample"}, "runs-sample", ExportOptions{
			Columns: []string{"id_run", "platform", "manufacturer", "run_date", "date_basis"},
			Limit:   10,
		})

		convey.Convey("when runs are exported for the sample, then advertised run metadata is populated from the run/date mirrors", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"id_run", "platform", "manufacturer", "run_date", "date_basis"})
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"61010", "Illumina", "Illumina", "2026-06-03", runDateBasisRunComplete},
				{"61011", "Illumina", "Illumina", "2026-06-04", runDateBasisRunComplete},
			})
		})
	})
}

func TestExportRunsIncludesPlatformNativeParentScopedRunsP1(t *testing.T) {
	convey.Convey("P1/export-runs: Given a study with platform-native runs", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportMultiPlatformRunScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "runs", ParentKind: "study"}, "RUNX", ExportOptions{
			Columns: []string{"id", "native_id", "id_run", "platform", "manufacturer", "run_date", "date_basis"},
			Limit:   20,
		})

		convey.Convey("when runs are exported for the study, then every platform contributes rows with authoritative date basis", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"elembio:77010", "77010", "77010", "Elembio", "Element Biosciences", "2026-06-07", runDateBasisRunComplete},
				{"illumina:61010", "61010", "61010", "Illumina", "Illumina", "2026-06-03", runDateBasisRunComplete},
				{"ont:ONTRUN-71005", "ONTRUN-71005", "", "ONT", "Oxford Nanopore", "2026-06-10", runDateBasisONTLoadTime},
				{"pacbio:pb-export-run-1", "pb-export-run-1", "", "PacBio", "PacBio", "2026-06-05", runDateBasisPacBioComplete},
				{"ultimagen:88010", "88010", "88010", "Ultimagen", "Ultima Genomics", "2026-06-09", runDateBasisRunArchived},
			})
			convey.So(result.Total, convey.ShouldEqual, 5)
		})
	})

	convey.Convey("P1/export-runs: Given a sample with a PacBio run", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportMultiPlatformRunScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "runs", ParentKind: "sample"}, "runs-pacbio", ExportOptions{
			Columns: []string{"id", "native_id", "platform", "manufacturer", "run_date", "date_basis"},
			Limit:   10,
		})

		convey.Convey("when runs are exported for the sample, then non-Illumina runs are included", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{{
				"pacbio:pb-export-run-1",
				"pb-export-run-1",
				"PacBio",
				"PacBio",
				"2026-06-05",
				runDateBasisPacBioComplete,
			}})
			convey.So(result.Total, convey.ShouldEqual, 1)
		})
	})
}

func TestExportRunsRetainsNonIlluminaIdentityRowsWithoutDatesP1(t *testing.T) {
	convey.Convey("P1/export-runs regression: Given a study with non-Illumina run identities but empty normalised dates", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportMultiPlatformRunScenario(t, client.cache.DB())
		clearExportNonIlluminaRunDates(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "runs", ParentKind: "study"}, "RUNX", ExportOptions{
			Columns: []string{"id", "native_id", "platform"},
			Limit:   20,
		})

		convey.Convey("when only identity columns are requested, then undated platform-native runs are still exported", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"elembio:77010", "77010", "Elembio"},
				{"illumina:61010", "61010", "Illumina"},
				{"ont:ONTRUN-71005", "ONTRUN-71005", "ONT"},
				{"pacbio:pb-export-run-1", "pb-export-run-1", "PacBio"},
				{"ultimagen:88010", "88010", "Ultimagen"},
			})
			convey.So(result.Total, convey.ShouldEqual, 5)
		})
	})

	convey.Convey("P1/export-runs regression: Given undated non-Illumina runs and a date export request", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportMultiPlatformRunScenario(t, client.cache.DB())
		clearExportNonIlluminaRunDates(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "runs", ParentKind: "study"}, "RUNX", ExportOptions{
			Columns: []string{"id", "native_id", "platform", "run_date", "date_basis"},
			Limit:   20,
		})

		convey.Convey("when run date columns are requested, then the missing authoritative date is reported instead of silently dropping the run", func() {
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring, `export run column "run_date" is unavailable`)
			convey.So(err.Error(), convey.ShouldContainSubstring, "elembio:77010")
			convey.So(result.Rows, convey.ShouldBeNil)
		})
	})
}

func seedExportMultiPlatformRunScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedExportRunScenario(t, db)
	seedHierarchyStudy(t, db, 710, "RUNX")
	seedManifestSampleRow(t, db, 71001, "runs-illumina", "runs-illumina-supplier", "EGAN-runs-illumina", "sanger-runs-illumina")
	seedManifestSampleRow(t, db, 71002, "runs-pacbio", "runs-pacbio-supplier", "EGAN-runs-pacbio", "sanger-runs-pacbio")
	seedManifestSampleRow(t, db, 71003, "runs-elembio", "runs-elembio-supplier", "EGAN-runs-elembio", "sanger-runs-elembio")
	seedManifestSampleRow(t, db, 71004, "runs-ultimagen", "runs-ultimagen-supplier", "EGAN-runs-ultimagen", "sanger-runs-ultimagen")
	seedManifestSampleRow(t, db, 71005, "runs-ont", "runs-ont-supplier", "EGAN-runs-ont", "sanger-runs-ont")
	for _, sampleID := range []int64{71001, 71002, 71003, 71004, 71005} {
		seedLibrarySample(t, db, "Standard", sampleID, "RUNX")
	}

	seedIseqProductMetricsMirrorRow(t, db, 7100101, 71001, 61010, 1, 1, "RUNX")
	seedExportPacBioRun(t, db)
	seedEseqProductMetricsMirrorRow(t, db, "eseq-export-77010", 77010, 71003, "RUNX")
	seedEseqRunLaneMetricsMirrorRow(t, db, 77010, map[string]time.Time{
		"run_complete": time.Date(2026, time.June, 7, 12, 0, 0, 0, time.UTC),
	})
	seedUseqProductMetricsMirrorRow(t, db, "useq-export-88010", 88010, 71004, "RUNX")
	seedUseqRunMetricsMirrorRow(t, db, 88010, runDateBasisRunArchived, map[string]time.Time{
		"run_complete": time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC),
	})
	seedExportONTRun(t, db)
	seedExportRunAggregationSyncState(t, db)
}

func clearExportNonIlluminaRunDates(t *testing.T, db *sql.DB) {
	t.Helper()

	updates := []struct {
		query string
		args  []any
	}{
		{
			query: `UPDATE pac_bio_run_well_metrics_mirror SET normalised_date = '' WHERE pac_bio_run_name = ?`,
			args:  []any{"pb-export-run-1"},
		},
		{
			query: `UPDATE eseq_run_lane_metrics_mirror SET normalised_date = '' WHERE id_run = ?`,
			args:  []any{int64(77010)},
		},
		{
			query: `UPDATE useq_run_metrics_mirror SET normalised_date = '' WHERE id_run = ?`,
			args:  []any{int64(88010)},
		},
		{
			query: `UPDATE oseq_flowcell_mirror SET normalised_date = '' WHERE experiment_name = ?`,
			args:  []any{"ONTRUN-71005"},
		},
	}
	for _, update := range updates {
		_, err := db.Exec(update.query, update.args...)
		convey.So(err, convey.ShouldBeNil)
	}
}

func TestExportNonSampleRelationshipsRejectSharedFiltersD1aC4(t *testing.T) {
	convey.Convey("C4/D1a reviewer: Given a non-sample non-iRODS export relationship", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportRunScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "runs", ParentKind: "study"}, "RUNS", ExportOptions{
			Columns: []string{"id_run"},
			QC:      "pass",
			Limit:   10,
		})

		convey.Convey("when a shared sample filter is requested, then the relationship is rejected with an actionable unsupported error", func() {
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring, "shared sample export filters are backed for iRODS/files, samples, sample-crams, and products exports")
			convey.So(err.Error(), convey.ShouldContainSubstring, "runs of study")
			convey.So(result.Rows, convey.ShouldBeNil)
		})
	})
}

func seedExportRunScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 610, "RUNS")
	seedManifestSampleRow(t, db, 31, "runs-sample", "runs-supplier", "EGAN-runs", "sanger-runs")
	seedLibrarySample(t, db, "Standard", 31, "RUNS")
	seedIseqProductMetricsMirrorRow(t, db, 61001, 31, 61010, 1, 1, "RUNS")
	seedIseqProductMetricsMirrorRow(t, db, 61002, 31, 61011, 2, 1, "RUNS")
	seedIseqRunStatusDictMirrorRow(t, db, 1, runDateBasisRunComplete)
	seedIseqRunStatusMirrorRow(t, db, 1, 61010, time.Date(2026, time.June, 3, 9, 0, 0, 0, time.UTC), 1, 0)
	seedIseqRunStatusMirrorRow(t, db, 2, 61011, time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC), 1, 0)
	seedExportSyncState(t, db)
	seedSyncStateRun(t, db, syncTableIseqRunStatus, time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC), time.Date(2026, time.June, 4, 10, 0, 0, 0, time.UTC))
	seedSyncStateRun(t, db, syncTableIseqRunStatusDict, time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC), time.Date(2026, time.June, 4, 10, 0, 0, 0, time.UTC))
}

func seedExportPacBioRun(t *testing.T, db *sql.DB) {
	t.Helper()

	complete := time.Date(2026, time.June, 5, 12, 0, 0, 0, time.UTC)
	_, err := db.Exec(
		`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_complete, run_status, well_status, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(71002),
		"pb-export-run-1",
		"A01",
		1,
		formatSyncTime(complete),
		"Complete",
		"Complete",
		formatSyncTime(complete.Add(time.Hour)),
		formatSyncDate(complete),
	)
	convey.So(err, convey.ShouldBeNil)
	seedPacBioProductMetricsMirrorRow(t, db, "pb-export-product-1", 71002, "RUNX")
}

func seedExportONTRun(t *testing.T, db *sql.DB) {
	t.Helper()

	loadTime := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)
	_, err := db.Exec(
		`INSERT INTO oseq_flowcell_mirror(id_oseq_flowcell_tmp, id_sample_tmp, id_study_lims, experiment_name, run_id, run_uuid, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(71005),
		int64(71005),
		"RUNX",
		"ONTRUN-71005",
		nil,
		"ont-run-uuid-71005",
		formatSyncTime(loadTime),
		formatSyncDate(loadTime),
	)
	convey.So(err, convey.ShouldBeNil)
}

func seedExportRunAggregationSyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	highWater := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)
	for offset, table := range []string{
		syncTablePacBioRunWellMetrics,
		syncTablePacBioProductMetrics,
		syncTableEseqRunLaneMetrics,
		syncTableEseqProductMetrics,
		syncTableUseqRunMetrics,
		syncTableUseqProductMetrics,
		syncTableOseqFlowcell,
	} {
		seedSyncStateRun(t, db, table, highWater, highWater.Add(time.Duration(offset)*time.Minute))
	}
}

func TestExportStudyProductsBoundedPageTotalAndCursorD1(t *testing.T) {
	convey.Convey("D1.1-D1.2: Given a 97-row study products export", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestStudy7568MergedCRAMScenario(t, client.cache.DB())

		first, firstErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7568", ExportOptions{
			Columns: []string{"name", "id_run", "lane", "tag_index"},
			Limit:   50,
		})
		second, secondErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7568", ExportOptions{
			Columns: []string{"name", "id_run", "lane", "tag_index"},
			Cursor:  first.NextCursor,
			Limit:   50,
		})

		convey.Convey("when the first bounded page is fetched, then it reports total size and a keyset cursor", func() {
			convey.So(firstErr, convey.ShouldBeNil)
			convey.So(first.Rows, convey.ShouldHaveLength, 50)
			convey.So(first.Total, convey.ShouldEqual, 97)
			convey.So(first.NextCursor, convey.ShouldNotBeEmpty)
			convey.So(first.Complete, convey.ShouldBeFalse)
		})

		convey.Convey("when the cursor is supplied, then Export returns the remaining product triples", func() {
			convey.So(secondErr, convey.ShouldBeNil)
			convey.So(second.Rows, convey.ShouldHaveLength, 47)
			convey.So(second.Total, convey.ShouldEqual, 97)
			convey.So(second.Rows[0], convey.ShouldResemble, []string{"7568STDY7568002", "49348", "2", "3"})
			convey.So(second.Rows[len(second.Rows)-1], convey.ShouldResemble, []string{"7568-direct-cram", "49348", "3", "99"})
			convey.So(second.NextCursor, convey.ShouldBeEmpty)
			convey.So(second.Complete, convey.ShouldBeTrue)
		})
	})
}

func TestExportStudyProductsCursorAcceptedD3(t *testing.T) {
	convey.Convey("D3.1: Given a NextCursor from a prior products page", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedManifestStudy7568MergedCRAMScenario(t, client.cache.DB())

		first, firstErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7568", ExportOptions{
			Columns: []string{"name", "id_run", "lane", "tag_index"},
			Limit:   50,
		})
		second, secondErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7568", ExportOptions{
			Columns: []string{"name", "id_run", "lane", "tag_index"},
			Cursor:  first.NextCursor,
			Limit:   50,
		})

		convey.Convey("when the cursor is supplied, then the products export succeeds by keyset continuation", func() {
			convey.So(firstErr, convey.ShouldBeNil)
			convey.So(first.NextCursor, convey.ShouldNotBeEmpty)
			convey.So(secondErr, convey.ShouldBeNil)
			convey.So(second.Rows, convey.ShouldHaveLength, 47)
			convey.So(second.Complete, convey.ShouldBeTrue)
			convey.So(second.NextCursor, convey.ShouldBeEmpty)
		})
	})
}

func TestExportStudySamplesCursorStillRejectedD3(t *testing.T) {
	convey.Convey("D3.2: Given a samples of study export", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportFilterScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "samples", ParentKind: "study"}, "FILTER", ExportOptions{
			Columns: []string{"name"},
			Cursor:  "cursor-from-products",
			Limit:   2,
		})

		convey.Convey("when a cursor is supplied, then the offset-backed export still rejects cursor continuation", func() {
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring, "cursor pagination is supported only for iRODS and products exports")
			convey.So(result.Rows, convey.ShouldBeNil)
		})
	})
}

func TestExportStudyProductsDefaultRequestUsesBoundedPageD1(t *testing.T) {
	convey.Convey("D1.3: Given a large study products export", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedLargeProductExportScenario(t, client.cache.DB(), 1500)

		result, err := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, "7799", ExportOptions{})

		convey.Convey("when the default request is fetched, then it returns the default bounded page with a continuation cursor", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldHaveLength, defaultExportAllLimit)
			convey.So(result.Total, convey.ShouldEqual, 1500)
			convey.So(result.NextCursor, convey.ShouldNotBeEmpty)
			convey.So(result.Complete, convey.ShouldBeFalse)
		})
	})
}

func TestExportStudyIRODSBoundedPageTotalAndCursorD1a(t *testing.T) {
	convey.Convey("D1a.5: Given a bounded study iRODS export page", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExport7556Scenario(t, client.cache.DB())

		first, firstErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7556", ExportOptions{
			Columns: []string{"name", "id_run", "lane", "tag_index"},
			Limit:   2,
		})
		second, secondErr := client.Export(context.Background(), ExportRelationship{Children: "irods", ParentKind: "study"}, "7556", ExportOptions{
			Columns: []string{"name", "id_run", "lane", "tag_index"},
			Cursor:  first.NextCursor,
			Limit:   2,
		})

		convey.Convey("when the first page is fetched, then it reports total size and a keyset cursor for the next page", func() {
			convey.So(firstErr, convey.ShouldBeNil)
			convey.So(first.Rows, convey.ShouldHaveLength, 2)
			convey.So(first.Total, convey.ShouldEqual, 3)
			convey.So(first.Total, convey.ShouldBeGreaterThanOrEqualTo, len(first.Rows))
			convey.So(first.NextCursor, convey.ShouldNotBeEmpty)
			convey.So(first.Complete, convey.ShouldBeFalse)
		})

		convey.Convey("when the next cursor is supplied, then Export continues after the previous canonical tuple", func() {
			convey.So(secondErr, convey.ShouldBeNil)
			convey.So(second.Rows, convey.ShouldResemble, [][]string{{"sample-beta", "52554", "2", "1"}})
			convey.So(second.NextCursor, convey.ShouldBeEmpty)
			convey.So(second.Complete, convey.ShouldBeTrue)
		})
	})
}

func TestExportNonIRODSPaginationDoesNotAdvertiseInvalidCursorD1a(t *testing.T) {
	convey.Convey("D1a reviewer: Given an offset-backed samples export with more rows than the first page", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportFilterScenario(t, client.cache.DB())

		first, firstErr := client.Export(context.Background(), ExportRelationship{Children: "samples", ParentKind: "study"}, "FILTER", ExportOptions{
			Columns: []string{"name"},
			Limit:   2,
		})
		cursorResult, cursorErr := client.Export(context.Background(), ExportRelationship{Children: "samples", ParentKind: "study"}, "FILTER", ExportOptions{
			Columns: []string{"name"},
			Cursor:  "2",
			Limit:   2,
		})
		allResult, allErr := client.Export(context.Background(), ExportRelationship{Children: "samples", ParentKind: "study"}, "FILTER", ExportOptions{
			Columns: []string{"name"},
			All:     true,
			Limit:   2,
		})
		var allRows [][]string
		allCount, allRenderErr := allResult.ForEachRow(context.Background(), func(row []string) error {
			allRows = append(allRows, append([]string(nil), row...))

			return nil
		})

		convey.Convey("when the first page is fetched, then no keyset cursor is advertised for the non-iRODS relationship", func() {
			convey.So(firstErr, convey.ShouldBeNil)
			convey.So(first.Rows, convey.ShouldHaveLength, 2)
			convey.So(first.Total, convey.ShouldEqual, 4)
			convey.So(first.Complete, convey.ShouldBeFalse)
			convey.So(first.NextCursor, convey.ShouldBeEmpty)
		})

		convey.Convey("when Cursor is requested, then Export rejects the unsupported continuation mode before emitting rows", func() {
			convey.So(errors.Is(cursorErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(cursorErr.Error(), convey.ShouldContainSubstring, "cursor pagination is supported only for iRODS and products exports")
			convey.So(cursorResult.Rows, convey.ShouldBeNil)
		})

		convey.Convey("when All is requested, then Export streams every offset-backed row by internal pages", func() {
			convey.So(allErr, convey.ShouldBeNil)
			convey.So(allRenderErr, convey.ShouldBeNil)
			convey.So(allCount, convey.ShouldEqual, 4)
			convey.So(allResult.Rows, convey.ShouldBeNil)
			convey.So(allResult.Total, convey.ShouldEqual, -1)
			convey.So(allResult.Complete, convey.ShouldBeTrue)
			convey.So(allRows, convey.ShouldResemble, [][]string{
				{"filter-human-standard-pass"},
				{"filter-mus-bespoke-pass"},
				{"filter-mus-standard-fail"},
				{"filter-mus-standard-pass"},
			})
		})
	})
}

func TestExportSampleStudiesAppliesBoundedPageD1a(t *testing.T) {
	convey.Convey("D1a reviewer: Given a sample linked to multiple studies", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()

		seedManifestSampleRow(t, client.cache.DB(), 91, "sample-linked-studies", "supplier-linked", "EGAN-linked", "sanger-linked")
		seedHierarchyStudy(t, client.cache.DB(), 652, "6570")
		seedHierarchyStudy(t, client.cache.DB(), 651, "6569")
		seedHierarchyStudy(t, client.cache.DB(), 650, "6568")
		seedLibrarySample(t, client.cache.DB(), "Standard", 91, "6568")
		seedLibrarySample(t, client.cache.DB(), "Chromium", 91, "6569")
		seedLibrarySample(t, client.cache.DB(), "Bespoke", 91, "6570")

		result, err := client.Export(context.Background(), ExportRelationship{Children: "studies", ParentKind: "sample"}, "sample-linked-studies", ExportOptions{
			Columns: []string{"id_study_lims", "name"},
			Limit:   1,
			Offset:  1,
		})
		aliasResult, aliasErr := client.Export(context.Background(), ExportRelationship{Children: "studies", ParentKind: "sample"}, "supplier-linked", ExportOptions{
			Columns: []string{"id_study_lims", "name"},
			Limit:   1,
			Offset:  1,
		})

		convey.Convey("when the second bounded page is fetched by canonical name or alias, then only that page is returned while total remains unbounded", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Rows, convey.ShouldResemble, [][]string{{"6569", "Study 6569"}})
			convey.So(result.Total, convey.ShouldEqual, 3)
			convey.So(result.Complete, convey.ShouldBeFalse)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)

			convey.So(aliasErr, convey.ShouldBeNil)
			convey.So(aliasResult.Rows, convey.ShouldResemble, result.Rows)
			convey.So(aliasResult.Total, convey.ShouldEqual, result.Total)
			convey.So(aliasResult.Complete, convey.ShouldEqual, result.Complete)
			convey.So(aliasResult.NextCursor, convey.ShouldEqual, result.NextCursor)
		})
	})
}

func TestExportSamplesAppliesSharedFilterFamilyD1aC4(t *testing.T) {
	convey.Convey("C4/D1a reviewer: Given study samples spanning organism, library type, raw QC, and deliverable states", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportFilterScenario(t, client.cache.DB())
		deliverablesOnly := true

		result, err := client.Export(context.Background(), ExportRelationship{Children: "samples", ParentKind: "study"}, "FILTER", ExportOptions{
			Columns:          []string{"name", "common_name", "supplier_name"},
			Organism:         "musculus",
			LibraryType:      "Standard",
			QC:               "pass",
			DeliverablesOnly: &deliverablesOnly,
			Limit:            10,
		})

		convey.Convey("when the shared filter family is applied to samples, then only the intersecting sample rows remain", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"name", "common_name", "supplier_name"})
			convey.So(result.Rows, convey.ShouldResemble, [][]string{{
				"filter-mus-standard-pass",
				"Mus Musculus",
				"supplier-1",
			}})
			convey.So(result.Total, convey.ShouldEqual, 1)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)
		})
	})
}

func TestExportStudySampleCramsBackedByIRODSMirrorD1a(t *testing.T) {
	convey.Convey("D1a reviewer: Given study CRAMS has merged, single-lane, control, and non-cram iRODS rows", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportSampleCRAMScenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "sample-crams", ParentKind: "study"}, "CRAMS", ExportOptions{})
		projectedResult, projectedErr := client.Export(context.Background(), ExportRelationship{Children: "sample-crams", ParentKind: "study"}, "CRAMS", ExportOptions{
			Columns: []string{
				"supplier_name",
				"sanger_sample_id",
				"accession_number",
				"study_accession_number",
				"id_study_lims",
				"manual_qc",
				"id_run",
				"lane",
				"tag_index",
				"platform",
				"created",
				"deliverable",
				"id_product",
				"id_sample_tmp",
				"collection",
				"data_object",
				"irods_path",
				"merged",
			},
			Limit: 1,
		})
		includeControls := false
		includeControlsResult, includeControlsErr := client.Export(context.Background(), ExportRelationship{Children: "sample-crams", ParentKind: "study"}, "CRAMS", ExportOptions{
			DeliverablesOnly: &includeControls,
		})

		convey.Convey("when sample-crams are exported by default, then deliverable CRAM rows are returned with merged objects preferred", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"name", "accession_number", "irods_path", "merged"})
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"cram-merged", "EGAN-merged", "/seq/crams/merged/49348_1-2#1.cram", "true"},
				{"cram-null-deliverable", "EGAN-null", "/seq/crams/null/pacbio.cram", "false"},
				{"cram-single", "EGAN-single", "/seq/crams/single/52553_1#1.cram", "false"},
			})
			convey.So(result.Total, convey.ShouldEqual, 3)
			convey.So(result.Complete, convey.ShouldBeTrue)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)
		})

		convey.Convey("when sample-crams selects file export columns, then it projects the chosen CRAM row with canonical names", func() {
			convey.So(projectedErr, convey.ShouldBeNil)
			convey.So(projectedResult.Columns, convey.ShouldResemble, []string{
				"supplier_name",
				"sanger_sample_id",
				"accession_number",
				"study_accession_number",
				"id_study_lims",
				"manual_qc",
				"id_run",
				"lane",
				"tag_index",
				"platform",
				"created",
				"deliverable",
				"id_product",
				"id_sample_tmp",
				"collection",
				"data_object",
				"irods_path",
				"merged",
			})
			convey.So(projectedResult.Rows, convey.ShouldResemble, [][]string{{
				"supplier-merged",
				"sanger-merged",
				"EGAN-merged",
				"EGAS0000CRAMS",
				"CRAMS",
				"pending",
				"0",
				"0",
				"0",
				"illumina",
				"2026-07-01T08:30:00Z",
				"true",
				"merged-composite",
				"102",
				"/seq/crams/merged",
				"49348_1-2#1.cram",
				"/seq/crams/merged/49348_1-2#1.cram",
				"true",
			}})
			convey.So(projectedResult.Total, convey.ShouldEqual, 3)
		})

		convey.Convey("when removed hidden sample-crams column aliases are selected, then Export rejects them and lists canonical names", func() {
			for _, alias := range []string{"ega_id", "irods_cram_path"} {
				aliasResult, aliasErr := client.Export(context.Background(), ExportRelationship{Children: "sample-crams", ParentKind: "study"}, "CRAMS", ExportOptions{
					Columns: []string{alias},
					Limit:   1,
				})

				convey.So(errors.Is(aliasErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
				convey.So(aliasErr.Error(), convey.ShouldContainSubstring, `unknown export column "`+alias+`"`)
				convey.So(aliasErr.Error(), convey.ShouldContainSubstring, "accession_number")
				convey.So(aliasErr.Error(), convey.ShouldContainSubstring, "irods_path")
				convey.So(aliasResult.Columns, convey.ShouldBeNil)
				convey.So(aliasResult.Rows, convey.ShouldBeNil)
			}
		})

		convey.Convey("when controls are explicitly included, then the non-deliverable-only CRAM is restored", func() {
			convey.So(includeControlsErr, convey.ShouldBeNil)
			convey.So(includeControlsResult.Rows, convey.ShouldResemble, [][]string{
				{"cram-control-only", "EGAN-control", "/seq/crams/control/52554_1#1.cram", "false"},
				{"cram-merged", "EGAN-merged", "/seq/crams/merged/49348_1-2#1.cram", "true"},
				{"cram-null-deliverable", "EGAN-null", "/seq/crams/null/pacbio.cram", "false"},
				{"cram-single", "EGAN-single", "/seq/crams/single/52553_1#1.cram", "false"},
			})
			convey.So(includeControlsResult.Total, convey.ShouldEqual, 4)
		})
	})
}

func TestExportStudyUsersHonoursRoleFilterG3(t *testing.T) {
	convey.Convey("G3: Given study 7568 has owner, manager and follower users", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedStudyUsersInverseFixture(t, client.cache)

		result, err := client.Export(context.Background(), ExportRelationship{Children: "users", ParentKind: "study"}, "7568", ExportOptions{
			Role:  "owner,manager",
			Limit: 100,
		})

		convey.Convey("when users of the study are exported with role=owner,manager, then only those role rows are printed", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"role", "name", "login", "email"})
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"manager", "Maya Manager", "mm1", "mm1@sanger.ac.uk"},
				{"owner", "Olive Owner", "oo1", "oo1@sanger.ac.uk"},
			})
			convey.So(result.Total, convey.ShouldEqual, 2)
		})
	})
}
