"use client";

import { useEffect, useMemo, useRef, useState } from "react";

import {
    type ColumnDef,
    flexRender,
    getCoreRowModel,
    getPaginationRowModel,
    getSortedRowModel,
    type SortingState,
    useReactTable,
} from "@tanstack/react-table";
import {
    ChevronDown,
    ChevronLeft,
    ChevronRight,
    History,
    Search,
} from "lucide-react";

import {
    boxPanelRadiusClass,
    boxTitleActionClass,
    boxTitleIconClass,
    boxTitleRowClass,
    boxTitleSectionClass,
    boxTitleTextClass,
} from "@/components/box-title-section";
import {
    defaultResultsColumnVisibility,
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

export type ResultsTableProps = {
    data: ResultSet[] | SearchResult[];
    emptyMessage?: string;
    hideSummary?: boolean;
    mode?: "recent" | "search";
    returnHref?: string;
    studyActive?: boolean;
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
    hideSummary = false,
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
    const [columnVisibility, setColumnVisibility] = useState<
        Record<string, boolean>
    >(() => defaultResultsColumnVisibility());
    const previousMode = useRef(mode);
    const [pagination, setPagination] = useState({
        pageIndex: 0,
        pageSize: 10,
    });

    useEffect(() => {
        if (previousMode.current === mode) {
            return;
        }

        previousMode.current = mode;
        setColumnVisibility(defaultResultsColumnVisibility());
    }, [mode]);

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
    const titleConfig =
        mode === "search"
            ? { Icon: Search, label: "Search results" }
            : { Icon: History, label: "Latest result sets" };

    return (
        <div
            className={cn(
                "overflow-hidden border border-border/70 bg-card/85 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]",
                boxPanelRadiusClass,
                hideSummary ? "" : "pt-4",
            )}
        >
            {hideSummary ? null : (
                <div
                    className={cn(
                        boxTitleSectionClass,
                        "border-b border-border/70 px-4",
                    )}
                    data-results-table-summary="true"
                >
                    <div className="min-w-0">
                        <div className={boxTitleRowClass}>
                            <titleConfig.Icon
                                className={boxTitleIconClass}
                                aria-hidden="true"
                            />
                            <p className={boxTitleTextClass}>
                                {titleConfig.label}
                            </p>
                        </div>
                    </div>
                    <div className="flex items-center gap-3">
                        <DropdownMenu>
                            <DropdownMenuTrigger
                                aria-label="Toggle column visibility"
                                className={boxTitleActionClass}
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
                    </div>
                </div>
            )}

            {rows.length === 0 ? (
                <div className="px-6 py-10 text-sm leading-7 text-muted-foreground">
                    {emptyMessage}
                </div>
            ) : (
                <div className="overflow-x-auto">
                    <table className="w-full table-fixed divide-y divide-border/70 text-left text-sm">
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
                        <p className="rounded-full border border-border/70 bg-background/90 px-3 py-1 text-sm text-muted-foreground">
                            {rows.length} rows
                        </p>
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
