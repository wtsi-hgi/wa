import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { BackendRequestError } from "@/lib/backend-client";
import type { EnrichmentResult } from "@/lib/contracts";
import { SeqmetaCache } from "@/lib/seqmeta-cache-core";
import { enrichSeqmetaMetadata } from "@/lib/seqmeta-enrichment";

describe("enrichSeqmetaMetadata does not impose an artificial timeout", () => {
    beforeEach(() => {
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("no artificial timeout — slow 404 is classified as not_found", async () => {
        const cache = new SeqmetaCache();
        const metadata: Record<string, string> = {
            seqmeta_studyid: "9999999",
        };

        const enrichIdentifier = vi.fn(async (_value: string) => {
            await new Promise((resolve) => setTimeout(resolve, 10_000));
            throw new BackendRequestError(404, { detail: "not found" });
        });

        const promise = enrichSeqmetaMetadata(
            metadata,
            cache,
            enrichIdentifier,
        );

        await vi.advanceTimersByTimeAsync(10_000);
        const state = await promise;

        expect(state.errors["9999999"]).toBe("not_found");
        expect(state.enrichments["9999999"]).toBeNull();
    });

    it("no artificial timeout — slow non-404 backend failure is classified as upstream_impaired", async () => {
        const cache = new SeqmetaCache();
        const metadata: Record<string, string> = {
            seqmeta_studyid: "boom",
        };

        const enrichIdentifier = vi.fn(async (_value: string) => {
            await new Promise((resolve) => setTimeout(resolve, 10_000));
            throw new BackendRequestError(503, { detail: "down" });
        });

        const promise = enrichSeqmetaMetadata(
            metadata,
            cache,
            enrichIdentifier,
        );

        await vi.advanceTimersByTimeAsync(10_000);
        const state = await promise;

        expect(state.errors["boom"]).toBe("upstream_impaired");
    });

    it("no artificial timeout — slow successful enrichment resolves without being aborted", async () => {
        const cache = new SeqmetaCache();
        const metadata: Record<string, string> = {
            seqmeta_studyid: "3361",
        };

        const enrichIdentifier = vi.fn(async (value: string) => {
            await new Promise((resolve) => setTimeout(resolve, 10_000));
            return {
                identifier: value,
                type: "study_id",
                graph: {
                    study: {
                        id_study_lims: value,
                        name: "IHTP_ISC_IBDCA_Edinburgh",
                    },
                },
                partial: false,
            } satisfies EnrichmentResult;
        });

        const promise = enrichSeqmetaMetadata(
            metadata,
            cache,
            enrichIdentifier,
        );

        await vi.advanceTimersByTimeAsync(10_000);
        const state = await promise;

        expect(state.errors["3361"]).toBeUndefined();
        expect(state.enrichments["3361"]).not.toBeNull();
        expect(state.enrichments["3361"]?.identifier).toBe("3361");
    });
});
