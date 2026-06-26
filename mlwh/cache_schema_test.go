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

func TestSampleSearchSchemaSQLiteDeclaresTrigramVirtualTable(t *testing.T) {
	convey.Convey("B1.1: Given the embedded SQLite sample search schema", t, func() {
		ddl, err := loadSearchIndexSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when inspected, then it declares an fts5 virtual table over the four sample fields with the trigram tokenizer", func() {
			upper := strings.ToUpper(ddl)
			convey.So(upper, convey.ShouldContainSubstring, "CREATE VIRTUAL TABLE")
			convey.So(upper, convey.ShouldContainSubstring, "USING FTS5")
			convey.So(ddl, convey.ShouldContainSubstring, "sample_search")
			convey.So(ddl, convey.ShouldContainSubstring, "content='sample_mirror'")
			convey.So(ddl, convey.ShouldContainSubstring, "content_rowid='id_sample_tmp'")
			convey.So(ddl, convey.ShouldContainSubstring, "tokenize='trigram'")

			for _, column := range []string{"name", "supplier_name", "common_name", "donor_id"} {
				convey.So(ddl, convey.ShouldContainSubstring, column)
			}
		})
	})
}

func TestSampleSearchSchemaMySQLDeclaresNgramFulltextIndex(t *testing.T) {
	convey.Convey("B1.2: Given the embedded MySQL sample search schema string", t, func() {
		ddl, err := loadSearchIndexSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when parsed, then it declares one ngram FULLTEXT index over exactly the four columns on sample_mirror", func() {
			statements := splitSQLStatements(ddl)
			convey.So(statements, convey.ShouldHaveLength, 1)

			table, columns, parser, err := parseMySQLFulltextIndex(statements[0])
			convey.So(err, convey.ShouldBeNil)
			convey.So(table, convey.ShouldEqual, "sample_mirror")
			convey.So(parser, convey.ShouldEqual, "ngram")
			convey.So(columns, convey.ShouldResemble, []string{"name", "supplier_name", "common_name", "donor_id"})
		})
	})
}

func TestParseSchemaShapeRecordsSearchIndex(t *testing.T) {
	convey.Convey("B2.1: Given the SQLite and MySQL full schemas at the current version", t, func() {
		sqliteFull, err := loadFullSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		mysqlFull, err := loadFullSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		sqliteShape, err := parseSchemaShape(sqliteFull)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlFull)
		convey.So(err, convey.ShouldBeNil)

		wantColumns := []string{"common_name", "donor_id", "name", "supplier_name"}

		convey.Convey("when parseSchemaShape runs on each, then both record a full-text search index over the same column set on sample_mirror", func() {
			convey.So(sqliteShape.FullText["sample_mirror"], convey.ShouldResemble, wantColumns)
			convey.So(mysqlShape.FullText["sample_mirror"], convey.ShouldResemble, wantColumns)
			convey.So(sqliteShape.FullText, convey.ShouldResemble, mysqlShape.FullText)
		})
	})
}

func TestParseSchemaShapeFullTextIsColumnSetSensitive(t *testing.T) {
	convey.Convey("B2.3: Given a deliberately divergent search index (SQLite fts5 over three fields, MySQL FULLTEXT over four)", t, func() {
		mysqlFull, err := loadFullSchema("mysql")
		convey.So(err, convey.ShouldBeNil)

		// Replace the real four-column SQLite fts5 search index with a divergent
		// three-column one, leaving the rest of the schema identical to MySQL.
		sqliteBase, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)
		sqliteDivergent := append(sqliteBase, `CREATE VIRTUAL TABLE sample_search USING fts5(
			name, supplier_name, common_name,
			content='sample_mirror', content_rowid='id_sample_tmp', tokenize='trigram')`)

		sqliteShape, err := parseSchemaShape(sqliteDivergent)
		convey.So(err, convey.ShouldBeNil)
		mysqlShape, err := parseSchemaShape(mysqlFull)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when the parity model compares them, then the full-text representations differ and the parity comparison fails", func() {
			convey.So(sqliteShape.FullText["sample_mirror"], convey.ShouldResemble, []string{"common_name", "name", "supplier_name"})
			convey.So(sqliteShape.FullText, convey.ShouldNotResemble, mysqlShape.FullText)
			convey.So(compareCacheSchemaShapes(mysqlShape, sqliteShape), convey.ShouldNotBeNil)
			convey.So(compareCacheSchemaShapes(sqliteShape, mysqlShape), convey.ShouldNotBeNil)
		})
	})
}

func TestParseSchemaShapeRecognisesInlineMySQLFulltext(t *testing.T) {
	convey.Convey("Given a MySQL CREATE TABLE with an inline FULLTEXT ... WITH PARSER ngram clause", t, func() {
		stmts := []string{`CREATE TABLE sample_mirror (
			id_sample_tmp INTEGER NOT NULL,
			name VARCHAR(255) NOT NULL,
			supplier_name VARCHAR(255) NOT NULL,
			common_name VARCHAR(255) NOT NULL,
			donor_id VARCHAR(255) NOT NULL,
			FULLTEXT (name, supplier_name, common_name, donor_id) WITH PARSER ngram
		)`}

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when parsed, then the inline full-text index is recorded as the normalised search column set, not as a table column", func() {
			convey.So(shape.FullText["sample_mirror"], convey.ShouldResemble, []string{"common_name", "donor_id", "name", "supplier_name"})
			convey.So(shape.Tables["sample_mirror"], convey.ShouldNotContainKey, "FULLTEXT")
		})
	})
}

func TestCompareCacheSchemaShapesDetectsFullTextDivergence(t *testing.T) {
	convey.Convey("Given two shapes that agree on tables, indexes, and unique constraints but differ on the full-text search index", t, func() {
		expected := schemaShape{FullText: map[string][]string{"sample_mirror": {"common_name", "donor_id", "name", "supplier_name"}}}
		actual := schemaShape{FullText: map[string][]string{"sample_mirror": {"common_name", "donor_id", "name"}}}

		convey.Convey("when compared, then the comparison reports a full-text search index mismatch", func() {
			convey.So(compareCacheSchemaShapes(expected, actual), convey.ShouldNotBeNil)
		})

		convey.Convey("when an identical full-text shape is compared, then the comparison passes", func() {
			convey.So(compareCacheSchemaShapes(expected, expected), convey.ShouldBeNil)
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

		convey.Convey("when the parsed schema shapes are compared, then the v2 table names match exactly", func() {
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

		convey.Convey("B2.2: when the full schema (including the search index) is parsed, then the full-text search representation matches across dialects along with tables, columns, indexes, and unique constraints", func() {
			sqliteFull, sqliteFullErr := loadFullSchema("sqlite")
			mysqlFull, mysqlFullErr := loadFullSchema("mysql")
			convey.So(sqliteFullErr, convey.ShouldBeNil)
			convey.So(mysqlFullErr, convey.ShouldBeNil)

			sqliteFullShape, sqliteFullErr := parseSchemaShape(sqliteFull)
			mysqlFullShape, mysqlFullErr := parseSchemaShape(mysqlFull)
			convey.So(sqliteFullErr, convey.ShouldBeNil)
			convey.So(mysqlFullErr, convey.ShouldBeNil)

			convey.So(sqliteFullShape.Tables, convey.ShouldResemble, mysqlFullShape.Tables)
			convey.So(sqliteFullShape.Index, convey.ShouldResemble, mysqlFullShape.Index)
			convey.So(sqliteFullShape.Unique, convey.ShouldResemble, mysqlFullShape.Unique)
			convey.So(sqliteFullShape.FullText, convey.ShouldResemble, mysqlFullShape.FullText)
			convey.So(compareCacheSchemaShapes(sqliteFullShape, mysqlFullShape), convey.ShouldBeNil)
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

		convey.Convey("when the schema loader runs, then the nine cache tables and the sample_search index exist", func() {
			convey.So(rows.Err(), convey.ShouldBeNil)
			for _, table := range schemaStatementOrder {
				convey.So(tables, convey.ShouldContain, table)
			}
			convey.So(tables, convey.ShouldContain, sampleSearchTable)
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
