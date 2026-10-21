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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestUsageFilterValidate(t *testing.T) {
	convey.Convey("Given usage filters", t, func() {
		valid := []UsageFilter{
			{},
			{Category: UsageCategoryMCP},
			{User: "alice", Tool: "mlwh_export", Agent: "claude-code"},
			{Method: "GET", Route: "/studies"},
		}

		for _, filter := range valid {
			convey.So(filter.Validate(), convey.ShouldBeNil)
		}

		invalid := map[string]UsageFilter{
			"unknown category":       {Category: "cli' OR '1'='1"},
			"method without a route": {Method: "GET"},
			"route without a method": {Route: "/studies"},
			"route without a slash":  {Method: "GET", Route: "studies"},
			"oversized user":         {User: strings.Repeat("u", UsageMaxUsernameBytes+1)},
			"oversized tool":         {Tool: strings.Repeat("t", UsageMaxMCPToolBytes+1)},
			"oversized agent":        {Agent: strings.Repeat("a", UsageMaxMCPAgentBytes+1)},
			"oversized method":       {Method: strings.Repeat("M", usageMaxMethodBytes+1), Route: "/"},
			"oversized route":        {Method: "GET", Route: "/" + strings.Repeat("r", UsageMaxPathBytes)},
		}

		for name, filter := range invalid {
			convey.Convey("then a filter with an "+name+" is invalid", func() {
				convey.So(filter.Validate(), convey.ShouldWrap, ErrUsageFilterInvalid)
			})
		}

		convey.Convey("then Summary rejects an invalid filter", func() {
			store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 365, usageQueueSize,
				usageSummaryTestNow)

			_, err := store.Summary(context.Background(), UsageWindow7d, UsageFilter{Category: "nope"})
			convey.So(err, convey.ShouldWrap, ErrUsageFilterInvalid)
		})
	})
}

func TestUsageSummaryFilters(t *testing.T) {
	filtered := func(w UsageWindow, filter UsageFilter) UsageSummary {
		return summariseFilteredEventsForTest(t, usageFixtureFForTest(), w, filter)
	}

	convey.Convey("Given F and 24h filtered to category cli", t, func() {
		summary := filtered(UsageWindow24h, UsageFilter{Category: UsageCategoryCLI})

		convey.Convey("then the filter is echoed and every aggregate counts only cli rows", func() {
			convey.So(summary.Filter, convey.ShouldResemble, UsageFilter{Category: UsageCategoryCLI})
			convey.So(summary.Totals.Requests, convey.ShouldEqual, 3)
			convey.So(summary.Totals.Users, convey.ShouldEqual, 2)
			convey.So(summary.Totals.ErrorSeverity, convey.ShouldResemble,
				usageBreakdownOfOneForTest(UsageErrorSeverityExpected, false))
			convey.So(summary.Categories[1].Requests, convey.ShouldEqual, 0)
			convey.So(summary.Categories[5].Requests, convey.ShouldEqual, 0)
			convey.So(len(summary.Endpoints), convey.ShouldEqual, 2)
			convey.So(summary.Endpoints[0].Requests, convey.ShouldEqual, 2)
			convey.So(summary.Endpoints[0].ByCategory, convey.ShouldResemble, map[string]int64{"cli": 2})
			convey.So(summary.MCPTools, convey.ShouldResemble, []UsageCount{})
			convey.So(summary.OtherUserAgents, convey.ShouldResemble, []UsageCount{})
			convey.So(summary.Unmatched, convey.ShouldResemble, []UsageUnmatched{})
			convey.So(len(summary.Users), convey.ShouldEqual, 2)
			convey.So(len(summary.Clients), convey.ShouldEqual, 2)
			convey.So(len(summary.Errors), convey.ShouldEqual, 1)
			convey.So(summary.Errors[0].Route, convey.ShouldEqual, "/classify/:id")
			convey.So(summary.Days[1].ByCategory, convey.ShouldResemble, map[string]int64{"cli": 3})
			convey.So(summary.Buckets[23].ByCategory, convey.ShouldResemble, map[string]int64{"cli": 1})
			convey.So(summary.Buckets[19].Requests, convey.ShouldEqual, 0)

			last := summary.Series[len(summary.Series)-1]
			convey.So(last.ByCategory, convey.ShouldResemble, map[string]int64{"cli": 3})
			convey.So(last.ByUser, convey.ShouldResemble, map[string]int64{"alice": 2, "bob": 1})
			convey.So(last.ByTool, convey.ShouldResemble, map[string]int64{})
			convey.So(summary.Series[0].Requests, convey.ShouldEqual, 1)
			convey.So(summary.Series[7].Requests, convey.ShouldEqual, 0)
		})

		convey.Convey("then earliest_record still reports the store's first row", func() {
			convey.So(summary.EarliestRecord, convey.ShouldEqual, "2026-10-01T00:00:00Z")
		})
	})

	convey.Convey("Given F and 24h, then each filter field selects its rows", t, func() {
		cases := []struct {
			filter   UsageFilter
			requests int64
		}{
			{UsageFilter{User: "alice"}, 2},
			{UsageFilter{User: "carol"}, 0},
			{UsageFilter{Tool: "mlwh_list_studies"}, 1},
			{UsageFilter{Agent: usageSummaryTestAgent}, 1},
			{UsageFilter{Agent: "claude-code"}, 1},
			{UsageFilter{Agent: "claude"}, 0},
			{UsageFilter{Agent: "claude-code/2"}, 0},
			{UsageFilter{Agent: "claude%"}, 0},
			{UsageFilter{Agent: "_laude-code"}, 0},
			{UsageFilter{Method: "GET", Route: usageSummaryTestRouteStudies}, 4},
			{UsageFilter{Method: "POST", Route: usageSummaryTestRouteStudies}, 0},
			{UsageFilter{Category: UsageCategoryOther, Method: "GET", Route: usageSummaryTestRouteStudies}, 1},
			{UsageFilter{Category: UsageCategoryCLI, User: "bob"}, 1},
		}

		for _, tc := range cases {
			summary := filtered(UsageWindow24h, tc.filter)
			convey.So(summary.Totals.Requests, convey.ShouldEqual, tc.requests)
			convey.So(usageSumBucketsForTest(summary.Buckets), convey.ShouldEqual, tc.requests)
		}
	})

	convey.Convey("Given F and 24h filtered to one route, then unmatched rows and other routes drop out", t, func() {
		summary := filtered(UsageWindow24h, UsageFilter{Method: "GET", Route: usageSummaryTestRouteStudies})

		convey.So(len(summary.Endpoints), convey.ShouldEqual, 1)
		convey.So(summary.Endpoints[0].P95Ms, convey.ShouldEqual, 40)
		convey.So(summary.Unmatched, convey.ShouldResemble, []UsageUnmatched{})
		convey.So(summary.Totals.ErrorSeverity, convey.ShouldResemble,
			usageBreakdownOfOneForTest(UsageErrorSeverityFault, true))
	})

	convey.Convey("Given a filter on one category, then its route's latency ranks only that category's rows", t,
		func() {
			events := []UsageEvent{latencyUsageEventForTest(1), latencyUsageEventForTest(2), latencyUsageEventForTest(3)}
			events[2].Category = UsageCategoryOther

			summary := summariseFilteredEventsForTest(t, events, UsageWindow24h,
				UsageFilter{Category: UsageCategoryCLI})
			convey.So(summary.Endpoints[0].P95Ms, convey.ShouldEqual, 2)
		})

	convey.Convey("Given values that look like SQL, then they are matched literally", t, func() {
		events := usageFixtureFForTest()
		events[0].Username = "o'brien"

		for _, user := range []string{"' OR '1'='1", "alice' --", "alice\" OR 1=1 --", "%"} {
			summary := summariseFilteredEventsForTest(t, events, UsageWindow24h, UsageFilter{User: user})
			convey.So(summary.Totals.Requests, convey.ShouldEqual, 0)
		}

		summary := summariseFilteredEventsForTest(t, events, UsageWindow24h, UsageFilter{User: "o'brien"})
		convey.So(summary.Totals.Requests, convey.ShouldEqual, 1)
	})
}

func TestUsagePreviousTotals(t *testing.T) {
	convey.Convey("Given F and 24h, then previous_totals count the 24h before the window", t, func() {
		summary := summariseFilteredEventsForTest(t, usageFixtureFForTest(), UsageWindow24h, UsageFilter{})

		convey.So(summary.PreviousTotals, convey.ShouldResemble, &UsageTotals{Requests: 1, Sessions: 1, Users: 1})
	})

	convey.Convey("Given F and all, then previous_totals is null", t, func() {
		summary := summariseFilteredEventsForTest(t, usageFixtureFForTest(), UsageWindowAll, UsageFilter{})
		convey.So(summary.PreviousTotals, convey.ShouldBeNil)

		doc, err := json.Marshal(summary)
		convey.So(err, convey.ShouldBeNil)
		convey.So(string(doc), convey.ShouldContainSubstring, `"previous_totals":null`)
	})

	convey.Convey("Given rows at the window edges, then each boundary row is in exactly one window", t, func() {
		at := func(d time.Duration, status int) UsageEvent {
			return UsageEvent{
				Time: usageSummaryTestNow.Add(-d), Category: UsageCategoryCLI, Method: "GET",
				Route: usageSummaryTestRouteStudies, Status: status, ErrorCode: httpErrorCodeUpstreamImpaired,
			}
		}

		summary := summariseEventsForTest(t, []UsageEvent{
			at(24*time.Hour, 502),
			at(24*time.Hour-time.Millisecond, 200),
			at(48*time.Hour, 200),
			at(48*time.Hour-time.Millisecond, 200),
		}, UsageWindow24h)

		convey.So(summary.Totals.Requests, convey.ShouldEqual, 1)
		convey.So(summary.PreviousTotals.Requests, convey.ShouldEqual, 2)
		convey.So(summary.PreviousTotals.ErrorSeverity, convey.ShouldResemble,
			usageBreakdownOfOneForTest(UsageErrorSeverityFault, false))
	})

	convey.Convey("Given F and 7d filtered to user carol, then previous_totals use the filter", t, func() {
		summary := summariseFilteredEventsForTest(t, usageFixtureFForTest(), UsageWindow7d,
			UsageFilter{User: "carol"})

		convey.So(summary.Totals.Requests, convey.ShouldEqual, 0)
		convey.So(summary.PreviousTotals.Requests, convey.ShouldEqual, 1)

		summary = summariseFilteredEventsForTest(t, usageFixtureFForTest(), UsageWindow7d, UsageFilter{User: "alice"})
		convey.So(summary.PreviousTotals.Requests, convey.ShouldEqual, 0)
	})
}

func TestUsageBuckets(t *testing.T) {
	convey.Convey("Given F and 24h, then buckets are the window's hours", t, func() {
		summary := summariseEventsForTest(t, usageFixtureFForTest(), UsageWindow24h)

		convey.So(summary.BucketUnit, convey.ShouldEqual, UsageBucketHour)
		convey.So(len(summary.Buckets), convey.ShouldEqual, 25)
		convey.So(summary.Buckets[0].Start, convey.ShouldEqual, "2026-10-09T12:00:00Z")
		convey.So(summary.Buckets[24].Start, convey.ShouldEqual, "2026-10-10T12:00:00Z")
		convey.So(summary.Buckets[23], convey.ShouldResemble, UsageBucket{
			Start: "2026-10-10T11:00:00Z", Requests: 1, ByCategory: map[string]int64{"cli": 1},
		})
		convey.So(summary.Buckets[19], convey.ShouldResemble, UsageBucket{
			Start: "2026-10-10T07:00:00Z", Requests: 1, ByCategory: map[string]int64{"other": 1},
			ErrorSeverity: usageBreakdownOfOneForTest(UsageErrorSeverityFault, true),
		})
		convey.So(summary.Buckets[0].ByCategory, convey.ShouldResemble, map[string]int64{})
		convey.So(usageSumBucketsForTest(summary.Buckets), convey.ShouldEqual, 6)
	})

	convey.Convey("Given F, then 7d, 30d, and a short all history are bucketed by day like days", t, func() {
		for _, w := range []UsageWindow{UsageWindow7d, UsageWindow30d, UsageWindowAll} {
			summary := summariseEventsForTest(t, usageFixtureFForTest(), w)

			convey.So(summary.BucketUnit, convey.ShouldEqual, UsageBucketDay)
			convey.So(len(summary.Buckets), convey.ShouldEqual, len(summary.Days))

			for i, day := range summary.Days {
				convey.So(summary.Buckets[i], convey.ShouldResemble, UsageBucket{
					Start: day.Date + "T00:00:00Z", Requests: day.Requests, ByCategory: day.ByCategory,
					ErrorSeverity: day.ErrorSeverity,
				})
			}
		}
	})

	convey.Convey("Given more than 120 days of history and all, then buckets are Monday-aligned weeks", t, func() {
		events := append(usageFixtureFForTest(), UsageEvent{
			Time: usageSummaryTestNow.Add(-130 * 24 * time.Hour), Category: UsageCategoryWeb, Method: "GET",
			Route: usageSummaryTestRouteStudies, Status: 200,
		})
		summary := summariseEventsForTest(t, events, UsageWindowAll)

		convey.So(summary.BucketUnit, convey.ShouldEqual, UsageBucketWeek)
		convey.So(usageSumBucketsForTest(summary.Buckets), convey.ShouldEqual, 10)

		earliest, err := time.Parse(time.RFC3339, summary.EarliestRecord)
		convey.So(err, convey.ShouldBeNil)

		var previous time.Time

		for i, bucket := range summary.Buckets {
			start, err := time.Parse(time.RFC3339, bucket.Start)
			convey.So(err, convey.ShouldBeNil)
			convey.So(start.Weekday(), convey.ShouldEqual, time.Monday)
			convey.So(start.Hour(), convey.ShouldEqual, 0)

			if i > 0 {
				convey.So(start.Sub(previous), convey.ShouldEqual, 7*24*time.Hour)
			}

			previous = start
		}

		first, _ := time.Parse(time.RFC3339, summary.Buckets[0].Start)
		convey.So(earliest.Before(first), convey.ShouldBeFalse)
		convey.So(earliest.Before(first.Add(7*24*time.Hour)), convey.ShouldBeTrue)
		convey.So(usageSummaryTestNow.Before(previous.Add(7*24*time.Hour)), convey.ShouldBeTrue)
		convey.So(summary.Buckets[0].ByCategory, convey.ShouldResemble, map[string]int64{"web": 1})
	})

	convey.Convey("Given an empty store, then all has no buckets and 7d has 8 empty days", t, func() {
		all := summariseEventsForTest(t, nil, UsageWindowAll)
		convey.So(all.BucketUnit, convey.ShouldEqual, UsageBucketDay)
		convey.So(all.Buckets, convey.ShouldResemble, []UsageBucket{})

		week := summariseEventsForTest(t, nil, UsageWindow7d)
		convey.So(len(week.Buckets), convey.ShouldEqual, 8)
		convey.So(usageSumBucketsForTest(week.Buckets), convey.ShouldEqual, 0)
	})
}

func TestUsageListErrorSeverity(t *testing.T) {
	convey.Convey("Given a route answered by probe misses, a final miss, and a failed probe", t, func() {
		at := usageSummaryTestNow.Add(-time.Hour)
		row := func(status int, code string, probe bool) UsageEvent {
			return UsageEvent{
				Time: at, Category: UsageCategoryMCP, Method: "GET", Route: "/resolve/study/:id", Status: status,
				ErrorCode: code, Probe: probe, MCPTool: "mlwh_resolve", MCPAgent: usageSummaryTestAgent,
			}
		}

		summary := summariseEventsForTest(t, []UsageEvent{
			row(404, httpErrorCodeNotFound, true),
			row(404, httpErrorCodeNotFound, true),
			row(404, httpErrorCodeNotFound, false),
			row(502, httpErrorCodeUpstreamImpaired, true),
			row(400, httpErrorCodeBadRequest, false),
			row(200, "", false),
		}, UsageWindow24h)

		want := UsageErrorBreakdown{Fault: 1, Caller: 1, Expected: 1, Probe: 2}

		convey.Convey("then the route, its category, MCP tool, and agent split their errors by severity", func() {
			convey.So(summary.Endpoints[0].Errors, convey.ShouldEqual, 5)
			convey.So(summary.Endpoints[0].ErrorSeverity, convey.ShouldResemble, want)
			convey.So(summary.Categories[1].ErrorSeverity, convey.ShouldResemble, want)
			convey.So(summary.Categories[0].ErrorSeverity, convey.ShouldResemble, UsageErrorBreakdown{})
			convey.So(summary.MCPTools[0].ErrorSeverity, convey.ShouldResemble, want)
			convey.So(summary.MCPAgents[0].ErrorSeverity, convey.ShouldResemble, want)
		})
	})
}

func TestUsageAdminRouteFilters(t *testing.T) {
	adminAuth := map[string]string{"Authorization": "Bearer " + usageTestAdminToken}

	convey.Convey("Given CLI requests from two users", t, func() {
		router, _, _ := newUsageTestRouter(t, &feedbackStudiesQueryerForTest{})

		for _, user := range []string{"alice", "alice", "bob"} {
			usageRequestForTest(router, http.MethodGet, "/studies", map[string]string{
				"User-Agent": "wa-cli/v1.0.0", HeaderClientUser: user,
			})
		}

		convey.Convey("when GET /usage filters by user and category, then the summary is narrowed and echoes it",
			func() {
				response := usageRequestForTest(router, http.MethodGet,
					"/usage?window=24h&user=alice&category=cli&ignored=1", adminAuth)
				convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

				summary := decodeUsageSummaryForTest(response)
				convey.So(summary.Filter, convey.ShouldResemble, UsageFilter{Category: UsageCategoryCLI, User: "alice"})
				convey.So(summary.Totals.Requests, convey.ShouldEqual, 2)
				convey.So(summary.BucketUnit, convey.ShouldEqual, UsageBucketHour)
				convey.So(summary.PreviousTotals, convey.ShouldNotBeNil)
			})

		convey.Convey("when GET /usage filters by method and route, then only that route is counted", func() {
			response := usageRequestForTest(router, http.MethodGet, "/usage?method=GET&route=%2Fstudies", adminAuth)
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(decodeUsageSummaryForTest(response).Totals.Requests, convey.ShouldEqual, 3)
		})

		convey.Convey("when GET /usage has no filter, then filter is an empty object", func() {
			response := usageRequestForTest(router, http.MethodGet, "/usage", adminAuth)
			convey.So(response.Body.String(), convey.ShouldContainSubstring, `"filter":{}`)
		})

		convey.Convey("when a filter is invalid, then it answers 400 bad_request saying why", func() {
			for query, message := range map[string]string{
				"?category=bogus":     `invalid usage filter: unknown category \"bogus\"`,
				"?method=GET":         "invalid usage filter: method and route must be given together",
				"?route=/studies":     "invalid usage filter: method and route must be given together",
				"?method=GET&route=x": "invalid usage filter: route must start with /",
			} {
				response := usageRequestForTest(router, http.MethodGet, "/usage"+query, adminAuth)
				convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
				convey.So(response.Body.String(), convey.ShouldEqual,
					`{"code":"bad_request","message":"`+message+`"}`)
			}
		})
	})
}

func usageSumBucketsForTest(buckets []UsageBucket) int64 {
	var total int64

	for _, bucket := range buckets {
		total += bucket.Requests
	}

	return total
}
