import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Locator } from "@playwright/test";

type ResultSet = {
    id: string;
};

type MetadataStripMetrics = {
    clientHeight: number;
    lineCount: number;
    overflowY: string;
    rows: Array<{
        key: string | null;
        text: string;
        width: number;
        x: number;
        y: number;
    }>;
    scrollHeight: number;
};

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const postFixScreenshotPath = path.join(
    evidenceDir,
    "metadata-layout-postfix.png",
);
const postFixDomEvidencePath = path.join(
    evidenceDir,
    "metadata-layout-postfix-dom.json",
);

function resultsBackendUrl(): string {
    const port = process.env.WA_TEST_RESULTS_PORT;

    if (!port) {
        throw new Error("WA_TEST_RESULTS_PORT is required for this test");
    }

    return `http://127.0.0.1:${port}`;
}

async function registerMetadataLayoutResult(): Promise<ResultSet> {
    const outputDirectory = path.resolve(
        process.cwd(),
        "..",
        ".tmp",
        "agent",
        "e2e-metadata-layout-output",
    );
    const response = await fetch(`${resultsBackendUrl()}/results`, {
        method: "POST",
        headers: {
            "content-type": "application/json",
        },
        body: JSON.stringify({
            pipeline_identifier:
                "https://github.com/wtsi-hgi/wa/e2e-metadata-layout-repro",
            run_key:
                "runid=48522&study=7607&sample=7607STDY14643771&library=71046409",
            requester: "agent",
            operator: "agent",
            command: "nextflow run seqmeta-rendering-repro",
            pipeline_name: "seqmeta/rendering-repro",
            pipeline_version: "2026.05",
            output_directory: outputDirectory,
            files: [
                {
                    path: path.join(outputDirectory, "sample", "report.txt"),
                    mtime: "2026-05-20T08:00:00Z",
                    size: 120,
                    kind: "output",
                },
            ],
            metadata: {
                seqmeta_libraryid: "71046409",
                seqmeta_librarytype: "Custom",
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

async function measureMetadataStrip(
    strip: Locator,
): Promise<MetadataStripMetrics> {
    return strip.evaluate((element) => {
        const rowElements = Array.from(
            element.querySelectorAll<HTMLElement>("[data-metadata-row]"),
        );
        const yValues = new Set(
            rowElements.map((row) => Math.round(row.getBoundingClientRect().y)),
        );
        const computed = window.getComputedStyle(element);

        return {
            clientHeight: element.clientHeight,
            lineCount: yValues.size,
            overflowY: computed.overflowY,
            rows: rowElements.map((row) => {
                const rect = row.getBoundingClientRect();

                return {
                    key: row.getAttribute("data-metadata-row"),
                    text: row.textContent?.replace(/\s+/g, " ").trim() ?? "",
                    width: Math.round(rect.width),
                    x: Math.round(rect.x),
                    y: Math.round(rect.y),
                };
            }),
            scrollHeight: element.scrollHeight,
        };
    });
}

test("keeps five result metadata items on two stable lines without vertical overflow", async ({
    page,
}) => {
    const result = await registerMetadataLayoutResult();
    let cleanupNeeded = true;

    try {
        await page.setViewportSize({ width: 1180, height: 900 });
        await page.goto(`/results/${encodeURIComponent(result.id)}`, {
            waitUntil: "domcontentloaded",
        });

        const strip = page.locator('[data-result-metadata-strip="true"]');

        await expect(strip).toBeVisible();
        await expect(strip.locator("[data-metadata-row]")).toHaveCount(5);

        const before = await measureMetadataStrip(strip);

        const firstDirectory = page.locator("[data-directory-path]").first();

        await expect(firstDirectory).toBeVisible();
        await firstDirectory.click();
        await expect(firstDirectory).toHaveAttribute(
            "data-directory-expanded",
            "false",
        );

        const after = await measureMetadataStrip(strip);
        const evidence = {
            route: `/results/${result.id}`,
            viewport: { width: 1180, height: 900 },
            before,
            after,
        };

        mkdirSync(evidenceDir, { recursive: true });
        await page
            .locator('[data-result-detail-summary="true"]')
            .screenshot({ path: postFixScreenshotPath });
        writeFileSync(
            postFixDomEvidencePath,
            `${JSON.stringify(evidence, null, 2)}\n`,
        );

        expect(before.lineCount).toBe(2);
        expect(before.scrollHeight).toBe(before.clientHeight);
        expect(after.lineCount).toBe(before.lineCount);
        expect(after.scrollHeight).toBe(after.clientHeight);
        expect(after.rows.map((row) => row.y)).toEqual(
            before.rows.map((row) => row.y),
        );
        expect(after.rows.map((row) => row.key)).toEqual(
            before.rows.map((row) => row.key),
        );

        await deleteResult(result.id);
        cleanupNeeded = false;
    } finally {
        if (cleanupNeeded) {
            await deleteResult(result.id);
        }
    }
});
