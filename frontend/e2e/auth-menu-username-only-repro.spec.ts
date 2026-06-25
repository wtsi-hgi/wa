import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const screenshotPath = path.join(
    evidenceDir,
    "auth-menu-username-only-post-fix.png",
);
const evidencePath = path.join(
    evidenceDir,
    "auth-menu-username-only-post-fix.json",
);

test("shows only the username in the signed-in account trigger and menu", async ({
    context,
    page,
}) => {
    test.setTimeout(150_000);
    mkdirSync(evidenceDir, { recursive: true });
    await page.setViewportSize({ width: 1280, height: 900 });
    await installResultsAuthCookie(context);

    await page.goto("/");

    const accountButton = page.locator(
        '[data-results-auth-bar="true"] [aria-label$=" account"]',
    );
    const accessToggle = page.getByRole("checkbox", {
        name: "Only show accessible result sets",
    });
    const accessToggleIndicator = page.locator(
        '[data-results-auth-bar="true"] [data-access-filter-state="accessible"]',
    );

    await expect(accountButton).toBeVisible({ timeout: 30_000 });
    await expect(accessToggle).toBeChecked();
    await expect(accessToggleIndicator).toBeVisible();

    const triggerEvidence = await page.evaluate(() => {
        const trigger = document.querySelector(
            '[data-results-auth-bar="true"] [aria-label$=" account"]',
        );
        const accessToggle = document.querySelector(
            '[data-results-auth-bar="true"] input[aria-label="Only show accessible result sets"]',
        );
        const accessIndicator = document.querySelector(
            '[data-results-auth-bar="true"] [data-access-filter-state]',
        );

        if (!(trigger instanceof HTMLElement)) {
            return null;
        }

        const username =
            trigger
                .getAttribute("aria-label")
                ?.replace(/\s+account$/, "")
                .trim() ?? "";
        const triggerText = trigger.textContent?.replace(/\s+/g, " ").trim();
        const badge = trigger.querySelector('[data-slot="badge"]');
        const avatar = trigger.querySelector('[data-slot="avatar"]');
        const avatarFallback = trigger.querySelector(
            '[data-slot="avatar-fallback"]',
        );
        const rect = trigger.getBoundingClientRect();
        const accessRect =
            accessIndicator instanceof HTMLElement
                ? accessIndicator.getBoundingClientRect()
                : null;

        return {
            accessToggleChecked:
                accessToggle instanceof HTMLInputElement
                    ? accessToggle.checked
                    : null,
            accessToggleState:
                accessIndicator instanceof HTMLElement
                    ? accessIndicator.dataset.accessFilterState
                    : null,
            accessToggleRect: accessRect
                ? {
                      bottom: Math.round(accessRect.bottom),
                      height: Math.round(accessRect.height),
                      left: Math.round(accessRect.left),
                      right: Math.round(accessRect.right),
                      top: Math.round(accessRect.top),
                      width: Math.round(accessRect.width),
                  }
                : null,
            avatarPresent: avatar !== null,
            avatarFallbackPresent: avatarFallback !== null,
            badgeText: badge?.textContent?.replace(/\s+/g, " ").trim() ?? null,
            triggerRect: {
                bottom: Math.round(rect.bottom),
                height: Math.round(rect.height),
                left: Math.round(rect.left),
                right: Math.round(rect.right),
                top: Math.round(rect.top),
                width: Math.round(rect.width),
            },
            triggerText,
            url: window.location.href,
            username,
            usernameVisibleInTrigger:
                username.length > 0 &&
                (triggerText?.toLowerCase().includes(username.toLowerCase()) ??
                    false),
        };
    });

    await page.screenshot({ fullPage: true, path: screenshotPath });

    await accountButton.click();

    const menu = page.getByRole("menu");

    await expect(menu).toBeVisible();

    const popupText = await menu.evaluate((element) =>
        element.textContent?.replace(/\s+/g, " ").trim(),
    );
    const popupAvatarEvidence = await menu.evaluate((element) => ({
        avatarFallbackPresent:
            element.querySelector('[data-slot="avatar-fallback"]') !== null,
        avatarPresent: element.querySelector('[data-slot="avatar"]') !== null,
    }));

    writeFileSync(
        evidencePath,
        `${JSON.stringify(
            {
                popupAvatar: popupAvatarEvidence,
                popupText,
                screenshot: screenshotPath,
                trigger: triggerEvidence,
            },
            null,
            2,
        )}\n`,
    );

    expect(triggerEvidence).not.toBeNull();
    expect(triggerEvidence?.avatarPresent).toBe(false);
    expect(triggerEvidence?.avatarFallbackPresent).toBe(false);
    expect(triggerEvidence?.triggerText).toBe(triggerEvidence?.username);
    expect(triggerEvidence?.accessToggleChecked).toBe(true);
    expect(triggerEvidence?.accessToggleState).toBe("accessible");
    expect(triggerEvidence?.accessToggleRect).not.toBeNull();
    expect(triggerEvidence?.accessToggleRect?.left).toBeGreaterThanOrEqual(
        triggerEvidence?.triggerRect.right ?? 0,
    );
    expect(popupText).toContain(triggerEvidence?.username);
    expect(popupAvatarEvidence.avatarPresent).toBe(false);
    expect(popupAvatarEvidence.avatarFallbackPresent).toBe(false);
    expect(triggerEvidence?.badgeText).toBeNull();
    expect(triggerEvidence?.usernameVisibleInTrigger).toBe(true);
});
