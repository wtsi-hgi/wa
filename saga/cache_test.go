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
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCacheKeyUsesDocumentedFormat(t *testing.T) {
	Convey("Given a GET request URL", t, func() {
		key := makeCacheKey(http.MethodGet, "https://example.test/version?name=value")

		Convey("when the cache key is generated, then it uses the documented GET:<fullURL> format", func() {
			So(key, ShouldEqual, "GET:https://example.test/version?name=value")
		})
	})
}

func TestGetResponsesAreCached(t *testing.T) {
	Convey("Given a mock server", t, func() {
		var requests atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		first, err := client.doGet(context.Background(), "/version", url.Values{"name": {"value"}})
		So(err, ShouldBeNil)

		second, err := client.doGet(context.Background(), "/version", url.Values{"name": {"value"}})

		Convey("when the same GET path is called twice, then only one HTTP request is made", func() {
			So(err, ShouldBeNil)
			So(string(first), ShouldEqual, `{"ok":true}`)
			So(string(second), ShouldEqual, `{"ok":true}`)
			So(requests.Load(), ShouldEqual, 1)
		})
	})
}

func TestPostInvalidatesCachedGetResponses(t *testing.T) {
	Convey("Given a cached GET response", t, func() {
		var getRequests atomic.Int32
		var postRequests atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				getRequests.Add(1)
				_, _ = w.Write([]byte(`{"ok":true}`))
			case http.MethodPost:
				postRequests.Add(1)
				w.WriteHeader(http.StatusCreated)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doGet(context.Background(), "/samples", nil)
		So(err, ShouldBeNil)

		_, err = client.doPost(context.Background(), "/samples", map[string]string{"source": "IRODS"})
		So(err, ShouldBeNil)

		_, err = client.doGet(context.Background(), "/samples", nil)

		Convey("when a POST to a related resource is made, then the cached entry is invalidated and the next GET hits the server", func() {
			So(err, ShouldBeNil)
			So(postRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}

func TestDeleteInvalidatesRelatedCachedGetResponses(t *testing.T) {
	Convey("Given a cached collection GET response", t, func() {
		var getRequests atomic.Int32
		var deleteRequests atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Path == "/samples" {
					getRequests.Add(1)
				}

				_, _ = w.Write([]byte(`{"ok":true}`))
			case http.MethodDelete:
				deleteRequests.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doGet(context.Background(), "/samples", nil)
		So(err, ShouldBeNil)

		err = client.doDelete(context.Background(), "/samples/1")
		So(err, ShouldBeNil)

		_, err = client.doGet(context.Background(), "/samples", nil)

		Convey("when a DELETE removes a specific resource, then the related collection GET cache is invalidated", func() {
			So(err, ShouldBeNil)
			So(deleteRequests.Load(), ShouldEqual, 1)
			So(getRequests.Load(), ShouldEqual, 2)
		})
	})
}

func TestPostAndDeleteAreNeverServedFromCache(t *testing.T) {
	Convey("Given POST and DELETE requests", t, func() {
		var postRequests atomic.Int32
		var deleteRequests atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				postRequests.Add(1)
				w.WriteHeader(http.StatusCreated)
			case http.MethodDelete:
				deleteRequests.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		_, err = client.doPost(context.Background(), "/samples", map[string]string{"source": "IRODS"})
		So(err, ShouldBeNil)

		_, err = client.doPost(context.Background(), "/samples", map[string]string{"source": "IRODS"})
		So(err, ShouldBeNil)

		err = client.doDelete(context.Background(), "/samples/1")
		So(err, ShouldBeNil)

		err = client.doDelete(context.Background(), "/samples/1")

		Convey("when POST and DELETE are called repeatedly, then they always hit the server", func() {
			So(err, ShouldBeNil)
			So(postRequests.Load(), ShouldEqual, 2)
			So(deleteRequests.Load(), ShouldEqual, 2)
		})
	})
}
