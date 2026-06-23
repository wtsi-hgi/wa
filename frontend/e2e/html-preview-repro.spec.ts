import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { expect, test } from "@playwright/test";

import {
    openResultDetail,
    selectDirectoryForFile,
} from "./helpers/results-navigation";
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
