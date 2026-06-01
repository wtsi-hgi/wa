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
const shortViewportScreenshotPath = path.join(
    evidenceDir,
    "login-popup-overlay-short-viewport-postfix.png",
);
const shortViewportEvidencePath = path.join(
    evidenceDir,
    "login-popup-overlay-short-viewport-postfix.json",
);
const dashboardScreenshotPath = path.join(
    evidenceDir,
    "login-popup-overlay-dashboard-current.png",
);
const dashboardEvidencePath = path.join(
    evidenceDir,
    "login-popup-overlay-dashboard-current.json",
);

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

type StablePageSnapshot = {
    authBar: RectSnapshot | null;
    backLink: RectSnapshot | null;
    headerActions: RectSnapshot | null;
    loginButton: RectSnapshot | null;
    loginForm: RectSnapshot | null;
    main: RectSnapshot | null;
    scrollY: number;
};

type DashboardSnapshot = {
    authBar: RectSnapshot | null;
    headerActions: RectSnapshot | null;
    loginButton: RectSnapshot | null;
    loginForm: RectSnapshot | null;
    main: RectSnapshot | null;
    resultsHeading: RectSnapshot | null;
    searchBuilder: RectSnapshot | null;
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

async function snapshotStablePageGeometry(
    page: Page,
): Promise<StablePageSnapshot> {
    return page.evaluate(() => {
        const roundedRect = (target: Element | null) => {
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
        const loginButton =
            Array.from(document.querySelectorAll("button")).find(
                (button) => button.textContent?.trim() === "Log in",
            ) ?? null;

        return {
            authBar: roundedRect(
                document.querySelector('[data-results-auth-bar="true"]'),
            ),
            backLink: roundedRect(
                document.querySelector('a[data-return-link="true"]'),
            ),
            headerActions: roundedRect(
                document.querySelector('[data-results-header-actions="true"]'),
            ),
            loginButton: roundedRect(loginButton),
            loginForm: roundedRect(
                document.querySelector('form[aria-label="Log in"]'),
            ),
            main: roundedRect(
                document.querySelector('[data-locked-result-detail="true"]'),
            ),
            scrollY: Math.round(window.scrollY),
        };
    });
}

function expectStableRect(
    after: RectSnapshot | null,
    before: RectSnapshot | null,
): void {
    expect(before).not.toBeNull();
    expect(after).not.toBeNull();
    expect(after).toEqual(before);
}

async function snapshotDashboardGeometry(
    page: Page,
): Promise<DashboardSnapshot> {
    return page.evaluate(() => {
        const roundedRect = (target: Element | null) => {
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
        const loginButton =
            Array.from(document.querySelectorAll("button")).find(
                (button) => button.textContent?.trim() === "Log in",
            ) ?? null;
        const resultsHeading =
            document.querySelector('[data-results-table-summary="true"] p') ??
            Array.from(document.querySelectorAll("h2")).find(
                (heading) =>
                    heading.textContent?.trim() === "Matching result sets",
            ) ??
            null;

        return {
            authBar: roundedRect(
                document.querySelector('[data-results-auth-bar="true"]'),
            ),
            headerActions: roundedRect(
                document.querySelector('[data-results-header-actions="true"]'),
            ),
            loginButton: roundedRect(loginButton),
            loginForm: roundedRect(
                document.querySelector('form[aria-label="Log in"]'),
            ),
            main: roundedRect(document.querySelector("main")),
            resultsHeading: roundedRect(resultsHeading),
            searchBuilder: roundedRect(
                document.querySelector('[data-search-builder="true"]'),
            ),
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

test("login popup keeps visible page elements stable in a short viewport", async ({
    context,
    page,
}) => {
    test.setTimeout(150_000);
    mkdirSync(evidenceDir, { recursive: true });
    await page.setViewportSize({ width: 390, height: 150 });
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
        page.getByRole("link", { name: "Back to dashboard" }),
    ).toBeVisible();
    await expect(
        page.locator('[data-locked-result-detail="true"]'),
    ).toBeVisible({ timeout: 30_000 });

    const before = await snapshotStablePageGeometry(page);

    await page.getByRole("button", { name: "Log in" }).click();
    await expect(page.getByRole("form", { name: "Log in" })).toBeVisible();

    const after = await snapshotStablePageGeometry(page);

    await page.screenshot({
        fullPage: true,
        path: shortViewportScreenshotPath,
    });
    writeFileSync(
        shortViewportEvidencePath,
        `${JSON.stringify(
            {
                after,
                before,
                detailUrl,
                screenshotPath: shortViewportScreenshotPath,
                shifts: {
                    authBarTop:
                        after.authBar && before.authBar
                            ? after.authBar.top - before.authBar.top
                            : null,
                    backLinkTop:
                        after.backLink && before.backLink
                            ? after.backLink.top - before.backLink.top
                            : null,
                    headerActionsTop:
                        after.headerActions && before.headerActions
                            ? after.headerActions.top - before.headerActions.top
                            : null,
                    loginButtonTop:
                        after.loginButton && before.loginButton
                            ? after.loginButton.top - before.loginButton.top
                            : null,
                    mainTop:
                        after.main && before.main
                            ? after.main.top - before.main.top
                            : null,
                    scrollY: after.scrollY - before.scrollY,
                },
            },
            null,
            2,
        )}\n`,
    );

    expectStableRect(after.authBar, before.authBar);
    expectStableRect(after.headerActions, before.headerActions);
    expectStableRect(after.backLink, before.backLink);
    expectStableRect(after.loginButton, before.loginButton);
    expectStableRect(after.main, before.main);
    expect(after.loginForm).not.toBeNull();
    expect(after.scrollY).toBe(before.scrollY);
});

test("login popup overlays the logged-out dashboard without shifting the search builder", async ({
    context,
    page,
}) => {
    test.setTimeout(150_000);
    mkdirSync(evidenceDir, { recursive: true });
    await page.setViewportSize({ width: 1280, height: 900 });
    await context.clearCookies();
    await page.goto("/");

    await expect(page.getByRole("button", { name: "Log in" })).toBeVisible();
    await expect(page.locator('[data-search-builder="true"]')).toBeVisible({
        timeout: 30_000,
    });
    await expect(page.getByText("Latest result sets")).toBeVisible();

    const before = await snapshotDashboardGeometry(page);

    await page.getByRole("button", { name: "Log in" }).click();
    await expect(page.getByRole("form", { name: "Log in" })).toBeVisible();

    const after = await snapshotDashboardGeometry(page);

    await page.screenshot({ fullPage: true, path: dashboardScreenshotPath });
    writeFileSync(
        dashboardEvidencePath,
        `${JSON.stringify(
            {
                after,
                before,
                screenshotPath: dashboardScreenshotPath,
                shifts: {
                    authBarTop:
                        after.authBar && before.authBar
                            ? after.authBar.top - before.authBar.top
                            : null,
                    headerActionsTop:
                        after.headerActions && before.headerActions
                            ? after.headerActions.top - before.headerActions.top
                            : null,
                    loginButtonTop:
                        after.loginButton && before.loginButton
                            ? after.loginButton.top - before.loginButton.top
                            : null,
                    mainTop:
                        after.main && before.main
                            ? after.main.top - before.main.top
                            : null,
                    resultsHeadingTop:
                        after.resultsHeading && before.resultsHeading
                            ? after.resultsHeading.top -
                              before.resultsHeading.top
                            : null,
                    scrollY: after.scrollY - before.scrollY,
                    searchBuilderTop:
                        after.searchBuilder && before.searchBuilder
                            ? after.searchBuilder.top - before.searchBuilder.top
                            : null,
                },
            },
            null,
            2,
        )}\n`,
    );

    expectStableRect(after.authBar, before.authBar);
    expectStableRect(after.headerActions, before.headerActions);
    expectStableRect(after.loginButton, before.loginButton);
    expectStableRect(after.main, before.main);
    expectStableRect(after.searchBuilder, before.searchBuilder);
    expectStableRect(after.resultsHeading, before.resultsHeading);
    expect(after.loginForm).not.toBeNull();
    expect(after.scrollY).toBe(before.scrollY);
});
