import {
    listFeedbackAction,
    type FeedbackListState,
} from "@/app/(results)/feedback/actions";
import { FeedbackAdminView } from "@/components/feedback-admin-view";
import {
    feedbackCategorySchema,
    type FeedbackListInput,
} from "@/lib/contracts";
import { feedbackReasonMessages } from "@/lib/feedback-messages";

type SearchParams = Record<string, string | string[] | undefined>;

const statusMessages: Record<
    Exclude<FeedbackListState["status"], "ok">,
    string
> = {
    unauthenticated: "Log in to view feedback.",
    forbidden: "You do not have access to feedback.",
    invalid_input: "Invalid feedback request.",
    unavailable: "Feedback is unavailable.",
};

// parseListInput turns untrusted query params into a valid list request,
// falling back to the default for each param it does not recognise.
function parseListInput(searchParams: SearchParams): FeedbackListInput {
    const { category, offset, show } = searchParams;
    const parsedCategory = feedbackCategorySchema.safeParse(category);
    const parsedOffset =
        typeof offset === "string" && /^\d+$/.test(offset) ? Number(offset) : 0;

    return {
        show: show === "all" ? "all" : "unacknowledged",
        category: parsedCategory.success ? parsedCategory.data : null,
        offset: Number.isSafeInteger(parsedOffset) ? parsedOffset : 0,
    };
}

export const dynamic = "force-dynamic";

export default async function FeedbackAdminPage({
    searchParams,
}: {
    searchParams?: Promise<SearchParams>;
}) {
    const input = parseListInput((await searchParams) ?? {});
    const state = await listFeedbackAction(input);

    if (state.status === "ok") {
        return (
            <FeedbackAdminView
                category={input.category}
                offset={input.offset}
                page={state.page}
                show={input.show}
            />
        );
    }

    return (
        <main className="mx-auto flex w-full max-w-[84rem] flex-col gap-5 px-4 py-6 sm:px-8 lg:py-8">
            <h1 className="text-3xl font-semibold tracking-tight">
                Agent feedback
            </h1>
            <section
                className="max-w-2xl space-y-1 rounded-xl border border-border/70 bg-card p-4 text-sm text-card-foreground"
                role="status"
            >
                <p className="font-medium">{statusMessages[state.status]}</p>
                {state.status === "unavailable" ? (
                    <p className="text-muted-foreground">
                        {feedbackReasonMessages[state.reason]}
                    </p>
                ) : null}
            </section>
        </main>
    );
}
