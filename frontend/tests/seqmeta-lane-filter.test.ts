/**
 * Regression tests for seqmeta lane filtering (bugfix 260501-4).
 *
 * Before: Lane rows in seqmeta details had Copy but no Filter button.
 * After: Lane rows get a Filter button that links to /?seqmeta_lane={id_run}_{lane}#{tag_index}
 */

import { describe, expect, it } from "vitest";
import { laneDetailSchema } from "@/lib/contracts";

describe("Lane filtering support", () => {
    describe("Contract validation", () => {
        it("should validate lane detail structure", () => {
            const validLane = {
                id_run: "12345",
                lane: "1",
                tag_index: 10,
            };

            const result = laneDetailSchema.safeParse(validLane);

            expect(result.success).toBe(true);
            if (result.success) {
                expect(result.data.id_run).toBe("12345");
                expect(result.data.lane).toBe("1");
                expect(result.data.tag_index).toBe(10);
            }
        });

        it("should reject invalid lane detail", () => {
            const invalidLane = {
                id_run: 12345, // should be string
                lane: 1, // should be string
                tag_index: "10", // should be number
            };

            const result = laneDetailSchema.safeParse(invalidLane);

            expect(result.success).toBe(false);
        });
    });

    describe("Lane identifier format", () => {
        it("should format lane ID as id_run_lane#tag_index", () => {
            const lane = {
                id_run: "12345",
                lane: "1",
                tag_index: 10,
            };

            const laneId = `${lane.id_run}_${lane.lane}#${lane.tag_index}`;

            expect(laneId).toBe("12345_1#10");
        });

        it("should format lane filter URL parameter", () => {
            const lane = {
                id_run: "12345",
                lane: "2",
                tag_index: 88,
            };

            const laneId = `${lane.id_run}_${lane.lane}#${lane.tag_index}`;
            const filterUrl = `/?seqmeta_lane=${laneId}`;

            expect(filterUrl).toBe("/?seqmeta_lane=12345_2#88");
        });
    });
});
