/**
 * @vitest-environment jsdom
 */

import type os from "node:os";

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
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

import { AppProviders } from "@/components/app-providers";
import type { CurrentSession } from "@/app/(results)/auth/actions";

const authActionMocks = vi.hoisted(() => ({
    currentSession: vi.fn(),
    loginAction: vi.fn(),
    logoutAction: vi.fn(),
}));
const headerMocks = vi.hoisted(() => ({
    cookies: vi.fn(),
}));
const navigationMocks = vi.hoisted(() => ({
    pathname: "/",
    push: vi.fn(),
    refresh: vi.fn(),
    searchParams: new URLSearchParams(),
}));
const browserNavigationMocks = vi.hoisted(() => ({
    reloadDocument: vi.fn(),
}));

vi.mock("@/app/(results)/auth/actions", () => ({
    currentSession: authActionMocks.currentSession,
    loginAction: authActionMocks.loginAction,
    logoutAction: authActionMocks.logoutAction,
}));
vi.mock("next/headers", () => ({
    cookies: headerMocks.cookies,
}));
vi.mock("next/navigation", () => ({
    usePathname: () => navigationMocks.pathname,
    useRouter: () => ({
        push: navigationMocks.push,
        refresh: navigationMocks.refresh,
    }),
    useSearchParams: () => navigationMocks.searchParams,
}));
vi.mock("@/lib/browser-navigation", () => ({
    reloadDocument: browserNavigationMocks.reloadDocument,
}));

const userInfoMock = vi.hoisted(() => vi.fn());

// Replace default as well as the named export: under the jsdom environment,
// app modules' named node:os imports resolve through the default export.
vi.mock("node:os", async (importOriginal) => {
    const actual = await importOriginal<typeof import("node:os")>();

    return {
        ...actual,
        default: { ...actual, userInfo: userInfoMock },
        userInfo: userInfoMock,
    };
});

beforeEach(() => {
    navigationMocks.pathname = "/";
    navigationMocks.searchParams = new URLSearchParams();
    userInfoMock
        .mockReset()
        .mockReturnValue({ username: "svc" } as ReturnType<typeof os.userInfo>);
    vi.stubGlobal("matchMedia", () => ({
        addEventListener: vi.fn(),
        addListener: vi.fn(),
        dispatchEvent: vi.fn(),
        matches: false,
        media: "",
        onchange: null,
        removeEventListener: vi.fn(),
        removeListener: vi.fn(),
    }));
});

function renderAuthMenu(initialSession: CurrentSession) {
    return import("@/components/auth-menu").then(({ AuthMenu }) =>
        render(
            createElement(
                AppProviders,
                undefined,
                createElement(AuthMenu, { initialSession }),
            ),
        ),
    );
}

function stubSessionRefresh(session: CurrentSession) {
    const fetchMock = vi.fn(() => Promise.resolve(Response.json(session)));

    vi.stubGlobal("fetch", fetchMock);

    return fetchMock;
}

// The browser session refresh never answers, so whatever the menu shows can
// only come from the login result itself.
function stubPendingSessionRefresh() {
    const fetchMock = vi.fn(() => new Promise<Response>(() => {}));

    vi.stubGlobal("fetch", fetchMock);

    return fetchMock;
}

async function submitLogin(username: string): Promise<void> {
    fireEvent.click(screen.getByRole("button", { name: "Log in" }));
    fireEvent.change(screen.getByLabelText("Username"), {
        target: { value: username },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
        target: { value: "secret" },
    });
    fireEvent.submit(screen.getByRole("form", { name: "Log in" }));

    await screen.findByRole("button", {
        name: new RegExp(`^${username} account$`),
    });
}

function openAccountMenu(username: string): HTMLElement {
    fireEvent.click(
        screen.getByRole("button", {
            name: new RegExp(`^${username} account$`),
        }),
    );

    return screen.getByRole("menu");
}

// serverSession runs the real currentSession() against a stubbed results
// backend, so the server's admin decision (WA_FEEDBACK_ADMINS, OS user) is
// the one the layout hands to the menu.
async function serverSession(username: string): Promise<CurrentSession> {
    const { currentSession } = await vi.importActual<
        typeof import("@/app/(results)/auth/actions")
    >("@/app/(results)/auth/actions");

    vi.stubEnv("WA_RESULTS_BACKEND_URL", "https://results.example/api");
    vi.stubEnv("WA_RESULTS_BACKEND_CA_CERT", undefined);
    headerMocks.cookies.mockResolvedValue({ get: () => ({ value: "jwt" }) });
    vi.stubGlobal(
        "fetch",
        vi.fn(() =>
            Promise.resolve(
                Response.json({
                    authenticated: true,
                    is_owner: false,
                    username,
                }),
            ),
        ),
    );

    return currentSession();
}

async function renderResultsLayoutMenu(username: string): Promise<HTMLElement> {
    const session = await serverSession(username);

    authActionMocks.currentSession.mockResolvedValueOnce(session);
    stubSessionRefresh(session);

    const { default: ResultsLayout } = await import("@/app/(results)/layout");
    const layout = await ResultsLayout({
        children: createElement("main", undefined, "Landing page"),
    });

    render(createElement(AppProviders, undefined, layout));

    return openAccountMenu(username);
}

describe("E3 auth menu", () => {
    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
        vi.unstubAllGlobals();
    });

    it("shows a Log in button for anonymous sessions", async () => {
        await renderAuthMenu({
            authenticated: false,
            isAdmin: false,
            username: null,
        });

        expect(screen.getByRole("button", { name: "Log in" })).toBeTruthy();
    });

    it("seeds the results layout auth menu from the current session", async () => {
        authActionMocks.currentSession.mockResolvedValueOnce({
            authenticated: false,
            isAdmin: false,
            username: null,
        });

        const { default: ResultsLayout } =
            await import("@/app/(results)/layout");

        const layout = await ResultsLayout({
            children: createElement("main", undefined, "Landing page"),
        });

        const markup = renderToStaticMarkup(
            createElement(AppProviders, undefined, layout),
        );

        expect(markup).toContain("Log in");
        expect(markup).toContain("Landing page");
    });

    it("reserves layout space for the auth menu before page content", async () => {
        authActionMocks.currentSession.mockResolvedValueOnce({
            authenticated: false,
            isAdmin: false,
            username: null,
        });

        const { default: ResultsLayout } =
            await import("@/app/(results)/layout");

        const layout = await ResultsLayout({
            children: createElement("main", undefined, "Landing page"),
        });

        const markup = renderToStaticMarkup(
            createElement(AppProviders, undefined, layout),
        );

        expect(markup).toContain('data-results-auth-bar="true"');
        expect(markup.indexOf('data-results-auth-bar="true"')).toBeLessThan(
            markup.indexOf("Landing page"),
        );
        expect(markup).not.toContain("fixed top-4 right-4");
    });

    it("shows only the username in the signed-in account trigger and menu", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn().mockResolvedValue(
                Response.json({
                    authenticated: true,
                    isAdmin: false,
                    username: "alice",
                }),
            ),
        );

        await renderAuthMenu({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        const accountButton = screen.getByRole("button", {
            name: /alice account/i,
        });

        expect(within(accountButton).getByText("alice")).toBeTruthy();
        expect(document.querySelector('[data-slot="avatar"]')).toBeNull();
        expect(
            document.querySelector('[data-slot="avatar-fallback"]'),
        ).toBeNull();
        expect(
            (
                screen.getByRole("checkbox", {
                    name: "Only show accessible result sets",
                }) as HTMLInputElement
            ).checked,
        ).toBe(true);

        fireEvent.click(accountButton);

        const accountMenu = screen.getByRole("menu");

        expect(within(accountMenu).getByText("alice")).toBeTruthy();
        expect(
            within(accountMenu).getByRole("menuitem", { name: "Log out" }),
        ).toBeTruthy();
        expect(document.querySelector('[data-slot="avatar"]')).toBeNull();
        expect(
            document.querySelector('[data-slot="avatar-fallback"]'),
        ).toBeNull();
    });

    it("keeps the access filter enabled by default and preserves search params when disabled", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn().mockResolvedValue(
                Response.json({
                    authenticated: true,
                    isAdmin: false,
                    username: "alice",
                }),
            ),
        );
        navigationMocks.searchParams = new URLSearchParams("user=alice");

        await renderAuthMenu({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        const accessToggle = screen.getByRole("checkbox", {
            name: "Only show accessible result sets",
        });

        expect((accessToggle as HTMLInputElement).checked).toBe(true);

        fireEvent.click(accessToggle);

        expect(navigationMocks.push).toHaveBeenCalledWith(
            "/?user=alice&show_locked=1",
        );
    });

    it("removes only the access opt-out flag when the signed-in toggle is re-enabled", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn().mockResolvedValue(
                Response.json({
                    authenticated: true,
                    isAdmin: false,
                    username: "alice",
                }),
            ),
        );
        navigationMocks.searchParams = new URLSearchParams(
            "user=alice&show_locked=1",
        );

        await renderAuthMenu({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        const accessToggle = screen.getByRole("checkbox", {
            name: "Only show accessible result sets",
        });

        expect((accessToggle as HTMLInputElement).checked).toBe(false);

        fireEvent.click(accessToggle);

        expect(navigationMocks.push).toHaveBeenCalledWith("/?user=alice");
    });

    it("refreshes an authenticated session through the browser on access", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn().mockResolvedValue(
                Response.json({
                    authenticated: true,
                    isAdmin: false,
                    username: "alice",
                }),
            ),
        );

        await renderAuthMenu({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        await waitFor(() => {
            expect(fetch).toHaveBeenCalledWith(
                "/api/auth/refresh",
                expect.objectContaining({
                    cache: "no-store",
                    credentials: "same-origin",
                    method: "POST",
                }),
            );
        });
    });

    it("keeps focus in the login control and announces Authentication failed", async () => {
        authActionMocks.loginAction.mockRejectedValueOnce(
            new Error("authentication failed"),
        );

        await renderAuthMenu({
            authenticated: false,
            isAdmin: false,
            username: null,
        });

        fireEvent.click(screen.getByRole("button", { name: "Log in" }));

        const usernameInput = screen.getByLabelText("Username");

        fireEvent.change(usernameInput, { target: { value: "alice" } });
        fireEvent.change(screen.getByLabelText("Password"), {
            target: { value: "wrong" },
        });
        fireEvent.submit(screen.getByRole("form", { name: "Log in" }));

        await screen.findByText("Authentication failed");

        expect(screen.getByRole("alert").textContent).toContain(
            "Authentication failed",
        );
        await waitFor(() => {
            expect(document.activeElement).toBe(usernameInput);
        });
    });

    it("shows the signed-in account menu after anonymous login succeeds", async () => {
        authActionMocks.loginAction.mockResolvedValueOnce({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        await renderAuthMenu({
            authenticated: false,
            isAdmin: false,
            username: null,
        });

        fireEvent.click(screen.getByRole("button", { name: "Log in" }));
        fireEvent.change(screen.getByLabelText("Username"), {
            target: { value: "alice" },
        });
        fireEvent.change(screen.getByLabelText("Password"), {
            target: { value: "secret" },
        });
        fireEvent.submit(screen.getByRole("form", { name: "Log in" }));

        const accountButton = await screen.findByRole("button", {
            name: /alice account/i,
        });

        expect(within(accountButton).getByText("alice")).toBeTruthy();
        expect(document.querySelector('[data-slot="avatar"]')).toBeNull();
        expect(
            document.querySelector('[data-slot="avatar-fallback"]'),
        ).toBeNull();

        fireEvent.click(accountButton);

        expect(
            within(screen.getByRole("menu")).getByText("alice"),
        ).toBeTruthy();
        expect(document.querySelector('[data-slot="avatar"]')).toBeNull();
        expect(
            document.querySelector('[data-slot="avatar-fallback"]'),
        ).toBeNull();
    });

    it("keeps the authenticated client tree intact until logout reloads the document", async () => {
        const fetchMock = vi.fn((url: string) =>
            Promise.resolve(
                Response.json(
                    url === "/api/auth/refresh"
                        ? {
                              authenticated: true,
                              isAdmin: false,
                              username: "alice",
                          }
                        : {
                              authenticated: false,
                              isAdmin: false,
                              username: null,
                          },
                ),
            ),
        );
        vi.stubGlobal("fetch", fetchMock);
        authActionMocks.logoutAction.mockResolvedValueOnce({
            authenticated: false,
            isAdmin: false,
            username: null,
        });

        await renderAuthMenu({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        await waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                "/api/auth/refresh",
                expect.any(Object),
            );
        });

        fireEvent.click(screen.getByRole("button", { name: /alice account/i }));
        await act(async () => {
            fireEvent.click(screen.getByRole("menuitem", { name: "Log out" }));
        });

        expect(authActionMocks.logoutAction).toHaveBeenCalled();
        expect(fetchMock).not.toHaveBeenCalledWith(
            "/api/auth/logout",
            expect.any(Object),
        );

        expect(
            screen.getByRole("button", { name: /alice account/i }),
        ).toBeTruthy();
        expect(screen.queryByRole("button", { name: "Log in" })).toBeNull();
        expect(navigationMocks.refresh).not.toHaveBeenCalled();
    });

    it("keeps the authenticated client tree intact when logout reloads after an action failure", async () => {
        authActionMocks.logoutAction.mockRejectedValueOnce(
            new Error("results backend request failed"),
        );

        await renderAuthMenu({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        fireEvent.click(screen.getByRole("button", { name: /alice account/i }));
        await act(async () => {
            fireEvent.click(screen.getByRole("menuitem", { name: "Log out" }));
        });

        expect(
            screen.getByRole("button", { name: /alice account/i }),
        ).toBeTruthy();
        expect(screen.queryByRole("button", { name: "Log in" })).toBeNull();
        expect(navigationMocks.refresh).not.toHaveBeenCalled();
        expect(browserNavigationMocks.reloadDocument).toHaveBeenCalledOnce();
    });
});

describe("E5 auth menu feedback link", () => {
    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
        vi.unstubAllGlobals();
        vi.unstubAllEnvs();
    });

    it("shows a Feedback menu link to /feedback above Log out for an admin session", async () => {
        stubSessionRefresh({
            authenticated: true,
            isAdmin: true,
            username: "alice",
        });

        await renderAuthMenu({
            authenticated: true,
            isAdmin: true,
            username: "alice",
        });

        const menu = openAccountMenu("alice");
        const feedback = within(menu).getByRole("menuitem", {
            name: "Feedback",
        });

        expect(feedback.tagName).toBe("A");
        expect(feedback.getAttribute("href")).toBe("/feedback");
        expect(
            within(menu)
                .getAllByRole("menuitem")
                .map((item) => item.textContent),
        ).toEqual(["Feedback", "Usage", "Log out"]);
    });

    it("closes the account menu when navigation changes the route", async () => {
        stubSessionRefresh({
            authenticated: true,
            isAdmin: true,
            username: "alice",
        });

        const session = {
            authenticated: true,
            isAdmin: true,
            username: "alice",
        };
        const { AuthMenu } = await import("@/components/auth-menu");
        const tree = () =>
            createElement(
                AppProviders,
                undefined,
                createElement(AuthMenu, { initialSession: session }),
            );
        const { rerender } = render(tree());

        openAccountMenu("alice");
        navigationMocks.pathname = "/feedback";
        rerender(tree());

        expect(screen.queryByRole("menu")).toBeNull();
    });

    it("closes the account menu when Feedback is chosen on /feedback", async () => {
        navigationMocks.pathname = "/feedback";
        stubSessionRefresh({
            authenticated: true,
            isAdmin: true,
            username: "alice",
        });

        await renderAuthMenu({
            authenticated: true,
            isAdmin: true,
            username: "alice",
        });

        const menu = openAccountMenu("alice");

        fireEvent.click(
            within(menu).getByRole("menuitem", { name: "Feedback" }),
        );

        expect(screen.queryByRole("menu")).toBeNull();
    });

    it("hides the Feedback link from a non-admin authenticated session", async () => {
        stubSessionRefresh({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        await renderAuthMenu({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        const menu = openAccountMenu("alice");

        expect(
            within(menu)
                .getAllByRole("menuitem")
                .map((item) => item.textContent),
        ).toEqual(["Log out"]);
        expect(screen.queryByText("Feedback")).toBeNull();
        expect(document.querySelector('a[href="/feedback"]')).toBeNull();
    });

    it("hides the Feedback link from anonymous sessions even when flagged admin", async () => {
        await renderAuthMenu({
            authenticated: false,
            isAdmin: true,
            username: null,
        });

        expect(screen.queryByText("Feedback")).toBeNull();

        fireEvent.click(screen.getByRole("button", { name: "Log in" }));

        expect(screen.getByRole("form", { name: "Log in" })).toBeTruthy();
        expect(screen.queryByText("Feedback")).toBeNull();
        expect(document.querySelector('a[href="/feedback"]')).toBeNull();
    });

    it("hides the layout Feedback link from a non-admin user", async () => {
        vi.stubEnv("WA_FEEDBACK_ADMINS", undefined);

        const menu = await renderResultsLayoutMenu("bob");

        expect(
            within(menu).getByRole("menuitem", { name: "Log out" }),
        ).toBeTruthy();
        expect(
            within(menu).queryByRole("menuitem", { name: "Feedback" }),
        ).toBeNull();
    });

    it("shows the layout Feedback link to the server's OS user", async () => {
        vi.stubEnv("WA_FEEDBACK_ADMINS", undefined);

        const menu = await renderResultsLayoutMenu("svc");

        expect(
            within(menu)
                .getByRole("menuitem", { name: "Feedback" })
                .getAttribute("href"),
        ).toBe("/feedback");
    });

    it("shows the layout Feedback link to a WA_FEEDBACK_ADMINS user", async () => {
        vi.stubEnv("WA_FEEDBACK_ADMINS", "alice");

        const menu = await renderResultsLayoutMenu("alice");

        expect(
            within(menu)
                .getByRole("menuitem", { name: "Feedback" })
                .getAttribute("href"),
        ).toBe("/feedback");
    });

    it("refreshes the route once after a successful login and not after a failed one", async () => {
        const fetchMock = stubSessionRefresh({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        authActionMocks.loginAction.mockResolvedValueOnce({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        await renderAuthMenu({
            authenticated: false,
            isAdmin: false,
            username: null,
        });

        fireEvent.click(screen.getByRole("button", { name: "Log in" }));
        fireEvent.change(screen.getByLabelText("Username"), {
            target: { value: "alice" },
        });
        fireEvent.change(screen.getByLabelText("Password"), {
            target: { value: "secret" },
        });
        fireEvent.submit(screen.getByRole("form", { name: "Log in" }));

        await waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                "/api/auth/refresh",
                expect.any(Object),
            );
        });
        await screen.findByRole("button", { name: /^alice account$/ });

        expect(navigationMocks.refresh).toHaveBeenCalledOnce();

        cleanup();
        vi.clearAllMocks();

        authActionMocks.loginAction.mockRejectedValueOnce(
            new Error("authentication failed"),
        );

        await renderAuthMenu({
            authenticated: false,
            isAdmin: false,
            username: null,
        });

        fireEvent.click(screen.getByRole("button", { name: "Log in" }));
        fireEvent.submit(screen.getByRole("form", { name: "Log in" }));

        await screen.findByText("Authentication failed");

        expect(navigationMocks.refresh).not.toHaveBeenCalled();
    });

    it("shows the Feedback and Usage links to an admin as soon as login completes, before the layout re-renders", async () => {
        const fetchMock = stubPendingSessionRefresh();
        authActionMocks.loginAction.mockResolvedValueOnce({
            authenticated: true,
            isAdmin: true,
            username: "alice",
        });

        await renderAuthMenu({
            authenticated: false,
            isAdmin: false,
            username: null,
        });
        await submitLogin("alice");

        const menu = openAccountMenu("alice");

        expect(
            within(menu)
                .getAllByRole("menuitem")
                .map((item) => item.textContent),
        ).toEqual(["Feedback", "Usage", "Log out"]);
        expect(
            within(menu)
                .getByRole("menuitem", { name: "Feedback" })
                .getAttribute("href"),
        ).toBe("/feedback");
        expect(
            within(menu)
                .getByRole("menuitem", { name: "Usage" })
                .getAttribute("href"),
        ).toBe("/usage");
        expect(navigationMocks.refresh).toHaveBeenCalledOnce();
        expect(fetchMock).toHaveBeenCalledWith(
            "/api/auth/refresh",
            expect.any(Object),
        );
    });

    it("hides the Feedback and Usage links from a non-admin straight after login", async () => {
        const fetchMock = stubPendingSessionRefresh();
        authActionMocks.loginAction.mockResolvedValueOnce({
            authenticated: true,
            isAdmin: false,
            username: "bob",
        });

        await renderAuthMenu({
            authenticated: false,
            isAdmin: false,
            username: null,
        });
        await submitLogin("bob");

        const menu = openAccountMenu("bob");

        expect(
            within(menu)
                .getAllByRole("menuitem")
                .map((item) => item.textContent),
        ).toEqual(["Log out"]);
        expect(document.querySelector('a[href="/feedback"]')).toBeNull();
        expect(document.querySelector('a[href="/usage"]')).toBeNull();
        expect(fetchMock).toHaveBeenCalledWith(
            "/api/auth/refresh",
            expect.any(Object),
        );
    });

    it("drops the Feedback link when the browser session refresh reports the user is no longer an admin", async () => {
        stubSessionRefresh({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        await renderAuthMenu({
            authenticated: true,
            isAdmin: true,
            username: "alice",
        });

        const menu = openAccountMenu("alice");

        await waitFor(() => {
            expect(
                within(menu)
                    .getAllByRole("menuitem")
                    .map((item) => item.textContent),
            ).toEqual(["Log out"]);
        });
    });

    it("renders the layout when the OS user lookup throws", async () => {
        vi.stubEnv("WA_FEEDBACK_ADMINS", "alice");
        userInfoMock.mockImplementation(() => {
            throw new Error("uid has no passwd entry");
        });

        const aliceMenu = await renderResultsLayoutMenu("alice");

        expect(userInfoMock).toHaveBeenCalled();
        expect(
            within(aliceMenu)
                .getByRole("menuitem", { name: "Feedback" })
                .getAttribute("href"),
        ).toBe("/feedback");

        cleanup();

        const bobMenu = await renderResultsLayoutMenu("bob");

        expect(
            within(bobMenu).getByRole("menuitem", { name: "Log out" }),
        ).toBeTruthy();
        expect(
            within(bobMenu).queryByRole("menuitem", { name: "Feedback" }),
        ).toBeNull();
    });
});

describe("E5 auth menu usage link", () => {
    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
        vi.unstubAllGlobals();
        vi.unstubAllEnvs();
    });

    it("shows a Usage menu link to /usage after Feedback and before Log out", async () => {
        stubSessionRefresh({
            authenticated: true,
            isAdmin: true,
            username: "alice",
        });

        await renderAuthMenu({
            authenticated: true,
            isAdmin: true,
            username: "alice",
        });

        const menu = openAccountMenu("alice");
        const usage = within(menu).getByRole("menuitem", { name: "Usage" });

        expect(usage.tagName).toBe("A");
        expect(usage.getAttribute("href")).toBe("/usage");
        expect(usage.querySelector("svg.lucide-chart-column")).not.toBeNull();
        expect(
            within(menu)
                .getAllByRole("menuitem")
                .map((item) => item.textContent),
        ).toEqual(["Feedback", "Usage", "Log out"]);
    });

    it("hides the Usage link from a non-admin authenticated session", async () => {
        stubSessionRefresh({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        await renderAuthMenu({
            authenticated: true,
            isAdmin: false,
            username: "alice",
        });

        const menu = openAccountMenu("alice");

        expect(
            within(menu)
                .getAllByRole("menuitem")
                .map((item) => item.textContent),
        ).toEqual(["Log out"]);
        expect(document.querySelector('a[href="/usage"]')).toBeNull();
    });

    it("hides the Usage link from anonymous sessions even when flagged admin", async () => {
        await renderAuthMenu({
            authenticated: false,
            isAdmin: true,
            username: null,
        });

        expect(screen.queryByText("Usage")).toBeNull();

        fireEvent.click(screen.getByRole("button", { name: "Log in" }));

        expect(screen.getByRole("form", { name: "Log in" })).toBeTruthy();
        expect(screen.queryByText("Usage")).toBeNull();
        expect(document.querySelector('a[href="/usage"]')).toBeNull();
    });

    it("shows the layout Usage and Feedback links to the server's OS user", async () => {
        vi.stubEnv("WA_FEEDBACK_ADMINS", undefined);

        const menu = await renderResultsLayoutMenu("svc");

        expect(
            within(menu)
                .getByRole("menuitem", { name: "Usage" })
                .getAttribute("href"),
        ).toBe("/usage");
        expect(
            within(menu)
                .getByRole("menuitem", { name: "Feedback" })
                .getAttribute("href"),
        ).toBe("/feedback");
    });

    it("hides the layout Usage and Feedback links from a non-admin user", async () => {
        vi.stubEnv("WA_FEEDBACK_ADMINS", undefined);

        const menu = await renderResultsLayoutMenu("bob");

        expect(
            within(menu)
                .getAllByRole("menuitem")
                .map((item) => item.textContent),
        ).toEqual(["Log out"]);
    });
});
