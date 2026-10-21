import type { ReactNode } from "react";

import type {
    UsageErrorBreakdown,
    UsageFilter,
    UsageSummary,
    UsageWindow,
} from "@/lib/contracts";
import {
    agentProducts,
    endpointLabel,
    endpointGroups,
    rate,
    usageHref,
    withFilter,
    worstSeverity,
    type UsageEndpoint,
} from "@/lib/usage-derive";
import { cn } from "@/lib/utils";

import {
    categoryColor,
    categoryLabels,
    formatCount,
    formatMs,
    formatRate,
    pluralise,
} from "./format";
import { RankedBars, type RankedRow } from "./ranked-bars";
import { Sparkline } from "./sparkline";

type PanelProps = {
    filter: UsageFilter;
    // filterable is false for servers that ignore filter parameters.
    filterable: boolean;
    summary: UsageSummary;
    window: UsageWindow;
};

function Panel({
    children,
    id,
    note,
    title,
}: {
    children: ReactNode;
    id: string;
    note?: ReactNode;
    title: string;
}) {
    return (
        <section
            aria-labelledby={`${id}-heading`}
            className="flex min-w-0 scroll-mt-6 flex-col gap-2 rounded-xl border border-border/70 bg-card p-4 text-card-foreground shadow-sm"
            id={id}
        >
            <div className="flex flex-wrap items-baseline justify-between gap-2">
                <h2
                    className="text-lg font-semibold tracking-tight"
                    id={`${id}-heading`}
                >
                    {title}
                </h2>
                {note ? (
                    <p className="text-sm text-muted-foreground">{note}</p>
                ) : null}
            </div>
            {children}
        </section>
    );
}

const severityText: Record<string, string> = {
    fault: "fault",
    caller: "caller",
    expected: "expected",
};

// SeverityBadge marks errors by their worst severity: the fault rate for
// faults, the caller error count for caller errors, and the expected misses
// otherwise. Faults link to their listing when href is set.
export function SeverityBadge({
    breakdown,
    href,
    requests,
}: {
    breakdown: UsageErrorBreakdown | undefined;
    href?: string;
    requests: number;
}) {
    const worst = worstSeverity(breakdown);

    if (!breakdown || !worst) {
        return null;
    }

    const text =
        worst === "fault"
            ? `${formatRate(rate(breakdown.fault, requests))} faults`
            : worst === "caller"
              ? `${formatCount(breakdown.caller)} caller`
              : `${formatCount(breakdown.expected + breakdown.probe)} expected`;
    const className = cn(
        "rounded-md px-1.5 py-0.5 text-xs font-medium whitespace-nowrap ring-1",
        worst === "fault" &&
            "bg-destructive/10 text-foreground ring-destructive/50",
        worst === "caller" && "text-foreground ring-[var(--usage-caller)]",
        worst === "expected" && "text-muted-foreground ring-border",
    );

    return href ? (
        <a
            aria-label={`${text} (${severityText[worst]}), see errors`}
            className={cn(
                className,
                "hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
            )}
            href={href}
        >
            {text}
        </a>
    ) : (
        <span className={className}>{text}</span>
    );
}

function filterHref(
    props: PanelProps,
    change: Partial<UsageFilter>,
): string | undefined {
    return props.filterable
        ? usageHref(props.window, withFilter(props.filter, change))
        : undefined;
}

export function PeoplePanel(props: PanelProps) {
    const { summary } = props;
    const rows: RankedRow[] = summary.users.map((user, index) => ({
        byCategory: user.by_category,
        href: filterHref(props, { user: user.name }),
        hrefLabel: `Filter to ${user.name}`,
        key: `${index} ${user.name}`,
        label: user.name,
        total: user.requests,
    }));
    const capped = summary.users.length < summary.totals.users;

    return (
        <Panel
            id="people"
            note={`${formatCount(summary.totals.users)} self-declared${capped ? `, top ${summary.users.length} shown` : ""}`}
            title="People"
        >
            <RankedBars
                empty="No usernames declared."
                label="People"
                rows={rows}
            />
        </Panel>
    );
}

export function ViaPanel(props: PanelProps) {
    const { summary } = props;
    const categoryRows: RankedRow[] = summary.categories
        .filter((row) => row.requests > 0)
        .sort((a, b) => b.requests - a.requests)
        .map((row) => ({
            badge: (
                <SeverityBadge
                    breakdown={row.error_severity}
                    requests={row.requests}
                />
            ),
            color: categoryColor(row.category),
            detail: `${pluralise(row.sessions, "session")} · ${pluralise(row.users, "person", "people")}`,
            href: filterHref(props, { category: row.category }),
            hrefLabel: `Filter to ${categoryLabels[row.category]}`,
            key: row.category,
            label: categoryLabels[row.category],
            total: row.requests,
        }));
    const agentRows: RankedRow[] = agentProducts(summary.mcp_agents).map(
        (agent) => ({
            badge: (
                <SeverityBadge
                    breakdown={agent.severity}
                    requests={agent.requests}
                />
            ),
            color: "var(--usage-mcp)",
            expansion:
                agent.versions.length > 1 ||
                agent.versions[0].value !== agent.product ? (
                    <ul className="flex flex-col gap-0.5 text-xs">
                        {agent.versions.map((version, index) => (
                            <li
                                className="flex justify-between gap-3"
                                key={`${index} ${version.value}`}
                            >
                                {props.filterable ? (
                                    <a
                                        className="font-mono wrap-anywhere underline decoration-border underline-offset-4 hover:decoration-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                                        href={filterHref(props, {
                                            agent: version.value,
                                        })}
                                    >
                                        {version.value}
                                    </a>
                                ) : (
                                    <span className="font-mono wrap-anywhere">
                                        {version.value}
                                    </span>
                                )}
                                <span className="tabular-nums">
                                    {formatCount(version.requests)}
                                </span>
                            </li>
                        ))}
                    </ul>
                ) : undefined,
            expansionLabel: pluralise(agent.versions.length, "version"),
            href: filterHref(props, { agent: agent.product }),
            hrefLabel: `Filter to agent ${agent.product}`,
            key: agent.product,
            label: agent.product,
            total: agent.requests,
        }),
    );
    const otherRows: RankedRow[] = summary.other_user_agents.map(
        (agent, index) => ({
            badge: (
                <SeverityBadge
                    breakdown={agent.error_severity}
                    requests={agent.requests}
                />
            ),
            color: "var(--usage-other)",
            key: `${index} ${agent.value}`,
            label: agent.value || "(none)",
            total: agent.requests,
        }),
    );

    return (
        <Panel id="via" title="Via what">
            <RankedBars
                label="Client types"
                rows={categoryRows}
                empty="No requests."
            />
            <h3 className="pt-2 text-sm font-semibold">MCP agents</h3>
            <RankedBars
                label="MCP agents"
                rows={agentRows}
                empty="No MCP agents."
            />
            {otherRows.length > 0 ? (
                <>
                    <h3 className="pt-2 text-sm font-semibold">
                        Other API user agents
                    </h3>
                    <RankedBars
                        label="Other API user agents"
                        rows={otherRows}
                    />
                </>
            ) : null}
        </Panel>
    );
}

function RouteList({
    anchors,
    props,
    routes,
}: {
    anchors: Map<string, string>;
    props: PanelProps;
    routes: UsageEndpoint[];
}) {
    const series = props.summary.series;

    return (
        <ul className="flex flex-col divide-y divide-border/60">
            {routes.map((endpoint) => {
                const label = endpointLabel(endpoint.method, endpoint.route);
                const anchor = anchors.get(label);
                const href = filterHref(props, {
                    method: endpoint.method,
                    route: endpoint.route,
                });

                return (
                    <li
                        className="flex flex-wrap items-center gap-x-3 gap-y-1 py-1.5 text-xs"
                        key={label}
                    >
                        <span className="min-w-0 flex-1 font-mono wrap-anywhere">
                            {href ? (
                                <a
                                    aria-label={`Filter to ${label}`}
                                    className="underline decoration-border underline-offset-4 hover:decoration-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                                    href={href}
                                >
                                    {label}
                                </a>
                            ) : (
                                label
                            )}
                        </span>
                        {series.length >= 2 ? (
                            <span className="w-20">
                                <Sparkline
                                    values={series.map(
                                        (period) =>
                                            period.by_endpoint[
                                                `${endpoint.method} ${endpoint.route}`
                                            ] ?? 0,
                                    )}
                                />
                            </span>
                        ) : null}
                        <span className="text-muted-foreground tabular-nums">
                            p50 {formatMs(endpoint.p50_ms)} · p95{" "}
                            {formatMs(endpoint.p95_ms)}
                        </span>
                        <span className="tabular-nums">
                            {formatCount(endpoint.requests)}
                        </span>
                        <SeverityBadge
                            breakdown={endpoint.error_severity}
                            href={anchor ? `#${anchor}` : undefined}
                            requests={endpoint.requests}
                        />
                    </li>
                );
            })}
        </ul>
    );
}

export function EndpointsPanel({
    anchors,
    ...props
}: PanelProps & { anchors: Map<string, string> }) {
    const groups = endpointGroups(props.summary.endpoints);
    const rows: RankedRow[] = groups.map((group) => {
        const single = group.routes.length === 1 ? group.routes[0] : undefined;
        const label = single
            ? endpointLabel(single.method, single.route)
            : `${group.prefix}/…`;
        const anchor = single ? anchors.get(label) : undefined;

        return {
            badge: (
                <SeverityBadge
                    breakdown={group.severity}
                    href={anchor ? `#${anchor}` : undefined}
                    requests={group.requests}
                />
            ),
            byCategory: group.byCategory,
            detail: single
                ? `p50 ${formatMs(single.p50_ms)} · p95 ${formatMs(single.p95_ms)}`
                : undefined,
            expansion: single ? undefined : (
                <RouteList
                    anchors={anchors}
                    props={props}
                    routes={group.routes}
                />
            ),
            expansionLabel: pluralise(group.routes.length, "route"),
            href: single
                ? filterHref(props, {
                      method: single.method,
                      route: single.route,
                  })
                : undefined,
            hrefLabel: `Filter to ${label}`,
            key: group.prefix,
            label,
            total: group.requests,
        };
    });

    return (
        <Panel
            id="endpoints"
            note={`${pluralise(props.summary.endpoints.length, "route")} in ${pluralise(groups.length, "group")}`}
            title="Endpoints"
        >
            <RankedBars
                empty="No requests to known routes."
                label="Endpoints"
                rows={rows}
            />
        </Panel>
    );
}

export function ToolsPanel(props: PanelProps) {
    const tools = props.summary.mcp_tools;
    const rows: RankedRow[] = tools.map((tool, index) => ({
        badge: (
            <SeverityBadge
                breakdown={tool.error_severity}
                requests={tool.requests}
            />
        ),
        color: "var(--usage-mcp)",
        href: filterHref(props, { tool: tool.value }),
        hrefLabel: `Filter to tool ${tool.value}`,
        key: `${index} ${tool.value}`,
        label: tool.value,
        total: tool.requests,
    }));

    return (
        <Panel
            id="tools"
            note={`${formatCount(tools.length)} used`}
            title="MCP tools"
        >
            <RankedBars
                empty="No MCP tool calls."
                label="MCP tools"
                rows={rows}
            />
        </Panel>
    );
}
