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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

type submitFeedbackRequestForTest struct {
	method        string
	path          string
	contentType   string
	authorization string
	hasAuth       bool
	body          []byte
}

// submitFeedbackStubForTest serves status, contentType and body for every
// request and records the last request it saw.
func submitFeedbackStubForTest(t *testing.T, status int, contentType, body string) (*httptest.Server, *submitFeedbackRequestForTest) {
	t.Helper()

	seen := &submitFeedbackRequestForTest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}

		_, hasAuth := r.Header["Authorization"]
		*seen = submitFeedbackRequestForTest{
			method:        r.Method,
			path:          r.URL.Path,
			contentType:   r.Header.Get("Content-Type"),
			authorization: r.Header.Get("Authorization"),
			hasAuth:       hasAuth,
			body:          raw,
		}

		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}

		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	return server, seen
}

func TestRemoteSubmitFeedbackRequest(t *testing.T) {
	convey.Convey("C1.1: Given a stub returning 201 with a receipt, then the receipt is decoded and the stub saw "+
		"POST /feedback with a JSON body equal to the submission", t, func() {
		server, seen := submitFeedbackStubForTest(t, http.StatusCreated, "application/json",
			`{"id":7,"created_at":"2026-10-01T12:00:00Z"}`)
		submission := submitFeedbackFullSubmissionForTest()

		receipt, err := submitFeedbackClientForTest(server.URL, "").SubmitFeedback(context.Background(), submission)

		convey.So(err, convey.ShouldBeNil)
		convey.So(receipt, convey.ShouldResemble, FeedbackReceipt{ID: 7, CreatedAt: "2026-10-01T12:00:00Z"})
		convey.So(seen.method, convey.ShouldEqual, http.MethodPost)
		convey.So(seen.path, convey.ShouldEqual, "/feedback")
		convey.So(seen.contentType, convey.ShouldEqual, "application/json")

		var sent FeedbackSubmission
		convey.So(json.Unmarshal(seen.body, &sent), convey.ShouldBeNil)
		convey.So(sent, convey.ShouldResemble, submission)
	})

	convey.Convey("C1.1: Given a 2xx other than 201, then the receipt is still decoded", t, func() {
		server, _ := submitFeedbackStubForTest(t, http.StatusOK, "application/json",
			`{"id":9,"created_at":"2026-10-02T00:00:00Z"}`)

		receipt, err := submitFeedbackClientForTest(server.URL, "").
			SubmitFeedback(context.Background(), submitFeedbackFullSubmissionForTest())

		convey.So(err, convey.ShouldBeNil)
		convey.So(receipt, convey.ShouldResemble, FeedbackReceipt{ID: 9, CreatedAt: "2026-10-02T00:00:00Z"})
	})

	convey.Convey("C1.1: Given an invalid submission, then it is sent unvalidated", t, func() {
		server, seen := submitFeedbackStubForTest(t, http.StatusCreated, "application/json",
			`{"id":1,"created_at":"2026-10-01T12:00:00Z"}`)

		_, err := submitFeedbackClientForTest(server.URL, "").
			SubmitFeedback(context.Background(), FeedbackSubmission{Category: "bogus"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(string(seen.body), convey.ShouldContainSubstring, `"category":"bogus"`)
	})

	convey.Convey("C1.2: Given a base URL with a path and a token, then the path is appended and the Bearer token "+
		"sent", t, func() {
		server, seen := submitFeedbackStubForTest(t, http.StatusCreated, "application/json",
			`{"id":1,"created_at":"2026-10-01T12:00:00Z"}`)

		_, err := submitFeedbackClientForTest(server.URL+"/rest/v1/auth", "jwt").
			SubmitFeedback(context.Background(), submitFeedbackFullSubmissionForTest())

		convey.So(err, convey.ShouldBeNil)
		convey.So(seen.path, convey.ShouldEqual, "/rest/v1/auth/feedback")
		convey.So(seen.authorization, convey.ShouldEqual, "Bearer jwt")

		_, err = submitFeedbackClientForTest(server.URL+"/rest/v1/auth", "").
			SubmitFeedback(context.Background(), submitFeedbackFullSubmissionForTest())

		convey.So(err, convey.ShouldBeNil)
		convey.So(seen.hasAuth, convey.ShouldBeFalse)
	})

	convey.Convey("C1: SubmitFeedback is not reachable through the Registry-driven Call", t, func() {
		server, seen := submitFeedbackStubForTest(t, http.StatusCreated, "application/json",
			`{"id":1,"created_at":"2026-10-01T12:00:00Z"}`)

		_, err := submitFeedbackClientForTest(server.URL, "").Call(context.Background(), "SubmitFeedback", nil, nil)

		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "registry entry missing")
		convey.So(seen.method, convey.ShouldEqual, "")
	})
}

func submitFeedbackFullSubmissionForTest() FeedbackSubmission {
	return FeedbackSubmission{
		Category:         FeedbackCategoryNoEndpoint,
		Description:      "No endpoint lists sample consent.",
		UserRequest:      "Which samples have withdrawn consent?",
		ToolsTried:       []string{"mlwh_search_samples", "mlwh_call_endpoint"},
		MCPServerVersion: "0.4.0",
		WAAPIVersion:     "1.9.0",
		Transport:        "stdio",
		ClientName:       "claude-code",
		ClientVersion:    "2.1.0",
		ClientUserAgent:  "agent/1.0",
	}
}

func submitFeedbackClientForTest(baseURL, token string) *RemoteClient {
	client, err := NewRemoteClient(RemoteConfig{BaseURL: baseURL, Token: token})
	convey.So(err, convey.ShouldBeNil)

	return client
}

func TestRemoteSubmitFeedbackOtherFailures(t *testing.T) {
	convey.Convey("C1.8: Given 500 internal_error, then no feedback sentinel and the message is kept", t, func() {
		err := submitFeedbackErrorForTest(t, http.StatusInternalServerError, "application/json",
			`{"code":"internal_error","message":"store write failed"}`)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(submitFeedbackMatchedSentinelsForTest(err), convey.ShouldBeEmpty)
		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "store write failed")
	})

	convey.Convey("C1.8: Given 503 cache_never_synced, then ErrCacheNeverSynced without ErrNotFound and no panic", t, func() {
		var err error

		convey.So(func() {
			err = submitFeedbackErrorForTest(t, http.StatusServiceUnavailable, "application/json",
				`{"code":"cache_never_synced","message":"cache has never synced"}`)
		}, convey.ShouldNotPanic)
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(submitFeedbackMatchedSentinelsForTest(err), convey.ShouldBeEmpty)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeFalse)
		convey.So(err.Error(), convey.ShouldContainSubstring, "cache has never synced")
	})

	convey.Convey("C1.8: Given a 503 without the feedback_disabled code, then not ErrFeedbackDisabled", t, func() {
		err := submitFeedbackErrorForTest(t, http.StatusServiceUnavailable, "text/plain", "feedback_disabled")

		convey.So(errors.Is(err, ErrFeedbackDisabled), convey.ShouldBeFalse)
		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
	})

	convey.Convey("C1.9: Given a closed listener, then ErrUpstreamImpaired", t, func() {
		server := httptest.NewServer(http.NotFoundHandler())
		baseURL := server.URL
		server.Close()

		_, err := submitFeedbackClientForTest(baseURL, "").
			SubmitFeedback(context.Background(), submitFeedbackFullSubmissionForTest())

		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		convey.So(submitFeedbackMatchedSentinelsForTest(err), convey.ShouldBeEmpty)
		convey.So(err.Error(), convey.ShouldContainSubstring, "SubmitFeedback request failed")
	})

	convey.Convey("C1.10: Given 201 with a non-JSON body, then ErrUpstreamImpaired", t, func() {
		server, _ := submitFeedbackStubForTest(t, http.StatusCreated, "text/plain", "not json")

		receipt, err := submitFeedbackClientForTest(server.URL, "").
			SubmitFeedback(context.Background(), submitFeedbackFullSubmissionForTest())

		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		convey.So(receipt, convey.ShouldResemble, FeedbackReceipt{})
	})

	convey.Convey("C1: Given a nil client, then ErrUpstreamImpaired", t, func() {
		var client *RemoteClient

		_, err := client.SubmitFeedback(context.Background(), submitFeedbackFullSubmissionForTest())

		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
	})
}

func submitFeedbackMatchedSentinelsForTest(err error) []error {
	var matched []error

	for _, sentinel := range submitFeedbackSentinelsForTest() {
		if errors.Is(err, sentinel) {
			matched = append(matched, sentinel)
		}
	}

	return matched
}

func submitFeedbackSentinelsForTest() []error {
	return []error{
		ErrFeedbackDisabled, ErrFeedbackInvalid, ErrFeedbackTooLarge,
		ErrFeedbackUnsupported, ErrFeedbackUnauthorized,
	}
}

func submitFeedbackErrorForTest(t *testing.T, status int, contentType, body string) error {
	t.Helper()

	server, _ := submitFeedbackStubForTest(t, status, contentType, body)
	_, err := submitFeedbackClientForTest(server.URL, "").
		SubmitFeedback(context.Background(), submitFeedbackFullSubmissionForTest())

	return err
}

func TestRemoteSubmitFeedbackStatusSentinels(t *testing.T) {
	convey.Convey("C1.3: Given 503 feedback_disabled, then ErrFeedbackDisabled with the envelope message", t, func() {
		err := submitFeedbackErrorForTest(t, http.StatusServiceUnavailable, "application/json",
			`{"code":"feedback_disabled","message":"feedback is disabled on this server"}`)

		convey.So(errors.Is(err, ErrFeedbackDisabled), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "feedback is disabled on this server")
	})

	convey.Convey("C1.4: Given a 404 with a text body, then ErrFeedbackUnsupported", t, func() {
		err := submitFeedbackErrorForTest(t, http.StatusNotFound, "text/plain", "404 page not found")

		convey.So(submitFeedbackMatchedSentinelsForTest(err), convey.ShouldResemble, []error{ErrFeedbackUnsupported})
		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
	})

	convey.Convey("C1.4: Given a 404 with a not_found envelope, then ErrFeedbackUnsupported by status alone", t, func() {
		err := submitFeedbackErrorForTest(t, http.StatusNotFound, "application/json",
			`{"code":"not_found","message":"no such route"}`)

		convey.So(errors.Is(err, ErrFeedbackUnsupported), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "no such route")
	})

	convey.Convey("C1.5: Given 400 bad_request, then ErrFeedbackInvalid with the message kept", t, func() {
		err := submitFeedbackErrorForTest(t, http.StatusBadRequest, "application/json",
			`{"code":"bad_request","message":"invalid category \"x\""}`)

		convey.So(submitFeedbackMatchedSentinelsForTest(err), convey.ShouldResemble, []error{ErrFeedbackInvalid})
		convey.So(err.Error(), convey.ShouldContainSubstring, `invalid category "x"`)
	})

	convey.Convey("C1.6: Given a 400 without a bad_request envelope, then not ErrFeedbackInvalid but "+
		"ErrUpstreamImpaired", t, func() {
		textErr := submitFeedbackErrorForTest(t, http.StatusBadRequest, "",
			"Client sent an HTTP request to an HTTPS server.\n")

		convey.So(errors.Is(textErr, ErrFeedbackInvalid), convey.ShouldBeFalse)
		convey.So(errors.Is(textErr, ErrUpstreamImpaired), convey.ShouldBeTrue)
		convey.So(textErr.Error(), convey.ShouldContainSubstring, "without a valid MLWH error envelope")

		for _, body := range []string{`{"message":"x"}`, `{"code":"bad_request","message":5}`} {
			err := submitFeedbackErrorForTest(t, http.StatusBadRequest, "application/json", body)

			convey.So(submitFeedbackMatchedSentinelsForTest(err), convey.ShouldBeEmpty)
			convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		}

		otherCodeErr := submitFeedbackErrorForTest(t, http.StatusBadRequest, "application/json",
			`{"code":"not_found","message":"x"}`)
		convey.So(submitFeedbackMatchedSentinelsForTest(otherCodeErr), convey.ShouldBeEmpty)
		convey.So(errors.Is(otherCodeErr, ErrNotFound), convey.ShouldBeTrue)
	})

	convey.Convey("C1.7: Given 413 payload_too_large, then ErrFeedbackTooLarge and ErrUpstreamImpaired", t, func() {
		err := submitFeedbackErrorForTest(t, http.StatusRequestEntityTooLarge, "application/json",
			`{"code":"payload_too_large","message":"description exceeds 16384 bytes"}`)

		convey.So(submitFeedbackMatchedSentinelsForTest(err), convey.ShouldResemble, []error{ErrFeedbackTooLarge})
		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "description exceeds 16384 bytes")
	})

	convey.Convey("C1.7: Given 401 with any body, then ErrFeedbackUnauthorized and ErrUpstreamImpaired", t, func() {
		for _, body := range []string{`{"code":401,"message":"x"}`, "unauthorized", ""} {
			err := submitFeedbackErrorForTest(t, http.StatusUnauthorized, "", body)

			convey.So(submitFeedbackMatchedSentinelsForTest(err), convey.ShouldResemble, []error{ErrFeedbackUnauthorized})
			convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		}

		envelopeErr := submitFeedbackErrorForTest(t, http.StatusUnauthorized, "application/json",
			`{"code":"unauthorized","message":"missing admin token"}`)
		convey.So(errors.Is(envelopeErr, ErrFeedbackUnauthorized), convey.ShouldBeTrue)
		convey.So(envelopeErr.Error(), convey.ShouldContainSubstring, "missing admin token")
	})

	convey.Convey("C1.7: Given 400 bad_request, then ErrUpstreamImpaired also matches", t, func() {
		err := submitFeedbackErrorForTest(t, http.StatusBadRequest, "application/json",
			`{"code":"bad_request","message":"invalid category \"x\""}`)

		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
	})
}

func TestRemoteSubmitFeedbackRoundTrip(t *testing.T) {
	convey.Convey("C1.11: Given the real NewServer with WithFeedback behind httptest, then SubmitFeedback returns "+
		"id 1 and the store holds the report", t, func() {
		gin.SetMode(gin.TestMode)

		store := newFeedbackServerTestStore(t)
		router := gin.New()
		NewServer(&serverFakeQueryer{}, WithFeedback(store, []byte("admin-token"))).RegisterRoutes(router, nil)

		server := httptest.NewServer(router)
		defer server.Close()

		submission := submitFeedbackFullSubmissionForTest()
		receipt, err := submitFeedbackClientForTest(server.URL, "").SubmitFeedback(context.Background(), submission)

		convey.So(err, convey.ShouldBeNil)
		convey.So(receipt.ID, convey.ShouldEqual, 1)
		convey.So(receipt.CreatedAt, convey.ShouldNotBeEmpty)

		page, err := store.List(context.Background(), FeedbackFilter{}, 10, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(page.Items, convey.ShouldHaveLength, 1)

		report := page.Items[0]
		convey.So(report.ID, convey.ShouldEqual, 1)
		convey.So(report.CreatedAt, convey.ShouldEqual, receipt.CreatedAt)
		convey.So(report.Category, convey.ShouldEqual, submission.Category)
		convey.So(report.Description, convey.ShouldEqual, submission.Description)
		convey.So(report.UserRequest, convey.ShouldEqual, submission.UserRequest)
		convey.So(report.ToolsTried, convey.ShouldResemble, submission.ToolsTried)
		convey.So(report.ClientUserAgent, convey.ShouldEqual, submission.ClientUserAgent)
		convey.So(report.RemoteAddr, convey.ShouldEqual, "127.0.0.1")
	})

	convey.Convey("C1.11: Given the real server with an invalid submission, then ErrFeedbackInvalid", t, func() {
		gin.SetMode(gin.TestMode)

		router := gin.New()
		NewServer(&serverFakeQueryer{}, WithFeedback(newFeedbackServerTestStore(t), []byte("admin-token"))).
			RegisterRoutes(router, nil)

		server := httptest.NewServer(router)
		defer server.Close()

		_, err := submitFeedbackClientForTest(server.URL, "").
			SubmitFeedback(context.Background(), FeedbackSubmission{Category: "bogus", Description: "d"})

		convey.So(errors.Is(err, ErrFeedbackInvalid), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, `invalid category "bogus"`)
	})
}
