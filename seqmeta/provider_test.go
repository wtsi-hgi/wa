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

package seqmeta

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestSeqmetaSourceHasNoRemovedImports(t *testing.T) {
	convey.Convey("D1: seqmeta source carries no removed upstream imports after the provider swap", t, func() {
		_, thisFile, _, ok := runtime.Caller(0)
		convey.So(ok, convey.ShouldBeTrue)

		dir := filepath.Dir(thisFile)
		entries, err := os.ReadDir(dir)
		convey.So(err, convey.ShouldBeNil)

		fset := token.NewFileSet()
		offenders := make([]string, 0)
		targetImport := "\"github.com/wtsi-hgi/wa/" + "s" + "aga\""

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			convey.So(parseErr, convey.ShouldBeNil)

			for _, imported := range file.Imports {
				if imported.Path == nil || imported.Path.Value != targetImport {
					continue
				}

				offenders = append(offenders, filepath.Base(path))
			}
		}

		convey.So(offenders, convey.ShouldBeEmpty)
	})
}
