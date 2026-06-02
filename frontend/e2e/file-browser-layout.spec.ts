import path from "node:path";
import { mkdirSync, writeFileSync } from "node:fs";

import { expect, test, type Locator, type Page } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

type PreviewBorderSurfaceMetrics = {
    root: {
        height: number;
        width: number;
        x: number;
        y: number;
    };
    surfaces: {
        height: number;
        tagName: string;
        width: number;
        x: number;
        y: number;
    }[];
};

type RectMetrics = {
    height: number;
    width: number;
    x: number;
    y: number;
};

type ExpandedDirectoryBoxMetrics = {
    childGroupRect: {
        height: number;
        width: number;
        x: number;
        y: number;
    } | null;
    childIndentMarkerRect: RectMetrics | null;
    contentBelongsToDirectoryRow: boolean;
    contentRect: RectMetrics | null;
    decorativeRailCount: number;
    fileRowsRect: RectMetrics | null;
    parentButtonRect: RectMetrics | null;
    parentIndentMarkerRect: RectMetrics | null;
    rowRect: RectMetrics | null;
};

type DirectoryDepthIndentMetrics = {
    additionalButtonPaddingPx: number;
    childButtonPaddingLeftPx: number;
    childDepth: string | null;
    childGroupRect: RectMetrics | null;
    childNameInsetWithinOwnRowPx: number | null;
    childNameRect: RectMetrics | null;
    childPath: string;
    childRowRect: RectMetrics | null;
    naturalNestedBoxOffsetPx: number | null;
    parentButtonPaddingLeftPx: number;
    parentDepth: string | null;
    parentNameInsetWithinOwnRowPx: number | null;
    parentNameRect: RectMetrics | null;
    parentPath: string;
    parentRowRect: RectMetrics | null;
    totalChildNameOffsetFromParentNamePx: number | null;
};

type PreviewCornerGripMetrics = {
    backgroundAlpha: number;
    backgroundImage: string;
    borderBottomWidth: number;
    borderLeftWidth: number;
    borderRightWidth: number;
    borderTopWidth: number;
    bottomInsetFromFrame: number;
    bottomInsetFromSurface: number;
    cursor: string;
    frameRightSlack: number;
    handleHeight: number;
    handlePaddingBottom: number;
    handlePaddingLeft: number;
    handlePaddingRight: number;
    handlePaddingTop: number;
    handleWidth: number;
    markerBackgroundImage: string;
    markerBackgroundAlpha: number;
    markerBorderBottomWidth: number;
    markerBorderLeftWidth: number;
    markerBorderRightWidth: number;
    markerBorderTopWidth: number;
    markerBottomInset: number;
    markerClipPath: string;
    markerHeight: number;
    markerRightInset: number;
    markerWidth: number;
    resizeMode: string;
    rightInsetFromFrame: number;
    rightInsetFromSurface: number;
    surfaceBottomRightRadius: string;
    surfaceRightMatchesPreview: boolean;
    surfaceTopLeftRadius: string;
};

async function openResultFileBrowser(page: Page) {
    await page.setViewportSize({ width: 1024, height: 768 });
    await page.goto("/");
    await expect(page.getByText("Latest result sets")).toBeVisible();
    await expect
        .poll(async () => seededRecentRows(page).count())
        .toBeGreaterThanOrEqual(4);

    const resultLink = page
        .getByRole("link", { name: "nf-core/rnaseq" })
        .first();
    const href = await resultLink.getAttribute("href");

    await page.goto(href ?? "/results/");
    await expect(page).toHaveURL(new RegExp(`${href ?? "/results/"}$`));
    await expect(
        page.getByRole("heading", { level: 1, name: "nf-core/rnaseq" }),
    ).toBeVisible({ timeout: 30000 });

    const fileBrowser = page.locator('[data-file-browser="true"]');
    await expect(fileBrowser).toBeVisible({ timeout: 30000 });
}

async function openNamedResultFileBrowser(page: Page, pipelineName: string) {
    await page.setViewportSize({ width: 1024, height: 768 });
    await page.goto("/");
    await expect(page.getByText("Latest result sets")).toBeVisible();
    await expect
        .poll(async () => seededRecentRows(page).count())
        .toBeGreaterThanOrEqual(4);

    const resultLink = page.getByRole("link", { name: pipelineName }).first();
    const href = await resultLink.getAttribute("href");

    await page.goto(href ?? "/results/");
    await expect(page).toHaveURL(new RegExp(`${href ?? "/results/"}$`));
    await expect(
        page.getByRole("heading", { level: 1, name: pipelineName }),
    ).toBeVisible({ timeout: 30000 });

    const fileBrowser = page.locator('[data-file-browser="true"]');
    await expect(fileBrowser).toBeVisible({ timeout: 30000 });
}

function seededRecentRows(page: Page): Locator {
    return page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasNotText: "seqmeta/rendering-repro" });
}

async function openPreviewModes(controls: Locator) {
    const summary = controls
        .locator('summary[aria-label="Preview modes"]')
        .first();

    await setDisclosureOpen(summary, true, "Missing preview modes disclosure");
}

async function openFileTypes(controls: Locator) {
    const summary = controls
        .locator('summary[aria-label="File types"]')
        .first();

    await setDisclosureOpen(summary, true, "Missing file types disclosure");
}

async function setDisclosureOpen(
    summary: Locator,
    shouldBeOpen: boolean,
    missingDisclosureMessage: string,
) {
    await expect(summary).toBeVisible();
    await summary
        .evaluate((element, desiredOpenState: boolean) => {
            const details = element.closest("details");

            if (!(details instanceof HTMLDetailsElement)) {
                throw new Error("__MISSING_DISCLOSURE__");
            }

            if (details.open !== desiredOpenState) {
                (element as HTMLElement).click();
            }
        }, shouldBeOpen)
        .catch((error: Error) => {
            throw new Error(
                error.message.includes("__MISSING_DISCLOSURE__")
                    ? missingDisclosureMessage
                    : error.message,
            );
        });
}

function backgroundAlpha(backgroundColor: string): number {
    const alphaMatch = backgroundColor.match(/\/[\s]*([0-9]*\.?[0-9]+)\)$/);

    if (alphaMatch) {
        return Number.parseFloat(alphaMatch[1] ?? "1");
    }

    const rgbaMatch = backgroundColor.match(
        /^rgba?\([^,]+,[^,]+,[^,]+(?:,[\s]*([0-9]*\.?[0-9]+))?\)$/,
    );

    if (rgbaMatch) {
        return rgbaMatch[1] ? Number.parseFloat(rgbaMatch[1]) : 1;
    }

    return 1;
}

async function expectOpaqueBackground(locator: Locator) {
    const backgroundColor = await locator.evaluate(
        (element) => window.getComputedStyle(element).backgroundColor,
    );

    expect(backgroundAlpha(backgroundColor)).toBe(1);
}

async function openFirstSinglePreview(page: Page, directoryPath: string) {
    await selectDirectory(page, directoryPath);

    const singleLayoutContainer = page
        .locator(`[data-file-browser-single-layout="${directoryPath}"]`)
        .first();
    const preview = singleLayoutContainer.locator(
        '[data-file-browser-preview="single"]',
    );

    await expect(singleLayoutContainer).toBeVisible();
    await expect(preview).toBeVisible();

    return { preview, singleLayoutContainer };
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

async function measurePreviewBorderSurfaces(
    preview: Locator,
): Promise<PreviewBorderSurfaceMetrics> {
    return preview.evaluate((root) => {
        const rootRect = root.getBoundingClientRect();
        const minimumSurfaceHeight = Math.min(rootRect.height * 0.45, 120);

        function rectMetrics(rect: DOMRect) {
            return {
                height: rect.height,
                width: rect.width,
                x: rect.x,
                y: rect.y,
            };
        }

        function hasVisibleBorder(element: Element): boolean {
            const styles = window.getComputedStyle(element);
            const sides = ["Top", "Right", "Bottom", "Left"] as const;

            return sides.some((side) => {
                const width = Number.parseFloat(
                    styles.getPropertyValue(
                        `border-${side.toLowerCase()}-width`,
                    ),
                );
                const style = styles.getPropertyValue(
                    `border-${side.toLowerCase()}-style`,
                );

                return width > 0 && style !== "none" && style !== "hidden";
            });
        }

        const elements = [root, ...root.querySelectorAll("*")];
        const surfaces = elements
            .map((element) => {
                const rect = element.getBoundingClientRect();

                return { element, rect };
            })
            .filter(({ element, rect }) => {
                const startsNearPreviewTop =
                    Math.abs(rect.y - rootRect.y) <= 16;
                const spansPreviewWidth = rect.width >= rootRect.width * 0.9;
                const isMeaningfulSurface = rect.height >= minimumSurfaceHeight;

                return (
                    hasVisibleBorder(element) &&
                    startsNearPreviewTop &&
                    spansPreviewWidth &&
                    isMeaningfulSurface
                );
            })
            .map(({ element, rect }) => ({
                ...rectMetrics(rect),
                tagName: element.tagName.toLowerCase(),
            }));

        return {
            root: rectMetrics(rootRect),
            surfaces,
        };
    });
}

async function measurePreviewCornerGrip(
    frameLocator: Locator,
): Promise<PreviewCornerGripMetrics> {
    await expect(frameLocator).toBeVisible();

    const handle = frameLocator.locator("[data-preview-resize-handle]").first();
    await expect(handle).toBeVisible();

    const metrics = await frameLocator.evaluate((frameElement) => {
        const frame = frameElement as HTMLElement;
        const surface = frame.querySelector("[data-preview-resize-surface]");
        const handle = frame.querySelector("[data-preview-resize-handle]");

        if (
            !(surface instanceof HTMLElement) ||
            !(handle instanceof HTMLElement)
        ) {
            return null;
        }

        const marker = handle.firstElementChild;

        if (!(marker instanceof HTMLElement)) {
            return null;
        }

        function hasVisibleBorder(element: Element): boolean {
            const styles = window.getComputedStyle(element);
            const sides = ["Top", "Right", "Bottom", "Left"] as const;

            return sides.some((side) => {
                const width = Number.parseFloat(
                    styles.getPropertyValue(
                        `border-${side.toLowerCase()}-width`,
                    ),
                );
                const style = styles.getPropertyValue(
                    `border-${side.toLowerCase()}-style`,
                );

                return width > 0 && style !== "none" && style !== "hidden";
            });
        }

        function alphaFromColor(color: string): number {
            const alphaMatch = color.match(/\/[\s]*([0-9]*\.?[0-9]+)\)$/);

            if (alphaMatch) {
                return Number.parseFloat(alphaMatch[1] ?? "1");
            }

            const rgbaMatch = color.match(
                /^rgba?\([^,]+,[^,]+,[^,]+(?:,[\s]*([0-9]*\.?[0-9]+))?\)$/,
            );

            if (rgbaMatch) {
                return rgbaMatch[1] ? Number.parseFloat(rgbaMatch[1]) : 1;
            }

            return 1;
        }

        const borderedPreviewSurface =
            Array.from(frame.querySelectorAll("*"))
                .filter((element): element is HTMLElement => {
                    if (!(element instanceof HTMLElement)) {
                        return false;
                    }

                    const rect = element.getBoundingClientRect();

                    return (
                        hasVisibleBorder(element) &&
                        rect.width > 80 &&
                        rect.height > 80
                    );
                })
                .sort((left, right) => {
                    const leftRect = left.getBoundingClientRect();
                    const rightRect = right.getBoundingClientRect();

                    return (
                        rightRect.width * rightRect.height -
                        leftRect.width * leftRect.height
                    );
                })[0] ?? null;
        const previewSurface =
            frame.querySelector("img") ?? borderedPreviewSurface;

        if (!(previewSurface instanceof HTMLElement)) {
            return null;
        }

        const styles = window.getComputedStyle(handle);
        const markerStyles = window.getComputedStyle(marker);
        const frameRect = frame.getBoundingClientRect();
        const surfaceRect = surface.getBoundingClientRect();
        const handleRect = handle.getBoundingClientRect();
        const markerRect = marker.getBoundingClientRect();
        const previewSurfaceRect = previewSurface.getBoundingClientRect();
        const surfaceStyles = window.getComputedStyle(previewSurface);

        return {
            backgroundAlpha: alphaFromColor(styles.backgroundColor),
            backgroundImage: styles.backgroundImage,
            borderBottomWidth: Number.parseFloat(styles.borderBottomWidth),
            borderLeftWidth: Number.parseFloat(styles.borderLeftWidth),
            borderRightWidth: Number.parseFloat(styles.borderRightWidth),
            borderTopWidth: Number.parseFloat(styles.borderTopWidth),
            bottomInsetFromFrame: frameRect.bottom - handleRect.bottom,
            bottomInsetFromSurface: surfaceRect.bottom - handleRect.bottom,
            cursor: styles.cursor,
            frameRightSlack: frameRect.right - previewSurfaceRect.right,
            handleHeight: handleRect.height,
            handlePaddingBottom: Number.parseFloat(styles.paddingBottom),
            handlePaddingLeft: Number.parseFloat(styles.paddingLeft),
            handlePaddingRight: Number.parseFloat(styles.paddingRight),
            handlePaddingTop: Number.parseFloat(styles.paddingTop),
            handleWidth: handleRect.width,
            markerBackgroundAlpha: alphaFromColor(markerStyles.backgroundColor),
            markerBackgroundImage: markerStyles.backgroundImage,
            markerBorderBottomWidth: Number.parseFloat(
                markerStyles.borderBottomWidth,
            ),
            markerBorderLeftWidth: Number.parseFloat(
                markerStyles.borderLeftWidth,
            ),
            markerBorderRightWidth: Number.parseFloat(
                markerStyles.borderRightWidth,
            ),
            markerBorderTopWidth: Number.parseFloat(
                markerStyles.borderTopWidth,
            ),
            markerBottomInset: handleRect.bottom - markerRect.bottom,
            markerClipPath: markerStyles.clipPath,
            markerHeight: markerRect.height,
            markerRightInset: handleRect.right - markerRect.right,
            markerWidth: markerRect.width,
            resizeMode: window.getComputedStyle(frame).resize,
            rightInsetFromFrame: frameRect.right - handleRect.right,
            rightInsetFromSurface: surfaceRect.right - handleRect.right,
            surfaceBottomRightRadius: surfaceStyles.borderBottomRightRadius,
            surfaceRightMatchesPreview:
                Math.abs(surfaceRect.right - previewSurfaceRect.right) <= 1,
            surfaceTopLeftRadius: surfaceStyles.borderTopLeftRadius,
        };
    });

    if (!metrics) {
        throw new Error("Missing preview resize corner grip metrics");
    }

    return metrics;
}

function expectTriangularPreviewCornerGrip(metrics: PreviewCornerGripMetrics) {
    expect(metrics.resizeMode).toBe("none");
    expect(metrics.backgroundAlpha).toBeLessThanOrEqual(0.05);
    expect(metrics.backgroundImage).toBe("none");
    expect(metrics.borderTopWidth).toBe(0);
    expect(metrics.borderRightWidth).toBe(0);
    expect(metrics.borderBottomWidth).toBe(0);
    expect(metrics.borderLeftWidth).toBe(0);
    expect(metrics.cursor).toBe("ns-resize");
    expect(metrics.handlePaddingTop).toBe(0);
    expect(metrics.handlePaddingRight).toBe(0);
    expect(metrics.handlePaddingBottom).toBe(0);
    expect(metrics.handlePaddingLeft).toBe(0);
    expect(metrics.handleWidth).toBeGreaterThanOrEqual(32);
    expect(metrics.handleWidth).toBeLessThanOrEqual(40);
    expect(metrics.handleHeight).toBeGreaterThanOrEqual(32);
    expect(metrics.handleHeight).toBeLessThanOrEqual(40);
    expect(Math.abs(metrics.rightInsetFromSurface)).toBeLessThanOrEqual(1);
    expect(Math.abs(metrics.bottomInsetFromSurface)).toBeLessThanOrEqual(1);
    expect(Math.abs(metrics.markerRightInset)).toBeLessThanOrEqual(1);
    expect(Math.abs(metrics.markerBottomInset)).toBeLessThanOrEqual(1);
    expect(metrics.markerWidth).toBeGreaterThanOrEqual(20);
    expect(metrics.markerWidth).toBeLessThanOrEqual(26);
    expect(metrics.markerHeight).toBeGreaterThanOrEqual(20);
    expect(metrics.markerHeight).toBeLessThanOrEqual(26);
    expect(metrics.markerBackgroundAlpha).toBeGreaterThanOrEqual(0.9);
    expect(metrics.markerBorderTopWidth).toBe(0);
    expect(metrics.markerBorderLeftWidth).toBe(0);
    expect(metrics.markerBorderRightWidth).toBe(1);
    expect(metrics.markerBorderBottomWidth).toBe(1);
    expect(metrics.markerClipPath).toContain("polygon");
    expect(metrics.markerBackgroundImage).toContain(
        "repeating-linear-gradient",
    );
    expect(metrics.surfaceTopLeftRadius).not.toBe("0px");
    expect(metrics.surfaceBottomRightRadius).toBe("0px");
}

async function measureExpandedDirectoryBox(
    page: Page,
    directoryPath: string,
    childDirectoryPath: string,
): Promise<ExpandedDirectoryBoxMetrics> {
    return page.evaluate(
        ({ childDirectoryPath, directoryPath }) => {
            function byData(attributeName: string, value: string) {
                return document.querySelector(
                    `[${attributeName}="${CSS.escape(value)}"]`,
                );
            }

            const row = byData("data-directory-row", directoryPath);
            const content = byData(
                "data-directory-group-content",
                directoryPath,
            );
            const parentButton = byData("data-directory-path", directoryPath);
            const childButton = byData(
                "data-directory-path",
                childDirectoryPath,
            );
            const fileRows = byData(
                "data-file-browser-directory-files",
                directoryPath,
            );
            const childGroup = byData(
                "data-directory-group",
                childDirectoryPath,
            );

            function rectMetrics(element: Element | null) {
                if (!(element instanceof HTMLElement)) {
                    return null;
                }

                const rect = element.getBoundingClientRect();

                return {
                    height: rect.height,
                    width: rect.width,
                    x: rect.x,
                    y: rect.y,
                };
            }

            const decorativeRailCount = content
                ? Array.from(content.querySelectorAll('[aria-hidden="true"]'))
                      .filter((element) => element instanceof HTMLElement)
                      .filter((element) => {
                          const rect = element.getBoundingClientRect();
                          const styles = window.getComputedStyle(element);
                          const hasVisiblePaint =
                              styles.backgroundImage !== "none" ||
                              (Number.parseFloat(styles.borderLeftWidth) > 0 &&
                                  styles.borderLeftStyle !== "none") ||
                              (Number.parseFloat(styles.borderBottomWidth) >
                                  0 &&
                                  styles.borderBottomStyle !== "none");

                          return (
                              styles.display !== "none" &&
                              styles.visibility !== "hidden" &&
                              rect.height > 24 &&
                              rect.width <= 2 &&
                              hasVisiblePaint
                          );
                      }).length
                : 0;

            return {
                childGroupRect: rectMetrics(childGroup),
                childIndentMarkerRect: rectMetrics(
                    childButton?.children.item(0) ?? null,
                ),
                contentBelongsToDirectoryRow:
                    row instanceof HTMLElement &&
                    content instanceof HTMLElement &&
                    row.contains(content),
                contentRect: rectMetrics(content),
                decorativeRailCount,
                fileRowsRect: rectMetrics(fileRows),
                parentButtonRect: rectMetrics(parentButton),
                parentIndentMarkerRect: rectMetrics(
                    parentButton?.children.item(0) ?? null,
                ),
                rowRect: rectMetrics(row),
            };
        },
        { childDirectoryPath, directoryPath },
    );
}

async function measureDirectoryDepthIndent(
    page: Page,
    parentPath: string,
    childPath: string,
): Promise<DirectoryDepthIndentMetrics> {
    return page.evaluate(
        ({ childPath, parentPath }) => {
            function byData(attributeName: string, value: string) {
                return document.querySelector(
                    `[${attributeName}="${CSS.escape(value)}"]`,
                );
            }

            function rectMetrics(element: Element | null) {
                if (!(element instanceof HTMLElement)) {
                    return null;
                }

                const rect = element.getBoundingClientRect();

                return {
                    height: rect.height,
                    width: rect.width,
                    x: rect.x,
                    y: rect.y,
                };
            }

            function directoryNameElement(button: Element | null) {
                if (!(button instanceof HTMLElement)) {
                    return null;
                }

                const textColumn = button.children.item(1);

                return textColumn?.firstElementChild ?? null;
            }

            function paddingLeft(element: Element | null) {
                if (!(element instanceof HTMLElement)) {
                    return 0;
                }

                return Number.parseFloat(
                    window.getComputedStyle(element).paddingLeft,
                );
            }

            const parentButton = byData("data-directory-path", parentPath);
            const childButton = byData("data-directory-path", childPath);
            const parentRow = byData("data-directory-row", parentPath);
            const childRow = byData("data-directory-row", childPath);
            const childGroup = byData("data-directory-group", childPath);
            const parentNameRect = rectMetrics(
                directoryNameElement(parentButton),
            );
            const childNameRect = rectMetrics(
                directoryNameElement(childButton),
            );
            const parentRowRect = rectMetrics(parentRow);
            const childRowRect = rectMetrics(childRow);
            const parentPaddingLeft = paddingLeft(parentButton);
            const childPaddingLeft = paddingLeft(childButton);

            return {
                additionalButtonPaddingPx: childPaddingLeft - parentPaddingLeft,
                childButtonPaddingLeftPx: childPaddingLeft,
                childDepth:
                    childButton instanceof HTMLElement
                        ? (childButton.dataset.depth ?? null)
                        : null,
                childGroupRect: rectMetrics(childGroup),
                childNameInsetWithinOwnRowPx:
                    childNameRect && childRowRect
                        ? childNameRect.x - childRowRect.x
                        : null,
                childNameRect,
                childPath,
                childRowRect,
                naturalNestedBoxOffsetPx:
                    childRowRect && parentRowRect
                        ? childRowRect.x - parentRowRect.x
                        : null,
                parentButtonPaddingLeftPx: parentPaddingLeft,
                parentDepth:
                    parentButton instanceof HTMLElement
                        ? (parentButton.dataset.depth ?? null)
                        : null,
                parentNameInsetWithinOwnRowPx:
                    parentNameRect && parentRowRect
                        ? parentNameRect.x - parentRowRect.x
                        : null,
                parentNameRect,
                parentPath,
                parentRowRect,
                totalChildNameOffsetFromParentNamePx:
                    childNameRect && parentNameRect
                        ? childNameRect.x - parentNameRect.x
                        : null,
            };
        },
        { childPath, parentPath },
    );
}

test.describe("File Browser single preview layout", () => {
    const fixturesRoot = path.resolve(
        process.cwd(),
        "..",
        ".docs",
        "results-web",
        "fixtures",
        "files",
    );
    const closeSubdirScreenshotPath = path.resolve(
        process.cwd(),
        "test-results",
        "close-subdir-parent-reselected.png",
    );
    const screenshotEvidenceDir = path.resolve(
        process.cwd(),
        "..",
        ".tmp",
        "agent",
    );
    const compactInlineScreenshotPath = path.join(
        screenshotEvidenceDir,
        "file-browser-inline-compact-postfix.png",
    );
    const titleNoRuleScreenshotPath = path.join(
        screenshotEvidenceDir,
        "file-browser-title-no-rule-postfix.png",
    );
    const rootGapScreenshotPath = path.join(
        screenshotEvidenceDir,
        "file-browser-root-gap-postfix.png",
    );
    const previewResizeGripScreenshotPath = path.join(
        screenshotEvidenceDir,
        "preview-resizer-triangular-grid-image-post-fix.png",
    );
    const singlePreviewCsvScreenshotPath = path.join(
        screenshotEvidenceDir,
        "preview-resizer-triangular-single-csv-post-fix.png",
    );
    const subfolderPreviewImageScreenshotPath = path.join(
        screenshotEvidenceDir,
        "preview-resizer-triangular-subfolder-image-post-fix.png",
    );
    const subfolderPreviewTableScreenshotPath = path.join(
        screenshotEvidenceDir,
        "preview-resizer-triangular-subfolder-table-post-fix.png",
    );
    const nestedControlOwnershipScreenshotPath = path.join(
        screenshotEvidenceDir,
        "bug3-nested-controls-hidden-parent-right-aligned-postfix.png",
    );
    const toolsPanelScreenshotPath = path.join(
        screenshotEvidenceDir,
        "file-browser-tools-panel-postfix.png",
    );
    const toolsPanelMetricsPath = path.join(
        screenshotEvidenceDir,
        "file-browser-tools-panel-postfix.json",
    );
    const subdirIndentScreenshotPath = path.join(
        screenshotEvidenceDir,
        "bug3-file-browser-subdir-indent-postfix.png",
    );
    const subdirIndentMetricsPath = path.join(
        screenshotEvidenceDir,
        "bug3-file-browser-subdir-indent-postfix.json",
    );
    const truncatedDirectoryHoverScreenshotPath = path.join(
        screenshotEvidenceDir,
        "file-browser-truncated-directory-hover-postfix.png",
    );
    const truncatedDirectoryHoverEvidencePath = path.join(
        screenshotEvidenceDir,
        "file-browser-truncated-directory-hover-postfix.json",
    );
    const rnaseqRootPath = path.join(fixturesRoot, "rnaseq");
    const rnaseqQcPath = path.join(rnaseqRootPath, "qc");
    const rnaseqImagesPath = path.join(fixturesRoot, "rnaseq", "qc", "images");
    const rnaseqImagePath = path.join(rnaseqImagesPath, "image.png");
    const rnaseqGalleryPath = path.join(
        fixturesRoot,
        "rnaseq",
        "qc",
        "images",
        "gallery",
    );
    const rnaseqNotesPath = path.join(fixturesRoot, "rnaseq", "qc", "notes");
    const rnaseqReportsPath = path.join(fixturesRoot, "rnaseq", "reports");
    const rnaseqReportCsvPath = path.join(rnaseqReportsPath, "report.csv");
    const rnaseqGalleryLowerImagePath = path.join(
        rnaseqGalleryPath,
        "plot-080.png",
    );
    const rnaseqNotesSummaryPath = path.join(rnaseqNotesPath, "summary.txt");
    const galleriesDemoRootPath = path.join(fixturesRoot, "galleries-demo");
    const galleriesDemoSampleAPath = path.join(
        galleriesDemoRootPath,
        "sample-a",
    );
    const galleriesDemoSampleALanesPath = path.join(
        galleriesDemoRootPath,
        "sample-a",
        "lanes",
    );
    const galleriesDemoSampleALane1Path = path.join(
        galleriesDemoSampleALanesPath,
        "lane-1",
    );
    const galleriesDemoLane1NotesPath = path.join(
        galleriesDemoSampleALanesPath,
        "lane-1",
        "lane-1-notes.tsv",
    );

    test("shows the full path on hover for a truncated directory path", async ({
        page,
    }) => {
        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await openResultFileBrowser(page);
        await page.setViewportSize({ width: 360, height: 720 });

        const directoryButton = page
            .locator(`[data-directory-path="${rnaseqRootPath}"]`)
            .first();

        await expect(directoryButton).toBeVisible();
        await directoryButton.scrollIntoViewIfNeeded();
        await expect
            .poll(async () =>
                directoryButton.evaluate((button) => {
                    const textColumn = button.children.item(1);
                    const label = textColumn?.firstElementChild;

                    if (!(label instanceof HTMLElement)) {
                        return false;
                    }

                    return label.scrollWidth > label.clientWidth + 1;
                }),
            )
            .toBe(true);

        await directoryButton.hover();
        await page.waitForTimeout(350);
        await page.screenshot({
            fullPage: true,
            path: truncatedDirectoryHoverScreenshotPath,
        });

        const evidence = await directoryButton.evaluate(
            (button, expectedPath) => {
                const textColumn = button.children.item(1);
                const label = textColumn?.firstElementChild;

                if (!(label instanceof HTMLElement)) {
                    throw new Error("Missing directory path label");
                }

                const styles = window.getComputedStyle(label);
                const rect = label.getBoundingClientRect();
                const tooltipTexts = Array.from(
                    document.querySelectorAll(
                        '[role="tooltip"], [data-radix-popper-content-wrapper], [data-tooltip]',
                    ),
                )
                    .map((element) => element.textContent?.trim() ?? "")
                    .filter((text) => text.length > 0);
                const buttonTitle = button.getAttribute("title");
                const labelTitle = label.getAttribute("title");
                const hasFullPathHoverSignal =
                    buttonTitle === expectedPath ||
                    labelTitle === expectedPath ||
                    tooltipTexts.some((text) => text.includes(expectedPath));

                return {
                    buttonAriaDescribedBy:
                        button.getAttribute("aria-describedby"),
                    buttonTitle,
                    expectedPath,
                    hasFullPathHoverSignal,
                    isTruncated: label.scrollWidth > label.clientWidth + 1,
                    labelClientWidth: label.clientWidth,
                    labelRectWidth: rect.width,
                    labelScrollWidth: label.scrollWidth,
                    labelText: label.textContent?.trim() ?? "",
                    labelTitle,
                    overflowX: styles.overflowX,
                    screenshotPath:
                        "file-browser-truncated-directory-hover-postfix.png",
                    textOverflow: styles.textOverflow,
                    tooltipTexts,
                    whiteSpace: styles.whiteSpace,
                };
            },
            rnaseqRootPath,
        );

        writeFileSync(
            truncatedDirectoryHoverEvidencePath,
            `${JSON.stringify(evidence, null, 2)}\n`,
            "utf8",
        );

        expect(evidence.isTruncated).toBe(true);
        expect(evidence.textOverflow).toBe("ellipsis");
        expect(evidence.hasFullPathHoverSignal).toBe(true);
    });

    test("does not draw a divider under the file browser title row", async ({
        page,
    }) => {
        await openResultFileBrowser(page);

        const fileBrowser = page.locator('[data-file-browser="true"]');
        const header = fileBrowser.locator('[data-file-browser-header="true"]');
        await expect(header).toBeVisible();

        const headerBorder = await header.evaluate((element) => {
            const styles = window.getComputedStyle(element);
            const width = Number.parseFloat(styles.borderBottomWidth);

            function alphaFromColor(color: string) {
                const slashAlpha = color.match(/\/[\s]*([0-9]*\.?[0-9]+)\)$/);

                if (slashAlpha) {
                    return Number.parseFloat(slashAlpha[1] ?? "1");
                }

                const rgbaAlpha = color.match(
                    /^rgba?\([^,]+,[^,]+,[^,]+(?:,[\s]*([0-9]*\.?[0-9]+))?\)$/,
                );

                if (rgbaAlpha) {
                    return rgbaAlpha[1] ? Number.parseFloat(rgbaAlpha[1]) : 1;
                }

                return color === "transparent" ? 0 : 1;
            }

            return {
                alpha: alphaFromColor(styles.borderBottomColor),
                width,
            };
        });

        expect(headerBorder.width * headerBorder.alpha).toBe(0);

        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await page.screenshot({
            fullPage: true,
            path: titleNoRuleScreenshotPath,
        });
    });

    test("keeps the root folder box close to the file browser title", async ({
        page,
    }) => {
        await openResultFileBrowser(page);

        const fileBrowser = page.locator('[data-file-browser="true"]');
        const header = fileBrowser.locator('[data-file-browser-header="true"]');
        const treeShell = fileBrowser.locator("[data-preview-mode]").first();

        await expect(header).toBeVisible();
        await expect(treeShell).toBeVisible();

        const metrics = await fileBrowser.evaluate((browser) => {
            const headerElement = browser.querySelector(
                '[data-file-browser-header="true"]',
            );
            const shellElement = browser.querySelector("[data-preview-mode]");

            if (
                !(headerElement instanceof HTMLElement) ||
                !(shellElement instanceof HTMLElement)
            ) {
                return null;
            }

            const browserRect = browser.getBoundingClientRect();
            const shellRect = shellElement.getBoundingClientRect();
            const titleContentBottom = Array.from(headerElement.children)
                .map((child) => child.getBoundingClientRect())
                .filter((rect) => rect.width > 0 && rect.height > 0)
                .reduce(
                    (bottom, rect) => Math.max(bottom, rect.bottom),
                    headerElement.getBoundingClientRect().top,
                );

            return {
                bottomInset: browserRect.bottom - shellRect.bottom,
                leftInset: shellRect.left - browserRect.left,
                rightInset: browserRect.right - shellRect.right,
                topGap: shellRect.top - titleContentBottom,
            };
        });

        expect(metrics).not.toBeNull();

        const sideInset = Math.min(
            metrics?.leftInset ?? 0,
            metrics?.rightInset ?? 0,
        );
        const surroundingInset = Math.min(sideInset, metrics?.bottomInset ?? 0);

        expect(metrics?.topGap).toBeGreaterThanOrEqual(surroundingInset - 1);
        expect(metrics?.topGap).toBeLessThanOrEqual(surroundingInset + 1);

        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await page.screenshot({
            fullPage: true,
            path: rootGapScreenshotPath,
        });
    });

    test("positions single preview to the right of file metadata at 1024px viewport", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        const { preview, singleLayoutContainer } = await openFirstSinglePreview(
            page,
            rnaseqNotesPath,
        );

        // Get the first file button in the layout
        const fileButton = singleLayoutContainer
            .locator("button[data-file-path]")
            .first();

        await expect(fileButton).toBeVisible();

        const fileBBox = await fileButton.boundingBox();
        const previewBBox = await preview.boundingBox();
        const containerBBox = await singleLayoutContainer.boundingBox();

        if (!fileBBox || !previewBBox || !containerBBox) {
            throw new Error("Missing bounding boxes for layout verification");
        }

        // CRITICAL: Preview left edge must be to the right of the file button right edge
        // This proves the preview is NOT stacked underneath the file metadata
        expect(previewBBox.x).toBeGreaterThan(fileBBox.x + fileBBox.width);

        // Preview top should align with or be near the container top (within padding/border)
        expect(Math.abs(previewBBox.y - containerBBox.y)).toBeLessThan(15);

        // Preview should not be stacked below the file button (must be side-by-side)
        expect(previewBBox.y).toBeLessThan(fileBBox.y + fileBBox.height);
    });

    test("keeps the single preview visible when selecting a lower file row", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await page.setViewportSize({ width: 1024, height: 520 });
        await selectDirectory(page, rnaseqGalleryPath);

        const preview = page.locator('[data-file-browser-preview="single"]');
        const lowerFile = page
            .locator(`[data-file-path="${rnaseqGalleryLowerImagePath}"]`)
            .first();

        await expect(preview).toBeVisible();
        await expect(lowerFile).toBeVisible();
        await lowerFile.scrollIntoViewIfNeeded();
        await lowerFile.click();

        await expect(preview).toBeVisible();

        const viewportMetrics = await preview.evaluate((element) => {
            const rect = element.getBoundingClientRect();

            return {
                bottom: rect.bottom,
                height: rect.height,
                top: rect.top,
                viewportHeight: window.innerHeight,
            };
        });

        expect(viewportMetrics.height).toBeGreaterThan(120);
        expect(viewportMetrics.top).toBeGreaterThanOrEqual(0);
        expect(viewportMetrics.top).toBeLessThanOrEqual(24);
        expect(viewportMetrics.bottom).toBeLessThanOrEqual(
            viewportMetrics.viewportHeight,
        );
    });

    test("reserves the preview column across multiple file rows in single preview mode", async ({
        page,
    }) => {
        await openResultFileBrowser(page);

        await selectDirectory(page, rnaseqGalleryPath);

        const singleLayoutContainer = page
            .locator(`[data-file-browser-single-layout="${rnaseqGalleryPath}"]`)
            .first();
        const preview = singleLayoutContainer.locator(
            '[data-file-browser-preview="single"]',
        );
        const fileButtons = singleLayoutContainer.locator(
            "button[data-file-path]",
        );

        await expect(singleLayoutContainer).toBeVisible();
        await expect(preview).toBeVisible();

        const fileCount = await fileButtons.count();
        expect(fileCount).toBeGreaterThan(1);

        const firstFile = fileButtons.first();
        const lastFile = fileButtons.nth(fileCount - 1);

        await expect(firstFile).toBeVisible();
        await expect(lastFile).toBeVisible();

        const firstFileBBox = await firstFile.boundingBox();
        const lastFileBBox = await lastFile.boundingBox();
        const previewBBox = await preview.boundingBox();

        if (!firstFileBBox || !lastFileBBox || !previewBBox) {
            throw new Error("Missing bounding boxes for layout verification");
        }

        const previewGridRow = await preview.evaluate(
            (element) => (element as HTMLElement).style.gridRow,
        );

        // Preview should reserve the second column for every file row while rendering at its natural sticky height.
        expect(previewGridRow).toBe(`1 / span ${fileCount}`);
        expect(previewBBox.y).toBeLessThanOrEqual(firstFileBBox.y + 15);

        // Preview should still be to the right of all file buttons (not stacked below)
        expect(previewBBox.x).toBeGreaterThan(
            firstFileBBox.x + firstFileBBox.width,
        );
        expect(previewBBox.x).toBeGreaterThan(
            lastFileBBox.x + lastFileBBox.width,
        );
    });

    test("renders single-preview text files with a bordered surface filling the preview area", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqNotesPath);

        const preview = page.locator('[data-file-browser-preview="single"]');
        const summaryFile = page
            .locator(`[data-file-path="${rnaseqNotesSummaryPath}"]`)
            .first();

        await expect(summaryFile).toBeVisible();
        await summaryFile.click();
        await expect(preview).toBeVisible();

        const metrics = await measurePreviewBorderSurfaces(preview);
        const [surface] = [...metrics.surfaces].sort(
            (left, right) =>
                right.width * right.height - left.width * left.height,
        );

        expect(surface).toBeDefined();

        expect(surface.x).toBeCloseTo(metrics.root.x, 1);
        expect(surface.y).toBeCloseTo(metrics.root.y, 1);
        expect(surface.width).toBeGreaterThanOrEqual(metrics.root.width - 2);
        expect(surface.height).toBeGreaterThanOrEqual(metrics.root.height - 2);
    });

    test("renders single-preview csv without an inner vertical scrollbar or detached resize corner", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqReportsPath);

        const preview = page.locator('[data-file-browser-preview="single"]');
        const frame = page
            .locator(`[data-preview-resize-frame="${rnaseqReportCsvPath}"]`)
            .first();
        const reportFile = page
            .locator(`[data-file-path="${rnaseqReportCsvPath}"]`)
            .first();

        await expect(reportFile).toBeVisible();
        await reportFile.click();
        await expect(preview).toBeVisible();
        await expect(preview.locator("table")).toBeVisible();

        const metrics = await preview.evaluate((root) => {
            const frame = root.closest("[data-preview-resize-frame]");
            const handle = root.querySelector("[data-preview-resize-handle]");

            if (
                !(root instanceof HTMLElement) ||
                !(frame instanceof HTMLElement) ||
                !(handle instanceof HTMLElement)
            ) {
                return null;
            }

            function rectMetrics(element: Element) {
                const rect = element.getBoundingClientRect();

                return {
                    bottom: rect.bottom,
                    height: rect.height,
                    right: rect.right,
                    width: rect.width,
                    x: rect.x,
                    y: rect.y,
                };
            }

            function hasVisibleBorder(element: Element) {
                const styles = window.getComputedStyle(element);

                return ["Top", "Right", "Bottom", "Left"].some((side) => {
                    const width = Number.parseFloat(
                        styles.getPropertyValue(
                            `border-${side.toLowerCase()}-width`,
                        ),
                    );
                    const style = styles.getPropertyValue(
                        `border-${side.toLowerCase()}-style`,
                    );

                    return width > 0 && style !== "none" && style !== "hidden";
                });
            }

            const scrollContainers = Array.from(root.querySelectorAll("*"))
                .filter((element): element is HTMLElement => {
                    if (!(element instanceof HTMLElement)) {
                        return false;
                    }

                    const styles = window.getComputedStyle(element);
                    const canScrollVertically =
                        styles.overflowY === "auto" ||
                        styles.overflowY === "scroll";

                    return (
                        canScrollVertically &&
                        element.scrollHeight > element.clientHeight + 1
                    );
                })
                .map((element) => ({
                    clientHeight: element.clientHeight,
                    overflowY: window.getComputedStyle(element).overflowY,
                    scrollHeight: element.scrollHeight,
                    tagName: element.tagName.toLowerCase(),
                }));

            const visibleSurface = Array.from(root.querySelectorAll("*"))
                .filter((element): element is HTMLElement => {
                    if (!(element instanceof HTMLElement)) {
                        return false;
                    }

                    const rect = element.getBoundingClientRect();

                    return (
                        rect.width > 80 &&
                        rect.height > 80 &&
                        hasVisibleBorder(element)
                    );
                })
                .map((element) => {
                    const rect = rectMetrics(element);

                    return {
                        ...rect,
                        area: rect.width * rect.height,
                        tagName: element.tagName.toLowerCase(),
                    };
                })
                .sort((left, right) => right.area - left.area)[0];

            const frameRect = rectMetrics(frame);
            const handleRect = rectMetrics(handle);
            const lineElement = handle.firstElementChild;
            const lineRect =
                lineElement instanceof HTMLElement
                    ? lineElement.getBoundingClientRect()
                    : null;

            return {
                frameRect,
                handleRect,
                lineRect: lineRect ? rectMetrics(lineElement) : null,
                scrollContainers,
                visibleSurface,
            };
        });

        if (!metrics?.visibleSurface) {
            throw new Error("Missing single-preview CSV surface metrics");
        }

        expect(metrics.scrollContainers).toEqual([]);
        expect(metrics.visibleSurface.width).toBeGreaterThanOrEqual(
            metrics.frameRect.width - 2,
        );
        expect(
            Math.abs(metrics.handleRect.right - metrics.visibleSurface.right),
        ).toBeLessThanOrEqual(1);
        expect(
            Math.abs(metrics.handleRect.bottom - metrics.visibleSurface.bottom),
        ).toBeLessThanOrEqual(1);

        const gripMetrics = await measurePreviewCornerGrip(frame);
        expectTriangularPreviewCornerGrip(gripMetrics);
        expect(Math.abs(gripMetrics.rightInsetFromFrame)).toBeLessThanOrEqual(
            1,
        );
        expect(Math.abs(gripMetrics.bottomInsetFromFrame)).toBeLessThanOrEqual(
            1,
        );

        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await page.screenshot({
            fullPage: true,
            path: singlePreviewCsvScreenshotPath,
        });
    });

    test("renders single-preview images with the same corner radius as the preview shell", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqGalleryPath);

        const preview = page.locator('[data-file-browser-preview="single"]');
        const lowerFile = page
            .locator(`[data-file-path="${rnaseqGalleryLowerImagePath}"]`)
            .first();

        await expect(lowerFile).toBeVisible();
        await lowerFile.click();
        await expect(preview).toBeVisible();

        const radiusMetrics = await preview.evaluate((root) => {
            const image = root.querySelector('img[alt="plot-080.png preview"]');
            const shell = image?.closest("div.group.relative");

            if (
                !(image instanceof HTMLElement) ||
                !(shell instanceof HTMLElement)
            ) {
                return null;
            }

            const imageStyles = window.getComputedStyle(image);
            const shellStyles = window.getComputedStyle(shell);

            return {
                imageRadius: imageStyles.borderRadius,
                shellRadius: shellStyles.borderRadius,
            };
        });

        expect(radiusMetrics).not.toBeNull();
        expect(radiusMetrics?.imageRadius).toBe(radiusMetrics?.shellRadius);
    });

    test("expands a directory as one enlarged row box without a left connector rail", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqImagesPath);

        const directoryRow = page.locator(
            `[data-directory-row="${rnaseqImagesPath}"]`,
        );
        const groupContent = page.locator(
            `[data-directory-group-content="${rnaseqImagesPath}"]`,
        );
        const fileRows = page.locator(
            `[data-file-browser-directory-files="${rnaseqImagesPath}"]`,
        );
        const childGroup = page.locator(
            `[data-directory-group="${rnaseqGalleryPath}"]`,
        );

        await expect(groupContent).toBeVisible();
        await expect(fileRows).toBeVisible();
        await expect(childGroup).toBeVisible();

        const metrics = await measureExpandedDirectoryBox(
            page,
            rnaseqImagesPath,
            rnaseqGalleryPath,
        );

        if (
            !metrics.rowRect ||
            !metrics.contentRect ||
            !metrics.fileRowsRect ||
            !metrics.childGroupRect ||
            !metrics.parentButtonRect ||
            !metrics.parentIndentMarkerRect ||
            !metrics.childIndentMarkerRect
        ) {
            throw new Error(
                "Missing expanded directory metrics for browser assertion",
            );
        }

        expect(metrics.contentBelongsToDirectoryRow).toBe(true);
        expect(metrics.decorativeRailCount).toBe(0);

        expect(metrics.contentRect.x).toBeGreaterThanOrEqual(
            metrics.rowRect.x + 8,
        );
        expect(metrics.contentRect.y).toBeGreaterThan(
            metrics.parentButtonRect.y,
        );
        expect(
            metrics.contentRect.y + metrics.contentRect.height,
        ).toBeLessThanOrEqual(metrics.rowRect.y + metrics.rowRect.height + 1);

        expect(metrics.fileRowsRect.x).toBeGreaterThanOrEqual(
            metrics.contentRect.x - 1,
        );
        expect(metrics.fileRowsRect.y).toBeGreaterThanOrEqual(
            metrics.contentRect.y,
        );
        expect(
            metrics.fileRowsRect.x + metrics.fileRowsRect.width,
        ).toBeLessThanOrEqual(
            metrics.contentRect.x + metrics.contentRect.width + 1,
        );

        expect(metrics.childGroupRect.x).toBeGreaterThanOrEqual(
            metrics.contentRect.x - 1,
        );
        expect(metrics.childGroupRect.y).toBeGreaterThan(
            metrics.fileRowsRect.y,
        );
        expect(
            metrics.childGroupRect.x + metrics.childGroupRect.width,
        ).toBeLessThanOrEqual(
            metrics.contentRect.x + metrics.contentRect.width + 1,
        );

        expect(metrics.childIndentMarkerRect.x).toBeGreaterThan(
            metrics.parentIndentMarkerRect.x,
        );
        expect(metrics.childIndentMarkerRect.x).toBeLessThanOrEqual(
            metrics.childGroupRect.x + 24,
        );
    });

    test("does not add depth-based indentation to nested directory names", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqGalleryPath);

        const parentButton = page.locator(
            `[data-directory-path="${rnaseqImagesPath}"]`,
        );
        const childButton = page.locator(
            `[data-directory-path="${rnaseqGalleryPath}"]`,
        );

        await expect(parentButton).toBeVisible();
        await expect(childButton).toBeVisible();

        const metrics = await measureDirectoryDepthIndent(
            page,
            rnaseqImagesPath,
            rnaseqGalleryPath,
        );

        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await page.screenshot({
            fullPage: true,
            path: subdirIndentScreenshotPath,
        });
        writeFileSync(
            subdirIndentMetricsPath,
            `${JSON.stringify(
                {
                    ...metrics,
                    screenshotPath: subdirIndentScreenshotPath,
                },
                null,
                2,
            )}\n`,
        );

        if (
            !metrics.parentRowRect ||
            !metrics.childRowRect ||
            metrics.parentNameInsetWithinOwnRowPx === null ||
            metrics.childNameInsetWithinOwnRowPx === null ||
            metrics.naturalNestedBoxOffsetPx === null ||
            metrics.totalChildNameOffsetFromParentNamePx === null
        ) {
            throw new Error(
                "Missing nested directory indentation metrics for browser assertion",
            );
        }

        expect(Number(metrics.childDepth)).toBe(
            Number(metrics.parentDepth) + 1,
        );
        expect(metrics.naturalNestedBoxOffsetPx).toBeGreaterThanOrEqual(8);
        expect(metrics.naturalNestedBoxOffsetPx).toBeLessThanOrEqual(24);
        expect(metrics.additionalButtonPaddingPx).toBeCloseTo(0, 1);
        expect(metrics.childButtonPaddingLeftPx).toBeCloseTo(
            metrics.parentButtonPaddingLeftPx,
            1,
        );
        expect(metrics.childNameInsetWithinOwnRowPx).toBeCloseTo(
            metrics.parentNameInsetWithinOwnRowPx,
            1,
        );
        expect(metrics.totalChildNameOffsetFromParentNamePx).toBeCloseTo(
            metrics.naturalNestedBoxOffsetPx,
            1,
        );
    });

    test("uses the file browser shell as the visual container for the root directory", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqQcPath);

        const rootRow = page.locator(
            `[data-directory-row="${rnaseqRootPath}"]`,
        );
        const rootContent = page.locator(
            `[data-directory-group-content="${rnaseqRootPath}"]`,
        );
        const nestedRow = page.locator(
            `[data-directory-row="${rnaseqQcPath}"]`,
        );

        await expect(rootRow).toBeVisible();
        await expect(rootContent).toBeVisible();
        await expect(nestedRow).toBeVisible();

        const metrics = await page.evaluate(
            ({ nestedPath, rootPath }) => {
                function byData(attributeName: string, value: string) {
                    return document.querySelector(
                        `[${attributeName}="${CSS.escape(value)}"]`,
                    );
                }

                function surfaceMetrics(element: Element | null) {
                    if (!(element instanceof HTMLElement)) {
                        return null;
                    }

                    const styles = window.getComputedStyle(element);
                    const rect = element.getBoundingClientRect();

                    return {
                        backgroundAlpha: backgroundAlpha(
                            styles.backgroundColor,
                        ),
                        borderBottomWidth: Number.parseFloat(
                            styles.borderBottomWidth,
                        ),
                        borderLeftWidth: Number.parseFloat(
                            styles.borderLeftWidth,
                        ),
                        borderRightWidth: Number.parseFloat(
                            styles.borderRightWidth,
                        ),
                        borderTopWidth: Number.parseFloat(
                            styles.borderTopWidth,
                        ),
                        paddingBottom: Number.parseFloat(styles.paddingBottom),
                        paddingLeft: Number.parseFloat(styles.paddingLeft),
                        paddingRight: Number.parseFloat(styles.paddingRight),
                        paddingTop: Number.parseFloat(styles.paddingTop),
                        rect: {
                            height: rect.height,
                            width: rect.width,
                            x: rect.x,
                            y: rect.y,
                        },
                    };
                }

                function backgroundAlpha(backgroundColor: string): number {
                    const rgbaMatch = backgroundColor.match(
                        /^rgba?\([^,]+,[^,]+,[^,]+(?:,[\s]*([0-9]*\.?[0-9]+))?\)$/,
                    );

                    if (rgbaMatch) {
                        return rgbaMatch[1]
                            ? Number.parseFloat(rgbaMatch[1])
                            : 1;
                    }

                    return backgroundColor === "transparent" ? 0 : 1;
                }

                return {
                    nestedRow: surfaceMetrics(
                        byData("data-directory-row", nestedPath),
                    ),
                    rootContent: surfaceMetrics(
                        byData("data-directory-group-content", rootPath),
                    ),
                    rootRow: surfaceMetrics(
                        byData("data-directory-row", rootPath),
                    ),
                    rootRowContainsContent:
                        byData("data-directory-row", rootPath)?.contains(
                            byData("data-directory-group-content", rootPath),
                        ) ?? false,
                };
            },
            { nestedPath: rnaseqQcPath, rootPath: rnaseqRootPath },
        );

        expect(metrics.rootRow).not.toBeNull();
        expect(metrics.rootContent).not.toBeNull();
        expect(metrics.nestedRow).not.toBeNull();
        expect(metrics.rootRowContainsContent).toBe(true);

        expect(metrics.rootRow).toMatchObject({
            backgroundAlpha: 0,
            borderBottomWidth: 0,
            borderLeftWidth: 0,
            borderRightWidth: 0,
            borderTopWidth: 0,
            paddingBottom: 0,
            paddingLeft: 0,
            paddingRight: 0,
            paddingTop: 0,
        });
        expect(metrics.rootContent).toMatchObject({
            paddingLeft: 0,
            paddingRight: 0,
        });
        expect(metrics.nestedRow?.borderTopWidth).toBeGreaterThan(0);
        expect(metrics.nestedRow?.backgroundAlpha).toBeGreaterThan(0);
    });

    test("keeps inline folder controls compact beside the directory heading when room allows", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqQcPath);

        const filePreviewFolderControls = page
            .locator(`[data-file-browser-folder-controls="${rnaseqQcPath}"]`)
            .filter({ has: page.locator("[data-preview-mode-disclosure]") });

        const folderControls = filePreviewFolderControls.first();
        const directoryRow = page
            .locator(`[data-directory-row="${rnaseqQcPath}"]`)
            .first();
        const directoryButton = directoryRow
            .locator("button[data-directory-path]")
            .first();

        await expect(directoryRow).toBeVisible();
        await expect(directoryButton).toBeVisible();
        await expect(folderControls).toBeVisible();

        const previewModeTrigger = folderControls.locator(
            '[data-file-browser-control-trigger="preview-modes"]',
        );
        const fileTypesTrigger = folderControls.locator(
            '[data-file-browser-control-trigger="file-types"]',
        );
        const previewModeState = folderControls.locator(
            '[data-file-browser-control-current="preview-modes"]',
        );
        const fileTypesState = folderControls.locator(
            '[data-file-browser-control-current="file-types"]',
        );

        await expect(
            folderControls.locator(
                '[data-file-browser-control-trigger="preview-height"]',
            ),
        ).toHaveCount(0);
        await expect(
            folderControls.locator(
                'input[type="range"][aria-label="Preview height"]',
            ),
        ).toHaveCount(0);
        await expect(previewModeTrigger).toBeVisible();
        await expect(fileTypesTrigger).toBeVisible();
        await expect(previewModeState).toBeVisible();
        await expect(fileTypesState).toBeVisible();
        await expect(previewModeState).toHaveText(/preview/i);
        await expect(fileTypesState).toHaveText(/file type/i);

        const previewModeBBox = await previewModeTrigger.boundingBox();
        const fileTypesBBox = await fileTypesTrigger.boundingBox();
        const controlsBBox = await folderControls.boundingBox();
        const buttonBBox = await directoryButton.boundingBox();
        const rowBBox = await directoryRow.boundingBox();

        if (
            !previewModeBBox ||
            !fileTypesBBox ||
            !controlsBBox ||
            !buttonBBox ||
            !rowBBox
        ) {
            throw new Error("Missing bounding boxes for controls verification");
        }

        // The compact controls should share the heading line at desktop width.
        expect(controlsBBox.y).toBeLessThan(
            buttonBBox.y + buttonBBox.height - 12,
        );
        expect(controlsBBox.y + controlsBBox.height).toBeGreaterThan(
            buttonBBox.y + 12,
        );

        // The controls remain inside the same directory row surface.
        expect(controlsBBox.y + controlsBBox.height).toBeLessThanOrEqual(
            rowBBox.y + rowBBox.height + 1,
        );
        expect(controlsBBox.x).toBeGreaterThanOrEqual(rowBBox.x);
        expect(controlsBBox.x + controlsBBox.width).toBeLessThanOrEqual(
            rowBBox.x + rowBBox.width + 1,
        );

        // All primary controls should fit on one row when the viewport has room.
        expect(Math.abs(previewModeBBox.y - fileTypesBBox.y)).toBeLessThan(8);
        expect(fileTypesBBox.x).toBeGreaterThan(previewModeBBox.x);

        // Current state should sit underneath each trigger label, reducing width.
        const triggerStateLayout = await folderControls
            .locator("[data-file-browser-control-trigger]")
            .evaluateAll((triggers) =>
                triggers.map((trigger) => {
                    const kind = trigger.getAttribute(
                        "data-file-browser-control-trigger",
                    );
                    const label = trigger.querySelector(
                        "[data-file-browser-control-label]",
                    );
                    const state = trigger.querySelector(
                        "[data-file-browser-control-current]",
                    );
                    const labelRect = label?.getBoundingClientRect();
                    const stateRect = state?.getBoundingClientRect();
                    const triggerRect = trigger.getBoundingClientRect();

                    return {
                        kind,
                        labelBottom: labelRect?.bottom ?? 0,
                        stateTop: stateRect?.top ?? 0,
                        triggerWidth: triggerRect.width,
                    };
                }),
            );

        for (const layout of triggerStateLayout) {
            expect(layout.stateTop).toBeGreaterThanOrEqual(
                layout.labelBottom - 1,
            );
            expect(layout.triggerWidth).toBeLessThan(180);
        }

        expect(controlsBBox.height).toBeLessThan(72);
    });

    test("renders file browser tools as a clear equal-padded panel", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqGalleryPath);

        const controls = page.locator(
            `[data-file-browser-folder-controls="${rnaseqGalleryPath}"]`,
        );
        const directoryRow = page.locator(
            `[data-directory-row="${rnaseqGalleryPath}"]`,
        );

        await expect(directoryRow).toBeVisible();
        await expect(controls).toBeVisible();

        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await controls.screenshot({
            path: toolsPanelScreenshotPath,
        });

        const measuredMetrics = await page.evaluate((directoryPath) => {
            const controlsElement = document.querySelector(
                `[data-file-browser-folder-controls="${CSS.escape(directoryPath)}"]`,
            );
            const rowElement = document.querySelector(
                `[data-directory-row="${CSS.escape(directoryPath)}"]`,
            );

            if (
                !(controlsElement instanceof HTMLElement) ||
                !(rowElement instanceof HTMLElement)
            ) {
                throw new Error("Missing file-browser tools panel metrics");
            }

            function colorToRgba(color: string) {
                const canvas = document.createElement("canvas");
                canvas.width = 1;
                canvas.height = 1;
                const context = canvas.getContext("2d", {
                    willReadFrequently: true,
                });

                if (!context) {
                    throw new Error("Unable to resolve CSS color");
                }

                context.clearRect(0, 0, 1, 1);
                context.fillStyle = color;
                context.fillRect(0, 0, 1, 1);

                const [red = 0, green = 0, blue = 0, alpha = 0] =
                    context.getImageData(0, 0, 1, 1).data;

                return {
                    alpha: alpha / 255,
                    blue,
                    green,
                    red,
                };
            }

            function relativeLuminance({
                blue,
                green,
                red,
            }: {
                blue: number;
                green: number;
                red: number;
            }) {
                const channels = [red, green, blue].map((value) => {
                    const channel = value / 255;

                    return channel <= 0.03928
                        ? channel / 12.92
                        : ((channel + 0.055) / 1.055) ** 2.4;
                });

                return (
                    0.2126 * (channels[0] ?? 0) +
                    0.7152 * (channels[1] ?? 0) +
                    0.0722 * (channels[2] ?? 0)
                );
            }

            function contrastRatio(
                firstColor: ReturnType<typeof colorToRgba>,
                secondColor: ReturnType<typeof colorToRgba>,
            ) {
                const first = relativeLuminance(firstColor);
                const second = relativeLuminance(secondColor);
                const lighter = Math.max(first, second);
                const darker = Math.min(first, second);

                return (lighter + 0.05) / (darker + 0.05);
            }

            function compositeColor(
                foreground: ReturnType<typeof colorToRgba>,
                background: ReturnType<typeof colorToRgba>,
            ) {
                const alpha =
                    foreground.alpha +
                    background.alpha * (1 - foreground.alpha);

                if (alpha === 0) {
                    return {
                        alpha: 0,
                        blue: 0,
                        green: 0,
                        red: 0,
                    };
                }

                return {
                    alpha,
                    blue:
                        (foreground.blue * foreground.alpha +
                            background.blue *
                                background.alpha *
                                (1 - foreground.alpha)) /
                        alpha,
                    green:
                        (foreground.green * foreground.alpha +
                            background.green *
                                background.alpha *
                                (1 - foreground.alpha)) /
                        alpha,
                    red:
                        (foreground.red * foreground.alpha +
                            background.red *
                                background.alpha *
                                (1 - foreground.alpha)) /
                        alpha,
                };
            }

            function visibleBackground(element: HTMLElement) {
                const ancestors: HTMLElement[] = [];
                let current: HTMLElement | null = element;

                while (current) {
                    ancestors.unshift(current);
                    current = current.parentElement;
                }

                return ancestors.reduce(
                    (background, ancestor) => {
                        const ancestorColor = colorToRgba(
                            window.getComputedStyle(ancestor).backgroundColor,
                        );

                        return compositeColor(ancestorColor, background);
                    },
                    {
                        alpha: 1,
                        blue: 255,
                        green: 255,
                        red: 255,
                    },
                );
            }

            const styles = window.getComputedStyle(controlsElement);
            const rowStyles = window.getComputedStyle(rowElement);
            const rect = controlsElement.getBoundingClientRect();
            const padding = {
                bottom: Number.parseFloat(styles.paddingBottom),
                left: Number.parseFloat(styles.paddingLeft),
                right: Number.parseFloat(styles.paddingRight),
                top: Number.parseFloat(styles.paddingTop),
            };
            const panelColor = colorToRgba(styles.backgroundColor);
            const rowColor = colorToRgba(rowStyles.backgroundColor);
            const panelVisibleColor = visibleBackground(controlsElement);
            const rowVisibleColor = visibleBackground(rowElement);

            return {
                backgroundAlpha: panelColor.alpha,
                backgroundColor: styles.backgroundColor,
                borderColor: styles.borderColor,
                contrastAgainstDirectoryRow: contrastRatio(
                    panelVisibleColor,
                    rowVisibleColor,
                ),
                rawContrastAgainstDirectoryRow: contrastRatio(
                    panelColor,
                    rowColor,
                ),
                height: rect.height,
                padding,
                rowBackgroundColor: rowStyles.backgroundColor,
                width: rect.width,
            };
        }, rnaseqGalleryPath);
        const metrics = {
            ...measuredMetrics,
            screenshotPath: toolsPanelScreenshotPath,
        };

        writeFileSync(
            toolsPanelMetricsPath,
            `${JSON.stringify(metrics, null, 2)}\n`,
        );

        expect(metrics.padding.top).toBeGreaterThanOrEqual(8);
        expect(metrics.padding.bottom).toBe(metrics.padding.top);
        expect(metrics.padding.left).toBe(metrics.padding.top);
        expect(metrics.padding.right).toBe(metrics.padding.top);
        expect(metrics.backgroundAlpha).toBe(1);
        expect(metrics.contrastAgainstDirectoryRow).toBeGreaterThanOrEqual(1.2);
    });

    test("renders subfolder preview controls in the folder-row control slot without the generic preview toggle", async ({
        page,
    }) => {
        await openResultFileBrowser(page);

        const directoryRow = page.locator(
            `[data-directory-row="${rnaseqRootPath}"]`,
        );
        const folderControls = page.locator(
            `[data-file-browser-folder-controls="${rnaseqRootPath}"]`,
        );
        const subdirControls = page.locator(
            `[data-subdir-preview-controls="${rnaseqRootPath}"]`,
        );

        await expect(directoryRow).toBeVisible();

        if ((await folderControls.count()) === 0) {
            const directoryButton = page.locator(
                `[data-directory-path="${rnaseqRootPath}"]`,
            );

            await directoryButton.evaluate((element) => {
                (element as HTMLButtonElement).click();
            });
        }

        await expect(folderControls).toBeVisible();
        await expect(subdirControls).toBeVisible();
        await expect(
            directoryRow.locator(
                `[data-subdir-preview-controls="${rnaseqRootPath}"]`,
            ),
        ).toBeVisible();
        await expect(
            directoryRow.locator('input[aria-label="1 preview per row"]'),
        ).toHaveCount(0);
        await expect(
            folderControls.locator(
                `[data-subdir-preview-kind-disclosure="${rnaseqRootPath}"]`,
            ),
        ).toBeVisible();
        await expect(subdirControls).not.toContainText("Preview file types");
    });

    test("stacks subfolder preview cards beneath the heading and keeps them compact", async ({
        page,
    }) => {
        await openResultFileBrowser(page);

        const directoryButton = page.locator(
            `[data-directory-path="${rnaseqRootPath}"]`,
        );
        const subdirControls = page.locator(
            `[data-subdir-preview-controls="${rnaseqRootPath}"]`,
        );

        if ((await subdirControls.count()) === 0) {
            await directoryButton.click();
        }

        await expect(subdirControls).toBeVisible();
        await openPreviewModes(subdirControls);
        await subdirControls
            .locator('input[aria-label="Subfolder previews"]')
            .check();

        const row = page.locator("[data-subdir-preview-row]").first();
        const heading = row.locator("[data-subdir-preview-heading]").first();
        const strip = row.locator("[data-subdir-preview-strip]").first();
        const card = row.locator("[data-subdir-preview-card]").first();
        const filename = row.locator("[data-subdir-preview-filename]").first();

        await expect(row).toBeVisible();
        await expect(heading).toBeVisible();
        await expect(strip).toBeVisible();
        await expect(card).toBeVisible();
        await expect(filename).toBeVisible();

        const rowBBox = await row.boundingBox();
        const headingBBox = await heading.boundingBox();
        const stripBBox = await strip.boundingBox();
        const cardBBox = await card.boundingBox();

        if (!rowBBox || !headingBBox || !stripBBox || !cardBBox) {
            throw new Error(
                "Missing subfolder preview bounding boxes for layout verification",
            );
        }

        expect(stripBBox.y).toBeGreaterThan(
            headingBBox.y + headingBBox.height - 4,
        );
        expect(Math.abs(stripBBox.x - headingBBox.x)).toBeLessThan(24);
        expect(cardBBox.width).toBeLessThan(rowBBox.width * 0.6);
        await expect(filename).toContainText(".");
    });

    test("renders image thumbnails with overlay controls at a usable width in subfolder preview cards", async ({
        page,
    }) => {
        await openResultFileBrowser(page);

        const directoryButton = page.locator(
            `[data-directory-path="${rnaseqRootPath}"]`,
        );
        const subdirControls = page.locator(
            `[data-subdir-preview-controls="${rnaseqRootPath}"]`,
        );

        if ((await subdirControls.count()) === 0) {
            await directoryButton.click();
        }

        await expect(subdirControls).toBeVisible();
        await openPreviewModes(subdirControls);
        await subdirControls
            .locator('input[aria-label="Subfolder previews"]')
            .check();

        const button = page
            .locator('button[aria-label="Open image lightbox"]')
            .first();
        const downloadLink = page
            .locator('a[aria-label="Download file"]')
            .first();
        const card = button.locator(
            "xpath=ancestor::*[@data-subdir-preview-card][1]",
        );
        const frame = page
            .locator("[data-subdir-preview-frame]")
            .filter({ has: button })
            .first();
        const image = card.locator("img").first();

        await expect(card).toBeVisible();
        await expect(frame).toBeVisible();
        await expect(button).toBeVisible();
        await expect(downloadLink).toBeVisible();
        await expect(image).toBeVisible();
        await expect(button.locator("img")).toHaveCount(0);
        await expect(frame).not.toHaveClass(/border/);
        await expect(frame).not.toHaveClass(/rounded-\[1\.25rem\]/);

        const overlayStructure = await card.evaluate((element) => {
            const cardImage = element.querySelector("img");
            const cardButton = element.querySelector(
                'button[aria-label="Open image lightbox"]',
            );
            const cardDownloadLink = element.querySelector(
                'a[aria-label="Download file"]',
            );

            return {
                overlaySharesImageSurface:
                    cardImage?.parentElement ===
                    cardDownloadLink?.parentElement,
                lightboxControlOverlaysSameShell:
                    cardButton?.parentElement?.contains(cardImage ?? null) ===
                        true &&
                    cardButton?.parentElement?.contains(
                        cardDownloadLink ?? null,
                    ) === true,
            };
        });

        expect(overlayStructure.overlaySharesImageSurface).toBe(true);
        expect(overlayStructure.lightboxControlOverlaysSameShell).toBe(true);

        const cardBBox = await card.boundingBox();
        const buttonBBox = await button.boundingBox();
        const imageBBox = await image.boundingBox();

        if (!cardBBox || !buttonBBox || !imageBBox) {
            throw new Error(
                "Missing subfolder preview image bounding boxes for width verification",
            );
        }

        expect(cardBBox.width).toBeGreaterThan(140);
        expect(buttonBBox.width).toBeGreaterThan(140);
        expect(imageBBox.width).toBeGreaterThan(140);
        expect(imageBBox.height).toBeGreaterThan(100);
    });

    test("keeps the row-preview download overlay inset from the image top-right corner without clipping", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqGalleryPath);

        const controls = page.locator(
            `[data-file-browser-folder-controls="${rnaseqGalleryPath}"]`,
        );

        await expect(controls).toBeVisible();
        await openPreviewModes(controls);
        await controls.locator('input[aria-label="1 preview per row"]').check();

        const row = page
            .locator(
                `[data-file-browser-grid-row="${path.join(rnaseqGalleryPath, "plot-001.png")}"]`,
            )
            .first();
        const preview = row.locator("[data-grid-preview-path]").first();
        const image = preview
            .locator('img[alt="plot-001.png preview"]')
            .first();
        const downloadLink = preview
            .locator('[data-preview-download-overlay="true"]')
            .first();
        const enlargeBadge = preview
            .locator('[data-preview-enlarge-badge="true"]')
            .first();

        await expect(row).toBeVisible();
        await expect(preview).toBeVisible();
        await expect(image).toBeVisible();
        await expect(downloadLink).toBeVisible();
        await expect(enlargeBadge).toBeVisible();

        const overlayMetrics = await preview.evaluate((element) => {
            const imageElement = element.querySelector("img");
            const downloadElement = element.querySelector(
                '[data-preview-download-overlay="true"]',
            );
            const enlargeElement = element.querySelector(
                '[data-preview-enlarge-badge="true"]',
            );

            if (
                !(imageElement instanceof HTMLElement) ||
                !(downloadElement instanceof HTMLElement) ||
                !(enlargeElement instanceof HTMLElement)
            ) {
                return null;
            }

            const imageRect = imageElement.getBoundingClientRect();
            const downloadRect = downloadElement.getBoundingClientRect();
            const enlargeRect = enlargeElement.getBoundingClientRect();

            return {
                bottomInset: imageRect.bottom - enlargeRect.bottom,
                downloadInsideImageBounds:
                    downloadRect.top >= imageRect.top &&
                    downloadRect.right <= imageRect.right &&
                    downloadRect.bottom <= imageRect.bottom,
                leftInset: enlargeRect.left - imageRect.left,
                rightInset: imageRect.right - downloadRect.right,
                topInset: downloadRect.top - imageRect.top,
            };
        });

        if (!overlayMetrics) {
            throw new Error("Missing row preview overlay metrics");
        }

        expect(overlayMetrics.downloadInsideImageBounds).toBe(true);
        expect(overlayMetrics.topInset).toBeGreaterThanOrEqual(8);
        expect(overlayMetrics.rightInset).toBeGreaterThanOrEqual(8);
        expect(overlayMetrics.topInset).toBeLessThanOrEqual(24);
        expect(overlayMetrics.rightInset).toBeLessThanOrEqual(24);
        expect(
            Math.abs(overlayMetrics.topInset - overlayMetrics.bottomInset),
        ).toBeLessThanOrEqual(6);
        expect(
            Math.abs(overlayMetrics.rightInset - overlayMetrics.leftInset),
        ).toBeLessThanOrEqual(6);
    });

    test("renders the preview height grip as a triangular preview-corner affordance", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqGalleryPath);

        const controls = page.locator(
            `[data-file-browser-folder-controls="${rnaseqGalleryPath}"]`,
        );

        await expect(controls).toBeVisible();
        await openPreviewModes(controls);
        await controls.locator('input[aria-label="1 preview per row"]').check();

        const frame = page
            .locator(
                `[data-preview-resize-frame="${path.join(rnaseqGalleryPath, "plot-001.png")}"]`,
            )
            .first();
        const image = frame.locator('img[alt="plot-001.png preview"]').first();
        const handle = frame.locator("[data-preview-resize-handle]").first();
        const surface = frame.locator("[data-preview-resize-surface]").first();

        await expect(frame).toBeVisible();
        await expect(image).toBeVisible();
        await expect(handle).toBeVisible();
        await expect(surface).toBeVisible();

        const gripMetrics = await measurePreviewCornerGrip(frame);
        expectTriangularPreviewCornerGrip(gripMetrics);
        expect(gripMetrics.frameRightSlack).toBeGreaterThan(80);
        expect(gripMetrics.surfaceRightMatchesPreview).toBe(true);
        expect(gripMetrics.rightInsetFromFrame).toBeGreaterThan(80);
        expect(Math.abs(gripMetrics.bottomInsetFromFrame)).toBeLessThanOrEqual(
            1,
        );

        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await frame.screenshot({
            path: previewResizeGripScreenshotPath,
        });
    });

    test("renders the triangular resize corner in subfolder image and table previews", async ({
        page,
    }) => {
        await openResultFileBrowser(page);

        const directoryButton = page.locator(
            `[data-directory-path="${rnaseqRootPath}"]`,
        );
        const subdirControls = page.locator(
            `[data-subdir-preview-controls="${rnaseqRootPath}"]`,
        );

        if ((await subdirControls.count()) === 0) {
            await directoryButton.click();
        }

        await expect(subdirControls).toBeVisible();
        await openPreviewModes(subdirControls);
        await subdirControls
            .locator('input[aria-label="Subfolder previews"]')
            .check();

        const imageFrame = page
            .locator(`[data-subdir-preview-frame="${rnaseqImagePath}"]`)
            .first();
        const tableFrame = page
            .locator("[data-subdir-preview-frame]")
            .filter({ has: page.locator("table") })
            .first();

        await expect(imageFrame.locator("img")).toBeVisible();
        await expect(tableFrame.locator("table")).toBeVisible();

        const imageGripMetrics = await measurePreviewCornerGrip(imageFrame);
        const tableGripMetrics = await measurePreviewCornerGrip(tableFrame);

        expectTriangularPreviewCornerGrip(imageGripMetrics);
        expectTriangularPreviewCornerGrip(tableGripMetrics);
        expect(imageGripMetrics.surfaceRightMatchesPreview).toBe(true);
        expect(tableGripMetrics.surfaceRightMatchesPreview).toBe(true);

        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await imageFrame.screenshot({
            path: subfolderPreviewImageScreenshotPath,
        });
        await tableFrame.screenshot({
            path: subfolderPreviewTableScreenshotPath,
        });
    });

    test("renders subfolder table previews with a single bordered surface", async ({
        page,
    }) => {
        await openNamedResultFileBrowser(page, "wtsi/galleries-demo");

        await selectDirectory(page, galleriesDemoSampleAPath);
        await selectDirectory(page, galleriesDemoSampleALanesPath);

        const controls = page.locator(
            `[data-subdir-preview-controls="${galleriesDemoSampleALanesPath}"]`,
        );

        await expect(controls).toBeVisible();
        await openPreviewModes(controls);

        const toggle = controls.locator(
            'input[aria-label="Subfolder previews"]',
        );
        const disclosure = page
            .locator("[data-subdir-preview-kind-disclosure]")
            .last();

        if (!(await toggle.isChecked())) {
            await toggle.click();
        }

        await disclosure.evaluate((element) => {
            const tableCheckbox = element.querySelector(
                'input[data-subdir-preview-kind="table"]',
            );
            const imageCheckbox = element.querySelector(
                'input[data-subdir-preview-kind="image"]',
            );

            if (!(tableCheckbox instanceof HTMLInputElement)) {
                throw new Error("Missing table subdir kind checkbox");
            }

            if (!(imageCheckbox instanceof HTMLInputElement)) {
                throw new Error("Missing image subdir kind checkbox");
            }

            if (!tableCheckbox.checked) {
                tableCheckbox.click();
            }

            if (imageCheckbox.checked) {
                imageCheckbox.click();
            }
        });

        const tableFrame = page.locator(
            `[data-subdir-preview-frame="${galleriesDemoLane1NotesPath}"]`,
        );

        await expect(tableFrame).toBeVisible();

        const metrics = await measurePreviewBorderSurfaces(tableFrame);

        expect(metrics.surfaces).toHaveLength(1);
        expect(metrics.surfaces[0]?.width).toBeGreaterThanOrEqual(
            metrics.root.width - 2,
        );
        expect(metrics.surfaces[0]?.height).toBeGreaterThanOrEqual(
            metrics.root.height - 2,
        );
    });

    test("keeps parent subfolder preview widgets visible until the parent collapses", async ({
        page,
    }) => {
        await openResultFileBrowser(page);

        const parentButton = page.locator(
            `[data-directory-path="${rnaseqRootPath}"]`,
        );
        const parentControls = page.locator(
            `[data-subdir-preview-controls="${rnaseqRootPath}"]`,
        );
        const previewModeDisclosure = page.locator(
            `[data-preview-mode-disclosure="${rnaseqRootPath}"]`,
        );
        const parentGalleryRow = page
            .locator("[data-subdir-preview-row]")
            .first();

        if ((await parentControls.count()) === 0) {
            await parentButton.click();
        }

        await expect(parentControls).toBeVisible();
        await openPreviewModes(parentControls);

        const subfolderToggle = parentControls
            .locator('input[aria-label="Subfolder previews"]')
            .first();

        if (
            (await parentButton.getAttribute("data-directory-expanded")) !==
            "true"
        ) {
            await parentButton.click();
        }

        await expect(parentButton).toHaveAttribute(
            "data-directory-expanded",
            "true",
        );
        await expect(parentControls).toBeVisible();
        await expect(subfolderToggle).not.toBeChecked();

        await subfolderToggle.check();
        await expect(parentControls).toBeVisible();
        await expect(subfolderToggle).toBeChecked();
        await expect(parentGalleryRow).toBeVisible();

        await parentButton.click();
        await expect(previewModeDisclosure).not.toHaveAttribute("open", "");
        await expect(parentButton).toHaveAttribute(
            "data-directory-expanded",
            "true",
        );
        await expect(parentControls).toBeVisible();
        await expect(parentGalleryRow).toBeVisible();

        await parentButton.click();
        await expect(parentButton).toHaveAttribute(
            "data-directory-expanded",
            "false",
        );
        await expect(parentControls).toHaveCount(0);
        await expect(parentGalleryRow).toHaveCount(0);

        await page.screenshot({
            fullPage: true,
            path: closeSubdirScreenshotPath,
        });
    });

    test("hides parent preview tools while a nested eligible subfolder is open and keeps the active tools beside the heading", async ({
        page,
    }) => {
        await page.setViewportSize({ width: 1440, height: 900 });
        await openNamedResultFileBrowser(page, "wtsi/galleries-demo");
        await page.setViewportSize({ width: 1440, height: 900 });

        await selectDirectory(page, galleriesDemoSampleAPath);

        const sampleAControls = page.locator(
            `[data-file-browser-folder-controls="${galleriesDemoSampleAPath}"]`,
        );

        await expect(sampleAControls).toBeVisible();
        await expect(
            page.locator(
                `[data-file-browser-folder-controls="${galleriesDemoSampleAPath}"] input[aria-label="1 preview per row"]`,
            ),
        ).toHaveCount(1);

        await selectDirectory(page, galleriesDemoSampleALanesPath);

        const lanesControls = page.locator(
            `[data-file-browser-folder-controls="${galleriesDemoSampleALanesPath}"]`,
        );

        await expect(sampleAControls).toHaveCount(0);
        await expect(lanesControls).toBeVisible();

        await selectDirectory(page, galleriesDemoSampleALane1Path);

        const lane1Controls = page.locator(
            `[data-file-browser-folder-controls="${galleriesDemoSampleALane1Path}"]`,
        );
        const lane1Button = page.locator(
            `button[data-directory-path="${galleriesDemoSampleALane1Path}"]`,
        );

        await expect(lanesControls).toHaveCount(0);
        await expect(lane1Controls).toBeVisible();
        await expect(
            lane1Controls.locator(
                '[data-file-browser-control-trigger="preview-modes"]',
            ),
        ).toBeVisible();
        await expect(
            lane1Controls.locator(
                '[data-file-browser-control-trigger="file-types"]',
            ),
        ).toBeVisible();

        const controlsBox = await lane1Controls.boundingBox();
        const buttonBox = await lane1Button.boundingBox();

        if (!controlsBox || !buttonBox) {
            throw new Error("Missing nested control layout boxes");
        }

        expect(controlsBox.x).toBeGreaterThan(buttonBox.x);
        expect(controlsBox.y).toBeLessThan(buttonBox.y + buttonBox.height - 12);
        expect(controlsBox.y + controlsBox.height).toBeGreaterThan(
            buttonBox.y + 12,
        );

        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await page.screenshot({
            fullPage: true,
            path: nestedControlOwnershipScreenshotPath,
        });
    });

    test("keeps parent and child preview mode selectors in sync when opening a nested subfolder", async ({
        page,
    }) => {
        await openNamedResultFileBrowser(page, "wtsi/galleries-demo");

        await selectDirectory(page, galleriesDemoSampleAPath);

        const sampleAControls = page.locator(
            `[data-file-browser-folder-controls="${galleriesDemoSampleAPath}"]`,
        );
        const sampleASummary = sampleAControls.locator(
            'summary[aria-label="Preview modes"]',
        );

        await expect(sampleAControls).toBeVisible();
        await openPreviewModes(sampleAControls);
        await sampleAControls
            .locator('input[aria-label="1 preview per row"]')
            .check();
        await sampleAControls
            .locator('input[aria-label="Subfolder previews"]')
            .check();
        await expect(sampleASummary).toContainText("Grid + subfolders");

        await sampleASummary.click();

        await selectDirectory(page, galleriesDemoSampleALanesPath);

        const lanesControls = page.locator(
            `[data-file-browser-folder-controls="${galleriesDemoSampleALanesPath}"]`,
        );
        const lanesSummary = lanesControls.locator(
            'summary[aria-label="Preview modes"]',
        );

        await expect(sampleAControls).toHaveCount(0);
        await expect(lanesControls).toBeVisible();
        await expect(sampleASummary).toHaveCount(0);
        await expect(
            sampleAControls.locator('input[aria-label="1 preview per row"]'),
        ).toHaveCount(0);
        await expect(lanesSummary).toContainText("Single preview");
        await expect(
            lanesControls.locator('input[aria-label="1 preview per row"]'),
        ).toHaveCount(0);

        await page
            .locator(
                `button[data-directory-path="${galleriesDemoSampleALanesPath}"]`,
            )
            .click();

        await expect(lanesControls).toHaveCount(0);
        await expect(sampleAControls).toBeVisible();
        await expect(sampleASummary).toContainText("Grid + subfolders");

        await openPreviewModes(sampleAControls);
        await sampleAControls
            .locator('input[aria-label="1 preview per row"]')
            .click();

        await expect(sampleASummary).toContainText("Subfolders");
        await expect(
            sampleAControls.locator('input[aria-label="1 preview per row"]'),
        ).not.toBeChecked();
        await expect(lanesSummary).toHaveCount(0);
    });

    test("renders solid backgrounds for preview mode and file type menus", async ({
        page,
    }) => {
        await openResultFileBrowser(page);

        const controls = page.locator(
            `[data-file-browser-folder-controls="${rnaseqRootPath}"]`,
        );

        if ((await controls.count()) === 0) {
            await page
                .locator(`[data-directory-path="${rnaseqRootPath}"]`)
                .click();
        }

        await expect(controls).toBeVisible();
        const previewSummary = controls.locator(
            'summary[aria-label="Preview modes"]',
        );
        const previewMenu = page.locator(
            `[data-preview-modes-menu="${rnaseqRootPath}"]`,
        );

        await expectOpaqueBackground(previewSummary);
        await expectOpaqueBackground(previewMenu);

        await setDisclosureOpen(
            previewSummary,
            false,
            "Missing preview modes disclosure",
        );

        const fileTypesSummary = controls.locator(
            'summary[aria-label="File types"]',
        );
        const fileTypesMenu = page.locator(
            `[data-subdir-preview-kinds="${rnaseqRootPath}"]`,
        );

        await openFileTypes(controls);
        await expectOpaqueBackground(fileTypesSummary);
        await expectOpaqueBackground(fileTypesMenu);
    });

    test("renders only the compact inline file browser design", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqGalleryPath);

        const fileBrowser = page.locator('[data-file-browser="true"]');
        const controls = page.locator(
            `[data-file-browser-folder-controls="${rnaseqGalleryPath}"]`,
        );
        const directoryRow = page.locator(
            `[data-directory-row="${rnaseqGalleryPath}"]`,
        );
        const directoryButton = directoryRow
            .locator(`[data-directory-path="${rnaseqGalleryPath}"]`)
            .first();
        const firstFile = page
            .locator(
                `[data-file-path="${path.join(rnaseqGalleryPath, "plot-001.png")}"]`,
            )
            .first();

        await expect(
            page.locator('[data-file-browser-design-selector="true"]'),
        ).toHaveCount(0);
        await expect(
            page.locator("[data-file-browser-design-option]"),
        ).toHaveCount(0);
        await expect(fileBrowser).toHaveAttribute(
            "data-file-browser-design",
            "inline",
        );
        mkdirSync(screenshotEvidenceDir, { recursive: true });
        await expect(controls).toBeVisible();
        await expect(controls).toHaveAttribute(
            "data-file-browser-control-surface",
            "true",
        );
        await expect(controls).toHaveAttribute(
            "data-file-browser-control-style",
            "inline-nameplate",
        );
        await expect(controls).toHaveAttribute(
            "data-file-browser-control-placement",
            "name-area",
        );
        await expect(
            page.locator(
                `[data-file-browser-name-area-controls="${rnaseqGalleryPath}"]`,
            ),
        ).toBeVisible();
        await page.screenshot({
            fullPage: true,
            path: compactInlineScreenshotPath,
        });
        await expect(
            controls.locator(
                '[data-file-browser-control-trigger="preview-modes"]',
            ),
        ).toBeVisible();
        await expect(
            controls.locator(
                '[data-file-browser-control-trigger="file-types"]',
            ),
        ).toBeVisible();
        await expect(
            controls.locator(
                '[data-file-browser-control-current="preview-modes"]',
            ),
        ).toBeVisible();
        await expect(
            controls.locator(
                '[data-file-browser-control-current="file-types"]',
            ),
        ).toBeVisible();

        const metrics = await page.evaluate(
            ({ countPath, directoryPath }) => {
                function bySelector(selectorValue: string) {
                    return document.querySelector(selectorValue);
                }

                function rectMetrics(element: Element | null) {
                    if (!(element instanceof HTMLElement)) {
                        return null;
                    }

                    const rect = element.getBoundingClientRect();

                    return {
                        height: rect.height,
                        width: rect.width,
                        x: rect.x,
                        y: rect.y,
                    };
                }

                const row = bySelector(
                    `[data-directory-row="${CSS.escape(directoryPath)}"]`,
                );
                const button = bySelector(
                    `[data-directory-path="${CSS.escape(directoryPath)}"]`,
                );
                const controlsElement = bySelector(
                    `[data-file-browser-folder-controls="${CSS.escape(directoryPath)}"]`,
                );
                const fileButton = bySelector("[data-file-path]");
                const directoryMeta = bySelector(
                    `[data-directory-meta="${CSS.escape(countPath)}"]`,
                );
                const fileCount = bySelector(
                    `[data-directory-file-count="${CSS.escape(countPath)}"]`,
                );
                const subfolderCount = bySelector(
                    `[data-directory-subfolder-count="${CSS.escape(countPath)}"]`,
                );
                const directoryTypeSummary = bySelector(
                    "[data-directory-type-summary]",
                );
                const directoryTypeSummaryMeta = directoryTypeSummary?.closest(
                    "[data-directory-meta]",
                );
                const fileKind = fileButton?.querySelector("[data-file-kind]");

                function textStyle(element: Element | undefined) {
                    if (!(element instanceof HTMLElement)) {
                        return null;
                    }

                    const styles = window.getComputedStyle(element);

                    return {
                        fontFamily: styles.fontFamily,
                        letterSpacing: styles.letterSpacing,
                        textTransform: styles.textTransform,
                    };
                }

                return {
                    buttonRect: rectMetrics(button),
                    controlsClass: controlsElement?.className ?? "",
                    controlsRect: rectMetrics(controlsElement),
                    directoryMetaRect: rectMetrics(directoryMeta),
                    directoryMetaSeparatorCount:
                        directoryTypeSummaryMeta?.querySelectorAll(
                            "[data-file-browser-meta-separator]",
                        ).length ?? 0,
                    directoryTypeSummaryText:
                        directoryTypeSummary?.textContent?.trim() ?? "",
                    directoryTypeSummaryTextStyle:
                        textStyle(directoryTypeSummary),
                    fileClass: fileButton?.className ?? "",
                    fileKindSeparatorCount:
                        fileButton?.querySelectorAll(
                            "[data-file-browser-meta-separator]",
                        ).length ?? 0,
                    fileKindText: fileKind?.textContent?.trim() ?? "",
                    fileKindTextStyle: textStyle(fileKind),
                    fileCountRect: rectMetrics(fileCount),
                    rowClass: row?.className ?? "",
                    rowRect: rectMetrics(row),
                    subfolderCountRect: rectMetrics(subfolderCount),
                };
            },
            { countPath: rnaseqRootPath, directoryPath: rnaseqGalleryPath },
        );

        if (
            !metrics.buttonRect ||
            !metrics.controlsRect ||
            !metrics.directoryMetaRect ||
            !metrics.fileCountRect ||
            !metrics.rowRect ||
            !metrics.subfolderCountRect
        ) {
            throw new Error("Missing control-surface metrics");
        }

        expect(metrics.controlsClass).toContain("inline-nameplate-controls");
        expect(metrics.controlsClass).not.toBe(metrics.rowClass);
        expect(metrics.controlsClass).not.toBe(metrics.fileClass);
        expect(metrics.controlsRect.x).toBeGreaterThanOrEqual(
            metrics.rowRect.x - 1,
        );
        expect(
            metrics.controlsRect.x + metrics.controlsRect.width,
        ).toBeLessThanOrEqual(metrics.rowRect.x + metrics.rowRect.width + 1);
        expect(metrics.subfolderCountRect.x).toBeGreaterThan(
            metrics.fileCountRect.x,
        );
        expect(
            Math.abs(metrics.fileCountRect.y - metrics.subfolderCountRect.y),
        ).toBeLessThan(4);
        expect(metrics.directoryTypeSummaryText).toMatch(/^\d+ [A-Z0-9]+/);
        expect(metrics.directoryTypeSummaryTextStyle).toMatchObject({
            letterSpacing: "normal",
            textTransform: "none",
        });
        expect(metrics.fileKindText).toBe("output");
        expect(metrics.fileKindTextStyle).toMatchObject({
            letterSpacing: "normal",
            textTransform: "none",
        });
        expect(metrics.directoryMetaSeparatorCount).toBeGreaterThanOrEqual(3);
        expect(metrics.fileKindSeparatorCount).toBeGreaterThanOrEqual(2);

        await expect(firstFile).toBeVisible();
        await expect(
            page.locator(
                `[data-file-browser-file-matrix-header="${rnaseqGalleryPath}"]`,
            ),
        ).toHaveCount(0);
        await expect(
            page.locator(
                `[data-file-browser-sidecar-layout="${rnaseqGalleryPath}"]`,
            ),
        ).toHaveCount(0);
        await expect(
            page.locator(
                `[data-file-browser-content-ribbon="${rnaseqGalleryPath}"]`,
            ),
        ).toHaveCount(0);
    });
});
