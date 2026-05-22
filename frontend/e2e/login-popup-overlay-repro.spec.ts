import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const screenshotPath = path.join(
    evidenceDir,
    "login-popup-overlay-postfix.png",
);
const evidencePath = path.join(evidenceDir, "login-popup-overlay-postfix.json");

type RectSnapshot = {
    bottom: number;
    height: number;
    left: number;
    right: number;
    top: number;
    width: number;
};

type LoginOverlaySnapshot = {
    authBar: RectSnapshot | null;
    lockedSection: RectSnapshot | null;
    loginForm: RectSnapshot | null;
    main: RectSnapshot | null;
    scrollY: number;
};

async function snapshotLoginOverlayGeometry(
    page: Page,
): Promise<LoginOverlaySnapshot> {
    return page.evaluate(() => {
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

        return {
            authBar: rectOf('[data-results-auth-bar="true"]'),
            lockedSection: rectOf('[data-locked-result-detail="true"] section'),
            loginForm: rectOf('form[aria-label="Log in"]'),
            main: rectOf('[data-locked-result-detail="true"]'),
            scrollY: Math.round(window.scrollY),
        };
    });
}

test("login popup overlays the protected result page without shifting layout", async ({
    context,
    page,
}) => {
    test.setTimeout(150_000);
    mkdirSync(evidenceDir, { recursive: true });
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

    await expect(page.getByRole("button", { name: "Log in" })).toBeVisible();
    await expect(
        page.locator('[data-locked-result-detail="true"] section'),
    ).toBeVisible({ timeout: 30_000 });

    const before = await snapshotLoginOverlayGeometry(page);

    await page.getByRole("button", { name: "Log in" }).click();
    await expect(page.getByRole("form", { name: "Log in" })).toBeVisible();

    const after = await snapshotLoginOverlayGeometry(page);

    await page.screenshot({ fullPage: true, path: screenshotPath });
    writeFileSync(
        evidencePath,
        `${JSON.stringify(
            {
                after,
                authBarShiftPx:
                    after.authBar && before.authBar
                        ? after.authBar.height - before.authBar.height
                        : null,
                before,
                detailUrl,
                lockedSectionShiftPx:
                    after.lockedSection && before.lockedSection
                        ? after.lockedSection.top - before.lockedSection.top
                        : null,
                mainShiftPx:
                    after.main && before.main
                        ? after.main.top - before.main.top
                        : null,
                screenshotPath,
            },
            null,
            2,
        )}\n`,
    );

    expect(before.authBar).not.toBeNull();
    expect(before.lockedSection).not.toBeNull();
    expect(before.main).not.toBeNull();
    expect(after.authBar).not.toBeNull();
    expect(after.lockedSection).not.toBeNull();
    expect(after.loginForm).not.toBeNull();
    expect(after.main).not.toBeNull();

    expect(after.authBar!.height).toBeLessThanOrEqual(
        before.authBar!.height + 2,
    );
    expect(after.main!.top).toBe(before.main!.top);
    expect(after.lockedSection!.top).toBe(before.lockedSection!.top);
    expect(after.scrollY).toBe(before.scrollY);
});
