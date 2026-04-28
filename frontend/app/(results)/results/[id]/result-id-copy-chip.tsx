"use client";

import { Copy } from "lucide-react";
import { toast } from "sonner";

type ResultIdCopyChipProps = {
    resultId: string;
};

const TRUNCATED_HEAD = 14;
const TRUNCATED_TAIL = 10;

function truncateResultId(resultId: string): string {
    if (resultId.length <= TRUNCATED_HEAD + TRUNCATED_TAIL + 3) {
        return resultId;
    }

    return `${resultId.slice(0, TRUNCATED_HEAD)}...${resultId.slice(-TRUNCATED_TAIL)}`;
}

function fallbackCopyText(value: string): boolean {
    if (
        typeof document === "undefined" ||
        typeof document.execCommand !== "function"
    ) {
        return false;
    }

    const textarea = document.createElement("textarea");

    textarea.value = value;
    textarea.setAttribute("readonly", "");
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    textarea.style.pointerEvents = "none";

    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();

    try {
        return document.execCommand("copy");
    } finally {
        document.body.removeChild(textarea);
    }
}

async function copyText(value: string): Promise<boolean> {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
        try {
            await navigator.clipboard.writeText(value);
            return true;
        } catch {
            return fallbackCopyText(value);
        }
    }

    return fallbackCopyText(value);
}

export function ResultIdCopyChip({ resultId }: ResultIdCopyChipProps) {
    const displayId = truncateResultId(resultId);

    async function handleCopy() {
        const copied = await copyText(resultId);

        if (copied) {
            toast.success("Result ID copied");
            return;
        }

        toast.error("Could not copy result ID");
    }

    return (
        <button
            type="button"
            onClick={() => {
                void handleCopy();
            }}
            aria-label={`Copy result ID ${resultId}`}
            title="Copy full result ID"
            data-result-id-copy={resultId}
            className="inline-flex max-w-full cursor-pointer items-center gap-2 rounded-full border border-border/70 bg-background/85 px-4 py-2 text-sm text-muted-foreground transition hover:border-foreground/20 hover:text-foreground"
        >
            <Copy className="h-4 w-4 shrink-0" aria-hidden="true" />
            <span className="font-mono text-xs uppercase tracking-[0.24em]">
                {displayId}
            </span>
        </button>
    );
}
