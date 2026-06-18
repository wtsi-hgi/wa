import { mlwhJson } from "@/lib/backend-client";
import { studiesSchema, type Study } from "@/lib/contracts";

type StudiesCache = {
    fetchedAt: number;
    studies: Study[];
};

const DEFAULT_TTL_SECONDS = 300;

let studiesCache: StudiesCache | null = null;

function getCacheTtlMilliseconds(): number {
    const rawTtl = process.env.WA_STUDIES_CACHE_TTL_SECONDS?.trim();
    const parsedTtl = rawTtl ? Number.parseInt(rawTtl, 10) : Number.NaN;
    const ttlSeconds =
        Number.isFinite(parsedTtl) && parsedTtl >= 0
            ? parsedTtl
            : DEFAULT_TTL_SECONDS;

    return ttlSeconds * 1000;
}

export async function getStudies(): Promise<Study[]> {
    const now = Date.now();

    if (
        studiesCache &&
        now - studiesCache.fetchedAt < getCacheTtlMilliseconds()
    ) {
        return studiesCache.studies;
    }

    const studies = await mlwhJson("/studies", studiesSchema);

    studiesCache = {
        fetchedAt: now,
        studies,
    };

    return studies;
}

export function resetStudiesCache(): void {
    studiesCache = null;
}
