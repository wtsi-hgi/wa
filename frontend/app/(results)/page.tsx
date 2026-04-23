import { DashboardToast } from "@/components/dashboard-toast";
import { FilterBuilder } from "@/components/filter-builder";
import { ResultsTable } from "@/components/results-table";
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
  const resolvedSearchParams = normalizeSearchParams(
    (await searchParams) ?? {},
  );
  const hasSearch = Object.keys(resolvedSearchParams).length > 0;
  const studyActive = (resolvedSearchParams.study_id?.length ?? 0) > 0;

  let stats = emptyStats;
  let statsError: string | null = null;
  let metaKeys: string[] = [];
  let studies: Study[] = [];
  const seqmetaAvailable = Boolean(process.env.WA_SEQMETA_BACKEND_URL?.trim());

  try {
    stats = await fetchStats(10, 30);
  } catch (error) {
    statsError = getErrorMessage(error, "Unable to load dashboard statistics");
  }

  try {
    const loadedMetaKeys = await fetchMetaKeys();
    metaKeys = Array.isArray(loadedMetaKeys) ? loadedMetaKeys : [];
  } catch (error) {
    statsError =
      statsError ?? getErrorMessage(error, "Unable to load filter fields");
  }

  if (seqmetaAvailable) {
    try {
      const loadedStudies = await fetchStudies();
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
        statsError ?? getErrorMessage(error, "Unable to search results");
    }
  }

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-6 px-6 py-8 sm:px-10 lg:px-12 lg:py-10">
      <DashboardToast message={statsError} />

      <section className="rounded-[2rem] border border-border/70 bg-[linear-gradient(135deg,color-mix(in_oklab,var(--card)_90%,white_10%),color-mix(in_oklab,var(--accent)_10%,var(--card)_90%))] p-4 shadow-[0_32px_110px_-76px_rgba(41,58,85,0.82)] sm:p-6">
        <FilterBuilder
          currentFilters={resolvedSearchParams}
          metaKeys={metaKeys}
          seqmetaAvailable={seqmetaAvailable}
          studies={studies}
        />
      </section>

      <ResultsTable
        data={tableData}
        emptyMessage={tableEmptyMessage}
        mode={tableMode}
        studyActive={studyActive}
      />
    </main>
  );
}
