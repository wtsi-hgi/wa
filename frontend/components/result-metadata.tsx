import { SeqmetaBadge } from "@/components/seqmeta-badge";
import type { EnrichmentResult } from "@/lib/contracts";

type ResultMetadataProps = {
    enrichments?: Record<string, EnrichmentResult | null>;
    errors?: Record<string, "not_found" | "upstream_impaired">;
    loading?: Record<string, boolean>;
    metadata: Record<string, string>;
};

function isSeqmetaKey(key: string): boolean {
    return key.startsWith("seqmeta_");
}

function seqmetaLookupKey(value: string): string {
    return value.trim();
}

function renderPanelValue(value: string | undefined): string | null {
    return value && value.trim() ? value : null;
}

function buildSampleSummary(enrichment: EnrichmentResult): string[] {
    const samples = enrichment.graph.samples ?? [];

    if (samples.length === 0) {
        return [];
    }

    const preview = samples.slice(0, 3).map((sample) => {
        const sampleName = renderPanelValue(sample.sample_name);
        const sangerId = renderPanelValue(sample.sanger_id);

        return [sampleName, sangerId].filter(Boolean).join(" / ");
    });

    if (samples.length > 3) {
        preview.push(`${samples.length} samples linked`);
    }

    return preview;
}

function EnrichmentPanels({ enrichment }: { enrichment: EnrichmentResult }) {
    const study = enrichment.graph.study;
    const libraries = enrichment.graph.libraries ?? [];
    const sampleSummary = buildSampleSummary(enrichment);

    if (!study && libraries.length === 0 && sampleSummary.length === 0) {
        return null;
    }

    return (
        <div className="grid gap-3 pt-1 sm:grid-cols-3">
            {study ? (
                <section className="rounded-[1.15rem] border border-border/70 bg-background/55 px-3 py-3">
                    <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                        Study
                    </p>
                    <p className="mt-2 text-sm font-medium text-foreground">
                        {study.name}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                        {[study.id_study_lims, study.accession_number]
                            .filter(Boolean)
                            .join(" • ")}
                    </p>
                </section>
            ) : null}

            {libraries.length > 0 ? (
                <section className="rounded-[1.15rem] border border-border/70 bg-background/55 px-3 py-3">
                    <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                        Libraries
                    </p>
                    <div className="mt-2 flex flex-wrap gap-2">
                        {libraries.slice(0, 4).map((library) => (
                            <span
                                key={`${library.id_study_lims}:${library.library_type}`}
                                className="rounded-full border border-border/70 bg-card/80 px-2.5 py-1 text-xs text-foreground"
                            >
                                {library.library_type}
                            </span>
                        ))}
                    </div>
                </section>
            ) : null}

            {sampleSummary.length > 0 ? (
                <section className="rounded-[1.15rem] border border-border/70 bg-background/55 px-3 py-3">
                    <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                        Samples
                    </p>
                    <div className="mt-2 space-y-1 text-xs text-foreground">
                        {sampleSummary.map((line) => (
                            <p key={line}>{line}</p>
                        ))}
                    </div>
                </section>
            ) : null}
        </div>
    );
}

export function ResultMetadata({
    enrichments = {},
    errors = {},
    loading = {},
    metadata,
}: ResultMetadataProps) {
    const entries = Object.entries(metadata);

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
                <div className="mt-6 overflow-visible rounded-[1.5rem] border border-border/70">
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
                                        {key}
                                    </td>
                                    <td className="px-4 py-3">
                                        {isSeqmetaKey(key) ? (
                                            <div className="space-y-3">
                                                <SeqmetaBadge
                                                    rawValue={value}
                                                    enrichment={
                                                        enrichments[
                                                            seqmetaLookupKey(
                                                                value,
                                                            )
                                                        ] ?? null
                                                    }
                                                    error={
                                                        errors[
                                                            seqmetaLookupKey(
                                                                value,
                                                            )
                                                        ]
                                                    }
                                                    loading={Boolean(
                                                        loading[
                                                            seqmetaLookupKey(
                                                                value,
                                                            )
                                                        ],
                                                    )}
                                                />
                                                {enrichments[
                                                    seqmetaLookupKey(value)
                                                ] ? (
                                                    <EnrichmentPanels
                                                        enrichment={
                                                            enrichments[
                                                                seqmetaLookupKey(
                                                                    value,
                                                                )
                                                            ]!
                                                        }
                                                    />
                                                ) : null}
                                            </div>
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
