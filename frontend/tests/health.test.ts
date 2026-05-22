import { afterEach, describe, expect, it, vi } from "vitest";

const resultsJsonMock = vi.fn();

vi.mock("@/lib/backend-client", () => ({
    resultsJson: resultsJsonMock,
}));

describe("I1 health check API route", () => {
    afterEach(() => {
        vi.clearAllMocks();
        vi.resetModules();
    });

    it("returns healthy when the Go backend stats endpoint succeeds", async () => {
        resultsJsonMock.mockResolvedValue({
            total: 1,
            recent: [],
            daily: [],
            pipelines: [],
        });

        const { GET, dynamic } = await import("@/app/api/health/route");

        const response = await GET();

        expect(dynamic).toBe("force-dynamic");
        expect(resultsJsonMock).toHaveBeenCalledTimes(1);
        expect(resultsJsonMock.mock.calls[0]?.[0]).toBe(
            "/rest/v1/results/stats",
        );
        expect(resultsJsonMock.mock.calls[0]?.[1]).toMatchObject({
            safeParse: expect.any(Function),
        });
        expect(response.status).toBe(200);
        await expect(response.json()).resolves.toEqual({ status: "healthy" });
    });

    it("returns unhealthy when the Go backend stats endpoint fails", async () => {
        resultsJsonMock.mockRejectedValue(new Error("backend unavailable"));

        const { GET } = await import("@/app/api/health/route");

        const response = await GET();

        expect(resultsJsonMock).toHaveBeenCalledTimes(1);
        expect(resultsJsonMock.mock.calls[0]?.[0]).toBe(
            "/rest/v1/results/stats",
        );
        expect(resultsJsonMock.mock.calls[0]?.[1]).toMatchObject({
            safeParse: expect.any(Function),
        });
        expect(response.status).toBe(503);
        await expect(response.json()).resolves.toEqual({ status: "unhealthy" });
    });
});
