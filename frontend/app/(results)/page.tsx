import { DashboardToast } from "@/components/dashboard-toast";
import { FilterBuilder } from "@/components/filter-builder";
import { ResultsTable } from "@/components/results-table";
import type {
    CombinedSearchFile,
    CombinedSearchRegistration,
} from "@/components/search-combined-file-browser";
import { SearchResultsView } from "@/components/search-results-view";
import type { FilterSuggestionMap } from "@/components/filter-builder";
import {
    fetchMetaKeys,
    fetchFiles,
    fetchStats,
    searchResults,
} from "@/app/(results)/actions";
import { BackendRequestError } from "@/lib/backend-client";
import type {
    FileEntry,
    ResultSet,
    SearchResult,
    StatsResult,
    Study,
} from "@/lib/contracts";
import { formatRegistrationUnique } from "@/lib/result-identity";
import { parseSearchFilters } from "@/lib/search-params";
import { canonicalSeqmetaKey } from "@/lib/seqmeta-keys";

type SearchParams = Record<string, string | string[] | undefined>;

const emptyStats: StatsResult = {
    total: 0,
    recent: [],
    daily: [],
    pipelines: [],
};
const combinedSearchFileFetchConcurrency = 6;

type CombinedRegistrationFetch = {
    index: number;
    result: ResultSet;
};
type LoadedCombinedRegistration =
    | (CombinedRegistrationFetch & { files: FileEntry[] })
    | (CombinedRegistrationFetch & { locked: true });

async function mapWithConcurrency<T, R>(
    items: T[],
    concurrency: number,
    mapper: (item: T) => Promise<R>,
): Promise<R[]> {
    const results = new Array<R>(items.length);
    const workerCount = Math.min(Math.max(1, concurrency), items.length);
    let nextIndex = 0;

    async function worker(): Promise<void> {
        while (nextIndex < items.length) {
            const currentIndex = nextIndex;
            nextIndex += 1;
            results[currentIndex] = await mapper(items[currentIndex]);
        }
    }

    await Promise.all(Array.from({ length: workerCount }, () => worker()));

    return results;
}

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
    const canonicalKey = canonicalSeqmetaKey(metaKey);

    if (
        metaKey === "study" ||
        metaKey === "study_id" ||
        canonicalKey === "seqmeta_id_study_lims" ||
        canonicalKey === "seqmeta_study_accession" ||
        canonicalKey === "seqmeta_uuid_study_lims" ||
        canonicalKey === "seqmeta_study_name"
    ) {
        return "study";
    }

    if (
        metaKey === "sample" ||
        metaKey === "sample_id" ||
        metaKey === "sample_name" ||
        metaKey === "sample_accession_number" ||
        canonicalKey === "seqmeta_sample_name" ||
        canonicalKey === "seqmeta_supplier_name" ||
        canonicalKey === "seqmeta_sanger_sample_id" ||
        canonicalKey === "seqmeta_id_sample_lims" ||
        canonicalKey === "seqmeta_accession_number" ||
        canonicalKey === "seqmeta_uuid_sample_lims" ||
        canonicalKey === "seqmeta_donor_id"
    ) {
        return "sample";
    }

    if (canonicalKey === "seqmeta_library_id") {
        return "seqmeta_library_id";
    }

    if (canonicalKey === "seqmeta_id_library_lims") {
        return "seqmeta_id_library_lims";
    }

    if (metaKey === "library" || canonicalKey === "seqmeta_pipeline_id_lims") {
        return "library";
    }

    return canonicalKey.startsWith("seqmeta_")
        ? canonicalKey
        : `meta_${canonicalKey}`;
}

function buildSuggestionValues(
    stats: StatsResult,
    tableData: ResultSet[] | SearchResult[],
    studies: Study[],
): FilterSuggestionMap {
    const suggestions: FilterSuggestionMap = {};
    const entries = [...stats.recent, ...tableData];

    for (const study of studies) {
        appendSuggestion(suggestions, "study", study.id_study_lims);
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
        appendSuggestion(
            suggestions,
            "run_key",
            formatRegistrationUnique(result.run_key),
        );
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
            appendSuggestion(suggestions, "sample", sampleId);
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

function isResultViewable(result: ResultSet): boolean {
    return result.access?.locked !== true && result.access?.can_view !== false;
}

function outputDirectorySpecificity(
    result: ResultSet,
    file: FileEntry,
): number {
    const outputDirectory = result.output_directory.replace(/\/+$/, "");

    if (
        file.path !== outputDirectory &&
        !file.path.startsWith(`${outputDirectory}/`)
    ) {
        return 0;
    }

    return outputDirectory.split("/").filter(Boolean).length;
}

async function fetchCombinedRegistrationFiles({
    index,
    result,
}: CombinedRegistrationFetch): Promise<LoadedCombinedRegistration> {
    try {
        return {
            files: await fetchFiles(result.id),
            index,
            result,
        };
    } catch (error) {
        if (error instanceof BackendRequestError && error.status === 403) {
            return {
                index,
                locked: true,
                result,
            };
        }

        throw error;
    }
}

async function collectCombinedSearchFiles(
    entries: ResultSet[] | SearchResult[],
): Promise<{
    files: CombinedSearchFile[];
    lockedRegistrations: CombinedSearchRegistration[];
    registrations: CombinedSearchRegistration[];
}> {
    const filesByPath = new Map<
        string,
        { file: CombinedSearchFile; specificity: number }
    >();
    const lockedRegistrationsByIndex: Array<{
        index: number;
        registration: CombinedSearchRegistration;
    }> = [];
    const registrations: CombinedSearchRegistration[] = [];
    const viewableRegistrations: CombinedRegistrationFetch[] = [];

    entries.forEach((entry, index) => {
        const result = toResultSet(entry);

        if (!isResultViewable(result)) {
            lockedRegistrationsByIndex.push({
                index,
                registration: { fileCount: 0, result },
            });
            return;
        }

        viewableRegistrations.push({ index, result });
    });

    const loadedRegistrations = await mapWithConcurrency(
        viewableRegistrations,
        combinedSearchFileFetchConcurrency,
        fetchCombinedRegistrationFiles,
    );

    for (const loadedRegistration of loadedRegistrations) {
        if ("locked" in loadedRegistration) {
            lockedRegistrationsByIndex.push({
                index: loadedRegistration.index,
                registration: {
                    fileCount: 0,
                    result: loadedRegistration.result,
                },
            });
            continue;
        }

        const outputFiles = loadedRegistration.files.filter(
            (file) => file.kind === "output",
        );

        if (outputFiles.length === 0) {
            continue;
        }

        registrations.push({
            fileCount: outputFiles.length,
            result: loadedRegistration.result,
        });
        for (const file of outputFiles) {
            const combinedFile = {
                ...file,
                resultId: loadedRegistration.result.id,
            };
            const specificity = outputDirectorySpecificity(
                loadedRegistration.result,
                file,
            );
            const existing = filesByPath.get(file.path);

            if (!existing || specificity > existing.specificity) {
                filesByPath.set(file.path, {
                    file: combinedFile,
                    specificity,
                });
            }
        }
    }

    return {
        files: [...filesByPath.values()].map(({ file }) => file),
        lockedRegistrations: lockedRegistrationsByIndex
            .sort((left, right) => left.index - right.index)
            .map(({ registration }) => registration),
        registrations,
    };
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
    const studyActive = (resolvedSearchParams.study?.length ?? 0) > 0;

    let stats = emptyStats;
    let statsError: string | null = null;
    let metaKeys: string[] = [];
    const studies: Study[] = [];
    const seqmetaAvailable = Boolean(
        process.env.WA_MLWH_BACKEND_URL?.trim(),
    );
    const statsPromise = fetchStats(10, 30);
    const metaKeysPromise = fetchMetaKeys();

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

    let tableData: ResultSet[] | SearchResult[] = stats.recent;
    let tableMode: "recent" | "search" = "recent";
    let tableEmptyMessage = "No recent results yet.";
    let combinedSearchFiles: CombinedSearchFile[] = [];
    let combinedSearchLockedRegistrations: CombinedSearchRegistration[] = [];
    let combinedSearchRegistrations: CombinedSearchRegistration[] = [];

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

        try {
            const combined = await collectCombinedSearchFiles(tableData);
            combinedSearchFiles = combined.files;
            combinedSearchLockedRegistrations = combined.lockedRegistrations;
            combinedSearchRegistrations = combined.registrations;
        } catch (error) {
            statsError =
                statsError ??
                getErrorMessage(error, "Unable to load search result files");
        }
    }

    const showCombinedSearchFileBrowser =
        combinedSearchFiles.length > 0 ||
        combinedSearchLockedRegistrations.length > 0;
    const suggestionValues = buildSuggestionValues(stats, tableData, studies);

    return (
        <main className="mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-6 px-6 py-8 sm:px-10 lg:px-12 lg:py-10">
            <DashboardToast message={statsError} />

            <FilterBuilder
                currentFilters={resolvedSearchParams}
                metaKeys={metaKeys}
                seqmetaAvailable={seqmetaAvailable}
                suggestionValues={suggestionValues}
                studies={studies}
            />

            {hasSearch && showCombinedSearchFileBrowser ? (
                <SearchResultsView
                    combinedFiles={combinedSearchFiles}
                    lockedRegistrations={combinedSearchLockedRegistrations}
                    registrations={combinedSearchRegistrations}
                    resultsTable={{
                        data: tableData,
                        emptyMessage: tableEmptyMessage,
                        hideSummary: showCombinedSearchFileBrowser,
                        mode: tableMode,
                        returnHref,
                        studyActive,
                    }}
                />
            ) : (
                <ResultsTable
                    data={tableData}
                    emptyMessage={tableEmptyMessage}
                    hideSummary={showCombinedSearchFileBrowser}
                    mode={tableMode}
                    returnHref={returnHref}
                    studyActive={studyActive}
                />
            )}
        </main>
    );
}
