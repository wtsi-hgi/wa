import { createElement, type ComponentPropsWithoutRef } from "react";

import { LocalTimestamp } from "@/components/local-timestamp";
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import { usageErrorKindHelp, usageGroupHelp } from "@/lib/usage-error-help";
import {
    type AttentionRoute,
    type CountMap,
    type UsageErrorGroup,
} from "@/lib/usage-derive";
import { cn } from "@/lib/utils";

import {
    categoryLabels,
    formatCategories,
    formatCount,
    formatCounts,
    pluralise,
} from "./format";

type UsageErrorExample = UsageErrorGroup["examples"][number];

// Heading renders an h<level> heading, at most h6.
function Heading({
    level,
    ...props
}: { level: number } & ComponentPropsWithoutRef<"h3">) {
    return createElement(`h${Math.min(level, 6)}`, props);
}

// exampleRequest is the example's path and query, or a note for rows stored
// before paths of matched routes were recorded.
function exampleRequest(example: UsageErrorExample): string {
    if (!example.path) {
        return "(not recorded)";
    }

    return example.query ? `${example.path}?${example.query}` : example.path;
}

// exampleCaller lists the example's category, MCP tool and agent, and user
// (or session when no user was declared), skipping empty values.
function exampleCaller(example: UsageErrorExample): string {
    return [
        categoryLabels[example.category],
        example.mcp_tool,
        example.mcp_agent,
        example.username || example.client_id,
    ]
        .filter(Boolean)
        .join(" · ");
}

function DetailLine({ label, value }: { label: string; value: string }) {
    if (!value) {
        return null;
    }

    return (
        <p className="text-sm wrap-anywhere">
            <span className="text-muted-foreground">{label}:</span> {value}
        </p>
    );
}

// CausesLine lists the internal error kinds, with the advice for known ones.
function CausesLine({ kinds }: { kinds: CountMap }) {
    const entries = Object.entries(kinds).sort(([a], [b]) =>
        a < b ? -1 : a > b ? 1 : 0,
    );

    if (entries.length === 0) {
        return null;
    }

    return (
        <div className="flex flex-col gap-1 text-sm">
            <p className="wrap-anywhere">
                <span className="text-muted-foreground">Causes:</span>{" "}
                {entries.map(([kind, count]) => `${kind} ${count}`).join(", ")}
            </p>
            {entries
                .filter(([kind]) => Object.hasOwn(usageErrorKindHelp, kind))
                .map(([kind]) => (
                    <p className="max-w-3xl text-muted-foreground" key={kind}>
                        {usageErrorKindHelp[kind]}
                    </p>
                ))}
        </div>
    );
}

function ExamplesTable({ examples }: { examples: UsageErrorExample[] }) {
    if (examples.length === 0) {
        return <p className="text-sm text-muted-foreground">None.</p>;
    }

    return (
        <div className="rounded-lg border border-border/70 bg-card">
            <Table>
                <TableHeader>
                    <TableRow className="hover:bg-transparent">
                        {["Time", "Request", "Caller", "Message"].map(
                            (header) => (
                                <TableHead
                                    className="h-9 whitespace-nowrap"
                                    key={header}
                                    scope="col"
                                >
                                    {header}
                                </TableHead>
                            ),
                        )}
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {examples.map((example, index) => (
                        <TableRow key={`${index} ${example.ts}`}>
                            <TableCell className="py-2">
                                <LocalTimestamp
                                    className="whitespace-nowrap tabular-nums"
                                    format="dateTime"
                                    value={example.ts}
                                />
                            </TableCell>
                            <TableCell className="min-w-40 py-2 font-mono text-xs wrap-anywhere">
                                {exampleRequest(example)}
                            </TableCell>
                            <TableCell className="min-w-40 py-2 wrap-anywhere">
                                {exampleCaller(example)}
                            </TableCell>
                            <TableCell className="min-w-40 py-2 wrap-anywhere">
                                {example.message}
                            </TableCell>
                        </TableRow>
                    ))}
                </TableBody>
            </Table>
        </div>
    );
}

// ErrorGroupEntry shows one error group: its status, code and meaning,
// counts and time span, and its messages and examples behind a disclosure.
export function ErrorGroupEntry({
    group,
    headingLevel,
    id,
}: {
    group: UsageErrorGroup;
    headingLevel: number;
    id: string;
}) {
    const help = usageGroupHelp(group.error_code, group.status, group.route);
    const serverError = group.status >= 500;
    const headingId = `${id}-heading`;

    return (
        <article
            aria-labelledby={headingId}
            className={cn(
                "relative flex flex-col gap-2 py-3 pr-3 pl-5 before:absolute before:inset-y-3 before:left-0 before:w-1 before:rounded-r-full",
                serverError ? "before:bg-destructive" : "before:bg-accent",
            )}
        >
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <Heading
                    className="flex min-w-0 items-baseline gap-2 text-sm font-medium"
                    id={headingId}
                    level={headingLevel}
                >
                    <span
                        className={cn(
                            "rounded-md px-1.5 py-0.5 text-xs font-semibold tabular-nums ring-1",
                            serverError
                                ? "bg-destructive/10 ring-destructive/40"
                                : "bg-accent/30 ring-accent",
                        )}
                    >
                        {group.status}
                    </span>{" "}
                    <span
                        className={cn(
                            "wrap-anywhere",
                            group.error_code !== null && "font-mono",
                        )}
                    >
                        {help.label}
                    </span>
                </Heading>
                <span className="text-sm font-semibold tabular-nums">
                    {pluralise(group.count, "error")}
                </span>
                {group.probe ? (
                    <span className="text-xs text-muted-foreground">
                        from lookup probes
                    </span>
                ) : null}
                <span className="text-xs text-muted-foreground">
                    first{" "}
                    <LocalTimestamp
                        format="dateTime"
                        value={group.first_seen}
                    />
                    , latest{" "}
                    <LocalTimestamp format="dateTime" value={group.last_seen} />
                </span>
            </div>
            <p className="max-w-3xl text-sm">{help.meaning}</p>
            <CausesLine kinds={group.error_kinds} />
            <details className="rounded-lg border border-border/70 bg-muted/30 open:bg-transparent">
                <summary className="cursor-pointer rounded-lg px-3 py-1.5 text-sm font-medium hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none">
                    Messages and examples (
                    {pluralise(group.examples.length, "example")})
                </summary>
                <div className="flex flex-col gap-3 px-3 pt-1 pb-3">
                    <div className="flex flex-col gap-1">
                        <DetailLine
                            label="Callers"
                            value={formatCategories(group.by_category)}
                        />
                        <DetailLine
                            label="MCP tools"
                            value={formatCounts(group.by_tool)}
                        />
                        <DetailLine
                            label="MCP agents"
                            value={formatCounts(group.by_agent)}
                        />
                    </div>
                    {group.messages.length > 0 ? (
                        <div className="flex flex-col gap-1">
                            <Heading
                                className="text-sm font-medium"
                                id={`${id}-messages`}
                                level={headingLevel + 1}
                            >
                                Top messages
                            </Heading>
                            <ol
                                aria-labelledby={`${id}-messages`}
                                className="flex flex-col divide-y divide-border/60 rounded-lg border border-border/70 bg-card"
                            >
                                {group.messages.map((message, index) => (
                                    <li
                                        className="flex items-baseline justify-between gap-4 px-3 py-1.5 text-sm"
                                        key={`${index} ${message.message}`}
                                    >
                                        <span className="font-mono text-xs wrap-anywhere">
                                            {message.message}
                                        </span>
                                        <span className="tabular-nums">
                                            {formatCount(message.count)}
                                        </span>
                                    </li>
                                ))}
                            </ol>
                        </div>
                    ) : null}
                    <div className="flex flex-col gap-1">
                        <Heading
                            className="text-sm font-medium"
                            level={headingLevel + 1}
                        >
                            Latest examples
                        </Heading>
                        <ExamplesTable examples={group.examples} />
                    </div>
                </div>
            </details>
        </article>
    );
}

// RouteErrorList renders error groups by route, each route headed at
// headingLevel with its groups one level below, in an element with the
// route's id so deep links reach it.
export function RouteErrorList({
    headingLevel,
    routes,
}: {
    headingLevel: number;
    routes: AttentionRoute[];
}) {
    return (
        <div className="flex flex-col gap-3">
            {routes.map((route) => (
                <div
                    aria-labelledby={`${route.id}-heading`}
                    className="scroll-mt-6 overflow-hidden rounded-xl border border-border/70 bg-card text-card-foreground"
                    id={route.id}
                    key={route.id}
                    role="group"
                >
                    <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-border/70 bg-muted/40 px-4 py-2">
                        <Heading
                            className="font-mono text-sm font-semibold wrap-anywhere"
                            id={`${route.id}-heading`}
                            level={headingLevel}
                        >
                            {route.label}
                        </Heading>
                        <p className="text-sm text-muted-foreground tabular-nums">
                            {pluralise(route.total, "error")}
                        </p>
                    </div>
                    <div className="divide-y divide-border/70">
                        {route.groups.map((group, index) => (
                            <ErrorGroupEntry
                                group={group}
                                headingLevel={headingLevel + 1}
                                id={`${route.id}-${index}`}
                                key={`${group.status} ${group.error_code} ${group.severity} ${group.probe}`}
                            />
                        ))}
                    </div>
                </div>
            ))}
        </div>
    );
}
