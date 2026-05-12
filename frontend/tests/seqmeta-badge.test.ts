/**
 * @vitest-environment jsdom
 */

import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

import { act, createElement } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToStaticMarkup, renderToString } from "react-dom/server";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
    EnrichmentResult,
    EnrichmentSample,
    EnrichmentStudy,
    FileEntry,
    IRODSPath,
    LaneDetail,
    ResultSet,
    SampleDetail,
} from "@/lib/contracts";
import { enrichmentResultSchema } from "@/lib/contracts";
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
const fetchLibrarySamplesMock = vi.fn();
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

vi.mock("@/lib/seqmeta-enrichment", async (importOriginal) => {
    const actual =
        await importOriginal<typeof import("@/lib/seqmeta-enrichment")>();
    return {
        ...actual,
        fetchLibrarySamples: fetchLibrarySamplesMock,
    };
});

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

function buildStudy(overrides: Partial<EnrichmentStudy> = {}): EnrichmentStudy {
    return {
        id_study_tmp: 42,
        id_lims: "SQSCP",
        id_study_lims: "6568",
        name: "RNA Seq",
        faculty_sponsor: "Dr Example",
        state: "active",
        accession_number: "ERP123456",
        data_release_strategy: "managed",
        study_title: "RNA Study",
        data_access_group: "group-a",
        programme: "Transcriptomics",
        reference_genome: "GRCh38",
        ethically_approved: true,
        study_type: "Whole Genome Sequencing",
        contains_human_dna: true,
        contaminated_human_dna: false,
        study_visibility: "Always Open",
        ega_dac_accession_number: "EGAC00001",
        ega_policy_accession_number: "EGAP00001",
        data_release_timing: "Immediate",
        ...overrides,
    };
}

function buildSample(
    overrides: Partial<EnrichmentSample> = {},
): EnrichmentSample {
    return {
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
        ...overrides,
    };
}

function buildLaneDetail(overrides: Partial<LaneDetail> = {}): LaneDetail {
    return {
        id_run: "1234",
        lane: "1",
        tag_index: 7,
        ...overrides,
    };
}

function buildIRODSPath(overrides: Partial<IRODSPath> = {}): IRODSPath {
    return {
        id_product: "1234_1#7",
        collection: "/seq",
        data_object: "1234_1#7.cram",
        irods_path: "/seq/1234_1#7.cram",
        ...overrides,
    };
}

function buildSampleDetail(
    overrides: {
        sanger_id?: string;
        sample_name?: string;
        sample?: Partial<EnrichmentSample>;
        lanes?: Array<Partial<LaneDetail>>;
        irods_paths?: Array<Partial<IRODSPath>>;
    } = {},
): SampleDetail {
    const sample = buildSample(overrides.sample);

    return {
        sanger_id: overrides.sanger_id ?? sample.sanger_id,
        sample_name: overrides.sample_name ?? sample.sample_name,
        sample,
        lanes: (overrides.lanes ?? []).map((lane) => buildLaneDetail(lane)),
        ...(overrides.irods_paths
            ? {
                  irods_paths: overrides.irods_paths.map((path) =>
                      buildIRODSPath(path),
                  ),
              }
            : {}),
    };
}

function buildEnrichment(
    overrides: Partial<EnrichmentResult> = {},
): EnrichmentResult {
    return {
        identifier: "SANG001",
        type: "sanger_sample_id",
        graph: {
            study: buildStudy(),
            sample: buildSample(),
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
        ).toBe("/?study=6568");
    });

    it("does not render project rows even when legacy project fields are present", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");
        const legacyEnrichment = enrichmentResultSchema.parse({
            identifier: "SANG001",
            type: "sanger_sample_id",
            graph: {
                study: buildStudy(),
                sample: {
                    id_study_lims: "6568",
                    id_sample_lims: "LIMS001",
                    sanger_id: "SANG001",
                    name: "Sample 1",
                    taxon_id: 9606,
                    common_name: "Human",
                    library_type: "RNA",
                    accession_number: "ERS123456",
                },
                libraries: [
                    {
                        library_type: "RNA",
                        id_study_lims: "6568",
                    },
                ],
                project: {
                    id: 101,
                    name: "Project RNA",
                },
                users: [
                    {
                        id: 202,
                        username: "alice",
                    },
                ],
            },
            partial: false,
        });

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: legacyEnrichment,
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.queryByText("Project")).toBeNull();
        expect(screen.queryByText("Project users")).toBeNull();
        expect(screen.queryByText("Project RNA")).toBeNull();
        expect(screen.queryByText("alice")).toBeNull();
    });

    it("contains no removed upstream wording in the badge source", async () => {
        const source = await readFile(
            resolve(process.cwd(), "components/seqmeta-badge.tsx"),
            "utf8",
        );

        expect(source).not.toContain("Sa" + "ga");
        expect(source).not.toContain("via " + "Sa" + "ga");
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
                        // Backend returns studies (plural array) for library_type enrichment
                        study: undefined, // Remove singular study
                        studies: [buildStudy()], // Use plural studies
                        samples: [
                            buildSample({
                                sample_name: "Sample 1",
                                sanger_id: "SANG001",
                            }),
                            buildSample({
                                sample_name: "Sample 2",
                                sanger_id: "SANG002",
                            }),
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
                name: /send study to search filter/i,
            }),
        ).toBeTruthy();

        // Individual sample filter links should be present
        expect(
            screen.getAllByRole("link", {
                name: /send sample to search filter/i,
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
                        samples: Array.from({ length: 24 }, (_, index) =>
                            buildSample({
                                sample_name: `Sample ${index + 1}`,
                                sanger_id: `SANG${String(index + 1).padStart(3, "0")}`,
                            }),
                        ),
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

    it("falls back to document copy for seqmeta details when Clipboard API is unavailable", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");
        const originalNavigatorDescriptor = Object.getOwnPropertyDescriptor(
            globalThis,
            "navigator",
        );
        const execCommandMock = vi.fn().mockReturnValue(true);

        Object.defineProperty(globalThis, "navigator", {
            configurable: true,
            value: {},
        });
        Object.defineProperty(document, "execCommand", {
            configurable: true,
            value: execCommandMock,
        });

        try {
            render(
                createElement(SeqmetaBadge, {
                    metadataKey: "seqmeta_sampleid",
                    rawValue: "SANG001",
                    enrichment: buildEnrichment(),
                }),
            );

            fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            fireEvent.click(
                screen.getAllByLabelText(/Copy seqmeta_sampleid/i)[0]!,
            );

            await waitFor(() => {
                expect(execCommandMock).toHaveBeenCalledWith("copy");
            });

            expect(screen.getByText("Copied")).toBeTruthy();
        } finally {
            if (originalNavigatorDescriptor) {
                Object.defineProperty(
                    globalThis,
                    "navigator",
                    originalNavigatorDescriptor,
                );
            }
        }
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

    it("hydrates without mismatches when the client cookie has a stale unavailable marker", async () => {
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        enrichIdentifierMock.mockReset();
        enrichIdentifierMock.mockResolvedValue(null);
        const serverTree = createElement(
            SeqmetaCacheContext.Provider,
            { value: new SeqmetaCache() },
            createElement(ResultMetadataEnrichment, {
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
            }),
        );
        const clientTree = createElement(
            SeqmetaCacheProvider,
            null,
            createElement(ResultMetadataEnrichment, {
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
            }),
        );
        const container = document.createElement("div");
        const recoverableErrors: unknown[] = [];

        document.cookie = buildSeqmetaCacheCookie({ SANG001: null });
        container.innerHTML = renderToString(serverTree);
        document.body.appendChild(container);

        let root: ReturnType<typeof hydrateRoot> | null = null;

        try {
            await act(async () => {
                root = hydrateRoot(container, clientTree, {
                    onRecoverableError: (error) => {
                        recoverableErrors.push(error);
                    },
                });
            });

            await waitFor(() => {
                expect(
                    container.querySelector(
                        '[aria-label="enrichment unavailable"]',
                    ),
                ).not.toBeNull();
            });

            expect(recoverableErrors).toHaveLength(0);
        } finally {
            await act(async () => {
                root?.unmount();
            });
            container.remove();
        }
    });

    it("hydrates without mismatches when both server and client use SeqmetaCacheProvider with a stale not_found cookie", async () => {
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        enrichIdentifierMock.mockReset();
        enrichIdentifierMock.mockResolvedValue(null);

        // Pre-populate document.cookie with a stale "not_found" entry, as
        // would happen on a returning user whose previous visit cached a
        // negative lookup for SANG001.
        document.cookie = buildSeqmetaCacheCookie({ SANG001: null });

        // Both the server-rendered tree and the client hydrated tree go
        // through the real SeqmetaCacheProvider, mirroring the production
        // (results)/layout.tsx wiring.
        const tree = () =>
            createElement(
                SeqmetaCacheProvider,
                null,
                createElement(ResultMetadataEnrichment, {
                    metadata: {
                        seqmeta_sampleid: "SANG001",
                    },
                }),
            );
        const serverMarkup = renderToString(tree());
        const container = document.createElement("div");
        const recoverableErrors: unknown[] = [];

        container.innerHTML = serverMarkup;
        document.body.appendChild(container);

        let root: ReturnType<typeof hydrateRoot> | null = null;

        try {
            await act(async () => {
                root = hydrateRoot(container, tree(), {
                    onRecoverableError: (error) => {
                        recoverableErrors.push(error);
                    },
                });
            });

            // No hydration mismatch errors should have been reported. In
            // particular the regressed bug surfaced as a mismatch between
            // server "loading enrichment" and client "enrichment unavailable".
            const hydrationMismatchErrors = recoverableErrors.filter(
                (error) =>
                    error instanceof Error && /hydrat/i.test(error.message),
            );
            expect(hydrationMismatchErrors).toEqual([]);
            expect(recoverableErrors).toEqual([]);
        } finally {
            await act(async () => {
                root?.unmount();
            });
            container.remove();
        }
    });

    it("renders the same indicator on the server and on the client's first hydration render even when the cookie cache is populated", async () => {
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        enrichIdentifierMock.mockReset();
        enrichIdentifierMock.mockResolvedValue(null);

        document.cookie = buildSeqmetaCacheCookie({ SANG001: null });

        const tree = () =>
            createElement(
                SeqmetaCacheProvider,
                null,
                createElement(ResultMetadataEnrichment, {
                    metadata: {
                        seqmeta_sampleid: "SANG001",
                    },
                }),
            );

        const serverMarkup = renderToString(tree());
        const container = document.createElement("div");

        container.innerHTML = serverMarkup;
        document.body.appendChild(container);

        // The aria-label that appears in the server-rendered HTML must be one
        // of the deterministic SSR-safe values: either "loading enrichment"
        // (initial state) or none at all. Crucially it must NOT be
        // "enrichment unavailable", because that requires reading the cookie
        // cache which is only available after hydration.
        expect(serverMarkup).not.toContain(
            'aria-label="enrichment unavailable"',
        );

        // Hydrating with the same tree should not produce any recoverable
        // hydration errors. The previous bug surfaced as
        //   server: aria-label="loading enrichment"
        //   client: aria-label="enrichment unavailable"
        const recoverableErrors: unknown[] = [];
        let root: ReturnType<typeof hydrateRoot> | null = null;

        try {
            await act(async () => {
                root = hydrateRoot(container, tree(), {
                    onRecoverableError: (error) => {
                        recoverableErrors.push(error);
                    },
                });
            });

            expect(recoverableErrors).toEqual([]);
        } finally {
            await act(async () => {
                root?.unmount();
            });
            container.remove();
        }
    });

    it("renders 'loading enrichment' on the server even when the SeqmetaCacheContext is fed a pre-populated cache (matches the client's first hydration render)", async () => {
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        enrichIdentifierMock.mockReset();
        enrichIdentifierMock.mockResolvedValue(null);

        // Simulate the worst case: a SeqmetaCache that already contains a
        // cached "not_found" result is provided via SeqmetaCacheContext for
        // the SSR pass. This mirrors what would happen if any code path on
        // the server (or a returning client whose cookies were merged into
        // the live cache before hydration completed) hands a populated cache
        // into the consumer. The component must still render the loading
        // state on the very first render so that SSR and hydration agree.
        const populatedCache = new SeqmetaCache({ SANG001: null });

        const serverTree = createElement(
            SeqmetaCacheContext.Provider,
            { value: populatedCache },
            createElement(ResultMetadataEnrichment, {
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
            }),
        );

        const serverMarkup = renderToString(serverTree);

        expect(serverMarkup).toContain('aria-label="loading enrichment"');
        expect(serverMarkup).not.toContain(
            'aria-label="enrichment unavailable"',
        );
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
                            accession_number: "ERP123456",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            programme: "",
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
                                accession_number: "ERP123456",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                programme: "",
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
        // Rows in the Libraries section should NOT have "Library type" labels
        const libraryTypeLabels = screen.queryAllByText("Library type", {
            exact: true,
        });

        // With the fix, library rows within the Libraries section should NOT have labels
        expect(libraryTypeLabels.length).toBe(0);

        // Each library should have copy and filter buttons
        const libraryButtons = screen.getAllByLabelText(/Copy|Filter/);

        expect(libraryButtons.length).toBeGreaterThan(0);
    });

    it("nests samples under each library, collapsed by default and expandable", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        const librarySamples = [
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
        ];

        // Mock JIT loading of library samples
        fetchLibrarySamplesMock.mockResolvedValue(librarySamples);

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
                            accession_number: "ERP123456",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            programme: "",
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
                                accession_number: "ERP123456",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                programme: "",
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
                                    samples: [], // Samples loaded JIT when library is expanded
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

        // There should be an expand button for the library with "Samples" text
        const expandButton = screen.getByText("Samples").closest("button");

        expect(expandButton).toBeTruthy();

        // Click to expand - this triggers JIT loading
        fireEvent.click(expandButton!);

        // Wait for JIT loading to complete and samples to appear
        await waitFor(() => {
            expect(screen.getByText("Sample 1 / S1")).toBeTruthy();
        });

        // Verify fetchLibrarySamples was called with correct arguments
        expect(fetchLibrarySamplesMock).toHaveBeenCalledWith(
            "6568",
            "RNA PolyA",
        );

        // Both samples should now be visible
        expect(screen.getByText("Sample 2 / S2")).toBeTruthy();

        // Button should now show count
        expect(screen.getByText("2 samples")).toBeTruthy();

        // Each sample should have its own copy/filter buttons
        const sampleButtons = screen.getAllByLabelText(/Copy|Filter/);

        expect(sampleButtons.length).toBeGreaterThan(2);
    });

    it("does not emit duplicate-key warnings when expanded library samples share sample IDs", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        const consoleErrorSpy = vi
            .spyOn(console, "error")
            .mockImplementation(() => undefined);

        const duplicateIdentitySamples = [
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
                id_sample_lims: "SMP001",
                sanger_id: "S1",
                sample_name: "Sample 1",
                taxon_id: 9606,
                common_name: "Human",
                library_type: "RNA PolyA",
                id_run: 100,
                lane: 2,
                tag_index: 11,
                irods_path: "/seq/100",
                study_accession_number: "ERP123456",
                accession_number: "ERS001",
            },
        ];

        fetchLibrarySamplesMock.mockResolvedValue(duplicateIdentitySamples);

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
                            accession_number: "ERP123456",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            programme: "",
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
                                accession_number: "ERP123456",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                programme: "",
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

        fireEvent.click(screen.getByLabelText("Show samples"));

        await waitFor(() => {
            expect(screen.getAllByText("Sample 1 / S1")).toHaveLength(2);
        });

        const duplicateKeyErrors = consoleErrorSpy.mock.calls.filter((call) =>
            call.some(
                (arg) =>
                    typeof arg === "string" &&
                    arg.includes("Encountered two children with the same key"),
            ),
        );

        expect(duplicateKeyErrors).toHaveLength(0);
    });

    it("falls back to graph.libraries for Libraries section when study_detail is absent", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        const librarySamples = [
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
        ];

        fetchLibrarySamplesMock.mockResolvedValue(librarySamples);

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
                            accession_number: "ERP123456",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            programme: "",
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
                        // No study_detail — simulates stale server-side cache
                        libraries: [
                            { library_type: "RNA", id_study_lims: "6568" },
                        ],
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should show Libraries sub-section, not the flat view with duplicate labels
        expect(screen.getByText("Libraries")).toBeTruthy();

        // Library type / seqmeta_library labels must NOT be duplicated per row
        expect(
            screen.queryAllByText("Library type", { exact: true }).length,
        ).toBe(0);

        // Should have a Show samples button
        const showSamplesButton = screen.getByLabelText("Show samples");
        expect(showSamplesButton).toBeTruthy();

        // Clicking triggers JIT fetch
        fireEvent.click(showSamplesButton);

        await waitFor(() => {
            expect(screen.getByText("Sample 1 / S1")).toBeTruthy();
        });

        expect(fetchLibrarySamplesMock).toHaveBeenCalledWith("6568", "RNA");
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
                        // Backend returns studies (plural array) for library_type enrichment
                        studies: [
                            {
                                id_study_tmp: 42,
                                id_lims: "SQSCP",
                                id_study_lims: "6568",
                                name: "RNA Seq",
                                faculty_sponsor: "Dr Example",
                                state: "active",
                                accession_number: "ERP123456",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                programme: "",
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
                        ],
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
                            accession_number: "EGAS00001005445",
                            data_release_strategy: "managed",
                            study_title: "HCA Embryo",
                            data_access_group: "team205 cellgeni team283",
                            programme: "Cellular Genomics",
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

        // The Sanger sample ID "WTSI_wEMB10524782" should NOT appear in the
        // direct metadata section because it matches the dialog title (rawValue)
        // and would be redundant
        const dialogTitle = screen
            .getByText("Seqmeta details")
            .parentElement?.querySelector("h3");
        expect(dialogTitle?.textContent).toBe("WTSI_wEMB10524782");

        // But it should show other sample fields that don't duplicate the title
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
                            accession_number: "EGAS00001005445",
                            data_release_strategy: "managed",
                            study_title: "HCA Embryo",
                            data_access_group: "team205 cellgeni team283",
                            programme: "Cellular Genomics",
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
                            accession_number: "EGAS00001005445",
                            data_release_strategy: "managed",
                            study_title: "HCA Embryo",
                            data_access_group: "team205",
                            programme: "Cellular Genomics",
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
                            buildSample({
                                id_sample_lims: "6050954",
                                sanger_id: "WTSI_wEMB10524782",
                                sample_name: "Sample1",
                                common_name: "human",
                                library_type: "Chromium single cell ATAC",
                                id_run: 42834,
                                lane: 4,
                                tag_index: 15,
                                irods_path:
                                    "/seq/illumina/runs/42/42834/lane4/plex15/42834_4#15.cram",
                                study_accession_number: "EGAS00001005445",
                                accession_number: "EGAN00003258234",
                            }),
                            buildSample({
                                id_sample_lims: "6050955",
                                sanger_id: "WTSI_wEMB10524783",
                                sample_name: "Sample2",
                                common_name: "human",
                                library_type: "Chromium single cell ATAC",
                                id_run: 42834,
                                lane: 4,
                                tag_index: 15,
                                irods_path:
                                    "/seq/illumina/runs/42/42834/lane4/plex15/42834_4#15.cram",
                                study_accession_number: "EGAS00001005445",
                                accession_number: "EGAN00003258235",
                            }),
                            buildSample({
                                id_sample_lims: "6050956",
                                sanger_id: "WTSI_wEMB10524784",
                                sample_name: "Sample3",
                                common_name: "human",
                                library_type: "Chromium single cell ATAC",
                                id_run: 42834,
                                lane: 4,
                                tag_index: 15,
                                irods_path:
                                    "/seq/illumina/runs/42/42834/lane4/plex15/42834_4#15.cram",
                                study_accession_number: "EGAS00001005445",
                                accession_number: "EGAN00003258236",
                            }),
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

    it("omits redundant direct metadata row when value duplicates the dialog title", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        // Click on a study ID field - rawValue is the study ID
        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                    graph: {
                        study: {
                            id_study_tmp: 6396,
                            id_lims: "SQSCP",
                            id_study_lims: "6568",
                            name: "HCA Embryo Foetal WSSS Dev RNA Sanger",
                            faculty_sponsor: "Omer Bayraktar/Muzz Hanniffa",
                            state: "active",
                            accession_number: "EGAS00001005445",
                            data_release_strategy: "managed",
                            study_title: "HCA Embryo",
                            data_access_group: "team205 cellgeni team283",
                            programme: "Cellular Genomics",
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
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const dialogTitle = screen
            .getByText("Seqmeta details")
            .parentElement?.querySelector("h3");
        expect(dialogTitle?.textContent).toBe("6568");

        // The direct metadata section should exist and show study name and accession
        const directMetadataSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="direct-metadata"]');
        expect(directMetadataSection).toBeTruthy();

        expect(screen.getByText("Study name")).toBeTruthy();
        expect(
            screen.getByText("HCA Embryo Foetal WSSS Dev RNA Sanger"),
        ).toBeTruthy();
        expect(screen.getByText("Study accession")).toBeTruthy();
        expect(screen.getByText("EGAS00001005445")).toBeTruthy();

        // But it should NOT have a redundant "Study identifier" row with value "6568"
        // because that value is already shown in the dialog title
        const studyIdLabel = screen.queryByText("Study identifier");
        if (studyIdLabel) {
            // If the label exists, verify it's NOT in the direct metadata section
            // (it might be elsewhere, but not in direct metadata)
            const fieldCard = studyIdLabel.closest(
                '[data-seqmeta-detail-key="study_id"]',
            );
            if (fieldCard) {
                expect(directMetadataSection?.contains(fieldCard)).toBe(false);
            }
        }

        // Alternative check: ensure the value "6568" only appears once in direct metadata
        // (it should not appear as a field value since it's in the title)
        if (directMetadataSection) {
            const directMetadataText = directMetadataSection.textContent || "";
            const titleValueCount = (
                directMetadataText.match(/\b6568\b/g) || []
            ).length;
            expect(titleValueCount).toBe(0);
        }
    });

    it("does not duplicate 'Lane' label in rows within Lanes section", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: buildEnrichment({
                    graph: {
                        ...buildEnrichment().graph,
                        sample_detail: buildSampleDetail({
                            lanes: [
                                { id_run: "12345", lane: "1", tag_index: 1 },
                                { id_run: "12345", lane: "2", tag_index: 1 },
                            ],
                        }),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Section heading "Lanes" should exist
        const lanesSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="lanes"]');
        expect(lanesSection).toBeTruthy();
        expect(screen.getByText("Lanes")).toBeTruthy();

        // Individual lane rows should NOT have "Lane" label
        const laneRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="lane"]');
        expect(laneRows.length).toBe(2);

        laneRows.forEach((row) => {
            // Each row should NOT contain the redundant "Lane" label
            const labels = Array.from(
                row.querySelectorAll(
                    ".text-xs.font-semibold.uppercase.tracking-\\[0\\.22em\\].text-muted-foreground",
                ),
            ).map((el) => el.textContent);
            expect(labels).not.toContain("Lane");
        });

        // But values and buttons should still be present
        expect(screen.getByText("12345_1#1")).toBeTruthy();
        expect(screen.getByText("12345_2#1")).toBeTruthy();
        expect(screen.getAllByLabelText(/Copy lane ID/i).length).toBe(2);
    });

    it("does not duplicate 'Sample' label in rows within Samples section", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_library",
                rawValue: "RNA",
                enrichment: buildEnrichment({
                    identifier: "RNA",
                    type: "library_type",
                    graph: {
                        samples: [
                            buildSample({
                                sample_name: "Sample 1",
                                sanger_id: "SANG001",
                                id_sample_lims: "LIMS001",
                            }),
                            buildSample({
                                sample_name: "Sample 2",
                                sanger_id: "SANG002",
                                id_sample_lims: "LIMS002",
                            }),
                        ],
                        // Backend returns studies (plural array) for library_type enrichment
                        studies: [buildStudy()],
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Section heading "Samples" should exist
        const samplesSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="samples"]');
        expect(samplesSection).toBeTruthy();
        expect(screen.getByText("Samples")).toBeTruthy();

        // Individual sample rows should NOT have "Sample" label
        const sampleRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="sample"]');
        expect(sampleRows.length).toBe(2);

        sampleRows.forEach((row) => {
            // Each row should NOT contain the redundant "Sample" label
            const labels = Array.from(
                row.querySelectorAll(
                    ".text-xs.font-semibold.uppercase.tracking-\\[0\\.22em\\].text-muted-foreground",
                ),
            ).map((el) => el.textContent);
            expect(labels).not.toContain("Sample");
        });

        // But values and buttons should still be present
        expect(screen.getByText("Sample 1 / SANG001")).toBeTruthy();
        expect(screen.getByText("Sample 2 / SANG002")).toBeTruthy();
    });

    it("does not duplicate 'Library type' label in rows within Library section", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: buildEnrichment({
                    graph: {
                        ...buildEnrichment().graph,
                        sample_detail: buildSampleDetail({ lanes: [] }),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Section heading "Library" should exist
        const librarySection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="library"]');
        expect(librarySection).toBeTruthy();
        expect(screen.getByText("Library")).toBeTruthy();

        // Individual library rows should NOT have "Library type" label
        const libraryRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="library"]');
        expect(libraryRows.length).toBe(1);

        libraryRows.forEach((row) => {
            // Each row should NOT contain the redundant "Library type" label
            const labels = Array.from(
                row.querySelectorAll(
                    ".text-xs.font-semibold.uppercase.tracking-\\[0\\.22em\\].text-muted-foreground",
                ),
            ).map((el) => el.textContent);
            expect(labels).not.toContain("Library type");
        });

        // But values and buttons should still be present
        expect(screen.getByText("RNA")).toBeTruthy();
    });

    it("does not duplicate 'Study name' label in rows within Study section", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_library",
                rawValue: "RNA",
                enrichment: buildEnrichment({
                    identifier: "RNA",
                    type: "library_type",
                    graph: {
                        // Backend returns studies (plural array) for library_type enrichment
                        studies: [
                            buildStudy({
                                name: "HCA Study",
                                id_study_lims: "6568",
                            }),
                        ],
                        samples: [],
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Section heading "Study" should exist
        const studySection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="study"]');
        expect(studySection).toBeTruthy();
        expect(screen.getByText("Study")).toBeTruthy();

        // Individual study rows should NOT have "Study name" label
        const studyRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="study"]');
        expect(studyRows.length).toBe(1);

        studyRows.forEach((row) => {
            // Each row should NOT contain the redundant "Study name" label
            const labels = Array.from(
                row.querySelectorAll(
                    ".text-xs.font-semibold.uppercase.tracking-\\[0\\.22em\\].text-muted-foreground",
                ),
            ).map((el) => el.textContent);
            expect(labels).not.toContain("Study name");
        });

        // But values and buttons should still be present
        expect(screen.getByText("HCA Study")).toBeTruthy();
    });

    it("does not duplicate 'Library type' label in Libraries section with nested samples", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        // Mock JIT loading of library samples
        const librarySamples = [
            {
                sample_name: "Sample 1",
                sanger_id: "SANG001",
                id_sample_lims: "LIMS001",
            },
        ];

        fetchLibrarySamplesMock.mockResolvedValue(librarySamples);

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                    graph: {
                        study: buildStudy(),
                        study_detail: {
                            study: buildStudy(),
                            library_details: [
                                {
                                    library_type: "RNA",
                                    id_study_lims: "6568",
                                    samples: [], // Samples loaded JIT when library is expanded
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

        // Section heading "Libraries" should exist
        const librariesSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="libraries"]');
        expect(librariesSection).toBeTruthy();
        expect(screen.getByText("Libraries")).toBeTruthy();

        // Library rows should NOT have "Library type" label
        const libraryRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="seqmeta_library"]');
        expect(libraryRows.length).toBe(1);

        libraryRows.forEach((row) => {
            const labels = Array.from(
                row.querySelectorAll(
                    ".text-xs.font-semibold.uppercase.tracking-\\[0\\.22em\\].text-muted-foreground",
                ),
            ).map((el) => el.textContent);
            expect(labels).not.toContain("Library type");
        });

        // Nested sample rows should also NOT have "Sample" label when expanded
        fireEvent.click(screen.getByLabelText("Show samples"));

        await waitFor(() => {
            const nestedSampleRows = screen
                .getByTestId("seqmeta-dialog-body")
                .querySelectorAll('[data-seqmeta-detail-key="sample"]');
            expect(nestedSampleRows.length).toBeGreaterThan(0);

            nestedSampleRows.forEach((row) => {
                const labels = Array.from(
                    row.querySelectorAll(
                        ".text-xs.font-semibold.uppercase.tracking-\\[0\\.22em\\].text-muted-foreground",
                    ),
                ).map((el) => el.textContent);
                expect(labels).not.toContain("Sample");
            });
        });
    });

    it("does not emit duplicate React key warnings when rendering library metadata with overlapping sample data", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        // Capture console.error calls to detect React key warnings
        const originalError = console.error;
        const errors: string[] = [];
        console.error = (...args: unknown[]) => {
            const message = String(args[0]);
            errors.push(message);
            originalError(...args);
        };

        try {
            render(
                createElement(SeqmetaBadge, {
                    metadataKey: "seqmeta_library",
                    rawValue: "RNA",
                    enrichment: buildEnrichment({
                        graph: {
                            ...buildEnrichment().graph,
                            // Backend returns studies (plural array) for library_type enrichment
                            study: undefined, // Remove singular study
                            studies: [buildStudy()], // Use plural studies
                            // graph.sample has sanger_id: "SANG001" from buildEnrichment()
                            // Override samples array to include same ID
                            samples: [
                                {
                                    sample_name: "Sample 1",
                                    sanger_id: "SANG001",
                                    id_sample_lims: "LIMS001",
                                    id_study_lims: "6568",
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
                                {
                                    sample_name: "Sample 2",
                                    sanger_id: "SANG002",
                                    id_sample_lims: "LIMS002",
                                    id_study_lims: "6568",
                                    taxon_id: 9606,
                                    common_name: "Human",
                                    library_type: "RNA",
                                    id_run: 1235,
                                    lane: 2,
                                    tag_index: 8,
                                    irods_path: "/seq/1235",
                                    study_accession_number: "ERP123456",
                                    accession_number: "ERS123457",
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

            // Should have Samples section
            expect(screen.getByText("Samples")).toBeTruthy();

            // No duplicate key warnings
            const duplicateKeyWarnings = errors.filter((msg) =>
                msg.includes("Encountered two children with the same key"),
            );
            expect(duplicateKeyWarnings).toEqual([]);
        } finally {
            console.error = originalError;
        }
    });

    it("displays study section for library metadata when study data is present", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_library",
                rawValue: "Chromium single cell",
                enrichment: {
                    identifier: "Chromium single cell",
                    type: "library_type",
                    graph: {
                        // Backend returns studies (plural array) for library_type enrichment
                        studies: [
                            {
                                id_study_tmp: 99,
                                id_lims: "SQSCP",
                                id_study_lims: "7777",
                                name: "Pilot study of dissociation methods for human gut tissues",
                                faculty_sponsor: "Dr Smith",
                                state: "active",
                                accession_number: "ERP999999",
                                data_release_strategy: "managed",
                                study_title: "Gut Dissociation Pilot",
                                data_access_group: "gut-team",
                                programme: "Tissue Methods",
                                reference_genome: "GRCh38",
                                ethically_approved: true,
                                study_type: "Single Cell RNA Sequencing",
                                contains_human_dna: true,
                                contaminated_human_dna: false,
                                study_visibility: "Always Open",
                                ega_dac_accession_number: "",
                                ega_policy_accession_number: "",
                                data_release_timing: "Standard",
                            },
                        ],
                        samples: [
                            {
                                id_study_lims: "7777",
                                id_sample_lims: "SC001",
                                sanger_id: "SANG_SC_001",
                                sample_name: "Gut tissue A",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "Chromium single cell",
                                id_run: 9001,
                                lane: 1,
                                tag_index: 1,
                                irods_path: "/seq/9001",
                                study_accession_number: "ERP999999",
                                accession_number: "ERS999001",
                            },
                            {
                                id_study_lims: "7777",
                                id_sample_lims: "SC002",
                                sanger_id: "SANG_SC_002",
                                sample_name: "Gut tissue B",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "Chromium single cell",
                                id_run: 9001,
                                lane: 1,
                                tag_index: 2,
                                irods_path: "/seq/9001",
                                study_accession_number: "ERP999999",
                                accession_number: "ERS999002",
                            },
                        ],
                    },
                    partial: false,
                },
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Study section should be present
        const studyHeading = screen.getByText("Study");
        expect(studyHeading).toBeTruthy();

        // Study name should be displayed
        expect(
            screen.getByText(
                "Pilot study of dissociation methods for human gut tissues",
            ),
        ).toBeTruthy();

        // Study filter link should be available
        expect(
            screen.getByRole("link", {
                name: /send study to search filter/i,
            }),
        ).toHaveProperty("href", expect.stringContaining("study=7777"));

        // Samples section should also be present
        expect(screen.getByText("Samples")).toBeTruthy();

        // Both samples should be listed with no duplicate key warnings
        expect(screen.getByText("Gut tissue A / SANG_SC_001")).toBeTruthy();
        expect(screen.getByText("Gut tissue B / SANG_SC_002")).toBeTruthy();
    });

    it("marks only the clicked library row copy button as Copied", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");
        const originalNavigatorDescriptor = Object.getOwnPropertyDescriptor(
            globalThis,
            "navigator",
        );
        const writeTextMock = vi.fn().mockResolvedValue(undefined);

        Object.defineProperty(globalThis, "navigator", {
            configurable: true,
            value: {
                clipboard: {
                    writeText: writeTextMock,
                },
            },
        });

        try {
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
                                accession_number: "ERP123456",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                programme: "",
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
                                    accession_number: "ERP123456",
                                    data_release_strategy: "",
                                    study_title: "",
                                    data_access_group: "",
                                    programme: "",
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
                                        samples: [],
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

            const copyButtons =
                screen.getAllByLabelText(/Copy seqmeta_library/i);
            expect(copyButtons).toHaveLength(2);

            fireEvent.click(copyButtons[0]!);

            await waitFor(() => {
                expect(writeTextMock).toHaveBeenCalledWith("RNA PolyA");
                expect(copyButtons[0]!.textContent).toContain("Copied");
            });
            expect(copyButtons[1]!.textContent).toContain("Copy");
        } finally {
            if (originalNavigatorDescriptor) {
                Object.defineProperty(
                    globalThis,
                    "navigator",
                    originalNavigatorDescriptor,
                );
            }
        }
    });

    it("marks only the clicked sample row copy button as Copied", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");
        const originalNavigatorDescriptor = Object.getOwnPropertyDescriptor(
            globalThis,
            "navigator",
        );
        const writeTextMock = vi.fn().mockResolvedValue(undefined);

        Object.defineProperty(globalThis, "navigator", {
            configurable: true,
            value: {
                clipboard: {
                    writeText: writeTextMock,
                },
            },
        });

        try {
            render(
                createElement(SeqmetaBadge, {
                    metadataKey: "seqmeta_library",
                    rawValue: "RNA",
                    enrichment: buildEnrichment({
                        identifier: "RNA",
                        type: "library_type",
                        graph: {
                            studies: [buildStudy()],
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
                                    id_run: 100,
                                    lane: 2,
                                    tag_index: 11,
                                    irods_path: "/seq/100",
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

            expect(screen.getByText("Sample 1 / S1")).toBeTruthy();

            const copyButtons = screen.getAllByLabelText(
                /Copy seqmeta_sampleid/i,
            );
            expect(copyButtons).toHaveLength(2);

            fireEvent.click(copyButtons[0]!);

            await waitFor(() => {
                expect(writeTextMock).toHaveBeenCalledWith("S1");
                expect(copyButtons[0]!.textContent).toContain("Copied");
            });
            expect(copyButtons[1]!.textContent).toContain("Copy");
        } finally {
            if (originalNavigatorDescriptor) {
                Object.defineProperty(
                    globalThis,
                    "navigator",
                    originalNavigatorDescriptor,
                );
            }
        }
    });

    it("copies study visible name while keeping study filter links", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");
        const originalNavigatorDescriptor = Object.getOwnPropertyDescriptor(
            globalThis,
            "navigator",
        );
        const writeTextMock = vi.fn().mockResolvedValue(undefined);

        Object.defineProperty(globalThis, "navigator", {
            configurable: true,
            value: {
                clipboard: {
                    writeText: writeTextMock,
                },
            },
        });

        try {
            render(
                createElement(SeqmetaBadge, {
                    metadataKey: "seqmeta_library",
                    rawValue: "Chromium single cell",
                    enrichment: {
                        identifier: "Chromium single cell",
                        type: "library_type",
                        graph: {
                            studies: [
                                {
                                    id_study_tmp: 99,
                                    id_lims: "SQSCP",
                                    id_study_lims: "7777",
                                    name: "Pilot study of dissociation methods for human gut tissues",
                                    faculty_sponsor: "Dr Smith",
                                    state: "active",
                                    accession_number: "ERP999999",
                                    data_release_strategy: "managed",
                                    study_title: "Gut Dissociation Pilot",
                                    data_access_group: "gut-team",
                                    programme: "Tissue Methods",
                                    reference_genome: "GRCh38",
                                    ethically_approved: true,
                                    study_type: "Single Cell RNA Sequencing",
                                    contains_human_dna: true,
                                    contaminated_human_dna: false,
                                    study_visibility: "Always Open",
                                    ega_dac_accession_number: "",
                                    ega_policy_accession_number: "",
                                    data_release_timing: "Standard",
                                },
                            ],
                            samples: [
                                {
                                    id_study_lims: "7777",
                                    id_sample_lims: "SC001",
                                    sanger_id: "SANG_SC_001",
                                    sample_name: "Gut tissue A",
                                    taxon_id: 9606,
                                    common_name: "Human",
                                    library_type: "Chromium single cell",
                                    id_run: 9001,
                                    lane: 1,
                                    tag_index: 1,
                                    irods_path: "/seq/9001",
                                    study_accession_number: "ERP999999",
                                    accession_number: "ERS999001",
                                },
                            ],
                        },
                        partial: false,
                    },
                }),
            );

            fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            fireEvent.click(
                screen.getByRole("button", {
                    name: /copy study_id/i,
                }),
            );

            await waitFor(() => {
                expect(writeTextMock).toHaveBeenCalledWith(
                    "Pilot study of dissociation methods for human gut tissues",
                );
            });

            expect(
                screen.getByRole("link", {
                    name: /send study to search filter/i,
                }),
            ).toHaveProperty("href", expect.stringContaining("study=7777"));
        } finally {
            if (originalNavigatorDescriptor) {
                Object.defineProperty(
                    globalThis,
                    "navigator",
                    originalNavigatorDescriptor,
                );
            }
        }
    });

    it("shows Related Data header before Study/Samples sections for seqmeta_library with no direct metadata", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_library",
                rawValue: "Chromium single cell",
                enrichment: {
                    identifier: "Chromium single cell",
                    type: "library_type",
                    graph: {
                        studies: [
                            {
                                id_study_tmp: 99,
                                id_lims: "SQSCP",
                                id_study_lims: "7777",
                                name: "Test Study",
                                faculty_sponsor: "Dr Test",
                                state: "active",
                                accession_number: "ERP000001",
                                data_release_strategy: "managed",
                                study_title: "Test Study",
                                data_access_group: "test-team",
                                programme: "Test Programme",
                                reference_genome: "GRCh38",
                                ethically_approved: true,
                                study_type: "Single Cell RNA Sequencing",
                                contains_human_dna: true,
                                contaminated_human_dna: false,
                                study_visibility: "Always Open",
                                ega_dac_accession_number: "",
                                ega_policy_accession_number: "",
                                data_release_timing: "Standard",
                            },
                        ],
                        samples: [
                            {
                                id_study_lims: "7777",
                                id_sample_lims: "S001",
                                sanger_id: "SANG_001",
                                sample_name: "Sample A",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "Chromium single cell",
                                id_run: 1001,
                                lane: 1,
                                tag_index: 1,
                                irods_path: "/seq/1001",
                                study_accession_number: "ERP000001",
                                accession_number: "ERS000001",
                            },
                        ],
                    },
                    partial: false,
                },
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should have a "Related Data" parent header
        expect(screen.getByText("Related Data")).toBeTruthy();

        // Study and Samples subsections should exist under Related Data
        expect(screen.getByText("Study")).toBeTruthy();
        expect(screen.getByText("Samples")).toBeTruthy();
    });

    it("shows Related Data header before Library/Study/Lanes sections for seqmeta_sampleid with direct metadata", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG_TEST_001",
                enrichment: {
                    identifier: "SANG_TEST_001",
                    type: "sanger_sample_id",
                    graph: {
                        study: {
                            id_study_tmp: 101,
                            id_lims: "SQSCP",
                            id_study_lims: "8888",
                            name: "Test Sample Study",
                            faculty_sponsor: "Dr Sample",
                            state: "active",
                            accession_number: "EGAS00001000001",
                            data_release_strategy: "managed",
                            study_title: "Test Sample Study",
                            data_access_group: "test-group",
                            programme: "Test Programme",
                            reference_genome: "GRCh38",
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
                            id_study_lims: "8888",
                            id_sample_lims: "7000001",
                            sanger_id: "SANG_TEST_001",
                            sample_name: "Test Sample A",
                            taxon_id: 9606,
                            common_name: "human",
                            library_type: "Chromium single cell RNA",
                            id_run: 50001,
                            lane: 1,
                            tag_index: 5,
                            irods_path:
                                "/seq/illumina/runs/50/50001/lane1/plex5/50001_1#5.cram",
                            study_accession_number: "EGAS00001000001",
                            accession_number: "EGAN00004000001",
                        },
                        sample_detail: {
                            sanger_id: "SANG_TEST_001",
                            sample_name: "Test Sample A",
                            sample: {
                                id_study_lims: "8888",
                                id_sample_lims: "7000001",
                                sanger_id: "SANG_TEST_001",
                                sample_name: "Test Sample A",
                                taxon_id: 9606,
                                common_name: "human",
                                library_type: "Chromium single cell RNA",
                                id_run: 50001,
                                lane: 1,
                                tag_index: 5,
                                irods_path:
                                    "/seq/illumina/runs/50/50001/lane1/plex5/50001_1#5.cram",
                                study_accession_number: "EGAS00001000001",
                                accession_number: "EGAN00004000001",
                            },
                            lanes: [
                                { id_run: "50001", lane: "1", tag_index: 5 },
                                { id_run: "50002", lane: "2", tag_index: 6 },
                            ],
                        },
                    },
                    partial: false,
                },
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should have Direct Metadata section (sample name, accession, etc.)
        expect(screen.getByText("Direct Metadata")).toBeTruthy();

        // Should have a "Related Data" parent header
        expect(screen.getByText("Related Data")).toBeTruthy();

        // Library, Study, and Lanes subsections should exist under Related Data
        expect(screen.getByText("Library")).toBeTruthy();
        expect(screen.getByText("Study")).toBeTruthy();
        expect(screen.getByText("Lanes")).toBeTruthy();
    });

    it("does not show individual sample fields in flat detail fields for study metadata with study_detail", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        // Mock JIT loading of library samples - return first library's sample on first call
        const polyASamples = [
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
        ];

        fetchLibrarySamplesMock.mockResolvedValue(polyASamples);

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
                            accession_number: "ERP123456",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            programme: "",
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
                        // Backend returns samples array even with study_detail
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
                                library_type: "RNA Ribozero",
                                id_run: 101,
                                lane: 1,
                                tag_index: 11,
                                irods_path: "/seq/101",
                                study_accession_number: "ERP123456",
                                accession_number: "ERS002",
                            },
                        ],
                        study_detail: {
                            study: {
                                id_study_tmp: 42,
                                id_lims: "SQSCP",
                                id_study_lims: "6568",
                                name: "RNA Seq",
                                faculty_sponsor: "Dr Example",
                                state: "active",
                                accession_number: "ERP123456",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                programme: "",
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
                                    samples: [], // Samples loaded JIT when library is expanded
                                },
                                {
                                    library_type: "RNA Ribozero",
                                    id_study_lims: "6568",
                                    samples: [], // Samples loaded JIT when library is expanded
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

        // Should have Direct Metadata section with study info
        expect(screen.getByText("Direct Metadata")).toBeTruthy();
        expect(screen.getByText("Study name")).toBeTruthy();
        expect(screen.getByText("RNA Seq")).toBeTruthy();
        expect(screen.getByText("Study accession")).toBeTruthy();
        expect(screen.getByText("ERP123456")).toBeTruthy();

        // Should NOT show individual sample fields as flat detail fields
        // (sample_name, sanger_id, etc. should only appear in hierarchical Libraries)
        const directMetadataSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="direct-metadata"]');

        // Sample-related field labels should NOT appear in direct metadata
        expect(
            directMetadataSection?.textContent?.includes("Sample name"),
        ).toBe(false);
        expect(
            directMetadataSection?.textContent?.includes("Sanger sample ID"),
        ).toBe(false);
        expect(
            directMetadataSection?.textContent?.includes("Sample LIMS ID"),
        ).toBe(false);

        // Should have hierarchical Libraries section
        expect(screen.getByText("Libraries")).toBeTruthy();
        expect(screen.getByText("RNA PolyA")).toBeTruthy();
        expect(screen.getByText("RNA Ribozero")).toBeTruthy();

        // Samples should only appear when library is expanded, not as flat fields
        expect(screen.queryByText("Sample 1")).toBeNull();
        expect(screen.queryByText("Sample 2")).toBeNull();

        // Expand first library to verify samples are there
        const expandButtons = screen.getAllByLabelText("Show samples");
        fireEvent.click(expandButtons[0]);

        await waitFor(() => {
            expect(screen.getByText("Sample 1 / S1")).toBeTruthy();
        });
    });

    it("does not show linked_samples field for study metadata with study_detail", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        // Mock JIT loading of library samples
        const librarySamples = [
            buildSample({
                id_sample_lims: "SMP001",
                sanger_id: "S1",
                sample_name: "Sample 1",
            }),
            buildSample({
                id_sample_lims: "SMP002",
                sanger_id: "S2",
                sample_name: "Sample 2",
            }),
            buildSample({
                id_sample_lims: "SMP003",
                sanger_id: "S3",
                sample_name: "Sample 3",
            }),
        ];

        fetchLibrarySamplesMock.mockResolvedValue(librarySamples);

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
                            accession_number: "ERP123456",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            programme: "",
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
                            buildSample({
                                id_sample_lims: "SMP001",
                                sanger_id: "S1",
                                sample_name: "Sample 1",
                            }),
                            buildSample({
                                id_sample_lims: "SMP002",
                                sanger_id: "S2",
                                sample_name: "Sample 2",
                            }),
                            buildSample({
                                id_sample_lims: "SMP003",
                                sanger_id: "S3",
                                sample_name: "Sample 3",
                            }),
                        ],
                        study_detail: {
                            study: {
                                id_study_tmp: 42,
                                id_lims: "SQSCP",
                                id_study_lims: "6568",
                                name: "RNA Seq",
                                faculty_sponsor: "Dr Example",
                                state: "active",
                                accession_number: "ERP123456",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                programme: "",
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
                                    library_type: "RNA",
                                    id_study_lims: "6568",
                                    samples: [], // Samples loaded JIT when library is expanded
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

        // Should NOT have a "Linked samples" field anywhere in the dialog
        expect(screen.queryByText("Linked samples")).toBeNull();

        // Should have hierarchical Libraries section instead
        expect(screen.getByText("Libraries")).toBeTruthy();
        expect(screen.getByText("RNA")).toBeTruthy();

        // Samples should be accessible by expanding the library
        const expandButton = screen.getByLabelText("Show samples");
        fireEvent.click(expandButton);

        await waitFor(() => {
            expect(screen.getByText("Sample 1 / S1")).toBeTruthy();
            expect(screen.getByText("Sample 2 / S2")).toBeTruthy();
            expect(screen.getByText("Sample 3 / S3")).toBeTruthy();
        });
    });

    it("does not render legacy sample or library rows for study metadata without study_detail", async () => {
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
                            accession_number: "ERP123456",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            programme: "",
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
                        sample: {
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
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const dialogBody = screen.getByTestId("seqmeta-dialog-body");

        expect(dialogBody.textContent).not.toContain("Sample name");
        expect(dialogBody.textContent).not.toContain("Sanger sample ID");
        expect(dialogBody.textContent).not.toContain("Sample LIMS ID");
        expect(dialogBody.textContent).not.toContain("Library type");
    });

    it("renders each sample as a single row under expanded library, not as multiple field rows", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        // Mock JIT loading of library samples
        const librarySamples = [
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
        ];

        fetchLibrarySamplesMock.mockResolvedValue(librarySamples);

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
                            accession_number: "ERP123456",
                            data_release_strategy: "",
                            study_title: "",
                            data_access_group: "",
                            programme: "",
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
                                accession_number: "ERP123456",
                                data_release_strategy: "",
                                study_title: "",
                                data_access_group: "",
                                programme: "",
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
                                    samples: [], // Samples loaded JIT when library is expanded
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

        // Expand library to show samples
        const expandButton = screen.getByLabelText("Show samples");
        fireEvent.click(expandButton);

        await waitFor(() => {
            expect(screen.getByText("Sample 1 / S1")).toBeTruthy();
        });

        // Each sample should appear as ONE row with display name
        // NOT as multiple rows for sample_name, sanger_id, etc.
        const sampleRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="sample"]');

        // Should have exactly 2 sample rows (one per sample)
        expect(sampleRows.length).toBe(2);

        // First sample row should contain both name and ID in one row
        const firstSampleRow = sampleRows[0];
        expect(firstSampleRow?.textContent).toContain("Sample 1 / S1");

        // Second sample row should contain both name and ID in one row
        const secondSampleRow = sampleRows[1];
        expect(secondSampleRow?.textContent).toContain("Sample 2 / S2");

        // Each sample row should have copy and filter buttons
        const copyButtons = screen.getAllByLabelText(/Copy seqmeta_sampleid/i);
        const filterButtons = screen.getAllByLabelText(
            /Send sample to search filter/i,
        );
        expect(copyButtons.length).toBe(2);
        expect(filterButtons.length).toBe(2);
    });
});
