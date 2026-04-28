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
        expect(container.textContent).toContain("001.png");
        expect(container.textContent).toContain("002.png");
        expect(container.textContent).not.toContain("003.png");
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

    it("surfaces the selected directory and preview controls", async () => {
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

        expect(container.textContent).toContain("1 preview per row");
        expect(container.textContent).toContain("Preview height");
        expect(container.textContent).toContain("Page 2 of 3");
    });

    it("paginates the file list in single preview mode", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = Array.from({ length: 101 }, (_, index) =>
            buildFile(
                `/results/plot-${String(index + 1).padStart(3, "0")}.png`,
                "output",
            ),
        );
        const handlePreviewPageChange = vi.fn();

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewPageChange: handlePreviewPageChange,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    previewPage: 1,
                    previewPageCount: 2,
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files.slice(0, 100),
                }),
            );
        });

        expect(container.textContent).toContain("Page 1 of 2");
        expect(
            container.querySelector(
                'button[data-file-path="/results/plot-100.png"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                'button[data-file-path="/results/plot-101.png"]',
            ),
        ).toBeNull();

        await click(
            Array.from(container.querySelectorAll("button")).find(
                (button) => button.textContent === "Next",
            ) ?? null,
        );

        expect(handlePreviewPageChange).toHaveBeenCalledWith(2);
    });
});
