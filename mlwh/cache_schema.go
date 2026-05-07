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
	"embed"
	"fmt"
	"strings"
)

//go:embed cache_schema/sqlite/*.sql cache_schema/mysql/*.sql
var cacheSchemaFS embed.FS

var schemaStatementOrder = []string{
	"study_mirror",
	"sample_mirror",
	"library_samples",
	"donor_samples",
	"negative_cache",
	"watermarks",
	"enrich_cache",
	"sync_state",
	"schema_version",
}

func loadSchema(dialect string) ([]string, error) {
	if dialect != "sqlite" && dialect != "mysql" {
		return nil, fmt.Errorf("mlwh: unsupported schema dialect %q", dialect)
	}

	stmts := make([]string, 0, len(schemaStatementOrder))

	for _, name := range schemaStatementOrder {
		path := fmt.Sprintf("cache_schema/%s/%s.sql", dialect, name)

		body, err := cacheSchemaFS.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("mlwh: load schema %s: %w", path, err)
		}

		stmts = append(stmts, string(body))
	}

	return stmts, nil
}

type schemaShape struct {
	Tables map[string]map[string]string
	Index  map[string][]string
}

func parseSchemaShape(stmts []string) (schemaShape, error) {
	shape := schemaShape{
		Tables: make(map[string]map[string]string, len(stmts)),
		Index:  make(map[string][]string, len(stmts)),
	}

	for _, group := range stmts {
		for _, stmt := range splitSQLStatements(group) {
			upper := strings.ToUpper(stmt)

			switch {
			case strings.HasPrefix(upper, "CREATE TABLE"):
				table, columns, err := parseCreateTable(stmt)
				if err != nil {
					return schemaShape{}, err
				}

				shape.Tables[table] = columns
			case strings.HasPrefix(upper, "CREATE INDEX"):
				table, columns, err := parseCreateIndex(stmt)
				if err != nil {
					return schemaShape{}, err
				}

				shape.Index[table] = append(shape.Index[table], strings.Join(columns, ","))
			default:
				return schemaShape{}, fmt.Errorf("mlwh: unsupported schema statement %q", stmt)
			}
		}
	}

	for table := range shape.Index {
		slices := shape.Index[table]
		if len(slices) < 2 {
			continue
		}

		for i := 0; i < len(slices)-1; i++ {
			for j := i + 1; j < len(slices); j++ {
				if slices[j] < slices[i] {
					slices[i], slices[j] = slices[j], slices[i]
				}
			}
		}

		shape.Index[table] = slices
	}

	return shape, nil
}

func splitSQLStatements(group string) []string {
	var (
		statements []string
		builder    strings.Builder
		depth      int
		quote      rune
	)

	flush := func() {
		stmt := strings.TrimSpace(builder.String())
		if stmt != "" {
			statements = append(statements, stmt)
		}

		builder.Reset()
	}

	for _, r := range group {
		switch {
		case quote != 0:
			builder.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
			builder.WriteRune(r)
		case r == '(':
			depth++
			builder.WriteRune(r)
		case r == ')':
			if depth > 0 {
				depth--
			}
			builder.WriteRune(r)
		case r == ';' && depth == 0:
			flush()
		default:
			builder.WriteRune(r)
		}
	}

	flush()

	return statements
}

func parseCreateTable(stmt string) (string, map[string]string, error) {
	bodyStart := strings.Index(stmt, "(")
	bodyEnd := strings.LastIndex(stmt, ")")
	if bodyStart == -1 || bodyEnd == -1 || bodyEnd <= bodyStart {
		return "", nil, fmt.Errorf("mlwh: malformed create table %q", stmt)
	}

	header := strings.Fields(stmt[:bodyStart])
	if len(header) < 3 {
		return "", nil, fmt.Errorf("mlwh: malformed create table header %q", stmt)
	}

	table := trimIdentifier(header[len(header)-1])
	columns := make(map[string]string)

	for _, part := range splitTopLevel(body(stmt, bodyStart, bodyEnd), ',') {
		fields := strings.Fields(part)
		if len(fields) < 2 {
			continue
		}

		keyword := strings.ToUpper(fields[0])
		if keyword == "PRIMARY" || keyword == "UNIQUE" || keyword == "CONSTRAINT" || keyword == "FOREIGN" || keyword == "CHECK" {
			continue
		}

		columns[trimIdentifier(fields[0])] = normaliseTypeFamily(fields[1])
	}

	return table, columns, nil
}

func parseCreateIndex(stmt string) (string, []string, error) {
	normalized := strings.Join(strings.Fields(stmt), " ")
	bodyStart := strings.Index(normalized, "(")
	bodyEnd := strings.LastIndex(normalized, ")")
	onIndex := strings.Index(strings.ToUpper(normalized), " ON ")
	if bodyStart == -1 || bodyEnd == -1 || onIndex == -1 || bodyEnd <= bodyStart {
		return "", nil, fmt.Errorf("mlwh: malformed create index %q", stmt)
	}

	header := strings.Fields(normalized[onIndex+4 : bodyStart])
	if len(header) != 1 {
		return "", nil, fmt.Errorf("mlwh: malformed create index target %q", stmt)
	}

	table := trimIdentifier(header[0])
	parts := splitTopLevel(body(normalized, bodyStart, bodyEnd), ',')
	columns := make([]string, 0, len(parts))

	for _, part := range parts {
		columns = append(columns, trimIdentifier(strings.Fields(part)[0]))
	}

	return table, columns, nil
}

func trimIdentifier(value string) string {
	return strings.Trim(value, "` \t\n\r")
}

func splitTopLevel(input string, separator rune) []string {
	var (
		parts   []string
		builder strings.Builder
		depth   int
		quote   rune
	)

	flush := func() {
		part := strings.TrimSpace(builder.String())
		if part != "" {
			parts = append(parts, part)
		}

		builder.Reset()
	}

	for _, r := range input {
		switch {
		case quote != 0:
			builder.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
			builder.WriteRune(r)
		case r == '(':
			depth++
			builder.WriteRune(r)
		case r == ')':
			if depth > 0 {
				depth--
			}
			builder.WriteRune(r)
		case r == separator && depth == 0:
			flush()
		default:
			builder.WriteRune(r)
		}
	}

	flush()

	return parts
}

func body(stmt string, start, end int) string {
	return stmt[start+1 : end]
}

func normaliseTypeFamily(raw string) string {
	typeName := strings.ToUpper(raw)
	if cut := strings.IndexRune(typeName, '('); cut >= 0 {
		typeName = typeName[:cut]
	}

	switch {
	case strings.Contains(typeName, "INT") || typeName == "BOOL" || typeName == "BOOLEAN":
		return "integer"
	case strings.Contains(typeName, "CHAR") || strings.Contains(typeName, "TEXT") || strings.Contains(typeName, "CLOB") || strings.Contains(typeName, "VARCHAR"):
		return "text"
	case strings.Contains(typeName, "BLOB") || strings.Contains(typeName, "BINARY"):
		return "blob"
	default:
		return strings.ToLower(typeName)
	}
}
