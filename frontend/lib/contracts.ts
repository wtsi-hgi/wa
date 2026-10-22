import { z } from "zod";

export const fileEntrySchema = z.object({
    path: z.string(),
    mtime: z.string(),
    size: z.number(),
    kind: z.enum(["output", "input", "pipeline"]),
});
export type FileEntry = z.infer<typeof fileEntrySchema>;

export const accessStateSchema = z.object({
    can_view: z.boolean(),
    locked: z.boolean(),
    reason: z.string().optional(),
});
export type AccessState = z.infer<typeof accessStateSchema>;

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
    output_directory_gid: z.number().int().nullable().default(null),
    access: accessStateSchema.default({
        can_view: true,
        locked: false,
    }),
    metadata: z.record(z.string(), z.string()),
    metadata_values: z.record(z.string(), z.array(z.string())).optional(),
    created_at: z.string(),
    updated_at: z.string(),
});
export type ResultSet = z.input<typeof resultSetSchema>;

export const searchResultSchema = z.object({
    result_set: resultSetSchema,
    matched_samples: z.array(z.string()).optional(),
});
export type SearchResult = z.input<typeof searchResultSchema>;

export const searchSuggestionSchema = z.object({
    field_key: z.string(),
    value: z.string(),
    label: z.string().optional(),
});
export type SearchSuggestion = z.infer<typeof searchSuggestionSchema>;

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
export type StatsResult = z.input<typeof statsResultSchema>;

export const metaKeysSchema = z.array(z.string());
export type MetaKeys = z.infer<typeof metaKeysSchema>;

const normalizedIdentifierResultSchema = z.object({
    identifier: z.string(),
    type: z.string(),
    object: z.unknown(),
});

const mlwhMatchSchema = z.object({
    kind: z.string(),
    canonical: z.string(),
    sample: z.unknown().nullable().optional(),
    study: z.unknown().nullable().optional(),
    run: z.unknown().nullable().optional(),
    library: z.unknown().nullable().optional(),
});

function mlwhMatchObject(match: z.infer<typeof mlwhMatchSchema>): unknown {
    return match.sample ?? match.study ?? match.run ?? match.library ?? null;
}

export const identifierResultSchema = z
    .union([
        normalizedIdentifierResultSchema,
        mlwhMatchSchema.transform((match) => ({
            identifier: match.canonical,
            type: match.kind,
            object: mlwhMatchObject(match),
        })),
    ])
    .pipe(normalizedIdentifierResultSchema);
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

const rawSampleSchema = z
    .object({
        sanger_id: z.string().optional(),
        sanger_sample_id: z.string().optional(),
        name: z.string().optional(),
    })
    .passthrough();

function normalizeSample(sample: z.infer<typeof rawSampleSchema>) {
    const sangerID = [
        sample.sanger_id,
        sample.sanger_sample_id,
        sample.name,
    ].find((value) => value?.trim());

    return {
        ...sample,
        sanger_id: sangerID,
    };
}

export const sampleSchema = rawSampleSchema
    .transform(normalizeSample)
    .pipe(z.object({ sanger_id: z.string() }).passthrough());
export type Sample = z.infer<typeof sampleSchema>;

export const samplesSchema = z.array(sampleSchema);
export type Samples = z.infer<typeof samplesSchema>;

export const enrichmentStudySchema = z.object({
    id_study_tmp: z.number(),
    id_lims: z.string(),
    id_study_lims: z.string(),
    uuid_study_lims: z.string().optional(),
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
    donor_id: z.string().optional(),
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
        donor_id: z.string().optional(),
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
    donor_id: z.string().optional(),
    taxon_id: z.number(),
    common_name: z.string(),
    library_type: z.string(),
    library_types: z.array(z.string()).optional(),
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
        donor_id: sample.donor_id,
        taxon_id: sample.taxon_id,
        common_name: sample.common_name,
        library_type: sample.library_type,
        library_types: undefined,
        accession_number: sample.accession_number,
        id_run: undefined,
        lane: undefined,
        tag_index: undefined,
        irods_path: undefined,
        study_accession_number: undefined,
    };
}

function distinctLibraryTypes(
    libraries: RawEnrichmentSampleInput["libraries"],
): string[] | undefined {
    const types = [
        ...new Set(
            (libraries ?? [])
                .map((library) => library.pipeline_id_lims.trim())
                .filter(Boolean),
        ),
    ];

    return types.length > 0 ? types : undefined;
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
        donor_id: sample.donor_id,
        taxon_id: sample.taxon_id,
        common_name: sample.common_name,
        library_type: firstLibrary?.pipeline_id_lims ?? "",
        library_types: distinctLibraryTypes(sample.libraries),
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

export const errorSchema = z.union([
    z.object({ error: z.string() }),
    z
        .object({
            code: z.string(),
            message: z.string(),
        })
        .transform(({ message }) => ({ error: message })),
]);
export type ErrorResponse = z.infer<typeof errorSchema>;

export const lockedResponseSchema = z.object({
    error: z.literal("locked"),
    locked: z.literal(true),
    result_id: z.string().optional(),
    message: z.string(),
});
export type LockedResponse = z.infer<typeof lockedResponseSchema>;

export const healthSchema = z.object({
    status: z.string(),
});
export type Health = z.infer<typeof healthSchema>;

export const feedbackCategorySchema = z.enum([
    "could_not_answer",
    "agent_mistake",
    "no_endpoint",
    "user_unhappy",
    "other",
]);
export type FeedbackCategory = z.infer<typeof feedbackCategorySchema>;

export const feedbackReportSchema = z.object({
    id: z.number().int().positive(),
    created_at: z.string(),
    category: feedbackCategorySchema,
    description: z.string(),
    user_request: z.string(),
    tools_tried: z.array(z.string()),
    mcp_server_version: z.string(),
    wa_api_version: z.string(),
    transport: z.string(),
    client_name: z.string(),
    client_version: z.string(),
    client_user_agent: z.string(),
    remote_addr: z.string(),
    acknowledged: z.boolean(),
    acknowledged_at: z.string(),
});
export type FeedbackReport = z.infer<typeof feedbackReportSchema>;

export const feedbackPageSchema = z.object({
    items: z.array(feedbackReportSchema),
    total: z.number().int().nonnegative(),
    next_offset: z.number().int(),
});
export type FeedbackPage = z.infer<typeof feedbackPageSchema>;

export const feedbackDeleteResponseSchema = z.literal("");

// Server Action inputs. Server Actions are public POST endpoints, so
// arguments are untrusted at runtime whatever their TS types say.
export const feedbackIdSchema = z
    .number()
    .int()
    .positive()
    .max(Number.MAX_SAFE_INTEGER);

// feedbackPageSize is the number of reports per /feedback page.
export const feedbackPageSize = 50;

export const feedbackListInputSchema = z.object({
    show: z.enum(["unacknowledged", "all"]),
    category: feedbackCategorySchema.nullable(),
    offset: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER),
});
export type FeedbackListInput = z.infer<typeof feedbackListInputSchema>;

// Usage summary from wa Go GET /usage (mlwh/usage_summary.go UsageSummary).
export const usageWindowSchema = z.enum(["24h", "7d", "30d", "all"]);
export type UsageWindow = z.infer<typeof usageWindowSchema>;
export const usageCategorySchema = z.enum([
    "cli",
    "mcp",
    "web",
    "results_server",
    "mlwhdiff_server",
    "other",
]);
export type UsageCategory = z.infer<typeof usageCategorySchema>;
const countMap = z.record(z.string(), z.number().int().nonnegative());
const count = z.number().int().nonnegative();
// Errors split by severity into parts that sum to their count
// (UsageErrorBreakdown), with expected answers to probe requests counted as
// probe. legacy counts, by severity, the rows among them recorded before
// error details, which the server judges by status. Absent from servers that
// predate it.
const usageErrorBreakdownSchema = z.object({
    fault: count,
    caller: count,
    expected: count,
    probe: count,
    legacy: z.object({ fault: count, caller: count, expected: count }),
});
export type UsageErrorBreakdown = z.infer<typeof usageErrorBreakdownSchema>;
// errors is absent from servers that predate error details, and
// error_severity from servers that predate per-list severities.
const usageValueCountSchema = z.object({
    value: z.string(),
    requests: count,
    errors: count.optional(),
    error_severity: usageErrorBreakdownSchema.optional(),
});
// The narrowing GET /usage applied (mlwh/usage_filter.go UsageFilter). Only
// servers that support filters echo it.
export const usageFilterSchema = z.object({
    category: usageCategorySchema.optional(),
    user: z.string().optional(),
    tool: z.string().optional(),
    agent: z.string().optional(),
    method: z.string().optional(),
    route: z.string().optional(),
});
export type UsageFilter = z.infer<typeof usageFilterSchema>;
// A filter the page may send: each value fits its wa column cap
// (mlwh/usage_filter.go), and method and route come together.
// filterValue is a non-empty string of at most maxBytes UTF-8 bytes, as wa
// measures its caps.
const filterValue = (maxBytes: number) =>
    z
        .string()
        .min(1)
        .refine((value) => new TextEncoder().encode(value).length <= maxBytes, {
            message: `longer than ${maxBytes} bytes`,
        });
export const usageFilterInputSchema = usageFilterSchema
    .extend({
        user: filterValue(128).optional(),
        tool: filterValue(128).optional(),
        agent: filterValue(256).optional(),
        method: filterValue(32).optional(),
        route: filterValue(256)
            .refine((route) => route.startsWith("/"))
            .optional(),
    })
    .strict()
    .refine(
        (filter) =>
            (filter.method === undefined) === (filter.route === undefined),
        {
            message: "method and route go together",
        },
    );
const usageTotalsSchema = z.object({
    requests: count,
    errors: count,
    error_rate: z.number(),
    error_severity: usageErrorBreakdownSchema.optional(),
    sessions: count,
    ip_clients: count,
    users: count,
});
export type UsageTotals = z.infer<typeof usageTotalsSchema>;
// One group of GET /usage errors (mlwh/usage_summary.go UsageErrorGroup).
// error_code is null for rows recorded before error details were stored.
// severity (mlwh/usage_error_summary.go UsageErrorSeverity: fault, caller,
// or expected) is a string, so a severity added later still parses; it and
// probe are absent from servers that predate them.
const usageErrorGroupSchema = z.object({
    method: z.string(),
    route: z.string(),
    status: z.number().int(),
    error_code: z.string().nullable(),
    severity: z.string().optional(),
    probe: z.boolean().optional(),
    count,
    first_seen: z.string(),
    last_seen: z.string(),
    error_kinds: countMap,
    messages: z.array(z.object({ message: z.string(), count })),
    examples: z.array(
        z.object({
            ts: z.string(),
            path: z.string(),
            query: z.string(),
            category: usageCategorySchema,
            mcp_tool: z.string(),
            mcp_agent: z.string(),
            client_id: z.string(),
            username: z.string(),
            message: z.string(),
            error_kind: z.string(),
        }),
    ),
    by_category: countMap,
    by_tool: countMap,
    by_agent: countMap,
});
export const usageSummarySchema = z.object({
    window: usageWindowSchema,
    // The fields marked optional below are absent from servers that predate
    // them; previous_totals is null for the all window.
    filter: usageFilterSchema.optional(),
    generated_at: z.string(),
    window_start: z.string(),
    earliest_record: z.string(),
    retention_days: z.number().int().positive(),
    dropped_events: count,
    totals: usageTotalsSchema,
    previous_totals: usageTotalsSchema.nullable().optional(),
    categories: z.array(
        z.object({
            category: usageCategorySchema,
            requests: count,
            errors: count,
            error_rate: z.number(),
            sessions: count,
            ip_clients: count,
            users: count,
            requests_per_session_median: count,
            requests_per_session_max: count,
            error_severity: usageErrorBreakdownSchema.optional(),
        }),
    ),
    endpoints: z.array(
        z.object({
            method: z.string(),
            route: z.string(),
            requests: count,
            errors: count,
            error_rate: z.number(),
            p50_ms: z.number(),
            p95_ms: z.number(),
            by_category: countMap,
            error_severity: usageErrorBreakdownSchema.optional(),
        }),
    ),
    mcp_tools: z.array(usageValueCountSchema),
    mcp_agents: z.array(usageValueCountSchema),
    other_user_agents: z.array(usageValueCountSchema),
    unmatched: z.array(
        z.object({
            method: z.string(),
            path: z.string(),
            requests: count,
            by_category: countMap,
            by_tool: countMap,
            by_agent: countMap,
        }),
    ),
    users: z.array(
        z.object({
            name: z.string(),
            by_ip: z.boolean(),
            requests: count,
            by_category: countMap,
        }),
    ),
    clients: z.array(
        z.object({
            name: z.string(),
            by_ip: z.boolean(),
            requests: count,
            by_category: countMap,
        }),
    ),
    days: z.array(
        z.object({
            date: z.string(),
            requests: count,
            by_category: countMap,
            error_severity: usageErrorBreakdownSchema.optional(),
        }),
    ),
    // Hours for 24h, days for 7d and 30d, and days or weeks for all.
    bucket_unit: z.enum(["hour", "day", "week"]).optional(),
    buckets: z
        .array(
            z.object({
                start: z.string(),
                requests: count,
                by_category: countMap,
                error_severity: usageErrorBreakdownSchema,
            }),
        )
        .optional(),
    series: z.array(
        z.object({
            start: z.string(),
            end: z.string(),
            requests: count,
            by_category: countMap,
            by_endpoint: countMap,
            by_tool: countMap.optional(),
            by_user: countMap.optional(),
        }),
    ),
    // Absent from servers that predate error details.
    errors: z.array(usageErrorGroupSchema).optional(),
});
export type UsageSummary = z.infer<typeof usageSummarySchema>;

// Cache freshness from wa Go GET /freshness (mlwh/freshness.go Freshness).
// Servers older than MLWH API 1.12.0 send only table, high_water, last_run
// and ever_synced, and no overall status.
const freshnessTableSchema = z.object({
    table: z.string(),
    high_water: z.string(),
    last_run: z.string(),
    ever_synced: z.boolean(),
    last_success_at: z.string().optional(),
    last_error: z.string().optional(),
    last_error_at: z.string().optional(),
    blocked_by: z.array(z.string()).nullable().optional(),
    status: z.string().optional(),
});
export const freshnessSchema = z.object({
    status: z.string().optional(),
    stale_after_seconds: z.number().optional(),
    tables: z.array(freshnessTableSchema),
});
export type Freshness = z.infer<typeof freshnessSchema>;

// The /freshness document from a server that reports sync status (MLWH API
// 1.12.0 or later): every status and outcome field is present.
export const mlwhSyncStatusSchema = z.object({
    status: z.string(),
    stale_after_seconds: z.number().int().nonnegative(),
    tables: z.array(
        freshnessTableSchema.extend({
            last_success_at: z.string(),
            last_error: z.string(),
            last_error_at: z.string(),
            blocked_by: z
                .array(z.string())
                .nullable()
                .transform((tables) => tables ?? []),
            status: z.string(),
        }),
    ),
});
export type MLWHSyncStatus = z.infer<typeof mlwhSyncStatusSchema>;
