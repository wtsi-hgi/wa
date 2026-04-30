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

async function openResultFileBrowser(page: Page) {
    await page.setViewportSize({ width: 1024, height: 768 });
    await page.goto("/");
    await expect(page.getByText("Recent registrations")).toBeVisible();
    await expect(page.locator('tbody tr[data-result-row="true"]')).toHaveCount(
        3,
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

async function openFirstSinglePreview(page: Page) {
    const singleLayoutContainer = page
        .locator("[data-file-browser-single-layout]")
        .first();
    const preview = page.locator('[data-file-browser-preview="single"]');

    let foundLayout = false;
    for (let attempt = 0; attempt < 10; attempt += 1) {
        const dirButtons = await page
            .locator('[data-directory-path][data-directory-expanded="false"]')
            .all();

        if (dirButtons.length === 0) {
            break;
        }

        await dirButtons[0].click();

        if (
            (await singleLayoutContainer.count()) > 0 &&
            (await preview.count()) > 0
        ) {
            foundLayout = true;
            break;
        }
    }

    expect(foundLayout).toBe(true);
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

test.describe("File Browser single preview layout", () => {
    const fixturesRoot = path.resolve(
        process.cwd(),
        "..",
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
    const rnaseqGalleryLowerImagePath = path.join(
        rnaseqGalleryPath,
        "plot-080.png",
    );

    test("positions single preview to the right of file metadata at 1024px viewport", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        const { preview, singleLayoutContainer } =
            await openFirstSinglePreview(page);

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

        // Navigate directories until we find one with multiple files
        let foundMultiFileLayout = false;
        for (let attempt = 0; attempt < 10; attempt += 1) {
            const dirButtons = await page
                .locator(
                    '[data-directory-path][data-directory-expanded="false"]',
                )
                .all();

            if (dirButtons.length === 0) {
                break;
            }

            await dirButtons[0].click();

            const singleLayoutContainer = page
                .locator("[data-file-browser-single-layout]")
                .first();
            const fileButtons = singleLayoutContainer.locator(
                "button[data-file-path]",
            );

            const fileCount = await fileButtons.count();

            if (fileCount > 1) {
                foundMultiFileLayout = true;
                break;
            }
        }

        expect(foundMultiFileLayout).toBe(true);

        const singleLayoutContainer = page
            .locator("[data-file-browser-single-layout]")
            .first();
        const preview = page.locator('[data-file-browser-preview="single"]');
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

    test("renders the single preview as one bordered surface filling the preview area", async ({
        page,
    }) => {
        await openResultFileBrowser(page);
        const { preview } = await openFirstSinglePreview(page);

        const metrics = await measurePreviewBorderSurfaces(preview);

        expect(metrics.surfaces).toHaveLength(1);

        const [surface] = metrics.surfaces;

        expect(surface.x).toBeCloseTo(metrics.root.x, 1);
        expect(surface.y).toBeCloseTo(metrics.root.y, 1);
        expect(surface.width).toBeGreaterThanOrEqual(metrics.root.width - 2);
        expect(surface.height).toBeGreaterThanOrEqual(metrics.root.height - 2);
    });
});
