"use client";

import { Copy } from "lucide-react";

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

export function ResultIdCopyChip({ resultId }: ResultIdCopyChipProps) {
    const displayId = truncateResultId(resultId);

    async function handleCopy() {
        if (!navigator.clipboard?.writeText) {
            return;
        }

        await navigator.clipboard.writeText(resultId);
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
            className="inline-flex max-w-full items-center gap-2 rounded-full border border-border/70 bg-background/85 px-4 py-2 text-sm text-muted-foreground transition hover:border-foreground/20 hover:text-foreground"
        >
            <Copy className="h-4 w-4 shrink-0" aria-hidden="true" />
            <span className="font-mono text-xs uppercase tracking-[0.24em]">
                {displayId}
            </span>
        </button>
    );
}