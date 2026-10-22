// Plain-English explanations of the error codes GET /usage reports, shown on
// the admin /usage page. Codes come from mlwh/errors_http.go, plus no_route
// and uncoded, which the usage middleware stores itself.

export type UsageErrorHelp = {
    action: string;
    label: string;
    meaning: string;
};

const legacyHelp: UsageErrorHelp = {
    label: "unknown (recorded before error details were captured)",
    meaning:
        "These errors were recorded before the server stored error codes and messages.",
    action: "Nothing to act on here; newer errors on this endpoint show their details.",
};

const codeHelp: Record<string, Omit<UsageErrorHelp, "label">> = {
    not_found: {
        meaning: "The identifier is not known to the MLWH.",
        action: "Often an agent guessing identifiers or using the wrong kind of identifier. Check the examples for what was asked; many misses from one MCP tool may mean its description needs work.",
    },
    ambiguous: {
        meaning: "The identifier matches more than one record.",
        action: "The caller should say which kind of identifier it is, or use a more specific one. The examples show which identifiers were ambiguous.",
    },
    unsupported_identifier: {
        meaning: "The identifier or argument is not one this endpoint accepts.",
        action: "Usually routine: the results page's search box classifies each typed term to offer suggestions, and text that is no known kind of identifier gets this answer. If one caller repeats it on another endpoint, check the messages and examples: it may be sending a name where an ID is needed, an identifier for a different kind of record, an unsupported kind or role value, or an empty required name.",
    },
    bad_request: {
        meaning: "The request's parameters were invalid.",
        action: "The messages say which parameter was wrong. Repeats from one client or MCP tool point to a client bug or an unclear parameter description.",
    },
    cache_never_synced: {
        meaning:
            "The MLWH cache has never finished a sync, so there is no data to answer from.",
        action: "Check that the cache sync is running and look in the wa mlwh serve logs for sync errors.",
    },
    upstream_impaired: {
        meaning:
            "The server could not read the MLWH cache database or another upstream service.",
        action: "Check the wa mlwh serve logs at the times shown, and the cache database's health.",
    },
    no_route: {
        meaning: "No endpoint matches the request's method and path.",
        action: "A client bug, or an old client calling an endpoint that no longer exists. Open this entry for the paths that were asked for.",
    },
    payload_too_large: {
        meaning: "The request body was over the size limit.",
        action: "Find the client that sent it from the examples; feedback reports are the only requests with a body.",
    },
    internal_error: {
        meaning: "The server failed unexpectedly while answering.",
        action: "Check the wa mlwh serve logs around the times shown.",
    },
    unauthorized: {
        meaning: "The request did not carry a valid token.",
        action: "Check the token the caller is configured with.",
    },
    feedback_disabled: {
        meaning: "Feedback was sent to a server with feedback turned off.",
        action: "Start wa mlwh serve with --feedback-db, or stop the client sending feedback.",
    },
    uncoded: {
        meaning: "The server answered with an error status but no error code.",
        action: "A server handler bypassed the shared error writer; check the wa mlwh serve logs and report it as a bug.",
    },
};

// usageErrorKindHelp explains the internal causes the server records where
// they differ from the public code.
export const usageErrorKindHelp: Record<string, string> = {
    timeout:
        "Timeouts mean a slow query or an overloaded database: check the wa mlwh serve logs for the query, and the cache database's load and indexes.",
    canceled:
        "Canceled means the client went away before the answer, usually because its own timeout expired on a slow query.",
};

// usageErrorHelp returns the label, meaning, and suggested action for code,
// or for rows recorded before codes were stored when code is null.
export function usageErrorHelp(code: string | null): UsageErrorHelp {
    if (code === null) {
        return legacyHelp;
    }

    // hasOwn, so a code such as "constructor" cannot match Object.prototype.
    if (Object.hasOwn(codeHelp, code)) {
        return { label: code, ...codeHelp[code] };
    }

    return {
        label: code,
        meaning: "No explanation for this code.",
        action: "See that entry's messages and examples.",
    };
}

// legacyStatusCodes are the codes wa answers each status with, for rows
// recorded before error codes were stored (mlwh/errors_http.go).
const legacyStatusCodes: Record<number, string> = {
    400: "bad_request",
    401: "unauthorized",
    409: "ambiguous",
    413: "payload_too_large",
    422: "unsupported_identifier",
    500: "internal_error",
    502: "upstream_impaired",
    503: "cache_never_synced",
};

// impliedErrorCode is the code wa sends with status on route ("" when
// unmatched), or null when the status does not imply one.
export function impliedErrorCode(status: number, route: string): string | null {
    if (status === 404) {
        return route ? "not_found" : "no_route";
    }

    return legacyStatusCodes[status] ?? null;
}

// usageGroupHelp is usageErrorHelp for a group's code, except that a group
// recorded before error codes gets the help of the code its status implies,
// labelled as inferred.
export function usageGroupHelp(
    code: string | null,
    status: number,
    route: string,
): UsageErrorHelp {
    const implied = code === null ? impliedErrorCode(status, route) : null;

    if (implied === null) {
        return usageErrorHelp(code);
    }

    const help = usageErrorHelp(implied);

    return {
        label: `${implied} (inferred, recorded before error details)`,
        meaning: `${help.meaning} These errors were recorded before error codes were stored, so the code is inferred from the status.`,
        action: help.action,
    };
}
