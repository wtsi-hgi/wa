import { type NextRequest, NextResponse } from "next/server";

import {
    usageSessionCookieName,
    usageSessionCookieOptions,
} from "@/lib/usage-session-cookie";

const uuidPattern =
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// proxy gives each browser an anonymous usage session cookie. A new one is
// set on both the forwarded request, so this request's Server Actions see
// it, and the response. request.cookies.set rewrites the request's cookie
// header with every cookie, so the results JWT keeps reaching the server.
export function proxy(request: NextRequest): NextResponse {
    const current = request.cookies.get(usageSessionCookieName)?.value;

    if (current && uuidPattern.test(current)) {
        return NextResponse.next();
    }

    const uuid = crypto.randomUUID();

    request.cookies.set(usageSessionCookieName, uuid);

    const response = NextResponse.next({
        request: { headers: request.headers },
    });

    response.cookies.set(
        usageSessionCookieName,
        uuid,
        usageSessionCookieOptions,
    );

    return response;
}

export const config = {
    matcher: ["/((?!api|_next/static|_next/image|favicon.ico).*)"],
};
