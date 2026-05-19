import { Info } from "lucide-react";

import { SeqmetaBadge } from "@/components/seqmeta-badge";
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@/components/ui/popover";
import type { EnrichmentResult } from "@/lib/contracts";
import { canonicalSeqmetaKey, isSeqmetaKey } from "@/lib/seqmeta-keys";

type ResultMetadataProps = {
    enrichments?: Record<string, EnrichmentResult | null>;
    errors?: Record<string, "not_found" | "upstream_impaired">;
    loading?: Record<string, boolean>;
    metadata: Record<string, string>;
    variant?: "section" | "integrated";
};

function seqmetaLookupKey(value: string): string {
    return value.trim();
}

function displayMetadataKey(key: string): string {
    return canonicalSeqmetaKey(key);
}

function displayMetadataStripKey(key: string): string {
    const displayKey = canonicalSeqmetaKey(key);

    if (
        displayKey === "seqmeta_id_study_lims" ||
        displayKey === "seqmeta_study_accession"
    ) {
        return "Study";
    }

    if (
        displayKey === "seqmeta_sample_name" ||
        displayKey === "seqmeta_sanger_sample_id" ||
        displayKey === "seqmeta_supplier_name" ||
        displayKey === "seqmeta_id_sample_lims"
    ) {
        return "Sample";
    }

    if (
        displayKey === "seqmeta_library_id" ||
        displayKey === "seqmeta_id_library_lims" ||
        displayKey === "seqmeta_pipeline_id_lims"
    ) {
        return "Library";
    }

    if (displayKey === "seqmeta_id_run") {
        return "Run";
    }

    if (displayKey === "seqmeta_lane" || displayKey === "seqmeta_tag_index") {
        return "Lane";
    }

    return displayMetadataKey(key);
}

function visibleIntegratedEntries(entries: [string, string][]) {
    const seqmetaEntries = entries.filter(([key]) => isSeqmetaKey(key));

    if (seqmetaEntries.length > 0) {
        return seqmetaEntries;
    }

    return entries.slice(0, 3);
}

function MetadataValue({
    display = "strip",
    enrichments,
    errors,
    loading,
    metadataKey,
    value,
}: {
    display?: "detail" | "strip";
    enrichments: Record<string, EnrichmentResult | null>;
    errors: Record<string, "not_found" | "upstream_impaired">;
    loading: Record<string, boolean>;
    metadataKey: string;
    value: string;
}) {
    if (isSeqmetaKey(metadataKey)) {
        const lookupKey = seqmetaLookupKey(value);

        return (
            <SeqmetaBadge
                metadataKey={metadataKey}
                rawValue={value}
                enrichment={enrichments[lookupKey] ?? null}
                error={errors[lookupKey]}
                loading={Boolean(loading[lookupKey])}
            />
        );
    }

    return (
        <span
            className={
                display === "detail"
                    ? "break-words text-sm leading-5 text-foreground"
                    : "min-w-0 truncate text-xs font-medium text-foreground"
            }
        >
            {value}
        </span>
    );
}

export function ResultMetadata({
    enrichments = {},
    errors = {},
    loading = {},
    metadata,
    variant = "section",
}: ResultMetadataProps) {
    const entries = Object.entries(metadata);
    const visibleEntries = visibleIntegratedEntries(entries);
    const hasHiddenEntries = entries.length > visibleEntries.length;

    if (variant === "integrated") {
        return (
            <div
                className="min-w-0 space-y-2"
                data-result-metadata-layout="integrated"
            >
                <div className="flex items-center justify-between gap-2">
                    <p className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                        Metadata
                    </p>
                    {hasHiddenEntries ? (
                        <Popover>
                            <PopoverTrigger
                                className="inline-flex min-h-7 items-center gap-1.5 rounded-full border border-border/70 bg-card/70 px-2.5 py-0.5 text-xs font-medium text-muted-foreground transition hover:border-primary/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
                                data-metadata-details-trigger="true"
                            >
                                <Info
                                    className="h-3.5 w-3.5"
                                    aria-hidden="true"
                                />
                                <span>All metadata</span>
                            </PopoverTrigger>
                            <PopoverContent
                                align="end"
                                className="w-[min(92vw,42rem)] p-4"
                            >
                                <div className="flex items-center justify-between gap-3">
                                    <p className="text-sm font-semibold text-foreground">
                                        Metadata
                                    </p>
                                    <p className="text-xs text-muted-foreground">
                                        {entries.length}{" "}
                                        {entries.length === 1 ? "key" : "keys"}
                                    </p>
                                </div>
                                <dl
                                    className="mt-3 grid max-h-[min(24rem,70vh)] gap-2 overflow-auto pr-1 sm:grid-cols-2"
                                    data-result-metadata-details-panel="true"
                                >
                                    {entries.map(([key, value]) => (
                                        <div
                                            key={key}
                                            className="min-w-0 rounded-lg border border-border/60 bg-background/70 px-3 py-2"
                                            data-metadata-detail-row={key}
                                        >
                                            <dt className="break-all font-mono text-[11px] text-muted-foreground">
                                                {key}
                                            </dt>
                                            <dd className="mt-1 min-w-0">
                                                <MetadataValue
                                                    display="detail"
                                                    enrichments={enrichments}
                                                    errors={errors}
                                                    loading={loading}
                                                    metadataKey={key}
                                                    value={value}
                                                />
                                            </dd>
                                        </div>
                                    ))}
                                </dl>
                            </PopoverContent>
                        </Popover>
                    ) : null}
                </div>

                {entries.length === 0 ? (
                    <p className="inline-flex min-h-8 items-center rounded-full border border-border/65 bg-background/70 px-3 py-1 text-xs text-muted-foreground">
                        No metadata
                    </p>
                ) : (
                    <dl
                        className="flex max-h-20 flex-wrap gap-1.5 overflow-auto pr-1"
                        data-result-metadata-strip="true"
                    >
                        {visibleEntries.map(([key, value]) => (
                            <div
                                key={key}
                                className="inline-flex min-h-7 max-w-full items-center gap-1.5 rounded-full border border-border/65 bg-background/70 px-2 py-0.5 text-xs shadow-[0_10px_28px_-26px_rgba(28,40,58,0.72)]"
                                data-metadata-row={key}
                            >
                                <dt className="min-w-0 shrink truncate font-mono text-[11px] text-muted-foreground">
                                    {displayMetadataStripKey(key)}
                                </dt>
                                <dd className="min-w-0">
                                    <MetadataValue
                                        enrichments={enrichments}
                                        errors={errors}
                                        loading={loading}
                                        metadataKey={key}
                                        value={value}
                                    />
                                </dd>
                            </div>
                        ))}
                        {hasHiddenEntries ? (
                            <div className="inline-flex min-h-7 items-center rounded-full border border-border/65 bg-card/70 px-2.5 py-0.5 text-xs text-muted-foreground">
                                +{entries.length - visibleEntries.length}
                            </div>
                        ) : null}
                    </dl>
                )}
            </div>
        );
    }

    return (
        <section className="rounded-[1.75rem] border border-border/70 bg-card/85 p-6 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]">
            <div className="space-y-2">
                <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
                    Result metadata
                </p>
                <h2 className="text-2xl font-semibold tracking-tight">
                    Metadata keys and values
                </h2>
            </div>

            {entries.length === 0 ? (
                <p className="mt-6 text-sm leading-7 text-muted-foreground">
                    No metadata
                </p>
            ) : (
                <div className="mt-6 overflow-hidden rounded-[1.5rem] border border-border/70">
                    <table className="min-w-full divide-y divide-border/70 text-left text-sm">
                        <thead className="bg-muted/40">
                            <tr>
                                <th className="px-4 py-3 font-medium">Key</th>
                                <th className="px-4 py-3 font-medium">Value</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-border/60 bg-card/60">
                            {entries.map(([key, value]) => (
                                <tr key={key} data-metadata-row={key}>
                                    <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                                        {displayMetadataKey(key)}
                                    </td>
                                    <td className="px-4 py-3">
                                        {isSeqmetaKey(key) ? (
                                            <SeqmetaBadge
                                                metadataKey={key}
                                                rawValue={value}
                                                enrichment={
                                                    enrichments[
                                                        seqmetaLookupKey(value)
                                                    ] ?? null
                                                }
                                                error={
                                                    errors[
                                                        seqmetaLookupKey(value)
                                                    ]
                                                }
                                                loading={Boolean(
                                                    loading[
                                                        seqmetaLookupKey(value)
                                                    ],
                                                )}
                                            />
                                        ) : (
                                            <span className="text-sm text-foreground">
                                                {value}
                                            </span>
                                        )}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </section>
    );
}
