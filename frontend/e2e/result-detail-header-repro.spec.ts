import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const screenshotPath = path.join(
    evidenceDir,
    "result-detail-header-reopened-repro.png",
);
const domEvidencePath = path.join(
    evidenceDir,
    "result-detail-header-reopened-repro-dom.json",
);

test("reproduces reopened result detail header layout issues", async ({
    page,
}) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto("/");

    const resultLink = page
        .getByRole("link", { name: "nf-core/rnaseq" })
        .first();
    const href = await resultLink.getAttribute("href");
    const detailUrl = new URL(href ?? "/results/", page.url()).toString();
    const resultId = decodeURIComponent(
        new URL(detailUrl).pathname.split("/").filter(Boolean).at(-1) ?? "",
    );

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
        const rectOf = (selector: string) => {
            const target = element.querySelector(selector);

            if (!(target instanceof HTMLElement)) {
                return null;
            }

            const targetRect = target.getBoundingClientRect();

            return {
                height: targetRect.height,
                width: targetRect.width,
                x: targetRect.x,
                y: targetRect.y,
            };
        };
        const rect = element.getBoundingClientRect();
        const heading = element.querySelector("h1");
        const titleLine = heading?.parentElement;
        const copyChip = element.querySelector("[data-result-id-copy]");
        const registrationLayout = element.querySelector(
            '[data-registration-layout="integrated"]',
        );
        const metadataLayout = element.querySelector(
            '[data-result-metadata-layout="integrated"]',
        );
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
            titleLineText:
                titleLine?.textContent?.replace(/\s+/g, " ").trim() ?? "",
            titleLineRect: rectOf("h1"),
            titleFileSummaryRect: rectOf("[data-title-file-summary]"),
            summaryHeight: rect.height,
            eyebrowTexts: text(
                "p.text-xs.font-semibold.uppercase, span.text-xs.font-semibold.uppercase",
            ),
            titleCopyChip: copyChip
                ? {
                      ariaLabel: copyChip.getAttribute("aria-label"),
                      fullId: copyChip.getAttribute("data-result-id-copy"),
                      text:
                          copyChip.textContent?.replace(/\s+/g, " ").trim() ??
                          "",
                  }
                : null,
            fileSummaryText:
                element
                    .querySelector("[data-file-summary]")
                    ?.textContent?.replace(/\s+/g, " ")
                    .trim() ?? "",
            titleFileSummaryText:
                element
                    .querySelector("[data-title-file-summary]")
                    ?.textContent?.replace(/\s+/g, " ")
                    .trim() ?? "",
            titleLineFileFacts:
                titleLine?.textContent
                    ?.match(/\d+\s+files?|\d+(?:\.\d+)?\s*(?:B|KB|MB|GB|TB)/gi)
                    ?.map((value) => value.trim()) ?? [],
            registrationLayoutText:
                registrationLayout?.textContent?.replace(/\s+/g, " ").trim() ??
                "",
            registrationHeaderLine:
                registrationLayout?.textContent?.replace(/\s+/g, " ").trim() ??
                "",
            registrationDetailsButtonText:
                registrationLayout
                    ?.querySelector("[data-registration-details-trigger]")
                    ?.textContent?.replace(/\s+/g, " ")
                    .trim() ?? "",
            metadataHeaderLine:
                metadataLayout?.firstElementChild?.textContent
                    ?.replace(/\s+/g, " ")
                    .trim() ?? "",
            metadataDetailsButtonText:
                metadataLayout
                    ?.querySelector("[data-metadata-details-trigger]")
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
    expect(evidence.headingText).not.toContain("result-");
    expect(evidence.titleCopyChip).toBeNull();
    expect(evidence.titleLineText).toContain("files");
    expect(evidence.titleLineText).toMatch(/\d+(?:\.\d+)?\s*(?:B|KB|MB|GB|TB)/);
    expect(evidence.titleFileSummaryText).toContain("files");
    expect(evidence.titleLineFileFacts.length).toBeGreaterThanOrEqual(2);
    expect(evidence.titleFileSummaryRect?.x ?? 0).toBeGreaterThan(
        evidence.titleLineRect?.x ?? 0,
    );
    expect(
        evidence.visibleRegistrationFields.map((field) => field.label),
    ).toEqual(["Last updated", "Requester", "Operator"]);
    expect(evidence.fileSummaryText).toBe("");
    expect(evidence.registrationLayoutText).toContain("Run details");
    expect(evidence.registrationLayoutText).toContain("All details");
    expect(evidence.registrationHeaderLine).toContain("Run details");
    expect(evidence.registrationHeaderLine).toContain("All details");
    expect(evidence.registrationHeaderLine).not.toContain("Last updated");
    expect(evidence.metadataHeaderLine).toContain("Metadata");
    expect(evidence.metadataHeaderLine).toContain("All metadata");
    expect(evidence.eyebrowTexts).not.toContain("Result detail");

    await summary.getByRole("button", { name: "All details" }).click();
    const resultIdDetail = page.locator(
        '[data-registration-detail-field="Result ID"]',
    );

    await expect(resultIdDetail).toBeVisible();
    await expect(resultIdDetail).toContainText(resultId);
    await expect(resultIdDetail.locator("[data-result-id-copy]")).toHaveCount(
        0,
    );
});
