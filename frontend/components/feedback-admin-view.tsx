"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { startTransition, useId, useState } from "react";
import { toast } from "sonner";

import {
    deleteFeedbackAction,
    setFeedbackAcknowledgedAction,
    type FeedbackMutationState,
} from "@/app/(results)/feedback/actions";
import { LocalTimestamp } from "@/components/local-timestamp";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
    feedbackCategorySchema,
    feedbackPageSize,
    type FeedbackCategory,
    type FeedbackListInput,
    type FeedbackPage,
    type FeedbackReport,
} from "@/lib/contracts";

type FeedbackShow = FeedbackListInput["show"];

export type FeedbackAdminViewProps = {
    category: FeedbackCategory | null;
    offset: number;
    page: FeedbackPage;
    show: FeedbackShow;
};

const categoryLabels: Record<FeedbackCategory, string> = {
    could_not_answer: "Could not answer",
    agent_mistake: "Agent mistake",
    no_endpoint: "No endpoint",
    user_unhappy: "User unhappy",
    other: "Other",
};

const mutationFailureMessages: Record<
    Exclude<FeedbackMutationState["status"], "ok">,
    string
> = {
    unauthenticated: "you are not logged in",
    forbidden: "you do not have access to feedback",
    invalid_input: "the request was invalid",
    not_found: "it no longer exists",
    unavailable: "feedback is unavailable",
};

// feedbackHref builds a /feedback link with params in the order show,
// category, offset, omitting each one that holds its default.
function feedbackHref(
    show: FeedbackShow,
    category: FeedbackCategory | null,
    offset = 0,
): string {
    const params = new URLSearchParams();

    if (show === "all") {
        params.set("show", "all");
    }

    if (category !== null) {
        params.set("category", category);
    }

    if (offset > 0) {
        params.set("offset", String(offset));
    }

    const query = params.toString();

    return query ? `/feedback?${query}` : "/feedback";
}

function clientDescription(report: FeedbackReport): string {
    const client = [report.client_name, report.client_version]
        .filter(Boolean)
        .join(" ");

    return client || report.client_user_agent;
}

function metadataParts(report: FeedbackReport): string[] {
    const parts: Array<[string, string]> = [
        ["Client", clientDescription(report)],
        ["MCP server", report.mcp_server_version],
        ["wa API", report.wa_api_version],
        ["Transport", report.transport],
        ["From", report.remote_addr],
    ];

    return parts
        .filter(([, value]) => value !== "")
        .map(([label, value]) => `${label} ${value}`);
}

function FeedbackReportArticle({
    busy,
    onAcknowledge,
    onDelete,
    report,
}: {
    busy: boolean;
    onAcknowledge: (report: FeedbackReport) => void;
    onDelete: (report: FeedbackReport) => void;
    report: FeedbackReport;
}) {
    const metadata = metadataParts(report);

    return (
        <article
            aria-label={`Feedback #${report.id}`}
            className="space-y-3 rounded-xl border border-border/70 bg-card p-4 text-card-foreground shadow-sm"
        >
            <div className="flex flex-wrap items-center gap-2 text-sm">
                <Badge>{categoryLabels[report.category]}</Badge>
                <h2 className="font-mono font-medium">#{report.id}</h2>
                <LocalTimestamp
                    className="text-muted-foreground"
                    format="dateTime"
                    value={report.created_at}
                />
                {report.acknowledged ? (
                    <Badge variant="outline">Acknowledged</Badge>
                ) : null}
            </div>

            <p className="text-sm leading-6 break-words whitespace-pre-wrap">
                {report.description}
            </p>

            {report.user_request !== "" || report.tools_tried.length > 0 ? (
                <dl className="grid gap-2 text-sm">
                    {report.user_request !== "" ? (
                        <div>
                            <dt className="font-medium text-muted-foreground">
                                User request
                            </dt>
                            <dd className="break-words whitespace-pre-wrap">
                                {report.user_request}
                            </dd>
                        </div>
                    ) : null}
                    {report.tools_tried.length > 0 ? (
                        <div>
                            <dt className="font-medium text-muted-foreground">
                                Tools tried
                            </dt>
                            <dd className="font-mono break-words">
                                {report.tools_tried.join(", ")}
                            </dd>
                        </div>
                    ) : null}
                </dl>
            ) : null}

            {metadata.length > 0 ? (
                <p className="text-xs break-words text-muted-foreground">
                    {metadata.join(" · ")}
                </p>
            ) : null}

            <div className="flex flex-wrap gap-2">
                <Button
                    disabled={busy}
                    onClick={() => onAcknowledge(report)}
                    size="sm"
                    type="button"
                    variant="outline"
                >
                    {report.acknowledged ? "Unacknowledge" : "Acknowledge"}
                </Button>
                <Button
                    disabled={busy}
                    onClick={() => onDelete(report)}
                    size="sm"
                    type="button"
                    variant="destructive"
                >
                    Delete
                </Button>
            </div>
        </article>
    );
}

export function FeedbackAdminView({
    category,
    offset,
    page,
    show,
}: FeedbackAdminViewProps) {
    const router = useRouter();
    const [busyIds, setBusyIds] = useState<ReadonlySet<number>>(
        () => new Set(),
    );
    const categorySelectId = useId();
    const showAcknowledgedId = useId();
    const previousOffset = Math.max(0, offset - feedbackPageSize);

    async function runMutation(
        id: number,
        verb: string,
        mutate: () => Promise<FeedbackMutationState>,
    ) {
        setBusyIds((ids) => new Set(ids).add(id));

        try {
            const state = await mutate();

            if (state.status !== "ok") {
                toast.error(
                    `Could not ${verb} feedback #${id}: ${mutationFailureMessages[state.status]}.`,
                );
            }
        } catch {
            toast.error(`Could not ${verb} feedback #${id}.`);
        }

        // Clear busy in the refresh's transition, so the buttons re-enable
        // only once the refreshed report has rendered.
        startTransition(() => {
            router.refresh();
            setBusyIds((ids) => {
                const next = new Set(ids);

                next.delete(id);

                return next;
            });
        });
    }

    function handleAcknowledge(report: FeedbackReport) {
        const acknowledged = !report.acknowledged;

        void runMutation(
            report.id,
            acknowledged ? "acknowledge" : "unacknowledge",
            () => setFeedbackAcknowledgedAction(report.id, acknowledged),
        );
    }

    function handleDelete(report: FeedbackReport) {
        if (
            !window.confirm(
                `Delete feedback #${report.id}? This cannot be undone.`,
            )
        ) {
            return;
        }

        void runMutation(report.id, "delete", () =>
            deleteFeedbackAction(report.id),
        );
    }

    function handleCategoryChange(value: string) {
        const parsed = feedbackCategorySchema.safeParse(value);

        router.push(feedbackHref(show, parsed.success ? parsed.data : null));
    }

    return (
        <main className="mx-auto flex min-h-screen w-full max-w-[84rem] flex-col gap-5 px-4 py-6 sm:px-8 lg:py-8">
            <header className="flex flex-col gap-1 sm:flex-row sm:items-baseline sm:justify-between">
                <h1 className="text-3xl font-semibold tracking-tight">
                    Agent feedback
                </h1>
                <p className="text-sm text-muted-foreground">
                    {page.total} {page.total === 1 ? "report" : "reports"}
                </p>
            </header>

            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:gap-6">
                <div className="flex items-center gap-2 text-sm">
                    <input
                        checked={show === "all"}
                        className="size-4 accent-primary"
                        id={showAcknowledgedId}
                        onChange={(event) =>
                            router.push(
                                feedbackHref(
                                    event.target.checked
                                        ? "all"
                                        : "unacknowledged",
                                    category,
                                ),
                            )
                        }
                        type="checkbox"
                    />
                    <label htmlFor={showAcknowledgedId}>
                        Show acknowledged
                    </label>
                </div>
                <div className="flex items-center gap-2 text-sm">
                    <label htmlFor={categorySelectId}>Category</label>
                    <select
                        className="h-9 rounded-md border border-border bg-background px-2 text-foreground"
                        id={categorySelectId}
                        onChange={(event) =>
                            handleCategoryChange(event.target.value)
                        }
                        value={category ?? ""}
                    >
                        <option value="">All categories</option>
                        {feedbackCategorySchema.options.map((value) => (
                            <option key={value} value={value}>
                                {categoryLabels[value]}
                            </option>
                        ))}
                    </select>
                </div>
            </div>

            {page.items.length === 0 ? (
                <p className="rounded-xl border border-dashed border-border/70 p-6 text-center text-sm text-muted-foreground">
                    No feedback to show.
                </p>
            ) : (
                <div className="flex flex-col gap-3">
                    {page.items.map((report) => (
                        <FeedbackReportArticle
                            busy={busyIds.has(report.id)}
                            key={report.id}
                            onAcknowledge={handleAcknowledge}
                            onDelete={handleDelete}
                            report={report}
                        />
                    ))}
                </div>
            )}

            {offset > 0 || page.next_offset !== -1 ? (
                <nav aria-label="Feedback pages" className="flex gap-2">
                    {offset > 0 ? (
                        <Button asChild size="sm" variant="outline">
                            <Link
                                href={feedbackHref(
                                    show,
                                    category,
                                    previousOffset,
                                )}
                            >
                                Previous
                            </Link>
                        </Button>
                    ) : null}
                    {page.next_offset !== -1 ? (
                        <Button
                            asChild
                            className="ml-auto"
                            size="sm"
                            variant="outline"
                        >
                            <Link
                                href={feedbackHref(
                                    show,
                                    category,
                                    page.next_offset,
                                )}
                            >
                                Next
                            </Link>
                        </Button>
                    ) : null}
                </nav>
            ) : null}
        </main>
    );
}
