import { describe, expect, it } from "vitest";

import { nextTableSort, sortTableRows, tableToTsv } from "@/lib/table-data";

describe("table data helpers", () => {
    const rows = [
        ["gamma", "10"],
        ["alpha", "9"],
        ["Beta", "100"],
        ["delta", ""],
    ];

    it("sorts numeric columns numerically and text columns naturally", () => {
        expect(
            sortTableRows(rows, { direction: "asc", index: 1 }).map(
                (row) => row[0],
            ),
        ).toEqual(["delta", "alpha", "gamma", "Beta"]);
        expect(
            sortTableRows(rows, { direction: "desc", index: 0 }).map(
                (row) => row[0],
            ),
        ).toEqual(["gamma", "delta", "Beta", "alpha"]);
        expect(
            sortTableRows([["run10"], ["run9"], ["run100"]], {
                direction: "asc",
                index: 0,
            }).map((row) => row[0]),
        ).toEqual(["run9", "run10", "run100"]);
    });

    it("keeps ties in their original order and leaves the input untouched", () => {
        const tied = [
            ["a", "1"],
            ["b", "1"],
            ["c", "0"],
        ];
        const sorted = sortTableRows(tied, { direction: "desc", index: 1 });

        expect(sorted.map((row) => row[0])).toEqual(["a", "b", "c"]);
        expect(tied.map((row) => row[0])).toEqual(["a", "b", "c"]);
        expect(sortTableRows(tied, null)).toEqual(tied);
    });

    it("cycles a column from ascending to descending and restarts on a new column", () => {
        expect(nextTableSort(null, 2)).toEqual({ direction: "asc", index: 2 });
        expect(nextTableSort({ direction: "asc", index: 2 }, 2)).toEqual({
            direction: "desc",
            index: 2,
        });
        expect(nextTableSort({ direction: "desc", index: 2 }, 2)).toEqual({
            direction: "asc",
            index: 2,
        });
        expect(nextTableSort({ direction: "desc", index: 2 }, 0)).toEqual({
            direction: "asc",
            index: 0,
        });
    });

    it("serialises rows as TSV, flattening tabs and newlines and padding short rows", () => {
        expect(
            tableToTsv(
                ["sample", "note", "count"],
                [
                    ["a", "two\twords", "1"],
                    ["b", "line\r\nbreak"],
                ],
            ),
        ).toBe("sample\tnote\tcount\na\ttwo words\t1\nb\tline break\t\n");
        expect(tableToTsv(["only"], [])).toBe("only\n");
    });
});
