import http from "node:http";

const port = Number(
    process.argv[2] ?? process.env.WA_TEST_SEQMETA_PORT ?? 8091,
);

const partialStudy = {
    identifier: "SANG5993",
    type: "sanger_sample_id",
    graph: {
        study: {
            id_study_tmp: 42,
            id_lims: "SQSCP",
            id_study_lims: "5993",
            name: "5993",
            faculty_sponsor: "Dr Example",
            state: "active",
            abstract: "Example study abstract",
            abbreviation: "EX",
            accession_number: "ERP5993",
            description: "Example study description",
            data_release_strategy: "managed",
            study_title: "Example study 5993",
            data_access_group: "group-a",
            hmdmc_number: "HMDMC-5993",
            programme: "Cancer",
            created: "2026-04-20T09:00:00Z",
            reference_genome: "GRCh38",
            ethically_approved: true,
            study_type: "Whole Genome Sequencing",
            contains_human_dna: true,
            contaminated_human_dna: false,
            study_visibility: "Always Open",
            ega_dac_accession_number: "EGAC5993",
            ega_policy_accession_number: "EGAP5993",
            data_release_timing: "Immediate",
        },
        samples: [
            {
                id_study_lims: "5993",
                id_sample_lims: "SMP5993",
                sanger_id: "SANG5993",
                sample_name: "Sample 5993",
                taxon_id: 9606,
                common_name: "Human",
                library_type: "exon",
                id_run: 48522,
                lane: 1,
                tag_index: 1,
                irods_path: "/irods/5993/SANG5993",
                study_accession_number: "ERP5993",
                accession_number: "SAMEA5993",
            },
        ],
        libraries: [
            {
                library_type: "exon",
                id_study_lims: "5993",
            },
        ],
    },
    partial: true,
    missing: [
        {
            hop: "samples",
            reason: "samples_truncated",
            status: 200,
        },
    ],
};

const laneFilterSample = {
    id_study_lims: "5993",
    id_sample_lims: "SMP10524782",
    sanger_id: "WTSI_wEMB10524782",
    sample_name: "WTSI_wEMB10524782",
    taxon_id: 9606,
    common_name: "Human",
    library_type: "exon",
    id_run: 48522,
    lane: 1,
    tag_index: 1,
    irods_path: "/irods/5993/WTSI_wEMB10524782",
    study_accession_number: "ERP5993",
    accession_number: "SAMEA10524782",
};

const laneFilterEnrichment = {
    identifier: "WTSI_wEMB10524782",
    type: "sanger_sample_id",
    object: laneFilterSample,
    graph: {
        study: partialStudy.graph.study,
        sample: laneFilterSample,
        samples: [laneFilterSample],
        library: partialStudy.graph.libraries[0],
        libraries: partialStudy.graph.libraries,
        sample_detail: {
            sanger_id: laneFilterSample.sanger_id,
            sample_name: laneFilterSample.sample_name,
            sample: laneFilterSample,
            lanes: [
                {
                    id_run: String(laneFilterSample.id_run),
                    lane: String(laneFilterSample.lane),
                    tag_index: laneFilterSample.tag_index,
                },
            ],
        },
    },
    partial: false,
};

const degradedSampleLims = {
    identifier: "SMP5994",
    type: "sample_lims_id",
    object: {
        id_sample_lims: "SMP5994",
        sanger_id: "SANG5994",
        id_study_lims: "5994",
    },
};

const libraryStudy = {
    id_study_tmp: 43,
    id_lims: "SQSCP",
    id_study_lims: "6591",
    name: "6591",
    faculty_sponsor: "Dr Example",
    state: "active",
    abstract: "Example study abstract",
    abbreviation: "RNA",
    accession_number: "ERP6591",
    description: "Example study description",
    data_release_strategy: "managed",
    study_title: "Example study 6591",
    data_access_group: "group-b",
    hmdmc_number: "HMDMC-6591",
    programme: "Transcriptomics",
    created: "2026-04-20T09:00:00Z",
    reference_genome: "GRCh38",
    ethically_approved: true,
    study_type: "RNA Sequencing",
    contains_human_dna: true,
    contaminated_human_dna: false,
    study_visibility: "Always Open",
    ega_dac_accession_number: "EGAC6591",
    ega_policy_accession_number: "EGAP6591",
    data_release_timing: "Immediate",
};

const libraryEnrichment = {
    identifier: "RNA",
    type: "library_type",
    graph: {
        study: libraryStudy,
        libraries: [
            {
                library_type: "RNA",
                id_study_lims: "6591",
            },
        ],
        samples: [
            {
                id_study_lims: "6591",
                id_sample_lims: "SMP6591",
                sanger_id: "SANG6591",
                sample_name: "Sample 6591",
                taxon_id: 9606,
                common_name: "Human",
                library_type: "RNA",
                id_run: 77123,
                lane: 1,
                tag_index: 3,
                irods_path: "/irods/6591/SANG6591",
                study_accession_number: "ERP6591",
                accession_number: "SAMEA6591",
            },
        ],
    },
    partial: false,
};

const validations = new Map([
    [
        "WTSI_wEMB10524782",
        {
            identifier: "WTSI_wEMB10524782",
            type: "sanger_sample_id",
            object: laneFilterSample,
        },
    ],
    [
        "SANG5993",
        {
            identifier: "SANG5993",
            type: "sanger_sample_id",
            object: partialStudy.graph.samples[0],
        },
    ],
    ["SMP5994", degradedSampleLims],
    [
        "RNA",
        {
            identifier: "RNA",
            type: "library_type",
            object: {
                library_type: "RNA",
                id_study_lims: "6591",
            },
        },
    ],
]);

function sendJson(response, statusCode, body) {
    response.writeHead(statusCode, { "content-type": "application/json" });
    response.end(JSON.stringify(body));
}

const server = http.createServer((request, response) => {
    const url = new URL(request.url ?? "/", `http://127.0.0.1:${port}`);

    if (url.pathname === "/studies") {
        sendJson(response, 200, []);
        return;
    }

    if (url.pathname === "/enrich/SANG5993") {
        sendJson(response, 200, partialStudy);
        return;
    }

    if (url.pathname === "/enrich/WTSI_wEMB10524782") {
        sendJson(response, 200, laneFilterEnrichment);
        return;
    }

    if (url.pathname === "/enrich/SMP5994") {
        sendJson(response, 502, {
            error: "seqmeta: all enrichment hops failed",
            missing: [
                {
                    hop: "classify",
                    reason: "upstream_error",
                    status: 502,
                },
            ],
        });
        return;
    }

    if (url.pathname === "/enrich/RNA") {
        sendJson(response, 200, libraryEnrichment);
        return;
    }

    if (url.pathname.startsWith("/validate/")) {
        const identifier = decodeURIComponent(
            url.pathname.slice("/validate/".length),
        );
        const result = validations.get(identifier);

        if (result) {
            sendJson(response, 200, result);
            return;
        }
    }

    if (
        url.pathname.startsWith("/enrich/") ||
        url.pathname.startsWith("/validate/")
    ) {
        sendJson(response, 404, { error: "seqmeta: unknown identifier" });
        return;
    }

    sendJson(response, 404, { error: "not found" });
});

const shutdown = () => {
    server.close(() => process.exit(0));
};

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);

server.listen(port, "127.0.0.1");
