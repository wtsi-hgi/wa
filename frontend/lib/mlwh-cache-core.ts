import { z } from "zod";

import { enrichmentResultSchema, type EnrichmentResult } from "@/lib/contracts";

// Lenient fallback for legacy cookies that persisted already-normalized
// enrichment results before the input schema was tightened. We only require
// the minimal identifying fields so older payloads keep round-tripping.
const legacyEnrichmentResultSchema = z
    .object({
        identifier: z.string().min(1),
        type: z.string(),
    })
    .passthrough();

export const MLWH_CACHE_COOKIE_NAME = ["wa", "seqmeta", "cache"].join("-");
const MLWH_CACHE_COOKIE_MAX_AGE_SECONDS = 60 * 60 * 24 * 7;
const MLWH_CACHE_COOKIE_MAX_VALUE_LENGTH = 3000;

export type MLWHCacheSnapshot = Record<string, EnrichmentResult | null>;

type MLWHCacheChangeListener = (snapshot: MLWHCacheSnapshot) => void;

export interface MLWHCacheStore {
    get(value: string): EnrichmentResult | null | undefined;
    set(value: string, result: EnrichmentResult | null): void;
    has(value: string): boolean;
}

function areCacheResultsEqual(
    left: EnrichmentResult | null | undefined,
    right: EnrichmentResult | null,
): boolean {
    if (left === right) {
        return true;
    }

    return left === null && right === null;
}

export class MLWHCache implements MLWHCacheStore {
    private cache: Map<string, EnrichmentResult | null>;
    private pendingFlush = false;

    constructor(
        snapshot: MLWHCacheSnapshot = {},
        private readonly onChange?: MLWHCacheChangeListener,
    ) {
        this.cache = new Map(Object.entries(snapshot));
    }

    get(value: string): EnrichmentResult | null | undefined {
        return this.cache.get(value);
    }

    set(value: string, result: EnrichmentResult | null): void {
        if (
            this.cache.has(value) &&
            areCacheResultsEqual(this.cache.get(value), result)
        ) {
            return;
        }

        this.cache.set(value, result);
        this.scheduleFlush();
    }

    has(value: string): boolean {
        return this.cache.has(value);
    }

    snapshot(): MLWHCacheSnapshot {
        return Object.fromEntries(this.cache.entries());
    }

    hydrateMissing(snapshot: MLWHCacheSnapshot): void {
        for (const [value, result] of Object.entries(snapshot)) {
            if (this.cache.has(value)) {
                continue;
            }

            this.cache.set(value, result);
        }
    }

    private scheduleFlush(): void {
        if (!this.onChange || this.pendingFlush) {
            return;
        }

        this.pendingFlush = true;
        queueMicrotask(() => {
            this.pendingFlush = false;
            this.onChange?.(this.snapshot());
        });
    }
}

function parseMLWHCacheSnapshot(value: unknown): MLWHCacheSnapshot {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
        return {};
    }

    const snapshot: MLWHCacheSnapshot = {};

    for (const [identifier, candidate] of Object.entries(value)) {
        if (typeof identifier !== "string" || !identifier.trim()) {
            continue;
        }

        if (candidate === null) {
            snapshot[identifier] = null;
            continue;
        }

        const parsed = enrichmentResultSchema.safeParse(candidate);

        if (parsed.success) {
            snapshot[identifier] = parsed.data;
            continue;
        }

        const legacy = legacyEnrichmentResultSchema.safeParse(candidate);

        if (legacy.success) {
            snapshot[identifier] = legacy.data as unknown as EnrichmentResult;
        }
    }

    return snapshot;
}

export function deserializeMLWHCacheCookie(
    cookieValue: string | undefined,
): MLWHCacheSnapshot {
    if (!cookieValue) {
        return {};
    }

    try {
        return parseMLWHCacheSnapshot(
            JSON.parse(decodeURIComponent(cookieValue)),
        );
    } catch {
        return {};
    }
}

export function serializeMLWHCacheCookie(snapshot: MLWHCacheSnapshot): string {
    let persisted: MLWHCacheSnapshot = {};
    let serialized = encodeURIComponent(JSON.stringify(persisted));

    for (const [identifier, result] of Object.entries(snapshot)) {
        if (result !== null) {
            continue;
        }

        const candidate = {
            ...persisted,
            [identifier]: null,
        };
        const candidateSerialized = encodeURIComponent(
            JSON.stringify(candidate),
        );

        if (candidateSerialized.length > MLWH_CACHE_COOKIE_MAX_VALUE_LENGTH) {
            continue;
        }

        persisted = candidate;
        serialized = candidateSerialized;
    }

    return serialized;
}

export function buildMLWHCacheCookie(snapshot: MLWHCacheSnapshot): string {
    return [
        `${MLWH_CACHE_COOKIE_NAME}=${serializeMLWHCacheCookie(snapshot)}`,
        "Path=/",
        `Max-Age=${MLWH_CACHE_COOKIE_MAX_AGE_SECONDS}`,
        "SameSite=Lax",
    ].join("; ");
}

export function readMLWHCacheSnapshotFromCookieHeader(
    cookieHeader: string | undefined,
): MLWHCacheSnapshot {
    if (!cookieHeader) {
        return {};
    }

    const cookiePrefix = `${MLWH_CACHE_COOKIE_NAME}=`;
    const cookie = cookieHeader
        .split(";")
        .map((entry) => entry.trim())
        .find((entry) => entry.startsWith(cookiePrefix));

    if (!cookie) {
        return {};
    }

    return deserializeMLWHCacheCookie(cookie.slice(cookiePrefix.length));
}
