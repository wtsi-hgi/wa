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
	"pac_bio_product_metrics_mirror":     {"id_sample_tmp", "id_study_lims"},
	"pac_bio_run_well_metrics_mirror":    {"pac_bio_run_name,well_label"},
	"eseq_product_metrics_mirror":        {"id_run", "id_sample_tmp", "id_study_lims"},
	"eseq_run_mirror":                    {"run_name"},
	"eseq_run_lane_metrics_mirror":       {"id_run", "run_name,lane"},
	"useq_product_metrics_mirror":        {"id_run", "id_sample_tmp", "id_study_lims", "id_useq_wafer_tmp"},
	"useq_run_metrics_mirror":            {"id_run", "run_name"},
	"oseq_flowcell_mirror":               {"id_sample_tmp", "id_study_lims"},
	"iseq_run_status_mirror":             {"id_run", "id_run,date"},
	"iseq_run_status_dict_mirror":        nil,
	"seq_ops_tracking_per_sample_mirror": {"id_sample_lims", "sanger_sample_name", "study_id"},
}

func TestLoadSchema(t *testing.T) {
	convey.Convey("Given the SQLite schema files", t, func() {
		stmts, err := loadSchema("sqlite")

		convey.Convey("when loadSchema runs, then it returns the 9 table statements in spec order", func() {
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

func TestSeqProductIRODSLocationsMirrorSQLiteShapeHasCreatedPlatformAndCreatedIndex(t *testing.T) {
	convey.Convey("A1.1: Given the SQLite schema parsed into a schemaShape", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when seq_product_irods_locations_mirror is inspected, then it has created and platform text columns and an index on (id_study_lims, created)", func() {
			columns := shape.Tables["seq_product_irods_locations_mirror"]
			convey.So(columns, convey.ShouldContainKey, "created")
			convey.So(columns, convey.ShouldContainKey, "platform")
			convey.So(columns["created"], convey.ShouldEqual, "text")
			convey.So(columns["platform"], convey.ShouldEqual, "text")
			convey.So(shape.Index["seq_product_irods_locations_mirror"], convey.ShouldContain, "id_study_lims,created")
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

		convey.Convey("when seq_product_irods_locations_mirror is inspected, then it has the same created/platform columns and (id_study_lims, created) index, so the two dialects compare equal", func() {
			columns := mysqlShape.Tables["seq_product_irods_locations_mirror"]
			convey.So(columns, convey.ShouldContainKey, "created")
			convey.So(columns, convey.ShouldContainKey, "platform")
			convey.So(columns["created"], convey.ShouldEqual, "text")
			convey.So(columns["platform"], convey.ShouldEqual, "text")
			convey.So(mysqlShape.Index["seq_product_irods_locations_mirror"], convey.ShouldContain, "id_study_lims,created")

			convey.So(mysqlShape.Tables["seq_product_irods_locations_mirror"], convey.ShouldResemble, sqliteShape.Tables["seq_product_irods_locations_mirror"])
			convey.So(mysqlShape.Index["seq_product_irods_locations_mirror"], convey.ShouldResemble, sqliteShape.Index["seq_product_irods_locations_mirror"])
			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)
			convey.So(compareCacheSchemaShapes(mysqlShape, sqliteShape), convey.ShouldBeNil)
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
			convey.So(compareCacheSchemaShapes(mysqlShape, sqliteShape), convey.ShouldBeNil)

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

			convey.So(shape.Index["seq_ops_tracking_per_sample_mirror"], convey.ShouldResemble, []string{"id_sample_lims", "sanger_sample_name", "study_id"})
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

		convey.Convey("when comparing the per-table index column lists, then they match across dialects (including the sample_search_token prefix index)", func() {
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

		convey.Convey("when the full schema parity is compared, then tables, columns, indexes, and unique constraints all match across dialects", func() {
			convey.So(sqliteErr, convey.ShouldBeNil)
			convey.So(mysqlErr, convey.ShouldBeNil)
			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldBeNil)
			convey.So(compareCacheSchemaShapes(mysqlShape, sqliteShape), convey.ShouldBeNil)
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

		convey.Convey("when the schema is executed against SQLite, then all 9 cache tables are created", func() {
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

func TestSeqProductIRODSLocationsMirrorEphemeralInsertReadsBackCreatedAndPlatform(t *testing.T) {
	convey.Convey("A1.3: Given an opened ephemeral SQLite cache", t, func() {
		db := openSQLiteSchemaTestDB(t)

		created := "2026-06-25T09:30:00Z"
		platform := "illumina"
		_, err := db.Exec(
			`INSERT INTO seq_product_irods_locations_mirror(id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"product-a1", "/seq", "run/1.cram", "/seq/run", "1.cram", int64(101), "6568",
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
		`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_start, run_complete, well_complete, qc_seq_date, run_status, well_status, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(11), "run-A", "A01", int64(1),
		"2026-06-20T00:00:00Z", "2026-06-21T00:00:00Z", "2026-06-22T00:00:00Z", "2026-06-23T00:00:00Z",
		"Complete", "Complete", "2026-06-24T00:00:00Z",
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
		`INSERT INTO eseq_run_lane_metrics_mirror(id_eseq_rlm_tmp, id_run, run_name, lane, run_started, run_complete, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		int64(41), int64(7700), "eseq-run-A", int64(1), "2026-06-20T00:00:00Z", "2026-06-21T00:00:00Z", "2026-06-26T10:00:00Z",
	); err != nil {
		return err
	}

	var (
		idRun       int64
		runName     string
		lane        int64
		runStarted  sql.NullString
		runComplete sql.NullString
	)

	if err := db.QueryRow(
		`SELECT id_run, run_name, lane, run_started, run_complete FROM eseq_run_lane_metrics_mirror WHERE id_eseq_rlm_tmp = ?`,
		int64(41),
	).Scan(&idRun, &runName, &lane, &runStarted, &runComplete); err != nil {
		return err
	}

	convey.So(idRun, convey.ShouldEqual, 7700)
	convey.So(runName, convey.ShouldEqual, "eseq-run-A")
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
		`INSERT INTO useq_run_metrics_mirror(id_useq_run_metrics_tmp, id_run, run_name, run_status, run_start, run_complete, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		int64(61), int64(7800), "useq-run-A", "Running", "2026-06-20T00:00:00Z", nil, "2026-06-26T10:00:00Z",
	); err != nil {
		return err
	}

	var (
		idRun     int64
		runName   string
		runStatus sql.NullString
	)

	if err := db.QueryRow(
		`SELECT id_run, run_name, run_status FROM useq_run_metrics_mirror WHERE id_useq_run_metrics_tmp = ?`,
		int64(61),
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
		`INSERT INTO oseq_flowcell_mirror(id_oseq_flowcell_tmp, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`,
		int64(71), int64(104), "6568",
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
		`INSERT INTO iseq_run_status_mirror(id_run_status, id_run, date, id_run_status_dict, iscurrent) VALUES (?, ?, ?, ?, ?)`,
		int64(900), int64(52553), "2026-06-25T09:00:00Z", int64(4), int64(1),
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
