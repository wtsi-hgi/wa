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
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	usagePath                 = "/usage"
	usageWindowParam          = "window"
	usageDisabledMessage      = "usage tracking is disabled on this server"
	usageSummaryFailedMessage = "could not summarise usage"
)

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
			RemoteAddr:    feedbackRemoteHost(c.Request.RemoteAddr),
			ClientVersion: version,
			UserAgent:     truncateUTF8(userAgent, UsageMaxUserAgentBytes),
			MCPTransport:  truncateUTF8(c.GetHeader(HeaderMCPTransport), UsageMaxMCPTransportBytes),
			MCPAgent:      truncateUTF8(c.GetHeader(HeaderMCPAgent), UsageMaxMCPAgentBytes),
			MCPTool:       truncateUTF8(c.GetHeader(HeaderMCPTool), UsageMaxMCPToolBytes),
			Status:        c.Writer.Status(),
			Latency:       time.Since(start),
		}

		if route == "" {
			event.Path = truncateUTF8(c.Request.URL.Path, UsageMaxPathBytes)
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

	if !feedbackAdminTokenMatches(c.GetHeader("Authorization"), s.usageAdminToken) {
		writeErrorEnvelope(c, http.StatusUnauthorized, httpErrorCodeUnauthorized, feedbackAdminTokenMessage)
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
