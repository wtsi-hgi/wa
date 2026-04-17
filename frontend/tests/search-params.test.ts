import { describe, expect, it } from "vitest";

import { buildSearchQuery, parseSearchFilters } from "@/lib/search-params";

describe("K1 search parameter utilities", () => {
    it("parses repeated query parameters into grouped OR filters", () => {
        const filters = parseSearchFilters(
            new URLSearchParams("user=alice&user=bob&pipeline_name=nf"),
        );

        expect(filters).toEqual({
            user: ["alice", "bob"],
            pipeline_name: ["nf"],
        });
    });

    it("builds repeated query parameters for multi-value filters", () => {
        const query = buildSearchQuery({
            user: ["alice", "bob"],
        });

        expect(query.toString()).toBe("user=alice&user=bob");
    });

    it("returns an empty query string for empty filters", () => {
        const query = buildSearchQuery({});

        expect(query.toString()).toBe("");
    });

    it("preserves metadata-style filter keys when parsing", () => {
        const filters = parseSearchFilters(
            new URLSearchParams("meta_library=exon&seqmeta_sampleid=SANG1"),
        );

        expect(filters).toEqual({
            meta_library: ["exon"],
            seqmeta_sampleid: ["SANG1"],
        });
    });
});
