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
    buttonCenterDeltaFromControl: {
        x: number;
        y: number;
    };
    control: RectMetric;
    field: RectMetric;
    icon: RectMetric;
    iconCenterDeltaFromButton: {
        x: number;
        y: number;
    };
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
        buttonCenterDeltaFromControl: {
            x: number;
            y: number;
        };
        height: number;
        iconCenterDeltaFromButton: {
            x: number;
            y: number;
        };
        iconHeight: number;
        iconWidth: number;
        key: string;
        width: number;
    }>;
    rowCount: number;
    rows: number[];
};

type LatestLayoutMetric = {
    box: RectMetric;
    outputColumnIndex: number;
    outputCell: RectMetric;
    outputCellHorizontalOverflow: number;
    outputText: RectMetric;
    outputTextOverflowRightPx: number;
    outputTextOverlapWithNextCellPx: number;
    outputTextLineCount: number;
    outputTextLength: number;
    rowCells: Array<{
        column: string;
        cell: RectMetric;
        text: RectMetric | null;
        textLength: number;
        textOverflowRightPx: number;
        textOverlapWithNextCellPx: number;
    }>;
    rowLocked: boolean;
    scroller: RectMetric;
    table: RectMetric;
    tableHorizontalOverflow: number;
    visibleHeaders: string[];
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
            const center = (rect: RectMetric) => ({
                x: rect.left + rect.width / 2,
                y: rect.top + rect.height / 2,
            });

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
                const control = input?.parentElement;
                const icon = button?.querySelector("svg");

                if (
                    !input ||
                    !button ||
                    !label ||
                    !(control instanceof HTMLElement) ||
                    !(icon instanceof SVGElement)
                ) {
                    throw new Error(
                        "Missing permanent field measurement target",
                    );
                }

                const buttonRect = toBrowserRect(button);
                const controlRect = toBrowserRect(control);
                const iconRect = toBrowserRect(icon);
                const buttonCenter = center(buttonRect);
                const controlCenter = center(controlRect);
                const iconCenter = center(iconRect);

                return {
                    button: buttonRect,
                    buttonCenterDeltaFromControl: {
                        x: Number(
                            (buttonCenter.x - controlCenter.x).toFixed(3),
                        ),
                        y: Number(
                            (buttonCenter.y - controlCenter.y).toFixed(3),
                        ),
                    },
                    control: controlRect,
                    field: toBrowserRect(form),
                    icon: iconRect,
                    iconCenterDeltaFromButton: {
                        x: Number((iconCenter.x - buttonCenter.x).toFixed(3)),
                        y: Number((iconCenter.y - buttonCenter.y).toFixed(3)),
                    },
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
            const visibleHeaders = headers.map(
                (header) => header.textContent?.trim() ?? "",
            );
            const outputColumnIndex = headers.findIndex((header) =>
                header.textContent?.includes("Output Directory"),
            );

            if (outputColumnIndex < 0) {
                throw new Error("Missing Output Directory column");
            }

            const cells = Array.from(resultRow.querySelectorAll("td"));
            const outputCell = cells[outputColumnIndex];
            const outputText = outputCell?.firstElementChild;

            if (!outputCell || !outputText) {
                throw new Error("Missing Output Directory cell");
            }

            const rowCells = cells.map((cell, index) => {
                const cellRect = toBrowserRect(cell);
                const text = cell.firstElementChild;
                const textRect = text ? toBrowserRect(text) : null;
                const nextCell = cells[index + 1];
                const nextCellRect = nextCell ? toBrowserRect(nextCell) : null;

                return {
                    cell: cellRect,
                    column: visibleHeaders[index] ?? "",
                    text: textRect,
                    textLength: text?.textContent?.length ?? 0,
                    textOverflowRightPx: textRect
                        ? Number(
                              Math.max(
                                  0,
                                  textRect.right - cellRect.right,
                              ).toFixed(3),
                          )
                        : 0,
                    textOverlapWithNextCellPx:
                        textRect && nextCellRect
                            ? Number(
                                  Math.max(
                                      0,
                                      textRect.right - nextCellRect.left,
                                  ).toFixed(3),
                              )
                            : 0,
                };
            });
            const outputCellMetric = rowCells[outputColumnIndex];

            const outputTextStyles = window.getComputedStyle(outputText);
            const lineHeight =
                Number.parseFloat(outputTextStyles.lineHeight) ||
                Number.parseFloat(outputTextStyles.fontSize) * 1.2;
            const outputTextRect = toBrowserRect(outputText);

            return {
                latest: {
                    box: toBrowserRect(latestBox),
                    outputColumnIndex,
                    outputCell: toBrowserRect(outputCell),
                    outputCellHorizontalOverflow:
                        outputCell.scrollWidth - outputCell.clientWidth,
                    outputText: outputTextRect,
                    outputTextOverflowRightPx:
                        outputCellMetric?.textOverflowRightPx ?? 0,
                    outputTextOverlapWithNextCellPx:
                        outputCellMetric?.textOverlapWithNextCellPx ?? 0,
                    outputTextLength: outputText.textContent?.length ?? 0,
                    outputTextLineCount: Math.max(
                        1,
                        Math.round(outputTextRect.height / lineHeight),
                    ),
                    rowCells,
                    rowLocked:
                        resultRow.dataset.resultRowLocked === "true" ||
                        resultRow.getAttribute("aria-disabled") === "true",
                    scroller: toBrowserRect(scroller),
                    table: toBrowserRect(table),
                    tableHorizontalOverflow:
                        scroller.scrollWidth - scroller.clientWidth,
                    visibleHeaders,
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
                        buttonCenterDeltaFromControl:
                            field.buttonCenterDeltaFromControl,
                        height: field.button.height,
                        iconCenterDeltaFromButton:
                            field.iconCenterDeltaFromButton,
                        iconHeight: field.icon.height,
                        iconWidth: field.icon.width,
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

async function showColumn(
    page: Page,
    name: string,
    columnId: string,
): Promise<void> {
    await page
        .getByRole("button", { name: "Toggle column visibility" })
        .click();
    await page.getByRole("menuitemcheckbox", { name }).click();
    await expect(
        page.locator(`button[data-column-sort="${columnId}"]`),
    ).toBeVisible();
}

test("reproduces real-data long path layout regression on latest results", async ({
    context,
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
    await installResultsAuthCookie(context);
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
    for (const plusButton of metric.search.plusButtonSizes) {
        expect
            .soft(Math.abs(plusButton.iconCenterDeltaFromButton.x))
            .toBeLessThanOrEqual(0.25);
        expect
            .soft(Math.abs(plusButton.iconCenterDeltaFromButton.y))
            .toBeLessThanOrEqual(0.25);
        expect
            .soft(Math.abs(plusButton.buttonCenterDeltaFromControl.y))
            .toBeLessThanOrEqual(0.25);
    }
    expect.soft(metric.latest.tableHorizontalOverflow).toBeLessThanOrEqual(1);
    expect
        .soft(metric.latest.outputText.width)
        .toBeLessThanOrEqual(metric.latest.scroller.width);
    expect
        .soft(metric.latest.outputCellHorizontalOverflow)
        .toBeLessThanOrEqual(1);
    expect.soft(metric.latest.outputTextLineCount).toBeGreaterThanOrEqual(2);
});

test("reproduces logged-out long path truncation and column overlap on latest results", async ({
    page,
}) => {
    const beforeScreenshotPath = path.join(
        evidenceDir,
        "latest-result-sets-logged-out-long-path-repro-before-column.png",
    );
    const afterScreenshotPath = path.join(
        evidenceDir,
        "latest-result-sets-logged-out-column-overlap-repro.png",
    );
    const evidencePath = path.join(
        evidenceDir,
        "latest-result-sets-logged-out-column-overlap-repro.json",
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

    const beforeColumnMetric = await collectLayoutMetric(page);
    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: beforeScreenshotPath,
    });

    await showColumn(page, "Operator", "operator");

    const afterColumnMetric = await collectLayoutMetric(page);
    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: afterScreenshotPath,
    });
    writeFileSync(
        evidencePath,
        `${JSON.stringify(
            {
                afterColumnMetric,
                beforeColumnMetric,
                expected: {
                    addedColumnName: "Operator",
                    latestResultSetsHorizontalOverflowMaxPx: 1,
                    outputTextOverlapWithNextCellMaxPx: 1,
                    outputTextOverflowRightMaxPx: 1,
                    wrappedOutputDirectoryLineCountMin: 2,
                },
                longOutputDirectory,
                screenshots: {
                    afterColumn: afterScreenshotPath,
                    beforeColumn: beforeScreenshotPath,
                },
            },
            null,
            2,
        )}\n`,
    );

    expect.soft(beforeColumnMetric.latest.rowLocked).toBe(true);
    expect
        .soft(beforeColumnMetric.latest.tableHorizontalOverflow)
        .toBeLessThanOrEqual(1);
    expect
        .soft(beforeColumnMetric.latest.outputCellHorizontalOverflow)
        .toBeLessThanOrEqual(1);
    expect
        .soft(beforeColumnMetric.latest.outputTextOverflowRightPx)
        .toBeLessThanOrEqual(1);
    expect
        .soft(beforeColumnMetric.latest.outputTextLineCount)
        .toBeGreaterThanOrEqual(2);

    expect(afterColumnMetric.latest.visibleHeaders).toContain("Operator");
    expect
        .soft(afterColumnMetric.latest.tableHorizontalOverflow)
        .toBeLessThanOrEqual(1);
    expect
        .soft(afterColumnMetric.latest.outputCellHorizontalOverflow)
        .toBeLessThanOrEqual(1);
    expect
        .soft(afterColumnMetric.latest.outputTextOverflowRightPx)
        .toBeLessThanOrEqual(1);
    expect
        .soft(afterColumnMetric.latest.outputTextOverlapWithNextCellPx)
        .toBeLessThanOrEqual(1);
    expect
        .soft(afterColumnMetric.latest.outputTextLineCount)
        .toBeGreaterThanOrEqual(2);
});
