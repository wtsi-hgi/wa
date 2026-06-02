"use server";

import { cookies } from "next/headers";
import { z } from "zod";

import { resultsRaw } from "@/lib/backend-client";
import {
    resultsAuthCookieName as authCookieName,
    resultsAuthCookieOptions as authCookieOptions,
} from "@/lib/results-auth-cookie";

const jwtSchema = z.string().min(1);
const backendSessionSchema = z.object({
    authenticated: z.boolean(),
    is_owner: z.boolean().optional(),
    username: z.string().nullable().optional(),
});

export type CurrentSession = {
    authenticated: boolean;
    username: string | null;
};

type LoginInput = {
    username: string;
    password: string;
};

function anonymousSession(): CurrentSession {
    return {
        authenticated: false,
        username: null,
    };
}

function bearerHeaders(jwt: string): HeadersInit {
    return {
        authorization: `Bearer ${jwt}`,
    };
}

async function authFetch(path: string, init: RequestInit): Promise<Response> {
    try {
        return await resultsRaw(path, {
            ...init,
            cache: "no-store",
        });
    } catch {
        throw new Error("results backend request failed");
    }
}

async function readJson(response: Response): Promise<unknown> {
    try {
        return await response.json();
    } catch {
        throw new Error("invalid backend response");
    }
}

async function readJwt(response: Response): Promise<string> {
    const parsed = jwtSchema.safeParse(await readJson(response));

    if (!parsed.success) {
        throw new Error("invalid backend response");
    }

    return parsed.data;
}

function parseSession(body: unknown): CurrentSession {
    const parsed = backendSessionSchema.safeParse(body);

    if (!parsed.success) {
        throw new Error("invalid backend response");
    }

    if (!parsed.data.authenticated) {
        return anonymousSession();
    }

    return {
        authenticated: true,
        username: parsed.data.username ?? null,
    };
}

async function sessionForToken(jwt: string): Promise<CurrentSession> {
    const response = await authFetch("/rest/v1/auth/session", {
        headers: bearerHeaders(jwt),
        method: "GET",
    });

    if (!response.ok) {
        return anonymousSession();
    }

    return parseSession(await readJson(response));
}

async function readCookieJwt(): Promise<string | null> {
    const cookieStore = await cookies();

    return cookieStore.get(authCookieName)?.value ?? null;
}

async function setAuthCookie(jwt: string): Promise<void> {
    const cookieStore = await cookies();

    cookieStore.set(authCookieName, jwt, authCookieOptions);
}

async function expireAuthCookie(): Promise<void> {
    const cookieStore = await cookies();

    cookieStore.set(authCookieName, "", {
        ...authCookieOptions,
        maxAge: 0,
    });
}

export async function loginAction(input: LoginInput): Promise<CurrentSession> {
    const response = await authFetch("/rest/v1/jwt", {
        body: JSON.stringify({
            password: input.password,
            username: input.username,
        }),
        headers: {
            "content-type": "application/json",
        },
        method: "POST",
    });

    if (!response.ok) {
        throw new Error("authentication failed");
    }

    const jwt = await readJwt(response);

    await setAuthCookie(jwt);

    return {
        authenticated: true,
        username: input.username,
    };
}

export async function logoutAction(): Promise<CurrentSession> {
    const jwt = await readCookieJwt();

    await expireAuthCookie();

    if (jwt) {
        void notifyBackendLogout(jwt);
    }

    return anonymousSession();
}

async function notifyBackendLogout(jwt: string): Promise<void> {
    try {
        await authFetch("/rest/v1/auth/logout", {
            headers: bearerHeaders(jwt),
            method: "POST",
        });
    } catch {
        // Browser logout is complete once the auth cookie is expired.
    }
}

export async function refreshAction(): Promise<CurrentSession> {
    const jwt = await readCookieJwt();

    if (!jwt) {
        return anonymousSession();
    }

    const response = await authFetch("/rest/v1/jwt", {
        headers: bearerHeaders(jwt),
        method: "GET",
    });

    if (!response.ok) {
        await expireAuthCookie();

        return anonymousSession();
    }

    const refreshedJwt = await readJwt(response);

    await setAuthCookie(refreshedJwt);

    return sessionForToken(refreshedJwt);
}

export async function currentSession(): Promise<CurrentSession> {
    const jwt = await readCookieJwt();

    if (!jwt) {
        return anonymousSession();
    }

    return sessionForToken(jwt);
}
