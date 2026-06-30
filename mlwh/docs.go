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
	"fmt"
	"reflect"
	"strings"
)

// endpointReferenceTitle and endpointReferenceIntro head the generated human
// endpoint reference, explaining that it is generated from the same Registry
// metadata as the OpenAPI document and must not be hand-edited.
const (
	endpointReferenceTitle = "# wa mlwh API endpoint reference"
	endpointReferenceIntro = "This catalogue lists every endpoint of the cache-backed, read-only `wa mlwh\n" +
		"serve` REST API. It is **generated** from the same enriched `Registry`\n" +
		"metadata in `mlwh/registry.go` that produces the machine-readable OpenAPI\n" +
		"document at `GET /openapi.json`, so it cannot drift from the served API. Do\n" +
		"not edit it by hand; refresh it with `go test ./mlwh -run\n" +
		"TestWriteEndpointReference` after changing the `Registry`.\n\n" +
		"All endpoints are HTTP `GET`, return JSON, and are unauthenticated by\n" +
		"default. Path parameters are shown as `:name`; paginated list and search\n" +
		"endpoints accept the `limit` and `offset` query parameters described per\n" +
		"entry. On failure every endpoint returns the shared `{code, message}` error\n" +
		"envelope (see the OpenAPI document for the full set of error codes and\n" +
		"statuses). Field-level descriptions of each response type are in the OpenAPI\n" +
		"document; the domain entities are defined in `glossary.md`."
)

// EndpointReference generates the human-readable Markdown endpoint reference
// from the enriched Registry metadata (every endpoint's path, path params,
// query params, summary, description, and response type). The output is
// deterministic - entries are emitted in Registry order - so the committed
// .docs/mcp/api-reference.md can be diffed against it by a no-drift test. The
// same Registry is the single source the OpenAPI generator reads, so the human
// and machine forms cannot diverge.
func EndpointReference() string {
	var builder strings.Builder

	builder.WriteString(endpointReferenceTitle)
	builder.WriteString("\n\n")
	builder.WriteString(endpointReferenceIntro)
	builder.WriteString("\n\n## Endpoints\n")

	for _, entry := range Registry {
		builder.WriteString("\n")
		writeEndpointReferenceEntry(&builder, entry)
	}

	return builder.String()
}

// writeEndpointReferenceEntry renders one Registry entry: its verb+path heading,
// summary, description, path params, query params (where present), and response
// type.
func writeEndpointReferenceEntry(builder *strings.Builder, entry Endpoint) {
	fmt.Fprintf(builder, "### `%s %s`\n\n", entry.Verb, entry.Path)
	fmt.Fprintf(builder, "%s\n\n", entry.Summary)
	fmt.Fprintf(builder, "%s\n\n", endpointReferenceMarkdownText(entry.Description))

	fmt.Fprintf(builder, "- Path parameters: %s\n", endpointReferencePathParams(entry))
	fmt.Fprintf(builder, "- Query parameters: %s\n", endpointReferenceQueryParams(entry))
	fmt.Fprintf(builder, "- Response: `%s`\n", endpointResponseTypeName(entry))
}

// endpointReferencePathParams renders an entry's path parameters as an inline
// list, or "none" when it has none.
func endpointReferencePathParams(entry Endpoint) string {
	if len(entry.PathParams) == 0 {
		return "none"
	}

	quoted := make([]string, 0, len(entry.PathParams))
	for _, name := range entry.PathParams {
		quoted = append(quoted, "`"+name+"`")
	}

	return strings.Join(quoted, ", ")
}

// endpointReferenceQueryParams renders an entry's query parameters with their
// type and description, or "none" when it has none.
func endpointReferenceQueryParams(entry Endpoint) string {
	if len(entry.QueryParams) == 0 {
		return "none"
	}

	rendered := make([]string, 0, len(entry.QueryParams))
	for _, param := range entry.QueryParams {
		rendered = append(rendered, fmt.Sprintf("`%s` (%s): %s", param.Name, param.Type, endpointReferenceMarkdownText(param.Description)))
	}

	return strings.Join(rendered, "; ")
}

// endpointReferenceMarkdownText escapes prose characters that Markdown can
// reinterpret as emphasis, while leaving inline code spans as source text.
func endpointReferenceMarkdownText(text string) string {
	runes := []rune(text)
	var builder strings.Builder
	builder.Grow(len(text))

	inCodeSpan := false
	for index, char := range runes {
		switch char {
		case '`':
			inCodeSpan = !inCodeSpan
			builder.WriteRune(char)
		case '_':
			if !inCodeSpan && !endpointReferenceInnerWordChar(runes, index) {
				builder.WriteRune('\\')
			}
			builder.WriteRune(char)
		case '*':
			if !inCodeSpan {
				builder.WriteRune('\\')
			}
			builder.WriteRune(char)
		default:
			builder.WriteRune(char)
		}
	}

	return builder.String()
}

func endpointReferenceInnerWordChar(runes []rune, index int) bool {
	return index > 0 && index+1 < len(runes) &&
		endpointReferenceWordChar(runes[index-1]) &&
		endpointReferenceWordChar(runes[index+1])
}

func endpointReferenceWordChar(char rune) bool {
	return (char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9')
}

// endpointResponseTypeName derives the human response-type label for an entry
// by reflecting over its NewResult() value, mirroring the OpenAPI generator's
// schema derivation: a slice result is rendered as "[]Element" and a struct
// result as its type name.
func endpointResponseTypeName(entry Endpoint) string {
	typ := reflect.TypeOf(entry.NewResult())
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	return reflectTypeLabel(typ)
}

// reflectTypeLabel returns a readable label for a reflected response type,
// recursing through slices so the element type is named.
func reflectTypeLabel(typ reflect.Type) string {
	if typ == nil {
		return "object"
	}

	switch typ.Kind() {
	case reflect.Slice, reflect.Array:
		return "[]" + reflectTypeLabel(typ.Elem())
	case reflect.Pointer:
		return reflectTypeLabel(typ.Elem())
	default:
		if typ.Name() != "" {
			return typ.Name()
		}

		return typ.Kind().String()
	}
}
