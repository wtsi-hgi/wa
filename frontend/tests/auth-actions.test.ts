import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const headerMocks = vi.hoisted(() => ({
    cookies: vi.fn(),
    getCookie: vi.fn(),
    setCookie: vi.fn(),
}));

vi.mock("next/headers", () => ({
    cookies: headerMocks.cookies,
}));

function expectFetchInit(callIndex = 0): RequestInit {
    const init = vi.mocked(fetch).mock.calls[callIndex]?.[1];

    expect(init).toBeDefined();

    return init as RequestInit;
}

describe("E1 auth server actions", () => {
    beforeEach(() => {
        process.env.WA_RESULTS_BACKEND_URL = "https://results.example/api";
        delete process.env.WA_RESULTS_BACKEND_CA_CERT;
        headerMocks.cookies.mockResolvedValue({
            get: headerMocks.getCookie,
            set: headerMocks.setCookie,
        });
        vi.stubGlobal("fetch", vi.fn());
    });

    afterEach(() => {
        delete process.env.WA_RESULTS_BACKEND_URL;
        delete process.env.WA_RESULTS_BACKEND_CA_CERT;
        headerMocks.cookies.mockReset();
        headerMocks.getCookie.mockReset();
        headerMocks.setCookie.mockReset();
        vi.resetModules();
        vi.unstubAllGlobals();
    });

    it("logs in through the backend and stores the JWT in a secure HTTP-only cookie", async () => {
        vi.mocked(fetch).mockResolvedValue(Response.json("jwt-1"));

        const { loginAction } = await import("@/app/(results)/auth/actions");

        await expect(
            loginAction({ username: "alice", password: "secret" }),
        ).resolves.toEqual({ authenticated: true, username: "alice" });

        expect(fetch).toHaveBeenCalledWith(
            "https://results.example/api/rest/v1/jwt",
            expect.objectContaining({
                method: "POST",
                headers: expect.objectContaining({
                    "content-type": "application/json",
                }),
            }),
        );
        expect(JSON.parse(String(expectFetchInit().body))).toEqual({
            password: "secret",
            username: "alice",
        });
        expect(headerMocks.setCookie).toHaveBeenCalledWith(
            "wa_results_jwt",
            "jwt-1",
            expect.objectContaining({
                httpOnly: true,
                maxAge: 86_400,
                path: "/",
                sameSite: "lax",
                secure: true,
            }),
        );
    });

    it("does not set a JWT cookie when backend login fails", async () => {
        vi.mocked(fetch).mockResolvedValue(
            Response.json({ error: "bad credentials" }, { status: 401 }),
        );

        const { loginAction } = await import("@/app/(results)/auth/actions");

        await expect(
            loginAction({ username: "alice", password: "wrong" }),
        ).rejects.toThrow("authentication failed");
        expect(headerMocks.setCookie).not.toHaveBeenCalled();
    });

    it("refreshes an existing JWT and replaces the secure cookie", async () => {
        headerMocks.getCookie.mockReturnValue({ value: "jwt-old" });
        vi.mocked(fetch)
            .mockResolvedValueOnce(Response.json("jwt-new"))
            .mockResolvedValueOnce(
                Response.json({
                    authenticated: true,
                    is_owner: false,
                    username: "alice",
                }),
            );

        const { refreshAction } = await import("@/app/(results)/auth/actions");

        await expect(refreshAction()).resolves.toEqual({
            authenticated: true,
            username: "alice",
        });

        expect(fetch).toHaveBeenNthCalledWith(
            1,
            "https://results.example/api/rest/v1/jwt",
            expect.objectContaining({
                headers: expect.objectContaining({
                    authorization: "Bearer jwt-old",
                }),
                method: "GET",
            }),
        );
        expect(headerMocks.setCookie).toHaveBeenCalledWith(
            "wa_results_jwt",
            "jwt-new",
            expect.objectContaining({
                maxAge: 86_400,
                secure: true,
            }),
        );
    });

    it("expires the JWT cookie, notifies the backend, and returns an anonymous session", async () => {
        headerMocks.getCookie.mockReturnValue({ value: "jwt-old" });
        vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));

        const { logoutAction } = await import("@/app/(results)/auth/actions");

        await expect(logoutAction()).resolves.toEqual({
            authenticated: false,
            username: null,
        });

        expect(fetch).toHaveBeenCalledWith(
            "https://results.example/api/rest/v1/auth/logout",
            expect.objectContaining({
                headers: expect.objectContaining({
                    authorization: "Bearer jwt-old",
                }),
                method: "POST",
            }),
        );
        expect(headerMocks.setCookie).toHaveBeenCalledWith(
            "wa_results_jwt",
            "",
            expect.objectContaining({
                maxAge: 0,
                secure: true,
            }),
        );
        expect(headerMocks.setCookie.mock.invocationCallOrder[0]).toBeLessThan(
            vi.mocked(fetch).mock.invocationCallOrder[0]!,
        );
    });

    it("expires the JWT cookie without waiting for backend logout", async () => {
        headerMocks.getCookie.mockReturnValue({ value: "jwt-old" });
        vi.mocked(fetch).mockReturnValue(new Promise<Response>(() => {}));

        const { logoutAction } = await import("@/app/(results)/auth/actions");
        const response = await Promise.race([
            logoutAction(),
            new Promise<"timed out">((resolve) => {
                setTimeout(() => {
                    resolve("timed out");
                }, 50);
            }),
        ]);

        expect(response).not.toBe("timed out");
        expect(response).toEqual({
            authenticated: false,
            username: null,
        });
        expect(headerMocks.setCookie).toHaveBeenCalledWith(
            "wa_results_jwt",
            "",
            expect.objectContaining({
                maxAge: 0,
            }),
        );
        expect(fetch).toHaveBeenCalledWith(
            "https://results.example/api/rest/v1/auth/logout",
            expect.objectContaining({
                headers: expect.objectContaining({
                    authorization: "Bearer jwt-old",
                }),
                method: "POST",
            }),
        );
    });

    it("loads the current session with the JWT cookie", async () => {
        headerMocks.getCookie.mockReturnValue({ value: "jwt-old" });
        vi.mocked(fetch).mockResolvedValue(
            Response.json({
                authenticated: true,
                is_owner: false,
                username: "alice",
            }),
        );

        const { currentSession } = await import("@/app/(results)/auth/actions");

        await expect(currentSession()).resolves.toEqual({
            authenticated: true,
            username: "alice",
        });
        expect(fetch).toHaveBeenCalledWith(
            "https://results.example/api/rest/v1/auth/session",
            expect.objectContaining({
                headers: expect.objectContaining({
                    authorization: "Bearer jwt-old",
                }),
                method: "GET",
            }),
        );
    });

    it.each([404, 501])(
        "still expires the JWT cookie when backend logout returns %i",
        async (status) => {
            headerMocks.getCookie.mockReturnValue({ value: "jwt-old" });
            vi.mocked(fetch).mockResolvedValue(new Response(null, { status }));

            const { logoutAction } =
                await import("@/app/(results)/auth/actions");

            await expect(logoutAction()).resolves.toEqual({
                authenticated: false,
                username: null,
            });
            expect(headerMocks.setCookie).toHaveBeenCalledWith(
                "wa_results_jwt",
                "",
                expect.objectContaining({
                    maxAge: 0,
                }),
            );
        },
    );
});
