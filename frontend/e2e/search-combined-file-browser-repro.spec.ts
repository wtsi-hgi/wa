import { mkdirSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultRegistration,
    type ResultSet,
} from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const fixtureRoot = path.join(
    evidenceDir,
    "search-combined-file-browser-fixture",
);
const workRoot = path.join(
    fixtureRoot,
    "shared-work",
    "pipelines",
    "2026-06-01",
    "rnaseq",
);
const pipelineName = "wa/combined-browser-repro";
const sampleAlpha = "COMBINED_SAMPLE_ALPHA";
const sampleBeta = "COMBINED_SAMPLE_BETA";

let registeredResults: ResultSet[] = [];

type CapturedConsoleMessage = {
    location: {
        columnNumber: number;
        lineNumber: number;
        url: string;
    };
    text: string;
    type: string;
};

type TitleTreatmentMetric = {
    icon: {
        color: string;
        height: number;
        present: boolean;
        width: number;
    };
    row: {
        alignItems: string;
        columnGap: string;
        display: string;
    };
    title: {
        color: string;
        fontSize: string;
        fontWeight: string;
        letterSpacing: string;
        text: string;
        textTransform: string;
    };
};

type PillMetric = {
    borderRadius: number;
    height: number;
    icon: {
        height: number;
        width: number;
    };
    paddingLeft: number;
    paddingRight: number;
};

type SearchToggleSpacingMetric = {
    aboveButtonsGap: number;
    belowButtonsGap: number;
    boxTop: number;
    mode: string;
    searchBuilderBottom: number;
    toggleBottom: number;
    toggleTop: number;
};

type RectMetric = {
    bottom: number;
    centerY: number;
    height: number;
    left: number;
    right: number;
    top: number;
    width: number;
};

type BoxTitleSpacingTargetMetric = {
    box: RectMetric;
    content: RectMetric | null;
    distances: {
        contentGapFromTitleTextBottom: number | null;
        iconCenterDeltaFromTitleText: number;
        iconLeftInsetWithinBox: number;
        rightButtonBottomInsetWithinTitleSection: number | null;
        rightButtonCenterDeltaFromIcon: number | null;
        rightButtonCenterDeltaFromTitleRow: number | null;
        rightButtonCenterDeltaFromTitleText: number | null;
        rightButtonExtraHeightOverTitleRow: number | null;
        rightButtonTopInsetWithinTitleSection: number | null;
        sectionExtraHeightOverRightButton: number | null;
        sectionExtraHeightOverTitleRow: number;
        titleRowBottomInsetWithinTitleSection: number;
        titleRowTopInsetWithinTitleSection: number;
        titleSectionTopInsetWithinBox: number;
        titleTextBottomInsetWithinTitleSection: number;
        titleTextTopInsetWithinTitleSection: number;
        toggleBottomToBoxTop: number | null;
        toggleBottomToTitleRowTop: number | null;
        toggleBottomToTitleTextTop: number | null;
    };
    icon: RectMetric;
    key: string;
    rightButton: RectMetric | null;
    styles: {
        boxPaddingBottom: number;
        boxPaddingTop: number;
        titleRowAlignItems: string;
        titleRowColumnGap: string;
        titleSectionAlignItems: string;
        titleSectionBorderBottomWidth: number;
        titleSectionDisplay: string;
        titleSectionJustifyContent: string;
        titleSectionPaddingBottom: number;
        titleSectionPaddingTop: number;
        titleSectionRowGap: string;
    };
    title: string;
    titleRow: RectMetric;
    titleSection: RectMetric;
    titleText: RectMetric;
};

type BoxTitleSpacingSnapshot = {
    boxes: Record<string, BoxTitleSpacingTargetMetric>;
    mode: string;
    toggle: RectMetric;
    viewport: {
        height: number;
        width: number;
    };
};

type BoxTitleSpacingComparison = {
    fileBrowserToResultSetsSectionHeightDelta: number;
    fileBrowserToResultSetsTitleRowTopFromToggleDelta: number | null;
    resultSetsRightButtonCenterDeltaFromTitleRow: number | null;
    resultSetsSectionExtraHeightOverTitleRow: number;
    searchRightButtonCenterDeltaFromTitleRow: number | null;
    searchSectionExtraHeightOverTitleRow: number;
};

test.beforeAll(() => {
    registeredResults = [
        registerCombinedBrowserResult({
            sample: sampleAlpha,
            leafDirectory: path.join("results", "samples", "alpha", "final"),
            runKey: "runid=260601&unique=combined-alpha",
            fileName: "alpha-expression-counts.tsv",
            content: "gene\talpha\nENSG000001\t42\n",
        }),
        registerCombinedBrowserResult({
            sample: sampleBeta,
            leafDirectory: path.join("results", "samples", "beta", "final"),
            runKey: "runid=260601&unique=combined-beta",
            fileName: "beta-expression-counts.tsv",
            content: "gene\tbeta\nENSG000001\t84\n",
        }),
    ];
});

test.afterAll(() => {
    for (const result of registeredResults) {
        deleteResult(result.id);
    }
});

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

function registerCombinedBrowserResult({
    content,
    fileName,
    leafDirectory,
    runKey,
    sample,
}: {
    content: string;
    fileName: string;
    leafDirectory: string;
    runKey: string;
    sample: string;
}): ResultSet {
    const outputDirectory = path.join(workRoot, leafDirectory);
    const outputPath = path.join(outputDirectory, fileName);

    mkdirSync(outputDirectory, { recursive: true });
    writeFileSync(outputPath, content);

    const stats = statSync(outputPath);
    const registration: ResultRegistration = {
        pipeline_identifier:
            "https://github.com/wtsi-hgi/wa/combined-browser-repro",
        run_key: runKey,
        requester: "combined-browser-requester",
        operator: "combined-browser-operator",
        command: `nextflow run ${pipelineName} --sample ${sample}`,
        pipeline_name: pipelineName,
        pipeline_version: "2026.06.01",
        output_directory: outputDirectory,
        metadata: {
            sample,
            cohort: "combined-browser-repro",
        },
        files: [
            {
                path: outputPath,
                mtime: stats.mtime.toISOString(),
                size: stats.size,
                kind: "output",
            },
        ],
    };

    return registerResult(registration);
}

function matchingRows(page: Page): Locator {
    return page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasText: pipelineName });
}

function lockedMatchingRows(page: Page): Locator {
    return page
        .locator(
            'tbody tr[data-result-row="true"][data-result-row-locked="true"]',
        )
        .filter({ hasText: pipelineName });
}

async function writeEvidence(
    page: Page,
    screenshotName: string,
    extraEvidence: Record<string, unknown> = {},
): Promise<void> {
    mkdirSync(evidenceDir, { recursive: true });

    const screenshotPath = path.join(evidenceDir, screenshotName);
    const evidencePath = screenshotPath.replace(/\.png$/, ".json");
    const evidence = await page.evaluate(() => {
        const searchBuilder = document.querySelector(
            '[data-search-builder="true"]',
        );
        const fileBrowsers = document.querySelectorAll(
            '[data-file-browser="true"]',
        );
        const combinedSearchFileBrowsers = document.querySelectorAll(
            '[data-search-combined-file-browser="true"]',
        );
        const resultRows = document.querySelectorAll(
            'tbody tr[data-result-row="true"]',
        );
        const lockedResultRows = document.querySelectorAll(
            'tbody tr[data-result-row-locked="true"]',
        );

        return {
            combinedSearchFileBrowserCount: combinedSearchFileBrowsers.length,
            fileBrowserCount: fileBrowsers.length,
            lockedResultRowCount: lockedResultRows.length,
            resultRowCount: resultRows.length,
            searchBuilderText: searchBuilder?.textContent ?? null,
            visibleText: document.body.innerText.slice(0, 4000),
        };
    });

    await page.screenshot({ fullPage: true, path: screenshotPath });
    writeFileSync(
        evidencePath,
        `${JSON.stringify({ ...evidence, ...extraEvidence, screenshotPath }, null, 2)}\n`,
    );
}

async function collectTitleTreatmentMetric(
    page: Page,
    rootSelector: string,
    titleText: string,
): Promise<TitleTreatmentMetric> {
    return page.evaluate(
        ({ rootSelector, titleText }) => {
            const root = document.querySelector(rootSelector);

            if (!(root instanceof HTMLElement)) {
                throw new Error(`Missing title root ${rootSelector}`);
            }

            const title = Array.from(root.querySelectorAll("p")).find(
                (candidate): candidate is HTMLParagraphElement =>
                    candidate instanceof HTMLParagraphElement &&
                    candidate.textContent?.trim() === titleText,
            );

            if (!title) {
                throw new Error(`Missing title ${titleText}`);
            }

            const row = title.parentElement;

            if (!(row instanceof HTMLElement)) {
                throw new Error(`Missing title row for ${titleText}`);
            }

            const icon = row.querySelector("svg");
            const rowStyles = window.getComputedStyle(row);
            const titleStyles = window.getComputedStyle(title);
            const iconStyles =
                icon instanceof SVGElement
                    ? window.getComputedStyle(icon)
                    : null;
            const iconRect =
                icon instanceof SVGElement
                    ? icon.getBoundingClientRect()
                    : null;

            return {
                icon: {
                    color: iconStyles?.color ?? "",
                    height: iconRect?.height ?? 0,
                    present: icon instanceof SVGElement,
                    width: iconRect?.width ?? 0,
                },
                row: {
                    alignItems: rowStyles.alignItems,
                    columnGap: rowStyles.columnGap,
                    display: rowStyles.display,
                },
                title: {
                    color: titleStyles.color,
                    fontSize: titleStyles.fontSize,
                    fontWeight: titleStyles.fontWeight,
                    letterSpacing: titleStyles.letterSpacing,
                    text: title.textContent?.trim() ?? "",
                    textTransform: titleStyles.textTransform,
                },
            };
        },
        { rootSelector, titleText },
    );
}

async function collectPillMetric(locator: Locator): Promise<PillMetric> {
    return locator.evaluate((element) => {
        const button = element as HTMLElement;
        const svg = button.querySelector("svg");

        if (!(svg instanceof SVGElement)) {
            throw new Error(
                `Missing pill icon for ${button.textContent?.trim() ?? "button"}`,
            );
        }

        const buttonRect = button.getBoundingClientRect();
        const iconRect = svg.getBoundingClientRect();
        const computed = window.getComputedStyle(button);

        return {
            borderRadius: Number.parseFloat(computed.borderTopLeftRadius),
            height: buttonRect.height,
            icon: {
                height: iconRect.height,
                width: iconRect.width,
            },
            paddingLeft: Number.parseFloat(computed.paddingLeft),
            paddingRight: Number.parseFloat(computed.paddingRight),
        };
    });
}

async function collectSearchToggleSpacingMetric(
    page: Page,
): Promise<SearchToggleSpacingMetric> {
    return page.evaluate(() => {
        const searchBuilder = document.querySelector<HTMLElement>(
            '[data-search-builder="true"]',
        );
        const combinedShell = document.querySelector<HTMLElement>(
            '[data-search-combined-file-browser="true"]',
        );
        const toggle = document.querySelector<HTMLElement>(
            '[aria-label="Search result display"]',
        );
        const mode = combinedShell?.dataset.searchFileMode ?? "";
        const contentBox =
            mode === "rows"
                ? document.querySelector<HTMLElement>(
                      '[data-results-table-summary="true"]',
                  )?.parentElement
                : document.querySelector<HTMLElement>(
                      '[data-file-browser="true"]',
                  );

        if (!searchBuilder || !combinedShell || !toggle || !contentBox) {
            throw new Error("Missing search toggle spacing measurement target");
        }

        const searchBuilderRect = searchBuilder.getBoundingClientRect();
        const toggleRect = toggle.getBoundingClientRect();
        const contentBoxRect = contentBox.getBoundingClientRect();

        return {
            aboveButtonsGap: toggleRect.top - searchBuilderRect.bottom,
            belowButtonsGap: contentBoxRect.top - toggleRect.bottom,
            boxTop: contentBoxRect.top,
            mode,
            searchBuilderBottom: searchBuilderRect.bottom,
            toggleBottom: toggleRect.bottom,
            toggleTop: toggleRect.top,
        };
    });
}

async function collectBoxTitleSpacingSnapshot(
    page: Page,
): Promise<BoxTitleSpacingSnapshot> {
    return page.evaluate(() => {
        type Target = {
            boxSelector?: string;
            contentSelector?: string;
            key: string;
            rightButtonText?: string;
            title: string;
            titleSectionSelector: string;
        };

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

        const numberStyle = (styles: CSSStyleDeclaration, property: string) =>
            Number.parseFloat(styles.getPropertyValue(property)) || 0;

        const toggle = document.querySelector(
            '[aria-label="Search result display"]',
        );
        const combinedShell = document.querySelector<HTMLElement>(
            '[data-search-combined-file-browser="true"]',
        );

        if (!(toggle instanceof HTMLElement) || !combinedShell) {
            throw new Error("Missing combined file browser toggle");
        }

        const toggleRect = toRect(toggle);
        const targets: Target[] = [
            {
                boxSelector: '[data-search-builder="true"]',
                contentSelector:
                    '[data-search-builder-permanent-fields="true"]',
                key: "search",
                rightButtonText: "Add filter",
                title: "Search",
                titleSectionSelector:
                    '[data-search-builder="true"] > div > div:first-child',
            },
            {
                boxSelector: '[data-file-browser="true"]',
                contentSelector:
                    '[data-file-browser="true"] [data-preview-mode]',
                key: "fileBrowser",
                title: "File Browser",
                titleSectionSelector: '[data-file-browser-header="true"]',
            },
            {
                contentSelector: "thead",
                key: "searchResults",
                rightButtonText: "Columns",
                title: "Search results",
                titleSectionSelector: '[data-results-table-summary="true"]',
            },
        ];

        const boxes: Record<string, BoxTitleSpacingTargetMetric> = {};

        for (const target of targets) {
            const titleSection = document.querySelector(
                target.titleSectionSelector,
            );

            if (!(titleSection instanceof HTMLElement)) {
                continue;
            }

            const box = target.boxSelector
                ? document.querySelector(target.boxSelector)
                : titleSection.parentElement;

            if (!(box instanceof HTMLElement)) {
                continue;
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

            const rightButton = target.rightButtonText
                ? (Array.from(titleSection.querySelectorAll("button")).find(
                      (button) =>
                          button.textContent
                              ?.trim()
                              .includes(target.rightButtonText ?? "") ||
                          button
                              .getAttribute("aria-label")
                              ?.includes(target.rightButtonText ?? ""),
                  ) ?? null)
                : null;
            const content = target.contentSelector
                ? document.querySelector(target.contentSelector)
                : null;
            const boxRect = toRect(box);
            const titleSectionRect = toRect(titleSection);
            const titleRowRect = toRect(titleRow);
            const titleTextRect = toRect(titleText);
            const iconRect = toRect(icon);
            const rightButtonRect =
                rightButton instanceof HTMLElement ? toRect(rightButton) : null;
            const contentRect =
                content instanceof HTMLElement ? toRect(content) : null;
            const boxStyles = window.getComputedStyle(box);
            const titleSectionStyles = window.getComputedStyle(titleSection);
            const titleRowStyles = window.getComputedStyle(titleRow);

            boxes[target.key] = {
                box: boxRect,
                content: contentRect,
                distances: {
                    contentGapFromTitleTextBottom: contentRect
                        ? contentRect.top - titleTextRect.bottom
                        : null,
                    iconCenterDeltaFromTitleText:
                        iconRect.centerY - titleTextRect.centerY,
                    iconLeftInsetWithinBox: iconRect.left - boxRect.left,
                    rightButtonBottomInsetWithinTitleSection: rightButtonRect
                        ? titleSectionRect.bottom - rightButtonRect.bottom
                        : null,
                    rightButtonCenterDeltaFromIcon: rightButtonRect
                        ? rightButtonRect.centerY - iconRect.centerY
                        : null,
                    rightButtonCenterDeltaFromTitleRow: rightButtonRect
                        ? rightButtonRect.centerY - titleRowRect.centerY
                        : null,
                    rightButtonCenterDeltaFromTitleText: rightButtonRect
                        ? rightButtonRect.centerY - titleTextRect.centerY
                        : null,
                    rightButtonExtraHeightOverTitleRow: rightButtonRect
                        ? rightButtonRect.height - titleRowRect.height
                        : null,
                    rightButtonTopInsetWithinTitleSection: rightButtonRect
                        ? rightButtonRect.top - titleSectionRect.top
                        : null,
                    sectionExtraHeightOverRightButton: rightButtonRect
                        ? titleSectionRect.height - rightButtonRect.height
                        : null,
                    sectionExtraHeightOverTitleRow:
                        titleSectionRect.height - titleRowRect.height,
                    titleRowBottomInsetWithinTitleSection:
                        titleSectionRect.bottom - titleRowRect.bottom,
                    titleRowTopInsetWithinTitleSection:
                        titleRowRect.top - titleSectionRect.top,
                    titleSectionTopInsetWithinBox:
                        titleSectionRect.top - boxRect.top,
                    titleTextBottomInsetWithinTitleSection:
                        titleSectionRect.bottom - titleTextRect.bottom,
                    titleTextTopInsetWithinTitleSection:
                        titleTextRect.top - titleSectionRect.top,
                    toggleBottomToBoxTop: boxRect.top - toggleRect.bottom,
                    toggleBottomToTitleRowTop:
                        titleRowRect.top - toggleRect.bottom,
                    toggleBottomToTitleTextTop:
                        titleTextRect.top - toggleRect.bottom,
                },
                icon: iconRect,
                key: target.key,
                rightButton: rightButtonRect,
                styles: {
                    boxPaddingBottom: numberStyle(boxStyles, "padding-bottom"),
                    boxPaddingTop: numberStyle(boxStyles, "padding-top"),
                    titleRowAlignItems: titleRowStyles.alignItems,
                    titleRowColumnGap: titleRowStyles.columnGap,
                    titleSectionAlignItems: titleSectionStyles.alignItems,
                    titleSectionBorderBottomWidth: numberStyle(
                        titleSectionStyles,
                        "border-bottom-width",
                    ),
                    titleSectionDisplay: titleSectionStyles.display,
                    titleSectionJustifyContent:
                        titleSectionStyles.justifyContent,
                    titleSectionPaddingBottom: numberStyle(
                        titleSectionStyles,
                        "padding-bottom",
                    ),
                    titleSectionPaddingTop: numberStyle(
                        titleSectionStyles,
                        "padding-top",
                    ),
                    titleSectionRowGap: titleSectionStyles.rowGap,
                },
                title: titleText.textContent?.trim() ?? "",
                titleRow: titleRowRect,
                titleSection: titleSectionRect,
                titleText: titleTextRect,
            };
        }

        return {
            boxes,
            mode: combinedShell.dataset.searchFileMode ?? "",
            toggle: toggleRect,
            viewport: {
                height: window.innerHeight,
                width: window.innerWidth,
            },
        };
    });
}

function compareBoxTitleSpacing(
    combined: BoxTitleSpacingSnapshot,
    resultSets: BoxTitleSpacingSnapshot,
): BoxTitleSpacingComparison {
    const fileBrowser = combined.boxes.fileBrowser;
    const search = combined.boxes.search;
    const searchResults = resultSets.boxes.searchResults;

    if (!fileBrowser || !search || !searchResults) {
        throw new Error("Missing box title spacing comparison target");
    }

    const titleRowTopFromToggleDelta =
        fileBrowser.distances.toggleBottomToTitleRowTop === null ||
        searchResults.distances.toggleBottomToTitleRowTop === null
            ? null
            : Math.abs(
                  fileBrowser.distances.toggleBottomToTitleRowTop -
                      searchResults.distances.toggleBottomToTitleRowTop,
              );

    return {
        fileBrowserToResultSetsSectionHeightDelta: Math.abs(
            fileBrowser.titleSection.height - searchResults.titleSection.height,
        ),
        fileBrowserToResultSetsTitleRowTopFromToggleDelta:
            titleRowTopFromToggleDelta,
        resultSetsRightButtonCenterDeltaFromTitleRow:
            searchResults.distances.rightButtonCenterDeltaFromTitleRow,
        resultSetsSectionExtraHeightOverTitleRow:
            searchResults.distances.sectionExtraHeightOverTitleRow,
        searchRightButtonCenterDeltaFromTitleRow:
            search.distances.rightButtonCenterDeltaFromTitleRow,
        searchSectionExtraHeightOverTitleRow:
            search.distances.sectionExtraHeightOverTitleRow,
    };
}

async function collectSubfolderPreviewEvidence(page: Page) {
    return page.evaluate(() => {
        const fileBrowser = document.querySelector(
            '[data-file-browser="true"]',
        );
        const directoryRows = [
            ...document.querySelectorAll<HTMLElement>("[data-directory-row]"),
        ].map((row) => ({
            path: row.dataset.directoryRow ?? null,
            text: row.innerText.slice(0, 1200),
        }));
        const controls = [
            ...document.querySelectorAll<HTMLElement>(
                "[data-file-browser-folder-controls]",
            ),
        ].map((control) => ({
            path: control.dataset.fileBrowserFolderControls ?? null,
            subdirPreviewControls:
                control.dataset.subdirPreviewControls ?? null,
            text: control.innerText,
        }));
        const strips = [
            ...document.querySelectorAll<HTMLElement>(
                "[data-subdir-preview-strip]",
            ),
        ].map((strip) => ({
            cardPaths: [
                ...strip.querySelectorAll<HTMLElement>(
                    "[data-subdir-preview-card]",
                ),
            ].map((card) => card.dataset.subdirPreviewCard ?? null),
            path: strip.dataset.subdirPreviewStrip ?? null,
            text: strip.innerText,
        }));

        return {
            controls,
            directoryRows,
            fileBrowserText: fileBrowser?.textContent ?? null,
            previewModeTriggerCount: document.querySelectorAll(
                '[data-file-browser-control-trigger="preview-modes"]',
            ).length,
            subfolderPreviewInputCount: document.querySelectorAll(
                'input[aria-label="Subfolder previews"]',
            ).length,
            subfolderPreviewStripCount: strips.length,
            strips,
        };
    });
}

async function collectCombinedGalleriesRootEvidence(page: Page) {
    return page.evaluate(() => {
        const fileBrowser = document.querySelector<HTMLElement>(
            '[data-file-browser="true"]',
        );
        const directoryRows = [
            ...document.querySelectorAll<HTMLElement>("[data-directory-row]"),
        ].map((row) => ({
            path: row.dataset.directoryRow ?? "",
            text: row.innerText.slice(0, 1000),
        }));
        const directoryFileGroups = [
            ...document.querySelectorAll<HTMLElement>(
                "[data-file-browser-directory-files]",
            ),
        ].map((group) => ({
            directory:
                group.dataset.fileBrowserDirectoryFiles ??
                group.getAttribute("data-file-browser-directory-files") ??
                "",
            files: [
                ...group.querySelectorAll<HTMLElement>("[data-file-path]"),
            ].map((file) => ({
                path: file.dataset.filePath ?? "",
                text: file.innerText,
            })),
        }));
        const resultRows = [
            ...document.querySelectorAll<HTMLElement>(
                'tbody tr[data-result-row="true"]',
            ),
        ].map((row) => row.innerText);

        return {
            directoryFileGroups,
            directoryRows,
            fileBrowserText: fileBrowser?.innerText ?? null,
            resultRows,
            rootDirectory: directoryRows[0]?.path ?? null,
        };
    });
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

async function openPreviewModes(controls: Locator) {
    const summary = controls
        .locator('summary[aria-label="Preview modes"]')
        .first();

    await expect(summary).toBeVisible();
    await summary.evaluate((element) => {
        const details = element.closest("details");

        if (!(details instanceof HTMLDetailsElement)) {
            throw new Error("Missing preview modes disclosure");
        }

        if (!details.open) {
            (element as HTMLElement).click();
        }
    });
}

test.describe("search combined file browser repro", () => {
    test("shows a locked combined file browser state for logged-out matching results", async ({
        context,
        page,
    }) => {
        await context.clearCookies();
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        await expect(matchingRows(page)).toHaveCount(0);
        await expect(lockedMatchingRows(page)).toHaveCount(0);

        await writeEvidence(
            page,
            "search-combined-file-browser-logged-out-locked.png",
        );

        const combinedBrowser = page.locator(
            '[data-search-combined-file-browser="true"]',
        );

        await expect(combinedBrowser).toHaveCount(1);
        await expect(combinedBrowser).toContainText("Combined files");
        await expect(combinedBrowser).toContainText("Result sets");
        await expect(combinedBrowser).not.toContainText("Result rows");
        await expect(combinedBrowser).toContainText("File access locked");
        await expect(combinedBrowser).toContainText("2 matching result sets");
        await expect(combinedBrowser).toContainText(workRoot);
        await expect(
            combinedBrowser.locator('[data-locked-output-directory="true"]'),
        ).toHaveCount(2);
        await expect(
            combinedBrowser.locator("button[data-directory-path]"),
        ).toHaveCount(0);
        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(0);

        await page.getByRole("button", { name: "Result sets" }).click();
        await expect(lockedMatchingRows(page)).toHaveCount(2);
    });

    test("shows one default file browser for all files across matching result sets", async ({
        page,
    }) => {
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        await writeEvidence(
            page,
            "search-combined-file-browser-missing-prefilter.png",
        );

        const searchBuilder = page.locator('[data-search-builder="true"]');
        const combinedBrowser = page.locator('[data-file-browser="true"]');
        const alphaOutputDirectory = path.join(
            workRoot,
            "results",
            "samples",
            "alpha",
            "final",
        );
        const betaOutputDirectory = path.join(
            workRoot,
            "results",
            "samples",
            "beta",
            "final",
        );

        await expect(combinedBrowser).toHaveCount(1);
        await expect(
            combinedBrowser.locator(
                `[data-directory-path="${alphaOutputDirectory}"]`,
            ),
        ).toBeVisible();
        await expect(
            combinedBrowser.locator(
                `[data-directory-path="${betaOutputDirectory}"]`,
            ),
        ).toBeVisible();

        const browserEvidence =
            await collectCombinedGalleriesRootEvidence(page);
        const rootFileGroup = browserEvidence.directoryFileGroups.find(
            (group) => group.directory === browserEvidence.rootDirectory,
        );
        expect(rootFileGroup?.files ?? []).toEqual([]);

        await selectDirectory(page, alphaOutputDirectory);
        await expect(combinedBrowser).toContainText(
            "alpha-expression-counts.tsv",
        );
        await expect(combinedBrowser).not.toContainText(
            "beta-expression-counts.tsv",
        );

        await selectDirectory(page, betaOutputDirectory);
        await expect(combinedBrowser).toContainText(
            "beta-expression-counts.tsv",
        );
        await expect(combinedBrowser).not.toContainText(
            "alpha-expression-counts.tsv",
        );

        const layout = await searchBuilder.evaluate((builder) => {
            const browser = document.querySelector(
                '[data-file-browser="true"]',
            );
            const resultRows = document.querySelectorAll(
                'tbody tr[data-result-row="true"]',
            );

            if (!(browser instanceof HTMLElement)) {
                return null;
            }

            return {
                browserTop: Math.round(browser.getBoundingClientRect().top),
                builderBottom: Math.round(
                    builder.getBoundingClientRect().bottom,
                ),
                resultRowCount: resultRows.length,
            };
        });

        expect(layout).not.toBeNull();
        expect(layout?.browserTop).toBeGreaterThanOrEqual(
            layout?.builderBottom ?? 0,
        );
        expect(layout?.resultRowCount).toBe(0);
    });

    test("hides the search results summary box under the default combined files view", async ({
        page,
    }) => {
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        const summaryBox = page.locator('[data-results-table-summary="true"]');
        const combinedBrowserShell = page.locator(
            '[data-search-combined-file-browser="true"]',
        );

        await expect(combinedBrowserShell).toHaveAttribute(
            "data-search-file-mode",
            "combined",
        );
        await expect(summaryBox).toHaveCount(0);

        const summaryEvidence = await page.evaluate(() => {
            const summary = document.querySelector(
                '[data-results-table-summary="true"]',
            );
            const combinedShell = document.querySelector(
                '[data-search-combined-file-browser="true"]',
            );
            const browser = document.querySelector(
                '[data-file-browser="true"]',
            );

            if (
                !(combinedShell instanceof HTMLElement) ||
                !(browser instanceof HTMLElement)
            ) {
                return null;
            }

            const combinedShellRect = combinedShell.getBoundingClientRect();
            const browserRect = browser.getBoundingClientRect();

            return {
                browserBottom: Math.round(browserRect.bottom),
                browserTop: Math.round(browserRect.top),
                combinedShellBottom: Math.round(combinedShellRect.bottom),
                combinedShellTop: Math.round(combinedShellRect.top),
                matchingHeadingCount: document.querySelectorAll(
                    '[data-results-table-summary="true"] h2',
                ).length,
                searchFileMode: combinedShell.dataset.searchFileMode ?? null,
                summaryCount: document.querySelectorAll(
                    '[data-results-table-summary="true"]',
                ).length,
                summaryText:
                    summary instanceof HTMLElement ? summary.innerText : null,
            };
        });

        await writeEvidence(
            page,
            "search-combined-file-browser-summary-under-combined-view.png",
            {
                searchUrl: page.url(),
                summaryEvidence,
            },
        );

        expect(summaryEvidence).not.toBeNull();
        expect(summaryEvidence?.searchFileMode).toBe("combined");
        expect(summaryEvidence?.summaryCount).toBe(0);
        expect(summaryEvidence?.matchingHeadingCount).toBe(0);
        await expect(matchingRows(page)).toHaveCount(0);
    });

    test("reproduces result rows still rendering under the default combined files view", async ({
        page,
    }) => {
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        const combinedBrowserShell = page.locator(
            '[data-search-combined-file-browser="true"]',
        );
        await expect(combinedBrowserShell).toHaveAttribute(
            "data-search-file-mode",
            "combined",
        );
        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(1);

        const combinedModeEvidence = await page.evaluate(() => {
            const combinedShell = document.querySelector(
                '[data-search-combined-file-browser="true"]',
            );
            const fileBrowser = document.querySelector(
                '[data-file-browser="true"]',
            );
            const table = document.querySelector("table");
            const rows = [
                ...document.querySelectorAll<HTMLElement>(
                    'tbody tr[data-result-row="true"]',
                ),
            ];

            if (
                !(combinedShell instanceof HTMLElement) ||
                !(fileBrowser instanceof HTMLElement)
            ) {
                return null;
            }

            const combinedShellRect = combinedShell.getBoundingClientRect();
            const fileBrowserRect = fileBrowser.getBoundingClientRect();
            const tableRect =
                table instanceof HTMLElement
                    ? table.getBoundingClientRect()
                    : null;
            const firstRowRect = rows[0]?.getBoundingClientRect() ?? null;

            return {
                combinedShellBottom: Math.round(combinedShellRect.bottom),
                combinedShellTop: Math.round(combinedShellRect.top),
                fileBrowserBottom: Math.round(fileBrowserRect.bottom),
                fileBrowserTop: Math.round(fileBrowserRect.top),
                firstResultRowTop: firstRowRect
                    ? Math.round(firstRowRect.top)
                    : null,
                resultRowCount: rows.length,
                resultRowText: rows.map((row) => row.innerText),
                searchFileMode: combinedShell.dataset.searchFileMode ?? null,
                tableTop: tableRect ? Math.round(tableRect.top) : null,
            };
        });

        await writeEvidence(
            page,
            "search-combined-file-browser-result-rows-under-combined-repro.png",
            {
                combinedModeEvidence,
                searchUrl: page.url(),
            },
        );

        expect(combinedModeEvidence).not.toBeNull();
        expect(
            combinedModeEvidence?.resultRowCount,
            "Combined files view should not render result rows under the file browser",
        ).toBe(0);
    });

    test("shows the latest-style title and columns menu in the result sets view", async ({
        page,
    }) => {
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto("/");
        await expect(
            page.locator('[data-results-table-summary="true"]'),
        ).toBeVisible();
        const latestMetric = await collectTitleTreatmentMetric(
            page,
            '[data-results-table-summary="true"]',
            "Latest result sets",
        );
        const latestColumnsMetric = await collectPillMetric(
            page.getByRole("button", { name: "Toggle column visibility" }),
        );

        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        await page.getByRole("button", { name: "Result sets" }).click();

        const combinedBrowserShell = page.locator(
            '[data-search-combined-file-browser="true"]',
        );
        const summary = page.locator('[data-results-table-summary="true"]');
        const columnsButton = page.getByRole("button", {
            name: "Toggle column visibility",
        });

        await expect(combinedBrowserShell).toHaveAttribute(
            "data-search-file-mode",
            "rows",
        );
        await expect(summary).toBeVisible();
        await expect(summary.getByText("Search results")).toBeVisible();
        await expect(summary.getByText("Columns")).toBeVisible();
        await expect(summary.getByText("Showing search results")).toHaveCount(
            0,
        );
        await expect(
            page.getByRole("heading", { name: "Matching result sets" }),
        ).toHaveCount(0);
        await expect(matchingRows(page)).toHaveCount(2);
        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(0);

        const searchMetric = await collectTitleTreatmentMetric(
            page,
            '[data-results-table-summary="true"]',
            "Search results",
        );
        const searchColumnsMetric = await collectPillMetric(columnsButton);
        const headerTextsBefore = await page
            .locator("thead th")
            .allTextContents();

        await columnsButton.click();
        await page.getByRole("menuitemcheckbox", { name: "Requester" }).click();

        const headerTextsAfter = await page
            .locator("thead th")
            .allTextContents();

        await writeEvidence(
            page,
            "search-result-sets-title-treatment-postfix.png",
            {
                headerTextsAfter,
                headerTextsBefore,
                latestColumnsMetric,
                latestMetric,
                searchColumnsMetric,
                searchMetric,
                searchUrl: page.url(),
            },
        );

        expect(searchMetric.title).toEqual({
            ...latestMetric.title,
            text: "Search results",
        });
        expect(searchMetric.row).toEqual(latestMetric.row);
        expect(searchMetric.icon.present).toBe(true);
        expect(searchMetric.icon.color).toBe(latestMetric.icon.color);
        expect(searchMetric.icon.width).toBeCloseTo(latestMetric.icon.width, 1);
        expect(searchMetric.icon.height).toBeCloseTo(
            latestMetric.icon.height,
            1,
        );
        expect(searchColumnsMetric.height).toBeCloseTo(
            latestColumnsMetric.height,
            1,
        );
        expect(searchColumnsMetric.paddingLeft).toBeCloseTo(
            latestColumnsMetric.paddingLeft,
            1,
        );
        expect(searchColumnsMetric.paddingRight).toBeCloseTo(
            latestColumnsMetric.paddingRight,
            1,
        );
        expect(searchColumnsMetric.borderRadius).toBeCloseTo(
            latestColumnsMetric.borderRadius,
            1,
        );
        expect(headerTextsBefore).toContain("Requester");
        expect(headerTextsAfter).not.toContain("Requester");
    });

    test("keeps search result view spacing consistent around the display toggle", async ({
        page,
    }) => {
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        const combinedShell = page.locator(
            '[data-search-combined-file-browser="true"]',
        );

        await expect(combinedShell).toHaveAttribute(
            "data-search-file-mode",
            "combined",
        );
        await expect(page.locator('[data-file-browser="true"]')).toBeVisible();

        const combinedSpacing = await collectSearchToggleSpacingMetric(page);

        await page.getByRole("button", { name: "Result sets" }).click();

        await expect(combinedShell).toHaveAttribute(
            "data-search-file-mode",
            "rows",
        );
        await expect(
            page.locator('[data-results-table-summary="true"]'),
        ).toBeVisible();

        const resultSetsSpacing = await collectSearchToggleSpacingMetric(page);
        const tolerance = 1;

        await writeEvidence(page, "search-toggle-spacing-postfix.png", {
            combinedSpacing,
            resultSetsSpacing,
            searchUrl: page.url(),
        });

        expect(
            combinedSpacing.belowButtonsGap,
            "Combined files content should sit the same distance below the toggle as the Result sets box",
        ).toBeCloseTo(resultSetsSpacing.belowButtonsGap, tolerance);
        expect(
            combinedSpacing.belowButtonsGap,
            "Combined files content should match the vertical rhythm above the toggle",
        ).toBeCloseTo(combinedSpacing.aboveButtonsGap, tolerance);
        expect(
            resultSetsSpacing.belowButtonsGap,
            "Result sets content should match the vertical rhythm above the toggle",
        ).toBeCloseTo(resultSetsSpacing.aboveButtonsGap, tolerance);
    });

    test("keeps title spacing consistent between combined files and result sets boxes", async ({
        page,
    }) => {
        const combinedScreenshotPath = path.join(
            evidenceDir,
            "box-title-spacing-repro-combined.png",
        );
        const resultSetsScreenshotPath = path.join(
            evidenceDir,
            "box-title-spacing-repro-result-sets.png",
        );
        const evidencePath = path.join(
            evidenceDir,
            "box-title-spacing-repro.json",
        );

        mkdirSync(evidenceDir, { recursive: true });
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        const combinedShell = page.locator(
            '[data-search-combined-file-browser="true"]',
        );

        await expect(combinedShell).toHaveAttribute(
            "data-search-file-mode",
            "combined",
        );
        await expect(page.locator('[data-file-browser="true"]')).toBeVisible();

        const combined = await collectBoxTitleSpacingSnapshot(page);

        await page.screenshot({
            animations: "disabled",
            fullPage: true,
            path: combinedScreenshotPath,
        });

        await page.getByRole("button", { name: "Result sets" }).click();
        await expect(combinedShell).toHaveAttribute(
            "data-search-file-mode",
            "rows",
        );
        await expect(
            page.locator('[data-results-table-summary="true"]'),
        ).toBeVisible();

        const resultSets = await collectBoxTitleSpacingSnapshot(page);
        const comparisons = compareBoxTitleSpacing(combined, resultSets);

        await page.screenshot({
            animations: "disabled",
            fullPage: true,
            path: resultSetsScreenshotPath,
        });
        writeFileSync(
            evidencePath,
            `${JSON.stringify(
                {
                    combined,
                    comparisons,
                    resultSets,
                    screenshots: {
                        combined: combinedScreenshotPath,
                        resultSets: resultSetsScreenshotPath,
                    },
                    searchUrl: page.url(),
                },
                null,
                2,
            )}\n`,
        );

        expect(combined.boxes.fileBrowser.title).toBe("File Browser");
        expect(resultSets.boxes.searchResults.title).toBe("Search results");
        expect(
            comparisons.fileBrowserToResultSetsTitleRowTopFromToggleDelta,
            "Combined files and Result sets title rows should start the same distance below the display toggle",
        ).not.toBeNull();
        expect(
            comparisons.fileBrowserToResultSetsTitleRowTopFromToggleDelta ?? 0,
        ).toBeLessThanOrEqual(1);
        expect(
            comparisons.fileBrowserToResultSetsSectionHeightDelta,
            "Result sets should keep the same compact title-section height as File Browser",
        ).toBeLessThanOrEqual(1);
        expect(
            comparisons.resultSetsSectionExtraHeightOverTitleRow,
            "The Columns control should not add extra vertical spacing around the Result sets title row",
        ).toBeLessThanOrEqual(
            combined.boxes.fileBrowser.distances
                .sectionExtraHeightOverTitleRow + 1,
        );
        expect(
            Math.abs(comparisons.searchRightButtonCenterDeltaFromTitleRow ?? 0),
            "Add filter should stay vertically centered on the Search title row",
        ).toBeLessThanOrEqual(1);
        expect(
            Math.abs(
                comparisons.resultSetsRightButtonCenterDeltaFromTitleRow ?? 0,
            ),
            "Columns should stay vertically centered on the Search results title row",
        ).toBeLessThanOrEqual(1);
    });

    test("narrows the combined file browser when the search filters to one sample", async ({
        page,
    }) => {
        await page.goto(
            `/?pipeline_name=${encodeURIComponent(pipelineName)}&sample=${encodeURIComponent(sampleAlpha)}`,
        );

        await writeEvidence(
            page,
            "search-combined-file-browser-missing-sample-filter.png",
        );

        const combinedBrowser = page.locator('[data-file-browser="true"]');

        await expect(combinedBrowser).toHaveCount(1);
        await expect(combinedBrowser).toContainText(
            "alpha-expression-counts.tsv",
        );
        await expect(combinedBrowser).not.toContainText(
            "beta-expression-counts.tsv",
        );
    });

    test("shows the seeded combined galleries fixture and does not emit duplicate key warnings", async ({
        page,
    }) => {
        const consoleMessages: CapturedConsoleMessage[] = [];
        const seededPipelineName = "wtsi/combined-galleries-demo";

        page.on("console", (message) => {
            consoleMessages.push({
                location: message.location(),
                text: message.text(),
                type: message.type(),
            });
        });

        await page.goto(
            `/?pipeline_name=${encodeURIComponent(seededPipelineName)}`,
        );

        await expect(
            page.locator('[data-search-combined-file-browser="true"]'),
        ).toBeVisible();
        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(1);
        const combinedBrowser = page.locator('[data-file-browser="true"]');
        await expect(
            page.locator("tbody tr[data-result-row='true']"),
        ).toHaveCount(0);
        const browserEvidence =
            await collectCombinedGalleriesRootEvidence(page);
        const rootFileGroup = browserEvidence.directoryFileGroups.find(
            (group) => group.directory === browserEvidence.rootDirectory,
        );
        expect(rootFileGroup?.files ?? []).toEqual([]);

        const sampleAOutputDirectory = path.join(
            repoRoot,
            ".docs",
            "results-web",
            "fixtures",
            "files",
            "sibling-gallery-runs",
            "sample-a",
        );
        const sampleBOutputDirectory = path.join(
            repoRoot,
            ".docs",
            "results-web",
            "fixtures",
            "files",
            "sibling-gallery-runs",
            "sample-b",
        );

        await selectDirectory(page, sampleAOutputDirectory);
        await expect(combinedBrowser).toContainText("blue-plot.svg");
        await expect(combinedBrowser).not.toContainText("orange-heatmap.svg");

        await selectDirectory(page, sampleBOutputDirectory);
        await expect(combinedBrowser).toContainText("orange-heatmap.svg");
        await expect(combinedBrowser).not.toContainText("blue-plot.svg");
        await page.waitForTimeout(1000);

        const duplicateKeyMessages = consoleMessages.filter((message) =>
            message.text.includes("Encountered two children with the same key"),
        );

        await writeEvidence(
            page,
            "search-combined-file-browser-galleries-duplicate-key.png",
            {
                consoleMessages,
                duplicateKeyMessages,
                searchUrl: page.url(),
            },
        );

        expect(duplicateKeyMessages).toEqual([]);

        await page.getByRole("button", { name: "Result sets" }).click();
        const resultRows = page.locator("tbody tr[data-result-row='true']");
        await expect(resultRows).toHaveCount(2);
        const resultRowText = (await resultRows.allTextContents()).join("\n");
        expect(resultRowText).toContain("sibling-gallery-runs/sample-a");
        expect(resultRowText).toContain("sibling-gallery-runs/sample-b");
        expect(resultRowText).not.toContain("combined-galleries-demo/sample-a");
        expect(resultRowText).not.toContain("combined-galleries-demo/sample-b");
        expect(resultRowText).not.toContain("galleries-demo/sample-a");
        expect(resultRowText).not.toContain("galleries-demo/sample-b");

        await page.goto(
            `/?pipeline_name=${encodeURIComponent(seededPipelineName)}&sample=${encodeURIComponent("gallery-alpha")}`,
        );

        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "blue-plot.svg",
        );
        await expect(
            page.locator('[data-file-browser="true"]'),
        ).not.toContainText("orange-heatmap.svg");
        await page.getByRole("button", { name: "Result sets" }).click();
        await expect(
            page.locator("tbody tr[data-result-row='true']"),
        ).toHaveCount(1);
    });

    test("characterizes combined galleries files appearing in the common parent directory", async ({
        page,
    }) => {
        const seededPipelineName = "wtsi/combined-galleries-demo";

        await page.goto(
            `/?pipeline_name=${encodeURIComponent(seededPipelineName)}`,
        );

        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(1);

        const browserEvidence =
            await collectCombinedGalleriesRootEvidence(page);
        await writeEvidence(
            page,
            "search-combined-file-browser-galleries-parent-files-repro.png",
            {
                browserEvidence,
                searchUrl: page.url(),
            },
        );

        expect(browserEvidence.rootDirectory).toContain("sibling-gallery-runs");
        expect(browserEvidence.rootDirectory).not.toContain("/sample-a");
        expect(browserEvidence.rootDirectory).not.toContain("/sample-b");

        const rootFiles =
            browserEvidence.directoryFileGroups[0]?.files.map(
                (file) => file.path,
            ) ?? [];
        const rootSampleFiles = rootFiles.filter(
            (file) =>
                file.includes("/sample-a/") || file.includes("/sample-b/"),
        );

        expect(rootSampleFiles).toEqual([]);

        await page.getByRole("button", { name: "Result sets" }).click();
        const resultRows = page.locator("tbody tr[data-result-row='true']");
        await expect(resultRows).toHaveCount(2);

        const resultRowText = (await resultRows.allTextContents()).join("\n");
        expect(resultRowText).toContain("sibling-gallery-runs/sample-a");
        expect(resultRowText).toContain("sibling-gallery-runs/sample-b");
    });

    test("reproduces missing subfolder preview affordance for seeded galleries sample-a", async ({
        page,
    }) => {
        test.setTimeout(120_000);

        const seededPipelineName = "wtsi/combined-galleries-demo";
        const sampleAlphaSearchUrl = `/?pipeline_name=${encodeURIComponent(seededPipelineName)}&sample=${encodeURIComponent("gallery-alpha")}`;

        await page.goto(sampleAlphaSearchUrl);

        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "overview",
        );

        const combinedSearchEvidence =
            await collectSubfolderPreviewEvidence(page);
        await writeEvidence(
            page,
            "search-combined-file-browser-sample-alpha-subfolder-preview-missing.png",
            {
                searchUrl: page.url(),
                subfolderPreview: combinedSearchEvidence,
            },
        );
        await expect
            .soft(
                combinedSearchEvidence.subfolderPreviewInputCount,
                "filtered combined search should offer subfolder previews for the overview folder",
            )
            .toBeGreaterThan(0);

        await page.getByRole("button", { name: "Result sets" }).click();
        await expect(
            page.locator("tbody tr[data-result-row='true']"),
        ).toHaveCount(1);
        const detailHref = await page
            .locator("tbody tr[data-result-row='true']")
            .filter({ hasText: "galleries_sample_a" })
            .locator("a")
            .first()
            .getAttribute("href");

        expect(detailHref).toBeTruthy();

        await page.goto(detailHref ?? "/");
        await expect(page.getByText("galleries_sample_a")).toBeVisible();
        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "overview",
        );

        const detailEvidence = await collectSubfolderPreviewEvidence(page);
        await writeEvidence(
            page,
            "result-detail-galleries-sample-a-subfolder-preview-missing.png",
            {
                detailUrl: page.url(),
                subfolderPreview: detailEvidence,
            },
        );
        await expect
            .soft(
                detailEvidence.subfolderPreviewInputCount,
                "direct result page should offer subfolder previews for the overview folder",
            )
            .toBeGreaterThan(0);

        await page.goto(
            `/?pipeline_name=${encodeURIComponent("wtsi/galleries-demo")}`,
        );
        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "sample-a",
        );

        await page
            .locator('[data-file-browser="true"]')
            .locator('[data-file-browser-control-trigger="preview-modes"]')
            .first()
            .click();
        await page.getByLabel("Subfolder previews").check();

        const parentSearchEvidence =
            await collectSubfolderPreviewEvidence(page);
        await writeEvidence(
            page,
            "search-galleries-demo-subfolder-preview-overview.png",
            {
                searchUrl: page.url(),
                subfolderPreview: parentSearchEvidence,
            },
        );

        await expect
            .soft(
                page.locator('[data-subdir-preview-card*="sample-a/overview"]'),
                "parent galleries-demo subfolder previews should include sample-a overview files",
            )
            .toHaveCount(2);
    });

    test("reproduces overview subfolder previews on the single seeded galleries-demo detail page", async ({
        page,
    }) => {
        test.setTimeout(120_000);

        const parentPipelineName = "wtsi/galleries-demo";
        const parentSearchUrl = `/?pipeline_name=${encodeURIComponent(parentPipelineName)}`;
        const parentOutputDirectory = path.join(
            repoRoot,
            ".docs",
            "results-web",
            "fixtures",
            "files",
            "galleries-demo",
        );
        const sampleAPath = path.join(parentOutputDirectory, "sample-a");
        const overviewPath = path.join(sampleAPath, "overview");

        await page.goto(parentSearchUrl);

        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "sample-a",
        );
        await page.getByRole("button", { name: "Result sets" }).click();

        const resultRows = page.locator("tbody tr[data-result-row='true']");
        await expect(resultRows).toHaveCount(1);
        await expect(resultRows.first()).toContainText(parentPipelineName);
        const resultRowText = (await resultRows.first().textContent()) ?? "";
        expect(resultRowText).toContain(
            ".docs/results-web/fixtures/files/galleries-demo",
        );
        expect(resultRowText).not.toContain("combined-galleries-demo");

        const detailHref = await resultRows
            .first()
            .locator("a")
            .first()
            .getAttribute("href");

        expect(detailHref).toBeTruthy();

        await page.goto(detailHref ?? "/");
        await expect(
            page.getByRole("heading", { level: 1, name: parentPipelineName }),
        ).toBeVisible({ timeout: 30000 });
        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "sample-a",
        );

        await selectDirectory(page, sampleAPath);

        const sampleAControls = page.locator(
            `[data-file-browser-folder-controls="${sampleAPath}"]`,
        );
        await expect(sampleAControls).toBeVisible();
        await openPreviewModes(sampleAControls);
        await sampleAControls
            .locator('input[aria-label="Subfolder previews"]')
            .check();

        const detailEvidence = await collectSubfolderPreviewEvidence(page);
        await writeEvidence(
            page,
            "result-detail-galleries-demo-overview-subfolder-preview.png",
            {
                detailHref,
                detailUrl: page.url(),
                expectedOverviewCards: [
                    path.join(overviewPath, "navy-summary.svg"),
                    path.join(overviewPath, "gold-summary.svg"),
                ],
                parentOutputDirectory,
                parentSearchUrl,
                resultRowText,
                sampleAPath,
                subfolderPreview: detailEvidence,
            },
        );

        await expect(
            page.locator(`[data-subdir-preview-strip="${overviewPath}"]`),
        ).toBeVisible();
        await expect(
            page.locator(
                `[data-subdir-preview-card="${path.join(
                    overviewPath,
                    "navy-summary.svg",
                )}"]`,
            ),
        ).toBeVisible();
        await expect(
            page.locator(
                `[data-subdir-preview-card="${path.join(
                    overviewPath,
                    "gold-summary.svg",
                )}"]`,
            ),
        ).toBeVisible();
    });
});
