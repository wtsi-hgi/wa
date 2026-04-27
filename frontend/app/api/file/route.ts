import { NextRequest, NextResponse } from "next/server";

import { resultsRaw } from "@/lib/backend-client";

export const dynamic = "force-dynamic";

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

    const headers = new Headers();
    const contentType = response.headers.get("content-type");
    const contentDisposition = response.headers.get("content-disposition");

    if (contentType) {
        headers.set("content-type", contentType);
    }

    if (contentDisposition) {
        headers.set("content-disposition", contentDisposition);
    }

    if (download !== "true") {
        headers.set("content-security-policy", "sandbox");
    }

    return new NextResponse(response.body, {
        status: response.status,
        headers,
    });
}
