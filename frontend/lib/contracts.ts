import { z } from "zod";

export const fileEntrySchema = z.object({
    path: z.string(),
    mtime: z.string(),
    size: z.number(),
    kind: z.enum(["output", "input", "pipeline"]),
});
export type FileEntry = z.infer<typeof fileEntrySchema>;

export const resultSetSchema = z.object({
    id: z.string(),
    pipeline_identifier: z.string(),
    run_key: z.string(),
    requester: z.string(),
    operator: z.string(),
    command: z.string(),
    pipeline_name: z.string(),
    pipeline_version: z.string(),
    output_directory: z.string(),
    metadata: z.record(z.string(), z.string()),
    created_at: z.string(),
    updated_at: z.string(),
});
export type ResultSet = z.infer<typeof resultSetSchema>;

export const searchResultSchema = z.object({
    result_set: resultSetSchema,
    matched_samples: z.array(z.string()).optional(),
});
export type SearchResult = z.infer<typeof searchResultSchema>;

export const dailyCountSchema = z.object({
    date: z.string(),
    count: z.number(),
});
export type DailyCount = z.infer<typeof dailyCountSchema>;

export const pipelineCountSchema = z.object({
    pipeline_name: z.string(),
    count: z.number(),
});
export type PipelineCount = z.infer<typeof pipelineCountSchema>;

export const statsResultSchema = z.object({
    total: z.number(),
    recent: z.array(resultSetSchema),
    daily: z.array(dailyCountSchema),
    pipelines: z.array(pipelineCountSchema),
});
export type StatsResult = z.infer<typeof statsResultSchema>;

export const metaKeysSchema = z.array(z.string());
export type MetaKeys = z.infer<typeof metaKeysSchema>;

export const identifierResultSchema = z.object({
    identifier: z.string(),
    type: z.string(),
    object: z.unknown(),
});
export type IdentifierResult = z.infer<typeof identifierResultSchema>;

export const studySchema = z
    .object({
        id_study_lims: z.string(),
        name: z.string(),
    })
    .passthrough();
export type Study = z.infer<typeof studySchema>;

export const studiesSchema = z.array(studySchema);
export type Studies = z.infer<typeof studiesSchema>;

export const sampleSchema = z
    .object({
        sanger_id: z.string(),
    })
    .passthrough();
export type Sample = z.infer<typeof sampleSchema>;

export const samplesSchema = z.array(sampleSchema);
export type Samples = z.infer<typeof samplesSchema>;

export const enrichmentStudySchema = z.object({
    id_study_tmp: z.number(),
    id_lims: z.string(),
    id_study_lims: z.string(),
    name: z.string(),
    faculty_sponsor: z.string(),
    state: z.string(),
    accession_number: z.string(),
    data_release_strategy: z.string(),
    study_title: z.string(),
    data_access_group: z.string(),
    programme: z.string(),
    reference_genome: z.string(),
    ethically_approved: z.boolean(),
    study_type: z.string(),
    contains_human_dna: z.boolean(),
    contaminated_human_dna: z.boolean(),
    study_visibility: z.string(),
    ega_dac_accession_number: z.string(),
    ega_policy_accession_number: z.string(),
    data_release_timing: z.string(),
});
export type EnrichmentStudy = z.infer<typeof enrichmentStudySchema>;

export const enrichmentStudiesSchema = z.array(enrichmentStudySchema);
export type EnrichmentStudies = z.infer<typeof enrichmentStudiesSchema>;

const mlwhLibraryLinkInputSchema = z
    .object({
        pipeline_id_lims: z.string(),
        id_study_lims: z.string(),
        library_id: z.string().optional(),
        id_library_lims: z.string().optional(),
    })
    .passthrough();

const mlwhEnrichmentSampleInputSchema = z.object({
    id_study_lims: z.string(),
    id_sample_lims: z.string(),
    sanger_id: z.string(),
    name: z.string(),
    supplier_name: z.string().optional(),
    taxon_id: z.number(),
    common_name: z.string(),
    library_type: z.string(),
    accession_number: z.string(),
});

type EnrichmentSampleInput = z.infer<typeof mlwhEnrichmentSampleInputSchema>;

const mlwhRawEnrichmentSampleInputSchema = z
    .object({
        id_sample_lims: z.string(),
        name: z.string(),
        sanger_sample_id: z.string().optional(),
        supplier_name: z.string().optional(),
        taxon_id: z.number(),
        common_name: z.string(),
        accession_number: z.string(),
        studies: z.array(enrichmentStudySchema).optional(),
        libraries: z.array(mlwhLibraryLinkInputSchema).optional(),
    })
    .passthrough();

type RawEnrichmentSampleInput = z.infer<
    typeof mlwhRawEnrichmentSampleInputSchema
>;

const normalizedEnrichmentSampleSchema = z.object({
    id_study_lims: z.string(),
    id_sample_lims: z.string(),
    sanger_id: z.string(),
    sample_name: z.string(),
    supplier_name: z.string().optional(),
    taxon_id: z.number(),
    common_name: z.string(),
    library_type: z.string(),
    accession_number: z.string(),
    id_run: z.number().optional(),
    lane: z.number().optional(),
    tag_index: z.number().optional(),
    irods_path: z.string().optional(),
    study_accession_number: z.string().optional(),
});

type NormalizedEnrichmentSample = z.infer<
    typeof normalizedEnrichmentSampleSchema
>;

function normalizeEnrichmentSample(
    sample: EnrichmentSampleInput,
): NormalizedEnrichmentSample {
    return {
        id_study_lims: sample.id_study_lims,
        id_sample_lims: sample.id_sample_lims,
        sanger_id: sample.sanger_id,
        sample_name: sample.name,
        supplier_name: sample.supplier_name,
        taxon_id: sample.taxon_id,
        common_name: sample.common_name,
        library_type: sample.library_type,
        accession_number: sample.accession_number,
        id_run: undefined,
        lane: undefined,
        tag_index: undefined,
        irods_path: undefined,
        study_accession_number: undefined,
    };
}

function normalizeRawEnrichmentSample(
    sample: RawEnrichmentSampleInput,
): NormalizedEnrichmentSample {
    const firstStudy = sample.studies?.[0];
    const firstLibrary = sample.libraries?.[0];

    return {
        id_study_lims:
            firstLibrary?.id_study_lims ?? firstStudy?.id_study_lims ?? "",
        id_sample_lims: sample.id_sample_lims,
        sanger_id: sample.sanger_sample_id ?? sample.name,
        sample_name: sample.name,
        supplier_name: sample.supplier_name,
        taxon_id: sample.taxon_id,
        common_name: sample.common_name,
        library_type: firstLibrary?.pipeline_id_lims ?? "",
        accession_number: sample.accession_number,
        id_run: undefined,
        lane: undefined,
        tag_index: undefined,
        irods_path: undefined,
        study_accession_number: firstStudy?.accession_number,
    };
}

const mlwhEnrichmentSampleSchema = mlwhEnrichmentSampleInputSchema
    .transform(normalizeEnrichmentSample)
    .pipe(normalizedEnrichmentSampleSchema);

const mlwhRawEnrichmentSampleSchema = mlwhRawEnrichmentSampleInputSchema
    .transform(normalizeRawEnrichmentSample)
    .pipe(normalizedEnrichmentSampleSchema);

export const enrichmentSampleSchema = z.union([
    normalizedEnrichmentSampleSchema,
    mlwhEnrichmentSampleSchema,
    mlwhRawEnrichmentSampleSchema,
]);
export type EnrichmentSample = z.infer<typeof enrichmentSampleSchema>;

const normalizedEnrichmentSamplesSchema = z.array(
    normalizedEnrichmentSampleSchema,
);

export const enrichmentSamplesSchema = z.array(enrichmentSampleSchema);
export type EnrichmentSamples = z.infer<typeof enrichmentSamplesSchema>;

export const libraryLinkSchema = z.object({
    library_type: z.string(),
    id_study_lims: z.string(),
    library_id: z.string().optional(),
    id_library_lims: z.string().optional(),
});
export type LibraryLink = z.infer<typeof libraryLinkSchema>;

export const irodsPathSchema = z.object({
    id_product: z.string(),
    collection: z.string(),
    data_object: z.string(),
    irods_path: z.string(),
});
export type IRODSPath = z.infer<typeof irodsPathSchema>;

const normalizedLaneDetailSchema = z.object({
    id_run: z.string(),
    lane: z.string(),
    tag_index: z.number(),
});

export const laneDetailSchema = z
    .object({
        id_run: z.union([z.string(), z.number()]).transform(String),
        lane: z.union([z.string(), z.number()]).transform(String),
        tag_index: z.number(),
    })
    .pipe(normalizedLaneDetailSchema);
export type LaneDetail = z.infer<typeof laneDetailSchema>;

const mlwhSampleDetailInputSchema = z.object({
    sample: enrichmentSampleSchema,
    study: enrichmentStudySchema.optional(),
    lanes: z.array(laneDetailSchema),
    libraries: z.array(mlwhLibraryLinkInputSchema).optional(),
    irods_paths: z.array(irodsPathSchema).optional(),
});

const normalizedSampleDetailSchema = z.object({
    sanger_id: z.string(),
    sample_name: z.string(),
    sample: normalizedEnrichmentSampleSchema,
    study: enrichmentStudySchema.optional(),
    lanes: z.array(normalizedLaneDetailSchema),
    libraries: z.array(libraryLinkSchema).optional(),
    irods_paths: z.array(irodsPathSchema).optional(),
});

type NormalizedSampleDetail = z.infer<typeof normalizedSampleDetailSchema>;

export const sampleDetailSchema = mlwhSampleDetailInputSchema
    .transform(
        (detail): NormalizedSampleDetail => ({
            sanger_id: detail.sample.sanger_id,
            sample_name: detail.sample.sample_name,
            sample: detail.sample,
            study: detail.study,
            lanes: detail.lanes,
            libraries: detail.libraries?.map((library) => ({
                library_type: library.pipeline_id_lims,
                id_study_lims: library.id_study_lims,
                library_id: library.library_id,
                id_library_lims: library.id_library_lims,
            })),
            irods_paths: detail.irods_paths,
        }),
    )
    .pipe(normalizedSampleDetailSchema);
export type SampleDetail = z.infer<typeof sampleDetailSchema>;

const mlwhLibraryDetailInputSchema = z.object({
    library: mlwhLibraryLinkInputSchema.optional(),
    samples: enrichmentSamplesSchema,
});

const normalizedLibraryDetailSchema = z.object({
    library_type: z.string(),
    id_study_lims: z.string(),
    library_id: z.string().optional(),
    id_library_lims: z.string().optional(),
    samples: normalizedEnrichmentSamplesSchema,
});

function normalizeLibraryDetail(
    detail: z.infer<typeof mlwhLibraryDetailInputSchema>,
    fallbackStudyLims?: string,
): {
    library_type: string;
    id_study_lims: string;
    library_id?: string;
    id_library_lims?: string;
    samples: EnrichmentSamples;
} {
    const firstSample = detail.samples[0];

    return {
        library_type:
            detail.library?.pipeline_id_lims ?? firstSample?.library_type ?? "",
        id_study_lims: fallbackStudyLims ?? firstSample?.id_study_lims ?? "",
        library_id: detail.library?.library_id,
        id_library_lims: detail.library?.id_library_lims,
        samples: detail.samples,
    };
}

export const libraryDetailSchema = mlwhLibraryDetailInputSchema
    .transform((detail) => normalizeLibraryDetail(detail))
    .pipe(normalizedLibraryDetailSchema);
export type LibraryDetail = z.infer<typeof libraryDetailSchema>;

const mlwhStudyDetailInputSchema = z.object({
    study: enrichmentStudySchema,
    library_details: z.array(mlwhLibraryDetailInputSchema),
});

const normalizedStudyDetailSchema = z.object({
    study: enrichmentStudySchema,
    library_details: z.array(normalizedLibraryDetailSchema),
});

export const studyDetailSchema = mlwhStudyDetailInputSchema
    .transform((detail) => ({
        study: detail.study,
        library_details: detail.library_details.map((libraryDetail) =>
            normalizeLibraryDetail(libraryDetail, detail.study.id_study_lims),
        ),
    }))
    .pipe(normalizedStudyDetailSchema);
export type StudyDetail = z.infer<typeof studyDetailSchema>;

export const enrichmentGraphSchema = z.object({
    study: enrichmentStudySchema.optional(),
    studies: enrichmentStudiesSchema.optional(),
    sample: enrichmentSampleSchema.optional(),
    samples: enrichmentSamplesSchema.optional(),
    library: libraryLinkSchema.optional(),
    libraries: z.array(libraryLinkSchema).optional(),
    // Hierarchical structures
    study_detail: studyDetailSchema.optional(),
    study_details: z.array(studyDetailSchema).optional(),
    sample_detail: sampleDetailSchema.optional(),
});
export type EnrichmentGraph = z.infer<typeof enrichmentGraphSchema>;

export const missingHopSchema = z.object({
    hop: z.string(),
    reason: z.string(),
    status: z.number(),
});
export type MissingHop = z.infer<typeof missingHopSchema>;

export const enrichmentResultSchema = z.object({
    identifier: z.string(),
    type: z.string(),
    graph: enrichmentGraphSchema,
    partial: z.boolean(),
    missing: z.array(missingHopSchema).optional(),
});
export type EnrichmentResult = z.infer<typeof enrichmentResultSchema>;

export const errorSchema = z.object({
    error: z.string(),
});
export type ErrorResponse = z.infer<typeof errorSchema>;

export const healthSchema = z.object({
    status: z.string(),
});
export type Health = z.infer<typeof healthSchema>;
