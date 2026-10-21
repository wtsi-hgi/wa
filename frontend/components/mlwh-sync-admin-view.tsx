import { LocalTimestamp } from "@/components/local-timestamp";
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import type { MLWHSyncStatus } from "@/lib/contracts";
import { cn } from "@/lib/utils";

export type MLWHSyncAdminViewProps = {
    freshness: MLWHSyncStatus;
};

type SyncTable = MLWHSyncStatus["tables"][number];

type StatusStyle = {
    label: string;
    // rank orders tables worst first, matching the server's overall status.
    rank: number;
    badge: string;
    dot: string;
};

const statusStyles: Record<string, StatusStyle> = {
    failing: {
        label: "Failing",
        rank: 0,
        badge: "bg-destructive/10 text-destructive ring-destructive/40",
        dot: "bg-destructive",
    },
    never_synced: {
        label: "Never synced",
        rank: 1,
        badge: "bg-amber-500/10 text-amber-700 ring-amber-500/40 dark:text-amber-300",
        dot: "bg-amber-500",
    },
    stale: {
        label: "Stale",
        rank: 2,
        badge: "bg-amber-500/10 text-amber-700 ring-amber-500/40 dark:text-amber-300",
        dot: "bg-amber-500",
    },
    ok: {
        label: "OK",
        rank: 3,
        badge: "bg-muted/60 text-muted-foreground ring-border",
        dot: "bg-primary/60",
    },
};

// statusStyle falls back to a neutral style for a status this page does not
// know, so a newer server never breaks it.
function statusStyle(status: string): StatusStyle {
    return (
        statusStyles[status] ?? {
            label: status,
            rank: 3,
            badge: "bg-muted/60 text-muted-foreground ring-border",
            dot: "bg-muted-foreground",
        }
    );
}

function plural(count: number, unit: string): string {
    return `${count} ${unit}${count === 1 ? "" : "s"}`;
}

// formatStaleAfter renders the stale threshold in its largest whole unit.
function formatStaleAfter(seconds: number): string {
    if (seconds > 0 && seconds % 3600 === 0) {
        return plural(seconds / 3600, "hour");
    }

    if (seconds > 0 && seconds % 60 === 0) {
        return plural(seconds / 60, "minute");
    }

    return plural(seconds, "second");
}

function problemCounts(tables: SyncTable[]): string[] {
    const counts = [
        ["failing", "failing"],
        ["never_synced", "never synced"],
        ["stale", "stale"],
    ].map(([status, label]) => {
        const count = tables.filter((table) => table.status === status).length;

        return count > 0 ? `${count} ${label}` : "";
    });

    return counts.filter((count) => count !== "");
}

function OverallBanner({ freshness }: MLWHSyncAdminViewProps) {
    const staleAfter = formatStaleAfter(freshness.stale_after_seconds);

    if (freshness.status === "ok") {
        return (
            <section
                className="flex items-start gap-3 rounded-xl border border-border/70 bg-card p-4 text-sm text-card-foreground"
                role="status"
            >
                <span
                    aria-hidden="true"
                    className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary/60"
                />
                <div className="space-y-0.5">
                    <p className="font-medium">
                        All {freshness.tables.length} tables are in sync
                    </p>
                    <p className="text-muted-foreground">
                        Each synced successfully within the last {staleAfter}.
                    </p>
                </div>
            </section>
        );
    }

    const failing = freshness.status === "failing";

    return (
        <section
            className={cn(
                "relative overflow-hidden rounded-xl p-4 pl-5 text-sm ring-1 before:absolute before:inset-y-0 before:left-0 before:w-1.5",
                failing
                    ? "bg-destructive/10 ring-destructive/40 before:bg-destructive"
                    : "bg-amber-500/10 ring-amber-500/40 before:bg-amber-500",
            )}
            role="alert"
        >
            <p
                className={cn(
                    "text-base font-semibold",
                    failing
                        ? "text-destructive"
                        : "text-amber-800 dark:text-amber-200",
                )}
            >
                MLWH sync needs attention
            </p>
            <p className="mt-1 font-medium text-foreground">
                {problemCounts(freshness.tables).join(", ")} of{" "}
                {plural(freshness.tables.length, "table")}.
            </p>
            <p className="mt-1 text-muted-foreground">
                A table is stale after {staleAfter} without a successful sync.
                Queries over failing or stale tables may miss recent MLWH
                changes.
            </p>
        </section>
    );
}

function StatusBadge({ status }: { status: string }) {
    const style = statusStyle(status);

    return (
        <span
            className={cn(
                "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap ring-1 ring-inset",
                style.badge,
            )}
        >
            <span
                aria-hidden="true"
                className={cn("h-1.5 w-1.5 rounded-full", style.dot)}
            />
            {style.label}
        </span>
    );
}

// MobileLabel names a cell's column when rows stack below sm. Screen readers
// already announce the column header, so it is hidden from them.
function MobileLabel({ children }: { children: string }) {
    return (
        <span
            aria-hidden="true"
            className="mb-0.5 block text-xs font-medium tracking-wide text-muted-foreground uppercase sm:hidden"
        >
            {children}
        </span>
    );
}

function firstLine(text: string): string {
    return text.split("\n", 1)[0];
}

function LastError({ table }: { table: SyncTable }) {
    if (table.last_error === "") {
        return <span className="text-muted-foreground">-</span>;
    }

    return (
        <details className="group max-w-xl">
            <summary className="flex cursor-pointer list-none items-baseline gap-1.5 rounded-sm text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none [&::-webkit-details-marker]:hidden">
                <span
                    aria-hidden="true"
                    className="shrink-0 text-muted-foreground transition-transform group-open:rotate-90"
                >
                    ▸
                </span>
                <span className="min-w-0 break-words sm:truncate">
                    {firstLine(table.last_error)}
                </span>
            </summary>
            <div className="mt-2 space-y-1 border-l-2 border-border pl-3">
                <p className="text-xs text-muted-foreground">
                    Recorded{" "}
                    <LocalTimestamp
                        format="dateTime"
                        value={table.last_error_at}
                    />
                </p>
                <pre className="font-mono text-xs break-words whitespace-pre-wrap text-foreground">
                    {table.last_error}
                </pre>
            </div>
        </details>
    );
}

export function MLWHSyncAdminView({ freshness }: MLWHSyncAdminViewProps) {
    const tables = [...freshness.tables].sort(
        (left, right) =>
            statusStyle(left.status).rank - statusStyle(right.status).rank,
    );

    return (
        <main className="mx-auto flex min-h-screen w-full max-w-[84rem] flex-col gap-6 px-4 py-6 sm:px-8 lg:py-8">
            <header className="flex flex-col gap-1">
                <h1 className="text-3xl font-semibold tracking-tight">
                    MLWH sync
                </h1>
                <p className="text-sm text-muted-foreground">
                    Per-table outcome of the latest wa mlwh sync runs, read from
                    the MLWH server&apos;s /freshness.
                </p>
            </header>

            <OverallBanner freshness={freshness} />

            <div className="rounded-xl border border-border/70 bg-card">
                {/* Below sm each row stacks into a card with visible field
                    labels, so nothing scrolls sideways at phone width. The
                    explicit roles keep table semantics, which some browsers
                    drop from table elements no longer displayed as tables. */}
                <Table className="max-sm:block" role="table">
                    <TableHeader className="max-sm:sr-only" role="rowgroup">
                        <TableRow role="row">
                            <TableHead role="columnheader" scope="col">
                                Table
                            </TableHead>
                            <TableHead role="columnheader" scope="col">
                                Status
                            </TableHead>
                            <TableHead role="columnheader" scope="col">
                                Last success
                            </TableHead>
                            <TableHead role="columnheader" scope="col">
                                Last error
                            </TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody className="max-sm:block" role="rowgroup">
                        {tables.map((table) => (
                            <TableRow
                                className="max-sm:grid max-sm:grid-cols-[minmax(0,1fr)_auto] max-sm:items-center max-sm:gap-x-3 max-sm:gap-y-2 max-sm:px-4 max-sm:py-3"
                                key={table.table}
                                role="row"
                            >
                                <th
                                    className="px-4 py-3 text-left align-top font-mono text-xs font-medium whitespace-nowrap text-foreground max-sm:truncate max-sm:p-0"
                                    role="rowheader"
                                    scope="row"
                                >
                                    {table.table}
                                </th>
                                <TableCell
                                    className="align-top max-sm:p-0"
                                    role="cell"
                                >
                                    <StatusBadge status={table.status} />
                                </TableCell>
                                <TableCell
                                    className="align-top text-sm whitespace-nowrap max-sm:col-span-2 max-sm:p-0"
                                    role="cell"
                                >
                                    <MobileLabel>Last success</MobileLabel>
                                    {table.last_success_at === "" ? (
                                        <span className="text-muted-foreground">
                                            Never
                                        </span>
                                    ) : (
                                        <LocalTimestamp
                                            format="dateTime"
                                            value={table.last_success_at}
                                        />
                                    )}
                                </TableCell>
                                <TableCell
                                    className="align-top text-sm whitespace-normal max-sm:col-span-2 max-sm:p-0"
                                    role="cell"
                                >
                                    <MobileLabel>Last error</MobileLabel>
                                    {table.blocked_by.length > 0 ? (
                                        <p className="mb-1 text-xs font-medium text-destructive">
                                            Blocked by{" "}
                                            {table.blocked_by.join(", ")}
                                        </p>
                                    ) : null}
                                    <LastError table={table} />
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        </main>
    );
}
