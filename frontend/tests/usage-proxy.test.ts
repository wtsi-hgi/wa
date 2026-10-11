import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import { config, proxy } from "@/proxy";

const uuidPattern =
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const validUuid = "0f8fad5b-d9cb-469f-a165-70867728950e";

function requestWithCookie(cookie?: string): NextRequest {
    return new NextRequest(
        "https://h/",
        cookie ? { headers: { cookie } } : undefined,
    );
}

function forwardedCookies(response: Response): Map<string, string> {
    const header = response.headers.get("x-middleware-request-cookie") ?? "";
    const pairs = header
        .split(";")
        .map((part) => part.trim())
        .filter(Boolean)
        .map((part) => {
            const index = part.indexOf("=");

            return [part.slice(0, index), part.slice(index + 1)] as const;
        });

    return new Map(pairs);
}

function setUsageCookie(response: Response): string {
    const setCookie = response.headers.get("set-cookie");

    if (!setCookie) {
        throw new Error("no set-cookie header");
    }

    return setCookie;
}

function setCookieValue(setCookie: string): string {
    const match = /^wa_usage_session=([^;]*)/.exec(setCookie);

    if (!match?.[1]) {
        throw new Error(`no wa_usage_session in ${setCookie}`);
    }

    return match[1];
}

describe("E1 usage session proxy", () => {
    it("sets a new UUID session cookie on the response and forwarded request when absent (test 1)", () => {
        const response = proxy(requestWithCookie());
        const setCookie = setUsageCookie(response);
        const uuid = setCookieValue(setCookie);
        const attributes = setCookie
            .split(";")
            .map((part) => part.trim().toLowerCase());

        expect(uuid).toMatch(uuidPattern);
        expect(attributes).toEqual(
            expect.arrayContaining([
                "httponly",
                "secure",
                "samesite=lax",
                "path=/",
                "max-age=31536000",
            ]),
        );
        expect(forwardedCookies(response).get("wa_usage_session")).toBe(uuid);
    });

    it("leaves a valid UUID cookie alone and replaces an invalid one (test 2)", () => {
        const kept = proxy(requestWithCookie(`wa_usage_session=${validUuid}`));

        expect(kept.headers.get("set-cookie")).toBeNull();
        expect(kept.headers.get("x-middleware-request-cookie")).toBeNull();

        const replaced = proxy(requestWithCookie("wa_usage_session=x"));
        const uuid = setCookieValue(setUsageCookie(replaced));

        expect(uuid).toMatch(uuidPattern);
        expect(forwardedCookies(replaced).get("wa_usage_session")).toBe(uuid);
    });

    it("keeps every other cookie in the forwarded cookie header (test 3)", () => {
        const added = proxy(requestWithCookie("wa_results_jwt=J"));
        const addedCookies = forwardedCookies(added);

        expect(addedCookies.get("wa_results_jwt")).toBe("J");
        expect(addedCookies.get("wa_usage_session")).toBe(
            setCookieValue(setUsageCookie(added)),
        );

        const replaced = proxy(
            requestWithCookie("wa_results_jwt=J; wa_usage_session=x"),
        );
        const replacedCookies = forwardedCookies(replaced);
        const newUuid = setCookieValue(setUsageCookie(replaced));

        expect(replacedCookies.get("wa_results_jwt")).toBe("J");
        expect(newUuid).toMatch(uuidPattern);
        expect(replacedCookies.get("wa_usage_session")).toBe(newUuid);
        expect(
            replaced.headers.get("x-middleware-request-cookie"),
        ).not.toContain("wa_usage_session=x");
    });

    it("matches pages but not API, static, image, or favicon paths (test 4)", () => {
        const matches = (url: string) =>
            unstable_doesMiddlewareMatch({ config, url });

        expect(matches("/")).toBe(true);
        expect(matches("/usage")).toBe(true);
        expect(matches("/api/health")).toBe(false);
        expect(matches("/_next/static/chunks/main.js")).toBe(false);
        expect(matches("/_next/image?url=%2Fx.png&w=64&q=75")).toBe(false);
        expect(matches("/favicon.ico")).toBe(false);
    });
});
