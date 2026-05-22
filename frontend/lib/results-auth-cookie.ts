export const resultsAuthCookieMaxAgeSeconds = 24 * 60 * 60;
export const resultsAuthCookieName = "wa_results_jwt";
export const resultsAuthCookieOptions = {
    httpOnly: true,
    maxAge: resultsAuthCookieMaxAgeSeconds,
    path: "/",
    sameSite: "lax",
    secure: true,
} as const;
