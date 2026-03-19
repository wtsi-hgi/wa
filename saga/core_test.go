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
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestClientPing(t *testing.T) {
	Convey("Given a healthy mock server", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		err = client.Ping(context.Background())

		Convey("when Ping is called, then it performs GET / and returns nil", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/")
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

		err = client.Ping(context.Background())

		Convey("when Ping is called, then error is non-nil", func() {
			So(err, ShouldNotBeNil)
		})
	})
}

func TestClientVersion(t *testing.T) {
	Convey("Given a mock server returning a populated revision", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`{"rev":"abc123"}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		version, err := client.Version(context.Background())

		Convey("when Version is called, then it decodes the revision string from GET /version", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/version")
			So(version, ShouldNotBeNil)
			So(version.Rev, ShouldNotBeNil)
			So(*version.Rev, ShouldEqual, "abc123")
		})
	})

	Convey("Given a mock server returning a null revision", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"rev":null}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		version, err := client.Version(context.Background())

		Convey("when Version is called, then it preserves a null revision as nil", func() {
			So(err, ShouldBeNil)
			So(version, ShouldNotBeNil)
			So(version.Rev, ShouldBeNil)
		})
	})
}

func TestClientAuthMe(t *testing.T) {
	Convey("Given a mock server returning the current user", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`{"username":"alice"}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		user, err := client.AuthMe(context.Background())

		Convey("when AuthMe is called, then it decodes the username from GET /auth/me", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/auth/me")
			So(user, ShouldNotBeNil)
			So(user.Username, ShouldEqual, "alice")
		})
	})
}

func TestClientGenerateToken(t *testing.T) {
	Convey("Given a mock server returning a generated token", t, func() {
		var requestedMethod string
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedMethod = r.Method
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`{"token":"new-tok"}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		response, err := client.GenerateToken(context.Background())

		Convey("when GenerateToken is called, then it decodes the token from POST /auth/token", func() {
			So(err, ShouldBeNil)
			So(requestedMethod, ShouldEqual, http.MethodPost)
			So(requestedPath, ShouldEqual, "/auth/token")
			So(response, ShouldNotBeNil)
			So(response.Token, ShouldEqual, "new-tok")
		})
	})
}

func TestClientListUsers(t *testing.T) {
	Convey("Given a mock server returning two users", t, func() {
		var requestedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			_, _ = w.Write([]byte(`[{"id":1,"username":"alice"},{"id":2,"username":"bob"}]`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		users, err := client.ListUsers(context.Background())

		Convey("when ListUsers is called, then it decodes both users from GET /users/", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/users/")
			So(users, ShouldHaveLength, 2)
			So(users[0].Username, ShouldEqual, "alice")
			So(users[1].Username, ShouldEqual, "bob")
		})
	})
}
