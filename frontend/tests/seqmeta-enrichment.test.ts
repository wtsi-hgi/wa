/**
 * @vitest-environment jsdom
 */

import { createElement } from "react";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EnrichmentResult, EnrichmentStudy } from "@/lib/contracts";
import { SeqmetaCacheProvider } from "@/lib/seqmeta-cache";

const enrichIdentifierMock = vi.fn();
const fetchStudyLibrarySamplesMock = vi.fn();

vi.mock("@/app/(results)/actions", () => ({
    enrichIdentifier: enrichIdentifierMock,
    fetchStudyLibrarySamples: fetchStudyLibrarySamplesMock,
}));

function buildEnrichmentStudy(
    overrides: Partial<EnrichmentStudy> = {},
): EnrichmentStudy {
    return {
        id_study_tmp: 6568,
        id_lims: "SQSCP",
        id_study_lims: "6568",
        name: "Cancer Programme",
        faculty_sponsor: "Faculty Sponsor",
        state: "active",
        abstract: "Study abstract",
        abbreviation: "CP",
        accession_number: "ERP123456",
        description: "Study description",
        data_release_strategy: "open",
        study_title: "Cancer Programme",
        data_access_group: "public",
        hmdmc_number: "",
        programme: "Cancer",
        created: "2026-04-30",
        reference_genome: "GRCh38",
        ethically_approved: true,
        study_type: "Genomic sequencing",
        contains_human_dna: true,
        contaminated_human_dna: false,
        study_visibility: "public",
        ega_dac_accession_number: "",
        ega_policy_accession_number: "",
        data_release_timing: "standard",
        ...overrides,
    };
}

function buildEnrichmentResult(
    overrides: Partial<EnrichmentResult> = {},
): EnrichmentResult {
    return {
        identifier: "6568",
        type: "study_id",
        graph: {
            study: buildEnrichmentStudy(),
            libraries: [
                {
                    library_type: "RNA",
                    id_study_lims: "6568",
                },
            ],
            samples: [
                {
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

describe("H3 enrichment state and badge", () => {
    beforeEach(() => {
        vi.resetModules();
        enrichIdentifierMock.mockReset();
        fetchStudyLibrarySamplesMock.mockReset();
    });

    afterEach(() => {
        cleanup();
    });

    async function openSeqmetaDetails() {
        await waitFor(() => {
            expect(screen.getByTestId("seqmeta-badge-trigger")).toBeTruthy();
        });

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });
    }

    it("shows the raw study ID in the badge even with enrichment available", async () => {
        enrichIdentifierMock.mockResolvedValue(buildEnrichmentResult());
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_studyid: "6568" },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
                "6568",
            );
        });
        expect(screen.queryByText("Some details unavailable")).toBeNull();
    });

    it("loads study library samples through server action for JIT expansion", async () => {
        const { fetchLibrarySamples } = await import("@/lib/seqmeta-enrichment");

        const samples = [
            {
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
        ];
        fetchStudyLibrarySamplesMock.mockResolvedValue(samples);

        await expect(fetchLibrarySamples("6568", "RNA")).resolves.toEqual(
            samples,
        );
        expect(fetchStudyLibrarySamplesMock).toHaveBeenCalledWith(
            "6568",
            "RNA",
        );
        expect(fetchStudyLibrarySamplesMock).toHaveBeenCalledTimes(1);
    });

    it("reuses a resolved enrichment for related sample values while looking up libraries independently", async () => {
        enrichIdentifierMock
            .mockResolvedValueOnce(buildEnrichmentResult())
            .mockResolvedValueOnce(
                buildEnrichmentResult({
                    identifier: "RNA",
                    type: "library_type",
                    graph: {
                        libraries: [
                            {
                                library_type: "RNA",
                                id_study_lims: "6568",
                            },
                        ],
                        samples: buildEnrichmentResult().graph.samples,
                    },
                }),
            );
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                metadata: {
                    seqmeta_studyid: "6568",
                    seqmeta_sampleid: "SANG001",
                    seqmeta_library: "RNA",
                },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            const studyRow = document.querySelector(
                '[data-metadata-row="seqmeta_studyid"] [data-testid="seqmeta-badge-label"]',
            );

            expect(studyRow?.textContent).toBe("6568");
        });

        expect(enrichIdentifierMock).toHaveBeenCalledTimes(2);
        expect(enrichIdentifierMock).toHaveBeenNthCalledWith(1, "6568");
        expect(enrichIdentifierMock).toHaveBeenNthCalledWith(2, "RNA");

        const sampleRow = document.querySelector(
            '[data-metadata-row="seqmeta_sampleid"] [data-testid="seqmeta-badge-label"]',
        );
        const libraryRow = document.querySelector(
            '[data-metadata-row="seqmeta_library"] [data-testid="seqmeta-badge-label"]',
        );

        expect(sampleRow?.textContent).toBe("SANG001");
        expect(libraryRow?.textContent).toBe("RNA");
    });

    it("prefers specific seqmeta identifiers over seqmeta_library when choosing the first lookup", async () => {
        enrichIdentifierMock.mockResolvedValue(buildEnrichmentResult());
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                metadata: {
                    seqmeta_library: "RNA",
                    seqmeta_studyid: "6568",
                    seqmeta_sampleid: "SANG001",
                },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            const studyRow = document.querySelector(
                '[data-metadata-row="seqmeta_studyid"] [data-testid="seqmeta-badge-label"]',
            );

            expect(studyRow?.textContent).toBe("6568");
        });

        expect(enrichIdentifierMock).toHaveBeenCalledTimes(2);
        expect(enrichIdentifierMock).toHaveBeenNthCalledWith(1, "6568");
        expect(enrichIdentifierMock).toHaveBeenNthCalledWith(2, "RNA");
    });

    it("looks up an amplicon library independently instead of showing sibling study libraries", async () => {
        enrichIdentifierMock
            .mockResolvedValueOnce(
                buildEnrichmentResult({
                    identifier: "4861",
                    type: "study_id",
                    graph: {
                        study: buildEnrichmentStudy({
                            id_study_tmp: 4861,
                            id_study_lims: "4861",
                            name: "Amplicon study",
                        }),
                        libraries: [
                            {
                                library_type: "Chromium single cell",
                                id_study_lims: "4861",
                            },
                            {
                                library_type: "Chromium single cell 3 prime v3",
                                id_study_lims: "4861",
                            },
                            {
                                library_type: "Chromium single cell ATAC",
                                id_study_lims: "4861",
                            },
                        ],
                        samples: [
                            {
                                id_study_lims: "4861",
                                id_sample_lims: "3990641",
                                sanger_id: "4861STDY7771117",
                                sample_name: "Amplicon sample",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "Chromium single cell",
                                id_run: 9876,
                                lane: 1,
                                tag_index: 1,
                                irods_path: "/seq/9876",
                                study_accession_number: "ERP7771117",
                                accession_number: "ERS7771117",
                            },
                        ],
                    },
                }),
            )
            .mockResolvedValueOnce(
                buildEnrichmentResult({
                    identifier: "Chromium single cell",
                    type: "library_type",
                    graph: {
                        libraries: [
                            {
                                library_type: "Chromium single cell",
                                id_study_lims: "4861",
                            },
                        ],
                        samples: [
                            {
                                id_study_lims: "4861",
                                id_sample_lims: "3990641",
                                sanger_id: "4861STDY7771117",
                                sample_name: "Amplicon sample",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "Chromium single cell",
                                id_run: 9876,
                                lane: 1,
                                tag_index: 1,
                                irods_path: "/seq/9876",
                                study_accession_number: "ERP7771117",
                                accession_number: "ERS7771117",
                            },
                        ],
                        studies: [
                            buildEnrichmentStudy({
                                id_study_tmp: 4861,
                                id_study_lims: "4861",
                                name: "Amplicon study",
                            }),
                        ],
                    },
                }),
            );
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                metadata: {
                    seqmeta_studyid: "4861",
                    seqmeta_sampleid: "4861STDY7771117",
                    seqmeta_sample_lims: "3990641",
                    seqmeta_library: "Chromium single cell",
                },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            expect(enrichIdentifierMock).toHaveBeenCalledTimes(2);
        });
        expect(enrichIdentifierMock).toHaveBeenNthCalledWith(1, "4861");
        expect(enrichIdentifierMock).toHaveBeenNthCalledWith(
            2,
            "Chromium single cell",
        );

        const libraryTrigger = document.querySelector(
            '[data-metadata-row="seqmeta_library"] [data-testid="seqmeta-badge-trigger"]',
        );
        expect(libraryTrigger).toBeTruthy();
        fireEvent.click(libraryTrigger as Element);

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });
        expect(
            screen.queryByText("Chromium single cell 3 prime v3"),
        ).toBeNull();
        expect(screen.queryByText("Chromium single cell ATAC")).toBeNull();
    });

    it("does not start a duplicate enrichment request when the component rerenders while the first request is in flight", async () => {
        const pending = deferred<EnrichmentResult | null>();
        enrichIdentifierMock.mockReturnValue(pending.promise);
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        const rendered = render(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_studyid: "6568" },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        rendered.rerender(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_studyid: "6568" },
            }),
        );

        expect(enrichIdentifierMock).toHaveBeenCalledTimes(1);

        pending.resolve(buildEnrichmentResult());

        await waitFor(() => {
            expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
                "6568",
            );
        });
    });

    it("keeps the latest enrichment visible when an older request resolves after metadata changes", async () => {
        const firstPending = deferred<EnrichmentResult | null>();
        const secondPending = deferred<EnrichmentResult | null>();
        enrichIdentifierMock
            .mockReturnValueOnce(firstPending.promise)
            .mockReturnValueOnce(secondPending.promise);

        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        const rendered = render(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_studyid: "6568" },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        rendered.rerender(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_studyid: "7777" },
            }),
        );

        secondPending.resolve(
            buildEnrichmentResult({
                identifier: "7777",
                graph: {
                    study: buildEnrichmentStudy({
                        id_study_tmp: 7777,
                        id_study_lims: "7777",
                        name: "Replacement Study",
                    }),
                    libraries: [],
                    samples: [],
                },
            }),
        );

        await waitFor(() => {
            expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
                "7777",
            );
        });

        firstPending.resolve(buildEnrichmentResult());

        await waitFor(() => {
            expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
                "7777",
            );
        });
    });

    it("shows the truncated-samples banner text for partial enrichment", async () => {
        enrichIdentifierMock.mockResolvedValue(
            buildEnrichmentResult({
                identifier: "RNA",
                type: "library_type",
                partial: true,
                missing: [
                    {
                        hop: "samples",
                        reason: "samples_truncated",
                        status: 200,
                    },
                ],
            }),
        );
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_library: "RNA" },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await openSeqmetaDetails();

        await waitFor(() => {
            expect(screen.getByText("Showing first 1000 samples")).toBeTruthy();
        });
    });

    it("shows the study-unavailable banner text for upstream partial failures", async () => {
        enrichIdentifierMock.mockResolvedValue(
            buildEnrichmentResult({
                identifier: "SANG001",
                type: "sanger_sample_id",
                partial: true,
                missing: [
                    {
                        hop: "study",
                        reason: "upstream_error",
                        status: 502,
                    },
                ],
            }),
        );
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_sampleid: "SANG001" },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await openSeqmetaDetails();

        await waitFor(() => {
            expect(screen.getByText("Study record unavailable")).toBeTruthy();
        });
    });

    it("shows the unavailable marker when enrichment resolves to null", async () => {
        enrichIdentifierMock.mockResolvedValue(null);
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_studyid: "6568" },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            expect(
                screen.getByLabelText("enrichment unavailable"),
            ).toBeTruthy();
        });
    });

    it("shows the impaired marker when enrichment rejects with a 502 backend error", async () => {
        const { BackendRequestError } = await import("@/lib/backend-client");
        enrichIdentifierMock.mockRejectedValue(
            new BackendRequestError(502, {
                error: "seqmeta: all enrichment hops failed",
            }),
        );
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_studyid: "6568" },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await waitFor(() => {
            expect(
                screen.getByLabelText("enrichment backend impaired"),
            ).toBeTruthy();
        });

        await openSeqmetaDetails();

        expect(
            screen.getByText(
                "Upstream services were unavailable while resolving this study identifier value.",
            ),
        ).toBeTruthy();
    });

    it("groups direct metadata and related data with section headers in the popup", async () => {
        enrichIdentifierMock.mockResolvedValue(
            buildEnrichmentResult({
                identifier: "WTSI_wEMB10524782",
                type: "sanger_sample_id",
                graph: {
                    study: buildEnrichmentStudy({
                        id_study_tmp: 6568,
                        id_study_lims: "6568",
                        name: "Cancer Programme",
                        description: "Study description",
                    }),
                    sample: {
                        id_sample_lims: "2153063",
                        sanger_id: "WTSI_wEMB10524782",
                        sample_name: "Sample A",
                        accession_number: "ERS123456",
                        taxon_id: 9606,
                        common_name: "Human",
                        library_type: "RNA PolyA",
                        id_run: 9876,
                        lane: 1,
                        tag_index: 1,
                        irods_path: "/seq/9876",
                        study_accession_number: "ERP123456",
                        id_study_lims: "6568",
                    },
                    libraries: [
                        {
                            library_type: "RNA PolyA",
                            id_study_lims: "6568",
                        },
                    ],
                },
            }),
        );
        const { ResultMetadataEnrichment } =
            await import("@/components/result-metadata-enrichment");

        render(
            createElement(ResultMetadataEnrichment, {
                metadata: { seqmeta_sampleid: "WTSI_wEMB10524782" },
            }),
            {
                wrapper: ({ children }) =>
                    createElement(SeqmetaCacheProvider, null, children),
            },
        );

        await openSeqmetaDetails();

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const directMetadataSection = screen.getByText("Direct Metadata");
        const relatedDataSection = screen.getByText("Related Data");

        expect(directMetadataSection).toBeTruthy();
        expect(relatedDataSection).toBeTruthy();

        const directFields = directMetadataSection
            .closest("[data-field-group]")
            ?.querySelectorAll("[data-seqmeta-detail-key]");

        const relatedFields = relatedDataSection
            .closest("[data-field-group]")
            ?.querySelectorAll("[data-seqmeta-detail-key]");

        expect(directFields && directFields.length > 0).toBe(true);
        expect(relatedFields && relatedFields.length > 0).toBe(true);

        const directKeys = Array.from(directFields ?? [])
            .map((el) => el.getAttribute("data-seqmeta-detail-key"))
            .filter(Boolean);

        const relatedKeys = Array.from(relatedFields ?? [])
            .map((el) => el.getAttribute("data-seqmeta-detail-key"))
            .filter(Boolean);

        expect(directKeys).toContain("sample_name");
        expect(directKeys).toContain("seqmeta_sample_lims");
        expect(directKeys).toContain("sample_accession_number");

        expect(relatedKeys).toContain("study_name");
        expect(relatedKeys).toContain("study_id");
        expect(relatedKeys).toContain("seqmeta_library");
    });
});
