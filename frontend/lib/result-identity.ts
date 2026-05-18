export function formatRegistrationUnique(runKey: string): string {
    const trimmed = runKey.trim();

    if (!trimmed || !trimmed.includes("=")) {
        return trimmed;
    }

    const values = new URLSearchParams(trimmed);
    const primary = values.get("runid")?.trim() ?? "";
    const additional = values.get("unique")?.trim() ?? "";

    if (primary && additional) {
        return `${primary} / ${additional}`;
    }

    return primary || additional || trimmed;
}
