import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Locator } from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultSet,
} from "./results-auth-helpers";

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

type MetadataDetailRow = {
    key: string | null;
    text: string;
};

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const screenshotPath = path.join(
    evidenceDir,
    "result-metadata-small-mixed-repro.png",
);
const domEvidencePath = path.join(
    evidenceDir,
    "result-metadata-small-mixed-repro-dom.json",
);
const largeScreenshotPath = path.join(
    evidenceDir,
    "result-metadata-large-truncation-postfix.png",
);
const largeDomEvidencePath = path.join(
    evidenceDir,
    "result-metadata-large-truncation-postfix-dom.json",
);
const expectedVisibleKeys = ["library", "seqmeta_studyid", "study"];
const largeMetadata = {
    library: "exon",
    study: "study-alpha",
    owner: "alice",
    project: "rna-expression",
    cohort: "embryo-development",
    analysis: "differential-expression",
    seqmeta_studyid: "7607",
    seqmeta_sampleid: "7607STDY14643771",
    seqmeta_libraryid: "71046409",
    seqmeta_runid: "48522",
};

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

async function registerLargeMetadataResult(): Promise<ResultSet> {
    const outputDirectory = path.resolve(
        process.cwd(),
        "..",
        ".tmp",
        "agent",
        "e2e-large-metadata-output",
    );
    const reportPath = path.join(outputDirectory, "sample", "report.txt");

    mkdirSync(path.dirname(reportPath), { recursive: true });
    writeFileSync(reportPath, "large metadata truncation evidence\n");

    return registerResult({
        pipeline_identifier:
            "https://github.com/wtsi-hgi/wa/e2e-large-metadata-truncation",
        run_key: "runid=large-metadata-truncation",
        requester: "agent",
        operator: "agent",
        command: "nextflow run large-metadata-truncation",
        pipeline_name: "seqmeta/rendering-repro",
        pipeline_version: "2026.06",
        output_directory: outputDirectory,
        files: [
            {
                path: reportPath,
                mtime: "2026-06-01T08:00:00Z",
                size: 160,
                kind: "output",
            },
        ],
        metadata: largeMetadata,
    });
}

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

test("shows small mixed result metadata without hiding plain keys behind All metadata", async ({
    page,
}) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto("/");

    const resultLink = page
        .getByRole("link", { name: "nf-core/rnaseq" })
        .first();
    const href = await resultLink.getAttribute("href");
    const detailUrl = new URL(href ?? "/results/", page.url()).toString();

    await page.goto(detailUrl);
    await expect(page.getByRole("heading", { level: 1 })).toContainText(
        "nf-core/rnaseq",
        { timeout: 30000 },
    );

    const summary = page.locator('[data-result-detail-summary="true"]');
    const metadataStrip = summary.locator(
        '[data-result-metadata-strip="true"]',
    );

    await expect(summary).toBeVisible();
    await expect(metadataStrip).toBeVisible();

    const stripMetrics = await measureMetadataStrip(metadataStrip);
    const allMetadataButton = summary.getByRole("button", {
        name: "All metadata",
    });
    const allMetadataButtonCount = await allMetadataButton.count();
    let detailRows: MetadataDetailRow[] = [];

    if (allMetadataButtonCount > 0) {
        await allMetadataButton.click();
        await expect(
            page.locator('[data-result-metadata-details-panel="true"]'),
        ).toBeVisible();
        detailRows = await page
            .locator("[data-metadata-detail-row]")
            .evaluateAll((nodes) =>
                nodes.map((node) => ({
                    key: node.getAttribute("data-metadata-detail-row"),
                    text: node.textContent?.replace(/\s+/g, " ").trim() ?? "",
                })),
            );
    }

    const visibleKeys = stripMetrics.rows.map((row) => row.key);
    const detailKeys = detailRows.map((row) => row.key);
    const evidence = {
        route: page.url(),
        viewport: { width: 1440, height: 1000 },
        expectedVisibleKeys,
        actualVisibleKeys: visibleKeys,
        allMetadataButtonCount,
        detailKeys,
        hiddenKeysInAllMetadata: detailKeys.filter(
            (key) => key !== null && !visibleKeys.includes(key),
        ),
        strip: {
            ...stripMetrics,
            wouldScroll: stripMetrics.scrollHeight > stripMetrics.clientHeight,
        },
    };

    mkdirSync(evidenceDir, { recursive: true });
    await page.screenshot({ path: screenshotPath, fullPage: true });
    writeFileSync(domEvidencePath, `${JSON.stringify(evidence, null, 2)}\n`);

    expect(stripMetrics.lineCount).toBeLessThanOrEqual(2);
    expect(stripMetrics.scrollHeight).toBe(stripMetrics.clientHeight);
    expect(visibleKeys).toHaveLength(expectedVisibleKeys.length);
    expect(visibleKeys).toEqual(expect.arrayContaining(expectedVisibleKeys));
    await expect(allMetadataButton).toHaveCount(0);
});

test("truncates larger result metadata only when the full strip would overflow", async ({
    page,
}) => {
    const result = await registerLargeMetadataResult();
    let cleanupNeeded = true;

    try {
        await page.setViewportSize({ width: 1440, height: 1000 });
        await page.goto(`/results/${encodeURIComponent(result.id)}`, {
            waitUntil: "domcontentloaded",
        });
        await expect(page.getByRole("heading", { level: 1 })).toContainText(
            "seqmeta/rendering-repro",
            { timeout: 30000 },
        );

        const summary = page.locator('[data-result-detail-summary="true"]');
        const metadataStrip = summary.locator(
            '[data-result-metadata-strip="true"]',
        );
        const allMetadataButton = summary.getByRole("button", {
            name: "All metadata",
        });

        await expect(summary).toBeVisible();
        await expect(metadataStrip).toBeVisible();
        await expect(allMetadataButton).toHaveCount(1);

        const stripMetrics = await measureMetadataStrip(metadataStrip);
        const visibleKeys = stripMetrics.rows.map((row) => row.key);

        await allMetadataButton.click();
        await expect(
            page.locator('[data-result-metadata-details-panel="true"]'),
        ).toBeVisible();
        const detailRows = await page
            .locator("[data-metadata-detail-row]")
            .evaluateAll((nodes) =>
                nodes.map((node) => ({
                    key: node.getAttribute("data-metadata-detail-row"),
                    text: node.textContent?.replace(/\s+/g, " ").trim() ?? "",
                })),
            );
        const detailKeys = detailRows.map((row) => row.key);
        const evidence = {
            route: page.url(),
            viewport: { width: 1440, height: 1000 },
            totalMetadataKeys: Object.keys(largeMetadata),
            actualVisibleKeys: visibleKeys,
            allMetadataButtonCount: await allMetadataButton.count(),
            detailKeys,
            hiddenKeysInAllMetadata: detailKeys.filter(
                (key) => key !== null && !visibleKeys.includes(key),
            ),
            strip: {
                ...stripMetrics,
                wouldScroll:
                    stripMetrics.scrollHeight > stripMetrics.clientHeight,
            },
        };

        mkdirSync(evidenceDir, { recursive: true });
        await summary.screenshot({ path: largeScreenshotPath });
        writeFileSync(
            largeDomEvidencePath,
            `${JSON.stringify(evidence, null, 2)}\n`,
        );

        expect(stripMetrics.lineCount).toBeLessThanOrEqual(2);
        expect(stripMetrics.scrollHeight).toBe(stripMetrics.clientHeight);
        expect(visibleKeys.length).toBeLessThan(
            Object.keys(largeMetadata).length,
        );
        expect(visibleKeys.every((key) => key?.startsWith("seqmeta_"))).toBe(
            true,
        );
        expect(detailKeys).toEqual(
            expect.arrayContaining(Object.keys(largeMetadata)),
        );
        expect(evidence.hiddenKeysInAllMetadata.length).toBeGreaterThan(0);

        deleteResult(result.id);
        cleanupNeeded = false;
    } finally {
        if (cleanupNeeded) {
            deleteResult(result.id);
        }
    }
});
