"use client";

import { useState } from "react";

import type { UsageCategory } from "@/lib/contracts";
import type { BucketUnit, TimeBucket } from "@/lib/usage-derive";
import { cn } from "@/lib/utils";

import {
    categoryColor,
    categoryLabels,
    formatBucketStart,
    formatBucketTime,
    formatCategories,
    formatCount,
    pluralise,
} from "./format";

const width = 760;
const height = 240;
const margin = { top: 36, right: 8, bottom: 26, left: 44 };
const plotWidth = width - margin.left - margin.right;
const plotHeight = height - margin.top - margin.bottom;
const maxTicks = 7;

const unitNouns: Record<BucketUnit, string> = {
    hour: "hour",
    day: "day",
    week: "week",
};

// niceMax rounds max up to 1, 2, or 5 times a power of ten, so the axis has
// round labels.
export function niceMax(max: number): number {
    if (max <= 0) {
        return 1;
    }

    const power = 10 ** Math.floor(Math.log10(max));

    for (const step of [1, 2, 5, 10]) {
        if (step * power >= max) {
            return step * power;
        }
    }

    return 10 * power;
}

// tickIndexes picks at most maxTicks evenly spaced bucket indexes to label,
// always including the first and last.
export function tickIndexes(n: number): number[] {
    if (n <= maxTicks) {
        return [...Array(n).keys()];
    }

    const step = Math.ceil((n - 1) / (maxTicks - 1));
    const ticks: number[] = [];

    for (let i = 0; i < n - 1; i += step) {
        ticks.push(i);
    }

    if (n - 1 - ticks.at(-1)! < step / 2) {
        ticks.pop();
    }

    return [...ticks, n - 1];
}

type Props = {
    buckets: TimeBucket[];
    categories: UsageCategory[];
    unit: BucketUnit;
};

function bucketTitle(bucket: TimeBucket, unit: BucketUnit): string {
    const parts = [
        `${unitNouns[unit]} from ${formatBucketTime(bucket.start, unit)}${unit === "hour" ? " UTC" : ""}`,
        `${pluralise(bucket.requests, "request")}`,
        formatCategories(bucket.byCategory),
    ];

    if (bucket.severity) {
        parts.push(
            `${pluralise(bucket.severity.fault, "fault")}, ${pluralise(bucket.severity.caller, "caller error")}, ${formatCount(bucket.severity.expected + bucket.severity.probe)} expected`,
        );
    }

    return parts.filter(Boolean).join("; ");
}

// ActivityChart draws requests per bucket as columns stacked by category,
// with fault (cross) and caller (circle) markers above each column and,
// when toggled on, expected misses (dot). Legend buttons hide or show a
// category. A table of the same numbers sits behind a disclosure.
export function ActivityChart({ buckets, categories, unit }: Props) {
    const [hidden, setHidden] = useState<UsageCategory[]>([]);
    const [showExpected, setShowExpected] = useState(false);
    const shown = categories.filter((category) => !hidden.includes(category));
    const totals = buckets.map((bucket) =>
        shown.reduce(
            (sum, category) => sum + (bucket.byCategory[category] ?? 0),
            0,
        ),
    );
    const top = niceMax(Math.max(0, ...totals));
    const band = plotWidth / Math.max(buckets.length, 1);
    const barWidth = Math.max(1, Math.min(band * 0.7, 32));
    const y = (value: number) =>
        margin.top + plotHeight - (value / top) * plotHeight;
    const total = buckets.reduce((sum, bucket) => sum + bucket.requests, 0);
    const peak = buckets.reduce<TimeBucket | undefined>(
        (best, bucket) =>
            !best || bucket.requests > best.requests ? bucket : best,
        undefined,
    );
    const faults = buckets.reduce(
        (sum, bucket) => sum + (bucket.severity?.fault ?? 0),
        0,
    );
    const callers = buckets.reduce(
        (sum, bucket) => sum + (bucket.severity?.caller ?? 0),
        0,
    );
    const hasSeverity = buckets.some((bucket) => bucket.severity);
    const first = buckets[0];
    const last = buckets.at(-1);
    const span =
        first && last && first !== last
            ? `per ${unitNouns[unit]} from ${formatBucketTime(first.start, unit)} to ${formatBucketTime(last.start, unit)}`
            : first
              ? `in the ${unitNouns[unit]} from ${formatBucketTime(first.start, unit)}`
              : "";
    const summary = peak
        ? `Requests ${span}${unit === "hour" ? " UTC" : ""}: ${pluralise(total, "request")}, peak ${formatCount(peak.requests)} at ${formatBucketTime(peak.start, unit)}.${hasSeverity ? ` ${pluralise(faults, "fault")}, ${pluralise(callers, "caller error")}.` : ""}`
        : "No requests.";
    const toggle = (category: UsageCategory) =>
        setHidden((current) =>
            current.includes(category)
                ? current.filter((c) => c !== category)
                : [...current, category],
        );

    return (
        <div className="flex flex-col gap-2">
            <div
                aria-label="Chart legend"
                className="flex flex-wrap gap-1.5"
                role="group"
            >
                {categories.map((category) => {
                    const on = !hidden.includes(category);

                    return (
                        <button
                            aria-pressed={on}
                            className={cn(
                                "flex items-center gap-1.5 rounded-md border border-border/70 px-2 py-1 text-xs font-medium focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
                                on
                                    ? "bg-card"
                                    : "bg-transparent text-muted-foreground line-through",
                            )}
                            key={category}
                            onClick={() => toggle(category)}
                            type="button"
                        >
                            <span
                                aria-hidden="true"
                                className="size-2.5 rounded-sm"
                                style={{
                                    background: on
                                        ? categoryColor(category)
                                        : "transparent",
                                    outline: `1.5px solid ${categoryColor(category)}`,
                                }}
                            />
                            {categoryLabels[category]}
                        </button>
                    );
                })}
                {hasSeverity ? (
                    <button
                        aria-pressed={showExpected}
                        className={cn(
                            "rounded-md border border-border/70 px-2 py-1 text-xs font-medium focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
                            showExpected ? "bg-card" : "text-muted-foreground",
                        )}
                        onClick={() => setShowExpected((on) => !on)}
                        type="button"
                    >
                        Expected misses
                    </button>
                ) : null}
            </div>
            <svg
                aria-label={summary}
                className="h-auto w-full"
                role="img"
                viewBox={`0 0 ${width} ${height}`}
            >
                {(Number.isInteger(top / 2) ? [0, top / 2, top] : [0, top]).map(
                    (tick) => (
                        <g key={tick}>
                            <line
                                stroke="var(--border)"
                                strokeWidth={tick === 0 ? 1 : 0.5}
                                x1={margin.left}
                                x2={width - margin.right}
                                y1={y(tick)}
                                y2={y(tick)}
                            />
                            <text
                                className="fill-muted-foreground text-[11px] tabular-nums"
                                dominantBaseline="middle"
                                textAnchor="end"
                                x={margin.left - 6}
                                y={y(tick)}
                            >
                                {formatCount(tick)}
                            </text>
                        </g>
                    ),
                )}
                {buckets.map((bucket, index) => {
                    const x =
                        margin.left + index * band + (band - barWidth) / 2;
                    const centre = x + barWidth / 2;
                    let base = 0;
                    const severity = bucket.severity;
                    const markers: Array<"fault" | "caller" | "expected"> = [];

                    if (severity?.fault) {
                        markers.push("fault");
                    }

                    if (severity?.caller) {
                        markers.push("caller");
                    }

                    if (
                        showExpected &&
                        severity &&
                        severity.expected + severity.probe > 0
                    ) {
                        markers.push("expected");
                    }

                    return (
                        <g data-testid="activity-column" key={bucket.start}>
                            <title>{bucketTitle(bucket, unit)}</title>
                            <rect
                                fill="transparent"
                                height={plotHeight + margin.top}
                                width={band}
                                x={margin.left + index * band}
                                y={0}
                            />
                            {shown.map((category) => {
                                const count = bucket.byCategory[category] ?? 0;

                                if (count === 0) {
                                    return null;
                                }

                                const segment = (
                                    <rect
                                        data-category={category}
                                        fill={categoryColor(category)}
                                        height={Math.max(
                                            y(base) - y(base + count),
                                            1,
                                        )}
                                        key={category}
                                        stroke="var(--card)"
                                        strokeWidth={1}
                                        width={barWidth}
                                        x={x}
                                        y={y(base + count)}
                                    />
                                );

                                base += count;

                                return segment;
                            })}
                            {markers.map((marker, markerIndex) => {
                                const my = Math.max(
                                    y(base) - 7 - markerIndex * 11,
                                    5,
                                );

                                if (marker === "fault") {
                                    return (
                                        <path
                                            d={`M${centre - 4} ${my - 4}L${centre + 4} ${my + 4}M${centre + 4} ${my - 4}L${centre - 4} ${my + 4}`}
                                            data-testid="fault-marker"
                                            key={marker}
                                            stroke="var(--usage-fault)"
                                            strokeLinecap="round"
                                            strokeWidth={2.5}
                                        />
                                    );
                                }

                                return (
                                    <circle
                                        cx={centre}
                                        cy={my}
                                        data-testid={`${marker}-marker`}
                                        fill={
                                            marker === "expected"
                                                ? "var(--muted-foreground)"
                                                : "none"
                                        }
                                        key={marker}
                                        r={marker === "expected" ? 3 : 4}
                                        stroke={
                                            marker === "caller"
                                                ? "var(--usage-caller)"
                                                : "none"
                                        }
                                        strokeWidth={2}
                                    />
                                );
                            })}
                        </g>
                    );
                })}
                {tickIndexes(buckets.length).map((index) => (
                    <text
                        className="fill-muted-foreground text-[11px]"
                        key={buckets[index].start}
                        textAnchor="middle"
                        x={margin.left + index * band + band / 2}
                        y={height - 8}
                    >
                        {formatBucketStart(buckets[index].start, unit)}
                    </text>
                ))}
            </svg>
            {hasSeverity ? (
                <p className="flex flex-wrap gap-x-4 text-xs text-muted-foreground">
                    <span className="flex items-center gap-1">
                        <svg
                            aria-hidden="true"
                            className="size-3"
                            viewBox="0 0 12 12"
                        >
                            <path
                                d="M2 2L10 10M10 2L2 10"
                                stroke="var(--usage-fault)"
                                strokeLinecap="round"
                                strokeWidth="2.5"
                            />
                        </svg>
                        fault
                    </span>
                    <span className="flex items-center gap-1">
                        <svg
                            aria-hidden="true"
                            className="size-3"
                            viewBox="0 0 12 12"
                        >
                            <circle
                                cx="6"
                                cy="6"
                                fill="none"
                                r="4"
                                stroke="var(--usage-caller)"
                                strokeWidth="2"
                            />
                        </svg>
                        caller error
                    </span>
                    {unit === "hour" ? <span>Times are UTC.</span> : null}
                </p>
            ) : null}
        </div>
    );
}
