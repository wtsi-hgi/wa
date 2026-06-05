import { BackendRequestError } from "@/lib/backend-shared";
import type { EnrichmentResult } from "@/lib/contracts";
import type { SeqmetaCacheStore } from "@/lib/seqmeta-cache-core";
import {
    canonicalSeqmetaKey,
    isSeqmetaKeyForUserFacingMlwhMetadataKey,
    isSeqmetaKey as isSeqmetaKeyValue,
    isUserFacingMlwhMetadataKey,
    preferredSeqmetaKeyForUserFacingMlwhMetadataKey,
    type UserFacingMlwhMetadataKey,
} from "@/lib/seqmeta-keys";

export type SeqmetaEnrichmentState = {
    enrichments: Record<string, EnrichmentResult | null>;
    errors: Record<string, "not_found" | "upstream_impaired">;
};

export type SeqmetaEnrichmentLookupResult = {
    enrichment: EnrichmentResult | null;
    error?: "not_found" | "upstream_impaired";
    value: string;
};

type SeqmetaAliasType =
    | "donor_id"
    | "id_library_lims"
    | "library_type"
    | "library_id"
    | "run_id"
    | "sample_accession"
    | "sample_lims_id"
    | "sample_uuid"
    | "sanger_sample_name"
    | "sanger_sample_id"
    | "supplier_name"
    | "study_accession"
    | "study_lims_id"
    | "study_name"
    | "study_uuid";

export function isSeqmetaKey(key: string): boolean {
    return isSeqmetaKeyValue(key);
}

export function hasUsableSeqmetaCacheEntry(
    cache: SeqmetaCacheStore,
    value: string,
): boolean {
    if (!cache.has(value)) {
        return false;
    }

    const cached = cache.get(value);

    return cached !== null;
}

type MetadataValueEntry = {
    index: number;
    key: string;
    value: string;
    valueIndex: number;
};

function nonEmptyMetadataValues(values: string[] | undefined): string[] {
    return (values ?? [])
        .map((value) => value.trim())
        .filter((value) => value.length > 0);
}

function orderedMetadataKeys(
    metadata: Record<string, string>,
    metadataValues: Record<string, string[]> | undefined,
): string[] {
    const keys: string[] = [];
    const seen = new Set<string>();

    for (const key of Object.keys(metadata)) {
        keys.push(key);
        seen.add(key);
    }

    for (const key of Object.keys(metadataValues ?? {})) {
        if (seen.has(key)) {
            continue;
        }

        keys.push(key);
    }

    return keys;
}

function metadataValueEntries(
    metadata: Record<string, string>,
    metadataValues: Record<string, string[]> | undefined,
): MetadataValueEntry[] {
    return orderedMetadataKeys(metadata, metadataValues).flatMap(
        (key, index) => {
            const repeatedValues = nonEmptyMetadataValues(
                metadataValues?.[key],
            );
            const values =
                repeatedValues.length > 0
                    ? repeatedValues
                    : nonEmptyMetadataValues(
                          metadata[key] === undefined ? [] : [metadata[key]],
                      );

            return values.map((value, valueIndex) => ({
                index,
                key,
                value,
                valueIndex,
            }));
        },
    );
}

function pairedMlwhDisplayKeys(
    entries: MetadataValueEntry[],
): Map<UserFacingMlwhMetadataKey, string> {
    const keysWithValues = Array.from(
        new Set(entries.map((entry) => entry.key)),
    );
    const displayKeys = new Map<UserFacingMlwhMetadataKey, string>();

    for (const key of keysWithValues) {
        if (!isUserFacingMlwhMetadataKey(key)) {
            continue;
        }

        const displayKey = preferredSeqmetaKeyForUserFacingMlwhMetadataKey(
            key,
            keysWithValues,
        );

        if (displayKey) {
            displayKeys.set(key, displayKey);
        }
    }

    return displayKeys;
}

function seqmetaMetadataValueEntries(
    metadata: Record<string, string>,
    metadataValues?: Record<string, string[]>,
): MetadataValueEntry[] {
    const entries = metadataValueEntries(metadata, metadataValues);
    const displayKeys = pairedMlwhDisplayKeys(entries);
    const seqmetaEntries: MetadataValueEntry[] = [];

    for (const entry of entries) {
        if (isSeqmetaKey(entry.key)) {
            seqmetaEntries.push(entry);

            continue;
        }

        if (!isUserFacingMlwhMetadataKey(entry.key)) {
            continue;
        }

        const displayKey = displayKeys.get(entry.key);

        if (!displayKey) {
            continue;
        }

        if (isSeqmetaKeyForUserFacingMlwhMetadataKey(entry.key, displayKey)) {
            seqmetaEntries.push({ ...entry, key: displayKey });
        }
    }

    return seqmetaEntries;
}

export function collectSeqmetaValues(
    metadata: Record<string, string>,
    metadataValues?: Record<string, string[]>,
): string[] {
    return Array.from(
        new Set(
            seqmetaMetadataValueEntries(metadata, metadataValues).map(
                (entry) => entry.value,
            ),
        ),
    );
}

function seqmetaLookupPriority(metadataKey: string): number {
    const canonicalKey = canonicalSeqmetaKey(metadataKey);

    if (
        canonicalKey === "seqmeta_id_study_lims" ||
        canonicalKey === "seqmeta_study_accession" ||
        canonicalKey === "seqmeta_uuid_study_lims" ||
        canonicalKey === "seqmeta_study_name"
    ) {
        return 0;
    }

    if (
        canonicalKey === "seqmeta_sample_name" ||
        canonicalKey === "seqmeta_supplier_name" ||
        canonicalKey === "seqmeta_sanger_sample_id" ||
        canonicalKey === "seqmeta_id_sample_lims" ||
        canonicalKey === "seqmeta_accession_number" ||
        canonicalKey === "seqmeta_uuid_sample_lims" ||
        canonicalKey === "seqmeta_donor_id"
    ) {
        return 1;
    }

    if (canonicalKey === "seqmeta_pipeline_id_lims") {
        return 4;
    }

    if (
        canonicalKey === "seqmeta_library_id" ||
        canonicalKey === "seqmeta_id_library_lims"
    ) {
        return 3;
    }

    return 2;
}

function collectSeqmetaLookupValues(
    metadata: Record<string, string>,
    metadataValues?: Record<string, string[]>,
): string[] {
    const plannedLookups = new Map<
        string,
        { index: number; priority: number; value: string; valueIndex: number }
    >();

    for (const { index, key, value, valueIndex } of seqmetaMetadataValueEntries(
        metadata,
        metadataValues,
    )) {
        if (!isSeqmetaKey(key)) {
            continue;
        }

        const priority = seqmetaLookupPriority(key);
        const existing = plannedLookups.get(value);

        if (
            existing &&
            (existing.priority < priority ||
                (existing.priority === priority &&
                    (existing.index < index ||
                        (existing.index === index &&
                            existing.valueIndex <= valueIndex))))
        ) {
            continue;
        }

        plannedLookups.set(value, {
            index,
            priority,
            value,
            valueIndex,
        });
    }

    return Array.from(plannedLookups.values())
        .sort(
            (left, right) =>
                left.priority - right.priority ||
                left.index - right.index ||
                left.valueIndex - right.valueIndex,
        )
        .map((entry) => entry.value);
}

export function buildCachedEnrichmentState(
    metadata: Record<string, string>,
    cache: SeqmetaCacheStore,
    metadataValues?: Record<string, string[]>,
): SeqmetaEnrichmentState {
    const enrichments: Record<string, EnrichmentResult | null> = {};
    const errors: Record<string, "not_found" | "upstream_impaired"> = {};

    for (const value of collectSeqmetaValues(metadata, metadataValues)) {
        if (!hasUsableSeqmetaCacheEntry(cache, value)) {
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
    add(enrichment.graph.study?.id_study_lims, "study_lims_id");
    add(enrichment.graph.study?.accession_number, "study_accession");
    add(enrichment.graph.study?.uuid_study_lims, "study_uuid");
    add(enrichment.graph.study?.name, "study_name");
    add(enrichment.graph.library?.id_study_lims, "study_lims_id");
    add(enrichment.graph.library?.library_type, "library_type");
    add(enrichment.graph.library?.library_id, "library_id");
    add(enrichment.graph.library?.id_library_lims, "id_library_lims");

    for (const library of enrichment.graph.libraries ?? []) {
        add(library.id_study_lims, "study_lims_id");
        add(library.library_type, "library_type");
        add(library.library_id, "library_id");
        add(library.id_library_lims, "id_library_lims");
    }

    function addSampleAliases(sample: {
        accession_number?: string;
        id_sample_lims?: string;
        id_study_lims?: string;
        library_type?: string;
        sanger_id?: string;
        sample_name?: string;
        supplier_name?: string;
        study_accession_number?: string;
        id_run?: number;
    }) {
        add(sample.sample_name, "sanger_sample_name");
        add(sample.sanger_id, "sanger_sample_id");
        add(sample.supplier_name, "supplier_name");
        add(sample.id_sample_lims, "sample_lims_id");
        add(sample.id_study_lims, "study_lims_id");
        add(sample.study_accession_number, "study_accession");
        add(sample.accession_number, "sample_accession");
        add(sample.library_type, "library_type");
        add(
            typeof sample.id_run === "number" ? String(sample.id_run) : null,
            "run_id",
        );
    }

    if (enrichment.graph.sample) {
        addSampleAliases(enrichment.graph.sample);
    }

    for (const lane of enrichment.graph.sample_detail?.lanes ?? []) {
        add(lane.id_run, "run_id");
    }

    return Array.from(aliases.entries());
}

export function primeSeqmetaCacheEntry(
    cache: SeqmetaCacheStore,
    enrichment: EnrichmentResult,
): void {
    for (const [value, type] of collectSeqmetaAliases(enrichment)) {
        const cached = cache.get(value);

        if (cached !== null && cached !== undefined) {
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
    metadataValues?: Record<string, string[]>,
): Promise<SeqmetaEnrichmentState> {
    const state = buildCachedEnrichmentState(metadata, cache, metadataValues);
    const pendingValues = collectSeqmetaLookupValues(
        metadata,
        metadataValues,
    ).filter((value) => !hasUsableSeqmetaCacheEntry(cache, value));

    if (pendingValues.length === 0) {
        return state;
    }

    if (pendingValues.length > 0) {
        const results = await Promise.all(
            pendingValues.map(async (value) => {
                if (hasUsableSeqmetaCacheEntry(cache, value)) {
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

                    cache.set(value, result);
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

        for (const result of results) {
            state.enrichments[result.value] = result.enrichment;
            if (result.error) {
                state.errors[result.value] = result.error;
            }
        }
    }

    return mergeSeqmetaEnrichmentState(
        state,
        buildCachedEnrichmentState(metadata, cache, metadataValues),
    );
}

export async function enrichSeqmetaMetadataBatch(
    metadata: Record<string, string>,
    cache: SeqmetaCacheStore,
    enrichIdentifiers: (
        values: string[],
    ) => Promise<SeqmetaEnrichmentLookupResult[]>,
    metadataValues?: Record<string, string[]>,
): Promise<SeqmetaEnrichmentState> {
    const state = buildCachedEnrichmentState(metadata, cache, metadataValues);
    const pendingValues = collectSeqmetaLookupValues(
        metadata,
        metadataValues,
    ).filter((value) => !hasUsableSeqmetaCacheEntry(cache, value));

    if (pendingValues.length === 0) {
        return state;
    }

    const results = await enrichIdentifiers(pendingValues);

    for (const result of results) {
        if (result.enrichment === null) {
            cache.set(result.value, null);
        } else {
            cache.set(result.value, result.enrichment);
            primeSeqmetaCacheEntry(cache, result.enrichment);
        }

        const enrichment = cache.get(result.value) ?? result.enrichment;
        state.enrichments[result.value] = enrichment;
        if (result.error) {
            state.errors[result.value] = result.error;
        }
    }

    return mergeSeqmetaEnrichmentState(
        state,
        buildCachedEnrichmentState(metadata, cache, metadataValues),
    );
}

export async function fetchLibrarySamples(
    studyId: string,
    libraryType: string,
    filters?: { idLibraryLims?: string; libraryId?: string },
): Promise<EnrichmentResult["graph"]["samples"]> {
    const { fetchStudyLibrarySamples } =
        await import("@/app/(results)/actions");

    return filters === undefined
        ? fetchStudyLibrarySamples(studyId, libraryType)
        : fetchStudyLibrarySamples(studyId, libraryType, filters);
}
