// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { fireEvent, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AppProviders } from "@/components/app-providers";
import type { FileEntry, ResultSet } from "@/lib/contracts";

const fetchFilesMock = vi.fn();
const fetchResultMock = vi.fn();
const validateIdentifierMock = vi.fn();
const enrichIdentifierMock = vi.fn();
const enrichSeqmetaMetadataMock = vi.fn();
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
    validateIdentifier: validateIdentifierMock,
}));

vi.mock("@/lib/seqmeta-enrichment", () => ({
    buildCachedEnrichmentState: buildCachedEnrichmentStateMock,
    collectSeqmetaValues: collectSeqmetaValuesMock,
    enrichSeqmetaMetadata: enrichSeqmetaMetadataMock,
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
        enrichSeqmetaMetadataMock.mockResolvedValue({
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

    it("renders the detail page without waiting for server-side seqmeta enrichment", async () => {
        const files = [buildFile("/results/a/sample.bam")];
        const result = buildResultSet();
        const pendingEnrichment = deferred<{
            enrichments: Record<string, never>;
            errors: Record<string, never>;
        }>();

        fetchFilesMock.mockResolvedValue(files);
        fetchResultMock.mockResolvedValue(result);
        enrichSeqmetaMetadataMock.mockReturnValue(pendingEnrichment.promise);

        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");
        const Page = pageModule.ResultDetailPageContent;
        const pagePromise = Page({
            id: "result-1",
        });

        const renderState = await Promise.race([
            pagePromise.then(() => "resolved" as const),
            new Promise<"timeout">((resolve) => {
                setTimeout(() => resolve("timeout"), 25);
            }),
        ]);

        expect(renderState).toBe("resolved");
        expect(enrichSeqmetaMetadataMock).not.toHaveBeenCalled();

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
        expect(fetchResultMock).not.toHaveBeenCalled();
        expect(fetchFilesMock).not.toHaveBeenCalled();
    });
});
