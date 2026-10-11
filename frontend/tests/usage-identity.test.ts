import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    cookies: vi.fn(),
    currentSession: vi.fn(),
}));

vi.mock("next/headers", () => ({
    cookies: mocks.cookies,
}));

vi.mock("@/app/(results)/auth/actions", () => ({
    currentSession: mocks.currentSession,
}));

import {
    mlwhIdentityHeaders,
    usageClientId,
    usageSessionCookieName,
} from "@/lib/usage-identity";

const abcClientId = "ba7816bf8f01cfea414140de5dae2223";

function cookieStoreWith(values: Record<string, string>) {
    return {
        get: (name: string) =>
            name in values ? { name, value: values[name] } : undefined,
    };
}

beforeEach(() => {
    mocks.cookies.mockReset();
    mocks.currentSession.mockReset();
});

afterEach(() => {
    vi.restoreAllMocks();
});

describe("E1 usageClientId", () => {
    it("returns the first 32 hex chars of SHA-256 of the cookie value (test 5)", () => {
        expect(usageClientId("abc")).toBe(abcClientId);
    });
});

describe("E1 mlwhIdentityHeaders", () => {
    it("returns the hashed client ID and the session user (test 6)", async () => {
        mocks.cookies.mockResolvedValue(
            cookieStoreWith({ [usageSessionCookieName]: "abc" }),
        );
        mocks.currentSession.mockResolvedValue({
            authenticated: true,
            username: "alice",
        });

        await expect(mlwhIdentityHeaders()).resolves.toEqual({
            "x-wa-client-id": abcClientId,
            "x-wa-client-user": "alice",
        });
    });

    it("omits the user for an anonymous session (test 6)", async () => {
        mocks.cookies.mockResolvedValue(
            cookieStoreWith({ [usageSessionCookieName]: "abc" }),
        );
        mocks.currentSession.mockResolvedValue({
            authenticated: false,
            username: null,
        });

        await expect(mlwhIdentityHeaders()).resolves.toEqual({
            "x-wa-client-id": abcClientId,
        });
    });

    it("skips currentSession when username is null (test 6)", async () => {
        mocks.cookies.mockResolvedValue(
            cookieStoreWith({ [usageSessionCookieName]: "abc" }),
        );

        await expect(mlwhIdentityHeaders(null)).resolves.toEqual({
            "x-wa-client-id": abcClientId,
        });
        expect(mocks.currentSession).not.toHaveBeenCalled();
    });

    it("uses a given username without calling currentSession", async () => {
        mocks.cookies.mockResolvedValue(cookieStoreWith({}));

        await expect(mlwhIdentityHeaders("bob")).resolves.toEqual({
            "x-wa-client-user": "bob",
        });
        expect(mocks.currentSession).not.toHaveBeenCalled();
    });

    it("resolves {} when cookies() throws and currentSession rejects (test 7)", async () => {
        mocks.cookies.mockImplementation(() => {
            throw new Error("outside a request scope");
        });
        mocks.currentSession.mockRejectedValue(new Error("backend down"));

        await expect(mlwhIdentityHeaders()).resolves.toEqual({});
    });

    it("keeps the client ID when currentSession rejects (test 7)", async () => {
        mocks.cookies.mockResolvedValue(
            cookieStoreWith({ [usageSessionCookieName]: "abc" }),
        );
        mocks.currentSession.mockRejectedValue(new Error("backend down"));

        await expect(mlwhIdentityHeaders()).resolves.toEqual({
            "x-wa-client-id": abcClientId,
        });
    });

    it("keeps the session user when cookies() throws (test 7)", async () => {
        mocks.cookies.mockImplementation(() => {
            throw new Error("outside a request scope");
        });
        mocks.currentSession.mockResolvedValue({
            authenticated: true,
            username: "alice",
        });

        await expect(mlwhIdentityHeaders()).resolves.toEqual({
            "x-wa-client-user": "alice",
        });
    });

    it("replaces each non-ASCII UTF-8 byte of the user with ? (test 7)", async () => {
        mocks.cookies.mockResolvedValue(cookieStoreWith({}));

        await expect(mlwhIdentityHeaders("zoë")).resolves.toEqual({
            "x-wa-client-user": "zo??",
        });
    });

    it("sanitises control bytes and caps the user at 128 bytes", async () => {
        mocks.cookies.mockResolvedValue(cookieStoreWith({}));

        await expect(mlwhIdentityHeaders("café\n")).resolves.toEqual({
            "x-wa-client-user": "caf???",
        });
        await expect(mlwhIdentityHeaders("u".repeat(200))).resolves.toEqual({
            "x-wa-client-user": "u".repeat(128),
        });
    });
});
