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
                    assay: "exon",
                    seqmeta_studyid: "6568",
                    seqmeta_library_id: "71046409",
                    project: "study-alpha",
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
            "assay",
            "seqmeta_studyid",
            "seqmeta_library_id",
            "project",
        ]);
        expect(rows[1]?.querySelector("dt")?.textContent).toBe("Study");
        expect(rows[2]?.querySelector("dt")?.textContent).toBe("Library");
        expect(
            within(container).queryByRole("button", { name: "All metadata" }),
        ).toBeNull();
        expect(container.textContent).not.toContain("seqmeta_studyid");
        expect(container.textContent).not.toContain("seqmeta_library_id");
        expect(container.textContent).toContain("assayexon");
        expect(container.textContent).toContain("projectstudy-alpha");

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

    it("uses user-facing sample metadata for seqmeta details when canonical sample metadata is present", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");
        const { container } = render(
            createElement(ResultMetadata, {
                metadata: {
                    sample: "Hek_R1",
                    seqmeta_name: "7607STDY14643771",
                },
                enrichments: {
                    Hek_R1: {
                        identifier: "Hek_R1",
                        type: "sanger_sample_name",
                        graph: {},
                        partial: false,
                    },
                },
                variant: "section",
            }),
        );

        const sampleRow = container.querySelector<HTMLElement>(
            '[data-metadata-row="sample"]',
        );

        expect(sampleRow).toBeTruthy();
        expect(sampleRow?.querySelector("td")?.textContent).toBe("Sample");
        expect(
            container.querySelector('[data-metadata-row="seqmeta_name"]'),
        ).toBeNull();
        expect(sampleRow?.textContent).toContain("Hek_R1");
        expect(sampleRow?.textContent).not.toContain("7607STDY14643771");
        expect(
            within(sampleRow as HTMLElement).getByRole("button", {
                name: "Open Sample details",
            }),
        ).toBeTruthy();
    });

    it("keeps repeated user-facing sample values individually clickable while preserving repeated plain metadata", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");
        const { container } = render(
            createElement(ResultMetadata, {
                metadata: {
                    foo: "bar",
                    sample: "Hek_R1",
                    seqmeta_supplier_name: "Hek_R1",
                    seqmeta_name: "7607STDY14643771",
                },
                metadataValues: {
                    foo: ["bar", "baz"],
                    sample: ["Hek_R1", "Hek_R2"],
                    seqmeta_supplier_name: ["Hek_R1", "Hek_R2"],
                    seqmeta_name: ["7607STDY14643771", "7607STDY14643772"],
                },
                enrichments: {
                    Hek_R1: {
                        identifier: "Hek_R1",
                        type: "sanger_sample_name",
                        graph: {},
                        partial: false,
                    },
                    Hek_R2: {
                        identifier: "Hek_R2",
                        type: "sanger_sample_name",
                        graph: {},
                        partial: false,
                    },
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
        expect(sampleRow).toBeTruthy();
        expect(sampleRow?.querySelector("td")?.textContent).toBe("Sample");
        expect(
            container.querySelector('[data-metadata-row="seqmeta_name"]'),
        ).toBeNull();
        expect(
            within(sampleRow as HTMLElement)
                .getAllByTestId("mlwh-badge-label")
                .map((label) => label.textContent),
        ).toEqual(["Hek_R1", "Hek_R2"]);
        expect(
            within(sampleRow as HTMLElement).getAllByRole("button", {
                name: "Open Sample details",
            }),
        ).toHaveLength(2);
    });

    it("spaces repeated supplier-backed sample pills without literal comma separators", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");
        const { container } = render(
            createElement(ResultMetadata, {
                metadata: {
                    foo: "bar",
                    sample: "Hek_R1",
                    seqmeta_name: "7607STDY14643771",
                },
                metadataValues: {
                    foo: ["bar", "baz"],
                    sample: ["Hek_R1", "Hek_R2"],
                    seqmeta_name: ["7607STDY14643771", "7607STDY14643772"],
                },
                enrichments: {
                    Hek_R1: {
                        identifier: "Hek_R1",
                        type: "supplier_name",
                        graph: {
                            sample: {
                                id_study_lims: "7607",
                                id_sample_lims: "SMP7607-0000",
                                sanger_id: "7607STDY14643771",
                                sample_name: "7607STDY14643771",
                                supplier_name: "Hek_R1",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "Custom",
                                accession_number: "SAMEA76070",
                            },
                        },
                        partial: false,
                    },
                    Hek_R2: {
                        identifier: "Hek_R2",
                        type: "supplier_name",
                        graph: {
                            sample: {
                                id_study_lims: "7607",
                                id_sample_lims: "SMP7607-0001",
                                sanger_id: "7607STDY14643772",
                                sample_name: "7607STDY14643772",
                                supplier_name: "Hek_R2",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "Custom",
                                accession_number: "SAMEA76071",
                            },
                        },
                        partial: false,
                    },
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
        expect(sampleRow).toBeTruthy();
        expect(
            within(sampleRow as HTMLElement)
                .getAllByTestId("mlwh-badge-label")
                .map((label) => label.textContent),
        ).toEqual(["Hek_R1", "Hek_R2"]);
        expect(
            within(sampleRow as HTMLElement).getAllByRole("button", {
                name: "Open Sample details",
            }),
        ).toHaveLength(2);
        expect(within(sampleRow as HTMLElement).queryByText(",")).toBeNull();
        expect(sampleRow?.textContent).not.toContain("Hek_R1,Hek_R2");

        fireEvent.click(
            within(sampleRow as HTMLElement).getAllByRole("button", {
                name: "Open Sample details",
            })[0]!,
        );

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const dialogHeader = screen.getByText("Seqmeta details").closest("div");
        const titleLabels = Array.from(
            dialogHeader?.querySelectorAll("p") ?? [],
        ).map((label) => label.textContent);

        expect(titleLabels).toContain("seqmeta_supplier_name");
        expect(titleLabels).not.toContain("Sample");
        expect(
            screen
                .getByTestId("seqmeta-title-actions")
                .querySelector('[aria-label="Copy seqmeta_supplier_name"]'),
        ).toBeTruthy();
        expect(
            screen
                .getByTestId("seqmeta-title-actions")
                .querySelector(
                    '[aria-label="Send seqmeta_supplier_name to search filter"]',
                )
                ?.getAttribute("href"),
        ).toBe("/?sample=Hek_R1");

        const directMetadataSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="direct-metadata"]');
        expect(
            directMetadataSection?.querySelector(
                '[data-seqmeta-detail-key="seqmeta_supplier_name"]',
            ),
        ).toBeNull();
    });

    it("applies user-facing MLWH rendering to study, library, and run metadata with canonical companions", async () => {
        const { ResultMetadata } = await import("@/components/result-metadata");
        const { container } = render(
            createElement(ResultMetadata, {
                metadata: {
                    study: "Study alias",
                    seqmeta_id_study_lims: "7607",
                    library: "Library alias",
                    seqmeta_pipeline_id_lims: "Custom",
                    run: "Run alias",
                    seqmeta_id_run: "48522",
                },
                enrichments: {
                    "Study alias": {
                        identifier: "Study alias",
                        type: "study_id",
                        graph: {},
                        partial: false,
                    },
                    "Library alias": {
                        identifier: "Library alias",
                        type: "library_type",
                        graph: {},
                        partial: false,
                    },
                    "Run alias": {
                        identifier: "Run alias",
                        type: "run_id",
                        graph: {},
                        partial: false,
                    },
                },
                variant: "section",
            }),
        );

        const visibleKeys = Array.from(
            container.querySelectorAll<HTMLElement>("[data-metadata-row]"),
        ).map((row) => row.getAttribute("data-metadata-row"));

        expect(visibleKeys).toEqual(["study", "library", "run"]);

        for (const [key, label, triggerLabel] of [
            ["study", "Study", "seqmeta_id_study_lims"],
            ["library", "Library", "seqmeta_pipeline_id_lims"],
            ["run", "Run", "seqmeta_id_run"],
        ]) {
            const row = container.querySelector<HTMLElement>(
                `[data-metadata-row="${key}"]`,
            );

            expect(row?.querySelector("td")?.textContent).toBe(label);
            expect(
                within(row as HTMLElement).getByRole("button", {
                    name: `Open ${triggerLabel} details`,
                }),
            ).toBeTruthy();
        }
    });

    it("uses seqmeta-prioritized compact rows only after measured overflow", async () => {
        const restoreOverflow = forceMetadataStripOverflowAfterThreeRows();

        try {
            const { ResultMetadata } =
                await import("@/components/result-metadata");
            const { container } = render(
                createElement(ResultMetadata, {
                    metadata: {
                        assay: "exon",
                        seqmeta_studyid: "6568",
                        seqmeta_library_id: "71046409",
                        project: "study-alpha",
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
                ).toEqual(["seqmeta_studyid", "seqmeta_library_id", "assay"]);
            });

            expect(container.textContent).toContain("+2");
            expect(
                within(container).getByRole("button", {
                    name: "All metadata",
                }).textContent,
            ).toBe("all");
            expect(container.textContent).not.toContain("projectstudy-alpha");
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
