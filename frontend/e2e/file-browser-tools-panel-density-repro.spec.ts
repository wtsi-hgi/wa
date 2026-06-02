import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

type RectMetric = {
    bottom: number;
    centerY: number;
    height: number;
    left: number;
    right: number;
    top: number;
    width: number;
};

type TriggerMetric = {
    bottomClearanceWithinPanel: number;
    label: string;
    rect: RectMetric;
    topClearanceWithinPanel: number;
};

type ToolsPanelDensityMetric = {
    classes: {
        heading: string;
        nameArea: string;
        panel: string;
        triggers: string[];
    };
    directoryPath: string;
    heading: RectMetric;
    nameArea: RectMetric;
    padding: {
        bottom: number;
        left: number;
        right: number;
        top: number;
    };
    panel: RectMetric;
    row: RectMetric;
    spacing: {
        headingBottomClearance: number;
        headingTopClearance: number;
        nameAreaBottomClearance: number;
        nameAreaTopClearance: number;
        rowBottomClearance: number;
        rowTopClearance: number;
        triggerBottomClearance: number;
        triggerTopBottomDelta: number;
        triggerTopClearance: number;
    };
    triggers: TriggerMetric[];
    url: string;
    viewport: {
        height: number;
        width: number;
    };
};

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const evidencePath = path.join(
    evidenceDir,
    "file-browser-tools-panel-density-repro-current.json",
);
const screenshots = {
    detail: path.join(
        evidenceDir,
        "file-browser-tools-panel-density-repro-detail.png",
    ),
    panel: path.join(
        evidenceDir,
        "file-browser-tools-panel-density-repro-panel.png",
    ),
};
const referenceClasses = {
    controlTriggerClass:
        "inline-flex min-w-0 cursor-pointer list-none items-center gap-1.5 rounded-md border border-border/80 bg-background px-2 py-1 text-foreground shadow-sm marker:hidden hover:bg-muted/70",
    folderControlsClass:
        "file-browser-control-surface inline-nameplate-controls flex w-fit max-w-full min-w-0 flex-wrap items-center justify-start gap-1.5 rounded-md border border-border bg-[color-mix(in_oklab,var(--card)_72%,var(--foreground)_28%)] p-2 text-sm shadow-sm",
    note: "git show HEAD~2:frontend/components/file-browser.tsx shows the same folder/trigger classes as current HEAD; HEAD~2 is a symmetry reference, while the compact threshold captures the new density request.",
    referenceCommit: "637cb85 Show full directory path on hover",
};
const rnaseqPipelineName = "nf-core/rnaseq";
const rnaseqGalleryPath = path.join(
    repoRoot,
    ".docs",
    "results-web",
    "fixtures",
    "files",
    "rnaseq",
    "qc",
    "images",
    "gallery",
);

function resultRows(page: Page): Locator {
    return page.locator('tbody tr[data-result-row="true"]');
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

async function collectToolsPanelDensityMetric(
    page: Page,
    directoryPath: string,
): Promise<ToolsPanelDensityMetric> {
    return page.evaluate((evaluatedDirectoryPath) => {
        const toRect = (element: Element): RectMetric => {
            const rect = element.getBoundingClientRect();

            return {
                bottom: rect.bottom,
                centerY: rect.top + rect.height / 2,
                height: rect.height,
                left: rect.left,
                right: rect.right,
                top: rect.top,
                width: rect.width,
            };
        };
        const escapedPath = CSS.escape(evaluatedDirectoryPath);
        const panel = document.querySelector(
            `[data-file-browser-folder-controls="${escapedPath}"]`,
        );
        const heading = document.querySelector(
            `[data-directory-heading-with-controls="${escapedPath}"]`,
        );
        const nameArea = document.querySelector(
            `[data-file-browser-name-area-controls="${escapedPath}"]`,
        );
        const row = document.querySelector(
            `[data-directory-row="${escapedPath}"]`,
        );

        if (
            !(panel instanceof HTMLElement) ||
            !(heading instanceof HTMLElement) ||
            !(nameArea instanceof HTMLElement) ||
            !(row instanceof HTMLElement)
        ) {
            throw new Error("Missing file browser tools panel density target");
        }

        const panelRect = toRect(panel);
        const headingRect = toRect(heading);
        const nameAreaRect = toRect(nameArea);
        const rowRect = toRect(row);
        const styles = window.getComputedStyle(panel);
        const triggers = Array.from(
            panel.querySelectorAll<HTMLElement>(
                "[data-file-browser-control-trigger]",
            ),
        ).map((trigger) => {
            const rect = toRect(trigger);

            return {
                bottomClearanceWithinPanel: panelRect.bottom - rect.bottom,
                label:
                    trigger.getAttribute("aria-label") ??
                    trigger.textContent?.trim() ??
                    "",
                rect,
                topClearanceWithinPanel: rect.top - panelRect.top,
            };
        });
        const topMostTrigger = Math.min(
            ...triggers.map((trigger) => trigger.rect.top),
        );
        const bottomMostTrigger = Math.max(
            ...triggers.map((trigger) => trigger.rect.bottom),
        );
        const triggerTopClearance = topMostTrigger - panelRect.top;
        const triggerBottomClearance = panelRect.bottom - bottomMostTrigger;

        return {
            classes: {
                heading: heading.className,
                nameArea: nameArea.className,
                panel: panel.className,
                triggers: Array.from(
                    panel.querySelectorAll<HTMLElement>(
                        "[data-file-browser-control-trigger]",
                    ),
                ).map((trigger) => trigger.className),
            },
            directoryPath: evaluatedDirectoryPath,
            heading: headingRect,
            nameArea: nameAreaRect,
            padding: {
                bottom: Number.parseFloat(styles.paddingBottom),
                left: Number.parseFloat(styles.paddingLeft),
                right: Number.parseFloat(styles.paddingRight),
                top: Number.parseFloat(styles.paddingTop),
            },
            panel: panelRect,
            row: rowRect,
            spacing: {
                headingBottomClearance: headingRect.bottom - panelRect.bottom,
                headingTopClearance: panelRect.top - headingRect.top,
                nameAreaBottomClearance: nameAreaRect.bottom - panelRect.bottom,
                nameAreaTopClearance: panelRect.top - nameAreaRect.top,
                rowBottomClearance: rowRect.bottom - panelRect.bottom,
                rowTopClearance: panelRect.top - rowRect.top,
                triggerBottomClearance,
                triggerTopBottomDelta:
                    triggerTopClearance - triggerBottomClearance,
                triggerTopClearance,
            },
            triggers,
            url: window.location.href,
            viewport: {
                height: window.innerHeight,
                width: window.innerWidth,
            },
        };
    }, directoryPath);
}

test("reproduces the file browser tools panel excessive density while preserving vertical symmetry", async ({
    page,
}) => {
    mkdirSync(evidenceDir, { recursive: true });

    await openRnaseqResultDetail(page);
    await selectDirectory(page, rnaseqGalleryPath);

    const toolsPanel = page.locator(
        `[data-file-browser-folder-controls="${rnaseqGalleryPath}"]`,
    );

    await expect(toolsPanel).toBeVisible();
    await page
        .locator("main")
        .screenshot({ animations: "disabled", path: screenshots.detail });
    await toolsPanel.screenshot({
        animations: "disabled",
        path: screenshots.panel,
    });
    const metric = await collectToolsPanelDensityMetric(
        page,
        rnaseqGalleryPath,
    );

    writeFileSync(
        evidencePath,
        `${JSON.stringify(
            {
                expected: {
                    maxCompactPanelPaddingPx: 6,
                    maxCompactPanelHeightPx: 56,
                    maxCompactTriggerClearancePx: 8,
                    maxVerticalSymmetryDeltaPx: 1,
                },
                metric,
                reference: referenceClasses,
                screenshots,
            },
            null,
            2,
        )}\n`,
    );

    expect(metric.triggers).toHaveLength(2);
    expect
        .soft(
            Math.abs(metric.padding.top - metric.padding.bottom),
            "panel top/bottom padding should remain visually even",
        )
        .toBeLessThanOrEqual(1);
    expect
        .soft(
            Math.abs(metric.spacing.triggerTopBottomDelta),
            "trigger top/bottom clearance should remain visually even",
        )
        .toBeLessThanOrEqual(1);
    expect
        .soft(
            metric.padding.top,
            "panel top padding should be compact, not the current p-2 density",
        )
        .toBeLessThanOrEqual(6);
    expect
        .soft(
            metric.spacing.triggerTopClearance,
            "trigger top clearance should be compact",
        )
        .toBeLessThanOrEqual(8);
    expect
        .soft(
            metric.spacing.triggerBottomClearance,
            "trigger bottom clearance should be compact",
        )
        .toBeLessThanOrEqual(8);
    expect
        .soft(metric.panel.height, "panel height should be compact")
        .toBeLessThanOrEqual(56);
});
