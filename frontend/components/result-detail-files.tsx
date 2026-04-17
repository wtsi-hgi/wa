"use client";

import { useEffect, useState } from "react";

import { FileBrowser } from "@/components/file-browser";
import { FilePreview, type FilePreviewError } from "@/components/file-preview";
import type { FileEntry } from "@/lib/contracts";

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

const proxyOnlyExtensions = new Set([
    "avif",
    "bam",
    "bmp",
    "cram",
    "gif",
    "h5",
    "hdf5",
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

function pickInitialFile(files: FileEntry[]): FileEntry | null {
    for (const kind of ["output", "input", "pipeline"] as const) {
        const match = files.find((file) => file.kind === kind);

        if (match) {
            return match;
        }
    }

    return files[0] ?? null;
}

function buildFileUrl(
    resultId: string,
    path: string,
    download = false,
): string {
    const query = new URLSearchParams({ id: resultId, path });

    if (download) {
        query.set("download", "true");
    }

    return `/api/file?${query.toString()}`;
}

function shouldFetchInlinePreview(path: string): boolean {
    const extension = path.split(".").pop()?.toLowerCase() ?? "";

    return !proxyOnlyExtensions.has(extension);
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
                    fileSize: fileSizeHeader ? Number(fileSizeHeader) : undefined,
                }
                : body,
        );
    }

    return {
        content: await response.text(),
        contentType: response.headers.get("content-type") ?? "text/plain",
    };
}

export function ResultDetailFiles({ files, resultId }: ResultDetailFilesProps) {
    const [selectedFile, setSelectedFile] = useState<FileEntry | null>(() =>
        pickInitialFile(files),
    );
    const [previewState, setPreviewState] = useState<PreviewState>(() => {
        const initialFile = pickInitialFile(files);

        return {
            content: undefined,
            error: undefined,
            isLoading: initialFile
                ? shouldFetchInlinePreview(initialFile.path)
                : false,
            path: initialFile?.path ?? null,
        };
    });

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
        if (!selectedFile || !shouldFetchInlinePreview(selectedFile.path)) {
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

                    setPreviewState({
                        content: undefined,
                        error: {
                            fileSize: payload?.fileSize,
                            message:
                                typeof payload?.body === "string" ? payload.body : undefined,
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
    }, [resultId, selectedFile]);

    return (
        <section className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(18rem,22rem)] 2xl:grid-cols-[minmax(0,1.3fr)_minmax(20rem,0.8fr)]">
            <div className="min-w-0">
                <FileBrowser
                    files={files}
                    onSelectFile={(file) => {
                        setSelectedFile(file);
                        setPreviewState({
                            content: undefined,
                            error: undefined,
                            isLoading: shouldFetchInlinePreview(file.path),
                            path: file.path,
                        });
                    }}
                    selectedPath={selectedFile?.path}
                />
            </div>

            <aside className="min-w-0 rounded-[1.75rem] border border-border/70 bg-card/85 p-6 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]">
                <div className="space-y-2">
                    <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
                        File focus
                    </p>
                    <h2 className="text-2xl font-semibold tracking-tight">
                        Selected file
                    </h2>
                </div>

                {selectedFile ? (
                    <div className="mt-6" data-selected-file-path={selectedFile.path}>
                        <FilePreview
                            content={previewContent}
                            error={previewError}
                            file={selectedFile}
                            isLoading={isLoading}
                            proxyUrl={buildFileUrl(resultId, selectedFile.path)}
                            resultId={resultId}
                        />
                    </div>
                ) : (
                    <p className="mt-6 text-sm leading-7 text-muted-foreground">
                        No files are registered for this result set.
                    </p>
                )}
            </aside>
        </section>
    );
}
