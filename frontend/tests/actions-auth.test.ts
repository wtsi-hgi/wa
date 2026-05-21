import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ResultSet, StatsResult } from "@/lib/contracts";

const headerMocks = vi.hoisted(() => ({
    cookies: vi.fn(),
    getCookie: vi.fn(),
}));

vi.mock("next/headers", () => ({
    cookies: headerMocks.cookies,
}));

const resultSet: ResultSet = {
    id: "abc",
    pipeline_identifier: "pipeline-1",
    run_key: "run-1",
    requester: "alice",
    operator: "alice",
    command: "wa run",
    pipeline_name: "example",
    pipeline_version: "1.0.0",
    output_directory: "/out",
    metadata: {},
    created_at: "2026-05-21T00:00:00Z",
    updated_at: "2026-05-21T00:00:00Z",
};

const lockedBody = {
    error: "locked",
    locked: true,
    result_id: "abc",
    message: "You do not have access to this result set",
};

function statsResult(): StatsResult {
    return {
        total: 1,
        recent: [resultSet],
        daily: [],
        pipelines: [],
    };
}

function expectAuthorization(callIndex: number, token: string): void {
    const init = vi.mocked(fetch).mock.calls[callIndex]?.[1] as
        | RequestInit
        | undefined;

    expect(new Headers(init?.headers).get("authorization")).toBe(
        `Bearer ${token}`,
    );
}

function expectNoAuthorization(callIndex: number): void {
    const init = vi.mocked(fetch).mock.calls[callIndex]?.[1] as
        | RequestInit
        | undefined;

    expect(new Headers(init?.headers).get("authorization")).toBeNull();
}

describe("E2 authenticated results server actions", () => {
    beforeEach(() => {
        process.env.WA_RESULTS_BACKEND_URL = "https://results.example/api";
        headerMocks.cookies.mockResolvedValue({
            get: headerMocks.getCookie,
        });
        vi.stubGlobal("fetch", vi.fn());
    });

    afterEach(() => {
        delete process.env.WA_RESULTS_BACKEND_URL;
        headerMocks.cookies.mockReset();
        headerMocks.getCookie.mockReset();
        vi.resetModules();
        vi.unstubAllGlobals();
    });

    it("fetches result details from the authenticated endpoint when the JWT cookie exists", async () => {
        headerMocks.getCookie.mockReturnValue({ value: "jwt-1" });
        vi.mocked(fetch).mockResolvedValue(Response.json(resultSet));

        const { fetchResult } = await import("@/app/(results)/actions");

        await expect(fetchResult("abc")).resolves.toEqual(resultSet);

        expect(fetch).toHaveBeenCalledWith(
            "https://results.example/api/rest/v1/auth/results/abc",
            expect.any(Object),
        );
        expectAuthorization(0, "jwt-1");
    });

    it("uses the public detail endpoint without Authorization and preserves locked JSON when no JWT cookie exists", async () => {
        headerMocks.getCookie.mockReturnValue(undefined);
        vi.mocked(fetch).mockResolvedValue(
            Response.json(lockedBody, { status: 403 }),
        );

        const { fetchResult } = await import("@/app/(results)/actions");
        const result = fetchResult("abc");

        await expect(result).rejects.toMatchObject({
            name: "BackendRequestError",
            status: 403,
            body: lockedBody,
        });
        expect(fetch).toHaveBeenCalledWith(
            "https://results.example/api/rest/v1/results/abc",
        );
        expectNoAuthorization(0);
    });

    it("loads anonymous landing page data from public search and stats endpoints", async () => {
        vi.mocked(fetch)
            .mockResolvedValueOnce(Response.json([resultSet]))
            .mockResolvedValueOnce(Response.json(statsResult()));

        const { fetchStats, searchResults } =
            await import("@/app/(results)/actions");

        await expect(searchResults({ requester: ["alice"] })).resolves.toEqual([
            resultSet,
        ]);
        await expect(fetchStats()).resolves.toEqual(statsResult());

        expect(fetch).toHaveBeenNthCalledWith(
            1,
            "https://results.example/api/rest/v1/results?requester=alice",
        );
        expect(fetch).toHaveBeenNthCalledWith(
            2,
            "https://results.example/api/rest/v1/results/stats",
        );
    });

    it("fetches files from the authenticated endpoint when the JWT cookie exists", async () => {
        headerMocks.getCookie.mockReturnValue({ value: "jwt-1" });
        vi.mocked(fetch).mockResolvedValue(
            Response.json([
                {
                    path: "/out/a.txt",
                    mtime: "2026-05-21T00:00:00Z",
                    size: 10,
                    kind: "output",
                },
            ]),
        );

        const { fetchFiles } = await import("@/app/(results)/actions");

        await expect(fetchFiles("abc")).resolves.toEqual([
            {
                path: "/out/a.txt",
                mtime: "2026-05-21T00:00:00Z",
                size: 10,
                kind: "output",
            },
        ]);
        expect(fetch).toHaveBeenCalledWith(
            "https://results.example/api/rest/v1/auth/results/abc/files",
            expect.any(Object),
        );
        expectAuthorization(0, "jwt-1");
    });

    it("fetches file content from the authenticated endpoint when the JWT cookie exists", async () => {
        headerMocks.getCookie.mockReturnValue({ value: "jwt-1" });
        vi.mocked(fetch).mockResolvedValue(
            new Response("alpha\n", {
                headers: { "content-type": "text/plain" },
            }),
        );

        const { fetchFileContent } = await import("@/app/(results)/actions");

        await expect(fetchFileContent("abc", "/out/a.txt")).resolves.toEqual({
            content: "alpha\n",
            contentType: "text/plain",
        });
        expect(fetch).toHaveBeenCalledWith(
            "https://results.example/api/rest/v1/auth/results/abc/file?path=%2Fout%2Fa.txt",
            expect.any(Object),
        );
        expectAuthorization(0, "jwt-1");
    });

    it.each([
        [
            "fetchFiles",
            async () =>
                (await import("@/app/(results)/actions")).fetchFiles("abc"),
        ],
        [
            "fetchFileContent",
            async () =>
                (await import("@/app/(results)/actions")).fetchFileContent(
                    "abc",
                    "/out/a.txt",
                ),
        ],
    ])("preserves locked JSON from %s", async (_name, runAction) => {
        headerMocks.getCookie.mockReturnValue({ value: "jwt-1" });
        vi.mocked(fetch).mockResolvedValue(
            Response.json(lockedBody, { status: 403 }),
        );

        const result = runAction();

        await expect(result).rejects.toMatchObject({
            name: "BackendRequestError",
            status: 403,
            body: lockedBody,
        });
    });
});
