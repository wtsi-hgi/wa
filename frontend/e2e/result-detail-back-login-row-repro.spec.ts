import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const screenshotPath = path.join(
    evidenceDir,
    "result-detail-back-login-row-fixed.png",
);
const domEvidencePath = path.join(
    evidenceDir,
    "result-detail-back-login-row-repro-dom.json",
);

test("places back-to-dashboard on the login row outside the locked detail box", async ({
    context,
    page,
}) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await installResultsAuthCookie(context);
    await page.goto("/");

    const resultLink = page
        .getByRole("link", { name: "nf-core/rnaseq" })
        .first();
    const href = await resultLink.getAttribute("href");
    const detailUrl = new URL(href ?? "/results/", page.url()).toString();

    await context.clearCookies();
    await page.goto(detailUrl);

    const loginButton = page.getByRole("button", { name: "Log in" });
    await expect(loginButton).toBeVisible();
    await expect(
        page.getByRole("heading", {
            level: 1,
            name: "You do not have access to this result set",
        }),
    ).toBeVisible({ timeout: 30000 });

    const backToDashboard = page.getByRole("link", {
        name: "Back to dashboard",
    });
    const lockedDetail = page.locator('[data-locked-result-detail="true"]');
    const lockedBox = lockedDetail.locator("section").first();
    const authBar = page.locator('[data-results-auth-bar="true"]');

    await expect(backToDashboard).toBeVisible();
    await expect(lockedBox).toBeVisible();
    await expect(authBar).toBeVisible();
    await expect(
        authBar.locator('a[aria-label="Back to dashboard"]'),
    ).toBeVisible();
    await expect(
        lockedBox.locator('a[aria-label="Back to dashboard"]'),
    ).toHaveCount(0);

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
        const backLink = document.querySelector(
            'a[aria-label="Back to dashboard"]',
        );
        const login = Array.from(document.querySelectorAll("button")).find(
            (button) => button.textContent?.trim() === "Log in",
        );
        const lockedSection = document.querySelector(
            '[data-locked-result-detail="true"] section',
        );
        const authHeader = document.querySelector(
            '[data-results-auth-bar="true"]',
        );

        return {
            authHeaderRect: rectOf('[data-results-auth-bar="true"]'),
            backLinkRect: rectOf('a[aria-label="Back to dashboard"]'),
            backLinkText: backLink?.textContent?.replace(/\s+/g, " ").trim(),
            lockedSectionRect: rectOf(
                '[data-locked-result-detail="true"] section',
            ),
            loginButtonRect: login
                ? (() => {
                      const rect = login.getBoundingClientRect();

                      return {
                          bottom: Math.round(rect.bottom),
                          height: Math.round(rect.height),
                          left: Math.round(rect.left),
                          right: Math.round(rect.right),
                          top: Math.round(rect.top),
                          width: Math.round(rect.width),
                      };
                  })()
                : null,
            loginButtonText: login?.textContent?.replace(/\s+/g, " ").trim(),
            sameRow:
                login instanceof HTMLElement &&
                backLink instanceof HTMLElement &&
                Math.abs(
                    login.getBoundingClientRect().top -
                        backLink.getBoundingClientRect().top,
                ) < 8,
            backInsideLockedSection:
                backLink instanceof HTMLElement &&
                lockedSection instanceof HTMLElement &&
                lockedSection.contains(backLink),
            backInsideAuthHeader:
                backLink instanceof HTMLElement &&
                authHeader instanceof HTMLElement &&
                authHeader.contains(backLink),
        };
    });

    mkdirSync(evidenceDir, { recursive: true });
    await page.screenshot({ fullPage: true, path: screenshotPath });
    writeFileSync(domEvidencePath, `${JSON.stringify(evidence, null, 2)}\n`);

    expect(evidence.backInsideLockedSection).toBe(false);
    expect(evidence.backInsideAuthHeader).toBe(true);
    expect(evidence.sameRow).toBe(true);
});
