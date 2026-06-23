"use client";

import { useSyncExternalStore } from "react";

import { formatDate, formatDateTime, formatFileDateTime } from "@/lib/utils";

type LocalTimestampFormat = "date" | "dateTime" | "fileDateTime";

type LocalTimestampProps = {
    className?: string;
    format: LocalTimestampFormat;
    value?: string;
};

const subscribe = () => () => {};
const getClientSnapshot = () => true;
const getServerSnapshot = () => false;

function formatTimestamp(
    value: string | undefined,
    format: LocalTimestampFormat,
    hydrated: boolean,
): string {
    const timeZone = hydrated ? "local" : "utc";

    if (format === "fileDateTime") {
        return formatFileDateTime(value, { timeZone });
    }

    if (!value) {
        return "Unknown time";
    }

    if (format === "date") {
        return formatDate(value, { timeZone });
    }

    return formatDateTime(value, { timeZone });
}

function validDateTime(value: string | undefined): string | undefined {
    if (!value) {
        return undefined;
    }

    const date = new Date(value);

    return Number.isNaN(date.getTime()) ? undefined : value;
}

export function LocalTimestamp({
    className,
    format,
    value,
}: LocalTimestampProps) {
    const hydrated = useSyncExternalStore(
        subscribe,
        getClientSnapshot,
        getServerSnapshot,
    );
    const label = formatTimestamp(value, format, hydrated);
    const dateTime = validDateTime(value);

    if (!dateTime) {
        return <span className={className}>{label}</span>;
    }

    return (
        <time className={className} dateTime={dateTime}>
            {label}
        </time>
    );
}
