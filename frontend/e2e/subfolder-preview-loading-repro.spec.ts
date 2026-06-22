import { mkdirSync, statSync, writeFileSync } from "node:fs";
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
const outputDirectory = path.join(
    evidenceDir,
    "subfolder-preview-loading-fixture",
);
const pipelineName = "wa/subfolder-preview-loading-repro";
const projectName = "Subfolder preview loading repro";
const subdirCount = 20;
const filesPerSubdir = 25;
const expectedPreviewFileCount = subdirCount * filesPerSubdir;

let registeredResult: ResultSet | null = null;

type FixtureFile = ResultRegistration["files"][number];

type LoadingSnapshot = {
    at: number;
    cardCount: number;
    checked: boolean;
    loadedPreviewCount: number;
    loadingPreviewCount: number;
    stripCount: number;
};

test.afterAll(() => {
    if (registeredResult) {
        deleteResult(registeredResult.id);
    }
});

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

function ensureLargeSubfolderPreviewFixture(): FixtureFile[] {
    mkdirSync(outputDirectory, { recursive: true });

    const files: FixtureFile[] = [];

    for (let subdirIndex = 1; subdirIndex <= subdirCount; subdirIndex += 1) {
        const subdirName = `batch-${String(subdirIndex).padStart(2, "0")}`;
        const subdirPath = path.join(outputDirectory, subdirName);

        mkdirSync(subdirPath, { recursive: true });

        for (let fileIndex = 1; fileIndex <= filesPerSubdir; fileIndex += 1) {
            const filePath = path.join(
                subdirPath,
                `preview-${String(fileIndex).padStart(2, "0")}.txt`,
            );

            writeFileSync(
                filePath,
                [
                    "Subfolder preview loading repro",
                    `Batch: ${subdirName}`,
                    `Preview file: ${fileIndex}`,
                    "This intentionally creates hundreds of previewable text files.",
                    "The current UI should reveal whether loading feedback appears immediately.",
                ].join("\n"),
                "utf8",
            );

            const stats = statSync(filePath);

            files.push({
                kind: "output",
                mtime: stats.mtime.toISOString(),
                path: filePath,
                size: stats.size,
            });
        }
    }

    return files;
}

function registerLargeSubfolderPreviewResult(): ResultSet {
    const files = ensureLargeSubfolderPreviewFixture();
    const registration: ResultRegistration = {
        command: "nextflow run wa/subfolder-preview-loading-repro",
        files,
        metadata: {
            panel: "subfolder-preview-loading-repro",
            project: projectName,
        },
        operator: "loading-repro-operator",
        output_directory: outputDirectory,
        pipeline_identifier:
            "https://github.com/wtsi-hgi/wa/subfolder-preview-loading-repro",
        pipeline_name: pipelineName,
        pipeline_version: "2026.06.22",
        requester: "loading-repro-requester",
        run_key: "runid=260622&unique=subfolder_preview_loading_gap",
    };

    return registerResult(registration);
}

async function openPreviewModes(controlsPath: string, page: Page) {
    const controls = page.locator(
        `[data-file-browser-folder-controls="${controlsPath}"]`,
    );
    const summary = controls
        .locator('summary[aria-label="Preview modes"]')
        .first();

    await expect(controls).toBeVisible();
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

async function collectLoadingSnapshot(
    page: Page,
    controlsPath: string,
): Promise<LoadingSnapshot> {
    return page.evaluate((directoryPath) => {
        const controls = [
            ...document.querySelectorAll<HTMLElement>(
                "[data-subdir-preview-controls]",
            ),
        ].find(
            (element) =>
                element.dataset.subdirPreviewControls === directoryPath,
        );
        const checkbox = controls?.querySelector<HTMLInputElement>(
            'input[aria-label="Subfolder previews"]',
        );
        const cards = [
            ...document.querySelectorAll<HTMLElement>(
                "[data-subdir-preview-card]",
            ),
        ];

        return {
            at: performance.now(),
            cardCount: cards.length,
            checked: checkbox?.checked ?? false,
            loadedPreviewCount: cards.filter((card) =>
                card.innerText.includes("Subfolder preview loading repro"),
            ).length,
            loadingPreviewCount: cards.filter((card) =>
                card.innerText.includes("Loading preview"),
            ).length,
            stripCount: document.querySelectorAll("[data-subdir-preview-strip]")
                .length,
        };
    }, controlsPath);
}

async function installTimingProbe(page: Page, controlsPath: string) {
    await page.evaluate((directoryPath) => {
        type ProbeSnapshot = LoadingSnapshot & { label: string };
        type ProbeState = {
            events: ProbeSnapshot[];
            firstCardAt: number | null;
            firstLoadingAt: number | null;
            frames: LoadingSnapshot[];
            keepSampling: boolean;
            longTasks: Array<{
                duration: number;
                name: string;
                startTime: number;
            }>;
            mutationObserver?: MutationObserver;
            performanceObserver?: PerformanceObserver;
        };

        const windowWithProbe = window as typeof window & {
            __subfolderPreviewLoadingRepro?: ProbeState;
        };

        const readSnapshot = (): LoadingSnapshot => {
            const controls = [
                ...document.querySelectorAll<HTMLElement>(
                    "[data-subdir-preview-controls]",
                ),
            ].find(
                (element) =>
                    element.dataset.subdirPreviewControls === directoryPath,
            );
            const checkbox = controls?.querySelector<HTMLInputElement>(
                'input[aria-label="Subfolder previews"]',
            );
            const cards = [
                ...document.querySelectorAll<HTMLElement>(
                    "[data-subdir-preview-card]",
                ),
            ];

            return {
                at: performance.now(),
                cardCount: cards.length,
                checked: checkbox?.checked ?? false,
                loadedPreviewCount: cards.filter((card) =>
                    card.innerText.includes("Subfolder preview loading repro"),
                ).length,
                loadingPreviewCount: cards.filter((card) =>
                    card.innerText.includes("Loading preview"),
                ).length,
                stripCount: document.querySelectorAll(
                    "[data-subdir-preview-strip]",
                ).length,
            };
        };
        const state: ProbeState = {
            events: [],
            firstCardAt: null,
            firstLoadingAt: null,
            frames: [],
            keepSampling: true,
            longTasks: [],
        };
        const recordEvent = (label: string) => {
            const snapshot = readSnapshot();

            if (label !== "mutation" || state.events.length < 240) {
                state.events.push({ ...snapshot, label });
            }

            if (state.firstCardAt === null && snapshot.cardCount > 0) {
                state.firstCardAt = snapshot.at;
            }

            if (
                state.firstLoadingAt === null &&
                snapshot.loadingPreviewCount > 0
            ) {
                state.firstLoadingAt = snapshot.at;
            }
        };
        let mutationRecordScheduled = false;
        const controls = [
            ...document.querySelectorAll<HTMLElement>(
                "[data-subdir-preview-controls]",
            ),
        ].find(
            (element) =>
                element.dataset.subdirPreviewControls === directoryPath,
        );
        const checkbox = controls?.querySelector<HTMLInputElement>(
            'input[aria-label="Subfolder previews"]',
        );

        checkbox?.addEventListener(
            "change",
            () => recordEvent("checkbox-change"),
            { capture: true, once: true },
        );

        state.mutationObserver = new MutationObserver(() => {
            if (mutationRecordScheduled) {
                return;
            }

            mutationRecordScheduled = true;
            window.setTimeout(() => {
                mutationRecordScheduled = false;
                recordEvent("mutation");
            }, 0);
        });
        state.mutationObserver.observe(document.documentElement, {
            childList: true,
            subtree: true,
        });

        let lastFrameSampleAt = 0;
        const sampleFrame = () => {
            if (!state.keepSampling) {
                return;
            }

            const now = performance.now();

            if (state.frames.length < 240 && now - lastFrameSampleAt >= 50) {
                state.frames.push(readSnapshot());
                lastFrameSampleAt = now;
            }

            window.requestAnimationFrame(sampleFrame);
        };

        window.requestAnimationFrame(sampleFrame);

        if ("PerformanceObserver" in window) {
            try {
                state.performanceObserver = new PerformanceObserver((list) => {
                    for (const entry of list.getEntries()) {
                        state.longTasks.push({
                            duration: entry.duration,
                            name: entry.name,
                            startTime: entry.startTime,
                        });
                    }
                });
                state.performanceObserver.observe({
                    entryTypes: ["longtask"],
                });
            } catch {
                // Long-task timing is helpful evidence, but not required.
            }
        }

        windowWithProbe.__subfolderPreviewLoadingRepro = state;
        recordEvent("probe-start");
    }, controlsPath);
}

async function collectTimingProbe(page: Page) {
    return page.evaluate(() => {
        const windowWithProbe = window as typeof window & {
            __subfolderPreviewLoadingRepro?: {
                events: Array<LoadingSnapshot & { label: string }>;
                firstCardAt: number | null;
                firstLoadingAt: number | null;
                frames: LoadingSnapshot[];
                keepSampling: boolean;
                longTasks: Array<{
                    duration: number;
                    name: string;
                    startTime: number;
                }>;
                mutationObserver?: MutationObserver;
                performanceObserver?: PerformanceObserver;
            };
        };
        const state = windowWithProbe.__subfolderPreviewLoadingRepro;

        if (!state) {
            return null;
        }

        state.keepSampling = false;
        state.mutationObserver?.disconnect();
        state.performanceObserver?.disconnect();

        return {
            events: state.events,
            firstCardAt: state.firstCardAt,
            firstLoadingAt: state.firstLoadingAt,
            frames: state.frames,
            longTasks: state.longTasks,
        };
    });
}

test("shows immediate loading indication when Subfolder previews queues hundreds of previews", async ({
    context,
    page,
}) => {
    test.setTimeout(180_000);

    registeredResult = registerLargeSubfolderPreviewResult();

    const cdpSession = await context.newCDPSession(page);
    const beforeScreenshotPath = path.join(
        evidenceDir,
        "subfolder-preview-loading-gap-before.png",
    );
    const immediateScreenshotPath = path.join(
        evidenceDir,
        "subfolder-preview-loading-gap-immediate.png",
    );
    const loadingScreenshotPath = path.join(
        evidenceDir,
        "subfolder-preview-loading-gap-loading.png",
    );
    const loadedScreenshotPath = path.join(
        evidenceDir,
        "subfolder-preview-loading-gap-loaded.png",
    );
    const evidencePath = path.join(
        evidenceDir,
        "subfolder-preview-loading-gap.json",
    );

    mkdirSync(evidenceDir, { recursive: true });

    await cdpSession.send("Emulation.setCPUThrottlingRate", { rate: 6 });
    await page.route("**/api/file**", async (route) => {
        if (route.request().method() === "GET") {
            await new Promise((resolve) => setTimeout(resolve, 1500));
        }

        await route.continue();
    });

    try {
        await page.goto(`/results/${registeredResult.id}`);
        await expect(
            page.getByRole("heading", { level: 1, name: projectName }),
        ).toBeVisible({ timeout: 30_000 });
        await expect(page.locator('[data-file-browser="true"]')).toBeVisible();

        const controls = page.locator("[data-subdir-preview-controls]").first();

        await expect(controls).toBeVisible();

        const controlsPath = await controls.getAttribute(
            "data-subdir-preview-controls",
        );

        expect(controlsPath).toBeTruthy();

        const beforeSnapshot = await collectLoadingSnapshot(
            page,
            controlsPath ?? "",
        );

        expect(beforeSnapshot.cardCount).toBe(0);

        await page.screenshot({
            animations: "disabled",
            path: beforeScreenshotPath,
        });
        await openPreviewModes(controlsPath ?? "", page);
        await installTimingProbe(page, controlsPath ?? "");

        const checkbox = page
            .locator(`[data-file-browser-folder-controls="${controlsPath}"]`)
            .locator('input[aria-label="Subfolder previews"]')
            .first();

        await expect(checkbox).toBeVisible();

        const clickStartedAt = await page.evaluate(() => performance.now());

        await checkbox.evaluate((element) => {
            (element as HTMLInputElement).click();
        });

        const clickResolvedAt = await page.evaluate(() => performance.now());
        const immediateSnapshot = await collectLoadingSnapshot(
            page,
            controlsPath ?? "",
        );

        await page.screenshot({
            animations: "disabled",
            path: immediateScreenshotPath,
        });

        await expect
            .poll(
                async () =>
                    (await collectLoadingSnapshot(page, controlsPath ?? ""))
                        .loadingPreviewCount,
                { timeout: 60_000 },
            )
            .toBeGreaterThan(0);

        const loadingSnapshot = await collectLoadingSnapshot(
            page,
            controlsPath ?? "",
        );

        await page.screenshot({
            animations: "disabled",
            path: loadingScreenshotPath,
        });

        await expect
            .poll(
                async () =>
                    (await collectLoadingSnapshot(page, controlsPath ?? ""))
                        .loadedPreviewCount,
                { timeout: 120_000 },
            )
            .toBe(expectedPreviewFileCount);

        const loadedSnapshot = await collectLoadingSnapshot(
            page,
            controlsPath ?? "",
        );

        await page.screenshot({
            animations: "disabled",
            path: loadedScreenshotPath,
        });

        const probe = await collectTimingProbe(page);
        const checkboxChangeEvent = probe?.events.find(
            (event) => event.label === "checkbox-change",
        );
        const firstIndicatorAt = probe?.firstLoadingAt ?? null;
        const gapFromChangeToLoadingMs =
            checkboxChangeEvent && firstIndicatorAt !== null
                ? firstIndicatorAt - checkboxChangeEvent.at
                : null;
        const firstIndicatorFrame =
            probe?.frames.find(
                (frame) => frame.cardCount > 0 || frame.loadingPreviewCount > 0,
            ) ?? null;
        const lastNoIndicatorFrameBeforeIndicator =
            probe?.frames.findLast(
                (frame) =>
                    (firstIndicatorFrame === null ||
                        frame.at < firstIndicatorFrame.at) &&
                    frame.cardCount === 0 &&
                    frame.loadingPreviewCount === 0,
            ) ?? null;
        const visibleFrameGapMs =
            firstIndicatorFrame && lastNoIndicatorFrameBeforeIndicator
                ? firstIndicatorFrame.at -
                  lastNoIndicatorFrameBeforeIndicator.at
                : null;
        const lastNoIndicatorFrameOffsetFromClickMs =
            lastNoIndicatorFrameBeforeIndicator
                ? clickStartedAt - lastNoIndicatorFrameBeforeIndicator.at
                : null;
        const maxLongTaskMs =
            probe?.longTasks.reduce(
                (max, task) => Math.max(max, task.duration),
                0,
            ) ?? 0;
        const evidence = {
            controlsPath,
            currentBug: {
                expected:
                    "Subfolder preview mode should show loading feedback within the next visible frame when hundreds of previews are queued.",
                observed:
                    "The loading preview boxes appeared without a multi-second blank/no-indicator gap.",
            },
            fixture: {
                expectedPreviewFileCount,
                filesPerSubdir,
                outputDirectory,
                subdirCount,
            },
            resultId: registeredResult.id,
            screenshots: {
                before: beforeScreenshotPath,
                immediate: immediateScreenshotPath,
                loaded: loadedScreenshotPath,
                loading: loadingScreenshotPath,
            },
            snapshots: {
                before: beforeSnapshot,
                immediate: immediateSnapshot,
                loaded: loadedSnapshot,
                loading: loadingSnapshot,
            },
            timing: {
                checkboxChangeEvent,
                clickDurationMs: clickResolvedAt - clickStartedAt,
                clickResolvedAt,
                clickStartedAt,
                firstIndicatorAt,
                firstIndicatorFrame,
                firstIndicatorFrameOffsetFromClickMs: firstIndicatorFrame
                    ? firstIndicatorFrame.at - clickStartedAt
                    : null,
                gapFromChangeToLoadingMs,
                lastNoIndicatorFrameBeforeIndicator,
                lastNoIndicatorFrameOffsetFromClickMs,
                maxLongTaskMs,
                probe,
                visibleFrameGapMs,
            },
            url: page.url(),
        };

        writeFileSync(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`);

        expect(firstIndicatorFrame).toMatchObject({
            loadingPreviewCount: subdirCount,
        });
        expect(firstIndicatorFrame?.cardCount ?? 0).toBeGreaterThan(0);
        expect(firstIndicatorFrame?.cardCount ?? 0).toBeLessThan(
            expectedPreviewFileCount,
        );
        expect(clickResolvedAt - clickStartedAt).toBeLessThan(1000);
        expect(firstIndicatorFrame).not.toBeNull();
        expect(
            firstIndicatorFrame ? firstIndicatorFrame.at - clickStartedAt : 0,
        ).toBeLessThan(1000);
        if (visibleFrameGapMs !== null) {
            expect(visibleFrameGapMs).toBeLessThan(1000);
        }
        expect(loadingSnapshot.cardCount).toBeGreaterThan(0);
        expect(loadingSnapshot.loadingPreviewCount).toBeGreaterThan(0);
        expect(loadedSnapshot.loadedPreviewCount).toBe(
            expectedPreviewFileCount,
        );
    } finally {
        await cdpSession
            .send("Emulation.setCPUThrottlingRate", { rate: 1 })
            .catch(() => undefined);
    }
});
