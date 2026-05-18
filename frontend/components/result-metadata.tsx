import { SeqmetaBadge } from "@/components/seqmeta-badge";
import type { EnrichmentResult } from "@/lib/contracts";

type ResultMetadataProps = {
    enrichments?: Record<string, EnrichmentResult | null>;
    errors?: Record<string, "not_found" | "upstream_impaired">;
    loading?: Record<string, boolean>;
    metadata: Record<string, string>;
    variant?: "section" | "integrated";
};

function isSeqmetaKey(key: string): boolean {
    return key.startsWith("seqmeta_");
}

function seqmetaLookupKey(value: string): string {
    return value.trim();
}

export function ResultMetadata({
    enrichments = {},
    errors = {},
    loading = {},
    metadata,
    variant = "section",
}: ResultMetadataProps) {
    const entries = Object.entries(metadata);

    if (variant === "integrated") {
        return (
            <div className="space-y-3" data-result-metadata-layout="integrated">
                <div className="flex items-center justify-between gap-3">
                    <p className="text-sm font-semibold text-foreground">
                        Metadata
                    </p>
                    <p className="text-xs text-muted-foreground">
                        {entries.length} {entries.length === 1 ? "key" : "keys"}
                    </p>
                </div>

                {entries.length === 0 ? (
                    <p className="rounded-lg border border-border/60 bg-background/65 px-3 py-2 text-sm text-muted-foreground">
                        No metadata
                    </p>
                ) : (
                    <dl className="grid max-h-72 gap-2 overflow-auto pr-1">
                        {entries.map(([key, value]) => (
                            <div
                                key={key}
                                className="min-w-0 rounded-lg border border-border/60 bg-background/65 px-3 py-2"
                                data-metadata-row={key}
                            >
                                <dt className="break-all font-mono text-xs text-muted-foreground">
                                    {key}
                                </dt>
                                <dd className="mt-1 min-w-0">
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
                                                errors[seqmetaLookupKey(value)]
                                            }
                                            loading={Boolean(
                                                loading[
                                                    seqmetaLookupKey(value)
                                                ],
                                            )}
                                        />
                                    ) : (
                                        <span className="break-all text-sm text-foreground">
                                            {value}
                                        </span>
                                    )}
                                </dd>
                            </div>
                        ))}
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
                                        {key}
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
