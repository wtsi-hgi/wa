// @vitest-environment jsdom

import { createElement } from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { FileEntry } from "@/lib/contracts";

const fetchMock = vi.fn<typeof fetch>();

vi.stubGlobal("fetch", fetchMock);

vi.mock("@/components/file-browser", () => ({
    FileBrowser: ({ selectedPath }: { selectedPath?: string }) =>
        createElement("div", { "data-selected-path": selectedPath ?? "" }),
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
});
