import { NextRequest, NextResponse } from "next/server";
import { z } from "zod";

import { resultsRaw } from "@/lib/backend-client";

export const dynamic = "force-dynamic";

const authCookieName = "wa_results_jwt";
const authCookieOptions = {
    httpOnly: true,
    path: "/",
    sameSite: "lax",
    secure: true,
} as const;

const jwtSchema = z.string().min(1);
const backendSessionSchema = z.object({
    authenticated: z.boolean(),
    is_owner: z.boolean().optional(),
    username: z.string().nullable().optional(),
});

type CurrentSession = {
    authenticated: boolean;
    username: string | null;
};

function anonymousSession(): CurrentSession {
    return {
        authenticated: false,
        username: null,
    };
}

function parseSession(body: unknown): CurrentSession {
    const parsed = backendSessionSchema.safeParse(body);

    if (!parsed.success || !parsed.data.authenticated) {
        return anonymousSession();
    }

    return {
        authenticated: true,
        username: parsed.data.username ?? null,
    };
}

async function readJwt(response: Response): Promise<string> {
    const body = await response.json().catch(() => null);
    const parsed = jwtSchema.safeParse(body);

    if (!parsed.success) {
        throw new Error("invalid backend response");
    }

    return parsed.data;
}

async function readCurrentSession(jwt: string): Promise<CurrentSession> {
    const response = await resultsRaw("/rest/v1/auth/session", {
        cache: "no-store",
        headers: {
            authorization: `Bearer ${jwt}`,
        },
        method: "GET",
    });

    if (!response.ok) {
        return anonymousSession();
    }

    return parseSession(await response.json().catch(() => null));
}

function expireAuthCookie(response: NextResponse): void {
    response.cookies.set(authCookieName, "", {
        ...authCookieOptions,
        maxAge: 0,
    });
}

export async function POST(request: NextRequest): Promise<NextResponse> {
    const jwt = request.cookies.get(authCookieName)?.value;

    if (!jwt) {
        return NextResponse.json(anonymousSession());
    }

    let refreshResponse: Response;
    try {
        refreshResponse = await resultsRaw("/rest/v1/jwt", {
            cache: "no-store",
            headers: {
                authorization: `Bearer ${jwt}`,
            },
            method: "GET",
        });
    } catch {
        const response = NextResponse.json(
            { error: "results backend request failed" },
            { status: 503 },
        );
        expireAuthCookie(response);

        return response;
    }

    if (!refreshResponse.ok) {
        const response = NextResponse.json(
            { error: "authentication failed" },
            { status: 401 },
        );
        expireAuthCookie(response);

        return response;
    }

    let refreshedJwt: string;
    try {
        refreshedJwt = await readJwt(refreshResponse);
    } catch {
        return NextResponse.json(
            { error: "invalid backend response" },
            { status: 502 },
        );
    }

    const session = await readCurrentSession(refreshedJwt);
    const response = NextResponse.json(session);
    response.cookies.set(authCookieName, refreshedJwt, authCookieOptions);

    return response;
}
