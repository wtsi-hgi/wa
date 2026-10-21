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

package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestMLWHFreshnessCheckLocalCache(t *testing.T) {
	now := time.Now().UTC()

	convey.Convey("Given a local cache where every table succeeded 10 minutes ago", t, func() {
		path, _ := freshnessTestCache(t, now.Add(-10*time.Minute))
		setFreshnessTestEnv(t, path)

		convey.Convey("then --check is silent and succeeds", func() {
			output, err := executeRootCommandForTest(t, []string{"mlwh", "freshness", "--check"})
			convey.So(err, convey.ShouldBeNil)
			convey.So(output, convey.ShouldBeEmpty)
		})

		convey.Convey("then WA_MLWH_STALE_AFTER shorter than that makes --check fail with every table stale", func() {
			t.Setenv("WA_MLWH_STALE_AFTER", "5m")

			output, err := executeRootCommandForTest(t, []string{"mlwh", "freshness", "--check"})
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(output, convey.ShouldContainSubstring, "sample: stale")
			convey.So(err.Error(), convey.ShouldContainSubstring, "stale")
		})

		convey.Convey("then an invalid WA_MLWH_STALE_AFTER is an error", func() {
			t.Setenv("WA_MLWH_STALE_AFTER", "soon")

			_, err := executeRootCommandForTest(t, []string{"mlwh", "freshness", "--check"})
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldContainSubstring, "WA_MLWH_STALE_AFTER")
		})
	})

	convey.Convey("Given a local cache with one failing table and one stale table", t, func() {
		path, tables := freshnessTestCache(t, now.Add(-10*time.Minute))
		setFreshnessTestEnv(t, path)

		execFreshnessTestSQL(t, path,
			`UPDATE sync_outcome SET last_error = ?, last_error_at = ? WHERE table_name = 'study'`,
			"mlwh: sync study: dial tcp: connection refused\nsecond line", now.Add(-time.Minute).Format(time.RFC3339))
		execFreshnessTestSQL(t, path,
			`UPDATE sync_outcome SET last_success_at = ? WHERE table_name = 'sample'`,
			now.Add(-5*time.Hour).Format(time.RFC3339))

		convey.Convey("then --check fails with one stderr line per problem table and an overall summary", func() {
			output, err := executeRootCommandForTest(t, []string{"mlwh", "freshness", "--check"})
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldContainSubstring, "failing")
			convey.So(err.Error(), convey.ShouldContainSubstring, "2 of "+strconv.Itoa(len(tables)))

			lines := nonEmptyLines(output)
			convey.So(lines, convey.ShouldHaveLength, 3)
			convey.So(lines[0], convey.ShouldStartWith, "study: failing")
			convey.So(lines[0], convey.ShouldContainSubstring, "dial tcp: connection refused")
			convey.So(lines[0], convey.ShouldNotContainSubstring, "second line")
			convey.So(lines[1], convey.ShouldStartWith, "sample: stale")
			convey.So(lines[1], convey.ShouldContainSubstring, "last success "+now.Add(-5*time.Hour).Format("2006-01-02T15:04:05Z"))
			convey.So(lines[2], convey.ShouldStartWith, "Error:")
		})

		convey.Convey("then without --check it lists every table and succeeds", func() {
			output, err := executeRootCommandForTest(t, []string{"mlwh", "freshness"})
			convey.So(err, convey.ShouldBeNil)
			convey.So(output, convey.ShouldContainSubstring, "Overall: failing")

			lines := nonEmptyLines(output)
			convey.So(len(lines), convey.ShouldEqual, len(tables)+2)
			convey.So(output, convey.ShouldContainSubstring, "connection refused")
		})

		convey.Convey("then --json emits the freshness document", func() {
			output, err := executeRootCommandForTest(t, []string{"mlwh", "freshness", "--json"})
			convey.So(err, convey.ShouldBeNil)

			var decoded mlwh.Freshness
			convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
			convey.So(decoded.Status, convey.ShouldEqual, mlwh.FreshnessStatusFailing)
		})
	})
}

// freshnessTestCache creates a SQLite cache in which every sync table last
// succeeded at lastSuccess, and returns its path and the table names.
func freshnessTestCache(t *testing.T, lastSuccess time.Time) (string, []string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cache.sqlite")

	client, err := mlwh.OpenCacheOnly(context.Background(), mlwh.CacheConfig{Path: path})
	if err != nil {
		t.Fatalf("OpenCacheOnly(): %v", err)
	}

	freshness, err := client.Freshness(context.Background())
	if err != nil {
		t.Fatalf("Freshness(): %v", err)
	}

	if err = client.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	tables := make([]string, 0, len(freshness.Tables))
	for _, table := range freshness.Tables {
		tables = append(tables, table.Table)
		execFreshnessTestSQL(t, path,
			`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, NULL, 0)`,
			table.Table, lastSuccess.Format(time.RFC3339), lastSuccess.Format(time.RFC3339))
		execFreshnessTestSQL(t, path,
			`INSERT INTO sync_outcome(table_name, last_success_at) VALUES (?, ?)`,
			table.Table, lastSuccess.Format(time.RFC3339))
	}

	return path, tables
}

func TestMLWHFreshnessCheckRemoteServer(t *testing.T) {
	convey.Convey("Given a remote server reporting every table ok", t, func() {
		setFreshnessTestEnv(t, "")
		t.Setenv("WA_MLWH_SERVER_URL", freshnessTestServer(t,
			`{"status":"ok","stale_after_seconds":7200,"tables":[{"table":"sample","status":"ok","last_success_at":"2026-10-21T10:00:00Z","blocked_by":[]}]}`))

		output, err := executeRootCommandForTest(t, []string{"mlwh", "freshness", "--check"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldBeEmpty)
	})

	convey.Convey("Given a remote server reporting a skipped table", t, func() {
		setFreshnessTestEnv(t, "")
		t.Setenv("WA_MLWH_SERVER_URL", freshnessTestServer(t,
			`{"status":"failing","stale_after_seconds":7200,"tables":[`+
				`{"table":"sample","status":"ok","last_success_at":"2026-10-21T10:00:00Z","blocked_by":[]},`+
				`{"table":"seq_product_irods_locations","status":"failing","last_success_at":"","last_error":"skipped: dependency sync failed: iseq_product_metrics","blocked_by":["iseq_product_metrics"]}]}`))

		output, err := executeRootCommandForTest(t, []string{"mlwh", "freshness", "--check"})

		convey.So(err, convey.ShouldNotBeNil)

		lines := nonEmptyLines(output)
		convey.So(lines, convey.ShouldHaveLength, 2)
		convey.So(lines[0], convey.ShouldEqual,
			"seq_product_irods_locations: failing, last success never, blocked by iseq_product_metrics: skipped: dependency sync failed: iseq_product_metrics")
	})

	convey.Convey("Given a remote server where several tables share one connect error", t, func() {
		setFreshnessTestEnv(t, "")

		connectErr := "connect to MLWH source: mlwh: ping source db: dial tcp 127.0.0.1:1: connect: connection refused"
		t.Setenv("WA_MLWH_SERVER_URL", freshnessTestServer(t,
			`{"status":"failing","stale_after_seconds":7200,"tables":[`+
				`{"table":"sample","status":"failing","last_success_at":"2026-10-21T10:00:00Z","last_error":"`+connectErr+`","blocked_by":[]},`+
				`{"table":"study","status":"failing","last_success_at":"2026-10-21T10:00:00Z","last_error":"`+connectErr+`","blocked_by":[]},`+
				`{"table":"iseq_flowcell","status":"ok","last_success_at":"2026-10-21T10:00:00Z","blocked_by":[]},`+
				`{"table":"iseq_run_status","status":"failing","last_success_at":"","last_error":"`+connectErr+`","blocked_by":[]},`+
				`{"table":"useq_run_metrics","status":"failing","last_success_at":"2026-10-21T09:00:00Z","last_error":"other failure","blocked_by":[]}]}`))

		output, err := executeRootCommandForTest(t, []string{"mlwh", "freshness", "--check"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeError)

		convey.Convey("then they share one line, and a table with its own error keeps its line", func() {
			lines := nonEmptyLines(output)
			convey.So(lines, convey.ShouldHaveLength, 3)
			convey.So(lines[0], convey.ShouldEqual,
				"3 tables failing (sample, study, iseq_run_status): "+connectErr)
			convey.So(lines[1], convey.ShouldEqual,
				"useq_run_metrics: failing, last success 2026-10-21T09:00:00Z: other failure")
			convey.So(lines[2], convey.ShouldContainSubstring, "4 of 5 tables need attention")
		})
	})

	convey.Convey("Given a remote server too old to report sync status", t, func() {
		setFreshnessTestEnv(t, "")
		t.Setenv("WA_MLWH_SERVER_URL", freshnessTestServer(t,
			`{"tables":[{"table":"sample","high_water":"","last_run":"2026-10-21T10:00:00Z","ever_synced":true}]}`))

		output, err := executeRootCommandForTest(t, []string{"mlwh", "freshness", "--check"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "1.12.0")
		convey.So(nonEmptyLines(output), convey.ShouldHaveLength, 1)
	})
}

func setFreshnessTestEnv(t *testing.T, cachePath string) {
	t.Helper()

	t.Setenv("WA_ENV", "")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_MLWH_DSN", "")
	t.Setenv("WA_MLWH_STALE_AFTER", "")
	t.Setenv("WA_MLWH_CACHE_PATH", cachePath)
}

func freshnessTestServer(t *testing.T, body string) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/freshness" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server.URL
}

func execFreshnessTestSQL(t *testing.T, path, stmt string, args ...any) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err = db.Exec(stmt, args...); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}

func nonEmptyLines(output string) []string {
	var lines []string
	for line := range strings.SplitSeq(output, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}

	return lines
}
