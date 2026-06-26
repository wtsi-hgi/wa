import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EnrichmentResult, IdentifierResult } from "@/lib/contracts";

const originalMlwhBackendUrl = process.env.WA_MLWH_BACKEND_URL;

function jsonResponse(body: unknown, status = 200): Response {
    return Response.json(body, { status });
}

function stubFetchResponses(...responses: Response[]) {
    const fetchMock = vi.fn();

    for (const response of responses) {
        fetchMock.mockResolvedValueOnce(response);
    }

    vi.stubGlobal("fetch", fetchMock);

    return fetchMock;
}

const enrichmentStudyFixture = {
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
};

const mlwhStudyMatchPayload = {
    kind: "study_lims_id",
    canonical: "6568",
    sample: null,
    study: {
        id_study_tmp: 42,
        id_lims: "SQSCP",
        id_study_lims: "6568",
        uuid_study_lims: "study-uuid-6568",
        name: "Cancer Programme",
        accession_number: "ERP123456",
        study_title: "Cancer Programme Cohort",
        faculty_sponsor: "Dr Example",
        state: "active",
        data_release_strategy: "managed",
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
    run: null,
    library: null,
};

const identifierFixture: IdentifierResult = {
    identifier: "6568",
    type: "study_lims_id",
    object: mlwhStudyMatchPayload.study,
};

const mlwhSampleMatchPayload = {
    kind: "sanger_sample_name",
    canonical: "7607STDY14643771",
    sample: {
        id_sample_tmp: 73,
        id_lims: "SQSCP",
        id_sample_lims: "SMP-7607-1",
        uuid_sample_lims: "sample-uuid-7607",
        name: "7607STDY14643771",
        sanger_sample_id: "SANG001",
        supplier_name: "supplier-one",
        accession_number: "SAMEA7607",
        donor_id: "DONOR-1",
        taxon_id: 9606,
        common_name: "Human",
        description: "primary sample",
        studies: [mlwhStudyMatchPayload.study],
        libraries: [
            {
                pipeline_id_lims: "Custom",
                id_study_lims: "7607",
                library_id: "71046409",
                id_library_lims: "SQPP-47463-G:B1",
            },
        ],
    },
    study: null,
    run: null,
    library: null,
};

const enrichmentFixture: EnrichmentResult = {
    identifier: "6568",
    type: "study_id",
    graph: {
        study: enrichmentStudyFixture,
    },
    partial: false,
};

const librarySamplesPayload = [
    {
        id_sample_lims: "SMP-1",
        name: "sample-one",
        sanger_sample_id: "SANG001",
        supplier_name: "supplier-one",
        taxon_id: 9606,
        common_name: "Human",
        accession_number: "SAMEA1",
        studies: [enrichmentStudyFixture],
        libraries: [
            {
                pipeline_id_lims: "Standard",
                id_study_lims: "6568",
                library_id: "L1",
                id_library_lims: "DN1",
            },
        ],
    },
];

describe("A3 MLWH-backed server actions", () => {
    beforeEach(() => {
        vi.resetModules();
        process.env.WA_MLWH_BACKEND_URL = "https://mlwh:9000";
    });

    afterEach(() => {
        if (originalMlwhBackendUrl === undefined) {
            delete process.env.WA_MLWH_BACKEND_URL;
        } else {
            process.env.WA_MLWH_BACKEND_URL = originalMlwhBackendUrl;
        }

        vi.unstubAllGlobals();
    });

    it("normalizes study classifications from the MLWH /classify endpoint", async () => {
        const fetchMock = stubFetchResponses(
            jsonResponse(mlwhStudyMatchPayload),
        );

        const { validateIdentifier } = await import("@/app/(results)/actions");

        await expect(validateIdentifier("6568")).resolves.toEqual(
            identifierFixture,
        );
        expect(fetchMock).toHaveBeenCalledWith(
            "https://mlwh:9000/classify/6568",
        );
    });

    it("normalizes sample classifications from the MLWH /classify endpoint", async () => {
        const fetchMock = stubFetchResponses(
            jsonResponse(mlwhSampleMatchPayload),
        );

        const { validateIdentifier } = await import("@/app/(results)/actions");

        await expect(validateIdentifier("7607STDY14643771")).resolves.toEqual({
            identifier: "7607STDY14643771",
            type: "sanger_sample_name",
            object: mlwhSampleMatchPayload.sample,
        });
        expect(fetchMock).toHaveBeenCalledWith(
            "https://mlwh:9000/classify/7607STDY14643771",
        );
    });

    it("returns null when identifier classification returns a MLWH 404 envelope", async () => {
        stubFetchResponses(
            jsonResponse(
                { code: "not_found", message: "identifier not found" },
                404,
            ),
        );

        const { validateIdentifier } = await import("@/app/(results)/actions");

        await expect(validateIdentifier("missing-id")).resolves.toBeNull();
    });

    it("enriches identifiers through the MLWH /enrich endpoint", async () => {
        const fetchMock = stubFetchResponses(jsonResponse(enrichmentFixture));

        const { enrichIdentifier } = await import("@/app/(results)/actions");

        await expect(enrichIdentifier("6568")).resolves.toMatchObject({
            type: "study_id",
            partial: false,
        });
        expect(fetchMock).toHaveBeenCalledWith("https://mlwh:9000/enrich/6568");
    });

    it("preserves BackendRequestError for 502 upstream-impaired responses", async () => {
        const fetchMock = stubFetchResponses(
            jsonResponse(
                {
                    code: "upstream_impaired",
                    message: "all enrichment hops failed",
                },
                502,
            ),
        );
        const { BackendRequestError } = await import("@/lib/backend-client");
        const { enrichIdentifier } = await import("@/app/(results)/actions");

        const result = enrichIdentifier("flaky-id");

        await expect(result).rejects.toMatchObject({
            status: 502,
            body: {
                code: "upstream_impaired",
                message: "all enrichment hops failed",
            },
        });
        await expect(result).rejects.toBeInstanceOf(BackendRequestError);
        expect(fetchMock).toHaveBeenCalledWith(
            "https://mlwh:9000/enrich/flaky-id",
        );
    });

    it("fetches study samples from the MLWH /study/:id/samples endpoint", async () => {
        const fetchMock = stubFetchResponses(
            jsonResponse([
                {
                    id_sample_lims: "SMP-1",
                    name: "sample-one",
                    sanger_sample_id: "SANG001",
                },
                {
                    id_sample_lims: "SMP-2",
                    name: "SANG002",
                    sanger_sample_id: "",
                },
            ]),
        );

        const { fetchStudySamples } = await import("@/app/(results)/actions");

        await expect(fetchStudySamples("6568")).resolves.toEqual([
            "SANG001",
            "SANG002",
        ]);
        expect(fetchMock).toHaveBeenCalledWith(
            "https://mlwh:9000/study/6568/samples",
        );
    });
});

describe("A4 study library sample endpoint selection", () => {
    beforeEach(() => {
        vi.resetModules();
        process.env.WA_MLWH_BACKEND_URL = "https://mlwh:9000";
    });

    afterEach(() => {
        if (originalMlwhBackendUrl === undefined) {
            delete process.env.WA_MLWH_BACKEND_URL;
        } else {
            process.env.WA_MLWH_BACKEND_URL = originalMlwhBackendUrl;
        }

        vi.unstubAllGlobals();
    });

    it("uses the study-and-library endpoint when no library identifiers are set", async () => {
        const fetchMock = stubFetchResponses(
            jsonResponse(librarySamplesPayload),
        );

        const { fetchStudyLibrarySamples } =
            await import("@/app/(results)/actions");

        await expect(
            fetchStudyLibrarySamples("6568", "Standard", {}),
        ).resolves.toEqual([
            expect.objectContaining({
                id_study_lims: "6568",
                library_type: "Standard",
                sample_name: "sample-one",
                sanger_id: "SANG001",
            }),
        ]);
        expect(fetchMock).toHaveBeenCalledWith(
            "https://mlwh:9000/library/Standard/study/6568/samples",
        );
    });

    it("uses the library-id endpoint when libraryId is set", async () => {
        const fetchMock = stubFetchResponses(
            jsonResponse(librarySamplesPayload),
        );

        const { fetchStudyLibrarySamples } =
            await import("@/app/(results)/actions");

        await fetchStudyLibrarySamples("6568", "Standard", { libraryId: "L1" });

        expect(fetchMock).toHaveBeenCalledWith(
            "https://mlwh:9000/library-id/L1/samples",
        );
    });

    it("uses the library-lims-id endpoint ahead of libraryId when both are set", async () => {
        const fetchMock = stubFetchResponses(
            jsonResponse(librarySamplesPayload),
        );

        const { fetchStudyLibrarySamples } =
            await import("@/app/(results)/actions");

        await fetchStudyLibrarySamples("6568", "Standard", {
            libraryId: "L1",
            idLibraryLims: "DN1",
        });

        expect(fetchMock).toHaveBeenCalledWith(
            "https://mlwh:9000/library-lims-id/DN1/samples",
        );
    });
});

describe("H2 enrichIdentifier action", () => {
    beforeEach(() => {
        vi.resetModules();
        process.env.WA_MLWH_BACKEND_URL = "https://mlwh:9000";
    });

    afterEach(() => {
        if (originalMlwhBackendUrl === undefined) {
            delete process.env.WA_MLWH_BACKEND_URL;
        } else {
            process.env.WA_MLWH_BACKEND_URL = originalMlwhBackendUrl;
        }

        vi.unstubAllGlobals();
    });

    it("returns null when the backend responds with 404", async () => {
        stubFetchResponses(
            jsonResponse(
                { code: "not_found", message: "unknown identifier" },
                404,
            ),
        );

        const { enrichIdentifier } = await import("@/app/(results)/actions");

        await expect(enrichIdentifier("missing-id")).resolves.toBeNull();
    });

    it("enriches multiple identifiers through one batched server action result", async () => {
        const fetchMock = stubFetchResponses(
            jsonResponse(enrichmentFixture),
            jsonResponse(
                { code: "not_found", message: "unknown identifier" },
                404,
            ),
            jsonResponse(
                {
                    code: "upstream_impaired",
                    message: "all enrichment hops failed",
                },
                502,
            ),
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
        expect(fetchMock).toHaveBeenCalledTimes(3);
        expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
            "https://mlwh:9000/enrich/6568",
            "https://mlwh:9000/enrich/missing-id",
            "https://mlwh:9000/enrich/flaky-id",
        ]);
    });

    it("reuses aliases from the first enrichment inside a batch", async () => {
        const studyEnrichment: EnrichmentResult = {
            ...enrichmentFixture,
            identifier: "7607",
            graph: {
                ...enrichmentFixture.graph,
                study: {
                    ...enrichmentStudyFixture,
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
        const fetchMock = stubFetchResponses(jsonResponse(studyEnrichment));

        const { enrichIdentifiers } = await import("@/app/(results)/actions");

        const results = await enrichIdentifiers([
            "7607",
            "7607STDY14643771",
            "48522",
            "71046409",
            "Custom",
        ]);

        expect(fetchMock).toHaveBeenCalledTimes(1);
        expect(fetchMock).toHaveBeenCalledWith("https://mlwh:9000/enrich/7607");
        expect(
            results.map((result) => [result.value, result.enrichment?.type]),
        ).toEqual([["7607", "study_id"]]);
    });
});

describe("fetchStudies server action", () => {
    beforeEach(() => {
        vi.resetModules();
    });

    afterEach(() => {
        vi.unstubAllEnvs();
    });

    it("delegates to getStudies", async () => {
        const studiesFixture = [
            { id_study_lims: "6568", name: "RNA Seq" },
            { id_study_lims: "7001", name: "Cancer Panel" },
        ];
        const getStudiesMock = vi.fn().mockResolvedValue(studiesFixture);

        vi.doMock("@/lib/studies-cache", () => ({
            getStudies: getStudiesMock,
            resetStudiesCache: vi.fn(),
        }));

        const { fetchStudies } = await import("@/app/(results)/actions");

        await expect(fetchStudies()).resolves.toEqual(studiesFixture);
        expect(getStudiesMock).toHaveBeenCalledTimes(1);
    });
});
