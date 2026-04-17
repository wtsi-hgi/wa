import { DailyChartPanel } from "@/components/daily-chart-panel";
import { DashboardToast } from "@/components/dashboard-toast";
import { FilterBuilder } from "@/components/filter-builder";
import { ResultsTable } from "@/components/results-table";
import { StatsCards } from "@/components/stats-cards";
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

  const todayCount = stats.daily.at(-1)?.count ?? 0;

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-8 px-6 py-8 sm:px-10 lg:px-12 lg:py-10">
      <DashboardToast message={statsError} />

      <section className="overflow-hidden rounded-[2rem] border border-border/70 bg-[linear-gradient(135deg,color-mix(in_oklab,var(--card)_88%,white_12%),color-mix(in_oklab,var(--accent)_12%,var(--card)_88%))] shadow-[0_36px_120px_-72px_rgba(41,58,85,0.85)]">
        <div className="grid gap-8 px-6 py-8 sm:px-8 lg:grid-cols-[1.25fr_0.95fr] lg:px-10 lg:py-10">
          <div className="space-y-6">
            <div className="space-y-3">
              <p className="text-sm font-semibold uppercase tracking-[0.32em] text-muted-foreground">
                WA Results Dashboard
              </p>
              <h1 className="max-w-3xl text-4xl font-semibold tracking-tight text-balance sm:text-5xl">
                Track recent registrations and search result sets.
              </h1>
              <p className="max-w-2xl text-base leading-7 text-muted-foreground sm:text-lg">
                See the last 30 days of activity, review recent results, or
                narrow the dashboard with filters.
              </p>
            </div>

            <FilterBuilder
              currentFilters={resolvedSearchParams}
              metaKeys={metaKeys}
              seqmetaAvailable={seqmetaAvailable}
              studies={studies}
            />
          </div>

          <div className="relative overflow-hidden rounded-[1.75rem] border border-border/70 bg-background/80 p-5">
            <div className="absolute inset-x-6 top-0 h-px bg-gradient-to-r from-transparent via-accent/80 to-transparent" />
            <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
              Dashboard pulse
            </p>
            <p className="mt-3 text-3xl font-semibold tracking-tight">
              30-day activity
            </p>
            <p className="mt-2 max-w-sm text-sm leading-6 text-muted-foreground">
              Keep recent activity in view while you filter and review results.
            </p>
            <div className="mt-6 grid gap-3 text-sm text-muted-foreground sm:grid-cols-2">
              <div className="rounded-2xl border border-border/70 bg-card/80 p-4">
                <p className="text-xs uppercase tracking-[0.22em]">
                  Recent window
                </p>
                <p className="mt-2 text-2xl font-semibold text-foreground">
                  10
                </p>
              </div>
              <div className="rounded-2xl border border-border/70 bg-card/80 p-4">
                <p className="text-xs uppercase tracking-[0.22em]">
                  Daily range
                </p>
                <p className="mt-2 text-2xl font-semibold text-foreground">
                  30
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      <StatsCards
        total={stats.total}
        pipelineCount={stats.pipelines.length}
        todayCount={todayCount}
      />

      <DailyChartPanel data={stats.daily} />

      <ResultsTable
        data={tableData}
        emptyMessage={tableEmptyMessage}
        mode={tableMode}
        studyActive={studyActive}
      />
    </main>
  );
}
