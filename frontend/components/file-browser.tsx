"use client";

import {
    cloneElement,
    type CSSProperties,
    memo,
    type ReactNode,
    useEffect,
    useMemo,
    useRef,
    useState,
} from "react";
import {
    ChevronDown,
    ChevronRight,
    Command as CommandIcon,
    Eye,
    FolderTree,
    GalleryHorizontal,
    ListFilter,
    ListTree,
    PanelTop,
    Table2,
    type LucideIcon,
} from "lucide-react";

import { PreviewPagination } from "@/components/preview-pagination";
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
    descendantDirectoryCount: number;
    descendantFileCount: number;
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

export type SubdirPreviewKind =
    | "image"
    | "table"
    | "markdown"
    | "code"
    | "document";

const subdirPreviewKindGroups: ReadonlyArray<{
    extensions: ReadonlyArray<string>;
    id: SubdirPreviewKind;
    label: string;
}> = [
    {
        extensions: [
            "avif",
            "bmp",
            "gif",
            "jpeg",
            "jpg",
            "png",
            "svg",
            "tif",
            "tiff",
            "webp",
        ],
        id: "image",
        label: "Images",
    },
    {
        extensions: ["csv", "tsv"],
        id: "table",
        label: "Tables",
    },
    {
        extensions: ["markdown", "md"],
        id: "markdown",
        label: "Markdown",
    },
    {
        extensions: [
            "htm",
            "html",
            "json",
            "log",
            "py",
            "txt",
            "xml",
            "yaml",
            "yml",
        ],
        id: "code",
        label: "Text & code",
    },
    {
        extensions: ["pdf"],
        id: "document",
        label: "Documents",
    },
];

const SUBDIR_PREVIEW_PAGE_SIZE = 20;
const compressedExtensions = new Set(["gz"]);
const allSubdirPreviewKinds = new Set<SubdirPreviewKind>(
    subdirPreviewKindGroups.map((group) => group.id),
);
const defaultSubdirPreviewKinds = new Set<SubdirPreviewKind>(
    allSubdirPreviewKinds,
);

function effectiveExtension(path: string): string {
    const name = path.split("/").pop() ?? path;
    const parts = name
        .split(".")
        .slice(1)
        .map((part) => part.toLowerCase())
        .filter((part) => part.length > 0);

    if (parts.length === 0) {
        return "";
    }

    const last = parts.at(-1) ?? "";

    if (compressedExtensions.has(last) && parts.length > 1) {
        return parts.at(-2) ?? last;
    }

    return last;
}

function previewKindForPath(path: string): SubdirPreviewKind | null {
    const extension = effectiveExtension(path);

    for (const group of subdirPreviewKindGroups) {
        if (group.extensions.includes(extension)) {
            return group.id;
        }
    }

    return null;
}

function pathSupportsFilePreview(path: string): boolean {
    return previewKindForPath(path) !== null;
}

export function findInitialSubdirPreviewDirectory(
    files: FileEntry[],
): string | undefined {
    return findInitialSubdirPreviewDirectoryInTree(
        buildDirectoryTree(files),
        allSubdirPreviewKinds,
    );
}

function findInitialSubdirPreviewDirectoryInTree(
    nodes: ReadonlyArray<DirectoryTreeNode>,
    kinds: ReadonlySet<SubdirPreviewKind>,
): string | undefined {
    for (const node of nodes) {
        if (qualifyingSubdirsFor(node, kinds).length > 1) {
            return node.path;
        }

        const nestedMatch = findInitialSubdirPreviewDirectoryInTree(
            node.children,
            kinds,
        );

        if (nestedMatch) {
            return nestedMatch;
        }
    }

    return undefined;
}

function collectSubtreeFiles(node: DirectoryTreeNode): FileEntry[] {
    return [
        ...node.files,
        ...node.children.flatMap((child) => collectSubtreeFiles(child)),
    ];
}

function findTreeNodeByPath(
    nodes: DirectoryTreeNode[],
    path: string,
): DirectoryTreeNode | undefined {
    for (const node of nodes) {
        if (node.path === path) {
            return node;
        }

        const ancestorMatch = pathMatchesNodeChain(node, path);

        if (ancestorMatch) {
            return ancestorMatch;
        }
    }

    return undefined;
}

function pathMatchesNodeChain(
    node: DirectoryTreeNode,
    path: string,
): DirectoryTreeNode | undefined {
    if (path === node.path) {
        return node;
    }

    if (!path.startsWith(`${node.path}/`) && node.path !== "/") {
        return undefined;
    }

    return findTreeNodeByPath(node.children, path);
}

function qualifyingSubdirsFor(
    node: DirectoryTreeNode | undefined,
    kinds: ReadonlySet<SubdirPreviewKind>,
): DirectoryTreeNode[] {
    if (!node || kinds.size === 0) {
        return [];
    }

    return node.children.filter(
        (child) =>
            (parentDirectory(child.path) === node.path ||
                node.fileCount === 0) &&
            previewableFilesForKinds(child, kinds).length > 0,
    );
}

function previewableFilesForKinds(
    node: DirectoryTreeNode,
    kinds: ReadonlySet<SubdirPreviewKind>,
): FileEntry[] {
    return collectSubtreeFiles(node).filter((file) => {
        const kind = previewKindForPath(file.path);

        return kind !== null && kinds.has(kind);
    });
}

function summarizeSubdirPreviewKinds(
    kinds: ReadonlySet<SubdirPreviewKind>,
): string {
    const selectedGroups = subdirPreviewKindGroups.filter((group) =>
        kinds.has(group.id),
    );

    if (selectedGroups.length === 0) {
        return "No file types";
    }

    if (selectedGroups.length === 1) {
        return selectedGroups[0]?.label ?? "1 file type";
    }

    return `${selectedGroups.length} file types`;
}

function summarizePreviewModes(
    previewMode: PreviewMode,
    subdirPreviewEnabled: boolean,
    showGridToggle: boolean,
): string {
    if (showGridToggle && previewMode === "grid" && subdirPreviewEnabled) {
        return "Grid + subfolders";
    }

    if (showGridToggle && previewMode === "grid") {
        return "1 per row";
    }

    if (subdirPreviewEnabled) {
        return "Subfolders";
    }

    return "Single preview";
}

function fileMatchesPreviewKinds(
    file: FileEntry,
    kinds: ReadonlySet<SubdirPreviewKind>,
): boolean {
    const kind = previewKindForPath(file.path);

    return kind !== null && kinds.has(kind);
}

type FileBrowserProps = {
    files: FileEntry[];
    onPreviewHeightChange?: (value: number) => void;
    onPreviewModeChange?: (mode: PreviewMode) => void;
    onPreviewPageChange?: (page: number) => void;
    onSelectDirectory?: (
        path: string,
        options?: { expanded: boolean; parentPath?: string },
    ) => void;
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

type FileBrowserDesignKey =
    | "classic"
    | "inline"
    | "sidecar"
    | "ribbon"
    | "matrix"
    | "deck";

type FileBrowserControlPlacement =
    | "classic-row"
    | "name-area"
    | "sidecar"
    | "content-ribbon"
    | "matrix-header"
    | "preview-dock";

type FileBrowserDesign = {
    Icon: LucideIcon;
    contextClass: string;
    controlLabelClass: string;
    controlMenuClass: string;
    controlMenuHeadingClass: string;
    controlStyle: string;
    controlTriggerClass: string;
    description: string;
    directoryButtonClass: string;
    directoryChevronClass: string;
    directoryContentClass: string;
    directoryGroupClass: string;
    directoryMetaClass: string;
    directoryRowBaseClass: string;
    directoryRowCollapsedClass: string;
    directoryRowIdleClass: string;
    directoryRowSelectedClass: string;
    directoryRowWithContentClass: string;
    directoryTagClass: string;
    emptyStateClass: string;
    fileButtonBaseClass: string;
    fileButtonCompactClass: string;
    fileButtonSelectedClass: string;
    fileGlyphClass: string;
    fileListGridClass: string;
    fileListSingleClass: string;
    fileListSinglePreviewClass: string;
    fileMetaClass: string;
    fileNameClass: string;
    folderControlsClass: string;
    gridFileCellClass: string;
    gridPreviewCellClass: string;
    gridRowClass: string;
    headerClass: string;
    headerIconClass: string;
    headerTitleClass: string;
    id: FileBrowserDesignKey;
    label: string;
    pageBadgeClass: string;
    paginationClass: string;
    previewHeightControlClass: string;
    sectionClass: string;
    selectorClass: string;
    selectorOptionClass: (active: boolean) => string;
    shortLabel: string;
    singlePreviewClass: string;
    subdirCardBaseClass: string;
    subdirFilenameClass: string;
    subdirFrameBaseClass: string;
    subdirImageCardClass: string;
    subdirImageFrameClass: string;
    subdirStripClass: string;
    subdirStripWrapperClass: string;
    subdirTextCardClass: string;
    subdirTextFrameClass: string;
    treeInnerClass: string;
    treeShellClass: string;
};

const selectorOptionBaseClass =
    "inline-flex h-8 min-w-8 items-center justify-center gap-1.5 px-2 text-xs font-medium transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

const fileBrowserDesigns: FileBrowserDesign[] = [
    {
        Icon: FolderTree,
        contextClass: "",
        controlLabelClass: "flex items-center justify-between gap-3 text-sm",
        controlMenuClass:
            "absolute left-0 z-20 mt-2 min-w-56 rounded-[1.25rem] border border-border/70 bg-[var(--popover)] p-3 shadow-lg",
        controlMenuHeadingClass:
            "mb-2 text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground",
        controlStyle: "classic-control",
        controlTriggerClass:
            "inline-flex cursor-pointer list-none items-center gap-2 rounded-full border border-border/70 bg-[var(--popover)] px-3 py-2 text-foreground marker:hidden",
        description: "Current rounded folder rows",
        directoryButtonClass:
            "grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-[1rem] px-3 py-3 text-left transition hover:bg-background/55",
        directoryChevronClass:
            "inline-flex size-6 items-center justify-center rounded-full border border-border/60 bg-background/80 text-muted-foreground",
        directoryContentClass: "space-y-3 px-3 pb-3 pt-1",
        directoryGroupClass: "space-y-2",
        directoryMetaClass:
            "mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground",
        directoryRowBaseClass:
            "grid w-full grid-cols-1 gap-3 rounded-[1.25rem] border transition",
        directoryRowCollapsedClass: "grid-cols-1 p-0",
        directoryRowIdleClass:
            "border-border/60 bg-background/60 hover:border-primary/35 hover:bg-background",
        directoryRowSelectedClass: "border-primary/45 bg-primary/10",
        directoryRowWithContentClass: "p-2",
        directoryTagClass:
            "text-xs uppercase tracking-[0.18em] text-muted-foreground",
        emptyStateClass:
            "mt-5 rounded-[1.5rem] border border-dashed border-border/70 bg-background/40 px-4 py-8 text-center text-sm text-muted-foreground",
        fileButtonBaseClass:
            "flex w-full items-start gap-3 rounded-[1rem] border border-border/60 bg-background/70 px-3 py-3 text-left transition hover:border-primary/40 hover:bg-background",
        fileButtonCompactClass: "h-full min-w-0",
        fileButtonSelectedClass: "border-primary/45 bg-primary/10",
        fileGlyphClass:
            "mt-1 inline-flex size-8 shrink-0 items-center justify-center rounded-full border border-border/60 bg-background/80 text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground",
        fileListGridClass: "space-y-3 xl:col-span-2",
        fileListSingleClass: "space-y-3",
        fileListSinglePreviewClass:
            "grid gap-3 grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] items-start",
        fileMetaClass:
            "mt-2 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground",
        fileNameClass: "block truncate text-base font-medium text-foreground",
        folderControlsClass:
            "file-browser-control-surface classic-control-surface flex w-full flex-wrap items-center justify-start gap-2 px-3 pb-3 text-sm",
        gridFileCellClass: "min-w-0 border-r border-border/60 pr-3",
        gridPreviewCellClass: "min-w-0",
        gridRowClass:
            "grid gap-3 grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] items-start",
        headerClass: "flex items-center gap-3 border-b border-border/60 pb-5",
        headerIconClass: "size-4 text-primary",
        headerTitleClass:
            "text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground",
        id: "classic",
        label: "Current",
        pageBadgeClass:
            "inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-2 py-1.5 text-muted-foreground",
        paginationClass:
            "inline-flex items-center gap-1 rounded-full border border-border/70 bg-background/75 p-1",
        previewHeightControlClass:
            "inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-foreground",
        sectionClass:
            "rounded-[1.75rem] border border-border/70 bg-card/85 p-4 shadow-[0_28px_90px_-72px_rgba(48,67,98,0.9)] sm:p-5",
        selectorClass:
            "ml-auto inline-flex flex-wrap items-center gap-1 rounded-full border border-border/70 bg-background/70 p-1",
        selectorOptionClass: (active) =>
            cn(
                selectorOptionBaseClass,
                "rounded-full",
                active
                    ? "bg-primary text-primary-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground",
            ),
        shortLabel: "Current",
        singlePreviewClass:
            "sticky top-4 z-10 min-w-0 col-start-2 row-start-1 self-start",
        subdirCardBaseClass: "inline-flex max-w-full shrink-0 flex-col gap-2",
        subdirFilenameClass:
            "truncate text-xs font-medium text-muted-foreground",
        subdirFrameBaseClass: "inline-flex max-w-full overflow-hidden",
        subdirImageCardClass: "w-full",
        subdirImageFrameClass: "w-full items-start justify-center",
        subdirStripClass:
            "flex w-full min-w-0 items-start gap-4 overflow-x-auto pb-1",
        subdirStripWrapperClass: "px-3 pb-3",
        subdirTextCardClass: "w-fit",
        subdirTextFrameClass:
            "w-fit items-stretch [&_button]:max-w-none [&_button]:justify-start [&_button]:w-auto [&_img]:max-w-none [&_img]:w-auto",
        treeInnerClass: "space-y-3",
        treeShellClass:
            "mt-5 rounded-[1.5rem] border border-border/70 bg-background/55 p-4",
    },
    {
        Icon: PanelTop,
        contextClass:
            "mt-4 flex flex-wrap items-center gap-2 rounded-lg border border-border/70 bg-background px-3 py-2 text-xs text-muted-foreground",
        controlLabelClass:
            "flex items-center justify-between gap-3 rounded-md px-1 py-0.5 text-sm",
        controlMenuClass:
            "absolute left-0 z-20 mt-2 min-w-56 rounded-lg border border-border bg-[var(--popover)] p-2 shadow-[0_16px_40px_-24px_rgba(28,40,58,0.65)]",
        controlMenuHeadingClass:
            "mb-2 border-b border-border/60 px-1 pb-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-muted-foreground",
        controlStyle: "inline-nameplate",
        controlTriggerClass:
            "inline-flex cursor-pointer list-none items-center gap-2 rounded-md border border-border/80 bg-background px-2.5 py-1.5 text-foreground shadow-sm marker:hidden hover:bg-muted/70",
        description: "Controls folded into the active folder nameplate",
        directoryButtonClass:
            "grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-md px-3 py-2.5 text-left transition hover:bg-muted/60",
        directoryChevronClass:
            "inline-flex size-6 items-center justify-center rounded-md border border-border/70 bg-muted text-muted-foreground",
        directoryContentClass: "space-y-2 px-2 pb-2 pt-0",
        directoryGroupClass: "space-y-2",
        directoryMetaClass:
            "mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground",
        directoryRowBaseClass:
            "grid w-full grid-cols-1 gap-2 rounded-lg border transition",
        directoryRowCollapsedClass: "p-0",
        directoryRowIdleClass:
            "border-border/70 bg-background/70 hover:border-primary/35 hover:bg-muted/30",
        directoryRowSelectedClass:
            "border-primary/45 bg-primary/10 shadow-[inset_0_1px_0_rgba(255,255,255,0.45)]",
        directoryRowWithContentClass: "p-1.5",
        directoryTagClass:
            "text-[11px] uppercase tracking-[0.16em] text-muted-foreground",
        emptyStateClass:
            "mt-4 rounded-lg border border-dashed border-border/70 bg-background px-4 py-8 text-center text-sm text-muted-foreground",
        fileButtonBaseClass:
            "flex w-full items-start gap-3 rounded-md border border-border/60 bg-background px-3 py-2.5 text-left transition hover:border-primary/40 hover:bg-muted/40",
        fileButtonCompactClass: "h-full min-w-0",
        fileButtonSelectedClass: "border-primary/45 bg-primary/10",
        fileGlyphClass:
            "mt-0.5 inline-flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground",
        fileListGridClass: "space-y-2 xl:col-span-2",
        fileListSingleClass: "space-y-2",
        fileListSinglePreviewClass:
            "grid gap-2 grid-cols-[minmax(18rem,0.86fr)_minmax(0,1.14fr)] items-start",
        fileMetaClass:
            "mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground",
        fileNameClass: "block truncate text-sm font-semibold text-foreground",
        folderControlsClass:
            "file-browser-control-surface inline-nameplate-controls flex w-full min-w-0 flex-wrap items-center justify-start gap-1.5 rounded-md border border-border/80 bg-muted/55 px-2 py-1.5 text-sm shadow-inner",
        gridFileCellClass: "min-w-0 border-r border-border/60 pr-2",
        gridPreviewCellClass: "min-w-0",
        gridRowClass:
            "grid gap-2 grid-cols-[minmax(18rem,0.86fr)_minmax(0,1.14fr)] items-start",
        headerClass:
            "flex flex-wrap items-center gap-3 border-b border-border/70 pb-4",
        headerIconClass: "size-4 text-primary",
        headerTitleClass:
            "text-sm font-semibold uppercase tracking-[0.18em] text-muted-foreground",
        id: "inline",
        label: "Inline Nameplate",
        pageBadgeClass:
            "inline-flex items-center gap-2 rounded-md border border-border/70 bg-background px-2 py-1.5 text-muted-foreground",
        paginationClass:
            "inline-flex items-center gap-1 rounded-md border border-border/70 bg-background p-1",
        previewHeightControlClass:
            "inline-flex items-center gap-2 rounded-md border border-border/70 bg-background px-2.5 py-1.5 text-foreground",
        sectionClass:
            "rounded-xl border border-border/75 bg-card p-3 shadow-[0_18px_60px_-48px_rgba(48,67,98,0.8)] sm:p-4",
        selectorClass:
            "ml-auto inline-flex flex-wrap items-center gap-1 rounded-lg border border-border/70 bg-background p-1",
        selectorOptionClass: (active) =>
            cn(
                selectorOptionBaseClass,
                "rounded-md",
                active
                    ? "bg-primary text-primary-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground",
            ),
        shortLabel: "Inline",
        singlePreviewClass:
            "sticky top-4 z-10 min-w-0 col-start-2 row-start-1 self-start",
        subdirCardBaseClass: "inline-flex max-w-full shrink-0 flex-col gap-1.5",
        subdirFilenameClass:
            "truncate text-[11px] font-semibold text-muted-foreground",
        subdirFrameBaseClass: "inline-flex max-w-full overflow-hidden",
        subdirImageCardClass: "w-full",
        subdirImageFrameClass: "w-full items-start justify-center",
        subdirStripClass:
            "flex w-full min-w-0 items-start gap-3 overflow-x-auto pb-1",
        subdirStripWrapperClass: "px-2 pb-2",
        subdirTextCardClass: "w-fit",
        subdirTextFrameClass:
            "w-fit items-stretch [&_button]:max-w-none [&_button]:justify-start [&_button]:w-auto [&_img]:max-w-none [&_img]:w-auto",
        treeInnerClass: "space-y-2",
        treeShellClass:
            "mt-4 rounded-lg border border-border/70 bg-background/65 p-2",
    },
    {
        Icon: ListTree,
        contextClass:
            "mt-4 flex flex-wrap items-center gap-2 rounded-r-xl border border-l-4 border-border/70 border-l-primary bg-background/80 px-3 py-2 text-xs text-muted-foreground",
        controlLabelClass:
            "flex items-center justify-between gap-3 rounded-lg px-1 py-0.5 text-sm",
        controlMenuClass:
            "absolute left-0 z-20 mt-2 min-w-56 rounded-r-xl rounded-l-md border border-l-4 border-border/70 border-l-primary bg-[var(--popover)] p-3 shadow-lg",
        controlMenuHeadingClass:
            "mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-primary",
        controlStyle: "sidecar-utility",
        controlTriggerClass:
            "inline-flex cursor-pointer list-none items-center gap-2 rounded-r-lg rounded-l-sm border border-l-4 border-border/70 border-l-primary bg-[var(--popover)] px-3 py-2 text-foreground marker:hidden",
        description: "Expanded folders get a utility sidecar",
        directoryButtonClass:
            "grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-r-lg rounded-l-sm px-3 py-3 text-left transition hover:bg-background/70",
        directoryChevronClass:
            "inline-flex size-6 items-center justify-center rounded-r-md rounded-l-sm border border-l-2 border-border/70 border-l-primary/45 bg-background text-muted-foreground",
        directoryContentClass: "space-y-3 px-3 pb-3 pt-0",
        directoryGroupClass: "space-y-2",
        directoryMetaClass:
            "mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground",
        directoryRowBaseClass:
            "grid w-full grid-cols-1 gap-3 rounded-r-xl rounded-l-sm border border-l-4 transition",
        directoryRowCollapsedClass: "p-0",
        directoryRowIdleClass:
            "border-border/60 border-l-accent bg-background/55 hover:border-primary/35 hover:border-l-primary hover:bg-background",
        directoryRowSelectedClass:
            "border-primary/45 border-l-primary bg-primary/10",
        directoryRowWithContentClass: "p-2",
        directoryTagClass:
            "text-xs uppercase tracking-[0.18em] text-muted-foreground",
        emptyStateClass:
            "mt-5 rounded-r-xl rounded-l-sm border border-l-4 border-dashed border-border/70 border-l-primary bg-background/40 px-4 py-8 text-center text-sm text-muted-foreground",
        fileButtonBaseClass:
            "flex w-full items-start gap-3 rounded-r-lg rounded-l-sm border border-l-4 border-border/60 border-l-muted bg-background/70 px-3 py-3 text-left transition hover:border-primary/40 hover:border-l-primary hover:bg-background",
        fileButtonCompactClass: "h-full min-w-0",
        fileButtonSelectedClass:
            "border-primary/45 border-l-primary bg-primary/10",
        fileGlyphClass:
            "mt-1 inline-flex size-8 shrink-0 items-center justify-center rounded-r-md rounded-l-sm border border-border/60 bg-background text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground",
        fileListGridClass: "space-y-3 xl:col-span-2",
        fileListSingleClass: "space-y-3",
        fileListSinglePreviewClass:
            "grid gap-3 grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] items-start",
        fileMetaClass:
            "mt-2 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground",
        fileNameClass: "block truncate text-base font-medium text-foreground",
        folderControlsClass:
            "file-browser-control-surface sidecar-controls flex w-full min-w-0 flex-col items-stretch justify-start gap-2 rounded-r-lg rounded-l-sm border border-l-4 border-border/70 border-l-primary bg-background/80 px-3 py-2 text-sm",
        gridFileCellClass: "min-w-0 border-r border-border/60 pr-3",
        gridPreviewCellClass: "min-w-0",
        gridRowClass:
            "grid gap-3 grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] items-start",
        headerClass:
            "flex flex-wrap items-center gap-3 border-b border-border/60 pb-5",
        headerIconClass: "size-4 text-primary",
        headerTitleClass:
            "text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground",
        id: "sidecar",
        label: "Utility Sidecar",
        pageBadgeClass:
            "inline-flex items-center gap-2 rounded-r-lg rounded-l-sm border border-l-4 border-border/70 border-l-primary bg-background/75 px-2 py-1.5 text-muted-foreground",
        paginationClass:
            "inline-flex items-center gap-1 rounded-r-lg rounded-l-sm border border-border/70 bg-background/75 p-1",
        previewHeightControlClass:
            "inline-flex items-center gap-2 rounded-r-lg rounded-l-sm border border-l-4 border-border/70 border-l-primary bg-background/75 px-3 py-2 text-foreground",
        sectionClass:
            "rounded-r-[1.5rem] rounded-l-lg border border-l-4 border-border/70 border-l-primary bg-card/85 p-4 shadow-[0_28px_90px_-72px_rgba(48,67,98,0.9)] sm:p-5",
        selectorClass:
            "ml-auto inline-flex flex-wrap items-center gap-1 rounded-r-lg rounded-l-sm border border-l-4 border-border/70 border-l-primary bg-background/70 p-1",
        selectorOptionClass: (active) =>
            cn(
                selectorOptionBaseClass,
                "rounded-md",
                active
                    ? "bg-primary text-primary-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground",
            ),
        shortLabel: "Sidecar",
        singlePreviewClass:
            "sticky top-4 z-10 min-w-0 col-start-2 row-start-1 self-start",
        subdirCardBaseClass: "inline-flex max-w-full shrink-0 flex-col gap-2",
        subdirFilenameClass:
            "truncate text-xs font-medium text-muted-foreground",
        subdirFrameBaseClass: "inline-flex max-w-full overflow-hidden",
        subdirImageCardClass: "w-full",
        subdirImageFrameClass: "w-full items-start justify-center",
        subdirStripClass:
            "flex w-full min-w-0 items-start gap-4 overflow-x-auto pb-1",
        subdirStripWrapperClass: "px-3 pb-3",
        subdirTextCardClass: "w-fit",
        subdirTextFrameClass:
            "w-fit items-stretch [&_button]:max-w-none [&_button]:justify-start [&_button]:w-auto [&_img]:max-w-none [&_img]:w-auto",
        treeInnerClass: "space-y-3",
        treeShellClass:
            "mt-5 rounded-r-[1.25rem] rounded-l-md border border-l-4 border-border/70 border-l-primary bg-background/50 p-4",
    },
    {
        Icon: CommandIcon,
        contextClass:
            "mt-4 flex flex-wrap items-center gap-2 rounded-2xl border border-primary/25 bg-popover px-3 py-2 text-xs text-muted-foreground shadow-[0_14px_36px_-30px_rgba(28,40,58,0.72)]",
        controlLabelClass:
            "flex items-center justify-between gap-3 rounded-xl px-2 py-1 text-sm hover:bg-muted/45",
        controlMenuClass:
            "absolute left-0 z-20 mt-2 min-w-60 rounded-2xl border border-primary/25 bg-[var(--popover)] p-2 shadow-[0_28px_90px_-54px_rgba(28,40,58,0.72)]",
        controlMenuHeadingClass:
            "mb-1 rounded-xl bg-muted/50 px-3 py-2 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground",
        controlStyle: "breadcrumb-ribbon",
        controlTriggerClass:
            "inline-flex cursor-pointer list-none items-center gap-2 rounded-xl border border-primary/25 bg-[var(--popover)] px-3 py-2 text-foreground shadow-[0_10px_30px_-24px_rgba(32,48,76,0.9)] marker:hidden hover:bg-muted/50",
        description: "Breadcrumb ribbon with popover controls",
        directoryButtonClass:
            "grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-xl px-3 py-3 text-left transition hover:bg-muted/45",
        directoryChevronClass:
            "inline-flex size-6 items-center justify-center rounded-xl border border-primary/25 bg-popover text-primary",
        directoryContentClass: "space-y-3 px-3 pb-3 pt-1",
        directoryGroupClass: "space-y-2",
        directoryMetaClass:
            "mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground",
        directoryRowBaseClass:
            "grid w-full grid-cols-1 gap-3 rounded-2xl border transition",
        directoryRowCollapsedClass: "p-0",
        directoryRowIdleClass:
            "border-border/60 bg-popover/70 hover:border-primary/35 hover:bg-popover",
        directoryRowSelectedClass:
            "border-primary/45 bg-primary/10 shadow-[0_18px_55px_-48px_rgba(28,40,58,0.8)]",
        directoryRowWithContentClass: "p-2",
        directoryTagClass:
            "text-xs uppercase tracking-[0.18em] text-muted-foreground",
        emptyStateClass:
            "mt-5 rounded-2xl border border-dashed border-primary/30 bg-popover px-4 py-8 text-center text-sm text-muted-foreground",
        fileButtonBaseClass:
            "flex w-full items-start gap-3 rounded-xl border border-border/60 bg-popover/75 px-3 py-3 text-left transition hover:border-primary/40 hover:bg-popover",
        fileButtonCompactClass: "h-full min-w-0",
        fileButtonSelectedClass: "border-primary/45 bg-primary/10",
        fileGlyphClass:
            "mt-1 inline-flex size-8 shrink-0 items-center justify-center rounded-xl border border-primary/25 bg-background text-xs font-semibold uppercase tracking-[0.16em] text-primary",
        fileListGridClass: "space-y-3 xl:col-span-2",
        fileListSingleClass: "space-y-3",
        fileListSinglePreviewClass:
            "grid gap-3 grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] items-start",
        fileMetaClass:
            "mt-2 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground",
        fileNameClass: "block truncate text-base font-medium text-foreground",
        folderControlsClass:
            "file-browser-control-surface ribbon-controls flex w-full flex-wrap items-center justify-start gap-2 rounded-2xl border border-primary/25 bg-popover px-3 py-2 text-sm shadow-[0_14px_45px_-38px_rgba(28,40,58,0.7)]",
        gridFileCellClass: "min-w-0 border-r border-border/60 pr-3",
        gridPreviewCellClass: "min-w-0",
        gridRowClass:
            "grid gap-3 grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] items-start",
        headerClass:
            "flex flex-wrap items-center gap-3 border-b border-primary/20 pb-5",
        headerIconClass: "size-4 text-primary",
        headerTitleClass:
            "text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground",
        id: "ribbon",
        label: "Breadcrumb Ribbon",
        pageBadgeClass:
            "inline-flex items-center gap-2 rounded-xl border border-primary/25 bg-background px-2 py-1.5 text-muted-foreground",
        paginationClass:
            "inline-flex items-center gap-1 rounded-xl border border-primary/25 bg-background p-1",
        previewHeightControlClass:
            "inline-flex items-center gap-2 rounded-xl border border-primary/25 bg-background px-3 py-2 text-foreground",
        sectionClass:
            "rounded-[1.75rem] border border-primary/20 bg-card p-4 shadow-[0_28px_90px_-72px_rgba(48,67,98,0.9)] sm:p-5",
        selectorClass:
            "ml-auto inline-flex flex-wrap items-center gap-1 rounded-2xl border border-primary/25 bg-popover p-1",
        selectorOptionClass: (active) =>
            cn(
                selectorOptionBaseClass,
                "rounded-xl",
                active
                    ? "bg-primary text-primary-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground",
            ),
        shortLabel: "Ribbon",
        singlePreviewClass:
            "sticky top-4 z-10 min-w-0 col-start-2 row-start-1 self-start",
        subdirCardBaseClass: "inline-flex max-w-full shrink-0 flex-col gap-2",
        subdirFilenameClass:
            "truncate text-xs font-medium text-muted-foreground",
        subdirFrameBaseClass: "inline-flex max-w-full overflow-hidden",
        subdirImageCardClass: "w-full",
        subdirImageFrameClass: "w-full items-start justify-center",
        subdirStripClass:
            "flex w-full min-w-0 items-start gap-4 overflow-x-auto pb-1",
        subdirStripWrapperClass: "px-3 pb-3",
        subdirTextCardClass: "w-fit",
        subdirTextFrameClass:
            "w-fit items-stretch [&_button]:max-w-none [&_button]:justify-start [&_button]:w-auto [&_img]:max-w-none [&_img]:w-auto",
        treeInnerClass: "space-y-3",
        treeShellClass:
            "mt-5 rounded-2xl border border-primary/20 bg-background/50 p-4",
    },
    {
        Icon: Table2,
        contextClass:
            "mt-4 flex flex-wrap items-center gap-2 border-y border-border/70 bg-muted/35 px-2 py-2 text-xs text-muted-foreground",
        controlLabelClass:
            "flex items-center justify-between gap-3 px-1 py-1 text-sm",
        controlMenuClass:
            "absolute left-0 z-20 mt-2 min-w-56 rounded-md border border-border bg-[var(--popover)] p-2 shadow-md",
        controlMenuHeadingClass:
            "mb-1 border-b border-border/70 px-1 pb-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-muted-foreground",
        controlStyle: "matrix-table",
        controlTriggerClass:
            "inline-flex cursor-pointer list-none items-center gap-2 rounded-md border border-border bg-[var(--popover)] px-2.5 py-1.5 text-foreground marker:hidden hover:bg-muted/60",
        description: "A table-like file matrix with header utilities",
        directoryButtonClass:
            "grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 px-2 py-2 text-left transition hover:bg-muted/40",
        directoryChevronClass:
            "inline-flex size-6 items-center justify-center rounded-sm border border-border/70 bg-background text-muted-foreground",
        directoryContentClass: "space-y-2 px-2 pb-2 pt-0",
        directoryGroupClass: "space-y-0",
        directoryMetaClass:
            "mt-0.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground",
        directoryRowBaseClass:
            "grid w-full grid-cols-1 gap-2 rounded-none border-x-0 border-t-0 border-b border-border/70 transition",
        directoryRowCollapsedClass: "p-0",
        directoryRowIdleClass: "bg-transparent hover:bg-muted/30",
        directoryRowSelectedClass: "bg-primary/10",
        directoryRowWithContentClass: "p-0",
        directoryTagClass:
            "text-[11px] uppercase tracking-[0.16em] text-muted-foreground",
        emptyStateClass:
            "mt-4 border border-dashed border-border/70 bg-background/40 px-4 py-8 text-center text-sm text-muted-foreground",
        fileButtonBaseClass:
            "flex w-full items-start gap-3 rounded-none border-x-0 border-t-0 border-b border-border/60 bg-transparent px-2 py-2 text-left transition hover:bg-muted/30",
        fileButtonCompactClass: "h-full min-w-0",
        fileButtonSelectedClass: "bg-primary/10",
        fileGlyphClass:
            "mt-0.5 inline-flex size-7 shrink-0 items-center justify-center rounded-sm bg-muted text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground",
        fileListGridClass: "space-y-0 xl:col-span-2",
        fileListSingleClass: "space-y-0",
        fileListSinglePreviewClass:
            "grid gap-2 grid-cols-[minmax(18rem,0.82fr)_minmax(0,1.18fr)] items-start",
        fileMetaClass:
            "mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground",
        fileNameClass: "block truncate text-sm font-medium text-foreground",
        folderControlsClass:
            "file-browser-control-surface matrix-controls flex w-full flex-wrap items-center justify-start gap-1.5 border-y border-border/70 bg-muted/35 px-2 py-1.5 text-sm",
        gridFileCellClass: "min-w-0 border-r border-border/60 pr-2",
        gridPreviewCellClass: "min-w-0",
        gridRowClass:
            "grid gap-2 grid-cols-[minmax(18rem,0.82fr)_minmax(0,1.18fr)] items-start",
        headerClass:
            "flex flex-wrap items-center gap-3 border-b border-border/70 pb-3",
        headerIconClass: "size-4 text-primary",
        headerTitleClass:
            "text-sm font-semibold uppercase tracking-[0.2em] text-muted-foreground",
        id: "matrix",
        label: "File Matrix",
        pageBadgeClass:
            "inline-flex items-center gap-2 rounded-md border border-border/70 bg-background px-2 py-1.5 text-muted-foreground",
        paginationClass:
            "inline-flex items-center gap-1 rounded-md border border-border/70 bg-background p-1",
        previewHeightControlClass:
            "inline-flex items-center gap-2 rounded-md border border-border/70 bg-background px-2.5 py-1.5 text-foreground",
        sectionClass:
            "border border-border/70 bg-card p-3 shadow-[0_12px_46px_-42px_rgba(48,67,98,0.9)] sm:p-4",
        selectorClass:
            "ml-auto inline-flex flex-wrap items-center gap-0.5 border border-border/70 bg-background p-0.5",
        selectorOptionClass: (active) =>
            cn(
                selectorOptionBaseClass,
                "rounded-sm",
                active
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground",
            ),
        shortLabel: "Matrix",
        singlePreviewClass:
            "sticky top-4 z-10 min-w-0 col-start-2 row-start-1 self-start",
        subdirCardBaseClass: "inline-flex max-w-full shrink-0 flex-col gap-1.5",
        subdirFilenameClass:
            "truncate text-[11px] font-medium text-muted-foreground",
        subdirFrameBaseClass: "inline-flex max-w-full overflow-hidden",
        subdirImageCardClass: "w-full",
        subdirImageFrameClass: "w-full items-start justify-center",
        subdirStripClass:
            "flex w-full min-w-0 items-start gap-3 overflow-x-auto pb-1",
        subdirStripWrapperClass: "px-2 pb-2",
        subdirTextCardClass: "w-fit",
        subdirTextFrameClass:
            "w-fit items-stretch [&_button]:max-w-none [&_button]:justify-start [&_button]:w-auto [&_img]:max-w-none [&_img]:w-auto",
        treeInnerClass: "space-y-0",
        treeShellClass: "mt-4 border border-border/70 bg-background/45 p-2",
    },
    {
        Icon: GalleryHorizontal,
        contextClass:
            "mt-4 flex flex-wrap items-center gap-2 rounded-xl border border-dashed border-accent/80 bg-accent/15 px-3 py-2 text-xs text-muted-foreground",
        controlLabelClass:
            "flex items-center justify-between gap-3 rounded-lg px-1 py-0.5 text-sm",
        controlMenuClass:
            "absolute left-0 z-20 mt-2 min-w-56 rounded-xl border border-dashed border-accent/80 bg-[var(--popover)] p-3 shadow-lg",
        controlMenuHeadingClass:
            "mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground",
        controlStyle: "preview-deck",
        controlTriggerClass:
            "inline-flex cursor-pointer list-none items-center gap-2 rounded-lg border border-dashed border-accent/80 bg-[var(--popover)] px-3 py-2 text-foreground marker:hidden hover:bg-accent/20",
        description: "Preview-first deck with a compact file strip",
        directoryButtonClass:
            "grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-lg px-3 py-3 text-left transition hover:bg-accent/10",
        directoryChevronClass:
            "inline-flex size-6 items-center justify-center rounded-lg border border-accent/80 bg-accent/15 text-foreground",
        directoryContentClass: "space-y-3 px-3 pb-3 pt-1",
        directoryGroupClass: "space-y-2",
        directoryMetaClass:
            "mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground",
        directoryRowBaseClass:
            "grid w-full grid-cols-1 gap-3 rounded-xl border transition",
        directoryRowCollapsedClass: "p-0",
        directoryRowIdleClass:
            "border-border/60 bg-background/55 hover:border-accent hover:bg-accent/10",
        directoryRowSelectedClass:
            "border-accent bg-accent/20 shadow-[0_18px_60px_-52px_rgba(115,83,25,0.8)]",
        directoryRowWithContentClass: "p-2",
        directoryTagClass:
            "text-xs uppercase tracking-[0.18em] text-muted-foreground",
        emptyStateClass:
            "mt-5 rounded-xl border border-dashed border-accent/80 bg-accent/10 px-4 py-8 text-center text-sm text-muted-foreground",
        fileButtonBaseClass:
            "flex w-full items-start gap-3 rounded-lg border border-border/60 bg-background/70 px-3 py-3 text-left transition hover:border-accent hover:bg-accent/10",
        fileButtonCompactClass: "h-full min-w-0",
        fileButtonSelectedClass: "border-accent bg-accent/20",
        fileGlyphClass:
            "mt-1 inline-flex size-8 shrink-0 items-center justify-center rounded-lg border border-accent/80 bg-accent/15 text-xs font-semibold uppercase tracking-[0.16em] text-foreground",
        fileListGridClass: "space-y-3 xl:col-span-2",
        fileListSingleClass: "space-y-3",
        fileListSinglePreviewClass:
            "grid gap-3 grid-cols-[minmax(0,1.22fr)_minmax(16rem,0.78fr)] items-start",
        fileMetaClass:
            "mt-2 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground",
        fileNameClass: "block truncate text-base font-medium text-foreground",
        folderControlsClass:
            "file-browser-control-surface deck-controls flex w-full flex-wrap items-center justify-start gap-2 rounded-xl border border-dashed border-accent/80 bg-accent/15 px-3 py-2 text-sm",
        gridFileCellClass: "min-w-0 border-r border-border/60 pr-3",
        gridPreviewCellClass: "min-w-0",
        gridRowClass:
            "grid gap-3 grid-cols-[minmax(18rem,0.78fr)_minmax(0,1.22fr)] items-start",
        headerClass:
            "flex flex-wrap items-center gap-3 border-b border-accent/60 pb-5",
        headerIconClass: "size-4 text-primary",
        headerTitleClass:
            "text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground",
        id: "deck",
        label: "Preview Deck",
        pageBadgeClass:
            "inline-flex items-center gap-2 rounded-lg border border-dashed border-accent/80 bg-background px-2 py-1.5 text-muted-foreground",
        paginationClass:
            "inline-flex items-center gap-1 rounded-lg border border-dashed border-accent/80 bg-background p-1",
        previewHeightControlClass:
            "inline-flex items-center gap-2 rounded-lg border border-dashed border-accent/80 bg-background px-3 py-2 text-foreground",
        sectionClass:
            "rounded-[1.5rem] border border-accent/70 bg-card/90 p-4 shadow-[0_28px_90px_-72px_rgba(115,83,25,0.8)] sm:p-5",
        selectorClass:
            "ml-auto inline-flex flex-wrap items-center gap-1 rounded-xl border border-dashed border-accent/80 bg-background/70 p-1",
        selectorOptionClass: (active) =>
            cn(
                selectorOptionBaseClass,
                "rounded-lg",
                active
                    ? "bg-accent text-accent-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-accent/20 hover:text-foreground",
            ),
        shortLabel: "Deck",
        singlePreviewClass:
            "sticky top-4 z-10 min-w-0 col-start-1 row-start-1 self-start",
        subdirCardBaseClass: "inline-flex max-w-full shrink-0 flex-col gap-2",
        subdirFilenameClass:
            "truncate text-xs font-semibold text-muted-foreground",
        subdirFrameBaseClass: "inline-flex max-w-full overflow-hidden",
        subdirImageCardClass: "w-full",
        subdirImageFrameClass: "w-full items-start justify-center",
        subdirStripClass:
            "flex w-full min-w-0 items-start gap-4 overflow-x-auto pb-1",
        subdirStripWrapperClass:
            "rounded-xl border border-dashed border-accent/60 bg-background/50 px-3 py-3",
        subdirTextCardClass: "w-fit",
        subdirTextFrameClass:
            "w-fit items-stretch [&_button]:max-w-none [&_button]:justify-start [&_button]:w-auto [&_img]:max-w-none [&_img]:w-auto",
        treeInnerClass: "space-y-3",
        treeShellClass:
            "mt-5 rounded-xl border border-accent/60 bg-background/50 p-4",
    },
];

const fileBrowserDesignById = new Map(
    fileBrowserDesigns.map((design) => [design.id, design]),
);
const defaultFileBrowserDesign = fileBrowserDesigns[0] as FileBrowserDesign;

function controlPlacementFor(
    designId: FileBrowserDesignKey,
): FileBrowserControlPlacement {
    switch (designId) {
        case "inline":
            return "name-area";
        case "sidecar":
            return "sidecar";
        case "ribbon":
            return "content-ribbon";
        case "matrix":
            return "matrix-header";
        case "deck":
            return "preview-dock";
        case "classic":
            return "classic-row";
    }
}

const previewHeightCommitKeys = new Set([
    "ArrowDown",
    "ArrowLeft",
    "ArrowRight",
    "ArrowUp",
    "End",
    "Home",
    "PageDown",
    "PageUp",
]);

const PreviewHeightControl = memo(function PreviewHeightControl({
    ariaLabel = "Preview height",
    className,
    label = "Preview height",
    onCommit,
    value,
}: {
    ariaLabel?: string;
    className?: string;
    label?: string;
    onCommit?: (value: number) => void;
    value: number;
}) {
    const [draftValue, setDraftValue] = useState(value);
    const committedValueRef = useRef(value);

    const commitDraftValue = () => {
        if (draftValue === committedValueRef.current) {
            return;
        }

        committedValueRef.current = draftValue;
        onCommit?.(draftValue);
    };

    return (
        <label
            className={cn(
                "inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-foreground",
                className,
            )}
        >
            <span className="inline-flex items-center gap-2 text-sm font-medium">
                <Eye className="size-4 text-primary" aria-hidden="true" />
                <span className="whitespace-nowrap">{label}</span>
            </span>
            <input
                aria-label={ariaLabel}
                className="h-1 w-24 accent-primary"
                max={420}
                min={120}
                onBlur={commitDraftValue}
                onChange={() => undefined}
                onInput={(event) => {
                    setDraftValue(Number(event.currentTarget.value));
                }}
                onKeyUp={(event) => {
                    if (previewHeightCommitKeys.has(event.key)) {
                        commitDraftValue();
                    }
                }}
                onMouseUp={commitDraftValue}
                onTouchEnd={commitDraftValue}
                step={20}
                type="range"
                value={draftValue}
            />
            <span className="text-xs text-muted-foreground tabular-nums">
                {draftValue}px
            </span>
        </label>
    );
});

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

function visibleDirectoryLabel(
    path: string,
    label: string,
    depth: number,
): string {
    if (depth === 0) {
        return path;
    }

    return label || directoryLabel(path);
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
    const descendantDirectoryCount = children.reduce(
        (total, child) => total + 1 + child.descendantDirectoryCount,
        0,
    );
    const descendantFileCount = children.reduce(
        (total, child) => total + child.descendantFileCount,
        current.group?.fileCount ?? 0,
    );
    const weight = Math.min(
        current.group
            ? fileKindOrder[current.group.files[0]?.kind ?? "pipeline"]
            : Number.POSITIVE_INFINITY,
        ...children.map((child) => child.weight),
    );

    return {
        children,
        descendantDirectoryCount,
        descendantFileCount,
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
    const [selectedDesignId, setSelectedDesignId] =
        useState<FileBrowserDesignKey>("classic");
    const activeDesign =
        fileBrowserDesignById.get(selectedDesignId) ?? defaultFileBrowserDesign;
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
    const initialSubdirPreviewDirectory = useMemo(
        () => findInitialSubdirPreviewDirectory(files),
        [files],
    );
    const preferredDirectory =
        selectedDirectory ??
        uncontrolledDirectory ??
        (renderGridPreview ? initialSubdirPreviewDirectory : undefined) ??
        directoryGroups[0]?.path;
    const activeDirectory = directoryGroups.find(
        (group) => group.path === preferredDirectory,
    );
    const activeFiles = activeDirectory?.files ?? [];
    const effectiveSelectedDirectory = preferredDirectory;
    const [uncontrolledPreviewHeight, setUncontrolledPreviewHeight] =
        useState(previewHeight);
    const [subdirPreviewEnabledByPath, setSubdirPreviewEnabledByPath] =
        useState<Record<string, boolean>>({});
    const [subdirPreviewKindsByPath, setSubdirPreviewKindsByPath] = useState<
        Record<string, Set<SubdirPreviewKind>>
    >({});
    const [subdirPreviewPages, setSubdirPreviewPages] = useState<
        Record<string, number>
    >({});
    const [
        openSubdirPreviewKindDisclosurePath,
        setOpenSubdirPreviewKindDisclosurePath,
    ] = useState<string | null>(null);
    const [openPreviewModeDisclosurePath, setOpenPreviewModeDisclosurePath] =
        useState<string | null>(null);
    const openSubdirPreviewKindDisclosureRef =
        useRef<HTMLDetailsElement | null>(null);
    const openPreviewModeDisclosureRef = useRef<HTMLDetailsElement | null>(
        null,
    );
    const effectivePreviewHeight = onPreviewHeightChange
        ? previewHeight
        : uncontrolledPreviewHeight;
    const subdirPreviewEnabledFor = (directoryPath: string): boolean =>
        subdirPreviewEnabledByPath[directoryPath] ?? false;
    const subdirPreviewKindsFor = (
        directoryPath: string,
    ): Set<SubdirPreviewKind> =>
        subdirPreviewKindsByPath[directoryPath] ??
        new Set(defaultSubdirPreviewKinds);
    const selectedPreviewKinds = effectiveSelectedDirectory
        ? subdirPreviewKindsFor(effectiveSelectedDirectory)
        : defaultSubdirPreviewKinds;
    const previewableActiveFiles = activeFiles.filter((file) =>
        fileMatchesPreviewKinds(file, selectedPreviewKinds),
    );
    const preferredSelectedPath = selectedPath ?? uncontrolledPath;
    const activeFile =
        previewableActiveFiles.find(
            (file) => file.path === preferredSelectedPath,
        ) ??
        previewableActiveFiles[0] ??
        activeFiles.find((file) => file.path === preferredSelectedPath) ??
        activeFiles[0];
    const effectiveSelectedPath = activeFile?.path;
    const displayedFiles = visibleFiles ?? activeFiles;
    const previewableDisplayedFiles = displayedFiles.filter((file) =>
        fileMatchesPreviewKinds(file, selectedPreviewKinds),
    );
    const hasPreviewableActiveFiles = previewableActiveFiles.length > 0;

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
    useEffect(() => {
        if (!openSubdirPreviewKindDisclosurePath) {
            return;
        }

        const handleDocumentClick = (event: MouseEvent) => {
            const disclosure = openSubdirPreviewKindDisclosureRef.current;

            if (!disclosure) {
                setOpenSubdirPreviewKindDisclosurePath(null);
                return;
            }

            const target = event.target;

            if (target instanceof Node && disclosure.contains(target)) {
                return;
            }

            disclosure.open = false;
            openSubdirPreviewKindDisclosureRef.current = null;
            setOpenSubdirPreviewKindDisclosurePath(null);

            const targetElement =
                target instanceof Element
                    ? target
                    : target instanceof Node
                      ? target.parentElement
                      : null;

            if (targetElement?.closest("button[data-directory-path]")) {
                event.preventDefault();
                event.stopPropagation();
            }
        };

        document.addEventListener("click", handleDocumentClick, true);

        return () => {
            document.removeEventListener("click", handleDocumentClick, true);
        };
    }, [openSubdirPreviewKindDisclosurePath]);
    useEffect(() => {
        if (!openPreviewModeDisclosurePath) {
            return;
        }

        const handleDocumentClick = (event: MouseEvent) => {
            const disclosure = openPreviewModeDisclosureRef.current;

            if (!disclosure) {
                setOpenPreviewModeDisclosurePath(null);
                return;
            }

            const target = event.target;

            if (target instanceof Node && disclosure.contains(target)) {
                return;
            }

            disclosure.open = false;
            openPreviewModeDisclosureRef.current = null;
            setOpenPreviewModeDisclosurePath(null);

            const targetElement =
                target instanceof Element
                    ? target
                    : target instanceof Node
                      ? target.parentElement
                      : null;

            if (targetElement?.closest("button[data-directory-path]")) {
                event.preventDefault();
                event.stopPropagation();
            }
        };

        document.addEventListener("click", handleDocumentClick, true);

        return () => {
            document.removeEventListener("click", handleDocumentClick, true);
        };
    }, [openPreviewModeDisclosurePath]);

    const handlePreviewHeightCommit = (value: number) => {
        if (onPreviewHeightChange) {
            onPreviewHeightChange(value);
            return;
        }

        setUncontrolledPreviewHeight(value);
    };
    const renderDesignSelector = () => (
        <div
            aria-label="Temporary file browser design selector"
            className={activeDesign.selectorClass}
            data-file-browser-design-selector="true"
            role="group"
        >
            {fileBrowserDesigns.map((design) => {
                const DesignIcon = design.Icon;
                const isActive = design.id === activeDesign.id;

                return (
                    <button
                        key={design.id}
                        type="button"
                        aria-pressed={isActive}
                        className={design.selectorOptionClass(isActive)}
                        data-file-browser-design-option={design.id}
                        onClick={() => setSelectedDesignId(design.id)}
                        title={`${design.label}: ${design.description}`}
                    >
                        <DesignIcon className="size-3.5" aria-hidden="true" />
                        <span className="hidden lg:inline">
                            {design.shortLabel}
                        </span>
                    </button>
                );
            })}
        </div>
    );
    const renderDesignContext = () => {
        if (activeDesign.id === "classic") {
            return null;
        }

        const activeNode = effectiveSelectedDirectory
            ? findTreeNodeByPath(directoryTree, effectiveSelectedDirectory)
            : undefined;
        const segments = pathSegments(effectiveSelectedDirectory ?? "/");

        return (
            <div
                className={activeDesign.contextClass}
                data-file-browser-design-context={activeDesign.id}
            >
                <span className="font-semibold text-foreground">
                    {activeDesign.label}
                </span>
                <span className="text-border">/</span>
                <span className="inline-flex min-w-0 flex-wrap items-center gap-1">
                    {segments.length === 0 ? (
                        <span className="rounded-sm bg-muted px-1.5 py-0.5 text-foreground">
                            root
                        </span>
                    ) : (
                        segments.map((segment) => (
                            <span
                                key={segment}
                                className="rounded-sm bg-muted px-1.5 py-0.5 text-foreground"
                            >
                                {segment}
                            </span>
                        ))
                    )}
                </span>
                <span className="text-border">/</span>
                <span>
                    {activeNode?.descendantFileCount ?? activeFiles.length}{" "}
                    files
                </span>
                <span>{activeNode?.descendantDirectoryCount ?? 0} folders</span>
            </div>
        );
    };
    const renderFileButton = (
        file: FileEntry,
        compact = false,
        style?: CSSProperties,
    ) => {
        const isMatrix = activeDesign.id === "matrix";
        const isDeck = activeDesign.id === "deck";

        return (
            <button
                type="button"
                key={file.path}
                className={cn(
                    activeDesign.fileButtonBaseClass,
                    isMatrix &&
                        "grid grid-cols-[auto_minmax(10rem,1fr)_5rem_minmax(9rem,0.75fr)_5rem] items-center gap-3",
                    isDeck &&
                        "grid grid-cols-[auto_minmax(0,1fr)] rounded-none border-x-0 border-t-0 bg-transparent",
                    effectiveSelectedPath === file.path &&
                        activeDesign.fileButtonSelectedClass,
                    compact && activeDesign.fileButtonCompactClass,
                )}
                data-file-browser-file-layout={
                    isMatrix ? "matrix-row" : isDeck ? "deck-strip" : "card"
                }
                data-file-path={file.path}
                onClick={() => {
                    if (selectedPath === undefined) {
                        setUncontrolledPath(file.path);
                    }

                    onSelectFile(file);
                }}
                style={style}
            >
                <span
                    aria-hidden="true"
                    className={activeDesign.fileGlyphClass}
                >
                    {file.kind.slice(0, 1)}
                </span>
                {isMatrix ? (
                    <>
                        <span
                            className={cn(
                                activeDesign.fileNameClass,
                                "min-w-0",
                            )}
                        >
                            {fileName(file.path)}
                        </span>
                        <span className="text-xs text-muted-foreground tabular-nums">
                            {formatBytes(file.size)}
                        </span>
                        <span className="truncate text-xs text-muted-foreground">
                            {formatMtime(file.mtime)}
                        </span>
                        <span className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                            {file.kind}
                        </span>
                    </>
                ) : (
                    <span className="min-w-0 flex-1">
                        <span className={activeDesign.fileNameClass}>
                            {fileName(file.path)}
                        </span>
                        <span className={activeDesign.fileMetaClass}>
                            <span>{formatBytes(file.size)}</span>
                            <span>{formatMtime(file.mtime)}</span>
                            <span className="uppercase tracking-[0.18em]">
                                {file.kind}
                            </span>
                        </span>
                    </span>
                )}
            </button>
        );
    };

    const renderPreviewControls = (directoryPath: string) => {
        const showPreviewPaging = previewPageCount > 1;

        if (!showPreviewPaging) {
            return null;
        }

        return (
            <div
                className="col-span-full flex flex-wrap items-center justify-end gap-2 pt-1 text-sm"
                data-file-browser-bottom-controls={directoryPath}
            >
                <div className={activeDesign.pageBadgeClass}>
                    <ListFilter
                        className="size-4 text-primary"
                        aria-hidden="true"
                    />
                    <span>
                        Page {previewPage} of {previewPageCount}
                    </span>
                </div>

                <PreviewPagination
                    nextLabel="Next preview page"
                    onPageChange={(page) => onPreviewPageChange?.(page)}
                    page={previewPage}
                    pageCount={previewPageCount}
                    previousLabel="Previous preview page"
                    selectLabel="Preview page"
                    className={activeDesign.paginationClass}
                />
            </div>
        );
    };

    const renderFolderControls = (
        directoryPath: string,
        options: {
            hasFilePreviewControls: boolean;
            hasSubdirPreviewControls: boolean;
            showGridToggle: boolean;
            subdirPageCount: number;
            safeSubdirPreviewPage: number;
        },
    ) => {
        const {
            hasFilePreviewControls,
            hasSubdirPreviewControls,
            safeSubdirPreviewPage,
            showGridToggle,
            subdirPageCount,
        } = options;
        const showPreviewPaging = previewPageCount > 1;
        const subdirPreviewEnabled = subdirPreviewEnabledFor(directoryPath);
        const subdirPreviewKinds = subdirPreviewKindsFor(directoryPath);
        const controlPlacement = controlPlacementFor(activeDesign.id);
        const hasPreviewModeControls =
            hasSubdirPreviewControls || showGridToggle;

        if (!hasFilePreviewControls && !hasSubdirPreviewControls) {
            return null;
        }

        return (
            <div
                className={activeDesign.folderControlsClass}
                data-file-browser-folder-controls={directoryPath}
                data-file-browser-control-placement={controlPlacement}
                data-file-browser-control-style={activeDesign.controlStyle}
                data-file-browser-control-surface="true"
                data-subdir-preview-controls={
                    hasSubdirPreviewControls ? directoryPath : undefined
                }
            >
                {hasPreviewModeControls ? (
                    <details
                        className="relative"
                        data-preview-mode-disclosure={directoryPath}
                        open={openPreviewModeDisclosurePath === directoryPath}
                        ref={(element) => {
                            if (
                                openPreviewModeDisclosurePath === directoryPath
                            ) {
                                openPreviewModeDisclosureRef.current = element;
                                return;
                            }

                            if (
                                openPreviewModeDisclosureRef.current === element
                            ) {
                                openPreviewModeDisclosureRef.current = null;
                            }
                        }}
                    >
                        <summary
                            aria-label="Preview modes"
                            className={activeDesign.controlTriggerClass}
                            data-file-browser-control-trigger="preview-modes"
                            onClick={(event) => {
                                event.preventDefault();
                                setOpenPreviewModeDisclosurePath((current) =>
                                    current === directoryPath
                                        ? null
                                        : directoryPath,
                                );
                            }}
                        >
                            <Eye
                                className="size-4 text-primary"
                                aria-hidden="true"
                            />
                            <span className="font-medium">Preview modes</span>
                            <span className="text-xs text-muted-foreground">
                                {summarizePreviewModes(
                                    previewMode,
                                    subdirPreviewEnabled,
                                    showGridToggle,
                                )}
                            </span>
                            <ChevronDown className="size-4 text-muted-foreground" />
                        </summary>
                        <div
                            className={activeDesign.controlMenuClass}
                            data-preview-modes-menu={directoryPath}
                        >
                            <div
                                className={activeDesign.controlMenuHeadingClass}
                            >
                                Preview modes
                            </div>
                            <div className="space-y-2">
                                {showGridToggle ? (
                                    <label
                                        className={
                                            activeDesign.controlLabelClass
                                        }
                                    >
                                        <span>1 preview per row</span>
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
                                    </label>
                                ) : null}
                                {hasSubdirPreviewControls ? (
                                    <label
                                        className={
                                            activeDesign.controlLabelClass
                                        }
                                    >
                                        <span>Subfolder previews</span>
                                        <input
                                            aria-label="Subfolder previews"
                                            checked={subdirPreviewEnabled}
                                            className="size-4 accent-primary"
                                            onChange={(event) => {
                                                setSubdirPreviewEnabledByPath(
                                                    (current) => ({
                                                        ...current,
                                                        [directoryPath]:
                                                            event.target
                                                                .checked,
                                                    }),
                                                );
                                                setSubdirPreviewPages(
                                                    (current) => ({
                                                        ...current,
                                                        [directoryPath]: 1,
                                                    }),
                                                );
                                            }}
                                            type="checkbox"
                                        />
                                    </label>
                                ) : null}
                            </div>
                        </div>
                    </details>
                ) : null}

                {hasFilePreviewControls || hasSubdirPreviewControls ? (
                    <>
                        <details
                            className="relative"
                            data-subdir-preview-kind-disclosure={directoryPath}
                            open={
                                openSubdirPreviewKindDisclosurePath ===
                                directoryPath
                            }
                            ref={(element) => {
                                if (
                                    openSubdirPreviewKindDisclosurePath ===
                                    directoryPath
                                ) {
                                    openSubdirPreviewKindDisclosureRef.current =
                                        element;
                                    return;
                                }

                                if (
                                    openSubdirPreviewKindDisclosureRef.current ===
                                    element
                                ) {
                                    openSubdirPreviewKindDisclosureRef.current =
                                        null;
                                }
                            }}
                        >
                            <summary
                                aria-label="File types"
                                className={activeDesign.controlTriggerClass}
                                data-file-browser-control-trigger="file-types"
                                onClick={(event) => {
                                    event.preventDefault();
                                    setOpenSubdirPreviewKindDisclosurePath(
                                        (current) =>
                                            current === directoryPath
                                                ? null
                                                : directoryPath,
                                    );
                                }}
                            >
                                <ListFilter
                                    className="size-4 text-primary"
                                    aria-hidden="true"
                                />
                                <span className="font-medium">File types</span>
                                <span className="text-xs text-muted-foreground">
                                    {summarizeSubdirPreviewKinds(
                                        subdirPreviewKinds,
                                    )}
                                </span>
                                <ChevronDown className="size-4 text-muted-foreground" />
                            </summary>
                            <div
                                className={cn(
                                    activeDesign.controlMenuClass,
                                    "right-0 left-auto min-w-52",
                                )}
                                data-subdir-preview-kinds={directoryPath}
                            >
                                <div
                                    className={
                                        activeDesign.controlMenuHeadingClass
                                    }
                                >
                                    File types
                                </div>
                                <div className="space-y-2">
                                    {subdirPreviewKindGroups.map((group) => (
                                        <label
                                            key={group.id}
                                            className={
                                                activeDesign.controlLabelClass
                                            }
                                        >
                                            <span>{group.label}</span>
                                            <input
                                                checked={subdirPreviewKinds.has(
                                                    group.id,
                                                )}
                                                className="size-3.5 accent-primary"
                                                data-subdir-preview-kind={
                                                    group.id
                                                }
                                                onChange={(event) => {
                                                    setSubdirPreviewKindsByPath(
                                                        (current) => {
                                                            const next = {
                                                                ...current,
                                                            };
                                                            const nextKinds =
                                                                new Set(
                                                                    subdirPreviewKinds,
                                                                );

                                                            if (
                                                                event.target
                                                                    .checked
                                                            ) {
                                                                nextKinds.add(
                                                                    group.id,
                                                                );
                                                            } else {
                                                                nextKinds.delete(
                                                                    group.id,
                                                                );
                                                            }

                                                            next[
                                                                directoryPath
                                                            ] = nextKinds;

                                                            return next;
                                                        },
                                                    );
                                                    setSubdirPreviewPages(
                                                        (current) => ({
                                                            ...current,
                                                            [directoryPath]: 1,
                                                        }),
                                                    );
                                                }}
                                                type="checkbox"
                                            />
                                        </label>
                                    ))}
                                </div>
                            </div>
                        </details>
                    </>
                ) : null}

                <PreviewHeightControl
                    className={activeDesign.previewHeightControlClass}
                    onCommit={handlePreviewHeightCommit}
                    value={effectivePreviewHeight}
                />

                {showPreviewPaging && hasFilePreviewControls ? (
                    <>
                        <div className={activeDesign.pageBadgeClass}>
                            <ListFilter
                                className="size-4 text-primary"
                                aria-hidden="true"
                            />
                            <span>
                                Page {previewPage} of {previewPageCount}
                            </span>
                        </div>
                        <PreviewPagination
                            nextLabel="Next preview page"
                            onPageChange={(page) => onPreviewPageChange?.(page)}
                            page={previewPage}
                            pageCount={previewPageCount}
                            previousLabel="Previous preview page"
                            selectLabel="Preview page"
                            className={activeDesign.paginationClass}
                        />
                    </>
                ) : null}

                {hasSubdirPreviewControls && subdirPageCount > 1 ? (
                    <>
                        <div className={activeDesign.pageBadgeClass}>
                            <ListFilter
                                className="size-4 text-primary"
                                aria-hidden="true"
                            />
                            <span>
                                Page {safeSubdirPreviewPage} of{" "}
                                {subdirPageCount}
                            </span>
                        </div>
                        <PreviewPagination
                            nextLabel="Next subfolder page"
                            onPageChange={(page) => {
                                setSubdirPreviewPages((current) => ({
                                    ...current,
                                    [directoryPath]: page,
                                }));
                            }}
                            page={safeSubdirPreviewPage}
                            pageCount={subdirPageCount}
                            previousLabel="Previous subfolder page"
                            selectLabel="Subfolder preview page"
                            className={activeDesign.paginationClass}
                        />
                    </>
                ) : null}
            </div>
        );
    };

    const subdirPreviewStateFor = (node: DirectoryTreeNode) => {
        const subdirPreviewKinds = subdirPreviewKindsFor(node.path);
        const eligibleSubdirs = qualifyingSubdirsFor(
            node,
            allSubdirPreviewKinds,
        );
        const qualifyingSubdirs = qualifyingSubdirsFor(
            node,
            subdirPreviewKinds,
        );
        const available = eligibleSubdirs.length > 1;
        const pageCount = Math.max(
            1,
            Math.ceil(qualifyingSubdirs.length / SUBDIR_PREVIEW_PAGE_SIZE),
        );
        const requestedPage = subdirPreviewPages[node.path] ?? 1;
        const safePage = Math.min(requestedPage, pageCount);
        const visibleSubdirs = available
            ? qualifyingSubdirs.slice(
                  (safePage - 1) * SUBDIR_PREVIEW_PAGE_SIZE,
                  safePage * SUBDIR_PREVIEW_PAGE_SIZE,
              )
            : [];

        return {
            available,
            pageCount,
            safePage,
            visibleSubdirs,
        };
    };

    const renderSubdirPreviewStrip = (
        subdir: DirectoryTreeNode,
        kinds: ReadonlySet<SubdirPreviewKind>,
    ): ReactNode => {
        const previewableFiles = previewableFilesForKinds(subdir, kinds);

        return (
            <div
                className={activeDesign.subdirStripWrapperClass}
                data-subdir-preview-strip-wrapper={subdir.path}
            >
                <div
                    className={activeDesign.subdirStripClass}
                    data-subdir-preview-strip={subdir.path}
                    style={
                        {
                            "--subdir-preview-height": `${effectivePreviewHeight}px`,
                        } as CSSProperties
                    }
                >
                    {previewableFiles.map((file) =>
                        (() => {
                            const isImageSubdirPreview =
                                previewKindForPath(file.path) === "image";

                            return (
                                <div
                                    key={file.path}
                                    className={cn(
                                        activeDesign.subdirCardBaseClass,
                                        isImageSubdirPreview
                                            ? activeDesign.subdirImageCardClass
                                            : activeDesign.subdirTextCardClass,
                                    )}
                                    data-subdir-preview-card={file.path}
                                    style={{
                                        maxWidth: `calc(var(--subdir-preview-height) * 1.8)`,
                                    }}
                                >
                                    <p
                                        className={
                                            activeDesign.subdirFilenameClass
                                        }
                                        data-subdir-preview-filename={file.path}
                                        title={fileName(file.path)}
                                    >
                                        {fileName(file.path)}
                                    </p>
                                    <div
                                        className={cn(
                                            activeDesign.subdirFrameBaseClass,
                                            isImageSubdirPreview
                                                ? activeDesign.subdirImageFrameClass
                                                : activeDesign.subdirTextFrameClass,
                                        )}
                                        data-subdir-preview-frame={file.path}
                                        style={{
                                            height: `var(--subdir-preview-height)`,
                                        }}
                                    >
                                        {renderGridPreview?.(file) ?? null}
                                    </div>
                                </div>
                            );
                        })(),
                    )}
                </div>
            </div>
        );
    };

    function renderDirectoryRows(
        nodes: DirectoryTreeNode[],
        depth = 0,
        parentPath?: string,
    ): ReactNode[] {
        return nodes.flatMap((node) => {
            const isStructurallyExpanded = visibleExpandedDirectories.has(
                node.path,
            );
            const isSelected = node.path === effectiveSelectedDirectory;
            const hasChildren = node.children.length > 0;
            const hasFiles = node.descendantFileCount > 0;
            const {
                available: subdirPreviewAvailable,
                pageCount: subdirPreviewPageCount,
                safePage: safeSubdirPreviewPage,
                visibleSubdirs,
            } = subdirPreviewStateFor(node);
            const nodePreviewKinds = subdirPreviewKindsFor(node.path);
            const previewableNodeFiles = node.files.filter((file) =>
                fileMatchesPreviewKinds(file, nodePreviewKinds),
            );
            const hasFilePreviewControls =
                isStructurallyExpanded &&
                isSelected &&
                activeFiles.length > 0 &&
                hasPreviewableActiveFiles &&
                Boolean(renderGridPreview || renderSinglePreview);
            const showFilePreviewWidgets =
                node.path === effectiveSelectedDirectory &&
                hasPreviewableActiveFiles;
            const hasSubdirPreviewControls =
                isStructurallyExpanded &&
                subdirPreviewAvailable &&
                Boolean(renderGridPreview);
            const showGridToggle =
                previewableNodeFiles.length > 1 &&
                Boolean(renderGridPreview || renderSinglePreview) &&
                (isSelected || previewMode === "grid");
            const subdirPreviewKinds = nodePreviewKinds;
            const parentNode = parentPath
                ? findTreeNodeByPath(directoryTree, parentPath)
                : undefined;
            const inlineSubdirPreviewKinds = parentNode
                ? subdirPreviewKindsFor(parentNode.path)
                : null;
            const showInlineSubdirPreview = Boolean(
                parentNode &&
                parentNode.path === effectiveSelectedDirectory &&
                subdirPreviewEnabledFor(parentNode.path) &&
                !isStructurallyExpanded &&
                subdirPreviewStateFor(parentNode).visibleSubdirs.some(
                    (subdir) => subdir.path === node.path,
                ),
            );
            const folderControls = renderFolderControls(node.path, {
                hasFilePreviewControls,
                hasSubdirPreviewControls,
                showGridToggle,
                safeSubdirPreviewPage,
                subdirPageCount: subdirPreviewPageCount,
            });
            const hasPreviewControls = Boolean(folderControls);
            const showsDirectoryFiles =
                isStructurallyExpanded &&
                node.path === effectiveSelectedDirectory &&
                displayedFiles.length > 0;
            const showsChildRows = isStructurallyExpanded && hasChildren;
            const isExpanded =
                hasPreviewControls || showsDirectoryFiles || showsChildRows;
            const controlPlacement = controlPlacementFor(activeDesign.id);
            const folderControlsInNameArea =
                controlPlacement === "name-area" && Boolean(folderControls);
            const folderControlsInSidecar =
                controlPlacement === "sidecar" && Boolean(folderControls);
            const folderControlsInRibbon =
                (controlPlacement === "content-ribbon" ||
                    controlPlacement === "matrix-header" ||
                    controlPlacement === "preview-dock") &&
                Boolean(folderControls);
            const renderDirectoryButton = () => (
                <button
                    type="button"
                    className={activeDesign.directoryButtonClass}
                    data-depth={depth}
                    data-directory-expanded={String(isExpanded)}
                    data-directory-path={node.path}
                    data-subdir-preview-heading={
                        showInlineSubdirPreview ? node.path : undefined
                    }
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

                        onSelectDirectory?.(node.path, {
                            expanded: nextIsExpanded,
                            parentPath,
                        });
                    }}
                    style={{ paddingLeft: `${depth * 1.2 + 0.75}rem` }}
                >
                    <span className={activeDesign.directoryChevronClass}>
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
                            {visibleDirectoryLabel(
                                node.path,
                                node.label,
                                depth,
                            )}
                        </span>
                        <span className={activeDesign.directoryMetaClass}>
                            <span>
                                {node.descendantFileCount === 0
                                    ? hasChildren
                                        ? "Expand to browse"
                                        : "Empty folder"
                                    : `${node.descendantFileCount} file${node.descendantFileCount === 1 ? "" : "s"}`}
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
                    <span className={activeDesign.directoryTagClass}>
                        {node.descendantDirectoryCount > 0
                            ? `${node.descendantDirectoryCount} subfolder${node.descendantDirectoryCount === 1 ? "" : "s"}`
                            : "Folder"}
                    </span>
                </button>
            );
            const renderDirectoryRow = (groupedContent: ReactNode) => {
                const ribbonLabel =
                    controlPlacement === "matrix-header"
                        ? "Matrix controls"
                        : controlPlacement === "preview-dock"
                          ? "Preview controls"
                          : "Path controls";
                const groupedContentWithSidecar =
                    folderControlsInSidecar && groupedContent ? (
                        <div
                            className="grid gap-3 lg:grid-cols-[minmax(12rem,0.32fr)_minmax(0,1fr)]"
                            data-file-browser-sidecar-layout={node.path}
                        >
                            <aside
                                className="min-w-0"
                                data-file-browser-sidecar-controls={node.path}
                            >
                                {folderControls}
                            </aside>
                            <div className="min-w-0">{groupedContent}</div>
                        </div>
                    ) : (
                        groupedContent
                    );

                return (
                    <div
                        key={`dir-${node.path}`}
                        className={cn(
                            activeDesign.directoryRowBaseClass,
                            hasPreviewControls || groupedContent
                                ? activeDesign.directoryRowWithContentClass
                                : activeDesign.directoryRowCollapsedClass,
                            isSelected
                                ? activeDesign.directoryRowSelectedClass
                                : activeDesign.directoryRowIdleClass,
                        )}
                        data-directory-row={node.path}
                        data-subdir-preview-row={
                            showInlineSubdirPreview ? node.path : undefined
                        }
                    >
                        {folderControlsInNameArea ? (
                            <div
                                className="grid w-full grid-cols-1 items-start gap-2 lg:grid-cols-[minmax(0,1fr)_minmax(20rem,0.74fr)]"
                                data-directory-heading-with-controls={node.path}
                            >
                                {renderDirectoryButton()}
                                <div
                                    className="min-w-0 self-center px-2 pb-2 lg:px-0 lg:pb-0 lg:pr-2"
                                    data-file-browser-name-area-controls={
                                        node.path
                                    }
                                >
                                    {folderControls}
                                </div>
                            </div>
                        ) : (
                            renderDirectoryButton()
                        )}
                        {!folderControlsInNameArea &&
                        !folderControlsInSidecar &&
                        !folderControlsInRibbon
                            ? folderControls
                            : null}
                        {folderControlsInRibbon ? (
                            <div
                                className="space-y-2"
                                data-file-browser-content-ribbon={node.path}
                            >
                                <div className="flex flex-wrap items-center gap-2 px-1 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                                    <span>{ribbonLabel}</span>
                                    <span className="h-px min-w-8 flex-1 bg-border/70" />
                                </div>
                                {folderControls}
                            </div>
                        ) : null}
                        {showInlineSubdirPreview && inlineSubdirPreviewKinds
                            ? renderSubdirPreviewStrip(
                                  node,
                                  inlineSubdirPreviewKinds,
                              )
                            : null}
                        {groupedContentWithSidecar}
                    </div>
                );
            };
            const contentRows: ReactNode[] = [];

            if (
                isStructurallyExpanded &&
                node.path === effectiveSelectedDirectory &&
                displayedFiles.length > 0
            ) {
                const directoryDisplayedFiles = displayedFiles;

                contentRows.push(
                    <div
                        key={`files-${node.path}`}
                        className={cn(
                            previewMode === "single"
                                ? showFilePreviewWidgets
                                    ? activeDesign.fileListSinglePreviewClass
                                    : activeDesign.fileListSingleClass
                                : activeDesign.fileListGridClass,
                        )}
                        data-file-browser-directory-files={node.path}
                        data-file-browser-single-layout={
                            previewMode === "single" && showFilePreviewWidgets
                                ? node.path
                                : undefined
                        }
                    >
                        {activeDesign.id === "matrix" ? (
                            <div
                                className="grid grid-cols-[auto_minmax(10rem,1fr)_5rem_minmax(9rem,0.75fr)_5rem] items-center gap-3 border-b border-border/70 px-2 pb-2 text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground"
                                data-file-browser-file-matrix-header={node.path}
                            >
                                <span />
                                <span>Name</span>
                                <span>Size</span>
                                <span>Modified</span>
                                <span>Kind</span>
                            </div>
                        ) : null}
                        {previewMode === "single"
                            ? showFilePreviewWidgets
                                ? activeDesign.id === "deck"
                                    ? [
                                          <div
                                              key={`single-preview-${node.path}`}
                                              className={
                                                  activeDesign.singlePreviewClass
                                              }
                                              data-file-browser-preview="single"
                                              style={{
                                                  gridRow: `1 / span ${Math.max(directoryDisplayedFiles.length, 1)}`,
                                              }}
                                          >
                                              {renderSinglePreview?.(
                                                  activeFile ?? null,
                                              ) ?? null}
                                          </div>,
                                          ...directoryDisplayedFiles.map(
                                              (file) =>
                                                  cloneElement(
                                                      renderFileButton(
                                                          file,
                                                          true,
                                                          {
                                                              gridColumn: "2",
                                                          },
                                                      ),
                                                      { key: file.path },
                                                  ),
                                          ),
                                      ]
                                    : [
                                          ...directoryDisplayedFiles.map(
                                              (file) =>
                                                  cloneElement(
                                                      renderFileButton(
                                                          file,
                                                          true,
                                                      ),
                                                      { key: file.path },
                                                  ),
                                          ),
                                          <div
                                              key={`single-preview-${node.path}`}
                                              className={
                                                  activeDesign.singlePreviewClass
                                              }
                                              data-file-browser-preview="single"
                                              style={{
                                                  gridRow: `1 / span ${Math.max(directoryDisplayedFiles.length, 1)}`,
                                              }}
                                          >
                                              {renderSinglePreview?.(
                                                  activeFile ?? null,
                                              ) ?? null}
                                          </div>,
                                      ]
                                : directoryDisplayedFiles.map((file) =>
                                      cloneElement(
                                          renderFileButton(file, true),
                                          { key: file.path },
                                      ),
                                  )
                            : directoryDisplayedFiles.map((file) =>
                                  showFilePreviewWidgets ? (
                                      <div
                                          key={file.path}
                                          className={activeDesign.gridRowClass}
                                          data-file-browser-grid-row={file.path}
                                      >
                                          <div
                                              className={
                                                  activeDesign.gridFileCellClass
                                              }
                                          >
                                              {renderFileButton(file, true)}
                                          </div>
                                          <div
                                              className={
                                                  activeDesign.gridPreviewCellClass
                                              }
                                              data-grid-preview-path={file.path}
                                          >
                                              {fileMatchesPreviewKinds(
                                                  file,
                                                  selectedPreviewKinds,
                                              )
                                                  ? (renderGridPreview?.(
                                                        file,
                                                    ) ?? null)
                                                  : null}
                                          </div>
                                      </div>
                                  ) : (
                                      cloneElement(
                                          renderFileButton(file, true),
                                          { key: file.path },
                                      )
                                  ),
                              )}
                        {showFilePreviewWidgets
                            ? renderPreviewControls(node.path)
                            : null}
                    </div>,
                );
            }

            if (isStructurallyExpanded && hasChildren) {
                contentRows.push(
                    ...renderDirectoryRows(node.children, depth + 1, node.path),
                );
            }

            const showsGroupedContent = contentRows.length > 0;
            const directoryRow = renderDirectoryRow(
                showsGroupedContent ? (
                    <div
                        className={activeDesign.directoryContentClass}
                        data-directory-group-content={node.path}
                    >
                        {contentRows}
                    </div>
                ) : null,
            );

            return [
                <div
                    key={`group-${node.path}`}
                    className={activeDesign.directoryGroupClass}
                    data-directory-group={node.path}
                >
                    {directoryRow}
                </div>,
            ];
        });
    }

    return (
        <section
            className={activeDesign.sectionClass}
            data-file-browser="true"
            data-file-browser-design={activeDesign.id}
        >
            <div
                className={activeDesign.headerClass}
                data-file-browser-header="true"
            >
                <FolderTree
                    className={activeDesign.headerIconClass}
                    aria-hidden="true"
                />
                <p className={activeDesign.headerTitleClass}>File Browser</p>
                {renderDesignSelector()}
            </div>

            {renderDesignContext()}

            {files.length === 0 ? (
                <div className={activeDesign.emptyStateClass}>
                    No registered files
                </div>
            ) : null}

            {files.length > 0 && previewMode === "single" ? (
                <div
                    className={activeDesign.treeShellClass}
                    data-preview-mode="single"
                >
                    <div className={activeDesign.treeInnerClass}>
                        {renderDirectoryRows(directoryTree)}
                    </div>
                </div>
            ) : null}
            {files.length > 0 && previewMode !== "single" ? (
                <div
                    className={activeDesign.treeShellClass}
                    data-preview-mode="grid"
                >
                    <div className={activeDesign.treeInnerClass}>
                        {renderDirectoryRows(directoryTree)}
                    </div>
                </div>
            ) : null}
        </section>
    );
}
