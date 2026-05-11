import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

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

type GroupedShellVisualMetrics = {
    childGroupRect: {
        height: number;
        width: number;
        x: number;
        y: number;
    } | null;
    connectorElbow: {
        borderBottomStyle: string;
        borderBottomWidth: number;
        borderLeftStyle: string;
        borderLeftWidth: number;
        height: number;
        width: number;
    } | null;
    connectorRail: {
        backgroundImage: string;
        height: number;
        width: number;
    } | null;
    fileRowsRect: {
        height: number;
        width: number;
        x: number;
        y: number;
    } | null;
    shellRect: { height: number; width: number; x: number; y: number } | null;
    shellSurface: {
        backgroundImage: string;
        borderStyle: string;
        borderWidth: number;
    } | null;
};

async function openResultFileBrowser(page: Page) {
    await page.setViewportSize({ width: 1024, height: 768 });
    await page.goto("/");
    await expect(page.getByText("Recent registrations")).toBeVisible();
    await expect(page.locator('tbody tr[data-result-row="true"]')).toHaveCount(
        4,
    );

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
    await expect(page.getByText("Recent registrations")).toBeVisible();
    await expect(page.locator('tbody tr[data-result-row="true"]')).toHaveCount(
        4,
    );

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

async function measureGroupedShellVisuals(
    groupContent: Locator,
): Promise<GroupedShellVisualMetrics> {
    return groupContent.evaluate((root) => {
        const [connectorElbow, connectorRail, shellSurface] = Array.from(
            root.children,
        );
        const fileRows = root.querySelector(
            "[data-file-browser-directory-files]",
        );
        const childGroup = root.querySelector("[data-directory-group]");

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

        function borderMetrics(element: Element | null) {
            if (!(element instanceof HTMLElement)) {
                return null;
            }

            const styles = window.getComputedStyle(element);
            const rect = element.getBoundingClientRect();

            return {
                borderBottomStyle: styles.borderBottomStyle,
                borderBottomWidth: Number.parseFloat(styles.borderBottomWidth),
                borderLeftStyle: styles.borderLeftStyle,
                borderLeftWidth: Number.parseFloat(styles.borderLeftWidth),
                height: rect.height,
                width: rect.width,
            };
        }

        function shellSurfaceMetrics(element: Element | null) {
            if (!(element instanceof HTMLElement)) {
                return null;
            }

            const styles = window.getComputedStyle(element);

            return {
                backgroundImage: styles.backgroundImage,
                borderStyle: styles.borderTopStyle,
                borderWidth: Number.parseFloat(styles.borderTopWidth),
            };
        }

        function railMetrics(element: Element | null) {
            if (!(element instanceof HTMLElement)) {
                return null;
            }

            const styles = window.getComputedStyle(element);
            const rect = element.getBoundingClientRect();

            return {
                backgroundImage: styles.backgroundImage,
                height: rect.height,
                width: rect.width,
            };
        }

        return {
            childGroupRect: rectMetrics(childGroup),
            connectorElbow: borderMetrics(connectorElbow ?? null),
            connectorRail: railMetrics(connectorRail ?? null),
            fileRowsRect: rectMetrics(fileRows),
            shellRect: rectMetrics(root),
            shellSurface: shellSurfaceMetrics(shellSurface ?? null),
        };
    });
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
    const rnaseqRootPath = path.join(fixturesRoot, "rnaseq");
    const rnaseqImagesPath = path.join(fixturesRoot, "rnaseq", "qc", "images");
    const rnaseqGalleryPath = path.join(
        fixturesRoot,
        "rnaseq",
        "qc",
        "images",
        "gallery",
    );
    const rnaseqNotesPath = path.join(fixturesRoot, "rnaseq", "qc", "notes");
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
    const galleriesDemoLane1NotesPath = path.join(
        galleriesDemoSampleALanesPath,
        "lane-1",
        "lane-1-notes.tsv",
    );

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

    test("renders a visible grouped shell around an expanded nested folder's files and child folders", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqImagesPath);

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

        const metrics = await measureGroupedShellVisuals(groupContent);

        if (
            !metrics.shellRect ||
            !metrics.shellSurface ||
            !metrics.connectorElbow ||
            !metrics.connectorRail ||
            !metrics.fileRowsRect ||
            !metrics.childGroupRect
        ) {
            throw new Error(
                "Missing grouped shell metrics for browser assertion",
            );
        }

        expect(metrics.shellSurface.borderWidth).toBeGreaterThan(0);
        expect(metrics.shellSurface.borderStyle).not.toBe("none");
        expect(metrics.shellSurface.backgroundImage).not.toBe("none");

        expect(metrics.connectorElbow.borderLeftWidth).toBe(0);
        expect(metrics.connectorElbow.borderBottomWidth).toBe(0);
        expect(metrics.connectorElbow.width).toBe(0);
        expect(metrics.connectorElbow.height).toBe(0);

        expect(metrics.connectorRail.width).toBeLessThanOrEqual(2);
        expect(metrics.connectorRail.height).toBeGreaterThan(24);
        expect(metrics.connectorRail.backgroundImage).not.toBe("none");

        expect(metrics.fileRowsRect.x).toBeGreaterThan(metrics.shellRect.x);
        expect(metrics.fileRowsRect.y).toBeGreaterThan(metrics.shellRect.y);
        expect(
            metrics.fileRowsRect.x + metrics.fileRowsRect.width,
        ).toBeLessThanOrEqual(metrics.shellRect.x + metrics.shellRect.width);

        expect(metrics.childGroupRect.x).toBeGreaterThan(metrics.shellRect.x);
        expect(metrics.childGroupRect.y).toBeGreaterThan(
            metrics.fileRowsRect.y,
        );
        expect(
            metrics.childGroupRect.x + metrics.childGroupRect.width,
        ).toBeLessThanOrEqual(metrics.shellRect.x + metrics.shellRect.width);

        expect(
            metrics.shellRect.width - metrics.fileRowsRect.width,
        ).toBeLessThan(96);
    });

    test("places folder controls beneath the directory heading while keeping compact widgets on the same row surface", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        await selectDirectory(page, rnaseqGalleryPath);

        const filePreviewFolderControls = page
            .locator(
                `[data-file-browser-folder-controls="${rnaseqGalleryPath}"]`,
            )
            .filter({
                has: page.locator('input[aria-label="1 preview per row"]'),
            });

        const folderControls = filePreviewFolderControls.first();
        const directoryRow = page.locator("[data-directory-row]").filter({
            has: folderControls,
        });
        const directoryButton = directoryRow
            .locator("button[data-directory-path]")
            .first();

        await expect(directoryRow).toBeVisible();
        await expect(directoryButton).toBeVisible();
        await expect(folderControls).toBeVisible();
        await openPreviewModes(folderControls);

        // Verify "1 preview per row" toggle is present
        const previewModeToggle = folderControls.locator(
            'input[aria-label="1 preview per row"]',
        );
        await expect(previewModeToggle).toBeVisible();

        // Verify preview height slider is present in the same controls container
        const previewHeightSlider = folderControls.locator(
            'input[type="range"][aria-label="Preview height"]',
        );
        await expect(previewHeightSlider).toBeVisible();

        // Get bounding boxes to verify they're on the same horizontal line
        const toggleBBox = await previewModeToggle.boundingBox();
        const sliderBBox = await previewHeightSlider.boundingBox();
        const controlsBBox = await folderControls.boundingBox();
        const buttonBBox = await directoryButton.boundingBox();
        const rowBBox = await directoryRow.boundingBox();

        if (
            !toggleBBox ||
            !sliderBBox ||
            !controlsBBox ||
            !buttonBBox ||
            !rowBBox
        ) {
            throw new Error("Missing bounding boxes for controls verification");
        }

        // Controls must sit below the folder heading/button, not in a reserved
        // right-hand column that steals name width.
        expect(controlsBBox.y).toBeGreaterThan(
            buttonBBox.y + buttonBBox.height - 8,
        );

        // The controls remain inside the same directory row surface.
        expect(controlsBBox.y + controlsBBox.height).toBeLessThanOrEqual(
            rowBBox.y + rowBBox.height + 1,
        );
        expect(controlsBBox.x).toBeGreaterThanOrEqual(rowBBox.x);
        expect(controlsBBox.x + controlsBBox.width).toBeLessThanOrEqual(
            rowBBox.x + rowBBox.width + 1,
        );

        // Both controls should be in the same container row (similar vertical position)
        expect(Math.abs(toggleBBox.y - sliderBBox.y)).toBeLessThan(30);

        // The slider should be compact (height similar to the toggle's parent label)
        const toggleLabel = page
            .locator('label:has(input[aria-label="1 preview per row"])')
            .first();
        const toggleLabelBBox = await toggleLabel.boundingBox();

        if (!toggleLabelBBox) {
            throw new Error("Missing toggle label bounding box");
        }

        // Slider height should be compact - less than 80px total (including padding)
        expect(sliderBBox.height).toBeLessThan(80);

        // Controls should be arranged horizontally within the folder controls container
        expect(controlsBBox.height).toBeLessThan(120);
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
                    cardButton?.parentElement ===
                    cardImage?.parentElement?.parentElement,
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

    test("shows nested eligible subfolder preview controls on both the parent and child rows", async ({
        page,
    }) => {
        await openNamedResultFileBrowser(page, "wtsi/galleries-demo");

        await selectDirectory(page, galleriesDemoSampleAPath);

        const sampleAControls = page.locator(
            `[data-subdir-preview-controls="${galleriesDemoSampleAPath}"]`,
        );

        await expect(sampleAControls).toBeVisible();
        await expect(
            page.locator(
                `[data-file-browser-folder-controls="${galleriesDemoSampleAPath}"] input[aria-label="1 preview per row"]`,
            ),
        ).toHaveCount(1);

        await selectDirectory(page, galleriesDemoSampleALanesPath);

        const lanesControls = page.locator(
            `[data-subdir-preview-controls="${galleriesDemoSampleALanesPath}"]`,
        );

        await expect(sampleAControls).toBeVisible();
        await expect(lanesControls).toBeVisible();
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

        await expect(sampleAControls).toBeVisible();
        await expect(lanesControls).toBeVisible();
        await expect(sampleASummary).toContainText("Grid + subfolders");
        await expect(
            sampleAControls.locator('input[aria-label="1 preview per row"]'),
        ).toHaveCount(1);
        await expect(lanesSummary).toContainText("Single preview");
        await expect(
            lanesControls.locator('input[aria-label="1 preview per row"]'),
        ).toHaveCount(0);

        await openPreviewModes(sampleAControls);
        await sampleAControls
            .locator('input[aria-label="1 preview per row"]')
            .click();

        await expect(sampleASummary).toContainText("Subfolders");
        await expect(
            sampleAControls.locator('input[aria-label="1 preview per row"]'),
        ).toHaveCount(0);
        await expect(lanesSummary).toContainText("Single preview");
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
});
