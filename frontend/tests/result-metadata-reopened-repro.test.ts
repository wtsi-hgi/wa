// @vitest-environment jsdom

import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { createElement } from "react";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const domEvidencePath = path.join(
    evidenceDir,
    "result-metadata-all-button-reopened-repro-dom.json",
);

describe("reopened result metadata layout", () => {
    it("hides All metadata when every metadata entry is already visible", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");
        const { container } = render(
            createElement(ResultMetadata, {
                metadata: {
                    seqmeta_studyid: "6568",
                },
                variant: "integrated",
            }),
        );

        const visibleRows = Array.from(
            container.querySelectorAll<HTMLElement>("[data-metadata-row]"),
        ).map((row) => ({
            key: row.getAttribute("data-metadata-row"),
            text: row.textContent?.replace(/\s+/g, " ").trim() ?? "",
        }));
        const allMetadataButton = container.querySelector<HTMLElement>(
            '[data-metadata-details-trigger="true"]',
        );
        const evidence = {
            allMetadataButtonText:
                allMetadataButton?.textContent?.replace(/\s+/g, " ").trim() ??
                "",
            metadataText:
                container.textContent?.replace(/\s+/g, " ").trim() ?? "",
            totalMetadataEntries: 1,
            visibleRows,
        };

        mkdirSync(evidenceDir, { recursive: true });
        writeFileSync(
            domEvidencePath,
            `${JSON.stringify(evidence, null, 2)}\n`,
        );

        expect(visibleRows).toHaveLength(1);
        expect(visibleRows[0]?.key).toBe("seqmeta_studyid");
        expect(allMetadataButton).toBeNull();
        expect(evidence.allMetadataButtonText).toBe("");
    });
});
