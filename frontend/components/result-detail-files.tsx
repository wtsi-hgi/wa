"use client";

import { useEffect, useMemo, useState } from "react";
import { Images } from "lucide-react";

import {
    buildDirectoryGroups,
    FileBrowser,
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
    content?: { content: string; contentType: string };
    error?: FilePreviewError;
    isLoading: boolean;
    path: string | null;
};

type FileUrlOptions = {
    download?: boolean;
    height?: number;
    thumbnail?: boolean;
    width?: number;
};

const thumbnailsPerPage = 100;
const defaultPreviewHeight = 220;
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

    if (options.thumbnail) {
        query.set("thumb", "true");
        query.set("w", String(options.width ?? Math.round(defaultPreviewHeight * 1.6)));
        query.set("h", String(options.height ?? defaultPreviewHeight));
    }

    return `/api/file?${query.toString()}`;
}

function shouldFetchInlinePreview(path: string): boolean {
    const extension = path.split(".").pop()?.toLowerCase() ?? "";

    return !proxyOnlyExtensions.has(extension);
}

function isImageFile(path: string): boolean {
    const extension = path.split(".").pop()?.toLowerCase() ?? "";

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
): Promise<{ content: string; contentType: string }> {
    const response = await fetch(buildFileUrl(resultId, path));

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

function GalleryPreviewRow({
    file,
    resultId,
}: {
    file: FileEntry;
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

        void fetchPreviewContent(resultId, file.path)
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
                proxyUrl={buildFileUrl(resultId, file.path)}
            />
        </div>
    );
}

export function ResultDetailFiles({ files, resultId }: ResultDetailFilesProps) {
    const directoryGroups = useMemo(() => buildDirectoryGroups(files), [files]);
    const [previewMode, setPreviewMode] = useState<PreviewMode>("single");
    const [previewHeight, setPreviewHeight] = useState(defaultPreviewHeight);
    const [selectedDirectory, setSelectedDirectory] = useState<string | undefined>(
        directoryGroups[0]?.path,
    );
    const [selectedFile, setSelectedFile] = useState<FileEntry | null>(
        directoryGroups[0]?.files[0] ?? null,
    );
    const [previewPage, setPreviewPage] = useState(1);
    const [previewState, setPreviewState] = useState<PreviewState>(() =>
        buildPreviewState(directoryGroups[0]?.files[0] ?? null),
    );

    const selectedGroup =
        directoryGroups.find((group) => group.path === selectedDirectory) ??
        directoryGroups[0];
    const selectedDirectoryFiles = selectedGroup?.files ?? [];
    const previewPageCount = Math.max(
        1,
        Math.ceil(selectedDirectoryFiles.length / thumbnailsPerPage),
    );
    const visiblePreviewFiles = selectedDirectoryFiles.slice(
        (previewPage - 1) * thumbnailsPerPage,
        previewPage * thumbnailsPerPage,
    );

    const previewContent =
        selectedFile && previewState.path === selectedFile.path
            ? previewState.content
            : undefined;
    const previewError =
        selectedFile && previewState.path === selectedFile.path
            ? previewState.error
            : undefined;
    const isLoading =
        selectedFile && previewState.path === selectedFile.path
            ? previewState.isLoading
            : false;

    useEffect(() => {
        const fallbackDirectory = directoryGroups[0]?.path;

        if (!selectedDirectory || !selectedGroup) {
            setSelectedDirectory(fallbackDirectory);
            setSelectedFile(directoryGroups[0]?.files[0] ?? null);
            setPreviewPage(1);
            setPreviewState(buildPreviewState(directoryGroups[0]?.files[0] ?? null));
            return;
        }

        const fileStillPresent = selectedFile
            ? selectedDirectoryFiles.some((file) => file.path === selectedFile.path)
            : false;

        if (!fileStillPresent) {
            const nextFile = selectedDirectoryFiles[0] ?? null;
            setSelectedFile(nextFile);
            setPreviewState(buildPreviewState(nextFile));
        }
    }, [directoryGroups, selectedDirectory, selectedDirectoryFiles, selectedFile, selectedGroup]);

    useEffect(() => {
        if (previewPage <= previewPageCount) {
            return;
        }

        setPreviewPage(previewPageCount);
    }, [previewPage, previewPageCount]);

    useEffect(() => {
        if (
            previewMode === "grid" ||
            !selectedFile ||
            !shouldFetchInlinePreview(selectedFile.path)
        ) {
            return;
        }

        let cancelled = false;
        const selectedPath = selectedFile.path;

        void fetchPreviewContent(resultId, selectedPath)
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
    }, [previewMode, resultId, selectedFile]);

    return (
        <section className="grid gap-6 xl:grid-cols-[minmax(0,1.05fr)_minmax(24rem,0.95fr)] 2xl:grid-cols-[minmax(0,1.05fr)_minmax(28rem,0.95fr)]">
            <div className="min-w-0">
                <FileBrowser
                    files={files}
                    onPreviewHeightChange={setPreviewHeight}
                    onPreviewModeChange={(nextMode) => {
                        setPreviewMode(nextMode);
                        setPreviewPage(1);
                    }}
                    onPreviewPageChange={setPreviewPage}
                    onSelectDirectory={(directoryPath) => {
                        if (directoryPath === selectedDirectory) {
                            return;
                        }

                        const nextGroup = directoryGroups.find(
                            (group) => group.path === directoryPath,
                        );
                        const nextFile = nextGroup?.files[0] ?? null;

                        setSelectedDirectory(directoryPath);
                        setSelectedFile(nextFile);
                        setPreviewPage(1);
                        setPreviewState(buildPreviewState(nextFile));
                    }}
                    onSelectFile={(file) => {
                        if (selectedFile?.path === file.path) {
                            return;
                        }

                        setSelectedFile(file);
                        setPreviewState(buildPreviewState(file));
                    }}
                    previewHeight={previewHeight}
                    previewMode={previewMode}
                    previewPage={previewPage}
                    previewPageCount={previewPageCount}
                    selectedDirectory={selectedGroup?.path}
                    selectedPath={selectedFile?.path}
                />
            </div>

            <section className="min-w-0 rounded-[1.75rem] border border-border/70 bg-card/85 p-6 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]">
                {previewMode === "single" ? (
                    <>
                        <div className="space-y-2">
                            <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
                                Preview
                            </p>
                            <h2 className="text-2xl font-semibold tracking-tight">
                                Selected file
                            </h2>
                        </div>

                        {selectedFile ? (
                            <div
                                className="mt-6"
                                data-selected-file-path={selectedFile.path}
                            >
                                <FilePreview
                                    content={previewContent}
                                    error={previewError}
                                    file={selectedFile}
                                    isLoading={isLoading}
                                    proxyUrl={buildFileUrl(resultId, selectedFile.path)}
                                />
                            </div>
                        ) : (
                            <p className="mt-6 text-sm leading-7 text-muted-foreground">
                                No files are registered for this result set.
                            </p>
                        )}
                    </>
                ) : (
                    <>
                        <div className="flex flex-col gap-3 border-b border-border/60 pb-5 xl:flex-row xl:items-end xl:justify-between">
                            <div>
                                <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
                                    Preview gallery
                                </p>
                                <h2 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
                                    {selectedGroup?.path ?? "Selected directory"}
                                </h2>
                                <p className="mt-2 text-sm text-muted-foreground">
                                    {pageSummary(selectedDirectoryFiles.length, previewPage)}
                                </p>
                            </div>
                            <div className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/70 px-4 py-2 text-sm text-muted-foreground">
                                <Images className="size-4 text-primary" aria-hidden="true" />
                                1 preview per row at {previewHeight}px high
                            </div>
                        </div>

                        <div className="mt-6 space-y-4" data-preview-mode="grid">
                            {visiblePreviewFiles.map((file) =>
                                isImageFile(file.path) ? (
                                    <FileImageThumbnail
                                        key={file.path}
                                        file={file}
                                        fullSizeUrl={buildFileUrl(resultId, file.path)}
                                        height={previewHeight}
                                        thumbnailUrl={buildFileUrl(resultId, file.path, {
                                            height: previewHeight,
                                            thumbnail: true,
                                            width: Math.max(
                                                320,
                                                Math.round(previewHeight * 1.6),
                                            ),
                                        })}
                                    />
                                ) : (
                                    <GalleryPreviewRow
                                        key={file.path}
                                        file={file}
                                        resultId={resultId}
                                    />
                                ),
                            )}
                        </div>
                    </>
                )}
            </section>
        </section>
    );
}
