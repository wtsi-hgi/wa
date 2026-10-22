import {
    CircleAlert,
    CircleCheck,
    OctagonAlert,
    TriangleAlert,
} from "lucide-react";

import { LocalTimestamp } from "@/components/local-timestamp";
import type { UsageSummary } from "@/lib/contracts";
import { usageGroupHelp } from "@/lib/usage-error-help";
import {
    attentionRows,
    expectedGroups,
    type AttentionRow,
} from "@/lib/usage-derive";
import { cn } from "@/lib/utils";

import { RouteErrorList } from "./error-group-entry";
import {
    formatCategories,
    formatCount,
    formatCounts,
    formatMs,
    pluralise,
} from "./format";

// slowP95Ms is the p95 latency above which an endpoint is a notice.
const slowP95Ms = 2000;

const severityLabels: Record<AttentionRow["severity"], string> = {
    fault: "Fault",
    caller: "Caller error",
};

// rowHelp is the help for a merged row's code, inferred from its status for
// rows recorded before error codes.
function rowHelp(row: AttentionRow) {
    return usageGroupHelp(
        row.code,
        row.status,
        row.routes[0]?.groups[0]?.route ?? "",
    );
}

// rowTitle names a merged row: its status and code, and where it happened.
function rowTitle(row: AttentionRow): string {
    const where =
        row.routes.length === 1
            ? row.routes[0].label
            : `${row.routes.length} routes`;

    return `${row.status} ${rowHelp(row).label} on ${where}`;
}

function AttentionEntry({
    row,
    unmatched,
}: {
    row: AttentionRow;
    unmatched: UsageSummary["unmatched"];
}) {
    const help = rowHelp(row);
    // Unmatched paths from the row's callers: wa's own clients for a fault
    // row, other callers for a caller row.
    const paths = unmatched.filter((path) =>
        Object.keys(path.by_category).some(
            (category) => row.categories[category],
        ),
    );
    const Icon = row.severity === "fault" ? OctagonAlert : TriangleAlert;
    const callers = [
        formatCategories(row.categories),
        formatCounts(row.tools),
        formatCounts(row.agents),
    ]
        .filter(Boolean)
        .join(" · ");

    return (
        <li className="scroll-mt-6" id={row.id}>
            <details className="group rounded-xl border border-border/70 bg-card text-card-foreground">
                <summary className="flex cursor-pointer list-none flex-col gap-1 rounded-xl px-4 py-3 hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                    <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                        <Icon
                            aria-hidden="true"
                            className={cn(
                                "size-4 shrink-0 self-center",
                                row.severity === "fault"
                                    ? "text-destructive"
                                    : "text-[var(--usage-caller)]",
                            )}
                        />
                        <span className="sr-only">
                            {severityLabels[row.severity]}:
                        </span>
                        <span className="font-medium wrap-anywhere">
                            {rowTitle(row)}
                        </span>
                        <span className="text-sm font-semibold tabular-nums">
                            {formatCount(row.count)}
                        </span>
                        <span className="text-xs text-muted-foreground wrap-anywhere">
                            {callers}
                        </span>
                        <span className="text-xs text-muted-foreground">
                            last{" "}
                            <LocalTimestamp
                                format="dateTime"
                                value={row.lastSeen}
                            />
                        </span>
                    </span>
                    <span className="max-w-3xl pl-7 text-sm text-muted-foreground">
                        {help.action}
                    </span>
                </summary>
                <div className="flex flex-col gap-3 px-4 pb-4">
                    <p className="max-w-3xl text-sm">{help.meaning}</p>
                    {row.code === "no_route" && paths.length > 0 ? (
                        <div className="flex flex-col gap-1">
                            <h3 className="text-sm font-medium">
                                Unmatched paths
                            </h3>
                            <ul className="flex flex-col divide-y divide-border/60 rounded-lg border border-border/70">
                                {paths.map((path, index) => (
                                    <li
                                        className="flex items-baseline justify-between gap-4 px-3 py-1.5 text-sm"
                                        key={`${index} ${path.method} ${path.path}`}
                                    >
                                        <span className="font-mono text-xs wrap-anywhere">
                                            {path.method} {path.path}
                                        </span>
                                        <span className="text-xs text-muted-foreground">
                                            {[
                                                formatCategories(
                                                    path.by_category,
                                                ),
                                                formatCounts(path.by_tool),
                                                formatCounts(path.by_agent),
                                            ]
                                                .filter(Boolean)
                                                .join(" · ")}
                                        </span>
                                        <span className="tabular-nums">
                                            {formatCount(path.requests)}
                                        </span>
                                    </li>
                                ))}
                            </ul>
                        </div>
                    ) : null}
                    <RouteErrorList headingLevel={3} routes={row.routes} />
                </div>
            </details>
        </li>
    );
}

// AttentionPanel lists what needs fixing: fault rows, then caller rows, then
// notices, each merged across routes with the action to take. It stays on the
// page when there is nothing to fix.
export function AttentionPanel({ summary }: { summary: UsageSummary }) {
    const errors = summary.errors;
    const rows = attentionRows(errors ?? []);
    const faults = rows
        .filter((row) => row.severity === "fault")
        .reduce((sum, row) => sum + row.count, 0);
    const callers = rows
        .filter((row) => row.severity === "caller")
        .reduce((sum, row) => sum + row.count, 0);
    const expected = expectedGroups(errors ?? []).reduce(
        (sum, group) => sum + group.count,
        0,
    );
    const shown = (errors ?? []).reduce((sum, group) => sum + group.count, 0);
    const slow = summary.endpoints.filter(
        (endpoint) => endpoint.p95_ms > slowP95Ms,
    );
    const notices: string[] = [];

    if (summary.dropped_events > 0) {
        notices.push(
            `${pluralise(summary.dropped_events, "request was", "requests were")} not recorded because the usage queue was full.`,
        );
    }

    for (const endpoint of slow) {
        notices.push(
            `${endpoint.method} ${endpoint.route} is slow: p95 ${formatMs(endpoint.p95_ms)}.`,
        );
    }

    if (errors !== undefined && shown < summary.totals.errors) {
        notices.push(
            `Showing the ${formatCount(errors.length)} largest error groups (${formatCount(shown)} of ${formatCount(summary.totals.errors)} errors).`,
        );
    }

    return (
        <section
            aria-labelledby="attention-heading"
            className="flex scroll-mt-6 flex-col gap-3"
            id="attention"
        >
            <div className="flex flex-wrap items-baseline justify-between gap-2">
                <h2
                    className="text-lg font-semibold tracking-tight"
                    id="attention-heading"
                >
                    Needs attention
                </h2>
                {errors !== undefined ? (
                    <p className="text-sm text-muted-foreground">
                        {pluralise(faults, "fault")} ·{" "}
                        {pluralise(callers, "caller error")}
                    </p>
                ) : null}
            </div>
            {errors === undefined ? (
                <p className="rounded-xl border border-dashed border-border/70 p-4 text-sm text-muted-foreground">
                    This MLWH server does not report error details. Upgrade wa
                    mlwh serve to see them.
                </p>
            ) : rows.length === 0 ? (
                <p className="flex items-center gap-2 rounded-xl border border-border/70 bg-card p-4 text-sm">
                    <CircleCheck
                        aria-hidden="true"
                        className="size-4 text-[var(--usage-good)]"
                    />
                    No faults or caller errors in this window.
                </p>
            ) : (
                <ul className="flex flex-col gap-2">
                    {rows.map((row) => (
                        <AttentionEntry
                            key={row.id}
                            row={row}
                            unmatched={summary.unmatched}
                        />
                    ))}
                </ul>
            )}
            {notices.length > 0 ? (
                <ul className="flex flex-col gap-1 text-sm">
                    {notices.map((notice) => (
                        <li className="flex items-start gap-2" key={notice}>
                            <CircleAlert
                                aria-hidden="true"
                                className="mt-0.5 size-4 shrink-0 text-muted-foreground"
                            />
                            {notice}
                        </li>
                    ))}
                </ul>
            ) : null}
            {expected > 0 ? (
                <p className="text-sm text-muted-foreground">
                    Expected lookup misses are not problems:{" "}
                    {formatCount(expected)} under{" "}
                    <a
                        className="underline decoration-border underline-offset-4 hover:decoration-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                        href="#expected"
                    >
                        Expected outcomes
                    </a>
                    .
                </p>
            ) : null}
        </section>
    );
}
