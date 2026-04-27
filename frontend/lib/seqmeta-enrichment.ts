import { BackendRequestError } from "@/lib/backend-client";
import type { EnrichmentResult } from "@/lib/contracts";
import type { SeqmetaCacheStore } from "@/lib/seqmeta-cache-core";

export type SeqmetaEnrichmentState = {
    enrichments: Record<string, EnrichmentResult | null>;
    errors: Record<string, "not_found" | "upstream_impaired">;
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
    const enrichments: Record<string, EnrichmentResult | null> = {};
    const errors: Record<string, "not_found" | "upstream_impaired"> = {};

    for (const value of collectSeqmetaValues(metadata)) {
        if (!cache.has(value)) {
            continue;
        }

        const enrichment = cache.get(value) ?? null;
        enrichments[value] = enrichment;

        if (enrichment === null) {
            errors[value] = "not_found";
        }
    }

    return { enrichments, errors };
}

export function primeSeqmetaCache(
    cache: SeqmetaCacheStore,
    enrichments: Record<string, EnrichmentResult | null>,
): void {
    for (const [value, result] of Object.entries(enrichments)) {
        cache.set(value, result);
    }
}

export function mergeSeqmetaEnrichmentState(
    base: SeqmetaEnrichmentState,
    override: Partial<SeqmetaEnrichmentState>,
): SeqmetaEnrichmentState {
    const enrichments = {
        ...base.enrichments,
        ...override.enrichments,
    };
    const errors = {
        ...base.errors,
        ...override.errors,
    };

    for (const [value, enrichment] of Object.entries(enrichments)) {
        if (enrichment !== null) {
            delete errors[value];
        }
    }

    return { enrichments, errors };
}

export async function enrichSeqmetaMetadata(
    metadata: Record<string, string>,
    cache: SeqmetaCacheStore,
    enrichIdentifier: (value: string) => Promise<EnrichmentResult | null>,
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
            result: await enrichIdentifier(value),
            value,
        })),
    );

    for (const [index, result] of settled.entries()) {
        const value = pendingValues[index];

        if (!value) {
            continue;
        }

        if (result.status === "fulfilled") {
            if (result.value.result === null) {
                cache.set(value, null);
                state.enrichments[value] = null;
                state.errors[value] = "not_found";
                continue;
            }

            cache.set(value, result.value.result);
            state.enrichments[value] = result.value.result;
            continue;
        }

        state.errors[value] =
            result.reason instanceof BackendRequestError &&
            result.reason.status === 404
                ? "not_found"
                : "upstream_impaired";
    }

    return state;
}
