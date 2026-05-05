import { ArrowUpDown } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";

import type { ResultSet, SearchResult } from "@/lib/contracts";
import { formatUtcDate } from "@/lib/utils";

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
            <ArrowUpDown className="h-3.5 w-3.5" aria-hidden="true" />
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

function linkedCell(
    id: string,
    value: string,
    className: string,
    returnHref: string,
) {
    return (
        <a href={detailHref(id, returnHref)} className={className}>
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
                linkedCell(
                    row.original.id,
                    row.original.result.pipeline_name,
                    "font-medium text-foreground transition hover:text-primary",
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
                linkedCell(
                    row.original.id,
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
                linkedCell(
                    row.original.id,
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
                linkedCell(
                    row.original.id,
                    row.original.result.output_directory,
                    "font-mono text-xs text-muted-foreground",
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
                linkedCell(
                    row.original.id,
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
                linkedCell(
                    row.original.id,
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
                linkedCell(
                    row.original.id,
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
                linkedCell(
                    row.original.id,
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
                    label="Run Key"
                    onSort={() =>
                        column.toggleSorting(column.getIsSorted() === "asc")
                    }
                />
            ),
            cell: ({ row }) =>
                linkedCell(
                    row.original.id,
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
                linkedCell(
                    row.original.id,
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
            cell: ({ row }) => (
                <a
                    href={detailHref(row.original.id, returnHref)}
                    className="text-muted-foreground"
                >
                    {row.original.matchedSamples.length > 0
                        ? row.original.matchedSamples.join(", ")
                        : "-"}
                </a>
            ),
        });
    }

    return columns;
}
