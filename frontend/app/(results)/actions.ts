"use server";

import {
    BackendRequestError,
    resultsJson,
    resultsRaw,
    seqmetaJson,
} from "@/lib/backend-client";
import {
    enrichmentResultSchema,
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

export async function fetchStats(
    recent?: number,
    days?: number,
): Promise<StatsResult> {
    return resultsJson(
        `/results/stats${buildStatsQuery(recent, days)}`,
        statsResultSchema,
    );
}

export async function searchResults(
    params: Record<string, string[]>,
): Promise<ResultSet[] | SearchResult[]> {
    return resultsJson(
        `/results${buildQueryString(params)}`,
        resultSetSchema.array().or(searchResultSchema.array()),
    );
}

export async function fetchMetaKeys(): Promise<string[]> {
    return resultsJson("/results/meta-keys", metaKeysSchema);
}

export async function fetchStudies(): Promise<Study[]> {
    return getStudies();
}

export async function fetchResult(id: string): Promise<ResultSet> {
    return resultsJson(`/results/${encodeURIComponent(id)}`, resultSetSchema);
}

export async function fetchFiles(id: string): Promise<FileEntry[]> {
    return resultsJson(
        `/results/${encodeURIComponent(id)}/files`,
        fileEntrySchema.array(),
    );
}

export async function fetchFileContent(
    id: string,
    path: string,
): Promise<{ content: string; contentType: string }> {
    const response = await resultsRaw(
        `/results/${encodeURIComponent(id)}/file?path=${encodeURIComponent(path)}`,
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

export async function fetchStudySamples(studyId: string): Promise<string[]> {
    const samples = await seqmetaJson(
        `/study/${encodeURIComponent(studyId)}/samples`,
        sampleSchema.array(),
    );

    return samples.map((sample) => sample.sanger_id);
}

export async function parseBackendError(response: Response): Promise<string> {
    const body = await response.json().catch(() => null);
    const parsed = errorSchema.safeParse(body);

    return parsed.success ? parsed.data.error : "Request failed";
}
