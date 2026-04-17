"use client";

import { createContext, createElement, useState, type ReactNode } from "react";

import {
    buildSeqmetaCacheCookie,
    readSeqmetaCacheSnapshotFromCookieHeader,
    SeqmetaCache,
} from "@/lib/seqmeta-cache-core";

export { SeqmetaCache } from "@/lib/seqmeta-cache-core";

export const SeqmetaCacheContext = createContext<SeqmetaCache>(
    new SeqmetaCache(),
);

function readInitialSeqmetaCache():
    | Record<string, never>
    | ReturnType<SeqmetaCache["snapshot"]> {
    if (typeof document === "undefined") {
        return {};
    }

    return readSeqmetaCacheSnapshotFromCookieHeader(document.cookie);
}

function persistSeqmetaCache(
    snapshot: ReturnType<SeqmetaCache["snapshot"]>,
): void {
    if (typeof document === "undefined") {
        return;
    }

    document.cookie = buildSeqmetaCacheCookie(snapshot);
}

export function SeqmetaCacheProvider({ children }: { children: ReactNode }) {
    const [cache] = useState(
        () => new SeqmetaCache(readInitialSeqmetaCache(), persistSeqmetaCache),
    );

    return createElement(
        SeqmetaCacheContext.Provider,
        { value: cache },
        children,
    );
}
