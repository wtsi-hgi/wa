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

    it("shows separate trees for outputs and inputs tabs", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/out/a/1.txt", "output"),
                        buildFile("/out/a/2.txt", "output"),
                        buildFile("/in/b.fastq", "input"),
                    ],
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(container.textContent).toContain("Outputs");
        expect(container.textContent).toContain("out");
        expect(container.textContent).toContain("a");
        expect(container.textContent).toContain("2 txt");

        await click(container.querySelector('button[role="tab"][value="input"]'));

        expect(container.textContent).toContain("in");
        expect(container.textContent).toContain("1 fastq");
    });

    it("shows an empty state for pipeline files when that tab has no entries", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [buildFile("/out/report.txt", "output")],
                    onSelectFile: vi.fn(),
                }),
            );
        });

        await click(
            container.querySelector('button[role="tab"][value="pipeline"]'),
        );

        expect(container.textContent).toContain("No pipeline files");
    });

    it("builds a tree with aggregated file counts for child folders", async () => {
        const { buildFileTree } = await import("@/components/file-browser");

        const tree = buildFileTree([
            buildFile("/out/a/1.csv", "output"),
            buildFile("/out/a/2.csv", "output"),
            buildFile("/out/a/3.png", "output"),
            buildFile("/out/b/4.txt", "output"),
        ]);

        expect(tree).toHaveLength(1);
        expect(tree[0]?.name).toBe("out");
        expect(tree[0]?.children).toHaveLength(2);
        expect(tree[0]?.children[0]?.fileCount).toBe(3);
        expect(tree[0]?.children[1]?.fileCount).toBe(1);
    });

    it("auto-expands a single root folder on first render", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [buildFile("/results/report.txt", "output")],
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(container.textContent).toContain("report.txt");
    });

    it("calls onSelectFile with the clicked file entry", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handleSelectFile = vi.fn();
        const file = buildFile("/results/report.txt", "output");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [file],
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
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(container.textContent).toContain("1.0 MB");
    });

    it("buildFileTree counts file types within a folder", async () => {
        const { buildFileTree } = await import("@/components/file-browser");

        const tree = buildFileTree([
            buildFile("/out/a/1.csv", "output"),
            buildFile("/out/a/2.csv", "output"),
            buildFile("/out/a/3.png", "output"),
        ]);

        expect(tree[0]?.children[0]?.typeCounts).toEqual({ csv: 2, png: 1 });
    });

    it("aggregates file type counts from all descendant folders", async () => {
        const { buildFileTree } = await import("@/components/file-browser");

        const tree = buildFileTree([
            buildFile("/out/a/1.csv", "output"),
            buildFile("/out/a/2.csv", "output"),
            buildFile("/out/a/3.csv", "output"),
            buildFile("/out/a/4.png", "output"),
            buildFile("/out/b/5.txt", "output"),
            buildFile("/out/b/6.txt", "output"),
        ]);

        expect(tree[0]?.typeCounts).toEqual({ csv: 3, png: 1, txt: 2 });
    });

    it("displays a folder type summary beside the folder name", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/out/a/1.csv", "output"),
                        buildFile("/out/a/2.csv", "output"),
                        buildFile("/out/a/3.csv", "output"),
                        buildFile("/out/a/4.png", "output"),
                    ],
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(container.textContent).toContain("3 csv, 1 png");
    });
});
