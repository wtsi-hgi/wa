import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const frontendRoot = fileURLToPath(new URL("../", import.meta.url));
const repoRoot = path.resolve(frontendRoot, "..");
const seedPath = path.join(
    repoRoot,
    ".docs",
    "results-web",
    "fixtures",
    "seed.json",
);

type SeedFixture = {
    output_directory?: unknown;
    pipeline_name?: unknown;
    run_key?: unknown;
};

describe("seed fixtures", () => {
    it("keeps combined galleries sample result output directories outside retired galleries roots", () => {
        const fixtures = JSON.parse(readFileSync(seedPath, "utf8")) as unknown;

        expect(Array.isArray(fixtures)).toBe(true);

        const combinedGalleriesFixtures = (fixtures as SeedFixture[]).filter(
            (fixture) =>
                fixture.pipeline_name === "wtsi/combined-galleries-demo",
        );
        const outputDirectories = combinedGalleriesFixtures
            .map((fixture) => fixture.output_directory)
            .filter((outputDirectory): outputDirectory is string => {
                return typeof outputDirectory === "string";
            });
        const runKeys = combinedGalleriesFixtures
            .map((fixture) => fixture.run_key)
            .filter((runKey): runKey is string => {
                return typeof runKey === "string";
            });

        expect(outputDirectories).toContain(
            ".docs/results-web/fixtures/files/sibling-gallery-runs/sample-a",
        );
        expect(outputDirectories).toContain(
            ".docs/results-web/fixtures/files/sibling-gallery-runs/sample-b",
        );
        expect(runKeys).toContain("runid=88205&unique=galleries_sample_a");
        expect(runKeys).toContain("runid=88206&unique=galleries_sample_b");

        for (const outputDirectory of outputDirectories) {
            expect(outputDirectory).not.toContain("galleries-demo/sample-a");
            expect(outputDirectory).not.toContain("galleries-demo/sample-b");
            expect(outputDirectory).not.toContain(
                "combined-galleries-demo/sample-a",
            );
            expect(outputDirectory).not.toContain(
                "combined-galleries-demo/sample-b",
            );
        }
    });
});
