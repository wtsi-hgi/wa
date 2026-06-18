"use client";

import {
    createContext,
    createElement,
    useMemo,
    useSyncExternalStore,
    type ReactNode,
} from "react";

import {
    buildMLWHCacheCookie,
    readMLWHCacheSnapshotFromCookieHeader,
    MLWHCache,
} from "@/lib/mlwh-cache-core";

export { MLWHCache } from "@/lib/mlwh-cache-core";

export const MLWHCacheContext = createContext<MLWHCache>(new MLWHCache());

function readMLWHCookieHeader(): string {
    if (typeof document === "undefined") {
        return "";
    }

    return document.cookie;
}

function subscribeToMLWHCookie(): () => void {
    return () => {};
}

function persistMLWHCache(snapshot: ReturnType<MLWHCache["snapshot"]>): void {
    if (typeof document === "undefined") {
        return;
    }

    const cookie = buildMLWHCacheCookie(snapshot);
    const [cookiePair = ""] = cookie.split(";");
    const existingCookie = document.cookie
        .split(";")
        .map((entry) => entry.trim())
        .find((entry) => entry.startsWith(`${cookiePair.split("=")[0]}=`));

    if (existingCookie === cookiePair) {
        return;
    }

    document.cookie = cookie;
}

export function MLWHCacheProvider({ children }: { children: ReactNode }) {
    const cache = useMemo(() => new MLWHCache({}, persistMLWHCache), []);

    const cookieHeader = useSyncExternalStore(
        subscribeToMLWHCookie,
        readMLWHCookieHeader,
        () => "",
    );
    const initialSnapshot = useMemo(
        () => readMLWHCacheSnapshotFromCookieHeader(cookieHeader),
        [cookieHeader],
    );

    cache.hydrateMissing(initialSnapshot);

    return createElement(MLWHCacheContext.Provider, { value: cache }, children);
}
