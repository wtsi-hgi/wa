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

	// sampleSearchIndexSchemaName is the basename of the per-dialect DDL that
	// declares the sample_mirror full-text search index (the SQLite fts5
	// virtual table and the MySQL ngram FULLTEXT index). It is applied after
	// the base schema rather than as one of schemaStatementOrder's tables.
	sampleSearchIndexSchemaName = "sample_search"
	// sampleSearchTable is the SQLite fts5 external-content virtual table that
	// mirrors sample_mirror for substring search.
	sampleSearchTable = "sample_search"
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
	"sync_state",
	"schema_version",
	"sync_lock",
}

var cacheMigrationRecreateTables = []string{
	"donor_samples",
	"iseq_product_metrics_mirror",
	"library_samples",
	"sample_mirror",
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
	// sample_search (the SQLite fts5 external-content table) is dropped first
	// so its content table sample_mirror can be recreated cleanly. On MySQL the
	// search index lives inside sample_mirror, so this drop is a harmless no-op.
	sampleSearchTable,
}

func parseSchemaStatement(stmt string, shape *schemaShape) error {
	upper := strings.ToUpper(strings.TrimSpace(stmt))

	switch {
	case strings.HasPrefix(upper, "CREATE VIRTUAL TABLE"):
		return parseSchemaVirtualTable(stmt, upper, shape)
	case strings.HasPrefix(upper, "CREATE TABLE"):
		table, columns, unique, fulltext, err := parseCreateTable(stmt)
		if err != nil {
			return err
		}

		shape.Tables[table] = columns
		if len(unique) > 0 {
			shape.Unique[table] = unique
		}
		if len(fulltext) > 0 {
			shape.FullText[table] = normaliseFullTextColumns(fulltext)
		}
	case strings.HasPrefix(upper, "CREATE INDEX"):
		table, columns, err := parseCreateIndex(stmt)
		if err != nil {
			return err
		}

		shape.Index[table] = append(shape.Index[table], strings.Join(columns, ","))
	case strings.HasPrefix(upper, "CREATE FULLTEXT INDEX"):
		table, columns, _, err := parseMySQLFulltextIndex(stmt)
		if err != nil {
			return err
		}

		shape.FullText[table] = normaliseFullTextColumns(columns)
	default:
		return fmt.Errorf("mlwh: unsupported schema statement %q", stmt)
	}

	return nil
}

// isInlineFulltextConstraint reports whether a CREATE TABLE body part is an
// inline full-text index declaration (`FULLTEXT (...)`, optionally `KEY`/`INDEX`
// and `WITH PARSER ...`), so it is recorded as a search index rather than a
// column or a unique constraint.
func isInlineFulltextConstraint(fields []string) bool {
	return strings.EqualFold(fields[0], "FULLTEXT")
}

func parseInlineFulltextConstraint(part string) ([]string, error) {
	bodyStart := strings.Index(part, "(")
	bodyEnd := strings.LastIndex(part, ")")
	if bodyStart == -1 || bodyEnd == -1 || bodyEnd <= bodyStart {
		return nil, fmt.Errorf("mlwh: malformed inline fulltext index %q", part)
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

	return columns, nil
}

// normaliseFullTextColumns returns the search columns as a deduplicated, sorted
// set so the representation is order-insensitive (matching equal across
// dialects) yet sensitive to the membership of the search column set.
func normaliseFullTextColumns(columns []string) []string {
	normalised := make([]string, 0, len(columns))
	for _, column := range columns {
		trimmed := trimIdentifier(column)
		if trimmed != "" {
			normalised = append(normalised, trimmed)
		}
	}

	slices.Sort(normalised)

	return slices.Compact(normalised)
}

// parseMySQLFulltextIndex parses a single
// `CREATE FULLTEXT INDEX <name> ON <table> (<columns>) WITH PARSER <parser>`
// statement, returning the target table, its indexed columns in declaration
// order, and the parser name. It reports an error if the statement is not a
// FULLTEXT index declaration of that shape.
func parseMySQLFulltextIndex(stmt string) (string, []string, string, error) {
	normalized := strings.Join(strings.Fields(stmt), " ")
	upper := strings.ToUpper(normalized)
	if !strings.HasPrefix(upper, "CREATE FULLTEXT INDEX") || !strings.Contains(upper, " WITH PARSER ") {
		return "", nil, "", fmt.Errorf("mlwh: not a fulltext index statement %q", stmt)
	}

	bodyStart := strings.Index(normalized, "(")
	bodyEnd := strings.LastIndex(normalized, ")")
	onIndex := strings.Index(upper, " ON ")
	if bodyStart == -1 || bodyEnd == -1 || onIndex == -1 || bodyEnd <= bodyStart {
		return "", nil, "", fmt.Errorf("mlwh: malformed fulltext index %q", stmt)
	}

	header := strings.Fields(normalized[onIndex+4 : bodyStart])
	if len(header) != 1 {
		return "", nil, "", fmt.Errorf("mlwh: malformed fulltext index target %q", stmt)
	}

	table := trimIdentifier(header[0])
	columns := make([]string, 0, 4)
	for _, part := range splitTopLevel(body(normalized, bodyStart, bodyEnd), ',') {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}

		columns = append(columns, trimIdentifier(fields[0]))
	}

	parserIndex := strings.Index(upper, " WITH PARSER ")
	parser := trimIdentifier(strings.Fields(normalized[parserIndex+len(" WITH PARSER "):])[0])

	return table, columns, parser, nil
}

// parseSchemaVirtualTable records a SQLite `CREATE VIRTUAL TABLE ... USING
// fts5(...)` declaration as a full-text search index over its searchable
// columns on the content table it mirrors. The fts5 column list is the leading
// run of bare column names before any `option=value` configuration (such as
// content=, content_rowid=, tokenize=), and the mirrored table is taken from
// the content= option so the entry compares equal to the MySQL FULLTEXT index
// on the same base table.
func parseSchemaVirtualTable(stmt, upper string, shape *schemaShape) error {
	if !strings.Contains(upper, "USING FTS5") {
		return fmt.Errorf("mlwh: unsupported virtual table %q", stmt)
	}

	bodyStart := strings.Index(stmt, "(")
	bodyEnd := strings.LastIndex(stmt, ")")
	if bodyStart == -1 || bodyEnd == -1 || bodyEnd <= bodyStart {
		return fmt.Errorf("mlwh: malformed virtual table %q", stmt)
	}

	var (
		columns       []string
		contentTable  string
		contentRowKey = "content"
	)

	for _, part := range splitTopLevel(body(stmt, bodyStart, bodyEnd), ',') {
		key, value, isOption := strings.Cut(part, "=")
		if !isOption {
			fields := strings.Fields(part)
			if len(fields) == 0 {
				continue
			}

			columns = append(columns, trimIdentifier(fields[0]))

			continue
		}

		if strings.EqualFold(strings.TrimSpace(key), contentRowKey) {
			contentTable = strings.Trim(strings.TrimSpace(value), "`'\" \t")
		}
	}

	if contentTable == "" {
		return fmt.Errorf("mlwh: fts5 virtual table without content table %q", stmt)
	}

	shape.FullText[contentTable] = normaliseFullTextColumns(columns)

	return nil
}

// loadFullSchema returns the base table statements plus the sample_mirror
// full-text search index DDL for the dialect, so the parsed shape represents the
// complete cache schema including search support. The base schema and the search
// index are declared in separate files (the index is applied after the tables
// exist), but the parity model must see both.
func loadFullSchema(dialect string) ([]string, error) {
	stmts, err := loadSchema(dialect)
	if err != nil {
		return nil, err
	}

	searchDDL, err := loadSearchIndexSchema(dialect)
	if err != nil {
		return nil, err
	}

	return append(stmts, searchDDL), nil
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
// lists, and unique-constraint column tuples. FullText captures the
// sample_mirror full-text search index (the SQLite fts5 virtual table and the
// MySQL ngram FULLTEXT index) as a normalised, sorted search column set per
// table, so the two dialects compare equal on search support yet diverge if the
// searchable column set differs.
type schemaShape struct {
	Tables   map[string]map[string]string
	Index    map[string][]string
	Unique   map[string][]string
	FullText map[string][]string
}

func parseSchemaShape(stmts []string) (schemaShape, error) {
	shape := schemaShape{
		Tables:   make(map[string]map[string]string, len(stmts)),
		Index:    make(map[string][]string, len(stmts)),
		Unique:   make(map[string][]string, len(stmts)),
		FullText: make(map[string][]string, len(stmts)),
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

// loadSearchIndexSchema returns the per-dialect DDL that declares the
// sample_mirror full-text search index. For SQLite this is the fts5
// external-content virtual table; for MySQL it is the ngram FULLTEXT index.
// It is kept out of schemaStatementOrder (and thus the parity-shape parser)
// and applied after the base tables exist.
func loadSearchIndexSchema(dialect string) (string, error) {
	if dialect != "sqlite" && dialect != "mysql" {
		return "", fmt.Errorf("mlwh: unsupported schema dialect %q", dialect)
	}

	path := fmt.Sprintf("cache_schema/%s/%s.sql", dialect, sampleSearchIndexSchemaName)

	body, err := cacheSchemaFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("mlwh: load schema %s: %w", path, err)
	}

	return string(body), nil
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

func parseCreateTable(stmt string) (string, map[string]string, []string, []string, error) {
	bodyStart := strings.Index(stmt, "(")
	bodyEnd := strings.LastIndex(stmt, ")")
	if bodyStart == -1 || bodyEnd == -1 || bodyEnd <= bodyStart {
		return "", nil, nil, nil, fmt.Errorf("mlwh: malformed create table %q", stmt)
	}

	header := strings.Fields(stmt[:bodyStart])
	if len(header) < 3 {
		return "", nil, nil, nil, fmt.Errorf("mlwh: malformed create table header %q", stmt)
	}

	table := trimIdentifier(header[len(header)-1])
	columns := make(map[string]string)
	unique := make([]string, 0, 1)

	var fulltext []string

	for _, part := range splitTopLevel(body(stmt, bodyStart, bodyEnd), ',') {
		fields := strings.Fields(part)
		if len(fields) < 2 {
			continue
		}

		if isInlineFulltextConstraint(fields) {
			searchColumns, err := parseInlineFulltextConstraint(part)
			if err != nil {
				return "", nil, nil, nil, err
			}

			fulltext = append(fulltext, searchColumns...)

			continue
		}

		if isTableConstraint(fields) {
			tuples, ok, err := parseUniqueConstraint(part)
			if err != nil {
				return "", nil, nil, nil, err
			}

			if ok {
				unique = append(unique, strings.Join(tuples, ","))
			}

			continue
		}

		columns[trimIdentifier(fields[0])] = normaliseTypeFamily(fields[1])
	}

	return table, columns, unique, fulltext, nil
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
