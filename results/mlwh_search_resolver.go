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
	"strings"
	"sync"
	"time"

	"github.com/wtsi-hgi/wa/mlwh"
)

const defaultMLWHSearchResolverCacheTTL = 5 * time.Minute

type mlwhSearchExpander interface {
	ExpandSearchValues(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error)
}

type mlwhIdentifierClassifier interface {
	ClassifyIdentifier(context.Context, string) (mlwh.Match, error)
}

type mlwhStudyResolver interface {
	ResolveStudy(context.Context, string) (mlwh.Match, error)
}

type mlwhSampleNameResolver interface {
	ResolveSampleName(context.Context, string) (mlwh.Match, error)
}

type mlwhSearchResolvedValues struct {
	samples []string
	runs    []string
	lanes   []string

	expiresAt time.Time
}

// MLWHSearchResolver expands search identifiers through mlwh and caches them for repeated searches.
type MLWHSearchResolver struct {
	client        mlwhSearchExpander
	classifier    mlwhIdentifierClassifier
	studyResolver mlwhStudyResolver
	cacheTTL      time.Duration
	cacheMu       sync.Mutex
	cache         map[string]mlwhSearchResolvedValues
}

// NewMLWHSearchResolver constructs a cache-backed resolver for results search expansion.
func NewMLWHSearchResolver(client mlwhSearchExpander) *MLWHSearchResolver {
	resolver := &MLWHSearchResolver{
		client:   client,
		cacheTTL: defaultMLWHSearchResolverCacheTTL,
		cache:    map[string]mlwhSearchResolvedValues{},
	}
	if studyResolver, ok := client.(mlwhStudyResolver); ok {
		resolver.studyResolver = studyResolver
	}
	if classifier, ok := client.(mlwhIdentifierClassifier); ok {
		resolver.classifier = classifier
	}

	return resolver
}

// ClassifyIdentifier delegates generic exact identifier classification to the configured MLWH client.
func (r *MLWHSearchResolver) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	if r == nil || r.classifier == nil {
		return mlwh.Match{}, mlwh.ErrUnsupportedIdentifier
	}

	return r.classifier.ClassifyIdentifier(ctx, raw)
}

// CanonicalStudySearchValue resolves study accessions, names, and IDs to the
// canonical study LIMS ID used by stored seqmeta_studyid metadata.
func (r *MLWHSearchResolver) CanonicalStudySearchValue(ctx context.Context, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if r == nil || r.studyResolver == nil {
		return trimmed, nil
	}

	match, err := r.studyResolver.ResolveStudy(ctx, trimmed)
	if err != nil {
		switch {
		case errors.Is(err, mlwh.ErrNotFound), errors.Is(err, mlwh.ErrUnsupportedIdentifier):
			return trimmed, nil
		default:
			return "", fmt.Errorf("%w: resolve study: %w", ErrMLWHFailed, err)
		}
	}

	if match.Canonical != "" {
		return match.Canonical, nil
	}
	if match.Study != nil && match.Study.IDStudyLims != "" {
		return match.Study.IDStudyLims, nil
	}

	return trimmed, nil
}

// ResolveRun delegates registration run lookups to the configured MLWH client.
func (r *MLWHSearchResolver) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	resolver, err := r.registrationResolver()
	if err != nil {
		return mlwh.Match{}, err
	}

	return resolver.ResolveRun(ctx, raw)
}

// ResolveStudy delegates registration study lookups to the configured MLWH client.
func (r *MLWHSearchResolver) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	resolver, err := r.registrationResolver()
	if err != nil {
		return mlwh.Match{}, err
	}

	return resolver.ResolveStudy(ctx, raw)
}

// ResolveSample delegates registration sample lookups to the configured MLWH client.
func (r *MLWHSearchResolver) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	resolver, err := r.registrationResolver()
	if err != nil {
		return mlwh.Match{}, err
	}

	return resolver.ResolveSample(ctx, raw)
}

// ResolveSampleName delegates exact registration sample-name lookups to the configured MLWH client.
func (r *MLWHSearchResolver) ResolveSampleName(ctx context.Context, raw string) (mlwh.Match, error) {
	resolver, ok := r.sampleNameResolver()
	if !ok {
		return mlwh.Match{}, mlwh.ErrUnsupportedIdentifier
	}

	return resolver.ResolveSampleName(ctx, raw)
}

// ResolveLibrary delegates registration library lookups to the configured MLWH client.
func (r *MLWHSearchResolver) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	resolver, err := r.registrationResolver()
	if err != nil {
		return mlwh.Match{}, err
	}

	return resolver.ResolveLibrary(ctx, raw)
}

// ResolveLibraryIdentifier delegates exact registration library identifier
// lookups to the configured MLWH client when available.
func (r *MLWHSearchResolver) ResolveLibraryIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	resolver, ok := r.libraryIdentifierResolver()
	if !ok {
		return mlwh.Match{}, mlwh.ErrNotFound
	}

	return resolver.ResolveLibraryIdentifier(ctx, raw)
}

func (r *MLWHSearchResolver) registrationResolver() (RegistrationResolver, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("%w: MLWH resolver is not configured", ErrMLWHFailed)
	}

	resolver, ok := r.client.(RegistrationResolver)
	if !ok {
		return nil, fmt.Errorf("%w: MLWH registration resolver is not configured", ErrMLWHFailed)
	}

	return resolver, nil
}

func (r *MLWHSearchResolver) sampleNameResolver() (registrationSampleNameResolver, bool) {
	if r == nil || r.client == nil {
		return nil, false
	}

	resolver, ok := r.client.(registrationSampleNameResolver)

	return resolver, ok
}

func (r *MLWHSearchResolver) libraryIdentifierResolver() (registrationLibraryIdentifierResolver, bool) {
	if r == nil || r.client == nil {
		return nil, false
	}

	resolver, ok := r.client.(registrationLibraryIdentifierResolver)

	return resolver, ok
}

// Expand resolves related search values for a canonical identifier.
func (r *MLWHSearchResolver) Expand(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]string, []string, []string, error) {
	if r == nil || r.client == nil {
		return nil, nil, nil, fmt.Errorf("%w: resolver is not configured", ErrMLWHFailed)
	}

	trimmed := strings.TrimSpace(canonical)
	if trimmed == "" {
		return []string{}, []string{}, []string{}, nil
	}

	cacheKey := string(kind) + ":" + trimmed
	if cached, ok := r.cacheGet(cacheKey); ok {
		return cached.samples, cached.runs, cached.lanes, nil
	}

	values, err := r.client.ExpandSearchValues(ctx, kind, trimmed)
	if err != nil {
		switch {
		case errors.Is(err, mlwh.ErrCacheNeverSynced):
			return nil, nil, nil, fmt.Errorf("%w: expand identifier: %w", ErrMLWHFailed, err)
		case errors.Is(err, mlwh.ErrNotFound), errors.Is(err, mlwh.ErrUnsupportedIdentifier):
			r.cachePut(cacheKey, nil, nil, nil)

			return []string{}, []string{}, []string{}, nil
		default:
			return nil, nil, nil, fmt.Errorf("%w: expand identifier: %w", ErrMLWHFailed, err)
		}
	}

	samples, runs, lanes := values.Samples, values.Runs, values.Lanes
	r.cachePut(cacheKey, samples, runs, lanes)

	return samples, runs, lanes, nil
}

// ExpandCandidateSampleSearchValues resolves a direct sample metadata value by
// checking only sample names already present in registered results.
func (r *MLWHSearchResolver) ExpandCandidateSampleSearchValues(ctx context.Context, kind mlwh.IdentifierKind, canonical string, candidates []string) ([]string, error) {
	if !directSampleSearchKind(kind) {
		return nil, mlwh.ErrUnsupportedIdentifier
	}

	resolver, ok := r.client.(mlwhSampleNameResolver)
	if !ok {
		return nil, mlwh.ErrUnsupportedIdentifier
	}

	target := strings.TrimSpace(canonical)
	if target == "" {
		return []string{}, nil
	}

	matches := []string{}
	seen := map[string]struct{}{}
	for _, candidate := range nonEmptySearchValues(candidates) {
		match, err := resolver.ResolveSampleName(ctx, candidate)
		if err != nil {
			if errors.Is(err, mlwh.ErrNotFound) {
				continue
			}
			if errors.Is(err, mlwh.ErrUnsupportedIdentifier) {
				return nil, err
			}

			return nil, fmt.Errorf("%w: resolve candidate sample metadata: %w", ErrMLWHFailed, err)
		}
		if !sampleMatchHasDirectMetadataValue(match.Sample, kind, target) {
			continue
		}

		if _, ok := seen[candidate]; ok {
			continue
		}

		seen[candidate] = struct{}{}
		matches = append(matches, candidate)
	}

	return matches, nil
}

func sampleMatchHasDirectMetadataValue(sample *mlwh.Sample, kind mlwh.IdentifierKind, target string) bool {
	if sample == nil {
		return false
	}

	switch kind {
	case mlwh.KindSampleLimsID:
		return strings.EqualFold(strings.TrimSpace(sample.IDSampleLims), target)
	case mlwh.KindSangerSampleID:
		return strings.EqualFold(strings.TrimSpace(sample.SangerSampleID), target)
	case mlwh.KindSupplierName:
		return strings.EqualFold(strings.TrimSpace(sample.SupplierName), target)
	case mlwh.KindSampleAccession:
		return strings.EqualFold(strings.TrimSpace(sample.AccessionNumber), target)
	default:
		return false
	}
}

func directSampleSearchKind(kind mlwh.IdentifierKind) bool {
	switch kind {
	case mlwh.KindSampleLimsID, mlwh.KindSangerSampleID, mlwh.KindSupplierName, mlwh.KindSampleAccession:
		return true
	default:
		return false
	}
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
