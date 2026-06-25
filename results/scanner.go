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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const scanDirectoryWarningThreshold = 10000

// ScanWarningReason identifies why a scan path was skipped.
type ScanWarningReason string

const (
	// ScanWarningEscapedDirectorySymlink means a directory symlink resolved outside the scan root.
	ScanWarningEscapedDirectorySymlink ScanWarningReason = "escaped_directory_symlink"
)

// ScanWarning describes a path skipped while scanning output files.
type ScanWarning struct {
	Path   string
	Target string
	Reason ScanWarningReason
}

// ScanDirectoryWithWarnings recursively scans a directory and returns output file entries with skip warnings.
func ScanDirectoryWithWarnings(dir string, includeHidden bool, matchPatterns ...string) ([]FileEntry, []ScanWarning, error) {
	rootPath, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, err
	}

	rootInfo, err := os.Stat(rootPath)
	if err != nil {
		return nil, nil, err
	}

	if !rootInfo.IsDir() {
		return nil, nil, fmt.Errorf("scan directory %q: not a directory", rootPath)
	}

	normalizedMatchPatterns, err := normalizeScanMatchPatterns(matchPatterns)
	if err != nil {
		return nil, nil, err
	}

	resolvedRoot, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return nil, nil, err
	}

	entries := make([]FileEntry, 0)
	warnings := make([]ScanWarning, 0)
	visitedDirs := map[string]struct{}{resolvedRoot: {}}

	err = scanDirectoryTree(rootPath, rootPath, resolvedRoot, includeHidden, normalizedMatchPatterns, visitedDirs, &entries, &warnings, true)
	if err != nil {
		return nil, warnings, err
	}

	if len(entries) > scanDirectoryWarningThreshold {
		_, _ = fmt.Fprintf(os.Stderr, "results: scanned %d files in %s\n", len(entries), rootPath)
	}

	return entries, warnings, nil
}

func appendScanWarning(warnings *[]ScanWarning, warning ScanWarning) {
	*warnings = append(*warnings, warning)
}

// ScanDirectory recursively scans a directory and returns output file entries.
func ScanDirectory(dir string, includeHidden bool, matchPatterns ...string) ([]FileEntry, int, error) {
	entries, warnings, err := ScanDirectoryWithWarnings(dir, includeHidden, matchPatterns...)

	return entries, len(warnings), err
}

func normalizeScanMatchPatterns(matchPatterns []string) ([]string, error) {
	if len(matchPatterns) == 0 {
		return nil, nil
	}

	normalized := make([]string, 0, len(matchPatterns))
	for _, pattern := range matchPatterns {
		slashPattern := filepath.ToSlash(pattern)
		if _, err := path.Match(slashPattern, ""); err != nil {
			return nil, fmt.Errorf("invalid match glob %q: %w", pattern, err)
		}

		normalized = append(normalized, slashPattern)
	}

	return normalized, nil
}

func scanDirectoryTree(
	rootPath string,
	dir string,
	resolvedRoot string,
	includeHidden bool,
	matchPatterns []string,
	visitedDirs map[string]struct{},
	entries *[]FileEntry,
	warnings *[]ScanWarning,
	isRoot bool,
) error {
	children, err := os.ReadDir(dir)
	if err != nil {
		if isRoot {
			return err
		}

		appendScanWarning(warnings, ScanWarning{})

		return nil
	}

	for _, child := range children {
		name := child.Name()
		if !includeHidden && isHiddenName(name) {
			continue
		}

		childPath := filepath.Join(dir, name)
		info, err := os.Stat(childPath)
		if err != nil {
			if os.IsNotExist(err) {
				appendScanWarning(warnings, ScanWarning{})
				continue
			}

			if isSymlinkLoopError(err) {
				appendScanWarning(warnings, ScanWarning{})
				continue
			}

			appendScanWarning(warnings, ScanWarning{})

			continue
		}

		if info.IsDir() {
			resolvedPath, err := filepath.EvalSymlinks(childPath)
			if err != nil {
				if isSymlinkLoopError(err) {
					appendScanWarning(warnings, ScanWarning{})
					continue
				}

				appendScanWarning(warnings, ScanWarning{})

				continue
			}

			if !pathWithinDirectory(resolvedRoot, resolvedPath) {
				appendScanWarning(warnings, ScanWarning{
					Path:   childPath,
					Target: resolvedPath,
					Reason: ScanWarningEscapedDirectorySymlink,
				})

				continue
			}

			if _, seen := visitedDirs[resolvedPath]; seen {
				appendScanWarning(warnings, ScanWarning{})
				continue
			}

			visitedDirs[resolvedPath] = struct{}{}

			err = scanDirectoryTree(rootPath, childPath, resolvedRoot, includeHidden, matchPatterns, visitedDirs, entries, warnings, false)
			if err != nil {
				return err
			}

			continue
		}

		absPath, err := filepath.Abs(childPath)
		if err != nil {
			return err
		}

		matches, err := scanFileMatches(rootPath, absPath, matchPatterns)
		if err != nil {
			return err
		}

		if !matches {
			continue
		}

		*entries = append(*entries, FileEntry{
			Path:  absPath,
			Mtime: info.ModTime(),
			Size:  info.Size(),
			Kind:  "output",
		})
	}

	return nil
}

func isHiddenName(name string) bool {
	return strings.HasPrefix(name, ".")
}

func isSymlinkLoopError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "too many links") || strings.Contains(message, "too many levels of symbolic links")
}

func scanFileMatches(rootPath string, filePath string, matchPatterns []string) (bool, error) {
	if len(matchPatterns) == 0 {
		return true, nil
	}

	relPath, err := filepath.Rel(rootPath, filePath)
	if err != nil {
		return false, err
	}

	slashRelPath := filepath.ToSlash(relPath)
	for _, pattern := range matchPatterns {
		matches, err := path.Match(pattern, slashRelPath)
		if err != nil {
			return false, err
		}

		if matches {
			return true, nil
		}
	}

	return false, nil
}
