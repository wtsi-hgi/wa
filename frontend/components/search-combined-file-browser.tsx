"use client";

import { useMemo } from "react";
import { Files, Info, LockKeyhole, Rows3 } from "lucide-react";

import {
    ResultDetailFiles,
    type RegisteredFileEntry,
} from "@/components/result-detail-files";
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@/components/ui/popover";
import type { DirectoryTreeNode } from "@/components/file-browser";
import type { ResultSet } from "@/lib/contracts";
import { formatRegistrationUnique } from "@/lib/result-identity";
import { cn } from "@/lib/utils";

export type CombinedSearchRegistration = {
    fileCount: number;
    result: ResultSet;
};

export type CombinedSearchFile = RegisteredFileEntry & {
    resultId: string;
};

type SearchCombinedFileBrowserProps = {
    files: CombinedSearchFile[];
    lockedRegistrations?: CombinedSearchRegistration[];
    mode: SearchFileMode;
    onModeChange: (mode: SearchFileMode) => void;
    registrations: CombinedSearchRegistration[];
};

export type SearchFileMode = "combined" | "rows";

function directoryName(path: string): string {
    return path.split("/").filter(Boolean).at(-1) ?? path;
}

function parentDirectory(path: string): string {
    const index = path.lastIndexOf("/");

    return index <= 0 ? "/" : path.slice(0, index);
}

function commonPath(paths: string[]): string | undefined {
    const [firstPath, ...remainingPaths] = paths;

    if (!firstPath) {
        return undefined;
    }

    let commonSegments = firstPath.split("/").filter(Boolean);

    for (const path of remainingPaths) {
        const segments = path.split("/").filter(Boolean);
        const nextCommon: string[] = [];

        for (let index = 0; index < commonSegments.length; index += 1) {
            if (commonSegments[index] !== segments[index]) {
                break;
            }

            nextCommon.push(commonSegments[index] ?? "");
        }

        commonSegments = nextCommon;
    }

    return commonSegments.length > 0 ? `/${commonSegments.join("/")}` : "/";
}

function commonDirectory(paths: string[]): string | undefined {
    return commonPath(paths.map(parentDirectory));
}

function commonLockedParentDirectory(
    registrations: CombinedSearchRegistration[],
): string | undefined {
    const outputDirectories = registrations.map(
        (registration) => registration.result.output_directory,
    );

    if (outputDirectories.length === 1) {
        return parentDirectory(outputDirectories[0] ?? "/");
    }

    return commonPath(outputDirectories);
}

function metadataEntries(result: ResultSet): Array<[string, string]> {
    return Object.entries(result.metadata).sort(([left], [right]) =>
        left.localeCompare(right),
    );
}

function RegistrationInfoButton({ result }: { result: ResultSet }) {
    const metadata = metadataEntries(result);
    const fields = [
        ["Pipeline", result.pipeline_name],
        ["Unique", formatRegistrationUnique(result.run_key)],
        ["Requester", result.requester],
        ["Operator", result.operator],
        ["Version", result.pipeline_version],
        ["Output", result.output_directory],
        ["Command", result.command],
    ];

    return (
        <Popover>
            <PopoverTrigger asChild>
                <button
                    aria-label={`Registration details for ${directoryName(result.output_directory)}`}
                    className="inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-border/70 bg-background text-muted-foreground transition hover:border-primary/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
                    data-combined-result-info={result.id}
                    type="button"
                >
                    <Info className="size-4" aria-hidden="true" />
                </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-[min(92vw,34rem)] p-4">
                <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                        <p className="truncate text-sm font-semibold text-foreground">
                            {result.pipeline_name}
                        </p>
                        <p className="mt-1 font-mono text-xs text-muted-foreground">
                            {formatRegistrationUnique(result.run_key)}
                        </p>
                    </div>
                    <p className="shrink-0 text-xs text-muted-foreground">
                        Registered output
                    </p>
                </div>

                <dl className="mt-3 grid max-h-[min(24rem,70vh)] gap-2 overflow-auto pr-1 sm:grid-cols-2">
                    {fields.map(([label, value]) => (
                        <div
                            className={cn(
                                "min-w-0 rounded-lg border border-border/60 bg-background/70 px-3 py-2",
                                (label === "Output" || label === "Command") &&
                                    "sm:col-span-2",
                            )}
                            data-combined-result-info-field={label}
                            key={label}
                        >
                            <dt className="text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                                {label}
                            </dt>
                            <dd className="mt-1 break-words text-xs leading-5 text-foreground">
                                {value}
                            </dd>
                        </div>
                    ))}
                    {metadata.map(([label, value]) => (
                        <div
                            className="min-w-0 rounded-lg border border-border/60 bg-background/70 px-3 py-2"
                            data-combined-result-info-metadata={label}
                            key={label}
                        >
                            <dt className="text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                                {label}
                            </dt>
                            <dd className="mt-1 break-words text-xs leading-5 text-foreground">
                                {value}
                            </dd>
                        </div>
                    ))}
                </dl>
            </PopoverContent>
        </Popover>
    );
}

function pluralize(count: number, singular: string, plural = `${singular}s`) {
    return count === 1 ? singular : plural;
}

function LockedCombinedFilesState({
    registrations,
}: {
    registrations: CombinedSearchRegistration[];
}) {
    const rootDirectory = commonLockedParentDirectory(registrations) ?? "/";
    const resultSetCount = registrations.length;

    if (resultSetCount === 0) {
        return null;
    }

    return (
        <div
            className="rounded-xl border border-border/75 bg-card p-3 shadow-[0_18px_60px_-48px_rgba(48,67,98,0.8)]"
            data-combined-file-browser-locked="true"
        >
            <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                    <p className="flex items-center gap-2 text-sm font-semibold uppercase tracking-[0.18em] text-muted-foreground">
                        <LockKeyhole className="size-4 text-primary" />
                        <span>File access locked</span>
                    </p>
                    <p className="mt-2 break-words font-mono text-sm text-foreground">
                        {rootDirectory}
                    </p>
                </div>
                <p className="rounded-md border border-border/70 bg-background px-2 py-1 text-sm text-muted-foreground">
                    {resultSetCount} matching{" "}
                    {pluralize(resultSetCount, "result set")}
                </p>
            </div>
            <div
                className="mt-3 space-y-2"
                data-locked-root-directory={rootDirectory}
            >
                {registrations.map(({ result }) => (
                    <div
                        className="grid gap-2 rounded-lg border border-border/70 bg-background/70 p-3 sm:grid-cols-[minmax(0,1fr)_auto]"
                        data-locked-output-directory="true"
                        key={result.id}
                    >
                        <div className="min-w-0">
                            <p className="truncate font-mono text-sm text-foreground">
                                {result.output_directory}
                            </p>
                            <p className="mt-1 text-xs text-muted-foreground">
                                Directory marker only
                            </p>
                        </div>
                        <p className="self-center text-xs font-medium text-muted-foreground">
                            Locked
                        </p>
                    </div>
                ))}
            </div>
        </div>
    );
}

export function SearchCombinedFileBrowser({
    files,
    lockedRegistrations = [],
    mode,
    onModeChange,
    registrations,
}: SearchCombinedFileBrowserProps) {
    const registrationsByOutputDirectory = useMemo(
        () =>
            new Map(
                registrations.map((registration) => [
                    registration.result.output_directory,
                    registration.result,
                ]),
            ),
        [registrations],
    );
    const initialDirectory = useMemo(
        () => commonDirectory(files.map((file) => file.path)),
        [files],
    );
    if (files.length === 0 && lockedRegistrations.length === 0) {
        return null;
    }

    const renderDirectoryAction = (node: DirectoryTreeNode) => {
        const result = registrationsByOutputDirectory.get(node.path);

        return result ? <RegistrationInfoButton result={result} /> : null;
    };

    return (
        <section
            className="space-y-3"
            data-search-combined-file-browser="true"
            data-search-file-mode={mode}
        >
            <div
                aria-label="Search result display"
                className="inline-flex w-fit max-w-full rounded-lg border border-border/70 bg-background p-1 shadow-sm"
                role="group"
            >
                <button
                    aria-pressed={mode === "combined"}
                    className={cn(
                        "inline-flex min-h-9 items-center gap-2 rounded-md px-3 text-sm font-medium transition",
                        mode === "combined"
                            ? "bg-primary text-primary-foreground shadow-sm"
                            : "text-muted-foreground hover:bg-muted/70 hover:text-foreground",
                    )}
                    onClick={() => onModeChange("combined")}
                    type="button"
                >
                    <Files className="size-4" aria-hidden="true" />
                    <span>Combined files</span>
                </button>
                <button
                    aria-pressed={mode === "rows"}
                    className={cn(
                        "inline-flex min-h-9 items-center gap-2 rounded-md px-3 text-sm font-medium transition",
                        mode === "rows"
                            ? "bg-primary text-primary-foreground shadow-sm"
                            : "text-muted-foreground hover:bg-muted/70 hover:text-foreground",
                    )}
                    onClick={() => onModeChange("rows")}
                    type="button"
                >
                    <Rows3 className="size-4" aria-hidden="true" />
                    <span>Result rows</span>
                </button>
            </div>

            {mode === "combined" ? (
                <div className="space-y-3">
                    {files.length > 0 ? (
                        <ResultDetailFiles
                            files={files}
                            initialSelectedDirectory={initialDirectory}
                            renderDirectoryAction={renderDirectoryAction}
                            resultId={files[0]?.resultId ?? ""}
                        />
                    ) : null}
                    <LockedCombinedFilesState
                        registrations={lockedRegistrations}
                    />
                </div>
            ) : null}
        </section>
    );
}
