// @vitest-environment jsdom

import { createElement } from "react";
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

    it("groups files by parent directory and flattens file rows within the selected directory", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/out/a/1.txt", "output"),
                        buildFile("/out/a/2.txt", "output"),
                        buildFile("/out/b/3.txt", "output"),
                        buildFile("/in/b.fastq", "input"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(container.textContent).toContain("Directories");
        expect(container.textContent).toContain("/out/a");
        expect(container.textContent).toContain("2 files");
        expect(container.textContent).toContain("1.txt");
        expect(container.textContent).toContain("2.txt");
        expect(container.textContent).not.toContain("/out/b/3.txt");

        await click(container.querySelector('button[data-directory-path="/out/b"]'));

        expect(container.textContent).toContain("3.txt");
        expect(container.textContent).not.toContain("1.txt");
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

    it("builds flat directory summaries instead of a nested tree", async () => {
        const { buildDirectoryGroups } = await import(
            "@/components/file-browser"
        );

        const groups = buildDirectoryGroups([
            buildFile("/out/a/1.csv", "output"),
            buildFile("/out/a/2.csv", "output"),
            buildFile("/out/a/3.png", "output"),
            buildFile("/out/b/4.txt", "output"),
        ]);

        expect(groups).toHaveLength(2);
        expect(groups[0]?.path).toBe("/out/a");
        expect(groups[0]?.fileCount).toBe(3);
        expect(groups[0]?.typeCounts).toEqual({ csv: 2, png: 1 });
        expect(groups[0]?.files.map((file) => file.path)).toEqual([
            "/out/a/1.csv",
            "/out/a/2.csv",
            "/out/a/3.png",
        ]);
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
            container.querySelector('button[data-file-path="/results/report.txt"]'),
        );

        expect(handleSelectFile).toHaveBeenCalledWith(file);
    });

    it("renders human-readable file sizes", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [buildFile("/results/report.txt", "output", 1048576)],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(container.textContent).toContain("1.0 MB");
    });

    it("keeps directory summaries ordered by path", async () => {
        const { buildDirectoryGroups } = await import(
            "@/components/file-browser"
        );

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

        expect(container.textContent).toContain("Preview first 100 files");
        expect(container.textContent).toContain("Preview height");
        expect(container.textContent).toContain("Page 2 of 3");
    });
});
