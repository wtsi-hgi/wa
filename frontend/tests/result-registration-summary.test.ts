// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("ResultRegistrationSummary", () => {
    it("renders registration details as an integrated compact grid without identity duplicates", async () => {
        const { ResultRegistrationSummary } =
            await import("@/components/result-registration-summary");

        const { container } = render(
            createElement(ResultRegistrationSummary, {
                fields: [
                    { label: "Pipeline version", value: "3.18.0" },
                    {
                        label: "Pipeline identifier",
                        value: "gh://repo/workflow.nf",
                        mono: true,
                    },
                    { label: "Unique", value: "1001", mono: true },
                    { label: "Requester", value: "alice" },
                    { label: "Operator", value: "bob" },
                    { label: "Registered", value: "23 Apr 2026, 09:15" },
                    {
                        label: "Output directory",
                        value: "/tmp/results/42",
                        mono: true,
                        wide: true,
                    },
                    {
                        label: "Command",
                        value: "nextflow run wf --input sample.tsv",
                        mono: true,
                        wide: true,
                    },
                ],
                variant: "integrated",
            }),
        );

        expect(screen.queryByText("Registration")).toBeNull();
        expect(screen.queryByText("Key details")).toBeNull();
        expect(screen.queryByText("Result ID")).toBeNull();
        expect(screen.queryByText("Pipeline name")).toBeNull();

        const compactLayout = container.querySelector(
            '[data-registration-layout="integrated"]',
        );
        const compactFields = Array.from(
            container.querySelectorAll<HTMLElement>(
                "[data-registration-field]",
            ),
        );
        const wideFields = Array.from(
            container.querySelectorAll<HTMLElement>(
                "[data-registration-wide-field]",
            ),
        );
        const detailsTrigger = screen.getByText("All details");

        expect(compactLayout).toBeTruthy();
        expect(compactFields).toHaveLength(4);
        expect(wideFields).toHaveLength(0);
        expect(
            compactFields.map((field) =>
                field.getAttribute("data-registration-field"),
            ),
        ).toEqual(["Pipeline version", "Unique", "Requester", "Operator"]);

        for (const field of compactFields) {
            expect(field.className).toContain("rounded-full");
            expect(field.className).toContain("min-h-8");
        }

        expect(detailsTrigger).toBeTruthy();
        expect(container.textContent).toContain("1001");
        expect(container.textContent).not.toContain("/tmp/results/42");

        await act(async () => {
            fireEvent.click(detailsTrigger);
        });

        await waitFor(() => {
            expect(
                document.querySelectorAll("[data-registration-detail-field]"),
            ).toHaveLength(8);
        });
        expect(
            document.querySelector(
                '[data-registration-detail-field="Output directory"]',
            )?.textContent,
        ).toContain("/tmp/results/42");
    });
});
