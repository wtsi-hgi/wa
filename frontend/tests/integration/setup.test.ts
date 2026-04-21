import { describe, expect, it } from "vitest";

import { buildResultsServerEnv } from "./setup";

describe("integration setup environment", () => {
    it("removes inherited seqmeta backend configuration before starting the results server", () => {
        const env = buildResultsServerEnv({
            HOME: "/tmp/home",
            PATH: "/usr/bin",
            WA_RESULTS_BACKEND_URL: "http://127.0.0.1:9999",
            WA_SEQMETA_BACKEND_URL: "http://127.0.0.1:3673",
        });

        expect(env).toMatchObject({
            HOME: "/tmp/home",
            PATH: "/usr/bin",
            WA_RESULTS_BACKEND_URL: "http://127.0.0.1:9999",
        });
        expect(env.WA_SEQMETA_BACKEND_URL).toBeUndefined();
    });
});