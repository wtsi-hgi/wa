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
	"testing"
	"time"
	"unicode/utf8"

	"github.com/smartystreets/goconvey/convey"
)

func TestUsageModelA1(t *testing.T) {
	convey.Convey("A1.1: UsageCategories lists the six categories in order", t, func() {
		convey.So(UsageCategories(), convey.ShouldResemble, []UsageCategory{
			"cli", "mcp", "web", "results_server", "mlwhdiff_server", "other",
		})
	})

	convey.Convey("A1.2: UsageWindows lists the four valid windows with their durations", t, func() {
		convey.So(UsageWindows(), convey.ShouldResemble, []UsageWindow{"24h", "7d", "30d", "all"})

		for _, w := range UsageWindows() {
			convey.So(w.Valid(), convey.ShouldBeTrue)
		}

		for _, w := range []UsageWindow{"1h", "", "ALL"} {
			convey.So(w.Valid(), convey.ShouldBeFalse)
			convey.So(w.Duration(), convey.ShouldEqual, time.Duration(0))
		}

		convey.So(UsageWindow24h.Duration(), convey.ShouldEqual, 24*time.Hour)
		convey.So(UsageWindow7d.Duration(), convey.ShouldEqual, 168*time.Hour)
		convey.So(UsageWindow30d.Duration(), convey.ShouldEqual, 720*time.Hour)
		convey.So(UsageWindowAll.Duration(), convey.ShouldEqual, time.Duration(0))
	})

	convey.Convey("A1.3: ParseUsageUserAgent maps product tokens to categories and versions", t, func() {
		cases := []struct {
			ua       string
			category UsageCategory
			version  string
		}{
			{"wa-cli/v0.11.0", UsageCategoryCLI, "v0.11.0"},
			{"mlwh-mcp-server/0.5.0", UsageCategoryMCP, "0.5.0"},
			{"wa-web", UsageCategoryWeb, ""},
			{"wa-results-server/v0.11.0", UsageCategoryResultsServer, "v0.11.0"},
			{"wa-mlwhdiff-server/v0.11.0 (x)", UsageCategoryMLWHDiffServer, "v0.11.0"},
			{"curl/8.5.0", UsageCategoryOther, "8.5.0"},
			{"Go-http-client/1.1", UsageCategoryOther, "1.1"},
			{"", UsageCategoryOther, ""},
			{"WA-CLI/1", UsageCategoryOther, "1"},
			{"wa-cli-extra/1", UsageCategoryOther, "1"},
			{"wa-cli (x)", UsageCategoryCLI, ""},
		}

		for _, c := range cases {
			category, version, counted := ParseUsageUserAgent(c.ua)
			convey.So(category, convey.ShouldEqual, c.category)
			convey.So(version, convey.ShouldEqual, c.version)
			convey.So(counted, convey.ShouldBeTrue)
		}

		for _, ua := range []string{"wa-run-dev", "wa-run-dev/1", "wa-run-dev (x)"} {
			_, _, counted := ParseUsageUserAgent(ua)
			convey.So(counted, convey.ShouldBeFalse)
		}
	})

	convey.Convey("A1.4: ParseUsageUserAgent truncates the version to 64 bytes", t, func() {
		_, version, counted := ParseUsageUserAgent("wa-cli/" + strings.Repeat("x", 100))
		convey.So(counted, convey.ShouldBeTrue)
		convey.So(version, convey.ShouldEqual, strings.Repeat("x", UsageMaxClientVersionBytes))
		convey.So(len(version), convey.ShouldEqual, 64)
	})

	convey.Convey("A1.5: truncateUTF8 keeps short strings and backs off to a rune boundary", t, func() {
		convey.So(truncateUTF8("abc", 5), convey.ShouldEqual, "abc")
		convey.So(truncateUTF8("abc", 3), convey.ShouldEqual, "abc")

		prefix := strings.Repeat("a", 127)
		got := truncateUTF8(prefix+"\xc3\xa9", 128)
		convey.So(got, convey.ShouldEqual, prefix)
		convey.So(utf8.ValidString(got), convey.ShouldBeTrue)
	})
}

func FuzzTruncateUTF8(f *testing.F) {
	f.Add("abc", 5)
	f.Add("abc", 3)
	f.Add(strings.Repeat("a", 127)+"\xc3\xa9", 128)
	f.Add("é€😀", 0)
	f.Add("é€😀", 1)
	f.Add("é€😀", 4)
	f.Add("é€😀", 6)
	f.Add("", 3)

	f.Fuzz(func(t *testing.T, s string, maxBytes int) {
		if maxBytes < 0 || !utf8.ValidString(s) {
			return
		}

		got := truncateUTF8(s, maxBytes)

		if len(got) > maxBytes {
			t.Fatalf("truncateUTF8(%q, %d) = %q, longer than max", s, maxBytes, got)
		}

		if !strings.HasPrefix(s, got) {
			t.Fatalf("truncateUTF8(%q, %d) = %q, not a prefix", s, maxBytes, got)
		}

		if !utf8.ValidString(got) {
			t.Fatalf("truncateUTF8(%q, %d) = %q, invalid UTF-8", s, maxBytes, got)
		}

		if len(s) <= maxBytes && got != s {
			t.Fatalf("truncateUTF8(%q, %d) = %q, want unchanged", s, maxBytes, got)
		}

		if len(s) > maxBytes && maxBytes-len(got) >= utf8.UTFMax {
			t.Fatalf("truncateUTF8(%q, %d) = %q, cut more than one rune", s, maxBytes, got)
		}
	})
}
