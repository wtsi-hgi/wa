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
        const studiesErrors = within(endpoints).getByRole("link", {
            name: "1 error on GET /studies, see details",
        });
        expect(studiesErrors.getAttribute("href")).toBe(
            "#usage-errors-GET-studies",
        );
        expect(studiesErrors.textContent).toBe("1");
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

        // Category rows read each period's by_category count.
        expect(rowCells(trend, "CLI").slice(0, 3)).toEqual(["CLI", "3", "1"]);
        const cliHeights = Array.from(
            within(trend)
                .getByRole("img", {
                    name: "CLI: latest 3, previous 1, 10 periods",
                })
                .querySelectorAll("[title]"),
        ).map((bar) => (bar.firstElementChild as HTMLElement).style.height);
        expect(cliHeights[9]).toBe("100%");
        expect(parseFloat(cliHeights[8])).toBeCloseTo(100 / 3, 5);
        expect(parseFloat(cliHeights[1])).toBe(0);
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

        await renderView(
            a3Summary({
                series: [
                    {
                        ...base.series[0],
                        by_endpoint: { "GET /b": 2, "GET /a": 2, "GET /z": 1 },
                    },
                    { ...base.series[1], by_endpoint: { "GET /z": 2 } },
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
            "0",
        ]);
        expect(rowCells(section("MCP agents"), "claude-code/2.1.0")).toEqual([
            "claude-code/2.1.0",
            "1",
            "0",
        ]);
        expect(
            rowCells(section("Other API user agents"), "curl/8.5.0"),
        ).toEqual(["curl/8.5.0", "2", "2"]);

        const days = section("Requests per day (UTC)");
        expect(rowCells(days, "2026-10-09")).toEqual(["2026-10-09", "0", ""]);
        expect(rowCells(days, "2026-10-10")).toEqual([
            "2026-10-10",
            "6",
            "CLI 3, MCP 1, Other API 2",
        ]);
    });

    it("names each unmatched path row by its path, not its method", async () => {
        await renderView(a3Summary(), "24h");

        const unmatched = section("Unmatched paths");
        expect(
            within(unmatched)
                .getAllByRole("rowheader")
                .map((cell) => cell.textContent),
        ).toEqual(["/bogus"]);
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

    it("writes counts in joined map cells without thousands separators", async () => {
        await renderView(
            a3Summary({
                unmatched: [
                    {
                        method: "GET",
                        path: "/bogus",
                        requests: 1239,
                        by_category: { web: 1234, cli: 5 },
                        by_tool: { mlwh_bogus: 1234, mlwh_other: 5 },
                        by_agent: { "claude-code/2.1.0": 1234, curl: 5 },
                    },
                ],
                users: [
                    {
                        name: "carol",
                        by_ip: false,
                        requests: 1239,
                        by_category: { web: 1234, cli: 5 },
                    },
                ],
            }),
            "24h",
        );

        expect(rowCells(section("Unmatched paths"), "GET")).toEqual([
            "GET",
            "/bogus",
            "1,239",
            "CLI 5, Web 1234",
            "mlwh_bogus 1234, mlwh_other 5",
            "claude-code/2.1.0 1234, curl 5",
        ]);
        expect(rowCells(section("Top users (self-declared)"), "carol")).toEqual(
            ["carol", "1,239", "CLI 5, Web 1234"],
        );
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
            "-",
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

    it("lets long caller-supplied values break anywhere, but not routes", async () => {
        const long = (prefix: string, n: number) =>
            prefix + "x".repeat(n - prefix.length);
        const path = long("/", 256);
        const userAgent = long("other/", 512);
        const tool = long("tool_", 128);
        const agent = long("agent/", 256);
        const user = long("user", 128);
        const client = long("run-", 128);

        await renderView(
            a3Summary({
                mcp_tools: [{ value: tool, requests: 1 }],
                mcp_agents: [{ value: agent, requests: 1 }],
                other_user_agents: [{ value: userAgent, requests: 1 }],
                unmatched: [
                    {
                        method: "GET",
                        path,
                        requests: 1,
                        by_category: { mcp: 1 },
                        by_tool: { [tool]: 1 },
                        by_agent: { [agent]: 1 },
                    },
                ],
                users: [
                    {
                        name: user,
                        by_ip: false,
                        requests: 1,
                        by_category: { cli: 1 },
                    },
                ],
                clients: [
                    {
                        name: client,
                        by_ip: false,
                        requests: 1,
                        by_category: { cli: 1 },
                    },
                ],
            }),
            "24h",
        );

        // jsdom has no layout, so this checks the overflow-wrap contract
        // that lets a cell's min-content width shrink below a long unbroken
        // value: the text's nearest wrap class must be wrap-anywhere.
        const wrapOf = (region: string, text: string) => {
            let element: HTMLElement | null = within(section(region)).getByText(
                text,
            );

            while (element) {
                const wrap = Array.from(element.classList).find((name) =>
                    /^(wrap-|break-words$|break-all$)/.test(name),
                );

                if (wrap) {
                    return wrap;
                }

                element = element.parentElement;
            }

            return undefined;
        };

        for (const [region, text] of [
            ["MCP tools", tool],
            ["MCP agents", agent],
            ["Other API user agents", userAgent],
            ["Unmatched paths", path],
            ["Unmatched paths", `${tool} 1`],
            ["Unmatched paths", `${agent} 1`],
            ["Top users (self-declared)", user],
            ["Top clients", client],
        ]) {
            expect(wrapOf(region, text), `${region}: ${text}`).toBe(
                "wrap-anywhere",
            );
        }

        expect(wrapOf("Endpoints", "GET /studies")).toBe("break-words");
    });

    it("renders rows whose values collide after U+FFFD replacement", async () => {
        // Go's JSON encoder writes invalid UTF-8 as U+FFFD, so distinct
        // stored values such as /a%FF and /a%FE arrive identical.
        const consoleError = vi
            .spyOn(console, "error")
            .mockImplementation(() => undefined);
        const replaced = "/a\uFFFD";
        const unmatchedRow = {
            method: "GET",
            path: replaced,
            requests: 1,
            by_category: { cli: 1 },
            by_tool: {},
            by_agent: {},
        };
        const nameRow = {
            name: "\uFFFD",
            by_ip: false,
            requests: 1,
            by_category: { cli: 1 },
        };
        const valueRow = { value: "\uFFFD", requests: 1 };

        try {
            await renderView(
                a3Summary({
                    mcp_tools: [valueRow, valueRow],
                    mcp_agents: [valueRow, valueRow],
                    other_user_agents: [valueRow, valueRow],
                    unmatched: [unmatchedRow, unmatchedRow],
                    users: [nameRow, nameRow],
                    clients: [nameRow, nameRow],
                }),
                "24h",
            );

            for (const [region, text] of [
                ["MCP tools", "\uFFFD"],
                ["MCP agents", "\uFFFD"],
                ["Other API user agents", "\uFFFD"],
                ["Unmatched paths", replaced],
                ["Top users (self-declared)", "\uFFFD"],
                ["Top clients", "\uFFFD"],
            ]) {
                expect(
                    within(section(region)).getAllByText(text),
                    region,
                ).toHaveLength(2);
            }

            const keyErrors = consoleError.mock.calls.filter((args) =>
                String(args[0]).includes("two children with the same key"),
            );
            expect(keyErrors).toEqual([]);
        } finally {
            consoleError.mockRestore();
        }
    });

    it("shows the note on every ok render", async () => {
        for (const window of ["24h", "7d", "30d", "all"] as const) {
            await renderView(a3Summary({ window }), window);

            expect(screen.getByText(noteText), window).toBeTruthy();
            cleanup();
        }
    });
});

type UsageErrorGroup = NonNullable<UsageSummary["errors"]>[number];

function errorGroup(overrides: Partial<UsageErrorGroup>): UsageErrorGroup {
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

// errorGuide returns the Errors section's per-code "What to do" block.
function errorGuide(): HTMLElement {
    return within(section("Errors")).getByRole("group", { name: "What to do" });
}

// guideAction returns the guide's action text for the code label.
function guideAction(label: string): string {
    const term = within(errorGuide())
        .getAllByRole("term")
        .find((candidate) => candidate.textContent === label);
    const definition = term?.nextElementSibling;

    if (!definition) {
        throw new Error(`no action for ${label}`);
    }

    return definition.textContent ?? "";
}

// errorEntry returns the Errors section's entry for the endpoint label and
// the status-and-code label.
function errorEntry(endpoint: string, code: string): HTMLElement {
    const group = within(section("Errors")).getByRole("group", {
        name: endpoint,
    });

    return within(group).getByRole("article", { name: code });
}

describe("usage errors section", () => {
    it("groups the A3 fixture's errors by endpoint, then status and code", async () => {
        await renderView(a3Summary(), "24h");

        const errors = section("Errors");
        expect(
            within(errors)
                .getAllByRole("heading", { level: 3 })
                .map((heading) => heading.textContent),
        ).toEqual([
            "What to do",
            "GET /classify/:id",
            "GET /studies",
            "GET (no matching route)",
        ]);

        expect(
            within(errorGuide())
                .getAllByRole("term")
                .map((term) => term.textContent),
        ).toEqual([
            "not_found",
            "unknown (recorded before error details were captured)",
            "no_route",
        ]);
        expect(guideAction("not_found")).toContain("guessing");
        expect(within(errors).getAllByText(/guessing/)).toHaveLength(1);

        const notFound = errorEntry("GET /classify/:id", "404 not_found");
        expect(notFound.textContent).toContain(
            "The identifier is not known to the MLWH.",
        );
        expect(notFound.textContent).not.toContain("guessing");
        expect(
            within(notFound)
                .getByRole("link", { name: "What to do about not_found" })
                .getAttribute("href"),
        ).toBe("#usage-error-help-not_found");
        expect(
            within(
                within(notFound).getByRole("list", { name: "Top messages" }),
            ).getByText("mlwh: not found: XYZ"),
        ).toBeTruthy();
        expect(within(notFound).getByText("/classify/XYZ")).toBeTruthy();
        expect(notFound.textContent).toContain("bob");

        const noRoute = errorEntry("GET (no matching route)", "404 no_route");
        expect(noRoute.textContent).toContain("No endpoint matches");
        expect(within(noRoute).getByText("/bogus")).toBeTruthy();
    });

    it("labels rows recorded before error details as unknown", async () => {
        await renderView(a3Summary(), "24h");

        const legacy = errorEntry(
            "GET /studies",
            "500 unknown (recorded before error details were captured)",
        );
        expect(legacy.textContent).toContain(
            "recorded before the server stored error codes and messages",
        );
    });

    it("explains every known code once, with its meaning in each entry and its action in the guide", async () => {
        const codes = [
            "not_found",
            "ambiguous",
            "unsupported_identifier",
            "bad_request",
            "cache_never_synced",
            "upstream_impaired",
            "no_route",
            "payload_too_large",
            "internal_error",
            "uncoded",
        ];

        await renderView(
            a3Summary({
                errors: codes.map((code, index) =>
                    errorGroup({ status: 400 + index, error_code: code }),
                ),
            }),
            "24h",
        );

        for (const [index, code] of codes.entries()) {
            const entry = errorEntry(
                "GET /classify/:id",
                `${400 + index} ${code}`,
            );

            expect(entry.textContent, code).not.toContain(
                "No explanation for this code",
            );
            expect(guideAction(code), code).not.toBe("");
            expect(guideAction(code), code).not.toContain(
                "See that entry's messages and examples",
            );
        }
    });

    it("points an unknown code's guide entry back at its messages and examples", async () => {
        await renderView(
            a3Summary({
                errors: [errorGroup({ status: 418, error_code: "teapot" })],
            }),
            "24h",
        );

        expect(
            errorEntry("GET /classify/:id", "418 teapot").textContent,
        ).toContain("No explanation for this code.");
        expect(guideAction("teapot")).toBe(
            "See that entry's messages and examples.",
        );
    });

    it("shows timeout and canceled kinds with their specific advice", async () => {
        await renderView(
            a3Summary({
                errors: [
                    errorGroup({
                        status: 502,
                        error_code: "upstream_impaired",
                        count: 3,
                        error_kinds: { timeout: 2, canceled: 1 },
                    }),
                ],
            }),
            "24h",
        );

        const entry = errorEntry("GET /classify/:id", "502 upstream_impaired");
        expect(entry.textContent).toContain("timeout 2");
        expect(entry.textContent).toContain("canceled 1");
        expect(guideAction("timeout")).toContain("slow query");
        expect(guideAction("canceled")).toContain("client went away");

        for (const [kind, count] of [
            ["canceled", 1],
            ["timeout", 2],
        ] as const) {
            const link = within(entry).getByRole("link", {
                name: `What to do about ${kind} (${count})`,
            });
            const target = document.getElementById(
                link.getAttribute("href")?.slice(1) ?? "",
            );

            expect(link.textContent).toBe(`${kind} ${count}`);
            expect(target?.querySelector("dt")?.textContent).toBe(kind);
            expect(errorGuide().contains(target)).toBe(true);
        }
    });

    it("leaves causes without guide entries unlinked", async () => {
        await renderView(
            a3Summary({
                errors: [
                    errorGroup({
                        status: 502,
                        error_code: "upstream_impaired",
                        error_kinds: { constructor: 1 },
                    }),
                ],
            }),
            "24h",
        );

        const entry = errorEntry("GET /classify/:id", "502 upstream_impaired");
        expect(entry.textContent).toContain("Causes: constructor 1");
        expect(
            within(entry).queryByRole("link", { name: /constructor/ }),
        ).toBeNull();
    });

    it("says how many errors the shown groups cover when the server capped them", async () => {
        const errors = [
            errorGroup({ count: 50 }),
            errorGroup({ status: 400, error_code: "bad_request", count: 3 }),
        ];

        await renderView(
            a3Summary({
                totals: { ...a3Summary().totals, errors: 56 },
                errors,
            }),
            "24h",
        );
        expect(
            within(section("Errors")).getByText(
                "Showing the 2 largest groups (53 of 56 errors).",
            ),
        ).toBeTruthy();

        cleanup();
        await renderView(
            a3Summary({
                totals: { ...a3Summary().totals, errors: 53 },
                errors,
            }),
            "24h",
        );
        expect(
            within(section("Errors")).queryByText(/largest groups/),
        ).toBeNull();
    });

    it("states shared advice once when several entries share a code or cause", async () => {
        await renderView(
            a3Summary({
                errors: [
                    errorGroup({
                        status: 502,
                        error_code: "upstream_impaired",
                        error_kinds: { timeout: 1 },
                    }),
                    errorGroup({
                        route: "/studies",
                        status: 502,
                        error_code: "upstream_impaired",
                        error_kinds: { timeout: 1 },
                    }),
                ],
            }),
            "24h",
        );

        const errors = section("Errors");
        expect(
            within(errors).getAllByText(/cache database's health/),
        ).toHaveLength(1);
        expect(within(errors).getAllByText(/slow query/)).toHaveLength(1);
        expect(
            within(errors).getAllByText(
                "The server could not read the MLWH cache database or another upstream service.",
            ),
        ).toHaveLength(2);
        expect(
            within(errorGuide())
                .getAllByRole("term")
                .map((term) => term.textContent),
        ).toEqual(["upstream_impaired", "timeout"]);
    });

    it("lists top messages, MCP callers, and examples with their query", async () => {
        await renderView(
            a3Summary({
                errors: [
                    errorGroup({
                        count: 3,
                        messages: [
                            { message: "no A", count: 2 },
                            { message: "no B", count: 1 },
                        ],
                        by_tool: { mlwh_classify_identifier: 2 },
                        by_agent: { "claude-code/2.1.0": 2 },
                        examples: [
                            {
                                ts: "2026-10-10T11:00:00Z",
                                path: "/classify/A",
                                query: "x=1",
                                category: "mcp",
                                mcp_tool: "mlwh_classify_identifier",
                                mcp_agent: "claude-code/2.1.0",
                                client_id: "sess-1",
                                username: "",
                                message: "no A",
                                error_kind: "",
                            },
                        ],
                    }),
                ],
            }),
            "24h",
        );

        const entry = errorEntry("GET /classify/:id", "404 not_found");
        const messages = within(entry).getByRole("list", {
            name: "Top messages",
        });
        expect(
            within(messages)
                .getAllByRole("listitem")
                .map((item) => item.textContent),
        ).toEqual(["no A2", "no B1"]);
        expect(entry.textContent).toContain("mlwh_classify_identifier 2");
        expect(entry.textContent).toContain("claude-code/2.1.0 2");
        expect(within(entry).getByText("/classify/A?x=1")).toBeTruthy();
        expect(
            within(entry).getByText("Messages and examples (1 example)"),
        ).toBeTruthy();
    });

    it("shows an empty state and a note for servers without error details", async () => {
        await renderView(a3Summary({ errors: [] }), "24h");
        expect(
            within(section("Errors")).getByText("No errors in this window."),
        ).toBeTruthy();

        cleanup();
        const { errors: _errors, ...withoutErrors } = a3Summary();
        await renderView(withoutErrors, "24h");
        expect(
            within(section("Errors")).getByText(
                /does not report error details/,
            ),
        ).toBeTruthy();
    });

    it("adds error counts to the MCP tool and agent tables", async () => {
        await renderView(
            a3Summary({
                mcp_tools: [
                    {
                        value: "mlwh_classify_identifier",
                        requests: 5,
                        errors: 2,
                    },
                ],
            }),
            "24h",
        );

        expect(
            rowCells(section("MCP tools"), "mlwh_classify_identifier"),
        ).toEqual(["mlwh_classify_identifier", "5", "2"]);
    });
});
