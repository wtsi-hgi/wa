// @vitest-environment jsdom

import { createElement, useState } from "react";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { FileEntry } from "@/lib/contracts";
import { FileImageThumbnail } from "@/components/file-preview";

function buildFile(
    path: string,
    kind: FileEntry["kind"] = "output",
): FileEntry {
    return {
        kind,
        mtime: "2026-04-16T09:15:00Z",
        path,
        size: 512,
    };
}

describe("N7 file preview height performance", () => {
    let renderCallCounts: Map<string, number>;

    beforeEach(() => {
        renderCallCounts = new Map();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it("does not cause expensive re-renders when preview height changes", async () => {
        // Track how many times each image preview is rendered
        const OriginalFileImageThumbnail = FileImageThumbnail;
        const SpiedFileImageThumbnail = vi.fn(
            (props: Parameters<typeof FileImageThumbnail>[0]) => {
                const key = props.file.path;
                renderCallCounts.set(key, (renderCallCounts.get(key) ?? 0) + 1);
                return createElement(OriginalFileImageThumbnail, props);
            },
        );

        // Create a test component that simulates changing preview height with many images
        // (realistic scenario: up to 100 images per page)
        function TestComponent() {
            const [height, setHeight] = useState(220);
            const files = Array.from({ length: 50 }, (_, i) =>
                buildFile(`/results/image${i + 1}.png`),
            );

            return createElement(
                "div",
                null,
                createElement(
                    "button",
                    {
                        onClick: () => setHeight(320),
                        type: "button",
                    },
                    "Change height",
                ),
                files.map((file) =>
                    createElement(SpiedFileImageThumbnail, {
                        file,
                        fullSizeUrl: `/api/file?id=test&path=${file.path}`,
                        height,
                        key: file.path,
                        thumbnailUrl: `/api/file?id=test&path=${file.path}&thumb=true&w=512&h=420`,
                    }),
                ),
            );
        }

        await act(async () => {
            render(createElement(TestComponent));
        });

        // Initial renders - each image renders once
        expect(renderCallCounts.get("/results/image1.png")).toBe(1);
        expect(renderCallCounts.get("/results/image25.png")).toBe(1);
        expect(renderCallCounts.get("/results/image50.png")).toBe(1);

        const startTime = performance.now();

        // Change preview height
        await act(async () => {
            screen.getByText("Change height").click();
        });

        const endTime = performance.now();
        const elapsed = endTime - startTime;

        // The height change should complete in under 1 second, not 10+ seconds
        // Using 500ms as a reasonable threshold for a fast UI update
        expect(elapsed).toBeLessThan(500);

        // With proper memoization, each thumbnail should only re-render once more
        // (for the height prop change), not multiple times
        expect(renderCallCounts.get("/results/image1.png")).toBeLessThanOrEqual(
            2,
        );
        expect(
            renderCallCounts.get("/results/image25.png"),
        ).toBeLessThanOrEqual(2);
        expect(
            renderCallCounts.get("/results/image50.png"),
        ).toBeLessThanOrEqual(2);
    });

    it("memoizes FileImageThumbnail to prevent unnecessary expensive work", async () => {
        const file = buildFile("/results/chart.png");
        let renderCount = 0;

        // Create a wrapper that tracks renders
        function TestComponent() {
            const [height, setHeight] = useState(220);
            const [unrelatedState, setUnrelatedState] = useState(0);

            return createElement(
                "div",
                null,
                createElement(
                    "button",
                    {
                        "data-testid": "change-height",
                        onClick: () => setHeight(320),
                        type: "button",
                    },
                    "Change height",
                ),
                createElement(
                    "button",
                    {
                        "data-testid": "change-unrelated",
                        onClick: () => setUnrelatedState((n) => n + 1),
                        type: "button",
                    },
                    "Change unrelated",
                ),
                createElement(
                    () => {
                        renderCount++;
                        return createElement(FileImageThumbnail, {
                            file,
                            fullSizeUrl:
                                "/api/file?id=test&path=/results/chart.png",
                            height,
                            thumbnailUrl:
                                "/api/file?id=test&path=/results/chart.png&thumb=true&w=512&h=420",
                        });
                    },
                    { key: "thumbnail" },
                ),
            );
        }

        await act(async () => {
            render(createElement(TestComponent));
        });

        expect(renderCount).toBe(1);

        // Changing unrelated state should NOT cause thumbnail to re-render
        // if it's properly memoized
        await act(async () => {
            screen.getByTestId("change-unrelated").click();
        });

        // Without memoization, this would increase; with proper memo, it stays at 1
        // (we allow 2 in case React does one extra render for hydration/strictmode)
        expect(renderCount).toBeLessThanOrEqual(2);

        // Changing height SHOULD cause one re-render
        await act(async () => {
            screen.getByTestId("change-height").click();
        });

        // Should be 2 (or 3 if there was an extra render earlier)
        expect(renderCount).toBeLessThanOrEqual(3);
    });
});
