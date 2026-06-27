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
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

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
	convey.Convey("Given the document, when inspected, then it defines the error envelope and the six stable codes with their statuses", t, func() {
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

	components, ok := doc["components"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	schemas, ok := components["schemas"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	schema, ok := schemas[name].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return schema
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
