export const previewBitmapImageExtensions = [
    "avif",
    "bmp",
    "gif",
    "jpeg",
    "jpg",
    "png",
    "tif",
    "tiff",
    "webp",
] as const;

export const previewSpecificFileExtensions = [
    "svg",
    "csv",
    "tsv",
    "md",
    "markdown",
    "html",
    "htm",
    "json",
    "log",
    "py",
    "txt",
    "xml",
    "yaml",
    "yml",
    "pdf",
] as const;

export const nonPreviewableBinaryExtensions = [
    "bam",
    "cram",
    "h5",
    "hdf5",
] as const;

const compressedPreviewExtensions = new Set(["gz"]);
const bitmapImageExtensionSet: ReadonlySet<string> = new Set(
    previewBitmapImageExtensions,
);
const specificFileExtensionSet: ReadonlySet<string> = new Set(
    previewSpecificFileExtensions,
);
const inlinePreviewContentBypassExtensionSet: ReadonlySet<string> = new Set([
    ...previewBitmapImageExtensions,
    ...nonPreviewableBinaryExtensions,
    "html",
    "htm",
    "pdf",
    "svg",
]);

export type PreviewSpecificFileExtension =
    (typeof previewSpecificFileExtensions)[number];
export type PreviewFileTypeId = "image" | PreviewSpecificFileExtension;

export type PreviewFileTypeOption = {
    extensions: readonly string[];
    id: PreviewFileTypeId;
    label: string;
};

export const previewFileTypeOptions = [
    {
        extensions: previewBitmapImageExtensions,
        id: "image",
        label: "Images",
    },
    { extensions: ["svg"], id: "svg", label: ".svg" },
    { extensions: ["csv"], id: "csv", label: ".csv" },
    { extensions: ["tsv"], id: "tsv", label: ".tsv" },
    { extensions: ["md"], id: "md", label: ".md" },
    { extensions: ["markdown"], id: "markdown", label: ".markdown" },
    { extensions: ["html"], id: "html", label: ".html" },
    { extensions: ["htm"], id: "htm", label: ".htm" },
    { extensions: ["json"], id: "json", label: ".json" },
    { extensions: ["log"], id: "log", label: ".log" },
    { extensions: ["py"], id: "py", label: ".py" },
    { extensions: ["txt"], id: "txt", label: ".txt" },
    { extensions: ["xml"], id: "xml", label: ".xml" },
    { extensions: ["yaml"], id: "yaml", label: ".yaml" },
    { extensions: ["yml"], id: "yml", label: ".yml" },
    { extensions: ["pdf"], id: "pdf", label: ".pdf" },
] as const satisfies readonly PreviewFileTypeOption[];

export const allPreviewFileTypeIds: readonly PreviewFileTypeId[] =
    previewFileTypeOptions.map((option) => option.id);

export function effectivePreviewExtension(path: string): string {
    const name = path.split("/").pop() ?? path;
    const extensions = name
        .split(".")
        .slice(1)
        .map((extension) => extension.toLowerCase())
        .filter((extension) => extension.length > 0);

    if (extensions.length === 0) {
        return "";
    }

    const lastExtension = extensions.at(-1) ?? "";

    if (
        compressedPreviewExtensions.has(lastExtension) &&
        extensions.length > 1
    ) {
        return extensions.at(-2) ?? lastExtension;
    }

    return lastExtension;
}

export function previewFileTypeForPath(path: string): PreviewFileTypeId | null {
    const extension = effectivePreviewExtension(path);

    if (bitmapImageExtensionSet.has(extension)) {
        return "image";
    }

    if (specificFileExtensionSet.has(extension)) {
        return extension as PreviewSpecificFileExtension;
    }

    return null;
}

export function pathSupportsFilePreview(path: string): boolean {
    return previewFileTypeForPath(path) !== null;
}

export function isBitmapPreviewFile(path: string): boolean {
    return bitmapImageExtensionSet.has(effectivePreviewExtension(path));
}

export function shouldFetchInlinePreviewContent(path: string): boolean {
    return !inlinePreviewContentBypassExtensionSet.has(
        effectivePreviewExtension(path),
    );
}
