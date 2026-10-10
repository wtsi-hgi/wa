#!/usr/bin/env node
// Drive agent feedback end to end against a stack started by launch.sh:
// POST /feedback as an MCP client would, read it back through the admin API,
// then log in through the frontend account menu, open /feedback from the
// menu, and acknowledge the report. Writes evidence into $RUN.
//
// Usage: drive-feedback.mjs <run-dir>   (run-dir is the one launch.sh printed)

import { createRequire } from "node:module";
import { readFileSync, writeFileSync } from "node:fs";
import { userInfo } from "node:os";
import path from "node:path";

const run = process.argv[2];
if (!run) {
    console.error("usage: drive-feedback.mjs <run-dir>");
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
const mlwh = env.MLWH_URL;
const frontend = env.FRONTEND_URL;
const adminToken = readFileSync(
    path.join(env.STATE_HOME, ".wa-mlwh-server.token"),
    "utf8",
).trim();
const ownerPassword = readFileSync(
    path.join(env.STATE_HOME, ".wa-results-server.token"),
    "utf8",
).trim();
const username = userInfo().username;
const marker = `verify-wa ${new Date().toISOString()}`;
const results = [];

function check(name, ok, detail) {
    results.push({ name, ok, detail });
    console.log(
        `${ok ? "PASS" : "FAIL"} ${name}${detail ? ` - ${detail}` : ""}`,
    );
}

function save(name, value) {
    writeFileSync(
        path.join(evidence, name),
        typeof value === "string"
            ? value
            : `${JSON.stringify(value, null, 2)}\n`,
    );
}

async function adminList(query = "") {
    const response = await fetch(`${mlwh}/feedback${query}`, {
        headers: { authorization: `Bearer ${adminToken}` },
    });

    return { status: response.status, body: await response.json() };
}

// 1. Submit like the MCP server: unauthenticated JSON POST to <root>/feedback.
const submission = {
    category: "could_not_answer",
    description: `${marker}: no endpoint returns per-lane yield for a study`,
    user_request: "How many gigabases did study 6568 produce per lane?",
    tools_tried: ["mlwh_info", "mlwh_runs"],
    mcp_server_version: "0.4.0",
    wa_api_version: "1.9.0",
    transport: "stdio",
    client_name: "verify-wa",
    client_version: "1.0.0",
};
const post = await fetch(`${mlwh}/feedback`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(submission),
});
const receipt = await post.json();
save("01-post-feedback.json", {
    request: submission,
    status: post.status,
    response: receipt,
});
check(
    "POST /feedback returns 201 with an id",
    post.status === 201 && Number.isInteger(receipt.id),
    `status ${post.status}`,
);

const id = receipt.id;

// 2. Side effect: the row is stored and readable by the admin API only.
const anonymous = await fetch(`${mlwh}/feedback`);
check(
    "GET /feedback without token is 401",
    anonymous.status === 401,
    `status ${anonymous.status}`,
);
const stored = await adminList();
save("02-admin-list-after-post.json", stored);
const row = stored.body.items?.find((item) => item.id === id);
check(
    "stored row matches submission",
    row?.description === submission.description &&
        row?.user_request === submission.user_request &&
        row?.tools_tried?.join(",") === submission.tools_tried.join(",") &&
        row?.acknowledged === false,
);

// 3. Browser: log in through the account menu as the server owner, then open
// the Feedback menu item.
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

try {
    await page.goto(frontend, { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Log in" }).click();
    const form = page.getByRole("form", { name: "Log in" });
    await form.locator('input[name="username"]').fill(username);
    await form.locator('input[name="password"]').fill(ownerPassword);
    await page.screenshot({ path: path.join(evidence, "03-login-form.png") });
    await form.getByRole("button", { name: "Continue" }).click();

    const account = page.getByRole("button", { name: `${username} account` });
    await account.waitFor({ timeout: 30_000 });
    await account.click();
    const feedbackLink = page.getByRole("menuitem", { name: "Feedback" });
    await feedbackLink.waitFor();
    await page.screenshot({ path: path.join(evidence, "04-account-menu.png") });
    check("account menu shows Feedback link for admin", true);
    await feedbackLink.click();
    await page.waitForURL("**/feedback**");

    await page.getByRole("heading", { name: "Agent feedback" }).waitFor();
    const article = page.getByRole("article", { name: `Feedback #${id}` });
    await article.waitFor();
    await page.screenshot({
        path: path.join(evidence, "05-feedback-page.png"),
        fullPage: true,
    });
    await article.screenshot({
        path: path.join(evidence, "06-feedback-report.png"),
    });
    const text = await article.innerText();
    save("06-feedback-report.txt", text);
    check(
        "admin page shows the submitted report",
        text.includes(marker) &&
            text.includes(submission.user_request) &&
            text.includes("mlwh_info, mlwh_runs"),
    );

    // 4. Acknowledge: it leaves the default (unacknowledged) view, reappears
    // with an Acknowledged badge under Show acknowledged, and is stored.
    await article.getByRole("button", { name: "Acknowledge" }).click();
    await article.waitFor({ state: "detached", timeout: 15_000 });
    // Controlled checkbox: it flips only after the router navigates, so
    // click and wait for the URL rather than using check().
    await page.getByLabel("Show acknowledged").click();
    await page.waitForURL("**show=all**");
    const acknowledged = page.getByRole("article", { name: `Feedback #${id}` });
    await acknowledged.getByText("Acknowledged", { exact: true }).waitFor();
    await acknowledged.screenshot({
        path: path.join(evidence, "07-acknowledged-report.png"),
    });
    const after = await adminList("?acknowledged=true");
    save("08-admin-list-after-ack.json", after);
    const ackRow = after.body.items?.find((item) => item.id === id);
    check(
        "acknowledge is stored",
        ackRow?.acknowledged === true && ackRow?.acknowledged_at !== "",
    );
} catch (error) {
    await page
        .screenshot({
            path: path.join(evidence, "99-failure.png"),
            fullPage: true,
        })
        .catch(() => {});
    check("browser drive", false, String(error));
} finally {
    await browser.close();
}

save("feedback-summary.json", { id, marker, results });
const failed = results.filter((result) => !result.ok).length;
console.log(failed === 0 ? `ALL PASS (feedback #${id})` : `${failed} FAILED`);
process.exit(failed === 0 ? 0 : 1);
