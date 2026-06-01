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

import { AppProviders } from "@/components/app-providers";
import type { ResultSet, SearchResult, StatsResult } from "@/lib/contracts";

const fetchStatsMock = vi.fn();
const searchResultsMock = vi.fn();
const fetchMetaKeysMock = vi.fn().mockResolvedValue([]);
const fetchStudiesMock = vi.fn();
const fetchResultMock = vi.fn();
const fetchFilesMock = vi.fn().mockResolvedValue([]);
const fetchFileContentMock = vi.fn();
const validateIdentifierMock = vi.fn();
const pushMock = vi.fn();

const testDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(testDir, "..");

beforeAll(() => {
    class ResizeObserverStub {
        observe() {}

        unobserve() {}

        disconnect() {}
    }

    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    vi.stubGlobal("matchMedia", () => ({
        addEventListener: vi.fn(),
        addListener: vi.fn(),
        dispatchEvent: vi.fn(),
        matches: false,
        media: "",
        onchange: null,
        removeEventListener: vi.fn(),
        removeListener: vi.fn(),
    }));
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
});

vi.mock("next/navigation", () => ({
    usePathname: () => "/",
    useRouter: () => ({
        push: pushMock,
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

function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason?: unknown) => void;

    const promise = new Promise<T>((innerResolve, innerReject) => {
        resolve = innerResolve;
        reject = innerReject;
    });

    return { promise, resolve, reject };
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
        delete process.env.WA_SEQMETA_BACKEND_URL;
        vi.clearAllMocks();
        fetchFilesMock.mockResolvedValue([]);
        vi.resetModules();
        pushMock.mockReset();
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
        expect(markup).not.toContain(
            "Stack repeated values as OR filters, combine fields as AND filters, and keep the search encoded in the URL.",
        );
        expect(markup).not.toContain("Dashboard pulse");
        expect(markup).not.toContain("30-day activity");
        expect(markup).not.toContain("Total result sets");
        expect(markup).not.toContain("Distinct pipelines");
        expect(markup).not.toContain("Registered today");
        expect(markup).not.toContain("Daily registrations");
        expect(markup).not.toContain("Last 30 days of result activity");
    });

    it("renders the search builder without an extra dashboard panel wrapper", async () => {
        fetchStatsMock.mockResolvedValue(buildStats());
        searchResultsMock.mockResolvedValue([]);

        const markup = await renderDashboard();

        document.body.innerHTML = markup;

        const searchBuilder = document.querySelector(
            '[data-search-builder="true"]',
        );

        expect(searchBuilder).toBeTruthy();
        expect(searchBuilder?.parentElement?.tagName).toBe("MAIN");
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
        const serverTree = createElement(
            AppProviders,
            undefined,
            await Page({ searchParams: Promise.resolve({}) }),
        );
        const serverMarkup = renderToString(serverTree);
        const container = document.createElement("div");
        const recoverableErrors: unknown[] = [];

        document.body.appendChild(container);
        container.innerHTML = serverMarkup;

        let root: ReturnType<typeof hydrateRoot> | null = null;

        await act(async () => {
            root = hydrateRoot(container, serverTree, {
                onRecoverableError: (error) => {
                    recoverableErrors.push(error);
                },
            });
        });

        expect(recoverableErrors).toHaveLength(0);
        expect(
            screen.queryByRole("dialog", {
                name: /search builder filter panel/i,
            }),
        ).toBeNull();

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));

        expect(
            screen.getByRole("dialog", {
                name: /search builder filter panel/i,
            }),
        ).toBeTruthy();

        await act(async () => {
            root?.unmount();
        });
    });

    it("starts loading filter metadata before stats resolves without live study lookup", async () => {
        const statsPending = deferred<StatsResult>();

        process.env.WA_SEQMETA_BACKEND_URL = "https://seqmeta.example";
        fetchStatsMock.mockReturnValue(statsPending.promise);
        fetchMetaKeysMock.mockResolvedValue([]);
        fetchStudiesMock.mockResolvedValue([]);

        const pageModule = await import("@/app/(results)/page");
        const Page = pageModule.default;
        const pagePromise = Page({ searchParams: Promise.resolve({}) });

        await Promise.resolve();

        expect(fetchStatsMock).toHaveBeenCalledTimes(1);
        expect(fetchMetaKeysMock).toHaveBeenCalledTimes(1);
        expect(fetchStudiesMock).not.toHaveBeenCalled();

        statsPending.resolve(buildStats());
        await pagePromise;

        delete process.env.WA_SEQMETA_BACKEND_URL;
    });

    it("does not block the landing page on slow live study suggestions", async () => {
        process.env.WA_SEQMETA_BACKEND_URL = "https://seqmeta.example";
        fetchStatsMock.mockResolvedValue(buildStats());
        fetchMetaKeysMock.mockResolvedValue([]);
        fetchStudiesMock.mockReturnValue(deferred().promise);
        searchResultsMock.mockResolvedValue([]);

        const pageModule = await import("@/app/(results)/page");
        const Page = pageModule.default;
        const pagePromise = Page({ searchParams: Promise.resolve({}) });

        await Promise.resolve();
        expect(fetchStudiesMock).not.toHaveBeenCalled();

        const markup = renderToStaticMarkup(await pagePromise);

        expect(markup).toContain("Search builder");
        expect(markup).toContain("Recent registrations");

        delete process.env.WA_SEQMETA_BACKEND_URL;
    });

    it("derives non-study filter suggestions from loaded result data", async () => {
        fetchStatsMock.mockResolvedValue(
            buildStats({
                recent: [
                    {
                        ...buildResultSet(1),
                        pipeline_name: "nf-core/rnaseq",
                        pipeline_version: "3.18.0",
                        pipeline_identifier: "gh://repo/rnaseq/main.nf",
                        requester: "carol",
                        operator: "operator-42",
                        output_directory: "/tmp/results/rnaseq",
                        metadata: {
                            seqmeta_sampleid: "SANG1001",
                            library: "RNA",
                        },
                    },
                ],
            }),
        );
        searchResultsMock.mockResolvedValue([]);

        const pageModule = await import("@/app/(results)/page");
        const Page = pageModule.default;
        const serverTree = createElement(
            AppProviders,
            undefined,
            await Page({ searchParams: Promise.resolve({}) }),
        );
        const container = document.createElement("div");
        const rootMarkup = renderToString(serverTree);

        document.body.appendChild(container);
        container.innerHTML = rootMarkup;

        let root: ReturnType<typeof hydrateRoot> | null = null;

        await act(async () => {
            root = hydrateRoot(container, serverTree);
        });

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));
        fireEvent.click(screen.getByRole("option", { name: /pipeline name/i }));
        const valueInput = screen.getByLabelText(/pipeline name value/i);

        fireEvent.change(valueInput, {
            target: { value: "rna" },
        });

        expect(valueInput.getAttribute("list")).toBe(
            "filter-suggestions-pipeline_name",
        );
        expect(
            container.querySelector(
                "datalist#filter-suggestions-pipeline_name option[value='nf-core/rnaseq']",
            ),
        ).toBeTruthy();
        expect(
            screen.queryByRole("button", { name: /use nf-core\/rnaseq/i }),
        ).toBeNull();

        await act(async () => {
            root?.unmount();
        });
    });

    it("hydrates requester suggestions derived on the server and applies the selected value", async () => {
        fetchStatsMock.mockResolvedValue(
            buildStats({
                recent: [
                    {
                        ...buildResultSet(1),
                        requester: "carol",
                    },
                    {
                        ...buildResultSet(2),
                        requester: "dave",
                    },
                ],
            }),
        );
        searchResultsMock.mockResolvedValue([]);

        const pageModule = await import("@/app/(results)/page");
        const Page = pageModule.default;
        const serverTree = createElement(
            AppProviders,
            undefined,
            await Page({ searchParams: Promise.resolve({}) }),
        );
        const container = document.createElement("div");
        const rootMarkup = renderToString(serverTree);

        document.body.appendChild(container);
        container.innerHTML = rootMarkup;

        let root: ReturnType<typeof hydrateRoot> | null = null;

        await act(async () => {
            root = hydrateRoot(container, serverTree);
        });

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));
        fireEvent.click(screen.getByRole("option", { name: /^requester$/i }));
        const valueInput = screen.getByLabelText(/requester value/i);

        fireEvent.change(valueInput, {
            target: { value: "car" },
        });

        expect(valueInput.getAttribute("list")).toBe("filter-suggestions-user");
        expect(
            container.querySelector(
                "datalist#filter-suggestions-user option[value='carol']",
            ),
        ).toBeTruthy();
        expect(screen.queryByRole("button", { name: /use carol/i })).toBeNull();

        fireEvent.change(valueInput, {
            target: { value: "carol" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?user=carol");

        await act(async () => {
            root?.unmount();
        });
    });

    it("keeps study filters usable without loading live study suggestions", async () => {
        process.env.WA_SEQMETA_BACKEND_URL = "https://seqmeta.example";
        fetchStatsMock.mockResolvedValue(buildStats());
        fetchStudiesMock.mockResolvedValue([
            { id_study_lims: "6568", name: "RNA Seq" },
            { id_study_lims: "7777", name: "Cancer Study" },
        ]);
        searchResultsMock.mockResolvedValue([]);

        const pageModule = await import("@/app/(results)/page");
        const Page = pageModule.default;
        const serverTree = createElement(
            AppProviders,
            undefined,
            await Page({ searchParams: Promise.resolve({}) }),
        );
        const container = document.createElement("div");
        const rootMarkup = renderToString(serverTree);

        document.body.appendChild(container);
        container.innerHTML = rootMarkup;

        let root: ReturnType<typeof hydrateRoot> | null = null;

        await act(async () => {
            root = hydrateRoot(container, serverTree);
        });

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));
        fireEvent.click(screen.getByRole("option", { name: /study/i }));
        const valueInput = screen.getByLabelText(/study value/i);

        fireEvent.change(valueInput, {
            target: { value: "656" },
        });

        expect(valueInput.getAttribute("list")).toBe(
            "filter-suggestions-study",
        );
        expect(
            container.querySelector(
                "datalist#filter-suggestions-study option[value='6568']",
            ),
        ).toBeNull();
        expect(fetchStudiesMock).not.toHaveBeenCalled();

        fireEvent.change(valueInput, {
            target: { value: "6568" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?study=6568");

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

        const toLocaleDateStringSpy = vi.spyOn(
            Date.prototype,
            "toLocaleDateString",
        );
        toLocaleDateStringSpy.mockImplementation(() => "16 Apr 2026");

        const pageModule = await import("@/app/(results)/page");
        const Page = pageModule.default;
        const serverTree = createElement(
            AppProviders,
            undefined,
            await Page({ searchParams: Promise.resolve({}) }),
        );
        const serverMarkup = renderToString(serverTree);
        const container = document.createElement("div");
        const recoverableErrors: unknown[] = [];

        document.body.appendChild(container);
        container.innerHTML = serverMarkup;

        toLocaleDateStringSpy.mockImplementation(() => "17 Apr 2026");

        let root: ReturnType<typeof hydrateRoot> | null = null;

        await act(async () => {
            root = hydrateRoot(container, serverTree, {
                onRecoverableError: (error) => {
                    recoverableErrors.push(error);
                },
            });
        });

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));
        expect(
            screen.getByRole("dialog", {
                name: /search builder filter panel/i,
            }),
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

    it("renders the combined search file browser by default for viewable matching result files", async () => {
        fetchStatsMock.mockResolvedValue(buildStats());
        const alpha = buildResultSet(21);
        const beta = buildResultSet(22);
        alpha.pipeline_name = "wa/combined-browser-repro";
        beta.pipeline_name = "wa/combined-browser-repro";
        alpha.output_directory = "/tmp/results/shared/sample-alpha/final";
        beta.output_directory = "/tmp/results/shared/sample-beta/final";
        alpha.metadata = { sample: "COMBINED_SAMPLE_ALPHA" };
        beta.metadata = { sample: "COMBINED_SAMPLE_BETA" };
        searchResultsMock.mockResolvedValue([alpha, beta]);
        fetchFilesMock.mockImplementation((id: string) =>
            Promise.resolve(
                id === alpha.id
                    ? [
                          {
                              kind: "output",
                              mtime: "2026-06-01T10:00:00Z",
                              path: `${alpha.output_directory}/alpha-expression-counts.tsv`,
                              size: 12,
                          },
                      ]
                    : [
                          {
                              kind: "output",
                              mtime: "2026-06-01T10:00:00Z",
                              path: `${beta.output_directory}/beta-expression-counts.tsv`,
                              size: 12,
                          },
                      ],
            ),
        );

        const markup = await renderDashboard({
            pipeline_name: "wa/combined-browser-repro",
        });

        expect(countOccurrences(markup, 'data-file-browser="true"')).toBe(1);
        expect(markup).toContain("Combined files");
        expect(markup).toContain("Result rows");
        expect(markup).not.toContain('data-results-table-summary="true"');
        expect(markup).not.toContain("Showing search results");
        expect(markup).not.toContain("Matching result sets");
        expect(markup).toContain('data-directory-path="/tmp/results/shared"');
        expect(markup).toContain(
            'data-directory-path="/tmp/results/shared/sample-alpha/final"',
        );
        expect(markup).toContain(
            'data-directory-path="/tmp/results/shared/sample-beta/final"',
        );
        expect(markup).not.toContain(
            'data-file-path="/tmp/results/shared/sample-alpha/final/alpha-expression-counts.tsv"',
        );
        expect(markup).not.toContain(
            'data-file-path="/tmp/results/shared/sample-beta/final/beta-expression-counts.tsv"',
        );
        expect(markup).toContain(`data-combined-result-info="${alpha.id}"`);
        expect(markup).toContain(`data-combined-result-info="${beta.id}"`);
        expect(countOccurrences(markup, 'data-result-row="true"')).toBe(2);
    });

    it("deduplicates overlapping combined search files by visible path", async () => {
        fetchStatsMock.mockResolvedValue(buildStats());
        const parent = buildResultSet(23);
        const sample = buildResultSet(24);
        const duplicatePath = "/tmp/results/galleries/sample-a/blue-plot.svg";
        parent.pipeline_name = "wa/overlapping-output-dir-regression";
        sample.pipeline_name = "wa/overlapping-output-dir-regression";
        parent.output_directory = "/tmp/results/galleries";
        sample.output_directory = "/tmp/results/galleries/sample-a";
        searchResultsMock.mockResolvedValue([parent, sample]);
        fetchFilesMock.mockImplementation((id: string) =>
            Promise.resolve(
                id === parent.id
                    ? [
                          {
                              kind: "output",
                              mtime: "2026-06-01T10:00:00Z",
                              path: duplicatePath,
                              size: 12,
                          },
                          {
                              kind: "output",
                              mtime: "2026-06-01T10:00:00Z",
                              path: "/tmp/results/galleries/sample-a/red-plot.svg",
                              size: 12,
                          },
                      ]
                    : [
                          {
                              kind: "output",
                              mtime: "2026-06-01T10:00:00Z",
                              path: duplicatePath,
                              size: 12,
                          },
                      ],
            ),
        );

        const markup = await renderDashboard({
            pipeline_name: "wa/overlapping-output-dir-regression",
        });

        expect(
            countOccurrences(markup, `data-file-path="${duplicatePath}"`),
        ).toBe(1);
        expect(markup).toContain(`data-combined-result-info="${sample.id}"`);
        expect(markup).toContain(`/api/file?id=${sample.id}&amp;`);
        expect(countOccurrences(markup, 'data-result-row="true"')).toBe(2);
    });

    it("renders a locked combined search file browser state for locked matching result rows", async () => {
        fetchStatsMock.mockResolvedValue(buildStats());
        const alpha = buildResultSet(21);
        const beta = buildResultSet(22);
        alpha.pipeline_name = "wa/combined-browser-repro";
        beta.pipeline_name = "wa/combined-browser-repro";
        alpha.output_directory = "/tmp/results/shared/sample-alpha/final";
        beta.output_directory = "/tmp/results/shared/sample-beta/final";
        alpha.access = { can_view: false, locked: true };
        beta.access = { can_view: false, locked: true };
        searchResultsMock.mockResolvedValue([alpha, beta]);

        const markup = await renderDashboard({
            pipeline_name: "wa/combined-browser-repro",
        });

        expect(fetchFilesMock).not.toHaveBeenCalled();
        expect(markup).toContain('data-search-combined-file-browser="true"');
        expect(markup).toContain('data-combined-file-browser-locked="true"');
        expect(markup).not.toContain('data-results-table-summary="true"');
        expect(markup).toContain("File access locked");
        expect(markup).toContain("2 matching result sets");
        expect(markup).toContain("/tmp/results/shared");
        expect(
            countOccurrences(markup, 'data-locked-output-directory="true"'),
        ).toBe(2);
        expect(countOccurrences(markup, 'data-file-browser="true"')).toBe(0);
        expect(countOccurrences(markup, 'data-result-row="true"')).toBe(2);
    });

    it("shows matched samples for study-driven searches", async () => {
        fetchStatsMock.mockResolvedValue(buildStats());
        searchResultsMock.mockResolvedValue([
            {
                result_set: buildResultSet(22),
                matched_samples: ["SANG22", "SANG77"],
            } satisfies SearchResult,
        ]);

        const markup = await renderDashboard({ study: "6568" });

        expect(searchResultsMock).toHaveBeenCalledWith({ study: ["6568"] });
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
