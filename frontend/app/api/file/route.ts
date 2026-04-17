import { NextRequest, NextResponse } from "next/server";

import { resultsRaw } from "@/lib/backend-client";

export const dynamic = "force-dynamic";

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

    const response = await resultsRaw(
        `/results/${encodeURIComponent(id)}/file?${query.toString()}`,
    );

    if (!response.ok) {
        const body = await response
            .json()
            .catch(() => ({ error: "unexpected error" }));
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

    return new NextResponse(response.body, {
        status: response.status,
        headers,
    });
}
