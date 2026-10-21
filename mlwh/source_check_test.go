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
	"path/filepath"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	_ "modernc.org/sqlite"
)

func TestPrepareSyncSourceQueriesReportsEachQueryWithoutExecuting(t *testing.T) {
	convey.Convey("Given a source with a study table holding one row", t, func() {
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "source.sqlite"))
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		mustExec(t, db, `CREATE TABLE study (id_study_tmp INTEGER PRIMARY KEY, id_lims TEXT NOT NULL, name TEXT NOT NULL)`)
		mustExec(t, db, `INSERT INTO study(id_study_tmp, id_lims, name) VALUES (1, 'SQSCP', 'kept')`)

		queries := []SyncSourceQuery{
			{Name: "valid", Query: `SELECT name FROM study WHERE id_lims = ?`, ArgCount: 1},
			{Name: "unknown column", Query: `SELECT application_type FROM study`},
			{Name: "unknown table", Query: `SELECT id FROM useq_wafer`},
			{Name: "would delete", Query: `DELETE FROM study`},
		}

		checks := PrepareSyncSourceQueries(context.Background(), db, queries)

		convey.Convey("when every query is prepared, then each gets its own verdict in input order", func() {
			convey.So(checks, convey.ShouldHaveLength, 4)
			convey.So(checks[0], convey.ShouldResemble, SourceQueryCheck{Name: "valid", OK: true})
			convey.So(checks[1].Name, convey.ShouldEqual, "unknown column")
			convey.So(checks[1].OK, convey.ShouldBeFalse)
			convey.So(checks[1].Error, convey.ShouldContainSubstring, "application_type")
			convey.So(checks[2].OK, convey.ShouldBeFalse)
			convey.So(checks[2].Error, convey.ShouldContainSubstring, "useq_wafer")
			convey.So(checks[3], convey.ShouldResemble, SourceQueryCheck{Name: "would delete", OK: true})
		})

		convey.Convey("when a prepared statement would modify data, then nothing is executed", func() {
			convey.So(countRows(t, db, `SELECT COUNT(*) FROM study`), convey.ShouldEqual, 1)
		})
	})
}
