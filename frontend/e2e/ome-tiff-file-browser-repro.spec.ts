import { mkdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";
import sharp from "sharp";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultRegistration,
    type ResultSet,
} from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const outputDirectory = path.join(evidenceDir, "ome-tiff-file-browser-fixture");
const subfolderPreviewDirectory = path.join(outputDirectory, "qc");
const nestedDirectory = path.join(outputDirectory, "qc", "ome");
const tiffPath = path.join(nestedDirectory, "small-multichannel.ome.tiff");
const nonPreviewDirectory = path.join(outputDirectory, "qc", "z-nonpreview");
const nonPreviewPath = path.join(nonPreviewDirectory, "notes.bin");
const pipelineName = "wa/ome-tiff-file-browser-repro";
const failureScreenshotPath = path.join(
    evidenceDir,
    "ome-tiff-file-browser-generated-failure.png",
);
const visibleScreenshotPath = path.join(
    evidenceDir,
    "ome-tiff-file-browser-generated-visible.png",
);
const subfolderControlsScreenshotPath = path.join(
    evidenceDir,
    "ome-tiff-file-browser-subfolder-controls.png",
);

let registeredResult: ResultSet | null = null;

test.beforeAll(async () => {
    mkdirSync(evidenceDir, { recursive: true });
    rmSync(outputDirectory, { force: true, recursive: true });
    mkdirSync(nestedDirectory, { recursive: true });
    mkdirSync(nonPreviewDirectory, { recursive: true });
    await writeSmallOmeTiff(tiffPath);
    writeFileSync(nonPreviewPath, "keeps the parent folder visible\n");

    const tiffStats = statSync(tiffPath);
    const nonPreviewStats = statSync(nonPreviewPath);
    const registration: ResultRegistration = {
        command: "nextflow run wa/ome-tiff-file-browser-repro",
        files: [
            {
                kind: "output",
                mtime: tiffStats.mtime.toISOString(),
                path: tiffPath,
                size: tiffStats.size,
            },
            {
                kind: "output",
                mtime: nonPreviewStats.mtime.toISOString(),
                path: nonPreviewPath,
                size: nonPreviewStats.size,
            },
        ],
        metadata: {
            panel: "ome-tiff-file-browser-repro",
            project: "Generated OME-TIFF file browser repro",
        },
        operator: "ome-tiff-repro-operator",
        output_directory: outputDirectory,
        pipeline_identifier:
            "https://github.com/wtsi-hgi/wa/ome-tiff-file-browser-repro",
        pipeline_name: pipelineName,
        pipeline_version: "2026.06.24",
        requester: "ome-tiff-repro-requester",
        run_key: `runid=260624&unique=ome_tiff_file_browser_${Date.now()}`,
    };

    registeredResult = registerResult(registration);
});

test.afterAll(() => {
    if (registeredResult) {
        deleteResult(registeredResult.id);
    }
});

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

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

async function selectDirectoryForSubfolderPreviews(
    page: Page,
    directoryPath: string,
): Promise<void> {
    const directoryButton = page
        .locator(`[data-directory-path="${directoryPath}"]`)
        .first();

    await directoryButton.scrollIntoViewIfNeeded();
    await expect(directoryButton).toBeVisible();

    if (
        (await directoryButton.getAttribute("data-directory-expanded")) ===
        "true"
    ) {
        await directoryButton.click();
        await expect(directoryButton).toHaveAttribute(
            "data-directory-expanded",
            "false",
        );
    }

    await directoryButton.click();
    await expect(directoryButton).toHaveAttribute(
        "data-directory-expanded",
        "true",
    );
}

async function writeSmallOmeTiff(filePath: string): Promise<void> {
    const width = 16;
    const pageHeight = 16;
    const channels = 3;
    const pages = 3;
    const pixels = Buffer.alloc(width * pageHeight * pages * channels);

    for (let page = 0; page < pages; page += 1) {
        for (let y = 0; y < pageHeight; y += 1) {
            for (let x = 0; x < width; x += 1) {
                const offset = ((page * pageHeight + y) * width + x) * channels;

                pixels[offset] = page === 0 ? 255 : x * 12;
                pixels[offset + 1] = page === 1 ? 255 : y * 12;
                pixels[offset + 2] = page === 2 ? 255 : 64;
            }
        }
    }

    const omeXml = `<?xml version="1.0" encoding="UTF-8"?><OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06"><Image ID="Image:0"><Pixels ID="Pixels:0:0" DimensionOrder="XYCZT" Type="uint8" SizeX="${width}" SizeY="${pageHeight}" SizeZ="1" SizeC="${pages}" SizeT="1"><Channel ID="Channel:0:0" Name="red" SamplesPerPixel="1"/><Channel ID="Channel:0:1" Name="green" SamplesPerPixel="1"/><Channel ID="Channel:0:2" Name="blue" SamplesPerPixel="1"/><TiffData IFD="0" PlaneCount="${pages}"/></Pixels></Image></OME>`;

    await sharp(pixels, {
        raw: {
            channels,
            height: pageHeight * pages,
            pageHeight,
            width,
        },
    })
        .withXmp(omeXml)
        .tiff({ compression: "none" })
        .toFile(filePath);
}

test("registered generated multichannel OME-TIFF renders in the file browser preview", async ({
    page,
}) => {
    test.setTimeout(120_000);

    if (!registeredResult) {
        throw new Error("Result registration did not complete");
    }

    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto(`/results/${registeredResult.id}`, {
        waitUntil: "domcontentloaded",
    });

    const fileBrowser = page.locator('[data-file-browser="true"]');
    await expect(fileBrowser).toBeVisible({ timeout: 30_000 });
    await expect(page.locator(`[data-file-path="${tiffPath}"]`)).toBeVisible();

    const preview = page.locator('[data-file-browser-preview="single"]');
    await expect(preview).toBeVisible();

    try {
        const previewImage = preview.getByAltText(
            "small-multichannel.ome.tiff preview",
        );

        await expect(previewImage).toBeVisible({ timeout: 30_000 });
        await expect
            .poll(
                async () =>
                    previewImage.evaluate((image) => {
                        const img = image as HTMLImageElement;

                        return (
                            img.complete &&
                            img.naturalHeight > 0 &&
                            img.naturalWidth > 0
                        );
                    }),
                { timeout: 30_000 },
            )
            .toBe(true);
        await expect(preview.getByText("Preview unavailable")).toHaveCount(0);
        await expect(preview.getByLabel("Channel")).toHaveCount(0);

        const dimensions = await previewImage.evaluate((image) => {
            const img = image as HTMLImageElement;
            const rect = img.getBoundingClientRect();

            return {
                naturalHeight: img.naturalHeight,
                naturalWidth: img.naturalWidth,
                renderedHeight: rect.height,
                renderedWidth: rect.width,
                src: img.currentSrc || img.src,
            };
        });

        expect(dimensions.naturalHeight).toBeGreaterThan(0);
        expect(dimensions.naturalWidth).toBeGreaterThan(0);
        expect(dimensions.renderedHeight).toBeGreaterThanOrEqual(120);
        expect(dimensions.renderedWidth).toBeGreaterThanOrEqual(120);
        expect(dimensions.src).toContain("ome=plane");
        expect(dimensions.src).toContain("channel=0");
        expect(dimensions.src).toContain("z=0");
        expect(dimensions.src).toContain("t=0");

        await page.screenshot({ fullPage: true, path: visibleScreenshotPath });

        await preview
            .getByRole("button", { name: /open image lightbox/i })
            .click();
        const lightbox = page.getByRole("dialog", {
            name: /image preview lightbox/i,
        });

        await expect(lightbox).toBeVisible();
        await expect(
            lightbox.getByLabel("Channel", { exact: true }),
        ).toBeVisible();
    } catch (error) {
        await page.screenshot({ fullPage: true, path: failureScreenshotPath });
        throw error;
    }
});

test("registered generated multichannel OME-TIFF shows channel controls when enlarged from subfolder previews", async ({
    page,
}) => {
    test.setTimeout(120_000);

    if (!registeredResult) {
        throw new Error("Result registration did not complete");
    }

    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto(`/results/${registeredResult.id}`, {
        waitUntil: "domcontentloaded",
    });

    const fileBrowser = page.locator('[data-file-browser="true"]');
    await expect(fileBrowser).toBeVisible({ timeout: 30_000 });
    await expect(page.locator(`[data-file-path="${tiffPath}"]`)).toBeVisible();

    await selectDirectoryForSubfolderPreviews(page, subfolderPreviewDirectory);

    const folderControls = page.locator(
        `[data-file-browser-folder-controls="${subfolderPreviewDirectory}"]`,
    );
    await expect(folderControls).toBeVisible();
    await openPreviewModes(folderControls);
    await folderControls.getByLabel("Subfolder previews").check();

    const subfolderStrip = page.locator(
        `[data-subdir-preview-strip="${nestedDirectory}"]`,
    );
    const subfolderFrame = page.locator(
        `[data-subdir-preview-frame="${tiffPath}"]`,
    );
    const subfolderPreviewImage = subfolderFrame.getByAltText(
        "small-multichannel.ome.tiff preview",
    );

    await expect(subfolderStrip).toBeVisible();
    await expect(subfolderFrame).toBeVisible();
    await expect(subfolderPreviewImage).toBeVisible({ timeout: 30_000 });
    await expect(subfolderPreviewImage).toHaveAttribute("src", /ome=plane/);
    await expect(subfolderPreviewImage).toHaveAttribute("src", /channel=0/);

    await subfolderFrame
        .getByRole("button", { name: /open image lightbox/i })
        .click();

    const lightbox = page.getByRole("dialog", {
        name: /image preview lightbox/i,
    });

    await expect(lightbox).toBeVisible();
    await expect(lightbox.getByLabel("Channel", { exact: true })).toBeVisible({
        timeout: 30_000,
    });
    await expect(lightbox.getByText("3 channels")).toBeVisible();
    await page.screenshot({
        fullPage: true,
        path: subfolderControlsScreenshotPath,
    });
});
