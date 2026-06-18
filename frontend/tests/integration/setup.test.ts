import { spawn } from "node:child_process";
import net from "node:net";

import { describe, expect, it } from "vitest";

import {
    buildResultsServerArgs,
    buildResultsServerEnv,
    stopProcess,
} from "./setup";

describe("integration setup environment", () => {
    it("removes inherited seqmeta backend configuration before starting the results server", () => {
        const env = buildResultsServerEnv({
            HOME: "/tmp/home",
            PATH: "/usr/bin",
            WA_RESULTS_BACKEND_URL: "http://127.0.0.1:9999",
            WA_MLWH_BACKEND_URL: "http://127.0.0.1:3673",
        });

        expect(env).toMatchObject({
            HOME: "/tmp/home",
            PATH: "/usr/bin",
            WA_RESULTS_BACKEND_URL: "http://127.0.0.1:9999",
        });
        expect(env.WA_MLWH_BACKEND_URL).toBeUndefined();
    });

    it("passes the fake MLWH server URL to results serve", () => {
        const args = buildResultsServerArgs({
            certPath: "/tmp/wa-dev-cert.pem",
            dbPath: "/tmp/results.sqlite",
            keyPath: "/tmp/wa-dev-key.pem",
            mlwhServerUrl: "http://127.0.0.1:9010",
            port: 8443,
        });

        expect(args).toContain("--mlwh-server-url");
        expect(args).toContain("http://127.0.0.1:9010");
        expect(args.join(" ")).toContain("results serve");
    });

    it("stops the spawned results server process group so child listeners do not leak", async () => {
        const childPort = await getFreePortForTest();
        const server = spawn(
            process.execPath,
            [
                "-e",
                [
                    "const { spawn } = require('node:child_process');",
                    "const port = Number(process.argv[1]);",
                    "spawn(process.execPath, ['-e', \"const http = require('node:http'); const port = Number(process.argv[1]); http.createServer((_, response) => response.end('ok')).listen(port, '127.0.0.1'); setInterval(() => {}, 1000);\", String(port)], { stdio: 'ignore' });",
                    "setInterval(() => {}, 1000);",
                ].join(" "),
                String(childPort),
            ],
            {
                detached: true,
                stdio: ["ignore", "pipe", "pipe"],
            },
        );

        try {
            await waitForPortForTest(childPort, true);
            await stopProcess(server as Parameters<typeof stopProcess>[0]);
            await waitForPortForTest(childPort, false);
        } finally {
            try {
                process.kill(-server.pid!, "SIGKILL");
            } catch {
                // The process group already exited.
            }
        }
    });
});

function getFreePortForTest(): Promise<number> {
    return new Promise((resolve, reject) => {
        const server = net.createServer();

        server.once("error", reject);
        server.listen(0, "127.0.0.1", () => {
            const address = server.address();
            if (!address || typeof address === "string") {
                server.close(() =>
                    reject(new Error("Unable to determine a free port")),
                );

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

async function waitForPortForTest(
    port: number,
    expectedOpen: boolean,
): Promise<void> {
    const deadline = Date.now() + 10_000;

    while (Date.now() < deadline) {
        const open = await isPortOpenForTest(port);
        if (open === expectedOpen) {
            return;
        }

        await new Promise((resolve) => setTimeout(resolve, 100));
    }

    throw new Error(
        expectedOpen
            ? `Timed out waiting for 127.0.0.1:${port} to start listening`
            : `Timed out waiting for 127.0.0.1:${port} to stop listening`,
    );
}

function isPortOpenForTest(port: number): Promise<boolean> {
    return new Promise((resolve) => {
        const socket = net.connect({ host: "127.0.0.1", port });

        socket.once("connect", () => {
            socket.end();
            resolve(true);
        });
        socket.once("error", () => {
            resolve(false);
        });
    });
}
