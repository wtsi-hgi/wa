// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { ResultsTable } from "@/components/results-table";
import type { ResultSet, SearchResult } from "@/lib/contracts";

function buildResultSet(index: number): ResultSet {
    const day = String((index % 28) + 1).padStart(2, "0");

    return {
        id: `result-${index}`,
        pipeline_identifier: `gh://repo/workflow-${index}.nf`,
        run_key: `run-${index}`,
        requester: index % 2 === 0 ? "alice" : "bob",
        operator: `operator-${index}`,
        command: `nextflow run workflow-${index}.nf`,
        pipeline_name: `pipeline-${String.fromCharCode(65 + (index % 26))}`,
        pipeline_version: `1.${index}.0`,
        output_directory: `/tmp/results/${index}`,
        metadata: {
            seqmeta_sampleid: `SANG${index}`,
        },
        created_at: `2026-04-${day}T12:00:00Z`,
        updated_at: `2026-04-${day}T12:30:00Z`,
    };
}

function buildSearchResult(
    index: number,
    matchedSamples?: string[],
): SearchResult {
    return {
        result_set: buildResultSet(index),
        matched_samples: matchedSamples,
    };
}

function getBodyRows(container: HTMLElement): HTMLTableRowElement[] {
    return Array.from(container.querySelectorAll("tbody tr"));
}

function getHeaderLabels(container: HTMLElement): string[] {
    return Array.from(container.querySelectorAll("thead th")).map(
        (cell) => cell.textContent?.trim() ?? "",
    );
}

async function click(target: Element | null): Promise<void> {
    if (!(target instanceof HTMLElement)) {
        throw new Error("Expected clickable HTMLElement");
    }

    await act(async () => {
        target.click();
    });
}

async function changeSelect(
    target: Element | null,
    value: string,
): Promise<void> {
    if (!(target instanceof HTMLSelectElement)) {
        throw new Error("Expected HTMLSelectElement");
    }

    await act(async () => {
        target.value = value;
        target.dispatchEvent(new Event("change", { bubbles: true }));
    });
}

describe("L1 results table", () => {
    let container: HTMLDivElement;
    let root: Root;

    beforeEach(() => {
        container = document.createElement("div");
        document.body.appendChild(container);
        root = createRoot(container);
    });

    afterEach(async () => {
        await act(async () => {
            root.unmount();
        });
        container.remove();
    });

    it("shows 10 rows by default for 25 results and renders page 1 of 3", async () => {
        const data = Array.from({ length: 25 }, (_, index) =>
            buildResultSet(index + 1),
        );

        await act(async () => {
            root.render(createElement(ResultsTable, { data }));
        });

        expect(getBodyRows(container)).toHaveLength(10);
        expect(container.textContent).toContain("Page 1 of 3");
    });

    it("hides the requester column when toggled from the column visibility menu", async () => {
        await act(async () => {
            root.render(createElement(ResultsTable, { data: [buildResultSet(1)] }));
        });

        expect(getHeaderLabels(container)).toContain("Requester");

        await click(
            container.querySelector('button[aria-label="Toggle column visibility"]'),
        );
        await click(
            container.querySelector(
                'button[role="menuitemcheckbox"][data-column-id="requester"]',
            ),
        );

        expect(getHeaderLabels(container)).not.toContain("Requester");
        expect(container.textContent).not.toContain("alice");
        expect(container.textContent).not.toContain("bob");
    });

    it("sorts by pipeline name ascending and descending when the header is clicked", async () => {
        const data = [buildResultSet(1), buildResultSet(2), buildResultSet(3)].map(
            (row, index) => ({
                ...row,
                pipeline_name: ["gamma", "alpha", "beta"][index],
            }),
        );

        await act(async () => {
            root.render(createElement(ResultsTable, { data }));
        });

        const pipelineHeader = container.querySelector(
            'button[data-column-sort="pipeline_name"]',
        );

        await click(pipelineHeader);

        expect(
            getBodyRows(container).map((row) => row.textContent ?? "")[0],
        ).toContain("alpha");
        expect(
            getBodyRows(container).map((row) => row.textContent ?? "")[2],
        ).toContain("gamma");

        await click(pipelineHeader);

        expect(
            getBodyRows(container).map((row) => row.textContent ?? "")[0],
        ).toContain("gamma");
        expect(
            getBodyRows(container).map((row) => row.textContent ?? "")[2],
        ).toContain("alpha");
    });

    it("shows the empty state when there are no results", async () => {
        await act(async () => {
            root.render(createElement(ResultsTable, { data: [] }));
        });

        expect(container.textContent).toContain("No results found.");
    });

    it("keeps command, pipeline version, pipeline identifier, run key, operator, and id hidden by default", async () => {
        await act(async () => {
            root.render(createElement(ResultsTable, { data: [buildResultSet(1)] }));
        });

        const headers = getHeaderLabels(container);

        expect(headers).not.toContain("Command");
        expect(headers).not.toContain("Pipeline version");
        expect(headers).not.toContain("Pipeline identifier");
        expect(headers).not.toContain("Run key");
        expect(headers).not.toContain("Operator");
        expect(headers).not.toContain("ID");
    });

    it("shows the matched samples column and values when studyActive is true for search results", async () => {
        const data = [
            buildSearchResult(1, ["SANG1", "SANG2"]),
            buildSearchResult(2),
        ];

        await act(async () => {
            root.render(createElement(ResultsTable, { data, studyActive: true }));
        });

        expect(getHeaderLabels(container)).toContain("Matched Samples");
        expect(container.textContent).toContain("SANG1, SANG2");
    });

    it("does not show the matched samples column when studyActive is false", async () => {
        const data = [
            buildSearchResult(1, ["SANG1", "SANG2"]),
            buildSearchResult(2),
        ];

        await act(async () => {
            root.render(createElement(ResultsTable, { data, studyActive: false }));
        });

        expect(getHeaderLabels(container)).not.toContain("Matched Samples");
    });

    it("switches pagination page size to 25 and keeps row links pointing at result details", async () => {
        const data = Array.from({ length: 26 }, (_, index) =>
            buildResultSet(index + 1),
        );

        await act(async () => {
            root.render(createElement(ResultsTable, { data }));
        });

        await changeSelect(
            container.querySelector('select[aria-label="Rows per page"]'),
            "25",
        );

        expect(getBodyRows(container)).toHaveLength(25);
        expect(container.textContent).toContain("Page 1 of 2");
        expect(
            container.querySelector('a[href="/results/result-1"]'),
        ).not.toBeNull();
    });
});
