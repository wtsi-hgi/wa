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
	"slices"
	"strings"
)

const (
	mySQLPreferredTextCollation = "utf8mb4_0900_ai_ci"
	mySQLLegacyTextCollation    = "utf8mb4_general_ci"
	mySQLTextCollationToken     = "{{MYSQL_TEXT_COLLATION}}"

	// sampleSearchTokenTable is the word-token prefix index that backs the
	// sample substring search: one row per (lowercased word token, owning
	// id_sample_tmp), indexed on (token, id_sample_tmp). It is a normal
	// table+index in both dialects, declared in
	// cache_schema/{sqlite,mysql}/sample_search_token.sql and maintained by the
	// sample sync, replacing the former SQLite fts5 trigram virtual table and
	// MySQL ngram FULLTEXT index.
	sampleSearchTokenTable = "sample_search_token"
)

//go:embed cache_schema/sqlite/*.sql cache_schema/mysql/*.sql
var cacheSchemaFS embed.FS

var schemaStatementOrder = []string{
	"sample_mirror",
	"study_mirror",
	"library_samples",
	"donor_samples",
	"iseq_product_metrics_mirror",
	"seq_product_irods_locations_mirror",
	"sample_search_token",
	"sync_state",
	"schema_version",
	"sync_lock",
}

var cacheMigrationRecreateTables = []string{
	"donor_samples",
	"iseq_product_metrics_mirror",
	"library_samples",
	"sample_mirror",
	"sample_search_token",
	"seq_product_irods_locations_mirror",
	"study_mirror",
}

var cacheMigrationSyncStateTables = []string{
	"iseq_product_metrics",
	"seq_product_irods_locations",
	syncTableIseqFlowcell,
	syncTableSample,
	syncTableStudy,
}

var cacheMigrationDropTables = []string{
	"sample_mirror",
	"study_mirror",
	"library_samples",
	"donor_samples",
	"iseq_product_metrics_mirror",
	"seq_product_irods_locations_mirror",
	"negative_cache",
	"enrich_cache",
	"watermarks",
	"sync_lock",
	// sample_search_token (the word-token prefix index) is dropped so the
	// migration recreates it cleanly in both dialects; it is repopulated from
	// sample_mirror by the next sample sync.
	sampleSearchTokenTable,
}

func parseSchemaStatement(stmt string, shape *schemaShape) error {
	upper := strings.ToUpper(strings.TrimSpace(stmt))

	switch {
	case strings.HasPrefix(upper, "CREATE TABLE"):
		table, columns, unique, err := parseCreateTable(stmt)
		if err != nil {
			return err
		}

		shape.Tables[table] = columns
		if len(unique) > 0 {
			shape.Unique[table] = unique
		}
	case strings.HasPrefix(upper, "CREATE INDEX"):
		table, columns, err := parseCreateIndex(stmt)
		if err != nil {
			return err
		}

		shape.Index[table] = append(shape.Index[table], strings.Join(columns, ","))
	default:
		return fmt.Errorf("mlwh: unsupported schema statement %q", stmt)
	}

	return nil
}

func loadSchema(dialect string) ([]string, error) {
	return loadSchemaWithMySQLCollation(dialect, mySQLPreferredTextCollation)
}

func loadSchemaWithMySQLCollation(dialect, collation string) ([]string, error) {
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

		ddl := string(body)
		if dialect == "mysql" {
			ddl = strings.ReplaceAll(ddl, mySQLTextCollationToken, collation)
		}

		stmts = append(stmts, ddl)
	}

	return stmts, nil
}

// schemaShape is the dialect-agnostic, comparable representation of the cache
// schema. Tables/Index/Unique capture column types, secondary index column
// lists, and unique-constraint column tuples. The word-token prefix index that
// backs sample search (sample_search_token) is an ordinary table+index, so it
// is represented in Tables/Index like any other table and compares equal across
// dialects without special casing.
type schemaShape struct {
	Tables map[string]map[string]string
	Index  map[string][]string
	Unique map[string][]string
}

func parseSchemaShape(stmts []string) (schemaShape, error) {
	shape := schemaShape{
		Tables: make(map[string]map[string]string, len(stmts)),
		Index:  make(map[string][]string, len(stmts)),
		Unique: make(map[string][]string, len(stmts)),
	}

	for _, group := range stmts {
		for _, stmt := range splitSQLStatements(group) {
			if err := parseSchemaStatement(stmt, &shape); err != nil {
				return schemaShape{}, err
			}
		}
	}

	for table := range shape.Index {
		slices.Sort(shape.Index[table])
	}

	for table := range shape.Unique {
		slices.Sort(shape.Unique[table])
	}

	return shape, nil
}

// splitSQLStatements splits a DDL group into individual statements on top-level
// `;`. It strips `--` line comments and keeps `CREATE TRIGGER ... BEGIN ...
// END;` bodies whole: the internal statement separators between BEGIN and the
// matching END do not split the statement, because a trigger body contains its
// own `;`.
func splitSQLStatements(group string) []string {
	var (
		statements []string
		builder    strings.Builder
		word       strings.Builder
		depth      int
		blockDepth int
		quote      rune
		inComment  bool
	)

	flush := func() {
		stmt := strings.TrimSpace(builder.String())
		if stmt != "" {
			statements = append(statements, stmt)
		}

		builder.Reset()
	}

	// finishWord consumes the identifier accumulated in word and adjusts the
	// BEGIN/END block depth so `;` inside a trigger body does not flush.
	finishWord := func() {
		switch {
		case strings.EqualFold(word.String(), "BEGIN"):
			blockDepth++
		case strings.EqualFold(word.String(), "END") && blockDepth > 0:
			blockDepth--
		}

		word.Reset()
	}

	runes := []rune(group)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case inComment:
			if r == '\n' {
				inComment = false
				builder.WriteRune(r)
			}
		case quote != 0:
			builder.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '-' && i+1 < len(runes) && runes[i+1] == '-':
			finishWord()
			inComment = true
			i++
		case r == '\'' || r == '"' || r == '`':
			finishWord()
			quote = r
			builder.WriteRune(r)
		case r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			word.WriteRune(r)
			builder.WriteRune(r)
		case r == '(':
			finishWord()
			depth++
			builder.WriteRune(r)
		case r == ')':
			finishWord()
			if depth > 0 {
				depth--
			}
			builder.WriteRune(r)
		case r == ';' && depth == 0 && blockDepth == 0:
			finishWord()
			flush()
		default:
			finishWord()
			builder.WriteRune(r)
		}
	}

	finishWord()
	flush()

	return statements
}

func parseCreateTable(stmt string) (string, map[string]string, []string, error) {
	bodyStart := strings.Index(stmt, "(")
	bodyEnd := strings.LastIndex(stmt, ")")
	if bodyStart == -1 || bodyEnd == -1 || bodyEnd <= bodyStart {
		return "", nil, nil, fmt.Errorf("mlwh: malformed create table %q", stmt)
	}

	header := strings.Fields(stmt[:bodyStart])
	if len(header) < 3 {
		return "", nil, nil, fmt.Errorf("mlwh: malformed create table header %q", stmt)
	}

	table := trimIdentifier(header[len(header)-1])
	columns := make(map[string]string)
	unique := make([]string, 0, 1)

	for _, part := range splitTopLevel(body(stmt, bodyStart, bodyEnd), ',') {
		fields := strings.Fields(part)
		if len(fields) < 2 {
			continue
		}

		if isTableConstraint(fields) {
			tuples, ok, err := parseUniqueConstraint(part)
			if err != nil {
				return "", nil, nil, err
			}

			if ok {
				unique = append(unique, strings.Join(tuples, ","))
			}

			continue
		}

		columns[trimIdentifier(fields[0])] = normaliseTypeFamily(fields[1])
	}

	return table, columns, unique, nil
}

func parseUniqueConstraint(part string) ([]string, bool, error) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(part), " "))
	if !strings.HasPrefix(normalized, "UNIQUE") && !strings.Contains(normalized, " UNIQUE") {
		return nil, false, nil
	}

	bodyStart := strings.Index(part, "(")
	bodyEnd := strings.LastIndex(part, ")")
	if bodyStart == -1 || bodyEnd == -1 || bodyEnd <= bodyStart {
		return nil, false, fmt.Errorf("mlwh: malformed unique constraint %q", part)
	}

	parts := splitTopLevel(body(part, bodyStart, bodyEnd), ',')
	columns := make([]string, 0, len(parts))

	for _, column := range parts {
		fields := strings.Fields(column)
		if len(fields) == 0 {
			continue
		}

		columns = append(columns, trimIdentifier(fields[0]))
	}

	return columns, true, nil
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

func isTableConstraint(fields []string) bool {
	keyword := strings.ToUpper(fields[0])

	switch {
	case strings.HasPrefix(keyword, "PRIMARY"):
		return true
	case strings.HasPrefix(keyword, "UNIQUE"):
		return true
	case strings.HasPrefix(keyword, "FOREIGN"):
		return true
	case strings.HasPrefix(keyword, "CHECK"):
		return true
	case keyword != "CONSTRAINT":
		return false
	}

	if len(fields) < 3 {
		return true
	}

	constraintType := strings.ToUpper(fields[2])

	return strings.HasPrefix(constraintType, "PRIMARY") ||
		strings.HasPrefix(constraintType, "UNIQUE") ||
		strings.HasPrefix(constraintType, "FOREIGN") ||
		strings.HasPrefix(constraintType, "CHECK")
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
