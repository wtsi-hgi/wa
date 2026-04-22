"use client";

import { useEffect, useState } from "react";

import { DailyChartShell } from "@/components/daily-chart-shell";
import type { DailyCount } from "@/lib/contracts";

type DailyChartPanelProps = {
  data: DailyCount[];
};

type LoadedDailyChart = typeof import("@/components/daily-chart").DailyChart;

export function DailyChartPanel({ data }: DailyChartPanelProps) {
  const [ChartComponent, setChartComponent] =
    useState<LoadedDailyChart | null>(null);

  useEffect(() => {
    let active = true;

    void import("@/components/daily-chart").then((module) => {
      if (active) {
        setChartComponent(() => module.DailyChart);
      }
    });

    return () => {
      active = false;
    };
  }, []);

  if (ChartComponent === null) {
    return <DailyChartShell data={data} />;
  }

  return <ChartComponent data={data} />;
}
