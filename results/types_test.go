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
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestBuildRunKey(t *testing.T) {
	convey.Convey("A2.1: BuildRunKey keeps the compatibility runid key for the primary unique value", t, func() {
		convey.So(
			BuildRunKey("48522", "random_exon"),
			convey.ShouldEqual,
			"runid=48522&unique=random_exon",
		)
	})

	convey.Convey("A2.2: BuildRunKey omits an empty legacy additional uniqueness value", t, func() {
		convey.So(BuildRunKey("48522", ""), convey.ShouldEqual, "runid=48522")
	})

	convey.Convey("A2.3: BuildRunKey accepts a legacy additional uniqueness value without a primary unique value", t, func() {
		convey.So(BuildRunKey("", "random_exon"), convey.ShouldEqual, "unique=random_exon")
	})

	convey.Convey("A2.4: BuildRunKey returns an empty string when both values are empty", t, func() {
		convey.So(BuildRunKey("", ""), convey.ShouldEqual, "")
	})

	convey.Convey("A2.5: BuildRunKey percent-encodes special characters", t, func() {
		convey.So(BuildRunKey("a&b", "c=d"), convey.ShouldEqual, "runid=a%26b&unique=c%3Dd")
	})
}

func TestDisplayRunKeyUnique(t *testing.T) {
	convey.Convey("Given compatibility run keys, DisplayRunKeyUnique returns user-facing unique labels", t, func() {
		convey.So(DisplayRunKeyUnique("runid=48522"), convey.ShouldEqual, "48522")
		convey.So(DisplayRunKeyUnique("runid=48522&unique=random_exon"), convey.ShouldEqual, "48522 / random_exon")
		convey.So(DisplayRunKeyUnique("unique=random_exon"), convey.ShouldEqual, "random_exon")
		convey.So(DisplayRunKeyUnique("run-1"), convey.ShouldEqual, "run-1")
	})
}

func TestResolveWorkflowIdentity(t *testing.T) {
	convey.Convey("ResolveWorkflowIdentity falls back to the raw workflow string when no manager-specific resolver recognizes it", t, func() {
		identity, err := ResolveWorkflowIdentity(" manual-qc-v1 ")

		convey.So(err, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, "manual-qc-v1")
		convey.So(identity.Name, convey.ShouldEqual, "manual-qc-v1")
		convey.So(identity.Version, convey.ShouldEqual, "manual-qc-v1")
		convey.So(identity.LocalPath, convey.ShouldBeBlank)
	})

	convey.Convey("Resolved generic workflow identity is a deterministic result key component", t, func() {
		identity, err := ResolveWorkflowIdentity("manual-qc-v1")
		convey.So(err, convey.ShouldBeNil)

		first := CompositeKeyID(identity.Identifier, "runid=generic-001")
		second := CompositeKeyID(identity.Identifier, "runid=generic-001")
		differentWorkflow := CompositeKeyID("manual-qc-v2", "runid=generic-001")

		convey.So(first, convey.ShouldEqual, second)
		convey.So(first, convey.ShouldNotEqual, differentWorkflow)
	})
}

func TestCompositeKeyID(t *testing.T) {
	convey.Convey("A1.1: CompositeKeyID returns the expected lowercase SHA256 hex digest", t, func() {
		serialized := "https://github.com/org/nf\x00runid=48522&unique=random_exon"
		id := CompositeKeyID(
			"https://github.com/org/nf",
			"runid=48522&unique=random_exon",
		)
		expected := sha256Hex(serialized)

		convey.So(id, convey.ShouldEqual, expected)
		convey.So(len(id), convey.ShouldEqual, 64)
		convey.So(regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(id), convey.ShouldBeTrue)
	})

	convey.Convey("A1.2: CompositeKeyID is deterministic for identical inputs", t, func() {
		first := CompositeKeyID("pipeline", "runid=48522")
		second := CompositeKeyID("pipeline", "runid=48522")
		expected := sha256Hex("pipeline\x00runid=48522")

		convey.So(first, convey.ShouldEqual, second)
		convey.So(first, convey.ShouldEqual, expected)
	})

	convey.Convey("A1.3: CompositeKeyID uses the null separator in the raw serialized key", t, func() {
		first := CompositeKeyID("ab", "c")
		second := CompositeKeyID("a", "bc")

		convey.So(first, convey.ShouldEqual, sha256Hex("ab\x00c"))
		convey.So(second, convey.ShouldEqual, sha256Hex("a\x00bc"))
		convey.So(first, convey.ShouldNotEqual, second)
	})

	convey.Convey("A1.3: Embedded null bytes do not collide across different natural-key pairs", t, func() {
		first := CompositeKeyID("a\x00b", "c")
		second := CompositeKeyID("a", "b\x00c")

		convey.So(first, convey.ShouldNotEqual, second)
	})
}

func TestResolveWorkflowIdentityNextflow(t *testing.T) {
	convey.Convey("readGitHubJSON sends an explicit User-Agent for GitHub REST API requests", t, func() {
		var userAgent string
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			userAgent = request.Header.Get("User-Agent")
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"ok":true}`))
		}))
		defer server.Close()

		oldAPIBaseURL := workflowGitHubAPIBaseURL
		oldHTTPClient := workflowHTTPClient
		workflowGitHubAPIBaseURL = server.URL
		workflowHTTPClient = server.Client()
		defer func() {
			workflowGitHubAPIBaseURL = oldAPIBaseURL
			workflowHTTPClient = oldHTTPClient
		}()

		err := readGitHubJSON("/rate_limit", nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(userAgent, convey.ShouldEqual, "wa-results-workflow-resolver")
	})

	convey.Convey("ResolveWorkflowIdentity resolves a GitHub repository URL as an online Nextflow workflow", t, func() {
		restore := installGitHubWorkflowServerForTest(t, map[string]string{
			"nf-core/sarek": "abc123",
		})
		defer restore()

		identity, err := ResolveWorkflowIdentity("https://github.com/nf-core/sarek")

		convey.So(err, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, "https://github.com/nf-core/sarek::main.nf")
		convey.So(identity.Name, convey.ShouldEqual, "nf-core/sarek")
		convey.So(identity.Version, convey.ShouldEqual, "abc123")
		convey.So(identity.LocalPath, convey.ShouldBeBlank)
	})

	convey.Convey("ResolveWorkflowIdentity resolves owner/repo shorthand through GitHub when no local path exists", t, func() {
		restore := installGitHubWorkflowServerForTest(t, map[string]string{
			"seqeralabs/nf-hello-world": "def456",
		})
		defer restore()

		identity, err := ResolveWorkflowIdentity("seqeralabs/nf-hello-world")

		convey.So(err, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, "https://github.com/seqeralabs/nf-hello-world::main.nf")
		convey.So(identity.Name, convey.ShouldEqual, "seqeralabs/nf-hello-world")
		convey.So(identity.Version, convey.ShouldEqual, "def456")
		convey.So(identity.LocalPath, convey.ShouldBeBlank)
	})

	convey.Convey("ResolveWorkflowIdentity keeps owner/repo-shaped local paths from being treated as GitHub shorthand", t, func() {
		cwd, err := os.Getwd()
		convey.So(err, convey.ShouldBeNil)

		repoRoot := t.TempDir()
		localWorkflowPath := filepath.Join(repoRoot, "seqeralabs", "nf-hello-world")
		convey.So(os.MkdirAll(filepath.Dir(localWorkflowPath), 0o755), convey.ShouldBeNil)
		writeFileForTest(t, localWorkflowPath, "workflow { }\n")
		convey.So(os.Chdir(repoRoot), convey.ShouldBeNil)
		defer func() {
			_ = os.Chdir(cwd)
		}()

		identity, resolveErr := ResolveWorkflowIdentity("seqeralabs/nf-hello-world")

		convey.So(resolveErr, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, "seqeralabs/nf-hello-world")
		convey.So(identity.LocalPath, convey.ShouldBeBlank)
	})

	convey.Convey("ResolveWorkflowIdentity uses git remote, repo name, and HEAD commit for local Nextflow files inside a git repo", t, func() {
		repoRoot := t.TempDir()
		workflowPath := filepath.Join(repoRoot, "main.nf")

		writeFileForTest(t, workflowPath, "workflow content\n")
		initGitRepoForTest(t, repoRoot)
		runGitForTest(t, repoRoot, "remote", "add", "origin", "https://github.com/org/nf_splicing.git")
		runGitForTest(t, repoRoot, "add", "main.nf")
		runGitForTest(t, repoRoot, "commit", "-m", "initial commit")

		commitHash := runGitForTest(t, repoRoot, "rev-parse", "HEAD")

		identity, err := ResolveWorkflowIdentity(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, "https://github.com/org/nf_splicing.git::main.nf")
		convey.So(identity.Name, convey.ShouldEqual, "nf_splicing")
		convey.So(identity.Version, convey.ShouldEqual, commitHash)
		convey.So(identity.LocalPath, convey.ShouldEqual, filepath.Clean(workflowPath))
	})

	convey.Convey("ResolveWorkflowIdentity derives the repo name correctly from an SSH remote", t, func() {
		repoRoot := t.TempDir()
		workflowPath := filepath.Join(repoRoot, "main.nf")

		writeFileForTest(t, workflowPath, "workflow content\n")
		initGitRepoForTest(t, repoRoot)
		runGitForTest(t, repoRoot, "remote", "add", "origin", "git@github.com:org/nf_splicing.git")
		runGitForTest(t, repoRoot, "add", "main.nf")
		runGitForTest(t, repoRoot, "commit", "-m", "initial commit")

		identity, err := ResolveWorkflowIdentity(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, "git@github.com:org/nf_splicing.git::main.nf")
		convey.So(identity.Name, convey.ShouldEqual, "nf_splicing")
	})

	convey.Convey("ResolveWorkflowIdentity falls back to the absolute repo root when a git repo has no remote", t, func() {
		repoRoot := t.TempDir()
		workflowPath := filepath.Join(repoRoot, "main.nf")

		writeFileForTest(t, workflowPath, "workflow content\n")
		initGitRepoForTest(t, repoRoot)
		runGitForTest(t, repoRoot, "add", "main.nf")
		runGitForTest(t, repoRoot, "commit", "-m", "initial commit")

		commitHash := runGitForTest(t, repoRoot, "rev-parse", "HEAD")

		identity, err := ResolveWorkflowIdentity(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, filepath.Clean(repoRoot)+"::main.nf")
		convey.So(identity.Name, convey.ShouldEqual, filepath.Base(repoRoot))
		convey.So(identity.Version, convey.ShouldEqual, commitHash)
	})

	convey.Convey("ResolveWorkflowIdentity falls back to the workflow content hash when git HEAD cannot be resolved", t, func() {
		repoRoot := t.TempDir()
		workflowPath := filepath.Join(repoRoot, "main.nf")
		content := "workflow content\n"

		writeFileForTest(t, workflowPath, content)
		initGitRepoForTest(t, repoRoot)
		runGitForTest(t, repoRoot, "remote", "add", "origin", "https://github.com/org/nf_splicing.git")

		identity, err := ResolveWorkflowIdentity(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, "https://github.com/org/nf_splicing.git::main.nf")
		convey.So(identity.Name, convey.ShouldEqual, "nf_splicing")
		convey.So(identity.Version, convey.ShouldEqual, sha256Hex(content))
	})

	convey.Convey("ResolveWorkflowIdentity distinguishes multiple workflow files in the same git repository", t, func() {
		repoRoot := t.TempDir()
		mainWorkflowPath := filepath.Join(repoRoot, "main.nf")
		otherWorkflowPath := filepath.Join(repoRoot, "workflows", "alt.nf")

		writeFileForTest(t, mainWorkflowPath, "workflow content\n")
		convey.So(os.MkdirAll(filepath.Dir(otherWorkflowPath), 0o755), convey.ShouldBeNil)
		writeFileForTest(t, otherWorkflowPath, "workflow content\n")
		initGitRepoForTest(t, repoRoot)
		runGitForTest(t, repoRoot, "remote", "add", "origin", "https://github.com/org/nf_splicing.git")
		runGitForTest(t, repoRoot, "add", "main.nf", "workflows/alt.nf")
		runGitForTest(t, repoRoot, "commit", "-m", "initial commit")

		mainIdentity, mainErr := ResolveWorkflowIdentity(mainWorkflowPath)
		otherIdentity, otherErr := ResolveWorkflowIdentity(otherWorkflowPath)

		convey.So(mainErr, convey.ShouldBeNil)
		convey.So(otherErr, convey.ShouldBeNil)
		convey.So(mainIdentity.Identifier, convey.ShouldNotEqual, otherIdentity.Identifier)
	})

	convey.Convey("ResolveWorkflowIdentity hashes file contents when the workflow is not inside a git repo", t, func() {
		workflowDir := filepath.Join(t.TempDir(), "pipeline")
		workflowPath := filepath.Join(workflowDir, "main.nf")
		content := "process ALIGN { }\n"

		convey.So(os.MkdirAll(workflowDir, 0o755), convey.ShouldBeNil)
		writeFileForTest(t, workflowPath, content)

		identity, err := ResolveWorkflowIdentity(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, filepath.Clean(workflowPath))
		convey.So(identity.Name, convey.ShouldEqual, filepath.Base(workflowDir))
		convey.So(identity.Version, convey.ShouldEqual, sha256Hex(content))
		convey.So(identity.LocalPath, convey.ShouldEqual, filepath.Clean(workflowPath))
	})

	convey.Convey("ResolveWorkflowIdentity returns an error for an unreadable local Nextflow workflow file", t, func() {
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeFileForTest(t, workflowPath, "workflow content\n")
		convey.So(os.Chmod(workflowPath, 0), convey.ShouldBeNil)
		defer func() {
			_ = os.Chmod(workflowPath, 0o600)
		}()

		_, err := ResolveWorkflowIdentity(workflowPath)

		convey.So(err, convey.ShouldNotBeNil)
	})

	convey.Convey("ResolveWorkflowIdentity resolves a relative local Nextflow workflow path to an absolute identifier", t, func() {
		cwd, err := os.Getwd()
		convey.So(err, convey.ShouldBeNil)

		workflowDir := t.TempDir()
		workflowPath := filepath.Join(workflowDir, "main.nf")
		content := "nextflow.enable.dsl=2\n"

		writeFileForTest(t, workflowPath, content)
		convey.So(os.Chdir(workflowDir), convey.ShouldBeNil)
		defer func() {
			_ = os.Chdir(cwd)
		}()

		identity, detectErr := ResolveWorkflowIdentity("./main.nf")

		convey.So(detectErr, convey.ShouldBeNil)
		convey.So(identity.Identifier, convey.ShouldEqual, filepath.Clean(workflowPath))
		convey.So(identity.Name, convey.ShouldEqual, filepath.Base(workflowDir))
		convey.So(identity.Version, convey.ShouldEqual, sha256Hex(content))
		convey.So(identity.LocalPath, convey.ShouldEqual, filepath.Clean(workflowPath))
	})
}

func installGitHubWorkflowServerForTest(t *testing.T, versions map[string]string) func() {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		for fullName, sha := range versions {
			switch request.URL.Path {
			case "/repos/" + fullName:
				if err := json.NewEncoder(writer).Encode(map[string]string{
					"default_branch": "main",
					"full_name":      fullName,
				}); err != nil {
					t.Errorf("write repository response: %v", err)
				}

				return
			case "/repos/" + fullName + "/commits/main":
				if err := json.NewEncoder(writer).Encode(map[string]string{
					"sha": sha,
				}); err != nil {
					t.Errorf("write commit response: %v", err)
				}

				return
			case "/repos/" + fullName + "/contents/main.nf":
				if ref := request.URL.Query().Get("ref"); ref != "main" {
					t.Errorf("content request ref = %q, want main", ref)
				}

				if err := json.NewEncoder(writer).Encode(map[string]string{
					"name": "main.nf",
				}); err != nil {
					t.Errorf("write workflow response: %v", err)
				}

				return
			}
		}

		http.NotFound(writer, request)
	}))

	oldAPIBaseURL := workflowGitHubAPIBaseURL
	oldHTTPClient := workflowHTTPClient
	workflowGitHubAPIBaseURL = server.URL
	workflowHTTPClient = server.Client()

	return func() {
		workflowGitHubAPIBaseURL = oldAPIBaseURL
		workflowHTTPClient = oldHTTPClient
		server.Close()
	}
}

func writeFileForTest(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func initGitRepoForTest(t *testing.T, repoRoot string) {
	t.Helper()

	runGitForTest(t, repoRoot, "init")
	runGitForTest(t, repoRoot, "config", "user.name", "Test User")
	runGitForTest(t, repoRoot, "config", "user.email", "test@example.com")
}

func runGitForTest(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}

	return string(bytesTrimSpace(output))
}

func bytesTrimSpace(value []byte) []byte {
	for len(value) > 0 && (value[0] == ' ' || value[0] == '\n' || value[0] == '\r' || value[0] == '\t') {
		value = value[1:]
	}

	for len(value) > 0 {
		last := value[len(value)-1]
		if last != ' ' && last != '\n' && last != '\r' && last != '\t' {
			break
		}

		value = value[:len(value)-1]
	}

	return value
}

func sha256Hex(value string) string {
	hash := sha256.Sum256([]byte(value))

	return hex.EncodeToString(hash[:])
}
