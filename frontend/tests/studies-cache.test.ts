import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Study } from "@/lib/contracts";

const mlwhJsonMock = vi.fn();

vi.mock("@/lib/backend-client", async () => {
    const actual = await vi.importActual<typeof import("@/lib/backend-client")>(
        "@/lib/backend-client",
    );

    return {
        ...actual,
        mlwhJson: mlwhJsonMock,
    };
});

const studiesFixture: Study[] = [
    { id_study_lims: "6568", name: "RNA Seq" },
    { id_study_lims: "7001", name: "Cancer Panel" },
    { id_study_lims: "8123", name: "Genome Build" },
];

const originalTtl = process.env.WA_STUDIES_CACHE_TTL_SECONDS;

async function loadStudiesCache() {
    return import("@/lib/studies-cache");
}

describe("K3 studies cache", () => {
    beforeEach(() => {
        vi.resetModules();
        vi.useFakeTimers();
        vi.setSystemTime(new Date("2026-04-16T12:00:00Z"));
        mlwhJsonMock.mockReset();
        delete process.env.WA_STUDIES_CACHE_TTL_SECONDS;
    });

    afterEach(async () => {
        try {
            const { resetStudiesCache } = await loadStudiesCache();
            resetStudiesCache();
        } catch {
            // Module may not exist yet while the test is driving implementation.
        }

        if (originalTtl === undefined) {
            delete process.env.WA_STUDIES_CACHE_TTL_SECONDS;
        } else {
            process.env.WA_STUDIES_CACHE_TTL_SECONDS = originalTtl;
        }

        vi.useRealTimers();
    });

    it("returns cached studies within the configured TTL and hits MLWH once", async () => {
        process.env.WA_STUDIES_CACHE_TTL_SECONDS = "60";
        mlwhJsonMock.mockResolvedValue(studiesFixture);

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(59_000);
        await expect(getStudies()).resolves.toEqual(studiesFixture);

        expect(mlwhJsonMock).toHaveBeenCalledTimes(1);
        expect(mlwhJsonMock).toHaveBeenCalledWith(
            "/studies",
            expect.anything(),
        );
    });

    it("re-fetches once the cache age exceeds the configured TTL", async () => {
        process.env.WA_STUDIES_CACHE_TTL_SECONDS = "1";
        mlwhJsonMock.mockResolvedValue(studiesFixture);

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(1_001);
        await expect(getStudies()).resolves.toEqual(studiesFixture);

        expect(mlwhJsonMock).toHaveBeenCalledTimes(2);
    });

    it("uses the default TTL of 300 seconds when the env var is unset", async () => {
        mlwhJsonMock.mockResolvedValue(studiesFixture);

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(299_000);
        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(2_000);
        await expect(getStudies()).resolves.toEqual(studiesFixture);

        expect(mlwhJsonMock).toHaveBeenCalledTimes(2);
    });

    it("throws on an initial MLWH failure and leaves the cache empty", async () => {
        process.env.WA_STUDIES_CACHE_TTL_SECONDS = "60";
        mlwhJsonMock
            .mockRejectedValueOnce(new Error("mlwh unavailable"))
            .mockResolvedValueOnce(studiesFixture);

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).rejects.toThrow("mlwh unavailable");
        await expect(getStudies()).resolves.toEqual(studiesFixture);

        expect(mlwhJsonMock).toHaveBeenCalledTimes(2);
    });

    it("does not serve stale studies when a refresh fails after TTL expiry", async () => {
        process.env.WA_STUDIES_CACHE_TTL_SECONDS = "1";
        mlwhJsonMock
            .mockResolvedValueOnce(studiesFixture)
            .mockRejectedValueOnce(new Error("refresh failed"));

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(1_001);
        await expect(getStudies()).rejects.toThrow("refresh failed");

        expect(mlwhJsonMock).toHaveBeenCalledTimes(2);
    });
});

describe("fetchStudies server action", () => {
    beforeEach(() => {
        vi.resetModules();
    });

    afterEach(() => {
        vi.unstubAllEnvs();
    });

    it("delegates to getStudies", async () => {
        const getStudiesMock = vi.fn().mockResolvedValue(studiesFixture);

        vi.doMock("@/lib/studies-cache", () => ({
            getStudies: getStudiesMock,
            resetStudiesCache: vi.fn(),
        }));

        const { fetchStudies } = await import("@/app/(results)/actions");

        await expect(fetchStudies()).resolves.toEqual(studiesFixture);
        expect(getStudiesMock).toHaveBeenCalledTimes(1);
    });
});
