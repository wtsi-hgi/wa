import type { IdentifierResult } from "@/lib/contracts";
import type { SeqmetaCacheStore } from "@/lib/seqmeta-cache-core";

export type SeqmetaEnrichmentState = {
    enrichments: Record<string, IdentifierResult | null>;
    errors: Record<string, boolean>;
};

export function isSeqmetaKey(key: string): boolean {
    return key.startsWith("seqmeta_");
}

export function collectSeqmetaValues(
    metadata: Record<string, string>,
): string[] {
    return Array.from(
        new Set(
            Object.entries(metadata)
                .filter(([key, value]) => isSeqmetaKey(key) && value.trim())
                .map(([, value]) => value.trim()),
        ),
    );
}

export function buildCachedEnrichmentState(
    metadata: Record<string, string>,
    cache: SeqmetaCacheStore,
): SeqmetaEnrichmentState {
    const enrichments: Record<string, IdentifierResult | null> = {};

    for (const value of collectSeqmetaValues(metadata)) {
        if (!cache.has(value)) {
            continue;
        }

        enrichments[value] = cache.get(value) ?? null;
    }

    return { enrichments, errors: {} };
}

export function primeSeqmetaCache(
    cache: SeqmetaCacheStore,
    enrichments: Record<string, IdentifierResult | null>,
): void {
    for (const [value, result] of Object.entries(enrichments)) {
        cache.set(value, result);
    }
}

export async function enrichSeqmetaMetadata(
    metadata: Record<string, string>,
    cache: SeqmetaCacheStore,
    validateIdentifier: (value: string) => Promise<IdentifierResult | null>,
): Promise<SeqmetaEnrichmentState> {
    const state = buildCachedEnrichmentState(metadata, cache);
    const pendingValues = collectSeqmetaValues(metadata).filter(
        (value) => !cache.has(value),
    );

    if (pendingValues.length === 0) {
        return state;
    }

    const settled = await Promise.allSettled(
        pendingValues.map(async (value) => ({
            result: await validateIdentifier(value),
            value,
        })),
    );

    for (const [index, result] of settled.entries()) {
        const value = pendingValues[index];

        if (!value) {
            continue;
        }

        if (result.status === "fulfilled") {
            cache.set(value, result.value.result);
            state.enrichments[value] = result.value.result;
            continue;
        }

        state.errors[value] = true;
    }

    return state;
}
