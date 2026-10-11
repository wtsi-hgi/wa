#!/usr/bin/env node
// Drive the account menu straight after a client-side login and assert the
// admin links (Feedback and Usage) are already there. Logs in as the server
// owner (always an MLWH admin), opens the account menu as soon as its button
// appears, and reads the menu items at once. The route refresh (RSC fetch) and the browser
// session refresh (POST /api/auth/refresh) that follow login are both held
// back for WA_VERIFY_REFRESH_DELAY_MS (default 2000), so the menu can only
// get its links from the login result itself. The login server action is not
// delayed.
//
// Usage: drive-menu-after-login.mjs <run-dir>   (run-dir from launch.sh)

import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { userInfo } from "node:os";
import path from "node:path";

const run = process.argv[2];
if (!run) {
    console.error("usage: drive-menu-after-login.mjs <run-dir>");
    process.exit(2);
}

const env = Object.fromEntries(
    readFileSync(path.join(run, "stack.env"), "utf8")
        .split("\n")
        .filter((line) => line.includes("="))
        .map((line) => [
            line.slice(0, line.indexOf("=")),
            line.slice(line.indexOf("=") + 1),
        ]),
);
const require = createRequire(path.join(env.REPO, "frontend", "package.json"));
const { chromium } = require("@playwright/test");

const evidence = path.join(run, "evidence");
const ownerPassword = readFileSync(
    path.join(env.STATE_HOME, ".wa-results-server.token"),
    "utf8",
).trim();
const username = userInfo().username;
const refreshDelay = Number(process.env.WA_VERIFY_REFRESH_DELAY_MS ?? 2000);

const browser = await chromium.launch(
    process.env.WA_VERIFY_CHROMIUM
        ? { executablePath: process.env.WA_VERIFY_CHROMIUM }
        : {},
);
const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width: 1280, height: 900 },
});
const page = await context.newPage();
let ok = false;

try {
    await page.goto(env.FRONTEND_URL, { waitUntil: "networkidle" });

    // Hold back RSC fetches (router.refresh) and the browser session refresh,
    // but not the login server action.
    await page.route("**/*", async (route) => {
        const request = route.request();
        const headers = request.headers();
        const isRouteRefresh = headers.rsc === "1" && !headers["next-action"];
        const isSessionRefresh =
            new URL(request.url()).pathname === "/api/auth/refresh";
        if (isRouteRefresh || isSessionRefresh) {
            await new Promise((resolve) => setTimeout(resolve, refreshDelay));
        }
        await route.continue();
    });

    await page.getByRole("button", { name: "Log in" }).click();
    const form = page.getByRole("form", { name: "Log in" });
    await form.locator('input[name="username"]').fill(username);
    await form.locator('input[name="password"]').fill(ownerPassword);
    await form.getByRole("button", { name: "Continue" }).click();

    const account = page.getByRole("button", { name: `${username} account` });
    await account.waitFor({ timeout: 30_000 });
    await account.click();
    await page.getByRole("menu").waitFor();
    const items = await page.getByRole("menuitem").allInnerTexts();
    const labels = items.map((text) => text.trim());
    await page.screenshot({
        path: path.join(evidence, "menu-after-login.png"),
    });
    console.log(`menu items right after login: ${JSON.stringify(labels)}`);
    ok = labels.includes("Feedback") && labels.includes("Usage");
    console.log(
        `${ok ? "PASS" : "FAIL"} account menu shows Feedback and Usage straight after login`,
    );
} catch (error) {
    await page
        .screenshot({
            path: path.join(evidence, "menu-after-login-failure.png"),
            fullPage: true,
        })
        .catch(() => {});
    console.log(`FAIL browser drive - ${String(error)}`);
} finally {
    await browser.close();
}

process.exit(ok ? 0 : 1);
