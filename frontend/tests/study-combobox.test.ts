/**
 * @vitest-environment jsdom
 */

import { createElement } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

describe("K2 study combobox", () => {
    afterEach(() => {
        cleanup();
    });

    it("shows matching studies when the user types a name substring", async () => {
        const { StudyCombobox } = await import("@/components/study-combobox");

        render(
            createElement(StudyCombobox, {
                onSelect: vi.fn(),
                studies: [
                    { id_study_lims: "6568", name: "RNA Seq" },
                    { id_study_lims: "7777", name: "Cancer Study" },
                ],
            }),
        );

        const input = screen.getByLabelText(/^study$/i);

        fireEvent.change(input, { target: { value: "RNA" } });

        expect(
            await screen.findByRole("button", { name: /rna seq/i }),
        ).not.toBeNull();
        expect(
            screen.queryByRole("button", { name: /cancer study/i }),
        ).toBeNull();
    });

    it("shows matching studies when the user types a study LIMS ID", async () => {
        const { StudyCombobox } = await import("@/components/study-combobox");

        render(
            createElement(StudyCombobox, {
                onSelect: vi.fn(),
                studies: [
                    { id_study_lims: "6568", name: "RNA Seq" },
                    { id_study_lims: "7777", name: "Cancer Study" },
                ],
            }),
        );

        const input = screen.getByLabelText(/^study$/i);

        fireEvent.change(input, { target: { value: "6568" } });

        expect(
            await screen.findByRole("button", { name: /rna seq/i }),
        ).not.toBeNull();
        expect(
            screen.queryByRole("button", { name: /cancer study/i }),
        ).toBeNull();
    });

    it("calls onSelect with the selected study id", async () => {
        const { StudyCombobox } = await import("@/components/study-combobox");
        const onSelect = vi.fn();

        render(
            createElement(StudyCombobox, {
                onSelect,
                studies: [{ id_study_lims: "6568", name: "RNA Seq" }],
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: /rna seq/i }));

        expect(onSelect).toHaveBeenCalledWith("6568");
    });

    it("shows a disabled unavailable placeholder when studies cannot be loaded", async () => {
        const { StudyCombobox } = await import("@/components/study-combobox");

        render(
            createElement(StudyCombobox, { onSelect: vi.fn(), studies: [] }),
        );

        const input = screen.getByPlaceholderText(/no studies available/i);

        expect(input).toHaveProperty("disabled", true);
    });

    it("shows all studies when the query is empty", async () => {
        const { StudyCombobox } = await import("@/components/study-combobox");

        render(
            createElement(StudyCombobox, {
                onSelect: vi.fn(),
                studies: [
                    { id_study_lims: "6568", name: "RNA Seq" },
                    { id_study_lims: "7777", name: "Cancer Study" },
                ],
            }),
        );

        const input = screen.getByLabelText(/^study$/i);

        fireEvent.change(input, { target: { value: "" } });

        expect(
            await screen.findByRole("button", { name: /rna seq/i }),
        ).not.toBeNull();
        expect(
            screen.getByRole("button", { name: /cancer study/i }),
        ).not.toBeNull();
    });
});
