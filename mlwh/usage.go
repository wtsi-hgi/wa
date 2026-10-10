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
	"strings"
	"time"
	"unicode/utf8"
)

// User-Agent product tokens sent by wa clients. A request from
// UserAgentProductRunDev is not counted.
const (
	UserAgentProductCLI            = "wa-cli"
	UserAgentProductMCP            = "mlwh-mcp-server"
	UserAgentProductWeb            = "wa-web"
	UserAgentProductResultsServer  = "wa-results-server"
	UserAgentProductMLWHDiffServer = "wa-mlwhdiff-server"
	UserAgentProductRunDev         = "wa-run-dev"
)

// Byte caps applied to caller-supplied usage fields before storage.
const (
	UsageMaxUserAgentBytes     = 512
	UsageMaxClientIDBytes      = 128
	UsageMaxUsernameBytes      = 128
	UsageMaxMCPTransportBytes  = 32
	UsageMaxMCPAgentBytes      = 256
	UsageMaxMCPToolBytes       = 128
	UsageMaxPathBytes          = 256
	UsageMaxClientVersionBytes = 64
)

// UsageCategory classifies the caller of a counted request.
type UsageCategory string

// The usage categories, in display order.
const (
	UsageCategoryCLI            UsageCategory = "cli"
	UsageCategoryMCP            UsageCategory = "mcp"
	UsageCategoryWeb            UsageCategory = "web"
	UsageCategoryResultsServer  UsageCategory = "results_server"
	UsageCategoryMLWHDiffServer UsageCategory = "mlwhdiff_server"
	UsageCategoryOther          UsageCategory = "other"
)

// UsageWindow is the length of a usage summary period.
type UsageWindow string

// The usage summary windows.
const (
	UsageWindow24h UsageWindow = "24h"
	UsageWindow7d  UsageWindow = "7d"
	UsageWindow30d UsageWindow = "30d"
	UsageWindowAll UsageWindow = "all"
)

var usageProductCategories = map[string]UsageCategory{
	UserAgentProductCLI:            UsageCategoryCLI,
	UserAgentProductMCP:            UsageCategoryMCP,
	UserAgentProductWeb:            UsageCategoryWeb,
	UserAgentProductResultsServer:  UsageCategoryResultsServer,
	UserAgentProductMLWHDiffServer: UsageCategoryMLWHDiffServer,
}

// ParseUsageUserAgent returns the category and version for ua, and
// counted=false for product wa-run-dev. The product is the text before the
// first "/" or space, matched exactly and case-sensitively; the version is the
// text after the first "/" up to the next space, truncated to
// UsageMaxClientVersionBytes.
func ParseUsageUserAgent(ua string) (category UsageCategory, version string, counted bool) {
	product := ua
	if i := strings.IndexAny(ua, "/ "); i >= 0 {
		product = ua[:i]
	}

	if product == UserAgentProductRunDev {
		return "", "", false
	}

	category, ok := usageProductCategories[product]
	if !ok {
		category = UsageCategoryOther
	}

	if _, after, found := strings.Cut(ua, "/"); found {
		version, _, _ = strings.Cut(after, " ")
	}

	return category, truncateUTF8(version, UsageMaxClientVersionBytes), true
}

// UsageCategories returns the six categories in display order.
func UsageCategories() []UsageCategory {
	return []UsageCategory{
		UsageCategoryCLI,
		UsageCategoryMCP,
		UsageCategoryWeb,
		UsageCategoryResultsServer,
		UsageCategoryMLWHDiffServer,
		UsageCategoryOther,
	}
}

// Valid reports whether w is one of the four windows.
func (w UsageWindow) Valid() bool {
	switch w {
	case UsageWindow24h, UsageWindow7d, UsageWindow30d, UsageWindowAll:
		return true
	default:
		return false
	}
}

// Duration returns 24h, 168h, or 720h for the bounded windows, and 0 for all
// and unknown values.
func (w UsageWindow) Duration() time.Duration {
	switch w {
	case UsageWindow24h:
		return 24 * time.Hour
	case UsageWindow7d:
		return 7 * 24 * time.Hour
	case UsageWindow30d:
		return 30 * 24 * time.Hour
	default:
		return 0
	}
}

// UsageWindows returns the four windows, shortest first, then all.
func UsageWindows() []UsageWindow {
	return []UsageWindow{UsageWindow24h, UsageWindow7d, UsageWindow30d, UsageWindowAll}
}

// UsageEvent is one counted request.
type UsageEvent struct {
	Time          time.Time
	Category      UsageCategory
	Method        string
	Route         string // registration template; "" when unmatched
	Path          string // URL path, only when Route == ""
	ClientID      string
	Username      string
	RemoteAddr    string
	ClientVersion string
	UserAgent     string
	MCPTransport  string
	MCPAgent      string
	MCPTool       string
	Status        int
	Latency       time.Duration
}

// truncateUTF8 returns s cut to at most maxBytes bytes, backing off to a rune
// boundary so the result stays valid UTF-8 when s was.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}

	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut]
}
