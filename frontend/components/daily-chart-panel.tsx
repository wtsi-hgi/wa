"use client";

import dynamic from "next/dynamic";

import { DailyChartShell } from "@/components/daily-chart-shell";
import type { DailyCount } from "@/lib/contracts";

type DailyChartPanelProps = {
  data: DailyCount[];
};

const ClientDailyChart = dynamic(
  () => import("@/components/daily-chart").then((module) => module.DailyChart),
  {
    ssr: false,
    loading: () => <DailyChartShell data={[]} />,
  },
);

export function DailyChartPanel({ data }: DailyChartPanelProps) {
  return <ClientDailyChart data={data} />;
}
