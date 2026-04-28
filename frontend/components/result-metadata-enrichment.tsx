"use client";

import { useContext, useEffect, useRef, useState } from "react";

import { enrichIdentifier } from "@/app/(results)/actions";
import { ResultMetadata } from "@/components/result-metadata";
import type { EnrichmentResult } from "@/lib/contracts";
import {
    buildCachedEnrichmentState,
    collectSeqmetaValues,
    enrichSeqmetaMetadata,
    mergeSeqmetaEnrichmentState,
    primeSeqmetaCache,
} from "@/lib/seqmeta-enrichment";
import { SeqmetaCacheContext } from "@/lib/seqmeta-cache";

const EMPTY_ENRICHMENTS: Record<string, EnrichmentResult | null> = {};
const EMPTY_ERRORS: Record<string, "not_found" | "upstream_impaired"> = {};
const EMPTY_LIVE_STATE = {
    enrichments: EMPTY_ENRICHMENTS,
    errors: EMPTY_ERRORS,
};

type ResultMetadataEnrichmentProps = {
    initialEnrichments?: Record<string, EnrichmentResult | null>;
    initialErrors?: Record<string, "not_found" | "upstream_impaired">;
    metadata: Record<string, string>;
};

export function ResultMetadataEnrichment({
    initialEnrichments = EMPTY_ENRICHMENTS,
    initialErrors = EMPTY_ERRORS,
    metadata,
}: ResultMetadataEnrichmentProps) {
    const cache = useContext(SeqmetaCacheContext);
    const inFlightValuesRef = useRef(new Set<string>());
    const values = collectSeqmetaValues(metadata);
    const requestKey = values.join("\u0000");
    const baseState = mergeSeqmetaEnrichmentState(
        buildCachedEnrichmentState(metadata, cache),
        {
            enrichments: initialEnrichments,
            errors: initialErrors,
        },
    );
    const [liveState, setLiveState] = useState<{
        requestKey: string;
        state: typeof EMPTY_LIVE_STATE;
    }>({
        requestKey: "",
        state: EMPTY_LIVE_STATE,
    });
    const activeLiveState =
        liveState.requestKey === requestKey
            ? liveState.state
            : EMPTY_LIVE_STATE;
    const activeLiveErrors = activeLiveState.errors;
    const activeLiveEnrichments = activeLiveState.enrichments;
    const mergedState = mergeSeqmetaEnrichmentState(baseState, activeLiveState);
    const loading = Object.fromEntries(
        values
            .filter(
                (value) =>
                    !cache.has(value) &&
                    !(value in initialErrors) &&
                    !(value in activeLiveErrors) &&
                    !(value in activeLiveEnrichments),
            )
            .map((value) => [value, true]),
    );

    useEffect(() => {
        primeSeqmetaCache(cache, initialEnrichments);

        const pendingValues = values.filter(
            (value) =>
                !cache.has(value) &&
                !inFlightValuesRef.current.has(value) &&
                !(value in initialErrors) &&
                !(value in activeLiveErrors) &&
                !(value in activeLiveEnrichments),
        );

        if (pendingValues.length === 0) {
            return;
        }

        for (const value of pendingValues) {
            inFlightValuesRef.current.add(value);
        }

        void enrichSeqmetaMetadata(metadata, cache, enrichIdentifier)
            .then((nextState) => {
                for (const value of pendingValues) {
                    inFlightValuesRef.current.delete(value);
                }

                setLiveState({
                    requestKey,
                    state: nextState,
                });
            })
            .catch(() => {
                // Enrichment failed - clear in-flight and set errors for all pending
                for (const value of pendingValues) {
                    inFlightValuesRef.current.delete(value);
                }

                const errors: Record<
                    string,
                    "not_found" | "upstream_impaired"
                > = {};
                for (const value of pendingValues) {
                    errors[value] = "upstream_impaired";
                }

                setLiveState({
                    requestKey,
                    state: {
                        enrichments: {},
                        errors,
                    },
                });
            });
    }, [
        cache,
        activeLiveEnrichments,
        activeLiveErrors,
        initialEnrichments,
        initialErrors,
        metadata,
        requestKey,
        values,
    ]);

    return (
        <ResultMetadata
            enrichments={mergedState.enrichments}
            errors={mergedState.errors}
            loading={loading}
            metadata={metadata}
        />
    );
}
