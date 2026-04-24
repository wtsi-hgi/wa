import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

function recentRows(page: Page): Locator {
    return page.locator('tbody tr[data-result-row="true"]');
}

async function addRequesterFilter(page: Page, requester: string): Promise<void> {
    const searchBuilder = page.locator('[data-search-builder="true"]');

    await expect(searchBuilder).toBeVisible();
    await searchBuilder.getByRole("button", { name: "Add filter" }).click();

    const filterPopover = page.locator('[data-search-builder-popover="true"]');

    await expect(filterPopover).toBeVisible();
    await filterPopover.locator('[data-filter-field-option="user"]').click();
    await filterPopover
        .locator('[data-filter-value-input="user"]')
        .fill(requester);
    await filterPopover.getByRole("button", { name: "Add" }).click();
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
    const ampliconPipelineName = "wtsi/amplicon";
    const fixturesRoot = path.resolve(
        process.cwd(),
        "..",
        ".docs",
        "results-web",
        "fixtures",
        "files",
    );
    const ampliconConfigPath = path.join(fixturesRoot, "config.json");
    const rnaseqReportPath = path.join(fixturesRoot, "report.csv");
    const rnaseqImagePath = path.join(fixturesRoot, "image.png");

    test("shows the search builder above recent registrations on the dashboard", async ({
        page,
    }) => {
        await page.goto("/");

        await expect(page.locator('[data-search-builder="true"]')).toBeVisible();
        await expect(
            page.getByRole("heading", { level: 2, name: "Latest result sets" }),
        ).toBeVisible();
        await expect(page.getByText("Recent registrations")).toBeVisible();
        await expect(page.locator('[data-stat-card="total"]')).toHaveCount(0);

        const rows = recentRows(page);
        await expect(rows).toHaveCount(3);
    });

    test("filters results by requester through the search builder", async ({
        page,
    }) => {
        await page.goto("/");

        await addRequesterFilter(page, "alice");

        await expect(page).toHaveURL(/\?user=alice/);
        await expect(page.getByText("Showing search results")).toBeVisible();

        const rows = recentRows(page);
        await expect(rows).toHaveCount(1);
        await expect(rows.first()).toContainText("alice");
        await expect(rows.first()).not.toContainText("carol");
        await expect(rows.first()).not.toContainText("erin");
    });

    test("returns to the dashboard state that opened a result detail", async ({
        page,
    }) => {
        await page.goto("/");

        const recentResultLink = page
            .getByRole("link", { name: rnaseqPipelineName })
            .first();

        await recentResultLink.click();

        const backToDashboard = page.getByRole("link", {
            name: "Back to dashboard",
        });

        await expect(backToDashboard).toBeVisible();
        await backToDashboard.click();

        await expect(page).toHaveURL(/\/$/);
        await expect(page.getByText("Recent registrations")).toBeVisible();
        await expect(recentRows(page)).toHaveCount(3);

        await addRequesterFilter(page, "alice");
        await expect(page).toHaveURL(/\?user=alice/);

        const searchResultLink = page
            .getByRole("link", { name: rnaseqPipelineName })
            .first();

        await searchResultLink.click();

        const backToSearch = page.getByRole("link", {
            name: "Back to search results",
        });

        await expect(page).toHaveURL(/\/results\/[^?]+\?returnTo=/);
        await expect(backToSearch).toHaveAttribute("href", "/?user=alice");

        await backToSearch.click();

        await expect(page).toHaveURL(/\?user=alice$/);
        await expect(page.getByText("Showing search results")).toBeVisible();
        await expect(recentRows(page)).toHaveCount(1);
        await expect(recentRows(page).first()).toContainText("alice");
    });

    test("navigates to result detail and shows registration metadata", async ({
        page,
    }) => {
        await openResultDetail(page, rnaseqPipelineName);
        await expect(
            page.getByRole("heading", { level: 1, name: rnaseqPipelineName }),
        ).toBeVisible();
        await expect(
            page.locator('[data-metadata-row="seqmeta_studyid"]'),
        ).toContainText("5993");
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

    test("renders seeded JSON file content after the loading state clears", async ({
        page,
    }) => {
        await openResultDetail(page, ampliconPipelineName);

        await page.getByRole("tab", { name: "Outputs" }).click();
        await expandFoldersUntilVisible(page, ampliconConfigPath);

        const preview = page.locator('[data-selected-file-path$="config.json"]');
        const selectedFile = page.locator(
            `[data-file-path="${ampliconConfigPath}"]`,
        );

        await expect(preview).toBeVisible();
        await expect(
            preview.getByText("Syntax-highlighted preview"),
        ).toBeVisible();
        await expect(preview.getByText(/"panel"/)).toBeVisible();
        await expect(preview.getByText(/"haem"/)).toBeVisible();

        await selectedFile.click();

        await expect(preview.getByText("Loading preview...")).toHaveCount(0);
        await expect(
            preview.getByText("Syntax-highlighted preview"),
        ).toBeVisible();
    });
});
