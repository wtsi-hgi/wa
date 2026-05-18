/**
 * @vitest-environment jsdom
 */
import { createElement } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import { ResultMetadataEnrichment } from "@/components/result-metadata-enrichment";
import { SeqmetaCacheContext } from "@/lib/seqmeta-cache";
import { SeqmetaCache } from "@/lib/seqmeta-cache-core";

// Mock the enrichIdentifier action
vi.mock("@/app/(results)/actions", () => ({
    enrichIdentifier: vi
        .fn()
        .mockRejectedValue(new Error("Backend unavailable")),
    enrichIdentifiers: vi
        .fn()
        .mockRejectedValue(new Error("Backend unavailable")),
}));

describe("seqmeta enrichment error recovery", () => {
    test("loading state clears when enrichment fails", async () => {
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

        // Wait for enrichment to fail and loading to clear
        // The .catch() handler should clear loading state even on failure
        await waitFor(
            () => {
                expect(
                    screen.queryByLabelText("loading enrichment"),
                ).toBeNull();
            },
            { timeout: 3000 },
        );

        // Should show error indicator
        const errorIndicator = screen.queryByLabelText(
            "enrichment backend impaired",
        );
        expect(errorIndicator).toBeTruthy();
    });

    test("dialog doesnt show Looking up indefinitely after error", async () => {
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

        // Wait for loading to clear (after enrichment error)
        await waitFor(
            () => {
                expect(
                    screen.queryByLabelText("loading enrichment"),
                ).toBeNull();
            },
            { timeout: 3000 },
        );

        // Click the badge to open dialog (using getAllByTestId since there might be multiple)
        const badges = screen.getAllByTestId("seqmeta-badge-trigger");
        fireEvent.click(badges[0]);

        // Dialog should open
        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Dialog should NOT show "Looking up" because loading is false
        const lookingUpText = screen.queryByText(/Looking up/i);
        expect(lookingUpText).toBeNull();

        // Should show error message instead
        const errorText = screen.queryByText(/unavailable/i);
        expect(errorText).toBeTruthy();
    });
});
