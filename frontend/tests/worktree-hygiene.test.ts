import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const frontendRoot = fileURLToPath(new URL("../", import.meta.url));
const repoRoot = path.resolve(frontendRoot, "..");

describe("worktree hygiene", () => {
    it("keeps Next's generated next-env.d.ts ignored and untracked", () => {
        const gitignore = readFileSync(
            path.join(repoRoot, ".gitignore"),
            "utf8",
        );

        expect(gitignore).toContain("frontend/next-env.d.ts");

        const ignoredPath = execFileSync(
            "git",
            ["check-ignore", "frontend/next-env.d.ts"],
            {
                cwd: repoRoot,
                encoding: "utf8",
            },
        ).trim();

        expect(ignoredPath).toBe("frontend/next-env.d.ts");

        expect(() =>
            execFileSync(
                "git",
                ["ls-files", "--error-unmatch", "frontend/next-env.d.ts"],
                {
                    cwd: repoRoot,
                    encoding: "utf8",
                    stdio: "pipe",
                },
            ),
        ).toThrow();
    });
});
