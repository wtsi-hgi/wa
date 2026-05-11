import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";

import {
    BackendRequestError,
    BackendUnavailableError,
    resultsJson,
    resultsRaw,
    seqmetaJson,
} from "@/lib/backend-client";

describe("H1 dual backend client", () => {
    afterEach(() => {
        delete process.env.WA_RESULTS_BACKEND_URL;
        delete process.env.WA_SEQMETA_BACKEND_URL;
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
    });

    it("returns validated results JSON from the configured results backend", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "http://localhost:8090";

        const fetchMock = vi
            .fn()
            .mockResolvedValue(Response.json({ total: 5 }, { status: 200 }));
        vi.stubGlobal("fetch", fetchMock);

        const statsSchema = z.object({ total: z.number() });

        await expect(
            resultsJson("/results/stats", statsSchema),
        ).resolves.toEqual({
            total: 5,
        });
        expect(fetchMock).toHaveBeenCalledWith(
            "http://localhost:8090/results/stats",
        );
    });

    it("preserves a path prefix in the configured results backend URL", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "https://host/results-api";

        const fetchMock = vi
            .fn()
            .mockResolvedValue(Response.json({ total: 5 }, { status: 200 }));
        vi.stubGlobal("fetch", fetchMock);

        const statsSchema = z.object({ total: z.number() });

        await expect(
            resultsJson("/results/stats", statsSchema),
        ).resolves.toEqual({
            total: 5,
        });
        expect(fetchMock).toHaveBeenCalledWith(
            "https://host/results-api/results/stats",
        );
    });

    it("throws BackendRequestError with the response status for non-2xx responses", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "http://localhost:8090";

        vi.stubGlobal(
            "fetch",
            vi
                .fn()
                .mockResolvedValue(
                    Response.json({ error: "not found" }, { status: 404 }),
                ),
        );

        const statsSchema = z.object({ total: z.number() });

        const result = resultsJson("/results/stats", statsSchema);

        await expect(result).rejects.toMatchObject({
            status: 404,
            body: { error: "not found" },
        });
        await expect(result).rejects.toBeInstanceOf(BackendRequestError);
    });

    it("throws BackendRequestError when the response body fails schema validation", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "http://localhost:8090";

        vi.stubGlobal(
            "fetch",
            vi
                .fn()
                .mockResolvedValue(
                    Response.json({ bad: "shape" }, { status: 200 }),
                ),
        );

        const statsSchema = z.object({ total: z.number() });

        await expect(
            resultsJson("/results/stats", statsSchema),
        ).rejects.toBeInstanceOf(BackendRequestError);
    });

    it("throws BackendUnavailableError when the seqmeta backend URL is not configured", async () => {
        const studiesSchema = z.array(z.object({ id: z.string() }));

        await expect(
            seqmetaJson("/studies", studiesSchema),
        ).rejects.toBeInstanceOf(BackendUnavailableError);
    });

    it("returns validated seqmeta JSON from the configured seqmeta backend", async () => {
        process.env.WA_SEQMETA_BACKEND_URL = "http://localhost:8091";

        const fetchMock = vi
            .fn()
            .mockResolvedValue(
                Response.json([{ id: "study-1" }], { status: 200 }),
            );
        vi.stubGlobal("fetch", fetchMock);

        const studiesSchema = z.array(z.object({ id: z.string() }));

        await expect(seqmetaJson("/studies", studiesSchema)).resolves.toEqual([
            { id: "study-1" },
        ]);
        expect(fetchMock).toHaveBeenCalledWith("http://localhost:8091/studies");
    });

    it("preserves a path prefix in the configured seqmeta backend URL", async () => {
        process.env.WA_SEQMETA_BACKEND_URL = "https://host/seqmeta-api";

        const fetchMock = vi
            .fn()
            .mockResolvedValue(
                Response.json([{ id: "study-1" }], { status: 200 }),
            );
        vi.stubGlobal("fetch", fetchMock);

        const studiesSchema = z.array(z.object({ id: z.string() }));

        await expect(seqmetaJson("/studies", studiesSchema)).resolves.toEqual([
            { id: "study-1" },
        ]);
        expect(fetchMock).toHaveBeenCalledWith(
            "https://host/seqmeta-api/studies",
        );
    });

    it("returns the raw response for results file requests", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "http://localhost:8090";

        const binaryBody = new Uint8Array([137, 80, 78, 71]);
        const response = new Response(binaryBody, {
            status: 200,
            headers: { "content-type": "image/png" },
        });

        const fetchMock = vi.fn().mockResolvedValue(response);
        vi.stubGlobal("fetch", fetchMock);

        const rawResponse = await resultsRaw("/results/1/file?path=/tmp/x.png");

        expect(fetchMock).toHaveBeenCalledWith(
            "http://localhost:8090/results/1/file?path=/tmp/x.png",
        );
        expect(rawResponse.status).toBe(200);
        expect(rawResponse.headers.get("content-type")).toBe("image/png");
        expect(new Uint8Array(await rawResponse.arrayBuffer())).toEqual(
            binaryBody,
        );
    });

    it("preserves a path prefix and query string for raw results file requests", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "https://host/results-api";

        const response = new Response(new Uint8Array([137, 80, 78, 71]), {
            status: 200,
            headers: { "content-type": "image/png" },
        });
        const fetchMock = vi.fn().mockResolvedValue(response);
        vi.stubGlobal("fetch", fetchMock);

        await resultsRaw("/results/1/file?path=/tmp/x.png");

        expect(fetchMock).toHaveBeenCalledWith(
            "https://host/results-api/results/1/file?path=/tmp/x.png",
        );
    });
});
