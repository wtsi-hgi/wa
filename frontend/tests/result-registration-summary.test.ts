// @vitest-environment jsdom

import { createElement } from "react";
import { render, screen } from "@testing-library/react";
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
                    { label: "Run key", value: "runid=1001", mono: true },
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

        expect(compactLayout).toBeTruthy();
        expect(compactFields).toHaveLength(6);
        expect(wideFields).toHaveLength(2);
        expect(
            compactFields.map((field) =>
                field.getAttribute("data-registration-field"),
            ),
        ).toEqual([
            "Pipeline version",
            "Pipeline identifier",
            "Run key",
            "Requester",
            "Operator",
            "Registered",
        ]);

        for (const field of compactFields) {
            expect(field.className).toContain("rounded-lg");
            expect(field.className).toContain("bg-background/65");
        }

        for (const field of wideFields) {
            expect(field.className).toContain("rounded-lg");
            expect(field.className).toContain("bg-background/65");
        }
    });
});
