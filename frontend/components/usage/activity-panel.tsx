import type { UsageCategory, UsageFilter, UsageWindow } from "@/lib/contracts";
import {
    periodChanges,
    usageHref,
    withFilter,
    type BucketUnit,
    type Change,
    type TimeBucket,
    type UsagePeriod,
} from "@/lib/usage-derive";
import { cn } from "@/lib/utils";

import { ActivityChart } from "./activity-chart";
import {
    categoryLabels,
    formatBucketStart,
    formatChange,
    formatCount,
    formatDay,
} from "./format";

const unitTitles: Record<BucketUnit, string> = {
    hour: "Requests per hour",
    day: "Requests per day",
    week: "Requests per week",
};

function ActivityTable({
    buckets,
    categories,
    unit,
}: {
    buckets: TimeBucket[];
    categories: UsageCategory[];
    unit: BucketUnit;
}) {
    const hasSeverity = buckets.some((bucket) => bucket.severity);

    return (
        <div className="overflow-x-auto rounded-lg border border-border/70">
            <table className="w-full text-sm">
                <caption className="sr-only">{unitTitles[unit]}</caption>
                <thead className="bg-muted/40 text-left">
                    <tr>
                        <th className="px-3 py-1.5 font-medium" scope="col">
                            {unit === "hour"
                                ? "Hour (UTC)"
                                : unit === "week"
                                  ? "Week from"
                                  : "Day"}
                        </th>
                        <th
                            className="px-3 py-1.5 text-right font-medium"
                            scope="col"
                        >
                            Requests
                        </th>
                        {categories.map((category) => (
                            <th
                                className="px-3 py-1.5 text-right font-medium"
                                key={category}
                                scope="col"
                            >
                                {categoryLabels[category]}
                            </th>
                        ))}
                        {hasSeverity ? (
                            <>
                                <th
                                    className="px-3 py-1.5 text-right font-medium"
                                    scope="col"
                                >
                                    Faults
                                </th>
                                <th
                                    className="px-3 py-1.5 text-right font-medium"
                                    scope="col"
                                >
                                    Caller errors
                                </th>
                                <th
                                    className="px-3 py-1.5 text-right font-medium"
                                    scope="col"
                                >
                                    Expected
                                </th>
                            </>
                        ) : null}
                    </tr>
                </thead>
                <tbody className="divide-y divide-border/60 tabular-nums">
                    {buckets.map((bucket) => (
                        <tr key={bucket.start}>
                            <th
                                className="px-3 py-1 text-left font-normal"
                                scope="row"
                            >
                                {formatBucketStart(bucket.start, unit)}
                            </th>
                            <td className="px-3 py-1 text-right">
                                {formatCount(bucket.requests)}
                            </td>
                            {categories.map((category) => (
                                <td
                                    className="px-3 py-1 text-right"
                                    key={category}
                                >
                                    {formatCount(
                                        bucket.byCategory[category] ?? 0,
                                    )}
                                </td>
                            ))}
                            {hasSeverity ? (
                                <>
                                    <td className="px-3 py-1 text-right">
                                        {formatCount(
                                            bucket.severity?.fault ?? 0,
                                        )}
                                    </td>
                                    <td className="px-3 py-1 text-right">
                                        {formatCount(
                                            bucket.severity?.caller ?? 0,
                                        )}
                                    </td>
                                    <td className="px-3 py-1 text-right">
                                        {formatCount(
                                            (bucket.severity?.expected ?? 0) +
                                                (bucket.severity?.probe ?? 0),
                                        )}
                                    </td>
                                </>
                            ) : null}
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}

export function ActivityPanel({
    buckets,
    categories,
    clipped,
    earliestRecord,
    unit,
}: {
    buckets: TimeBucket[];
    categories: UsageCategory[];
    clipped: boolean;
    earliestRecord: string;
    unit: BucketUnit;
}) {
    return (
        <section
            aria-labelledby="activity-heading"
            className="flex min-w-0 scroll-mt-6 flex-col gap-2 rounded-xl border border-border/70 bg-card p-4 text-card-foreground shadow-sm"
            id="activity"
        >
            <div className="flex flex-wrap items-baseline justify-between gap-2">
                <h2
                    className="text-lg font-semibold tracking-tight"
                    id="activity-heading"
                >
                    Activity
                </h2>
                <p className="text-sm text-muted-foreground">
                    {unitTitles[unit]}
                    {clipped && earliestRecord
                        ? ` · no records before ${formatDay(earliestRecord)}`
                        : ""}
                </p>
            </div>
            {buckets.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                    No usage recorded yet.
                </p>
            ) : (
                <>
                    <ActivityChart
                        buckets={buckets}
                        categories={categories}
                        unit={unit}
                    />
                    <details>
                        <summary className="cursor-pointer rounded-md text-sm font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                            View as table
                        </summary>
                        <div className="pt-2">
                            <ActivityTable
                                buckets={buckets}
                                categories={categories}
                                unit={unit}
                            />
                        </div>
                    </details>
                </>
            )}
        </section>
    );
}

const changeLabels: Record<Change["kind"], string> = {
    new: "new",
    quiet: "went quiet",
    up: "up",
    down: "down",
};

const dimensionLabels: Record<Change["dimension"], string> = {
    category: "client type",
    endpoint: "endpoint",
    tool: "MCP tool",
    user: "person",
};

function changeName(change: Change): string {
    return change.dimension === "category"
        ? (categoryLabels[change.key as UsageCategory] ?? change.key)
        : change.key;
}

// changeFilter is the filter a change chip narrows the page to.
function changeFilter(change: Change): Partial<UsageFilter> {
    switch (change.dimension) {
        case "category":
            return { category: change.key as UsageCategory };
        case "tool":
            return { tool: change.key };
        case "user":
            return { user: change.key };
        default: {
            const [method, ...route] = change.key.split(" ");

            return { method, route: route.join(" ") };
        }
    }
}

const visibleChanges = 8;

function ChangeChip({
    change,
    filter,
    filterable,
    window,
}: {
    change: Change;
    filter: UsageFilter;
    filterable: boolean;
    window: UsageWindow;
}) {
    const text = (
        <>
            <span
                className={cn(
                    "rounded px-1 text-xs font-semibold uppercase",
                    change.kind === "quiet" || change.kind === "down"
                        ? "bg-muted text-foreground"
                        : "bg-primary/10 text-foreground",
                )}
            >
                {changeLabels[change.kind]}
            </span>{" "}
            <span className="font-mono text-xs wrap-anywhere">
                {changeName(change)}
            </span>{" "}
            <span className="text-xs text-muted-foreground">
                {dimensionLabels[change.dimension]} ·{" "}
                {change.kind === "quiet"
                    ? `${formatCount(change.previous)} before`
                    : change.kind === "new"
                      ? `${formatCount(change.latest)} requests`
                      : `${formatChange(change.latest, change.previous)} (${formatCount(change.previous)} → ${formatCount(change.latest)})`}
            </span>
        </>
    );

    return (
        <li className="rounded-lg border border-border/70 px-2 py-1">
            {filterable ? (
                <a
                    className="hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                    href={usageHref(
                        window,
                        withFilter(filter, changeFilter(change)),
                    )}
                >
                    {text}
                </a>
            ) : (
                text
            )}
        </li>
    );
}

// ChangesPanel lists what changed between the latest window-length period
// and the one before.
export function ChangesPanel({
    earliestRecord,
    filter,
    filterable,
    series,
    window,
}: {
    earliestRecord: string;
    filter: UsageFilter;
    filterable: boolean;
    series: UsagePeriod[];
    window: UsageWindow;
}) {
    const result =
        window === "all" ? null : periodChanges(series, earliestRecord);

    return (
        <section
            aria-labelledby="changes-heading"
            className="flex min-w-0 scroll-mt-6 flex-col gap-2 rounded-xl border border-border/70 bg-card p-4 text-card-foreground shadow-sm"
            id="changes"
        >
            <h2
                className="text-lg font-semibold tracking-tight"
                id="changes-heading"
            >
                What changed
            </h2>
            {result === null ? (
                <p className="text-sm text-muted-foreground">
                    Pick a window to compare it with the one before.
                </p>
            ) : result.status === "short" ? (
                <p className="text-sm text-muted-foreground">
                    Not enough history yet
                    {result.earliest
                        ? ` (records start ${formatDay(result.earliest)})`
                        : ""}
                    .
                </p>
            ) : result.changes.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                    No large changes from the previous period.
                </p>
            ) : (
                <>
                    <p className="text-sm text-muted-foreground">
                        Against the previous{" "}
                        {window === "24h"
                            ? "24 hours"
                            : window === "7d"
                              ? "7 days"
                              : "30 days"}
                        .
                    </p>
                    <ul className="flex flex-col gap-1.5">
                        {result.changes
                            .slice(0, visibleChanges)
                            .map((change) => (
                                <ChangeChip
                                    change={change}
                                    filter={filter}
                                    filterable={filterable}
                                    key={`${change.dimension} ${change.key}`}
                                    window={window}
                                />
                            ))}
                    </ul>
                    {result.changes.length > visibleChanges ? (
                        <details>
                            <summary className="cursor-pointer text-sm font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                                Show all {result.changes.length}
                            </summary>
                            <ul className="flex flex-col gap-1.5 pt-1.5">
                                {result.changes
                                    .slice(visibleChanges)
                                    .map((change) => (
                                        <ChangeChip
                                            change={change}
                                            filter={filter}
                                            filterable={filterable}
                                            key={`${change.dimension} ${change.key}`}
                                            window={window}
                                        />
                                    ))}
                            </ul>
                        </details>
                    ) : null}
                </>
            )}
        </section>
    );
}
