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

package mlwh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

// identityRequestForTest is one request seen by an identity recording server.
type identityRequestForTest struct {
	limit  string
	header http.Header
}

// identityRecordingServerForTest answers GET /studies with [] and POST
// /feedback with a receipt, recording each request's headers in order.
type identityRecordingServerForTest struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []identityRequestForTest
}

func newIdentityRecordingServerForTest(t *testing.T) *identityRecordingServerForTest {
	t.Helper()

	recorder := &identityRecordingServerForTest{}
	recorder.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, identityRequestForTest{
			limit:  r.URL.Query().Get("limit"),
			header: r.Header.Clone(),
		})
		recorder.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && r.URL.Path == "/feedback" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1,"created_at":"2026-10-11T00:00:00Z"}`))

			return
		}

		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(recorder.server.Close)

	return recorder
}

func (r *identityRecordingServerForTest) allRequests() []identityRequestForTest {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]identityRequestForTest(nil), r.requests...)
}

func (r *identityRecordingServerForTest) all() []http.Header {
	requests := r.allRequests()
	headers := make([]http.Header, len(requests))

	for i, request := range requests {
		headers[i] = request.header
	}

	return headers
}

func TestRemoteClientIdentityHeadersC1(t *testing.T) {
	convey.Convey("C1.1: Given a UserAgent and config identity, then AllStudies and SubmitFeedback both send them", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{
			BaseURL:   recorder.server.URL,
			UserAgent: "wa-cli/v1",
			Identity:  ClientIdentity{ClientID: "run-a", Username: "alice"},
		})

		_, err := client.AllStudies(context.Background(), 1, 0)
		convey.So(err, convey.ShouldBeNil)

		_, err = client.SubmitFeedback(context.Background(), FeedbackSubmission{Category: "other", Description: "x"})
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 2)

		for _, header := range headers {
			convey.So(header.Get("User-Agent"), convey.ShouldEqual, "wa-cli/v1")
			convey.So(header.Get("X-WA-Client-ID"), convey.ShouldEqual, "run-a")
			convey.So(header.Get("X-WA-Client-User"), convey.ShouldEqual, "alice")
			convey.So(identityMCPHeaderNamesForTest(header), convey.ShouldBeEmpty)
		}
	})

	convey.Convey("C1.2: Given no UserAgent or identity, then Go's User-Agent is sent and no X-WA-* header", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{BaseURL: recorder.server.URL})

		_, err := client.AllStudies(context.Background(), 1, 0)
		convey.So(err, convey.ShouldBeNil)

		_, err = client.SubmitFeedback(context.Background(), FeedbackSubmission{Category: "other", Description: "x"})
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 2)

		for _, header := range headers {
			convey.So(header.Get("User-Agent"), convey.ShouldStartWith, "Go-http-client/")
			convey.So(identityXWAHeaderNamesForTest(header), convey.ShouldBeEmpty)
		}
	})

	convey.Convey("Given a WithLookupProbe context, then only its requests send X-WA-Lookup: probe", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{BaseURL: recorder.server.URL, UserAgent: "wa-cli/v1"})

		_, err := client.AllStudies(WithLookupProbe(context.Background()), 1, 0)
		convey.So(err, convey.ShouldBeNil)

		_, err = client.AllStudies(context.Background(), 2, 0)
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 2)
		convey.So(headers[0].Get(HeaderLookup), convey.ShouldEqual, LookupProbe)
		convey.So(headers[1].Values(HeaderLookup), convey.ShouldBeEmpty)
	})

	convey.Convey("C1.3: Given MCP config and a context identity, when Call runs, then all five headers arrive", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{
			BaseURL:   recorder.server.URL,
			UserAgent: "mlwh-mcp-server/0.5.0",
			Identity:  ClientIdentity{MCPTransport: "stdio"},
		})

		ctx := WithClientIdentity(context.Background(), ClientIdentity{
			ClientID: "s1",
			MCPAgent: "claude-code/2.1.0",
			MCPTool:  "mlwh_list_studies",
		})

		_, err := client.Call(ctx, "AllStudies", nil, nil)
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 1)
		convey.So(headers[0].Get("User-Agent"), convey.ShouldEqual, "mlwh-mcp-server/0.5.0")
		// Literal names pin the wire contract independently of the constants.
		convey.So(headers[0].Get("X-WA-Client-ID"), convey.ShouldEqual, "s1")
		convey.So(headers[0].Get("X-WA-MCP-Transport"), convey.ShouldEqual, "stdio")
		convey.So(headers[0].Get("X-WA-MCP-Agent"), convey.ShouldEqual, "claude-code/2.1.0")
		convey.So(headers[0].Get("X-WA-MCP-Tool"), convey.ShouldEqual, "mlwh_list_studies")
		convey.So(headers[0].Values("X-WA-Client-User"), convey.ShouldBeEmpty)
	})

	convey.Convey("C1.4: Given two concurrent calls with different tools, then each request carries its own tool", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{BaseURL: recorder.server.URL})

		// The limit query parameter tells the two requests apart server-side.
		tools := map[int]string{1: "t1", 2: "t2"}

		var wg sync.WaitGroup

		errs := make([]error, 0, len(tools))

		var errsMu sync.Mutex

		for limit, tool := range tools {
			wg.Go(func() {
				ctx := WithClientIdentity(context.Background(), ClientIdentity{MCPTool: tool})

				_, err := client.AllStudies(ctx, limit, 0)

				errsMu.Lock()
				errs = append(errs, err)
				errsMu.Unlock()
			})
		}

		wg.Wait()

		convey.So(errs, convey.ShouldResemble, []error{nil, nil})

		byLimit := map[string]string{}
		for _, request := range recorder.allRequests() {
			byLimit[request.limit] = request.header.Get(HeaderMCPTool)
		}

		convey.So(byLimit, convey.ShouldResemble, map[string]string{"1": "t1", "2": "t2"})
	})

	convey.Convey("C1.5: Given config Username alice, a context Username overrides it and an empty one keeps it", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{
			BaseURL:  recorder.server.URL,
			Identity: ClientIdentity{Username: "alice"},
		})

		bob := WithClientIdentity(context.Background(), ClientIdentity{Username: "bob"})
		_, err := client.AllStudies(bob, 1, 0)
		convey.So(err, convey.ShouldBeNil)

		empty := WithClientIdentity(context.Background(), ClientIdentity{Username: ""})
		_, err = client.AllStudies(empty, 1, 0)
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 2)
		convey.So(headers[0].Get(HeaderClientUser), convey.ShouldEqual, "bob")
		convey.So(headers[1].Get(HeaderClientUser), convey.ShouldEqual, "alice")
	})

	convey.Convey("A nested WithClientIdentity replaces the outer identity", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{BaseURL: recorder.server.URL})

		outer := WithClientIdentity(context.Background(), ClientIdentity{ClientID: "outer", MCPTool: "t-outer"})
		inner := WithClientIdentity(outer, ClientIdentity{MCPTool: "t-inner"})

		_, err := client.AllStudies(inner, 1, 0)
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 1)
		convey.So(headers[0].Get(HeaderMCPTool), convey.ShouldEqual, "t-inner")
		convey.So(headers[0].Values(HeaderClientID), convey.ShouldBeEmpty)
	})

	convey.Convey("C1.6: Given non-printable bytes and an oversized ClientID, then values are sanitised and capped", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{BaseURL: recorder.server.URL})

		ctx := WithClientIdentity(context.Background(), ClientIdentity{
			Username: "caf\xc3\xa9\n",
			ClientID: strings.Repeat("c", 200),
		})

		_, err := client.AllStudies(ctx, 1, 0)
		convey.So(err, convey.ShouldBeNil)

		_, err = client.SubmitFeedback(ctx, FeedbackSubmission{Category: "other", Description: "x"})
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 2)

		for _, header := range headers {
			convey.So(header.Get(HeaderClientUser), convey.ShouldEqual, "caf???")
			convey.So(header.Get(HeaderClientID), convey.ShouldEqual, strings.Repeat("c", UsageMaxClientIDBytes))
		}
	})

	convey.Convey("Spaces and ~ are kept, while tab, 0x1f and DEL become ? and the calls still succeed", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{BaseURL: recorder.server.URL})

		ctx := WithClientIdentity(context.Background(), ClientIdentity{Username: "a b\x7f\t\x1fc~"})

		_, err := client.AllStudies(ctx, 1, 0)
		convey.So(err, convey.ShouldBeNil)

		_, err = client.SubmitFeedback(ctx, FeedbackSubmission{Category: "other", Description: "x"})
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 2)

		for _, header := range headers {
			convey.So(header.Get(HeaderClientUser), convey.ShouldEqual, "a b???c~")
		}
	})

	convey.Convey("Given every identity field set in both config and context, then each header carries the context value", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{
			BaseURL: recorder.server.URL,
			Identity: ClientIdentity{
				ClientID:     "cfg-id",
				Username:     "cfg-user",
				MCPTransport: "cfg-transport",
				MCPAgent:     "cfg-agent",
				MCPTool:      "cfg-tool",
			},
		})

		ctx := WithClientIdentity(context.Background(), ClientIdentity{
			ClientID:     "ctx-id",
			Username:     "ctx-user",
			MCPTransport: "ctx-transport",
			MCPAgent:     "ctx-agent",
			MCPTool:      "ctx-tool",
		})

		_, err := client.AllStudies(ctx, 1, 0)
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 1)
		convey.So(headers[0].Get(HeaderClientID), convey.ShouldEqual, "ctx-id")
		convey.So(headers[0].Get(HeaderClientUser), convey.ShouldEqual, "ctx-user")
		convey.So(headers[0].Get(HeaderMCPTransport), convey.ShouldEqual, "ctx-transport")
		convey.So(headers[0].Get(HeaderMCPAgent), convey.ShouldEqual, "ctx-agent")
		convey.So(headers[0].Get(HeaderMCPTool), convey.ShouldEqual, "ctx-tool")
	})

	convey.Convey("Each MCP field is cut to its own cap", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{
			BaseURL: recorder.server.URL,
			Identity: ClientIdentity{
				Username:     strings.Repeat("u", 300),
				MCPTransport: strings.Repeat("t", 300),
				MCPAgent:     strings.Repeat("a", 300),
				MCPTool:      strings.Repeat("o", 300),
			},
		})

		_, err := client.AllStudies(context.Background(), 1, 0)
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 1)
		convey.So(len(headers[0].Get(HeaderClientUser)), convey.ShouldEqual, UsageMaxUsernameBytes)
		convey.So(len(headers[0].Get(HeaderMCPTransport)), convey.ShouldEqual, UsageMaxMCPTransportBytes)
		convey.So(len(headers[0].Get(HeaderMCPAgent)), convey.ShouldEqual, UsageMaxMCPAgentBytes)
		convey.So(len(headers[0].Get(HeaderMCPTool)), convey.ShouldEqual, UsageMaxMCPToolBytes)
	})

	convey.Convey("A ClientID one byte over its cap arrives cut to exactly the cap", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{
			BaseURL:  recorder.server.URL,
			Identity: ClientIdentity{ClientID: strings.Repeat("c", UsageMaxClientIDBytes+1)},
		})

		_, err := client.AllStudies(context.Background(), 1, 0)
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 1)
		convey.So(headers[0].Get(HeaderClientID), convey.ShouldEqual, strings.Repeat("c", UsageMaxClientIDBytes))
	})

	convey.Convey("C1.7: Given a 600-byte UserAgent, then 512 bytes are sent", t, func() {
		recorder := newIdentityRecordingServerForTest(t)
		client := newIdentityClientForTest(t, RemoteConfig{
			BaseURL:   recorder.server.URL,
			UserAgent: strings.Repeat("u", 600),
		})

		_, err := client.AllStudies(context.Background(), 1, 0)
		convey.So(err, convey.ShouldBeNil)

		headers := recorder.all()
		convey.So(headers, convey.ShouldHaveLength, 1)
		convey.So(headers[0].Get("User-Agent"), convey.ShouldEqual, strings.Repeat("u", UsageMaxUserAgentBytes))
	})

	convey.Convey("C1.8: Given the real server with usage enabled, then AllStudies stores one cli row for run-a", t, func() {
		gin.SetMode(gin.TestMode)

		store, path := openUsageServerTestStore(t)
		router := gin.New()
		NewServer(&feedbackStudiesQueryerForTest{}, WithUsage(store, []byte(usageTestAdminToken))).
			RegisterRoutes(router, nil)

		server := httptest.NewServer(router)
		t.Cleanup(server.Close)

		client := newIdentityClientForTest(t, RemoteConfig{
			BaseURL:   server.URL,
			UserAgent: "wa-cli/v1",
			Identity:  ClientIdentity{ClientID: "run-a"},
		})

		studies, err := client.AllStudies(context.Background(), 1, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(studies, convey.ShouldHaveLength, 1)

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].Category, convey.ShouldEqual, string(UsageCategoryCLI))
		convey.So(rows[0].ClientID, convey.ShouldEqual, "run-a")
		convey.So(rows[0].ClientVersion, convey.ShouldEqual, "v1")
	})
}

func newIdentityClientForTest(t *testing.T, cfg RemoteConfig) *RemoteClient {
	t.Helper()

	client, err := NewRemoteClient(cfg)
	convey.So(err, convey.ShouldBeNil)
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func identityMCPHeaderNamesForTest(header http.Header) []string {
	return identityHeaderNamesWithPrefixForTest(header, "X-Wa-Mcp-")
}

func identityHeaderNamesWithPrefixForTest(header http.Header, prefix string) []string {
	names := []string{}

	for name := range header {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}

	return names
}

// identityXWAHeaderNamesForTest returns the canonical names of every X-WA-*
// header in header.
func identityXWAHeaderNamesForTest(header http.Header) []string {
	return identityHeaderNamesWithPrefixForTest(header, "X-Wa-")
}
