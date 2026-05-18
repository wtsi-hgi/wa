import path from "node:path";

import { expect, test } from "@playwright/test";

type ResultSet = {
    id: string;
};

function resultsBackendUrl(): string {
    const port = process.env.WA_TEST_RESULTS_PORT;

    if (!port) {
        throw new Error("WA_TEST_RESULTS_PORT is required for this test");
    }

    return `http://127.0.0.1:${port}`;
}

async function registerMultiSeqmetaResult(): Promise<ResultSet> {
    const outputDirectory = path.resolve(
        process.cwd(),
        "..",
        ".tmp",
        "agent",
        "e2e-seqmeta-performance-output",
    );
    const response = await fetch(`${resultsBackendUrl()}/results`, {
        method: "POST",
        headers: {
            "content-type": "application/json",
        },
        body: JSON.stringify({
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
        }),
    });

    if (!response.ok) {
        throw new Error(
            `registration failed ${response.status}: ${await response.text()}`,
        );
    }

    return (await response.json()) as ResultSet;
}

async function deleteResult(resultId: string): Promise<void> {
    const response = await fetch(
        `${resultsBackendUrl()}/results/${encodeURIComponent(resultId)}`,
        { method: "DELETE" },
    );

    if (!response.ok && response.status !== 404) {
        throw new Error(
            `delete failed ${response.status}: ${await response.text()}`,
        );
    }
}

test("renders five seqmeta result metadata details in under one second", async ({
    page,
}) => {
    const result = await registerMultiSeqmetaResult();
    let cleanupNeeded = true;

    try {
        const startedAt = Date.now();

        await page.goto(`/results/${encodeURIComponent(result.id)}`, {
            waitUntil: "domcontentloaded",
        });
        await page.locator('[data-metadata-row="seqmeta_studyid"]').waitFor({
            state: "visible",
        });
        await deleteResult(result.id);
        cleanupNeeded = false;

        await page.waitForFunction(
            () =>
                document.querySelectorAll('[aria-label="loading enrichment"]')
                    .length === 0,
        );

        expect(await page.getByText("Rendering...").count()).toBe(0);

        await page
            .locator('[data-metadata-row="seqmeta_studyid"]')
            .getByRole("button", {
                name: /Open seqmeta_id_study_lims details/i,
            })
            .click();

        const dialog = page.getByRole("dialog");

        await expect(dialog.getByText("LIB7607-71046409")).toBeVisible();

        const elapsedMs = Date.now() - startedAt;

        expect(elapsedMs).toBeLessThan(1000);
    } finally {
        if (cleanupNeeded) {
            await deleteResult(result.id);
        }
    }
});
