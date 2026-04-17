import type { IdentifierResult } from "@/lib/contracts";
import { cn } from "@/lib/utils";

type SeqmetaBadgeProps = {
    rawValue: string;
    enrichment: IdentifierResult | null;
    error?: boolean;
};

function asRecord(value: unknown): Record<string, unknown> {
    return value && typeof value === "object" && !Array.isArray(value)
        ? (value as Record<string, unknown>)
        : {};
}

function asString(value: unknown): string | null {
    return typeof value === "string" && value.trim() ? value : null;
}

function buildTooltipLines(
    enrichment: IdentifierResult | null,
    error: boolean,
): string[] {
    if (error) {
        return ["enrichment unavailable"];
    }

    if (!enrichment) {
        return [];
    }

    const object = asRecord(enrichment.object);
    const details = [
        asString(object.study_name),
        asString(object.name),
        asString(object.study_accession_number),
        asString(object.accession_number),
        asString(object.sample_name),
        asString(object.id_study_lims),
        asString(object.id_sample_lims),
        asString(object.sanger_id),
        typeof object.id_run === "number" || typeof object.id_run === "string"
            ? `Run ${String(object.id_run)}`
            : null,
        asString(object.library_type)
            ? `Library ${String(object.library_type)}`
            : null,
    ].filter((value): value is string => Boolean(value));

    return [...new Set(details)];
}

export function SeqmetaBadge({
    rawValue,
    enrichment,
    error = false,
}: SeqmetaBadgeProps) {
    const tooltipLines = buildTooltipLines(enrichment, error);
    const inlineLabel = enrichment ? `${enrichment.type}: ${rawValue}` : rawValue;

    return (
        <span className="group relative inline-flex items-center gap-2 align-middle">
            <span
                className={cn(
                    "inline-flex items-center rounded-full border border-border/80 px-3 py-1 text-xs font-medium tracking-[0.16em]",
                    enrichment
                        ? "bg-accent/20 text-foreground"
                        : "bg-background/80 text-muted-foreground",
                )}
            >
                {inlineLabel}
            </span>

            {error ? (
                <span
                    aria-label="enrichment unavailable"
                    className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-border/80 bg-background text-[11px] font-semibold text-muted-foreground"
                >
                    ?
                </span>
            ) : null}

            {tooltipLines.length > 0 ? (
                <span
                    role="tooltip"
                    className="pointer-events-none absolute left-0 top-full z-20 mt-2 hidden min-w-52 rounded-2xl border border-border/80 bg-popover px-3 py-2 text-xs leading-5 text-popover-foreground shadow-[0_20px_80px_-48px_rgba(30,45,63,0.75)] group-hover:block group-focus-within:block"
                >
                    {tooltipLines.map((line) => (
                        <span key={line} className="block whitespace-nowrap">
                            {line}
                        </span>
                    ))}
                </span>
            ) : null}
        </span>
    );
}
