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
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

// TestQueryReferencesTable is a hermetic (no DB) unit test for the
// boundary-aware containment that decides sync-table coverage. It pins the
// behaviour that distinguishes a whole-identifier table reference from a mere
// substring, which is what stops a table being falsely reported as covered.
func TestQueryReferencesTable(t *testing.T) {
	convey.Convey("queryReferencesTable matches a table only as a whole SQL identifier", t, func() {
		convey.Convey("it matches the table when referenced as a real identifier", func() {
			convey.So(queryReferencesTable("SELECT * FROM iseq_run_status WHERE x > ?", "iseq_run_status"), convey.ShouldBeTrue)
			convey.So(queryReferencesTable("SELECT * FROM a JOIN iseq_run_status x ON x.id = a.id", "iseq_run_status"), convey.ShouldBeTrue)
			convey.So(queryReferencesTable("SELECT * FROM mlwh_reporting.seq_ops_tracking_per_sample", "mlwh_reporting.seq_ops_tracking_per_sample"), convey.ShouldBeTrue)
			convey.So(queryReferencesTable("SELECT * FROM iseq_run_status", "iseq_run_status"), convey.ShouldBeTrue)
			convey.So(queryReferencesTable("SELECT * FROM iseq_run_status(", "iseq_run_status"), convey.ShouldBeTrue)
			convey.So(queryReferencesTable("SELECT iseq_run_status,other FROM t", "iseq_run_status"), convey.ShouldBeTrue)
			convey.So(queryReferencesTable("SELECT * FROM iseq_run_status;", "iseq_run_status"), convey.ShouldBeTrue)
			convey.So(queryReferencesTable("SELECT * FROM\niseq_run_status\nWHERE x = 1", "iseq_run_status"), convey.ShouldBeTrue)
		})

		convey.Convey("it is case-insensitive", func() {
			convey.So(queryReferencesTable("SELECT * FROM ISEQ_RUN_STATUS", "iseq_run_status"), convey.ShouldBeTrue)
		})

		convey.Convey("it does NOT match when the table name is only a substring of a longer identifier", func() {
			convey.So(queryReferencesTable("SELECT * FROM iseq_run_status_dict", "iseq_run_status"), convey.ShouldBeFalse)
			convey.So(queryReferencesTable("SELECT id_sample_lims FROM t", "sample"), convey.ShouldBeFalse)
			convey.So(queryReferencesTable("SELECT * FROM mlwh_reporting.seq_ops_tracking_per_sample", "sample"), convey.ShouldBeFalse)
			convey.So(queryReferencesTable("SELECT * FROM eseq_run_lane_metrics", "eseq_run"), convey.ShouldBeFalse)
		})
	})
}
