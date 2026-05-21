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

const credentialsSchema = z.object({
    password: z.string(),
    username: z.string().min(1),
});
const jwtSchema = z.string().min(1);

type CurrentSession = {
    authenticated: boolean;
    username: string | null;
};

function authenticatedSession(username: string): CurrentSession {
    return {
        authenticated: true,
        username,
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

export async function POST(request: NextRequest): Promise<NextResponse> {
    const body = await request.json().catch(() => null);
    const credentials = credentialsSchema.safeParse(body);

    if (!credentials.success) {
        return NextResponse.json(
            { error: "username and password are required" },
            { status: 400 },
        );
    }

    let response: Response;
    try {
        response = await resultsRaw("/rest/v1/jwt", {
            body: JSON.stringify(credentials.data),
            cache: "no-store",
            headers: {
                "content-type": "application/json",
            },
            method: "POST",
        });
    } catch {
        return NextResponse.json(
            { error: "results backend request failed" },
            { status: 503 },
        );
    }

    if (!response.ok) {
        return NextResponse.json(
            { error: "authentication failed" },
            { status: 401 },
        );
    }

    let jwt: string;
    try {
        jwt = await readJwt(response);
    } catch {
        return NextResponse.json(
            { error: "invalid backend response" },
            { status: 502 },
        );
    }

    const nextResponse = NextResponse.json(
        authenticatedSession(credentials.data.username),
    );
    nextResponse.cookies.set(authCookieName, jwt, authCookieOptions);

    return nextResponse;
}
