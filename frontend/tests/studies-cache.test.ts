import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Study } from "@/lib/contracts";

const seqmetaJsonMock = vi.fn();

vi.mock("@/lib/backend-client", async () => {
    const actual = await vi.importActual<typeof import("@/lib/backend-client")>(
        "@/lib/backend-client",
    );

    return {
        ...actual,
        seqmetaJson: seqmetaJsonMock,
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
        seqmetaJsonMock.mockReset();
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

    it("returns cached studies within the configured TTL and hits seqmeta once", async () => {
        process.env.WA_STUDIES_CACHE_TTL_SECONDS = "60";
        seqmetaJsonMock.mockResolvedValue(studiesFixture);

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(59_000);
        await expect(getStudies()).resolves.toEqual(studiesFixture);

        expect(seqmetaJsonMock).toHaveBeenCalledTimes(1);
    });

    it("re-fetches once the cache age exceeds the configured TTL", async () => {
        process.env.WA_STUDIES_CACHE_TTL_SECONDS = "1";
        seqmetaJsonMock.mockResolvedValue(studiesFixture);

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(1_001);
        await expect(getStudies()).resolves.toEqual(studiesFixture);

        expect(seqmetaJsonMock).toHaveBeenCalledTimes(2);
    });

    it("uses the default TTL of 300 seconds when the env var is unset", async () => {
        seqmetaJsonMock.mockResolvedValue(studiesFixture);

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(299_000);
        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(2_000);
        await expect(getStudies()).resolves.toEqual(studiesFixture);

        expect(seqmetaJsonMock).toHaveBeenCalledTimes(2);
    });

    it("throws on an initial seqmeta failure and leaves the cache empty", async () => {
        process.env.WA_STUDIES_CACHE_TTL_SECONDS = "60";
        seqmetaJsonMock
            .mockRejectedValueOnce(new Error("seqmeta unavailable"))
            .mockResolvedValueOnce(studiesFixture);

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).rejects.toThrow("seqmeta unavailable");
        await expect(getStudies()).resolves.toEqual(studiesFixture);

        expect(seqmetaJsonMock).toHaveBeenCalledTimes(2);
    });

    it("does not serve stale studies when a refresh fails after TTL expiry", async () => {
        process.env.WA_STUDIES_CACHE_TTL_SECONDS = "1";
        seqmetaJsonMock
            .mockResolvedValueOnce(studiesFixture)
            .mockRejectedValueOnce(new Error("refresh failed"));

        const { getStudies } = await loadStudiesCache();

        await expect(getStudies()).resolves.toEqual(studiesFixture);
        vi.advanceTimersByTime(1_001);
        await expect(getStudies()).rejects.toThrow("refresh failed");

        expect(seqmetaJsonMock).toHaveBeenCalledTimes(2);
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
