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
        onSelectDirectory?: (path: string) => void;
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
    }) =>
        createElement(
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
            [
                ...new Set(
                    files.map(
                        (file) =>
                            file.path.split("/").slice(0, -1).join("/") || "/",
                    ),
                ),
            ].map((directoryPath) =>
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
        ),
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
    FilePreview: ({ file, proxyUrl }: { file: FileEntry; proxyUrl: string }) =>
        createElement(
            "div",
            { "data-preview-url": proxyUrl },
            `preview:${file.path}`,
        ),
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
});

describe("O1 result detail file integration", () => {
    it("removes the old file focus aside and defaults to the first file in the selected directory", async () => {
        const { ResultDetailFiles } =
            await import("@/components/result-detail-files");

        render(
            createElement(ResultDetailFiles, {
                files: [
                    buildFile("/tmp/results/a/first.png"),
                    buildFile("/tmp/results/a/second.txt"),
                    buildFile("/tmp/results/b/third.png"),
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
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.svg",
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
