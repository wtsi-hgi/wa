import path from "node:path";

import { expect, type Locator, type Page } from "@playwright/test";

export function recentRows(page: Page): Locator {
    return page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasNotText: "seqmeta/rendering-repro" });
}

export async function expectRecentRowsLoaded(page: Page): Promise<void> {
    await expect.poll(async () => recentRows(page).count()).toBeGreaterThan(0);
}

export async function openResultDetail(
    page: Page,
    pipelineName: string,
): Promise<void> {
    await page.goto("/");
    await expect(page.getByText("Latest result sets")).toBeVisible();
    await expectRecentRowsLoaded(page);

    const resultLink = page.getByRole("link", { name: pipelineName }).first();
    const href = await resultLink.getAttribute("href");
    const detailUrl = new URL(href ?? "/results/", page.url()).toString();

    await page.goto(detailUrl);
    await expect(page).toHaveURL(detailUrl);
    await expect(
        page.getByRole("heading", { level: 1, name: pipelineName }),
    ).toBeVisible({ timeout: 30000 });
}

export async function selectDirectoryForFile(
    page: Page,
    filePath: string,
): Promise<void> {
    const directoryPath = path.dirname(filePath);

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
            await expect(
                page.locator(`[data-file-path="${filePath}"]`).first(),
            ).toBeVisible();

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

        await expect
            .poll(async () => {
                const descendantCount = await page
                    .locator("[data-directory-path]")
                    .evaluateAll(
                        (elements, prefix) =>
                            elements.filter((element) => {
                                const value = element.getAttribute(
                                    "data-directory-path",
                                );

                                return (
                                    typeof value === "string" &&
                                    value.startsWith(`${prefix}/`)
                                );
                            }).length,
                        nextPath,
                    );

                const fileCount = await page
                    .locator(`[data-file-path="${filePath}"]`)
                    .count();

                return descendantCount + fileCount;
            })
            .toBeGreaterThan(0);
    }

    const directoryButton = page
        .locator(`[data-directory-path="${directoryPath}"]`)
        .first();

    await directoryButton.scrollIntoViewIfNeeded();
    await expect(directoryButton).toBeVisible();
    await directoryButton.click();
    await expect(
        page.locator(`[data-file-path="${filePath}"]`).first(),
    ).toBeVisible();
}
