import { mkdirSync, readdirSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";
import { DatabaseSync } from "node:sqlite";

import { expect, test } from "@playwright/test";

import {
    deleteResult,
    installResultsAuthCookie,
    registerResult,
    type ResultSet,
} from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");
const fixtureRoot = path.join(evidenceDir, "date-timezone-fixture");
const screenshotPath = path.join(
    evidenceDir,
    "result-date-timezone-london-utc-repro.png",
);
const evidencePath = screenshotPath.replace(/\.png$/, ".json");
const pinnedTimestamp = "2026-06-19T09:03:29Z";

test.use({ timezoneId: "Europe/London" });

function formatRegistrationTimestamp(value: string, timeZone: string): string {
    return new Date(value).toLocaleString("en-GB", {
        year: "numeric",
        month: "short",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        timeZone,
    });
}

function formatFileTimestamp(value: string, timeZone: string): string {
    const parts = new Intl.DateTimeFormat("en-GB", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
        timeZone,
    }).formatToParts(new Date(value));
    const part = (type: string) =>
        parts.find((entry) => entry.type === type)?.value ?? "";

    return `${part("year")}-${part("month")}-${part("day")} ${part("hour")}:${part("minute")}`;
}

function resultDatabaseCandidates(): string[] {
    const tmpDir = path.join(repoRoot, ".tmp");

    return readdirSync(tmpDir)
        .filter((entry) => /^results-dev\..+\.sqlite$/.test(entry))
        .map((entry) => path.join(tmpDir, entry))
        .sort((left, right) => {
            return statSync(right).mtimeMs - statSync(left).mtimeMs;
        });
}

function pinResultTimestamps(
    resultId: string,
    timestamp: string,
): { databasePath: string; scannedDatabasePaths: string[] } {
    const scannedDatabasePaths = resultDatabaseCandidates();
    const errors: string[] = [];

    for (const databasePath of scannedDatabasePaths) {
        let database: DatabaseSync | null = null;

        try {
            database = new DatabaseSync(databasePath);
            database.exec("PRAGMA busy_timeout = 5000");

            const result = database
                .prepare(
                    "UPDATE result_sets SET created_at = ?, updated_at = ? WHERE id = ?",
                )
                .run(timestamp, timestamp, resultId);

            if (result.changes > 0) {
                return { databasePath, scannedDatabasePaths };
            }
        } catch (error) {
            errors.push(
                `${databasePath}: ${
                    error instanceof Error ? error.message : String(error)
                }`,
            );
        } finally {
            database?.close();
        }
    }

    throw new Error(
        [
            `Could not pin timestamps for result ${resultId}.`,
            `Scanned: ${scannedDatabasePaths.join(", ") || "(none)"}.`,
            `Errors: ${errors.join("; ") || "(none)"}.`,
        ].join(" "),
    );
}

function registerTimezoneFixtureResult(token: string): ResultSet {
    const outputDirectory = path.join(fixtureRoot, token);
    const reportPath = path.join(outputDirectory, "report.txt");

    mkdirSync(outputDirectory, { recursive: true });
    writeFileSync(reportPath, "timezone rendering repro\n");

    return registerResult({
        pipeline_identifier: `https://github.com/wtsi-hgi/wa/date-timezone-repro/${token}`,
        run_key: `runid=timezone-repro&unique=${token}`,
        requester: "agent",
        operator: "agent",
        command: "nextflow run timezone-rendering-repro",
        pipeline_name: "wtsi/date-timezone-repro",
        pipeline_version: "2026.06",
        output_directory: outputDirectory,
        files: [
            {
                path: reportPath,
                mtime: pinnedTimestamp,
                size: 24,
                kind: "output",
            },
        ],
        metadata: {
            project: "timezone-local-repro",
        },
    });
}

test("renders registration timestamps in the browser timezone", async ({
    context,
    page,
}) => {
    const token = `bst-${Date.now().toString(36)}`;
    const result = registerTimezoneFixtureResult(token);
    let cleanupNeeded = true;

    try {
        const databaseEvidence = pinResultTimestamps(
            result.id,
            pinnedTimestamp,
        );
        await installResultsAuthCookie(context);
        await page.setViewportSize({ width: 1180, height: 860 });
        await page.goto(`/results/${encodeURIComponent(result.id)}`, {
            waitUntil: "domcontentloaded",
        });

        const summary = page.locator('[data-result-detail-summary="true"]');
        const lastUpdatedField = summary.locator(
            '[data-registration-field="Last updated"]',
        );
        const fileButton = page
            .locator("button[data-file-path]")
            .filter({ hasText: "report.txt" });

        await expect(summary).toBeVisible();
        await expect(lastUpdatedField).toBeVisible();
        await expect(fileButton).toBeVisible();

        const utcLabel = formatRegistrationTimestamp(pinnedTimestamp, "UTC");
        const londonLabel = formatRegistrationTimestamp(
            pinnedTimestamp,
            "Europe/London",
        );
        const utcFileLabel = formatFileTimestamp(pinnedTimestamp, "UTC");
        const londonFileLabel = formatFileTimestamp(
            pinnedTimestamp,
            "Europe/London",
        );
        await expect(lastUpdatedField).toContainText(londonLabel);
        await expect(fileButton).toContainText(londonFileLabel);

        const visibleFieldText = ((await lastUpdatedField.textContent()) ?? "")
            .replace(/\s+/g, " ")
            .trim();
        const visibleFileText = ((await fileButton.textContent()) ?? "")
            .replace(/\s+/g, " ")
            .trim();
        const browserTimeZone = await page.evaluate(
            () => Intl.DateTimeFormat().resolvedOptions().timeZone,
        );
        const evidence = {
            browserTimeZone,
            databasePath: databaseEvidence.databasePath,
            expectedLocalLabel: londonLabel,
            fixtureTimestamp: pinnedTimestamp,
            resultId: result.id,
            route: `/results/${result.id}`,
            scannedDatabasePaths: databaseEvidence.scannedDatabasePaths,
            screenshotPath,
            utcFileLabel,
            utcLabel,
            visibleFileText,
            visibleFieldText,
        };

        mkdirSync(evidenceDir, { recursive: true });
        await summary.screenshot({
            animations: "disabled",
            path: screenshotPath,
        });
        writeFileSync(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`);

        expect(browserTimeZone).toBe("Europe/London");
        expect(utcLabel).toBe("19 Jun 2026, 09:03");
        expect(londonLabel).toBe("19 Jun 2026, 10:03");
        expect(utcFileLabel).toBe("2026-06-19 09:03");
        expect(londonFileLabel).toBe("2026-06-19 10:03");
        expect(visibleFieldText).toContain(londonLabel);
        expect(visibleFieldText).not.toContain(utcLabel);
        expect(visibleFileText).toContain(londonFileLabel);
        expect(visibleFileText).not.toContain(utcFileLabel);

        deleteResult(result.id);
        cleanupNeeded = false;
    } finally {
        if (cleanupNeeded) {
            deleteResult(result.id);
        }
    }
});
