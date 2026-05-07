import { afterEach, describe, expect, it } from "vitest";

import { resolveAllowedDevOrigins } from "../next.config";

function buildEnv(
    overrides: Record<string, string | undefined> = {},
): Record<string, string | undefined> {
    return {
        ...process.env,
        ...overrides,
    };
}

describe("resolveAllowedDevOrigins", () => {
    const originalEnv = process.env.WA_DEV_ALLOWED_ORIGINS;

    afterEach(() => {
        if (originalEnv === undefined) {
            delete process.env.WA_DEV_ALLOWED_ORIGINS;
        } else {
            process.env.WA_DEV_ALLOWED_ORIGINS = originalEnv;
        }
    });

    it("always includes loopback origins so dev server works without extra config", () => {
        const origins = resolveAllowedDevOrigins(
            buildEnv({ WA_DEV_ALLOWED_ORIGINS: undefined }),
        );

        expect(origins).toEqual(
            expect.arrayContaining(["localhost", "127.0.0.1"]),
        );
    });

    it("appends comma-separated WA_DEV_ALLOWED_ORIGINS entries with whitespace trimmed", () => {
        const origins = resolveAllowedDevOrigins(
            buildEnv({
                WA_DEV_ALLOWED_ORIGINS: "dev-host.example.com, my-laptop.local",
            }),
        );

        expect(origins).toContain("dev-host.example.com");
        expect(origins).toContain("my-laptop.local");
    });

    it("deduplicates origins present in both defaults and the env var", () => {
        const origins = resolveAllowedDevOrigins(
            buildEnv({
                WA_DEV_ALLOWED_ORIGINS: "localhost,my-laptop.local",
            }),
        );

        const localhosts = origins.filter((entry) => entry === "localhost");

        expect(localhosts).toHaveLength(1);
    });

    it("ignores empty entries produced by trailing or stray commas", () => {
        const origins = resolveAllowedDevOrigins(
            buildEnv({
                WA_DEV_ALLOWED_ORIGINS: ",,host.example.com,, ,",
            }),
        );

        expect(origins).not.toContain("");
        expect(origins).toContain("host.example.com");
    });
});
