import Link from "next/link";
import type { ReactNode } from "react";

import { LocalTimestamp } from "@/components/local-timestamp";
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import {
    usageCategorySchema,
    type UsageCategory,
    type UsageSummary,
    type UsageWindow,
} from "@/lib/contracts";
import { cn } from "@/lib/utils";

export type UsageAdminViewProps = {
    summary: UsageSummary;
    window: UsageWindow;
};

type CountMap = Record<string, number>;
type UsagePeriod = UsageSummary["series"][number];

const categoryLabels: Record<UsageCategory, string> = {
    cli: "CLI",
    mcp: "MCP",
    web: "Web",
    results_server: "Results server",
    mlwhdiff_server: "mlwhdiff server",
    other: "Other API",
};

const windowLinks: Array<{ href: string; label: string; window: UsageWindow }> =
    [
        { window: "24h", label: "Last 24 hours", href: "/usage?window=24h" },
        { window: "7d", label: "Last 7 days", href: "/usage" },
        { window: "30d", label: "Last 30 days", href: "/usage?window=30d" },
        { window: "all", label: "All time", href: "/usage?window=all" },
    ];

const trendPeriodNames: Record<Exclude<UsageWindow, "all">, string> = {
    "24h": "24-hour",
    "7d": "7-day",
    "30d": "30-day",
};

const countFormat = new Intl.NumberFormat("en-GB");

function formatCount(value: number): string {
    return countFormat.format(value);
}

function formatRate(rate: number): string {
    return `${(rate * 100).toFixed(1)}%`;
}

// formatPeriods returns "1 period" or "<n> periods".
function formatPeriods(n: number): string {
    return n === 1 ? "1 period" : `${n} periods`;
}

function formatMs(ms: number): string {
    return ms.toFixed(1);
}

// formatCategories lists the non-zero categories of byCategory in category
// order, as "<label> <n>" joined by ", ".
function formatCategories(byCategory: CountMap): string {
    return usageCategorySchema.options
        .filter((category) => (byCategory[category] ?? 0) > 0)
        .map(
            (category) =>
                `${categoryLabels[category]} ${formatCount(byCategory[category])}`,
        )
        .join(", ");
}

// formatCounts lists every entry of counts in key order as "<key> <n>".
function formatCounts(counts: CountMap): string {
    return Object.entries(counts)
        .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
        .map(([key, value]) => `${key} ${formatCount(value)}`)
        .join(", ");
}

type TrendRow = {
    counts: number[];
    label: string;
};

// trendRows builds the Trend rows: all requests, each category, then every
// endpoint seen in any period, by series total descending then key.
function trendRows(series: UsagePeriod[]): TrendRow[] {
    const endpointTotals = new Map<string, number>();

    for (const period of series) {
        for (const [key, value] of Object.entries(period.by_endpoint)) {
            endpointTotals.set(key, (endpointTotals.get(key) ?? 0) + value);
        }
    }

    const endpointKeys = [...endpointTotals.entries()]
        .sort(([keyA, totalA], [keyB, totalB]) =>
            totalA !== totalB ? totalB - totalA : keyA < keyB ? -1 : 1,
        )
        .map(([key]) => key);

    return [
        {
            label: "All requests",
            counts: series.map((period) => period.requests),
        },
        ...usageCategorySchema.options.map((category) => ({
            label: categoryLabels[category],
            counts: series.map((period) => period.by_category[category] ?? 0),
        })),
        ...endpointKeys.map((key) => ({
            label: key,
            counts: series.map((period) => period.by_endpoint[key] ?? 0),
        })),
    ];
}

function UsageSection({
    children,
    id,
    title,
}: {
    children: ReactNode;
    id: string;
    title: string;
}) {
    const headingId = `usage-${id}`;

    return (
        <section aria-labelledby={headingId} className="flex flex-col gap-2">
            <h2 className="text-lg font-semibold tracking-tight" id={headingId}>
                {title}
            </h2>
            {children}
        </section>
    );
}

function EmptyNote({ children }: { children: ReactNode }) {
    return (
        <p className="rounded-xl border border-dashed border-border/70 p-4 text-sm text-muted-foreground">
            {children}
        </p>
    );
}

type Column<Row> = {
    header: string;
    numeric?: boolean;
    render: (row: Row) => ReactNode;
};

// UsageTable renders rows as a horizontally scrolling table, or "None." when
// there are no rows. The column at rowHeader (the first by default) is the
// row header. Text columns keep a minimum width and break only between words,
// so on narrow screens the table scrolls rather than splitting labels such as
// routes into tall rows.
function UsageTable<Row>({
    columns,
    rowHeader = 0,
    rowKey,
    rows,
}: {
    columns: Array<Column<Row>>;
    rowHeader?: number;
    rowKey: (row: Row, index: number) => string;
    rows: Row[];
}) {
    if (rows.length === 0) {
        return <EmptyNote>None.</EmptyNote>;
    }

    return (
        <div className="rounded-xl border border-border/70 bg-card text-card-foreground shadow-sm">
            <Table>
                <TableHeader>
                    <TableRow className="hover:bg-transparent">
                        {columns.map((column) => (
                            <TableHead
                                className={cn(
                                    "h-10 whitespace-nowrap",
                                    column.numeric && "text-right",
                                )}
                                key={column.header}
                                scope="col"
                            >
                                {column.header}
                            </TableHead>
                        ))}
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {rows.map((row, index) => (
                        <TableRow key={rowKey(row, index)}>
                            {columns.map((column, columnIndex) =>
                                columnIndex === rowHeader ? (
                                    <th
                                        className="min-w-40 px-4 py-2 text-left align-middle font-medium break-words"
                                        key={column.header}
                                        scope="row"
                                    >
                                        {column.render(row)}
                                    </th>
                                ) : (
                                    <TableCell
                                        className={cn(
                                            "py-2",
                                            column.numeric
                                                ? "text-right whitespace-nowrap tabular-nums"
                                                : "min-w-40 break-words",
                                        )}
                                        key={column.header}
                                    >
                                        {column.render(row)}
                                    </TableCell>
                                ),
                            )}
                        </TableRow>
                    ))}
                </TableBody>
            </Table>
        </div>
    );
}

function TrendStrip({
    label,
    latest,
    previous,
    row,
    series,
}: {
    label: string;
    latest: string;
    previous: string;
    row: TrendRow;
    series: UsagePeriod[];
}) {
    const max = Math.max(...row.counts);

    return (
        <div
            aria-label={`${label}: latest ${latest}, previous ${previous}, ${formatPeriods(series.length)}`}
            className="flex h-10 w-full min-w-48 items-end justify-end gap-0.5 border-b border-border"
            role="img"
        >
            {series.map((period, index) => {
                const count = row.counts[index];

                return (
                    <span
                        className="flex h-full max-w-6 min-w-1 flex-1 items-end rounded-t-[4px] hover:bg-muted"
                        key={period.end}
                        title={`${period.start} to ${period.end}: ${formatCount(count)}`}
                    >
                        <span
                            className="w-full rounded-t-[4px] bg-primary"
                            style={{
                                height: `${max > 0 ? (count / max) * 100 : 0}%`,
                            }}
                        />
                    </span>
                );
            })}
        </div>
    );
}

function TrendSection({
    series,
    window,
}: {
    series: UsagePeriod[];
    window: Exclude<UsageWindow, "all">;
}) {
    const title = `Trend (${trendPeriodNames[window]} periods)`;

    if (series.length === 0) {
        return (
            <UsageSection id="trend" title={title}>
                <EmptyNote>No usage recorded yet.</EmptyNote>
            </UsageSection>
        );
    }

    const latestIndex = series.length - 1;
    const display = (row: TrendRow, index: number) =>
        index < 0 ? "-" : formatCount(row.counts[index]);

    return (
        <UsageSection id="trend" title={title}>
            <UsageTable<TrendRow>
                columns={[
                    { header: "Series", render: (row) => row.label },
                    {
                        header: "Latest",
                        numeric: true,
                        render: (row) => display(row, latestIndex),
                    },
                    {
                        header: "Previous",
                        numeric: true,
                        render: (row) => display(row, latestIndex - 1),
                    },
                    {
                        header: `${formatPeriods(series.length)}, oldest first`,
                        render: (row) => (
                            <TrendStrip
                                label={row.label}
                                latest={display(row, latestIndex)}
                                previous={display(row, latestIndex - 1)}
                                row={row}
                                series={series}
                            />
                        ),
                    },
                ]}
                rowKey={(row) => row.label}
                rows={trendRows(series)}
            />
        </UsageSection>
    );
}

function StatTile({ label, children }: { children: ReactNode; label: string }) {
    return (
        <div className="flex flex-col gap-1 rounded-xl border border-border/70 bg-card p-4 text-card-foreground shadow-sm">
            <dt className="text-sm text-muted-foreground">{label}</dt>
            <dd className="text-3xl font-semibold tracking-tight tabular-nums">
                {children}
            </dd>
        </div>
    );
}

type ValueCount = UsageSummary["mcp_tools"][number];

function valueCountSection(id: string, title: string, rows: ValueCount[]) {
    return (
        <UsageSection id={id} title={title}>
            <UsageTable<ValueCount>
                columns={[
                    {
                        header: "Value",
                        render: (row) => row.value || "(none)",
                    },
                    {
                        header: "Requests",
                        numeric: true,
                        render: (row) => formatCount(row.requests),
                    },
                ]}
                rowKey={(row) => row.value}
                rows={rows}
            />
        </UsageSection>
    );
}

export function UsageAdminView({ summary, window }: UsageAdminViewProps) {
    const { totals } = summary;

    return (
        <main className="mx-auto flex min-h-screen w-full max-w-[84rem] flex-col gap-6 px-4 py-6 sm:px-8 lg:py-8">
            <header className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
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
                    </p>
                </div>
                <nav aria-label="Window">
                    <ul className="flex flex-wrap gap-1 rounded-lg border border-border/70 bg-muted/40 p-1 text-sm">
                        {windowLinks.map((link) => (
                            <li key={link.window}>
                                <Link
                                    aria-current={
                                        link.window === window
                                            ? "page"
                                            : undefined
                                    }
                                    className="block rounded-md px-3 py-1.5 font-medium text-muted-foreground transition-colors hover:bg-background hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none aria-[current=page]:bg-primary aria-[current=page]:text-primary-foreground"
                                    href={link.href}
                                >
                                    {link.label}
                                </Link>
                            </li>
                        ))}
                    </ul>
                </nav>
            </header>

            <div className="max-w-3xl space-y-1 text-sm text-muted-foreground">
                <p>
                    Usernames are self-declared by callers. Web counts are
                    requests that reached the MLWH server; lookups served from
                    browser or Next.js caches are not counted.
                </p>
                {summary.dropped_events > 0 ? (
                    <p className="font-medium text-foreground">
                        {formatCount(summary.dropped_events)} requests were not
                        recorded because the usage queue was full.
                    </p>
                ) : null}
            </div>

            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
                <StatTile label="Requests">
                    {formatCount(totals.requests)}
                </StatTile>
                <StatTile label="Errors">
                    {formatCount(totals.errors)}
                    <span className="text-base font-normal text-muted-foreground">
                        {` (${formatRate(totals.error_rate)})`}
                    </span>
                </StatTile>
                <StatTile label="Sessions">
                    {formatCount(totals.sessions)}
                </StatTile>
                <StatTile label="Clients by IP">
                    {formatCount(totals.ip_clients)}
                </StatTile>
                <StatTile label="Users (self-declared)">
                    {formatCount(totals.users)}
                </StatTile>
            </dl>

            <UsageSection id="categories" title="By category">
                <UsageTable<UsageSummary["categories"][number]>
                    columns={[
                        {
                            header: "Category",
                            render: (row) => categoryLabels[row.category],
                        },
                        {
                            header: "Requests",
                            numeric: true,
                            render: (row) => formatCount(row.requests),
                        },
                        {
                            header: "Errors",
                            numeric: true,
                            render: (row) => formatCount(row.errors),
                        },
                        {
                            header: "Error rate",
                            numeric: true,
                            render: (row) => formatRate(row.error_rate),
                        },
                        {
                            header: "Sessions",
                            numeric: true,
                            render: (row) => formatCount(row.sessions),
                        },
                        {
                            header: "Clients by IP",
                            numeric: true,
                            render: (row) => formatCount(row.ip_clients),
                        },
                        {
                            header: "Users",
                            numeric: true,
                            render: (row) => formatCount(row.users),
                        },
                        {
                            header: "Requests/session (median)",
                            numeric: true,
                            render: (row) =>
                                formatCount(row.requests_per_session_median),
                        },
                        {
                            header: "Requests/session (max)",
                            numeric: true,
                            render: (row) =>
                                formatCount(row.requests_per_session_max),
                        },
                    ]}
                    rowKey={(row) => row.category}
                    rows={summary.categories}
                />
            </UsageSection>

            {window === "all" ? null : (
                <TrendSection series={summary.series} window={window} />
            )}

            <UsageSection id="endpoints" title="Endpoints">
                <UsageTable<UsageSummary["endpoints"][number]>
                    columns={[
                        {
                            header: "Endpoint",
                            render: (row) => `${row.method} ${row.route}`,
                        },
                        {
                            header: "Requests",
                            numeric: true,
                            render: (row) => formatCount(row.requests),
                        },
                        {
                            header: "Errors",
                            numeric: true,
                            render: (row) => formatCount(row.errors),
                        },
                        {
                            header: "Error rate",
                            numeric: true,
                            render: (row) => formatRate(row.error_rate),
                        },
                        {
                            header: "Median latency (ms)",
                            numeric: true,
                            render: (row) => formatMs(row.p50_ms),
                        },
                        {
                            header: "p95 latency (ms)",
                            numeric: true,
                            render: (row) => formatMs(row.p95_ms),
                        },
                        {
                            header: "Categories",
                            render: (row) => formatCategories(row.by_category),
                        },
                    ]}
                    rowKey={(row) => `${row.method} ${row.route}`}
                    rows={summary.endpoints}
                />
            </UsageSection>

            {valueCountSection("mcp-tools", "MCP tools", summary.mcp_tools)}
            {valueCountSection("mcp-agents", "MCP agents", summary.mcp_agents)}
            {valueCountSection(
                "other-user-agents",
                "Other API user agents",
                summary.other_user_agents,
            )}

            <UsageSection id="unmatched" title="Unmatched paths">
                <UsageTable<UsageSummary["unmatched"][number]>
                    columns={[
                        { header: "Method", render: (row) => row.method },
                        {
                            header: "Path",
                            render: (row) => (
                                <span className="font-mono break-words">
                                    {row.path}
                                </span>
                            ),
                        },
                        {
                            header: "Requests",
                            numeric: true,
                            render: (row) => formatCount(row.requests),
                        },
                        {
                            header: "Categories",
                            render: (row) => formatCategories(row.by_category),
                        },
                        {
                            header: "MCP tools",
                            render: (row) => formatCounts(row.by_tool),
                        },
                        {
                            header: "MCP agents",
                            render: (row) => formatCounts(row.by_agent),
                        },
                    ]}
                    rowHeader={1}
                    rowKey={(row) => `${row.method} ${row.path}`}
                    rows={summary.unmatched}
                />
            </UsageSection>

            <UsageSection id="users" title="Top users (self-declared)">
                <UsageTable<UsageSummary["users"][number]>
                    columns={[
                        { header: "User", render: (row) => row.name },
                        {
                            header: "Requests",
                            numeric: true,
                            render: (row) => formatCount(row.requests),
                        },
                        {
                            header: "Categories",
                            render: (row) => formatCategories(row.by_category),
                        },
                    ]}
                    rowKey={(row) => row.name}
                    rows={summary.users}
                />
            </UsageSection>

            <UsageSection id="clients" title="Top clients">
                <UsageTable<UsageSummary["clients"][number]>
                    columns={[
                        {
                            header: "Client",
                            render: (row) =>
                                row.by_ip ? `${row.name} (by IP)` : row.name,
                        },
                        {
                            header: "Requests",
                            numeric: true,
                            render: (row) => formatCount(row.requests),
                        },
                        {
                            header: "Categories",
                            render: (row) => formatCategories(row.by_category),
                        },
                    ]}
                    rowKey={(row) => `${row.by_ip ? "ip" : "id"}:${row.name}`}
                    rows={summary.clients}
                />
            </UsageSection>

            <UsageSection id="days" title="Requests per day (UTC)">
                <UsageTable<UsageSummary["days"][number]>
                    columns={[
                        {
                            header: "Date",
                            render: (row) => (
                                <span className="tabular-nums">{row.date}</span>
                            ),
                        },
                        {
                            header: "Requests",
                            numeric: true,
                            render: (row) => formatCount(row.requests),
                        },
                        {
                            header: "Categories",
                            render: (row) => formatCategories(row.by_category),
                        },
                    ]}
                    rowKey={(row) => row.date}
                    rows={summary.days}
                />
            </UsageSection>
        </main>
    );
}
