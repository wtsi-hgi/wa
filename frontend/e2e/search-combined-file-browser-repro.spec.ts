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
        `${JSON.stringify({ ...evidence, screenshotPath }, null, 2)}\n`,
    );
}

test.describe("search combined file browser repro", () => {
    test("shows a locked combined file browser state for logged-out matching results", async ({
        context,
        page,
    }) => {
        await context.clearCookies();
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        await expect(page.getByText("Showing search results")).toBeVisible();
        await expect(matchingRows(page)).toHaveCount(2);
        await expect(lockedMatchingRows(page)).toHaveCount(2);

        await writeEvidence(
            page,
            "search-combined-file-browser-logged-out-locked.png",
        );

        const combinedBrowser = page.locator(
            '[data-search-combined-file-browser="true"]',
        );

        await expect(combinedBrowser).toHaveCount(1);
        await expect(combinedBrowser).toContainText("Combined files");
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
    });

    test("shows one default file browser for all files across matching result sets", async ({
        page,
    }) => {
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        await expect(page.getByText("Showing search results")).toBeVisible();
        await expect(matchingRows(page)).toHaveCount(2);

        await writeEvidence(
            page,
            "search-combined-file-browser-missing-prefilter.png",
        );

        const searchBuilder = page.locator('[data-search-builder="true"]');
        const combinedBrowser = page.locator('[data-file-browser="true"]');

        await expect(combinedBrowser).toHaveCount(1);
        await expect(combinedBrowser).toContainText(
            "alpha-expression-counts.tsv",
        );
        await expect(combinedBrowser).toContainText(
            "beta-expression-counts.tsv",
        );

        const layout = await searchBuilder.evaluate((builder) => {
            const browser = document.querySelector(
                '[data-file-browser="true"]',
            );
            const table = document.querySelector("table");

            if (
                !(browser instanceof HTMLElement) ||
                !(table instanceof HTMLElement)
            ) {
                return null;
            }

            return {
                browserTop: Math.round(browser.getBoundingClientRect().top),
                builderBottom: Math.round(
                    builder.getBoundingClientRect().bottom,
                ),
                tableTop: Math.round(table.getBoundingClientRect().top),
            };
        });

        expect(layout).not.toBeNull();
        expect(layout?.browserTop).toBeGreaterThanOrEqual(
            layout?.builderBottom ?? 0,
        );
        expect(layout?.browserTop).toBeLessThan(layout?.tableTop ?? 0);
    });

    test("does not show the matching result sets box under the result rows view", async ({
        page,
    }) => {
        await page.goto(`/?pipeline_name=${encodeURIComponent(pipelineName)}`);

        await expect(matchingRows(page)).toHaveCount(2);

        await page.getByRole("button", { name: "Result rows" }).click();

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

        await expect(page.getByText("Showing search results")).toBeVisible();
        await expect(matchingRows(page)).toHaveCount(1);
        await expect(matchingRows(page).first()).toContainText(
            path.join("samples", "alpha", "final"),
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
});
