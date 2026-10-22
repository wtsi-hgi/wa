// Pure derivations for the admin /usage dashboard: severity fallback,
// dropping errors recorded before error details, error merging, period
// changes, grouping of agents and endpoints, time buckets, and filter links.
// Every function reads a parsed GET /usage summary and tolerates the fields
// older servers omit.

import {
    usageCategorySchema,
    type UsageCategory,
    type UsageErrorBreakdown,
    type UsageFilter,
    type UsageSummary,
    type UsageWindow,
} from "@/lib/contracts";

export type CountMap = Record<string, number>;
export type UsageErrorGroup = NonNullable<UsageSummary["errors"]>[number];
export type UsageEndpoint = UsageSummary["endpoints"][number];
export type UsageValueCount = UsageSummary["mcp_agents"][number];
export type UsagePeriod = UsageSummary["series"][number];

export type Severity = "fault" | "caller" | "expected";

const severities: Severity[] = ["fault", "caller", "expected"];

// ownClientCategories are wa's own clients, whose rejected requests are wa
// bugs (mlwh/usage_error_summary.go usageOwnClientCategories).
const ownClientCategories: UsageCategory[] = [
    "cli",
    "web",
    "results_server",
    "mlwhdiff_server",
];

// fallbackSeverity judges a group from a server that predates severities with
// the server's rule (usageErrorSeveritySQL): 5xx is a fault; not_found,
// ambiguous, and unsupported_identifier, or a code-less 404, 409, or 422 on a
// matched route, are expected; other
// 4xx are faults when any caller is one of wa's own clients, else caller
// errors. The server judges each row, but an old server's group can mix
// callers, so a 4xx group with any of wa's own clients counts wholly as a
// fault here.
export function fallbackSeverity(
    group: Pick<
        UsageErrorGroup,
        "by_category" | "error_code" | "route" | "status"
    >,
): Severity {
    if (group.status >= 500) {
        return "fault";
    }

    const code = group.error_code;

    if (
        code === "not_found" ||
        code === "ambiguous" ||
        code === "unsupported_identifier" ||
        (code === null &&
            group.route !== "" &&
            (group.status === 404 ||
                group.status === 409 ||
                group.status === 422))
    ) {
        return "expected";
    }

    const own = ownClientCategories.some(
        (category) => (group.by_category[category] ?? 0) > 0,
    );

    return own ? "fault" : "caller";
}

// groupSeverity is the server's severity for group, or the fallback when it
// sent none or one this page does not know.
export function groupSeverity(group: UsageErrorGroup): Severity {
    return severities.includes(group.severity as Severity)
        ? (group.severity as Severity)
        : fallbackSeverity(group);
}

// slug makes text safe for an element id. Route parameters keep a marker,
// so /a/:id and /a/id differ.
export function slug(text: string): string {
    return text
        .replace(/:/g, "_")
        .replace(/[^A-Za-z0-9_]+/g, "-")
        .replace(/^-+|-+$/g, "");
}

// endpointLabel names an error group's endpoint, or marks it unmatched.
export function endpointLabel(method: string, route: string): string {
    return route ? `${method} ${route}` : `${method} (no matching route)`;
}

export type AttentionRoute = {
    groups: UsageErrorGroup[];
    id: string;
    label: string;
    total: number;
};

// AttentionRow is the fault or caller errors of one status and code, merged
// across routes.
export type AttentionRow = {
    agents: CountMap;
    categories: CountMap;
    code: string | null;
    count: number;
    id: string;
    kinds: CountMap;
    lastSeen: string;
    routes: AttentionRoute[];
    severity: "fault" | "caller";
    status: number;
    tools: CountMap;
};

function addCounts(into: CountMap, from: CountMap) {
    for (const [key, value] of Object.entries(from)) {
        into[key] = (into[key] ?? 0) + value;
    }
}

function compareText(a: string, b: string): number {
    return a < b ? -1 : a > b ? 1 : 0;
}

// attentionRowId is the element id of the attention row of one severity,
// status, and code; err-<id> deep links open it.
function attentionRowId(
    severity: Severity,
    status: number,
    code: string | null,
): string {
    return `err-${severity}-${status}-${slug(code ?? "legacy")}`;
}

// routeGroups groups errors by endpoint label, keeping the server's order
// within each, ordered by count then label, each with an id under prefix.
export function routeGroups(
    errors: UsageErrorGroup[],
    prefix: string,
): AttentionRoute[] {
    const byLabel = new Map<string, AttentionRoute>();

    for (const group of errors) {
        const label = endpointLabel(group.method, group.route);
        const route = byLabel.get(label) ?? {
            groups: [],
            id: `${prefix}-${slug(label)}`,
            label,
            total: 0,
        };

        route.groups.push(group);
        route.total += group.count;
        byLabel.set(label, route);
    }

    return [...byLabel.values()].sort(
        (a, b) => b.total - a.total || compareText(a.label, b.label),
    );
}

// attentionRows merges the fault and caller groups by severity, status, and
// code, faults first, then by count descending, then status and code.
export function attentionRows(errors: UsageErrorGroup[]): AttentionRow[] {
    const rows = new Map<string, AttentionRow & { all: UsageErrorGroup[] }>();

    for (const group of errors) {
        const severity = groupSeverity(group);

        if (severity === "expected") {
            continue;
        }

        const id = attentionRowId(severity, group.status, group.error_code);
        const row = rows.get(id) ?? {
            agents: {},
            all: [],
            categories: {},
            code: group.error_code,
            count: 0,
            id,
            kinds: {},
            lastSeen: group.last_seen,
            routes: [],
            severity,
            status: group.status,
            tools: {},
        };

        row.all.push(group);
        row.count += group.count;
        addCounts(row.categories, group.by_category);
        addCounts(row.tools, group.by_tool);
        addCounts(row.agents, group.by_agent);
        addCounts(row.kinds, group.error_kinds);

        if (group.last_seen > row.lastSeen) {
            row.lastSeen = group.last_seen;
        }

        rows.set(id, row);
    }

    return [...rows.values()]
        .map(({ all, ...row }) => ({
            ...row,
            routes: routeGroups(all, row.id),
        }))
        .sort(
            (a, b) =>
                severities.indexOf(a.severity) -
                    severities.indexOf(b.severity) ||
                b.count - a.count ||
                a.status - b.status ||
                compareText(a.code ?? "", b.code ?? ""),
        );
}

// expectedGroups lists the expected groups.
export function expectedGroups(errors: UsageErrorGroup[]): UsageErrorGroup[] {
    return errors.filter((group) => groupSeverity(group) === "expected");
}

export const expectedPrefix = "err-expected";

// errorAnchors maps each endpoint label with errors to the id of its first
// listing: attention rows, then expected outcomes.
export function errorAnchors(errors: UsageErrorGroup[]): Map<string, string> {
    const anchors = new Map<string, string>();
    const add = (routes: AttentionRoute[]) => {
        for (const route of routes) {
            if (!anchors.has(route.label)) {
                anchors.set(route.label, route.id);
            }
        }
    };

    for (const row of attentionRows(errors)) {
        add(row.routes);
    }

    add(routeGroups(expectedGroups(errors), expectedPrefix));

    return anchors;
}

function legacyCount(breakdown: UsageErrorBreakdown): number {
    return (
        breakdown.legacy.fault +
        breakdown.legacy.caller +
        breakdown.legacy.expected
    );
}

// detailedBreakdown is breakdown without the errors recorded before error
// details, which the server counts in both its parts and legacy.
function detailedBreakdown(
    breakdown: UsageErrorBreakdown,
): UsageErrorBreakdown {
    const { legacy } = breakdown;

    return {
        fault: Math.max(0, breakdown.fault - legacy.fault),
        caller: Math.max(0, breakdown.caller - legacy.caller),
        expected: Math.max(0, breakdown.expected - legacy.expected),
        probe: breakdown.probe,
        legacy: { fault: 0, caller: 0, expected: 0 },
    };
}

type ErrorCounts = {
    error_rate?: number;
    error_severity?: UsageErrorBreakdown;
    errors?: number;
    requests: number;
};

// withoutLegacyCounts drops entry's legacy errors from its breakdown and,
// when it has them, its errors and error rate.
function withoutLegacyCounts<T extends ErrorCounts>(entry: T): T {
    const breakdown = entry.error_severity;

    if (!breakdown) {
        return entry;
    }

    const result: T = {
        ...entry,
        error_severity: detailedBreakdown(breakdown),
    };

    if (entry.errors !== undefined) {
        result.errors = Math.max(0, entry.errors - legacyCount(breakdown));
    }

    if (entry.error_rate !== undefined) {
        result.error_rate = rate(result.errors ?? 0, entry.requests);
    }

    return result;
}

// withoutLegacyErrors drops the errors recorded before error details (code
// null, counted in each breakdown's legacy), which nobody can act on: their
// groups go, and every breakdown, errors count, and error rate leaves them
// out. legacy is how many errors were dropped. A server without
// totals.error_severity cannot say how many there are, so its summary is
// returned unchanged.
export function withoutLegacyErrors(summary: UsageSummary): {
    legacy: number;
    summary: UsageSummary;
} {
    const totals = summary.totals.error_severity;

    if (!totals) {
        return { legacy: 0, summary };
    }

    return {
        legacy: legacyCount(totals),
        summary: {
            ...summary,
            totals: withoutLegacyCounts(summary.totals),
            previous_totals: summary.previous_totals
                ? withoutLegacyCounts(summary.previous_totals)
                : summary.previous_totals,
            categories: summary.categories.map(withoutLegacyCounts),
            endpoints: summary.endpoints.map(withoutLegacyCounts),
            mcp_tools: summary.mcp_tools.map(withoutLegacyCounts),
            mcp_agents: summary.mcp_agents.map(withoutLegacyCounts),
            other_user_agents:
                summary.other_user_agents.map(withoutLegacyCounts),
            days: summary.days.map(withoutLegacyCounts),
            buckets: summary.buckets?.map(withoutLegacyCounts),
            errors: summary.errors?.filter(
                (group) => group.error_code !== null,
            ),
        },
    };
}

// worstSeverity is the most urgent severity with errors in breakdown, or null.
export function worstSeverity(
    breakdown: UsageErrorBreakdown | undefined,
): Severity | null {
    if (!breakdown) {
        return null;
    }

    if (breakdown.fault > 0) {
        return "fault";
    }

    if (breakdown.caller > 0) {
        return "caller";
    }

    return breakdown.expected + breakdown.probe > 0 ? "expected" : null;
}

export function rate(part: number, whole: number): number {
    return whole > 0 ? part / whole : 0;
}

export type ChangeKind = "new" | "quiet" | "up" | "down";

export type Change = {
    dimension: "category" | "endpoint" | "tool" | "user";
    key: string;
    kind: ChangeKind;
    latest: number;
    previous: number;
};

export type PeriodChanges =
    | { status: "ok"; changes: Change[] }
    | { status: "short"; earliest: string };

// changeThreshold is the relative change that counts as up or down, and
// changeFloor the count one of the two periods must reach, so 1 -> 2 is not
// news.
const changeThreshold = 0.25;
const changeFloor = 5;

function classifyChange(latest: number, previous: number): ChangeKind | null {
    if (previous === 0) {
        return latest > 0 ? "new" : null;
    }

    if (latest === 0) {
        return "quiet";
    }

    if (Math.max(latest, previous) < changeFloor) {
        return null;
    }

    const change = (latest - previous) / previous;

    if (change > changeThreshold) {
        return "up";
    }

    return change < -changeThreshold ? "down" : null;
}

const changeOrder: ChangeKind[] = ["new", "quiet", "up", "down"];

// periodChanges compares the latest series period with the one before it per
// category, endpoint, MCP tool, and user. It reports "short" when there is no
// previous period or records start after the previous period does, since
// the comparison would then read missing history as a change.
export function periodChanges(
    series: UsagePeriod[],
    earliestRecord: string,
): PeriodChanges {
    if (series.length < 2 || earliestRecord > series.at(-2)!.start) {
        return { status: "short", earliest: earliestRecord };
    }

    const latest = series.at(-1)!;
    const previous = series.at(-2)!;
    const dimensions: Array<[Change["dimension"], CountMap, CountMap]> = [
        ["category", latest.by_category, previous.by_category],
        ["endpoint", latest.by_endpoint, previous.by_endpoint],
        ["tool", latest.by_tool ?? {}, previous.by_tool ?? {}],
        ["user", latest.by_user ?? {}, previous.by_user ?? {}],
    ];
    const changes: Change[] = [];

    for (const [dimension, now, before] of dimensions) {
        const keys = new Set([...Object.keys(now), ...Object.keys(before)]);

        for (const key of keys) {
            const kind = classifyChange(now[key] ?? 0, before[key] ?? 0);

            if (kind) {
                changes.push({
                    dimension,
                    key,
                    kind,
                    latest: now[key] ?? 0,
                    previous: before[key] ?? 0,
                });
            }
        }
    }

    const size = (change: Change) => Math.abs(change.latest - change.previous);

    changes.sort(
        (a, b) =>
            changeOrder.indexOf(a.kind) - changeOrder.indexOf(b.kind) ||
            size(b) - size(a) ||
            compareText(a.key, b.key),
    );

    return { status: "ok", changes };
}

export type AgentProduct = {
    errors: number;
    product: string;
    requests: number;
    severity: UsageErrorBreakdown | undefined;
    versions: UsageValueCount[];
};

// agentProduct is the text of an agent before its first "/", as the server's
// agent filter matches.
export function agentProduct(agent: string): string {
    const slash = agent.indexOf("/");

    return slash < 0 ? agent : agent.slice(0, slash);
}

function addBreakdown(
    into: UsageErrorBreakdown | undefined,
    from: UsageErrorBreakdown | undefined,
): UsageErrorBreakdown | undefined {
    if (!into || !from) {
        return into ?? from;
    }

    return {
        fault: into.fault + from.fault,
        caller: into.caller + from.caller,
        expected: into.expected + from.expected,
        probe: into.probe + from.probe,
        legacy: {
            fault: into.legacy.fault + from.legacy.fault,
            caller: into.legacy.caller + from.legacy.caller,
            expected: into.legacy.expected + from.legacy.expected,
        },
    };
}

// agentProducts merges agents by product, by requests then product.
export function agentProducts(agents: UsageValueCount[]): AgentProduct[] {
    const products = new Map<string, AgentProduct>();

    for (const agent of agents) {
        const product = agentProduct(agent.value);
        const entry = products.get(product) ?? {
            errors: 0,
            product,
            requests: 0,
            severity: undefined,
            versions: [],
        };

        entry.requests += agent.requests;
        entry.errors += agent.errors ?? 0;
        entry.severity =
            entry.versions.length === 0
                ? agent.error_severity
                : addBreakdown(entry.severity, agent.error_severity);
        entry.versions.push(agent);
        products.set(product, entry);
    }

    return [...products.values()].sort(
        (a, b) => b.requests - a.requests || compareText(a.product, b.product),
    );
}

export type EndpointGroup = {
    byCategory: CountMap;
    prefix: string;
    requests: number;
    routes: UsageEndpoint[];
    severity: UsageErrorBreakdown | undefined;
};

// routePrefix is a route's first path segment, such as /resolve for
// /resolve/study/:id.
export function routePrefix(route: string): string {
    const end = route.indexOf("/", 1);

    return end < 0 ? route : route.slice(0, end);
}

// endpointGroups groups endpoints by first path segment, by requests then
// prefix; routes keep the server's order.
export function endpointGroups(endpoints: UsageEndpoint[]): EndpointGroup[] {
    const groups = new Map<string, EndpointGroup>();

    for (const endpoint of endpoints) {
        const prefix = routePrefix(endpoint.route);
        const group = groups.get(prefix) ?? {
            byCategory: {},
            prefix,
            requests: 0,
            routes: [],
            severity: undefined,
        };

        group.requests += endpoint.requests;
        addCounts(group.byCategory, endpoint.by_category);
        group.severity =
            group.routes.length === 0
                ? endpoint.error_severity
                : addBreakdown(group.severity, endpoint.error_severity);
        group.routes.push(endpoint);
        groups.set(prefix, group);
    }

    return [...groups.values()].sort(
        (a, b) => b.requests - a.requests || compareText(a.prefix, b.prefix),
    );
}

export type BucketUnit = "hour" | "day" | "week";

export type TimeBucket = {
    byCategory: CountMap;
    requests: number;
    severity: UsageErrorBreakdown | undefined;
    start: string; // RFC3339 UTC
};

const dayMS = 24 * 60 * 60 * 1000;
const weekMS = 7 * dayMS;
// weekOffsetMS aligns weeks to Mondays, as the server does: the Unix epoch
// was a Thursday.
const weekOffsetMS = 4 * dayMS;
const weeklyAfterMS = 120 * dayMS;

// weekStart returns the Monday 00:00 UTC on or before ms.
export function weekStart(ms: number): number {
    return Math.floor((ms - weekOffsetMS) / weekMS) * weekMS + weekOffsetMS;
}

function emptyBreakdown(): UsageErrorBreakdown {
    return {
        fault: 0,
        caller: 0,
        expected: 0,
        probe: 0,
        legacy: { fault: 0, caller: 0, expected: 0 },
    };
}

// weekBuckets sums day buckets into Monday-aligned weeks.
export function weekBuckets(days: TimeBucket[]): TimeBucket[] {
    const weeks = new Map<number, TimeBucket>();

    for (const day of days) {
        const start = weekStart(Date.parse(day.start));
        const week = weeks.get(start) ?? {
            byCategory: {},
            requests: 0,
            severity: day.severity ? emptyBreakdown() : undefined,
            start: new Date(start).toISOString().replace(".000Z", "Z"),
        };

        week.requests += day.requests;
        addCounts(week.byCategory, day.byCategory);
        week.severity = day.severity
            ? addBreakdown(week.severity ?? emptyBreakdown(), day.severity)
            : week.severity;
        weeks.set(start, week);
    }

    return [...weeks.values()];
}

// timeBuckets returns the summary's buckets and their unit. Servers that
// predate buckets give days, summed into weeks for an all window longer than
// 120 days, as newer servers do.
export function timeBuckets(summary: UsageSummary): {
    buckets: TimeBucket[];
    unit: BucketUnit;
} {
    if (summary.buckets && summary.bucket_unit) {
        return {
            unit: summary.bucket_unit,
            buckets: summary.buckets.map((bucket) => ({
                byCategory: bucket.by_category,
                requests: bucket.requests,
                severity: bucket.error_severity,
                start: bucket.start,
            })),
        };
    }

    const days = summary.days.map((day) => ({
        byCategory: day.by_category,
        requests: day.requests,
        severity: day.error_severity,
        start: `${day.date}T00:00:00Z`,
    }));
    const first = days[0];

    if (
        summary.window === "all" &&
        first &&
        Date.parse(summary.generated_at) - Date.parse(first.start) >
            weeklyAfterMS
    ) {
        return { unit: "week", buckets: weekBuckets(days) };
    }

    return { unit: "day", buckets: days };
}

const unitMS: Record<BucketUnit, number> = {
    hour: 60 * 60 * 1000,
    day: dayMS,
    week: weekMS,
};

// clipBuckets drops the buckets that end before earliestRecord, so a window
// longer than the history does not look like an outage. clipped says whether
// any were dropped.
export function clipBuckets(
    buckets: TimeBucket[],
    unit: BucketUnit,
    earliestRecord: string,
): { buckets: TimeBucket[]; clipped: boolean } {
    if (!earliestRecord) {
        return { buckets, clipped: false };
    }

    const earliest = Date.parse(earliestRecord);
    const kept = buckets.filter(
        (bucket) => Date.parse(bucket.start) + unitMS[unit] > earliest,
    );

    return { buckets: kept, clipped: kept.length < buckets.length };
}

// activeCategories lists the categories with requests, in category order,
// and those without.
export function activeCategories(summary: UsageSummary): {
    active: UsageCategory[];
    idle: UsageCategory[];
} {
    const requests = new Map(
        summary.categories.map((row) => [row.category, row.requests]),
    );
    const active = usageCategorySchema.options.filter(
        (category) => (requests.get(category) ?? 0) > 0,
    );

    return {
        active,
        idle: usageCategorySchema.options.filter(
            (category) => !active.includes(category),
        ),
    };
}

// previousComparable reports whether previous_totals cover a full window of
// records: the records must start no later than the previous window does.
export function previousComparable(summary: UsageSummary): boolean {
    if (!summary.previous_totals || !summary.window_start) {
        return false;
    }

    const start = Date.parse(summary.window_start);
    const length = Date.parse(summary.generated_at) - start;

    return (
        summary.earliest_record !== "" &&
        Date.parse(summary.earliest_record) <= start - length
    );
}

export const filterKeys = [
    "category",
    "user",
    "tool",
    "agent",
    "method",
    "route",
] as const;

export type FilterKey = (typeof filterKeys)[number];

// usageHref is the /usage link for window and filter. 7d is the default and
// is left out.
export function usageHref(window: UsageWindow, filter: UsageFilter): string {
    const params = new URLSearchParams();

    if (window !== "7d") {
        params.set("window", window);
    }

    for (const key of filterKeys) {
        const value = filter[key];

        if (value) {
            params.set(key, value);
        }
    }

    const query = params.toString();

    return query ? `/usage?${query}` : "/usage";
}

// withFilter returns filter with change applied; an undefined value removes
// its key. Removing method or route removes both.
export function withFilter(
    filter: UsageFilter,
    change: Partial<Record<FilterKey, string | undefined>>,
): UsageFilter {
    const next: Record<string, string | undefined> = { ...filter, ...change };

    if (
        ("method" in change && !change.method) ||
        ("route" in change && !change.route)
    ) {
        delete next.method;
        delete next.route;
    }

    for (const key of filterKeys) {
        if (!next[key]) {
            delete next[key];
        }
    }

    return next as UsageFilter;
}

export function isFiltered(filter: UsageFilter): boolean {
    return filterKeys.some((key) => Boolean(filter[key]));
}
