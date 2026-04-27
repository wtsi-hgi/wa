import Link from "next/link";

import { ChevronLeft } from "lucide-react";

import {
    enrichIdentifier,
    fetchFiles,
    fetchResult,
} from "@/app/(results)/actions";
import { ResultIdCopyChip } from "@/app/(results)/results/[id]/result-id-copy-chip";
import { ResultDetailFiles } from "@/components/result-detail-files";
import { ResultMetadataEnrichment } from "@/components/result-metadata-enrichment";
import { ResultRegistrationSummary } from "@/components/result-registration-summary";
import type { FileEntry, ResultSet } from "@/lib/contracts";
import { enrichSeqmetaMetadata } from "@/lib/seqmeta-enrichment";
import { getRequestSeqmetaCache } from "@/lib/seqmeta-cache-server";

type DetailPageParams = {
    id: string;
};

type DetailPageSearchParams = {
    returnTo?: string | string[];
};

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

function groupFiles(
    files: FileEntry[],
): Array<{ label: string; count: number }> {
    return [
        {
            label: "output",
            count: files.filter((file) => file.kind === "output").length,
        },
        {
            label: "input",
            count: files.filter((file) => file.kind === "input").length,
        },
        {
            label: "pipeline",
            count: files.filter((file) => file.kind === "pipeline").length,
        },
    ];
}

function detailFields(result: ResultSet) {
    return [
        { label: "Result ID", value: result.id, mono: true },
        { label: "Pipeline name", value: result.pipeline_name },
        { label: "Pipeline version", value: result.pipeline_version },
        {
            label: "Pipeline identifier",
            value: result.pipeline_identifier,
            mono: true,
        },
        { label: "Run key", value: result.run_key, mono: true },
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

export const dynamic = "force-dynamic";

export default async function ResultDetailPage({
    params,
    searchParams,
}: {
    params: Promise<DetailPageParams>;
    searchParams?: Promise<DetailPageSearchParams>;
}) {
    const { id } = await params;
    const returnHref = resolveReturnHref((await searchParams) ?? {});
    const returnLabel =
        returnHref === "/" ? "Back to dashboard" : "Back to search results";
    const resultPromise = fetchResult(id);
    const filesPromise = fetchFiles(id);
    const requestCachePromise = getRequestSeqmetaCache();
    const result = await resultPromise;
    const enrichmentPromise = requestCachePromise.then((cache) =>
        enrichSeqmetaMetadata(result.metadata, cache, enrichIdentifier),
    );
    const [files, enrichmentState] = await Promise.all([
        filesPromise,
        enrichmentPromise,
    ]);
    const fileGroups = groupFiles(files);

    return (
        <main className="mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-8 px-6 py-8 sm:px-10 lg:px-12 lg:py-10">
            <section className="overflow-hidden rounded-[2rem] border border-border/70 bg-[linear-gradient(135deg,color-mix(in_oklab,var(--card)_88%,white_12%),color-mix(in_oklab,var(--accent)_12%,var(--card)_88%))] shadow-[0_36px_120px_-72px_rgba(41,58,85,0.85)]">
                <div className="grid gap-8 px-6 py-8 sm:px-8 lg:grid-cols-[1.35fr_0.85fr] lg:px-10 lg:py-10">
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

                    <section className="rounded-[1.75rem] border border-border/70 bg-background/80 p-5">
                        <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                            Registered files
                        </p>
                        <p className="mt-3 text-3xl font-semibold tracking-tight">
                            {files.length}
                        </p>
                        <div className="mt-6 grid gap-3 sm:grid-cols-3">
                            {fileGroups.map((group) => (
                                <div
                                    key={group.label}
                                    className="rounded-2xl border border-border/70 bg-card/80 p-4"
                                >
                                    <p className="text-xs uppercase tracking-[0.22em] text-muted-foreground">
                                        {group.label}
                                    </p>
                                    <p className="mt-2 text-2xl font-semibold text-foreground">
                                        {group.count}
                                    </p>
                                    <p className="mt-1 text-sm text-muted-foreground">
                                        {group.count} {group.label}
                                    </p>
                                </div>
                            ))}
                        </div>
                    </section>
                </div>
            </section>

            <ResultRegistrationSummary fields={detailFields(result)} />

            <ResultDetailFiles files={files} resultId={result.id} />

            <ResultMetadataEnrichment
                key={result.id}
                initialEnrichments={enrichmentState.enrichments}
                initialErrors={enrichmentState.errors}
                metadata={result.metadata}
            />
        </main>
    );
}
