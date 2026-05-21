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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	gas "github.com/wtsi-hgi/go-authserver"
)

func TestResultsDeleteCommand(t *testing.T) {
	convey.Convey("C3 CLI: Given a valid ID, when delete <id> is run, then it calls the authenticated owner-protected endpoint", t, func() {
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				handlerErrCh <- errors.New("unexpected request method")
				http.NotFound(w, r)

				return
			}

			if r.URL.Path != gas.EndPointAuth+"/results/result-123" {
				handlerErrCh <- errors.New("unexpected request path")
				http.NotFound(w, r)

				return
			}

			w.WriteHeader(http.StatusNoContent)
			handlerErrCh <- nil
		}))
		defer server.Close()

		output, err := executeRootCommandForTest(t, []string{"results", "delete", "--server", server.URL, "result-123"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldBeBlank)
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
	})

	convey.Convey("G4.2: Given non-existent ID, then exit code is non-zero", t, func() {
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				handlerErrCh <- errors.New("unexpected request method")
				http.NotFound(w, r)

				return
			}

			if r.URL.Path != gas.EndPointAuth+"/results/missing-id" {
				handlerErrCh <- errors.New("unexpected request path")
				http.NotFound(w, r)

				return
			}

			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			handlerErrCh <- nil
		}))
		defer server.Close()

		_, err := executeRootCommandForTest(t, []string{"results", "delete", "--server", server.URL, "missing-id"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
	})
}
