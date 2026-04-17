"use client";

import {
    Bar,
    BarChart,
    CartesianGrid,
    Cell,
    ResponsiveContainer,
    Tooltip,
    XAxis,
    YAxis,
} from "recharts";

import type { DailyCount } from "@/lib/contracts";

type DailyChartProps = {
    data: DailyCount[];
};

function formatLabel(date: string): string {
    const value = new Date(`${date}T00:00:00Z`);

    if (Number.isNaN(value.getTime())) {
        return date;
    }

    return value.toLocaleDateString("en-GB", {
        month: "short",
        day: "numeric",
    });
}

export function DailyChart({ data }: DailyChartProps) {
    const chartData = data.map((entry, index) => ({
        ...entry,
        label: formatLabel(entry.date),
        fill:
            index === data.length - 1
                ? "var(--color-accent)"
                : "color-mix(in oklab, var(--color-primary) 72%, white 28%)",
    }));

    return (
        <section className="overflow-hidden rounded-[1.85rem] border border-border/70 bg-card/85 p-6 shadow-[0_26px_90px_-76px_rgba(46,65,90,0.9)]">
            <div className="flex flex-wrap items-end justify-between gap-4">
                <div>
                    <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
                        Daily registrations
                    </p>
                    <h2 className="mt-2 text-2xl font-semibold tracking-tight">
                        Last 30 days of result activity
                    </h2>
                </div>
                <p className="max-w-md text-sm leading-6 text-muted-foreground">
                    Recent throughput stays readable on wide and narrow screens, with the
                    newest day lifted in accent colour.
                </p>
            </div>

            <div className="mt-6 h-[320px] w-full">
                <ResponsiveContainer width="100%" height="100%">
                    <BarChart
                        data={chartData}
                        barCategoryGap={6}
                        margin={{ top: 12, right: 8, left: -20, bottom: 0 }}
                    >
                        <CartesianGrid
                            vertical={false}
                            stroke="color-mix(in oklab, var(--color-border) 82%, transparent)"
                        />
                        <XAxis
                            dataKey="label"
                            interval={4}
                            tick={{ fill: "var(--color-muted-foreground)", fontSize: 12 }}
                            tickLine={false}
                            axisLine={false}
                        />
                        <YAxis
                            allowDecimals={false}
                            tick={{ fill: "var(--color-muted-foreground)", fontSize: 12 }}
                            tickLine={false}
                            axisLine={false}
                        />
                        <Tooltip
                            cursor={{
                                fill: "color-mix(in oklab, var(--color-accent) 10%, transparent)",
                            }}
                            contentStyle={{
                                borderRadius: "1rem",
                                border: "1px solid var(--color-border)",
                                background: "var(--color-card)",
                                color: "var(--color-foreground)",
                            }}
                        />
                        <Bar dataKey="count" radius={[10, 10, 4, 4]}>
                            {chartData.map((entry) => (
                                <Cell key={entry.date} fill={entry.fill} />
                            ))}
                        </Bar>
                    </BarChart>
                </ResponsiveContainer>
            </div>

            <div className="sr-only" aria-label="Daily registration bars">
                {chartData.map((entry) => (
                    <span key={entry.date} data-daily-bar="true">
                        {entry.date}:{entry.count}
                    </span>
                ))}
            </div>
        </section>
    );
}
