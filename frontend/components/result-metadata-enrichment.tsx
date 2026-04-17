"use client";

import { useContext, useEffect } from "react";

import { ResultMetadata } from "@/components/result-metadata";
import type { IdentifierResult } from "@/lib/contracts";
import {
    buildCachedEnrichmentState,
    primeSeqmetaCache,
} from "@/lib/seqmeta-enrichment";
import { SeqmetaCacheContext } from "@/lib/seqmeta-cache";

type ResultMetadataEnrichmentProps = {
    initialEnrichments?: Record<string, IdentifierResult | null>;
    initialErrors?: Record<string, boolean>;
    metadata: Record<string, string>;
};

export function ResultMetadataEnrichment({
    initialEnrichments = {},
    initialErrors = {},
    metadata,
}: ResultMetadataEnrichmentProps) {
    const cache = useContext(SeqmetaCacheContext);
    const cachedState = buildCachedEnrichmentState(metadata, cache);

    useEffect(() => {
        primeSeqmetaCache(cache, initialEnrichments);
    }, [cache, initialEnrichments]);

    const enrichments = {
        ...cachedState.enrichments,
        ...initialEnrichments,
    };

    return (
        <ResultMetadata
            enrichments={enrichments}
            errors={initialErrors}
            metadata={metadata}
        />
    );
}
