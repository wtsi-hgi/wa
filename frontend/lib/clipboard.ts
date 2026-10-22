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

/**
 * Copies text to the clipboard, falling back to a hidden textarea and
 * execCommand when the async Clipboard API is missing or refuses (for
 * example on plain-HTTP origins). Returns whether the copy succeeded.
 */
export async function copyText(value: string): Promise<boolean> {
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
