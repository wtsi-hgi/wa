import { NextRequest, NextResponse } from "next/server";
import sharp from "sharp";

import { resultsRaw } from "@/lib/backend-client";

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

async function readErrorBody(response: Response): Promise<{ error: string }> {
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
            return { error: body.error };
        }
    }

    const text = await response.text().catch(() => "");

    return { error: text.trim() || "unexpected error" };
}

export async function GET(request: NextRequest): Promise<NextResponse> {
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

    let response: Response;
    try {
        response = await resultsRaw(
            `/results/${encodeURIComponent(id)}/file?${query.toString()}`,
        );
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

    if (download !== "true") {
        headers.set("content-security-policy", "sandbox");
    }

    return new NextResponse(response.body, {
        status: response.status,
        headers,
    });
}
