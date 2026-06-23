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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestScanDirectory(t *testing.T) {
	convey.Convey("B1.1: ScanDirectory recursively returns absolute output files with size and mtime", t, func() {
		dir := t.TempDir()
		createSizedFile(t, filepath.Join(dir, "a.txt"), 10)
		createSizedFile(t, filepath.Join(dir, "sub", "b.txt"), 20)

		entries, warnings, err := ScanDirectory(dir, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldEqual, 0)
		convey.So(entries, convey.ShouldHaveLength, 2)

		entriesByRelPath := entriesByRelPath(dir, entries)
		convey.So(entriesByRelPath["a.txt"].Kind, convey.ShouldEqual, "output")
		convey.So(entriesByRelPath[filepath.Join("sub", "b.txt")].Kind, convey.ShouldEqual, "output")
		convey.So(entriesByRelPath["a.txt"].Size, convey.ShouldEqual, 10)
		convey.So(entriesByRelPath[filepath.Join("sub", "b.txt")].Size, convey.ShouldEqual, 20)
		convey.So(entriesByRelPath["a.txt"].Mtime.IsZero(), convey.ShouldBeFalse)
		convey.So(entriesByRelPath[filepath.Join("sub", "b.txt")].Mtime.IsZero(), convey.ShouldBeFalse)
		convey.So(filepath.IsAbs(entriesByRelPath["a.txt"].Path), convey.ShouldBeTrue)
		convey.So(filepath.IsAbs(entriesByRelPath[filepath.Join("sub", "b.txt")].Path), convey.ShouldBeTrue)
	})

	convey.Convey("B1.2: ScanDirectory excludes hidden files unless requested", t, func() {
		dir := t.TempDir()
		createSizedFile(t, filepath.Join(dir, ".hidden"), 3)
		createSizedFile(t, filepath.Join(dir, "visible.txt"), 4)

		excludedEntries, excludedWarnings, excludedErr := ScanDirectory(dir, false)
		includedEntries, includedWarnings, includedErr := ScanDirectory(dir, true)

		convey.So(excludedErr, convey.ShouldBeNil)
		convey.So(excludedWarnings, convey.ShouldEqual, 0)
		convey.So(excludedEntries, convey.ShouldHaveLength, 1)
		convey.So(filepath.Base(excludedEntries[0].Path), convey.ShouldEqual, "visible.txt")

		convey.So(includedErr, convey.ShouldBeNil)
		convey.So(includedWarnings, convey.ShouldEqual, 0)
		convey.So(includedEntries, convey.ShouldHaveLength, 2)
	})

	convey.Convey("ScanDirectory filters output files by output-relative glob matches", t, func() {
		dir := t.TempDir()
		createSizedFile(t, filepath.Join(dir, "reports", "summary.html"), 7)
		createSizedFile(t, filepath.Join(dir, "reports", "summary.txt"), 8)
		createSizedFile(t, filepath.Join(dir, "metrics", "qc.json"), 9)
		createSizedFile(t, filepath.Join(dir, "metrics", "qc.tsv"), 10)

		entries, warnings, err := ScanDirectory(dir, false, "reports/*.html", "metrics/*.json")

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldEqual, 0)
		convey.So(entries, convey.ShouldHaveLength, 2)

		entriesByRel := entriesByRelPath(dir, entries)
		convey.So(entriesByRel[filepath.Join("reports", "summary.html")].Kind, convey.ShouldEqual, "output")
		convey.So(entriesByRel[filepath.Join("reports", "summary.html")].Size, convey.ShouldEqual, 7)
		convey.So(entriesByRel[filepath.Join("metrics", "qc.json")].Kind, convey.ShouldEqual, "output")
		convey.So(entriesByRel[filepath.Join("metrics", "qc.json")].Size, convey.ShouldEqual, 9)
		convey.So(entriesByRel[filepath.Join("reports", "summary.txt")].Path, convey.ShouldBeBlank)
		convey.So(entriesByRel[filepath.Join("metrics", "qc.tsv")].Path, convey.ShouldBeBlank)
	})

	convey.Convey("B1.3: ScanDirectory excludes hidden directories and their contents", t, func() {
		dir := t.TempDir()
		createSizedFile(t, filepath.Join(dir, ".hidden_dir", "file.txt"), 5)

		entries, warnings, err := ScanDirectory(dir, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldEqual, 0)
		convey.So(entries, convey.ShouldHaveLength, 0)
	})

	convey.Convey("B1.4: ScanDirectory stats symlinked files via their targets", t, func() {
		dir := t.TempDir()
		realPath := filepath.Join(dir, "real.txt")
		linkPath := filepath.Join(dir, "link.txt")
		createSizedFile(t, realPath, 50)
		mustSymlink(t, realPath, linkPath)

		entries, warnings, err := ScanDirectory(dir, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldEqual, 0)
		convey.So(entries, convey.ShouldHaveLength, 2)

		entriesByName := entriesByName(entries)
		convey.So(entriesByName["link.txt"].Size, convey.ShouldEqual, 50)
		convey.So(entriesByName["link.txt"].Kind, convey.ShouldEqual, "output")
	})

	convey.Convey("B1.5: ScanDirectory skips cyclic symlinks and reports warnings", t, func() {
		dir := t.TempDir()
		mustSymlink(t, filepath.Join(dir, "b"), filepath.Join(dir, "a"))
		mustSymlink(t, filepath.Join(dir, "a"), filepath.Join(dir, "b"))

		entries, warnings, err := ScanDirectory(dir, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(entries, convey.ShouldHaveLength, 0)
		convey.So(warnings, convey.ShouldBeGreaterThanOrEqualTo, 1)
	})

	convey.Convey("ScanDirectory does not duplicate files when the same directory is reachable via a symlink alias", t, func() {
		dir := t.TempDir()
		realDir := filepath.Join(dir, "real")
		aliasDir := filepath.Join(dir, "alias")
		createSizedFile(t, filepath.Join(realDir, "a.txt"), 10)
		mustSymlink(t, realDir, aliasDir)

		entries, warnings, err := ScanDirectory(dir, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldBeGreaterThanOrEqualTo, 1)
		convey.So(entries, convey.ShouldHaveLength, 1)
		convey.So(filepath.Base(entries[0].Path), convey.ShouldEqual, "a.txt")
	})

	convey.Convey("ScanDirectory follows directory symlinks that resolve inside the root while preserving the discovered alias path", t, func() {
		rootDir := t.TempDir()
		realDir := filepath.Join(rootDir, "real")
		aliasDir := filepath.Join(rootDir, "alias")
		createSizedFile(t, filepath.Join(realDir, "nested", "linked.txt"), 13)
		mustSymlink(t, realDir, aliasDir)

		entries, warnings, err := ScanDirectory(rootDir, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldBeGreaterThanOrEqualTo, 1)
		convey.So(entries, convey.ShouldHaveLength, 1)

		entriesByRel := entriesByRelPath(rootDir, entries)
		convey.So(entriesByRel[filepath.Join("alias", "nested", "linked.txt")].Path, convey.ShouldEqual, filepath.Join(rootDir, "alias", "nested", "linked.txt"))
		convey.So(entriesByRel[filepath.Join("real", "nested", "linked.txt")].Path, convey.ShouldBeBlank)
	})

	convey.Convey("ScanDirectory skips directory symlinks that resolve outside the root and reports a warning", t, func() {
		rootDir := t.TempDir()
		externalDir := t.TempDir()
		createSizedFile(t, filepath.Join(rootDir, "local.txt"), 8)
		createSizedFile(t, filepath.Join(externalDir, "external.txt"), 13)
		mustSymlink(t, externalDir, filepath.Join(rootDir, "escape"))

		entries, warnings, err := ScanDirectory(rootDir, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldBeGreaterThanOrEqualTo, 1)
		convey.So(entries, convey.ShouldHaveLength, 1)

		entriesByRel := entriesByRelPath(rootDir, entries)
		convey.So(entriesByRel["local.txt"].Path, convey.ShouldEqual, filepath.Join(rootDir, "local.txt"))
		convey.So(entriesByRel[filepath.Join("escape", "external.txt")].Path, convey.ShouldBeBlank)
	})

	convey.Convey("B1.6: ScanDirectory returns no entries for an empty directory", t, func() {
		entries, warnings, err := ScanDirectory(t.TempDir(), false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldEqual, 0)
		convey.So(entries, convey.ShouldHaveLength, 0)
	})

	convey.Convey("B1.7: ScanDirectory returns an error for a non-existent directory", t, func() {
		entries, warnings, err := ScanDirectory(filepath.Join(t.TempDir(), "missing"), false)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(entries, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldEqual, 0)
	})

	convey.Convey("ScanDirectory skips unreadable nested directories and their contents without aborting the scan", t, func() {
		dir := t.TempDir()
		visiblePath := filepath.Join(dir, "visible.txt")
		unreadableDirPath := filepath.Join(dir, "sealed")

		createSizedFile(t, visiblePath, 7)
		createSizedFile(t, filepath.Join(unreadableDirPath, "hidden.txt"), 9)

		convey.So(os.Chmod(unreadableDirPath, 0o000), convey.ShouldBeNil)
		defer func() {
			_ = os.Chmod(unreadableDirPath, 0o755)
		}()

		entries, warnings, err := ScanDirectory(dir, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldBeGreaterThanOrEqualTo, 1)
		convey.So(entries, convey.ShouldHaveLength, 1)
		convey.So(entries[0].Path, convey.ShouldEqual, visiblePath)
	})

	convey.Convey("ScanDirectory skips unresolved nested directory symlinks without aborting the scan", t, func() {
		dir := t.TempDir()
		visiblePath := filepath.Join(dir, "visible.txt")
		brokenDirLinkPath := filepath.Join(dir, "broken-dir")

		createSizedFile(t, visiblePath, 11)
		mustSymlink(t, filepath.Join(dir, "missing-target"), brokenDirLinkPath)
		convey.So(os.Mkdir(filepath.Join(dir, "subdir"), 0o755), convey.ShouldBeNil)

		entries, warnings, err := ScanDirectory(dir, false)

		convey.So(err, convey.ShouldBeNil)
		convey.So(warnings, convey.ShouldBeGreaterThanOrEqualTo, 1)
		convey.So(entries, convey.ShouldHaveLength, 1)
		convey.So(entries[0].Path, convey.ShouldEqual, visiblePath)
	})
}

func createSizedFile(t *testing.T, path string, size int) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}

	err = os.WriteFile(path, make([]byte, size), 0o644)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	now := time.Now().Add(-1 * time.Minute).Truncate(time.Second)
	err = os.Chtimes(path, now, now)
	if err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func entriesByRelPath(root string, entries []FileEntry) map[string]FileEntry {
	indexed := make(map[string]FileEntry, len(entries))

	for _, entry := range entries {
		if relPath, err := filepath.Rel(root, entry.Path); err == nil {
			indexed[relPath] = entry
		}
	}

	return indexed
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()

	err := os.Symlink(target, link)
	if err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}

func entriesByName(entries []FileEntry) map[string]FileEntry {
	indexed := make(map[string]FileEntry, len(entries))

	for _, entry := range entries {
		indexed[filepath.Base(entry.Path)] = entry
	}

	return indexed
}
