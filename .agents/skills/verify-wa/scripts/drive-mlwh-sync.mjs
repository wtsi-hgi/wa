#!/usr/bin/env node
// Drive the MLWH sync admin page: log in as the server owner (always an MLWH
// admin), open "MLWH sync" from the account menu, expand the study table's
// last error, and save light and dark screenshots to <run-dir>/evidence.
// Prints the menu items, banner text and row count, then PASS or FAIL.
//
// Usage: drive-mlwh-sync.mjs <run-dir>   (run-dir from launch.sh)

import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { userInfo } from "node:os";
import path from "node:path";

import { readStackEnv } from "./stack-env.mjs";

const run = process.argv[2];
if (!run) {
    console.error("usage: drive-mlwh-sync.mjs <run-dir>");
    process.exit(2);
}

const env = readStackEnv(run);
const require = createRequire(path.join(env.REPO, "frontend", "package.json"));
const { chromium } = require("@playwright/test");

const evidence = path.join(run, "evidence");
const ownerPassword = readFileSync(
    path.join(env.STATE_HOME, ".wa-results-server.token"),
    "utf8",
).trim();
const username = userInfo().username;

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
    await page.getByRole("button", { name: "Log in" }).click();
    const form = page.getByRole("form", { name: "Log in" });
    await form.locator('input[name="username"]').fill(username);
    await form.locator('input[name="password"]').fill(ownerPassword);
    await form.getByRole("button", { name: "Continue" }).click();

    const account = page.getByRole("button", { name: `${username} account` });
    await account.waitFor({ timeout: 30_000 });
    await account.click();
    const item = page.getByRole("menuitem", { name: "MLWH sync" });
    await item.waitFor();
    const items = await page.getByRole("menuitem").allInnerTexts();
    console.log(`menu items: ${JSON.stringify(items.map((t) => t.trim()))}`);
    await item.click();

    await page
        .getByRole("heading", { level: 1, name: "MLWH sync" })
        .waitFor({ timeout: 60_000 });
    const banner = page.getByRole("alert").or(page.getByRole("status"));
    console.log(
        `banner: ${(await banner.first().innerText()).replace(/\s+/g, " ")}`,
    );
    const rows = await page.locator("tbody tr").count();
    console.log(`table rows: ${rows}`);
    await page.screenshot({
        path: path.join(evidence, "mlwh-sync-01-page.png"),
        fullPage: true,
    });

    const study = page.locator("tbody tr").filter({
        has: page.getByRole("rowheader", { name: "study", exact: true }),
    });
    let expanded = true;
    if ((await study.locator("summary").count()) > 0) {
        await study.locator("summary").click();
        expanded = await study.locator("details").evaluate((el) => el.open);
        await study.screenshot({
            path: path.join(evidence, "mlwh-sync-02-study-error.png"),
        });
    }
    console.log(`study error expanded: ${expanded}`);

    await page.evaluate(() => document.documentElement.classList.add("dark"));
    await page.screenshot({
        path: path.join(evidence, "mlwh-sync-03-dark.png"),
        fullPage: true,
    });
    ok = rows > 0 && expanded;
} catch (error) {
    await page
        .screenshot({
            path: path.join(evidence, "mlwh-sync-failure.png"),
            fullPage: true,
        })
        .catch(() => {});
    console.log(`FAIL browser drive - ${String(error)}`);
} finally {
    await browser.close();
}

console.log(ok ? "PASS" : "FAIL");
process.exit(ok ? 0 : 1);
