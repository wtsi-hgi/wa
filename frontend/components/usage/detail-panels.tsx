import type { ReactNode } from "react";

import type { UsageSummary } from "@/lib/contracts";
import {
    expectedGroups,
    expectedPrefix,
    routeGroups,
} from "@/lib/usage-derive";

import { RouteErrorList } from "./error-group-entry";
import { categoryLabels, formatCount, pluralise } from "./format";
import { RankedBars } from "./ranked-bars";

// CollapsedPanel is a closed disclosure titled with its heading and count.
function CollapsedPanel({
    children,
    id,
    title,
}: {
    children: ReactNode;
    id: string;
    title: string;
}) {
    return (
        <section
            aria-labelledby={`${id}-heading`}
            className="scroll-mt-6"
            id={id}
        >
            <details className="rounded-xl border border-border/70 bg-card text-card-foreground shadow-sm">
                <summary className="cursor-pointer rounded-xl px-4 py-3 hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                    <h2
                        className="inline text-base font-semibold"
                        id={`${id}-heading`}
                    >
                        {title}
                    </h2>
                </summary>
                <div className="flex flex-col gap-3 px-4 pb-4">{children}</div>
            </details>
        </section>
    );
}

export function ExpectedPanel({ summary }: { summary: UsageSummary }) {
    const routes = routeGroups(
        expectedGroups(summary.errors ?? []),
        expectedPrefix,
    );
    const total = routes.reduce((sum, route) => sum + route.total, 0);

    return (
        <CollapsedPanel
            id="expected"
            title={`Expected outcomes (${pluralise(total, "lookup miss", "lookup misses")})`}
        >
            <p className="max-w-3xl text-sm text-muted-foreground">
                Lookups whose answer was that the identifier is unknown or names
                several records. Nothing to fix: wa mlwh info tries each kind of
                identifier in turn, so misses from lookup probes are normal.
            </p>
            {routes.length === 0 ? (
                <p className="text-sm text-muted-foreground">None.</p>
            ) : (
                <RouteErrorList headingLevel={3} routes={routes} />
            )}
        </CollapsedPanel>
    );
}

export function SessionsPanel({ summary }: { summary: UsageSummary }) {
    const categories = summary.categories.filter((row) => row.requests > 0);
    const clientRows = summary.clients.map((client, index) => ({
        byCategory: client.by_category,
        key: `${index} ${client.name}`,
        label: client.by_ip ? `${client.name} (by IP)` : client.name,
        total: client.requests,
    }));
    const clientsTotal = summary.totals.sessions + summary.totals.ip_clients;

    return (
        <CollapsedPanel
            id="sessions"
            title={`Sessions and clients (${pluralise(summary.totals.sessions, "session")}, ${pluralise(summary.totals.ip_clients, "client")} by IP)`}
        >
            {categories.length > 0 ? (
                <div className="overflow-x-auto rounded-lg border border-border/70">
                    <table className="w-full text-sm">
                        <caption className="sr-only">
                            Sessions by client type
                        </caption>
                        <thead className="bg-muted/40 text-left">
                            <tr>
                                {[
                                    "Client type",
                                    "Sessions",
                                    "Clients by IP",
                                    "People",
                                    "Requests per session (median)",
                                    "Requests per session (max)",
                                ].map((header, index) => (
                                    <th
                                        className={
                                            index === 0
                                                ? "px-3 py-1.5 font-medium"
                                                : "px-3 py-1.5 text-right font-medium"
                                        }
                                        key={header}
                                        scope="col"
                                    >
                                        {header}
                                    </th>
                                ))}
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-border/60 tabular-nums">
                            {categories.map((row) => (
                                <tr key={row.category}>
                                    <th
                                        className="px-3 py-1 text-left font-normal"
                                        scope="row"
                                    >
                                        {categoryLabels[row.category]}
                                    </th>
                                    {[
                                        row.sessions,
                                        row.ip_clients,
                                        row.users,
                                        row.requests_per_session_median,
                                        row.requests_per_session_max,
                                    ].map((value, index) => (
                                        <td
                                            className="px-3 py-1 text-right"
                                            key={index}
                                        >
                                            {formatCount(value)}
                                        </td>
                                    ))}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            ) : null}
            <p className="max-w-3xl text-sm text-muted-foreground">
                A session is one client ID: a CLI run, an MCP session, or a
                browser. Callers without one are counted by IP, so callers
                behind one host merge.
            </p>
            <h3 className="text-sm font-semibold">
                Largest clients
                {summary.clients.length < clientsTotal
                    ? ` (top ${summary.clients.length} of ${formatCount(clientsTotal)})`
                    : ""}
            </h3>
            <RankedBars label="Largest clients" rows={clientRows} />
        </CollapsedPanel>
    );
}
