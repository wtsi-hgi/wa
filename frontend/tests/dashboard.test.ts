/**
 * @vitest-environment jsdom
 */

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { act } from "react";
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

beforeAll(() => {
  class ResizeObserverStub {
    observe() {}

    unobserve() {}

    disconnect() {}
  }

  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  window.HTMLElement.prototype.scrollIntoView = vi.fn();
});

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

afterAll(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
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

function buildStats(overrides: Partial<StatsResult> = {}): StatsResult {
  return {
    total: 0,
    recent: [],
    daily: [],
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

describe("J1 dashboard with search builder and recent results", () => {
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
    expect(pageSource).not.toContain("DailyChartPanel");
    expect(pageSource).not.toContain("StatsCards");
    expect(pageSource).toContain("<FilterBuilder");
    expect(pageSource).toContain("currentFilters={resolvedSearchParams}");
    expect(pageSource).toContain("metaKeys={metaKeys}");
    expect(pageSource).toContain("seqmetaAvailable={seqmetaAvailable}");
    expect(pageSource).toContain("<ResultsTable");
    expect(pageSource).not.toContain("function ResultsTable(");
  });

  it("renders only the search builder above recent registrations on the landing page", async () => {
    fetchStatsMock.mockResolvedValue(
      buildStats({
        total: 42,
        pipelines: [
          { pipeline_name: "alpha", count: 10 },
          { pipeline_name: "beta", count: 7 },
        ],
        recent: Array.from({ length: 3 }, (_, index) =>
          buildResultSet(index + 1),
        ),
      }),
    );
    searchResultsMock.mockResolvedValue([]);

    const markup = await renderDashboard();

    expect(markup).toContain("Search builder");
    expect(markup).toContain("Recent registrations");
    expect(markup).toContain("Latest result sets");
    expect(markup).not.toContain("Dashboard pulse");
    expect(markup).not.toContain("30-day activity");
    expect(markup).not.toContain("Total result sets");
    expect(markup).not.toContain("Distinct pipelines");
    expect(markup).not.toContain("Registered today");
    expect(markup).not.toContain("Daily registrations");
    expect(markup).not.toContain("Last 30 days of result activity");
  });

  it("hydrates the landing page without recoverable mismatches and keeps Add filter interactive", async () => {
    fetchStatsMock.mockResolvedValue(
      buildStats({
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

  it("keeps dashboard buttons interactive when client locale formatting differs", async () => {
    fetchStatsMock.mockResolvedValue(
      buildStats({
        recent: Array.from({ length: 3 }, (_, index) =>
          buildResultSet(index + 1),
        ),
      }),
    );
    searchResultsMock.mockResolvedValue([]);

    const toLocaleDateStringSpy = vi.spyOn(Date.prototype, "toLocaleDateString");
    toLocaleDateStringSpy.mockImplementation(() => "16 Apr 2026");

    const pageModule = await import("@/app/(results)/page");
    const Page = pageModule.default;
    const serverMarkup = renderToString(
      await Page({ searchParams: Promise.resolve({}) }),
    );
    const container = document.createElement("div");
    const recoverableErrors: Error[] = [];

    document.body.appendChild(container);
    container.innerHTML = serverMarkup;

    toLocaleDateStringSpy.mockImplementation(() => "17 Apr 2026");

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

    fireEvent.click(screen.getByRole("button", { name: /add filter/i }));
    expect(
      screen.getByRole("dialog", { name: /search builder filter panel/i }),
    ).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: /toggle column visibility/i }),
    );
    expect(screen.getByRole("menu")).toBeTruthy();
    expect(recoverableErrors).toHaveLength(0);

    toLocaleDateStringSpy.mockRestore();

    await act(async () => {
      root?.unmount();
    });
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
