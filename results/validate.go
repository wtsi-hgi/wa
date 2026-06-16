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
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wtsi-hgi/wa/mlwh"
)

var validFileKinds = map[string]struct{}{
	"input":    {},
	"output":   {},
	"pipeline": {},
}

// NewMLWHValidator constructs a validator backed by an MLWH queryer.
func NewMLWHValidator(q mlwh.Queryer) *MLWHValidator {
	return &MLWHValidator{q: q}
}

func seqmetaMetadataValueKeys(metadata map[string][]string) []string {
	seqmetaKeys := make([]string, 0, len(metadata))

	for key := range metadata {
		if strings.HasPrefix(key, "seqmeta_") {
			seqmetaKeys = append(seqmetaKeys, key)
		}
	}

	slices.Sort(seqmetaKeys)

	return seqmetaKeys
}

// ValidateRegistration checks required registration fields and tracked files.
func ValidateRegistration(reg *Registration) error {
	if reg == nil {
		return fmt.Errorf("%w: nil registration", ErrInvalidInput)
	}

	if strings.TrimSpace(reg.PipelineIdentifier) == "" {
		return fmt.Errorf("%w: pipeline identifier is required", ErrInvalidInput)
	}

	if strings.TrimSpace(reg.RunKey) == "" {
		return fmt.Errorf("%w: unique key is required", ErrInvalidInput)
	}

	if strings.TrimSpace(reg.Requester) == "" {
		return fmt.Errorf("%w: requester is required", ErrInvalidInput)
	}

	if strings.TrimSpace(reg.PipelineName) == "" {
		return fmt.Errorf("%w: pipeline name is required", ErrInvalidInput)
	}

	if strings.TrimSpace(reg.PipelineVersion) == "" {
		return fmt.Errorf("%w: pipeline version is required", ErrInvalidInput)
	}

	if strings.TrimSpace(reg.OutputDirectory) == "" {
		return fmt.Errorf("%w: output directory is required", ErrInvalidInput)
	}

	if !filepath.IsAbs(strings.TrimSpace(reg.OutputDirectory)) {
		return fmt.Errorf("%w: output directory must be absolute", ErrInvalidInput)
	}

	if err := validateTrackedFiles(reg.Files); err != nil {
		return err
	}

	if err := validateOutputFilesWithinDirectory(reg.OutputDirectory, reg.Files); err != nil {
		return err
	}

	return nil
}

func validateTrackedFiles(files []FileEntry) error {
	seenPaths := make(map[string]struct{}, len(files))

	for _, file := range files {
		trimmedPath := strings.TrimSpace(file.Path)
		if trimmedPath == "" {
			return fmt.Errorf("%w: file path is required", ErrInvalidInput)
		}

		if !filepath.IsAbs(trimmedPath) {
			return fmt.Errorf("%w: file path must be absolute", ErrInvalidInput)
		}

		cleanPath := filepath.Clean(trimmedPath)
		if _, seen := seenPaths[cleanPath]; seen {
			return fmt.Errorf("%w: duplicate file path %q", ErrInvalidInput, file.Path)
		}
		seenPaths[cleanPath] = struct{}{}

		if _, ok := validFileKinds[file.Kind]; !ok {
			return fmt.Errorf("%w: invalid file kind %q", ErrInvalidInput, file.Kind)
		}
	}

	return nil
}

func validateOutputFilesWithinDirectory(outputDirectory string, files []FileEntry) error {
	basePath, err := filepath.Abs(strings.TrimSpace(outputDirectory))
	if err != nil {
		return fmt.Errorf("%w: resolve output directory: %v", ErrInvalidInput, err)
	}

	resolvedBasePath, baseResolved := resolveExistingPath(basePath)

	for _, file := range files {
		if file.Kind != "output" {
			continue
		}

		path := strings.TrimSpace(file.Path)
		if path == "" {
			return fmt.Errorf("%w: file path is required", ErrInvalidInput)
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("%w: resolve output file path %q: %v", ErrInvalidInput, file.Path, err)
		}

		rawWithinBase := pathWithinDirectory(basePath, absPath)
		if !rawWithinBase {
			resolvedParentPath, parentResolved := resolveExistingPath(filepath.Dir(absPath))
			if baseResolved && parentResolved && pathWithinDirectory(resolvedBasePath, resolvedParentPath) {
				continue
			}

			return fmt.Errorf("%w: output file %q is outside output directory %q", ErrInvalidInput, file.Path, outputDirectory)
		}

		resolvedParentPath, parentResolved := resolveExistingPath(filepath.Dir(absPath))
		if !baseResolved || !parentResolved || pathWithinDirectory(resolvedBasePath, resolvedParentPath) {
			continue
		}

		return fmt.Errorf("%w: output file %q is outside output directory %q", ErrInvalidInput, file.Path, outputDirectory)
	}

	return nil
}

func pathWithinDirectory(rootPath, candidatePath string) bool {
	relPath, err := filepath.Rel(rootPath, candidatePath)
	if err != nil {
		return false
	}

	return relPath == "." || (relPath != ".." && !strings.HasPrefix(relPath, ".."+string(os.PathSeparator)))
}

func resolveExistingPath(path string) (string, bool) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}

	return resolvedPath, true
}

func mlwhValidationError(err error) error {
	switch {
	case errors.Is(err, mlwh.ErrUpstreamImpaired), errors.Is(err, mlwh.ErrCacheNeverSynced):
		return fmt.Errorf("%w: classify identifier: %w", ErrMLWHFailed, err)
	case errors.Is(err, mlwh.ErrNotFound):
		return fmt.Errorf("%w: identifier not found", ErrMLWHRejected)
	case errors.Is(err, mlwh.ErrUnsupportedIdentifier):
		return fmt.Errorf("%w: %w", ErrMLWHRejected, err)
	default:
		return fmt.Errorf("%w: classify identifier: %w", ErrMLWHFailed, err)
	}
}

// ValidateMetadata checks all seqmeta_* fields in metadata.
func (v *MLWHValidator) ValidateMetadata(ctx context.Context, metadata map[string]string) error {
	return v.ValidateMetadataValues(ctx, metadataValuesFromMap(metadata))
}

// ValidateMetadataValues checks all seqmeta_* values in metadata.
func (v *MLWHValidator) ValidateMetadataValues(ctx context.Context, metadata map[string][]string) error {
	if v == nil || v.q == nil {
		return nil
	}

	for _, key := range seqmetaMetadataValueKeys(metadata) {
		suffix := strings.TrimPrefix(key, "seqmeta_")
		expectedType, ok := SeqmetaFieldTypes[suffix]
		if !ok {
			return fmt.Errorf("%w: unknown seqmeta field %q", ErrInvalidInput, key)
		}

		for _, value := range metadata[key] {
			if err := v.validateIdentifier(ctx, value, expectedType); err != nil {
				return fmt.Errorf("%s=%q: %w", key, value, err)
			}
		}
	}

	return nil
}

func (v *MLWHValidator) validateIdentifier(ctx context.Context, identifier, expectedType string) error {
	match, err := v.q.ClassifyIdentifier(ctx, identifier)
	if err != nil {
		return mlwhValidationError(err)
	}

	actualType := string(match.Kind)
	if actualType != expectedType {
		return fmt.Errorf("%w: expected %q, got %q", ErrMLWHRejected, expectedType, actualType)
	}

	return nil
}
