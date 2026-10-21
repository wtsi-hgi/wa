import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

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

const currentFreshness = {
    status: "failing",
    stale_after_seconds: 7200,
    tables: [
        {
            table: "sample",
            high_water: "2026-10-21T09:00:00Z",
            last_run: "2026-10-21T09:30:00Z",
            ever_synced: true,
            last_success_at: "2026-10-21T09:30:00Z",
            last_error: "",
            last_error_at: "",
            blocked_by: [],
            status: "ok",
        },
        {
            table: "seq_product_irods_locations",
            high_water: "",
            last_run: "",
            ever_synced: false,
            last_success_at: "",
            last_error: "skipped: dependency sync failed: iseq_product_metrics",
            last_error_at: "2026-10-21T09:31:00Z",
            blocked_by: ["iseq_product_metrics"],
            status: "failing",
        },
    ],
};

const oldServerFreshness = {
    tables: [
        {
            table: "sample",
            high_water: "2026-10-21T09:00:00Z",
            last_run: "2026-10-21T09:30:00Z",
            ever_synced: true,
        },
    ],
};

function signedInAs(username: string | null): void {
    mocks.currentSession.mockResolvedValue({
        authenticated: username !== null,
        username,
    });
}

async function getSyncStatus() {
    const { getMLWHSyncStatusAction } =
        await import("@/app/(results)/mlwh-sync/actions");

    return getMLWHSyncStatusAction();
}

beforeEach(() => {
    vi.stubEnv("WA_MLWH_BACKEND_URL", base);
    vi.stubEnv("WA_MLWH_SERVER_TOKEN", "/nonexistent/server.token");
    vi.stubEnv("WA_FEEDBACK_ADMINS", "alice");
    mocks.userInfo.mockReset().mockReturnValue({ username: "svc" });
    mocks.currentSession.mockReset();
    vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    vi.resetModules();
});

describe("getMLWHSyncStatusAction: admin gate", () => {
    it("returns unauthenticated with no session and no fetch", async () => {
        signedInAs(null);

        await expect(getSyncStatus()).resolves.toEqual({
            status: "unauthenticated",
        });
        expect(fetch).not.toHaveBeenCalled();
    });

    it("returns forbidden for a session not on the allowlist and no fetch", async () => {
        signedInAs("bob");

        await expect(getSyncStatus()).resolves.toEqual({
            status: "forbidden",
        });
        expect(fetch).not.toHaveBeenCalled();
    });
});

describe("getMLWHSyncStatusAction: fetching /freshness", () => {
    it("fetches /freshness uncached without the admin token, and needs no token file", async () => {
        signedInAs("alice");
        vi.mocked(fetch).mockResolvedValue(
            Response.json(currentFreshness, { status: 200 }),
        );

        const state = await getSyncStatus();

        expect(state.status).toBe("ok");
        expect(state.status === "ok" && state.freshness.status).toBe("failing");
        expect(
            state.status === "ok" && state.freshness.tables[1].blocked_by,
        ).toEqual(["iseq_product_metrics"]);

        const calls = vi.mocked(fetch).mock.calls;

        expect(calls).toHaveLength(1);
        expect(String(calls[0][0])).toBe(`${base}/freshness`);

        const init = calls[0][1] as RequestInit;
        const headers = new Headers(init.headers);

        expect(init).toMatchObject({ cache: "no-store" });
        expect(headers.get("authorization")).toBeNull();
        expect(headers.get("x-wa-client-user")).toBe("alice");
    });

    it("reports unsupported for a server whose /freshness has no status", async () => {
        signedInAs("alice");
        vi.mocked(fetch).mockResolvedValue(
            Response.json(oldServerFreshness, { status: 200 }),
        );

        await expect(getSyncStatus()).resolves.toEqual({
            status: "unsupported",
        });
    });

    it("reports unavailable when the server answers with an error", async () => {
        signedInAs("alice");
        vi.mocked(fetch).mockResolvedValue(
            Response.json(
                { code: "upstream_impaired", message: "x" },
                { status: 503 },
            ),
        );

        await expect(getSyncStatus()).resolves.toEqual({
            status: "unavailable",
        });
    });

    it("reports invalid_response when a status-reporting server omits a table's outcome fields", async () => {
        signedInAs("alice");
        vi.mocked(fetch).mockResolvedValue(
            Response.json(
                {
                    ...currentFreshness,
                    tables: [{ ...oldServerFreshness.tables[0], status: "ok" }],
                },
                { status: 200 },
            ),
        );

        await expect(getSyncStatus()).resolves.toEqual({
            status: "invalid_response",
        });
    });

    it("reports invalid_response when a table field has the wrong type", async () => {
        signedInAs("alice");
        vi.mocked(fetch).mockResolvedValue(
            Response.json(
                {
                    ...currentFreshness,
                    tables: [{ ...currentFreshness.tables[0], last_error: 7 }],
                },
                { status: 200 },
            ),
        );

        await expect(getSyncStatus()).resolves.toEqual({
            status: "invalid_response",
        });
    });

    it("reports invalid_response when /freshness is not a freshness document", async () => {
        signedInAs("alice");
        vi.mocked(fetch).mockResolvedValue(
            Response.json({ tables: "none" }, { status: 200 }),
        );

        await expect(getSyncStatus()).resolves.toEqual({
            status: "invalid_response",
        });
    });

    it("reports unavailable when the server cannot be reached", async () => {
        signedInAs("alice");
        vi.mocked(fetch).mockRejectedValue(new TypeError("fetch failed"));

        await expect(getSyncStatus()).resolves.toEqual({
            status: "unavailable",
        });
    });
});
