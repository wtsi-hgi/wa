import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const screenshotPath = path.join(
    evidenceDir,
    "result-detail-header-postfix.png",
);
const domEvidencePath = path.join(
    evidenceDir,
    "result-detail-header-postfix-dom.json",
);

test("keeps result detail title and inline facts focused", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto("/");

    const resultLink = page
        .getByRole("link", { name: "nf-core/rnaseq" })
        .first();
    const href = await resultLink.getAttribute("href");
    const detailUrl = new URL(href ?? "/results/", page.url()).toString();

    await page.goto(detailUrl);
    await expect(page.getByRole("heading", { level: 1 })).toContainText(
        "nf-core/rnaseq",
        { timeout: 30000 },
    );

    const summary = page.locator('[data-result-detail-summary="true"]');
    const metadataStrip = summary.locator(
        '[data-result-metadata-strip="true"]',
    );

    await expect(summary).toBeVisible();
    await expect(
        summary.getByText("Result detail", { exact: true }),
    ).toHaveCount(0);
    await expect(
        summary.locator('[data-registration-field="Unique"]'),
    ).toHaveCount(0);
    await expect(
        summary.locator('[data-registration-field="Pipeline version"]'),
    ).toHaveCount(0);
    await expect(
        summary.locator('[data-registration-field="Last updated"]'),
    ).toBeVisible();
    await expect(
        metadataStrip.locator('[data-metadata-row="seqmeta_studyid"]'),
    ).toContainText("Study");
    await expect(
        metadataStrip.locator('[data-metadata-row="library"]'),
    ).toHaveCount(0);

    const evidence = await summary.evaluate((element) => {
        const text = (selector: string) =>
            Array.from(element.querySelectorAll(selector)).map(
                (node) => node.textContent?.replace(/\s+/g, " ").trim() ?? "",
            );
        const rect = element.getBoundingClientRect();
        const heading = element.querySelector("h1");
        const visibleRegistrationFields = Array.from(
            element.querySelectorAll("[data-registration-field]"),
        ).map((node) => ({
            label: node.getAttribute("data-registration-field"),
            text: node.textContent?.replace(/\s+/g, " ").trim() ?? "",
        }));
        const metadataRows = Array.from(
            element.querySelectorAll("[data-metadata-row]"),
        ).map((node) => ({
            key: node.getAttribute("data-metadata-row"),
            text: node.textContent?.replace(/\s+/g, " ").trim() ?? "",
        }));

        return {
            headingText: heading?.textContent?.replace(/\s+/g, " ").trim(),
            summaryHeight: rect.height,
            eyebrowTexts: text(
                "p.text-xs.font-semibold.uppercase, span.text-xs.font-semibold.uppercase",
            ),
            fileSummaryText:
                element
                    .querySelector("[data-file-summary]")
                    ?.textContent?.replace(/\s+/g, " ")
                    .trim() ?? "",
            visibleRegistrationFields,
            metadataRows,
        };
    });

    mkdirSync(evidenceDir, { recursive: true });
    await summary.screenshot({ path: screenshotPath });
    writeFileSync(domEvidencePath, `${JSON.stringify(evidence, null, 2)}\n`);

    expect(evidence.headingText).toContain("nf-core/rnaseq");
    expect(evidence.headingText).toContain("48522 / exon_lib");
    expect(
        evidence.visibleRegistrationFields.map((field) => field.label),
    ).toEqual(["Last updated", "Requester", "Operator"]);
    expect(evidence.fileSummaryText).toBe("");
    expect(evidence.eyebrowTexts).not.toContain("Result detail");
});
