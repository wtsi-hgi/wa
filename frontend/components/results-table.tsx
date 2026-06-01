"use client";

import { useMemo, useState } from "react";

import {
    type ColumnDef,
    flexRender,
    getCoreRowModel,
    getPaginationRowModel,
    getSortedRowModel,
    type SortingState,
    useReactTable,
} from "@tanstack/react-table";
import { ChevronDown, ChevronLeft, ChevronRight } from "lucide-react";

import {
    getResultsColumns,
    isResultsTableRowLocked,
    toResultsTableRows,
    type ResultsTableRow,
} from "@/components/results-columns";
import {
    DropdownMenu,
    DropdownMenuCheckboxItem,
    DropdownMenuContent,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { ResultSet, SearchResult } from "@/lib/contracts";
import { cn } from "@/lib/utils";

type ResultsTableProps = {
    data: ResultSet[] | SearchResult[];
    emptyMessage?: string;
    mode?: "recent" | "search";
    returnHref?: string;
    studyActive?: boolean;
};

const defaultHiddenColumns: Record<string, boolean> = {
    operator: false,
    command: false,
    pipeline_version: false,
    pipeline_identifier: false,
    run_key: false,
    id: false,
};

function columnVisibilityLabel(columnId: string): string {
    if (columnId === "id") {
        return "ID";
    }

    if (columnId === "run_key") {
        return "Stored Key";
    }

    if (columnId === "registration_unique") {
        return "Unique";
    }

    return columnId
        .split("_")
        .map((segment) => segment.charAt(0).toUpperCase() + segment.slice(1))
        .join(" ");
}

export function ResultsTable({
    data,
    emptyMessage = "No results found.",
    mode = "recent",
    returnHref = "/",
    studyActive = false,
}: ResultsTableProps) {
    const rows = useMemo(() => toResultsTableRows(data), [data]);
    const showMatchedSamples =
        studyActive && rows.some((row) => row.searchResult);
    const columns = useMemo<ColumnDef<ResultsTableRow>[]>(
        () => getResultsColumns(showMatchedSamples, returnHref),
        [returnHref, showMatchedSamples],
    );
    const [sorting, setSorting] = useState<SortingState>([]);
    const [columnVisibility, setColumnVisibility] =
        useState<Record<string, boolean>>(defaultHiddenColumns);
    const [pagination, setPagination] = useState({
        pageIndex: 0,
        pageSize: 10,
    });

    // TanStack Table's hook currently triggers a React Compiler compatibility lint false positive.
    // eslint-disable-next-line react-hooks/incompatible-library
    const table = useReactTable({
        columns,
        data: rows,
        getCoreRowModel: getCoreRowModel(),
        getPaginationRowModel: getPaginationRowModel(),
        getSortedRowModel: getSortedRowModel(),
        onColumnVisibilityChange: setColumnVisibility,
        onPaginationChange: setPagination,
        onSortingChange: setSorting,
        state: {
            columnVisibility,
            pagination,
            sorting,
        },
    });

    const visibleColumns = table
        .getAllLeafColumns()
        .filter((column) => column.getCanHide());

    return (
        <div className="overflow-hidden rounded-[1.75rem] border border-border/70 bg-card/85 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]">
            <div
                className="flex items-center justify-between gap-4 border-b border-border/70 px-6 py-5"
                data-results-table-summary="true"
            >
                <div>
                    <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
                        {mode === "search"
                            ? "Showing search results"
                            : "Recent registrations"}
                    </p>
                    <h2 className="mt-2 text-2xl font-semibold tracking-tight">
                        {mode === "search"
                            ? "Matching result sets"
                            : "Latest result sets"}
                    </h2>
                </div>
                <div className="flex items-center gap-3">
                    <DropdownMenu>
                        <DropdownMenuTrigger
                            aria-label="Toggle column visibility"
                            className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/90 px-3 py-2 text-sm text-muted-foreground transition hover:text-foreground"
                        >
                            <span>Columns</span>
                            <ChevronDown
                                className="h-4 w-4"
                                aria-hidden="true"
                            />
                        </DropdownMenuTrigger>
                        <DropdownMenuContent>
                            {visibleColumns.map((column) => (
                                <DropdownMenuCheckboxItem
                                    key={column.id}
                                    checked={column.getIsVisible()}
                                    data-column-id={column.id}
                                    onCheckedChange={(checked) =>
                                        column.toggleVisibility(checked)
                                    }
                                >
                                    {columnVisibilityLabel(column.id)}
                                </DropdownMenuCheckboxItem>
                            ))}
                        </DropdownMenuContent>
                    </DropdownMenu>

                    <p className="rounded-full border border-border/70 bg-background/90 px-3 py-1 text-sm text-muted-foreground">
                        {rows.length} rows
                    </p>
                </div>
            </div>

            {rows.length === 0 ? (
                <div className="px-6 py-10 text-sm leading-7 text-muted-foreground">
                    {emptyMessage}
                </div>
            ) : (
                <div className="overflow-x-auto">
                    <table className="min-w-full divide-y divide-border/70 text-left text-sm">
                        <thead className="bg-muted/40">
                            {table.getHeaderGroups().map((headerGroup) => (
                                <tr key={headerGroup.id}>
                                    {headerGroup.headers.map((header) => (
                                        <th
                                            key={header.id}
                                            className="px-6 py-4 font-medium"
                                        >
                                            {header.isPlaceholder
                                                ? null
                                                : flexRender(
                                                      header.column.columnDef
                                                          .header,
                                                      header.getContext(),
                                                  )}
                                        </th>
                                    ))}
                                </tr>
                            ))}
                        </thead>
                        <tbody className="divide-y divide-border/60">
                            {table.getRowModel().rows.map((row) => {
                                const locked = isResultsTableRowLocked(
                                    row.original,
                                );

                                return (
                                    <tr
                                        key={row.id}
                                        aria-disabled={
                                            locked ? "true" : undefined
                                        }
                                        data-result-row="true"
                                        data-result-row-locked={
                                            locked ? "true" : undefined
                                        }
                                        className={cn(
                                            "bg-card/60",
                                            locked &&
                                                "cursor-not-allowed opacity-60",
                                        )}
                                    >
                                        {row.getVisibleCells().map((cell) => (
                                            <td
                                                key={cell.id}
                                                className="px-6 py-4 align-top"
                                            >
                                                {flexRender(
                                                    cell.column.columnDef.cell,
                                                    cell.getContext(),
                                                )}
                                            </td>
                                        ))}
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                </div>
            )}

            {rows.length > 0 ? (
                <div className="flex flex-col gap-4 border-t border-border/70 px-6 py-5 sm:flex-row sm:items-center sm:justify-between">
                    <div className="flex items-center gap-3 text-sm text-muted-foreground">
                        <label htmlFor="results-page-size">Rows per page</label>
                        <select
                            id="results-page-size"
                            aria-label="Rows per page"
                            className="rounded-xl border border-border/70 bg-background px-3 py-2 text-foreground outline-none transition focus:border-primary"
                            value={String(table.getState().pagination.pageSize)}
                            onChange={(event) => {
                                table.setPageSize(Number(event.target.value));
                            }}
                        >
                            <option value="10">10</option>
                            <option value="25">25</option>
                            <option value="50">50</option>
                        </select>
                    </div>

                    <div className="flex items-center justify-between gap-4 sm:justify-end">
                        <p className="text-sm text-muted-foreground">
                            Page {table.getState().pagination.pageIndex + 1} of{" "}
                            {table.getPageCount()}
                        </p>
                        <div className="flex items-center gap-2">
                            <button
                                type="button"
                                className="inline-flex h-10 w-10 items-center justify-center rounded-full border border-border/70 bg-background/90 text-muted-foreground transition hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                                disabled={!table.getCanPreviousPage()}
                                onClick={() => table.previousPage()}
                            >
                                <ChevronLeft
                                    className="h-4 w-4"
                                    aria-hidden="true"
                                />
                                <span className="sr-only">Previous page</span>
                            </button>
                            <button
                                type="button"
                                className="inline-flex h-10 w-10 items-center justify-center rounded-full border border-border/70 bg-background/90 text-muted-foreground transition hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                                disabled={!table.getCanNextPage()}
                                onClick={() => table.nextPage()}
                            >
                                <ChevronRight
                                    className="h-4 w-4"
                                    aria-hidden="true"
                                />
                                <span className="sr-only">Next page</span>
                            </button>
                        </div>
                    </div>
                </div>
            ) : null}
        </div>
    );
}
