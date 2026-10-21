import {
    getMLWHSyncStatusAction,
    type MLWHSyncStatusState,
} from "@/app/(results)/mlwh-sync/actions";
import { MLWHSyncAdminView } from "@/components/mlwh-sync-admin-view";

const statusMessages: Record<
    Exclude<MLWHSyncStatusState["status"], "ok">,
    { title: string; detail?: string }
> = {
    unauthenticated: { title: "Log in to view MLWH sync status." },
    forbidden: { title: "You do not have access to MLWH sync status." },
    unsupported: {
        title: "Status unavailable: server too old.",
        detail: "wa mlwh serve reports sync status from MLWH API 1.12.0.",
    },
    unavailable: { title: "The MLWH server could not be reached." },
};

export const dynamic = "force-dynamic";

export default async function MLWHSyncAdminPage() {
    const state = await getMLWHSyncStatusAction();

    if (state.status === "ok") {
        return <MLWHSyncAdminView freshness={state.freshness} />;
    }

    const message = statusMessages[state.status];

    return (
        <main className="mx-auto flex w-full max-w-[84rem] flex-col gap-5 px-4 py-6 sm:px-8 lg:py-8">
            <h1 className="text-3xl font-semibold tracking-tight">MLWH sync</h1>
            <section
                className="max-w-2xl space-y-1 rounded-xl border border-border/70 bg-card p-4 text-sm text-card-foreground"
                role="status"
            >
                <p className="font-medium">{message.title}</p>
                {message.detail ? (
                    <p className="text-muted-foreground">{message.detail}</p>
                ) : null}
            </section>
        </main>
    );
}
