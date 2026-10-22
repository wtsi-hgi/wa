export type TableSortDirection = "asc" | "desc";

export type TableSort = {
    direction: TableSortDirection;
    index: number;
};

function compareTableCells(left: string, right: string): number {
    const numericLeft = Number(left);
    const numericRight = Number(right);

    if (
        left.trim() !== "" &&
        right.trim() !== "" &&
        !Number.isNaN(numericLeft) &&
        !Number.isNaN(numericRight)
    ) {
        return numericLeft - numericRight;
    }

    return left.localeCompare(right, undefined, {
        numeric: true,
        sensitivity: "base",
    });
}

/**
 * Returns rows sorted by one column, numerically when both cells are numbers
 * and with natural string order otherwise. Equal rows keep their order.
 */
export function sortTableRows(
    rows: readonly string[][],
    sort: TableSort | null,
): string[][] {
    if (!sort) {
        return [...rows];
    }

    const sign = sort.direction === "asc" ? 1 : -1;

    return rows
        .map((row, position) => ({ position, row }))
        .sort(
            (left, right) =>
                sign *
                    compareTableCells(
                        left.row[sort.index] ?? "",
                        right.row[sort.index] ?? "",
                    ) || left.position - right.position,
        )
        .map(({ row }) => row);
}

/** Cycles a header click: a new column sorts ascending, then descending. */
export function nextTableSort(
    current: TableSort | null,
    index: number,
): TableSort {
    if (current?.index === index && current.direction === "asc") {
        return { direction: "desc", index };
    }

    return { direction: "asc", index };
}

function tsvCell(value: string): string {
    return value.replaceAll(/[\t\r\n]+/g, " ");
}

/**
 * Serialises a table as tab-separated values with a header line. Tabs and
 * line breaks inside cells become spaces so every row stays one TSV line,
 * and short rows are padded to the header width.
 */
export function tableToTsv(
    headers: readonly string[],
    rows: readonly string[][],
): string {
    const width = headers.length;
    const lines = [
        headers.map(tsvCell).join("\t"),
        ...rows.map((row) =>
            Array.from({ length: width }, (_, index) =>
                tsvCell(row[index] ?? ""),
            ).join("\t"),
        ),
    ];

    return `${lines.join("\n")}\n`;
}
