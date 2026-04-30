// @vitest-environment jsdom

import { createElement, type ReactNode } from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { FileEntry } from "@/lib/contracts";

function buildFile(
    path: string,
    kind: FileEntry["kind"],
    size = 512,
    mtime = "2026-04-16T09:15:00Z",
): FileEntry {
    return {
        kind,
        mtime,
        path,
        size,
    };
}

async function click(target: Element | null): Promise<void> {
    if (!(target instanceof HTMLElement)) {
        throw new Error("Expected clickable HTMLElement");
    }

    await act(async () => {
        target.click();
    });
}

describe("N1 file browser", () => {
    let container: HTMLDivElement;
    let root: Root;

    beforeEach(() => {
        container = document.createElement("div");
        document.body.appendChild(container);
        root = createRoot(container);
    });

    afterEach(async () => {
        await act(async () => {
            root.unmount();
        });
        container.remove();
    });

    it("renders a single tree-view pane with expandable directory rows", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/out/project/run/1.txt", "output"),
                        buildFile("/out/project/run/2.txt", "output"),
                        buildFile("/out/project/other/3.txt", "output"),
                        buildFile("/in/b.fastq", "input"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file
                                ? `preview:${file.path.split("/").pop()}`
                                : "none",
                        ),
                }),
            );
        });

        expect(container.textContent).toContain("File Browser");
        expect(container.textContent).not.toContain("Folders");
        expect(
            container.querySelector(
                'button[data-directory-path="/out/project"]',
            ),
        ).toBeTruthy();
        expect(container.textContent).toContain("3.txt");
        expect(container.textContent).not.toContain("1.txt");
        expect(container.textContent).not.toContain("2.txt");

        await click(
            container.querySelector(
                'button[data-directory-path="/out/project"]',
            ),
        );

        expect(
            container.querySelector(
                'button[data-directory-path="/out/project/run"]',
            ),
        ).toBeNull();
        expect(container.textContent).not.toContain("1.txt");
        expect(container.textContent).not.toContain("2.txt");

        await click(
            container.querySelector(
                'button[data-directory-path="/out/project"]',
            ),
        );

        expect(
            container.querySelector(
                'button[data-directory-path="/out/project/run"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                'button[data-directory-path="/out/project/other"]',
            ),
        ).toBeTruthy();
        expect(container.textContent).not.toContain("1.txt");
        expect(container.textContent).not.toContain("2.txt");

        await click(
            container.querySelector(
                'button[data-directory-path="/out/project/run"]',
            ),
        );

        expect(container.textContent).toContain("1.txt");
        expect(container.textContent).toContain("2.txt");
        expect(container.textContent).not.toContain("3.txt");

        await click(
            container.querySelector(
                'button[data-directory-path="/out/project/run"]',
            ),
        );

        expect(
            container.querySelector(
                'button[data-file-path="/out/project/run/1.txt"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                'button[data-file-path="/out/project/run/2.txt"]',
            ),
        ).toBeNull();
    });

    it("renders grid previews beside the current page of file rows", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/a/001.png", "output"),
            buildFile("/results/a/002.png", "output"),
            buildFile("/results/a/003.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onPreviewPageChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "grid",
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "grid-preview" },
                            `preview:${file.path}`,
                        ),
                    visibleFiles: files.slice(0, 2),
                }),
            );
        });

        expect(
            container.querySelectorAll('[data-testid="grid-preview"]'),
        ).toHaveLength(2);
        expect(container.textContent).toContain("preview:/results/a/001.png");
        expect(container.textContent).toContain("preview:/results/a/002.png");
        expect(container.textContent).not.toContain(
            "preview:/results/a/003.png",
        );
        const firstGridRow = container.querySelector(
            '[data-file-browser-grid-row="/results/a/001.png"]',
        );

        expect(firstGridRow).toBeTruthy();
        expect(
            firstGridRow?.querySelector(
                'button[data-file-path="/results/a/001.png"]',
            ),
        ).toBeTruthy();
        expect(
            firstGridRow?.querySelector(
                '[data-grid-preview-path="/results/a/001.png"]',
            ),
        ).toBeTruthy();
        expect(container.textContent).toContain("001.png");
        expect(container.textContent).toContain("002.png");
        expect(container.textContent).not.toContain("003.png");
    });

    it("renders grid-mode previews beside the file row at all screen widths (no xl: breakpoint)", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/a/001.png", "output"),
            buildFile("/results/a/002.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onPreviewPageChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "grid",
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "grid-preview" },
                            `preview:${file.path}`,
                        ),
                    visibleFiles: files,
                }),
            );
        });

        const gridRows = container.querySelectorAll(
            "[data-file-browser-grid-row]",
        );

        expect(gridRows.length).toBeGreaterThan(0);

        for (const row of gridRows) {
            // The per-file grid row must use unconditional side-by-side
            // grid-cols, not the xl:-prefixed variant which only applies at
            // wide viewport widths and incorrectly stacks the preview
            // underneath at standard viewport widths.
            expect(row.className).toContain("grid");
            expect(row.className).toMatch(/(?:^|\s)grid-cols-\[minmax/);
            expect(row.className).not.toMatch(/xl:grid-cols-/);
            expect(row.className).not.toMatch(/xl:items-start/);
        }
    });

    it("does not wrap grid previews in extra bordered padded containers or duplicate file size", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const { FileImageThumbnail } =
            await import("@/components/file-preview");
        const file = buildFile("/results/a/plot.png", "output", 1048576);

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [file],
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "grid",
                    renderGridPreview: (entry: FileEntry): ReactNode =>
                        createElement(FileImageThumbnail, {
                            file: entry,
                            fullSizeUrl: `/api/file?path=${encodeURIComponent(entry.path)}`,
                            height: 180,
                            thumbnailUrl: `/api/file?path=${encodeURIComponent(entry.path)}&thumb=true&w=320&h=180`,
                        }),
                    visibleFiles: [file],
                }),
            );
        });

        const gridRow = container.querySelector(
            '[data-file-browser-grid-row="/results/a/plot.png"]',
        ) as HTMLElement | null;
        const previewCell = container.querySelector(
            '[data-grid-preview-path="/results/a/plot.png"]',
        ) as HTMLElement | null;
        const thumbnailButton = container.querySelector(
            'button[aria-label="Open image lightbox"]',
        ) as HTMLElement | null;
        const sizeOccurrences =
            gridRow?.textContent?.match(/1\.0 MB/g)?.length ?? 0;

        expect(gridRow).toBeTruthy();
        expect(previewCell).toBeTruthy();
        expect(thumbnailButton).toBeTruthy();
        expect(gridRow?.className).not.toMatch(
            /(?:^|\s)(rounded(?:-|\[)|border(?:\s|-)|bg-[^\s]+|p[xytrbl]?-[^\s]+)/,
        );
        expect(previewCell?.className).not.toMatch(
            /(?:^|\s)(rounded(?:-|\[)|border(?:\s|-)|bg-[^\s]+|p[xytrbl]?-[^\s]+)/,
        );
        expect(thumbnailButton?.className).not.toMatch(
            /(?:^|\s)(border(?:\s|-)|p[xytrbl]?-[^\s]+)/,
        );
        expect(sizeOccurrences).toBe(1);
    });

    it("keeps the file browser to a single explorer pane without duplicate path sections", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/gallery/001.png", "output"),
            buildFile("/results/gallery/002.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file
                                ? `preview:${file.path.split("/").pop()}`
                                : "none",
                        ),
                }),
            );
        });

        expect(container.textContent).toContain("File Browser");
        expect(container.textContent).not.toContain("Explorer");
        expect(container.textContent).not.toContain("Preview focus");
        expect(container.textContent).not.toContain("/results/gallery/001.png");
        expect(container.textContent).not.toContain("/results/gallery/002.png");
        expect(container.textContent).toContain("001.png");
        expect(container.textContent).toContain("002.png");
        expect(
            container.querySelector('[data-file-browser-preview="single"]'),
        ).toBeTruthy();
    });

    it("renders the single preview inside the selected directory file box", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/gallery/001.png", "output"),
            buildFile("/results/gallery/002.png", "output"),
            buildFile("/results/gallery/003.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                }),
            );
        });

        const directoryFiles = container.querySelector(
            '[data-file-browser-directory-files="/results/gallery"]',
        );
        const preview = container.querySelector(
            '[data-file-browser-preview="single"]',
        );
        const singleLayout = container.querySelector(
            '[data-file-browser-single-layout="/results/gallery"]',
        );

        expect(directoryFiles).toBeTruthy();
        expect(preview).toBeTruthy();
        expect(directoryFiles?.contains(preview ?? null)).toBe(true);
        expect(singleLayout).toBeTruthy();
        expect(singleLayout?.contains(preview ?? null)).toBe(true);
        expect((preview as HTMLElement | null)?.style.gridRow).toBe(
            "1 / span 3",
        );
    });

    it("renders single preview to the right of file metadata in a two-column grid at all screen widths", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/images/photo1.png", "output"),
            buildFile("/results/images/photo2.png", "output"),
            buildFile("/results/images/photo3.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                }),
            );
        });

        const singleLayout = container.querySelector(
            '[data-file-browser-single-layout="/results/images"]',
        ) as HTMLElement;
        const preview = container.querySelector(
            '[data-file-browser-preview="single"]',
        ) as HTMLElement;
        const fileButtons = container.querySelectorAll(
            '[data-file-path^="/results/images/"]',
        );

        expect(singleLayout).toBeTruthy();
        expect(preview).toBeTruthy();
        expect(fileButtons).toHaveLength(3);

        // Verify layout box has grid classes WITHOUT xl: prefix (applies at all widths)
        expect(singleLayout.className).toContain("grid");
        expect(singleLayout.className).toMatch(/grid-cols-\[minmax/);
        expect(singleLayout.className).not.toMatch(/xl:grid-cols-/);

        // Verify all file buttons are direct children of the grid
        fileButtons.forEach((button) => {
            expect(button.parentElement).toBe(singleLayout);
        });

        // Verify preview is a direct child of the grid
        expect(preview.parentElement).toBe(singleLayout);

        // Verify preview row-spans all file rows
        expect(preview.style.gridRow).toBe("1 / span 3");

        // Verify preview starts at column 2 (no xl: prefix, applies at all widths)
        expect(preview.className).toContain("col-start-2");
        expect(preview.className).not.toMatch(/xl:col-start-/);
    });

    it("does not add a second bordered box around the single preview panel", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/images/photo1.png", "output"),
            buildFile("/results/images/photo2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "section",
                            { "data-testid": "single-preview-surface" },
                            file?.path ?? "none",
                        ),
                }),
            );
        });

        const previewPanel = container.querySelector(
            '[data-file-browser-preview="single"]',
        ) as HTMLElement | null;

        expect(previewPanel).toBeTruthy();
        expect(previewPanel?.className).toContain("sticky");
        expect(previewPanel?.className).toContain("top-4");
        expect(previewPanel?.className).toContain("self-start");
        expect(previewPanel?.className).not.toMatch(/(?:^|\s)border(?:\s|$)/);
        expect(previewPanel?.className).not.toMatch(/(?:^|\s)p-\d/);
        expect(previewPanel?.className).not.toMatch(/(?:^|\s)bg-/);
    });

    it("shows an empty state when there are no registered files", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(container.textContent).toContain("No registered files");
    });

    it("compresses single-child directory chains into tree nodes", async () => {
        const { buildDirectoryTree } =
            await import("@/components/file-browser");

        const tree = buildDirectoryTree([
            buildFile("/out/project/run/1.csv", "output"),
            buildFile("/out/project/run/2.csv", "output"),
            buildFile("/out/project/other/3.png", "output"),
            buildFile("/in/raw/4.txt", "input"),
        ]);

        expect(tree.map((node) => node.path)).toEqual([
            "/out/project",
            "/in/raw",
        ]);
        expect(tree[0]?.label).toBe("out/project");
        expect(tree[0]?.children.map((node) => node.path)).toEqual([
            "/out/project/other",
            "/out/project/run",
        ]);
        expect(tree[0]?.children[1]?.fileCount).toBe(2);
    });

    it("tracks recursive descendant totals for directory tree summaries", async () => {
        const { buildDirectoryTree } =
            await import("@/components/file-browser");

        const tree = buildDirectoryTree([
            buildFile("/out/project/run/1.csv", "output"),
            buildFile("/out/project/run/deep/2.csv", "output"),
            buildFile("/out/project/other/3.png", "output"),
            buildFile("/out/project/other/leaf/4.txt", "output"),
        ]);

        expect(tree[0]?.path).toBe("/out/project");
        expect(tree[0]?.fileCount).toBe(0);
        expect(tree[0]?.children.map((node) => node.path)).toEqual([
            "/out/project/other",
            "/out/project/run",
        ]);
        expect(tree[0]?.descendantFileCount).toBe(4);
        expect(tree[0]?.descendantDirectoryCount).toBe(4);
        expect(tree[0]?.children[0]?.descendantFileCount).toBe(2);
        expect(tree[0]?.children[0]?.descendantDirectoryCount).toBe(1);
        expect(tree[0]?.children[1]?.descendantFileCount).toBe(2);
        expect(tree[0]?.children[1]?.descendantDirectoryCount).toBe(1);
    });

    it("renders recursive file and subfolder totals in directory rows", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/out/project/run/1.csv", "output"),
                        buildFile("/out/project/run/deep/2.csv", "output"),
                        buildFile("/out/project/other/3.png", "output"),
                        buildFile("/out/project/other/leaf/4.txt", "output"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(
            container.querySelector(
                'button[data-directory-path="/out/project"]',
            )?.textContent,
        ).toContain("4 files");
        expect(
            container.querySelector(
                'button[data-directory-path="/out/project"]',
            )?.textContent,
        ).toContain("4 subfolders");
    });

    it("retains the root directory when files live directly under slash", async () => {
        const { buildDirectoryTree } =
            await import("@/components/file-browser");

        const tree = buildDirectoryTree([
            buildFile("/report.csv", "output"),
            buildFile("/nested/image.png", "output"),
        ]);

        expect(tree.map((node) => node.path)).toEqual(["/"]);
        expect(tree[0]?.fileCount).toBe(1);
        expect(tree[0]?.files.map((file) => file.path)).toEqual([
            "/report.csv",
        ]);
        expect(tree[0]?.children.map((node) => node.path)).toEqual(["/nested"]);
    });

    it("selects the first directory and first file on first render", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handleSelectDirectory = vi.fn();
        const handleSelectFile = vi.fn();

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/results/report.txt", "output"),
                        buildFile("/results/report-2.txt", "output"),
                    ],
                    onSelectDirectory: handleSelectDirectory,
                    onSelectFile: handleSelectFile,
                }),
            );
        });

        expect(handleSelectDirectory).toHaveBeenCalledWith("/results");
        expect(handleSelectFile).toHaveBeenCalledWith(
            expect.objectContaining({ path: "/results/report.txt" }),
        );
    });

    it("calls onSelectFile with the clicked file entry", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handleSelectFile = vi.fn();
        const file = buildFile("/results/report.txt", "output");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [file],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: handleSelectFile,
                }),
            );
        });

        await click(
            container.querySelector(
                'button[data-file-path="/results/report.txt"]',
            ),
        );

        expect(handleSelectFile).toHaveBeenCalledWith(file);
    });

    it("renders human-readable file sizes", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/results/report.txt", "output", 1048576),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(container.textContent).toContain("1.0 MB");
    });

    it("keeps directory summaries ordered by path", async () => {
        const { buildDirectoryGroups } =
            await import("@/components/file-browser");

        const groups = buildDirectoryGroups([
            buildFile("/out/z/1.csv", "output"),
            buildFile("/out/a/2.csv", "output"),
            buildFile("/out/m/3.png", "output"),
        ]);

        expect(groups.map((group) => group.path)).toEqual([
            "/out/a",
            "/out/m",
            "/out/z",
        ]);
    });

    it("surfaces preview height without putting paging controls in the browser header", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/results/plot-001.png", "output"),
                        buildFile("/results/plot-002.png", "output"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewHeight: 180,
                    previewMode: "grid",
                    previewPage: 2,
                    previewPageCount: 3,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onPreviewPageChange: vi.fn(),
                }),
            );
        });

        expect(container.textContent).toContain("Preview height");
        const header = container.querySelector("[data-file-browser-header]");

        expect(header?.textContent).not.toContain("1 preview per row");
        expect(header?.textContent).not.toContain("Page 2 of 3");
    });

    it("keeps preview height drag updates local until the slider is committed", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handlePreviewHeightChange = vi.fn();
        const renderGridPreview = vi.fn(
            (file: FileEntry): ReactNode =>
                createElement(
                    "div",
                    { "data-testid": `grid-preview-${file.path}` },
                    `preview:${file.path}`,
                ),
        );
        const files = [
            buildFile("/results/plot-001.png", "output"),
            buildFile("/results/plot-002.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: handlePreviewHeightChange,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewHeight: 220,
                    previewMode: "grid",
                    renderGridPreview,
                    visibleFiles: files,
                }),
            );
        });

        expect(renderGridPreview).toHaveBeenCalledTimes(2);

        const slider = container.querySelector(
            'input[aria-label="Preview height"]',
        );

        expect(slider).toBeTruthy();

        await act(async () => {
            const range = slider as HTMLInputElement;

            range.value = "260";
            range.dispatchEvent(new Event("input", { bubbles: true }));
            range.value = "300";
            range.dispatchEvent(new Event("input", { bubbles: true }));
        });

        expect(container.textContent).toContain("300px");
        expect(handlePreviewHeightChange).not.toHaveBeenCalled();
        expect(renderGridPreview).toHaveBeenCalledTimes(2);

        await act(async () => {
            slider?.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
        });

        expect(handlePreviewHeightChange).toHaveBeenCalledTimes(1);
        expect(handlePreviewHeightChange).toHaveBeenCalledWith(300);
    });

    it("renders paging and preview-mode controls on the expanded folder row and below the file list", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = Array.from({ length: 250 }, (_, index) =>
            buildFile(
                `/results/plot-${String(index + 1).padStart(3, "0")}.png`,
                "output",
            ),
        );
        const handlePreviewPageChange = vi.fn();
        const handlePreviewModeChange = vi.fn();

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewModeChange: handlePreviewModeChange,
                    onPreviewPageChange: handlePreviewPageChange,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    previewPage: 2,
                    previewPageCount: 3,
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files.slice(100, 200),
                }),
            );
        });

        const folderControls = container.querySelector(
            '[data-file-browser-folder-controls="/results"]',
        );
        const bottomControls = container.querySelector(
            '[data-file-browser-bottom-controls="/results"]',
        );

        expect(folderControls).toBeTruthy();
        expect(bottomControls).toBeTruthy();
        expect(folderControls?.textContent).toContain("1 preview per row");
        expect(folderControls?.textContent).toContain("Page 2 of 3");
        expect(bottomControls?.textContent).toContain("Page 2 of 3");
        expect(
            container.querySelector("[data-file-browser-header]")?.textContent,
        ).not.toContain("Page 2 of 3");
        expect(
            container.querySelector(
                'button[data-file-path="/results/plot-101.png"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                'button[data-file-path="/results/plot-001.png"]',
            ),
        ).toBeNull();

        await click(
            folderControls?.querySelector(
                'button[aria-label="Next preview page"]',
            ) ?? null,
        );

        expect(handlePreviewPageChange).toHaveBeenCalledWith(3);

        await click(
            bottomControls?.querySelector(
                'button[aria-label="Previous preview page"]',
            ) ?? null,
        );

        expect(handlePreviewPageChange).toHaveBeenCalledWith(1);

        await click(
            folderControls?.querySelector(
                'input[aria-label="1 preview per row"]',
            ) ?? null,
        );

        expect(handlePreviewModeChange).toHaveBeenCalledWith("grid");

        await act(async () => {
            const pageSelect = folderControls?.querySelector(
                'select[aria-label="Preview page"]',
            ) as HTMLSelectElement | null;

            if (!pageSelect) {
                throw new Error("Missing preview page selector");
            }

            pageSelect.value = "3";
            pageSelect.dispatchEvent(new Event("change", { bubbles: true }));
        });

        expect(handlePreviewPageChange).toHaveBeenCalledWith(3);
    });

    it("hides folder paging controls until the previewable folder is expanded", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/alpha/first.txt", "output"),
                        buildFile("/results/a/plot-001.png", "output"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "grid",
                    previewPage: 1,
                    previewPageCount: 2,
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement("div", {}, file.path),
                    visibleFiles: [
                        buildFile("/results/a/plot-001.png", "output"),
                    ],
                }),
            );
        });

        expect(
            container.querySelector(
                '[data-file-browser-folder-controls="/results/a"]',
            ),
        ).toBeNull();

        await click(
            container.querySelector('button[data-directory-path="/results/a"]'),
        );

        expect(
            container.querySelector(
                '[data-file-browser-folder-controls="/results/a"]',
            ),
        ).toBeTruthy();
    });

    it("renders file buttons and preview as direct grid siblings in single mode", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/plot-001.png", "output"),
            buildFile("/results/plot-002.png", "output"),
            buildFile("/results/plot-003.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                }),
            );
        });

        const gridContainer = container.querySelector(
            '[data-file-browser-single-layout="/results"]',
        );
        const preview = container.querySelector(
            '[data-file-browser-preview="single"]',
        );

        expect(gridContainer).toBeTruthy();
        expect(preview).toBeTruthy();

        // Preview must be a direct child of the grid container
        expect(preview?.parentElement).toBe(gridContainer);

        // All file buttons must be direct children of the grid container
        const fileButtons = container.querySelectorAll(
            'button[data-file-path^="/results/plot-"]',
        );

        expect(fileButtons).toHaveLength(3);

        for (const button of fileButtons) {
            // Each file button must be a direct child of the grid container,
            // not wrapped in an intermediate div
            expect(button.parentElement).toBe(gridContainer);
        }

        // The grid container should have grid classes WITHOUT xl: prefix (applies at all widths)
        expect(gridContainer?.className).toContain("grid");
        expect(gridContainer?.className).toMatch(/grid-cols-\[minmax/);
        expect(gridContainer?.className).not.toMatch(/xl:grid-cols-/);

        // Preview should row-span all file rows
        const previewElement = preview as HTMLElement;
        expect(previewElement.style.gridRow).toBe("1 / span 3");

        // Verify DOM structure: file buttons and preview are siblings
        const gridChildren = Array.from(gridContainer?.children ?? []);
        const buttonIndices = Array.from(fileButtons).map((btn) =>
            gridChildren.indexOf(btn),
        );
        const previewIndex = gridChildren.indexOf(preview ?? document.body);

        expect(buttonIndices.every((i) => i >= 0)).toBe(true);
        expect(previewIndex).toBeGreaterThanOrEqual(0);

        // All should be siblings (children of the same container)
        expect(
            buttonIndices.every((i) =>
                gridChildren.includes(fileButtons[i] as Element),
            ),
        ).toBe(true);
    });
});
