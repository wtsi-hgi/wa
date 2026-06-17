import { spawn } from "node:child_process";
import net from "node:net";
import { fileURLToPath } from "node:url";

import { afterAll, beforeAll, describe, expect, it } from "vitest";

const stubPath = fileURLToPath(
    new URL("../e2e/seqmeta-stub.mjs", import.meta.url),
);

describe("Playwright MLWH seqmeta stub", () => {
    let baseUrl: string;
    let child: ReturnType<typeof spawn>;
    let stderr = "";

    beforeAll(async () => {
        const port = await getFreePort();
        baseUrl = `http://127.0.0.1:${port}`;
        child = spawn(process.execPath, [stubPath, String(port)], {
            stdio: ["ignore", "ignore", "pipe"],
        });
        child.stderr?.on("data", (chunk: Buffer) => {
            stderr += chunk.toString();
        });

        await waitForStub(
            baseUrl,
            () => child.exitCode,
            () => stderr,
        );
    });

    afterAll(async () => {
        await stopStub(child);
    });

    it("serves real MLWH Match JSON from /classify/:id", async () => {
        const response = await fetch(`${baseUrl}/classify/7607`);
        const body = (await response.json()) as Record<string, unknown>;

        expect(response.status).toBe(200);
        expect(body).toMatchObject({
            Canonical: "7607",
            Kind: "study_lims_id",
            Sample: null,
            Study: {
                id_study_lims: "7607",
                name: "7607",
            },
            Run: null,
            Library: null,
        });
    });

    it("classifies samples into the Sample Match field", async () => {
        const response = await fetch(`${baseUrl}/classify/7607STDY14643771`);
        const body = (await response.json()) as Record<string, unknown>;

        expect(response.status).toBe(200);
        expect(body).toMatchObject({
            Canonical: "7607STDY14643771",
            Kind: "sanger_sample_name",
            Sample: {
                sanger_id: "7607STDY14643771",
                sample_name: "7607STDY14643771",
            },
            Study: null,
            Run: null,
            Library: null,
        });
    });

    it("returns an MLWH error envelope for missing classifications", async () => {
        const response = await fetch(`${baseUrl}/classify/missing-id`);

        await expect(response.json()).resolves.toEqual({
            code: "not_found",
            message: "mlwh: identifier not found",
        });
        expect(response.status).toBe(404);
    });

    it("keeps the legacy /validate/:id compatibility response", async () => {
        const response = await fetch(`${baseUrl}/validate/7607`);
        const body = (await response.json()) as Record<string, unknown>;

        expect(response.status).toBe(200);
        expect(body).toMatchObject({
            identifier: "7607",
            type: "study_id",
            object: {
                id_study_lims: "7607",
            },
        });
    });
});

function getFreePort(): Promise<number> {
    return new Promise((resolve, reject) => {
        const server = net.createServer();

        server.once("error", reject);
        server.listen(0, "127.0.0.1", () => {
            const address = server.address();
            server.close((error) => {
                if (error) {
                    reject(error);
                    return;
                }

                if (address && typeof address === "object") {
                    resolve(address.port);
                    return;
                }

                reject(new Error("Unable to allocate a test port"));
            });
        });
    });
}

async function waitForStub(
    baseUrl: string,
    exitCode: () => number | null,
    stderr: () => string,
): Promise<void> {
    for (let attempt = 0; attempt < 50; attempt += 1) {
        if (exitCode() !== null) {
            throw new Error(`seqmeta stub exited before startup: ${stderr()}`);
        }

        try {
            const response = await fetch(`${baseUrl}/studies`);

            if (response.ok) {
                return;
            }
        } catch {
            // Retry until the stub has bound its listener.
        }

        await new Promise((resolve) => setTimeout(resolve, 50));
    }

    throw new Error(`Timed out waiting for seqmeta stub startup: ${stderr()}`);
}

async function stopStub(
    child: ReturnType<typeof spawn> | undefined,
): Promise<void> {
    if (!child || child.exitCode !== null || child.killed) {
        return;
    }

    await new Promise<void>((resolve) => {
        const timeout = setTimeout(() => {
            child.kill("SIGKILL");
            resolve();
        }, 5000);

        child.once("exit", () => {
            clearTimeout(timeout);
            resolve();
        });
        child.kill("SIGTERM");
    });
}
