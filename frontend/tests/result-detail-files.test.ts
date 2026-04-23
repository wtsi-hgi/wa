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
    FileBrowser: ({
        files,
        onSelectFile,
        selectedPath,
    }: {
        files: FileEntry[];
        onSelectFile: (file: FileEntry) => void;
        selectedPath?: string;
    }) =>
        createElement(
            "div",
            { "data-selected-path": selectedPath ?? "" },
            files.map((file) =>
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

        const frame = screen.getByTitle("HTML preview");

        expect(frame.getAttribute("src")).toBe(
            "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Freport.html",
        );
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
            expect(screen.getByText("Syntax-highlighted preview")).toBeTruthy();
        });

        expect(screen.queryByAltText("plot.svg preview")).toBeNull();
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

        expect(screen.getByText("Loading preview...")).toBeTruthy();

        await waitFor(() => {
            expect(screen.getByText("Syntax-highlighted preview")).toBeTruthy();
        });

        await waitFor(() => {
            expect(screen.queryByText("Loading preview...")).toBeNull();
        });

        expect(screen.getByText(/\"sample\"/i)).toBeTruthy();
        expect(screen.getByText(/\"alpha\"/i)).toBeTruthy();
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
            expect(screen.getByText("Syntax-highlighted preview")).toBeTruthy();
        });

        fireEvent.click(screen.getByRole("button", { name: file.path }));

        expect(screen.queryByText("Loading preview...")).toBeNull();
        expect(screen.getByText("Syntax-highlighted preview")).toBeTruthy();
        expect(fetchMock).toHaveBeenCalledTimes(1);
    });
});
