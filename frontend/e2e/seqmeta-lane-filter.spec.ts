/**
 * Regression tests for lane filtering support (bugfix 260501-4).
 *
 * Tests that the seeded nf-core/sarek sample details render a lane Filter
 * button that links to the landing page with a seqmeta_lane filter.
 */

import { expect, test } from "@playwright/test";

test.describe("Lane filtering support (bugfix 260501-4)", () => {
    test("lane rows show a Filter button that links to seqmeta_lane filter", async ({
        page,
    }) => {
        await page.goto("/");
        await expect(page.getByText("Recent registrations")).toBeVisible();
        await expect(
            page.locator('tbody tr[data-result-row="true"]'),
        ).toHaveCount(4);

        const resultLink = page
            .getByRole("link", { name: "nf-core/sarek" })
            .first();
        const href = await resultLink.getAttribute("href");

        await page.goto(href ?? "/results/");
        await expect(page).toHaveURL(new RegExp(`${href ?? "/results/"}$`));
        await expect(
            page.getByRole("heading", { level: 1, name: "nf-core/sarek" }),
        ).toBeVisible({ timeout: 30000 });

        const metadataRow = page.locator(
            '[data-metadata-row="seqmeta_sampleid"]',
        );
        await expect(metadataRow).toContainText("WTSI_wEMB10524782");

        const trigger = metadataRow.getByTestId("seqmeta-badge-trigger");
        await trigger.click();

        const dialog = page.getByRole("dialog", {
            name: /WTSI_wEMB10524782/i,
        });
        await expect(dialog).toBeVisible();
        await expect(
            dialog.getByRole("heading", { name: /WTSI_wEMB10524782/i }),
        ).toBeVisible();

        const lanesGroup = dialog.locator('[data-field-group="lanes"]');
        await expect(lanesGroup).toBeVisible();

        const firstLane = lanesGroup
            .locator('[data-seqmeta-detail-key="lane"]')
            .first();
        await expect(firstLane).toBeVisible();

        const copyButton = firstLane.getByRole("button", {
            name: /copy lane/i,
        });
        await expect(copyButton).toBeVisible();

        const filterLink = firstLane.getByRole("link", { name: /filter/i });
        await expect(filterLink).toBeVisible();
        await expect(filterLink).toHaveAttribute(
            "href",
            "/?seqmeta_lane=48522_1#1",
        );
    });
});
