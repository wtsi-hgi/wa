import { X } from "lucide-react";

import type { UsageFilter, UsageWindow } from "@/lib/contracts";
import {
    filterKeys,
    isFiltered,
    usageHref,
    withFilter,
    type FilterKey,
} from "@/lib/usage-derive";

import { categoryLabels } from "./format";

const filterNames: Record<Exclude<FilterKey, "method">, string> = {
    category: "Client type",
    user: "Person",
    tool: "MCP tool",
    agent: "Agent",
    route: "Endpoint",
};

function chipValue(
    filter: UsageFilter,
    key: Exclude<FilterKey, "method">,
): string {
    if (key === "category" && filter.category) {
        return categoryLabels[filter.category];
    }

    if (key === "route") {
        return `${filter.method} ${filter.route}`;
    }

    return filter[key] ?? "";
}

// FilterBar shows the active filters as removable chips, or how to add one.
// supported is false when the server ignored the filter.
export function FilterBar({
    filter,
    supported,
    window,
}: {
    filter: UsageFilter;
    supported: boolean;
    window: UsageWindow;
}) {
    if (!isFiltered(filter)) {
        return (
            <p className="text-sm text-muted-foreground">
                {supported
                    ? "Showing all traffic. Select a person, client type, agent, endpoint, or tool to filter."
                    : "Showing all traffic. This MLWH server cannot filter usage."}
            </p>
        );
    }

    if (!supported) {
        return (
            <p
                className="rounded-lg border border-destructive/50 p-2 text-sm"
                role="status"
            >
                This MLWH server cannot filter usage, so all traffic is shown.{" "}
                <a
                    className="underline underline-offset-4"
                    href={usageHref(window, {})}
                >
                    Clear the filter
                </a>
            </p>
        );
    }

    const keys = filterKeys.filter(
        (key): key is Exclude<FilterKey, "method"> =>
            key !== "method" && Boolean(filter[key]),
    );

    return (
        <div
            aria-label="Filters"
            className="flex flex-wrap items-center gap-2 text-sm"
            role="group"
        >
            <span className="text-muted-foreground">Showing only:</span>
            {keys.map((key) => (
                <span
                    className="flex items-center gap-1 rounded-full border border-border/70 bg-card py-0.5 pr-1 pl-3"
                    data-testid="filter-chip"
                    key={key}
                >
                    <span className="text-muted-foreground">
                        {filterNames[key]}
                    </span>
                    <span className="font-medium wrap-anywhere">
                        {chipValue(filter, key)}
                    </span>
                    <a
                        aria-label={`Remove filter ${filterNames[key]} ${chipValue(filter, key)}`}
                        className="rounded-full p-1 hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                        href={usageHref(
                            window,
                            withFilter(filter, { [key]: undefined }),
                        )}
                    >
                        <X aria-hidden="true" className="size-3.5" />
                    </a>
                </span>
            ))}
            {keys.length > 1 ? (
                <a
                    className="underline decoration-border underline-offset-4 hover:decoration-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                    href={usageHref(window, {})}
                >
                    Clear all
                </a>
            ) : null}
        </div>
    );
}
