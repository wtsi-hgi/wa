import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { expect, test, type Page } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const fixturesRoot = path.join(
    repoRoot,
    ".docs",
    "results-web",
    "fixtures",
    "files",
);
const rnaseqPipelineName = "nf-core/rnaseq";
const scriptedHtmlPath = path.join(
    fixturesRoot,
    "rnaseq",
    "reports",
    "scripted-table.html",
);
const previewScreenshotPath = path.join(
    evidenceDir,
    "html-preview-scripted-table-postfix.png",
);
const enlargedScreenshotPath = path.join(
    evidenceDir,
    "html-preview-scripted-table-enlarged-postfix.png",
);
const directScreenshotPath = path.join(
    evidenceDir,
    "html-preview-scripted-table-direct-visible.png",
);
const evidencePath = path.join(
    evidenceDir,
    "html-preview-scripted-table-postfix.json",
);

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

function recentRows(page: Page) {
    return page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasNotText: "seqmeta/rendering-repro" });
}

async function expectRecentRowsLoaded(page: Page): Promise<void> {
    await expect.poll(async () => recentRows(page).count()).toBeGreaterThan(0);
}

async function openResultDetail(
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

test("renders scripted html table in the file-browser preview", async ({
    context,
    page,
}) => {
    mkdirSync(evidenceDir, { recursive: true });

    const directPage = await context.newPage();

    await directPage.goto(pathToFileURL(scriptedHtmlPath).toString());
    await expect(
        directPage.getByRole("table", { name: "Script rendered QC table" }),
    ).toBeVisible();
    await directPage.screenshot({ fullPage: true, path: directScreenshotPath });
    await directPage.close();

    await page.setViewportSize({ width: 1280, height: 900 });
    await openResultDetail(page, rnaseqPipelineName);
    await selectDirectoryForFile(page, scriptedHtmlPath);
    await page
        .locator(`[data-file-path="${scriptedHtmlPath}"]`)
        .first()
        .click();

    const preview = page.locator('[data-file-browser-preview="single"]');
    const frameElement = preview
        .locator('iframe[title="HTML preview"]')
        .first();
    const frame = preview.frameLocator('iframe[title="HTML preview"]');

    await expect(preview).toBeVisible();
    await expect(frameElement).toBeVisible();
    await expect(frame.locator("body")).toContainText(
        "Scripted Table Preview Fixture",
    );

    const previewBodyText = await frame.locator("body").innerText();
    const previewTableCount = await frame.locator("table").count();

    await preview.screenshot({ path: previewScreenshotPath });

    await preview
        .getByRole("button", { name: "Enlarge scripted-table.html preview" })
        .click();
    const dialog = page.getByRole("dialog", {
        name: "Enlarged scripted-table.html preview",
    });
    const enlargedFrameElement = dialog
        .locator('iframe[title="Enlarged HTML preview"]')
        .first();
    const enlargedFrame = dialog.frameLocator(
        'iframe[title="Enlarged HTML preview"]',
    );

    await expect(dialog).toBeVisible();
    await expect(enlargedFrameElement).toBeVisible();
    await expect(
        enlargedFrame.getByRole("table", {
            name: "Script rendered QC table",
        }),
    ).toBeVisible();

    const enlargedBodyText = await enlargedFrame.locator("body").innerText();
    const enlargedTableCount = await enlargedFrame.locator("table").count();

    await dialog.screenshot({ path: enlargedScreenshotPath });

    writeFileSync(
        evidencePath,
        JSON.stringify(
            {
                directHtmlPath: scriptedHtmlPath,
                directScreenshotPath,
                enlargedBodyText,
                enlargedScreenshotPath,
                enlargedTableCount,
                previewBodyText,
                previewScreenshotPath,
                previewTableCount,
            },
            null,
            2,
        ),
    );

    expect(previewTableCount).toBeGreaterThan(0);
    expect(enlargedTableCount).toBeGreaterThan(0);
});
