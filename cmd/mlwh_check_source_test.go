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
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	_ "modernc.org/sqlite"

	"github.com/wtsi-hgi/wa/mlwh"
)

func TestMLWHCheckSourceCommand(t *testing.T) {
	convey.Convey("Given wa mlwh check-source against a source with a study table", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
		t.Setenv("WA_MLWH_PASSWORD", "secret")

		source, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "source.sqlite"))
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = source.Close() }()
		_, err = source.Exec(`CREATE TABLE study (id_study_tmp INTEGER PRIMARY KEY, id_lims TEXT NOT NULL)`)
		convey.So(err, convey.ShouldBeNil)

		var openedDSN, openedPassword string

		originalOpen, originalQueries := openMLWHCheckSource, mlwhCheckSourceQueries
		defer func() { openMLWHCheckSource, mlwhCheckSourceQueries = originalOpen, originalQueries }()

		openMLWHCheckSource = func(_ context.Context, dsn, password string) (*sql.DB, error) {
			openedDSN, openedPassword = dsn, password

			return source, nil
		}

		validQuery := mlwh.SyncSourceQuery{Name: "study incremental", Query: `SELECT id_study_tmp FROM study WHERE id_lims = ?`, ArgCount: 1}
		brokenQuery := mlwh.SyncSourceQuery{Name: "useq_wafer ids", Query: `SELECT id_useq_wafer_tmp FROM useq_wafer`}

		convey.Convey("when every query prepares, then it lists each as ok and exits zero", func() {
			mlwhCheckSourceQueries = func() []mlwh.SyncSourceQuery { return []mlwh.SyncSourceQuery{validQuery} }

			output, runErr := executeRootCommandForTest(t, []string{"mlwh", "check-source"})

			convey.So(runErr, convey.ShouldBeNil)
			convey.So(openedDSN, convey.ShouldEqual, "mlwh_user@tcp(localhost:3306)/mlwarehouse")
			convey.So(openedPassword, convey.ShouldEqual, "secret")
			convey.So(output, convey.ShouldEqual, "ok    study incremental\nall 1 sync source queries prepared against the MLWH source")
		})

		convey.Convey("when a query fails to prepare, then it names the query and error and exits non-zero", func() {
			mlwhCheckSourceQueries = func() []mlwh.SyncSourceQuery { return []mlwh.SyncSourceQuery{validQuery, brokenQuery} }

			output, runErr := executeRootCommandForTest(t, []string{"mlwh", "check-source"})

			convey.So(runErr, convey.ShouldNotBeNil)
			convey.So(runErr.Error(), convey.ShouldEqual, "1 of 2 sync source queries failed to prepare against the MLWH source")
			convey.So(output, convey.ShouldContainSubstring, "ok    study incremental\n")
			convey.So(output, convey.ShouldContainSubstring, "FAIL  useq_wafer ids: ")
			convey.So(output, convey.ShouldContainSubstring, "useq_wafer")
		})

		convey.Convey("when --json is given, then stdout is one JSON report and a failure still exits non-zero", func() {
			mlwhCheckSourceQueries = func() []mlwh.SyncSourceQuery { return []mlwh.SyncSourceQuery{validQuery, brokenQuery} }

			stdout, runErr := executeMLWHCheckSourceStdout(t, "--json")

			var report struct {
				OK      bool                    `json:"ok"`
				Queries []mlwh.SourceQueryCheck `json:"queries"`
			}

			convey.So(runErr, convey.ShouldNotBeNil)
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.OK, convey.ShouldBeFalse)
			convey.So(report.Queries, convey.ShouldHaveLength, 2)
			convey.So(report.Queries[0], convey.ShouldResemble, mlwh.SourceQueryCheck{Name: "study incremental", OK: true})
			convey.So(report.Queries[1].Name, convey.ShouldEqual, "useq_wafer ids")
			convey.So(report.Queries[1].OK, convey.ShouldBeFalse)
			convey.So(report.Queries[1].Error, convey.ShouldContainSubstring, "useq_wafer")
		})
	})

	convey.Convey("Given an MLWH source that accepts connections but never answers, when wa mlwh check-source runs, then it fails within the connect deadline", t, func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = listener.Close() }()

		go holdConnectionsSilently(listener)

		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp("+listener.Addr().String()+")/mlwarehouse")
		t.Setenv("WA_MLWH_PASSWORD", "secret")

		originalTimeout := mlwhCheckSourceConnectTimeout
		defer func() { mlwhCheckSourceConnectTimeout = originalTimeout }()

		mlwhCheckSourceConnectTimeout = 200 * time.Millisecond

		type result struct {
			output string
			err    error
		}

		done := make(chan result, 1)
		started := time.Now()

		go func() {
			output, runErr := executeRootCommandForTest(t, []string{"mlwh", "check-source"})
			done <- result{output: output, err: runErr}
		}()

		select {
		case got := <-done:
			convey.So(time.Since(started), convey.ShouldBeLessThan, 5*time.Second)
			convey.So(got.err, convey.ShouldNotBeNil)
			convey.So(got.output, convey.ShouldContainSubstring, "open mlwh source")
		case <-time.After(5 * time.Second):
			convey.So("wa mlwh check-source still waiting on the silent source after 5s", convey.ShouldBeEmpty)
		}
	})

	convey.Convey("Given WA_MLWH_DSN is unset, when wa mlwh check-source runs, then it exits non-zero naming WA_MLWH_DSN without connecting", t, func() {
		t.Setenv("WA_MLWH_DSN", "")

		originalOpen := openMLWHCheckSource
		defer func() { openMLWHCheckSource = originalOpen }()

		openMLWHCheckSource = func(context.Context, string, string) (*sql.DB, error) {
			return nil, errors.New("should not be called")
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "check-source"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_DSN must be set")
	})
}

// executeMLWHCheckSourceStdout runs wa mlwh check-source with args and returns
// stdout alone, so a JSON report can be parsed without cobra's error line.
func executeMLWHCheckSourceStdout(t *testing.T, args ...string) (string, error) {
	t.Helper()

	stdout := &strings.Builder{}
	command := NewRootCommand()
	command.SetOut(stdout)
	command.SetErr(&strings.Builder{})
	command.SetArgs(append([]string{"mlwh", "check-source"}, args...))

	err := command.Execute()

	return stdout.String(), err
}

// holdConnectionsSilently accepts connections on listener and never writes, so
// a MySQL client waits forever for the server greeting. Connections stay open
// until the listener closes.
func holdConnectionsSilently(listener net.Listener) {
	var conns []net.Conn
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}

		conns = append(conns, conn)
	}
}
