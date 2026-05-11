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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestMLWHSyncCommandRequiresDSN(t *testing.T) {
	convey.Convey("E3.2: Given a missing WA_MLWH_DSN, when wa mlwh sync runs, then the exit code is non-zero and stderr names WA_MLWH_DSN", t, func() {
		t.Setenv("WA_MLWH_DSN", "")

		originalOpen := openMLWHSyncClient
		defer func() { openMLWHSyncClient = originalOpen }()

		openMLWHSyncClient = func(context.Context, mlwh.Config) (mlwhSyncClient, error) {
			return nil, errors.New("should not be called")
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "sync"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_DSN")
	})
}

func TestMLWHCommandsHaveDescriptiveLongHelp(t *testing.T) {
	convey.Convey("Every wa mlwh command and subcommand has a substantive Long help that documents required configuration", t, func() {
		root := newMLWHCommand()

		var visit func(*cobra.Command)
		visit = func(c *cobra.Command) {
			convey.Convey("command "+c.CommandPath(), func() {
				convey.So(strings.TrimSpace(c.Long), convey.ShouldNotBeBlank)
				convey.So(len(c.Long), convey.ShouldBeGreaterThan, 200)
				convey.So(c.Long, convey.ShouldContainSubstring, "WA_MLWH_DSN")
				convey.So(c.Long, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
				convey.So(c.Long, convey.ShouldContainSubstring, "--env")
				convey.So(c.Long, convey.ShouldContainSubstring, "Example")
			})

			for _, child := range c.Commands() {
				visit(child)
			}
		}

		visit(root)
	})
}

func TestMLWHSyncHelpRendersConfigurationDetails(t *testing.T) {
	convey.Convey("wa mlwh sync --help renders documentation about env vars and an example", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "sync", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_DSN")
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_PASSWORD")
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PASSWORD")
		convey.So(output, convey.ShouldContainSubstring, "--env")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh sync")
	})
}

type stubMLWHSyncClient struct {
	reports    []mlwh.SyncReport
	err        error
	seenTables []string
	closed     bool
}

func (c *stubMLWHSyncClient) Sync(_ context.Context, tables ...string) ([]mlwh.SyncReport, error) {
	c.seenTables = append([]string(nil), tables...)

	return c.reports, c.err
}

func (c *stubMLWHSyncClient) Close() error {
	c.closed = true

	return nil
}

func TestMLWHSyncCommandReports(t *testing.T) {
	convey.Convey("E3.1: Given a configured mlwh client whose Sync returns reports, when wa mlwh sync runs, then stdout names each table and its inserted and updated counts", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

		originalOpen := openMLWHSyncClient
		defer func() { openMLWHSyncClient = originalOpen }()

		openMLWHSyncClient = func(context.Context, mlwh.Config) (mlwhSyncClient, error) {
			return &stubMLWHSyncClient{
				reports: []mlwh.SyncReport{
					{Table: "sample", Inserted: 3, Updated: 1, HighWater: time.Date(2026, time.May, 7, 9, 0, 0, 0, time.UTC)},
					{Table: "study", Inserted: 2, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 1, 0, 0, time.UTC)},
					{Table: "iseq_flowcell", Inserted: 4, Updated: 2, HighWater: time.Date(2026, time.May, 7, 9, 2, 0, 0, time.UTC)},
				},
			}, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "sync"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "sample")
		convey.So(output, convey.ShouldContainSubstring, "inserted=3")
		convey.So(output, convey.ShouldContainSubstring, "updated=1")
		convey.So(output, convey.ShouldContainSubstring, "study")
		convey.So(output, convey.ShouldContainSubstring, "inserted=2")
		convey.So(output, convey.ShouldContainSubstring, "iseq_flowcell")
		convey.So(output, convey.ShouldContainSubstring, "updated=2")
	})
}

func TestMLWHSyncCommandFiltersTables(t *testing.T) {
	convey.Convey("E3.3: Given wa mlwh sync --tables sample, when it runs, then only the sample table is synced and the report list has length 1", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

		client := &stubMLWHSyncClient{
			reports: []mlwh.SyncReport{{Table: "sample", Inserted: 1, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 3, 0, 0, time.UTC)}},
		}

		originalOpen := openMLWHSyncClient
		defer func() { openMLWHSyncClient = originalOpen }()

		openMLWHSyncClient = func(context.Context, mlwh.Config) (mlwhSyncClient, error) {
			return client, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "sync", "--tables", "sample"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(client.seenTables, convey.ShouldResemble, []string{"sample"})
		convey.So(output, convey.ShouldContainSubstring, "sample")
		convey.So(output, convey.ShouldNotContainSubstring, "study")
		convey.So(output, convey.ShouldNotContainSubstring, "iseq_flowcell")
	})
}
