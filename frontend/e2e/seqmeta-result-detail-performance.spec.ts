import { mkdirSync } from "node:fs";
import path from "node:path";

import { expect, test, type Page, type Route } from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultSet,
} from "./results-auth-helpers";
import { withSeqmetaHeavyE2ELock } from "./seqmeta-test-lock";

type DeferredSignal = {
    promise: Promise<void>;
    resolve: () => void;
};

type HeldServerActions = {
    release: () => void;
    started: Promise<void>;
    stop: () => Promise<void>;
};

function deferredSignal(): DeferredSignal {
    let resolve: () => void = () => {};
    const promise = new Promise<void>((next) => {
        resolve = next;
    });

    return { promise, resolve };
}

function hasNextActionHeader(headers: Record<string, string>): boolean {
    return Object.keys(headers).some(
        (header) => header.toLowerCase() === "next-action",
    );
}

async function holdClientEnrichmentActions(
    page: Page,
): Promise<HeldServerActions> {
    const started = deferredSignal();
    const release = deferredSignal();
    let startedResolved = false;

    const handler = async (route: Route) => {
        const request = route.request();

        if (
            request.method() !== "POST" ||
            !hasNextActionHeader(request.headers())
        ) {
            await route.continue();
            return;
        }

        if (!startedResolved) {
            startedResolved = true;
            started.resolve();
        }

        await release.promise;
        await route.continue();
    };

    await page.route("**/*", handler);

    return {
        release: release.resolve,
        started: started.promise,
        stop: () => page.unroute("**/*", handler),
    };
}

async function registerMultiSeqmetaResult(): Promise<ResultSet> {
    const outputDirectory = path.resolve(
        process.cwd(),
        "..",
        ".tmp",
        "agent",
        "e2e-seqmeta-performance-output",
    );

    mkdirSync(outputDirectory, { recursive: true });

    return registerResult({
        pipeline_identifier: "https://github.com/wtsi-hgi/wa/e2e-repro",
        run_key:
            "runid=48522&study=7607&sample=7607STDY14643771&library=71046409",
        requester: "agent",
        operator: "agent",
        command: "nextflow run seqmeta-rendering-repro",
        pipeline_name: "seqmeta/rendering-repro",
        pipeline_version: "2026.05",
        output_directory: outputDirectory,
        files: [],
        metadata: {
            seqmeta_librarytype: "Custom",
            seqmeta_libraryid: "71046409",
            seqmeta_runid: "48522",
            seqmeta_sampleid: "7607STDY14643771",
            seqmeta_studyid: "7607",
        },
    });
}

test("renders seqmeta details without blocking on client enrichment", async ({
    context,
    page,
}) => {
    await withSeqmetaHeavyE2ELock(async () => {
        const result = await registerMultiSeqmetaResult();
        const heldEnrichment = await holdClientEnrichmentActions(page);
        let cleanupNeeded = true;

        try {
            await installResultsAuthCookie(context);
            const initialRenderStartedAt = Date.now();

            await page.goto(`/results/${encodeURIComponent(result.id)}`, {
                waitUntil: "domcontentloaded",
            });
            await page
                .locator('[data-metadata-row="seqmeta_studyid"]')
                .waitFor({
                    state: "visible",
                });
            const initialRenderElapsedMs = Date.now() - initialRenderStartedAt;

            await heldEnrichment.started;

            expect(await page.getByText("Rendering...").count()).toBe(0);
            expect(
                await page.locator('[aria-label="loading enrichment"]').count(),
            ).toBeGreaterThan(0);
            expect(initialRenderElapsedMs).toBeLessThan(1000);

            heldEnrichment.release();

            await page.waitForFunction(
                () =>
                    document.querySelectorAll(
                        '[aria-label="loading enrichment"]',
                    ).length === 0,
            );

            const detailStartedAt = Date.now();
            await page
                .locator('[data-metadata-row="seqmeta_studyid"]')
                .getByRole("button", {
                    name: /Open seqmeta_id_study_lims details/i,
                })
                .click();

            const dialog = page.getByRole("dialog");

            await expect(dialog.getByText("LIB7607-71046409")).toBeVisible();
            const detailElapsedMs = Date.now() - detailStartedAt;

            expect(detailElapsedMs).toBeLessThan(1000);
            deleteResult(result.id);
            cleanupNeeded = false;
        } finally {
            heldEnrichment.release();
            await heldEnrichment.stop();

            if (cleanupNeeded) {
                deleteResult(result.id);
            }
        }
    });
});
