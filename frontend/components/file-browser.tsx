"use client";

import { type ReactNode, useEffect, useMemo, useState } from "react";
import { Eye, FolderTree, ListFilter } from "lucide-react";

import { type FileEntry } from "@/lib/contracts";
import { cn, formatBytes } from "@/lib/utils";

export type PreviewMode = "single" | "grid";

export type DirectoryGroup = {
    fileCount: number;
    files: FileEntry[];
    path: string;
    totalSize: number;
    typeCounts: Record<string, number>;
};

const fileKindOrder: Record<FileEntry["kind"], number> = {
    output: 0,
    input: 1,
    pipeline: 2,
};

type FileBrowserProps = {
    files: FileEntry[];
    onPreviewHeightChange?: (value: number) => void;
    onPreviewModeChange?: (mode: PreviewMode) => void;
    onPreviewPageChange?: (page: number) => void;
    onSelectDirectory?: (path: string) => void;
    onSelectFile: (file: FileEntry) => void;
    previewHeight?: number;
    previewMode?: PreviewMode;
    previewPage?: number;
    previewPageCount?: number;
    previewSummary?: string;
    renderGridPreview?: (file: FileEntry) => ReactNode;
    renderSinglePreview?: (file: FileEntry | null) => ReactNode;
    selectedDirectory?: string;
    selectedPath?: string;
    visibleFiles?: FileEntry[];
};

function parentDirectory(path: string): string {
    const normalized = path.trim();
    const index = normalized.lastIndexOf("/");

    if (index <= 0) {
        return "/";
    }

    return normalized.slice(0, index);
}

function fileName(path: string): string {
    return path.split("/").pop() ?? path;
}

function formatMtime(mtime: string | undefined): string {
    if (!mtime) {
        return "Unknown time";
    }

    const date = new Date(mtime);

    if (Number.isNaN(date.getTime())) {
        return mtime;
    }

    const year = date.getUTCFullYear();
    const month = String(date.getUTCMonth() + 1).padStart(2, "0");
    const day = String(date.getUTCDate()).padStart(2, "0");
    const hours = String(date.getUTCHours()).padStart(2, "0");
    const minutes = String(date.getUTCMinutes()).padStart(2, "0");

    return `${year}-${month}-${day} ${hours}:${minutes} UTC`;
}

function toTypeKey(path: string): string {
    const name = fileName(path);
    const extensionIndex = name.lastIndexOf(".");

    if (extensionIndex <= 0 || extensionIndex === name.length - 1) {
        return "file";
    }

    return name.slice(extensionIndex + 1).toLowerCase();
}

function formatTypeSummary(typeCounts: Record<string, number>): string {
    const entries = Object.entries(typeCounts);

    if (entries.length === 0) {
        return "No files";
    }

    return entries
        .sort(
            (left, right) =>
                right[1] - left[1] || left[0].localeCompare(right[0]),
        )
        .map(([type, count]) => `${count} ${type}`)
        .join(", ");
}

export function buildDirectoryGroups(files: FileEntry[]): DirectoryGroup[] {
    const groups = new Map<string, DirectoryGroup>();

    for (const file of files) {
        const directoryPath = parentDirectory(file.path);
        const current =
            groups.get(directoryPath) ??
            ({
                fileCount: 0,
                files: [],
                path: directoryPath,
                totalSize: 0,
                typeCounts: {},
            } satisfies DirectoryGroup);

        current.files.push(file);
        current.fileCount += 1;
        current.totalSize += file.size;

        const typeKey = toTypeKey(file.path);
        current.typeCounts[typeKey] = (current.typeCounts[typeKey] ?? 0) + 1;
        groups.set(directoryPath, current);
    }

    return [...groups.values()].sort(
        (left, right) =>
            fileKindOrder[left.files[0]?.kind ?? "pipeline"] -
                fileKindOrder[right.files[0]?.kind ?? "pipeline"] ||
            left.path.localeCompare(right.path),
    );
}

export function FileBrowser({
    files,
    onPreviewHeightChange,
    onPreviewModeChange,
    onPreviewPageChange,
    onSelectDirectory,
    onSelectFile,
    previewHeight = 220,
    previewMode = "single",
    previewPage = 1,
    previewPageCount = 1,
    previewSummary,
    renderGridPreview,
    renderSinglePreview,
    selectedDirectory,
    selectedPath,
    visibleFiles,
}: FileBrowserProps) {
    const [uncontrolledDirectory, setUncontrolledDirectory] = useState<
        string | undefined
    >(selectedDirectory);
    const [uncontrolledPath, setUncontrolledPath] = useState<
        string | undefined
    >(selectedPath);
    const directoryGroups = useMemo(() => buildDirectoryGroups(files), [files]);
    const preferredDirectory = selectedDirectory ?? uncontrolledDirectory;
    const activeDirectory =
        directoryGroups.find((group) => group.path === preferredDirectory) ??
        directoryGroups[0];
    const activeFiles = activeDirectory?.files ?? [];
    const effectiveSelectedDirectory = activeDirectory?.path;
    const preferredSelectedPath = selectedPath ?? uncontrolledPath;
    const activeFile =
        activeFiles.find((file) => file.path === preferredSelectedPath) ??
        activeFiles[0];
    const effectiveSelectedPath = activeFile?.path;
    const displayedFiles = visibleFiles ?? activeFiles;

    useEffect(() => {
        if (!activeDirectory) {
            return;
        }

        if (preferredDirectory === activeDirectory.path) {
            return;
        }

        onSelectDirectory?.(activeDirectory.path);
    }, [
        activeDirectory,
        preferredDirectory,
        onSelectDirectory,
        selectedDirectory,
    ]);

    useEffect(() => {
        if (!activeFile) {
            return;
        }

        if (preferredSelectedPath === activeFile.path) {
            return;
        }

        onSelectFile(activeFile);
    }, [activeFile, onSelectFile, preferredSelectedPath, selectedPath]);

    if (directoryGroups.length === 0) {
        return (
            <section className="rounded-[1.75rem] border border-border/70 bg-card/85 p-5 shadow-[0_28px_90px_-72px_rgba(48,67,98,0.9)]">
                <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                    File Browser
                </p>
                <div className="mt-5 rounded-[1.5rem] border border-dashed border-border/70 bg-background/40 px-5 py-8 text-sm text-muted-foreground">
                    No registered files
                </div>
            </section>
        );
    }

    const renderFileButton = (file: FileEntry) => (
        <button
            type="button"
            className={cn(
                "flex w-full items-start gap-4 rounded-[1.25rem] border px-4 py-4 text-left transition",
                file.path === activeFile?.path
                    ? "border-primary/45 bg-primary/10"
                    : "border-border/60 bg-background/65 hover:border-primary/35 hover:bg-background",
            )}
            data-file-path={file.path}
            onClick={() => {
                if (selectedPath === undefined) {
                    setUncontrolledPath(file.path);
                }

                onSelectFile(file);
            }}
        >
            <span className="min-w-0 flex-1">
                <span className="block truncate text-base font-medium text-foreground">
                    {fileName(file.path)}
                </span>
                <span className="mt-2 block break-all text-xs text-muted-foreground">
                    {file.path}
                </span>
                <span className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground">
                    <span>{formatBytes(file.size)}</span>
                    <span>{formatMtime(file.mtime)}</span>
                    <span className="uppercase tracking-[0.18em]">
                        {file.kind}
                    </span>
                </span>
            </span>
        </button>
    );

    return (
        <section
            className="rounded-[1.75rem] border border-border/70 bg-card/85 p-4 shadow-[0_28px_90px_-72px_rgba(48,67,98,0.9)] sm:p-5"
            data-file-browser="true"
        >
            <div className="flex flex-col gap-4 border-b border-border/60 pb-5 xl:flex-row xl:items-start xl:justify-between">
                <div>
                    <div className="flex items-center gap-3">
                        <FolderTree
                            className="size-4 text-primary"
                            aria-hidden="true"
                        />
                        <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                            File Browser
                        </p>
                    </div>
                    <h2 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
                        {activeDirectory?.path}
                    </h2>
                    <p className="mt-2 text-sm text-muted-foreground">
                        {previewSummary ??
                            `Showing ${activeFiles.length} file${activeFiles.length === 1 ? "" : "s"} in this directory.`}
                    </p>
                </div>

                <div className="space-y-3 xl:min-w-[24rem]">
                    <div className="flex flex-wrap items-center gap-3">
                        <label className="inline-flex items-center gap-3 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-sm text-foreground">
                            <input
                                checked={previewMode === "grid"}
                                className="size-4 accent-primary"
                                onChange={(event) =>
                                    onPreviewModeChange?.(
                                        event.target.checked
                                            ? "grid"
                                            : "single",
                                    )
                                }
                                type="checkbox"
                            />
                            <span className="inline-flex items-center gap-2">
                                <Eye
                                    className="size-4 text-primary"
                                    aria-hidden="true"
                                />
                                Preview first 100 files
                            </span>
                        </label>

                        <div className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-sm text-muted-foreground">
                            <ListFilter
                                className="size-4 text-primary"
                                aria-hidden="true"
                            />
                            {previewPageCount > 1
                                ? `Page ${previewPage} of ${previewPageCount}`
                                : "Single preview"}
                        </div>
                    </div>

                    <label className="block rounded-[1.25rem] border border-border/70 bg-background/75 px-4 py-3 text-sm text-foreground">
                        <div className="flex items-center justify-between gap-3">
                            <span className="font-medium">Preview height</span>
                            <span className="text-muted-foreground">
                                {previewHeight}px
                            </span>
                        </div>
                        <input
                            aria-label="Preview height"
                            className="mt-3 w-full accent-primary"
                            max={420}
                            min={120}
                            onChange={(event) =>
                                onPreviewHeightChange?.(
                                    Number(event.target.value),
                                )
                            }
                            step={20}
                            type="range"
                            value={previewHeight}
                        />
                    </label>

                    {previewPageCount > 1 ? (
                        <div className="flex items-center justify-end gap-2">
                            <button
                                type="button"
                                className="rounded-full border border-border/70 bg-background px-3 py-2 text-sm text-foreground transition hover:border-primary/35 disabled:cursor-not-allowed disabled:opacity-50"
                                disabled={previewPage <= 1}
                                onClick={() =>
                                    onPreviewPageChange?.(previewPage - 1)
                                }
                            >
                                Previous
                            </button>
                            <button
                                type="button"
                                className="rounded-full border border-border/70 bg-background px-3 py-2 text-sm text-foreground transition hover:border-primary/35 disabled:cursor-not-allowed disabled:opacity-50"
                                disabled={previewPage >= previewPageCount}
                                onClick={() =>
                                    onPreviewPageChange?.(previewPage + 1)
                                }
                            >
                                Next
                            </button>
                        </div>
                    ) : null}
                </div>
            </div>

            <div className="mt-5 rounded-[1.5rem] border border-border/70 bg-background/55 p-4">
                <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                    Folders
                </p>
                <div className="mt-4 flex flex-wrap gap-3">
                    {directoryGroups.map((group) => {
                        const isSelected = group.path === activeDirectory?.path;

                        return (
                            <button
                                key={group.path}
                                type="button"
                                className={cn(
                                    "min-w-[14rem] flex-1 rounded-[1.25rem] border px-4 py-3 text-left transition xl:flex-none xl:basis-[calc(33.333%-0.75rem)]",
                                    isSelected
                                        ? "border-primary/45 bg-primary/10"
                                        : "border-border/60 bg-background/60 hover:border-primary/35 hover:bg-background",
                                )}
                                data-directory-path={group.path}
                                onClick={() => {
                                    if (selectedDirectory === undefined) {
                                        setUncontrolledDirectory(group.path);
                                    }

                                    onSelectDirectory?.(group.path);
                                }}
                            >
                                <p className="break-all font-medium text-foreground">
                                    {group.path}
                                </p>
                                <p className="mt-2 text-sm text-muted-foreground">
                                    {group.fileCount} file
                                    {group.fileCount === 1 ? "" : "s"} ·{" "}
                                    {formatBytes(group.totalSize)}
                                </p>
                                <p className="mt-1 text-xs uppercase tracking-[0.18em] text-muted-foreground">
                                    {formatTypeSummary(group.typeCounts)}
                                </p>
                            </button>
                        );
                    })}
                </div>
            </div>

            {previewMode === "single" ? (
                <div className="mt-5 grid gap-5 xl:grid-cols-[minmax(18rem,0.9fr)_minmax(0,1.1fr)]">
                    <div className="min-w-0 rounded-[1.5rem] border border-border/70 bg-background/55 p-4">
                        <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                            Files
                        </p>
                        <p className="mt-2 text-sm text-muted-foreground">
                            Select a file from {activeDirectory?.path}.
                        </p>
                        <ul className="mt-5 space-y-3">
                            {displayedFiles.map((file) => (
                                <li key={file.path}>
                                    {renderFileButton(file)}
                                </li>
                            ))}
                        </ul>
                    </div>

                    <div className="min-w-0 rounded-[1.5rem] border border-border/70 bg-background/55 p-4">
                        <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                            Preview
                        </p>
                        <h3 className="mt-2 break-all text-xl font-semibold tracking-tight text-foreground">
                            {activeFile
                                ? fileName(activeFile.path)
                                : "No file selected"}
                        </h3>
                        {activeFile ? (
                            <p className="mt-2 break-all text-sm text-muted-foreground">
                                {activeFile.path}
                            </p>
                        ) : null}
                        <div
                            className="mt-5"
                            data-file-browser-preview="single"
                        >
                            {renderSinglePreview?.(activeFile) ?? null}
                        </div>
                    </div>
                </div>
            ) : (
                <div
                    className="mt-5 rounded-[1.5rem] border border-border/70 bg-background/55 p-4"
                    data-preview-mode="grid"
                >
                    <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                        Files and previews
                    </p>
                    <p className="mt-2 text-sm text-muted-foreground">
                        1 preview per row at {previewHeight}px high.
                    </p>

                    <div className="mt-5 space-y-3">
                        {displayedFiles.map((file) => (
                            <div
                                key={file.path}
                                className="grid gap-3 xl:grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] xl:items-start"
                            >
                                <div>{renderFileButton(file)}</div>
                                <div
                                    className="min-w-0 rounded-[1.25rem] border border-border/60 bg-background/65 p-3"
                                    data-grid-preview-path={file.path}
                                >
                                    {renderGridPreview?.(file) ?? null}
                                </div>
                            </div>
                        ))}
                    </div>
                </div>
            )}
        </section>
    );
}
