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
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

const (
	usageSummaryTestLatestStudies = "GET /studies"
	usageSummaryTestClassify      = "GET /classify/:id"
	usageSummaryTestRouteStudies  = "/studies"
	usageSummaryTestAgent         = "claude-code/2.1.0"
)

var usageSummaryTestNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func TestUsageSummaryA3(t *testing.T) {
	convey.Convey("A3.1: Given F and window 24h", t, func() {
		summary := summariseFixtureFForTest(t, UsageWindow24h)

		convey.Convey("then the window metadata is set", func() {
			convey.So(summary.Window, convey.ShouldEqual, UsageWindow24h)
			convey.So(summary.WindowStart, convey.ShouldEqual, "2026-10-09T12:00:00Z")
			convey.So(summary.EarliestRecord, convey.ShouldEqual, "2026-10-01T00:00:00Z")
			convey.So(summary.RetentionDays, convey.ShouldEqual, 365)
			convey.So(summary.GeneratedAt, convey.ShouldEqual, "2026-10-10T12:00:00Z")
		})

		convey.Convey("then the totals count the window's rows", func() {
			convey.So(summary.Totals, convey.ShouldResemble, UsageTotals{
				Requests: 6, Errors: 3, ErrorRate: 0.5, Sessions: 3, IPClients: 1, Users: 2,
				ErrorSeverity: UsageErrorBreakdown{
					Fault: 1, Caller: 1, Expected: 1, Legacy: UsageLegacyErrors{Fault: 1},
				},
			})
		})

		convey.Convey("then categories list all six in order", func() {
			cats := summary.Categories
			convey.So(len(cats), convey.ShouldEqual, 6)

			convey.So(cats[0].ErrorRate, convey.ShouldAlmostEqual, 1.0/3)
			cats[0].ErrorRate = 0
			convey.So(cats[0], convey.ShouldResemble, UsageCategoryStats{
				Category: UsageCategoryCLI, Requests: 3, Errors: 1, Sessions: 2, Users: 2,
				RequestsPerSessionMedian: 1, RequestsPerSessionMax: 2,
				ErrorSeverity: usageBreakdownOfOneForTest(UsageErrorSeverityExpected, false),
			})
			convey.So(cats[1], convey.ShouldResemble, UsageCategoryStats{
				Category: UsageCategoryMCP, Requests: 1, Sessions: 1,
				RequestsPerSessionMedian: 1, RequestsPerSessionMax: 1,
			})
			convey.So(cats[2], convey.ShouldResemble, UsageCategoryStats{Category: UsageCategoryWeb})
			convey.So(cats[3], convey.ShouldResemble, UsageCategoryStats{Category: UsageCategoryResultsServer})
			convey.So(cats[4], convey.ShouldResemble, UsageCategoryStats{Category: UsageCategoryMLWHDiffServer})
			convey.So(cats[5], convey.ShouldResemble, UsageCategoryStats{
				Category: UsageCategoryOther, Requests: 2, Errors: 2, ErrorRate: 1.0, IPClients: 1,
				ErrorSeverity: UsageErrorBreakdown{Fault: 1, Caller: 1, Legacy: UsageLegacyErrors{Fault: 1}},
			})
		})

		convey.Convey("then endpoints hold matched routes with latency percentiles", func() {
			convey.So(summary.Endpoints, convey.ShouldResemble, []UsageEndpointStats{
				{
					Method: "GET", Route: usageSummaryTestRouteStudies, Requests: 4, Errors: 1, ErrorRate: 0.25,
					P50Ms: 20, P95Ms: 40, ByCategory: map[string]int64{"cli": 2, "mcp": 1, "other": 1},
					ErrorSeverity: usageBreakdownOfOneForTest(UsageErrorSeverityFault, true),
				},
				{
					Method: "GET", Route: "/classify/:id", Requests: 1, Errors: 1, ErrorRate: 1.0,
					P50Ms: 5, P95Ms: 5, ByCategory: map[string]int64{"cli": 1},
					ErrorSeverity: usageBreakdownOfOneForTest(UsageErrorSeverityExpected, false),
				},
			})
		})

		convey.Convey("then MCP tools, agents, and other User-Agents are counted", func() {
			convey.So(summary.MCPTools, convey.ShouldResemble, []UsageCount{{Value: "mlwh_list_studies", Requests: 1}})
			convey.So(summary.MCPAgents, convey.ShouldResemble, []UsageCount{{Value: usageSummaryTestAgent, Requests: 1}})
			convey.So(summary.OtherUserAgents, convey.ShouldResemble, []UsageCount{
				{
					Value: "curl/8.5.0", Requests: 2, Errors: 2,
					ErrorSeverity: UsageErrorBreakdown{Fault: 1, Caller: 1, Legacy: UsageLegacyErrors{Fault: 1}},
				},
			})
		})

		convey.Convey("then unmatched paths are listed", func() {
			convey.So(summary.Unmatched, convey.ShouldResemble, []UsageUnmatched{{
				Method: "GET", Path: "/bogus", Requests: 1, ByCategory: map[string]int64{"other": 1},
				ByTool: map[string]int64{}, ByAgent: map[string]int64{},
			}})
		})

		convey.Convey("then users and clients are ranked with ties by name", func() {
			convey.So(summary.Users, convey.ShouldResemble, []UsageActor{
				{Name: "alice", Requests: 2, ByCategory: map[string]int64{"cli": 2}},
				{Name: "bob", Requests: 1, ByCategory: map[string]int64{"cli": 1}},
			})
			convey.So(summary.Clients, convey.ShouldResemble, []UsageActor{
				{Name: "10.0.0.9", ByIP: true, Requests: 2, ByCategory: map[string]int64{"other": 2}},
				{Name: "run-a", Requests: 2, ByCategory: map[string]int64{"cli": 2}},
				{Name: "run-b", Requests: 1, ByCategory: map[string]int64{"cli": 1}},
				{Name: "sess-1", Requests: 1, ByCategory: map[string]int64{"mcp": 1}},
			})
		})

		convey.Convey("then days count only rows inside the window", func() {
			convey.So(summary.Days, convey.ShouldResemble, []UsageDay{
				{Date: "2026-10-09", ByCategory: map[string]int64{}},
				{
					Date: "2026-10-10", Requests: 6, ByCategory: map[string]int64{"cli": 3, "mcp": 1, "other": 2},
					ErrorSeverity: UsageErrorBreakdown{
						Fault: 1, Caller: 1, Expected: 1, Legacy: UsageLegacyErrors{Fault: 1},
					},
				},
			})
		})
	})

	convey.Convey("A3.2: Given F and 24h, then series has 10 periods back to the earliest row", t, func() {
		series := summariseFixtureFForTest(t, UsageWindow24h).Series
		convey.So(len(series), convey.ShouldEqual, 10)

		convey.So(series[9], convey.ShouldResemble, UsagePeriod{
			Start: "2026-10-09T12:00:00Z", End: "2026-10-10T12:00:00Z", Requests: 6,
			ByCategory: map[string]int64{"cli": 3, "mcp": 1, "other": 2},
			ByEndpoint: map[string]int64{usageSummaryTestLatestStudies: 4, usageSummaryTestClassify: 1},
			ByTool:     map[string]int64{"mlwh_list_studies": 1},
			ByUser:     map[string]int64{"alice": 2, "bob": 1},
		})
		convey.So(series[8].Requests, convey.ShouldEqual, 1)
		convey.So(series[8].ByCategory, convey.ShouldResemble, map[string]int64{"cli": 1})
		convey.So(series[7].Requests, convey.ShouldEqual, 1)
		convey.So(series[7].ByCategory, convey.ShouldResemble, map[string]int64{"web": 1})
		convey.So(series[0].Start, convey.ShouldEqual, "2026-09-30T12:00:00Z")
		convey.So(series[0].End, convey.ShouldEqual, "2026-10-01T12:00:00Z")
		convey.So(series[0].Requests, convey.ShouldEqual, 1)
		convey.So(series[0].ByCategory, convey.ShouldResemble, map[string]int64{"cli": 1})

		empty := 0

		for _, period := range series[1:7] {
			if period.Requests == 0 && len(period.ByCategory) == 0 && len(period.ByEndpoint) == 0 &&
				period.ByCategory != nil && period.ByEndpoint != nil {
				empty++
			}
		}

		convey.So(empty, convey.ShouldEqual, 6)
	})

	convey.Convey("A3.3: Given F and 7d, then the week's rows are summarised", t, func() {
		summary := summariseFixtureFForTest(t, UsageWindow7d)

		convey.So(summary.Totals.Requests, convey.ShouldEqual, 8)
		convey.So(summary.Totals.Sessions, convey.ShouldEqual, 4)
		convey.So(summary.Totals.Users, convey.ShouldEqual, 2)

		web := summary.Categories[2]
		convey.So(web.Category, convey.ShouldEqual, UsageCategoryWeb)
		convey.So(web.Requests, convey.ShouldEqual, 1)
		convey.So(web.Sessions, convey.ShouldEqual, 1)
		convey.So(web.Users, convey.ShouldEqual, 1)

		convey.So(summary.Users, convey.ShouldResemble, []UsageActor{
			{Name: "alice", Requests: 4, ByCategory: map[string]int64{"cli": 3, "web": 1}},
			{Name: "bob", Requests: 1, ByCategory: map[string]int64{"cli": 1}},
		})

		convey.So(len(summary.Days), convey.ShouldEqual, 8)
		convey.So(summary.Days[0].Date, convey.ShouldEqual, "2026-10-03")
		convey.So(summary.Days[7].Date, convey.ShouldEqual, "2026-10-10")
		convey.So(summary.Days[6], convey.ShouldResemble, UsageDay{
			Date: "2026-10-09", Requests: 1, ByCategory: map[string]int64{"cli": 1},
		})
		convey.So(sumUsageDaysForTest(summary.Days), convey.ShouldEqual, 8)

		convey.So(usagePeriodRequestsForTest(summary.Series), convey.ShouldResemble, []int64{1, 8})
	})

	convey.Convey("A3.4: Given F and all, then every row is summarised with no series", t, func() {
		summary := summariseFixtureFForTest(t, UsageWindowAll)

		convey.So(summary.WindowStart, convey.ShouldEqual, "")
		convey.So(summary.Totals.Requests, convey.ShouldEqual, 9)
		convey.So(summary.Totals.Users, convey.ShouldEqual, 3)
		convey.So(summary.Series, convey.ShouldResemble, []UsagePeriod{})

		convey.So(len(summary.Days), convey.ShouldEqual, 10)
		convey.So(summary.Days[0].Date, convey.ShouldEqual, "2026-10-01")
		convey.So(summary.Days[0].Requests, convey.ShouldEqual, 1)
		convey.So(summary.Days[8].Date, convey.ShouldEqual, "2026-10-09")
		convey.So(summary.Days[8].Requests, convey.ShouldEqual, 1)
		convey.So(summary.Days[9].Date, convey.ShouldEqual, "2026-10-10")
		convey.So(summary.Days[9].Requests, convey.ShouldEqual, 6)
	})

	convey.Convey("A3.5: Given F and 30d, then every row falls in one period", t, func() {
		summary := summariseFixtureFForTest(t, UsageWindow30d)

		convey.So(summary.Totals.Requests, convey.ShouldEqual, 9)
		convey.So(usagePeriodRequestsForTest(summary.Series), convey.ShouldResemble, []int64{9})
	})

	convey.Convey("A3.6: Given F plus a row at now+1h, when 24h is summarised, then period 0 includes it", t, func() {
		store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 365, usageQueueSize, usageSummaryTestNow)
		recordUsageEventsForTest(store, append(usageFixtureFForTest(), UsageEvent{
			Time: usageSummaryTestNow.Add(time.Hour), Category: UsageCategoryCLI, Method: "GET",
			Route: usageSummaryTestRouteStudies, RemoteAddr: "10.0.0.1", Status: 200,
		}))

		summary, err := store.Summary(context.Background(), UsageWindow24h, UsageFilter{})
		convey.So(err, convey.ShouldBeNil)
		convey.So(summary.Totals.Requests, convey.ShouldEqual, 7)
		convey.So(summary.Series[len(summary.Series)-1].Requests, convey.ShouldEqual, 7)
		convey.So(summary.Days[len(summary.Days)-1], convey.ShouldResemble, UsageDay{
			Date: "2026-10-10", Requests: 7, ByCategory: map[string]int64{"cli": 4, "mcp": 1, "other": 2},
			ErrorSeverity: UsageErrorBreakdown{
				Fault: 1, Caller: 1, Expected: 1, Legacy: UsageLegacyErrors{Fault: 1},
			},
		})
	})

	convey.Convey("A3.7: Given an empty store", t, func() {
		store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 365, usageQueueSize, usageSummaryTestNow)

		convey.Convey("when 7d is summarised, then everything is zero or empty except days", func() {
			summary, err := store.Summary(context.Background(), UsageWindow7d, UsageFilter{})
			convey.So(err, convey.ShouldBeNil)

			convey.So(summary.Totals, convey.ShouldResemble, UsageTotals{})
			convey.So(summary.EarliestRecord, convey.ShouldEqual, "")

			wantCategories := make([]UsageCategoryStats, 0, 6)
			for _, category := range UsageCategories() {
				wantCategories = append(wantCategories, UsageCategoryStats{Category: category})
			}

			convey.So(summary.Categories, convey.ShouldResemble, wantCategories)
			convey.So(summary.Endpoints, convey.ShouldResemble, []UsageEndpointStats{})
			convey.So(summary.MCPTools, convey.ShouldResemble, []UsageCount{})
			convey.So(summary.MCPAgents, convey.ShouldResemble, []UsageCount{})
			convey.So(summary.OtherUserAgents, convey.ShouldResemble, []UsageCount{})
			convey.So(summary.Unmatched, convey.ShouldResemble, []UsageUnmatched{})
			convey.So(summary.Users, convey.ShouldResemble, []UsageActor{})
			convey.So(summary.Clients, convey.ShouldResemble, []UsageActor{})
			convey.So(summary.Series, convey.ShouldResemble, []UsagePeriod{})

			convey.So(len(summary.Days), convey.ShouldEqual, 8)
			convey.So(sumUsageDaysForTest(summary.Days), convey.ShouldEqual, 0)
		})

		convey.Convey("when all is summarised, then days is empty", func() {
			summary, err := store.Summary(context.Background(), UsageWindowAll, UsageFilter{})
			convey.So(err, convey.ShouldBeNil)
			convey.So(summary.Days, convey.ShouldResemble, []UsageDay{})
		})
	})

	convey.Convey("A3.8: Given 30 distinct MCP tools with 1-30 requests, then mcp_tools holds the top 25", t, func() {
		var events []UsageEvent

		for tool := 1; tool <= 30; tool++ {
			for range tool {
				events = append(events, UsageEvent{
					Time: usageSummaryTestNow.Add(-time.Hour), Category: UsageCategoryMCP, Method: "GET",
					Route: usageSummaryTestRouteStudies, MCPTool: fmt.Sprintf("tool-%02d", tool), Status: 200,
				})
			}
		}

		summary := summariseEventsForTest(t, events, UsageWindow24h)
		convey.So(len(summary.MCPTools), convey.ShouldEqual, 25)
		convey.So(summary.MCPTools[0], convey.ShouldResemble, UsageCount{Value: "tool-30", Requests: 30})
		convey.So(summary.MCPTools[24], convey.ShouldResemble, UsageCount{Value: "tool-06", Requests: 6})

		convey.Convey("given 30 tools tied at 1 request, then the cap keeps the first 25 by name", func() {
			tied := make([]UsageEvent, 0, 30)

			for tool := 30; tool >= 1; tool-- {
				tied = append(tied, UsageEvent{
					Time: usageSummaryTestNow.Add(-time.Hour), Category: UsageCategoryMCP, Method: "GET",
					Route: usageSummaryTestRouteStudies, MCPTool: fmt.Sprintf("tool-%02d", tool), Status: 200,
				})
			}

			tools := summariseEventsForTest(t, tied, UsageWindow24h).MCPTools
			convey.So(len(tools), convey.ShouldEqual, 25)
			convey.So(tools[0].Value, convey.ShouldEqual, "tool-01")
			convey.So(tools[24].Value, convey.ShouldEqual, "tool-25")
		})
	})

	convey.Convey("A3.8: Given 30 distinct values for each capped list, then each holds 25 entries", t, func() {
		capped := []struct {
			name string
			set  func(ev *UsageEvent, value string)
			size func(s UsageSummary) int
		}{
			{
				"mcp_agents",
				func(ev *UsageEvent, v string) { ev.MCPAgent = v },
				func(s UsageSummary) int { return len(s.MCPAgents) },
			},
			{
				"other_user_agents",
				func(ev *UsageEvent, v string) { ev.Category, ev.UserAgent = UsageCategoryOther, v },
				func(s UsageSummary) int { return len(s.OtherUserAgents) },
			},
			{
				"unmatched",
				func(ev *UsageEvent, v string) { ev.Route, ev.Path = "", "/"+v },
				func(s UsageSummary) int { return len(s.Unmatched) },
			},
			{
				"users",
				func(ev *UsageEvent, v string) { ev.Username = v },
				func(s UsageSummary) int { return len(s.Users) },
			},
			{
				"clients",
				func(ev *UsageEvent, v string) { ev.ClientID = v },
				func(s UsageSummary) int { return len(s.Clients) },
			},
		}

		for _, list := range capped {
			convey.Convey("then "+list.name+" is capped at 25", func() {
				events := make([]UsageEvent, 0, 30)

				for i := range 30 {
					ev := UsageEvent{
						Time: usageSummaryTestNow.Add(-time.Hour), Category: UsageCategoryMCP, Method: "GET",
						Route: usageSummaryTestRouteStudies, RemoteAddr: "10.0.0.1", Status: 200,
					}
					list.set(&ev, fmt.Sprintf("v-%02d", i))
					events = append(events, ev)
				}

				convey.So(list.size(summariseEventsForTest(t, events, UsageWindow24h)), convey.ShouldEqual, 25)
			})
		}

		convey.Convey("then endpoints lists all 30 matched routes", func() {
			events := make([]UsageEvent, 0, 30)

			for i := range 30 {
				events = append(events, UsageEvent{
					Time: usageSummaryTestNow.Add(-time.Hour), Category: UsageCategoryCLI, Method: "GET",
					Route: fmt.Sprintf("/r-%02d", i), Status: 200,
				})
			}

			convey.So(len(summariseEventsForTest(t, events, UsageWindow24h).Endpoints), convey.ShouldEqual, 30)
		})
	})

	convey.Convey("A3.9: Given one endpoint with latencies 1-100 ms, then p50 50 and p95 95", t, func() {
		events := make([]UsageEvent, 0, 100)

		for ms := 100; ms >= 1; ms-- {
			events = append(events, latencyUsageEventForTest(ms))
		}

		endpoints := summariseEventsForTest(t, events, UsageWindow24h).Endpoints
		convey.So(len(endpoints), convey.ShouldEqual, 1)
		convey.So(endpoints[0].P50Ms, convey.ShouldEqual, 50)
		convey.So(endpoints[0].P95Ms, convey.ShouldEqual, 95)

		convey.Convey("given one row of 7 ms, then p50 7 and p95 7", func() {
			single := summariseEventsForTest(t, []UsageEvent{latencyUsageEventForTest(7)}, UsageWindow24h).Endpoints
			convey.So(single[0].P50Ms, convey.ShouldEqual, 7)
			convey.So(single[0].P95Ms, convey.ShouldEqual, 7)
		})
	})

	convey.Convey("A3.10: Given cli sessions with counts [1, 2, 3, 10], then median 2 and max 10", t, func() {
		var events []UsageEvent

		for i, count := range []int{10, 1, 3, 2} {
			for range count {
				events = append(events, UsageEvent{
					Time: usageSummaryTestNow.Add(-time.Hour), Category: UsageCategoryCLI, Method: "GET",
					Route: usageSummaryTestRouteStudies, ClientID: fmt.Sprintf("run-%d", i), Status: 200,
				})
			}
		}

		cli := summariseEventsForTest(t, events, UsageWindow24h).Categories[0]
		convey.So(cli.Sessions, convey.ShouldEqual, 4)
		convey.So(cli.RequestsPerSessionMedian, convey.ShouldEqual, 2)
		convey.So(cli.RequestsPerSessionMax, convey.ShouldEqual, 10)
	})

	convey.Convey("A3.11: Given 2 recorded events with no Flush, when Summary runs, then both are counted", t, func() {
		store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 365, usageQueueSize, usageSummaryTestNow)
		fixture := usageFixtureFForTest()
		store.Record(fixture[0])
		store.Record(fixture[1])

		summary, err := store.Summary(context.Background(), UsageWindow24h, UsageFilter{})
		convey.So(err, convey.ShouldBeNil)
		convey.So(summary.Totals.Requests, convey.ShouldEqual, 2)
	})

	convey.Convey("A3.12: Given window 1h, then Summary returns ErrUsageWindowInvalid", t, func() {
		store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 365, usageQueueSize, usageSummaryTestNow)

		_, err := store.Summary(context.Background(), UsageWindow("1h"), UsageFilter{})
		convey.So(errors.Is(err, ErrUsageWindowInvalid), convey.ShouldBeTrue)
	})

	convey.Convey("A3.13: Given F plus an unmatched MCP row, when 24h is summarised, "+
		"then the unmatched entry breaks down by category, tool, and agent", t, func() {
		events := append(usageFixtureFForTest(), UsageEvent{
			Time: usageSummaryTestNow.Add(-30 * time.Minute), Category: UsageCategoryMCP, Method: "GET",
			Path: "/bogus", ClientID: "sess-1", RemoteAddr: "10.0.0.2", MCPTool: "mlwh_bogus",
			MCPAgent: usageSummaryTestAgent, Status: 404,
		})

		summary := summariseEventsForTest(t, events, UsageWindow24h)
		convey.So(summary.Unmatched, convey.ShouldResemble, []UsageUnmatched{{
			Method: "GET", Path: "/bogus", Requests: 2,
			ByCategory: map[string]int64{"mcp": 1, "other": 1},
			ByTool:     map[string]int64{"mlwh_bogus": 1},
			ByAgent:    map[string]int64{usageSummaryTestAgent: 1},
		}})
	})

	convey.Convey("A3.14: Given F with 3 drops recorded, then dropped_events is 3", t, func() {
		store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 365, usageQueueSize, usageSummaryTestNow)
		recordUsageEventsForTest(store, usageFixtureFForTest())
		store.dropped.Add(3)

		summary, err := store.Summary(context.Background(), UsageWindow24h, UsageFilter{})
		convey.So(err, convey.ShouldBeNil)
		convey.So(summary.DroppedEvents, convey.ShouldEqual, 3)
	})

	convey.Convey("A3.15: Given F and 24h, when JSON-marshalled, then maps are objects and lists are arrays", t, func() {
		data, err := json.Marshal(summariseFixtureFForTest(t, UsageWindow24h))
		convey.So(err, convey.ShouldBeNil)

		var doc map[string]any

		convey.So(json.Unmarshal(data, &doc), convey.ShouldBeNil)

		lists := []string{
			"categories", "endpoints", "mcp_tools", "mcp_agents", "other_user_agents",
			"unmatched", "users", "clients", "days", "series", "errors",
		}
		convey.So(countNonArraysForTest(doc, lists), convey.ShouldEqual, 0)
		convey.So(countNonObjectByMapsForTest(doc, lists), convey.ShouldEqual, 0)
	})

	convey.Convey("A3.15: Given an empty store and 7d, when JSON-marshalled, then every list is an array", t, func() {
		store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 365, usageQueueSize, usageSummaryTestNow)

		summary, err := store.Summary(context.Background(), UsageWindow7d, UsageFilter{})
		convey.So(err, convey.ShouldBeNil)

		data, err := json.Marshal(summary)
		convey.So(err, convey.ShouldBeNil)

		var doc map[string]any

		convey.So(json.Unmarshal(data, &doc), convey.ShouldBeNil)
		convey.So(countNonArraysForTest(doc, []string{
			"categories", "endpoints", "mcp_tools", "mcp_agents", "other_user_agents",
			"unmatched", "users", "clients", "days", "series", "errors",
		}), convey.ShouldEqual, 0)
		convey.So(countNonObjectByMapsForTest(doc, []string{"days"}), convey.ShouldEqual, 0)
	})
}

func summariseFixtureFForTest(t *testing.T, w UsageWindow) UsageSummary {
	t.Helper()

	return summariseEventsForTest(t, usageFixtureFForTest(), w)
}

func sumUsageDaysForTest(days []UsageDay) int64 {
	var total int64

	for _, day := range days {
		total += day.Requests
	}

	return total
}

func TestUsageSummarySemantics(t *testing.T) {
	convey.Convey("Given a status 400 row and a status 200 row, then the 400 counts as an error", t, func() {
		events := make([]UsageEvent, 0, 2)

		for _, status := range []int{400, 200} {
			events = append(events, UsageEvent{
				Time: usageSummaryTestNow.Add(-time.Hour), Category: UsageCategoryCLI, Method: "GET",
				Route: usageSummaryTestRouteStudies, Status: status,
			})
		}

		summary := summariseEventsForTest(t, events, UsageWindow24h)
		convey.So(summary.Totals.Errors, convey.ShouldEqual, 1)
		convey.So(summary.Categories[0].Errors, convey.ShouldEqual, 1)
		convey.So(summary.Endpoints[0].Errors, convey.ShouldEqual, 1)
		convey.So(summary.Endpoints[0].ErrorRate, convey.ShouldEqual, 0.5)
	})

	convey.Convey("Given rows at exactly now-24h and now-24h+1ms, when 24h is summarised", t, func() {
		edge := usageSummaryTestNow.Add(-24 * time.Hour)
		event := func(at time.Time, name string, status, latencyMs int) UsageEvent {
			return UsageEvent{
				Time: at, Category: UsageCategoryCLI, Method: "GET", Route: usageSummaryTestRouteStudies,
				ClientID: "run-a", Username: name, MCPTool: name, Status: status,
				Latency: time.Duration(latencyMs) * time.Millisecond,
			}
		}

		summary := summariseEventsForTest(t, []UsageEvent{
			event(edge, "outside", 500, 90),
			event(edge.Add(time.Millisecond), "inside", 200, 10),
		}, UsageWindow24h)

		convey.Convey("then only the later row is inside the window", func() {
			convey.So(summary.Totals, convey.ShouldResemble, UsageTotals{Requests: 1, Sessions: 1, Users: 1})
			convey.So(summary.Categories[0].Requests, convey.ShouldEqual, 1)
			convey.So(summary.Categories[0].RequestsPerSessionMax, convey.ShouldEqual, 1)
			convey.So(summary.Endpoints, convey.ShouldResemble, []UsageEndpointStats{{
				Method: "GET", Route: usageSummaryTestRouteStudies, Requests: 1,
				P50Ms: 10, P95Ms: 10, ByCategory: map[string]int64{"cli": 1},
			}})
			convey.So(summary.MCPTools, convey.ShouldResemble, []UsageCount{{Value: "inside", Requests: 1}})
			convey.So(summary.Users, convey.ShouldResemble, []UsageActor{
				{Name: "inside", Requests: 1, ByCategory: map[string]int64{"cli": 1}},
			})
			convey.So(summary.Days, convey.ShouldResemble, []UsageDay{
				{Date: "2026-10-09", Requests: 1, ByCategory: map[string]int64{"cli": 1}},
				{Date: "2026-10-10", ByCategory: map[string]int64{}},
			})
		})

		convey.Convey("then the row at now-24h is in the previous period", func() {
			convey.So(usagePeriodRequestsForTest(summary.Series), convey.ShouldResemble, []int64{1, 1})
			convey.So(summary.Series[0].End, convey.ShouldEqual, "2026-10-09T12:00:00Z")
		})
	})

	convey.Convey("Given only a row at now+49h, when 24h is summarised, then series is one period holding it", t, func() {
		summary := summariseEventsForTest(t, []UsageEvent{{
			Time: usageSummaryTestNow.Add(49 * time.Hour), Category: UsageCategoryCLI, Method: "GET",
			Route: usageSummaryTestRouteStudies, Status: 200,
		}}, UsageWindow24h)

		convey.So(summary.Totals.Requests, convey.ShouldEqual, 1)
		convey.So(usagePeriodRequestsForTest(summary.Series), convey.ShouldResemble, []int64{1})
	})

	convey.Convey("Given two unmatched paths each with its own tool and agent, "+
		"then each entry breaks down by its own tool and agent", t, func() {
		event := func(path, tool, agent string, hoursAgo int) UsageEvent {
			return UsageEvent{
				Time:     usageSummaryTestNow.Add(-time.Duration(hoursAgo) * time.Hour),
				Category: UsageCategoryMCP, Method: "GET", Path: path, MCPTool: tool, MCPAgent: agent, Status: 404,
			}
		}

		summary := summariseEventsForTest(t, []UsageEvent{
			event("/a", "tool_a", "agent-a", 1), event("/a", "tool_a", "agent-a", 2),
			event("/b", "tool_b", "agent-b", 3),
		}, UsageWindow24h)

		convey.So(summary.Unmatched, convey.ShouldResemble, []UsageUnmatched{
			{
				Method: "GET", Path: "/a", Requests: 2, ByCategory: map[string]int64{"mcp": 2},
				ByTool: map[string]int64{"tool_a": 2}, ByAgent: map[string]int64{"agent-a": 2},
			},
			{
				Method: "GET", Path: "/b", Requests: 1, ByCategory: map[string]int64{"mcp": 1},
				ByTool: map[string]int64{"tool_b": 1}, ByAgent: map[string]int64{"agent-b": 1},
			},
		})
	})

	convey.Convey("Given other rows with and without a User-Agent and a cli row with none, "+
		"then other_user_agents counts the empty User-Agent of other rows only", t, func() {
		event := func(category UsageCategory, userAgent string) UsageEvent {
			return UsageEvent{
				Time: usageSummaryTestNow.Add(-time.Hour), Category: category, Method: "GET",
				Route: usageSummaryTestRouteStudies, UserAgent: userAgent, Status: 200,
			}
		}

		summary := summariseEventsForTest(t, []UsageEvent{
			event(UsageCategoryOther, "curl/8.5.0"), event(UsageCategoryOther, "curl/8.5.0"),
			event(UsageCategoryOther, ""), event(UsageCategoryCLI, ""),
		}, UsageWindow24h)

		convey.So(summary.OtherUserAgents, convey.ShouldResemble, []UsageCount{
			{Value: "curl/8.5.0", Requests: 2}, {Value: "", Requests: 1},
		})
	})

	convey.Convey("Given GET and POST rows on one route, then each method has its own latency percentiles", t, func() {
		get, post := latencyUsageEventForTest(10), latencyUsageEventForTest(30)
		post.Method = "POST"

		endpoints := summariseEventsForTest(t, []UsageEvent{get, post}, UsageWindow24h).Endpoints
		convey.So(endpoints, convey.ShouldHaveLength, 2)
		convey.So(endpoints[0].Method, convey.ShouldEqual, "GET")
		convey.So(endpoints[0].P95Ms, convey.ShouldEqual, 10)
		convey.So(endpoints[1].Method, convey.ShouldEqual, "POST")
		convey.So(endpoints[1].P50Ms, convey.ShouldEqual, 30)
	})

	convey.Convey("Given one row of 1500us latency, then p50 and p95 are 1.5 ms", t, func() {
		event := latencyUsageEventForTest(0)
		event.Latency = 1500 * time.Microsecond

		endpoints := summariseEventsForTest(t, []UsageEvent{event}, UsageWindow24h).Endpoints
		convey.So(endpoints[0].P50Ms, convey.ShouldEqual, 1.5)
		convey.So(endpoints[0].P95Ms, convey.ShouldEqual, 1.5)
	})
}

func TestUsageSummaryErrors(t *testing.T) {
	convey.Convey("Given error rows of several endpoints, codes, and kinds", t, func() {
		summary := summariseEventsForTest(t, usageErrorFixtureForTest(), UsageWindow24h)

		convey.Convey("then errors are grouped by method, route, status, code, and severity, detailed "+
			"before legacy, then faults, caller errors, and expected answers, then largest first", func() {
			keys := make([]string, 0, len(summary.Errors))
			for _, group := range summary.Errors {
				code := "<nil>"
				if group.ErrorCode != nil {
					code = *group.ErrorCode
				}

				keys = append(keys, fmt.Sprintf("%s %s %d %s %s %d", group.Method, group.Route, group.Status, code,
					group.Severity, group.Count))
			}

			convey.So(keys, convey.ShouldResemble, []string{
				"GET /classify/:id 502 upstream_impaired fault 1",
				"GET  404 no_route caller 1",
				"GET /classify/:id 404 not_found expected 3",
				"GET /classify/:id 404 <nil> expected 1",
			})
		})

		convey.Convey("then the totals split errors by severity, with legacy rows apart", func() {
			convey.So(summary.Totals.ErrorSeverity, convey.ShouldResemble, UsageErrorBreakdown{
				Fault: 1, Caller: 1, Expected: 4, Legacy: UsageLegacyErrors{Expected: 1},
			})
		})

		convey.Convey("then a group holds its time span, messages, examples, and breakdowns", func() {
			group := summary.Errors[2]
			convey.So(group.FirstSeen, convey.ShouldEqual, "2026-10-10T09:00:00Z")
			convey.So(group.LastSeen, convey.ShouldEqual, "2026-10-10T11:00:00Z")
			convey.So(group.ErrorKinds, convey.ShouldResemble, map[string]int64{})
			convey.So(group.Messages, convey.ShouldResemble, []UsageErrorMessage{
				{Message: "no A", Count: 2}, {Message: "no B", Count: 1},
			})
			convey.So(group.ByCategory, convey.ShouldResemble, map[string]int64{"cli": 2, "mcp": 1})
			convey.So(group.ByTool, convey.ShouldResemble, map[string]int64{"mlwh_classify_identifier": 1})
			convey.So(group.ByAgent, convey.ShouldResemble, map[string]int64{usageSummaryTestAgent: 1})
			convey.So(group.Examples, convey.ShouldResemble, []UsageErrorExample{
				{
					Time: "2026-10-10T11:00:00Z", Path: "/classify/A", Query: "x=1", Category: UsageCategoryMCP,
					MCPTool: "mlwh_classify_identifier", MCPAgent: usageSummaryTestAgent, ClientID: "sess-1",
					Message: "no A",
				},
				{
					Time: "2026-10-10T10:00:00Z", Path: "/classify/A", Category: UsageCategoryCLI,
					ClientID: "run-a", Username: "alice", Message: "no A",
				},
				{
					Time: "2026-10-10T09:00:00Z", Path: "/classify/B", Category: UsageCategoryCLI,
					ClientID: "run-a", Username: "alice", Message: "no B",
				},
			})
		})

		convey.Convey("then legacy rows have a null code and examples without details", func() {
			group := summary.Errors[3]
			convey.So(group.ErrorCode, convey.ShouldBeNil)
			convey.So(group.Messages, convey.ShouldResemble, []UsageErrorMessage{})
			convey.So(group.Examples, convey.ShouldResemble, []UsageErrorExample{
				{Time: "2026-10-10T08:00:00Z", Category: UsageCategoryCLI},
			})
		})

		convey.Convey("then error kinds are counted and unmatched rows keep their path", func() {
			convey.So(summary.Errors[0].ErrorKinds, convey.ShouldResemble, map[string]int64{UsageErrorKindTimeout: 1})
			convey.So(summary.Errors[1].Examples[0].Path, convey.ShouldEqual, "/bogus")
		})

		convey.Convey("then MCP tools and agents carry their error counts", func() {
			convey.So(summary.MCPTools, convey.ShouldResemble, []UsageCount{
				{
					Value: "mlwh_classify_identifier", Requests: 2, Errors: 1,
					ErrorSeverity: usageBreakdownOfOneForTest(UsageErrorSeverityExpected, false),
				},
			})
			convey.So(summary.MCPAgents, convey.ShouldResemble, []UsageCount{
				{
					Value: usageSummaryTestAgent, Requests: 2, Errors: 1,
					ErrorSeverity: usageBreakdownOfOneForTest(UsageErrorSeverityExpected, false),
				},
			})
		})

		convey.Convey("then rows outside the window are not counted", func() {
			var total int64
			for _, group := range summary.Errors {
				total += group.Count
			}

			convey.So(total, convey.ShouldEqual, summary.Totals.Errors)

			breakdown := summary.Totals.ErrorSeverity
			convey.So(breakdown.Fault+breakdown.Caller+breakdown.Expected+breakdown.Probe, convey.ShouldEqual,
				summary.Totals.Errors)
		})
	})

	convey.Convey("Given 12 errors with 7 messages, then 5 messages and the 10 latest examples are kept", t, func() {
		events := make([]UsageEvent, 0, 12)

		for i := range 12 {
			events = append(events, UsageEvent{
				Time: usageSummaryTestNow.Add(-time.Duration(i+1) * time.Minute), Category: UsageCategoryCLI,
				Method: "GET", Route: "/classify/:id", Path: fmt.Sprintf("/classify/%d", i), Status: 404,
				ErrorCode: httpErrorCodeNotFound, ErrorMessage: fmt.Sprintf("m%d", min(i, 6)),
			})
		}

		group := summariseEventsForTest(t, events, UsageWindow24h).Errors[0]
		convey.So(group.Count, convey.ShouldEqual, 12)
		convey.So(group.Messages, convey.ShouldResemble, []UsageErrorMessage{
			{Message: "m6", Count: 6}, {Message: "m0", Count: 1}, {Message: "m1", Count: 1},
			{Message: "m2", Count: 1}, {Message: "m3", Count: 1},
		})
		convey.So(group.Examples, convey.ShouldHaveLength, 10)
		convey.So(group.Examples[0].Path, convey.ShouldEqual, "/classify/0")
		convey.So(group.Examples[9].Path, convey.ShouldEqual, "/classify/9")
	})

	convey.Convey("Given 60 distinct error groups, then the 50 largest are listed", t, func() {
		events := make([]UsageEvent, 0, 61)

		for i := range 60 {
			events = append(events, UsageEvent{
				Time: usageSummaryTestNow.Add(-time.Minute), Category: UsageCategoryCLI, Method: "GET",
				Route: "/studies", Status: 400 + i, ErrorCode: httpErrorCodeBadRequest,
			})
		}

		events = append(events, events[59])

		errorGroups := summariseEventsForTest(t, events, UsageWindow24h).Errors
		convey.So(errorGroups, convey.ShouldHaveLength, 50)
		convey.So(errorGroups[0].Status, convey.ShouldEqual, 459)
		convey.So(errorGroups[1].Status, convey.ShouldEqual, 400)
	})

	convey.Convey("Given no errors, then errors is an empty JSON array", t, func() {
		data, err := json.Marshal(summariseEventsForTest(t, []UsageEvent{latencyUsageEventForTest(1)},
			UsageWindow24h))
		convey.So(err, convey.ShouldBeNil)

		var doc map[string]any

		convey.So(json.Unmarshal(data, &doc), convey.ShouldBeNil)
		convey.So(countNonArraysForTest(doc, []string{"errors"}), convey.ShouldEqual, 0)
	})
}

func TestUsageErrorSeverity(t *testing.T) {
	convey.Convey("Given one error row, then its group's severity follows status, code, route, and caller", t,
		func() {
			for _, tc := range []struct {
				name     string
				category UsageCategory
				route    string
				status   int
				code     string // "" for a legacy row
				want     UsageErrorSeverity
			}{
				{"a CLI 502", UsageCategoryCLI, "/classify/:id", 502, httpErrorCodeUpstreamImpaired, "fault"},
				{"another client's 503", UsageCategoryOther, "/studies", 503, httpErrorCodeCacheNeverSynced, "fault"},
				{"an MCP 500", UsageCategoryMCP, "/studies", 500, httpErrorCodeInternal, "fault"},
				{"a legacy CLI 502", UsageCategoryCLI, "/runs/count", 502, "", "fault"},
				{"a CLI not_found", UsageCategoryCLI, "/resolve/study/:id", 404, httpErrorCodeNotFound, "expected"},
				{"an MCP not_found", UsageCategoryMCP, "/study/:id/samples", 404, httpErrorCodeNotFound, "expected"},
				{"an MCP ambiguous", UsageCategoryMCP, "/find/sample/supplier-name/:id", 409,
					httpErrorCodeAmbiguous, "expected"},
				{"another client's ambiguous", UsageCategoryOther, "/classify/:id", 409, httpErrorCodeAmbiguous,
					"expected"},
				{"a legacy CLI 404", UsageCategoryCLI, "/resolve/sample-name/:id", 404, "", "expected"},
				{"a legacy CLI 409", UsageCategoryCLI, "/export/:children/:parent_kind/:parent_id", 409, "",
					"expected"},
				{"a CLI bad_request", UsageCategoryCLI, "/study/:id/latest-data", 400, httpErrorCodeBadRequest,
					"fault"},
				{"a legacy CLI 400", UsageCategoryCLI, "/study/:id/latest-data", 400, "", "fault"},
				{"a web unsupported_identifier", UsageCategoryWeb, "/export/:children/:parent_kind/:parent_id", 422,
					httpErrorCodeUnsupportedIdentifier, "expected"},
				{"a results server unsupported_identifier", UsageCategoryResultsServer, "/classify/:id", 422,
					httpErrorCodeUnsupportedIdentifier, "expected"},
				{"a CLI unsupported_identifier", UsageCategoryCLI, "/classify/:id", 422,
					httpErrorCodeUnsupportedIdentifier, "expected"},
				{"a legacy results server 422", UsageCategoryResultsServer, "/classify/:id", 422, "", "expected"},
				{"a legacy unmatched 422", UsageCategoryResultsServer, "", 422, "", "fault"},
				{"a results server bad_request on classify", UsageCategoryResultsServer, "/classify/:id", 400,
					httpErrorCodeBadRequest, "fault"},
				{"a web bad_request", UsageCategoryWeb, "/studies", 400, httpErrorCodeBadRequest, "fault"},
				{"a results server bad_request", UsageCategoryResultsServer, "/studies", 400,
					httpErrorCodeBadRequest, "fault"},
				{"an mlwhdiff server no_route", UsageCategoryMLWHDiffServer, "", 404, UsageErrorCodeNoRoute,
					"fault"},
				{"a CLI uncoded 404", UsageCategoryCLI, "/studies", 404, usageErrorCodeUncoded, "fault"},
				{"an MCP bad_request", UsageCategoryMCP, "/runs", 400, httpErrorCodeBadRequest, "caller"},
				{"an MCP unsupported_identifier", UsageCategoryMCP, "/sample/:id/irods", 422,
					httpErrorCodeUnsupportedIdentifier, "expected"},
				{"another client's bad_request", UsageCategoryOther, "/runs", 400, httpErrorCodeBadRequest, "caller"},
				{"another client's no_route", UsageCategoryOther, "", 404, UsageErrorCodeNoRoute, "caller"},
				{"a legacy unmatched 404", UsageCategoryOther, "", 404, "", "caller"},
				{"an MCP unauthorized", UsageCategoryMCP, "/feedback", 401, httpErrorCodeUnauthorized, "caller"},
			} {
				convey.Convey("given "+tc.name+", then it is "+string(tc.want), func() {
					summary := summariseEventsForTest(t, []UsageEvent{{
						Time: usageSummaryTestNow.Add(-time.Minute), Category: tc.category, Method: "GET",
						Route: tc.route, Path: "/p", Status: tc.status, ErrorCode: tc.code,
					}}, UsageWindow24h)

					convey.So(summary.Errors, convey.ShouldHaveLength, 1)
					convey.So(summary.Errors[0].Severity, convey.ShouldEqual, tc.want)

					want := usageBreakdownOfOneForTest(tc.want, tc.code == "")

					convey.So(summary.Totals.ErrorSeverity, convey.ShouldResemble, want)
				})
			}
		})

	convey.Convey("Given one route's 400s from the CLI and from another client, then they form a fault group "+
		"and a caller group", t, func() {
		events := make([]UsageEvent, 0, 3)
		for _, category := range []UsageCategory{UsageCategoryCLI, UsageCategoryOther, UsageCategoryOther} {
			events = append(events, UsageEvent{
				Time: usageSummaryTestNow.Add(-time.Minute), Category: category, Method: "GET", Route: "/runs",
				Status: 400, ErrorCode: httpErrorCodeBadRequest,
			})
		}

		groups := summariseEventsForTest(t, events, UsageWindow24h).Errors
		convey.So(groups, convey.ShouldHaveLength, 2)
		convey.So(groups[0].Severity, convey.ShouldEqual, UsageErrorSeverityFault)
		convey.So(groups[0].ByCategory, convey.ShouldResemble, map[string]int64{"cli": 1})
		convey.So(groups[1].Severity, convey.ShouldEqual, UsageErrorSeverityCaller)
		convey.So(groups[1].Count, convey.ShouldEqual, 2)
	})

	convey.Convey("Given 60 large expected and legacy groups and one small fault and caller group, "+
		"then the group cap keeps the fault and caller groups", t, func() {
		events := make([]UsageEvent, 0, 124)

		for i := range 60 {
			for _, code := range []string{httpErrorCodeNotFound, ""} {
				events = append(events, UsageEvent{
					Time: usageSummaryTestNow.Add(-time.Minute), Category: UsageCategoryCLI, Method: "GET",
					Route: fmt.Sprintf("/r%02d/:id", i), Status: 404, ErrorCode: code,
				})
			}
		}

		events = append(events,
			UsageEvent{
				Time: usageSummaryTestNow.Add(-time.Minute), Category: UsageCategoryOther, Method: "GET",
				Route: "/runs", Status: 400, ErrorCode: httpErrorCodeBadRequest,
			},
			UsageEvent{
				Time: usageSummaryTestNow.Add(-time.Minute), Category: UsageCategoryCLI, Method: "GET",
				Route: "/runs", Status: 502, ErrorCode: httpErrorCodeUpstreamImpaired,
			},
		)

		summary := summariseEventsForTest(t, events, UsageWindow24h)
		groups := summary.Errors
		convey.So(groups, convey.ShouldHaveLength, 50)
		convey.So(groups[0].Severity, convey.ShouldEqual, UsageErrorSeverityFault)
		convey.So(groups[1].Severity, convey.ShouldEqual, UsageErrorSeverityCaller)
		convey.So(groups[2].ErrorCode, convey.ShouldNotBeNil)
		convey.So(groups[49].ErrorCode, convey.ShouldNotBeNil)
		convey.So(summary.Totals.ErrorSeverity, convey.ShouldResemble, UsageErrorBreakdown{
			Fault: 1, Caller: 1, Expected: 120, Legacy: UsageLegacyErrors{Expected: 60},
		})
	})
}

func TestUsageLegacyErrorTotals(t *testing.T) {
	convey.Convey("Given legacy 502s, a legacy CLI 400, legacy lookup misses, and a detailed fault", t, func() {
		legacy := func(route string, status int, category UsageCategory) UsageEvent {
			return UsageEvent{
				Time: usageSummaryTestNow.Add(-time.Hour), Category: category, Method: "GET", Route: route,
				Status: status,
			}
		}

		events := []UsageEvent{
			legacy("/export/:children/:parent_kind/:parent_id", 502, UsageCategoryCLI),
			legacy("/export/:children/:parent_kind/:parent_id", 502, UsageCategoryCLI),
			legacy("/classify/:id", 502, UsageCategoryOther),
			legacy("/study/:id/latest-data", 400, UsageCategoryCLI),
			legacy("/resolve/study/:id", 404, UsageCategoryCLI),
			legacy("/classify/:id", 409, UsageCategoryCLI),
			legacy("", 404, UsageCategoryOther),
			{
				Time: usageSummaryTestNow.Add(-time.Hour), Category: UsageCategoryCLI, Method: "GET",
				Route: "/runs", Status: 503, ErrorCode: httpErrorCodeCacheNeverSynced,
			},
		}

		summary := summariseEventsForTest(t, events, UsageWindow24h)

		convey.Convey("then the totals judge legacy rows by status, as their groups do, and count them apart", func() {
			convey.So(summary.Totals.ErrorSeverity, convey.ShouldResemble, UsageErrorBreakdown{
				Fault: 5, Caller: 1, Expected: 2,
				Legacy: UsageLegacyErrors{Fault: 4, Caller: 1, Expected: 2},
			})

			bySeverity := map[UsageErrorSeverity]int64{}
			for _, group := range summary.Errors {
				bySeverity[group.Severity] += group.Count
			}

			convey.So(bySeverity, convey.ShouldResemble, map[UsageErrorSeverity]int64{
				UsageErrorSeverityFault: 5, UsageErrorSeverityCaller: 1, UsageErrorSeverityExpected: 2,
			})
		})
	})
}

func TestUsageErrorProbes(t *testing.T) {
	convey.Convey("Given misses from probes and from a final lookup, and a probe that failed", t, func() {
		at := func(hoursAgo int) time.Time { return usageSummaryTestNow.Add(-time.Duration(hoursAgo) * time.Hour) }
		miss := func(hoursAgo int, route string, probe bool) UsageEvent {
			return UsageEvent{
				Time: at(hoursAgo), Category: UsageCategoryCLI, Method: "GET", Route: route, Status: 404,
				ErrorCode: httpErrorCodeNotFound, Probe: probe,
			}
		}

		summary := summariseEventsForTest(t, []UsageEvent{
			miss(1, "/resolve/study/:id", true),
			miss(2, "/resolve/study/:id", true),
			miss(30, "/resolve/study/:id", true),
			miss(1, "/resolve/study/:id", false),
			miss(1, "/classify/:id", false),
			{
				Time: at(1), Category: UsageCategoryCLI, Method: "GET", Route: "/resolve/study/:id", Status: 502,
				ErrorCode: httpErrorCodeUpstreamImpaired, Probe: true,
			},
			{Time: at(1), Category: UsageCategoryCLI, Method: "GET", Route: "/classify/:id", Status: 200, Probe: true},
		}, UsageWindow7d)

		convey.Convey("then probe and final misses form separate groups, and a failed probe is a fault", func() {
			keys := make([]string, 0, len(summary.Errors))
			for _, group := range summary.Errors {
				keys = append(keys, fmt.Sprintf("%s %d %s %t %d", group.Route, group.Status, group.Severity,
					group.Probe, group.Count))
			}

			convey.So(keys, convey.ShouldResemble, []string{
				"/resolve/study/:id 502 fault true 1",
				"/resolve/study/:id 404 expected true 3",
				"/classify/:id 404 expected false 1",
				"/resolve/study/:id 404 expected false 1",
			})
		})

		convey.Convey("then the totals count expected probe answers apart from other expected answers", func() {
			convey.So(summary.Totals.ErrorSeverity, convey.ShouldResemble, UsageErrorBreakdown{
				Fault: 1, Expected: 2, Probe: 3,
			})
		})

		convey.Convey("then each day carries its own breakdown", func() {
			breakdowns := map[string]UsageErrorBreakdown{}
			for _, day := range summary.Days {
				breakdowns[day.Date] = day.ErrorSeverity
			}

			convey.So(breakdowns["2026-10-09"], convey.ShouldResemble, UsageErrorBreakdown{Probe: 1})
			convey.So(breakdowns["2026-10-10"], convey.ShouldResemble, UsageErrorBreakdown{
				Fault: 1, Expected: 2, Probe: 2,
			})
			convey.So(breakdowns["2026-10-08"], convey.ShouldResemble, UsageErrorBreakdown{})
		})
	})
}

// usageBreakdownOfOneForTest returns the breakdown of one error row of
// severity, recorded before error details when legacy.
func usageBreakdownOfOneForTest(severity UsageErrorSeverity, legacy bool) UsageErrorBreakdown {
	var one int64
	if legacy {
		one = 1
	}

	switch severity {
	case UsageErrorSeverityFault:
		return UsageErrorBreakdown{Fault: 1, Legacy: UsageLegacyErrors{Fault: one}}
	case UsageErrorSeverityCaller:
		return UsageErrorBreakdown{Caller: 1, Legacy: UsageLegacyErrors{Caller: one}}
	default:
		return UsageErrorBreakdown{Expected: 1, Legacy: UsageLegacyErrors{Expected: one}}
	}
}

// usageFixtureFForTest returns the nine rows of spec fixture F.
func usageFixtureFForTest() []UsageEvent {
	type row struct {
		hoursAgo  int
		category  UsageCategory
		route     string
		path      string
		clientID  string
		username  string
		ip        string
		userAgent string
		tool      string
		agent     string
		status    int
		latencyMs int
	}

	cliUA, mcpUA, curlUA := "wa-cli/v0.11.0", "mlwh-mcp-server/0.5.0", "curl/8.5.0"
	studies := usageSummaryTestRouteStudies

	rows := []row{
		{1, UsageCategoryCLI, studies, "", "run-a", "alice", "10.0.0.1", cliUA, "", "", 200, 10},
		{2, UsageCategoryCLI, studies, "", "run-a", "alice", "10.0.0.1", cliUA, "", "", 200, 30},
		{3, UsageCategoryCLI, "/classify/:id", "", "run-b", "bob", "10.0.0.1", cliUA, "", "", 404, 5},
		{
			4, UsageCategoryMCP, studies, "", "sess-1", "", "10.0.0.2", mcpUA,
			"mlwh_list_studies", usageSummaryTestAgent, 200, 20,
		},
		{5, UsageCategoryOther, studies, "", "", "", "10.0.0.9", curlUA, "", "", 500, 40},
		{6, UsageCategoryOther, "", "/bogus", "", "", "10.0.0.9", curlUA, "", "", 404, 1},
		{30, UsageCategoryCLI, studies, "", "run-a", "alice", "10.0.0.1", cliUA, "", "", 200, 10},
		{60, UsageCategoryWeb, studies, "", "web-1", "alice", "10.0.0.3", "wa-web", "", "", 200, 50},
		{228, UsageCategoryCLI, studies, "", "run-c", "carol", "10.0.0.1", cliUA, "", "", 200, 10},
	}

	events := make([]UsageEvent, 0, len(rows))

	for _, r := range rows {
		events = append(events, UsageEvent{
			Time:       usageSummaryTestNow.Add(-time.Duration(r.hoursAgo) * time.Hour),
			Category:   r.category,
			Method:     "GET",
			Route:      r.route,
			Path:       r.path,
			ClientID:   r.clientID,
			Username:   r.username,
			RemoteAddr: r.ip,
			UserAgent:  r.userAgent,
			MCPTool:    r.tool,
			MCPAgent:   r.agent,
			Status:     r.status,
			Latency:    time.Duration(r.latencyMs) * time.Millisecond,
		})
	}

	// The classify 404 and the unmatched 404 carry error details; the
	// /studies 500 has none, as rows recorded before details were captured.
	events[2].Path, events[2].ErrorCode = "/classify/XYZ", httpErrorCodeNotFound
	events[2].ErrorMessage = "mlwh: not found: XYZ"
	events[5].ErrorCode = UsageErrorCodeNoRoute

	return events
}

// summariseEventsForTest writes events to a new store with retention 365 and
// the clock fixed at usageSummaryTestNow, then summarises window w.
func summariseEventsForTest(t *testing.T, events []UsageEvent, w UsageWindow) UsageSummary {
	t.Helper()

	return summariseFilteredEventsForTest(t, events, w, UsageFilter{})
}

// summariseFilteredEventsForTest is summariseEventsForTest narrowed by filter.
func summariseFilteredEventsForTest(t *testing.T, events []UsageEvent, w UsageWindow,
	filter UsageFilter,
) UsageSummary {
	t.Helper()

	store := openTestUsageStore(t, filepath.Join(t.TempDir(), "u.sqlite"), 365, usageQueueSize, usageSummaryTestNow)
	recordUsageEventsForTest(store, events)

	summary, err := store.Summary(context.Background(), w, filter)
	convey.So(err, convey.ShouldBeNil)

	return summary
}

func usagePeriodRequestsForTest(series []UsagePeriod) []int64 {
	requests := make([]int64, 0, len(series))

	for _, period := range series {
		requests = append(requests, period.Requests)
	}

	return requests
}

func recordUsageEventsForTest(store *UsageStore, events []UsageEvent) {
	for _, ev := range events {
		store.Record(ev)
	}

	convey.So(store.Flush(context.Background()), convey.ShouldBeNil)
}

// usageErrorFixtureForTest returns error rows for the 24h window: three
// not_found (one MCP), one unmatched, one legacy row with no details, one
// timeout, plus a successful MCP row and a not_found row outside the window.
func usageErrorFixtureForTest() []UsageEvent {
	at := func(hoursAgo int) time.Time { return usageSummaryTestNow.Add(-time.Duration(hoursAgo) * time.Hour) }
	classify := "/classify/:id"
	tool := "mlwh_classify_identifier"
	cli := func(hoursAgo int, path, message string) UsageEvent {
		return UsageEvent{
			Time: at(hoursAgo), Category: UsageCategoryCLI, Method: "GET", Route: classify, Path: path,
			ClientID: "run-a", Username: "alice", Status: 404, ErrorCode: httpErrorCodeNotFound,
			ErrorMessage: message,
		}
	}

	return []UsageEvent{
		{
			Time: at(1), Category: UsageCategoryMCP, Method: "GET", Route: classify, Path: "/classify/A",
			Query: "x=1", ClientID: "sess-1", MCPTool: tool, MCPAgent: usageSummaryTestAgent, Status: 404,
			ErrorCode: httpErrorCodeNotFound, ErrorMessage: "no A",
		},
		cli(2, "/classify/A", "no A"),
		cli(3, "/classify/B", "no B"),
		{Time: at(4), Category: UsageCategoryCLI, Method: "GET", Route: classify, Status: 404},
		{
			Time: at(5), Category: UsageCategoryOther, Method: "GET", Path: "/bogus", Status: 404,
			ErrorCode: UsageErrorCodeNoRoute,
		},
		{
			Time: at(6), Category: UsageCategoryCLI, Method: "GET", Route: classify, Path: "/classify/C",
			Status: 502, ErrorCode: httpErrorCodeUpstreamImpaired, ErrorKind: UsageErrorKindTimeout,
			ErrorMessage: "deadline",
		},
		{
			Time: at(7), Category: UsageCategoryMCP, Method: "GET", Route: classify, MCPTool: tool,
			MCPAgent: usageSummaryTestAgent, Status: 200,
		},
		cli(30, "/classify/Z", "no Z"),
	}
}

func latencyUsageEventForTest(ms int) UsageEvent {
	return UsageEvent{
		Time: usageSummaryTestNow.Add(-time.Hour), Category: UsageCategoryCLI, Method: "GET",
		Route: usageSummaryTestRouteStudies, Status: 200, Latency: time.Duration(ms) * time.Millisecond,
	}
}

// countNonArraysForTest counts the named keys of doc that are not JSON arrays.
func countNonArraysForTest(doc map[string]any, keys []string) int {
	bad := 0

	for _, key := range keys {
		if _, ok := doc[key].([]any); !ok {
			bad++
		}
	}

	return bad
}

// countNonObjectByMapsForTest counts by_* fields, in the entries of the named
// list keys of doc, that are not JSON objects.
func countNonObjectByMapsForTest(doc map[string]any, lists []string) int {
	bad := 0

	for _, list := range lists {
		entries, _ := doc[list].([]any)

		for _, entry := range entries {
			fields, _ := entry.(map[string]any)

			for _, key := range []string{"by_category", "by_endpoint", "by_tool", "by_agent"} {
				value, present := fields[key]
				if _, isObject := value.(map[string]any); present && !isObject {
					bad++
				}
			}
		}
	}

	return bad
}
