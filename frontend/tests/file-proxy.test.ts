import { afterEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import sharp from "sharp";

const resultsRawMock = vi.fn();
const onePixelPng = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a7foAAAAASUVORK5CYII=",
    "base64",
);

vi.mock("@/lib/backend-client", () => ({
    resultsRaw: resultsRawMock,
}));

function makeRequest(query: string): NextRequest {
    return new NextRequest(`http://localhost/api/file?${query}`);
}

describe("P1 file content streaming API route", () => {
    afterEach(() => {
        vi.clearAllMocks();
        vi.resetModules();
    });

    it("streams binary file content and content type from the Go backend", async () => {
        const body = new Uint8Array([137, 80, 78, 71]);
        resultsRawMock.mockResolvedValue(
            new Response(body, {
                status: 200,
                headers: { "content-type": "image/png" },
            }),
        );

        const { GET, dynamic } = await import("@/app/api/file/route");

        const response = await GET(makeRequest("id=abc&path=%2Fout%2Fimg.png"));

        expect(dynamic).toBe("force-dynamic");
        expect(resultsRawMock).toHaveBeenCalledWith(
            "/results/abc/file?path=%2Fout%2Fimg.png",
        );
        expect(response.status).toBe(200);
        expect(response.headers.get("content-type")).toBe("image/png");
        expect(response.headers.get("content-security-policy")).toBe("sandbox");
        expect(new Uint8Array(await response.arrayBuffer())).toEqual(body);
    });

    it("preserves content disposition when download=true is requested", async () => {
        resultsRawMock.mockResolvedValue(
            new Response(new Uint8Array([1, 2, 3]), {
                status: 200,
                headers: {
                    "content-type": "application/gzip",
                    "content-disposition": 'attachment; filename="data.csv.gz"',
                },
            }),
        );

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(
            makeRequest("id=abc&path=%2Fout%2Fdata.csv.gz&download=true"),
        );

        expect(resultsRawMock).toHaveBeenCalledWith(
            "/results/abc/file?path=%2Fout%2Fdata.csv.gz&download=true",
        );
        expect(response.status).toBe(200);
        expect(response.headers.get("content-disposition")).toBe(
            'attachment; filename="data.csv.gz"',
        );
        expect(response.headers.get("content-security-policy")).toBeNull();
    });

    it("passes through the original image when it is already smaller than the requested thumbnail", async () => {
        resultsRawMock.mockResolvedValue(
            new Response(onePixelPng, {
                status: 200,
                headers: { "content-type": "image/png" },
            }),
        );

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(
            makeRequest("id=abc&path=%2Fout%2Fimg.png&thumb=true&w=320&h=180"),
        );

        expect(resultsRawMock).toHaveBeenCalledWith(
            "/results/abc/file?path=%2Fout%2Fimg.png",
        );
        expect(response.status).toBe(200);
        expect(response.headers.get("content-type")).toBe("image/png");
        expect(response.headers.get("content-security-policy")).toBe("sandbox");
        expect((await response.arrayBuffer()).byteLength).toBeGreaterThan(0);
    });

    it("returns resized thumbnail responses for large image thumbnail requests", async () => {
        const largePng = await sharp({
            create: {
                background: { alpha: 1, b: 16, g: 24, r: 32 },
                channels: 4,
                height: 720,
                width: 1280,
            },
        })
            .png()
            .toBuffer();

        resultsRawMock.mockResolvedValue(
            new Response(new Uint8Array(largePng), {
                status: 200,
                headers: { "content-type": "image/png" },
            }),
        );

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(
            makeRequest("id=abc&path=%2Fout%2Fimg.png&thumb=true&w=320&h=180"),
        );

        expect(response.status).toBe(200);
        expect(response.headers.get("content-type")).toBe("image/webp");
        expect(response.headers.get("content-security-policy")).toBe("sandbox");
        expect((await response.arrayBuffer()).byteLength).toBeGreaterThan(0);
    });

    it("falls back to the original streamed response when thumbnail generation fails", async () => {
        const invalidImage = new Uint8Array([137, 80, 78, 71, 0, 1, 2, 3]);
        resultsRawMock.mockResolvedValue(
            new Response(invalidImage, {
                status: 200,
                headers: { "content-type": "image/png" },
            }),
        );

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(
            makeRequest(
                "id=abc&path=%2Fout%2Fbroken.png&thumb=true&w=320&h=180",
            ),
        );

        expect(response.status).toBe(200);
        expect(response.headers.get("content-type")).toBe("image/png");
        expect(response.headers.get("content-security-policy")).toBe("sandbox");
        expect(new Uint8Array(await response.arrayBuffer())).toEqual(
            invalidImage,
        );
    });

    it("forwards error status and JSON when the Go backend rejects access", async () => {
        resultsRawMock.mockResolvedValue(
            Response.json({ error: "forbidden" }, { status: 403 }),
        );

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(
            makeRequest("id=abc&path=%2Fout%2Fsecret.txt"),
        );

        expect(response.status).toBe(403);
        await expect(response.json()).resolves.toEqual({ error: "forbidden" });
    });

    it("forwards gone responses from the Go backend", async () => {
        resultsRawMock.mockResolvedValue(
            Response.json({ error: "file not found on disk" }, { status: 410 }),
        );

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(
            makeRequest("id=abc&path=%2Fout%2Fmissing.txt"),
        );

        expect(response.status).toBe(410);
        await expect(response.json()).resolves.toEqual({
            error: "file not found on disk",
        });
    });

    it("preserves X-File-Size on 413 responses", async () => {
        resultsRawMock.mockResolvedValue(
            Response.json(
                { error: "file exceeds preview limit" },
                {
                    status: 413,
                    headers: { "x-file-size": "20971520" },
                },
            ),
        );

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(
            makeRequest("id=abc&path=%2Fout%2Flarge.bin"),
        );

        expect(response.status).toBe(413);
        expect(response.headers.get("x-file-size")).toBe("20971520");
        await expect(response.json()).resolves.toEqual({
            error: "file exceeds preview limit",
        });
    });

    it("forwards line-limited preview requests and preserves truncation metadata", async () => {
        resultsRawMock.mockResolvedValue(
            new Response("sample\tstatus\nalpha\tready\n", {
                status: 200,
                headers: {
                    "content-type": "text/tab-separated-values",
                    "x-preview-truncated": "true",
                },
            }),
        );

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(
            makeRequest("id=abc&path=%2Fout%2Freport.tsv&line_limit=2"),
        );

        expect(resultsRawMock).toHaveBeenCalledWith(
            "/results/abc/file?path=%2Fout%2Freport.tsv&line_limit=2",
        );
        expect(response.status).toBe(200);
        expect(response.headers.get("x-preview-truncated")).toBe("true");
        await expect(response.text()).resolves.toBe(
            "sample\tstatus\nalpha\tready\n",
        );
    });

    it("returns 400 when id or path is missing", async () => {
        const { GET } = await import("@/app/api/file/route");

        const missingId = await GET(makeRequest("path=%2Fout%2Fimg.png"));
        const missingPath = await GET(makeRequest("id=abc"));

        expect(resultsRawMock).not.toHaveBeenCalled();
        expect(missingId.status).toBe(400);
        expect(missingPath.status).toBe(400);
        await expect(missingId.json()).resolves.toEqual({
            error: "missing required query params: id, path",
        });
        await expect(missingPath.json()).resolves.toEqual({
            error: "missing required query params: id, path",
        });
    });

    it("returns 503 JSON when the results backend request throws", async () => {
        resultsRawMock.mockRejectedValue(new Error("socket closed"));

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(makeRequest("id=abc&path=%2Fout%2Fimg.png"));

        expect(response.status).toBe(503);
        await expect(response.json()).resolves.toEqual({
            error: "results backend request failed",
        });
    });

    it("preserves plain-text backend error messages", async () => {
        resultsRawMock.mockResolvedValue(
            new Response("backend timed out", {
                status: 504,
                headers: { "content-type": "text/plain" },
            }),
        );

        const { GET } = await import("@/app/api/file/route");

        const response = await GET(makeRequest("id=abc&path=%2Fout%2Fimg.png"));

        expect(response.status).toBe(504);
        await expect(response.json()).resolves.toEqual({
            error: "backend timed out",
        });
    });
});
