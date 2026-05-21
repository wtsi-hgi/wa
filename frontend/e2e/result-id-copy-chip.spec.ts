import { expect, test } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

test.describe("result detail title identity", () => {
    test("keeps the old copy chip out of the result detail title", async ({
        page,
    }) => {
        await page.goto("/");
        await expect(page.getByText("Recent registrations")).toBeVisible();
        await expect(
            page
                .locator('tbody tr[data-result-row="true"]')
                .filter({ hasNotText: "seqmeta/rendering-repro" }),
        ).toHaveCount(4);

        const resultLink = page
            .getByRole("link", { name: "nf-core/rnaseq" })
            .first();
        const href = await resultLink.getAttribute("href");

        await page.goto(href ?? "/results/");
        await expect(page).toHaveURL(new RegExp(`${href ?? "/results/"}$`));
        await expect(
            page.getByRole("heading", { level: 1, name: "nf-core/rnaseq" }),
        ).toBeVisible({ timeout: 30000 });

        await expect(page.locator("[data-result-id-copy]")).toHaveCount(0);
    });
});
