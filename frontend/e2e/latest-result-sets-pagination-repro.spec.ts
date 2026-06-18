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

type StatsResponse = {
    total: number;
    recent: unknown[];
};

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
