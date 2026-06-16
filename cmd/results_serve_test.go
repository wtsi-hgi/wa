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
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/mlwh"
	"github.com/wtsi-hgi/wa/results"
)

func TestResultsServeCommandNoSeqmetaFlags(t *testing.T) {
	t.Setenv("WA_MLWH_BACKEND_URL", "http://seqmeta.example")

	command := newResultsServeCommand()

	convey.Convey("E2.4: Given results serve flags, then seqmeta URL and timeout flags are absent", t, func() {
		convey.So(command.Flags().Lookup("seqmeta-url"), convey.ShouldBeNil)
		convey.So(command.Flags().Lookup("seqmeta-timeout"), convey.ShouldBeNil)
	})
}

func TestResultsServeCommandHelpIncludesMLWHFlags(t *testing.T) {
	convey.Convey("E2.4: Given results serve help, then it documents MLWH selection and omits seqmeta flags", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "serve", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "--mlwh-server-url")
		convey.So(output, convey.ShouldContainSubstring, "--mlwh-cache")
		convey.So(output, convey.ShouldContainSubstring, "MLWH cache")
		convey.So(output, convey.ShouldNotContainSubstring, "--seqmeta-url")
		convey.So(output, convey.ShouldNotContainSubstring, "--seqmeta-timeout")
		convey.So(output, convey.ShouldNotContainSubstring, "--mlwh-sync-interval")
	})
}

type fakeResultsServeMLWHHandle struct {
	mlwh.Queryer

	mu                 sync.Mutex
	classifiedValues   []string
	classifyByRaw      map[string]mlwh.Match
	expandSearchValue  mlwh.SearchValues
	resolveStudyByRaw  map[string]mlwh.Match
	resolveRunByRaw    map[string]mlwh.Match
	resolveSampleByRaw map[string]mlwh.Match
}

func (f *fakeResultsServeMLWHHandle) ClassifyIdentifier(_ context.Context, raw string) (mlwh.Match, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.classifiedValues = append(f.classifiedValues, raw)
	if match, ok := f.classifyByRaw[raw]; ok {
		return match, nil
	}

	return mlwh.Match{Kind: mlwh.KindRunID, Canonical: raw}, nil
}

func (f *fakeResultsServeMLWHHandle) ResolveRun(_ context.Context, raw string) (mlwh.Match, error) {
	if match, ok := f.resolveRunByRaw[raw]; ok {
		return match, nil
	}

	return mlwh.Match{}, mlwh.ErrNotFound
}

func (f *fakeResultsServeMLWHHandle) ResolveStudy(_ context.Context, raw string) (mlwh.Match, error) {
	if match, ok := f.resolveStudyByRaw[raw]; ok {
		return match, nil
	}

	return mlwh.Match{}, mlwh.ErrNotFound
}

func (f *fakeResultsServeMLWHHandle) ResolveSample(_ context.Context, raw string) (mlwh.Match, error) {
	if match, ok := f.resolveSampleByRaw[raw]; ok {
		return match, nil
	}

	return mlwh.Match{}, mlwh.ErrNotFound
}

func (f *fakeResultsServeMLWHHandle) ResolveSampleName(context.Context, string) (mlwh.Match, error) {
	return mlwh.Match{}, mlwh.ErrNotFound
}

func (f *fakeResultsServeMLWHHandle) ResolveLibrary(context.Context, string) (mlwh.Match, error) {
	return mlwh.Match{}, mlwh.ErrNotFound
}

func (f *fakeResultsServeMLWHHandle) ResolveLibraryIdentifier(context.Context, string) (mlwh.Match, error) {
	return mlwh.Match{}, mlwh.ErrNotFound
}

func (f *fakeResultsServeMLWHHandle) ExpandSearchValues(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error) {
	return f.expandSearchValue, nil
}

func (f *fakeResultsServeMLWHHandle) Close() error {
	return nil
}

func (f *fakeResultsServeMLWHHandle) ClassifiedValues() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	values := make([]string, len(f.classifiedValues))
	copy(values, f.classifiedValues)

	return values
}

func TestResultsServeMLWHHandleE2(t *testing.T) {
	convey.Convey("E2.1: Given a remote MLWH server URL and no cache path, the results serve handle is a RemoteClient", t, func() {
		handle, err := openResultsServeMLWHClientWithConfig(
			context.Background(),
			resultsServeMLWHConfig{ServerURL: "https://mlwh.example:9000"},
		)

		convey.So(err, convey.ShouldBeNil)
		defer func() { convey.So(handle.Close(), convey.ShouldBeNil) }()
		_, ok := handle.(*mlwh.RemoteClient)
		convey.So(ok, convey.ShouldBeTrue)
	})

	convey.Convey("E2.2: Given a local MLWH cache path and no server URL, the results serve handle is a local Client", t, func() {
		handle, err := openResultsServeMLWHClientWithConfig(
			context.Background(),
			resultsServeMLWHConfig{CachePath: filepath.Join(t.TempDir(), "mlwh.sqlite")},
		)

		convey.So(err, convey.ShouldBeNil)
		defer func() { convey.So(handle.Close(), convey.ShouldBeNil) }()
		_, ok := handle.(*mlwh.Client)
		convey.So(ok, convey.ShouldBeTrue)
	})
}

func TestResultsServeCommand(t *testing.T) {
	originalListen := listenFunc
	originalOpenMLWH := resultsServeOpenMLWHClient
	defer func() { listenFunc = originalListen }()
	defer func() { resultsServeOpenMLWHClient = originalOpenMLWH }()

	convey.Convey("results serve rejects password-bearing MySQL DSNs on the command line", t, func() {
		_, err := resolveResultsServeDBDSN("user:secret@tcp(localhost:3306)/results", true)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "must not be supplied on the command line")
	})

	convey.Convey("results serve can combine a passwordless MySQL DSN with WA_RESULTS_DB_PASSWORD", t, func() {
		t.Setenv("WA_RESULTS_DB_PASSWORD", "secret")

		dsn, err := resolveResultsServeDBDSN("user@tcp(localhost:3306)/results", true)

		convey.So(err, convey.ShouldBeNil)
		convey.So(dsn, convey.ShouldEqual, "user:secret@tcp(localhost:3306)/results")
	})

	convey.Convey("results serve falls back to WA_RESULTS_DB_PATH without exposing it as a flag default", t, func() {
		t.Setenv("WA_RESULTS_DB_PATH", "user:secret@tcp(localhost:3306)/results")

		dsn, err := resolveResultsServeDBDSN("results.db", false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(dsn, convey.ShouldEqual, "user:secret@tcp(localhost:3306)/results")
	})

	convey.Convey("H1.1: Given results serve with faked auth, when started, then POST /rest/v1/auth/results with valid JSON returns 201", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)

		var statusCode int
		fakeAuth.onStart = func(server *fakeResultsServeAuthServer) error {
			body, err := json.Marshal(registrationForResultsServeTest(t))
			convey.So(err, convey.ShouldBeNil)

			request := httptest.NewRequest(http.MethodPost, gas.EndPointAuth+"/results", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.router.ServeHTTP(response, request)
			statusCode = response.Code

			return nil
		}

		_, err := executeRootCommandForTest(t, secureResultsServeArgs("--port", "0"))

		convey.So(err, convey.ShouldBeNil)
		convey.So(statusCode, convey.ShouldEqual, http.StatusCreated)
	})

	convey.Convey("E2.5: Given results serve with an MLWH queryer, posting seqmeta metadata validates through that queryer", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)

		fakeMLWH := &fakeResultsServeMLWHHandle{
			classifyByRaw: map[string]mlwh.Match{
				"48522": {Kind: mlwh.KindRunID, Canonical: "48522"},
			},
		}
		resultsServeOpenMLWHClient = func(context.Context, resultsServeMLWHConfig) (resultsServeMLWHHandle, error) {
			return fakeMLWH, nil
		}

		var statusCode int
		fakeAuth.onStart = func(server *fakeResultsServeAuthServer) error {
			registration := registrationForResultsServeTest(t)
			registration.Metadata = map[string]string{"seqmeta_runid": "48522"}

			body, err := json.Marshal(registration)
			convey.So(err, convey.ShouldBeNil)

			request := httptest.NewRequest(http.MethodPost, gas.EndPointAuth+"/results", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.router.ServeHTTP(response, request)
			statusCode = response.Code

			return nil
		}

		_, err := executeRootCommandForTest(t, secureResultsServeArgs("--port", "0", "--mlwh-server-url", "https://mlwh.example"))
		convey.So(err, convey.ShouldBeNil)
		convey.So(statusCode, convey.ShouldEqual, http.StatusCreated)
		convey.So(fakeMLWH.ClassifiedValues(), convey.ShouldResemble, []string{"48522"})
	})

	convey.Convey("H1.3: Given results serve --port abc, then exit code is non-zero", t, func() {
		_, err := executeRootCommandForTest(t, []string{"results", "serve", "--port", "abc"})

		convey.So(err, convey.ShouldNotBeNil)
	})

	convey.Convey("results serve reports a clear error when the SQLite database directory does not exist", t, func() {
		dbPath := filepath.Join(t.TempDir(), "missing", "results.db")

		output, err := executeRootCommandForTest(t, secureResultsServeArgs("--port", "8725", "--db", dbPath))

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "results database directory does not exist")
		convey.So(output, convey.ShouldContainSubstring, filepath.Dir(dbPath))
	})

	convey.Convey("SQLite paths containing @ still use the sqlite driver", t, func() {
		convey.So(resultsDBDriverName("/tmp/results@review.db"), convey.ShouldEqual, "sqlite")
		convey.So(resultsDBDriverName(":memory:"), convey.ShouldEqual, "sqlite")
		convey.So(resultsDBDriverName("user:pass@tcp(localhost:3306)/results"), convey.ShouldEqual, "mysql")
	})

	convey.Convey("SQLite file paths use WAL and a busy timeout for concurrent e2e reads and writes", t, func() {
		dbPath := filepath.Join(t.TempDir(), "results.db")

		convey.So(
			resultsSQLiteDSN(dbPath),
			convey.ShouldEqual,
			"file:"+filepath.ToSlash(dbPath)+"?mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		)
		convey.So(resultsSQLiteDSN(":memory:"), convey.ShouldEqual, ":memory:")
		convey.So(resultsSQLiteDSN("file:/tmp/results.db?mode=ro"), convey.ShouldEqual, "file:/tmp/results.db?mode=ro")
	})

	convey.Convey("E2.1: Given WA_MLWH_SERVER_URL and no cache path, when results serve boots, then remote MLWH config is selected", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)

		t.Setenv("WA_MLWH_SERVER_URL", "https://mlwh:9000")
		t.Setenv("WA_MLWH_CACHE_PATH", "")

		seenConfigCh := make(chan resultsServeMLWHConfig, 1)
		resultsServeOpenMLWHClient = func(_ context.Context, cfg resultsServeMLWHConfig) (resultsServeMLWHHandle, error) {
			seenConfigCh <- cfg

			return &fakeResultsServeMLWHHandle{}, nil
		}

		cmd := NewRootCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(secureResultsServeArgs("--port", "0"))

		err := cmd.ExecuteContext(context.Background())
		seenConfig := <-seenConfigCh

		convey.So(err, convey.ShouldBeNil)
		convey.So(seenConfig.ServerURL, convey.ShouldEqual, "https://mlwh:9000")
		convey.So(seenConfig.CachePath, convey.ShouldEqual, "")
	})

	convey.Convey("E2.2: Given only WA_MLWH_CACHE_PATH, when results serve boots, then local MLWH cache config is selected", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)

		cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
		t.Setenv("WA_MLWH_SERVER_URL", "")
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)

		seenConfigCh := make(chan resultsServeMLWHConfig, 1)
		resultsServeOpenMLWHClient = func(_ context.Context, cfg resultsServeMLWHConfig) (resultsServeMLWHHandle, error) {
			seenConfigCh <- cfg

			return &fakeResultsServeMLWHHandle{}, nil
		}

		_, err := executeRootCommandForTest(t, []string{
			"results", "serve",
			"--db", ":memory:",
			"--cert", "cert.pem",
			"--key", "key.pem",
			"--ldap_server", "ldap.example.org",
			"--ldap_dn", "uid=%s,ou=people,dc=example,dc=org",
			"--port", "0",
		})
		seenConfig := <-seenConfigCh

		convey.So(err, convey.ShouldBeNil)
		convey.So(seenConfig.ServerURL, convey.ShouldEqual, "")
		convey.So(seenConfig.CachePath, convey.ShouldEqual, cachePath)
	})

	convey.Convey("E2.3: Given no MLWH server URL or cache path, when results serve runs, then configuration is rejected", t, func() {
		t.Setenv("WA_MLWH_SERVER_URL", "")
		t.Setenv("WA_MLWH_CACHE_PATH", "")

		_, err := executeRootCommandForTest(t, secureResultsServeArgs("--port", "0", "--mlwh-server-url", ""))

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_MLWH_SERVER_URL")
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
	})

	convey.Convey("results serve opens the read-only MLWH resolver from cache when cache config is present", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)

		resultsServeOpenMLWHClient = openResultsServeMLWHClientWithConfig
		t.Setenv("WA_MLWH_SERVER_URL", "")
		cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")

		_, err := executeRootCommandForTest(t, secureResultsServeArgs("--port", "0", "--mlwh-cache", cachePath))

		convey.So(err, convey.ShouldBeNil)
	})

	convey.Convey("E5.3: Given --mlwh-cache with an embedded password, when results serve parses flags, then the error wraps ErrPasswordInDSN and names --mlwh-cache", t, func() {
		t.Setenv("WA_MLWH_SERVER_URL", "")

		_, err := executeRootCommandForTest(t, secureResultsServeArgs("--mlwh-cache", "cache_user:secret@tcp(localhost:3306)/wa_cache"))

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, mlwh.ErrPasswordInDSN.Error())
		convey.So(err.Error(), convey.ShouldContainSubstring, "--mlwh-cache")
	})

	convey.Convey("E5.4: results serve rejects the removed --mlwh-sync-interval flag", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "serve", "--db", ":memory:", "--mlwh-sync-interval", "5m"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "unknown flag: --mlwh-sync-interval")
	})

	convey.Convey("E5.5: Given the default sync interval, when results serve runs, then no MLWH sync ticker is started", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)

		resultsServeOpenMLWHClient = func(context.Context, resultsServeMLWHConfig) (resultsServeMLWHHandle, error) {
			return &fakeResultsServeMLWHHandle{}, nil
		}

		tickerCreated := 0
		resultsServeNewTicker = func(_ time.Duration) resultsServeTicker {
			tickerCreated++

			return newFakeResultsServeTicker()
		}

		cmd := NewRootCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(secureResultsServeArgs("--port", "0"))

		err := cmd.ExecuteContext(context.Background())

		convey.So(err, convey.ShouldBeNil)
		convey.So(tickerCreated, convey.ShouldEqual, 0)
	})

	convey.Convey("Bug 260519-2: Given results serve with a sample-only MLWH cache, seqmeta_supplier_name search uses the runtime sample-only expansion", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)

		resultsServeOpenMLWHClient = openResultsServeMLWHClientWithConfig
		t.Setenv("WA_MLWH_SERVER_URL", "")
		dbPath := filepath.Join(t.TempDir(), "results.sqlite")
		mlwhCachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
		seedResultsServeDirectSampleSearchFixture(t, dbPath, mlwhCachePath)

		var response *httptest.ResponseRecorder
		fakeAuth.onStart = func(server *fakeResultsServeAuthServer) error {
			request := httptest.NewRequest(http.MethodGet, gas.EndPointREST+"/results?seqmeta_supplier_name=Hek_R1", nil)
			response = httptest.NewRecorder()
			server.router.ServeHTTP(response, request)

			return nil
		}

		_, err := executeRootCommandForTest(t, secureResultsServeArgs("--port", "0", "--db", dbPath, "--mlwh-cache", mlwhCachePath))
		convey.So(err, convey.ShouldBeNil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var payload []results.ResultSet
		convey.So(json.NewDecoder(response.Body).Decode(&payload), convey.ShouldBeNil)
		convey.So(payload, convey.ShouldHaveLength, 1)
		convey.So(payload[0].RunKey, convey.ShouldEqual, "direct-supplier")
	})
}

func newFakeResultsServeAuthServer() *fakeResultsServeAuthServer {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	return &fakeResultsServeAuthServer{
		router: router,
	}
}

func installFakeResultsServeAuthServer(t *testing.T, fake *fakeResultsServeAuthServer) {
	t.Helper()

	originalNewAuthServer := resultsServeNewAuthServer
	resultsServeNewAuthServer = func(io.Writer) resultsServeAuthServer {
		return fake
	}

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Cleanup(func() {
		resultsServeNewAuthServer = originalNewAuthServer
	})
}

func registrationForResultsServeTest(t *testing.T) *results.Registration {
	t.Helper()

	registration := registerCommandRegistrationForTest()
	registration.OutputDirectory = t.TempDir()
	registration.Files[0].Path = filepath.Join(registration.OutputDirectory, "out.txt")

	return registration
}

func secureResultsServeArgs(extra ...string) []string {
	args := []string{
		"results", "serve",
		"--db", ":memory:",
		"--cert", "cert.pem",
		"--key", "key.pem",
		"--ldap_server", "ldap.example.org",
		"--ldap_dn", "uid=%s,ou=people,dc=example,dc=org",
	}
	if !resultsServeArgsIncludeMLWHForTest(extra) && strings.TrimSpace(os.Getenv("WA_MLWH_SERVER_URL")) == "" {
		args = append(args, "--mlwh-server-url", "https://mlwh.example")
	}

	return append(args, extra...)
}

func resultsServeArgsIncludeMLWHForTest(args []string) bool {
	for _, arg := range args {
		if arg == "--mlwh-server-url" || strings.HasPrefix(arg, "--mlwh-server-url=") {
			return true
		}
		if arg == "--mlwh-cache" || strings.HasPrefix(arg, "--mlwh-cache=") {
			return true
		}
	}

	return false
}

type fakeResultsServeAuthEnableCall struct {
	certFile      string
	keyFile       string
	tokenBasename string
}

type fakeResultsServeAuthStartCall struct {
	kind     string
	addr     string
	certFile string
	keyFile  string
	acmeURL  string
	cacheDir string
}

type fakeResultsServeAuthServer struct {
	router      *gin.Engine
	auth        *gin.RouterGroup
	enableCalls []fakeResultsServeAuthEnableCall
	startCalls  []fakeResultsServeAuthStartCall
	onStart     func(*fakeResultsServeAuthServer) error
}

func (f *fakeResultsServeAuthServer) Router() *gin.Engine {
	return f.router
}

func (f *fakeResultsServeAuthServer) AuthRouter() *gin.RouterGroup {
	return f.auth
}

func (f *fakeResultsServeAuthServer) EnableAuthWithServerToken(certFile, keyFile, tokenBasename string, _ gas.AuthCallback) error {
	f.enableCalls = append(f.enableCalls, fakeResultsServeAuthEnableCall{
		certFile:      certFile,
		keyFile:       keyFile,
		tokenBasename: tokenBasename,
	})
	f.auth = f.router.Group(gas.EndPointAuth)

	return nil
}

func (f *fakeResultsServeAuthServer) Start(addr, certFile, keyFile string) error {
	f.startCalls = append(f.startCalls, fakeResultsServeAuthStartCall{
		kind:     "start",
		addr:     addr,
		certFile: certFile,
		keyFile:  keyFile,
	})

	if f.onStart != nil {
		return f.onStart(f)
	}

	return nil
}

func (f *fakeResultsServeAuthServer) StartACME(addr string, acmeURL, cacheDir string) error {
	f.startCalls = append(f.startCalls, fakeResultsServeAuthStartCall{
		kind:     "acme",
		addr:     addr,
		acmeURL:  acmeURL,
		cacheDir: cacheDir,
	})

	if f.onStart != nil {
		return f.onStart(f)
	}

	return nil
}

func (f *fakeResultsServeAuthServer) StartACMETLSOnly(addr string, acmeURL, cacheDir string) error {
	f.startCalls = append(f.startCalls, fakeResultsServeAuthStartCall{
		kind:     "acme-tls-only",
		addr:     addr,
		acmeURL:  acmeURL,
		cacheDir: cacheDir,
	})

	if f.onStart != nil {
		return f.onStart(f)
	}

	return nil
}

func (f *fakeResultsServeAuthServer) Stop() {}

func TestResultsServeCommandA2(t *testing.T) {
	convey.Convey("A2.1: Given results serve --url without certs, when validation runs, then TLS material is required", t, func() {
		clearResultsServeTLSModeEnvForTest(t)

		_, err := executeRootCommandForTest(t, []string{"results", "serve", "--db", ":memory:", "--url", "127.0.0.1:8443"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldEqual, "you must supply --cert and --key, or --acme and --cache")
	})

	convey.Convey("A2.2: Given cert/key but no LDAP flags, when validation runs in non-test mode, then LDAP is required", t, func() {
		t.Setenv("WA_RESULTS_LDAP_SERVER", "")
		t.Setenv("WA_RESULTS_LDAP_DN", "")

		_, err := executeRootCommandForTest(t, []string{"results", "serve", "--db", ":memory:", "--cert", "cert.pem", "--key", "key.pem"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldEqual, "--ldap_server and --ldap_dn are required")
	})

	convey.Convey("A2.3: Given cert/key and LDAP flags, when wired with fakes, then authserver receives TLS paths and server-token basename", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, secureResultsServeArgs())

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.enableCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.enableCalls[0].certFile, convey.ShouldEqual, "cert.pem")
		convey.So(fakeAuth.enableCalls[0].keyFile, convey.ShouldEqual, "key.pem")
		convey.So(fakeAuth.enableCalls[0].tokenBasename, convey.ShouldEqual, resultsServerTokenBasename)
	})

	convey.Convey("Given an existing server token with loose permissions, owner-session setup rotates it and keeps authserver token reuse aligned", t, func() {
		tokenPath := filepath.Join(t.TempDir(), "server.token")
		leakedToken, err := gas.GenerateToken()
		convey.So(err, convey.ShouldBeNil)
		convey.So(os.WriteFile(tokenPath, leakedToken, 0o644), convey.ShouldBeNil)

		ownerConfig, err := resultsServeOwnerSessionConfig(tokenPath, results.NewOwnerSessionStore())
		convey.So(err, convey.ShouldBeNil)
		convey.So(bytes.Equal(ownerConfig.ServerToken, leakedToken), convey.ShouldBeFalse)

		info, err := os.Stat(tokenPath)
		convey.So(err, convey.ShouldBeNil)
		convey.So(info.Mode().Perm(), convey.ShouldEqual, 0o600)

		authServerToken, err := gas.GenerateAndStoreTokenForSelfClient(tokenPath)
		convey.So(err, convey.ShouldBeNil)
		convey.So(authServerToken, convey.ShouldResemble, ownerConfig.ServerToken)
	})

	convey.Convey("A2.3b: Given both cert/key and ACME flags, when validation runs, then TLS mode selection is rejected as ambiguous", t, func() {
		cacheDir := filepath.Join(t.TempDir(), "certs")
		convey.So(os.Mkdir(cacheDir, 0o700), convey.ShouldBeNil)

		_, err := executeRootCommandForTest(t, secureResultsServeArgs(
			"--acme", "https://acme.example/dir",
			"--cache", cacheDir,
		))

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldEqual, "you must supply either --cert and --key, or --acme and --cache, not both")
	})

	convey.Convey("A2.4: Given ACME cache dir with loose permissions, when startup runs, then it fails before serving", t, func() {
		clearResultsServeTLSModeEnvForTest(t)

		cacheDir := filepath.Join(t.TempDir(), "certs")
		convey.So(os.Mkdir(cacheDir, 0o755), convey.ShouldBeNil)

		_, err := executeRootCommandForTest(t, []string{
			"results", "serve",
			"--db", ":memory:",
			"--acme", "https://acme.example/dir",
			"--cache", cacheDir,
			"--ldap_server", "ldap.example.org",
			"--ldap_dn", "uid=%s,ou=people,dc=example,dc=org",
		})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldEqual, "cert cache directory must only be readable by the server user")
	})

	convey.Convey("A2.5: Given legacy --port with cert/key/LDAP flags, when validation runs, then HTTPS bind addr uses localhost port", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)
		t.Setenv("WA_ENV", "")
		t.Setenv("WA_DEV_RESULTS_HOST", "")
		t.Setenv("WA_PROD_RESULTS_HOST", "")
		t.Setenv("WA_RESULTS_SERVER_URL", "")

		_, err := executeRootCommandForTest(t, secureResultsServeArgs("--port", "9443"))

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.startCalls[0].kind, convey.ShouldEqual, "start")
		convey.So(fakeAuth.startCalls[0].addr, convey.ShouldEqual, "127.0.0.1:9443")
	})

	convey.Convey("Given development bind host and port envs, results serve ignores WA_RESULTS_SERVER_URL and binds that host and port without --port", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_RESULTS_PORT", "3672")
		t.Setenv("WA_RESULTS_SERVER_URL", "https://dev-host.example.org:3672")

		_, err := executeRootCommandForTest(t, secureResultsServeArgs())

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.startCalls[0].kind, convey.ShouldEqual, "start")
		convey.So(fakeAuth.startCalls[0].addr, convey.ShouldEqual, "0.0.0.0:3672")
	})

	convey.Convey("Given production bind host and port envs, results serve ignores WA_RESULTS_SERVER_URL and binds that host and port without --port", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)
		t.Setenv("WA_ENV", "production")
		t.Setenv("WA_PROD_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_PROD_RESULTS_PORT", "8090")
		t.Setenv("WA_RESULTS_SERVER_URL", "https://prod-host.example.org:8090")

		_, err := executeRootCommandForTest(t, secureResultsServeArgs())

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.startCalls[0].kind, convey.ShouldEqual, "start")
		convey.So(fakeAuth.startCalls[0].addr, convey.ShouldEqual, "0.0.0.0:8090")
	})

	convey.Convey("Given active bind envs and explicit --port, results serve keeps the port override", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_RESULTS_PORT", "3672")

		_, err := executeRootCommandForTest(t, secureResultsServeArgs("--port", "9443"))

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.startCalls[0].kind, convey.ShouldEqual, "start")
		convey.So(fakeAuth.startCalls[0].addr, convey.ShouldEqual, "0.0.0.0:9443")
	})

	convey.Convey("Given active bind envs and explicit --url, results serve keeps the URL override", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)
		t.Setenv("WA_ENV", "development")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")
		t.Setenv("WA_DEV_RESULTS_PORT", "3672")

		_, err := executeRootCommandForTest(t, secureResultsServeArgs("--url", "127.0.0.1:9443"))

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.startCalls[0].kind, convey.ShouldEqual, "start")
		convey.So(fakeAuth.startCalls[0].addr, convey.ShouldEqual, "127.0.0.1:9443")
	})

	convey.Convey("Given no active scenario port, results serve falls back to localhost 8080 and ignores WA_RESULTS_SERVER_URL", t, func() {
		fakeAuth := newFakeResultsServeAuthServer()
		installFakeResultsServeAuthServer(t, fakeAuth)
		t.Setenv("WA_ENV", "")
		t.Setenv("WA_TEST_RESULTS_PORT", "")
		t.Setenv("WA_DEV_RESULTS_PORT", "")
		t.Setenv("WA_PROD_RESULTS_PORT", "")
		t.Setenv("WA_DEV_RESULTS_HOST", "")
		t.Setenv("WA_PROD_RESULTS_HOST", "")
		t.Setenv("WA_RESULTS_SERVER_URL", "https://public.example.org:3672")

		_, err := executeRootCommandForTest(t, secureResultsServeArgs())

		convey.So(err, convey.ShouldBeNil)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 1)
		convey.So(fakeAuth.startCalls[0].kind, convey.ShouldEqual, "start")
		convey.So(fakeAuth.startCalls[0].addr, convey.ShouldEqual, "127.0.0.1:8080")
	})
}

func clearResultsServeTLSModeEnvForTest(t *testing.T) {
	t.Helper()

	t.Setenv("WA_RESULTS_SERVER_CERT", "")
	t.Setenv("WA_RESULTS_SERVER_KEY", "")
	t.Setenv("WA_RESULTS_SERVER_ACME", "")
	t.Setenv("WA_RESULTS_SERVER_CACHE", "")
}

func resultsServeListenFuncForTest(addrCh chan<- string) func(string, string) (net.Listener, error) {
	return func(network, addr string) (net.Listener, error) {
		listener, err := net.Listen(network, "127.0.0.1:0")
		if err == nil {
			addrCh <- listener.Addr().String()
		}

		return listener, err
	}
}

func seedResultsServeDirectSampleSearchFixture(t *testing.T, dbPath, mlwhCachePath string) {
	t.Helper()

	db, err := openResultsDB(dbPath)
	if err != nil {
		t.Fatalf("open results DB: %v", err)
	}
	store, err := results.NewStore(db)
	if err != nil {
		t.Fatalf("create results store: %v", err)
	}
	_, err = store.Upsert(context.Background(), &results.Registration{
		PipelineIdentifier: "pipeline-direct-supplier",
		RunKey:             "direct-supplier",
		Requester:          "alice",
		Operator:           "bob",
		Command:            "nextflow run",
		PipelineName:       "nf",
		PipelineVersion:    "1.0.0",
		OutputDirectory:    t.TempDir(),
		Metadata: map[string]string{
			results.SeqmetaSampleNameKey: "7607STDY14643771",
		},
	})
	if err != nil {
		t.Fatalf("seed results store: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatalf("close results store: %v", err)
	}

	cache, err := mlwh.OpenCache(context.Background(), mlwh.CacheConfig{Path: mlwhCachePath})
	if err != nil {
		t.Fatalf("open mlwh cache: %v", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close mlwh cache: %v", err)
		}
	}()

	_, err = cache.DB().Exec(
		`INSERT INTO sample_mirror(
			id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name,
			sanger_sample_id, supplier_name, accession_number, donor_id,
			taxon_id, common_name, description, last_updated
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, "SQSCP", "101", "sample-uuid-1", "7607STDY14643771",
		"7607STDY14643771", "Hek_R1", "", "donor-1",
		9606, "human", "", "2026-05-19T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("seed mlwh sample: %v", err)
	}

	_, err = cache.DB().Exec(
		`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped)
		 VALUES (?, ?, ?, ?, ?)`,
		"sample", "2026-05-19T00:00:00Z", "2026-05-19T00:00:00Z", nil, 0,
	)
	if err != nil {
		t.Fatalf("seed mlwh sample sync state: %v", err)
	}
}

func executeServeCommandUntilListeningForTest(t *testing.T, args []string) error {
	t.Helper()

	addrCh := make(chan string, 1)
	listenFunc = resultsServeListenFuncForTest(addrCh)

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()

	select {
	case <-addrCh:
		cancel()

		select {
		case err := <-errCh:
			return err
		case <-time.After(time.Second):
			return errors.New("timed out waiting for serve command to stop")
		}
	case err := <-errCh:
		return err
	case <-time.After(time.Second):
		return errors.New("timed out waiting for serve command to listen")
	}
}

func newFakeResultsServeTicker() *fakeResultsServeTicker {
	return &fakeResultsServeTicker{
		ch:      make(chan time.Time, 8),
		stopped: make(chan struct{}),
	}
}

type fakeResultsServeTicker struct {
	ch       chan time.Time
	stopped  chan struct{}
	stopOnce sync.Once
}

func (f *fakeResultsServeTicker) Chan() <-chan time.Time {
	return f.ch
}

func (f *fakeResultsServeTicker) Stop() {
	f.stopOnce.Do(func() {
		close(f.stopped)
	})
}

func (f *fakeResultsServeTicker) Tick() {
	f.ch <- time.Now()
}

func (f *fakeResultsServeTicker) WaitForStop(t *testing.T) {
	t.Helper()

	select {
	case <-f.stopped:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ticker stop")
	}
}
