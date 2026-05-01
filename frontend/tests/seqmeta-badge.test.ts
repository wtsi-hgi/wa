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

        expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
            "SANG001",
        );
        expect(screen.queryByRole("dialog")).toBeNull();

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });
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

    it("renders seqmeta_library details without singular sample or study guesses", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_library",
                rawValue: "RNA",
                enrichment: buildEnrichment({
                    graph: {
                        ...buildEnrichment().graph,
                        samples: [
                            {
                                sample_name: "Sample 1",
                                sanger_id: "SANG001",
                            },
                            {
                                sample_name: "Sample 2",
                                sanger_id: "SANG002",
                            },
                        ],
                    },
                }),
            }),
        );

        expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
            "RNA",
        );
        expect(screen.queryByText("sanger_sample_id: RNA")).toBeNull();

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(
            screen.queryByText("Resolved via Sanger sample ID SANG001."),
        ).toBeNull();
        expect(screen.queryByText("Study context: RNA Seq.")).toBeNull();
        expect(screen.queryByText("Resolved seqmeta type")).toBeNull();

        // With the hierarchical fix, library metadata with samples now shows:
        // - Study section with study_id filter link
        // - Samples section with individual sample rows
        expect(screen.getByText("Study")).toBeTruthy();
        expect(screen.getByText("Samples")).toBeTruthy();

        // Study link should be present
        expect(
            screen.getByRole("link", {
                name: /send study_id to search filter/i,
            }),
        ).toBeTruthy();

        // Individual sample filter links should be present
        expect(
            screen.getAllByRole("link", {
                name: /send seqmeta_sampleid to search filter/i,
            }).length,
        ).toBeGreaterThan(0);

        expect(screen.queryByText("Status")).toBeNull();
    });

    it("renders a vertically scrollable body for long seqmeta detail content", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: buildEnrichment({
                    graph: {
                        ...buildEnrichment().graph,
                        samples: Array.from({ length: 24 }, (_, index) => ({
                            sample_name: `Sample ${index + 1}`,
                            sanger_id: `SANG${String(index + 1).padStart(3, "0")}`,
                        })),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const dialog = screen.getByRole("dialog");
        const dialogBody = dialog.querySelector(".overflow-y-auto");

        expect(dialogBody).toBeTruthy();
        expect(dialogBody?.className).toContain("overflow-y-auto");
        expect(dialogBody?.className).toContain("max-h-[calc(100vh-12rem)]");
    });

    it("displays the raw metadata value in the badge, not enriched study name", async () => {
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
            "6568",
        );
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
        expect(
            screen.getByText(
                "No enrichment matched this sanger sample id value.",
            ),
        ).toBeTruthy();
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

    it("marks clickable seqmeta metadata values with a pointer cursor", async () => {
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

        expect(screen.getByTestId("seqmeta-badge-trigger").className).toContain(
            "cursor-pointer",
        );
    });

    it("does not start seqmeta enrichments during server detail rendering", async () => {
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

        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");
        const markup = renderToStaticMarkup(
            await pageModule.ResultDetailPageContent({
                id: "result-42",
            }),
        );

        expect(enrichIdentifierMock).not.toHaveBeenCalled();
        expect(markup).toContain("seqmeta_library");
        expect(markup).toContain("RNA");
    });

    it("shows an empty metadata state when a result set has no metadata", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");

        render(createElement(ResultMetadata, { metadata: {} }));

        expect(screen.getByText("No metadata")).toBeTruthy();
    });

    it("renders the detail page content without server-started seqmeta enrichment", async () => {
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
        setRequestCookieHeader();

        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");
        const Page = pageModule.ResultDetailPageContent;
        const markup = renderToStaticMarkup(
            await Page({
                id: "result-42",
            }),
        );

        expect(fetchResultMock).toHaveBeenCalledWith("result-42");
        expect(fetchFilesMock).toHaveBeenCalledWith("result-42");
        expect(enrichIdentifierMock).not.toHaveBeenCalled();
        expect(markup).not.toContain("Explorer");
        expect(markup).not.toContain("Preview focus");
        expect(markup).not.toContain("data-selected-file-path");
        expect(markup).not.toContain("Path");
        expect(markup).not.toContain("Kind");
        expect(markup).not.toContain("Updated");
        expect(markup).toContain("Result metadata");
        expect(markup).toContain("SANG001");
        expect(markup).not.toContain("sanger_sample_id: SANG001");
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

        expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
            "SANG001",
        );
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

        expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
            "SANG001",
        );
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

        expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
            "SANG001",
        );
        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });
        expect(screen.getAllByText("RNA Seq").length).toBeGreaterThan(0);
        expect(screen.queryByLabelText("enrichment unavailable")).toBeNull();
    });

    it("mirrors the client cache to a cookie while server detail renders stay cache-agnostic", async () => {
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
        setRequestCookieHeader();

        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");

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
        expect(deserializeSeqmetaCacheCookie(cookieValue)).toEqual({});

        const firstMarkup = renderToStaticMarkup(
            await pageModule.ResultDetailPageContent({
                id: "result-42",
            }),
        );

        expect(enrichIdentifierMock).toHaveBeenCalledTimes(0);
        expect(firstMarkup).toContain("SANG001");
        expect(firstMarkup).not.toContain("sanger_sample_id: SANG001");

        setRequestCookieHeader(
            buildSeqmetaCacheCookie({ SANG001: buildEnrichment() }),
        );

        const secondMarkup = renderToStaticMarkup(
            await pageModule.ResultDetailPageContent({
                id: "result-99",
            }),
        );

        expect(enrichIdentifierMock).toHaveBeenCalledTimes(0);
        expect(secondMarkup).toContain("SANG001");
        expect(secondMarkup).not.toContain("sanger_sample_id: SANG001");
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
            expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
                "SANG001",
            );
        });

        expect(cookieWrites).toHaveLength(writesBeforeRender);
        expect(readSeqmetaCookieFromDocument()).toBe(document.cookie);
    });

    it("keeps server detail rendering neutral when seqmeta validation would fail client-side", async () => {
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

        const pageModule =
            await import("@/app/(results)/results/[id]/page-content");
        const markup = renderToStaticMarkup(
            await pageModule.ResultDetailPageContent({
                id: "result-42",
            }),
        );

        expect(enrichIdentifierMock).not.toHaveBeenCalled();
        expect(markup).not.toContain("enrichment backend impaired");
        expect(markup).toContain("SANG001");
    });

    it("shows dialog title matching raw value with key and type in subtitle", async () => {
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

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const dialog = screen.getByRole("dialog");
        const title = dialog.querySelector("h3");

        expect(title?.textContent).toBe("6568");

        const subtitle = title?.nextElementSibling;

        expect(subtitle?.textContent).toContain("seqmeta_studyid");
        expect(subtitle?.textContent).toContain("study_id");
    });

    it("omits duplicate selected metadata value row from dialog", async () => {
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

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.queryByText("Selected metadata value")).toBeNull();
    });

    it("omits redundant resolved seqmeta type row from dialog", async () => {
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

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.queryByText("Resolved seqmeta type")).toBeNull();
    });

    it("omits summary and resolution aside from dialog", async () => {
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

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.queryByText("Summary")).toBeNull();
        expect(screen.queryByText("Resolution")).toBeNull();
    });

    it("groups libraries without label duplication when study_detail hierarchy is present", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                    graph: {
                        study: {
                            id_study_tmp: 42,
                            id_lims: "SQSCP",
                            id_study_lims: "6568",
                            name: "RNA Seq",
                            faculty_sponsor: "Dr Example",
                            state: "active",
                            abstract: "",
                            abbreviation: "",
                            accession_number: "ERP123456",
                            description: "",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            hmdmc_number: "",
                            programme: "",
                            created: "2026-04-20T09:00:00Z",
                            reference_genome: "",
                            ethically_approved: false,
                            study_type: "",
                            contains_human_dna: false,
                            contaminated_human_dna: false,
                            study_visibility: "",
                            ega_dac_accession_number: "",
                            ega_policy_accession_number: "",
                            data_release_timing: "",
                        },
                        study_detail: {
                            study: {
                                id_study_tmp: 42,
                                id_lims: "SQSCP",
                                id_study_lims: "6568",
                                name: "RNA Seq",
                                faculty_sponsor: "Dr Example",
                                state: "active",
                                abstract: "",
                                abbreviation: "",
                                accession_number: "ERP123456",
                                description: "",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                hmdmc_number: "",
                                programme: "",
                                created: "2026-04-20T09:00:00Z",
                                reference_genome: "",
                                ethically_approved: false,
                                study_type: "",
                                contains_human_dna: false,
                                contaminated_human_dna: false,
                                study_visibility: "",
                                ega_dac_accession_number: "",
                                ega_policy_accession_number: "",
                                data_release_timing: "",
                            },
                            library_details: [
                                {
                                    library_type: "RNA PolyA",
                                    id_study_lims: "6568",
                                    samples: [
                                        {
                                            id_study_lims: "6568",
                                            id_sample_lims: "SMP001",
                                            sanger_id: "S1",
                                            sample_name: "Sample 1",
                                            taxon_id: 9606,
                                            common_name: "Human",
                                            library_type: "RNA PolyA",
                                            id_run: 100,
                                            lane: 1,
                                            tag_index: 10,
                                            irods_path: "/seq/100",
                                            study_accession_number: "ERP123456",
                                            accession_number: "ERS001",
                                        },
                                    ],
                                },
                                {
                                    library_type: "RNA Ribozero",
                                    id_study_lims: "6568",
                                    samples: [
                                        {
                                            id_study_lims: "6568",
                                            id_sample_lims: "SMP002",
                                            sanger_id: "S2",
                                            sample_name: "Sample 2",
                                            taxon_id: 9606,
                                            common_name: "Human",
                                            library_type: "RNA Ribozero",
                                            id_run: 101,
                                            lane: 1,
                                            tag_index: 11,
                                            irods_path: "/seq/101",
                                            study_accession_number: "ERP123456",
                                            accession_number: "ERS002",
                                        },
                                        {
                                            id_study_lims: "6568",
                                            id_sample_lims: "SMP003",
                                            sanger_id: "S3",
                                            sample_name: "Sample 3",
                                            taxon_id: 9606,
                                            common_name: "Human",
                                            library_type: "RNA Ribozero",
                                            id_run: 101,
                                            lane: 2,
                                            tag_index: 12,
                                            irods_path: "/seq/101",
                                            study_accession_number: "ERP123456",
                                            accession_number: "ERS003",
                                        },
                                    ],
                                },
                                {
                                    library_type: "WGS",
                                    id_study_lims: "6568",
                                    samples: [],
                                },
                            ],
                        },
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should have a Libraries section
        expect(screen.getByText("Libraries")).toBeTruthy();

        // Each library should appear once with its own copy/filter buttons
        expect(screen.getAllByText("RNA PolyA")).toHaveLength(1);
        expect(screen.getAllByText("RNA Ribozero")).toHaveLength(1);
        expect(screen.getAllByText("WGS")).toHaveLength(1);

        // Should not duplicate the "Library type" or "seqmeta_library" labels
        // Each library card should have exactly one "Library type" label
        const libraryTypeLabels = screen.queryAllByText("Library type", {
            exact: true,
        });

        // We have 3 libraries, so we should have 3 "Library type" labels (one per library)
        expect(libraryTypeLabels.length).toBe(3);

        // Each library should have copy and filter buttons
        const libraryButtons = screen.getAllByLabelText(/Copy|Filter/);

        expect(libraryButtons.length).toBeGreaterThan(0);
    });

    it("nests samples under each library, collapsed by default and expandable", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                    graph: {
                        study: {
                            id_study_tmp: 42,
                            id_lims: "SQSCP",
                            id_study_lims: "6568",
                            name: "RNA Seq",
                            faculty_sponsor: "Dr Example",
                            state: "active",
                            abstract: "",
                            abbreviation: "",
                            accession_number: "ERP123456",
                            description: "",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            hmdmc_number: "",
                            programme: "",
                            created: "2026-04-20T09:00:00Z",
                            reference_genome: "",
                            ethically_approved: false,
                            study_type: "",
                            contains_human_dna: false,
                            contaminated_human_dna: false,
                            study_visibility: "",
                            ega_dac_accession_number: "",
                            ega_policy_accession_number: "",
                            data_release_timing: "",
                        },
                        study_detail: {
                            study: {
                                id_study_tmp: 42,
                                id_lims: "SQSCP",
                                id_study_lims: "6568",
                                name: "RNA Seq",
                                faculty_sponsor: "Dr Example",
                                state: "active",
                                abstract: "",
                                abbreviation: "",
                                accession_number: "ERP123456",
                                description: "",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                hmdmc_number: "",
                                programme: "",
                                created: "2026-04-20T09:00:00Z",
                                reference_genome: "",
                                ethically_approved: false,
                                study_type: "",
                                contains_human_dna: false,
                                contaminated_human_dna: false,
                                study_visibility: "",
                                ega_dac_accession_number: "",
                                ega_policy_accession_number: "",
                                data_release_timing: "",
                            },
                            library_details: [
                                {
                                    library_type: "RNA PolyA",
                                    id_study_lims: "6568",
                                    samples: [
                                        {
                                            id_study_lims: "6568",
                                            id_sample_lims: "SMP001",
                                            sanger_id: "S1",
                                            sample_name: "Sample 1",
                                            taxon_id: 9606,
                                            common_name: "Human",
                                            library_type: "RNA PolyA",
                                            id_run: 100,
                                            lane: 1,
                                            tag_index: 10,
                                            irods_path: "/seq/100",
                                            study_accession_number: "ERP123456",
                                            accession_number: "ERS001",
                                        },
                                        {
                                            id_study_lims: "6568",
                                            id_sample_lims: "SMP002",
                                            sanger_id: "S2",
                                            sample_name: "Sample 2",
                                            taxon_id: 9606,
                                            common_name: "Human",
                                            library_type: "RNA PolyA",
                                            id_run: 100,
                                            lane: 2,
                                            tag_index: 11,
                                            irods_path: "/seq/100",
                                            study_accession_number: "ERP123456",
                                            accession_number: "ERS002",
                                        },
                                    ],
                                },
                            ],
                        },
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Samples should be collapsed by default (not visible)
        expect(screen.queryByText("Sample 1")).toBeNull();
        expect(screen.queryByText("Sample 2")).toBeNull();

        // There should be an expand button for the library with "2 samples" text
        const expandButton = screen.getByText("2 samples").closest("button");

        expect(expandButton).toBeTruthy();

        // Click to expand
        fireEvent.click(expandButton!);

        await waitFor(() => {
            expect(screen.getByText("Sample 1 / S1")).toBeTruthy();
        });

        // Both samples should now be visible
        expect(screen.getByText("Sample 2 / S2")).toBeTruthy();

        // Each sample should have its own copy/filter buttons
        const sampleButtons = screen.getAllByLabelText(/Copy|Filter/);

        expect(sampleButtons.length).toBeGreaterThan(2);
    });

    it("lists samples individually for library detail and includes parent study", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_library",
                rawValue: "RNA",
                enrichment: buildEnrichment({
                    identifier: "RNA",
                    type: "library_type",
                    graph: {
                        study: {
                            id_study_tmp: 42,
                            id_lims: "SQSCP",
                            id_study_lims: "6568",
                            name: "RNA Seq",
                            faculty_sponsor: "Dr Example",
                            state: "active",
                            abstract: "",
                            abbreviation: "",
                            accession_number: "ERP123456",
                            description: "",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            hmdmc_number: "",
                            programme: "",
                            created: "2026-04-20T09:00:00Z",
                            reference_genome: "",
                            ethically_approved: false,
                            study_type: "",
                            contains_human_dna: false,
                            contaminated_human_dna: false,
                            study_visibility: "",
                            ega_dac_accession_number: "",
                            ega_policy_accession_number: "",
                            data_release_timing: "",
                        },
                        samples: [
                            {
                                id_study_lims: "6568",
                                id_sample_lims: "SMP001",
                                sanger_id: "S1",
                                sample_name: "Sample 1",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "RNA",
                                id_run: 100,
                                lane: 1,
                                tag_index: 10,
                                irods_path: "/seq/100",
                                study_accession_number: "ERP123456",
                                accession_number: "ERS001",
                            },
                            {
                                id_study_lims: "6568",
                                id_sample_lims: "SMP002",
                                sanger_id: "S2",
                                sample_name: "Sample 2",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "RNA",
                                id_run: 101,
                                lane: 1,
                                tag_index: 11,
                                irods_path: "/seq/101",
                                study_accession_number: "ERP123456",
                                accession_number: "ERS002",
                            },
                        ],
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should have a Samples section
        expect(screen.getByText("Samples")).toBeTruthy();

        // Each sample should be on its own row with display name
        expect(screen.getByText("Sample 1 / S1")).toBeTruthy();
        expect(screen.getByText("Sample 2 / S2")).toBeTruthy();

        // Should have a Study section showing the parent study
        expect(screen.getByText("Study")).toBeTruthy();
        expect(screen.getByText("RNA Seq")).toBeTruthy();
    });

    it("shows all direct metadata fields for a sample, not just sampleid", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "WTSI_wEMB10524782",
                enrichment: buildEnrichment({
                    identifier: "WTSI_wEMB10524782",
                    type: "sanger_sample_id",
                    graph: {
                        study: {
                            id_study_tmp: 6396,
                            id_lims: "SQSCP",
                            id_study_lims: "6568",
                            name: "HCA Embryo Foetal WSSS Dev RNA Sanger",
                            faculty_sponsor: "Omer Bayraktar/Muzz Hanniffa",
                            state: "active",
                            abstract: "Study abstract",
                            abbreviation: "WTSI_wEMB",
                            accession_number: "EGAS00001005445",
                            description: "Detailed single cell atlas",
                            data_release_strategy: "managed",
                            study_title: "HCA Embryo",
                            data_access_group: "team205 cellgeni team283",
                            hmdmc_number: "19/0127",
                            programme: "Cellular Genomics",
                            created: "2021-07-05T10:08:11Z",
                            reference_genome: "GRCh38_15_plus_hs38d1",
                            ethically_approved: true,
                            study_type: "Transcriptome Analysis",
                            contains_human_dna: true,
                            contaminated_human_dna: false,
                            study_visibility: "Hold",
                            ega_dac_accession_number: "",
                            ega_policy_accession_number: "",
                            data_release_timing: "delayed",
                        },
                        sample: {
                            id_study_lims: "6568",
                            id_sample_lims: "6050954",
                            sanger_id: "WTSI_wEMB10524782",
                            sample_name: "C84-WEM-2-FO-1_S2_mA",
                            taxon_id: 9606,
                            common_name: "human",
                            library_type: "Chromium single cell ATAC",
                            id_run: 42834,
                            lane: 4,
                            tag_index: 15,
                            irods_path:
                                "/seq/illumina/runs/42/42834/lane4/plex15/42834_4#15.cram",
                            study_accession_number: "EGAS00001005445",
                            accession_number: "EGAN00003258234",
                        },
                        sample_detail: {
                            sanger_id: "WTSI_wEMB10524782",
                            sample_name: "C84-WEM-2-FO-1_S2_mA",
                            sample: {
                                id_study_lims: "6568",
                                id_sample_lims: "6050954",
                                sanger_id: "WTSI_wEMB10524782",
                                sample_name: "C84-WEM-2-FO-1_S2_mA",
                                taxon_id: 9606,
                                common_name: "human",
                                library_type: "Chromium single cell ATAC",
                                id_run: 42834,
                                lane: 4,
                                tag_index: 15,
                                irods_path:
                                    "/seq/illumina/runs/42/42834/lane4/plex15/42834_4#15.cram",
                                study_accession_number: "EGAS00001005445",
                                accession_number: "EGAN00003258234",
                            },
                            lanes: [
                                { id_run: "42834", lane: "4", tag_index: 15 },
                                { id_run: "42826", lane: "4", tag_index: 14 },
                            ],
                        },
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Direct metadata section should include multiple fields, not just sampleid
        expect(screen.getByText("Sample name")).toBeTruthy();
        expect(screen.getByText("C84-WEM-2-FO-1_S2_mA")).toBeTruthy();
        expect(screen.getByText("Sanger sample ID")).toBeTruthy();
        expect(screen.getAllByText("WTSI_wEMB10524782").length).toBeGreaterThan(
            0,
        );
        expect(screen.getByText("Sample LIMS ID")).toBeTruthy();
        expect(screen.getByText("6050954")).toBeTruthy();
        expect(screen.getByText("Sample accession")).toBeTruthy();
        expect(screen.getByText("EGAN00003258234")).toBeTruthy();
    });

    it("shows hierarchical related data for sample with library parent, study grandparent, and lanes", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "WTSI_wEMB10524782",
                enrichment: buildEnrichment({
                    identifier: "WTSI_wEMB10524782",
                    type: "sanger_sample_id",
                    graph: {
                        study: {
                            id_study_tmp: 6396,
                            id_lims: "SQSCP",
                            id_study_lims: "6568",
                            name: "HCA Embryo Foetal WSSS Dev RNA Sanger",
                            faculty_sponsor: "Omer Bayraktar/Muzz Hanniffa",
                            state: "active",
                            abstract: "Study abstract",
                            abbreviation: "WTSI_wEMB",
                            accession_number: "EGAS00001005445",
                            description: "Detailed single cell atlas",
                            data_release_strategy: "managed",
                            study_title: "HCA Embryo",
                            data_access_group: "team205 cellgeni team283",
                            hmdmc_number: "19/0127",
                            programme: "Cellular Genomics",
                            created: "2021-07-05T10:08:11Z",
                            reference_genome: "GRCh38_15_plus_hs38d1",
                            ethically_approved: true,
                            study_type: "Transcriptome Analysis",
                            contains_human_dna: true,
                            contaminated_human_dna: false,
                            study_visibility: "Hold",
                            ega_dac_accession_number: "",
                            ega_policy_accession_number: "",
                            data_release_timing: "delayed",
                        },
                        sample: {
                            id_study_lims: "6568",
                            id_sample_lims: "6050954",
                            sanger_id: "WTSI_wEMB10524782",
                            sample_name: "C84-WEM-2-FO-1_S2_mA",
                            taxon_id: 9606,
                            common_name: "human",
                            library_type: "Chromium single cell ATAC",
                            id_run: 42834,
                            lane: 4,
                            tag_index: 15,
                            irods_path:
                                "/seq/illumina/runs/42/42834/lane4/plex15/42834_4#15.cram",
                            study_accession_number: "EGAS00001005445",
                            accession_number: "EGAN00003258234",
                        },
                        sample_detail: {
                            sanger_id: "WTSI_wEMB10524782",
                            sample_name: "C84-WEM-2-FO-1_S2_mA",
                            sample: {
                                id_study_lims: "6568",
                                id_sample_lims: "6050954",
                                sanger_id: "WTSI_wEMB10524782",
                                sample_name: "C84-WEM-2-FO-1_S2_mA",
                                taxon_id: 9606,
                                common_name: "human",
                                library_type: "Chromium single cell ATAC",
                                id_run: 42834,
                                lane: 4,
                                tag_index: 15,
                                irods_path:
                                    "/seq/illumina/runs/42/42834/lane4/plex15/42834_4#15.cram",
                                study_accession_number: "EGAS00001005445",
                                accession_number: "EGAN00003258234",
                            },
                            lanes: [
                                { id_run: "42834", lane: "4", tag_index: 15 },
                                { id_run: "42826", lane: "4", tag_index: 14 },
                                { id_run: "42826", lane: "4", tag_index: 15 },
                            ],
                        },
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should have a Library section showing the parent library
        expect(screen.getByText("Library")).toBeTruthy();
        expect(screen.getByText("Chromium single cell ATAC")).toBeTruthy();

        // Should have a Study section showing the study the library belongs to
        expect(screen.getByText("Study")).toBeTruthy();
        expect(
            screen.getByText("HCA Embryo Foetal WSSS Dev RNA Sanger"),
        ).toBeTruthy();

        // Should have a Lanes section
        expect(screen.getByText("Lanes")).toBeTruthy();

        // Should list the 3 lanes
        expect(screen.getByText("42834_4#15")).toBeTruthy();
        expect(screen.getByText("42826_4#14")).toBeTruthy();
        expect(screen.getByText("42826_4#15")).toBeTruthy();
    });

    it("does not show linked samples for a sample (no sample-to-sample relations)", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "WTSI_wEMB10524782",
                enrichment: buildEnrichment({
                    identifier: "WTSI_wEMB10524782",
                    type: "sanger_sample_id",
                    graph: {
                        study: {
                            id_study_tmp: 6396,
                            id_lims: "SQSCP",
                            id_study_lims: "6568",
                            name: "HCA Embryo",
                            faculty_sponsor: "Omer",
                            state: "active",
                            abstract: "Study abstract",
                            abbreviation: "WTSI_wEMB",
                            accession_number: "EGAS00001005445",
                            description: "Detailed single cell atlas",
                            data_release_strategy: "managed",
                            study_title: "HCA Embryo",
                            data_access_group: "team205",
                            hmdmc_number: "19/0127",
                            programme: "Cellular Genomics",
                            created: "2021-07-05T10:08:11Z",
                            reference_genome: "GRCh38_15_plus_hs38d1",
                            ethically_approved: true,
                            study_type: "Transcriptome Analysis",
                            contains_human_dna: true,
                            contaminated_human_dna: false,
                            study_visibility: "Hold",
                            ega_dac_accession_number: "",
                            ega_policy_accession_number: "",
                            data_release_timing: "delayed",
                        },
                        sample: {
                            id_study_lims: "6568",
                            id_sample_lims: "6050954",
                            sanger_id: "WTSI_wEMB10524782",
                            sample_name: "Sample1",
                            taxon_id: 9606,
                            common_name: "human",
                            library_type: "Chromium single cell ATAC",
                            id_run: 42834,
                            lane: 4,
                            tag_index: 15,
                            irods_path:
                                "/seq/illumina/runs/42/42834/lane4/plex15/42834_4#15.cram",
                            study_accession_number: "EGAS00001005445",
                            accession_number: "EGAN00003258234",
                        },
                        samples: [
                            {
                                id_study_lims: "6568",
                                id_sample_lims: "6050954",
                                sanger_id: "WTSI_wEMB10524782",
                                sample_name: "Sample1",
                            },
                            {
                                id_study_lims: "6568",
                                id_sample_lims: "6050955",
                                sanger_id: "WTSI_wEMB10524783",
                                sample_name: "Sample2",
                            },
                            {
                                id_study_lims: "6568",
                                id_sample_lims: "6050956",
                                sanger_id: "WTSI_wEMB10524784",
                                sample_name: "Sample3",
                            },
                        ],
                        sample_detail: {
                            sanger_id: "WTSI_wEMB10524782",
                            sample_name: "Sample1",
                            sample: {
                                id_study_lims: "6568",
                                id_sample_lims: "6050954",
                                sanger_id: "WTSI_wEMB10524782",
                                sample_name: "Sample1",
                                taxon_id: 9606,
                                common_name: "human",
                                library_type: "Chromium single cell ATAC",
                                id_run: 42834,
                                lane: 4,
                                tag_index: 15,
                                irods_path:
                                    "/seq/illumina/runs/42/42834/lane4/plex15/42834_4#15.cram",
                                study_accession_number: "EGAS00001005445",
                                accession_number: "EGAN00003258234",
                            },
                            lanes: [
                                { id_run: "42834", lane: "4", tag_index: 15 },
                            ],
                        },
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should NOT have "Linked samples" section or multiple sample rows
        expect(screen.queryByText("Linked samples")).toBeNull();
        expect(screen.queryByText("Sample2")).toBeNull();
        expect(screen.queryByText("Sample3")).toBeNull();
        expect(screen.queryByText("WTSI_wEMB10524783")).toBeNull();
        expect(screen.queryByText("WTSI_wEMB10524784")).toBeNull();

        // Should have Library and Study sections (hierarchical parent/grandparent)
        expect(screen.getByText("Library")).toBeTruthy();
        expect(screen.getByText("Study")).toBeTruthy();
    });
});
