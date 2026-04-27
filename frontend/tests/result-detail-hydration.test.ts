// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { fireEvent, screen, waitFor } from "@testing-library/react";
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
const mergeSeqmetaEnrichmentStateMock = vi.fn();
const primeSeqmetaCacheMock = vi.fn();

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
        const recoverableErrors: Error[] = [];

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
        const writeTextMock = vi.fn().mockResolvedValue(undefined);

        vi.stubGlobal("matchMedia", matchMediaStub);
        vi.stubGlobal("navigator", {
            clipboard: {
                writeText: writeTextMock,
            },
        });
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

        const pageModule = await import("@/app/(results)/results/[id]/page");
        const Page = pageModule.default;
        const serverTree = createElement(
            AppProviders,
            undefined,
            await Page({ params: Promise.resolve({ id: "result-1" }) }),
        );
        const serverMarkup = renderToString(serverTree);
        const container = document.createElement("div");
        const recoverableErrors: Error[] = [];

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

        const copyButton = screen.getByRole("button", {
            name: `Copy result ID ${result.id}`,
        });

        expect(copyButton.getAttribute("data-result-id-copy")).toBe(result.id);
        expect(copyButton.textContent).not.toContain(result.id);
        expect(copyButton.textContent).toContain("...");

        fireEvent.click(copyButton);

        await waitFor(() => {
            expect(writeTextMock).toHaveBeenCalledWith(result.id);
        });

        expect(recoverableErrors).toHaveLength(0);

        await act(async () => {
            root?.unmount();
        });
    });
});
