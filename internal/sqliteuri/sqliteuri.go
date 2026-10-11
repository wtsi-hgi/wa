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

// Package sqliteuri builds SQLite "file:" URIs for filesystem paths.
package sqliteuri

import (
	"net/url"
	"path/filepath"
	"strings"
)

// File returns a SQLite URI that opens exactly path, with query (for example
// "mode=ro") as its query string. The path is percent-escaped so that "?",
// "#" and "%" in a file name are not read as URI syntax.
func File(path, query string) string {
	slashed := filepath.ToSlash(path)
	escaped := (&url.URL{Path: slashed}).EscapedPath()

	// SQLite reads a leading "//" as the start of an authority, so give such
	// paths an explicit empty one.
	if strings.HasPrefix(slashed, "//") {
		escaped = "//" + escaped
	}

	return "file:" + escaped + "?" + query
}
