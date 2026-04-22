// @vitest-environment jsdom

import { createElement } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import {
  afterAll,
  afterEach,
  beforeAll,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import { DailyChart } from "@/components/daily-chart";
import { DailyChartShell } from "@/components/daily-chart-shell";
import type { DailyCount } from "@/lib/contracts";

function buildDailyCounts(totalDays: number, todayCount: number): DailyCount[] {
  return Array.from({ length: totalDays }, (_, index) => ({
    date: `2026-03-${String(index + 1).padStart(2, "0")}`,
    count: index === totalDays - 1 ? todayCount : index % 4,
  }));
}

describe("DailyChart hydration", () => {
  beforeAll(() => {
    class ResizeObserverStub {
      observe() {}

      unobserve() {}

      disconnect() {}
    }

    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  afterAll(() => {
    vi.unstubAllGlobals();
  });

  it("renders a stable non-Recharts shell during server render", () => {
    const data = buildDailyCounts(30, 5);
    const markup = renderToString(createElement(DailyChartShell, { data }));

    expect(markup).toContain("Last 30 days of result activity");
    expect(markup).toContain('data-chart-shell-bar="latest"');
    expect(markup).not.toContain("recharts-responsive-container");
    expect(markup.match(/data-daily-bar="true"/g) ?? []).toHaveLength(30);
  });

  it("renders zero-count shell days without a visible fallback bar height", () => {
    const markup = renderToString(
      createElement(DailyChartShell, {
        data: [
          { date: "2026-03-01", count: 0 },
          { date: "2026-03-02", count: 3 },
        ],
      }),
    );

    expect(markup).toContain('style="height:0px"');
    expect(markup).toContain('style="height:62px"');
    expect(markup).not.toContain('style="height:34px"');
  });

  it("renders an explicit empty state instead of placeholder bars", () => {
    const markup = renderToString(createElement(DailyChartShell, { data: [] }));

    expect(markup).toContain("No result activity recorded for the last 30 days.");
    expect(markup).not.toContain('data-chart-shell-bar="');
    expect(markup).not.toContain('data-daily-bar="true"');
  });

  it("mounts the client chart after hydration completes", async () => {
    const data = buildDailyCounts(30, 5);
    const container = document.createElement("div");
    const root = createRoot(container);

    document.body.appendChild(container);

    await act(async () => {
      root.render(createElement(DailyChart, { data }));
    });

    expect(
      container.querySelector(".recharts-responsive-container"),
    ).not.toBeNull();
    expect(container.querySelectorAll('[data-daily-bar="true"]')).toHaveLength(
      30,
    );

    await act(async () => {
      root.unmount();
    });
  });
});
