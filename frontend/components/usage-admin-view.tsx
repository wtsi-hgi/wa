import Link from "next/link";
import {
    createElement,
    type ComponentPropsWithoutRef,
    type ReactNode,
} from "react";

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
    type UsageErrorBreakdown,
    type UsageSummary,
    type UsageWindow,
} from "@/lib/contracts";
import { usageErrorHelp, usageErrorKindHelp } from "@/lib/usage-error-help";
import { cn } from "@/lib/utils";

export type UsageAdminViewProps = {
    summary: UsageSummary;
    window: UsageWindow;
};

type CountMap = Record<string, number>;
type UsagePeriod = UsageSummary["series"][number];
type UsageErrorGroup = NonNullable<UsageSummary["errors"]>[number];
type UsageErrorExample = UsageErrorGroup["examples"][number];

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
// order, as "<label> <n>" joined by ", ". <n> is a plain integer: a thousands
// separator would be ambiguous with the ", " join.
function formatCategories(byCategory: CountMap): string {
    return usageCategorySchema.options
        .filter((category) => (byCategory[category] ?? 0) > 0)
        .map(
            (category) => `${categoryLabels[category]} ${byCategory[category]}`,
        )
        .join(", ");
}

// formatCounts lists every entry of counts in key order as "<key> <n>" joined
// by ", ", with <n> a plain integer like formatCategories.
function formatCounts(counts: CountMap): string {
    return Object.entries(counts)
        .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
        .map(([key, value]) => `${key} ${value}`)
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
    // wrapAnywhere marks a column of caller-supplied values (paths, user
    // agents, names) that may be hundreds of bytes without a space.
    wrapAnywhere?: boolean;
};

// UsageTable renders rows as a horizontally scrolling table, or "None." when
// there are no rows. The column at rowHeader (the first by default) is the
// row header. Text columns keep a minimum width and break only between words,
// so on narrow screens the table scrolls rather than splitting labels such as
// routes into tall rows. wrapAnywhere columns break anywhere instead:
// break-words does not lower a table cell's min-content width, so one long
// unbroken value would otherwise widen the table far past the screen.
function UsageTable<Row>({
    columns,
    rowHeader = 0,
    rowKey,
    rows,
}: {
    columns: Array<Column<Row>>;
    rowHeader?: number;
    // rowKey must be unique per row. Values the server turned into U+FFFD
    // can repeat, so keys for caller-supplied values include the index.
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
                                        className={cn(
                                            "min-w-40 px-4 py-2 text-left align-middle font-medium",
                                            column.wrapAnywhere
                                                ? "wrap-anywhere"
                                                : "break-words",
                                        )}
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
                                                : column.wrapAnywhere
                                                  ? "min-w-40 wrap-anywhere"
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
            className="flex h-10 w-full min-w-48 items-end justify-end gap-px border-b border-border"
            role="img"
        >
            {series.map((period, index) => {
                const count = row.counts[index];

                return (
                    <span
                        className="flex h-full max-w-6 min-w-px flex-1 items-end rounded-t-[4px] hover:bg-muted"
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

// errorEndpointLabel names an error group's endpoint as the Endpoints table
// does, or marks it unmatched.
function errorEndpointLabel(method: string, route: string): string {
    return route ? `${method} ${route}` : `${method} (no matching route)`;
}

// errorEndpointId is the element id of an endpoint's errors in the listing
// prefix names ("" when there is one listing). Distinct labels can share an
// id; a link then reaches the first, which is harmless.
function errorEndpointId(prefix: string, label: string): string {
    const slug = label.replace(/[^A-Za-z0-9]+/g, "-");

    return prefix ? `usage-errors-${prefix}-${slug}` : `usage-errors-${slug}`;
}

type ErrorEndpoint = {
    groups: UsageErrorGroup[];
    label: string;
    total: number;
    unmatched: boolean;
};

// errorEndpoints groups errors by endpoint, keeping the server's order within
// each, and orders endpoints by error count, matched routes before unmatched
// requests, then label.
function errorEndpoints(errors: UsageErrorGroup[]): ErrorEndpoint[] {
    const byLabel = new Map<string, ErrorEndpoint>();

    for (const group of errors) {
        const label = errorEndpointLabel(group.method, group.route);
        const endpoint = byLabel.get(label) ?? {
            groups: [],
            label,
            total: 0,
            unmatched: group.route === "",
        };

        endpoint.groups.push(group);
        endpoint.total += group.count;
        byLabel.set(label, endpoint);
    }

    return [...byLabel.values()].sort((a, b) => {
        if (a.total !== b.total) {
            return b.total - a.total;
        }

        if (a.unmatched !== b.unmatched) {
            return a.unmatched ? 1 : -1;
        }

        return a.label < b.label ? -1 : a.label > b.label ? 1 : 0;
    });
}

// errorHelpId is the element id of a code's entry in the "What to do" guide.
function errorHelpId(code: string | null): string {
    return `usage-error-help-${code === null ? "legacy" : code.replace(/[^A-Za-z0-9_]+/g, "-")}`;
}

// errorKindHelpId is the element id of a known kind's entry in the "What to
// do" guide.
function errorKindHelpId(kind: string): string {
    return `usage-error-kind-help-${kind}`;
}

type ErrorGuideEntry = { action: string; id: string; label: string };

// displayOrder returns errors in the order ErrorEndpointList shows them.
function displayOrder(errors: UsageErrorGroup[]): UsageErrorGroup[] {
    return errorEndpoints(errors).flatMap((endpoint) => endpoint.groups);
}

// errorGuideEntries returns one "What to do" entry per code in groups, in
// the order the codes first appear, then one per known kind (timeout,
// canceled) any group recorded.
function errorGuideEntries(groups: UsageErrorGroup[]): ErrorGuideEntry[] {
    const entries = new Map<string, ErrorGuideEntry>();
    const kinds = new Set<string>();

    for (const group of groups) {
        const id = errorHelpId(group.error_code);

        if (!entries.has(id)) {
            const help = usageErrorHelp(group.error_code);
            entries.set(id, { action: help.action, id, label: help.label });
        }

        for (const kind of Object.keys(group.error_kinds)) {
            kinds.add(kind);
        }
    }

    const kindEntries = Object.keys(usageErrorKindHelp)
        .filter((kind) => kinds.has(kind))
        .map((kind) => ({
            action: usageErrorKindHelp[kind],
            id: errorKindHelpId(kind),
            label: kind,
        }));

    return [...entries.values(), ...kindEntries];
}

function ErrorGuide({ entries }: { entries: ErrorGuideEntry[] }) {
    return (
        <div
            aria-labelledby="usage-error-guide-heading"
            className="rounded-xl border border-border/70 bg-muted/30 px-4 py-3"
            role="group"
        >
            <h3
                className="text-sm font-semibold"
                id="usage-error-guide-heading"
            >
                What to do
            </h3>
            <dl className="mt-2 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)]">
                {entries.map((entry) => (
                    <div
                        className="scroll-mt-6 sm:col-span-2 sm:grid sm:grid-cols-subgrid"
                        id={entry.id}
                        key={entry.id}
                    >
                        <dt className="font-mono text-xs leading-5 font-medium wrap-anywhere">
                            {entry.label}
                        </dt>
                        <dd className="max-w-3xl text-muted-foreground">
                            {entry.action}
                        </dd>
                    </div>
                ))}
            </dl>
        </div>
    );
}

function pluralise(n: number, noun: string): string {
    return `${formatCount(n)} ${noun}${n === 1 ? "" : "s"}`;
}

// exampleRequest is the example's path and query, or a note for rows stored
// before paths of matched routes were recorded.
function exampleRequest(example: UsageErrorExample): string {
    if (!example.path) {
        return "(not recorded)";
    }

    return example.query ? `${example.path}?${example.query}` : example.path;
}

// exampleCaller lists the example's category, MCP tool and agent, and user
// (or session when no user was declared), skipping empty values.
function exampleCaller(example: UsageErrorExample): string {
    return [
        categoryLabels[example.category],
        example.mcp_tool,
        example.mcp_agent,
        example.username || example.client_id,
    ]
        .filter(Boolean)
        .join(" · ");
}

function ErrorDetailLine({ label, value }: { label: string; value: string }) {
    if (!value) {
        return null;
    }

    return (
        <p className="text-sm wrap-anywhere">
            <span className="text-muted-foreground">{label}:</span> {value}
        </p>
    );
}

const helpLinkClassName =
    "text-muted-foreground underline decoration-border underline-offset-4 hover:decoration-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none";

// ErrorCausesLine lists kinds like formatCounts, linking each known kind to
// its entry in the "What to do" guide.
function ErrorCausesLine({ kinds }: { kinds: CountMap }) {
    const entries = Object.entries(kinds).sort(([a], [b]) =>
        a < b ? -1 : a > b ? 1 : 0,
    );

    if (entries.length === 0) {
        return null;
    }

    return (
        <p className="text-sm wrap-anywhere">
            <span className="text-muted-foreground">Causes:</span>{" "}
            {entries.map(([kind, count], index) => (
                <span key={kind}>
                    {index > 0 ? ", " : null}
                    {Object.hasOwn(usageErrorKindHelp, kind) ? (
                        <a
                            aria-label={`What to do about ${kind} (${count})`}
                            className={helpLinkClassName}
                            href={`#${errorKindHelpId(kind)}`}
                        >
                            {`${kind} ${count}`}
                        </a>
                    ) : (
                        `${kind} ${count}`
                    )}
                </span>
            ))}
        </p>
    );
}

// Heading renders an h<level> heading, at most h6.
function Heading({
    level,
    ...props
}: { level: number } & ComponentPropsWithoutRef<"h3">) {
    return createElement(`h${Math.min(level, 6)}`, props);
}

function ErrorGroupEntry({
    group,
    guided,
    headingLevel,
    id,
}: {
    group: UsageErrorGroup;
    // guided entries link to their code's "What to do" guide entry.
    guided: boolean;
    headingLevel: number;
    id: string;
}) {
    const help = usageErrorHelp(group.error_code);
    const serverError = group.status >= 500;
    const headingId = `${id}-heading`;

    return (
        <article
            aria-labelledby={headingId}
            className={cn(
                "relative flex flex-col gap-2 py-4 pr-4 pl-6 before:absolute before:inset-y-4 before:left-0 before:w-1 before:rounded-r-full",
                serverError ? "before:bg-destructive" : "before:bg-accent",
            )}
        >
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <Heading
                    className="flex min-w-0 items-baseline gap-2 font-medium"
                    level={headingLevel}
                    id={headingId}
                >
                    <span
                        className={cn(
                            "rounded-md px-1.5 py-0.5 text-xs font-semibold tabular-nums ring-1",
                            serverError
                                ? "bg-destructive/10 ring-destructive/40"
                                : "bg-accent/30 ring-accent",
                        )}
                    >
                        {group.status}
                    </span>{" "}
                    <span
                        className={cn(
                            "wrap-anywhere",
                            group.error_code !== null && "font-mono text-sm",
                        )}
                    >
                        {help.label}
                    </span>
                </Heading>
                <span className="text-sm font-semibold tabular-nums">
                    {pluralise(group.count, "error")}
                </span>
                {group.probe ? (
                    <span className="text-xs text-muted-foreground">
                        from lookup probes
                    </span>
                ) : null}
                <span className="text-xs text-muted-foreground">
                    first{" "}
                    <LocalTimestamp
                        format="dateTime"
                        value={group.first_seen}
                    />
                    , latest{" "}
                    <LocalTimestamp format="dateTime" value={group.last_seen} />
                </span>
            </div>
            <p className="max-w-3xl text-sm">
                {help.meaning}
                {guided ? (
                    <>
                        {" "}
                        <a
                            aria-label={`What to do about ${help.label}`}
                            className={helpLinkClassName}
                            href={`#${errorHelpId(group.error_code)}`}
                        >
                            What to do
                        </a>
                    </>
                ) : null}
            </p>
            <ErrorCausesLine kinds={group.error_kinds} />
            <details className="mt-1 rounded-lg border border-border/70 bg-muted/30 open:bg-transparent">
                <summary className="cursor-pointer rounded-lg px-3 py-2 text-sm font-medium hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                    Messages and examples (
                    {pluralise(group.examples.length, "example")})
                </summary>
                <div className="flex flex-col gap-3 px-3 pt-1 pb-3">
                    <div className="flex flex-col gap-1">
                        <ErrorDetailLine
                            label="Callers"
                            value={formatCategories(group.by_category)}
                        />
                        <ErrorDetailLine
                            label="MCP tools"
                            value={formatCounts(group.by_tool)}
                        />
                        <ErrorDetailLine
                            label="MCP agents"
                            value={formatCounts(group.by_agent)}
                        />
                    </div>
                    {group.messages.length > 0 ? (
                        <div className="flex flex-col gap-1">
                            <Heading
                                level={headingLevel + 1}
                                className="text-sm font-medium"
                                id={`${id}-messages`}
                            >
                                Top messages
                            </Heading>
                            <ol
                                aria-labelledby={`${id}-messages`}
                                className="flex flex-col divide-y divide-border/60 rounded-lg border border-border/70 bg-card"
                            >
                                {group.messages.map((message, index) => (
                                    <li
                                        className="flex items-baseline justify-between gap-4 px-3 py-1.5 text-sm"
                                        key={`${index} ${message.message}`}
                                    >
                                        <span className="font-mono text-xs wrap-anywhere">
                                            {message.message}
                                        </span>
                                        <span className="tabular-nums">
                                            {formatCount(message.count)}
                                        </span>
                                    </li>
                                ))}
                            </ol>
                        </div>
                    ) : null}
                    <div className="flex flex-col gap-1">
                        <Heading
                            level={headingLevel + 1}
                            className="text-sm font-medium"
                        >
                            Latest examples
                        </Heading>
                        <UsageTable<UsageErrorExample>
                            columns={[
                                {
                                    header: "Time",
                                    render: (row) => (
                                        <LocalTimestamp
                                            className="whitespace-nowrap tabular-nums"
                                            format="dateTime"
                                            value={row.ts}
                                        />
                                    ),
                                },
                                {
                                    header: "Request",
                                    render: (row) => (
                                        <span className="font-mono text-xs">
                                            {exampleRequest(row)}
                                        </span>
                                    ),
                                    wrapAnywhere: true,
                                },
                                {
                                    header: "Caller",
                                    render: exampleCaller,
                                    wrapAnywhere: true,
                                },
                                {
                                    header: "Message",
                                    render: (row) => row.message,
                                    wrapAnywhere: true,
                                },
                            ]}
                            rowKey={(row, index) => `${index} ${row.ts}`}
                            rows={group.examples}
                        />
                    </div>
                </div>
            </details>
        </article>
    );
}

// ErrorEndpointList renders errors grouped by endpoint, each endpoint headed
// at headingLevel and its groups one level below.
function ErrorEndpointList({
    errors,
    guided,
    headingLevel,
    idPrefix,
}: {
    errors: UsageErrorGroup[];
    guided: boolean;
    headingLevel: number;
    idPrefix: string;
}) {
    return (
        <div className="flex flex-col gap-4">
            {errorEndpoints(errors).map((endpoint) => {
                const id = errorEndpointId(idPrefix, endpoint.label);

                return (
                    <div
                        aria-labelledby={`${id}-heading`}
                        className="scroll-mt-6 overflow-hidden rounded-xl border border-border/70 bg-card text-card-foreground shadow-sm"
                        id={id}
                        key={endpoint.label}
                        role="group"
                    >
                        <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-border/70 bg-muted/40 px-4 py-3">
                            <Heading
                                className="font-mono text-sm font-semibold wrap-anywhere"
                                level={headingLevel}
                                id={`${id}-heading`}
                            >
                                {endpoint.label}
                            </Heading>
                            <p className="text-sm text-muted-foreground tabular-nums">
                                {pluralise(endpoint.total, "error")}
                            </p>
                        </div>
                        <div className="divide-y divide-border/70">
                            {endpoint.groups.map((group, index) => (
                                <ErrorGroupEntry
                                    group={group}
                                    guided={guided}
                                    headingLevel={headingLevel + 1}
                                    id={`${id}-${index}`}
                                    key={`${group.status} ${group.error_code} ${group.severity} ${group.probe}`}
                                />
                            ))}
                        </div>
                    </div>
                );
            })}
        </div>
    );
}

type ErrorBucket = {
    // guided buckets are the actionable ones the "What to do" guide covers.
    guided: boolean;
    id: string;
    intro: string;
    open: boolean;
    severity: string;
    title: string;
};

// errorBuckets are the Errors subsections of a server that reports
// severities, in display order.
const errorBuckets: ErrorBucket[] = [
    {
        severity: "fault",
        id: "faults",
        title: "Faults",
        intro: "Server failures, and requests wa's own CLI, web pages, or servers got wrong: wa needs fixing.",
        guided: true,
        open: true,
    },
    {
        severity: "caller",
        id: "caller",
        title: "Caller errors",
        intro: "Requests an MCP agent or another client got wrong. Repeats may mean a tool description or a client needs work.",
        guided: true,
        open: true,
    },
    {
        severity: "expected",
        id: "expected",
        title: "Expected outcomes",
        intro: "Lookups whose answer was that the identifier is unknown or names several records. Nothing to fix; lookup probes are the CLI trying each kind of identifier in turn.",
        guided: false,
        open: false,
    },
];

const legacyBucketTitle = "Recorded before error details";

// isLegacy reports whether group was recorded before error details.
function isLegacy(group: UsageErrorGroup): boolean {
    return group.error_code === null;
}

// bucketGroups returns bucket's groups: those with error details, then those
// recorded before them.
function bucketGroups(
    errors: UsageErrorGroup[],
    bucket: ErrorBucket,
): { detailed: UsageErrorGroup[]; legacy: UsageErrorGroup[] } {
    const groups = errors.filter((group) => group.severity === bucket.severity);

    return {
        detailed: groups.filter((group) => !isLegacy(group)),
        legacy: groups.filter(isLegacy),
    };
}

// hasSeverities reports whether the server classified every error group.
function hasSeverities(errors: UsageErrorGroup[]): boolean {
    return errors.every((group) =>
        (errorBuckets.map((bucket) => bucket.severity) as string[]).includes(
            group.severity ?? "",
        ),
    );
}

// errorAnchors maps each endpoint label with errors to the element id of its
// first listing: by bucket, detailed before legacy.
function errorAnchors(errors: UsageErrorGroup[]): Map<string, string> {
    const anchors = new Map<string, string>();
    const add = (prefix: string, groups: UsageErrorGroup[]) => {
        for (const group of groups) {
            const label = errorEndpointLabel(group.method, group.route);

            if (!anchors.has(label)) {
                anchors.set(label, errorEndpointId(prefix, label));
            }
        }
    };

    if (!hasSeverities(errors)) {
        add("", errors);

        return anchors;
    }

    for (const bucket of errorBuckets) {
        const { detailed, legacy } = bucketGroups(errors, bucket);
        add(bucket.id, detailed);
        add(`${bucket.id}-legacy`, legacy);
    }

    return anchors;
}

function sumCounts(groups: UsageErrorGroup[]): number {
    return groups.reduce((sum, group) => sum + group.count, 0);
}

function ErrorBucketSection({
    bucket,
    errors,
}: {
    bucket: ErrorBucket;
    errors: UsageErrorGroup[];
}) {
    const { detailed, legacy } = bucketGroups(errors, bucket);
    const total = sumCounts(detailed) + sumCounts(legacy);
    const headingId = `usage-errors-${bucket.id}-heading`;
    const heading = (
        <h3 className="inline text-base font-semibold" id={headingId}>
            {`${bucket.title} (${formatCount(total)})`}
        </h3>
    );
    const body =
        total === 0 ? (
            <EmptyNote>None.</EmptyNote>
        ) : (
            <>
                <ErrorEndpointList
                    errors={detailed}
                    guided={bucket.guided}
                    headingLevel={4}
                    idPrefix={bucket.id}
                />
                {legacy.length > 0 ? (
                    <details className="rounded-xl border border-border/70 bg-muted/30">
                        <summary className="cursor-pointer rounded-xl px-4 py-2 text-sm font-medium hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                            {`${legacyBucketTitle} (${pluralise(sumCounts(legacy), "error")})`}
                        </summary>
                        <div className="flex flex-col gap-3 px-3 pt-1 pb-3">
                            <p className="max-w-3xl text-sm text-muted-foreground">
                                These have no error code or message; their
                                severity is judged from the status and caller.
                            </p>
                            <ErrorEndpointList
                                errors={legacy}
                                guided={false}
                                headingLevel={4}
                                idPrefix={`${bucket.id}-legacy`}
                            />
                        </div>
                    </details>
                ) : null}
            </>
        );

    return (
        <section aria-labelledby={headingId} className="flex flex-col gap-2">
            {bucket.open ? (
                <>
                    {heading}
                    <p className="max-w-3xl text-sm text-muted-foreground">
                        {bucket.intro}
                    </p>
                    {body}
                </>
            ) : (
                <details className="rounded-xl border border-border/70">
                    <summary className="cursor-pointer rounded-xl px-4 py-2 hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                        {heading}
                    </summary>
                    <div className="flex flex-col gap-2 px-3 pb-3">
                        <p className="max-w-3xl text-sm text-muted-foreground">
                            {bucket.intro}
                        </p>
                        {body}
                    </div>
                </details>
            )}
        </section>
    );
}

function ErrorsSection({
    errors,
    totalErrors,
}: {
    errors: UsageErrorGroup[] | undefined;
    totalErrors: number;
}) {
    if (errors === undefined) {
        return (
            <UsageSection id="errors" title="Errors">
                <EmptyNote>
                    This MLWH server does not report error details. Upgrade wa
                    mlwh serve to see them.
                </EmptyNote>
            </UsageSection>
        );
    }

    if (errors.length === 0) {
        return (
            <UsageSection id="errors" title="Errors">
                <EmptyNote>No errors in this window.</EmptyNote>
            </UsageSection>
        );
    }

    const shownErrors = sumCounts(errors);
    const capNote =
        shownErrors < totalErrors ? (
            <p className="max-w-3xl text-sm text-muted-foreground">
                Showing the {formatCount(errors.length)} largest groups (
                {formatCount(shownErrors)} of {formatCount(totalErrors)}{" "}
                errors).
            </p>
        ) : null;

    if (!hasSeverities(errors)) {
        return (
            <UsageSection id="errors" title="Errors">
                <p className="max-w-3xl text-sm text-muted-foreground">
                    Grouped by endpoint, then by status and error code. Expand
                    an entry for its most common messages and latest examples.
                </p>
                {capNote}
                <ErrorGuide entries={errorGuideEntries(displayOrder(errors))} />
                <ErrorEndpointList
                    errors={errors}
                    guided
                    headingLevel={3}
                    idPrefix=""
                />
            </UsageSection>
        );
    }

    const guided = errorBuckets
        .filter((bucket) => bucket.guided)
        .flatMap((bucket) =>
            displayOrder(bucketGroups(errors, bucket).detailed),
        );

    return (
        <UsageSection id="errors" title="Errors">
            <p className="max-w-3xl text-sm text-muted-foreground">
                By severity, then endpoint, then status and error code. Expand
                an entry for its most common messages and latest examples.
            </p>
            {capNote}
            {guided.length > 0 ? (
                <ErrorGuide entries={errorGuideEntries(guided)} />
            ) : null}
            {errorBuckets.map((bucket) => (
                <ErrorBucketSection
                    bucket={bucket}
                    errors={errors}
                    key={bucket.id}
                />
            ))}
        </UsageSection>
    );
}

// errorBreakdownText summarises how the window's errors split by severity.
function errorBreakdownText(
    errors: number,
    severity: UsageErrorBreakdown,
): string {
    const legacy =
        severity.legacy.fault +
        severity.legacy.caller +
        severity.legacy.expected;
    const parts = [
        pluralise(severity.fault, "fault"),
        pluralise(severity.caller, "caller error"),
        pluralise(severity.expected, "expected outcome"),
        `${formatCount(severity.probe)} lookup probe ${severity.probe === 1 ? "miss" : "misses"}`,
    ];
    const legacyNote =
        legacy > 0
            ? ` ${formatCount(legacy)} (${formatCount(severity.legacy.fault)} of the faults) were recorded before error details and are judged by status.`
            : "";

    return `Of ${pluralise(errors, "error")}: ${parts.join(", ")}.${legacyNote}`;
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
                        wrapAnywhere: true,
                    },
                    {
                        header: "Requests",
                        numeric: true,
                        render: (row) => formatCount(row.requests),
                    },
                    {
                        header: "Errors",
                        numeric: true,
                        // Servers that predate error details send none.
                        render: (row) =>
                            row.errors === undefined
                                ? "-"
                                : formatCount(row.errors),
                    },
                ]}
                rowKey={(row, index) => `${index} ${row.value}`}
                rows={rows}
            />
        </UsageSection>
    );
}

export function UsageAdminView({ summary, window }: UsageAdminViewProps) {
    const { totals } = summary;
    const errorAnchorIds = errorAnchors(summary.errors ?? []);
    const severity = totals.error_severity;

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
                {severity ? (
                    <p data-testid="usage-error-breakdown">
                        {errorBreakdownText(totals.errors, severity)}
                    </p>
                ) : null}
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
                {severity ? (
                    <StatTile label="Faults">
                        {formatCount(severity.fault)}
                        <span className="text-base font-normal text-muted-foreground">
                            {` (${formatRate(totals.requests > 0 ? severity.fault / totals.requests : 0)})`}
                        </span>
                    </StatTile>
                ) : (
                    <StatTile label="Errors">
                        {formatCount(totals.errors)}
                        <span className="text-base font-normal text-muted-foreground">
                            {` (${formatRate(totals.error_rate)})`}
                        </span>
                    </StatTile>
                )}
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
                            render: (row) => {
                                const label = errorEndpointLabel(
                                    row.method,
                                    row.route,
                                );

                                const anchor = errorAnchorIds.get(label);

                                return anchor ? (
                                    <a
                                        aria-label={`${pluralise(row.errors, "error")} on ${label}, see details`}
                                        className="underline decoration-border underline-offset-4 hover:decoration-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                                        href={`#${anchor}`}
                                    >
                                        {formatCount(row.errors)}
                                    </a>
                                ) : (
                                    formatCount(row.errors)
                                );
                            },
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

            <ErrorsSection
                errors={summary.errors}
                totalErrors={totals.errors}
            />

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
                                <span className="font-mono">{row.path}</span>
                            ),
                            wrapAnywhere: true,
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
                            wrapAnywhere: true,
                        },
                        {
                            header: "MCP agents",
                            render: (row) => formatCounts(row.by_agent),
                            wrapAnywhere: true,
                        },
                    ]}
                    rowHeader={1}
                    rowKey={(row, index) =>
                        `${index} ${row.method} ${row.path}`
                    }
                    rows={summary.unmatched}
                />
            </UsageSection>

            <UsageSection id="users" title="Top users (self-declared)">
                <UsageTable<UsageSummary["users"][number]>
                    columns={[
                        {
                            header: "User",
                            render: (row) => row.name,
                            wrapAnywhere: true,
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
                    rowKey={(row, index) => `${index} ${row.name}`}
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
                            wrapAnywhere: true,
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
                    rowKey={(row, index) => `${index} ${row.name}`}
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
