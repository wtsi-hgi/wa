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
    abstract: z.string(),
    abbreviation: z.string(),
    accession_number: z.string(),
    description: z.string(),
    data_release_strategy: z.string(),
    study_title: z.string(),
    data_access_group: z.string(),
    hmdmc_number: z.string(),
    programme: z.string(),
    created: z.string(),
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

export const enrichmentSampleSchema = z.object({
    id_study_lims: z.string(),
    id_sample_lims: z.string(),
    sanger_id: z.string(),
    sample_name: z.string(),
    taxon_id: z.number(),
    common_name: z.string(),
    library_type: z.string(),
    id_run: z.number(),
    lane: z.number(),
    tag_index: z.number(),
    irods_path: z.string(),
    study_accession_number: z.string(),
    accession_number: z.string(),
});
export type EnrichmentSample = z.infer<typeof enrichmentSampleSchema>;

export const enrichmentSamplesSchema = z.array(enrichmentSampleSchema);
export type EnrichmentSamples = z.infer<typeof enrichmentSamplesSchema>;

export const libraryLinkSchema = z.object({
    library_type: z.string(),
    id_study_lims: z.string(),
});
export type LibraryLink = z.infer<typeof libraryLinkSchema>;

export const projectSchema = z.object({
    id: z.number(),
    name: z.string(),
});
export type Project = z.infer<typeof projectSchema>;

export const projectUserSchema = z.object({
    id: z.number(),
    username: z.string(),
});
export type ProjectUser = z.infer<typeof projectUserSchema>;

export const laneDetailSchema = z.object({
    id_run: z.string(),
    lane: z.string(),
    tag_index: z.number(),
});
export type LaneDetail = z.infer<typeof laneDetailSchema>;

export const sampleDetailSchema = z.object({
    sanger_id: z.string(),
    sample_name: z.string(),
    sample: enrichmentSampleSchema,
    lanes: z.array(laneDetailSchema),
});
export type SampleDetail = z.infer<typeof sampleDetailSchema>;

export const libraryDetailSchema = z.object({
    library_type: z.string(),
    id_study_lims: z.string(),
    samples: enrichmentSamplesSchema,
});
export type LibraryDetail = z.infer<typeof libraryDetailSchema>;

export const studyDetailSchema = z.object({
    study: enrichmentStudySchema,
    library_details: z.array(libraryDetailSchema),
});
export type StudyDetail = z.infer<typeof studyDetailSchema>;

export const enrichmentGraphSchema = z.object({
    study: enrichmentStudySchema.optional(),
    studies: enrichmentStudiesSchema.optional(),
    sample: enrichmentSampleSchema.optional(),
    samples: enrichmentSamplesSchema.optional(),
    library: libraryLinkSchema.optional(),
    libraries: z.array(libraryLinkSchema).optional(),
    project: projectSchema.optional(),
    users: z.array(projectUserSchema).optional(),
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
