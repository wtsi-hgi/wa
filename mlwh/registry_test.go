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

package mlwh

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestRegistryUsesUniqueGetPaths(t *testing.T) {
	convey.Convey("Given Registry, when checked, then every path is unique and every verb is GET", t, func() {
		paths := make(map[string]struct{}, len(Registry))
		duplicatePaths := []string{}
		nonGetMethods := []string{}

		for _, entry := range Registry {
			if entry.Verb != "GET" {
				nonGetMethods = append(nonGetMethods, entry.Method)
			}
			if _, seen := paths[entry.Path]; seen {
				duplicatePaths = append(duplicatePaths, entry.Path)
			}

			paths[entry.Path] = struct{}{}
		}

		convey.So(nonGetMethods, convey.ShouldBeEmpty)
		convey.So(duplicatePaths, convey.ShouldBeEmpty)
		convey.So(paths, convey.ShouldHaveLength, len(Registry))
	})
}

func TestAddMLWHQueryDocumentation(t *testing.T) {
	convey.Convey("Given DEVELOPING.md", t, func() {
		developing := readRepoRootFile(t, "DEVELOPING.md")
		section, found := markdownSection(developing, "## Add a new MLWH query")

		convey.Convey("when read, then it contains the F1 checklist and parity references", func() {
			convey.So(found, convey.ShouldBeTrue)
			convey.So(numberedSteps(section), convey.ShouldResemble, []string{
				"Add any required schema column and index in both `mlwh/cache_schema/sqlite/` and `mlwh/cache_schema/mysql/`.",
				"Add one `Client` method.",
				"Add one `Queryer` member.",
				"Add one `Registry` entry.",
			})
			convey.So(section, convey.ShouldContainSubstring, ".docs/mlwh-sync/spec.md")
			convey.So(strings.ToLower(section), convey.ShouldContainSubstring, "read-path audit")
			convey.So(section, convey.ShouldContainSubstring, "TestParseSchemaShapeParity")
		})
	})
}

func readRepoRootFile(t *testing.T, name string) string {
	t.Helper()

	return readFile(t, filepath.Join("..", name))
}

func markdownSection(doc string, heading string) (string, bool) {
	start := strings.Index(doc, heading)
	if start == -1 {
		return "", false
	}

	section := doc[start+len(heading):]
	if next := strings.Index(section, "\n## "); next != -1 {
		section = section[:next]
	}

	return strings.TrimSpace(section), true
}

func numberedSteps(section string) []string {
	matches := numberedStepPattern().FindAllStringSubmatch(section, -1)
	steps := make([]string, 0, len(matches))

	for _, match := range matches {
		steps = append(steps, strings.TrimSpace(match[1]))
	}

	return steps
}

func numberedStepPattern() *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\d+\.\s+(.+)$`)
}

func TestRegistryDocumentationDeclaresSingleSource(t *testing.T) {
	convey.Convey("Given mlwh/registry.go", t, func() {
		source := readPackageFile(t, "registry.go")
		docText, err := registryDocText(source)

		convey.Convey("when read, then the package or Registry docs explain the derivation contract", func() {
			convey.So(err, convey.ShouldBeNil)
			normalized := normalizeDocText(docText)
			convey.So(normalized, convey.ShouldContainSubstring, "Registry is the single source from which the handler and RemoteClient derive")
			convey.So(normalized, convey.ShouldContainSubstring, "Adding a Queryer method requires adding a Registry entry")
		})
	})
}

func TestCacheSchemaParityTestGuardsAddQuerySchemaStep(t *testing.T) {
	convey.Convey("Given mlwh/cache_schema_test.go", t, func() {
		source := readPackageFile(t, "cache_schema_test.go")

		convey.Convey("when inspected, then it still compares table, column, and index sets across dialects", func() {
			convey.So(source, convey.ShouldContainSubstring, "TestParseSchemaShapeParity")
			convey.So(source, convey.ShouldContainSubstring, "sqliteShape.Tables, convey.ShouldResemble, mysqlShape.Tables")
			convey.So(source, convey.ShouldContainSubstring, "sqliteShape.Index, convey.ShouldResemble, mysqlShape.Index")
		})
	})
}

func readPackageFile(t *testing.T, name string) string {
	t.Helper()

	return readFile(t, name)
}

func readFile(t *testing.T, name string) string {
	t.Helper()

	content, err := os.ReadFile(name)
	convey.So(err, convey.ShouldBeNil)
	if err != nil {
		return ""
	}

	return string(content)
}

func registryDocText(source string) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "registry.go", source, parser.ParseComments)
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, 2)
	if file.Doc != nil {
		parts = append(parts, file.Doc.Text())
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR || genDecl.Doc == nil {
			continue
		}

		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for _, name := range valueSpec.Names {
				if name.Name == "Registry" {
					parts = append(parts, genDecl.Doc.Text())
				}
			}
		}
	}

	return strings.Join(parts, "\n"), nil
}

func normalizeDocText(doc string) string {
	return strings.Join(strings.Fields(doc), " ")
}

func TestRegistryCoversQueryer(t *testing.T) {
	convey.Convey("Given Queryer and Registry", t, func() {
		queryer := reflect.TypeOf((*Queryer)(nil)).Elem()
		registryMethods := registryMethodNames(Registry)

		convey.Convey("when compared, then every Queryer method has exactly one Registry entry", func() {
			missing, duplicate, unknown := registryCoverageIssues(queryer, registryMethods)

			convey.So(Registry, convey.ShouldHaveLength, 33)
			convey.So(missing, convey.ShouldBeEmpty)
			convey.So(duplicate, convey.ShouldBeEmpty)
			convey.So(unknown, convey.ShouldBeEmpty)
		})

		convey.Convey("when one Registry entry is removed, then the missing Queryer method is reported", func() {
			convey.So(Registry, convey.ShouldNotBeEmpty)
			if len(Registry) == 0 {
				return
			}

			removedMethod := Registry[0].Method
			trimmedRegistryMethods := append([]string{}, registryMethods[1:]...)

			convey.So(missingRegistryMethods(queryer, trimmedRegistryMethods), convey.ShouldContain, removedMethod)
		})
	})
}

func registryMethodNames(registry []Endpoint) []string {
	methods := make([]string, 0, len(registry))
	for _, endpoint := range registry {
		methods = append(methods, endpoint.Method)
	}

	return methods
}

func registryCoverageIssues(queryer reflect.Type, registryMethods []string) ([]string, []string, []string) {
	queryerMethods := make(map[string]struct{}, queryer.NumMethod())
	for i := range queryer.NumMethod() {
		queryerMethods[queryer.Method(i).Name] = struct{}{}
	}

	registered := make(map[string]struct{}, len(registryMethods))
	duplicates := make([]string, 0)
	unknown := make([]string, 0)

	for _, method := range registryMethods {
		if _, ok := registered[method]; ok {
			duplicates = append(duplicates, method)
		}

		registered[method] = struct{}{}

		if _, ok := queryerMethods[method]; !ok {
			unknown = append(unknown, method)
		}
	}

	missing := missingRegistryMethods(queryer, registryMethods)
	slices.Sort(duplicates)
	slices.Sort(unknown)

	return missing, duplicates, unknown
}

func missingRegistryMethods(queryer reflect.Type, registryMethods []string) []string {
	registered := make(map[string]struct{}, len(registryMethods))
	for _, method := range registryMethods {
		registered[method] = struct{}{}
	}

	missing := make([]string, 0)
	for i := range queryer.NumMethod() {
		method := queryer.Method(i).Name
		if _, ok := registered[method]; !ok {
			missing = append(missing, method)
		}
	}

	slices.Sort(missing)

	return missing
}

func TestRegistryPaginationMatchesQueryerSignatures(t *testing.T) {
	convey.Convey("Given paginated registry entries, when checked, then matching Queryer methods end in limit and offset ints", t, func() {
		queryer := reflect.TypeOf((*Queryer)(nil)).Elem()
		violations := []string{}

		for _, entry := range Registry {
			if !entry.Paginated {
				continue
			}

			method, ok := queryer.MethodByName(entry.Method)
			if !ok || !hasTrailingLimitOffset(method.Type) {
				violations = append(violations, entry.Method)
			}
		}

		convey.So(violations, convey.ShouldBeEmpty)
	})
}

func hasTrailingLimitOffset(methodType reflect.Type) bool {
	intType := reflect.TypeOf(0)
	paramCount := methodType.NumIn()

	return paramCount >= 3 &&
		methodType.In(paramCount-2) == intType &&
		methodType.In(paramCount-1) == intType
}

func TestRegistrySamplesForStudy(t *testing.T) {
	convey.Convey("Given the SamplesForStudy entry, then it describes the study samples endpoint", t, func() {
		entry, ok := registryEntryByMethod("SamplesForStudy")

		convey.So(ok, convey.ShouldBeTrue)
		convey.So(entry.Path, convey.ShouldEqual, "/study/:id/samples")
		convey.So(entry.PathParams, convey.ShouldResemble, []string{"id"})
		convey.So(entry.Query, convey.ShouldResemble, []string{})
		convey.So(entry.Paginated, convey.ShouldBeTrue)

		_, ok = entry.NewResult().(*[]Sample)
		convey.So(ok, convey.ShouldBeTrue)
	})
}

func TestRegistrySamplesForLibrary(t *testing.T) {
	convey.Convey("Given the SamplesForLibrary entry, then it describes the library and study scoped samples endpoint", t, func() {
		entry, ok := registryEntryByMethod("SamplesForLibrary")

		convey.So(ok, convey.ShouldBeTrue)
		convey.So(entry.Path, convey.ShouldEqual, "/library/:pipeline/study/:study/samples")
		convey.So(entry.PathParams, convey.ShouldResemble, []string{"pipeline", "study"})
	})
}

func registryEntryByMethod(method string) (Endpoint, bool) {
	for _, entry := range Registry {
		if entry.Method == method {
			return entry, true
		}
	}

	return Endpoint{}, false
}
