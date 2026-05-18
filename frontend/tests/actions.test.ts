import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EnrichmentResult } from "@/lib/contracts";

const seqmetaJsonMock = vi.fn();

vi.mock("@/lib/backend-client", async () => {
    const actual = await vi.importActual<typeof import("@/lib/backend-client")>(
        "@/lib/backend-client",
    );

    return {
        ...actual,
        seqmetaJson: seqmetaJsonMock,
    };
});

vi.mock("@/lib/studies-cache", () => ({
    getStudies: vi.fn(),
}));

const enrichmentFixture: EnrichmentResult = {
    identifier: "6568",
    type: "study_id",
    graph: {
        study: {
            id_study_tmp: 42,
            id_lims: "SQSCP",
            id_study_lims: "6568",
            name: "Cancer Programme",
            faculty_sponsor: "Dr Example",
            state: "active",
            accession_number: "ERP123456",
            data_release_strategy: "managed",
            study_title: "Cancer Programme Cohort",
            data_access_group: "group-a",
            programme: "Cancer",
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
    },
    partial: false,
};

describe("H2 enrichIdentifier action", () => {
    beforeEach(() => {
        vi.resetModules();
        seqmetaJsonMock.mockReset();
    });

    afterEach(() => {
        vi.unstubAllEnvs();
    });

    it("returns the enrichment result for a valid identifier", async () => {
        seqmetaJsonMock.mockResolvedValue(enrichmentFixture);

        const { enrichIdentifier } = await import("@/app/(results)/actions");

        await expect(enrichIdentifier("6568")).resolves.toMatchObject({
            type: "study_id",
            partial: false,
        });
        expect(seqmetaJsonMock).toHaveBeenCalledWith(
            "/enrich/6568",
            expect.anything(),
        );
    });

    it("returns null when the backend responds with 404", async () => {
        const { BackendRequestError } = await import("@/lib/backend-client");
        seqmetaJsonMock.mockRejectedValue(
            new BackendRequestError(404, {
                error: "seqmeta: unknown identifier",
            }),
        );

        const { enrichIdentifier } = await import("@/app/(results)/actions");

        await expect(enrichIdentifier("missing-id")).resolves.toBeNull();
    });

    it("preserves BackendRequestError for 502 upstream-impaired responses", async () => {
        const { BackendRequestError } = await import("@/lib/backend-client");
        seqmetaJsonMock.mockRejectedValue(
            new BackendRequestError(502, {
                error: "seqmeta: all enrichment hops failed",
                missing: [
                    {
                        hop: "classify",
                        reason: "upstream_error",
                        status: 502,
                    },
                ],
            }),
        );

        const { enrichIdentifier } = await import("@/app/(results)/actions");

        await expect(enrichIdentifier("flaky-id")).rejects.toMatchObject({
            status: 502,
        });
    });

    it("enriches multiple identifiers through one batched server action result", async () => {
        const { BackendRequestError } = await import("@/lib/backend-client");
        seqmetaJsonMock
            .mockResolvedValueOnce(enrichmentFixture)
            .mockRejectedValueOnce(
                new BackendRequestError(404, {
                    error: "seqmeta: unknown identifier",
                }),
            )
            .mockRejectedValueOnce(
                new BackendRequestError(502, {
                    error: "seqmeta: all enrichment hops failed",
                }),
            );

        const { enrichIdentifiers } = await import("@/app/(results)/actions");

        await expect(
            enrichIdentifiers(["6568", "missing-id", "flaky-id", "6568"]),
        ).resolves.toEqual([
            {
                value: "6568",
                enrichment: enrichmentFixture,
                error: undefined,
            },
            {
                value: "missing-id",
                enrichment: null,
                error: "not_found",
            },
            {
                value: "flaky-id",
                enrichment: null,
                error: "upstream_impaired",
            },
        ]);
        expect(seqmetaJsonMock).toHaveBeenCalledTimes(3);
    });

    it("reuses aliases from the first enrichment inside a batch", async () => {
        const studyEnrichment: EnrichmentResult = {
            ...enrichmentFixture,
            identifier: "7607",
            graph: {
                ...enrichmentFixture.graph,
                study: {
                    ...enrichmentFixture.graph.study!,
                    id_study_lims: "7607",
                    accession_number: "ERP7607",
                },
                sample: {
                    id_study_lims: "7607",
                    id_sample_lims: "SMP7607-0001",
                    sanger_id: "7607STDY14643771",
                    sample_name: "7607STDY14643771",
                    taxon_id: 9606,
                    common_name: "Human",
                    library_type: "Custom",
                    accession_number: "SAMEA7607",
                    id_run: 48522,
                    study_accession_number: "ERP7607",
                },
                library: {
                    library_type: "Custom",
                    id_study_lims: "7607",
                    library_id: "71046409",
                    id_library_lims: "LIB7607-71046409",
                },
            },
        };
        seqmetaJsonMock.mockResolvedValueOnce(studyEnrichment);

        const { enrichIdentifiers } = await import("@/app/(results)/actions");

        const results = await enrichIdentifiers([
            "7607",
            "7607STDY14643771",
            "48522",
            "71046409",
            "Custom",
        ]);

        expect(seqmetaJsonMock).toHaveBeenCalledTimes(1);
        expect(seqmetaJsonMock).toHaveBeenCalledWith(
            "/enrich/7607",
            expect.anything(),
        );
        expect(
            results.map((result) => [result.value, result.enrichment?.type]),
        ).toEqual([["7607", "study_id"]]);
    });
});
