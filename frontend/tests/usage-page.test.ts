/**
 * @vitest-environment jsdom
 */

import { createElement } from "react";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
    UsageSummaryState,
    UsageUnavailableReason,
} from "@/app/(results)/usage/actions";
import {
    usageSummarySchema,
    type UsageErrorBreakdown,
    type UsageFilter,
    type UsageSummary,
    type UsageWindow,
} from "@/lib/contracts";
import { impliedErrorCode } from "@/lib/usage-error-help";

import usageSummaryAnonFixture from "./fixtures/usage-summary-30d-anon.json";
import usageSummaryA3Fixture from "./fixtures/usage-summary-a3-24h.json";

const actionMocks = vi.hoisted(() => ({
    getUsageSummaryAction: vi.fn(),
}));

vi.mock("@/app/(results)/usage/actions", () => actionMocks);

function a3Summary(overrides: Partial<UsageSummary> = {}): UsageSummary {
    return {
        ...usageSummarySchema.parse(usageSummaryA3Fixture),
        ...overrides,
    };
}

async function renderPage(searchParams: Record<string, string | string[]>) {
    const { default: UsageAdminPage } =
        await import("@/app/(results)/usage/page");
    const element = await UsageAdminPage({
        searchParams: Promise.resolve(searchParams),
    });

    return render(element);
}

// anonSummary is the real 30-day production shape, anonymised, with the
// severity fields a current server adds.
function anonSummary(overrides: Partial<UsageSummary> = {}): UsageSummary {
    return {
        ...usageSummarySchema.parse(usageSummaryAnonFixture),
        ...overrides,
    };
}

// detailedAnon is anonSummary with its faults recorded as if after error
// details: each code-less fault group takes the code its status implies, and
// no breakdown counts a fault as legacy. Its legacy caller errors and expected
// misses stay legacy.
function detailedAnon(): UsageSummary {
    const summary = anonSummary();
    const detailed = <T extends { error_severity?: UsageErrorBreakdown }>(
        row: T,
    ): T =>
        row.error_severity
            ? {
                  ...row,
                  error_severity: {
                      ...row.error_severity,
                      legacy: { ...row.error_severity.legacy, fault: 0 },
                  },
              }
            : row;

    return {
        ...summary,
        totals: detailed(summary.totals),
        categories: summary.categories.map(detailed),
        endpoints: summary.endpoints.map(detailed),
        mcp_tools: summary.mcp_tools.map(detailed),
        mcp_agents: summary.mcp_agents.map(detailed),
        other_user_agents: summary.other_user_agents.map(detailed),
        days: summary.days.map(detailed),
        buckets: summary.buckets?.map(detailed),
        errors: summary.errors?.map((group) =>
            group.error_code === null && group.severity === "fault"
                ? {
                      ...group,
                      error_code: impliedErrorCode(group.status, group.route),
                  }
                : group,
        ),
    };
}

async function renderView(
    summary: UsageSummary,
    window: UsageWindow,
    filter: UsageFilter = {},
) {
    const { UsageAdminView } = await import("@/components/usage-admin-view");

    return render(createElement(UsageAdminView, { filter, summary, window }));
}

function section(name: string): HTMLElement {
    return screen.getByRole("region", { name });
}

// kpi returns the value and change text of the KPI tile label.
function kpi(label: string): { change: string; detail: string; value: string } {
    const tile = screen.getByTestId(`kpi-${label}`);
    const details = tile.querySelectorAll("dd");

    return {
        change: within(tile).queryByTestId("kpi-change")?.textContent ?? "",
        detail: details[details.length - 1]?.textContent ?? "",
        value: within(tile).getByTestId("kpi-value").textContent ?? "",
    };
}

// attentionItems returns the Needs attention rows' summary text.
function attentionItems(): string[] {
    return within(section("Needs attention"))
        .queryAllByRole("listitem")
        .filter((item) => item.id.startsWith("err-"))
        .map((item) => item.querySelector("summary")?.textContent ?? "");
}

// panelLinks returns the hrefs of the links in region name, by link text.
function panelLinks(name: string): Record<string, string> {
    return Object.fromEntries(
        within(section(name))
            .getAllByRole("link")
            .map((link) => [link.textContent, link.getAttribute("href") ?? ""]),
    );
}

const noteText =
    "Usernames are self-declared by callers. Web counts are requests that reached the MLWH server; lookups served from browser or Next.js caches are not counted.";

beforeEach(() => {
    actionMocks.getUsageSummaryAction.mockResolvedValue({
        status: "ok",
        summary: a3Summary(),
    } satisfies UsageSummaryState);
});

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

describe("E4 usage page", () => {
    it("defaults any invalid window to 7d", async () => {
        const cases: Array<{
            expected: UsageWindow;
            params: Record<string, string | string[]>;
        }> = [
            { params: {}, expected: "7d" },
            { params: { window: "30d" }, expected: "30d" },
            { params: { window: "24h" }, expected: "24h" },
            { params: { window: "all" }, expected: "all" },
            { params: { window: "bogus" }, expected: "7d" },
            { params: { window: ["24h", "7d"] }, expected: "7d" },
        ];

        for (const { params, expected } of cases) {
            actionMocks.getUsageSummaryAction.mockClear();
            await renderPage(params);
            cleanup();

            expect(
                actionMocks.getUsageSummaryAction,
                JSON.stringify(params),
            ).toHaveBeenCalledExactlyOnceWith(expected, {});
        }
    });

    it("handles absent searchParams", async () => {
        const { default: UsageAdminPage } =
            await import("@/app/(results)/usage/page");

        render(await UsageAdminPage({}));

        expect(actionMocks.getUsageSummaryAction).toHaveBeenCalledWith(
            "7d",
            {},
        );
    });

    it("passes a valid filter to the action and drops an invalid one", async () => {
        const cases: Array<{
            expected: UsageFilter;
            params: Record<string, string | string[]>;
        }> = [
            {
                params: { category: "mcp", user: "user01", ignored: "x" },
                expected: { category: "mcp", user: "user01" },
            },
            {
                params: {
                    method: "GET",
                    route: "/studies",
                    tool: "t",
                    agent: "a",
                },
                expected: {
                    method: "GET",
                    route: "/studies",
                    tool: "t",
                    agent: "a",
                },
            },
            { params: { user: ["a", "b"], tool: "" }, expected: {} },
            { params: { category: "bogus", user: "user01" }, expected: {} },
            { params: { method: "GET" }, expected: {} },
            { params: { method: "GET", route: "studies" }, expected: {} },
            { params: { user: "u".repeat(129) }, expected: {} },
        ];

        for (const { params, expected } of cases) {
            actionMocks.getUsageSummaryAction.mockClear();
            await renderPage(params);
            cleanup();

            expect(
                actionMocks.getUsageSummaryAction,
                JSON.stringify(params),
            ).toHaveBeenCalledExactlyOnceWith("7d", expected);
        }
    });

    it("renders the view for an ok state with the requested window", async () => {
        await renderPage({ window: "24h" });

        expect(
            screen
                .getByRole("link", { name: "Last 24 hours" })
                .getAttribute("aria-current"),
        ).toBe("page");
        expect(screen.getByText(noteText)).toBeTruthy();
    });

    const reasonMessages: Record<UsageUnavailableReason, string> = {
        no_token: "The MLWH server token is not readable by this server.",
        usage_disabled: "Usage tracking is disabled on the MLWH server.",
        token_rejected: "The MLWH server rejected the admin token.",
        unsupported:
            "wa mlwh serve is too old for usage metrics (needs MLWH API 1.10.0).",
        backend_error: "The MLWH server could not be reached.",
    };
    const statusMessages = {
        unauthenticated: "Log in to view usage.",
        forbidden: "You do not have access to usage.",
        invalid_input: "Invalid usage request.",
    } as const;
    const unavailableMessage = "Usage is unavailable.";
    const allMessages = [
        ...Object.values(reasonMessages),
        ...Object.values(statusMessages),
        unavailableMessage,
    ];

    function expectStatusPanel(expected: string[]) {
        expect(
            screen.getByRole("heading", { level: 1, name: "MLWH usage" }),
        ).toBeTruthy();

        const text = screen.getByRole("status").textContent ?? "";

        for (const message of allMessages) {
            if (expected.includes(message)) {
                expect(text).toContain(message);
            } else {
                expect(text).not.toContain(message);
            }
        }

        expect(screen.queryByText(noteText)).toBeNull();
    }

    for (const [status, message] of Object.entries(statusMessages)) {
        it(`renders the ${status} message`, async () => {
            actionMocks.getUsageSummaryAction.mockResolvedValue({ status });

            await renderPage({});

            expectStatusPanel([message]);
        });
    }

    for (const [reason, message] of Object.entries(reasonMessages)) {
        it(`renders the unavailable ${reason} message`, async () => {
            actionMocks.getUsageSummaryAction.mockResolvedValue({
                status: "unavailable",
                reason,
            });

            await renderPage({});

            expectStatusPanel([unavailableMessage, message]);
        });
    }
});

type UsageErrorGroup = NonNullable<UsageSummary["errors"]>[number];

function errorGroup(overrides: Partial<UsageErrorGroup>): UsageErrorGroup {
    return {
        method: "GET",
        route: "/classify/:id",
        status: 404,
        error_code: "not_found",
        severity: "expected",
        probe: false,
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

// oldServer strips every field a server older than the dashboard API lacks.
function oldServer(summary: UsageSummary): UsageSummary {
    const strip = <T extends object>(row: T) => {
        const { error_severity: _severity, ...rest } = row as T & {
            error_severity?: unknown;
        };

        return rest as T;
    };

    return {
        ...summary,
        filter: undefined,
        previous_totals: undefined,
        buckets: undefined,
        bucket_unit: undefined,
        totals: strip(summary.totals),
        categories: summary.categories.map(strip),
        endpoints: summary.endpoints.map(strip),
        mcp_tools: summary.mcp_tools.map(strip),
        mcp_agents: summary.mcp_agents.map(strip),
        other_user_agents: summary.other_user_agents.map(strip),
        days: summary.days.map(strip),
        series: summary.series.map(
            ({ by_tool: _tool, by_user: _user, ...rest }) => rest,
        ),
        errors: summary.errors?.map(
            ({ severity: _severity, probe: _probe, ...rest }) => rest,
        ),
    };
}

describe("usage dashboard header", () => {
    it("links each window preset and marks the current one", async () => {
        await renderView(a3Summary(), "24h");

        const nav = screen.getByRole("navigation", { name: "Window" });

        expect(
            within(nav)
                .getAllByRole("link")
                .map((link) => [
                    link.textContent,
                    link.getAttribute("href"),
                    link.getAttribute("aria-current"),
                ]),
        ).toEqual([
            ["Last 24 hours", "/usage?window=24h", "page"],
            ["Last 7 days", "/usage", null],
            ["Last 30 days", "/usage?window=30d", null],
            ["All time", "/usage?window=all", null],
        ]);
    });

    it("keeps the filter in window links only when the server filters", async () => {
        const filter = { user: "user01" };

        await renderView(anonSummary({ filter }), "30d", filter);
        expect(
            screen
                .getByRole("link", { name: "Last 24 hours" })
                .getAttribute("href"),
        ).toBe("/usage?window=24h&user=user01");

        cleanup();
        await renderView(oldServer(anonSummary()), "30d", filter);
        expect(
            screen
                .getByRole("link", { name: "Last 24 hours" })
                .getAttribute("href"),
        ).toBe("/usage?window=24h");
        expect(screen.getByRole("status").textContent).toContain(
            "This MLWH server cannot filter usage, so all traffic is shown.",
        );
    });

    it("shows removable filter chips", async () => {
        const filter: UsageFilter = {
            category: "mcp",
            method: "GET",
            route: "/studies",
        };

        await renderView(anonSummary({ filter }), "7d", filter);

        const chips = screen.getByRole("group", { name: "Filters" });

        expect(
            within(chips)
                .getAllByRole("link")
                .map((link) => [
                    link.getAttribute("aria-label") ?? link.textContent,
                    link.getAttribute("href"),
                ]),
        ).toEqual([
            [
                "Remove filter Client type MCP",
                "/usage?method=GET&route=%2Fstudies",
            ],
            ["Remove filter Endpoint GET /studies", "/usage?category=mcp"],
            ["Clear all", "/usage"],
        ]);
    });

    it("shows the note on every ok render", async () => {
        for (const window of ["24h", "7d", "30d", "all"] as const) {
            await renderView(a3Summary({ window }), window);

            expect(screen.getByText(noteText), window).toBeTruthy();
            cleanup();
        }
    });
});

describe("usage KPI strip", () => {
    it("headlines faults, not all errors", async () => {
        await renderView(detailedAnon(), "30d");

        expect(kpi("Requests").value).toBe("1,578");
        expect(kpi("Requests").detail).toBe("CLI 95.9% · MCP 3.0%");
        expect(kpi("People").value).toBe("12");
        expect(kpi("Sessions").value).toBe("292");
        expect(kpi("MCP requests").value).toBe("47");
        expect(kpi("Faults").value).toBe("15");
        expect(kpi("Faults").detail).toBe("0.95% of requests · 1 caller error");
        expect(
            within(screen.getByTestId("kpi-Faults"))
                .getByRole("link")
                .getAttribute("href"),
        ).toBe("#attention");
        // Records start inside the window, so there is nothing to compare.
        expect(kpi("Requests").change).toBe("no earlier records");
    });

    it("compares with the previous window when records cover it", async () => {
        await renderView(
            {
                ...detailedAnon(),
                window: "24h",
                window_start: "2026-10-20T15:49:43Z",
                earliest_record: "2026-10-01T00:00:00Z",
                previous_totals: {
                    requests: 1000,
                    errors: 30,
                    error_rate: 0.03,
                    error_severity: breakdown({ fault: 20 }),
                    sessions: 300,
                    ip_clients: 1,
                    users: 10,
                },
            },
            "24h",
        );

        expect(kpi("Requests").change).toBe("+58% vs previous");
        expect(kpi("People").change).toBe("+2 vs previous");
        expect(kpi("Sessions").change).toBe("−8 vs previous");
        expect(kpi("Faults").change).toBe("−5 vs previous");
    });

    it("shows Errors for a server without severities", async () => {
        await renderView(oldServer(anonSummary()), "30d");

        expect(kpi("Errors").value).toBe("483");
        expect(screen.queryByTestId("kpi-Faults")).toBeNull();
        expect(kpi("Requests").change).toBe("");
    });
});

describe("usage Needs attention", () => {
    it("lists merged faults, then caller errors, with what to do", async () => {
        await renderView(detailedAnon(), "30d");

        const items = attentionItems();

        expect(items).toHaveLength(3);
        expect(items[0]).toContain("502 upstream_impaired on 6 routes14");
        expect(items[0]).toContain(
            "Check the wa mlwh serve logs at the times shown, and the cache database's health.",
        );
        expect(items[0]).toMatch(/^Fault:/);
        expect(items[1]).toContain(
            "400 bad_request on GET /study/:id/latest-data1",
        );
        expect(items[2]).toMatch(
            /^Caller error:404 no_route on GET \(no matching route\)1/,
        );

        const region = section("Needs attention");

        expect(region.textContent).toContain("15 faults · 1 caller error");
        expect(region.textContent).toContain(
            "Expected lookup misses are not problems: 21 under Expected outcomes.",
        );
    });

    it("ignores the errors recorded before error details", async () => {
        await renderView(anonSummary({ filter: {} }), "30d");

        expect(kpi("Faults").value).toBe("0");
        expect(kpi("Faults").detail).toBe("0.0% of requests · 1 caller error");
        expect(attentionItems()).toEqual([
            expect.stringMatching(
                /^Caller error:404 no_route on GET \(no matching route\)1/,
            ),
        ]);
        expect(section("Needs attention").textContent).toContain(
            "0 faults · 1 caller error",
        );
        expect(screen.queryAllByTestId("fault-marker")).toHaveLength(0);
        expect(section("Endpoints").textContent).not.toContain("faults");
        expect(document.body.textContent).not.toContain("inferred");
        expect(
            screen.queryByRole("region", { name: /Older errors/ }),
        ).toBeNull();
        expect(
            screen.getByText(
                "461 errors recorded before error details are not shown.",
            ),
        ).toBeTruthy();
    });

    it("expands a row into its routes and lists unmatched paths for no_route", async () => {
        await renderView(detailedAnon(), "30d");

        const row = document.getElementById("err-caller-404-no_route")!;

        expect(row.querySelector("details")?.open).toBe(false);
        expect(within(row).getByText("GET /version")).toBeTruthy();
        expect(
            within(row).getByRole("group", { name: "GET (no matching route)" }),
        ).toBeTruthy();

        const faults = document.getElementById(
            "err-fault-502-upstream_impaired",
        )!;

        expect(
            within(faults)
                .getAllByRole("group")
                .map((group) => group.id),
        ).toContain(
            "err-fault-502-upstream_impaired-GET-export-_children-_parent_kind-_parent_id",
        );
    });

    it("shows notices for dropped events and slow endpoints", async () => {
        const summary = anonSummary({ dropped_events: 3 });

        summary.endpoints[0] = { ...summary.endpoints[0], p95_ms: 2500 };
        await renderView(summary, "30d");

        const text = section("Needs attention").textContent ?? "";

        expect(text).toContain(
            "3 requests were not recorded because the usage queue was full.",
        );
        expect(text).toContain(
            `GET ${summary.endpoints[0].route} is slow: p95 2.5 s.`,
        );
    });

    it("says when nothing needs fixing, and when the server reports no errors", async () => {
        await renderView(
            anonSummary({
                errors: [errorGroup({ count: 5 })],
                totals: { ...anonSummary().totals, errors: 5 },
            }),
            "30d",
        );
        expect(section("Needs attention").textContent).toContain(
            "No faults or caller errors in this window.",
        );

        cleanup();
        await renderView(anonSummary({ errors: undefined }), "30d");
        expect(section("Needs attention").textContent).toContain(
            "This MLWH server does not report error details.",
        );
    });

    it("says how many errors the shown groups cover when the server capped them", async () => {
        await renderView(
            anonSummary({
                errors: [
                    errorGroup({
                        status: 502,
                        error_code: "upstream_impaired",
                        severity: "fault",
                        count: 2,
                    }),
                ],
            }),
            "30d",
        );

        expect(section("Needs attention").textContent).toContain(
            "Showing the 1 largest error groups (2 of 22 errors).",
        );
    });

    it("classifies groups from a server without severities", async () => {
        await renderView(oldServer(anonSummary()), "30d");

        expect(attentionItems()).toHaveLength(4);
    });

    it("shows a group's causes, messages, and examples", async () => {
        await renderView(
            anonSummary({
                errors: [
                    errorGroup({
                        status: 502,
                        error_code: "upstream_impaired",
                        severity: "fault",
                        count: 2,
                        error_kinds: { timeout: 1, other: 1 },
                        messages: [{ message: "mlwh: deadline", count: 2 }],
                        examples: [
                            {
                                ts: "2026-10-10T09:00:00Z",
                                path: "/classify/ID1",
                                query: "token=[redacted]",
                                category: "mcp",
                                mcp_tool: "mlwh_classify_identifier",
                                mcp_agent: "claude-code/2.1.0",
                                client_id: "sess-1",
                                username: "",
                                message: "mlwh: deadline",
                                error_kind: "timeout",
                            },
                        ],
                        by_category: { mcp: 2 },
                        by_tool: { mlwh_classify_identifier: 2 },
                    }),
                ],
            }),
            "30d",
        );

        const entry = screen.getByRole("article", {
            name: "502 upstream_impaired",
        });

        expect(entry.textContent).toContain("Causes: other 1, timeout 1");
        expect(entry.textContent).toContain("Timeouts mean a slow query");
        expect(
            within(entry).getByText("mlwh: deadline", { selector: "li span" }),
        ).toBeTruthy();
        expect(entry.textContent).toContain("/classify/ID1?token=[redacted]");
        expect(entry.textContent).toContain(
            "MCP · mlwh_classify_identifier · claude-code/2.1.0 · sess-1",
        );
    });
});

describe("usage activity and changes", () => {
    it("clips the chart to the records and marks faults", async () => {
        await renderView(detailedAnon(), "30d");

        const region = section("Activity");

        expect(region.textContent).toContain(
            "Requests per day · no records before 20 Oct",
        );
        expect(screen.getAllByTestId("activity-column")).toHaveLength(2);
        expect(screen.getAllByTestId("fault-marker")).toHaveLength(2);
        expect(within(region).getByRole("img").getAttribute("aria-label")).toBe(
            "Requests per day from 20 Oct to 21 Oct: 1,578 requests, peak 859 at 21 Oct. 15 faults, 1 caller error.",
        );

        const table = within(region).getByRole("table");

        expect(
            within(table)
                .getAllByRole("row")
                .map((row) => row.textContent),
        ).toEqual(
            [
                "DayRequestsCLIMCPOther APIFaultsCaller errorsExpected",
                "20 Oct7196792515",
                "21 Oct8598352221",
            ].map((text, index) =>
                index === 0 ? text : expect.stringContaining(text.slice(0, 10)),
            ),
        );
    });

    it("hides a category with its legend button and shows expected misses on demand", async () => {
        await renderView(anonSummary(), "30d");

        const legend = screen.getByRole("group", { name: "Chart legend" });

        expect(
            within(legend)
                .getAllByRole("button")
                .map((button) => button.textContent),
        ).toEqual(["CLI", "MCP", "Other API", "Expected misses"]);

        const cli = within(legend).getByRole("button", { name: "CLI" });
        const segments = () =>
            document.querySelectorAll(
                '[data-testid="activity-column"] rect[data-category="cli"]',
            ).length;

        expect(segments()).toBe(2);
        fireEvent.click(cli);
        expect(cli.getAttribute("aria-pressed")).toBe("false");
        expect(segments()).toBe(0);

        expect(screen.queryAllByTestId("expected-marker")).toHaveLength(0);
        fireEvent.click(
            within(legend).getByRole("button", { name: "Expected misses" }),
        );
        // The fixture's expected misses all fall on its last day.
        expect(screen.getAllByTestId("expected-marker")).toHaveLength(1);
    });

    it("hides all-zero categories and names them", async () => {
        await renderView(anonSummary(), "30d");

        expect(
            screen.getByText(
                "No requests from Web, Results server, mlwhdiff server.",
            ),
        ).toBeTruthy();
    });

    it("says there is not enough history with one period", async () => {
        await renderView(anonSummary(), "30d");

        expect(section("What changed").textContent).toContain(
            "Not enough history yet (records start 20 Oct).",
        );
    });

    it("lists changes with links that filter to them", async () => {
        const latest = anonSummary().series[0];

        await renderView(
            anonSummary({
                earliest_record: "2026-08-01T00:00:00Z",
                series: [
                    {
                        ...latest,
                        start: "2026-08-22T15:49:43Z",
                        end: "2026-09-21T15:49:43Z",
                        by_category: { cli: 100, web: 7 },
                        by_tool: {},
                        by_user: { user01: 900 },
                    },
                    latest,
                ],
            }),
            "30d",
        );

        const links = panelLinks("What changed");
        const keys = Object.keys(links);
        const mcp = keys.find((key) => key.startsWith("new MCP client type"));

        expect(keys[0]).toMatch(/^new user02 person/);
        expect(mcp && links[mcp]).toBe("/usage?window=30d&category=mcp");
        expect(keys.some((key) => key.startsWith("went quiet Web"))).toBe(true);
        expect(
            section("What changed").querySelector("ul")?.children,
        ).toHaveLength(8);
        expect(section("What changed").textContent).toMatch(/Show all \d+/);
    });

    it("renders an empty store", async () => {
        await renderView(
            anonSummary({
                earliest_record: "",
                days: [],
                buckets: [],
                series: [],
                errors: [],
                users: [],
                clients: [],
                endpoints: [],
                mcp_tools: [],
                mcp_agents: [],
                other_user_agents: [],
                unmatched: [],
                categories: anonSummary().categories.map((row) => ({
                    ...row,
                    requests: 0,
                })),
            }),
            "all",
        );

        expect(section("Activity").textContent).toContain(
            "No usage recorded yet.",
        );
        expect(section("What changed").textContent).toContain(
            "Pick a window to compare it with the one before.",
        );
        expect(section("People").textContent).toContain(
            "No usernames declared.",
        );
        expect(section("Endpoints").textContent).toContain(
            "No requests to known routes.",
        );
    });
});

describe("usage ranked panels", () => {
    it("shows the top five people and the rest behind Show all, each a filter link", async () => {
        await renderView(anonSummary({ filter: {} }), "30d");

        const people = section("People");
        const rows = within(people).getAllByTestId("ranked-row");

        expect(rows).toHaveLength(12);
        expect(
            within(people).getByText("Show all 12").closest("details")?.open,
        ).toBe(false);
        expect(
            within(rows[0])
                .getByRole("link", { name: "Filter to user01" })
                .getAttribute("href"),
        ).toBe("/usage?window=30d&user=user01");
        expect(people.textContent).toContain("12 self-declared");
    });

    it("merges agents by product with their versions", async () => {
        await renderView(anonSummary({ filter: {} }), "30d");

        const via = section("Via what");
        const claude = within(via).getByRole("link", {
            name: "Filter to agent claude-code",
        });

        expect(claude.getAttribute("href")).toBe(
            "/usage?window=30d&agent=claude-code",
        );
        expect(via.textContent).toContain("5 versions");
        expect(
            within(via)
                .getByRole("link", { name: "claude-code/2.1.292 (cli)" })
                .getAttribute("href"),
        ).toBe("/usage?window=30d&agent=claude-code%2F2.1.292+%28cli%29");
        expect(
            within(via)
                .getByRole("link", { name: "Filter to CLI" })
                .getAttribute("href"),
        ).toBe("/usage?window=30d&category=cli");
    });

    it("groups endpoints by first segment with fault-aware badges", async () => {
        await renderView({ ...detailedAnon(), filter: {} }, "30d");

        const endpoints = section("Endpoints");

        expect(endpoints.textContent).toContain("40 routes in 16 groups");

        const resolveRow = within(endpoints)
            .getAllByTestId("ranked-row")
            .find((row) => row.textContent?.startsWith("/resolve/…"))!;

        // /resolve is all expected misses: no fault rate, just a count.
        expect(resolveRow.textContent).toMatch(/\d+ expected/);
        expect(resolveRow.textContent).not.toContain("faults");
        expect(within(resolveRow).getByText("3 routes")).toBeTruthy();
        expect(
            within(resolveRow)
                .getByRole("link", { name: "Filter to GET /resolve/study/:id" })
                .getAttribute("href"),
        ).toBe("/usage?window=30d&method=GET&route=%2Fresolve%2Fstudy%2F%3Aid");

        const exportRow = within(endpoints)
            .getAllByTestId("ranked-row")
            .find((row) => row.textContent?.startsWith("GET /export"))!;
        const badge = within(exportRow).getByRole("link", {
            name: /faults \(fault\), see errors/,
        });

        expect(badge.getAttribute("href")).toBe(
            "#err-fault-502-upstream_impaired-GET-export-_children-_parent_kind-_parent_id",
        );
    });

    it("marks MCP tools with caller errors", async () => {
        const summary = anonSummary({ filter: {} });

        summary.mcp_tools[0] = {
            ...summary.mcp_tools[0],
            error_severity: breakdown({ caller: 2 }),
        };
        await renderView(summary, "30d");

        expect(within(section("MCP tools")).getByText("2 caller")).toBeTruthy();
    });

    it("renders bars without filter links for a server that cannot filter", async () => {
        await renderView(oldServer(anonSummary()), "30d");

        expect(within(section("People")).queryAllByRole("link")).toHaveLength(
            0,
        );
        expect(
            within(section("MCP tools")).queryAllByRole("link"),
        ).toHaveLength(0);
    });

    it("lets long caller-supplied values break anywhere", async () => {
        const long = "x".repeat(256);

        await renderView(
            anonSummary({
                users: [
                    {
                        name: long,
                        by_ip: false,
                        requests: 1,
                        by_category: { cli: 1 },
                    },
                ],
                mcp_tools: [{ value: long, requests: 1 }],
            }),
            "30d",
        );

        for (const name of ["People", "MCP tools"]) {
            expect(
                within(section(name)).getByText(long).closest(".wrap-anywhere"),
                name,
            ).not.toBeNull();
        }
    });

    it("renders rows whose values collide after U+FFFD replacement", async () => {
        const consoleError = vi
            .spyOn(console, "error")
            .mockImplementation(() => undefined);
        const nameRow = {
            name: "�",
            by_ip: false,
            requests: 1,
            by_category: { cli: 1 },
        };
        const valueRow = { value: "�", requests: 1 };

        try {
            await renderView(
                anonSummary({
                    other_user_agents: [valueRow, valueRow],
                    users: [nameRow, nameRow],
                    clients: [nameRow, nameRow],
                    unmatched: [
                        {
                            method: "GET",
                            path: "/a�",
                            requests: 1,
                            by_category: { other: 1 },
                            by_tool: {},
                            by_agent: {},
                        },
                        {
                            method: "GET",
                            path: "/a�",
                            requests: 1,
                            by_category: { other: 1 },
                            by_tool: {},
                            by_agent: {},
                        },
                    ],
                }),
                "30d",
            );

            expect(
                consoleError.mock.calls.filter((args) =>
                    String(args[0]).includes("two children with the same key"),
                ),
            ).toEqual([]);
        } finally {
            consoleError.mockRestore();
        }
    });
});

describe("usage collapsed details and deep links", () => {
    it("collapses expected outcomes and sessions", async () => {
        await renderView(anonSummary(), "30d");

        for (const [name, title] of [
            ["Expected outcomes (21 lookup misses)", "Expected outcomes"],
            [
                "Sessions and clients (292 sessions, 2 clients by IP)",
                "Sessions",
            ],
        ]) {
            const region = section(name);

            expect(region.querySelector("details")?.open, title).toBe(false);
        }

        expect(
            section("Expected outcomes (21 lookup misses)").textContent,
        ).toContain("wa mlwh info tries each kind of identifier in turn");
        expect(
            section("Sessions and clients (292 sessions, 2 clients by IP)")
                .textContent,
        ).toContain("Largest clients (top 25 of 294)");
    });

    it("ignores a malformed hash", async () => {
        window.location.hash = "#%E0%A4%A";

        try {
            await renderView(anonSummary(), "30d");
            window.dispatchEvent(new HashChangeEvent("hashchange"));
            expect(section("Needs attention")).toBeTruthy();
        } finally {
            window.location.hash = "";
        }
    });

    it("opens the disclosures holding an #err- target", async () => {
        window.location.hash =
            "#err-fault-502-upstream_impaired-GET-runs-count";

        try {
            await renderView(detailedAnon(), "30d");

            const target = document.getElementById(
                "err-fault-502-upstream_impaired-GET-runs-count",
            )!;

            expect(target.closest("details")?.open).toBe(true);

            window.location.hash = "#err-expected-GET-resolve-study-_id";
            window.dispatchEvent(new HashChangeEvent("hashchange"));

            expect(
                document
                    .getElementById("err-expected-GET-resolve-study-_id")!
                    .closest("details")?.open,
            ).toBe(true);
        } finally {
            window.location.hash = "";
        }
    });
});
