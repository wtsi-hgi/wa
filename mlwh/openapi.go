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
	"net/http"
	"reflect"
	"strings"
)

// mlwhAPIVersion is the semantic version of the served MLWH REST API, surfaced
// as info.version in the OpenAPI document. Its minor component tracks the cache
// schema-version lineage (CacheSchemaVersion) so the documented API version
// moves with each schema/contract change; the patch component is reserved for
// documentation-only revisions within a schema version.
const mlwhAPIVersion = "1.5.0"

// openAPIVersion is the OpenAPI specification version the generated document
// conforms to.
const openAPIVersion = "3.1.0"

// openAPISchemaRefPrefix is the JSON-pointer prefix every component schema is
// referenced by ($ref).
const openAPISchemaRefPrefix = "#/components/schemas/"

// openAPIErrorSchemaName is the component schema name of the {code, message}
// error envelope shared by every error response.
const openAPIErrorSchemaName = "Error"

// openAPIErrorResponse pairs a stable error code with the HTTP status it is
// returned under, derived from httpStatusAndErrorCode in errors_http.go plus the
// handler's bad_request 400. Documented on every Registry operation so an MCP
// implementor sees the full failure contract without reading Go source.
type openAPIErrorResponse struct {
	status      string
	code        string
	description string
}

// openAPIErrorResponses are the six stable error responses, in ascending status
// order, that every documented endpoint may return.
func openAPIErrorResponses() []openAPIErrorResponse {
	return []openAPIErrorResponse{
		{status: "400", code: "bad_request", description: "the request was malformed (e.g. a non-integer or over-maximum limit/offset)"},
		{status: "404", code: httpErrorCodeNotFound, description: "the identifier was not found"},
		{status: "409", code: httpErrorCodeAmbiguous, description: "the identifier matched multiple records"},
		{status: "422", code: httpErrorCodeUnsupportedIdentifier, description: "the identifier form is not supported"},
		{status: "502", code: httpErrorCodeUpstreamImpaired, description: "the cache backend was impaired"},
		{status: "503", code: httpErrorCodeCacheNeverSynced, description: "the cache has never been synced"},
	}
}

// openAPIErrorResponseObject builds the response object for one stable error
// code, referencing the shared Error schema and carrying an example whose code
// is the stable code so the document records each code with its status.
func openAPIErrorResponseObject(errResponse openAPIErrorResponse) map[string]any {
	return map[string]any{
		"description": errResponse.description,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{"$ref": openAPISchemaRefPrefix + openAPIErrorSchemaName},
				"example": map[string]any{
					"code":    errResponse.code,
					"message": errResponse.description,
				},
			},
		},
	}
}

// openAPISchemaCollector builds and de-duplicates the component schemas reachable
// from the Registry result types, keyed by the Go type name.
type openAPISchemaCollector struct {
	schemas map[string]any
}

// schemaForResult returns the schema for an entry's NewResult() value. The value
// is a pointer to the result (e.g. *Match or *[]Sample); the schema describes
// the pointed-to type.
func (c *openAPISchemaCollector) schemaForResult(result any) map[string]any {
	typ := reflect.TypeOf(result)
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	return c.schemaForType(typ)
}

// schemaForType returns an inline schema for typ, registering any named struct
// it encounters as a component and referencing it. Slices, pointers, and named
// structs are handled recursively.
func (c *openAPISchemaCollector) schemaForType(typ reflect.Type) map[string]any {
	switch typ.Kind() {
	case reflect.Pointer:
		return c.schemaForType(typ.Elem())
	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": c.schemaForType(typ.Elem()),
		}
	case reflect.Struct:
		c.registerStruct(typ)

		return map[string]any{"$ref": openAPISchemaRefPrefix + typ.Name()}
	default:
		return openAPIScalarSchema(typ)
	}
}

// openAPIScalarSchema maps a Go scalar type to its OpenAPI scalar schema.
func openAPIScalarSchema(typ reflect.Type) map[string]any {
	switch typ.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	default:
		return map[string]any{"type": "string"}
	}
}

// registerStruct adds typ's object schema to the component schemas if it is not
// already present, recursing into its fields. It is registered before its fields
// are walked so recursive or mutually-referential types terminate.
func (c *openAPISchemaCollector) registerStruct(typ reflect.Type) {
	name := typ.Name()
	if name == "" {
		return
	}
	if _, ok := c.schemas[name]; ok {
		return
	}

	c.schemas[name] = map[string]any{}

	properties := map[string]any{}
	required := []any{}

	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		jsonName, omitempty, skip := openAPIJSONFieldName(field)
		if skip {
			continue
		}

		properties[jsonName] = c.schemaForField(field)

		if !omitempty && field.Type.Kind() != reflect.Pointer {
			required = append(required, jsonName)
		}
	}

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	c.schemas[name] = schema
}

// openAPIJSONFieldName resolves a struct field's JSON property name, whether it
// is omitempty, and whether it is skipped (json:"-").
func openAPIJSONFieldName(field reflect.StructField) (string, bool, bool) {
	tag := field.Tag.Get("json")
	name, rest, _ := strings.Cut(tag, ",")

	if name == "-" {
		return "", false, true
	}
	if name == "" {
		name = field.Name
	}

	omitempty := slicesContainsCSV(rest, "omitempty")

	return name, omitempty, false
}

// schemaForField returns the schema for a struct field, attaching the doc: tag
// as the field description where one is present.
func (c *openAPISchemaCollector) schemaForField(field reflect.StructField) map[string]any {
	schema := c.schemaForType(field.Type)

	doc := strings.TrimSpace(field.Tag.Get("doc"))
	if doc == "" {
		return schema
	}

	// A $ref schema cannot carry sibling keywords in strict OpenAPI 3.1, so
	// describe a referenced field by wrapping the ref under allOf.
	if ref, ok := schema["$ref"]; ok {
		return map[string]any{
			"description": doc,
			"allOf":       []any{map[string]any{"$ref": ref}},
		}
	}

	schema["description"] = doc

	return schema
}

// OpenAPIDocument assembles the OpenAPI 3.1.0 document describing the served
// MLWH API. It is generated from the Registry (paths, verbs, path params,
// summaries, descriptions, query params), reflection over each entry's
// NewResult() type for the response schemas (post-Goal-4 snake_case field names
// from json: tags, with doc: tags as field descriptions), and the {code,
// message} error envelope. The plain GET /health route is added explicitly
// because it is not a Registry entry. The result marshals directly to the JSON
// served at GET /openapi.json.
func OpenAPIDocument() map[string]any {
	schemas := map[string]any{}
	collector := &openAPISchemaCollector{schemas: schemas}

	paths := map[string]any{}
	for _, entry := range Registry {
		addOpenAPIRegistryPath(paths, entry, collector)
	}

	addOpenAPIHealthPath(paths, collector)

	schemas[openAPIErrorSchemaName] = openAPIErrorSchema()

	return map[string]any{
		"openapi": openAPIVersion,
		"info": map[string]any{
			"title":       "wa mlwh API",
			"version":     mlwhAPIVersion,
			"description": "Cache-backed, read-only REST API mirroring Multi-LIMS Warehouse (MLWH) study, sample, run, and library metadata. Unauthenticated by default; the network boundary is the access-control boundary.",
		},
		"paths": paths,
		"components": map[string]any{
			"schemas": schemas,
		},
	}
}

// openAPIErrorSchema is the {code, message} error envelope component schema.
func openAPIErrorSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"description":          "Error envelope returned by every endpoint on failure.",
		"properties": map[string]any{
			"code": map[string]any{
				"type":        "string",
				"description": "stable machine-readable error code",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "human-readable error message",
			},
		},
		"required": []any{"code", "message"},
	}
}

// addOpenAPIRegistryPath adds one Registry entry's path item and operation to
// the document's paths, registering any referenced component schemas.
func addOpenAPIRegistryPath(paths map[string]any, entry Endpoint, collector *openAPISchemaCollector) {
	operation := map[string]any{
		"summary":          entry.Summary,
		"description":      entry.Description,
		"x-queryer-method": entry.Method,
		"responses":        openAPISuccessAndErrorResponses(entry, collector),
	}

	if params := openAPIOperationParameters(entry); len(params) > 0 {
		operation["parameters"] = params
	}

	addOpenAPIOperation(paths, openAPIPathTemplate(entry.Path), strings.ToLower(entry.Verb), operation)
}

// openAPIOperationParameters builds the path and query parameter objects for an
// entry, in path-then-query order.
func openAPIOperationParameters(entry Endpoint) []any {
	params := make([]any, 0, len(entry.PathParams)+len(entry.QueryParams))

	for _, name := range entry.PathParams {
		params = append(params, map[string]any{
			"name":        name,
			"in":          "path",
			"required":    true,
			"description": "the " + name + " path parameter",
			"schema":      map[string]any{"type": "string"},
		})
	}

	for _, param := range entry.QueryParams {
		params = append(params, map[string]any{
			"name":        param.Name,
			"in":          "query",
			"required":    param.Required,
			"description": param.Description,
			"schema":      map[string]any{"type": openAPIScalarType(param.Type)},
		})
	}

	return params
}

// openAPIScalarType normalises a declared QueryParam type to a known OpenAPI
// scalar type name, defaulting to string.
func openAPIScalarType(declared string) string {
	switch declared {
	case "integer", "number", "boolean", "string":
		return declared
	default:
		return "string"
	}
}

// addOpenAPIOperation attaches an operation (verb) to a path item, creating the
// path item if it does not yet exist.
func addOpenAPIOperation(paths map[string]any, path, verb string, operation map[string]any) {
	item, ok := paths[path].(map[string]any)
	if !ok {
		item = map[string]any{}
		paths[path] = item
	}

	item[verb] = operation
}

// openAPIPathTemplate converts a gin-style Registry path (":param") to the
// OpenAPI path-templating form ("{param}").
func openAPIPathTemplate(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if strings.HasPrefix(segment, ":") {
			segments[i] = "{" + segment[1:] + "}"
		}
	}

	return strings.Join(segments, "/")
}

// openAPISuccessAndErrorResponses builds the responses object for an entry: the
// 200 response referencing the reflected result schema plus the six stable error
// responses.
func openAPISuccessAndErrorResponses(entry Endpoint, collector *openAPISchemaCollector) map[string]any {
	responses := map[string]any{
		"200": openAPIJSONResponse("the matching "+entry.Summary, collector.schemaForResult(entry.NewResult())),
	}

	for _, errResponse := range openAPIErrorResponses() {
		responses[errResponse.status] = openAPIErrorResponseObject(errResponse)
	}

	return responses
}

// openAPIJSONResponse wraps a schema as an application/json 200-style response.
func openAPIJSONResponse(description string, schema map[string]any) map[string]any {
	return map[string]any{
		"description": description,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": schema,
			},
		},
	}
}

// addOpenAPIHealthPath adds the plain GET /health operational route. It is not a
// Registry entry, so it is documented explicitly with its static {status}
// response (D1 acceptance test 3).
func addOpenAPIHealthPath(paths map[string]any, _ *openAPISchemaCollector) {
	operation := map[string]any{
		"summary":     "Liveness probe",
		"description": "Returns a cheap static {\"status\":\"ok\"} body for readiness checks. Performs no read of the mirrored data, so it never surfaces a never-synced error.",
		"responses": map[string]any{
			"200": openAPIJSONResponse("liveness status", map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"status": map[string]any{
						"type":        "string",
						"description": "always \"ok\" when the server is live",
					},
				},
				"required": []any{"status"},
			}),
		},
	}

	addOpenAPIOperation(paths, "/health", strings.ToLower(http.MethodGet), operation)
}

// slicesContainsCSV reports whether a comma-separated option list contains opt.
func slicesContainsCSV(csv, opt string) bool {
	for _, part := range strings.Split(csv, ",") {
		if part == opt {
			return true
		}
	}

	return false
}
