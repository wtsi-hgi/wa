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
    within,
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
    LibraryDetail,
    ResultSet,
    SampleDetail,
} from "@/lib/contracts";
import { enrichmentResultSchema } from "@/lib/contracts";
import {
    buildMLWHCacheCookie,
    deserializeMLWHCacheCookie,
    MLWH_CACHE_COOKIE_NAME,
} from "@/lib/mlwh-cache-core";
import {
    MLWHCache,
    MLWHCacheContext,
    MLWHCacheProvider,
} from "@/lib/mlwh-cache";

const fetchResultMock = vi.fn();
const fetchFilesMock = vi.fn();
const validateIdentifierMock = vi.fn();
const enrichIdentifierMock = vi.fn();
const enrichIdentifiersMock = vi.fn(async (values: string[]) =>
    Promise.all(
        values.map(async (value) => {
            try {
                const enrichment = await enrichIdentifierMock(value);

                return {
                    value,
                    enrichment: enrichment ?? null,
                    error: enrichment == null ? "not_found" : undefined,
                };
            } catch {
                return {
                    value,
                    enrichment: null,
                    error: "upstream_impaired" as const,
                };
            }
        }),
    ),
);
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
    enrichIdentifiers: enrichIdentifiersMock,
}));

vi.mock("@/lib/mlwh-enrichment", async (importOriginal) => {
    const actual =
        await importOriginal<typeof import("@/lib/mlwh-enrichment")>();
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
    const prefix = `${MLWH_CACHE_COOKIE_NAME}=`;

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
        libraries?: SampleDetail["libraries"];
        irods_paths?: Array<Partial<IRODSPath>>;
    } = {},
): SampleDetail {
    const sample = buildSample(overrides.sample);

    return {
        sanger_id: overrides.sanger_id ?? sample.sanger_id,
        sample_name: overrides.sample_name ?? sample.sample_name,
        sample,
        lanes: (overrides.lanes ?? []).map((lane) => buildLaneDetail(lane)),
        ...(overrides.libraries ? { libraries: overrides.libraries } : {}),
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

function entityTitle(row: Element | null | undefined): string {
    return (
        row?.querySelector('[data-testid="seqmeta-entity-title"]')
            ?.textContent ?? ""
    );
}

function entityText(row: Element | null | undefined): string {
    return row?.textContent ?? "";
}

function expectEntityRowTitle(row: Element | null | undefined, title: string) {
    expect(entityTitle(row)).toBe(title);
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
        document.cookie = `${MLWH_CACHE_COOKIE_NAME}=; Max-Age=0; Path=/`;
        setRequestCookieHeader();

        if (originalDocumentCookie) {
            Object.defineProperty(document, "cookie", originalDocumentCookie);
        }
    });

    it("opens a modal with structured seqmeta details for an enriched badge", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: buildEnrichment(),
            }),
        );
        expect(screen.queryByRole("dialog")).toBeNull();

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const studyRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="study"]');

        expect(studyRows).toHaveLength(1);
        expectEntityRowTitle(studyRows[0], "6568");
        expect(studyRows[0]?.textContent).toContain("name:");
        expect(studyRows[0]?.textContent).toContain("RNA Seq");
        expect(studyRows[0]?.textContent).not.toContain("id:6568");
        expect(studyRows[0]?.textContent).toContain("6568");
        expect(screen.queryByText("Study name")).toBeNull();
        expect(
            screen
                .getByRole("link", {
                    name: /send study to search filter/i,
                })
                .getAttribute("href"),
        ).toBe("/?study=6568");
    });

    it("does not render project rows even when legacy project fields are present", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");
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
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: legacyEnrichment,
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
            resolve(process.cwd(), "components/mlwh-badge.tsx"),
            "utf8",
        );

        expect(source).not.toContain("Sa" + "ga");
        expect(source).not.toContain("via " + "Sa" + "ga");
    });

    it("renders seqmeta_library details without singular sample or study guesses", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        expect(screen.getByTestId("mlwh-badge-label").textContent).toBe("RNA");
        expect(screen.queryByText("sanger_sample_id: RNA")).toBeNull();

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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

    it("opens library details quickly without rendering every related sample row", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");
        const samples = Array.from({ length: 1000 }, (_, index) =>
            buildSample({
                id_sample_lims: `LIMS${index}`,
                sanger_id: `SANG${index}`,
                sample_name: `Sample ${index}`,
            }),
        );

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_librarytype",
                rawValue: "Custom",
                enrichment: buildEnrichment({
                    identifier: "Custom",
                    type: "library_type",
                    graph: {
                        study: undefined,
                        studies: [buildStudy()],
                        samples,
                    },
                    partial: true,
                    missing: [
                        {
                            hop: "samples",
                            reason: "samples_truncated",
                            status: 200,
                        },
                    ],
                }),
            }),
        );

        const startedAt = performance.now();

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const elapsedMs = performance.now() - startedAt;
        const sampleRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="sample"]');

        expect(elapsedMs).toBeLessThan(1000);
        expect(sampleRows.length).toBeLessThanOrEqual(50);
        expect(screen.getByText("SANG0")).toBeTruthy();
        expect(screen.getByText("Sample 0")).toBeTruthy();
        expect(screen.getByText("Showing 50 of 1000 samples")).toBeTruthy();
    });

    it("renders a vertically scrollable body for long seqmeta detail content", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                }),
            }),
        );

        expect(screen.getByTestId("mlwh-badge-label").textContent).toBe("6568");
    });

    it("shows the raw value with a failure indicator and unavailable tooltip when enrichment fails", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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
        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });
        expect(
            screen.getByText("No enrichment matched this sample name value."),
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const studyRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="study"]');

        expect(studyRows).toHaveLength(1);
        expect(studyRows[0]?.textContent).toContain("name:");
        expect(studyRows[0]?.textContent).toContain("RNA Seq");
        expect(screen.queryByText("Study name")).toBeNull();
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

        expect(screen.getByTestId("mlwh-badge-trigger").className).toContain(
            "cursor-pointer",
        );
    });

    it("falls back to document copy for seqmeta details when Clipboard API is unavailable", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");
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
                createElement(MLWHBadge, {
                    metadataKey: "seqmeta_sampleid",
                    rawValue: "SANG001",
                    enrichment: buildEnrichment(),
                }),
            );

            fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            fireEvent.click(
                screen.getAllByLabelText(/Copy seqmeta_sample_name/i)[0]!,
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

    it("leaves seqmeta enrichment to the client during server detail rendering", async () => {
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

        expect(enrichIdentifiersMock).not.toHaveBeenCalled();
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
        expect(enrichIdentifiersMock).not.toHaveBeenCalled();
        expect(markup).not.toContain("Explorer");
        expect(markup).not.toContain("Preview focus");
        expect(markup).not.toContain("data-selected-file-path");
        expect(markup).not.toContain("Path");
        expect(markup).not.toContain("Kind");
        expect(markup).not.toContain("Updated");
        expect(markup).toContain('data-result-detail-summary="true"');
        expect(markup).toContain('data-registration-layout="integrated"');
        expect(markup).toContain('data-result-metadata-layout="integrated"');
        expect(markup).toContain("SANG001");
        expect(markup).not.toContain("sanger_sample_id: SANG001");
        expect(markup).not.toContain('data-file-summary="true"');
        expect(markup).not.toContain('data-registration-layout="compact"');
        expect(markup).toContain('data-registration-field="Last updated"');
        expect(markup).toContain('data-registration-field="Requester"');
        expect(markup).toContain('data-registration-field="Operator"');
        expect(markup).not.toContain(
            'data-registration-field="Pipeline version"',
        );
        expect(markup).not.toContain('data-registration-field="Unique"');
        expect(markup).not.toContain('data-registration-field="Result ID"');
        expect(markup).not.toContain('data-registration-field="Pipeline name"');
        expect(markup).not.toMatch(/>Registration</);
        expect(markup).not.toMatch(/>Result metadata</);
        expect(markup).not.toMatch(/>Key details</);
        expect(markup).not.toContain("Registration summary");
        expect(countOccurrences(markup, "nf-core/rnaseq")).toBe(1);
        expect(countOccurrences(markup, "data-metadata-row=")).toBe(2);
        expect(markup).toContain("library");
        expect(markup).toContain("rna");
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
        const cache = new MLWHCache();
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
                        MLWHCacheContext.Provider,
                        { value: cache },
                        children,
                    ),
            },
        );

        expect(screen.getByTestId("mlwh-badge-label").textContent).toBe(
            "SANG001",
        );
        expect(screen.queryByLabelText("loading enrichment")).toBeNull();
        expect(enrichIdentifiersMock).not.toHaveBeenCalled();
        expect(enrichIdentifierMock).not.toHaveBeenCalled();

        firstRender.unmount();

        render(
            createElement(ResultMetadataEnrichment, {
                metadata,
            }),
            {
                wrapper: ({ children }) =>
                    createElement(
                        MLWHCacheContext.Provider,
                        { value: cache },
                        children,
                    ),
            },
        );

        expect(screen.getByTestId("mlwh-badge-label").textContent).toBe(
            "SANG001",
        );
        expect(screen.queryByLabelText("loading enrichment")).toBeNull();
        expect(enrichIdentifiersMock).not.toHaveBeenCalled();
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
                    createElement(MLWHCacheProvider, null, children),
            },
        );

        expect(screen.getByTestId("mlwh-badge-label").textContent).toBe(
            "SANG001",
        );
        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
                    createElement(MLWHCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            expect(readSeqmetaCookieFromDocument()).toContain(
                MLWH_CACHE_COOKIE_NAME,
            );
        });

        const persistedCookie = readSeqmetaCookieFromDocument();
        expect(persistedCookie).toBeTruthy();

        const cookieValue = persistedCookie?.split("=").slice(1).join("=");
        expect(deserializeMLWHCacheCookie(cookieValue)).toEqual({});

        const firstMarkup = renderToStaticMarkup(
            await pageModule.ResultDetailPageContent({
                id: "result-42",
            }),
        );

        expect(enrichIdentifiersMock).toHaveBeenCalledTimes(0);
        expect(firstMarkup).toContain("SANG001");
        expect(firstMarkup).not.toContain("sanger_sample_id: SANG001");

        setRequestCookieHeader(
            buildMLWHCacheCookie({ SANG001: buildEnrichment() }),
        );

        const secondMarkup = renderToStaticMarkup(
            await pageModule.ResultDetailPageContent({
                id: "result-99",
            }),
        );

        expect(enrichIdentifiersMock).toHaveBeenCalledTimes(0);
        expect(secondMarkup).toContain("SANG001");
        expect(secondMarkup).not.toContain("sanger_sample_id: SANG001");
    });

    it("does not rewrite the seqmeta cookie when mount enrichment matches the existing cache", async () => {
        const enrichment = buildEnrichment();

        document.cookie = buildMLWHCacheCookie({ SANG001: enrichment });
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
                    createElement(MLWHCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            expect(screen.getByTestId("mlwh-badge-label").textContent).toBe(
                "SANG001",
            );
        });

        expect(cookieWrites).toHaveLength(writesBeforeRender);
        expect(readSeqmetaCookieFromDocument()).toBe(document.cookie);
    });

    it("keeps server detail rendering neutral when seqmeta enrichment would fail client-side", async () => {
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

        expect(enrichIdentifiersMock).not.toHaveBeenCalled();
        expect(markup).not.toContain("enrichment backend impaired");
        expect(markup).toContain("SANG001");
    });

    it("hydrates without mismatches when the client cookie has a stale unavailable marker", async () => {
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        enrichIdentifierMock.mockReset();
        enrichIdentifierMock.mockResolvedValue(null);
        const serverTree = createElement(
            MLWHCacheContext.Provider,
            { value: new MLWHCache() },
            createElement(ResultMetadataEnrichment, {
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
            }),
        );
        const clientTree = createElement(
            MLWHCacheProvider,
            null,
            createElement(ResultMetadataEnrichment, {
                metadata: {
                    seqmeta_sampleid: "SANG001",
                },
            }),
        );
        const container = document.createElement("div");
        const recoverableErrors: unknown[] = [];

        document.cookie = buildMLWHCacheCookie({ SANG001: null });
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

    it("hydrates without mismatches when both server and client use MLWHCacheProvider with a stale not_found cookie", async () => {
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        enrichIdentifierMock.mockReset();
        enrichIdentifierMock.mockResolvedValue(null);

        // Pre-populate document.cookie with a stale "not_found" entry, as
        // would happen on a returning user whose previous visit cached a
        // negative lookup for SANG001.
        document.cookie = buildMLWHCacheCookie({ SANG001: null });

        // Both the server-rendered tree and the client hydrated tree go
        // through the real MLWHCacheProvider, mirroring the production
        // (results)/layout.tsx wiring.
        const tree = () =>
            createElement(
                MLWHCacheProvider,
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

        document.cookie = buildMLWHCacheCookie({ SANG001: null });

        const tree = () =>
            createElement(
                MLWHCacheProvider,
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

    it("renders 'loading enrichment' on the server even when the MLWHCacheContext is fed a pre-populated cache (matches the client's first hydration render)", async () => {
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");
        enrichIdentifierMock.mockReset();
        enrichIdentifierMock.mockResolvedValue(null);

        // Simulate the worst case: a MLWHCache that already contains a
        // cached "not_found" result is provided via MLWHCacheContext for
        // the SSR pass. This mirrors what would happen if any code path on
        // the server (or a returning client whose cookies were merged into
        // the live cache before hydration completed) hands a populated cache
        // into the consumer. The component must still render the loading
        // state on the very first render so that SSR and hydration agree.
        const populatedCache = new MLWHCache({ SANG001: null });

        const serverTree = createElement(
            MLWHCacheContext.Provider,
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

    it("shows dialog title matching raw value with canonical key in subtitle", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const dialog = screen.getByRole("dialog");
        const title = dialog.querySelector("h3");

        expect(title?.textContent).toBe("6568");

        const subtitle = dialog.querySelector("p.font-mono");

        expect(subtitle?.textContent).toContain("seqmeta_id_study_lims");
        expect(subtitle?.textContent).not.toContain("study_id");
    });

    it("omits duplicate selected metadata value row from dialog", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.queryByText("Selected metadata value")).toBeNull();
    });

    it("omits redundant resolved seqmeta type row from dialog", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.queryByText("Resolved seqmeta type")).toBeNull();
    });

    it("omits summary and resolution aside from dialog", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.queryByText("Summary")).toBeNull();
        expect(screen.queryByText("Resolution")).toBeNull();
    });

    it("groups libraries without label duplication when study_detail hierarchy is present", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

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
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
            expect(screen.getByText("S1")).toBeTruthy();
        });

        // Verify fetchLibrarySamples was called with correct arguments
        expect(fetchLibrarySamplesMock).toHaveBeenCalledWith(
            "6568",
            "RNA PolyA",
        );

        // Both samples should now be visible
        expect(screen.getByText("S2")).toBeTruthy();

        // Button should now show count
        expect(screen.getByText("2 samples")).toBeTruthy();

        // Each sample should have its own copy/filter buttons
        const sampleButtons = screen.getAllByLabelText(/Copy|Filter/);

        expect(sampleButtons.length).toBeGreaterThan(2);
    });

    it("expands study libraries by specific library identifiers when a study has repeated library types", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");
        const firstLibrarySample = buildSample({
            id_sample_lims: "SMP001",
            sanger_id: "S1",
            sample_name: "Sample 1",
        });
        const secondLibrarySample = buildSample({
            id_sample_lims: "SMP002",
            sanger_id: "S2",
            sample_name: "Sample 2",
        });

        fetchLibrarySamplesMock.mockImplementation(
            async (
                _studyId: string,
                _libraryType: string,
                filters?: { idLibraryLims?: string; libraryId?: string },
            ) =>
                filters?.idLibraryLims === "DN111:A1"
                    ? [firstLibrarySample]
                    : [secondLibrarySample],
        );

        const libraryDetails = [
            {
                library_type: "RNA PolyA",
                id_study_lims: "6568",
                library_id: "1001",
                id_library_lims: "DN111:A1",
                samples: [],
            },
            {
                library_type: "RNA PolyA",
                id_study_lims: "6568",
                library_id: "1002",
                id_library_lims: "DN222:B1",
                samples: [],
            },
        ] as unknown as LibraryDetail[];

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                    graph: {
                        study: buildStudy(),
                        study_detail: {
                            study: buildStudy(),
                            library_details: libraryDetails,
                        },
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.getByText("DN111:A1")).toBeTruthy();
        expect(screen.getByText("DN222:B1")).toBeTruthy();

        const expandButtons = screen.getAllByLabelText("Show samples");
        fireEvent.click(expandButtons[0]!);

        await waitFor(() => {
            expect(screen.getByText("S1")).toBeTruthy();
        });

        expect(screen.queryByText("S2")).toBeNull();
        expect(fetchLibrarySamplesMock).toHaveBeenCalledWith(
            "6568",
            "RNA PolyA",
            {
                idLibraryLims: "DN111:A1",
                libraryId: "1001",
            },
        );
    });

    it("shows run-id related library identifiers with the run samples already in the details payload", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");
        const runSamples = [
            buildSample({
                id_study_lims: "7607",
                id_sample_lims: "SMP001",
                sanger_id: "S1",
                sample_name: "Run Sample 1",
                library_type: "Custom",
            }),
            buildSample({
                id_study_lims: "7607",
                id_sample_lims: "SMP002",
                sanger_id: "S2",
                sample_name: "Run Sample 2",
                library_type: "Custom",
            }),
        ];

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_runid",
                rawValue: "48522",
                enrichment: buildEnrichment({
                    identifier: "48522",
                    type: "run_id",
                    graph: {
                        studies: [buildStudy({ id_study_lims: "7607" })],
                        samples: runSamples,
                        libraries: [
                            {
                                library_type: "Custom",
                                id_study_lims: "7607",
                                library_id: "71046409",
                                id_library_lims: "SQPP-47463-G:B1",
                            },
                        ],
                        study_details: [
                            {
                                study: buildStudy({ id_study_lims: "7607" }),
                                library_details: [
                                    {
                                        library_type: "Custom",
                                        id_study_lims: "7607",
                                        library_id: "71046409",
                                        id_library_lims: "SQPP-47463-G:B1",
                                        samples: runSamples,
                                    },
                                ],
                            },
                        ],
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.getByText("SQPP-47463-G:B1")).toBeTruthy();
        expect(screen.getByText("Custom")).toBeTruthy();

        fireEvent.click(screen.getByLabelText("Show samples"));

        expect(screen.getByText("S1")).toBeTruthy();
        expect(screen.getByText("S2")).toBeTruthy();
        expect(screen.getByText("2 samples")).toBeTruthy();
        expect(fetchLibrarySamplesMock).not.toHaveBeenCalled();
    });

    it("renders run-id related study and sample as one row per entity", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_runid",
                rawValue: "48522",
                enrichment: buildEnrichment({
                    identifier: "48522",
                    type: "run_id",
                    graph: {
                        study: buildStudy({
                            id_study_lims: "7607",
                            name: "Run Study",
                            accession_number: "EGAS00001005445",
                        }),
                        sample: buildSample({
                            id_study_lims: "7607",
                            id_sample_lims: "SMP7607",
                            sanger_id: "7607STDY14643771",
                            sample_name: "Run Sample",
                        }),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const dialogBody = screen.getByTestId("seqmeta-dialog-body");
        const relatedData = dialogBody.querySelector(
            '[data-field-group="related-data"]',
        );
        const studyRows = relatedData?.querySelectorAll(
            '[data-seqmeta-detail-key="study"]',
        );
        const sampleRows = relatedData?.querySelectorAll(
            '[data-seqmeta-detail-key="sample"]',
        );

        expect(studyRows).toHaveLength(1);
        expect(studyRows?.[0]?.textContent).toContain("Run Study");
        expect(studyRows?.[0]?.textContent).toContain("7607");
        expect(studyRows?.[0]?.textContent).toContain("EGAS00001005445");

        expect(sampleRows).toHaveLength(1);
        expect(sampleRows?.[0]?.textContent).toContain("Run Sample");
        expect(sampleRows?.[0]?.textContent).toContain("7607STDY14643771");
        expect(sampleRows?.[0]?.textContent).toContain("SMP7607");

        expect(relatedData?.textContent).not.toContain("Study name");
        expect(relatedData?.textContent).not.toContain("Study identifier");
        expect(relatedData?.textContent).not.toContain("Sanger sample ID");
        expect(relatedData?.textContent).not.toContain("Sample LIMS ID");
    });

    it("uses ID titles and omits duplicated identity metadata for run-id related entities", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_runid",
                rawValue: "48522",
                enrichment: buildEnrichment({
                    identifier: "48522",
                    type: "run_id",
                    graph: {
                        study: buildStudy({
                            id_study_lims: "7607",
                            name: "7607",
                            accession_number: "ERP7607",
                        }),
                        sample: buildSample({
                            id_study_lims: "7607",
                            id_sample_lims: "SMP7607-0000",
                            sanger_id: "7607STDY14643771",
                            sample_name: "7607STDY14643771",
                            accession_number: "SAMEA76070",
                            library_type: "Custom",
                        }),
                        libraries: [
                            {
                                library_type: "Custom",
                                id_study_lims: "7607",
                                library_id: "71046409",
                                id_library_lims: "LIB7607-71046409",
                            },
                        ],
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const relatedData = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="related-data"]');
        const studyRow = relatedData?.querySelector(
            '[data-seqmeta-detail-key="study"]',
        );
        const sampleRow = relatedData?.querySelector(
            '[data-seqmeta-detail-key="sample"]',
        );
        const libraryRow = relatedData?.querySelector(
            '[data-seqmeta-detail-key="seqmeta_pipeline_id_lims"]',
        );

        expectEntityRowTitle(studyRow, "7607");
        expect(entityText(studyRow)).not.toContain("name:7607");
        expect(entityText(studyRow)).not.toContain("id:7607");
        expect(entityText(studyRow)).toContain("accession:ERP7607");

        expectEntityRowTitle(sampleRow, "7607STDY14643771");
        expect(entityText(sampleRow)).not.toContain("name:7607STDY14643771");
        expect(entityText(sampleRow)).not.toContain("id:7607STDY14643771");
        expect(entityText(sampleRow)).toContain("sample_lims:SMP7607-0000");
        expect(entityText(sampleRow)).toContain("accession:SAMEA76070");

        expectEntityRowTitle(libraryRow, "71046409");
        expect(entityText(libraryRow)).not.toContain("id:71046409");
        expect(entityText(libraryRow)).toContain(
            "library_lims:LIB7607-71046409",
        );
        expect(entityText(libraryRow)).toContain("type:Custom");
    });

    it("uses the same related entity row shape from sample, study, and library detail contexts", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");
        const sharedStudy = buildStudy({
            id_study_lims: "6568",
            name: "RNA Seq",
            accession_number: "ERP123456",
        });
        const sharedSample = buildSample({
            id_study_lims: "6568",
            id_sample_lims: "LIMS001",
            sanger_id: "SANG001",
            sample_name: "Sample 1",
            accession_number: "ERS123456",
            library_type: "RNA PolyA",
        });
        const sharedLibrary = {
            library_type: "RNA PolyA",
            id_study_lims: "6568",
            library_id: "1001",
            id_library_lims: "DN111:A1",
        };

        const cases = [
            {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: buildEnrichment({
                    identifier: "SANG001",
                    type: "sanger_sample_id",
                    graph: {
                        study: sharedStudy,
                        sample: sharedSample,
                        library: sharedLibrary,
                        sample_detail: buildSampleDetail({
                            sample: sharedSample,
                            libraries: [sharedLibrary],
                            lanes: [
                                {
                                    id_run: "12345",
                                    lane: "2",
                                    tag_index: 88,
                                },
                            ],
                        }),
                    },
                }),
                rows: [
                    {
                        selector: '[data-seqmeta-detail-key="library"]',
                        title: "1001",
                        absent: "id:1001",
                        present: "type:RNA PolyA",
                    },
                    {
                        selector: '[data-seqmeta-detail-key="study"]',
                        title: "6568",
                        absent: "id:6568",
                        present: "name:RNA Seq",
                    },
                    {
                        selector: '[data-seqmeta-detail-key="lane"]',
                        title: "12345_2#88",
                        absent: "id:12345_2#88",
                        present: "id_run:12345",
                    },
                ],
            },
            {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                    graph: {
                        study: sharedStudy,
                        study_detail: {
                            study: sharedStudy,
                            library_details: [
                                {
                                    ...sharedLibrary,
                                    samples: [],
                                },
                            ],
                        },
                    },
                }),
                rows: [
                    {
                        selector:
                            '[data-seqmeta-detail-key="seqmeta_pipeline_id_lims"]',
                        title: "1001",
                        absent: "id:1001",
                        present: "library_lims:DN111:A1",
                    },
                ],
            },
            {
                metadataKey: "seqmeta_libraryid",
                rawValue: "1001",
                enrichment: buildEnrichment({
                    identifier: "1001",
                    type: "library_id",
                    graph: {
                        studies: [sharedStudy],
                        samples: [sharedSample],
                        library: sharedLibrary,
                    },
                }),
                rows: [
                    {
                        selector: '[data-seqmeta-detail-key="study"]',
                        title: "6568",
                        absent: "id:6568",
                        present: "accession:ERP123456",
                    },
                    {
                        selector: '[data-seqmeta-detail-key="sample"]',
                        title: "SANG001",
                        absent: "id:SANG001",
                        present: "name:Sample 1",
                    },
                ],
            },
        ];

        for (const testCase of cases) {
            cleanup();
            render(
                createElement(MLWHBadge, {
                    metadataKey: testCase.metadataKey,
                    rawValue: testCase.rawValue,
                    enrichment: testCase.enrichment,
                }),
            );

            fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            const relatedData = screen
                .getByTestId("seqmeta-dialog-body")
                .querySelector('[data-field-group="related-data"]');

            for (const rowExpectation of testCase.rows) {
                const row = relatedData?.querySelector(rowExpectation.selector);

                expectEntityRowTitle(row, rowExpectation.title);
                expect(entityText(row)).not.toContain(rowExpectation.absent);
                expect(entityText(row)).toContain(rowExpectation.present);
            }
        }
    });

    it("renders study related libraries as entity rows with metadata pairs", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_studyid",
                rawValue: "6568",
                enrichment: buildEnrichment({
                    identifier: "6568",
                    type: "study_id",
                    graph: {
                        study: buildStudy({
                            id_study_lims: "6568",
                            name: "RNA Seq",
                        }),
                        study_detail: {
                            study: buildStudy({
                                id_study_lims: "6568",
                                name: "RNA Seq",
                            }),
                            library_details: [
                                {
                                    library_type: "RNA PolyA",
                                    id_study_lims: "6568",
                                    library_id: "1001",
                                    id_library_lims: "DN111:A1",
                                    samples: [],
                                },
                            ],
                        },
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const libraryRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll(
                '[data-seqmeta-detail-key="seqmeta_pipeline_id_lims"]',
            );

        expect(libraryRows).toHaveLength(1);
        expectEntityRowTitle(libraryRows[0], "1001");
        expect(libraryRows[0]?.textContent).toContain("type:");
        expect(libraryRows[0]?.textContent).toContain("RNA PolyA");
        expect(libraryRows[0]?.textContent).not.toContain("id:1001");
        expect(libraryRows[0]?.textContent).toContain("1001");
        expect(libraryRows[0]?.textContent).toContain("library_lims:");
        expect(libraryRows[0]?.textContent).toContain("DN111:A1");
    });

    it("renders sample related library and lane rows with metadata pairs", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "SANG001",
                enrichment: buildEnrichment({
                    identifier: "SANG001",
                    type: "sanger_sample_id",
                    graph: {
                        study: buildStudy({
                            id_study_lims: "6568",
                            name: "RNA Seq",
                            accession_number: "ERP123456",
                        }),
                        sample: buildSample({
                            id_study_lims: "6568",
                            id_sample_lims: "LIMS001",
                            sanger_id: "SANG001",
                            sample_name: "Sample 1",
                            library_type: "RNA PolyA",
                        }),
                        library: {
                            library_type: "RNA PolyA",
                            id_study_lims: "6568",
                            library_id: "1001",
                            id_library_lims: "DN111:A1",
                        },
                        sample_detail: buildSampleDetail({
                            sample: {
                                id_study_lims: "6568",
                                id_sample_lims: "LIMS001",
                                sanger_id: "SANG001",
                                sample_name: "Sample 1",
                                library_type: "RNA PolyA",
                            },
                            lanes: [
                                {
                                    id_run: "12345",
                                    lane: "2",
                                    tag_index: 88,
                                },
                            ],
                        }),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const libraryRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="library"]');
        const laneRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="lane"]');

        expect(libraryRows).toHaveLength(1);
        expectEntityRowTitle(libraryRows[0], "1001");
        expect(libraryRows[0]?.textContent).toContain("type:");
        expect(libraryRows[0]?.textContent).toContain("RNA PolyA");
        expect(libraryRows[0]?.textContent).not.toContain("id:1001");
        expect(libraryRows[0]?.textContent).toContain("1001");
        expect(libraryRows[0]?.textContent).toContain("library_lims:");
        expect(libraryRows[0]?.textContent).toContain("DN111:A1");

        expect(laneRows).toHaveLength(1);
        expectEntityRowTitle(laneRows[0], "12345_2#88");
        expect(laneRows[0]?.textContent).not.toContain("id:12345_2#88");
        expect(laneRows[0]?.textContent).toContain("12345_2#88");
        expect(laneRows[0]?.textContent).toContain("id_run:");
        expect(laneRows[0]?.textContent).toContain("12345");
        expect(laneRows[0]?.textContent).toContain("lane:");
        expect(laneRows[0]?.textContent).toContain("2");
        expect(laneRows[0]?.textContent).toContain("tag_index:");
        expect(laneRows[0]?.textContent).toContain("88");
        expect(
            screen
                .getByRole("link", {
                    name: /send lane to search filter/i,
                })
                .getAttribute("href"),
        ).toBe("/?seqmeta_lane=12345_2#88");
    });

    it("renders library related samples as entity rows with metadata pairs", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_librarytype",
                rawValue: "RNA",
                enrichment: buildEnrichment({
                    identifier: "RNA",
                    type: "library_type",
                    graph: {
                        studies: [buildStudy()],
                        samples: [
                            buildSample({
                                id_sample_lims: "LIMS001",
                                sanger_id: "SANG001",
                                sample_name: "Sample 1",
                                accession_number: "ERS123456",
                            }),
                        ],
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const sampleRows = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelectorAll('[data-seqmeta-detail-key="sample"]');

        expect(sampleRows).toHaveLength(1);
        expectEntityRowTitle(sampleRows[0], "SANG001");
        expect(sampleRows[0]?.textContent).toContain("name:");
        expect(sampleRows[0]?.textContent).toContain("Sample 1");
        expect(sampleRows[0]?.textContent).not.toContain("id:SANG001");
        expect(sampleRows[0]?.textContent).toContain("SANG001");
        expect(sampleRows[0]?.textContent).toContain("sample_lims:");
        expect(sampleRows[0]?.textContent).toContain("LIMS001");
        expect(sampleRows[0]?.textContent).toContain("accession:");
        expect(sampleRows[0]?.textContent).toContain("ERS123456");
    });

    it("renders direct metadata for exact library-id details from related library identifiers", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_libraryid",
                rawValue: "71046409",
                enrichment: buildEnrichment({
                    identifier: "71046409",
                    type: "library_id",
                    graph: {
                        studies: [buildStudy({ id_study_lims: "7607" })],
                        samples: [
                            buildSample({
                                id_study_lims: "7607",
                                id_sample_lims: "9575305",
                                sanger_id: "7607STDY14643771",
                                sample_name: "7607STDY14643771",
                                library_type: "Custom",
                            }),
                        ],
                        libraries: [
                            {
                                library_type: "Custom",
                                id_study_lims: "7607",
                                library_id: "71046409",
                                id_library_lims: "SQPP-47463-G:B1",
                            },
                        ],
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const directMetadata = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="direct-metadata"]');

        expect(directMetadata).toBeTruthy();
        expect(
            directMetadata?.querySelectorAll(
                '[data-seqmeta-detail-key="seqmeta_library_id"]',
            ),
        ).toHaveLength(0);
        expect(
            directMetadata?.querySelectorAll(
                '[data-seqmeta-detail-key="seqmeta_id_library_lims"]',
            ),
        ).toHaveLength(1);
        expect(directMetadata?.textContent).not.toContain("71046409");
        expect(directMetadata?.textContent).toContain("SQPP-47463-G:B1");
        expect(directMetadata?.textContent).toContain("Custom");

        const titleActions = screen.getByTestId("seqmeta-title-actions");
        expect(
            titleActions.querySelector(
                '[aria-label="Copy seqmeta_library_id"]',
            ),
        ).toBeTruthy();
        expect(
            titleActions
                .querySelector(
                    '[aria-label="Send seqmeta_library_id to search filter"]',
                )
                ?.getAttribute("href"),
        ).toBe("/?seqmeta_library_id=71046409");
    });

    it("hides duplicate library-id direct metadata and keeps title copy and filter actions", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");
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
                createElement(MLWHBadge, {
                    metadataKey: "seqmeta_libraryid",
                    rawValue: "71046409",
                    enrichment: buildEnrichment({
                        identifier: "71046409",
                        type: "library_id",
                        graph: {
                            studies: [buildStudy({ id_study_lims: "7607" })],
                            samples: [
                                buildSample({
                                    id_study_lims: "7607",
                                    id_sample_lims: "9575305",
                                    sanger_id: "7607STDY14643771",
                                    sample_name: "7607STDY14643771",
                                    library_type: "Custom",
                                }),
                            ],
                            libraries: [
                                {
                                    library_type: "Custom",
                                    id_study_lims: "7607",
                                    library_id: "71046409",
                                    id_library_lims: "LIB7607-71046409",
                                },
                            ],
                        },
                    }),
                }),
            );

            fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            const dialogHeader = screen
                .getByText("MLWH details")
                .closest("div");
            expect(dialogHeader?.querySelector("h3")?.textContent).toBe(
                "71046409",
            );

            expect(
                dialogHeader?.querySelector(
                    '[aria-label="Copy seqmeta_library_id"]',
                ),
            ).toBeTruthy();
            const titleFilter = dialogHeader?.querySelector(
                '[aria-label="Send seqmeta_library_id to search filter"]',
            );
            expect(titleFilter?.getAttribute("href")).toBe(
                "/?seqmeta_library_id=71046409",
            );

            const directMetadata = screen
                .getByTestId("seqmeta-dialog-body")
                .querySelector('[data-field-group="direct-metadata"]');

            expect(directMetadata).toBeTruthy();
            expect(
                directMetadata?.querySelectorAll(
                    '[data-seqmeta-detail-key="seqmeta_library_id"]',
                ),
            ).toHaveLength(0);
            expect(
                directMetadata?.querySelectorAll(
                    '[data-seqmeta-detail-key="seqmeta_pipeline_id_lims"]',
                ),
            ).toHaveLength(1);
            expect(
                directMetadata?.querySelectorAll(
                    '[data-seqmeta-detail-key="seqmeta_id_library_lims"]',
                ),
            ).toHaveLength(1);

            const copyButton = dialogHeader?.querySelector(
                '[aria-label="Copy seqmeta_library_id"]',
            );
            fireEvent.click(copyButton as Element);

            await waitFor(() => {
                expect(writeTextMock).toHaveBeenCalledWith("71046409");
            });
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

    it.each([
        {
            metadataKey: "seqmeta_id_sample_lims",
            rawValue: "6050954",
            suppressedDetailKey: "seqmeta_id_sample_lims",
            expectedHref: "/?seqmeta_id_sample_lims=6050954",
            sample: {
                id_sample_lims: "6050954",
                sanger_id: "9575305",
                sample_name: "7607STDY14643771",
            },
        },
        {
            metadataKey: "seqmeta_sanger_sample_id",
            rawValue: "9575305",
            suppressedDetailKey: "seqmeta_sanger_sample_id",
            expectedHref: "/?seqmeta_sanger_sample_id=9575305",
            sample: {
                id_sample_lims: "6050954",
                sanger_id: "9575305",
                sample_name: "7607STDY14643771",
            },
        },
    ])(
        "keeps $metadataKey title filter on the direct metadata key when the duplicate direct row is hidden",
        async ({
            expectedHref,
            metadataKey,
            rawValue,
            sample,
            suppressedDetailKey,
        }) => {
            const { MLWHBadge } = await import("@/components/mlwh-badge");

            render(
                createElement(MLWHBadge, {
                    metadataKey,
                    rawValue,
                    enrichment: buildEnrichment({
                        identifier: rawValue,
                        type: "sanger_sample_id",
                        graph: {
                            sample: buildSample(sample),
                            sample_detail: buildSampleDetail({ sample }),
                        },
                    }),
                }),
            );

            fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            const directMetadata = screen
                .getByTestId("seqmeta-dialog-body")
                .querySelector('[data-field-group="direct-metadata"]');

            expect(directMetadata).toBeTruthy();
            expect(
                directMetadata?.querySelector(
                    `[data-seqmeta-detail-key="${suppressedDetailKey}"]`,
                ),
            ).toBeNull();
            expect(
                screen
                    .getByTestId("seqmeta-title-actions")
                    .querySelector(
                        `[aria-label="Send ${metadataKey} to search filter"]`,
                    )
                    ?.getAttribute("href"),
            ).toBe(expectedHref);
        },
    );

    it("omits the direct metadata section when only the selected title value would remain", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_librarytype",
                rawValue: "Custom",
                enrichment: buildEnrichment({
                    identifier: "Custom",
                    type: "library_type",
                    graph: {
                        studies: [buildStudy({ id_study_lims: "7607" })],
                        samples: [
                            buildSample({
                                id_study_lims: "7607",
                                id_sample_lims: "9575305",
                                sanger_id: "7607STDY14643771",
                                sample_name: "7607STDY14643771",
                                library_type: "Custom",
                            }),
                        ],
                        libraries: [
                            {
                                library_type: "Custom",
                                id_study_lims: "7607",
                            },
                        ],
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(
            screen
                .getByTestId("seqmeta-dialog-body")
                .querySelector('[data-field-group="direct-metadata"]'),
        ).toBeNull();
        expect(screen.getByTestId("seqmeta-title-actions")).toBeTruthy();
        expect(
            screen
                .getByTestId("seqmeta-title-actions")
                .querySelector(
                    '[aria-label="Send seqmeta_pipeline_id_lims to search filter"]',
                )
                ?.getAttribute("href"),
        ).toBe("/?library=Custom");
        expect(screen.getByText("Related Data")).toBeTruthy();
    });

    it("filters sample related library rows by library id when the id is only on sample detail libraries", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");
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
                createElement(MLWHBadge, {
                    metadataKey: "seqmeta_sampleid",
                    rawValue: "7607STDY14643771",
                    enrichment: buildEnrichment({
                        identifier: "7607STDY14643771",
                        type: "sanger_sample_id",
                        graph: {
                            study: buildStudy({
                                id_study_lims: "7607",
                                name: "Study 7607",
                            }),
                            sample: buildSample({
                                id_study_lims: "7607",
                                id_sample_lims: "9575305",
                                sanger_id: "7607STDY14643771",
                                sample_name: "7607STDY14643771",
                                library_type: "Custom",
                            }),
                            sample_detail: buildSampleDetail({
                                sample: {
                                    id_study_lims: "7607",
                                    id_sample_lims: "9575305",
                                    sanger_id: "7607STDY14643771",
                                    sample_name: "7607STDY14643771",
                                    library_type: "Custom",
                                },
                                libraries: [
                                    {
                                        library_type: "Custom",
                                        id_study_lims: "7607",
                                        library_id: "71046409",
                                        id_library_lims: "SQPP-47463-G:B1",
                                    },
                                ],
                                lanes: [],
                            }),
                        },
                    }),
                }),
            );

            fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            const libraryRow = screen
                .getByTestId("seqmeta-dialog-body")
                .querySelector('[data-seqmeta-detail-key="library"]');

            expect(libraryRow).toBeTruthy();
            expectEntityRowTitle(libraryRow, "71046409");
            expect(libraryRow?.textContent).not.toContain("id:71046409");
            expect(libraryRow?.textContent).toContain("71046409");
            expect(libraryRow?.textContent).toContain("library_lims:");
            expect(libraryRow?.textContent).toContain("SQPP-47463-G:B1");
            expect(
                screen
                    .getByRole("link", {
                        name: /send library to search filter/i,
                    })
                    .getAttribute("href"),
            ).toBe("/?seqmeta_library_id=71046409");

            fireEvent.click(
                screen.getByLabelText(/Copy seqmeta_pipeline_id_lims/i),
            );

            await waitFor(() => {
                expect(writeTextMock).toHaveBeenCalledWith("71046409");
            });
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

    it("does not emit duplicate-key warnings when expanded library samples share sample IDs", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

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
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        fireEvent.click(screen.getByLabelText("Show samples"));

        await waitFor(() => {
            expect(screen.getAllByText("S1")).toHaveLength(2);
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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

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
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
            expect(screen.getByText("S1")).toBeTruthy();
        });

        expect(fetchLibrarySamplesMock).toHaveBeenCalledWith("6568", "RNA");
    });

    it("lists samples individually for library detail and includes parent study", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should have a Samples section
        expect(screen.getByText("Samples")).toBeTruthy();

        // Each sample should be on its own row with display name
        expect(screen.getByText("S1")).toBeTruthy();
        expect(screen.getByText("S2")).toBeTruthy();

        // Should have a Study section showing the parent study
        expect(screen.getByText("Study")).toBeTruthy();
        expect(screen.getByText("RNA Seq")).toBeTruthy();
    });

    it("shows all direct metadata fields for a sample, not just sampleid", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
            .getByText("MLWH details")
            .parentElement?.querySelector("h3");
        expect(dialogTitle?.textContent).toBe("WTSI_wEMB10524782");

        // But it should show other sample fields that don't duplicate the title
        expect(screen.getByText("Sample LIMS ID")).toBeTruthy();
        expect(screen.getByText("6050954")).toBeTruthy();
        expect(screen.getByText("Sample accession")).toBeTruthy();
        expect(screen.getByText("EGAN00003258234")).toBeTruthy();
    });

    it("displays legacy sample metadata details with MLWH field names and no alias/type subtitle", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "7607STDY14643771",
                enrichment: buildEnrichment({
                    identifier: "7607STDY14643771",
                    type: "sanger_sample_name",
                    graph: {
                        sample: buildSample({
                            sanger_id: "9575305",
                            sample_name: "7607STDY14643771",
                            id_sample_lims: "6050954",
                        }),
                        sample_detail: buildSampleDetail({
                            sample: {
                                sanger_id: "9575305",
                                sample_name: "7607STDY14643771",
                                id_sample_lims: "6050954",
                            },
                        }),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(screen.getByText("seqmeta_sample_name")).toBeTruthy();
        expect(
            screen.queryByText("seqmeta_sampleid (sanger_sample_name)"),
        ).toBeNull();

        const directMetadataSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="direct-metadata"]');
        expect(directMetadataSection).toBeTruthy();
        expect(
            directMetadataSection?.querySelector(
                '[data-seqmeta-detail-key="seqmeta_sampleid"]',
            ),
        ).toBeNull();

        const sangerSampleIDRow = directMetadataSection?.querySelector(
            '[data-seqmeta-detail-key="seqmeta_sanger_sample_id"]',
        );
        expect(sangerSampleIDRow?.textContent).toContain("Sanger sample ID");
        expect(sangerSampleIDRow?.textContent).toContain("9575305");
    });

    it("keeps canonical direct metadata keys in label hover text instead of a visible second line", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "7607STDY14643771",
                enrichment: buildEnrichment({
                    identifier: "7607STDY14643771",
                    type: "sanger_sample_name",
                    graph: {
                        sample: buildSample({
                            sanger_id: "9575305",
                            sample_name: "7607STDY14643771",
                            id_sample_lims: "6050954",
                        }),
                        sample_detail: buildSampleDetail({
                            sample: {
                                sanger_id: "9575305",
                                sample_name: "7607STDY14643771",
                                id_sample_lims: "6050954",
                            },
                        }),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const directMetadataSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="direct-metadata"]');
        const sangerSampleIDRow = directMetadataSection?.querySelector(
            '[data-seqmeta-detail-key="seqmeta_sanger_sample_id"]',
        );
        expect(sangerSampleIDRow).toBeTruthy();
        expect(
            within(sangerSampleIDRow as HTMLElement).getByText(
                "Sanger sample ID",
            ),
        ).toBeTruthy();
        expect(sangerSampleIDRow?.textContent).toContain("9575305");
        expect(sangerSampleIDRow?.textContent).not.toContain(
            "seqmeta_sanger_sample_id",
        );

        const label = sangerSampleIDRow?.querySelector(
            '[data-testid="seqmeta-direct-metadata-label"]',
        );
        expect(label?.getAttribute("title")).toBe(
            "MLWH metadata key: seqmeta_sanger_sample_id",
        );
    });

    it("uses the combined Sample search key for supplier-name direct metadata filters", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "7607STDY14643771",
                enrichment: buildEnrichment({
                    identifier: "7607STDY14643771",
                    type: "sanger_sample_name",
                    graph: {
                        sample: buildSample({
                            sample_name: "7607STDY14643771",
                            supplier_name: "Hek_R1",
                        }),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const supplierRow = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-seqmeta-detail-key="seqmeta_supplier_name"]');
        const filterLink = within(supplierRow as HTMLElement).getByLabelText(
            "Send seqmeta_supplier_name to search filter",
        );

        expect(filterLink.getAttribute("href")).toBe("/?sample=Hek_R1");
    });

    it("labels supplier-backed sample dialogs with the source-specific supplier key and hides the duplicate supplier-name direct row", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_supplier_name",
                rawValue: "Hek_R1",
                enrichment: buildEnrichment({
                    identifier: "Hek_R1",
                    type: "supplier_name",
                    graph: {
                        sample: buildSample({
                            id_sample_lims: "SMP7607-0000",
                            sanger_id: "7607STDY14643771",
                            sample_name: "7607STDY14643771",
                            supplier_name: "Hek_R1",
                            accession_number: "SAMEA76070",
                        }),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const dialogHeader = screen.getByText("MLWH details").closest("div");
        const titleLabels = Array.from(
            dialogHeader?.querySelectorAll("p") ?? [],
        ).map((label) => label.textContent);

        expect(dialogHeader?.querySelector("h3")?.textContent).toBe("Hek_R1");
        expect(titleLabels).toContain("seqmeta_supplier_name");
        expect(titleLabels).not.toContain("seqmeta_sample_name");
        expect(
            screen.getByRole("button", {
                name: "Open seqmeta_supplier_name details",
            }),
        ).toBeTruthy();
        expect(
            screen
                .getByTestId("seqmeta-title-actions")
                .querySelector('[aria-label="Copy seqmeta_supplier_name"]'),
        ).toBeTruthy();
        expect(
            screen
                .getByTestId("seqmeta-title-actions")
                .querySelector(
                    '[aria-label="Send seqmeta_supplier_name to search filter"]',
                )
                ?.getAttribute("href"),
        ).toBe("/?sample=Hek_R1");

        const directMetadataSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="direct-metadata"]');

        expect(directMetadataSection).toBeTruthy();
        expect(
            directMetadataSection?.querySelector(
                '[data-seqmeta-detail-key="seqmeta_supplier_name"]',
            ),
        ).toBeNull();
        expect(
            directMetadataSection?.querySelector(
                '[data-seqmeta-detail-key="seqmeta_sample_name"]',
            )?.textContent,
        ).toContain("7607STDY14643771");
        expect(
            directMetadataSection?.querySelector(
                '[data-seqmeta-detail-key="seqmeta_sanger_sample_id"]',
            )?.textContent,
        ).toContain("7607STDY14643771");
        expect(
            directMetadataSection?.querySelector(
                '[data-seqmeta-detail-key="seqmeta_id_sample_lims"]',
            )?.textContent,
        ).toContain("SMP7607-0000");
        expect(
            directMetadataSection?.querySelector(
                '[data-seqmeta-detail-key="seqmeta_accession_number"]',
            )?.textContent,
        ).toContain("SAMEA76070");
    });

    it("shows hierarchical related data for sample with library parent, study grandparent, and lanes", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        // Click on a study ID field - rawValue is the study ID
        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const dialogTitle = screen
            .getByText("MLWH details")
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
                '[data-seqmeta-detail-key="seqmeta_id_study_lims"]',
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

    it("keeps the study accession detail row when a study-accession title has the same value", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
                metadataKey: "seqmeta_study_accession",
                rawValue: "EGAS00001005445",
                enrichment: buildEnrichment({
                    identifier: "EGAS00001005445",
                    type: "study_id",
                    graph: {
                        study: buildStudy({
                            accession_number: "EGAS00001005445",
                        }),
                    },
                }),
            }),
        );

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        expect(
            screen
                .getByText("MLWH details")
                .parentElement?.querySelector("p.font-mono")?.textContent,
        ).toBe("seqmeta_study_accession");

        const studyAccessionRow = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector(
                '[data-seqmeta-detail-key="seqmeta_accession_number"]',
            );

        expect(studyAccessionRow).toBeTruthy();
        expect(studyAccessionRow?.textContent).toContain("Study accession");
        expect(studyAccessionRow?.textContent).toContain("EGAS00001005445");
    });

    it("does not duplicate 'Lane' label in rows within Lanes section", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        expect(screen.getByText("SANG001")).toBeTruthy();
        expect(screen.getByText("SANG002")).toBeTruthy();
    });

    it("does not duplicate 'Library type' label in rows within Library section", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

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
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
            .querySelectorAll(
                '[data-seqmeta-detail-key="seqmeta_pipeline_id_lims"]',
            );
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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

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
                createElement(MLWHBadge, {
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

            fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        expect(screen.getByText("SANG_SC_001")).toBeTruthy();
        expect(screen.getByText("SANG_SC_002")).toBeTruthy();
    });

    it("marks only the clicked library row copy button as Copied", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");
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
                createElement(MLWHBadge, {
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

            fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            const copyButtons = screen.getAllByLabelText(
                /Copy seqmeta_pipeline_id_lims/i,
            );
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
        const { MLWHBadge } = await import("@/components/mlwh-badge");
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
                createElement(MLWHBadge, {
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

            fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            expect(screen.getByText("S1")).toBeTruthy();

            const copyButtons = screen.getAllByLabelText(
                /Copy seqmeta_sample_name/i,
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
        const { MLWHBadge } = await import("@/components/mlwh-badge");
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
                createElement(MLWHBadge, {
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

            fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

            await waitFor(() => {
                expect(screen.getByRole("dialog")).toBeTruthy();
            });

            fireEvent.click(
                screen.getByRole("button", {
                    name: /copy seqmeta_id_study_lims/i,
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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

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
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
            expect(screen.getByText("S1")).toBeTruthy();
        });
    });

    it("does not show linked_samples field for study metadata with study_detail", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

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
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
            expect(screen.getByText("S1")).toBeTruthy();
            expect(screen.getByText("S2")).toBeTruthy();
            expect(screen.getByText("S3")).toBeTruthy();
        });
    });

    it("does not render legacy sample or library rows for study metadata without study_detail", async () => {
        const { MLWHBadge } = await import("@/components/mlwh-badge");

        render(
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

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
        const { MLWHBadge } = await import("@/components/mlwh-badge");

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
            createElement(MLWHBadge, {
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

        fireEvent.click(screen.getByTestId("mlwh-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Expand library to show samples
        const expandButton = screen.getByLabelText("Show samples");
        fireEvent.click(expandButton);

        await waitFor(() => {
            expect(screen.getByText("S1")).toBeTruthy();
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
        expectEntityRowTitle(firstSampleRow, "S1");
        expect(firstSampleRow?.textContent).toContain("name:Sample 1");

        // Second sample row should contain both name and ID in one row
        const secondSampleRow = sampleRows[1];
        expectEntityRowTitle(secondSampleRow, "S2");
        expect(secondSampleRow?.textContent).toContain("name:Sample 2");

        // Each sample row should have copy and filter buttons
        const copyButtons = Array.from(sampleRows).map((row) =>
            within(row as HTMLElement).getByLabelText(
                /Copy seqmeta_sample_name/i,
            ),
        );
        const filterButtons = screen.getAllByLabelText(
            /Send sample to search filter/i,
        );
        expect(copyButtons.length).toBe(2);
        expect(filterButtons.length).toBe(2);
    });
});
