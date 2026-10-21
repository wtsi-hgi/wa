"use server";

import { z } from "zod";

import { BackendRequestError, mlwhJson } from "@/lib/backend-client";
import {
    usageFilterInputSchema,
    usageSummarySchema,
    usageWindowSchema,
    type UsageFilter,
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

const usageSummaryInputSchema = z.object({
    window: usageWindowSchema,
    filter: usageFilterInputSchema,
});

export async function getUsageSummaryAction(
    window: UsageWindow,
    filter: UsageFilter = {},
): Promise<UsageSummaryState> {
    const gate = await authorizeMLWHAdmin(() =>
        usageSummaryInputSchema.safeParse({ window, filter }),
    );

    if (!("token" in gate)) {
        return gate;
    }

    try {
        const query = new URLSearchParams({ window: gate.args.window });

        for (const [key, value] of Object.entries(gate.args.filter)) {
            if (value) {
                query.set(key, value);
            }
        }

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
