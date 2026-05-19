/**
 * @vitest-environment jsdom
 */

import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

import { createElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
    within,
} from "@testing-library/react";

import { SeqmetaBadge } from "@/components/seqmeta-badge";
import type { EnrichmentResult } from "@/lib/contracts";
import * as enrichmentModule from "@/lib/seqmeta-enrichment";

describe("SeqmetaBadge sample-name key repro", () => {
    beforeEach(() => {
        vi.spyOn(enrichmentModule, "fetchLibrarySamples").mockResolvedValue([]);
    });

    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("exposes direct sample name metadata as seqmeta_sample_name", async () => {
        const enrichment: EnrichmentResult = {
            identifier: "WTSI_SAMPLE_001",
            type: "sanger_sample_id",
            graph: {
                sample: {
                    id_study_lims: "6568",
                    id_sample_lims: "12345",
                    sanger_id: "WTSI_SAMPLE_001",
                    sample_name: "Test_Sample_Name",
                    supplier_name: "Supplier_Sample_Name",
                    taxon_id: 9606,
                    common_name: "human",
                    library_type: "RNA",
                    id_run: 11111,
                    lane: 1,
                    tag_index: 1,
                    irods_path: "/seq/11111/1.cram",
                    study_accession_number: "EGAS00001234567",
                    accession_number: "EGAN00001234567",
                },
                sample_detail: {
                    sanger_id: "WTSI_SAMPLE_001",
                    sample_name: "Test_Sample_Name",
                    sample: {
                        id_study_lims: "6568",
                        id_sample_lims: "12345",
                        sanger_id: "WTSI_SAMPLE_001",
                        sample_name: "Test_Sample_Name",
                        supplier_name: "Supplier_Sample_Name",
                        taxon_id: 9606,
                        common_name: "human",
                        library_type: "RNA",
                        id_run: 11111,
                        lane: 1,
                        tag_index: 1,
                        irods_path: "/seq/11111/1.cram",
                        study_accession_number: "EGAS00001234567",
                        accession_number: "EGAN00001234567",
                    },
                    lanes: [{ id_run: "11111", lane: "1", tag_index: 1 }],
                },
            },
            partial: false,
        };

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "WTSI_SAMPLE_001",
                enrichment,
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));
        await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());

        const directMetadataSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="direct-metadata"]');
        expect(directMetadataSection).toBeTruthy();

        const sampleNameRow = within(directMetadataSection as HTMLElement)
            .getByText("Test_Sample_Name")
            .closest("[data-seqmeta-detail-key]");
        expect(sampleNameRow).toBeTruthy();

        const copyButton = within(sampleNameRow as HTMLElement).getByRole(
            "button",
            { name: "Copy seqmeta_sample_name" },
        );
        const filterLink = within(sampleNameRow as HTMLElement).getByRole(
            "link",
            { name: "Send seqmeta_sample_name to search filter" },
        );

        const evidence = {
            description:
                "Direct sample-name metadata is rendered with the precise seqmeta_sample_name key.",
            rowKey: sampleNameRow?.getAttribute("data-seqmeta-detail-key"),
            label: sampleNameRow?.querySelector(
                '[data-testid="seqmeta-direct-metadata-label"]',
            )?.textContent,
            value: sampleNameRow?.textContent,
            copyAriaLabel: copyButton.getAttribute("aria-label"),
            filterAriaLabel: filterLink.getAttribute("aria-label"),
            filterHref: filterLink.getAttribute("href"),
        };
        const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
        await mkdir(evidenceDir, { recursive: true });
        await writeFile(
            path.join(evidenceDir, "seqmeta-sample-name-key-repro.json"),
            `${JSON.stringify(evidence, null, 2)}\n`,
        );

        expect(evidence.rowKey).toBe("seqmeta_sample_name");
        expect(evidence.copyAriaLabel).toBe("Copy seqmeta_sample_name");
        expect(evidence.filterAriaLabel).toBe(
            "Send seqmeta_sample_name to search filter",
        );
        expect(evidence.filterHref).toBe(
            "/?seqmeta_sample_name=Test_Sample_Name",
        );
    });
});
