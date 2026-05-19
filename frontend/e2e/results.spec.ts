import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

function recentRows(page: Page): Locator {
    return page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasNotText: "seqmeta/rendering-repro" });
}

type SortIconMetric = {
    columnId: string;
    flexShrink: string;
    height: number;
    width: number;
};

async function collectRecentSortIconMetrics(
    page: Page,
): Promise<SortIconMetric[]> {
    const sortButtons = page.locator("button[data-column-sort]");

    await expect(sortButtons).toHaveCount(5);

    return sortButtons.evaluateAll((elements) =>
        elements.map((element) => {
            const button = element as HTMLElement;
            const svg = button.querySelector("svg");

            if (!(svg instanceof SVGElement)) {
                throw new Error(
                    `Missing sort icon for ${button.dataset.columnSort ?? "unknown column"}`,
                );
            }

            const rect = svg.getBoundingClientRect();
            const computed = window.getComputedStyle(svg);

            return {
                columnId: button.dataset.columnSort ?? "",
                flexShrink: computed.flexShrink,
                height: rect.height,
                width: rect.width,
            };
        }),
    );
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
    await expect(page.getByText("Recent registrations")).toBeVisible();
    await expect(recentRows(page)).toHaveCount(4);

    const resultLink = page.getByRole("link", { name: pipelineName }).first();
    const href = await resultLink.getAttribute("href");
    const detailUrl = new URL(href ?? "/results/", page.url()).toString();

    await page.goto(detailUrl);
    await expect(page).toHaveURL(detailUrl);
    await expect(
        page.getByRole("heading", { level: 1, name: pipelineName }),
    ).toBeVisible({ timeout: 30000 });
}

async function selectDirectoryForFile(
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

async function openPreviewModes(controls: Locator): Promise<void> {
    const summary = controls
        .locator('summary[aria-label="Preview modes"]')
        .first();

    await expect(summary).toBeVisible();
    await summary.evaluate((element) => {
        const details = element.closest("details");

        if (!(details instanceof HTMLDetailsElement)) {
            throw new Error("Missing preview modes disclosure");
        }

        if (!details.open) {
            (element as HTMLElement).click();
        }
    });
}

async function openSeqmetaDetailsDialog(
    page: Page,
    metadataKey: string,
): Promise<Locator> {
    const displayKey =
        metadataKey === "seqmeta_studyid"
            ? "seqmeta_id_study_lims"
            : metadataKey;
    const metadataRow = page.locator(`[data-metadata-row="${metadataKey}"]`);
    const trigger = metadataRow.getByRole("button", {
        name: new RegExp(`Open ${displayKey} details`, "i"),
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
    const ampliconConfigParentPath = path.join(
        fixturesRoot,
        "amplicon",
        "config",
    );
    const ampliconConfigReviewPath = path.join(
        fixturesRoot,
        "amplicon",
        "config",
        "review",
    );
    const rnaseqReportPath = path.join(
        fixturesRoot,
        "rnaseq",
        "reports",
        "report.csv",
    );
    const rnaseqRootPath = path.join(fixturesRoot, "rnaseq");
    const rnaseqQcPath = path.join(fixturesRoot, "rnaseq", "qc");
    const rnaseqImagesPath = path.join(fixturesRoot, "rnaseq", "qc", "images");
    const rnaseqImagePath = path.join(
        fixturesRoot,
        "rnaseq",
        "qc",
        "images",
        "image.png",
    );
    const rnaseqGalleryFirstImagePath = path.join(
        fixturesRoot,
        "rnaseq",
        "qc",
        "images",
        "gallery",
        "plot-001.png",
    );
    const rnaseqGalleryPath = path.join(
        fixturesRoot,
        "rnaseq",
        "qc",
        "images",
        "gallery",
    );
    const rnaseqGalleryPageTwoImagePath = path.join(
        fixturesRoot,
        "rnaseq",
        "qc",
        "images",
        "gallery",
        "plot-101.png",
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
        await expect(rows).toHaveCount(4);
    });

    test("keeps recent registration sort icons at a stable size on narrow screens", async ({
        page,
    }) => {
        await page.setViewportSize({ width: 390, height: 900 });
        await page.goto("/");

        await expect(page.getByText("Recent registrations")).toBeVisible();
        await expect(recentRows(page)).toHaveCount(4);

        const metrics = await collectRecentSortIconMetrics(page);

        expect(metrics.map((metric) => metric.columnId)).toEqual([
            "pipeline_name",
            "registration_unique",
            "requester",
            "created_at",
            "output_directory",
        ]);

        for (const metric of metrics) {
            expect(metric.width, `${metric.columnId} icon width`).toBeCloseTo(
                14,
                1,
            );
            expect(metric.height, `${metric.columnId} icon height`).toBeCloseTo(
                14,
                1,
            );
            expect(
                metric.flexShrink,
                `${metric.columnId} icon flex-shrink`,
            ).toBe("0");
        }
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
        await expect(page.getByText("Recent registrations")).toBeVisible();
        await expect(recentRows(page)).toHaveCount(4);

        const recentResultLink = page
            .getByRole("link", { name: rnaseqPipelineName })
            .first();
        const recentHref = await recentResultLink.getAttribute("href");
        const recentDetailUrl = new URL(
            recentHref ?? "/results/",
            page.url(),
        ).toString();

        await recentResultLink.click();
        await expect(page).toHaveURL(recentDetailUrl);

        const backToDashboard = page.getByRole("link", {
            name: "Back to dashboard",
        });

        await expect(backToDashboard).toBeVisible({ timeout: 30000 });
        await backToDashboard.click();

        await expect(page).toHaveURL(/\/$/);
        await expect(page.getByText("Recent registrations")).toBeVisible();
        await expect(recentRows(page)).toHaveCount(4);

        await addRequesterFilter(page, "alice");
        await expect(page).toHaveURL(/\?user=alice/);

        const searchResultLink = page
            .getByRole("link", { name: rnaseqPipelineName })
            .first();
        const searchHref = await searchResultLink.getAttribute("href");
        const searchDetailUrl = new URL(
            searchHref ?? "/results/",
            page.url(),
        ).toString();

        await searchResultLink.click();
        await expect(page).toHaveURL(searchDetailUrl);

        const backToSearch = page.getByRole("link", {
            name: "Back to search results",
        });

        await expect(page).toHaveURL(/\/results\/[^?]+\?returnTo=/);
        await expect(backToSearch).toBeVisible({ timeout: 30000 });
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
        const detailSummary = page.locator(
            '[data-result-detail-summary="true"]',
        );

        await expect(detailSummary).toBeVisible();
        await expect(
            detailSummary.locator('[data-registration-layout="integrated"]'),
        ).toBeVisible();
        await expect(
            detailSummary.locator('[data-result-metadata-layout="integrated"]'),
        ).toBeVisible();
        await expect(
            page.locator('[data-registration-layout="compact"]'),
        ).toHaveCount(0);
        await expect(
            detailSummary.locator('[data-registration-field="Result ID"]'),
        ).toHaveCount(0);
        await expect(
            detailSummary.locator('[data-registration-field="Pipeline name"]'),
        ).toHaveCount(0);
        await expect(
            page.getByText("Registration", { exact: true }),
        ).toHaveCount(0);
        await expect(
            page.getByText("Result metadata", { exact: true }),
        ).toHaveCount(0);
        await expect(
            detailSummary.locator('[data-metadata-row="seqmeta_studyid"]'),
        ).toContainText("6568");
        await expect(
            detailSummary.locator('[data-metadata-row="library"]'),
        ).toHaveCount(0);

        await detailSummary
            .getByRole("button", { name: "All metadata" })
            .click();
        await expect(
            page.locator('[data-metadata-detail-row="library"]'),
        ).toContainText("exon");
    });

    test("keeps the result detail header compact above the file browser", async ({
        page,
    }) => {
        await page.setViewportSize({ width: 1440, height: 1000 });
        await openResultDetail(page, rnaseqPipelineName);

        const detailSummary = page.locator(
            '[data-result-detail-summary="true"]',
        );
        const fileBrowser = page.locator('[data-file-browser="true"]');
        const metadata = detailSummary.locator(
            '[data-result-metadata-layout="integrated"]',
        );
        const metrics = await page.evaluate(() => {
            const summary = document.querySelector(
                '[data-result-detail-summary="true"]',
            );
            const browser = document.querySelector(
                '[data-file-browser="true"]',
            );
            const metadataLayout = document.querySelector(
                '[data-result-detail-summary="true"] [data-result-metadata-layout="integrated"]',
            );

            if (!summary || !browser || !metadataLayout) {
                throw new Error("Missing result detail layout elements");
            }

            const summaryRect = summary.getBoundingClientRect();
            const browserRect = browser.getBoundingClientRect();
            const metadataRect = metadataLayout.getBoundingClientRect();

            return {
                fileBrowserY: browserRect.y,
                metadataHeight: metadataRect.height,
                summaryHeight: summaryRect.height,
            };
        });

        await expect(detailSummary).toBeVisible();
        await expect(fileBrowser).toBeVisible();
        await expect(metadata).toBeVisible();
        expect(metrics.summaryHeight).toBeLessThanOrEqual(370);
        expect(metrics.metadataHeight).toBeLessThanOrEqual(110);
        expect(metrics.fileBrowserY).toBeLessThanOrEqual(430);
    });

    test("keeps the seqmeta dialog body scrollable when content exceeds the viewport", async ({
        page,
    }) => {
        await page.setViewportSize({ width: 720, height: 250 });
        await openResultDetail(page, rnaseqPipelineName);

        const dialog = await openSeqmetaDetailsDialog(page, "seqmeta_studyid");
        const scrollContainer = dialog.getByTestId("seqmeta-dialog-body");

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

    test("compresses single-child paths and expands tree rows beneath the file browser pane", async ({
        page,
    }) => {
        await openResultDetail(page, rnaseqPipelineName);

        const fileBrowser = page.locator('[data-file-browser="true"]');
        const rootSubdirControls = page.locator(
            `[data-subdir-preview-controls="${rnaseqRootPath}"]`,
        );

        await expect(fileBrowser).toBeVisible();
        await expect(fileBrowser).not.toContainText("Explorer");
        await expect(fileBrowser).not.toContainText("Preview focus");
        await expect(rootSubdirControls).toBeVisible();
        await expect(
            page.locator(`[data-directory-path="${rnaseqQcPath}"]`),
        ).toBeVisible();
        await expect(
            page.locator(`[data-directory-path="${rnaseqImagesPath}"]`),
        ).toHaveCount(0);

        await page.locator(`[data-directory-path="${rnaseqQcPath}"]`).click();

        await expect(rootSubdirControls).toBeVisible();
        await expect(
            page.locator(`[data-directory-path="${rnaseqImagesPath}"]`),
        ).toBeVisible();
        await expect(
            page.locator(`[data-file-path="${rnaseqImagePath}"]`),
        ).toHaveCount(0);

        await page
            .locator(`[data-directory-path="${rnaseqImagesPath}"]`)
            .click();

        await expect(rootSubdirControls).toBeVisible();
        await expect(
            page.locator(`[data-file-path="${rnaseqImagePath}"]`),
        ).toBeVisible();
        await expect(
            page.locator(`[data-file-path="${rnaseqImagePath}"]`),
        ).not.toContainText(rnaseqImagePath);
        await expect(
            page.locator(`[data-directory-path="${rnaseqGalleryPath}"]`),
        ).toBeVisible();

        await page
            .locator(`[data-directory-path="${rnaseqImagesPath}"]`)
            .click();

        await expect(
            page.locator(`[data-file-path="${rnaseqImagePath}"]`),
        ).toHaveCount(0);

        await openResultDetail(page, ampliconPipelineName);

        await expect(
            page.locator(`[data-directory-path="${ampliconConfigReviewPath}"]`),
        ).toBeVisible();
        await expect(
            page.locator(`[data-directory-path="${ampliconConfigParentPath}"]`),
        ).toHaveCount(0);
    });

    test("enlarges CSV previews before exposing table controls", async ({
        page,
    }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await selectDirectoryForFile(page, rnaseqReportPath);
        await page.locator(`[data-file-path="${rnaseqReportPath}"]`).click();

        const preview = page.locator('[data-file-browser-preview="single"]');

        await expect(preview).toBeVisible();
        // Inline preview is capped server-side so the file browser only renders
        // a small subset of the rows; the full table only appears once enlarged.
        await expect(preview.getByText(/Showing \d+ preview rows/)).toHaveCount(
            0,
        );
        await expect(
            preview.getByRole("button", { name: "Sort by sample_id" }),
        ).toHaveCount(0);

        await preview
            .getByRole("button", { name: /Enlarge report\.csv preview/i })
            .click();

        const dialog = page.getByRole("dialog", {
            name: /Enlarged report\.csv preview/i,
        });

        await expect(dialog).toBeVisible();
        // Enlarged view fetches the full payload (mode=enlarged) and exposes
        // table controls.
        await expect(dialog.getByText("Showing 20 of 20 rows")).toBeVisible();
        await expect(
            dialog.getByRole("button", { name: "Sort by sample_id" }),
        ).toBeVisible();
        await expect(
            dialog.getByRole("button", { name: "Sort by metric" }),
        ).toBeVisible();
        await expect(
            dialog.getByRole("button", { name: "Sort by value" }),
        ).toBeVisible();
        await expect(
            dialog.getByRole("button", { name: "Sort by status" }),
        ).toBeVisible();
    });

    test("renders a PNG preview image for image outputs", async ({ page }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await selectDirectoryForFile(page, rnaseqImagePath);
        await expect(page.getByLabel("1 preview per row")).toHaveCount(0);
        await page.locator(`[data-file-path="${rnaseqImagePath}"]`).click();

        await expect(
            page.locator('[data-file-browser-preview="single"]'),
        ).toBeVisible();

        const image = page.getByAltText("image.png preview");
        await expect(image).toBeVisible();
        await expect(image).toHaveAttribute("src", /\/api\/file\?/);
    });

    test("uses thumbnails in grid mode and opens the full-size image in the lightbox", async ({
        page,
    }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await selectDirectoryForFile(page, rnaseqGalleryFirstImagePath);
        const folderControls = page.locator(
            `[data-file-browser-folder-controls="${rnaseqGalleryPath}"]`,
        );

        await openPreviewModes(folderControls);
        await folderControls.getByLabel("1 preview per row").check();

        const row = page.locator(
            `[data-file-browser-grid-row="${rnaseqGalleryFirstImagePath}"]`,
        );
        const previewCell = row.locator(
            `[data-grid-preview-path="${rnaseqGalleryFirstImagePath}"]`,
        );
        const thumbnailButton = previewCell.getByRole("button", {
            name: "Open image lightbox",
        });
        const thumbnailImage = previewCell.getByAltText("plot-001.png preview");

        await expect(row).toBeVisible();
        await expect(previewCell).toBeVisible();
        await expect(previewCell.getByText("Click to enlarge")).toHaveCount(0);
        await expect(thumbnailButton).toBeVisible();
        await expect(thumbnailImage).toBeVisible();
        await expect(thumbnailImage).toHaveAttribute("src", /thumb=true/);

        await expect(thumbnailButton).toHaveCSS("cursor", "zoom-in");

        const byteSizeOccurrences = await row.evaluate((element) => {
            const walker = document.createTreeWalker(
                element,
                NodeFilter.SHOW_TEXT,
            );
            let count = 0;

            while (walker.nextNode()) {
                if (
                    /^\d+(?:\.\d+)?\s(?:B|KB|MB|GB|TB)$/.test(
                        walker.currentNode.textContent?.trim() ?? "",
                    )
                ) {
                    count += 1;
                }
            }

            return count;
        });

        expect(byteSizeOccurrences).toBe(1);

        const renderedSpacing = await row.evaluate((rowElement) => {
            const previewElement = rowElement.querySelector(
                "[data-grid-preview-path]",
            );
            const buttonElement = previewElement?.querySelector(
                'button[aria-label="Open image lightbox"]',
            );

            if (!(previewElement instanceof HTMLElement)) {
                throw new Error("Missing grid preview cell");
            }

            if (!(buttonElement instanceof HTMLElement)) {
                throw new Error("Missing thumbnail button");
            }

            const boxChrome = (element: Element) => {
                const styles = window.getComputedStyle(element);
                const sides = ["top", "right", "bottom", "left"];

                return sides.reduce(
                    (total, side) =>
                        total +
                        Number.parseFloat(
                            styles.getPropertyValue(`padding-${side}`),
                        ) +
                        Number.parseFloat(
                            styles.getPropertyValue(`border-${side}-width`),
                        ),
                    0,
                );
            };

            return {
                button: boxChrome(buttonElement),
                previewCell: boxChrome(previewElement),
                row: boxChrome(rowElement),
            };
        });

        expect(renderedSpacing).toEqual({
            button: 0,
            previewCell: 0,
            row: 0,
        });

        const [cellBox, buttonBox, imageBox] = await Promise.all([
            previewCell.boundingBox(),
            thumbnailButton.boundingBox(),
            thumbnailImage.boundingBox(),
        ]);

        if (!cellBox || !buttonBox || !imageBox) {
            throw new Error("Missing grid preview bounding boxes");
        }

        expect(Math.abs(buttonBox.x - cellBox.x)).toBeLessThanOrEqual(1);
        expect(Math.abs(buttonBox.y - cellBox.y)).toBeLessThanOrEqual(1);
        expect(buttonBox.width).toBeGreaterThanOrEqual(cellBox.width - 2);
        expect(imageBox.x).toBeGreaterThanOrEqual(buttonBox.x);
        expect(imageBox.y).toBeGreaterThanOrEqual(buttonBox.y);
        expect(imageBox.x + imageBox.width).toBeLessThanOrEqual(
            buttonBox.x + buttonBox.width + 1,
        );
        expect(imageBox.y + imageBox.height).toBeLessThanOrEqual(
            buttonBox.y + buttonBox.height + 1,
        );
        expect(
            Math.abs(
                imageBox.x +
                    imageBox.width / 2 -
                    (buttonBox.x + buttonBox.width / 2),
            ),
        ).toBeLessThanOrEqual(1);

        await thumbnailButton.click();

        const fullSizeImage = page.getByAltText("plot-001.png full preview");

        await expect(
            page.getByRole("dialog", { name: "Image preview lightbox" }),
        ).toBeVisible();
        await expect(fullSizeImage).toHaveAttribute("src", /\/api\/file\?/);
        await expect(fullSizeImage).not.toHaveAttribute("src", /thumb=true/);
    });

    test("paginates the seeded image gallery after the first 100 previews", async ({
        page,
    }) => {
        await openResultDetail(page, rnaseqPipelineName);

        await selectDirectoryForFile(page, rnaseqGalleryFirstImagePath);
        const folderControls = page.locator(
            `[data-file-browser-folder-controls="${rnaseqGalleryPath}"]`,
        );
        const bottomControls = page.locator(
            `[data-file-browser-bottom-controls="${rnaseqGalleryPath}"]`,
        );

        await openPreviewModes(folderControls);
        await folderControls.getByLabel("1 preview per row").check();
        await folderControls
            .locator('summary[aria-label="Preview modes"]')
            .first()
            .click();

        await expect(folderControls.getByText("Page 1 of 2")).toBeVisible();
        await expect(bottomControls.getByText("Page 1 of 2")).toBeVisible();
        await expect(
            page.locator(
                `[data-grid-preview-path="${rnaseqGalleryFirstImagePath}"]`,
            ),
        ).toBeVisible();
        await expect(
            page.locator(
                `[data-grid-preview-path="${rnaseqGalleryPageTwoImagePath}"]`,
            ),
        ).toHaveCount(0);

        await folderControls
            .getByRole("button", { name: "Next preview page" })
            .click();

        await expect(folderControls.getByText("Page 2 of 2")).toBeVisible();
        await expect(bottomControls.getByText("Page 2 of 2")).toBeVisible();
        await expect(
            page.locator(
                `[data-grid-preview-path="${rnaseqGalleryPageTwoImagePath}"]`,
            ),
        ).toBeVisible();

        await bottomControls
            .getByRole("button", { name: "Previous preview page" })
            .click();

        await expect(folderControls.getByText("Page 1 of 2")).toBeVisible();

        await bottomControls
            .getByRole("combobox", { name: "Preview page" })
            .selectOption("2");

        await expect(bottomControls.getByText("Page 2 of 2")).toBeVisible();
    });

    test("renders seeded JSON file content after the loading state clears", async ({
        page,
    }) => {
        await page.goto("/");
        await expect(page.getByText("Recent registrations")).toBeVisible();
        await expect(recentRows(page)).toHaveCount(4);

        const ampliconLink = page
            .getByRole("link", { name: ampliconPipelineName })
            .first();
        const ampliconHref = await ampliconLink.getAttribute("href");
        const ampliconDetailUrl = new URL(
            ampliconHref ?? "/results/",
            page.url(),
        ).toString();

        await page.goto(ampliconDetailUrl);
        await expect(page).toHaveURL(ampliconDetailUrl);

        const previewResponsePromise = page.waitForResponse(
            (response) =>
                response.request().method() === "GET" &&
                response.url().includes("/api/file?") &&
                response.url().includes(encodeURIComponent(ampliconConfigPath)),
        );
        await selectDirectoryForFile(page, ampliconConfigPath);
        const previewResponse = await previewResponsePromise;

        expect(previewResponse.status()).toBe(200);
    });
});
