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
    Eye,
    FolderTree,
    ListFilter,
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

export type SubdirPreviewKind = "image" | "table" | "markdown" | "code";

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
        extensions: ["htm", "html", "json", "log", "py", "txt", "yaml", "yml"],
        id: "code",
        label: "Text & code",
    },
];

const SUBDIR_PREVIEW_PAGE_SIZE = 20;
const SUBDIR_PREVIEW_DEFAULT_HEIGHT = 200;
const compressedExtensions = new Set(["gz"]);
const allSubdirPreviewKinds = new Set<SubdirPreviewKind>(
    subdirPreviewKindGroups.map((group) => group.id),
);
const defaultSubdirPreviewKinds = new Set<SubdirPreviewKind>(["image"]);
const directFilePreviewExtensions = new Set([
    "csv",
    "htm",
    "html",
    "json",
    "log",
    "markdown",
    "md",
    "pdf",
    "py",
    "svg",
    "tsv",
    "txt",
    "xml",
    "yaml",
    "yml",
]);

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
    const extension = effectiveExtension(path);

    return (
        previewKindForPath(path) !== null ||
        directFilePreviewExtensions.has(extension)
    );
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
    label = "Preview height",
    onCommit,
    value,
}: {
    ariaLabel?: string;
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
        <label className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-foreground">
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
    const preferredSelectedPath = selectedPath ?? uncontrolledPath;
    const activeFile =
        activeFiles.find((file) => file.path === preferredSelectedPath) ??
        activeFiles[0];
    const effectiveSelectedPath = activeFile?.path;
    const displayedFiles = visibleFiles ?? activeFiles;
    const hasPreviewableActiveFiles = activeFiles.some((file) =>
        pathSupportsFilePreview(file.path),
    );
    const [subdirPreviewEnabled, setSubdirPreviewEnabled] = useState(false);
    const [subdirPreviewKinds, setSubdirPreviewKinds] = useState<
        Set<SubdirPreviewKind>
    >(() => new Set(defaultSubdirPreviewKinds));
    const [subdirPreviewHeight, setSubdirPreviewHeight] = useState(
        SUBDIR_PREVIEW_DEFAULT_HEIGHT,
    );
    const [subdirPreviewPages, setSubdirPreviewPages] = useState<
        Record<string, number>
    >({});

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

    const renderFileButton = (
        file: FileEntry,
        nested = false,
        embedded = false,
    ) => (
        <button
            type="button"
            className={cn(
                "flex w-full items-start gap-4 text-left transition",
                embedded
                    ? "rounded-[1rem] px-0 py-0"
                    : "rounded-[1.25rem] border px-4 py-4",
                nested ? "min-h-[5.5rem]" : "",
                embedded
                    ? file.path === activeFile?.path
                        ? "text-foreground"
                        : "text-foreground/90"
                    : file.path === activeFile?.path
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

    const renderPreviewControls = (
        directoryPath: string,
        placement: "folder" | "bottom",
    ) => {
        const showPreviewPaging = previewPageCount > 1;

        if (placement === "bottom" && !showPreviewPaging) {
            return null;
        }

        return (
            <div
                className={cn(
                    "flex flex-wrap items-center gap-2 text-sm",
                    placement === "bottom"
                        ? "col-span-full justify-end pt-1"
                        : "w-full justify-start px-3 pb-3",
                )}
                data-file-browser-bottom-controls={
                    placement === "bottom" ? directoryPath : undefined
                }
                data-file-browser-folder-controls={
                    placement === "folder" ? directoryPath : undefined
                }
            >
                {placement === "folder" ? (
                    <>
                        {displayedFiles.length > 1 ? (
                            <label className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-foreground">
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
                        ) : null}
                        <PreviewHeightControl
                            onCommit={onPreviewHeightChange}
                            value={previewHeight}
                        />
                    </>
                ) : null}

                {showPreviewPaging ? (
                    <div className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-2 py-1.5 text-muted-foreground">
                        <ListFilter
                            className="size-4 text-primary"
                            aria-hidden="true"
                        />
                        <span>
                            Page {previewPage} of {previewPageCount}
                        </span>
                    </div>
                ) : null}

                {showPreviewPaging ? (
                    <PreviewPagination
                        nextLabel="Next preview page"
                        onPageChange={(page) => onPreviewPageChange?.(page)}
                        page={previewPage}
                        pageCount={previewPageCount}
                        previousLabel="Previous preview page"
                        selectLabel="Preview page"
                    />
                ) : null}
            </div>
        );
    };

    const subdirPreviewStateFor = (node: DirectoryTreeNode) => {
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

    const renderSubdirPreviewControls = (
        directoryPath: string,
        pageCount: number,
        safePage: number,
    ) => (
        <div
            className="flex w-full flex-wrap items-center justify-start gap-2 px-3 pb-3 text-sm"
            data-file-browser-folder-controls={directoryPath}
            data-subdir-preview-controls={directoryPath}
        >
            <label className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-foreground">
                <input
                    aria-label="Subfolder previews"
                    checked={subdirPreviewEnabled}
                    className="size-4 accent-primary"
                    onChange={(event) => {
                        setSubdirPreviewEnabled(event.target.checked);
                        setSubdirPreviewPages({});
                    }}
                    type="checkbox"
                />
                <span className="inline-flex items-center gap-2">
                    <FolderTree
                        className="size-4 text-primary"
                        aria-hidden="true"
                    />
                    Subfolder previews
                </span>
            </label>
            <details
                className="relative"
                data-subdir-preview-kind-disclosure={directoryPath}
            >
                <summary className="inline-flex cursor-pointer list-none items-center gap-2 rounded-full border border-border/70 bg-background/75 px-3 py-2 text-foreground marker:hidden">
                    <ListFilter
                        className="size-4 text-primary"
                        aria-hidden="true"
                    />
                    <span className="font-medium">File types</span>
                    <span className="text-xs text-muted-foreground">
                        {summarizeSubdirPreviewKinds(subdirPreviewKinds)}
                    </span>
                    <ChevronDown className="size-4 text-muted-foreground" />
                </summary>
                <div
                    className="absolute right-0 z-20 mt-2 min-w-52 rounded-[1.25rem] border border-border/70 bg-background/95 p-3 shadow-lg"
                    data-subdir-preview-kinds={directoryPath}
                >
                    <div className="mb-2 text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">
                        File types
                    </div>
                    <div className="space-y-2">
                        {subdirPreviewKindGroups.map((group) => (
                            <label
                                key={group.id}
                                className="flex items-center justify-between gap-3 text-sm"
                            >
                                <span>{group.label}</span>
                                <input
                                    checked={subdirPreviewKinds.has(group.id)}
                                    className="size-3.5 accent-primary"
                                    data-subdir-preview-kind={group.id}
                                    onChange={(event) => {
                                        setSubdirPreviewKinds((current) => {
                                            const next = new Set(current);

                                            if (event.target.checked) {
                                                next.add(group.id);
                                            } else {
                                                next.delete(group.id);
                                            }

                                            return next;
                                        });
                                        setSubdirPreviewPages({});
                                    }}
                                    type="checkbox"
                                />
                            </label>
                        ))}
                    </div>
                </div>
            </details>
            <PreviewHeightControl
                ariaLabel="Subfolder preview height"
                onCommit={setSubdirPreviewHeight}
                value={subdirPreviewHeight}
            />
            {pageCount > 1 ? (
                <>
                    <div className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/75 px-2 py-1.5 text-muted-foreground">
                        <ListFilter
                            className="size-4 text-primary"
                            aria-hidden="true"
                        />
                        <span>
                            Page {safePage} of {pageCount}
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
                        page={safePage}
                        pageCount={pageCount}
                        previousLabel="Previous subfolder page"
                        selectLabel="Subfolder preview page"
                    />
                </>
            ) : null}
        </div>
    );

    const renderSubdirGalleryRow = (subdir: DirectoryTreeNode): ReactNode => {
        const previewableFiles = previewableFilesForKinds(
            subdir,
            subdirPreviewKinds,
        );

        return (
            <div
                key={`subdir-gallery-${subdir.path}`}
                className="flex w-full flex-col items-start gap-3 rounded-[1.25rem] border border-border/60 bg-background/60 p-3"
                data-subdir-preview-row={subdir.path}
            >
                <div
                    className="min-w-0"
                    data-subdir-preview-heading={subdir.path}
                >
                    <p className="truncate text-base font-medium text-foreground">
                        {subdir.label || directoryLabel(subdir.path)}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                        {previewableFiles.length} preview
                        {previewableFiles.length === 1 ? "" : "s"}
                    </p>
                </div>
                <div
                    className="flex w-full min-w-0 items-start gap-4 overflow-x-auto pb-1"
                    data-subdir-preview-strip={subdir.path}
                    style={
                        {
                            "--subdir-preview-height": `${subdirPreviewHeight}px`,
                        } as CSSProperties
                    }
                >
                    {previewableFiles.map((file) => (
                        <div
                            key={file.path}
                            className={cn(
                                "inline-flex max-w-full shrink-0 flex-col gap-2",
                                previewKindForPath(file.path) === "image"
                                    ? "w-full"
                                    : "w-fit",
                            )}
                            data-subdir-preview-card={file.path}
                            style={{
                                maxWidth: `calc(var(--subdir-preview-height) * 1.8)`,
                            }}
                        >
                            <p
                                className="truncate text-xs font-medium text-muted-foreground"
                                data-subdir-preview-filename={file.path}
                                title={fileName(file.path)}
                            >
                                {fileName(file.path)}
                            </p>
                            <div
                                className={cn(
                                    "inline-flex max-w-full items-start overflow-hidden rounded-[1.25rem] border border-border/60 bg-background/70 shadow-sm",
                                    previewKindForPath(file.path) === "image"
                                        ? "w-full justify-center"
                                        : "w-fit [&_button]:max-w-none [&_button]:justify-start [&_button]:w-auto [&_img]:max-w-none [&_img]:w-auto",
                                )}
                                style={{
                                    height: `var(--subdir-preview-height)`,
                                }}
                            >
                                {renderGridPreview?.(file) ?? null}
                            </div>
                        </div>
                    ))}
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
            const showSubdirGallery =
                hasSubdirPreviewControls && subdirPreviewEnabled;
            const folderControls =
                hasSubdirPreviewControls || hasFilePreviewControls ? (
                    <>
                        {hasSubdirPreviewControls
                            ? renderSubdirPreviewControls(
                                  node.path,
                                  subdirPreviewPageCount,
                                  safeSubdirPreviewPage,
                              )
                            : null}
                        {hasFilePreviewControls
                            ? renderPreviewControls(node.path, "folder")
                            : null}
                    </>
                ) : null;
            const hasPreviewControls = Boolean(folderControls);
            const showsDirectoryFiles =
                isStructurallyExpanded &&
                node.path === effectiveSelectedDirectory &&
                displayedFiles.length > 0;
            const showsChildRows =
                isStructurallyExpanded && hasChildren && !showSubdirGallery;
            const isExpanded =
                hasPreviewControls ||
                showSubdirGallery ||
                showsDirectoryFiles ||
                showsChildRows;
            const rows: ReactNode[] = [
                <div
                    key={`dir-${node.path}`}
                    className={cn(
                        "grid w-full grid-cols-1 gap-3 rounded-[1.25rem] border transition",
                        hasPreviewControls ? "p-2" : "grid-cols-1 p-0",
                        isSelected
                            ? "border-primary/45 bg-primary/10"
                            : "border-border/60 bg-background/60 hover:border-primary/35 hover:bg-background",
                    )}
                    data-directory-row={node.path}
                >
                    <button
                        type="button"
                        className="grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-[1rem] px-3 py-3 text-left transition hover:bg-background/55"
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
                                    for (const path of ancestorPaths(
                                        node.path,
                                    )) {
                                        if (path !== node.path) {
                                            next.add(path);
                                        }
                                    }
                                } else {
                                    for (const path of ancestorPaths(
                                        node.path,
                                    )) {
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
                        <span className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                            {node.descendantDirectoryCount > 0
                                ? `${node.descendantDirectoryCount} subfolder${node.descendantDirectoryCount === 1 ? "" : "s"}`
                                : "Folder"}
                        </span>
                    </button>
                    {folderControls}
                </div>,
            ];

            if (showSubdirGallery) {
                rows.push(
                    <div
                        key={`subdir-gallery-${node.path}`}
                        className="space-y-3"
                        data-subdir-preview-gallery={node.path}
                    >
                        {visibleSubdirs.map((subdir) =>
                            renderSubdirGalleryRow(subdir),
                        )}
                    </div>,
                );
            }

            if (
                isStructurallyExpanded &&
                node.path === effectiveSelectedDirectory &&
                displayedFiles.length > 0
            ) {
                rows.push(
                    <div
                        key={`files-${node.path}`}
                        className={cn(
                            previewMode === "single"
                                ? showFilePreviewWidgets
                                    ? "grid gap-3 grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] items-start"
                                    : "space-y-3"
                                : "space-y-3 xl:col-span-2",
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
                                      ...displayedFiles.map((file) =>
                                          cloneElement(
                                              renderFileButton(file, true),
                                              { key: file.path },
                                          ),
                                      ),
                                      <div
                                          key={`single-preview-${node.path}`}
                                          className="sticky top-4 z-10 min-w-0 col-start-2 row-start-1 self-start"
                                          data-file-browser-preview="single"
                                          style={{
                                              gridRow: `1 / span ${Math.max(displayedFiles.length, 1)}`,
                                          }}
                                      >
                                          {renderSinglePreview?.(
                                              activeFile ?? null,
                                          ) ?? null}
                                      </div>,
                                  ]
                                : displayedFiles.map((file) =>
                                      cloneElement(
                                          renderFileButton(file, true),
                                          { key: file.path },
                                      ),
                                  )
                            : displayedFiles.map((file) =>
                                  showFilePreviewWidgets ? (
                                      <div
                                          key={file.path}
                                          className="grid gap-3 grid-cols-[minmax(18rem,0.88fr)_minmax(0,1.12fr)] items-start"
                                          data-file-browser-grid-row={file.path}
                                      >
                                          <div className="min-w-0 border-r border-border/60 pr-3">
                                              {renderFileButton(
                                                  file,
                                                  true,
                                                  true,
                                              )}
                                          </div>
                                          <div
                                              className="min-w-0"
                                              data-grid-preview-path={file.path}
                                          >
                                              {renderGridPreview?.(file) ??
                                                  null}
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
                            ? renderPreviewControls(node.path, "bottom")
                            : null}
                    </div>,
                );
            }

            if (isStructurallyExpanded && hasChildren && !showSubdirGallery) {
                rows.push(
                    ...renderDirectoryRows(node.children, depth + 1, node.path),
                );
            }

            return rows;
        });
    }

    return (
        <section
            className="rounded-[1.75rem] border border-border/70 bg-card/85 p-4 shadow-[0_28px_90px_-72px_rgba(48,67,98,0.9)] sm:p-5"
            data-file-browser="true"
        >
            <div
                className="flex items-center gap-3 border-b border-border/60 pb-5"
                data-file-browser-header="true"
            >
                <FolderTree
                    className="size-4 text-primary"
                    aria-hidden="true"
                />
                <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                    File Browser
                </p>
            </div>

            {previewMode === "single" ? (
                <div
                    className="mt-5 rounded-[1.5rem] border border-border/70 bg-background/55 p-4"
                    data-preview-mode="single"
                >
                    <div className="space-y-3">
                        {renderDirectoryRows(directoryTree)}
                    </div>
                </div>
            ) : (
                <div
                    className="mt-5 rounded-[1.5rem] border border-border/70 bg-background/55 p-4"
                    data-preview-mode="grid"
                >
                    <div className="space-y-3">
                        {renderDirectoryRows(directoryTree)}
                    </div>
                </div>
            )}
        </section>
    );
}
