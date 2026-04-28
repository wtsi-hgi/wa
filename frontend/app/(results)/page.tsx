import { DashboardToast } from "@/components/dashboard-toast";
import { FilterBuilder } from "@/components/filter-builder";
import { ResultsTable } from "@/components/results-table";
import type { FilterSuggestionMap } from "@/components/filter-builder";
import {
    fetchStudies,
    fetchMetaKeys,
    fetchStats,
    searchResults,
} from "@/app/(results)/actions";
import type {
    ResultSet,
    SearchResult,
    StatsResult,
    Study,
} from "@/lib/contracts";
import { parseSearchFilters } from "@/lib/search-params";

type SearchParams = Record<string, string | string[] | undefined>;

const emptyStats: StatsResult = {
    total: 0,
    recent: [],
    daily: [],
    pipelines: [],
};

function appendSuggestion(
    suggestions: FilterSuggestionMap,
    key: string,
    value: string,
) {
    const trimmedValue = value.trim();
    if (!trimmedValue) {
        return;
    }

    const entries = suggestions[key] ?? [];
    const alreadyPresent = entries.some(
        (entry) => entry.toLowerCase() === trimmedValue.toLowerCase(),
    );

    if (!alreadyPresent) {
        suggestions[key] = [...entries, trimmedValue];
    }
}

function toSuggestionResult(
    entry: ResultSet | SearchResult,
): SearchResult | null {
    return "result_set" in entry ? entry : null;
}

function toResultSet(entry: ResultSet | SearchResult): ResultSet {
    return "result_set" in entry ? entry.result_set : entry;
}

function toMetaSuggestionKey(metaKey: string): string {
    return metaKey.startsWith("seqmeta_") ? metaKey : `meta_${metaKey}`;
}

function buildSuggestionValues(
    stats: StatsResult,
    tableData: ResultSet[] | SearchResult[],
    studies: Study[],
): FilterSuggestionMap {
    const suggestions: FilterSuggestionMap = {};
    const entries = [...stats.recent, ...tableData];

    for (const study of studies) {
        appendSuggestion(suggestions, "study_id", study.id_study_lims);
    }

    for (const pipeline of stats.pipelines) {
        appendSuggestion(suggestions, "pipeline_name", pipeline.pipeline_name);
    }

    for (const entry of entries) {
        const result = toResultSet(entry);
        const searchResult = toSuggestionResult(entry);

        appendSuggestion(suggestions, "user", result.requester);
        appendSuggestion(suggestions, "operator", result.operator);
        appendSuggestion(suggestions, "pipeline_name", result.pipeline_name);
        appendSuggestion(
            suggestions,
            "pipeline_version",
            result.pipeline_version,
        );
        appendSuggestion(
            suggestions,
            "pipeline_identifier",
            result.pipeline_identifier,
        );
        appendSuggestion(suggestions, "run_key", result.run_key);
        appendSuggestion(
            suggestions,
            "output_dir_prefix",
            result.output_directory,
        );

        for (const [metaKey, metaValue] of Object.entries(result.metadata)) {
            appendSuggestion(
                suggestions,
                toMetaSuggestionKey(metaKey),
                metaValue,
            );
        }

        for (const sampleId of searchResult?.matched_samples ?? []) {
            appendSuggestion(suggestions, "seqmeta_sampleid", sampleId);
        }
    }

    return suggestions;
}

function normalizeSearchParams(
    searchParams: SearchParams,
): Record<string, string[]> {
    const params = new URLSearchParams();

    for (const [key, value] of Object.entries(searchParams)) {
        const values = Array.isArray(value) ? value : value ? [value] : [];

        for (const entry of values) {
            params.append(key, entry);
        }
    }

    return parseSearchFilters(params);
}

function buildReturnHref(searchParams: SearchParams): string {
    const params = new URLSearchParams();

    for (const [key, value] of Object.entries(searchParams)) {
        const values = Array.isArray(value) ? value : value ? [value] : [];

        for (const entry of values) {
            params.append(key, entry);
        }
    }

    const query = params.toString();

    return query ? `/?${query}` : "/";
}

function getErrorMessage(error: unknown, fallback: string): string {
    if (error instanceof Error && error.message.trim()) {
        return error.message;
    }

    return fallback;
}

export const dynamic = "force-dynamic";

export default async function ResultsLandingPage({
    searchParams,
}: {
    searchParams?: Promise<SearchParams>;
}) {
    const rawSearchParams = (await searchParams) ?? {};
    const resolvedSearchParams = normalizeSearchParams(rawSearchParams);
    const returnHref = buildReturnHref(rawSearchParams);
    const hasSearch = Object.keys(resolvedSearchParams).length > 0;
    const studyActive = (resolvedSearchParams.study_id?.length ?? 0) > 0;

    let stats = emptyStats;
    let statsError: string | null = null;
    let metaKeys: string[] = [];
    let studies: Study[] = [];
    const seqmetaAvailable = Boolean(
        process.env.WA_SEQMETA_BACKEND_URL?.trim(),
    );
    const statsPromise = fetchStats(10, 30);
    const metaKeysPromise = fetchMetaKeys();
    const studiesPromise = seqmetaAvailable
        ? fetchStudies()
        : Promise.resolve<Study[]>([]);

    try {
        stats = await statsPromise;
    } catch (error) {
        statsError = getErrorMessage(
            error,
            "Unable to load dashboard statistics",
        );
    }

    try {
        const loadedMetaKeys = await metaKeysPromise;
        metaKeys = Array.isArray(loadedMetaKeys) ? loadedMetaKeys : [];
    } catch (error) {
        statsError =
            statsError ??
            getErrorMessage(error, "Unable to load filter fields");
    }

    if (seqmetaAvailable) {
        try {
            const loadedStudies = await studiesPromise;
            studies = Array.isArray(loadedStudies) ? loadedStudies : [];
        } catch (error) {
            statsError =
                statsError ?? getErrorMessage(error, "Unable to load studies");
        }
    }

    let tableData: ResultSet[] | SearchResult[] = stats.recent;
    let tableMode: "recent" | "search" = "recent";
    let tableEmptyMessage = "No recent results yet.";

    if (hasSearch) {
        tableMode = "search";
        tableEmptyMessage = "No result sets matched the current search.";

        try {
            tableData = await searchResults(resolvedSearchParams);
        } catch (error) {
            tableData = [];
            statsError =
                statsError ??
                getErrorMessage(error, "Unable to search results");
        }
    }

    const suggestionValues = buildSuggestionValues(stats, tableData, studies);

    return (
        <main className="mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-6 px-6 py-8 sm:px-10 lg:px-12 lg:py-10">
            <DashboardToast message={statsError} />

            <section className="rounded-[2rem] border border-border/70 bg-[linear-gradient(135deg,color-mix(in_oklab,var(--card)_90%,white_10%),color-mix(in_oklab,var(--accent)_10%,var(--card)_90%))] p-4 shadow-[0_32px_110px_-76px_rgba(41,58,85,0.82)] sm:p-6">
                <FilterBuilder
                    currentFilters={resolvedSearchParams}
                    metaKeys={metaKeys}
                    seqmetaAvailable={seqmetaAvailable}
                    suggestionValues={suggestionValues}
                    studies={studies}
                />
            </section>

            <ResultsTable
                data={tableData}
                emptyMessage={tableEmptyMessage}
                mode={tableMode}
                returnHref={returnHref}
                studyActive={studyActive}
            />
        </main>
    );
}
