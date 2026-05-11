import { describe, expect, it, vi } from "vitest";

import type { EnrichmentResult } from "@/lib/contracts";
import { enrichSeqmetaMetadata } from "@/lib/seqmeta-enrichment";
import { SeqmetaCache } from "@/lib/seqmeta-cache-core";

describe("enrichSeqmetaMetadata", () => {
    it("should parallelize enrichment lookups for multiple seqmeta values to avoid slow sequential execution", async () => {
        const cache = new SeqmetaCache();

        // Create metadata with 5 different seqmeta values
        const metadata: Record<string, string> = {
            seqmeta_sampleid: "SAMPLE-1",
            seqmeta_sample_lims: "LIMS-1",
            seqmeta_studyid: "STUDY-1",
            seqmeta_library: "LIBRARY-1",
            seqmeta_study_accession: "ACC-1",
        };

        // Mock enrichIdentifier with 100ms delay to simulate network latency
        const enrichIdentifier = vi.fn(async (value: string) => {
            await new Promise((resolve) => setTimeout(resolve, 100));

            return {
                identifier: value,
                type: "sanger_sample_id",
                graph: {},
                partial: false,
            } as EnrichmentResult;
        });

        const startTime = Date.now();

        await enrichSeqmetaMetadata(metadata, cache, enrichIdentifier);

        const duration = Date.now() - startTime;

        // If sequential: 5 values × 100ms = 500ms+
        // If parallel: 100ms+ (all concurrent)
        // Allow some overhead but should be much less than sequential
        expect(duration).toBeLessThan(300);
        expect(enrichIdentifier).toHaveBeenCalledTimes(5);
    });

    it("should handle parallel enrichment failures gracefully", async () => {
        const cache = new SeqmetaCache();

        const metadata: Record<string, string> = {
            seqmeta_sampleid: "GOOD-1",
            seqmeta_studyid: "BAD-1",
            seqmeta_library: "GOOD-2",
        };

        const enrichIdentifier = vi.fn(async (value: string) => {
            await new Promise((resolve) => setTimeout(resolve, 50));

            if (value === "BAD-1") {
                throw new Error("Upstream error");
            }

            return {
                identifier: value,
                type: "sanger_sample_id",
                graph: {},
                partial: false,
            } as EnrichmentResult;
        });

        const result = await enrichSeqmetaMetadata(
            metadata,
            cache,
            enrichIdentifier,
        );

        // Should have enrichments for GOOD-1 and GOOD-2
        expect(result.enrichments["GOOD-1"]).toBeDefined();
        expect(result.enrichments["GOOD-2"]).toBeDefined();

        // Should have error for BAD-1
        expect(result.errors["BAD-1"]).toBe("upstream_impaired");

        // Should still be fast (parallel execution)
        expect(enrichIdentifier).toHaveBeenCalledTimes(3);
    });
});
