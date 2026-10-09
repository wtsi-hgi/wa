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
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

const feedbackExampleBodyForTest = `{"category":"no_endpoint","description":"No endpoint lists sample consent.",
 "user_request":"Which samples have withdrawn consent?",
 "tools_tried":["mlwh_search_samples","mlwh_call_endpoint"],
 "mcp_server_version":"0.4.0","wa_api_version":"1.9.0","transport":"stdio",
 "client_name":"claude-code","client_version":"2.1.0"}`

// unknownContentLengthForTest makes the request body length undeclared, so
// only the read cap can reject it.
func unknownContentLengthForTest(request *http.Request) {
	request.ContentLength = -1
}

type capturingSlogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func captureDefaultSlogForTest(t *testing.T) *capturingSlogHandler {
	t.Helper()

	previous := slog.Default()
	handler := &capturingSlogHandler{}
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return handler
}

func (h *capturingSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingSlogHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.records = append(h.records, record.Clone())

	return nil
}

func (h *capturingSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *capturingSlogHandler) WithGroup(string) slog.Handler { return h }

func (h *capturingSlogHandler) recordsWithMessage(message string) []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()

	var matched []slog.Record

	for _, record := range h.records {
		if record.Message == message {
			matched = append(matched, record)
		}
	}

	return matched
}

func TestFeedbackSubmitB1(t *testing.T) {
	ctx := context.Background()
	receivedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	convey.Convey("B1.1: Given a plain-mode server with a temp store, when POSTing the Architecture example, "+
		"then 201, the exact receipt body, and the stored report equals the submission", t, func() {
		store := newFeedbackServerTestStore(t)
		store.now = func() time.Time { return receivedAt.In(time.FixedZone("UTC+1", 3600)) }
		router := newFeedbackTestRouter(store)

		response := postFeedbackForTest(router, feedbackExampleBodyForTest, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(response.Body.String(), convey.ShouldEqual, `{"id":1,"created_at":"2026-10-01T12:00:00Z"}`)

		page, err := store.List(ctx, FeedbackFilter{}, 50, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(page.Items, convey.ShouldResemble, []FeedbackReport{{
			ID:               1,
			CreatedAt:        "2026-10-01T12:00:00Z",
			Category:         FeedbackCategoryNoEndpoint,
			Description:      "No endpoint lists sample consent.",
			UserRequest:      "Which samples have withdrawn consent?",
			ToolsTried:       []string{"mlwh_search_samples", "mlwh_call_endpoint"},
			MCPServerVersion: "0.4.0",
			WAAPIVersion:     "1.9.0",
			Transport:        "stdio",
			ClientName:       "claude-code",
			ClientVersion:    "2.1.0",
			ClientUserAgent:  "",
			RemoteAddr:       "192.0.2.1",
		}})
	})

	convey.Convey("B1.2: Given X-Forwarded-For, when POSTing, then the stored RemoteAddr is the connection host", t, func() {
		store := newFeedbackServerTestStore(t)
		router := newFeedbackTestRouter(store)

		response := postFeedbackForTest(router, feedbackExampleBodyForTest, func(request *http.Request) {
			request.Header.Set("X-Forwarded-For", "203.0.113.9")
			request.Header.Set("X-Real-IP", "203.0.113.10")
		})

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(storedFeedbackRemoteAddrs(ctx, store), convey.ShouldResemble, []string{"192.0.2.1"})
	})

	convey.Convey("B1 RemoteAddr: Given connection addresses, when POSTing, then the stored RemoteAddr is the "+
		"SplitHostPort host, or the raw value when it cannot be split", t, func() {
		for _, tc := range []struct{ remoteAddr, want string }{
			{"[2001:db8::1]:4321", "2001:db8::1"},
			{"198.51.100.7:80", "198.51.100.7"},
			{"no-port-here", "no-port-here"},
		} {
			store := newFeedbackServerTestStore(t)
			router := newFeedbackTestRouter(store)

			response := postFeedbackForTest(router, feedbackExampleBodyForTest, func(request *http.Request) {
				request.RemoteAddr = tc.remoteAddr
			})

			convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
			convey.So(storedFeedbackRemoteAddrs(ctx, store), convey.ShouldResemble, []string{tc.want})
		}
	})

	convey.Convey("B1.3: Given a captured default slog handler, when one report is accepted, then one record "+
		"has the message and id, category and remote_addr attributes", t, func() {
		handler := captureDefaultSlogForTest(t)
		router := newFeedbackTestRouter(newFeedbackServerTestStore(t))

		response := postFeedbackForTest(router, feedbackExampleBodyForTest, func(request *http.Request) {
			request.Header.Set("X-Forwarded-For", "203.0.113.9")
		})
		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)

		records := handler.recordsWithMessage("mlwh feedback received")
		convey.So(records, convey.ShouldHaveLength, 1)
		convey.So(records[0].Level, convey.ShouldEqual, slog.LevelInfo)
		convey.So(slogAttrsForTest(records[0]), convey.ShouldResemble, map[string]string{
			"id":          "1",
			"category":    "no_endpoint",
			"remote_addr": "192.0.2.1",
		})
	})

	convey.Convey("B1.4: Given an unknown category, then 400 bad_request naming the category and no row", t, func() {
		store := newFeedbackServerTestStore(t)

		response := postFeedbackForTest(newFeedbackTestRouter(store), `{"category":"bogus","description":"x"}`, nil)

		assertFeedbackErrorForTest(response, http.StatusBadRequest, "bad_request", `invalid category "bogus"`)
		convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 0)
	})

	convey.Convey("B1.5: Given a blank description, then 400 bad_request and no row", t, func() {
		store := newFeedbackServerTestStore(t)

		response := postFeedbackForTest(newFeedbackTestRouter(store), `{"category":"other","description":" \t\n "}`, nil)

		assertFeedbackErrorForTest(response, http.StatusBadRequest, "bad_request", "description is required")
		convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 0)
	})

	convey.Convey("B1.6: Given non-object or trailing-data bodies, then 400 invalid JSON body and no row", t, func() {
		for _, body := range []string{
			"",
			"not json",
			"[1]",
			`"a string"`,
			feedbackExampleBodyForTest + "{}",
			feedbackExampleBodyForTest + "x",
		} {
			store := newFeedbackServerTestStore(t)

			response := postFeedbackForTest(newFeedbackTestRouter(store), body, nil)

			assertFeedbackErrorForTest(response, http.StatusBadRequest, "bad_request", "invalid JSON body")
			convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 0)
		}
	})

	convey.Convey("B1.6: Given the example followed by a newline, then 201", t, func() {
		store := newFeedbackServerTestStore(t)

		response := postFeedbackForTest(newFeedbackTestRouter(store), feedbackExampleBodyForTest+"\n", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 1)
	})

	convey.Convey("B1.7: Given a 70000-byte valid JSON body, then 413 payload_too_large and no row, "+
		"whether or not Content-Length is declared", t, func() {
		body := feedbackBodyOfSizeForTest(70000)
		convey.So(len(body), convey.ShouldEqual, 70000)

		for _, declareLength := range []bool{true, false} {
			store := newFeedbackServerTestStore(t)

			response := postFeedbackForTest(newFeedbackTestRouter(store), body, func(request *http.Request) {
				if !declareLength {
					request.ContentLength = -1
				}
			})

			assertFeedbackErrorForTest(response, http.StatusRequestEntityTooLarge, "payload_too_large",
				"request body exceeds 65536 bytes")
			convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 0)
		}
	})

	convey.Convey("B1 step 2: Given a declared Content-Length over 65536 on a short valid body, then 413 "+
		"before the body is read", t, func() {
		store := newFeedbackServerTestStore(t)

		response := postFeedbackForTest(newFeedbackTestRouter(store), feedbackExampleBodyForTest, func(request *http.Request) {
			request.ContentLength = FeedbackMaxBodyBytes + 1
		})

		assertFeedbackErrorForTest(response, http.StatusRequestEntityTooLarge, "payload_too_large",
			"request body exceeds 65536 bytes")
		convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 0)
	})

	convey.Convey("B1.7: Given a body at exactly 65536 bytes, then the body cap does not reject it", t, func() {
		store := newFeedbackServerTestStore(t)
		body := feedbackBodyOfSizeForTest(FeedbackMaxBodyBytes)

		response := postFeedbackForTest(newFeedbackTestRouter(store), body, nil)

		assertFeedbackErrorForTest(response, http.StatusRequestEntityTooLarge, "payload_too_large",
			"user_request exceeds 16384 bytes")
	})

	convey.Convey("B1 step 3: Given an under-cap object followed by whitespace past 65536 bytes with no "+
		"Content-Length, then the second decode's MaxBytesError is 413", t, func() {
		store := newFeedbackServerTestStore(t)
		body := feedbackExampleBodyForTest + strings.Repeat(" ", FeedbackMaxBodyBytes)

		response := postFeedbackForTest(newFeedbackTestRouter(store), body, func(request *http.Request) {
			request.ContentLength = -1
		})

		assertFeedbackErrorForTest(response, http.StatusRequestEntityTooLarge, "payload_too_large",
			"request body exceeds 65536 bytes")
		convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 0)
	})

	convey.Convey("B1.8: Given a 16385-byte description in an under-cap body, then 413 naming description", t, func() {
		store := newFeedbackServerTestStore(t)
		body := `{"category":"other","description":"` + strings.Repeat("d", FeedbackMaxDescriptionBytes+1) + `"}`

		response := postFeedbackForTest(newFeedbackTestRouter(store), body, nil)

		assertFeedbackErrorForTest(response, http.StatusRequestEntityTooLarge, "payload_too_large",
			"description exceeds 16384 bytes")
		convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 0)
	})

	convey.Convey("B1.9: Given an extra unknown field, then 201", t, func() {
		store := newFeedbackServerTestStore(t)
		body := `{"category":"other","description":"x","future_field":"x"}`

		response := postFeedbackForTest(newFeedbackTestRouter(store), body, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 1)
	})

	convey.Convey("B1.10: Given a store closed before the request, then 500 internal_error", t, func() {
		store := newFeedbackServerTestStore(t)
		convey.So(store.Close(), convey.ShouldBeNil)

		response := postFeedbackForTest(newFeedbackTestRouter(store), feedbackExampleBodyForTest, nil)

		assertFeedbackErrorForTest(response, http.StatusInternalServerError, "internal_error", "could not store feedback")
	})

	convey.Convey("B1.11: Given two sequential valid POSTs, then ids 1 and 2", t, func() {
		router := newFeedbackTestRouter(newFeedbackServerTestStore(t))

		first := postFeedbackForTest(router, feedbackExampleBodyForTest, nil)
		second := postFeedbackForTest(router, feedbackExampleBodyForTest, nil)

		convey.So(first.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(second.Code, convey.ShouldEqual, http.StatusCreated)

		var firstReceipt, secondReceipt FeedbackReceipt
		decodeMLWHJSONResponseForTest(t, first, &firstReceipt)
		decodeMLWHJSONResponseForTest(t, second, &secondReceipt)
		convey.So(firstReceipt.ID, convey.ShouldEqual, 1)
		convey.So(secondReceipt.ID, convey.ShouldEqual, 2)
	})

	convey.Convey("B1.12: Given body null, then 400 invalid category \"\" and no row", t, func() {
		store := newFeedbackServerTestStore(t)

		response := postFeedbackForTest(newFeedbackTestRouter(store), "null", nil)

		assertFeedbackErrorForTest(response, http.StatusBadRequest, "bad_request", `invalid category ""`)
		convey.So(feedbackRowCount(ctx, store), convey.ShouldEqual, 0)
	})

	convey.Convey("B1 step 1: Given feedback disabled, when POSTing an oversized body, then 503 feedback_disabled "+
		"is checked before the body", t, func() {
		router := newFeedbackTestRouter(nil)

		response := postFeedbackForTest(router, feedbackBodyOfSizeForTest(70000), nil)

		assertFeedbackErrorForTest(response, http.StatusServiceUnavailable, "feedback_disabled",
			"feedback is disabled on this server")
	})

	convey.Convey("Given a remote envelope with code feedback_disabled, then the client error wraps "+
		"ErrFeedbackDisabled", t, func() {
		server := newRemoteClientErrorServerForTest(http.StatusServiceUnavailable, "feedback_disabled")
		defer server.Close()

		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		_, err := client.SamplesForStudy(ctx, "6568", 100, 0)

		convey.So(errors.Is(err, ErrFeedbackDisabled), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeFalse)
	})
}

func newFeedbackServerTestStore(t *testing.T) *FeedbackStore {
	t.Helper()

	return openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))
}

func newFeedbackTestRouter(store *FeedbackStore) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	NewServer(&serverFakeQueryer{}, WithFeedback(store, []byte("admin-token"))).RegisterRoutes(router, nil)

	return router
}

func postFeedbackForTest(router *gin.Engine, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	if mutate != nil {
		mutate(request)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return response
}

func storedFeedbackRemoteAddrs(ctx context.Context, store *FeedbackStore) []string {
	page, err := store.List(ctx, FeedbackFilter{}, 50, 0)
	convey.So(err, convey.ShouldBeNil)

	addrs := make([]string, 0, len(page.Items))
	for _, report := range page.Items {
		addrs = append(addrs, report.RemoteAddr)
	}

	return addrs
}

func slogAttrsForTest(record slog.Record) map[string]string {
	attrs := map[string]string{}
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Resolve().String()

		return true
	})

	return attrs
}

func assertFeedbackErrorForTest(response *httptest.ResponseRecorder, status int, code, message string) {
	convey.So(response.Code, convey.ShouldEqual, status)
	convey.So(response.Header().Get("Content-Type"), convey.ShouldStartWith, "application/json")

	var envelope httpErrorEnvelope
	convey.So(json.Unmarshal(response.Body.Bytes(), &envelope), convey.ShouldBeNil)
	convey.So(envelope, convey.ShouldResemble, httpErrorEnvelope{Code: code, Message: message})
}

func feedbackRowCount(ctx context.Context, store *FeedbackStore) int {
	var count int
	convey.So(store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM feedback").Scan(&count), convey.ShouldBeNil)

	return count
}

// feedbackBodyOfSizeForTest returns a valid submission JSON object of exactly
// size bytes, padded through user_request.
func feedbackBodyOfSizeForTest(size int) string {
	prefix := `{"category":"other","description":"x","user_request":"`
	suffix := `"}`

	return prefix + strings.Repeat("u", size-len(prefix)-len(suffix)) + suffix
}

// feedbackStudiesQueryerForTest answers GET /studies with one study.
type feedbackStudiesQueryerForTest struct {
	serverFakeQueryer
}

func (q *feedbackStudiesQueryerForTest) AllStudies(context.Context, int, int) ([]Study, error) {
	return []Study{{IDStudyLims: "6568", Name: "feedback study"}}, nil
}

func TestFeedbackAdminAuthB2(t *testing.T) {
	const token = "admin-token"

	adminRoutes := []struct{ method, path string }{
		{http.MethodGet, "/feedback"},
		{http.MethodPatch, "/feedback/1"},
		{http.MethodDelete, "/feedback/1"},
	}

	convey.Convey("B2.1: Given token T, when GET /feedback has no Authorization, then 401 admin token required", t, func() {
		router := newFeedbackTestRouter(newFeedbackServerTestStore(t))

		response := adminFeedbackRequestForTest(router, http.MethodGet, "/feedback", "")

		assertFeedbackErrorForTest(response, http.StatusUnauthorized, "unauthorized", "admin token required")
	})

	convey.Convey("B2.2: Given a wrong token, another scheme, or an empty Bearer value, then 401 for each of "+
		"GET, PATCH and DELETE", t, func() {
		router := newFeedbackTestRouter(newFeedbackServerTestStore(t))

		for _, authorization := range []string{
			"Bearer wrong",
			"Basic " + token,
			"Bearer ",
			"Bearer " + token + "x",
			"Bearer " + token[:len(token)-1],
			"bearer " + token,
			"Bearer" + token,
			token,
			"Basic Bearer " + token,
		} {
			for _, route := range adminRoutes {
				response := adminFeedbackRequestForTest(router, route.method, route.path, authorization)

				assertFeedbackErrorForTest(response, http.StatusUnauthorized, "unauthorized", "admin token required")
			}
		}
	})

	convey.Convey("B2.3: Given Authorization Bearer T, then GET /feedback returns 200", t, func() {
		router := newFeedbackTestRouter(newFeedbackServerTestStore(t))

		response := adminFeedbackRequestForTest(router, http.MethodGet, "/feedback", "Bearer "+token)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, `{"items":[],"total":0,"next_offset":-1}`)
	})

	convey.Convey("B2 trimming: Given a configured token with surrounding whitespace, then a Bearer value with "+
		"different surrounding whitespace is accepted", t, func() {
		gin.SetMode(gin.TestMode)

		router := gin.New()
		NewServer(&serverFakeQueryer{}, WithFeedback(newFeedbackServerTestStore(t), []byte(" "+token+"\n"))).
			RegisterRoutes(router, nil)

		response := adminFeedbackRequestForTest(router, http.MethodGet, "/feedback", "Bearer \t"+token+"  ")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
	})

	convey.Convey("B2 empty token: Given an enabled store with a blank admin token, then an empty Bearer value "+
		"is still 401", t, func() {
		gin.SetMode(gin.TestMode)

		router := gin.New()
		NewServer(&serverFakeQueryer{}, WithFeedback(newFeedbackServerTestStore(t), []byte(" \n"))).
			RegisterRoutes(router, nil)

		for _, authorization := range []string{"Bearer ", "Bearer  ", ""} {
			response := adminFeedbackRequestForTest(router, http.MethodGet, "/feedback", authorization)

			assertFeedbackErrorForTest(response, http.StatusUnauthorized, "unauthorized", "admin token required")
		}
	})

	convey.Convey("B2.4: Given feedback disabled, when an admin route is called with or without a token, then "+
		"503 feedback_disabled", t, func() {
		router := newFeedbackTestRouter(nil)

		for _, authorization := range []string{"", "Bearer " + token, "Bearer wrong"} {
			for _, route := range adminRoutes {
				response := adminFeedbackRequestForTest(router, route.method, route.path, authorization)

				assertFeedbackErrorForTest(response, http.StatusServiceUnavailable, "feedback_disabled",
					"feedback is disabled on this server")
			}
		}
	})

	convey.Convey("B2.5: Given token T, when POST /feedback has no Authorization in plain mode, then 201", t, func() {
		router := newFeedbackTestRouter(newFeedbackServerTestStore(t))

		response := postFeedbackForTest(router, feedbackExampleBodyForTest, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
	})

	convey.Convey("B2.6: Given token T, when GET /studies has no Authorization in plain mode, then its status "+
		"matches a server without feedback", t, func() {
		gin.SetMode(gin.TestMode)

		queryer := &feedbackStudiesQueryerForTest{}

		withoutFeedback := gin.New()
		NewServer(queryer).RegisterRoutes(withoutFeedback, nil)

		withFeedback := gin.New()
		NewServer(queryer, WithFeedback(newFeedbackServerTestStore(t), []byte(token))).
			RegisterRoutes(withFeedback, nil)

		baseline := adminFeedbackRequestForTest(withoutFeedback, http.MethodGet, "/studies", "")
		response := adminFeedbackRequestForTest(withFeedback, http.MethodGet, "/studies", "")

		convey.So(baseline.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Code, convey.ShouldEqual, baseline.Code)
		convey.So(response.Body.String(), convey.ShouldEqual, baseline.Body.String())
	})
}

// adminFeedbackRequestForTest sends an empty-bodied request, setting
// Authorization only when authorization is non-empty.
func adminFeedbackRequestForTest(router *gin.Engine, method, path, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return response
}

func TestFeedbackListB3(t *testing.T) {
	ctx := context.Background()

	convey.Convey("B3.1: Given 3 reports, when GET with a valid token, then ids [3,2,1], total 3, next_offset -1, "+
		"and each item has all 15 FeedbackReport keys and the stored values", t, func() {
		store := newFeedbackServerTestStore(t)
		store.now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }

		var added []FeedbackReport

		for _, category := range []FeedbackCategory{
			FeedbackCategoryNoEndpoint, FeedbackCategoryAgentMistake, FeedbackCategoryOther,
		} {
			report, err := store.Add(ctx, FeedbackSubmission{
				Category: category, Description: "d " + string(category), ToolsTried: []string{"t"},
			}, "192.0.2.7")
			convey.So(err, convey.ShouldBeNil)

			added = append(added, report)
		}

		acked, err := store.SetAcknowledged(ctx, 2, true)
		convey.So(err, convey.ShouldBeNil)

		added[1] = acked

		response := listFeedbackForTest(newFeedbackTestRouter(store), "")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldStartWith, "application/json")

		page := decodeFeedbackPageForTest(response)
		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, []int64{3, 2, 1})
		convey.So(page.Total, convey.ShouldEqual, 3)
		convey.So(page.NextOffset, convey.ShouldEqual, -1)
		convey.So(page.Items, convey.ShouldResemble, []FeedbackReport{added[2], added[1], added[0]})

		var raw struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		convey.So(json.Unmarshal(response.Body.Bytes(), &raw), convey.ShouldBeNil)
		convey.So(raw.Items, convey.ShouldHaveLength, 3)

		wantKeys := []string{
			"acknowledged", "acknowledged_at", "category", "client_name", "client_user_agent", "client_version",
			"created_at", "description", "id", "mcp_server_version", "remote_addr", "tools_tried", "transport",
			"user_request", "wa_api_version",
		}

		for _, item := range raw.Items {
			convey.So(slices.Sorted(maps.Keys(item)), convey.ShouldResemble, wantKeys)
		}
	})

	convey.Convey("B3.2: Given 60 reports, when GET with no params, then 50 items (60..11), total 60, "+
		"next_offset 50", t, func() {
		store := newFeedbackServerTestStore(t)
		addTestFeedback(ctx, store, repeatedFeedbackCategoryForTest(60)...)

		page := decodeFeedbackPageForTest(listFeedbackForTest(newFeedbackTestRouter(store), ""))

		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, descendingIDsForTest(60, 11))
		convey.So(page.Total, convey.ShouldEqual, 60)
		convey.So(page.NextOffset, convey.ShouldEqual, 50)
	})

	convey.Convey("B3 pagination: Given 60 reports, then limit and offset select the page and next_offset", t, func() {
		store := newFeedbackServerTestStore(t)
		addTestFeedback(ctx, store, repeatedFeedbackCategoryForTest(60)...)
		router := newFeedbackTestRouter(store)

		page := decodeFeedbackPageForTest(listFeedbackForTest(router, "?limit=20&offset=20"))
		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, descendingIDsForTest(40, 21))
		convey.So(page.Total, convey.ShouldEqual, 60)
		convey.So(page.NextOffset, convey.ShouldEqual, 40)

		page = decodeFeedbackPageForTest(listFeedbackForTest(router, "?offset=50"))
		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, descendingIDsForTest(10, 1))
		convey.So(page.NextOffset, convey.ShouldEqual, -1)

		page = decodeFeedbackPageForTest(listFeedbackForTest(router, "?limit=500&offset=0"))
		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, descendingIDsForTest(60, 1))
		convey.So(page.Total, convey.ShouldEqual, 60)
		convey.So(page.NextOffset, convey.ShouldEqual, -1)
	})

	convey.Convey("B3.3: Given an invalid query value, then 400 bad_request naming the parameter", t, func() {
		store := newFeedbackServerTestStore(t)
		addTestFeedback(ctx, store, FeedbackCategoryOther)
		router := newFeedbackTestRouter(store)

		cases := []struct{ query, message string }{
			{"?limit=501", "limit must be between 0 and 500"},
			{"?limit=-1", "limit must be between 0 and 500"},
			{"?limit=x", "invalid limit"},
			{"?offset=-1", "offset must not be negative"},
			{"?offset=x", "invalid offset"},
			{"?acknowledged=yes", `invalid acknowledged "yes"`},
			{"?acknowledged=1", `invalid acknowledged "1"`},
			{"?acknowledged=TRUE", `invalid acknowledged "TRUE"`},
			{"?category=bogus", `invalid category "bogus"`},
			{"?category=USER_UNHAPPY", `invalid category "USER_UNHAPPY"`},
		}

		for _, tc := range cases {
			convey.Convey(tc.query, func() {
				assertFeedbackErrorForTest(listFeedbackForTest(router, tc.query),
					http.StatusBadRequest, httpErrorCodeBadRequest, tc.message)
			})
		}
	})

	convey.Convey("B3.4: Given id 1 acknowledged and id 2 not, then acknowledged=false lists [2] and "+
		"acknowledged=true lists [1]", t, func() {
		store := newFeedbackServerTestStore(t)
		addTestFeedback(ctx, store, FeedbackCategoryOther, FeedbackCategoryOther)

		_, err := store.SetAcknowledged(ctx, 1, true)
		convey.So(err, convey.ShouldBeNil)

		router := newFeedbackTestRouter(store)

		page := decodeFeedbackPageForTest(listFeedbackForTest(router, "?acknowledged=false"))
		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, []int64{2})
		convey.So(page.Total, convey.ShouldEqual, 1)
		convey.So(page.NextOffset, convey.ShouldEqual, -1)

		page = decodeFeedbackPageForTest(listFeedbackForTest(router, "?acknowledged=true"))
		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, []int64{1})
		convey.So(page.Total, convey.ShouldEqual, 1)
	})

	convey.Convey("B3.5: Given category=user_unhappy&acknowledged=true with one matching report among decoys, "+
		"then exactly that report", t, func() {
		store := newFeedbackServerTestStore(t)
		addTestFeedback(ctx, store,
			FeedbackCategoryUserUnhappy, // 1: right category, unacknowledged
			FeedbackCategoryOther,       // 2: wrong category, acknowledged
			FeedbackCategoryUserUnhappy, // 3: match
			FeedbackCategoryOther,       // 4: wrong category, unacknowledged
		)

		for _, id := range []int64{2, 3} {
			_, err := store.SetAcknowledged(ctx, id, true)
			convey.So(err, convey.ShouldBeNil)
		}

		router := newFeedbackTestRouter(store)

		page := decodeFeedbackPageForTest(listFeedbackForTest(router, "?category=user_unhappy&acknowledged=true"))
		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, []int64{3})
		convey.So(page.Items[0].Category, convey.ShouldEqual, FeedbackCategoryUserUnhappy)
		convey.So(page.Items[0].Acknowledged, convey.ShouldBeTrue)
		convey.So(page.Total, convey.ShouldEqual, 1)
		convey.So(page.NextOffset, convey.ShouldEqual, -1)

		page = decodeFeedbackPageForTest(listFeedbackForTest(router, "?category=user_unhappy"))
		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, []int64{3, 1})
		convey.So(page.Total, convey.ShouldEqual, 2)
	})

	convey.Convey("B3.6: Given an empty store, then the exact empty page body", t, func() {
		response := listFeedbackForTest(newFeedbackTestRouter(newFeedbackServerTestStore(t)), "")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, `{"items":[],"total":0,"next_offset":-1}`)
	})

	convey.Convey("B3.7: Given 3 reports, when limit=0, then the exact empty page with total 3 and "+
		"next_offset 0", t, func() {
		store := newFeedbackServerTestStore(t)
		addTestFeedback(ctx, store, FeedbackCategoryOther, FeedbackCategoryOther, FeedbackCategoryOther)

		response := listFeedbackForTest(newFeedbackTestRouter(store), "?limit=0")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, `{"items":[],"total":3,"next_offset":0}`)
	})

	convey.Convey("B3 store failure: Given a store closed before the request, then 500 internal_error", t, func() {
		store := newFeedbackServerTestStore(t)
		router := newFeedbackTestRouter(store)
		convey.So(store.Close(), convey.ShouldBeNil)

		assertFeedbackErrorForTest(listFeedbackForTest(router, ""),
			http.StatusInternalServerError, httpErrorCodeInternal, "could not list feedback")
	})
}

func TestFeedbackAcknowledgeB4(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	acknowledgedAt := time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC)

	newStore := func(n int) *FeedbackStore {
		store := newFeedbackServerTestStore(t)
		store.now = func() time.Time { return createdAt }
		addTestFeedback(ctx, store, repeatedFeedbackCategoryForTest(n)...)
		store.now = func() time.Time { return acknowledgedAt }

		return store
	}

	convey.Convey("B4.1: Given reports 1-3, when a valid-token PATCH /feedback/2 sends acknowledged true, then the "+
		"handler returns 200 with exactly the updated report and only report 2 is acknowledged", t, func() {
		store := newStore(3)

		response := patchFeedbackForTest(newFeedbackTestRouter(store), "/feedback/2", `{"acknowledged":true}`, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldStartWith, "application/json")

		var report FeedbackReport
		convey.So(json.Unmarshal(response.Body.Bytes(), &report), convey.ShouldBeNil)
		convey.So(report.ID, convey.ShouldEqual, 2)
		convey.So(report.Acknowledged, convey.ShouldBeTrue)
		convey.So(report.AcknowledgedAt, convey.ShouldEqual, "2026-10-02T09:30:00Z")

		stored := storedFeedbackReportForTest(ctx, store, 2)
		want, err := json.Marshal(stored)
		convey.So(err, convey.ShouldBeNil)
		convey.So(response.Body.String(), convey.ShouldEqual, string(want))

		acknowledged := true
		convey.So(listFeedbackIDs(ctx, store, FeedbackFilter{Acknowledged: &acknowledged}),
			convey.ShouldResemble, []int64{2})
	})

	convey.Convey("B4.2: Given an acknowledged report 1, when PATCH acknowledged false, then 200 with "+
		"acknowledged false and an empty acknowledged_at, and the store agrees", t, func() {
		store := newStore(1)
		_, err := store.SetAcknowledged(ctx, 1, true)
		convey.So(err, convey.ShouldBeNil)

		response := patchFeedbackForTest(newFeedbackTestRouter(store), "/feedback/1", `{"acknowledged":false}`, nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var report FeedbackReport
		convey.So(json.Unmarshal(response.Body.Bytes(), &report), convey.ShouldBeNil)
		convey.So(report.ID, convey.ShouldEqual, 1)
		convey.So(report.Acknowledged, convey.ShouldBeFalse)
		convey.So(report.AcknowledgedAt, convey.ShouldEqual, "")
		convey.So(response.Body.String(), convey.ShouldContainSubstring, `"acknowledged_at":""`)
		convey.So(storedFeedbackReportForTest(ctx, store, 1).Acknowledged, convey.ShouldBeFalse)
	})

	convey.Convey("B4.3: Given an unknown id, then 404 with the exact not_found envelope naming that id", t, func() {
		router := newFeedbackTestRouter(newStore(3))

		for _, id := range []string{"99", "4", "9223372036854775807"} {
			response := patchFeedbackForTest(router, "/feedback/"+id, `{"acknowledged":true}`, nil)

			convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
			convey.So(response.Header().Get("Content-Type"), convey.ShouldStartWith, "application/json")
			convey.So(response.Body.String(), convey.ShouldEqual,
				`{"code":"not_found","message":"feedback `+id+` not found"}`)
		}
	})

	convey.Convey("B4.4: Given an id that is not a positive base-10 int64, then 400 invalid feedback id and "+
		"report 1 stays unacknowledged", t, func() {
		store := newStore(1)
		router := newFeedbackTestRouter(store)

		for _, id := range []string{"abc", "0", "-1", "1.5", "0x1", "1abc", "9223372036854775808"} {
			response := patchFeedbackForTest(router, "/feedback/"+id, `{"acknowledged":true}`, nil)

			assertFeedbackErrorForTest(response, http.StatusBadRequest, httpErrorCodeBadRequest, "invalid feedback id")
		}

		convey.So(storedFeedbackReportForTest(ctx, store, 1).Acknowledged, convey.ShouldBeFalse)
	})

	convey.Convey("B4.5: Given a missing or non-boolean acknowledged, then 400 acknowledged must be a boolean, "+
		"and given a body that is not one JSON value, then 400 invalid JSON body; report 1 is unchanged", t, func() {
		store := newStore(1)
		router := newFeedbackTestRouter(store)

		cases := []struct{ body, message string }{
			{`{}`, "acknowledged must be a boolean"},
			{`{"acknowledged":"yes"}`, "acknowledged must be a boolean"},
			{`{"acknowledged":"true"}`, "acknowledged must be a boolean"},
			{`{"acknowledged":1}`, "acknowledged must be a boolean"},
			{`{"acknowledged":null}`, "acknowledged must be a boolean"},
			{`{"pad":true}`, "acknowledged must be a boolean"},
			{`null`, "acknowledged must be a boolean"},
			{`not json`, "invalid JSON body"},
			{``, "invalid JSON body"},
			{`[true]`, "invalid JSON body"},
			{`{"acknowledged":true}{}`, "invalid JSON body"},
		}

		for _, tc := range cases {
			response := patchFeedbackForTest(router, "/feedback/1", tc.body, nil)

			assertFeedbackErrorForTest(response, http.StatusBadRequest, httpErrorCodeBadRequest, tc.message)
		}

		convey.So(storedFeedbackReportForTest(ctx, store, 1).Acknowledged, convey.ShouldBeFalse)
	})

	convey.Convey("B4.6: Given a 2000-byte body, with or without a declared Content-Length, then 413 "+
		"payload_too_large and report 1 is still unacknowledged", t, func() {
		store := newStore(1)
		router := newFeedbackTestRouter(store)
		body := feedbackPatchBodyOfSizeForTest(2000)

		for _, mutate := range []func(*http.Request){nil, unknownContentLengthForTest} {
			response := patchFeedbackForTest(router, "/feedback/1", body, mutate)

			assertFeedbackErrorForTest(response, http.StatusRequestEntityTooLarge, httpErrorCodePayloadTooLarge,
				"request body exceeds 1024 bytes")
		}

		convey.So(storedFeedbackReportForTest(ctx, store, 1).Acknowledged, convey.ShouldBeFalse)
	})

	convey.Convey("B4.6 cap boundary: Given a 1024-byte body then 200, and a 1025-byte body then 413, with or "+
		"without a declared Content-Length", t, func() {
		for _, mutate := range []func(*http.Request){nil, unknownContentLengthForTest} {
			store := newStore(1)
			router := newFeedbackTestRouter(store)

			response := patchFeedbackForTest(router, "/feedback/1", feedbackPatchBodyOfSizeForTest(1025), mutate)
			assertFeedbackErrorForTest(response, http.StatusRequestEntityTooLarge, httpErrorCodePayloadTooLarge,
				"request body exceeds 1024 bytes")
			convey.So(storedFeedbackReportForTest(ctx, store, 1).Acknowledged, convey.ShouldBeFalse)

			response = patchFeedbackForTest(router, "/feedback/1", feedbackPatchBodyOfSizeForTest(1024), mutate)
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(storedFeedbackReportForTest(ctx, store, 1).Acknowledged, convey.ShouldBeTrue)
		}
	})

	convey.Convey("B4.6 Content-Length: Given a short valid body declaring a Content-Length over 1024, then 413 "+
		"and report 1 is still unacknowledged", t, func() {
		store := newStore(1)

		response := patchFeedbackForTest(newFeedbackTestRouter(store), "/feedback/1", `{"acknowledged":true}`,
			func(request *http.Request) { request.ContentLength = feedbackMaxPatchBodyBytes + 1 })

		assertFeedbackErrorForTest(response, http.StatusRequestEntityTooLarge, httpErrorCodePayloadTooLarge,
			"request body exceeds 1024 bytes")
		convey.So(storedFeedbackReportForTest(ctx, store, 1).Acknowledged, convey.ShouldBeFalse)
	})

	convey.Convey("B4 check order: Given an oversized body on an invalid id, then 413 because the body is "+
		"read before the id is parsed", t, func() {
		router := newFeedbackTestRouter(newStore(1))

		for _, mutate := range []func(*http.Request){nil, unknownContentLengthForTest} {
			response := patchFeedbackForTest(router, "/feedback/abc", feedbackPatchBodyOfSizeForTest(2000), mutate)

			assertFeedbackErrorForTest(response, http.StatusRequestEntityTooLarge, httpErrorCodePayloadTooLarge,
				"request body exceeds 1024 bytes")
		}
	})

	convey.Convey("B4 store failure: Given a store closed before the request, then 500 internal_error", t, func() {
		store := newStore(1)
		router := newFeedbackTestRouter(store)
		convey.So(store.Close(), convey.ShouldBeNil)

		assertFeedbackErrorForTest(patchFeedbackForTest(router, "/feedback/1", `{"acknowledged":true}`, nil),
			http.StatusInternalServerError, httpErrorCodeInternal, "could not update feedback")
	})
}

func TestFeedbackDeleteB5(t *testing.T) {
	ctx := context.Background()

	newStore := func(n int) *FeedbackStore {
		store := newFeedbackServerTestStore(t)
		addTestFeedback(ctx, store, repeatedFeedbackCategoryForTest(n)...)

		return store
	}

	convey.Convey("B5.1: Given reports 1 and 2, when a valid-token DELETE /feedback/1 is sent, then 204 with a "+
		"zero-length body, and a later GET lists only [2]", t, func() {
		router := newFeedbackTestRouter(newStore(2))

		response := deleteFeedbackForTest(router, "/feedback/1")

		convey.So(response.Code, convey.ShouldEqual, http.StatusNoContent)
		convey.So(response.Body.Len(), convey.ShouldEqual, 0)

		page := decodeFeedbackPageForTest(listFeedbackForTest(router, ""))
		convey.So(feedbackPageIDsForTest(page), convey.ShouldResemble, []int64{2})
		convey.So(page.Total, convey.ShouldEqual, 1)
	})

	convey.Convey("B5.1 other id: Given reports 1-3, when DELETE /feedback/2, then 204 and a later GET lists "+
		"[3,1]", t, func() {
		router := newFeedbackTestRouter(newStore(3))

		convey.So(deleteFeedbackForTest(router, "/feedback/2").Code, convey.ShouldEqual, http.StatusNoContent)
		convey.So(feedbackPageIDsForTest(decodeFeedbackPageForTest(listFeedbackForTest(router, ""))),
			convey.ShouldResemble, []int64{3, 1})
	})

	convey.Convey("B5.2: Given DELETE /feedback/1 repeated, then the second returns 404 with the exact "+
		"not_found envelope", t, func() {
		router := newFeedbackTestRouter(newStore(2))

		convey.So(deleteFeedbackForTest(router, "/feedback/1").Code, convey.ShouldEqual, http.StatusNoContent)

		response := deleteFeedbackForTest(router, "/feedback/1")

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldStartWith, "application/json")
		convey.So(response.Body.String(), convey.ShouldEqual, `{"code":"not_found","message":"feedback 1 not found"}`)
		convey.So(feedbackPageIDsForTest(decodeFeedbackPageForTest(listFeedbackForTest(router, ""))),
			convey.ShouldResemble, []int64{2})
	})

	convey.Convey("B5.2 unknown id: Given reports 1 and 2, when DELETE an unknown id, then 404 naming that id "+
		"and both reports remain", t, func() {
		router := newFeedbackTestRouter(newStore(2))

		for _, id := range []string{"99", "3", "9223372036854775807"} {
			response := deleteFeedbackForTest(router, "/feedback/"+id)

			convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
			convey.So(response.Body.String(), convey.ShouldEqual,
				`{"code":"not_found","message":"feedback `+id+` not found"}`)
		}

		convey.So(feedbackPageIDsForTest(decodeFeedbackPageForTest(listFeedbackForTest(router, ""))),
			convey.ShouldResemble, []int64{2, 1})
	})

	convey.Convey("B5.3: Given an id that is not a positive base-10 int64, then 400 invalid feedback id and "+
		"no report is deleted", t, func() {
		router := newFeedbackTestRouter(newStore(1))

		for _, id := range []string{"abc", "0", "-1", "1.5", "0x1", "1abc", "9223372036854775808"} {
			response := deleteFeedbackForTest(router, "/feedback/"+id)

			assertFeedbackErrorForTest(response, http.StatusBadRequest, httpErrorCodeBadRequest, "invalid feedback id")
		}

		convey.So(feedbackPageIDsForTest(decodeFeedbackPageForTest(listFeedbackForTest(router, ""))),
			convey.ShouldResemble, []int64{1})
	})

	convey.Convey("B5 store failure: Given a store closed before the request, then 500 internal_error", t, func() {
		store := newStore(1)
		router := newFeedbackTestRouter(store)
		convey.So(store.Close(), convey.ShouldBeNil)

		assertFeedbackErrorForTest(deleteFeedbackForTest(router, "/feedback/1"),
			http.StatusInternalServerError, httpErrorCodeInternal, "could not delete feedback")
	})
}

func TestFeedbackRoutePlacementB6(t *testing.T) {
	const (
		token       = "admin-token"
		authPrefix  = "/rest/v1/auth"
		authHeader  = "X-Test-Auth"
		disabledMsg = "feedback is disabled on this server"
	)

	adminRoutes := []struct{ method, path string }{
		{http.MethodGet, "/feedback"},
		{http.MethodPatch, "/feedback/1"},
		{http.MethodDelete, "/feedback/1"},
	}

	convey.Convey("B6.1: Given NewServer(q) with no WithFeedback, when POST /feedback is sent in plain mode, "+
		"then 503 feedback_disabled", t, func() {
		gin.SetMode(gin.TestMode)

		router := gin.New()
		NewServer(&serverFakeQueryer{}).RegisterRoutes(router, nil)

		response := postFeedbackForTest(router, feedbackExampleBodyForTest, nil)

		assertFeedbackErrorForTest(response, http.StatusServiceUnavailable, "feedback_disabled", disabledMsg)
	})

	convey.Convey("B6.2: Given WithFeedback(nil, nil), then POST /feedback and the admin routes return 503", t,
		func() {
			gin.SetMode(gin.TestMode)

			router := gin.New()
			NewServer(&serverFakeQueryer{}, WithFeedback(nil, nil)).RegisterRoutes(router, nil)

			response := postFeedbackForTest(router, feedbackExampleBodyForTest, nil)
			assertFeedbackErrorForTest(response, http.StatusServiceUnavailable, "feedback_disabled", disabledMsg)

			for _, route := range adminRoutes {
				response = adminFeedbackRequestForTest(router, route.method, route.path, "Bearer "+token)

				assertFeedbackErrorForTest(response, http.StatusServiceUnavailable, "feedback_disabled", disabledMsg)
			}
		})

	convey.Convey("B6.3: Given a gin engine and a separate auth group, with feedback enabled, then submit is "+
		"served only behind the group and the admin routes only on the plain router", t, func() {
		router := newSecuredFeedbackRouterForTest(newFeedbackServerTestStore(t), []byte(token), authPrefix, authHeader)
		authed := func(request *http.Request) { request.Header.Set(authHeader, "ok") }

		response := postFeedbackForTest(router, feedbackExampleBodyForTest, func(request *http.Request) {
			request.URL.Path = authPrefix + "/feedback"
			authed(request)
		})
		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)

		response = postFeedbackForTest(router, feedbackExampleBodyForTest, func(request *http.Request) {
			request.URL.Path = authPrefix + "/feedback"
		})
		convey.So(response.Code, convey.ShouldEqual, http.StatusUnauthorized)

		response = postFeedbackForTest(router, feedbackExampleBodyForTest, authed)
		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)

		response = adminFeedbackRequestForTest(router, http.MethodGet, "/feedback", "Bearer "+token)
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(feedbackPageIDsForTest(decodeFeedbackPageForTest(response)), convey.ShouldResemble, []int64{1})

		response = patchFeedbackForTest(router, "/feedback/1", `{"acknowledged":true}`, func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
		})
		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		response = adminFeedbackRequestForTest(router, http.MethodDelete, "/feedback/1", "Bearer "+token)
		convey.So(response.Code, convey.ShouldEqual, http.StatusNoContent)

		for _, route := range adminRoutes {
			request := httptest.NewRequest(route.method, authPrefix+route.path, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			authed(request)

			response = httptest.NewRecorder()
			router.ServeHTTP(response, request)

			convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
		}
	})

	convey.Convey("B6.3 disabled: Given the secured layout with feedback disabled, then submit behind the group "+
		"and the admin routes on the plain router still return 503", t, func() {
		router := newSecuredFeedbackRouterForTest(nil, nil, authPrefix, authHeader)

		response := postFeedbackForTest(router, feedbackExampleBodyForTest, func(request *http.Request) {
			request.URL.Path = authPrefix + "/feedback"
			request.Header.Set(authHeader, "ok")
		})
		assertFeedbackErrorForTest(response, http.StatusServiceUnavailable, "feedback_disabled", disabledMsg)

		for _, route := range adminRoutes {
			response = adminFeedbackRequestForTest(router, route.method, route.path, "")

			assertFeedbackErrorForTest(response, http.StatusServiceUnavailable, "feedback_disabled", disabledMsg)
		}
	})

	convey.Convey("B6.4: Given the 90 Registry entries before this feature, then the count is unchanged and no "+
		"entry has path /feedback", t, func() {
		convey.So(len(Registry), convey.ShouldEqual, 90)

		var feedbackPaths []string

		for _, entry := range Registry {
			if strings.HasPrefix(entry.Path, "/feedback") {
				feedbackPaths = append(feedbackPaths, entry.Path)
			}
		}

		convey.So(feedbackPaths, convey.ShouldBeEmpty)
	})
}

// newSecuredFeedbackRouterForTest lays routes out as secured mode does: a gin
// engine plus a separate auth group at authPrefix whose middleware stands in
// for the gas JWT check by rejecting requests without authHeader.
func newSecuredFeedbackRouterForTest(store *FeedbackStore, token []byte, authPrefix, authHeader string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	auth := router.Group(authPrefix, func(c *gin.Context) {
		if c.GetHeader(authHeader) == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
		}
	})

	NewServer(&serverFakeQueryer{}, WithFeedback(store, token)).RegisterRoutes(router, auth)

	return router
}

func repeatedFeedbackCategoryForTest(n int) []FeedbackCategory {
	categories := make([]FeedbackCategory, n)
	for i := range categories {
		categories[i] = FeedbackCategoryOther
	}

	return categories
}

// deleteFeedbackForTest sends DELETE path with the admin token.
func deleteFeedbackForTest(router *gin.Engine, path string) *httptest.ResponseRecorder {
	return adminFeedbackRequestForTest(router, http.MethodDelete, path, "Bearer admin-token")
}

func listFeedbackForTest(router *gin.Engine, query string) *httptest.ResponseRecorder {
	return adminFeedbackRequestForTest(router, http.MethodGet, "/feedback"+query, "Bearer admin-token")
}

func feedbackPageIDsForTest(page Page[FeedbackReport]) []int64 {
	ids := make([]int64, 0, len(page.Items))
	for _, report := range page.Items {
		ids = append(ids, report.ID)
	}

	return ids
}

func decodeFeedbackPageForTest(response *httptest.ResponseRecorder) Page[FeedbackReport] {
	convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

	var page Page[FeedbackReport]
	convey.So(json.Unmarshal(response.Body.Bytes(), &page), convey.ShouldBeNil)

	return page
}

// patchFeedbackForTest sends PATCH path with the admin token and body.
func patchFeedbackForTest(router *gin.Engine, path, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer admin-token")

	if mutate != nil {
		mutate(request)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return response
}

func storedFeedbackReportForTest(ctx context.Context, store *FeedbackStore, id int64) FeedbackReport {
	page, err := store.List(ctx, FeedbackFilter{}, feedbackMaxListLimit, 0)
	convey.So(err, convey.ShouldBeNil)

	index := slices.IndexFunc(page.Items, func(report FeedbackReport) bool { return report.ID == id })
	convey.So(index, convey.ShouldBeGreaterThanOrEqualTo, 0)

	return page.Items[index]
}

// feedbackPatchBodyOfSizeForTest returns a valid acknowledge-true JSON object
// of exactly size bytes, padded through an ignored field.
func feedbackPatchBodyOfSizeForTest(size int) string {
	prefix := `{"acknowledged":true,"pad":"`
	suffix := `"}`

	return prefix + strings.Repeat("p", size-len(prefix)-len(suffix)) + suffix
}

// descendingIDsForTest returns from, from-1, ..., to.
func descendingIDsForTest(from, to int64) []int64 {
	ids := make([]int64, 0, from-to+1)
	for id := from; id >= to; id-- {
		ids = append(ids, id)
	}

	return ids
}
