import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

const monthLabels = [
    "Jan",
    "Feb",
    "Mar",
    "Apr",
    "May",
    "Jun",
    "Jul",
    "Aug",
    "Sep",
    "Oct",
    "Nov",
    "Dec",
];

export function cn(...inputs: ClassValue[]) {
    return twMerge(clsx(inputs));
}

export function formatBytes(size: number | undefined): string {
    if (size === undefined) {
        return "Unknown size";
    }

    if (size < 1024) {
        return `${size} B`;
    }

    if (size < 1024 * 1024) {
        return `${(size / 1024).toFixed(1)} KB`;
    }

    if (size < 1024 * 1024 * 1024) {
        return `${(size / (1024 * 1024)).toFixed(1)} MB`;
    }

    return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

type TimestampTimeZone = "local" | "utc";

type TimestampFormatOptions = {
    timeZone?: TimestampTimeZone;
};

function parseTimestamp(value: string): Date | null {
    const date = new Date(value);

    if (Number.isNaN(date.getTime())) {
        return null;
    }

    return date;
}

function datePart(
    date: Date,
    timeZone: TimestampTimeZone,
    part: "date" | "month" | "year" | "hours" | "minutes",
): number {
    if (timeZone === "utc") {
        switch (part) {
            case "date":
                return date.getUTCDate();
            case "month":
                return date.getUTCMonth();
            case "year":
                return date.getUTCFullYear();
            case "hours":
                return date.getUTCHours();
            case "minutes":
                return date.getUTCMinutes();
        }
    }

    switch (part) {
        case "date":
            return date.getDate();
        case "month":
            return date.getMonth();
        case "year":
            return date.getFullYear();
        case "hours":
            return date.getHours();
        case "minutes":
            return date.getMinutes();
    }
}

export function formatDate(
    value: string,
    { timeZone = "local" }: TimestampFormatOptions = {},
): string {
    const date = parseTimestamp(value);

    if (!date) {
        return value;
    }

    return `${datePart(date, timeZone, "date")} ${monthLabels[datePart(date, timeZone, "month")]} ${datePart(date, timeZone, "year")}`;
}

export function formatDateTime(
    value: string,
    { timeZone = "local" }: TimestampFormatOptions = {},
): string {
    const date = parseTimestamp(value);

    if (!date) {
        return value;
    }

    const hours = String(datePart(date, timeZone, "hours")).padStart(2, "0");
    const minutes = String(datePart(date, timeZone, "minutes")).padStart(
        2,
        "0",
    );

    return `${formatDate(value, { timeZone })}, ${hours}:${minutes}`;
}

export function formatFileDateTime(
    value: string | undefined,
    { timeZone = "local" }: TimestampFormatOptions = {},
): string {
    if (!value) {
        return "Unknown time";
    }

    const date = parseTimestamp(value);

    if (!date) {
        return value;
    }

    const year = datePart(date, timeZone, "year");
    const month = String(datePart(date, timeZone, "month") + 1).padStart(
        2,
        "0",
    );
    const day = String(datePart(date, timeZone, "date")).padStart(2, "0");
    const hours = String(datePart(date, timeZone, "hours")).padStart(2, "0");
    const minutes = String(datePart(date, timeZone, "minutes")).padStart(
        2,
        "0",
    );

    return `${year}-${month}-${day} ${hours}:${minutes}`;
}

export function formatUtcDate(value: string): string {
    return formatDate(value, { timeZone: "utc" });
}

export function formatUtcDateTime(value: string): string {
    return formatDateTime(value, { timeZone: "utc" });
}
