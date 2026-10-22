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
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

const (
	libraryTypeStudy    = "LTYPE"
	libraryTypeRun      = 61000
	libraryTypeStandard = "Standard"
	libraryTypeChromium = "Chromium single cell"
	libraryTypeBespoke  = "Bespoke"
)

// seedExportLibraryTypeScenario models study LTYPE:
//   - LT-alpha (51): flowcell type Standard on products 61000/1/1, 61000/2/1
//     and its merged product 61000/0/0, plus a second 61000/2/1 product row on
//     a Bespoke flowcell, so that lane has two disagreeing types.
//   - LT-beta (52): product 61000/1/2 on a flowcell with no library type.
//   - LT-gamma (53): product 61000/1/3 and merged 61000/0/0 on a Chromium
//     flowcell, while library_samples lists Standard and Bespoke for it, so a
//     library_samples-derived value would be wrong.
//   - LT-pacbio (54): a PacBio CRAM, whose product has no iseq_product_metrics
//     row.
//
// The two merged products share 61000/0/0, so the products row for that
// triple groups two disagreeing types.
func seedExportLibraryTypeScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 950, libraryTypeStudy)
	seedManifestSampleRow(t, db, 51, "LT-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha")
	seedManifestSampleRow(t, db, 52, "LT-beta", "supplier-beta", "EGAN-beta", "sanger-beta")
	seedManifestSampleRow(t, db, 53, "LT-gamma", "supplier-gamma", "EGAN-gamma", "sanger-gamma")
	seedManifestSampleRow(t, db, 54, "LT-pacbio", "supplier-pacbio", "EGAN-pacbio", "sanger-pacbio")
	seedLibrarySample(t, db, libraryTypeStandard, 51, libraryTypeStudy)
	seedLibrarySample(t, db, libraryTypeStandard, 52, libraryTypeStudy)
	seedLibrarySample(t, db, libraryTypeStandard, 53, libraryTypeStudy)
	seedLibrarySample(t, db, libraryTypeBespoke, 53, libraryTypeStudy)
	seedLibrarySample(t, db, "PacBio_HiFi", 54, libraryTypeStudy)

	seedLibraryTypeFlowcell(t, db, 51, 51, sql.NullString{String: libraryTypeStandard, Valid: true})
	seedLibraryTypeFlowcell(t, db, 52, 52, sql.NullString{})
	seedLibraryTypeFlowcell(t, db, 53, 53, sql.NullString{String: libraryTypeChromium, Valid: true})
	seedLibraryTypeFlowcell(t, db, 59, 51, sql.NullString{String: libraryTypeBespoke, Valid: true})

	seedLibraryTypeProduct(t, db, "5101", 51, 51, 1, 1)
	seedLibraryTypeProduct(t, db, "5102", 51, 51, 2, 1)
	seedLibraryTypeProduct(t, db, "5199", 59, 51, 2, 1)
	seedLibraryTypeProduct(t, db, "5111", 51, 51, 0, 0)
	seedLibraryTypeProduct(t, db, "5201", 52, 52, 1, 2)
	seedLibraryTypeProduct(t, db, "5301", 53, 53, 1, 3)
	seedLibraryTypeProduct(t, db, "5311", 53, 53, 0, 0)

	deliverable := sql.NullInt64{Int64: 1, Valid: true}
	for _, row := range []exportIRODSSeedRow{
		{IDSeqProductLocation: 1, IDProduct: "5101", Collection: "/seq/lt", FileName: "61000_1#1.cram", IDSampleTmp: 51, IDRun: libraryTypeRun, Position: 1, TagIndex: 1, IsDeliverable: deliverable},
		{IDSeqProductLocation: 2, IDProduct: "5111", Collection: "/seq/lt", FileName: "61000_1-2#1.cram", IDSampleTmp: 51, IDRun: libraryTypeRun, IsDeliverable: deliverable, Merged: true},
		{IDSeqProductLocation: 3, IDProduct: "5201", Collection: "/seq/lt", FileName: "61000_1#2.cram", IDSampleTmp: 52, IDRun: libraryTypeRun, Position: 1, TagIndex: 2, IsDeliverable: deliverable},
		{IDSeqProductLocation: 4, IDProduct: "5301", Collection: "/seq/lt", FileName: "61000_1#3.cram", IDSampleTmp: 53, IDRun: libraryTypeRun, Position: 1, TagIndex: 3, IsDeliverable: deliverable},
		{IDSeqProductLocation: 5, IDProduct: "pb-541", Collection: "/seq/pacbio", FileName: "pacbio-541.cram", IDSampleTmp: 54, Platform: "pacbio"},
	} {
		row.StudyID = libraryTypeStudy
		seedExportIRODSRow(t, db, row)
	}

	seedExportSyncState(t, db)
}

func seedLibraryTypeFlowcell(t *testing.T, db *sql.DB, idFlowcell, idSampleTmp int64, pipelineIDLims sql.NullString) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_flowcell_mirror(id_iseq_flowcell_tmp, entity_type, pipeline_id_lims, id_sample_tmp, id_study_tmp) VALUES (?, ?, ?, ?, ?)`,
		idFlowcell, "library_indexed", pipelineIDLims, idSampleTmp, 950,
	)
	if err != nil {
		t.Fatalf("seedLibraryTypeFlowcell(): %v", err)
	}
}

func seedLibraryTypeProduct(t *testing.T, db *sql.DB, idProduct string, idFlowcell, idSampleTmp int64, position, tagIndex int) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idProduct, idFlowcell, libraryTypeRun, position, tagIndex, idSampleTmp, libraryTypeStudy, 1, 1, 1,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedLibraryTypeProduct(): %v", err)
	}
}

func exportStudyRowsForTest(client *Client, children string, opts ExportOptions) (ExportResult, [][]string, error) {
	opts.All = true
	result, err := client.Export(context.Background(), ExportRelationship{Children: children, ParentKind: "study"}, libraryTypeStudy, opts)
	if err != nil {
		return result, nil, err
	}

	rows := make([][]string, 0)
	_, err = result.ForEachRow(context.Background(), func(row []string) error {
		rows = append(rows, row)

		return nil
	})

	return result, rows, err
}

func TestExportLibraryTypeColumn(t *testing.T) {
	convey.Convey("Given study LTYPE with per-product flowcell library types in a SQLite cache", t, func() {
		client, cleanup := newExportTestClient(t)
		defer cleanup()
		seedExportLibraryTypeScenario(t, client.cache.DB())

		assertExportLibraryTypeColumn(client)
	})
}

func TestRealMySQLExportLibraryTypeColumnUsesPrimaryKeyJoins(t *testing.T) {
	client := openThrowawayMySQLExportClientForTest(t)
	seedExportLibraryTypeScenario(t, client.cache.DB())
	seedLibraryTypeExplainFiller(t, client.cache.DB())

	convey.Convey("Given study LTYPE with per-product flowcell library types in a throwaway MySQL cache", t, func() {
		assertExportLibraryTypeColumn(client)

		convey.Convey("when EXPLAIN runs each export with library_type, then the flowcell joins are primary-key lookups and the scans keep their indexes", func() {
			assertLibraryTypeExportsIndexServed(t, client)
		})
	})
}

func assertExportLibraryTypeColumn(client *Client) {
	convey.Convey("when products are exported with library_type, then each product row carries its flowcell's type, blank where grouped rows disagree or the type is unknown", func() {
		result, rows, err := exportStudyRowsForTest(client, "products", ExportOptions{Columns: []string{"name", "id_run", "lane", "tag_index", "library_type"}})

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Columns, convey.ShouldResemble, []string{"name", "id_run", "lane", "tag_index", "library_type"})
		convey.So(rows, convey.ShouldResemble, [][]string{
			{"LT-alpha", "61000", "0", "0", ""},
			{"LT-alpha", "61000", "1", "1", libraryTypeStandard},
			{"LT-beta", "61000", "1", "2", ""},
			{"LT-gamma", "61000", "1", "3", libraryTypeChromium},
			{"LT-alpha", "61000", "2", "1", ""},
		})
	})

	convey.Convey("when products are exported with and without library_type and an iRODS attachment, then the row count and Total do not change", func() {
		plain, plainErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, libraryTypeStudy, ExportOptions{
			Columns: []string{"name", "id_run", "lane", "tag_index", "irods_path"},
		})
		typed, typedErr := client.Export(context.Background(), ExportRelationship{Children: "products", ParentKind: "study"}, libraryTypeStudy, ExportOptions{
			Columns: []string{"name", "id_run", "lane", "tag_index", "irods_path", "pipeline_id_lims"},
		})

		convey.So(plainErr, convey.ShouldBeNil)
		convey.So(typedErr, convey.ShouldBeNil)
		convey.So(typed.Columns[len(typed.Columns)-1], convey.ShouldEqual, "library_type")
		convey.So(typed.Total, convey.ShouldEqual, plain.Total)
		convey.So(len(typed.Rows), convey.ShouldEqual, len(plain.Rows))
		convey.So(len(typed.Rows), convey.ShouldEqual, 5)
		for index, row := range typed.Rows {
			convey.So(row[:len(row)-1], convey.ShouldResemble, plain.Rows[index])
		}
		convey.So(typed.Rows[1][5], convey.ShouldEqual, libraryTypeStandard)
	})

	convey.Convey("when iRODS objects are exported with library_type, then Illumina objects (merged included) carry their product's type and the PacBio object is blank", func() {
		_, rows, err := exportStudyRowsForTest(client, "irods", ExportOptions{Columns: []string{"irods_path", "merged", "library_type"}})

		convey.So(err, convey.ShouldBeNil)
		convey.So(rows, convey.ShouldResemble, [][]string{
			{"/seq/pacbio/pacbio-541.cram", "false", ""},
			{"/seq/lt/61000_1-2#1.cram", "true", libraryTypeStandard},
			{"/seq/lt/61000_1#1.cram", "false", libraryTypeStandard},
			{"/seq/lt/61000_1#2.cram", "false", ""},
			{"/seq/lt/61000_1#3.cram", "false", libraryTypeChromium},
		})
	})

	convey.Convey("when sample-crams are exported with library_type, then each sample's chosen CRAM carries its product's type, not a library_samples type", func() {
		_, rows, err := exportStudyRowsForTest(client, "sample-crams", ExportOptions{Columns: []string{"name", "irods_path", "library_type"}})

		convey.So(err, convey.ShouldBeNil)
		convey.So(rows, convey.ShouldResemble, [][]string{
			{"LT-alpha", "/seq/lt/61000_1-2#1.cram", libraryTypeStandard},
			{"LT-beta", "/seq/lt/61000_1#2.cram", ""},
			{"LT-gamma", "/seq/lt/61000_1#3.cram", libraryTypeChromium},
			{"LT-pacbio", "/seq/pacbio/pacbio-541.cram", ""},
		})
	})

	convey.Convey("when a sample's lanes are exported with library_type, then each lane carries its flowcell type, blank where two product rows disagree, one row per lane", func() {
		alpha, alphaRows, alphaErr := exportRowsForTest(client, "lanes", "LT-alpha", ExportOptions{All: true, Columns: []string{"id_run", "lane", "tag_index", "library_type"}})
		_, gammaRows, gammaErr := exportRowsForTest(client, "lanes", "LT-gamma", ExportOptions{All: true, Columns: []string{"lane", "tag_index", "library_type"}})

		convey.So(alphaErr, convey.ShouldBeNil)
		convey.So(alpha.Columns, convey.ShouldResemble, []string{"id_run", "lane", "tag_index", "library_type"})
		convey.So(alphaRows, convey.ShouldResemble, [][]string{
			{"61000", "0", "0", libraryTypeStandard},
			{"61000", "1", "1", libraryTypeStandard},
			{"61000", "2", "1", ""},
		})
		convey.So(gammaErr, convey.ShouldBeNil)
		convey.So(gammaRows, convey.ShouldResemble, [][]string{
			{"0", "0", libraryTypeChromium},
			{"1", "3", libraryTypeChromium},
		})
	})

	convey.Convey("when the default columns are used, then library_type is not among them", func() {
		for _, children := range []string{"products", "irods", "sample-crams"} {
			result, _, err := exportStudyRowsForTest(client, children, ExportOptions{})

			convey.So(err, convey.ShouldBeNil)
			convey.So(result.Columns, convey.ShouldNotContain, "library_type")
		}

		lanes, _, err := exportRowsForTest(client, "lanes", "LT-alpha", ExportOptions{All: true})
		convey.So(err, convey.ShouldBeNil)
		convey.So(lanes.Columns, convey.ShouldNotContain, "library_type")
	})
}

// seedLibraryTypeExplainFiller adds decoy flowcells, products and iRODS rows
// so MySQL statistics favour index access over scanning tiny tables.
func seedLibraryTypeExplainFiller(t *testing.T, db *sql.DB) {
	t.Helper()

	for i := range 300 {
		id := int64(10_000 + i)
		seedLibraryTypeFlowcell(t, db, id, id, sql.NullString{String: libraryTypeStandard, Valid: true})
		_, err := db.Exec(
			`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"filler-"+formatInt(id), id, 62000+i/96, i%8+1, i%12+1, id, "LTYPE-decoy", 1, 1, 1,
			formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
		)
		if err != nil {
			t.Fatalf("seed library type filler product %d: %v", i, err)
		}
		seedExportIRODSRow(t, db, exportIRODSSeedRow{
			IDSeqProductLocation: 1000 + id, IDProduct: "filler-" + formatInt(id), FileName: formatInt(id) + ".cram",
			IDSampleTmp: id, StudyID: "LTYPE-decoy", IDRun: 62000 + i/96, Position: i%8 + 1, TagIndex: i%12 + 1,
		})
	}

	for _, table := range []string{"iseq_flowcell_mirror", "iseq_product_metrics_mirror", "seq_product_irods_locations_mirror", "sample_mirror"} {
		if _, err := db.Exec("ANALYZE TABLE " + table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
}

func assertLibraryTypeExportsIndexServed(t *testing.T, client *Client) {
	t.Helper()

	db := client.cache.DB()
	columns := []exportColumn{{Name: "irods_path"}, libraryTypeExportColumn()}

	productQuery, productArgs := exportProductListQuery(exportProductQueryInput{studyID: libraryTypeStudy, withLibraryType: true, limit: 100})
	plans := explainPlanRows(t, db, productQuery, productArgs...)
	assertExplainUsesKey(plans, "ipm", "iseq_product_metrics_mirror_id_study_lims_id_run_position_idx", "ipm_mirror_study_sample_product_qc_idx")
	assertLibraryTypeJoinUsesPrimaryKey(plans, "lt_ifc")

	irodsQuery, irodsArgs, err := exportIRODSPageQuery(exportIRODSQueryInput{parentKind: "study", parentValue: libraryTypeStudy, columns: columns, normalisedFile: "cram", limit: 100})
	convey.So(err, convey.ShouldBeNil)
	plans = explainPlanRows(t, db, irodsQuery, irodsArgs...)
	assertExplainUsesKey(plans, "spi", "spi_mirror_study_lims_export_idx")
	spi, _ := findExplainPlanRow(plans, "spi")
	convey.So(strings.ToLower(spi.extra), convey.ShouldNotContainSubstring, "filesort")
	assertLibraryTypeProductJoinIndexed(plans)
	assertLibraryTypeJoinUsesPrimaryKey(plans, "lt_ifc")

	cramQuery, cramArgs := exportSampleCRAMPageQuery(exportSampleCRAMQueryInput{studyID: libraryTypeStudy, withLibraryType: true, normalisedFile: "cram", limit: 100})
	plans = explainPlanRows(t, db, cramQuery, cramArgs...)
	assertLibraryTypeProductJoinIndexed(plans)
	assertLibraryTypeJoinUsesPrimaryKey(plans, "lt_ifc")

	lanesFrom, lanesArgs := exportLanesFrom(singleSampleScope(51), true)
	plans = explainPlanRows(t, db, `SELECT lanes.id_run, lanes.library_type`+lanesFrom, lanesArgs...)
	assertExplainUsesKey(plans, "ipm", "ipm_mirror_sample_run_position_tag_idx", "ipm_mirror_sample_qc_idx")
	assertLibraryTypeJoinUsesPrimaryKey(plans, "lt_ifc")
}

// assertLibraryTypeProductJoinIndexed asserts the iRODS object's product row
// is found by product id: through the primary key, or through the product-id
// index a large cold load keeps while the primary key is deferred.
func assertLibraryTypeProductJoinIndexed(plans []mysqlExplainPlanRow) {
	plan, ok := findExplainPlanRow(plans, "lt_ipm")
	convey.So(ok, convey.ShouldBeTrue)
	convey.So(plan.key, convey.ShouldBeIn, []string{"PRIMARY", "ipm_mirror_iseq_product_idx"})
	convey.So(strings.ToLower(plan.scanType), convey.ShouldBeIn, []string{"eq_ref", "ref"})
}

// assertLibraryTypeJoinUsesPrimaryKey asserts the alias is joined one row per
// outer row through its primary key, so the join can neither scan nor fan out.
func assertLibraryTypeJoinUsesPrimaryKey(plans []mysqlExplainPlanRow, alias string) {
	plan, ok := findExplainPlanRow(plans, alias)
	convey.So(ok, convey.ShouldBeTrue)
	convey.So(plan.key, convey.ShouldEqual, "PRIMARY")
	convey.So(strings.ToLower(plan.scanType), convey.ShouldEqual, "eq_ref")
}
