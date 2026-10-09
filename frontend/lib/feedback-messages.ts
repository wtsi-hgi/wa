import type { FeedbackUnavailableReason } from "@/app/(results)/feedback/actions";

// feedbackReasonMessages explains each reason feedback is unavailable, for
// the /feedback page and its mutation toasts.
export const feedbackReasonMessages: Record<FeedbackUnavailableReason, string> =
    {
        no_token: "The MLWH server token is not readable by this server.",
        feedback_disabled:
            "Feedback collection is disabled on the MLWH server.",
        token_rejected: "The MLWH server rejected the admin token.",
        unsupported:
            "wa mlwh serve is too old for feedback (needs MLWH API 1.9.0).",
        backend_error: "The MLWH server could not be reached.",
    };
