import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const screenshotPath = path.join(
    evidenceDir,
    "result-detail-back-authenticated-row-repro.png",
);
const domEvidencePath = path.join(
    evidenceDir,
    "result-detail-back-authenticated-row-repro-dom.json",
);

test("places authenticated result detail back link on the account row", async ({
    context,
    page,
}) => {
    test.setTimeout(150_000);
    mkdirSync(evidenceDir, { recursive: true });
    await page.setViewportSize({ width: 1280, height: 900 });
    await installResultsAuthCookie(context);

    const detailUrl =
        "/results/d5f89b64be9d4944dec2b69b84503c5d0c9b460281941b23f878a3edf937a8b1";

    await page.goto(detailUrl);

    const accountButton = page.locator('[aria-label$=" account"]');
    const backToDashboard = page.getByRole("link", {
        name: "Back to dashboard",
    });
    const authBar = page.locator('[data-results-auth-bar="true"]');
    const summary = page.locator('[data-result-detail-summary="true"]');

    await expect(accountButton).toBeVisible({ timeout: 30_000 });
    await expect(summary).toBeVisible({ timeout: 30_000 });
    await expect(backToDashboard).toBeVisible({ timeout: 30_000 });
    await expect(authBar).toBeVisible();

    const evidence = await page.evaluate(() => {
        const rectOf = (selector: string) => {
            const target = document.querySelector(selector);

            if (!(target instanceof HTMLElement)) {
                return null;
            }

            const rect = target.getBoundingClientRect();

            return {
                bottom: Math.round(rect.bottom),
                height: Math.round(rect.height),
                left: Math.round(rect.left),
                right: Math.round(rect.right),
                top: Math.round(rect.top),
                width: Math.round(rect.width),
            };
        };
        const backLink = document.querySelector('a[data-return-link="true"]');
        const account = document.querySelector('[aria-label$=" account"]');
        const authHeader = document.querySelector(
            '[data-results-auth-bar="true"]',
        );
        const summaryCard = document.querySelector(
            '[data-result-detail-summary="true"]',
        );

        return {
            accountButtonRect: rectOf('[aria-label$=" account"]'),
            accountButtonText: account?.textContent
                ?.replace(/\s+/g, " ")
                .trim(),
            authHeaderRect: rectOf('[data-results-auth-bar="true"]'),
            backInsideAuthHeader:
                backLink instanceof HTMLElement &&
                authHeader instanceof HTMLElement &&
                authHeader.contains(backLink),
            backInsideSummary:
                backLink instanceof HTMLElement &&
                summaryCard instanceof HTMLElement &&
                summaryCard.contains(backLink),
            backLinkHtml: backLink?.outerHTML ?? null,
            backLinkRect: rectOf('a[data-return-link="true"]'),
            backLinkText: backLink?.textContent?.replace(/\s+/g, " ").trim(),
            sameRow:
                account instanceof HTMLElement &&
                backLink instanceof HTMLElement &&
                Math.abs(
                    account.getBoundingClientRect().top -
                        backLink.getBoundingClientRect().top,
                ) < 8,
            summaryRect: rectOf('[data-result-detail-summary="true"]'),
            url: window.location.href,
        };
    });

    await page.screenshot({ fullPage: true, path: screenshotPath });
    writeFileSync(
        domEvidencePath,
        `${JSON.stringify(
            {
                ...evidence,
                screenshots: {
                    authenticatedDetail: screenshotPath,
                },
            },
            null,
            2,
        )}\n`,
    );

    expect(evidence.backInsideSummary).toBe(false);
    expect(evidence.backInsideAuthHeader).toBe(true);
    expect(evidence.sameRow).toBe(true);
});
