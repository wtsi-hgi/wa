import { useCallback, useSyncExternalStore } from "react";

export const fileBrowserCompactStorageKey = "wa:file-browser:compact";
export const fileBrowserListWidthStorageKey = "wa:file-browser:list-width";
export const fileBrowserPreviewHeightStorageKey =
    "wa:file-browser:preview-height";

export const FILE_LIST_WIDTH_DEFAULT = 25;
export const FILE_LIST_WIDTH_MIN = 12;
export const FILE_LIST_WIDTH_MAX = 70;

const listeners = new Set<() => void>();

function notify(): void {
    for (const listener of listeners) {
        listener();
    }
}

function subscribe(listener: () => void): () => void {
    listeners.add(listener);

    if (typeof window !== "undefined") {
        window.addEventListener("storage", listener);
    }

    return () => {
        listeners.delete(listener);

        if (typeof window !== "undefined") {
            window.removeEventListener("storage", listener);
        }
    };
}

function readStoredValue(key: string): string | null {
    if (typeof window === "undefined") {
        return null;
    }

    try {
        return window.localStorage.getItem(key);
    } catch {
        return null;
    }
}

function writeStoredValue(key: string, value: string): void {
    if (typeof window !== "undefined") {
        try {
            window.localStorage.setItem(key, value);
        } catch {
            // Browser storage can be unavailable; the preference then only
            // lasts for this page.
        }
    }

    notify();
}

export function clampFileListWidth(value: number): number {
    if (!Number.isFinite(value)) {
        return FILE_LIST_WIDTH_DEFAULT;
    }

    return Math.min(
        FILE_LIST_WIDTH_MAX,
        Math.max(FILE_LIST_WIDTH_MIN, Math.round(value)),
    );
}

function parseStoredNumber(value: string | null): number | null {
    if (value === null || value.trim() === "") {
        return null;
    }

    const parsed = Number(value);

    return Number.isFinite(parsed) ? parsed : null;
}

/** Whether the file browser shows the dense, metadata-free file list. */
export function useFileBrowserCompact(): [boolean, (value: boolean) => void] {
    const compact = useSyncExternalStore(
        subscribe,
        () => readStoredValue(fileBrowserCompactStorageKey) === "1",
        () => false,
    );
    const setCompact = useCallback((value: boolean) => {
        writeStoredValue(fileBrowserCompactStorageKey, value ? "1" : "0");
    }, []);

    return [compact, setCompact];
}

/** The file list's share of the list/preview split, as a percentage. */
export function useFileBrowserListWidth(): [number, (value: number) => void] {
    const width = useSyncExternalStore(
        subscribe,
        () => {
            const stored = parseStoredNumber(
                readStoredValue(fileBrowserListWidthStorageKey),
            );

            return stored === null
                ? FILE_LIST_WIDTH_DEFAULT
                : clampFileListWidth(stored);
        },
        () => FILE_LIST_WIDTH_DEFAULT,
    );
    const setWidth = useCallback((value: number) => {
        writeStoredValue(
            fileBrowserListWidthStorageKey,
            String(clampFileListWidth(value)),
        );
    }, []);

    return [width, setWidth];
}

/** The last preview height the user chose, or the fallback. */
export function useStoredPreviewHeight(
    fallback: number,
): [number, (value: number) => void] {
    const height = useSyncExternalStore(
        subscribe,
        () =>
            parseStoredNumber(
                readStoredValue(fileBrowserPreviewHeightStorageKey),
            ) ?? fallback,
        () => fallback,
    );
    const setHeight = useCallback((value: number) => {
        if (!Number.isFinite(value)) {
            return;
        }

        writeStoredValue(
            fileBrowserPreviewHeightStorageKey,
            String(Math.round(value)),
        );
    }, []);

    return [height, setHeight];
}
