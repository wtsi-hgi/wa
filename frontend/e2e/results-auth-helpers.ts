import { execFileSync } from "node:child_process";
import {
    copyFileSync,
    existsSync,
    mkdirSync,
    readFileSync,
    rmSync,
} from "node:fs";
import path from "node:path";

import type { BrowserContext } from "@playwright/test";

export type ResultSet = {
    id: string;
};

type FileEntry = {
    path: string;
    mtime: string;
    size: number;
    kind: "output" | "input" | "pipeline";
};

export type ResultRegistration = {
    pipeline_identifier: string;
    run_key: string;
    requester: string;
    operator: string;
    command: string;
    pipeline_name: string;
    pipeline_version: string;
    output_directory: string;
    files: FileEntry[];
    metadata: Record<string, string>;
};

const repoRoot = path.resolve(process.cwd(), "..");
const waBinaryPath = path.join(repoRoot, ".tmp", "wa");
const sharedStateHome = path.join(repoRoot, ".tmp", "state-test");
const stateHome = path.join(
    repoRoot,
    ".tmp",
    `state-test-e2e-${process.env.TEST_WORKER_INDEX ?? "0"}`,
);
const serverTokenBasename = ".wa-results-server.token";
const sharedServerTokenPath = path.join(sharedStateHome, serverTokenBasename);
const workerServerTokenPath = path.join(stateHome, serverTokenBasename);
const jwtPath = path.join(stateHome, ".wa-results.jwt");
const certPath = path.join(repoRoot, ".tmp", "wa-dev-cert.pem");
let jwtReady = false;

function requiredEnv(name: string): string {
    const value = process.env[name];

    if (!value) {
        throw new Error(`${name} is required for this test`);
    }

    return value;
}

function resultsBackendUrl(): string {
    return `https://127.0.0.1:${requiredEnv("WA_TEST_RESULTS_PORT")}`;
}

function frontendBaseUrl(): string {
    return `http://127.0.0.1:${requiredEnv("WA_TEST_FRONTEND_PORT")}`;
}

function commandEnv(): NodeJS.ProcessEnv {
    return {
        ...process.env,
        WA_ENV: "test",
        WA_TEST_RESULTS_PORT: requiredEnv("WA_TEST_RESULTS_PORT"),
        XDG_STATE_HOME: stateHome,
    };
}

function prepareStateHome(): void {
    mkdirSync(stateHome, { recursive: true });
    copyFileSync(sharedServerTokenPath, workerServerTokenPath);
}

export function registerResult(registration: ResultRegistration): ResultSet {
    prepareStateHome();

    const output = execFileSync(
        waBinaryPath,
        [
            "results",
            "register",
            "--json",
            "--server",
            resultsBackendUrl(),
            "--cert",
            certPath,
        ],
        {
            cwd: repoRoot,
            encoding: "utf8",
            env: commandEnv(),
            input: `${JSON.stringify(registration)}\n`,
            stdio: ["pipe", "pipe", "pipe"],
        },
    );

    jwtReady = existsSync(jwtPath);

    return JSON.parse(output) as ResultSet;
}

export function deleteResult(resultId: string): void {
    prepareStateHome();

    try {
        execFileSync(
            waBinaryPath,
            [
                "results",
                "delete",
                "--server",
                resultsBackendUrl(),
                "--cert",
                certPath,
                resultId,
            ],
            {
                cwd: repoRoot,
                encoding: "utf8",
                env: commandEnv(),
                stdio: ["ignore", "pipe", "pipe"],
            },
        );
    } catch (error) {
        const commandError = error as { message?: string; stderr?: string };
        const message = `${commandError.stderr ?? ""}\n${commandError.message ?? ""}`;

        if (!message.includes("results server returned 404")) {
            throw error;
        }
    }

    jwtReady = existsSync(jwtPath);
}

function ensureResultsJWT(): void {
    if (jwtReady && existsSync(jwtPath)) {
        return;
    }

    prepareStateHome();
    rmSync(jwtPath, { force: true });

    try {
        execFileSync(
            waBinaryPath,
            [
                "results",
                "delete",
                "--server",
                resultsBackendUrl(),
                "--cert",
                certPath,
                "__wa_e2e_auth_probe__",
            ],
            {
                cwd: repoRoot,
                encoding: "utf8",
                env: commandEnv(),
                stdio: ["ignore", "pipe", "pipe"],
            },
        );
    } catch (error) {
        const commandError = error as { message?: string; stderr?: string };
        const message = `${commandError.stderr ?? ""}\n${commandError.message ?? ""}`;

        if (!message.includes("results server returned 404")) {
            throw error;
        }
    }

    jwtReady = true;
}

export async function installResultsAuthCookie(
    context: BrowserContext,
): Promise<void> {
    ensureResultsJWT();

    const jwt = readFileSync(jwtPath, "utf8").trim();

    await context.addCookies([
        {
            name: "wa_results_jwt",
            value: jwt,
            url: frontendBaseUrl(),
        },
    ]);
}
