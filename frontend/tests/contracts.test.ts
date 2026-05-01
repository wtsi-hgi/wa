import { describe, expect, it } from "vitest";

import {
    enrichmentResultSchema,
    errorSchema,
    fileEntrySchema,
    healthSchema,
    identifierResultSchema,
    metaKeysSchema,
    pipelineCountSchema,
    resultSetSchema,
    sampleSchema,
    samplesSchema,
    searchResultSchema,
    statsResultSchema,
    studiesSchema,
    studySchema,
} from "@/lib/contracts";

describe("contract schemas", () => {
    it("parses a valid ResultSet JSON object", () => {
        const parsed = resultSetSchema.parse({
            id: "result-1",
            pipeline_identifier: "gh://repo/workflow.nf",
            run_key: "runid=123",
            requester: "alice",
            operator: "bob",
            command: "nextflow run workflow.nf",
            pipeline_name: "nf-core/rnaseq",
            pipeline_version: "3.18.0",
            output_directory: "/tmp/out",
            metadata: {
                seqmeta_sampleid: "SANG123",
            },
            created_at: "2026-04-16T09:00:00Z",
            updated_at: "2026-04-16T10:00:00Z",
        });

        expect(parsed.id).toBe("result-1");
        expect(parsed.pipeline_name).toBe("nf-core/rnaseq");
        expect(parsed.metadata.seqmeta_sampleid).toBe("SANG123");
    });

    it("rejects a ResultSet missing the id field", () => {
        const result = resultSetSchema.safeParse({
            pipeline_identifier: "gh://repo/workflow.nf",
            run_key: "runid=123",
            requester: "alice",
            operator: "bob",
            command: "nextflow run workflow.nf",
            pipeline_name: "nf-core/rnaseq",
            pipeline_version: "3.18.0",
            output_directory: "/tmp/out",
            metadata: {},
            created_at: "2026-04-16T09:00:00Z",
            updated_at: "2026-04-16T10:00:00Z",
        });

        expect(result.success).toBe(false);
    });

    it("parses valid stats and related response collections", () => {
        const stats = statsResultSchema.parse({
            total: 2,
            recent: [
                {
                    id: "result-1",
                    pipeline_identifier: "gh://repo/workflow.nf",
                    run_key: "runid=123",
                    requester: "alice",
                    operator: "bob",
                    command: "nextflow run workflow.nf",
                    pipeline_name: "nf-core/rnaseq",
                    pipeline_version: "3.18.0",
                    output_directory: "/tmp/out",
                    metadata: {},
                    created_at: "2026-04-16T09:00:00Z",
                    updated_at: "2026-04-16T10:00:00Z",
                },
            ],
            daily: [{ date: "2026-04-16", count: 2 }],
            pipelines: [{ pipeline_name: "nf-core/rnaseq", count: 2 }],
        });
        const search = searchResultSchema.parse({
            result_set: stats.recent[0],
            matched_samples: ["SANG123"],
        });
        const pipeline = pipelineCountSchema.parse({
            pipeline_name: "nf-core/rnaseq",
            count: 2,
        });

        expect(stats.total).toBe(2);
        expect(Array.isArray(stats.recent)).toBe(true);
        expect(Array.isArray(stats.daily)).toBe(true);
        expect(Array.isArray(stats.pipelines)).toBe(true);
        expect(search.matched_samples).toEqual(["SANG123"]);
        expect(pipeline.count).toBe(2);
    });

    it("accepts known FileEntry kinds and rejects unknown kinds", () => {
        expect(() =>
            fileEntrySchema.parse({
                path: "/tmp/out/report.html",
                mtime: "2026-04-16T09:00:00Z",
                size: 100,
                kind: "output",
            }),
        ).not.toThrow();

        expect(
            fileEntrySchema.safeParse({
                path: "/tmp/out/report.html",
                mtime: "2026-04-16T09:00:00Z",
                size: 100,
                kind: "unknown",
            }).success,
        ).toBe(false);
    });

    it("accepts study and sample payloads with extra fields via passthrough wrappers", () => {
        const study = studySchema.parse({
            id_study_lims: "6568",
            name: "Cancer Genome Project",
            programme: "Cancer",
        });
        const studies = studiesSchema.parse([study]);
        const sample = sampleSchema.parse({
            sanger_id: "SANG123",
            library_type: "RNA",
        });
        const samples = samplesSchema.parse([sample]);

        expect(study.programme).toBe("Cancer");
        expect(studies).toHaveLength(1);
        expect(sample.library_type).toBe("RNA");
        expect(samples).toHaveLength(1);
    });

    it("parses meta key and basic error and health payloads", () => {
        expect(metaKeysSchema.parse(["lib", "run"])).toEqual(["lib", "run"]);
        expect(errorSchema.parse({ error: "not found" }).error).toBe(
            "not found",
        );
        expect(healthSchema.parse({ status: "healthy" }).status).toBe(
            "healthy",
        );
    });

    it("accepts identifier results with arbitrary nested object data", () => {
        const parsed = identifierResultSchema.parse({
            identifier: "6568",
            type: "study_id",
            object: {
                nested: {
                    values: [1, 2, 3],
                },
            },
        });

        expect(parsed.object).toEqual({
            nested: {
                values: [1, 2, 3],
            },
        });
    });

    it("parses a valid enrichment result payload for a study graph", () => {
        const result = enrichmentResultSchema.safeParse({
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
                libraries: [
                    {
                        library_type: "RNA",
                        id_study_lims: "6568",
                    },
                ],
            },
            partial: false,
        });

        expect(result.success).toBe(true);
    });

    it("preserves missing hop reasons for partial enrichment results", () => {
        const parsed = enrichmentResultSchema.parse({
            identifier: "RNA",
            type: "library_type",
            graph: {
                library: {
                    library_type: "RNA",
                    id_study_lims: "6568",
                },
                samples: [
                    {
                        id_study_lims: "6568",
                        id_sample_lims: "SMP001",
                        sanger_id: "SANG001",
                        sample_name: "sample-1",
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
            partial: true,
            missing: [
                {
                    hop: "samples",
                    reason: "samples_truncated",
                    status: 206,
                },
            ],
        });

        expect(parsed.missing?.[0]?.reason).toBe("samples_truncated");
    });

    it("rejects enrichment results that omit the graph envelope", () => {
        const result = enrichmentResultSchema.safeParse({
            identifier: "6568",
            type: "study_id",
            partial: false,
        });

        expect(result.success).toBe(false);
    });

    it("parses hierarchical enrichment structures with study_detail and library_details", () => {
        const result = enrichmentResultSchema.safeParse({
            identifier: "6568",
            type: "study_id",
            graph: {
                study: {
                    id_study_tmp: 42,
                    id_lims: "SQSCP",
                    id_study_lims: "6568",
                    name: "Test Study",
                    faculty_sponsor: "Dr Example",
                    state: "active",
                    abstract: "",
                    abbreviation: "",
                    accession_number: "ERP001",
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
                        name: "Test Study",
                        faculty_sponsor: "Dr Example",
                        state: "active",
                        abstract: "",
                        abbreviation: "",
                        accession_number: "ERP001",
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
                                    study_accession_number: "ERP001",
                                    accession_number: "ERS001",
                                },
                            ],
                        },
                    ],
                },
            },
            partial: false,
        });

        expect(result.success).toBe(true);
        if (result.success) {
            expect(
                result.data.graph.study_detail?.library_details,
            ).toHaveLength(1);
            expect(
                result.data.graph.study_detail?.library_details[0]?.samples,
            ).toHaveLength(1);
        }
    });

    it("parses hierarchical sample_detail with lanes", () => {
        const result = enrichmentResultSchema.safeParse({
            identifier: "S1",
            type: "sanger_sample_id",
            graph: {
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
                    study_accession_number: "ERP001",
                    accession_number: "ERS001",
                },
                sample_detail: {
                    sanger_id: "S1",
                    sample_name: "Sample 1",
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
                        study_accession_number: "ERP001",
                        accession_number: "ERS001",
                    },
                    lanes: [
                        {
                            id_run: "100",
                            lane: "1",
                            tag_index: 10,
                        },
                        {
                            id_run: "100",
                            lane: "2",
                            tag_index: 10,
                        },
                    ],
                },
            },
            partial: false,
        });

        expect(result.success).toBe(true);
        if (result.success) {
            expect(result.data.graph.sample_detail?.lanes).toHaveLength(2);
        }
    });
});
