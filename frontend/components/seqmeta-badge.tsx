"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";

import {
    ChevronDown,
    ChevronRight,
    Copy,
    Loader2,
    Search,
    X,
} from "lucide-react";

import type {
    EnrichmentResult,
    EnrichmentSample,
    LibraryDetail,
    MissingHop,
} from "@/lib/contracts";
import { fetchLibrarySamples } from "@/lib/seqmeta-enrichment";
import { cn } from "@/lib/utils";

type SeqmetaBadgeProps = {
    metadataKey: string;
    rawValue: string;
    enrichment: EnrichmentResult | null;
    error?: "not_found" | "upstream_impaired";
    loading?: boolean;
};

type SeqmetaDetailField = {
    key: string;
    label: string;
    searchKey?: string;
    value: string;
    group: "direct" | "related";
};

type HierarchicalLibrary = {
    libraryType: string;
    idStudyLims: string;
    samples: EnrichmentSample[];
};

type HierarchicalGroup = {
    type: "libraries" | "library" | "samples" | "study" | "lanes";
    title: string;
    items:
        | HierarchicalLibrary[]
        | EnrichmentSample[]
        | { name: string; id: string; accession?: string }[]
        | { id_run: string; lane: string; tag_index: number }[];
};

function asString(value: unknown): string | null {
    return typeof value === "string" && value.trim() ? value : null;
}

function humanizeToken(token: string): string {
    return token
        .split("_")
        .filter(Boolean)
        .map((part) => {
            if (part === "id") {
                return "ID";
            }

            if (part === "lims") {
                return "LIMS";
            }

            return part.replace(/^./, (letter) => letter.toUpperCase());
        })
        .join(" ");
}

function metadataLabel(metadataKey: string): string {
    const trimmedKey = metadataKey.replace(/^seqmeta_/, "");

    if (trimmedKey === "library" || trimmedKey === "librarytype") {
        return "Library type";
    }

    if (trimmedKey === "sampleid") {
        return "Sanger sample ID";
    }

    if (trimmedKey === "sample_lims") {
        return "Sample LIMS ID";
    }

    if (trimmedKey === "studyid") {
        return "Study identifier";
    }

    if (trimmedKey === "study_accession") {
        return "Study accession";
    }

    return humanizeToken(trimmedKey);
}

function isLibraryMetadataKey(metadataKey: string): boolean {
    return (
        metadataKey === "seqmeta_library" ||
        metadataKey === "seqmeta_librarytype"
    );
}

function directLibraryMetadataKey(metadataKey: string): string {
    return metadataKey === "seqmeta_librarytype"
        ? "seqmeta_librarytype"
        : "seqmeta_library";
}

function copiedStateKey(fieldKey: string, fieldValue: string): string {
    return `${fieldKey}:${fieldValue}`;
}

function enrichmentTypeLabel(type: string): string {
    if (type === "sanger_sample_id") {
        return "Sanger sample ID";
    }

    if (type === "study_id") {
        return "Study identifier";
    }

    if (type === "study_accession") {
        return "Study accession";
    }

    return humanizeToken(type);
}

function resolvedEnrichmentValue(enrichment: EnrichmentResult): string | null {
    if (
        enrichment.type === "study_id" ||
        enrichment.type === "study_accession"
    ) {
        return (
            asString(enrichment.graph.study?.id_study_lims) ??
            asString(enrichment.graph.study?.accession_number) ??
            asString(enrichment.identifier)
        );
    }

    return (
        asString(enrichment.graph.sample?.sanger_id) ??
        asString(enrichment.identifier)
    );
}

function primaryLabel(
    rawValue: string,
    enrichment: EnrichmentResult | null,
): string {
    return rawValue;
}

function librarySampleKey(sample: EnrichmentSample, index: number): string {
    const keyParts = [
        asString(sample.sanger_id) ?? "",
        asString(sample.id_sample_lims) ?? "",
        asString(sample.sample_name) ?? "",
        String(sample.id_run ?? ""),
        String(sample.lane ?? ""),
        String(sample.tag_index ?? ""),
    ];

    return `${keyParts.join("|")}|${index}`;
}

function sampleCopyStateKey(sample: EnrichmentSample, index?: number): string {
    const identity =
        index === undefined
            ? [
                  asString(sample.sanger_id) ?? "",
                  asString(sample.id_sample_lims) ?? "",
              ].join("|")
            : librarySampleKey(sample, index);

    return copiedStateKey("seqmeta_sampleid", identity);
}

function humanizeMissingHop(missing: MissingHop): string {
    if (missing.hop === "samples" && missing.reason === "samples_truncated") {
        return "Showing first 1000 samples";
    }

    if (missing.hop === "study" && missing.reason === "upstream_error") {
        return "Study record unavailable";
    }

    if (missing.hop === "samples" && missing.reason === "upstream_error") {
        return "Sample details unavailable";
    }

    if (missing.hop === "libraries" && missing.reason === "upstream_error") {
        return "Library details unavailable";
    }

    return `${missing.hop.replace(/^./, (letter) => letter.toUpperCase())} details unavailable`;
}

function appendDetailField(
    fields: SeqmetaDetailField[],
    field: SeqmetaDetailField | null,
    rawValue?: string,
    metadataKey?: string,
) {
    if (!field) {
        return;
    }

    const value = field.value.trim();

    if (!value) {
        return;
    }

    const duplicate = fields.some(
        (entry) =>
            entry.key === field.key &&
            entry.value.toLowerCase() === value.toLowerCase(),
    );

    if (!duplicate) {
        // Skip direct metadata fields whose value matches the dialog title (rawValue)
        // EXCEPT for primary identifier fields (where field.key matches the metadata key)
        const isPrimaryIdentifier = metadataKey && field.key === metadataKey;
        if (
            rawValue &&
            field.group === "direct" &&
            value.toLowerCase() === rawValue.trim().toLowerCase() &&
            !isPrimaryIdentifier
        ) {
            return;
        }

        fields.push({ ...field, value });
    }
}

function buildDetailFields(
    metadataKey: string,
    rawValue: string,
    enrichment: EnrichmentResult | null,
): SeqmetaDetailField[] {
    const fields: SeqmetaDetailField[] = [];

    if (!enrichment) {
        return fields;
    }

    const libraryMetadata = isLibraryMetadataKey(metadataKey);
    const studyMetadata =
        metadataKey === "seqmeta_studyid" ||
        metadataKey === "seqmeta_study_accession";
    const sampleMetadata =
        metadataKey === "seqmeta_sampleid" ||
        metadataKey === "seqmeta_sample_lims";

    // Study detail modals should never synthesize flat sample/library rows.
    // This avoids stale cached study responses rendering misleading legacy rows.
    const skipSampleFieldsForStudy = studyMetadata;

    if (!libraryMetadata) {
        appendDetailField(
            fields,
            enrichment.graph.study?.name
                ? {
                      key: "study_name",
                      label: "Study name",
                      value: enrichment.graph.study.name,
                      group: studyMetadata ? "direct" : "related",
                  }
                : null,
            rawValue,
            metadataKey,
        );
        appendDetailField(
            fields,
            enrichment.graph.study?.id_study_lims
                ? {
                      key: "study_id",
                      label: "Study identifier",
                      searchKey: "study",
                      value: enrichment.graph.study.id_study_lims,
                      group: studyMetadata ? "direct" : "related",
                  }
                : null,
            rawValue,
            metadataKey,
        );
        appendDetailField(
            fields,
            enrichment.graph.study?.accession_number
                ? {
                      key: "study_accession_number",
                      label: "Study accession",
                      value: enrichment.graph.study.accession_number,
                      group: studyMetadata ? "direct" : "related",
                  }
                : null,
            rawValue,
            metadataKey,
        );

        // Skip individual sample fields for study metadata.
        if (!skipSampleFieldsForStudy) {
            appendDetailField(
                fields,
                enrichment.graph.sample?.sample_name
                    ? {
                          key: "sample_name",
                          label: "Sample name",
                          value: enrichment.graph.sample.sample_name,
                          group: sampleMetadata ? "direct" : "related",
                      }
                    : null,
                rawValue,
                metadataKey,
            );
            appendDetailField(
                fields,
                enrichment.graph.sample?.sanger_id
                    ? {
                          key: "seqmeta_sampleid",
                          label: "Sanger sample ID",
                          searchKey: "sample",
                          value: enrichment.graph.sample.sanger_id,
                          group: sampleMetadata ? "direct" : "related",
                      }
                    : null,
                rawValue,
                metadataKey,
            );
            appendDetailField(
                fields,
                enrichment.graph.sample?.id_sample_lims
                    ? {
                          key: "seqmeta_sample_lims",
                          label: "Sample LIMS ID",
                          searchKey: "sample",
                          value: enrichment.graph.sample.id_sample_lims,
                          group: sampleMetadata ? "direct" : "related",
                      }
                    : null,
                rawValue,
                metadataKey,
            );
            appendDetailField(
                fields,
                enrichment.graph.sample?.accession_number
                    ? {
                          key: "sample_accession_number",
                          label: "Sample accession",
                          value: enrichment.graph.sample.accession_number,
                          group: sampleMetadata ? "direct" : "related",
                      }
                    : null,
                rawValue,
                metadataKey,
            );
        }
    }

    // Skip library fields for study metadata.
    if (!skipSampleFieldsForStudy) {
        const libraryTypes = [
            enrichment.graph.library?.library_type,
            enrichment.graph.sample?.library_type,
            ...(!libraryMetadata
                ? (enrichment.graph.libraries ?? []).map((library) =>
                      asString(library.library_type),
                  )
                : []),
        ].filter((value): value is string => Boolean(value));

        for (const libraryType of libraryTypes) {
            appendDetailField(
                fields,
                {
                    key: libraryMetadata
                        ? directLibraryMetadataKey(metadataKey)
                        : "seqmeta_library",
                    label: "Library type",
                    searchKey: "library",
                    value: libraryType,
                    group: libraryMetadata ? "direct" : "related",
                },
                rawValue,
                metadataKey,
            );
        }
    }

    return fields;
}

function buildHierarchicalGroups(
    metadataKey: string,
    enrichment: EnrichmentResult | null,
): HierarchicalGroup[] {
    if (!enrichment) {
        return [];
    }

    const groups: HierarchicalGroup[] = [];
    const studyMetadata =
        metadataKey === "seqmeta_studyid" ||
        metadataKey === "seqmeta_study_accession";
    const libraryMetadata = isLibraryMetadataKey(metadataKey);
    const sampleMetadata =
        metadataKey === "seqmeta_sampleid" ||
        metadataKey === "seqmeta_sample_lims";

    // For study details with hierarchy, group libraries.
    // Prefer study_detail.library_details; fall back to graph.libraries for
    // enrichment results cached before study_detail was introduced.
    if (studyMetadata) {
        const libraryItems: HierarchicalLibrary[] = [];

        if (enrichment.graph.study_detail?.library_details?.length) {
            for (const lib of enrichment.graph.study_detail.library_details) {
                libraryItems.push({
                    libraryType: lib.library_type,
                    idStudyLims: lib.id_study_lims,
                    samples: [], // Empty - samples loaded JIT on expansion
                });
            }
        } else if (enrichment.graph.libraries?.length) {
            for (const lib of enrichment.graph.libraries) {
                libraryItems.push({
                    libraryType: lib.library_type,
                    idStudyLims: lib.id_study_lims,
                    samples: [], // Empty - samples loaded JIT on expansion
                });
            }
        }

        if (libraryItems.length > 0) {
            groups.push({
                type: "libraries",
                title: "Libraries",
                items: libraryItems,
            });
        }
    }

    // For library details, show samples and parent study
    if (libraryMetadata) {
        // Collect all samples first (used for both study filtering and display)
        const allSamples = [
            ...(enrichment.graph.sample ? [enrichment.graph.sample] : []),
            ...(enrichment.graph.samples ?? []),
        ];

        // Show parent study/studies
        // Backend returns graph.studies (plural array) for library_type enrichment
        // globally, but we should only show studies that the returned samples belong to
        const allStudies = enrichment.graph.studies ?? [];

        // Get unique study IDs from samples
        const sampleStudyIds = new Set(
            allSamples
                .map((sample) => sample.id_study_lims)
                .filter((id): id is string => Boolean(id)),
        );

        // Filter studies to only those that samples belong to
        const studies =
            sampleStudyIds.size > 0
                ? allStudies.filter((study) =>
                      sampleStudyIds.has(study.id_study_lims),
                  )
                : allStudies;

        if (studies.length > 0) {
            groups.push({
                type: "study",
                title: "Study",
                items: studies.map((study) => ({
                    name: study.name,
                    id: study.id_study_lims,
                    accession: asString(study.accession_number) ?? undefined,
                })),
            });
        }

        // Show samples individually
        // Deduplicate samples by sanger_id + id_sample_lims
        const seenKeys = new Set<string>();
        const samples = allSamples.filter((sample) => {
            const key = `${sample.sanger_id}|${sample.id_sample_lims}`;

            if (seenKeys.has(key)) {
                return false;
            }

            seenKeys.add(key);
            return true;
        });

        if (samples.length > 0) {
            groups.push({
                type: "samples",
                title: "Samples",
                items: samples,
            });
        }
    }

    // For sample details with hierarchy, show library parent, study grandparent, and lanes
    if (sampleMetadata && enrichment.graph.sample_detail) {
        // Show parent library
        const libraryType = enrichment.graph.sample?.library_type;
        const idStudyLims = enrichment.graph.sample?.id_study_lims || "";

        if (libraryType) {
            groups.push({
                type: "library",
                title: "Library",
                items: [
                    {
                        libraryType,
                        idStudyLims,
                        samples: [],
                    },
                ],
            });
        }

        // Show study (grandparent of the sample)
        if (enrichment.graph.study) {
            groups.push({
                type: "study",
                title: "Study",
                items: [
                    {
                        name: enrichment.graph.study.name,
                        id: enrichment.graph.study.id_study_lims,
                        accession:
                            asString(enrichment.graph.study.accession_number) ??
                            undefined,
                    },
                ],
            });
        }

        // Show lanes
        if (
            enrichment.graph.sample_detail.lanes &&
            enrichment.graph.sample_detail.lanes.length > 0
        ) {
            groups.push({
                type: "lanes",
                title: "Lanes",
                items: enrichment.graph.sample_detail.lanes,
            });
        }
    }

    return groups;
}

function buildStatusLines(
    metadataKey: string,
    rawValue: string,
    enrichment: EnrichmentResult | null,
    error: SeqmetaBadgeProps["error"],
    loading: boolean,
): string[] {
    if (loading) {
        return [`Looking up ${metadataLabel(metadataKey).toLowerCase()}.`];
    }

    if (error === "not_found") {
        return [
            `No enrichment matched this ${metadataLabel(metadataKey).toLowerCase()} value.`,
        ];
    }

    if (error === "upstream_impaired") {
        return [
            `Upstream services were unavailable while resolving this ${metadataLabel(metadataKey).toLowerCase()} value.`,
        ];
    }

    if (!enrichment) {
        return [];
    }

    if (isLibraryMetadataKey(metadataKey)) {
        return [
            `Selected ${metadataLabel(metadataKey).toLowerCase()}: ${rawValue}.`,
        ];
    }

    const lines = [
        `Selected ${metadataLabel(metadataKey).toLowerCase()}: ${rawValue}.`,
    ];
    const resolvedValue = resolvedEnrichmentValue(enrichment);

    if (resolvedValue) {
        lines.push(
            `Resolved via ${enrichmentTypeLabel(enrichment.type)} ${resolvedValue}.`,
        );
    } else {
        lines.push(`Resolved via ${enrichmentTypeLabel(enrichment.type)}.`);
    }

    const studyName = asString(enrichment.graph.study?.name);

    if (studyName) {
        lines.push(`Study context: ${studyName}.`);
    }

    return lines;
}

async function writeClipboard(value: string): Promise<boolean> {
    function fallbackCopyText(text: string): boolean {
        if (
            typeof document === "undefined" ||
            typeof document.execCommand !== "function"
        ) {
            return false;
        }

        const textarea = document.createElement("textarea");

        textarea.value = text;
        textarea.setAttribute("readonly", "");
        textarea.style.position = "fixed";
        textarea.style.opacity = "0";
        textarea.style.pointerEvents = "none";

        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();

        try {
            return document.execCommand("copy");
        } finally {
            document.body.removeChild(textarea);
        }
    }

    if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
        return fallbackCopyText(value);
    }

    try {
        await navigator.clipboard.writeText(value);
        return true;
    } catch {
        return fallbackCopyText(value);
    }
}

export function SeqmetaBadge({
    metadataKey,
    rawValue,
    enrichment,
    error,
    loading = false,
}: SeqmetaBadgeProps) {
    const inlineLabel = primaryLabel(rawValue, enrichment);
    const detailFields = useMemo(
        () => buildDetailFields(metadataKey, rawValue, enrichment),
        [enrichment, metadataKey, rawValue],
    );
    const hierarchicalGroups = useMemo(
        () => buildHierarchicalGroups(metadataKey, enrichment),
        [enrichment, metadataKey],
    );
    const statusLines = useMemo(
        () =>
            buildStatusLines(metadataKey, rawValue, enrichment, error, loading),
        [enrichment, error, loading, metadataKey, rawValue],
    );
    const missingLines = useMemo(
        () =>
            enrichment?.partial
                ? (enrichment.missing ?? []).map(humanizeMissingHop)
                : [],
        [enrichment],
    );
    const [dialogOpen, setDialogOpen] = useState(false);
    const [copiedKey, setCopiedKey] = useState<string | null>(null);
    const [expandedLibraries, setExpandedLibraries] = useState<Set<string>>(
        new Set(),
    );
    const [loadedLibrarySamples, setLoadedLibrarySamples] = useState<
        Map<string, EnrichmentSample[]>
    >(new Map());
    const [loadingLibraries, setLoadingLibraries] = useState<Set<string>>(
        new Set(),
    );

    // Fetch library samples when a library is expanded
    useEffect(() => {
        const librariesGroup = hierarchicalGroups.find(
            (g) => g.type === "libraries",
        );
        if (!librariesGroup) {
            return;
        }

        const libraries = librariesGroup.items as HierarchicalLibrary[];
        const toLoad = libraries.filter(
            (lib) =>
                expandedLibraries.has(lib.libraryType) &&
                !loadedLibrarySamples.has(lib.libraryType) &&
                !loadingLibraries.has(lib.libraryType),
        );

        if (toLoad.length === 0) {
            return;
        }

        // Async function to handle loading
        const loadSamples = async () => {
            // Mark libraries as loading
            setLoadingLibraries((prev) => {
                const next = new Set(prev);
                for (const lib of toLoad) {
                    next.add(lib.libraryType);
                }
                return next;
            });

            // Fetch samples for each library
            await Promise.all(
                toLoad.map(async (lib) => {
                    try {
                        const samples = await fetchLibrarySamples(
                            lib.idStudyLims,
                            lib.libraryType,
                        );
                        setLoadedLibrarySamples((prev) => {
                            const next = new Map(prev);
                            next.set(lib.libraryType, samples ?? []);
                            return next;
                        });
                    } catch (error) {
                        console.error(
                            `Failed to load samples for library ${lib.libraryType}:`,
                            error,
                        );
                        setLoadedLibrarySamples((prev) => {
                            const next = new Map(prev);
                            next.set(lib.libraryType, []); // Set empty on error
                            return next;
                        });
                    }
                }),
            );

            // Clear loading state
            setLoadingLibraries((prev) => {
                const next = new Set(prev);
                for (const lib of toLoad) {
                    next.delete(lib.libraryType);
                }
                return next;
            });
        };

        void loadSamples();
    }, [
        expandedLibraries,
        hierarchicalGroups,
        loadedLibrarySamples,
        loadingLibraries,
    ]);

    useEffect(() => {
        if (!dialogOpen) {
            return undefined;
        }

        function handleKeyDown(event: KeyboardEvent) {
            if (event.key === "Escape") {
                setDialogOpen(false);
            }
        }

        window.addEventListener("keydown", handleKeyDown);

        return () => {
            window.removeEventListener("keydown", handleKeyDown);
        };
    }, [dialogOpen]);

    useEffect(() => {
        if (!copiedKey) {
            return undefined;
        }

        const timeout = window.setTimeout(() => {
            setCopiedKey(null);
        }, 1500);

        return () => {
            window.clearTimeout(timeout);
        };
    }, [copiedKey]);

    return (
        <>
            <span className="inline-flex max-w-full items-center gap-2 align-middle">
                <button
                    type="button"
                    aria-expanded={dialogOpen}
                    aria-haspopup="dialog"
                    aria-label={`Open ${metadataKey} details`}
                    data-testid="seqmeta-badge-trigger"
                    className={cn(
                        "inline-flex max-w-full cursor-pointer items-center rounded-full border border-border/80 px-3 py-1 text-left text-xs font-medium tracking-[0.16em] transition hover:border-primary/45 hover:bg-accent/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
                        enrichment
                            ? "bg-accent/20 text-foreground"
                            : "bg-background/80 text-muted-foreground",
                    )}
                    onClick={() => setDialogOpen(true)}
                >
                    <span
                        data-testid="seqmeta-badge-label"
                        className="truncate"
                    >
                        {inlineLabel}
                    </span>
                </button>

                {loading ? (
                    <span
                        aria-label="loading enrichment"
                        className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-border/80 bg-background text-[11px] font-semibold text-muted-foreground"
                    >
                        ...
                    </span>
                ) : null}

                {error === "not_found" ? (
                    <span
                        aria-label="enrichment unavailable"
                        className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-border/80 bg-background text-[11px] font-semibold text-muted-foreground"
                    >
                        ?
                    </span>
                ) : null}

                {error === "upstream_impaired" ? (
                    <span
                        aria-label="enrichment backend impaired"
                        className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-amber-500/40 bg-amber-500/10 text-[11px] font-semibold text-amber-700"
                    >
                        !
                    </span>
                ) : null}
            </span>

            {dialogOpen ? (
                <div
                    aria-labelledby={`seqmeta-dialog-title-${metadataKey}`}
                    aria-modal="true"
                    className="fixed inset-0 z-50 flex items-center justify-center p-4 sm:p-6"
                    role="dialog"
                >
                    <button
                        type="button"
                        aria-label="Close seqmeta details backdrop"
                        className="absolute inset-0 bg-[color:rgba(15,23,42,0.64)] backdrop-blur-sm"
                        onClick={() => setDialogOpen(false)}
                    />
                    <section className="relative z-10 flex max-h-[calc(100vh-2rem)] w-full max-w-4xl flex-col overflow-hidden rounded-[2rem] border border-border/80 bg-[linear-gradient(145deg,color-mix(in_oklab,var(--card)_88%,white_12%),color-mix(in_oklab,var(--accent)_14%,var(--card)_86%))] shadow-[0_36px_140px_-72px_rgba(20,31,49,0.9)] sm:max-h-[calc(100vh-3rem)]">
                        <div className="flex items-start justify-between gap-4 border-b border-border/70 px-6 py-5 sm:px-7">
                            <div className="space-y-2">
                                <p className="text-xs font-semibold uppercase tracking-[0.28em] text-muted-foreground">
                                    Seqmeta details
                                </p>
                                <h3
                                    id={`seqmeta-dialog-title-${metadataKey}`}
                                    className="text-2xl font-semibold tracking-tight text-foreground"
                                >
                                    {inlineLabel}
                                </h3>
                                <p className="font-mono text-xs text-muted-foreground">
                                    {metadataKey}
                                    {enrichment &&
                                    !isLibraryMetadataKey(metadataKey)
                                        ? ` (${enrichment.type})`
                                        : null}
                                </p>
                            </div>
                            <button
                                type="button"
                                aria-label="Close seqmeta details"
                                className="inline-flex size-10 items-center justify-center rounded-full border border-border/70 bg-background/80 text-foreground transition hover:border-primary/35 hover:bg-accent/25"
                                onClick={() => setDialogOpen(false)}
                            >
                                <X className="size-4" aria-hidden="true" />
                            </button>
                        </div>

                        <div
                            data-testid="seqmeta-dialog-body"
                            className="max-h-[calc(100vh-12rem)] min-h-0 overflow-y-auto px-6 py-6 sm:px-7"
                        >
                            <div className="space-y-6">
                                {loading ||
                                error ||
                                (enrichment?.partial &&
                                    missingLines.length > 0) ? (
                                    <div className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4">
                                        {loading ? (
                                            <p className="text-sm text-foreground">
                                                Looking up{" "}
                                                {metadataLabel(
                                                    metadataKey,
                                                ).toLowerCase()}
                                                .
                                            </p>
                                        ) : null}
                                        {error === "not_found" ? (
                                            <p className="text-sm text-foreground">
                                                No enrichment matched this{" "}
                                                {metadataLabel(
                                                    metadataKey,
                                                ).toLowerCase()}{" "}
                                                value.
                                            </p>
                                        ) : null}
                                        {error === "upstream_impaired" ? (
                                            <p className="text-sm text-foreground">
                                                Upstream services were
                                                unavailable while resolving this{" "}
                                                {metadataLabel(
                                                    metadataKey,
                                                ).toLowerCase()}{" "}
                                                value.
                                            </p>
                                        ) : null}
                                        {enrichment?.partial &&
                                        missingLines.length > 0 ? (
                                            <div>
                                                <p className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                    Partial response
                                                </p>
                                                <div className="mt-2 space-y-2 text-sm leading-6 text-foreground">
                                                    {missingLines.map(
                                                        (line) => (
                                                            <p key={line}>
                                                                {line}
                                                            </p>
                                                        ),
                                                    )}
                                                </div>
                                            </div>
                                        ) : null}
                                    </div>
                                ) : null}
                                {hierarchicalGroups.length > 0 ? (
                                    <div className="space-y-6">
                                        {/* Show direct metadata fields first for sample metadata with hierarchical groups */}
                                        {(() => {
                                            const directFields =
                                                detailFields.filter(
                                                    (field) =>
                                                        field.group ===
                                                        "direct",
                                                );

                                            if (directFields.length === 0) {
                                                return null;
                                            }

                                            return (
                                                <div data-field-group="direct-metadata">
                                                    <h4 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                        Direct Metadata
                                                    </h4>
                                                    <div className="space-y-3">
                                                        {directFields.map(
                                                            (field) => {
                                                                const href =
                                                                    field.searchKey
                                                                        ? `/?${new URLSearchParams(
                                                                              {
                                                                                  [field.searchKey]:
                                                                                      field.value,
                                                                              },
                                                                          ).toString()}`
                                                                        : null;

                                                                return (
                                                                    <article
                                                                        key={`${field.key}:${field.value}`}
                                                                        data-seqmeta-detail-key={
                                                                            field.key
                                                                        }
                                                                        className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
                                                                    >
                                                                        <div className="flex flex-wrap items-start justify-between gap-3">
                                                                            <div className="min-w-0">
                                                                                <p className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                                    {
                                                                                        field.label
                                                                                    }
                                                                                </p>
                                                                                <p className="mt-1 font-mono text-[11px] text-muted-foreground">
                                                                                    {
                                                                                        field.key
                                                                                    }
                                                                                </p>
                                                                            </div>
                                                                            <div className="flex flex-wrap gap-2">
                                                                                <button
                                                                                    type="button"
                                                                                    aria-label={`Copy ${field.key}`}
                                                                                    className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                    onClick={() => {
                                                                                        void writeClipboard(
                                                                                            field.value,
                                                                                        ).then(
                                                                                            (
                                                                                                copied,
                                                                                            ) => {
                                                                                                if (
                                                                                                    copied
                                                                                                ) {
                                                                                                    setCopiedKey(
                                                                                                        field.key,
                                                                                                    );
                                                                                                }
                                                                                            },
                                                                                        );
                                                                                    }}
                                                                                >
                                                                                    <Copy
                                                                                        className="size-3.5"
                                                                                        aria-hidden="true"
                                                                                    />
                                                                                    {copiedKey ===
                                                                                    field.key
                                                                                        ? "Copied"
                                                                                        : "Copy"}
                                                                                </button>
                                                                                {href ? (
                                                                                    <Link
                                                                                        aria-label={`Send ${field.key} to search filter`}
                                                                                        className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                        href={
                                                                                            href
                                                                                        }
                                                                                    >
                                                                                        <Search
                                                                                            className="size-3.5"
                                                                                            aria-hidden="true"
                                                                                        />
                                                                                        Filter
                                                                                    </Link>
                                                                                ) : null}
                                                                            </div>
                                                                        </div>
                                                                        <p className="mt-3 break-all text-sm leading-6 text-foreground">
                                                                            {
                                                                                field.value
                                                                            }
                                                                        </p>
                                                                    </article>
                                                                );
                                                            },
                                                        )}
                                                    </div>
                                                </div>
                                            );
                                        })()}
                                        {/* Show hierarchical groups for related data */}
                                        <div data-field-group="related-data">
                                            <h4 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                Related Data
                                            </h4>
                                            <div className="space-y-6">
                                                {hierarchicalGroups.map(
                                                    (group) => {
                                                        if (
                                                            group.type ===
                                                            "libraries"
                                                        ) {
                                                            const libraries =
                                                                group.items as HierarchicalLibrary[];

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="libraries"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {libraries.map(
                                                                            (
                                                                                library,
                                                                                index,
                                                                            ) => {
                                                                                const isExpanded =
                                                                                    expandedLibraries.has(
                                                                                        library.libraryType,
                                                                                    );
                                                                                const libraryDetailKey =
                                                                                    directLibraryMetadataKey(
                                                                                        metadataKey,
                                                                                    );
                                                                                const libraryCopyKey =
                                                                                    copiedStateKey(
                                                                                        libraryDetailKey,
                                                                                        library.libraryType,
                                                                                    );
                                                                                const isLoading =
                                                                                    loadingLibraries.has(
                                                                                        library.libraryType,
                                                                                    );
                                                                                const loadedSamples =
                                                                                    loadedLibrarySamples.get(
                                                                                        library.libraryType,
                                                                                    ) ??
                                                                                    [];

                                                                                return (
                                                                                    <div
                                                                                        key={`${library.libraryType}-${index}`}
                                                                                        className="space-y-2"
                                                                                    >
                                                                                        <article
																data-seqmeta-detail-key={libraryDetailKey}
                                                                                            className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
                                                                                        >
                                                                                            <div className="flex flex-wrap items-start justify-between gap-3">
                                                                                                <div className="min-w-0 flex-1">
                                                                                                    <p className="break-all text-sm leading-6 text-foreground">
                                                                                                        {
                                                                                                            library.libraryType
                                                                                                        }
                                                                                                    </p>
                                                                                                </div>
                                                                                                <div className="flex flex-wrap gap-2">
                                                                                                    <button
                                                                                                        type="button"
                                                                                                        aria-label={`Copy ${libraryDetailKey}`}
                                                                                                        className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                        onClick={() => {
                                                                                                            void writeClipboard(
                                                                                                                library.libraryType,
                                                                                                            ).then(
                                                                                                                (
                                                                                                                    copied,
                                                                                                                ) => {
                                                                                                                    if (
                                                                                                                        copied
                                                                                                                    ) {
                                                                                                                        setCopiedKey(
                                                                                                                            libraryCopyKey,
                                                                                                                        );
                                                                                                                    }
                                                                                                                },
                                                                                                            );
                                                                                                        }}
                                                                                                    >
                                                                                                        <Copy
                                                                                                            className="size-3.5"
                                                                                                            aria-hidden="true"
                                                                                                        />
                                                                                                        {copiedKey ===
                                                                                                        libraryCopyKey
                                                                                                            ? "Copied"
                                                                                                            : "Copy"}
                                                                                                    </button>
                                                                                                    <Link
                                                                                                        aria-label="Send library to search filter"
                                                                                                        className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                        href={`/?library=${library.libraryType}`}
                                                                                                    >
                                                                                                        <Search
                                                                                                            className="size-3.5"
                                                                                                            aria-hidden="true"
                                                                                                        />
                                                                                                        Filter
                                                                                                    </Link>
                                                                                                    <button
                                                                                                        type="button"
                                                                                                        aria-label={
                                                                                                            isExpanded
                                                                                                                ? "Hide samples"
                                                                                                                : "Show samples"
                                                                                                        }
                                                                                                        className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                        disabled={
                                                                                                            isLoading
                                                                                                        }
                                                                                                        onClick={() => {
                                                                                                            const newExpanded =
                                                                                                                new Set(
                                                                                                                    expandedLibraries,
                                                                                                                );

                                                                                                            if (
                                                                                                                isExpanded
                                                                                                            ) {
                                                                                                                newExpanded.delete(
                                                                                                                    library.libraryType,
                                                                                                                );
                                                                                                            } else {
                                                                                                                newExpanded.add(
                                                                                                                    library.libraryType,
                                                                                                                );
                                                                                                            }

                                                                                                            setExpandedLibraries(
                                                                                                                newExpanded,
                                                                                                            );
                                                                                                        }}
                                                                                                    >
                                                                                                        {isLoading ? (
                                                                                                            <Loader2
                                                                                                                className="size-3.5 animate-spin"
                                                                                                                aria-hidden="true"
                                                                                                            />
                                                                                                        ) : isExpanded ? (
                                                                                                            <ChevronDown
                                                                                                                className="size-3.5"
                                                                                                                aria-hidden="true"
                                                                                                            />
                                                                                                        ) : (
                                                                                                            <ChevronRight
                                                                                                                className="size-3.5"
                                                                                                                aria-hidden="true"
                                                                                                            />
                                                                                                        )}
                                                                                                        {isExpanded &&
                                                                                                        !isLoading ? (
                                                                                                            <>
                                                                                                                {
                                                                                                                    loadedSamples.length
                                                                                                                }{" "}
                                                                                                                sample
                                                                                                                {loadedSamples.length !==
                                                                                                                1
                                                                                                                    ? "s"
                                                                                                                    : ""}
                                                                                                            </>
                                                                                                        ) : isLoading ? (
                                                                                                            "Loading..."
                                                                                                        ) : (
                                                                                                            "Samples"
                                                                                                        )}
                                                                                                    </button>
                                                                                                </div>
                                                                                            </div>
                                                                                        </article>
                                                                                        {isExpanded &&
                                                                                        !isLoading &&
                                                                                        loadedSamples.length >
                                                                                            0 ? (
                                                                                            <div className="ml-4 space-y-2 border-l-2 border-border/40 pl-4">
                                                                                                {loadedSamples.map(
                                                                                                    (
                                                                                                        sample,
                                                                                                        index,
                                                                                                    ) => {
                                                                                                        const displayName =
                                                                                                            [
                                                                                                                asString(
                                                                                                                    sample.sample_name,
                                                                                                                ),
                                                                                                                asString(
                                                                                                                    sample.sanger_id,
                                                                                                                ),
                                                                                                            ]
                                                                                                                .filter(
                                                                                                                    Boolean,
                                                                                                                )
                                                                                                                .join(
                                                                                                                    " / ",
                                                                                                                );
                                                                                                        const sampleCopyKey =
                                                                                                            sampleCopyStateKey(
                                                                                                                sample,
                                                                                                                index,
                                                                                                            );

                                                                                                        return (
                                                                                                            <article
                                                                                                                key={librarySampleKey(
                                                                                                                    sample,
                                                                                                                    index,
                                                                                                                )}
                                                                                                                data-seqmeta-detail-key="sample"
                                                                                                                className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-3 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
                                                                                                            >
                                                                                                                <div className="flex flex-wrap items-start justify-between gap-3">
                                                                                                                    <div className="min-w-0 flex-1">
                                                                                                                        <p className="break-all text-sm leading-6 text-foreground">
                                                                                                                            {
                                                                                                                                displayName
                                                                                                                            }
                                                                                                                        </p>
                                                                                                                    </div>
                                                                                                                    <div className="flex flex-wrap gap-2">
                                                                                                                        {sample.sanger_id ? (
                                                                                                                            <>
                                                                                                                                <button
                                                                                                                                    type="button"
                                                                                                                                    aria-label="Copy seqmeta_sampleid"
                                                                                                                                    className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                                                    onClick={() => {
                                                                                                                                        void writeClipboard(
                                                                                                                                            sample.sanger_id,
                                                                                                                                        ).then(
                                                                                                                                            (
                                                                                                                                                copied,
                                                                                                                                            ) => {
                                                                                                                                                if (
                                                                                                                                                    copied
                                                                                                                                                ) {
                                                                                                                                                    setCopiedKey(
                                                                                                                                                        sampleCopyKey,
                                                                                                                                                    );
                                                                                                                                                }
                                                                                                                                            },
                                                                                                                                        );
                                                                                                                                    }}
                                                                                                                                >
                                                                                                                                    <Copy
                                                                                                                                        className="size-3.5"
                                                                                                                                        aria-hidden="true"
                                                                                                                                    />
                                                                                                                                    {copiedKey ===
                                                                                                                                    sampleCopyKey
                                                                                                                                        ? "Copied"
                                                                                                                                        : "Copy"}
                                                                                                                                </button>
                                                                                                                                <Link
                                                                                                                                    aria-label="Send sample to search filter"
                                                                                                                                    className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                                                    href={`/?sample=${sample.sanger_id}`}
                                                                                                                                >
                                                                                                                                    <Search
                                                                                                                                        className="size-3.5"
                                                                                                                                        aria-hidden="true"
                                                                                                                                    />
                                                                                                                                    Filter
                                                                                                                                </Link>
                                                                                                                            </>
                                                                                                                        ) : null}
                                                                                                                    </div>
                                                                                                                </div>
                                                                                                            </article>
                                                                                                        );
                                                                                                    },
                                                                                                )}
                                                                                            </div>
                                                                                        ) : null}
                                                                                    </div>
                                                                                );
                                                                            },
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        if (
                                                            group.type ===
                                                            "study"
                                                        ) {
                                                            const studies =
                                                                group.items as {
                                                                    name: string;
                                                                    id: string;
                                                                    accession?: string;
                                                                }[];

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="study"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {studies.map(
                                                                            (
                                                                                study,
                                                                            ) => {
                                                                                const studyCopyKey =
                                                                                    copiedStateKey(
                                                                                        "study_id",
                                                                                        study.id,
                                                                                    );

                                                                                return (
                                                                                    <article
                                                                                        key={
                                                                                            study.id
                                                                                        }
                                                                                        data-seqmeta-detail-key="study"
                                                                                        className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
                                                                                    >
                                                                                        <div className="flex flex-wrap items-start justify-between gap-3">
                                                                                            <div className="min-w-0 flex-1">
                                                                                                <p className="break-all text-sm leading-6 text-foreground">
                                                                                                    {
                                                                                                        study.name
                                                                                                    }
                                                                                                </p>
                                                                                            </div>
                                                                                            <div className="flex flex-wrap gap-2">
                                                                                                <button
                                                                                                    type="button"
                                                                                                    aria-label="Copy study_id"
                                                                                                    className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                    onClick={() => {
                                                                                                        void writeClipboard(
                                                                                                            study.name,
                                                                                                        ).then(
                                                                                                            (
                                                                                                                copied,
                                                                                                            ) => {
                                                                                                                if (
                                                                                                                    copied
                                                                                                                ) {
                                                                                                                    setCopiedKey(
                                                                                                                        studyCopyKey,
                                                                                                                    );
                                                                                                                }
                                                                                                            },
                                                                                                        );
                                                                                                    }}
                                                                                                >
                                                                                                    <Copy
                                                                                                        className="size-3.5"
                                                                                                        aria-hidden="true"
                                                                                                    />
                                                                                                    {copiedKey ===
                                                                                                    studyCopyKey
                                                                                                        ? "Copied"
                                                                                                        : "Copy"}
                                                                                                </button>
                                                                                                <Link
                                                                                                    aria-label="Send study to search filter"
                                                                                                    className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                    href={`/?study=${study.id}`}
                                                                                                >
                                                                                                    <Search
                                                                                                        className="size-3.5"
                                                                                                        aria-hidden="true"
                                                                                                    />
                                                                                                    Filter
                                                                                                </Link>
                                                                                            </div>
                                                                                        </div>
                                                                                    </article>
                                                                                );
                                                                            },
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        if (
                                                            group.type ===
                                                            "samples"
                                                        ) {
                                                            const samples =
                                                                group.items as EnrichmentSample[];

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="samples"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {samples.map(
                                                                            (
                                                                                sample,
                                                                            ) => {
                                                                                const displayName =
                                                                                    [
                                                                                        asString(
                                                                                            sample.sample_name,
                                                                                        ),
                                                                                        asString(
                                                                                            sample.sanger_id,
                                                                                        ),
                                                                                    ]
                                                                                        .filter(
                                                                                            Boolean,
                                                                                        )
                                                                                        .join(
                                                                                            " / ",
                                                                                        );
                                                                                const sampleCopyKey =
                                                                                    sampleCopyStateKey(
                                                                                        sample,
                                                                                    );

                                                                                return (
                                                                                    <article
                                                                                        key={`${sample.sanger_id}|${sample.id_sample_lims}`}
                                                                                        data-seqmeta-detail-key="sample"
                                                                                        className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
                                                                                    >
                                                                                        <div className="flex flex-wrap items-start justify-between gap-3">
                                                                                            <div className="min-w-0 flex-1">
                                                                                                <p className="break-all text-sm leading-6 text-foreground">
                                                                                                    {
                                                                                                        displayName
                                                                                                    }
                                                                                                </p>
                                                                                            </div>
                                                                                            <div className="flex flex-wrap gap-2">
                                                                                                {sample.sanger_id ? (
                                                                                                    <>
                                                                                                        <button
                                                                                                            type="button"
                                                                                                            aria-label="Copy seqmeta_sampleid"
                                                                                                            className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                            onClick={() => {
                                                                                                                void writeClipboard(
                                                                                                                    sample.sanger_id,
                                                                                                                ).then(
                                                                                                                    (
                                                                                                                        copied,
                                                                                                                    ) => {
                                                                                                                        if (
                                                                                                                            copied
                                                                                                                        ) {
                                                                                                                            setCopiedKey(
                                                                                                                                sampleCopyKey,
                                                                                                                            );
                                                                                                                        }
                                                                                                                    },
                                                                                                                );
                                                                                                            }}
                                                                                                        >
                                                                                                            <Copy
                                                                                                                className="size-3.5"
                                                                                                                aria-hidden="true"
                                                                                                            />
                                                                                                            {copiedKey ===
                                                                                                            sampleCopyKey
                                                                                                                ? "Copied"
                                                                                                                : "Copy"}
                                                                                                        </button>
                                                                                                        <Link
                                                                                                            aria-label="Send sample to search filter"
                                                                                                            className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                            href={`/?sample=${sample.sanger_id}`}
                                                                                                        >
                                                                                                            <Search
                                                                                                                className="size-3.5"
                                                                                                                aria-hidden="true"
                                                                                                            />
                                                                                                            Filter
                                                                                                        </Link>
                                                                                                    </>
                                                                                                ) : null}
                                                                                            </div>
                                                                                        </div>
                                                                                    </article>
                                                                                );
                                                                            },
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        if (
                                                            group.type ===
                                                            "library"
                                                        ) {
                                                            const libraries =
                                                                group.items as HierarchicalLibrary[];

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="library"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {libraries.map(
                                                                            (
                                                                                library,
                                                                                index,
                                                                            ) => (
                                                                                <article
                                                                                    key={`${library.libraryType}-${index}`}
                                                                                    data-seqmeta-detail-key="library"
                                                                                    className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
                                                                                >
                                                                                    <div className="flex flex-wrap items-start justify-between gap-3">
                                                                                        <div className="min-w-0 flex-1">
                                                                                            <p className="break-all text-sm leading-6 text-foreground">
                                                                                                {
                                                                                                    library.libraryType
                                                                                                }
                                                                                            </p>
                                                                                        </div>
                                                                                        <div className="flex flex-wrap gap-2">
                                                                                            <button
                                                                                                type="button"
                                                                                                aria-label={`Copy ${directLibraryMetadataKey(metadataKey)}`}
                                                                                                className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                onClick={() => {
                                                                                                    void writeClipboard(
                                                                                                        library.libraryType,
                                                                                                    ).then(
                                                                                                        (
                                                                                                            copied,
                                                                                                        ) => {
                                                                                                            if (
                                                                                                                copied
                                                                                                            ) {
                                                                                                                setCopiedKey(
                                                                                                                    directLibraryMetadataKey(
                                                                                                                        metadataKey,
                                                                                                                    ),
                                                                                                                );
                                                                                                            }
                                                                                                        },
                                                                                                    );
                                                                                                }}
                                                                                            >
                                                                                                <Copy
                                                                                                    className="size-3.5"
                                                                                                    aria-hidden="true"
                                                                                                />
                                                                                                {copiedKey ===
                                                                                                directLibraryMetadataKey(
                                                                                                    metadataKey,
                                                                                                )
                                                                                                    ? "Copied"
                                                                                                    : "Copy"}
                                                                                            </button>
                                                                                            <Link
                                                                                                aria-label="Send library to search filter"
                                                                                                className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                href={`/?library=${library.libraryType}`}
                                                                                            >
                                                                                                <Search
                                                                                                    className="size-3.5"
                                                                                                    aria-hidden="true"
                                                                                                />
                                                                                                Filter
                                                                                            </Link>
                                                                                        </div>
                                                                                    </div>
                                                                                </article>
                                                                            ),
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        if (
                                                            group.type ===
                                                            "lanes"
                                                        ) {
                                                            const lanes =
                                                                group.items as {
                                                                    id_run: string;
                                                                    lane: string;
                                                                    tag_index: number;
                                                                }[];

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="lanes"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {lanes.map(
                                                                            (
                                                                                lane,
                                                                                index,
                                                                            ) => {
                                                                                const laneId = `${lane.id_run}_${lane.lane}#${lane.tag_index}`;

                                                                                return (
                                                                                    <article
                                                                                        key={`${laneId}-${index}`}
                                                                                        data-seqmeta-detail-key="lane"
                                                                                        className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
                                                                                    >
                                                                                        <div className="flex flex-wrap items-start justify-between gap-3">
                                                                                            <div className="min-w-0 flex-1">
                                                                                                <p className="break-all text-sm leading-6 text-foreground">
                                                                                                    {
                                                                                                        laneId
                                                                                                    }
                                                                                                </p>
                                                                                            </div>
                                                                                            <div className="flex flex-wrap gap-2">
                                                                                                <button
                                                                                                    type="button"
                                                                                                    aria-label="Copy lane ID"
                                                                                                    className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                    onClick={() => {
                                                                                                        void writeClipboard(
                                                                                                            laneId,
                                                                                                        ).then(
                                                                                                            (
                                                                                                                copied,
                                                                                                            ) => {
                                                                                                                if (
                                                                                                                    copied
                                                                                                                ) {
                                                                                                                    setCopiedKey(
                                                                                                                        laneId,
                                                                                                                    );
                                                                                                                }
                                                                                                            },
                                                                                                        );
                                                                                                    }}
                                                                                                >
                                                                                                    <Copy
                                                                                                        className="size-3.5"
                                                                                                        aria-hidden="true"
                                                                                                    />
                                                                                                    {copiedKey ===
                                                                                                    laneId
                                                                                                        ? "Copied"
                                                                                                        : "Copy"}
                                                                                                </button>
                                                                                                <Link
                                                                                                    aria-label="Send lane to search filter"
                                                                                                    className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                    href={`/?seqmeta_lane=${laneId}`}
                                                                                                >
                                                                                                    <Search
                                                                                                        className="size-3.5"
                                                                                                        aria-hidden="true"
                                                                                                    />
                                                                                                    Filter
                                                                                                </Link>
                                                                                            </div>
                                                                                        </div>
                                                                                    </article>
                                                                                );
                                                                            },
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        return null;
                                                    },
                                                )}
                                            </div>
                                        </div>
                                    </div>
                                ) : (
                                    <>
                                        {(() => {
                                            const directFields =
                                                detailFields.filter(
                                                    (field) =>
                                                        field.group ===
                                                        "direct",
                                                );
                                            const relatedFields =
                                                detailFields.filter(
                                                    (field) =>
                                                        field.group ===
                                                        "related",
                                                );

                                            const renderFieldGroup = (
                                                fields: SeqmetaDetailField[],
                                                title: string,
                                            ) => {
                                                if (fields.length === 0) {
                                                    return null;
                                                }

                                                return (
                                                    <div
                                                        data-field-group={title
                                                            .toLowerCase()
                                                            .replace(
                                                                /\s+/g,
                                                                "-",
                                                            )}
                                                    >
                                                        <h4 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                            {title}
                                                        </h4>
                                                        <div className="space-y-3">
                                                            {fields.map(
                                                                (field) => {
                                                                    const href =
                                                                        field.searchKey
                                                                            ? `/?${new URLSearchParams(
                                                                                  {
                                                                                      [field.searchKey]:
                                                                                          field.value,
                                                                                  },
                                                                              ).toString()}`
                                                                            : null;

                                                                    return (
                                                                        <article
                                                                            key={`${field.key}:${field.value}`}
                                                                            data-seqmeta-detail-key={
                                                                                field.key
                                                                            }
                                                                            className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
                                                                        >
                                                                            <div className="flex flex-wrap items-start justify-between gap-3">
                                                                                <div className="min-w-0">
                                                                                    <p className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                                        {
                                                                                            field.label
                                                                                        }
                                                                                    </p>
                                                                                    <p className="mt-1 font-mono text-[11px] text-muted-foreground">
                                                                                        {
                                                                                            field.key
                                                                                        }
                                                                                    </p>
                                                                                </div>
                                                                                <div className="flex flex-wrap gap-2">
                                                                                    <button
                                                                                        type="button"
                                                                                        aria-label={`Copy ${field.key}`}
                                                                                        className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                        onClick={() => {
                                                                                            void writeClipboard(
                                                                                                field.value,
                                                                                            ).then(
                                                                                                (
                                                                                                    copied,
                                                                                                ) => {
                                                                                                    if (
                                                                                                        copied
                                                                                                    ) {
                                                                                                        setCopiedKey(
                                                                                                            field.key,
                                                                                                        );
                                                                                                    }
                                                                                                },
                                                                                            );
                                                                                        }}
                                                                                    >
                                                                                        <Copy
                                                                                            className="size-3.5"
                                                                                            aria-hidden="true"
                                                                                        />
                                                                                        {copiedKey ===
                                                                                        field.key
                                                                                            ? "Copied"
                                                                                            : "Copy"}
                                                                                    </button>
                                                                                    {href ? (
                                                                                        <Link
                                                                                            aria-label={`Send ${field.key} to search filter`}
                                                                                            className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                            href={
                                                                                                href
                                                                                            }
                                                                                        >
                                                                                            <Search
                                                                                                className="size-3.5"
                                                                                                aria-hidden="true"
                                                                                            />
                                                                                            Filter
                                                                                        </Link>
                                                                                    ) : null}
                                                                                </div>
                                                                            </div>
                                                                            <p className="mt-3 break-all text-sm leading-6 text-foreground">
                                                                                {
                                                                                    field.value
                                                                                }
                                                                            </p>
                                                                        </article>
                                                                    );
                                                                },
                                                            )}
                                                        </div>
                                                    </div>
                                                );
                                            };

                                            return (
                                                <>
                                                    {renderFieldGroup(
                                                        directFields,
                                                        "Direct Metadata",
                                                    )}
                                                    {renderFieldGroup(
                                                        relatedFields,
                                                        "Related Data",
                                                    )}
                                                </>
                                            );
                                        })()}
                                    </>
                                )}
                            </div>
                        </div>
                    </section>
                </div>
            ) : null}
        </>
    );
}
