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
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
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

func TestResultsServeCommand(t *testing.T) {
	originalListen := listenFunc
	defer func() { listenFunc = originalListen }()

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
