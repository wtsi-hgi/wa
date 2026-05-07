"use client";

import {
    useContext,
    useEffect,
    useRef,
    useState,
    useSyncExternalStore,
} from "react";

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
import { SeqmetaCache, SeqmetaCacheContext } from "@/lib/seqmeta-cache";

const EMPTY_ENRICHMENTS: Record<string, EnrichmentResult | null> = {};
const EMPTY_ERRORS: Record<string, "not_found" | "upstream_impaired"> = {};
const EMPTY_LIVE_STATE = {
    enrichments: EMPTY_ENRICHMENTS,
    errors: EMPTY_ERRORS,
};

// A shared, never-mutated empty cache used during SSR and the first client
// render so that the markup is deterministic regardless of any cookie state
// the SeqmetaCacheProvider may have already merged into the live cache.
const EMPTY_CACHE = new SeqmetaCache();

// Idiomatic hydration-safe "have we mounted yet" check. useSyncExternalStore
// returns the server snapshot during SSR *and* the very first client render
// (hydration), and only switches to the client snapshot after hydration has
// committed. This avoids the react-hooks/set-state-in-effect lint rule and
// also avoids an extra render that a useState+useEffect mounted flag would
// cause.
const subscribeNoop = () => () => {};
const getMountedClient = () => true;
const getMountedServer = () => false;

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
    const liveCache = useContext(SeqmetaCacheContext);
    const inFlightValuesRef = useRef(new Set<string>());
    // Until we have mounted on the client, expose an empty cache to the
    // render so that the SSR markup and the first client render are
    // deterministically identical regardless of any cookie-restored entries
    // the provider may already hold. Without this guard a returning user
    // whose cookie carries a "not_found" entry would get a hydration mismatch
    // where the server renders "loading enrichment" while the client renders
    // "enrichment unavailable".
    const mounted = useSyncExternalStore(
        subscribeNoop,
        getMountedClient,
        getMountedServer,
    );
    const cache = mounted ? liveCache : EMPTY_CACHE;
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
        if (!mounted) {
            return;
        }

        primeSeqmetaCache(liveCache, initialEnrichments);

        const pendingValues = values.filter(
            (value) =>
                !liveCache.has(value) &&
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

        void enrichSeqmetaMetadata(metadata, liveCache, enrichIdentifier)
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
        liveCache,
        mounted,
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
