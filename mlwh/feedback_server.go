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
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	feedbackDisabledMessage      = "feedback is disabled on this server"
	feedbackInvalidJSONMessage   = "invalid JSON body"
	feedbackStoreFailedMessage   = "could not store feedback"
	feedbackReceivedLogMessage   = "mlwh feedback received"
	feedbackPath                 = "/feedback"
	feedbackBodyTooLargeTemplate = "request body exceeds %d bytes"
	feedbackAdminTokenMessage    = "admin token required"
	feedbackAdminItemPath        = "/feedback/:id"
	feedbackListFailedMessage    = "could not list feedback"
	feedbackBearerPrefix         = "Bearer "
	feedbackDefaultListLimit     = 50
	feedbackMaxListLimit         = 500
	feedbackAcknowledgedParam    = "acknowledged"
	feedbackCategoryParam        = "category"
	feedbackInvalidIDMessage     = "invalid feedback id"
	feedbackAckNotBoolMessage    = "acknowledged must be a boolean"
	feedbackNotFoundTemplate     = "feedback %d not found"
	feedbackUpdateFailedMessage  = "could not update feedback"
	feedbackDeleteFailedMessage  = "could not delete feedback"

	// feedbackMaxPatchBodyBytes caps the PATCH /feedback/:id body.
	feedbackMaxPatchBodyBytes = 1024
)

// errFeedbackInvalidJSON marks a submit body that is not exactly one JSON
// object.
var errFeedbackInvalidJSON = errors.New(feedbackInvalidJSONMessage)

// feedbackIDParam parses the :id path parameter as a positive base-10 int64,
// writing a 400 bad_request envelope and returning false otherwise.
func feedbackIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeFeedbackError(c, http.StatusBadRequest, httpErrorCodeBadRequest, feedbackInvalidIDMessage)

		return 0, false
	}

	return id, true
}

// decodeFeedbackBody decodes exactly one JSON value from a body capped at
// maxBytes into target, writing a 413 or 400 envelope and returning false on
// failure.
func decodeFeedbackBody(c *gin.Context, maxBytes int64, target any) bool {
	tooLarge := fmt.Sprintf(feedbackBodyTooLargeTemplate, maxBytes)

	if c.Request.ContentLength > maxBytes {
		writeFeedbackError(c, http.StatusRequestEntityTooLarge, httpErrorCodePayloadTooLarge, tooLarge)

		return false
	}

	err := decodeSingleJSONValue(http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes), target)
	if err == nil {
		return true
	}

	if maxBytesErr := (*http.MaxBytesError)(nil); errors.As(err, &maxBytesErr) {
		writeFeedbackError(c, http.StatusRequestEntityTooLarge, httpErrorCodePayloadTooLarge, tooLarge)

		return false
	}

	writeFeedbackError(c, http.StatusBadRequest, httpErrorCodeBadRequest, feedbackInvalidJSONMessage)

	return false
}

// writeFeedbackStoreItemError writes 404 not_found naming id when err wraps
// ErrNotFound, else 500 internal_error with failedMessage.
func writeFeedbackStoreItemError(c *gin.Context, id int64, err error, failedMessage string) {
	if errors.Is(err, ErrNotFound) {
		writeFeedbackError(c, http.StatusNotFound, httpErrorCodeNotFound, fmt.Sprintf(feedbackNotFoundTemplate, id))

		return
	}

	writeFeedbackError(c, http.StatusInternalServerError, httpErrorCodeInternal, failedMessage)
}

func writeFeedbackValidationError(c *gin.Context, err error) {
	if errors.Is(err, ErrFeedbackTooLarge) {
		writeFeedbackError(c, http.StatusRequestEntityTooLarge, httpErrorCodePayloadTooLarge, err.Error())

		return
	}

	writeFeedbackError(c, http.StatusBadRequest, httpErrorCodeBadRequest, err.Error())
}

func writeFeedbackError(c *gin.Context, status int, code, message string) {
	c.JSON(status, httpErrorEnvelope{Code: code, Message: message})
}

// decodeSingleJSONValue decodes one JSON value into target and requires
// nothing but whitespace after it.
func decodeSingleJSONValue(body io.Reader, target any) error {
	decoder := json.NewDecoder(body)

	if err := decoder.Decode(target); err != nil {
		return err
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errFeedbackInvalidJSON
		}

		return err
	}

	return nil
}

// feedbackRemoteHost returns the host part of the connection's remote
// address, or the raw value when it cannot be split. Forwarded headers are
// never consulted.
func feedbackRemoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}

	return host
}

// feedbackAdminTokenMatches reports whether authorization is "Bearer " plus a
// value equal to token, comparing whitespace-trimmed bytes in constant time.
// A blank token never matches.
func feedbackAdminTokenMatches(authorization string, token []byte) bool {
	presented, ok := strings.CutPrefix(authorization, feedbackBearerPrefix)
	if !ok {
		return false
	}

	want := bytes.TrimSpace(token)
	if len(want) == 0 {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(presented)), want) == 1
}

// feedbackListQuery parses the list query parameters, writing a 400
// bad_request envelope naming the offending parameter and returning false on
// an invalid value. Absent or empty parameters take their defaults.
func feedbackListQuery(c *gin.Context) (FeedbackFilter, mlwhPagination, bool) {
	limit, ok := mlwhQueryInt(c, "limit", feedbackDefaultListLimit)
	if !ok {
		return FeedbackFilter{}, mlwhPagination{}, false
	}

	if limit < 0 || limit > feedbackMaxListLimit {
		writeMLWHBadRequest(c, fmt.Sprintf("limit must be between 0 and %d", feedbackMaxListLimit))

		return FeedbackFilter{}, mlwhPagination{}, false
	}

	offset, ok := mlwhQueryInt(c, "offset", 0)
	if !ok {
		return FeedbackFilter{}, mlwhPagination{}, false
	}

	if offset < 0 {
		writeMLWHBadRequest(c, "offset must not be negative")

		return FeedbackFilter{}, mlwhPagination{}, false
	}

	filter, ok := feedbackFilterFromQuery(c)

	return filter, mlwhPagination{limit: limit, offset: offset}, ok
}

// feedbackFilterFromQuery parses the acknowledged ("true" or "false") and
// category (an enum value) query parameters.
func feedbackFilterFromQuery(c *gin.Context) (FeedbackFilter, bool) {
	var filter FeedbackFilter

	switch raw := c.Query(feedbackAcknowledgedParam); raw {
	case "":
	case "true", "false":
		acknowledged := raw == "true"
		filter.Acknowledged = &acknowledged
	default:
		writeMLWHBadRequest(c, fmt.Sprintf("invalid %s %q", feedbackAcknowledgedParam, raw))

		return FeedbackFilter{}, false
	}

	filter.Category = FeedbackCategory(c.Query(feedbackCategoryParam))
	if filter.Category != "" && !filter.Category.Valid() {
		writeMLWHBadRequest(c, fmt.Sprintf("invalid %s %q", feedbackCategoryParam, filter.Category))

		return FeedbackFilter{}, false
	}

	return filter, true
}

// registerFeedbackAdminRoutes registers the admin list, acknowledge and delete
// routes, each behind requireFeedbackAdmin.
func (s *Server) registerFeedbackAdminRoutes(registrar mlwhRouteRegistrar) {
	registrar.Handle(http.MethodGet, feedbackPath, s.requireFeedbackAdmin, s.handleFeedbackList)
	registrar.Handle(http.MethodPatch, feedbackAdminItemPath, s.requireFeedbackAdmin, s.handleFeedbackAcknowledge)
	registrar.Handle(http.MethodDelete, feedbackAdminItemPath, s.requireFeedbackAdmin, s.handleFeedbackDelete)
}

// requireFeedbackAdmin aborts with 503 when feedback is disabled, else with
// 401 unless the request carries the admin Bearer token.
func (s *Server) requireFeedbackAdmin(c *gin.Context) {
	if s.feedback == nil {
		writeFeedbackError(c, http.StatusServiceUnavailable, httpErrorCodeFeedbackDisabled, feedbackDisabledMessage)
		c.Abort()

		return
	}

	if !feedbackAdminTokenMatches(c.GetHeader("Authorization"), s.feedbackAdminToken) {
		writeFeedbackError(c, http.StatusUnauthorized, httpErrorCodeUnauthorized, feedbackAdminTokenMessage)
		c.Abort()
	}
}

// handleFeedbackList returns one newest-first page of feedback reports matching
// the limit, offset, acknowledged and category query parameters.
func (s *Server) handleFeedbackList(c *gin.Context) {
	filter, pagination, ok := feedbackListQuery(c)
	if !ok {
		return
	}

	page, err := s.feedback.List(c.Request.Context(), filter, pagination.limit, pagination.offset)
	if err != nil {
		writeFeedbackError(c, http.StatusInternalServerError, httpErrorCodeInternal, feedbackListFailedMessage)

		return
	}

	c.JSON(http.StatusOK, page)
}

// handleFeedbackAcknowledge sets or clears one report's acknowledgement and
// returns the updated report. The body is read before the id is parsed, so an
// oversized body is 413 whatever the id.
func (s *Server) handleFeedbackAcknowledge(c *gin.Context) {
	var patch struct {
		Acknowledged json.RawMessage `json:"acknowledged"`
	}

	if !decodeFeedbackBody(c, feedbackMaxPatchBodyBytes, &patch) {
		return
	}

	id, ok := feedbackIDParam(c)
	if !ok {
		return
	}

	var acknowledged *bool
	if json.Unmarshal(patch.Acknowledged, &acknowledged) != nil || acknowledged == nil {
		writeFeedbackError(c, http.StatusBadRequest, httpErrorCodeBadRequest, feedbackAckNotBoolMessage)

		return
	}

	report, err := s.feedback.SetAcknowledged(c.Request.Context(), id, *acknowledged)
	if err != nil {
		writeFeedbackStoreItemError(c, id, err, feedbackUpdateFailedMessage)

		return
	}

	c.JSON(http.StatusOK, report)
}

// handleFeedbackDelete removes one report, returning 204 with an empty body.
func (s *Server) handleFeedbackDelete(c *gin.Context) {
	id, ok := feedbackIDParam(c)
	if !ok {
		return
	}

	if err := s.feedback.Delete(c.Request.Context(), id); err != nil {
		writeFeedbackStoreItemError(c, id, err, feedbackDeleteFailedMessage)

		return
	}

	c.Status(http.StatusNoContent)
}

func (s *Server) registerFeedbackSubmitRoute(registrar mlwhRouteRegistrar) {
	registrar.Handle(http.MethodPost, feedbackPath, s.handleFeedbackSubmit)
}

// handleFeedbackSubmit stores one agent feedback report. Checks run in the
// wire-contract order: disabled, body size, JSON shape, validation, store.
func (s *Server) handleFeedbackSubmit(c *gin.Context) {
	if s.feedback == nil {
		writeFeedbackError(c, http.StatusServiceUnavailable, httpErrorCodeFeedbackDisabled, feedbackDisabledMessage)

		return
	}

	var submission FeedbackSubmission
	if !decodeFeedbackBody(c, FeedbackMaxBodyBytes, &submission) {
		return
	}

	if err := submission.Validate(); err != nil {
		writeFeedbackValidationError(c, err)

		return
	}

	addr := feedbackRemoteHost(c.Request.RemoteAddr)

	report, err := s.feedback.Add(c.Request.Context(), submission, addr)
	if err != nil {
		writeFeedbackError(c, http.StatusInternalServerError, httpErrorCodeInternal, feedbackStoreFailedMessage)

		return
	}

	slog.Default().Info(feedbackReceivedLogMessage,
		"id", report.ID, "category", string(report.Category), "remote_addr", addr)

	c.JSON(http.StatusCreated, FeedbackReceipt{ID: report.ID, CreatedAt: report.CreatedAt})
}
