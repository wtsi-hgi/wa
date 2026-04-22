"use client";

import { useEffect, useMemo, useState } from "react";
import { ArrowDownToLine, Expand, FileCode2, Search, X } from "lucide-react";
import hljs from "highlight.js/lib/core";
import json from "highlight.js/lib/languages/json";
import markdownLanguage from "highlight.js/lib/languages/markdown";
import plaintext from "highlight.js/lib/languages/plaintext";
import python from "highlight.js/lib/languages/python";
import xml from "highlight.js/lib/languages/xml";
import ReactMarkdown from "react-markdown";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { FileEntry } from "@/lib/contracts";
import { formatBytes } from "@/lib/utils";

hljs.registerLanguage("json", json);
hljs.registerLanguage("markdown", markdownLanguage);
hljs.registerLanguage("plaintext", plaintext);
hljs.registerLanguage("python", python);
hljs.registerLanguage("xml", xml);

const nonPreviewableExtensions = new Set(["bam", "cram", "h5", "hdf5"]);
const imageExtensions = new Set([
  "png",
  "jpg",
  "jpeg",
  "gif",
  "webp",
  "bmp",
  "tif",
  "tiff",
  "avif",
]);

export type PreviewRenderer =
  | "image"
  | "csv"
  | "markdown"
  | "html"
  | "svg"
  | "pdf"
  | "code"
  | "binary";

export type FilePreviewError = {
  fileSize?: number;
  message?: string;
  status: number;
};

export type FilePreviewProps = {
  file: FileEntry;
  content?: { content: string; contentType: string };
  error?: FilePreviewError;
  isLoading?: boolean;
  proxyUrl: string;
};

type ParsedTable = {
  headers: string[];
  rows: string[][];
};

type SortDirection = "asc" | "desc";

function normalizeContentType(contentType: string): string {
  return (
    contentType.split(";")[0]?.trim().toLowerCase() ??
    "application/octet-stream"
  );
}

function extensionFromPath(path: string): string {
  const name = path.split("/").pop() ?? path;
  const index = name.lastIndexOf(".");

  if (index === -1 || index === name.length - 1) {
    return "";
  }

  return name.slice(index + 1).toLowerCase();
}

function guessRendererFromPath(path: string): PreviewRenderer {
  const extension = extensionFromPath(path);

  if (extension === "svg") {
    return "svg";
  }

  if (extension === "json") {
    return "code";
  }

  if (imageExtensions.has(extension)) {
    return "image";
  }

  if (extension === "pdf") {
    return "pdf";
  }

  return "binary";
}

function isPreviewable(renderer: PreviewRenderer, path: string): boolean {
  return (
    renderer !== "binary" &&
    !nonPreviewableExtensions.has(extensionFromPath(path))
  );
}

function buildDownloadUrl(proxyUrl: string): string {
  if (proxyUrl.includes("download=true")) {
    return proxyUrl;
  }

  return `${proxyUrl}${proxyUrl.includes("?") ? "&" : "?"}download=true`;
}

function formatTimestamp(value: string): string {
  const date = new Date(value);

  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return date.toLocaleString("en-GB", {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    timeZone: "UTC",
  });
}

function parseDelimitedContent(
  content: string,
  contentType: string,
): ParsedTable {
  const delimiter = normalizeContentType(contentType).startsWith("text/tab-")
    ? "\t"
    : ",";
  const lines = content
    .split(/\r?\n/)
    .map((line) => line.trimEnd())
    .filter((line) => line.length > 0);

  if (lines.length === 0) {
    return { headers: [], rows: [] };
  }

  const [headerLine, ...rowLines] = lines;

  return {
    headers: headerLine.split(delimiter).map((cell) => cell.trim()),
    rows: rowLines.map((line) =>
      line.split(delimiter).map((cell) => cell.trim()),
    ),
  };
}

function inferHighlightLanguage(contentType: string): string | undefined {
  const normalized = normalizeContentType(contentType);

  if (normalized === "application/json") {
    return "json";
  }

  if (normalized === "text/markdown") {
    return "markdown";
  }

  if (normalized === "text/html") {
    return "xml";
  }

  if (normalized === "text/x-python") {
    return "python";
  }

  return undefined;
}

function highlightCode(content: string, contentType: string): string {
  const language = inferHighlightLanguage(contentType);

  if (language) {
    return hljs.highlight(content, { ignoreIllegals: true, language }).value;
  }

  return hljs.highlightAuto(content).value;
}

function DownloadButton({ href }: { href: string }) {
  return (
    <a
      className="inline-flex items-center justify-center gap-2 rounded-full border border-border/70 bg-accent/15 px-4 py-2 text-sm font-medium text-foreground transition hover:border-primary/35 hover:bg-accent/25"
      href={href}
    >
      <ArrowDownToLine className="size-4" aria-hidden="true" />
      Download file
    </a>
  );
}

function MetadataPanel({ file }: { file: FileEntry }) {
  return (
    <div className="space-y-4">
      <div className="rounded-[1.5rem] border border-border/70 bg-background/75 p-4">
        <p className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
          Path
        </p>
        <p className="mt-2 break-all font-mono text-sm text-foreground">
          {file.path}
        </p>
      </div>

      <dl className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <div className="rounded-[1.5rem] border border-border/70 bg-background/75 p-4">
          <dt className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
            Kind
          </dt>
          <dd className="mt-2 text-sm text-foreground">{file.kind}</dd>
        </div>
        <div className="rounded-[1.5rem] border border-border/70 bg-background/75 p-4">
          <dt className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
            Size
          </dt>
          <dd className="mt-2 text-sm text-foreground">
            {formatBytes(file.size)}
          </dd>
        </div>
        <div className="rounded-[1.5rem] border border-border/70 bg-background/75 p-4">
          <dt className="text-xs font-semibold uppercase tracking-[0.22em] text-muted-foreground">
            Updated
          </dt>
          <dd className="mt-2 text-sm text-foreground">
            {formatTimestamp(file.mtime)}
          </dd>
        </div>
      </dl>
    </div>
  );
}

function CsvPreview({
  content,
  contentType,
}: {
  content: string;
  contentType: string;
}) {
  const parsed = useMemo(
    () => parseDelimitedContent(content, contentType),
    [content, contentType],
  );
  const [showAllRows, setShowAllRows] = useState(false);
  const [filterValue, setFilterValue] = useState("");
  const [sortIndex, setSortIndex] = useState<number | null>(null);
  const [sortDirection, setSortDirection] = useState<SortDirection>("asc");

  const filteredRows = useMemo(() => {
    const normalizedFilter = filterValue.trim().toLowerCase();

    if (!normalizedFilter) {
      return parsed.rows;
    }

    return parsed.rows.filter((row) =>
      row.some((cell) => cell.toLowerCase().includes(normalizedFilter)),
    );
  }, [filterValue, parsed.rows]);

  const sortedRows = useMemo(() => {
    if (sortIndex === null) {
      return filteredRows;
    }

    return [...filteredRows].sort((left, right) => {
      const leftValue = left[sortIndex] ?? "";
      const rightValue = right[sortIndex] ?? "";
      const numericLeft = Number(leftValue);
      const numericRight = Number(rightValue);
      const bothNumeric =
        !Number.isNaN(numericLeft) && !Number.isNaN(numericRight);
      const order = bothNumeric
        ? numericLeft - numericRight
        : leftValue.localeCompare(rightValue, undefined, {
            numeric: true,
            sensitivity: "base",
          });

      return sortDirection === "asc" ? order : -order;
    });
  }, [filteredRows, sortDirection, sortIndex]);

  const visibleRows = showAllRows ? sortedRows : sortedRows.slice(0, 100);

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <p className="text-sm text-muted-foreground">
          Showing {visibleRows.length} of {parsed.rows.length} rows
        </p>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <label className="relative block">
            <span className="sr-only">Filter rows</span>
            <Search
              className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
              aria-hidden="true"
            />
            <input
              aria-label="Filter rows"
              className="w-full rounded-full border border-border/70 bg-background px-10 py-2 text-sm text-foreground outline-none transition focus:border-primary sm:w-64"
              onChange={(event) => setFilterValue(event.target.value)}
              placeholder="Filter rows"
              value={filterValue}
            />
          </label>

          {parsed.rows.length > 100 ? (
            <button
              type="button"
              className="inline-flex items-center justify-center rounded-full border border-border/70 bg-background px-4 py-2 text-sm font-medium text-foreground transition hover:border-primary/35"
              onClick={() => setShowAllRows((current) => !current)}
            >
              {showAllRows ? "Show first 100 rows" : "Show all rows"}
            </button>
          ) : null}
        </div>
      </div>

      <div className="rounded-[1.5rem] border border-border/70 bg-background/70 p-2">
        <Table>
          <TableHeader>
            <TableRow>
              {parsed.headers.map((header, index) => (
                <TableHead key={`${header}-${index}`}>
                  <button
                    type="button"
                    aria-label={`Sort by ${header}`}
                    className="inline-flex items-center gap-2 rounded-full px-2 py-1 text-left text-sm font-medium text-foreground transition hover:bg-muted/50"
                    onClick={() => {
                      if (sortIndex === index) {
                        setSortDirection((current) =>
                          current === "asc" ? "desc" : "asc",
                        );
                        return;
                      }

                      setSortIndex(index);
                      setSortDirection("asc");
                    }}
                  >
                    <span>{header}</span>
                    {sortIndex === index ? (
                      <span className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                        {sortDirection}
                      </span>
                    ) : null}
                  </button>
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {visibleRows.map((row, rowIndex) => (
              <TableRow key={`${row.join("|")}-${rowIndex}`}>
                {parsed.headers.map((header, columnIndex) => (
                  <TableCell key={`${header}-${rowIndex}-${columnIndex}`}>
                    {row[columnIndex] ?? ""}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

function ImagePreview({
  fileName,
  proxyUrl,
}: {
  fileName: string;
  proxyUrl: string;
}) {
  const [lightboxOpen, setLightboxOpen] = useState(false);

  useEffect(() => {
    if (!lightboxOpen) {
      return undefined;
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setLightboxOpen(false);
      }
    }

    window.addEventListener("keydown", handleKeyDown);

    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [lightboxOpen]);

  return (
    <>
      <button
        type="button"
        aria-label="Open image lightbox"
        className="group inline-flex overflow-hidden rounded-[1.5rem] border border-border/70 bg-background/75 p-3 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]"
        onClick={() => setLightboxOpen(true)}
      >
        <img
          alt={`${fileName} preview`}
          className="rounded-xl object-contain transition duration-200 group-hover:scale-[1.01]"
          src={proxyUrl}
          style={{ maxHeight: "240px", maxWidth: "320px" }}
        />
      </button>

      {lightboxOpen ? (
        <div
          aria-label="Image preview lightbox"
          aria-modal="true"
          className="fixed inset-0 z-50 flex items-center justify-center p-6"
          role="dialog"
        >
          <button
            type="button"
            aria-label="Close image preview backdrop"
            className="absolute inset-0 bg-[color:rgba(17,24,39,0.75)] backdrop-blur-sm"
            onClick={() => setLightboxOpen(false)}
          />
          <div className="relative z-10 max-h-full max-w-5xl rounded-[2rem] border border-white/15 bg-[color:rgba(15,23,42,0.9)] p-4 shadow-2xl">
            <button
              type="button"
              aria-label="Close image preview"
              className="absolute right-4 top-4 inline-flex size-10 items-center justify-center rounded-full border border-white/10 bg-white/5 text-white transition hover:bg-white/10"
              onClick={() => setLightboxOpen(false)}
            >
              <X className="size-4" aria-hidden="true" />
            </button>
            <img
              alt={`${fileName} full preview`}
              className="max-h-[80vh] max-w-full rounded-[1.5rem] object-contain"
              src={proxyUrl}
            />
          </div>
        </div>
      ) : null}
    </>
  );
}

export function selectRenderer(contentType: string): PreviewRenderer {
  const normalized = normalizeContentType(contentType);

  if (normalized === "image/svg+xml") {
    return "svg";
  }

  if (normalized.startsWith("image/")) {
    return "image";
  }

  if (normalized === "text/csv" || normalized.startsWith("text/tab-")) {
    return "csv";
  }

  if (normalized === "text/markdown") {
    return "markdown";
  }

  if (normalized === "text/html") {
    return "html";
  }

  if (normalized === "application/pdf") {
    return "pdf";
  }

  if (normalized === "application/octet-stream") {
    return "binary";
  }

  if (normalized === "application/json" || normalized.startsWith("text/")) {
    return "code";
  }

  return "binary";
}

export function FilePreview({
  file,
  content,
  error,
  isLoading = false,
  proxyUrl,
}: FilePreviewProps) {
  const renderer = content
    ? selectRenderer(content.contentType)
    : guessRendererFromPath(file.path);
  const downloadUrl = buildDownloadUrl(proxyUrl);
  const fileName = file.path.split("/").pop() ?? file.path;
  const previewable = isPreviewable(renderer, file.path);

  if (error?.status === 413) {
    return (
      <section className="space-y-5">
        <MetadataPanel file={file} />
        <div className="rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 p-6">
          <p className="text-sm font-semibold uppercase tracking-[0.22em] text-muted-foreground">
            Preview unavailable
          </p>
          <h3 className="mt-3 text-xl font-semibold tracking-tight text-foreground">
            File too large for preview
          </h3>
          <p className="mt-3 text-sm leading-7 text-muted-foreground">
            This file exceeds the preview limit. Reported size:{" "}
            {formatBytes(error.fileSize)}.
          </p>
          <div className="mt-5">
            <DownloadButton href={downloadUrl} />
          </div>
        </div>
      </section>
    );
  }

  return (
    <section className="space-y-5">
      <MetadataPanel file={file} />

      <div className="overflow-hidden rounded-[1.75rem] border border-border/70 bg-[linear-gradient(160deg,color-mix(in_oklab,var(--background)_92%,white_8%),color-mix(in_oklab,var(--accent)_10%,var(--background)_90%))] p-5 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]">
        <div className="flex flex-col gap-4 border-b border-border/60 pb-5 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
              Preview
            </p>
            <h3 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
              {fileName}
            </h3>
          </div>

          {previewable ? <DownloadButton href={downloadUrl} /> : null}
        </div>

        <div className="mt-5">
          {isLoading ? (
            <div className="rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 px-5 py-8 text-sm text-muted-foreground">
              Loading preview...
            </div>
          ) : null}

          {!isLoading && !previewable ? (
            <div className="rounded-[1.5rem] border border-dashed border-border/70 bg-background/55 px-5 py-8 text-sm text-muted-foreground">
              <div className="flex items-center gap-3 text-foreground">
                <FileCode2
                  className="size-5 text-muted-foreground"
                  aria-hidden="true"
                />
                <span>Binary preview is unavailable for this file type.</span>
              </div>
            </div>
          ) : null}

          {!isLoading && previewable && renderer === "image" ? (
            <ImagePreview fileName={fileName} proxyUrl={proxyUrl} />
          ) : null}

          {!isLoading && previewable && renderer === "svg" ? (
            <div className="inline-flex overflow-hidden rounded-[1.5rem] border border-border/70 bg-background/75 p-3 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]">
              <img
                alt={`${fileName} preview`}
                className="rounded-xl object-contain"
                src={proxyUrl}
                style={{ maxHeight: "480px", maxWidth: "100%" }}
              />
            </div>
          ) : null}

          {!isLoading && previewable && renderer === "pdf" ? (
            <iframe
              className="min-h-[32rem] w-full rounded-[1.5rem] border border-border/70 bg-background"
              src={proxyUrl}
              title="PDF preview"
            />
          ) : null}

          {!isLoading && previewable && renderer === "html" ? (
            <iframe
              className="min-h-[32rem] w-full rounded-[1.5rem] border border-border/70 bg-white"
              sandbox="allow-same-origin"
              src={proxyUrl}
              title="HTML preview"
            />
          ) : null}

          {!isLoading && previewable && renderer === "markdown" ? (
            <article className="max-w-none rounded-[1.5rem] border border-border/70 bg-background/75 p-6">
              <ReactMarkdown>{content?.content ?? ""}</ReactMarkdown>
            </article>
          ) : null}

          {!isLoading && previewable && renderer === "csv" && content ? (
            <CsvPreview
              content={content.content}
              contentType={content.contentType}
            />
          ) : null}

          {!isLoading && previewable && renderer === "code" ? (
            <div className="overflow-hidden rounded-[1.5rem] border border-border/70 bg-[color:rgba(15,23,42,0.96)]">
              <div className="flex items-center gap-2 border-b border-white/10 px-4 py-3 text-xs uppercase tracking-[0.24em] text-slate-300">
                <Expand className="size-4" aria-hidden="true" />
                Syntax-highlighted preview
              </div>
              <pre className="overflow-x-auto p-5 text-sm leading-7 text-slate-100">
                <code
                  dangerouslySetInnerHTML={{
                    __html: highlightCode(
                      content?.content ?? "",
                      content?.contentType ?? "text/plain",
                    ),
                  }}
                />
              </pre>
            </div>
          ) : null}
        </div>
      </div>
    </section>
  );
}
