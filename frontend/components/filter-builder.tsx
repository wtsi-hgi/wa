"use client";

import { useState, type FormEvent } from "react";
import { Check, Plus, X } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";

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

const coreFieldOptions: FieldOption[] = [
    { key: "user", label: "Requester", placeholder: "alice" },
    { key: "operator", label: "Operator", placeholder: "operator-1" },
    {
        key: "pipeline_name",
        label: "Pipeline name",
        placeholder: "nf-core/rnaseq",
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
    { key: "run_key", label: "Run key", placeholder: "runid=48522" },
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
    seqmetaAvailable: boolean,
): FieldOption[] {
    const options = [...coreFieldOptions];

    if (seqmetaAvailable) {
        options.push({
            key: "study_id",
            label: "Study ID",
            placeholder: "6568",
        });
    }

    const dynamicOptions = metaKeys
        .map((metaKey) => ({
            key: toMetaQueryKey(metaKey),
            label: toTitleCase(metaKey),
            placeholder: metaKey === "seqmeta_sampleid" ? "SANG001" : "value",
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

    const [isPopoverOpen, setIsPopoverOpen] = useState(false);
    const [selectedFieldKey, setSelectedFieldKey] = useState<string | null>(
        null,
    );
    const [draftValue, setDraftValue] = useState("");

    const selectedField =
        fieldOptions.find((option) => option.key === selectedFieldKey) ?? null;
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

    return (
        <section
            data-search-builder="true"
            className="rounded-[1.5rem] border border-border/70 bg-background/80 p-4 shadow-[0_24px_80px_-64px_rgba(29,44,69,0.78)] sm:p-5"
        >
            <div className="flex flex-col gap-4">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                    <div className="space-y-1">
                        <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                            Search builder
                        </p>
                    </div>

                    <div className="relative">
                        <button
                            type="button"
                            aria-expanded={isPopoverOpen}
                            aria-haspopup="dialog"
                            onClick={() =>
                                setIsPopoverOpen((current) => !current)
                            }
                            className="inline-flex h-11 items-center justify-center gap-2 rounded-xl border border-border/80 bg-card px-4 text-sm font-medium text-foreground transition hover:border-primary/40 hover:bg-accent/35"
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
                                                {fieldOptions.map((field) => {
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
                                                                {field.label}
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
                                                })}
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
                                                            {selectedField.label} value
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
                                                            placeholder={
                                                                selectedField.placeholder
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

                {Object.keys(currentFilters).length === 0 ? (
                    <div className="rounded-2xl border border-dashed border-border/80 bg-muted/35 px-4 py-5 text-sm text-muted-foreground">
                        No filters applied. Start with Requester, Pipeline name,
                        or any metadata key exposed by the results service.
                    </div>
                ) : (
                    <div className="flex flex-wrap gap-3">
                        {Object.entries(currentFilters).map(([key, values]) => (
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
                        ))}
                    </div>
                )}
            </div>
        </section>
    );
}
