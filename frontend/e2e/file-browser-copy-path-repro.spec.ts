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
const fixtureRoot = path.join(evidenceDir, "file-browser-copy-path-fixture");
const reproToken = `copy-path-${Date.now()}-${process.pid}`;
const requester = `copy-path-requester-${reproToken}`;
const pipelineName = "wa/file-browser-copy-path";
const outputDirectory = path.join(fixtureRoot, reproToken, "copy-root");
const initiallySelectedFilePath = path.join(
    outputDirectory,
    "alpha-selected.log",
);
const fileCopyTargetPath = path.join(outputDirectory, "beta-copy-target.log");
const directoryCopyTargetPath = path.join(
    outputDirectory,
    "nested-copy-target",
);
const nestedFilePath = path.join(directoryCopyTargetPath, "nested-report.tsv");
const screenshotPath = path.join(
    evidenceDir,
    "file-browser-copy-full-path-missing-affordance.png",
);

let registeredResult: ResultSet | null = null;

type CopyPathEvidence = {
    candidateCopyPathButtons: Array<{
        ariaLabel: string | null;
        pathAttribute: string | null;
        tag: string;
        text: string;
        title: string | null;
    }>;
    directoryRows: Array<{
        copyPathButtonCount: number;
        expanded: string | null;
        path: string;
        text: string;
    }>;
    fileRows: Array<{
        copyPathButtonCount: number;
        path: string;
        text: string;
    }>;
    previewPath: string | null;
    screenshotPath: string;
    visibleText: string | null;
};

test.beforeAll(() => {
    registeredResult = registerCopyPathResult();
});

test.afterAll(() => {
    if (registeredResult) {
        deleteResult(registeredResult.id);
    }
});

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
});

function registerCopyPathResult(): ResultSet {
    const files = [
        {
            content: "alpha file should remain selected\n",
            path: initiallySelectedFilePath,
        },
        {
            content: "beta file is the copy-path click target\n",
            path: fileCopyTargetPath,
        },
        {
            content: "name\tvalue\nnested\t42\n",
            path: nestedFilePath,
        },
    ].map((file) => {
        mkdirSync(path.dirname(file.path), { recursive: true });
        writeFileSync(file.path, file.content);

        const stats = statSync(file.path);

        return {
            kind: "output" as const,
            mtime: stats.mtime.toISOString(),
            path: file.path,
            size: stats.size,
        };
    });

    const registration: ResultRegistration = {
        command: `nextflow run ${pipelineName} --copy-path-repro ${reproToken}`,
        files,
        metadata: {
            project: "copy-path-repro",
            sample: reproToken,
        },
        operator: "copy-path-operator",
        output_directory: outputDirectory,
        pipeline_identifier: `https://github.com/wtsi-hgi/${pipelineName}`,
        pipeline_name: pipelineName,
        pipeline_version: "2026.06.18",
        requester,
        run_key: `runid=260618&unique=${reproToken}`,
    };

    return registerResult(registration);
}

async function openResultDetailFileBrowser(page: Page): Promise<void> {
    if (!registeredResult) {
        throw new Error("Missing registered result for copy-path repro");
    }

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(`/results/${registeredResult.id}`);
    const heading = page.getByRole("heading", { level: 1 });

    await expect(heading).toContainText("copy-path-repro", {
        timeout: 30000,
    });
    await expect(heading).not.toContainText(pipelineName);

    const fileBrowser = page.locator('[data-file-browser="true"]');

    await expect(fileBrowser).toBeVisible({ timeout: 30000 });
    await expect(
        page.locator('[data-directory-path$="/nested-copy-target"]'),
    ).toBeVisible();
    await expect(
        page.locator('[data-file-path$="/alpha-selected.log"]'),
    ).toBeVisible();
    await expect(
        page.locator('[data-file-path$="/beta-copy-target.log"]'),
    ).toBeVisible();
    await expect(
        page.locator('[data-file-browser-preview="single"]'),
    ).toHaveAttribute("data-preview-resize-frame", initiallySelectedFilePath);
}

async function writeCopyPathEvidence(page: Page): Promise<CopyPathEvidence> {
    mkdirSync(evidenceDir, { recursive: true });

    const evidence = await page.evaluate(
        ({ screenshotPath }) => {
            const isCopyPathControl = (element: Element): boolean => {
                if (
                    element.matches("[data-directory-path], [data-file-path]")
                ) {
                    return false;
                }

                const label = [
                    element.getAttribute("aria-label"),
                    element.getAttribute("title"),
                    element.textContent,
                ]
                    .filter(Boolean)
                    .join(" ");

                return /\bcopy\b/i.test(label) && /\bpath\b/i.test(label);
            };
            const copyPathButtonCount = (element: Element): number =>
                [...element.querySelectorAll("button, [role='button']")].filter(
                    isCopyPathControl,
                ).length;
            const fileBrowser = document.querySelector<HTMLElement>(
                '[data-file-browser="true"]',
            );
            const candidateCopyPathButtons = [
                ...((fileBrowser?.querySelectorAll("button, [role='button']") ??
                    []) as NodeListOf<HTMLElement>),
            ]
                .filter(isCopyPathControl)
                .map((element) => ({
                    ariaLabel: element.getAttribute("aria-label"),
                    pathAttribute:
                        element.getAttribute("data-directory-path") ??
                        element.getAttribute("data-file-path"),
                    tag: element.tagName.toLowerCase(),
                    text: element.textContent?.trim() ?? "",
                    title: element.getAttribute("title"),
                }));

            return {
                candidateCopyPathButtons,
                directoryRows: [
                    ...document.querySelectorAll<HTMLElement>(
                        "[data-directory-row]",
                    ),
                ].map((element) => ({
                    copyPathButtonCount: copyPathButtonCount(element),
                    expanded:
                        element
                            .querySelector("[data-directory-path]")
                            ?.getAttribute("data-directory-expanded") ?? null,
                    path: element.getAttribute("data-directory-row") ?? "",
                    text: element.innerText.replace(/\s+/g, " ").trim(),
                })),
                fileRows: [
                    ...document.querySelectorAll<HTMLElement>(
                        "[data-file-path]",
                    ),
                ].map((element) => ({
                    copyPathButtonCount: isCopyPathControl(element)
                        ? 1
                        : copyPathButtonCount(element),
                    path: element.getAttribute("data-file-path") ?? "",
                    text: element.innerText.replace(/\s+/g, " ").trim(),
                })),
                previewPath:
                    document
                        .querySelector('[data-file-browser-preview="single"]')
                        ?.getAttribute("data-preview-resize-frame") ?? null,
                screenshotPath,
                visibleText: fileBrowser?.innerText.slice(0, 3000) ?? null,
            };
        },
        { screenshotPath },
    );

    await page.locator('[data-file-browser="true"]').screenshot({
        animations: "disabled",
        path: screenshotPath,
    });
    writeFileSync(
        screenshotPath.replace(/\.png$/, ".json"),
        `${JSON.stringify(
            {
                ...evidence,
                expected: {
                    directoryCopyTargetPath,
                    fileCopyTargetPath,
                    initiallySelectedFilePath,
                    note: "Each displayed file and directory should expose an independent copy full path button that does not fire the row click action.",
                },
                pageUrl: page.url(),
            },
            null,
            2,
        )}\n`,
    );

    return evidence;
}

function copyPathButtonWithin(row: Locator): Locator {
    return row.getByRole("button", { name: /copy.*path/i });
}

async function expectIndependentCopyButton(
    copyButton: Locator,
    normalRowButton: Locator,
): Promise<void> {
    await expect(copyButton).toHaveCount(1);
    await expect(normalRowButton).toHaveCount(1);

    const copyButtonIsNormalRowButton = await copyButton
        .first()
        .evaluate((element) =>
            element.matches("[data-directory-path], [data-file-path]"),
        );

    expect(
        copyButtonIsNormalRowButton,
        "The copy-path control must be independent of the file/directory row button.",
    ).toBe(false);
}

async function clipboardText(page: Page): Promise<string> {
    return page.evaluate(() => navigator.clipboard.readText());
}

test("file browser rows expose independent copy full path controls", async ({
    page,
}) => {
    await openResultDetailFileBrowser(page);

    const evidence = await writeCopyPathEvidence(page);

    expect(
        evidence.directoryRows.some((row) =>
            row.path.endsWith("/nested-copy-target"),
        ),
        "The repro fixture should display a nested directory row.",
    ).toBe(true);
    expect(
        evidence.fileRows.some((row) =>
            row.path.endsWith("/beta-copy-target.log"),
        ),
        "The repro fixture should display a file row.",
    ).toBe(true);

    const nestedDirectoryRow = page
        .locator('[data-directory-row$="/nested-copy-target"]')
        .first();
    const nestedDirectoryButton = page
        .locator('[data-directory-path$="/nested-copy-target"]')
        .first();
    const fileRow = page
        .locator('[data-file-path$="/beta-copy-target.log"]')
        .first();
    const directoryCopyButton = copyPathButtonWithin(nestedDirectoryRow);
    const fileCopyButton = copyPathButtonWithin(fileRow);

    await expect(
        directoryCopyButton,
        "Directory rows should include an independent button named like 'Copy full path'.",
    ).toHaveCount(1);
    await expect(
        fileCopyButton,
        "File rows should include an independent button named like 'Copy full path'.",
    ).toHaveCount(1);

    await expectIndependentCopyButton(
        directoryCopyButton,
        nestedDirectoryButton,
    );
    await expectIndependentCopyButton(fileCopyButton, fileRow);

    await expect(nestedDirectoryButton).toHaveAttribute(
        "data-directory-expanded",
        "false",
    );
    await directoryCopyButton.click();
    await expect.poll(() => clipboardText(page)).toBe(directoryCopyTargetPath);
    await expect(nestedDirectoryButton).toHaveAttribute(
        "data-directory-expanded",
        "false",
    );
    await expect(fileRow).toBeVisible();

    await expect(
        page.locator('[data-file-browser-preview="single"]'),
    ).toHaveAttribute("data-preview-resize-frame", initiallySelectedFilePath);
    await fileCopyButton.click();
    await expect.poll(() => clipboardText(page)).toBe(fileCopyTargetPath);
    await expect(
        page.locator('[data-file-browser-preview="single"]'),
    ).toHaveAttribute("data-preview-resize-frame", initiallySelectedFilePath);
});
