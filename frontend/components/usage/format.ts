import {
    usageCategorySchema,
    type UsageCategory,
    type UsageWindow,
} from "@/lib/contracts";
import type { CountMap } from "@/lib/usage-derive";

export const categoryLabels: Record<UsageCategory, string> = {
    cli: "CLI",
    mcp: "MCP",
    web: "Web",
    results_server: "Results server",
    mlwhdiff_server: "mlwhdiff server",
    other: "Other API",
};

// categoryColor is the CSS colour of a category's marks (globals.css).
export function categoryColor(category: UsageCategory): string {
    return `var(--usage-${category.replace("_", "-")})`;
}

export const windowLabels: Record<UsageWindow, string> = {
    "24h": "Last 24 hours",
    "7d": "Last 7 days",
    "30d": "Last 30 days",
    all: "All time",
};

const countFormat = new Intl.NumberFormat("en-GB");

export function formatCount(value: number): string {
    return countFormat.format(value);
}

export function formatRate(rate: number): string {
    return `${(rate * 100).toFixed(rate > 0 && rate < 0.01 ? 2 : 1)}%`;
}

export function formatMs(ms: number): string {
    return ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${ms.toFixed(1)} ms`;
}

export function pluralise(
    n: number,
    noun: string,
    plural = `${noun}s`,
): string {
    return `${formatCount(n)} ${n === 1 ? noun : plural}`;
}

// formatCategories lists the non-zero categories of byCategory in category
// order, as "<label> <n>" joined by ", ". <n> is a plain integer: a thousands
// separator would be ambiguous with the ", " join.
export function formatCategories(byCategory: CountMap): string {
    return usageCategorySchema.options
        .filter((category) => (byCategory[category] ?? 0) > 0)
        .map(
            (category) => `${categoryLabels[category]} ${byCategory[category]}`,
        )
        .join(", ");
}

// formatCounts lists every entry of counts in key order as "<key> <n>" joined
// by ", ", with <n> a plain integer like formatCategories.
export function formatCounts(counts: CountMap): string {
    return Object.entries(counts)
        .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
        .map(([key, value]) => `${key} ${value}`)
        .join(", ");
}

// formatChange is the signed relative change from previous to latest, or
// "new" when previous is 0.
export function formatChange(latest: number, previous: number): string {
    if (previous === 0) {
        return latest === 0 ? "no change" : "new";
    }

    const change = Math.round(((latest - previous) / previous) * 100);

    return change === 0
        ? "no change"
        : `${change > 0 ? "+" : "−"}${Math.abs(change)}%`;
}

// formatDifference is the signed difference from previous to latest.
export function formatDifference(latest: number, previous: number): string {
    const difference = latest - previous;

    return difference === 0
        ? "no change"
        : `${difference > 0 ? "+" : "−"}${formatCount(Math.abs(difference))}`;
}

const dayFormat = new Intl.DateTimeFormat("en-GB", {
    day: "numeric",
    month: "short",
    timeZone: "UTC",
});
const hourFormat = new Intl.DateTimeFormat("en-GB", {
    hour: "2-digit",
    minute: "2-digit",
    timeZone: "UTC",
});

// formatBucketStart labels a bucket start (RFC3339 UTC) in UTC: "14:00" for
// hours, "21 Oct" for days and weeks.
export function formatBucketStart(start: string, unit: string): string {
    const date = new Date(start);

    return unit === "hour" ? hourFormat.format(date) : dayFormat.format(date);
}

// formatBucketTime is formatBucketStart with the date for hours, "21 Oct
// 14:00", for text that has no axis to place it.
export function formatBucketTime(start: string, unit: string): string {
    return unit === "hour"
        ? `${dayFormat.format(new Date(start))} ${hourFormat.format(new Date(start))}`
        : dayFormat.format(new Date(start));
}

export function formatDay(value: string): string {
    return dayFormat.format(new Date(value));
}
