import type { NextConfig } from "next";

const BASE_DEV_ORIGINS = ["localhost", "127.0.0.1"];

type EnvLike = Record<string, string | undefined>;

export function resolveAllowedDevOrigins(
    env: EnvLike = process.env,
): string[] {
    const raw = env.WA_DEV_ALLOWED_ORIGINS ?? "";
    const extra = raw
        .split(",")
        .map((entry) => entry.trim())
        .filter((entry) => entry.length > 0);

    return Array.from(new Set([...BASE_DEV_ORIGINS, ...extra]));
}

const isDev = process.env.NODE_ENV !== "production";

const nextConfig: NextConfig = {
    reactStrictMode: true,
    ...(isDev ? { allowedDevOrigins: resolveAllowedDevOrigins() } : {}),
};

export default nextConfig;
