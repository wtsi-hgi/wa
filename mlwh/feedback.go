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
	"fmt"
	"strings"
)

// Submission caps. String caps count bytes of the UTF-8 string, not runes.
const (
	FeedbackMaxBodyBytes        = 65536
	FeedbackMaxDescriptionBytes = 16384
	FeedbackMaxUserRequestBytes = 16384
	FeedbackMaxToolsTried       = 50
	FeedbackMaxToolNameBytes    = 128
	FeedbackMaxMetaBytes        = 256
)

// FeedbackCategory classifies the problem an agent reports.
type FeedbackCategory string

// The feedback categories, in their canonical order.
const (
	FeedbackCategoryCouldNotAnswer FeedbackCategory = "could_not_answer"
	FeedbackCategoryAgentMistake   FeedbackCategory = "agent_mistake"
	FeedbackCategoryNoEndpoint     FeedbackCategory = "no_endpoint"
	FeedbackCategoryUserUnhappy    FeedbackCategory = "user_unhappy"
	FeedbackCategoryOther          FeedbackCategory = "other"
)

var feedbackCategoryDescriptions = map[FeedbackCategory]string{
	FeedbackCategoryCouldNotAnswer: "the agent could not work out how to answer the request",
	FeedbackCategoryAgentMistake:   "the agent made a mistake while answering",
	FeedbackCategoryNoEndpoint:     "no endpoint could possibly answer the question",
	FeedbackCategoryUserUnhappy:    "the user was unhappy with the answer",
	FeedbackCategoryOther:          "any other problem with the request",
}

// Description returns the human description of c; "" for unknown values.
func (c FeedbackCategory) Description() string {
	return feedbackCategoryDescriptions[c]
}

// Valid reports whether c is one of the five categories.
func (c FeedbackCategory) Valid() bool {
	_, ok := feedbackCategoryDescriptions[c]

	return ok
}

// FeedbackCategories returns the five categories in canonical order.
func FeedbackCategories() []FeedbackCategory {
	return []FeedbackCategory{
		FeedbackCategoryCouldNotAnswer,
		FeedbackCategoryAgentMistake,
		FeedbackCategoryNoEndpoint,
		FeedbackCategoryUserUnhappy,
		FeedbackCategoryOther,
	}
}

// FeedbackSubmission is a problem report sent by an agent.
type FeedbackSubmission struct {
	Category         FeedbackCategory `json:"category"                    doc:"problem category"`
	Description      string           `json:"description"                 doc:"the agent's own description of the problem"`
	UserRequest      string           `json:"user_request,omitempty"      doc:"the user's original request, verbatim"`
	ToolsTried       []string         `json:"tools_tried,omitempty"       doc:"MCP tools the agent tried"`
	MCPServerVersion string           `json:"mcp_server_version,omitempty" doc:"reporting MCP server build version"`
	WAAPIVersion     string           `json:"wa_api_version,omitempty"    doc:"MLWH API version the MCP server targets"`
	Transport        string           `json:"transport,omitempty"         doc:"MCP transport: stdio or http"`
	ClientName       string           `json:"client_name,omitempty"       doc:"agent application name from the MCP handshake"`
	ClientVersion    string           `json:"client_version,omitempty"    doc:"agent application version from the MCP handshake"`
	ClientUserAgent  string           `json:"client_user_agent,omitempty" doc:"HTTP User-Agent of the agent application (MCP HTTP mode)"`
}

// Validate returns nil, or an error wrapping ErrFeedbackTooLarge (any cap) or
// ErrFeedbackInvalid (enum, blank description). Caps are checked first, then
// category, then description. The error message names the offending field.
func (s FeedbackSubmission) Validate() error {
	if err := s.validateCaps(); err != nil {
		return err
	}

	if !s.Category.Valid() {
		return newFeedbackValidationError(ErrFeedbackInvalid, "invalid category %q", string(s.Category))
	}

	if strings.TrimSpace(s.Description) == "" {
		return newFeedbackValidationError(ErrFeedbackInvalid, "description is required")
	}

	return nil
}

func newFeedbackValidationError(sentinel error, format string, args ...any) error {
	return &feedbackValidationError{sentinel: sentinel, message: fmt.Sprintf(format, args...)}
}

func (s FeedbackSubmission) validateCaps() error {
	if err := checkFeedbackFieldCap("description", s.Description, FeedbackMaxDescriptionBytes); err != nil {
		return err
	}

	if err := checkFeedbackFieldCap("user_request", s.UserRequest, FeedbackMaxUserRequestBytes); err != nil {
		return err
	}

	if err := validateFeedbackToolsTried(s.ToolsTried); err != nil {
		return err
	}

	for _, meta := range []struct{ name, value string }{
		{"mcp_server_version", s.MCPServerVersion},
		{"wa_api_version", s.WAAPIVersion},
		{"transport", s.Transport},
		{"client_name", s.ClientName},
		{"client_version", s.ClientVersion},
		{"client_user_agent", s.ClientUserAgent},
	} {
		if err := checkFeedbackFieldCap(meta.name, meta.value, FeedbackMaxMetaBytes); err != nil {
			return err
		}
	}

	return nil
}

func checkFeedbackFieldCap(name, value string, maxBytes int) error {
	if len(value) <= maxBytes {
		return nil
	}

	return newFeedbackValidationError(ErrFeedbackTooLarge, "%s exceeds %d bytes", name, maxBytes)
}

func validateFeedbackToolsTried(tools []string) error {
	if len(tools) > FeedbackMaxToolsTried {
		return newFeedbackValidationError(ErrFeedbackTooLarge,
			"tools_tried exceeds %d items", FeedbackMaxToolsTried)
	}

	for i, tool := range tools {
		if len(tool) > FeedbackMaxToolNameBytes {
			return newFeedbackValidationError(ErrFeedbackTooLarge,
				"tools_tried[%d] exceeds %d bytes", i, FeedbackMaxToolNameBytes)
		}
	}

	return nil
}

// feedbackValidationError carries a client-facing message naming the
// offending field, and wraps ErrFeedbackTooLarge or ErrFeedbackInvalid.
type feedbackValidationError struct {
	sentinel error
	message  string
}

func (e *feedbackValidationError) Error() string { return e.message }

func (e *feedbackValidationError) Unwrap() error { return e.sentinel }

// FeedbackReceipt confirms a stored submission.
type FeedbackReceipt struct {
	ID        int64  `json:"id"         doc:"feedback report id"`
	CreatedAt string `json:"created_at" doc:"time wa received the report (UTC RFC3339)"`
}

// FeedbackReport is a stored report. Every field is always present in JSON;
// absent optional strings are "" and ToolsTried is [] (never null).
type FeedbackReport struct {
	ID               int64            `json:"id"`
	CreatedAt        string           `json:"created_at"`
	Category         FeedbackCategory `json:"category"`
	Description      string           `json:"description"`
	UserRequest      string           `json:"user_request"`
	ToolsTried       []string         `json:"tools_tried"`
	MCPServerVersion string           `json:"mcp_server_version"`
	WAAPIVersion     string           `json:"wa_api_version"`
	Transport        string           `json:"transport"`
	ClientName       string           `json:"client_name"`
	ClientVersion    string           `json:"client_version"`
	ClientUserAgent  string           `json:"client_user_agent"`
	RemoteAddr       string           `json:"remote_addr"`
	Acknowledged     bool             `json:"acknowledged"`
	AcknowledgedAt   string           `json:"acknowledged_at"`
}

// FeedbackFilter selects which stored reports to list.
type FeedbackFilter struct {
	Acknowledged *bool            // nil: both states
	Category     FeedbackCategory // "": all categories
}
