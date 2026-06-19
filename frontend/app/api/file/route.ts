import { NextRequest, NextResponse } from "next/server";
import sharp from "sharp";

import { resultsAuthCookieName, resultsRaw } from "@/lib/backend-client";

export const dynamic = "force-dynamic";

const defaultThumbnailHeight = 220;
const defaultThumbnailWidth = 360;

function clampDimension(value: string | null, fallback: number): number {
    const parsed = Number(value);

    if (!Number.isFinite(parsed)) {
        return fallback;
    }

    return Math.min(1600, Math.max(64, Math.round(parsed)));
}

function canThumbnail(contentType: string | null): boolean {
    if (!contentType) {
        return false;
    }

    const normalized = contentType.split(";")[0]?.trim().toLowerCase() ?? "";

    return normalized.startsWith("image/") && normalized !== "image/svg+xml";
}

async function buildThumbnailResponse(
    response: Response,
    width: number,
    height: number,
): Promise<NextResponse | null> {
    const contentType = response.headers.get("content-type");

    if (!canThumbnail(contentType)) {
        return null;
    }

    try {
        const sourceBuffer = Buffer.from(await response.arrayBuffer());
        const metadata = await sharp(sourceBuffer).metadata();

        if (
            (metadata.width ?? width + 1) <= width &&
            (metadata.height ?? height + 1) <= height
        ) {
            const headers = new Headers({
                "cache-control":
                    "private, max-age=300, stale-while-revalidate=3600",
                "content-security-policy": "sandbox",
            });

            if (contentType) {
                headers.set("content-type", contentType);
            }

            return new NextResponse(new Uint8Array(sourceBuffer), {
                headers,
                status: response.status,
            });
        }

        const thumbnail = await sharp(sourceBuffer)
            .resize({
                fit: "inside",
                height,
                kernel: sharp.kernel.lanczos3,
                width,
                withoutEnlargement: true,
            })
            .webp({ quality: 82 })
            .toBuffer();

        return new NextResponse(new Uint8Array(thumbnail), {
            headers: {
                "cache-control":
                    "private, max-age=300, stale-while-revalidate=3600",
                "content-security-policy": "sandbox",
                "content-type": "image/webp",
            },
            status: response.status,
        });
    } catch {
        return null;
    }
}

async function readErrorBody(
    response: Response,
): Promise<Record<string, unknown> | { error: string }> {
    const contentType = response.headers.get("content-type") ?? "";

    if (contentType.includes("application/json")) {
        const body = await response.json().catch(() => null);

        if (
            body &&
            typeof body === "object" &&
            "error" in body &&
            typeof body.error === "string" &&
            body.error.trim().length > 0
        ) {
            return body as Record<string, unknown>;
        }
    }

    const text = await response.text().catch(() => "");

    return { error: text.trim() || "unexpected error" };
}

function buildPassthroughHeaders(
    response: Response,
    options: { sandbox: boolean },
): Headers {
    const headers = new Headers();
    const contentType = response.headers.get("content-type");
    const contentDisposition = response.headers.get("content-disposition");
    const previewTruncated = response.headers.get("x-preview-truncated");

    if (contentType) {
        headers.set("content-type", contentType);
    }

    if (contentDisposition) {
        headers.set("content-disposition", contentDisposition);
    }

    if (previewTruncated) {
        headers.set("x-preview-truncated", previewTruncated);
    }

    if (options.sandbox) {
        headers.set("content-security-policy", "sandbox");
    }

    return headers;
}

type ResultsFileRequestOptions = {
    jwt?: string | null;
    method?: "HEAD";
};

async function cancelResponseBody(response: Response): Promise<void> {
    const body = response.body;

    if (!body) {
        return;
    }

    await body.cancel().catch(() => undefined);
}

async function fetchResultsFile(
    path: string,
    options: ResultsFileRequestOptions,
): Promise<Response> {
    const requestOptions: { jwt?: string; method?: "HEAD" } = {};

    if (options.jwt) {
        requestOptions.jwt = options.jwt;
    }

    if (options.method) {
        requestOptions.method = options.method;
    }

    return Object.keys(requestOptions).length > 0
        ? resultsRaw(path, requestOptions)
        : resultsRaw(path);
}

async function fetchFileResponse(
    path: string,
    options: { includeBody: boolean; jwt?: string | null },
): Promise<Response> {
    if (options.includeBody) {
        return fetchResultsFile(path, { jwt: options.jwt });
    }

    const headResponse = await fetchResultsFile(path, {
        jwt: options.jwt,
        method: "HEAD",
    });

    if (headResponse.status !== 405) {
        return headResponse;
    }

    await cancelResponseBody(headResponse);

    return fetchResultsFile(path, { jwt: options.jwt });
}

async function handleFileRequest(
    request: NextRequest,
    options: { includeBody: boolean },
): Promise<NextResponse> {
    const id = request.nextUrl.searchParams.get("id")?.trim();
    const path = request.nextUrl.searchParams.get("path")?.trim();
    const download = request.nextUrl.searchParams.get("download");
    const lineLimit = request.nextUrl.searchParams.get("line_limit")?.trim();
    const mode = request.nextUrl.searchParams.get("mode")?.trim();
    const thumbnail = request.nextUrl.searchParams.get("thumb");
    const thumbnailWidth = clampDimension(
        request.nextUrl.searchParams.get("w"),
        defaultThumbnailWidth,
    );
    const thumbnailHeight = clampDimension(
        request.nextUrl.searchParams.get("h"),
        defaultThumbnailHeight,
    );

    if (!id || !path) {
        return NextResponse.json(
            { error: "missing required query params: id, path" },
            { status: 400 },
        );
    }

    const query = new URLSearchParams({ path });
    if (download === "true") {
        query.set("download", "true");
    }
    if (lineLimit) {
        query.set("line_limit", lineLimit);
    }
    if (mode) {
        query.set("mode", mode);
    }

    const jwt = request.cookies.get(resultsAuthCookieName)?.value ?? null;
    const resultsPath = jwt ? "/rest/v1/auth/results" : "/rest/v1/results";

    let response: Response;
    try {
        const publicBackendPath = `/rest/v1/results/${encodeURIComponent(id)}/file?${query.toString()}`;
        const backendPath = `${resultsPath}/${encodeURIComponent(id)}/file?${query.toString()}`;
        response = await fetchFileResponse(backendPath, {
            includeBody: options.includeBody,
            jwt,
        });

        if (jwt && response.status === 401) {
            await cancelResponseBody(response);
            response = await fetchFileResponse(publicBackendPath, {
                includeBody: options.includeBody,
            });
        }
    } catch {
        return NextResponse.json(
            { error: "results backend request failed" },
            { status: 503 },
        );
    }

    if (!response.ok) {
        const body = await readErrorBody(response);
        const headers = new Headers();

        if (response.status === 413) {
            const fileSize = response.headers.get("x-file-size");

            if (fileSize) {
                headers.set("x-file-size", fileSize);
            }
        }

        return NextResponse.json(body, {
            status: response.status,
            headers,
        });
    }

    if (!options.includeBody) {
        const headers = buildPassthroughHeaders(response, {
            sandbox: download !== "true",
        });

        await cancelResponseBody(response);

        return new NextResponse(null, {
            status: response.status,
            headers,
        });
    }

    if (thumbnail === "true" && download !== "true") {
        const thumbnailResponse = await buildThumbnailResponse(
            response.clone(),
            thumbnailWidth,
            thumbnailHeight,
        );

        if (thumbnailResponse) {
            return thumbnailResponse;
        }
    }

    return new NextResponse(response.body, {
        status: response.status,
        headers: buildPassthroughHeaders(response, {
            sandbox: download !== "true",
        }),
    });
}

export async function GET(request: NextRequest): Promise<NextResponse> {
    return handleFileRequest(request, { includeBody: true });
}

export async function HEAD(request: NextRequest): Promise<NextResponse> {
    return handleFileRequest(request, { includeBody: false });
}
