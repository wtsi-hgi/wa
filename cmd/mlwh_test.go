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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
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
	reports []mlwh.SyncReport
	err     error
	closed  bool
}

func (c *stubMLWHSyncClient) Sync(_ context.Context) ([]mlwh.SyncReport, error) {
	return c.reports, c.err
}

func (c *stubMLWHSyncClient) Close() error {
	c.closed = true

	return nil
}

func TestMLWHSyncCommandReports(t *testing.T) {
	convey.Convey("B1.4: Given a configured mlwh client whose Sync returns five reports, when wa mlwh sync runs, then stdout contains exactly five success lines", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

		originalOpen := openMLWHSyncClient
		defer func() { openMLWHSyncClient = originalOpen }()

		openMLWHSyncClient = func(context.Context, mlwh.Config) (mlwhSyncClient, error) {
			return &stubMLWHSyncClient{
				reports: []mlwh.SyncReport{
					{Table: "sample", Inserted: 3, Updated: 1, HighWater: time.Date(2026, time.May, 7, 9, 0, 0, 0, time.UTC)},
					{Table: "study", Inserted: 2, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 1, 0, 0, time.UTC)},
					{Table: "iseq_flowcell", Inserted: 4, Updated: 2, HighWater: time.Date(2026, time.May, 7, 9, 2, 0, 0, time.UTC)},
					{Table: "iseq_product_metrics", Inserted: 5, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 3, 0, 0, time.UTC)},
					{Table: "seq_product_irods_locations", Inserted: 6, Updated: 1, HighWater: time.Date(2026, time.May, 7, 9, 4, 0, 0, time.UTC)},
				},
			}, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "sync"})

		convey.So(err, convey.ShouldBeNil)
		lines := strings.Split(output, "\n")
		convey.So(lines, convey.ShouldHaveLength, 5)

		linePattern := regexp.MustCompile(`^(sample|study|iseq_flowcell|iseq_product_metrics|seq_product_irods_locations) inserted=\d+ updated=\d+ high_water=\d{4}-.+Z$`)
		for _, line := range lines {
			convey.So(linePattern.MatchString(line), convey.ShouldBeTrue)
		}
	})
}

func TestMLWHSyncCommandRejectsRemovedTablesFlag(t *testing.T) {
	convey.Convey("B1.5: Given wa mlwh sync --tables sample, when parsing flags, then the command exits non-zero with unknown flag", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

		originalOpen := openMLWHSyncClient
		defer func() { openMLWHSyncClient = originalOpen }()

		openMLWHSyncClient = func(context.Context, mlwh.Config) (mlwhSyncClient, error) {
			return &stubMLWHSyncClient{}, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "sync", "--tables", "sample"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "unknown flag: --tables")
	})
}

func TestMLWHSyncCommandReportsConcurrentCacheLockOnStderrOnly(t *testing.T) {
	convey.Convey("B6.1/B6.2: Given a concurrent sync lock failure, when wa mlwh sync runs, then stderr contains the spec message and stdout stays empty", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

		originalOpen := openMLWHSyncClient
		defer func() { openMLWHSyncClient = originalOpen }()

		openMLWHSyncClient = func(context.Context, mlwh.Config) (mlwhSyncClient, error) {
			return &stubMLWHSyncClient{err: mlwh.ErrSyncAlreadyRunning}, nil
		}

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		command := NewRootCommand()
		command.SetOut(stdout)
		command.SetErr(stderr)
		command.SetArgs([]string{"mlwh", "sync"})

		err := command.Execute()

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.TrimSpace(stdout.String()), convey.ShouldEqual, "")
		convey.So(strings.TrimSpace(stderr.String()), convey.ShouldEqual, mlwh.ErrSyncAlreadyRunning.Error())
	})
}

type liveMLWHSyncClientStub struct {
	finishOrder []mlwh.SyncReport
	successes   []mlwh.SyncReport
	err         error
	writer      io.Writer
}

func (c *liveMLWHSyncClientStub) SetSyncReportWriter(writer io.Writer) {
	c.writer = writer
}

func (c *liveMLWHSyncClientStub) Sync(_ context.Context) ([]mlwh.SyncReport, error) {
	if c.writer != nil {
		for _, report := range c.successes {
			_, _ = fmt.Fprintf(
				c.writer,
				"%s inserted=%d updated=%d high_water=%s\n",
				report.Table,
				report.Inserted,
				report.Updated,
				report.HighWater.UTC().Format("2006-01-02T15:04:05Z"),
			)
		}
	}

	return append([]mlwh.SyncReport(nil), c.finishOrder...), c.err
}

func (c *liveMLWHSyncClientStub) Close() error {
	return nil
}

func TestMLWHSyncCommandEmitsLinesInFinishOrder(t *testing.T) {
	convey.Convey("B1.7: Given a stub that finishes out of lexical order, when wa mlwh sync runs, then stdout line order matches finish order", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

		originalOpen := openMLWHSyncClient
		defer func() { openMLWHSyncClient = originalOpen }()

		finishOrder := []mlwh.SyncReport{
			{Table: "iseq_flowcell", Inserted: 4, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 2, 0, 0, time.UTC)},
			{Table: "sample", Inserted: 3, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 1, 0, 0, time.UTC)},
			{Table: "iseq_product_metrics", Inserted: 5, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 3, 0, 0, time.UTC)},
			{Table: "seq_product_irods_locations", Inserted: 6, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 4, 0, 0, time.UTC)},
			{Table: "study", Inserted: 2, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 5, 0, 0, time.UTC)},
		}

		lexical := append([]mlwh.SyncReport(nil), finishOrder...)
		sort.Slice(lexical, func(i, j int) bool {
			return lexical[i].Table < lexical[j].Table
		})

		openMLWHSyncClient = func(context.Context, mlwh.Config) (mlwhSyncClient, error) {
			return &liveMLWHSyncClientStub{finishOrder: lexical, successes: finishOrder}, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "sync"})

		convey.So(err, convey.ShouldBeNil)
		lines := strings.Split(output, "\n")
		convey.So(lines, convey.ShouldHaveLength, 5)
		convey.So(lines[0], convey.ShouldStartWith, "iseq_flowcell inserted=")
		convey.So(lines[len(lines)-1], convey.ShouldStartWith, "study inserted=")
		convey.So(strings.Join(lines, "\n"), convey.ShouldNotContainSubstring, "iseq_flowcell inserted=4 updated=0 high_water=2026-05-07T09:02:00Z\nlseq")
	})

	convey.Convey("B1.8: Given two failing tables and three successes, when wa mlwh sync runs, then the error mentions both failures and stdout still contains the success lines", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

		originalOpen := openMLWHSyncClient
		defer func() { openMLWHSyncClient = originalOpen }()

		successes := []mlwh.SyncReport{
			{Table: "sample", Inserted: 3, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 1, 0, 0, time.UTC)},
			{Table: "iseq_product_metrics", Inserted: 5, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 3, 0, 0, time.UTC)},
			{Table: "seq_product_irods_locations", Inserted: 6, Updated: 0, HighWater: time.Date(2026, time.May, 7, 9, 4, 0, 0, time.UTC)},
		}

		openMLWHSyncClient = func(context.Context, mlwh.Config) (mlwhSyncClient, error) {
			return &liveMLWHSyncClientStub{
				successes: successes,
				err: errors.Join(
					fmt.Errorf("study: forced study failure"),
					fmt.Errorf("iseq_flowcell: forced iseq_flowcell failure"),
				),
			}, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "sync"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "study")
		convey.So(output, convey.ShouldContainSubstring, "forced study failure")
		convey.So(output, convey.ShouldContainSubstring, "iseq_flowcell")
		convey.So(output, convey.ShouldContainSubstring, "forced iseq_flowcell failure")
		for _, table := range []string{"sample", "iseq_product_metrics", "seq_product_irods_locations"} {
			convey.So(output, convey.ShouldContainSubstring, table+" inserted=")
		}
	})
}
