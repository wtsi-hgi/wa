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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
	"github.com/spf13/cobra"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/mlwh"
)

// mlwhServeG3UnauthenticatedPaths are the new Registry endpoints plus the
// operational plain routes that G3 requires reachable at root paths in the
// default unauthenticated serve mode.
var mlwhServeG3UnauthenticatedPaths = []string{
	"/search/study/malar",
	"/studies/count",
	"/freshness",
	"/health",
	"/openapi.json",
}

const mlwhFeedbackExampleSubmissionForTest = `{"category":"no_endpoint","description":"No endpoint lists sample consent.",
 "user_request":"Which samples have withdrawn consent?",
 "tools_tried":["mlwh_search_samples","mlwh_call_endpoint"],
 "mcp_server_version":"0.4.0","wa_api_version":"1.9.0","transport":"stdio",
 "client_name":"claude-code","client_version":"2.1.0"}`

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
	convey.Convey("Every wa mlwh command and subcommand has substantive Long help with centralized configuration guidance", t, func() {
		root := newMLWHCommand()

		var visit func(*cobra.Command)
		visit = func(c *cobra.Command) {
			convey.Convey("command "+c.CommandPath(), func() {
				convey.So(strings.TrimSpace(c.Long), convey.ShouldNotBeBlank)
				convey.So(len(c.Long), convey.ShouldBeGreaterThan, 200)
				convey.So(c.Long, convey.ShouldContainSubstring, "Example")
				convey.So(c.Long, convey.ShouldNotContainSubstring, "Normal CLI users")
				if c.CommandPath() == "mlwh" || c.CommandPath() == "mlwh sync" || c.CommandPath() == "mlwh serve" {
					convey.So(c.Long, convey.ShouldContainSubstring, "WA_MLWH_DSN")
					convey.So(c.Long, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
					convey.So(c.Long, convey.ShouldContainSubstring, "--env")
				} else {
					convey.So(c.Long, convey.ShouldContainSubstring, "see `wa mlwh -h`")
					convey.So(c.Long, convey.ShouldNotContainSubstring, "before resolving:")
				}
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

func TestMLWHSyncHelpAvoidsStaleTableCountAndList(t *testing.T) {
	convey.Convey("Given wa mlwh --help, then sync has clear short help without a stale table count", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Sync supported MLWH metadata and data into the local cache")
		convey.So(output, convey.ShouldNotContainSubstring, "Sync the five mirrored MLWH tables into the local cache")
		convey.So(output, convey.ShouldNotContainSubstring, "five mirrored MLWH tables")
	})

	convey.Convey("Given wa mlwh sync --help, then long help describes supported data without an incomplete table list", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "sync", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "supported MLWH metadata and data into the local cache")
		convey.So(output, convey.ShouldNotContainSubstring, "study, sample, iseq_flowcell")
		convey.So(output, convey.ShouldNotContainSubstring, "iseq_product_metrics and seq_product_irods_locations")
	})
}

func TestMLWHHelpExposesKCommandsWithoutIrods(t *testing.T) {
	convey.Convey("K acceptance 1: Given wa mlwh --help, then the K commands are listed and the removed irods command is not advertised", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "export")
		convey.So(output, convey.ShouldContainSubstring, "runs")
		convey.So(output, convey.ShouldContainSubstring, "latest")
		convey.So(output, convey.ShouldContainSubstring, "programmes")
		convey.So(output, convey.ShouldNotContainSubstring, "irods")
	})
}

func TestMLWHManifestCommandRemovedG1(t *testing.T) {
	convey.Convey("G1.1: Given the built CLI, when wa mlwh manifest runs, then Cobra reports an unknown command", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "manifest", "7568"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "unknown command")
		convey.So(output, convey.ShouldContainSubstring, "manifest")
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

func TestOpenMLWHServeFeedbackD1(t *testing.T) {
	convey.Convey("D1.1: Given XDG_STATE_HOME set to a temp dir and a blank feedbackDB, then it returns nil store, nil token, nil error, and no default token file exists", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)

		for _, blank := range []string{"", "  \t"} {
			store, token, err := openMLWHServeFeedback(context.Background(), blank, mlwhServeConfig{})

			convey.So(err, convey.ShouldBeNil)
			convey.So(store, convey.ShouldBeNil)
			convey.So(token, convey.ShouldBeNil)
		}

		entries, err := os.ReadDir(stateDir)
		convey.So(err, convey.ShouldBeNil)
		convey.So(entries, convey.ShouldBeEmpty)
	})

	convey.Convey("D1.2: Given plain config and feedbackDB <tmp>/sub/fb.sqlite, then sub/ and the DB are created and $XDG_STATE_HOME/.wa-mlwh-server.token holds the returned token at mode 0600", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)
		dbPath := filepath.Join(t.TempDir(), "sub", "fb.sqlite")

		store, token, err := openMLWHServeFeedback(context.Background(), dbPath, mlwhServeConfig{addr: "127.0.0.1:0"})
		convey.So(err, convey.ShouldBeNil)
		convey.So(store, convey.ShouldNotBeNil)
		closeMLWHFeedbackStoreForTest(t, store)

		dirInfo, err := os.Stat(filepath.Dir(dbPath))
		convey.So(err, convey.ShouldBeNil)
		convey.So(dirInfo.IsDir(), convey.ShouldBeTrue)

		_, err = os.Stat(dbPath)
		convey.So(err, convey.ShouldBeNil)

		added, err := store.Add(context.Background(), mlwh.FeedbackSubmission{Category: mlwh.FeedbackCategoryOther, Description: "d"}, "")
		convey.So(err, convey.ShouldBeNil)
		convey.So(added.ID, convey.ShouldEqual, 1)

		tokenPath := filepath.Join(stateDir, mlwhServeDefaultServerTokenBasename)
		convey.So(mlwhServeDefaultServerTokenBasename, convey.ShouldEqual, ".wa-mlwh-server.token")
		info, err := os.Stat(tokenPath)
		convey.So(err, convey.ShouldBeNil)
		convey.So(info.Mode(), convey.ShouldEqual, os.FileMode(0o600))

		contents, err := os.ReadFile(tokenPath)
		convey.So(err, convey.ShouldBeNil)
		convey.So(string(token), convey.ShouldEqual, string(contents))
		convey.So(len(token), convey.ShouldEqual, 43)
	})

	convey.Convey("D1.3: Given the default token file already exists, when called again, then the same token is returned and the file is unchanged", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)
		dbPath := filepath.Join(t.TempDir(), "fb.sqlite")

		firstStore, firstToken, err := openMLWHServeFeedback(context.Background(), dbPath, mlwhServeConfig{})
		convey.So(err, convey.ShouldBeNil)
		convey.So(firstStore.Close(), convey.ShouldBeNil)

		tokenPath := filepath.Join(stateDir, ".wa-mlwh-server.token")
		before, err := os.Stat(tokenPath)
		convey.So(err, convey.ShouldBeNil)

		secondStore, secondToken, err := openMLWHServeFeedback(context.Background(), dbPath, mlwhServeConfig{})
		convey.So(err, convey.ShouldBeNil)
		closeMLWHFeedbackStoreForTest(t, secondStore)

		convey.So(string(secondToken), convey.ShouldEqual, string(firstToken))

		after, err := os.Stat(tokenPath)
		convey.So(err, convey.ShouldBeNil)
		convey.So(after.ModTime(), convey.ShouldEqual, before.ModTime())
		convey.So(after.Mode(), convey.ShouldEqual, os.FileMode(0o600))

		contents, err := os.ReadFile(tokenPath)
		convey.So(err, convey.ShouldBeNil)
		convey.So(string(contents), convey.ShouldEqual, string(firstToken))
	})

	convey.Convey("D1.4: Given secured config whose server token file already exists at mode 0600, then that token is returned, the file is unchanged, and no default token file is created", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)
		existing := strings.Repeat("a", 43)

		cases := []struct {
			name      string
			tokenPath string
		}{
			{name: "basename", tokenPath: filepath.Join(stateDir, ".my.token")},
			{name: "absolute path", tokenPath: filepath.Join(t.TempDir(), "abs.token")},
		}

		for _, tc := range cases {
			convey.So(os.WriteFile(tc.tokenPath, []byte(existing), 0o600), convey.ShouldBeNil)
			convey.So(os.Chmod(tc.tokenPath, 0o600), convey.ShouldBeNil)
			before, err := os.Stat(tc.tokenPath)
			convey.So(err, convey.ShouldBeNil)

			serverToken := tc.tokenPath
			if tc.name == "basename" {
				serverToken = filepath.Base(tc.tokenPath)
			}

			store, token, err := openMLWHServeFeedback(
				context.Background(),
				filepath.Join(t.TempDir(), "fb.sqlite"),
				mlwhServeConfig{cert: "cert.pem", key: "key.pem", serverToken: serverToken, secured: true},
			)
			convey.So(err, convey.ShouldBeNil)
			closeMLWHFeedbackStoreForTest(t, store)
			convey.So(string(token), convey.ShouldEqual, existing)

			after, err := os.Stat(tc.tokenPath)
			convey.So(err, convey.ShouldBeNil)
			convey.So(after.ModTime(), convey.ShouldEqual, before.ModTime())

			contents, err := os.ReadFile(tc.tokenPath)
			convey.So(err, convey.ShouldBeNil)
			convey.So(string(contents), convey.ShouldEqual, existing)
		}

		_, err := os.Stat(filepath.Join(stateDir, ".wa-mlwh-server.token"))
		convey.So(errors.Is(err, os.ErrNotExist), convey.ShouldBeTrue)
	})

	convey.Convey("D1.5: Given a MySQL-looking feedbackDB, then the error mentions SQLite file path and no token file is created", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)

		store, token, err := openMLWHServeFeedback(context.Background(), "user@tcp(db:3306)/fb", mlwhServeConfig{})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "SQLite file path")
		convey.So(store, convey.ShouldBeNil)
		convey.So(token, convey.ShouldBeNil)

		entries, readErr := os.ReadDir(stateDir)
		convey.So(readErr, convey.ShouldBeNil)
		convey.So(entries, convey.ShouldBeEmpty)
	})

	convey.Convey("Given :memory: or a file: URI as feedbackDB, then the error mentions SQLite file path and no token file is created", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)
		uriPath := "file:" + filepath.Join(t.TempDir(), "fb.sqlite")

		for _, value := range []string{":memory:", " :memory: ", uriPath, "file::memory:?cache=shared"} {
			store, token, err := openMLWHServeFeedback(context.Background(), value, mlwhServeConfig{})

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldContainSubstring, "SQLite file path")
			convey.So(store, convey.ShouldBeNil)
			convey.So(token, convey.ShouldBeNil)
		}

		entries, readErr := os.ReadDir(stateDir)
		convey.So(readErr, convey.ShouldBeNil)
		convey.So(entries, convey.ShouldBeEmpty)
	})
}

func closeMLWHFeedbackStoreForTest(t *testing.T, store *mlwh.FeedbackStore) {
	t.Helper()

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close feedback store: %v", err)
		}
	})
}

// prepareMLWHServeMalariaCacheForTest builds a synced SQLite cache seeded with a
// study whose name contains "malar", so GET /search/study/malar matches at least
// one row over the served cache. It is used by the G3 reachability tests, which
// require malar to match seeded studies.
func prepareMLWHServeMalariaCacheForTest(t *testing.T) string {
	t.Helper()

	cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
	cache, err := mlwh.OpenCache(context.Background(), mlwh.CacheConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("open mlwh cache: %v", err)
	}
	defer func() {
		if err = cache.Close(); err != nil {
			t.Fatalf("close mlwh cache: %v", err)
		}
	}()

	seedMLWHServeMalariaStudyForTest(t, cache.DB())

	return cachePath
}

func seedMLWHServeMalariaStudyForTest(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1,
		"SQSCP",
		"6568",
		"study-uuid-6568",
		"Malaria genomics survey",
		"EGAS00001006568",
		"Study title 6568",
		"Faculty sponsor 6568",
		"active",
		"strategy",
		"group",
		"programme",
		"GRCh38",
		true,
		"study-type",
		false,
		false,
		"public",
		"EGAD0001",
		"EGAP0001",
		"immediate",
		"2026-05-11T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert study_mirror: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, ?, ?)`,
		"study",
		"2026-05-11T00:00:00Z",
		"2026-05-11T00:00:00Z",
		nil,
		0,
	)
	if err != nil {
		t.Fatalf("insert sync_state: %v", err)
	}
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

func startMLWHNeverSyncedServerForTest(t *testing.T) string {
	t.Helper()

	cachePath := prepareMLWHServeCacheForTest(t, false)
	client, err := mlwh.OpenCacheOnly(context.Background(), mlwh.CacheConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("open never-synced MLWH cache: %v", err)
	}
	t.Cleanup(func() {
		if err = client.Close(); err != nil {
			t.Fatalf("close never-synced MLWH cache: %v", err)
		}
	})

	router := gin.New()
	configureMLWHServeRouter(router)
	mlwh.NewServer(client).RegisterRoutes(router, nil)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server.URL
}

func prepareMLWHServeCacheForTest(t *testing.T, synced bool) string {
	t.Helper()

	cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
	cache, err := mlwh.OpenCache(context.Background(), mlwh.CacheConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("open mlwh cache: %v", err)
	}
	defer func() {
		if err = cache.Close(); err != nil {
			t.Fatalf("close mlwh cache: %v", err)
		}
	}()

	if synced {
		seedMLWHServeStudyForTest(t, cache.DB())
	}

	return cachePath
}

func seedMLWHServeStudyForTest(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1,
		"SQSCP",
		"6568",
		"study-uuid-6568",
		"Study 6568",
		"EGAS00001006568",
		"Study title 6568",
		"Faculty sponsor 6568",
		"active",
		"strategy",
		"group",
		"programme",
		"GRCh38",
		true,
		"study-type",
		false,
		false,
		"public",
		"EGAD0001",
		"EGAP0001",
		"immediate",
		"2026-05-11T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert study_mirror: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, ?, ?)`,
		"study",
		"2026-05-11T00:00:00Z",
		"2026-05-11T00:00:00Z",
		nil,
		0,
	)
	if err != nil {
		t.Fatalf("insert sync_state: %v", err)
	}
}

func freeLocalAddrForTest(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}

	addr := listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatalf("close free port listener: %v", err)
	}

	return addr
}

func waitForMLWHServeHealthForTest(baseURL string, done <-chan error) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			return fmt.Errorf("command exited early: %w", err)
		default:
		}

		response, err := client.Get(baseURL + "/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	return errors.New("GET /health did not return 200 within 5s")
}

type fakeMLWHServeAuthEnableCall struct {
	certFile      string
	keyFile       string
	tokenBasename string
}

type fakeMLWHServeAuthStartCall struct {
	kind     string
	addr     string
	certFile string
	keyFile  string
}

type fakeMLWHServeAuthServer struct {
	router      *gin.Engine
	auth        *gin.RouterGroup
	enableCalls []fakeMLWHServeAuthEnableCall
	startCalls  []fakeMLWHServeAuthStartCall
	onStart     func(*fakeMLWHServeAuthServer) error
	onEnable    func() error
}

func newFakeMLWHServeAuthServer() *fakeMLWHServeAuthServer {
	gin.SetMode(gin.TestMode)

	return &fakeMLWHServeAuthServer{router: gin.New()}
}

func (f *fakeMLWHServeAuthServer) Router() *gin.Engine {
	return f.router
}

func (f *fakeMLWHServeAuthServer) AuthRouter() *gin.RouterGroup {
	return f.auth
}

func (f *fakeMLWHServeAuthServer) EnableAuthWithServerToken(certFile, keyFile, tokenBasename string, _ gas.AuthCallback) error {
	f.enableCalls = append(f.enableCalls, fakeMLWHServeAuthEnableCall{
		certFile:      certFile,
		keyFile:       keyFile,
		tokenBasename: tokenBasename,
	})
	f.auth = f.router.Group(gas.EndPointAuth)
	f.auth.Use(func(c *gin.Context) {
		if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") {
			c.AbortWithStatus(http.StatusUnauthorized)

			return
		}

		c.Next()
	})

	if f.onEnable != nil {
		return f.onEnable()
	}

	return nil
}

func (f *fakeMLWHServeAuthServer) StartHTTP(_ context.Context, addr string) error {
	f.startCalls = append(f.startCalls, fakeMLWHServeAuthStartCall{kind: "http", addr: addr})

	if f.onStart != nil {
		return f.onStart(f)
	}

	return nil
}

func (f *fakeMLWHServeAuthServer) Start(addr, certFile, keyFile string) error {
	f.startCalls = append(f.startCalls, fakeMLWHServeAuthStartCall{
		kind:     "tls",
		addr:     addr,
		certFile: certFile,
		keyFile:  keyFile,
	})

	if f.onStart != nil {
		return f.onStart(f)
	}

	return nil
}

func (f *fakeMLWHServeAuthServer) Stop() {}

func TestMLWHServeColdCacheReturnsNeverSynced(t *testing.T) {
	convey.Convey("E4.1: Given wa mlwh serve with WA_MLWH_CACHE_PATH set to a never-synced cache, when GET /studies is requested, then status is 503 with code cache_never_synced", t, func() {
		cachePath := prepareMLWHServeCacheForTest(t, false)
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)
		fakeAuth := newFakeMLWHServeAuthServer()
		fakeAuth.onStart = func(server *fakeMLWHServeAuthServer) error {
			response := performMLWHServeRequestForTest(server.router, http.MethodGet, "/studies")
			convey.So(response.Code, convey.ShouldEqual, http.StatusServiceUnavailable)
			convey.So(mlwhServeErrorCodeForTest(t, response), convey.ShouldEqual, "cache_never_synced")
			convey.So(server.startCalls, convey.ShouldHaveLength, 1)
			convey.So(server.startCalls[0].kind, convey.ShouldEqual, "http")

			return nil
		}
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve", "--port", "0"})

		convey.So(err, convey.ShouldBeNil)
	})
}

func performMLWHServeRequestForTest(handler http.Handler, method, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func mlwhServeErrorCodeForTest(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()

	return mlwhServeEnvelopeCodeForTest(t, response.Body.Bytes())
}

func mlwhServeEnvelopeCodeForTest(t *testing.T, body []byte) string {
	t.Helper()

	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode mlwh error envelope %q: %v", body, err)
	}

	return payload.Code
}

func TestMLWHServeWarmCacheUnauthenticatedByDefault(t *testing.T) {
	convey.Convey("E4.2: Given a synced cache, when GET /studies is requested with no auth configured, then status is 200", t, func() {
		cachePath := prepareMLWHServeCacheForTest(t, true)
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)
		fakeAuth := newFakeMLWHServeAuthServer()
		fakeAuth.onStart = func(server *fakeMLWHServeAuthServer) error {
			response := performMLWHServeRequestForTest(server.router, http.MethodGet, "/studies")
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(server.enableCalls, convey.ShouldHaveLength, 0)
			convey.So(server.startCalls, convey.ShouldHaveLength, 1)
			convey.So(server.startCalls[0].kind, convey.ShouldEqual, "http")

			return nil
		}
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve", "--port", "0"})

		convey.So(err, convey.ShouldBeNil)
	})
}

func TestMLWHServeSecuredModeRequiresBearerToken(t *testing.T) {
	convey.Convey("E4.3: Given wa mlwh serve with a server token and cert configured, when an endpoint is requested without a Bearer token, then status is 401", t, func() {
		cachePath := prepareMLWHServeCacheForTest(t, true)
		fakeAuth := newFakeMLWHServeAuthServer()
		fakeAuth.onStart = func(server *fakeMLWHServeAuthServer) error {
			response := performMLWHServeRequestForTest(server.router, http.MethodGet, gas.EndPointAuth+"/studies")
			convey.So(response.Code, convey.ShouldEqual, http.StatusUnauthorized)
			convey.So(server.enableCalls, convey.ShouldHaveLength, 1)
			convey.So(server.enableCalls[0].certFile, convey.ShouldEqual, "cert.pem")
			convey.So(server.enableCalls[0].keyFile, convey.ShouldEqual, "key.pem")
			convey.So(server.enableCalls[0].tokenBasename, convey.ShouldEqual, "mlwh-server.token")
			convey.So(server.startCalls, convey.ShouldHaveLength, 1)
			convey.So(server.startCalls[0].kind, convey.ShouldEqual, "tls")

			return nil
		}
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{
			"mlwh", "serve",
			"--port", "0",
			"--mlwh-cache", cachePath,
			"--cert", "cert.pem",
			"--key", "key.pem",
			"--server-token", "mlwh-server.token",
		})

		convey.So(err, convey.ShouldBeNil)
	})
}

func TestMLWHServeUnauthenticatedReachabilityG3(t *testing.T) {
	convey.Convey("G3.1: Given wa mlwh serve over a synced cache with no auth configured, when search, counts, freshness, health, and openapi are each requested, then all return 200 unauthenticated", t, func() {
		cachePath := prepareMLWHServeMalariaCacheForTest(t)
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)
		fakeAuth := newFakeMLWHServeAuthServer()
		fakeAuth.onStart = func(server *fakeMLWHServeAuthServer) error {
			for _, path := range mlwhServeG3UnauthenticatedPaths {
				response := performMLWHServeRequestForTest(server.router, http.MethodGet, path)
				convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			}

			convey.So(server.enableCalls, convey.ShouldHaveLength, 0)
			convey.So(server.startCalls, convey.ShouldHaveLength, 1)
			convey.So(server.startCalls[0].kind, convey.ShouldEqual, "http")

			return nil
		}
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve", "--port", "0"})

		convey.So(err, convey.ShouldBeNil)
	})
}

func TestMLWHServeSecuredModeUnauthorizedForNewEndpointsG3(t *testing.T) {
	convey.Convey("G3.2: Given wa mlwh serve secured with cert, key, and server token, when the new endpoints are requested without a Bearer token, then each returns 401 behind the auth group", t, func() {
		cachePath := prepareMLWHServeMalariaCacheForTest(t)
		fakeAuth := newFakeMLWHServeAuthServer()
		fakeAuth.onStart = func(server *fakeMLWHServeAuthServer) error {
			for _, path := range []string{"/search/study/malar", "/studies/count", "/freshness"} {
				response := performMLWHServeRequestForTest(server.router, http.MethodGet, gas.EndPointAuth+path)
				convey.So(response.Code, convey.ShouldEqual, http.StatusUnauthorized)
			}

			// /health and /openapi.json stay plain routes on the public router,
			// so they remain reachable unauthenticated even in secured mode.
			for _, path := range []string{"/health", "/openapi.json"} {
				response := performMLWHServeRequestForTest(server.router, http.MethodGet, path)
				convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			}

			convey.So(server.enableCalls, convey.ShouldHaveLength, 1)
			convey.So(server.startCalls, convey.ShouldHaveLength, 1)
			convey.So(server.startCalls[0].kind, convey.ShouldEqual, "tls")

			return nil
		}
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{
			"mlwh", "serve",
			"--port", "0",
			"--mlwh-cache", cachePath,
			"--cert", "cert.pem",
			"--key", "key.pem",
			"--server-token", "mlwh-server.token",
		})

		convey.So(err, convey.ShouldBeNil)
	})
}

func TestMLWHServeFreshnessNeverSyncedReturns200G3(t *testing.T) {
	convey.Convey("G3.3: Given wa mlwh serve over a never-synced cache, when GET /freshness is requested, then status is 200 and not 503 (freshness degrades gracefully)", t, func() {
		cachePath := prepareMLWHServeCacheForTest(t, false)
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)
		fakeAuth := newFakeMLWHServeAuthServer()
		fakeAuth.onStart = func(server *fakeMLWHServeAuthServer) error {
			response := performMLWHServeRequestForTest(server.router, http.MethodGet, "/freshness")
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(response.Code, convey.ShouldNotEqual, http.StatusServiceUnavailable)

			// The companion plain routes also stay reachable on the never-synced
			// cache, since they never consult the queryer.
			for _, path := range []string{"/health", "/openapi.json"} {
				probe := performMLWHServeRequestForTest(server.router, http.MethodGet, path)
				convey.So(probe.Code, convey.ShouldEqual, http.StatusOK)
			}

			convey.So(server.startCalls, convey.ShouldHaveLength, 1)
			convey.So(server.startCalls[0].kind, convey.ShouldEqual, "http")

			return nil
		}
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve", "--port", "0"})

		convey.So(err, convey.ShouldBeNil)
	})
}

func TestMLWHServeRequiresCacheConfiguration(t *testing.T) {
	convey.Convey("E4.4: Given wa mlwh serve with no WA_MLWH_CACHE_PATH and no --mlwh-cache, then it errors naming the missing cache configuration", t, func() {
		t.Setenv("WA_MLWH_CACHE_PATH", "")
		fakeAuth := newFakeMLWHServeAuthServer()
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve", "--port", "0"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
		convey.So(err.Error(), convey.ShouldContainSubstring, "--mlwh-cache")
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 0)
	})
}

func TestMLWHServeScenarioBindDefaults(t *testing.T) {
	convey.Convey("Given development MLWH bind envs and a public server URL, mlwh serve binds the local host and port", t, func() {
		cachePath := prepareMLWHServeCacheForTest(t, true)
		fakeAuth := newFakeMLWHServeAuthServer()
		installFakeMLWHServeAuthServer(t, fakeAuth)
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_SEQMETA_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_SEQMETA_PORT", "3673")
		t.Setenv("WA_MLWH_SERVER_URL", "https://dev-host.example.org:3673")
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.startCalls[0].kind, convey.ShouldEqual, "http")
		convey.So(fakeAuth.startCalls[0].addr, convey.ShouldEqual, "0.0.0.0:3673")
	})

	convey.Convey("Given production MLWH bind host and port envs, mlwh serve uses the production bind address", t, func() {
		cachePath := prepareMLWHServeCacheForTest(t, true)
		fakeAuth := newFakeMLWHServeAuthServer()
		installFakeMLWHServeAuthServer(t, fakeAuth)
		t.Setenv("WA_ENV", "production")
		t.Setenv("WA_PROD_SEQMETA_HOST", "0.0.0.0")
		t.Setenv("WA_PROD_SEQMETA_PORT", "8091")
		t.Setenv("WA_MLWH_SERVER_URL", "https://prod-host.example.org:8091")
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.startCalls[0].kind, convey.ShouldEqual, "http")
		convey.So(fakeAuth.startCalls[0].addr, convey.ShouldEqual, "0.0.0.0:8091")
	})

	convey.Convey("Given a public MLWH server URL without an active scenario, mlwh serve ignores it and uses the server port fallback", t, func() {
		cachePath := prepareMLWHServeCacheForTest(t, true)
		fakeAuth := newFakeMLWHServeAuthServer()
		installFakeMLWHServeAuthServer(t, fakeAuth)
		t.Setenv("WA_ENV", "")
		t.Setenv("WA_MLWH_SERVER_PORT", "9000")
		t.Setenv("WA_MLWH_SERVER_URL", "https://public.example.org:3673")
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.startCalls[0].kind, convey.ShouldEqual, "http")
		convey.So(fakeAuth.startCalls[0].addr, convey.ShouldEqual, "127.0.0.1:9000")
	})
}

func TestMLWHServeStartsOnAnyBackendWithoutFlavorRefusal(t *testing.T) {
	convey.Convey("Given any supported cache backend, when wa mlwh serve runs, then it registers routes and binds a listener with no backend-flavor or version refusal", t, func() {
		cachePath := prepareMLWHServeCacheForTest(t, true)
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)
		fakeAuth := newFakeMLWHServeAuthServer()
		fakeAuth.onStart = func(server *fakeMLWHServeAuthServer) error {
			response := performMLWHServeRequestForTest(server.router, http.MethodGet, "/studies")
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(server.startCalls, convey.ShouldHaveLength, 1)
			convey.So(server.startCalls[0].kind, convey.ShouldEqual, "http")

			return nil
		}
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve", "--port", "0"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
	})
}

func TestMLWHServeSecuredFeedbackUsesServerTokenD1(t *testing.T) {
	convey.Convey("Given secured serve with --feedback-db, then submit sits behind the auth group and admin routes accept the --server-token file written by auth setup", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)
		cachePath := prepareMLWHServeCacheForTest(t, true)
		dbPath := filepath.Join(t.TempDir(), "fb.sqlite")
		tokenPath := filepath.Join(stateDir, "mlwh-server.token")

		fakeAuth := newFakeMLWHServeAuthServer()
		fakeAuth.onEnable = func() error {
			_, err := gas.GenerateAndStoreTokenForSelfClient(tokenPath)

			return err
		}
		fakeAuth.onStart = func(server *fakeMLWHServeAuthServer) error {
			token, err := os.ReadFile(tokenPath)
			convey.So(err, convey.ShouldBeNil)

			submit := performMLWHServeJSONRequestForTest(server.router, http.MethodPost, gas.EndPointAuth+"/feedback", mlwhFeedbackExampleSubmissionForTest, "Bearer jwt")
			convey.So(submit.Code, convey.ShouldEqual, http.StatusCreated)

			rootSubmit := performMLWHServeJSONRequestForTest(server.router, http.MethodPost, "/feedback", mlwhFeedbackExampleSubmissionForTest, "")
			convey.So(rootSubmit.Code, convey.ShouldEqual, http.StatusNotFound)

			list := performMLWHServeJSONRequestForTest(server.router, http.MethodGet, "/feedback", "", "Bearer "+string(token))
			convey.So(list.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(list.Body.String(), convey.ShouldContainSubstring, "No endpoint lists sample consent.")

			return nil
		}
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{
			"mlwh", "serve",
			"--port", "0",
			"--mlwh-cache", cachePath,
			"--cert", "cert.pem",
			"--key", "key.pem",
			"--server-token", "mlwh-server.token",
			"--feedback-db", dbPath,
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)

		_, err = os.Stat(filepath.Join(stateDir, ".wa-mlwh-server.token"))
		convey.So(errors.Is(err, os.ErrNotExist), convey.ShouldBeTrue)
	})

	convey.Convey("Given secured serve with --feedback-db whose auth setup fails, then the command errors and creates neither the feedback DB nor a token file", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)
		cachePath := prepareMLWHServeCacheForTest(t, true)
		dbDir := filepath.Join(t.TempDir(), "sub")

		fakeAuth := newFakeMLWHServeAuthServer()
		fakeAuth.onEnable = func() error {
			return errors.New("auth setup failed")
		}
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{
			"mlwh", "serve",
			"--port", "0",
			"--mlwh-cache", cachePath,
			"--cert", "cert.pem",
			"--key", "key.pem",
			"--server-token", "mlwh-server.token",
			"--feedback-db", filepath.Join(dbDir, "fb.sqlite"),
		})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "auth setup failed")
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 0)

		_, err = os.Stat(dbDir)
		convey.So(errors.Is(err, os.ErrNotExist), convey.ShouldBeTrue)

		entries, err := os.ReadDir(stateDir)
		convey.So(err, convey.ShouldBeNil)
		convey.So(entries, convey.ShouldBeEmpty)
	})
}

func performMLWHServeJSONRequestForTest(handler http.Handler, method, target, body, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func installFakeMLWHServeAuthServer(t *testing.T, fake *fakeMLWHServeAuthServer) {
	t.Helper()

	// A developer's exported feedback path must not make these tests open a
	// store or write a token file under the real XDG_STATE_HOME.
	t.Setenv("WA_MLWH_FEEDBACK_PATH", "")

	originalNewAuthServer := mlwhServeNewAuthServer
	mlwhServeNewAuthServer = func(io.Writer) mlwhServeAuthServer {
		return fake
	}
	t.Cleanup(func() {
		mlwhServeNewAuthServer = originalNewAuthServer
	})
}

type mlwhServeHTTPResponseForTest struct {
	status int
	body   []byte
}

func doMLWHServeHTTPRequestForTest(t *testing.T, method, target, body, authorization string) mlwhServeHTTPResponseForTest {
	t.Helper()

	request, err := http.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	return mlwhServeHTTPResponseForTest{status: response.StatusCode, body: responseBody}
}

func TestMLWHServeFeedbackEndToEndD1(t *testing.T) {
	convey.Convey("D1.8: Given --feedback-db, when the example is submitted over real HTTP, then it is stored, listed with the admin token, rejected without it, and the store is closed on shutdown", t, func() {
		stateDir := t.TempDir()
		dbPath := filepath.Join(stateDir, "fb.sqlite")
		baseURL, stop := startMLWHServeEndToEndForTest(t, stateDir, "--feedback-db", dbPath)

		submit := doMLWHServeHTTPRequestForTest(t, http.MethodPost, baseURL+"/feedback", mlwhFeedbackExampleSubmissionForTest, "")
		convey.So(submit.status, convey.ShouldEqual, http.StatusCreated)

		var receipt map[string]any
		convey.So(json.Unmarshal(submit.body, &receipt), convey.ShouldBeNil)
		convey.So(receipt, convey.ShouldHaveLength, 2)
		convey.So(receipt["id"], convey.ShouldEqual, float64(1))
		createdAt, ok := receipt["created_at"].(string)
		convey.So(ok, convey.ShouldBeTrue)
		parsed, err := time.Parse(time.RFC3339, createdAt)
		convey.So(err, convey.ShouldBeNil)
		convey.So(parsed.Location(), convey.ShouldEqual, time.UTC)
		convey.So(createdAt, convey.ShouldEndWith, "Z")

		token, err := os.ReadFile(filepath.Join(stateDir, ".wa-mlwh-server.token"))
		convey.So(err, convey.ShouldBeNil)

		list := doMLWHServeHTTPRequestForTest(t, http.MethodGet, baseURL+"/feedback", "", "Bearer "+string(token))
		convey.So(list.status, convey.ShouldEqual, http.StatusOK)

		var page mlwh.Page[mlwh.FeedbackReport]
		convey.So(json.Unmarshal(list.body, &page), convey.ShouldBeNil)
		convey.So(page.Items, convey.ShouldHaveLength, 1)
		convey.So(page.Items[0].ID, convey.ShouldEqual, 1)
		convey.So(page.Items[0].Description, convey.ShouldEqual, "No endpoint lists sample consent.")

		unauthorized := doMLWHServeHTTPRequestForTest(t, http.MethodGet, baseURL+"/feedback", "", "")
		convey.So(unauthorized.status, convey.ShouldEqual, http.StatusUnauthorized)
		convey.So(mlwhServeEnvelopeCodeForTest(t, unauthorized.body), convey.ShouldEqual, "unauthorized")

		convey.So(stop(), convey.ShouldBeNil)

		_, err = os.Stat(dbPath + "-wal")
		convey.So(errors.Is(err, os.ErrNotExist), convey.ShouldBeTrue)

		reopened, err := mlwh.OpenFeedbackStore(context.Background(), dbPath)
		convey.So(err, convey.ShouldBeNil)
		closeMLWHFeedbackStoreForTest(t, reopened)

		stored, err := reopened.List(context.Background(), mlwh.FeedbackFilter{}, 10, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(stored.Items, convey.ShouldHaveLength, 1)
		convey.So(stored.Items[0].ID, convey.ShouldEqual, 1)
	})

	convey.Convey("D1.9: Given no --feedback-db, when the example is submitted over real HTTP, then 503 feedback_disabled and no token file is created", t, func() {
		stateDir := t.TempDir()
		baseURL, stop := startMLWHServeEndToEndForTest(t, stateDir)

		submit := doMLWHServeHTTPRequestForTest(t, http.MethodPost, baseURL+"/feedback", mlwhFeedbackExampleSubmissionForTest, "")
		convey.So(submit.status, convey.ShouldEqual, http.StatusServiceUnavailable)
		convey.So(mlwhServeEnvelopeCodeForTest(t, submit.body), convey.ShouldEqual, "feedback_disabled")

		convey.So(stop(), convey.ShouldBeNil)

		_, err := os.Stat(filepath.Join(stateDir, ".wa-mlwh-server.token"))
		convey.So(errors.Is(err, os.ErrNotExist), convey.ShouldBeTrue)
	})
}

// startMLWHServeEndToEndForTest runs the real wa mlwh serve command on a free
// local port until the returned stop func cancels it. stop returns the
// command's error, or a timeout error if it does not return within 10s.
func startMLWHServeEndToEndForTest(t *testing.T, stateDir string, extraArgs ...string) (string, func() error) {
	t.Helper()

	t.Setenv("XDG_STATE_HOME", stateDir)
	t.Setenv("WA_MLWH_SERVER_TOKEN", "")
	t.Setenv("WA_MLWH_SERVER_CERT", "")
	t.Setenv("WA_MLWH_SERVER_KEY", "")
	t.Setenv("WA_MLWH_FEEDBACK_PATH", "")

	cachePath := prepareMLWHServeCacheForTest(t, true)
	addr := freeLocalAddrForTest(t)

	ctx, cancel := context.WithCancel(context.Background())
	command := NewRootCommand()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs(append([]string{"mlwh", "serve", "--url", addr, "--mlwh-cache", cachePath}, extraArgs...))

	done := make(chan error, 1)
	go func() {
		done <- command.ExecuteContext(ctx)
	}()

	stopped := false
	stop := func() error {
		if stopped {
			return nil
		}
		stopped = true
		cancel()

		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			return errors.New("mlwh serve did not return within 10s of cancellation")
		}
	}
	t.Cleanup(func() { _ = stop() })

	baseURL := "http://" + addr
	if err := waitForMLWHServeHealthForTest(baseURL, done); err != nil {
		t.Fatalf("mlwh serve not healthy: %v", err)
	}

	return baseURL, stop
}

func TestMLWHKCommandsDegradeGracefullyOnNeverSyncedCache(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{
			name: "export irods created-desc window",
			args: []string{"mlwh", "export", "irods", "study", "5901", "--sort", "created-desc",
				"--since", "2026-01-01T00:00:00Z", "--until", "2026-02-01T00:00:00Z"},
		},
		{name: "export parent-scoped runs", args: []string{"mlwh", "export", "runs", "study", "5901"}},
		{name: "export users by role", args: []string{"mlwh", "export", "users", "study", "5901", "--role", "owner,manager,follower"}},
		{name: "export sample crams", args: []string{"mlwh", "export", "sample-crams", "study", "5901"}},
		{name: "latest data", args: []string{"mlwh", "latest", "5901", "--file-type", "cram"}},
		{
			name: "search sample filters",
			args: []string{"mlwh", "search", "malaria", "--type", "sample", "--words",
				"--library-type", "Chromium", "--organism", "Homo sapiens", "--qc", "pass", "--deliverables-only"},
		},
		{name: "flat runs", args: []string{"mlwh", "runs", "--platform", "PacBio", "--since", "2026-01-01", "--until", "2026-02-01"}},
		{
			name: "monthly programme runs",
			args: []string{"mlwh", "runs", "--monthly", "--group-by", "programme",
				"--platform", "PacBio", "--since", "2026-01-01", "--until", "2026-02-01"},
		},
		{name: "studies by programme", args: []string{"mlwh", "studies", "--programme", "Human Genetics"}},
		{name: "programmes", args: []string{"mlwh", "programmes"}},
		{name: "info study", args: []string{"mlwh", "info", "5901", "--type", "study"}},
	}

	convey.Convey("K acceptance 2: Given a never-synced local cache, every exposed K command renders a clean message and exits 0", t, func() {
		cachePath := prepareMLWHServeCacheForTest(t, false)
		for _, tc := range cases {
			convey.Convey(tc.name, func() {
				configureMLWHNeverSyncedCommandEnvForTest(t, cachePath)

				output, err := executeRootCommandForTest(t, tc.args)

				convey.So(err, convey.ShouldBeNil)
				convey.So(output, convey.ShouldContainSubstring, mlwhCacheUnavailableMessage)
				convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
				convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
			})
		}
	})

	convey.Convey("K acceptance 2: Given a never-synced cache via --server, every exposed K command renders a clean message and exits 0", t, func() {
		serverURL := startMLWHNeverSyncedServerForTest(t)
		for _, tc := range cases {
			convey.Convey(tc.name, func() {
				configureMLWHNeverSyncedCommandEnvForTest(t, "")
				args := append([]string{}, tc.args...)
				args = append(args, "--server", serverURL)

				output, err := executeRootCommandForTest(t, args)

				convey.So(err, convey.ShouldBeNil)
				convey.So(output, convey.ShouldContainSubstring, mlwhCacheUnavailableMessage)
				convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
				convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
			})
		}
	})
}

func configureMLWHNeverSyncedCommandEnvForTest(t *testing.T, cachePath string) {
	t.Helper()

	t.Setenv("WA_MLWH_DSN", "")
	t.Setenv("WA_MLWH_PASSWORD", "")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_MLWH_CACHE_PATH", cachePath)
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")
}

func TestMLWHServeDoesNotSyncOrExposeSyncInterval(t *testing.T) {
	convey.Convey("E4.5: Given the serve command source, when audited, then it never calls client.Sync and never exposes --mlwh-sync-interval", t, func() {
		source, err := os.ReadFile("mlwh.go")
		convey.So(err, convey.ShouldBeNil)

		serveSource := mlwhServeCommandSourceForTest(string(source))
		convey.So(serveSource, convey.ShouldContainSubstring, "newMLWHServeCommand")
		convey.So(serveSource, convey.ShouldNotContainSubstring, ".Sync(")

		command := newMLWHServeCommand()
		convey.So(command.Flags().Lookup("mlwh-sync-interval"), convey.ShouldBeNil)
		convey.So(command.Flags().Lookup("mlwh-cache"), convey.ShouldNotBeNil)
	})
}

func mlwhServeCommandSourceForTest(source string) string {
	start := strings.Index(source, "func newMLWHServeCommand")
	if start == -1 {
		return ""
	}

	remaining := source[start+len("func "):]
	end := strings.Index(remaining, "\nfunc ")
	if end == -1 {
		return source[start:]
	}

	return source[start : start+len("func ")+end]
}

func TestMLWHServeFeedbackDBFlagD1(t *testing.T) {
	convey.Convey("D1.6: Given WA_MLWH_FEEDBACK_PATH and no flag, then --feedback-db resolves to that path; given --feedback-db, the flag wins", t, func() {
		envPath := filepath.Join(t.TempDir(), "fb.sqlite")
		otherPath := filepath.Join(t.TempDir(), "other.sqlite")
		t.Setenv("WA_MLWH_FEEDBACK_PATH", envPath)

		convey.So(mlwhServeFeedbackDBFlagForTest(t, nil), convey.ShouldEqual, envPath)
		convey.So(mlwhServeFeedbackDBFlagForTest(t, []string{"--feedback-db", otherPath}), convey.ShouldEqual, otherPath)

		t.Setenv("WA_MLWH_FEEDBACK_PATH", "")
		convey.So(mlwhServeFeedbackDBFlagForTest(t, nil), convey.ShouldEqual, "")
	})

	convey.Convey("D1.7: Given the mlwh serve command from NewRootCommand, then its Long names --feedback-db, WA_MLWH_FEEDBACK_PATH, and .wa-mlwh-server.token", t, func() {
		serve, _, err := NewRootCommand().Find([]string{"mlwh", "serve"})
		convey.So(err, convey.ShouldBeNil)
		convey.So(serve.Long, convey.ShouldContainSubstring, "--feedback-db")
		convey.So(serve.Long, convey.ShouldContainSubstring, "WA_MLWH_FEEDBACK_PATH")
		convey.So(serve.Long, convey.ShouldContainSubstring, ".wa-mlwh-server.token")
	})
}

func mlwhServeFeedbackDBFlagForTest(t *testing.T, args []string) string {
	t.Helper()

	serve, _, err := NewRootCommand().Find([]string{"mlwh", "serve"})
	if err != nil {
		t.Fatalf("find mlwh serve: %v", err)
	}

	if err = serve.ParseFlags(args); err != nil {
		t.Fatalf("parse mlwh serve flags: %v", err)
	}

	value, err := serve.Flags().GetString("feedback-db")
	if err != nil {
		t.Fatalf("get --feedback-db: %v", err)
	}

	return value
}
