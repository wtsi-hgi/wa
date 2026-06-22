"use client";

import {
    cloneElement,
    type CSSProperties,
    type HTMLAttributes,
    type KeyboardEvent as ReactKeyboardEvent,
    memo,
    type MouseEvent as ReactMouseEvent,
    type ReactNode,
    type TouchEvent as ReactTouchEvent,
    useCallback,
    useEffect,
    useMemo,
    useRef,
    useState,
} from "react";
import {
    ChevronDown,
    ChevronRight,
    Copy,
    Eye,
    FolderTree,
    ListFilter,
    Save,
    X,
} from "lucide-react";
import { toast } from "sonner";

import {
    boxPanelInsetClass,
    boxPanelRadiusClass,
    boxTitleIconClass,
    boxTitleTextClass,
    boxTitleTopAlignedRowClass,
} from "@/components/box-title-section";
import { LocalTimestamp } from "@/components/local-timestamp";
import { PreviewPagination } from "@/components/preview-pagination";
import { type FileEntry } from "@/lib/contracts";
import {
    fileBrowserGlobFilterStorageKey,
    filterFilesByGlobPattern,
    saveFileBrowserGlobFilter,
    useSavedFileBrowserGlobFilter,
} from "@/lib/file-glob-filter";
import {
    allPreviewFileTypeIds,
    previewFileTypeForPath,
    previewFileTypeOptions,
    type PreviewFileTypeId,
} from "@/lib/preview-file-types";
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

export type SubdirPreviewKind = PreviewFileTypeId;

const SUBDIR_PREVIEW_PAGE_SIZE = 20;
const DEFER_SUBDIR_PREVIEW_MOUNT_THRESHOLD = 80;
const PREVIEW_HEIGHT_MIN = 120;
const PREVIEW_HEIGHT_MAX = 420;
const allSubdirPreviewKinds = new Set<SubdirPreviewKind>(allPreviewFileTypeIds);
const defaultSubdirPreviewKinds = new Set<SubdirPreviewKind>(
    allSubdirPreviewKinds,
);

function previewKindForPath(path: string): SubdirPreviewKind | null {
    return previewFileTypeForPath(path);
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
    const selectedOptions = previewFileTypeOptions.filter((option) =>
        kinds.has(option.id),
    );

    if (selectedOptions.length === 0) {
        return "No file types";
    }

    if (selectedOptions.length === previewFileTypeOptions.length) {
        return "All file types";
    }

    if (selectedOptions.length === 1) {
        return selectedOptions[0]?.label ?? "1 file type";
    }

    return `${selectedOptions.length} file types`;
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

function clampPreviewHeight(value: number): number {
    if (!Number.isFinite(value)) {
        return PREVIEW_HEIGHT_MIN;
    }

    return Math.min(
        PREVIEW_HEIGHT_MAX,
        Math.max(PREVIEW_HEIGHT_MIN, Math.round(value)),
    );
}

type FileBrowserProps = {
    activeFiles?: FileEntry[];
    /** True when files, activeFiles, and visibleFiles already reflect fileFilterValue. */
    fileFilterApplied?: boolean;
    filterStorageKey?: string;
    files: FileEntry[];
    fileFilterValue?: string;
    onFileFilterChange?: (value: string) => void;
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
    renderDirectoryAction?: (node: DirectoryTreeNode) => ReactNode;
    renderGridPreview?: (file: FileEntry) => ReactNode;
    renderSinglePreview?: (file: FileEntry | null) => ReactNode;
    selectedDirectory?: string;
    selectedPath?: string;
    unfilteredFileCount?: number;
    visibleFiles?: FileEntry[];
};

type RawDirectoryNode = {
    children: Map<string, RawDirectoryNode>;
    group?: DirectoryGroup;
    name: string;
    path: string;
};

type FileBrowserControlPlacement = "name-area";

type FileBrowserDesign = {
    controlLabelClass: string;
    controlMenuClass: string;
    controlMenuHeadingClass: string;
    controlStyle: string;
    controlTriggerClass: string;
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
    id: "inline";
    pageBadgeClass: string;
    paginationClass: string;
    sectionClass: string;
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

const activeFileBrowserDesign: FileBrowserDesign = {
    controlLabelClass:
        "flex items-center justify-between gap-3 rounded-md px-1 py-0.5 text-sm",
    controlMenuClass:
        "absolute left-0 z-20 mt-2 min-w-56 rounded-lg border border-border bg-[var(--popover)] p-2 shadow-[0_16px_40px_-24px_rgba(28,40,58,0.65)]",
    controlMenuHeadingClass:
        "mb-2 border-b border-border/60 px-1 pb-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-muted-foreground",
    controlStyle: "inline-nameplate",
    controlTriggerClass:
        "inline-flex min-w-0 cursor-pointer list-none items-center gap-1.5 rounded-md border border-border/80 bg-background px-2 py-1 text-foreground shadow-sm marker:hidden hover:bg-muted/70",
    directoryButtonClass:
        "grid w-full min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-2 rounded-md px-3 py-2 text-left transition hover:bg-muted/60",
    directoryChevronClass:
        "inline-flex size-6 items-center justify-center rounded-md border border-border/70 bg-muted text-muted-foreground",
    directoryContentClass: "space-y-2 px-2 pb-2 pt-0",
    directoryGroupClass: "space-y-2",
    directoryMetaClass:
        "mt-1 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground",
    directoryRowBaseClass:
        "grid w-full grid-cols-1 gap-2 rounded-lg border transition",
    directoryRowCollapsedClass: "p-0",
    directoryRowIdleClass:
        "border-border/70 bg-background/70 hover:border-primary/35 hover:bg-muted/30",
    directoryRowSelectedClass:
        "border-primary/45 bg-primary/10 shadow-[inset_0_1px_0_rgba(255,255,255,0.45)]",
    directoryRowWithContentClass: "p-2",
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
        "mt-1.5 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground",
    fileNameClass: "block truncate text-sm font-semibold text-foreground",
    folderControlsClass:
        "file-browser-control-surface inline-nameplate-controls flex w-fit max-w-full min-w-0 flex-wrap items-center justify-start gap-1.5 rounded-md border border-border bg-[color-mix(in_oklab,var(--card)_72%,var(--foreground)_28%)] p-1.5 text-sm shadow-sm",
    gridFileCellClass: "min-w-0 border-r border-border/60 pr-2",
    gridPreviewCellClass: "min-w-0",
    gridRowClass:
        "grid gap-2 grid-cols-[minmax(18rem,0.86fr)_minmax(0,1.14fr)] items-start",
    headerClass: "-mx-0.5 relative flex min-h-9 items-start pb-4",
    headerIconClass: boxTitleIconClass,
    headerTitleClass: boxTitleTextClass,
    id: "inline",
    pageBadgeClass:
        "inline-flex items-center gap-2 rounded-md border border-border/70 bg-background px-2 py-1 text-muted-foreground",
    paginationClass:
        "inline-flex items-center gap-1 rounded-md border border-border/70 bg-background p-1",
    sectionClass: `${boxPanelRadiusClass} border border-border/75 bg-card ${boxPanelInsetClass} shadow-[0_18px_60px_-48px_rgba(48,67,98,0.8)]`,
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
        "mt-0 rounded-lg border border-border/70 bg-background/65 p-0.5",
};

const inlineControlPlacement: FileBrowserControlPlacement = "name-area";

const ResizablePreviewFrame = memo(function ResizablePreviewFrame({
    children,
    className,
    height,
    kind,
    onHeightChange,
    path,
    style,
    ...attributes
}: {
    children: ReactNode;
    className?: string;
    height: number;
    kind: "grid" | "single" | "subfolder";
    onHeightChange: (value: number) => void;
    path?: string;
    style?: CSSProperties;
} & Omit<HTMLAttributes<HTMLDivElement>, "children" | "className" | "style">) {
    const frameRef = useRef<HTMLDivElement | null>(null);
    const committedHeightRef = useRef(height);

    useEffect(() => {
        committedHeightRef.current = height;
    }, [height]);

    const commitHeight = useCallback(
        (value: number) => {
            const nextHeight = clampPreviewHeight(value);

            if (nextHeight === committedHeightRef.current) {
                return;
            }

            committedHeightRef.current = nextHeight;
            onHeightChange(nextHeight);
        },
        [onHeightChange],
    );

    useEffect(() => {
        const element = frameRef.current;

        if (!element || typeof ResizeObserver === "undefined") {
            return;
        }

        const observer = new ResizeObserver((entries) => {
            const observedHeight = entries[0]?.contentRect.height;

            if (observedHeight === undefined) {
                return;
            }

            commitHeight(observedHeight);
        });

        observer.observe(element);

        return () => {
            observer.disconnect();
        };
    }, [commitHeight]);

    const beginMouseResize = (
        event: ReactMouseEvent<HTMLButtonElement>,
    ): void => {
        event.preventDefault();
        event.stopPropagation();

        const startY = event.clientY;
        const startHeight =
            frameRef.current?.getBoundingClientRect().height || height;
        const handleMove = (moveEvent: MouseEvent) => {
            commitHeight(startHeight + moveEvent.clientY - startY);
        };
        const handleEnd = () => {
            document.removeEventListener("mousemove", handleMove);
            document.removeEventListener("mouseup", handleEnd);
        };

        document.addEventListener("mousemove", handleMove);
        document.addEventListener("mouseup", handleEnd);
    };

    const beginTouchResize = (
        event: ReactTouchEvent<HTMLButtonElement>,
    ): void => {
        const touch = event.touches[0];

        if (!touch) {
            return;
        }

        event.preventDefault();
        event.stopPropagation();

        const startY = touch.clientY;
        const startHeight =
            frameRef.current?.getBoundingClientRect().height || height;
        const handleMove = (moveEvent: TouchEvent) => {
            const nextTouch = moveEvent.touches[0];

            if (!nextTouch) {
                return;
            }

            commitHeight(startHeight + nextTouch.clientY - startY);
        };
        const handleEnd = () => {
            document.removeEventListener("touchmove", handleMove);
            document.removeEventListener("touchend", handleEnd);
            document.removeEventListener("touchcancel", handleEnd);
        };

        document.addEventListener("touchmove", handleMove, {
            passive: false,
        });
        document.addEventListener("touchend", handleEnd);
        document.addEventListener("touchcancel", handleEnd);
    };

    const handleKeyDown = (
        event: ReactKeyboardEvent<HTMLButtonElement>,
    ): void => {
        const keyDeltas: Record<string, number> = {
            ArrowDown: 10,
            ArrowUp: -10,
            PageDown: 40,
            PageUp: -40,
        };

        if (event.key === "Home") {
            event.preventDefault();
            commitHeight(PREVIEW_HEIGHT_MIN);
            return;
        }

        if (event.key === "End") {
            event.preventDefault();
            commitHeight(PREVIEW_HEIGHT_MAX);
            return;
        }

        const delta = keyDeltas[event.key];

        if (delta === undefined) {
            return;
        }

        event.preventDefault();
        commitHeight(height + delta);
    };
    const fitVisiblePreviewSurface =
        path !== undefined && previewKindForPath(path) === "image";

    return (
        <div
            {...attributes}
            ref={frameRef}
            className={cn(
                "relative min-w-0 overflow-hidden",
                fitVisiblePreviewSurface && "flex items-start justify-center",
                className,
            )}
            data-preview-resize-frame={path ?? kind}
            data-preview-resize-kind={kind}
            style={{
                ...style,
                boxSizing: "border-box",
                height: `${height}px`,
                maxHeight: `${PREVIEW_HEIGHT_MAX}px`,
                minHeight: `${PREVIEW_HEIGHT_MIN}px`,
            }}
        >
            <div
                className={cn(
                    "preview-resize-square-corner relative h-full max-w-full",
                    fitVisiblePreviewSurface
                        ? "inline-flex w-fit"
                        : "flex w-full",
                )}
                data-preview-resize-surface={path ?? kind}
            >
                {children}
                <button
                    aria-label="Resize preview height"
                    aria-orientation="vertical"
                    aria-valuemax={PREVIEW_HEIGHT_MAX}
                    aria-valuemin={PREVIEW_HEIGHT_MIN}
                    aria-valuenow={height}
                    className={cn(
                        "absolute right-0 bottom-0 z-20 block !size-9 cursor-ns-resize touch-none overflow-hidden border-0 bg-transparent p-0 text-muted-foreground/75 shadow-none",
                        "hover:text-foreground",
                        "focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none",
                    )}
                    data-preview-resize-handle={path ?? kind}
                    onKeyDown={handleKeyDown}
                    onMouseDown={beginMouseResize}
                    onTouchStart={beginTouchResize}
                    role="separator"
                    type="button"
                >
                    <span
                        aria-hidden="true"
                        className="absolute right-0 bottom-0 block size-6 border-r border-b border-border/70 [clip-path:polygon(100%_0,0_100%,100%_100%)]"
                        style={{
                            backgroundColor:
                                "color-mix(in oklab, var(--background) 88%, var(--muted-foreground) 12%)",
                            backgroundImage:
                                "repeating-linear-gradient(135deg, transparent 0 4px, currentColor 4px 5px, transparent 5px 6px)",
                        }}
                    />
                </button>
            </div>
        </div>
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

function directoryContainsFile(
    directoryPath: string,
    files: FileEntry[],
): boolean {
    if (directoryPath === "/") {
        return files.length > 0;
    }

    return files.some(
        (file) =>
            parentDirectory(file.path) === directoryPath ||
            file.path.startsWith(`${directoryPath}/`),
    );
}

function fileName(path: string): string {
    return path.split("/").pop() ?? path;
}

function fallbackCopyText(value: string): boolean {
    if (
        typeof document === "undefined" ||
        typeof document.execCommand !== "function"
    ) {
        return false;
    }

    const textarea = document.createElement("textarea");

    textarea.value = value;
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

async function copyText(value: string): Promise<boolean> {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
        try {
            await navigator.clipboard.writeText(value);
            return true;
        } catch {
            return fallbackCopyText(value);
        }
    }

    return fallbackCopyText(value);
}

function CopyPathControl({
    kind,
    path,
}: {
    kind: "directory" | "file";
    path: string;
}) {
    const handleCopy = async () => {
        const copied = await copyText(path);

        if (copied) {
            toast.success("Path copied");
            return;
        }

        toast.error("Could not copy path");
    };
    const handleClick = (event: ReactMouseEvent<HTMLSpanElement>): void => {
        event.preventDefault();
        event.stopPropagation();
        void handleCopy();
    };
    const handleKeyDown = (
        event: ReactKeyboardEvent<HTMLSpanElement>,
    ): void => {
        event.stopPropagation();

        if (event.key !== "Enter" && event.key !== " ") {
            return;
        }

        event.preventDefault();
        void handleCopy();
    };

    return (
        <span
            aria-label={`Copy ${kind} full path ${path}`}
            className="inline-flex size-6 shrink-0 items-center justify-center rounded-md border border-border/70 bg-background/80 text-muted-foreground shadow-sm transition hover:border-primary/45 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45"
            data-copy-path-control={kind}
            onClick={handleClick}
            onKeyDown={handleKeyDown}
            role="button"
            tabIndex={0}
            title="Copy full path"
        >
            <Copy className="size-3.5" aria-hidden="true" />
        </span>
    );
}

function SubdirPreviewLoadingCard({ count }: { count: number }) {
    return (
        <div className="flex h-full w-full box-border flex-col items-center justify-center rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 px-5 py-8 text-center text-sm text-muted-foreground">
            <span>Loading preview...</span>
            {count > 1 ? (
                <span className="mt-1 text-xs">{count} files queued</span>
            ) : null}
        </div>
    );
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
        .map(([type, count]) => `${count} ${type.toUpperCase()}`)
        .join(", ");
}

function renderMetaItems(items: ReactNode[]): ReactNode[] {
    return items.flatMap((item, index) => {
        const keyedItem = <span key={`item-${index}`}>{item}</span>;

        if (index === 0) {
            return [keyedItem];
        }

        return [
            <span
                aria-hidden="true"
                className="text-muted-foreground/60"
                data-file-browser-meta-separator="true"
                key={`separator-${index}`}
            >
                ·
            </span>,
            keyedItem,
        ];
    });
}

function renderDirectoryMetaItems(
    node: DirectoryTreeNode,
    hasChildren: boolean,
): ReactNode[] {
    const items: ReactNode[] = [
        <span data-directory-file-count={node.path} key="file-count">
            {node.descendantFileCount === 0
                ? hasChildren
                    ? "Expand to browse"
                    : "Empty folder"
                : `${node.descendantFileCount} file${node.descendantFileCount === 1 ? "" : "s"}`}
        </span>,
        <span data-directory-subfolder-count={node.path} key="subfolder-count">
            {node.descendantDirectoryCount > 0
                ? `${node.descendantDirectoryCount} subfolder${node.descendantDirectoryCount === 1 ? "" : "s"}`
                : "Folder"}
        </span>,
    ];

    if (node.totalSize > 0) {
        items.push(<span key="total-size">{formatBytes(node.totalSize)}</span>);
    }

    if (Object.keys(node.typeCounts).length > 0) {
        items.push(
            <span data-directory-type-summary={node.path} key="type-summary">
                {formatTypeSummary(node.typeCounts)}
            </span>,
        );
    }

    return renderMetaItems(items);
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
    activeFiles: activeFilesOverride,
    fileFilterApplied = false,
    filterStorageKey,
    files,
    fileFilterValue,
    onFileFilterChange,
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
    renderDirectoryAction,
    renderGridPreview,
    renderSinglePreview,
    selectedDirectory,
    selectedPath,
    unfilteredFileCount,
    visibleFiles,
}: FileBrowserProps) {
    const activeDesign = activeFileBrowserDesign;
    const resolvedFilterStorageKey = useMemo(
        () => fileBrowserGlobFilterStorageKey(filterStorageKey),
        [filterStorageKey],
    );
    const savedFileFilter = useSavedFileBrowserGlobFilter(filterStorageKey);
    const [uncontrolledFileFilter, setUncontrolledFileFilter] = useState<{
        storageKey: string | undefined;
        value: string;
    } | null>(null);
    const effectiveUncontrolledFileFilter =
        uncontrolledFileFilter &&
        uncontrolledFileFilter.storageKey === resolvedFilterStorageKey
            ? uncontrolledFileFilter.value
            : savedFileFilter;
    const effectiveFileFilter =
        fileFilterValue ?? effectiveUncontrolledFileFilter;
    const filteredFiles = useMemo(
        () =>
            fileFilterApplied
                ? files
                : filterFilesByGlobPattern(files, effectiveFileFilter),
        [effectiveFileFilter, fileFilterApplied, files],
    );
    const filteredActiveFilesOverride = useMemo(
        () =>
            activeFilesOverride && !fileFilterApplied
                ? filterFilesByGlobPattern(
                      activeFilesOverride,
                      effectiveFileFilter,
                  )
                : activeFilesOverride,
        [activeFilesOverride, effectiveFileFilter, fileFilterApplied],
    );
    const filteredVisibleFiles = useMemo(
        () =>
            visibleFiles && !fileFilterApplied
                ? filterFilesByGlobPattern(visibleFiles, effectiveFileFilter)
                : visibleFiles,
        [effectiveFileFilter, fileFilterApplied, visibleFiles],
    );
    const registeredFileCount = unfilteredFileCount ?? files.length;
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
                    selectedDirectory ??
                        (renderGridPreview
                            ? findInitialSubdirPreviewDirectory(filteredFiles)
                            : undefined) ??
                        parentDirectory(filteredFiles[0]?.path ?? "/"),
                ),
            ),
    );
    const initialDirectoryNotificationRef = useRef<string | undefined>(
        undefined,
    );
    const directoryGroups = useMemo(
        () => buildDirectoryGroups(filteredFiles),
        [filteredFiles],
    );
    const directoryTree = useMemo(
        () => buildDirectoryTree(filteredFiles),
        [filteredFiles],
    );
    const initialSubdirPreviewDirectory = useMemo(
        () => findInitialSubdirPreviewDirectory(filteredFiles),
        [filteredFiles],
    );
    const requestedDirectory =
        selectedDirectory ??
        uncontrolledDirectory ??
        (renderGridPreview ? initialSubdirPreviewDirectory : undefined);
    const fallbackDirectory =
        (renderGridPreview ? initialSubdirPreviewDirectory : undefined) ??
        directoryGroups[0]?.path;
    const preferredDirectory =
        requestedDirectory &&
        directoryContainsFile(requestedDirectory, filteredFiles)
            ? requestedDirectory
            : fallbackDirectory;
    const activeDirectory = directoryGroups.find(
        (group) => group.path === preferredDirectory,
    );
    const activeFiles =
        filteredActiveFilesOverride ?? activeDirectory?.files ?? [];
    const effectiveSelectedDirectory = preferredDirectory;
    const [uncontrolledPreviewHeight, setUncontrolledPreviewHeight] =
        useState(previewHeight);
    const [subdirPreviewEnabledByPath, setSubdirPreviewEnabledByPath] =
        useState<Record<string, boolean>>({});
    const [subdirPreviewActivatingByPath, setSubdirPreviewActivatingByPath] =
        useState<Record<string, boolean>>({});
    const [subdirPreviewReadyByPath, setSubdirPreviewReadyByPath] = useState<
        Record<string, boolean>
    >({});
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
    const scheduledSubdirPreviewRenderRef = useRef<Record<string, () => void>>(
        {},
    );
    const scheduledSubdirPreviewActivationRef = useRef<
        Record<string, () => void>
    >({});
    const effectivePreviewHeight = clampPreviewHeight(
        onPreviewHeightChange ? previewHeight : uncontrolledPreviewHeight,
    );
    const subdirPreviewEnabledFor = (directoryPath: string): boolean =>
        subdirPreviewEnabledByPath[directoryPath] ?? false;
    const subdirPreviewActivatingFor = (directoryPath: string): boolean =>
        subdirPreviewActivatingByPath[directoryPath] ?? false;
    const subdirPreviewReadyFor = (directoryPath: string): boolean =>
        subdirPreviewReadyByPath[directoryPath] ?? false;
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
    const hasSupportedPreviewableActiveFiles = activeFiles.some((file) =>
        fileMatchesPreviewKinds(file, allSubdirPreviewKinds),
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
    const displayedFiles = filteredVisibleFiles ?? activeFiles;
    const previewableDisplayedFiles = displayedFiles.filter((file) =>
        fileMatchesPreviewKinds(file, selectedPreviewKinds),
    );
    const hasPreviewableActiveFiles = previewableActiveFiles.length > 0;
    const updateFileFilter = useCallback(
        (value: string) => {
            setUncontrolledFileFilter({
                storageKey: resolvedFilterStorageKey,
                value,
            });
            onFileFilterChange?.(value);
        },
        [onFileFilterChange, resolvedFilterStorageKey],
    );
    const handleSaveFileFilter = useCallback(() => {
        saveFileBrowserGlobFilter(filterStorageKey, effectiveFileFilter);
    }, [effectiveFileFilter, filterStorageKey]);
    const cancelScheduledSubdirPreviewRender = useCallback(
        (directoryPath: string): void => {
            const cancel =
                scheduledSubdirPreviewRenderRef.current[directoryPath];

            if (!cancel) {
                return;
            }

            cancel();
            delete scheduledSubdirPreviewRenderRef.current[directoryPath];
        },
        [],
    );
    const cancelScheduledSubdirPreviewActivation = useCallback(
        (directoryPath: string): void => {
            const cancel =
                scheduledSubdirPreviewActivationRef.current[directoryPath];

            if (!cancel) {
                return;
            }

            cancel();
            delete scheduledSubdirPreviewActivationRef.current[directoryPath];
        },
        [],
    );
    const clearSubdirPreviewActivating = useCallback(
        (directoryPath: string): void => {
            setSubdirPreviewActivatingByPath((current) => {
                if (!(directoryPath in current)) {
                    return current;
                }

                const next = { ...current };

                delete next[directoryPath];

                return next;
            });
        },
        [],
    );
    const clearSubdirPreviewReady = useCallback(
        (directoryPath: string): void => {
            setSubdirPreviewReadyByPath((current) => {
                if (!(directoryPath in current)) {
                    return current;
                }

                const next = { ...current };

                delete next[directoryPath];

                return next;
            });
        },
        [],
    );
    const scheduleSubdirPreviewRender = useCallback(
        (directoryPath: string): void => {
            cancelScheduledSubdirPreviewRender(directoryPath);
            setSubdirPreviewReadyByPath((current) => ({
                ...current,
                [directoryPath]: false,
            }));

            if (
                typeof window === "undefined" ||
                typeof window.requestAnimationFrame !== "function"
            ) {
                if (typeof window === "undefined") {
                    setSubdirPreviewReadyByPath((current) => ({
                        ...current,
                        [directoryPath]: true,
                    }));
                    return;
                }

                const timeout = window.setTimeout(() => {
                    delete scheduledSubdirPreviewRenderRef.current[
                        directoryPath
                    ];
                    setSubdirPreviewReadyByPath((current) => ({
                        ...current,
                        [directoryPath]: true,
                    }));
                }, 0);

                scheduledSubdirPreviewRenderRef.current[directoryPath] = () => {
                    window.clearTimeout(timeout);
                };
                return;
            }

            let secondFrame: number | null = null;
            const firstFrame = window.requestAnimationFrame(() => {
                secondFrame = window.requestAnimationFrame(() => {
                    delete scheduledSubdirPreviewRenderRef.current[
                        directoryPath
                    ];
                    setSubdirPreviewReadyByPath((current) => ({
                        ...current,
                        [directoryPath]: true,
                    }));
                });
            });

            scheduledSubdirPreviewRenderRef.current[directoryPath] = () => {
                window.cancelAnimationFrame(firstFrame);

                if (secondFrame !== null) {
                    window.cancelAnimationFrame(secondFrame);
                }
            };
        },
        [cancelScheduledSubdirPreviewRender],
    );
    const prepareSubdirPreviewRender = useCallback(
        (directoryPath: string, deferRender: boolean): void => {
            if (deferRender) {
                scheduleSubdirPreviewRender(directoryPath);
                return;
            }

            cancelScheduledSubdirPreviewRender(directoryPath);
            setSubdirPreviewReadyByPath((current) => ({
                ...current,
                [directoryPath]: true,
            }));
        },
        [cancelScheduledSubdirPreviewRender, scheduleSubdirPreviewRender],
    );
    const enableSubdirPreview = useCallback(
        (directoryPath: string, deferRender: boolean): void => {
            cancelScheduledSubdirPreviewActivation(directoryPath);
            clearSubdirPreviewActivating(directoryPath);
            setSubdirPreviewEnabledByPath((current) => ({
                ...current,
                [directoryPath]: true,
            }));
            setSubdirPreviewPages((current) => ({
                ...current,
                [directoryPath]: 1,
            }));
            prepareSubdirPreviewRender(directoryPath, deferRender);
        },
        [
            cancelScheduledSubdirPreviewActivation,
            clearSubdirPreviewActivating,
            prepareSubdirPreviewRender,
        ],
    );
    const scheduleSubdirPreviewActivation = useCallback(
        (directoryPath: string, deferRender: boolean): void => {
            cancelScheduledSubdirPreviewActivation(directoryPath);
            setSubdirPreviewActivatingByPath((current) => ({
                ...current,
                [directoryPath]: true,
            }));
            setSubdirPreviewPages((current) => ({
                ...current,
                [directoryPath]: 1,
            }));

            const activate = () => {
                delete scheduledSubdirPreviewActivationRef.current[
                    directoryPath
                ];
                enableSubdirPreview(directoryPath, deferRender);
            };

            if (
                typeof window === "undefined" ||
                typeof window.requestAnimationFrame !== "function"
            ) {
                if (typeof window === "undefined") {
                    activate();
                    return;
                }

                const timeout = window.setTimeout(activate, 0);

                scheduledSubdirPreviewActivationRef.current[directoryPath] =
                    () => {
                        window.clearTimeout(timeout);
                    };
                return;
            }

            let secondFrame: number | null = null;
            const firstFrame = window.requestAnimationFrame(() => {
                secondFrame = window.requestAnimationFrame(activate);
            });

            scheduledSubdirPreviewActivationRef.current[directoryPath] = () => {
                window.cancelAnimationFrame(firstFrame);

                if (secondFrame !== null) {
                    window.cancelAnimationFrame(secondFrame);
                }
            };
        },
        [cancelScheduledSubdirPreviewActivation, enableSubdirPreview],
    );
    const updateSubdirPreviewKinds = useCallback(
        (
            directoryPath: string,
            kinds: ReadonlySet<SubdirPreviewKind>,
            deferRender: boolean,
        ): void => {
            setSubdirPreviewKindsByPath((current) => ({
                ...current,
                [directoryPath]: new Set(kinds),
            }));
            setSubdirPreviewPages((current) => ({
                ...current,
                [directoryPath]: 1,
            }));

            if (subdirPreviewEnabledByPath[directoryPath]) {
                prepareSubdirPreviewRender(directoryPath, deferRender);
            }
        },
        [prepareSubdirPreviewRender, subdirPreviewEnabledByPath],
    );

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
    useEffect(() => {
        const scheduledRenders = scheduledSubdirPreviewRenderRef.current;
        const scheduledActivations =
            scheduledSubdirPreviewActivationRef.current;

        return () => {
            for (const cancel of Object.values(scheduledRenders)) {
                cancel();
            }

            for (const cancel of Object.values(scheduledActivations)) {
                cancel();
            }

            scheduledSubdirPreviewRenderRef.current = {};
            scheduledSubdirPreviewActivationRef.current = {};
        };
    }, []);

    const handlePreviewHeightCommit = useCallback(
        (value: number) => {
            const nextHeight = clampPreviewHeight(value);

            if (onPreviewHeightChange) {
                onPreviewHeightChange(nextHeight);
                return;
            }

            setUncontrolledPreviewHeight(nextHeight);
        },
        [onPreviewHeightChange],
    );
    const renderFileButton = (
        file: FileEntry,
        compact = false,
        style?: CSSProperties,
    ) => {
        const name = fileName(file.path);

        return (
            <button
                type="button"
                aria-label={`Select file ${name}`}
                key={file.path}
                className={cn(
                    activeDesign.fileButtonBaseClass,
                    effectiveSelectedPath === file.path &&
                        activeDesign.fileButtonSelectedClass,
                    compact && activeDesign.fileButtonCompactClass,
                )}
                data-file-browser-file-layout="card"
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
                <span className="min-w-0 flex-1">
                    <span className="flex min-w-0 items-center gap-1.5">
                        <span
                            className={cn(
                                activeDesign.fileNameClass,
                                "min-w-0 flex-1",
                            )}
                        >
                            {name}
                        </span>
                        <CopyPathControl kind="file" path={file.path} />
                    </span>
                    <span className={activeDesign.fileMetaClass}>
                        {renderMetaItems([
                            formatBytes(file.size),
                            <LocalTimestamp
                                format="fileDateTime"
                                key="mtime"
                                value={file.mtime}
                            />,
                            <span data-file-kind={file.path} key="file-kind">
                                {file.kind}
                            </span>,
                        ])}
                    </span>
                </span>
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
            deferSubdirPreviewRender: boolean;
            hasFilePreviewControls: boolean;
            hasSubdirPreviewControls: boolean;
            showGridToggle: boolean;
            subdirPageCount: number;
            safeSubdirPreviewPage: number;
        },
    ) => {
        const {
            deferSubdirPreviewRender,
            hasFilePreviewControls,
            hasSubdirPreviewControls,
            safeSubdirPreviewPage,
            showGridToggle,
            subdirPageCount,
        } = options;
        const showPreviewPaging = previewPageCount > 1;
        const subdirPreviewEnabled = subdirPreviewEnabledFor(directoryPath);
        const subdirPreviewActivating =
            subdirPreviewActivatingFor(directoryPath);
        const subdirPreviewModeActive =
            subdirPreviewEnabled || subdirPreviewActivating;
        const showSubdirPreviewLoadingStatus =
            hasSubdirPreviewControls &&
            (subdirPreviewActivating ||
                (subdirPreviewEnabled &&
                    deferSubdirPreviewRender &&
                    !subdirPreviewReadyFor(directoryPath)));
        const subdirPreviewKinds = subdirPreviewKindsFor(directoryPath);
        const controlPlacement = inlineControlPlacement;
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
                            <span className="flex min-w-0 flex-col leading-tight">
                                <span
                                    className="whitespace-nowrap font-medium"
                                    data-file-browser-control-label="preview-modes"
                                >
                                    Preview modes
                                </span>
                                <span
                                    className="whitespace-nowrap text-[11px] text-muted-foreground"
                                    data-file-browser-control-current="preview-modes"
                                >
                                    {summarizePreviewModes(
                                        previewMode,
                                        subdirPreviewModeActive,
                                        showGridToggle,
                                    )}
                                </span>
                            </span>
                            <ChevronDown className="size-3.5 text-muted-foreground" />
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
                                            checked={subdirPreviewModeActive}
                                            className="size-4 accent-primary"
                                            onChange={(event) => {
                                                const nextEnabled =
                                                    event.target.checked;

                                                if (nextEnabled) {
                                                    if (
                                                        deferSubdirPreviewRender
                                                    ) {
                                                        scheduleSubdirPreviewActivation(
                                                            directoryPath,
                                                            deferSubdirPreviewRender,
                                                        );
                                                    } else {
                                                        enableSubdirPreview(
                                                            directoryPath,
                                                            deferSubdirPreviewRender,
                                                        );
                                                    }
                                                } else {
                                                    cancelScheduledSubdirPreviewActivation(
                                                        directoryPath,
                                                    );
                                                    cancelScheduledSubdirPreviewRender(
                                                        directoryPath,
                                                    );
                                                    clearSubdirPreviewActivating(
                                                        directoryPath,
                                                    );
                                                    setSubdirPreviewEnabledByPath(
                                                        (current) => ({
                                                            ...current,
                                                            [directoryPath]: false,
                                                        }),
                                                    );
                                                    clearSubdirPreviewReady(
                                                        directoryPath,
                                                    );
                                                }
                                            }}
                                            type="checkbox"
                                        />
                                    </label>
                                ) : null}
                            </div>
                        </div>
                    </details>
                ) : null}

                {showSubdirPreviewLoadingStatus ? (
                    <span
                        aria-live="polite"
                        className="inline-flex items-center rounded-md border border-dashed border-border/70 bg-background/80 px-2 py-1 text-xs font-medium text-muted-foreground"
                        data-subdir-preview-loading-status={directoryPath}
                        role="status"
                    >
                        Loading previews...
                    </span>
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
                                <span className="flex min-w-0 flex-col leading-tight">
                                    <span
                                        className="whitespace-nowrap font-medium"
                                        data-file-browser-control-label="file-types"
                                    >
                                        File types
                                    </span>
                                    <span
                                        className="whitespace-nowrap text-[11px] text-muted-foreground"
                                        data-file-browser-control-current="file-types"
                                    >
                                        {summarizeSubdirPreviewKinds(
                                            subdirPreviewKinds,
                                        )}
                                    </span>
                                </span>
                                <ChevronDown className="size-3.5 text-muted-foreground" />
                            </summary>
                            <div
                                className={cn(
                                    activeDesign.controlMenuClass,
                                    "right-0 left-auto w-[min(28rem,calc(100vw-2rem))] max-w-[calc(100vw-2rem)]",
                                )}
                                data-subdir-preview-kinds={directoryPath}
                            >
                                <div
                                    className={cn(
                                        activeDesign.controlMenuHeadingClass,
                                        "flex items-center justify-between gap-2",
                                    )}
                                >
                                    <span>File types</span>
                                    <button
                                        aria-label="Deselect all file types"
                                        className="inline-flex shrink-0 items-center gap-1 rounded-md border border-border/70 bg-background px-1.5 py-0.5 text-[11px] font-medium normal-case tracking-normal text-muted-foreground transition hover:border-primary/45 hover:bg-muted/70 hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
                                        data-subdir-preview-kind-clear={
                                            directoryPath
                                        }
                                        disabled={subdirPreviewKinds.size === 0}
                                        onClick={() => {
                                            updateSubdirPreviewKinds(
                                                directoryPath,
                                                new Set<SubdirPreviewKind>(),
                                                deferSubdirPreviewRender,
                                            );
                                        }}
                                        type="button"
                                    >
                                        <X
                                            aria-hidden="true"
                                            className="size-3"
                                        />
                                        <span>Deselect all</span>
                                    </button>
                                </div>
                                <div
                                    className="grid grid-cols-[repeat(auto-fit,minmax(5.6rem,1fr))] gap-1"
                                    data-subdir-preview-kind-options={
                                        directoryPath
                                    }
                                >
                                    {previewFileTypeOptions.map((option) => (
                                        <label
                                            key={option.id}
                                            className="inline-flex min-w-0 cursor-pointer items-center justify-start gap-1.5 rounded-md border border-border/70 bg-background px-2 py-1 text-xs font-medium text-foreground hover:border-primary/45 hover:bg-muted/55"
                                        >
                                            <input
                                                checked={subdirPreviewKinds.has(
                                                    option.id,
                                                )}
                                                className="size-3.5 accent-primary"
                                                data-subdir-preview-kind={
                                                    option.id
                                                }
                                                onChange={(event) => {
                                                    const nextKinds = new Set(
                                                        subdirPreviewKinds,
                                                    );

                                                    if (event.target.checked) {
                                                        nextKinds.add(
                                                            option.id,
                                                        );
                                                    } else {
                                                        nextKinds.delete(
                                                            option.id,
                                                        );
                                                    }

                                                    updateSubdirPreviewKinds(
                                                        directoryPath,
                                                        nextKinds,
                                                        deferSubdirPreviewRender,
                                                    );
                                                }}
                                                type="checkbox"
                                            />
                                            <span className="truncate">
                                                {option.label}
                                            </span>
                                        </label>
                                    ))}
                                </div>
                            </div>
                        </details>
                    </>
                ) : null}

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

                                if (subdirPreviewEnabledFor(directoryPath)) {
                                    prepareSubdirPreviewRender(
                                        directoryPath,
                                        deferSubdirPreviewRender,
                                    );
                                }
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
        const available = eligibleSubdirs.length > 0;
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
        previewsReady: boolean,
    ): ReactNode => {
        const previewableFiles = previewableFilesForKinds(subdir, kinds);
        const previewCards = previewsReady
            ? previewableFiles.map((file) => ({
                  file,
                  isLoadingSummary: false,
              }))
            : previewableFiles.slice(0, 1).map((file) => ({
                  file,
                  isLoadingSummary: true,
              }));

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
                    {previewCards.map(({ file, isLoadingSummary }) =>
                        (() => {
                            const previewKind = previewKindForPath(file.path);
                            const isVisualSubdirPreview =
                                previewKind === "image" ||
                                previewKind === "svg";
                            const cardKey = isLoadingSummary
                                ? `${subdir.path}::loading`
                                : file.path;

                            return (
                                <div
                                    key={cardKey}
                                    className={cn(
                                        activeDesign.subdirCardBaseClass,
                                        isVisualSubdirPreview
                                            ? activeDesign.subdirImageCardClass
                                            : activeDesign.subdirTextCardClass,
                                    )}
                                    data-subdir-preview-card={cardKey}
                                    style={{
                                        maxWidth: `calc(var(--subdir-preview-height) * 1.8)`,
                                    }}
                                >
                                    <div className="flex min-w-0 items-center gap-1.5">
                                        <p
                                            className={cn(
                                                activeDesign.subdirFilenameClass,
                                                "min-w-0 flex-1",
                                            )}
                                            data-subdir-preview-filename={
                                                isLoadingSummary
                                                    ? cardKey
                                                    : file.path
                                            }
                                            title={
                                                isLoadingSummary
                                                    ? `${previewableFiles.length} previews queued`
                                                    : fileName(file.path)
                                            }
                                        >
                                            {isLoadingSummary
                                                ? `${previewableFiles.length} previews queued`
                                                : fileName(file.path)}
                                        </p>
                                        {isLoadingSummary ? null : (
                                            <CopyPathControl
                                                kind="file"
                                                path={file.path}
                                            />
                                        )}
                                    </div>
                                    <ResizablePreviewFrame
                                        className={cn(
                                            activeDesign.subdirFrameBaseClass,
                                            isVisualSubdirPreview
                                                ? activeDesign.subdirImageFrameClass
                                                : activeDesign.subdirTextFrameClass,
                                        )}
                                        data-subdir-preview-frame={file.path}
                                        height={effectivePreviewHeight}
                                        kind="subfolder"
                                        onHeightChange={
                                            handlePreviewHeightCommit
                                        }
                                        path={
                                            isLoadingSummary
                                                ? cardKey
                                                : file.path
                                        }
                                        style={{
                                            height: `var(--subdir-preview-height)`,
                                        }}
                                    >
                                        {isLoadingSummary ? (
                                            <SubdirPreviewLoadingCard
                                                count={previewableFiles.length}
                                            />
                                        ) : (
                                            (renderGridPreview?.(file) ?? null)
                                        )}
                                    </ResizablePreviewFrame>
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
            const hasExpandedDescendant = node.children.some((child) =>
                collectTreePaths(child).some((path) =>
                    visibleExpandedDirectories.has(path),
                ),
            );
            const nodePreviewKinds = subdirPreviewKindsFor(node.path);
            const visibleSubdirPreviewFileCount = visibleSubdirs.reduce(
                (count, subdir) =>
                    count +
                    previewableFilesForKinds(subdir, nodePreviewKinds).length,
                0,
            );
            const deferSubdirPreviewRender =
                visibleSubdirPreviewFileCount >=
                DEFER_SUBDIR_PREVIEW_MOUNT_THRESHOLD;
            const previewableNodeFiles = node.files.filter((file) =>
                fileMatchesPreviewKinds(file, nodePreviewKinds),
            );
            const hasFilePreviewControls =
                isStructurallyExpanded &&
                isSelected &&
                activeFiles.length > 0 &&
                hasSupportedPreviewableActiveFiles &&
                Boolean(renderGridPreview || renderSinglePreview);
            const showFilePreviewWidgets =
                node.path === effectiveSelectedDirectory &&
                hasPreviewableActiveFiles;
            const hasSubdirPreviewControls =
                isStructurallyExpanded &&
                !hasExpandedDescendant &&
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
            const inlineSubdirPreviewsReady = parentNode
                ? subdirPreviewReadyFor(parentNode.path)
                : false;
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
                deferSubdirPreviewRender,
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
            const isRootDirectoryRow = depth === 0;
            const controlPlacement = inlineControlPlacement;
            const folderControlsInNameArea =
                controlPlacement === "name-area" && Boolean(folderControls);
            const directoryAction = renderDirectoryAction?.(node) ?? null;
            const directoryDisplayLabel = visibleDirectoryLabel(
                node.path,
                node.label,
                depth,
            );
            const headingSideContent =
                folderControlsInNameArea || directoryAction ? (
                    <div
                        className="flex max-w-full min-w-0 flex-wrap items-center justify-end gap-2"
                        data-file-browser-name-area-actions={node.path}
                    >
                        {directoryAction}
                        {folderControlsInNameArea ? folderControls : null}
                    </div>
                ) : null;
            const renderDirectoryButton = () => (
                <button
                    type="button"
                    aria-label={`Toggle directory ${directoryDisplayLabel}`}
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
                        <span
                            className="block truncate text-base font-medium text-foreground"
                            title={node.path}
                        >
                            {directoryDisplayLabel}
                        </span>
                        <span className="mt-1 flex min-w-0 items-start gap-1.5">
                            <span
                                className={cn(
                                    activeDesign.directoryMetaClass,
                                    "mt-0 min-w-0 flex-1",
                                )}
                                data-directory-meta={node.path}
                            >
                                {renderDirectoryMetaItems(node, hasChildren)}
                            </span>
                            <CopyPathControl
                                kind="directory"
                                path={node.path}
                            />
                        </span>
                    </span>
                </button>
            );
            const renderDirectoryRow = (groupedContent: ReactNode) => {
                return (
                    <div
                        key={`dir-${node.path}`}
                        className={cn(
                            isRootDirectoryRow
                                ? "grid w-full grid-cols-1 gap-2 transition"
                                : activeDesign.directoryRowBaseClass,
                            isRootDirectoryRow
                                ? null
                                : hasPreviewControls || groupedContent
                                  ? activeDesign.directoryRowWithContentClass
                                  : activeDesign.directoryRowCollapsedClass,
                            isRootDirectoryRow
                                ? null
                                : isSelected
                                  ? activeDesign.directoryRowSelectedClass
                                  : activeDesign.directoryRowIdleClass,
                        )}
                        data-directory-row={node.path}
                        data-subdir-preview-row={
                            showInlineSubdirPreview ? node.path : undefined
                        }
                    >
                        {headingSideContent ? (
                            <div
                                className={cn(
                                    "grid w-full grid-cols-1 items-start gap-2",
                                    "lg:grid-cols-[minmax(0,1fr)_auto]",
                                )}
                                data-directory-heading-with-controls={node.path}
                            >
                                {renderDirectoryButton()}
                                <div
                                    className="min-w-0 self-center p-2 lg:pl-0"
                                    data-file-browser-name-area-controls={
                                        node.path
                                    }
                                >
                                    {headingSideContent}
                                </div>
                            </div>
                        ) : (
                            renderDirectoryButton()
                        )}
                        {!folderControlsInNameArea ? folderControls : null}
                        {showInlineSubdirPreview && inlineSubdirPreviewKinds
                            ? renderSubdirPreviewStrip(
                                  node,
                                  inlineSubdirPreviewKinds,
                                  inlineSubdirPreviewsReady,
                              )
                            : null}
                        {groupedContent}
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
                        {previewMode === "single"
                            ? showFilePreviewWidgets
                                ? [
                                      ...directoryDisplayedFiles.map((file) =>
                                          cloneElement(
                                              renderFileButton(file, true),
                                              { key: file.path },
                                          ),
                                      ),
                                      <ResizablePreviewFrame
                                          key={`single-preview-${node.path}`}
                                          className={
                                              activeDesign.singlePreviewClass
                                          }
                                          data-file-browser-preview="single"
                                          height={effectivePreviewHeight}
                                          kind="single"
                                          onHeightChange={
                                              handlePreviewHeightCommit
                                          }
                                          path={activeFile?.path ?? node.path}
                                          style={{
                                              gridRow: `1 / span ${Math.max(directoryDisplayedFiles.length, 1)}`,
                                          }}
                                      >
                                          {renderSinglePreview?.(
                                              activeFile ?? null,
                                          ) ?? null}
                                      </ResizablePreviewFrame>,
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
                                              ) ? (
                                                  <ResizablePreviewFrame
                                                      height={
                                                          effectivePreviewHeight
                                                      }
                                                      kind="grid"
                                                      onHeightChange={
                                                          handlePreviewHeightCommit
                                                      }
                                                      path={file.path}
                                                  >
                                                      {renderGridPreview?.(
                                                          file,
                                                      ) ?? null}
                                                  </ResizablePreviewFrame>
                                              ) : null}
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
                        className={
                            isRootDirectoryRow
                                ? "space-y-2 pt-0"
                                : activeDesign.directoryContentClass
                        }
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
            data-preview-height={effectivePreviewHeight}
        >
            <div
                className={activeDesign.headerClass}
                data-file-browser-header="true"
            >
                <div className={boxTitleTopAlignedRowClass}>
                    <FolderTree
                        className={activeDesign.headerIconClass}
                        aria-hidden="true"
                    />
                    <p className={activeDesign.headerTitleClass}>
                        File Browser
                    </p>
                    <div
                        className="mt-3 flex w-full min-w-0 max-w-full items-center gap-1.5 sm:absolute sm:top-0 sm:right-0 sm:mt-0 sm:w-auto"
                        data-file-browser-filter-controls="true"
                    >
                        <input
                            aria-label="Filter files by glob"
                            className="h-8 w-full max-w-[min(16rem,52vw)] rounded-md border border-border/80 bg-background px-2.5 font-mono text-sm text-foreground shadow-sm transition placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45 sm:w-44"
                            data-file-browser-glob-filter="true"
                            onChange={(event) =>
                                updateFileFilter(event.target.value)
                            }
                            placeholder="Glob filter"
                            type="search"
                            value={effectiveFileFilter}
                        />
                        <button
                            aria-label="Save file glob filter"
                            className="inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-border/80 bg-background text-muted-foreground shadow-sm transition hover:border-primary/45 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45 disabled:cursor-not-allowed disabled:opacity-45"
                            data-file-browser-glob-filter-save="true"
                            disabled={!resolvedFilterStorageKey}
                            onClick={handleSaveFileFilter}
                            title="Save glob filter"
                            type="button"
                        >
                            <Save className="size-4" aria-hidden="true" />
                        </button>
                    </div>
                </div>
            </div>

            {registeredFileCount === 0 ? (
                <div className={activeDesign.emptyStateClass}>
                    No registered files
                </div>
            ) : null}

            {registeredFileCount > 0 && filteredFiles.length === 0 ? (
                <div
                    className={activeDesign.emptyStateClass}
                    data-file-browser-empty-filter="true"
                >
                    No files match
                </div>
            ) : null}

            {filteredFiles.length > 0 && previewMode === "single" ? (
                <div
                    className={activeDesign.treeShellClass}
                    data-preview-mode="single"
                >
                    <div className={activeDesign.treeInnerClass}>
                        {renderDirectoryRows(directoryTree)}
                    </div>
                </div>
            ) : null}
            {filteredFiles.length > 0 && previewMode !== "single" ? (
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
