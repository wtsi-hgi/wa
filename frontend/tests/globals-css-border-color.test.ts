import { readFile } from "node:fs/promises";
import path from "node:path";

import { chromium } from "@playwright/test";
import { compile } from "tailwindcss";
import { describe, expect, it } from "vitest";

import { resolveChromiumExecutablePath } from "@/lib/playwright-browser";

const frontendRoot = process.cwd();
const globalsCSSPath = path.join(frontendRoot, "app", "globals.css");
const tailwindIndexPath = path.join(
    frontendRoot,
    "node_modules",
    "tailwindcss",
    "index.css",
);

async function compileGlobalsCSS(candidates: string[]): Promise<string> {
    const compiler = await compile(await readFile(globalsCSSPath, "utf8"), {
        base: path.dirname(globalsCSSPath),
        loadStylesheet: async (id, base) => {
            if (id !== "tailwindcss") {
                throw new Error(`unexpected stylesheet import ${id}`);
            }

            return {
                path: tailwindIndexPath,
                base,
                content: await readFile(tailwindIndexPath, "utf8"),
            };
        },
    });

    return compiler.build(candidates);
}

describe("globals.css border colours", () => {
    it(
        "lets border colour utilities override the default border colour in a real render",
        { timeout: 60000 },
        async () => {
            const css = await compileGlobalsCSS([
                "border",
                "border-destructive",
                "border-primary",
            ]);
            const browser = await chromium.launch({
                executablePath: resolveChromiumExecutablePath(),
            });

            try {
                const page = await browser.newPage();
                await page.setContent(`<!doctype html>
<html><head><style>${css}</style></head><body>
<div id="plain" class="border"></div>
<div id="destructive" class="border border-destructive"></div>
<div id="primary" class="border border-primary"></div>
<div id="ref-border" style="color: var(--border)"></div>
<div id="ref-destructive" style="color: var(--destructive)"></div>
<div id="ref-primary" style="color: var(--primary)"></div>
</body></html>`);

                const colours = await page.evaluate(() => {
                    const style = (id: string) =>
                        getComputedStyle(document.getElementById(id)!);

                    return {
                        plain: style("plain").borderTopColor,
                        destructive: style("destructive").borderTopColor,
                        primary: style("primary").borderTopColor,
                        refBorder: style("ref-border").color,
                        refDestructive: style("ref-destructive").color,
                        refPrimary: style("ref-primary").color,
                    };
                });

                expect(colours.plain).toBe(colours.refBorder);
                expect(colours.destructive).toBe(colours.refDestructive);
                expect(colours.primary).toBe(colours.refPrimary);
                expect(colours.destructive).not.toBe(colours.refBorder);
            } finally {
                await browser.close();
            }
        },
    );
});
