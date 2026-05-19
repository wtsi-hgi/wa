// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("ResultMetadata", () => {
    it("renders integrated metadata as a dense strip instead of one card per key", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");
        const { container } = render(
            createElement(ResultMetadata, {
                metadata: {
                    library: "exon",
                    seqmeta_studyid: "6568",
                    seqmeta_library_id: "71046409",
                    study: "study-alpha",
                },
                variant: "integrated",
            }),
        );

        expect(screen.queryByText("Result metadata")).toBeNull();

        const strip = container.querySelector(
            '[data-result-metadata-strip="true"]',
        );
        const rows = Array.from(
            container.querySelectorAll<HTMLElement>("[data-metadata-row]"),
        );

        expect(strip).not.toBeNull();
        expect(rows).toHaveLength(2);
        expect(
            rows.map((row) => row.getAttribute("data-metadata-row")),
        ).toEqual(["seqmeta_studyid", "seqmeta_library_id"]);
        expect(rows[0]?.querySelector("dt")?.textContent).toBe("Study");
        expect(rows[1]?.querySelector("dt")?.textContent).toBe("Library");
        expect(container.textContent).not.toContain("seqmeta_studyid");
        expect(container.textContent).not.toContain("seqmeta_library_id");
        expect(container.textContent).not.toContain("libraryexon");

        for (const row of rows) {
            expect(row.className).toContain("inline-flex");
            expect(row.className).not.toContain("rounded-lg");
        }
    });

    it("falls back to three compact non-seqmeta entries when no seqmeta metadata exists", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");
        const { container } = render(
            createElement(ResultMetadata, {
                metadata: {
                    library: "exon",
                    study: "study-alpha",
                    project: "cancer",
                    owner: "alice",
                },
                variant: "integrated",
            }),
        );

        const rows = Array.from(
            container.querySelectorAll<HTMLElement>("[data-metadata-row]"),
        );

        expect(rows).toHaveLength(3);
        expect(
            rows.map((row) => row.getAttribute("data-metadata-row")),
        ).toEqual(["library", "study", "project"]);
        expect(container.textContent).toContain("+1");
        expect(container.textContent).not.toContain("owneralice");
    });

    it("keeps full plain metadata values available from the details popover", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");
        const longValue =
            "https://example.invalid/results/with/a/very/long/plain-metadata-value-that-needs-wrapping";
        const { container } = render(
            createElement(ResultMetadata, {
                metadata: {
                    output_uri: longValue,
                },
                variant: "integrated",
            }),
        );

        const stripValue = container.querySelector(
            '[data-metadata-row="output_uri"] span',
        );

        expect(stripValue?.className).toContain("truncate");

        await act(async () => {
            fireEvent.click(
                container.querySelector(
                    '[data-metadata-details-trigger="true"]',
                )!,
            );
        });

        await waitFor(() => {
            expect(
                document.querySelector(
                    '[data-metadata-detail-row="output_uri"]',
                ),
            ).not.toBeNull();
        });

        const detailRow = document.querySelector<HTMLElement>(
            '[data-metadata-detail-row="output_uri"]',
        );
        const detailKey = detailRow?.querySelector("dt");
        const detailValue = detailRow?.querySelector("span");

        expect(detailRow?.textContent).toContain(longValue);
        expect(detailKey?.textContent).toBe("output_uri");
        expect(detailKey?.className).toContain("break-all");
        expect(detailKey?.className).not.toContain("truncate");
        expect(detailValue?.className).toContain("break-words");
        expect(detailValue?.className).not.toContain("truncate");
    });
});
