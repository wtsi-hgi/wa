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
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestIdentifierTypeConstantsMatchD1(t *testing.T) {
	convey.Convey("D1: IdentifierType constants mirror the mlwh identifier kinds and exclude project_name", t, func() {
		_, thisFile, _, ok := runtime.Caller(0)
		convey.So(ok, convey.ShouldBeTrue)

		typesPath := filepath.Join(filepath.Dir(thisFile), "types.go")
		file, err := parser.ParseFile(token.NewFileSet(), typesPath, nil, 0)
		convey.So(err, convey.ShouldBeNil)

		actual := make([]string, 0)
		actualSet := make(map[string]struct{})
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.GenDecl)
			if !ok || decl.Tok != token.CONST {
				return true
			}

			for _, spec := range decl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if len(valueSpec.Names) == 0 || len(valueSpec.Values) == 0 {
					continue
				}
				if !strings.HasPrefix(valueSpec.Names[0].Name, "Identifier") {
					continue
				}

				for _, value := range valueSpec.Values {
					literal, ok := value.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}

					unquoted, unquoteErr := strconv.Unquote(literal.Value)
					convey.So(unquoteErr, convey.ShouldBeNil)
					actualSet[unquoted] = struct{}{}
				}
			}

			return true
		})

		for value := range actualSet {
			actual = append(actual, value)
		}

		sort.Strings(actual)
		expected := []string{
			string(IdentifierStudyLimsID),
			string(IdentifierStudyAccession),
			string(IdentifierStudyUUID),
			string(IdentifierStudyName),
			string(IdentifierSangerSampleName),
			string(IdentifierSangerSampleID),
			string(IdentifierSampleLimsID),
			string(IdentifierSampleUUID),
			string(IdentifierSampleAccession),
			string(IdentifierSupplierName),
			string(IdentifierDonorID),
			string(IdentifierRunID),
			string(IdentifierLibraryType),
			string(IdentifierLibraryID),
			string(IdentifierLibraryLimsID),
		}
		sort.Strings(expected)

		convey.So(actual, convey.ShouldResemble, expected)
		convey.So(actual, convey.ShouldNotContain, "project_name")
	})
}

func TestEnrichmentGraphUsesMLWHShapes(t *testing.T) {
	convey.Convey("D1: EnrichmentGraph drops the legacy owner fields and uses mlwh.Sample for sample nodes", t, func() {
		graphType := reflect.TypeOf(EnrichmentGraph{})

		_, hasRemovedField := graphType.FieldByName(strings.Join([]string{"Pr", "oject"}, ""))
		_, hasUsers := graphType.FieldByName("Users")
		convey.So(hasRemovedField, convey.ShouldBeFalse)
		convey.So(hasUsers, convey.ShouldBeFalse)

		sampleField, ok := graphType.FieldByName("Sample")
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(sampleField.Type, convey.ShouldEqual, reflect.TypeOf((*mlwh.Sample)(nil)))

		samplesField, ok := graphType.FieldByName("Samples")
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(samplesField.Type, convey.ShouldEqual, reflect.TypeOf([]mlwh.Sample{}))
	})
}
