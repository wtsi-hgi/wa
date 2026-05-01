/**
 * @vitest-environment jsdom
 */

/**
 * Regression test for bugfix 260501-3:
 * Library type enrichment should show only the study that samples belong to,
 * not all studies globally that use that library type.
 */

import { createElement } from "react";
import {
    cleanup,
    fireEvent,
    render,
    screen,
    waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

afterEach(cleanup);

describe("seqmeta-badge library type study filtering", () => {
    it("shows only the single study when all samples belong to one study, even when enrichment returns multiple studies globally", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        // Simulate what Saga returns: multiple studies globally for "Chromium single cell 3 prime v3"
        // but all samples in this enrichment belong to study 5631
        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_library",
                rawValue: "Chromium single cell 3 prime v3",
                enrichment: {
                    identifier: "Chromium single cell 3 prime v3",
                    type: "library_type",
                    graph: {
                        // Backend returns 4 studies globally
                        studies: [
                            {
                                id_study_tmp: 1,
                                id_lims: "SQSCP",
                                id_study_lims: "5631",
                                name: "Pilot_study_of_dissociation_methods_for_human_gut_tissues",
                                faculty_sponsor: "Dr Test",
                                state: "active",
                                abstract: "",
                                abbreviation: "PSDM",
                                accession_number: "EGAS00001003420",
                                description: "Pilot study",
                                data_release_strategy: "managed",
                                study_title: "Pilot Study",
                                data_access_group: "hgi",
                                hmdmc_number: "",
                                programme: "Human Cell Atlas",
                                created: "2019-01-01T00:00:00Z",
                                reference_genome: "GRCh38",
                                ethically_approved: true,
                                study_type: "Transcriptome Analysis",
                                contains_human_dna: true,
                                contaminated_human_dna: false,
                                study_visibility: "Hold",
                                ega_dac_accession_number: null,
                                ega_policy_accession_number: null,
                                data_release_timing: "standard",
                            },
                            {
                                id_study_tmp: 2,
                                id_lims: "SQSCP",
                                id_study_lims: "5819",
                                name: "OTARscRNA",
                                faculty_sponsor: "Dr Other",
                                state: "active",
                                abstract: "",
                                abbreviation: "OTAR",
                                accession_number: "EGAS00001003647",
                                description: "Another study",
                                data_release_strategy: "managed",
                                study_title: "OTAR Study",
                                data_access_group: "hgi",
                                hmdmc_number: "",
                                programme: "OTAR",
                                created: "2019-01-01T00:00:00Z",
                                reference_genome: "GRCh38",
                                ethically_approved: true,
                                study_type: "Transcriptome Analysis",
                                contains_human_dna: true,
                                contaminated_human_dna: false,
                                study_visibility: "Hold",
                                ega_dac_accession_number: null,
                                ega_policy_accession_number: null,
                                data_release_timing: "standard",
                            },
                            {
                                id_study_tmp: 3,
                                id_lims: "SQSCP",
                                id_study_lims: "4861",
                                name: "Third Study",
                                faculty_sponsor: "Dr Third",
                                state: "active",
                                abstract: "",
                                abbreviation: "TST",
                                accession_number: "EGAS00001003001",
                                description: "Third study",
                                data_release_strategy: "managed",
                                study_title: "Third Study",
                                data_access_group: "hgi",
                                hmdmc_number: "",
                                programme: "Test",
                                created: "2019-01-01T00:00:00Z",
                                reference_genome: "GRCh38",
                                ethically_approved: true,
                                study_type: "Transcriptome Analysis",
                                contains_human_dna: true,
                                contaminated_human_dna: false,
                                study_visibility: "Hold",
                                ega_dac_accession_number: null,
                                ega_policy_accession_number: null,
                                data_release_timing: "standard",
                            },
                            {
                                id_study_tmp: 4,
                                id_lims: "SQSCP",
                                id_study_lims: "4931",
                                name: "Fourth Study",
                                faculty_sponsor: "Dr Fourth",
                                state: "active",
                                abstract: "",
                                abbreviation: "FST",
                                accession_number: "EGAS00001003002",
                                description: "Fourth study",
                                data_release_strategy: "managed",
                                study_title: "Fourth Study",
                                data_access_group: "hgi",
                                hmdmc_number: "",
                                programme: "Test",
                                created: "2019-01-01T00:00:00Z",
                                reference_genome: "GRCh38",
                                ethically_approved: true,
                                study_type: "Transcriptome Analysis",
                                contains_human_dna: true,
                                contaminated_human_dna: false,
                                study_visibility: "Hold",
                                ega_dac_accession_number: null,
                                ega_policy_accession_number: null,
                                data_release_timing: "standard",
                            },
                        ],
                        // But all samples in this enrichment belong to study 5631 only
                        samples: [
                            {
                                id_study_lims: "5631",
                                id_sample_lims: "4141357",
                                sanger_id:
                                    "Pilot_study_of_dissociation_methods_for_human_gut_tissues7993354",
                                sample_name: "07-06-2019-001-TI",
                                taxon_id: 9606,
                                common_name: "Homo Sapiens",
                                library_type: "Chromium single cell 3 prime v3",
                                id_run: 29995,
                                lane: 3,
                                tag_index: 1,
                                irods_path: "/seq/29995/29995_3#1.cram",
                                study_accession_number: "EGAS00001003420",
                                accession_number: "EGAN00002124268",
                            },
                            {
                                id_study_lims: "5631",
                                id_sample_lims: "4141357",
                                sanger_id:
                                    "Pilot_study_of_dissociation_methods_for_human_gut_tissues7993354",
                                sample_name: "07-06-2019-001-TI",
                                taxon_id: 9606,
                                common_name: "Homo Sapiens",
                                library_type: "Chromium single cell 3 prime v3",
                                id_run: 29995,
                                lane: 3,
                                tag_index: 2,
                                irods_path: "/seq/29995/29995_3#2.cram",
                                study_accession_number: "EGAS00001003420",
                                accession_number: "EGAN00002124268",
                            },
                        ],
                    },
                    partial: false,
                },
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should show only ONE study (5631), not all 4
        expect(
            screen.getByText(
                "Pilot_study_of_dissociation_methods_for_human_gut_tissues",
            ),
        ).toBeTruthy();

        // Should NOT show the other studies
        expect(screen.queryByText("OTARscRNA")).toBeNull();
        expect(screen.queryByText("Third Study")).toBeNull();
        expect(screen.queryByText("Fourth Study")).toBeNull();

        // Should have Study section with the correct study
        expect(screen.getByText("Study")).toBeTruthy();

        // Should have Samples section
        expect(screen.getByText("Samples")).toBeTruthy();
    });

    it("shows all studies when samples belong to multiple studies", async () => {
        const { SeqmetaBadge } = await import("@/components/seqmeta-badge");

        // When samples actually belong to multiple studies, show all of them
        render(
            createElement(SeqmetaBadge, {
                metadataKey: "seqmeta_library",
                rawValue: "RNA PolyA",
                enrichment: {
                    identifier: "RNA PolyA",
                    type: "library_type",
                    graph: {
                        studies: [
                            {
                                id_study_tmp: 1,
                                id_lims: "SQSCP",
                                id_study_lims: "1001",
                                name: "Study A",
                                faculty_sponsor: "Dr A",
                                state: "active",
                                abstract: "",
                                abbreviation: "SA",
                                accession_number: "ERP001",
                                description: "Study A",
                                data_release_strategy: "managed",
                                study_title: "Study A",
                                data_access_group: "team",
                                hmdmc_number: "",
                                programme: "P1",
                                created: "2020-01-01T00:00:00Z",
                                reference_genome: "GRCh38",
                                ethically_approved: true,
                                study_type: "RNA-Seq",
                                contains_human_dna: true,
                                contaminated_human_dna: false,
                                study_visibility: "Open",
                                ega_dac_accession_number: null,
                                ega_policy_accession_number: null,
                                data_release_timing: "standard",
                            },
                            {
                                id_study_tmp: 2,
                                id_lims: "SQSCP",
                                id_study_lims: "1002",
                                name: "Study B",
                                faculty_sponsor: "Dr B",
                                state: "active",
                                abstract: "",
                                abbreviation: "SB",
                                accession_number: "ERP002",
                                description: "Study B",
                                data_release_strategy: "managed",
                                study_title: "Study B",
                                data_access_group: "team",
                                hmdmc_number: "",
                                programme: "P2",
                                created: "2020-01-01T00:00:00Z",
                                reference_genome: "GRCh38",
                                ethically_approved: true,
                                study_type: "RNA-Seq",
                                contains_human_dna: true,
                                contaminated_human_dna: false,
                                study_visibility: "Open",
                                ega_dac_accession_number: null,
                                ega_policy_accession_number: null,
                                data_release_timing: "standard",
                            },
                        ],
                        samples: [
                            {
                                id_study_lims: "1001",
                                id_sample_lims: "S001",
                                sanger_id: "SAMP_A_001",
                                sample_name: "Sample A1",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "RNA PolyA",
                                id_run: 1001,
                                lane: 1,
                                tag_index: 1,
                                irods_path: "/seq/1001",
                                study_accession_number: "ERP001",
                                accession_number: "ERS001",
                            },
                            {
                                id_study_lims: "1002",
                                id_sample_lims: "S002",
                                sanger_id: "SAMP_B_001",
                                sample_name: "Sample B1",
                                taxon_id: 9606,
                                common_name: "Human",
                                library_type: "RNA PolyA",
                                id_run: 1002,
                                lane: 1,
                                tag_index: 1,
                                irods_path: "/seq/1002",
                                study_accession_number: "ERP002",
                                accession_number: "ERS002",
                            },
                        ],
                    },
                    partial: false,
                },
            }),
        );

        fireEvent.click(screen.getByTestId("seqmeta-badge-trigger"));

        await waitFor(() => {
            expect(screen.getByRole("dialog")).toBeTruthy();
        });

        // Should show BOTH studies because samples belong to both
        expect(screen.getByText("Study A")).toBeTruthy();
        expect(screen.getByText("Study B")).toBeTruthy();
    });
});
