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

type TitleTextSpacingTarget = {
    actionText?: string;
    boxSelector?: string;
    contentSelector: string;
    key: string;
    title: string;
    titleSectionSelector: string;
};

type ActionButtonMetric = {
    centerDeltaFromTitleText: number;
    rect: RectMetric;
};

type TitleTextSpacingMetric = {
    actionButton: ActionButtonMetric | null;
    box: RectMetric;
    content: RectMetric;
    distances: {
        actionBottomClearanceToNextVisible: number | null;
        actionBottomInsetWithinTitleSection: number | null;
        actionTopClearanceFromBoxTop: number | null;
        actionTopInsetWithinTitleSection: number | null;
        iconCenterDeltaFromTitleText: number;
        titleRowTopFromBoxTop: number;
        titleTextBottomToNextVisible: number;
        titleTextTopFromBoxTop: number;
    };
    flags: {
        actionTouchesOrNearlyTouchesLowerDivider: boolean | null;
    };
    icon: RectMetric;
    key: string;
    title: string;
    titleRow: RectMetric;
    titleSection: RectMetric;
    titleText: RectMetric;
};

type TitleTextSpacingSnapshot = {
    boxes: Record<string, TitleTextSpacingMetric>;
    mode: string | null;
    url: string;
    viewport: {
        height: number;
        width: number;
    };
};

type ToolsPanelMetric = {
    buttons: {
        bottomClearanceWithinPanel: number;
        label: string;
        rect: RectMetric;
        topClearanceWithinPanel: number;
    }[];
    heading: RectMetric;
    padding: {
        bottom: number;
        left: number;
        right: number;
        top: number;
    };
    panel: RectMetric;
    row: RectMetric;
    spacing: {
        bottomPaddingDelta: number;
        buttonBottomClearance: number;
        buttonTopClearance: number;
        headingBottomMargin: number;
        headingTopMargin: number;
        rowBottomMargin: number;
        rowTopMargin: number;
    };
};

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const titleEvidencePath = path.join(
    evidenceDir,
    "box-title-text-spacing-repro.json",
);
const toolsEvidencePath = path.join(
    evidenceDir,
    "file-browser-tools-panel-spacing-repro.json",
);
const screenshots = {
    combinedFiles: path.join(
        evidenceDir,
        "box-title-text-spacing-repro-combined-files.png",
    ),
    homeLatest: path.join(
        evidenceDir,
        "box-title-text-spacing-repro-home-latest-result-sets.png",
    ),
    resultSets: path.join(
        evidenceDir,
        "box-title-text-spacing-repro-result-sets.png",
    ),
    toolsDetail: path.join(
        evidenceDir,
        "file-browser-tools-panel-spacing-repro-detail.png",
    ),
    toolsPanel: path.join(
        evidenceDir,
        "file-browser-tools-panel-spacing-repro-panel.png",
    ),
};
const rnaseqPipelineName = "nf-core/rnaseq";
const fixturesRoot = path.join(
    repoRoot,
    ".docs",
    "results-web",
    "fixtures",
    "files",
);
const rnaseqGalleryPath = path.join(
    fixturesRoot,
    "rnaseq",
    "qc",
    "images",
    "gallery",
);

function resultRows(page: Page): Locator {
    return page.locator('tbody tr[data-result-row="true"]');
}

async function collectTitleTextSpacingSnapshot(
    page: Page,
    targets: TitleTextSpacingTarget[],
): Promise<TitleTextSpacingSnapshot> {
    return page.evaluate((evaluatedTargets) => {
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

        const boxes: Record<string, TitleTextSpacingMetric> = {};

        for (const target of evaluatedTargets) {
            const titleSection = document.querySelector(
                target.titleSectionSelector,
            );

            if (!(titleSection instanceof HTMLElement)) {
                throw new Error(`Missing title section for ${target.title}`);
            }

            const box = target.boxSelector
                ? document.querySelector(target.boxSelector)
                : titleSection.parentElement;
            const content = document.querySelector(target.contentSelector);

            if (
                !(box instanceof HTMLElement) ||
                !(content instanceof HTMLElement)
            ) {
                throw new Error(
                    `Missing title box/content for ${target.title}`,
                );
            }

            const titleText = Array.from(
                titleSection.querySelectorAll("p"),
            ).find(
                (candidate): candidate is HTMLParagraphElement =>
                    candidate instanceof HTMLParagraphElement &&
                    candidate.textContent?.trim() === target.title,
            );

            if (!titleText) {
                throw new Error(`Missing title text for ${target.title}`);
            }

            const titleRow = titleText.parentElement;

            if (!(titleRow instanceof HTMLElement)) {
                throw new Error(`Missing title row for ${target.title}`);
            }

            const icon = titleRow.querySelector("svg");

            if (!(icon instanceof SVGElement)) {
                throw new Error(`Missing title icon for ${target.title}`);
            }

            const actionButton = target.actionText
                ? (Array.from(titleSection.querySelectorAll("button")).find(
                      (button) =>
                          button.textContent
                              ?.trim()
                              .includes(target.actionText ?? "") ||
                          button
                              .getAttribute("aria-label")
                              ?.includes(target.actionText ?? ""),
                  ) ?? null)
                : null;
            const boxRect = toRect(box);
            const contentRect = toRect(content);
            const iconRect = toRect(icon);
            const sectionRect = toRect(titleSection);
            const titleRect = toRect(titleText);
            const titleRowRect = toRect(titleRow);
            const actionRect =
                actionButton instanceof HTMLElement
                    ? toRect(actionButton)
                    : null;
            const actionBottomClearanceToNextVisible = actionRect
                ? contentRect.top - actionRect.bottom
                : null;

            boxes[target.key] = {
                actionButton: actionRect
                    ? {
                          centerDeltaFromTitleText:
                              actionRect.centerY - titleRect.centerY,
                          rect: actionRect,
                      }
                    : null,
                box: boxRect,
                content: contentRect,
                distances: {
                    actionBottomClearanceToNextVisible,
                    actionBottomInsetWithinTitleSection: actionRect
                        ? sectionRect.bottom - actionRect.bottom
                        : null,
                    actionTopClearanceFromBoxTop: actionRect
                        ? actionRect.top - boxRect.top
                        : null,
                    actionTopInsetWithinTitleSection: actionRect
                        ? actionRect.top - sectionRect.top
                        : null,
                    iconCenterDeltaFromTitleText:
                        iconRect.centerY - titleRect.centerY,
                    titleRowTopFromBoxTop: titleRowRect.top - boxRect.top,
                    titleTextBottomToNextVisible:
                        contentRect.top - titleRect.bottom,
                    titleTextTopFromBoxTop: titleRect.top - boxRect.top,
                },
                flags: {
                    actionTouchesOrNearlyTouchesLowerDivider:
                        actionBottomClearanceToNextVisible === null
                            ? null
                            : actionBottomClearanceToNextVisible <= 1.5,
                },
                icon: iconRect,
                key: target.key,
                title: titleText.textContent?.trim() ?? "",
                titleRow: titleRowRect,
                titleSection: sectionRect,
                titleText: titleRect,
            };
        }

        return {
            boxes,
            mode:
                document.querySelector<HTMLElement>(
                    "[data-search-combined-file-browser]",
                )?.dataset.searchFileMode ?? null,
            url: window.location.href,
            viewport: {
                height: window.innerHeight,
                width: window.innerWidth,
            },
        };
    }, targets);
}

async function collectToolsPanelMetric(
    page: Page,
    directoryPath: string,
): Promise<ToolsPanelMetric> {
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
        const panel = document.querySelector(
            `[data-file-browser-folder-controls="${CSS.escape(evaluatedDirectoryPath)}"]`,
        );
        const heading = document.querySelector(
            `[data-directory-heading-with-controls="${CSS.escape(evaluatedDirectoryPath)}"]`,
        );
        const row = document.querySelector(
            `[data-directory-row="${CSS.escape(evaluatedDirectoryPath)}"]`,
        );

        if (
            !(panel instanceof HTMLElement) ||
            !(heading instanceof HTMLElement) ||
            !(row instanceof HTMLElement)
        ) {
            throw new Error("Missing file browser tools panel");
        }

        const headingRect = toRect(heading);
        const panelRect = toRect(panel);
        const rowRect = toRect(row);
        const styles = window.getComputedStyle(panel);
        const buttons = Array.from(
            panel.querySelectorAll<HTMLElement>(
                "[data-file-browser-control-trigger]",
            ),
        ).map((button) => {
            const rect = toRect(button);

            return {
                bottomClearanceWithinPanel: panelRect.bottom - rect.bottom,
                label:
                    button.getAttribute("aria-label") ??
                    button.textContent?.trim() ??
                    "",
                rect,
                topClearanceWithinPanel: rect.top - panelRect.top,
            };
        });
        const topMostButton = Math.min(
            ...buttons.map((button) => button.rect.top),
        );
        const bottomMostButton = Math.max(
            ...buttons.map((button) => button.rect.bottom),
        );
        const buttonTopClearance = topMostButton - panelRect.top;
        const buttonBottomClearance = panelRect.bottom - bottomMostButton;

        return {
            buttons,
            heading: headingRect,
            padding: {
                bottom: Number.parseFloat(styles.paddingBottom),
                left: Number.parseFloat(styles.paddingLeft),
                right: Number.parseFloat(styles.paddingRight),
                top: Number.parseFloat(styles.paddingTop),
            },
            panel: panelRect,
            row: rowRect,
            spacing: {
                bottomPaddingDelta:
                    Number.parseFloat(styles.paddingBottom) -
                    Number.parseFloat(styles.paddingTop),
                buttonBottomClearance,
                buttonTopClearance,
                headingBottomMargin: headingRect.bottom - panelRect.bottom,
                headingTopMargin: panelRect.top - headingRect.top,
                rowBottomMargin: rowRect.bottom - panelRect.bottom,
                rowTopMargin: panelRect.top - rowRect.top,
            },
        };
    }, directoryPath);
}

async function openRnaseqResultDetail(page: Page): Promise<void> {
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

test("reproduces title text spacing mismatch against the file browser baseline", async ({
    page,
}) => {
    mkdirSync(evidenceDir, { recursive: true });
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await expect(page.getByText("Latest result sets")).toBeVisible();
    await expect.poll(async () => resultRows(page).count()).toBeGreaterThan(0);
    const homeLatest = await collectTitleTextSpacingSnapshot(page, [
        {
            actionText: "Columns",
            contentSelector: "thead",
            key: "latestResultSets",
            title: "Latest result sets",
            titleSectionSelector: '[data-results-table-summary="true"]',
        },
    ]);
    await page
        .locator("main")
        .screenshot({ animations: "disabled", path: screenshots.homeLatest });

    await page.goto(
        `/?pipeline_name=${encodeURIComponent(rnaseqPipelineName)}`,
    );
    await expect(page.locator('[data-search-builder="true"]')).toBeVisible();
    await expect(page.locator('[data-file-browser="true"]')).toBeVisible();
    await expect(
        page.locator('[data-search-combined-file-browser="true"]'),
    ).toHaveAttribute("data-search-file-mode", "combined");
    const combinedFiles = await collectTitleTextSpacingSnapshot(page, [
        {
            actionText: "Add filter",
            boxSelector: '[data-search-builder="true"]',
            contentSelector: '[data-search-builder-permanent-fields="true"]',
            key: "search",
            title: "Search",
            titleSectionSelector:
                '[data-search-builder="true"] > div > div:first-child',
        },
        {
            boxSelector: '[data-file-browser="true"]',
            contentSelector: '[data-file-browser="true"] [data-preview-mode]',
            key: "fileBrowser",
            title: "File Browser",
            titleSectionSelector: '[data-file-browser-header="true"]',
        },
    ]);
    await page.locator("main").screenshot({
        animations: "disabled",
        path: screenshots.combinedFiles,
    });

    await page.getByRole("button", { name: "Result sets" }).click();
    await expect(
        page.locator('[data-search-combined-file-browser="true"]'),
    ).toHaveAttribute("data-search-file-mode", "rows");
    await expect(
        page.locator('[data-results-table-summary="true"]'),
    ).toBeVisible();
    const resultSets = await collectTitleTextSpacingSnapshot(page, [
        {
            actionText: "Add filter",
            boxSelector: '[data-search-builder="true"]',
            contentSelector: '[data-search-builder-permanent-fields="true"]',
            key: "search",
            title: "Search",
            titleSectionSelector:
                '[data-search-builder="true"] > div > div:first-child',
        },
        {
            actionText: "Columns",
            contentSelector: "thead",
            key: "searchResults",
            title: "Search results",
            titleSectionSelector: '[data-results-table-summary="true"]',
        },
    ]);
    await page
        .locator("main")
        .screenshot({ animations: "disabled", path: screenshots.resultSets });

    await openRnaseqResultDetail(page);
    await selectDirectory(page, rnaseqGalleryPath);
    const toolsPanel = page.locator(
        `[data-file-browser-folder-controls="${rnaseqGalleryPath}"]`,
    );

    await expect(toolsPanel).toBeVisible();
    await page
        .locator("main")
        .screenshot({ animations: "disabled", path: screenshots.toolsDetail });
    await toolsPanel.screenshot({
        animations: "disabled",
        path: screenshots.toolsPanel,
    });
    const toolsPanelMetric = await collectToolsPanelMetric(
        page,
        rnaseqGalleryPath,
    );
    const evidence = {
        baseline: "combinedFiles.boxes.fileBrowser",
        screenshots,
        snapshots: {
            combinedFiles,
            homeLatest,
            resultSets,
        },
    };

    writeFileSync(titleEvidencePath, `${JSON.stringify(evidence, null, 2)}\n`);
    writeFileSync(
        toolsEvidencePath,
        `${JSON.stringify(
            {
                screenshots: {
                    detail: screenshots.toolsDetail,
                    panel: screenshots.toolsPanel,
                },
                toolsPanel: toolsPanelMetric,
            },
            null,
            2,
        )}\n`,
    );

    const baseline = combinedFiles.boxes.fileBrowser;
    const targets = [
        combinedFiles.boxes.search,
        resultSets.boxes.search,
        resultSets.boxes.searchResults,
        homeLatest.boxes.latestResultSets,
    ];

    for (const target of targets) {
        expect.soft(target, `Missing metric for ${target?.key}`).toBeDefined();

        if (!target || !baseline) {
            continue;
        }

        expect
            .soft(
                Math.abs(
                    target.distances.titleTextTopFromBoxTop -
                        baseline.distances.titleTextTopFromBoxTop,
                ),
                `${target.title} title text top distance from box border should match File Browser`,
            )
            .toBeLessThanOrEqual(1);
        expect
            .soft(
                Math.abs(
                    target.distances.titleTextBottomToNextVisible -
                        baseline.distances.titleTextBottomToNextVisible,
                ),
                `${target.title} title text bottom distance to next visible content should match File Browser`,
            )
            .toBeLessThanOrEqual(1);
        expect
            .soft(
                Math.abs(target.distances.iconCenterDeltaFromTitleText),
                `${target.title} icon should share the title text centerline`,
            )
            .toBeLessThanOrEqual(2);

        if (target.actionButton) {
            expect
                .soft(
                    Math.abs(target.actionButton.centerDeltaFromTitleText),
                    `${target.title} action button should stay centered on the title text`,
                )
                .toBeLessThanOrEqual(1);
            expect
                .soft(
                    target.flags.actionTouchesOrNearlyTouchesLowerDivider,
                    `${target.title} action button should not touch or nearly touch the divider/content below`,
                )
                .toBe(false);
        }
    }

    expect
        .soft(
            Math.abs(
                toolsPanelMetric.padding.top - toolsPanelMetric.padding.bottom,
            ),
            "File Browser tools panel vertical padding should be even",
        )
        .toBeLessThanOrEqual(1);
    expect
        .soft(
            Math.abs(
                toolsPanelMetric.spacing.buttonTopClearance -
                    toolsPanelMetric.spacing.buttonBottomClearance,
            ),
            "File Browser tools panel button top/bottom clearance should be even",
        )
        .toBeLessThanOrEqual(1);
});
