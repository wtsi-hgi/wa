"use client";

import { useMemo, useState } from "react";
import {
    ChevronDown,
    ChevronRight,
    File as FileIcon,
    FolderTree,
} from "lucide-react";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { type FileEntry } from "@/lib/contracts";
import { cn, formatBytes } from "@/lib/utils";

type FileBrowserProps = {
    files: FileEntry[];
    onSelectFile: (file: FileEntry) => void;
    selectedPath?: string;
};

type FileKind = FileEntry["kind"];

type TreeNode = {
    children: TreeNode[];
    fileCount: number;
    isDir: boolean;
    mtime?: string;
    name: string;
    path: string;
    size?: number;
    typeCounts: Record<string, number>;
};

const fileKindOrder: FileKind[] = ["output", "input", "pipeline"];

const fileKindLabels: Record<FileKind, string> = {
    input: "Inputs",
    output: "Outputs",
    pipeline: "Pipeline",
};

function compareNodes(left: TreeNode, right: TreeNode): number {
    if (left.isDir !== right.isDir) {
        return left.isDir ? -1 : 1;
    }

    return left.name.localeCompare(right.name);
}

function toTypeKey(path: string): string {
    const name = path.split("/").pop() ?? path;
    const extensionIndex = name.lastIndexOf(".");

    if (extensionIndex <= 0 || extensionIndex === name.length - 1) {
        return "file";
    }

    return name.slice(extensionIndex + 1).toLowerCase();
}

function joinPath(parentPath: string, segment: string): string {
    return `${parentPath}/${segment}`.replace(/\/+/g, "/");
}

function mergeTypeCounts(
    target: Record<string, number>,
    source: Record<string, number>,
) {
    for (const [type, count] of Object.entries(source)) {
        target[type] = (target[type] ?? 0) + count;
    }
}

function aggregateTree(node: TreeNode): TreeNode {
    if (!node.isDir) {
        return node;
    }

    node.children = node.children.map(aggregateTree).sort(compareNodes);
    node.fileCount = 0;
    node.typeCounts = {};

    for (const child of node.children) {
        node.fileCount += child.fileCount;
        mergeTypeCounts(node.typeCounts, child.typeCounts);
    }

    return node;
}

export function buildFileTree(files: FileEntry[]): TreeNode[] {
    const roots: TreeNode[] = [];

    for (const file of [...files].sort((left, right) =>
        left.path.localeCompare(right.path),
    )) {
        const segments = file.path.split("/").filter(Boolean);

        if (segments.length === 0) {
            continue;
        }

        let currentChildren = roots;
        let parentPath = "";

        for (const [index, segment] of segments.entries()) {
            const path = joinPath(parentPath, segment);
            const isLeaf = index === segments.length - 1;

            if (isLeaf) {
                currentChildren.push({
                    children: [],
                    fileCount: 1,
                    isDir: false,
                    mtime: file.mtime,
                    name: segment,
                    path,
                    size: file.size,
                    typeCounts: { [toTypeKey(file.path)]: 1 },
                });
                break;
            }

            let folderNode = currentChildren.find(
                (node) => node.isDir && node.path === path,
            );

            if (!folderNode) {
                folderNode = {
                    children: [],
                    fileCount: 0,
                    isDir: true,
                    name: segment,
                    path,
                    typeCounts: {},
                };
                currentChildren.push(folderNode);
            }

            currentChildren = folderNode.children;
            parentPath = path;
        }
    }

    return roots.map(aggregateTree).sort(compareNodes);
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

function formatTypeSummary(typeCounts: Record<string, number>): string {
    const entries = Object.entries(typeCounts);

    if (entries.length === 0) {
        return "No files";
    }

    return entries
        .sort(
            (left, right) => right[1] - left[1] || left[0].localeCompare(right[0]),
        )
        .map(([type, count]) => `${count} ${type}`)
        .join(", ");
}

function collectAutoExpandedPaths(nodes: TreeNode[]): string[] {
    const expandedPaths: string[] = [];

    for (const node of nodes) {
        if (!node.isDir) {
            continue;
        }

        if (node.children.length === 1) {
            expandedPaths.push(node.path);
        }

        expandedPaths.push(...collectAutoExpandedPaths(node.children));
    }

    return expandedPaths;
}

type TreeBranchProps = {
    depth?: number;
    expandedPaths: Set<string>;
    filesByPath: Map<string, FileEntry>;
    onSelectFile: (file: FileEntry) => void;
    onToggleFolder: (path: string) => void;
    selectedPath?: string;
    nodes: TreeNode[];
};

function TreeBranch({
    depth = 0,
    expandedPaths,
    filesByPath,
    onSelectFile,
    onToggleFolder,
    selectedPath,
    nodes,
}: TreeBranchProps) {
    return (
        <ul className="space-y-2">
            {nodes.map((node) => {
                if (node.isDir) {
                    const isExpanded = expandedPaths.has(node.path);

                    return (
                        <li key={node.path}>
                            <button
                                type="button"
                                aria-expanded={isExpanded}
                                className="flex w-full items-start gap-3 rounded-2xl border border-border/60 bg-background/70 px-4 py-3 text-left transition hover:border-primary/35 hover:bg-background"
                                data-folder-path={node.path}
                                onClick={() => onToggleFolder(node.path)}
                            >
                                <span
                                    className="mt-0.5 text-muted-foreground"
                                    aria-hidden="true"
                                >
                                    {isExpanded ? (
                                        <ChevronDown className="size-4" />
                                    ) : (
                                        <ChevronRight className="size-4" />
                                    )}
                                </span>
                                <span className="mt-0.5 text-primary" aria-hidden="true">
                                    <FolderTree className="size-4" />
                                </span>
                                <span className="min-w-0 flex-1">
                                    <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                                        <span className="font-medium text-foreground">
                                            {node.name}
                                        </span>
                                        <span className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                                            {node.fileCount} file{node.fileCount === 1 ? "" : "s"}
                                        </span>
                                        <span className="text-sm text-muted-foreground">
                                            {formatTypeSummary(node.typeCounts)}
                                        </span>
                                    </span>
                                </span>
                            </button>

                            {isExpanded ? (
                                <div
                                    className="mt-2 pl-4"
                                    style={{ paddingLeft: `${(depth + 1) * 0.85}rem` }}
                                >
                                    <TreeBranch
                                        depth={depth + 1}
                                        expandedPaths={expandedPaths}
                                        filesByPath={filesByPath}
                                        nodes={node.children}
                                        onSelectFile={onSelectFile}
                                        onToggleFolder={onToggleFolder}
                                        selectedPath={selectedPath}
                                    />
                                </div>
                            ) : null}
                        </li>
                    );
                }

                const file = filesByPath.get(node.path);

                return (
                    <li key={node.path}>
                        <button
                            type="button"
                            className={cn(
                                "flex w-full items-start gap-3 rounded-2xl border px-4 py-3 text-left transition",
                                selectedPath === node.path
                                    ? "border-primary/45 bg-primary/10"
                                    : "border-border/60 bg-background/60 hover:border-primary/35 hover:bg-background",
                            )}
                            data-file-path={node.path}
                            onClick={() => {
                                if (file) {
                                    onSelectFile(file);
                                }
                            }}
                        >
                            <span className="mt-0.5 text-primary" aria-hidden="true">
                                <FileIcon className="size-4" />
                            </span>
                            <span className="min-w-0 flex-1">
                                <span className="block truncate font-medium text-foreground">
                                    {node.name}
                                </span>
                                <span className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground">
                                    <span>{formatBytes(node.size)}</span>
                                    <span>{formatMtime(node.mtime)}</span>
                                </span>
                            </span>
                        </button>
                    </li>
                );
            })}
        </ul>
    );
}

export function FileBrowser({
    files,
    onSelectFile,
    selectedPath,
}: FileBrowserProps) {
    const [activeTab, setActiveTab] = useState<FileKind>("output");

    const filesByKind = useMemo<Record<FileKind, FileEntry[]>>(
        () => ({
            input: files.filter((file) => file.kind === "input"),
            output: files.filter((file) => file.kind === "output"),
            pipeline: files.filter((file) => file.kind === "pipeline"),
        }),
        [files],
    );

    const treesByKind = useMemo<Record<FileKind, TreeNode[]>>(
        () => ({
            input: buildFileTree(filesByKind.input),
            output: buildFileTree(filesByKind.output),
            pipeline: buildFileTree(filesByKind.pipeline),
        }),
        [filesByKind],
    );

    const autoExpandedPaths = useMemo(
        () =>
            fileKindOrder
                .map((kind) => treesByKind[kind])
                .flatMap((nodes) => collectAutoExpandedPaths(nodes)),
        [treesByKind],
    );

    const [expansionOverrides, setExpansionOverrides] = useState<
        Record<string, boolean>
    >({});

    const expandedPaths = useMemo(() => {
        const next = new Set(autoExpandedPaths);

        for (const [path, isExpanded] of Object.entries(expansionOverrides)) {
            if (isExpanded) {
                next.add(path);
            } else {
                next.delete(path);
            }
        }

        return next;
    }, [autoExpandedPaths, expansionOverrides]);

    function toggleFolder(path: string) {
        setExpansionOverrides((current) => ({
            ...current,
            [path]: !expandedPaths.has(path),
        }));
    }

    return (
        <section className="rounded-[1.75rem] border border-border/70 bg-card/85 p-4 shadow-[0_28px_90px_-72px_rgba(48,67,98,0.9)] sm:p-5">
            <Tabs
                defaultValue="output"
                value={activeTab}
                onValueChange={(nextValue) => {
                    if (fileKindOrder.includes(nextValue as FileKind)) {
                        setActiveTab(nextValue as FileKind);
                    }
                }}
            >
                <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                    <div>
                        <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                            File browser
                        </p>
                        <h2 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
                            Inspect result assets by source
                        </h2>
                    </div>
                    <TabsList className="sm:w-auto">
                        {fileKindOrder.map((kind) => (
                            <TabsTrigger key={kind} value={kind}>
                                {fileKindLabels[kind]}
                            </TabsTrigger>
                        ))}
                    </TabsList>
                </div>

                {fileKindOrder.map((kind) => {
                    const tree = treesByKind[kind];
                    const filesForKind = filesByKind[kind];
                    const filesByPath = new Map(
                        filesForKind.map((file) => [file.path, file]),
                    );

                    return (
                        <TabsContent key={kind} value={kind}>
                            {tree.length === 0 ? (
                                <div className="rounded-[1.5rem] border border-dashed border-border/70 bg-background/40 px-5 py-8 text-sm text-muted-foreground">
                                    No {kind} files
                                </div>
                            ) : (
                                <TreeBranch
                                    expandedPaths={expandedPaths}
                                    filesByPath={filesByPath}
                                    nodes={tree}
                                    onSelectFile={onSelectFile}
                                    onToggleFolder={toggleFolder}
                                    selectedPath={selectedPath}
                                />
                            )}
                        </TabsContent>
                    );
                })}
            </Tabs>
        </section>
    );
}
