import {
    getUsageSummaryAction,
    type UsageSummaryState,
} from "@/app/(results)/usage/actions";
import { UsageAdminView } from "@/components/usage-admin-view";
import { usageWindowSchema, type UsageWindow } from "@/lib/contracts";
import { usageReasonMessages } from "@/lib/usage-messages";

type SearchParams = Record<string, string | string[] | undefined>;

const statusMessages: Record<
    Exclude<UsageSummaryState["status"], "ok">,
    string
> = {
    unauthenticated: "Log in to view usage.",
    forbidden: "You do not have access to usage.",
    invalid_input: "Invalid usage request.",
    unavailable: "Usage is unavailable.",
};

// parseWindow turns the untrusted window query param into a valid window,
// falling back to 7d for anything it does not recognise.
function parseWindow(searchParams: SearchParams): UsageWindow {
    const parsed = usageWindowSchema.safeParse(searchParams.window);

    return parsed.success ? parsed.data : "7d";
}

export const dynamic = "force-dynamic";

export default async function UsageAdminPage({
    searchParams,
}: {
    searchParams?: Promise<SearchParams>;
}) {
    const window = parseWindow((await searchParams) ?? {});
    const state = await getUsageSummaryAction(window);

    if (state.status === "ok") {
        return <UsageAdminView summary={state.summary} window={window} />;
    }

    return (
        <main className="mx-auto flex w-full max-w-[84rem] flex-col gap-5 px-4 py-6 sm:px-8 lg:py-8">
            <h1 className="text-3xl font-semibold tracking-tight">
                MLWH usage
            </h1>
            <section
                className="max-w-2xl space-y-1 rounded-xl border border-border/70 bg-card p-4 text-sm text-card-foreground"
                role="status"
            >
                <p className="font-medium">{statusMessages[state.status]}</p>
                {state.status === "unavailable" ? (
                    <p className="text-muted-foreground">
                        {usageReasonMessages[state.reason]}
                    </p>
                ) : null}
            </section>
        </main>
    );
}
