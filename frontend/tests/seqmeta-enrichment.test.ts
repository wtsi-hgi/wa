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

import type { EnrichmentResult } from "@/lib/contracts";
import { SeqmetaCacheProvider } from "@/lib/seqmeta-cache";

const enrichIdentifierMock = vi.fn();

vi.mock("@/app/(results)/actions", () => ({
    enrichIdentifier: enrichIdentifierMock,
}));

function buildEnrichmentResult(
    overrides: Partial<EnrichmentResult> = {},
): EnrichmentResult {
    return {
        identifier: "6568",
        type: "study_id",
        graph: {
            study: {
                id_study_lims: "6568",
                name: "Cancer Programme",
            },
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

    it("shows the study name without a banner for a full study enrichment", async () => {
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
                "Cancer Programme",
            );
        });
        expect(screen.queryByText("study_id: 6568")).toBeNull();
        expect(screen.queryByText("Some details unavailable")).toBeNull();
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
                "Cancer Programme",
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
                    study: {
                        id_study_lims: "7777",
                        name: "Replacement Study",
                    },
                    libraries: [],
                    samples: [],
                },
            }),
        );

        await waitFor(() => {
            expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
                "Replacement Study",
            );
        });

        firstPending.resolve(buildEnrichmentResult());

        await waitFor(() => {
            expect(screen.getByTestId("seqmeta-badge-label").textContent).toBe(
                "Replacement Study",
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
});
