"use server";

import { ZodError } from "zod";

import { BackendRequestError, mlwhJson } from "@/lib/backend-client";
import {
    freshnessSchema,
    mlwhSyncStatusSchema,
    type MLWHSyncStatus,
} from "@/lib/contracts";
import { authorizeMLWHAdminSession } from "@/lib/mlwh-admin-gate";
import { mlwhIdentityHeaders } from "@/lib/usage-identity";

export type MLWHSyncStatusState =
    | { status: "ok"; freshness: MLWHSyncStatus }
    | {
          status:
              | "unauthenticated"
              | "forbidden"
              | "unsupported"
              | "invalid_response"
              | "unavailable";
      };

// getMLWHSyncStatusAction reads wa's GET /freshness for an MLWH admin. The
// route needs no admin token, so unlike /usage this works without the token
// file. A server older than MLWH API 1.12.0 sends no status, so it is
// reported as unsupported. A response the page cannot parse is reported as
// invalid_response, so it is not mistaken for an unreachable server.
export async function getMLWHSyncStatusAction(): Promise<MLWHSyncStatusState> {
    const gate = await authorizeMLWHAdminSession();

    if ("status" in gate) {
        return gate;
    }

    try {
        const freshness = await mlwhJson("/freshness", freshnessSchema, {
            cache: "no-store",
            headers: await mlwhIdentityHeaders(gate.username),
        });

        if (freshness.status === undefined) {
            return { status: "unsupported" };
        }

        return {
            status: "ok",
            freshness: mlwhSyncStatusSchema.parse(freshness),
        };
    } catch (error) {
        if (isInvalidFreshnessResponse(error)) {
            return { status: "invalid_response" };
        }

        return { status: "unavailable" };
    }
}

// isInvalidFreshnessResponse reports whether error is a successful response
// whose body failed schema validation, rather than a failed request.
function isInvalidFreshnessResponse(error: unknown): boolean {
    if (error instanceof ZodError) {
        return true;
    }

    return (
        error instanceof BackendRequestError &&
        error.status >= 200 &&
        error.status < 300
    );
}
