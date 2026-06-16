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

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestMLWHDiffD1CommandContract(t *testing.T) {
	convey.Convey("D1.3: mlwhdiff help lists diff and serve without validate", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwhdiff", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "diff")
		convey.So(output, convey.ShouldContainSubstring, "serve")
		convey.So(output, convey.ShouldNotContainSubstring, "validate")
	})

	convey.Convey("D1.4: mlwhdiff serve defaults to mlwhdiff.db", t, func() {
		command := newMLWHDiffCommand()
		dbFlag := command.PersistentFlags().Lookup("db")

		convey.So(dbFlag, convey.ShouldNotBeNil)
		convey.So(filepath.Base(dbFlag.DefValue), convey.ShouldEqual, "mlwhdiff.db")
	})
}

func TestMLWHDiffD1LegacySourceStrings(t *testing.T) {
	repoRoot := repoRootForD1Test(t)

	convey.Convey("D1.1: source has no seqmeta package declarations", t, func() {
		matches := sourceMatchesForD1Test(t, repoRoot, "package "+"seqmeta")

		convey.So(matches, convey.ShouldBeEmpty)
	})

	convey.Convey("D1.2: Go source has no old CLI command examples", t, func() {
		matches := sourceMatchesForD1Test(t, repoRoot, "wa "+"seqmeta", ".go")

		convey.So(matches, convey.ShouldBeEmpty)
	})

	convey.Convey("D1.5: source has no removed seqmeta backend env var", t, func() {
		matches := sourceMatchesForD1Test(t, repoRoot, "WA_"+"SEQMETA_BACKEND_URL")

		convey.So(matches, convey.ShouldBeEmpty)
	})
}

func repoRootForD1Test(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}

func sourceMatchesForD1Test(t *testing.T, repoRoot string, needle string, extensions ...string) []string {
	t.Helper()

	allowedExtensions := map[string]bool{}
	for _, extension := range extensions {
		allowedExtensions[extension] = true
	}

	matches := []string{}
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if skipSourceDirForD1Test(repoRoot, path) {
				return filepath.SkipDir
			}

			return nil
		}

		if !scanSourceFileForD1Test(path, allowedExtensions) {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if strings.Contains(string(body), needle) {
			relativePath, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			matches = append(matches, filepath.ToSlash(relativePath))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("scan source: %v", err)
	}

	return matches
}

func skipSourceDirForD1Test(repoRoot string, path string) bool {
	relativePath, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return false
	}

	switch filepath.ToSlash(relativePath) {
	case ".docs", ".git", ".tmp", "frontend/.next", "frontend/node_modules":
		return true
	default:
		return false
	}
}

func scanSourceFileForD1Test(path string, allowedExtensions map[string]bool) bool {
	if len(allowedExtensions) > 0 {
		return allowedExtensions[filepath.Ext(path)]
	}

	switch filepath.Ext(path) {
	case ".go", ".js", ".mjs", ".sh", ".ts", ".tsx":
		return true
	default:
		base := filepath.Base(path)

		return strings.HasPrefix(base, ".env")
	}
}
