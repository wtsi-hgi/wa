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

func TestRegistryEntriesAreDocumented(t *testing.T) {
	// C1 acceptance test 1: every Registry entry carries a non-empty Summary
	// and Description so the generated OpenAPI document and human reference are
	// useful to an LLM.
	convey.Convey("Given Registry, when iterated, then every entry has a non-empty Summary and Description", t, func() {
		missingSummary := []string{}
		missingDescription := []string{}

		for _, entry := range Registry {
			if strings.TrimSpace(entry.Summary) == "" {
				missingSummary = append(missingSummary, entry.Method)
			}
			if strings.TrimSpace(entry.Description) == "" {
				missingDescription = append(missingDescription, entry.Method)
			}
		}

		convey.So(missingSummary, convey.ShouldBeEmpty)
		convey.So(missingDescription, convey.ShouldBeEmpty)
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

func TestRegistryNewEndpointsAreFullyDocumentedG1(t *testing.T) {
	// G1 acceptance test 1, stated explicitly over the Phase 2-4 endpoint set:
	// every new Method carries a non-empty Summary AND Description, and every new
	// paginated entry declares integer limit/offset QueryParams. The all-entries
	// guards (TestRegistryEntriesAreDocumented,
	// TestRegistryPaginatedEntriesDeclareLimitOffset) already cover the superset;
	// this pins the bar to the new surface so a future addition cannot regress it.
	convey.Convey("Given the Phase 2-4 endpoints, when iterated, then each is fully documented", t, func() {
		newMethods := newAvailabilityRecencyProgressMethods()

		convey.Convey("the named new methods all exist in the Registry (the assertion is not vacuous)", func() {
			convey.So(newMethodsExistInRegistry(newMethods), convey.ShouldBeEmpty)
		})

		convey.Convey("every new Method has a non-empty Summary and Description", func() {
			missingSummary := []string{}
			missingDescription := []string{}

			for _, method := range newMethods {
				entry, ok := registryEntryByMethod(method)
				if !ok {
					continue
				}
				if strings.TrimSpace(entry.Summary) == "" {
					missingSummary = append(missingSummary, method)
				}
				if strings.TrimSpace(entry.Description) == "" {
					missingDescription = append(missingDescription, method)
				}
			}

			convey.So(missingSummary, convey.ShouldBeEmpty)
			convey.So(missingDescription, convey.ShouldBeEmpty)
		})

		convey.Convey("every new paginated Method declares integer limit and offset QueryParams", func() {
			missingLimitOffset := []string{}

			for _, method := range newMethods {
				entry, ok := registryEntryByMethod(method)
				if !ok || !entry.Paginated {
					continue
				}

				limit, hasLimit := queryParamByName(entry.QueryParams, "limit")
				offset, hasOffset := queryParamByName(entry.QueryParams, "offset")
				if !hasLimit || limit.Type != "integer" || !hasOffset || offset.Type != "integer" {
					missingLimitOffset = append(missingLimitOffset, method)
				}
			}

			convey.So(missingLimitOffset, convey.ShouldBeEmpty)
		})
	})
}

// newAvailabilityRecencyProgressMethods is the closed set of Queryer methods
// added across Phases 2-5 (the availability / recency / run-overview / budget
// /count / progress surface, plus the Phase 2/3/5 run-iRODS, study-manifest and
// people endpoints). G1's wiring assertions are stated explicitly over this set
// so a new endpoint that is wired into the Queryer/Registry but left without a
// Summary, a Description or (when paginated) limit/offset QueryParams fails here.
// newMethodsExistInRegistry guards that the set is not vacuous: each name must be
// a live Registry method, so a typo or a removed endpoint surfaces as a missing
// entry rather than a silently skipped assertion.
func newAvailabilityRecencyProgressMethods() []string {
	return []string{
		// Phase 2: availability (B) + recency (C).
		"StudyOverview",
		"SamplesWithData",
		"SamplesWithoutData",
		"CountSamplesWithData",
		// Phase 3: run overview (D) + the budget-safety /count counterparts (E1).
		"RunOverview",
		"CountSamplesForRun",
		"CountSamplesForLibrary",
		"CountSamplesForLibraryID",
		"CountSamplesForLibraryLimsID",
		"CountSamplesForLibraryType",
		"CountRunsForStudy",
		"CountLibrariesForStudy",
		"CountLanesForSample",
		"CountIRODSPathsForSample",
		"CountIRODSPathsForStudy",
		"CountFindSamplesBySangerID",
		"CountFindSamplesByIDSampleLims",
		"CountFindSamplesByAccessionNumber",
		"CountFindSamplesBySupplierName",
		"CountFindSamplesByLibraryType",
		// Phase 4: progress (F).
		"RunStatus",
		"SampleProgress",
		"StatusBreakdown",
		// Phases 2/3/5: run-iRODS, study-manifest and people endpoints (G1).
		"IRODSPathsForRun",
		"CountIRODSPathsForRun",
		"StudyManifest",
		"CountStudyManifest",
		"StudiesForFacultySponsor",
		"CountStudiesForFacultySponsor",
		"StudiesForUser",
		"CountStudiesForUser",
		"ResolvePerson",
		"CountResolvePerson",
	}
}

func newMethodsExistInRegistry(methods []string) []string {
	missing := []string{}
	for _, method := range methods {
		if _, ok := registryEntryByMethod(method); !ok {
			missing = append(missing, method)
		}
	}

	slices.Sort(missing)

	return missing
}

func TestRegistryRecencyDescriptionsCiteCreationTimestampG1(t *testing.T) {
	// G1 acceptance test 4: every windowed/recency Description must state that the
	// "added since" filter is on the iRODS CREATION timestamp (the created column)
	// and NEVER on last_updated or last_run (the freshness Key Decision / HARD REQ
	// 3). The rule is data-driven over the whole Registry so it cannot silently
	// miss a new endpoint: any Description that mentions the created column (the
	// recency surface) must also carry the never-last_updated/last_run clause.
	convey.Convey("Given the Registry, when each Description that cites the iRODS created column is inspected", t, func() {
		convey.Convey("then it also states the filter is never on last_updated or last_run", func() {
			missingNegativeClause := []string{}

			for _, entry := range Registry {
				if !mentionsIRODSCreationTimestamp(entry.Description) {
					continue
				}
				if !statesCreatedNotLastUpdatedOrLastRun(entry.Description) {
					missingNegativeClause = append(missingNegativeClause, entry.Method)
				}
			}

			convey.So(missingNegativeClause, convey.ShouldBeEmpty)
		})

		convey.Convey("and the four windowed/recency endpoints each carry the creation-timestamp wording", func() {
			windowedRecency := []string{"StudyOverview", "RunOverview", "SamplesWithData", "CountSamplesWithData"}
			notStated := []string{}

			for _, method := range windowedRecency {
				entry, ok := registryEntryByMethod(method)
				if !ok {
					notStated = append(notStated, method+" (missing)")

					continue
				}
				if !mentionsIRODSCreationTimestamp(entry.Description) || !statesCreatedNotLastUpdatedOrLastRun(entry.Description) {
					notStated = append(notStated, method)
				}
			}

			convey.So(notStated, convey.ShouldBeEmpty)
		})
	})
}

// mentionsIRODSCreationTimestamp reports whether a Description refers to the
// iRODS creation timestamp by its canonical column name, the marker that an
// endpoint exposes recency/windowed semantics.
func mentionsIRODSCreationTimestamp(description string) bool {
	return strings.Contains(strings.ToLower(description), "created column")
}

// statesCreatedNotLastUpdatedOrLastRun reports whether a Description states the
// recency filter is never on last_updated or last_run, tolerating the two
// phrasings in use ("NEVER last_updated or last_run" and "NEVER on last_updated
// or last_run").
func statesCreatedNotLastUpdatedOrLastRun(description string) bool {
	normalized := normalizeDocText(strings.ToLower(description))

	return strings.Contains(normalized, "never last_updated or last_run") ||
		strings.Contains(normalized, "never on last_updated or last_run")
}

func normalizeDocText(doc string) string {
	return strings.Join(strings.Fields(doc), " ")
}

func TestRegistryQueryListMatchesQueryParamsG1(t *testing.T) {
	// G1 internal-consistency guard: the Endpoint.Query list and the structured
	// QueryParams must agree, so the Registry cannot declare a query param in one
	// place but not the other (the inconsistency seen on /study/:id/detail and
	// /run/:id/detail, which declared lean in QueryParams but left Query empty).
	// The contract is: Query lists exactly the endpoint's non-pagination query
	// params; limit/offset are pagination controls, declared via QueryParams (and
	// surfaced by the OpenAPI generator) but deliberately omitted from Query, so a
	// paginated list with no extra filter has an empty Query.
	convey.Convey("Given each Registry entry, when its Query list is compared to its non-pagination QueryParams, then they agree", t, func() {
		queryNotInParams := []string{}
		nonPaginationParamNotInQuery := []string{}

		for _, entry := range Registry {
			paramNames := queryParamNameSet(entry.QueryParams)

			for _, name := range entry.Query {
				if !paramNames[name] {
					queryNotInParams = append(queryNotInParams, entry.Method+":"+name)
				}
			}

			for _, param := range entry.QueryParams {
				if isPaginationQueryParamName(param.Name) {
					continue
				}
				if !slices.Contains(entry.Query, param.Name) {
					nonPaginationParamNotInQuery = append(nonPaginationParamNotInQuery, entry.Method+":"+param.Name)
				}
			}
		}

		convey.So(queryNotInParams, convey.ShouldBeEmpty)
		convey.So(nonPaginationParamNotInQuery, convey.ShouldBeEmpty)
	})

	convey.Convey("Given the detail endpoints, then each lists its lean query param in both Query and QueryParams", t, func() {
		for _, method := range []string{"StudyDetail", "RunDetail"} {
			entry, ok := registryEntryByMethod(method)
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(entry.Query, convey.ShouldContain, "lean")
			_, hasLean := queryParamByName(entry.QueryParams, "lean")
			convey.So(hasLean, convey.ShouldBeTrue)
		}
	})
}

func queryParamNameSet(params []QueryParam) map[string]bool {
	names := make(map[string]bool, len(params))
	for _, param := range params {
		names[param.Name] = true
	}

	return names
}

// isPaginationQueryParamName reports whether a QueryParam name is one of the
// pagination controls (limit/offset), which are declared via QueryParams but
// deliberately kept out of the Endpoint.Query list.
func isPaginationQueryParamName(name string) bool {
	return name == "limit" || name == "offset"
}

func TestRegistryCoversQueryer(t *testing.T) {
	convey.Convey("Given Queryer and Registry", t, func() {
		queryer := reflect.TypeOf((*Queryer)(nil)).Elem()
		registryMethods := registryMethodNames(Registry)

		convey.Convey("when compared, then every Queryer method has exactly one Registry entry", func() {
			missing, duplicate, unknown := registryCoverageIssues(queryer, registryMethods)

			convey.So(Registry, convey.ShouldHaveLength, 73)
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

func TestRegistrySearchCountFreshnessEntries(t *testing.T) {
	// Item 4.1: the seven Phase 4 Queryer members each get a Registry entry
	// matching the spec's Registry table: paths, PathParams, Paginated, and
	// the NewResult type the handler and RemoteClient derive from.
	convey.Convey("Given the Phase 4 Registry entries, then each matches the spec's Registry table", t, func() {
		convey.Convey("SearchStudies is a paginated study-search list endpoint", func() {
			entry, ok := registryEntryByMethod("SearchStudies")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(entry.Path, convey.ShouldEqual, "/search/study/:term")
			convey.So(entry.PathParams, convey.ShouldResemble, []string{"term"})
			convey.So(entry.Paginated, convey.ShouldBeTrue)
			_, isSlice := entry.NewResult().(*[]Study)
			convey.So(isSlice, convey.ShouldBeTrue)
		})

		convey.Convey("SearchSamples is a paginated sample-search list endpoint", func() {
			entry, ok := registryEntryByMethod("SearchSamples")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(entry.Path, convey.ShouldEqual, "/search/sample/:term")
			convey.So(entry.PathParams, convey.ShouldResemble, []string{"term"})
			convey.So(entry.Paginated, convey.ShouldBeTrue)
			_, isSlice := entry.NewResult().(*[]Sample)
			convey.So(isSlice, convey.ShouldBeTrue)
		})

		convey.Convey("CountStudySearch is a non-paginated count endpoint", func() {
			entry, ok := registryEntryByMethod("CountStudySearch")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(entry.Path, convey.ShouldEqual, "/search/study/:term/count")
			convey.So(entry.PathParams, convey.ShouldResemble, []string{"term"})
			convey.So(entry.Paginated, convey.ShouldBeFalse)
			_, isCount := entry.NewResult().(*Count)
			convey.So(isCount, convey.ShouldBeTrue)
		})

		convey.Convey("CountSampleSearch is a non-paginated count endpoint", func() {
			entry, ok := registryEntryByMethod("CountSampleSearch")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(entry.Path, convey.ShouldEqual, "/search/sample/:term/count")
			convey.So(entry.PathParams, convey.ShouldResemble, []string{"term"})
			convey.So(entry.Paginated, convey.ShouldBeFalse)
			_, isCount := entry.NewResult().(*Count)
			convey.So(isCount, convey.ShouldBeTrue)
		})

		convey.Convey("CountStudies is a non-paginated count endpoint with no path params", func() {
			entry, ok := registryEntryByMethod("CountStudies")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(entry.Path, convey.ShouldEqual, "/studies/count")
			convey.So(entry.PathParams, convey.ShouldBeEmpty)
			convey.So(entry.Paginated, convey.ShouldBeFalse)
			_, isCount := entry.NewResult().(*Count)
			convey.So(isCount, convey.ShouldBeTrue)
		})

		convey.Convey("CountSamplesForStudy is a non-paginated count endpoint keyed by study id", func() {
			entry, ok := registryEntryByMethod("CountSamplesForStudy")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(entry.Path, convey.ShouldEqual, "/study/:id/samples/count")
			convey.So(entry.PathParams, convey.ShouldResemble, []string{"id"})
			convey.So(entry.Paginated, convey.ShouldBeFalse)
			_, isCount := entry.NewResult().(*Count)
			convey.So(isCount, convey.ShouldBeTrue)
		})

		convey.Convey("CountSamplesWithData is a non-paginated count endpoint keyed by study id", func() {
			entry, ok := registryEntryByMethod("CountSamplesWithData")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(entry.Path, convey.ShouldEqual, "/study/:id/samples-with-data/count")
			convey.So(entry.PathParams, convey.ShouldResemble, []string{"id"})
			convey.So(entry.Paginated, convey.ShouldBeFalse)
			_, isCount := entry.NewResult().(*Count)
			convey.So(isCount, convey.ShouldBeTrue)
		})

		convey.Convey("Freshness is a non-paginated freshness endpoint with no path params", func() {
			entry, ok := registryEntryByMethod("Freshness")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(entry.Path, convey.ShouldEqual, "/freshness")
			convey.So(entry.PathParams, convey.ShouldBeEmpty)
			convey.So(entry.Paginated, convey.ShouldBeFalse)
			_, isFreshness := entry.NewResult().(*Freshness)
			convey.So(isFreshness, convey.ShouldBeTrue)
		})
	})
}

func TestRegistryPaginatedEntriesDeclareLimitOffset(t *testing.T) {
	// C1 acceptance test 2: every Paginated entry declares limit and offset
	// QueryParams of type integer, the structured specs the OpenAPI generator
	// turns into query parameters.
	convey.Convey("Given each Paginated entry, when checked, then its QueryParams include integer limit and offset", t, func() {
		missingLimit := []string{}
		missingOffset := []string{}

		for _, entry := range Registry {
			if !entry.Paginated {
				continue
			}

			limit, hasLimit := queryParamByName(entry.QueryParams, "limit")
			offset, hasOffset := queryParamByName(entry.QueryParams, "offset")

			if !hasLimit || limit.Type != "integer" {
				missingLimit = append(missingLimit, entry.Method)
			}
			if !hasOffset || offset.Type != "integer" {
				missingOffset = append(missingOffset, entry.Method)
			}
		}

		convey.So(missingLimit, convey.ShouldBeEmpty)
		convey.So(missingOffset, convey.ShouldBeEmpty)
	})

	convey.Convey("Given a non-paginated entry, when checked, then it declares no limit/offset QueryParams", t, func() {
		entry, ok := registryEntryByMethod("CountStudies")
		convey.So(ok, convey.ShouldBeTrue)

		_, hasLimit := queryParamByName(entry.QueryParams, "limit")
		_, hasOffset := queryParamByName(entry.QueryParams, "offset")
		convey.So(hasLimit, convey.ShouldBeFalse)
		convey.So(hasOffset, convey.ShouldBeFalse)
	})

	convey.Convey("Given the search entries, when checked, then their limit QueryParam documents the default of 100", t, func() {
		for _, method := range []string{"SearchStudies", "SearchSamples"} {
			entry, ok := registryEntryByMethod(method)
			convey.So(ok, convey.ShouldBeTrue)

			limit, hasLimit := queryParamByName(entry.QueryParams, "limit")
			convey.So(hasLimit, convey.ShouldBeTrue)
			convey.So(limit.Description, convey.ShouldContainSubstring, "100")
		}
	})
}

func queryParamByName(params []QueryParam, name string) (QueryParam, bool) {
	for _, param := range params {
		if param.Name == name {
			return param, true
		}
	}

	return QueryParam{}, false
}

func registryEntryByMethod(method string) (Endpoint, bool) {
	for _, entry := range Registry {
		if entry.Method == method {
			return entry, true
		}
	}

	return Endpoint{}, false
}

func TestServedTypeFieldsCarryDocTags(t *testing.T) {
	// C1 acceptance test 3: every JSON-serialised field of the directly-served
	// result types carries a non-empty doc: tag, the per-field description the
	// OpenAPI generator and human reference read by reflection. The spec names
	// Study and Match; the full directly-served set is checked so none drifts.
	servedTypes := []struct {
		name string
		typ  reflect.Type
	}{
		{"Match", reflect.TypeOf(Match{})},
		{"TaggedID", reflect.TypeOf(TaggedID{})},
		{"Study", reflect.TypeOf(Study{})},
		{"Sample", reflect.TypeOf(Sample{})},
		{"Lane", reflect.TypeOf(Lane{})},
		{"IRODSPath", reflect.TypeOf(IRODSPath{})},
		{"SampleWithData", reflect.TypeOf(SampleWithData{})},
		{"Library", reflect.TypeOf(Library{})},
		{"Run", reflect.TypeOf(Run{})},
		{"SampleDetail", reflect.TypeOf(SampleDetail{})},
		{"StudyDetail", reflect.TypeOf(StudyDetail{})},
		{"RunDetail", reflect.TypeOf(RunDetail{})},
		{"LibraryDetail", reflect.TypeOf(LibraryDetail{})},
		{"LibraryLink", reflect.TypeOf(LibraryLink{})},
		{"EnrichmentResult", reflect.TypeOf(EnrichmentResult{})},
		{"EnrichmentGraph", reflect.TypeOf(EnrichmentGraph{})},
		{"MissingHop", reflect.TypeOf(MissingHop{})},
		{"SearchValues", reflect.TypeOf(SearchValues{})},
		{"Count", reflect.TypeOf(Count{})},
		{"Freshness", reflect.TypeOf(Freshness{})},
		{"TableFreshness", reflect.TypeOf(TableFreshness{})},
	}

	convey.Convey("Given the directly-served types, when reflected, then every JSON-serialised field has a non-empty doc tag", t, func() {
		fieldsMissingDoc := []string{}

		for _, served := range servedTypes {
			fieldsMissingDoc = append(fieldsMissingDoc, jsonFieldsMissingDocTag(served.name, served.typ)...)
		}

		convey.So(fieldsMissingDoc, convey.ShouldBeEmpty)
	})

	convey.Convey("Given the Study struct, when reflected, then every JSON-serialised field has a non-empty doc tag", t, func() {
		convey.So(jsonFieldsMissingDocTag("Study", reflect.TypeOf(Study{})), convey.ShouldBeEmpty)
	})

	convey.Convey("Given the Match struct, when reflected, then every JSON-serialised field has a non-empty doc tag", t, func() {
		convey.So(jsonFieldsMissingDocTag("Match", reflect.TypeOf(Match{})), convey.ShouldBeEmpty)
	})
}

func jsonFieldsMissingDocTag(typeName string, typ reflect.Type) []string {
	missing := []string{}

	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if jsonName == "-" {
			continue
		}

		if strings.TrimSpace(field.Tag.Get("doc")) == "" {
			missing = append(missing, typeName+"."+field.Name)
		}
	}

	return missing
}
