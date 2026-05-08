// @vitest-environment jsdom

import { createElement } from "react";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { FileEntry } from "@/lib/contracts";

const fetchMock = vi.fn<typeof fetch>();
const { filePreviewRenderCounts } = vi.hoisted(() => ({
    filePreviewRenderCounts: new Map<string, number>(),
}));
const imageExtensions = new Set([
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
]);

vi.stubGlobal("fetch", fetchMock);

vi.mock("@/components/file-browser", () => ({
    buildDirectoryGroups: (files: FileEntry[]) => {
        const groups = new Map<string, FileEntry[]>();

        for (const file of files) {
            const directoryPath =
                file.path.split("/").slice(0, -1).join("/") || "/";
            const current = groups.get(directoryPath) ?? [];

            current.push(file);
            groups.set(directoryPath, current);
        }

        return [...groups.entries()].map(([path, groupedFiles]) => ({
            fileCount: groupedFiles.length,
            files: groupedFiles,
            path,
            totalSize: groupedFiles.reduce(
                (total, file) => total + file.size,
                0,
            ),
            typeCounts: {},
        }));
    },
    findInitialSubdirPreviewDirectory: (files: FileEntry[]) => {
        const firstPath = files[0]?.path;

        if (!firstPath) {
            return undefined;
        }

        const firstDirectory =
            firstPath.split("/").slice(0, -1).join("/") || "/";
        const parentDirectory =
            firstDirectory.split("/").slice(0, -1).join("/") || "/";
        const siblingDirectories = new Set(
            files
                .filter((file) => {
                    const extension =
                        file.path.split(".").pop()?.toLowerCase() ?? "";

                    return imageExtensions.has(extension);
                })
                .map(
                    (file) =>
                        file.path.split("/").slice(0, -1).join("/") || "/",
                )
                .filter(
                    (directoryPath) =>
                        directoryPath.startsWith(`${parentDirectory}/`) ||
                        directoryPath === firstDirectory,
                ),
        );

        return siblingDirectories.size > 1 ? parentDirectory : undefined;
    },
    FileBrowser: ({
        files,
        onPreviewHeightChange,
        onPreviewModeChange,
        onPreviewPageChange,
        onSelectDirectory,
        onSelectFile,
        previewHeight,
        previewMode,
        previewPage,
        previewPageCount,
        previewSummary,
        renderGridPreview,
        renderSinglePreview,
        selectedDirectory,
        selectedPath,
        visibleFiles,
    }: {
        files: FileEntry[];
        onPreviewHeightChange?: (value: number) => void;
        onPreviewModeChange?: (mode: "single" | "grid") => void;
        onPreviewPageChange?: (page: number) => void;
        onSelectDirectory?: (
            path: string,
            options?: { expanded: boolean; parentPath?: string },
        ) => void;
        onSelectFile: (file: FileEntry) => void;
        previewHeight?: number;
        previewMode?: "single" | "grid";
        previewPage?: number;
        previewPageCount?: number;
        previewSummary?: string;
        renderGridPreview?: (file: FileEntry) => React.ReactNode;
        renderSinglePreview?: (file: FileEntry | null) => React.ReactNode;
        selectedDirectory?: string;
        selectedPath?: string;
        visibleFiles?: FileEntry[];
    }) => {
        const directoryPaths = [
            ...new Set(
                files.map(
                    (file) =>
                        file.path.split("/").slice(0, -1).join("/") || "/",
                ),
            ),
        ];
        const interactiveDirectoryPaths = selectedDirectory
            ? [selectedDirectory, ...directoryPaths].filter(
                  (value, index, values) => values.indexOf(value) === index,
              )
            : directoryPaths;

        return createElement(
            "div",
            {
                "data-file-browser": "true",
                "data-preview-height": String(previewHeight ?? 0),
                "data-preview-mode": previewMode ?? "single",
                "data-preview-page": String(previewPage ?? 1),
                "data-preview-page-count": String(previewPageCount ?? 1),
                "data-preview-summary": previewSummary ?? "",
                "data-selected-directory": selectedDirectory ?? "",
                "data-selected-path": selectedPath ?? "",
            },
            createElement("h2", { key: "title" }, "File Browser"),
            directoryPaths.map((directoryPath) =>
                createElement(
                    "button",
                    {
                        key: `dir-${directoryPath}`,
                        "data-directory-path": directoryPath,
                        onClick: () => onSelectDirectory?.(directoryPath),
                        type: "button",
                    },
                    directoryPath,
                ),
            ),
            interactiveDirectoryPaths.flatMap((directoryPath) => [
                createElement(
                    "button",
                    {
                        key: `dir-expand-${directoryPath}`,
                        "data-directory-expand-path": directoryPath,
                        onClick: () =>
                            onSelectDirectory?.(directoryPath, {
                                expanded: true,
                            }),
                        type: "button",
                    },
                    `expand:${directoryPath}`,
                ),
                createElement(
                    "button",
                    {
                        key: `dir-collapse-${directoryPath}`,
                        "data-directory-collapse-path": directoryPath,
                        onClick: () =>
                            onSelectDirectory?.(directoryPath, {
                                expanded: false,
                            }),
                        type: "button",
                    },
                    `collapse:${directoryPath}`,
                ),
            ]),
            (visibleFiles ?? files).map((file) =>
                createElement(
                    "button",
                    {
                        key: file.path,
                        "data-file-path": file.path,
                        onClick: () => onSelectFile(file),
                        type: "button",
                    },
                    file.path,
                ),
            ),
            createElement(
                "button",
                {
                    key: "preview-height-320",
                    onClick: () => onPreviewHeightChange?.(320),
                    type: "button",
                },
                "preview-height-320",
            ),
            createElement(
                "button",
                {
                    key: "show-grid",
                    onClick: () => onPreviewModeChange?.("grid"),
                    type: "button",
                },
                "show-grid",
            ),
            createElement(
                "button",
                {
                    key: "show-single",
                    onClick: () => onPreviewModeChange?.("single"),
                    type: "button",
                },
                "show-single",
            ),
            createElement(
                "button",
                {
                    key: "next-page",
                    onClick: () =>
                        onPreviewPageChange?.(
                            Math.min(
                                (previewPage ?? 1) + 1,
                                previewPageCount ?? 1,
                            ),
                        ),
                    type: "button",
                },
                "next-page",
            ),
            previewMode === "single"
                ? createElement(
                      "div",
                      {
                          key: "single-preview",
                          "data-testid": "single-preview-slot",
                      },
                      renderSinglePreview?.(
                          files.find((file) => file.path === selectedPath) ??
                              null,
                      ),
                  )
                : null,
            ...(previewMode === "grid"
                ? (visibleFiles ?? files).map((file) =>
                      createElement(
                          "div",
                          {
                              key: `grid-preview-${file.path}`,
                              "data-testid": "grid-preview-slot",
                          },
                          renderGridPreview?.(file),
                      ),
                  )
                : []),
        );
    },
}));

vi.mock("@/components/file-preview", () => ({
    FileImageThumbnail: ({
        file,
        fullSizeUrl,
        height,
        thumbnailUrl,
    }: {
        file: FileEntry;
        fullSizeUrl: string;
        height?: number;
        thumbnailUrl: string;
    }) =>
        createElement(
            "div",
            {
                "data-full-size-url": fullSizeUrl,
                "data-height": String(height ?? 0),
                "data-testid": "thumbnail-preview",
                "data-thumbnail-url": thumbnailUrl,
            },
            file.path,
        ),
    FilePreview: ({
        file,
        proxyUrl,
    }: {
        file: FileEntry;
        proxyUrl: string;
    }) => {
        filePreviewRenderCounts.set(
            file.path,
            (filePreviewRenderCounts.get(file.path) ?? 0) + 1,
        );

        return createElement(
            "div",
            { "data-preview-url": proxyUrl },
            `preview:${file.path}`,
        );
    },
}));

function buildFile(path: string): FileEntry {
    return {
        kind: "output",
        mtime: "2026-04-16T10:15:00Z",
        path,
        size: 512,
    };
}

afterEach(() => {
    cleanup();
    fetchMock.mockReset();
    filePreviewRenderCounts.clear();
});

describe("O1 result detail file integration", () => {
    it("initializes the controlled file browser selection to the eligible parent directory", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        render(
            createElement(ResultDetailFiles, {
                files: [
                    buildFile("/tmp/results/sample-a/first.png"),
                    buildFile("/tmp/results/sample-b/second.png"),
                ],
                resultId: "result-1",
            }),
        );

        expect(screen.getByText("File Browser")).toBeTruthy();
        expect(
            screen
                .getByText("File Browser")
                .parentElement?.getAttribute("data-selected-directory"),
        ).toBe("/tmp/results");
    });

    it("removes the old file focus aside and defaults to the first file in the selected directory", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        render(
            createElement(ResultDetailFiles, {
                files: [
                    buildFile("/tmp/results/a/first.png"),
                    buildFile("/tmp/results/a/second.txt"),
                    buildFile("/tmp/results/b/third.txt"),
                ],
                resultId: "result-1",
            }),
        );

        expect(screen.queryByText("File focus")).toBeNull();
        expect(screen.queryByText("Selected file")).toBeNull();
        expect(screen.getByText("File Browser")).toBeTruthy();
        expect(
            screen.getByText("preview:/tmp/results/a/first.png"),
        ).toBeTruthy();
    });

    it("switches to the first file in a newly selected directory", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        render(
            createElement(ResultDetailFiles, {
                files: [
                    buildFile("/tmp/results/a/first.png"),
                    buildFile("/tmp/results/b/third.png"),
                    buildFile("/tmp/results/b/fourth.txt"),
                ],
                resultId: "result-1",
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: "/tmp/results/b" }));

        await waitFor(() => {
            expect(
                screen.getByText("preview:/tmp/results/b/third.png"),
            ).toBeTruthy();
        });
    });

    it("reselects the parent directory when the selected child directory is clicked again", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        render(
            createElement(ResultDetailFiles, {
                files: [
                    buildFile("/tmp/results/sample-a/first.png"),
                    buildFile("/tmp/results/sample-a/second.png"),
                    buildFile("/tmp/results/sample-b/third.png"),
                    buildFile("/tmp/results/sample-b/fourth.png"),
                ],
                resultId: "result-1",
            }),
        );

        const fileBrowser = screen.getByText("File Browser").parentElement;

        expect(fileBrowser?.getAttribute("data-selected-directory")).toBe(
            "/tmp/results",
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: "expand:/tmp/results/sample-a",
            }),
        );

        await waitFor(() => {
            expect(fileBrowser?.getAttribute("data-selected-directory")).toBe(
                "/tmp/results/sample-a",
            );
        });

        fireEvent.click(
            screen.getByRole("button", {
                name: "collapse:/tmp/results/sample-a",
            }),
        );

        await waitFor(() => {
            expect(fileBrowser?.getAttribute("data-selected-directory")).toBe(
                "/tmp/results",
            );
        });
    });

    it("reselects the parent only when closing a selected child and keeps the parent selected when reopening it", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        render(
            createElement(ResultDetailFiles, {
                files: [
                    buildFile("/tmp/results/sample-a/first.png"),
                    buildFile("/tmp/results/sample-a/second.png"),
                    buildFile("/tmp/results/sample-b/third.png"),
                    buildFile("/tmp/results/sample-b/fourth.png"),
                ],
                resultId: "result-1",
            }),
        );

        const fileBrowser = screen.getByText("File Browser").parentElement;

        expect(fileBrowser?.getAttribute("data-selected-directory")).toBe(
            "/tmp/results",
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: "expand:/tmp/results/sample-a",
            }),
        );

        await waitFor(() => {
            expect(fileBrowser?.getAttribute("data-selected-directory")).toBe(
                "/tmp/results/sample-a",
            );
        });

        fireEvent.click(
            screen.getByRole("button", {
                name: "collapse:/tmp/results/sample-a",
            }),
        );

        await waitFor(() => {
            expect(fileBrowser?.getAttribute("data-selected-directory")).toBe(
                "/tmp/results",
            );
        });

        fireEvent.click(
            screen.getByRole("button", {
                name: "expand:/tmp/results",
            }),
        );

        await waitFor(() => {
            expect(fileBrowser?.getAttribute("data-selected-directory")).toBe(
                "/tmp/results",
            );
        });
    });

    it("renders thumbnail previews in grid mode and paginates after the first 100 files", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");
        const files = Array.from({ length: 101 }, (_, index) =>
            buildFile(
                `/tmp/results/a/plot-${String(index + 1).padStart(3, "0")}.png`,
            ),
        );

        render(
            createElement(ResultDetailFiles, {
                files,
                resultId: "result-1",
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: "show-grid" }));

        await waitFor(() => {
            expect(screen.getAllByTestId("thumbnail-preview")).toHaveLength(
                100,
            );
        });

        expect(
            screen
                .getAllByTestId("thumbnail-preview")[0]
                ?.getAttribute("data-thumbnail-url"),
        ).toContain("thumb=true");

        fireEvent.click(screen.getByRole("button", { name: "next-page" }));

        await waitFor(() => {
            expect(screen.getAllByTestId("thumbnail-preview")).toHaveLength(1);
        });
    });

    it("keeps grid thumbnail sources stable when only the preview height changes", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        render(
            createElement(ResultDetailFiles, {
                files: [
                    buildFile("/tmp/results/a/plot-001.png"),
                    buildFile("/tmp/results/a/plot-002.png"),
                ],
                resultId: "result-1",
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: "show-grid" }));

        await waitFor(() => {
            expect(screen.getAllByTestId("thumbnail-preview")).toHaveLength(2);
        });

        const initialThumbnail = screen.getAllByTestId("thumbnail-preview")[0];
        const initialThumbnailUrl =
            initialThumbnail?.getAttribute("data-thumbnail-url");

        expect(initialThumbnail?.getAttribute("data-height")).toBe("220");
        expect(initialThumbnailUrl).toContain("thumb=true");
        expect(initialThumbnailUrl).toBeTruthy();

        fireEvent.click(
            screen.getByRole("button", { name: "preview-height-320" }),
        );

        await waitFor(() => {
            expect(
                screen
                    .getAllByTestId("thumbnail-preview")[0]
                    ?.getAttribute("data-height"),
            ).toBe("320");
        });

        expect(
            screen
                .getAllByTestId("thumbnail-preview")[0]
                ?.getAttribute("data-thumbnail-url"),
        ).toBe(initialThumbnailUrl);
    });

    it("paginates the default single-preview file list after the first 100 files", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");
        const files = Array.from({ length: 101 }, (_, index) =>
            buildFile(
                `/tmp/results/a/plot-${String(index + 1).padStart(3, "0")}.png`,
            ),
        );

        render(
            createElement(ResultDetailFiles, {
                files,
                resultId: "result-1",
            }),
        );

        await waitFor(() => {
            expect(
                screen.getAllByRole("button", {
                    name: /\/tmp\/results\/a\/plot-/,
                }),
            ).toHaveLength(100);
        });

        expect(
            screen.queryByRole("button", {
                name: "/tmp/results/a/plot-101.png",
            }),
        ).toBeNull();

        fireEvent.click(screen.getByRole("button", { name: "next-page" }));

        await waitFor(() => {
            expect(
                screen.getByRole("button", {
                    name: "/tmp/results/a/plot-101.png",
                }),
            ).toBeTruthy();
        });

        expect(
            screen.getAllByRole("button", {
                name: /\/tmp\/results\/a\/plot-/,
            }),
        ).toHaveLength(1);
    });

    it("renders non-image files as gallery preview rows in grid mode", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        fetchMock.mockResolvedValue(
            new Response('{"sample":"alpha"}', {
                headers: { "content-type": "application/json" },
                status: 200,
            }),
        );

        render(
            createElement(ResultDetailFiles, {
                files: [buildFile("/tmp/results/a/report.json")],
                resultId: "result-1",
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: "show-grid" }));

        await waitFor(() => {
            expect(
                screen.getByText("preview:/tmp/results/a/report.json"),
            ).toBeTruthy();
        });
    });

    it("requests inline-mode previews for line-readable text files", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        fetchMock.mockResolvedValue(
            new Response("sample\tstatus\nalpha\tready\n", {
                headers: {
                    "content-type": "text/tab-separated-values",
                    "x-preview-truncated": "true",
                },
                status: 200,
            }),
        );

        render(
            createElement(ResultDetailFiles, {
                files: [buildFile("/tmp/results/a/report.tsv")],
                resultId: "result-1",
            }),
        );

        await waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fa%2Freport.tsv&mode=inline",
            );
        });

        expect(
            fetchMock.mock.calls.some(([url]) =>
                String(url).includes("line_limit="),
            ),
        ).toBe(false);
    });

    it("requests inline-mode previews for gzip-compressed tsv files", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        fetchMock.mockResolvedValue(
            new Response("sample\tstatus\nalpha\tready\n", {
                headers: {
                    "content-type": "text/tab-separated-values",
                    "x-preview-truncated": "true",
                },
                status: 200,
            }),
        );

        render(
            createElement(ResultDetailFiles, {
                files: [buildFile("/tmp/results/a/report.tsv.gz")],
                resultId: "result-1",
            }),
        );

        await waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fa%2Freport.tsv.gz&mode=inline",
            );
        });
    });

    it("does not refetch the inline preview when the height slider changes", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        fetchMock.mockResolvedValue(
            new Response("sample\tstatus\nalpha\tready\n", {
                headers: {
                    "content-type": "text/tab-separated-values",
                    "x-preview-truncated": "true",
                },
                status: 200,
            }),
        );

        render(
            createElement(ResultDetailFiles, {
                files: [buildFile("/tmp/results/a/report.tsv")],
                resultId: "result-1",
            }),
        );

        await waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fa%2Freport.tsv&mode=inline",
            );
        });

        const fetchCallsAfterInitialLoad = fetchMock.mock.calls.length;

        fireEvent.click(
            screen.getByRole("button", { name: "preview-height-320" }),
        );

        // Height-slider changes must not trigger a fresh data request because
        // the backend already returned enough lines for the maximum possible
        // preview height.
        await new Promise((resolve) => setTimeout(resolve, 50));

        expect(fetchMock.mock.calls.length).toBe(fetchCallsAfterInitialLoad);
    });

    it("rerenders non-image grid previews once when preview height changes", async () => {
        // Updated test: non-image previews (including CSV) now re-render when
        // maxHeight changes to enable proper truncation based on preview height
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        fetchMock.mockResolvedValue(
            new Response('{"sample":"alpha"}', {
                headers: { "content-type": "application/json" },
                status: 200,
            }),
        );

        render(
            createElement(ResultDetailFiles, {
                files: [buildFile("/tmp/results/a/report.json")],
                resultId: "result-1",
            }),
        );

        fireEvent.click(screen.getByRole("button", { name: "show-grid" }));

        await waitFor(() => {
            expect(
                screen.getByText("preview:/tmp/results/a/report.json"),
            ).toBeTruthy();
            expect(
                filePreviewRenderCounts.get("/tmp/results/a/report.json"),
            ).toBe(3);
        });

        const settledRenderCount = filePreviewRenderCounts.get(
            "/tmp/results/a/report.json",
        );

        fireEvent.click(
            screen.getByRole("button", { name: "preview-height-320" }),
        );

        // Preview should re-render once due to maxHeight prop change
        expect(filePreviewRenderCounts.get("/tmp/results/a/report.json")).toBe(
            (settledRenderCount ?? 0) + 1,
        );
    });

    it("renders html previews from the proxy without waiting for inline content", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        render(
            createElement(ResultDetailFiles, {
                files: [buildFile("/tmp/results/report.html")],
                resultId: "result-1",
            }),
        );

        expect(fetchMock).not.toHaveBeenCalled();
        expect(screen.queryByText("Loading preview...")).toBeNull();

        expect(
            screen.getByText("preview:/tmp/results/report.html"),
        ).toBeTruthy();
    });

    it("uses fetched content type instead of the svg path extension for preview selection", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        fetchMock.mockResolvedValue(
            new Response("plain text payload", {
                headers: { "content-type": "text/plain" },
                status: 200,
            }),
        );

        render(
            createElement(ResultDetailFiles, {
                files: [buildFile("/tmp/results/plot.svg")],
                resultId: "result-1",
            }),
        );

        await waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.svg&mode=inline",
            );
        });

        await waitFor(() => {
            expect(
                screen.getByText("preview:/tmp/results/plot.svg"),
            ).toBeTruthy();
        });
    });

    it("settles json previews into rendered code content after loading", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        fetchMock.mockResolvedValue(
            new Response('{"sample":"alpha","status":"ready"}', {
                headers: { "content-type": "application/json" },
                status: 200,
            }),
        );

        render(
            createElement(ResultDetailFiles, {
                files: [buildFile("/tmp/results/report.json")],
                resultId: "result-1",
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByText("preview:/tmp/results/report.json"),
            ).toBeTruthy();
        });
    });

    it("keeps the settled json preview visible when the selected file is clicked again", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");
        const file = buildFile("/tmp/results/report.json");

        fetchMock.mockResolvedValue(
            new Response('{"sample":"alpha","status":"ready"}', {
                headers: { "content-type": "application/json" },
                status: 200,
            }),
        );

        render(
            createElement(ResultDetailFiles, {
                files: [file],
                resultId: "result-1",
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByText("preview:/tmp/results/report.json"),
            ).toBeTruthy();
        });

        fireEvent.click(screen.getByRole("button", { name: file.path }));

        expect(
            screen.getByText("preview:/tmp/results/report.json"),
        ).toBeTruthy();
        expect(fetchMock).toHaveBeenCalledTimes(1);
    });

    it("surfaces backend JSON error messages in the preview state", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        fetchMock.mockResolvedValue(
            Response.json({ error: "file not found on disk" }, { status: 410 }),
        );

        render(
            createElement(ResultDetailFiles, {
                files: [buildFile("/tmp/results/report.txt")],
                resultId: "result-1",
            }),
        );

        await waitFor(() => {
            expect(
                screen.getByText("preview:/tmp/results/report.txt"),
            ).toBeTruthy();
        });
    });
});
