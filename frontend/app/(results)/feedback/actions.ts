"use server";

import { type ZodType, z } from "zod";

import { currentSession } from "@/app/(results)/auth/actions";
import { BackendRequestError, mlwhJson } from "@/lib/backend-client";
import {
    feedbackDeleteResponseSchema,
    feedbackIdSchema,
    feedbackListInputSchema,
    feedbackPageSchema,
    feedbackReportSchema,
    type FeedbackListInput,
    type FeedbackPage,
} from "@/lib/contracts";
import {
    feedbackPageSize,
    isFeedbackAdmin,
    readMLWHServerToken,
} from "@/lib/feedback-admin";

export type FeedbackUnavailableReason =
    | "no_token"
    | "feedback_disabled"
    | "token_rejected"
    | "unsupported"
    | "backend_error";

type FeedbackUnavailableState = {
    status: "unavailable";
    reason: FeedbackUnavailableReason;
};

export type FeedbackListState =
    | { status: "ok"; page: FeedbackPage }
    | { status: "unauthenticated" | "forbidden" | "invalid_input" }
    | FeedbackUnavailableState;

export type FeedbackMutationState =
    | { status: "ok" }
    | {
          status:
              | "unauthenticated"
              | "forbidden"
              | "invalid_input"
              | "not_found";
      }
    | FeedbackUnavailableState;

type FeedbackGateState =
    | { status: "unauthenticated" | "forbidden" | "invalid_input" }
    | FeedbackUnavailableState;

type ParseResult<T> = { success: true; data: T } | { success: false };

const waErrorCodeSchema = z.object({ code: z.string() });
const acknowledgeArgsSchema = z.tuple([feedbackIdSchema, z.boolean()]);
const unsupportedState: FeedbackUnavailableState = {
    status: "unavailable",
    reason: "unsupported",
};

// authorizeFeedback runs the session, allowlist, and argument checks in that
// order, then reads the MLWH server token, all before any fetch. It returns
// the token and parsed arguments, or the state to return without calling wa.
async function authorizeFeedback<T>(
    parseArgs: () => ParseResult<T>,
): Promise<{ token: string; args: T } | FeedbackGateState> {
    const session = await currentSession();

    if (!session.authenticated) {
        return { status: "unauthenticated" };
    }

    if (!isFeedbackAdmin(session.username)) {
        return { status: "forbidden" };
    }

    const parsed = parseArgs();

    if (!parsed.success) {
        return { status: "invalid_input" };
    }

    const token = readMLWHServerToken();

    if (token === null) {
        return { status: "unavailable", reason: "no_token" };
    }

    return { token, args: parsed.data };
}

// feedbackErrorState maps a failed wa request to a returned state. notFound
// is the state for a 404 carrying wa's not_found envelope.
function feedbackErrorState<N>(
    error: unknown,
    notFound: N,
): FeedbackUnavailableState | N {
    if (!(error instanceof BackendRequestError)) {
        return { status: "unavailable", reason: "backend_error" };
    }

    const parsedBody = waErrorCodeSchema.safeParse(error.body);
    const code = parsedBody.success ? parsedBody.data.code : null;

    if (error.status === 503 && code === "feedback_disabled") {
        return { status: "unavailable", reason: "feedback_disabled" };
    }

    if (error.status === 401) {
        return { status: "unavailable", reason: "token_rejected" };
    }

    if (error.status === 404) {
        // A wa older than API 1.9.0 has no feedback routes, so gin answers
        // 404 with plain text.
        return code === "not_found" ? notFound : unsupportedState;
    }

    return { status: "unavailable", reason: "backend_error" };
}

async function feedbackRequest<T>(
    token: string,
    path: string,
    schema: ZodType<T>,
    init: { method?: string; body?: string } = {},
): Promise<T> {
    const headers: Record<string, string> = {
        authorization: `Bearer ${token}`,
    };

    if (init.body !== undefined) {
        headers["content-type"] = "application/json";
    }

    return mlwhJson(path, schema, { ...init, cache: "no-store", headers });
}

function feedbackListPath(input: FeedbackListInput): string {
    const query = new URLSearchParams({
        limit: String(feedbackPageSize),
        offset: String(input.offset),
    });

    if (input.show === "unacknowledged") {
        query.set("acknowledged", "false");
    }

    if (input.category !== null) {
        query.set("category", input.category);
    }

    return `/feedback?${query.toString()}`;
}

export async function listFeedbackAction(
    input: FeedbackListInput,
): Promise<FeedbackListState> {
    const gate = await authorizeFeedback(() =>
        feedbackListInputSchema.safeParse(input),
    );

    if (!("token" in gate)) {
        return gate;
    }

    try {
        const page = await feedbackRequest(
            gate.token,
            feedbackListPath(gate.args),
            feedbackPageSchema,
        );

        return { status: "ok", page };
    } catch (error) {
        // A 1.9.0+ wa never answers 404 on list, so any 404 is an old wa.
        return feedbackErrorState(error, unsupportedState);
    }
}

async function mutateFeedback<T>(
    token: string,
    id: number,
    schema: ZodType<T>,
    init: { method: string; body?: string },
): Promise<FeedbackMutationState> {
    try {
        await feedbackRequest(token, `/feedback/${id}`, schema, init);

        return { status: "ok" };
    } catch (error) {
        return feedbackErrorState(error, { status: "not_found" } as const);
    }
}

export async function setFeedbackAcknowledgedAction(
    id: number,
    acknowledged: boolean,
): Promise<FeedbackMutationState> {
    const gate = await authorizeFeedback(() =>
        acknowledgeArgsSchema.safeParse([id, acknowledged]),
    );

    if (!("token" in gate)) {
        return gate;
    }

    const [parsedId, parsedAcknowledged] = gate.args;

    return mutateFeedback(gate.token, parsedId, feedbackReportSchema, {
        method: "PATCH",
        body: JSON.stringify({ acknowledged: parsedAcknowledged }),
    });
}

export async function deleteFeedbackAction(
    id: number,
): Promise<FeedbackMutationState> {
    const gate = await authorizeFeedback(() => feedbackIdSchema.safeParse(id));

    if (!("token" in gate)) {
        return gate;
    }

    return mutateFeedback(gate.token, gate.args, feedbackDeleteResponseSchema, {
        method: "DELETE",
    });
}
