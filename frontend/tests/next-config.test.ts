import { afterEach, describe, expect, it } from "vitest";

import { resolveAllowedDevOrigins } from "../next.config";

describe("resolveAllowedDevOrigins", () => {
    const originalEnv = process.env.WA_DEV_ALLOWED_ORIGINS;

    afterEach(() => {
        if (originalEnv === undefined) {
            delete process.env.WA_DEV_ALLOWED_ORIGINS;
        } else {
            process.env.WA_DEV_ALLOWED_ORIGINS = originalEnv;
        }
    });

    it("always includes loopback and Sanger wildcard hosts so dev server works for internal users", () => {
        const origins = resolveAllowedDevOrigins({} as NodeJS.ProcessEnv);

        expect(origins).toEqual(
            expect.arrayContaining([
                "localhost",
                "127.0.0.1",
                "*.sanger.ac.uk",
                "*.internal.sanger.ac.uk",
            ]),
        );
    });

    it("appends comma-separated WA_DEV_ALLOWED_ORIGINS entries with whitespace trimmed", () => {
        const origins = resolveAllowedDevOrigins({
            WA_DEV_ALLOWED_ORIGINS:
                "farm22-wrstat01.internal.sanger.ac.uk, my-laptop.local",
        } as NodeJS.ProcessEnv);

        expect(origins).toContain("farm22-wrstat01.internal.sanger.ac.uk");
        expect(origins).toContain("my-laptop.local");
    });

    it("deduplicates origins present in both defaults and the env var", () => {
        const origins = resolveAllowedDevOrigins({
            WA_DEV_ALLOWED_ORIGINS: "localhost,my-laptop.local",
        } as NodeJS.ProcessEnv);

        const localhosts = origins.filter((entry) => entry === "localhost");

        expect(localhosts).toHaveLength(1);
    });

    it("ignores empty entries produced by trailing or stray commas", () => {
        const origins = resolveAllowedDevOrigins({
            WA_DEV_ALLOWED_ORIGINS: ",,host.example.com,, ,",
        } as NodeJS.ProcessEnv);

        expect(origins).not.toContain("");
        expect(origins).toContain("host.example.com");
    });
});
