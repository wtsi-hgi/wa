import { mkdirSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Page } from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultRegistration,
    type ResultSet,
} from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const fixtureRoot = path.join(evidenceDir, "real-data-layout-repro-fixture");
const pipelineName = "wa/real-data-layout-repro";
const longSegment = `realDataOutputDirectorySegment${"A".repeat(180)}`;
const longOutputDirectory = path.join(
    fixtureRoot,
    "nfs",
    "lustre",
    "groups",
    "humgen",
    "teams",
    "informatics",
    "workflows",
    "2026",
    "06",
    "03",
    longSegment,
    "project",
    "study-87654321",
    "sample-SANG-REAL-DATA-LAYOUT-0000000000000001",
    "analysis",
    "nextflow",
    "work",
    "publish",
    "multiqc-and-deliverables",
);
const outputFilePath = path.join(
    longOutputDirectory,
    "real-data-layout-repro-summary.tsv",
);

let registeredResult: ResultSet | null = null;

type RectMetric = {
    bottom: number;
    height: number;
    left: number;
    right: number;
    top: number;
    width: number;
};

type PermanentFieldMetric = {
    button: RectMetric;
    field: RectMetric;
    icon: RectMetric | null;
    input: RectMetric;
    key: string;
    label: string;
};

type SearchLayoutMetric = {
    fieldCount: number;
    fieldWidthRange: {
        max: number;
        min: number;
    };
    firstRowFieldCount: number;
    grid: RectMetric;
    plusButtonSizes: Array<{
        height: number;
        iconHeight: number | null;
        iconWidth: number | null;
        key: string;
        width: number;
    }>;
    rowCount: number;
    rows: number[];
};

type LatestLayoutMetric = {
    box: RectMetric;
    outputCell: RectMetric;
    outputCellHorizontalOverflow: number;
    outputText: RectMetric;
    outputTextLineCount: number;
    outputTextLength: number;
    scroller: RectMetric;
    table: RectMetric;
    tableHorizontalOverflow: number;
};

type LayoutMetric = {
    latest: LatestLayoutMetric;
    search: SearchLayoutMetric;
    viewport: {
        height: number;
        width: number;
    };
};

test.beforeAll(() => {
    mkdirSync(longOutputDirectory, { recursive: true });
    writeFileSync(
        outputFilePath,
        "sample\tstatus\nSANG-REAL-DATA-LAYOUT-0000000000000001\tcomplete\n",
    );

    const stats = statSync(outputFilePath);
    const registration: ResultRegistration = {
        pipeline_identifier:
            "https://github.com/wtsi-hgi/wa-real-data-layout-repro",
        run_key: "runid=260603&unique=real-data-layout-repro",
        requester: "real-data-layout-requester",
        operator: "real-data-layout-operator",
        command: `nextflow run ${pipelineName} --sample SANG-REAL-DATA-LAYOUT-0000000000000001`,
        pipeline_name: pipelineName,
        pipeline_version: "2026.06.03",
        output_directory: longOutputDirectory,
        metadata: {
            sample: "SANG-REAL-DATA-LAYOUT-0000000000000001",
            study: "87654321",
        },
        files: [
            {
                kind: "output",
                mtime: stats.mtime.toISOString(),
                path: outputFilePath,
                size: stats.size,
            },
        ],
    };

    registeredResult = registerResult(registration);
});

test.afterAll(() => {
    if (registeredResult) {
        deleteResult(registeredResult.id);
    }
});

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

async function collectLayoutMetric(page: Page): Promise<LayoutMetric> {
    return page.evaluate(
        ({ pipelineName: expectedPipelineName }) => {
            const toBrowserRect = (element: Element): RectMetric => {
                const rect = element.getBoundingClientRect();

                return {
                    bottom: rect.bottom,
                    height: rect.height,
                    left: rect.left,
                    right: rect.right,
                    top: rect.top,
                    width: rect.width,
                };
            };

            const searchGrid = document.querySelector<HTMLElement>(
                '[data-search-builder-permanent-fields="true"]',
            );

            if (!searchGrid) {
                throw new Error("Missing permanent search fields grid");
            }

            const fields: PermanentFieldMetric[] = Array.from(
                searchGrid.querySelectorAll("form"),
            ).map((form) => {
                const input = form.querySelector<HTMLInputElement>(
                    "[data-permanent-filter-input]",
                );
                const button = form.querySelector<HTMLButtonElement>(
                    'button[type="submit"]',
                );
                const label = form.querySelector("label");
                const icon = button?.querySelector("svg") ?? null;

                if (!input || !button || !label) {
                    throw new Error(
                        "Missing permanent field measurement target",
                    );
                }

                return {
                    button: toBrowserRect(button),
                    field: toBrowserRect(form),
                    icon: icon ? toBrowserRect(icon) : null,
                    input: toBrowserRect(input),
                    key: input.dataset.permanentFilterInput ?? "",
                    label: label.textContent?.trim() ?? "",
                };
            });
            const rowTops = [
                ...new Set(fields.map((field) => Math.round(field.field.top))),
            ].sort((left, right) => left - right);
            const firstRowTop = rowTops[0] ?? 0;
            const firstRowFieldCount = fields.filter(
                (field) => Math.round(field.field.top) === firstRowTop,
            ).length;
            const fieldWidths = fields.map((field) => field.field.width);

            const summary = document.querySelector<HTMLElement>(
                '[data-results-table-summary="true"]',
            );

            if (!summary) {
                throw new Error("Missing latest result sets summary");
            }

            const latestBox = summary.parentElement;

            if (!(latestBox instanceof HTMLElement)) {
                throw new Error("Missing latest result sets box");
            }

            const scroller = Array.from(latestBox.querySelectorAll("div")).find(
                (candidate): candidate is HTMLElement =>
                    candidate instanceof HTMLElement &&
                    candidate.querySelector("table") instanceof
                        HTMLTableElement &&
                    window.getComputedStyle(candidate).overflowX === "auto",
            );

            if (!scroller) {
                throw new Error(
                    "Missing latest result sets horizontal scroller",
                );
            }

            const table = scroller.querySelector("table");

            if (!(table instanceof HTMLTableElement)) {
                throw new Error("Missing latest result sets table");
            }

            const resultRow = Array.from(
                table.querySelectorAll<HTMLElement>(
                    'tbody tr[data-result-row="true"]',
                ),
            ).find((row) => row.textContent?.includes(expectedPipelineName));

            if (!resultRow) {
                throw new Error(
                    `Missing long-path result row for ${expectedPipelineName}`,
                );
            }

            const headers = Array.from(table.querySelectorAll("thead th"));
            const outputColumnIndex = headers.findIndex((header) =>
                header.textContent?.includes("Output Directory"),
            );

            if (outputColumnIndex < 0) {
                throw new Error("Missing Output Directory column");
            }

            const outputCell =
                resultRow.querySelectorAll("td")[outputColumnIndex];
            const outputText = outputCell?.firstElementChild;

            if (!outputCell || !outputText) {
                throw new Error("Missing Output Directory cell");
            }

            const outputTextStyles = window.getComputedStyle(outputText);
            const lineHeight =
                Number.parseFloat(outputTextStyles.lineHeight) ||
                Number.parseFloat(outputTextStyles.fontSize) * 1.2;
            const outputTextRect = toBrowserRect(outputText);

            return {
                latest: {
                    box: toBrowserRect(latestBox),
                    outputCell: toBrowserRect(outputCell),
                    outputCellHorizontalOverflow:
                        outputCell.scrollWidth - outputCell.clientWidth,
                    outputText: outputTextRect,
                    outputTextLength: outputText.textContent?.length ?? 0,
                    outputTextLineCount: Math.max(
                        1,
                        Math.round(outputTextRect.height / lineHeight),
                    ),
                    scroller: toBrowserRect(scroller),
                    table: toBrowserRect(table),
                    tableHorizontalOverflow:
                        scroller.scrollWidth - scroller.clientWidth,
                },
                search: {
                    fieldCount: fields.length,
                    fieldWidthRange: {
                        max: Math.max(...fieldWidths),
                        min: Math.min(...fieldWidths),
                    },
                    firstRowFieldCount,
                    grid: toBrowserRect(searchGrid),
                    plusButtonSizes: fields.map((field) => ({
                        height: field.button.height,
                        iconHeight: field.icon?.height ?? null,
                        iconWidth: field.icon?.width ?? null,
                        key: field.key,
                        width: field.button.width,
                    })),
                    rowCount: rowTops.length,
                    rows: rowTops,
                },
                viewport: {
                    height: window.innerHeight,
                    width: window.innerWidth,
                },
            };
        },
        { pipelineName },
    );
}

test("reproduces real-data long path layout regression on latest results", async ({
    page,
}) => {
    const screenshotPath = path.join(
        evidenceDir,
        "real-data-layout-repro-current.png",
    );
    const evidencePath = path.join(
        evidenceDir,
        "real-data-layout-repro-current.json",
    );

    mkdirSync(evidenceDir, { recursive: true });
    await page.setViewportSize({ width: 1000, height: 900 });
    await page.goto("/");

    await expect(page.getByText("Latest result sets")).toBeVisible();
    await expect(
        page
            .locator('tbody tr[data-result-row="true"]')
            .filter({ hasText: pipelineName })
            .first(),
    ).toBeVisible();

    const metric = await collectLayoutMetric(page);

    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: screenshotPath,
    });
    writeFileSync(
        evidencePath,
        `${JSON.stringify(
            {
                ...metric,
                expected: {
                    latestResultSetsHorizontalOverflowMaxPx: 1,
                    outputDirectoryRenderedWidthMaxPx:
                        metric.latest.scroller.width,
                    permanentFieldsPerRow: 5,
                    permanentFieldRows: 1,
                    wrappedOutputDirectoryLineCountMin: 2,
                },
                longOutputDirectory,
                screenshotPath,
            },
            null,
            2,
        )}\n`,
    );

    expect.soft(metric.search.fieldCount).toBe(5);
    expect.soft(metric.search.rowCount).toBe(1);
    expect.soft(metric.search.firstRowFieldCount).toBe(5);
    expect.soft(metric.latest.tableHorizontalOverflow).toBeLessThanOrEqual(1);
    expect
        .soft(metric.latest.outputText.width)
        .toBeLessThanOrEqual(metric.latest.scroller.width);
    expect
        .soft(metric.latest.outputCellHorizontalOverflow)
        .toBeLessThanOrEqual(1);
    expect.soft(metric.latest.outputTextLineCount).toBeGreaterThanOrEqual(2);
});
