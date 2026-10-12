// Read the stack.env that launch.sh wrote into a run dir.
//
// stack.env is shell-quoted with printf %q, so bash parses it rather than
// this module reimplementing %q unquoting. Only PATH is inherited, so every
// value comes from stack.env (plus bash's own PWD and SHLVL).

import { execFileSync } from "node:child_process";
import path from "node:path";

export function readStackEnv(run) {
    return Object.fromEntries(
        execFileSync(
            "bash",
            [
                "-c",
                'set -a; . "$1"; exec env -0',
                "bash",
                path.join(run, "stack.env"),
            ],
            {
                encoding: "utf8",
                env: { PATH: process.env.PATH },
            },
        )
            .split("\0")
            .filter((entry) => entry.includes("="))
            .map((entry) => [
                entry.slice(0, entry.indexOf("=")),
                entry.slice(entry.indexOf("=") + 1),
            ]),
    );
}
