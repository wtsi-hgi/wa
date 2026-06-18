import fs from "node:fs/promises";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const lockDirectory = path.resolve(process.cwd(), "..", ".tmp", "agent");
const lockPath = path.join(lockDirectory, "e2e-seqmeta-heavy.lock");
const staleLockMs = 4 * 60 * 1000;
const acquireTimeoutMs = 3 * 60 * 1000;

export async function withSeqmetaHeavyE2ELock<T>(
    run: () => Promise<T>,
): Promise<T> {
    const release = await acquireSeqmetaHeavyE2ELock();

    try {
        return await run();
    } finally {
        await release();
    }
}

async function acquireSeqmetaHeavyE2ELock(): Promise<() => Promise<void>> {
    await fs.mkdir(lockDirectory, { recursive: true });

    const startedAt = Date.now();

    for (;;) {
        try {
            const handle = await fs.open(lockPath, "wx");
            await handle.writeFile(
                JSON.stringify({
                    createdAt: new Date().toISOString(),
                    pid: process.pid,
                }),
                "utf8",
            );
            await handle.close();

            return async () => {
                await fs.rm(lockPath, { force: true });
            };
        } catch (error) {
            const code = (error as NodeJS.ErrnoException).code;

            if (code !== "EEXIST") {
                throw error;
            }

            await removeStaleSeqmetaLock();

            if (Date.now() - startedAt > acquireTimeoutMs) {
                throw new Error(
                    `Timed out waiting for seqmeta e2e lock at ${lockPath}`,
                );
            }

            await delay(100);
        }
    }
}

async function removeStaleSeqmetaLock(): Promise<void> {
    try {
        const stat = await fs.stat(lockPath);

        if (Date.now() - stat.mtimeMs > staleLockMs) {
            await fs.rm(lockPath, { force: true });
        }
    } catch (error) {
        const code = (error as NodeJS.ErrnoException).code;

        if (code !== "ENOENT") {
            throw error;
        }
    }
}
