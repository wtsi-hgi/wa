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
	"sort"
	"strings"

	"github.com/wtsi-hgi/wa/mlwh"
)

var registrationLookupFlagMetaKeys = map[string]string{
	"run":     SeqmetaIDRunKey,
	"study":   SeqmetaIDStudyLimsKey,
	"sample":  SeqmetaSampleNameKey,
	"library": SeqmetaPipelineIDLimsKey,
}

// RegistrationResolver resolves MLWH registration lookup values on the server.
type RegistrationResolver interface {
	ResolveSample(context.Context, string) (mlwh.Match, error)
	ResolveStudy(context.Context, string) (mlwh.Match, error)
	ResolveRun(context.Context, string) (mlwh.Match, error)
	ResolveLibrary(context.Context, string) (mlwh.Match, error)
}

// ApplyRegistrationLookups resolves raw MLWH lookup values and merges them into
// registration metadata before validation and storage.
func ApplyRegistrationLookups(ctx context.Context, registration *Registration, resolver RegistrationResolver) error {
	if registration == nil || registration.LookupValues == nil || !registration.LookupValues.HasValues() {
		return nil
	}

	if resolver == nil {
		return fmt.Errorf("%w: MLWH resolver is not configured for registration lookups", ErrSeqmetaFailed)
	}

	lookupMetadata, err := ResolveRegistrationLookupMetadata(ctx, resolver, *registration.LookupValues)
	if err != nil {
		return registrationLookupDomainError(err)
	}

	if err := mergeRegistrationLookupMetadata(registration, lookupMetadata); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	registration.LookupValues = nil

	return nil
}

func registrationLookupDomainError(err error) error {
	switch {
	case errors.Is(err, mlwh.ErrNotFound), errors.Is(err, mlwh.ErrUnsupportedIdentifier), errors.Is(err, mlwh.ErrAmbiguous):
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	default:
		return fmt.Errorf("%w: %w", ErrSeqmetaFailed, err)
	}
}

func mergeRegistrationLookupMetadata(registration *Registration, lookupMetadata map[string][]string) error {
	if len(lookupMetadata) == 0 {
		return nil
	}

	metadataValues := normalizedRegistrationMetadataValues(registration)
	for _, key := range sortedRegistrationMetadataKeys(lookupMetadata) {
		values := nonEmptyRegistrationLookupValues(lookupMetadata[key])
		if len(values) == 0 {
			continue
		}

		for _, equivalentKey := range registrationEquivalentSeqmetaKeys(key) {
			if _, exists := metadataValues[equivalentKey]; exists {
				return fmt.Errorf("metadata key %q was supplied via both --meta and --%s", equivalentKey, registrationSeqmetaFlagName(key))
			}
		}

		if _, exists := metadataValues[key]; exists {
			return fmt.Errorf("metadata key %q was supplied via both --meta and --%s", key, registrationSeqmetaFlagName(key))
		}

		for _, value := range values {
			appendMetadataValue(metadataValues, key, value)
		}
	}

	registration.MetadataValues = metadataValues
	registration.Metadata = singleMetadataFromValues(metadataValues)

	return nil
}

func sortedRegistrationMetadataKeys(metadata map[string][]string) []string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func nonEmptyRegistrationLookupValues(values []string) []string {
	nonEmpty := make([]string, 0, len(values))

	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}

	return nonEmpty
}

func registrationEquivalentSeqmetaKeys(metaKey string) []string {
	switch metaKey {
	case SeqmetaIDRunKey:
		return []string{LegacySeqmetaRunIDKey}
	case SeqmetaIDStudyLimsKey:
		return []string{LegacySeqmetaStudyIDKey}
	case SeqmetaSampleNameKey:
		return []string{LegacySeqmetaSampleIDKey}
	case SeqmetaPipelineIDLimsKey:
		return []string{LegacySeqmetaLibraryKey, LegacySeqmetaLibraryTypeKey}
	case SeqmetaLibraryIDKey:
		return []string{LegacySeqmetaLibraryIDKey}
	case SeqmetaIDLibraryLimsKey:
		return []string{LegacySeqmetaLibraryLimsKey}
	default:
		return nil
	}
}

func registrationSeqmetaFlagName(metaKey string) string {
	switch metaKey {
	case SeqmetaIDStudyLimsKey,
		SeqmetaStudyAccessionKey,
		SeqmetaStudyUUIDKey,
		SeqmetaStudyNameKey,
		LegacySeqmetaStudyIDKey,
		"study":
		return "study"
	case SeqmetaSampleNameKey,
		SeqmetaSampleNameURLKey,
		SeqmetaIDSampleLimsKey,
		SeqmetaSangerSampleIDKey,
		SeqmetaSupplierNameKey,
		SeqmetaAccessionNumberKey,
		SeqmetaSampleUUIDKey,
		SeqmetaDonorIDKey,
		LegacySeqmetaSampleIDKey,
		LegacySeqmetaSampleLimsKey,
		"sample":
		return "sample"
	}

	switch metaKey {
	case SeqmetaLibraryIDKey,
		SeqmetaIDLibraryLimsKey,
		SeqmetaPipelineIDLimsKey,
		LegacySeqmetaLibraryIDKey,
		LegacySeqmetaLibraryLimsKey,
		LegacySeqmetaLibraryTypeKey:
		return "library"
	}

	for flagName, key := range registrationLookupFlagMetaKeys {
		if key == metaKey {
			return flagName
		}
	}

	return metaKey
}

// ResolveRegistrationLookupMetadata resolves raw lookup values to canonical
// seqmeta metadata while preserving repeated and source-specific values.
func ResolveRegistrationLookupMetadata(ctx context.Context, resolver RegistrationResolver, values RegistrationLookupValues) (map[string][]string, error) {
	if !values.HasValues() {
		return nil, nil
	}

	metadata := make(map[string][]string, 4)

	for _, trimmedRun := range nonEmptyRegistrationLookupValues(values.Run) {
		resolvedRunID, err := resolveRegistrationRunID(ctx, resolver, trimmedRun)
		if err != nil {
			return nil, err
		}

		appendMetadataValue(metadata, SeqmetaIDRunKey, resolvedRunID)
	}

	for _, trimmedStudy := range nonEmptyRegistrationLookupValues(values.Study) {
		studyMetadata, err := resolveRegistrationStudyMetadata(ctx, resolver, trimmedStudy)
		if err != nil {
			return nil, err
		}

		for key, value := range studyMetadata {
			appendMetadataValue(metadata, key, value)
		}
	}

	for _, trimmedSample := range nonEmptyRegistrationLookupValues(values.Sample) {
		sampleMetadata, err := resolveRegistrationSampleMetadata(ctx, resolver, trimmedSample)
		if err != nil {
			return nil, err
		}

		for key, value := range sampleMetadata {
			appendMetadataValue(metadata, key, value)
		}
	}

	for _, trimmedLibrary := range nonEmptyRegistrationLookupValues(values.Library) {
		libraryMetadata, err := resolveRegistrationLibraryMetadata(ctx, resolver, trimmedLibrary)
		if err != nil {
			return nil, err
		}

		for key, value := range libraryMetadata {
			appendMetadataValue(metadata, key, value)
		}
	}

	return metadata, nil
}

func resolveRegistrationRunID(ctx context.Context, resolver RegistrationResolver, value string) (string, error) {
	match, err := resolver.ResolveRun(ctx, value)
	if err != nil {
		return "", fmt.Errorf("resolve --run %q: %w", value, err)
	}

	return registrationResolvedCanonical("--run", value, match.Canonical)
}

func registrationResolvedCanonical(flagName, value, canonical string) (string, error) {
	trimmed := strings.TrimSpace(canonical)
	if trimmed == "" {
		return "", fmt.Errorf("resolve %s %q: %w", flagName, value, mlwh.ErrNotFound)
	}

	return trimmed, nil
}

func resolveRegistrationStudyMetadata(ctx context.Context, resolver RegistrationResolver, value string) (map[string]string, error) {
	match, err := resolver.ResolveStudy(ctx, value)
	if err != nil {
		return nil, fmt.Errorf("resolve --study %q: %w", value, err)
	}

	canonicalStudyID, err := registrationResolvedCanonical("--study", value, match.Canonical)
	if err != nil {
		return nil, err
	}

	metadata := map[string]string{SeqmetaIDStudyLimsKey: canonicalStudyID}
	trimmedValue := strings.TrimSpace(value)
	if sourceKey := registrationStudySourceMetadataKey(match.Kind); sourceKey != "" && trimmedValue != "" {
		metadata[sourceKey] = trimmedValue
		if !strings.EqualFold(trimmedValue, canonicalStudyID) {
			metadata["study"] = trimmedValue
		}
	}

	return metadata, nil
}

func registrationStudySourceMetadataKey(kind mlwh.IdentifierKind) string {
	switch kind {
	case mlwh.KindStudyAccession:
		return SeqmetaStudyAccessionKey
	case mlwh.KindStudyUUID:
		return SeqmetaStudyUUIDKey
	case mlwh.KindStudyName:
		return SeqmetaStudyNameKey
	default:
		return ""
	}
}

func resolveRegistrationSampleMetadata(ctx context.Context, resolver RegistrationResolver, value string) (map[string]string, error) {
	match, err := resolveRegistrationSample(ctx, resolver, value)
	if err != nil {
		return nil, err
	}

	canonicalSampleName, err := registrationResolvedCanonical("--sample", value, match.Canonical)
	if err != nil {
		return nil, err
	}

	metadata := map[string]string{SeqmetaSampleNameKey: canonicalSampleName}
	trimmedValue := strings.TrimSpace(value)
	if sourceKey := registrationSampleSourceMetadataKey(match.Kind); sourceKey != "" && trimmedValue != "" {
		metadata[sourceKey] = trimmedValue
	}
	if trimmedValue != "" && !strings.EqualFold(trimmedValue, canonicalSampleName) {
		metadata["sample"] = trimmedValue
	}

	return metadata, nil
}

func registrationSampleSourceMetadataKey(kind mlwh.IdentifierKind) string {
	switch kind {
	case mlwh.KindSampleLimsID:
		return SeqmetaIDSampleLimsKey
	case mlwh.KindSangerSampleID:
		return SeqmetaSangerSampleIDKey
	case mlwh.KindSupplierName:
		return SeqmetaSupplierNameKey
	case mlwh.KindSampleAccession:
		return SeqmetaAccessionNumberKey
	case mlwh.KindSampleUUID:
		return SeqmetaSampleUUIDKey
	case mlwh.KindDonorID:
		return SeqmetaDonorIDKey
	default:
		return ""
	}
}

func resolveRegistrationSample(ctx context.Context, resolver RegistrationResolver, value string) (mlwh.Match, error) {
	if nameResolver, ok := resolver.(registrationSampleNameResolver); ok {
		match, err := nameResolver.ResolveSampleName(ctx, value)
		if err == nil {
			return match, nil
		}
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			return mlwh.Match{}, fmt.Errorf("resolve --sample %q: %w", value, err)
		}
		if !errors.Is(err, mlwh.ErrNotFound) {
			return mlwh.Match{}, fmt.Errorf("resolve --sample %q: %w", value, err)
		}
	}

	match, err := resolver.ResolveSample(ctx, value)
	if err != nil {
		return mlwh.Match{}, fmt.Errorf("resolve --sample %q: %w", value, err)
	}

	return match, nil
}

func resolveRegistrationLibraryMetadata(ctx context.Context, resolver RegistrationResolver, value string) (map[string]string, error) {
	match, err := resolveRegistrationLibrary(ctx, resolver, value)
	if err != nil {
		return nil, fmt.Errorf("resolve --library %q: %w", value, err)
	}

	metadata := make(map[string]string, 2)
	if libraryType := strings.TrimSpace(matchRegistrationLibraryType(match)); libraryType != "" {
		metadata[SeqmetaPipelineIDLimsKey] = libraryType
	}

	switch match.Kind {
	case mlwh.KindLibraryID:
		libraryID, err := registrationResolvedCanonical("--library", value, match.Canonical)
		if err != nil {
			return nil, err
		}
		metadata[SeqmetaLibraryIDKey] = libraryID
	case mlwh.KindLibraryLimsID:
		libraryLimsID, err := registrationResolvedCanonical("--library", value, match.Canonical)
		if err != nil {
			return nil, err
		}
		metadata[SeqmetaIDLibraryLimsKey] = libraryLimsID
	default:
		if libraryID := matchingRegistrationLibraryID(value, match.Library); libraryID != "" {
			metadata[SeqmetaLibraryIDKey] = libraryID

			return metadata, nil
		}
		if libraryLimsID := matchingRegistrationLibraryLimsID(value, match.Library); libraryLimsID != "" {
			metadata[SeqmetaIDLibraryLimsKey] = libraryLimsID

			return metadata, nil
		}

		libraryType, err := registrationResolvedCanonical("--library", value, match.Canonical)
		if err != nil {
			return nil, err
		}
		metadata[SeqmetaPipelineIDLimsKey] = libraryType
	}

	return metadata, nil
}

func matchRegistrationLibraryType(match mlwh.Match) string {
	if match.Library != nil {
		return strings.TrimSpace(match.Library.PipelineIDLims)
	}
	if match.Kind == mlwh.KindLibraryType {
		return strings.TrimSpace(match.Canonical)
	}

	return ""
}

func matchingRegistrationLibraryID(value string, library *mlwh.Library) string {
	if library == nil {
		return ""
	}

	trimmed := strings.TrimSpace(library.LibraryID)
	if strings.EqualFold(strings.TrimSpace(value), trimmed) {
		return trimmed
	}

	return ""
}

func matchingRegistrationLibraryLimsID(value string, library *mlwh.Library) string {
	if library == nil {
		return ""
	}

	trimmed := strings.TrimSpace(library.IDLibraryLims)
	if strings.EqualFold(strings.TrimSpace(value), trimmed) {
		return trimmed
	}

	return ""
}

func resolveRegistrationLibrary(ctx context.Context, resolver RegistrationResolver, value string) (mlwh.Match, error) {
	if identifierResolver, ok := resolver.(registrationLibraryIdentifierResolver); ok {
		match, err := identifierResolver.ResolveLibraryIdentifier(ctx, value)
		if err == nil {
			return match, nil
		}
		if !errors.Is(err, mlwh.ErrNotFound) {
			return mlwh.Match{}, err
		}
	}

	return resolver.ResolveLibrary(ctx, value)
}

type registrationSampleNameResolver interface {
	ResolveSampleName(context.Context, string) (mlwh.Match, error)
}

type registrationLibraryIdentifierResolver interface {
	ResolveLibraryIdentifier(context.Context, string) (mlwh.Match, error)
}

// HasValues reports whether any lookup values are present.
func (v RegistrationLookupValues) HasValues() bool {
	return len(nonEmptyRegistrationLookupValues(v.Run)) > 0 ||
		len(nonEmptyRegistrationLookupValues(v.Study)) > 0 ||
		len(nonEmptyRegistrationLookupValues(v.Sample)) > 0 ||
		len(nonEmptyRegistrationLookupValues(v.Library)) > 0
}
