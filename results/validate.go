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
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var validFileKinds = map[string]struct{}{
	"input":    {},
	"output":   {},
	"pipeline": {},
}

// NewSeqmetaValidator constructs a seqmeta validator for the given service URL.
func NewSeqmetaValidator(baseURL string, timeout time.Duration) *SeqmetaValidator {
	return &SeqmetaValidator{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func seqmetaMetadataKeys(metadata map[string]string) []string {
	seqmetaKeys := make([]string, 0, len(metadata))

	for key := range maps.Keys(metadata) {
		if strings.HasPrefix(key, "seqmeta_") {
			seqmetaKeys = append(seqmetaKeys, key)
		}
	}

	slices.Sort(seqmetaKeys)

	return seqmetaKeys
}

// ValidateMetadata checks all seqmeta_* fields in metadata.
func (v *SeqmetaValidator) ValidateMetadata(ctx context.Context, metadata map[string]string) error {
	if v == nil || v.baseURL == "" {
		return nil
	}

	for _, key := range seqmetaMetadataKeys(metadata) {
		suffix := strings.TrimPrefix(key, "seqmeta_")
		expectedType, ok := SeqmetaFieldTypes[suffix]
		if !ok {
			return fmt.Errorf("%w: unknown seqmeta field %q", ErrInvalidInput, key)
		}

		if err := v.validateIdentifier(ctx, metadata[key], expectedType); err != nil {
			return fmt.Errorf("%s=%q: %w", key, metadata[key], err)
		}
	}

	return nil
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
		return fmt.Errorf("%w: run key is required", ErrInvalidInput)
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

type seqmetaValidationResponse struct {
	Type string `json:"type"`
}

func (v *SeqmetaValidator) validateIdentifier(ctx context.Context, identifier, expectedType string) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		v.baseURL+"/validate/"+url.PathEscape(identifier),
		nil,
	)
	if err != nil {
		return fmt.Errorf("%w: build request: %w", ErrSeqmetaFailed, err)
	}

	response, err := v.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: request seqmeta: %w", ErrSeqmetaFailed, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: identifier not found", ErrSeqmetaRejected)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: unexpected status %d", ErrSeqmetaFailed, response.StatusCode)
	}

	var payload seqmetaValidationResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return fmt.Errorf("%w: decode response: %w", ErrSeqmetaFailed, err)
	}

	if payload.Type != expectedType {
		return fmt.Errorf("%w: expected %q, got %q", ErrSeqmetaRejected, expectedType, payload.Type)
	}

	return nil
}

func validateTrackedFiles(files []FileEntry) error {
	seenPaths := make(map[string]struct{}, len(files))
	seenResolvedPaths := make(map[string]string, len(files))

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

		if resolvedPath, resolved := resolveExistingPath(cleanPath); resolved {
			if existingPath, seen := seenResolvedPaths[resolvedPath]; seen {
				return fmt.Errorf("%w: duplicate file target %q via %q", ErrInvalidInput, file.Path, existingPath)
			}

			seenResolvedPaths[resolvedPath] = cleanPath
		}

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
			resolvedFilePath, fileResolved := resolveExistingPath(absPath)
			if !baseResolved || !fileResolved || pathWithinDirectory(resolvedBasePath, resolvedFilePath) {
				continue
			}
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
