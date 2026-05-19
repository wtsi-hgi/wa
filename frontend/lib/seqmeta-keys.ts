const legacySeqmetaKeyAliases: Record<string, string> = {
    seqmeta_library: "seqmeta_pipeline_id_lims",
    seqmeta_libraryid: "seqmeta_library_id",
    seqmeta_library_lims: "seqmeta_id_library_lims",
    seqmeta_librarytype: "seqmeta_pipeline_id_lims",
    seqmeta_name: "seqmeta_sample_name",
    seqmeta_runid: "seqmeta_id_run",
    seqmeta_sampleid: "seqmeta_sample_name",
    seqmeta_sample_lims: "seqmeta_id_sample_lims",
    seqmeta_studyid: "seqmeta_id_study_lims",
};

export function canonicalSeqmetaKey(metadataKey: string): string {
    return legacySeqmetaKeyAliases[metadataKey] ?? metadataKey;
}

export function isSeqmetaKey(key: string): boolean {
    return key.startsWith("seqmeta_");
}
