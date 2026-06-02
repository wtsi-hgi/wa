import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

type RectMetric = {
    bottom: number;
    centerX: number;
    centerY: number;
    height: number;
    left: number;
    right: number;
    top: number;
    width: number;
};

type ClearanceMetric = {
    bottom: number;
    bottomRightDelta: number;
    left: number;
    maxTopBottomRightSpread: number;
    right: number;
    top: number;
    topBottomDelta: number;
    topRightDelta: number;
};

type TriggerMetric = {
    bottomClearanceWithinPanel: number;
    label: string;
    rect: RectMetric;
    topClearanceWithinPanel: number;
};

type ToolsPanelExternalSpacingMetric = {
    caseKey: string;
    context: string;
    directoryKind: "root" | "short-subdirectory";
    directoryPath: string;
    externalClearance: {
        actions: ClearanceMetric | null;
        heading: ClearanceMetric;
        nameArea: ClearanceMetric;
        row: ClearanceMetric;
    };
    internalPadding: {
        bottom: number;
        left: number;
        right: number;
        top: number;
    };
    observed: {
        headingRightDiffersFromBalancedTopBottom: boolean;
        headingTopBottomApproximatelyEqual: boolean;
        rowRightDiffersFromBalancedTopBottom: boolean;
        rowTopIsMuchLessThanBottom: boolean;
    };
    previewMode: string | null;
    rects: {
        actions: RectMetric | null;
        directoryButton: RectMetric;
        directoryName: RectMetric | null;
        heading: RectMetric;
        nameArea: RectMetric;
        panel: RectMetric;
        row: RectMetric;
    };
    styles: {
        heading: {
            alignItems: string;
            columnGap: string;
            display: string;
            gridTemplateColumns: string;
            rowGap: string;
        };
        nameArea: {
            alignSelf: string;
            paddingBottom: number;
            paddingLeft: number;
            paddingRight: number;
            paddingTop: number;
        };
        panel: {
            alignItems: string;
            columnGap: string;
            display: string;
            flexWrap: string;
            rowGap: string;
        };
    };
    triggerClearance: {
        bottom: number;
        top: number;
        topBottomDelta: number;
    };
    triggers: TriggerMetric[];
    url: string;
    viewport: {
        height: number;
        width: number;
    };
    visibleDirectoryLabel: string | null;
};

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const evidencePath = path.join(
    evidenceDir,
    "file-browser-tools-panel-external-spacing-repro-current.json",
);
const rnaseqPipelineName = "nf-core/rnaseq";
const fixturesRoot = path.join(
    repoRoot,
    ".docs",
    "results-web",
    "fixtures",
    "files",
);
const rnaseqRootPath = path.join(fixturesRoot, "rnaseq");
const rnaseqQcPath = path.join(rnaseqRootPath, "qc");
const measuredCases = [
    {
        context: "result-detail",
        directoryKind: "root" as const,
        directoryPath: rnaseqRootPath,
        key: "result-detail-root",
    },
    {
        context: "result-detail",
        directoryKind: "short-subdirectory" as const,
        directoryPath: rnaseqQcPath,
        key: "result-detail-short-subdirectory",
    },
    {
        context: "combined-search",
        directoryKind: "root" as const,
        directoryPath: rnaseqRootPath,
        key: "combined-search-root",
    },
    {
        context: "combined-search",
        directoryKind: "short-subdirectory" as const,
        directoryPath: rnaseqQcPath,
        key: "combined-search-short-subdirectory",
    },
];

function resultRows(page: Page): Locator {
    return page.locator('tbody tr[data-result-row="true"]');
}

function controlsFor(page: Page, directoryPath: string): Locator {
    return page
        .locator(`[data-file-browser-folder-controls="${directoryPath}"]`)
        .first();
}

async function openRnaseqResultDetail(page: Page): Promise<void> {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    await expect(page.getByText("Latest result sets")).toBeVisible();
    await expect.poll(async () => resultRows(page).count()).toBeGreaterThan(0);

    const resultLink = page
        .getByRole("link", { name: rnaseqPipelineName })
        .first();
    const href = await resultLink.getAttribute("href");
    const detailUrl = new URL(href ?? "/results/", page.url()).toString();

    await page.goto(detailUrl);
    await expect(page).toHaveURL(detailUrl);
    await expect(
        page.getByRole("heading", { level: 1, name: rnaseqPipelineName }),
    ).toBeVisible({ timeout: 30000 });
    await expect(page.locator('[data-file-browser="true"]')).toBeVisible();
}

async function openRnaseqCombinedSearch(page: Page): Promise<void> {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(
        `/?pipeline_name=${encodeURIComponent(rnaseqPipelineName)}`,
    );
    await expect(page.locator('[data-search-builder="true"]')).toBeVisible();
    await expect(
        page.locator('[data-search-combined-file-browser="true"]'),
    ).toHaveAttribute("data-search-file-mode", "combined");
    await expect(page.locator('[data-file-browser="true"]')).toHaveCount(1);
}

async function selectDirectory(page: Page, directoryPath: string) {
    await expect
        .poll(async () => page.locator("[data-directory-path]").count())
        .toBeGreaterThan(0);

    for (let attempt = 0; attempt < 12; attempt += 1) {
        const directoryButton = page
            .locator(`[data-directory-path="${directoryPath}"]`)
            .first();

        if ((await directoryButton.count()) > 0) {
            await directoryButton.scrollIntoViewIfNeeded();
            await expect(directoryButton).toBeVisible();
            await directoryButton.click();
            return;
        }

        const visiblePaths = await page
            .locator("[data-directory-path]")
            .evaluateAll((elements) =>
                elements
                    .map((element) =>
                        element.getAttribute("data-directory-path"),
                    )
                    .filter((value): value is string => Boolean(value)),
            );
        const nextPath = visiblePaths
            .filter(
                (candidate) =>
                    directoryPath.startsWith(`${candidate}${path.sep}`) ||
                    directoryPath === candidate,
            )
            .sort((left, right) => right.length - left.length)[0];

        if (!nextPath || nextPath === directoryPath) {
            break;
        }

        const nextDirectoryButton = page
            .locator(`[data-directory-path="${nextPath}"]`)
            .first();

        await nextDirectoryButton.scrollIntoViewIfNeeded();
        await expect(nextDirectoryButton).toBeVisible();
        await nextDirectoryButton.click();
        await expect(nextDirectoryButton).toHaveAttribute(
            "data-directory-expanded",
            "true",
        );
    }

    const directoryButton = page
        .locator(`[data-directory-path="${directoryPath}"]`)
        .first();

    await directoryButton.scrollIntoViewIfNeeded();
    await expect(directoryButton).toBeVisible();
    await directoryButton.click();
}

async function ensureControlsVisible(
    page: Page,
    directoryPath: string,
): Promise<Locator> {
    const controls = controlsFor(page, directoryPath);

    for (let attempt = 0; attempt < 3; attempt += 1) {
        if (
            (await controls.count()) > 0 &&
            (await controls.isVisible().catch(() => false))
        ) {
            break;
        }

        const directoryButton = page
            .locator(`[data-directory-path="${directoryPath}"]`)
            .first();

        await directoryButton.scrollIntoViewIfNeeded();
        await expect(directoryButton).toBeVisible();
        await directoryButton.click();
        await page.waitForTimeout(100);
    }

    await expect(controls).toBeVisible();
    await expect(
        controls.locator('[data-file-browser-control-trigger="preview-modes"]'),
    ).toBeVisible();
    await expect(
        controls.locator('[data-file-browser-control-trigger="file-types"]'),
    ).toBeVisible();

    return controls;
}

async function prepareDirectoryForMeasurement(
    page: Page,
    directoryPath: string,
) {
    if (directoryPath !== rnaseqRootPath) {
        await selectDirectory(page, directoryPath);
    }

    await ensureControlsVisible(page, directoryPath);
}

async function captureScreenshots(
    page: Page,
    caseKey: string,
    controls: Locator,
): Promise<{ main: string; panel: string }> {
    const main = path.join(
        evidenceDir,
        `file-browser-tools-panel-external-spacing-repro-${caseKey}.png`,
    );
    const panel = path.join(
        evidenceDir,
        `file-browser-tools-panel-external-spacing-repro-${caseKey}-panel.png`,
    );

    await page
        .locator("main")
        .screenshot({ animations: "disabled", path: main });
    await controls.screenshot({ animations: "disabled", path: panel });

    return { main, panel };
}

async function collectToolsPanelExternalSpacingMetric(
    page: Page,
    context: string,
    directoryKind: "root" | "short-subdirectory",
    directoryPath: string,
    caseKey: string,
): Promise<ToolsPanelExternalSpacingMetric> {
    return page.evaluate(
        ({ caseKey, context, directoryKind, directoryPath }) => {
            const round = (value: number) =>
                Math.round((value + Number.EPSILON) * 100) / 100;
            const toRect = (element: Element): RectMetric => {
                const rect = element.getBoundingClientRect();

                return {
                    bottom: round(rect.bottom),
                    centerX: round(rect.left + rect.width / 2),
                    centerY: round(rect.top + rect.height / 2),
                    height: round(rect.height),
                    left: round(rect.left),
                    right: round(rect.right),
                    top: round(rect.top),
                    width: round(rect.width),
                };
            };
            const clearanceWithin = (
                parent: RectMetric,
                child: RectMetric,
            ): ClearanceMetric => {
                const top = round(child.top - parent.top);
                const bottom = round(parent.bottom - child.bottom);
                const right = round(parent.right - child.right);
                const left = round(child.left - parent.left);
                const topBottomRight = [top, bottom, right];

                return {
                    bottom,
                    bottomRightDelta: round(bottom - right),
                    left,
                    maxTopBottomRightSpread: round(
                        Math.max(...topBottomRight) -
                            Math.min(...topBottomRight),
                    ),
                    right,
                    top,
                    topBottomDelta: round(top - bottom),
                    topRightDelta: round(top - right),
                };
            };
            const selector = (attribute: string) =>
                `[${attribute}="${CSS.escape(directoryPath)}"]`;
            const panel = document.querySelector(
                selector("data-file-browser-folder-controls"),
            );
            const heading = document.querySelector(
                selector("data-directory-heading-with-controls"),
            );
            const nameArea = document.querySelector(
                selector("data-file-browser-name-area-controls"),
            );
            const actions = document.querySelector(
                selector("data-file-browser-name-area-actions"),
            );
            const row = document.querySelector(selector("data-directory-row"));
            const directoryButton = document.querySelector(
                `button${selector("data-directory-path")}`,
            );

            if (
                !(panel instanceof HTMLElement) ||
                !(heading instanceof HTMLElement) ||
                !(nameArea instanceof HTMLElement) ||
                !(row instanceof HTMLElement) ||
                !(directoryButton instanceof HTMLElement)
            ) {
                throw new Error(
                    `Missing file browser tools panel external spacing target for ${directoryPath}`,
                );
            }

            const directoryName = Array.from(
                directoryButton.querySelectorAll<HTMLElement>("span[title]"),
            ).find(
                (candidate) =>
                    candidate.getAttribute("title") === directoryPath,
            );
            const panelRect = toRect(panel);
            const headingRect = toRect(heading);
            const nameAreaRect = toRect(nameArea);
            const actionsRect =
                actions instanceof HTMLElement ? toRect(actions) : null;
            const rowRect = toRect(row);
            const panelStyles = window.getComputedStyle(panel);
            const headingStyles = window.getComputedStyle(heading);
            const nameAreaStyles = window.getComputedStyle(nameArea);
            const triggers = Array.from(
                panel.querySelectorAll<HTMLElement>(
                    "[data-file-browser-control-trigger]",
                ),
            ).map((trigger) => {
                const rect = toRect(trigger);

                return {
                    bottomClearanceWithinPanel: round(
                        panelRect.bottom - rect.bottom,
                    ),
                    label:
                        trigger.getAttribute("aria-label") ??
                        trigger.textContent?.trim() ??
                        "",
                    rect,
                    topClearanceWithinPanel: round(rect.top - panelRect.top),
                };
            });
            const topMostTrigger = Math.min(
                ...triggers.map((trigger) => trigger.rect.top),
            );
            const bottomMostTrigger = Math.max(
                ...triggers.map((trigger) => trigger.rect.bottom),
            );
            const headingClearance = clearanceWithin(headingRect, panelRect);
            const rowClearance = clearanceWithin(rowRect, panelRect);

            return {
                caseKey,
                context,
                directoryKind,
                directoryPath,
                externalClearance: {
                    actions: actionsRect
                        ? clearanceWithin(actionsRect, panelRect)
                        : null,
                    heading: headingClearance,
                    nameArea: clearanceWithin(nameAreaRect, panelRect),
                    row: rowClearance,
                },
                internalPadding: {
                    bottom: round(Number.parseFloat(panelStyles.paddingBottom)),
                    left: round(Number.parseFloat(panelStyles.paddingLeft)),
                    right: round(Number.parseFloat(panelStyles.paddingRight)),
                    top: round(Number.parseFloat(panelStyles.paddingTop)),
                },
                observed: {
                    headingRightDiffersFromBalancedTopBottom:
                        Math.abs(headingClearance.topBottomDelta) <= 1 &&
                        Math.abs(
                            headingClearance.top - headingClearance.right,
                        ) > 1,
                    headingTopBottomApproximatelyEqual:
                        Math.abs(headingClearance.topBottomDelta) <= 1,
                    rowRightDiffersFromBalancedTopBottom:
                        Math.abs(rowClearance.topBottomDelta) <= 1 &&
                        Math.abs(rowClearance.top - rowClearance.right) > 1,
                    rowTopIsMuchLessThanBottom:
                        rowClearance.bottom - rowClearance.top > 4,
                },
                previewMode:
                    panel
                        .closest('[data-file-browser="true"]')
                        ?.querySelector<HTMLElement>("[data-preview-mode]")
                        ?.dataset.previewMode ?? null,
                rects: {
                    actions: actionsRect,
                    directoryButton: toRect(directoryButton),
                    directoryName: directoryName ? toRect(directoryName) : null,
                    heading: headingRect,
                    nameArea: nameAreaRect,
                    panel: panelRect,
                    row: rowRect,
                },
                styles: {
                    heading: {
                        alignItems: headingStyles.alignItems,
                        columnGap: headingStyles.columnGap,
                        display: headingStyles.display,
                        gridTemplateColumns: headingStyles.gridTemplateColumns,
                        rowGap: headingStyles.rowGap,
                    },
                    nameArea: {
                        alignSelf: nameAreaStyles.alignSelf,
                        paddingBottom: round(
                            Number.parseFloat(nameAreaStyles.paddingBottom),
                        ),
                        paddingLeft: round(
                            Number.parseFloat(nameAreaStyles.paddingLeft),
                        ),
                        paddingRight: round(
                            Number.parseFloat(nameAreaStyles.paddingRight),
                        ),
                        paddingTop: round(
                            Number.parseFloat(nameAreaStyles.paddingTop),
                        ),
                    },
                    panel: {
                        alignItems: panelStyles.alignItems,
                        columnGap: panelStyles.columnGap,
                        display: panelStyles.display,
                        flexWrap: panelStyles.flexWrap,
                        rowGap: panelStyles.rowGap,
                    },
                },
                triggerClearance: {
                    bottom: round(panelRect.bottom - bottomMostTrigger),
                    top: round(topMostTrigger - panelRect.top),
                    topBottomDelta: round(
                        topMostTrigger -
                            panelRect.top -
                            (panelRect.bottom - bottomMostTrigger),
                    ),
                },
                triggers,
                url: window.location.href,
                viewport: {
                    height: window.innerHeight,
                    width: window.innerWidth,
                },
                visibleDirectoryLabel:
                    directoryName?.textContent?.trim() ?? null,
            };
        },
        { caseKey, context, directoryKind, directoryPath },
    );
}

function expectCompactInternalPanelSpacing(
    metric: ToolsPanelExternalSpacingMetric,
) {
    expect
        .soft(
            metric.internalPadding.top,
            `${metric.caseKey} panel top padding should preserve compact 6px density`,
        )
        .toBeLessThanOrEqual(6);
    expect
        .soft(
            metric.internalPadding.bottom,
            `${metric.caseKey} panel bottom padding should preserve compact 6px density`,
        )
        .toBeLessThanOrEqual(6);
    expect
        .soft(
            Math.abs(
                metric.internalPadding.top - metric.internalPadding.bottom,
            ),
            `${metric.caseKey} panel internal top/bottom padding should stay even`,
        )
        .toBeLessThanOrEqual(1);
}

function expectExternalClearanceToMatch(
    metric: ToolsPanelExternalSpacingMetric,
) {
    const clearance = metric.externalClearance.heading;

    expect
        .soft(
            Math.abs(clearance.topBottomDelta),
            `${metric.caseKey} external top and bottom clearance should match`,
        )
        .toBeLessThanOrEqual(1);
    expect
        .soft(
            Math.abs(clearance.topRightDelta),
            `${metric.caseKey} external top and right clearance should match`,
        )
        .toBeLessThanOrEqual(1);
    expect
        .soft(
            Math.abs(clearance.bottomRightDelta),
            `${metric.caseKey} external bottom and right clearance should match`,
        )
        .toBeLessThanOrEqual(1);
}

test("reproduces inconsistent external spacing around File Browser tools panels", async ({
    page,
}) => {
    test.setTimeout(120_000);
    mkdirSync(evidenceDir, { recursive: true });

    const metrics: ToolsPanelExternalSpacingMetric[] = [];
    const screenshots: Record<string, { main: string; panel: string }> = {};

    await openRnaseqResultDetail(page);

    for (const measuredCase of measuredCases.filter(
        (candidate) => candidate.context === "result-detail",
    )) {
        await prepareDirectoryForMeasurement(page, measuredCase.directoryPath);

        const controls = controlsFor(page, measuredCase.directoryPath);
        screenshots[measuredCase.key] = await captureScreenshots(
            page,
            measuredCase.key,
            controls,
        );
        metrics.push(
            await collectToolsPanelExternalSpacingMetric(
                page,
                measuredCase.context,
                measuredCase.directoryKind,
                measuredCase.directoryPath,
                measuredCase.key,
            ),
        );
    }

    await openRnaseqCombinedSearch(page);

    for (const measuredCase of measuredCases.filter(
        (candidate) => candidate.context === "combined-search",
    )) {
        await prepareDirectoryForMeasurement(page, measuredCase.directoryPath);

        const controls = controlsFor(page, measuredCase.directoryPath);
        screenshots[measuredCase.key] = await captureScreenshots(
            page,
            measuredCase.key,
            controls,
        );
        metrics.push(
            await collectToolsPanelExternalSpacingMetric(
                page,
                measuredCase.context,
                measuredCase.directoryKind,
                measuredCase.directoryPath,
                measuredCase.key,
            ),
        );
    }

    writeFileSync(
        evidencePath,
        `${JSON.stringify(
            {
                expected: {
                    externalClearanceTolerancePx: 1,
                    internalPanelPaddingMaxPx: 6,
                    note: "The assertions require the external top, bottom, and right clearances around the tools panel to match in the user-visible header/control band for root and short-subdirectory rows while preserving compact 6px internal panel padding.",
                },
                measuredCases: metrics,
                screenshots,
                viewsCovered: [
                    "result detail File Browser, default single preview mode, short visible subdirectory label",
                    "result detail File Browser, default single preview mode, root directory row",
                    "combined search File Browser, default single preview mode, short visible subdirectory label",
                    "combined search File Browser, default single preview mode, root directory row",
                ],
            },
            null,
            2,
        )}\n`,
    );

    for (const metric of metrics) {
        expect(metric.triggers.map((trigger) => trigger.label)).toEqual([
            "Preview modes",
            "File types",
        ]);
        expectCompactInternalPanelSpacing(metric);
        expectExternalClearanceToMatch(metric);
    }
});
