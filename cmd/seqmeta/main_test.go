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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
	"github.com/wtsi-hgi/wa/seqmeta"
)

func TestDiffCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/integrations/mlwh/samples":
			_, _ = w.Write([]byte(`{"items":[{"sanger_id":"S1"},{"sanger_id":"S2"}],"total":2,"offset":0,"limit":100}`))
		case "/integrations/irods/samples/ABC":
			_, _ = w.Write([]byte(`{"items":[{"collection":"/abc"}],"total":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	convey.Convey("F1: diff subcommand prints JSON output", t, func() {
		var stderr *bytes.Buffer

		stdout, _, err := executeCommand(t, []string{"diff", "--study", "100", "--db", t.TempDir() + "/seqmeta.db", "--token", "test", "--base-url", server.URL})
		convey.So(err, convey.ShouldBeNil)

		var result seqmeta.DiffResult[saga.MLWHSample]
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)

		stdout, _, err = executeCommand(t, []string{"diff", "--sample", "ABC", "--db", t.TempDir() + "/seqmeta.db", "--token", "test", "--base-url", server.URL})
		convey.So(err, convey.ShouldBeNil)

		var fileResult seqmeta.DiffResult[saga.IRODSFile]
		convey.So(json.Unmarshal(stdout.Bytes(), &fileResult), convey.ShouldBeNil)
		convey.So(fileResult.Added, convey.ShouldHaveLength, 1)

		_, stderr, err = executeCommand(t, []string{"diff", "--token", "test", "--base-url", server.URL})
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "usage")

		_, _, err = executeCommand(t, []string{"diff", "--study", "100", "--sample", "ABC", "--token", "test", "--base-url", server.URL})
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func TestValidateCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/integrations/mlwh/studies/6568":
			_, _ = w.Write([]byte(`{"id_study_lims":"6568","name":"HCA"}`))
		case "/integrations/mlwh/studies/unknown_id":
			http.NotFound(w, r)
		case "/integrations/mlwh/studies":
			_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
		case "/integrations/mlwh/samples":
			_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
		case "/projects/":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	convey.Convey("F2: validate subcommand prints JSON and errors on bad input", t, func() {
		stdout, _, err := executeCommand(t, []string{"validate", "6568", "--token", "test", "--base-url", server.URL})
		convey.So(err, convey.ShouldBeNil)

		var result seqmeta.IdentifierResult
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, seqmeta.IdentifierStudyID)
		convey.So(result.Object, convey.ShouldNotBeNil)

		_, stderr, err := executeCommand(t, []string{"validate", "unknown_id", "--token", "test", "--base-url", server.URL})
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "unknown identifier")

		_, stderr, err = executeCommand(t, []string{"validate", "--token", "test", "--base-url", server.URL})
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "usage")
	})
}

func TestServeCommand(t *testing.T) {
	originalListen := listenFunc
	defer func() { listenFunc = originalListen }()

	mockSaga := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/integrations/mlwh/studies/6568":
			_, _ = w.Write([]byte(`{"id_study_lims":"6568","name":"HCA"}`))
		case "/integrations/mlwh/studies":
			_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
		case "/integrations/mlwh/samples":
			_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
		case "/projects/":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockSaga.Close()

	convey.Convey("F3: serve subcommand starts the HTTP API", t, func() {
		addrCh := make(chan string, 1)
		requestedAddr := ""
		listenFunc = func(network, addr string) (net.Listener, error) {
			requestedAddr = addr
			listener, err := net.Listen(network, "127.0.0.1:0")
			if err == nil {
				addrCh <- listener.Addr().String()
			}

			return listener, err
		}

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd := newRootCommand()
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"serve", "--port", "0", "--db", t.TempDir() + "/seqmeta.db", "--token", "test", "--base-url", mockSaga.URL})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		addr := <-addrCh
		var response *http.Response
		var err error
		for attempt := 0; attempt < 20; attempt++ {
			response, err = http.Get("http://" + addr + "/validate/6568")
			if err == nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = response.Body.Close() }()
		convey.So(response.StatusCode, convey.ShouldEqual, http.StatusOK)

		var result seqmeta.IdentifierResult
		convey.So(json.NewDecoder(response.Body).Decode(&result), convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, seqmeta.IdentifierStudyID)
		convey.So(result.Object, convey.ShouldNotBeNil)

		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)

		cmd = newRootCommand()
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"serve", "--db", t.TempDir() + "/seqmeta.db", "--token", "test", "--base-url", mockSaga.URL})
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
		errCh = make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()
		<-addrCh
		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)
		convey.So(requestedAddr, convey.ShouldEqual, ":8080")

		_, _, err = executeCommand(t, []string{"serve", "--port", "abc", "--token", "test", "--base-url", mockSaga.URL})
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func executeCommand(t *testing.T, args []string) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := newRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return stdout, stderr, err
}
