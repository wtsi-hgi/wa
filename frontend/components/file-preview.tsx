"use client";

import Image from "next/image";
import { memo, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { ArrowDownToLine, Expand, FileCode2, Search, X } from "lucide-react";
import hljs from "highlight.js/lib/core";
import json from "highlight.js/lib/languages/json";
import markdownLanguage from "highlight.js/lib/languages/markdown";
import plaintext from "highlight.js/lib/languages/plaintext";
import python from "highlight.js/lib/languages/python";
import xml from "highlight.js/lib/languages/xml";
import ReactMarkdown from "react-markdown";

import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import { PreviewPagination } from "@/components/preview-pagination";
import type { FileEntry } from "@/lib/contracts";
import {
    buildOmeTiffMetadataUrl,
    buildOmeTiffPlaneUrl,
    isTiffPreviewPath,
    type OmeTiffMetadata,
} from "@/lib/ome-tiff";
import {
    effectivePreviewExtension,
    nonPreviewableBinaryExtensions,
    previewBitmapImageExtensions,
} from "@/lib/preview-file-types";
import { cn, formatBytes } from "@/lib/utils";

hljs.registerLanguage("json", json);
hljs.registerLanguage("markdown", markdownLanguage);
hljs.registerLanguage("plaintext", plaintext);
hljs.registerLanguage("python", python);
hljs.registerLanguage("xml", xml);

const nonPreviewableExtensions: ReadonlySet<string> = new Set(
    nonPreviewableBinaryExtensions,
);
const STABLE_THUMBNAIL_HEIGHT = 420;
const STABLE_THUMBNAIL_WIDTH = Math.max(
    320,
    Math.round(STABLE_THUMBNAIL_HEIGHT * 1.6),
);
const EXPANDED_TABLE_PAGE_SIZE = 1000;
const INLINE_TABLE_HEADER_HEIGHT = 48;
const INLINE_TABLE_ROW_HEIGHT = 44;
const htmlPreviewSandbox = "allow-scripts";
const imageExtensions: ReadonlySet<string> = new Set(
    previewBitmapImageExtensions,
);

export type PreviewRenderer =
    | "image"
    | "csv"
    | "markdown"
    | "html"
    | "svg"
    | "pdf"
    | "code"
    | "binary";

export type FilePreviewError = {
    fileSize?: number;
    message?: string;
    status: number;
};

export type FilePreviewProps = {
    file: FileEntry;
    content?: { content: string; contentType: string; truncated?: boolean };
    enlargedContent?: {
        content: string;
        contentType: string;
        truncated?: boolean;
    };
    enlargedError?: FilePreviewError;
    enlargedLoading?: boolean;
    error?: FilePreviewError;
    isLoading?: boolean;
    maxHeight?: number;
    onEnlargeOpen?: () => void;
    proxyUrl: string;
};

type LightboxImageProps = {
    buttonClassName?: string;
    dialogCloseButtonClassName?: string;
    dialogContent?: ReactNode;
    dialogPanelClassName?: string;
    downloadUrl?: string;
    fileName: string;
    fullSizeUrl: string;
    imageClassName?: string;
    maxHeightPx?: number;
    minimumWidthPx?: number;
    sizes?: string;
    thumbnailHeight?: number;
    thumbnailUrl: string;
    thumbnailWidth?: number;
};

type ExpandablePreviewProps = {
    children: ReactNode;
    dialogContent: ReactNode;
    fileName: string;
    onOpen?: () => void;
};

export type FileImageThumbnailProps = {
    file: FileEntry;
    fullSizeUrl: string;
    height?: number;
    proxyUrl?: string;
    thumbnailUrl: string;
};

type ParsedTable = {
    headers: string[];
    rows: string[][];
};

type SortDirection = "asc" | "desc";

function hasVerticalOverflow(element: HTMLElement): boolean {
    return element.scrollHeight > element.clientHeight + 1;
}

function useInlinePreviewOverflow<T extends HTMLElement>(
    enabled: boolean,
    contentKey: string,
    maxHeight?: number,
) {
    const measureRef = useRef<T | null>(null);
    const [isOverflowing, setIsOverflowing] = useState(false);

    useEffect(() => {
        if (!enabled) {
            return undefined;
        }

        const node = measureRef.current;

        if (!node) {
            return undefined;
        }

        const updateOverflow = () => {
            const nextValue = hasVerticalOverflow(node);

            setIsOverflowing((currentValue) =>
                currentValue === nextValue ? currentValue : nextValue,
            );
        };
        const initialFrame = window.requestAnimationFrame(updateOverflow);

        window.addEventListener("resize", updateOverflow);

        if (typeof ResizeObserver === "undefined") {
            return () => {
                window.cancelAnimationFrame(initialFrame);
                window.removeEventListener("resize", updateOverflow);
            };
        }

        const resizeObserver = new ResizeObserver(() => {
            updateOverflow();
        });

        resizeObserver.observe(node);

        return () => {
            window.cancelAnimationFrame(initialFrame);
            resizeObserver.disconnect();
            window.removeEventListener("resize", updateOverflow);
        };
    }, [contentKey, enabled, maxHeight]);

    return [measureRef, isOverflowing] as const;
}

function normalizeContentType(contentType: string): string {
    return (
        contentType.split(";")[0]?.trim().toLowerCase() ??
        "application/octet-stream"
    );
}

function guessRendererFromPath(path: string): PreviewRenderer {
    const extension = effectivePreviewExtension(path);

    if (extension === "svg") {
        return "svg";
    }

    if (extension === "htm" || extension === "html") {
        return "html";
    }

    if (extension === "json") {
        return "code";
    }

    if (imageExtensions.has(extension)) {
        return "image";
    }

    if (extension === "pdf") {
        return "pdf";
    }

    return "binary";
}

function isPreviewable(renderer: PreviewRenderer, path: string): boolean {
    return (
        renderer !== "binary" &&
        !nonPreviewableExtensions.has(effectivePreviewExtension(path))
    );
}

function buildDownloadUrl(proxyUrl: string): string {
    if (proxyUrl.includes("download=true")) {
        return proxyUrl;
    }

    return `${proxyUrl}${proxyUrl.includes("?") ? "&" : "?"}download=true`;
}

function buildPreviewInstanceKey(
    path: string,
    contentType: string,
    content: string,
    mode: "inline" | "expanded",
): string {
    return [path, contentType, content, mode].join("\u0000");
}

function parseDelimitedContent(
    content: string,
    contentType: string,
): ParsedTable {
    const delimiter = normalizeContentType(contentType).startsWith("text/tab-")
        ? "\t"
        : ",";
    const lines = content
        .split(/\r?\n/)
        .map((line) => line.trimEnd())
        .filter((line) => line.length > 0);

    if (lines.length === 0) {
        return { headers: [], rows: [] };
    }

    const [headerLine, ...rowLines] = lines;

    return {
        headers: headerLine.split(delimiter).map((cell) => cell.trim()),
        rows: rowLines.map((line) =>
            line.split(delimiter).map((cell) => cell.trim()),
        ),
    };
}

function inferHighlightLanguage(contentType: string): string | undefined {
    const normalized = normalizeContentType(contentType);

    if (normalized === "application/json") {
        return "json";
    }

    if (normalized === "text/markdown") {
        return "markdown";
    }

    if (normalized === "text/html") {
        return "xml";
    }

    if (normalized === "text/x-python") {
        return "python";
    }

    return undefined;
}

function estimateInlineTableHeight(parsed: ParsedTable): number {
    const headerHeight =
        parsed.headers.length > 0 ? INLINE_TABLE_HEADER_HEIGHT : 0;

    return headerHeight + parsed.rows.length * INLINE_TABLE_ROW_HEIGHT;
}

function highlightCode(content: string, contentType: string): string {
    const language = inferHighlightLanguage(contentType);

    if (language) {
        return hljs.highlight(content, { ignoreIllegals: true, language })
            .value;
    }

    return hljs.highlightAuto(content).value;
}

function DownloadIconLink({
    className,
    href,
}: {
    className?: string;
    href: string;
}) {
    return (
        <a
            aria-label="Download file"
            className={cn(
                "inline-flex size-9 items-center justify-center rounded-full border border-border/70 bg-background/80 text-foreground shadow-[0_8px_24px_-18px_rgba(48,67,98,0.85)] transition hover:border-primary/35 hover:bg-accent/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40",
                className,
            )}
            data-preview-download-overlay="true"
            href={href}
        >
            <ArrowDownToLine className="size-4" aria-hidden="true" />
        </a>
    );
}

function TruncationFade({ className }: { className?: string }) {
    return (
        <div
            aria-label="Content truncated"
            className={cn(
                "pointer-events-none absolute inset-x-0 bottom-0 h-16",
                className,
            )}
            data-truncated="true"
        />
    );
}

function ExpandablePreview({
    children,
    dialogContent,
    fileName,
    onOpen,
}: ExpandablePreviewProps) {
    const [previewOpen, setPreviewOpen] = useState(false);
    const onOpenRef = useRef(onOpen);

    useEffect(() => {
        onOpenRef.current = onOpen;
    }, [onOpen]);

    useEffect(() => {
        if (!previewOpen) {
            return undefined;
        }

        onOpenRef.current?.();

        function handleKeyDown(event: KeyboardEvent) {
            if (event.key === "Escape") {
                setPreviewOpen(false);
            }
        }

        window.addEventListener("keydown", handleKeyDown);

        return () => {
            window.removeEventListener("keydown", handleKeyDown);
        };
    }, [previewOpen]);

    return (
        <>
            <div className="group relative h-full w-full min-h-0 cursor-zoom-in">
                <div className="h-full w-full">{children}</div>
                <button
                    type="button"
                    aria-label={`Enlarge ${fileName} preview`}
                    className="absolute inset-0 z-10 flex cursor-zoom-in items-end justify-start rounded-[1.5rem] text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/45"
                    onClick={() => setPreviewOpen(true)}
                >
                    <span className="pointer-events-none m-3 inline-flex size-9 items-center justify-center rounded-full bg-[color:rgba(15,23,42,0.78)] text-white opacity-95 shadow-lg transition group-hover:bg-[color:rgba(15,23,42,0.88)]">
                        <Expand className="size-3.5" aria-hidden="true" />
                    </span>
                </button>
            </div>

            {previewOpen ? (
                <div
                    aria-label={`Enlarged ${fileName} preview`}
                    aria-modal="true"
                    className="fixed inset-0 z-50 flex items-center justify-center p-6"
                    role="dialog"
                >
                    <button
                        type="button"
                        aria-label="Close enlarged preview backdrop"
                        className="absolute inset-0 bg-[color:rgba(17,24,39,0.75)] backdrop-blur-sm"
                        onClick={() => setPreviewOpen(false)}
                    />
                    <div className="relative z-10 flex max-h-full w-full max-w-6xl flex-col rounded-[2rem] border border-white/15 bg-background p-5 text-foreground shadow-2xl">
                        <button
                            type="button"
                            aria-label="Close enlarged preview"
                            className="absolute right-4 top-4 z-10 inline-flex size-10 items-center justify-center rounded-full border border-border/70 bg-background/90 text-foreground transition hover:bg-muted"
                            onClick={() => setPreviewOpen(false)}
                        >
                            <X className="size-4" aria-hidden="true" />
                        </button>
                        <div className="max-h-[calc(100vh-8rem)] overflow-auto pr-1">
                            {dialogContent}
                        </div>
                    </div>
                </div>
            ) : null}
        </>
    );
}

function CsvPreview({
    content,
    contentType,
    isExpanded = false,
    maxHeight,
    truncated = false,
}: {
    content: string;
    contentType: string;
    isExpanded?: boolean;
    maxHeight?: number;
    truncated?: boolean;
}) {
    const parsed = useMemo(
        () => parseDelimitedContent(content, contentType),
        [content, contentType],
    );
    const [filterValue, setFilterValue] = useState("");
    const [currentPage, setCurrentPage] = useState(1);
    const [expandedTableReady, setExpandedTableReady] = useState(
        !isExpanded || parsed.rows.length <= EXPANDED_TABLE_PAGE_SIZE,
    );
    const [sortIndex, setSortIndex] = useState<number | null>(null);
    const [sortDirection, setSortDirection] = useState<SortDirection>("asc");

    const filteredRows = useMemo(() => {
        const normalizedFilter = filterValue.trim().toLowerCase();

        if (!normalizedFilter) {
            return parsed.rows;
        }

        return parsed.rows.filter((row) =>
            row.some((cell) => cell.toLowerCase().includes(normalizedFilter)),
        );
    }, [filterValue, parsed.rows]);

    const sortedRows = useMemo(() => {
        if (sortIndex === null) {
            return filteredRows;
        }

        return [...filteredRows].sort((left, right) => {
            const leftValue = left[sortIndex] ?? "";
            const rightValue = right[sortIndex] ?? "";
            const numericLeft = Number(leftValue);
            const numericRight = Number(rightValue);
            const bothNumeric =
                !Number.isNaN(numericLeft) && !Number.isNaN(numericRight);
            const order = bothNumeric
                ? numericLeft - numericRight
                : leftValue.localeCompare(rightValue, undefined, {
                      numeric: true,
                      sensitivity: "base",
                  });

            return sortDirection === "asc" ? order : -order;
        });
    }, [filteredRows, sortDirection, sortIndex]);

    useEffect(() => {
        if (expandedTableReady) {
            return undefined;
        }

        const timeoutId = window.setTimeout(() => {
            setExpandedTableReady(true);
        }, 0);

        return () => {
            window.clearTimeout(timeoutId);
        };
    }, [expandedTableReady]);

    const totalExpandedPages = isExpanded
        ? Math.max(1, Math.ceil(sortedRows.length / EXPANDED_TABLE_PAGE_SIZE))
        : 1;
    const safeCurrentPage = Math.min(currentPage, totalExpandedPages);
    const expandedPageStartIndex =
        (safeCurrentPage - 1) * EXPANDED_TABLE_PAGE_SIZE;

    const visibleRows = isExpanded
        ? totalExpandedPages > 1
            ? sortedRows.slice(
                  expandedPageStartIndex,
                  expandedPageStartIndex + EXPANDED_TABLE_PAGE_SIZE,
              )
            : sortedRows
        : sortedRows;

    const expandedPageEndIndex = expandedPageStartIndex + visibleRows.length;

    if (isExpanded && !expandedTableReady) {
        return (
            <div className="rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 px-5 py-8 text-sm text-muted-foreground">
                Loading full preview...
            </div>
        );
    }

    return (
        <div className="space-y-4">
            {isExpanded ? (
                <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                    <p className="text-sm text-muted-foreground">
                        {totalExpandedPages > 1
                            ? `Showing rows ${expandedPageStartIndex + 1}-${expandedPageEndIndex} of ${sortedRows.length}`
                            : `Showing ${visibleRows.length} of ${parsed.rows.length} rows`}
                    </p>
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                        <label className="relative block">
                            <span className="sr-only">Filter rows</span>
                            <Search
                                className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
                                aria-hidden="true"
                            />
                            <input
                                aria-label="Filter rows"
                                className="w-full rounded-full border border-border/70 bg-background px-10 py-2 text-sm text-foreground outline-none transition focus:border-primary sm:w-64"
                                onChange={(event) => {
                                    setCurrentPage(1);
                                    setFilterValue(event.target.value);
                                }}
                                placeholder="Filter rows"
                                value={filterValue}
                            />
                        </label>
                        {totalExpandedPages > 1 ? (
                            <div className="flex items-center gap-2 self-start sm:self-auto">
                                <span className="text-sm text-muted-foreground">
                                    Page {safeCurrentPage} of{" "}
                                    {totalExpandedPages}
                                </span>
                                <PreviewPagination
                                    onPageChange={(page) =>
                                        setCurrentPage(page)
                                    }
                                    page={safeCurrentPage}
                                    pageCount={totalExpandedPages}
                                />
                            </div>
                        ) : null}
                    </div>
                </div>
            ) : null}

            <div className="overflow-auto">
                <Table>
                    <TableHeader>
                        <TableRow>
                            {parsed.headers.map((header, index) => (
                                <TableHead key={`${header}-${index}`}>
                                    {isExpanded ? (
                                        <button
                                            type="button"
                                            aria-label={`Sort by ${header}`}
                                            className="inline-flex items-center gap-2 rounded-full px-2 py-1 text-left text-sm font-medium text-foreground transition hover:bg-muted/50"
                                            onClick={() => {
                                                if (sortIndex === index) {
                                                    setCurrentPage(1);
                                                    setSortDirection(
                                                        (current) =>
                                                            current === "asc"
                                                                ? "desc"
                                                                : "asc",
                                                    );
                                                    return;
                                                }

                                                setCurrentPage(1);
                                                setSortIndex(index);
                                                setSortDirection("asc");
                                            }}
                                        >
                                            <span>{header}</span>
                                            {sortIndex === index ? (
                                                <span className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                                                    {sortDirection}
                                                </span>
                                            ) : null}
                                        </button>
                                    ) : (
                                        <span className="px-2 py-1 text-sm font-medium text-foreground">
                                            {header}
                                        </span>
                                    )}
                                </TableHead>
                            ))}
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {visibleRows.map((row, rowIndex) => (
                            <TableRow
                                key={`${expandedPageStartIndex + rowIndex}`}
                            >
                                {parsed.headers.map((header, columnIndex) => (
                                    <TableCell
                                        key={`${header}-${rowIndex}-${columnIndex}`}
                                    >
                                        {row[columnIndex] ?? ""}
                                    </TableCell>
                                ))}
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        </div>
    );
}

function LightboxImage({
    buttonClassName,
    dialogCloseButtonClassName,
    dialogContent,
    dialogPanelClassName,
    downloadUrl,
    fileName,
    fullSizeUrl,
    imageClassName,
    maxHeightPx = 240,
    minimumWidthPx,
    sizes = "320px",
    thumbnailHeight = 240,
    thumbnailUrl,
    thumbnailWidth = 320,
}: LightboxImageProps) {
    const [lightboxOpen, setLightboxOpen] = useState(false);
    const defaultDialogPanelClassName =
        "relative z-10 max-h-full max-w-5xl rounded-[2rem] border border-white/15 bg-[color:rgba(15,23,42,0.9)] p-4 shadow-2xl";
    const defaultDialogCloseButtonClassName =
        "absolute right-4 top-4 inline-flex size-10 items-center justify-center rounded-full border border-white/10 bg-white/5 text-white transition hover:bg-white/10";

    useEffect(() => {
        if (!lightboxOpen) {
            return undefined;
        }

        function handleKeyDown(event: KeyboardEvent) {
            if (event.key === "Escape") {
                setLightboxOpen(false);
            }
        }

        window.addEventListener("keydown", handleKeyDown);

        return () => {
            window.removeEventListener("keydown", handleKeyDown);
        };
    }, [lightboxOpen]);

    return (
        <>
            <div
                className={
                    buttonClassName ??
                    "group relative inline-flex cursor-zoom-in overflow-hidden rounded-[1.5rem]"
                }
            >
                <button
                    type="button"
                    aria-label="Open image lightbox"
                    className="absolute inset-0 z-10 cursor-zoom-in rounded-[inherit] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/45"
                    onClick={() => setLightboxOpen(true)}
                />
                <div className="relative overflow-hidden rounded-[inherit]">
                    <Image
                        alt={`${fileName} preview`}
                        className={cn(
                            "rounded-[inherit] object-contain transition duration-200 group-hover:scale-[1.01]",
                            imageClassName,
                        )}
                        decoding="async"
                        loading="lazy"
                        src={thumbnailUrl}
                        unoptimized
                        width={thumbnailWidth}
                        height={thumbnailHeight}
                        sizes={sizes}
                        style={{
                            height: `${maxHeightPx}px`,
                            maxHeight: `${maxHeightPx}px`,
                            maxWidth: `${thumbnailWidth}px`,
                            minWidth:
                                minimumWidthPx !== undefined
                                    ? `${minimumWidthPx}px`
                                    : undefined,
                            width: "auto",
                        }}
                    />
                    {downloadUrl ? (
                        <DownloadIconLink
                            className="absolute right-0 top-0 z-20 m-3"
                            href={downloadUrl}
                        />
                    ) : null}
                    <span
                        className="pointer-events-none absolute bottom-0 left-0 m-3 inline-flex size-9 items-center justify-center rounded-full bg-[color:rgba(15,23,42,0.78)] text-white shadow-lg"
                        data-preview-enlarge-badge="true"
                    >
                        <Expand className="size-3.5" aria-hidden="true" />
                    </span>
                </div>
            </div>

            {lightboxOpen ? (
                <div
                    aria-label="Image preview lightbox"
                    aria-modal="true"
                    className="fixed inset-0 z-50 flex items-center justify-center p-6"
                    role="dialog"
                >
                    <button
                        type="button"
                        aria-label="Close image preview backdrop"
                        className="absolute inset-0 bg-[color:rgba(17,24,39,0.75)] backdrop-blur-sm"
                        onClick={() => setLightboxOpen(false)}
                    />
                    <div
                        className={
                            dialogPanelClassName ?? defaultDialogPanelClassName
                        }
                    >
                        <button
                            type="button"
                            aria-label="Close image preview"
                            className={
                                dialogCloseButtonClassName ??
                                defaultDialogCloseButtonClassName
                            }
                            onClick={() => setLightboxOpen(false)}
                        >
                            <X className="size-4" aria-hidden="true" />
                        </button>
                        {dialogContent ?? (
                            <Image
                                alt={`${fileName} full preview`}
                                className="max-h-[80vh] max-w-full rounded-[1.5rem] object-contain"
                                src={fullSizeUrl}
                                unoptimized
                                width={1600}
                                height={1200}
                                sizes="100vw"
                            />
                        )}
                    </div>
                </div>
            ) : null}
        </>
    );
}

function ImagePreview({
    fileName,
    maxHeightPx,
    proxyUrl,
}: {
    fileName: string;
    maxHeightPx?: number;
    proxyUrl: string;
}) {
    return (
        <LightboxImage
            downloadUrl={buildDownloadUrl(proxyUrl)}
            fileName={fileName}
            fullSizeUrl={proxyUrl}
            maxHeightPx={maxHeightPx}
            thumbnailUrl={proxyUrl}
        />
    );
}

function clampPreviewCoordinate(value: number, size: number): number {
    if (size <= 0) {
        return 0;
    }

    return Math.min(size - 1, Math.max(0, Math.round(value)));
}

function metadataSummary(metadata: OmeTiffMetadata): string[] {
    const summary = [
        `${metadata.width} x ${metadata.height}`,
        metadata.hasOmeMetadata
            ? `${metadata.channelCount} channels`
            : `${metadata.pageCount} planes`,
    ];

    if (metadata.sizeZ > 1) {
        summary.push(`${metadata.sizeZ} Z slices`);
    }

    if (metadata.sizeT > 1) {
        summary.push(`${metadata.sizeT} timepoints`);
    }

    return summary;
}

function OmeTiffPreview({
    fileName,
    maxHeightPx,
    proxyUrl,
}: {
    fileName: string;
    maxHeightPx?: number;
    proxyUrl: string;
}) {
    const thumbnailUrl = buildOmeTiffPlaneUrl(proxyUrl, {
        channel: 0,
        height: STABLE_THUMBNAIL_HEIGHT,
        t: 0,
        width: STABLE_THUMBNAIL_WIDTH,
        z: 0,
    });
    const fullPlaneUrl = buildOmeTiffPlaneUrl(proxyUrl, {
        channel: 0,
        height: 1200,
        t: 0,
        width: 1600,
        z: 0,
    });
    const renderedHeight = maxHeightPx ?? 240;

    return (
        <LightboxImage
            buttonClassName="group relative flex h-full w-full max-w-full cursor-zoom-in justify-center overflow-hidden rounded-[1.5rem]"
            dialogCloseButtonClassName="absolute right-4 top-4 z-10 inline-flex size-10 items-center justify-center rounded-full border border-border/70 bg-background/90 text-foreground transition hover:bg-muted"
            dialogContent={
                <OmeTiffExpandedPreview
                    fileName={fileName}
                    proxyUrl={proxyUrl}
                />
            }
            dialogPanelClassName="relative z-10 flex max-h-full w-full max-w-6xl flex-col rounded-[2rem] border border-white/15 bg-background p-5 text-foreground shadow-2xl"
            downloadUrl={buildDownloadUrl(proxyUrl)}
            fileName={fileName}
            fullSizeUrl={fullPlaneUrl}
            imageClassName="mx-auto block"
            maxHeightPx={renderedHeight}
            sizes="(min-width: 1280px) 32vw, 92vw"
            thumbnailHeight={STABLE_THUMBNAIL_HEIGHT}
            thumbnailUrl={thumbnailUrl}
            thumbnailWidth={STABLE_THUMBNAIL_WIDTH}
        />
    );
}

function OmeTiffExpandedPreview({
    fileName,
    proxyUrl,
}: {
    fileName: string;
    proxyUrl: string;
}) {
    const [metadataState, setMetadataState] = useState<{
        error: string | null;
        metadata: OmeTiffMetadata | null;
        proxyUrl: string;
    }>({
        error: null,
        metadata: null,
        proxyUrl: "",
    });
    const [channel, setChannel] = useState(0);
    const [z, setZ] = useState(0);
    const [t, setT] = useState(0);

    useEffect(() => {
        let cancelled = false;

        void fetch(buildOmeTiffMetadataUrl(proxyUrl))
            .then(async (response) => {
                if (!response.ok) {
                    throw new Error("Unable to load TIFF metadata");
                }

                return (await response.json()) as OmeTiffMetadata;
            })
            .then((nextMetadata) => {
                if (cancelled) {
                    return;
                }

                setMetadataState({
                    error: null,
                    metadata: nextMetadata,
                    proxyUrl,
                });
            })
            .catch((metadataError: unknown) => {
                if (cancelled) {
                    return;
                }

                setMetadataState({
                    error:
                        metadataError instanceof Error
                            ? metadataError.message
                            : "Unable to load TIFF metadata",
                    metadata: null,
                    proxyUrl,
                });
            });

        return () => {
            cancelled = true;
        };
    }, [proxyUrl]);

    const metadata =
        metadataState.proxyUrl === proxyUrl ? metadataState.metadata : null;
    const error =
        metadataState.proxyUrl === proxyUrl ? metadataState.error : null;
    const downloadUrl = buildDownloadUrl(proxyUrl);

    if (error) {
        return (
            <div className="relative min-h-64 w-full rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 p-6">
                <DownloadIconLink
                    className="absolute right-4 top-4"
                    href={downloadUrl}
                />
                <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                    Preview unavailable
                </p>
                <h3 className="mt-3 text-xl font-semibold tracking-tight text-foreground">
                    Unable to read TIFF metadata
                </h3>
                <p className="mt-3 text-sm leading-7 text-muted-foreground">
                    {error}
                </p>
            </div>
        );
    }

    if (!metadata) {
        return (
            <div className="relative flex min-h-64 w-full items-center justify-center rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 px-5 py-8 text-center text-sm text-muted-foreground">
                <DownloadIconLink
                    className="absolute right-4 top-4"
                    href={downloadUrl}
                />
                Loading TIFF metadata...
            </div>
        );
    }

    const safeChannel = clampPreviewCoordinate(channel, metadata.channelCount);
    const safeZ = clampPreviewCoordinate(z, metadata.sizeZ);
    const safeT = clampPreviewCoordinate(t, metadata.sizeT);
    const planeUrl = buildOmeTiffPlaneUrl(proxyUrl, {
        channel: safeChannel,
        height: 1200,
        t: safeT,
        width: 1600,
        z: safeZ,
    });
    const showChannelControl = metadata.channelCount > 1;
    const showZControl = metadata.sizeZ > 1;
    const showTControl = metadata.sizeT > 1;

    return (
        <div className="flex max-h-[calc(100vh-8rem)] w-full max-w-full flex-col gap-4 pr-12">
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                {metadataSummary(metadata).map((item) => (
                    <span
                        className="rounded-full border border-border/70 bg-background/75 px-2.5 py-1"
                        key={item}
                    >
                        {item}
                    </span>
                ))}
            </div>

            {showChannelControl || showZControl || showTControl ? (
                <div className="flex flex-wrap items-end gap-3">
                    {showChannelControl ? (
                        <label className="grid min-w-44 gap-1 text-xs font-medium text-muted-foreground">
                            <span>
                                {metadata.hasOmeMetadata ? "Channel" : "Plane"}
                            </span>
                            <select
                                aria-label={
                                    metadata.hasOmeMetadata
                                        ? "Channel"
                                        : "Plane"
                                }
                                className="h-9 rounded-md border border-border/80 bg-background px-2 text-sm text-foreground outline-none transition focus:border-primary"
                                onChange={(event) =>
                                    setChannel(Number(event.target.value))
                                }
                                value={safeChannel}
                            >
                                {metadata.channels.map((metadataChannel) => (
                                    <option
                                        key={metadataChannel.index}
                                        value={metadataChannel.index}
                                    >
                                        {metadataChannel.name}
                                    </option>
                                ))}
                            </select>
                        </label>
                    ) : null}

                    {showZControl ? (
                        <label className="grid min-w-44 gap-1 text-xs font-medium text-muted-foreground">
                            <span>
                                Z {safeZ + 1} of {metadata.sizeZ}
                            </span>
                            <input
                                aria-label="Z slice"
                                className="accent-primary"
                                max={metadata.sizeZ - 1}
                                min={0}
                                onChange={(event) =>
                                    setZ(Number(event.target.value))
                                }
                                type="range"
                                value={safeZ}
                            />
                        </label>
                    ) : null}

                    {showTControl ? (
                        <label className="grid min-w-44 gap-1 text-xs font-medium text-muted-foreground">
                            <span>
                                T {safeT + 1} of {metadata.sizeT}
                            </span>
                            <input
                                aria-label="Timepoint"
                                className="accent-primary"
                                max={metadata.sizeT - 1}
                                min={0}
                                onChange={(event) =>
                                    setT(Number(event.target.value))
                                }
                                type="range"
                                value={safeT}
                            />
                        </label>
                    ) : null}
                </div>
            ) : null}

            <div className="min-h-0 flex-1 overflow-auto rounded-[1.5rem] bg-muted/30">
                <Image
                    alt={`${fileName} full preview`}
                    className="mx-auto block max-h-[calc(100vh-16rem)] max-w-full object-contain"
                    height={1200}
                    sizes="100vw"
                    src={planeUrl}
                    unoptimized
                    width={1600}
                />
            </div>
        </div>
    );
}

export const FileImageThumbnail = memo(
    function FileImageThumbnail({
        file,
        fullSizeUrl,
        height = 220,
        proxyUrl,
        thumbnailUrl,
    }: FileImageThumbnailProps) {
        const fileName = file.path.split("/").pop() ?? file.path;
        const isTiffThumbnail = isTiffPreviewPath(file.path);
        const sourceUrl = proxyUrl ?? fullSizeUrl;

        return (
            <LightboxImage
                buttonClassName="group relative inline-flex max-w-full cursor-zoom-in justify-center overflow-hidden rounded-[1.25rem]"
                dialogCloseButtonClassName={
                    isTiffThumbnail
                        ? "absolute right-4 top-4 z-10 inline-flex size-10 items-center justify-center rounded-full border border-border/70 bg-background/90 text-foreground transition hover:bg-muted"
                        : undefined
                }
                dialogContent={
                    isTiffThumbnail ? (
                        <OmeTiffExpandedPreview
                            fileName={fileName}
                            proxyUrl={sourceUrl}
                        />
                    ) : undefined
                }
                dialogPanelClassName={
                    isTiffThumbnail
                        ? "relative z-10 flex max-h-full w-full max-w-6xl flex-col rounded-[2rem] border border-white/15 bg-background p-5 text-foreground shadow-2xl"
                        : undefined
                }
                downloadUrl={buildDownloadUrl(sourceUrl)}
                fileName={fileName}
                fullSizeUrl={fullSizeUrl}
                maxHeightPx={height}
                minimumWidthPx={160}
                sizes="(min-width: 1536px) 26vw, (min-width: 1280px) 30vw, 92vw"
                thumbnailHeight={STABLE_THUMBNAIL_HEIGHT}
                thumbnailUrl={thumbnailUrl}
                thumbnailWidth={STABLE_THUMBNAIL_WIDTH}
            />
        );
    },
    (prevProps, nextProps) => {
        // Only re-render if file path, URLs, or height changed
        // Memo prevents unnecessary rerenders from parent updates
        return (
            prevProps.file.path === nextProps.file.path &&
            prevProps.fullSizeUrl === nextProps.fullSizeUrl &&
            prevProps.proxyUrl === nextProps.proxyUrl &&
            prevProps.thumbnailUrl === nextProps.thumbnailUrl &&
            prevProps.height === nextProps.height
        );
    },
);

export function selectRenderer(contentType: string): PreviewRenderer {
    const normalized = normalizeContentType(contentType);

    if (normalized === "image/svg+xml") {
        return "svg";
    }

    if (normalized.startsWith("image/")) {
        return "image";
    }

    if (normalized === "text/csv" || normalized.startsWith("text/tab-")) {
        return "csv";
    }

    if (normalized === "text/markdown") {
        return "markdown";
    }

    if (normalized === "text/html") {
        return "html";
    }

    if (normalized === "application/pdf") {
        return "pdf";
    }

    if (normalized === "application/octet-stream") {
        return "binary";
    }

    if (normalized === "application/json" || normalized.startsWith("text/")) {
        return "code";
    }

    return "binary";
}

export function FilePreview({
    file,
    content,
    enlargedContent,
    enlargedError,
    enlargedLoading = false,
    error,
    isLoading = false,
    maxHeight,
    onEnlargeOpen,
    proxyUrl,
}: FilePreviewProps) {
    const renderer = content
        ? selectRenderer(content.contentType)
        : guessRendererFromPath(file.path);
    const downloadUrl = buildDownloadUrl(proxyUrl);
    const fileName = file.path.split("/").pop() ?? file.path;
    const previewable = isPreviewable(renderer, file.path);
    const previewContent = content?.content;
    const previewContentType = content?.contentType;
    const hasPreviewContent = Boolean(content);
    const highlightedContent = useMemo(() => {
        if (renderer !== "code" || isLoading || !hasPreviewContent) {
            return undefined;
        }

        return highlightCode(
            previewContent ?? "",
            previewContentType ?? "text/plain",
        );
    }, [
        hasPreviewContent,
        isLoading,
        previewContent,
        previewContentType,
        renderer,
    ]);

    const dialogContent = enlargedContent ?? content;
    const inlineCsvParsed = useMemo(() => {
        if (renderer !== "csv" || !content) {
            return undefined;
        }

        return parseDelimitedContent(content.content, content.contentType);
    }, [content, renderer]);
    const dialogHighlightedContent = useMemo(() => {
        if (renderer !== "code") {
            return undefined;
        }

        if (!dialogContent) {
            return undefined;
        }

        if (dialogContent === content) {
            return highlightedContent;
        }

        return highlightCode(
            dialogContent?.content ?? "",
            dialogContent?.contentType ?? "text/plain",
        );
    }, [content, dialogContent, highlightedContent, renderer]);
    const enlargedLoadingNode = (
        <div className="rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 px-5 py-8 text-sm text-muted-foreground">
            Loading full preview...
        </div>
    );
    const enlargedErrorMessage = enlargedError?.message?.trim();
    const enlargedErrorNode = enlargedError ? (
        <div className="rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 px-5 py-8 text-sm text-muted-foreground">
            {enlargedErrorMessage && enlargedErrorMessage.length > 0
                ? enlargedErrorMessage
                : "Unable to load full preview"}
        </div>
    ) : null;
    const isImagePreview = !isLoading && previewable && renderer === "image";
    const isTiffPreview =
        !isLoading &&
        previewable &&
        renderer === "image" &&
        isTiffPreviewPath(file.path);
    const [markdownMeasureRef, markdownIsOverflowing] =
        useInlinePreviewOverflow<HTMLElement>(
            !isLoading && previewable && renderer === "markdown",
            content?.content ?? "",
            maxHeight,
        );
    const [csvMeasureRef, csvIsOverflowing] =
        useInlinePreviewOverflow<HTMLDivElement>(
            !isLoading && previewable && renderer === "csv",
            content?.content ?? "",
            maxHeight,
        );
    const [codeMeasureRef, codeIsOverflowing] =
        useInlinePreviewOverflow<HTMLPreElement>(
            !isLoading && previewable && renderer === "code",
            highlightedContent ?? content?.content ?? "",
            maxHeight,
        );
    const markdownInlineTruncated =
        renderer === "markdown" &&
        (Boolean(content?.truncated) || markdownIsOverflowing);
    const csvInlineTruncated =
        renderer === "csv" &&
        (Boolean(content?.truncated) ||
            csvIsOverflowing ||
            (maxHeight !== undefined &&
                inlineCsvParsed !== undefined &&
                estimateInlineTableHeight(inlineCsvParsed) > maxHeight));
    const codeInlineTruncated =
        renderer === "code" &&
        (Boolean(content?.truncated) || codeIsOverflowing);

    if (error?.status === 413) {
        return (
            <section className="h-full w-full">
                <div className="relative h-full w-full rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 p-6">
                    <DownloadIconLink
                        className="absolute right-4 top-4"
                        href={downloadUrl}
                    />
                    <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                        Preview unavailable
                    </p>
                    <h3 className="mt-3 text-xl font-semibold tracking-tight text-foreground">
                        File too large for preview
                    </h3>
                    <p className="mt-3 text-sm leading-7 text-muted-foreground">
                        This file exceeds the preview limit. Reported size:{" "}
                        {formatBytes(error.fileSize)}.
                    </p>
                </div>
            </section>
        );
    }

    if (error) {
        return (
            <section className="h-full w-full">
                <div className="relative h-full w-full rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 p-6">
                    <DownloadIconLink
                        className="absolute right-4 top-4"
                        href={downloadUrl}
                    />
                    <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                        Preview unavailable
                    </p>
                    <h3 className="mt-3 text-xl font-semibold tracking-tight text-foreground">
                        Unable to load preview
                    </h3>
                    <p className="mt-3 text-sm leading-7 text-muted-foreground">
                        {error.message?.trim() || "Preview request failed"}
                    </p>
                </div>
            </section>
        );
    }

    if (isTiffPreview) {
        return (
            <section className="h-full max-w-full">
                <OmeTiffPreview
                    fileName={fileName}
                    key={proxyUrl}
                    maxHeightPx={maxHeight}
                    proxyUrl={proxyUrl}
                />
            </section>
        );
    }

    if (isImagePreview) {
        return (
            <section className="inline-flex h-full max-w-full">
                <ImagePreview
                    fileName={fileName}
                    maxHeightPx={maxHeight}
                    proxyUrl={proxyUrl}
                />
            </section>
        );
    }

    return (
        <section className="h-full w-full">
            <div className="relative flex h-full w-full flex-col overflow-hidden rounded-[1.75rem] border border-border/70 bg-[linear-gradient(160deg,color-mix(in_oklab,var(--background)_92%,white_8%),color-mix(in_oklab,var(--accent)_10%,var(--background)_90%))] shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]">
                <DownloadIconLink
                    className="absolute right-3 top-3 z-20"
                    href={downloadUrl}
                />

                <div className="min-h-0 flex-1 overflow-hidden">
                    {isLoading ? (
                        <div className="flex h-full w-full box-border items-center justify-center rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 px-5 py-8 text-center text-sm text-muted-foreground">
                            Loading preview...
                        </div>
                    ) : null}

                    {!isLoading && !previewable ? (
                        <div className="rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 px-5 py-8 text-sm text-muted-foreground">
                            <div className="flex items-center gap-3 text-foreground">
                                <FileCode2
                                    className="size-5 text-muted-foreground"
                                    aria-hidden="true"
                                />
                                <span>
                                    Binary preview is unavailable for this file
                                    type.
                                </span>
                            </div>
                        </div>
                    ) : null}

                    {!isLoading && previewable && renderer === "svg" ? (
                        <ExpandablePreview
                            fileName={fileName}
                            dialogContent={
                                <Image
                                    alt={`${fileName} full preview`}
                                    className="mx-auto block max-h-[calc(100vh-9rem)] max-w-full object-contain"
                                    src={proxyUrl}
                                    unoptimized
                                    width={1600}
                                    height={1200}
                                    sizes="100vw"
                                />
                            }
                        >
                            <Image
                                alt={`${fileName} preview`}
                                className="mx-auto block object-contain"
                                src={proxyUrl}
                                unoptimized
                                width={1200}
                                height={1200}
                                sizes="100vw"
                                style={{
                                    maxHeight: maxHeight
                                        ? `${maxHeight}px`
                                        : "480px",
                                    maxWidth: "100%",
                                }}
                            />
                        </ExpandablePreview>
                    ) : null}

                    {!isLoading && previewable && renderer === "pdf" ? (
                        <ExpandablePreview
                            fileName={fileName}
                            dialogContent={
                                <iframe
                                    className="block h-[calc(100vh-9rem)] w-full bg-background"
                                    src={proxyUrl}
                                    title="Enlarged PDF preview"
                                />
                            }
                        >
                            <iframe
                                className="block w-full bg-background"
                                src={proxyUrl}
                                style={{ height: `${maxHeight ?? 512}px` }}
                                title="PDF preview"
                            />
                        </ExpandablePreview>
                    ) : null}

                    {!isLoading && previewable && renderer === "html" ? (
                        <ExpandablePreview
                            fileName={fileName}
                            dialogContent={
                                <iframe
                                    className="block h-[calc(100vh-9rem)] w-full bg-white"
                                    sandbox={htmlPreviewSandbox}
                                    src={proxyUrl}
                                    title="Enlarged HTML preview"
                                />
                            }
                        >
                            <iframe
                                className="block w-full bg-white"
                                sandbox={htmlPreviewSandbox}
                                src={proxyUrl}
                                style={{ height: `${maxHeight ?? 512}px` }}
                                title="HTML preview"
                            />
                        </ExpandablePreview>
                    ) : null}

                    {!isLoading && previewable && renderer === "markdown" ? (
                        <ExpandablePreview
                            fileName={fileName}
                            onOpen={onEnlargeOpen}
                            dialogContent={
                                enlargedLoading && !enlargedContent ? (
                                    enlargedLoadingNode
                                ) : enlargedErrorNode ? (
                                    enlargedErrorNode
                                ) : (
                                    <div className="h-full overflow-auto">
                                        <article className="max-w-none p-6 text-foreground">
                                            <ReactMarkdown>
                                                {dialogContent?.content ?? ""}
                                            </ReactMarkdown>
                                        </article>
                                    </div>
                                )
                            }
                        >
                            <div>
                                <div className="relative">
                                    <article
                                        ref={markdownMeasureRef}
                                        className="max-w-none overflow-hidden p-6"
                                        style={
                                            maxHeight
                                                ? {
                                                      maxHeight: `${maxHeight}px`,
                                                  }
                                                : undefined
                                        }
                                    >
                                        <ReactMarkdown>
                                            {content?.content ?? ""}
                                        </ReactMarkdown>
                                    </article>
                                    {markdownInlineTruncated ? (
                                        <TruncationFade className="rounded-b-[1.5rem] bg-gradient-to-t from-background/95 via-background/60 to-transparent" />
                                    ) : null}
                                </div>
                            </div>
                        </ExpandablePreview>
                    ) : null}

                    {!isLoading &&
                    previewable &&
                    renderer === "csv" &&
                    content ? (
                        <ExpandablePreview
                            fileName={fileName}
                            onOpen={onEnlargeOpen}
                            dialogContent={
                                enlargedLoading && !enlargedContent ? (
                                    enlargedLoadingNode
                                ) : enlargedErrorNode ? (
                                    enlargedErrorNode
                                ) : dialogContent ? (
                                    <div>
                                        <CsvPreview
                                            key={buildPreviewInstanceKey(
                                                file.path,
                                                dialogContent.contentType,
                                                dialogContent.content,
                                                "expanded",
                                            )}
                                            content={dialogContent.content}
                                            contentType={
                                                dialogContent.contentType
                                            }
                                            isExpanded
                                            truncated={dialogContent.truncated}
                                        />
                                    </div>
                                ) : (
                                    enlargedLoadingNode
                                )
                            }
                        >
                            <div>
                                <div className="relative">
                                    <div
                                        ref={csvMeasureRef}
                                        className="h-full overflow-hidden"
                                        style={
                                            maxHeight
                                                ? {
                                                      maxHeight: `${maxHeight}px`,
                                                  }
                                                : undefined
                                        }
                                    >
                                        <CsvPreview
                                            key={buildPreviewInstanceKey(
                                                file.path,
                                                content.contentType,
                                                content.content,
                                                "inline",
                                            )}
                                            content={content.content}
                                            contentType={content.contentType}
                                            maxHeight={maxHeight}
                                            truncated={content.truncated}
                                        />
                                    </div>
                                    {csvInlineTruncated ? (
                                        <TruncationFade className="bg-gradient-to-t from-background/95 via-background/60 to-transparent" />
                                    ) : null}
                                </div>
                            </div>
                        </ExpandablePreview>
                    ) : null}

                    {!isLoading && previewable && renderer === "code" ? (
                        <ExpandablePreview
                            fileName={fileName}
                            onOpen={onEnlargeOpen}
                            dialogContent={
                                enlargedLoading && !enlargedContent ? (
                                    enlargedLoadingNode
                                ) : enlargedErrorNode ? (
                                    enlargedErrorNode
                                ) : (
                                    <div>
                                        <div className="overflow-hidden bg-[color:rgba(15,23,42,0.96)]">
                                            <pre className="overflow-auto p-5 text-sm leading-7 text-slate-100">
                                                <code
                                                    dangerouslySetInnerHTML={{
                                                        __html:
                                                            dialogHighlightedContent ??
                                                            "",
                                                    }}
                                                />
                                            </pre>
                                        </div>
                                    </div>
                                )
                            }
                        >
                            <div>
                                <div className="relative overflow-hidden bg-[color:rgba(15,23,42,0.96)]">
                                    <pre
                                        ref={codeMeasureRef}
                                        className="overflow-hidden p-5 text-sm leading-7 text-slate-100"
                                        style={
                                            maxHeight
                                                ? {
                                                      maxHeight: `${maxHeight}px`,
                                                  }
                                                : undefined
                                        }
                                    >
                                        <code
                                            dangerouslySetInnerHTML={{
                                                __html:
                                                    highlightedContent ?? "",
                                            }}
                                        />
                                    </pre>
                                    {codeInlineTruncated ? (
                                        <TruncationFade className="bg-gradient-to-t from-[color:rgba(15,23,42,0.96)] via-[color:rgba(15,23,42,0.7)] to-transparent" />
                                    ) : null}
                                </div>
                            </div>
                        </ExpandablePreview>
                    ) : null}
                </div>
            </div>
        </section>
    );
}
