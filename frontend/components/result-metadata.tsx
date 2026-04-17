import { SeqmetaBadge } from "@/components/seqmeta-badge";
import type { IdentifierResult } from "@/lib/contracts";

type ResultMetadataProps = {
    enrichments?: Record<string, IdentifierResult | null>;
    errors?: Record<string, boolean>;
    metadata: Record<string, string>;
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
                                                rawValue={value}
                                                enrichment={
                                                    enrichments[seqmetaLookupKey(value)] ?? null
                                                }
                                                error={Boolean(errors[seqmetaLookupKey(value)])}
                                            />
                                        ) : (
                                            <span className="text-sm text-foreground">{value}</span>
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
