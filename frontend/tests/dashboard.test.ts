/**
 * @vitest-environment jsdom
 */

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { act, createElement } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToStaticMarkup, renderToString } from "react-dom/server";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import {
  afterAll,
  afterEach,
  beforeAll,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import type {
  DailyCount,
  ResultSet,
  SearchResult,
  StatsResult,
} from "@/lib/contracts";

const fetchStatsMock = vi.fn();
const searchResultsMock = vi.fn();
const fetchMetaKeysMock = vi.fn().mockResolvedValue([]);
const fetchStudiesMock = vi.fn();
const fetchResultMock = vi.fn();
const fetchFilesMock = vi.fn();
const fetchFileContentMock = vi.fn();
const validateIdentifierMock = vi.fn();

const testDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(testDir, "..");

vi.mock("next/navigation", () => ({
  usePathname: () => "/",
  useRouter: () => ({
    push: vi.fn(),
  }),
}));

vi.mock("@/app/(results)/actions", () => ({
  fetchStats: fetchStatsMock,
  searchResults: searchResultsMock,
  fetchMetaKeys: fetchMetaKeysMock,
  fetchStudies: fetchStudiesMock,
  fetchResult: fetchResultMock,
  fetchFiles: fetchFilesMock,
  fetchFileContent: fetchFileContentMock,
  validateIdentifier: validateIdentifierMock,
}));

beforeAll(() => {
  class ResizeObserverStub {
    observe() {}

    unobserve() {}

    disconnect() {}
  }

  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  window.HTMLElement.prototype.scrollIntoView = vi.fn();
});

afterAll(() => {
  vi.unstubAllGlobals();
});

function buildResultSet(index: number): ResultSet {
  const day = String((index % 9) + 1).padStart(2, "0");

  return {
    id: `result-${index}`,
    pipeline_identifier: `gh://repo/workflow-${index}.nf`,
    run_key: `runid=${1000 + index}`,
    requester: index % 2 === 0 ? "alice" : "bob",
    operator: "operator-1",
    command: `nextflow run workflow-${index}.nf`,
    pipeline_name: `pipeline-${index % 3}`,
    pipeline_version: `1.${index}.0`,
    output_directory: `/tmp/results/${index}`,
    metadata: {
      seqmeta_sampleid: `SANG${index}`,
    },
    created_at: `2026-04-${day}T10:00:00Z`,
    updated_at: `2026-04-${day}T10:30:00Z`,
  };
}

function buildDailyCounts(totalDays: number, todayCount: number): DailyCount[] {
  return Array.from({ length: totalDays }, (_, index) => ({
    date: `2026-03-${String(index + 1).padStart(2, "0")}`,
    count: index === totalDays - 1 ? todayCount : index % 4,
  }));
}

function buildStats(overrides: Partial<StatsResult> = {}): StatsResult {
  return {
    total: 0,
    recent: [],
    daily: buildDailyCounts(30, 0),
    pipelines: [],
    ...overrides,
  };
}

function countOccurrences(markup: string, needle: string): number {
  return markup.match(new RegExp(needle, "g"))?.length ?? 0;
}

async function renderDashboard(
  searchParams?: Record<string, string | string[]>,
) {
  const pageModule = await import("@/app/(results)/page");
  const Page = pageModule.default;

  return renderToStaticMarkup(
    await Page({ searchParams: Promise.resolve(searchParams ?? {}) }),
  );
}

describe("J1 dashboard with stats, search, and recent results", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    vi.clearAllMocks();
    vi.resetModules();
  });

  it("composes the dashboard page from the shared filter builder and results table components", () => {
    const pageSource = readFileSync(
      path.join(frontendRoot, "app", "(results)", "page.tsx"),
      "utf8",
    );

    expect(pageSource).toContain(
      'import { FilterBuilder } from "@/components/filter-builder"',
    );
    expect(pageSource).toContain(
      'import { ResultsTable } from "@/components/results-table"',
    );
    expect(pageSource).toContain("<FilterBuilder");
    expect(pageSource).toContain("currentFilters={resolvedSearchParams}");
    expect(pageSource).toContain("metaKeys={metaKeys}");
    expect(pageSource).toContain("seqmetaAvailable={seqmetaAvailable}");
    expect(pageSource).toContain("<ResultsTable");
    expect(pageSource).not.toContain("function ResultsTable(");
  });

  it("shows total result sets, pipeline count, and today's registrations in the stat cards", async () => {
    fetchStatsMock.mockResolvedValue(
      buildStats({
        total: 42,
        pipelines: [
          { pipeline_name: "alpha", count: 10 },
          { pipeline_name: "beta", count: 7 },
          { pipeline_name: "gamma", count: 4 },
        ],
        daily: buildDailyCounts(30, 5),
      }),
    );
    searchResultsMock.mockResolvedValue([]);

    const markup = await renderDashboard();

    expect(markup).toContain('data-stat-card="total">42<');
    expect(markup).toContain('data-stat-card="pipelines">3<');
    expect(markup).toContain('data-stat-card="today">5<');
    expect(markup).not.toContain("Registered result sets currently tracked by WA.");
    expect(markup).not.toContain(
      "Registrations recorded in the latest dashboard day bucket.",
    );
  });

  it("renders 30 daily bars when the chart receives 30 entries", async () => {
    const { DailyChart } = await import("@/components/daily-chart");

    const markup = renderToStaticMarkup(
      createElement(DailyChart, {
        data: buildDailyCounts(30, 5),
      }),
    );

    expect(countOccurrences(markup, 'data-daily-bar="true"')).toBe(30);
  });

  it("server-renders a chart shell instead of Recharts markup on the dashboard", async () => {
    fetchStatsMock.mockResolvedValue(buildStats());
    searchResultsMock.mockResolvedValue([]);

    const markup = await renderDashboard();

    expect(markup).toContain('data-chart-shell-bar="latest"');
    expect(markup).toContain('style="height:0px"');
    expect(markup).not.toContain("recharts-responsive-container");
  });

  it("server-renders shell bars from stats data instead of placeholder loading bars", async () => {
    fetchStatsMock.mockResolvedValue(
      buildStats({
        daily: buildDailyCounts(3, 5),
      }),
    );
    searchResultsMock.mockResolvedValue([]);

    const markup = await renderDashboard();

    expect(countOccurrences(markup, 'data-chart-shell-bar="')).toBe(3);
    expect(markup).toContain("2026-03-03:5");
  });

  it("hydrates the landing page without recoverable mismatches and keeps Add filter interactive", async () => {
    fetchStatsMock.mockResolvedValue(
      buildStats({
        daily: buildDailyCounts(30, 5),
        recent: Array.from({ length: 3 }, (_, index) =>
          buildResultSet(index + 1),
        ),
      }),
    );
    searchResultsMock.mockResolvedValue([]);

    const pageModule = await import("@/app/(results)/page");
    const Page = pageModule.default;
    const serverMarkup = renderToString(
      await Page({ searchParams: Promise.resolve({}) }),
    );
    const container = document.createElement("div");
    const recoverableErrors: Error[] = [];

    document.body.appendChild(container);
    container.innerHTML = serverMarkup;

    expect(
      container.querySelector('[data-chart-shell-bar="latest"]'),
    ).not.toBeNull();

    let root: ReturnType<typeof hydrateRoot> | null = null;

    await act(async () => {
      root = hydrateRoot(
        container,
        await Page({ searchParams: Promise.resolve({}) }),
        {
          onRecoverableError: (error) => {
            recoverableErrors.push(error);
          },
        },
      );
    });

    await waitFor(() => {
      expect(
        container.querySelector(".recharts-responsive-container"),
      ).not.toBeNull();
    });

    expect(recoverableErrors).toHaveLength(0);
    expect(
      screen.queryByRole("dialog", { name: /search builder filter panel/i }),
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /add filter/i }));

    expect(
      screen.getByRole("dialog", { name: /search builder filter panel/i }),
    ).toBeTruthy();

    await act(async () => {
      root?.unmount();
    });
  });

  it("uses concise end-user copy in the dashboard header and chart", async () => {
    fetchStatsMock.mockResolvedValue(buildStats());
    searchResultsMock.mockResolvedValue([]);

    const markup = await renderDashboard();

    expect(markup).toContain(
      "Track recent registrations and search result sets.",
    );
    expect(markup).toContain(
      "See the last 30 days of activity, review recent results, or narrow the dashboard with filters.",
    );
    expect(markup).toContain(
      "Keep recent activity in view while you filter and review results.",
    );
    expect(markup).toContain(
      "Daily registrations for the last 30 days, with the newest day highlighted.",
    );
    expect(markup).not.toContain(
      "Registrations, pipeline flow, and search entry in one landing view.",
    );
    expect(markup).not.toContain(
      "Review the last 30 days at a glance, then pivot into recent result sets or a targeted search without leaving the dashboard.",
    );
    expect(markup).not.toContain(
      "The chart and cards below stay visible even during filtered views, so search doesn&apos;t hide the broader system context.",
    );
    expect(markup).not.toContain(
      "Recent throughput stays readable on wide and narrow screens, with the newest day lifted in accent colour.",
    );
  });

  it("shows the 10 recent result rows when there are no search params", async () => {
    fetchStatsMock.mockResolvedValue(
      buildStats({
        recent: Array.from({ length: 10 }, (_, index) =>
          buildResultSet(index + 1),
        ),
      }),
    );
    searchResultsMock.mockResolvedValue([]);

    const markup = await renderDashboard();

    expect(searchResultsMock).not.toHaveBeenCalled();
    expect(countOccurrences(markup, 'data-result-row="true"')).toBe(10);
  });

  it("calls searchResults with repeated string arrays and shows search rows when params are present", async () => {
    fetchStatsMock.mockResolvedValue(
      buildStats({
        recent: Array.from({ length: 10 }, (_, index) =>
          buildResultSet(index + 1),
        ),
      }),
    );
    searchResultsMock.mockResolvedValue([
      buildResultSet(21),
      {
        result_set: buildResultSet(22),
        matched_samples: ["SANG22", "SANG77"],
      } satisfies SearchResult,
    ]);

    const markup = await renderDashboard({ user: "alice" });

    expect(searchResultsMock).toHaveBeenCalledWith({ user: ["alice"] });
    expect(countOccurrences(markup, 'data-result-row="true"')).toBe(2);
    expect(markup).toContain("Showing search results");
  });

  it("shows matched samples for study-driven searches", async () => {
    fetchStatsMock.mockResolvedValue(buildStats());
    searchResultsMock.mockResolvedValue([
      {
        result_set: buildResultSet(22),
        matched_samples: ["SANG22", "SANG77"],
      } satisfies SearchResult,
    ]);

    const markup = await renderDashboard({ study_id: "6568" });

    expect(searchResultsMock).toHaveBeenCalledWith({ study_id: ["6568"] });
    expect(markup).toContain("Matched Samples");
    expect(markup).toContain("SANG22, SANG77");
  });

  it("shows an error toast message and empty state when stats loading fails", async () => {
    fetchStatsMock.mockRejectedValue(new Error("stats failed"));
    searchResultsMock.mockResolvedValue([]);

    const markup = await renderDashboard();

    expect(markup).toContain('data-toast-message="stats failed"');
    expect(markup).toContain("No recent results yet.");
    expect(countOccurrences(markup, 'data-result-row="true"')).toBe(0);
  });
});
