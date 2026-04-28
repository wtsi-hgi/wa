import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

function recentRows(page: Page): Locator {
    return page.locator('tbody tr[data-result-row="true"]');
}

async function addRequesterFilter(
    page: Page,
    requester: string,
): Promise<void> {
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

async function selectDirectoryForFile(
    page: Page,
    filePath: string,
): Promise<void> {
    const directoryPath = path.dirname(filePath);
    const directoryButton = page
        .locator(`[data-directory-path="${directoryPath}"]`)
        .first();

    await expect(directoryButton).toBeVisible();
    await directoryButton.click();
    await expect(
        page.locator(`[data-file-path="${filePath}"]`).first(),
    ).toBeVisible();
}

async function openSeqmetaDetailsDialog(
    page: Page,
    metadataKey: string,
): Promise<Locator> {
    const metadataRow = page.locator(`[data-metadata-row="${metadataKey}"]`);
    const trigger = metadataRow.getByRole("button", {
        name: new RegExp(`Open ${metadataKey} details`, "i"),
    });

    await expect(trigger).toBeVisible();
    await trigger.click();

    const dialog = page.getByRole("dialog");

    await expect(dialog).toBeVisible();

    return dialog;
}

test.describe("Q1 critical results flows", () => {
    const rnaseqPipelineName = "nf-core/rnaseq";
    const sarekPipelineName = "nf-core/sarek";
    const ampliconPipelineName = "wtsi/amplicon";
    const fixturesRoot = path.resolve(
        process.cwd(),
        "..",
        ".docs",
        "results-web",
        "fixtures",
        "files",
    );
    const ampliconConfigPath = path.join(
        fixturesRoot,
        "amplicon",
        "config",
        "review",
        "config.json",
    );
    const rnaseqReportPath = path.join(
        fixturesRoot,
        "rnaseq",
        "reports",
        "report.csv",
    );
    const rnaseqImagePath = path.join(
        fixturesRoot,
        "rnaseq",
        "qc",
        "images",
        "image.png",
    );

    test("shows the search builder above recent registrations on the dashboard", async ({
        page,
    }) => {
        await page.goto("/");

        await expect(
            page.locator('[data-search-builder="true"]'),
        ).toBeVisible();
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
            page.locator('[data-metadata-row="seqmeta_sampleid"]'),
        ).toContainText("SANG5993");
        await expect(
            page.locator('[data-metadata-row="library"]'),
        ).toContainText("exon");
    });

    test("keeps the seqmeta dialog body scrollable when content exceeds the viewport", async ({
        page,
    }) => {
        await page.setViewportSize({ width: 720, height: 420 });
        await openResultDetail(page, rnaseqPipelineName);

        const dialog = await openSeqmetaDetailsDialog(page, "seqmeta_sampleid");
        const summaryPanel = dialog
            .getByText("Summary")
            .locator("xpath=ancestor::aside[1]");
        const scrollContainer = summaryPanel.locator("xpath=..");

        await expect(scrollContainer).toBeVisible();

        const metrics = await scrollContainer.evaluate((element) => {
            if (!(element instanceof HTMLElement)) {
                throw new Error("Expected an HTML element");
            }

            const overflowY = window.getComputedStyle(element).overflowY;
            const before = element.scrollTop;

            element.scrollTop = element.scrollHeight;

            return {
                clientHeight: element.clientHeight,
                overflowY,
                scrollHeight: element.scrollHeight,
                scrolled: element.scrollTop > before,
            };
        });

        expect(metrics.overflowY).toBe("auto");
        expect(metrics.scrollHeight).toBeGreaterThan(metrics.clientHeight);
        expect(metrics.scrolled).toBe(true);
    });

    test.fixme("shows the truncated-samples enrichment banner for partial seqmeta responses", async ({
        page,
    }) => {
        test.fixme(
            true,
            "Seqmeta enrichment e2e assertions are currently failing against the Playwright run-dev harness; revisit with later seqmeta fixture work.",
        );

        await openResultDetail(page, rnaseqPipelineName);

        await expect(
            page.locator('[data-metadata-row="seqmeta_sampleid"]'),
        ).toContainText("Showing first 1000 samples");
    });

    test.fixme("shows the impaired marker when seqmeta enrichment returns 502", async ({
        page,
    }) => {
        test.fixme(
            true,
            "Seqmeta enrichment e2e assertions are currently failing against the Playwright run-dev harness; revisit with later seqmeta fixture work.",
        );

        await openResultDetail(page, sarekPipelineName);

        await expect(
            page
                .locator('[data-metadata-row="seqmeta_sample_lims"]')
                .getByLabel("enrichment backend impaired"),
        ).toBeVisible();
    });

    test("selects the directory and reveals registered files", async ({
        page,
    }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await expect(page.locator('[data-file-browser="true"]')).toBeVisible();

        await selectDirectoryForFile(page, rnaseqReportPath);

        await expect(
            page.locator(`[data-file-path="${rnaseqReportPath}"]`),
        ).toBeVisible();
        await expect(
            page.locator(`[data-file-path="${rnaseqImagePath}"]`),
        ).toHaveCount(0);

        await selectDirectoryForFile(page, rnaseqImagePath);

        await expect(
            page.locator(`[data-file-path="${rnaseqImagePath}"]`),
        ).toBeVisible();
    });

    test("renders a CSV preview table for report outputs", async ({ page }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await selectDirectoryForFile(page, rnaseqReportPath);
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

        await selectDirectoryForFile(page, rnaseqImagePath);
        await page.locator(`[data-file-path="${rnaseqImagePath}"]`).click();

        await expect(
            page.locator('[data-selected-file-path$="image.png"]'),
        ).toBeVisible();

        const image = page.getByAltText("image.png preview");
        await expect(image).toBeVisible();
        await expect(image).toHaveAttribute("src", /\/api\/file\?/);
    });

    test("uses thumbnails in grid mode and opens the full-size image in the lightbox", async ({
        page,
    }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await selectDirectoryForFile(page, rnaseqImagePath);
        await page.getByLabel("Preview first 100 files").check();

        const thumbnailButton = page.getByRole("button", {
            name: "Open image lightbox",
        });
        const thumbnailImage = page.getByAltText("image.png preview");

        await expect(page.getByText("Click to enlarge")).toBeVisible();
        await expect(thumbnailImage).toHaveAttribute("src", /thumb=true/);

        await thumbnailButton.click();

        const fullSizeImage = page.getByAltText("image.png full preview");

        await expect(
            page.getByRole("dialog", { name: "Image preview lightbox" }),
        ).toBeVisible();
        await expect(fullSizeImage).toHaveAttribute("src", /\/api\/file\?/);
        await expect(fullSizeImage).not.toHaveAttribute("src", /thumb=true/);
    });

    test("renders seeded JSON file content after the loading state clears", async ({
        page,
    }) => {
        await openResultDetail(page, ampliconPipelineName);

        await selectDirectoryForFile(page, ampliconConfigPath);

        const preview = page.locator(
            '[data-selected-file-path$="config.json"]',
        );
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
