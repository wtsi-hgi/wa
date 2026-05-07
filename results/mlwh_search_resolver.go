/*******************************************************************************
 * Copyright (c) 2026 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

package results

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wtsi-hgi/wa/mlwh"
)

type mlwhSearchExpander interface {
	ExpandIdentifier(context.Context, mlwh.IdentifierKind, string) ([]mlwh.TaggedID, error)
	LanesForSample(context.Context, string, int, int) ([]mlwh.Lane, error)
}

type mlwhSearchResolvedValues struct {
	samples []string
	runs    []string
	lanes   []string

	expiresAt time.Time
}

// MLWHSearchResolver expands search identifiers through mlwh and caches them for repeated searches.
type MLWHSearchResolver struct {
	client   mlwhSearchExpander
	cacheTTL time.Duration
	cacheMu  sync.Mutex
	cache    map[string]mlwhSearchResolvedValues
}

// NewMLWHSearchResolver constructs a cache-backed resolver for results search expansion.
func NewMLWHSearchResolver(client mlwhSearchExpander) *MLWHSearchResolver {
	return &MLWHSearchResolver{
		client:   client,
		cacheTTL: defaultSeqmetaResolverCacheTTL,
		cache:    map[string]mlwhSearchResolvedValues{},
	}
}

// Expand resolves related search values for a canonical identifier.
func (r *MLWHSearchResolver) Expand(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]string, []string, []string, error) {
	if r == nil || r.client == nil {
		return nil, nil, nil, fmt.Errorf("%w: resolver is not configured", ErrSeqmetaFailed)
	}

	trimmed := strings.TrimSpace(canonical)
	if trimmed == "" {
		return []string{}, []string{}, []string{}, nil
	}

	cacheKey := string(kind) + ":" + trimmed
	if cached, ok := r.cacheGet(cacheKey); ok {
		return cached.samples, cached.runs, cached.lanes, nil
	}

	taggedIDs, err := r.client.ExpandIdentifier(ctx, kind, trimmed)
	if err != nil {
		switch {
		case errors.Is(err, mlwh.ErrNotFound), errors.Is(err, mlwh.ErrUnsupportedIdentifier):
			r.cachePut(cacheKey, nil, nil, nil)

			return []string{}, []string{}, []string{}, nil
		default:
			return nil, nil, nil, fmt.Errorf("%w: expand identifier: %w", ErrSeqmetaFailed, err)
		}
	}

	samples := []string{}
	runs := []string{}
	for _, taggedID := range taggedIDs {
		switch taggedID.Kind {
		case mlwh.KindSangerSampleName:
			samples = mergeSearchValues(samples, []string{taggedID.Canonical})
		case mlwh.KindRunID:
			runs = mergeSearchValues(runs, []string{taggedID.Canonical})
		}
	}

	lanes, err := r.expandLanesForSamples(ctx, samples)
	if err != nil {
		return nil, nil, nil, err
	}

	runs = mergeSearchValues(runs, runIDsFromLaneValues(lanes))
	r.cachePut(cacheKey, samples, runs, lanes)

	return samples, runs, lanes, nil
}

func (r *MLWHSearchResolver) expandLanesForSamples(ctx context.Context, sampleIDs []string) ([]string, error) {
	resolved := []string{}

	for _, sampleID := range sampleIDs {
		lanes, err := r.client.LanesForSample(ctx, sampleID, mlwh.MaxSamplesPerHop, 0)
		if err != nil {
			if errors.Is(err, mlwh.ErrNotFound) {
				continue
			}

			return nil, fmt.Errorf("%w: expand sample lanes: %w", ErrSeqmetaFailed, err)
		}

		for _, lane := range lanes {
			laneID := buildLaneID(strconv.Itoa(lane.IDRun), strconv.Itoa(lane.Position), lane.TagIndex)
			if laneID == "" {
				continue
			}

			resolved = mergeSearchValues(resolved, []string{laneID})
		}
	}

	return resolved, nil
}

func (r *MLWHSearchResolver) cacheGet(key string) (mlwhSearchResolvedValues, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	entry, ok := r.cache[key]
	if !ok {
		return mlwhSearchResolvedValues{}, false
	}

	if time.Now().After(entry.expiresAt) {
		delete(r.cache, key)

		return mlwhSearchResolvedValues{}, false
	}

	return entry, true
}

func (r *MLWHSearchResolver) cachePut(key string, samples, runs, lanes []string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	r.cache[key] = mlwhSearchResolvedValues{
		samples:   samples,
		runs:      runs,
		lanes:     lanes,
		expiresAt: time.Now().Add(r.cacheTTL),
	}
}