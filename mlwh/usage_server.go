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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	usagePath                 = "/usage"
	usageWindowParam          = "window"
	usageDisabledMessage      = "usage tracking is disabled on this server"
	usageSummaryFailedMessage = "could not summarise usage"
)

// usageRedactedQueryNames are substrings of normalised query parameter names
// (see normaliseUsageQueryName) whose values are never stored. No MLWH
// endpoint takes credentials in the query, but a misconfigured client could
// send them.
var usageRedactedQueryNames = []string{
	"token", "password", "passwd", "secret", "apikey", "authorization", "credential",
	"auth", "jwt", "bearer", "session", "pwd", "pass", "key", "signature", "privatekey",
}

const usageRedactedValue = "[redacted]"

// usageErrorCodeUncoded is stored for an error response no error writer
// produced, so it reads differently from rows recorded before codes were.
const usageErrorCodeUncoded = "uncoded"

// usageErrorContextKey is the gin context key of the usageErrorDetail that the
// error writers note for usageMiddleware.
type usageErrorContextKey struct{}

// setUsageError notes the error envelope's code and message on c.
func setUsageError(c *gin.Context, code, message string) {
	detail := usageErrorFromContext(c)
	detail.code, detail.message = code, message
	c.Set(usageErrorContextKey{}, detail)
}

func usageErrorFromContext(c *gin.Context) usageErrorDetail {
	value, _ := c.Get(usageErrorContextKey{})
	detail, _ := value.(usageErrorDetail)

	return detail
}

// setUsageErrorKind notes on c the cause of err, answered with status, that
// its public error code hides: a deadline (timeout) or the client going away
// (canceled).
func setUsageErrorKind(c *gin.Context, status int, err error) {
	detail := usageErrorFromContext(c)
	detail.kind = usageErrorKind(c.Request.Context(), status, err)
	c.Set(usageErrorContextKey{}, detail)
}

// usageErrorKind classifies err. For a 5xx status it falls back to the
// request context's state, because a driver may report a cancelled query
// without wrapping its cause; a 4xx answer is the request's own fault however
// the client's context ended.
func usageErrorKind(ctx context.Context, status int, err error) string {
	causes := []error{err}
	if status >= http.StatusInternalServerError {
		causes = append(causes, ctx.Err())
	}

	for _, cause := range causes {
		switch {
		case errors.Is(cause, context.DeadlineExceeded):
			return UsageErrorKindTimeout
		case errors.Is(cause, context.Canceled):
			return UsageErrorKindCanceled
		}
	}

	return ""
}

type usageErrorDetail struct {
	code, kind, message string
}

// addUsageErrorDetails sets ev's error code, kind, message, and query from
// what the error writers noted on c. Unmatched requests get code no_route
// and, like their successful counterparts, no query: their parameters are
// unknown to the server.
func addUsageErrorDetails(c *gin.Context, route string, ev *UsageEvent) {
	if route == "" {
		ev.ErrorCode = UsageErrorCodeNoRoute

		return
	}

	detail := usageErrorFromContext(c)
	ev.ErrorCode = detail.code
	ev.ErrorKind = detail.kind
	ev.ErrorMessage = truncateUTF8(detail.message, UsageMaxErrorMessageBytes)
	ev.Query = truncateUTF8(redactUsageQuery(c.Request.URL.RawQuery), UsageMaxQueryBytes)

	if ev.ErrorCode == "" {
		ev.ErrorCode = usageErrorCodeUncoded
	}
}

// redactUsageQuery returns raw with the value of every parameter whose
// normalised name contains a usageRedactedQueryNames entry replaced. Both '&'
// and ';' separate parameters, since some clients and servers treat ';' as a
// separator.
func redactUsageQuery(raw string) string {
	if raw == "" {
		return ""
	}

	var redacted strings.Builder

	for raw != "" {
		end := strings.IndexAny(raw, "&;")
		if end < 0 {
			end = len(raw)
		}

		redacted.WriteString(redactUsageQueryParam(raw[:end]))

		if end < len(raw) {
			redacted.WriteByte(raw[end])
			end++
		}

		raw = raw[end:]
	}

	return redacted.String()
}

func redactUsageQueryParam(param string) string {
	name, _, hasValue := strings.Cut(param, "=")
	if !hasValue {
		return param
	}

	normalised := normaliseUsageQueryName(name)
	if slices.ContainsFunc(usageRedactedQueryNames, func(s string) bool { return strings.Contains(normalised, s) }) {
		return name + "=" + usageRedactedValue
	}

	return param
}

// normaliseUsageQueryName unescapes name, lower-cases it, and strips '-' and
// '_', so API-KEY, x_api_key and apiKey all read as apikey.
func normaliseUsageQueryName(name string) string {
	unescaped, err := url.QueryUnescape(name)
	if err != nil {
		unescaped = name
	}

	return strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(unescaped))
}

// countedHandlers returns handlers with usageMiddleware(route) prepended when
// usage is enabled, else handlers unchanged.
func (s *Server) countedHandlers(route string, handlers ...gin.HandlerFunc) []gin.HandlerFunc {
	if s.usage == nil {
		return handlers
	}

	return append([]gin.HandlerFunc{s.usageMiddleware(route)}, handlers...)
}

// usageMiddleware records one UsageEvent per request after the remaining
// handlers run. route is the registration template, or "" for unmatched
// requests, which store their URL path instead. Header values are cut to
// their caps before queueing so a full queue cannot hold oversized strings.
// Requests whose User-Agent is not counted record nothing.
func (s *Server) usageMiddleware(route string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		userAgent := c.GetHeader("User-Agent")

		category, version, counted := ParseUsageUserAgent(userAgent)
		if !counted {
			return
		}

		event := UsageEvent{
			Time:          start,
			Category:      category,
			Method:        truncateUTF8(c.Request.Method, usageMaxMethodBytes),
			Route:         route,
			ClientID:      truncateUTF8(c.GetHeader(HeaderClientID), UsageMaxClientIDBytes),
			Username:      truncateUTF8(c.GetHeader(HeaderClientUser), UsageMaxUsernameBytes),
			RemoteAddr:    remoteHost(c.Request.RemoteAddr),
			ClientVersion: version,
			UserAgent:     truncateUTF8(userAgent, UsageMaxUserAgentBytes),
			MCPTransport:  truncateUTF8(c.GetHeader(HeaderMCPTransport), UsageMaxMCPTransportBytes),
			MCPAgent:      truncateUTF8(c.GetHeader(HeaderMCPAgent), UsageMaxMCPAgentBytes),
			MCPTool:       truncateUTF8(c.GetHeader(HeaderMCPTool), UsageMaxMCPToolBytes),
			Status:        c.Writer.Status(),
			Latency:       time.Since(start),
		}

		if route == "" || event.Status >= http.StatusBadRequest {
			event.Path = truncateUTF8(c.Request.URL.Path, UsageMaxPathBytes)
		}

		if event.Status >= http.StatusBadRequest {
			addUsageErrorDetails(c, route, &event)
		}

		s.usage.Record(event)
	}
}

// registerUsageAdminRoute registers GET /usage behind requireUsageAdmin, with
// no usage middleware.
func (s *Server) registerUsageAdminRoute(registrar mlwhRouteRegistrar) {
	registrar.Handle(http.MethodGet, usagePath, s.requireUsageAdmin, s.handleUsageSummary)
}

// requireUsageAdmin aborts with 503 when usage is disabled, else with 401
// unless the request carries the admin Bearer token.
func (s *Server) requireUsageAdmin(c *gin.Context) {
	if s.usage == nil {
		writeErrorEnvelope(c, http.StatusServiceUnavailable, httpErrorCodeUsageDisabled, usageDisabledMessage)
		c.Abort()

		return
	}

	if !adminTokenMatches(c.GetHeader("Authorization"), s.usageAdminToken) {
		writeErrorEnvelope(c, http.StatusUnauthorized, httpErrorCodeUnauthorized, adminTokenMessage)
		c.Abort()
	}
}

// handleUsageSummary returns the usage summary for the window query parameter,
// which defaults to 7d when absent or empty.
func (s *Server) handleUsageSummary(c *gin.Context) {
	window := UsageWindow(c.Query(usageWindowParam))
	if window == "" {
		window = UsageWindow7d
	}

	summary, err := s.usage.Summary(c.Request.Context(), window)

	switch {
	case errors.Is(err, ErrUsageWindowInvalid):
		writeMLWHBadRequest(c, fmt.Sprintf("invalid %s %q", usageWindowParam, window))
	case err != nil:
		writeErrorEnvelope(c, http.StatusInternalServerError, httpErrorCodeInternal, usageSummaryFailedMessage)
	default:
		c.JSON(http.StatusOK, summary)
	}
}
