import type { ReactNode } from "react";

import type { UsageSummary } from "@/lib/contracts";
import {
    agentProducts,
    previousComparable,
    rate,
    type TimeBucket,
} from "@/lib/usage-derive";
import { cn } from "@/lib/utils";

import {
    categoryLabels,
    formatChange,
    formatCount,
    formatDifference,
    formatRate,
    pluralise,
} from "./format";
import { Sparkline } from "./sparkline";

function KpiTile({
    change,
    children,
    detail,
    href,
    label,
    sparkline,
    tone = "default",
}: {
    change?: string;
    children: ReactNode;
    detail: ReactNode;
    href?: string;
    label: string;
    sparkline?: ReactNode;
    tone?: "default" | "alert";
}) {
    return (
        <div
            className={cn(
                "flex flex-col gap-1 rounded-xl border bg-card p-4 text-card-foreground shadow-sm",
                tone === "alert" ? "border-destructive/60" : "border-border/70",
            )}
            data-testid={`kpi-${label}`}
        >
            <dt className="text-sm text-muted-foreground">{label}</dt>
            <dd className="flex flex-wrap items-baseline gap-x-2">
                <span
                    className={cn(
                        "text-3xl font-semibold tracking-tight",
                        tone === "alert" && "text-destructive",
                    )}
                    data-testid="kpi-value"
                >
                    {children}
                </span>
                {change ? (
                    <span
                        className="text-sm text-muted-foreground"
                        data-testid="kpi-change"
                    >
                        {change}
                    </span>
                ) : null}
            </dd>
            {sparkline ? <dd className="min-h-7">{sparkline}</dd> : null}
            <dd className="text-sm text-muted-foreground">
                {href ? (
                    <a
                        className="underline decoration-border underline-offset-4 hover:decoration-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                        href={href}
                    >
                        {detail}
                    </a>
                ) : (
                    detail
                )}
            </dd>
        </div>
    );
}

// changeText describes latest against the previous window, or why there is
// no comparison.
function changeText(
    comparable: boolean,
    latest: number,
    previous: number | undefined,
    format: (latest: number, previous: number) => string,
): string | undefined {
    if (previous === undefined) {
        return undefined;
    }

    return comparable
        ? `${format(latest, previous)} vs previous`
        : "no earlier records";
}

export function KpiStrip({
    buckets,
    summary,
}: {
    buckets: TimeBucket[];
    summary: UsageSummary;
}) {
    const { totals } = summary;
    const previous = summary.previous_totals ?? undefined;
    const comparable = previousComparable(summary);
    const severity = totals.error_severity;
    const mcp = summary.categories.find((row) => row.category === "mcp");
    const mcpPrevious = summary.series.at(-2)?.by_category.mcp ?? 0;
    const shares = [...summary.categories]
        .filter((row) => row.requests > 0)
        .sort((a, b) => b.requests - a.requests)
        .slice(0, 2)
        .map(
            (row) =>
                `${categoryLabels[row.category]} ${formatRate(rate(row.requests, totals.requests))}`,
        );
    const busiest = [...summary.categories].sort(
        (a, b) => b.sessions - a.sessions,
    )[0];

    return (
        <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
            <KpiTile
                change={changeText(
                    comparable,
                    totals.requests,
                    previous?.requests,
                    formatChange,
                )}
                detail={shares.join(" · ") || "No requests"}
                label="Requests"
                sparkline={
                    <Sparkline values={buckets.map((b) => b.requests)} />
                }
            >
                {formatCount(totals.requests)}
            </KpiTile>
            <KpiTile
                change={changeText(
                    comparable,
                    totals.users,
                    previous?.users,
                    formatDifference,
                )}
                detail="Self-declared usernames"
                label="People"
            >
                {formatCount(totals.users)}
            </KpiTile>
            <KpiTile
                change={changeText(
                    comparable,
                    totals.sessions,
                    previous?.sessions,
                    formatDifference,
                )}
                detail={
                    busiest && busiest.sessions > 0
                        ? `${categoryLabels[busiest.category]}: median ${formatCount(busiest.requests_per_session_median)}, max ${formatCount(busiest.requests_per_session_max)} requests per session`
                        : "No sessions"
                }
                label="Sessions"
            >
                {formatCount(totals.sessions)}
            </KpiTile>
            <KpiTile
                // previous_totals has no categories; the previous series
                // period is the same window.
                change={changeText(
                    comparable && summary.series.length >= 2,
                    mcp?.requests ?? 0,
                    previous ? mcpPrevious : undefined,
                    formatChange,
                )}
                detail={`${pluralise(mcp?.sessions ?? 0, "session")} · ${pluralise(agentProducts(summary.mcp_agents).length, "agent")}`}
                label="MCP requests"
                sparkline={
                    <Sparkline
                        color="var(--usage-mcp)"
                        values={buckets.map((b) => b.byCategory.mcp ?? 0)}
                    />
                }
            >
                {formatCount(mcp?.requests ?? 0)}
            </KpiTile>
            {severity ? (
                <KpiTile
                    change={changeText(
                        comparable,
                        severity.fault,
                        previous?.error_severity?.fault,
                        formatDifference,
                    )}
                    detail={`${formatRate(rate(severity.fault, totals.requests))} of requests · ${pluralise(severity.caller, "caller error")}`}
                    href="#attention"
                    label="Faults"
                    sparkline={
                        <Sparkline
                            color="var(--usage-fault)"
                            values={buckets.map((b) => b.severity?.fault ?? 0)}
                        />
                    }
                    tone={severity.fault > 0 ? "alert" : "default"}
                >
                    {formatCount(severity.fault)}
                </KpiTile>
            ) : (
                <KpiTile
                    detail={`${formatRate(totals.error_rate)} of requests; this server does not classify errors`}
                    href="#attention"
                    label="Errors"
                >
                    {formatCount(totals.errors)}
                </KpiTile>
            )}
        </dl>
    );
}
