import { NextRequest, NextResponse } from "next/server";

import { resultsRaw } from "@/lib/backend-client";

export const dynamic = "force-dynamic";

const authCookieName = "wa_results_jwt";
const authCookieOptions = {
    httpOnly: true,
    path: "/",
    sameSite: "lax",
    secure: true,
} as const;

function anonymousSession() {
    return {
        authenticated: false,
        username: null,
    };
}

function expireAuthCookie(response: NextResponse): void {
    response.cookies.set(authCookieName, "", {
        ...authCookieOptions,
        maxAge: 0,
    });
}

export async function POST(request: NextRequest): Promise<NextResponse> {
    const jwt = request.cookies.get(authCookieName)?.value;
    let status = 200;
    let body: { authenticated: boolean; username: null } | { error: string } =
        anonymousSession();

    if (jwt) {
        try {
            const response = await resultsRaw("/rest/v1/auth/logout", {
                cache: "no-store",
                headers: {
                    authorization: `Bearer ${jwt}`,
                },
                method: "POST",
            });

            if (
                !response.ok &&
                response.status !== 404 &&
                response.status !== 501
            ) {
                status = 502;
                body = { error: "logout failed" };
            }
        } catch {
            status = 503;
            body = { error: "results backend request failed" };
        }
    }

    const response = NextResponse.json(body, { status });
    expireAuthCookie(response);

    return response;
}
