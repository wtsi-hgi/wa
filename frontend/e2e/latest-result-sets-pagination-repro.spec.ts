import { mkdirSync } from "node:fs";
import path from "node:path";

import { expect, request, test } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const screenshotPath = path.join(
    repoRoot,
    ".tmp",
    "agent",
    "latest-result-sets-pagination-repro.png",
);
const projectColumnScreenshotPath = path.join(
    repoRoot,
    ".tmp",
    "agent",
    "latest-result-sets-project-column-repro.png",
);

type LatestResultSet = {
    pipeline_name: string;
    run_key: string;
    metadata?: Record<string, string>;
};

type StatsResponse = {
    total: number;
    recent: LatestResultSet[];
};

function uniqueToken(runKey: string): string {
    return new URLSearchParams(runKey).get("unique") ?? runKey;
}

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

test("latest result sets pagination and rows per page expose all stats rows", async ({
    page,
}) => {
    const resultsPort = process.env.WA_TEST_RESULTS_PORT;

    if (!resultsPort) {
        throw new Error("WA_TEST_RESULTS_PORT is required for this repro");
    }

    const api = await request.newContext({
        baseURL: `https://127.0.0.1:${resultsPort}`,
        ignoreHTTPSErrors: true,
    });
    const statsResponse = await api.get("/rest/v1/results/stats?recent=100");

    expect(statsResponse.ok()).toBe(true);

    const stats = (await statsResponse.json()) as StatsResponse;
    const expectedVisibleRows = Math.min(stats.total, 25);

    await api.dispose();

    expect(stats.total).toBeGreaterThan(10);
    expect(stats.recent.length).toBeGreaterThan(10);

    mkdirSync(path.dirname(screenshotPath), { recursive: true });

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    await expect(page.getByText("Latest result sets")).toBeVisible();
    await expect(page.locator('tbody tr[data-result-row="true"]')).toHaveCount(
        10,
    );
    await expect(page.getByText(`${stats.total} rows`)).toBeVisible();
    await expect(
        page.getByText(`Page 1 of ${Math.ceil(stats.total / 10)}`),
    ).toBeVisible();

    await page.getByRole("button", { name: "Next page" }).click();
    await expect(page.locator('tbody tr[data-result-row="true"]')).toHaveCount(
        Math.min(10, stats.total - 10),
    );
    await expect(page.getByText("Page 2 of")).toBeVisible();

    await page.getByLabel("Rows per page").selectOption("25");
    await page.screenshot({ path: screenshotPath, fullPage: true });

    await expect(page.locator('tbody tr[data-result-row="true"]')).toHaveCount(
        expectedVisibleRows,
    );
});

test("latest result sets project column leads with pipeline fallback", async ({
    page,
}) => {
    const resultsPort = process.env.WA_TEST_RESULTS_PORT;

    if (!resultsPort) {
        throw new Error("WA_TEST_RESULTS_PORT is required for this repro");
    }

    const api = await request.newContext({
        baseURL: `https://127.0.0.1:${resultsPort}`,
        ignoreHTTPSErrors: true,
    });
    const statsResponse = await api.get("/rest/v1/results/stats?recent=100");

    expect(statsResponse.ok()).toBe(true);

    const stats = (await statsResponse.json()) as StatsResponse;

    await api.dispose();

    const projectResult = stats.recent.find(
        (result) => result.metadata?.project,
    );
    const fallbackResult = stats.recent.find(
        (result) => !result.metadata?.project,
    );

    expect(
        projectResult,
        "dev fixtures should include a latest result with project metadata",
    ).toBeTruthy();
    expect(
        fallbackResult,
        "dev fixtures should include a latest result without project metadata",
    ).toBeTruthy();

    mkdirSync(path.dirname(projectColumnScreenshotPath), { recursive: true });

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    await expect(page.getByText("Latest result sets")).toBeVisible();
    await expect(page.locator("thead th").first()).toBeVisible();

    const headerLabels = (await page.locator("thead th").allTextContents()).map(
        (label) => label.trim(),
    );
    const projectRow = page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasText: uniqueToken(projectResult!.run_key) });
    const fallbackRow = page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasText: uniqueToken(fallbackResult!.run_key) });

    expect.soft(headerLabels.slice(0, 2)).toEqual(["Project", "Unique"]);
    expect.soft(headerLabels).not.toContain("Pipeline Name");
    await expect
        .soft(projectRow.locator("td").first())
        .toHaveText(projectResult!.metadata!.project);
    await expect
        .soft(fallbackRow.locator("td").first())
        .toHaveText(fallbackResult!.pipeline_name);

    await page
        .getByRole("button", { name: "Toggle column visibility" })
        .click();

    const pipelineNameToggle = page.locator(
        'button[role="menuitemcheckbox"][data-column-id="pipeline_name"]',
    );

    await expect.soft(pipelineNameToggle).toBeVisible();
    await expect
        .soft(pipelineNameToggle)
        .toHaveAttribute("aria-checked", "false");
    await page.screenshot({
        path: projectColumnScreenshotPath,
        fullPage: true,
    });
});
