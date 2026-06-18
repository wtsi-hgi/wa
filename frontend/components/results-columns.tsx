import { ArrowUpDown, LockKeyhole } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";

import type { ResultSet, SearchResult } from "@/lib/contracts";
import { formatRegistrationUnique } from "@/lib/result-identity";
import { cn, formatUtcDate } from "@/lib/utils";

export type ResultsTableRow = {
    id: string;
    result: ResultSet;
    matchedSamples: string[];
    searchResult: boolean;
};

type SortableHeaderProps = {
    columnId: string;
    label: string;
    onSort: () => void;
};

function SortableHeader({ columnId, label, onSort }: SortableHeaderProps) {
    return (
        <button
            type="button"
            data-column-sort={columnId}
            className="inline-flex items-center gap-2 text-xs font-medium uppercase tracking-[0.22em] text-muted-foreground transition hover:text-foreground"
            onClick={onSort}
        >
            <span>{label}</span>
            <ArrowUpDown className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        </button>
    );
}

function formatRegisteredDate(value: string): string {
    return formatUtcDate(value);
}

function detailHref(id: string, returnHref: string): string {
    if (!returnHref || returnHref === "/") {
        return `/results/${id}`;
    }

    const searchParams = new URLSearchParams({ returnTo: returnHref });

    return `/results/${id}?${searchParams.toString()}`;
}

export function isResultsTableRowLocked(row: ResultsTableRow): boolean {
    return row.result.access?.locked === true;
}

function lockedTooltipMessage(result: ResultSet): string {
    if (result.access?.reason === "login_required") {
        return "Log in to view this result set";
    }

    return "You do not have access to this result set";
}

function tooltipId(id: string): string {
    return `locked-result-${id.replace(/[^a-zA-Z0-9_-]/g, "-")}-tooltip`;
}

function LockedResultIndicator({ result }: { result: ResultSet }) {
    const message = lockedTooltipMessage(result);
    const id = tooltipId(result.id);

    return (
        <span
            aria-describedby={id}
            aria-label={`Locked result: ${message}`}
            className="group relative inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full border border-border/70 bg-muted text-muted-foreground"
            title={message}
        >
            <LockKeyhole
                aria-hidden="true"
                className="h-3.5 w-3.5"
                data-locked-result-icon="true"
            />
            <span
                className="pointer-events-none absolute left-0 top-full z-20 mt-2 w-max max-w-56 rounded-md border border-border/70 bg-popover px-2 py-1 text-xs font-medium text-popover-foreground opacity-0 shadow-lg transition group-hover:opacity-100"
                id={id}
                role="tooltip"
            >
                {message}
            </span>
        </span>
    );
}

function projectName(result: ResultSet): string {
    const project = result.metadata?.project?.trim();

    return project || result.pipeline_name;
}

function resultCell(
    row: ResultsTableRow,
    value: string,
    className: string,
    returnHref: string,
    options: { showLock?: boolean } = {},
) {
    const isLocked = isResultsTableRowLocked(row);

    if (isLocked) {
        const text = (
            <span className={cn(className, "min-w-0 text-muted-foreground")}>
                {value}
            </span>
        );

        if (!options.showLock) {
            return text;
        }

        return (
            <span className="flex min-w-0 max-w-full items-start gap-2">
                <LockedResultIndicator result={row.result} />
                {text}
            </span>
        );
    }

    return (
        <a href={detailHref(row.id, returnHref)} className={className}>
            {value}
        </a>
    );
}

export function toResultsTableRows(
    data: ResultSet[] | SearchResult[],
): ResultsTableRow[] {
    return data.map((row) => {
        if ("result_set" in row) {
            return {
                id: row.result_set.id,
                result: row.result_set,
                matchedSamples: row.matched_samples ?? [],
                searchResult: true,
            };
        }

        return {
            id: row.id,
            result: row,
            matchedSamples: [],
            searchResult: false,
        };
    });
}

export function getResultsColumns(
    showMatchedSamples: boolean,
    returnHref = "/",
): ColumnDef<ResultsTableRow>[] {
    const columns: ColumnDef<ResultsTableRow>[] = [
        {
            accessorKey: "project",
            id: "project",
            accessorFn: (row) => projectName(row.result),
            header: ({ column }) => (
                <SortableHeader
                    columnId="project"
                    label="Project"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    projectName(row.original.result),
                    "font-medium text-foreground transition hover:text-primary",
                    returnHref,
                    { showLock: true },
                ),
        },
        {
            accessorKey: "pipeline_name",
            id: "pipeline_name",
            accessorFn: (row) => row.result.pipeline_name,
            header: ({ column }) => (
                <SortableHeader
                    columnId="pipeline_name"
                    label="Pipeline Name"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.result.pipeline_name,
                    "font-medium text-foreground transition hover:text-primary",
                    returnHref,
                    { showLock: true },
                ),
        },
        {
            accessorKey: "registration_unique",
            id: "registration_unique",
            accessorFn: (row) => formatRegistrationUnique(row.result.run_key),
            header: ({ column }) => (
                <SortableHeader
                    columnId="registration_unique"
                    label="Unique"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    formatRegistrationUnique(row.original.result.run_key),
                    "font-mono text-xs text-foreground",
                    returnHref,
                ),
        },
        {
            accessorKey: "requester",
            id: "requester",
            accessorFn: (row) => row.result.requester,
            header: ({ column }) => (
                <SortableHeader
                    columnId="requester"
                    label="Requester"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.result.requester,
                    "text-foreground",
                    returnHref,
                ),
        },
        {
            accessorKey: "created_at",
            id: "created_at",
            accessorFn: (row) => row.result.created_at,
            header: ({ column }) => (
                <SortableHeader
                    columnId="created_at"
                    label="Registered"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    formatRegisteredDate(row.original.result.created_at),
                    "text-muted-foreground",
                    returnHref,
                ),
        },
        {
            accessorKey: "output_directory",
            id: "output_directory",
            accessorFn: (row) => row.result.output_directory,
            header: ({ column }) => (
                <SortableHeader
                    columnId="output_directory"
                    label="Output Directory"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.result.output_directory,
                    "block max-w-full break-words font-mono text-xs leading-5 text-muted-foreground [overflow-wrap:anywhere]",
                    returnHref,
                ),
        },
        {
            accessorKey: "operator",
            id: "operator",
            accessorFn: (row) => row.result.operator,
            header: ({ column }) => (
                <SortableHeader
                    columnId="operator"
                    label="Operator"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.result.operator,
                    "text-muted-foreground",
                    returnHref,
                ),
        },
        {
            accessorKey: "command",
            id: "command",
            accessorFn: (row) => row.result.command,
            header: ({ column }) => (
                <SortableHeader
                    columnId="command"
                    label="Command"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.result.command,
                    "font-mono text-xs text-muted-foreground",
                    returnHref,
                ),
        },
        {
            accessorKey: "pipeline_version",
            id: "pipeline_version",
            accessorFn: (row) => row.result.pipeline_version,
            header: ({ column }) => (
                <SortableHeader
                    columnId="pipeline_version"
                    label="Pipeline Version"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.result.pipeline_version,
                    "text-muted-foreground",
                    returnHref,
                ),
        },
        {
            accessorKey: "pipeline_identifier",
            id: "pipeline_identifier",
            accessorFn: (row) => row.result.pipeline_identifier,
            header: ({ column }) => (
                <SortableHeader
                    columnId="pipeline_identifier"
                    label="Pipeline Identifier"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.result.pipeline_identifier,
                    "font-mono text-xs text-muted-foreground",
                    returnHref,
                ),
        },
        {
            accessorKey: "run_key",
            id: "run_key",
            accessorFn: (row) => row.result.run_key,
            header: ({ column }) => (
                <SortableHeader
                    columnId="run_key"
                    label="Stored Key"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.result.run_key,
                    "font-mono text-xs text-muted-foreground",
                    returnHref,
                ),
        },
        {
            accessorKey: "id",
            id: "id",
            accessorFn: (row) => row.id,
            header: ({ column }) => (
                <SortableHeader
                    columnId="id"
                    label="ID"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.id,
                    "font-mono text-xs text-muted-foreground",
                    returnHref,
                ),
        },
    ];

    if (showMatchedSamples) {
        columns.push({
            accessorKey: "matched_samples",
            id: "matched_samples",
            accessorFn: (row) => row.matchedSamples.join(", "),
            header: ({ column }) => (
                <SortableHeader
                    columnId="matched_samples"
                    label="Matched Samples"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                resultCell(
                    row.original,
                    row.original.matchedSamples.length > 0
                        ? row.original.matchedSamples.join(", ")
                        : "-",
                    "text-muted-foreground",
                    returnHref,
                ),
        });
    }

    return columns;
}
