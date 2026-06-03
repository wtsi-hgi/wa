"use client";

import { useState, type FormEvent } from "react";
import { Check, Plus, Search, X } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";

import {
    boxPanelInsetClass,
    boxPanelRadiusClass,
    boxTitleActionClass,
    boxTitleIconClass,
    boxTitleRowClass,
    boxTitleSectionClass,
    boxTitleTextClass,
} from "@/components/box-title-section";
import {
    Command,
    CommandEmpty,
    CommandGroup,
    CommandInput,
    CommandItem,
    CommandList,
} from "@/components/ui/command";
import { buildSearchQuery, type SearchFilters } from "@/lib/search-params";
import { cn } from "@/lib/utils";
import type { Study } from "@/lib/contracts";

type FilterBuilderProps = {
    metaKeys: string[];
    seqmetaAvailable: boolean;
    studies: Study[];
    currentFilters: Record<string, string[]>;
    suggestionValues?: FilterSuggestionMap;
};

export type FilterSuggestionMap = Record<string, string[]>;

type FieldOption = {
    key: string;
    label: string;
    placeholder: string;
};

const combinedStudyMetaKeys = new Set([
    "study",
    "study_id",
    "seqmeta_id_study_lims",
    "seqmeta_studyid",
    "seqmeta_study_accession",
]);

const combinedSampleMetaKeys = new Set([
    "sample",
    "sample_id",
    "sample_name",
    "sample_accession_number",
    "seqmeta_id_sample_lims",
    "seqmeta_name",
    "seqmeta_sample_name",
    "seqmeta_sanger_sample_id",
    "seqmeta_sampleid",
    "seqmeta_sample_lims",
]);

const combinedLibraryMetaKeys = new Set([
    "library",
    "seqmeta_id_library_lims",
    "seqmeta_library_id",
    "seqmeta_library",
    "seqmeta_libraryid",
    "seqmeta_library_lims",
    "seqmeta_librarytype",
    "seqmeta_pipeline_id_lims",
]);

const permanentFieldOptions: FieldOption[] = [
    {
        key: "pipeline_name",
        label: "Pipeline name",
        placeholder: "nf-core/rnaseq",
    },
    { key: "run_key", label: "Unique", placeholder: "48522 or 48522 / exon" },
    { key: "study", label: "Study", placeholder: "6568 or ERP012345" },
    { key: "sample", label: "Sample", placeholder: "SANG001 or SMP001" },
    { key: "user", label: "Requester", placeholder: "alice" },
];

const permanentFieldKeys = new Set(
    permanentFieldOptions.map((option) => option.key),
);

const coreFieldOptions: FieldOption[] = [
    ...permanentFieldOptions,
    { key: "operator", label: "Operator", placeholder: "operator-1" },
    { key: "library", label: "Library", placeholder: "RNA or WGS" },
    { key: "seqmeta_library_id", label: "Library ID", placeholder: "71046409" },
    {
        key: "seqmeta_id_library_lims",
        label: "Library LIMS ID",
        placeholder: "SQPP-47463-G:B1",
    },
    {
        key: "pipeline_version",
        label: "Pipeline version",
        placeholder: "3.18.0",
    },
    {
        key: "pipeline_identifier",
        label: "Pipeline identifier",
        placeholder: "gh://repo/workflow.nf",
    },
    {
        key: "output_dir_prefix",
        label: "Output directory prefix",
        placeholder: "/lustre/scratch/project-a",
    },
];

function toTitleCase(value: string): string {
    return value
        .split(/[_\s]+/)
        .filter(Boolean)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(" ");
}

function toMetaQueryKey(metaKey: string): string {
    return metaKey.startsWith("seqmeta_") ? metaKey : `meta_${metaKey}`;
}

function getFieldOptions(
    metaKeys: string[],
    _seqmetaAvailable: boolean,
): FieldOption[] {
    const options = [...coreFieldOptions];

    const dynamicOptions = metaKeys
        .filter(
            (metaKey) =>
                !combinedStudyMetaKeys.has(metaKey) &&
                !combinedLibraryMetaKeys.has(metaKey) &&
                !combinedSampleMetaKeys.has(metaKey),
        )
        .map((metaKey) => ({
            key: toMetaQueryKey(metaKey),
            label: toTitleCase(metaKey),
            placeholder:
                metaKey === "seqmeta_name" ||
                metaKey === "seqmeta_sample_name" ||
                metaKey === "seqmeta_sampleid"
                    ? "SANG001"
                    : "value",
        }))
        .filter(
            (option, index, entries) =>
                entries.findIndex((entry) => entry.key === option.key) ===
                index,
        );

    return [...options, ...dynamicOptions];
}

function getFieldLabel(fieldOptions: FieldOption[], key: string): string {
    return (
        fieldOptions.find((option) => option.key === key)?.label ??
        toTitleCase(key.replace(/^meta_/, ""))
    );
}

function getAdditionalFieldOptions(fieldOptions: FieldOption[]): FieldOption[] {
    return fieldOptions.filter((option) => !permanentFieldKeys.has(option.key));
}

function createNextFilters(
    currentFilters: SearchFilters,
    key: string,
    value: string,
): SearchFilters {
    const trimmedValue = value.trim();
    if (!trimmedValue) {
        return currentFilters;
    }

    const nextValues = [...(currentFilters[key] ?? [])];
    if (!nextValues.includes(trimmedValue)) {
        nextValues.push(trimmedValue);
    }

    return {
        ...currentFilters,
        [key]: nextValues,
    };
}

function removeFilterValue(
    currentFilters: SearchFilters,
    key: string,
    value: string,
): SearchFilters {
    const remainingValues = (currentFilters[key] ?? []).filter(
        (entry) => entry !== value,
    );

    if (remainingValues.length === 0) {
        const { [key]: _removed, ...remainingFilters } = currentFilters;

        return remainingFilters;
    }

    return {
        ...currentFilters,
        [key]: remainingValues,
    };
}

function getVisibleSuggestions(
    currentFilters: SearchFilters,
    suggestionValues: FilterSuggestionMap,
    fieldKey: string | null,
    draftValue: string,
): string[] {
    if (!fieldKey) {
        return [];
    }

    const existingValues = new Set(currentFilters[fieldKey] ?? []);
    const normalizedDraft = draftValue.trim().toLowerCase();

    return (suggestionValues[fieldKey] ?? [])
        .filter((value) => !existingValues.has(value))
        .filter(
            (value) =>
                normalizedDraft.length === 0 ||
                value.toLowerCase().includes(normalizedDraft),
        )
        .slice(0, 8);
}

export function FilterBuilder({
    currentFilters,
    metaKeys,
    seqmetaAvailable,
    suggestionValues = {},
    studies,
}: FilterBuilderProps) {
    const pathname = usePathname();
    const router = useRouter();
    const fieldOptions = getFieldOptions(metaKeys, seqmetaAvailable);
    const additionalFieldOptions = getAdditionalFieldOptions(fieldOptions);

    const [isPopoverOpen, setIsPopoverOpen] = useState(false);
    const [selectedFieldKey, setSelectedFieldKey] = useState<string | null>(
        null,
    );
    const [draftValue, setDraftValue] = useState("");
    const [permanentDraftValues, setPermanentDraftValues] = useState<
        Record<string, string>
    >({});

    const selectedField =
        additionalFieldOptions.find(
            (option) => option.key === selectedFieldKey,
        ) ?? null;
    const visibleSuggestions = getVisibleSuggestions(
        currentFilters,
        suggestionValues,
        selectedFieldKey,
        draftValue,
    );
    const suggestionListId = selectedFieldKey
        ? `filter-suggestions-${selectedFieldKey}`
        : undefined;

    function pushFilters(filters: SearchFilters) {
        const renderedQuery = buildSearchQuery(filters).toString();

        router.push(renderedQuery ? `${pathname}?${renderedQuery}` : pathname);
    }

    function applyFilterValue(fieldKey: string, value: string) {
        const nextFilters = createNextFilters(currentFilters, fieldKey, value);
        if (nextFilters === currentFilters) {
            return;
        }

        pushFilters(nextFilters);
        setDraftValue("");
        setSelectedFieldKey(null);
        setIsPopoverOpen(false);
    }

    function applyPermanentFilterValue(fieldKey: string, value: string) {
        const nextFilters = createNextFilters(currentFilters, fieldKey, value);
        if (nextFilters === currentFilters) {
            return;
        }

        pushFilters(nextFilters);
        setPermanentDraftValues((currentValues) => {
            const { [fieldKey]: _removed, ...remainingValues } = currentValues;

            return remainingValues;
        });
    }

    function handleFieldSelect(fieldKey: string) {
        setSelectedFieldKey(fieldKey);
        setDraftValue("");
    }

    function handleAddFilter(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!selectedField) {
            return;
        }

        if (!draftValue.trim()) {
            return;
        }

        applyFilterValue(selectedField.key, draftValue);
    }

    function handlePermanentFilterSubmit(
        event: FormEvent<HTMLFormElement>,
        fieldKey: string,
    ) {
        event.preventDefault();
        applyPermanentFilterValue(
            fieldKey,
            permanentDraftValues[fieldKey] ?? "",
        );
    }

    return (
        <section
            data-search-builder="true"
            className={cn(
                boxPanelRadiusClass,
                boxPanelInsetClass,
                "border border-border/70 bg-background/80 shadow-[0_24px_80px_-64px_rgba(29,44,69,0.78)]",
            )}
        >
            <div className="flex flex-col">
                <div className={boxTitleSectionClass}>
                    <div className={boxTitleRowClass}>
                        <Search
                            className={boxTitleIconClass}
                            aria-hidden="true"
                        />
                        <p className={boxTitleTextClass}>Search</p>
                    </div>

                    <div className="relative">
                        <button
                            type="button"
                            aria-expanded={isPopoverOpen}
                            aria-haspopup="dialog"
                            onClick={() =>
                                setIsPopoverOpen((current) => !current)
                            }
                            className={boxTitleActionClass}
                        >
                            <Plus className="size-4" />
                            Add filter
                        </button>

                        {isPopoverOpen ? (
                            <div
                                data-search-builder-popover="true"
                                role="dialog"
                                aria-label="Search builder filter panel"
                                className="absolute right-0 z-50 mt-3 w-[min(24rem,calc(100vw-2rem))] rounded-2xl border border-border/80 bg-popover p-0 text-popover-foreground shadow-[0_28px_90px_-54px_rgba(28,40,58,0.72)] outline-none"
                            >
                                <Command className="max-h-[32rem]">
                                    <CommandInput placeholder="Find a field" />
                                    <div className="flex min-h-0 flex-col">
                                        <CommandList
                                            data-search-builder-field-list="true"
                                            className="max-h-72 min-h-0 flex-1 p-2"
                                        >
                                            <CommandEmpty>
                                                No matching fields.
                                            </CommandEmpty>
                                            <CommandGroup>
                                                {additionalFieldOptions.map(
                                                    (field) => {
                                                        const isSelected =
                                                            field.key ===
                                                            selectedFieldKey;

                                                        return (
                                                            <CommandItem
                                                                key={field.key}
                                                                aria-label={
                                                                    field.label
                                                                }
                                                                className="flex w-full items-center justify-between gap-3 text-left"
                                                                data-filter-field-option={
                                                                    field.key
                                                                }
                                                                value={`${field.label} ${field.key}`}
                                                                onSelect={() =>
                                                                    handleFieldSelect(
                                                                        field.key,
                                                                    )
                                                                }
                                                            >
                                                                <span className="font-medium text-foreground">
                                                                    {
                                                                        field.label
                                                                    }
                                                                </span>
                                                                <Check
                                                                    className={cn(
                                                                        "ml-auto size-4 text-primary",
                                                                        isSelected
                                                                            ? "opacity-100"
                                                                            : "opacity-0",
                                                                    )}
                                                                />
                                                            </CommandItem>
                                                        );
                                                    },
                                                )}
                                            </CommandGroup>
                                        </CommandList>
                                        <div
                                            data-search-builder-selected-field-panel="true"
                                            className="border-t border-border/70 p-3"
                                        >
                                            {selectedField ? (
                                                <form
                                                    className="space-y-3"
                                                    onSubmit={handleAddFilter}
                                                >
                                                    <div className="space-y-2">
                                                        <label
                                                            htmlFor="filter-value"
                                                            className="text-sm font-medium text-foreground"
                                                        >
                                                            {
                                                                selectedField.label
                                                            }{" "}
                                                            value
                                                        </label>
                                                        <input
                                                            data-filter-value-input={
                                                                selectedField.key
                                                            }
                                                            id="filter-value"
                                                            list={
                                                                suggestionListId
                                                            }
                                                            value={draftValue}
                                                            onChange={(event) =>
                                                                setDraftValue(
                                                                    event.target
                                                                        .value,
                                                                )
                                                            }
                                                            className="h-11 w-full rounded-xl border border-border bg-background px-3 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-ring/30"
                                                        />
                                                        {visibleSuggestions.length >
                                                        0 ? (
                                                            <datalist
                                                                id={
                                                                    suggestionListId
                                                                }
                                                            >
                                                                {visibleSuggestions.map(
                                                                    (
                                                                        suggestion,
                                                                    ) => (
                                                                        <option
                                                                            key={
                                                                                suggestion
                                                                            }
                                                                            value={
                                                                                suggestion
                                                                            }
                                                                        />
                                                                    ),
                                                                )}
                                                            </datalist>
                                                        ) : null}
                                                    </div>
                                                    {selectedField.key ===
                                                    "library" ? (
                                                        <p
                                                            data-testid="library-filter-help"
                                                            className="text-sm leading-6 text-muted-foreground"
                                                        >
                                                            The first call for a
                                                            library= search can
                                                            take longer while a
                                                            cold MLWH cache
                                                            warms. Run wa mlwh
                                                            sync ahead of time
                                                            if you want to avoid
                                                            that delay.
                                                        </p>
                                                    ) : null}
                                                    <button
                                                        type="submit"
                                                        className="inline-flex h-11 w-full items-center justify-center rounded-xl bg-primary px-4 text-sm font-medium text-primary-foreground transition hover:opacity-95"
                                                    >
                                                        Add
                                                    </button>
                                                </form>
                                            ) : (
                                                <p className="text-sm leading-6 text-muted-foreground">
                                                    Choose a field, then enter a
                                                    value to append it to the
                                                    current search.
                                                </p>
                                            )}
                                        </div>
                                    </div>
                                </Command>
                            </div>
                        ) : null}
                    </div>
                </div>

                <div className="flex flex-col gap-4">
                    <div
                        data-search-builder-permanent-fields="true"
                        className="grid gap-3 sm:grid-cols-2 md:grid-cols-5"
                    >
                        {permanentFieldOptions.map((field) => {
                            const fieldInputId = `permanent-filter-${field.key}`;
                            const fieldDraftValue =
                                permanentDraftValues[field.key] ?? "";
                            const fieldSuggestions = getVisibleSuggestions(
                                currentFilters,
                                suggestionValues,
                                field.key,
                                fieldDraftValue,
                            );
                            const fieldSuggestionListId = `filter-suggestions-${field.key}`;

                            return (
                                <form
                                    key={field.key}
                                    className="min-w-0 space-y-1.5"
                                    onSubmit={(event) =>
                                        handlePermanentFilterSubmit(
                                            event,
                                            field.key,
                                        )
                                    }
                                >
                                    <label
                                        htmlFor={fieldInputId}
                                        className="block truncate text-xs font-semibold text-muted-foreground"
                                    >
                                        {field.label}
                                    </label>
                                    <div className="flex h-10 min-w-0 overflow-hidden rounded-xl border border-border bg-background transition focus-within:border-primary focus-within:ring-2 focus-within:ring-ring/30">
                                        <input
                                            data-permanent-filter-input={
                                                field.key
                                            }
                                            id={fieldInputId}
                                            list={fieldSuggestionListId}
                                            value={fieldDraftValue}
                                            onChange={(event) =>
                                                setPermanentDraftValues(
                                                    (currentValues) => ({
                                                        ...currentValues,
                                                        [field.key]:
                                                            event.target.value,
                                                    }),
                                                )
                                            }
                                            className="min-w-0 flex-1 bg-transparent px-3 text-sm outline-none placeholder:text-muted-foreground"
                                        />
                                        <button
                                            type="submit"
                                            aria-label={`Add ${field.label} filter`}
                                            title={`Add ${field.label} filter`}
                                            className="grid size-10 shrink-0 place-items-center border-l border-border bg-card text-foreground transition hover:bg-accent/35"
                                        >
                                            <Plus className="size-4" />
                                        </button>
                                    </div>
                                    {fieldSuggestions.length > 0 ? (
                                        <datalist id={fieldSuggestionListId}>
                                            {fieldSuggestions.map(
                                                (suggestion) => (
                                                    <option
                                                        key={suggestion}
                                                        value={suggestion}
                                                    />
                                                ),
                                            )}
                                        </datalist>
                                    ) : null}
                                </form>
                            );
                        })}
                    </div>

                    {Object.keys(currentFilters).length === 0 ? (
                        <div className="rounded-2xl border border-dashed border-border/80 bg-muted/35 px-4 py-5 text-sm text-muted-foreground">
                            No filters applied. Use the permanent fields above,
                            or add another filter.
                        </div>
                    ) : (
                        <div className="flex flex-wrap gap-3">
                            {Object.entries(currentFilters).map(
                                ([key, values]) => (
                                    <div
                                        key={key}
                                        className="flex flex-wrap items-center gap-2 rounded-2xl border border-border/70 bg-card/90 px-3 py-2"
                                    >
                                        <span className="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">
                                            {getFieldLabel(fieldOptions, key)}
                                        </span>
                                        {values.map((value) => {
                                            const fieldLabel = getFieldLabel(
                                                fieldOptions,
                                                key,
                                            );

                                            return (
                                                <button
                                                    key={`${key}:${value}`}
                                                    type="button"
                                                    onClick={() =>
                                                        pushFilters(
                                                            removeFilterValue(
                                                                currentFilters,
                                                                key,
                                                                value,
                                                            ),
                                                        )
                                                    }
                                                    aria-label={`Remove ${fieldLabel} ${value}`}
                                                    className="inline-flex items-center gap-2 rounded-full bg-secondary px-3 py-1.5 text-sm text-secondary-foreground transition hover:bg-secondary/80"
                                                >
                                                    <span>{value}</span>
                                                    <X className="size-3.5" />
                                                </button>
                                            );
                                        })}
                                    </div>
                                ),
                            )}
                        </div>
                    )}
                </div>
            </div>
        </section>
    );
}
