// @vitest-environment jsdom

import { createElement } from "react";
import {
    act,
    cleanup,
    fireEvent,
    render,
    screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const { highlightAutoMock } = vi.hoisted(() => ({
    highlightAutoMock: vi.fn((content: string) => ({
        value: `highlighted:${content}`,
    })),
}));

vi.mock("highlight.js/lib/core", () => ({
    default: {
        highlight: vi.fn((content: string) => ({
            value: `highlighted:${content}`,
        })),
        highlightAuto: highlightAutoMock,
        registerLanguage: vi.fn(),
    },
}));

import { FilePreview, type FilePreviewProps } from "@/components/file-preview";
import type { FileEntry } from "@/lib/contracts";

function mockElementOverflow(
    element: Element,
    {
        clientHeight,
        scrollHeight,
    }: { clientHeight: number; scrollHeight: number },
) {
    Object.defineProperty(element, "clientHeight", {
        configurable: true,
        value: clientHeight,
    });
    Object.defineProperty(element, "scrollHeight", {
        configurable: true,
        value: scrollHeight,
    });
}

function buildFile(overrides: Partial<FileEntry> = {}): FileEntry {
    return {
        kind: "output",
        mtime: "2026-04-16T10:15:00Z",
        path: "/tmp/results/report.txt",
        size: 512,
        ...overrides,
    };
}

function buildCsv(rowCount: number): string {
    const rows = ["sample,status,count"];

    for (let index = 1; index <= rowCount; index += 1) {
        const label = index % 2 === 0 ? `foo-${index}` : `bar-${index}`;
        rows.push(`${label},${index % 3 === 0 ? "pending" : "ready"},${index}`);
    }

    return rows.join("\n");
}

function renderPreview(props: Partial<FilePreviewProps> = {}) {
    return render(
        createElement(FilePreview, {
            file: buildFile(),
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.txt",
            ...props,
        }),
    );
}

const nonImagePreviewCases = [
    {
        content: "# Run report\n\nPreview body",
        contentType: "text/markdown",
        filePath: "/tmp/results/report.md",
        label: "markdown",
    },
    {
        content: "<html><body><h1>Report</h1></body></html>",
        contentType: "text/html",
        filePath: "/tmp/results/report.html",
        label: "HTML",
    },
    {
        content: "<svg><rect width='10' height='10'/></svg>",
        contentType: "image/svg+xml",
        filePath: "/tmp/results/plot.svg",
        label: "SVG",
    },
    {
        content: "",
        contentType: "application/pdf",
        filePath: "/tmp/results/report.pdf",
        label: "PDF",
    },
    {
        content: "const status = 'ready';",
        contentType: "text/plain",
        filePath: "/tmp/results/notes.txt",
        label: "text",
    },
    {
        content: '{"status":"ready"}',
        contentType: "application/json",
        filePath: "/tmp/results/report.json",
        label: "code",
    },
];

function fileNameFromPath(filePath: string): string {
    return filePath.split("/").pop() ?? filePath;
}

afterEach(() => {
    cleanup();
    highlightAutoMock.mockClear();
});

describe("O1 file preview", () => {
    it("returns image for image/png", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("image/png")).toBe("image");
    });

    it("returns csv for text/csv", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("text/csv")).toBe("csv");
    });

    it("returns markdown for text/markdown", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("text/markdown")).toBe("markdown");
    });

    it("returns html for text/html", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("text/html")).toBe("html");
    });

    it("returns svg for image/svg+xml", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("image/svg+xml")).toBe("svg");
    });

    it("returns pdf for application/pdf", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("application/pdf")).toBe("pdf");
    });

    it("returns code for text/x-python", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("text/x-python")).toBe("code");
    });

    it("returns code for application/json", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("application/json")).toBe("code");
    });

    it("returns binary for application/octet-stream", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("application/octet-stream")).toBe("binary");
    });

    it("returns code for text/plain", async () => {
        const { selectRenderer } = await import("@/components/file-preview");

        expect(selectRenderer("text/plain")).toBe("code");
    });

    it("renders html previews in a sandboxed iframe without scripts", () => {
        renderPreview({
            content: {
                content: "<html><body><h1>Report</h1></body></html>",
                contentType: "text/html",
            },
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.html",
        });

        const frame = screen.getByTitle("HTML preview");

        expect(frame.getAttribute("sandbox")).toBe("allow-same-origin");
        expect(frame.getAttribute("sandbox")).not.toContain("allow-scripts");
        expect(frame.getAttribute("src")).toContain(
            "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.html",
        );
        expect(frame.getAttribute("srcdoc")).toBeNull();
    });

    it("renders svg content through an img element rather than inline svg", () => {
        const { container } = renderPreview({
            file: buildFile({ path: "/tmp/results/plot.svg" }),
            content: {
                content: "<svg><rect width='10' height='10'/></svg>",
                contentType: "image/svg+xml",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.svg",
        });

        const image = screen.getByAltText("plot.svg preview");
        const previewSurface = image.parentElement;

        expect(image.tagName).toBe("IMG");
        expect(previewSurface).not.toBeNull();
        expect(previewSurface?.querySelector("rect")).toBeNull();
        expect(
            container.querySelector('img[alt="plot.svg preview"]'),
        ).not.toBeNull();
    });

    it("renders a reusable thumbnail preview with the thumbnail source", async () => {
        const { FileImageThumbnail } =
            await import("@/components/file-preview");

        render(
            createElement(FileImageThumbnail, {
                file: buildFile({ path: "/tmp/results/plot.png" }),
                fullSizeUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png",
                height: 180,
                thumbnailUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png&thumb=true&w=320&h=180",
            }),
        );

        const image = screen.getByAltText("plot.png preview");

        expect(image.getAttribute("src")).toContain("thumb=true");
    });

    it("uses a full-width thumbnail wrapper for row previews", async () => {
        const { FileImageThumbnail } =
            await import("@/components/file-preview");

        render(
            createElement(FileImageThumbnail, {
                file: buildFile({ path: "/tmp/results/plot.png" }),
                fullSizeUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png",
                height: 180,
                thumbnailUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png&thumb=true&w=320&h=180",
            }),
        );

        const image = screen.getByAltText("plot.png preview");
        const wrapper = image.parentElement;

        expect(wrapper?.className).toContain("w-full");
        expect(wrapper?.className).not.toContain("inline-flex");
    });

    it("opens the lightbox from the reusable thumbnail with the full-size source", async () => {
        const { FileImageThumbnail } =
            await import("@/components/file-preview");

        render(
            createElement(FileImageThumbnail, {
                file: buildFile({ path: "/tmp/results/plot.png" }),
                fullSizeUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png",
                height: 180,
                thumbnailUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png&thumb=true&w=320&h=180",
            }),
        );

        fireEvent.click(
            screen.getByRole("button", { name: /open image lightbox/i }),
        );

        expect(screen.getByRole("dialog")).toBeTruthy();
        expect(
            screen.getByAltText("plot.png full preview").getAttribute("src"),
        ).toBe("/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png");
    });

    it("shows a file too large message with download link on 413", () => {
        renderPreview({
            error: {
                fileSize: 20971520,
                status: 413,
            },
        });

        expect(screen.getByText(/File too large for preview/i)).toBeTruthy();
        expect(screen.getByText(/20.0 MB/i)).toBeTruthy();
        expect(
            screen
                .getByRole("link", { name: /download file/i })
                .getAttribute("href"),
        ).toContain("download=true");
    });

    it("shows a generic preview error message with a download link on non-413 failures", () => {
        renderPreview({
            error: {
                message: "file not found on disk",
                status: 410,
            },
        });

        expect(screen.getByText(/unable to load preview/i)).toBeTruthy();
        expect(screen.getByText(/file not found on disk/i)).toBeTruthy();
        expect(
            screen
                .getByRole("link", { name: /download file/i })
                .getAttribute("href"),
        ).toContain("download=true");
    });

    it("renders binary previews with a download button", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/sample.bam", size: 1048576 }),
            content: {
                content: "",
                contentType: "application/octet-stream",
            },
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fsample.bam",
        });

        expect(
            screen.getByText(
                /binary preview is unavailable for this file type/i,
            ),
        ).toBeTruthy();
        expect(
            screen.queryByText(
                /metadata remains available for audit and manual retrieval/i,
            ),
        ).toBeNull();
        expect(
            screen
                .getByRole("link", { name: /download file/i })
                .getAttribute("href"),
        ).toContain("download=true");
    });

    it("shows all available rows for inline csv previews without a row count summary", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: buildCsv(200),
                contentType: "text/csv",
            },
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.csv",
        });

        expect(screen.queryByText("Showing 200 of 200 rows")).toBeNull();
        expect(screen.getAllByRole("row")).toHaveLength(201);
    });

    it("enlarges csv previews on click and shows all rows in the enlarged dialog", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: buildCsv(120),
                contentType: "text/csv",
            },
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.csv",
        });

        expect(screen.queryByText("Showing 120 of 120 rows")).toBeNull();
        expect(screen.queryByRole("dialog")).toBeNull();

        fireEvent.click(
            screen.getByRole("button", { name: /enlarge report.csv preview/i }),
        );

        const dialog = screen.getByRole("dialog", {
            name: /enlarged report.csv preview/i,
        });

        expect(dialog).toBeTruthy();
        expect(screen.getByText("Showing 120 of 120 rows")).toBeTruthy();
        expect(dialog.querySelectorAll("tr")).toHaveLength(121);
    });

    it("shows an explicit loading state before mounting the enlarged csv preview", () => {
        vi.useFakeTimers();

        try {
            renderPreview({
                file: buildFile({ path: "/tmp/results/report.csv" }),
                content: {
                    content: buildCsv(2505),
                    contentType: "text/csv",
                },
            });

            fireEvent.click(
                screen.getByRole("button", {
                    name: /enlarge report.csv preview/i,
                }),
            );

            const dialog = screen.getByRole("dialog", {
                name: /enlarged report.csv preview/i,
            });

            expect(dialog).toBeTruthy();
            expect(screen.getByText(/loading full preview/i)).toBeTruthy();
            expect(
                screen.queryByText("Showing rows 1-1000 of 2505"),
            ).toBeNull();

            act(() => {
                vi.runAllTimers();
            });

            expect(screen.queryByText(/loading full preview/i)).toBeNull();
            expect(
                screen.getByText("Showing rows 1-1000 of 2505"),
            ).toBeTruthy();
        } finally {
            vi.useRealTimers();
        }
    });

    it("paginates large enlarged csv previews to 1000 rows per page", () => {
        vi.useFakeTimers();

        try {
            renderPreview({
                file: buildFile({ path: "/tmp/results/report.csv" }),
                content: {
                    content: buildCsv(2505),
                    contentType: "text/csv",
                },
            });

            fireEvent.click(
                screen.getByRole("button", {
                    name: /enlarge report.csv preview/i,
                }),
            );

            act(() => {
                vi.runAllTimers();
            });

            const dialog = screen.getByRole("dialog", {
                name: /enlarged report.csv preview/i,
            });
            const getDialogBodyRows = () =>
                Array.from(dialog.querySelectorAll("tbody tr")).map(
                    (row) => row.textContent ?? "",
                );

            expect(
                screen.getByText("Showing rows 1-1000 of 2505"),
            ).toBeTruthy();
            expect(screen.getByText("Page 1 of 3")).toBeTruthy();
            expect(dialog.querySelectorAll("tbody tr")).toHaveLength(1000);
            expect(getDialogBodyRows()[0]).toContain("bar-1");
            expect(
                getDialogBodyRows().some((rowText) =>
                    rowText.includes("foo-1002"),
                ),
            ).toBe(false);

            fireEvent.click(screen.getByRole("button", { name: /next page/i }));

            expect(
                screen.getByText("Showing rows 1001-2000 of 2505"),
            ).toBeTruthy();
            expect(screen.getByText("Page 2 of 3")).toBeTruthy();
            expect(dialog.querySelectorAll("tbody tr")).toHaveLength(1000);
            expect(getDialogBodyRows()[0]).toContain("bar-1001");
            expect(getDialogBodyRows()[1]).toContain("foo-1002");
        } finally {
            vi.useRealTimers();
        }
    });

    it("lets users jump to a specific enlarged-preview page via a page selector", () => {
        vi.useFakeTimers();

        try {
            renderPreview({
                file: buildFile({ path: "/tmp/results/report.csv" }),
                content: {
                    content: buildCsv(2505),
                    contentType: "text/csv",
                },
            });

            fireEvent.click(
                screen.getByRole("button", {
                    name: /enlarge report.csv preview/i,
                }),
            );

            act(() => {
                vi.runAllTimers();
            });

            const dialog = screen.getByRole("dialog", {
                name: /enlarged report.csv preview/i,
            });
            const pageSelect = dialog.querySelector(
                'select[aria-label="Preview page"]',
            ) as HTMLSelectElement | null;

            if (!pageSelect) {
                throw new Error("Missing preview page selector");
            }

            expect(pageSelect.options).toHaveLength(3);
            expect(
                Array.from(pageSelect.options).map((option) => option.value),
            ).toEqual(["1", "2", "3"]);

            act(() => {
                fireEvent.change(pageSelect, { target: { value: "3" } });
            });

            expect(
                screen.getByText("Showing rows 2001-2505 of 2505"),
            ).toBeTruthy();
            expect(screen.getByText("Page 3 of 3")).toBeTruthy();
        } finally {
            vi.useRealTimers();
        }
    });

    it.each(nonImagePreviewCases)(
        "enlarges $label previews on click",
        ({ content, contentType, filePath }) => {
            const fileName = fileNameFromPath(filePath);

            renderPreview({
                file: buildFile({ path: filePath }),
                content: { content, contentType },
                proxyUrl: `/api/file?id=result-1&path=${encodeURIComponent(filePath)}`,
            });

            expect(screen.queryByRole("dialog")).toBeNull();

            fireEvent.click(
                screen.getByRole("button", {
                    name: `Enlarge ${fileName} preview`,
                }),
            );

            expect(
                screen.getByRole("dialog", {
                    name: `Enlarged ${fileName} preview`,
                }),
            ).toBeTruthy();
        },
    );

    it("only exposes csv column sorting once the preview is enlarged", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: [
                    "sample,status,count",
                    "gamma,ready,3",
                    "alpha,ready,1",
                    "beta,pending,2",
                ].join("\n"),
                contentType: "text/csv",
            },
        });

        expect(
            screen.queryByRole("button", { name: /sort by sample/i }),
        ).toBeNull();
        expect(screen.getAllByRole("row")[1]?.textContent).toContain("gamma");

        fireEvent.click(
            screen.getByRole("button", { name: /enlarge report.csv preview/i }),
        );

        const toggle = screen.getByRole("button", { name: /sort by sample/i });

        fireEvent.click(toggle);
        const dialog = screen.getByRole("dialog", {
            name: /enlarged report.csv preview/i,
        });
        let rows = dialog.querySelectorAll("tbody tr");

        expect(rows[0]?.textContent).toContain("alpha");
        expect(rows[2]?.textContent).toContain("gamma");
    });

    it("shows a download button for previewable files", () => {
        renderPreview({
            content: {
                content: "# Preview",
                contentType: "text/markdown",
            },
        });

        expect(
            screen
                .getByRole("link", { name: /download file/i })
                .getAttribute("href"),
        ).toContain("download=true");
    });

    it("treats loading json files as previewable before content arrives", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.json" }),
            isLoading: true,
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.json",
        });

        expect(
            screen.queryByText(/inspect the selected asset inline/i),
        ).toBeNull();
        expect(screen.getByText(/loading preview/i)).toBeTruthy();
        expect(
            screen.queryByText(
                /this file type is not previewable in the browser/i,
            ),
        ).toBeNull();
        expect(
            screen
                .getByRole("link", { name: /download file/i })
                .getAttribute("href"),
        ).toContain("download=true");
    });

    it("treats loading gzip-compressed tsv files as previewable before content arrives", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.tsv.gz" }),
            isLoading: true,
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.tsv.gz",
        });

        expect(
            screen.queryByText(/inspect the selected asset inline/i),
        ).toBeNull();
        expect(screen.getByText(/loading preview/i)).toBeTruthy();
        expect(
            screen.queryByText(
                /this file type is not previewable in the browser/i,
            ),
        ).toBeNull();
        expect(
            screen
                .getByRole("link", { name: /download file/i })
                .getAttribute("href"),
        ).toContain("download=true");
    });

    it("does not show inline csv controls before the preview is enlarged", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: buildCsv(200),
                contentType: "text/csv",
            },
        });

        expect(
            screen.queryByRole("button", { name: /show all rows/i }),
        ).toBeNull();
        expect(screen.queryByLabelText(/filter rows/i)).toBeNull();
        expect(screen.queryByText("Showing 200 of 200 rows")).toBeNull();
    });

    it("sorts enlarged csv rows ascending then descending when a column header is clicked", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: [
                    "sample,status,count",
                    "gamma,ready,3",
                    "alpha,ready,1",
                    "beta,pending,2",
                ].join("\n"),
                contentType: "text/csv",
            },
        });

        fireEvent.click(
            screen.getByRole("button", { name: /enlarge report.csv preview/i }),
        );

        const toggle = screen.getByRole("button", { name: /sort by sample/i });

        fireEvent.click(toggle);
        const dialog = screen.getByRole("dialog", {
            name: /enlarged report.csv preview/i,
        });
        let rows = dialog.querySelectorAll("tbody tr");

        expect(rows[0]?.textContent).toContain("alpha");
        expect(rows[2]?.textContent).toContain("gamma");

        fireEvent.click(toggle);
        rows = dialog.querySelectorAll("tbody tr");
        expect(rows[0]?.textContent).toContain("gamma");
        expect(rows[2]?.textContent).toContain("alpha");
    });

    it("filters expanded csv rows by matching text across columns", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: [
                    "sample,status,count",
                    "alpha,ready,1",
                    "foo,pending,2",
                    "charlie,ready,3",
                ].join("\n"),
                contentType: "text/csv",
            },
        });

        fireEvent.click(
            screen.getByRole("button", { name: /enlarge report.csv preview/i }),
        );

        fireEvent.change(screen.getByLabelText(/filter rows/i), {
            target: { value: "foo" },
        });

        const dialog = screen.getByRole("dialog", {
            name: /enlarged report.csv preview/i,
        });
        const rows = dialog.querySelectorAll("tbody tr");

        expect(rows).toHaveLength(1);
        expect(rows[0]?.textContent).toContain("foo");
    });

    it("renders image previews as constrained thumbnails", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/image.png" }),
            content: {
                content: "",
                contentType: "image/png",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fimage.png",
        });

        const image = screen.getByAltText("image.png preview");

        expect(image.getAttribute("src")).toContain("/api/file?");
        expect(image.getAttribute("style")).toContain("max-width: 320px");
        expect(image.getAttribute("style")).toContain("max-height: 240px");
    });

    it("keeps the enlarge icon without rendering the enlarge text on image previews", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/image.png" }),
            content: {
                content: "",
                contentType: "image/png",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fimage.png",
        });

        const lightboxButton = screen.getByRole("button", {
            name: /open image lightbox/i,
        });
        const surface = lightboxButton.parentElement;

        expect(surface?.querySelector("span svg")).not.toBeNull();
        expect(screen.queryByText("Click to enlarge")).toBeNull();
    });

    it("renders the download button as an overlay on the single-preview image surface", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/image.png" }),
            content: {
                content: "",
                contentType: "image/png",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fimage.png",
        });

        const image = screen.getByAltText("image.png preview");
        const lightboxButton = screen.getByRole("button", {
            name: /open image lightbox/i,
        });
        const link = screen.getByRole("link", { name: /download file/i });
        const surface = image.closest("div.group.relative");

        expect(link.getAttribute("href")).toContain("download=true");
        expect(link.className).toContain("absolute");
        expect(surface).not.toBeNull();
        expect(lightboxButton.parentElement).toBe(surface);
        expect(link.parentElement?.className).toContain(
            "relative flex w-full justify-center",
        );
        expect(surface?.contains(image)).toBe(true);
        expect(surface?.contains(lightboxButton)).toBe(true);
        expect(surface?.contains(link)).toBe(true);
        expect(surface?.className).toContain("cursor-zoom-in");
        expect(surface?.textContent).not.toContain("Click to enlarge");
    });

    it("uses the preview shell radius for single-preview images instead of a nested image radius", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/image.png" }),
            content: {
                content: "",
                contentType: "image/png",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fimage.png",
        });

        const image = screen.getByAltText("image.png preview");
        const surface = image.closest("div.group.relative");

        expect(surface?.className).toContain("rounded-[1.5rem]");
        expect(image.className).toContain("rounded-[inherit]");
        expect(image.className).not.toContain("rounded-xl");
    });

    it("opens a lightbox overlay when the image thumbnail is clicked", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/image.png" }),
            content: {
                content: "",
                contentType: "image/png",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fimage.png",
        });

        fireEvent.click(
            screen.getByRole("button", { name: /open image lightbox/i }),
        );

        expect(
            screen.getByRole("dialog", { name: /image preview lightbox/i }),
        ).toBeTruthy();
        expect(screen.getByAltText("image.png full preview")).toBeTruthy();
    });

    it("closes the image lightbox on backdrop click or escape", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/image.png" }),
            content: {
                content: "",
                contentType: "image/png",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fimage.png",
        });

        fireEvent.click(
            screen.getByRole("button", { name: /open image lightbox/i }),
        );
        fireEvent.click(screen.getByLabelText(/close image preview backdrop/i));
        expect(
            screen.queryByRole("dialog", { name: /image preview lightbox/i }),
        ).toBeNull();

        fireEvent.click(
            screen.getByRole("button", { name: /open image lightbox/i }),
        );
        fireEvent.keyDown(window, { key: "Escape" });
        expect(
            screen.queryByRole("dialog", { name: /image preview lightbox/i }),
        ).toBeNull();
    });

    it("does not recompute syntax highlighting when a loaded code preview rerenders unchanged", () => {
        const props: FilePreviewProps = {
            content: {
                content: '{"status":"ready"}',
                contentType: "text/plain",
            },
            file: buildFile({ path: "/tmp/results/report.log" }),
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.log",
        };
        const rendered = render(createElement(FilePreview, props));

        expect(highlightAutoMock).toHaveBeenCalledTimes(1);

        rendered.rerender(createElement(FilePreview, props));

        expect(highlightAutoMock).toHaveBeenCalledTimes(1);
    });

    it("does not repeat the file name or render a 'Preview' eyebrow in the single preview header", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.txt" }),
            content: { content: "hello", contentType: "text/plain" },
        });

        expect(
            screen.queryByRole("heading", { name: "report.txt" }),
        ).toBeNull();
        expect(screen.queryByText(/^preview$/i, { selector: "p" })).toBeNull();
    });

    it("renders an icon-only download anchor on the single preview", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.txt" }),
            content: { content: "hello", contentType: "text/plain" },
        });

        const link = screen.getByRole("link", { name: /download file/i });

        expect(link.getAttribute("aria-label")).toBe("Download file");
        expect(link.textContent?.trim()).toBe("");
        expect(link.querySelector("svg")).not.toBeNull();
    });

    it("uses a full-size outer surface so browser panels do not need wrapper boxes", () => {
        const { container } = renderPreview({
            file: buildFile({ path: "/tmp/results/report.txt" }),
            content: { content: "hello", contentType: "text/plain" },
        });

        const root = container.querySelector("section");
        const surface = root?.firstElementChild;

        expect(root?.className).toContain("h-full");
        expect(root?.className).toContain("w-full");
        expect(surface?.className).toContain("h-full");
        expect(surface?.className).toContain("w-full");
    });

    it("renders html previews without a nested bordered iframe shell", () => {
        const { container } = renderPreview({
            file: buildFile({ path: "/tmp/results/report.html" }),
            content: {
                content: "<html><body><h1>Report</h1></body></html>",
                contentType: "text/html",
            },
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.html",
        });

        const surface = container.querySelector("section > div");
        const frame = screen.getByTitle("HTML preview");

        expect(surface?.className).not.toContain("p-5");
        expect(frame.className).not.toContain("border");
        expect(frame.className).not.toContain("rounded-[1.5rem]");
    });

    it("renders text previews directly on the shared surface without a bordered inner box", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.txt" }),
            content: { content: "hello", contentType: "text/plain" },
        });

        const codeBlock = screen.getByText("highlighted:hello");
        const shell = codeBlock.closest("div");

        expect(shell?.className).not.toContain("border");
        expect(shell?.className).not.toContain("rounded-[1.5rem]");
    });

    it("only shows the truncation fade for code previews when inline content actually overflows", () => {
        const { container, rerender } = renderPreview({
            file: buildFile({ path: "/tmp/results/report.txt" }),
            content: { content: "line 1\nline 2", contentType: "text/plain" },
            maxHeight: 120,
        });

        const initialPre = container.querySelector("pre");

        if (!initialPre) {
            throw new Error("Missing code preview element");
        }

        mockElementOverflow(initialPre, {
            clientHeight: 120,
            scrollHeight: 120,
        });

        act(() => {
            window.dispatchEvent(new Event("resize"));
        });

        expect(screen.queryByLabelText(/content truncated/i)).toBeNull();

        rerender(
            createElement(FilePreview, {
                file: buildFile({ path: "/tmp/results/report.txt" }),
                proxyUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.txt",
                content: {
                    content: "line 1\nline 2\nline 3\nline 4",
                    contentType: "text/plain",
                },
                maxHeight: 120,
            }),
        );

        const overflowPre = container.querySelector("pre");

        if (!overflowPre) {
            throw new Error("Missing rerendered code preview element");
        }

        mockElementOverflow(overflowPre, {
            clientHeight: 120,
            scrollHeight: 220,
        });

        act(() => {
            window.dispatchEvent(new Event("resize"));
        });

        expect(screen.getByLabelText(/content truncated/i)).toBeTruthy();
    });

    it("shows the truncation fade for csv previews when preview height clips the table locally", () => {
        const props: Partial<FilePreviewProps> = {
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: buildCsv(20),
                contentType: "text/csv",
            },
            maxHeight: 120,
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.csv",
        };
        const { container, rerender } = renderPreview(props);

        const tableWrapper = container.querySelector(
            "div.h-full.overflow-hidden",
        );

        if (!tableWrapper) {
            throw new Error("Missing inline csv wrapper");
        }

        mockElementOverflow(tableWrapper, {
            clientHeight: 120,
            scrollHeight: 260,
        });

        rerender(
            createElement(FilePreview, {
                file: props.file ?? buildFile(),
                proxyUrl:
                    props.proxyUrl ??
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.csv",
                content: props.content,
                maxHeight: 119,
            }),
        );

        const rerenderedWrapper = container.querySelector(
            "div.h-full.overflow-hidden",
        );

        if (!rerenderedWrapper) {
            throw new Error("Missing rerendered inline csv wrapper");
        }

        mockElementOverflow(rerenderedWrapper, {
            clientHeight: 119,
            scrollHeight: 260,
        });

        act(() => {
            window.dispatchEvent(new Event("resize"));
        });

        expect(screen.getByLabelText(/content truncated/i)).toBeTruthy();
    });

    it("renders svg previews without a nested bordered frame", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/plot.svg" }),
            content: {
                content: "<svg><rect width='10' height='10'/></svg>",
                contentType: "image/svg+xml",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.svg",
        });

        const image = screen.getByAltText("plot.svg preview");
        const surface = image.closest("div");

        expect(surface?.className).not.toContain("border");
        expect(surface?.className).not.toContain("p-3");
    });

    it("uses an icon-only download anchor on the 413 too-large branch without a 'Preview' eyebrow", () => {
        renderPreview({
            error: { fileSize: 20971520, status: 413 },
        });

        const link = screen.getByRole("link", { name: /download file/i });

        expect(link.textContent?.trim()).toBe("");
        expect(link.querySelector("svg")).not.toBeNull();
    });

    it("uses an icon-only download anchor on generic preview errors", () => {
        renderPreview({
            error: { message: "boom", status: 410 },
        });

        const link = screen.getByRole("link", { name: /download file/i });

        expect(link.textContent?.trim()).toBe("");
        expect(link.querySelector("svg")).not.toBeNull();
    });

    it("does not render the file name or an 'Image preview' eyebrow on FileImageThumbnail", async () => {
        const { FileImageThumbnail } =
            await import("@/components/file-preview");

        render(
            createElement(FileImageThumbnail, {
                file: buildFile({ path: "/tmp/results/plot.png" }),
                fullSizeUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png",
                height: 180,
                thumbnailUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png&thumb=true",
            }),
        );

        expect(screen.queryByRole("heading", { name: "plot.png" })).toBeNull();
        expect(
            screen.queryByText(/image preview/i, { selector: "p" }),
        ).toBeNull();
    });

    it("renders a download overlay on FileImageThumbnail previews", async () => {
        const { FileImageThumbnail } =
            await import("@/components/file-preview");

        render(
            createElement(FileImageThumbnail, {
                file: buildFile({ path: "/tmp/results/plot.png" }),
                fullSizeUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png",
                height: 180,
                thumbnailUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png&thumb=true",
            }),
        );

        const image = screen.getByAltText("plot.png preview");
        const link = screen.getByRole("link", { name: /download file/i });
        const surface = image.closest("div.group.relative");

        expect(link.getAttribute("href")).toContain("download=true");
        expect(link.className).toContain("absolute");
        expect(surface).not.toBeNull();
        expect(link.parentElement?.parentElement).toBe(surface);
        expect(surface?.contains(image)).toBe(true);
        expect(surface?.contains(link)).toBe(true);
        expect(surface?.className).toContain("cursor-zoom-in");
        expect(surface?.textContent).not.toContain("Click to enlarge");
    });

    it("uses a pointer affordance without enlarge text for non-image previews", () => {
        for (const { content, contentType, filePath } of nonImagePreviewCases) {
            const { container } = renderPreview({
                file: buildFile({ path: filePath }),
                content: { content, contentType },
                proxyUrl: `/api/file?id=result-1&path=${encodeURIComponent(filePath)}`,
            });

            const surface = container.querySelector("div.group.relative");

            expect(surface?.className).toContain("cursor-zoom-in");
            expect(surface?.textContent).not.toContain("Click to enlarge");
            expect(
                screen.getByRole("button", {
                    name: new RegExp(
                        `enlarge ${fileNameFromPath(filePath)} preview`,
                        "i",
                    ),
                }),
            ).toBeTruthy();
            cleanup();
        }
    });

    it("uses the thumbnail shell radius for grid previews instead of a nested image radius", async () => {
        const { FileImageThumbnail } =
            await import("@/components/file-preview");

        render(
            createElement(FileImageThumbnail, {
                file: buildFile({ path: "/tmp/results/plot.png" }),
                fullSizeUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png",
                height: 180,
                thumbnailUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png&thumb=true",
            }),
        );

        const image = screen.getByAltText("plot.png preview");
        const surface = image.closest("div.group.relative");

        expect(surface?.className).toContain("rounded-[1.25rem]");
        expect(image.className).toContain("rounded-[inherit]");
        expect(image.className).not.toContain("rounded-xl");
    });

    it("applies maxHeight to single-mode image previews so the slider takes effect", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/image.png" }),
            content: { content: "", contentType: "image/png" },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fimage.png",
            maxHeight: 180,
        });

        const image = screen.getByAltText("image.png preview");

        expect(image.getAttribute("style")).toContain("max-height: 180px");
    });

    it("keeps FileImageThumbnail intrinsic image dimensions stable when height prop changes", async () => {
        const { FileImageThumbnail } =
            await import("@/components/file-preview");
        const sharedProps = {
            file: buildFile({ path: "/tmp/results/plot.png" }),
            fullSizeUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png",
            thumbnailUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png&thumb=true&w=672&h=420",
        };

        const small = render(
            createElement(FileImageThumbnail, {
                ...sharedProps,
                height: 160,
            }),
        );
        const smallImage = screen.getByAltText("plot.png preview");
        const smallWidth = smallImage.getAttribute("width");
        const smallHeight = smallImage.getAttribute("height");
        const smallStyle = smallImage.getAttribute("style") ?? "";

        small.unmount();

        render(
            createElement(FileImageThumbnail, {
                ...sharedProps,
                height: 360,
            }),
        );
        const largeImage = screen.getByAltText("plot.png preview");

        expect(largeImage.getAttribute("width")).toBe(smallWidth);
        expect(largeImage.getAttribute("height")).toBe(smallHeight);
        expect(smallStyle).toContain("max-height: 160px");
        expect(largeImage.getAttribute("style")).toContain("max-height: 360px");
    });

    it("shows all inline csv rows for small files even when maxHeight is set", () => {
        const csvContent = buildCsv(50);

        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: csvContent,
                contentType: "text/csv",
            },
            maxHeight: 220,
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.csv",
        });

        const rows = screen.getAllByRole("row");
        expect(rows.length).toBe(51);
        expect(screen.queryByText("Showing 50 of 50 rows")).toBeNull();
    });

    it("shows the same inline csv rows regardless of maxHeight until backend truncation occurs", () => {
        const csvContent = buildCsv(20);

        const small = renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: csvContent,
                contentType: "text/csv",
            },
            maxHeight: 150,
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.csv",
        });

        const smallRowCount = screen.getAllByRole("row").length;
        expect(screen.queryByText("Showing 20 of 20 rows")).toBeNull();
        small.unmount();

        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: csvContent,
                contentType: "text/csv",
            },
            maxHeight: 400,
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.csv",
        });

        const largeRowCount = screen.getAllByRole("row").length;

        expect(smallRowCount).toBe(21);
        expect(largeRowCount).toBe(21);
        expect(screen.queryByText("Showing 20 of 20 rows")).toBeNull();
    });

    it("constrained markdown preview shows the truncation indicator when the preview content is marked truncated", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/readme.md" }),
            content: {
                content: "# Title\n\n" + "Line content\n".repeat(50),
                contentType: "text/markdown",
                truncated: true,
            },
            maxHeight: 200,
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freadme.md",
        });

        const article = screen.getByText(/Title/i).closest("article");

        expect(article).not.toBeNull();
        expect(article?.className).not.toContain("overflow-auto");
        expect(article?.className).not.toContain("overflow-y-auto");
        expect(article?.className).toContain("overflow-hidden");

        const truncationIndicator = screen.getByLabelText(/content truncated/i);
        expect(truncationIndicator).toBeTruthy();
        expect(truncationIndicator.getAttribute("data-truncated")).toBe("true");
    });

    it("constrained code preview shows the truncation indicator when the preview content is marked truncated", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/script.py" }),
            content: {
                content: "def function():\n    " + "pass\n".repeat(50),
                contentType: "text/x-python",
                truncated: true,
            },
            maxHeight: 200,
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fscript.py",
        });

        const pre = screen.getByText(/highlighted:/i).closest("pre");

        expect(pre).not.toBeNull();
        expect(pre?.className).not.toContain("overflow-auto");
        expect(pre?.className).not.toContain("overflow-y-auto");
        expect(pre?.className).toContain("overflow-hidden");

        const truncationIndicator = screen.getByLabelText(/content truncated/i);
        expect(truncationIndicator).toBeTruthy();
        expect(truncationIndicator.getAttribute("data-truncated")).toBe("true");
    });

    it("inline csv preview does not show a truncation indicator when the backend response is complete and the selected height fits the table", () => {
        const { container } = renderPreview({
            file: buildFile({ path: "/tmp/results/data.csv" }),
            content: {
                content: buildCsv(2),
                contentType: "text/csv",
            },
            maxHeight: 200,
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fdata.csv",
        });

        // Find the container with overflow-hidden class
        const overflowHiddenContainer =
            container.querySelector(".overflow-hidden");
        expect(overflowHiddenContainer).not.toBeNull();

        expect(screen.queryByLabelText(/content truncated/i)).toBeNull();
        expect(
            screen.queryByText(/preview truncated after the first lines/i),
        ).toBeNull();
    });

    it("does not show a truncation note when enlarged preview can expose the remaining content", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/data.tsv" }),
            content: {
                content: "sample\tstatus\nalpha\tready\n",
                contentType: "text/tab-separated-values",
                truncated: true,
            },
            maxHeight: 220,
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fdata.tsv",
        });

        expect(
            screen.queryByText(/preview truncated after the first lines/i),
        ).toBeNull();
    });

    it("shows the same inline tsv rows for truncated previews regardless of maxHeight", () => {
        const tsvContent = buildCsv(20).replaceAll(",", "\t");

        const small = renderPreview({
            file: buildFile({ path: "/tmp/results/data.tsv" }),
            content: {
                content: tsvContent,
                contentType: "text/tab-separated-values",
                truncated: true,
            },
            maxHeight: 150,
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fdata.tsv",
        });

        const smallRowCount = screen.getAllByRole("row").length;

        expect(
            screen.queryByText(/preview truncated after the first lines/i),
        ).toBeNull();

        small.unmount();

        renderPreview({
            file: buildFile({ path: "/tmp/results/data.tsv" }),
            content: {
                content: tsvContent,
                contentType: "text/tab-separated-values",
                truncated: true,
            },
            maxHeight: 400,
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fdata.tsv",
        });

        const largeRowCount = screen.getAllByRole("row").length;

        expect(smallRowCount).toBe(21);
        expect(largeRowCount).toBe(21);
    });

    it("applies maxHeight to complete inline tsv previews with an invisible layout wrapper", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/data.tsv" }),
            content: {
                content: buildCsv(20).replaceAll(",", "\t"),
                contentType: "text/tab-separated-values",
            },
            maxHeight: 220,
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fdata.tsv",
        });

        const table = screen.getByRole("table");
        const heightWrapper = table.closest(
            'div[style*="max-height: 220px"]',
        ) as HTMLElement | null;

        expect(heightWrapper).toBeTruthy();
        expect(heightWrapper?.className).toContain("overflow-hidden");
        expect(heightWrapper?.className).not.toContain("border");
        expect(heightWrapper?.className).not.toContain("rounded");
    });

    it("keeps enlarged pagination at 1000 rows for large delimited previews", () => {
        vi.useFakeTimers();

        try {
            renderPreview({
                file: buildFile({ path: "/tmp/results/data.tsv" }),
                content: {
                    content: buildCsv(2505),
                    contentType: "text/tab-separated-values",
                    truncated: true,
                },
                maxHeight: 220,
                proxyUrl:
                    "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fdata.tsv",
            });

            expect(screen.queryByText("Showing 3 preview rows")).toBeNull();
            expect(screen.getAllByRole("row")).toHaveLength(2506);

            fireEvent.click(
                screen.getByRole("button", {
                    name: /enlarge data.tsv preview/i,
                }),
            );

            act(() => {
                vi.runAllTimers();
            });

            const dialog = screen.getByRole("dialog", {
                name: /enlarged data.tsv preview/i,
            });

            expect(
                screen.getByText("Showing rows 1-1000 of 2505"),
            ).toBeTruthy();
            expect(screen.getByText("Page 1 of 3")).toBeTruthy();
            expect(dialog.querySelectorAll("tbody tr")).toHaveLength(1000);
        } finally {
            vi.useRealTimers();
        }
    });

    it("shows all 20 inline csv rows when the backend response is not truncated", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/reports/report.csv" }),
            content: {
                content: buildCsv(20),
                contentType: "text/csv",
            },
            maxHeight: 220,
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freports%2Freport.csv",
        });

        const rows = screen.getAllByRole("row");
        expect(rows.length).toBe(21);
        expect(screen.queryByText("Showing 20 of 20 rows")).toBeNull();
    });

    it("shows all rows for small csv when maxHeight is undefined (fallback behavior)", () => {
        // This test documents the current behavior when maxHeight is not provided.
        // Without maxHeight, the component falls back to showing min(100, rowCount).
        // This is what happens in GalleryPreviewRow which doesn't pass maxHeight.
        renderPreview({
            file: buildFile({ path: "/tmp/results/reports/report.csv" }),
            content: {
                content: buildCsv(20),
                contentType: "text/csv",
            },
            // No maxHeight prop
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freports%2Freport.csv",
        });

        const rows = screen.getAllByRole("row");
        // Without maxHeight, shows all 20 data rows + 1 header = 21 total
        expect(rows.length).toBe(21);
    });

    it("enlarged code preview allows scrolling with overflow-auto", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/script.py" }),
            content: {
                content: "def function():\n    " + "pass\n".repeat(50),
                contentType: "text/x-python",
            },
            maxHeight: 200,
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fscript.py",
        });

        fireEvent.click(
            screen.getByRole("button", {
                name: /enlarge script.py preview/i,
            }),
        );

        const dialog = screen.getByRole("dialog");
        const enlargedPre = dialog.querySelector("pre");

        expect(enlargedPre).not.toBeNull();
        expect(enlargedPre?.className).toContain("overflow-auto");
    });

    it("enlarged markdown preview allows scrolling in dialog wrapper", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/readme.md" }),
            content: {
                content: "# Title\n\n" + "Line content\n".repeat(50),
                contentType: "text/markdown",
            },
            maxHeight: 200,
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freadme.md",
        });

        fireEvent.click(
            screen.getByRole("button", {
                name: /enlarge readme.md preview/i,
            }),
        );

        const dialog = screen.getByRole("dialog");
        const dialogInner = dialog.querySelector(
            ".max-h-\\[calc\\(100vh-8rem\\)\\]",
        );

        expect(dialogInner).not.toBeNull();
        expect(dialogInner?.className).toContain("overflow-auto");
    });

    it("does not display syntax-highlighted preview banner on JSON files", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/data.json" }),
            content: {
                content: '{"status": "ready", "count": 42}',
                contentType: "application/json",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fdata.json",
        });

        expect(screen.queryByText(/syntax-highlighted preview/i)).toBeNull();
    });

    it("does not display syntax-highlighted preview banner on code files", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/script.py" }),
            content: {
                content: "def main():\n    print('hello')",
                contentType: "text/x-python",
            },
            proxyUrl: "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fscript.py",
        });

        expect(screen.queryByText(/syntax-highlighted preview/i)).toBeNull();
    });
});
