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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

const (
	usageTestAdminToken = "usage-admin-token"
	usageTestRemoteHost = "192.0.2.1"
)

func TestUsageMiddlewareB1(t *testing.T) {
	convey.Convey("a counted Registry request stores one row with the caller identity", t, func() {
		router, store, path := newUsageTestRouter(t, &feedbackStudiesQueryerForTest{})

		response := usageRequestForTest(router, http.MethodGet, "/studies", map[string]string{
			"User-Agent":       "wa-cli/v0.11.0",
			"X-WA-Client-ID":   "run-a",
			"X-WA-Client-User": "alice",
		})

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)

		row := rows[0]
		convey.So(row.Category, convey.ShouldEqual, string(UsageCategoryCLI))
		convey.So(row.Method, convey.ShouldEqual, http.MethodGet)
		convey.So(row.Route, convey.ShouldEqual, "/studies")
		convey.So(row.Path, convey.ShouldEqual, "")
		convey.So(row.ClientID, convey.ShouldEqual, "run-a")
		convey.So(row.Username, convey.ShouldEqual, "alice")
		convey.So(row.RemoteAddr, convey.ShouldEqual, usageTestRemoteHost)
		convey.So(row.ClientVersion, convey.ShouldEqual, "v0.11.0")
		convey.So(row.UserAgent, convey.ShouldEqual, "wa-cli/v0.11.0")
		convey.So(row.Status, convey.ShouldEqual, http.StatusOK)
		convey.So(row.LatencyUS, convey.ShouldBeGreaterThanOrEqualTo, 0)
	})

	convey.Convey("a parameterised route stores its template, not the path", t, func() {
		router, store, path := newUsageTestRouter(t, classifyQueryerForUsageTest(nil))

		usageRequestForTest(router, http.MethodGet, "/classify/ABC?x=1", nil)

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].Route, convey.ShouldEqual, "/classify/:id")
		convey.So(rows[0].Path, convey.ShouldEqual, "")
	})

	convey.Convey("an unmatched path is counted with its path and gin's 404 body", t, func() {
		router, store, path := newUsageTestRouter(t, &serverFakeQueryer{})

		response := usageRequestForTest(router, http.MethodGet, "/no/such?id=secret", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(response.Body.String(), convey.ShouldEqual, "404 page not found")

		long := "/" + strings.Repeat("p", 299)
		usageRequestForTest(router, http.MethodGet, long, nil)

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 2)
		convey.So(rows[0].Route, convey.ShouldEqual, "")
		convey.So(rows[0].Path, convey.ShouldEqual, "/no/such")
		convey.So(rows[0].Status, convey.ShouldEqual, http.StatusNotFound)
		convey.So(rows[1].Path, convey.ShouldEqual, long[:UsageMaxPathBytes])
	})

	convey.Convey("a wrong method on a known path is counted as unmatched", t, func() {
		router, store, path := newUsageTestRouter(t, &serverFakeQueryer{})

		usageRequestForTest(router, http.MethodPost, "/studies", nil)

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].Method, convey.ShouldEqual, http.MethodPost)
		convey.So(rows[0].Route, convey.ShouldEqual, "")
		convey.So(rows[0].Path, convey.ShouldEqual, "/studies")
		convey.So(rows[0].Status, convey.ShouldEqual, http.StatusNotFound)
	})

	convey.Convey("a matched route's 404 is counted under its template", t, func() {
		router, store, path := newUsageTestRouter(t, classifyQueryerForUsageTest(ErrNotFound))

		response := usageRequestForTest(router, http.MethodGet, "/classify/X", nil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].Route, convey.ShouldEqual, "/classify/:id")
		convey.So(rows[0].Status, convey.ShouldEqual, http.StatusNotFound)
	})

	convey.Convey("health, the usage summary, and the feedback admin list are not counted", t, func() {
		router, store, path := newUsageTestRouter(t, &serverFakeQueryer{},
			WithFeedback(newFeedbackServerTestStore(t), []byte(usageTestAdminToken)))

		health := usageRequestForTest(router, http.MethodGet, "/health", nil)
		convey.So(health.Code, convey.ShouldEqual, http.StatusOK)

		summary := usageRequestForTest(router, http.MethodGet, "/usage", map[string]string{
			"Authorization": "Bearer " + usageTestAdminToken,
		})
		convey.So(summary.Code, convey.ShouldEqual, http.StatusOK)

		list := usageRequestForTest(router, http.MethodGet, "/feedback", map[string]string{
			"Authorization": "Bearer " + usageTestAdminToken,
		})
		convey.So(list.Code, convey.ShouldEqual, http.StatusOK)

		convey.So(flushedUsageRowsForTest(store, path), convey.ShouldBeEmpty)
	})

	convey.Convey("the OpenAPI document and feedback submissions are counted", t, func() {
		router, store, path := newUsageTestRouter(t, &serverFakeQueryer{},
			WithFeedback(newFeedbackServerTestStore(t), []byte(usageTestAdminToken)))

		openapi := usageRequestForTest(router, http.MethodGet, "/openapi.json", nil)
		convey.So(openapi.Code, convey.ShouldEqual, http.StatusOK)

		submit := postFeedbackForTest(router, `{"category":"other","description":"x"}`, func(r *http.Request) {
			r.RemoteAddr = usageTestRemoteHost + ":1234"
		})
		convey.So(submit.Code, convey.ShouldEqual, http.StatusCreated)

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 2)
		convey.So(rows[0].Route, convey.ShouldEqual, "/openapi.json")
		convey.So(rows[1].Method, convey.ShouldEqual, http.MethodPost)
		convey.So(rows[1].Route, convey.ShouldEqual, "/feedback")
		convey.So(rows[1].Status, convey.ShouldEqual, http.StatusCreated)
	})

	convey.Convey("run-dev User-Agents are not counted and unknown ones are other", t, func() {
		router, store, path := newUsageTestRouter(t, &feedbackStudiesQueryerForTest{})

		for _, ua := range []string{"wa-run-dev", "wa-run-dev/1"} {
			for _, target := range []string{"/studies", "/bogus"} {
				usageRequestForTest(router, http.MethodGet, target, map[string]string{"User-Agent": ua})
			}
		}

		convey.So(flushedUsageRowsForTest(store, path), convey.ShouldBeEmpty)

		usageRequestForTest(router, http.MethodGet, "/studies", map[string]string{"User-Agent": "curl/8.5.0"})

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].Category, convey.ShouldEqual, string(UsageCategoryOther))
	})

	convey.Convey("MCP headers are stored with the mcp category", t, func() {
		router, store, path := newUsageTestRouter(t, &feedbackStudiesQueryerForTest{})

		usageRequestForTest(router, http.MethodGet, "/studies", map[string]string{
			"User-Agent":         "mlwh-mcp-server/0.5.0",
			"X-WA-MCP-Transport": "stdio",
			"X-WA-MCP-Agent":     "claude-code/2.1.0",
			"X-WA-MCP-Tool":      "mlwh_list_studies",
		})

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].Category, convey.ShouldEqual, string(UsageCategoryMCP))
		convey.So(rows[0].MCPTransport, convey.ShouldEqual, "stdio")
		convey.So(rows[0].MCPAgent, convey.ShouldEqual, "claude-code/2.1.0")
		convey.So(rows[0].MCPTool, convey.ShouldEqual, "mlwh_list_studies")
	})

	convey.Convey("the socket address is stored, never X-Forwarded-For", t, func() {
		router, store, path := newUsageTestRouter(t, &feedbackStudiesQueryerForTest{})

		usageRequestForTest(router, http.MethodGet, "/studies", map[string]string{"X-Forwarded-For": "203.0.113.9"})

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].RemoteAddr, convey.ShouldEqual, usageTestRemoteHost)
	})

	convey.Convey("an over-long client ID is stored cut to its cap", t, func() {
		router, store, path := newUsageTestRouter(t, &feedbackStudiesQueryerForTest{})

		usageRequestForTest(router, http.MethodGet, "/studies", map[string]string{
			"X-WA-Client-ID": strings.Repeat("c", 200),
		})

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].ClientID, convey.ShouldEqual, strings.Repeat("c", UsageMaxClientIDBytes))
	})

	convey.Convey("secured-mode requests store the template, and auth rejections are not counted", t, func() {
		store, path := openUsageServerTestStore(t)

		router := newSecuredUsageRouterForTest(store, func(*gin.Context) {})
		response := usageRequestForTest(router, http.MethodGet, "/rest/v1/auth/studies", nil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].Route, convey.ShouldEqual, "/studies")

		rejecting := newSecuredUsageRouterForTest(store, func(c *gin.Context) {
			c.AbortWithStatus(http.StatusUnauthorized)
		})
		rejected := usageRequestForTest(rejecting, http.MethodGet, "/rest/v1/auth/studies", nil)
		convey.So(rejected.Code, convey.ShouldEqual, http.StatusUnauthorized)

		convey.So(flushedUsageRowsForTest(store, path), convey.ShouldHaveLength, 1)
	})

	convey.Convey("usage disabled installs no middleware and keeps gin's 404", t, func() {
		gin.SetMode(gin.TestMode)

		// gin.New has no recovery, so a middleware touching the nil store
		// would panic out of ServeHTTP.
		router := gin.New()
		NewServer(&serverFakeQueryer{}, WithUsage(nil, nil)).RegisterRoutes(router, nil)

		response := usageRequestForTest(router, http.MethodGet, "/bogus", nil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(response.Body.String(), convey.ShouldEqual, "404 page not found")
	})

	convey.Convey("a broken store never changes the response", t, func() {
		router, store, _ := newUsageTestRouter(t, &feedbackStudiesQueryerForTest{})
		convey.So(store.db.Close(), convey.ShouldBeNil)

		gin.SetMode(gin.TestMode)

		disabled := gin.New()
		NewServer(&feedbackStudiesQueryerForTest{}, WithUsage(nil, nil)).RegisterRoutes(disabled, nil)

		want := usageRequestForTest(disabled, http.MethodGet, "/studies", nil)
		convey.So(want.Code, convey.ShouldEqual, http.StatusOK)

		mismatches := 0

		for range 10 {
			got := usageRequestForTest(router, http.MethodGet, "/studies", nil)
			if got.Code != want.Code || got.Body.String() != want.Body.String() {
				mismatches++
			}
		}

		convey.So(mismatches, convey.ShouldEqual, 0)
	})
}

func TestUsageAdminRouteB2(t *testing.T) {
	adminAuth := map[string]string{"Authorization": "Bearer " + usageTestAdminToken}

	convey.Convey("usage disabled answers 503 usage_disabled with or without a token", t, func() {
		router := newUsageDisabledTestRouter()

		for _, headers := range []map[string]string{nil, adminAuth} {
			response := usageRequestForTest(router, http.MethodGet, "/usage", headers)
			convey.So(response.Code, convey.ShouldEqual, http.StatusServiceUnavailable)
			convey.So(response.Body.String(), convey.ShouldEqual,
				`{"code":"usage_disabled","message":"usage tracking is disabled on this server"}`)
		}
	})

	convey.Convey("a missing, wrong, or non-Bearer token answers 401 unauthorized", t, func() {
		router, _, _ := newUsageTestRouter(t, &serverFakeQueryer{})

		for _, headers := range []map[string]string{
			nil,
			{"Authorization": "Bearer wrong"},
			{"Authorization": "Basic " + usageTestAdminToken},
		} {
			response := usageRequestForTest(router, http.MethodGet, "/usage", headers)
			convey.So(response.Code, convey.ShouldEqual, http.StatusUnauthorized)
			convey.So(response.Body.String(), convey.ShouldEqual,
				`{"code":"unauthorized","message":"admin token required"}`)
		}
	})

	convey.Convey("the admin token returns the 7d summary including just-recorded requests", t, func() {
		router, _, _ := newUsageTestRouter(t, &feedbackStudiesQueryerForTest{})

		for range 2 {
			usageRequestForTest(router, http.MethodGet, "/studies", nil)
		}

		response := usageRequestForTest(router, http.MethodGet, "/usage", adminAuth)
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		summary := decodeUsageSummaryForTest(response)
		convey.So(summary.Window, convey.ShouldEqual, UsageWindow7d)
		convey.So(summary.Totals.Requests, convey.ShouldEqual, 2)
	})

	convey.Convey("each valid window is echoed and an empty window means 7d", t, func() {
		router, _, _ := newUsageTestRouter(t, &serverFakeQueryer{})

		for query, want := range map[string]UsageWindow{
			"?window=24h": UsageWindow24h,
			"?window=30d": UsageWindow30d,
			"?window=all": UsageWindowAll,
			"?window=":    UsageWindow7d,
		} {
			response := usageRequestForTest(router, http.MethodGet, "/usage"+query, adminAuth)
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(decodeUsageSummaryForTest(response).Window, convey.ShouldEqual, want)
		}
	})

	convey.Convey("an unknown window answers 400 bad_request naming the value", t, func() {
		router, _, _ := newUsageTestRouter(t, &serverFakeQueryer{})

		response := usageRequestForTest(router, http.MethodGet, "/usage?window=1h", adminAuth)
		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
		convey.So(response.Body.String(), convey.ShouldEqual,
			`{"code":"bad_request","message":"invalid window \"1h\""}`)
	})

	convey.Convey("a closed store answers 500 internal_error", t, func() {
		router, store, _ := newUsageTestRouter(t, &serverFakeQueryer{})
		convey.So(store.Close(), convey.ShouldBeNil)

		response := usageRequestForTest(router, http.MethodGet, "/usage", adminAuth)
		convey.So(response.Code, convey.ShouldEqual, http.StatusInternalServerError)
		convey.So(response.Body.String(), convey.ShouldEqual,
			`{"code":"internal_error","message":"could not summarise usage"}`)
	})

	convey.Convey("usage works without feedback, which stays disabled", t, func() {
		router, _, _ := newUsageTestRouter(t, &serverFakeQueryer{}, WithFeedback(nil, nil))

		usage := usageRequestForTest(router, http.MethodGet, "/usage", adminAuth)
		convey.So(usage.Code, convey.ShouldEqual, http.StatusOK)

		feedback := usageRequestForTest(router, http.MethodGet, "/feedback", adminAuth)
		convey.So(feedback.Code, convey.ShouldEqual, http.StatusServiceUnavailable)
		convey.So(decodeErrorEnvelopeForUsageTest(feedback).Code, convey.ShouldEqual, httpErrorCodeFeedbackDisabled)
	})
}

// newUsageDisabledTestRouter returns a plain-mode router for a server with
// usage disabled.
func newUsageDisabledTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)

	return newUsageServerRouterForTest(NewServer(&serverFakeQueryer{}, WithUsage(nil, nil)))
}

func TestUsageRoutePlacementB3(t *testing.T) {
	convey.Convey("the Registry keeps its 90 entries and none has path /usage", t, func() {
		convey.So(len(Registry), convey.ShouldEqual, 90)

		var usagePaths []string

		for _, entry := range Registry {
			if strings.HasPrefix(entry.Path, "/usage") {
				usagePaths = append(usagePaths, entry.Path)
			}
		}

		convey.So(usagePaths, convey.ShouldBeEmpty)
	})

	convey.Convey("the OpenAPI document has no /usage path", t, func() {
		paths := openAPIPathsForTest(t, decodedOpenAPIDocForTest(t))

		convey.So(paths, convey.ShouldNotContainKey, "/usage")
	})

	convey.Convey("secured mode serves GET /usage on the root router, not behind the auth group", t, func() {
		store, path := openUsageServerTestStore(t)
		router := newSecuredUsageRouterForTest(store, func(*gin.Context) {})
		adminAuth := map[string]string{"Authorization": "Bearer " + usageTestAdminToken}

		root := usageRequestForTest(router, http.MethodGet, "/usage", adminAuth)
		convey.So(root.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(decodeUsageSummaryForTest(root).Window, convey.ShouldEqual, UsageWindow7d)

		grouped := usageRequestForTest(router, http.MethodGet, "/rest/v1/auth/usage", adminAuth)
		convey.So(grouped.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(grouped.Body.String(), convey.ShouldEqual, "404 page not found")

		rows := flushedUsageRowsForTest(store, path)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].Route, convey.ShouldEqual, "")
		convey.So(rows[0].Path, convey.ShouldEqual, "/rest/v1/auth/usage")
	})

	convey.Convey("plain mode with usage disabled still registers GET /usage", t, func() {
		gin.SetMode(gin.TestMode)

		router := gin.New()
		NewServer(&serverFakeQueryer{}).RegisterRoutes(router, nil)

		response := usageRequestForTest(router, http.MethodGet, "/usage", nil)
		convey.So(response.Code, convey.ShouldEqual, http.StatusServiceUnavailable)
		convey.So(decodeErrorEnvelopeForUsageTest(response).Code, convey.ShouldEqual, httpErrorCodeUsageDisabled)
	})
}

func classifyQueryerForUsageTest(err error) *serverFakeQueryer {
	return &serverFakeQueryer{
		classifyIdentifierFunc: func(context.Context, string) (Match, error) {
			return Match{}, err
		},
	}
}

// newUsageTestRouter returns a plain-mode router for a server over q with
// usage enabled, plus the store and its file path.
func newUsageTestRouter(t *testing.T, q Queryer, opts ...ServerOption) (*gin.Engine, *UsageStore, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	store, path := openUsageServerTestStore(t)

	opts = append(opts, WithUsage(store, []byte(usageTestAdminToken)))

	return newUsageServerRouterForTest(NewServer(q, opts...)), store, path
}

// newSecuredUsageRouterForTest lays routes out as secured mode does, with
// authMiddleware standing in for the gas JWT check on the auth group.
func newSecuredUsageRouterForTest(store *UsageStore, authMiddleware gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	auth := router.Group("/rest/v1/auth", authMiddleware)

	NewServer(&feedbackStudiesQueryerForTest{}, WithUsage(store, []byte(usageTestAdminToken))).
		RegisterRoutes(router, auth)

	return router
}

// usageRequestForTest serves one request from usageTestRemoteHost with the
// given headers.
func usageRequestForTest(router *gin.Engine, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	request.RemoteAddr = usageTestRemoteHost + ":1234"

	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return response
}

func decodeUsageSummaryForTest(response *httptest.ResponseRecorder) UsageSummary {
	var summary UsageSummary
	convey.So(json.Unmarshal(response.Body.Bytes(), &summary), convey.ShouldBeNil)

	return summary
}

func flushedUsageRowsForTest(store *UsageStore, path string) []usageRowForTest {
	convey.So(store.Flush(context.Background()), convey.ShouldBeNil)

	return usageRowsForTest(path)
}

// openUsageServerTestStore opens a usage store in a temp dir, closing it when
// the test ends, and returns it with its file path.
func openUsageServerTestStore(t *testing.T) (*UsageStore, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "usage.sqlite")

	store, err := OpenUsageStore(context.Background(), path, 30)
	convey.So(err, convey.ShouldBeNil)
	t.Cleanup(func() { _ = store.Close() })

	return store, path
}

// newUsageServerRouterForTest registers server's routes on a new plain-mode
// engine.
func newUsageServerRouterForTest(server *Server) *gin.Engine {
	router := gin.New()
	server.RegisterRoutes(router, nil)

	return router
}

func decodeErrorEnvelopeForUsageTest(response *httptest.ResponseRecorder) httpErrorEnvelope {
	var envelope httpErrorEnvelope
	convey.So(json.Unmarshal(response.Body.Bytes(), &envelope), convey.ShouldBeNil)

	return envelope
}
