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

	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode mlwh error envelope: %v", err)
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

func installFakeMLWHServeAuthServer(t *testing.T, fake *fakeMLWHServeAuthServer) {
	t.Helper()

	originalNewAuthServer := mlwhServeNewAuthServer
	mlwhServeNewAuthServer = func(io.Writer) mlwhServeAuthServer {
		return fake
	}
	t.Cleanup(func() {
		mlwhServeNewAuthServer = originalNewAuthServer
	})
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
