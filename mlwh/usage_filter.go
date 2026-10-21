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
	"slices"
	"strings"
)

// ErrUsageFilterInvalid is wrapped by Summary errors for an invalid
// UsageFilter.
var ErrUsageFilterInvalid = errors.New("mlwh: invalid usage filter")

// usageFilterValueMaxBytes caps each UsageFilter value at its column's
// stored cap, since a longer value can match no row.
var usageFilterValueMaxBytes = map[string]int{
	"user":   UsageMaxUsernameBytes,
	"tool":   UsageMaxMCPToolBytes,
	"agent":  UsageMaxMCPAgentBytes,
	"method": usageMaxMethodBytes,
	"route":  UsageMaxPathBytes,
}

// UsageFilter narrows a UsageSummary to the rows matching every non-empty
// field. Agent matches an MCP agent exactly or by its product, the text
// before its first "/", so "claude-code" matches every claude-code version.
// Method and Route select one matched route and are set together.
type UsageFilter struct {
	Category UsageCategory `json:"category,omitempty"`
	User     string        `json:"user,omitempty"`
	Tool     string        `json:"tool,omitempty"`
	Agent    string        `json:"agent,omitempty"`
	Method   string        `json:"method,omitempty"`
	Route    string        `json:"route,omitempty"`
}

// Validate returns an error wrapping ErrUsageFilterInvalid unless Category
// is empty or one of UsageCategories, Method and Route are both set or both
// empty, Route starts with "/", and every value fits its column's cap.
func (f UsageFilter) Validate() error {
	if f.Category != "" && !slices.Contains(UsageCategories(), f.Category) {
		return fmt.Errorf("%w: unknown category %q", ErrUsageFilterInvalid, f.Category)
	}

	if (f.Method == "") != (f.Route == "") {
		return fmt.Errorf("%w: method and route must be given together", ErrUsageFilterInvalid)
	}

	if f.Route != "" && !strings.HasPrefix(f.Route, "/") {
		return fmt.Errorf("%w: route must start with /", ErrUsageFilterInvalid)
	}

	values := map[string]string{
		"user": f.User, "tool": f.Tool, "agent": f.Agent, "method": f.Method, "route": f.Route,
	}

	for name, value := range values {
		if len(value) > usageFilterValueMaxBytes[name] {
			return fmt.Errorf("%w: %s is longer than %d bytes", ErrUsageFilterInvalid, name,
				usageFilterValueMaxBytes[name])
		}
	}

	return nil
}

// sql returns the filter as a boolean SQL expression over usage_requests
// columns, "" when no field is set, and the values its placeholders bind.
// Only these fixed column terms are ever written into the SQL; every value
// is a bound parameter.
func (f UsageFilter) sql() (string, []any) {
	var (
		terms []string
		args  []any
	)

	add := func(term string, values ...any) {
		terms = append(terms, term)
		args = append(args, values...)
	}

	if f.Category != "" {
		add("category = ?", string(f.Category))
	}

	if f.User != "" {
		add("username = ?", f.User)
	}

	if f.Tool != "" {
		add("mcp_tool = ?", f.Tool)
	}

	if f.Agent != "" {
		add("(mcp_agent = ? OR substr(mcp_agent, 1, length(?) + 1) = ? || '/')", f.Agent, f.Agent, f.Agent)
	}

	if f.Method != "" {
		add("method = ? AND route = ?", f.Method, f.Route)
	}

	return strings.Join(terms, " AND "), args
}
