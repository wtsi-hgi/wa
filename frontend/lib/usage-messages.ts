import type { UsageUnavailableReason } from "@/app/(results)/usage/actions";

// usageReasonMessages explains each reason usage metrics are unavailable,
// for the /usage page.
export const usageReasonMessages: Record<UsageUnavailableReason, string> = {
    no_token: "The MLWH server token is not readable by this server.",
    usage_disabled: "Usage tracking is disabled on the MLWH server.",
    token_rejected: "The MLWH server rejected the admin token.",
    unsupported:
        "wa mlwh serve is too old for usage metrics (needs MLWH API 1.10.0).",
    backend_error: "The MLWH server could not be reached.",
};
