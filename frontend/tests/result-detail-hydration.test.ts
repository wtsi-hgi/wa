// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToStaticMarkup, renderToString } from "react-dom/server";
import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AppProviders } from "@/components/app-providers";
import type { FileEntry, ResultSet } from "@/lib/contracts";

const fetchFilesMock = vi.fn();
const fetchResultMock = vi.fn();
const validateIdentifierMock = vi.fn();
const enrichIdentifierMock = vi.fn();
const enrichIdentifiersMock = vi.fn();
const enrichSeqmetaMetadataMock = vi.fn();
const enrichSeqmetaMetadataBatchMock = vi.fn();
const getRequestMLWHCacheMock = vi.fn();
const buildCachedEnrichmentStateMock = vi.fn();
const collectSeqmetaValuesMock = vi.fn();
const hasUsableMLWHCacheEntryMock = vi.fn();
const mergeSeqmetaEnrichmentStateMock = vi.fn();
const primeMLWHCacheMock = vi.fn();
const { toastErrorMock, toastSuccessMock } = vi.hoisted(() => ({
    toastErrorMock: vi.fn(),
    toastSuccessMock: vi.fn(),
}));

vi.mock("sonner", () => ({
    Toaster: () => createElement("div", { "data-testid": "sonner-toaster" }),
    toast: {
        error: toastErrorMock,
        success: toastSuccessMock,
    },
}));

vi.mock("@/app/(results)/actions", () => ({
    fetchFiles: fetchFilesMock,
    fetchResult: fetchResultMock,
    enrichIdentifier: enrichIdentifierMock,
    enrichIdentifiers: enrichIdentifiersMock,
    validateIdentifier: validateIdentifierMock,
}));

vi.mock("@/lib/mlwh-enrichment", () => ({
    buildCachedEnrichmentState: buildCachedEnrichmentStateMock,
    collectSeqmetaValues: collectSeqmetaValuesMock,
    enrichSeqmetaMetadata: enrichSeqmetaMetadataMock,
    enrichSeqmetaMetadataBatch: enrichSeqmetaMetadataBatchMock,
    hasUsableMLWHCacheEntry: hasUsableMLWHCacheEntryMock,
    mergeSeqmetaEnrichmentState: mergeSeqmetaEnrichmentStateMock,
    primeMLWHCache: primeMLWHCacheMock,
}));

vi.mock("@/lib/mlwh-cache-server", () => ({
    getRequestMLWHCache: getRequestMLWHCacheMock,
}));

function buildFile(path: string): FileEntry {
    return {
        kind: "output",
        mtime: "2026-04-16T10:15:00Z",
        path,
        size: 512,
    };
}

function buildResultSet(): ResultSet {
    return {
        command: "nextflow run workflow.nf",
        created_at: "2026-04-16T10:15:00Z",
        id: "result-2026-04-16-operator-1-pipeline-run-abcdef1234567890",
        metadata: {
            seqmeta_sampleid: "SANG001",
        },
        operator: "operator-1",
        output_directory: "/results",
        pipeline_identifier: "gh://repo/workflow.nf",
        pipeline_name: "nf-core/rnaseq",
        pipeline_version: "3.18.0",
        requester: "alice",
        run_key: "runid=1001",
        updated_at: "2026-04-16T10:45:00Z",
    };
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

describe("O1 result detail hydration", () => {
    const matchMediaStub = () => ({
        addEventListener: vi.fn(),
        addListener: vi.fn(),
        dispatchEvent: vi.fn(),
        matches: false,
        media: "",
        onchange: null,
        removeEventListener: vi.fn(),
        removeListener: vi.fn(),
    });

    afterEach(() => {
        document.body.innerHTML = "";
        window.localStorage.clear();
        vi.clearAllMocks();
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
    });

    it("keeps directory switching interactive when client locale formatting differs", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");
        const files = [
            buildFile("/results/a/sample.bam"),
            buildFile("/results/b/report.txt"),
        ];
        const toLocaleStringSpy = vi.spyOn(Date.prototype, "toLocaleString");

        vi.stubGlobal("matchMedia", matchMediaStub);

        toLocaleStringSpy.mockImplementation(() => "16 Apr 2026, 10:15");

        const serverTree = createElement(
            AppProviders,
            undefined,
            createElement(ResultDetailFiles, {
                files,
                resultId: "result-1",
            }),
        );
        const serverMarkup = renderToString(serverTree);
        const container = document.createElement("div");
        const recoverableErrors: unknown[] = [];

        document.body.appendChild(container);
        container.innerHTML = serverMarkup;

        toLocaleStringSpy.mockImplementation(() => "17 Apr 2026, 10:15");

        let root: ReturnType<typeof hydrateRoot> | null = null;

        await act(async () => {
            root = hydrateRoot(container, serverTree, {
                onRecoverableError: (error) => {
                    recoverableErrors.push(error);
                },
            });
        });

        expect(
            container.querySelector(
                'button[data-file-path="/results/a/sample.bam"]',
            ),
        ).not.toBeNull();

        fireEvent.click(
            container.querySelector(
                'button[data-directory-path="/results/b"]',
            )!,
        );

        await waitFor(() => {
            expect(
                container.querySelector(
                    'button[data-file-path="/results/a/sample.bam"]',
                ),
            ).toBeNull();
        });

        expect(recoverableErrors).toHaveLength(0);

        await act(async () => {
            root?.unmount();
        });
    });

    it("hydrates a saved file glob filter after mount without changing the first client render", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");
        const files = [
            buildFile("/results/a/sample.bam"),
            buildFile("/results/a/sample.cram"),
        ];
        const tree = createElement(ResultDetailFiles, {
            files,
            filterStorageKey: "pipeline-alpha",
            resultId: "result-1",
        });
        const container = document.createElement("div");
        const recoverableErrors: unknown[] = [];

        window.localStorage.setItem(
            "wa:file-browser:glob-filter:pipeline-alpha",
            "*.bam",
        );
        document.body.appendChild(container);

        const serverMarkup = renderToString(tree);

        expect(serverMarkup).toContain("sample.bam");
        expect(serverMarkup).toContain("sample.cram");

        container.innerHTML = serverMarkup;

        let root: ReturnType<typeof hydrateRoot> | null = null;

        await act(async () => {
            root = hydrateRoot(container, tree, {
                onRecoverableError: (error) => {
                    recoverableErrors.push(error);
                },
            });
        });

        await waitFor(() => {
            const input = container.querySelector(
                'input[aria-label="Filter files by glob"]',
            ) as HTMLInputElement | null;

            expect(input?.value).toBe("*.bam");
        });

        expect(container.textContent).toContain("sample.bam");
        expect(container.textContent).not.toContain("sample.cram");
        expect(recoverableErrors).toHaveLength(0);

        await act(async () => {
            root?.unmount();
        });
    });

    it("applies default glob wildcard toggles before rendering result-detail files", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");
        const files = [
            buildFile("/results/a/barcodes.bam"),
            buildFile("/results/a/raw-barcodes.bam"),
            buildFile("/results/a/metrics.bam"),
        ];
        const { container } = render(
            createElement(ResultDetailFiles, {
                files,
                resultId: "result-1",
            }),
        );
        const input = container.querySelector(
            'input[aria-label="Filter files by glob"]',
        ) as HTMLInputElement | null;
        const leadingWildcardButton = container.querySelector(
            'button[aria-label="Include leading wildcard"]',
        ) as HTMLButtonElement | null;

        expect(input).toBeTruthy();
        expect(leadingWildcardButton?.getAttribute("aria-pressed")).toBe(
            "true",
        );

        await act(async () => {
            if (!input) {
                throw new Error("Missing glob filter input");
            }

            fireEvent.change(input, {
                target: { value: "barcodes" },
            });
        });

        await waitFor(() => {
            expect(container.textContent).toContain("barcodes.bam");
            expect(container.textContent).toContain("raw-barcodes.bam");
        });
        expect(container.textContent).not.toContain("metrics.bam");

        await act(async () => {
            leadingWildcardButton?.click();
        });

        await waitFor(() => {
            expect(container.textContent).toContain("barcodes.bam");
            expect(container.textContent).not.toContain("raw-barcodes.bam");
        });
    });

    it("hydrates the result detail page without mismatches and keeps directory switching interactive when locale formatting differs", async () => {
        const files = [
            buildFile("/results/a/sample.bam"),
            buildFile("/results/b/report.txt"),
        ];
        const result = buildResultSet();
        const toLocaleStringSpy = vi.spyOn(Date.prototype, "toLocaleString");

        vi.stubGlobal("matchMedia", matchMediaStub);
        fetchFilesMock.mockResolvedValue(files);
        fetchResultMock.mockResolvedValue(result);
        validateIdentifierMock.mockResolvedValue(true);
        enrichSeqmetaMetadataBatchMock.mockResolvedValue({
            enrichments: {},
            errors: {},
        });
        buildCachedEnrichmentStateMock.mockReturnValue({
            enrichments: {},
            errors: {},
        });
        collectSeqmetaValuesMock.mockReturnValue(["SANG001"]);
        hasUsableMLWHCacheEntryMock.mockReturnValue(false);
        mergeSeqmetaEnrichmentStateMock.mockImplementation(
            (base, override) => ({
                enrichments: {
                    ...base.enrichments,
                    ...override?.enrichments,
                },
                errors: {
                    ...base.errors,
                    ...override?.errors,
                },
            }),
        );
        getRequestMLWHCacheMock.mockResolvedValue({});

        toLocaleStringSpy.mockImplementation(() => "16 Apr 2026, 10:15");

        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");
        const Page = pageModule.ResultDetailPageContent;
        const serverTree = createElement(
            AppProviders,
            undefined,
            await Page({ id: "result-1" }),
        );
        const serverMarkup = renderToString(serverTree);
        const container = document.createElement("div");
        const recoverableErrors: unknown[] = [];

        document.body.appendChild(container);
        container.innerHTML = serverMarkup;

        toLocaleStringSpy.mockImplementation(() => "17 Apr 2026, 10:15");

        let root: ReturnType<typeof hydrateRoot> | null = null;

        await act(async () => {
            root = hydrateRoot(container, serverTree, {
                onRecoverableError: (error) => {
                    recoverableErrors.push(error);
                },
            });
        });

        fireEvent.click(
            container.querySelector(
                'button[data-directory-path="/results/b"]',
            )!,
        );

        await waitFor(() => {
            expect(
                container.querySelector(
                    'button[data-file-path="/results/a/sample.bam"]',
                ),
            ).toBeNull();
        });

        expect(recoverableErrors).toHaveLength(0);

        await act(async () => {
            root?.unmount();
        });
    });

    it("integrates registration and metadata in the top result detail summary without duplicated identity fields", async () => {
        const result = {
            ...buildResultSet(),
            metadata: {
                library: "exon",
                seqmeta_studyid: "6568",
                study: "study-alpha",
            },
        };

        fetchFilesMock.mockResolvedValue([buildFile("/results/a/report.csv")]);
        fetchResultMock.mockResolvedValue(result);
        enrichSeqmetaMetadataBatchMock.mockResolvedValue({
            enrichments: {},
            errors: {},
        });
        buildCachedEnrichmentStateMock.mockReturnValue({
            enrichments: {},
            errors: {},
        });
        collectSeqmetaValuesMock.mockReturnValue(["6568"]);
        hasUsableMLWHCacheEntryMock.mockReturnValue(false);
        mergeSeqmetaEnrichmentStateMock.mockImplementation(
            (base, override) => ({
                enrichments: {
                    ...base.enrichments,
                    ...override?.enrichments,
                },
                errors: {
                    ...base.errors,
                    ...override?.errors,
                },
            }),
        );

        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");
        const Page = pageModule.ResultDetailPageContent;
        const markup = renderToStaticMarkup(
            await Page({
                id: "result-1",
            }),
        );
        const container = document.createElement("div");

        container.innerHTML = markup;

        const detailSummary = container.querySelector<HTMLElement>(
            '[data-result-detail-summary="true"]',
        );
        const registrationLabels = Array.from(
            detailSummary?.querySelectorAll<HTMLElement>(
                "[data-registration-field], [data-registration-wide-field]",
            ) ?? [],
        ).map(
            (field) =>
                field.getAttribute("data-registration-field") ??
                field.getAttribute("data-registration-wide-field"),
        );
        const metadataKeys = Array.from(
            detailSummary?.querySelectorAll<HTMLElement>(
                "[data-metadata-row]",
            ) ?? [],
        ).map((row) => row.getAttribute("data-metadata-row"));
        const titleFileSummary = detailSummary?.querySelector<HTMLElement>(
            "[data-title-file-summary]",
        );

        expect(detailSummary).not.toBeNull();
        expect(
            detailSummary?.querySelector('a[data-return-link="true"]'),
        ).toBeNull();
        expect(
            detailSummary?.querySelector(
                '[data-registration-layout="integrated"]',
            ),
        ).not.toBeNull();
        expect(
            detailSummary?.querySelector(
                '[data-result-metadata-layout="integrated"]',
            ),
        ).not.toBeNull();
        expect(registrationLabels).toEqual([
            "Last updated",
            "Requester",
            "Operator",
        ]);
        expect(registrationLabels).not.toContain("Result ID");
        expect(registrationLabels).not.toContain("Pipeline name");
        expect(metadataKeys).toEqual(["library", "study"]);
        expect(detailSummary?.textContent).toContain("1001");
        expect(detailSummary?.textContent).not.toContain("runid=1001");
        expect(titleFileSummary?.textContent).toContain("1 file");
        expect(titleFileSummary?.textContent).toContain("512 B");
        expect(detailSummary?.textContent).toContain("Study");
        expect(detailSummary?.textContent).toContain("libraryexon");
        expect(detailSummary?.textContent).not.toContain("6568");
        expect(detailSummary?.textContent).toContain("study-alpha");
        expect(container.querySelector("[data-result-id-copy]")).toBeNull();
        expect(container.querySelector("h1")?.textContent).toContain(
            "nf-core/rnaseq",
        );
        expect(container.querySelector("h1")?.textContent).not.toContain(
            result.id,
        );
        expect(
            container.querySelector('[data-registration-layout="compact"]'),
        ).toBeNull();
        expect(markup).not.toMatch(/>Registration</);
        expect(markup).not.toMatch(/>Result metadata</);
    });

    it("titles the result detail page with project metadata and falls back to pipeline name", async () => {
        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");
        const Page = pageModule.ResultDetailPageContent;
        const renderHeading = async (result: ResultSet) => {
            fetchFilesMock.mockResolvedValue([
                buildFile("/results/a/report.csv"),
            ]);
            fetchResultMock.mockResolvedValue(result);
            enrichSeqmetaMetadataBatchMock.mockResolvedValue({
                enrichments: {},
                errors: {},
            });
            buildCachedEnrichmentStateMock.mockReturnValue({
                enrichments: {},
                errors: {},
            });
            collectSeqmetaValuesMock.mockReturnValue([]);
            hasUsableMLWHCacheEntryMock.mockReturnValue(false);
            mergeSeqmetaEnrichmentStateMock.mockImplementation(
                (base, override) => ({
                    enrichments: {
                        ...base.enrichments,
                        ...override?.enrichments,
                    },
                    errors: {
                        ...base.errors,
                        ...override?.errors,
                    },
                }),
            );

            const markup = renderToStaticMarkup(
                await Page({
                    id: result.id,
                }),
            );
            const container = document.createElement("div");

            container.innerHTML = markup;

            const heading = container.querySelector("h1");
            const unique = heading?.querySelector("span");

            return {
                text: heading?.textContent ?? "",
                uniqueText: unique?.textContent ?? "",
            };
        };
        const projectHeading = await renderHeading({
            ...buildResultSet(),
            metadata: {
                project: "Atlas cohort",
            },
        });
        const fallbackHeading = await renderHeading({
            ...buildResultSet(),
            id: "result-with-blank-project",
            metadata: {
                project: "   ",
            },
        });

        expect(projectHeading.text).toContain("Atlas cohort");
        expect(projectHeading.text).toContain("1001");
        expect(projectHeading.text).not.toContain("nf-core/rnaseq");
        expect(projectHeading.uniqueText).toBe("1001");
        expect(fallbackHeading.text).toContain("nf-core/rnaseq");
        expect(fallbackHeading.text).toContain("1001");
        expect(fallbackHeading.uniqueText).toBe("1001");
    });

    it("keeps the raw stored run key out of the Run details popover while preserving Unique", async () => {
        const result = {
            ...buildResultSet(),
            run_key: "runid=48522&unique=exon_lib",
            metadata: {
                library: "exon",
                seqmeta_studyid: "6568",
                study: "study-alpha",
            },
        };

        fetchFilesMock.mockResolvedValue([buildFile("/results/a/report.csv")]);
        fetchResultMock.mockResolvedValue(result);
        enrichSeqmetaMetadataBatchMock.mockResolvedValue({
            enrichments: {},
            errors: {},
        });
        buildCachedEnrichmentStateMock.mockReturnValue({
            enrichments: {},
            errors: {},
        });
        collectSeqmetaValuesMock.mockReturnValue(["6568"]);
        hasUsableMLWHCacheEntryMock.mockReturnValue(false);
        mergeSeqmetaEnrichmentStateMock.mockImplementation(
            (base, override) => ({
                enrichments: {
                    ...base.enrichments,
                    ...override?.enrichments,
                },
                errors: {
                    ...base.errors,
                    ...override?.errors,
                },
            }),
        );

        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");
        const Page = pageModule.ResultDetailPageContent;
        const page = await Page({
            id: "result-1",
        });

        vi.stubGlobal("matchMedia", matchMediaStub);

        render(createElement(AppProviders, undefined, page));

        await act(async () => {
            fireEvent.click(
                document.querySelector<HTMLElement>(
                    '[data-registration-details-trigger="true"]',
                )!,
            );
        });

        await waitFor(() => {
            expect(
                document.querySelector(
                    '[data-registration-details-panel="true"]',
                ),
            ).not.toBeNull();
        });

        const detailLabels = Array.from(
            document.querySelectorAll<HTMLElement>(
                "[data-registration-detail-field] dt",
            ),
        ).map((label) => label.textContent);
        const uniqueDetail = document.querySelector<HTMLElement>(
            '[data-registration-detail-field="Unique"]',
        );

        expect(detailLabels).toContain("Unique");
        expect(detailLabels).not.toContain("Raw run key");
        expect(uniqueDetail?.textContent).toContain("48522 / exon_lib");
        expect(document.body.textContent).not.toContain(
            "runid=48522&unique=exon_lib",
        );
    });

    it("renders the detail page without waiting for server-side seqmeta enrichment", async () => {
        const files = [buildFile("/results/a/sample.bam")];
        const result = buildResultSet();
        const pendingEnrichment = deferred<{
            enrichments: Record<string, never>;
            errors: Record<string, never>;
        }>();

        fetchFilesMock.mockResolvedValue(files);
        fetchResultMock.mockResolvedValue(result);
        collectSeqmetaValuesMock.mockReturnValue(["SANG001"]);
        enrichSeqmetaMetadataBatchMock.mockReturnValue(
            pendingEnrichment.promise,
        );

        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");
        const Page = pageModule.ResultDetailPageContent;
        const pagePromise = Page({
            id: "result-1",
        });
        const renderState = await Promise.race([
            pagePromise.then(() => "resolved" as const),
            new Promise<"timeout">((resolve) =>
                setTimeout(() => resolve("timeout"), 100),
            ),
        ]);

        expect(renderState).toBe("resolved");
        expect(enrichSeqmetaMetadataBatchMock).not.toHaveBeenCalled();

        pendingEnrichment.resolve({ enrichments: {}, errors: {} });
        await pagePromise;
    });

    it("renders a loading shell without waiting for result fetches", async () => {
        vi.stubGlobal("matchMedia", matchMediaStub);

        const loadingModule =
            await import("@/app/(results)/results/[id]/loading");
        const Loading = loadingModule.default;
        const serverTree = createElement(
            AppProviders,
            undefined,
            createElement(Loading),
        );
        const serverMarkup = renderToString(serverTree);

        expect(serverMarkup).toContain("Loading result details");
        expect(serverMarkup).not.toContain("Result detail");
        expect(fetchResultMock).not.toHaveBeenCalled();
        expect(fetchFilesMock).not.toHaveBeenCalled();
    });
});
