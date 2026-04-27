import type { EnrichmentResult, MissingHop } from "@/lib/contracts";
import { cn } from "@/lib/utils";

type SeqmetaBadgeProps = {
    rawValue: string;
    enrichment: EnrichmentResult | null;
    error?: "not_found" | "upstream_impaired";
    loading?: boolean;
};

function asString(value: unknown): string | null {
    return typeof value === "string" && value.trim() ? value : null;
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

    return `${enrichment.type}: ${rawValue}`;
}

function primaryDetails(enrichment: EnrichmentResult | null): string[] {
    if (!enrichment) {
        return [];
    }

    const details = [
        asString(enrichment.graph.study?.name),
        asString(enrichment.graph.study?.accession_number),
        asString(enrichment.graph.sample?.sample_name),
        asString(enrichment.graph.sample?.sanger_id),
        asString(enrichment.graph.sample?.accession_number),
        asString(enrichment.graph.library?.library_type),
        ...(enrichment.graph.libraries ?? []).map((library) =>
            asString(library.library_type),
        ),
        ...(enrichment.graph.samples ?? []).flatMap((sample) => [
            asString(sample.sample_name),
            asString(sample.sanger_id),
        ]),
    ].filter((value): value is string => Boolean(value));

    return [...new Set(details)].slice(0, 6);
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

function buildTooltipLines(
    enrichment: EnrichmentResult | null,
    error: SeqmetaBadgeProps["error"],
    loading: boolean,
): string[] {
    if (loading) {
        return ["Loading enrichment..."];
    }

    if (error === "not_found") {
        return ["enrichment unavailable"];
    }

    if (error === "upstream_impaired") {
        return ["Backend could not reach upstream"];
    }

    if (!enrichment) {
        return [];
    }

    return primaryDetails(enrichment);
}

export function SeqmetaBadge({
    rawValue,
    enrichment,
    error,
    loading = false,
}: SeqmetaBadgeProps) {
    const tooltipLines = buildTooltipLines(enrichment, error, loading);
    const inlineLabel = primaryLabel(rawValue, enrichment);
    const missingLines = enrichment?.partial
        ? (enrichment.missing ?? []).map(humanizeMissingHop)
        : [];

    return (
        <span className="group relative inline-flex max-w-full flex-col items-start gap-3 align-middle">
            <span className="inline-flex items-center gap-2">
                <span
                    data-testid="seqmeta-badge-label"
                    className={cn(
                        "inline-flex items-center rounded-full border border-border/80 px-3 py-1 text-xs font-medium tracking-[0.16em]",
                        enrichment
                            ? "bg-accent/20 text-foreground"
                            : "bg-background/80 text-muted-foreground",
                    )}
                >
                    {inlineLabel}
                </span>

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

            {missingLines.length > 0 ? (
                <span className="rounded-2xl border border-dashed border-border/70 bg-background/70 px-3 py-2 text-xs leading-5 text-muted-foreground">
                    <span className="block font-medium text-foreground">
                        Some details unavailable
                    </span>
                    <ul className="mt-1 list-disc pl-4">
                        {missingLines.map((line) => (
                            <li key={line}>{line}</li>
                        ))}
                    </ul>
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
