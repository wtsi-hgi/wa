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
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
	"github.com/smartystreets/goconvey/convey"
	_ "modernc.org/sqlite"
)

// a4MirrorTables enumerates the platform-coverage / tracking / run-status mirror
// tables added by A4, with the secondary-index column tuples each must declare
// (in the comma-joined, sorted form parseSchemaShape stores). It is the single
// source of truth shared by the A4 existence, cross-dialect-equality and
// per-table assertions below.
var a4MirrorTables = map[string][]string{
	"pac_bio_product_metrics_mirror":     {"id_pac_bio_rw_metrics_tmp", "id_sample_tmp", "id_study_lims", "id_study_lims,id_sample_tmp,id_pac_bio_product,qc"},
	"pac_bio_run_well_metrics_mirror":    {"normalised_date", "pac_bio_run_name,well_label"},
	"eseq_product_metrics_mirror":        {"id_run", "id_sample_tmp", "id_study_lims", "id_study_lims,id_sample_tmp,id_eseq_product,qc"},
	"eseq_run_mirror":                    {"run_name"},
	"eseq_run_lane_metrics_mirror":       {"id_run", "normalised_date"},
	"useq_product_metrics_mirror":        {"id_run", "id_sample_tmp", "id_study_lims", "id_study_lims,id_sample_tmp,id_useq_product,qc", "id_useq_wafer_tmp"},
	"useq_run_metrics_mirror":            {"normalised_date", "run_name"},
	"oseq_flowcell_mirror":               {"experiment_name", "id_sample_tmp", "id_study_lims", "last_updated", "normalised_date"},
	"iseq_run_status_mirror":             {"id_run", "id_run,date", "normalised_date,id_run_status_dict,id_run"},
	"iseq_run_status_dict_mirror":        nil,
	"seq_ops_tracking_per_sample_mirror": {"id_sample_lims", "sanger_sample_name", "study_id", "study_id,id_sample_lims"},
}

// studyUsersMirrorColumns is the column set study_users_mirror declares in both
// dialects (id_study_users_tmp INTEGER PK + the six text/integer columns the
// study_users source carries), with the normalised type family parseSchemaShape
// records. It is the single source of truth shared by the A1 sqlite-shape,
// mysql-shape and cross-dialect-equality assertions below.
var studyUsersMirrorColumns = map[string]string{
	"id_study_users_tmp": "integer",
	"id_study_tmp":       "integer",
	"role":               "text",
	"login":              "text",
	"email":              "text",
	"name":               "text",
	"last_updated":       "text",
}

// studyUsersMirrorIndexes is the sorted, comma-joined column list of the five
// study_users_mirror secondary indexes, in the form parseSchemaShape stores.
var studyUsersMirrorIndexes = []string{"email", "id_study_tmp", "login", "name", "role"}

var iseqFlowcellMirrorSchemaColumns = map[string]string{
	"id_iseq_flowcell_tmp": "integer",
	"entity_type":          "text",
	"pipeline_id_lims":     "text",
	"id_sample_tmp":        "integer",
	"id_study_tmp":         "integer",
}

// a2NewIndexColumns maps each table A2 adds a single-column index to, to that
// index's column in the comma-joined form parseSchemaShape stores. study_mirror
// gains study_mirror_faculty_sponsor_idx (faculty_sponsor) for the D4
// faculty-sponsor lookup and the resolve-person GROUP BY faculty_sponsor
// enumeration; seq_product_irods_locations_mirror gains
// spi_mirror_iseq_product_idx (id_iseq_product) for the D1 run-scoped iRODS join
// and the D2 manifest per-product iRODS LEFT JOIN. It is the single source of
// truth shared by the A2 existence and cross-dialect-equality assertions below.
var a2NewIndexColumns = map[string]string{
	"study_mirror":                       "faculty_sponsor",
	"seq_product_irods_locations_mirror": "id_iseq_product",
}

const (
	a2ProductMetricsTable          = "iseq_product_metrics_mirror"
	a2IRODSLocationsTable          = "seq_product_irods_locations_mirror"
	a2ProductIDColumn              = "id_iseq_product"
	a2RedundantProductIDIndex      = "ipm_mirror_iseq_product_idx"
	a2RedundantMySQLProductIDIndex = a2RedundantProductIDIndex
)

func TestLoadSchema(t *testing.T) {
	convey.Convey("Given the SQLite schema files", t, func() {
		stmts, err := loadSchema("sqlite")

		convey.Convey("when loadSchema runs, then it returns the table statements in spec order", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(stmts, convey.ShouldHaveLength, len(schemaStatementOrder))

			for i, table := range schemaStatementOrder {
				convey.So(stmts[i], convey.ShouldContainSubstring, "CREATE TABLE IF NOT EXISTS "+table)
			}
		})
	})
}

func TestSampleSearchTokenSchemaSQLiteDeclaresTokenTableAndIndex(t *testing.T) {
	convey.Convey("Given the embedded SQLite sample_search_token schema", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		ddl, err := cacheSchemaFS.ReadFile("cache_schema/sqlite/sample_search_token.sql")
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when inspected, then it declares a normal token table over (token, id_sample_tmp) with a covering index on the same columns", func() {
			upper := strings.ToUpper(string(ddl))
			convey.So(upper, convey.ShouldContainSubstring, "CREATE TABLE IF NOT EXISTS SAMPLE_SEARCH_TOKEN")
			convey.So(upper, convey.ShouldNotContainSubstring, "VIRTUAL TABLE")
			convey.So(upper, convey.ShouldNotContainSubstring, "FTS5")
			convey.So(string(ddl), convey.ShouldContainSubstring, "token")
			convey.So(string(ddl), convey.ShouldContainSubstring, "id_sample_tmp")
			convey.So(string(ddl), convey.ShouldContainSubstring, "ON sample_search_token(token, id_sample_tmp)")

			// The token table is one of the ordinary schema tables loaded by
			// loadSchema, not a separately applied search index.
			joined := strings.Join(stmts, "\n")
			convey.So(joined, convey.ShouldContainSubstring, "sample_search_token")
		})
	})
}

func TestSampleSearchTokenSchemaMySQLDeclaresTokenTableAndIndex(t *testing.T) {
	convey.Convey("Given the embedded MySQL sample_search_token schema string", t, func() {
		ddl, err := cacheSchemaFS.ReadFile("cache_schema/mysql/sample_search_token.sql")
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when parsed, then it declares a normal token table and a (token, id_sample_tmp) index, with no FULLTEXT", func() {
			statements := splitSQLStatements(string(ddl))
			convey.So(statements, convey.ShouldHaveLength, 2)

			table, columns, _, _, err := parseCreateTable(statements[0])
			convey.So(err, convey.ShouldBeNil)
			convey.So(table, convey.ShouldEqual, "sample_search_token")
			convey.So(columns, convey.ShouldContainKey, "token")
			convey.So(columns, convey.ShouldContainKey, "id_sample_tmp")

			indexTable, indexColumns, err := parseCreateIndex(statements[1])
			convey.So(err, convey.ShouldBeNil)
			convey.So(indexTable, convey.ShouldEqual, "sample_search_token")
			convey.So(indexColumns, convey.ShouldResemble, []string{"token", "id_sample_tmp"})

			convey.So(strings.ToUpper(string(ddl)), convey.ShouldNotContainSubstring, "FULLTEXT")
		})
	})
}

func TestParseSchemaShapeRecordsTokenIndexAsNormalTable(t *testing.T) {
	convey.Convey("Given the SQLite and MySQL schemas at the current version", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when parseSchemaShape runs on each, then both record sample_search_token as a normal table with a (token, id_sample_tmp) index", func() {
			convey.So(sqliteShape.Tables, convey.ShouldContainKey, "sample_search_token")
			convey.So(mysqlShape.Tables, convey.ShouldContainKey, "sample_search_token")
			convey.So(sqliteShape.Tables["sample_search_token"], convey.ShouldResemble, map[string]string{"token": "text", "id_sample_tmp": "integer"})
			convey.So(sqliteShape.Tables["sample_search_token"], convey.ShouldResemble, mysqlShape.Tables["sample_search_token"])
			convey.So(sqliteShape.Index["sample_search_token"], convey.ShouldResemble, []string{"token,id_sample_tmp"})
			convey.So(sqliteShape.Index["sample_search_token"], convey.ShouldResemble, mysqlShape.Index["sample_search_token"])
		})
	})
}

func TestCommonNameWordMirrorSchemaDeclaresVocabularyTableAndIndex(t *testing.T) {
	convey.Convey("A5: Given the SQLite and MySQL schemas at the current version", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when common_name_word_mirror is inspected, then both dialects declare (word, common_name) rows indexed by word", func() {
			convey.So(schemaStatementOrder, convey.ShouldContain, "common_name_word_mirror")
			convey.So(sqliteShape.Tables, convey.ShouldContainKey, "common_name_word_mirror")
			convey.So(mysqlShape.Tables, convey.ShouldContainKey, "common_name_word_mirror")
			convey.So(sqliteShape.Tables["common_name_word_mirror"], convey.ShouldResemble, map[string]string{
				"word":        "text",
				"common_name": "text",
			})
			convey.So(sqliteShape.Tables["common_name_word_mirror"], convey.ShouldResemble, mysqlShape.Tables["common_name_word_mirror"])
			convey.So(sqliteShape.Index["common_name_word_mirror"], convey.ShouldResemble, []string{"word"})
			convey.So(sqliteShape.Index["common_name_word_mirror"], convey.ShouldResemble, mysqlShape.Index["common_name_word_mirror"])
		})
	})
}

func TestSeqProductIRODSLocationsMirrorSQLiteShapeHasCreatedPlatformAndCreatedIndex(t *testing.T) {
	convey.Convey("A1.1: Given the SQLite schema parsed into a schemaShape", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when seq_product_irods_locations_mirror is inspected, then it has source identity, created and platform columns plus required indexes", func() {
			columns := shape.Tables["seq_product_irods_locations_mirror"]
			convey.So(columns, convey.ShouldContainKey, "id_seq_product_irods_locations_tmp")
			convey.So(columns, convey.ShouldContainKey, "created")
			convey.So(columns, convey.ShouldContainKey, "platform")
			convey.So(columns["id_seq_product_irods_locations_tmp"], convey.ShouldEqual, "integer")
			convey.So(columns["created"], convey.ShouldEqual, "text")
			convey.So(columns["platform"], convey.ShouldEqual, "text")
			convey.So(shape.Index["seq_product_irods_locations_mirror"], convey.ShouldContain, "id_seq_product_irods_locations_tmp")
			convey.So(shape.Index["seq_product_irods_locations_mirror"], convey.ShouldContain, "id_study_lims,created")
		})
	})
}

func TestA4SeqProductIRODSLocationsMirrorExportColumnsAndIndexes(t *testing.T) {
	convey.Convey("A4.1: Given the SQLite and MySQL schemas parsed into schemaShapes", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when seq_product_irods_locations_mirror is inspected, then export columns and indexes exist in dialect parity", func() {
			requiredColumns := map[string]string{
				"id_run":         "integer",
				"position":       "integer",
				"tag_index":      "integer",
				"qc":             "integer",
				"is_deliverable": "integer",
				"merged":         "integer",
			}
			for column, columnType := range requiredColumns {
				convey.So(sqliteShape.Tables["seq_product_irods_locations_mirror"], convey.ShouldContainKey, column)
				convey.So(sqliteShape.Tables["seq_product_irods_locations_mirror"][column], convey.ShouldEqual, columnType)
				convey.So(mysqlShape.Tables["seq_product_irods_locations_mirror"], convey.ShouldContainKey, column)
				convey.So(mysqlShape.Tables["seq_product_irods_locations_mirror"][column], convey.ShouldEqual, columnType)
			}

			convey.So(sqliteShape.Nullable["seq_product_irods_locations_mirror"]["qc"], convey.ShouldBeTrue)
			convey.So(sqliteShape.Nullable["seq_product_irods_locations_mirror"]["is_deliverable"], convey.ShouldBeTrue)
			convey.So(mysqlShape.Nullable["seq_product_irods_locations_mirror"]["qc"], convey.ShouldBeTrue)
			convey.So(mysqlShape.Nullable["seq_product_irods_locations_mirror"]["is_deliverable"], convey.ShouldBeTrue)

			requiredIndexes := []string{
				"id_study_lims,id_run,position,tag_index,id_seq_product_irods_locations_tmp",
				"id_study_lims,created",
				"id_sample_tmp,created",
				"id_run,created",
			}
			for _, index := range requiredIndexes {
				convey.So(sqliteShape.Index["seq_product_irods_locations_mirror"], convey.ShouldContain, index)
				convey.So(mysqlShape.Index["seq_product_irods_locations_mirror"], convey.ShouldContain, index)
			}

			convey.So(mysqlShape.Tables["seq_product_irods_locations_mirror"], convey.ShouldResemble, sqliteShape.Tables["seq_product_irods_locations_mirror"])
			convey.So(mysqlShape.Index["seq_product_irods_locations_mirror"], convey.ShouldResemble, sqliteShape.Index["seq_product_irods_locations_mirror"])
		})
	})
}

func TestA6RunDateMirrorSchemasAndIndexes(t *testing.T) {
	convey.Convey("A6: Given the SQLite and MySQL schemas parsed into schemaShapes", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when oseq_flowcell_mirror is inspected, then ONT run identity and date columns exist in dialect parity", func() {
			required := map[string]string{
				"experiment_name": "text",
				"run_id":          "integer",
				"run_uuid":        "text",
				"last_updated":    "text",
				"normalised_date": "text",
			}
			for column, columnType := range required {
				convey.So(sqliteShape.Tables["oseq_flowcell_mirror"], convey.ShouldContainKey, column)
				convey.So(sqliteShape.Tables["oseq_flowcell_mirror"][column], convey.ShouldEqual, columnType)
				convey.So(mysqlShape.Tables["oseq_flowcell_mirror"], convey.ShouldContainKey, column)
				convey.So(mysqlShape.Tables["oseq_flowcell_mirror"][column], convey.ShouldEqual, columnType)
			}

			convey.So(sqliteShape.Nullable["oseq_flowcell_mirror"]["run_id"], convey.ShouldBeTrue)
			convey.So(mysqlShape.Nullable["oseq_flowcell_mirror"]["run_id"], convey.ShouldBeTrue)
			convey.So(sqliteShape.Index["oseq_flowcell_mirror"], convey.ShouldContain, "experiment_name")
			convey.So(sqliteShape.Index["oseq_flowcell_mirror"], convey.ShouldContain, "last_updated")
			convey.So(sqliteShape.Index["oseq_flowcell_mirror"], convey.ShouldContain, "normalised_date")
			convey.So(mysqlShape.Index["oseq_flowcell_mirror"], convey.ShouldContain, "experiment_name")
			convey.So(mysqlShape.Index["oseq_flowcell_mirror"], convey.ShouldContain, "last_updated")
			convey.So(mysqlShape.Index["oseq_flowcell_mirror"], convey.ShouldContain, "normalised_date")
		})

		convey.Convey("when run-date source mirrors are inspected, then each has an indexed sync-derived normalised_date", func() {
			requiredIndexes := map[string]string{
				"iseq_run_status_mirror":          "normalised_date,id_run_status_dict,id_run",
				"pac_bio_run_well_metrics_mirror": "normalised_date",
				"oseq_flowcell_mirror":            "normalised_date",
				"useq_run_metrics_mirror":         "normalised_date",
				"eseq_run_lane_metrics_mirror":    "normalised_date",
			}
			for table, index := range requiredIndexes {
				convey.So(sqliteShape.Tables[table], convey.ShouldContainKey, "normalised_date")
				convey.So(sqliteShape.Tables[table]["normalised_date"], convey.ShouldEqual, "text")
				convey.So(mysqlShape.Tables[table], convey.ShouldContainKey, "normalised_date")
				convey.So(mysqlShape.Tables[table]["normalised_date"], convey.ShouldEqual, "text")
				convey.So(sqliteShape.Index[table], convey.ShouldContain, index)
				convey.So(mysqlShape.Index[table], convey.ShouldContain, index)
			}
		})
	})
}

func TestSeqProductIRODSLocationsMirrorMySQLShapeMatchesSQLite(t *testing.T) {
	convey.Convey("A1.2: Given the MySQL schema parsed into a schemaShape", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when seq_product_irods_locations_mirror is inspected, then it has the same source identity, created/platform columns and indexes, so the two dialects compare equal", func() {
			columns := mysqlShape.Tables["seq_product_irods_locations_mirror"]
			convey.So(columns, convey.ShouldContainKey, "id_seq_product_irods_locations_tmp")
			convey.So(columns, convey.ShouldContainKey, "created")
			convey.So(columns, convey.ShouldContainKey, "platform")
			convey.So(columns["id_seq_product_irods_locations_tmp"], convey.ShouldEqual, "integer")
			convey.So(columns["created"], convey.ShouldEqual, "text")
			convey.So(columns["platform"], convey.ShouldEqual, "text")
			convey.So(mysqlShape.Index["seq_product_irods_locations_mirror"], convey.ShouldContain, "id_seq_product_irods_locations_tmp")
			convey.So(mysqlShape.Index["seq_product_irods_locations_mirror"], convey.ShouldContain, "id_study_lims,created")

			convey.So(mysqlShape.Tables["seq_product_irods_locations_mirror"], convey.ShouldResemble, sqliteShape.Tables["seq_product_irods_locations_mirror"])
			convey.So(mysqlShape.Index["seq_product_irods_locations_mirror"], convey.ShouldResemble, sqliteShape.Index["seq_product_irods_locations_mirror"])
			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)
		})
	})
}

func TestA4MirrorTablesExistWithIndexesAndDialectsCompareEqual(t *testing.T) {
	convey.Convey("A4.1: Given the sqlite and mysql schemas parsed into schemaShapes", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when each new mirror table is inspected, then it exists with its declared indexes in both dialects", func() {
			for table, indexes := range a4MirrorTables {
				convey.So(sqliteShape.Tables, convey.ShouldContainKey, table)
				convey.So(mysqlShape.Tables, convey.ShouldContainKey, table)

				want := append([]string(nil), indexes...)
				sort.Strings(want)
				convey.So(sqliteShape.Index[table], convey.ShouldResemble, want)
				convey.So(mysqlShape.Index[table], convey.ShouldResemble, want)
			}
		})

		convey.Convey("when the two dialects are compared, then they are structurally equal", func() {
			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)

			for table := range a4MirrorTables {
				convey.So(mysqlShape.Tables[table], convey.ShouldResemble, sqliteShape.Tables[table])
				convey.So(mysqlShape.Index[table], convey.ShouldResemble, sqliteShape.Index[table])
			}
		})
	})
}

func TestA4SeqOpsTrackingMirrorHasAllMilestonesAndLookupIndexes(t *testing.T) {
	convey.Convey("A4.3: Given seq_ops_tracking_per_sample_mirror parsed from the sqlite schema", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when inspected, then it carries all 9 milestone columns and is indexed by id_sample_lims, sanger_sample_name, study_id", func() {
			columns := shape.Tables["seq_ops_tracking_per_sample_mirror"]
			milestones := []string{
				"manifest_created", "manifest_uploaded", "labware_received",
				"order_made", "working_dilution", "library_start",
				"library_complete", "sequencing_run_start", "sequencing_qc_complete",
			}
			for _, milestone := range milestones {
				convey.So(columns, convey.ShouldContainKey, milestone)
				convey.So(columns[milestone], convey.ShouldEqual, "text")
			}

			convey.So(shape.Index["seq_ops_tracking_per_sample_mirror"], convey.ShouldResemble, []string{"id_sample_lims", "sanger_sample_name", "study_id", "study_id,id_sample_lims"})
		})
	})
}

func TestA3IseqProductMetricsMirrorQCColumnsNullableInBothDialects(t *testing.T) {
	convey.Convey("A3.1: Given the sqlite and mysql schemas parsed into schemaShapes", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when iseq_product_metrics_mirror is inspected, then qc, qc_seq and qc_lib are nullable in both dialects", func() {
			for _, column := range []string{"qc", "qc_seq", "qc_lib"} {
				convey.So(sqliteShape.Nullable["iseq_product_metrics_mirror"][column], convey.ShouldBeTrue)
				convey.So(mysqlShape.Nullable["iseq_product_metrics_mirror"][column], convey.ShouldBeTrue)
			}
		})

		convey.Convey("when every product-metrics mirror that carries QC is inspected, then its QC columns are nullable in both dialects", func() {
			qcColumnsByTable := map[string][]string{
				"iseq_product_metrics_mirror":    {"qc", "qc_seq", "qc_lib"},
				"pac_bio_product_metrics_mirror": {"qc"},
				"eseq_product_metrics_mirror":    {"qc", "qc_seq", "qc_lib"},
				"useq_product_metrics_mirror":    {"qc", "qc_seq", "qc_lib"},
			}
			for table, columns := range qcColumnsByTable {
				for _, column := range columns {
					convey.So(sqliteShape.Nullable[table][column], convey.ShouldBeTrue)
					convey.So(mysqlShape.Nullable[table][column], convey.ShouldBeTrue)
				}
			}
		})

		convey.Convey("when the parsed nullability is compared across dialects for every table, then it matches (no pre-existing dialect mismatch)", func() {
			convey.So(sqliteShape.Nullable, convey.ShouldResemble, mysqlShape.Nullable)
		})
	})
}

func TestA1StudyUsersMirrorSQLiteShapeHasColumnsAndIndexes(t *testing.T) {
	convey.Convey("A1.1: Given the SQLite schema parsed into a schemaShape", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when study_users_mirror is inspected, then it has the seven declared columns and the five declared indexes", func() {
			convey.So(shape.Tables, convey.ShouldContainKey, "study_users_mirror")
			convey.So(shape.Tables["study_users_mirror"], convey.ShouldResemble, studyUsersMirrorColumns)
			convey.So(shape.Index["study_users_mirror"], convey.ShouldResemble, studyUsersMirrorIndexes)
		})
	})
}

func TestA2CrossDialectShapeParityOmitsProductIDPrimaryKeyDuplicate(t *testing.T) {
	convey.Convey("A2.2: Given both dialect schemas parsed into schemaShapes", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when the full per-table index column lists are compared, then they match without a redundant product-id secondary index", func() {
			for table, column := range a2NewIndexColumns {
				convey.So(sqliteShape.Index[table], convey.ShouldContain, column)
			}

			convey.So(sqliteShape.Index[a2ProductMetricsTable], convey.ShouldNotContain, a2ProductIDColumn)
			convey.So(mysqlShape.Index[a2ProductMetricsTable], convey.ShouldNotContain, a2ProductIDColumn)
			convey.So(sqliteShape.Index, convey.ShouldResemble, mysqlShape.Index)
			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)
		})
	})
}

func TestA1IseqFlowcellMirrorAndPlatformControlColumnsInBothDialects(t *testing.T) {
	convey.Convey("A1: Given the sqlite and mysql schemas parsed into schemaShapes", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when iseq_flowcell_mirror is inspected, then it has the product-join primary key and entity_type index in parity", func() {
			for _, schema := range [][]string{sqliteSchema, mysqlSchema} {
				ddl, ddlErr := createTableStatement(schema, "iseq_flowcell_mirror")
				convey.So(ddlErr, convey.ShouldBeNil)
				definition, ok := createTableColumnDefinition(ddl, "id_iseq_flowcell_tmp")
				convey.So(ok, convey.ShouldBeTrue)
				convey.So(strings.ToUpper(definition), convey.ShouldContainSubstring, "PRIMARY KEY")
			}

			convey.So(sqliteShape.Tables["iseq_flowcell_mirror"], convey.ShouldResemble, iseqFlowcellMirrorSchemaColumns)
			convey.So(mysqlShape.Tables["iseq_flowcell_mirror"], convey.ShouldResemble, iseqFlowcellMirrorSchemaColumns)
			convey.So(sqliteShape.Index["iseq_flowcell_mirror"], convey.ShouldResemble, []string{"entity_type"})
			convey.So(mysqlShape.Index["iseq_flowcell_mirror"], convey.ShouldResemble, []string{"entity_type"})
			convey.So(sqliteShape.Index["iseq_flowcell_mirror"], convey.ShouldResemble, mysqlShape.Index["iseq_flowcell_mirror"])
		})

		convey.Convey("when Element and Ultima product mirrors are inspected, then is_sequencing_control is nullable and in parity", func() {
			for _, table := range []string{"eseq_product_metrics_mirror", "useq_product_metrics_mirror"} {
				convey.So(sqliteShape.Tables[table]["is_sequencing_control"], convey.ShouldEqual, "integer")
				convey.So(mysqlShape.Tables[table]["is_sequencing_control"], convey.ShouldEqual, "integer")
				convey.So(sqliteShape.Nullable[table]["is_sequencing_control"], convey.ShouldBeTrue)
				convey.So(mysqlShape.Nullable[table]["is_sequencing_control"], convey.ShouldBeTrue)
				convey.So(sqliteShape.Tables[table], convey.ShouldResemble, mysqlShape.Tables[table])
			}

			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)
		})
	})
}

func TestA1StudyUsersMirrorMySQLShapeMatchesSQLite(t *testing.T) {
	convey.Convey("A1.2: Given the SQLite and MySQL schemas parsed into schemaShapes", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when study_users_mirror is inspected, then it has the same columns and indexes and the two dialects compare equal", func() {
			convey.So(mysqlShape.Tables, convey.ShouldContainKey, "study_users_mirror")
			convey.So(mysqlShape.Tables["study_users_mirror"], convey.ShouldResemble, studyUsersMirrorColumns)
			convey.So(mysqlShape.Index["study_users_mirror"], convey.ShouldResemble, studyUsersMirrorIndexes)

			convey.So(mysqlShape.Tables["study_users_mirror"], convey.ShouldResemble, sqliteShape.Tables["study_users_mirror"])
			convey.So(mysqlShape.Index["study_users_mirror"], convey.ShouldResemble, sqliteShape.Index["study_users_mirror"])
			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)
		})
	})
}

func TestA2NewLookupIndexesExistInBothDialectsAndCompareEqual(t *testing.T) {
	convey.Convey("A2.1: Given the sqlite and mysql schemas parsed into schemaShapes", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when study_mirror and seq_product_irods_locations_mirror are inspected, then each carries its new single-column index in both dialects", func() {
			for table, column := range a2NewIndexColumns {
				convey.So(sqliteShape.Index[table], convey.ShouldContain, column)
				convey.So(mysqlShape.Index[table], convey.ShouldContain, column)
			}
		})

		convey.Convey("when the two dialects are compared, then the affected tables' index lists are equal and the schemas compare structurally equal", func() {
			for table := range a2NewIndexColumns {
				convey.So(mysqlShape.Index[table], convey.ShouldResemble, sqliteShape.Index[table])
			}

			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)
		})
	})
}

func TestA7StudyMirrorProgrammeIndexExistsInBothDialectsAndComparesEqual(t *testing.T) {
	convey.Convey("A7: Given the sqlite and mysql schemas parsed into schemaShapes", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteSchema)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlSchema)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when study_mirror is inspected, then programme has an index in both dialects", func() {
			convey.So(sqliteShape.Index["study_mirror"], convey.ShouldContain, "programme")
			convey.So(mysqlShape.Index["study_mirror"], convey.ShouldContain, "programme")
		})

		convey.Convey("when the two dialects are compared, then study_mirror indexes and full schema shapes are in parity", func() {
			convey.So(mysqlShape.Index["study_mirror"], convey.ShouldResemble, sqliteShape.Index["study_mirror"])
			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)
		})
	})
}

func TestA2ProductIDDDLDeclaresPrimaryKeyWithoutRedundantIndex(t *testing.T) {
	convey.Convey("A2: Given the embedded cache schemas", t, func() {
		for _, dialect := range []string{"mysql", "sqlite"} {
			schema, err := loadSchema(dialect)
			convey.So(err, convey.ShouldBeNil)
			shape, err := parseSchemaShape(schema)
			convey.So(err, convey.ShouldBeNil)

			productDDL, err := createTableStatement(schema, a2ProductMetricsTable)
			convey.So(err, convey.ShouldBeNil)
			irodsDDL, err := createTableStatement(schema, a2IRODSLocationsTable)
			convey.So(err, convey.ShouldBeNil)
			productIndexes, err := createIndexesForTable(schema, a2ProductMetricsTable)
			convey.So(err, convey.ShouldBeNil)

			productDefinition, ok := createTableColumnDefinition(productDDL, a2ProductIDColumn)
			convey.So(ok, convey.ShouldBeTrue)
			irodsDefinition, ok := createTableColumnDefinition(irodsDDL, a2ProductIDColumn)
			convey.So(ok, convey.ShouldBeTrue)

			convey.Convey("when the "+dialect+" product mirror id_iseq_product column is inspected, then it is the non-null primary key", func() {
				fields := strings.Fields(productDefinition)
				convey.So(len(fields), convey.ShouldBeGreaterThanOrEqualTo, 5)
				if dialect == "mysql" {
					convey.So(strings.ToUpper(fields[1]), convey.ShouldEqual, "CHAR(64)")
				} else {
					convey.So(strings.ToUpper(fields[1]), convey.ShouldEqual, "TEXT")
				}

				normalized := strings.ToUpper(strings.Join(fields, " "))
				convey.So(normalized, convey.ShouldContainSubstring, "NOT NULL")
				convey.So(normalized, convey.ShouldContainSubstring, "PRIMARY KEY")
			})

			convey.Convey("when the "+dialect+" iRODS mirror id_iseq_product column is inspected, then it is not the primary key", func() {
				fields := strings.Fields(irodsDefinition)
				convey.So(len(fields), convey.ShouldBeGreaterThanOrEqualTo, 3)

				normalized := strings.ToUpper(strings.Join(fields, " "))
				convey.So(normalized, convey.ShouldContainSubstring, "NOT NULL")
				convey.So(normalized, convey.ShouldNotContainSubstring, "PRIMARY KEY")
			})

			convey.Convey("when the "+dialect+" product mirror secondary indexes are inspected, then the redundant id_iseq_product index is not declared", func() {
				_, ok := productIndexes[a2RedundantProductIDIndex]
				convey.So(ok, convey.ShouldBeFalse)

				for name, columns := range productIndexes {
					convey.So(name, convey.ShouldNotEqual, a2RedundantProductIDIndex)
					convey.So(strings.Join(columns, ","), convey.ShouldNotEqual, a2ProductIDColumn)
				}

				convey.So(shape.Index[a2ProductMetricsTable], convey.ShouldNotContain, a2ProductIDColumn)
			})
		}
	})
}

func TestParseSchemaShapeParity(t *testing.T) {
	convey.Convey("Given both dialect schema directories", t, func() {
		sqliteSchema, sqliteErr := loadSchema("sqlite")
		mysqlSchema, mysqlErr := loadSchema("mysql")
		convey.So(sqliteErr, convey.ShouldBeNil)
		convey.So(mysqlErr, convey.ShouldBeNil)

		sqliteShape, sqliteErr := parseSchemaShape(sqliteSchema)
		mysqlShape, mysqlErr := parseSchemaShape(mysqlSchema)

		convey.Convey("when the parsed schema shapes are compared, then the table names match exactly", func() {
			convey.So(sqliteErr, convey.ShouldBeNil)
			convey.So(mysqlErr, convey.ShouldBeNil)
			convey.So(tableNames(sqliteShape.Tables), convey.ShouldResemble, sortedSchemaTableNames())
			convey.So(tableNames(mysqlShape.Tables), convey.ShouldResemble, sortedSchemaTableNames())
		})

		convey.Convey("when comparing each table's columns, then the column sets match across dialects", func() {
			convey.So(sqliteErr, convey.ShouldBeNil)
			convey.So(mysqlErr, convey.ShouldBeNil)
			convey.So(sqliteShape.Tables, convey.ShouldResemble, mysqlShape.Tables)
		})

		convey.Convey("when comparing the per-table index column lists, then they match across dialects", func() {
			convey.So(sqliteErr, convey.ShouldBeNil)
			convey.So(mysqlErr, convey.ShouldBeNil)
			convey.So(sqliteShape.Index, convey.ShouldResemble, mysqlShape.Index)
			convey.So(sqliteShape.Index["sample_search_token"], convey.ShouldResemble, []string{"token,id_sample_tmp"})
		})

		convey.Convey("when comparing unique constraints, then the per-table column tuples match across dialects", func() {
			convey.So(sqliteErr, convey.ShouldBeNil)
			convey.So(mysqlErr, convey.ShouldBeNil)
			convey.So(sqliteShape.Unique, convey.ShouldResemble, mysqlShape.Unique)
			convey.So(sqliteShape.Unique, convey.ShouldResemble, map[string][]string{
				"donor_samples":   {"donor_id,id_sample_tmp"},
				"library_samples": {"pipeline_id_lims,id_sample_tmp,id_study_lims"},
			})
		})

		convey.Convey("when the full schema parity is compared, then tables, columns, indexes, and unique constraints all match", func() {
			convey.So(sqliteErr, convey.ShouldBeNil)
			convey.So(mysqlErr, convey.ShouldBeNil)
			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)
		})
	})
}

func TestMySQLSchemaIndexNamesFitIdentifierLimit(t *testing.T) {
	convey.Convey("Given the embedded MySQL schema", t, func() {
		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when CREATE INDEX statements are inspected, then every index name fits MySQL's 64-character identifier limit", func() {
			for _, statementGroup := range mysqlSchema {
				for _, statement := range splitSQLStatements(statementGroup) {
					fields := strings.Fields(strings.TrimSpace(statement))
					if len(fields) < 3 || !strings.EqualFold(fields[0], "CREATE") || !strings.EqualFold(fields[1], "INDEX") {
						continue
					}

					indexName := strings.Trim(fields[2], "`")
					convey.So(len(indexName), convey.ShouldBeLessThanOrEqualTo, 64)
				}
			}
		})
	})
}

func TestSQLiteSchemaExecution(t *testing.T) {
	convey.Convey("Given the embedded SQLite schema", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		db, err := sql.Open("sqlite", ":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = db.Close() })

		for _, group := range stmts {
			for _, stmt := range splitSQLStatements(group) {
				_, err = db.Exec(stmt)
				convey.So(err, convey.ShouldBeNil)
			}
		}

		rows, err := db.Query(`
			SELECT name
			FROM sqlite_master
			WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
			ORDER BY rowid
		`)
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = rows.Close() })

		tables := make([]string, 0, len(schemaStatementOrder))
		for rows.Next() {
			var table string
			convey.So(rows.Scan(&table), convey.ShouldBeNil)
			tables = append(tables, table)
		}

		convey.Convey("when the schema is executed against SQLite, then every ordered cache table is created", func() {
			convey.So(rows.Err(), convey.ShouldBeNil)
			convey.So(tables, convey.ShouldResemble, schemaStatementOrder)
		})
	})
}

func TestSQLiteSchemaExecutionViaOpenCache(t *testing.T) {
	convey.Convey("Given an in-memory SQLite cache opened through OpenCache", t, func() {
		cache, err := OpenCache(context.Background(), CacheConfig{Path: ":memory:"})
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { convey.So(cache.Close(), convey.ShouldBeNil) })

		rows, err := cache.DB().Query(`
			SELECT name
			FROM sqlite_master
			WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
			ORDER BY rowid
		`)
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = rows.Close() })

		tables := make([]string, 0, len(schemaStatementOrder)+1)
		for rows.Next() {
			var table string
			convey.So(rows.Scan(&table), convey.ShouldBeNil)
			tables = append(tables, table)
		}

		convey.Convey("when the schema loader runs, then every cache table (including sample_search_token) exists", func() {
			convey.So(rows.Err(), convey.ShouldBeNil)
			for _, table := range schemaStatementOrder {
				convey.So(tables, convey.ShouldContain, table)
			}
			convey.So(tables, convey.ShouldContain, "sample_search_token")
		})
	})
}

func TestSchemaIndexesAreSorted(t *testing.T) {
	convey.Convey("Given a parsed schema shape", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when index declarations are captured, then they are stored deterministically", func() {
			for _, indexes := range shape.Index {
				convey.So(sort.StringsAreSorted(indexes), convey.ShouldBeTrue)
			}
		})
	})
}

func TestSchemaUniqueConstraintsAreSorted(t *testing.T) {
	convey.Convey("Given a parsed schema shape", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when unique constraints are captured, then they are stored deterministically", func() {
			for _, tuples := range shape.Unique {
				convey.So(sort.StringsAreSorted(tuples), convey.ShouldBeTrue)
			}
		})
	})
}

func TestSchemaIndexesLibraryIdentifiers(t *testing.T) {
	convey.Convey("Given the embedded SQLite schema", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when library_samples indexes are inspected, then exact library identifiers have lookup indexes", func() {
			convey.So(shape.Index["library_samples"], convey.ShouldContain, "library_id")
			convey.So(shape.Index["library_samples"], convey.ShouldContain, "id_library_lims")
		})
	})
}

func TestV2SchemaIncludesExpectedMigrationColumns(t *testing.T) {
	convey.Convey("Given the parsed SQLite v2 schema", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when inspected, then the new cache tables and migration-state columns are present", func() {
			convey.So(shape.Tables["study_mirror"], convey.ShouldContainKey, "last_updated")
			convey.So(shape.Tables["sample_mirror"], convey.ShouldContainKey, "last_updated")
			convey.So(shape.Tables, convey.ShouldContainKey, "iseq_product_metrics_mirror")
			convey.So(shape.Tables, convey.ShouldContainKey, "seq_product_irods_locations_mirror")
			convey.So(shape.Tables, convey.ShouldContainKey, "sync_lock")
			convey.So(shape.Tables["sync_state"], convey.ShouldContainKey, "resume_cursor")
			convey.So(shape.Tables["sync_state"], convey.ShouldContainKey, "indexes_dropped")
		})
	})
}

func TestSchemaDeclaresCaseInsensitiveLookupCollations(t *testing.T) {
	convey.Convey("Given the embedded A3 schema statements", t, func() {
		sqliteSchema, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		mysqlSchema, err := loadSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteDDL := strings.Join(sqliteSchema, "\n")
		mysqlDDL := strings.Join(mysqlSchema, "\n")

		sqliteExpected := []string{
			"id_sample_lims    TEXT    NOT NULL COLLATE NOCASE",
			"uuid_sample_lims  TEXT    NOT NULL COLLATE NOCASE",
			"name              TEXT    NOT NULL COLLATE NOCASE",
			"sanger_sample_id  TEXT    NOT NULL COLLATE NOCASE",
			"supplier_name     TEXT    NOT NULL COLLATE NOCASE",
			"accession_number  TEXT    NOT NULL COLLATE NOCASE",
			"donor_id          TEXT    NOT NULL COLLATE NOCASE",
			"common_name       TEXT    NOT NULL COLLATE NOCASE",
			"id_study_lims              TEXT    NOT NULL COLLATE NOCASE",
			"uuid_study_lims            TEXT    NOT NULL COLLATE NOCASE",
			"accession_number           TEXT    NOT NULL COLLATE NOCASE",
			"pipeline_id_lims TEXT    NOT NULL COLLATE NOCASE",
			"id_study_lims    TEXT    NOT NULL COLLATE NOCASE",
			"donor_id      TEXT    NOT NULL COLLATE NOCASE",
			"id_study_lims        TEXT    NOT NULL COLLATE NOCASE",
			"id_study_lims            TEXT    NOT NULL COLLATE NOCASE",
		}

		mysqlExpected := []string{
			"id_sample_lims   VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"uuid_sample_lims VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"name             VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"sanger_sample_id VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"supplier_name    VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"accession_number VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"donor_id         VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"common_name      VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"id_study_lims               VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"uuid_study_lims             VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"pipeline_id_lims VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"donor_id      VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"id_study_lims        VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
			"id_study_lims            VARCHAR(255) NOT NULL COLLATE utf8mb4_0900_ai_ci",
		}

		convey.Convey("when inspected, then each spec-defined lookup column carries the backend collation", func() {
			for _, snippet := range sqliteExpected {
				convey.So(sqliteDDL, convey.ShouldContainSubstring, snippet)
			}

			for _, snippet := range mysqlExpected {
				convey.So(mysqlDDL, convey.ShouldContainSubstring, snippet)
			}
		})
	})
}

func createTableStatement(stmts []string, table string) (string, error) {
	for _, group := range stmts {
		for _, stmt := range splitSQLStatements(group) {
			if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "CREATE TABLE") {
				continue
			}

			name, _, _, _, err := parseCreateTable(stmt)
			if err != nil {
				return "", err
			}
			if name == table {
				return stmt, nil
			}
		}
	}

	return "", fmt.Errorf("mlwh: create table statement for %s not found", table)
}

func createIndexesForTable(stmts []string, table string) (map[string][]string, error) {
	indexes := map[string][]string{}
	for _, group := range stmts {
		for _, stmt := range splitSQLStatements(group) {
			if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "CREATE INDEX") {
				continue
			}

			name, err := createIndexName(stmt)
			if err != nil {
				return nil, err
			}
			indexTable, columns, err := parseCreateIndex(stmt)
			if err != nil {
				return nil, err
			}
			if indexTable == table {
				indexes[name] = columns
			}
		}
	}

	return indexes, nil
}

func createIndexName(stmt string) (string, error) {
	fields := strings.Fields(strings.Join(strings.Fields(stmt), " "))
	if len(fields) < 3 || !strings.EqualFold(fields[0], "CREATE") || !strings.EqualFold(fields[1], "INDEX") {
		return "", fmt.Errorf("mlwh: malformed create index statement %q", stmt)
	}

	return trimIdentifier(fields[2]), nil
}

func createTableColumnDefinition(stmt, column string) (string, bool) {
	bodyStart := strings.Index(stmt, "(")
	bodyEnd := strings.LastIndex(stmt, ")")
	if bodyStart == -1 || bodyEnd <= bodyStart {
		return "", false
	}

	for _, part := range splitTopLevel(body(stmt, bodyStart, bodyEnd), ',') {
		fields := strings.Fields(part)
		if len(fields) < 2 || isTableConstraint(fields) {
			continue
		}
		if trimIdentifier(fields[0]) == column {
			return strings.Join(fields, " "), true
		}
	}

	return "", false
}

func TestA2SQLiteProductIDSchemaCreationDoesNotCreateRedundantIndex(t *testing.T) {
	convey.Convey("A2: Given an opened ephemeral SQLite cache schema", t, func() {
		db := openSQLiteSchemaTestDB(t)

		convey.Convey("when the product mirror indexes are inspected, then id_iseq_product is served only by the primary key", func() {
			indexes, _, err := readSQLiteTableIndexes(context.Background(), db, a2ProductMetricsTable)
			convey.So(err, convey.ShouldBeNil)
			convey.So(indexes, convey.ShouldNotContain, a2ProductIDColumn)

			var redundantIndexCount int
			err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, a2RedundantProductIDIndex).Scan(&redundantIndexCount)
			convey.So(err, convey.ShouldBeNil)
			convey.So(redundantIndexCount, convey.ShouldEqual, 0)
		})
	})
}

func TestA6MonthlyRunCountSQLitePlansUseNormalisedDateIndexes(t *testing.T) {
	convey.Convey("A6: Given an opened ephemeral SQLite cache with run-date mirror rows", t, func() {
		db := openSQLiteSchemaTestDB(t)
		seedA6RunDatePlanRows(t, db)

		convey.Convey("when monthly count shapes are explained, then every platform source uses its normalised-date index", func() {
			since := "2026-06-01"
			until := "2026-08-01"

			assertSQLitePlanUsesIndex(t, db,
				`SELECT substr(s.normalised_date, 1, 7), COUNT(DISTINCT s.id_run)
				FROM iseq_run_status_mirror AS s
				INNER JOIN iseq_run_status_dict_mirror AS d
					ON d.id_run_status_dict = s.id_run_status_dict
				WHERE s.normalised_date >= ? AND s.normalised_date < ?
					AND d.description IN ('run complete', 'run archived')
				GROUP BY substr(s.normalised_date, 1, 7)`,
				"iseq_run_status_mirror_normalised_date_idx", since, until,
			)
			assertSQLitePlanUsesIndex(t, db,
				`SELECT substr(normalised_date, 1, 7), COUNT(DISTINCT pac_bio_run_name || ':' || well_label)
				FROM pac_bio_run_well_metrics_mirror
				WHERE normalised_date >= ? AND normalised_date < ?
				GROUP BY substr(normalised_date, 1, 7)`,
				"pac_bio_run_well_metrics_mirror_normalised_date_idx", since, until,
			)
			assertSQLitePlanUsesIndex(t, db,
				`SELECT substr(normalised_date, 1, 7), COUNT(DISTINCT experiment_name)
				FROM oseq_flowcell_mirror
				WHERE normalised_date >= ? AND normalised_date < ?
				GROUP BY substr(normalised_date, 1, 7)`,
				"oseq_flowcell_mirror_normalised_date_idx", since, until,
			)
			assertSQLitePlanUsesIndex(t, db,
				`SELECT substr(normalised_date, 1, 7), COUNT(DISTINCT id_run)
				FROM useq_run_metrics_mirror
				WHERE normalised_date >= ? AND normalised_date < ?
				GROUP BY substr(normalised_date, 1, 7)`,
				"useq_run_metrics_mirror_normalised_date_idx", since, until,
			)
			assertSQLitePlanUsesIndex(t, db,
				`SELECT substr(normalised_date, 1, 7), COUNT(DISTINCT id_run)
				FROM eseq_run_lane_metrics_mirror
				WHERE normalised_date >= ? AND normalised_date < ?
				GROUP BY substr(normalised_date, 1, 7)`,
				"eseq_run_lane_metrics_mirror_normalised_date_idx", since, until,
			)
		})
	})
}

func TestSeqProductIRODSLocationsMirrorEphemeralInsertReadsBackCreatedAndPlatform(t *testing.T) {
	convey.Convey("A1.3: Given an opened ephemeral SQLite cache", t, func() {
		db := openSQLiteSchemaTestDB(t)

		created := "2026-06-25T09:30:00Z"
		platform := "illumina"
		_, err := db.Exec(
			`INSERT INTO seq_product_irods_locations_mirror(id_seq_product_irods_locations_tmp, id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			int64(7001), "product-a1", "/seq", "run/1.cram", "/seq/run", "1.cram", int64(101), "6568",
			"2026-06-26T10:00:00Z", created, platform,
		)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when a row is inserted with the new column list, then it reads back with the stored created and platform", func() {
			var (
				gotCreated  string
				gotPlatform string
			)
			err = db.QueryRow(
				`SELECT created, platform FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`,
				"product-a1",
			).Scan(&gotCreated, &gotPlatform)
			convey.So(err, convey.ShouldBeNil)
			convey.So(gotCreated, convey.ShouldEqual, created)
			convey.So(gotPlatform, convey.ShouldEqual, platform)
		})
	})
}

func TestA4MirrorTablesEphemeralInsertReadsBack(t *testing.T) {
	convey.Convey("A4.2: Given an opened ephemeral SQLite cache", t, func() {
		db := openSQLiteSchemaTestDB(t)

		convey.Convey("when a row is inserted into each new mirror with its column list, then it reads back unchanged", func() {
			convey.So(insertReadBackPacBioProductMetricsMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackPacBioRunWellMetricsMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackEseqProductMetricsMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackEseqRunMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackEseqRunLaneMetricsMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackUseqProductMetricsMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackUseqRunMetricsMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackOseqFlowcellMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackIseqRunStatusMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackIseqRunStatusDictMirror(t, db), convey.ShouldBeNil)
			convey.So(insertReadBackSeqOpsTrackingPerSampleMirror(t, db), convey.ShouldBeNil)
		})
	})
}

func TestSQLiteSchemaCaseInsensitiveSampleNameEquality(t *testing.T) {
	convey.Convey("Given a populated SQLite sample_mirror row", t, func() {
		db := openSQLiteSchemaTestDB(t)

		_, err := db.Exec(`INSERT INTO sample_mirror(
			id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name,
			sanger_sample_id, supplier_name, accession_number, donor_id,
			taxon_id, common_name, description, last_updated
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			1, "SQSCP", "sample-1", "uuid-1", "HCA-LCA6-1",
			"sanger-1", "supplier-1", "ACC-1", "donor-1",
			9606, "human", "desc", "2026-05-11T12:00:00Z",
		)
		convey.So(err, convey.ShouldBeNil)

		var id int64
		err = db.QueryRow(`SELECT id_sample_tmp FROM sample_mirror WHERE name = ?`, "hca-lca6-1").Scan(&id)

		convey.Convey("when queried with different case, then the row is returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(id, convey.ShouldEqual, 1)
		})
	})
}

func TestMySQLSchemaCaseInsensitiveSampleNameEquality(t *testing.T) {
	convey.Convey("Given a populated MySQL sample_mirror row", t, func() {
		cfg, skipReason := loadMySQLCacheConfigForTest(t)
		if skipReason != "" {
			t.Skip(skipReason)
		}

		cache := openMySQLCacheForTest(t, cfg)

		_, err := cache.DB().Exec(`INSERT INTO sample_mirror(
			id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name,
			sanger_sample_id, supplier_name, accession_number, donor_id,
			taxon_id, common_name, description, last_updated
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			1, "SQSCP", "sample-1", "uuid-1", "HCA-LCA6-1",
			"sanger-1", "supplier-1", "ACC-1", "donor-1",
			9606, "human", "desc", "2026-05-11T12:00:00Z",
		)
		convey.So(err, convey.ShouldBeNil)

		var id int64
		err = cache.DB().QueryRow(`SELECT id_sample_tmp FROM sample_mirror WHERE name = ?`, "hca-lca6-1").Scan(&id)

		convey.Convey("when queried with different case, then the row is returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(id, convey.ShouldEqual, 1)
		})
	})
}

func TestSchemaCaseInsensitiveStudyAccessionNumberEquality(t *testing.T) {
	convey.Convey("Given a populated SQLite study_mirror row", t, func() {
		db := openSQLiteSchemaTestDB(t)

		_, err := db.Exec(`INSERT INTO study_mirror(
			id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name,
			accession_number, study_title, faculty_sponsor, state,
			data_release_strategy, data_access_group, programme,
			reference_genome, ethically_approved, study_type,
			contains_human_dna, contaminated_human_dna, study_visibility,
			ega_dac_accession_number, ega_policy_accession_number,
			data_release_timing, last_updated
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			11, "SQSCP", "study-11", "uuid-study-11", "Study 11",
			"EGAS00001006568", "Study title", "Sponsor", "active",
			"open", "dag", "programme", "GRCh38", 1, "genome",
			1, 0, "visible", "EGAD0001", "EGAP0001",
			"immediate", "2026-05-11T12:00:00Z",
		)
		convey.So(err, convey.ShouldBeNil)

		var sqliteID int64
		sqliteErr := db.QueryRow(`SELECT id_study_tmp FROM study_mirror WHERE accession_number = ?`, "egas00001006568").Scan(&sqliteID)

		convey.Convey("when queried in SQLite with different case, then the row is returned", func() {
			convey.So(sqliteErr, convey.ShouldBeNil)
			convey.So(sqliteID, convey.ShouldEqual, 11)
		})

		cfg, skipReason := loadMySQLCacheConfigForTest(t)
		if skipReason != "" {
			convey.SkipConvey("Given the same row in MySQL", func() {})

			return
		}

		cache := openMySQLCacheForTest(t, cfg)
		_, err = cache.DB().Exec(`INSERT INTO study_mirror(
			id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name,
			accession_number, study_title, faculty_sponsor, state,
			data_release_strategy, data_access_group, programme,
			reference_genome, ethically_approved, study_type,
			contains_human_dna, contaminated_human_dna, study_visibility,
			ega_dac_accession_number, ega_policy_accession_number,
			data_release_timing, last_updated
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			11, "SQSCP", "study-11", "uuid-study-11", "Study 11",
			"EGAS00001006568", "Study title", "Sponsor", "active",
			"open", "dag", "programme", "GRCh38", 1, "genome",
			1, 0, "visible", "EGAD0001", "EGAP0001",
			"immediate", "2026-05-11T12:00:00Z",
		)
		convey.So(err, convey.ShouldBeNil)

		var mysqlID int64
		mysqlErr := cache.DB().QueryRow(`SELECT id_study_tmp FROM study_mirror WHERE accession_number = ?`, "egas00001006568").Scan(&mysqlID)

		convey.Convey("Given the same row in MySQL", func() {
			convey.Convey("when queried with different case, then the row is returned", func() {
				convey.So(mysqlErr, convey.ShouldBeNil)
				convey.So(mysqlID, convey.ShouldEqual, 11)
			})
		})
	})
}

func TestSchemaCaseInsensitiveLibraryPipelineIDLimsEquality(t *testing.T) {
	convey.Convey("Given a populated SQLite library_samples row", t, func() {
		db := openSQLiteSchemaTestDB(t)

		_, err := db.Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "Standard", 7, "study-7")
		convey.So(err, convey.ShouldBeNil)

		var sampleID int64
		sqliteErr := db.QueryRow(`SELECT id_sample_tmp FROM library_samples WHERE pipeline_id_lims = ?`, "STANDARD").Scan(&sampleID)

		convey.Convey("when queried in SQLite with different case, then the row is returned", func() {
			convey.So(sqliteErr, convey.ShouldBeNil)
			convey.So(sampleID, convey.ShouldEqual, 7)
		})

		cfg, skipReason := loadMySQLCacheConfigForTest(t)
		if skipReason != "" {
			convey.SkipConvey("Given the same row in MySQL", func() {})

			return
		}

		cache := openMySQLCacheForTest(t, cfg)
		_, err = cache.DB().Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "Standard", 7, "study-7")
		convey.So(err, convey.ShouldBeNil)

		var mysqlSampleID int64
		mysqlErr := cache.DB().QueryRow(`SELECT id_sample_tmp FROM library_samples WHERE pipeline_id_lims = ?`, "STANDARD").Scan(&mysqlSampleID)

		convey.Convey("Given the same row in MySQL", func() {
			convey.Convey("when queried with different case, then the row is returned", func() {
				convey.So(mysqlErr, convey.ShouldBeNil)
				convey.So(mysqlSampleID, convey.ShouldEqual, 7)
			})
		})
	})
}

func TestSchemaRejectsEmptyLibrarySampleStudyLims(t *testing.T) {
	convey.Convey("B7.3: Given a SQLite library_samples insert with an empty study identifier", t, func() {
		db := openSQLiteSchemaTestDB(t)

		_, err := db.Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "Standard", 9, "")

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(err.Error()), convey.ShouldContainSubstring, "check")

		cfg, skipReason := loadMySQLCacheConfigForTest(t)
		if skipReason != "" {
			convey.SkipConvey("Given the same row in MySQL", func() {})

			return
		}

		cache := openMySQLCacheForTest(t, cfg)
		_, err = cache.DB().Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "Standard", 9, "")

		convey.Convey("Given the same row in MySQL", func() {
			convey.Convey("when inserted, then the CHECK constraint rejects it", func() {
				convey.So(err, convey.ShouldNotBeNil)
				convey.So(strings.ToLower(err.Error()), convey.ShouldContainSubstring, "check")
			})
		})
	})
}

func tableNames(tables map[string]map[string]string) []string {
	names := slices.Collect(maps.Keys(tables))
	sort.Strings(names)

	return names
}

func sortedSchemaTableNames() []string {
	names := append([]string(nil), schemaStatementOrder...)
	sort.Strings(names)

	return names
}

func TestA1StudyUsersMirrorEphemeralInsertReadsBack(t *testing.T) {
	convey.Convey("A1.3: Given an opened ephemeral SQLite cache", t, func() {
		db := openSQLiteSchemaTestDB(t)

		convey.Convey("when a row is inserted with the study_users_mirror column list, then it reads back unchanged", func() {
			convey.So(insertReadBackStudyUsersMirror(t, db), convey.ShouldBeNil)
		})
	})
}

func openSQLiteSchemaTestDB(t *testing.T) *sql.DB {
	t.Helper()

	stmts, err := loadSchema("sqlite")
	if err != nil {
		t.Fatalf("loadSchema(sqlite): %v", err)
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open(sqlite): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, group := range stmts {
		for _, stmt := range splitSQLStatements(group) {
			if _, err = db.Exec(stmt); err != nil {
				t.Fatalf("db.Exec(%q): %v", stmt, err)
			}
		}
	}

	return db
}

func insertReadBackStudyUsersMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO study_users_mirror(id_study_users_tmp, id_study_tmp, role, login, email, name, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		int64(4242), int64(6568), "manager", "abc", "abc@sanger.ac.uk", "Ann B Cole",
		"2026-06-25T09:00:00Z",
	); err != nil {
		return err
	}

	var (
		idStudyTmp  int64
		role        string
		login       string
		email       string
		name        string
		lastUpdated string
	)

	if err := db.QueryRow(
		`SELECT id_study_tmp, role, login, email, name, last_updated FROM study_users_mirror WHERE id_study_users_tmp = ?`,
		int64(4242),
	).Scan(&idStudyTmp, &role, &login, &email, &name, &lastUpdated); err != nil {
		return err
	}

	convey.So(idStudyTmp, convey.ShouldEqual, 6568)
	convey.So(role, convey.ShouldEqual, "manager")
	convey.So(login, convey.ShouldEqual, "abc")
	convey.So(email, convey.ShouldEqual, "abc@sanger.ac.uk")
	convey.So(name, convey.ShouldEqual, "Ann B Cole")
	convey.So(lastUpdated, convey.ShouldEqual, "2026-06-25T09:00:00Z")

	return nil
}

func loadMySQLCacheConfigForTest(t *testing.T) (CacheConfig, string) {
	t.Helper()

	if path := strings.TrimSpace(os.Getenv("WA_MLWH_CACHE_PATH")); path != "" {
		if !looksLikeMySQLDSN(path) {
			return CacheConfig{}, "skipping MySQL cache integration: WA_MLWH_CACHE_PATH does not point at a MySQL DSN"
		}

		return CacheConfig{Path: path, Password: strings.TrimSpace(os.Getenv("WA_MLWH_CACHE_PASSWORD"))}, ""
	}

	repoRoot, err := findRepoRootForTest()
	if err != nil {
		return CacheConfig{}, "skipping MySQL cache integration: could not locate repository root to load development env files"
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

	path := strings.TrimSpace(loaded["WA_MLWH_CACHE_PATH"])
	if path == "" {
		return CacheConfig{}, "skipping MySQL cache integration: WA_MLWH_CACHE_PATH is not set in the environment or development dotenv files"
	}
	if !looksLikeMySQLDSN(path) {
		return CacheConfig{}, "skipping MySQL cache integration: WA_MLWH_CACHE_PATH does not point at a MySQL DSN"
	}

	return CacheConfig{Path: path, Password: strings.TrimSpace(loaded["WA_MLWH_CACHE_PASSWORD"])}, ""
}

func openMySQLCacheForTest(t *testing.T, cfg CacheConfig) Cache {
	t.Helper()

	resolvedDSN, err := resolveMySQLDSN(cfg)
	if err != nil {
		t.Fatalf("resolveMySQLDSN(): %v", err)
	}

	adminCfg, err := mysql.ParseDSN(resolvedDSN)
	if err != nil {
		t.Fatalf("mysql.ParseDSN(resolved): %v", err)
	}
	if adminCfg.DBName == "" {
		t.Fatalf("mysql cache integration requires a DSN with a database name")
	}

	adminCfg.DBName = ""
	adminDB, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatalf("sql.Open(mysql admin): %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })

	testDBName := fmt.Sprintf("wa_mlwh_a3_%d", time.Now().UnixNano())
	if _, err = adminDB.ExecContext(context.Background(), "CREATE DATABASE `"+testDBName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Skipf("skipping MySQL cache integration: create database failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS `"+testDBName+"`")
	})

	testDSN, err := mysql.ParseDSN(normalizeMySQLDSNInput(cfg.Path))
	if err != nil {
		t.Fatalf("mysql.ParseDSN(path): %v", err)
	}
	testDSN.DBName = testDBName

	cache, err := OpenCache(context.Background(), CacheConfig{Path: testDSN.FormatDSN(), Password: cfg.Password})
	if err != nil {
		if isMySQLCacheIntegrationPermissionError(err) {
			t.Skipf("skipping MySQL cache integration: cache user lacks privileges on temporary database: %v", err)
		}

		t.Fatalf("OpenCache(mysql): %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	return cache
}

func isMySQLCacheIntegrationPermissionError(err error) bool {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}

	return mysqlErr.Number == 1044 || mysqlErr.Number == 1049 || mysqlErr.Number == 1142
}

func insertReadBackPacBioProductMetricsMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO pac_bio_product_metrics_mirror(id_pac_bio_product, id_pac_bio_rw_metrics_tmp, id_sample_tmp, id_study_lims, qc, last_updated) VALUES (?, ?, ?, ?, ?, ?)`,
		"pacbio-prod-1", int64(11), int64(101), "6568", nil, "2026-06-26T10:00:00Z",
	); err != nil {
		return err
	}

	var (
		idSampleTmp int64
		idStudyLims string
		qc          sql.NullInt64
	)

	if err := db.QueryRow(
		`SELECT id_sample_tmp, id_study_lims, qc FROM pac_bio_product_metrics_mirror WHERE id_pac_bio_product = ?`,
		"pacbio-prod-1",
	).Scan(&idSampleTmp, &idStudyLims, &qc); err != nil {
		return err
	}

	convey.So(idSampleTmp, convey.ShouldEqual, 101)
	convey.So(idStudyLims, convey.ShouldEqual, "6568")
	convey.So(qc.Valid, convey.ShouldBeFalse)

	return nil
}

func insertReadBackPacBioRunWellMetricsMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_start, run_complete, well_complete, qc_seq_date, run_status, well_status, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(11), "run-A", "A01", int64(1),
		"2026-06-20T00:00:00Z", "2026-06-21T00:00:00Z", "2026-06-22T00:00:00Z", "2026-06-23T00:00:00Z",
		"Complete", "Complete", "2026-06-24T00:00:00Z", "2026-06-21",
	); err != nil {
		return err
	}

	var (
		runName    string
		wellLabel  string
		runStatus  sql.NullString
		wellStatus sql.NullString
		qcSeqDate  sql.NullString
	)

	if err := db.QueryRow(
		`SELECT pac_bio_run_name, well_label, run_status, well_status, qc_seq_date FROM pac_bio_run_well_metrics_mirror WHERE id_pac_bio_rw_metrics_tmp = ?`,
		int64(11),
	).Scan(&runName, &wellLabel, &runStatus, &wellStatus, &qcSeqDate); err != nil {
		return err
	}

	convey.So(runName, convey.ShouldEqual, "run-A")
	convey.So(wellLabel, convey.ShouldEqual, "A01")
	convey.So(runStatus.String, convey.ShouldEqual, "Complete")
	convey.So(wellStatus.String, convey.ShouldEqual, "Complete")
	convey.So(qcSeqDate.String, convey.ShouldEqual, "2026-06-23T00:00:00Z")

	return nil
}

func insertReadBackEseqProductMetricsMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO eseq_product_metrics_mirror(id_eseq_product, id_eseq_flowcell_tmp, id_run, id_sample_tmp, id_study_lims, qc, qc_seq, qc_lib, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"eseq-prod-1", int64(21), int64(7700), int64(102), "6568", nil, nil, nil, "2026-06-26T10:00:00Z",
	); err != nil {
		return err
	}

	var (
		idRun       int64
		idSampleTmp int64
		idStudyLims string
		qc          sql.NullInt64
		qcSeq       sql.NullInt64
		qcLib       sql.NullInt64
	)

	if err := db.QueryRow(
		`SELECT id_run, id_sample_tmp, id_study_lims, qc, qc_seq, qc_lib FROM eseq_product_metrics_mirror WHERE id_eseq_product = ?`,
		"eseq-prod-1",
	).Scan(&idRun, &idSampleTmp, &idStudyLims, &qc, &qcSeq, &qcLib); err != nil {
		return err
	}

	convey.So(idRun, convey.ShouldEqual, 7700)
	convey.So(idSampleTmp, convey.ShouldEqual, 102)
	convey.So(idStudyLims, convey.ShouldEqual, "6568")
	convey.So(qc.Valid, convey.ShouldBeFalse)
	convey.So(qcSeq.Valid, convey.ShouldBeFalse)
	convey.So(qcLib.Valid, convey.ShouldBeFalse)

	return nil
}

func insertReadBackEseqRunMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO eseq_run_mirror(id_eseq_run_tmp, run_name, run_status, run_start, run_complete, last_updated) VALUES (?, ?, ?, ?, ?, ?)`,
		int64(31), "eseq-run-A", "Sequencing", "2026-06-20T00:00:00Z", nil, "2026-06-26T10:00:00Z",
	); err != nil {
		return err
	}

	var (
		runName     string
		runStatus   sql.NullString
		runComplete sql.NullString
	)

	if err := db.QueryRow(
		`SELECT run_name, run_status, run_complete FROM eseq_run_mirror WHERE id_eseq_run_tmp = ?`,
		int64(31),
	).Scan(&runName, &runStatus, &runComplete); err != nil {
		return err
	}

	convey.So(runName, convey.ShouldEqual, "eseq-run-A")
	convey.So(runStatus.String, convey.ShouldEqual, "Sequencing")
	convey.So(runComplete.Valid, convey.ShouldBeFalse)

	return nil
}

func insertReadBackEseqRunLaneMetricsMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO eseq_run_lane_metrics_mirror(id_run, lane, run_started, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
		int64(7700), int64(1), "2026-06-20T00:00:00Z", "2026-06-21T00:00:00Z", "2026-06-26T10:00:00Z", "2026-06-21",
	); err != nil {
		return err
	}

	var (
		idRun       int64
		lane        int64
		runStarted  sql.NullString
		runComplete sql.NullString
	)

	if err := db.QueryRow(
		`SELECT id_run, lane, run_started, run_complete FROM eseq_run_lane_metrics_mirror WHERE id_run = ? AND lane = ?`,
		int64(7700), int64(1),
	).Scan(&idRun, &lane, &runStarted, &runComplete); err != nil {
		return err
	}

	convey.So(idRun, convey.ShouldEqual, 7700)
	convey.So(lane, convey.ShouldEqual, 1)
	convey.So(runStarted.String, convey.ShouldEqual, "2026-06-20T00:00:00Z")
	convey.So(runComplete.String, convey.ShouldEqual, "2026-06-21T00:00:00Z")

	return nil
}

func insertReadBackUseqProductMetricsMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO useq_product_metrics_mirror(id_useq_product, id_useq_wafer_tmp, id_run, id_sample_tmp, id_study_lims, qc, qc_seq, qc_lib, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"useq-prod-1", int64(51), int64(7800), int64(103), "6568", nil, nil, nil, "2026-06-26T10:00:00Z",
	); err != nil {
		return err
	}

	var (
		idUseqWaferTmp int64
		idRun          int64
		idSampleTmp    int64
		idStudyLims    string
		qc             sql.NullInt64
	)

	if err := db.QueryRow(
		`SELECT id_useq_wafer_tmp, id_run, id_sample_tmp, id_study_lims, qc FROM useq_product_metrics_mirror WHERE id_useq_product = ?`,
		"useq-prod-1",
	).Scan(&idUseqWaferTmp, &idRun, &idSampleTmp, &idStudyLims, &qc); err != nil {
		return err
	}

	convey.So(idUseqWaferTmp, convey.ShouldEqual, 51)
	convey.So(idRun, convey.ShouldEqual, 7800)
	convey.So(idSampleTmp, convey.ShouldEqual, 103)
	convey.So(idStudyLims, convey.ShouldEqual, "6568")
	convey.So(qc.Valid, convey.ShouldBeFalse)

	return nil
}

func insertReadBackUseqRunMetricsMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO useq_run_metrics_mirror(id_run, run_name, run_status, run_start, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		int64(7800), "useq-run-A", "Running", "2026-06-20T00:00:00Z", nil, "2026-06-26T10:00:00Z", "",
	); err != nil {
		return err
	}

	var (
		idRun     int64
		runName   string
		runStatus sql.NullString
	)

	if err := db.QueryRow(
		`SELECT id_run, run_name, run_status FROM useq_run_metrics_mirror WHERE id_run = ?`,
		int64(7800),
	).Scan(&idRun, &runName, &runStatus); err != nil {
		return err
	}

	convey.So(idRun, convey.ShouldEqual, 7800)
	convey.So(runName, convey.ShouldEqual, "useq-run-A")
	convey.So(runStatus.String, convey.ShouldEqual, "Running")

	return nil
}

func insertReadBackOseqFlowcellMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO oseq_flowcell_mirror(id_oseq_flowcell_tmp, id_sample_tmp, id_study_lims, experiment_name, run_id, run_uuid, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(71), int64(104), "6568", "ONTRUN-71", nil, "ont-run-uuid-71", "2026-06-24T00:00:00Z", "2026-06-24",
	); err != nil {
		return err
	}

	var (
		idSampleTmp int64
		idStudyLims string
	)

	if err := db.QueryRow(
		`SELECT id_sample_tmp, id_study_lims FROM oseq_flowcell_mirror WHERE id_oseq_flowcell_tmp = ?`,
		int64(71),
	).Scan(&idSampleTmp, &idStudyLims); err != nil {
		return err
	}

	convey.So(idSampleTmp, convey.ShouldEqual, 104)
	convey.So(idStudyLims, convey.ShouldEqual, "6568")

	return nil
}

func insertReadBackIseqRunStatusMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO iseq_run_status_mirror(id_run_status, id_run, date, id_run_status_dict, iscurrent, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
		int64(900), int64(52553), "2026-06-25T09:00:00Z", int64(4), int64(1), "2026-06-25",
	); err != nil {
		return err
	}

	var (
		idRun           int64
		date            string
		idRunStatusDict int64
		iscurrent       int64
	)

	if err := db.QueryRow(
		`SELECT id_run, date, id_run_status_dict, iscurrent FROM iseq_run_status_mirror WHERE id_run_status = ?`,
		int64(900),
	).Scan(&idRun, &date, &idRunStatusDict, &iscurrent); err != nil {
		return err
	}

	convey.So(idRun, convey.ShouldEqual, 52553)
	convey.So(date, convey.ShouldEqual, "2026-06-25T09:00:00Z")
	convey.So(idRunStatusDict, convey.ShouldEqual, 4)
	convey.So(iscurrent, convey.ShouldEqual, 1)

	return nil
}

func insertReadBackIseqRunStatusDictMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO iseq_run_status_dict_mirror(id_run_status_dict, description, temporal_index) VALUES (?, ?, ?)`,
		int64(4), "qc review pending", int64(11),
	); err != nil {
		return err
	}

	var (
		description   string
		temporalIndex sql.NullInt64
	)

	if err := db.QueryRow(
		`SELECT description, temporal_index FROM iseq_run_status_dict_mirror WHERE id_run_status_dict = ?`,
		int64(4),
	).Scan(&description, &temporalIndex); err != nil {
		return err
	}

	convey.So(description, convey.ShouldEqual, "qc review pending")
	convey.So(temporalIndex.Int64, convey.ShouldEqual, 11)

	return nil
}

func insertReadBackSeqOpsTrackingPerSampleMirror(t *testing.T, db *sql.DB) error {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO seq_ops_tracking_per_sample_mirror(
			id_sample_lims, sanger_sample_id, sanger_sample_name, study_id,
			programme, faculty_sponsor, library_type, platform,
			manifest_created, manifest_uploaded, labware_received, order_made,
			working_dilution, library_start, library_complete,
			sequencing_run_start, sequencing_qc_complete
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sample-lims-1", "sanger-1", "7607STDY16897354", "7607",
		"DNA Pipelines", "Sponsor", "Standard", "Illumina",
		"2026-05-29T00:00:00Z", nil, "2026-06-02T00:00:00Z", "2026-06-19T00:00:00Z",
		nil, "2026-06-19T00:00:00Z", "2026-06-19T00:00:00Z",
		"2026-06-25T00:00:00Z", nil,
	); err != nil {
		return err
	}

	var (
		sangerSampleName   string
		studyID            string
		manifestCreated    sql.NullString
		manifestUploaded   sql.NullString
		sequencingRunStart sql.NullString
		sequencingQCDone   sql.NullString
	)

	if err := db.QueryRow(
		`SELECT sanger_sample_name, study_id, manifest_created, manifest_uploaded, sequencing_run_start, sequencing_qc_complete FROM seq_ops_tracking_per_sample_mirror WHERE id_sample_lims = ?`,
		"sample-lims-1",
	).Scan(&sangerSampleName, &studyID, &manifestCreated, &manifestUploaded, &sequencingRunStart, &sequencingQCDone); err != nil {
		return err
	}

	convey.So(sangerSampleName, convey.ShouldEqual, "7607STDY16897354")
	convey.So(studyID, convey.ShouldEqual, "7607")
	convey.So(manifestCreated.String, convey.ShouldEqual, "2026-05-29T00:00:00Z")
	convey.So(manifestUploaded.Valid, convey.ShouldBeFalse)
	convey.So(sequencingRunStart.String, convey.ShouldEqual, "2026-06-25T00:00:00Z")
	convey.So(sequencingQCDone.Valid, convey.ShouldBeFalse)

	return nil
}

func seedA6RunDatePlanRows(t *testing.T, db *sql.DB) {
	t.Helper()

	execSchemaTestSQL(t, db,
		`INSERT INTO iseq_run_status_dict_mirror(id_run_status_dict, description, temporal_index) VALUES (?, ?, ?)`,
		int64(1), "run complete", int64(1),
	)
	execSchemaTestSQL(t, db,
		`INSERT INTO iseq_run_status_dict_mirror(id_run_status_dict, description, temporal_index) VALUES (?, ?, ?)`,
		int64(2), "run archived", int64(2),
	)
	execSchemaTestSQL(t, db,
		`INSERT INTO iseq_run_status_mirror(id_run_status, id_run, date, id_run_status_dict, iscurrent, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
		int64(101), int64(52553), "2026-07-01T09:30:00Z", int64(1), int64(1), "2026-07-01",
	)
	execSchemaTestSQL(t, db,
		`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_start, run_complete, well_complete, qc_seq_date, run_status, well_status, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(201), "pacbio-run-a", "A01", int64(1), "2026-06-01T08:00:00Z", "2026-06-02T08:00:00Z", nil, nil, "Complete", "Complete", "2026-06-03T08:00:00Z", "2026-06-02",
	)
	execSchemaTestSQL(t, db,
		`INSERT INTO oseq_flowcell_mirror(id_oseq_flowcell_tmp, id_sample_tmp, id_study_lims, experiment_name, run_id, run_uuid, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(301), int64(104), "6568", "ONTRUN-11", nil, "ont-run-uuid-11", "2026-06-04T10:00:00Z", "2026-06-04",
	)
	execSchemaTestSQL(t, db,
		`INSERT INTO useq_run_metrics_mirror(id_run, run_name, run_status, run_start, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		int64(401), "ultima-run-a", "run archived", "2026-06-05T08:00:00Z", "2026-06-06T08:00:00Z", "2026-06-07T08:00:00Z", "2026-06-06",
	)
	execSchemaTestSQL(t, db,
		`INSERT INTO eseq_run_lane_metrics_mirror(id_run, lane, run_started, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
		int64(501), int64(1), "2026-06-08T08:00:00Z", "2026-06-09T08:00:00Z", "2026-06-10T08:00:00Z", "2026-06-09",
	)
}

func execSchemaTestSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()

	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec schema test SQL %q: %v", query, err)
	}
}

func assertSQLitePlanUsesIndex(t *testing.T, db *sql.DB, query, indexName string, args ...any) {
	t.Helper()

	details := explainSQLiteQueryPlanDetails(t, db, query, args...)
	for _, detail := range details {
		if strings.Contains(detail, "SEARCH") && strings.Contains(detail, indexName) {
			return
		}
	}

	t.Fatalf("expected EXPLAIN QUERY PLAN for %s to use %s as a SEARCH; details: %v", query, indexName, details)
}

func explainSQLiteQueryPlanDetails(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()

	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()

	var details []string
	for rows.Next() {
		var (
			id     int
			parent int
			unused int
			detail string
		)
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatalf("scan EXPLAIN QUERY PLAN %q: %v", query, err)
		}
		details = append(details, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("read EXPLAIN QUERY PLAN %q: %v", query, err)
	}

	return details
}
