/**
 * @vitest-environment jsdom
 */

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

describe("SeqmetaBadge - sample details regression (bug 4)", () => {
    beforeEach(() => {
        // Mock fetchLibrarySamples to prevent actual API calls
        vi.spyOn(enrichmentModule, "fetchLibrarySamples").mockResolvedValue([]);
    });

    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("shows complete direct metadata for sample enrichment (not just sampleid)", async () => {
        // Sample enrichment data similar to WTSI_wEMB10524782 from make dev-fixtures fixture
        const enrichment: EnrichmentResult = {
            identifier: "WTSI_TEST_SAMPLE",
            type: "sanger_sample_id",
            graph: {
                study: {
                    id_study_tmp: 1001,
                    id_lims: "SQSCP",
                    id_study_lims: "6568",
                    name: "Test Study Name",
                    faculty_sponsor: "Test Sponsor",
                    state: "active",
                    abstract: "Abstract",
                    abbreviation: "TST",
                    accession_number: "EGAS00001234567",
                    description: "Description",
                    data_release_strategy: "managed",
                    study_title: "Test Study",
                    data_access_group: "team123",
                    hmdmc_number: "19/0001",
                    programme: "Test Programme",
                    created: "2021-01-01T00:00:00Z",
                    reference_genome: "Test genome",
                    ethically_approved: true,
                    study_type: "Test Type",
                    contains_human_dna: true,
                    contaminated_human_dna: false,
                    study_visibility: "Hold",
                    ega_dac_accession_number: "",
                    ega_policy_accession_number: "",
                    data_release_timing: "delayed",
                },
                sample: {
                    id_study_lims: "6568",
                    id_sample_lims: "12345",
                    sanger_id: "WTSI_TEST_SAMPLE",
                    sample_name: "Test_Sample_Name",
                    taxon_id: 9606,
                    common_name: "human",
                    library_type: "Test Library Type",
                    id_run: 11111,
                    lane: 1,
                    tag_index: 1,
                    irods_path: "/path/to/data.cram",
                    study_accession_number: "EGAS00001234567",
                    accession_number: "EGAN00001234567",
                },
                samples: [],
                library: {
                    library_type: "Test Library Type",
                    id_study_lims: "6568",
                },
                sample_detail: {
                    sanger_id: "WTSI_TEST_SAMPLE",
                    sample_name: "Test_Sample_Name",
                    sample: {
                        id_study_lims: "6568",
                        id_sample_lims: "12345",
                        sanger_id: "WTSI_TEST_SAMPLE",
                        sample_name: "Test_Sample_Name",
                        taxon_id: 9606,
                        common_name: "human",
                        library_type: "Test Library Type",
                        id_run: 11111,
                        lane: 1,
                        tag_index: 1,
                        irods_path: "/path/to/data.cram",
                        study_accession_number: "EGAS00001234567",
                        accession_number: "EGAN00001234567",
                    },
                    lanes: [
                        { id_run: "11111", lane: "1", tag_index: 1 },
                        { id_run: "11111", lane: "2", tag_index: 1 },
                        { id_run: "22222", lane: "1", tag_index: 5 },
                    ],
                },
            },
            partial: false,
        };

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "WTSI_TEST_SAMPLE",
                enrichment,
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));
        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        const directMetadataSection = screen
            .getByTestId("seqmeta-dialog-body")
            .querySelector('[data-field-group="direct-metadata"]');
        expect(directMetadataSection).toBeTruthy();

        // Should have multiple direct metadata fields (not just sampleid)
        const directFields = within(
            directMetadataSection as HTMLElement,
        ).getAllByRole("article");
        expect(directFields.length).toBeGreaterThan(1);

        // Should show sample name
        expect(
            within(directMetadataSection as HTMLElement).getByText(
                "Test_Sample_Name",
            ),
        ).toBeTruthy();

        // Should show Sanger sample ID
        expect(
            within(directMetadataSection as HTMLElement).getByText(
                "WTSI_TEST_SAMPLE",
            ),
        ).toBeTruthy();

        // Should show Sample LIMS ID
        expect(
            within(directMetadataSection as HTMLElement).getByText("12345"),
        ).toBeTruthy();

        // Should show sample accession
        expect(
            within(directMetadataSection as HTMLElement).getByText(
                "EGAN00001234567",
            ),
        ).toBeTruthy();
    });

    it("shows exactly one parent library row in related data for sample, not multiple library rows", () => {
        const enrichment: EnrichmentResult = {
            identifier: "WTSI_TEST_SAMPLE",
            type: "sanger_sample_id",
            graph: {
                study: {
                    id_study_tmp: 1001,
                    id_lims: "SQSCP",
                    id_study_lims: "6568",
                    name: "Test Study Name",
                    faculty_sponsor: "Test Sponsor",
                    state: "active",
                    abstract: "Abstract",
                    abbreviation: "TST",
                    accession_number: "EGAS00001234567",
                    description: "Description",
                    data_release_strategy: "managed",
                    study_title: "Test Study",
                    data_access_group: "team123",
                    hmdmc_number: "19/0001",
                    programme: "Test Programme",
                    created: "2021-01-01T00:00:00Z",
                    reference_genome: "Test genome",
                    ethically_approved: true,
                    study_type: "Test Type",
                    contains_human_dna: true,
                    contaminated_human_dna: false,
                    study_visibility: "Hold",
                    ega_dac_accession_number: "",
                    ega_policy_accession_number: "",
                    data_release_timing: "delayed",
                },
                sample: {
                    id_study_lims: "6568",
                    id_sample_lims: "12345",
                    sanger_id: "WTSI_TEST_SAMPLE",
                    sample_name: "Test_Sample_Name",
                    taxon_id: 9606,
                    common_name: "human",
                    library_type: "Test Library Type",
                    id_run: 11111,
                    lane: 1,
                    tag_index: 1,
                    irods_path: "/path/to/data.cram",
                    study_accession_number: "EGAS00001234567",
                    accession_number: "EGAN00001234567",
                },
                samples: [],
                library: {
                    library_type: "Test Library Type",
                    id_study_lims: "6568",
                },
                sample_detail: {
                    sanger_id: "WTSI_TEST_SAMPLE",
                    sample_name: "Test_Sample_Name",
                    sample: {
                        id_study_lims: "6568",
                        id_sample_lims: "12345",
                        sanger_id: "WTSI_TEST_SAMPLE",
                        sample_name: "Test_Sample_Name",
                        taxon_id: 9606,
                        common_name: "human",
                        library_type: "Test Library Type",
                        id_run: 11111,
                        lane: 1,
                        tag_index: 1,
                        irods_path: "/path/to/data.cram",
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
                rawValue: "WTSI_TEST_SAMPLE",
                enrichment,
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));
        const dialogBody = screen.getByTestId("seqmeta-dialog-body");

        const relatedDataSection = dialogBody.querySelector(
            '[data-field-group="related-data"]',
        );
        expect(relatedDataSection).toBeTruthy();

        const librarySection = dialogBody.querySelector(
            '[data-field-group="library"]',
        );
        expect(librarySection).toBeTruthy();

        // Should have exactly ONE library row
        const libraryRows = dialogBody.querySelectorAll(
            '[data-seqmeta-detail-key="library"]',
        );
        expect(libraryRows.length).toBe(1);

        // Should show the library type
        expect(
            within(librarySection as HTMLElement).getByText(
                "Test Library Type",
            ),
        ).toBeTruthy();
    });

    it("shows exactly one parent study row in related data for sample, not multiple study rows", () => {
        const enrichment: EnrichmentResult = {
            identifier: "WTSI_TEST_SAMPLE",
            type: "sanger_sample_id",
            graph: {
                study: {
                    id_study_tmp: 1001,
                    id_lims: "SQSCP",
                    id_study_lims: "6568",
                    name: "Test Study Name",
                    faculty_sponsor: "Test Sponsor",
                    state: "active",
                    abstract: "Abstract",
                    abbreviation: "TST",
                    accession_number: "EGAS00001234567",
                    description: "Description",
                    data_release_strategy: "managed",
                    study_title: "Test Study",
                    data_access_group: "team123",
                    hmdmc_number: "19/0001",
                    programme: "Test Programme",
                    created: "2021-01-01T00:00:00Z",
                    reference_genome: "Test genome",
                    ethically_approved: true,
                    study_type: "Test Type",
                    contains_human_dna: true,
                    contaminated_human_dna: false,
                    study_visibility: "Hold",
                    ega_dac_accession_number: "",
                    ega_policy_accession_number: "",
                    data_release_timing: "delayed",
                },
                sample: {
                    id_study_lims: "6568",
                    id_sample_lims: "12345",
                    sanger_id: "WTSI_TEST_SAMPLE",
                    sample_name: "Test_Sample_Name",
                    taxon_id: 9606,
                    common_name: "human",
                    library_type: "Test Library Type",
                    id_run: 11111,
                    lane: 1,
                    tag_index: 1,
                    irods_path: "/path/to/data.cram",
                    study_accession_number: "EGAS00001234567",
                    accession_number: "EGAN00001234567",
                },
                samples: [],
                library: {
                    library_type: "Test Library Type",
                    id_study_lims: "6568",
                },
                sample_detail: {
                    sanger_id: "WTSI_TEST_SAMPLE",
                    sample_name: "Test_Sample_Name",
                    sample: {
                        id_study_lims: "6568",
                        id_sample_lims: "12345",
                        sanger_id: "WTSI_TEST_SAMPLE",
                        sample_name: "Test_Sample_Name",
                        taxon_id: 9606,
                        common_name: "human",
                        library_type: "Test Library Type",
                        id_run: 11111,
                        lane: 1,
                        tag_index: 1,
                        irods_path: "/path/to/data.cram",
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
                rawValue: "WTSI_TEST_SAMPLE",
                enrichment,
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));
        const dialogBody = screen.getByTestId("seqmeta-dialog-body");

        const relatedDataSection = dialogBody.querySelector(
            '[data-field-group="related-data"]',
        );
        expect(relatedDataSection).toBeTruthy();

        const studySection = dialogBody.querySelector(
            '[data-field-group="study"]',
        );
        expect(studySection).toBeTruthy();

        // Should have exactly ONE study row (not 3 separate rows for name/id/accession)
        const studyRows = dialogBody.querySelectorAll(
            '[data-seqmeta-detail-key="study"]',
        );
        expect(studyRows.length).toBe(1);

        // Should show the study name
        expect(
            within(studySection as HTMLElement).getByText("Test Study Name"),
        ).toBeTruthy();
    });

    it("shows lane identifiers in related data for sample", () => {
        const enrichment: EnrichmentResult = {
            identifier: "WTSI_TEST_SAMPLE",
            type: "sanger_sample_id",
            graph: {
                study: {
                    id_study_tmp: 1001,
                    id_lims: "SQSCP",
                    id_study_lims: "6568",
                    name: "Test Study Name",
                    faculty_sponsor: "Test Sponsor",
                    state: "active",
                    abstract: "Abstract",
                    abbreviation: "TST",
                    accession_number: "EGAS00001234567",
                    description: "Description",
                    data_release_strategy: "managed",
                    study_title: "Test Study",
                    data_access_group: "team123",
                    hmdmc_number: "19/0001",
                    programme: "Test Programme",
                    created: "2021-01-01T00:00:00Z",
                    reference_genome: "Test genome",
                    ethically_approved: true,
                    study_type: "Test Type",
                    contains_human_dna: true,
                    contaminated_human_dna: false,
                    study_visibility: "Hold",
                    ega_dac_accession_number: "",
                    ega_policy_accession_number: "",
                    data_release_timing: "delayed",
                },
                sample: {
                    id_study_lims: "6568",
                    id_sample_lims: "12345",
                    sanger_id: "WTSI_TEST_SAMPLE",
                    sample_name: "Test_Sample_Name",
                    taxon_id: 9606,
                    common_name: "human",
                    library_type: "Test Library Type",
                    id_run: 11111,
                    lane: 1,
                    tag_index: 1,
                    irods_path: "/path/to/data.cram",
                    study_accession_number: "EGAS00001234567",
                    accession_number: "EGAN00001234567",
                },
                samples: [],
                library: {
                    library_type: "Test Library Type",
                    id_study_lims: "6568",
                },
                sample_detail: {
                    sanger_id: "WTSI_TEST_SAMPLE",
                    sample_name: "Test_Sample_Name",
                    sample: {
                        id_study_lims: "6568",
                        id_sample_lims: "12345",
                        sanger_id: "WTSI_TEST_SAMPLE",
                        sample_name: "Test_Sample_Name",
                        taxon_id: 9606,
                        common_name: "human",
                        library_type: "Test Library Type",
                        id_run: 11111,
                        lane: 1,
                        tag_index: 1,
                        irods_path: "/path/to/data.cram",
                        study_accession_number: "EGAS00001234567",
                        accession_number: "EGAN00001234567",
                    },
                    lanes: [
                        { id_run: "11111", lane: "1", tag_index: 1 },
                        { id_run: "11111", lane: "2", tag_index: 2 },
                        { id_run: "22222", lane: "3", tag_index: 5 },
                    ],
                },
            },
            partial: false,
        };

        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_sampleid",
                rawValue: "WTSI_TEST_SAMPLE",
                enrichment,
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));
        const dialogBody = screen.getByTestId("seqmeta-dialog-body");

        const relatedDataSection = dialogBody.querySelector(
            '[data-field-group="related-data"]',
        );
        expect(relatedDataSection).toBeTruthy();

        const lanesSection = dialogBody.querySelector(
            '[data-field-group="lanes"]',
        );
        expect(lanesSection).toBeTruthy();

        // Should show all three lane identifiers
        expect(
            within(lanesSection as HTMLElement).getByText("11111_1#1"),
        ).toBeTruthy();
        expect(
            within(lanesSection as HTMLElement).getByText("11111_2#2"),
        ).toBeTruthy();
        expect(
            within(lanesSection as HTMLElement).getByText("22222_3#5"),
        ).toBeTruthy();

        // Should have copy and filter buttons for each lane
        const laneRows = dialogBody.querySelectorAll(
            '[data-seqmeta-detail-key="lane"]',
        );
        expect(laneRows.length).toBe(3);
    });
});
