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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestBuildRunKey(t *testing.T) {
	convey.Convey("A2.1: BuildRunKey includes run ID and additional uniqueness in sorted query order", t, func() {
		convey.So(
			BuildRunKey("48522", "random_exon"),
			convey.ShouldEqual,
			"runid=48522&unique=random_exon",
		)
	})

	convey.Convey("A2.2: BuildRunKey omits an empty additional uniqueness value", t, func() {
		convey.So(BuildRunKey("48522", ""), convey.ShouldEqual, "runid=48522")
	})

	convey.Convey("A2.3: BuildRunKey omits an empty run ID", t, func() {
		convey.So(BuildRunKey("", "random_exon"), convey.ShouldEqual, "unique=random_exon")
	})

	convey.Convey("A2.4: BuildRunKey returns an empty string when both values are empty", t, func() {
		convey.So(BuildRunKey("", ""), convey.ShouldEqual, "")
	})

	convey.Convey("A2.5: BuildRunKey percent-encodes special characters", t, func() {
		convey.So(BuildRunKey("a&b", "c=d"), convey.ShouldEqual, "runid=a%26b&unique=c%3Dd")
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

func TestDetectPipeline(t *testing.T) {
	convey.Convey("A3.1: DetectPipeline uses git remote, repo name, and HEAD commit for files inside a git repo", t, func() {
		repoRoot := t.TempDir()
		workflowPath := filepath.Join(repoRoot, "main.nf")

		writeFileForTest(t, workflowPath, "workflow content\n")
		initGitRepoForTest(t, repoRoot)
		runGitForTest(t, repoRoot, "remote", "add", "origin", "https://github.com/org/nf_splicing.git")
		runGitForTest(t, repoRoot, "add", "main.nf")
		runGitForTest(t, repoRoot, "commit", "-m", "initial commit")

		commitHash := runGitForTest(t, repoRoot, "rev-parse", "HEAD")

		identifier, name, version, err := DetectPipeline(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identifier, convey.ShouldEqual, "https://github.com/org/nf_splicing.git::main.nf")
		convey.So(name, convey.ShouldEqual, "nf_splicing")
		convey.So(version, convey.ShouldEqual, commitHash)
	})

	convey.Convey("DetectPipeline derives the repo name correctly from an SSH remote", t, func() {
		repoRoot := t.TempDir()
		workflowPath := filepath.Join(repoRoot, "main.nf")

		writeFileForTest(t, workflowPath, "workflow content\n")
		initGitRepoForTest(t, repoRoot)
		runGitForTest(t, repoRoot, "remote", "add", "origin", "git@github.com:org/nf_splicing.git")
		runGitForTest(t, repoRoot, "add", "main.nf")
		runGitForTest(t, repoRoot, "commit", "-m", "initial commit")

		identifier, name, _, err := DetectPipeline(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identifier, convey.ShouldEqual, "git@github.com:org/nf_splicing.git::main.nf")
		convey.So(name, convey.ShouldEqual, "nf_splicing")
	})

	convey.Convey("A3.2: DetectPipeline falls back to the absolute repo root when a git repo has no remote", t, func() {
		repoRoot := t.TempDir()
		workflowPath := filepath.Join(repoRoot, "main.nf")

		writeFileForTest(t, workflowPath, "workflow content\n")
		initGitRepoForTest(t, repoRoot)
		runGitForTest(t, repoRoot, "add", "main.nf")
		runGitForTest(t, repoRoot, "commit", "-m", "initial commit")

		commitHash := runGitForTest(t, repoRoot, "rev-parse", "HEAD")

		identifier, name, version, err := DetectPipeline(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identifier, convey.ShouldEqual, filepath.Clean(repoRoot)+"::main.nf")
		convey.So(name, convey.ShouldEqual, filepath.Base(repoRoot))
		convey.So(version, convey.ShouldEqual, commitHash)
	})

	convey.Convey("DetectPipeline falls back to the workflow content hash when git HEAD cannot be resolved", t, func() {
		repoRoot := t.TempDir()
		workflowPath := filepath.Join(repoRoot, "main.nf")
		content := "workflow content\n"

		writeFileForTest(t, workflowPath, content)
		initGitRepoForTest(t, repoRoot)
		runGitForTest(t, repoRoot, "remote", "add", "origin", "https://github.com/org/nf_splicing.git")

		identifier, name, version, err := DetectPipeline(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identifier, convey.ShouldEqual, "https://github.com/org/nf_splicing.git::main.nf")
		convey.So(name, convey.ShouldEqual, "nf_splicing")
		convey.So(version, convey.ShouldEqual, sha256Hex(content))
	})

	convey.Convey("DetectPipeline distinguishes multiple workflow files in the same git repository", t, func() {
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

		mainIdentifier, _, _, mainErr := DetectPipeline(mainWorkflowPath)
		otherIdentifier, _, _, otherErr := DetectPipeline(otherWorkflowPath)

		convey.So(mainErr, convey.ShouldBeNil)
		convey.So(otherErr, convey.ShouldBeNil)
		convey.So(mainIdentifier, convey.ShouldNotEqual, otherIdentifier)
	})

	convey.Convey("A3.3: DetectPipeline hashes file contents when the workflow is not inside a git repo", t, func() {
		workflowDir := filepath.Join(t.TempDir(), "pipeline")
		workflowPath := filepath.Join(workflowDir, "main.nf")
		content := "process ALIGN { }\n"

		convey.So(os.MkdirAll(workflowDir, 0o755), convey.ShouldBeNil)
		writeFileForTest(t, workflowPath, content)

		identifier, name, version, err := DetectPipeline(workflowPath)

		convey.So(err, convey.ShouldBeNil)
		convey.So(identifier, convey.ShouldEqual, filepath.Clean(workflowPath))
		convey.So(name, convey.ShouldEqual, filepath.Base(workflowDir))
		convey.So(version, convey.ShouldEqual, sha256Hex(content))
	})

	convey.Convey("A3.4: DetectPipeline returns an error for an unreadable workflow file", t, func() {
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeFileForTest(t, workflowPath, "workflow content\n")
		convey.So(os.Chmod(workflowPath, 0), convey.ShouldBeNil)
		defer func() {
			_ = os.Chmod(workflowPath, 0o600)
		}()

		_, _, _, err := DetectPipeline(workflowPath)

		convey.So(err, convey.ShouldNotBeNil)
	})

	convey.Convey("A3.5: DetectPipeline resolves a relative workflow path to an absolute identifier", t, func() {
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

		identifier, name, version, detectErr := DetectPipeline("./main.nf")

		convey.So(detectErr, convey.ShouldBeNil)
		convey.So(identifier, convey.ShouldEqual, filepath.Clean(workflowPath))
		convey.So(name, convey.ShouldEqual, filepath.Base(workflowDir))
		convey.So(version, convey.ShouldEqual, sha256Hex(content))
	})
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
