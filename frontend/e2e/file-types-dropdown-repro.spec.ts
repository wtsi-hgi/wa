import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

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
const galleriesDemoRootPath = path.join(fixturesRoot, "galleries-demo");
const galleriesDemoSampleAPath = path.join(galleriesDemoRootPath, "sample-a");
const galleriesDemoSampleBPath = path.join(galleriesDemoRootPath, "sample-b");
const sampleBFirstPreviewPath = path.join(
    galleriesDemoSampleBPath,
    "orange-heatmap.svg",
);
const screenshotPath = path.join(
    evidenceDir,
    "file-types-dropdown-compact-extensions-postfix.png",
);
const evidencePath = screenshotPath.replace(/\.png$/, ".json");
const deselectAllScreenshotPath = path.join(
    evidenceDir,
    "file-types-dropdown-deselect-all-postfix.png",
);
const deselectAllEvidencePath = deselectAllScreenshotPath.replace(
    /\.png$/,
    ".json",
);
const directDeselectAllScreenshotPath = path.join(
    evidenceDir,
    "file-types-dropdown-direct-deselect-all-postfix.png",
);
const directDeselectAllEvidencePath = directDeselectAllScreenshotPath.replace(
    /\.png$/,
    ".json",
);
const expectedFileTypeLabels = [
    "Images",
    ".svg",
    ".csv",
    ".tsv",
    ".md",
    ".markdown",
    ".html",
    ".htm",
    ".json",
    ".log",
    ".py",
    ".txt",
    ".xml",
    ".yaml",
    ".yml",
    ".pdf",
];

type RectEvidence = {
    bottom: number;
    height: number;
    left: number;
    top: number;
    width: number;
};

type FileTypeOptionEvidence = {
    checked: boolean;
    dataKind: string | null;
    rect: RectEvidence;
    text: string;
};

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

function seededRecentRows(page: Page): Locator {
    return page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasNotText: "seqmeta/rendering-repro" });
}

async function openNamedResultFileBrowser(page: Page, pipelineName: string) {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    await expect(page.getByText("Latest result sets")).toBeVisible();
    await expect
        .poll(async () => seededRecentRows(page).count())
        .toBeGreaterThanOrEqual(4);

    const resultLink = page.getByRole("link", { name: pipelineName }).first();

    await expect(resultLink).toBeVisible();

    const href = await resultLink.getAttribute("href");

    await page.goto(href ?? "/results/");
    await expect(
        page.getByRole("heading", { level: 1, name: pipelineName }),
    ).toBeVisible({ timeout: 30000 });
    await expect(page.locator('[data-file-browser="true"]')).toBeVisible({
        timeout: 30000,
    });
}

async function selectDirectory(page: Page, directoryPath: string) {
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
    }

    const directoryButton = page
        .locator(`[data-directory-path="${directoryPath}"]`)
        .first();

    await directoryButton.scrollIntoViewIfNeeded();
    await expect(directoryButton).toBeVisible();
    await directoryButton.click();
}

async function openFileTypes(controls: Locator) {
    const summary = controls
        .locator('summary[aria-label="File types"]')
        .first();

    await expect(summary).toBeVisible();
    await summary.evaluate((element) => {
        const details = element.closest("details");

        if (!(details instanceof HTMLDetailsElement)) {
            throw new Error("Missing file types disclosure");
        }

        if (!details.open) {
            (element as HTMLElement).click();
        }
    });
}

async function openPreviewModes(controls: Locator) {
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

async function collectFileTypeEvidence(page: Page) {
    return page.evaluate(
        ({ sampleAPath, screenshotPath }) => {
            function rectEvidence(element: Element): RectEvidence {
                const rect = element.getBoundingClientRect();

                return {
                    bottom: Math.round(rect.bottom),
                    height: Math.round(rect.height),
                    left: Math.round(rect.left),
                    top: Math.round(rect.top),
                    width: Math.round(rect.width),
                };
            }

            const fileBrowser = document.querySelector<HTMLElement>(
                '[data-file-browser="true"]',
            );
            const controls = [
                ...document.querySelectorAll<HTMLElement>(
                    "[data-file-browser-folder-controls]",
                ),
            ].find(
                (element) =>
                    element.getAttribute(
                        "data-file-browser-folder-controls",
                    ) === sampleAPath,
            );
            const menu = [
                ...document.querySelectorAll<HTMLElement>(
                    "[data-subdir-preview-kinds]",
                ),
            ].find(
                (element) =>
                    element.getAttribute("data-subdir-preview-kinds") ===
                    sampleAPath,
            );

            if (!controls || !menu) {
                throw new Error(
                    "Missing open File types controls for sample-a",
                );
            }

            const fileTypeOptions: FileTypeOptionEvidence[] = [
                ...menu.querySelectorAll("label"),
            ].map((label) => {
                const input = label.querySelector("input");
                const labelText =
                    label.querySelector("span")?.textContent?.trim() ??
                    label.textContent?.trim() ??
                    "";

                return {
                    checked:
                        input instanceof HTMLInputElement
                            ? input.checked
                            : false,
                    dataKind:
                        input?.getAttribute("data-subdir-preview-kind") ?? null,
                    rect: rectEvidence(label),
                    text: labelText,
                };
            });
            const directFilePaths = [
                ...document.querySelectorAll<HTMLElement>("[data-file-path]"),
            ]
                .map(
                    (element) =>
                        element.dataset.filePath ??
                        element.getAttribute("data-file-path") ??
                        "",
                )
                .filter((filePath) => {
                    const relativePath = filePath.slice(sampleAPath.length + 1);

                    return (
                        filePath.startsWith(`${sampleAPath}/`) &&
                        relativePath.length > 0 &&
                        !relativePath.includes("/")
                    );
                });
            const directFileExtensions = [
                ...new Set(
                    directFilePaths.map((filePath) => {
                        const fileName = filePath.split("/").pop() ?? "";
                        const parts = fileName.split(".");

                        return parts.length > 1
                            ? (parts.pop() ?? "").toLowerCase()
                            : "";
                    }),
                ),
            ].sort();
            const extensionSpecificLabelsPresent = fileTypeOptions.some(
                (option) =>
                    /^\.(?:csv|html?|json|log|md|svg|tsv|txt)$/i.test(
                        option.text,
                    ),
            );
            const labelsAreOnePerLine = fileTypeOptions.every(
                (option, index) =>
                    index === 0 ||
                    option.rect.top >=
                        (fileTypeOptions[index - 1]?.rect.bottom ?? 0) - 1,
            );
            const rowTops = [
                ...new Set(fileTypeOptions.map((option) => option.rect.top)),
            ];

            return {
                currentBug: {
                    expected:
                        "specific supported extensions, with bitmap formats grouped as Images and svg separate",
                    observed:
                        "compact extension labels are displayed in a wrapping grid",
                },
                directFileExtensions,
                directFilePaths,
                extensionSpecificLabelsPresent,
                fileBrowserText: fileBrowser?.innerText.slice(0, 2500) ?? null,
                fileTypeOptions,
                labelsAreOnePerLine,
                menuRect: rectEvidence(menu),
                menuText: menu.innerText,
                pageUrl: window.location.href,
                rowCount: rowTops.length,
                sampleAPath,
                screenshotPath,
                summaryText:
                    controls
                        .querySelector(
                            '[data-file-browser-control-current="file-types"]',
                        )
                        ?.textContent?.trim() ?? null,
            };
        },
        { sampleAPath: galleriesDemoSampleAPath, screenshotPath },
    );
}

test("shows compact specific preview extensions in the File types dropdown", async ({
    page,
}) => {
    test.setTimeout(120000);

    await openNamedResultFileBrowser(page, "wtsi/galleries-demo");
    await selectDirectory(page, galleriesDemoSampleAPath);

    const controls = page.locator(
        `[data-file-browser-folder-controls="${galleriesDemoSampleAPath}"]`,
    );
    const menu = page.locator(
        `[data-subdir-preview-kinds="${galleriesDemoSampleAPath}"]`,
    );

    await expect(controls).toBeVisible();
    await openFileTypes(controls);
    await expect(menu).toBeVisible();
    await expect(
        menu.locator('input[data-subdir-preview-kind="image"]'),
    ).toBeChecked();
    await expect(
        menu.locator('input[data-subdir-preview-kind="svg"]'),
    ).toBeChecked();

    const evidence = await collectFileTypeEvidence(page);

    mkdirSync(evidenceDir, { recursive: true });
    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: screenshotPath,
    });
    writeFileSync(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`);

    expect(evidence.directFileExtensions).toEqual(
        expect.arrayContaining(["csv", "html", "log", "md", "svg"]),
    );
    expect(evidence.fileTypeOptions.map((option) => option.text)).toEqual(
        expectedFileTypeLabels,
    );
    expect(evidence.fileTypeOptions.map((option) => option.text)).not.toEqual(
        expect.arrayContaining([
            "Tables",
            "Markdown",
            "Text & code",
            "Documents",
        ]),
    );
    expect(evidence.fileTypeOptions.every((option) => option.checked)).toBe(
        true,
    );
    expect(evidence.extensionSpecificLabelsPresent).toBe(true);
    expect(evidence.fileTypeOptions[0]).toMatchObject({
        dataKind: "image",
        text: "Images",
    });
    expect(evidence.fileTypeOptions[1]).toMatchObject({
        dataKind: "svg",
        text: ".svg",
    });
    expect(evidence.labelsAreOnePerLine).toBe(false);
    expect(evidence.rowCount).toBeLessThan(evidence.fileTypeOptions.length);
});

test("deselects all file types from the compact File types dropdown", async ({
    page,
}) => {
    test.setTimeout(120000);

    await openNamedResultFileBrowser(page, "wtsi/galleries-demo");
    await selectDirectory(page, galleriesDemoSampleAPath);

    const controls = page.locator(
        `[data-file-browser-folder-controls="${galleriesDemoSampleAPath}"]`,
    );
    const menu = page.locator(
        `[data-subdir-preview-kinds="${galleriesDemoSampleAPath}"]`,
    );

    await expect(controls).toBeVisible();
    await openFileTypes(controls);
    await expect(menu).toBeVisible();
    await expect(
        menu.locator('input[data-subdir-preview-kind="image"]'),
    ).toBeChecked();
    await expect(
        menu.locator('input[data-subdir-preview-kind="svg"]'),
    ).toBeChecked();
    await expect(menu.getByText(".csv", { exact: true })).toBeVisible();

    const initialCheckedCount = await menu
        .locator("input[data-subdir-preview-kind]:checked")
        .count();
    const deselectAllButton = menu.getByRole("button", {
        name: /deselect all/i,
    });

    await expect(deselectAllButton).toBeVisible();
    expect(initialCheckedCount).toBeGreaterThan(1);

    await deselectAllButton.click();
    await expect(
        menu.locator("input[data-subdir-preview-kind]:checked"),
    ).toHaveCount(0);
    await expect(
        controls.locator('[data-file-browser-control-current="file-types"]'),
    ).toHaveText("No file types");

    const evidence = await menu.evaluate(
        (element, details) => {
            const labels = [...element.querySelectorAll("label")].map(
                (label) => label.textContent?.trim() ?? "",
            );
            const checkedLabels = [
                ...element.querySelectorAll("label"),
            ].flatMap((label) => {
                const input = label.querySelector("input");

                if (!(input instanceof HTMLInputElement) || !input.checked) {
                    return [];
                }

                return label.textContent?.trim() ?? "";
            });
            const buttons = [...element.querySelectorAll("button")].map(
                (button) => button.textContent?.trim() ?? "",
            );

            return {
                buttons,
                checkedLabels,
                fixedBug: {
                    expected:
                        "a Deselect all button clears every selected File types option",
                    observed:
                        "the File types menu has a Deselect all button and no checked options after clicking it",
                },
                hasDeselectAllButton: buttons.some((label) =>
                    /deselect all/i.test(label),
                ),
                initialCheckedCount: details.initialCheckedCount,
                labels,
                menuText: element.textContent?.trim() ?? "",
                screenshotPath: details.screenshotPath,
            };
        },
        {
            initialCheckedCount,
            screenshotPath: deselectAllScreenshotPath,
        },
    );

    mkdirSync(evidenceDir, { recursive: true });
    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: deselectAllScreenshotPath,
    });
    writeFileSync(
        deselectAllEvidencePath,
        `${JSON.stringify(evidence, null, 2)}\n`,
    );

    expect(evidence.labels).toEqual(expectedFileTypeLabels);
    expect(evidence.initialCheckedCount).toBeGreaterThan(1);
    expect(evidence.checkedLabels).toEqual([]);
    expect(evidence.hasDeselectAllButton).toBe(true);
});

test("keeps File types available after deselecting all in a direct-file-only folder", async ({
    page,
}) => {
    test.setTimeout(120000);

    await openNamedResultFileBrowser(page, "wtsi/galleries-demo");
    await selectDirectory(page, galleriesDemoSampleBPath);

    const controls = page.locator(
        `[data-file-browser-folder-controls="${galleriesDemoSampleBPath}"]`,
    );
    const menu = page.locator(
        `[data-subdir-preview-kinds="${galleriesDemoSampleBPath}"]`,
    );
    const gridRow = page.locator(
        `[data-file-browser-grid-row="${sampleBFirstPreviewPath}"]`,
    );

    await expect(controls).toBeVisible();
    await openPreviewModes(controls);
    await controls.getByLabel("1 preview per row").check();
    await expect(gridRow).toBeVisible();

    await openFileTypes(controls);
    await expect(menu).toBeVisible();
    await expect(
        menu.locator('input[data-subdir-preview-kind="svg"]'),
    ).toBeChecked();

    const deselectAllButton = menu.getByRole("button", {
        name: /deselect all/i,
    });

    await deselectAllButton.click();

    await expect(controls).toBeVisible();
    await expect(menu).toBeVisible();
    await expect(
        menu.locator("input[data-subdir-preview-kind]:checked"),
    ).toHaveCount(0);
    await expect(
        controls.locator('[data-file-browser-control-current="file-types"]'),
    ).toHaveText("No file types");
    await expect(gridRow).toHaveCount(0);

    await menu.locator('input[data-subdir-preview-kind="svg"]').check();

    await expect(
        controls.locator('[data-file-browser-control-current="file-types"]'),
    ).toHaveText(".svg");
    await expect(gridRow).toBeVisible();
    await expect(
        page
            .locator(`[data-grid-preview-path="${sampleBFirstPreviewPath}"]`)
            .first(),
    ).toBeVisible();

    const evidence = await page.evaluate(
        (details) => {
            const controls = document.querySelector<HTMLElement>(
                `[data-file-browser-folder-controls="${details.directoryPath}"]`,
            );
            const menu = document.querySelector<HTMLElement>(
                `[data-subdir-preview-kinds="${details.directoryPath}"]`,
            );
            const restoredRow = document.querySelector<HTMLElement>(
                `[data-file-browser-grid-row="${details.previewPath}"]`,
            );

            return {
                directoryPath: details.directoryPath,
                fixedBug: {
                    expected:
                        "direct-file-only folders keep File types available after Deselect all and allow reselecting a type",
                    observed:
                        "the File types control remains mounted, reports .svg after reselecting, and the SVG preview row is restored",
                },
                restoredPreviewText: restoredRow?.innerText.trim() ?? null,
                screenshotPath: details.screenshotPath,
                selectedSummary:
                    controls
                        ?.querySelector(
                            '[data-file-browser-control-current="file-types"]',
                        )
                        ?.textContent?.trim() ?? null,
                visibleMenuText: menu?.innerText.trim() ?? null,
            };
        },
        {
            directoryPath: galleriesDemoSampleBPath,
            previewPath: sampleBFirstPreviewPath,
            screenshotPath: directDeselectAllScreenshotPath,
        },
    );

    mkdirSync(evidenceDir, { recursive: true });
    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: directDeselectAllScreenshotPath,
    });
    writeFileSync(
        directDeselectAllEvidencePath,
        `${JSON.stringify(evidence, null, 2)}\n`,
    );

    expect(evidence.selectedSummary).toBe(".svg");
    expect(evidence.restoredPreviewText).toContain("orange-heatmap.svg");
});
