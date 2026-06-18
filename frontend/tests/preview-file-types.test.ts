import { describe, expect, it } from "vitest";

import {
    effectivePreviewExtension,
    previewFileTypeForPath,
    previewFileTypeOptions,
} from "@/lib/preview-file-types";

describe("preview file types", () => {
    it("lists bitmap Images once and exposes other supported preview extensions directly", () => {
        expect(previewFileTypeOptions.map((option) => option.label)).toEqual([
            "Images",
            ".svg",
            ".csv",
            ".tsv",
            ".md",
            ".markdown",
            ".html",
            ".htm",
            ".json",
            ".log",
            ".py",
            ".txt",
            ".xml",
            ".yaml",
            ".yml",
            ".pdf",
        ]);
        expect(
            previewFileTypeOptions.map((option) => option.label),
        ).not.toEqual(
            expect.arrayContaining([
                "Tables",
                "Markdown",
                "Text & code",
                "Documents",
            ]),
        );
    });

    it("keeps bitmap formats grouped while selecting svg and text-like extensions separately", () => {
        expect(previewFileTypeForPath("/results/plot.png")).toBe("image");
        expect(previewFileTypeForPath("/results/plot.svg")).toBe("svg");
        expect(previewFileTypeForPath("/results/report.csv.gz")).toBe("csv");
        expect(previewFileTypeForPath("/results/notes.md")).toBe("md");
        expect(previewFileTypeForPath("/results/run.log")).toBe("log");
        expect(previewFileTypeForPath("/results/raw.bam")).toBeNull();
    });

    it("normalizes compressed preview paths to the extension the renderer supports", () => {
        expect(effectivePreviewExtension("/results/table.tsv.gz")).toBe("tsv");
        expect(effectivePreviewExtension("/results/archive.gz")).toBe("gz");
        expect(effectivePreviewExtension("/results/no-extension")).toBe("");
    });
});
