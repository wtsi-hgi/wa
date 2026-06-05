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

const userFacingMlwhKeyConfigs = {
    sample: {
        label: "Sample",
        seqmetaKeys: [
            "seqmeta_sample_name",
            "seqmeta_sanger_sample_id",
            "seqmeta_id_sample_lims",
        ],
    },
    study: {
        label: "Study",
        seqmetaKeys: ["seqmeta_id_study_lims", "seqmeta_study_accession"],
    },
    library: {
        label: "Library",
        seqmetaKeys: [
            "seqmeta_pipeline_id_lims",
            "seqmeta_library_id",
            "seqmeta_id_library_lims",
        ],
    },
    run: {
        label: "Run",
        seqmetaKeys: ["seqmeta_id_run"],
    },
} as const;

export type UserFacingMlwhMetadataKey = keyof typeof userFacingMlwhKeyConfigs;

export const userFacingMlwhMetadataKeys = Object.keys(
    userFacingMlwhKeyConfigs,
) as UserFacingMlwhMetadataKey[];

export function canonicalSeqmetaKey(metadataKey: string): string {
    return legacySeqmetaKeyAliases[metadataKey] ?? metadataKey;
}

export function isSeqmetaKey(key: string): boolean {
    return key.startsWith("seqmeta_");
}

export function isUserFacingMlwhMetadataKey(
    key: string,
): key is UserFacingMlwhMetadataKey {
    return key in userFacingMlwhKeyConfigs;
}

export function userFacingMlwhMetadataLabel(
    key: UserFacingMlwhMetadataKey,
): string {
    return userFacingMlwhKeyConfigs[key].label;
}

export function isSeqmetaKeyForUserFacingMlwhMetadataKey(
    userFacingKey: UserFacingMlwhMetadataKey,
    metadataKey: string,
): boolean {
    const canonicalKey = canonicalSeqmetaKey(metadataKey);

    return userFacingMlwhKeyConfigs[userFacingKey].seqmetaKeys.some(
        (seqmetaKey) => seqmetaKey === canonicalKey,
    );
}

export function preferredSeqmetaKeyForUserFacingMlwhMetadataKey(
    userFacingKey: UserFacingMlwhMetadataKey,
    availableKeys: Iterable<string>,
): string | null {
    const keys = Array.from(availableKeys);

    for (const preferredKey of userFacingMlwhKeyConfigs[userFacingKey]
        .seqmetaKeys) {
        const matchingKey = keys.find(
            (key) => canonicalSeqmetaKey(key) === preferredKey,
        );

        if (matchingKey) {
            return preferredKey;
        }
    }

    return null;
}
