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

export const errorSchema = z.object({
    error: z.string(),
});
export type ErrorResponse = z.infer<typeof errorSchema>;

export const healthSchema = z.object({
    status: z.string(),
});
export type Health = z.infer<typeof healthSchema>;
