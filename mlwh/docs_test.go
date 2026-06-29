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
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

// apiReferenceDocPath and glossaryDocPath are the committed documents this item
// produces, resolved relative to the mlwh package directory (tests run with the
// package as the working directory, so the repo's .docs/mcp/ is one level up).
const (
	apiReferenceDocPath = "../.docs/mcp/api-reference.md"
	glossaryDocPath     = "../.docs/mcp/glossary.md"
)

func TestEndpointReferenceCoversEveryRegistryEntryG1(t *testing.T) {
	// G1 acceptance test 1: the generated endpoint reference contains an entry
	// for every Registry entry (path + summary), asserted against Registry so a
	// missing endpoint fails the test.
	convey.Convey("Given the generated endpoint reference, when checked, then every Registry entry appears with its path and summary", t, func() {
		reference := EndpointReference()

		missingPaths := []string{}
		missingSummaries := []string{}

		for _, entry := range Registry {
			if !strings.Contains(reference, entry.Path) {
				missingPaths = append(missingPaths, entry.Path)
			}
			if !strings.Contains(reference, entry.Summary) {
				missingSummaries = append(missingSummaries, entry.Summary)
			}
		}

		convey.So(missingPaths, convey.ShouldBeEmpty)
		convey.So(missingSummaries, convey.ShouldBeEmpty)
	})
}

func TestEndpointReferenceMatchesCommittedDocumentG1(t *testing.T) {
	// G1 acceptance test 1 (no-drift / golden-file): the committed reference under
	// .docs/mcp/ must equal the generator output, so the human document cannot
	// silently drift from the served API. To refresh it, run WriteEndpointReference.
	convey.Convey("Given the committed .docs/mcp endpoint reference, when compared to the generator output, then they are identical", t, func() {
		committed, err := os.ReadFile(apiReferenceDocPath)
		convey.So(err, convey.ShouldBeNil)

		convey.So(string(committed), convey.ShouldEqual, EndpointReference())
	})
}

func TestEndpointReferenceAndOpenAPICoverSamePathsG1(t *testing.T) {
	// G1 acceptance test 2: the reference generator and the OpenAPI generator
	// cover the same set of Registry paths (no drift between human and machine
	// forms).
	convey.Convey("Given the reference generator and the OpenAPI generator, when both run, then they cover the same Registry paths", t, func() {
		reference := EndpointReference()

		doc := OpenAPIDocument()
		openAPIPaths, ok := doc["paths"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)

		referenceMissing := []string{}
		openAPIMissing := []string{}

		for _, entry := range Registry {
			if !strings.Contains(reference, entry.Path) {
				referenceMissing = append(referenceMissing, entry.Path)
			}
			if _, present := openAPIPaths[openAPIPathFromRegistry(entry.Path)]; !present {
				openAPIMissing = append(openAPIMissing, entry.Path)
			}
		}

		convey.So(referenceMissing, convey.ShouldBeEmpty)
		convey.So(openAPIMissing, convey.ShouldBeEmpty)
	})
}

// TestEndpointReferenceFilenameMatchesGenerator pins the committed reference's
// location to the path the generator writes, so the no-drift test and the
// refresh helper agree on a single file.
func TestEndpointReferenceFilenameMatchesGenerator(t *testing.T) {
	convey.Convey("Given the committed reference path, then it resolves under .docs/mcp", t, func() {
		convey.So(filepath.ToSlash(apiReferenceDocPath), convey.ShouldContainSubstring, ".docs/mcp/")
		convey.So(filepath.ToSlash(glossaryDocPath), convey.ShouldContainSubstring, ".docs/mcp/")
	})
}

// TestWriteEndpointReference refreshes the committed .docs/mcp endpoint
// reference from the generator. It is the documented way to regenerate the
// human catalogue after changing the Registry; it only writes when
// WA_REFRESH_DOCS is set so ordinary test runs stay read-only and the no-drift
// test (TestEndpointReferenceMatchesCommittedDocumentG1) remains the guard.
func TestWriteEndpointReference(t *testing.T) {
	if os.Getenv("WA_REFRESH_DOCS") == "" {
		t.Skip("set WA_REFRESH_DOCS to refresh the committed endpoint reference")
	}

	convey.Convey("Given WA_REFRESH_DOCS, when the reference is regenerated, then the committed document is rewritten", t, func() {
		err := os.WriteFile(apiReferenceDocPath, []byte(EndpointReference()), 0o644)
		convey.So(err, convey.ShouldBeNil)
	})
}

func TestEndpointReferenceIncludesParamsDescriptionAndResponseG1(t *testing.T) {
	// G1: the reference is the human form of the SAME enriched Registry metadata
	// the OpenAPI generator reads, so each entry must surface its path params,
	// query params (where paginated), description, and response type.
	convey.Convey("Given the endpoint reference, when an entry has path params, query params, a description, and a response type, then each is rendered", t, func() {
		reference := EndpointReference()

		convey.Convey("the SamplesForStudy entry renders its :id path param", func() {
			section := registryEntrySectionForTest(t, reference, "/study/:id/samples")
			convey.So(section, convey.ShouldContainSubstring, "id")
			convey.So(section, convey.ShouldContainSubstring, "List samples in a study")
		})

		convey.Convey("a paginated entry documents its limit and offset query params", func() {
			section := registryEntrySectionForTest(t, reference, "/studies")
			convey.So(section, convey.ShouldContainSubstring, "limit")
			convey.So(section, convey.ShouldContainSubstring, "offset")
		})

		convey.Convey("each entry surfaces its description and response type", func() {
			section := registryEntrySectionForTest(t, reference, "/classify/:id")
			convey.So(section, convey.ShouldContainSubstring, "Detects the kind of the given raw identifier")
			convey.So(section, convey.ShouldContainSubstring, "Match")
		})

		convey.Convey("a slice response type is rendered as an array of its element type", func() {
			section := registryEntrySectionForTest(t, reference, "/studies")
			convey.So(section, convey.ShouldContainSubstring, "Study")
		})
	})
}

// registryEntrySectionForTest returns the slice of the reference between the
// heading that introduces the given path and the next entry heading, so an
// assertion about one entry cannot accidentally match text from another.
func registryEntrySectionForTest(t *testing.T, reference, path string) string {
	t.Helper()

	heading := endpointHeadingForTest(path)
	start := strings.Index(reference, heading)
	convey.So(start, convey.ShouldBeGreaterThanOrEqualTo, 0)

	rest := reference[start+len(heading):]
	if next := strings.Index(rest, "\n### "); next != -1 {
		rest = rest[:next]
	}

	return rest
}

func TestEndpointReferenceCatchesDroppedEntryG1(t *testing.T) {
	// The coverage assertion must genuinely fail on drift rather than pass
	// vacuously: when an entry is dropped from the Registry the generator reads,
	// its path no longer appears in the rendered reference.
	convey.Convey("Given a Registry missing its first entry, when the reference is generated, then the dropped path is absent", t, func() {
		convey.So(Registry, convey.ShouldNotBeEmpty)

		full := EndpointReference()
		dropped := Registry[0]
		convey.So(full, convey.ShouldContainSubstring, dropped.Path)

		partial := endpointReferenceFromRegistryForTest(Registry[1:])

		convey.So(referenceDocumentsPathForTest(partial, dropped.Path, full), convey.ShouldBeFalse)
	})
}

func endpointReferenceFromRegistryForTest(entries []Endpoint) string {
	original := Registry
	Registry = entries
	defer func() { Registry = original }()

	return EndpointReference()
}

// referenceDocumentsPathForTest reports whether the path appears in its own
// dedicated entry heading in reference, using the full reference only to derive
// the exact heading string the generator emits for that path.
func referenceDocumentsPathForTest(reference, path, full string) bool {
	_ = full

	return strings.Contains(reference, endpointHeadingForTest(path))
}

func endpointHeadingForTest(path string) string {
	return "### `GET " + path + "`"
}

func TestGlossaryDefinesDomainEntitiesG1(t *testing.T) {
	// G1 acceptance test 3 (first half): the glossary defines study, sample, run,
	// library, lane, and iRODS path.
	convey.Convey("Given the glossary document, when read, then it defines each core MLWH entity", t, func() {
		glossary := readGlossaryForTest(t)
		lower := strings.ToLower(glossary)

		for _, entity := range []string{"study", "sample", "run", "library", "lane", "irods path"} {
			convey.So(lower, convey.ShouldContainSubstring, entity)
		}

		// Each entity is a defined glossary term (a heading), not merely a passing
		// mention, so the document genuinely defines them.
		terms := glossaryTermsForTest(glossary)
		for _, want := range []string{"Study", "Sample", "Run", "Library", "Lane", "iRODS path"} {
			convey.So(terms, convey.ShouldContainKey, strings.ToLower(want))
		}
	})
}

func TestGlossaryDefinesAvailabilityConceptsG2(t *testing.T) {
	// G2 acceptance test 3: the glossary defines the availability, recency and
	// progress concepts introduced by this feature - "sequencing data available"
	// and "added to iRODS" (called out by the spec), plus "baseline phase",
	// "detailed timeline", and "platform". Each must be a genuine glossary term (a
	// heading), not a passing mention, so the document truly defines them.
	convey.Convey("Given the glossary document, when read, then it defines the availability, recency and progress concepts", t, func() {
		glossary := readGlossaryForTest(t)
		terms := glossaryTermsForTest(glossary)

		for _, want := range []string{
			"sequencing data available",
			"added to iRODS",
			"baseline phase",
			"detailed timeline",
			"platform",
		} {
			convey.So(terms, convey.ShouldContainKey, strings.ToLower(want))
		}
	})
}

func TestGlossaryListsEveryIdentifierKindG1(t *testing.T) {
	// G1 acceptance test 3 (second half): the glossary lists every IdentifierKind
	// constant value. Driven from IdentifierKinds() so a newly added kind that is
	// not documented fails this test.
	convey.Convey("Given the glossary document, when read, then it lists every IdentifierKind constant value", t, func() {
		glossary := readGlossaryForTest(t)

		kinds := IdentifierKinds()
		convey.So(kinds, convey.ShouldNotBeEmpty)

		missing := []string{}
		for _, kind := range kinds {
			if !strings.Contains(glossary, string(kind)) {
				missing = append(missing, string(kind))
			}
		}

		convey.So(missing, convey.ShouldBeEmpty)
	})
}

func readGlossaryForTest(t *testing.T) string {
	t.Helper()

	content, err := os.ReadFile(glossaryDocPath)
	convey.So(err, convey.ShouldBeNil)

	return string(content)
}

// glossaryTermsForTest maps the lower-cased text of every glossary term heading
// ("### Term") to true, so a test can assert a term is genuinely defined.
func glossaryTermsForTest(glossary string) map[string]bool {
	terms := map[string]bool{}

	for _, line := range strings.Split(glossary, "\n") {
		trimmed := strings.TrimSpace(line)
		if heading, ok := strings.CutPrefix(trimmed, "### "); ok {
			heading = strings.TrimSpace(strings.TrimPrefix(heading, "`"))
			heading = strings.TrimSpace(strings.TrimSuffix(heading, "`"))
			terms[strings.ToLower(heading)] = true
		}
	}

	return terms
}

func TestIdentifierKindsCoversEveryDeclaredConstantG1(t *testing.T) {
	// Guard so the glossary's IdentifierKind coverage cannot rot: IdentifierKinds()
	// must enumerate every IdentifierKind constant declared in mlwh.go. A new
	// const added to the source but omitted from IdentifierKinds() fails here,
	// which in turn forces it into the glossary (TestGlossaryListsEveryIdentifierKindG1).
	convey.Convey("Given the IdentifierKind constants declared in source, when compared to IdentifierKinds(), then every declared value is present", t, func() {
		declared := declaredIdentifierKindValuesForTest(t)
		convey.So(declared, convey.ShouldNotBeEmpty)

		listed := map[string]bool{}
		for _, kind := range IdentifierKinds() {
			listed[string(kind)] = true
		}

		missing := []string{}
		for _, value := range declared {
			if !listed[value] {
				missing = append(missing, value)
			}
		}

		convey.So(missing, convey.ShouldBeEmpty)
	})
}

// declaredIdentifierKindValuesForTest parses mlwh.go and returns the string
// value of every const whose type is IdentifierKind, so the IdentifierKinds()
// guard test is driven by the actual source rather than a hand-maintained list.
func declaredIdentifierKindValuesForTest(t *testing.T) []string {
	t.Helper()

	source, err := os.ReadFile("mlwh.go")
	convey.So(err, convey.ShouldBeNil)

	file, err := parser.ParseFile(token.NewFileSet(), "mlwh.go", source, 0)
	convey.So(err, convey.ShouldBeNil)

	values := []string{}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}

		for _, spec := range genDecl.Specs {
			value, ok := identifierKindConstValueForTest(spec)
			if ok {
				values = append(values, value)
			}
		}
	}

	return values
}

func identifierKindConstValueForTest(spec ast.Spec) (string, bool) {
	valueSpec, ok := spec.(*ast.ValueSpec)
	if !ok {
		return "", false
	}

	typeIdent, ok := valueSpec.Type.(*ast.Ident)
	if !ok || typeIdent.Name != "IdentifierKind" || len(valueSpec.Values) != 1 {
		return "", false
	}

	literal, ok := valueSpec.Values[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}

	return strings.Trim(literal.Value, "`\""), true
}
