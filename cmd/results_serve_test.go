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
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
	"github.com/wtsi-hgi/wa/results"
)

func TestResultsServeCommandSeqmetaURLFallback(t *testing.T) {
	t.Setenv("WA_SEQMETA_BACKEND_URL", "http://seqmeta.example")

	command := newResultsServeCommand()
	flag := command.Flags().Lookup("seqmeta-url")
	if flag == nil {
		t.Fatal("expected seqmeta-url flag")
	}

	convey.Convey("results serve falls back to WA_SEQMETA_BACKEND_URL when --seqmeta-url is unset", t, func() {
		convey.So(flag.DefValue, convey.ShouldEqual, "http://seqmeta.example")
	})
}

func TestResultsServeCommandHelpIncludesMLWHFlags(t *testing.T) {
	convey.Convey("E5.1: Given results serve help, then it documents the MLWH cache and sync flags", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "serve", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "--mlwh-cache")
		convey.So(output, convey.ShouldContainSubstring, "MLWH cache")
		convey.So(output, convey.ShouldContainSubstring, "--mlwh-sync-interval")
		convey.So(output, convey.ShouldContainSubstring, "sync")
	})
}

type fakeResultsServeSyncClient struct {
	mu        sync.Mutex
	syncCalls int
	syncCh    chan struct{}
}

func (f *fakeResultsServeSyncClient) Sync(context.Context, ...string) ([]mlwh.SyncReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.syncCh == nil {
		f.syncCh = make(chan struct{}, 8)
	}

	f.syncCalls++
	f.syncCh <- struct{}{}

	return nil, nil
}

func (f *fakeResultsServeSyncClient) ExpandIdentifier(context.Context, mlwh.IdentifierKind, string) ([]mlwh.TaggedID, error) {
	return nil, nil
}

func (f *fakeResultsServeSyncClient) LanesForSample(context.Context, string, int, int) ([]mlwh.Lane, error) {
	return nil, nil
}

func (f *fakeResultsServeSyncClient) Close() error {
	return nil
}

func (f *fakeResultsServeSyncClient) SyncCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.syncCalls
}

func (f *fakeResultsServeSyncClient) WaitForSyncCall(t *testing.T) {
	t.Helper()

	f.mu.Lock()
	if f.syncCh == nil {
		f.syncCh = make(chan struct{}, 8)
	}
	ch := f.syncCh
	f.mu.Unlock()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sync call")
	}
}

func TestResultsServeCommand(t *testing.T) {
	originalListen := listenFunc
	originalOpenMLWH := resultsServeOpenMLWHClient
	originalNewTicker := resultsServeNewTicker
	defer func() { listenFunc = originalListen }()
	defer func() { resultsServeOpenMLWHClient = originalOpenMLWH }()
	defer func() { resultsServeNewTicker = originalNewTicker }()

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

	convey.Convey("H1.1: Given results serve --port 0 --db :memory:, when started, then POST /results with valid JSON returns 201", t, func() {
		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		cmd := NewRootCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"results", "serve", "--port", "0", "--db", ":memory:"})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		addr := <-addrCh
		response, err := postResultsRegistrationForTest("http://"+addr+"/results", registerCommandRegistrationForTest())
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = response.Body.Close() }()
		convey.So(response.StatusCode, convey.ShouldEqual, http.StatusCreated)

		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)
	})

	convey.Convey("H1.2: Given results serve with --seqmeta-url, posting seqmeta metadata triggers validation", t, func() {
		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		validationCh := make(chan string, 1)
		seqmetaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			validationCh <- r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "run_id", "object": map[string]any{}})
		}))
		defer seqmetaServer.Close()

		cmd := NewRootCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"results", "serve", "--port", "0", "--db", ":memory:", "--seqmeta-url", seqmetaServer.URL})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		addr := <-addrCh
		registration := registerCommandRegistrationForTest()
		registration.Metadata = map[string]string{"seqmeta_runid": "48522"}

		response, err := postResultsRegistrationForTest("http://"+addr+"/results", registration)
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = response.Body.Close() }()
		convey.So(response.StatusCode, convey.ShouldEqual, http.StatusCreated)
		convey.So(<-validationCh, convey.ShouldEqual, "/validate/48522")

		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)
	})

	convey.Convey("H1.3: Given results serve --port abc, then exit code is non-zero", t, func() {
		_, err := executeRootCommandForTest(t, []string{"results", "serve", "--port", "abc"})

		convey.So(err, convey.ShouldNotBeNil)
	})

	convey.Convey("results serve reports a clear error when the SQLite database directory does not exist", t, func() {
		dbPath := filepath.Join(t.TempDir(), "missing", "results.db")

		output, err := executeRootCommandForTest(t, []string{"results", "serve", "--port", "8725", "--db", dbPath})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "results database directory does not exist")
		convey.So(output, convey.ShouldContainSubstring, filepath.Dir(dbPath))
	})

	convey.Convey("SQLite paths containing @ still use the sqlite driver", t, func() {
		convey.So(resultsDBDriverName("/tmp/results@review.db"), convey.ShouldEqual, "sqlite")
		convey.So(resultsDBDriverName(":memory:"), convey.ShouldEqual, "sqlite")
		convey.So(resultsDBDriverName("user:pass@tcp(localhost:3306)/results"), convey.ShouldEqual, "mysql")
	})

	convey.Convey("E5.2: Given MLWH env vars and no flag overrides, when results serve boots, then the resolved DSN includes the env password and the cache path comes from the environment", t, func() {
		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		secret := "topsecret"
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(mlwh-db-ro:3435)/mlwarehouse")
		t.Setenv("WA_MLWH_PASSWORD", secret)
		t.Setenv("WA_MLWH_CACHE_PATH", filepath.Join(t.TempDir(), "mlwh.sqlite"))

		seenConfigCh := make(chan resultsServeMLWHConfig, 1)
		resultsServeOpenMLWHClient = func(_ context.Context, cfg resultsServeMLWHConfig) (resultsServeSyncClient, error) {
			seenConfigCh <- cfg

			return &fakeResultsServeSyncClient{}, nil
		}

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd := NewRootCommand()
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"results", "serve", "--port", "0", "--db", ":memory:"})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		<-addrCh
		seenConfig := <-seenConfigCh
		cancel()

		convey.So(<-errCh, convey.ShouldBeNil)
		convey.So(seenConfig.DSN, convey.ShouldEqual, "mlwh_user:"+secret+"@tcp(mlwh-db-ro:3435)/mlwarehouse")
		convey.So(seenConfig.CachePath, convey.ShouldEqual, os.Getenv("WA_MLWH_CACHE_PATH"))
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, secret)
		convey.So(stderr.String(), convey.ShouldNotContainSubstring, secret)
		convey.So(strings.Join(os.Args, " "), convey.ShouldNotContainSubstring, secret)
	})

	convey.Convey("E5.3: Given --mlwh-cache with an embedded password, when results serve parses flags, then the error wraps ErrPasswordInDSN and names --mlwh-cache", t, func() {
		_, err := executeRootCommandForTest(t, []string{"results", "serve", "--db", ":memory:", "--mlwh-cache", "cache_user:secret@tcp(localhost:3306)/wa_cache"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, mlwh.ErrPasswordInDSN.Error())
		convey.So(err.Error(), convey.ShouldContainSubstring, "--mlwh-cache")
	})

	convey.Convey("results serve rejects --mlwh-sync-interval when MLWH is not configured", t, func() {
		t.Setenv("WA_MLWH_DSN", "")
		t.Setenv("WA_MLWH_PASSWORD", "")
		t.Setenv("WA_MLWH_CACHE_PATH", "")
		t.Setenv("WA_MLWH_CACHE_PASSWORD", "")

		output, err := executeRootCommandForTest(t, []string{"results", "serve", "--db", ":memory:", "--mlwh-sync-interval", "5m"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "--mlwh-sync-interval requires MLWH configuration")
	})

	convey.Convey("E5.4: Given --mlwh-sync-interval=5m, when results serve starts, then one sync loop runs and exits before serve returns", t, func() {
		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(mlwh-db-ro:3435)/mlwarehouse")
		t.Setenv("WA_MLWH_PASSWORD", "secret")
		t.Setenv("WA_MLWH_CACHE_PATH", filepath.Join(t.TempDir(), "mlwh.sqlite"))

		fakeClient := &fakeResultsServeSyncClient{}
		resultsServeOpenMLWHClient = func(_ context.Context, _ resultsServeMLWHConfig) (resultsServeSyncClient, error) {
			return fakeClient, nil
		}

		tickerCreated := 0
		intervalCh := make(chan time.Duration, 1)
		fakeTicker := newFakeResultsServeTicker()
		resultsServeNewTicker = func(interval time.Duration) resultsServeTicker {
			tickerCreated++
			intervalCh <- interval

			return fakeTicker
		}

		cmd := NewRootCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"results", "serve", "--port", "0", "--db", ":memory:", "--mlwh-sync-interval", "5m"})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		<-addrCh
		var interval time.Duration
		select {
		case interval = <-intervalCh:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for sync ticker creation")
		}

		convey.So(tickerCreated, convey.ShouldEqual, 1)
		convey.So(interval, convey.ShouldEqual, 5*time.Minute)
		convey.So(fakeClient.SyncCalls(), convey.ShouldEqual, 0)

		fakeTicker.Tick()
		fakeClient.WaitForSyncCall(t)
		convey.So(fakeClient.SyncCalls(), convey.ShouldEqual, 1)

		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)
		fakeTicker.WaitForStop(t)
	})

	convey.Convey("E5.5: Given the default sync interval, when results serve runs, then no MLWH sync loop is started", t, func() {
		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(mlwh-db-ro:3435)/mlwarehouse")
		t.Setenv("WA_MLWH_PASSWORD", "secret")
		t.Setenv("WA_MLWH_CACHE_PATH", filepath.Join(t.TempDir(), "mlwh.sqlite"))

		fakeClient := &fakeResultsServeSyncClient{}
		resultsServeOpenMLWHClient = func(_ context.Context, _ resultsServeMLWHConfig) (resultsServeSyncClient, error) {
			return fakeClient, nil
		}

		tickerCreated := 0
		resultsServeNewTicker = func(_ time.Duration) resultsServeTicker {
			tickerCreated++

			return newFakeResultsServeTicker()
		}

		cmd := NewRootCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"results", "serve", "--port", "0", "--db", ":memory:"})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		<-addrCh
		time.Sleep(20 * time.Millisecond)
		cancel()

		convey.So(<-errCh, convey.ShouldBeNil)
		convey.So(tickerCreated, convey.ShouldEqual, 0)
		convey.So(fakeClient.SyncCalls(), convey.ShouldEqual, 0)
	})
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

func postResultsRegistrationForTest(endpoint string, registration *results.Registration) (*http.Response, error) {
	body, err := json.Marshal(registration)
	if err != nil {
		return nil, err
	}

	var response *http.Response
	for range 20 {
		response, err = http.Post(endpoint, "application/json", bytes.NewReader(body))
		if err == nil {
			return response, nil
		}

		time.Sleep(25 * time.Millisecond)
	}

	return nil, err
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
