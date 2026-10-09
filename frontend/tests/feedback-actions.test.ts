import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { FeedbackPage, FeedbackReport } from "@/lib/contracts";

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

const report: FeedbackReport = {
    id: 3,
    created_at: "2026-10-01T12:00:00Z",
    category: "no_endpoint",
    description: "No endpoint lists sample consent.",
    user_request: "Which samples have withdrawn consent?",
    tools_tried: ["mlwh_search_samples"],
    mcp_server_version: "0.4.0",
    wa_api_version: "1.9.0",
    transport: "stdio",
    client_name: "claude-code",
    client_version: "2.1.0",
    client_user_agent: "",
    remote_addr: "127.0.0.1",
    acknowledged: false,
    acknowledged_at: "",
};

const page: FeedbackPage = { items: [report], total: 51, next_offset: -1 };

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

async function actions() {
    return import("@/app/(results)/feedback/actions");
}

function fetchCall(index = 0): { url: string; init: RequestInit } {
    const call = vi.mocked(fetch).mock.calls[index];

    if (!call) {
        throw new Error(`no fetch call ${index}`);
    }

    return { url: String(call[0]), init: (call[1] ?? {}) as RequestInit };
}

function jsonResponse(body: unknown, status: number): Response {
    return Response.json(body, { status });
}

function text404(): Response {
    return new Response("404 page not found", {
        status: 404,
        headers: { "content-type": "text/plain" },
    });
}

function notFound404(): Response {
    return jsonResponse({ code: "not_found", message: "x" }, 404);
}

function disabled503(): Response {
    return jsonResponse(
        {
            code: "feedback_disabled",
            message: "feedback is disabled on this server",
        },
        503,
    );
}

function unauthorized401(): Response {
    return jsonResponse(
        { code: "unauthorized", message: "admin token required" },
        401,
    );
}

function internal500(): Response {
    return jsonResponse({ code: "internal_error", message: "boom" }, 500);
}

const unavailable = (reason: string) => ({ status: "unavailable", reason });

beforeEach(async () => {
    tokenDir = await mkdtemp(join(tmpdir(), "wa-feedback-actions-"));
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

describe("E3 feedback actions: session, allowlist, input, token checks", () => {
    it("returns unauthenticated with no session and no fetch (test 1)", async () => {
        signedInAs(null);
        const { listFeedbackAction } = await actions();

        await expect(
            listFeedbackAction({ show: "all", category: null, offset: 0 }),
        ).resolves.toEqual({ status: "unauthenticated" });
        await expect(listFeedbackAction(null as never)).resolves.toEqual({
            status: "unauthenticated",
        });
        expect(fetch).not.toHaveBeenCalled();
    });

    it("returns forbidden for a non-admin session and no fetch (test 2)", async () => {
        signedInAs("bob");
        const { listFeedbackAction } = await actions();

        await expect(
            listFeedbackAction({ show: "all", category: null, offset: 0 }),
        ).resolves.toEqual({ status: "forbidden" });
        await expect(
            listFeedbackAction({ show: "x" } as never),
        ).resolves.toEqual({ status: "forbidden" });
        expect(fetch).not.toHaveBeenCalled();
    });

    it("treats the OS user as an admin alongside WA_FEEDBACK_ADMINS", async () => {
        signedInAs("svc");
        await writeToken("T\n");
        vi.mocked(fetch).mockResolvedValue(jsonResponse(page, 200));
        const { listFeedbackAction } = await actions();

        await expect(
            listFeedbackAction({ show: "all", category: null, offset: 0 }),
        ).resolves.toEqual({ status: "ok", page });
    });

    it("returns unavailable/no_token for an admin without a token file (test 3)", async () => {
        signedInAs("alice");
        const {
            deleteFeedbackAction,
            listFeedbackAction,
            setFeedbackAcknowledgedAction,
        } = await actions();

        await expect(
            listFeedbackAction({ show: "all", category: null, offset: 0 }),
        ).resolves.toEqual(unavailable("no_token"));
        await expect(setFeedbackAcknowledgedAction(3, true)).resolves.toEqual(
            unavailable("no_token"),
        );
        await expect(deleteFeedbackAction(3)).resolves.toEqual(
            unavailable("no_token"),
        );
        expect(fetch).not.toHaveBeenCalled();
    });

    it("returns forbidden for non-admin mutations with no fetch (test 9)", async () => {
        signedInAs("bob");
        await writeToken("T");
        const { deleteFeedbackAction, setFeedbackAcknowledgedAction } =
            await actions();

        await expect(setFeedbackAcknowledgedAction(3, true)).resolves.toEqual({
            status: "forbidden",
        });
        await expect(deleteFeedbackAction(3)).resolves.toEqual({
            status: "forbidden",
        });
        expect(fetch).not.toHaveBeenCalled();
    });

    it("returns unauthenticated for a mutation with no session (test 10)", async () => {
        signedInAs(null);
        await writeToken("T");
        const { setFeedbackAcknowledgedAction } = await actions();

        await expect(setFeedbackAcknowledgedAction(3, true)).resolves.toEqual({
            status: "unauthenticated",
        });
        expect(fetch).not.toHaveBeenCalled();
    });

    it("rejects invalid mutation arguments without fetching (test 15)", async () => {
        signedInAs("alice");
        await writeToken("T");
        const { deleteFeedbackAction, setFeedbackAcknowledgedAction } =
            await actions();
        const badIds: unknown[] = [
            "../x",
            -1,
            0,
            1.5,
            Number.MAX_SAFE_INTEGER + 1,
        ];

        for (const id of badIds) {
            await expect(
                setFeedbackAcknowledgedAction(id as number, true),
            ).resolves.toEqual({ status: "invalid_input" });
            await expect(deleteFeedbackAction(id as number)).resolves.toEqual({
                status: "invalid_input",
            });
        }
        await expect(
            setFeedbackAcknowledgedAction(3, "yes" as unknown as boolean),
        ).resolves.toEqual({ status: "invalid_input" });
        expect(fetch).not.toHaveBeenCalled();
    });

    it("rejects invalid list input without fetching (test 16)", async () => {
        signedInAs("alice");
        await writeToken("T");
        const { listFeedbackAction } = await actions();
        const badInputs: unknown[] = [
            { show: "all", category: "bogus", offset: 0 },
            { show: "all", category: "../x", offset: 0 },
            { show: "x", category: null, offset: 0 },
            { show: "all", category: null, offset: -1 },
            { show: "all", category: null, offset: 1.5 },
            null,
        ];

        for (const input of badInputs) {
            await expect(listFeedbackAction(input as never)).resolves.toEqual({
                status: "invalid_input",
            });
        }
        expect(fetch).not.toHaveBeenCalled();
    });

    it("validates arguments before reading the token", async () => {
        signedInAs("alice");
        const {
            deleteFeedbackAction,
            listFeedbackAction,
            setFeedbackAcknowledgedAction,
        } = await actions();

        await expect(listFeedbackAction(null as never)).resolves.toEqual({
            status: "invalid_input",
        });
        await expect(
            setFeedbackAcknowledgedAction(3, "yes" as unknown as boolean),
        ).resolves.toEqual({ status: "invalid_input" });
        await expect(deleteFeedbackAction("../x" as never)).resolves.toEqual({
            status: "invalid_input",
        });
        expect(fetch).not.toHaveBeenCalled();
    });

    it("checks auth before input validation (test 17)", async () => {
        signedInAs(null);
        const { deleteFeedbackAction, setFeedbackAcknowledgedAction } =
            await actions();

        await expect(deleteFeedbackAction("../x" as never)).resolves.toEqual({
            status: "unauthenticated",
        });
        signedInAs("bob");
        await expect(
            setFeedbackAcknowledgedAction("../x" as never, true),
        ).resolves.toEqual({ status: "forbidden" });
        expect(fetch).not.toHaveBeenCalled();
    });
});

describe("E3 feedback actions: requests to wa", () => {
    beforeEach(async () => {
        signedInAs("alice");
        await writeToken("T\n");
    });

    it("lists unacknowledged reports in a category with the token (test 4)", async () => {
        vi.mocked(fetch).mockResolvedValue(jsonResponse(page, 200));
        const { listFeedbackAction } = await actions();

        await expect(
            listFeedbackAction({
                show: "unacknowledged",
                category: "no_endpoint",
                offset: 50,
            }),
        ).resolves.toEqual({ status: "ok", page });

        expect(fetch).toHaveBeenCalledTimes(1);
        const { url, init } = fetchCall();
        expect(url).toBe(
            `${base}/feedback?limit=50&offset=50&acknowledged=false&category=no_endpoint`,
        );
        expect(init.method ?? "GET").toBe("GET");
        expect(init.cache).toBe("no-store");
        expect(new Headers(init.headers).get("authorization")).toBe("Bearer T");
    });

    it("lists all reports without filters (test 5)", async () => {
        vi.mocked(fetch).mockResolvedValue(jsonResponse(page, 200));
        const { listFeedbackAction } = await actions();

        await expect(
            listFeedbackAction({ show: "all", category: null, offset: 0 }),
        ).resolves.toEqual({ status: "ok", page });
        expect(fetchCall().url).toBe(`${base}/feedback?limit=50&offset=0`);
    });

    it("PATCHes acknowledgement with a JSON body (test 7)", async () => {
        vi.mocked(fetch).mockResolvedValue(
            jsonResponse({ ...report, acknowledged: true }, 200),
        );
        const { setFeedbackAcknowledgedAction } = await actions();

        await expect(setFeedbackAcknowledgedAction(3, true)).resolves.toEqual({
            status: "ok",
        });

        const { url, init } = fetchCall();
        expect(url).toBe(`${base}/feedback/3`);
        expect(init.method).toBe("PATCH");
        expect(init.body).toBe('{"acknowledged":true}');
        expect(init.cache).toBe("no-store");
        const headers = new Headers(init.headers);
        expect(headers.get("authorization")).toBe("Bearer T");
        expect(headers.get("content-type")).toBe("application/json");
    });

    it("PATCHes un-acknowledgement for another id", async () => {
        vi.mocked(fetch).mockResolvedValue(jsonResponse(report, 200));
        const { setFeedbackAcknowledgedAction } = await actions();

        await expect(setFeedbackAcknowledgedAction(12, false)).resolves.toEqual(
            { status: "ok" },
        );
        const { url, init } = fetchCall();
        expect(url).toBe(`${base}/feedback/12`);
        expect(init.body).toBe('{"acknowledged":false}');
    });

    it("maps a not_found PATCH to not_found (test 7)", async () => {
        vi.mocked(fetch).mockResolvedValue(notFound404());
        const { setFeedbackAcknowledgedAction } = await actions();

        await expect(setFeedbackAcknowledgedAction(3, true)).resolves.toEqual({
            status: "not_found",
        });
    });

    it("DELETEs a report and accepts 204 (test 8)", async () => {
        vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
        const { deleteFeedbackAction } = await actions();

        await expect(deleteFeedbackAction(3)).resolves.toEqual({
            status: "ok",
        });

        const { url, init } = fetchCall();
        expect(url).toBe(`${base}/feedback/3`);
        expect(init.method).toBe("DELETE");
        expect(init.cache).toBe("no-store");
        expect(new Headers(init.headers).get("authorization")).toBe("Bearer T");
    });

    it("maps a not_found DELETE to not_found", async () => {
        vi.mocked(fetch).mockResolvedValue(notFound404());
        const { deleteFeedbackAction } = await actions();

        await expect(deleteFeedbackAction(3)).resolves.toEqual({
            status: "not_found",
        });
    });
});

describe("E3 feedback actions: backend error mapping", () => {
    beforeEach(async () => {
        signedInAs("alice");
        await writeToken("T");
    });

    const listInput = { show: "all", category: null, offset: 0 } as const;

    it("maps 503 feedback_disabled and 401 on list (test 6)", async () => {
        const { listFeedbackAction } = await actions();

        vi.mocked(fetch).mockResolvedValueOnce(disabled503());
        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("feedback_disabled"),
        );
        vi.mocked(fetch).mockResolvedValueOnce(unauthorized401());
        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("token_rejected"),
        );
    });

    it("maps 401 on mutations to token_rejected", async () => {
        const { deleteFeedbackAction, setFeedbackAcknowledgedAction } =
            await actions();

        vi.mocked(fetch).mockResolvedValueOnce(unauthorized401());
        await expect(setFeedbackAcknowledgedAction(3, true)).resolves.toEqual(
            unavailable("token_rejected"),
        );
        vi.mocked(fetch).mockResolvedValueOnce(unauthorized401());
        await expect(deleteFeedbackAction(3)).resolves.toEqual(
            unavailable("token_rejected"),
        );
    });

    it("maps 503 feedback_disabled on delete and PATCH (test 11)", async () => {
        const { deleteFeedbackAction, setFeedbackAcknowledgedAction } =
            await actions();

        vi.mocked(fetch).mockResolvedValueOnce(disabled503());
        await expect(deleteFeedbackAction(3)).resolves.toEqual(
            unavailable("feedback_disabled"),
        );
        vi.mocked(fetch).mockResolvedValueOnce(disabled503());
        await expect(setFeedbackAcknowledgedAction(3, true)).resolves.toEqual(
            unavailable("feedback_disabled"),
        );
    });

    it("maps a rejecting fetch, 500, and other 503s to backend_error (test 12)", async () => {
        const { listFeedbackAction } = await actions();

        vi.mocked(fetch).mockRejectedValueOnce(new TypeError("fetch failed"));
        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("backend_error"),
        );
        vi.mocked(fetch).mockResolvedValueOnce(internal500());
        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("backend_error"),
        );
        vi.mocked(fetch).mockResolvedValueOnce(
            jsonResponse({ code: "unavailable", message: "busy" }, 503),
        );
        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("backend_error"),
        );
        vi.mocked(fetch).mockResolvedValueOnce(
            jsonResponse({ code: "feedback_disabled", message: "x" }, 500),
        );
        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("backend_error"),
        );
        vi.mocked(fetch).mockResolvedValueOnce(
            jsonResponse({ items: "nope" }, 200),
        );
        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("backend_error"),
        );
    });

    it("maps an unconfigured MLWH backend to backend_error without fetch (test 13)", async () => {
        vi.stubEnv("WA_MLWH_BACKEND_URL", "");
        const {
            deleteFeedbackAction,
            listFeedbackAction,
            setFeedbackAcknowledgedAction,
        } = await actions();

        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("backend_error"),
        );
        await expect(setFeedbackAcknowledgedAction(3, true)).resolves.toEqual(
            unavailable("backend_error"),
        );
        await expect(deleteFeedbackAction(3)).resolves.toEqual(
            unavailable("backend_error"),
        );
        expect(fetch).not.toHaveBeenCalled();
    });

    it("maps a gin text 404 to unsupported on every action (test 14)", async () => {
        const {
            deleteFeedbackAction,
            listFeedbackAction,
            setFeedbackAcknowledgedAction,
        } = await actions();

        vi.mocked(fetch).mockResolvedValueOnce(text404());
        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("unsupported"),
        );
        vi.mocked(fetch).mockResolvedValueOnce(text404());
        await expect(setFeedbackAcknowledgedAction(3, true)).resolves.toEqual(
            unavailable("unsupported"),
        );
        vi.mocked(fetch).mockResolvedValueOnce(text404());
        await expect(deleteFeedbackAction(3)).resolves.toEqual(
            unavailable("unsupported"),
        );
    });

    it("maps any 404 on list, even a not_found envelope, to unsupported", async () => {
        vi.mocked(fetch).mockResolvedValueOnce(notFound404());
        const { listFeedbackAction } = await actions();

        await expect(listFeedbackAction(listInput)).resolves.toEqual(
            unavailable("unsupported"),
        );
    });

    it("maps a mutation 404 with another JSON code to unsupported", async () => {
        const { deleteFeedbackAction } = await actions();

        vi.mocked(fetch).mockResolvedValueOnce(
            jsonResponse({ code: "no_route", message: "x" }, 404),
        );
        await expect(deleteFeedbackAction(3)).resolves.toEqual(
            unavailable("unsupported"),
        );
    });
});

describe("E3 feedback actions never return the token (test 18)", () => {
    const secret = "tok-7f3a9c";
    const responses: Array<[string, () => Response | Error]> = [
        ["ok", () => new Response(null, { status: 204 })],
        ["401", unauthorized401],
        ["404 text", text404],
        ["404 not_found", notFound404],
        ["500", internal500],
        ["503 feedback_disabled", disabled503],
        ["rejecting fetch", () => new TypeError(`fetch failed ${secret}`)],
    ];

    beforeEach(async () => {
        signedInAs("alice");
        await writeToken(secret);
    });

    it.each(responses)(
        "keeps the token out of states for %s",
        async (name, make) => {
            const {
                deleteFeedbackAction,
                listFeedbackAction,
                setFeedbackAcknowledgedAction,
            } = await actions();
            const respond = (okBody: unknown) => {
                const value = make();

                if (value instanceof Error) {
                    return Promise.reject(value);
                }
                if (name === "ok" && okBody !== null) {
                    return Promise.resolve(jsonResponse(okBody, 200));
                }

                return Promise.resolve(value);
            };

            vi.mocked(fetch).mockImplementationOnce(() => respond(page));
            const listed = await listFeedbackAction({
                show: "all",
                category: null,
                offset: 0,
            });
            vi.mocked(fetch).mockImplementationOnce(() => respond(report));
            const patched = await setFeedbackAcknowledgedAction(3, true);
            vi.mocked(fetch).mockImplementationOnce(() => respond(null));
            const deleted = await deleteFeedbackAction(3);

            expect(fetch).toHaveBeenCalledTimes(3);
            expect(
                new Headers(fetchCall(0).init.headers).get("authorization"),
            ).toBe(`Bearer ${secret}`);
            for (const state of [listed, patched, deleted]) {
                expect(state.status).not.toBe("invalid_input");
                expect(JSON.stringify(state)).not.toContain(secret);
            }
        },
    );
});
