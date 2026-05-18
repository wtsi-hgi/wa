import Link from "next/link";

import { ChevronLeft } from "lucide-react";

import { fetchFiles, fetchResult } from "@/app/(results)/actions";
import { ResultIdCopyChip } from "@/app/(results)/results/[id]/result-id-copy-chip";
import { ResultDetailFiles } from "@/components/result-detail-files";
import { ResultMetadataEnrichment } from "@/components/result-metadata-enrichment";
import { ResultRegistrationSummary } from "@/components/result-registration-summary";
import type { FileEntry, ResultSet } from "@/lib/contracts";
import { formatRegistrationUnique } from "@/lib/result-identity";
import { formatBytes } from "@/lib/utils";

type DetailPageParams = {
    id: string;
};

type DetailPageSearchParams = {
    returnTo?: string | string[];
};

export type ResultDetailPageProps = {
    params: Promise<DetailPageParams>;
    searchParams?: Promise<DetailPageSearchParams>;
};

export type ResultDetailPageContentProps = {
    id: string;
    searchParams?: DetailPageSearchParams;
};

export async function resolveResultDetailPageProps({
    params,
    searchParams,
}: ResultDetailPageProps): Promise<ResultDetailPageContentProps> {
    const { id } = await params;

    return {
        id,
        searchParams: (await searchParams) ?? {},
    };
}

function formatTimestamp(value: string): string {
    return new Date(value).toLocaleString("en-GB", {
        year: "numeric",
        month: "short",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        timeZone: "UTC",
    });
}

type FileSummary = {
    count: number;
    label: string;
    size: number;
};

function summarizeFiles(files: FileEntry[]): {
    categories: FileSummary[];
    total: FileSummary;
} {
    const categories: Array<{
        count: number;
        key: FileEntry["kind"];
        label: string;
        size: number;
    }> = [
        { key: "output", label: "Output", count: 0, size: 0 },
        { key: "input", label: "Input", count: 0, size: 0 },
        { key: "pipeline", label: "Pipeline", count: 0, size: 0 },
    ];

    for (const file of files) {
        const category = categories.find((entry) => entry.key === file.kind);

        if (!category) {
            continue;
        }

        category.count += 1;
        category.size += file.size;
    }

    return {
        total: {
            label: "Total",
            count: files.length,
            size: files.reduce((total, file) => total + file.size, 0),
        },
        categories: categories.map(({ count, label, size }) => ({
            count,
            label,
            size,
        })),
    };
}

function formatFileCount(count: number): string {
    return `${count} file${count === 1 ? "" : "s"}`;
}

function detailFields(result: ResultSet) {
    return [
        { label: "Pipeline version", value: result.pipeline_version },
        {
            label: "Pipeline identifier",
            value: result.pipeline_identifier,
            mono: true,
        },
        {
            label: "Unique",
            value: formatRegistrationUnique(result.run_key),
            mono: true,
        },
        { label: "Requester", value: result.requester },
        { label: "Operator", value: result.operator },
        {
            label: "Output directory",
            value: result.output_directory,
            mono: true,
            wide: true,
        },
        { label: "Registered", value: formatTimestamp(result.created_at) },
        { label: "Last updated", value: formatTimestamp(result.updated_at) },
        { label: "Command", value: result.command, mono: true, wide: true },
    ];
}

function resolveReturnHref(searchParams: DetailPageSearchParams): string {
    const returnTo = Array.isArray(searchParams.returnTo)
        ? searchParams.returnTo[0]
        : searchParams.returnTo;

    if (!returnTo || !returnTo.startsWith("/") || returnTo.startsWith("//")) {
        return "/";
    }

    return returnTo;
}

export function ResultDetailLoadingFallback() {
    return (
        <main className="mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-8 px-6 py-8 sm:px-10 lg:px-12 lg:py-10">
            <section className="overflow-hidden rounded-[2rem] border border-border/70 bg-[linear-gradient(135deg,color-mix(in_oklab,var(--card)_88%,white_12%),color-mix(in_oklab,var(--accent)_12%,var(--card)_88%))] shadow-[0_36px_120px_-72px_rgba(41,58,85,0.85)]">
                <div className="grid gap-8 px-6 py-8 sm:px-8 lg:px-10 lg:py-10">
                    <div className="space-y-4">
                        <p className="text-sm font-semibold uppercase tracking-[0.32em] text-muted-foreground">
                            Result detail
                        </p>
                        <div className="space-y-3">
                            <h1 className="text-4xl font-semibold tracking-tight text-balance sm:text-5xl">
                                Loading result details
                            </h1>
                            <p className="max-w-2xl text-sm leading-6 text-muted-foreground sm:text-base">
                                Preparing registered files and metadata.
                            </p>
                        </div>
                    </div>
                </div>
            </section>
        </main>
    );
}

export async function ResultDetailPageContent({
    id,
    searchParams,
}: ResultDetailPageContentProps) {
    const returnHref = resolveReturnHref(searchParams ?? {});
    const returnLabel =
        returnHref === "/" ? "Back to dashboard" : "Back to search results";
    const [result, files] = await Promise.all([
        fetchResult(id),
        fetchFiles(id),
    ]);
    const fileSummary = summarizeFiles(files);

    return (
        <main className="mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-6 px-6 py-8 sm:px-10 lg:px-12 lg:py-10">
            <section
                className="overflow-hidden rounded-[1.5rem] border border-border/70 bg-[linear-gradient(135deg,color-mix(in_oklab,var(--card)_90%,white_10%),color-mix(in_oklab,var(--accent)_10%,var(--card)_90%))] shadow-[0_28px_90px_-64px_rgba(41,58,85,0.8)]"
                data-result-detail-summary="true"
            >
                <div className="grid gap-5 px-6 py-6 sm:px-8 lg:grid-cols-[minmax(18rem,0.8fr)_minmax(0,1.6fr)] lg:px-10 lg:py-8">
                    <div className="space-y-4">
                        <div className="space-y-4">
                            <Link
                                href={returnHref}
                                className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/85 px-4 py-2 text-sm text-muted-foreground transition hover:text-foreground"
                                data-return-link="true"
                            >
                                <ChevronLeft
                                    className="h-4 w-4"
                                    aria-hidden="true"
                                />
                                <span>{returnLabel}</span>
                            </Link>
                            <p className="text-sm font-semibold uppercase tracking-[0.32em] text-muted-foreground">
                                Result detail
                            </p>
                            <div className="space-y-3">
                                <h1 className="text-4xl font-semibold tracking-tight text-balance sm:text-5xl">
                                    {result.pipeline_name}
                                </h1>
                            </div>
                            <ResultIdCopyChip resultId={result.id} />
                        </div>

                        <section
                            className="rounded-lg border border-border/70 bg-background/70 p-4"
                            data-file-summary="true"
                        >
                            <div className="flex items-center justify-between gap-3">
                                <p className="text-sm font-semibold text-foreground">
                                    Registered files
                                </p>
                                <p className="text-xs text-muted-foreground">
                                    {formatBytes(fileSummary.total.size)}
                                </p>
                            </div>
                            <dl className="mt-3 grid gap-2 sm:grid-cols-2">
                                <div className="rounded-lg border border-border/60 bg-card/75 px-3 py-2 sm:col-span-2">
                                    <dt className="text-xs text-muted-foreground">
                                        {fileSummary.total.label}
                                    </dt>
                                    <dd className="mt-1 flex items-baseline justify-between gap-3">
                                        <span className="text-sm font-semibold text-foreground">
                                            {formatFileCount(
                                                fileSummary.total.count,
                                            )}
                                        </span>
                                        <span className="text-sm text-muted-foreground">
                                            {formatBytes(
                                                fileSummary.total.size,
                                            )}
                                        </span>
                                    </dd>
                                </div>
                                {fileSummary.categories.map((group) => (
                                    <div
                                        key={group.label}
                                        className="rounded-lg border border-border/60 bg-card/70 px-3 py-2"
                                    >
                                        <dt className="text-xs text-muted-foreground">
                                            {group.label}
                                        </dt>
                                        <dd className="mt-1 flex items-baseline justify-between gap-3">
                                            <span className="text-sm font-medium text-foreground">
                                                {formatFileCount(group.count)}
                                            </span>
                                            <span className="text-xs text-muted-foreground">
                                                {formatBytes(group.size)}
                                            </span>
                                        </dd>
                                    </div>
                                ))}
                            </dl>
                        </section>
                    </div>

                    <div className="grid content-start gap-4 xl:grid-cols-[minmax(0,1.45fr)_minmax(18rem,0.75fr)]">
                        <div className="rounded-lg border border-border/70 bg-card/65 p-4">
                            <ResultRegistrationSummary
                                fields={detailFields(result)}
                                variant="integrated"
                            />
                        </div>

                        <div className="rounded-lg border border-border/70 bg-card/65 p-4">
                            <ResultMetadataEnrichment
                                key={result.id}
                                metadata={result.metadata}
                                variant="integrated"
                            />
                        </div>
                    </div>
                </div>
            </section>

            <ResultDetailFiles files={files} resultId={result.id} />
        </main>
    );
}
