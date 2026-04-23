// @vitest-environment jsdom

import { act, createElement } from "react";
import { hydrateRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useTheme } from "next-themes";
import { Toaster as Sonner } from "sonner";

let resolvedThemeForRender = "light";

vi.mock("next-themes", () => ({
  useTheme: () => ({
    resolvedTheme: resolvedThemeForRender,
  }),
}));

vi.mock("sonner", () => ({
  Toaster: ({ theme }: { theme?: string }) =>
    createElement("div", { "data-testid": "sonner-theme" }, `theme:${theme ?? "unknown"}`),
}));

function UngatedToaster() {
  const { resolvedTheme } = useTheme();

  return createElement(Sonner, {
    theme: resolvedTheme === "dark" ? "dark" : "light",
  });
}

describe("toaster hydration", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    resolvedThemeForRender = "light";
    vi.resetModules();
  });

  it("avoids the theme hydration mismatch that an ungated toaster triggers", async () => {
    const { Toaster } = await import("@/components/ui/toaster");
    const container = document.createElement("div");
    const ungatedContainer = document.createElement("div");
    const gatedRecoverableErrors: Error[] = [];
    const ungatedRecoverableErrors: Error[] = [];

    document.body.appendChild(container);
    document.body.appendChild(ungatedContainer);

    resolvedThemeForRender = "light";
    const gatedServerTree = createElement(Toaster);
    const ungatedServerTree = createElement(UngatedToaster);

    container.innerHTML = renderToString(gatedServerTree);
    ungatedContainer.innerHTML = renderToString(ungatedServerTree);

    expect(container.innerHTML).toBe("");
    expect(ungatedContainer.textContent).toContain("theme:light");

    resolvedThemeForRender = "dark";

    let gatedRoot: ReturnType<typeof hydrateRoot> | null = null;
    let ungatedRoot: ReturnType<typeof hydrateRoot> | null = null;

    await act(async () => {
      gatedRoot = hydrateRoot(container, gatedServerTree, {
        onRecoverableError: (error) => {
          gatedRecoverableErrors.push(error);
        },
      });
      ungatedRoot = hydrateRoot(ungatedContainer, ungatedServerTree, {
        onRecoverableError: (error) => {
          ungatedRecoverableErrors.push(error);
        },
      });
    });

    await waitFor(() => {
      expect(container.textContent).toContain("theme:dark");
    });

    expect(gatedRecoverableErrors).toHaveLength(0);
    expect(ungatedRecoverableErrors.length).toBeGreaterThan(0);

    await act(async () => {
      gatedRoot?.unmount();
      ungatedRoot?.unmount();
    });
  });
});