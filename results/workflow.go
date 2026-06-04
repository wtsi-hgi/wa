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
	workflowGitHubAPIBaseURL = "https://api.github.com"
	workflowHTTPClient       = &http.Client{Timeout: 10 * time.Second}
)

var (
	errGitHubNotFound            = errors.New("not found")
	errNextflowWorkflowUnmatched = errors.New("nextflow workflow not matched")
)

type workflowResolver func(string) (WorkflowIdentity, bool, error)

var workflowResolvers = []workflowResolver{
	resolveNextflowWorkflowIdentity,
}

type githubWorkflowReference struct {
	owner        string
	repo         string
	ref          string
	workflowPath string
}

func nextflowGitHubReference(workflow string) (githubWorkflowReference, bool) {
	trimmed := strings.TrimSpace(workflow)
	if trimmed == "" {
		return githubWorkflowReference{}, false
	}

	if parsed, err := url.Parse(trimmed); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return githubWorkflowReferenceFromURL(parsed)
	}

	if !looksLikeGitHubShorthand(trimmed) {
		return githubWorkflowReference{}, false
	}

	if _, err := os.Stat(trimmed); err == nil || !errors.Is(err, os.ErrNotExist) {
		return githubWorkflowReference{}, false
	}

	parts := strings.Split(trimmed, "/")

	return githubWorkflowReference{
		owner:        parts[0],
		repo:         strings.TrimSuffix(parts[1], ".git"),
		workflowPath: "main.nf",
	}, true
}

func githubWorkflowReferenceFromURL(parsed *url.URL) (githubWorkflowReference, bool) {
	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return githubWorkflowReference{}, false
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return githubWorkflowReference{}, false
	}

	ref := githubWorkflowReference{
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

func verifyGitHubWorkflow(ref githubWorkflowReference) error {
	endpoint := "/repos/" + url.PathEscape(ref.owner) + "/" + url.PathEscape(ref.repo) + "/contents/" + pathEscapeGitHubContentPath(ref.workflowPath)
	query := url.Values{"ref": []string{ref.ref}}

	if err := readGitHubJSON(endpoint+"?"+query.Encode(), nil); err != nil {
		return fmt.Errorf("resolve GitHub Nextflow workflow file: %w", err)
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
	endpoint := strings.TrimRight(workflowGitHubAPIBaseURL, "/") + path
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := workflowHTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusNotFound {
		return errGitHubNotFound
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status %d", response.StatusCode)
	}

	if target == nil {
		return nil
	}

	return json.NewDecoder(response.Body).Decode(target)
}

type githubRepositoryResponse struct {
	DefaultBranch string `json:"default_branch"`
	FullName      string `json:"full_name"`
}

func readGitHubRepository(ref githubWorkflowReference) (githubRepositoryResponse, error) {
	var repository githubRepositoryResponse
	err := readGitHubJSON("/repos/"+url.PathEscape(ref.owner)+"/"+url.PathEscape(ref.repo), &repository)
	if err != nil {
		return githubRepositoryResponse{}, fmt.Errorf("resolve GitHub Nextflow workflow: %w", err)
	}

	return repository, nil
}

type githubCommitResponse struct {
	SHA string `json:"sha"`
}

func readGitHubCommit(ref githubWorkflowReference) (githubCommitResponse, error) {
	var commit githubCommitResponse
	err := readGitHubJSON("/repos/"+url.PathEscape(ref.owner)+"/"+url.PathEscape(ref.repo)+"/commits/"+url.PathEscape(ref.ref), &commit)
	if err != nil {
		return githubCommitResponse{}, fmt.Errorf("resolve GitHub Nextflow workflow commit: %w", err)
	}

	if commit.SHA == "" {
		return githubCommitResponse{}, errors.New("resolve GitHub Nextflow workflow commit: empty commit sha")
	}

	return commit, nil
}

// WorkflowIdentity is the resolved identity used to key a result registration.
type WorkflowIdentity struct {
	Identifier string
	Name       string
	Version    string
	LocalPath  string
}

// ResolveWorkflowIdentity resolves manager-specific workflow references before
// falling back to the trimmed raw workflow string.
func ResolveWorkflowIdentity(workflow string) (WorkflowIdentity, error) {
	trimmed := strings.TrimSpace(workflow)
	if trimmed == "" {
		return WorkflowIdentity{}, errors.New("workflow is required")
	}

	for _, resolver := range workflowResolvers {
		identity, matched, err := resolver(trimmed)
		if err != nil {
			return WorkflowIdentity{}, err
		}

		if matched {
			return identity, nil
		}
	}

	return rawWorkflowIdentity(trimmed), nil
}

func rawWorkflowIdentity(workflow string) WorkflowIdentity {
	return WorkflowIdentity{
		Identifier: workflow,
		Name:       workflow,
		Version:    workflow,
	}
}

func resolveNextflowWorkflowIdentity(workflow string) (WorkflowIdentity, bool, error) {
	if ref, ok := nextflowGitHubReference(workflow); ok {
		identity, err := resolveGitHubNextflowWorkflow(ref)
		if errors.Is(err, errNextflowWorkflowUnmatched) {
			return WorkflowIdentity{}, false, nil
		}

		if err != nil {
			return WorkflowIdentity{}, true, err
		}

		return identity, true, nil
	}

	if !looksLikeLocalNextflowWorkflow(workflow) {
		return WorkflowIdentity{}, false, nil
	}

	identity, err := resolveLocalWorkflowFile(workflow)
	if err != nil {
		return WorkflowIdentity{}, true, fmt.Errorf("resolve local Nextflow workflow: %w", err)
	}

	return identity, true, nil
}

func resolveLocalWorkflowFile(workflowPath string) (WorkflowIdentity, error) {
	absPath, err := filepath.Abs(workflowPath)
	if err != nil {
		return WorkflowIdentity{}, fmt.Errorf("resolve workflow path: %w", err)
	}

	absPath = filepath.Clean(absPath)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return WorkflowIdentity{}, fmt.Errorf("read workflow file: %w", err)
	}

	workflowDir := filepath.Dir(absPath)
	hash := sha256.Sum256(content)
	contentVersion := hex.EncodeToString(hash[:])

	repoRoot, repoErr := gitOutput(workflowDir, "rev-parse", "--show-toplevel")
	if repoErr == nil {
		return workflowIdentityFromGitFile(absPath, repoRoot, contentVersion), nil
	}

	return WorkflowIdentity{
		Identifier: absPath,
		Name:       filepath.Base(workflowDir),
		Version:    contentVersion,
		LocalPath:  absPath,
	}, nil
}

func workflowIdentityFromGitFile(absPath, repoRoot, contentVersion string) WorkflowIdentity {
	repoRoot = filepath.Clean(repoRoot)
	name := filepath.Base(repoRoot)
	relWorkflowPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
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
		version = contentVersion
	}

	return WorkflowIdentity{
		Identifier: identifier,
		Name:       name,
		Version:    version,
		LocalPath:  absPath,
	}
}

func resolveGitHubNextflowWorkflow(ref githubWorkflowReference) (WorkflowIdentity, error) {
	repository, err := readGitHubRepository(ref)
	if errors.Is(err, errGitHubNotFound) {
		return WorkflowIdentity{}, errNextflowWorkflowUnmatched
	}

	if err != nil {
		return WorkflowIdentity{}, err
	}

	if ref.ref == "" {
		ref.ref = repository.DefaultBranch
	}

	if ref.ref == "" {
		return WorkflowIdentity{}, fmt.Errorf("resolve GitHub Nextflow workflow: repository %s/%s has no default branch", ref.owner, ref.repo)
	}

	if err := verifyGitHubWorkflow(ref); errors.Is(err, errGitHubNotFound) {
		return WorkflowIdentity{}, errNextflowWorkflowUnmatched
	} else if err != nil {
		return WorkflowIdentity{}, err
	}

	commit, err := readGitHubCommit(ref)
	if err != nil {
		return WorkflowIdentity{}, err
	}

	fullName := repository.FullName
	if fullName == "" {
		fullName = ref.owner + "/" + ref.repo
	}

	identifier := "https://github.com/" + fullName + "::" + ref.workflowPath

	return WorkflowIdentity{
		Identifier: identifier,
		Name:       fullName,
		Version:    commit.SHA,
	}, nil
}

func looksLikeLocalNextflowWorkflow(workflow string) bool {
	if strings.Contains(workflow, "://") {
		return false
	}

	return strings.EqualFold(filepath.Ext(filepath.Base(workflow)), ".nf")
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
