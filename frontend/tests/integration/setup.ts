import { spawn, type ChildProcessByStdio } from "node:child_process";
import { chmod, mkdir, mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { request as httpsRequest } from "node:https";
import { createServer } from "node:net";
import { userInfo } from "node:os";
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

type EnvLike = Record<string, string | undefined>;

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

export function buildResultsServerEnv(env: EnvLike): EnvLike {
    const serverEnv = { ...env };

    delete serverEnv.WA_MLWH_BACKEND_URL;

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
                createCommandError(
                    `${command} ${args.join(" ")}`,
                    stderr,
                    stdout,
                ),
            );
        });
    });
}

function signalProcessGroup(
    child: ServerProcess,
    signal: NodeJS.Signals,
): void {
    if (child.pid === undefined) {
        return;
    }

    try {
        process.kill(-child.pid, signal);
    } catch (error) {
        const processError = error as NodeJS.ErrnoException;

        if (processError.code === "ESRCH") {
            return;
        }

        throw error;
    }
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
    caCertPath: string,
    child: ServerProcess,
    stdout: string[],
    stderr: string[],
): Promise<void> {
    const healthPath = "/rest/v1/results/stats";

    for (let attempt = 0; attempt < 120; attempt += 1) {
        if (child.exitCode !== null) {
            throw createCommandError(
                `wa results serve exited with code ${child.exitCode}`,
                stderr.join(""),
                stdout.join(""),
            );
        }

        try {
            const response = await httpsRequestWithCA(
                baseUrl,
                caCertPath,
                healthPath,
            );

            if (response.statusCode >= 200 && response.statusCode < 300) {
                return;
            }
        } catch {
            // The server is still starting.
        }

        await new Promise((resolve) => setTimeout(resolve, 250));
    }

    throw createCommandError(
        `Timed out waiting for ${new URL(healthPath, baseUrl).toString()}`,
        stderr.join(""),
        stdout.join(""),
    );
}

type HTTPSResponse = {
    statusCode: number;
    body: string;
};

async function httpsRequestWithCA(
    baseUrl: string,
    caCertPath: string,
    resourcePath: string,
    options: {
        method?: string;
        headers?: Record<string, string>;
        body?: string;
    } = {},
): Promise<HTTPSResponse> {
    const ca = await readFile(caCertPath);
    const endpoint = new URL(resourcePath, baseUrl);

    return new Promise<HTTPSResponse>((resolve, reject) => {
        const request = httpsRequest(
            endpoint,
            {
                ca,
                headers: options.headers,
                method: options.method ?? "GET",
            },
            (response) => {
                const chunks: Buffer[] = [];

                response.on("data", (chunk: Buffer) => {
                    chunks.push(chunk);
                });
                response.on("end", () => {
                    resolve({
                        body: Buffer.concat(chunks).toString("utf8"),
                        statusCode: response.statusCode ?? 0,
                    });
                });
            },
        );

        request.on("error", reject);

        if (options.body !== undefined) {
            request.write(options.body);
        }

        request.end();
    });
}

async function ownerJWT(
    baseUrl: string,
    caCertPath: string,
    tokenDir: string,
): Promise<string> {
    const token = (
        await readFile(path.join(tokenDir, ".wa-results-server.token"), "utf8")
    ).trim();
    const body = new URLSearchParams({
        password: token,
        username: userInfo().username,
    }).toString();

    const response = await httpsRequestWithCA(
        baseUrl,
        caCertPath,
        "/rest/v1/jwt",
        {
            body,
            headers: {
                "content-type": "application/x-www-form-urlencoded",
            },
            method: "POST",
        },
    );

    if (response.statusCode !== 200) {
        throw new Error(
            `Owner login failed with ${response.statusCode}: ${response.body}`,
        );
    }

    return JSON.parse(response.body) as string;
}

async function seedResults(
    baseUrl: string,
    caCertPath: string,
    tokenDir: string,
): Promise<string> {
    const rawSeed = await readFile(seedPath, "utf8");
    const registrations = JSON.parse(rawSeed) as SeedRegistration[];
    const jwt = await ownerJWT(baseUrl, caCertPath, tokenDir);

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
        const response = await httpsRequestWithCA(
            baseUrl,
            caCertPath,
            "/rest/v1/auth/results",
            {
                body: JSON.stringify(payload),
                headers: {
                    authorization: `Bearer ${jwt}`,
                    "content-type": "application/json",
                },
                method: "POST",
            },
        );

        if (response.statusCode < 200 || response.statusCode >= 300) {
            throw new Error(
                `Seeding fixtures failed with ${response.statusCode}: ${response.body}`,
            );
        }
    }

    return jwt;
}

async function createSelfSignedCertificate(
    certPath: string,
    keyPath: string,
): Promise<void> {
    await runCommand(
        "openssl",
        [
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-days",
            "7",
            "-keyout",
            keyPath,
            "-out",
            certPath,
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=DNS:localhost,IP:127.0.0.1",
        ],
        repoRoot,
    );
    await chmod(keyPath, 0o600);
    await chmod(certPath, 0o644);
}

export async function stopProcess(child: ServerProcess): Promise<void> {
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
        signalProcessGroup(child, "SIGTERM");

        setTimeout(() => {
            if (child.exitCode === null) {
                signalProcessGroup(child, "SIGKILL");
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
    const certPath = path.join(tempDir, "wa-dev-cert.pem");
    const keyPath = path.join(tempDir, "wa-dev-key.pem");
    const dbPath = path.join(tempDir, "results.sqlite");
    const port = await getFreePort();
    const baseUrl = `https://127.0.0.1:${port}`;
    const previousBackendUrl = process.env.WA_RESULTS_BACKEND_URL;
    const previousBackendCACert = process.env.WA_RESULTS_BACKEND_CA_CERT;
    const previousResultsTestJWT = process.env.WA_RESULTS_TEST_JWT;

    await runCommand("go", ["build", "-o", binaryPath, "."], repoRoot);
    await createSelfSignedCertificate(certPath, keyPath);

    const server = spawn(
        binaryPath,
        [
            "results",
            "serve",
            "--port",
            String(port),
            "--db",
            dbPath,
            "--cert",
            certPath,
            "--key",
            keyPath,
            "--ldap_server",
            "wa-test-ldap.invalid",
            "--ldap_dn",
            "uid=%s,ou=people,dc=example,dc=org",
        ],
        {
            cwd: repoRoot,
            detached: true,
            env: buildResultsServerEnv({
                ...process.env,
                XDG_STATE_HOME: tempDir,
            }) as NodeJS.ProcessEnv,
            stdio: ["ignore", "pipe", "pipe"],
        },
    );
    const { stdout, stderr } = collectProcessOutput(server);

    await waitForServer(baseUrl, certPath, server, stdout, stderr);
    const ownerJWT = await seedResults(baseUrl, certPath, tempDir);

    process.env.WA_RESULTS_BACKEND_URL = baseUrl;
    process.env.WA_RESULTS_BACKEND_CA_CERT = certPath;
    process.env.WA_RESULTS_TEST_JWT = ownerJWT;

    return async () => {
        delete process.env.WA_RESULTS_BACKEND_URL;
        delete process.env.WA_RESULTS_BACKEND_CA_CERT;
        delete process.env.WA_RESULTS_TEST_JWT;
        if (previousBackendUrl) {
            process.env.WA_RESULTS_BACKEND_URL = previousBackendUrl;
        }
        if (previousBackendCACert) {
            process.env.WA_RESULTS_BACKEND_CA_CERT = previousBackendCACert;
        }
        if (previousResultsTestJWT) {
            process.env.WA_RESULTS_TEST_JWT = previousResultsTestJWT;
        }

        await stopProcess(server);
        await rm(tempDir, { force: true, recursive: true });
    };
}
