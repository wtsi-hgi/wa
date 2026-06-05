import { mkdirSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const postFixScreenshotPath = path.join(
    evidenceDir,
    "raw-run-key-popup-postfix.png",
);
const seededRnaseqResultPath =
    "/results/105b6601ff53101ba1413e6a499a59a07bce3afc02ec6210fd6e4ba1d24441f7";

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

test("omits the raw stored run key from the Run details popup", async ({
    page,
}) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto(seededRnaseqResultPath, { waitUntil: "domcontentloaded" });

    const summary = page.locator('[data-result-detail-summary="true"]');
    const detailsTrigger = summary.getByRole("button", {
        name: "All details",
    });

    await expect(summary).toBeVisible();
    await expect(page.getByRole("heading", { level: 1 })).toContainText(
        "nf-core/rnaseq",
    );
    await detailsTrigger.click();

    const detailsPanel = page.locator(
        '[data-registration-details-panel="true"]',
    );
    const detailLabels = detailsPanel.locator(
        "[data-registration-detail-field] dt",
    );

    await expect(detailsPanel).toBeVisible();
    await expect(
        page.locator('[data-registration-detail-field="Unique"]'),
    ).toContainText("48522 / exon_lib");
    await expect(detailLabels.filter({ hasText: /^RAW RUN KEY$/ })).toHaveCount(
        0,
    );
    await expect(detailsPanel).not.toContainText("runid=48522&unique=exon_lib");

    mkdirSync(evidenceDir, { recursive: true });
    await detailsPanel.screenshot({ path: postFixScreenshotPath });
});
