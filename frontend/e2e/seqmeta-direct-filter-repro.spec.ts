import fs from "node:fs/promises";
import path from "node:path";

import { expect, test, type Page } from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultSet,
} from "./results-auth-helpers";

type FilterEvidence = {
    finalUrl: string;
    label: string;
    millisUntilSearchResults: number | null;
    resultRows: number;
    renderingSampleCount: number;
    screenshotPath: string;
    textSamples: Array<{
        elapsedMs: number;
        matchingHeadingVisible: boolean;
        renderingVisible: boolean;
        resultRows: number;
    }>;
};

async function registerDirectMetadataReproResult(): Promise<ResultSet> {
    const outputDirectory = path.resolve(
        process.cwd(),
        "..",
        ".tmp",
        "agent",
        "e2e-seqmeta-direct-filter-output",
    );

    await fs.mkdir(outputDirectory, { recursive: true });

    return registerResult({
        pipeline_identifier:
            "https://github.com/wtsi-hgi/wa/e2e-direct-filter-repro",
        run_key:
            "runid=48522&study=7607&sample=7607STDY14643771&library=71046409",
        requester: "agent",
        operator: "agent",
        command: "nextflow run seqmeta-direct-filter-repro",
        pipeline_name: "seqmeta/direct-filter-repro",
        pipeline_version: "2026.05",
        output_directory: outputDirectory,
        files: [],
        metadata: {
            seqmeta_librarytype: "Custom",
            seqmeta_libraryid: "71046409",
            seqmeta_runid: "48522",
            seqmeta_id_sample_lims: "SMP7607-0000",
            seqmeta_sampleid: "7607STDY14643771",
            seqmeta_supplier_name: "Supplier Sample 7607",
            seqmeta_studyid: "7607",
        },
    });
}

async function openSampleDetails(page: Page, resultId: string): Promise<void> {
    await page.goto(`/results/${encodeURIComponent(resultId)}`, {
        waitUntil: "domcontentloaded",
    });
    await page.locator('[data-metadata-row="seqmeta_sampleid"]').waitFor({
        state: "visible",
    });
    await page.waitForFunction(
        () =>
            document.querySelectorAll('[aria-label="loading enrichment"]')
                .length === 0,
    );
    await page
        .locator('[data-metadata-row="seqmeta_sampleid"]')
        .getByRole("button", {
            name: /Open seqmeta_sample_name details/i,
        })
        .click();
    await expect(page.getByRole("dialog")).toBeVisible();
}

async function captureFilterEvidence(
    page: Page,
    label: string,
    ariaLabel: RegExp,
    screenshotName: string,
    maxCaptureMs = 12_000,
): Promise<FilterEvidence> {
    const evidenceRoot = path.resolve(process.cwd(), "..", ".tmp", "agent");
    await fs.mkdir(evidenceRoot, { recursive: true });

    const startedAt = Date.now();
    const textSamples: FilterEvidence["textSamples"] = [];
    let millisUntilSearchResults: number | null = null;

    await page.getByRole("link", { name: ariaLabel }).click();

    while (Date.now() - startedAt < maxCaptureMs) {
        const elapsedMs = Date.now() - startedAt;
        const bodyText = await page.locator("body").innerText();
        const matchingHeadingVisible =
            (await page
                .getByRole("heading", { name: "Matching result sets" })
                .count()) > 0;
        const resultRows = await page
            .locator('tbody tr[data-result-row="true"]')
            .count();
        const renderingVisible = bodyText.includes("Rendering...");

        textSamples.push({
            elapsedMs,
            matchingHeadingVisible,
            renderingVisible,
            resultRows,
        });

        if (matchingHeadingVisible) {
            millisUntilSearchResults = elapsedMs;
            break;
        }

        await page.waitForTimeout(250);
    }

    const screenshotPath = path.join(evidenceRoot, screenshotName);
    await page.screenshot({ fullPage: true, path: screenshotPath });

    return {
        finalUrl: page.url(),
        label,
        millisUntilSearchResults,
        resultRows: await page
            .locator('tbody tr[data-result-row="true"]')
            .count(),
        renderingSampleCount: textSamples.filter(
            (sample) => sample.renderingVisible,
        ).length,
        screenshotPath,
        textSamples,
    };
}

test("direct seqmeta sample metadata filter links return the originating result as quickly as the title filter", async ({
    browser,
}) => {
    test.setTimeout(180_000);

    const result = await registerDirectMetadataReproResult();
    const evidenceRoot = path.resolve(process.cwd(), "..", ".tmp", "agent");
    const evidencePath = path.join(
        evidenceRoot,
        "seqmeta-direct-filter-repro.json",
    );
    const tracePath = path.join(
        evidenceRoot,
        "seqmeta-direct-filter-repro-trace.zip",
    );
    let cleanupNeeded = true;

    const context = await browser.newContext();
    await installResultsAuthCookie(context);
    await context.tracing.start({ screenshots: true, snapshots: true });
    const page = await context.newPage();

    try {
        await openSampleDetails(page, result.id);
        const titleEvidence = await captureFilterEvidence(
            page,
            "title sample-name filter",
            /Send seqmeta_sample_name to search filter/i,
            "seqmeta-title-filter-repro.png",
        );

        await openSampleDetails(page, result.id);
        const directSupplierEvidence = await captureFilterEvidence(
            page,
            "direct supplier-name filter",
            /Send seqmeta_supplier_name to search filter/i,
            "seqmeta-direct-supplier-filter-repro.png",
        );

        await openSampleDetails(page, result.id);
        const directLimsEvidence = await captureFilterEvidence(
            page,
            "direct sample-LIMS filter",
            /Send seqmeta_id_sample_lims to search filter/i,
            "seqmeta-direct-lims-filter-repro.png",
        );

        await context.tracing.stop({ path: tracePath });

        const evidence = {
            resultId: result.id,
            tracePath,
            filters: [
                titleEvidence,
                directSupplierEvidence,
                directLimsEvidence,
            ],
        };
        await fs.writeFile(evidencePath, JSON.stringify(evidence, null, 2));

        deleteResult(result.id);
        cleanupNeeded = false;

        expect(titleEvidence.millisUntilSearchResults).not.toBeNull();
        expect(directSupplierEvidence.millisUntilSearchResults).not.toBeNull();
        expect(directLimsEvidence.millisUntilSearchResults).not.toBeNull();
        expect(titleEvidence.resultRows).toBeGreaterThanOrEqual(1);
        expect(directSupplierEvidence.resultRows).toBeGreaterThanOrEqual(1);
        expect(directLimsEvidence.resultRows).toBeGreaterThanOrEqual(1);
        expect(new URL(titleEvidence.finalUrl).searchParams.get("sample")).toBe(
            "7607STDY14643771",
        );
        expect(
            new URL(directSupplierEvidence.finalUrl).searchParams.get(
                "seqmeta_supplier_name",
            ),
        ).toBe("Supplier Sample 7607");
        expect(
            new URL(directLimsEvidence.finalUrl).searchParams.get(
                "seqmeta_id_sample_lims",
            ),
        ).toBe("SMP7607-0000");
        expect(directSupplierEvidence.renderingSampleCount).toBe(0);
        expect(directLimsEvidence.renderingSampleCount).toBe(0);
        expect(
            Math.max(
                directSupplierEvidence.millisUntilSearchResults ?? Infinity,
                directLimsEvidence.millisUntilSearchResults ?? Infinity,
            ),
        ).toBeLessThanOrEqual(
            (titleEvidence.millisUntilSearchResults ?? 0) + 1_000,
        );
    } finally {
        if (cleanupNeeded) {
            deleteResult(result.id);
        }

        await context.close();
    }
});
