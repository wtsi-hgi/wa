import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

function makeRequest(
    path: string,
    init: ConstructorParameters<typeof NextRequest>[1] = {},
): NextRequest {
    return new NextRequest(`http://localhost${path}`, init);
}

function setCookieHeader(response: Response): string {
    return response.headers.get("set-cookie") ?? "";
}

describe("E1 auth API route handlers", () => {
    beforeEach(() => {
        process.env.WA_RESULTS_BACKEND_URL = "https://results.example/api";
        delete process.env.WA_RESULTS_BACKEND_CA_CERT;
        vi.stubGlobal("fetch", vi.fn());
    });

    afterEach(() => {
        delete process.env.WA_RESULTS_BACKEND_URL;
        delete process.env.WA_RESULTS_BACKEND_CA_CERT;
        vi.resetModules();
        vi.unstubAllGlobals();
    });

    it("proxies login credentials to the backend and returns a secure auth cookie", async () => {
        vi.mocked(fetch).mockResolvedValue(Response.json("jwt-1"));

        const { POST } = await import("@/app/api/auth/login/route");
        const response = await POST(
            makeRequest("/api/auth/login", {
                body: JSON.stringify({
                    password: "secret",
                    username: "alice",
                }),
                headers: { "content-type": "application/json" },
                method: "POST",
            }),
        );

        expect(response.status).toBe(200);
        await expect(response.json()).resolves.toEqual({
            authenticated: true,
            username: "alice",
        });
        expect(fetch).toHaveBeenCalledWith(
            "https://results.example/api/rest/v1/jwt",
            expect.objectContaining({ method: "POST" }),
        );
        expect(setCookieHeader(response)).toContain("wa_results_jwt=jwt-1");
        expect(setCookieHeader(response)).toContain("HttpOnly");
        expect(setCookieHeader(response)).toContain("Secure");
        expect(setCookieHeader(response)).toMatch(/SameSite=Lax/i);
    });

    it("refreshes the JWT cookie without exposing the token in the response body", async () => {
        vi.mocked(fetch)
            .mockResolvedValueOnce(Response.json("jwt-new"))
            .mockResolvedValueOnce(
                Response.json({
                    authenticated: true,
                    is_owner: false,
                    username: "alice",
                }),
            );

        const { POST } = await import("@/app/api/auth/refresh/route");
        const response = await POST(
            makeRequest("/api/auth/refresh", {
                headers: { cookie: "wa_results_jwt=jwt-old" },
                method: "POST",
            }),
        );

        expect(response.status).toBe(200);
        await expect(response.json()).resolves.toEqual({
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
        expect(setCookieHeader(response)).toContain("wa_results_jwt=jwt-new");
    });

    it("expires the JWT cookie even when the backend logout endpoint is unavailable", async () => {
        vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 501 }));

        const { POST } = await import("@/app/api/auth/logout/route");
        const response = await POST(
            makeRequest("/api/auth/logout", {
                headers: { cookie: "wa_results_jwt=jwt-old" },
                method: "POST",
            }),
        );

        expect(response.status).toBe(200);
        await expect(response.json()).resolves.toEqual({
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
        expect(setCookieHeader(response)).toContain("wa_results_jwt=");
        expect(setCookieHeader(response)).toContain("Max-Age=0");
    });

    it("expires the JWT cookie immediately when the backend logout endpoint does not answer", async () => {
        vi.mocked(fetch).mockReturnValue(new Promise<Response>(() => {}));

        const { POST } = await import("@/app/api/auth/logout/route");
        const response = await Promise.race([
            POST(
                makeRequest("/api/auth/logout", {
                    headers: { cookie: "wa_results_jwt=jwt-old" },
                    method: "POST",
                }),
            ),
            new Promise<"timed out">((resolve) => {
                setTimeout(() => {
                    resolve("timed out");
                }, 50);
            }),
        ]);

        expect(response).not.toBe("timed out");
        expect(response.status).toBe(200);
        await expect(response.json()).resolves.toEqual({
            authenticated: false,
            username: null,
        });
        expect(setCookieHeader(response)).toContain("wa_results_jwt=");
        expect(setCookieHeader(response)).toContain("Max-Age=0");
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
});
