"use client";

import Link from "next/link";
import { useEffect, useMemo, useState, type ReactNode } from "react";

import {
    ChevronDown,
    ChevronRight,
    Copy,
    Loader2,
    Search,
    X,
} from "lucide-react";

import type {
    EnrichmentResult,
    EnrichmentSample,
    LibraryDetail,
    MissingHop,
} from "@/lib/contracts";
import { canonicalSeqmetaKey } from "@/lib/seqmeta-keys";
import { fetchLibrarySamples } from "@/lib/seqmeta-enrichment";
import { cn } from "@/lib/utils";

type SeqmetaBadgeProps = {
    metadataKey: string;
    rawValue: string;
    enrichment: EnrichmentResult | null;
    error?: "not_found" | "upstream_impaired";
    loading?: boolean;
    statusPlacement?: "inline" | "overlay";
    triggerLabel?: string;
};

type SeqmetaDetailField = {
    key: string;
    label: string;
    searchKey?: string;
    searchValue?: string;
    value: string;
    group: "direct" | "related";
};

type HierarchicalLibrary = {
    libraryType: string;
    idStudyLims: string;
    libraryId?: string;
    idLibraryLims?: string;
    samples: EnrichmentSample[];
};

type HierarchicalGroup = {
    type: "libraries" | "library" | "samples" | "study" | "lanes";
    title: string;
    items:
        | HierarchicalLibrary[]
        | EnrichmentSample[]
        | { name: string; id: string; accession?: string }[]
        | { id_run: string; lane: string; tag_index: number }[];
};

type EntityMetadataPair = {
    label: string;
    value: string;
};

type EntityDisplay = {
    title: string;
    metadata: EntityMetadataPair[];
};

type RelatedEntityRowProps = {
    children?: ReactNode;
    className?: string;
    copied: boolean;
    copyAriaLabel: string;
    detailKey: string;
    filterAriaLabel?: string;
    filterHref?: string;
    metadata: EntityMetadataPair[];
    onCopy: () => void;
    title: string;
};

type DirectMetadataRowProps = {
    copied: boolean;
    field: SeqmetaDetailField;
    onCopy: () => void;
};

type LibrarySearchTarget = {
    key: string;
    value: string;
};

type LibraryLinkLike = {
    library_type?: string;
    id_study_lims?: string;
    library_id?: string;
    id_library_lims?: string;
};

const RELATED_SAMPLE_RENDER_LIMIT = 50;

function asString(value: unknown): string | null {
    return typeof value === "string" && value.trim() ? value : null;
}

function seqmetaDisplayKey(metadataKey: string): string {
    return canonicalSeqmetaKey(metadataKey);
}

function humanizeToken(token: string): string {
    return token
        .split("_")
        .filter(Boolean)
        .map((part) => {
            if (part === "id") {
                return "ID";
            }

            if (part === "lims") {
                return "LIMS";
            }

            return part.replace(/^./, (letter) => letter.toUpperCase());
        })
        .join(" ");
}

function metadataLabel(metadataKey: string): string {
    const displayKey = seqmetaDisplayKey(metadataKey);
    const trimmedKey = displayKey.replace(/^seqmeta_/, "");

    if (trimmedKey === "library_id") {
        return "Library ID";
    }

    if (trimmedKey === "id_library_lims") {
        return "Library LIMS ID";
    }

    if (trimmedKey === "pipeline_id_lims") {
        return "Library type";
    }

    if (trimmedKey === "sample_name") {
        return "Sample name";
    }

    if (trimmedKey === "study_name") {
        return "Study name";
    }

    if (trimmedKey === "sanger_sample_id") {
        return "Sanger sample ID";
    }

    if (trimmedKey === "supplier_name") {
        return "Sample supplier name";
    }

    if (trimmedKey === "id_sample_lims") {
        return "Sample LIMS ID";
    }

    if (trimmedKey === "id_study_lims") {
        return "Study LIMS ID";
    }

    if (trimmedKey === "study_accession") {
        return "Study accession";
    }

    if (trimmedKey === "uuid_study_lims") {
        return "Study UUID";
    }

    if (trimmedKey === "uuid_sample_lims") {
        return "Sample UUID";
    }

    if (trimmedKey === "donor_id") {
        return "Donor ID";
    }

    return humanizeToken(trimmedKey);
}

function metadataLabelForSentence(metadataKey: string): string {
    const displayKey = seqmetaDisplayKey(metadataKey);

    if (displayKey === "seqmeta_library_id") {
        return "library ID";
    }

    if (displayKey === "seqmeta_id_library_lims") {
        return "library LIMS ID";
    }

    return metadataLabel(metadataKey).toLowerCase();
}

function isLibraryMetadataKey(metadataKey: string): boolean {
    const displayKey = seqmetaDisplayKey(metadataKey);

    return (
        displayKey === "seqmeta_pipeline_id_lims" ||
        displayKey === "seqmeta_library_id" ||
        displayKey === "seqmeta_id_library_lims"
    );
}

function directLibraryMetadataKey(metadataKey: string): string {
    const displayKey = seqmetaDisplayKey(metadataKey);

    if (displayKey === "seqmeta_library_id") {
        return "seqmeta_library_id";
    }

    if (displayKey === "seqmeta_id_library_lims") {
        return "seqmeta_id_library_lims";
    }

    return "seqmeta_pipeline_id_lims";
}

function copiedStateKey(fieldKey: string, fieldValue: string): string {
    return `${fieldKey}:${fieldValue}`;
}

function titleFilterSearchKey(metadataKey: string): string | null {
    const displayKey = seqmetaDisplayKey(metadataKey);

    if (
        displayKey === "seqmeta_id_study_lims" ||
        displayKey === "seqmeta_study_accession" ||
        displayKey === "seqmeta_uuid_study_lims" ||
        displayKey === "seqmeta_study_name"
    ) {
        return "study";
    }

    if (
        displayKey === "seqmeta_sample_name" ||
        displayKey === "seqmeta_supplier_name"
    ) {
        return "sample";
    }

    if (
        displayKey === "seqmeta_sanger_sample_id" ||
        displayKey === "seqmeta_id_sample_lims"
    ) {
        return displayKey;
    }

    if (displayKey === "seqmeta_pipeline_id_lims") {
        return "library";
    }

    if (displayKey.startsWith("seqmeta_")) {
        return displayKey;
    }

    return null;
}

function titleFilterHref(metadataKey: string, rawValue: string): string | null {
    const searchKey = titleFilterSearchKey(metadataKey);
    const value = rawValue.trim();

    if (!searchKey || !value) {
        return null;
    }

    return `/?${new URLSearchParams({ [searchKey]: value }).toString()}`;
}

function detailFieldHref(field: SeqmetaDetailField): string | null {
    if (!field.searchKey) {
        return null;
    }

    return `/?${new URLSearchParams({ [field.searchKey]: field.searchValue ?? field.value }).toString()}`;
}

function directDetailSearchKey(
    metadataKey: string,
    fieldKey: string,
): string | undefined {
    const displayFieldKey = seqmetaDisplayKey(fieldKey);

    if (isStudyMetadataKey(metadataKey)) {
        if (
            displayFieldKey === "seqmeta_study_name" ||
            displayFieldKey === "seqmeta_id_study_lims" ||
            displayFieldKey === "seqmeta_accession_number"
        ) {
            return "study";
        }
    }

    if (isSampleMetadataKey(metadataKey)) {
        if (displayFieldKey === "seqmeta_sample_name") {
            return displayFieldKey;
        }

        if (displayFieldKey === "seqmeta_supplier_name") {
            return "sample";
        }

        if (
            displayFieldKey === "seqmeta_sanger_sample_id" ||
            displayFieldKey === "seqmeta_id_sample_lims" ||
            displayFieldKey === "seqmeta_accession_number"
        ) {
            return displayFieldKey;
        }
    }

    if (isLibraryMetadataKey(metadataKey)) {
        if (displayFieldKey === "seqmeta_pipeline_id_lims") {
            return "library";
        }

        if (
            displayFieldKey === "seqmeta_library_id" ||
            displayFieldKey === "seqmeta_id_library_lims"
        ) {
            return displayFieldKey;
        }
    }

    if (isRunMetadataKey(metadataKey) && displayFieldKey === "seqmeta_id_run") {
        return displayFieldKey;
    }

    return undefined;
}

function isStudyMetadataKey(metadataKey: string): boolean {
    const displayKey = seqmetaDisplayKey(metadataKey);

    return (
        displayKey === "seqmeta_id_study_lims" ||
        displayKey === "seqmeta_study_accession" ||
        displayKey === "seqmeta_uuid_study_lims" ||
        displayKey === "seqmeta_study_name"
    );
}

function isRunMetadataKey(metadataKey: string): boolean {
    return seqmetaDisplayKey(metadataKey) === "seqmeta_id_run";
}

function isSampleMetadataKey(metadataKey: string): boolean {
    const displayKey = seqmetaDisplayKey(metadataKey);

    return (
        displayKey === "seqmeta_sample_name" ||
        displayKey === "seqmeta_supplier_name" ||
        displayKey === "seqmeta_sanger_sample_id" ||
        displayKey === "seqmeta_id_sample_lims" ||
        displayKey === "seqmeta_accession_number" ||
        displayKey === "seqmeta_uuid_sample_lims" ||
        displayKey === "seqmeta_donor_id"
    );
}

function primaryLabel(
    rawValue: string,
    enrichment: EnrichmentResult | null,
): string {
    return rawValue;
}

function sampleRecordForTitle(
    enrichment: EnrichmentResult | null,
): EnrichmentSample | null {
    return (
        enrichment?.graph.sample ??
        enrichment?.graph.sample_detail?.sample ??
        null
    );
}

function valuesMatch(left: string | null, right: string): boolean {
    return left?.trim().toLowerCase() === right.trim().toLowerCase();
}

function isSupplierBackedSampleTitle(
    metadataKey: string,
    rawValue: string,
    enrichment: EnrichmentResult | null,
): boolean {
    const displayKey = seqmetaDisplayKey(metadataKey);

    if (
        displayKey !== "seqmeta_sample_name" &&
        displayKey !== "seqmeta_supplier_name"
    ) {
        return false;
    }

    const sampleRecord = sampleRecordForTitle(enrichment);

    if (!sampleRecord) {
        return false;
    }

    const raw = rawValue.trim();

    if (!raw) {
        return false;
    }

    const supplierName = asString(sampleRecord.supplier_name);

    return (
        enrichment?.type === "supplier_name" ||
        (valuesMatch(supplierName, raw) &&
            !valuesMatch(asString(sampleRecord.sample_name), raw) &&
            !valuesMatch(asString(sampleRecord.sanger_id), raw) &&
            !valuesMatch(asString(sampleRecord.id_sample_lims), raw))
    );
}

function titleMetadataDisplayKey(
    metadataKey: string,
    rawValue: string,
    enrichment: EnrichmentResult | null,
): string {
    if (isSupplierBackedSampleTitle(metadataKey, rawValue, enrichment)) {
        return "seqmeta_supplier_name";
    }

    return seqmetaDisplayKey(metadataKey);
}

function librarySampleKey(sample: EnrichmentSample, index: number): string {
    const keyParts = [
        asString(sample.sanger_id) ?? "",
        asString(sample.id_sample_lims) ?? "",
        asString(sample.sample_name) ?? "",
        String(sample.id_run ?? ""),
        String(sample.lane ?? ""),
        String(sample.tag_index ?? ""),
    ];

    return `${keyParts.join("|")}|${index}`;
}

function libraryIdentityKey(library: HierarchicalLibrary): string {
    return [
        library.idStudyLims,
        library.libraryType,
        library.libraryId ?? "",
        library.idLibraryLims ?? "",
    ].join("|");
}

function libraryDisplayLabel(library: HierarchicalLibrary): string {
    return librarySearchTarget(library).value;
}

function librarySearchTarget(
    library: HierarchicalLibrary,
): LibrarySearchTarget {
    const libraryId = asString(library.libraryId);
    if (libraryId) {
        return { key: "seqmeta_library_id", value: libraryId };
    }

    const libraryLimsId = asString(library.idLibraryLims);
    if (libraryLimsId) {
        return { key: "seqmeta_id_library_lims", value: libraryLimsId };
    }

    return { key: "library", value: library.libraryType };
}

function libraryFilterHref(library: HierarchicalLibrary): string {
    const target = librarySearchTarget(library);

    return `/?${new URLSearchParams({ [target.key]: target.value }).toString()}`;
}

function librarySampleFilters(
    library: HierarchicalLibrary,
): { idLibraryLims?: string; libraryId?: string } | undefined {
    const filters = {
        idLibraryLims: library.idLibraryLims,
        libraryId: library.libraryId,
    };

    return filters.idLibraryLims || filters.libraryId ? filters : undefined;
}

function visibleRelatedSamples(
    samples: EnrichmentSample[],
): EnrichmentSample[] {
    return samples.slice(0, RELATED_SAMPLE_RENDER_LIMIT);
}

function relatedSamplesSummary(samples: EnrichmentSample[]): string | null {
    if (samples.length <= RELATED_SAMPLE_RENDER_LIMIT) {
        return null;
    }

    return `Showing ${RELATED_SAMPLE_RENDER_LIMIT} of ${samples.length} samples`;
}

function laneDetailId(lane: {
    id_run: string;
    lane: string;
    tag_index: number;
}): string {
    return `${lane.id_run}_${lane.lane}#${lane.tag_index}`;
}

function entityTitle(candidates: (string | null | undefined)[]): string {
    return candidates.find((value) => asString(value))?.trim() ?? "";
}

function entityMetadataPairs(
    pairs: (EntityMetadataPair | null)[],
    title?: string,
): EntityMetadataPair[] {
    const seen = new Set<string>();
    const metadata: EntityMetadataPair[] = [];
    const titleValue = title?.trim().toLowerCase();

    for (const pair of pairs) {
        if (!pair) {
            continue;
        }

        const value = pair.value.trim();
        if (!value) {
            continue;
        }

        if (titleValue && value.toLowerCase() === titleValue) {
            continue;
        }

        const key = `${pair.label}:${value}`;
        if (seen.has(key)) {
            continue;
        }

        seen.add(key);
        metadata.push({ ...pair, value });
    }

    return metadata;
}

function studyEntityDisplay(study: {
    name: string;
    id: string;
    accession?: string;
}): EntityDisplay {
    const title = entityTitle([study.id, study.name]);

    return {
        title,
        metadata: entityMetadataPairs(
            [
                { label: "name", value: study.name },
                { label: "id", value: study.id },
                study.accession
                    ? { label: "accession", value: study.accession }
                    : null,
            ],
            title,
        ),
    };
}

function sampleEntityDisplay(sample: EnrichmentSample): EntityDisplay {
    const title = entityTitle([
        asString(sample.sanger_id),
        asString(sample.id_sample_lims),
        asString(sample.sample_name),
    ]);

    return {
        title,
        metadata: entityMetadataPairs(
            [
                asString(sample.sample_name)
                    ? { label: "name", value: sample.sample_name }
                    : null,
                asString(sample.sanger_id)
                    ? { label: "id", value: sample.sanger_id }
                    : null,
                asString(sample.id_sample_lims)
                    ? { label: "sample_lims", value: sample.id_sample_lims }
                    : null,
                asString(sample.accession_number)
                    ? { label: "accession", value: sample.accession_number }
                    : null,
            ],
            title,
        ),
    };
}

function libraryEntityDisplay(library: HierarchicalLibrary): EntityDisplay {
    const libraryId = asString(library.libraryId);
    const libraryLimsId = asString(library.idLibraryLims);
    const title = entityTitle([
        librarySearchTarget(library).value,
        libraryId,
        libraryLimsId,
        library.libraryType,
    ]);

    return {
        title,
        metadata: entityMetadataPairs(
            [
                libraryId ? { label: "id", value: libraryId } : null,
                libraryLimsId
                    ? { label: "library_lims", value: libraryLimsId }
                    : null,
                asString(library.libraryType)
                    ? { label: "type", value: library.libraryType }
                    : null,
            ],
            title,
        ),
    };
}

function libraryFromLink(
    link: LibraryLinkLike | null | undefined,
    samples: EnrichmentSample[] = [],
): HierarchicalLibrary | null {
    const libraryType = asString(link?.library_type);
    if (!libraryType) {
        return null;
    }

    return {
        libraryType,
        idStudyLims: asString(link?.id_study_lims) ?? "",
        libraryId: asString(link?.library_id) ?? undefined,
        idLibraryLims: asString(link?.id_library_lims) ?? undefined,
        samples,
    };
}

function addLibraryCandidate(
    candidates: HierarchicalLibrary[],
    seen: Set<string>,
    candidate: HierarchicalLibrary | null,
) {
    if (!candidate) {
        return;
    }

    const key = libraryIdentityKey(candidate);
    if (seen.has(key)) {
        return;
    }

    seen.add(key);
    candidates.push(candidate);
}

function libraryCandidates(
    enrichment: EnrichmentResult | null,
): HierarchicalLibrary[] {
    if (!enrichment) {
        return [];
    }

    const candidates: HierarchicalLibrary[] = [];
    const seen = new Set<string>();

    addLibraryCandidate(
        candidates,
        seen,
        libraryFromLink(enrichment.graph.library),
    );

    for (const library of enrichment.graph.sample_detail?.libraries ?? []) {
        addLibraryCandidate(candidates, seen, libraryFromLink(library));
    }

    for (const library of enrichment.graph.libraries ?? []) {
        addLibraryCandidate(candidates, seen, libraryFromLink(library));
    }

    for (const detail of enrichment.graph.study_detail?.library_details ?? []) {
        addLibraryCandidate(
            candidates,
            seen,
            libraryFromLink(detail, detail.samples),
        );
    }

    for (const studyDetail of enrichment.graph.study_details ?? []) {
        for (const detail of studyDetail.library_details) {
            addLibraryCandidate(
                candidates,
                seen,
                libraryFromLink(detail, detail.samples),
            );
        }
    }

    const sampleLibrary = enrichment.graph.sample?.library_type
        ? {
              libraryType: enrichment.graph.sample.library_type,
              idStudyLims: enrichment.graph.sample.id_study_lims,
              samples: [],
          }
        : null;
    addLibraryCandidate(candidates, seen, sampleLibrary);

    return candidates;
}

function libraryMatchesMetadataValue(
    library: HierarchicalLibrary,
    metadataKey: string,
    rawValue: string,
): boolean {
    const value = rawValue.trim().toLowerCase();
    if (!value) {
        return false;
    }

    const displayKey = seqmetaDisplayKey(metadataKey);

    if (displayKey === "seqmeta_library_id") {
        return library.libraryId?.toLowerCase() === value;
    }

    if (displayKey === "seqmeta_id_library_lims") {
        return library.idLibraryLims?.toLowerCase() === value;
    }

    return library.libraryType.toLowerCase() === value;
}

function bestLibraryForMetadata(
    metadataKey: string,
    rawValue: string,
    enrichment: EnrichmentResult | null,
): HierarchicalLibrary | null {
    const candidates = libraryCandidates(enrichment);

    return (
        candidates.find((library) =>
            libraryMatchesMetadataValue(library, metadataKey, rawValue),
        ) ??
        candidates.find(
            (library) => library.libraryId || library.idLibraryLims,
        ) ??
        candidates[0] ??
        null
    );
}

function sampleLibraryForGroups(
    enrichment: EnrichmentResult,
): HierarchicalLibrary | null {
    const libraryType = asString(enrichment.graph.sample?.library_type);
    const idStudyLims = asString(enrichment.graph.sample?.id_study_lims) ?? "";
    const candidates = libraryCandidates(enrichment);
    const matchesSample = (library: HierarchicalLibrary) =>
        (!libraryType || library.libraryType === libraryType) &&
        (!idStudyLims ||
            !library.idStudyLims ||
            library.idStudyLims === idStudyLims);

    return (
        candidates.find(
            (library) =>
                matchesSample(library) &&
                (library.libraryId || library.idLibraryLims),
        ) ??
        candidates.find(matchesSample) ??
        (libraryType
            ? {
                  libraryType,
                  idStudyLims,
                  samples: [],
              }
            : null)
    );
}

function laneEntityDisplay(lane: {
    id_run: string;
    lane: string;
    tag_index: number;
}): EntityDisplay {
    const title = laneDetailId(lane);

    return {
        title,
        metadata: entityMetadataPairs(
            [
                { label: "id", value: laneDetailId(lane) },
                { label: "id_run", value: lane.id_run },
                { label: "lane", value: lane.lane },
                { label: "tag_index", value: String(lane.tag_index) },
            ],
            title,
        ),
    };
}

function EntityMetadataPairs({ pairs }: { pairs: EntityMetadataPair[] }) {
    if (pairs.length === 0) {
        return null;
    }

    return (
        <dl className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs leading-5 text-muted-foreground">
            {pairs.map((pair) => (
                <div
                    key={`${pair.label}:${pair.value}`}
                    className="inline-flex max-w-full gap-1"
                >
                    <dt className="font-medium text-foreground/70">
                        {pair.label}:
                    </dt>
                    <dd className="break-all">{pair.value}</dd>
                </div>
            ))}
        </dl>
    );
}

function RelatedEntityRow({
    children,
    className,
    copied,
    copyAriaLabel,
    detailKey,
    filterAriaLabel,
    filterHref,
    metadata,
    onCopy,
    title,
}: RelatedEntityRowProps) {
    return (
        <article
            data-seqmeta-detail-key={detailKey}
            className={cn(
                "rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]",
                className,
            )}
        >
            <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                    <p
                        data-testid="seqmeta-entity-title"
                        className="break-all text-sm leading-6 text-foreground"
                    >
                        {title}
                    </p>
                    <EntityMetadataPairs pairs={metadata} />
                </div>
                <div className="flex flex-wrap gap-2">
                    <button
                        type="button"
                        aria-label={copyAriaLabel}
                        className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                        onClick={onCopy}
                    >
                        <Copy className="size-3.5" aria-hidden="true" />
                        {copied ? "Copied" : "Copy"}
                    </button>
                    {filterHref && filterAriaLabel ? (
                        <Link
                            aria-label={filterAriaLabel}
                            className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                            href={filterHref}
                        >
                            <Search className="size-3.5" aria-hidden="true" />
                            Filter
                        </Link>
                    ) : null}
                    {children}
                </div>
            </div>
        </article>
    );
}

function DirectMetadataRow({ copied, field, onCopy }: DirectMetadataRowProps) {
    const href = detailFieldHref(field);
    const tooltip = `MLWH seqmeta key: ${field.key}`;

    return (
        <article
            data-seqmeta-detail-key={field.key}
            className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4 shadow-[0_18px_54px_-44px_rgba(48,67,98,0.55)]"
        >
            <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                    <p
                        data-testid="seqmeta-direct-metadata-label"
                        title={tooltip}
                        aria-label={`${field.label}. ${tooltip}`}
                        className="cursor-help text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground"
                    >
                        {field.label}
                    </p>
                </div>
                <div className="flex flex-wrap gap-2">
                    <button
                        type="button"
                        aria-label={`Copy ${field.key}`}
                        className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                        onClick={onCopy}
                    >
                        <Copy className="size-3.5" aria-hidden="true" />
                        {copied ? "Copied" : "Copy"}
                    </button>
                    {href ? (
                        <Link
                            aria-label={`Send ${field.key} to search filter`}
                            className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                            href={href}
                        >
                            <Search className="size-3.5" aria-hidden="true" />
                            Filter
                        </Link>
                    ) : null}
                </div>
            </div>
            <p className="mt-3 break-all text-sm leading-6 text-foreground">
                {field.value}
            </p>
        </article>
    );
}

function sampleCopyStateKey(sample: EnrichmentSample, index?: number): string {
    const identity =
        index === undefined
            ? [
                  asString(sample.sanger_id) ?? "",
                  asString(sample.id_sample_lims) ?? "",
              ].join("|")
            : librarySampleKey(sample, index);

    return copiedStateKey("seqmeta_sample_name", identity);
}

function sampleFilterValue(sample: EnrichmentSample): string | undefined {
    return (
        asString(sample.sample_name) ??
        asString(sample.sanger_id) ??
        asString(sample.id_sample_lims) ??
        undefined
    );
}

function humanizeMissingHop(missing: MissingHop): string {
    if (missing.hop === "samples" && missing.reason === "samples_truncated") {
        return "Showing first 1000 samples";
    }

    if (missing.hop === "study" && missing.reason === "upstream_error") {
        return "Study record unavailable";
    }

    if (missing.hop === "samples" && missing.reason === "upstream_error") {
        return "Sample details unavailable";
    }

    if (missing.hop === "libraries" && missing.reason === "upstream_error") {
        return "Library details unavailable";
    }

    return `${missing.hop.replace(/^./, (letter) => letter.toUpperCase())} details unavailable`;
}

function appendDetailField(
    fields: SeqmetaDetailField[],
    field: SeqmetaDetailField | null,
    rawValue?: string,
    metadataKey?: string,
) {
    if (!field) {
        return;
    }

    const value = field.value.trim();

    if (!value) {
        return;
    }

    const duplicate = fields.some(
        (entry) =>
            entry.key === field.key &&
            entry.value.toLowerCase() === value.toLowerCase(),
    );

    if (!duplicate) {
        if (
            rawValue &&
            field.group === "direct" &&
            value.toLowerCase() === rawValue.trim().toLowerCase() &&
            isSelectedTitleDetailField(metadataKey ?? "", field.key)
        ) {
            return;
        }

        const searchKey =
            field.group === "direct"
                ? (directDetailSearchKey(metadataKey ?? "", field.key) ??
                  field.searchKey)
                : field.searchKey;
        const searchValue =
            field.group === "direct" && searchKey !== field.searchKey
                ? undefined
                : field.searchValue;

        fields.push({ ...field, searchKey, searchValue, value });
    }
}

function isSelectedTitleDetailField(metadataKey: string, fieldKey: string) {
    const displayKey = seqmetaDisplayKey(metadataKey);

    return (
        fieldKey === displayKey ||
        (displayKey === "seqmeta_sample_name" &&
            fieldKey === "seqmeta_supplier_name")
    );
}

function buildDetailFields(
    metadataKey: string,
    rawValue: string,
    enrichment: EnrichmentResult | null,
): SeqmetaDetailField[] {
    const fields: SeqmetaDetailField[] = [];

    if (!enrichment) {
        return fields;
    }

    const libraryMetadata = isLibraryMetadataKey(metadataKey);
    const studyMetadata = isStudyMetadataKey(metadataKey);
    const runMetadata = isRunMetadataKey(metadataKey);
    const sampleMetadata = isSampleMetadataKey(metadataKey);

    // Study detail modals should never synthesize flat sample/library rows.
    // This avoids stale cached study responses rendering misleading legacy rows.
    const skipSampleFieldsForStudy = studyMetadata;

    if (!libraryMetadata) {
        appendDetailField(
            fields,
            enrichment.graph.study?.name
                ? {
                      key: "seqmeta_study_name",
                      label: "Study name",
                      value: enrichment.graph.study.name,
                      group: studyMetadata ? "direct" : "related",
                  }
                : null,
            rawValue,
            metadataKey,
        );
        appendDetailField(
            fields,
            enrichment.graph.study?.id_study_lims
                ? {
                      key: "seqmeta_id_study_lims",
                      label: "Study LIMS ID",
                      searchKey: "study",
                      value: enrichment.graph.study.id_study_lims,
                      group: studyMetadata ? "direct" : "related",
                  }
                : null,
            rawValue,
            metadataKey,
        );
        appendDetailField(
            fields,
            enrichment.graph.study?.accession_number
                ? {
                      key: "seqmeta_accession_number",
                      label: "Study accession",
                      value: enrichment.graph.study.accession_number,
                      group: studyMetadata ? "direct" : "related",
                  }
                : null,
            rawValue,
            metadataKey,
        );

        // Skip individual sample fields for study metadata.
        if (!skipSampleFieldsForStudy) {
            const sampleRecord =
                enrichment.graph.sample ??
                enrichment.graph.sample_detail?.sample;
            const sampleSearchValue = sampleRecord
                ? sampleFilterValue(sampleRecord)
                : undefined;

            appendDetailField(
                fields,
                sampleRecord?.sample_name
                    ? {
                          key: "seqmeta_sample_name",
                          label: "Sample name",
                          searchKey: "sample",
                          searchValue: sampleSearchValue,
                          value: sampleRecord.sample_name,
                          group: sampleMetadata ? "direct" : "related",
                      }
                    : null,
                rawValue,
                metadataKey,
            );
            appendDetailField(
                fields,
                sampleRecord?.sanger_id
                    ? {
                          key: "seqmeta_sanger_sample_id",
                          label: "Sanger sample ID",
                          searchKey: "sample",
                          searchValue: sampleSearchValue,
                          value: sampleRecord.sanger_id,
                          group: sampleMetadata ? "direct" : "related",
                      }
                    : null,
                rawValue,
                metadataKey,
            );
            appendDetailField(
                fields,
                sampleRecord?.supplier_name
                    ? {
                          key: "seqmeta_supplier_name",
                          label: "Sample supplier name",
                          searchKey: "sample",
                          value: sampleRecord.supplier_name,
                          group: sampleMetadata ? "direct" : "related",
                      }
                    : null,
                rawValue,
                metadataKey,
            );
            appendDetailField(
                fields,
                sampleRecord?.id_sample_lims
                    ? {
                          key: "seqmeta_id_sample_lims",
                          label: "Sample LIMS ID",
                          searchKey: "sample",
                          searchValue: sampleSearchValue,
                          value: sampleRecord.id_sample_lims,
                          group: sampleMetadata ? "direct" : "related",
                      }
                    : null,
                rawValue,
                metadataKey,
            );
            appendDetailField(
                fields,
                sampleRecord?.accession_number
                    ? {
                          key: "seqmeta_accession_number",
                          label: "Sample accession",
                          searchKey: "sample",
                          searchValue: sampleSearchValue,
                          value: sampleRecord.accession_number,
                          group: sampleMetadata ? "direct" : "related",
                      }
                    : null,
                rawValue,
                metadataKey,
            );
        }
    }

    if (libraryMetadata) {
        const library = bestLibraryForMetadata(
            metadataKey,
            rawValue,
            enrichment,
        );
        const rawLibraryValue = rawValue.trim().toLowerCase();
        const displayKey = seqmetaDisplayKey(metadataKey);
        const showExactLibraryFields =
            displayKey === "seqmeta_library_id" ||
            displayKey === "seqmeta_id_library_lims" ||
            library?.libraryId?.toLowerCase() === rawLibraryValue ||
            library?.idLibraryLims?.toLowerCase() === rawLibraryValue;

        appendDetailField(
            fields,
            library?.libraryType
                ? {
                      key: "seqmeta_pipeline_id_lims",
                      label: "Library type",
                      searchKey: "library",
                      value: library.libraryType,
                      group: "direct",
                  }
                : null,
            rawValue,
            metadataKey,
        );
        appendDetailField(
            fields,
            showExactLibraryFields && library?.libraryId
                ? {
                      key: "seqmeta_library_id",
                      label: "Library ID",
                      searchKey: "seqmeta_library_id",
                      value: library.libraryId,
                      group: "direct",
                  }
                : null,
            rawValue,
            metadataKey,
        );
        appendDetailField(
            fields,
            showExactLibraryFields && library?.idLibraryLims
                ? {
                      key: "seqmeta_id_library_lims",
                      label: "Library LIMS ID",
                      searchKey: "seqmeta_id_library_lims",
                      value: library.idLibraryLims,
                      group: "direct",
                  }
                : null,
            rawValue,
            metadataKey,
        );
    }

    // Skip library fields for study metadata.
    if (!libraryMetadata && !skipSampleFieldsForStudy && !runMetadata) {
        const libraryTypes = [
            enrichment.graph.library?.library_type,
            enrichment.graph.sample?.library_type,
            ...(enrichment.graph.libraries ?? []).map((library) =>
                asString(library.library_type),
            ),
        ].filter((value): value is string => Boolean(value));

        for (const libraryType of libraryTypes) {
            appendDetailField(
                fields,
                {
                    key: "seqmeta_pipeline_id_lims",
                    label: "Library type",
                    searchKey: "library",
                    value: libraryType,
                    group: "related",
                },
                rawValue,
                metadataKey,
            );
        }
    }

    return fields;
}

function samplesForLibrary(
    samples: EnrichmentSample[],
    library: HierarchicalLibrary,
): EnrichmentSample[] {
    return samples.filter((sample) => {
        if (sample.library_type !== library.libraryType) {
            return false;
        }

        return (
            !library.idStudyLims || sample.id_study_lims === library.idStudyLims
        );
    });
}

function runLibraryItems(enrichment: EnrichmentResult): HierarchicalLibrary[] {
    const libraryItems: HierarchicalLibrary[] = [];

    for (const detail of enrichment.graph.study_details ?? []) {
        for (const lib of detail.library_details) {
            libraryItems.push({
                libraryType: lib.library_type,
                idStudyLims: lib.id_study_lims,
                libraryId: lib.library_id,
                idLibraryLims: lib.id_library_lims,
                samples: lib.samples,
            });
        }
    }

    if (libraryItems.length > 0) {
        return libraryItems;
    }

    const samples = enrichment.graph.samples ?? [];
    for (const lib of enrichment.graph.libraries ?? []) {
        const item = {
            libraryType: lib.library_type,
            idStudyLims: lib.id_study_lims,
            libraryId: lib.library_id,
            idLibraryLims: lib.id_library_lims,
            samples: [],
        };

        libraryItems.push({
            ...item,
            samples: samplesForLibrary(samples, item),
        });
    }

    return libraryItems;
}

function buildHierarchicalGroups(
    metadataKey: string,
    enrichment: EnrichmentResult | null,
): HierarchicalGroup[] {
    if (!enrichment) {
        return [];
    }

    const groups: HierarchicalGroup[] = [];
    const studyMetadata = isStudyMetadataKey(metadataKey);
    const libraryMetadata = isLibraryMetadataKey(metadataKey);
    const runMetadata = isRunMetadataKey(metadataKey);
    const sampleMetadata = isSampleMetadataKey(metadataKey);
    const hasGroup = (type: HierarchicalGroup["type"]) =>
        groups.some((group) => group.type === type);

    // For study details with hierarchy, group libraries.
    // Prefer study_detail.library_details; fall back to graph.libraries for
    // enrichment results cached before study_detail was introduced.
    if (studyMetadata) {
        const libraryItems: HierarchicalLibrary[] = [];

        if (enrichment.graph.study_detail?.library_details?.length) {
            for (const lib of enrichment.graph.study_detail.library_details) {
                libraryItems.push({
                    libraryType: lib.library_type,
                    idStudyLims: lib.id_study_lims,
                    libraryId: lib.library_id,
                    idLibraryLims: lib.id_library_lims,
                    samples: [], // Empty - samples loaded JIT on expansion
                });
            }
        } else if (enrichment.graph.libraries?.length) {
            for (const lib of enrichment.graph.libraries) {
                libraryItems.push({
                    libraryType: lib.library_type,
                    idStudyLims: lib.id_study_lims,
                    libraryId: lib.library_id,
                    idLibraryLims: lib.id_library_lims,
                    samples: [], // Empty - samples loaded JIT on expansion
                });
            }
        }

        if (libraryItems.length > 0) {
            groups.push({
                type: "libraries",
                title: "Libraries",
                items: libraryItems,
            });
        }
    }

    if (runMetadata) {
        if (enrichment.graph.study) {
            groups.push({
                type: "study",
                title: "Study",
                items: [
                    {
                        name: enrichment.graph.study.name,
                        id: enrichment.graph.study.id_study_lims,
                        accession:
                            asString(enrichment.graph.study.accession_number) ??
                            undefined,
                    },
                ],
            });
        }

        if (enrichment.graph.sample) {
            groups.push({
                type: "samples",
                title: "Sample",
                items: [enrichment.graph.sample],
            });
        }

        const libraryItems = runLibraryItems(enrichment);

        if (libraryItems.length > 0) {
            groups.push({
                type: "libraries",
                title: "Libraries",
                items: libraryItems,
            });
        }
    }

    // For library details, show samples and parent study
    if (libraryMetadata) {
        // Collect all samples first (used for both study filtering and display)
        const allSamples = [
            ...(enrichment.graph.sample ? [enrichment.graph.sample] : []),
            ...(enrichment.graph.samples ?? []),
        ];

        // Show parent study/studies
        // Backend returns graph.studies (plural array) for library_type enrichment
        // globally, but we should only show studies that the returned samples belong to
        const allStudies = enrichment.graph.studies ?? [];

        // Get unique study IDs from samples
        const sampleStudyIds = new Set(
            allSamples
                .map((sample) => sample.id_study_lims)
                .filter((id): id is string => Boolean(id)),
        );

        // Filter studies to only those that samples belong to
        const studies =
            sampleStudyIds.size > 0
                ? allStudies.filter((study) =>
                      sampleStudyIds.has(study.id_study_lims),
                  )
                : allStudies;

        if (studies.length > 0) {
            groups.push({
                type: "study",
                title: "Study",
                items: studies.map((study) => ({
                    name: study.name,
                    id: study.id_study_lims,
                    accession: asString(study.accession_number) ?? undefined,
                })),
            });
        }

        // Show samples individually
        // Deduplicate samples by sanger_id + id_sample_lims
        const seenKeys = new Set<string>();
        const samples = allSamples.filter((sample) => {
            const key = `${sample.sanger_id}|${sample.id_sample_lims}`;

            if (seenKeys.has(key)) {
                return false;
            }

            seenKeys.add(key);
            return true;
        });

        if (samples.length > 0) {
            groups.push({
                type: "samples",
                title: "Samples",
                items: samples,
            });
        }
    }

    // For sample details with hierarchy, show library parent, study grandparent, and lanes
    if (sampleMetadata && enrichment.graph.sample_detail) {
        // Show parent library
        const library = sampleLibraryForGroups(enrichment);

        if (library) {
            groups.push({
                type: "library",
                title: "Library",
                items: [{ ...library, samples: [] }],
            });
        }

        // Show study (grandparent of the sample)
        if (enrichment.graph.study) {
            groups.push({
                type: "study",
                title: "Study",
                items: [
                    {
                        name: enrichment.graph.study.name,
                        id: enrichment.graph.study.id_study_lims,
                        accession:
                            asString(enrichment.graph.study.accession_number) ??
                            undefined,
                    },
                ],
            });
        }

        // Show lanes
        if (
            enrichment.graph.sample_detail.lanes &&
            enrichment.graph.sample_detail.lanes.length > 0
        ) {
            groups.push({
                type: "lanes",
                title: "Lanes",
                items: enrichment.graph.sample_detail.lanes,
            });
        }
    }

    if (
        !libraryMetadata &&
        !studyMetadata &&
        !hasGroup("library") &&
        !hasGroup("libraries")
    ) {
        const library = sampleLibraryForGroups(enrichment);

        if (library) {
            groups.push({
                type: "library",
                title: "Library",
                items: [{ ...library, samples: [] }],
            });
        }
    }

    if (
        !studyMetadata &&
        !libraryMetadata &&
        !hasGroup("study") &&
        enrichment.graph.study
    ) {
        groups.push({
            type: "study",
            title: "Study",
            items: [
                {
                    name: enrichment.graph.study.name,
                    id: enrichment.graph.study.id_study_lims,
                    accession:
                        asString(enrichment.graph.study.accession_number) ??
                        undefined,
                },
            ],
        });
    }

    if (
        !sampleMetadata &&
        !studyMetadata &&
        !hasGroup("samples") &&
        enrichment.graph.sample
    ) {
        groups.push({
            type: "samples",
            title: "Sample",
            items: [enrichment.graph.sample],
        });
    }

    return groups;
}

async function writeClipboard(value: string): Promise<boolean> {
    function fallbackCopyText(text: string): boolean {
        if (
            typeof document === "undefined" ||
            typeof document.execCommand !== "function"
        ) {
            return false;
        }

        const textarea = document.createElement("textarea");

        textarea.value = text;
        textarea.setAttribute("readonly", "");
        textarea.style.position = "fixed";
        textarea.style.opacity = "0";
        textarea.style.pointerEvents = "none";

        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();

        try {
            return document.execCommand("copy");
        } finally {
            document.body.removeChild(textarea);
        }
    }

    if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
        return fallbackCopyText(value);
    }

    try {
        await navigator.clipboard.writeText(value);
        return true;
    } catch {
        return fallbackCopyText(value);
    }
}

export function SeqmetaBadge({
    metadataKey,
    rawValue,
    enrichment,
    error,
    loading = false,
    statusPlacement = "inline",
    triggerLabel,
}: SeqmetaBadgeProps) {
    const inlineLabel = primaryLabel(rawValue, enrichment);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [copiedKey, setCopiedKey] = useState<string | null>(null);
    const [expandedLibraries, setExpandedLibraries] = useState<Set<string>>(
        new Set(),
    );
    const [loadedLibrarySamples, setLoadedLibrarySamples] = useState<
        Map<string, EnrichmentSample[]>
    >(new Map());
    const [loadingLibraries, setLoadingLibraries] = useState<Set<string>>(
        new Set(),
    );
    const titleDisplayKey = titleMetadataDisplayKey(
        metadataKey,
        rawValue,
        enrichment,
    );
    const triggerDisplayKey = triggerLabel ?? titleDisplayKey;
    const titleCopyKey = copiedStateKey(
        `title:${titleDisplayKey}`,
        inlineLabel,
    );
    const titleHref = titleFilterHref(metadataKey, rawValue);
    const detailFields = useMemo(
        () =>
            dialogOpen
                ? buildDetailFields(metadataKey, rawValue, enrichment)
                : [],
        [dialogOpen, enrichment, metadataKey, rawValue],
    );
    const hierarchicalGroups = useMemo(
        () =>
            dialogOpen ? buildHierarchicalGroups(metadataKey, enrichment) : [],
        [dialogOpen, enrichment, metadataKey],
    );
    const missingLines = useMemo(
        () =>
            dialogOpen && enrichment?.partial
                ? (enrichment.missing ?? []).map(humanizeMissingHop)
                : [],
        [dialogOpen, enrichment],
    );

    // Fetch library samples when a library is expanded
    useEffect(() => {
        const librariesGroup = hierarchicalGroups.find(
            (g) => g.type === "libraries",
        );
        if (!librariesGroup) {
            return;
        }

        const libraries = librariesGroup.items as HierarchicalLibrary[];
        const toLoad = libraries.filter((lib) => {
            const identity = libraryIdentityKey(lib);

            return (
                expandedLibraries.has(identity) &&
                lib.samples.length === 0 &&
                !loadedLibrarySamples.has(identity) &&
                !loadingLibraries.has(identity)
            );
        });

        if (toLoad.length === 0) {
            return;
        }

        // Async function to handle loading
        const loadSamples = async () => {
            // Mark libraries as loading
            setLoadingLibraries((prev) => {
                const next = new Set(prev);
                for (const lib of toLoad) {
                    next.add(libraryIdentityKey(lib));
                }
                return next;
            });

            // Fetch samples for each library
            await Promise.all(
                toLoad.map(async (lib) => {
                    try {
                        const filters = librarySampleFilters(lib);
                        const samples = filters
                            ? await fetchLibrarySamples(
                                  lib.idStudyLims,
                                  lib.libraryType,
                                  filters,
                              )
                            : await fetchLibrarySamples(
                                  lib.idStudyLims,
                                  lib.libraryType,
                              );
                        setLoadedLibrarySamples((prev) => {
                            const next = new Map(prev);
                            next.set(libraryIdentityKey(lib), samples ?? []);
                            return next;
                        });
                    } catch (error) {
                        console.error(
                            `Failed to load samples for library ${lib.libraryType}:`,
                            error,
                        );
                        setLoadedLibrarySamples((prev) => {
                            const next = new Map(prev);
                            next.set(libraryIdentityKey(lib), []); // Set empty on error
                            return next;
                        });
                    }
                }),
            );

            // Clear loading state
            setLoadingLibraries((prev) => {
                const next = new Set(prev);
                for (const lib of toLoad) {
                    next.delete(libraryIdentityKey(lib));
                }
                return next;
            });
        };

        void loadSamples();
    }, [
        expandedLibraries,
        hierarchicalGroups,
        loadedLibrarySamples,
        loadingLibraries,
    ]);

    useEffect(() => {
        if (!dialogOpen) {
            return undefined;
        }

        function handleKeyDown(event: KeyboardEvent) {
            if (event.key === "Escape") {
                setDialogOpen(false);
            }
        }

        window.addEventListener("keydown", handleKeyDown);

        return () => {
            window.removeEventListener("keydown", handleKeyDown);
        };
    }, [dialogOpen]);

    useEffect(() => {
        if (!copiedKey) {
            return undefined;
        }

        const timeout = window.setTimeout(() => {
            setCopiedKey(null);
        }, 1500);

        return () => {
            window.clearTimeout(timeout);
        };
    }, [copiedKey]);

    const status = loading ? (
        <span
            aria-label="loading enrichment"
            className={cn(
                "inline-flex h-5 w-5 items-center justify-center rounded-full border border-border/80 bg-background text-[11px] font-semibold text-muted-foreground",
                statusPlacement === "overlay" &&
                    "absolute -right-1.5 -top-1 h-2.5 w-2.5 text-[0] ring-2 ring-background",
            )}
        >
            ...
        </span>
    ) : error === "not_found" ? (
        <span
            aria-label="enrichment unavailable"
            className={cn(
                "inline-flex h-5 w-5 items-center justify-center rounded-full border border-border/80 bg-background text-[11px] font-semibold text-muted-foreground",
                statusPlacement === "overlay" &&
                    "absolute -right-1.5 -top-1 h-2.5 w-2.5 text-[0] ring-2 ring-background",
            )}
        >
            ?
        </span>
    ) : error === "upstream_impaired" ? (
        <span
            aria-label="enrichment backend impaired"
            className={cn(
                "inline-flex h-5 w-5 items-center justify-center rounded-full border border-amber-500/40 bg-amber-500/10 text-[11px] font-semibold text-amber-700",
                statusPlacement === "overlay" &&
                    "absolute -right-1.5 -top-1 h-2.5 w-2.5 text-[0] ring-2 ring-background",
            )}
        >
            !
        </span>
    ) : null;

    return (
        <>
            <span
                className={cn(
                    "inline-flex max-w-full items-center gap-2 align-middle",
                    statusPlacement === "overlay" && "relative",
                )}
            >
                <button
                    type="button"
                    aria-expanded={dialogOpen}
                    aria-haspopup="dialog"
                    aria-label={`Open ${triggerDisplayKey} details`}
                    data-testid="seqmeta-badge-trigger"
                    className={cn(
                        "inline-flex max-w-full cursor-pointer items-center rounded-full border border-border/80 px-3 py-1 text-left text-xs font-medium tracking-[0.16em] transition hover:border-primary/45 hover:bg-accent/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
                        enrichment
                            ? "bg-accent/20 text-foreground"
                            : "bg-background/80 text-muted-foreground",
                    )}
                    onClick={() => setDialogOpen(true)}
                >
                    <span
                        data-testid="seqmeta-badge-label"
                        className="truncate"
                    >
                        {inlineLabel}
                    </span>
                </button>

                {status}
            </span>

            {dialogOpen ? (
                <div
                    aria-labelledby={`seqmeta-dialog-title-${metadataKey}`}
                    aria-modal="true"
                    className="fixed inset-0 z-50 flex items-center justify-center p-4 sm:p-6"
                    role="dialog"
                >
                    <button
                        type="button"
                        aria-label="Close seqmeta details backdrop"
                        className="absolute inset-0 bg-[color:rgba(15,23,42,0.64)] backdrop-blur-sm"
                        onClick={() => setDialogOpen(false)}
                    />
                    <section className="relative z-10 flex max-h-[calc(100vh-2rem)] w-full max-w-4xl flex-col overflow-hidden rounded-[2rem] border border-border/80 bg-[linear-gradient(145deg,color-mix(in_oklab,var(--card)_88%,white_12%),color-mix(in_oklab,var(--accent)_14%,var(--card)_86%))] shadow-[0_36px_140px_-72px_rgba(20,31,49,0.9)] sm:max-h-[calc(100vh-3rem)]">
                        <div className="flex items-start justify-between gap-4 border-b border-border/70 px-6 py-5 sm:px-7">
                            <div className="min-w-0 flex-1 space-y-2">
                                <p className="text-xs font-semibold uppercase tracking-[0.28em] text-muted-foreground">
                                    Seqmeta details
                                </p>
                                <div className="flex flex-wrap items-center gap-3">
                                    <h3
                                        id={`seqmeta-dialog-title-${metadataKey}`}
                                        className="break-all text-2xl font-semibold tracking-tight text-foreground"
                                    >
                                        {inlineLabel}
                                    </h3>
                                    <div
                                        data-testid="seqmeta-title-actions"
                                        className="flex flex-wrap gap-2"
                                    >
                                        <button
                                            type="button"
                                            aria-label={`Copy ${titleDisplayKey}`}
                                            className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                            onClick={() => {
                                                void writeClipboard(
                                                    inlineLabel,
                                                ).then((copied) => {
                                                    if (copied) {
                                                        setCopiedKey(
                                                            titleCopyKey,
                                                        );
                                                    }
                                                });
                                            }}
                                        >
                                            <Copy
                                                className="size-3.5"
                                                aria-hidden="true"
                                            />
                                            {copiedKey === titleCopyKey
                                                ? "Copied"
                                                : "Copy"}
                                        </button>
                                        {titleHref ? (
                                            <Link
                                                aria-label={`Send ${titleDisplayKey} to search filter`}
                                                className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                href={titleHref}
                                            >
                                                <Search
                                                    className="size-3.5"
                                                    aria-hidden="true"
                                                />
                                                Filter
                                            </Link>
                                        ) : null}
                                    </div>
                                </div>
                                <p className="font-mono text-xs text-muted-foreground">
                                    {titleDisplayKey}
                                </p>
                            </div>
                            <button
                                type="button"
                                aria-label="Close seqmeta details"
                                className="inline-flex size-10 items-center justify-center rounded-full border border-border/70 bg-background/80 text-foreground transition hover:border-primary/35 hover:bg-accent/25"
                                onClick={() => setDialogOpen(false)}
                            >
                                <X className="size-4" aria-hidden="true" />
                            </button>
                        </div>

                        <div
                            data-testid="seqmeta-dialog-body"
                            className="max-h-[calc(100vh-12rem)] min-h-0 overflow-y-auto px-6 py-6 sm:px-7"
                        >
                            <div className="space-y-6">
                                {loading ||
                                error ||
                                (enrichment?.partial &&
                                    missingLines.length > 0) ? (
                                    <div className="rounded-[1.35rem] border border-border/70 bg-background/72 px-4 py-4">
                                        {loading ? (
                                            <p className="text-sm text-foreground">
                                                Looking up{" "}
                                                {metadataLabelForSentence(
                                                    metadataKey,
                                                )}
                                                .
                                            </p>
                                        ) : null}
                                        {error === "not_found" ? (
                                            <p className="text-sm text-foreground">
                                                No enrichment matched this{" "}
                                                {metadataLabelForSentence(
                                                    metadataKey,
                                                )}{" "}
                                                value.
                                            </p>
                                        ) : null}
                                        {error === "upstream_impaired" ? (
                                            <p className="text-sm text-foreground">
                                                Upstream services were
                                                unavailable while resolving this{" "}
                                                {metadataLabelForSentence(
                                                    metadataKey,
                                                )}{" "}
                                                value.
                                            </p>
                                        ) : null}
                                        {enrichment?.partial &&
                                        missingLines.length > 0 ? (
                                            <div>
                                                <p className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                    Partial response
                                                </p>
                                                <div className="mt-2 space-y-2 text-sm leading-6 text-foreground">
                                                    {missingLines.map(
                                                        (line) => (
                                                            <p key={line}>
                                                                {line}
                                                            </p>
                                                        ),
                                                    )}
                                                </div>
                                            </div>
                                        ) : null}
                                    </div>
                                ) : null}
                                {hierarchicalGroups.length > 0 ? (
                                    <div className="space-y-6">
                                        {/* Show direct metadata fields first for sample metadata with hierarchical groups */}
                                        {(() => {
                                            const directFields =
                                                detailFields.filter(
                                                    (field) =>
                                                        field.group ===
                                                        "direct",
                                                );

                                            if (directFields.length === 0) {
                                                return null;
                                            }

                                            return (
                                                <div data-field-group="direct-metadata">
                                                    <h4 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                        Direct Metadata
                                                    </h4>
                                                    <div className="space-y-3">
                                                        {directFields.map(
                                                            (field) => {
                                                                return (
                                                                    <DirectMetadataRow
                                                                        key={`${field.key}:${field.value}`}
                                                                        copied={
                                                                            copiedKey ===
                                                                            field.key
                                                                        }
                                                                        field={
                                                                            field
                                                                        }
                                                                        onCopy={() => {
                                                                            void writeClipboard(
                                                                                field.value,
                                                                            ).then(
                                                                                (
                                                                                    copied,
                                                                                ) => {
                                                                                    if (
                                                                                        copied
                                                                                    ) {
                                                                                        setCopiedKey(
                                                                                            field.key,
                                                                                        );
                                                                                    }
                                                                                },
                                                                            );
                                                                        }}
                                                                    />
                                                                );
                                                            },
                                                        )}
                                                    </div>
                                                </div>
                                            );
                                        })()}
                                        {/* Show hierarchical groups for related data */}
                                        <div data-field-group="related-data">
                                            <h4 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                Related Data
                                            </h4>
                                            <div className="space-y-6">
                                                {hierarchicalGroups.map(
                                                    (group) => {
                                                        if (
                                                            group.type ===
                                                            "libraries"
                                                        ) {
                                                            const libraries =
                                                                group.items as HierarchicalLibrary[];

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="libraries"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {libraries.map(
                                                                            (
                                                                                library,
                                                                                index,
                                                                            ) => {
                                                                                const libraryIdentity =
                                                                                    libraryIdentityKey(
                                                                                        library,
                                                                                    );
                                                                                const libraryLabel =
                                                                                    libraryDisplayLabel(
                                                                                        library,
                                                                                    );
                                                                                const libraryDisplay =
                                                                                    libraryEntityDisplay(
                                                                                        library,
                                                                                    );
                                                                                const isExpanded =
                                                                                    expandedLibraries.has(
                                                                                        libraryIdentity,
                                                                                    );
                                                                                const libraryDetailKey =
                                                                                    directLibraryMetadataKey(
                                                                                        metadataKey,
                                                                                    );
                                                                                const libraryCopyKey =
                                                                                    copiedStateKey(
                                                                                        libraryDetailKey,
                                                                                        libraryIdentity,
                                                                                    );
                                                                                const isLoading =
                                                                                    loadingLibraries.has(
                                                                                        libraryIdentity,
                                                                                    );
                                                                                const loadedSamples =
                                                                                    loadedLibrarySamples.get(
                                                                                        libraryIdentity,
                                                                                    ) ??
                                                                                    library.samples;
                                                                                const visibleLoadedSamples =
                                                                                    visibleRelatedSamples(
                                                                                        loadedSamples,
                                                                                    );
                                                                                const loadedSamplesSummary =
                                                                                    relatedSamplesSummary(
                                                                                        loadedSamples,
                                                                                    );

                                                                                return (
                                                                                    <div
                                                                                        key={`${libraryIdentity}-${index}`}
                                                                                        className="space-y-2"
                                                                                    >
                                                                                        <RelatedEntityRow
                                                                                            copied={
                                                                                                copiedKey ===
                                                                                                libraryCopyKey
                                                                                            }
                                                                                            copyAriaLabel={`Copy ${libraryDetailKey}`}
                                                                                            detailKey={
                                                                                                libraryDetailKey
                                                                                            }
                                                                                            filterAriaLabel="Send library to search filter"
                                                                                            filterHref={libraryFilterHref(
                                                                                                library,
                                                                                            )}
                                                                                            metadata={
                                                                                                libraryDisplay.metadata
                                                                                            }
                                                                                            onCopy={() => {
                                                                                                void writeClipboard(
                                                                                                    libraryLabel,
                                                                                                ).then(
                                                                                                    (
                                                                                                        copied,
                                                                                                    ) => {
                                                                                                        if (
                                                                                                            copied
                                                                                                        ) {
                                                                                                            setCopiedKey(
                                                                                                                libraryCopyKey,
                                                                                                            );
                                                                                                        }
                                                                                                    },
                                                                                                );
                                                                                            }}
                                                                                            title={
                                                                                                libraryDisplay.title
                                                                                            }
                                                                                        >
                                                                                            <button
                                                                                                type="button"
                                                                                                aria-label={
                                                                                                    isExpanded
                                                                                                        ? "Hide samples"
                                                                                                        : "Show samples"
                                                                                                }
                                                                                                className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-card/85 px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/20"
                                                                                                disabled={
                                                                                                    isLoading
                                                                                                }
                                                                                                onClick={() => {
                                                                                                    const newExpanded =
                                                                                                        new Set(
                                                                                                            expandedLibraries,
                                                                                                        );

                                                                                                    if (
                                                                                                        isExpanded
                                                                                                    ) {
                                                                                                        newExpanded.delete(
                                                                                                            libraryIdentity,
                                                                                                        );
                                                                                                    } else {
                                                                                                        newExpanded.add(
                                                                                                            libraryIdentity,
                                                                                                        );
                                                                                                    }

                                                                                                    setExpandedLibraries(
                                                                                                        newExpanded,
                                                                                                    );
                                                                                                }}
                                                                                            >
                                                                                                {isLoading ? (
                                                                                                    <Loader2
                                                                                                        className="size-3.5 animate-spin"
                                                                                                        aria-hidden="true"
                                                                                                    />
                                                                                                ) : isExpanded ? (
                                                                                                    <ChevronDown
                                                                                                        className="size-3.5"
                                                                                                        aria-hidden="true"
                                                                                                    />
                                                                                                ) : (
                                                                                                    <ChevronRight
                                                                                                        className="size-3.5"
                                                                                                        aria-hidden="true"
                                                                                                    />
                                                                                                )}
                                                                                                {isExpanded &&
                                                                                                !isLoading ? (
                                                                                                    <>
                                                                                                        {
                                                                                                            loadedSamples.length
                                                                                                        }{" "}
                                                                                                        sample
                                                                                                        {loadedSamples.length !==
                                                                                                        1
                                                                                                            ? "s"
                                                                                                            : ""}
                                                                                                    </>
                                                                                                ) : isLoading ? (
                                                                                                    "Loading..."
                                                                                                ) : (
                                                                                                    "Samples"
                                                                                                )}
                                                                                            </button>
                                                                                        </RelatedEntityRow>
                                                                                        {isExpanded &&
                                                                                        !isLoading &&
                                                                                        loadedSamples.length >
                                                                                            0 ? (
                                                                                            <div className="ml-4 space-y-2 border-l-2 border-border/40 pl-4">
                                                                                                {visibleLoadedSamples.map(
                                                                                                    (
                                                                                                        sample,
                                                                                                        index,
                                                                                                    ) => {
                                                                                                        const sampleDisplay =
                                                                                                            sampleEntityDisplay(
                                                                                                                sample,
                                                                                                            );
                                                                                                        const sampleCopyKey =
                                                                                                            sampleCopyStateKey(
                                                                                                                sample,
                                                                                                                index,
                                                                                                            );

                                                                                                        return (
                                                                                                            <RelatedEntityRow
                                                                                                                key={librarySampleKey(
                                                                                                                    sample,
                                                                                                                    index,
                                                                                                                )}
                                                                                                                className="py-3"
                                                                                                                copied={
                                                                                                                    copiedKey ===
                                                                                                                    sampleCopyKey
                                                                                                                }
                                                                                                                copyAriaLabel="Copy seqmeta_sample_name"
                                                                                                                detailKey="sample"
                                                                                                                filterAriaLabel={
                                                                                                                    sample.sanger_id
                                                                                                                        ? "Send sample to search filter"
                                                                                                                        : undefined
                                                                                                                }
                                                                                                                filterHref={
                                                                                                                    sample.sanger_id
                                                                                                                        ? `/?sample=${sample.sanger_id}`
                                                                                                                        : undefined
                                                                                                                }
                                                                                                                metadata={
                                                                                                                    sampleDisplay.metadata
                                                                                                                }
                                                                                                                onCopy={() => {
                                                                                                                    void writeClipboard(
                                                                                                                        sample.sanger_id,
                                                                                                                    ).then(
                                                                                                                        (
                                                                                                                            copied,
                                                                                                                        ) => {
                                                                                                                            if (
                                                                                                                                copied
                                                                                                                            ) {
                                                                                                                                setCopiedKey(
                                                                                                                                    sampleCopyKey,
                                                                                                                                );
                                                                                                                            }
                                                                                                                        },
                                                                                                                    );
                                                                                                                }}
                                                                                                                title={
                                                                                                                    sampleDisplay.title
                                                                                                                }
                                                                                                            ></RelatedEntityRow>
                                                                                                        );
                                                                                                    },
                                                                                                )}
                                                                                                {loadedSamplesSummary ? (
                                                                                                    <p className="px-1 text-xs font-medium text-muted-foreground">
                                                                                                        {
                                                                                                            loadedSamplesSummary
                                                                                                        }
                                                                                                    </p>
                                                                                                ) : null}
                                                                                            </div>
                                                                                        ) : null}
                                                                                    </div>
                                                                                );
                                                                            },
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        if (
                                                            group.type ===
                                                            "study"
                                                        ) {
                                                            const studies =
                                                                group.items as {
                                                                    name: string;
                                                                    id: string;
                                                                    accession?: string;
                                                                }[];

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="study"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {studies.map(
                                                                            (
                                                                                study,
                                                                            ) => {
                                                                                const studyDisplay =
                                                                                    studyEntityDisplay(
                                                                                        study,
                                                                                    );
                                                                                const studyCopyValue =
                                                                                    asString(
                                                                                        study.name,
                                                                                    ) ??
                                                                                    studyDisplay.title;
                                                                                const studyCopyKey =
                                                                                    copiedStateKey(
                                                                                        "seqmeta_id_study_lims",
                                                                                        study.id,
                                                                                    );

                                                                                return (
                                                                                    <RelatedEntityRow
                                                                                        key={
                                                                                            study.id
                                                                                        }
                                                                                        copied={
                                                                                            copiedKey ===
                                                                                            studyCopyKey
                                                                                        }
                                                                                        copyAriaLabel="Copy seqmeta_id_study_lims"
                                                                                        detailKey="study"
                                                                                        filterAriaLabel="Send study to search filter"
                                                                                        filterHref={`/?study=${study.id}`}
                                                                                        metadata={
                                                                                            studyDisplay.metadata
                                                                                        }
                                                                                        onCopy={() => {
                                                                                            void writeClipboard(
                                                                                                studyCopyValue,
                                                                                            ).then(
                                                                                                (
                                                                                                    copied,
                                                                                                ) => {
                                                                                                    if (
                                                                                                        copied
                                                                                                    ) {
                                                                                                        setCopiedKey(
                                                                                                            studyCopyKey,
                                                                                                        );
                                                                                                    }
                                                                                                },
                                                                                            );
                                                                                        }}
                                                                                        title={
                                                                                            studyDisplay.title
                                                                                        }
                                                                                    ></RelatedEntityRow>
                                                                                );
                                                                            },
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        if (
                                                            group.type ===
                                                            "samples"
                                                        ) {
                                                            const samples =
                                                                group.items as EnrichmentSample[];
                                                            const visibleSamples =
                                                                visibleRelatedSamples(
                                                                    samples,
                                                                );
                                                            const samplesSummary =
                                                                relatedSamplesSummary(
                                                                    samples,
                                                                );

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="samples"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {visibleSamples.map(
                                                                            (
                                                                                sample,
                                                                            ) => {
                                                                                const sampleCopyKey =
                                                                                    sampleCopyStateKey(
                                                                                        sample,
                                                                                    );
                                                                                const sampleDisplay =
                                                                                    sampleEntityDisplay(
                                                                                        sample,
                                                                                    );

                                                                                return (
                                                                                    <RelatedEntityRow
                                                                                        key={`${sample.sanger_id}|${sample.id_sample_lims}`}
                                                                                        copied={
                                                                                            copiedKey ===
                                                                                            sampleCopyKey
                                                                                        }
                                                                                        copyAriaLabel="Copy seqmeta_sample_name"
                                                                                        detailKey="sample"
                                                                                        filterAriaLabel={
                                                                                            sample.sanger_id
                                                                                                ? "Send sample to search filter"
                                                                                                : undefined
                                                                                        }
                                                                                        filterHref={
                                                                                            sample.sanger_id
                                                                                                ? `/?sample=${sample.sanger_id}`
                                                                                                : undefined
                                                                                        }
                                                                                        metadata={
                                                                                            sampleDisplay.metadata
                                                                                        }
                                                                                        onCopy={() => {
                                                                                            void writeClipboard(
                                                                                                sample.sanger_id,
                                                                                            ).then(
                                                                                                (
                                                                                                    copied,
                                                                                                ) => {
                                                                                                    if (
                                                                                                        copied
                                                                                                    ) {
                                                                                                        setCopiedKey(
                                                                                                            sampleCopyKey,
                                                                                                        );
                                                                                                    }
                                                                                                },
                                                                                            );
                                                                                        }}
                                                                                        title={
                                                                                            sampleDisplay.title
                                                                                        }
                                                                                    ></RelatedEntityRow>
                                                                                );
                                                                            },
                                                                        )}
                                                                        {samplesSummary ? (
                                                                            <p className="px-1 text-xs font-medium text-muted-foreground">
                                                                                {
                                                                                    samplesSummary
                                                                                }
                                                                            </p>
                                                                        ) : null}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        if (
                                                            group.type ===
                                                            "library"
                                                        ) {
                                                            const libraries =
                                                                group.items as HierarchicalLibrary[];

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="library"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {libraries.map(
                                                                            (
                                                                                library,
                                                                                index,
                                                                            ) => {
                                                                                const libraryLabel =
                                                                                    libraryDisplayLabel(
                                                                                        library,
                                                                                    );
                                                                                const libraryDisplay =
                                                                                    libraryEntityDisplay(
                                                                                        library,
                                                                                    );
                                                                                const libraryCopyKey =
                                                                                    copiedStateKey(
                                                                                        directLibraryMetadataKey(
                                                                                            metadataKey,
                                                                                        ),
                                                                                        libraryIdentityKey(
                                                                                            library,
                                                                                        ),
                                                                                    );

                                                                                return (
                                                                                    <RelatedEntityRow
                                                                                        key={`${libraryIdentityKey(library)}-${index}`}
                                                                                        copied={
                                                                                            copiedKey ===
                                                                                            libraryCopyKey
                                                                                        }
                                                                                        copyAriaLabel={`Copy ${directLibraryMetadataKey(metadataKey)}`}
                                                                                        detailKey="library"
                                                                                        filterAriaLabel="Send library to search filter"
                                                                                        filterHref={libraryFilterHref(
                                                                                            library,
                                                                                        )}
                                                                                        metadata={
                                                                                            libraryDisplay.metadata
                                                                                        }
                                                                                        onCopy={() => {
                                                                                            void writeClipboard(
                                                                                                libraryLabel,
                                                                                            ).then(
                                                                                                (
                                                                                                    copied,
                                                                                                ) => {
                                                                                                    if (
                                                                                                        copied
                                                                                                    ) {
                                                                                                        setCopiedKey(
                                                                                                            libraryCopyKey,
                                                                                                        );
                                                                                                    }
                                                                                                },
                                                                                            );
                                                                                        }}
                                                                                        title={
                                                                                            libraryDisplay.title
                                                                                        }
                                                                                    ></RelatedEntityRow>
                                                                                );
                                                                            },
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        if (
                                                            group.type ===
                                                            "lanes"
                                                        ) {
                                                            const lanes =
                                                                group.items as {
                                                                    id_run: string;
                                                                    lane: string;
                                                                    tag_index: number;
                                                                }[];

                                                            return (
                                                                <div
                                                                    key={
                                                                        group.title
                                                                    }
                                                                    data-field-group="lanes"
                                                                >
                                                                    <h5 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                                        {
                                                                            group.title
                                                                        }
                                                                    </h5>
                                                                    <div className="space-y-3">
                                                                        {lanes.map(
                                                                            (
                                                                                lane,
                                                                                index,
                                                                            ) => {
                                                                                const laneId =
                                                                                    laneDetailId(
                                                                                        lane,
                                                                                    );
                                                                                const laneDisplay =
                                                                                    laneEntityDisplay(
                                                                                        lane,
                                                                                    );

                                                                                return (
                                                                                    <RelatedEntityRow
                                                                                        key={`${laneId}-${index}`}
                                                                                        copied={
                                                                                            copiedKey ===
                                                                                            laneId
                                                                                        }
                                                                                        copyAriaLabel="Copy lane ID"
                                                                                        detailKey="lane"
                                                                                        filterAriaLabel="Send lane to search filter"
                                                                                        filterHref={`/?seqmeta_lane=${laneId}`}
                                                                                        metadata={
                                                                                            laneDisplay.metadata
                                                                                        }
                                                                                        onCopy={() => {
                                                                                            void writeClipboard(
                                                                                                laneId,
                                                                                            ).then(
                                                                                                (
                                                                                                    copied,
                                                                                                ) => {
                                                                                                    if (
                                                                                                        copied
                                                                                                    ) {
                                                                                                        setCopiedKey(
                                                                                                            laneId,
                                                                                                        );
                                                                                                    }
                                                                                                },
                                                                                            );
                                                                                        }}
                                                                                        title={
                                                                                            laneDisplay.title
                                                                                        }
                                                                                    ></RelatedEntityRow>
                                                                                );
                                                                            },
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            );
                                                        }

                                                        return null;
                                                    },
                                                )}
                                            </div>
                                        </div>
                                    </div>
                                ) : (
                                    <>
                                        {(() => {
                                            const directFields =
                                                detailFields.filter(
                                                    (field) =>
                                                        field.group ===
                                                        "direct",
                                                );
                                            const relatedFields =
                                                detailFields.filter(
                                                    (field) =>
                                                        field.group ===
                                                        "related",
                                                );

                                            const renderFieldGroup = (
                                                fields: SeqmetaDetailField[],
                                                title: string,
                                            ) => {
                                                if (fields.length === 0) {
                                                    return null;
                                                }

                                                return (
                                                    <div
                                                        data-field-group={title
                                                            .toLowerCase()
                                                            .replace(
                                                                /\s+/g,
                                                                "-",
                                                            )}
                                                    >
                                                        <h4 className="mb-3 text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                                            {title}
                                                        </h4>
                                                        <div className="space-y-3">
                                                            {fields.map(
                                                                (field) => {
                                                                    return (
                                                                        <DirectMetadataRow
                                                                            key={`${field.key}:${field.value}`}
                                                                            copied={
                                                                                copiedKey ===
                                                                                field.key
                                                                            }
                                                                            field={
                                                                                field
                                                                            }
                                                                            onCopy={() => {
                                                                                void writeClipboard(
                                                                                    field.value,
                                                                                ).then(
                                                                                    (
                                                                                        copied,
                                                                                    ) => {
                                                                                        if (
                                                                                            copied
                                                                                        ) {
                                                                                            setCopiedKey(
                                                                                                field.key,
                                                                                            );
                                                                                        }
                                                                                    },
                                                                                );
                                                                            }}
                                                                        />
                                                                    );
                                                                },
                                                            )}
                                                        </div>
                                                    </div>
                                                );
                                            };

                                            return (
                                                <>
                                                    {renderFieldGroup(
                                                        directFields,
                                                        "Direct Metadata",
                                                    )}
                                                    {renderFieldGroup(
                                                        relatedFields,
                                                        "Related Data",
                                                    )}
                                                </>
                                            );
                                        })()}
                                    </>
                                )}
                            </div>
                        </div>
                    </section>
                </div>
            ) : null}
        </>
    );
}
