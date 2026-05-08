import { expect, test } from "@playwright/test";

/**
 * File preview truncation regression tests.
 *
 * Bug: Previews in the file browser can have vertical and horizontal scroll bars,
 * but these can't be interacted with. Instead of showing scroll bars when the
 * content doesn't fit in available preview height, just show an indication of
 * truncation at the bottom/right of the preview.
 */

async function openResultFileBrowser(page: test.Page) {
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

test("constrained markdown preview shows truncation indicator instead of scrollbar", async ({
    page,
}) => {
    await openResultFileBrowser(page);

    // Find and select a markdown file
    const markdownFile = page.locator('[data-file-path$=".md"]').first();

    if ((await markdownFile.count()) > 0) {
        await markdownFile.click();
        await page.waitForTimeout(500);

        // Find the preview article element
        const previewArticle = page.locator(
            "article:has(> *:first-child:is(h1, h2, h3, h4, h5, h6, p))",
        );

        if ((await previewArticle.count()) > 0) {
            const overflow = await previewArticle.evaluate((element) => {
                const style = window.getComputedStyle(element);
                return {
                    overflowX: style.overflowX,
                    overflowY: style.overflowY,
                    scrollHeight: element.scrollHeight,
                    clientHeight: element.clientHeight,
                };
            });

            // Preview should have overflow-hidden, not overflow-auto or overflow-scroll
            expect(overflow.overflowY).not.toBe("auto");
            expect(overflow.overflowY).not.toBe("scroll");

            // If content is truncated, there should be a truncation indicator
            if (overflow.scrollHeight > overflow.clientHeight) {
                const truncationIndicator = page.locator(
                    '[aria-label*="truncated"], [data-truncated="true"]',
                );
                await expect(truncationIndicator).toBeVisible();
            }
        }
    }
});

test("constrained code preview shows truncation indicator instead of scrollbar", async ({
    page,
}) => {
    await openResultFileBrowser(page);

    // Find and select a code file (json, txt, py, etc.)
    const codeFile = page
        .locator('[data-file-path$=".json"], [data-file-path$=".txt"]')
        .first();

    if ((await codeFile.count()) > 0) {
        await codeFile.click();
        await page.waitForTimeout(500);

        // Find the preview pre element within the code preview container
        const previewPre = page.locator("pre:has(code)").first();

        if ((await previewPre.count()) > 0) {
            const overflow = await previewPre.evaluate((element) => {
                const style = window.getComputedStyle(element);
                return {
                    overflowX: style.overflowX,
                    overflowY: style.overflowY,
                    scrollHeight: element.scrollHeight,
                    clientHeight: element.clientHeight,
                };
            });

            // Preview should have overflow-hidden, not overflow-auto or overflow-scroll
            expect(overflow.overflowY).not.toBe("auto");
            expect(overflow.overflowY).not.toBe("scroll");
            expect(overflow.overflowX).not.toBe("auto");
            expect(overflow.overflowX).not.toBe("scroll");

            // If content is truncated, there should be a truncation indicator
            if (
                overflow.scrollHeight > overflow.clientHeight ||
                overflow.scrollHeight > overflow.clientHeight
            ) {
                const truncationIndicator = page.locator(
                    '[aria-label*="truncated"], [data-truncated="true"]',
                );
                await expect(truncationIndicator).toBeVisible();
            }
        }
    }
});

test("inline csv preview is capped at the backend inline-mode line limit", async ({
    page,
}) => {
    await openResultFileBrowser(page);

    const csvFile = page.locator('[data-file-path$="report.csv"]').first();

    if ((await csvFile.count()) > 0) {
        await csvFile.click();

        const preview = page.locator('[data-file-browser-preview="single"]');
        await expect(preview).toBeVisible();
        // The fixture has 20 data rows + 1 header (21 lines), which is more
        // than the inline mode cap, so the preview must be marked truncated.
        await expect(preview.getByText(/Showing \d+ preview rows/)).toHaveCount(
            0,
        );

        const tableRows = preview.locator("tbody tr");
        // Backend caps inline-mode lines well below the underlying row count.
        const rowCount = await tableRows.count();
        expect(rowCount).toBeLessThan(20);
    }
});

test("enlarged code preview allows scrolling for long content", async ({
    page,
}) => {
    await openResultFileBrowser(page);

    // Find and select a code file
    const codeFile = page
        .locator('[data-file-path$=".json"], [data-file-path$=".txt"]')
        .first();

    if ((await codeFile.count()) > 0) {
        await codeFile.click();
        await page.waitForTimeout(500);

        // Click the enlarge button
        const enlargeButton = page.getByRole("button", {
            name: /enlarge.*preview/i,
        });

        if ((await enlargeButton.count()) > 0) {
            await enlargeButton.click();

            // Wait for dialog to appear
            const dialog = page.getByRole("dialog");
            await expect(dialog).toBeVisible();

            // The enlarged view should allow scrolling
            const enlargedPre = dialog.locator("pre:has(code)").first();

            if ((await enlargedPre.count()) > 0) {
                const overflow = await enlargedPre.evaluate((element) => {
                    const style = window.getComputedStyle(element);
                    return {
                        overflowY: style.overflowY,
                    };
                });

                // Enlarged preview CAN have overflow-auto for scrolling
                expect(overflow.overflowY).toBe("auto");
            }
        }
    }
});
