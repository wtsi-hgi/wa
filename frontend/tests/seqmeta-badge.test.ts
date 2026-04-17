/**
 * @vitest-environment jsdom
 */

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { FileEntry, IdentifierResult, ResultSet } from "@/lib/contracts";
import {
  buildSeqmetaCacheCookie,
  deserializeSeqmetaCacheCookie,
  SEQMETA_CACHE_COOKIE_NAME,
} from "@/lib/seqmeta-cache-core";
import {
  SeqmetaCache,
  SeqmetaCacheContext,
  SeqmetaCacheProvider,
} from "@/lib/seqmeta-cache";

const fetchResultMock = vi.fn();
const fetchFilesMock = vi.fn();
const validateIdentifierMock = vi.fn();
const cookiesMock = vi.fn();
const originalDocumentCookie = Object.getOwnPropertyDescriptor(
  document,
  "cookie",
);
let cookieJar = "";

vi.mock("next/headers", () => ({
  cookies: cookiesMock,
}));

vi.mock("@/app/(results)/actions", () => ({
  fetchResult: fetchResultMock,
  fetchFiles: fetchFilesMock,
  validateIdentifier: validateIdentifierMock,
}));

function setRequestCookieHeader(cookieHeader?: string) {
  cookiesMock.mockResolvedValue({
    get(name: string) {
      const prefix = `${name}=`;
      const cookie = cookieHeader
        ?.split(";")
        .map((entry) => entry.trim())
        .find((entry) => entry.startsWith(prefix));

      if (!cookie) {
        return undefined;
      }

      return { value: cookie.slice(prefix.length) };
    },
  });
}

function readSeqmetaCookieFromDocument(): string | undefined {
  const prefix = `${SEQMETA_CACHE_COOKIE_NAME}=`;

  return document.cookie
    .split(";")
    .map((entry) => entry.trim())
    .find((entry) => entry.startsWith(prefix));
}

beforeEach(() => {
  cookieJar = "";
  Object.defineProperty(document, "cookie", {
    configurable: true,
    get() {
      return cookieJar;
    },
    set(value: string) {
      const [cookiePair = "", ...attributes] = value
        .split(";")
        .map((entry) => entry.trim());

      if (attributes.some((entry) => entry === "Max-Age=0")) {
        cookieJar = "";
        return;
      }

      cookieJar = cookiePair;
    },
  });
});

function buildResultSet(overrides: Partial<ResultSet> = {}): ResultSet {
  return {
    id: "result-42",
    pipeline_identifier: "gh://repo/pipeline.nf",
    run_key: "runid=42",
    requester: "alice",
    operator: "bob",
    command: "nextflow run pipeline.nf",
    pipeline_name: "nf-core/rnaseq",
    pipeline_version: "3.18.0",
    output_directory: "/tmp/results/42",
    metadata: {
      seqmeta_sampleid: "SANG001",
    },
    created_at: "2026-04-16T10:00:00Z",
    updated_at: "2026-04-16T10:30:00Z",
    ...overrides,
  };
}

function buildFileEntry(overrides: Partial<FileEntry> = {}): FileEntry {
  return {
    path: "/tmp/results/42/report.html",
    kind: "output",
    mtime: "2026-04-16T10:15:00Z",
    size: 120,
    ...overrides,
  };
}

function buildEnrichment(
  overrides: Partial<IdentifierResult> = {},
): IdentifierResult {
  return {
    identifier: "SANG001",
    type: "sanger_sample_id",
    object: {
      sanger_id: "SANG001",
      study_name: "RNA Seq",
    },
    ...overrides,
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;

  const promise = new Promise<T>((innerResolve, innerReject) => {
    resolve = innerResolve;
    reject = innerReject;
  });

  return { promise, resolve, reject };
}

describe("M1 result detail seqmeta enrichment", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    document.cookie = `${SEQMETA_CACHE_COOKIE_NAME}=; Max-Age=0; Path=/`;
    setRequestCookieHeader();

    if (originalDocumentCookie) {
      Object.defineProperty(document, "cookie", originalDocumentCookie);
    }
  });

  it("shows the resolved seqmeta type and tooltip details for an enriched badge", async () => {
    const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

    render(
      createElement(SeqmetaBadge, {
        rawValue: "SANG001",
        enrichment: buildEnrichment(),
      }),
    );

    expect(screen.getByText("sanger_sample_id: SANG001")).toBeTruthy();
    expect(screen.getByText("RNA Seq")).toBeTruthy();
  });

  it("shows the raw value with a failure indicator and unavailable tooltip when enrichment fails", async () => {
    const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

    render(
      createElement(SeqmetaBadge, {
        rawValue: "SANG001",
        enrichment: null,
        error: true,
      }),
    );

    expect(screen.getByText("SANG001")).toBeTruthy();
    expect(
      screen.getByLabelText("enrichment unavailable").textContent,
    ).toContain("?");
    expect(screen.getByText("enrichment unavailable")).toBeTruthy();
  });

  it("renders non-seqmeta metadata without attempting enrichment", async () => {
    const { ResultMetadata } = await import("@/components/result-metadata");

    render(
      createElement(ResultMetadata, {
        metadata: {
          library: "exon",
        },
      }),
    );

    expect(screen.getByText("library")).toBeTruthy();
    expect(screen.getByText("exon")).toBeTruthy();
    expect(validateIdentifierMock).not.toHaveBeenCalled();
  });

  it("keeps seqmeta details visible when rendered inside the metadata table", async () => {
    const { ResultMetadata } = await import("@/components/result-metadata");

    render(
      createElement(ResultMetadata, {
        metadata: {
          seqmeta_sampleid: "SANG001",
        },
        enrichments: {
          SANG001: buildEnrichment(),
        },
      }),
    );

    const metadataTable = screen.getByRole("table");
    const metadataWrapper = metadataTable.parentElement;

    expect(metadataWrapper).toBeTruthy();
    expect(metadataWrapper?.className).toContain("overflow-visible");
    expect(metadataWrapper?.className).not.toContain("overflow-hidden");
    expect(screen.getByRole("tooltip")).toBeTruthy();
    expect(screen.getByText("RNA Seq")).toBeTruthy();
  });

  it("starts all seqmeta enrichments in parallel during server detail rendering", async () => {
    const pending = Array.from({ length: 5 }, () =>
      deferred<IdentifierResult | null>(),
    );
    const queue = [...pending];

    fetchResultMock.mockResolvedValue(
      buildResultSet({
        metadata: {
          seqmeta_sampleid: "SANG001",
          seqmeta_sample_lims: "LIMS001",
          seqmeta_runid: "1234",
          seqmeta_studyid: "6568",
          seqmeta_library: "RNA",
        },
      }),
    );
    fetchFilesMock.mockResolvedValue([]);
    setRequestCookieHeader();

    validateIdentifierMock.mockImplementation((value: string) => {
      const next = queue.shift();

      if (!next) {
        throw new Error(`unexpected identifier ${value}`);
      }

      return next.promise;
    });

    const pageModule = await import("@/app/(results)/results/[id]/page");
    const renderPromise = pageModule.default({
      params: Promise.resolve({ id: "result-42" }),
    });

    await waitFor(() => {
      expect(validateIdentifierMock).toHaveBeenCalledTimes(5);
    });

    for (const [index, item] of pending.entries()) {
      item.resolve(buildEnrichment({ identifier: `ID-${index}` }));
    }

    renderToStaticMarkup(await renderPromise);
  });

  it("shows an empty metadata state when a result set has no metadata", async () => {
    const { ResultMetadata } = await import("@/components/result-metadata");

    render(createElement(ResultMetadata, { metadata: {} }));

    expect(screen.getByText("No metadata")).toBeTruthy();
  });

  it("renders the detail page shell with server-started seqmeta enrichment", async () => {
    fetchResultMock.mockResolvedValue(
      buildResultSet({
        metadata: {
          seqmeta_sampleid: "SANG001",
          library: "rna",
        },
      }),
    );
    fetchFilesMock.mockResolvedValue([
      buildFileEntry(),
      buildFileEntry({ path: "/tmp/results/42/input.cram", kind: "input" }),
    ]);
    validateIdentifierMock.mockResolvedValue(buildEnrichment());
    setRequestCookieHeader();

    const pageModule = await import("@/app/(results)/results/[id]/page");
    const Page = pageModule.default;
    const markup = renderToStaticMarkup(
      await Page({
        params: Promise.resolve({ id: "result-42" }),
      }),
    );

    expect(fetchResultMock).toHaveBeenCalledWith("result-42");
    expect(fetchFilesMock).toHaveBeenCalledWith("result-42");
    expect(validateIdentifierMock).toHaveBeenCalledWith("SANG001");
    expect(markup).toContain("File browser");
    expect(markup).toContain("Selected file");
    expect(markup).toContain("/tmp/results/42/report.html");
    expect(markup).toContain("Result metadata");
    expect(markup).toContain("sanger_sample_id: SANG001");
    expect(markup).toContain("Registered files");
    expect(markup).toContain("1 input");
    expect(markup).toContain("1 output");
  });

  it("primes the client cache with server enrichments so remounts reuse them", async () => {
    const enrichment = buildEnrichment();

    const { ResultMetadataEnrichment } =
      await import("@/components/result-metadata-enrichment");
    const cache = new SeqmetaCache();
    const metadata = {
      seqmeta_sampleid: "SANG001",
    };

    const firstRender = render(
      createElement(ResultMetadataEnrichment, {
        initialEnrichments: {
          SANG001: enrichment,
        },
        metadata,
      }),
      {
        wrapper: ({ children }) =>
          createElement(
            SeqmetaCacheContext.Provider,
            { value: cache },
            children,
          ),
      },
    );

    expect(screen.getByText("sanger_sample_id: SANG001")).toBeTruthy();
    expect(validateIdentifierMock).not.toHaveBeenCalled();

    firstRender.unmount();

    render(
      createElement(ResultMetadataEnrichment, {
        metadata,
      }),
      {
        wrapper: ({ children }) =>
          createElement(
            SeqmetaCacheContext.Provider,
            { value: cache },
            children,
          ),
      },
    );

    expect(screen.getByText("sanger_sample_id: SANG001")).toBeTruthy();
    expect(validateIdentifierMock).not.toHaveBeenCalled();
  });

  it("mirrors the client cache to a cookie and reuses it on the next detail render", async () => {
    fetchFilesMock.mockResolvedValue([]);
    fetchResultMock
      .mockResolvedValueOnce(
        buildResultSet({
          id: "result-42",
          metadata: { seqmeta_sampleid: "SANG001" },
        }),
      )
      .mockResolvedValueOnce(
        buildResultSet({
          id: "result-99",
          metadata: { seqmeta_sampleid: "SANG001" },
        }),
      );
    validateIdentifierMock.mockResolvedValue(buildEnrichment());
    setRequestCookieHeader();

    const { ResultMetadataEnrichment } =
      await import("@/components/result-metadata-enrichment");
    const pageModule = await import("@/app/(results)/results/[id]/page");

    render(
      createElement(ResultMetadataEnrichment, {
        initialEnrichments: {
          SANG001: buildEnrichment(),
        },
        metadata: {
          seqmeta_sampleid: "SANG001",
        },
      }),
      {
        wrapper: ({ children }) =>
          createElement(SeqmetaCacheProvider, null, children),
      },
    );

    await waitFor(() => {
      expect(readSeqmetaCookieFromDocument()).toContain(
        SEQMETA_CACHE_COOKIE_NAME,
      );
    });

    const persistedCookie = readSeqmetaCookieFromDocument();
    expect(persistedCookie).toBeTruthy();

    const cookieValue = persistedCookie?.split("=").slice(1).join("=");
    expect(deserializeSeqmetaCacheCookie(cookieValue).SANG001?.identifier).toBe(
      "SANG001",
    );

    const firstMarkup = renderToStaticMarkup(
      await pageModule.default({
        params: Promise.resolve({ id: "result-42" }),
      }),
    );

    expect(validateIdentifierMock).toHaveBeenCalledTimes(1);
    expect(firstMarkup).toContain("sanger_sample_id: SANG001");

    setRequestCookieHeader(
      buildSeqmetaCacheCookie({ SANG001: buildEnrichment() }),
    );

    const secondMarkup = renderToStaticMarkup(
      await pageModule.default({
        params: Promise.resolve({ id: "result-99" }),
      }),
    );

    expect(validateIdentifierMock).toHaveBeenCalledTimes(1);
    expect(secondMarkup).toContain("sanger_sample_id: SANG001");
  });

  it("marks seqmeta enrichment as unavailable when server validation fails", async () => {
    fetchResultMock.mockResolvedValue(
      buildResultSet({
        metadata: {
          seqmeta_sampleid: "SANG001",
        },
      }),
    );
    fetchFilesMock.mockResolvedValue([]);
    validateIdentifierMock.mockRejectedValue(new Error("seqmeta unavailable"));
    setRequestCookieHeader();

    const pageModule = await import("@/app/(results)/results/[id]/page");
    const markup = renderToStaticMarkup(
      await pageModule.default({
        params: Promise.resolve({ id: "result-42" }),
      }),
    );

    expect(markup).toContain("enrichment unavailable");
    expect(markup).toContain("SANG001");
  });
});
