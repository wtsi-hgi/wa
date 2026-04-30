import { expect, test } from "@playwright/test";

test.describe("bug 2 result ID copy chip", () => {
    test("renders a pointer cursor for the clickable copy affordance", async ({
        page,
    }) => {
        await page.goto("/");
        await expect(page.getByText("Recent registrations")).toBeVisible();
        await expect(
            page.locator('tbody tr[data-result-row="true"]'),
        ).toHaveCount(3);

        const resultLink = page
            .getByRole("link", { name: "nf-core/rnaseq" })
            .first();
        const href = await resultLink.getAttribute("href");

        await page.goto(href ?? "/results/");
        await expect(page).toHaveURL(new RegExp(`${href ?? "/results/"}$`));
        await expect(
            page.getByRole("heading", { level: 1, name: "nf-core/rnaseq" }),
        ).toBeVisible({ timeout: 30000 });

        const copyButton = page.locator("[data-result-id-copy]").first();

        await expect(copyButton).toBeVisible({ timeout: 30000 });
        await expect(copyButton).toHaveCSS("cursor", "pointer");
    });
});
