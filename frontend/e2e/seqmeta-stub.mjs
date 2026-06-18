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
            accession_number: "ERP5993",
            data_release_strategy: "managed",
            study_title: "Example study 5993",
            data_access_group: "group-a",
            programme: "Cancer",
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
    accession_number: "ERP6591",
    data_release_strategy: "managed",
    study_title: "Example study 6591",
    data_access_group: "group-b",
    programme: "Transcriptomics",
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

function multiFieldSample(index) {
    return {
        id_study_lims: "7607",
        id_sample_lims: `SMP7607-${String(index).padStart(4, "0")}`,
        sanger_id:
            index === 0 ? "7607STDY14643771" : `7607STDY${14643771 + index}`,
        sample_name: index === 0 ? "7607STDY14643771" : `Sample ${index}`,
        supplier_name:
            index === 0 ? "Supplier Sample 7607" : `Supplier Sample ${index}`,
        taxon_id: 9606,
        common_name: "Human",
        library_type: "Custom",
        id_run: 48522,
        lane: (index % 8) + 1,
        tag_index: (index % 96) + 1,
        irods_path: `/irods/7607/sample-${index}`,
        study_accession_number: "ERP7607",
        accession_number: `SAMEA7607${index}`,
    };
}

const multiFieldSamples = Array.from({ length: 2500 }, (_, index) =>
    multiFieldSample(index),
);
const multiFieldPrimarySample = multiFieldSamples[0];
const multiFieldStudy = {
    id_study_tmp: 7607,
    id_lims: "SQSCP",
    id_study_lims: "7607",
    name: "7607",
    faculty_sponsor: "Dr Repro",
    state: "active",
    accession_number: "ERP7607",
    data_release_strategy: "managed",
    study_title: "Seqmeta rendering repro study",
    data_access_group: "seqmeta",
    programme: "Performance",
    reference_genome: "GRCh38",
    ethically_approved: true,
    study_type: "Whole Genome Sequencing",
    contains_human_dna: true,
    contaminated_human_dna: false,
    study_visibility: "Always Open",
    ega_dac_accession_number: "EGAC7607",
    ega_policy_accession_number: "EGAP7607",
    data_release_timing: "Immediate",
};
const multiFieldLibrary = {
    library_type: "Custom",
    id_study_lims: "7607",
    library_id: "71046409",
    id_library_lims: "LIB7607-71046409",
};
const multiFieldMlwhLibrary = {
    pipeline_id_lims: "Custom",
    id_study_lims: "7607",
    library_id: "71046409",
    id_library_lims: "LIB7607-71046409",
};
const multiFieldGraph = {
    study: multiFieldStudy,
    sample: multiFieldPrimarySample,
    samples: multiFieldSamples,
    library: multiFieldLibrary,
    libraries: [multiFieldLibrary],
    study_detail: {
        study: multiFieldStudy,
        library_details: [
            {
                library: multiFieldMlwhLibrary,
                samples: multiFieldSamples,
            },
        ],
    },
    sample_detail: {
        sample: multiFieldPrimarySample,
        study: multiFieldStudy,
        lanes: [
            {
                id_run: "48522",
                lane: "1",
                tag_index: 1,
            },
        ],
        libraries: [multiFieldMlwhLibrary],
    },
};
const hekR1Sample = {
    ...multiFieldPrimarySample,
    supplier_name: "Hek_R1",
};
const hekR2Sample = {
    ...multiFieldSample(1),
    sanger_id: "7607STDY14643772",
    sample_name: "7607STDY14643772",
    supplier_name: "Hek_R2",
};

function graphForSample(sample) {
    return {
        ...multiFieldGraph,
        sample,
        samples: [sample],
        sample_detail: {
            ...multiFieldGraph.sample_detail,
            sanger_id: sample.sanger_id,
            sample_name: sample.sample_name,
            sample,
        },
    };
}

const hekR1Graph = graphForSample(hekR1Sample);
const hekR2Graph = graphForSample(hekR2Sample);
const multiFieldEnrichments = new Map([
    [
        "7607",
        {
            identifier: "7607",
            type: "study_id",
            graph: multiFieldGraph,
            partial: false,
        },
    ],
    [
        "7607STDY14643771",
        {
            identifier: "7607STDY14643771",
            type: "sanger_sample_id",
            graph: multiFieldGraph,
            partial: false,
        },
    ],
    [
        "7607STDY14643772",
        {
            identifier: "7607STDY14643772",
            type: "sanger_sample_id",
            graph: hekR2Graph,
            partial: false,
        },
    ],
    [
        "Hek_R1",
        {
            identifier: "Hek_R1",
            type: "supplier_name",
            graph: hekR1Graph,
            partial: false,
        },
    ],
    [
        "Hek_R2",
        {
            identifier: "Hek_R2",
            type: "supplier_name",
            graph: hekR2Graph,
            partial: false,
        },
    ],
    [
        "SMP7607-0000",
        {
            identifier: "SMP7607-0000",
            type: "sample_lims_id",
            graph: multiFieldGraph,
            partial: false,
        },
    ],
    [
        "Supplier Sample 7607",
        {
            identifier: "Supplier Sample 7607",
            type: "supplier_name",
            graph: multiFieldGraph,
            partial: false,
        },
    ],
    [
        "48522",
        {
            identifier: "48522",
            type: "run_id",
            graph: multiFieldGraph,
            partial: false,
        },
    ],
    [
        "Custom",
        {
            identifier: "Custom",
            type: "library_type",
            graph: multiFieldGraph,
            partial: false,
        },
    ],
    [
        "71046409",
        {
            identifier: "71046409",
            type: "library_id",
            graph: multiFieldGraph,
            partial: false,
        },
    ],
]);

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
    [
        "7607",
        {
            identifier: "7607",
            type: "study_id",
            object: multiFieldStudy,
        },
    ],
    [
        "7607STDY14643771",
        {
            identifier: "7607STDY14643771",
            type: "sanger_sample_id",
            object: multiFieldPrimarySample,
        },
    ],
    [
        "7607STDY14643772",
        {
            identifier: "7607STDY14643772",
            type: "sanger_sample_id",
            object: hekR2Sample,
        },
    ],
    [
        "Hek_R1",
        {
            identifier: "Hek_R1",
            type: "supplier_name",
            object: hekR1Sample,
        },
    ],
    [
        "Hek_R2",
        {
            identifier: "Hek_R2",
            type: "supplier_name",
            object: hekR2Sample,
        },
    ],
    [
        "SMP7607-0000",
        {
            identifier: "SMP7607-0000",
            type: "sample_lims_id",
            object: multiFieldPrimarySample,
        },
    ],
    [
        "Supplier Sample 7607",
        {
            identifier: "Supplier Sample 7607",
            type: "supplier_name",
            object: multiFieldPrimarySample,
        },
    ],
    [
        "48522",
        {
            identifier: "48522",
            type: "run_id",
            object: multiFieldPrimarySample,
        },
    ],
    [
        "Custom",
        {
            identifier: "Custom",
            type: "library_type",
            object: multiFieldLibrary,
        },
    ],
    [
        "71046409",
        {
            identifier: "71046409",
            type: "library_id",
            object: multiFieldLibrary,
        },
    ],
]);

const mlwhKindAliases = new Map([["study_id", "study_lims_id"]]);

const sampleKinds = new Set([
    "sample_uuid",
    "sample_lims_id",
    "sanger_sample_name",
    "sanger_sample_id",
    "supplier_name",
    "sample_accession",
    "donor_id",
]);
const studyKinds = new Set([
    "study_uuid",
    "study_lims_id",
    "study_accession",
    "study_name",
]);
const runKinds = new Set(["run_id"]);
const libraryKinds = new Set(["library_type", "library_id", "id_library_lims"]);

function mlwhKind(type) {
    return mlwhKindAliases.get(type) ?? type;
}

function sampleName(result) {
    return result.object?.name ?? result.object?.sample_name;
}

function mlwhKindFromValidation(result) {
    const kind = mlwhKind(result.type);

    if (
        kind === "sanger_sample_id" &&
        sampleName(result) === result.identifier
    ) {
        return "sanger_sample_name";
    }

    return kind;
}

function canonicalIdentifier(result, kind) {
    if (sampleKinds.has(kind)) {
        return sampleName(result) ?? result.identifier;
    }

    if (studyKinds.has(kind)) {
        return result.object?.id_study_lims ?? result.identifier;
    }

    if (runKinds.has(kind)) {
        const idRun = Number(result.object?.id_run ?? result.identifier);

        if (Number.isFinite(idRun)) {
            return String(idRun);
        }
    }

    if (kind === "library_type") {
        return (
            result.object?.pipeline_id_lims ??
            result.object?.library_type ??
            result.identifier
        );
    }

    if (kind === "library_id") {
        return result.object?.library_id ?? result.identifier;
    }

    if (kind === "id_library_lims") {
        return result.object?.id_library_lims ?? result.identifier;
    }

    return result.identifier;
}

function runMatchObject(result) {
    const idRun = Number(result.object?.id_run ?? result.identifier);

    if (Number.isFinite(idRun)) {
        return { id_run: idRun };
    }

    return result.object ?? null;
}

function mlwhMatchFromValidation(result) {
    const kind = mlwhKindFromValidation(result);
    const match = {
        Kind: kind,
        Canonical: canonicalIdentifier(result, kind),
        Sample: null,
        Study: null,
        Run: null,
        Library: null,
    };

    if (sampleKinds.has(kind)) {
        match.Sample = result.object ?? null;
        return match;
    }

    if (studyKinds.has(kind)) {
        match.Study = result.object ?? null;
        return match;
    }

    if (runKinds.has(kind)) {
        match.Run = runMatchObject(result);
        return match;
    }

    if (libraryKinds.has(kind)) {
        match.Library = result.object ?? null;
    }

    return match;
}

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

    if (url.pathname.startsWith("/enrich/")) {
        const identifier = decodeURIComponent(
            url.pathname.slice("/enrich/".length),
        );
        const enrichment = multiFieldEnrichments.get(identifier);

        if (enrichment) {
            sendJson(response, 200, enrichment);
            return;
        }
    }

    if (url.pathname.startsWith("/classify/")) {
        const identifier = decodeURIComponent(
            url.pathname.slice("/classify/".length),
        );
        const result = validations.get(identifier);

        if (result) {
            sendJson(response, 200, mlwhMatchFromValidation(result));
            return;
        }
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

    if (url.pathname.startsWith("/classify/")) {
        sendJson(response, 404, {
            code: "not_found",
            message: "mlwh: identifier not found",
        });
        return;
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
