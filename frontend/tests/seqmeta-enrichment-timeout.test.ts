/**
 * @vitest-environment jsdom
 */
import { createElement } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { ResultMetadataEnrichment } from "@/components/result-metadata-enrichment";
import { SeqmetaCacheContext } from "@/lib/seqmeta-cache";
import { SeqmetaCache } from "@/lib/seqmeta-cache-core";

// Mock enrichIdentifier to return a promise that never resolves (hanging request)
vi.mock("@/app/(results)/actions", () => ({
    enrichIdentifier: vi.fn().mockReturnValue(
        new Promise<never>(() => {
            // Never resolves, never rejects - simulates a hanging network request
        }),
    ),
}));

describe("seqmeta enrichment timeout handling", () => {
    beforeEach(() => {
        // Ensure we're using real timers so setTimeout in withTimeout works
        vi.useRealTimers();
    });

    test("loading state clears within 6 seconds when enrichment hangs", async () => {
        const cache = new SeqmetaCache();
        const metadata = {
            seqmeta_sampleid: "SANG5993",
        };

        render(
            createElement(
                SeqmetaCacheContext.Provider,
                { value: cache },
                createElement(ResultMetadataEnrichment, { metadata }),
            ),
        );

        // Initially shows loading state
        expect(screen.getByLabelText("loading enrichment")).toBeTruthy();

        // Wait for timeout to trigger and loading to clear
        // Should timeout within 5s, we give it 6s to be safe
        await waitFor(
            () => {
                expect(
                    screen.queryByLabelText("loading enrichment"),
                ).toBeNull();
            },
            { timeout: 6000 },
        );

        // Should show upstream_impaired error indicator
        const errorIndicator = screen.queryByLabelText(
            "enrichment backend impaired",
        );
        expect(errorIndicator).toBeTruthy();
    });

    test("dialog stops showing Looking up within 6 seconds when enrichment hangs", async () => {
        const cache = new SeqmetaCache();
        const metadata = {
            seqmeta_sampleid: "SANG5993",
        };

        render(
            createElement(
                SeqmetaCacheContext.Provider,
                { value: cache },
                createElement(ResultMetadataEnrichment, { metadata }),
            ),
        );

        // Wait for timeout to trigger and loading to clear (within 6s)
        await waitFor(
            () => {
                expect(
                    screen.queryByLabelText("loading enrichment"),
                ).toBeNull();
            },
            { timeout: 6000 },
        );

        // Click the badge to open dialog
        const badges = screen.getAllByTestId("seqmeta-badge-trigger");
        fireEvent.click(badges[0]);

        // Dialog should open
        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Dialog should NOT show "Looking up" because loading timed out
        const lookingUpText = screen.queryByText(/Looking up/i);
        expect(lookingUpText).toBeNull();

        // Should show error message instead
        const errorText = screen.queryByText(/unavailable/i);
        expect(errorText).toBeTruthy();
    });

    test("parallel enrichment requests all timeout independently", async () => {
        const cache = new SeqmetaCache();
        const metadata = {
            seqmeta_sampleid: "SANG5993",
            seqmeta_studyid: "6568",
            seqmeta_library: "RNA",
        };

        render(
            createElement(
                SeqmetaCacheContext.Provider,
                { value: cache },
                createElement(ResultMetadataEnrichment, { metadata }),
            ),
        );

        // First enrichment (studyid) times out after 5s, then parallel
        // enrichments (sampleid, library) start and time out after another 5s.
        // Total time: 10s + buffer = 11s
        await waitFor(
            () => {
                expect(
                    screen.queryAllByLabelText("loading enrichment"),
                ).toHaveLength(0);
            },
            { timeout: 11000 },
        );

        // All values should show upstream_impaired
        const errorIndicators = screen.queryAllByLabelText(
            "enrichment backend impaired",
        );
        expect(errorIndicators.length).toBeGreaterThan(0);
    });
});
