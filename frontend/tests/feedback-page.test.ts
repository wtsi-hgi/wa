/**
 * @vitest-environment jsdom
 */

import {
    createElement,
    startTransition,
    Suspense,
    use,
    useEffect,
    useState,
} from "react";
import {
    act,
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
    within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
    FeedbackListState,
    FeedbackMutationState,
    FeedbackUnavailableReason,
} from "@/app/(results)/feedback/actions";
import type {
    FeedbackCategory,
    FeedbackPage,
    FeedbackReport,
} from "@/lib/contracts";

const actionMocks = vi.hoisted(() => ({
    deleteFeedbackAction: vi.fn(),
    listFeedbackAction: vi.fn(),
    setFeedbackAcknowledgedAction: vi.fn(),
}));
const navigationMocks = vi.hoisted(() => ({
    push: vi.fn(),
    refresh: vi.fn(),
}));
const toastMocks = vi.hoisted(() => ({
    error: vi.fn(),
    success: vi.fn(),
}));

vi.mock("@/app/(results)/feedback/actions", () => actionMocks);
vi.mock("next/navigation", () => ({
    usePathname: () => "/feedback",
    useRouter: () => ({
        push: navigationMocks.push,
        refresh: navigationMocks.refresh,
    }),
    useSearchParams: () => new URLSearchParams(),
}));
vi.mock("sonner", () => ({ toast: toastMocks }));

type Show = "unacknowledged" | "all";
type ViewProps = {
    category: FeedbackCategory | null;
    offset: number;
    page: FeedbackPage;
    show: Show;
};

function buildReport(overrides: Partial<FeedbackReport> = {}): FeedbackReport {
    return {
        id: 1,
        created_at: "2026-10-01T12:00:00Z",
        category: "no_endpoint",
        description: "No endpoint lists sample consent.",
        user_request: "Which samples have withdrawn consent?",
        tools_tried: ["mlwh_search_samples", "mlwh_call_endpoint"],
        mcp_server_version: "0.4.0",
        wa_api_version: "1.9.0",
        transport: "stdio",
        client_name: "claude-code",
        client_version: "2.1.0",
        client_user_agent: "",
        remote_addr: "10.0.0.5:51234",
        acknowledged: false,
        acknowledged_at: "",
        ...overrides,
    };
}

function buildPage(overrides: Partial<FeedbackPage> = {}): FeedbackPage {
    return {
        items: [buildReport()],
        total: 1,
        next_offset: -1,
        ...overrides,
    };
}

async function renderPage(searchParams: Record<string, string | string[]>) {
    const { default: FeedbackAdminPage } =
        await import("@/app/(results)/feedback/page");
    const element = await FeedbackAdminPage({
        searchParams: Promise.resolve(searchParams),
    });

    return render(element);
}

async function renderView(overrides: Partial<ViewProps> = {}) {
    const { FeedbackAdminView } =
        await import("@/components/feedback-admin-view");
    const props: ViewProps = {
        category: null,
        offset: 0,
        page: buildPage(),
        show: "unacknowledged",
        ...overrides,
    };

    return render(createElement(FeedbackAdminView, props));
}

function articleFor(id: number): HTMLElement {
    return screen.getByRole("article", { name: `Feedback #${id}` });
}

function linkHref(name: string): string | null {
    return screen.getByRole("link", { name }).getAttribute("href");
}

function deferred<T>() {
    let resolve: (value: T) => void = () => {};
    const promise = new Promise<T>((settle) => {
        resolve = settle;
    });

    return { promise, resolve };
}

beforeEach(() => {
    actionMocks.listFeedbackAction.mockResolvedValue({
        status: "ok",
        page: buildPage({ items: [], total: 0 }),
    } satisfies FeedbackListState);
    actionMocks.setFeedbackAcknowledgedAction.mockResolvedValue({
        status: "ok",
    });
    actionMocks.deleteFeedbackAction.mockResolvedValue({ status: "ok" });
});

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.restoreAllMocks();
});

describe("E4 feedback page", () => {
    it("sanitises searchParams before listing", async () => {
        const cases: Array<{
            expected: {
                category: FeedbackCategory | null;
                offset: number;
                show: Show;
            };
            params: Record<string, string | string[]>;
        }> = [
            {
                params: {},
                expected: {
                    show: "unacknowledged",
                    category: null,
                    offset: 0,
                },
            },
            {
                params: { show: "all", category: "bogus", offset: "-3" },
                expected: { show: "all", category: null, offset: 0 },
            },
            {
                params: { show: "all", category: "other", offset: "50" },
                expected: { show: "all", category: "other", offset: 50 },
            },
            {
                params: {
                    show: "ALL",
                    category: "../x",
                    offset: "1.5",
                },
                expected: {
                    show: "unacknowledged",
                    category: null,
                    offset: 0,
                },
            },
            {
                params: { show: "unacknowledged", offset: "1e3" },
                expected: {
                    show: "unacknowledged",
                    category: null,
                    offset: 0,
                },
            },
            {
                params: { offset: "9007199254740993" },
                expected: {
                    show: "unacknowledged",
                    category: null,
                    offset: 0,
                },
            },
            {
                params: { category: "could_not_answer", offset: "abc" },
                expected: {
                    show: "unacknowledged",
                    category: "could_not_answer",
                    offset: 0,
                },
            },
        ];

        for (const { params, expected } of cases) {
            actionMocks.listFeedbackAction.mockClear();
            await renderPage(params);
            cleanup();

            expect(
                actionMocks.listFeedbackAction,
                JSON.stringify(params),
            ).toHaveBeenCalledOnce();
            expect(
                actionMocks.listFeedbackAction,
                JSON.stringify(params),
            ).toHaveBeenCalledWith(expected);
        }
    });

    it("passes the sanitised filters to the view", async () => {
        actionMocks.listFeedbackAction.mockResolvedValue({
            status: "ok",
            page: buildPage({ next_offset: 100 }),
        } satisfies FeedbackListState);

        await renderPage({ show: "all", category: "other", offset: "50" });

        expect(
            screen.getByRole<HTMLInputElement>("checkbox", {
                name: "Show acknowledged",
            }).checked,
        ).toBe(true);
        expect(
            screen.getByRole<HTMLSelectElement>("combobox", {
                name: "Category",
            }).value,
        ).toBe("other");
        expect(linkHref("Previous")).toBe("/feedback?show=all&category=other");
        expect(linkHref("Next")).toBe(
            "/feedback?show=all&category=other&offset=100",
        );
    });

    const reasonMessages: Record<FeedbackUnavailableReason, string> = {
        no_token: "The MLWH server token is not readable by this server.",
        feedback_disabled:
            "Feedback collection is disabled on the MLWH server.",
        token_rejected: "The MLWH server rejected the admin token.",
        unsupported:
            "wa mlwh serve is too old for feedback (needs MLWH API 1.9.0).",
        backend_error: "The MLWH server could not be reached.",
    };
    const statusMessages = {
        unauthenticated: "Log in to view feedback.",
        forbidden: "You do not have access to feedback.",
        invalid_input: "Invalid feedback request.",
    } as const;
    const unavailableMessage = "Feedback is unavailable.";
    const allMessages = [
        ...Object.values(reasonMessages),
        ...Object.values(statusMessages),
        unavailableMessage,
    ];

    function expectOnlyMessages(text: string, expected: string[]) {
        for (const message of allMessages) {
            if (expected.includes(message)) {
                expect(text).toContain(message);
            } else {
                expect(text).not.toContain(message);
            }
        }
    }

    for (const [status, message] of Object.entries(statusMessages)) {
        it(`renders the ${status} message`, async () => {
            actionMocks.listFeedbackAction.mockResolvedValue({ status });

            const { container } = await renderPage({});

            expectOnlyMessages(container.textContent ?? "", [message]);
            expect(screen.queryByRole("article")).toBeNull();
        });
    }

    for (const [reason, message] of Object.entries(reasonMessages)) {
        it(`renders the unavailable ${reason} message`, async () => {
            actionMocks.listFeedbackAction.mockResolvedValue({
                status: "unavailable",
                reason,
            });

            const { container } = await renderPage({});

            expectOnlyMessages(container.textContent ?? "", [
                unavailableMessage,
                message,
            ]);
        });
    }
});

describe("E4 feedback admin view", () => {
    it("renders one article per report with acknowledge state", async () => {
        await renderView({
            page: buildPage({
                items: [
                    buildReport({
                        id: 7,
                        acknowledged: true,
                        acknowledged_at: "2026-10-02T09:00:00Z",
                    }),
                    buildReport({ id: 2, category: "agent_mistake" }),
                ],
                total: 2,
            }),
        });

        expect(
            screen.getByRole("heading", { name: "Agent feedback" }),
        ).toBeTruthy();
        expect(screen.getByText("2 reports")).toBeTruthy();

        const articles = screen.getAllByRole("article");
        expect(articles).toHaveLength(2);

        const first = within(articles[0]);
        expect(first.getByText("#7")).toBeTruthy();
        expect(
            first.getByRole("button", { name: "Unacknowledge" }),
        ).toBeTruthy();
        expect(first.queryByRole("button", { name: "Acknowledge" })).toBeNull();
        expect(first.getByText("Acknowledged")).toBeTruthy();

        const second = within(articles[1]);
        expect(second.getByText("#2")).toBeTruthy();
        expect(second.getByText("Agent mistake")).toBeTruthy();
        expect(
            second.getByRole("button", { name: "Acknowledge" }),
        ).toBeTruthy();
        expect(
            second.queryByRole("button", { name: "Unacknowledge" }),
        ).toBeNull();
        expect(second.queryByText("Acknowledged")).toBeNull();
    });

    it("counts the total reports, not just this page", async () => {
        await renderView({
            page: buildPage({ items: [buildReport()], total: 120 }),
        });

        expect(screen.getByText("120 reports")).toBeTruthy();
    });

    it("uses the singular for one report", async () => {
        await renderView({ page: buildPage({ total: 1 }) });

        expect(screen.getByText("1 report")).toBeTruthy();
    });

    it("disables a report's buttons while its mutation is pending", async () => {
        let resolveAction: (state: { status: "ok" }) => void = () => {};
        actionMocks.setFeedbackAcknowledgedAction.mockReturnValue(
            new Promise((resolve) => {
                resolveAction = resolve;
            }),
        );

        await renderView({
            page: buildPage({
                items: [buildReport({ id: 1 }), buildReport({ id: 2 })],
                total: 2,
            }),
        });

        const pending = within(articleFor(2));
        fireEvent.click(pending.getByRole("button", { name: "Acknowledge" }));

        await waitFor(() => {
            expect(
                pending.getByRole<HTMLButtonElement>("button", {
                    name: "Acknowledge",
                }).disabled,
            ).toBe(true);
        });
        expect(
            pending.getByRole<HTMLButtonElement>("button", { name: "Delete" })
                .disabled,
        ).toBe(true);
        expect(
            within(articleFor(1)).getByRole<HTMLButtonElement>("button", {
                name: "Acknowledge",
            }).disabled,
        ).toBe(false);

        resolveAction({ status: "ok" });

        await waitFor(() => {
            expect(navigationMocks.refresh).toHaveBeenCalledOnce();
        });
        expect(
            pending.getByRole<HTMLButtonElement>("button", {
                name: "Acknowledge",
            }).disabled,
        ).toBe(false);
    });

    it("shows report details and metadata", async () => {
        await renderView({
            page: buildPage({
                items: [
                    buildReport({
                        id: 4,
                        category: "user_unhappy",
                        created_at: "2026-10-01T12:00:00Z",
                    }),
                    buildReport({
                        id: 5,
                        client_name: "",
                        client_version: "",
                        client_user_agent: "curl/8.5.0",
                    }),
                ],
                total: 2,
            }),
        });

        const article = within(articleFor(4));
        expect(article.getByText("User unhappy")).toBeTruthy();
        expect(
            article.getByText("No endpoint lists sample consent."),
        ).toBeTruthy();
        expect(article.getByText("User request")).toBeTruthy();
        expect(
            article.getByText("Which samples have withdrawn consent?"),
        ).toBeTruthy();
        expect(article.getByText("Tools tried")).toBeTruthy();
        expect(
            article.getByText("mlwh_search_samples, mlwh_call_endpoint"),
        ).toBeTruthy();
        expect(article.getByRole("time").getAttribute("datetime")).toBe(
            "2026-10-01T12:00:00Z",
        );

        const metadata = articleFor(4).textContent ?? "";
        expect(metadata).toContain("claude-code 2.1.0");
        expect(metadata).toContain("0.4.0");
        expect(metadata).toContain("1.9.0");
        expect(metadata).toContain("stdio");
        expect(metadata).toContain("10.0.0.5:51234");
        expect(articleFor(5).textContent).toContain("curl/8.5.0");
    });

    it("omits empty user request and tools tried", async () => {
        await renderView({
            page: buildPage({
                items: [
                    buildReport({ id: 1, user_request: "", tools_tried: [] }),
                    buildReport({ id: 2, user_request: "" }),
                    buildReport({ id: 3, tools_tried: [] }),
                ],
                total: 3,
            }),
        });

        const neither = within(articleFor(1));
        expect(neither.queryByText("User request")).toBeNull();
        expect(neither.queryByText("Tools tried")).toBeNull();

        const toolsOnly = within(articleFor(2));
        expect(toolsOnly.queryByText("User request")).toBeNull();
        expect(toolsOnly.getByText("Tools tried")).toBeTruthy();

        const requestOnly = within(articleFor(3));
        expect(requestOnly.getByText("User request")).toBeTruthy();
        expect(requestOnly.queryByText("Tools tried")).toBeNull();
    });

    it("acknowledges a report then refreshes", async () => {
        await renderView({
            page: buildPage({
                items: [
                    buildReport({ id: 1, acknowledged: true }),
                    buildReport({ id: 2 }),
                ],
                total: 2,
            }),
        });

        fireEvent.click(
            within(articleFor(2)).getByRole("button", { name: "Acknowledge" }),
        );

        await waitFor(() => {
            expect(navigationMocks.refresh).toHaveBeenCalledOnce();
        });
        expect(
            actionMocks.setFeedbackAcknowledgedAction,
        ).toHaveBeenCalledOnce();
        expect(actionMocks.setFeedbackAcknowledgedAction).toHaveBeenCalledWith(
            2,
            true,
        );
        expect(
            actionMocks.setFeedbackAcknowledgedAction.mock
                .invocationCallOrder[0],
        ).toBeLessThan(navigationMocks.refresh.mock.invocationCallOrder[0]);
        expect(actionMocks.deleteFeedbackAction).not.toHaveBeenCalled();
        expect(toastMocks.error).not.toHaveBeenCalled();
    });

    it("unacknowledges an acknowledged report", async () => {
        await renderView({
            page: buildPage({
                items: [buildReport({ id: 9, acknowledged: true })],
            }),
        });

        fireEvent.click(
            within(articleFor(9)).getByRole("button", {
                name: "Unacknowledge",
            }),
        );

        await waitFor(() => {
            expect(navigationMocks.refresh).toHaveBeenCalledOnce();
        });
        expect(actionMocks.setFeedbackAcknowledgedAction).toHaveBeenCalledWith(
            9,
            false,
        );
    });

    it("deletes only after confirmation", async () => {
        const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

        await renderView({
            page: buildPage({
                items: [buildReport({ id: 1 }), buildReport({ id: 3 })],
                total: 2,
            }),
        });

        const deleteButton = within(articleFor(3)).getByRole("button", {
            name: "Delete",
        });
        fireEvent.click(deleteButton);

        expect(confirmSpy).toHaveBeenCalledWith(
            "Delete feedback #3? This cannot be undone.",
        );
        expect(actionMocks.deleteFeedbackAction).not.toHaveBeenCalled();
        expect(navigationMocks.refresh).not.toHaveBeenCalled();

        confirmSpy.mockReturnValue(true);
        fireEvent.click(deleteButton);

        await waitFor(() => {
            expect(navigationMocks.refresh).toHaveBeenCalledOnce();
        });
        expect(actionMocks.deleteFeedbackAction).toHaveBeenCalledOnce();
        expect(actionMocks.deleteFeedbackAction).toHaveBeenCalledWith(3);
        expect(
            actionMocks.deleteFeedbackAction.mock.invocationCallOrder[0],
        ).toBeLessThan(navigationMocks.refresh.mock.invocationCallOrder[0]);
        expect(
            actionMocks.setFeedbackAcknowledgedAction,
        ).not.toHaveBeenCalled();
        expect(toastMocks.error).not.toHaveBeenCalled();
    });

    it("shows an error toast when acknowledge is not ok", async () => {
        actionMocks.setFeedbackAcknowledgedAction.mockResolvedValue({
            status: "not_found",
        });

        await renderView({
            page: buildPage({ items: [buildReport({ id: 2 })] }),
        });

        fireEvent.click(screen.getByRole("button", { name: "Acknowledge" }));

        await waitFor(() => {
            expect(toastMocks.error).toHaveBeenCalledOnce();
            expect(navigationMocks.refresh).toHaveBeenCalledOnce();
        });
        expect(toastMocks.success).not.toHaveBeenCalled();
    });

    it("shows an error toast when delete is not ok", async () => {
        vi.spyOn(window, "confirm").mockReturnValue(true);
        actionMocks.deleteFeedbackAction.mockResolvedValue({
            status: "unavailable",
            reason: "token_rejected",
        });

        await renderView({
            page: buildPage({ items: [buildReport({ id: 2 })] }),
        });

        fireEvent.click(screen.getByRole("button", { name: "Delete" }));

        await waitFor(() => {
            expect(toastMocks.error).toHaveBeenCalledOnce();
            expect(navigationMocks.refresh).toHaveBeenCalledOnce();
        });
    });

    it("shows an error toast when a mutation action rejects", async () => {
        actionMocks.setFeedbackAcknowledgedAction.mockRejectedValue(
            new Error("network down"),
        );

        await renderView({
            page: buildPage({ items: [buildReport({ id: 2 })] }),
        });

        fireEvent.click(screen.getByRole("button", { name: "Acknowledge" }));

        await waitFor(() => {
            expect(toastMocks.error).toHaveBeenCalledOnce();
            expect(navigationMocks.refresh).toHaveBeenCalledOnce();
        });
    });

    it("keeps each report busy until its own concurrent mutation resolves", async () => {
        const first = deferred<FeedbackMutationState>();
        const second = deferred<FeedbackMutationState>();

        actionMocks.setFeedbackAcknowledgedAction
            .mockReturnValueOnce(first.promise)
            .mockReturnValueOnce(second.promise);

        await renderView({
            page: buildPage({
                items: [buildReport({ id: 1 }), buildReport({ id: 2 })],
                total: 2,
            }),
        });

        const buttonsOf = (id: number) =>
            within(articleFor(id)).getAllByRole<HTMLButtonElement>("button");
        const disabledStates = (id: number) =>
            buttonsOf(id).map((button) => button.disabled);

        fireEvent.click(
            within(articleFor(1)).getByRole("button", { name: "Acknowledge" }),
        );
        fireEvent.click(
            within(articleFor(2)).getByRole("button", { name: "Acknowledge" }),
        );

        expect(disabledStates(1)).toEqual([true, true]);
        expect(disabledStates(2)).toEqual([true, true]);

        first.resolve({ status: "ok" });

        await waitFor(() => {
            expect(disabledStates(1)).toEqual([false, false]);
        });
        expect(disabledStates(2)).toEqual([true, true]);

        second.resolve({ status: "ok" });

        await waitFor(() => {
            expect(disabledStates(2)).toEqual([false, false]);
        });
        expect(actionMocks.setFeedbackAcknowledgedAction).toHaveBeenCalledTimes(
            2,
        );
        expect(navigationMocks.refresh).toHaveBeenCalledTimes(2);
    });

    it("keeps a report busy until its refresh delivers new data", async () => {
        const { FeedbackAdminView } =
            await import("@/components/feedback-admin-view");
        const fulfilled = (page: FeedbackPage) =>
            Object.assign(Promise.resolve(page), {
                status: "fulfilled",
                value: page,
            });
        const refreshed = deferred<FeedbackPage>();
        const pageSetter: {
            current: (promise: Promise<FeedbackPage>) => void;
        } = { current: () => {} };

        // Mimic the App Router: refresh starts a transition that swaps in a
        // pending RSC payload the tree reads with use(), so it suspends and
        // keeps the old UI until the payload arrives.
        function RoutedView() {
            const [pagePromise, setPromise] = useState<Promise<FeedbackPage>>(
                () => fulfilled(buildPage({ items: [buildReport({ id: 2 })] })),
            );

            useEffect(() => {
                pageSetter.current = setPromise;
            }, []);

            return createElement(FeedbackAdminView, {
                category: null,
                offset: 0,
                page: use(pagePromise),
                show: "all",
            });
        }

        navigationMocks.refresh.mockImplementationOnce(() => {
            startTransition(() => {
                pageSetter.current(refreshed.promise);
            });
        });
        render(createElement(Suspense, null, createElement(RoutedView)));

        fireEvent.click(screen.getByRole("button", { name: "Acknowledge" }));

        await waitFor(() => {
            expect(navigationMocks.refresh).toHaveBeenCalledOnce();
        });
        expect(
            screen.getByRole<HTMLButtonElement>("button", {
                name: "Acknowledge",
            }).disabled,
        ).toBe(true);

        await act(async () => {
            refreshed.resolve(
                buildPage({
                    items: [buildReport({ id: 2, acknowledged: true })],
                }),
            );
            await refreshed.promise;
        });

        await waitFor(() => {
            expect(
                screen.getByRole<HTMLButtonElement>("button", {
                    name: "Unacknowledge",
                }).disabled,
            ).toBe(false);
        });
    });

    it("pushes show=all and keeps category when Show acknowledged is ticked", async () => {
        await renderView({ show: "unacknowledged", category: "other" });

        fireEvent.click(
            screen.getByRole("checkbox", { name: "Show acknowledged" }),
        );

        expect(navigationMocks.push).toHaveBeenCalledOnce();
        expect(navigationMocks.push).toHaveBeenCalledWith(
            "/feedback?show=all&category=other",
        );
    });

    it("pushes /feedback when Show acknowledged is unticked with no category", async () => {
        await renderView({ show: "all", category: null, offset: 50 });

        fireEvent.click(
            screen.getByRole("checkbox", { name: "Show acknowledged" }),
        );

        expect(navigationMocks.push).toHaveBeenCalledOnce();
        expect(navigationMocks.push).toHaveBeenCalledWith("/feedback");
    });

    it("offers every category in the Category select", async () => {
        await renderView();

        const select = screen.getByRole<HTMLSelectElement>("combobox", {
            name: "Category",
        });
        const options = Array.from(select.options).map((option) => [
            option.textContent,
            option.value,
        ]);

        expect(options).toEqual([
            ["All categories", ""],
            ["Could not answer", "could_not_answer"],
            ["Agent mistake", "agent_mistake"],
            ["No endpoint", "no_endpoint"],
            ["User unhappy", "user_unhappy"],
            ["Other", "other"],
        ]);
        expect(select.value).toBe("");
    });

    it("pushes the selected category and drops offset", async () => {
        await renderView({ show: "all", category: null, offset: 50 });

        fireEvent.change(screen.getByRole("combobox", { name: "Category" }), {
            target: { value: "no_endpoint" },
        });

        expect(navigationMocks.push).toHaveBeenCalledOnce();
        expect(navigationMocks.push).toHaveBeenCalledWith(
            "/feedback?show=all&category=no_endpoint",
        );
    });

    it("omits show when changing category while unacknowledged", async () => {
        await renderView({ show: "unacknowledged", category: "other" });

        fireEvent.change(screen.getByRole("combobox", { name: "Category" }), {
            target: { value: "user_unhappy" },
        });

        expect(navigationMocks.push).toHaveBeenCalledWith(
            "/feedback?category=user_unhappy",
        );
    });

    it("pushes without category when All categories is selected", async () => {
        await renderView({ show: "all", category: "no_endpoint", offset: 50 });

        fireEvent.change(screen.getByRole("combobox", { name: "Category" }), {
            target: { value: "" },
        });

        expect(navigationMocks.push).toHaveBeenCalledOnce();
        expect(navigationMocks.push).toHaveBeenCalledWith("/feedback?show=all");
    });

    it("links Next from the first page and omits Previous", async () => {
        await renderView({
            show: "unacknowledged",
            category: null,
            offset: 0,
            page: buildPage({ next_offset: 50, total: 120 }),
        });

        expect(linkHref("Next")).toBe("/feedback?offset=50");
        expect(screen.queryByRole("link", { name: "Previous" })).toBeNull();
    });

    it("keeps filters and uses next_offset on the Next link", async () => {
        await renderView({
            show: "all",
            category: "agent_mistake",
            offset: 50,
            page: buildPage({ next_offset: 75, total: 220 }),
        });

        expect(linkHref("Next")).toBe(
            "/feedback?show=all&category=agent_mistake&offset=75",
        );
    });

    it("builds Previous links at each edge and omits Next on the last page", async () => {
        const cases: Array<{
            category: FeedbackCategory | null;
            expected: string;
            offset: number;
            show: Show;
        }> = [
            {
                show: "all",
                category: "other",
                offset: 120,
                expected: "/feedback?show=all&category=other&offset=70",
            },
            {
                show: "all",
                category: "other",
                offset: 30,
                expected: "/feedback?show=all&category=other",
            },
            {
                show: "unacknowledged",
                category: null,
                offset: 50,
                expected: "/feedback",
            },
            {
                show: "unacknowledged",
                category: "other",
                offset: 51,
                expected: "/feedback?category=other&offset=1",
            },
        ];

        for (const { show, category, offset, expected } of cases) {
            await renderView({
                show,
                category,
                offset,
                page: buildPage({ next_offset: -1 }),
            });

            expect(linkHref("Previous")).toBe(expected);
            expect(screen.queryByRole("link", { name: "Next" })).toBeNull();
            cleanup();
        }
    });

    it("shows the empty message for an empty page", async () => {
        await renderView({
            page: buildPage({ items: [], total: 0, next_offset: -1 }),
        });

        expect(screen.getByText("No feedback to show.")).toBeTruthy();
        expect(screen.queryByRole("article")).toBeNull();
    });

    it("does not show the empty message when reports exist", async () => {
        await renderView();

        expect(screen.queryByText("No feedback to show.")).toBeNull();
    });
});
