/**
 * @vitest-environment jsdom
 */

import { createElement } from "react";
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
    UsageSummaryState,
    UsageUnavailableReason,
} from "@/app/(results)/usage/actions";
import {
    usageSummarySchema,
    type UsageSummary,
    type UsageWindow,
} from "@/lib/contracts";

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

async function renderView(summary: UsageSummary, window: UsageWindow) {
    const { UsageAdminView } = await import("@/components/usage-admin-view");

    return render(createElement(UsageAdminView, { summary, window }));
}

function section(name: string): HTMLElement {
    return screen.getByRole("region", { name });
}

// rowCells returns the text of each cell in the row of table whose first
// cell is exactly label.
function rowCells(table: HTMLElement, label: string): string[] {
    const row = within(table)
        .getAllByRole("row")
        .find((candidate) => {
            const first = candidate.querySelector("th, td");

            return first?.textContent === label;
        });

    if (!row) {
        throw new Error(`no row ${label}`);
    }

    return Array.from(row.querySelectorAll("th, td")).map(
        (cell) => cell.textContent ?? "",
    );
}

function statValue(label: string): string {
    const term = screen.getByText(label, { selector: "dt" });

    return term.nextElementSibling?.textContent ?? "";
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
            ).toHaveBeenCalledExactlyOnceWith(expected);
        }
    });

    it("handles absent searchParams", async () => {
        const { default: UsageAdminPage } =
            await import("@/app/(results)/usage/page");

        render(await UsageAdminPage({}));

        expect(actionMocks.getUsageSummaryAction).toHaveBeenCalledWith("7d");
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

describe("E4 usage admin view", () => {
    it("links each window preset and marks the current one", async () => {
        await renderView(a3Summary(), "24h");

        const nav = screen.getByRole("navigation", { name: "Window" });
        const links = within(nav).getAllByRole("link");

        expect(
            links.map((link) => [
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

    it("marks Last 7 days current for the 7d window", async () => {
        await renderView(a3Summary({ window: "7d" }), "7d");

        expect(
            screen
                .getByRole("link", { name: "Last 7 days" })
                .getAttribute("aria-current"),
        ).toBe("page");
        expect(
            screen
                .getByRole("link", { name: "Last 24 hours" })
                .getAttribute("aria-current"),
        ).toBeNull();
    });

    it("renders the A3 fixture's stats, categories, and endpoints", async () => {
        await renderView(a3Summary(), "24h");

        expect(
            screen.getByRole("heading", { level: 1, name: "MLWH usage" }),
        ).toBeTruthy();
        expect(statValue("Requests")).toBe("6");
        expect(statValue("Errors")).toBe("3 (50.0%)");
        expect(statValue("Sessions")).toBe("3");
        expect(statValue("Clients by IP")).toBe("1");
        expect(statValue("Users (self-declared)")).toBe("2");

        const byCategory = section("By category");
        expect(
            within(byCategory)
                .getAllByRole("columnheader")
                .map((cell) => cell.textContent),
        ).toEqual([
            "Category",
            "Requests",
            "Errors",
            "Error rate",
            "Sessions",
            "Clients by IP",
            "Users",
            "Requests/session (median)",
            "Requests/session (max)",
        ]);
        expect(within(byCategory).getAllByRole("row")).toHaveLength(7);
        expect(rowCells(byCategory, "CLI")).toEqual([
            "CLI",
            "3",
            "1",
            "33.3%",
            "2",
            "0",
            "2",
            "1",
            "2",
        ]);
        expect(rowCells(byCategory, "Results server")[1]).toBe("0");
        expect(rowCells(byCategory, "mlwhdiff server")[1]).toBe("0");
        expect(rowCells(byCategory, "Web")[1]).toBe("0");
        expect(rowCells(byCategory, "Other API")).toEqual([
            "Other API",
            "2",
            "2",
            "100.0%",
            "0",
            "1",
            "0",
            "0",
            "0",
        ]);

        const endpoints = section("Endpoints");
        expect(
            within(endpoints)
                .getAllByRole("columnheader")
                .map((cell) => cell.textContent),
        ).toEqual([
            "Endpoint",
            "Requests",
            "Errors",
            "Error rate",
            "Median latency (ms)",
            "p95 latency (ms)",
            "Categories",
        ]);
        expect(rowCells(endpoints, "GET /studies")).toEqual([
            "GET /studies",
            "4",
            "1",
            "25.0%",
            "20.0",
            "40.0",
            "CLI 2, MCP 1, Other API 1",
        ]);
        expect(rowCells(endpoints, "GET /classify/:id")).toEqual([
            "GET /classify/:id",
            "1",
            "1",
            "100.0%",
            "5.0",
            "5.0",
            "CLI 1",
        ]);

        expect(
            screen.getByRole("heading", {
                level: 2,
                name: "Trend (24-hour periods)",
            }),
        ).toBeTruthy();
    });

    it("renders the Trend rows with latest, previous, and one bar per period", async () => {
        await renderView(a3Summary(), "24h");

        const trend = section("Trend (24-hour periods)");
        const labels = within(trend)
            .getAllByRole("row")
            .slice(1)
            .map((row) => row.querySelector("th, td")?.textContent);

        expect(labels).toEqual([
            "All requests",
            "CLI",
            "MCP",
            "Web",
            "Results server",
            "mlwhdiff server",
            "Other API",
            "GET /studies",
            "GET /classify/:id",
        ]);
        expect(rowCells(trend, "All requests").slice(0, 3)).toEqual([
            "All requests",
            "6",
            "1",
        ]);
        expect(rowCells(trend, "Web").slice(0, 3)).toEqual(["Web", "0", "0"]);

        expect(
            within(trend).getByRole("columnheader", {
                name: "10 periods, oldest first",
            }),
        ).toBeTruthy();

        const strip = within(trend).getByRole("img", {
            name: "All requests: latest 6, previous 1, 10 periods",
        });
        const bars = Array.from(strip.querySelectorAll("[title]"));

        expect(bars).toHaveLength(10);
        expect(bars[9].getAttribute("title")).toBe(
            "2026-10-09T12:00:00Z to 2026-10-10T12:00:00Z: 6",
        );
        expect(bars[0].getAttribute("title")).toBe(
            "2026-09-30T12:00:00Z to 2026-10-01T12:00:00Z: 1",
        );

        // Each bar's height is proportional to the row's maximum (6).
        const heights = bars.map(
            (bar) => (bar.firstElementChild as HTMLElement).style.height,
        );
        expect(heights[9]).toBe("100%");
        expect(parseFloat(heights[8])).toBeCloseTo(100 / 6, 5);
        expect(parseFloat(heights[1])).toBe(0);

        // A row with a smaller maximum is scaled to its own maximum (1), not
        // to the All requests maximum.
        const classifyBars = Array.from(
            within(trend)
                .getByRole("img", {
                    name: "GET /classify/:id: latest 1, previous 0, 10 periods",
                })
                .querySelectorAll("[title]"),
        );
        expect(
            (classifyBars[9].firstElementChild as HTMLElement).style.height,
        ).toBe("100%");
        expect(
            parseFloat(
                (classifyBars[8].firstElementChild as HTMLElement).style.height,
            ),
        ).toBe(0);
    });

    it("formats Trend bar title counts like the table cells", async () => {
        const base = a3Summary();

        await renderView(
            a3Summary({ series: [{ ...base.series[9], requests: 1234 }] }),
            "24h",
        );

        const trend = section("Trend (24-hour periods)");
        expect(rowCells(trend, "All requests")[1]).toBe("1,234");

        const bar = within(trend)
            .getByRole("img", {
                name: "All requests: latest 1,234, previous -, 1 period",
            })
            .querySelector("[title]");
        expect(bar?.getAttribute("title")).toBe(
            "2026-10-09T12:00:00Z to 2026-10-10T12:00:00Z: 1,234",
        );
    });

    it("orders Trend endpoint rows by series total, then key", async () => {
        const base = a3Summary();
        const period = base.series[0];

        await renderView(
            a3Summary({
                series: [
                    {
                        ...period,
                        by_endpoint: { "GET /b": 2, "GET /a": 2, "GET /z": 1 },
                    },
                    { ...period, by_endpoint: { "GET /z": 2 } },
                ],
            }),
            "24h",
        );

        const labels = within(section("Trend (24-hour periods)"))
            .getAllByRole("row")
            .slice(8)
            .map((row) => row.querySelector("th, td")?.textContent);

        expect(labels).toEqual(["GET /z", "GET /a", "GET /b"]);
    });

    it("shows - for Previous with a single period", async () => {
        const base = a3Summary();

        await renderView(
            a3Summary({ window: "30d", series: [base.series[9]] }),
            "30d",
        );

        const trend = section("Trend (30-day periods)");
        expect(rowCells(trend, "All requests").slice(0, 3)).toEqual([
            "All requests",
            "6",
            "-",
        ]);
        expect(
            within(trend).getByRole("img", {
                name: "All requests: latest 6, previous -, 1 period",
            }),
        ).toBeTruthy();
        expect(
            within(trend).getByRole("columnheader", {
                name: "1 period, oldest first",
            }),
        ).toBeTruthy();
    });

    it("names the Trend period length after the window", async () => {
        await renderView(a3Summary({ window: "7d" }), "7d");

        expect(
            screen.getByRole("heading", { name: "Trend (7-day periods)" }),
        ).toBeTruthy();
    });

    it("renders top clients, unmatched paths, users, and days", async () => {
        await renderView(a3Summary(), "24h");

        const clients = section("Top clients");
        expect(rowCells(clients, "10.0.0.9 (by IP)")).toEqual([
            "10.0.0.9 (by IP)",
            "2",
            "Other API 2",
        ]);
        expect(rowCells(clients, "run-a")).toEqual(["run-a", "2", "CLI 2"]);

        const unmatched = section("Unmatched paths");
        expect(rowCells(unmatched, "GET")).toEqual([
            "GET",
            "/bogus",
            "1",
            "Other API 1",
            "",
            "",
        ]);

        expect(rowCells(section("Top users (self-declared)"), "alice")).toEqual(
            ["alice", "2", "CLI 2"],
        );
        expect(rowCells(section("MCP tools"), "mlwh_list_studies")).toEqual([
            "mlwh_list_studies",
            "1",
        ]);
        expect(rowCells(section("MCP agents"), "claude-code/2.1.0")).toEqual([
            "claude-code/2.1.0",
            "1",
        ]);
        expect(
            rowCells(section("Other API user agents"), "curl/8.5.0"),
        ).toEqual(["curl/8.5.0", "2"]);

        const days = section("Requests per day (UTC)");
        expect(rowCells(days, "2026-10-09")).toEqual(["2026-10-09", "0", ""]);
        expect(rowCells(days, "2026-10-10")).toEqual([
            "2026-10-10",
            "6",
            "CLI 3, MCP 1, Other API 2",
        ]);
    });

    it("renders map cells for unmatched tools and agents", async () => {
        await renderView(
            a3Summary({
                unmatched: [
                    {
                        method: "GET",
                        path: "/bogus",
                        requests: 2,
                        by_category: { other: 1, mcp: 1 },
                        by_tool: { mlwh_bogus: 1 },
                        by_agent: { "claude-code/2.1.0": 1 },
                    },
                ],
            }),
            "24h",
        );

        expect(rowCells(section("Unmatched paths"), "GET")).toEqual([
            "GET",
            "/bogus",
            "2",
            "MCP 1, Other API 1",
            "mlwh_bogus 1",
            "claude-code/2.1.0 1",
        ]);
    });

    it("lists categories cells in category order, not label order", async () => {
        await renderView(
            a3Summary({
                users: [
                    {
                        name: "carol",
                        by_ip: false,
                        requests: 6,
                        by_category: {
                            other: 1,
                            web: 1,
                            mlwhdiff_server: 1,
                            results_server: 1,
                            mcp: 1,
                            cli: 1,
                        },
                    },
                ],
            }),
            "24h",
        );

        expect(rowCells(section("Top users (self-declared)"), "carol")).toEqual(
            [
                "carol",
                "6",
                "CLI 1, MCP 1, Web 1, Results server 1, mlwhdiff server 1, Other API 1",
            ],
        );
    });

    it('renders an empty user agent as "(none)"', async () => {
        await renderView(
            a3Summary({ other_user_agents: [{ value: "", requests: 4 }] }),
            "24h",
        );

        expect(rowCells(section("Other API user agents"), "(none)")).toEqual([
            "(none)",
            "4",
        ]);
    });

    it("omits Trend for all and explains an empty series otherwise", async () => {
        await renderView(a3Summary({ window: "all", series: [] }), "all");

        expect(screen.queryByRole("heading", { name: /^Trend/ })).toBeNull();
        expect(screen.queryByText("No usage recorded yet.")).toBeNull();

        cleanup();
        await renderView(a3Summary({ window: "7d", series: [] }), "7d");

        expect(
            within(section("Trend (7-day periods)")).getByText(
                "No usage recorded yet.",
            ),
        ).toBeTruthy();
    });

    it("reports dropped events only when there are some", async () => {
        const dropped =
            "3 requests were not recorded because the usage queue was full.";

        await renderView(a3Summary({ dropped_events: 3 }), "24h");
        expect(screen.getByText(dropped, { exact: false })).toBeTruthy();

        cleanup();
        await renderView(a3Summary({ dropped_events: 0 }), "24h");
        expect(
            screen.queryByText(/were not recorded because the usage queue/),
        ).toBeNull();
    });

    it('shows "None." for each empty list section', async () => {
        await renderView(
            a3Summary({
                endpoints: [],
                mcp_tools: [],
                mcp_agents: [],
                other_user_agents: [],
                unmatched: [],
                users: [],
                clients: [],
                days: [],
            }),
            "24h",
        );

        for (const name of [
            "Endpoints",
            "MCP tools",
            "MCP agents",
            "Other API user agents",
            "Unmatched paths",
            "Top users (self-declared)",
            "Top clients",
            "Requests per day (UTC)",
        ]) {
            const region = section(name);

            expect(within(region).getByText("None."), name).toBeTruthy();
            expect(within(region).queryByRole("table"), name).toBeNull();
        }

        expect(within(section("By category")).getAllByRole("row")).toHaveLength(
            7,
        );
    });

    it("shows the note on every ok render", async () => {
        for (const window of ["24h", "7d", "30d", "all"] as const) {
            await renderView(a3Summary({ window }), window);

            expect(screen.getByText(noteText), window).toBeTruthy();
            cleanup();
        }
    });
});
