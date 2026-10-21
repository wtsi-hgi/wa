"use server";

import { mlwhJson } from "@/lib/backend-client";
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
              | "unavailable";
      };

// getMLWHSyncStatusAction reads wa's GET /freshness for an MLWH admin. The
// route needs no admin token, so unlike /usage this works without the token
// file. A server older than MLWH API 1.12.0 sends no status, so it is
// reported as unsupported.
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
    } catch {
        return { status: "unavailable" };
    }
}
