import { spawn, type ChildProcessByStdio } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { createServer } from "node:net";
import path from "node:path";
import type { Readable } from "node:stream";
import { fileURLToPath } from "node:url";

type ServerProcess = ChildProcessByStdio<null, Readable, Readable>;

type SeedFile = {
    path: string;
    mtime: string;
    size: number;
    kind: "output" | "input" | "pipeline";
};

type SeedRegistration = {
    output_directory: string;
    files: SeedFile[];
} & Record<string, unknown>;

const setupDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(setupDir, "..", "..");
const repoRoot = path.resolve(frontendRoot, "..");
const agentTmpRoot = path.join(repoRoot, ".tmp", "agent");
const seedPath = path.join(
    repoRoot,
    ".docs",
    "results-web",
    "fixtures",
    "seed.json",
);

export function buildResultsServerEnv(
    env: NodeJS.ProcessEnv,
): NodeJS.ProcessEnv {
    const serverEnv = { ...env };

    delete serverEnv.WA_SEQMETA_BACKEND_URL;

    return serverEnv;
}

function createCommandError(
    command: string,
    stderr: string,
    stdout: string,
): Error {
    const output = [stderr.trim(), stdout.trim()].filter(Boolean).join("\n");

    return new Error(
        output ? `${command} failed\n${output}` : `${command} failed`,
    );
}

async function runCommand(
    command: string,
    args: string[],
    cwd: string,
): Promise<void> {
    await new Promise<void>((resolve, reject) => {
        const child = spawn(command, args, {
            cwd,
            env: process.env,
            stdio: ["ignore", "pipe", "pipe"],
        });

        let stdout = "";
        let stderr = "";

        child.stdout.on("data", (chunk: Buffer | string) => {
            stdout += chunk.toString();
        });
        child.stderr.on("data", (chunk: Buffer | string) => {
            stderr += chunk.toString();
        });
        child.on("error", reject);
        child.on("exit", (code) => {
            if (code === 0) {
                resolve();

                return;
            }

            reject(
                createCommandError(`${command} ${args.join(" ")}`, stderr, stdout),
            );
        });
    });
}

async function getFreePort(): Promise<number> {
    return new Promise<number>((resolve, reject) => {
        const server = createServer();

        server.on("error", reject);
        server.listen(0, "127.0.0.1", () => {
            const address = server.address();

            if (!address || typeof address === "string") {
                server.close(() => {
                    reject(new Error("Unable to determine a free port"));
                });

                return;
            }

            server.close((error) => {
                if (error) {
                    reject(error);

                    return;
                }

                resolve(address.port);
            });
        });
    });
}

function collectProcessOutput(child: ServerProcess): {
    stdout: string[];
    stderr: string[];
} {
    const stdout: string[] = [];
    const stderr: string[] = [];

    child.stdout.on("data", (chunk: Buffer | string) => {
        stdout.push(chunk.toString());
    });
    child.stderr.on("data", (chunk: Buffer | string) => {
        stderr.push(chunk.toString());
    });

    return { stdout, stderr };
}

async function waitForServer(
    baseUrl: string,
    child: ServerProcess,
    stdout: string[],
    stderr: string[],
): Promise<void> {
    const healthUrl = new URL("/results/stats", baseUrl);

    for (let attempt = 0; attempt < 120; attempt += 1) {
        if (child.exitCode !== null) {
            throw createCommandError(
                `wa results serve exited with code ${child.exitCode}`,
                stderr.join(""),
                stdout.join(""),
            );
        }

        try {
            const response = await fetch(healthUrl, { cache: "no-store" });

            if (response.ok) {
                return;
            }
        } catch {
            // The server is still starting.
        }

        await new Promise((resolve) => setTimeout(resolve, 250));
    }

    throw createCommandError(
        `Timed out waiting for ${healthUrl.toString()}`,
        stderr.join(""),
        stdout.join(""),
    );
}

async function seedResults(baseUrl: string): Promise<void> {
    const rawSeed = await readFile(seedPath, "utf8");
    const registrations = JSON.parse(rawSeed) as SeedRegistration[];

    for (const registration of registrations) {
        const outputDirectory = path.resolve(
            repoRoot,
            registration.output_directory,
        );
        const files = await Promise.all(
            registration.files.map(async (file) => {
                const absolutePath = path.isAbsolute(file.path)
                    ? file.path
                    : path.resolve(outputDirectory, file.path);
                const fileStats = await stat(absolutePath);

                return {
                    ...file,
                    path: absolutePath,
                    mtime: fileStats.mtime.toISOString(),
                    size: fileStats.size,
                };
            }),
        );

        const payload = {
            ...registration,
            output_directory: outputDirectory,
            files,
        };
        const response = await fetch(new URL("/results", baseUrl), {
            method: "POST",
            headers: {
                "content-type": "application/json",
            },
            body: JSON.stringify(payload),
        });

        if (!response.ok) {
            throw new Error(
                `Seeding fixtures failed with ${response.status}: ${await response.text()}`,
            );
        }
    }
}

async function stopProcess(child: ServerProcess): Promise<void> {
    if (child.exitCode !== null || child.killed) {
        return;
    }

    await new Promise<void>((resolve) => {
        let settled = false;

        const finish = () => {
            if (settled) {
                return;
            }

            settled = true;
            resolve();
        };

        child.once("exit", finish);
        child.kill("SIGTERM");

        setTimeout(() => {
            if (child.exitCode === null) {
                child.kill("SIGKILL");
            }

            finish();
        }, 5000).unref();
    });
}

export default async function setup(): Promise<() => Promise<void>> {
    await mkdir(agentTmpRoot, { recursive: true });

    const tempDir = await mkdtemp(
        path.join(agentTmpRoot, "results-integration-"),
    );
    const binaryPath = path.join(tempDir, "wa");
    const dbPath = path.join(tempDir, "results.sqlite");
    const port = await getFreePort();
    const baseUrl = `http://127.0.0.1:${port}`;
    const previousBackendUrl = process.env.WA_RESULTS_BACKEND_URL;

    await runCommand("go", ["build", "-o", binaryPath, "."], repoRoot);

    const server = spawn(
        binaryPath,
        ["results", "serve", "--port", String(port), "--db", dbPath],
        {
            cwd: repoRoot,
            env: buildResultsServerEnv(process.env),
            stdio: ["ignore", "pipe", "pipe"],
        },
    );
    const { stdout, stderr } = collectProcessOutput(server);

    await waitForServer(baseUrl, server, stdout, stderr);
    await seedResults(baseUrl);

    process.env.WA_RESULTS_BACKEND_URL = baseUrl;

    return async () => {
        delete process.env.WA_RESULTS_BACKEND_URL;
        if (previousBackendUrl) {
            process.env.WA_RESULTS_BACKEND_URL = previousBackendUrl;
        }

        await stopProcess(server);
        await rm(tempDir, { force: true, recursive: true });
    };
}
