import { existsSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { MLWHBadge } from "@/components/mlwh-badge";
import {
    buildMLWHCacheCookie,
    MLWHCache,
    MLWH_CACHE_COOKIE_NAME,
} from "@/lib/mlwh-cache-core";
import { MLWHCacheProvider } from "@/lib/mlwh-cache";

const frontendRoot = process.cwd();

function legacyLibPath(suffix: string): string {
    return join(frontendRoot, "lib", `${["seqmeta", suffix].join("-")}.ts`);
}

function legacyComponentPath(): string {
    return join(
        frontendRoot,
        "components",
        `${["seqmeta", "badge"].join("-")}.tsx`,
    );
}

describe("E5 MLWH frontend renames", () => {
    it("exposes the renamed cache and component contracts", () => {
        const cache = new MLWHCache();
        cache.set("missing-id", null);

        expect(cache.has("missing-id")).toBe(true);
        expect(typeof MLWHBadge).toBe("function");
        expect(typeof MLWHCacheProvider).toBe("function");
    });

    it("keeps persisted cache cookie compatibility under the renamed API", () => {
        const legacyCookieName = ["wa", "seqmeta", "cache"].join("-");

        expect(MLWH_CACHE_COOKIE_NAME).toBe(legacyCookieName);
        expect(buildMLWHCacheCookie({ "missing-id": null })).toContain(
            `${legacyCookieName}=`,
        );
    });

    it("keeps only the renamed frontend module files", () => {
        expect(existsSync(join(frontendRoot, "lib/mlwh-cache-core.ts"))).toBe(
            true,
        );
        expect(existsSync(join(frontendRoot, "lib/mlwh-cache.ts"))).toBe(true);
        expect(existsSync(join(frontendRoot, "lib/mlwh-cache-server.ts"))).toBe(
            true,
        );
        expect(existsSync(join(frontendRoot, "lib/mlwh-enrichment.ts"))).toBe(
            true,
        );
        expect(
            existsSync(join(frontendRoot, "components/mlwh-badge.tsx")),
        ).toBe(true);

        expect(existsSync(legacyLibPath("cache-core"))).toBe(false);
        expect(existsSync(legacyLibPath("cache"))).toBe(false);
        expect(existsSync(legacyLibPath("cache-server"))).toBe(false);
        expect(existsSync(legacyLibPath("enrichment"))).toBe(false);
        expect(existsSync(legacyComponentPath())).toBe(false);
    });
});
