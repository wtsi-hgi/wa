import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { type UsageSummaryState } from "@/app/(results)/usage/actions";
import { usageSummarySchema, type UsageWindow } from "@/lib/contracts";

import usageSummaryA3Fixture from "./fixtures/usage-summary-a3-24h.json";

const mocks = vi.hoisted(() => ({
    currentSession: vi.fn(),
    userInfo: vi.fn<() => { username: string }>(),
}));

vi.mock("@/app/(results)/auth/actions", () => ({
    currentSession: mocks.currentSession,
}));

vi.mock("node:os", async (importOriginal) => {
    const actual = await importOriginal<typeof import("node:os")>();

    return {
        ...actual,
        userInfo: mocks.userInfo,
    };
});

const base = "http://mlwh.example/api";

let tokenDir: string;
let tokenPath: string;

async function writeToken(contents: string): Promise<void> {
    await writeFile(tokenPath, contents);
}

function signedInAs(username: string | null): void {
    mocks.currentSession.mockResolvedValue({
        authenticated: username !== null,
        username,
    });
}

async function getUsageSummary(window: UsageWindow) {
    const { getUsageSummaryAction } =
        await import("@/app/(results)/usage/actions");

    return getUsageSummaryAction(window);
}

function onlyFetchCall(): { url: string; headers: Headers } {
    const calls = vi.mocked(fetch).mock.calls;

    expect(calls).toHaveLength(1);

    const [url, init] = calls[0];

    return {
        url: String(url),
        headers: new Headers((init as RequestInit | undefined)?.headers),
    };
}

function jsonResponse(body: unknown, status: number): Response {
    return Response.json(body, { status });
}

const unavailable = (reason: string) => ({ status: "unavailable", reason });

beforeEach(async () => {
    tokenDir = await mkdtemp(join(tmpdir(), "wa-usage-actions-"));
    tokenPath = join(tokenDir, "server.token");
    vi.stubEnv("WA_MLWH_BACKEND_URL", base);
    vi.stubEnv("WA_MLWH_SERVER_TOKEN", tokenPath);
    vi.stubEnv("WA_FEEDBACK_ADMINS", "alice");
    mocks.userInfo.mockReset().mockReturnValue({ username: "svc" });
    mocks.currentSession.mockReset();
    vi.stubGlobal("fetch", vi.fn());
});

afterEach(async () => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    vi.resetModules();
    await rm(tokenDir, { recursive: true, force: true });
});

describe("E3 getUsageSummaryAction: session, allowlist, window, token checks", () => {
    it("returns unauthenticated with no session and no fetch (test 1)", async () => {
        signedInAs(null);
        await writeToken("T");

        await expect(getUsageSummary("24h")).resolves.toEqual({
            status: "unauthenticated",
        });
        expect(fetch).not.toHaveBeenCalled();
    });

    it("returns forbidden for a session not on the allowlist (test 2)", async () => {
        signedInAs("bob");
        await writeToken("T");

        await expect(getUsageSummary("24h")).resolves.toEqual({
            status: "forbidden",
        });
        expect(fetch).not.toHaveBeenCalled();
    });

    it("returns unavailable/no_token for an admin with no token file (test 3)", async () => {
        signedInAs("alice");

        await expect(getUsageSummary("24h")).resolves.toEqual(
            unavailable("no_token"),
        );
        expect(fetch).not.toHaveBeenCalled();
    });

    it.each([["1h"], [null]])(
        "returns invalid_input for window %s with no fetch (test 5)",
        async (window) => {
            signedInAs("alice");
            await writeToken("T");

            await expect(
                getUsageSummary(window as unknown as UsageWindow),
            ).resolves.toEqual({ status: "invalid_input" });
            expect(fetch).not.toHaveBeenCalled();
        },
    );

    it("checks the window before the token file", async () => {
        signedInAs("alice");

        await expect(
            getUsageSummary("1h" as unknown as UsageWindow),
        ).resolves.toEqual({ status: "invalid_input" });
    });
});

describe("E3 getUsageSummaryAction: fetching the summary", () => {
    it("fetches the window with the admin token and returns the parsed summary (test 4)", async () => {
        signedInAs("alice");
        await writeToken("T\n");
        vi.mocked(fetch).mockResolvedValue(
            jsonResponse(usageSummaryA3Fixture, 200),
        );

        await expect(getUsageSummary("24h")).resolves.toEqual({
            status: "ok",
            summary: usageSummarySchema.parse(usageSummaryA3Fixture),
        });

        const { url, headers } = onlyFetchCall();

        expect(url).toBe(`${base}/usage?window=24h`);
        expect(headers.get("authorization")).toBe("Bearer T");
        expect(headers.get("user-agent")).toBe("wa-web");
        expect(vi.mocked(fetch).mock.calls[0][1]).toMatchObject({
            cache: "no-store",
        });
    });

    it("sends the gate's session user as the usage identity, calling currentSession once", async () => {
        signedInAs("alice");
        await writeToken("T");
        vi.mocked(fetch).mockResolvedValue(
            jsonResponse(usageSummaryA3Fixture, 200),
        );

        await getUsageSummary("7d");

        expect(onlyFetchCall().headers.get("x-wa-client-user")).toBe("alice");
        expect(mocks.currentSession).toHaveBeenCalledTimes(1);
    });

    it.each<[string, () => Promise<Response>, UsageSummaryState]>([
        [
            "503 usage_disabled",
            async () =>
                jsonResponse(
                    {
                        code: "usage_disabled",
                        message: "usage tracking is disabled on this server",
                    },
                    503,
                ),
            unavailable("usage_disabled") as UsageSummaryState,
        ],
        [
            "401",
            async () =>
                jsonResponse(
                    { code: "unauthorized", message: "admin token required" },
                    401,
                ),
            unavailable("token_rejected") as UsageSummaryState,
        ],
        [
            "404 plain text",
            async () =>
                new Response("404 page not found", {
                    status: 404,
                    headers: { "content-type": "text/plain" },
                }),
            unavailable("unsupported") as UsageSummaryState,
        ],
        [
            "500",
            async () =>
                jsonResponse(
                    {
                        code: "internal_error",
                        message: "could not summarise usage",
                    },
                    500,
                ),
            unavailable("backend_error") as UsageSummaryState,
        ],
        [
            "a rejecting fetch",
            async () => {
                throw new TypeError("fetch failed");
            },
            unavailable("backend_error") as UsageSummaryState,
        ],
        [
            "503 with another code",
            async () =>
                jsonResponse(
                    {
                        code: "feedback_disabled",
                        message: "feedback is disabled on this server",
                    },
                    503,
                ),
            unavailable("backend_error") as UsageSummaryState,
        ],
        [
            "a 200 body that breaks the contract",
            async () =>
                jsonResponse({ ...usageSummaryA3Fixture, window: "1h" }, 200),
            unavailable("backend_error") as UsageSummaryState,
        ],
    ])(
        "maps %s to its unavailable reason (test 6)",
        async (_, respond, want) => {
            signedInAs("alice");
            await writeToken("T");
            vi.mocked(fetch).mockImplementation(respond);

            await expect(getUsageSummary("24h")).resolves.toEqual(want);
        },
    );

    it("returns backend_error when the MLWH backend URL is unset", async () => {
        signedInAs("alice");
        await writeToken("T");
        vi.stubEnv("WA_MLWH_BACKEND_URL", "");

        await expect(getUsageSummary("24h")).resolves.toEqual(
            unavailable("backend_error"),
        );
        expect(fetch).not.toHaveBeenCalled();
    });
});

describe("E3 getUsageSummaryAction: token secrecy", () => {
    it("never returns the token contents in any state (test 7)", async () => {
        const secret = "tok-7f3a9c";
        const responses: Array<() => Promise<Response>> = [
            async () => jsonResponse(usageSummaryA3Fixture, 200),
            async () =>
                jsonResponse(
                    { code: "usage_disabled", message: "disabled" },
                    503,
                ),
            async () =>
                jsonResponse({ code: "unauthorized", message: secret }, 401),
            async () =>
                new Response(`404 ${secret}`, {
                    status: 404,
                    headers: { "content-type": "text/plain" },
                }),
            async () =>
                jsonResponse({ code: "internal_error", message: secret }, 500),
            async () => {
                throw new TypeError(`fetch failed ${secret}`);
            },
        ];

        signedInAs("alice");
        await writeToken(secret);

        for (const respond of responses) {
            vi.mocked(fetch).mockReset().mockImplementation(respond);

            const state = await getUsageSummary("24h");

            expect(onlyFetchCall().headers.get("authorization")).toBe(
                `Bearer ${secret}`,
            );
            expect(JSON.stringify(state)).not.toContain(secret);
        }

        vi.mocked(fetch).mockReset();

        for (const [username, window] of [
            [null, "24h"],
            ["bob", "24h"],
            ["alice", "1h"],
        ] as const) {
            signedInAs(username);

            const state = await getUsageSummary(window as UsageWindow);

            expect(JSON.stringify(state)).not.toContain(secret);
        }
    });
});
