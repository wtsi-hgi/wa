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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

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

		convey.Convey("when created is selected with created-desc and a half-open window, then only in-window rows render newest-first", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"id_product", "created"})
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"inside-window", formatSyncTime(inside)},
				{"at-since", formatSyncTime(since)},
			})
			convey.So(result.Total, convey.ShouldEqual, 2)
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
		{IDSeqProductLocation: 3, IDProduct: "4934812", Collection: "/seq/illumina/runs/49/49348/lane1-2/plex1", FileName: "49348_1-2#1.cram", IDSampleTmp: 4934800, StudyID: "7568", IDRun: 0, Position: 0, TagIndex: 0, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}, Merged: true},
		{IDSeqProductLocation: 4, IDProduct: "4934899", Collection: "/seq/illumina/runs/composite/multi-run", FileName: "multi-run#1.cram", IDSampleTmp: 4934800, StudyID: "7568", IDRun: 0, Position: 0, TagIndex: 0, IsDeliverable: sql.NullInt64{Int64: 1, Valid: true}, Merged: true},
	} {
		seedExportIRODSRow(t, db, row)
	}
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

func TestExportStudyIRODSCramColumnsAliasAndDeliverablesD1a(t *testing.T) {
	convey.Convey("D1a.1: Given study 7556 with mixed iRODS rows", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExport7556Scenario(t, client.cache.DB())

		result, err := client.Export(context.Background(), ExportRelationship{Children: "files", ParentKind: "study"}, "EGAS00007556", ExportOptions{
			Columns:  []string{"supplier_sample_name", "study_accession_number", "sanger_sample_id", "manual_qc", "irods_path"},
			FileType: "cram",
		})

		convey.Convey("when the files alias export runs, then supplier_sample_name is canonicalised, controls/non-crams are dropped, and merged CRAMs sort first", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"supplier_name", "study_accession_number", "sanger_sample_id", "manual_qc", "irods_path"})
			convey.So(result.Rows, convey.ShouldHaveLength, 3)
			convey.So(result.Total, convey.ShouldEqual, 3)
			convey.So(result.Complete, convey.ShouldBeTrue)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)
			convey.So(result.Rows[0][0], convey.ShouldEqual, "supplier-beta")
			convey.So(result.Rows[0][1], convey.ShouldEqual, "EGAS00007556")
			convey.So(result.Rows[0][2], convey.ShouldEqual, "sanger-beta")
			convey.So(result.Rows[0][3], convey.ShouldEqual, "pass")
			convey.So(result.Rows[0][4], convey.ShouldContainSubstring, "/lane1-2/plex1/49348_1-2#1.cram")

			nonCramPaths := 0
			for _, row := range result.Rows {
				if !strings.HasSuffix(row[4], ".cram") {
					nonCramPaths++
				}
			}
			convey.So(nonCramPaths, convey.ShouldEqual, 0)
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
			convey.So(err.Error(), convey.ShouldContainSubstring, "shared sample export filters are backed for iRODS/files, samples, and sample-crams exports")
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

		convey.Convey("when the first page is fetched, then no keyset cursor is advertised for the non-iRODS relationship", func() {
			convey.So(firstErr, convey.ShouldBeNil)
			convey.So(first.Rows, convey.ShouldHaveLength, 2)
			convey.So(first.Total, convey.ShouldEqual, 4)
			convey.So(first.Complete, convey.ShouldBeFalse)
			convey.So(first.NextCursor, convey.ShouldBeEmpty)
		})

		convey.Convey("when Cursor or All is requested, then Export rejects the unsupported continuation mode before emitting rows", func() {
			convey.So(errors.Is(cursorErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(cursorErr.Error(), convey.ShouldContainSubstring, "cursor pagination is supported only for iRODS exports")
			convey.So(cursorResult.Rows, convey.ShouldBeNil)

			convey.So(errors.Is(allErr, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(allErr.Error(), convey.ShouldContainSubstring, "--all streaming is supported only for iRODS exports")
			convey.So(allResult.Rows, convey.ShouldBeNil)
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
		includeControls := false
		includeControlsResult, includeControlsErr := client.Export(context.Background(), ExportRelationship{Children: "sample-crams", ParentKind: "study"}, "CRAMS", ExportOptions{
			DeliverablesOnly: &includeControls,
		})

		convey.Convey("when sample-crams are exported by default, then deliverable CRAM rows are returned with merged objects preferred", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldResemble, []string{"name", "ega_id", "irods_cram_path", "merged"})
			convey.So(result.Rows, convey.ShouldResemble, [][]string{
				{"cram-merged", "EGAN-merged", "/seq/crams/merged/49348_1-2#1.cram", "true"},
				{"cram-null-deliverable", "EGAN-null", "/seq/crams/null/pacbio.cram", "false"},
				{"cram-single", "EGAN-single", "/seq/crams/single/52553_1#1.cram", "false"},
			})
			convey.So(result.Total, convey.ShouldEqual, 3)
			convey.So(result.Complete, convey.ShouldBeTrue)
			convey.So(result.NextCursor, convey.ShouldBeEmpty)
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
