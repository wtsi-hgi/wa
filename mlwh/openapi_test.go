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
	"encoding/json"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

// openAPIB7InfoDescription is the exact info.description B7 requires.
const openAPIB7InfoDescription = "Cache-backed REST API mirroring Multi-LIMS Warehouse (MLWH) study, sample, run, and library metadata. MLWH data is read-only; the only documented write endpoint is POST /feedback, which stores agent feedback in a separate database. Unauthenticated by default; the network boundary is the access-control boundary."

func TestServerOpenAPIRouteC2(t *testing.T) {
	// C2 acceptance test 6: GET /openapi.json served with no auth returns 200,
	// a JSON content type, and a body that parses as the same document.
	convey.Convey("Given GET /openapi.json served with no auth, then status is 200, the content type is JSON, and the body parses", t, func() {
		queryer := &serverFakeQueryer{}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/openapi.json")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldContainSubstring, "application/json")

		var served map[string]any
		decodeMLWHJSONResponseForTest(t, response, &served)

		convey.So(served["openapi"], convey.ShouldEqual, "3.1.0")

		info, ok := served["info"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(info["title"], convey.ShouldEqual, "wa mlwh API")

		paths, ok := served["paths"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(paths, convey.ShouldContainKey, "/classify/{id}")
		convey.So(paths, convey.ShouldContainKey, "/health")
	})
}

func TestServerOpenAPIRouteDoesNotReadCacheC2(t *testing.T) {
	// /openapi.json is a static document like /health: it must not consult the
	// queryer, so a fake that panics on every cache-backed method still serves
	// the document.
	convey.Convey("Given GET /openapi.json over a queryer that panics on every cache method, then it still returns 200", t, func() {
		queryer := &serverFakeQueryer{}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/openapi.json")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
	})
}

func TestServerOpenAPIRouteServesDocumentC2(t *testing.T) {
	// The served document is built once and reused (memoised), but the route's
	// observable behaviour must be unchanged: the body must decode to exactly the
	// document OpenAPIDocument() produces, and successive requests must return the
	// identical body.
	convey.Convey("Given GET /openapi.json, then the served body equals OpenAPIDocument() and is stable across requests", t, func() {
		queryer := &serverFakeQueryer{}

		want, err := json.Marshal(OpenAPIDocument())
		convey.So(err, convey.ShouldBeNil)

		var wantDoc map[string]any
		convey.So(json.Unmarshal(want, &wantDoc), convey.ShouldBeNil)

		first := performMLWHRequestForTest(t, queryer, http.MethodGet, "/openapi.json")
		convey.So(first.Code, convey.ShouldEqual, http.StatusOK)

		var servedDoc map[string]any
		decodeMLWHJSONResponseForTest(t, first, &servedDoc)
		convey.So(reflect.DeepEqual(servedDoc, wantDoc), convey.ShouldBeTrue)

		second := performMLWHRequestForTest(t, queryer, http.MethodGet, "/openapi.json")
		convey.So(second.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(second.Body.Bytes(), convey.ShouldResemble, first.Body.Bytes())
	})
}

func TestAPIVersionIsThePhase1DocumentationPatch(t *testing.T) {
	// B7 acceptance test 1: documenting the new POST /feedback contract is a
	// minor API change, so the version moves from 1.8.1 to 1.9.0 and the served
	// info.version follows it.
	convey.Convey("Given the public APIVersion constant, then it equals 1.9.0 and the served info.version equals it", t, func() {
		convey.So(APIVersion, convey.ShouldEqual, "1.9.0")

		info := servedOpenAPIInfoForTest(t)
		convey.So(info["version"], convey.ShouldEqual, "1.9.0")
	})
}

func TestOpenAPIServedInfoDescriptionB7(t *testing.T) {
	// B7 acceptance test 8.
	convey.Convey("Given the served /openapi.json, then info.description is the B7 text naming POST /feedback", t, func() {
		info := servedOpenAPIInfoForTest(t)

		convey.So(info["description"], convey.ShouldEqual, openAPIB7InfoDescription)
		convey.So(info["description"], convey.ShouldContainSubstring, "POST /feedback")
		convey.So(info["description"], convey.ShouldNotContainSubstring, "read-only REST API")
	})
}

// servedOpenAPIInfoForTest fetches GET /openapi.json from a real server and
// returns its info object.
func servedOpenAPIInfoForTest(t *testing.T) map[string]any {
	t.Helper()

	response := performMLWHRequestForTest(t, &serverFakeQueryer{}, http.MethodGet, "/openapi.json")
	convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

	var served map[string]any
	decodeMLWHJSONResponseForTest(t, response, &served)

	info, ok := served["info"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return info
}

func TestOpenAPIDocumentIdentityC2(t *testing.T) {
	// C2 acceptance test 1.
	convey.Convey("Given the generated document, when parsed, then its identity fields match the spec", t, func() {
		doc := decodedOpenAPIDocForTest(t)

		convey.So(doc["openapi"], convey.ShouldEqual, "3.1.0")

		info, ok := doc["info"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(info["title"], convey.ShouldEqual, "wa mlwh API")
		convey.So(info["version"], convey.ShouldEqual, mlwhAPIVersion)
		convey.So(strings.TrimSpace(mlwhAPIVersion), convey.ShouldNotBeBlank)
	})
}

func TestOpenAPIPublicVersionConstantNoDrift(t *testing.T) {
	// An external consumer (e.g. an MCP server importing this package) must be
	// able to read the targeted API version as a typed, compiled-in symbol
	// without contacting a live server or walking the untyped document. Assert at
	// that public boundary: APIVersion is non-blank and equals the info.version
	// value actually served by OpenAPIDocument(), so the exported symbol and the
	// served document can never drift apart.
	convey.Convey("Given the public APIVersion constant, then it is non-blank and equals the served info.version", t, func() {
		convey.So(strings.TrimSpace(APIVersion), convey.ShouldNotBeBlank)

		doc := decodedOpenAPIDocForTest(t)

		info, ok := doc["info"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(info["version"], convey.ShouldEqual, APIVersion)
	})
}

func TestOpenAPIDocumentCoversRegistryPathsC2(t *testing.T) {
	// C2 acceptance test 2: every Registry entry's Path and Verb appears with
	// the correct path params and (for paginated entries) limit/offset query
	// params.
	convey.Convey("Given the document, when its paths are compared to Registry, then every entry appears with the right params", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		paths := openAPIPathsForTest(t, doc)

		missingPaths := []string{}
		missingPathParams := []string{}
		missingQueryParams := []string{}

		for _, entry := range Registry {
			openAPIPath := openAPIPathFromRegistry(entry.Path)

			item, ok := paths[openAPIPath].(map[string]any)
			if !ok {
				missingPaths = append(missingPaths, entry.Path)

				continue
			}

			operation, ok := item[strings.ToLower(entry.Verb)].(map[string]any)
			if !ok {
				missingPaths = append(missingPaths, entry.Verb+" "+entry.Path)

				continue
			}

			names := openAPIParameterNames(operation, "path")
			for _, param := range entry.PathParams {
				if !slices.Contains(names, param) {
					missingPathParams = append(missingPathParams, entry.Path+":"+param)
				}
			}

			if entry.Paginated {
				queryNames := openAPIParameterNames(operation, "query")
				for _, want := range []string{"limit", "offset"} {
					if !slices.Contains(queryNames, want) {
						missingQueryParams = append(missingQueryParams, entry.Path+":"+want)
					}
				}
			}
		}

		convey.So(missingPaths, convey.ShouldBeEmpty)
		convey.So(missingPathParams, convey.ShouldBeEmpty)
		convey.So(missingQueryParams, convey.ShouldBeEmpty)
	})
}

func TestOpenAPIDocumentOmitsManifestPathsG1(t *testing.T) {
	convey.Convey("G1.4: Given the generated OpenAPI document, when paths are inspected, then manifest paths are absent", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		paths := openAPIPathsForTest(t, doc)

		convey.So(paths, convey.ShouldNotContainKey, "/study/{id}/manifest")
		convey.So(paths, convey.ShouldNotContainKey, "/study/{id}/manifest/count")
	})
}

func TestOpenAPIDocumentCoversQueryerC2(t *testing.T) {
	// C2 acceptance test 3 plus the anti-drift coverage requirement: every
	// Queryer method name maps to exactly one documented path (1:1), and every
	// Registry entry appears in the document. Asserted by reflecting over a
	// Queryer-typed nil, mirroring TestRegistryCoversQueryer.
	convey.Convey("Given the Queryer interface and the document", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		methodCounts := openAPIQueryerMethodCounts(t, doc)
		queryer := reflect.TypeOf((*Queryer)(nil)).Elem()

		convey.Convey("when checked, then every Queryer method maps to exactly one documented path", func() {
			missing := []string{}
			notOneToOne := []string{}

			for i := range queryer.NumMethod() {
				name := queryer.Method(i).Name
				switch methodCounts[name] {
				case 0:
					missing = append(missing, name)
				case 1:
				default:
					notOneToOne = append(notOneToOne, name)
				}
			}

			convey.So(missing, convey.ShouldBeEmpty)
			convey.So(notOneToOne, convey.ShouldBeEmpty)
		})

		convey.Convey("when every Registry path+verb is looked up, then each is documented (anti-drift)", func() {
			paths := openAPIPathsForTest(t, doc)
			undocumented := []string{}

			for _, entry := range Registry {
				item, ok := paths[openAPIPathFromRegistry(entry.Path)].(map[string]any)
				if !ok {
					undocumented = append(undocumented, entry.Path)

					continue
				}
				if _, ok := item[strings.ToLower(entry.Verb)]; !ok {
					undocumented = append(undocumented, entry.Verb+" "+entry.Path)
				}
			}

			convey.So(undocumented, convey.ShouldBeEmpty)
		})

		convey.Convey("when one Registry entry is dropped from the document, then the coverage check catches the missing method", func() {
			// Build the document from a Registry missing its first entry and
			// confirm that entry's Queryer method is no longer documented, so the
			// coverage assertion genuinely fails on drift rather than passing
			// vacuously.
			convey.So(Registry, convey.ShouldNotBeEmpty)

			dropped := Registry[0]
			trimmed := slices.Clone(Registry[1:])
			partial := openAPIQueryerMethodCountsFromDoc(t, openAPIDocumentFromRegistry(trimmed))

			convey.So(methodCounts[dropped.Method], convey.ShouldEqual, 1)
			convey.So(partial[dropped.Method], convey.ShouldEqual, 0)
		})
	})
}

func TestOpenAPIMatchSchemaSnakeCaseC2(t *testing.T) {
	// C2 acceptance test 4.
	convey.Convey("Given the Match schema in the document, when inspected, then it has snake_case properties and no PascalCase keys", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		properties := openAPISchemaProperties(t, doc, "Match")

		for _, want := range []string{"kind", "canonical", "sample", "study", "run", "library"} {
			convey.So(properties, convey.ShouldContainKey, want)
		}

		convey.So(properties, convey.ShouldNotContainKey, "Kind")
		convey.So(properties, convey.ShouldNotContainKey, "Canonical")
	})
}

func TestOpenAPISchemaUsesDocTagDescriptionsC2(t *testing.T) {
	// The schemas must carry the doc: tags as field descriptions (C2: "use the
	// doc: tags as field descriptions").
	convey.Convey("Given the Study schema, when inspected, then its properties carry the doc tag descriptions", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		properties := openAPISchemaProperties(t, doc, "Study")

		name, ok := properties["name"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(name["description"], convey.ShouldEqual, "study name")
	})
}

func TestOpenAPIFreshnessSchemaDistinguishesSyncProgressFromCacheCurrency(t *testing.T) {
	convey.Convey("Given the freshness schema, then its field descriptions distinguish mode-specific sync progress from cache currency", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		tableProperties := openAPISchemaProperties(t, doc, "TableFreshness")

		highWater, ok := tableProperties["high_water"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		highWaterDescription, ok := highWater["description"].(string)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(highWaterDescription, convey.ShouldContainSubstring, "sync-mode-specific source-progress watermark")
		convey.So(highWaterDescription, convey.ShouldContainSubstring, "latest source-row change for incremental tables")
		convey.So(highWaterDescription, convey.ShouldContainSubstring, "refresh/snapshot time for full-refresh tables")
		convey.So(highWaterDescription, convey.ShouldContainSubstring, "empty for unsynced tables and sync modes without a meaningful watermark")
		convey.So(highWaterDescription, convey.ShouldContainSubstring, "may remain old when source data is unchanged")
		convey.So(highWaterDescription, convey.ShouldContainSubstring, "do not use as cache refresh currency")

		lastRun, ok := tableProperties["last_run"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		lastRunDescription, ok := lastRun["description"].(string)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(lastRunDescription, convey.ShouldContainSubstring, "last cache sync/refresh time")
		convey.So(lastRunDescription, convey.ShouldContainSubstring, "per-table cache currency and as-of caveats")

		freshnessProperties := openAPISchemaProperties(t, doc, "Freshness")
		tables, ok := freshnessProperties["tables"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		tablesDescription, ok := tables["description"].(string)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(tablesDescription, convey.ShouldContainSubstring, "use last_run, not high_water, for cache currency")
	})
}

func TestOpenAPISchemaExplainsEGAAccessionTerminologyC2(t *testing.T) {
	convey.Convey("Given the generated OpenAPI schemas, then every accession-number property explains EGA terminology and the canonical field name", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		schemas := openAPISchemas(t, doc)
		checked := 0

		for _, rawSchema := range schemas {
			schema, ok := rawSchema.(map[string]any)
			if !ok {
				continue
			}

			properties, ok := schema["properties"].(map[string]any)
			if !ok {
				continue
			}

			for _, fieldName := range []string{"accession_number", "study_accession_number"} {
				property, ok := properties[fieldName].(map[string]any)
				if !ok {
					continue
				}

				description, ok := property["description"].(string)
				convey.So(ok, convey.ShouldBeTrue)
				convey.So(description, convey.ShouldContainSubstring, "EGA ID")
				convey.So(description, convey.ShouldContainSubstring, "EGA accession")
				convey.So(description, convey.ShouldContainSubstring, fieldName)
				checked++
			}
		}

		convey.So(checked, convey.ShouldBeGreaterThan, 3)
	})
}

func TestOpenAPISchemaRequiredHonoursOmitemptyC2(t *testing.T) {
	// Pointer / omitempty fields must not be required; plain value fields must
	// be (C2: "Handle ... omitempty correctly (omitempty / pointer => not
	// required)").
	convey.Convey("Given the Match schema, when inspected, then required reflects pointer/omitempty fields", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		schema := openAPISchema(t, doc, "Match")

		required := openAPIStringSlice(schema["required"])
		convey.So(required, convey.ShouldContain, "kind")
		convey.So(required, convey.ShouldContain, "canonical")
		convey.So(required, convey.ShouldNotContain, "sample")
		convey.So(required, convey.ShouldNotContain, "study")
	})
}

func TestOpenAPINestedStructReferencedC2(t *testing.T) {
	// Nested struct fields must reference the nested schema rather than inlining
	// it (C2: "Handle nested structs, slices, pointers ... correctly").
	convey.Convey("Given the SampleDetail schema, when inspected, then nested struct and slice fields reference their schemas", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		properties := openAPISchemaProperties(t, doc, "SampleDetail")

		sample, ok := properties["sample"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(openAPIResolveRef(sample), convey.ShouldEqual, "#/components/schemas/Sample")

		lanes, ok := properties["lanes"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(lanes["type"], convey.ShouldEqual, "array")
		items, ok := lanes["items"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(openAPIResolveRef(items), convey.ShouldEqual, "#/components/schemas/Lane")
	})
}

func TestOpenAPIErrorEnvelopeC2(t *testing.T) {
	// C2 acceptance test 5.
	convey.Convey("Given the document, when inspected, then it defines the error envelope and the nine stable codes with their statuses", t, func() {
		doc := decodedOpenAPIDocForTest(t)

		properties := openAPISchemaProperties(t, doc, "Error")
		convey.So(properties, convey.ShouldContainKey, "code")
		convey.So(properties, convey.ShouldContainKey, "message")

		statusByCode := openAPIDocumentedErrorCodes(t, doc)
		convey.So(statusByCode, convey.ShouldResemble, map[string]string{
			"not_found":              "404",
			"ambiguous":              "409",
			"unsupported_identifier": "422",
			"cache_never_synced":     "503",
			"upstream_impaired":      "502",
			"bad_request":            "400",
			"payload_too_large":      "413",
			"internal_error":         "500",
			"feedback_disabled":      "503",
		})
	})
}

func TestOpenAPIResponseSchemasByPathC2(t *testing.T) {
	// C2 acceptance test 7.
	convey.Convey("Given the search/count/freshness responses, when looked up by path, then each 200 response references the correct schema", t, func() {
		doc := decodedOpenAPIDocForTest(t)

		convey.So(openAPIObjectResponseRef(t, doc, "/freshness", "get"), convey.ShouldEqual, "#/components/schemas/Freshness")
		convey.So(openAPIObjectResponseRef(t, doc, "/studies/count", "get"), convey.ShouldEqual, "#/components/schemas/Count")
		convey.So(openAPIArrayResponseItemRef(t, doc, "/search/study/{term}", "get"), convey.ShouldEqual, "#/components/schemas/Study")
		convey.So(openAPIArrayResponseItemRef(t, doc, "/search/sample/{term}", "get"), convey.ShouldEqual, "#/components/schemas/Sample")
	})
}

func TestOpenAPIDocumentIncludesHealthD1(t *testing.T) {
	// D1 acceptance test 3: /health appears in the document with a 200 {status}
	// response even though it is a plain route, not a Registry entry.
	convey.Convey("Given the OpenAPI document, when inspected, then /health appears with a 200 {status} response", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		paths := openAPIPathsForTest(t, doc)

		item, ok := paths["/health"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)

		operation, ok := item["get"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)

		schema := openAPIResponseSchema(t, operation, "200")
		properties, ok := schema["properties"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(properties, convey.ShouldContainKey, "status")
	})
}

func TestOpenAPIFeedbackPostOperationB7(t *testing.T) {
	// B7 acceptance test 2.
	convey.Convey("Given the document, then POST /feedback has a required FeedbackSubmission body, the feedback responses, and no Queryer method", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		operation := openAPIOperation(t, doc, "/feedback", "post")

		convey.So(operation["summary"], convey.ShouldEqual, "Submit agent feedback")
		convey.So(operation, convey.ShouldNotContainKey, "x-queryer-method")

		description, ok := operation["description"].(string)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(description, convey.ShouldContainSubstring, "not a Registry endpoint")
		convey.So(description, convey.ShouldContainSubstring, "AuthRouter()")
		convey.So(description, convey.ShouldContainSubstring, "secured mode")

		requestBody, ok := operation["requestBody"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(requestBody["required"], convey.ShouldEqual, true)

		content, ok := requestBody["content"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(slices.Sorted(maps.Keys(content)), convey.ShouldResemble, []string{"application/json"})

		mediaType, ok := content["application/json"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)

		schema, ok := mediaType["schema"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(schema, convey.ShouldResemble, map[string]any{"$ref": "#/components/schemas/FeedbackSubmission"})

		responses, ok := operation["responses"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(slices.Sorted(maps.Keys(responses)), convey.ShouldResemble, []string{"201", "400", "413", "500", "503"})

		receipt := openAPIResponseSchema(t, operation, "201")
		convey.So(receipt, convey.ShouldResemble, map[string]any{"$ref": "#/components/schemas/FeedbackReceipt"})

		for status, code := range map[string]string{
			"400": "bad_request",
			"413": "payload_too_large",
			"500": "internal_error",
			"503": "feedback_disabled",
		} {
			convey.So(openAPIResponseExampleCodes(responses[status]), convey.ShouldResemble, []string{code})
			convey.So(openAPIResponseSchema(t, operation, status), convey.ShouldResemble,
				map[string]any{"$ref": "#/components/schemas/Error"})
		}
	})
}

func TestOpenAPIRegistryErrorResponsesUnchangedB7(t *testing.T) {
	// B7: openAPIErrorResponses() (the six Registry codes) is unchanged, so no
	// Registry operation documents a feedback-only error code.
	convey.Convey("Given a Registry operation, then its error responses are still exactly the six Registry statuses", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		operation := openAPIOperation(t, doc, "/freshness", "get")

		responses, ok := operation["responses"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(slices.Sorted(maps.Keys(responses)), convey.ShouldResemble,
			[]string{"200", "400", "404", "409", "422", "502", "503"})
		convey.So(openAPIResponseExampleCodes(responses["503"]), convey.ShouldResemble, []string{"cache_never_synced"})
	})
}

func TestOpenAPIFeedbackSubmissionSchemaB7(t *testing.T) {
	// B7 acceptance test 3.
	convey.Convey("Given component FeedbackSubmission, then it has the 10 JSON fields, the required pair, the category enum, and allows unknown fields", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		schema := openAPISchema(t, doc, "FeedbackSubmission")

		properties, ok := schema["properties"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(slices.Sorted(maps.Keys(properties)), convey.ShouldResemble, []string{
			"category", "client_name", "client_user_agent", "client_version", "description",
			"mcp_server_version", "tools_tried", "transport", "user_request", "wa_api_version",
		})
		convey.So(openAPIStringSlice(schema["required"]), convey.ShouldResemble, []string{"category", "description"})
		convey.So(schema["additionalProperties"], convey.ShouldEqual, true)

		category, ok := properties["category"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(category["type"], convey.ShouldEqual, "string")
		convey.So(openAPIStringSlice(category["enum"]), convey.ShouldResemble, []string{
			"could_not_answer", "agent_mistake", "no_endpoint", "user_unhappy", "other",
		})

		for name, rawProperty := range properties {
			if name == "category" {
				continue
			}

			property, ok := rawProperty.(map[string]any)
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(property, convey.ShouldNotContainKey, "enum")
		}
	})

	convey.Convey("Given every other component, then additionalProperties is still false", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		schemas := openAPISchemas(t, doc)

		convey.So(openAPISchema(t, doc, "FeedbackReceipt")["additionalProperties"], convey.ShouldEqual, false)

		open := []string{}

		for name, rawSchema := range schemas {
			if name == "FeedbackSubmission" {
				continue
			}

			schema, ok := rawSchema.(map[string]any)
			convey.So(ok, convey.ShouldBeTrue)

			if schema["additionalProperties"] != false {
				open = append(open, name)
			}
		}

		convey.So(open, convey.ShouldBeEmpty)
	})
}

func TestOpenAPIFeedbackReceiptSchemaB7(t *testing.T) {
	// B7 acceptance test 4.
	convey.Convey("Given component FeedbackReceipt, then id is an integer, created_at is a string, and both are required", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		schema := openAPISchema(t, doc, "FeedbackReceipt")

		properties, ok := schema["properties"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(slices.Sorted(maps.Keys(properties)), convey.ShouldResemble, []string{"created_at", "id"})

		id, ok := properties["id"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(id["type"], convey.ShouldEqual, "integer")

		createdAt, ok := properties["created_at"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(createdAt["type"], convey.ShouldEqual, "string")

		convey.So(openAPIStringSlice(schema["required"]), convey.ShouldResemble, []string{"id", "created_at"})
	})
}

func TestOpenAPIFeedbackAdminRoutesUndocumentedB7(t *testing.T) {
	// B7 acceptance test 5.
	convey.Convey("Given the document, then /feedback documents only post and /feedback/{id} is absent", t, func() {
		doc := decodedOpenAPIDocForTest(t)
		paths := openAPIPathsForTest(t, doc)

		item, ok := paths["/feedback"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(item, convey.ShouldNotContainKey, "get")
		convey.So(item, convey.ShouldNotContainKey, "patch")
		convey.So(item, convey.ShouldNotContainKey, "delete")
		convey.So(slices.Sorted(maps.Keys(item)), convey.ShouldResemble, []string{"post"})

		convey.So(paths, convey.ShouldNotContainKey, "/feedback/{id}")
	})
}

// decodedOpenAPIDocForTest marshals the generated document to JSON and decodes
// it back into a generic map, exercising the same path the served route uses so
// the tests assert the wire shape rather than the in-memory Go value.
func decodedOpenAPIDocForTest(t *testing.T) map[string]any {
	t.Helper()

	raw, err := json.Marshal(OpenAPIDocument())
	convey.So(err, convey.ShouldBeNil)

	var doc map[string]any
	convey.So(json.Unmarshal(raw, &doc), convey.ShouldBeNil)

	return doc
}

func openAPIObjectResponseRef(t *testing.T, doc map[string]any, path, verb string) string {
	t.Helper()

	schema := openAPIResponseSchema(t, openAPIOperation(t, doc, path, verb), "200")
	ref, _ := schema["$ref"].(string)

	return ref
}

func openAPIArrayResponseItemRef(t *testing.T, doc map[string]any, path, verb string) string {
	t.Helper()

	schema := openAPIResponseSchema(t, openAPIOperation(t, doc, path, verb), "200")
	convey.So(schema["type"], convey.ShouldEqual, "array")

	items, ok := schema["items"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)
	ref, _ := items["$ref"].(string)

	return ref
}

func openAPISchemaProperties(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()

	properties, ok := openAPISchema(t, doc, name)["properties"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return properties
}

func openAPISchema(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()

	schemas := openAPISchemas(t, doc)
	schema, ok := schemas[name].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return schema
}

func openAPISchemas(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()

	components, ok := doc["components"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	schemas, ok := components["schemas"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return schemas
}

// openAPIDocumentedErrorCodes walks every operation's error responses and maps
// each documented stable error code to the HTTP status it is documented under,
// proving the six codes appear with their statuses somewhere in the document.
func openAPIDocumentedErrorCodes(t *testing.T, doc map[string]any) map[string]string {
	t.Helper()

	statusByCode := map[string]string{}
	paths := openAPIPathsForTest(t, doc)

	for _, rawItem := range paths {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}

		for _, rawOp := range item {
			operation, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}

			collectOpenAPIErrorCodes(operation, statusByCode)
		}
	}

	return statusByCode
}

// openAPIResolveRef returns the schema's $ref, resolving the allOf wrapper the
// generator uses when a referenced field also carries a description (a $ref
// alongside a description is modelled as {description, allOf:[{$ref}]}).
func openAPIResolveRef(schema map[string]any) string {
	if ref, ok := schema["$ref"].(string); ok {
		return ref
	}

	allOf, ok := schema["allOf"].([]any)
	if !ok || len(allOf) != 1 {
		return ""
	}

	wrapped, ok := allOf[0].(map[string]any)
	if !ok {
		return ""
	}

	ref, _ := wrapped["$ref"].(string)

	return ref
}

func openAPIStringSlice(raw any) []string {
	rawSlice, ok := raw.([]any)
	if !ok {
		return nil
	}

	out := make([]string, 0, len(rawSlice))
	for _, item := range rawSlice {
		if str, ok := item.(string); ok {
			out = append(out, str)
		}
	}

	return out
}

// openAPIQueryerMethodCounts decodes the document and counts, per Queryer method
// name, how many documented operations carry it (via the x-queryer-method
// extension the generator stamps from Registry).
func openAPIQueryerMethodCounts(t *testing.T, doc map[string]any) map[string]int {
	t.Helper()

	return openAPIQueryerMethodCountsFromDoc(t, doc)
}

// openAPIPathFromRegistry converts a gin-style Registry path (":param") to the
// OpenAPI path templating form ("{param}") the document uses.
func openAPIPathFromRegistry(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if strings.HasPrefix(segment, ":") {
			segments[i] = "{" + segment[1:] + "}"
		}
	}

	return strings.Join(segments, "/")
}

func openAPIResponseSchema(t *testing.T, operation map[string]any, status string) map[string]any {
	t.Helper()

	responses, ok := operation["responses"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	response, ok := responses[status].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	content, ok := response["content"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	mediaType, ok := content["application/json"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	schema, ok := mediaType["schema"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return schema
}

func openAPIOperation(t *testing.T, doc map[string]any, path, verb string) map[string]any {
	t.Helper()

	paths := openAPIPathsForTest(t, doc)

	item, ok := paths[path].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	operation, ok := item[verb].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return operation
}

func openAPIQueryerMethodCountsFromDoc(t *testing.T, doc map[string]any) map[string]int {
	t.Helper()

	counts := map[string]int{}
	paths := openAPIPathsForTest(t, doc)

	for _, rawItem := range paths {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}

		for _, rawOp := range item {
			operation, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			if method, ok := operation["x-queryer-method"].(string); ok && method != "" {
				counts[method]++
			}
		}
	}

	return counts
}

func openAPIPathsForTest(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()

	paths, ok := doc["paths"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return paths
}

func collectOpenAPIErrorCodes(operation map[string]any, statusByCode map[string]string) {
	responses, ok := operation["responses"].(map[string]any)
	if !ok {
		return
	}

	for status, rawResponse := range responses {
		if status == "200" {
			continue
		}

		for _, code := range openAPIResponseExampleCodes(rawResponse) {
			statusByCode[code] = status
		}
	}
}

// openAPIResponseExampleCodes extracts the documented stable code value(s) from
// an error response object (carried in the example of the Error schema body).
func openAPIResponseExampleCodes(rawResponse any) []string {
	response, ok := rawResponse.(map[string]any)
	if !ok {
		return nil
	}

	content, ok := response["content"].(map[string]any)
	if !ok {
		return nil
	}

	mediaType, ok := content["application/json"].(map[string]any)
	if !ok {
		return nil
	}

	example, ok := mediaType["example"].(map[string]any)
	if !ok {
		return nil
	}

	code, ok := example["code"].(string)
	if !ok || code == "" {
		return nil
	}

	return []string{code}
}

// openAPIDocumentFromRegistry builds a document from an arbitrary endpoint slice
// by temporarily swapping the package Registry, so the anti-drift test can prove
// a missing entry drops out of the generated document.
func openAPIDocumentFromRegistry(entries []Endpoint) map[string]any {
	original := Registry
	Registry = entries
	defer func() { Registry = original }()

	raw, _ := json.Marshal(OpenAPIDocument())

	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)

	return doc
}

// openAPIParameterNames returns the names of the operation's parameters that
// live in the given location ("path" or "query").
func openAPIParameterNames(operation map[string]any, in string) []string {
	rawParams, ok := operation["parameters"].([]any)
	if !ok {
		return nil
	}

	names := []string{}

	for _, raw := range rawParams {
		param, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if param["in"] != in {
			continue
		}

		if name, ok := param["name"].(string); ok {
			names = append(names, name)
		}
	}

	return names
}
