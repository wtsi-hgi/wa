"use client";

import {
    createContext,
    createElement,
    useMemo,
    useSyncExternalStore,
    type ReactNode,
} from "react";

import {
    buildSeqmetaCacheCookie,
    readSeqmetaCacheSnapshotFromCookieHeader,
    SeqmetaCache,
} from "@/lib/seqmeta-cache-core";

export { SeqmetaCache } from "@/lib/seqmeta-cache-core";

export const SeqmetaCacheContext = createContext<SeqmetaCache>(
    new SeqmetaCache(),
);

function readSeqmetaCookieHeader(): string {
    if (typeof document === "undefined") {
        return "";
    }

    return document.cookie;
}

function subscribeToSeqmetaCookie(): () => void {
    return () => {};
}

function persistSeqmetaCache(
    snapshot: ReturnType<SeqmetaCache["snapshot"]>,
): void {
    if (typeof document === "undefined") {
        return;
    }

    const cookie = buildSeqmetaCacheCookie(snapshot);
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

export function SeqmetaCacheProvider({ children }: { children: ReactNode }) {
    const cache = useMemo(() => new SeqmetaCache({}, persistSeqmetaCache), []);

    const cookieHeader = useSyncExternalStore(
        subscribeToSeqmetaCookie,
        readSeqmetaCookieHeader,
        () => "",
    );
    const initialSnapshot = useMemo(
        () => readSeqmetaCacheSnapshotFromCookieHeader(cookieHeader),
        [cookieHeader],
    );

    cache.hydrateMissing(initialSnapshot);

    return createElement(
        SeqmetaCacheContext.Provider,
        { value: cache },
        children,
    );
}
