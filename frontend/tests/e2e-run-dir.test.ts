import { execFileSync } from "node:child_process";
import {
    existsSync,
    mkdirSync,
    mkdtempSync,
    readlinkSync,
    rmSync,
    symlinkSync,
    writeFileSync,
} from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

const frontendRoot = process.cwd();
const repoRoot = path.resolve(frontendRoot, "..");

type RunDevEnv = {
    WA_RUN_DEV_TMP_DIR?: string;
    WA_RUN_DEV_LOG_DIR?: string;
    WA_RESULTS_SERVER_CERT?: string;
    WA_RESULTS_SERVER_KEY?: string;
};

let runDevEnv: RunDevEnv;
let scratchRoot: string | undefined;

function isWithin(child: string, parent: string): boolean {
    const relative = path.relative(parent, child);

    return (
        relative !== "" &&
        !relative.startsWith("..") &&
        !path.isAbsolute(relative)
    );
}

function plant(root: string, relativePath: string): string {
    const target = path.join(root, relativePath);
    mkdirSync(path.dirname(target), { recursive: true });
    writeFileSync(target, "sentinel\n");

    return target;
}

beforeAll(async () => {
    // Pinned ports stop the config allocating ports and writing its cache.
    vi.stubEnv("WA_TEST_FRONTEND_PORT", "3000");
    vi.stubEnv("WA_TEST_RESULTS_PORT", "8090");
    vi.stubEnv("WA_TEST_SEQMETA_PORT", "8091");

    const configModule = (await import(
        pathToFileURL(path.join(frontendRoot, "playwright.config.ts")).href
    )) as { default: { webServer?: { env?: RunDevEnv } } };

    vi.unstubAllEnvs();
    runDevEnv = configModule.default.webServer?.env ?? {};
});

afterEach(() => {
    if (scratchRoot) {
        rmSync(scratchRoot, { recursive: true, force: true });
        scratchRoot = undefined;
    }
});

describe("Playwright's run-dev stack directory", () => {
    it("keeps run-dev's scratch files, logs and TLS material out of the checkout's shared .tmp and logs", () => {
        const sharedTmp = path.join(repoRoot, ".tmp");
        const tmpDir = runDevEnv.WA_RUN_DEV_TMP_DIR ?? "";

        expect(isWithin(tmpDir, sharedTmp)).toBe(true);
        expect(isWithin(runDevEnv.WA_RUN_DEV_LOG_DIR ?? "", tmpDir)).toBe(true);
        expect(isWithin(runDevEnv.WA_RESULTS_SERVER_CERT ?? "", tmpDir)).toBe(
            true,
        );
        expect(isWithin(runDevEnv.WA_RESULTS_SERVER_KEY ?? "", tmpDir)).toBe(
            true,
        );
    });

    it("is what make clean-test-tmp removes, leaving another stack's .tmp files, logs and agent evidence alone", () => {
        const e2eDir = path.relative(
            repoRoot,
            runDevEnv.WA_RUN_DEV_TMP_DIR ?? "",
        );
        scratchRoot = mkdtempSync(path.join(os.tmpdir(), "wa-clean-test-"));

        const kept = [
            ".tmp/wa-dev-cert.pem",
            ".tmp/wa-dev-key.pem",
            ".tmp/state-test/.wa-results-server.token",
            ".tmp/results-dev.abc123.sqlite",
            ".tmp/mlwh-test.abc123.sqlite",
            ".tmp/mlwh-feedback-test.abc123.sqlite",
            ".tmp/mlwh-usage-test.abc123.sqlite",
            ".tmp/agent/evidence.png",
            "logs/results.log",
        ].map((relativePath) => plant(scratchRoot as string, relativePath));
        const sharedLink = path.join(scratchRoot, ".tmp", "wa");
        symlinkSync("/nonexistent/wa", sharedLink);

        for (const relativePath of [
            "wa-dev-key.pem",
            "playwright-ports.json",
            "state-test/.wa-results-server.token",
            "results-dev.abc123.sqlite",
            "logs/results.log",
        ]) {
            plant(scratchRoot, path.join(e2eDir, relativePath));
        }

        execFileSync(
            "make",
            [
                "--no-print-directory",
                "-f",
                path.join(repoRoot, "Makefile"),
                "-C",
                scratchRoot,
                "clean-test-tmp",
            ],
            { stdio: ["ignore", "pipe", "pipe"] },
        );

        expect(existsSync(path.join(scratchRoot, e2eDir))).toBe(false);
        expect(kept.filter((target) => !existsSync(target))).toEqual([]);
        expect(readlinkSync(sharedLink)).toBe("/nonexistent/wa");
    });
});
