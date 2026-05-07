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
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode"

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

func TestParseSchemaShapeParity(t *testing.T) {
	convey.Convey("Given both dialect schema directories", t, func() {
		sqliteSchema, sqliteErr := loadSchema("sqlite")
		mysqlSchema, mysqlErr := loadSchema("mysql")
		convey.So(sqliteErr, convey.ShouldBeNil)
		convey.So(mysqlErr, convey.ShouldBeNil)

		sqliteShape, sqliteErr := parseSchemaShape(sqliteSchema)
		mysqlShape, mysqlErr := parseSchemaShape(mysqlSchema)

		convey.Convey("when the parsed schema shapes are compared, then tables, columns, and index columns match", func() {
			convey.So(sqliteErr, convey.ShouldBeNil)
			convey.So(mysqlErr, convey.ShouldBeNil)
			convey.So(sqliteShape, convey.ShouldResemble, mysqlShape)
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

		tables := make([]string, 0, len(schemaStatementOrder))
		for rows.Next() {
			var table string
			convey.So(rows.Scan(&table), convey.ShouldBeNil)
			tables = append(tables, table)
		}

		convey.Convey("when the schema loader runs, then the nine cache tables exist", func() {
			convey.So(rows.Err(), convey.ShouldBeNil)
			convey.So(tables, convey.ShouldResemble, schemaStatementOrder)
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

func TestMirrorSchemaCoversStructFields(t *testing.T) {
	convey.Convey("Given the parsed SQLite mirror shapes", t, func() {
		stmts, err := loadSchema("sqlite")
		convey.So(err, convey.ShouldBeNil)

		shape, err := parseSchemaShape(stmts)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when inspected, then the mirror tables include last_updated and every Study and Sample field", func() {
			convey.So(shape.Tables["study_mirror"], convey.ShouldContainKey, "last_updated")
			convey.So(shape.Tables["sample_mirror"], convey.ShouldContainKey, "last_updated")

			assertStructColumns(t, shape.Tables["study_mirror"], reflect.TypeOf(Study{}))
			assertStructColumns(t, shape.Tables["sample_mirror"], reflect.TypeOf(Sample{}))
		})
	})
}

func assertStructColumns(t *testing.T, columns map[string]string, typ reflect.Type) {
	t.Helper()

	for i := range typ.NumField() {
		field := typ.Field(i)
		_, ok := columns[toSnakeCase(field.Name)]
		convey.So(ok, convey.ShouldBeTrue)
	}
}

func toSnakeCase(name string) string {
	var builder strings.Builder

	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := rune(name[i-1])
				nextLower := false
				if i+1 < len(name) {
					nextLower = unicode.IsLower(rune(name[i+1]))
				}

				if unicode.IsLower(prev) || nextLower {
					builder.WriteByte('_')
				}
			}

			builder.WriteRune(unicode.ToLower(r))

			continue
		}

		builder.WriteRune(r)
	}

	return builder.String()
}
