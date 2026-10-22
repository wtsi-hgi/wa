"use client";

import {
    createContext,
    type ReactNode,
    useContext,
    useEffect,
    useMemo,
    useRef,
} from "react";
import { ChevronLeft, ChevronRight, X } from "lucide-react";

import type { FileEntry } from "@/lib/contracts";

export type PreviewGalleryContextValue = {
    has: (path: string) => boolean;
    open: (path: string) => void;
};

/**
 * Lets a preview's enlarge control open the shared gallery lightbox instead
 * of its own, so the expanded view can step through every matching file.
 */
export const PreviewGalleryContext =
    createContext<PreviewGalleryContextValue | null>(null);

/**
 * Builds the gallery context for files, keeping the same value while the
 * set of paths is unchanged so memoised previews do not re-render.
 */
export function usePreviewGalleryContextValue(
    files: readonly FileEntry[],
    open: (path: string) => void,
): PreviewGalleryContextValue | null {
    const key = files.map((file) => file.path).join("\u0000");

    return useMemo(() => {
        if (!key) {
            return null;
        }

        const paths = new Set(key.split("\u0000"));

        return { has: (path) => paths.has(path), open };
    }, [key, open]);
}

/** Returns a function that opens path in the gallery, or null when the path is not part of one. */
export function usePreviewGalleryOpener(
    path: string | undefined,
): (() => void) | null {
    const gallery = useContext(PreviewGalleryContext);

    if (!gallery || path === undefined || !gallery.has(path)) {
        return null;
    }

    return () => gallery.open(path);
}

/** Path of file relative to basePath, without a leading slash. */
export function galleryRelativePath(path: string, basePath?: string): string {
    if (basePath && basePath !== "/" && path.startsWith(`${basePath}/`)) {
        return path.slice(basePath.length + 1);
    }

    return path.replace(/^\/+/, "");
}

function isEditableTarget(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) {
        return false;
    }

    return (
        target.isContentEditable ||
        target instanceof HTMLInputElement ||
        target instanceof HTMLSelectElement ||
        target instanceof HTMLTextAreaElement
    );
}

const navButtonClass =
    "inline-flex size-9 shrink-0 items-center justify-center rounded-full border border-border/70 bg-background/90 text-foreground transition hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-40";

export function PreviewGalleryLightbox({
    basePath,
    files,
    index,
    onClose,
    onIndexChange,
    renderExpanded,
}: {
    basePath?: string;
    files: readonly FileEntry[];
    index: number;
    onClose: () => void;
    onIndexChange: (index: number) => void;
    renderExpanded: (file: FileEntry) => ReactNode;
}) {
    const file = files[index];
    const closeButtonRef = useRef<HTMLButtonElement | null>(null);
    const hasPrevious = index > 0;
    const hasNext = index < files.length - 1;
    const keyStateRef = useRef({ hasNext, hasPrevious, index });

    useEffect(() => {
        keyStateRef.current = { hasNext, hasPrevious, index };
    }, [hasNext, hasPrevious, index]);

    useEffect(() => {
        const previouslyFocused =
            document.activeElement instanceof HTMLElement
                ? document.activeElement
                : null;

        closeButtonRef.current?.focus();

        return () => {
            if (previouslyFocused?.isConnected) {
                previouslyFocused.focus();
            }
        };
    }, []);

    useEffect(() => {
        function handleKeyDown(event: KeyboardEvent) {
            if (event.key === "Escape") {
                event.preventDefault();
                onClose();
                return;
            }

            if (
                event.altKey ||
                event.ctrlKey ||
                event.metaKey ||
                isEditableTarget(event.target)
            ) {
                return;
            }

            const state = keyStateRef.current;

            if (event.key === "ArrowLeft" && state.hasPrevious) {
                event.preventDefault();
                onIndexChange(state.index - 1);
            } else if (event.key === "ArrowRight" && state.hasNext) {
                event.preventDefault();
                onIndexChange(state.index + 1);
            }
        }

        window.addEventListener("keydown", handleKeyDown);

        return () => {
            window.removeEventListener("keydown", handleKeyDown);
        };
    }, [onClose, onIndexChange]);

    if (!file) {
        return null;
    }

    const relativePath = galleryRelativePath(file.path, basePath);
    // Named like the single-file enlarged views, so the dialog names the
    // file it currently shows.
    const fileName = file.path.split("/").pop() ?? file.path;

    return (
        <div
            aria-label={`Enlarged ${fileName} preview`}
            aria-modal="true"
            className="fixed inset-0 z-50 flex items-center justify-center p-2 sm:p-4"
            data-preview-gallery="true"
            role="dialog"
        >
            <button
                type="button"
                aria-label="Close preview gallery backdrop"
                className="absolute inset-0 bg-[color:rgba(17,24,39,0.75)] backdrop-blur-sm"
                onClick={onClose}
                tabIndex={-1}
            />
            <div className="relative z-10 flex h-full max-h-full w-full max-w-[min(100%,120rem)] flex-col gap-3 rounded-[1.5rem] border border-white/15 bg-background p-3 text-foreground shadow-2xl sm:p-4">
                <div className="flex min-w-0 items-center gap-2">
                    <button
                        type="button"
                        aria-label="Previous file"
                        className={navButtonClass}
                        data-preview-gallery-previous="true"
                        disabled={!hasPrevious}
                        onClick={() => onIndexChange(index - 1)}
                        title="Previous file (Left arrow)"
                    >
                        <ChevronLeft className="size-4" aria-hidden="true" />
                    </button>
                    <button
                        type="button"
                        aria-label="Next file"
                        className={navButtonClass}
                        data-preview-gallery-next="true"
                        disabled={!hasNext}
                        onClick={() => onIndexChange(index + 1)}
                        title="Next file (Right arrow)"
                    >
                        <ChevronRight className="size-4" aria-hidden="true" />
                    </button>
                    <span
                        aria-live="polite"
                        className="shrink-0 text-sm tabular-nums text-muted-foreground"
                        data-preview-gallery-position="true"
                    >
                        {index + 1} of {files.length}
                    </span>
                    <p
                        className="min-w-0 flex-1 truncate font-mono text-sm font-medium"
                        data-preview-gallery-path={file.path}
                        title={file.path}
                    >
                        {relativePath}
                    </p>
                    <button
                        ref={closeButtonRef}
                        type="button"
                        aria-label="Close preview gallery"
                        className={navButtonClass}
                        onClick={onClose}
                    >
                        <X className="size-4" aria-hidden="true" />
                    </button>
                </div>
                <div
                    key={file.path}
                    className="flex min-h-0 flex-1 flex-col overflow-auto"
                    data-preview-gallery-body={file.path}
                >
                    {renderExpanded(file)}
                </div>
            </div>
        </div>
    );
}
