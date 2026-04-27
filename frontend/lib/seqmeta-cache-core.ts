import { enrichmentResultSchema, type EnrichmentResult } from "@/lib/contracts";

export const SEQMETA_CACHE_COOKIE_NAME = "wa-seqmeta-cache";
const SEQMETA_CACHE_COOKIE_MAX_AGE_SECONDS = 60 * 60 * 24 * 7;

export type SeqmetaCacheSnapshot = Record<string, EnrichmentResult | null>;

type SeqmetaCacheChangeListener = (snapshot: SeqmetaCacheSnapshot) => void;

export interface SeqmetaCacheStore {
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

    if (left == null || right == null) {
        return left === right;
    }

    return JSON.stringify(left) === JSON.stringify(right);
}

export class SeqmetaCache implements SeqmetaCacheStore {
    private cache: Map<string, EnrichmentResult | null>;

    constructor(
        snapshot: SeqmetaCacheSnapshot = {},
        private readonly onChange?: SeqmetaCacheChangeListener,
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
        this.onChange?.(this.snapshot());
    }

    has(value: string): boolean {
        return this.cache.has(value);
    }

    snapshot(): SeqmetaCacheSnapshot {
        return Object.fromEntries(this.cache.entries());
    }
}

function parseSeqmetaCacheSnapshot(value: unknown): SeqmetaCacheSnapshot {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
        return {};
    }

    const snapshot: SeqmetaCacheSnapshot = {};

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
        }
    }

    return snapshot;
}

export function deserializeSeqmetaCacheCookie(
    cookieValue: string | undefined,
): SeqmetaCacheSnapshot {
    if (!cookieValue) {
        return {};
    }

    try {
        return parseSeqmetaCacheSnapshot(
            JSON.parse(decodeURIComponent(cookieValue)),
        );
    } catch {
        return {};
    }
}

export function serializeSeqmetaCacheCookie(
    snapshot: SeqmetaCacheSnapshot,
): string {
    return encodeURIComponent(JSON.stringify(snapshot));
}

export function buildSeqmetaCacheCookie(
    snapshot: SeqmetaCacheSnapshot,
): string {
    return [
        `${SEQMETA_CACHE_COOKIE_NAME}=${serializeSeqmetaCacheCookie(snapshot)}`,
        "Path=/",
        `Max-Age=${SEQMETA_CACHE_COOKIE_MAX_AGE_SECONDS}`,
        "SameSite=Lax",
    ].join("; ");
}

export function readSeqmetaCacheSnapshotFromCookieHeader(
    cookieHeader: string | undefined,
): SeqmetaCacheSnapshot {
    if (!cookieHeader) {
        return {};
    }

    const cookiePrefix = `${SEQMETA_CACHE_COOKIE_NAME}=`;
    const cookie = cookieHeader
        .split(";")
        .map((entry) => entry.trim())
        .find((entry) => entry.startsWith(cookiePrefix));

    if (!cookie) {
        return {};
    }

    return deserializeSeqmetaCacheCookie(cookie.slice(cookiePrefix.length));
}
