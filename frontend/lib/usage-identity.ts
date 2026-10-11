import { createHash } from "node:crypto";

import { cookies } from "next/headers";

import { currentSession } from "@/app/(results)/auth/actions";
import { usageSessionCookieName } from "@/lib/usage-session-cookie";

// maxUsernameBytes matches wa's UsageMaxUsernameBytes. The client ID is
// always 32 hex chars, well within UsageMaxClientIDBytes.
const maxUsernameBytes = 128;

// usageClientId returns the first 32 hex chars of SHA-256(cookieValue).
export function usageClientId(cookieValue: string): string {
    return createHash("sha256").update(cookieValue).digest("hex").slice(0, 32);
}

// sanitiseHeaderValue replaces every UTF-8 byte outside 0x20-0x7E with "?",
// then cuts the result to maxBytes, as wa's RemoteClient does.
function sanitiseHeaderValue(value: string, maxBytes: number): string {
    let sanitised = "";

    for (const byte of Buffer.from(value, "utf8")) {
        sanitised +=
            byte >= 0x20 && byte <= 0x7e ? String.fromCharCode(byte) : "?";
    }

    return sanitised.slice(0, maxBytes);
}

async function readUsageCookie(): Promise<string | null> {
    try {
        const cookieStore = await cookies();

        return cookieStore.get(usageSessionCookieName)?.value ?? null;
    } catch {
        return null;
    }
}

async function resolveUsername(
    username: string | null | undefined,
): Promise<string | null> {
    if (username !== undefined) {
        return username;
    }

    try {
        const session = await currentSession();

        return session.authenticated ? session.username : null;
    } catch {
        return null;
    }
}

// mlwhIdentityHeaders returns x-wa-client-id from the usage cookie and
// x-wa-client-user from username, or from currentSession() when username
// is undefined. It never throws: a failing cookies() omits the ID and a
// failing currentSession() omits the user.
export async function mlwhIdentityHeaders(
    username?: string | null,
): Promise<Record<string, string>> {
    const [cookieValue, user] = await Promise.all([
        readUsageCookie(),
        resolveUsername(username),
    ]);
    const headers: Record<string, string> = {};
    const clientUser = user ? sanitiseHeaderValue(user, maxUsernameBytes) : "";

    if (cookieValue) {
        headers["x-wa-client-id"] = usageClientId(cookieValue);
    }

    if (clientUser) {
        headers["x-wa-client-user"] = clientUser;
    }

    return headers;
}
