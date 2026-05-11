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
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("results: not found")

	ErrInvalidInput = errors.New("results: invalid input")

	ErrFileGone = errors.New("results: file not found on disk")

	ErrFileTooLarge = errors.New("results: file exceeds preview limit")

	ErrSeqmetaFailed = errors.New("results: seqmeta unavailable")

	ErrSeqmetaRejected = errors.New("results: seqmeta validation failed")
)

// SeqmetaFieldTypes maps metadata key suffixes to expected seqmeta identifier types.
var SeqmetaFieldTypes = map[string]string{
	"runid":       "run_id",
	"studyid":     "study_id",
	"sampleid":    "sanger_sample_id",
	"librarytype": "library_type",
}

// ResultSet is the core domain object returned by queries.
type ResultSet struct {
	ID                 string            `json:"id"`
	PipelineIdentifier string            `json:"pipeline_identifier"`
	RunKey             string            `json:"run_key"`
	Requester          string            `json:"requester"`
	Operator           string            `json:"operator"`
	Command            string            `json:"command"`
	PipelineName       string            `json:"pipeline_name"`
	PipelineVersion    string            `json:"pipeline_version"`
	OutputDirectory    string            `json:"output_directory"`
	Metadata           map[string]string `json:"metadata"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

// FileEntry represents one tracked file.
type FileEntry struct {
	Path  string    `json:"path"`
	Mtime time.Time `json:"mtime"`
	Size  int64     `json:"size"`
	Kind  string    `json:"kind"`
}

// Registration is the POST /results request body.
type Registration struct {
	PipelineIdentifier string            `json:"pipeline_identifier"`
	RunKey             string            `json:"run_key"`
	Requester          string            `json:"requester"`
	Operator           string            `json:"operator"`
	Command            string            `json:"command"`
	PipelineName       string            `json:"pipeline_name"`
	PipelineVersion    string            `json:"pipeline_version"`
	OutputDirectory    string            `json:"output_directory"`
	Files              []FileEntry       `json:"files"`
	Metadata           map[string]string `json:"metadata"`
}

// SearchParams holds parsed query parameters for filtering.
type SearchParams struct {
	Requester          string
	Operator           string
	PipelineName       string
	PipelineVersion    string
	PipelineIdentifier string
	RunKey             string
	OutputDirPrefix    string
	Meta               map[string]string
}

// MultiSearchParams holds parsed query parameters for multi-value filtering.
type MultiSearchParams struct {
	Requester          []string
	Operator           []string
	PipelineName       []string
	PipelineVersion    []string
	PipelineIdentifier []string
	RunKey             []string
	OutputDirPrefix    []string
	Meta               map[string][]string
	// OrMeta, if non-empty, is a list of single-key meta conditions ORed together.
	// A result must match at least one condition. Each element maps one meta key to
	// one or more values.
	OrMeta []map[string][]string
}

// DailyCount is registrations per day.
type DailyCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// PipelineCount is result sets per pipeline.
type PipelineCount struct {
	PipelineName string `json:"pipeline_name"`
	Count        int    `json:"count"`
}

// StatsResult is returned by GET /results/stats.
type StatsResult struct {
	Total     int             `json:"total"`
	Recent    []ResultSet     `json:"recent"`
	Daily     []DailyCount    `json:"daily"`
	Pipelines []PipelineCount `json:"pipelines"`
}

// SearchResult wraps a ResultSet with optional matched sample IDs.
type SearchResult struct {
	ResultSet      ResultSet `json:"result_set"`
	MatchedSamples []string  `json:"matched_samples,omitempty"`
}

// Store persists result sets in SQL.
type Store struct{ db *sql.DB }

// SeqmetaValidator validates seqmeta_* metadata fields against a remote seqmeta service.
type SeqmetaValidator struct {
	baseURL string
	client  *http.Client
}

// CompositeKeyID returns the deterministic ID for a pipeline identifier and run key.
func CompositeKeyID(pipelineIdentifier, runKey string) string {
	var key []byte
	if strings.ContainsRune(pipelineIdentifier, '\x00') || strings.ContainsRune(runKey, '\x00') {
		key = make([]byte, 0, len(pipelineIdentifier)+len(runKey)+16)
		key = appendLengthPrefixedKeyPart(key, pipelineIdentifier)
		key = appendLengthPrefixedKeyPart(key, runKey)
	} else {
		key = append([]byte(pipelineIdentifier), '\x00')
		key = append(key, runKey...)
	}

	hash := sha256.Sum256(key)

	return hex.EncodeToString(hash[:])
}

func appendLengthPrefixedKeyPart(key []byte, value string) []byte {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	key = append(key, length[:]...)

	return append(key, value...)
}

// DetectPipeline returns pipeline identity derived from git metadata or file content.
func DetectPipeline(workflowPath string) (string, string, string, error) {
	absPath, err := filepath.Abs(workflowPath)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve workflow path: %w", err)
	}

	absPath = filepath.Clean(absPath)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", "", "", fmt.Errorf("read workflow file: %w", err)
	}

	workflowDir := filepath.Dir(absPath)
	hash := sha256.Sum256(content)
	contentVersion := hex.EncodeToString(hash[:])

	repoRoot, repoErr := gitOutput(workflowDir, "rev-parse", "--show-toplevel")
	if repoErr == nil {
		repoRoot = filepath.Clean(repoRoot)
		name := filepath.Base(repoRoot)
		relWorkflowPath, relErr := filepath.Rel(repoRoot, absPath)
		if relErr != nil {
			relWorkflowPath = filepath.Base(absPath)
		}
		relWorkflowPath = filepath.ToSlash(filepath.Clean(relWorkflowPath))

		identifier := repoRoot + "::" + relWorkflowPath

		if remote, remoteErr := gitOutput(repoRoot, "config", "--get", "remote.origin.url"); remoteErr == nil && remote != "" {
			identifier = remote + "::" + relWorkflowPath
			name = repoNameFromIdentifier(remote)
		}

		version, versionErr := gitOutput(repoRoot, "rev-parse", "HEAD")
		if versionErr != nil {
			return identifier, name, contentVersion, nil
		}

		return identifier, name, version, nil
	}

	return absPath, filepath.Base(workflowDir), contentVersion, nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func repoNameFromIdentifier(identifier string) string {
	trimmed := strings.TrimSpace(identifier)
	if !strings.Contains(trimmed, "://") && strings.Contains(trimmed, ":") {
		trimmed = trimmed[strings.LastIndex(trimmed, ":")+1:]
	}

	base := filepath.Base(trimmed)

	return strings.TrimSuffix(base, ".git")
}

// BuildRunKey returns a canonical query-encoded run key from non-empty parts.
func BuildRunKey(runID, additionalUnique string) string {
	values := url.Values{}

	if runID != "" {
		values.Set("runid", runID)
	}

	if additionalUnique != "" {
		values.Set("unique", additionalUnique)
	}

	return values.Encode()
}
