// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { fireEvent, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AppProviders } from "@/components/app-providers";
import type { FileEntry, ResultSet } from "@/lib/contracts";

const fetchFilesMock = vi.fn();
const fetchResultMock = vi.fn();
const validateIdentifierMock = vi.fn();
const enrichSeqmetaMetadataMock = vi.fn();
const getRequestSeqmetaCacheMock = vi.fn();
const buildCachedEnrichmentStateMock = vi.fn();
const primeSeqmetaCacheMock = vi.fn();

vi.mock("@/app/(results)/actions", () => ({
  fetchFiles: fetchFilesMock,
  fetchResult: fetchResultMock,
  validateIdentifier: validateIdentifierMock,
}));

vi.mock("@/lib/seqmeta-enrichment", () => ({
  buildCachedEnrichmentState: buildCachedEnrichmentStateMock,
  enrichSeqmetaMetadata: enrichSeqmetaMetadataMock,
  primeSeqmetaCache: primeSeqmetaCacheMock,
}));

vi.mock("@/lib/seqmeta-cache-server", () => ({
  getRequestSeqmetaCache: getRequestSeqmetaCacheMock,
}));

function buildFile(path: string): FileEntry {
  return {
    kind: "output",
    mtime: "2026-04-16T10:15:00Z",
    path,
    size: 512,
  };
}

function buildResultSet(): ResultSet {
  return {
    command: "nextflow run workflow.nf",
    created_at: "2026-04-16T10:15:00Z",
    id: "result-1",
    metadata: {
      seqmeta_sampleid: "SANG001",
    },
    operator: "operator-1",
    output_directory: "/results",
    pipeline_identifier: "gh://repo/workflow.nf",
    pipeline_name: "nf-core/rnaseq",
    pipeline_version: "3.18.0",
    requester: "alice",
    run_key: "runid=1001",
    updated_at: "2026-04-16T10:45:00Z",
  };
}

describe("O1 result detail hydration", () => {
  const matchMediaStub = () => ({
    addEventListener: vi.fn(),
    addListener: vi.fn(),
    dispatchEvent: vi.fn(),
    matches: false,
    media: "",
    onchange: null,
    removeEventListener: vi.fn(),
    removeListener: vi.fn(),
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("keeps file-browser folder toggles interactive when client locale formatting differs", async () => {
    const { ResultDetailFiles } = await import("@/components/result-detail-files");
    const files = [buildFile("/results/sample.bam")];
    const toLocaleStringSpy = vi.spyOn(Date.prototype, "toLocaleString");

    vi.stubGlobal("matchMedia", matchMediaStub);

    toLocaleStringSpy.mockImplementation(() => "16 Apr 2026, 10:15");

    const serverTree = createElement(
      AppProviders,
      undefined,
      createElement(ResultDetailFiles, {
        files,
        resultId: "result-1",
      }),
    );
    const serverMarkup = renderToString(serverTree);
    const container = document.createElement("div");
    const recoverableErrors: Error[] = [];

    document.body.appendChild(container);
    container.innerHTML = serverMarkup;

    toLocaleStringSpy.mockImplementation(() => "17 Apr 2026, 10:15");

    let root: ReturnType<typeof hydrateRoot> | null = null;

    await act(async () => {
      root = hydrateRoot(container, serverTree, {
        onRecoverableError: (error) => {
          recoverableErrors.push(error);
        },
      });
    });

    expect(
      container.querySelector('button[data-file-path="/results/sample.bam"]'),
    ).not.toBeNull();

    fireEvent.click(
      container.querySelector('button[data-folder-path="/results"]')!,
    );

    await waitFor(() => {
      expect(
        container.querySelector('button[data-file-path="/results/sample.bam"]'),
      ).toBeNull();
    });

    expect(recoverableErrors).toHaveLength(0);

    await act(async () => {
      root?.unmount();
    });
  });

  it("hydrates the result detail page without mismatches and keeps folder toggles interactive when locale formatting differs", async () => {
    const files = [buildFile("/results/sample.bam")];
    const result = buildResultSet();
    const toLocaleStringSpy = vi.spyOn(Date.prototype, "toLocaleString");

    vi.stubGlobal("matchMedia", matchMediaStub);
    fetchFilesMock.mockResolvedValue(files);
    fetchResultMock.mockResolvedValue(result);
    validateIdentifierMock.mockResolvedValue(true);
    enrichSeqmetaMetadataMock.mockResolvedValue({
      enrichments: {},
      errors: {},
    });
    buildCachedEnrichmentStateMock.mockReturnValue({
      enrichments: {},
      errors: {},
    });
    getRequestSeqmetaCacheMock.mockResolvedValue({});

    toLocaleStringSpy.mockImplementation(() => "16 Apr 2026, 10:15");

    const pageModule = await import("@/app/(results)/results/[id]/page");
    const Page = pageModule.default;
    const serverTree = createElement(
      AppProviders,
      undefined,
      await Page({ params: Promise.resolve({ id: "result-1" }) }),
    );
    const serverMarkup = renderToString(serverTree);
    const container = document.createElement("div");
    const recoverableErrors: Error[] = [];

    document.body.appendChild(container);
    container.innerHTML = serverMarkup;

    toLocaleStringSpy.mockImplementation(() => "17 Apr 2026, 10:15");

    let root: ReturnType<typeof hydrateRoot> | null = null;

    await act(async () => {
      root = hydrateRoot(container, serverTree, {
        onRecoverableError: (error) => {
          recoverableErrors.push(error);
        },
      });
    });

    fireEvent.click(
      container.querySelector('button[data-folder-path="/results"]')!,
    );

    await waitFor(() => {
      expect(
        container.querySelector('button[data-file-path="/results/sample.bam"]'),
      ).toBeNull();
    });

    expect(recoverableErrors).toHaveLength(0);

    await act(async () => {
      root?.unmount();
    });
  });
});