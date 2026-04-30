import { describe, expect, it, vi } from "vitest";

import type { EnrichmentResult } from "@/lib/contracts";
import { SeqmetaCache } from "@/lib/seqmeta-cache-core";
import { primeSeqmetaCache } from "@/lib/seqmeta-enrichment";

function flushMicrotasks(): Promise<void> {
    return new Promise((resolve) => {
        queueMicrotask(() => resolve());
    });
}

function buildEnrichment(identifier: string): EnrichmentResult {
    return {
        identifier,
        type: "sanger_sample_id",
        graph: {
            study: {
                id_study_lims: `STUDY-${identifier}`,
                accession_number: `ACC-${identifier}`,
            },
            library: {
                library_type: `LIB-${identifier}`,
                id_study_lims: `STUDY-${identifier}`,
            },
            sample: {
                sanger_id: `SANGER-${identifier}`,
                id_sample_lims: `LIMS-${identifier}`,
                study_accession_number: `ACC-${identifier}`,
            },
            samples: Array.from({ length: 5 }, (_, i) => ({
                sanger_id: `SANGER-${identifier}-${i}`,
                id_sample_lims: `LIMS-${identifier}-${i}`,
                id_study_lims: `STUDY-${identifier}`,
            })),
        },
        partial: false,
    } as EnrichmentResult;
}

describe("SeqmetaCache onChange coalescing", () => {
    it("invokes onChange at most once per microtask flush across many synchronous set calls", async () => {
        const onChange = vi.fn();
        const cache = new SeqmetaCache({}, onChange);

        // Simulate the storm: many sets in a single tick, mimicking
        // primeSeqmetaCacheEntry's many alias writes per enrichment, across
        // several enrichments that resolve in parallel.
        const enrichments: Record<string, EnrichmentResult | null> = {};
        for (let i = 0; i < 10; i++) {
            enrichments[`SAMPLE-${i}`] = buildEnrichment(`SAMPLE-${i}`);
        }

        primeSeqmetaCache(cache, enrichments);

        // Synchronously after many sets, listener should NOT have fired N times.
        // It should be coalesced to a single deferred call per flush.
        expect(onChange.mock.calls.length).toBeLessThanOrEqual(1);

        await flushMicrotasks();

        // After the microtask flush, listener fires exactly once with the
        // final snapshot containing every set value.
        expect(onChange).toHaveBeenCalledTimes(1);

        const snapshot = onChange.mock.calls[0]![0]!;
        for (let i = 0; i < 10; i++) {
            expect(snapshot[`SAMPLE-${i}`]).toBeDefined();
        }
    });

    it("makes set values visible synchronously via get/has/snapshot before flush", () => {
        const cache = new SeqmetaCache({}, vi.fn());
        const enrichment = buildEnrichment("X");

        cache.set("X", enrichment);

        expect(cache.has("X")).toBe(true);
        expect(cache.get("X")).toBe(enrichment);
        expect(cache.snapshot()["X"]).toBe(enrichment);
    });

    it("does not invoke onChange when a set is a no-op (equality short-circuit)", async () => {
        const enrichment = buildEnrichment("Y");
        const onChange = vi.fn();
        const cache = new SeqmetaCache({ Y: enrichment }, onChange);

        // Re-setting equal value must not schedule a flush.
        cache.set("Y", enrichment);

        await flushMicrotasks();
        await flushMicrotasks();

        expect(onChange).not.toHaveBeenCalled();
    });

    it("coalesces multiple flush windows independently", async () => {
        const onChange = vi.fn();
        const cache = new SeqmetaCache({}, onChange);

        cache.set("A", buildEnrichment("A"));
        cache.set("B", buildEnrichment("B"));
        await flushMicrotasks();
        expect(onChange).toHaveBeenCalledTimes(1);

        cache.set("C", buildEnrichment("C"));
        cache.set("D", buildEnrichment("D"));
        await flushMicrotasks();
        expect(onChange).toHaveBeenCalledTimes(2);

        const finalSnapshot = onChange.mock.calls[1]![0]!;
        expect(Object.keys(finalSnapshot).sort()).toEqual(["A", "B", "C", "D"]);
    });
});
