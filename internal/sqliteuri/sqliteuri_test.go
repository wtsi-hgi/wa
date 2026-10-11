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

package sqliteuri

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	_ "modernc.org/sqlite"
)

func TestFile(t *testing.T) {
	names := []string{
		"f#1.sqlite", "f?1.sqlite", "f%231.sqlite", "with space.db", "a+b.db", "a:b.db", "ünï.db",
	}

	for _, name := range names {
		convey.Convey("Given the file name "+name+", the URI opens exactly that file read-write and read-only", t, func() {
			dir := t.TempDir()
			path := filepath.Join(dir, name)

			createTestTable(path)

			convey.So(listDir(dir), convey.ShouldResemble, []string{name})
			convey.So(countTestRows(File(path, "mode=ro")), convey.ShouldEqual, 1)
		})
	}

	convey.Convey("A relative path opens that file relative to the working directory", t, func() {
		dir := t.TempDir()
		t.Chdir(dir)

		createTestTable(filepath.Join("sub", "r#1.db"))

		convey.So(listDir(filepath.Join(dir, "sub")), convey.ShouldResemble, []string{"r#1.db"})
	})

	convey.Convey("An absolute path with a leading double slash is not read as a URI authority", t, func() {
		dir := t.TempDir()

		createTestTable("/" + filepath.Join(dir, "d.db"))

		convey.So(listDir(dir), convey.ShouldResemble, []string{"d.db"})
	})
}

func createTestTable(path string) {
	if dir := filepath.Dir(path); dir != "." {
		convey.So(os.MkdirAll(dir, 0o700), convey.ShouldBeNil)
	}

	db, err := sql.Open("sqlite", File(path, "mode=rwc"))
	convey.So(err, convey.ShouldBeNil)

	_, err = db.Exec("CREATE TABLE t (a INTEGER); INSERT INTO t VALUES (1)")
	convey.So(err, convey.ShouldBeNil)
	convey.So(db.Close(), convey.ShouldBeNil)
}

func listDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	convey.So(err, convey.ShouldBeNil)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

func countTestRows(dsn string) int {
	db, err := sql.Open("sqlite", dsn)
	convey.So(err, convey.ShouldBeNil)

	defer func() { _ = db.Close() }()

	var count int
	convey.So(db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count), convey.ShouldBeNil)

	return count
}
