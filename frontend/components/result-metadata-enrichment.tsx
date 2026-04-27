"use client";

import { useContext, useEffect, useRef, useState } from "react";

import { enrichIdentifier } from "@/app/(results)/actions";
import { ResultMetadata } from "@/components/result-metadata";
import type { EnrichmentResult } from "@/lib/contracts";
import {
    buildCachedEnrichmentState,
    collectSeqmetaValues,
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
    const mergedState = mergeSeqmetaEnrichmentState(baseState, activeLiveState);
    const loading = Object.fromEntries(
        values
            .filter(
                (value) =>
                    !cache.has(value) &&
                    !(value in initialErrors) &&
                    !(value in activeLiveState.errors) &&
                    !(value in activeLiveState.enrichments),
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
                !(value in activeLiveState.errors) &&
                !(value in activeLiveState.enrichments),
        );

        if (pendingValues.length === 0) {
            return;
        }

        for (const value of pendingValues) {
            inFlightValuesRef.current.add(value);
        }

        Promise.allSettled(
            pendingValues.map(async (value) => ({
                result: await enrichIdentifier(value),
                value,
            })),
        ).then((settled) => {
            const nextEnrichments = { ...activeLiveState.enrichments };
            const nextErrors = { ...activeLiveState.errors };

            for (const [index, result] of settled.entries()) {
                const value = pendingValues[index];

                if (!value) {
                    continue;
                }

                inFlightValuesRef.current.delete(value);

                if (result.status === "fulfilled") {
                    if (result.value.result === null) {
                        cache.set(value, null);
                        nextEnrichments[value] = null;
                        nextErrors[value] = "not_found";
                    } else {
                        cache.set(value, result.value.result);
                        nextEnrichments[value] = result.value.result;
                        delete nextErrors[value];
                    }

                    continue;
                }

                nextErrors[value] = "upstream_impaired";
            }

            setLiveState({
                requestKey,
                state: {
                    enrichments: nextEnrichments,
                    errors: nextErrors,
                },
            });
        });
    }, [
        activeLiveState.enrichments,
        activeLiveState.errors,
        cache,
        initialEnrichments,
        initialErrors,
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
