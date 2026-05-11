export type SearchFilters = Record<string, string[]>;

export function parseSearchFilters(params: URLSearchParams): SearchFilters {
    const filters: SearchFilters = {};

    for (const [key, rawValue] of params.entries()) {
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

    for (const [key, values] of Object.entries(filters)) {
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
