import type { ReactNode } from "react";

import { usageCategorySchema } from "@/lib/contracts";
import type { CountMap } from "@/lib/usage-derive";

import { categoryColor, categoryLabels, formatCount } from "./format";

export type RankedRow = {
    // badge follows the count, such as an error severity.
    badge?: ReactNode;
    // byCategory splits the bar by category colour; without it the bar is
    // one colour.
    byCategory?: CountMap;
    color?: string;
    // detail is a second line under the label.
    detail?: ReactNode;
    // expansion, when set, is shown by a disclosure under the row named
    // expansionLabel.
    expansion?: ReactNode;
    expansionLabel?: string;
    href?: string;
    hrefLabel?: string;
    key: string;
    label: string;
    total: number;
};

const visibleRows = 5;

const linkClassName =
    "underline decoration-border underline-offset-4 hover:decoration-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none";

function Bar({ max, row }: { max: number; row: RankedRow }) {
    const width = max > 0 ? (row.total / max) * 100 : 0;
    const segments = row.byCategory
        ? usageCategorySchema.options
              .filter((category) => (row.byCategory![category] ?? 0) > 0)
              .map((category) => ({
                  color: categoryColor(category),
                  key: category,
                  label: categoryLabels[category],
                  value: row.byCategory![category],
              }))
        : [];

    return (
        <span
            aria-hidden="true"
            className="flex h-2.5 w-full overflow-hidden rounded-full bg-muted/60"
        >
            <span
                className="flex h-full gap-px overflow-hidden rounded-full"
                style={{ width: `${Math.max(width, row.total > 0 ? 1 : 0)}%` }}
            >
                {segments.length > 0 ? (
                    segments.map((segment) => (
                        <span
                            className="h-full"
                            data-category={segment.key}
                            key={segment.key}
                            style={{
                                background: segment.color,
                                flexGrow: segment.value,
                            }}
                            title={`${segment.label} ${segment.value}`}
                        />
                    ))
                ) : (
                    <span
                        className="h-full w-full"
                        style={{ background: row.color ?? "var(--usage-cli)" }}
                    />
                )}
            </span>
        </span>
    );
}

function RowBody({ max, row }: { max: number; row: RankedRow }) {
    return (
        <span className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 sm:grid-cols-[minmax(7rem,14rem)_minmax(0,1fr)_auto]">
            <span className="flex min-w-0 flex-col">
                <span className="text-sm wrap-anywhere">
                    {row.href ? (
                        <a
                            aria-label={row.hrefLabel}
                            className={linkClassName}
                            href={row.href}
                        >
                            {row.label}
                        </a>
                    ) : (
                        row.label
                    )}
                </span>
                {row.detail ? (
                    <span className="text-xs text-muted-foreground wrap-anywhere">
                        {row.detail}
                    </span>
                ) : null}
            </span>
            <span className="col-span-2 row-start-2 sm:col-span-1 sm:row-start-auto">
                <Bar max={max} row={row} />
            </span>
            <span className="col-start-2 row-start-1 flex items-center justify-end gap-2 text-sm tabular-nums sm:col-start-auto sm:row-start-auto">
                {formatCount(row.total)}
                {row.badge}
            </span>
        </span>
    );
}

function Row({ max, row }: { max: number; row: RankedRow }) {
    return (
        <li className="px-1 py-1.5" data-testid="ranked-row">
            <RowBody max={max} row={row} />
            {row.expansion ? (
                <details className="mt-1 rounded-lg open:bg-muted/30">
                    <summary className="cursor-pointer rounded-md px-1 text-xs font-medium text-muted-foreground hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                        {row.expansionLabel ?? "Details"}
                    </summary>
                    <div className="px-1 pt-1 pb-2">{row.expansion}</div>
                </details>
            ) : null}
        </li>
    );
}

// RankedBars lists rows as horizontal bars scaled to the largest, the first
// five openly and the rest behind a "Show all" disclosure.
export function RankedBars({
    empty = "None.",
    label,
    rows,
}: {
    empty?: string;
    label: string;
    rows: RankedRow[];
}) {
    if (rows.length === 0) {
        return <p className="text-sm text-muted-foreground">{empty}</p>;
    }

    const max = Math.max(...rows.map((row) => row.total));
    const shown = rows.slice(0, visibleRows);
    const rest = rows.slice(visibleRows);

    return (
        <div className="flex flex-col">
            <ul aria-label={label} className="flex flex-col">
                {shown.map((row) => (
                    <Row key={row.key} max={max} row={row} />
                ))}
            </ul>
            {rest.length > 0 ? (
                <details>
                    <summary className="cursor-pointer rounded-md px-1 py-1 text-sm font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                        Show all {formatCount(rows.length)}
                    </summary>
                    <ul
                        aria-label={`${label}, the rest`}
                        className="flex flex-col"
                    >
                        {rest.map((row) => (
                            <Row key={row.key} max={max} row={row} />
                        ))}
                    </ul>
                </details>
            ) : null}
        </div>
    );
}
