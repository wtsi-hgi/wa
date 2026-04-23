// @vitest-environment jsdom

import { createElement } from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("ResultRegistrationSummary", () => {
    it("renders registration details in a compact summary layout", async () => {
        const { ResultRegistrationSummary } = await import(
            "@/components/result-registration-summary"
        );

        const { container } = render(
            createElement(ResultRegistrationSummary, {
                fields: [
                    { label: "Result ID", value: "result-42", mono: true },
                    { label: "Pipeline name", value: "RNA pipeline" },
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
            }),
        );

        expect(screen.getByText("Registration")).toBeTruthy();
        expect(screen.getByText("Key details")).toBeTruthy();
        const compactLayout = container.querySelector(
            '[data-registration-layout="compact"]',
        );
        const compactFields = Array.from(
            container.querySelectorAll<HTMLElement>("[data-registration-field]"),
        );
        const wideFields = Array.from(
            container.querySelectorAll<HTMLElement>(
                "[data-registration-wide-field]",
            ),
        );

        expect(compactLayout).toBeTruthy();
        expect(compactLayout?.children).toHaveLength(3);
        expect(compactFields).toHaveLength(3);
        expect(wideFields).toHaveLength(2);

        for (const field of compactFields) {
            expect(field.className).toContain("border-b");
            expect(field.className).not.toContain("rounded-[1.25rem]");
            expect(field.className).not.toContain("bg-background/60");
        }

        for (const field of wideFields) {
            expect(field.className).toContain("rounded-[1.25rem]");
            expect(field.className).toContain("bg-background/60");
        }
    });
});
