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

    await page.getByRole("link", { name: "nf-core/rnaseq" }).first().click();

    const fileBrowser = page.locator('[data-file-browser="true"]');
    await expect(fileBrowser).toBeVisible();
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

    test("row-spans multiple file rows when directory has multiple files in single preview mode", async ({
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

        // Preview should span from the top of the first file to at least near the bottom of the last file
        expect(previewBBox.y).toBeLessThanOrEqual(firstFileBBox.y + 15);
        expect(previewBBox.y + previewBBox.height).toBeGreaterThanOrEqual(
            lastFileBBox.y + lastFileBBox.height - 15,
        );

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
