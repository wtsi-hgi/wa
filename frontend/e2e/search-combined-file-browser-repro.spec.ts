import { mkdirSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Locator, type Page } from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultRegistration,
    type ResultSet,
} from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const fixtureRoot = path.join(
    evidenceDir,
    "search-combined-file-browser-fixture",
);
const workRoot = path.join(
    fixtureRoot,
    "shared-work",
    "pipelines",
    "2026-06-01",
    "rnaseq",
);
const pipelineName = "wa/combined-browser-repro";
const sampleAlpha = "COMBINED_SAMPLE_ALPHA";
const sampleBeta = "COMBINED_SAMPLE_BETA";

let registeredResults: ResultSet[] = [];

type CapturedConsoleMessage = {
    location: {
        columnNumber: number;
        lineNumber: number;
        url: string;
    };
    text: string;
    type: string;
};

test.beforeAll(() => {
    registeredResults = [
        registerCombinedBrowserResult({
            sample: sampleAlpha,
            leafDirectory: path.join("results", "samples", "alpha", "final"),
            runKey: "runid=260601&unique=combined-alpha",
            fileName: "alpha-expression-counts.tsv",
            content: "gene\talpha\nENSG000001\t42\n",
        }),
        registerCombinedBrowserResult({
            sample: sampleBeta,
            leafDirectory: path.join("results", "samples", "beta", "final"),
            runKey: "runid=260601&unique=combined-beta",
            fileName: "beta-expression-counts.tsv",
            content: "gene\tbeta\nENSG000001\t84\n",
        }),
    ];
});

test.afterAll(() => {
    for (const result of registeredResults) {
        deleteResult(result.id);
    }
});

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

function registerCombinedBrowserResult({
    content,
    fileName,
    leafDirectory,
    runKey,
    sample,
}: {
    content: string;
    fileName: string;
    leafDirectory: string;
    runKey: string;
    sample: string;
}): ResultSet {
    const outputDirectory = path.join(workRoot, leafDirectory);
    const outputPath = path.join(outputDirectory, fileName);

    mkdirSync(outputDirectory, { recursive: true });
    writeFileSync(outputPath, content);

    const stats = statSync(outputPath);
    const registration: ResultRegistration = {
        pipeline_identifier:
            "https://github.com/wtsi-hgi/wa/combined-browser-repro",
        run_key: runKey,
        requester: "combined-browser-requester",
        operator: "combined-browser-operator",
        command: `nextflow run ${pipelineName} --sample ${sample}`,
        pipeline_name: pipelineName,
        pipeline_version: "2026.06.01",
        output_directory: outputDirectory,
        metadata: {
            sample,
            cohort: "combined-browser-repro",
        },
        files: [
            {
                path: outputPath,
                mtime: stats.mtime.toISOString(),
                size: stats.size,
                kind: "output",
            },
        ],
    };

    return registerResult(registration);
}

function matchingRows(page: Page): Locator {
    return page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasText: pipelineName });
}

function lockedMatchingRows(page: Page): Locator {
    return page
        .locator(
            'tbody tr[data-result-row="true"][data-result-row-locked="true"]',
        )
        .filter({ hasText: pipelineName });
}

async function writeEvidence(
    page: Page,
    screenshotName: string,
    extraEvidence: Record<string, unknown> = {},
): Promise<void> {
    mkdirSync(evidenceDir, { recursive: true });

    const screenshotPath = path.join(evidenceDir, screenshotName);
    const evidencePath = screenshotPath.replace(/\.png$/, ".json");
    const evidence = await page.evaluate(() => {
        const searchBuilder = document.querySelector(
            '[data-search-builder="true"]',
        );
        const fileBrowsers = document.querySelectorAll(
            '[data-file-browser="true"]',
        );
        const combinedSearchFileBrowsers = document.querySelectorAll(
            '[data-search-combined-file-browser="true"]',
        );
        const resultRows = document.querySelectorAll(
            'tbody tr[data-result-row="true"]',
        );
        const lockedResultRows = document.querySelectorAll(
            'tbody tr[data-result-row-locked="true"]',
        );

        return {
            combinedSearchFileBrowserCount: combinedSearchFileBrowsers.length,
            fileBrowserCount: fileBrowsers.length,
            lockedResultRowCount: lockedResultRows.length,
            resultRowCount: resultRows.length,
            searchBuilderText: searchBuilder?.textContent ?? null,
            visibleText: document.body.innerText.slice(0, 4000),
        };
    });

    await page.screenshot({ fullPage: true, path: screenshotPath });
    writeFileSync(
        evidencePath,
        `${JSON.stringify({ ...evidence, ...extraEvidence, screenshotPath }, null, 2)}\n`,
    );
}

async function collectSubfolderPreviewEvidence(page: Page) {
    return page.evaluate(() => {
        const fileBrowser = document.querySelector(
            '[data-file-browser="true"]',
        );
        const directoryRows = [
            ...document.querySelectorAll<HTMLElement>("[data-directory-row]"),
        ].map((row) => ({
            path: row.dataset.directoryRow ?? null,
            text: row.innerText.slice(0, 1200),
        }));
        const controls = [
            ...document.querySelectorAll<HTMLElement>(
                "[data-file-browser-folder-controls]",
            ),
        ].map((control) => ({
            path: control.dataset.fileBrowserFolderControls ?? null,
            subdirPreviewControls:
                control.dataset.subdirPreviewControls ?? null,
            text: control.innerText,
        }));
        const strips = [
            ...document.querySelectorAll<HTMLElement>(
                "[data-subdir-preview-strip]",
            ),
        ].map((strip) => ({
            cardPaths: [
                ...strip.querySelectorAll<HTMLElement>(
                    "[data-subdir-preview-card]",
                ),
            ].map((card) => card.dataset.subdirPreviewCard ?? null),
            path: strip.dataset.subdirPreviewStrip ?? null,
            text: strip.innerText,
        }));

        return {
            controls,
            directoryRows,
            fileBrowserText: fileBrowser?.textContent ?? null,
            previewModeTriggerCount: document.querySelectorAll(
                '[data-file-browser-control-trigger="preview-modes"]',
            ).length,
            subfolderPreviewInputCount: document.querySelectorAll(
                'input[aria-label="Subfolder previews"]',
            ).length,
            subfolderPreviewStripCount: strips.length,
            strips,
        };
    });
}

async function collectCombinedGalleriesRootEvidence(page: Page) {
    return page.evaluate(() => {
        const fileBrowser = document.querySelector<HTMLElement>(
            '[data-file-browser="true"]',
        );
        const directoryRows = [
            ...document.querySelectorAll<HTMLElement>("[data-directory-row]"),
        ].map((row) => ({
            path: row.dataset.directoryRow ?? "",
            text: row.innerText.slice(0, 1000),
        }));
        const directoryFileGroups = [
            ...document.querySelectorAll<HTMLElement>(
                "[data-file-browser-directory-files]",
            ),
        ].map((group) => ({
            directory:
                group.dataset.fileBrowserDirectoryFiles ??
                group.getAttribute("data-file-browser-directory-files") ??
                "",
            files: [
                ...group.querySelectorAll<HTMLElement>("[data-file-path]"),
            ].map((file) => ({
                path: file.dataset.filePath ?? "",
                text: file.innerText,
            })),
        }));
        const resultRows = [
            ...document.querySelectorAll<HTMLElement>(
                'tbody tr[data-result-row="true"]',
            ),
        ].map((row) => row.innerText);

        return {
            directoryFileGroups,
            directoryRows,
            fileBrowserText: fileBrowser?.innerText ?? null,
            resultRows,
            rootDirectory: directoryRows[0]?.path ?? null,
        };
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

test.describe("search combined file browser repro", () => {
    test("shows a locked combined file browser state for logged-out matching results", async ({
        context,
        page,
    }) => {
        await context.clearCookies();
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        await expect(matchingRows(page)).toHaveCount(0);
        await expect(lockedMatchingRows(page)).toHaveCount(0);

        await writeEvidence(
            page,
            "search-combined-file-browser-logged-out-locked.png",
        );

        const combinedBrowser = page.locator(
            '[data-search-combined-file-browser="true"]',
        );

        await expect(combinedBrowser).toHaveCount(1);
        await expect(combinedBrowser).toContainText("Combined files");
        await expect(combinedBrowser).toContainText("Result sets");
        await expect(combinedBrowser).not.toContainText("Result rows");
        await expect(combinedBrowser).toContainText("File access locked");
        await expect(combinedBrowser).toContainText("2 matching result sets");
        await expect(combinedBrowser).toContainText(workRoot);
        await expect(
            combinedBrowser.locator('[data-locked-output-directory="true"]'),
        ).toHaveCount(2);
        await expect(
            combinedBrowser.locator("button[data-directory-path]"),
        ).toHaveCount(0);
        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(0);

        await page.getByRole("button", { name: "Result sets" }).click();
        await expect(lockedMatchingRows(page)).toHaveCount(2);
    });

    test("shows one default file browser for all files across matching result sets", async ({
        page,
    }) => {
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        await writeEvidence(
            page,
            "search-combined-file-browser-missing-prefilter.png",
        );

        const searchBuilder = page.locator('[data-search-builder="true"]');
        const combinedBrowser = page.locator('[data-file-browser="true"]');
        const alphaOutputDirectory = path.join(
            workRoot,
            "results",
            "samples",
            "alpha",
            "final",
        );
        const betaOutputDirectory = path.join(
            workRoot,
            "results",
            "samples",
            "beta",
            "final",
        );

        await expect(combinedBrowser).toHaveCount(1);
        await expect(
            combinedBrowser.locator(
                `[data-directory-path="${alphaOutputDirectory}"]`,
            ),
        ).toBeVisible();
        await expect(
            combinedBrowser.locator(
                `[data-directory-path="${betaOutputDirectory}"]`,
            ),
        ).toBeVisible();

        const browserEvidence =
            await collectCombinedGalleriesRootEvidence(page);
        const rootFileGroup = browserEvidence.directoryFileGroups.find(
            (group) => group.directory === browserEvidence.rootDirectory,
        );
        expect(rootFileGroup?.files ?? []).toEqual([]);

        await selectDirectory(page, alphaOutputDirectory);
        await expect(combinedBrowser).toContainText(
            "alpha-expression-counts.tsv",
        );
        await expect(combinedBrowser).not.toContainText(
            "beta-expression-counts.tsv",
        );

        await selectDirectory(page, betaOutputDirectory);
        await expect(combinedBrowser).toContainText(
            "beta-expression-counts.tsv",
        );
        await expect(combinedBrowser).not.toContainText(
            "alpha-expression-counts.tsv",
        );

        const layout = await searchBuilder.evaluate((builder) => {
            const browser = document.querySelector(
                '[data-file-browser="true"]',
            );
            const resultRows = document.querySelectorAll(
                'tbody tr[data-result-row="true"]',
            );

            if (!(browser instanceof HTMLElement)) {
                return null;
            }

            return {
                browserTop: Math.round(browser.getBoundingClientRect().top),
                builderBottom: Math.round(
                    builder.getBoundingClientRect().bottom,
                ),
                resultRowCount: resultRows.length,
            };
        });

        expect(layout).not.toBeNull();
        expect(layout?.browserTop).toBeGreaterThanOrEqual(
            layout?.builderBottom ?? 0,
        );
        expect(layout?.resultRowCount).toBe(0);
    });

    test("hides the search results summary box under the default combined files view", async ({
        page,
    }) => {
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        const summaryBox = page.locator('[data-results-table-summary="true"]');
        const combinedBrowserShell = page.locator(
            '[data-search-combined-file-browser="true"]',
        );

        await expect(combinedBrowserShell).toHaveAttribute(
            "data-search-file-mode",
            "combined",
        );
        await expect(summaryBox).toHaveCount(0);

        const summaryEvidence = await page.evaluate(() => {
            const summary = document.querySelector(
                '[data-results-table-summary="true"]',
            );
            const combinedShell = document.querySelector(
                '[data-search-combined-file-browser="true"]',
            );
            const browser = document.querySelector(
                '[data-file-browser="true"]',
            );

            if (
                !(combinedShell instanceof HTMLElement) ||
                !(browser instanceof HTMLElement)
            ) {
                return null;
            }

            const combinedShellRect = combinedShell.getBoundingClientRect();
            const browserRect = browser.getBoundingClientRect();

            return {
                browserBottom: Math.round(browserRect.bottom),
                browserTop: Math.round(browserRect.top),
                combinedShellBottom: Math.round(combinedShellRect.bottom),
                combinedShellTop: Math.round(combinedShellRect.top),
                matchingHeadingCount: document.querySelectorAll(
                    '[data-results-table-summary="true"] h2',
                ).length,
                searchFileMode: combinedShell.dataset.searchFileMode ?? null,
                summaryCount: document.querySelectorAll(
                    '[data-results-table-summary="true"]',
                ).length,
                summaryText:
                    summary instanceof HTMLElement ? summary.innerText : null,
            };
        });

        await writeEvidence(
            page,
            "search-combined-file-browser-summary-under-combined-view.png",
            {
                searchUrl: page.url(),
                summaryEvidence,
            },
        );

        expect(summaryEvidence).not.toBeNull();
        expect(summaryEvidence?.searchFileMode).toBe("combined");
        expect(summaryEvidence?.summaryCount).toBe(0);
        expect(summaryEvidence?.matchingHeadingCount).toBe(0);
        await expect(matchingRows(page)).toHaveCount(0);
    });

    test("reproduces result rows still rendering under the default combined files view", async ({
        page,
    }) => {
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        const combinedBrowserShell = page.locator(
            '[data-search-combined-file-browser="true"]',
        );
        await expect(combinedBrowserShell).toHaveAttribute(
            "data-search-file-mode",
            "combined",
        );
        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(1);

        const combinedModeEvidence = await page.evaluate(() => {
            const combinedShell = document.querySelector(
                '[data-search-combined-file-browser="true"]',
            );
            const fileBrowser = document.querySelector(
                '[data-file-browser="true"]',
            );
            const table = document.querySelector("table");
            const rows = [
                ...document.querySelectorAll<HTMLElement>(
                    'tbody tr[data-result-row="true"]',
                ),
            ];

            if (
                !(combinedShell instanceof HTMLElement) ||
                !(fileBrowser instanceof HTMLElement)
            ) {
                return null;
            }

            const combinedShellRect = combinedShell.getBoundingClientRect();
            const fileBrowserRect = fileBrowser.getBoundingClientRect();
            const tableRect =
                table instanceof HTMLElement
                    ? table.getBoundingClientRect()
                    : null;
            const firstRowRect = rows[0]?.getBoundingClientRect() ?? null;

            return {
                combinedShellBottom: Math.round(combinedShellRect.bottom),
                combinedShellTop: Math.round(combinedShellRect.top),
                fileBrowserBottom: Math.round(fileBrowserRect.bottom),
                fileBrowserTop: Math.round(fileBrowserRect.top),
                firstResultRowTop: firstRowRect
                    ? Math.round(firstRowRect.top)
                    : null,
                resultRowCount: rows.length,
                resultRowText: rows.map((row) => row.innerText),
                searchFileMode: combinedShell.dataset.searchFileMode ?? null,
                tableTop: tableRect ? Math.round(tableRect.top) : null,
            };
        });

        await writeEvidence(
            page,
            "search-combined-file-browser-result-rows-under-combined-repro.png",
            {
                combinedModeEvidence,
                searchUrl: page.url(),
            },
        );

        expect(combinedModeEvidence).not.toBeNull();
        expect(
            combinedModeEvidence?.resultRowCount,
            "Combined files view should not render result rows under the file browser",
        ).toBe(0);
    });

    test("does not show the matching result sets box under the result rows view", async ({
        page,
    }) => {
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        await page.getByRole("button", { name: "Result sets" }).click();

        const combinedBrowserShell = page.locator(
            '[data-search-combined-file-browser="true"]',
        );
        await expect(combinedBrowserShell).toHaveAttribute(
            "data-search-file-mode",
            "rows",
        );

        await writeEvidence(
            page,
            "search-combined-file-browser-result-rows-matching-box.png",
        );

        await expect(
            page.getByRole("heading", { name: "Matching result sets" }),
        ).toHaveCount(0);
        await expect(matchingRows(page)).toHaveCount(2);
        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(0);
    });

    test("narrows the combined file browser when the search filters to one sample", async ({
        page,
    }) => {
        await page.goto(
            `/?pipeline_name=${encodeURIComponent(pipelineName)}&sample=${encodeURIComponent(sampleAlpha)}`,
        );

        await writeEvidence(
            page,
            "search-combined-file-browser-missing-sample-filter.png",
        );

        const combinedBrowser = page.locator('[data-file-browser="true"]');

        await expect(combinedBrowser).toHaveCount(1);
        await expect(combinedBrowser).toContainText(
            "alpha-expression-counts.tsv",
        );
        await expect(combinedBrowser).not.toContainText(
            "beta-expression-counts.tsv",
        );
    });

    test("shows the seeded combined galleries fixture and does not emit duplicate key warnings", async ({
        page,
    }) => {
        const consoleMessages: CapturedConsoleMessage[] = [];
        const seededPipelineName = "wtsi/combined-galleries-demo";

        page.on("console", (message) => {
            consoleMessages.push({
                location: message.location(),
                text: message.text(),
                type: message.type(),
            });
        });

        await page.goto(
            `/?pipeline_name=${encodeURIComponent(seededPipelineName)}`,
        );

        await expect(
            page.locator('[data-search-combined-file-browser="true"]'),
        ).toBeVisible();
        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(1);
        const combinedBrowser = page.locator('[data-file-browser="true"]');
        await expect(
            page.locator("tbody tr[data-result-row='true']"),
        ).toHaveCount(0);
        const browserEvidence =
            await collectCombinedGalleriesRootEvidence(page);
        const rootFileGroup = browserEvidence.directoryFileGroups.find(
            (group) => group.directory === browserEvidence.rootDirectory,
        );
        expect(rootFileGroup?.files ?? []).toEqual([]);

        const sampleAOutputDirectory = path.join(
            repoRoot,
            ".docs",
            "results-web",
            "fixtures",
            "files",
            "sibling-gallery-runs",
            "sample-a",
        );
        const sampleBOutputDirectory = path.join(
            repoRoot,
            ".docs",
            "results-web",
            "fixtures",
            "files",
            "sibling-gallery-runs",
            "sample-b",
        );

        await selectDirectory(page, sampleAOutputDirectory);
        await expect(combinedBrowser).toContainText("blue-plot.svg");
        await expect(combinedBrowser).not.toContainText("orange-heatmap.svg");

        await selectDirectory(page, sampleBOutputDirectory);
        await expect(combinedBrowser).toContainText("orange-heatmap.svg");
        await expect(combinedBrowser).not.toContainText("blue-plot.svg");
        await page.waitForTimeout(1000);

        const duplicateKeyMessages = consoleMessages.filter((message) =>
            message.text.includes("Encountered two children with the same key"),
        );

        await writeEvidence(
            page,
            "search-combined-file-browser-galleries-duplicate-key.png",
            {
                consoleMessages,
                duplicateKeyMessages,
                searchUrl: page.url(),
            },
        );

        expect(duplicateKeyMessages).toEqual([]);

        await page.getByRole("button", { name: "Result sets" }).click();
        const resultRows = page.locator("tbody tr[data-result-row='true']");
        await expect(resultRows).toHaveCount(2);
        const resultRowText = (await resultRows.allTextContents()).join("\n");
        expect(resultRowText).toContain("sibling-gallery-runs/sample-a");
        expect(resultRowText).toContain("sibling-gallery-runs/sample-b");
        expect(resultRowText).not.toContain("combined-galleries-demo/sample-a");
        expect(resultRowText).not.toContain("combined-galleries-demo/sample-b");
        expect(resultRowText).not.toContain("galleries-demo/sample-a");
        expect(resultRowText).not.toContain("galleries-demo/sample-b");

        await page.goto(
            `/?pipeline_name=${encodeURIComponent(seededPipelineName)}&sample=${encodeURIComponent("gallery-alpha")}`,
        );

        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "blue-plot.svg",
        );
        await expect(
            page.locator('[data-file-browser="true"]'),
        ).not.toContainText("orange-heatmap.svg");
        await page.getByRole("button", { name: "Result sets" }).click();
        await expect(
            page.locator("tbody tr[data-result-row='true']"),
        ).toHaveCount(1);
    });

    test("characterizes combined galleries files appearing in the common parent directory", async ({
        page,
    }) => {
        const seededPipelineName = "wtsi/combined-galleries-demo";

        await page.goto(
            `/?pipeline_name=${encodeURIComponent(seededPipelineName)}`,
        );

        await expect(page.locator('[data-file-browser="true"]')).toHaveCount(1);

        const browserEvidence =
            await collectCombinedGalleriesRootEvidence(page);
        await writeEvidence(
            page,
            "search-combined-file-browser-galleries-parent-files-repro.png",
            {
                browserEvidence,
                searchUrl: page.url(),
            },
        );

        expect(browserEvidence.rootDirectory).toContain("sibling-gallery-runs");
        expect(browserEvidence.rootDirectory).not.toContain("/sample-a");
        expect(browserEvidence.rootDirectory).not.toContain("/sample-b");

        const rootFiles =
            browserEvidence.directoryFileGroups[0]?.files.map(
                (file) => file.path,
            ) ?? [];
        const rootSampleFiles = rootFiles.filter(
            (file) =>
                file.includes("/sample-a/") || file.includes("/sample-b/"),
        );

        expect(rootSampleFiles).toEqual([]);

        await page.getByRole("button", { name: "Result sets" }).click();
        const resultRows = page.locator("tbody tr[data-result-row='true']");
        await expect(resultRows).toHaveCount(2);

        const resultRowText = (await resultRows.allTextContents()).join("\n");
        expect(resultRowText).toContain("sibling-gallery-runs/sample-a");
        expect(resultRowText).toContain("sibling-gallery-runs/sample-b");
    });

    test("reproduces missing subfolder preview affordance for seeded galleries sample-a", async ({
        page,
    }) => {
        test.setTimeout(120_000);

        const seededPipelineName = "wtsi/combined-galleries-demo";
        const sampleAlphaSearchUrl = `/?pipeline_name=${encodeURIComponent(seededPipelineName)}&sample=${encodeURIComponent("gallery-alpha")}`;

        await page.goto(sampleAlphaSearchUrl);

        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "overview",
        );

        const combinedSearchEvidence =
            await collectSubfolderPreviewEvidence(page);
        await writeEvidence(
            page,
            "search-combined-file-browser-sample-alpha-subfolder-preview-missing.png",
            {
                searchUrl: page.url(),
                subfolderPreview: combinedSearchEvidence,
            },
        );
        await expect
            .soft(
                combinedSearchEvidence.subfolderPreviewInputCount,
                "filtered combined search should offer subfolder previews for the overview folder",
            )
            .toBeGreaterThan(0);

        await page.getByRole("button", { name: "Result sets" }).click();
        await expect(
            page.locator("tbody tr[data-result-row='true']"),
        ).toHaveCount(1);
        const detailHref = await page
            .locator("tbody tr[data-result-row='true']")
            .filter({ hasText: "galleries_sample_a" })
            .locator("a")
            .first()
            .getAttribute("href");

        expect(detailHref).toBeTruthy();

        await page.goto(detailHref ?? "/");
        await expect(page.getByText("galleries_sample_a")).toBeVisible();
        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "overview",
        );

        const detailEvidence = await collectSubfolderPreviewEvidence(page);
        await writeEvidence(
            page,
            "result-detail-galleries-sample-a-subfolder-preview-missing.png",
            {
                detailUrl: page.url(),
                subfolderPreview: detailEvidence,
            },
        );
        await expect
            .soft(
                detailEvidence.subfolderPreviewInputCount,
                "direct result page should offer subfolder previews for the overview folder",
            )
            .toBeGreaterThan(0);

        await page.goto(
            `/?pipeline_name=${encodeURIComponent("wtsi/galleries-demo")}`,
        );
        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "sample-a",
        );

        await page
            .locator('[data-file-browser="true"]')
            .locator('[data-file-browser-control-trigger="preview-modes"]')
            .first()
            .click();
        await page.getByLabel("Subfolder previews").check();

        const parentSearchEvidence =
            await collectSubfolderPreviewEvidence(page);
        await writeEvidence(
            page,
            "search-galleries-demo-subfolder-preview-overview.png",
            {
                searchUrl: page.url(),
                subfolderPreview: parentSearchEvidence,
            },
        );

        await expect
            .soft(
                page.locator('[data-subdir-preview-card*="sample-a/overview"]'),
                "parent galleries-demo subfolder previews should include sample-a overview files",
            )
            .toHaveCount(2);
    });

    test("reproduces overview subfolder previews on the single seeded galleries-demo detail page", async ({
        page,
    }) => {
        test.setTimeout(120_000);

        const parentPipelineName = "wtsi/galleries-demo";
        const parentSearchUrl = `/?pipeline_name=${encodeURIComponent(parentPipelineName)}`;
        const parentOutputDirectory = path.join(
            repoRoot,
            ".docs",
            "results-web",
            "fixtures",
            "files",
            "galleries-demo",
        );
        const sampleAPath = path.join(parentOutputDirectory, "sample-a");
        const overviewPath = path.join(sampleAPath, "overview");

        await page.goto(parentSearchUrl);

        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "sample-a",
        );
        await page.getByRole("button", { name: "Result sets" }).click();

        const resultRows = page.locator("tbody tr[data-result-row='true']");
        await expect(resultRows).toHaveCount(1);
        await expect(resultRows.first()).toContainText(parentPipelineName);
        const resultRowText = (await resultRows.first().textContent()) ?? "";
        expect(resultRowText).toContain(
            ".docs/results-web/fixtures/files/galleries-demo",
        );
        expect(resultRowText).not.toContain("combined-galleries-demo");

        const detailHref = await resultRows
            .first()
            .locator("a")
            .first()
            .getAttribute("href");

        expect(detailHref).toBeTruthy();

        await page.goto(detailHref ?? "/");
        await expect(
            page.getByRole("heading", { level: 1, name: parentPipelineName }),
        ).toBeVisible({ timeout: 30000 });
        await expect(page.locator('[data-file-browser="true"]')).toContainText(
            "sample-a",
        );

        await selectDirectory(page, sampleAPath);

        const sampleAControls = page.locator(
            `[data-file-browser-folder-controls="${sampleAPath}"]`,
        );
        await expect(sampleAControls).toBeVisible();
        await openPreviewModes(sampleAControls);
        await sampleAControls
            .locator('input[aria-label="Subfolder previews"]')
            .check();

        const detailEvidence = await collectSubfolderPreviewEvidence(page);
        await writeEvidence(
            page,
            "result-detail-galleries-demo-overview-subfolder-preview.png",
            {
                detailHref,
                detailUrl: page.url(),
                expectedOverviewCards: [
                    path.join(overviewPath, "navy-summary.svg"),
                    path.join(overviewPath, "gold-summary.svg"),
                ],
                parentOutputDirectory,
                parentSearchUrl,
                resultRowText,
                sampleAPath,
                subfolderPreview: detailEvidence,
            },
        );

        await expect(
            page.locator(`[data-subdir-preview-strip="${overviewPath}"]`),
        ).toBeVisible();
        await expect(
            page.locator(
                `[data-subdir-preview-card="${path.join(
                    overviewPath,
                    "navy-summary.svg",
                )}"]`,
            ),
        ).toBeVisible();
        await expect(
            page.locator(
                `[data-subdir-preview-card="${path.join(
                    overviewPath,
                    "gold-summary.svg",
                )}"]`,
            ),
        ).toBeVisible();
    });
});
