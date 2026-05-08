"use client";

import { memo, useEffect, useMemo, useState } from "react";

import {
    buildDirectoryGroups,
    FileBrowser,
    findInitialSubdirPreviewDirectory,
    type PreviewMode,
} from "@/components/file-browser";
import {
    FileImageThumbnail,
    FilePreview,
    type FilePreviewError,
} from "@/components/file-preview";
import type { FileEntry } from "@/lib/contracts";
import { formatBytes } from "@/lib/utils";

type ResultDetailFilesProps = {
    files: FileEntry[];
    resultId: string;
};

type PreviewState = {
    content?: { content: string; contentType: string; truncated?: boolean };
    error?: FilePreviewError;
    isLoading: boolean;
    path: string | null;
};

type EnlargedPreviewState = {
    content?: { content: string; contentType: string; truncated?: boolean };
    error?: FilePreviewError;
    isLoading: boolean;
    path: string | null;
};

type FileUrlOptions = {
    download?: boolean;
    height?: number;
    mode?: "inline" | "enlarged" | "download";
    thumbnail?: boolean;
    width?: number;
};

const thumbnailsPerPage = 100;
const defaultPreviewHeight = 220;
const thumbnailRenderHeight = 420;
const compressedExtensions = new Set(["gz"]);
const imageExtensions = new Set([
    "avif",
    "bmp",
    "gif",
    "jpeg",
    "jpg",
    "png",
    "tif",
    "tiff",
    "webp",
]);
const proxyOnlyExtensions = new Set([
    "avif",
    "bam",
    "bmp",
    "cram",
    "gif",
    "h5",
    "hdf5",
    "htm",
    "html",
    "jpeg",
    "jpg",
    "pdf",
    "png",
    "tif",
    "tiff",
    "webp",
]);

function effectiveExtensionFromPath(path: string): string {
    const name = path.split("/").pop() ?? path;
    const extensions = name
        .split(".")
        .slice(1)
        .map((extension) => extension.toLowerCase())
        .filter((extension) => extension.length > 0);

    if (extensions.length === 0) {
        return "";
    }

    const lastExtension = extensions.at(-1) ?? "";

    if (compressedExtensions.has(lastExtension) && extensions.length > 1) {
        return extensions.at(-2) ?? lastExtension;
    }

    return lastExtension;
}

class PreviewRequestError extends Error {
    constructor(
        readonly status: number,
        readonly body:
            | { body?: unknown; fileSize?: number }
            | string
            | null
            | undefined,
    ) {
        super(`Preview request failed: ${status}`);
        this.name = "PreviewRequestError";
    }
}

function extractPreviewErrorMessage(body: unknown): string | undefined {
    if (typeof body === "string") {
        const trimmed = body.trim();

        return trimmed.length > 0 ? trimmed : undefined;
    }

    if (
        body &&
        typeof body === "object" &&
        "error" in body &&
        typeof body.error === "string"
    ) {
        const trimmed = body.error.trim();

        return trimmed.length > 0 ? trimmed : undefined;
    }

    return undefined;
}

function buildFileUrl(
    resultId: string,
    path: string,
    options: FileUrlOptions = {},
): string {
    const query = new URLSearchParams({ id: resultId, path });

    if (options.download) {
        query.set("download", "true");
    }

    if (options.mode) {
        query.set("mode", options.mode);
    }

    if (options.thumbnail) {
        query.set("thumb", "true");
        query.set(
            "w",
            String(options.width ?? Math.round(defaultPreviewHeight * 1.6)),
        );
        query.set("h", String(options.height ?? defaultPreviewHeight));
    }

    return `/api/file?${query.toString()}`;
}

function shouldFetchInlinePreview(path: string): boolean {
    const extension = effectiveExtensionFromPath(path);

    return !proxyOnlyExtensions.has(extension);
}

function isImageFile(path: string): boolean {
    const extension = effectiveExtensionFromPath(path);

    return imageExtensions.has(extension);
}

function buildPreviewState(file: FileEntry | null): PreviewState {
    return {
        content: undefined,
        error: undefined,
        isLoading: file ? shouldFetchInlinePreview(file.path) : false,
        path: file?.path ?? null,
    };
}

async function fetchPreviewContent(
    resultId: string,
    path: string,
    mode: "inline" | "enlarged",
): Promise<{ content: string; contentType: string; truncated?: boolean }> {
    const response = await fetch(buildFileUrl(resultId, path, { mode }));

    if (!response.ok) {
        const contentType = response.headers.get("content-type") ?? "";
        const body = contentType.includes("application/json")
            ? await response.json().catch(() => null)
            : await response.text();
        const fileSizeHeader = response.headers.get("x-file-size");

        throw new PreviewRequestError(
            response.status,
            response.status === 413
                ? {
                      body,
                      fileSize: fileSizeHeader
                          ? Number(fileSizeHeader)
                          : undefined,
                  }
                : body,
        );
    }

    return {
        content: await response.text(),
        contentType: response.headers.get("content-type") ?? "text/plain",
        truncated: response.headers.get("x-preview-truncated") === "true",
    };
}

function pageSummary(total: number, page: number): string {
    if (total === 0) {
        return "No previews available";
    }

    const start = (page - 1) * thumbnailsPerPage + 1;
    const end = Math.min(total, page * thumbnailsPerPage);

    return `Showing ${start}-${end} of ${total} files`;
}

function parentDirectory(path: string): string {
    const normalized = path.trim();
    const index = normalized.lastIndexOf("/");

    if (index <= 0) {
        return "/";
    }

    return normalized.slice(0, index);
}

function resolveDirectorySelection(
    directoryPath: string,
    currentSelectedDirectory: string | undefined,
    directoryGroups: Array<{ path: string }>,
    options?: { expanded: boolean; parentPath?: string },
): string {
    if (directoryPath !== currentSelectedDirectory) {
        return directoryPath;
    }

    if (options?.expanded !== false) {
        return directoryPath;
    }

    const fallbackDirectory =
        options?.parentPath ?? parentDirectory(directoryPath);

    return directoryGroups.some(
        (group) =>
            group.path === fallbackDirectory ||
            group.path.startsWith(`${fallbackDirectory}/`),
    )
        ? fallbackDirectory
        : directoryPath;
}

const GalleryPreviewRow = memo(function GalleryPreviewRow({
    file,
    maxHeight,
    resultId,
}: {
    file: FileEntry;
    maxHeight: number;
    resultId: string;
}) {
    const [previewState, setPreviewState] = useState<PreviewState>(() =>
        buildPreviewState(file),
    );

    useEffect(() => {
        if (!shouldFetchInlinePreview(file.path)) {
            return;
        }

        let cancelled = false;

        void fetchPreviewContent(resultId, file.path, "inline")
            .then((nextContent) => {
                if (cancelled) {
                    return;
                }

                setPreviewState({
                    content: nextContent,
                    error: undefined,
                    isLoading: false,
                    path: file.path,
                });
            })
            .catch((error: unknown) => {
                if (cancelled) {
                    return;
                }

                if (error instanceof PreviewRequestError) {
                    const payload = error.body as {
                        body?: unknown;
                        fileSize?: number;
                    } | null;
                    const body =
                        error.status === 413 ? payload?.body : error.body;

                    setPreviewState({
                        content: undefined,
                        error: {
                            fileSize: payload?.fileSize,
                            message: extractPreviewErrorMessage(body),
                            status: error.status,
                        },
                        isLoading: false,
                        path: file.path,
                    });
                } else {
                    setPreviewState({
                        content: undefined,
                        error: {
                            message: "Preview request failed",
                            status: 500,
                        },
                        isLoading: false,
                        path: file.path,
                    });
                }
            });

        return () => {
            cancelled = true;
        };
    }, [file.path, resultId]);

    return (
        <div data-grid-preview-path={file.path}>
            <FilePreview
                content={previewState.content}
                error={previewState.error}
                file={file}
                isLoading={previewState.isLoading}
                maxHeight={maxHeight}
                proxyUrl={buildFileUrl(resultId, file.path)}
            />
        </div>
    );
}, areGalleryPreviewRowPropsEqual);

function areGalleryPreviewRowPropsEqual(
    previous: { file: FileEntry; maxHeight: number; resultId: string },
    next: { file: FileEntry; maxHeight: number; resultId: string },
): boolean {
    return (
        previous.file.path === next.file.path &&
        previous.maxHeight === next.maxHeight &&
        previous.resultId === next.resultId
    );
}

export function ResultDetailFiles({ files, resultId }: ResultDetailFilesProps) {
    const directoryGroups = useMemo(() => buildDirectoryGroups(files), [files]);
    const initialSelectedDirectory = useMemo(
        () =>
            findInitialSubdirPreviewDirectory(files) ??
            directoryGroups[0]?.path,
        [directoryGroups, files],
    );
    const initialSelectedFile = useMemo(
        () =>
            directoryGroups.find(
                (group) => group.path === initialSelectedDirectory,
            )?.files[0] ?? null,
        [directoryGroups, initialSelectedDirectory],
    );
    const [previewMode, setPreviewMode] = useState<PreviewMode>("single");
    const [previewHeight, setPreviewHeight] = useState(defaultPreviewHeight);
    const [selectedDirectory, setSelectedDirectory] = useState<
        string | undefined
    >(initialSelectedDirectory);
    const [selectedFile, setSelectedFile] = useState<FileEntry | null>(
        initialSelectedFile,
    );
    const [previewPage, setPreviewPage] = useState(1);
    const [previewState, setPreviewState] = useState<PreviewState>(() =>
        buildPreviewState(initialSelectedFile),
    );
    const [enlargedState, setEnlargedState] = useState<EnlargedPreviewState>({
        content: undefined,
        error: undefined,
        isLoading: false,
        path: null,
    });
    const effectiveSelectedDirectory =
        selectedDirectory ?? initialSelectedDirectory;

    const selectedGroup = useMemo(
        () =>
            directoryGroups.find(
                (group) => group.path === effectiveSelectedDirectory,
            ),
        [directoryGroups, effectiveSelectedDirectory],
    );
    const selectedDirectoryFiles = useMemo(
        () => selectedGroup?.files ?? [],
        [selectedGroup],
    );
    const effectiveSelectedFile = useMemo(() => {
        if (!selectedFile) {
            return selectedDirectoryFiles[0] ?? null;
        }

        const matchingFile = selectedDirectoryFiles.find(
            (file) => file.path === selectedFile.path,
        );

        return matchingFile ?? selectedDirectoryFiles[0] ?? null;
    }, [selectedDirectoryFiles, selectedFile]);
    const previewPageCount = Math.max(
        1,
        Math.ceil(selectedDirectoryFiles.length / thumbnailsPerPage),
    );
    const effectivePreviewPage = Math.min(previewPage, previewPageCount);
    const visiblePreviewFiles = selectedDirectoryFiles.slice(
        (effectivePreviewPage - 1) * thumbnailsPerPage,
        effectivePreviewPage * thumbnailsPerPage,
    );

    useEffect(() => {
        if (
            previewMode === "grid" ||
            !effectiveSelectedFile ||
            !shouldFetchInlinePreview(effectiveSelectedFile.path)
        ) {
            return;
        }

        let cancelled = false;
        const selectedPath = effectiveSelectedFile.path;

        void fetchPreviewContent(resultId, selectedPath, "inline")
            .then((nextContent) => {
                if (cancelled) {
                    return;
                }

                setPreviewState({
                    content: nextContent,
                    error: undefined,
                    isLoading: false,
                    path: selectedPath,
                });
            })
            .catch((error: unknown) => {
                if (cancelled) {
                    return;
                }

                if (error instanceof PreviewRequestError) {
                    const payload = error.body as {
                        body?: unknown;
                        fileSize?: number;
                    } | null;
                    const body =
                        error.status === 413 ? payload?.body : error.body;

                    setPreviewState({
                        content: undefined,
                        error: {
                            fileSize: payload?.fileSize,
                            message: extractPreviewErrorMessage(body),
                            status: error.status,
                        },
                        isLoading: false,
                        path: selectedPath,
                    });
                } else {
                    setPreviewState({
                        content: undefined,
                        error: {
                            message: "Preview request failed",
                            status: 500,
                        },
                        isLoading: false,
                        path: selectedPath,
                    });
                }
            });

        return () => {
            cancelled = true;
        };
    }, [effectiveSelectedFile, previewMode, resultId]);

    const renderSinglePreview = (file: FileEntry | null) => {
        if (!file) {
            return (
                <p className="text-sm leading-7 text-muted-foreground">
                    No files are registered for this result set.
                </p>
            );
        }

        const previewContent =
            previewState.path === file.path ? previewState.content : undefined;
        const previewError =
            previewState.path === file.path ? previewState.error : undefined;
        const isLoading =
            previewState.path === file.path ? previewState.isLoading : false;
        const enlargedForFile =
            enlargedState.path === file.path ? enlargedState : undefined;

        return (
            <div>
                <FilePreview
                    content={previewContent}
                    enlargedContent={enlargedForFile?.content}
                    enlargedError={enlargedForFile?.error}
                    enlargedLoading={enlargedForFile?.isLoading ?? false}
                    error={previewError}
                    file={file}
                    isLoading={isLoading}
                    maxHeight={previewHeight}
                    onEnlargeOpen={() => {
                        if (
                            !shouldFetchInlinePreview(file.path) ||
                            (enlargedState.path === file.path &&
                                (enlargedState.content !== undefined ||
                                    enlargedState.isLoading))
                        ) {
                            return;
                        }

                        setEnlargedState({
                            content: undefined,
                            error: undefined,
                            isLoading: true,
                            path: file.path,
                        });

                        void fetchPreviewContent(
                            resultId,
                            file.path,
                            "enlarged",
                        )
                            .then((nextContent) => {
                                setEnlargedState((current) => {
                                    if (current.path !== file.path) {
                                        return current;
                                    }

                                    return {
                                        content: nextContent,
                                        error: undefined,
                                        isLoading: false,
                                        path: file.path,
                                    };
                                });
                            })
                            .catch((fetchError: unknown) => {
                                setEnlargedState((current) => {
                                    if (current.path !== file.path) {
                                        return current;
                                    }

                                    if (
                                        fetchError instanceof
                                        PreviewRequestError
                                    ) {
                                        const payload = fetchError.body as {
                                            body?: unknown;
                                            fileSize?: number;
                                        } | null;
                                        const body =
                                            fetchError.status === 413
                                                ? payload?.body
                                                : fetchError.body;

                                        return {
                                            content: undefined,
                                            error: {
                                                fileSize: payload?.fileSize,
                                                message:
                                                    extractPreviewErrorMessage(
                                                        body,
                                                    ),
                                                status: fetchError.status,
                                            },
                                            isLoading: false,
                                            path: file.path,
                                        };
                                    }

                                    return {
                                        content: undefined,
                                        error: {
                                            message: "Preview request failed",
                                            status: 500,
                                        },
                                        isLoading: false,
                                        path: file.path,
                                    };
                                });
                            });
                    }}
                    proxyUrl={buildFileUrl(resultId, file.path)}
                />
            </div>
        );
    };

    return (
        <FileBrowser
            files={files}
            onPreviewHeightChange={setPreviewHeight}
            onPreviewModeChange={(nextMode) => {
                setPreviewMode(nextMode);
                setPreviewPage(1);
            }}
            onPreviewPageChange={setPreviewPage}
            onSelectDirectory={(directoryPath, options) => {
                const nextDirectoryPath = resolveDirectorySelection(
                    directoryPath,
                    selectedDirectory,
                    directoryGroups,
                    options,
                );

                if (nextDirectoryPath === selectedDirectory) {
                    return;
                }

                const nextGroup = directoryGroups.find(
                    (group) => group.path === nextDirectoryPath,
                );
                const nextFile = nextGroup?.files[0] ?? null;

                setSelectedDirectory(nextDirectoryPath);
                setSelectedFile(nextFile);
                setPreviewPage(1);
                setPreviewState(buildPreviewState(nextFile));
                setEnlargedState({
                    content: undefined,
                    error: undefined,
                    isLoading: false,
                    path: null,
                });
            }}
            onSelectFile={(file) => {
                if (selectedFile?.path === file.path) {
                    return;
                }

                setSelectedFile(file);
                setPreviewState(buildPreviewState(file));
                setEnlargedState({
                    content: undefined,
                    error: undefined,
                    isLoading: false,
                    path: null,
                });
            }}
            previewHeight={previewHeight}
            previewMode={previewMode}
            previewPage={effectivePreviewPage}
            previewPageCount={previewPageCount}
            previewSummary={
                previewPageCount > 1
                    ? pageSummary(
                          selectedDirectoryFiles.length,
                          effectivePreviewPage,
                      )
                    : undefined
            }
            renderGridPreview={(file) =>
                isImageFile(file.path) ? (
                    <FileImageThumbnail
                        file={file}
                        fullSizeUrl={buildFileUrl(resultId, file.path)}
                        height={previewHeight}
                        thumbnailUrl={buildFileUrl(resultId, file.path, {
                            height: thumbnailRenderHeight,
                            thumbnail: true,
                            width: Math.max(
                                320,
                                Math.round(thumbnailRenderHeight * 1.6),
                            ),
                        })}
                    />
                ) : (
                    <GalleryPreviewRow
                        file={file}
                        key={file.path}
                        maxHeight={previewHeight}
                        resultId={resultId}
                    />
                )
            }
            renderSinglePreview={renderSinglePreview}
            selectedDirectory={effectiveSelectedDirectory}
            selectedPath={effectiveSelectedFile?.path}
            visibleFiles={visiblePreviewFiles}
        />
    );
}
