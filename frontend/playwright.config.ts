import { defineConfig } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";

import { resolveChromiumExecutablePath } from "@/lib/playwright-browser";

const frontendRoot = process.cwd();
const repoRoot = path.resolve(frontendRoot, "..");
const portsFile = path.join(repoRoot, ".tmp", "playwright-ports.json");
const portsFileTtlMs = 10 * 60 * 1000;

type ResolvedPorts = {
    frontendPort: number;
    resultsPort: number;
    seqmetaPort: number;
};

function allocatePort(fallback: string): number {
    const script = [
        "const net = require('node:net');",
        "const server = net.createServer();",
        "server.listen(0, '127.0.0.1', () => {",
        "  const address = server.address();",
        "  if (!address || typeof address === 'string') process.exit(1);",
        "  process.stdout.write(String(address.port));",
        "  server.close();",
        "});",
    ].join(" ");

    const port = execFileSync(process.execPath, ["-e", script], {
        encoding: "utf8",
    }).trim();

    return Number.parseInt(port || fallback, 10);
}

function resolvePorts(): ResolvedPorts {
    const configuredFrontendPort = process.env.WA_TEST_FRONTEND_PORT;
    const configuredResultsPort = process.env.WA_TEST_RESULTS_PORT;
    const configuredSeqmetaPort = process.env.WA_TEST_SEQMETA_PORT;

    if (
        configuredFrontendPort ||
        configuredResultsPort ||
        configuredSeqmetaPort
    ) {
        return {
            frontendPort: Number.parseInt(configuredFrontendPort ?? "3000", 10),
            resultsPort: Number.parseInt(configuredResultsPort ?? "8090", 10),
            seqmetaPort: Number.parseInt(configuredSeqmetaPort ?? "8091", 10),
        };
    }

    try {
        const stats = statSync(portsFile);

        if (Date.now() - stats.mtimeMs <= portsFileTtlMs) {
            const parsed = JSON.parse(
                readFileSync(portsFile, "utf8"),
            ) as ResolvedPorts;

            if (
                parsed.frontendPort > 0 &&
                parsed.resultsPort > 0 &&
                parsed.seqmetaPort > 0
            ) {
                return parsed;
            }
        }
    } catch {
        // Fall through to allocate fresh ports.
    }

    const allocated = {
        frontendPort: allocatePort("3000"),
        resultsPort: allocatePort("8090"),
        seqmetaPort: allocatePort("8091"),
    };

    mkdirSync(path.dirname(portsFile), { recursive: true });
    writeFileSync(portsFile, JSON.stringify(allocated), "utf8");

    return allocated;
}

const { frontendPort, resultsPort, seqmetaPort } = resolvePorts();
process.env.WA_TEST_FRONTEND_PORT = String(frontendPort);
process.env.WA_TEST_RESULTS_PORT = String(resultsPort);
process.env.WA_TEST_SEQMETA_PORT = String(seqmetaPort);
const frontendHealthUrl = `http://127.0.0.1:${frontendPort}/api/health`;
const seqmetaStubPath = path.join(frontendRoot, "e2e", "seqmeta-stub.mjs");
const chromiumExecutablePath = resolveChromiumExecutablePath();
const defaultFrontendHealthMaxAttempts = 120;
const frontendHealthMaxAttempts = 720;
const frontendHealthPollIntervalMs = 250;
const frontendStartupTimeoutMs =
    180_000 +
    Math.max(0, frontendHealthMaxAttempts - defaultFrontendHealthMaxAttempts) *
        frontendHealthPollIntervalMs;
const frontendStartCommand = [
    `WA_RUN_DEV_FRONTEND_HEALTH_URL=${JSON.stringify(frontendHealthUrl)}`,
    `WA_RUN_DEV_FRONTEND_HEALTH_MAX_ATTEMPTS=${JSON.stringify(String(frontendHealthMaxAttempts))}`,
    'WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD="printf \"\""',
    `WA_RUN_DEV_SEQMETA_CMD=${JSON.stringify(
        `node ${JSON.stringify(seqmetaStubPath)} ${seqmetaPort}`,
    )}`,
    `WA_RUN_DEV_FRONTEND_DEV_CMD=${JSON.stringify(
        `bash -lc ${JSON.stringify(
            `pnpm exec next build && exec pnpm exec next start --port ${frontendPort}`,
        )}`,
    )}`,
    `bash ../run-dev.sh --mode test --frontend-port ${frontendPort} --results-port ${resultsPort} --seqmeta-port ${seqmetaPort}`,
].join(" ");

export default defineConfig({
    testDir: "./e2e",
    timeout: 60_000,
    fullyParallel: false,
    retries: process.env.CI ? 2 : 0,
    use: {
        baseURL: `http://127.0.0.1:${frontendPort}`,
        browserName: "chromium",
        launchOptions: chromiumExecutablePath
            ? {
                  executablePath: chromiumExecutablePath,
              }
            : undefined,
        trace: "on-first-retry",
    },
    webServer: {
        command: frontendStartCommand,
        port: frontendPort,
        reuseExistingServer: true,
        timeout: frontendStartupTimeoutMs,
    },
});
