import { mkdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Page } from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultRegistration,
    type ResultSet,
} from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const fixtureRoot = path.join(evidenceDir, "generic-search-repro-fixture");
const screenshotPath = path.join(
    evidenceDir,
    "generic-search-current-ui-repro.png",
);
const substringScreenshotPath = path.join(
    evidenceDir,
    "generic-search-substring-current-repro.png",
);
const outputSubstringScreenshotPath = path.join(
    evidenceDir,
    "generic-search-output-directory-substring-choice-repro.png",
);
const outputSubstringPostfixScreenshotPath = path.join(
    evidenceDir,
    "generic-search-output-directory-substring-choice-postfix.png",
);
const evidencePath = path.join(
    evidenceDir,
    "generic-search-current-ui-repro.json",
);
const substringEvidencePath = path.join(
    evidenceDir,
    "generic-search-substring-current-repro.json",
);
const outputSubstringEvidencePath = path.join(
    evidenceDir,
    "generic-search-output-directory-substring-choice-repro.json",
);
const outputSubstringPostfixEvidencePath = path.join(
    evidenceDir,
    "generic-search-output-directory-substring-choice-postfix.json",
);

const pipelineName = "wa/generic-search-repro";
const sharedNeedle = "needle-260618";
const assayValue = `alpha-${sharedNeedle}-omega`;
const outputSubstringPipelineName = "wa/generic-output-substring-repro";
const outputDirectorySubstring = "output-substring-260618";
const pinnedFieldLabels = [
    "Pipeline name",
    "Unique",
    "Study",
    "Sample",
    "Requester",
];
const pinnedFieldKeys = ["pipeline_name", "run_key", "study", "sample", "user"];

let registeredResults: ResultSet[] = [];

type SearchUiEvidence = {
    addFilterButtonText: string | null;
    fieldOptionKeys: string[];
    fieldOptionLabels: string[];
    genericInputCandidates: Array<{
        ariaLabel: string | null;
        dataGenericSearchInput: string | null;
        id: string;
        placeholder: string | null;
    }>;
    permanentFieldKeys: string[];
    permanentFieldLabels: string[];
    visibleText: string;
};

type SubstringEvidence = {
    bodyText: string;
    matchedReproRows: number;
    resultRows: number;
    url: string;
};

type OutputSubstringEvidence = {
    genericInputValue: string | null;
    optionLabels: string[];
    outputDirectoryOptions: string[];
    typedSubstringOptionCount: number;
};

type OutputSubstringPostfixEvidence = {
    bodyText: string;
    matchedOutputDirectories: string[];
    registrationInfoLabels: string[];
    resultRows: number;
    url: string;
};

test.beforeAll(() => {
    mkdirSync(evidenceDir, { recursive: true });
    rmSync(fixtureRoot, { force: true, recursive: true });
    registeredResults = [
        registerGenericSearchResult(),
        registerOutputSubstringResult("alpha"),
        registerOutputSubstringResult("beta"),
    ];
});

test.afterAll(() => {
    for (const result of registeredResults) {
        deleteResult(result.id);
    }

    rmSync(fixtureRoot, { force: true, recursive: true });
});

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

test("reproduces missing generic all-field search and substring matching on the landing page", async ({
    page,
}) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");

    const searchBuilder = page.locator('[data-search-builder="true"]');
    await expect(searchBuilder).toBeVisible();
    await expect(page.getByText(pipelineName).first()).toBeVisible();

    await searchBuilder
        .getByRole("button", {
            name: /add filter|add specific field to filter/i,
        })
        .click();
    await expect(
        page.locator('[data-search-builder-popover="true"]'),
    ).toBeVisible();

    const uiEvidence = await collectSearchUiEvidence(page);

    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: screenshotPath,
    });
    writeFileSync(
        evidencePath,
        `${JSON.stringify({ ...uiEvidence, screenshotPath }, null, 2)}\n`,
    );

    expect
        .soft(uiEvidence.addFilterButtonText)
        .toBe("Add specific field to filter");
    expect
        .soft(uiEvidence.permanentFieldKeys)
        .not.toEqual(expect.arrayContaining(pinnedFieldKeys));
    expect.soft(uiEvidence.genericInputCandidates.length).toBeGreaterThan(0);
    expect
        .soft(uiEvidence.fieldOptionLabels.slice(0, 5))
        .toEqual(pinnedFieldLabels);

    await page.goto(`/?meta_assay_tag=${encodeURIComponent(sharedNeedle)}`);
    await expect(searchBuilder).toBeVisible();

    const substringEvidence = await collectSubstringEvidence(page);

    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: substringScreenshotPath,
    });
    writeFileSync(
        substringEvidencePath,
        `${JSON.stringify(
            {
                ...substringEvidence,
                expectedRegisteredValue: assayValue,
                searchedSubstring: sharedNeedle,
                screenshotPath: substringScreenshotPath,
            },
            null,
            2,
        )}\n`,
    );

    expect.soft(substringEvidence.matchedReproRows).toBeGreaterThan(0);
});

test("adds a generic output-directory typed substring filter and returns every matching result set", async ({
    page,
}) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");

    const searchBuilder = page.locator('[data-search-builder="true"]');
    await expect(searchBuilder).toBeVisible();

    await searchBuilder
        .getByLabel(/generic all-field search/i)
        .fill(outputDirectorySubstring);

    const suggestions = page.locator(
        '[data-generic-search-suggestions="true"]',
    );
    await expect(suggestions).toBeVisible();
    await expect(suggestions.getByRole("option").first()).toBeVisible();

    const outputEvidence = await collectOutputSubstringEvidence(page);

    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: outputSubstringScreenshotPath,
    });
    writeFileSync(
        outputSubstringEvidencePath,
        `${JSON.stringify(
            {
                ...outputEvidence,
                expectedTypedSubstring: outputDirectorySubstring,
                screenshotPath: outputSubstringScreenshotPath,
            },
            null,
            2,
        )}\n`,
    );

    expect
        .soft(outputEvidence.outputDirectoryOptions.length)
        .toBeGreaterThanOrEqual(2);
    expect(outputEvidence.typedSubstringOptionCount).toBe(1);

    const typedSubstringOption = suggestions.getByRole("option", {
        name: new RegExp(
            `add output directory prefix filter ${outputDirectorySubstring}`,
            "i",
        ),
    });
    await expect(typedSubstringOption).toBeVisible();
    await typedSubstringOption.click();

    await expect(page).toHaveURL(
        new RegExp(
            `output_dir_prefix=${encodeURIComponent(outputDirectorySubstring)}`,
        ),
    );
    await expect(
        page.getByRole("button", {
            name: /registration details for alpha/i,
        }),
    ).toBeVisible();
    await expect(
        page.getByRole("button", {
            name: /registration details for beta/i,
        }),
    ).toBeVisible();

    const postfixEvidence = await collectOutputSubstringPostfixEvidence(page);

    await page.screenshot({
        animations: "disabled",
        fullPage: true,
        path: outputSubstringPostfixScreenshotPath,
    });
    writeFileSync(
        outputSubstringPostfixEvidencePath,
        `${JSON.stringify(
            {
                ...postfixEvidence,
                expectedTypedSubstring: outputDirectorySubstring,
                screenshotPath: outputSubstringPostfixScreenshotPath,
            },
            null,
            2,
        )}\n`,
    );

    expect(postfixEvidence.matchedOutputDirectories).toEqual([
        outputSubstringDirectory("alpha"),
        outputSubstringDirectory("beta"),
    ]);
});

function registerGenericSearchResult(): ResultSet {
    const outputDirectory = path.join(fixtureRoot, "results", "alpha");
    const outputPath = path.join(outputDirectory, "summary.txt");

    mkdirSync(outputDirectory, { recursive: true });
    writeFileSync(outputPath, `assay\tvalue\nrepro\t${assayValue}\n`, "utf8");

    const stats = statSync(outputPath);
    const registration: ResultRegistration = {
        pipeline_identifier:
            "https://github.com/wtsi-hgi/wa/generic-search-repro",
        run_key: "runid=260618&unique=generic-search-repro",
        requester: `requester-${sharedNeedle}`,
        operator: "generic-search-operator",
        command: `nextflow run ${pipelineName} --assay ${assayValue}`,
        pipeline_name: pipelineName,
        pipeline_version: "2026.06.18",
        output_directory: outputDirectory,
        metadata: {
            assay_tag: assayValue,
            cohort: "generic-search-repro",
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

function registerOutputSubstringResult(label: "alpha" | "beta"): ResultSet {
    const outputDirectory = outputSubstringDirectory(label);
    const outputFile = `summary-${label}.txt`;
    const outputPath = path.join(outputDirectory, outputFile);

    mkdirSync(outputDirectory, { recursive: true });
    writeFileSync(outputPath, `sample\t${label}\n`, "utf8");

    const stats = statSync(outputPath);
    const registration: ResultRegistration = {
        pipeline_identifier: `https://github.com/wtsi-hgi/wa/generic-output-substring-${label}`,
        run_key: `runid=260618&unique=generic-output-substring-${label}`,
        requester: `output-substring-requester-${label}`,
        operator: "generic-search-operator",
        command: `nextflow run ${outputSubstringPipelineName} --sample ${label}`,
        pipeline_name: outputSubstringPipelineName,
        pipeline_version: "2026.06.18",
        output_directory: outputDirectory,
        metadata: {
            cohort: "generic-output-substring-repro",
            sample: label,
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

function outputSubstringDirectory(label: "alpha" | "beta"): string {
    return path.join(fixtureRoot, outputDirectorySubstring, label);
}

async function collectSearchUiEvidence(page: Page): Promise<SearchUiEvidence> {
    return page.evaluate(() => {
        const searchBuilder = document.querySelector<HTMLElement>(
            '[data-search-builder="true"]',
        );
        const addFilterButton = Array.from(
            searchBuilder?.querySelectorAll("button") ?? [],
        ).find((button) =>
            /add filter|add specific field to filter/i.test(
                button.textContent?.trim() ?? "",
            ),
        );
        const permanentInputs = Array.from(
            searchBuilder?.querySelectorAll<HTMLInputElement>(
                "[data-permanent-filter-input]",
            ) ?? [],
        );
        const permanentLabels = permanentInputs.map(
            (input) =>
                searchBuilder
                    ?.querySelector<HTMLLabelElement>(
                        `label[for="${CSS.escape(input.id)}"]`,
                    )
                    ?.textContent?.trim() ?? "",
        );
        const fieldOptions = Array.from(
            document.querySelectorAll<HTMLElement>(
                "[data-filter-field-option]",
            ),
        );
        const genericInputCandidates = Array.from(
            searchBuilder?.querySelectorAll<HTMLInputElement>("input") ?? [],
        )
            .filter((input) => {
                const haystack = [
                    input.getAttribute("aria-label") ?? "",
                    input.getAttribute("placeholder") ?? "",
                    input.dataset.genericSearchInput ?? "",
                    input.id,
                ]
                    .join(" ")
                    .toLowerCase();

                return (
                    haystack.includes("generic") ||
                    (haystack.includes("all") && haystack.includes("field"))
                );
            })
            .map((input) => ({
                ariaLabel: input.getAttribute("aria-label"),
                dataGenericSearchInput:
                    input.dataset.genericSearchInput ?? null,
                id: input.id,
                placeholder: input.getAttribute("placeholder"),
            }));

        return {
            addFilterButtonText: addFilterButton?.textContent?.trim() ?? null,
            fieldOptionKeys: fieldOptions.map(
                (option) => option.dataset.filterFieldOption ?? "",
            ),
            fieldOptionLabels: fieldOptions.map(
                (option) => option.textContent?.trim() ?? "",
            ),
            genericInputCandidates,
            permanentFieldKeys: permanentInputs.map(
                (input) => input.dataset.permanentFilterInput ?? "",
            ),
            permanentFieldLabels: permanentLabels,
            visibleText: document.body.innerText.slice(0, 4000),
        };
    });
}

async function collectSubstringEvidence(
    page: Page,
): Promise<SubstringEvidence> {
    return page.evaluate(
        ({ fixtureMarker, pipeline }) => {
            const resultRows = Array.from(
                document.querySelectorAll<HTMLElement>(
                    'tbody tr[data-result-row="true"]',
                ),
            );
            const bodyText = document.body.innerText.slice(0, 4000);
            const matchedCombinedBrowser = bodyText.includes(fixtureMarker)
                ? 1
                : 0;

            return {
                bodyText,
                matchedReproRows:
                    resultRows.filter((row) =>
                        row.textContent?.includes(pipeline),
                    ).length + matchedCombinedBrowser,
                resultRows: resultRows.length,
                url: window.location.href,
            };
        },
        {
            fixtureMarker: "generic-search-repro-fixture",
            pipeline: pipelineName,
        },
    );
}

async function collectOutputSubstringEvidence(
    page: Page,
): Promise<OutputSubstringEvidence> {
    return page.evaluate((expectedSubstring) => {
        const input = document.querySelector<HTMLInputElement>(
            '[data-generic-search-input="true"]',
        );
        const options = Array.from(
            document.querySelectorAll<HTMLButtonElement>(
                '[data-generic-search-suggestions="true"] [role="option"]',
            ),
        );
        const optionLabels = options.map(
            (option) =>
                option.getAttribute("aria-label") ??
                option.textContent?.trim() ??
                "",
        );
        const outputDirectoryOptions = optionLabels.filter((label) =>
            /output directory prefix/i.test(label),
        );

        return {
            genericInputValue: input?.value ?? null,
            optionLabels,
            outputDirectoryOptions,
            typedSubstringOptionCount: outputDirectoryOptions.filter(
                (label) =>
                    label.toLowerCase() ===
                    `add output directory prefix filter ${expectedSubstring}`.toLowerCase(),
            ).length,
        };
    }, outputDirectorySubstring);
}

async function collectOutputSubstringPostfixEvidence(
    page: Page,
): Promise<OutputSubstringPostfixEvidence> {
    return page.evaluate(
        (expectedOutputDirectories) => {
            const bodyText = document.body.innerText.slice(0, 4000);
            const directoryRows = Array.from(
                document.querySelectorAll<HTMLElement>("[data-directory-row]"),
            ).map((row) => row.dataset.directoryRow ?? "");
            const registrationInfoLabels = Array.from(
                document.querySelectorAll<HTMLButtonElement>(
                    "[data-combined-result-info]",
                ),
            ).map((button) => button.getAttribute("aria-label") ?? "");

            return {
                bodyText,
                matchedOutputDirectories: expectedOutputDirectories.filter(
                    (directory) => directoryRows.includes(directory),
                ),
                registrationInfoLabels,
                resultRows: document.querySelectorAll(
                    'tbody tr[data-result-row="true"]',
                ).length,
                url: window.location.href,
            };
        },
        [outputSubstringDirectory("alpha"), outputSubstringDirectory("beta")],
    );
}
