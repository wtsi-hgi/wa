export type SearchFilters = Record<string, string[]>;

const legacySearchFilterKeys: Record<string, string> = {
    output_dir_prefix: "output_directory",
};

export function canonicalSearchFilterKey(key: string): string {
    return legacySearchFilterKeys[key] ?? key;
}

export function parseSearchFilters(params: URLSearchParams): SearchFilters {
    const filters: SearchFilters = {};

    for (const [rawKey, rawValue] of params.entries()) {
        const key = canonicalSearchFilterKey(rawKey);
        const value = rawValue.trim();
        if (!value) {
            continue;
        }

        if (!filters[key]) {
            filters[key] = [];
        }

        filters[key].push(value);
    }

    return filters;
}

export function buildSearchQuery(filters: SearchFilters): URLSearchParams {
    const params = new URLSearchParams();

    for (const [rawKey, values] of Object.entries(filters)) {
        const key = canonicalSearchFilterKey(rawKey);
        for (const rawValue of values) {
            const value = rawValue.trim();
            if (!value) {
                continue;
            }

            params.append(key, value);
        }
    }

    return params;
}
