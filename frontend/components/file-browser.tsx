"use client";

import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import {
    ChevronDown,
    ChevronRight,
    Eye,
    FolderTree,
    ListFilter,
} from "lucide-react";

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

export type DirectoryTreeNode = {
    children: DirectoryTreeNode[];
    fileCount: number;
    files: FileEntry[];
    label: string;
    path: string;
    totalSize: number;
    typeCounts: Record<string, number>;
    weight: number;
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

type RawDirectoryNode = {
    children: Map<string, RawDirectoryNode>;
    group?: DirectoryGroup;
    name: string;
    path: string;
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

function directoryLabel(path: string): string {
    return path === "/" ? "/" : path.slice(1);
}

function pathSegments(path: string): string[] {
    if (path === "/") {
        return [];
    }

    return path.split("/").filter(Boolean);
}

function subtreeWeight(node: DirectoryTreeNode): number {
    return node.weight;
}

function compareDirectoryTreeNodes(
    left: DirectoryTreeNode,
    right: DirectoryTreeNode,
): number {
    return (
        subtreeWeight(left) - subtreeWeight(right) ||
        left.path.localeCompare(right.path)
    );
}

function buildRawDirectoryTree(groups: DirectoryGroup[]): RawDirectoryNode[] {
    const root: RawDirectoryNode = {
        children: new Map<string, RawDirectoryNode>(),
        name: "",
        path: "/",
    };

    for (const group of groups) {
        const segments = pathSegments(group.path);

        if (segments.length === 0) {
            root.group = group;
            continue;
        }

        let current = root;
        let currentPath = "";

        for (const segment of segments) {
            currentPath = `${currentPath}/${segment}`;
            const existingChild = current.children.get(segment);

            if (existingChild) {
                current = existingChild;
                continue;
            }

            const next: RawDirectoryNode = {
                children: new Map<string, RawDirectoryNode>(),
                name: segment,
                path: currentPath,
            };

            current.children.set(segment, next);
            current = next;
        }

        current.group = group;
    }

    if (root.group) {
        return [root];
    }

    return [...root.children.values()];
}

function compressDirectoryNode(rawNode: RawDirectoryNode): DirectoryTreeNode {
    const labelSegments = [rawNode.name];
    let current = rawNode;

    while (!current.group && current.children.size === 1) {
        const nextChild = [...current.children.values()][0];

        if (!nextChild) {
            break;
        }

        labelSegments.push(nextChild.name);
        current = nextChild;
    }

    const children = [...current.children.values()]
        .map((child) => compressDirectoryNode(child))
        .sort(compareDirectoryTreeNodes);
    const weight = Math.min(
        current.group
            ? fileKindOrder[current.group.files[0]?.kind ?? "pipeline"]
            : Number.POSITIVE_INFINITY,
        ...children.map((child) => child.weight),
    );

    return {
        children,
        fileCount: current.group?.fileCount ?? 0,
        files: current.group?.files ?? [],
        label: labelSegments.join("/"),
        path: current.path,
        totalSize: current.group?.totalSize ?? 0,
        typeCounts: current.group?.typeCounts ?? {},
        weight: Number.isFinite(weight) ? weight : fileKindOrder.pipeline,
    };
}

export function buildDirectoryTree(files: FileEntry[]): DirectoryTreeNode[] {
    return buildRawDirectoryTree(buildDirectoryGroups(files))
        .map((node) => compressDirectoryNode(node))
        .sort(compareDirectoryTreeNodes);
}

function ancestorPaths(path: string | undefined): string[] {
    if (!path) {
        return [];
    }

    const segments = pathSegments(path);

    return segments.map(
        (_, index) => `/${segments.slice(0, index + 1).join("/")}`,
    );
}

function collectTreePaths(node: DirectoryTreeNode): string[] {
    return [
        node.path,
        ...node.children.flatMap((child) => collectTreePaths(child)),
    ];
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
    const [collapsedDirectories, setCollapsedDirectories] = useState<
        Set<string>
    >(() => new Set<string>());
    const [expandedDirectories, setExpandedDirectories] = useState<Set<string>>(
        () =>
            new Set(
                ancestorPaths(
                    selectedDirectory ?? parentDirectory(files[0]?.path ?? "/"),
                ),
            ),
    );
    const initialDirectoryNotificationRef = useRef<string | undefined>(
        undefined,
    );
    const directoryGroups = useMemo(() => buildDirectoryGroups(files), [files]);
    const directoryTree = useMemo(() => buildDirectoryTree(files), [files]);
    const preferredDirectory =
        selectedDirectory ?? uncontrolledDirectory ?? directoryGroups[0]?.path;
    const activeDirectory = directoryGroups.find(
        (group) => group.path === preferredDirectory,
    );
    const activeFiles = activeDirectory?.files ?? [];
    const effectiveSelectedDirectory = preferredDirectory;
    const preferredSelectedPath = selectedPath ?? uncontrolledPath;
    const activeFile =
        activeFiles.find((file) => file.path === preferredSelectedPath) ??
        activeFiles[0];
    const effectiveSelectedPath = activeFile?.path;
    const displayedFiles = visibleFiles ?? activeFiles;
    const visibleExpandedDirectories = useMemo(() => {
        const next = new Set(expandedDirectories);

        for (const path of ancestorPaths(effectiveSelectedDirectory)) {
            if (!collapsedDirectories.has(path)) {
                next.add(path);
            }
        }

        return next;
    }, [collapsedDirectories, effectiveSelectedDirectory, expandedDirectories]);

    useEffect(() => {
        if (
            !effectiveSelectedDirectory ||
            !onSelectDirectory ||
            selectedDirectory !== undefined ||
            uncontrolledDirectory !== undefined
        ) {
            return;
        }

        if (
            initialDirectoryNotificationRef.current ===
            effectiveSelectedDirectory
        ) {
            return;
        }

        initialDirectoryNotificationRef.current = effectiveSelectedDirectory;
        onSelectDirectory(effectiveSelectedDirectory);
    }, [
        effectiveSelectedDirectory,
        onSelectDirectory,
        selectedDirectory,
        uncontrolledDirectory,
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

    const renderFileButton = (file: FileEntry, nested = false) => (
        <button
            type="button"
            className={cn(
                "flex w-full items-start gap-4 rounded-[1.25rem] border px-4 py-4 text-left transition",
                nested ? "min-h-[6.5rem]" : "",
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
            <span
                aria-hidden="true"
                className="mt-1 inline-flex size-8 shrink-0 items-center justify-center rounded-full border border-border/60 bg-background/80 text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground"
            >
                {file.kind.slice(0, 1)}
            </span>
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

    function renderDirectoryRows(
        nodes: DirectoryTreeNode[],
        depth = 0,
    ): ReactNode[] {
        return nodes.flatMap((node) => {
            const isExpanded = visibleExpandedDirectories.has(node.path);
            const isSelected = node.path === effectiveSelectedDirectory;
            const hasChildren = node.children.length > 0;
            const hasFiles = node.fileCount > 0;
            const rows: ReactNode[] = [
                <button
                    key={`dir-${node.path}`}
                    type="button"
                    className={cn(
                        "grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-[1.25rem] border px-4 py-3 text-left transition",
                        isSelected
                            ? "border-primary/45 bg-primary/10"
                            : "border-border/60 bg-background/60 hover:border-primary/35 hover:bg-background",
                    )}
                    data-depth={depth}
                    data-directory-expanded={String(isExpanded)}
                    data-directory-path={node.path}
                    onClick={() => {
                        const nextIsExpanded = !isExpanded;

                        setCollapsedDirectories((current) => {
                            const next = new Set(current);

                            if (nextIsExpanded) {
                                next.delete(node.path);
                            } else {
                                next.add(node.path);
                            }

                            return next;
                        });
                        setExpandedDirectories((current) => {
                            const next = new Set(current);

                            if (isExpanded) {
                                for (const path of collectTreePaths(node)) {
                                    next.delete(path);
                                }
                                for (const path of ancestorPaths(node.path)) {
                                    if (path !== node.path) {
                                        next.add(path);
                                    }
                                }
                            } else {
                                for (const path of ancestorPaths(node.path)) {
                                    next.add(path);
                                }
                            }

                            return next;
                        });

                        if (selectedDirectory === undefined) {
                            setUncontrolledDirectory(node.path);
                        }

                        onSelectDirectory?.(node.path);
                    }}
                    style={{ paddingLeft: `${depth * 1.2 + 1}rem` }}
                >
                    <span className="inline-flex size-6 items-center justify-center rounded-full border border-border/60 bg-background/80 text-muted-foreground">
                        {hasChildren || hasFiles ? (
                            isExpanded ? (
                                <ChevronDown
                                    className="size-4"
                                    aria-hidden="true"
                                />
                            ) : (
                                <ChevronRight
                                    className="size-4"
                                    aria-hidden="true"
                                />
                            )
                        ) : (
                            <span className="size-4" />
                        )}
                    </span>
                    <span className="min-w-0">
                        <span className="block truncate text-base font-medium text-foreground">
                            {node.label || directoryLabel(node.path)}
                        </span>
                        <span className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground">
                            <span>
                                {node.fileCount === 0
                                    ? hasChildren
                                        ? "Expand to browse"
                                        : "Empty folder"
                                    : `${node.fileCount} file${node.fileCount === 1 ? "" : "s"}`}
                            </span>
                            {node.totalSize > 0 ? (
                                <span>{formatBytes(node.totalSize)}</span>
                            ) : null}
                            {Object.keys(node.typeCounts).length > 0 ? (
                                <span className="uppercase tracking-[0.18em]">
                                    {formatTypeSummary(node.typeCounts)}
                                </span>
                            ) : null}
                        </span>
                    </span>
                    <span className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                        {hasChildren
                            ? `${node.children.length} subfolder${node.children.length === 1 ? "" : "s"}`
                            : "Folder"}
                    </span>
                </button>,
            ];

            if (
                isExpanded &&
                node.path === effectiveSelectedDirectory &&
                displayedFiles.length > 0
            ) {
                rows.push(
                    <div
                        key={`files-${node.path}`}
                        className={cn(
                            previewMode === "single"
                                ? "space-y-3"
                                : "space-y-3 xl:col-span-2",
                        )}
                        data-file-browser-directory-files={node.path}
                    >
                        {previewMode === "single"
                            ? displayedFiles.map((file) => (
                                  <div key={file.path}>
                                      {renderFileButton(file, true)}
                                  </div>
                              ))
                            : displayedFiles.map((file) => (
                                  <div
                                      key={file.path}
                                      className="grid gap-3 xl:grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] xl:items-start"
                                  >
                                      <div>{renderFileButton(file, true)}</div>
                                      <div
                                          className="min-w-0 rounded-[1.25rem] border border-border/60 bg-background/65 p-3"
                                          data-grid-preview-path={file.path}
                                      >
                                          {renderGridPreview?.(file) ?? null}
                                      </div>
                                  </div>
                              ))}
                    </div>,
                );
            }

            if (isExpanded && hasChildren) {
                rows.push(...renderDirectoryRows(node.children, depth + 1));
            }

            return rows;
        });
    }

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
                        {effectiveSelectedDirectory ?? "File Browser"}
                    </h2>
                    <p className="mt-2 text-sm text-muted-foreground">
                        {previewSummary ??
                            (activeFiles.length > 0
                                ? `Showing ${activeFiles.length} file${activeFiles.length === 1 ? "" : "s"} in this directory.`
                                : "Expand a folder row to browse its files.")}
                    </p>
                </div>

                <div className="space-y-3 xl:min-w-[24rem]">
                    <div className="flex flex-wrap items-center gap-3">
                        <label className="inline-flex items-center gap-3 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-sm text-foreground">
                            <input
                                aria-label="1 preview per row"
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
                                1 preview per row
                            </span>
                        </label>

                        <div className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-sm text-muted-foreground">
                            <ListFilter
                                className="size-4 text-primary"
                                aria-hidden="true"
                            />
                            {previewPageCount > 1
                                ? `Page ${previewPage} of ${previewPageCount}`
                                : previewMode === "grid"
                                  ? "Grid previews"
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

            {previewMode === "single" ? (
                <div className="mt-5 grid gap-5 xl:grid-cols-[minmax(20rem,0.92fr)_minmax(0,1.08fr)] xl:items-start">
                    <div className="min-w-0 rounded-[1.5rem] border border-border/70 bg-background/55 p-4">
                        <div className="flex items-center justify-between gap-3 border-b border-border/60 pb-3">
                            <div>
                                <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                    Explorer
                                </p>
                                <p className="mt-1 text-sm text-muted-foreground">
                                    Expand folders to reveal up to 100 paginated
                                    files.
                                </p>
                            </div>
                            {displayedFiles.length > 0 ? (
                                <span className="rounded-full border border-border/70 bg-background/80 px-3 py-1 text-xs uppercase tracking-[0.18em] text-muted-foreground">
                                    {displayedFiles.length} visible
                                </span>
                            ) : null}
                        </div>
                        <div className="mt-4 space-y-3">
                            {renderDirectoryRows(directoryTree)}
                        </div>
                    </div>

                    <div className="min-w-0 rounded-[1.5rem] border border-border/70 bg-background/55 p-4 xl:sticky xl:top-4">
                        <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                            Preview focus
                        </p>
                        <h3 className="mt-2 break-all text-xl font-semibold tracking-tight text-foreground">
                            {activeFile
                                ? fileName(activeFile.path)
                                : "Select a file to preview"}
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
                            {renderSinglePreview?.(activeFile ?? null) ?? null}
                        </div>
                    </div>
                </div>
            ) : (
                <div
                    className="mt-5 rounded-[1.5rem] border border-border/70 bg-background/55 p-4"
                    data-preview-mode="grid"
                >
                    <div className="flex items-center justify-between gap-3 border-b border-border/60 pb-3">
                        <div>
                            <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                Explorer
                            </p>
                            <p className="mt-1 text-sm text-muted-foreground">
                                1 preview per row at {previewHeight}px high.
                            </p>
                        </div>
                        {displayedFiles.length > 0 ? (
                            <span className="rounded-full border border-border/70 bg-background/80 px-3 py-1 text-xs uppercase tracking-[0.18em] text-muted-foreground">
                                {displayedFiles.length} visible
                            </span>
                        ) : null}
                    </div>

                    <div className="mt-4 space-y-3">
                        {renderDirectoryRows(directoryTree)}
                    </div>
                </div>
            )}
        </section>
    );
}
