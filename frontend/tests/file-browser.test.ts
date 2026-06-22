// @vitest-environment jsdom

import { createElement, type ReactNode, useState } from "react";
import { act } from "react";
import { createRoot, hydrateRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { fireEvent, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { FileEntry, ResultSet } from "@/lib/contracts";
import { previewFileTypeOptions } from "@/lib/preview-file-types";

const { toastErrorMock, toastSuccessMock } = vi.hoisted(() => ({
    toastErrorMock: vi.fn(),
    toastSuccessMock: vi.fn(),
}));

vi.mock("sonner", () => ({
    toast: {
        error: toastErrorMock,
        success: toastSuccessMock,
    },
}));

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

function buildResultSet(pipelineName: string, index: number): ResultSet {
    const day = String((index % 9) + 1).padStart(2, "0");

    return {
        command: `nextflow run workflow-${index}.nf`,
        created_at: `2026-04-${day}T10:00:00Z`,
        id: `result-${index}`,
        metadata: {
            seqmeta_sampleid: `SANG${index}`,
        },
        operator: "operator-1",
        output_directory: `/demo/pipeline-${index}`,
        pipeline_identifier: `gh://repo/workflow-${index}.nf`,
        pipeline_name: pipelineName,
        pipeline_version: `1.${index}.0`,
        requester: "alice",
        run_key: `runid=${1000 + index}`,
        updated_at: `2026-04-${day}T10:30:00Z`,
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
        window.localStorage.clear();
        root = createRoot(container);
    });

    afterEach(async () => {
        await act(async () => {
            root.unmount();
        });
        container.remove();
        vi.clearAllMocks();
        vi.unstubAllGlobals();
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

    it("collapses a sibling leaf folder chevron when selection moves to another sibling", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/results/alpha/one.txt", "output"),
                        buildFile("/results/beta/two.txt", "output"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        const alphaButton = () =>
            container.querySelector(
                'button[data-directory-path="/results/alpha"]',
            );
        const betaButton = () =>
            container.querySelector(
                'button[data-directory-path="/results/beta"]',
            );

        expect(alphaButton()?.getAttribute("data-directory-expanded")).toBe(
            "true",
        );
        expect(betaButton()?.getAttribute("data-directory-expanded")).toBe(
            "false",
        );
        expect(
            container.querySelector(
                'button[data-file-path="/results/alpha/one.txt"]',
            ),
        ).toBeTruthy();

        await click(betaButton());

        expect(
            container.querySelector(
                'button[data-file-path="/results/alpha/one.txt"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                'button[data-file-path="/results/beta/two.txt"]',
            ),
        ).toBeTruthy();
        expect(alphaButton()?.getAttribute("data-directory-expanded")).toBe(
            "false",
        );
        expect(betaButton()?.getAttribute("data-directory-expanded")).toBe(
            "true",
        );
    });

    it("renders top-level directory labels with a leading slash", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/results/alpha/one.txt", "output"),
                        buildFile("/results/beta/two.txt", "output"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        const resultsButton = container.querySelector(
            'button[data-directory-path="/results"]',
        );

        expect(resultsButton).toBeTruthy();
        expect(resultsButton?.textContent).toContain("/results");
    });

    it("filters files and keeps only directories containing glob matches", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/demo/reports/alpha-summary.tsv", "output"),
                        buildFile("/demo/logs/alpha-run.log", "output"),
                        buildFile("/demo/metrics/alpha-qc.json", "output"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        const filterInput = container.querySelector(
            'input[aria-label="Filter files by glob"]',
        ) as HTMLInputElement | null;

        expect(filterInput).toBeTruthy();

        await act(async () => {
            if (!filterInput) {
                throw new Error("Missing glob filter input");
            }

            fireEvent.change(filterInput, {
                target: { value: "**/*.tsv" },
            });
        });

        expect(filterInput?.value).toBe("**/*.tsv");
        expect(
            container.querySelector(
                'button[data-directory-path="/demo/reports"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector('button[data-file-path$=".tsv"]'),
        ).toBeTruthy();
        expect(
            container.querySelector('button[data-directory-path="/demo/logs"]'),
        ).toBeNull();
        expect(
            container.querySelector(
                'button[data-directory-path="/demo/metrics"]',
            ),
        ).toBeNull();
        expect(container.textContent).toContain("alpha-summary.tsv");
        expect(container.textContent).not.toContain("alpha-run.log");
        expect(container.textContent).not.toContain("alpha-qc.json");
    });

    it("saves glob filters per storage key and clears when a new key has no saved value", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const storageKey = "wa:file-browser:glob-filter:pipeline-alpha";

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/demo/reports/alpha-summary.tsv", "output"),
                        buildFile("/demo/logs/alpha-run.log", "output"),
                    ],
                    filterStorageKey: "pipeline-alpha",
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        const filterInput = () =>
            container.querySelector(
                'input[aria-label="Filter files by glob"]',
            ) as HTMLInputElement | null;
        const saveButton = () =>
            container.querySelector(
                'button[aria-label="Save file glob filter"]',
            ) as HTMLButtonElement | null;

        expect(filterInput()).toBeTruthy();
        expect(saveButton()).toBeTruthy();

        await act(async () => {
            const input = filterInput();

            if (!input) {
                throw new Error("Missing glob filter input");
            }

            fireEvent.change(input, {
                target: { value: "*.tsv" },
            });
        });
        await click(saveButton());

        expect(window.localStorage.getItem(storageKey)).toBe("*.tsv");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/demo/reports/alpha-summary.tsv", "output"),
                        buildFile("/demo/logs/alpha-run.log", "output"),
                    ],
                    filterStorageKey: "pipeline-beta",
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(filterInput()?.value).toBe("");
        expect(container.textContent).toContain("alpha-run.log");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/demo/reports/alpha-summary.tsv", "output"),
                        buildFile("/demo/logs/alpha-run.log", "output"),
                    ],
                    filterStorageKey: "pipeline-alpha",
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        expect(filterInput()?.value).toBe("*.tsv");
        expect(container.textContent).toContain("alpha-summary.tsv");
        expect(container.textContent).not.toContain("alpha-run.log");
    });

    it("keeps combined-browser glob filter storage keys distinct when pipeline names contain delimiters", async () => {
        const { SearchCombinedFileBrowser } =
            await import("@/components/search-combined-file-browser");
        const files = [
            {
                ...buildFile("/demo/reports/alpha-summary.tsv", "output"),
                resultId: "result-1",
            },
            {
                ...buildFile("/demo/logs/alpha-run.log", "output"),
                resultId: "result-2",
            },
        ];
        const renderCombinedBrowser = async (pipelineNames: string[]) => {
            await act(async () => {
                root.render(
                    createElement(SearchCombinedFileBrowser, {
                        files,
                        mode: "combined",
                        onModeChange: vi.fn(),
                        registrations: pipelineNames.map(
                            (pipelineName, index) => ({
                                fileCount: 1,
                                result: buildResultSet(pipelineName, index + 1),
                            }),
                        ),
                    }),
                );
            });
        };
        const filterInput = () =>
            container.querySelector(
                'input[aria-label="Filter files by glob"]',
            ) as HTMLInputElement | null;
        const saveButton = () =>
            container.querySelector(
                'button[aria-label="Save file glob filter"]',
            ) as HTMLButtonElement | null;

        await renderCombinedBrowser(["a|b", "c"]);

        await act(async () => {
            const input = filterInput();

            if (!input) {
                throw new Error("Missing glob filter input");
            }

            fireEvent.change(input, {
                target: { value: "*.tsv" },
            });
        });
        await click(saveButton());

        await renderCombinedBrowser(["a", "b|c"]);

        expect(filterInput()?.value).toBe("");
        expect(
            container.querySelector('button[data-directory-path="/demo/logs"]'),
        ).toBeTruthy();

        await act(async () => {
            const input = filterInput();

            if (!input) {
                throw new Error("Missing glob filter input");
            }

            fireEvent.change(input, {
                target: { value: "*.log" },
            });
        });
        await click(saveButton());

        const savedGlobFilters = Object.entries(window.localStorage).filter(
            ([key]) => key.startsWith("wa:file-browser:glob-filter:pipelines:"),
        );

        expect(savedGlobFilters).toHaveLength(2);
        expect(savedGlobFilters.map(([key]) => key).sort()[0]).not.toBe(
            savedGlobFilters.map(([key]) => key).sort()[1],
        );
        expect(savedGlobFilters.map(([, value]) => value).sort()).toEqual([
            "*.log",
            "*.tsv",
        ]);
    });

    it("hydrates saved glob filters after the first client render without mismatches", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const hydrationContainer = document.createElement("div");
        const storageScope = "pipeline-alpha";
        const storageKey = "wa:file-browser:glob-filter:pipeline-alpha";
        const files = [
            buildFile("/demo/alpha-summary.tsv", "output"),
            buildFile("/demo/alpha-run.log", "output"),
        ];
        const tree = createElement(FileBrowser, {
            files,
            filterStorageKey: storageScope,
            onSelectDirectory: vi.fn(),
            onSelectFile: vi.fn(),
        });
        const recoverableErrors: unknown[] = [];

        window.localStorage.setItem(storageKey, "*.tsv");
        document.body.appendChild(hydrationContainer);

        const serverMarkup = renderToString(tree);

        expect(serverMarkup).toContain("alpha-summary.tsv");
        expect(serverMarkup).toContain("alpha-run.log");

        hydrationContainer.innerHTML = serverMarkup;

        let hydrationRoot: ReturnType<typeof hydrateRoot> | null = null;

        await act(async () => {
            hydrationRoot = hydrateRoot(hydrationContainer, tree, {
                onRecoverableError: (error) => {
                    recoverableErrors.push(error);
                },
            });
        });

        await waitFor(() => {
            const input = hydrationContainer.querySelector(
                'input[aria-label="Filter files by glob"]',
            ) as HTMLInputElement | null;

            expect(input?.value).toBe("*.tsv");
        });

        expect(hydrationContainer.textContent).toContain("alpha-summary.tsv");
        expect(hydrationContainer.textContent).not.toContain("alpha-run.log");
        expect(recoverableErrors).toHaveLength(0);

        await act(async () => {
            hydrationRoot?.unmount();
        });
        hydrationContainer.remove();
    });

    it("renders expanded nested directory contents inside the directory row box", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/results/root/alpha/summary.txt", "output"),
                        buildFile("/results/root/alpha/sub/plot.png", "output"),
                        buildFile("/results/root/beta/readme.txt", "output"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        const alphaGroup = container.querySelector(
            '[data-directory-group="/results/root/alpha"]',
        );
        const alphaContent = container.querySelector(
            '[data-directory-group-content="/results/root/alpha"]',
        );
        const alphaRow = container.querySelector(
            '[data-directory-row="/results/root/alpha"]',
        );
        const alphaButton = container.querySelector(
            'button[data-directory-path="/results/root/alpha"]',
        ) as HTMLButtonElement | null;
        const alphaFiles = container.querySelector(
            '[data-file-browser-directory-files="/results/root/alpha"]',
        );
        const alphaChildGroup = container.querySelector(
            '[data-directory-group="/results/root/alpha/sub"]',
        );
        const alphaChildButton = container.querySelector(
            'button[data-directory-path="/results/root/alpha/sub"]',
        ) as HTMLButtonElement | null;
        const betaGroup = container.querySelector(
            '[data-directory-group="/results/root/beta"]',
        );

        expect(alphaGroup).toBeTruthy();
        expect(alphaContent).toBeTruthy();
        expect(alphaRow).toBeTruthy();
        expect(alphaButton).toBeTruthy();
        expect(alphaFiles).toBeTruthy();
        expect(alphaChildGroup).toBeTruthy();
        expect(alphaChildButton).toBeTruthy();
        expect(betaGroup).toBeTruthy();
        expect(alphaGroup?.contains(alphaRow ?? null)).toBe(true);
        expect(alphaGroup?.contains(alphaContent ?? null)).toBe(true);
        expect(alphaRow?.contains(alphaContent ?? null)).toBe(true);
        expect(alphaContent?.parentElement).toBe(alphaRow);
        expect(alphaContent?.contains(alphaFiles ?? null)).toBe(true);
        expect(alphaContent?.contains(alphaChildGroup ?? null)).toBe(true);
        expect(alphaContent?.contains(betaGroup ?? null)).toBe(false);
        expect(Number(alphaChildButton?.dataset.depth)).toBe(
            Number(alphaButton?.dataset.depth) + 1,
        );
        expect(alphaChildButton?.style.paddingLeft).toBe(
            alphaButton?.style.paddingLeft,
        );
    });

    it("does not render decorative connector rails beside expanded directory contents", async () => {
        const { FileBrowser } = await import("@/components/file-browser");

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [
                        buildFile("/results/root/alpha/summary.txt", "output"),
                        buildFile("/results/root/alpha/sub/plot.png", "output"),
                    ],
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                }),
            );
        });

        const alphaContent = container.querySelector(
            '[data-directory-group-content="/results/root/alpha"]',
        ) as HTMLElement | null;

        expect(alphaContent).toBeTruthy();

        const directDecorativeChildren = Array.from(
            alphaContent?.children ?? [],
        ).filter(
            (element) =>
                element instanceof HTMLElement &&
                element.getAttribute("aria-hidden") === "true",
        ) as HTMLElement[];

        expect(directDecorativeChildren).toHaveLength(0);
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

    it("copies full file and directory paths without selecting or expanding rows", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const writeTextMock = vi.fn().mockResolvedValue(undefined);
        const handleSelectDirectory = vi.fn();
        const handleSelectFile = vi.fn();
        const file = buildFile("/results/root/report.txt", "output");
        const nestedFile = buildFile(
            "/results/root/nested/table.tsv",
            "output",
        );

        vi.stubGlobal("navigator", {
            clipboard: {
                writeText: writeTextMock,
            },
        });

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files: [file, nestedFile],
                    onSelectDirectory: handleSelectDirectory,
                    onSelectFile: handleSelectFile,
                }),
            );
        });

        handleSelectDirectory.mockClear();
        handleSelectFile.mockClear();

        const fileRow = container.querySelector(
            '[data-file-path="/results/root/report.txt"]',
        );
        const directoryButton = container.querySelector(
            'button[data-directory-path="/results/root/nested"]',
        );
        const directoryCopyButton = container.querySelector(
            '[aria-label="Copy directory full path /results/root/nested"]',
        );
        const fileCopyButton = fileRow?.querySelector(
            '[aria-label="Copy file full path /results/root/report.txt"]',
        );

        expect(fileRow).toBeTruthy();
        expect(directoryButton).toBeTruthy();
        expect(fileCopyButton).toBeTruthy();
        expect(directoryCopyButton).toBeTruthy();
        expect(fileCopyButton).not.toBe(fileRow);
        expect(directoryCopyButton).not.toBe(directoryButton);
        expect(directoryButton?.getAttribute("data-directory-expanded")).toBe(
            "false",
        );

        await click(directoryCopyButton);

        expect(writeTextMock).toHaveBeenCalledWith("/results/root/nested");
        expect(handleSelectDirectory).not.toHaveBeenCalled();
        expect(directoryButton?.getAttribute("data-directory-expanded")).toBe(
            "false",
        );

        await click(fileCopyButton);

        expect(writeTextMock).toHaveBeenCalledWith("/results/root/report.txt");
        expect(handleSelectFile).not.toHaveBeenCalled();
        expect(toastSuccessMock).toHaveBeenCalledWith("Path copied");
        expect(toastErrorMock).not.toHaveBeenCalled();
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

    it("matches glob filters against basenames and nested paths", async () => {
        const { filePathMatchesGlobPattern, filterFilesByGlobPattern } =
            await import("@/lib/file-glob-filter");
        const files = [
            buildFile("/out/reports/alpha-summary.tsv", "output"),
            buildFile("/out/reports/alpha-summary.txt", "output"),
            buildFile("/out/logs/alpha-run.log", "output"),
            buildFile(
                "/lustre/scratch127/gengen/projects_v2/dual_tf/MORF_10/Multiome/sc_analysis_MORF_v2/sc_analysis_BTLNCK/qc_outputs/HAP1_MORF10_BTLNECK2_1_A1_BTLNCK/HAP1_MORF10_BTLNECK2_1_A1_BTLNCK_py_inputs/atac/barcodes.tsv.gz",
                "output",
            ),
        ];

        expect(filePathMatchesGlobPattern(files[0]?.path ?? "", "*.tsv")).toBe(
            true,
        );
        expect(
            filePathMatchesGlobPattern(
                files[3]?.path ?? "",
                "*py_inputs*.tsv.gz",
            ),
        ).toBe(true);
        expect(
            filePathMatchesGlobPattern(
                files[0]?.path ?? "",
                "reports/alpha-[!q]*.tsv",
            ),
        ).toBe(true);
        expect(
            filterFilesByGlobPattern(files, "**/*-summary.t??").map(
                (file) => file.path,
            ),
        ).toEqual([
            "/out/reports/alpha-summary.tsv",
            "/out/reports/alpha-summary.txt",
        ]);
        expect(
            filterFilesByGlobPattern(files, "*py_inputs*.tsv.gz").map(
                (file) => file.path,
            ),
        ).toEqual([files[3]?.path]);
    });

    it("treats unmatched glob brackets as literal characters", async () => {
        const { filePathMatchesGlobPattern, filterFilesByGlobPattern } =
            await import("@/lib/file-glob-filter");
        const files = [
            buildFile("/out/reports/sample[abc.txt", "output"),
            buildFile("/out/reports/sample]abc.txt", "output"),
            buildFile("/out/reports/unrelated.txt", "output"),
        ];

        expect(
            filterFilesByGlobPattern(files, "sample[abc.txt").map(
                (file) => file.path,
            ),
        ).toEqual(["/out/reports/sample[abc.txt"]);
        expect(
            filterFilesByGlobPattern(files, "sample]abc.txt").map(
                (file) => file.path,
            ),
        ).toEqual(["/out/reports/sample]abc.txt"]);
        expect(
            filePathMatchesGlobPattern(
                "/out/reports/unrelated.txt",
                "sample[abc.txt",
            ),
        ).toBe(false);
    });

    it("compiles glob filters once for each file filter operation", async () => {
        const { filterFilesByGlobPattern } =
            await import("@/lib/file-glob-filter");
        const realRegExp = globalThis.RegExp;
        const regexpConstructor = vi.fn(
            (pattern: string, flags?: string): RegExp =>
                new realRegExp(pattern, flags),
        );
        const files = [
            buildFile("/out/reports/alpha-summary.tsv", "output"),
            buildFile("/out/reports/beta-summary.tsv", "output"),
            buildFile("/out/logs/alpha-run.log", "output"),
            buildFile("/out/metrics/alpha-qc.json", "output"),
        ];

        vi.stubGlobal("RegExp", regexpConstructor);

        expect(
            filterFilesByGlobPattern(files, "**/*-summary.tsv").map(
                (file) => file.path,
            ),
        ).toEqual([
            "/out/reports/alpha-summary.tsv",
            "/out/reports/beta-summary.tsv",
        ]);
        expect(regexpConstructor).toHaveBeenCalledTimes(1);

        filterFilesByGlobPattern(files.slice(0, 1), "**/*-summary.tsv");

        expect(regexpConstructor).toHaveBeenCalledTimes(2);
    });

    it("does not show a visible preview height control in the browser controls", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/plot-001.png", "output"),
            buildFile("/results/plot-002.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewHeight: 180,
                    previewMode: "grid",
                    previewPage: 2,
                    previewPageCount: 3,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onPreviewPageChange: vi.fn(),
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

        expect(container.textContent).not.toContain("Preview height");
        expect(
            container.querySelector('input[aria-label="Preview height"]'),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-file-browser-control-trigger="preview-height"]',
            ),
        ).toBeNull();
        const header = container.querySelector("[data-file-browser-header]");

        expect(header?.textContent).not.toContain("1 preview per row");
        expect(header?.textContent).not.toContain("Page 2 of 3");
        expect(header?.textContent).not.toContain("Preview height");

        // Verify controls are in the folder controls section, not the header
        const folderControls = container.querySelector(
            "[data-file-browser-folder-controls]",
        );

        expect(folderControls).toBeTruthy();
        expect(folderControls?.textContent).toContain("1 preview per row");
    });

    it("renders only the selected inline file browser design", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/plot-001.png", "output"),
            buildFile("/results/plot-002.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files,
                }),
            );
        });

        const selector = container.querySelector(
            '[data-file-browser-design-selector="true"]',
        );
        const designButtons = container.querySelectorAll(
            "[data-file-browser-design-option]",
        );
        const browser = container.querySelector("[data-file-browser]");

        expect(selector).toBeNull();
        expect(designButtons).toHaveLength(0);
        expect(browser?.getAttribute("data-file-browser-design")).toBe(
            "inline",
        );
        expect(
            container.querySelector(
                '[data-file-browser-folder-controls="/results"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-file-browser-name-area-controls="/results"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                'button[data-file-path="/results/plot-001.png"]',
            ),
        ).toBeTruthy();
    });

    it("marks preview controls as a distinct surface from directory and file rows", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/plot-001.png", "output"),
            buildFile("/results/plot-002.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files,
                }),
            );
        });

        const folderControls = container.querySelector(
            '[data-file-browser-folder-controls="/results"]',
        ) as HTMLElement | null;
        const directoryRow = container.querySelector(
            '[data-directory-row="/results"]',
        ) as HTMLElement | null;
        const fileButton = container.querySelector(
            'button[data-file-path="/results/plot-001.png"]',
        ) as HTMLElement | null;

        expect(folderControls).toBeTruthy();
        expect(directoryRow).toBeTruthy();
        expect(fileButton).toBeTruthy();
        expect(folderControls?.dataset.fileBrowserControlSurface).toBe("true");
        expect(folderControls?.dataset.fileBrowserControlStyle).toBeTruthy();
        expect(folderControls?.className).not.toBe(directoryRow?.className);
        expect(folderControls?.className).not.toBe(fileButton?.className);
        expect(folderControls?.dataset.fileBrowserControlStyle).toBe(
            "inline-nameplate",
        );
        expect(
            folderControls?.querySelector(
                '[data-file-browser-control-trigger="preview-modes"]',
            ),
        ).toBeTruthy();
        expect(
            folderControls?.querySelector(
                '[data-file-browser-control-trigger="file-types"]',
            ),
        ).toBeTruthy();
        expect(
            folderControls?.querySelector(
                '[data-file-browser-control-current="preview-modes"]',
            ),
        ).toHaveProperty("textContent", "Single preview");
        expect(
            folderControls?.querySelector(
                '[data-file-browser-control-current="file-types"]',
            ),
        ).toHaveProperty("textContent", "All file types");
    });

    it("keeps inline controls in the active folder name area", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/plot-001.png", "output"),
            buildFile("/results/plot-002.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files,
                }),
            );
        });

        const heading = container.querySelector(
            '[data-directory-heading-with-controls="/results"]',
        );
        const nameAreaControls = container.querySelector(
            '[data-file-browser-name-area-controls="/results"]',
        );
        const groupedContent = container.querySelector(
            '[data-directory-group-content="/results"]',
        );
        const folderControls = container.querySelector(
            '[data-file-browser-folder-controls="/results"]',
        ) as HTMLElement | null;

        expect(heading).toBeTruthy();
        expect(nameAreaControls).toBeTruthy();
        expect(folderControls?.dataset.fileBrowserControlPlacement).toBe(
            "name-area",
        );
        expect(heading?.contains(folderControls)).toBe(true);
        expect(groupedContent?.contains(folderControls)).toBe(false);
    });

    it("does not render temporary alternative design layouts", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/plot-001.png", "output"),
            buildFile("/results/plot-002.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files,
                }),
            );
        });

        expect(
            container.querySelector(
                '[data-file-browser-sidecar-layout="/results"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-file-browser-content-ribbon="/results"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-file-browser-file-matrix-header="/results"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-file-browser-file-layout="matrix-row"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-file-browser-file-layout="deck-strip"]',
            ),
        ).toBeNull();
        expect(
            (
                container.querySelector(
                    '[data-file-browser-folder-controls="/results"]',
                ) as HTMLElement | null
            )?.dataset.fileBrowserControlPlacement,
        ).toBe("name-area");
        expect(
            container.querySelector('[data-file-browser-file-layout="card"]'),
        ).toBeTruthy();
    });

    it("dragging one preview resize handle updates the global height for all visible previews", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handlePreviewHeightChange = vi.fn();
        const files = [
            buildFile("/results/plot-001.png", "output"),
            buildFile("/results/plot-002.png", "output"),
        ];
        function Harness() {
            const [height, setHeight] = useState(220);

            return createElement(FileBrowser, {
                files,
                onPreviewHeightChange: (value: number) => {
                    handlePreviewHeightChange(value);
                    setHeight(value);
                },
                onSelectDirectory: vi.fn(),
                onSelectFile: vi.fn(),
                previewHeight: height,
                previewMode: "grid",
                renderGridPreview: (file: FileEntry): ReactNode =>
                    createElement(
                        "div",
                        { "data-testid": `grid-preview-${file.path}` },
                        `preview:${file.path}`,
                    ),
                visibleFiles: files,
            });
        }

        await act(async () => {
            root.render(createElement(Harness));
        });

        const frames = () =>
            Array.from(
                container.querySelectorAll<HTMLElement>(
                    "[data-preview-resize-frame]",
                ),
            );
        const handles = container.querySelectorAll<HTMLElement>(
            "[data-preview-resize-handle]",
        );

        expect(frames()).toHaveLength(2);
        expect(handles).toHaveLength(2);
        expect(frames().map((frame) => frame.style.height)).toEqual([
            "220px",
            "220px",
        ]);

        await act(async () => {
            handles[0]?.dispatchEvent(
                new MouseEvent("mousedown", {
                    bubbles: true,
                    clientY: 200,
                }),
            );
            document.dispatchEvent(
                new MouseEvent("mousemove", {
                    bubbles: true,
                    clientY: 280,
                }),
            );
            document.dispatchEvent(
                new MouseEvent("mouseup", { bubbles: true }),
            );
        });

        expect(handlePreviewHeightChange).toHaveBeenCalledWith(300);
        expect(frames().map((frame) => frame.style.height)).toEqual([
            "300px",
            "300px",
        ]);
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

    it("hides the single-page preview widget for expanded folders", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/plot-001.png", "output"),
            buildFile("/results/plot-002.png", "output"),
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
                    previewPage: 1,
                    previewPageCount: 1,
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement("div", {}, file.path),
                    visibleFiles: files,
                }),
            );
        });

        const folderControls = container.querySelector(
            '[data-file-browser-folder-controls="/results"]',
        );

        expect(folderControls).toBeTruthy();
        expect(folderControls?.textContent).toContain("1 preview per row");
        expect(folderControls?.textContent).not.toContain("Page 1 of 1");
        expect(
            container.querySelector(
                '[data-file-browser-bottom-controls="/results"]',
            ),
        ).toBeNull();
        expect(container.textContent).not.toContain("Page 1 of 1");
    });

    it("hides preview widgets when a folder only contains unsupported binary files", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/lane-1.bam", "output"),
            buildFile("/results/lane-2.bam", "output"),
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
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files,
                }),
            );
        });

        expect(container.textContent).toContain("lane-1.bam");
        expect(container.textContent).toContain("lane-2.bam");
        expect(
            container.querySelector(
                '[data-file-browser-folder-controls="/results"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector('[data-file-browser-preview="single"]'),
        ).toBeNull();
        expect(
            container.querySelector('[data-testid="single-preview"]'),
        ).toBeNull();

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
                            { "data-testid": `grid-preview-${file.path}` },
                            file.path,
                        ),
                    visibleFiles: files,
                }),
            );
        });

        expect(
            container.querySelector(
                '[data-file-browser-folder-controls="/results"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-grid-preview-path="/results/lane-1.bam"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-grid-preview-path="/results/lane-2.bam"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-testid="grid-preview-/results/lane-1.bam"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-testid="grid-preview-/results/lane-2.bam"]',
            ),
        ).toBeNull();
    });

    it("keeps folder-scoped preview controls when the current page only contains unsupported binaries", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/page-1-plot.png", "output"),
            buildFile("/results/page-2-lane-1.bam", "output"),
            buildFile("/results/page-2-lane-2.bam", "output"),
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
                    previewMode: "single",
                    previewPage: 2,
                    previewPageCount: 2,
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files.slice(1),
                }),
            );
        });

        const folderControls = container.querySelector(
            '[data-file-browser-folder-controls="/results"]',
        );

        expect(folderControls).toBeTruthy();
        expect(folderControls?.textContent).toContain("Page 2 of 2");
        expect(container.textContent).toContain("page-2-lane-1.bam");
        expect(container.textContent).toContain("page-2-lane-2.bam");
        expect(
            container.querySelector('[data-file-browser-preview="single"]'),
        ).toBeTruthy();
        expect(
            container.querySelector('[data-testid="single-preview"]')
                ?.textContent,
        ).toContain("/results/page-1-plot.png");
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

    it("hides the 1 preview per row toggle for folders with only one file", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [buildFile("/results/a/plot-001.png", "output")];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onPreviewPageChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "single",
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files,
                }),
            );
        });

        const folderControls = container.querySelector(
            '[data-file-browser-folder-controls="/results/a"]',
        );

        expect(folderControls).toBeTruthy();
        expect(
            folderControls?.querySelector(
                'input[aria-label="1 preview per row"]',
            ),
        ).toBeNull();
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

    it("shows the subfolder preview gallery controls when one subdirectory contains previewable files", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-b/archive.bin", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-testid": `subdir-preview-${file.path}`,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [],
                }),
            );
        });

        const controls = container.querySelector(
            '[data-subdir-preview-controls="/demo"]',
        );
        const toggle = controls?.querySelector(
            'input[aria-label="Subfolder previews"]',
        ) as HTMLInputElement | null;

        expect(controls).toBeTruthy();
        expect(toggle).toBeTruthy();

        await click(toggle);

        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a"]',
            ),
        ).toBeTruthy();
    });

    it("hides subfolder preview controls when immediate subdirectories only contain nested previewable files", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/summary.txt", "output"),
            buildFile("/demo/sample-a/run-1/img-1.png", "output"),
            buildFile("/demo/sample-a/run-1/img-2.png", "output"),
            buildFile("/demo/sample-b/run-2/pic-1.png", "output"),
            buildFile("/demo/sample-b/run-2/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-testid": `subdir-preview-${file.path}`,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [files[0] as FileEntry],
                }),
            );
        });

        expect(container.textContent).toContain("summary.txt");
        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeNull();
    });

    it("shows subfolder preview controls for compressed immediate-child chains when the parent has no direct files", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/run-1/img-1.png", "output"),
            buildFile("/demo/sample-a/run-1/img-2.png", "output"),
            buildFile("/demo/sample-b/run-2/pic-1.png", "output"),
            buildFile("/demo/sample-b/run-2/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-testid": `subdir-preview-${file.path}`,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [],
                }),
            );
        });

        const controls = container.querySelector(
            '[data-subdir-preview-controls="/demo"]',
        );
        const toggle = controls?.querySelector(
            'input[aria-label="Subfolder previews"]',
        ) as HTMLInputElement | null;

        expect(controls).toBeTruthy();
        expect(toggle).toBeTruthy();

        await click(toggle);

        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a/run-1"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-b/run-2"]',
            ),
        ).toBeTruthy();
    });

    it("renders one shared resize affordance for folders eligible for both direct-file and subfolder previews", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handlePreviewHeightChange = vi.fn();
        const files = [
            buildFile("/demo/summary.png", "output"),
            buildFile("/demo/table.tsv", "output"),
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: handlePreviewHeightChange,
                    onPreviewModeChange: vi.fn(),
                    onPreviewPageChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewHeight: 220,
                    previewMode: "single",
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            { "data-subdir-preview-file": file.path },
                            file.path,
                        ),
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: files.slice(0, 2),
                }),
            );
        });

        const folderControls = container.querySelectorAll(
            '[data-file-browser-folder-controls="/demo"]',
        );
        const previewResizeHandles = container.querySelectorAll<HTMLElement>(
            "[data-preview-resize-handle]",
        );

        expect(folderControls).toHaveLength(1);
        expect(
            container.querySelector('input[aria-label="Preview height"]'),
        ).toBeNull();
        expect(previewResizeHandles).toHaveLength(1);
        expect(
            container.querySelector('input[aria-label="1 preview per row"]'),
        ).toBeTruthy();
        expect(
            container.querySelector('input[aria-label="Subfolder previews"]'),
        ).toBeTruthy();

        await act(async () => {
            previewResizeHandles[0]?.dispatchEvent(
                new MouseEvent("mousedown", {
                    bubbles: true,
                    clientY: 200,
                }),
            );
            document.dispatchEvent(
                new MouseEvent("mousemove", {
                    bubbles: true,
                    clientY: 280,
                }),
            );
            document.dispatchEvent(
                new MouseEvent("mouseup", { bubbles: true }),
            );
        });

        expect(handlePreviewHeightChange).toHaveBeenCalledWith(300);

        const subfolderToggle = container.querySelector(
            'input[aria-label="Subfolder previews"]',
        ) as HTMLInputElement | null;

        expect(subfolderToggle).toBeTruthy();

        await act(async () => {
            if (!subfolderToggle) {
                throw new Error("Missing subfolder preview toggle");
            }

            subfolderToggle.checked = true;
            subfolderToggle.dispatchEvent(
                new Event("change", { bubbles: true }),
            );
        });

        expect(
            container.querySelectorAll(
                '[data-file-browser-folder-controls="/demo"] input[type="range"]',
            ),
        ).toHaveLength(0);
        expect(
            container.querySelectorAll("[data-preview-resize-handle]").length,
        ).toBeGreaterThan(0);
        expect(handlePreviewHeightChange).toHaveBeenCalledTimes(1);
    });

    it("applies the file types widget to single, grid, and subfolder previews with all previewable types selected by default", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/photo.png", "output"),
            buildFile("/demo/notes.txt", "output"),
            buildFile("/demo/sample-a/plot.png", "output"),
            buildFile("/demo/sample-a/table.csv", "output"),
            buildFile("/demo/sample-b/readme.md", "output"),
            buildFile("/demo/sample-b/stats.tsv", "output"),
        ];
        function PreviewModeHarness(): ReactNode {
            const [previewMode, setPreviewMode] = useState<"single" | "grid">(
                "single",
            );

            return createElement(FileBrowser, {
                files,
                onPreviewModeChange: setPreviewMode,
                onSelectDirectory: vi.fn(),
                onSelectFile: vi.fn(),
                previewMode,
                renderGridPreview: (file: FileEntry): ReactNode =>
                    createElement(
                        "div",
                        { "data-subdir-preview-file": file.path },
                        file.path,
                    ),
                renderSinglePreview: (file: FileEntry | null): ReactNode =>
                    createElement(
                        "div",
                        { "data-testid": "single-preview" },
                        file?.path ?? "none",
                    ),
                selectedDirectory: "/demo",
                visibleFiles: files.slice(0, 2),
            });
        }

        await act(async () => {
            root.render(createElement(PreviewModeHarness));
        });

        const folderControls = container.querySelector(
            '[data-file-browser-folder-controls="/demo"]',
        );
        const gridToggle = folderControls?.querySelector(
            'input[aria-label="1 preview per row"]',
        ) as HTMLInputElement | null;
        const subfolderToggle = folderControls?.querySelector(
            'input[aria-label="Subfolder previews"]',
        ) as HTMLInputElement | null;
        const disclosureTrigger = folderControls?.querySelector(
            'summary[aria-label="File types"]',
        ) as HTMLElement | null;
        const previewModesTrigger = folderControls?.querySelector(
            'summary[aria-label="Preview modes"]',
        ) as HTMLElement | null;

        expect(folderControls).toBeTruthy();
        expect(gridToggle).toBeTruthy();
        expect(subfolderToggle).toBeTruthy();
        expect(previewModesTrigger).toBeTruthy();
        expect(
            folderControls?.querySelectorAll(
                "input[data-subdir-preview-kind]:checked",
            ),
        ).toHaveLength(previewFileTypeOptions.length);
        expect(
            container.querySelector('[data-testid="single-preview"]')
                ?.textContent,
        ).toContain("/demo/photo.png");

        await click(previewModesTrigger);
        await click(gridToggle);

        expect(
            container.querySelector(
                '[data-file-browser-grid-row="/demo/photo.png"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-file-browser-grid-row="/demo/notes.txt"]',
            ),
        ).toBeTruthy();

        await click(disclosureTrigger);

        const optionLabels = [
            ...(folderControls?.querySelectorAll(
                "[data-subdir-preview-kind-options] label",
            ) ?? []),
        ].map((label) => label.textContent?.trim() ?? "");
        const optionGrid = folderControls?.querySelector(
            "[data-subdir-preview-kind-options]",
        ) as HTMLElement | null;
        const imageCheckbox = container.querySelector(
            'input[data-subdir-preview-kind="image"]',
        ) as HTMLInputElement | null;
        const csvCheckbox = container.querySelector(
            'input[data-subdir-preview-kind="csv"]',
        ) as HTMLInputElement | null;
        const mdCheckbox = container.querySelector(
            'input[data-subdir-preview-kind="md"]',
        ) as HTMLInputElement | null;
        const txtCheckbox = container.querySelector(
            'input[data-subdir-preview-kind="txt"]',
        ) as HTMLInputElement | null;
        const getDeselectAllButton = () =>
            container.querySelector(
                'button[data-subdir-preview-kind-clear="/demo"]',
            ) as HTMLButtonElement | null;
        const getFileTypeMenu = () =>
            container.querySelector(
                '[data-subdir-preview-kinds="/demo"]',
            ) as HTMLElement | null;

        expect(optionLabels).toEqual(
            previewFileTypeOptions.map((option) => option.label),
        );
        expect(optionLabels).not.toEqual(
            expect.arrayContaining([
                "Tables",
                "Markdown",
                "Text & code",
                "Documents",
            ]),
        );
        expect(optionGrid?.className).toContain("grid-cols");
        expect(imageCheckbox?.checked).toBe(true);
        expect(csvCheckbox?.checked).toBe(true);
        expect(mdCheckbox?.checked).toBe(true);
        expect(txtCheckbox?.checked).toBe(true);
        expect(getDeselectAllButton()?.textContent).toContain("Deselect all");

        await click(csvCheckbox);
        await click(mdCheckbox);
        await click(txtCheckbox);

        expect(
            container.querySelector(
                '[data-file-browser-grid-row="/demo/photo.png"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-file-browser-grid-row="/demo/notes.txt"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-grid-preview-path="/demo/notes.txt"]',
            )?.textContent,
        ).toBe("");
        expect(subfolderToggle?.checked).toBe(false);

        await click(getDeselectAllButton());

        const selectedFileTypeInputs = [
            ...(getFileTypeMenu()?.querySelectorAll<HTMLInputElement>(
                "input[data-subdir-preview-kind]",
            ) ?? []),
        ];

        expect(selectedFileTypeInputs).toHaveLength(
            previewFileTypeOptions.length,
        );
        expect(selectedFileTypeInputs.every((input) => !input.checked)).toBe(
            true,
        );
        expect(
            container.querySelector(
                '[data-file-browser-control-current="file-types"]',
            )?.textContent,
        ).toBe("No file types");
        expect(
            container.querySelector(
                '[data-file-browser-grid-row="/demo/photo.png"]',
            ),
        ).toBeNull();
    });

    it("keeps direct-file-only file type controls after deselecting all so a type can be reselected", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/results/plots/plot-001.png", "output"),
            buildFile("/results/plots/plot-002.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewModeChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewMode: "grid",
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": `grid-preview-${file.path}` },
                            file.path,
                        ),
                    selectedDirectory: "/results/plots",
                    visibleFiles: files,
                }),
            );
        });

        const controls = () =>
            container.querySelector(
                '[data-file-browser-folder-controls="/results/plots"]',
            ) as HTMLElement | null;
        const menu = () =>
            container.querySelector(
                '[data-subdir-preview-kinds="/results/plots"]',
            ) as HTMLElement | null;
        const summary = () =>
            controls()?.querySelector(
                'summary[aria-label="File types"]',
            ) as HTMLElement | null;
        const selectedSummary = () =>
            controls()?.querySelector(
                '[data-file-browser-control-current="file-types"]',
            )?.textContent ?? null;
        const imageCheckbox = () =>
            menu()?.querySelector(
                'input[data-subdir-preview-kind="image"]',
            ) as HTMLInputElement | null;

        expect(controls()).toBeTruthy();
        expect(
            container.querySelector(
                '[data-file-browser-grid-row="/results/plots/plot-001.png"]',
            ),
        ).toBeTruthy();

        await click(summary());
        await click(
            menu()?.querySelector(
                'button[data-subdir-preview-kind-clear="/results/plots"]',
            ) ?? null,
        );

        expect(controls()).toBeTruthy();
        expect(summary()).toBeTruthy();
        expect(selectedSummary()).toBe("No file types");
        expect(imageCheckbox()?.checked).toBe(false);
        expect(
            container.querySelector(
                '[data-file-browser-grid-row="/results/plots/plot-001.png"]',
            ),
        ).toBeNull();

        await click(imageCheckbox());

        expect(controls()).toBeTruthy();
        expect(selectedSummary()).toBe("Images");
        expect(imageCheckbox()?.checked).toBe(true);
        expect(
            container.querySelector(
                '[data-file-browser-grid-row="/results/plots/plot-001.png"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-testid="grid-preview-/results/plots/plot-001.png"]',
            ),
        ).toBeTruthy();
    });

    it("keeps preview mode summaries and toggles aligned when selection moves from a parent folder to a subfolder", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/summary.png", "output"),
            buildFile("/demo/sample-a/notes.txt", "output"),
            buildFile("/demo/sample-a/lanes/lane-1/plot.png", "output"),
            buildFile("/demo/sample-a/lanes/lane-1/metrics.tsv", "output"),
            buildFile("/demo/sample-a/lanes/lane-2/plot.png", "output"),
            buildFile("/demo/sample-a/lanes/lane-2/metrics.tsv", "output"),
            buildFile("/demo/sample-a/reports/overview.png", "output"),
            buildFile("/demo/sample-a/reports/summary.tsv", "output"),
        ];

        function PreviewSelectionHarness(): ReactNode {
            const [previewMode, setPreviewMode] = useState<"single" | "grid">(
                "single",
            );
            const [selectedDirectory, setSelectedDirectory] =
                useState("/demo/sample-a");

            return createElement(FileBrowser, {
                files,
                onPreviewModeChange: setPreviewMode,
                onSelectDirectory: (
                    directoryPath: string,
                    options?: { expanded: boolean; parentPath?: string },
                ) => {
                    setSelectedDirectory(
                        options?.expanded === false && options.parentPath
                            ? options.parentPath
                            : directoryPath,
                    );
                },
                onSelectFile: vi.fn(),
                previewMode,
                renderGridPreview: (file: FileEntry): ReactNode =>
                    createElement(
                        "div",
                        { "data-subdir-preview-file": file.path },
                        file.path,
                    ),
                renderSinglePreview: (file: FileEntry | null): ReactNode =>
                    createElement(
                        "div",
                        { "data-testid": "single-preview" },
                        file?.path ?? "none",
                    ),
                selectedDirectory,
                visibleFiles: files.filter(
                    (file) =>
                        file.path.startsWith(`${selectedDirectory}/`) &&
                        !file.path
                            .slice(selectedDirectory.length + 1)
                            .includes("/"),
                ),
            });
        }

        await act(async () => {
            root.render(createElement(PreviewSelectionHarness));
        });

        const parentControls = () =>
            container.querySelector(
                '[data-file-browser-folder-controls="/demo/sample-a"]',
            ) as HTMLElement | null;
        const childControls = () =>
            container.querySelector(
                '[data-file-browser-folder-controls="/demo/sample-a/lanes"]',
            ) as HTMLElement | null;
        const previewModeSummary = (directoryPath: string) =>
            container.querySelector(
                `[data-preview-mode-disclosure="${directoryPath}"] [data-file-browser-control-current="preview-modes"]`,
            ) as HTMLElement | null;
        const gridToggle = (directoryPath: string) =>
            container.querySelector(
                `[data-file-browser-folder-controls="${directoryPath}"] input[aria-label="1 preview per row"]`,
            ) as HTMLInputElement | null;
        const subfolderToggle = (directoryPath: string) =>
            container.querySelector(
                `[data-file-browser-folder-controls="${directoryPath}"] input[aria-label="Subfolder previews"]`,
            ) as HTMLInputElement | null;

        expect(parentControls()).toBeTruthy();
        expect(gridToggle("/demo/sample-a")?.checked).toBe(false);
        expect(subfolderToggle("/demo/sample-a")?.checked).toBe(false);
        expect(previewModeSummary("/demo/sample-a")?.textContent).toBe(
            "Single preview",
        );

        await click(
            parentControls()?.querySelector(
                'summary[aria-label="Preview modes"]',
            ) ?? null,
        );
        await click(gridToggle("/demo/sample-a"));
        await click(subfolderToggle("/demo/sample-a"));

        expect(gridToggle("/demo/sample-a")?.checked).toBe(true);
        expect(subfolderToggle("/demo/sample-a")?.checked).toBe(true);
        expect(previewModeSummary("/demo/sample-a")?.textContent).toBe(
            "Grid + subfolders",
        );

        await click(
            parentControls()?.querySelector(
                'summary[aria-label="Preview modes"]',
            ) ?? null,
        );

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a/lanes"]',
            ),
        );

        expect(childControls()).toBeTruthy();
        expect(previewModeSummary("/demo/sample-a/lanes")?.textContent).toBe(
            "Single preview",
        );
        expect(gridToggle("/demo/sample-a/lanes")).toBeNull();
        expect(subfolderToggle("/demo/sample-a/lanes")?.checked).toBe(false);
        expect(parentControls()).toBeNull();
        expect(previewModeSummary("/demo/sample-a")?.textContent).toBe(
            undefined,
        );

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a/lanes"]',
            ),
        );

        expect(childControls()).toBeNull();
        expect(parentControls()).toBeTruthy();
        expect(previewModeSummary("/demo/sample-a")?.textContent).toBe(
            "Grid + subfolders",
        );
        expect(gridToggle("/demo/sample-a")).toBeTruthy();
        expect(gridToggle("/demo/sample-a")?.checked).toBe(true);

        await click(gridToggle("/demo/sample-a"));

        expect(gridToggle("/demo/sample-a")?.checked).toBe(false);
        expect(previewModeSummary("/demo/sample-a")?.textContent).toBe(
            "Subfolders",
        );
        expect(previewModeSummary("/demo/sample-a/lanes")).toBeNull();
    });

    it("restores subfolder preview widgets on an uncontrolled parent after an open child closes", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-a/notes.txt", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    visibleFiles: [],
                }),
            );
        });

        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeTruthy();

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a"]',
            ),
        );

        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeNull();

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a"]',
            ),
        );

        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeTruthy();
    });

    it("restores subfolder preview widgets on a controlled parent after an open child closes", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-a/notes.txt", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
        ];

        function ControlledHarness(): ReactNode {
            const [selectedDirectory, setSelectedDirectory] = useState("/demo");

            return createElement(FileBrowser, {
                files,
                onSelectDirectory: (path: string) => {
                    setSelectedDirectory(path);
                },
                onSelectFile: vi.fn(),
                renderGridPreview: (file: FileEntry): ReactNode =>
                    createElement(
                        "div",
                        {
                            "data-subdir-preview-file": file.path,
                        },
                        file.path,
                    ),
                selectedDirectory,
                visibleFiles: [],
            });
        }

        await act(async () => {
            root.render(createElement(ControlledHarness));
        });

        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeTruthy();

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a"]',
            ),
        );

        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeNull();

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a"]',
            ),
        );

        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeTruthy();
    });

    it("hides parent preview tools while an eligible child folder is open and restores them when the child closes", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/qc/direct-plot.svg", "output"),
            buildFile("/demo/delivery/plots/volcano.svg", "output"),
            buildFile("/demo/qc/images/overview/plot-0.png", "output"),
            buildFile("/demo/qc/images/overview/plot-1.png", "output"),
            buildFile("/demo/qc/images/image.png", "output"),
            buildFile("/demo/qc/images/image-2.png", "output"),
            buildFile("/demo/qc/images/gallery/plot-1.png", "output"),
            buildFile("/demo/qc/images/gallery/plot-2.png", "output"),
            buildFile("/demo/qc/images/gallery/plate-a/plot-a.png", "output"),
            buildFile("/demo/qc/images/gallery/plate-a/metrics.tsv", "output"),
            buildFile("/demo/qc/images/gallery/plate-b/plot-b.png", "output"),
            buildFile("/demo/qc/images/gallery/plate-b/metrics.tsv", "output"),
            buildFile("/demo/qc/notes/summary.txt", "output"),
            buildFile("/demo/qc/notes/multiqc-summary.txt", "output"),
        ];

        function ControlledHarness(): ReactNode {
            const [selectedDirectory, setSelectedDirectory] = useState("/demo");

            return createElement(FileBrowser, {
                files,
                onSelectDirectory: (
                    path: string,
                    options?: { expanded: boolean; parentPath?: string },
                ) => {
                    setSelectedDirectory(
                        options?.expanded === false && options.parentPath
                            ? options.parentPath
                            : path,
                    );
                },
                onSelectFile: vi.fn(),
                renderGridPreview: (file: FileEntry): ReactNode =>
                    createElement(
                        "div",
                        {
                            "data-subdir-preview-file": file.path,
                        },
                        file.path,
                    ),
                selectedDirectory,
                visibleFiles: [],
            });
        }

        await act(async () => {
            root.render(createElement(ControlledHarness));
        });

        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeTruthy();

        await click(
            container.querySelector('button[data-directory-path="/demo/qc"]'),
        );

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/qc/images"]',
            ),
        );

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/qc/images/gallery"]',
            ),
        );

        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-subdir-preview-controls="/demo/qc/images"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-subdir-preview-controls="/demo/qc/images/gallery"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-controls="/demo/qc"]',
            ),
        ).toBeNull();

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/qc/images/gallery"]',
            ),
        );

        expect(
            container.querySelector(
                '[data-subdir-preview-controls="/demo/qc/images"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-controls="/demo/qc/images/gallery"]',
            ),
        ).toBeNull();
    });

    it("keeps deeply nested folder controls beside the directory heading when a wide viewport can fit them", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/lanes/lane-1/rep-a/plot.png", "output"),
            buildFile("/demo/sample-a/lanes/lane-1/rep-b/plot.png", "output"),
            buildFile("/demo/sample-a/lanes/lane-2/plot.png", "output"),
            buildFile("/demo/sample-a/reports/summary.txt", "output"),
            buildFile("/demo/sample-b/readme.txt", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo/sample-a/lanes/lane-1",
                    visibleFiles: [],
                }),
            );
        });

        const heading = container.querySelector(
            '[data-directory-heading-with-controls="/demo/sample-a/lanes/lane-1"]',
        ) as HTMLElement | null;
        const controls = container.querySelector(
            '[data-file-browser-name-area-controls="/demo/sample-a/lanes/lane-1"]',
        ) as HTMLElement | null;

        expect(heading).toBeTruthy();
        expect(controls).toBeTruthy();
        expect(heading?.className).toContain(
            "lg:grid-cols-[minmax(0,1fr)_auto]",
        );
    });

    it("keeps nested subfolder preview ownership and file-type settings on the selected folder", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/direct.png", "output"),
            buildFile("/demo/sample-a/direct.tsv", "output"),
            buildFile("/demo/sample-a/lanes/lane-1/plot.png", "output"),
            buildFile("/demo/sample-a/lanes/lane-1/metrics.tsv", "output"),
            buildFile("/demo/sample-a/lanes/lane-2/plot.png", "output"),
            buildFile("/demo/sample-a/lanes/lane-2/metrics.tsv", "output"),
            buildFile("/demo/sample-b/direct.png", "output"),
            buildFile("/demo/sample-b/direct.tsv", "output"),
        ];

        function ControlledHarness(): ReactNode {
            const [selectedDirectory, setSelectedDirectory] = useState("/demo");

            return createElement(FileBrowser, {
                files,
                onSelectDirectory: (path: string) => {
                    setSelectedDirectory(path);
                },
                onSelectFile: vi.fn(),
                renderGridPreview: (file: FileEntry): ReactNode =>
                    createElement(
                        "div",
                        {
                            "data-subdir-preview-file": file.path,
                        },
                        file.path,
                    ),
                selectedDirectory,
                visibleFiles: [],
            });
        }

        await act(async () => {
            root.render(createElement(ControlledHarness));
        });

        const demoControls = () =>
            container.querySelector(
                '[data-subdir-preview-controls="/demo"]',
            ) as HTMLElement | null;
        const lanesControls = () =>
            container.querySelector(
                '[data-subdir-preview-controls="/demo/sample-a/lanes"]',
            ) as HTMLElement | null;

        await click(
            demoControls()?.querySelector(
                'input[aria-label="Subfolder previews"]',
            ) ?? null,
        );
        await click(
            demoControls()?.querySelector(
                'input[data-subdir-preview-kind="tsv"]',
            ) ?? null,
        );

        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/direct.png"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/direct.tsv"]',
            ),
        ).toBeNull();

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a"]',
            ),
        );
        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a/lanes"]',
            ),
        );

        expect(lanesControls()).toBeTruthy();

        await click(
            lanesControls()?.querySelector(
                'input[aria-label="Subfolder previews"]',
            ) ?? null,
        );

        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a/lanes/lane-1"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/lanes/lane-1/metrics.tsv"]',
            ),
        ).toBeTruthy();

        await click(
            container.querySelector('button[data-directory-path="/demo"]'),
        );
        await click(
            container.querySelector('button[data-directory-path="/demo"]'),
        );

        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/direct.tsv"]',
            ),
        ).toBeNull();

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a"]',
            ),
        );
        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a/lanes"]',
            ),
        );

        const lanesTsvCheckbox = lanesControls()?.querySelector(
            'input[data-subdir-preview-kind="tsv"]',
        ) as HTMLInputElement | null;

        expect(lanesTsvCheckbox?.checked).toBe(true);
        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a/lanes/lane-1"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/lanes/lane-1/metrics.tsv"]',
            ),
        ).toBeTruthy();
    });

    it("shows subfolder preview controls on initial load when the first tree row is the eligible parent folder", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const onSelectDirectory = vi.fn();
        const files = [
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory,
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    visibleFiles: [],
                }),
            );
        });

        expect(onSelectDirectory).toHaveBeenCalledWith("/demo");
        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeTruthy();
    });

    it("shows subfolder preview controls on initial load when the eligible parent folder is nested under a later top-level branch", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const onSelectDirectory = vi.fn();
        const files = [
            buildFile("/alpha/readme.txt", "output"),
            buildFile("/results/demo/sample-a/img-1.png", "output"),
            buildFile("/results/demo/sample-a/img-2.png", "output"),
            buildFile("/results/demo/sample-b/pic-1.png", "output"),
            buildFile("/results/demo/sample-b/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory,
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    visibleFiles: [],
                }),
            );
        });

        expect(onSelectDirectory).toHaveBeenCalledWith("/results/demo");
        expect(
            container.querySelector(
                '[data-subdir-preview-controls="/results/demo"]',
            ),
        ).toBeTruthy();
    });

    it("keeps resize handles available alongside subfolder preview controls", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/readme.md", "output"),
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-a/data.csv", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
            buildFile("/demo/sample-b/pic-3.png", "output"),
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
                    previewHeight: 180,
                    previewMode: "grid",
                    previewPage: 1,
                    previewPageCount: 2,
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-testid": `subdir-preview-${file.path}`,
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [buildFile("/demo/readme.md", "output")],
                }),
            );
        });

        const controls = container.querySelector(
            '[data-subdir-preview-controls="/demo"]',
        );
        const folderControls = container.querySelector(
            '[data-file-browser-folder-controls="/demo"]',
        );

        expect(controls).toBeTruthy();
        expect(folderControls).toBeTruthy();
        expect(
            controls?.closest('[data-file-browser-folder-controls="/demo"]'),
        ).toBe(folderControls);
        expect(controls?.textContent).toContain("Subfolder previews");
        expect(controls?.textContent).not.toContain("Preview file types");
        expect(
            container.querySelector('input[aria-label="Preview height"]'),
        ).toBeNull();
        expect(
            controls?.querySelector(
                '[data-subdir-preview-kind-disclosure="/demo"]',
            ),
        ).toBeTruthy();

        // Default state: toggle disabled; no gallery rows rendered.
        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a"]',
            ),
        ).toBeNull();

        const toggle = controls?.querySelector(
            'input[aria-label="Subfolder previews"]',
        ) as HTMLInputElement | null;

        expect(toggle).toBeTruthy();
        expect(toggle?.checked).toBe(false);

        // Default file-type selection includes all previewable types.
        const imageCheckbox = controls?.querySelector(
            'input[data-subdir-preview-kind="image"]',
        ) as HTMLInputElement | null;
        const csvCheckbox = controls?.querySelector(
            'input[data-subdir-preview-kind="csv"]',
        ) as HTMLInputElement | null;

        expect(imageCheckbox?.checked).toBe(true);
        expect(csvCheckbox?.checked).toBe(true);

        await click(toggle);

        const rowA = container.querySelector(
            '[data-subdir-preview-row="/demo/sample-a"]',
        );
        const rowB = container.querySelector(
            '[data-subdir-preview-row="/demo/sample-b"]',
        );

        expect(rowA).toBeTruthy();
        expect(rowB).toBeTruthy();
        expect(
            container.querySelectorAll("[data-preview-resize-handle]").length,
        ).toBeGreaterThan(0);

        // Each row shows all selected previewable file types by default.
        expect(
            rowA?.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/img-1.png"]',
            ),
        ).toBeTruthy();
        expect(
            rowA?.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/img-2.png"]',
            ),
        ).toBeTruthy();
        expect(
            rowA?.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/data.csv"]',
            ),
        ).toBeTruthy();
        expect(
            rowB?.querySelector(
                '[data-subdir-preview-file="/demo/sample-b/pic-1.png"]',
            ),
        ).toBeTruthy();

        // Previews are laid out horizontally on each row.
        const galleryStripA = rowA?.querySelector(
            '[data-subdir-preview-strip="/demo/sample-a"]',
        );
        const rowAHeading = rowA?.querySelector(
            '[data-subdir-preview-heading="/demo/sample-a"]',
        );
        const cardA = rowA?.querySelector(
            '[data-subdir-preview-card="/demo/sample-a/img-1.png"]',
        ) as HTMLElement | null;
        const cardAFilename = rowA?.querySelector(
            '[data-subdir-preview-filename="/demo/sample-a/img-1.png"]',
        );
        const cardACopyPathControl = cardA?.querySelector(
            '[aria-label="Copy file full path /demo/sample-a/img-1.png"]',
        );

        expect(galleryStripA).toBeTruthy();
        expect(rowAHeading).toBeTruthy();
        expect(cardA).toBeTruthy();
        expect(cardAFilename?.textContent).toBe("img-1.png");
        expect(cardACopyPathControl).toBeTruthy();
        expect(rowA?.className).not.toMatch(/lg:grid-cols-\[/);
        expect(galleryStripA?.className).toMatch(/(?:^|\s)flex/);
        expect(galleryStripA?.className).toMatch(/(?:^|\s)w-full/);
        expect(cardA?.className).toMatch(/(?:^|\s)w-full/);
        expect(cardA?.className).toMatch(/(?:^|\s)shrink-0/);

        // Narrowing away from tables removes csv previews on the row.
        await click(csvCheckbox);

        expect(
            rowA?.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/data.csv"]',
            ),
        ).toBeNull();
    });

    it("renders subfolder previews inside the original subdirectory row and hides them when that row expands", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [],
                }),
            );
        });

        const controls = container.querySelector(
            '[data-subdir-preview-controls="/demo"]',
        );
        const toggle = controls?.querySelector(
            'input[aria-label="Subfolder previews"]',
        );

        expect(controls).toBeTruthy();
        expect(toggle).toBeTruthy();

        await click(toggle);

        const sampleARow = container.querySelector(
            '[data-directory-row="/demo/sample-a"]',
        );

        expect(sampleARow).toBeTruthy();
        expect(
            container.querySelector('[data-subdir-preview-gallery="/demo"]'),
        ).toBeNull();
        expect(
            container.querySelectorAll('[data-directory-row="/demo/sample-a"]'),
        ).toHaveLength(1);
        expect(
            sampleARow?.querySelector(
                '[data-subdir-preview-strip="/demo/sample-a"]',
            ),
        ).toBeTruthy();
        expect(
            sampleARow?.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/img-1.png"]',
            ),
        ).toBeTruthy();

        await click(
            container.querySelector(
                'button[data-directory-path="/demo/sample-a"]',
            ),
        );

        const expandedSampleARow = container.querySelector(
            '[data-directory-row="/demo/sample-a"]',
        );

        expect(
            expandedSampleARow?.querySelector(
                '[data-subdir-preview-strip="/demo/sample-a"]',
            ),
        ).toBeNull();
    });

    it("stacks folder-row widgets beneath the directory button inside the same row surface", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = Array.from({ length: 120 }, (_, index) =>
            buildFile(
                `/results/very-long-folder-name-${String(index + 1).padStart(3, "0")}.png`,
                "output",
            ),
        );

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onPreviewHeightChange: vi.fn(),
                    onPreviewModeChange: vi.fn(),
                    onPreviewPageChange: vi.fn(),
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    previewHeight: 180,
                    previewMode: "single",
                    previewPage: 2,
                    previewPageCount: 3,
                    renderSinglePreview: (file: FileEntry | null): ReactNode =>
                        createElement(
                            "div",
                            { "data-testid": "single-preview" },
                            file?.path ?? "none",
                        ),
                    visibleFiles: files.slice(40, 80),
                }),
            );
        });

        const directoryRow = container.querySelector(
            '[data-directory-row="/results"]',
        ) as HTMLElement | null;
        const directoryButton = directoryRow?.querySelector(
            'button[data-directory-path="/results"]',
        ) as HTMLElement | null;
        const folderControls = directoryRow?.querySelector(
            '[data-file-browser-folder-controls="/results"]',
        ) as HTMLElement | null;
        const groupContent = directoryRow?.querySelector(
            '[data-directory-group-content="/results"]',
        ) as HTMLElement | null;

        expect(directoryRow).toBeTruthy();
        expect(directoryButton).toBeTruthy();
        expect(folderControls).toBeTruthy();
        expect(groupContent).toBeTruthy();
        const heading = directoryRow?.querySelector(
            '[data-directory-heading-with-controls="/results"]',
        ) as HTMLElement | null;
        const nameAreaControls = directoryRow?.querySelector(
            '[data-file-browser-name-area-controls="/results"]',
        ) as HTMLElement | null;

        expect(heading).toBeTruthy();
        expect(nameAreaControls).toBeTruthy();
        expect(Array.from(directoryRow?.children ?? [])).toEqual([
            heading,
            groupContent,
        ]);
        expect(heading?.contains(directoryButton)).toBe(true);
        expect(nameAreaControls?.contains(folderControls)).toBe(true);
        expect(groupContent?.parentElement).toBe(directoryRow);
        expect(directoryRow?.className).not.toMatch(/lg:grid-cols-\[/);
        expect(folderControls?.className).toContain("justify-start");
        expect(folderControls?.className).not.toContain("justify-end");
    });

    it("paginates subfolder preview gallery rows at twenty folders per page and preserves preview height", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const subdirs = Array.from(
            { length: 21 },
            (_, index) => `sample-${String(index + 1).padStart(2, "0")}`,
        );
        const files = subdirs.flatMap((name, index) =>
            [1, 2].map((n) =>
                buildFile(`/demo/${name}/img-${index}-${n}.png`, "output"),
            ),
        );

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [],
                }),
            );
        });

        const toggle = container.querySelector(
            'input[aria-label="Subfolder previews"]',
        );

        await click(toggle);

        const visibleRows = () =>
            Array.from(
                container.querySelectorAll("[data-subdir-preview-row]"),
            ).map((row) => row.getAttribute("data-subdir-preview-row"));

        expect(visibleRows()).toHaveLength(20);
        expect(visibleRows()).toContain("/demo/sample-01");
        expect(visibleRows()).toContain("/demo/sample-20");
        expect(visibleRows()).not.toContain("/demo/sample-21");

        const pageSelect = container.querySelector(
            'select[aria-label="Subfolder preview page"]',
        ) as HTMLSelectElement | null;

        expect(pageSelect).toBeTruthy();

        await act(async () => {
            if (!pageSelect) {
                throw new Error("missing page select");
            }
            pageSelect.value = "2";
            pageSelect.dispatchEvent(new Event("change", { bubbles: true }));
        });

        expect(visibleRows()).toEqual(["/demo/sample-21"]);

        const resizeHandle = container.querySelector<HTMLElement>(
            '[data-subdir-preview-row="/demo/sample-21"] [data-preview-resize-handle]',
        );

        expect(resizeHandle).toBeTruthy();

        await act(async () => {
            resizeHandle?.dispatchEvent(
                new MouseEvent("mousedown", {
                    bubbles: true,
                    clientY: 220,
                }),
            );
            document.dispatchEvent(
                new MouseEvent("mousemove", {
                    bubbles: true,
                    clientY: 260,
                }),
            );
            document.dispatchEvent(
                new MouseEvent("mouseup", { bubbles: true }),
            );
        });

        const strip = container.querySelector(
            '[data-subdir-preview-strip="/demo/sample-21"]',
        ) as HTMLElement | null;

        expect(strip).toBeTruthy();
        expect(strip?.style.getPropertyValue("--subdir-preview-height")).toBe(
            "260px",
        );
    });

    it("shows subfolder preview loading cards before mounting large preview batches", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const subdirs = Array.from(
            { length: 20 },
            (_, index) => `batch-${String(index + 1).padStart(2, "0")}`,
        );
        const files = subdirs.flatMap((name) =>
            Array.from({ length: 25 }, (_, index) =>
                buildFile(
                    `/demo/${name}/preview-${String(index + 1).padStart(2, "0")}.txt`,
                    "output",
                ),
            ),
        );
        const renderGridPreview = vi.fn(
            (file: FileEntry): ReactNode =>
                createElement(
                    "div",
                    {
                        "data-mounted-preview": file.path,
                    },
                    `preview:${file.path}`,
                ),
        );

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview,
                    selectedDirectory: "/demo",
                    visibleFiles: [],
                }),
            );
        });

        const toggle = container.querySelector(
            'input[aria-label="Subfolder previews"]',
        );

        renderGridPreview.mockClear();
        await click(toggle);

        expect(container.textContent).toContain("Loading previews...");
        expect(
            container.querySelectorAll("[data-subdir-preview-card]"),
        ).toHaveLength(0);
        expect(
            container.querySelectorAll("[data-mounted-preview]"),
        ).toHaveLength(0);
        expect(renderGridPreview).not.toHaveBeenCalled();

        await act(async () => {
            await new Promise((resolve) =>
                window.requestAnimationFrame(() =>
                    window.requestAnimationFrame(resolve),
                ),
            );
        });

        expect(container.textContent).toContain("Loading preview...");
        expect(
            container.querySelectorAll("[data-subdir-preview-card]"),
        ).toHaveLength(subdirs.length);
        expect(container.textContent).toContain("25 files queued");
        expect(
            container.querySelectorAll("[data-mounted-preview]"),
        ).toHaveLength(0);
        expect(renderGridPreview).not.toHaveBeenCalled();

        await act(async () => {
            await new Promise((resolve) =>
                window.requestAnimationFrame(() =>
                    window.requestAnimationFrame(resolve),
                ),
            );
        });

        await waitFor(() => {
            expect(
                container.querySelectorAll("[data-mounted-preview]"),
            ).toHaveLength(500);
        });
    });

    it("hides the single-page widget for subfolder previews", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-b/img-1.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [],
                }),
            );
        });

        const controls = container.querySelector(
            '[data-subdir-preview-controls="/demo"]',
        );
        const toggle = controls?.querySelector(
            'input[aria-label="Subfolder previews"]',
        );

        expect(controls).toBeTruthy();
        expect(toggle).toBeTruthy();

        await click(toggle);

        const subdirControls = container.querySelector(
            '[data-subdir-preview-controls="/demo"]',
        );

        expect(subdirControls).toBeTruthy();
        expect(subdirControls?.textContent).not.toContain("Page 1 of 1");
        expect(
            container.querySelector(
                'select[aria-label="Subfolder preview page"]',
            ),
        ).toBeNull();
        expect(container.textContent).not.toContain("Page 1 of 1");
    });

    it("keeps subfolder preview controls visible when selected file types narrow the gallery to one subdirectory", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const files = [
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/data.csv", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [],
                }),
            );
        });

        const controls = container.querySelector(
            '[data-subdir-preview-controls="/demo"]',
        );
        const toggle = controls?.querySelector(
            'input[aria-label="Subfolder previews"]',
        ) as HTMLInputElement | null;
        const imageCheckbox = controls?.querySelector(
            'input[data-subdir-preview-kind="image"]',
        ) as HTMLInputElement | null;
        const csvCheckbox = controls?.querySelector(
            'input[data-subdir-preview-kind="csv"]',
        ) as HTMLInputElement | null;

        expect(controls).toBeTruthy();
        expect(toggle).toBeTruthy();
        expect(imageCheckbox?.checked).toBe(true);
        expect(csvCheckbox?.checked).toBe(true);

        await click(toggle);
        expect(
            container.querySelector('[data-subdir-preview-controls="/demo"]'),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-b"]',
            ),
        ).toBeTruthy();

        await click(imageCheckbox);

        const remainingControls = container.querySelector(
            '[data-subdir-preview-controls="/demo"]',
        );
        const remainingToggle = remainingControls?.querySelector(
            'input[aria-label="Subfolder previews"]',
        ) as HTMLInputElement | null;

        expect(remainingControls).toBeTruthy();
        expect(remainingToggle?.checked).toBe(true);
        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-a"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-row="/demo/sample-b"]',
            ),
        ).toBeNull();
        expect(
            container.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/data.csv"]',
            ),
        ).toBeTruthy();
        expect(
            container.querySelector(
                '[data-subdir-preview-file="/demo/sample-a/img-1.png"]',
            ),
        ).toBeNull();
    });

    it("closes the file types menu on outside clicks without collapsing the folder", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handleSelectDirectory = vi.fn();
        const files = [
            buildFile("/demo/readme.md", "output"),
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: handleSelectDirectory,
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [files[0] as FileEntry],
                }),
            );
        });

        const disclosure = () =>
            container.querySelector(
                '[data-subdir-preview-kind-disclosure="/demo"]',
            ) as HTMLElement | null;
        const trigger = () =>
            disclosure()?.querySelector(
                'summary, button[aria-label="File types"]',
            ) as HTMLElement | null;
        const directoryButton = () =>
            container.querySelector(
                'button[data-directory-path="/demo"]',
            ) as HTMLElement | null;

        expect(disclosure()).toBeTruthy();
        expect(directoryButton()).toBeTruthy();

        await click(trigger());

        expect(disclosure()?.hasAttribute("open")).toBe(true);
        expect(
            container.querySelector('[data-subdir-preview-kinds="/demo"]'),
        ).toBeTruthy();

        await act(async () => {
            directoryButton()?.dispatchEvent(
                new MouseEvent("click", { bubbles: true, cancelable: true }),
            );
        });

        expect(disclosure()?.hasAttribute("open")).toBe(false);
        expect(directoryButton()?.getAttribute("data-directory-expanded")).toBe(
            "true",
        );
        expect(handleSelectDirectory).not.toHaveBeenCalled();
    });

    it("closes the file types menu without consuming unrelated outside file clicks", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handleSelectFile = vi.fn();
        const files = [
            buildFile("/demo/readme.md", "output"),
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: handleSelectFile,
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [files[0] as FileEntry],
                }),
            );
        });

        const disclosure = () =>
            container.querySelector(
                '[data-subdir-preview-kind-disclosure="/demo"]',
            ) as HTMLElement | null;
        const trigger = () =>
            disclosure()?.querySelector(
                'summary, button[aria-label="File types"]',
            ) as HTMLElement | null;
        const fileButton = () =>
            container.querySelector(
                'button[data-file-path="/demo/readme.md"]',
            ) as HTMLElement | null;

        expect(disclosure()).toBeTruthy();
        expect(fileButton()).toBeTruthy();

        handleSelectFile.mockClear();

        await click(trigger());

        expect(disclosure()?.hasAttribute("open")).toBe(true);

        await act(async () => {
            fileButton()?.dispatchEvent(
                new MouseEvent("click", { bubbles: true, cancelable: true }),
            );
        });

        expect(disclosure()?.hasAttribute("open")).toBe(false);
        expect(handleSelectFile).toHaveBeenCalledTimes(1);
        expect(handleSelectFile).toHaveBeenCalledWith(
            expect.objectContaining({ path: "/demo/readme.md" }),
        );
    });

    it("closes the preview modes menu on outside clicks without collapsing the folder", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handleSelectDirectory = vi.fn();
        const files = [
            buildFile("/demo/readme.md", "output"),
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: handleSelectDirectory,
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [files[0] as FileEntry],
                }),
            );
        });

        const disclosure = () =>
            container.querySelector(
                '[data-preview-mode-disclosure="/demo"]',
            ) as HTMLElement | null;
        const trigger = () =>
            disclosure()?.querySelector(
                'summary, button[aria-label="Preview modes"]',
            ) as HTMLElement | null;
        const directoryButton = () =>
            container.querySelector(
                'button[data-directory-path="/demo"]',
            ) as HTMLElement | null;

        expect(disclosure()).toBeTruthy();
        expect(directoryButton()).toBeTruthy();

        await click(trigger());

        expect(disclosure()?.hasAttribute("open")).toBe(true);
        expect(
            container.querySelector('[data-preview-modes-menu="/demo"]'),
        ).toBeTruthy();

        await act(async () => {
            directoryButton()?.dispatchEvent(
                new MouseEvent("click", { bubbles: true, cancelable: true }),
            );
        });

        expect(disclosure()?.hasAttribute("open")).toBe(false);
        expect(directoryButton()?.getAttribute("data-directory-expanded")).toBe(
            "true",
        );
        expect(handleSelectDirectory).not.toHaveBeenCalled();
    });

    it("closes the preview modes menu without consuming unrelated outside file clicks", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const handleSelectFile = vi.fn();
        const files = [
            buildFile("/demo/readme.md", "output"),
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-a/img-2.png", "output"),
            buildFile("/demo/sample-b/pic-1.png", "output"),
            buildFile("/demo/sample-b/pic-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectFile: handleSelectFile,
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(
                            "div",
                            {
                                "data-subdir-preview-file": file.path,
                            },
                            file.path,
                        ),
                    selectedDirectory: "/demo",
                    visibleFiles: [files[0] as FileEntry],
                }),
            );
        });

        const disclosure = () =>
            container.querySelector(
                '[data-preview-mode-disclosure="/demo"]',
            ) as HTMLElement | null;
        const trigger = () =>
            disclosure()?.querySelector(
                'summary, button[aria-label="Preview modes"]',
            ) as HTMLElement | null;
        const fileButton = () =>
            container.querySelector(
                'button[data-file-path="/demo/readme.md"]',
            ) as HTMLElement | null;

        expect(disclosure()).toBeTruthy();
        expect(fileButton()).toBeTruthy();

        handleSelectFile.mockClear();

        await click(trigger());

        expect(disclosure()?.hasAttribute("open")).toBe(true);

        await act(async () => {
            fileButton()?.dispatchEvent(
                new MouseEvent("click", { bubbles: true, cancelable: true }),
            );
        });

        expect(disclosure()?.hasAttribute("open")).toBe(false);
        expect(handleSelectFile).toHaveBeenCalledTimes(1);
        expect(handleSelectFile).toHaveBeenCalledWith(
            expect.objectContaining({ path: "/demo/readme.md" }),
        );
    });

    it("does not apply collapsing width overrides to image subfolder preview cards", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const { FileImageThumbnail } =
            await import("@/components/file-preview");
        const files = [
            buildFile("/demo/sample-a/img-1.png", "output"),
            buildFile("/demo/sample-b/img-2.png", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        createElement(FileImageThumbnail, {
                            file,
                            fullSizeUrl: `/api/file?path=${encodeURIComponent(file.path)}`,
                            height: 200,
                            thumbnailUrl: `/api/file?path=${encodeURIComponent(file.path)}&thumb=true&w=320&h=200`,
                        }),
                    selectedDirectory: "/demo",
                    visibleFiles: [],
                }),
            );
        });

        await click(
            container.querySelector('input[aria-label="Subfolder previews"]'),
        );

        const card = container.querySelector(
            '[data-subdir-preview-card="/demo/sample-a/img-1.png"]',
        ) as HTMLElement | null;
        const surface = card?.lastElementChild as HTMLElement | null;

        expect(card).toBeTruthy();
        expect(surface).toBeTruthy();
        expect(card?.className).toContain("w-full");
        expect(card?.className).not.toContain("w-fit");
        expect(surface?.className).not.toContain("[&_button]:w-auto");
        expect(surface?.className).not.toContain("[&_img]:w-auto");
        expect(surface?.className).not.toContain("border");
        expect(surface?.className).not.toContain("bg-background");
        expect(surface?.className).not.toContain("rounded-[1.25rem]");
        expect(
            surface?.querySelector('button[aria-label="Open image lightbox"]'),
        ).toBeTruthy();
        expect(
            surface?.querySelector('img[alt="img-1.png preview"]'),
        ).toBeTruthy();
    });

    it("renders table subfolder previews on the shared preview surface without an extra bordered frame", async () => {
        const { FileBrowser } = await import("@/components/file-browser");
        const { FileImageThumbnail, FilePreview } =
            await import("@/components/file-preview");
        const files = [
            buildFile("/demo/lanes/lane-1/lane-1-plot.svg", "output"),
            buildFile("/demo/lanes/lane-1/lane-1-notes.tsv", "output"),
            buildFile("/demo/lanes/lane-2/lane-2-plot.svg", "output"),
            buildFile("/demo/lanes/lane-2/lane-2-notes.tsv", "output"),
        ];

        await act(async () => {
            root.render(
                createElement(FileBrowser, {
                    files,
                    onSelectDirectory: vi.fn(),
                    onSelectFile: vi.fn(),
                    renderGridPreview: (file: FileEntry): ReactNode =>
                        file.path.endsWith(".svg")
                            ? createElement(FileImageThumbnail, {
                                  file,
                                  fullSizeUrl: `/api/file?path=${encodeURIComponent(file.path)}`,
                                  height: 200,
                                  thumbnailUrl: `/api/file?path=${encodeURIComponent(file.path)}&thumb=true&w=320&h=200`,
                              })
                            : createElement(FilePreview, {
                                  content: {
                                      content:
                                          "metric\tvalue\nyield\t0.92\nclusters\t184\n",
                                      contentType: "text/tab-separated-values",
                                  },
                                  file,
                                  maxHeight: 200,
                                  proxyUrl: `/api/file?path=${encodeURIComponent(file.path)}`,
                              }),
                    selectedDirectory: "/demo/lanes",
                    visibleFiles: [],
                }),
            );
        });

        const controls = container.querySelector(
            '[data-subdir-preview-controls="/demo/lanes"]',
        );
        const toggle = controls?.querySelector(
            'input[aria-label="Subfolder previews"]',
        ) as HTMLInputElement | null;
        const svgCheckbox = controls?.querySelector(
            'input[data-subdir-preview-kind="svg"]',
        ) as HTMLInputElement | null;
        const tsvCheckbox = controls?.querySelector(
            'input[data-subdir-preview-kind="tsv"]',
        ) as HTMLInputElement | null;

        expect(controls).toBeTruthy();
        expect(toggle).toBeTruthy();
        expect(svgCheckbox?.checked).toBe(true);
        expect(tsvCheckbox?.checked).toBe(true);

        await click(toggle);

        const imageFrame = container.querySelector(
            '[data-subdir-preview-frame="/demo/lanes/lane-1/lane-1-plot.svg"]',
        ) as HTMLElement | null;

        expect(imageFrame).toBeTruthy();
        expect(imageFrame?.className).not.toContain("border");
        expect(imageFrame?.className).not.toContain("rounded-[1.25rem]");

        await click(svgCheckbox);

        const tableFrame = container.querySelector(
            '[data-subdir-preview-frame="/demo/lanes/lane-1/lane-1-notes.tsv"]',
        ) as HTMLElement | null;
        const tableShell = tableFrame?.querySelector(
            "section > div",
        ) as HTMLElement | null;

        expect(tableFrame).toBeTruthy();
        expect(tableFrame?.className).not.toContain("border");
        expect(tableFrame?.className).not.toContain("rounded-[1.25rem]");
        expect(tableShell).toBeTruthy();
        expect(tableShell?.className).toContain("border");
        expect(tableShell?.className).toContain("rounded-[1.75rem]");
    });
});
