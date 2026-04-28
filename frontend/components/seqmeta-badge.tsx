"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";

import { Copy, Search, X } from "lucide-react";

import type { EnrichmentResult, MissingHop } from "@/lib/contracts";
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

    if (trimmedKey === "library") {
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
    return metadataKey === "seqmeta_library";
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
    if (!enrichment) {
        return rawValue;
    }

    if (
        enrichment.type === "study_id" ||
        enrichment.type === "study_accession"
    ) {
        return (
            asString(enrichment.graph.study?.name) ??
            asString(enrichment.graph.study?.accession_number) ??
            rawValue
        );
    }

    return rawValue;
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

    if (missing.hop === "project" && missing.reason === "upstream_error") {
        return "Project details unavailable";
    }

    if (missing.hop === "users" && missing.reason === "upstream_error") {
        return "Project users unavailable";
    }

    return `${missing.hop.replace(/^./, (letter) => letter.toUpperCase())} details unavailable`;
}

function appendDetailField(
    fields: SeqmetaDetailField[],
    field: SeqmetaDetailField | null,
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
        fields.push({ ...field, value });
    }
}

function buildDetailFields(
    metadataKey: string,
    rawValue: string,
    enrichment: EnrichmentResult | null,
): SeqmetaDetailField[] {
    const fields: SeqmetaDetailField[] = [];

    appendDetailField(fields, {
        key: metadataKey,
        label: "Selected metadata value",
        searchKey: metadataKey,
        value: rawValue,
    });

    if (!enrichment) {
        return fields;
    }

    const libraryMetadata = isLibraryMetadataKey(metadataKey);

    if (!libraryMetadata) {
        appendDetailField(fields, {
            key: "seqmeta_type",
            label: "Resolved seqmeta type",
            value: enrichment.type,
        });

        appendDetailField(
            fields,
            enrichment.graph.study?.name
                ? {
                      key: "study_name",
                      label: "Study name",
                      value: enrichment.graph.study.name,
                  }
                : null,
        );
        appendDetailField(
            fields,
            enrichment.graph.study?.id_study_lims
                ? {
                      key: "study_id",
                      label: "Study identifier",
                      searchKey: "study_id",
                      value: enrichment.graph.study.id_study_lims,
                  }
                : null,
        );
        appendDetailField(
            fields,
            enrichment.graph.study?.accession_number
                ? {
                      key: "study_accession_number",
                      label: "Study accession",
                      value: enrichment.graph.study.accession_number,
                  }
                : null,
        );

        appendDetailField(
            fields,
            enrichment.graph.sample?.sample_name
                ? {
                      key: "sample_name",
                      label: "Sample name",
                      value: enrichment.graph.sample.sample_name,
                  }
                : null,
        );
        appendDetailField(
            fields,
            enrichment.graph.sample?.sanger_id
                ? {
                      key: "seqmeta_sampleid",
                      label: "Sanger sample ID",
                      searchKey: "seqmeta_sampleid",
                      value: enrichment.graph.sample.sanger_id,
                  }
                : null,
        );
        appendDetailField(
            fields,
            enrichment.graph.sample?.id_sample_lims
                ? {
                      key: "seqmeta_sample_lims",
                      label: "Sample LIMS ID",
                      searchKey: "seqmeta_sample_lims",
                      value: enrichment.graph.sample.id_sample_lims,
                  }
                : null,
        );
        appendDetailField(
            fields,
            enrichment.graph.sample?.accession_number
                ? {
                      key: "sample_accession_number",
                      label: "Sample accession",
                      value: enrichment.graph.sample.accession_number,
                  }
                : null,
        );
    }

    const libraryTypes = [
        enrichment.graph.library?.library_type,
        enrichment.graph.sample?.library_type,
        ...(enrichment.graph.libraries ?? []).map((library) =>
            asString(library.library_type),
        ),
    ].filter((value): value is string => Boolean(value));

    for (const libraryType of libraryTypes) {
        appendDetailField(fields, {
            key: "seqmeta_library",
            label: "Library type",
            searchKey: "seqmeta_library",
            value: libraryType,
        });
    }

    appendDetailField(
        fields,
        enrichment.graph.project?.name
            ? {
                  key: "project_name",
                  label: "Project",
                  value: enrichment.graph.project.name,
              }
            : null,
    );
    appendDetailField(
        fields,
        enrichment.graph.users && enrichment.graph.users.length > 0
            ? {
                  key: "project_users",
                  label: "Project users",
                  value: enrichment.graph.users
                      .map((user) => asString(user.username))
                      .filter((value): value is string => Boolean(value))
                      .join(", "),
              }
            : null,
    );
    appendDetailField(
        fields,
        (() => {
            const linkedSamples = Array.from(
                new Set(
                    (libraryMetadata && enrichment.graph.sample
                        ? [
                              enrichment.graph.sample,
                              ...(enrichment.graph.samples ?? []),
                          ]
                        : (enrichment.graph.samples ?? [])
                    )
                        .map((sample) => {
                            const sampleName = asString(sample.sample_name);
                            const sangerId = asString(sample.sanger_id);

                            return [sampleName, sangerId]
                                .filter(Boolean)
                                .join(" / ");
                        })
                        .filter(Boolean),
                ),
            );

            if (linkedSamples.length === 0) {
                return null;
            }

            return {
                key: "linked_samples",
                label: "Linked samples",
                value: linkedSamples.slice(0, 5).join(", "),
            };
        })(),
    );

    return fields;
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
    if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
        return false;
    }

    try {
        await navigator.clipboard.writeText(value);
        return true;
    } catch {
        return false;
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
                        "inline-flex max-w-full items-center rounded-full border border-border/80 px-3 py-1 text-left text-xs font-medium tracking-[0.16em] transition hover:border-primary/45 hover:bg-accent/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
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

                        <div className="grid max-h-[calc(100vh-12rem)] min-h-0 gap-6 overflow-y-auto px-6 py-6 sm:px-7 lg:grid-cols-[minmax(0,1fr)_18rem]">
                            <div className="space-y-3">
                                {detailFields.map((field) => {
                                    const href = field.searchKey
                                        ? `/?${new URLSearchParams({
                                              [field.searchKey]: field.value,
                                          }).toString()}`
                                        : null;

                                    return (
                                        <article
                                            key={`${field.key}:${field.value}`}
                                            data-seqmeta-detail-key={field.key}
                                            className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
                                        >
                                            <div className="flex flex-wrap items-start justify-between gap-3">
                                                <div className="min-w-0">
                                                    <p className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                        {field.label}
                                                    </p>
                                                    <p className="mt-1 font-mono text-[11px] text-muted-foreground">
                                                        {field.key}
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
                                                            ).then((copied) => {
                                                                if (copied) {
                                                                    setCopiedKey(
                                                                        field.key,
                                                                    );
                                                                }
                                                            });
                                                        }}
                                                    >
                                                        <Copy
                                                            className="size-3.5"
                                                            aria-hidden="true"
                                                        />
                                                        {copiedKey === field.key
                                                            ? "Copied"
                                                            : "Copy"}
                                                    </button>
                                                    {href ? (
                                                        <Link
                                                            aria-label={`Send ${field.key} to search filter`}
                                                            className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                            href={href}
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
                                                {field.value}
                                            </p>
                                        </article>
                                    );
                                })}
                            </div>

                            <aside className="space-y-3 rounded-[1.5rem] border border-border/70 bg-background/66 p-4">
                                <div>
                                    <p className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                        Summary
                                    </p>
                                    <p className="mt-2 text-sm leading-6 text-foreground">
                                        {detailFields.length} field
                                        {detailFields.length === 1
                                            ? ""
                                            : "s"}{" "}
                                        available for this seqmeta value.
                                    </p>
                                </div>

                                {statusLines.length > 0 ? (
                                    <div>
                                        <p className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                            Resolution
                                        </p>
                                        <div className="mt-2 space-y-2 text-sm leading-6 text-foreground">
                                            {statusLines.map((line) => (
                                                <p key={line}>{line}</p>
                                            ))}
                                        </div>
                                    </div>
                                ) : null}

                                {missingLines.length > 0 ? (
                                    <div>
                                        <p className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                            Partial response
                                        </p>
                                        <div className="mt-2 space-y-2 text-sm leading-6 text-foreground">
                                            {missingLines.map((line) => (
                                                <p key={line}>{line}</p>
                                            ))}
                                        </div>
                                    </div>
                                ) : null}
                            </aside>
                        </div>
                    </section>
                </div>
            ) : null}
        </>
    );
}
