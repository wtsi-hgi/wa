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
	"encoding/json"
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

var (
	detectPipelineGitHubAPIBaseURL = "https://api.github.com"
	detectPipelineHTTPClient       = &http.Client{Timeout: 10 * time.Second}
)

type githubPipelineReference struct {
	owner        string
	repo         string
	ref          string
	workflowPath string
}

type githubRepositoryResponse struct {
	DefaultBranch string `json:"default_branch"`
	FullName      string `json:"full_name"`
}

type githubCommitResponse struct {
	SHA string `json:"sha"`
}

const (
	// SeqmetaIDRunKey stores an MLWH id_run value.
	SeqmetaIDRunKey = "seqmeta_id_run"
	// SeqmetaIDStudyLimsKey stores an MLWH id_study_lims value.
	SeqmetaIDStudyLimsKey = "seqmeta_id_study_lims"
	// SeqmetaSampleNameKey stores the MLWH sample name value used as the
	// canonical sample identity for sample-scoped result metadata.
	SeqmetaSampleNameKey = "seqmeta_name"
	// SeqmetaSampleNameURLKey is the precise frontend query/display alias for
	// sample-name metadata. Stored result metadata may still use
	// SeqmetaSampleNameKey.
	SeqmetaSampleNameURLKey = "seqmeta_sample_name"
	// SeqmetaIDSampleLimsKey stores an MLWH id_sample_lims value.
	SeqmetaIDSampleLimsKey = "seqmeta_id_sample_lims"
	// SeqmetaSangerSampleIDKey stores an MLWH sanger_sample_id value.
	SeqmetaSangerSampleIDKey = "seqmeta_sanger_sample_id"
	// SeqmetaSupplierNameKey stores an MLWH supplier_name value.
	SeqmetaSupplierNameKey = "seqmeta_supplier_name"
	// SeqmetaAccessionNumberKey stores an MLWH sample accession_number value.
	SeqmetaAccessionNumberKey = "seqmeta_accession_number"
	// SeqmetaPipelineIDLimsKey stores an MLWH pipeline_id_lims value.
	SeqmetaPipelineIDLimsKey = "seqmeta_pipeline_id_lims"
	// SeqmetaLibraryIDKey stores an MLWH library_id value.
	SeqmetaLibraryIDKey = "seqmeta_library_id"
	// SeqmetaIDLibraryLimsKey stores an MLWH id_library_lims value.
	SeqmetaIDLibraryLimsKey = "seqmeta_id_library_lims"

	// Legacy seqmeta keys remain supported for existing result databases and
	// URLs, but new registrations use the MLWH-named keys above.
	LegacySeqmetaRunIDKey       = "seqmeta_runid"
	LegacySeqmetaStudyIDKey     = "seqmeta_studyid"
	LegacySeqmetaSampleIDKey    = "seqmeta_sampleid"
	LegacySeqmetaSampleLimsKey  = "seqmeta_sample_lims"
	LegacySeqmetaLibraryKey     = "seqmeta_library"
	LegacySeqmetaLibraryIDKey   = "seqmeta_libraryid"
	LegacySeqmetaLibraryLimsKey = "seqmeta_library_lims"
	LegacySeqmetaLibraryTypeKey = "seqmeta_librarytype"
)

// SeqmetaFieldTypes maps metadata key suffixes to expected seqmeta identifier types.
var SeqmetaFieldTypes = map[string]string{
	"id_run":           "run_id",
	"runid":            "run_id",
	"id_study_lims":    "study_lims_id",
	"studyid":          "study_lims_id",
	"name":             "sanger_sample_name",
	"sampleid":         "sanger_sample_name",
	"id_sample_lims":   "sample_lims_id",
	"sample_lims":      "sample_lims_id",
	"sanger_sample_id": "sanger_sample_id",
	"library":          "library_type",
	"library_id":       "library_id",
	"libraryid":        "library_id",
	"id_library_lims":  "id_library_lims",
	"library_lims":     "id_library_lims",
	"librarytype":      "library_type",
	"pipeline_id_lims": "library_type",
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
	OutputDirectoryGID *int64            `json:"output_directory_gid"`
	Metadata           map[string]string `json:"metadata"`
	Access             AccessState       `json:"access"`
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
	OutputDirectoryGID *int64            `json:"-"`
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
	if ref, ok := remotePipelineReference(workflowPath); ok {
		return detectGitHubPipeline(ref)
	}

	return detectLocalPipeline(workflowPath)
}

func detectLocalPipeline(workflowPath string) (string, string, string, error) {
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

// RemotePipelineReference reports whether workflowPath names an online
// Nextflow workflow rather than a local workflow file.
func RemotePipelineReference(workflowPath string) bool {
	_, ok := remotePipelineReference(workflowPath)

	return ok
}

func remotePipelineReference(workflowPath string) (githubPipelineReference, bool) {
	trimmed := strings.TrimSpace(workflowPath)
	if trimmed == "" {
		return githubPipelineReference{}, false
	}

	if parsed, err := url.Parse(trimmed); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return githubReferenceFromURL(parsed)
	}

	if !looksLikeGitHubShorthand(trimmed) {
		return githubPipelineReference{}, false
	}

	if _, err := os.Stat(trimmed); err == nil || !errors.Is(err, os.ErrNotExist) {
		return githubPipelineReference{}, false
	}

	parts := strings.Split(trimmed, "/")

	return githubPipelineReference{
		owner:        parts[0],
		repo:         strings.TrimSuffix(parts[1], ".git"),
		workflowPath: "main.nf",
	}, true
}

func githubReferenceFromURL(parsed *url.URL) (githubPipelineReference, bool) {
	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return githubPipelineReference{}, false
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return githubPipelineReference{}, false
	}

	ref := githubPipelineReference{
		owner:        parts[0],
		repo:         strings.TrimSuffix(parts[1], ".git"),
		workflowPath: "main.nf",
	}

	if len(parts) >= 5 && parts[2] == "blob" {
		ref.ref = parts[3]
		ref.workflowPath = strings.Join(parts[4:], "/")
	} else if len(parts) >= 4 && parts[2] == "tree" {
		ref.ref = parts[3]
	}

	return ref, ref.owner != "" && ref.repo != ""
}

func looksLikeGitHubShorthand(value string) bool {
	if strings.Contains(value, "://") || strings.Contains(value, ":") || filepath.IsAbs(value) {
		return false
	}

	parts := strings.Split(value, "/")

	return len(parts) == 2 && validGitHubPathPart(parts[0]) && validGitHubPathPart(parts[1])
}

func validGitHubPathPart(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `\`)
}

func detectGitHubPipeline(ref githubPipelineReference) (string, string, string, error) {
	repository, err := readGitHubRepository(ref)
	if err != nil {
		return "", "", "", err
	}

	if ref.ref == "" {
		ref.ref = repository.DefaultBranch
	}

	if ref.ref == "" {
		return "", "", "", fmt.Errorf("detect GitHub pipeline: repository %s/%s has no default branch", ref.owner, ref.repo)
	}

	if err := verifyGitHubWorkflow(ref); err != nil {
		return "", "", "", err
	}

	commit, err := readGitHubCommit(ref)
	if err != nil {
		return "", "", "", err
	}

	fullName := repository.FullName
	if fullName == "" {
		fullName = ref.owner + "/" + ref.repo
	}

	identifier := "https://github.com/" + fullName + "::" + ref.workflowPath

	return identifier, fullName, commit.SHA, nil
}

func readGitHubRepository(ref githubPipelineReference) (githubRepositoryResponse, error) {
	var repository githubRepositoryResponse
	err := readGitHubJSON("/repos/"+url.PathEscape(ref.owner)+"/"+url.PathEscape(ref.repo), &repository)
	if err != nil {
		return githubRepositoryResponse{}, fmt.Errorf("detect GitHub pipeline: %w", err)
	}

	return repository, nil
}

func readGitHubCommit(ref githubPipelineReference) (githubCommitResponse, error) {
	var commit githubCommitResponse
	err := readGitHubJSON("/repos/"+url.PathEscape(ref.owner)+"/"+url.PathEscape(ref.repo)+"/commits/"+url.PathEscape(ref.ref), &commit)
	if err != nil {
		return githubCommitResponse{}, fmt.Errorf("detect GitHub pipeline commit: %w", err)
	}

	if commit.SHA == "" {
		return githubCommitResponse{}, errors.New("detect GitHub pipeline commit: empty commit sha")
	}

	return commit, nil
}

func verifyGitHubWorkflow(ref githubPipelineReference) error {
	endpoint := "/repos/" + url.PathEscape(ref.owner) + "/" + url.PathEscape(ref.repo) + "/contents/" + pathEscapeGitHubContentPath(ref.workflowPath)
	query := url.Values{"ref": []string{ref.ref}}

	if err := readGitHubJSON(endpoint+"?"+query.Encode(), nil); err != nil {
		return fmt.Errorf("detect GitHub pipeline workflow: %w", err)
	}

	return nil
}

func pathEscapeGitHubContentPath(workflowPath string) string {
	parts := strings.Split(strings.Trim(workflowPath, "/"), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}

	return strings.Join(parts, "/")
}

func readGitHubJSON(path string, target any) error {
	endpoint := strings.TrimRight(detectPipelineGitHubAPIBaseURL, "/") + path
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := detectPipelineHTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusNotFound {
		return errors.New("not found")
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status %d", response.StatusCode)
	}

	if target == nil {
		return nil
	}

	return json.NewDecoder(response.Body).Decode(target)
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
//
// The primary value is now user-facing as "unique", but the stored query key
// remains "runid" so reruns of existing registrations keep the same ID.
func BuildRunKey(unique, additionalUnique string) string {
	values := url.Values{}

	if unique != "" {
		values.Set("runid", unique)
	}

	if additionalUnique != "" {
		values.Set("unique", additionalUnique)
	}

	return values.Encode()
}

// DisplayRunKeyUnique returns the user-facing unique label for a stored run key.
func DisplayRunKeyUnique(runKey string) string {
	trimmed := strings.TrimSpace(runKey)
	if trimmed == "" || !strings.Contains(trimmed, "=") {
		return trimmed
	}

	values, err := url.ParseQuery(trimmed)
	if err != nil {
		return trimmed
	}

	primary := firstQueryValue(values, "runid")
	additional := firstQueryValue(values, "unique")

	switch {
	case primary != "" && additional != "":
		return primary + " / " + additional
	case primary != "":
		return primary
	case additional != "":
		return additional
	default:
		return trimmed
	}
}

func firstQueryValue(values url.Values, key string) string {
	for _, value := range values[key] {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}
