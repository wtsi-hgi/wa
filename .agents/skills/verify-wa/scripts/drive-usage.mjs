// Usage: node drive-usage.mjs <RUN>. Run after making the traffic in
// features/usage-admin-page.md, including MCP requests from a claude-code
// agent and a caller 400 on GET /studies. Writes usagedash-*.png and
// usagedash-drive.json to <RUN>/evidence and exits 1 when a check fails.
//
// Drives the /usage page on a verify-wa stack: login through the
// account menu, the 24h window, KPIs, Needs attention, the chart legend,
// click-to-filter, deep links, layout overflow, screenshots light and dark,
// and axe (the repo's transitive axe-core, injected) on the page with every
// disclosure closed and open.
import { createRequire } from "node:module";
import { readFileSync, writeFileSync } from "node:fs";
import { userInfo } from "node:os";
import path from "node:path";

const run = process.argv[2];
const env = Object.fromEntries(
    readFileSync(path.join(run, "stack.env"), "utf8")
        .split("\n")
        .filter(Boolean)
        .map((l) => [l.slice(0, l.indexOf("=")), l.slice(l.indexOf("=") + 1)]),
);
const require = createRequire(path.join(env.REPO, "frontend", "package.json"));
const { chromium } = require("@playwright/test");
const evidence = path.join(run, "evidence");
const axePath = path.join(
    env.REPO,
    "frontend/node_modules/.pnpm/axe-core@4.11.3/node_modules/axe-core/axe.min.js",
);
const pw = readFileSync(
    path.join(env.STATE_HOME, ".wa-results-server.token"),
    "utf8",
).trim();
const out = { checks: {} };
const check = (name, ok, detail) => {
    out.checks[name] = { ok: Boolean(ok), detail };
};

const browser = await chromium.launch(
    process.env.WA_VERIFY_CHROMIUM
        ? { executablePath: process.env.WA_VERIFY_CHROMIUM }
        : {},
);

async function axe(page, label) {
    await page.addScriptTag({ path: axePath });
    const res = await page.evaluate(
        async () => await window.axe.run(document.querySelector("main")),
    );
    out[`axe ${label}`] = res.violations.map(
        (v) =>
            `${v.id} (${v.impact}): ${v.nodes.length} ${v.nodes
                .slice(0, 3)
                .map((n) => n.target.join(" "))
                .join(" | ")}`,
    );
    return res.violations.filter(
        (v) => v.impact === "serious" || v.impact === "critical",
    );
}

async function openAll(page) {
    await page.evaluate(() =>
        document.querySelectorAll("main details").forEach((d) => {
            d.open = true;
        }),
    );
}

for (const scheme of ["light", "dark"]) {
    const context = await browser.newContext({
        ignoreHTTPSErrors: true,
        viewport: { width: 1280, height: 900 },
        colorScheme: scheme,
    });
    const page = await context.newPage();
    await page.goto(env.FRONTEND_URL, { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Log in" }).click();
    const form = page.getByRole("form", { name: "Log in" });
    await form.locator('input[name="username"]').fill(userInfo().username);
    await form.locator('input[name="password"]').fill(pw);
    await form.getByRole("button", { name: "Continue" }).click();
    await page
        .getByRole("button", { name: `${userInfo().username} account` })
        .click();
    await page.getByRole("menuitem", { name: "Usage" }).click();
    await page.getByRole("heading", { name: "MLWH usage" }).waitFor();
    await page.getByRole("link", { name: "Last 24 hours" }).click();
    await page.waitForURL(/window=24h/);
    await page.getByRole("heading", { name: "Needs attention" }).waitFor();
    await page.waitForLoadState("networkidle");
    const isDark = await page.evaluate(() =>
        document.documentElement.classList.contains("dark"),
    );
    check(`${scheme}: theme applied`, isDark === (scheme === "dark"), isDark);

    if (scheme === "light") {
        out.kpis = await page.locator("dl").first().innerText();
        out.requests = Number(
            (
                await page
                    .getByTestId("kpi-Requests")
                    .getByTestId("kpi-value")
                    .innerText()
            ).replace(/,/g, ""),
        );
        out.attention = await page
            .getByRole("region", { name: "Needs attention" })
            .locator("li[id^=err-] > details > summary")
            .allInnerTexts();
        const kinds = out.attention.map((text) =>
            text.startsWith("Fault:") ? 0 : 1,
        );
        check(
            "faults first, then caller",
            kinds.length > 0 &&
                kinds.every((k, n) => n === 0 || k >= kinds[n - 1]),
            out.attention,
        );
        const legend = page.getByRole("group", { name: "Chart legend" });
        out.legend = await legend.getByRole("button").allInnerTexts();
        const cliSegments = () =>
            page
                .locator(
                    '[data-testid="activity-column"] rect[data-category="cli"]',
                )
                .count();
        const before = await cliSegments();
        await legend.getByRole("button", { name: "CLI" }).click();
        const after = await cliSegments();
        check("legend hides CLI", before > 0 && after === 0, { before, after });
        await legend.getByRole("button", { name: "CLI" }).click();
        await legend.getByRole("button", { name: "Expected misses" }).click();
        check(
            "expected markers shown",
            (await page.getByTestId("expected-marker").count()) > 0,
        );
        await legend.getByRole("button", { name: "Expected misses" }).click();
        out.chartLabel = await page
            .getByRole("region", { name: "Activity" })
            .getByRole("img")
            .getAttribute("aria-label");
        out.changes = await page
            .getByRole("region", { name: "What changed" })
            .innerText();
        const width = await page.evaluate(() => [
            document.documentElement.scrollWidth,
            document.documentElement.clientWidth,
        ]);
        check("no horizontal scroll at 1280", width[0] <= width[1], width);
    }

    await page.screenshot({
        path: path.join(evidence, `usagedash-${scheme}-page.png`),
        fullPage: true,
    });
    await page.getByRole("region", { name: "Needs attention" }).screenshot({
        path: path.join(evidence, `usagedash-${scheme}-attention.png`),
    });
    await page.locator("#activity").screenshot({
        path: path.join(evidence, `usagedash-${scheme}-activity.png`),
    });
    const serious = await axe(page, `${scheme} collapsed`);
    check(
        `${scheme}: axe collapsed has no serious/critical`,
        serious.length === 0,
        out[`axe ${scheme} collapsed`],
    );
    await openAll(page);
    await page.screenshot({
        path: path.join(evidence, `usagedash-${scheme}-expanded.png`),
        fullPage: true,
    });
    const seriousOpen = await axe(page, `${scheme} expanded`);
    check(
        `${scheme}: axe expanded has no serious/critical`,
        seriousOpen.length === 0,
        out[`axe ${scheme} expanded`],
    );

    if (scheme === "light") {
        // Click-to-filter: the agent product link narrows every panel.
        await page.goto(`${env.FRONTEND_URL}/usage?window=24h`, {
            waitUntil: "networkidle",
        });
        await page
            .getByRole("link", { name: "Filter to agent claude-code" })
            .click();
        await page.waitForURL(/agent=claude-code/);
        await page.getByRole("group", { name: "Filters" }).waitFor();
        out.filteredUrl = page.url();
        out.filteredKpis = await page.locator("dl").first().innerText();
        out.filterChips = await page
            .getByRole("group", { name: "Filters" })
            .innerText();
        const filtered = Number(
            (
                await page
                    .getByTestId("kpi-Requests")
                    .getByTestId("kpi-value")
                    .innerText()
            ).replace(/,/g, ""),
        );
        check(
            "agent filter narrows requests",
            filtered > 0 && filtered < out.requests,
            { filtered, all: out.requests },
        );
        out.filteredTools = await page
            .getByRole("region", { name: "MCP tools" })
            .innerText();
        await page.screenshot({
            path: path.join(evidence, "usagedash-light-filtered.png"),
            fullPage: true,
        });
        await page.getByRole("link", { name: /Last 7 days/ }).click();
        await page.waitForURL((url) => !url.search.includes("window="));
        check(
            "window change keeps the filter",
            !/window=/.test(page.url()) && /agent=claude-code/.test(page.url()),
            page.url(),
        );
        await page.getByRole("link", { name: /^Remove filter Agent/ }).click();
        await page.waitForURL((url) => !url.search.includes("agent"));
        check(
            "removing the chip clears the filter",
            !page.url().includes("agent"),
            page.url(),
        );

        // Deep link into a closed disclosure.
        const target = "err-caller-400-bad_request-GET-studies";
        await page.goto(`${env.FRONTEND_URL}/usage?window=24h#${target}`, {
            waitUntil: "networkidle",
        });
        await page.waitForTimeout(500);
        const opened = await page.evaluate((id) => {
            const el = document.getElementById(id);
            return el ? (el.closest("details")?.open ?? null) : "missing";
        }, target);
        check("#err- deep link opens its group", opened === true, opened);

        // Narrow screen: must not scroll sideways.
        await page.setViewportSize({ width: 390, height: 900 });
        await page.goto(`${env.FRONTEND_URL}/usage?window=24h`, {
            waitUntil: "networkidle",
        });
        const narrow = await page.evaluate(() => [
            document.documentElement.scrollWidth,
            document.documentElement.clientWidth,
        ]);
        check("no horizontal scroll at 390", narrow[0] <= narrow[1], narrow);
    }

    await context.close();
}

writeFileSync(
    path.join(evidence, "usagedash-drive.json"),
    JSON.stringify(out, null, 2),
);
console.log(JSON.stringify(out, null, 2));
await browser.close();
process.exit(Object.values(out.checks).every((c) => c.ok) ? 0 : 1);
