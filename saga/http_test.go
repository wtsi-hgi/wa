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

package saga

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestRequestHeaders(t *testing.T) {
	Convey("Given a mock server", t, func() {
		var receivedAPIKey string
		var receivedAuthorization string
		var receivedUserAgent string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAPIKey = r.Header.Get("X-Api-Key")
			receivedAuthorization = r.Header.Get("Authorization")
			receivedUserAgent = r.Header.Get("User-Agent")
			w.WriteHeader(http.StatusOK)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doGet(context.Background(), "/version", url.Values{"name": {"value"}})

		Convey("when a GET request is made, then auth and user-agent headers are sent", func() {
			So(err, ShouldBeNil)
			So(receivedAPIKey, ShouldEqual, "test-key")
			So(receivedAuthorization, ShouldEqual, "Bearer test-key")
			So(receivedUserAgent, ShouldEqual, "wtsi-hgi/wa")
		})
	})

	Convey("Given a client created with a Bearer-prefixed token", t, func() {
		var receivedAPIKey string
		var receivedAuthorization string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAPIKey = r.Header.Get("X-Api-Key")
			receivedAuthorization = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		Reset(server.Close)

		client, err := NewClient("Bearer test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doGet(context.Background(), "/version", nil)

		Convey("when a GET request is made, then the raw API key and bearer auth header are both valid", func() {
			So(err, ShouldBeNil)
			So(receivedAPIKey, ShouldEqual, "test-key")
			So(receivedAuthorization, ShouldEqual, "Bearer test-key")
		})
	})

	Convey("Given a mock server accepting POST requests", t, func() {
		var receivedAPIKey string
		var receivedAuthorization string
		var receivedUserAgent string
		var receivedContentType string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAPIKey = r.Header.Get("X-Api-Key")
			receivedAuthorization = r.Header.Get("Authorization")
			receivedUserAgent = r.Header.Get("User-Agent")
			receivedContentType = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusCreated)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doPost(context.Background(), "/token", map[string]string{"name": "value"})

		Convey("when a POST request is made, then auth, user-agent, and content-type headers are sent", func() {
			So(err, ShouldBeNil)
			So(receivedAPIKey, ShouldEqual, "test-key")
			So(receivedAuthorization, ShouldEqual, "Bearer test-key")
			So(receivedUserAgent, ShouldEqual, "wtsi-hgi/wa")
			So(receivedContentType, ShouldEqual, "application/json")
		})
	})
}

func TestDeleteRequestHeaders(t *testing.T) {
	Convey("Given a mock server accepting DELETE requests", t, func() {
		var receivedAPIKey string
		var receivedAuthorization string
		var receivedUserAgent string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAPIKey = r.Header.Get("X-Api-Key")
			receivedAuthorization = r.Header.Get("Authorization")
			receivedUserAgent = r.Header.Get("User-Agent")
			w.WriteHeader(http.StatusNoContent)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		err = client.doDelete(context.Background(), "/token")

		Convey("when a DELETE request is made, then auth and user-agent headers are sent", func() {
			So(err, ShouldBeNil)
			So(receivedAPIKey, ShouldEqual, "test-key")
			So(receivedAuthorization, ShouldEqual, "Bearer test-key")
			So(receivedUserAgent, ShouldEqual, "wtsi-hgi/wa")
		})
	})
}

func TestRequestURLWithBasePath(t *testing.T) {
	Convey("Given a client configured with a base URL path prefix", t, func() {
		client, err := NewClient("test-key", WithBaseURL("https://example.com/api"))
		So(err, ShouldBeNil)
		Reset(client.Close)

		requestURL, err := client.requestURL("/version", url.Values{"page": {"2"}})

		Convey("when requestURL is called with an absolute-style endpoint path, then it preserves the /api prefix", func() {
			So(err, ShouldBeNil)
			parsedURL, parseErr := url.Parse(requestURL)
			So(parseErr, ShouldBeNil)
			So(parsedURL.Scheme, ShouldEqual, "https")
			So(parsedURL.Host, ShouldEqual, "example.com")
			So(parsedURL.Path, ShouldEqual, "/api/version")
			So(parsedURL.Query().Get("page"), ShouldEqual, "2")
		})
	})

	Convey("Given a client configured with a base URL path prefix and the API root endpoint", t, func() {
		client, err := NewClient("test-key", WithBaseURL("https://example.com/api"))
		So(err, ShouldBeNil)
		Reset(client.Close)

		requestURL, err := client.requestURL("/", nil)

		Convey("when requestURL is called for the API root, then it stays under the /api prefix", func() {
			So(err, ShouldBeNil)
			So(strings.HasPrefix(requestURL, "https://example.com/api"), ShouldBeTrue)
		})
	})
}

func TestAPIErrorHandling(t *testing.T) {
	Convey("Given a mock server returning 401", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad key", http.StatusUnauthorized)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doGet(context.Background(), "/version", nil)

		Convey("when a request is made, then the error wraps ErrUnauthorized", func() {
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrUnauthorized), ShouldBeTrue)

			var apiErr *APIError
			So(errors.As(err, &apiErr), ShouldBeTrue)
			So(apiErr.StatusCode, ShouldEqual, http.StatusUnauthorized)
		})
	})

	Convey("Given a mock server returning 404", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "missing", http.StatusNotFound)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doGet(context.Background(), "/version", nil)

		Convey("when a request is made, then errors.Is matches ErrNotFound", func() {
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrNotFound), ShouldBeTrue)
		})
	})

	Convey("Given a mock server returning 500", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doGet(context.Background(), "/version", nil)

		Convey("when a request is made, then errors.Is matches ErrServerError", func() {
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrServerError), ShouldBeTrue)
		})
	})

	Convey("Given a mock server returning 200", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		body, err := client.doGet(context.Background(), "/version", nil)

		Convey("when a request is made, then error is nil", func() {
			So(err, ShouldBeNil)
			So(string(body), ShouldEqual, `{"ok":true}`)
		})
	})

	Convey("Given an APIError", t, func() {
		err := APIError{StatusCode: http.StatusUnauthorized, Message: "bad key"}

		Convey("when Error is called, then it uses the required format", func() {
			So(err.Error(), ShouldEqual, "saga: HTTP 401: bad key")
		})
	})
}

func TestRetryWithBackoff(t *testing.T) {
	Convey("Given a mock server returning 500 twice then 200", t, func() {
		var attempts atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempt := attempts.Add(1)
			if attempt < 3 {
				http.Error(w, "boom", http.StatusInternalServerError)

				return
			}

			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		body, err := client.doGet(context.Background(), "/version", nil)

		Convey("when a GET request is made, then it retries and returns the successful body", func() {
			So(err, ShouldBeNil)
			So(string(body), ShouldEqual, `{"ok":true}`)
			So(attempts.Load(), ShouldEqual, 3)
		})
	})

	Convey("Given a mock server returning 500 four times", t, func() {
		var attempts atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doGet(context.Background(), "/version", nil)

		Convey("when a GET request is made, then it retries up to the configured limit and returns ErrServerError", func() {
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrServerError), ShouldBeTrue)
			So(attempts.Load(), ShouldEqual, 4)
		})
	})

	Convey("Given a mock server returning 401", t, func() {
		var attempts atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			http.Error(w, "bad key", http.StatusUnauthorized)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doGet(context.Background(), "/version", nil)

		Convey("when a GET request is made, then it does not retry and returns ErrUnauthorized", func() {
			So(err, ShouldNotBeNil)
			So(errors.Is(err, ErrUnauthorized), ShouldBeTrue)
			So(attempts.Load(), ShouldEqual, 1)
		})
	})
}
