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
            abstract: "Study abstract",
            abbreviation: "CP",
            accession_number: "ERP123456",
            description: "Study description",
            data_release_strategy: "managed",
            study_title: "Cancer Programme Cohort",
            data_access_group: "group-a",
            hmdmc_number: "HMDMC-1",
            programme: "Cancer",
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
});
