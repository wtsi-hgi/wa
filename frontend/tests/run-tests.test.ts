import { describe, expect, it } from "vitest";

import { vitestRuns } from "../scripts/run-tests.mjs";

const mainRun = ["run", "--exclude", "tests/scaffold.test.ts"];
const scaffoldRun = [
    "run",
    "--config",
    "vitest.custom.config.ts",
    "tests/scaffold.test.ts",
    "--poolOptions.forks.singleFork",
];

describe("pnpm test argument dispatch", () => {
    it("runs the main suite then the scaffold build test when given no arguments", () => {
        expect(vitestRuns([])).toEqual([mainRun, scaffoldRun]);
    });

    it.each([
        ["after pnpm's forwarded --", ["--", "tests/health.test.ts"]],
        ["without --", ["tests/health.test.ts"]],
    ])("runs only the named file %s", (_label, args) => {
        expect(vitestRuns(args)).toEqual([
            [...mainRun, "tests/health.test.ts"],
        ]);
    });

    it("runs several named files in one main run", () => {
        expect(
            vitestRuns([
                "--",
                "tests/health.test.ts",
                "tests/contracts.test.ts",
            ]),
        ).toEqual([
            [...mainRun, "tests/health.test.ts", "tests/contracts.test.ts"],
        ]);
    });

    it("runs a named scaffold test only in its single-fork run", () => {
        expect(vitestRuns(["--", "tests/scaffold.test.ts"])).toEqual([
            scaffoldRun,
        ]);
    });

    it("splits named main and scaffold files between their runs", () => {
        expect(
            vitestRuns(["tests/scaffold.test.ts", "tests/health.test.ts"]),
        ).toEqual([[...mainRun, "tests/health.test.ts"], scaffoldRun]);
    });

    it("passes options to every run without treating them as file filters", () => {
        expect(vitestRuns(["--", "--reporter=verbose"])).toEqual([
            [...mainRun, "--reporter=verbose"],
            [...scaffoldRun, "--reporter=verbose"],
        ]);
    });
});
