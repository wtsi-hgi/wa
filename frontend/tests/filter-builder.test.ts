/**
 * @vitest-environment jsdom
 */

import { createElement } from "react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
} from "@testing-library/react";
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
const fetchMock = vi.fn();

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
    vi.stubGlobal("fetch", fetchMock);
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
});

afterAll(() => {
    vi.unstubAllGlobals();
});

describe("K1 filter builder component", () => {
    afterEach(() => {
        cleanup();
        fetchMock.mockReset();
        pushMock.mockReset();
    });

    it("matches the file browser title treatment and shows a generic search entry", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        const { container } = render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: [],
                seqmetaAvailable: true,
                studies: [],
            }),
        );

        const title = screen.getByText("Search");
        const titleRow = title.parentElement;
        const titleIcon = titleRow?.querySelector("svg");

        expect(titleRow?.className).toContain("flex");
        expect(titleRow?.className).toContain("items-center");
        expect(titleRow?.className).toContain("gap-3");
        expect(titleIcon).toBeTruthy();
        expect(titleIcon?.getAttribute("aria-hidden")).toBe("true");
        expect(titleIcon?.className.baseVal).toContain("size-4");
        expect(titleIcon?.className.baseVal).toContain("text-primary");
        expect(title.className).toContain("text-sm");
        expect(title.className).toContain("font-semibold");
        expect(title.className).toContain("uppercase");
        expect(title.className).toContain("tracking-[0.18em]");
        expect(title.className).toContain("text-muted-foreground");

        const genericInput = screen.getByLabelText(/generic all-field search/i);

        expect(genericInput).toBeTruthy();
        expect(genericInput.getAttribute("data-generic-search-input")).toBe(
            "true",
        );
        expect(
            screen.getByRole("button", {
                name: /add generic search match/i,
            }),
        ).toBeTruthy();
        expect(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-search-builder-permanent-fields="true"]',
            ),
        ).toBeNull();
        expect(screen.queryByLabelText(/pipeline name value/i)).toBeNull();
        expect(screen.queryByLabelText(/unique value/i)).toBeNull();
        expect(screen.queryByLabelText(/study value/i)).toBeNull();
        expect(screen.queryByLabelText(/sample value/i)).toBeNull();
        expect(screen.queryByLabelText(/requester value/i)).toBeNull();
    });

    it("pins common fields at the top of the specific field dropdown with suggestions", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        const { container } = render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: ["library", "sample_name"],
                seqmetaAvailable: true,
                studies: [],
                suggestionValues: {
                    pipeline_name: ["nf-core/rnaseq"],
                    run_key: ["48522 / random_exon"],
                    study: ["6568"],
                    sample: ["SMP1001"],
                    user: ["alice"],
                    meta_library: ["RNA"],
                },
            }),
        );

        expect(screen.getByText("Search")).toBeTruthy();
        expect(screen.queryByText("Search builder")).toBeNull();

        const pinnedInputs = [
            ["Pipeline name", "pipeline_name", "nf-core/rnaseq"],
            ["Unique", "run_key", "48522 / random_exon"],
            ["Study", "study", "6568"],
            ["Sample", "sample", "SMP1001"],
            ["Requester", "user", "alice"],
        ] as const;

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );

        const filterPopover = container.querySelector(
            "[data-search-builder-popover='true']",
        );

        expect(filterPopover).toBeTruthy();
        expect(screen.getByRole("option", { name: /^library$/i })).toBeTruthy();

        const optionLabels = Array.from(
            container.querySelectorAll("[data-filter-field-option]"),
        ).map((option) => option.textContent?.trim());

        expect(optionLabels.slice(0, 5)).toEqual([
            "Pipeline name",
            "Unique",
            "Study",
            "Sample",
            "Requester",
        ]);

        fireEvent.change(screen.getByPlaceholderText("Find a field"), {
            target: { value: "unique" },
        });

        expect(screen.getByRole("option", { name: /^unique$/i })).toBeTruthy();

        fireEvent.click(screen.getByRole("option", { name: /^unique$/i }));

        const valueInput = screen.getByLabelText(
            /unique value/i,
        ) as HTMLInputElement;

        expect(valueInput.getAttribute("list")).toBe(
            "filter-suggestions-run_key",
        );
        expect(
            container.querySelector(
                "datalist#filter-suggestions-run_key option[value='48522 / random_exon']",
            ),
        ).toBeTruthy();
        expect(pinnedInputs.map(([, key]) => key)).toEqual([
            "pipeline_name",
            "run_key",
            "study",
            "sample",
            "user",
        ]);
    });

    it("does not show suggested placeholders in empty generic or add-filter value inputs", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        const { container } = render(
            createElement(FilterBuilder, {
                currentFilters: {
                    pipeline_name: ["nf-core/rnaseq"],
                },
                metaKeys: ["library"],
                seqmetaAvailable: true,
                studies: [],
                suggestionValues: {
                    pipeline_name: ["nf-core/sarek"],
                    run_key: ["48522 / random_exon"],
                    study: ["6568"],
                    sample: ["SMP1001"],
                    user: ["alice"],
                    library: ["RNA"],
                },
            }),
        );

        const genericInput = screen.getByLabelText(
            /generic all-field search/i,
        ) as HTMLInputElement;

        expect(genericInput.value).toBe("");
        expect(genericInput.getAttribute("placeholder")).toBeNull();
        expect(genericInput.getAttribute("list")).toBeNull();

        expect(
            screen.getByRole("button", { name: /nf-core\/rnaseq/i }),
        ).toBeTruthy();

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(screen.getByRole("option", { name: /^library$/i }));

        const valueInput = screen.getByLabelText(
            /library value/i,
        ) as HTMLInputElement;

        expect(valueInput.value).toBe("");
        expect(valueInput.getAttribute("placeholder")).toBeNull();
        expect(valueInput.getAttribute("list")).toBe(
            "filter-suggestions-library",
        );
        expect(
            container.querySelector(
                "datalist#filter-suggestions-library option[value='RNA']",
            ),
        ).toBeTruthy();
    });

    it("updates the URL when a pinned specific field is added alongside an existing filter", async () => {
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

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(
            screen.getByRole("option", { name: /^pipeline name$/i }),
        );
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

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(screen.getByRole("option", { name: /^requester$/i }));
        fireEvent.change(screen.getByLabelText(/requester value/i), {
            target: { value: "bob" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?user=alice&user=bob");
    });

    it("uses the shared autocomplete input flow for combined Study filters", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        const { container } = render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: [],
                seqmetaAvailable: true,
                studies: [],
                suggestionValues: {
                    study: ["6568", "7777"],
                },
            }),
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(screen.getByRole("option", { name: /^study$/i }));

        const studyInput = await screen.findByLabelText(/study value/i);

        fireEvent.change(studyInput, {
            target: { value: "656" },
        });

        expect(studyInput.getAttribute("list")).toBe(
            "filter-suggestions-study",
        );
        expect(
            container.querySelector(
                "datalist#filter-suggestions-study option[value='6568']",
            ),
        ).toBeTruthy();

        fireEvent.change(studyInput, {
            target: { value: "6568" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?study=6568");
    });

    it("adds combined Sample filters and sends the logical sample key", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: [
                    "seqmeta_sampleid",
                    "seqmeta_sample_lims",
                    "sample_name",
                ],
                seqmetaAvailable: true,
                studies: [],
                suggestionValues: {
                    sample: ["SANG1001", "SMP1001", "SAMPLE-A"],
                },
            }),
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(screen.getByRole("option", { name: /^sample$/i }));
        fireEvent.change(screen.getByLabelText(/sample value/i), {
            target: { value: "SMP1001" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?sample=SMP1001");
    });

    it("adds combined Library filters and sends the logical library key", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: ["library", "seqmeta_library"],
                seqmetaAvailable: true,
                studies: [],
                suggestionValues: {
                    library: ["RNA", "WGS"],
                },
            }),
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(screen.getByRole("option", { name: /^library$/i }));
        fireEvent.change(screen.getByLabelText(/library value/i), {
            target: { value: "RNA" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?library=RNA");
    });

    it("labels the registration uniqueness filter as Unique and sends the existing run_key query key", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: [],
                seqmetaAvailable: false,
                studies: [],
                suggestionValues: {
                    run_key: ["48522 / random_exon"],
                },
            }),
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(screen.getByRole("option", { name: /^unique$/i }));
        fireEvent.change(screen.getByLabelText(/unique value/i), {
            target: { value: "48522 / random_exon" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith(
            "/?run_key=48522+%2F+random_exon",
        );
    });

    it("keeps library ID filters on their first-class seqmeta key", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: ["seqmeta_libraryid"],
                seqmetaAvailable: true,
                studies: [],
                suggestionValues: {
                    seqmeta_library_id: ["71046409"],
                },
            }),
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(screen.getByRole("option", { name: /^library id$/i }));
        fireEvent.change(screen.getByLabelText(/library id value/i), {
            target: { value: "71046409" },
        });
        fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

        expect(pushMock).toHaveBeenCalledWith("/?seqmeta_library_id=71046409");
    });

    it("shows library filter help warning about the first call and wa mlwh sync", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: ["library", "seqmeta_library"],
                seqmetaAvailable: true,
                studies: [],
            }),
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(screen.getByRole("option", { name: /^library$/i }));

        const panel = screen.getByTestId("library-filter-help");

        expect(panel.textContent).toContain("first call");
        expect(panel.textContent).toContain("wa mlwh sync");
        expect(panel.textContent).not.toContain("Sa" + "ga");
        expect(panel.textContent).not.toContain("via " + "Sa" + "ga");
    });

    it("documents library search expansion with first-call and wa mlwh sync wording in the helper JSDoc", () => {
        const source = readFileSync(
            resolve(process.cwd(), "app/(results)/actions.ts"),
            "utf8",
        );

        expect(source).toMatch(
            /\/\*\*[\s\S]*first call[\s\S]*wa mlwh sync[\s\S]*\*\/\s*export async function searchResults/,
        );
        expect(source).not.toContain("Sa" + "ga");
        expect(source).not.toContain("via " + "Sa" + "ga");
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

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );
        fireEvent.click(
            screen.getByRole("option", { name: /^pipeline name$/i }),
        );

        const popover = container.querySelector(
            "[data-search-builder-popover='true']",
        );
        const fieldList = container.querySelector(
            "[data-search-builder-field-list='true']",
        );
        const valueInput = screen.getByLabelText(/pipeline name value/i);

        fireEvent.change(valueInput, {
            target: { value: "rna" },
        });

        expect(popover).toBeTruthy();
        expect(fieldList).toBeTruthy();
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

    it("shows friendly specific field names in the add filter dropdown", async () => {
        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: ["library"],
                seqmetaAvailable: false,
                studies: [],
            }),
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: /add specific field to filter/i,
            }),
        );

        const libraryOption = screen.getByRole("option", {
            name: /^library$/i,
        });

        expect(libraryOption.textContent?.trim()).toBe("Library");
        expect(
            screen.getByRole("option", { name: /pipeline name/i }),
        ).toBeTruthy();
        expect(screen.queryByText("pipeline_name")).toBeNull();
    });

    it("fetches generic all-field suggestions, labels their field type, and adds the selected filter", async () => {
        fetchMock.mockResolvedValue({
            json: async () => [
                {
                    field_key: "meta_assay_tag",
                    value: "alpha-needle-260618-omega",
                },
                {
                    field_key: "user",
                    value: "requester-needle-260618",
                },
            ],
            ok: true,
        });

        const { FilterBuilder } = await import("@/components/filter-builder");

        render(
            createElement(FilterBuilder, {
                currentFilters: {},
                metaKeys: ["assay_tag"],
                seqmetaAvailable: false,
                studies: [],
            }),
        );

        fireEvent.change(screen.getByLabelText(/generic all-field search/i), {
            target: { value: "needle-260618" },
        });

        await waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                "/api/results/search-suggestions?q=needle-260618",
                expect.objectContaining({ signal: expect.any(AbortSignal) }),
            );
        });

        const assayOption = await screen.findByRole("option", {
            name: /add assay tag filter alpha-needle-260618-omega/i,
        });

        expect(assayOption.textContent).toContain("Assay Tag");
        expect(assayOption.textContent).toContain("alpha-needle-260618-omega");
        expect(
            screen.getByRole("option", {
                name: /add requester filter requester-needle-260618/i,
            }).textContent,
        ).toContain("Requester");

        fireEvent.click(assayOption);

        expect(pushMock).toHaveBeenCalledWith(
            "/?meta_assay_tag=alpha-needle-260618-omega",
        );
    });
});
