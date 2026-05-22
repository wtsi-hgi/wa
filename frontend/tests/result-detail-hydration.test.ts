// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToStaticMarkup, renderToString } from "react-dom/server";
import { fireEvent, waitFor } from "@testing-library/react";
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
const getRequestSeqmetaCacheMock = vi.fn();
const buildCachedEnrichmentStateMock = vi.fn();
const collectSeqmetaValuesMock = vi.fn();
const hasUsableSeqmetaCacheEntryMock = vi.fn();
const mergeSeqmetaEnrichmentStateMock = vi.fn();
const primeSeqmetaCacheMock = vi.fn();
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

vi.mock("@/lib/seqmeta-enrichment", () => ({
    buildCachedEnrichmentState: buildCachedEnrichmentStateMock,
    collectSeqmetaValues: collectSeqmetaValuesMock,
    enrichSeqmetaMetadata: enrichSeqmetaMetadataMock,
    enrichSeqmetaMetadataBatch: enrichSeqmetaMetadataBatchMock,
    hasUsableSeqmetaCacheEntry: hasUsableSeqmetaCacheEntryMock,
    mergeSeqmetaEnrichmentState: mergeSeqmetaEnrichmentStateMock,
    primeSeqmetaCache: primeSeqmetaCacheMock,
}));

vi.mock("@/lib/seqmeta-cache-server", () => ({
    getRequestSeqmetaCache: getRequestSeqmetaCacheMock,
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
        hasUsableSeqmetaCacheEntryMock.mockReturnValue(false);
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
        getRequestSeqmetaCacheMock.mockResolvedValue({});

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
        hasUsableSeqmetaCacheEntryMock.mockReturnValue(false);
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
        expect(metadataKeys).toEqual(["seqmeta_studyid"]);
        expect(detailSummary?.textContent).toContain("1001");
        expect(detailSummary?.textContent).not.toContain("runid=1001");
        expect(titleFileSummary?.textContent).toContain("1 file");
        expect(titleFileSummary?.textContent).toContain("512 B");
        expect(detailSummary?.textContent).toContain("Study");
        expect(detailSummary?.textContent).not.toContain("libraryexon");
        expect(detailSummary?.textContent).toContain("6568");
        expect(detailSummary?.textContent).not.toContain("study-alpha");
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
