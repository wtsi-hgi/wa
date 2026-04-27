import http from "node:http";

const port = Number(
    process.argv[2] ?? process.env.WA_TEST_SEQMETA_PORT ?? 8091,
);

const partialStudy = {
    identifier: "5993",
    type: "study_id",
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

const validations = new Map([
    [
        "5993",
        {
            identifier: "5993",
            type: "study_id",
            object: partialStudy.graph.study,
        },
    ],
    [
        "5994",
        {
            identifier: "5994",
            type: "study_id",
            object: {
                id_study_lims: "5994",
                name: "5994",
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

    if (url.pathname === "/enrich/5993") {
        sendJson(response, 200, partialStudy);
        return;
    }

    if (url.pathname === "/enrich/5994") {
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
