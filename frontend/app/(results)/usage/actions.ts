"use server";

import { z } from "zod";

import { BackendRequestError, mlwhJson } from "@/lib/backend-client";
import {
    usageSummarySchema,
    usageWindowSchema,
    type UsageSummary,
    type UsageWindow,
} from "@/lib/contracts";
import { authorizeMLWHAdmin } from "@/lib/mlwh-admin-gate";
import { mlwhIdentityHeaders } from "@/lib/usage-identity";

export type UsageUnavailableReason =
    | "no_token"
    | "usage_disabled"
    | "token_rejected"
    | "unsupported"
    | "backend_error";

export type UsageSummaryState =
    | { status: "ok"; summary: UsageSummary }
    | { status: "unauthenticated" | "forbidden" | "invalid_input" }
    | { status: "unavailable"; reason: UsageUnavailableReason };

const waErrorCodeSchema = z.object({ code: z.string() });

// usageErrorReason maps a failed wa request to the reason shown to the admin.
function usageErrorReason(error: unknown): UsageUnavailableReason {
    if (!(error instanceof BackendRequestError)) {
        return "backend_error";
    }

    const parsedBody = waErrorCodeSchema.safeParse(error.body);
    const code = parsedBody.success ? parsedBody.data.code : null;

    if (error.status === 503 && code === "usage_disabled") {
        return "usage_disabled";
    }

    if (error.status === 401) {
        return "token_rejected";
    }

    if (error.status === 404) {
        // A wa older than 1.10.0 has no /usage route, so gin answers 404.
        return "unsupported";
    }

    return "backend_error";
}

export async function getUsageSummaryAction(
    window: UsageWindow,
): Promise<UsageSummaryState> {
    const gate = await authorizeMLWHAdmin(() =>
        usageWindowSchema.safeParse(window),
    );

    if (!("token" in gate)) {
        return gate;
    }

    try {
        const query = new URLSearchParams({ window: gate.args });
        const summary = await mlwhJson(
            `/usage?${query.toString()}`,
            usageSummarySchema,
            {
                cache: "no-store",
                headers: {
                    ...(await mlwhIdentityHeaders(gate.username)),
                    authorization: `Bearer ${gate.token}`,
                },
            },
        );

        return { status: "ok", summary };
    } catch (error) {
        return { status: "unavailable", reason: usageErrorReason(error) };
    }
}
