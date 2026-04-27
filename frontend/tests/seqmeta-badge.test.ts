/**
 * @vitest-environment jsdom
 */

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EnrichmentResult, FileEntry, ResultSet } from "@/lib/contracts";
import {
    buildSeqmetaCacheCookie,
    deserializeSeqmetaCacheCookie,
    SEQMETA_CACHE_COOKIE_NAME,
} from "@/lib/seqmeta-cache-core";
import {
    SeqmetaCache,
    SeqmetaCacheContext,
    SeqmetaCacheProvider,
} from "@/lib/seqmeta-cache";

const fetchResultMock = vi.fn();
const fetchFilesMock = vi.fn();
const validateIdentifierMock = vi.fn();
const enrichIdentifierMock = vi.fn();
const cookiesMock = vi.fn();
const originalDocumentCookie = Object.getOwnPropertyDescriptor(
    document,
    "cookie",
);
let cookieJar = "";
let cookieWrites: string[] = [];

vi.mock("next/headers", () => ({
    cookies: cookiesMock,
}));

vi.mock("@/app/(results)/actions", () => ({
    fetchResult: fetchResultMock,
    fetchFiles: fetchFilesMock,
    validateIdentifier: validateIdentifierMock,
    enrichIdentifier: enrichIdentifierMock,
}));

function setRequestCookieHeader(cookieHeader?: string) {
    cookiesMock.mockResolvedValue({
        get(name: string) {
            const prefix = `${name}=`;
            const cookie = cookieHeader
                ?.split(";")
                .map((entry) => entry.trim())
                .find((entry) => entry.startsWith(prefix));

            if (!cookie) {
                return undefined;
            }

            return { value: cookie.slice(prefix.length) };
        },
    });
}

function readSeqmetaCookieFromDocument(): string | undefined {
    const prefix = `${SEQMETA_CACHE_COOKIE_NAME}=`;

    return document.cookie
        .split(";")
        .map((entry) => entry.trim())
        .find((entry) => entry.startsWith(prefix));
}

beforeEach(() => {
    cookieJar = "";
    cookieWrites = [];
    Object.defineProperty(document, "cookie", {
        configurable: true,
        get() {
            return cookieJar;
        },
        set(value: string) {
            cookieWrites.push(value);
            const [cookiePair = "", ...attributes] = value
                .split(";")
                .map((entry) => entry.trim());

            if (attributes.some((entry) => entry === "Max-Age=0")) {
                cookieJar = "";
                return;
            }

            cookieJar = cookiePair;
        },
    });
});

function buildResultSet(overrides: Partial<ResultSet> = {}): ResultSet {
    return {
        id: "result-42",
        pipeline_identifier: "gh://repo/pipeline.nf",
        run_key: "runid=42",
        requester: "alice",
        operator: "bob",
        command: "nextflow run pipeline.nf",
        pipeline_name: "nf-core/rnaseq",
        pipeline_version: "3.18.0",
        output_directory: "/tmp/results/42",
        metadata: {
            seqmeta_sampleid: "SANG001",
        },
        created_at: "2026-04-16T10:00:00Z",
        updated_at: "2026-04-16T10:30:00Z",
        ...overrides,
    };
}

function buildFileEntry(overrides: Partial<FileEntry> = {}): FileEntry {
    return {
        path: "/tmp/results/42/report.html",
        kind: "output",
        mtime: "2026-04-16T10:15:00Z",
        size: 120,
        ...overrides,
    };
}

function buildEnrichment(
    overrides: Partial<EnrichmentResult> = {},
): EnrichmentResult {
    return {
        identifier: "SANG001",
        type: "sanger_sample_id",
        graph: {
            study: {
                id_study_tmp: 42,
                id_lims: "SQSCP",
                id_study_lims: "6568",
                name: "RNA Seq",
                faculty_sponsor: "Dr Example",
                state: "active",
                abstract: "Study abstract",
                abbreviation: "RNA",
                accession_number: "ERP123456",
                description: "Study description",
                data_release_strategy: "managed",
                study_title: "RNA Study",
                data_access_group: "group-a",
                hmdmc_number: "HMDMC-1",
                programme: "Transcriptomics",
                created: "2026-04-20T09:00:00Z",
                reference_genome: "GRCh38",
                ethically_approved: true,
                study_type: "Whole Genome Sequencing",
                contains_human_dna: true,
                contaminated_human_dna: false,
                study_visibility: "Always Open",
                ega_dac_accession_number: "EGAC00001",
                ega_policy_accession_number: "EGAP00001",
                data_release_timing: "Immediate",
            },
            sample: {
                id_study_lims: "6568",
                id_sample_lims: "LIMS001",
                sanger_id: "SANG001",
                sample_name: "Sample 1",
                taxon_id: 9606,
                common_name: "Human",
                library_type: "RNA",
                id_run: 1234,
                lane: 1,
                tag_index: 7,
                irods_path: "/seq/1234",
                study_accession_number: "ERP123456",
                accession_number: "ERS123456",
            },
            libraries: [
                {
                    library_type: "RNA",
                    id_study_lims: "6568",
                },
            ],
        },
        partial: false,
        ...overrides,
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

function countOccurrences(haystack: string, needle: string): number {
    return haystack.split(needle).length - 1;
}

describe("M1 result detail seqmeta enrichment", () => {
    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
        document.cookie = `${SEQMETA_CACHE_COOKIE_NAME}=; Max-Age=0; Path=/`;
        setRequestCookieHeader();

        if (originalDocumentCookie) {
            Object.defineProperty(document, "cookie", originalDocumentCookie);
        }
    });

    it("opens a modal with structured seqmeta details for an enriched badge", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: buildEnrichment(),
            }),
        );

        expect(screen.getByText("sanger_sample_id: SANG001")).toBeTruthy();
        expect(screen.queryByRole("dialog")).toBeNull();

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });
        expect(screen.getByText("Selected metadata value")).toBeTruthy();
        expect(screen.getByText("Study name")).toBeTruthy();
        expect(screen.getAllByText("RNA Seq").length).toBeGreaterThan(0);
        expect(
            screen
                .getByRole("link", {
                    name: /send study_id to search filter/i,
                })
                .getAttribute("href"),
        ).toBe("/?study_id=6568");
    });

    it("uses the study name as the badge label for a full study enrichment", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                }),
            }),
        );

        expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
            "RNA Seq",
        );
        expect(screen.queryByText("study_id: 6568")).toBeNull();
    });

    it("shows the raw value with a failure indicator and unavailable tooltip when enrichment fails", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: null,
                error: "not_found",
            }),
        );

        expect(screen.getByText("SANG001")).toBeTruthy();
        expect(
            screen.getByLabelText("enrichment unavailable").textContent,
        ).toContain("?");
        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });
        expect(screen.getByText("enrichment unavailable")).toBeTruthy();
    });

    it("renders non-seqmeta metadata without attempting enrichment", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");

        render(
            createElement(ResultMetadata, {
                metadata: {
                    library: "exon",
                },
            }),
        );

        expect(screen.getByText("library")).toBeTruthy();
        expect(screen.getByText("exon")).toBeTruthy();
        expect(enrichIdentifierMock).not.toHaveBeenCalled();
    });

    it("renders seqmeta metadata as a modal trigger without inline panels", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");

        render(
            createElement(ResultMetadata, {
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
                enrichments: {
                    SANG001: buildEnrichment(),
                },
            }),
        );

        const metadataTable = screen.getByRole("table");
        const metadataWrapper = metadataTable.parentElement;

        expect(metadataWrapper).toBeTruthy();
        expect(metadataWrapper?.className).toContain("overflow-hidden");
        expect(screen.queryByRole("tooltip")).toBeNull();

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });
        expect(screen.getByText("Study name")).toBeTruthy();
        expect(screen.getAllByText("RNA Seq").length).toBeGreaterThan(0);
    });

    it("starts all seqmeta enrichments in parallel during server detail rendering", async () => {
        const pending = Array.from({ length: 5 }, () =>
            deferred<EnrichmentResult | null>(),
        );
        const queue = [...pending];

        fetchResultMock.mockResolvedValue(
            buildResultSet({
                metadata: {
                    seqmeta_sampleid: "SANG001",
                    seqmeta_sample_lims: "LIMS001",
                    seqmeta_runid: "1234",
                    seqmeta_studyid: "6568",
                    seqmeta_library: "RNA",
                },
            }),
        );
        fetchFilesMock.mockResolvedValue([]);
        setRequestCookieHeader();

        enrichIdentifierMock.mockImplementation((value: string) => {
            const next = queue.shift();

            if (!next) {
                throw new Error(`unexpected identifier ${value}`);
            }

            return next.promise;
        });

        const pageModule = await import("@/app/(results)/results/[id]/page");
        const renderPromise = pageModule.default({
            params: Promise.resolve({ id: "result-42" }),
        });

        await waitFor(() => {
            expect(enrichIdentifierMock).toHaveBeenCalledTimes(5);
        });

        for (const [index, item] of pending.entries()) {
            item.resolve(buildEnrichment({ identifier: `ID-${index}` }));
        }

        renderToStaticMarkup(await renderPromise);
    });

    it("shows an empty metadata state when a result set has no metadata", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");

        render(createElement(ResultMetadata, { metadata: {} }));

        expect(screen.getByText("No metadata")).toBeTruthy();
    });

    it("renders the detail page shell with server-started seqmeta enrichment", async () => {
        fetchResultMock.mockResolvedValue(
            buildResultSet({
                metadata: {
                    seqmeta_sampleid: "SANG001",
                    library: "rna",
                },
            }),
        );
        fetchFilesMock.mockResolvedValue([
            buildFileEntry({
                size: 1536,
            }),
            buildFileEntry({
                path: "/tmp/results/42/input.cram",
                kind: "input",
                size: 512,
            }),
        ]);
        enrichIdentifierMock.mockResolvedValue(buildEnrichment());
        setRequestCookieHeader();

        const pageModule = await import("@/app/(results)/results/[id]/page");
        const Page = pageModule.default;
        const markup = renderToStaticMarkup(
            await Page({
                params: Promise.resolve({ id: "result-42" }),
            }),
        );

        expect(fetchResultMock).toHaveBeenCalledWith("result-42");
        expect(fetchFilesMock).toHaveBeenCalledWith("result-42");
        expect(enrichIdentifierMock).toHaveBeenCalledWith("SANG001");
        expect(markup).toContain("Directories");
        expect(markup).toContain("Selected file");
        expect(markup).toContain("/tmp/results/42/report.html");
        expect(markup).toContain("Result metadata");
        expect(markup).toContain("sanger_sample_id: SANG001");
        expect(markup).toContain("Registered files");
        expect(markup).toContain('data-registration-layout="compact"');
        expect(markup).toContain("Key details");
        expect(markup).not.toContain("Registration summary");
        expect(
            countOccurrences(markup, 'class="border-b border-border/60 pb-3"'),
        ).toBe(9);
        expect(
            countOccurrences(
                markup,
                'class="rounded-[1.25rem] border border-border/70 bg-background/60 px-4 py-3"',
            ),
        ).toBe(2);
        expect(markup).toContain("2 files");
        expect(markup).toContain("2.0 KB");
        expect(markup).toContain("Output");
        expect(markup).toContain("Input");
        expect(markup).toContain("Pipeline");
        expect(markup).toContain("1 file");
        expect(markup).toContain("1.5 KB");
        expect(markup).toContain("512 B");
        expect(markup).toContain("0 files");
        expect(markup).toContain("0 B");
        expect(markup).not.toContain("1 input");
        expect(markup).not.toContain("1 output");
        expect(markup).not.toContain(
            "Review the full registration envelope, inspect seqmeta-linked values, and confirm the registered file footprint before opening previews.",
        );
        expect(markup).not.toContain(
            "Browse the registered inventory directly on this page, pivot by source, and open the selected asset through the existing file proxy route.",
        );
        expect(markup).not.toContain("File browser");
    });

    it("primes the client cache with server enrichments so remounts reuse them", async () => {
        const enrichment = buildEnrichment();

        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        const cache = new SeqmetaCache();
        const metadata = {
            seqmeta_sampleid: "SANG001",
        };

        const firstRender = render(
            createElement(ResultMetadataEnrichment, {
                initialEnrichments: {
                    SANG001: enrichment,
                },
                metadata,
            }),
            {
                wrapper: ({ children }) =>
                    createElement(
                        SeqmetaCacheContext.Provider,
                        { value: cache },
                        children,
                    ),
            },
        );

        expect(screen.getByText("sanger_sample_id: SANG001")).toBeTruthy();
        expect(enrichIdentifierMock).not.toHaveBeenCalled();

        firstRender.unmount();

        render(
            createElement(ResultMetadataEnrichment, {
                metadata,
            }),
            {
                wrapper: ({ children }) =>
                    createElement(
                        SeqmetaCacheContext.Provider,
                        { value: cache },
                        children,
                    ),
            },
        );

        expect(screen.getByText("sanger_sample_id: SANG001")).toBeTruthy();
        expect(enrichIdentifierMock).not.toHaveBeenCalled();
    });

    it("prefers available enrichment details over a stale unavailable marker", async () => {
        const enrichment = buildEnrichment();

        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                initialEnrichments: {
                    SANG001: enrichment,
                },
                initialErrors: {
                    SANG001: "not_found",
                },
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        expect(screen.getByText("sanger_sample_id: SANG001")).toBeTruthy();
        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });
        expect(screen.getAllByText("RNA Seq").length).toBeGreaterThan(0);
        expect(screen.queryByLabelText("enrichment unavailable")).toBeNull();
    });

    it("mirrors the client cache to a cookie and reuses it on the next detail render", async () => {
        fetchFilesMock.mockResolvedValue([]);
        fetchResultMock
            .mockResolvedValueOnce(
                buildResultSet({
                    id: "result-42",
                    metadata: { seqmeta_sampleid: "SANG001" },
                }),
            )
            .mockResolvedValueOnce(
                buildResultSet({
                    id: "result-99",
                    metadata: { seqmeta_sampleid: "SANG001" },
                }),
            );
        enrichIdentifierMock.mockResolvedValue(buildEnrichment());
        setRequestCookieHeader();

        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        const pageModule = await import("@/app/(results)/results/[id]/page");

        render(
            createElement(ResultMetadataEnrichment, {
                initialEnrichments: {
                    SANG001: buildEnrichment(),
                },
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            expect(readSeqmetaCookieFromDocument()).toContain(
                SEQMETA_CACHE_COOKIE_NAME,
            );
        });

        const persistedCookie = readSeqmetaCookieFromDocument();
        expect(persistedCookie).toBeTruthy();

        const cookieValue = persistedCookie?.split("=").slice(1).join("=");
        expect(
            deserializeSeqmetaCacheCookie(cookieValue).SANG001?.identifier,
        ).toBe("SANG001");

        const firstMarkup = renderToStaticMarkup(
            await pageModule.default({
                params: Promise.resolve({ id: "result-42" }),
            }),
        );

        expect(enrichIdentifierMock).toHaveBeenCalledTimes(1);
        expect(firstMarkup).toContain("sanger_sample_id: SANG001");

        setRequestCookieHeader(
            buildSeqmetaCacheCookie({ SANG001: buildEnrichment() }),
        );

        const secondMarkup = renderToStaticMarkup(
            await pageModule.default({
                params: Promise.resolve({ id: "result-99" }),
            }),
        );

        expect(enrichIdentifierMock).toHaveBeenCalledTimes(1);
        expect(secondMarkup).toContain("sanger_sample_id: SANG001");
    });

    it("does not rewrite the seqmeta cookie when mount enrichment matches the existing cache", async () => {
        const enrichment = buildEnrichment();

        document.cookie = buildSeqmetaCacheCookie({ SANG001: enrichment });
        const writesBeforeRender = cookieWrites.length;

        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                initialEnrichments: {
                    SANG001: enrichment,
                },
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            expect(screen.getByText("sanger_sample_id: SANG001")).toBeTruthy();
        });

        expect(cookieWrites).toHaveLength(writesBeforeRender);
        expect(readSeqmetaCookieFromDocument()).toBe(document.cookie);
    });

    it("marks seqmeta enrichment as unavailable when server validation fails", async () => {
        fetchResultMock.mockResolvedValue(
            buildResultSet({
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
            }),
        );
        fetchFilesMock.mockResolvedValue([]);
        const { BackendRequestError } = await import("@/lib/backend-client");
        enrichIdentifierMock.mockRejectedValue(
            new BackendRequestError(502, {
                error: "seqmeta: all enrichment hops failed",
            }),
        );
        setRequestCookieHeader();

        const pageModule = await import("@/app/(results)/results/[id]/page");
        const markup = renderToStaticMarkup(
            await pageModule.default({
                params: Promise.resolve({ id: "result-42" }),
            }),
        );

        expect(markup).toContain("enrichment backend impaired");
        expect(markup).toContain("SANG001");
    });
});
