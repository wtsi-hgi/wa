import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

function parseInteger(value: string | null): number {
    const numeric = Number.parseInt(value?.trim() ?? "", 10);

    if (Number.isNaN(numeric)) {
        throw new Error(`Expected an integer but received: ${value ?? "<null>"}`);
    }

    return numeric;
}

function recentRows(page: Page): Locator {
    return page.locator('tbody tr[data-result-row="true"]');
}

async function openResultDetail(
    page: Page,
    pipelineName: string,
): Promise<void> {
    await page.goto("/");

    const resultLink = page.getByRole("link", { name: pipelineName }).first();
    const href = await resultLink.getAttribute("href");

    await resultLink.click();
    await expect(page).toHaveURL(new RegExp(`${href ?? "/results/"}$`));
}

async function expandFoldersUntilVisible(
    page: Page,
    filePath: string,
): Promise<void> {
    const outputsPanel = page.getByRole("tabpanel", { name: "Outputs" });
    await expect(outputsPanel).toBeVisible();

    const normalizedPath = filePath.replace(/^\/+/, "");
    const segments = normalizedPath.split("/").filter(Boolean);
    const target = outputsPanel.locator(`[data-file-path="/${normalizedPath}"]`);

    let currentPath = "";

    for (const segment of segments.slice(0, -1)) {
        currentPath = `${currentPath}/${segment}`;
        const folder = outputsPanel
            .locator(`[data-folder-path="${currentPath}"]`)
            .first();

        if ((await folder.count()) === 0) {
            continue;
        }

        await expect(folder).toBeVisible();
        const expanded = await folder.getAttribute("aria-expanded");
        if (expanded !== "true") {
            await folder.click({ position: { x: 24, y: 20 } });
            await expect(folder).toHaveAttribute("aria-expanded", "true");
        }
    }

    await expect(target.first()).toBeVisible();
}

test.describe("Q1 critical results flows", () => {
    const rnaseqPipelineName = "nf-core/rnaseq";
    const fixturesRoot = path.resolve(
        process.cwd(),
        "..",
        ".docs",
        "results-web",
        "fixtures",
        "files",
    );
    const rnaseqReportPath = path.join(fixturesRoot, "report.csv");
    const rnaseqImagePath = path.join(fixturesRoot, "image.png");

    test("shows seeded stats and recent rows on the dashboard", async ({
        page,
    }) => {
        await page.goto("/");

        await expect(page.locator('[data-stat-card="total"]')).toBeVisible();

        const total = parseInteger(
            await page.locator('[data-stat-card="total"]').textContent(),
        );
        expect(total).toBeGreaterThanOrEqual(3);

        const rows = recentRows(page);
        await expect(rows).toHaveCount(3);
    });

    test("filters results by requester through the search builder", async ({
        page,
    }) => {
        await page.goto("/");

        const searchBuilder = page.locator('[data-search-builder="true"]');
        await expect(searchBuilder).toBeVisible();

        await searchBuilder.getByRole("button", { name: "Add filter" }).click();

        const filterPopover = page.locator('[data-search-builder-popover="true"]');

        await expect(filterPopover).toBeVisible();
        await filterPopover.locator('[data-filter-field-option="user"]').click();
        await filterPopover
            .locator('[data-filter-value-input="user"]')
            .fill("alice");
        await filterPopover.getByRole("button", { name: "Add" }).click();

        await expect(page).toHaveURL(/\?user=alice/);
        await expect(page.getByText("Showing search results")).toBeVisible();

        const rows = recentRows(page);
        await expect(rows).toHaveCount(1);
        await expect(rows.first()).toContainText("alice");
        await expect(rows.first()).not.toContainText("carol");
        await expect(rows.first()).not.toContainText("erin");
    });

    test("navigates to result detail and shows registration metadata", async ({
        page,
    }) => {
        await openResultDetail(page, rnaseqPipelineName);
        await expect(
            page.getByRole("heading", { level: 1, name: rnaseqPipelineName }),
        ).toBeVisible();
        await expect(
            page.locator('[data-metadata-row="seqmeta_sampleid"]'),
        ).toContainText("SANG001");
        await expect(page.locator('[data-metadata-row="library"]')).toContainText(
            "exon",
        );
    });

    test("expands the outputs tree and reveals registered files", async ({
        page,
    }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await page.getByRole("tab", { name: "Outputs" }).click();
        await expandFoldersUntilVisible(page, rnaseqReportPath);

        await expect(
            page.locator(`[data-file-path="${rnaseqReportPath}"]`),
        ).toBeVisible();
        await expect(
            page.locator(`[data-file-path="${rnaseqImagePath}"]`),
        ).toBeVisible();
    });

    test("renders a CSV preview table for report outputs", async ({ page }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await page.getByRole("tab", { name: "Outputs" }).click();
        await expandFoldersUntilVisible(page, rnaseqReportPath);
        await page.locator(`[data-file-path="${rnaseqReportPath}"]`).click();

        const preview = page.locator('[data-selected-file-path$="report.csv"]');

        await expect(preview).toBeVisible();
        await expect(
            preview.getByRole("button", { name: "Sort by sample_id" }),
        ).toBeVisible();
        await expect(
            preview.getByRole("button", { name: "Sort by metric" }),
        ).toBeVisible();
        await expect(
            preview.getByRole("button", { name: "Sort by value" }),
        ).toBeVisible();
        await expect(
            preview.getByRole("button", { name: "Sort by status" }),
        ).toBeVisible();
    });

    test("renders a PNG preview image for image outputs", async ({ page }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await page.getByRole("tab", { name: "Outputs" }).click();
        await expandFoldersUntilVisible(page, rnaseqImagePath);
        await page.locator(`[data-file-path="${rnaseqImagePath}"]`).click();

        await expect(
            page.locator('[data-selected-file-path$="image.png"]'),
        ).toBeVisible();

        const image = page.getByAltText("image.png preview");
        await expect(image).toBeVisible();
        await expect(image).toHaveAttribute("src", /\/api\/file\?/);
    });
});
