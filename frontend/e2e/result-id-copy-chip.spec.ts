import { expect, test } from "@playwright/test";

test.describe("bug 2 result ID copy chip", () => {
    test("renders a pointer cursor for the clickable copy affordance", async ({
        page,
    }) => {
        await page.goto("/");

        await page
            .getByRole("link", { name: "nf-core/rnaseq" })
            .first()
            .click();

        const copyButton = page.locator("[data-result-id-copy]").first();

        await expect(copyButton).toBeVisible();
        await expect(copyButton).toHaveCSS("cursor", "pointer");
    });
});
