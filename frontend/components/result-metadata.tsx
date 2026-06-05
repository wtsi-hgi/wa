"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";

import { Info } from "lucide-react";

import { SeqmetaBadge } from "@/components/seqmeta-badge";
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@/components/ui/popover";
import type { EnrichmentResult } from "@/lib/contracts";
import {
    canonicalSeqmetaKey,
    isSeqmetaKey,
    isSeqmetaKeyForUserFacingMlwhMetadataKey,
    isUserFacingMlwhMetadataKey,
    preferredSeqmetaKeyForUserFacingMlwhMetadataKey,
    type UserFacingMlwhMetadataKey,
    userFacingMlwhMetadataLabel,
} from "@/lib/seqmeta-keys";

type ResultMetadataProps = {
    enrichments?: Record<string, EnrichmentResult | null>;
    errors?: Record<string, "not_found" | "upstream_impaired">;
    loading?: Record<string, boolean>;
    metadata: Record<string, string>;
    metadataValues?: Record<string, string[]>;
    variant?: "section" | "integrated";
};

type MetadataEntry = {
    displayKey: string;
    key: string;
    value: string;
    values: string[];
};

type IntegratedMetadataLayout = {
    limit: number;
    overflowing: boolean;
    signature: string;
};

const DEFAULT_INTEGRATED_METADATA_LIMIT = 3;
const OVERFLOW_TOLERANCE_PX = 1;

function initialIntegratedMetadataLayout(
    signature: string,
): IntegratedMetadataLayout {
    return {
        limit: DEFAULT_INTEGRATED_METADATA_LIMIT,
        overflowing: false,
        signature,
    };
}

function seqmetaLookupKey(value: string): string {
    return value.trim();
}

function nonEmptyMetadataValues(values: string[] | undefined): string[] {
    return (values ?? [])
        .map((value) => value.trim())
        .filter((value) => value.length > 0);
}

function orderedMetadataKeys(
    metadata: Record<string, string>,
    metadataValues: Record<string, string[]> | undefined,
): string[] {
    const keys: string[] = [];
    const seen = new Set<string>();

    for (const key of Object.keys(metadata)) {
        keys.push(key);
        seen.add(key);
    }

    for (const key of Object.keys(metadataValues ?? {})) {
        if (seen.has(key)) {
            continue;
        }

        keys.push(key);
    }

    return keys;
}

function metadataValuesForKey(
    key: string,
    metadata: Record<string, string>,
    metadataValues: Record<string, string[]> | undefined,
): string[] {
    const values = nonEmptyMetadataValues(metadataValues?.[key]);

    if (values.length > 0) {
        return values;
    }

    const value = metadata[key]?.trim();

    return value ? [value] : [];
}

function userFacingMlwhDisplayKeys(
    metadata: Record<string, string>,
    metadataValues: Record<string, string[]> | undefined,
): Map<UserFacingMlwhMetadataKey, string> {
    const keysWithValues = orderedMetadataKeys(metadata, metadataValues).filter(
        (key) => metadataValuesForKey(key, metadata, metadataValues).length > 0,
    );
    const displayKeys = new Map<UserFacingMlwhMetadataKey, string>();

    for (const key of keysWithValues) {
        if (!isUserFacingMlwhMetadataKey(key)) {
            continue;
        }

        const displayKey = preferredSeqmetaKeyForUserFacingMlwhMetadataKey(
            key,
            keysWithValues,
        );

        if (displayKey) {
            displayKeys.set(key, displayKey);
        }
    }

    return displayKeys;
}

function hidesDuplicateSeqmetaEntry(
    key: string,
    displayKeys: Map<UserFacingMlwhMetadataKey, string>,
): boolean {
    if (!isSeqmetaKey(key)) {
        return false;
    }

    for (const userFacingKey of displayKeys.keys()) {
        if (isSeqmetaKeyForUserFacingMlwhMetadataKey(userFacingKey, key)) {
            return true;
        }
    }

    return false;
}

function displayMetadataEntries(
    metadata: Record<string, string>,
    metadataValues: Record<string, string[]> | undefined,
): MetadataEntry[] {
    const entries: MetadataEntry[] = [];
    const seen = new Set<string>();
    const displayKeys = userFacingMlwhDisplayKeys(metadata, metadataValues);

    for (const [key, value] of Object.entries(metadata)) {
        if (hidesDuplicateSeqmetaEntry(key, displayKeys)) {
            seen.add(key);

            continue;
        }

        const values = nonEmptyMetadataValues(metadataValues?.[key]);
        entries.push({
            displayKey:
                displayKeys.get(key as UserFacingMlwhMetadataKey) ?? key,
            key,
            value: values.length > 1 ? values.join(", ") : value,
            values,
        });
        seen.add(key);
    }

    for (const [key, values] of Object.entries(metadataValues ?? {})) {
        if (seen.has(key)) {
            continue;
        }

        if (hidesDuplicateSeqmetaEntry(key, displayKeys)) {
            continue;
        }

        const displayValues = nonEmptyMetadataValues(values);
        if (displayValues.length === 0) {
            continue;
        }

        entries.push({
            displayKey:
                displayKeys.get(key as UserFacingMlwhMetadataKey) ?? key,
            key,
            value: displayValues.join(", "),
            values: displayValues,
        });
    }

    return entries;
}

function displayMetadataKey(key: string, displayKey = key): string {
    if (isUserFacingMlwhMetadataKey(key) && displayKey !== key) {
        return userFacingMlwhMetadataLabel(key);
    }

    return canonicalSeqmetaKey(key);
}

function displayMetadataStripKey(key: string, entryDisplayKey = key): string {
    if (isUserFacingMlwhMetadataKey(key) && entryDisplayKey !== key) {
        return userFacingMlwhMetadataLabel(key);
    }

    const displayKey = canonicalSeqmetaKey(key);

    if (
        displayKey === "seqmeta_id_study_lims" ||
        displayKey === "seqmeta_study_accession"
    ) {
        return "Study";
    }

    if (
        displayKey === "seqmeta_sample_name" ||
        displayKey === "seqmeta_sanger_sample_id" ||
        displayKey === "seqmeta_supplier_name" ||
        displayKey === "seqmeta_id_sample_lims"
    ) {
        return "Sample";
    }

    if (
        displayKey === "seqmeta_library_id" ||
        displayKey === "seqmeta_id_library_lims" ||
        displayKey === "seqmeta_pipeline_id_lims"
    ) {
        return "Library";
    }

    if (displayKey === "seqmeta_id_run") {
        return "Run";
    }

    if (displayKey === "seqmeta_lane" || displayKey === "seqmeta_tag_index") {
        return "Lane";
    }

    return displayMetadataKey(key);
}

function integratedSeqmetaPriority(key: string): number {
    const displayKey = canonicalSeqmetaKey(key);

    if (
        displayKey === "seqmeta_id_study_lims" ||
        displayKey === "seqmeta_study_accession"
    ) {
        return 0;
    }

    if (displayKey === "seqmeta_sample_name") {
        return 1;
    }

    if (displayKey === "seqmeta_sanger_sample_id") {
        return 2;
    }

    if (displayKey === "seqmeta_id_sample_lims") {
        return 3;
    }

    if (displayKey === "seqmeta_supplier_name") {
        return 4;
    }

    if (displayKey === "seqmeta_id_run") {
        return 5;
    }

    if (displayKey === "seqmeta_lane" || displayKey === "seqmeta_tag_index") {
        return 6;
    }

    if (displayKey === "seqmeta_library_id") {
        return 7;
    }

    if (displayKey === "seqmeta_id_library_lims") {
        return 8;
    }

    if (displayKey === "seqmeta_pipeline_id_lims") {
        return 9;
    }

    return 10;
}

function prioritizedIntegratedEntries(entries: MetadataEntry[]) {
    const seqmetaEntries = entries
        .map((entry, index) => ({ entry, index }))
        .filter(({ entry }) => isSeqmetaKey(entry.key))
        .sort(
            (
                { entry: leftEntry, index: leftIndex },
                { entry: rightEntry, index: rightIndex },
            ) =>
                integratedSeqmetaPriority(leftEntry.key) -
                    integratedSeqmetaPriority(rightEntry.key) ||
                leftIndex - rightIndex,
        )
        .map(({ entry }) => entry);

    if (seqmetaEntries.length === 0) {
        return entries;
    }

    const nonSeqmetaEntries = entries.filter(
        (entry) => !isSeqmetaKey(entry.key),
    );

    return [...seqmetaEntries, ...nonSeqmetaEntries];
}

function truncatedIntegratedEntries(entries: MetadataEntry[], limit: number) {
    const boundedLimit = Math.max(1, Math.min(limit, entries.length - 1));

    return prioritizedIntegratedEntries(entries).slice(0, boundedLimit);
}

function metadataEntriesSignature(entries: MetadataEntry[]): string {
    return entries
        .map((entry) => `${entry.key}\u0000${entry.value}`)
        .join("\u0001");
}

function hasVerticalOverflow(element: HTMLElement): boolean {
    return element.scrollHeight - element.clientHeight > OVERFLOW_TOLERANCE_PX;
}

function MetadataValue({
    display = "strip",
    enrichments,
    errors,
    loading,
    metadataKey,
    value,
    values,
}: {
    display?: "detail" | "strip";
    enrichments: Record<string, EnrichmentResult | null>;
    errors: Record<string, "not_found" | "upstream_impaired">;
    loading: Record<string, boolean>;
    metadataKey: string;
    value: string;
    values: string[];
}) {
    const displayValue = values.length > 1 ? values.join(", ") : value;

    if (isSeqmetaKey(metadataKey)) {
        const seqmetaValues =
            values.length > 0
                ? values
                : nonEmptyMetadataValues(displayValue ? [displayValue] : []);

        return (
            <span
                className={
                    display === "detail"
                        ? "inline-flex max-w-full flex-wrap items-center gap-x-1.5 gap-y-1"
                        : "inline-flex min-w-0 max-w-full items-center gap-1 overflow-hidden align-middle"
                }
            >
                {seqmetaValues.map((rawValue, index) => {
                    const lookupKey = seqmetaLookupKey(rawValue);

                    return (
                        <SeqmetaBadge
                            key={`${rawValue}:${index}`}
                            metadataKey={metadataKey}
                            rawValue={rawValue}
                            enrichment={enrichments[lookupKey] ?? null}
                            error={errors[lookupKey]}
                            loading={Boolean(loading[lookupKey])}
                            statusPlacement={
                                display === "strip" ? "overlay" : "inline"
                            }
                        />
                    );
                })}
            </span>
        );
    }

    return (
        <span
            className={
                display === "detail"
                    ? "break-words text-sm leading-5 text-foreground"
                    : "min-w-0 truncate text-xs font-medium text-foreground"
            }
        >
            {displayValue}
        </span>
    );
}

export function ResultMetadata({
    enrichments = {},
    errors = {},
    loading = {},
    metadata,
    metadataValues,
    variant = "section",
}: ResultMetadataProps) {
    const entries = displayMetadataEntries(metadata, metadataValues);
    const metadataSignature = metadataEntriesSignature(entries);
    const integratedLayoutRef = useRef<HTMLDivElement>(null);
    const metadataStripRef = useRef<HTMLDListElement>(null);
    const [measureVersion, setMeasureVersion] = useState(0);
    const [integratedLayout, setIntegratedLayout] = useState(() =>
        initialIntegratedMetadataLayout(metadataSignature),
    );
    const activeIntegratedLayout =
        integratedLayout.signature === metadataSignature
            ? integratedLayout
            : initialIntegratedMetadataLayout(metadataSignature);
    const visibleEntries =
        activeIntegratedLayout.overflowing && entries.length > 1
            ? truncatedIntegratedEntries(entries, activeIntegratedLayout.limit)
            : entries;
    const hasHiddenEntries =
        variant === "integrated" &&
        activeIntegratedLayout.overflowing &&
        entries.length > visibleEntries.length;

    useLayoutEffect(() => {
        if (variant !== "integrated" || entries.length === 0) {
            return;
        }

        const strip = metadataStripRef.current;

        if (!strip) {
            return;
        }

        const animationFrame = window.requestAnimationFrame(() => {
            const currentStrip = metadataStripRef.current;

            if (!currentStrip) {
                return;
            }

            const overflowing = hasVerticalOverflow(currentStrip);

            if (!activeIntegratedLayout.overflowing) {
                if (overflowing) {
                    setIntegratedLayout({
                        overflowing: true,
                        signature: metadataSignature,
                        limit: Math.max(
                            1,
                            Math.min(
                                DEFAULT_INTEGRATED_METADATA_LIMIT,
                                entries.length - 1,
                            ),
                        ),
                    });
                }

                return;
            }

            if (overflowing && activeIntegratedLayout.limit > 1) {
                setIntegratedLayout((current) => {
                    if (
                        current.signature !== metadataSignature ||
                        !current.overflowing ||
                        current.limit !== activeIntegratedLayout.limit
                    ) {
                        return current;
                    }

                    return {
                        ...current,
                        limit: current.limit - 1,
                    };
                });
            }
        });

        return () => {
            window.cancelAnimationFrame(animationFrame);
        };
    }, [
        activeIntegratedLayout.limit,
        activeIntegratedLayout.overflowing,
        entries.length,
        measureVersion,
        metadataSignature,
        variant,
    ]);

    useEffect(() => {
        if (variant !== "integrated" || typeof window === "undefined") {
            return;
        }

        const layout = integratedLayoutRef.current;

        if (!layout) {
            return;
        }

        let lastWidth = layout.getBoundingClientRect().width;
        let animationFrame = 0;
        const resetForMeasurement = (nextWidth: number) => {
            if (Math.abs(nextWidth - lastWidth) < 1) {
                return;
            }

            lastWidth = nextWidth;

            if (animationFrame) {
                window.cancelAnimationFrame(animationFrame);
            }

            animationFrame = window.requestAnimationFrame(() => {
                setIntegratedLayout(
                    initialIntegratedMetadataLayout(metadataSignature),
                );
                setMeasureVersion((version) => version + 1);
            });
        };
        const handleWindowResize = () => {
            resetForMeasurement(layout.getBoundingClientRect().width);
        };
        const resizeObserver =
            "ResizeObserver" in window
                ? new window.ResizeObserver((observedEntries) => {
                      const observedWidth =
                          observedEntries[0]?.contentRect.width ??
                          layout.getBoundingClientRect().width;

                      resetForMeasurement(observedWidth);
                  })
                : null;

        resizeObserver?.observe(layout);
        window.addEventListener("resize", handleWindowResize);

        return () => {
            if (animationFrame) {
                window.cancelAnimationFrame(animationFrame);
            }

            resizeObserver?.disconnect();
            window.removeEventListener("resize", handleWindowResize);
        };
    }, [metadataSignature, variant]);

    if (variant === "integrated") {
        return (
            <div
                ref={integratedLayoutRef}
                className="min-w-0 space-y-2"
                data-result-metadata-layout="integrated"
            >
                <div className="flex min-h-7 items-center gap-2">
                    <p className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                        Metadata
                    </p>
                    {hasHiddenEntries ? (
                        <Popover>
                            <PopoverTrigger
                                aria-label="All metadata"
                                className="inline-flex min-h-7 items-center gap-1.5 rounded-full border border-border/70 bg-card/70 px-2.5 py-0.5 text-xs font-medium text-muted-foreground transition hover:border-primary/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
                                data-metadata-details-trigger="true"
                            >
                                <Info
                                    className="h-3.5 w-3.5"
                                    aria-hidden="true"
                                />
                                <span>all</span>
                            </PopoverTrigger>
                            <PopoverContent
                                align="end"
                                className="w-[min(92vw,42rem)] p-4"
                            >
                                <div className="flex items-center justify-between gap-3">
                                    <p className="text-sm font-semibold text-foreground">
                                        Metadata
                                    </p>
                                    <p className="text-xs text-muted-foreground">
                                        {entries.length}{" "}
                                        {entries.length === 1 ? "key" : "keys"}
                                    </p>
                                </div>
                                <dl
                                    className="mt-3 grid max-h-[min(24rem,70vh)] gap-2 overflow-auto pr-1 sm:grid-cols-2"
                                    data-result-metadata-details-panel="true"
                                >
                                    {entries.map(
                                        ({
                                            displayKey,
                                            key,
                                            value,
                                            values,
                                        }) => (
                                            <div
                                                key={key}
                                                className="min-w-0 rounded-lg border border-border/60 bg-background/70 px-3 py-2"
                                                data-metadata-detail-row={key}
                                            >
                                                <dt className="break-all font-mono text-[11px] text-muted-foreground">
                                                    {displayMetadataKey(
                                                        key,
                                                        displayKey,
                                                    )}
                                                </dt>
                                                <dd className="mt-1 min-w-0">
                                                    <MetadataValue
                                                        display="detail"
                                                        enrichments={
                                                            enrichments
                                                        }
                                                        errors={errors}
                                                        loading={loading}
                                                        metadataKey={displayKey}
                                                        value={value}
                                                        values={values}
                                                    />
                                                </dd>
                                            </div>
                                        ),
                                    )}
                                </dl>
                            </PopoverContent>
                        </Popover>
                    ) : null}
                </div>

                {entries.length === 0 ? (
                    <p className="inline-flex min-h-8 items-center rounded-full border border-border/65 bg-background/70 px-3 py-1 text-xs text-muted-foreground">
                        No metadata
                    </p>
                ) : (
                    <dl
                        ref={metadataStripRef}
                        className="flex max-h-20 flex-wrap gap-1.5 overflow-auto pr-1"
                        data-result-metadata-strip="true"
                    >
                        {visibleEntries.map(
                            ({ displayKey, key, value, values }) => (
                                <div
                                    key={key}
                                    className="inline-flex min-h-7 max-w-full items-center gap-1.5 rounded-full border border-border/65 bg-background/70 px-2 py-0.5 text-xs shadow-[0_10px_28px_-26px_rgba(28,40,58,0.72)]"
                                    data-metadata-row={key}
                                >
                                    <dt className="min-w-0 shrink truncate font-mono text-[11px] text-muted-foreground">
                                        {displayMetadataStripKey(
                                            key,
                                            displayKey,
                                        )}
                                    </dt>
                                    <dd className="min-w-0">
                                        <MetadataValue
                                            enrichments={enrichments}
                                            errors={errors}
                                            loading={loading}
                                            metadataKey={displayKey}
                                            value={value}
                                            values={values}
                                        />
                                    </dd>
                                </div>
                            ),
                        )}
                        {hasHiddenEntries ? (
                            <div className="inline-flex min-h-7 items-center rounded-full border border-border/65 bg-card/70 px-2.5 py-0.5 text-xs text-muted-foreground">
                                +{entries.length - visibleEntries.length}
                            </div>
                        ) : null}
                    </dl>
                )}
            </div>
        );
    }

    return (
        <section className="rounded-[1.75rem] border border-border/70 bg-card/85 p-6 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]">
            <div className="space-y-2">
                <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
                    Result metadata
                </p>
                <h2 className="text-2xl font-semibold tracking-tight">
                    Metadata keys and values
                </h2>
            </div>

            {entries.length === 0 ? (
                <p className="mt-6 text-sm leading-7 text-muted-foreground">
                    No metadata
                </p>
            ) : (
                <div className="mt-6 overflow-hidden rounded-[1.5rem] border border-border/70">
                    <table className="min-w-full divide-y divide-border/70 text-left text-sm">
                        <thead className="bg-muted/40">
                            <tr>
                                <th className="px-4 py-3 font-medium">Key</th>
                                <th className="px-4 py-3 font-medium">Value</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-border/60 bg-card/60">
                            {entries.map(
                                ({ displayKey, key, value, values }) => (
                                    <tr key={key} data-metadata-row={key}>
                                        <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                                            {displayMetadataKey(
                                                key,
                                                displayKey,
                                            )}
                                        </td>
                                        <td className="px-4 py-3">
                                            <MetadataValue
                                                display="detail"
                                                enrichments={enrichments}
                                                errors={errors}
                                                loading={loading}
                                                metadataKey={displayKey}
                                                value={value}
                                                values={values}
                                            />
                                        </td>
                                    </tr>
                                ),
                            )}
                        </tbody>
                    </table>
                </div>
            )}
        </section>
    );
}
