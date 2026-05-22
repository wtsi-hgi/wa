import { NextRequest, NextResponse } from "next/server";

import { resultsRaw } from "@/lib/backend-client";
import {
    resultsAuthCookieName as authCookieName,
    resultsAuthCookieOptions as authCookieOptions,
} from "@/lib/results-auth-cookie";

export const dynamic = "force-dynamic";

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

async function notifyBackendLogout(jwt: string): Promise<void> {
    try {
        await resultsRaw("/rest/v1/auth/logout", {
            cache: "no-store",
            headers: {
                authorization: `Bearer ${jwt}`,
            },
            method: "POST",
        });
    } catch {
        // Browser logout is complete once the auth cookie is expired.
    }
}

export async function POST(request: NextRequest): Promise<NextResponse> {
    const jwt = request.cookies.get(authCookieName)?.value;

    if (jwt) {
        void notifyBackendLogout(jwt);
    }

    const response = NextResponse.json(anonymousSession());
    expireAuthCookie(response);

    return response;
}
