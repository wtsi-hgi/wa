import { spawnSync } from "node:child_process";
import { realpathSync } from "node:fs";
import { fileURLToPath } from "node:url";

// tests/scaffold.test.ts runs `next build`, so it runs after the main suite in
// its own vitest invocation with a single fork rather than alongside it.
const scaffoldTest = "tests/scaffold.test.ts";

/**
 * Returns the vitest argument lists that `pnpm test` runs, in order. pnpm
 * appends script arguments to the end of the whole command and forwards a
 * literal "--", which vitest treats as the end of file filters; dispatching
 * here routes each file filter to the run that owns it. Arguments starting
 * with "-" are passed to every run, so options taking a value must use the
 * "--option=value" form.
 */
export function vitestRuns(args: readonly string[]): string[][] {
    const rest = args.filter((arg) => arg !== "--");
    const flags = rest.filter((arg) => arg.startsWith("-"));
    const filters = rest.filter((arg) => !arg.startsWith("-"));
    const scaffoldFilters = filters.filter((arg) => arg.includes("scaffold"));
    const mainFilters = filters.filter((arg) => !arg.includes("scaffold"));

    const runs: string[][] = [];

    if (filters.length === 0 || mainFilters.length > 0) {
        runs.push(["run", "--exclude", scaffoldTest, ...mainFilters, ...flags]);
    }

    if (filters.length === 0 || scaffoldFilters.length > 0) {
        runs.push([
            "run",
            "--config",
            "vitest.custom.config.ts",
            scaffoldTest,
            "--poolOptions.forks.singleFork",
            ...flags,
        ]);
    }

    return runs;
}

function main(): void {
    for (const run of vitestRuns(process.argv.slice(2))) {
        const result = spawnSync("vitest", run, { stdio: "inherit" });

        if (result.error) {
            throw result.error;
        }

        if (result.status !== 0) {
            process.exit(result.status ?? 1);
        }
    }
}

if (realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
    main();
}
