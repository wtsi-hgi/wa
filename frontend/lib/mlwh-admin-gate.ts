import { currentSession } from "@/app/(results)/auth/actions";
import { isMLWHAdmin, readMLWHServerToken } from "@/lib/feedback-admin";

// MLWHAdminAuth is what an admin request to wa needs from the gate: the MLWH
// server token and the session username, sent as the usage identity.
export type MLWHAdminAuth = { token: string; username: string | null };

export type MLWHAdminGateState =
    | { status: "unauthenticated" | "forbidden" | "invalid_input" }
    | { status: "unavailable"; reason: "no_token" };

type ParseResult<T> = { success: true; data: T } | { success: false };

// authorizeMLWHAdminSession runs the session and allowlist checks, returning
// the admin's username or the state to return without calling wa. Pages that
// read only unauthenticated wa routes need no server token, so stop here.
export async function authorizeMLWHAdminSession(): Promise<
    { username: string | null } | { status: "unauthenticated" | "forbidden" }
> {
    const session = await currentSession();

    if (!session.authenticated) {
        return { status: "unauthenticated" };
    }

    if (!isMLWHAdmin(session.username)) {
        return { status: "forbidden" };
    }

    return { username: session.username };
}

// authorizeMLWHAdmin runs the session, allowlist, and argument checks in that
// order, then reads the MLWH server token, all before any fetch. It returns
// the token, parsed arguments, and session username, or the state to return
// without calling wa. currentSession runs once.
export async function authorizeMLWHAdmin<T>(
    parseArgs: () => ParseResult<T>,
): Promise<(MLWHAdminAuth & { args: T }) | MLWHAdminGateState> {
    const session = await authorizeMLWHAdminSession();

    if ("status" in session) {
        return session;
    }

    const parsed = parseArgs();

    if (!parsed.success) {
        return { status: "invalid_input" };
    }

    const token = readMLWHServerToken();

    if (token === null) {
        return { status: "unavailable", reason: "no_token" };
    }

    return { token, args: parsed.data, username: session.username };
}
