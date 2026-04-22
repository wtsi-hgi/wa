// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { fireEvent, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { FileEntry } from "@/lib/contracts";

function buildFile(path: string): FileEntry {
  return {
    kind: "output",
    mtime: "2026-04-16T10:15:00Z",
    path,
    size: 512,
  };
}

describe("O1 result detail hydration", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("keeps file-browser folder toggles interactive when client locale formatting differs", async () => {
    const { ResultDetailFiles } = await import("@/components/result-detail-files");
    const files = [buildFile("/results/sample.bam")];
    const toLocaleStringSpy = vi.spyOn(Date.prototype, "toLocaleString");

    toLocaleStringSpy.mockImplementation(() => "16 Apr 2026, 10:15");

    const serverMarkup = renderToString(
      createElement(ResultDetailFiles, {
        files,
        resultId: "result-1",
      }),
    );
    const container = document.createElement("div");
    const recoverableErrors: Error[] = [];

    document.body.appendChild(container);
    container.innerHTML = serverMarkup;

    toLocaleStringSpy.mockImplementation(() => "17 Apr 2026, 10:15");

    let root: ReturnType<typeof hydrateRoot> | null = null;

    await act(async () => {
      root = hydrateRoot(
        container,
        createElement(ResultDetailFiles, {
          files,
          resultId: "result-1",
        }),
        {
          onRecoverableError: (error) => {
            recoverableErrors.push(error);
          },
        },
      );
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
});