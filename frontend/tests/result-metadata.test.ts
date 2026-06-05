// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import {
    fireEvent,
    render,
    screen,
    waitFor,
    within,
} from "@testing-library/react";
import { describe, expect, it } from "vitest";

function forceMetadataStripOverflowAfterThreeRows(): () => void {
    const originalClientHeight = Object.getOwnPropertyDescriptor(
        HTMLElement.prototype,
        "clientHeight",
    );
    const originalScrollHeight = Object.getOwnPropertyDescriptor(
        HTMLElement.prototype,
        "scrollHeight",
    );

    Object.defineProperty(HTMLElement.prototype, "clientHeight", {
        configurable: true,
        get() {
            if (
                this instanceof HTMLElement &&
                this.getAttribute("data-result-metadata-strip") === "true"
            ) {
                return 56;
            }

            return originalClientHeight?.get?.call(this) ?? 0;
        },
    });
    Object.defineProperty(HTMLElement.prototype, "scrollHeight", {
        configurable: true,
        get() {
            if (
                this instanceof HTMLElement &&
                this.getAttribute("data-result-metadata-strip") === "true"
            ) {
                const rowCount = this.querySelectorAll(
                    "[data-metadata-row]",
                ).length;

                return rowCount > 3 ? 96 : 56;
            }

            return originalScrollHeight?.get?.call(this) ?? 0;
        },
    });

    return () => {
        if (originalClientHeight) {
            Object.defineProperty(
                HTMLElement.prototype,
                "clientHeight",
                originalClientHeight,
            );
        } else {
            delete (HTMLElement.prototype as { clientHeight?: number })
                .clientHeight;
        }

        if (originalScrollHeight) {
            Object.defineProperty(
                HTMLElement.prototype,
                "scrollHeight",
                originalScrollHeight,
            );
        } else {
            delete (HTMLElement.prototype as { scrollHeight?: number })
                .scrollHeight;
        }
    };
}

describe("ResultMetadata", () => {
    it("shows all integrated metadata rows when the dense strip does not overflow", async () => {
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
        expect(rows).toHaveLength(4);
        expect(
            rows.map((row) => row.getAttribute("data-metadata-row")),
        ).toEqual([
            "library",
            "seqmeta_studyid",
            "seqmeta_library_id",
            "study",
        ]);
        expect(rows[1]?.querySelector("dt")?.textContent).toBe("Study");
        expect(rows[2]?.querySelector("dt")?.textContent).toBe("Library");
        expect(
            within(container).queryByRole("button", { name: "All metadata" }),
        ).toBeNull();
        expect(container.textContent).not.toContain("seqmeta_studyid");
        expect(container.textContent).not.toContain("seqmeta_library_id");
        expect(container.textContent).toContain("libraryexon");
        expect(container.textContent).toContain("studystudy-alpha");

        for (const row of rows) {
            expect(row.className).toContain("inline-flex");
            expect(row.className).not.toContain("rounded-lg");
        }
    });

    it("shows all compact non-seqmeta entries when the strip does not overflow", async () => {
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

        expect(rows).toHaveLength(4);
        expect(
            rows.map((row) => row.getAttribute("data-metadata-row")),
        ).toEqual(["library", "study", "project", "owner"]);
        expect(container.textContent).not.toContain("+1");
        expect(container.textContent).toContain("owneralice");
        expect(
            within(container).queryByRole("button", { name: "All metadata" }),
        ).toBeNull();
    });

    it("renders repeated metadata_values as comma-separated display values", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");
        const { container } = render(
            createElement(ResultMetadata, {
                metadata: {
                    foo: "bar",
                    sample: "Hek_R1",
                },
                metadataValues: {
                    foo: ["bar", "baz"],
                    sample: ["Hek_R1", "Hek_R2"],
                },
                variant: "section",
            }),
        );

        const fooRow = container.querySelector<HTMLElement>(
            '[data-metadata-row="foo"]',
        );
        const sampleRow = container.querySelector<HTMLElement>(
            '[data-metadata-row="sample"]',
        );

        expect(fooRow?.textContent).toContain("bar, baz");
        expect(sampleRow?.textContent).toContain("Hek_R1, Hek_R2");
    });

    it("uses seqmeta-prioritized compact rows only after measured overflow", async () => {
        const restoreOverflow = forceMetadataStripOverflowAfterThreeRows();

        try {
            const { ResultMetadata } =
                await import("@/components/result-metadata");
            const { container } = render(
                createElement(ResultMetadata, {
                    metadata: {
                        library: "exon",
                        seqmeta_studyid: "6568",
                        seqmeta_library_id: "71046409",
                        study: "study-alpha",
                        owner: "alice",
                    },
                    variant: "integrated",
                }),
            );

            await waitFor(() => {
                expect(
                    Array.from(
                        container.querySelectorAll<HTMLElement>(
                            "[data-metadata-row]",
                        ),
                    ).map((row) => row.getAttribute("data-metadata-row")),
                ).toEqual(["seqmeta_studyid", "seqmeta_library_id", "library"]);
            });

            expect(container.textContent).toContain("+2");
            expect(
                within(container).getByRole("button", {
                    name: "All metadata",
                }).textContent,
            ).toBe("all");
            expect(container.textContent).not.toContain("studystudy-alpha");
            expect(container.textContent).not.toContain("owneralice");
        } finally {
            restoreOverflow();
        }
    });

    it("keeps full plain metadata values available from the details popover", async () => {
        const restoreOverflow = forceMetadataStripOverflowAfterThreeRows();

        try {
            const { ResultMetadata } =
                await import("@/components/result-metadata");
            const longValue =
                "https://example.invalid/results/with/a/very/long/plain-metadata-value-that-needs-wrapping";
            const { container } = render(
                createElement(ResultMetadata, {
                    metadata: {
                        output_uri: longValue,
                        owner: "alice",
                        project: "cancer",
                        hidden_key: "hidden value",
                    },
                    variant: "integrated",
                }),
            );

            const stripValue = container.querySelector(
                '[data-metadata-row="output_uri"] span',
            );

            expect(stripValue?.className).toContain("truncate");

            await waitFor(() => {
                expect(
                    within(container).getByRole("button", {
                        name: "All metadata",
                    }),
                ).toBeTruthy();
            });

            await act(async () => {
                fireEvent.click(
                    within(container).getByRole("button", {
                        name: "All metadata",
                    }),
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
        } finally {
            restoreOverflow();
        }
    });
});
