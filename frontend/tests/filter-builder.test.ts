/**
 * @vitest-environment jsdom
 */

import { createElement } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import {
    afterAll,
    afterEach,
    beforeAll,
    describe,
    expect,
    it,
    vi,
} from "vitest";

const pushMock = vi.fn();

vi.mock("next/navigation", () => ({
    usePathname: () => "/",
    useRouter: () => ({
        push: pushMock,
    }),
}));

beforeAll(() => {
    class ResizeObserverStub {
        observe() {}

        unobserve() {}

        disconnect() {}
    }

    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
});

afterAll(() => {
    vi.unstubAllGlobals();
});

describe("K1 filter builder component", () => {
    afterEach(() => {
        cleanup();
        pushMock.mockReset();
    });

    it("updates the URL when a new field is added alongside an existing filter", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {
                    user: ["alice"],
                },
                metaKeys: ["library"],
                seqmetaAvailable: false,
                studies: [],
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));
        fireEvent.click(screen.getByRole("option", { name: /pipeline name/i }));
        fireEvent.change(screen.getByLabelText(/pipeline name value/i), {
            target: { value: "nf" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?user=alice&pipeline_name=nf");
    });

    it("updates the URL when an active filter chip is removed", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {
                    user: ["alice"],
                    pipeline_name: ["nf"],
                },
                metaKeys: [],
                seqmetaAvailable: false,
                studies: [],
            }),
        );

        fireEvent.click(
            screen.getByRole("button", { name: /remove requester alice/i }),
        );

        expect(pushMock).toHaveBeenCalledWith("/?pipeline_name=nf");
    });

    it("adds a second value for the same field as a repeated OR query parameter", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {
                    user: ["alice"],
                },
                metaKeys: ["library", "seqmeta_sampleid"],
                seqmetaAvailable: true,
                studies: [],
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));
        fireEvent.click(screen.getByRole("option", { name: /^requester$/i }));
        fireEvent.change(screen.getByLabelText(/requester value/i), {
            target: { value: "bob" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?user=alice&user=bob");
    });

    it("uses the shared autocomplete input flow for study_id filters", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        const { container } = render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: [],
                seqmetaAvailable: true,
                studies: [],
                suggestionValues: {
                    study_id: ["6568", "7777"],
                },
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));
        fireEvent.click(screen.getByRole("option", { name: /study id/i }));

        const studyInput = await screen.findByLabelText(/study id value/i);

        fireEvent.change(studyInput, {
            target: { value: "656" },
        });

        expect(studyInput.getAttribute("list")).toBe(
            "filter-suggestions-study_id",
        );
        expect(
            container.querySelector(
                "datalist#filter-suggestions-study_id option[value='6568']",
            ),
        ).toBeTruthy();

        fireEvent.change(studyInput, {
            target: { value: "6568" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?study_id=6568");
    });

    it("shows cached suggestions for non-study fields and applies a selected value", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        const { container } = render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: ["library"],
                seqmetaAvailable: false,
                studies: [],
                suggestionValues: {
                    pipeline_name: ["nf-core/rnaseq", "nf-core/sarek"],
                    meta_library: ["RNA", "WGS"],
                },
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));
        fireEvent.click(screen.getByRole("option", { name: /pipeline name/i }));
        fireEvent.change(screen.getByLabelText(/pipeline name value/i), {
            target: { value: "rna" },
        });

        const popover = container.querySelector(
            "[data-search-builder-popover='true']",
        );
        const fieldList = container.querySelector(
            "[data-search-builder-field-list='true']",
        );
        const footerPanel = container.querySelector(
            "[data-search-builder-selected-field-panel='true']",
        );
        const valueInput = screen.getByLabelText(/pipeline name value/i);

        expect(popover).toBeTruthy();
        expect(fieldList).toBeTruthy();
        expect(footerPanel).toBeTruthy();
        expect(fieldList?.contains(valueInput)).toBe(false);
        expect(footerPanel?.contains(valueInput)).toBe(true);
        expect(
            screen.queryByRole("button", { name: /use nf-core\/rnaseq/i }),
        ).toBeNull();
        expect(valueInput.getAttribute("list")).toBe(
            "filter-suggestions-pipeline_name",
        );
        expect(
            container.querySelector(
                "datalist#filter-suggestions-pipeline_name option[value='nf-core/rnaseq']",
            ),
        ).toBeTruthy();

        fireEvent.change(valueInput, {
            target: { value: "nf-core/rnaseq" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith(
            "/?pipeline_name=nf-core%2Frnaseq",
        );
    });

    it("shows only friendly field names in the add filter dropdown", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: ["library"],
                seqmetaAvailable: false,
                studies: [],
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: /add filter/i }));

        const pipelineNameOption = screen.getByRole("option", {
            name: /pipeline name/i,
        });

        expect(pipelineNameOption.textContent?.trim()).toBe("Pipeline name");
        expect(screen.queryByText("pipeline_name")).toBeNull();
    });
});
