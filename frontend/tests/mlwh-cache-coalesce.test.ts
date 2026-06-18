import { describe, expect, it, vi } from "vitest";

import type { EnrichmentResult } from "@/lib/contracts";
import {
    buildMLWHCacheCookie,
    deserializeMLWHCacheCookie,
    serializeMLWHCacheCookie,
    MLWHCache,
} from "@/lib/mlwh-cache-core";
import { primeMLWHCache } from "@/lib/mlwh-enrichment";

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
                id_study_tmp: 42,
                id_lims: "SQSCP",
                id_study_lims: `STUDY-${identifier}`,
                name: `Study ${identifier}`,
                faculty_sponsor: "Dr Example",
                state: "active",
                accession_number: `ACC-${identifier}`,
                data_release_strategy: "managed",
                study_title: "RNA Study",
                data_access_group: "group-a",
                programme: "Transcriptomics",
                reference_genome: "GRCh38",
                ethically_approved: true,
                study_type: "Whole Genome Sequencing",
                contains_human_dna: true,
                contaminated_human_dna: false,
                study_visibility: "Always Open",
                ega_dac_accession_number: "EGAC00001",
                ega_policy_accession_number: "EGAP00001",
                data_release_timing: "Immediate",
            },
            library: {
                library_type: `LIB-${identifier}`,
                id_study_lims: `STUDY-${identifier}`,
            },
            sample: {
                sanger_id: `SANGER-${identifier}`,
                id_study_lims: `STUDY-${identifier}`,
                id_sample_lims: `LIMS-${identifier}`,
                sample_name: `Sample ${identifier}`,
                taxon_id: 9606,
                common_name: "Human",
                library_type: `LIB-${identifier}`,
                id_run: 1234,
                lane: 1,
                tag_index: 7,
                irods_path: `/seq/${identifier}`,
                study_accession_number: `ACC-${identifier}`,
                accession_number: `ERS-${identifier}`,
            },
            samples: Array.from({ length: 5 }, (_, i) => ({
                sanger_id: `SANGER-${identifier}-${i}`,
                id_sample_lims: `LIMS-${identifier}-${i}`,
                id_study_lims: `STUDY-${identifier}`,
                sample_name: `Sample ${identifier}-${i}`,
                taxon_id: 9606,
                common_name: "Human",
                library_type: `LIB-${identifier}`,
                id_run: 1234,
                lane: 1,
                tag_index: i,
                irods_path: `/seq/${identifier}-${i}`,
                study_accession_number: `ACC-${identifier}`,
                accession_number: `ERS-${identifier}-${i}`,
            })),
        },
        partial: false,
    } as EnrichmentResult;
}

describe("MLWHCache onChange coalescing", () => {
    it("invokes onChange at most once per microtask flush across many synchronous set calls", async () => {
        const onChange = vi.fn();
        const cache = new MLWHCache({}, onChange);

        // Simulate the storm: many sets in a single tick, mimicking
        // primeMLWHCacheEntry's many alias writes per enrichment, across
        // several enrichments that resolve in parallel.
        const enrichments: Record<string, EnrichmentResult | null> = {};
        for (let i = 0; i < 10; i++) {
            enrichments[`SAMPLE-${i}`] = buildEnrichment(`SAMPLE-${i}`);
        }

        primeMLWHCache(cache, enrichments);

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
        const cache = new MLWHCache({}, vi.fn());
        const enrichment = buildEnrichment("X");

        cache.set("X", enrichment);

        expect(cache.has("X")).toBe(true);
        expect(cache.get("X")).toBe(enrichment);
        expect(cache.snapshot()["X"]).toBe(enrichment);
    });

    it("does not invoke onChange when a set is a no-op (equality short-circuit)", async () => {
        const enrichment = buildEnrichment("Y");
        const onChange = vi.fn();
        const cache = new MLWHCache({ Y: enrichment }, onChange);

        // Re-setting equal value must not schedule a flush.
        cache.set("Y", enrichment);

        await flushMicrotasks();
        await flushMicrotasks();

        expect(onChange).not.toHaveBeenCalled();
    });

    it("does not stringify successful enrichment graphs when checking repeated sets", () => {
        const enrichment = {
            ...buildEnrichment("Z"),
            graph: {
                toJSON() {
                    throw new RangeError("Invalid string length");
                },
            },
        } as unknown as EnrichmentResult;
        const cache = new MLWHCache({ Z: enrichment }, vi.fn());

        expect(() => {
            cache.set("Z", { ...enrichment });
        }).not.toThrow();
    });

    it("coalesces multiple flush windows independently", async () => {
        const onChange = vi.fn();
        const cache = new MLWHCache({}, onChange);

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

describe("MLWHCache cookie persistence", () => {
    it("does not stringify successful enrichment graphs when serializing a cookie", () => {
        const oversizeEnrichment = {
            ...buildEnrichment("SAMPLE-1"),
            graph: {
                toJSON() {
                    throw new RangeError("Invalid string length");
                },
            },
        } as unknown as EnrichmentResult;

        const serialized = serializeMLWHCacheCookie({
            "SAMPLE-1": oversizeEnrichment,
            "MISSING-1": null,
        });

        expect(deserializeMLWHCacheCookie(serialized)).toEqual({
            "MISSING-1": null,
        });
    });

    it("keeps persisted cache cookies below the browser cookie budget", () => {
        const snapshot = Object.fromEntries(
            Array.from({ length: 1000 }, (_, index) => [
                `MISSING-${index.toString().padStart(4, "0")}`,
                null,
            ]),
        );

        const cookie = buildMLWHCacheCookie(snapshot);
        const persisted = deserializeMLWHCacheCookie(
            cookie.split(";")[0]!.split("=").slice(1).join("="),
        );

        expect(cookie.length).toBeLessThan(4096);
        expect(Object.keys(persisted).length).toBeGreaterThan(0);
        expect(Object.values(persisted).every((value) => value === null)).toBe(
            true,
        );
    });

    it("still reads legacy cookies that contain successful enrichments", () => {
        const enrichment = buildEnrichment("SAMPLE-2");
        const legacyCookieValue = encodeURIComponent(
            JSON.stringify({ "SAMPLE-2": enrichment }),
        );

        expect(
            deserializeMLWHCacheCookie(legacyCookieValue)["SAMPLE-2"]
                ?.identifier,
        ).toBe("SAMPLE-2");
    });
});
