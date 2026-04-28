import { expect, test } from "@playwright/test";

test.describe("File Browser single preview layout", () => {
    test("positions single preview to the right of file metadata at 1024px viewport", async ({
        page,
    }) => {
        await page.setViewportSize({ width: 1024, height: 768 });
        await page.goto("/");

        await page
            .getByRole("link", { name: "nf-core/rnaseq" })
            .first()
            .click();

        // Wait for file browser to be visible with single preview mode
        const fileBrowser = page.locator('[data-file-browser="true"]');
        await expect(fileBrowser).toBeVisible();

        // Wait for a directory with files to be expanded in single preview mode
        const singleLayoutContainer = page
            .locator("[data-file-browser-single-layout]")
            .first();
        const preview = page.locator('[data-file-browser-preview="single"]');

        // Keep clicking directory rows until we find one that shows files with preview
        let foundLayout = false;
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

            // Check if we now have a single layout container with preview
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
        await page.setViewportSize({ width: 1024, height: 768 });
        await page.goto("/");

        await page
            .getByRole("link", { name: "nf-core/rnaseq" })
            .first()
            .click();

        // Wait for file browser
        await expect(page.locator('[data-file-browser="true"]')).toBeVisible();

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
});
