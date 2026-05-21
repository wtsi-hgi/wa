import Link from "next/link";

import { ChevronLeft, LockKeyhole } from "lucide-react";

import { fetchFiles, fetchResult } from "@/app/(results)/actions";
import { ResultDetailFiles } from "@/components/result-detail-files";
import { ResultMetadataEnrichment } from "@/components/result-metadata-enrichment";
import { ResultRegistrationSummary } from "@/components/result-registration-summary";
import { BackendRequestError } from "@/lib/backend-client";
import {
    lockedResponseSchema,
    type FileEntry,
    type LockedResponse,
    type ResultSet,
} from "@/lib/contracts";
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

function fileDetailFields(fileSummary: ReturnType<typeof summarizeFiles>) {
    return [
        {
            label: "Registered files",
            value: formatFileCount(fileSummary.total.count),
        },
        {
            label: "Total file size",
            value: formatBytes(fileSummary.total.size),
            mono: true,
        },
        ...fileSummary.categories.map((category) => ({
            label: `${category.label} files`,
            value: `${formatFileCount(category.count)} / ${formatBytes(category.size)}`,
        })),
    ];
}

function detailFields(
    result: ResultSet,
    fileSummary: ReturnType<typeof summarizeFiles>,
) {
    return [
        { label: "Result ID", value: result.id, mono: true, wide: true },
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
        {
            label: "Raw run key",
            value: result.run_key,
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
        ...fileDetailFields(fileSummary),
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

function lockedResponseFromError(error: unknown): LockedResponse | null {
    if (!(error instanceof BackendRequestError) || error.status !== 403) {
        return null;
    }

    const parsed = lockedResponseSchema.safeParse(error.body);

    return parsed.success ? parsed.data : null;
}

function LockedResultDetailState({
    locked,
    returnHref,
}: {
    locked: LockedResponse;
    returnHref: string;
}) {
    return (
        <main
            className="mx-auto flex min-h-screen w-full max-w-[84rem] items-center justify-center px-4 py-6 sm:px-8 lg:py-8"
            data-locked-result-detail="true"
        >
            <section className="flex max-w-xl flex-col items-center gap-5 text-center">
                <span className="inline-flex h-16 w-16 items-center justify-center rounded-full border border-border/70 bg-muted text-muted-foreground">
                    <LockKeyhole
                        aria-hidden="true"
                        className="h-8 w-8"
                        data-locked-detail-icon="true"
                    />
                </span>
                <h1 className="text-2xl font-semibold tracking-tight text-balance sm:text-3xl">
                    {locked.message}
                </h1>
                <Link
                    aria-label="Back to dashboard"
                    className="inline-flex min-h-9 items-center rounded-full border border-border/70 bg-background/85 px-4 py-2 text-sm font-medium text-muted-foreground transition hover:text-foreground"
                    data-return-link="true"
                    href={returnHref}
                >
                    Back to dashboard
                </Link>
            </section>
        </main>
    );
}

export function ResultDetailLoadingFallback() {
    return (
        <main className="mx-auto flex min-h-screen w-full max-w-[84rem] flex-col gap-6 px-4 py-6 sm:px-8 lg:py-8">
            <section className="overflow-hidden rounded-2xl border border-border/70 bg-[linear-gradient(135deg,color-mix(in_oklab,var(--card)_90%,white_10%),color-mix(in_oklab,var(--accent)_8%,var(--card)_92%))] shadow-[0_28px_80px_-64px_rgba(41,58,85,0.78)]">
                <div className="grid gap-5 p-3">
                    <div className="space-y-4">
                        <div className="space-y-3">
                            <h1 className="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
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
    let result: ResultSet;

    try {
        result = await fetchResult(id);
    } catch (error) {
        const locked = lockedResponseFromError(error);

        if (locked) {
            return (
                <LockedResultDetailState
                    locked={locked}
                    returnHref={returnHref}
                />
            );
        }

        throw error;
    }

    let files: FileEntry[];

    try {
        files = await fetchFiles(id);
    } catch (error) {
        const locked = lockedResponseFromError(error);

        if (locked) {
            return (
                <LockedResultDetailState
                    locked={locked}
                    returnHref={returnHref}
                />
            );
        }

        throw error;
    }

    const fileSummary = summarizeFiles(files);

    return (
        <main className="mx-auto flex min-h-screen w-full max-w-[84rem] flex-col gap-5 px-4 py-6 sm:px-8 lg:py-8">
            <section
                className="overflow-hidden rounded-2xl border border-border/70 bg-[linear-gradient(135deg,color-mix(in_oklab,var(--card)_90%,white_10%),color-mix(in_oklab,var(--accent)_8%,var(--card)_92%))] shadow-[0_28px_80px_-64px_rgba(41,58,85,0.78)]"
                data-result-detail-summary="true"
            >
                <div className="space-y-4 p-3">
                    <div className="min-w-0 space-y-3">
                        <div className="flex flex-wrap items-center gap-2">
                            <Link
                                href={returnHref}
                                className="inline-flex min-h-8 items-center gap-2 rounded-full border border-border/70 bg-background/85 px-3 py-1 text-xs font-medium text-muted-foreground transition hover:text-foreground"
                                data-return-link="true"
                            >
                                <ChevronLeft
                                    className="h-3.5 w-3.5"
                                    aria-hidden="true"
                                />
                                <span>{returnLabel}</span>
                            </Link>
                        </div>

                        <div className="flex min-w-0 flex-col gap-2 lg:flex-row lg:items-baseline lg:justify-between">
                            <h1 className="min-w-0 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                                {result.pipeline_name}
                                <span className="ml-3 align-baseline font-mono text-base font-medium text-muted-foreground sm:text-lg">
                                    {formatRegistrationUnique(result.run_key)}
                                </span>
                            </h1>
                            <div
                                className="flex shrink-0 items-center gap-2 text-sm font-medium text-muted-foreground lg:justify-end"
                                data-title-file-summary="true"
                            >
                                <span>
                                    {formatFileCount(fileSummary.total.count)}
                                </span>
                                <span
                                    className="h-1 w-1 rounded-full bg-muted-foreground/50"
                                    aria-hidden="true"
                                />
                                <span className="font-mono">
                                    {formatBytes(fileSummary.total.size)}
                                </span>
                            </div>
                        </div>
                    </div>

                    <div className="grid gap-3 border-t border-border/60 pt-3 lg:grid-cols-[minmax(0,1.08fr)_minmax(20rem,0.92fr)]">
                        <div className="min-w-0 space-y-3">
                            <ResultRegistrationSummary
                                fields={detailFields(result, fileSummary)}
                                variant="integrated"
                            />
                        </div>

                        <div className="min-w-0">
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
