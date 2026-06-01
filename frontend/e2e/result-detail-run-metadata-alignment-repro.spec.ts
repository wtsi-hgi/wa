import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import {
    expect,
    test,
    type BrowserContext,
    type Locator,
    type Page,
} from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultSet,
} from "./results-auth-helpers";

type BoxMetrics = {
    bottom: number;
    height: number;
    top: number;
};

type AlignmentMetrics = {
    metadataAllButtonCount: number;
    metadataLineCount: number;
    metadataSection: {
        firstValue: BoxMetrics;
        title: BoxMetrics;
    };
    route: string;
    runDetailsSection: {
        allButton: BoxMetrics;
        firstValue: BoxMetrics;
        title: BoxMetrics;
    };
    viewport: {
        height: number;
        width: number;
    };
};

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const postFixScreenshotPath = path.join(
    evidenceDir,
    "bug1-run-details-metadata-alignment-post-fix.png",
);
const postFixMetricsPath = path.join(
    evidenceDir,
    "bug1-run-details-metadata-alignment-post-fix.json",
);

async function registerMetadataAlignmentResult({
    caseName,
    metadata,
}: {
    caseName: string;
    metadata: Record<string, string>;
}): Promise<ResultSet> {
    const outputDirectory = path.resolve(
        process.cwd(),
        "..",
        ".tmp",
        "agent",
        `e2e-run-metadata-alignment-output-${caseName}`,
    );
    const reportPath = path.join(outputDirectory, "summary", "report.txt");

    mkdirSync(path.dirname(reportPath), { recursive: true });
    writeFileSync(reportPath, "run details metadata alignment evidence\n");

    return registerResult({
        pipeline_identifier:
            "https://github.com/wtsi-hgi/wa/e2e-run-metadata-alignment-repro",
        run_key: `bug=1&case=${caseName}`,
        requester: "alignment-agent",
        operator: "alignment-agent",
        command: "nextflow run visual-alignment-repro",
        pipeline_name: "visual/alignment-repro",
        pipeline_version: "2026.06",
        output_directory: outputDirectory,
        files: [
            {
                path: reportPath,
                mtime: "2026-06-01T08:00:00Z",
                size: 128,
                kind: "output",
            },
        ],
        metadata,
    });
}

async function measureAlignment(
    summary: Locator,
    route: string,
): Promise<AlignmentMetrics> {
    return summary.evaluate((element, measuredRoute) => {
        function boxMetrics(found: Element): BoxMetrics {
            const rect = found.getBoundingClientRect();

            return {
                bottom: Math.round(rect.bottom),
                height: Math.round(rect.height),
                top: Math.round(rect.top),
            };
        }

        function requiredElement(selector: string): HTMLElement {
            const found = element.querySelector(selector);

            if (!(found instanceof HTMLElement)) {
                throw new Error(`Missing alignment element: ${selector}`);
            }

            return found;
        }

        const runDetailsTitle = requiredElement(
            '[data-registration-header="true"] p',
        );
        const runDetailsAllButton = requiredElement(
            '[data-registration-details-trigger="true"]',
        );
        const runDetailsFirstValue = requiredElement(
            '[data-registration-field-strip="true"] [data-registration-field]',
        );
        const metadataLayout = requiredElement(
            '[data-result-metadata-layout="integrated"]',
        );
        const metadataTitle = requiredElement(
            '[data-result-metadata-layout="integrated"] p',
        );
        const metadataFirstValue = requiredElement(
            '[data-result-metadata-strip="true"] [data-metadata-row]',
        );
        const metadataRows = Array.from(
            element.querySelectorAll<HTMLElement>(
                '[data-result-metadata-strip="true"] [data-metadata-row]',
            ),
        );
        const metadataLineCount = new Set(
            metadataRows.map((row) =>
                Math.round(row.getBoundingClientRect().top),
            ),
        ).size;

        return {
            metadataAllButtonCount: metadataLayout.querySelectorAll(
                '[data-metadata-details-trigger="true"]',
            ).length,
            metadataLineCount,
            metadataSection: {
                firstValue: boxMetrics(metadataFirstValue),
                title: boxMetrics(metadataTitle),
            },
            route: measuredRoute,
            runDetailsSection: {
                allButton: boxMetrics(runDetailsAllButton),
                firstValue: boxMetrics(runDetailsFirstValue),
                title: boxMetrics(runDetailsTitle),
            },
            viewport: {
                height: window.innerHeight,
                width: window.innerWidth,
            },
        };
    }, route);
}

async function assertAlignedRunDetailsAndMetadata({
    caseName,
    context,
    expectedMetadataLineCount,
    expectedMetadataRows,
    metadata,
    page,
    recordPostFixEvidence = false,
}: {
    caseName: string;
    context: BrowserContext;
    expectedMetadataLineCount?: number;
    expectedMetadataRows: number;
    metadata: Record<string, string>;
    page: Page;
    recordPostFixEvidence?: boolean;
}) {
    const result = await registerMetadataAlignmentResult({
        caseName,
        metadata,
    });
    let cleanupNeeded = true;

    try {
        await installResultsAuthCookie(context);
        await page.setViewportSize({ width: 1180, height: 900 });

        const route = `/results/${encodeURIComponent(result.id)}`;

        await page.goto(route, { waitUntil: "domcontentloaded" });

        const summary = page.locator('[data-result-detail-summary="true"]');

        await expect(summary).toBeVisible();
        await expect(
            summary.locator('[data-registration-details-trigger="true"]'),
        ).toBeVisible();
        await expect(
            summary.locator('[data-metadata-details-trigger="true"]'),
        ).toHaveCount(0);
        await expect(summary.locator("[data-metadata-row]")).toHaveCount(
            expectedMetadataRows,
        );

        mkdirSync(evidenceDir, { recursive: true });
        const metrics = await measureAlignment(summary, route);

        if (recordPostFixEvidence) {
            await summary.screenshot({ path: postFixScreenshotPath });
            writeFileSync(
                postFixMetricsPath,
                `${JSON.stringify(metrics, null, 2)}\n`,
            );
        }

        expect(metrics.metadataAllButtonCount).toBe(0);
        if (expectedMetadataLineCount !== undefined) {
            expect(metrics.metadataLineCount).toBe(expectedMetadataLineCount);
        } else {
            expect(metrics.metadataLineCount).toBeLessThanOrEqual(2);
        }
        expect(metrics.runDetailsSection.allButton.height).toBeGreaterThan(0);
        expect(
            Math.abs(
                metrics.runDetailsSection.title.top -
                    metrics.metadataSection.title.top,
            ),
        ).toBeLessThanOrEqual(1);
        expect(
            Math.abs(
                metrics.runDetailsSection.firstValue.top -
                    metrics.metadataSection.firstValue.top,
            ),
        ).toBeLessThanOrEqual(1);

        deleteResult(result.id);
        cleanupNeeded = false;
    } finally {
        if (cleanupNeeded) {
            deleteResult(result.id);
        }
    }
}

test("aligns Run details and Metadata when only Run details has an all button", async ({
    context,
    page,
}) => {
    await assertAlignedRunDetailsAndMetadata({
        caseName: "single-metadata-key",
        context,
        expectedMetadataRows: 1,
        metadata: {
            sample: "single-visible-key",
        },
        page,
        recordPostFixEvidence: true,
    });
});

test("aligns Run details and two-line Metadata when Metadata has no all button", async ({
    context,
    page,
}) => {
    await assertAlignedRunDetailsAndMetadata({
        caseName: "two-line-metadata",
        context,
        expectedMetadataLineCount: 2,
        expectedMetadataRows: 4,
        metadata: {
            library: "exon-library",
            owner: "alignment-agent",
            project: "metadata-layout",
            sample: "two-line-visible-key",
        },
        page,
    });
});
