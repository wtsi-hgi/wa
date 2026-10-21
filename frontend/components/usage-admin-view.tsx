import Link from "next/link";

import { LocalTimestamp } from "@/components/local-timestamp";
import { ActivityPanel, ChangesPanel } from "@/components/usage/activity-panel";
import { AttentionPanel } from "@/components/usage/attention-panel";
import {
    ExpectedPanel,
    OlderErrorsPanel,
    SessionsPanel,
} from "@/components/usage/detail-panels";
import { FilterBar } from "@/components/usage/filter-bar";
import {
    categoryLabels,
    formatDay,
    windowLabels,
} from "@/components/usage/format";
import { HashOpener } from "@/components/usage/hash-opener";
import { KpiStrip } from "@/components/usage/kpi-strip";
import {
    EndpointsPanel,
    PeoplePanel,
    ToolsPanel,
    ViaPanel,
} from "@/components/usage/ranked-panels";
import {
    usageWindowSchema,
    type UsageFilter,
    type UsageSummary,
    type UsageWindow,
} from "@/lib/contracts";
import {
    activeCategories,
    clipBuckets,
    errorAnchors,
    isFiltered,
    timeBuckets,
    usageHref,
} from "@/lib/usage-derive";

export type UsageAdminViewProps = {
    filter?: UsageFilter;
    summary: UsageSummary;
    window: UsageWindow;
};

// UsageAdminView is the admin usage dashboard: KPIs, what needs fixing,
// activity and changes, ranked breakdowns, then collapsed detail. Window and
// filters are URL state, so every view can be linked.
export function UsageAdminView({
    filter = {},
    summary,
    window,
}: UsageAdminViewProps) {
    // Servers that predate filters do not echo one and ignore the params.
    const filterable = summary.filter !== undefined;
    const { buckets: allBuckets, unit } = timeBuckets(summary);
    const { buckets, clipped } = clipBuckets(
        allBuckets,
        unit,
        summary.earliest_record,
    );
    const { active, idle } = activeCategories(summary);
    const anchors = errorAnchors(summary.errors ?? []);
    const panelProps = { filter, filterable, summary, window };

    return (
        <main className="mx-auto flex min-h-screen w-full max-w-[84rem] flex-col gap-5 px-4 py-6 sm:px-8 lg:py-8">
            <HashOpener />
            <header className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
                <div className="flex flex-col gap-1">
                    <h1 className="text-3xl font-semibold tracking-tight">
                        MLWH usage
                    </h1>
                    <p className="text-sm text-muted-foreground">
                        Generated{" "}
                        <LocalTimestamp
                            format="dateTime"
                            value={summary.generated_at}
                        />
                        {summary.earliest_record
                            ? ` · records start ${formatDay(summary.earliest_record)}`
                            : " · no records yet"}
                    </p>
                </div>
                <nav aria-label="Window">
                    <ul className="flex flex-wrap gap-1 rounded-lg border border-border/70 bg-muted/40 p-1 text-sm">
                        {usageWindowSchema.options.map((option) => (
                            <li key={option}>
                                <Link
                                    aria-current={
                                        option === window ? "page" : undefined
                                    }
                                    className="block rounded-md px-3 py-1.5 font-medium text-muted-foreground transition-colors hover:bg-background hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none aria-[current=page]:bg-primary aria-[current=page]:text-primary-foreground"
                                    href={usageHref(
                                        option,
                                        filterable ? filter : {},
                                    )}
                                >
                                    {windowLabels[option]}
                                </Link>
                            </li>
                        ))}
                    </ul>
                </nav>
            </header>

            <FilterBar filter={filter} supported={filterable} window={window} />

            <KpiStrip buckets={buckets} summary={summary} />

            <AttentionPanel summary={summary} />

            <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
                <ActivityPanel
                    buckets={buckets}
                    categories={active}
                    clipped={clipped}
                    earliestRecord={summary.earliest_record}
                    unit={unit}
                />
                <ChangesPanel
                    earliestRecord={summary.earliest_record}
                    filter={filter}
                    filterable={filterable}
                    series={summary.series}
                    window={window}
                />
            </div>

            <div className="grid gap-4 lg:grid-cols-2">
                <PeoplePanel {...panelProps} />
                <ViaPanel {...panelProps} />
                <EndpointsPanel anchors={anchors} {...panelProps} />
                <ToolsPanel {...panelProps} />
            </div>

            {idle.length > 0 && active.length > 0 ? (
                <p className="text-sm text-muted-foreground">
                    No requests{" "}
                    {isFiltered(filter) && filterable
                        ? "match the filter "
                        : ""}
                    from{" "}
                    {idle
                        .map((category) => categoryLabels[category])
                        .join(", ")}
                    .
                </p>
            ) : null}

            <div className="flex flex-col gap-2">
                <ExpectedPanel summary={summary} />
                <OlderErrorsPanel summary={summary} />
                <SessionsPanel summary={summary} />
            </div>

            <p className="max-w-3xl text-sm text-muted-foreground">
                Usernames are self-declared by callers. Web counts are requests
                that reached the MLWH server; lookups served from browser or
                Next.js caches are not counted.
            </p>
        </main>
    );
}
