export const usageSessionCookieName = "wa_usage_session";
export const usageSessionCookieOptions = {
    httpOnly: true,
    maxAge: 365 * 24 * 60 * 60,
    path: "/",
    sameSite: "lax",
    secure: true,
} as const;
