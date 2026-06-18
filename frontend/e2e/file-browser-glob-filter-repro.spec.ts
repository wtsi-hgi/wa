import { mkdirSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type BrowserContext, type Page } from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultRegistration,
    type ResultSet,
} from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const fixtureRoot = path.join(evidenceDir, "file-browser-glob-filter-fixture");
const reproToken = `glob-filter-${Date.now()}-${process.pid}`;
const requester = `glob-filter-requester-${reproToken}`;
const expectedGlobPattern = "**/*.tsv";
const pipelineNames = ["wa/glob-filter-alpha", "wa/glob-filter-beta"] as const;

let registeredResults: ResultSet[] = [];

type FilterEvidence = {
    directoryPaths: string[];
    filePaths: string[];
    headerControls: Array<{
        ariaLabel: string | null;
        placeholder: string | null;
        tag: string;
        text: string;
        title: string | null;
        type: string | null;
    }>;
    headerSaveButtonCount: number;
    headerText: string | null;
    headerTextboxCount: number;
    label: string;
    localStorageEntries: Array<[string, string]>;
    screenshotPath: string;
    visibleText: string | null;
};

type ReloadConsoleEvidence = {
    location: string;
    text: string;
    type: string;
};

type MatchingReloadMessage = {
    source: string;
    text: string;
};

test.beforeAll(() => {
    registeredResults = [
        registerGlobFilterResult({
            files: [
                {
                    content: "gene\talpha\nENSG000001\t42\n",
                    relativePath: path.join("reports", "alpha-summary.tsv"),
                },
                {
                    content: "alpha run completed\n",
                    relativePath: path.join("logs", "alpha-run.log"),
                },
                {
                    content: '{"sample":"alpha","pass":true}\n',
                    relativePath: path.join("metrics", "alpha-qc.json"),
                },
            ],
            pipelineName: pipelineNames[0],
            runKey: `runid=260618&unique=${reproToken}-alpha`,
            sample: "glob-filter-alpha",
        }),
        registerGlobFilterResult({
            files: [
                {
                    content: "gene\tbeta\nENSG000001\t84\n",
                    relativePath: path.join("reports", "beta-summary.tsv"),
                },
                {
                    content:
                        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 80"><rect width="120" height="80" fill="#f8fafc"/><circle cx="60" cy="40" r="24" fill="#2563eb"/></svg>\n',
                    relativePath: path.join("plots", "beta-plot.svg"),
                },
                {
                    content: "beta run completed\n",
                    relativePath: path.join("logs", "beta-run.log"),
                },
            ],
            pipelineName: pipelineNames[1],
            runKey: `runid=260618&unique=${reproToken}-beta`,
            sample: "glob-filter-beta",
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

function registerGlobFilterResult({
    files,
    pipelineName,
    runKey,
    sample,
}: {
    files: Array<{ content: string; relativePath: string }>;
    pipelineName: string;
    runKey: string;
    sample: string;
}): ResultSet {
    const outputDirectory = path.join(
        fixtureRoot,
        pipelineName.replaceAll("/", "-"),
        sample,
    );
    const registeredFiles = files.map((file) => {
        const outputPath = path.join(outputDirectory, file.relativePath);

        mkdirSync(path.dirname(outputPath), { recursive: true });
        writeFileSync(outputPath, file.content);

        const stats = statSync(outputPath);

        return {
            kind: "output" as const,
            mtime: stats.mtime.toISOString(),
            path: outputPath,
            size: stats.size,
        };
    });
    const registration: ResultRegistration = {
        command: `nextflow run ${pipelineName} --sample ${sample}`,
        files: registeredFiles,
        metadata: {
            cohort: "glob-filter-repro",
            sample,
        },
        operator: "glob-filter-operator",
        output_directory: outputDirectory,
        pipeline_identifier: `https://github.com/wtsi-hgi/${pipelineName}`,
        pipeline_name: pipelineName,
        pipeline_version: "2026.06.18",
        requester,
        run_key: runKey,
    };

    return registerResult(registration);
}

async function openSearchResultFileBrowser(page: Page): Promise<void> {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(`/?user=${encodeURIComponent(requester)}`);
    await expect(
        page.locator('[data-search-combined-file-browser="true"]'),
    ).toHaveAttribute("data-search-file-mode", "combined");
    await expect(page.locator('[data-file-browser="true"]')).toBeVisible();
}

async function openResultDetailFileBrowser(
    context: BrowserContext,
    page: Page,
): Promise<void> {
    await installResultsAuthCookie(context);
    const resultId = registeredResults[0]?.id;

    if (!resultId) {
        throw new Error("Missing registered result for detail repro");
    }

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(`/results/${resultId}`);
    await expect(page.locator('[data-file-browser="true"]')).toBeVisible();
}

async function writeFilterEvidence(
    page: Page,
    label: string,
    screenshotName: string,
): Promise<FilterEvidence> {
    mkdirSync(evidenceDir, { recursive: true });

    const screenshotPath = path.join(evidenceDir, screenshotName);
    const evidencePath = screenshotPath.replace(/\.png$/, ".json");
    const evidence = await page.evaluate(
        ({ label, screenshotPath }) => {
            const fileBrowser = document.querySelector<HTMLElement>(
                '[data-file-browser="true"]',
            );
            const header = document.querySelector<HTMLElement>(
                '[data-file-browser-header="true"]',
            );
            const headerTextboxes = [
                ...((header?.querySelectorAll(
                    'input:not([type]), input[type="search"], input[type="text"], textarea, [role="textbox"]',
                ) ?? []) as NodeListOf<HTMLElement>),
            ];
            const headerSaveButtons = [
                ...((header?.querySelectorAll('button, [role="button"]') ??
                    []) as NodeListOf<HTMLElement>),
            ].filter((element) => {
                const labelText = [
                    element.textContent,
                    element.getAttribute("aria-label"),
                    element.getAttribute("title"),
                ]
                    .filter(Boolean)
                    .join(" ");

                return /save/i.test(labelText);
            });
            const headerControls = [
                ...((header?.querySelectorAll(
                    'input, textarea, button, [role="button"], [role="textbox"]',
                ) ?? []) as NodeListOf<HTMLElement>),
            ].map((element) => ({
                ariaLabel: element.getAttribute("aria-label"),
                placeholder: element.getAttribute("placeholder"),
                tag: element.tagName.toLowerCase(),
                text: element.textContent?.trim() ?? "",
                title: element.getAttribute("title"),
                type: element.getAttribute("type"),
            }));
            const localStorageEntries = Object.entries(window.localStorage)
                .filter(([key]) => /file|browser|glob|filter/i.test(key))
                .sort(([left], [right]) => left.localeCompare(right));

            return {
                directoryPaths: [
                    ...document.querySelectorAll<HTMLElement>(
                        "[data-directory-path]",
                    ),
                ].map(
                    (element) =>
                        element.dataset.directoryPath ??
                        element.getAttribute("data-directory-path") ??
                        "",
                ),
                filePaths: [
                    ...document.querySelectorAll<HTMLElement>(
                        "[data-file-path]",
                    ),
                ].map(
                    (element) =>
                        element.dataset.filePath ??
                        element.getAttribute("data-file-path") ??
                        "",
                ),
                headerControls,
                headerSaveButtonCount: headerSaveButtons.length,
                headerText: header?.innerText ?? null,
                headerTextboxCount: headerTextboxes.length,
                label,
                localStorageEntries,
                screenshotPath,
                visibleText: fileBrowser?.innerText.slice(0, 3000) ?? null,
            };
        },
        { label, screenshotPath },
    );

    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: screenshotPath,
    });
    writeFileSync(
        evidencePath,
        `${JSON.stringify(
            {
                ...evidence,
                expected: {
                    globPattern: expectedGlobPattern,
                    matchingPipelines: [...pipelineNames],
                    persistenceScope:
                        "search browsers should use a deduplicated matching-pipeline key; detail browsers should use the result pipeline key",
                },
                pageUrl: page.url(),
            },
            null,
            2,
        )}\n`,
    );

    return evidence;
}

async function writeRefreshErrorEvidence({
    consoleMessages,
    label,
    page,
    pageErrors,
    screenshotName,
}: {
    consoleMessages: ReloadConsoleEvidence[];
    label: string;
    page: Page;
    pageErrors: string[];
    screenshotName: string;
}): Promise<{
    bodyText: string;
    evidencePath: string;
    matchingMessages: MatchingReloadMessage[];
    overlayText: string | null;
    screenshotPath: string;
}> {
    mkdirSync(evidenceDir, { recursive: true });

    const screenshotPath = path.join(evidenceDir, screenshotName);
    const evidencePath = screenshotPath.replace(/\.png$/, ".json");
    const pageSnapshot = await page.evaluate(() => {
        const portal = document.querySelector("nextjs-portal") as
            | (HTMLElement & { shadowRoot?: ShadowRoot | null })
            | null;

        return {
            bodyText: document.body.innerText,
            overlayText:
                portal?.shadowRoot?.textContent ?? portal?.textContent ?? null,
        };
    });
    const scriptTagErrorPattern =
        /Encountered a script tag while rendering React component|Scripts inside React components are never executed/i;
    const matchCandidates: MatchingReloadMessage[] = [
        ...consoleMessages.map((message) => ({
            source: `console:${message.type}`,
            text: message.text,
        })),
        ...pageErrors.map((message) => ({
            source: "pageerror",
            text: message,
        })),
        {
            source: "nextjs-portal",
            text: pageSnapshot.overlayText ?? "",
        },
        {
            source: "body",
            text: pageSnapshot.bodyText,
        },
    ];
    const matchingMessages = matchCandidates
        .filter((message) => scriptTagErrorPattern.test(message.text))
        .map((message) => {
            const match = scriptTagErrorPattern.exec(message.text);
            const matchIndex = match?.index ?? 0;
            const snippetStart = Math.max(0, matchIndex - 300);
            const snippetEnd = Math.min(message.text.length, matchIndex + 1200);

            scriptTagErrorPattern.lastIndex = 0;

            return {
                source: message.source,
                text: message.text.slice(snippetStart, snippetEnd),
            };
        });

    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: screenshotPath,
    });
    writeFileSync(
        evidencePath,
        `${JSON.stringify(
            {
                bodyText: pageSnapshot.bodyText.slice(0, 4000),
                consoleMessages,
                expected: {
                    globPattern: expectedGlobPattern,
                    noNextScriptTagErrorAfterRefresh: true,
                },
                label,
                matchingMessages,
                overlayText: pageSnapshot.overlayText?.slice(0, 4000) ?? null,
                pageErrors,
                pageUrl: page.url(),
                screenshotPath,
            },
            null,
            2,
        )}\n`,
    );

    return {
        bodyText: pageSnapshot.bodyText,
        evidencePath,
        matchingMessages,
        overlayText: pageSnapshot.overlayText,
        screenshotPath,
    };
}

test("filters and persists a search-result file browser glob", async ({
    page,
}) => {
    await openSearchResultFileBrowser(page);

    const globInput = page.getByLabel("Filter files by glob");
    const saveButton = page.getByRole("button", {
        name: "Save file glob filter",
    });

    await expect(globInput).toBeVisible();
    await expect(saveButton).toBeEnabled();
    await globInput.fill(expectedGlobPattern);
    await expect(globInput).toHaveValue(expectedGlobPattern);
    await expect(page.locator('[data-directory-path$="/reports"]')).toHaveCount(
        2,
    );
    await expect(page.locator('[data-directory-path$="/logs"]')).toHaveCount(0);
    await expect(page.locator('[data-directory-path$="/metrics"]')).toHaveCount(
        0,
    );
    await expect(page.locator('[data-directory-path$="/plots"]')).toHaveCount(
        0,
    );

    await page.locator('[data-directory-path$="/reports"]').first().click();
    await expect(page.locator('[data-file-path$=".tsv"]')).toHaveCount(1);
    await expect(page.locator('[data-file-path$=".log"]')).toHaveCount(0);
    await expect(page.locator('[data-file-path$=".json"]')).toHaveCount(0);
    await expect(page.locator('[data-file-path$=".svg"]')).toHaveCount(0);
    await saveButton.click();

    const evidence = await writeFilterEvidence(
        page,
        "search-result file browser",
        "file-browser-glob-filter-search-postfix.png",
    );

    expect(evidence.headerText?.toLowerCase()).toContain("file browser");
    expect(
        evidence.directoryPaths.some((directoryPath) =>
            directoryPath.includes("glob-filter-alpha"),
        ),
    ).toBe(true);
    expect(
        evidence.directoryPaths.some((directoryPath) =>
            directoryPath.includes("glob-filter-beta"),
        ),
    ).toBe(true);
    expect(
        evidence.headerTextboxCount,
        "File Browser header should include a glob filter input in line with the title",
    ).toBeGreaterThan(0);
    expect(
        evidence.headerSaveButtonCount,
        "File Browser header should include a save button for persisting the glob filter",
    ).toBeGreaterThan(0);
    expect(
        evidence.localStorageEntries.some(
            ([key, value]) =>
                key.includes("wa:file-browser:glob-filter:pipelines:") &&
                key.includes(pipelineNames[0]) &&
                key.includes(pipelineNames[1]) &&
                value === expectedGlobPattern,
        ),
    ).toBe(true);

    await page.reload();
    await expect(globInput).toHaveValue(expectedGlobPattern);
    await expect(page.locator('[data-directory-path$="/logs"]')).toHaveCount(0);
});

test("filters and persists a result-detail file browser glob", async ({
    context,
    page,
}) => {
    await openResultDetailFileBrowser(context, page);

    const globInput = page.getByLabel("Filter files by glob");
    const saveButton = page.getByRole("button", {
        name: "Save file glob filter",
    });

    await expect(globInput).toBeVisible();
    await expect(saveButton).toBeEnabled();
    await globInput.fill(expectedGlobPattern);
    await expect(globInput).toHaveValue(expectedGlobPattern);
    await expect(page.locator('[data-directory-path$="/reports"]')).toHaveCount(
        1,
    );
    await expect(page.locator('[data-directory-path$="/logs"]')).toHaveCount(0);
    await expect(page.locator('[data-directory-path$="/metrics"]')).toHaveCount(
        0,
    );
    await page.locator('[data-directory-path$="/reports"]').first().click();
    await expect(page.locator('[data-file-path$=".tsv"]')).toHaveCount(1);
    await expect(page.locator('[data-file-path$=".log"]')).toHaveCount(0);
    await expect(page.locator('[data-file-path$=".json"]')).toHaveCount(0);
    await saveButton.click();

    const evidence = await writeFilterEvidence(
        page,
        "result-detail file browser",
        "file-browser-glob-filter-detail-postfix.png",
    );

    expect(evidence.headerText?.toLowerCase()).toContain("file browser");
    expect(
        evidence.directoryPaths.some((directoryPath) =>
            directoryPath.includes("glob-filter-alpha"),
        ),
    ).toBe(true);
    expect(
        evidence.directoryPaths.some((directoryPath) =>
            directoryPath.endsWith("/reports"),
        ),
        "The detail browser fixture should include a TSV-containing reports directory for a future glob filter assertion",
    ).toBe(true);
    expect(
        evidence.directoryPaths.some(
            (directoryPath) =>
                directoryPath.endsWith("/logs") ||
                directoryPath.endsWith("/metrics"),
        ),
        "The detail browser should hide non-matching file-type directories after applying the glob filter",
    ).toBe(false);
    expect(
        evidence.headerTextboxCount,
        "Result-detail File Browser header should include a glob filter input in line with the title",
    ).toBeGreaterThan(0);
    expect(
        evidence.headerSaveButtonCount,
        "Result-detail File Browser header should include a save button for persisting the glob filter",
    ).toBeGreaterThan(0);
    expect(
        evidence.localStorageEntries.some(
            ([key, value]) =>
                key.includes(
                    `wa:file-browser:glob-filter:pipeline:${pipelineNames[0]}`,
                ) && value === expectedGlobPattern,
        ),
    ).toBe(true);

    await page.reload();
    await expect(globInput).toHaveValue(expectedGlobPattern);
    await expect(page.locator('[data-directory-path$="/logs"]')).toHaveCount(0);
});

test("does not emit a Next script-tag error after reloading a saved search file-browser glob", async ({
    page,
}) => {
    await openSearchResultFileBrowser(page);

    const globInput = page.getByLabel("Filter files by glob");
    const saveButton = page.getByRole("button", {
        name: "Save file glob filter",
    });

    await globInput.fill(expectedGlobPattern);
    await saveButton.click();

    const consoleMessages: ReloadConsoleEvidence[] = [];
    const pageErrors: string[] = [];

    page.on("console", (message) => {
        consoleMessages.push({
            location: message.location().url,
            text: message.text(),
            type: message.type(),
        });
    });
    page.on("pageerror", (error) => {
        pageErrors.push(error.stack ?? error.message);
    });

    await page.reload();
    await expect(page.getByLabel("Filter files by glob")).toHaveValue(
        expectedGlobPattern,
    );
    await page.waitForTimeout(750);

    const evidence = await writeRefreshErrorEvidence({
        consoleMessages,
        label: "search-result file browser refresh after saved glob",
        page,
        pageErrors,
        screenshotName: "file-browser-glob-filter-refresh-script-tag-repro.png",
    });

    expect(
        evidence.matchingMessages,
        `Reloading after saving the file-browser glob filter should not emit Next's script-tag render error. Evidence: ${evidence.evidencePath}`,
    ).toEqual([]);
});
