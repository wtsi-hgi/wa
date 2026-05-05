import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { describe, expect, it } from "vitest";

const testDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(testDir, "..");
const repoRoot = path.resolve(frontendRoot, "..");

describe("G1 scaffold", () => {
    it(
        "builds successfully with the required scaffold files present",
        { timeout: 30000 },
        () => {
            const requiredPaths = [
                "package.json",
                "components.json",
                "tsconfig.json",
                "next.config.ts",
                "vitest.config.ts",
                "eslint.config.mjs",
                "postcss.config.cjs",
                "app/globals.css",
                "app/layout.tsx",
                "app/(results)/page.tsx",
                "components/theme-provider.tsx",
                "components/ui/toaster.tsx",
                "lib/utils.ts",
                ".env.example",
            ];

            for (const relativePath of requiredPaths) {
                expect(
                    existsSync(path.join(frontendRoot, relativePath)),
                    relativePath,
                ).toBe(true);
            }

            execFileSync("pnpm", ["build"], {
                cwd: frontendRoot,
                env: process.env,
                stdio: "pipe",
            });
        },
    );

    it("includes the required frontend environment variables", () => {
        const envExample = readFileSync(
            path.join(frontendRoot, ".env.example"),
            "utf8",
        );

        expect(envExample).toContain("WA_RESULTS_BACKEND_URL=");
        expect(envExample).toContain("WA_SEQMETA_BACKEND_URL=");
        expect(envExample).toContain("WA_STUDIES_CACHE_TTL_SECONDS=");
    });

    it("documents the per-scenario root env files", () => {
        const testEnv = readFileSync(path.join(repoRoot, ".env.test"), "utf8");
        const devExample = readFileSync(
            path.join(repoRoot, ".env.dev.example"),
            "utf8",
        );
        const prodExample = readFileSync(
            path.join(repoRoot, ".env.prod.example"),
            "utf8",
        );

        expect(testEnv).toContain("WA_ENV=test");
        expect(testEnv).toContain("WA_TEST_FRONTEND_PORT=");
        expect(testEnv).toContain("WA_TEST_RESULTS_PORT=");
        expect(testEnv).toContain("WA_TEST_SEQMETA_PORT=");
        expect(testEnv).not.toMatch(/^WA_RESULTS_DB_PATH=/m);

        expect(devExample).toContain("WA_ENV=development");
        expect(devExample).toContain("WA_DEV_FRONTEND_PORT=");
        expect(devExample).toContain("WA_DEV_RESULTS_PORT=");
        expect(devExample).toContain("WA_DEV_SEQMETA_PORT=");
        expect(devExample).toContain("WA_RESULTS_DB_PATH=");
        expect(devExample).toContain("SAGA_API_TOKEN=");

        expect(prodExample).toContain("WA_ENV=production");
        expect(prodExample).toContain("WA_PROD_FRONTEND_PORT=");
        expect(prodExample).toContain("WA_PROD_RESULTS_PORT=");
        expect(prodExample).toContain("WA_PROD_SEQMETA_PORT=");
        expect(prodExample).toContain("WA_RESULTS_DB_PATH=");
    });

    it("commits .env.test so make test works on a fresh clone", () => {
        const tracked = execFileSync(
            "git",
            ["ls-files", "--error-unmatch", ".env.test"],
            { cwd: repoRoot, stdio: ["ignore", "pipe", "pipe"] },
        )
            .toString()
            .trim();

        expect(tracked).toBe(".env.test");
    });

    it("uses the shadcn new-york style and utility alias", () => {
        const componentsConfig = JSON.parse(
            readFileSync(path.join(frontendRoot, "components.json"), "utf8"),
        ) as {
            style?: string;
            aliases?: {
                utils?: string;
            };
        };

        expect(componentsConfig.style).toBe("new-york");
        expect(componentsConfig.aliases?.utils).toBe("@/lib/utils");
    });

    it("configures vitest for the node environment with the frontend root alias", async () => {
        const configModule = (await import(
            pathToFileURL(path.join(frontendRoot, "vitest.config.ts")).href
        )) as {
            default: {
                test?: {
                    environment?: string;
                };
                resolve?: {
                    alias?: Record<string, string>;
                };
            };
        };

        expect(configModule.default.test?.environment).toBe("node");
        expect(configModule.default.resolve?.alias?.["@"]).toBe(
            frontendRoot + path.sep,
        );
        expect(existsSync(path.join(repoRoot, ".env.test"))).toBe(true);
        expect(existsSync(path.join(repoRoot, ".env.dev.example"))).toBe(true);
        expect(existsSync(path.join(repoRoot, ".env.prod.example"))).toBe(true);
    });
});
