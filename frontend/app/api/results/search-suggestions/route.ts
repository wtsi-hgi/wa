import { NextRequest, NextResponse } from "next/server";

import {
    BackendRequestError,
    resultsAuthCookieName,
    resultsJson,
} from "@/lib/backend-client";
import { searchSuggestionSchema } from "@/lib/contracts";

export const dynamic = "force-dynamic";

function suggestionPath(prefix: string, request: NextRequest): string {
    const query = request.nextUrl.searchParams;
    const params = new URLSearchParams();
    const term = query.get("q")?.trim() ?? "";
    const limit = query.get("limit")?.trim() ?? "";

    if (term) {
        params.set("q", term);
    }
    if (limit) {
        params.set("limit", limit);
    }

    const rendered = params.toString();

    return rendered ? `${prefix}?${rendered}` : prefix;
}

export async function GET(request: NextRequest): Promise<NextResponse> {
    const jwt = request.cookies.get(resultsAuthCookieName)?.value ?? null;
    const publicPath = suggestionPath(
        "/rest/v1/results/search-suggestions",
        request,
    );
    const authPath = suggestionPath(
        "/rest/v1/auth/results/search-suggestions",
        request,
    );

    try {
        const suggestions = jwt
            ? await resultsJson(authPath, searchSuggestionSchema.array(), {
                  jwt,
              })
            : await resultsJson(publicPath, searchSuggestionSchema.array());

        return NextResponse.json(suggestions);
    } catch (error) {
        if (
            jwt &&
            error instanceof BackendRequestError &&
            error.status === 401
        ) {
            const suggestions = await resultsJson(
                publicPath,
                searchSuggestionSchema.array(),
            );

            return NextResponse.json(suggestions);
        }

        throw error;
    }
}
