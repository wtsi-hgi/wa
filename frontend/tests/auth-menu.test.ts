/**
 * @vitest-environment jsdom
 */

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
const navigationMocks = vi.hoisted(() => ({
    refresh: vi.fn(),
}));

vi.mock("@/app/(results)/auth/actions", () => ({
    currentSession: authActionMocks.currentSession,
    loginAction: authActionMocks.loginAction,
    logoutAction: authActionMocks.logoutAction,
}));
vi.mock("next/navigation", () => ({
    useRouter: () => ({
        refresh: navigationMocks.refresh,
    }),
}));

beforeEach(() => {
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

describe("E3 auth menu", () => {
    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
        vi.unstubAllGlobals();
    });

    it("shows a Log in button for anonymous sessions", async () => {
        await renderAuthMenu({ authenticated: false, username: null });

        expect(screen.getByRole("button", { name: "Log in" })).toBeTruthy();
    });

    it("seeds the results layout auth menu from the current session", async () => {
        authActionMocks.currentSession.mockResolvedValueOnce({
            authenticated: false,
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
                    username: "alice",
                }),
            ),
        );

        await renderAuthMenu({ authenticated: true, username: "alice" });

        const accountButton = screen.getByRole("button", {
            name: /alice account/i,
        });

        expect(within(accountButton).getByText("alice")).toBeTruthy();
        expect(document.querySelector('[data-slot="avatar"]')).toBeNull();
        expect(
            document.querySelector('[data-slot="avatar-fallback"]'),
        ).toBeNull();

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

    it("refreshes an authenticated session through the browser on access", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn().mockResolvedValue(
                Response.json({
                    authenticated: true,
                    username: "alice",
                }),
            ),
        );

        await renderAuthMenu({ authenticated: true, username: "alice" });

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

        await renderAuthMenu({ authenticated: false, username: null });

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
            username: "alice",
        });

        await renderAuthMenu({ authenticated: false, username: null });

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

    it("removes the username and shows Log in after successful logout", async () => {
        const fetchMock = vi.fn((url: string) =>
            Promise.resolve(
                Response.json(
                    url === "/api/auth/refresh"
                        ? {
                              authenticated: true,
                              username: "alice",
                          }
                        : {
                              authenticated: false,
                              username: null,
                          },
                ),
            ),
        );
        vi.stubGlobal("fetch", fetchMock);
        authActionMocks.logoutAction.mockResolvedValueOnce({
            authenticated: false,
            username: null,
        });

        await renderAuthMenu({ authenticated: true, username: "alice" });

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

        await waitFor(() => {
            expect(screen.queryByText("alice")).toBeNull();
        });
        expect(screen.getByRole("button", { name: "Log in" })).toBeTruthy();
        expect(navigationMocks.refresh).toHaveBeenCalled();
    });

    it("shows Log in after logout clears the cookie but the backend call fails", async () => {
        authActionMocks.logoutAction.mockRejectedValueOnce(
            new Error("results backend request failed"),
        );

        await renderAuthMenu({ authenticated: true, username: "alice" });

        fireEvent.click(screen.getByRole("button", { name: /alice account/i }));
        fireEvent.click(screen.getByRole("menuitem", { name: "Log out" }));

        await waitFor(() => {
            expect(screen.queryByText("alice")).toBeNull();
        });
        expect(screen.getByRole("button", { name: "Log in" })).toBeTruthy();
        expect(navigationMocks.refresh).toHaveBeenCalled();
    });
});
