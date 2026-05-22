import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type BrowserContext, type Page } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const immediateScreenshotPath = path.join(
    evidenceDir,
    "logout-delay-immediate.png",
);
const finalScreenshotPath = path.join(evidenceDir, "logout-delay-final.png");
const evidencePath = path.join(evidenceDir, "logout-delay-evidence.json");

type AuthSnapshot = {
    elapsedMs: number;
    hasAuthCookie: boolean;
    lockedDetailVisible: boolean;
    loginVisible: boolean;
    logoutMenuItemDisabled: boolean;
    logoutMenuItemVisible: boolean;
    protectedDetailVisible: boolean;
    signedInTriggerVisible: boolean;
    url: string;
};

async function authSnapshot(
    page: Page,
    context: BrowserContext,
    startedAt: number,
): Promise<AuthSnapshot> {
    const cookies = await context.cookies();
    const visibleStates = await page.evaluate(() => {
        function isVisible(element: Element | null): boolean {
            if (!(element instanceof HTMLElement)) {
                return false;
            }

            const style = window.getComputedStyle(element);
            const rect = element.getBoundingClientRect();

            return (
                style.display !== "none" &&
                style.visibility !== "hidden" &&
                rect.width > 0 &&
                rect.height > 0
            );
        }

        const buttons = Array.from(document.querySelectorAll("button"));
        const loginButton = buttons.find(
            (button) => button.textContent?.trim() === "Log in",
        );
        const logoutMenuItem = document.querySelector(
            '[role="menuitem"]',
        ) as HTMLButtonElement | null;

        return {
            lockedDetailVisible: isVisible(
                document.querySelector('[data-locked-result-detail="true"]'),
            ),
            loginVisible: isVisible(loginButton ?? null),
            logoutMenuItemDisabled: logoutMenuItem?.disabled ?? false,
            logoutMenuItemVisible: isVisible(logoutMenuItem),
            protectedDetailVisible: isVisible(
                document.querySelector('[data-result-detail-summary="true"]'),
            ),
            signedInTriggerVisible: isVisible(
                document.querySelector('[aria-label$=" account"]'),
            ),
        };
    });

    return {
        ...visibleStates,
        elapsedMs: Math.round(performance.now() - startedAt),
        hasAuthCookie: cookies.some(
            (cookie) => cookie.name === "wa_results_jwt",
        ),
        url: page.url(),
    };
}

test("logout immediately revokes protected result access", async ({
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
    await expect(
        page.locator('[data-result-detail-summary="true"]'),
    ).toBeVisible({ timeout: 30_000 });

    const startedAt = performance.now();
    const beforeLogout = await authSnapshot(page, context, startedAt);
    const logoutResponsePromise = page.waitForResponse(
        (response) =>
            response.url().endsWith("/api/auth/logout") &&
            response.request().method() === "POST",
        { timeout: 5_000 },
    );

    await page.evaluate(() => {
        const accountTrigger = document.querySelector(
            '[aria-label$=" account"]',
        );

        if (!(accountTrigger instanceof HTMLElement)) {
            throw new Error("Account trigger is not available");
        }

        accountTrigger.click();
    });
    await expect(page.getByRole("menuitem", { name: "Log out" })).toBeVisible();
    await page.getByRole("menuitem", { name: "Log out" }).click();

    const logoutResponse = await logoutResponsePromise;
    const logoutResponseAtMs = Math.round(performance.now() - startedAt);

    await expect(page.getByRole("button", { name: "Log in" })).toBeVisible({
        timeout: 5_000,
    });
    await expect(
        page.locator('[data-locked-result-detail="true"]'),
    ).toBeVisible({ timeout: 5_000 });
    await expect(
        page.locator('[data-result-detail-summary="true"]'),
    ).toBeHidden({ timeout: 5_000 });

    await page.waitForTimeout(50);
    const immediate = await authSnapshot(page, context, startedAt);

    await page.screenshot({ fullPage: true, path: immediateScreenshotPath });

    const timeline: AuthSnapshot[] = [immediate];

    for (let index = 0; index < 15; index += 1) {
        await page.waitForTimeout(1_000);
        const snapshot = await authSnapshot(page, context, startedAt);
        timeline.push(snapshot);

        if (snapshot.lockedDetailVisible && !snapshot.protectedDetailVisible) {
            break;
        }
    }

    const finalSnapshot = timeline[timeline.length - 1]!;

    await page.screenshot({ fullPage: true, path: finalScreenshotPath });
    writeFileSync(
        evidencePath,
        `${JSON.stringify(
            {
                beforeLogout,
                detailUrl,
                finalSnapshot,
                logoutResponse: {
                    elapsedMs: logoutResponseAtMs,
                    ok: logoutResponse.ok(),
                    setCookie:
                        logoutResponse.headers()["set-cookie"] ??
                        logoutResponse.headers()["Set-Cookie"] ??
                        null,
                    status: logoutResponse.status(),
                    url: logoutResponse.url(),
                },
                screenshots: {
                    final: finalScreenshotPath,
                    immediate: immediateScreenshotPath,
                },
                timeline,
            },
            null,
            2,
        )}\n`,
    );

    expect(beforeLogout.hasAuthCookie).toBe(true);
    expect(beforeLogout.protectedDetailVisible).toBe(true);
    expect(logoutResponse.ok()).toBe(true);
    expect(logoutResponseAtMs).toBeLessThan(5_000);
    expect(immediate.hasAuthCookie).toBe(false);
    expect(immediate.loginVisible).toBe(true);
    expect(immediate.protectedDetailVisible).toBe(false);
    expect(immediate.lockedDetailVisible).toBe(true);
    expect(finalSnapshot.protectedDetailVisible).toBe(false);
});
