import { BackendRequestError } from "@/lib/backend-client";
import type { EnrichmentResult } from "@/lib/contracts";
import type { SeqmetaCacheStore } from "@/lib/seqmeta-cache-core";

export type SeqmetaEnrichmentState = {
    enrichments: Record<string, EnrichmentResult | null>;
    errors: Record<string, "not_found" | "upstream_impaired">;
};

type SeqmetaAliasType =
    | "sample_lims"
    | "sanger_sample_id"
    | "study_accession"
    | "study_id";

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

function seqmetaLookupPriority(metadataKey: string): number {
    if (
        metadataKey === "seqmeta_studyid" ||
        metadataKey === "seqmeta_study_accession"
    ) {
        return 0;
    }

    if (
        metadataKey === "seqmeta_sampleid" ||
        metadataKey === "seqmeta_sample_lims"
    ) {
        return 1;
    }

    if (metadataKey === "seqmeta_library") {
        return 3;
    }

    return 2;
}

function collectSeqmetaLookupValues(
    metadata: Record<string, string>,
): string[] {
    const plannedLookups = new Map<
        string,
        { index: number; priority: number; value: string }
    >();

    for (const [index, [key, rawValue]] of Object.entries(metadata).entries()) {
        if (!isSeqmetaKey(key)) {
            continue;
        }

        const value = rawValue.trim();

        if (!value) {
            continue;
        }

        const priority = seqmetaLookupPriority(key);
        const existing = plannedLookups.get(value);

        if (
            existing &&
            (existing.priority < priority ||
                (existing.priority === priority && existing.index <= index))
        ) {
            continue;
        }

        plannedLookups.set(value, {
            index,
            priority,
            value,
        });
    }

    return Array.from(plannedLookups.values())
        .sort(
            (left, right) =>
                left.priority - right.priority || left.index - right.index,
        )
        .map((entry) => entry.value);
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

function cloneEnrichmentForAlias(
    enrichment: EnrichmentResult,
    identifier: string,
    type: string,
): EnrichmentResult {
    if (enrichment.identifier === identifier && enrichment.type === type) {
        return enrichment;
    }

    return {
        ...enrichment,
        identifier,
        type,
    };
}

function trimSeqmetaValue(value: string | null | undefined): string | null {
    if (typeof value !== "string") {
        return null;
    }

    const trimmed = value.trim();

    return trimmed ? trimmed : null;
}

function collectSeqmetaAliases(
    enrichment: EnrichmentResult,
): Array<[string, SeqmetaAliasType | string]> {
    const aliases = new Map<string, SeqmetaAliasType | string>();

    function add(value: string | null | undefined, type: SeqmetaAliasType) {
        const trimmed = trimSeqmetaValue(value);

        if (!trimmed || aliases.has(trimmed)) {
            return;
        }

        aliases.set(trimmed, type);
    }

    add(enrichment.identifier, enrichment.type as SeqmetaAliasType);
    add(enrichment.graph.study?.id_study_lims, "study_id");
    add(enrichment.graph.study?.accession_number, "study_accession");
    add(enrichment.graph.library?.id_study_lims, "study_id");

    for (const library of enrichment.graph.libraries ?? []) {
        add(library.id_study_lims, "study_id");
    }

    function addSampleAliases(sample: {
        accession_number?: string;
        id_sample_lims?: string;
        id_study_lims?: string;
        library_type?: string;
        sanger_id?: string;
        study_accession_number?: string;
    }) {
        add(sample.sanger_id, "sanger_sample_id");
        add(sample.id_sample_lims, "sample_lims");
        add(sample.id_study_lims, "study_id");
        add(sample.study_accession_number, "study_accession");
        add(sample.accession_number, "sanger_sample_id");
    }

    if (enrichment.graph.sample) {
        addSampleAliases(enrichment.graph.sample);
    }

    for (const sample of enrichment.graph.samples ?? []) {
        addSampleAliases(sample);
    }

    return Array.from(aliases.entries());
}

function primeSeqmetaCacheEntry(
    cache: SeqmetaCacheStore,
    enrichment: EnrichmentResult,
): void {
    for (const [value, type] of collectSeqmetaAliases(enrichment)) {
        if (cache.has(value)) {
            continue;
        }

        cache.set(value, cloneEnrichmentForAlias(enrichment, value, type));
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
    const pendingValues = collectSeqmetaLookupValues(metadata).filter(
        (value) => !cache.has(value),
    );

    if (pendingValues.length === 0) {
        return state;
    }

    // Hybrid approach: enrich the first (highest priority) value sequentially
    // to prime cache aliases, then parallelize remaining lookups
    const [firstValue, ...remainingValues] = pendingValues;

    // First value enrichment (sequential to populate cache aliases)
    if (!cache.has(firstValue)) {
        try {
            const result = await enrichIdentifier(firstValue);

            if (result === null) {
                cache.set(firstValue, null);
                state.enrichments[firstValue] = null;
                state.errors[firstValue] = "not_found";
            } else {
                primeSeqmetaCacheEntry(cache, result);
                state.enrichments[firstValue] = cache.get(firstValue) ?? result;
            }
        } catch (error) {
            if (error instanceof BackendRequestError && error.status === 404) {
                cache.set(firstValue, null);
                state.enrichments[firstValue] = null;
                state.errors[firstValue] = "not_found";
            } else {
                state.errors[firstValue] = "upstream_impaired";
            }
        }
    }

    // Remaining values in parallel (after cache may be primed)
    const stillPending = remainingValues.filter((value) => !cache.has(value));

    if (stillPending.length > 0) {
        const results = await Promise.all(
            stillPending.map(async (value) => {
                // Double-check cache in case first lookup populated it
                if (cache.has(value)) {
                    const enrichment = cache.get(value) ?? null;
                    return { value, enrichment, error: null };
                }

                try {
                    const result = await enrichIdentifier(value);

                    if (result === null) {
                        cache.set(value, null);
                        return {
                            value,
                            enrichment: null,
                            error: "not_found" as const,
                        };
                    }

                    primeSeqmetaCacheEntry(cache, result);
                    const enrichment = cache.get(value) ?? result;
                    return { value, enrichment, error: null };
                } catch (error) {
                    if (
                        error instanceof BackendRequestError &&
                        error.status === 404
                    ) {
                        cache.set(value, null);
                        return {
                            value,
                            enrichment: null,
                            error: "not_found" as const,
                        };
                    }

                    return {
                        value,
                        enrichment: null,
                        error: "upstream_impaired" as const,
                    };
                }
            }),
        );

        // Apply results to state
        for (const result of results) {
            state.enrichments[result.value] = result.enrichment;
            if (result.error) {
                state.errors[result.value] = result.error;
            }
        }
    }

    return mergeSeqmetaEnrichmentState(
        state,
        buildCachedEnrichmentState(metadata, cache),
    );
}
