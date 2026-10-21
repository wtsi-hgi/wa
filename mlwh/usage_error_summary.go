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
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// UsageErrorSeverity says whether an error needs attention.
type UsageErrorSeverity string

// The usage error severities, most urgent first.
const (
	// UsageErrorSeverityFault is a server failure (5xx), or a request wa's own
	// CLI, web frontend, or internal servers got wrong: wa needs fixing.
	UsageErrorSeverityFault UsageErrorSeverity = "fault"
	// UsageErrorSeverityCaller is a request an MCP agent or third-party
	// client got wrong.
	UsageErrorSeverityCaller UsageErrorSeverity = "caller"
	// UsageErrorSeverityExpected is a lookup's answer: the identifier is not
	// known (not_found) or names several records (ambiguous).
	UsageErrorSeverityExpected UsageErrorSeverity = "expected"
)

// usageOwnClientCategories are the categories of wa's own clients, whose
// malformed requests are wa bugs. MCP is not one: its tools pass agents'
// arguments through, so its rejected requests are usually the agent's.
var usageOwnClientCategories = []UsageCategory{
	UsageCategoryCLI, UsageCategoryWeb, UsageCategoryResultsServer, UsageCategoryMLWHDiffServer,
}

// usageErrorSeveritySQL is the UsageErrorSeverity of an error row. Rows
// recorded before error details (NULL code) are judged by status alone: on a
// matched route only not_found answers 404 and only ambiguous answers 409.
var usageErrorSeveritySQL = fmt.Sprintf(`CASE
    WHEN status >= 500 THEN '%s'
    WHEN error_code IN ('%s', '%s') OR (error_code IS NULL AND route <> '' AND status IN (404, 409)) THEN '%s'
    WHEN category IN ('%s') THEN '%s'
    ELSE '%s' END`,
	UsageErrorSeverityFault, httpErrorCodeNotFound, httpErrorCodeAmbiguous, UsageErrorSeverityExpected,
	strings.Join(usageCategoryStrings(usageOwnClientCategories), "', '"), UsageErrorSeverityFault,
	UsageErrorSeverityCaller)

func usageCategoryStrings(categories []UsageCategory) []string {
	values := make([]string, len(categories))
	for i, category := range categories {
		values[i] = string(category)
	}

	return values
}

const (
	// usageErrorGroupLimit caps UsageSummary.Errors.
	usageErrorGroupLimit = 50
	// usageErrorMessageLimit caps each group's Messages.
	usageErrorMessageLimit = 5
	// usageErrorExampleLimit caps each group's Examples.
	usageErrorExampleLimit = 10

	// usageErrorGroupColumns are the columns of e an error group is keyed by.
	usageErrorGroupColumns = "method, route, status, error_code, severity, probe"
	// usageErrorGroupRank orders error groups so the group cap drops expected
	// and legacy groups before faults: detailed before legacy, then by
	// severity, then by count.
	usageErrorGroupRank = "error_code IS NULL, CASE severity WHEN '" + string(UsageErrorSeverityFault) +
		"' THEN 0 WHEN '" + string(UsageErrorSeverityCaller) + "' THEN 1 ELSE 2 END, COUNT(*) DESC"
)

// errorRowsCTE defines e, the window's error rows that match the filter,
// with their severity and whether they were probes; its parameters are
// q.whereArgs. The status term matches usage_requests_errors.
func (q usageSummaryQuery) errorRowsCTE() string {
	return "e AS (SELECT *, " + usageErrorSeveritySQL + " AS severity, lookup IS NOT NULL AS probe " +
		"FROM usage_requests WHERE " + q.where + " AND status >= 400)"
}

// usageErrorBreakdownSQL is the UsageErrorBreakdown field of a row of e,
// probe or its severity, suffixed with usageLegacySuffix for a row recorded
// before error details.
const usageErrorBreakdownSQL = "CASE WHEN probe AND severity = '" + string(UsageErrorSeverityExpected) +
	"' THEN 'probe' ELSE severity END || CASE WHEN error_code IS NULL THEN '" + usageLegacySuffix +
	"' ELSE '' END"

const usageLegacySuffix = ":legacy"

// usageErrorGroupKey identifies a UsageErrorGroup; code is NULL for rows
// recorded before error details were stored.
type usageErrorGroupKey struct {
	method, route string
	status        int
	code          sql.NullString
	severity      UsageErrorSeverity
	probe         bool
}

// scanDest returns the Scan destinations of the usageErrorGroupColumns.
func (k *usageErrorGroupKey) scanDest() []any {
	return []any{&k.method, &k.route, &k.status, &k.code, &k.severity, &k.probe}
}

func (q usageSummaryQuery) listErrorGroups(ctx context.Context) (usageErrorGroups, error) {
	groups := usageErrorGroups{list: []UsageErrorGroup{}, index: map[usageErrorGroupKey]int{}}

	rows, err := q.tx.QueryContext(ctx, "WITH "+q.errorRowsCTE()+" SELECT "+usageErrorGroupColumns+
		", COUNT(*), MIN(ts_ms), MAX(ts_ms) FROM e GROUP BY "+usageErrorGroupColumns+" ORDER BY "+
		usageErrorGroupRank+", "+usageErrorGroupColumns+" LIMIT ?", q.args(usageErrorGroupLimit)...)
	if err != nil {
		return groups, err
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			key           usageErrorGroupKey
			count         int64
			first, latest int64
		)

		if err = rows.Scan(append(key.scanDest(), &count, &first, &latest)...); err != nil {
			return groups, err
		}

		groups.index[key] = len(groups.list)
		groups.list = append(groups.list, newUsageErrorGroup(key, count, first, latest))
	}

	return groups, rows.Err()
}

func newUsageErrorGroup(key usageErrorGroupKey, count, first, latest int64) UsageErrorGroup {
	group := UsageErrorGroup{
		Method: key.method, Route: key.route, Status: key.status, Severity: key.severity, Probe: key.probe,
		Count: count, FirstSeen: usageFormatMS(first), LastSeen: usageFormatMS(latest),
		ErrorKinds: map[string]int64{}, Messages: []UsageErrorMessage{}, Examples: []UsageErrorExample{},
		ByCategory: map[string]int64{}, ByTool: map[string]int64{}, ByAgent: map[string]int64{},
	}

	if key.code.Valid {
		group.ErrorCode = &key.code.String
	}

	return group
}

// usageErrorBreakdowns returns the UsageErrorBreakdown of q's error rows
// matching filter per value of the key expression, whose parameters are
// keyArgs. filter takes no parameters.
func usageErrorBreakdowns[K comparable](ctx context.Context, q usageSummaryQuery, key, filter string,
	keyArgs ...any,
) (map[K]UsageErrorBreakdown, error) {
	counts, err := usageGroupCounts[K](ctx, q.tx, "WITH "+q.errorRowsCTE()+" SELECT "+key+", "+
		usageErrorBreakdownSQL+", COUNT(*) FROM e WHERE "+filter+" GROUP BY 1, 2", q.args(keyArgs...)...)
	if err != nil {
		return nil, err
	}

	breakdowns := make(map[K]UsageErrorBreakdown, len(counts))

	for key, byField := range counts {
		legacy := func(severity UsageErrorSeverity) int64 { return byField[string(severity)+usageLegacySuffix] }
		detailed := func(severity UsageErrorSeverity) int64 { return byField[string(severity)] + legacy(severity) }

		breakdowns[key] = UsageErrorBreakdown{
			Fault: detailed(UsageErrorSeverityFault), Caller: detailed(UsageErrorSeverityCaller),
			Expected: detailed(UsageErrorSeverityExpected), Probe: byField["probe"],
			Legacy: UsageLegacyErrors{
				Fault: legacy(UsageErrorSeverityFault), Caller: legacy(UsageErrorSeverityCaller),
				Expected: legacy(UsageErrorSeverityExpected),
			},
		}
	}

	return breakdowns, nil
}

// groupBreakdown counts each listed group's rows per non-empty value of
// column into the map field returns.
func (q usageSummaryQuery) groupBreakdown(ctx context.Context, groups usageErrorGroups, column string,
	field func(*UsageErrorGroup) map[string]int64,
) error {
	rows, err := q.tx.QueryContext(ctx, fmt.Sprintf("WITH %s SELECT %s, %s, COUNT(*) FROM e "+
		"WHERE COALESCE(%s, '') <> '' GROUP BY %s, %s", q.errorRowsCTE(), usageErrorGroupColumns, column, column,
		usageErrorGroupColumns, column), q.args()...)
	if err != nil {
		return err
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			key   usageErrorGroupKey
			value string
			count int64
		)

		if err = rows.Scan(append(key.scanDest(), &value, &count)...); err != nil {
			return err
		}

		if group, ok := groups.get(key); ok {
			field(group)[value] = count
		}
	}

	return rows.Err()
}

// errorMessages sets each listed group's top usageErrorMessageLimit
// non-empty messages, by count descending then message.
func (q usageSummaryQuery) errorMessages(ctx context.Context, groups usageErrorGroups) error {
	rows, err := q.tx.QueryContext(ctx, `
WITH `+q.errorRowsCTE()+`, m AS (
    SELECT `+usageErrorGroupColumns+`, error_message AS v, COUNT(*) AS c FROM e
    WHERE error_message <> '' GROUP BY `+usageErrorGroupColumns+`, error_message
), r AS (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY `+usageErrorGroupColumns+` ORDER BY c DESC, v) AS rn FROM m
)
SELECT `+usageErrorGroupColumns+`, v, c FROM r WHERE rn <= ? ORDER BY `+usageErrorGroupColumns+`, rn`,
		q.args(usageErrorMessageLimit)...)
	if err != nil {
		return err
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			key     usageErrorGroupKey
			message UsageErrorMessage
		)

		if err = rows.Scan(append(key.scanDest(), &message.Message, &message.Count)...); err != nil {
			return err
		}

		if group, ok := groups.get(key); ok {
			group.Messages = append(group.Messages, message)
		}
	}

	return rows.Err()
}

// errorExamples sets each listed group's latest usageErrorExampleLimit rows,
// newest first.
func (q usageSummaryQuery) errorExamples(ctx context.Context, groups usageErrorGroups) error {
	rows, err := q.tx.QueryContext(ctx, `
WITH `+q.errorRowsCTE()+`, r AS (
    SELECT `+usageErrorGroupColumns+`, ts_ms, path, COALESCE(query, '') AS query, category, mcp_tool,
        mcp_agent, client_id, username, COALESCE(error_message, '') AS message,
        COALESCE(error_kind, '') AS kind,
        ROW_NUMBER() OVER (PARTITION BY `+usageErrorGroupColumns+` ORDER BY ts_ms DESC, id DESC) AS rn
    FROM e
)
SELECT `+usageErrorGroupColumns+`, ts_ms, path, query, category, mcp_tool, mcp_agent, client_id, username,
    message, kind
FROM r WHERE rn <= ? ORDER BY `+usageErrorGroupColumns+`, rn`, q.args(usageErrorExampleLimit)...)
	if err != nil {
		return err
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			key     usageErrorGroupKey
			tsMS    int64
			example UsageErrorExample
		)

		if err = rows.Scan(append(key.scanDest(), &tsMS, &example.Path, &example.Query, &example.Category,
			&example.MCPTool, &example.MCPAgent, &example.ClientID, &example.Username, &example.Message,
			&example.ErrorKind)...); err != nil {
			return err
		}

		if group, ok := groups.get(key); ok {
			example.Time = usageFormatMS(tsMS)
			group.Examples = append(group.Examples, example)
		}
	}

	return rows.Err()
}

// usageErrorGroups indexes the listed groups by key.
type usageErrorGroups struct {
	list  []UsageErrorGroup
	index map[usageErrorGroupKey]int
}

func (g usageErrorGroups) get(key usageErrorGroupKey) (*UsageErrorGroup, bool) {
	i, ok := g.index[key]
	if !ok {
		return nil, false
	}

	return &g.list[i], true
}

// errorGroups returns the window's top usageErrorGroupLimit error groups by
// count, then key, each with its kind, category, tool, and agent counts, top
// messages, and latest examples.
func (q usageSummaryQuery) errorGroups(ctx context.Context) ([]UsageErrorGroup, error) {
	groups, err := q.listErrorGroups(ctx)
	if err != nil {
		return nil, err
	}

	breakdowns := []struct {
		column string
		field  func(*UsageErrorGroup) map[string]int64
	}{
		{"error_kind", func(g *UsageErrorGroup) map[string]int64 { return g.ErrorKinds }},
		{"category", func(g *UsageErrorGroup) map[string]int64 { return g.ByCategory }},
		{"mcp_tool", func(g *UsageErrorGroup) map[string]int64 { return g.ByTool }},
		{"mcp_agent", func(g *UsageErrorGroup) map[string]int64 { return g.ByAgent }},
	}

	for _, breakdown := range breakdowns {
		if err = q.groupBreakdown(ctx, groups, breakdown.column, breakdown.field); err != nil {
			return nil, err
		}
	}

	if err = q.errorMessages(ctx, groups); err != nil {
		return nil, err
	}

	if err = q.errorExamples(ctx, groups); err != nil {
		return nil, err
	}

	return groups.list, nil
}
