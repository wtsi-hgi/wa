"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";

import { cn } from "@/lib/utils";

export type PreviewPaginationProps = {
    className?: string;
    nextLabel?: string;
    onPageChange: (page: number) => void;
    page: number;
    pageCount: number;
    previousLabel?: string;
    selectLabel?: string;
};

export function PreviewPagination({
    className,
    nextLabel = "Next page",
    onPageChange,
    page,
    pageCount,
    previousLabel = "Previous page",
    selectLabel = "Preview page",
}: PreviewPaginationProps) {
    if (pageCount <= 1) {
        return null;
    }

    return (
        <div
            className={cn(
                "inline-flex items-center gap-1 rounded-full border border-border/70 bg-background/75 p-1",
                className,
            )}
        >
            <button
                type="button"
                aria-label={previousLabel}
                className="inline-flex size-8 items-center justify-center rounded-full text-foreground transition hover:bg-muted disabled:cursor-not-allowed disabled:opacity-45"
                disabled={page <= 1}
                onClick={() => onPageChange(Math.max(1, page - 1))}
            >
                <ChevronLeft className="size-4" aria-hidden="true" />
            </button>
            <select
                aria-label={selectLabel}
                className="h-8 rounded-full border border-border/70 bg-background px-2 text-sm text-foreground"
                onChange={(event) => onPageChange(Number(event.target.value))}
                value={page}
            >
                {Array.from({ length: pageCount }, (_, index) => index + 1).map(
                    (pageNumber) => (
                        <option key={pageNumber} value={pageNumber}>
                            {pageNumber}
                        </option>
                    ),
                )}
            </select>
            <button
                type="button"
                aria-label={nextLabel}
                className="inline-flex size-8 items-center justify-center rounded-full text-foreground transition hover:bg-muted disabled:cursor-not-allowed disabled:opacity-45"
                disabled={page >= pageCount}
                onClick={() => onPageChange(Math.min(pageCount, page + 1))}
            >
                <ChevronRight className="size-4" aria-hidden="true" />
            </button>
        </div>
    );
}
