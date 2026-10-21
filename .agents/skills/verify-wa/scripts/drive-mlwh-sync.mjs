#!/usr/bin/env node
// Drive the MLWH sync admin page: log in as the server owner (always an MLWH
// admin), open "MLWH sync" from the account menu, expand the study table's
// last error, and save light and dark screenshots to <run-dir>/evidence. Then
// at a 390px phone width, check that no row needs horizontal scrolling and
// that the table keeps its header and row header roles.
// Prints the menu items, banner text, row count and narrow-width geometry,
// then PASS or FAIL.
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

    await page.evaluate(() =>
        document.documentElement.classList.remove("dark"),
    );
    await page.setViewportSize({ width: 390, height: 844 });
    const narrow = await page.evaluate(() => {
        const width = window.innerWidth;
        const table = document.querySelector("table");
        const wrapper = table?.parentElement;
        const cells = [...document.querySelectorAll("tbody td")];

        return {
            width,
            pageOverflow: document.documentElement.scrollWidth - width,
            tableOverflow: wrapper
                ? wrapper.scrollWidth - wrapper.clientWidth
                : -1,
            cellsPastEdge: cells.filter(
                (cell) => cell.getBoundingClientRect().right > width + 0.5,
            ).length,
        };
    });
    const narrowHeaders = await page.getByRole("columnheader").allInnerTexts();
    const narrowRowHeaders = await page.getByRole("rowheader").count();
    console.log(
        `narrow: ${JSON.stringify(narrow)} columnheaders=${narrowHeaders.length} rowheaders=${narrowRowHeaders}`,
    );
    await page.screenshot({
        path: path.join(evidence, "mlwh-sync-04-narrow.png"),
        fullPage: true,
    });
    await study.screenshot({
        path: path.join(evidence, "mlwh-sync-05-narrow-study.png"),
    });
    const narrowOK =
        narrow.pageOverflow <= 0 &&
        narrow.tableOverflow === 0 &&
        narrow.cellsPastEdge === 0 &&
        narrowHeaders.length === 4 &&
        narrowRowHeaders === rows;

    ok = rows > 0 && expanded && narrowOK;
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
