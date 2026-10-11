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
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestMLWHServeStartHTTPWaitsForInFlightRequests(t *testing.T) {
	convey.Convey("Given plain mlwh serve with a request in flight, when the serve context is cancelled, then StartHTTP returns only after the handler finishes", t, func() {
		authServer := &mlwhServeGasAuthServer{Server: gas.New(io.Discard)}
		entered := make(chan struct{})
		release := make(chan struct{})
		releaseHandler := sync.OnceFunc(func() { close(release) })
		defer releaseHandler()
		var startHTTPReturned atomic.Bool
		var returnedBeforeHandlerFinished atomic.Bool

		authServer.Router().GET("/slow", func(c *gin.Context) {
			close(entered)
			<-release
			returnedBeforeHandlerFinished.Store(startHTTPReturned.Load())
			c.String(http.StatusOK, "done")
		})

		addr := freeLocalAddrForTest(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		served := make(chan error, 1)
		go func() {
			err := authServer.StartHTTP(ctx, addr)
			startHTTPReturned.Store(true)
			served <- err
		}()

		responded := make(chan error, 1)
		go func() {
			var response *http.Response
			var err error
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				response, err = http.Get("http://" + addr + "/slow") //nolint:noctx
				if err == nil {
					_ = response.Body.Close()

					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			responded <- err
		}()

		handlerReached := false
		select {
		case <-entered:
			handlerReached = true
		case <-time.After(5 * time.Second):
		}
		convey.So(handlerReached, convey.ShouldBeTrue)

		cancel()

		var serveErr error
		returnedEarly := false
		select {
		case serveErr = <-served:
			returnedEarly = true
		case <-time.After(500 * time.Millisecond):
		}

		releaseHandler()

		returned := returnedEarly
		if !returnedEarly {
			select {
			case serveErr = <-served:
				returned = true
			case <-time.After(5 * time.Second):
			}
		}

		convey.So(returned, convey.ShouldBeTrue)

		convey.So(serveErr, convey.ShouldBeNil)
		convey.So(returnedEarly, convey.ShouldBeFalse)
		convey.So(returnedBeforeHandlerFinished.Load(), convey.ShouldBeFalse)
		convey.So(<-responded, convey.ShouldBeNil)
	})
}

func TestMLWHServeKeepsUsageStoreOpenForInFlightRequests(t *testing.T) {
	originalNewAuthServer := mlwhServeNewAuthServer
	t.Cleanup(func() { mlwhServeNewAuthServer = originalNewAuthServer })

	convey.Convey("Given plain mlwh serve with --usage-db and a counted request in flight, when the command context is cancelled, then the command returns only after the request finishes and its usage row is persisted", t, func() {
		entered := make(chan struct{})
		enterOnce := sync.OnceFunc(func() { close(entered) })
		release := make(chan struct{})
		releaseHandler := sync.OnceFunc(func() { close(release) })
		defer releaseHandler()

		mlwhServeNewAuthServer = func(logWriter io.Writer) mlwhServeAuthServer {
			authServer := &mlwhServeGasAuthServer{Server: gas.New(logWriter)}
			authServer.Router().Use(func(c *gin.Context) {
				if c.Request.URL.Path == "/hold" {
					enterOnce()
					<-release
				}

				c.Next()
			})

			return authServer
		}

		stateDir := t.TempDir()
		dbPath := filepath.Join(stateDir, "u.sqlite")
		baseURL, stop := startMLWHServeEndToEndForTest(t, stateDir, "--usage-db", dbPath)

		type holdResponse struct {
			status int
			err    error
		}

		responded := make(chan holdResponse, 1)
		go func() {
			request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/hold", nil)
			if err != nil {
				responded <- holdResponse{err: err}

				return
			}
			request.Header.Set("User-Agent", "wa-cli/x")

			response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
			if err != nil {
				responded <- holdResponse{err: err}

				return
			}
			_ = response.Body.Close()
			responded <- holdResponse{status: response.StatusCode}
		}()

		handlerReached := false
		select {
		case <-entered:
			handlerReached = true
		case <-time.After(5 * time.Second):
		}
		convey.So(handlerReached, convey.ShouldBeTrue)

		stopped := make(chan error, 1)
		go func() { stopped <- stop() }()

		var stopErr error
		returnedEarly := false
		select {
		case stopErr = <-stopped:
			returnedEarly = true
		case <-time.After(500 * time.Millisecond):
		}

		releaseHandler()

		if !returnedEarly {
			stopErr = <-stopped
		}

		var response holdResponse
		select {
		case response = <-responded:
		case <-time.After(5 * time.Second):
			response.err = context.DeadlineExceeded
		}

		convey.So(stopErr, convey.ShouldBeNil)
		convey.So(returnedEarly, convey.ShouldBeFalse)
		convey.So(response.err, convey.ShouldBeNil)
		convey.So(response.status, convey.ShouldEqual, http.StatusNotFound)

		db, err := sql.Open("sqlite", dbPath)
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		var rows, holdRows int
		convey.So(db.QueryRow("SELECT COUNT(*) FROM usage_requests").Scan(&rows), convey.ShouldBeNil)
		convey.So(db.QueryRow("SELECT COUNT(*) FROM usage_requests WHERE path = '/hold'").Scan(&holdRows), convey.ShouldBeNil)
		convey.So(holdRows, convey.ShouldEqual, 1)
		convey.So(rows, convey.ShouldEqual, 1)
	})
}

func TestMLWHDiffServeWaitsForInFlightRequests(t *testing.T) {
	setMLWHDiffMLWHEnvForTest(t)

	originalListen := listenFunc
	defer func() { listenFunc = originalListen }()
	originalOpen := openMLWHDiffClientFunc
	defer func() { openMLWHDiffClientFunc = originalOpen }()

	convey.Convey("Given mlwhdiff serve with a request in flight, when the command context is cancelled, then the command returns and closes the provider only after the handler finishes", t, func() {
		entered := make(chan struct{})
		release := make(chan struct{})
		releaseHandler := sync.OnceFunc(func() { close(release) })
		defer releaseHandler()
		enterOnce := sync.OnceFunc(func() { close(entered) })
		var handlerFinished atomic.Bool
		var commandReturned atomic.Bool
		var commandReturnedBeforeHandlerFinished atomic.Bool
		var closedBeforeHandlerFinished atomic.Bool

		provider := &mlwhdiffMockProvider{
			irodsPathsForSampleFunc: func(_ context.Context, _ string, _, _ int) ([]mlwh.IRODSPath, error) {
				enterOnce()
				<-release
				commandReturnedBeforeHandlerFinished.Store(commandReturned.Load())
				handlerFinished.Store(true)

				return []mlwh.IRODSPath{{IDProduct: "P1"}}, nil
			},
		}
		openMLWHDiffClientFunc = func(_ context.Context, _ mlwhdiffMLWHConfig) (mlwhdiffMLWHHandle, error) {
			return &mlwhdiffTestClient{provider: provider, closeFunc: func() error {
				if !handlerFinished.Load() {
					closedBeforeHandlerFinished.Store(true)
				}

				return nil
			}}, nil
		}

		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		cmd := NewRootCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"mlwhdiff", "serve", "--port", "0", "--db", t.TempDir() + "/mlwhdiff.db"})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		executed := make(chan error, 1)
		go func() {
			err := cmd.ExecuteContext(ctx)
			commandReturned.Store(true)
			executed <- err
		}()

		var addr string
		select {
		case addr = <-addrCh:
		case <-time.After(5 * time.Second):
		}
		convey.So(addr, convey.ShouldNotBeEmpty)

		type slowResponse struct {
			status int
			err    error
		}

		responded := make(chan slowResponse, 1)
		go func() {
			response, err := http.Get("http://" + addr + "/diff/sample/S1") //nolint:noctx
			if err != nil {
				responded <- slowResponse{err: err}

				return
			}
			_ = response.Body.Close()
			responded <- slowResponse{status: response.StatusCode}
		}()

		handlerReached := false
		select {
		case <-entered:
			handlerReached = true
		case <-time.After(5 * time.Second):
		}
		convey.So(handlerReached, convey.ShouldBeTrue)

		cancel()

		var executeErr error
		returnedEarly := false
		select {
		case executeErr = <-executed:
			returnedEarly = true
		case <-time.After(500 * time.Millisecond):
		}

		releaseHandler()

		returned := returnedEarly
		if !returnedEarly {
			select {
			case executeErr = <-executed:
				returned = true
			case <-time.After(5 * time.Second):
			}
		}

		var response slowResponse
		select {
		case response = <-responded:
		case <-time.After(5 * time.Second):
			response.err = context.DeadlineExceeded
		}

		convey.So(returned, convey.ShouldBeTrue)
		convey.So(executeErr, convey.ShouldBeNil)
		convey.So(returnedEarly, convey.ShouldBeFalse)
		convey.So(commandReturnedBeforeHandlerFinished.Load(), convey.ShouldBeFalse)
		convey.So(closedBeforeHandlerFinished.Load(), convey.ShouldBeFalse)
		convey.So(response.err, convey.ShouldBeNil)
		convey.So(response.status, convey.ShouldEqual, http.StatusOK)
	})
}

func TestMLWHServeStartHTTPClosesSlowHeaderConnections(t *testing.T) {
	shortenServeHTTPReadHeaderTimeoutForTest(t)

	convey.Convey("Given plain mlwh serve, when a client never finishes its request header, then the server closes the connection after the read-header timeout", t, func() {
		authServer := &mlwhServeGasAuthServer{Server: gas.New(io.Discard)}
		addr := freeLocalAddrForTest(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		served := make(chan error, 1)
		go func() { served <- authServer.StartHTTP(ctx, addr) }()

		closed, err := slowHeaderConnectionClosedForTest(addr)

		cancel()

		var serveErr error
		returned := false
		select {
		case serveErr = <-served:
			returned = true
		case <-time.After(5 * time.Second):
		}

		convey.So(err, convey.ShouldBeNil)
		convey.So(closed, convey.ShouldBeTrue)
		convey.So(returned, convey.ShouldBeTrue)
		convey.So(serveErr, convey.ShouldBeNil)
	})
}

func TestMLWHDiffServeClosesSlowHeaderConnections(t *testing.T) {
	setMLWHDiffMLWHEnvForTest(t)
	shortenServeHTTPReadHeaderTimeoutForTest(t)

	originalListen := listenFunc
	defer func() { listenFunc = originalListen }()
	originalOpen := openMLWHDiffClientFunc
	defer func() { openMLWHDiffClientFunc = originalOpen }()

	convey.Convey("Given mlwhdiff serve, when a client never finishes its request header, then the server closes the connection after the read-header timeout", t, func() {
		openMLWHDiffClientFunc = func(_ context.Context, _ mlwhdiffMLWHConfig) (mlwhdiffMLWHHandle, error) {
			return &mlwhdiffTestClient{provider: &mlwhdiffMockProvider{}}, nil
		}

		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		cmd := NewRootCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"mlwhdiff", "serve", "--port", "0", "--db", t.TempDir() + "/mlwhdiff.db"})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		executed := make(chan error, 1)
		go func() { executed <- cmd.ExecuteContext(ctx) }()

		var addr string
		select {
		case addr = <-addrCh:
		case <-time.After(5 * time.Second):
		}
		convey.So(addr, convey.ShouldNotBeEmpty)

		closed, err := slowHeaderConnectionClosedForTest(addr)

		cancel()

		var executeErr error
		returned := false
		select {
		case executeErr = <-executed:
			returned = true
		case <-time.After(5 * time.Second):
		}

		convey.So(err, convey.ShouldBeNil)
		convey.So(closed, convey.ShouldBeTrue)
		convey.So(returned, convey.ShouldBeTrue)
		convey.So(executeErr, convey.ShouldBeNil)
	})
}

// shortenServeHTTPReadHeaderTimeoutForTest makes the serve commands' read-header
// timeout short enough to observe in a test, restoring it afterwards.
func shortenServeHTTPReadHeaderTimeoutForTest(t *testing.T) {
	t.Helper()

	original := serveHTTPReadHeaderTimeout
	serveHTTPReadHeaderTimeout = 150 * time.Millisecond

	t.Cleanup(func() { serveHTTPReadHeaderTimeout = original })
}

// slowHeaderConnectionClosedForTest connects to addr, sends a request header
// without its terminating blank line, and reports whether the server closes
// the connection within 2 seconds.
func slowHeaderConnectionClosedForTest(addr string) (bool, error) {
	conn, err := dialWithRetryForTest(addr)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()

	if _, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: x\r\n"); err != nil {
		return false, err
	}

	if err = conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return false, err
	}

	_, err = io.ReadAll(conn)

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false, nil
	}

	return true, nil
}

func dialWithRetryForTest(addr string) (net.Conn, error) {
	var err error

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var conn net.Conn

		conn, err = net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			return conn, nil
		}

		time.Sleep(10 * time.Millisecond)
	}

	return nil, err
}
