// @vitest-environment jsdom

import { createElement } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { FilePreview, type FilePreviewProps } from "@/components/file-preview";
import type { FileEntry } from "@/lib/contracts";

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

afterEach(() => {
    cleanup();
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
        const previewSurface = image.closest("div.inline-flex");

        expect(image.tagName).toBe("IMG");
        expect(previewSurface?.querySelector("svg")).toBeNull();
        expect(
            container.querySelector('img[alt="plot.svg preview"]'),
        ).not.toBeNull();
    });

    it("renders a reusable thumbnail preview with the thumbnail source", async () => {
        const { FileImageThumbnail } = await import(
            "@/components/file-preview"
        );

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

    it("opens the lightbox from the reusable thumbnail with the full-size source", async () => {
        const { FileImageThumbnail } = await import(
            "@/components/file-preview"
        );

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

        fireEvent.click(screen.getByRole("button", { name: /open image lightbox/i }));

        expect(screen.getByRole("dialog")).toBeTruthy();
        expect(screen.getByAltText("plot.png full preview").getAttribute("src")).toBe(
            "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fplot.png",
        );
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

    it("renders binary previews as metadata with a download button", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/sample.bam", size: 1048576 }),
            content: {
                content: "",
                contentType: "application/octet-stream",
            },
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fsample.bam",
        });

        expect(screen.getByText("/tmp/results/sample.bam")).toBeTruthy();
        expect(screen.getByText(/1.0 MB/i)).toBeTruthy();
        expect(screen.getByText(/16 Apr 2026/i)).toBeTruthy();
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

    it("shows the first 100 rows for csv previews with a row count summary", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: buildCsv(200),
                contentType: "text/csv",
            },
            proxyUrl:
                "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.csv",
        });

        expect(screen.getByText("Showing 100 of 200 rows")).toBeTruthy();
        expect(screen.getAllByRole("row")).toHaveLength(101);
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

    it("expands csv previews to show all rows when requested", () => {
        renderPreview({
            file: buildFile({ path: "/tmp/results/report.csv" }),
            content: {
                content: buildCsv(200),
                contentType: "text/csv",
            },
        });

        fireEvent.click(screen.getByRole("button", { name: /show all rows/i }));

        expect(screen.getByText("Showing 200 of 200 rows")).toBeTruthy();
        expect(screen.getAllByRole("row")).toHaveLength(201);
    });

    it("sorts csv rows ascending then descending when a column header is clicked", () => {
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

        const toggle = screen.getByRole("button", { name: /sort by sample/i });

        fireEvent.click(toggle);
        expect(screen.getAllByRole("row")[1]?.textContent).toContain("alpha");
        expect(screen.getAllByRole("row")[3]?.textContent).toContain("gamma");

        fireEvent.click(toggle);
        expect(screen.getAllByRole("row")[1]?.textContent).toContain("gamma");
        expect(screen.getAllByRole("row")[3]?.textContent).toContain("alpha");
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

        fireEvent.change(screen.getByLabelText(/filter rows/i), {
            target: { value: "foo" },
        });

        const rows = screen.getAllByRole("row");
        expect(rows).toHaveLength(2);
        expect(rows[1]?.textContent).toContain("foo");
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
});
