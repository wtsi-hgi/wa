"use server";

import { cookies } from "next/headers";

import {
    BackendRequestError,
    resultsAuthCookieName,
    resultsJson,
    resultsRaw,
    seqmetaJson,
} from "@/lib/backend-client";
import {
    enrichmentResultSchema,
    enrichmentSamplesSchema,
    errorSchema,
    fileEntrySchema,
    identifierResultSchema,
    metaKeysSchema,
    resultSetSchema,
    sampleSchema,
    searchResultSchema,
    statsResultSchema,
    type EnrichmentResult,
    type FileEntry,
    type IdentifierResult,
    type ResultSet,
    type SearchResult,
    type StatsResult,
    type Study,
} from "@/lib/contracts";
import { buildSearchQuery } from "@/lib/search-params";
import { SeqmetaCache } from "@/lib/seqmeta-cache-core";
import { primeSeqmetaCacheEntry } from "@/lib/seqmeta-enrichment";
import { getStudies } from "@/lib/studies-cache";

function buildQueryString(params: Record<string, string[]>): string {
    const rendered = buildSearchQuery(params).toString();

    return rendered ? `?${rendered}` : "";
}

function buildStatsQuery(recent?: number, days?: number): string {
    const query = new URLSearchParams();

    if (typeof recent === "number") {
        query.set("recent", String(recent));
    }

    if (typeof days === "number") {
        query.set("days", String(days));
    }

    const rendered = query.toString();

    return rendered ? `?${rendered}` : "";
}

async function readResultsAuthJwt(): Promise<string | null> {
    try {
        const cookieStore = await cookies();

        return cookieStore.get(resultsAuthCookieName)?.value ?? null;
    } catch {
        return null;
    }
}

function resultsCollectionPath(jwt: string | null): string {
    return jwt ? "/rest/v1/auth/results" : "/rest/v1/results";
}

function authenticatedOptions(jwt: string | null): { jwt: string } | undefined {
    return jwt ? { jwt } : undefined;
}

export async function fetchStats(
    recent?: number,
    days?: number,
): Promise<StatsResult> {
    const jwt = await readResultsAuthJwt();

    return resultsJson(
        `${resultsCollectionPath(jwt)}/stats${buildStatsQuery(recent, days)}`,
        statsResultSchema,
        authenticatedOptions(jwt),
    );
}

/**
 * Runs the hierarchical results search, including library= expansion via the
 * backend. The first call for a cold library= lookup can take longer while the
 * MLWH cache warms, so operators can run wa mlwh sync ahead of time to avoid
 * that delay.
 */
export async function searchResults(
    params: Record<string, string[]>,
): Promise<ResultSet[] | SearchResult[]> {
    const jwt = await readResultsAuthJwt();

    return resultsJson(
        `${resultsCollectionPath(jwt)}${buildQueryString(params)}`,
        resultSetSchema.array().or(searchResultSchema.array()),
        authenticatedOptions(jwt),
    );
}

export async function fetchMetaKeys(): Promise<string[]> {
    return resultsJson("/rest/v1/results/meta-keys", metaKeysSchema);
}

export async function fetchStudies(): Promise<Study[]> {
    return getStudies();
}

export async function fetchResult(id: string): Promise<ResultSet> {
    const jwt = await readResultsAuthJwt();

    return resultsJson(
        `${resultsCollectionPath(jwt)}/${encodeURIComponent(id)}`,
        resultSetSchema,
        authenticatedOptions(jwt),
    );
}

export async function fetchFiles(id: string): Promise<FileEntry[]> {
    const jwt = await readResultsAuthJwt();

    return resultsJson(
        `${resultsCollectionPath(jwt)}/${encodeURIComponent(id)}/files`,
        fileEntrySchema.array(),
        authenticatedOptions(jwt),
    );
}

export async function fetchFileContent(
    id: string,
    path: string,
): Promise<{ content: string; contentType: string }> {
    const jwt = await readResultsAuthJwt();
    const response = await resultsRaw(
        `${resultsCollectionPath(jwt)}/${encodeURIComponent(id)}/file?path=${encodeURIComponent(path)}`,
        authenticatedOptions(jwt),
    );

    if (!response.ok) {
        const contentType = response.headers.get("content-type") ?? "";
        const body = contentType.includes("application/json")
            ? await response.json().catch(() => null)
            : await response.text();
        const fileSizeHeader = response.headers.get("x-file-size");

        throw new BackendRequestError(
            response.status,
            response.status === 413
                ? {
                      body,
                      fileSize: fileSizeHeader
                          ? Number(fileSizeHeader)
                          : undefined,
                  }
                : body,
        );
    }

    return {
        content: await response.text(),
        contentType: response.headers.get("content-type") ?? "text/plain",
    };
}

export async function validateIdentifier(
    value: string,
): Promise<IdentifierResult | null> {
    const trimmed = value.trim();
    if (!trimmed) {
        return null;
    }

    try {
        return await seqmetaJson(
            `/validate/${encodeURIComponent(trimmed)}`,
            identifierResultSchema,
        );
    } catch (error) {
        if (error instanceof BackendRequestError && error.status === 404) {
            return null;
        }

        throw error;
    }
}

export async function enrichIdentifier(
    value: string,
): Promise<EnrichmentResult | null> {
    const trimmed = value.trim();
    if (!trimmed) {
        return null;
    }

    try {
        return await seqmetaJson(
            `/enrich/${encodeURIComponent(trimmed)}`,
            enrichmentResultSchema,
        );
    } catch (error) {
        if (error instanceof BackendRequestError && error.status === 404) {
            return null;
        }

        throw error;
    }
}

export type EnrichmentLookupResult = {
    enrichment: EnrichmentResult | null;
    error?: "not_found" | "upstream_impaired";
    value: string;
};

export async function enrichIdentifiers(
    values: string[],
): Promise<EnrichmentLookupResult[]> {
    const uniqueValues = Array.from(
        new Set(values.map((value) => value.trim()).filter(Boolean)),
    );
    const cache = new SeqmetaCache();
    const results: EnrichmentLookupResult[] = [];

    for (const value of uniqueValues) {
        const cached = cache.get(value);
        if (cached !== undefined) {
            if (cached !== null) {
                continue;
            }

            results.push({
                value,
                enrichment: cached,
                error: cached === null ? "not_found" : undefined,
            });
            continue;
        }

        try {
            const enrichment = await enrichIdentifier(value);

            if (enrichment === null) {
                cache.set(value, null);
                results.push({ value, enrichment: null, error: "not_found" });
                continue;
            }

            cache.set(value, enrichment);
            primeSeqmetaCacheEntry(cache, enrichment);
            results.push({
                value,
                enrichment: cache.get(value) ?? enrichment,
            });
        } catch (error) {
            if (error instanceof BackendRequestError && error.status === 404) {
                cache.set(value, null);
                results.push({ value, enrichment: null, error: "not_found" });
                continue;
            }

            results.push({
                value,
                enrichment: null,
                error: "upstream_impaired",
            });
        }
    }

    return results;
}

export async function fetchStudySamples(studyId: string): Promise<string[]> {
    const samples = await seqmetaJson(
        `/study/${encodeURIComponent(studyId)}/samples`,
        sampleSchema.array(),
    );

    return samples.map((sample) => sample.sanger_id);
}

export async function fetchStudyLibrarySamples(
    studyId: string,
    libraryType: string,
    filters: { idLibraryLims?: string; libraryId?: string } = {},
) {
    const params = new URLSearchParams({ library_type: libraryType });
    if (filters.libraryId) {
        params.set("library_id", filters.libraryId);
    }
    if (filters.idLibraryLims) {
        params.set("id_library_lims", filters.idLibraryLims);
    }

    return seqmetaJson(
        `/study/${encodeURIComponent(studyId)}/samples?${params.toString()}`,
        enrichmentSamplesSchema,
    );
}

export async function parseBackendError(response: Response): Promise<string> {
    const body = await response.json().catch(() => null);
    const parsed = errorSchema.safeParse(body);

    return parsed.success ? parsed.data.error : "Request failed";
}
