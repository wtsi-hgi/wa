import { describe, expect, it } from "vitest";

import {
    usageSummarySchema,
    type UsageErrorBreakdown,
    type UsageSummary,
} from "@/lib/contracts";
import {
    activeCategories,
    agentProduct,
    agentProducts,
    attentionRows,
    clipBuckets,
    endpointGroups,
    errorAnchors,
    expectedGroups,
    fallbackSeverity,
    groupSeverity,
    periodChanges,
    previousComparable,
    routePrefix,
    slug,
    timeBuckets,
    usageHref,
    weekBuckets,
    weekStart,
    withFilter,
    worstSeverity,
    type TimeBucket,
    type UsageErrorGroup,
    type UsagePeriod,
} from "@/lib/usage-derive";

import { niceMax, tickIndexes } from "@/components/usage/activity-chart";
import { impliedErrorCode, usageGroupHelp } from "@/lib/usage-error-help";

import anonFixture from "./fixtures/usage-summary-30d-anon.json";

function anonSummary(overrides: Partial<UsageSummary> = {}): UsageSummary {
    return { ...usageSummarySchema.parse(anonFixture), ...overrides };
}

function group(overrides: Partial<UsageErrorGroup>): UsageErrorGroup {
    return {
        method: "GET",
        route: "/classify/:id",
        status: 404,
        error_code: "not_found",
        count: 1,
        first_seen: "2026-10-10T09:00:00Z",
        last_seen: "2026-10-10T09:00:00Z",
        error_kinds: {},
        messages: [],
        examples: [],
        by_category: { cli: 1 },
        by_tool: {},
        by_agent: {},
        ...overrides,
    };
}

function breakdown(
    overrides: Partial<UsageErrorBreakdown> = {},
): UsageErrorBreakdown {
    return {
        fault: 0,
        caller: 0,
        expected: 0,
        probe: 0,
        legacy: { fault: 0, caller: 0, expected: 0 },
        ...overrides,
    };
}

function period(
    start: string,
    overrides: Partial<UsagePeriod> = {},
): UsagePeriod {
    return {
        start,
        end: start,
        requests: 0,
        by_category: {},
        by_endpoint: {},
        ...overrides,
    };
}

describe("fallbackSeverity", () => {
    // Each case matches the server's usageErrorSeveritySQL.
    const cases: Array<[string, Partial<UsageErrorGroup>, string]> = [
        ["5xx", { status: 502, error_code: "upstream_impaired" }, "fault"],
        [
            "500 from a third party",
            {
                status: 500,
                error_code: "internal_error",
                by_category: { other: 1 },
            },
            "fault",
        ],
        ["legacy 5xx", { status: 500, error_code: null }, "fault"],
        ["not_found", { status: 404, error_code: "not_found" }, "expected"],
        ["ambiguous", { status: 409, error_code: "ambiguous" }, "expected"],
        [
            "not_found from MCP",
            { error_code: "not_found", by_category: { mcp: 1 } },
            "expected",
        ],
        ["legacy 404 on a route", { error_code: null }, "expected"],
        [
            "legacy 409 on a route",
            { status: 409, error_code: null },
            "expected",
        ],
        [
            "legacy unmatched 404 from curl",
            { route: "", error_code: null, by_category: { other: 1 } },
            "caller",
        ],
        [
            "legacy CLI 400",
            { status: 400, error_code: null, by_category: { cli: 1 } },
            "fault",
        ],
        [
            "CLI bad_request",
            { status: 400, error_code: "bad_request" },
            "fault",
        ],
        [
            "web no_route",
            {
                route: "",
                error_code: "no_route",
                by_category: { web: 1 },
            },
            "fault",
        ],
        [
            "MCP bad_request",
            { status: 400, error_code: "bad_request", by_category: { mcp: 2 } },
            "caller",
        ],
        [
            "other no_route",
            { route: "", error_code: "no_route", by_category: { other: 1 } },
            "caller",
        ],
        [
            "results_server uncoded",
            {
                status: 400,
                error_code: "uncoded",
                by_category: { results_server: 1 },
            },
            "fault",
        ],
    ];

    for (const [name, overrides, want] of cases) {
        it(`judges ${name} as ${want}`, () => {
            expect(fallbackSeverity(group(overrides))).toBe(want);
        });
    }

    it("prefers the server's severity and falls back for unknown ones", () => {
        expect(groupSeverity(group({ severity: "caller" }))).toBe("caller");
        expect(groupSeverity(group({ severity: "mystery" }))).toBe("expected");
        expect(groupSeverity(group({}))).toBe("expected");
    });
});

describe("attentionRows", () => {
    it("merges faults across routes by status and code, faults first", () => {
        const rows = attentionRows([
            group({ severity: "expected", count: 200 }),
            group({
                route: "/no/such",
                status: 404,
                error_code: "no_route",
                severity: "caller",
                count: 9,
                by_category: { other: 9 },
            }),
            group({
                route: "/a",
                status: 502,
                error_code: "upstream_impaired",
                severity: "fault",
                count: 2,
                last_seen: "2026-10-10T10:00:00Z",
                by_category: { cli: 2 },
            }),
            group({
                route: "/b",
                status: 502,
                error_code: "upstream_impaired",
                severity: "fault",
                count: 3,
                by_category: { mcp: 3 },
                by_tool: { mlwh_export: 3 },
            }),
            group({
                route: "/c",
                status: 400,
                error_code: "bad_request",
                severity: "fault",
                count: 1,
            }),
        ]);

        expect(
            rows.map((row) => [row.id, row.count, row.routes.length]),
        ).toEqual([
            ["err-fault-502-upstream_impaired", 5, 2],
            ["err-fault-400-bad_request", 1, 1],
            ["err-caller-404-no_route", 9, 1],
        ]);
        expect(rows[0].routes.map((route) => route.label)).toEqual([
            "GET /b",
            "GET /a",
        ]);
        expect(rows[0].routes[0].id).toBe(
            "err-fault-502-upstream_impaired-GET-b",
        );
        expect(rows[0].categories).toEqual({ cli: 2, mcp: 3 });
        expect(rows[0].tools).toEqual({ mlwh_export: 3 });
        expect(rows[0].lastSeen).toBe("2026-10-10T10:00:00Z");
    });

    it("keeps legacy faults apart from coded ones", () => {
        const rows = attentionRows([
            group({ status: 502, error_code: null, severity: "fault" }),
            group({
                status: 502,
                error_code: "upstream_impaired",
                severity: "fault",
            }),
        ]);

        expect(rows.map((row) => row.id)).toEqual([
            "err-fault-502-legacy",
            "err-fault-502-upstream_impaired",
        ]);
    });

    it("gives the real 30-day shape 15 faults in two rows and 3 caller errors", () => {
        const rows = attentionRows(anonSummary().errors ?? []);

        expect(
            rows.map((row) => [row.severity, row.status, row.code, row.count]),
        ).toEqual([
            ["fault", 502, null, 14],
            ["fault", 400, null, 1],
            ["caller", 404, null, 2],
            ["caller", 404, "no_route", 1],
        ]);
        expect(rows[0].routes).toHaveLength(6);
    });
});

describe("expectedGroups and errorAnchors", () => {
    const errors = [
        group({ severity: "expected", count: 4 }),
        group({ severity: "expected", error_code: null, count: 2 }),
        group({
            route: "/x",
            status: 502,
            error_code: "upstream_impaired",
            severity: "fault",
        }),
        group({
            route: "/x",
            status: 404,
            severity: "expected",
        }),
    ];

    it("splits expected groups by whether they have details", () => {
        const { detailed, legacy } = expectedGroups(errors);

        expect(detailed.map((g) => g.route)).toEqual(["/classify/:id", "/x"]);
        expect(legacy.map((g) => g.count)).toEqual([2]);
    });

    it("anchors each route at its first listing: attention, expected, older", () => {
        expect(Object.fromEntries(errorAnchors(errors))).toEqual({
            "GET /x": "err-fault-502-upstream_impaired-GET-x",
            "GET /classify/:id": "err-expected-GET-classify-_id",
        });
        expect(
            Object.fromEntries(
                errorAnchors([
                    group({ severity: "expected", error_code: null }),
                ]),
            ),
        ).toEqual({ "GET /classify/:id": "err-older-GET-classify-_id" });
    });
});

describe("periodChanges", () => {
    const earlier = "2026-10-01T00:00:00Z";
    const series = [
        period("2026-10-02T00:00:00Z", {
            by_category: { cli: 100, web: 10 },
            by_endpoint: { "GET /a": 20, "GET /b": 20, "GET /c": 3 },
            by_tool: { t1: 4 },
            by_user: { alice: 10 },
        }),
        period("2026-10-09T00:00:00Z", {
            by_category: { cli: 126, mcp: 5 },
            by_endpoint: { "GET /a": 26, "GET /b": 10, "GET /c": 4 },
            by_tool: { t1: 4 },
            by_user: { alice: 10, bob: 1 },
        }),
    ];

    it("classifies new, quiet, up, and down, ignoring small or steady counts", () => {
        const result = periodChanges(series, earlier);

        expect(result.status).toBe("ok");
        expect(
            result.status === "ok" &&
                result.changes.map((c) => `${c.kind} ${c.dimension} ${c.key}`),
        ).toEqual([
            "new category mcp",
            "new user bob",
            "quiet category web",
            "up category cli",
            "up endpoint GET /a",
            "down endpoint GET /b",
        ]);
    });

    it("needs two periods and records reaching back to the previous one", () => {
        expect(periodChanges(series.slice(1), earlier)).toEqual({
            status: "short",
            earliest: earlier,
        });
        expect(periodChanges(series, "2026-10-05T00:00:00Z").status).toBe(
            "short",
        );
        expect(periodChanges(series, "2026-10-02T00:00:00Z").status).toBe("ok");
    });

    it("tolerates series without tool and user breakdowns", () => {
        const old = series.map(
            ({ by_tool: _tool, by_user: _user, ...rest }) => rest,
        );
        const result = periodChanges(old, earlier);

        expect(
            result.status === "ok" &&
                result.changes.every(
                    (c) =>
                        c.dimension === "category" ||
                        c.dimension === "endpoint",
                ),
        ).toBe(true);
    });
});

describe("agent and endpoint grouping", () => {
    it("merges agents by product and sums their severities", () => {
        expect(agentProduct("claude-code/2.1.0 (cli)")).toBe("claude-code");
        expect(agentProduct("curl")).toBe("curl");

        const products = agentProducts([
            {
                value: "a/1",
                requests: 2,
                errors: 1,
                error_severity: breakdown({ caller: 1 }),
            },
            {
                value: "b/1",
                requests: 5,
                errors: 0,
                error_severity: breakdown(),
            },
            {
                value: "a/2",
                requests: 4,
                errors: 1,
                error_severity: breakdown({ fault: 1 }),
            },
        ]);

        expect(products.map((p) => [p.product, p.requests, p.errors])).toEqual([
            ["a", 6, 2],
            ["b", 5, 0],
        ]);
        expect(products[0].severity).toEqual(
            breakdown({ caller: 1, fault: 1 }),
        );
        expect(products[0].versions.map((v) => v.value)).toEqual([
            "a/1",
            "a/2",
        ]);
    });

    it("groups the real 40 routes by first path segment", () => {
        expect(routePrefix("/resolve/study/:id")).toBe("/resolve");
        expect(routePrefix("/studies")).toBe("/studies");
        expect(routePrefix("/")).toBe("/");

        const groups = endpointGroups(anonSummary().endpoints);
        const resolve = groups.find((g) => g.prefix === "/resolve");

        expect(groups).toHaveLength(16);
        expect(groups.reduce((sum, g) => sum + g.routes.length, 0)).toBe(40);
        expect(resolve?.routes.length).toBeGreaterThan(1);
        expect(resolve?.severity?.fault).toBe(0);
        expect(resolve?.severity?.expected).toBeGreaterThan(300);
        expect(groups.map((g) => g.requests)).toEqual(
            [...groups.map((g) => g.requests)].sort((a, b) => b - a),
        );
    });

    it("names the worst severity in a breakdown", () => {
        expect(worstSeverity(undefined)).toBeNull();
        expect(worstSeverity(breakdown())).toBeNull();
        expect(worstSeverity(breakdown({ probe: 1 }))).toBe("expected");
        expect(worstSeverity(breakdown({ probe: 1, caller: 1 }))).toBe(
            "caller",
        );
        expect(worstSeverity(breakdown({ caller: 1, fault: 1 }))).toBe("fault");
    });
});

describe("time buckets", () => {
    const day = (date: string, requests: number): TimeBucket => ({
        byCategory: { cli: requests },
        requests,
        severity: breakdown({ fault: requests }),
        start: `${date}T00:00:00Z`,
    });

    it("aligns weeks to Mondays and sums days into them", () => {
        // 2026-10-19 is a Monday.
        expect(
            new Date(
                weekStart(Date.parse("2026-10-21T15:00:00Z")),
            ).toISOString(),
        ).toBe("2026-10-19T00:00:00.000Z");
        expect(
            new Date(
                weekStart(Date.parse("2026-10-19T00:00:00Z")),
            ).toISOString(),
        ).toBe("2026-10-19T00:00:00.000Z");

        const weeks = weekBuckets([
            day("2026-10-18", 1),
            day("2026-10-19", 2),
            day("2026-10-25", 3),
            day("2026-10-26", 4),
        ]);

        expect(
            weeks.map((w) => [w.start, w.requests, w.severity?.fault]),
        ).toEqual([
            ["2026-10-12T00:00:00Z", 1, 1],
            ["2026-10-19T00:00:00Z", 5, 5],
            ["2026-10-26T00:00:00Z", 4, 4],
        ]);
    });

    it("uses the server's buckets, else days, else weeks for a long all window", () => {
        const summary = anonSummary();

        expect(timeBuckets(summary).unit).toBe("day");
        expect(timeBuckets(summary).buckets).toHaveLength(31);

        const old = anonSummary({ buckets: undefined, bucket_unit: undefined });

        expect(timeBuckets(old).buckets[30]).toEqual(
            timeBuckets(summary).buckets[30],
        );

        const longAll = anonSummary({
            buckets: undefined,
            bucket_unit: undefined,
            window: "all",
            days: [
                { date: "2026-01-05", requests: 1, by_category: { cli: 1 } },
                { date: "2026-10-20", requests: 2, by_category: { cli: 2 } },
            ],
        });

        expect(timeBuckets(longAll).unit).toBe("week");
        expect(timeBuckets(longAll).buckets.map((b) => b.start)).toEqual([
            "2026-01-05T00:00:00Z",
            "2026-10-19T00:00:00Z",
        ]);
    });

    it("clips buckets that end before the earliest record", () => {
        const { buckets, unit } = timeBuckets(anonSummary());
        const clipped = clipBuckets(buckets, unit, "2026-10-20T08:39:29Z");

        expect(clipped.clipped).toBe(true);
        expect(clipped.buckets.map((b) => b.start)).toEqual([
            "2026-10-20T00:00:00Z",
            "2026-10-21T00:00:00Z",
        ]);
        expect(clipBuckets(buckets, unit, "").clipped).toBe(false);
        expect(
            clipBuckets(buckets, unit, "2026-09-21T00:00:00Z").buckets,
        ).toHaveLength(31);
        expect(
            clipBuckets(
                [{ ...buckets[0], start: "2026-10-20T08:00:00Z" }],
                "hour",
                "2026-10-20T08:59:59Z",
            ).buckets,
        ).toHaveLength(1);
    });
});

describe("summary helpers", () => {
    it("lists active and idle categories", () => {
        expect(activeCategories(anonSummary())).toEqual({
            active: ["cli", "mcp", "other"],
            idle: ["web", "results_server", "mlwhdiff_server"],
        });
    });

    it("compares with the previous window only when records cover it", () => {
        const base = {
            window: "24h" as const,
            generated_at: "2026-10-10T12:00:00Z",
            window_start: "2026-10-09T12:00:00Z",
        };

        expect(previousComparable(anonSummary())).toBe(false);
        expect(
            previousComparable(
                anonSummary({
                    ...base,
                    earliest_record: "2026-10-08T12:00:00Z",
                }),
            ),
        ).toBe(true);
        expect(
            previousComparable(
                anonSummary({
                    ...base,
                    earliest_record: "2026-10-08T12:00:01Z",
                }),
            ),
        ).toBe(false);
        expect(
            previousComparable(
                anonSummary({
                    ...base,
                    earliest_record: "2026-10-01T00:00:00Z",
                    previous_totals: undefined,
                }),
            ),
        ).toBe(false);
    });

    it("builds filter links, dropping the default window and empty values", () => {
        expect(usageHref("7d", {})).toBe("/usage");
        expect(usageHref("30d", { user: "a b&c" })).toBe(
            "/usage?window=30d&user=a+b%26c",
        );
        expect(
            usageHref("7d", {
                method: "GET",
                route: "/studies/:id",
                category: "mcp",
            }),
        ).toBe("/usage?category=mcp&method=GET&route=%2Fstudies%2F%3Aid");
    });

    it("adds and removes filters, removing method and route together", () => {
        const filter = { user: "alice", method: "GET", route: "/a" };

        expect(withFilter(filter, { tool: "t" })).toEqual({
            ...filter,
            tool: "t",
        });
        expect(withFilter(filter, { route: undefined })).toEqual({
            user: "alice",
        });
        expect(withFilter(filter, { method: undefined })).toEqual({
            user: "alice",
        });
        expect(withFilter(filter, { user: undefined })).toEqual({
            method: "GET",
            route: "/a",
        });
    });
});

describe("ids, help, and chart scales", () => {
    it("keeps route parameters distinct in ids", () => {
        expect(slug("GET /a/:id")).toBe("GET-a-_id");
        expect(slug("GET /a/id")).toBe("GET-a-id");
    });

    it("infers the code of a group recorded before error details", () => {
        expect(impliedErrorCode(502, "/a")).toBe("upstream_impaired");
        expect(impliedErrorCode(404, "")).toBe("no_route");
        expect(impliedErrorCode(404, "/a")).toBe("not_found");
        expect(impliedErrorCode(418, "/a")).toBeNull();
        expect(usageGroupHelp(null, 502, "/a").label).toBe(
            "upstream_impaired (inferred, recorded before error details)",
        );
        expect(usageGroupHelp(null, 502, "/a").action).toBe(
            usageGroupHelp("upstream_impaired", 502, "/a").action,
        );
        expect(usageGroupHelp(null, 418, "/a").label).toBe(
            "unknown (recorded before error details were captured)",
        );
    });

    it("rounds the axis top up to 1, 2, or 5 times a power of ten", () => {
        expect([0, 1, 3, 7, 10, 11, 859, 2001].map(niceMax)).toEqual([
            1, 1, 5, 10, 10, 20, 1000, 5000,
        ]);
    });

    it("labels at most seven evenly spaced buckets, first and last included", () => {
        expect(tickIndexes(1)).toEqual([0]);
        expect(tickIndexes(7)).toEqual([0, 1, 2, 3, 4, 5, 6]);

        for (const n of [8, 25, 31, 366]) {
            const ticks = tickIndexes(n);

            expect(ticks.length, String(n)).toBeLessThanOrEqual(7);
            expect(ticks[0]).toBe(0);
            expect(ticks.at(-1)).toBe(n - 1);
        }
    });
});
