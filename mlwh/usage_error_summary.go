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
)

const (
	// usageErrorGroupLimit caps UsageSummary.Errors.
	usageErrorGroupLimit = 50
	// usageErrorMessageLimit caps each group's Messages.
	usageErrorMessageLimit = 5
	// usageErrorExampleLimit caps each group's Examples.
	usageErrorExampleLimit = 10

	// usageErrorRows selects the window's error rows; its parameter is the
	// window start. The status term matches usage_requests_errors.
	usageErrorRows = "FROM usage_requests WHERE ts_ms > ? AND status >= 400"
	// usageErrorGroupColumns are the columns an error group is keyed by.
	usageErrorGroupColumns = "method, route, status, error_code"
)

// usageErrorGroupKey identifies a UsageErrorGroup; code is NULL for rows
// recorded before error details were stored.
type usageErrorGroupKey struct {
	method, route string
	status        int
	code          sql.NullString
}

func (q usageSummaryQuery) listErrorGroups(ctx context.Context) (usageErrorGroups, error) {
	groups := usageErrorGroups{list: []UsageErrorGroup{}, index: map[usageErrorGroupKey]int{}}

	rows, err := q.tx.QueryContext(ctx, "SELECT "+usageErrorGroupColumns+", COUNT(*), MIN(ts_ms), MAX(ts_ms) "+
		usageErrorRows+" GROUP BY "+usageErrorGroupColumns+" ORDER BY 5 DESC, "+usageErrorGroupColumns+
		" LIMIT ?", q.fromMS, usageErrorGroupLimit)
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

		if err = rows.Scan(&key.method, &key.route, &key.status, &key.code, &count, &first, &latest); err != nil {
			return groups, err
		}

		groups.index[key] = len(groups.list)
		groups.list = append(groups.list, newUsageErrorGroup(key, count, first, latest))
	}

	return groups, rows.Err()
}

func newUsageErrorGroup(key usageErrorGroupKey, count, first, latest int64) UsageErrorGroup {
	group := UsageErrorGroup{
		Method: key.method, Route: key.route, Status: key.status, Count: count,
		FirstSeen: usageFormatMS(first), LastSeen: usageFormatMS(latest),
		ErrorKinds: map[string]int64{}, Messages: []UsageErrorMessage{}, Examples: []UsageErrorExample{},
		ByCategory: map[string]int64{}, ByTool: map[string]int64{}, ByAgent: map[string]int64{},
	}

	if key.code.Valid {
		group.ErrorCode = &key.code.String
	}

	return group
}

// errorBreakdown counts each listed group's rows per non-empty value of
// column into the map field returns.
func (q usageSummaryQuery) errorBreakdown(ctx context.Context, groups usageErrorGroups, column string,
	field func(*UsageErrorGroup) map[string]int64,
) error {
	rows, err := q.tx.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s, COUNT(*) %s AND COALESCE(%s, '') <> '' "+
		"GROUP BY %s, %s", usageErrorGroupColumns, column, usageErrorRows, column, usageErrorGroupColumns, column),
		q.fromMS)
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

		if err = rows.Scan(&key.method, &key.route, &key.status, &key.code, &value, &count); err != nil {
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
WITH m AS (
    SELECT `+usageErrorGroupColumns+`, error_message AS v, COUNT(*) AS c `+usageErrorRows+`
    AND error_message <> '' GROUP BY `+usageErrorGroupColumns+`, error_message
), r AS (
    SELECT *, ROW_NUMBER() OVER (PARTITION BY `+usageErrorGroupColumns+` ORDER BY c DESC, v) AS rn FROM m
)
SELECT `+usageErrorGroupColumns+`, v, c FROM r WHERE rn <= ? ORDER BY `+usageErrorGroupColumns+`, rn`,
		q.fromMS, usageErrorMessageLimit)
	if err != nil {
		return err
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			key     usageErrorGroupKey
			message UsageErrorMessage
		)

		if err = rows.Scan(&key.method, &key.route, &key.status, &key.code, &message.Message,
			&message.Count); err != nil {
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
WITH r AS (
    SELECT `+usageErrorGroupColumns+`, ts_ms, path, COALESCE(query, '') AS query, category, mcp_tool,
        mcp_agent, client_id, username, COALESCE(error_message, '') AS message,
        COALESCE(error_kind, '') AS kind,
        ROW_NUMBER() OVER (PARTITION BY `+usageErrorGroupColumns+` ORDER BY ts_ms DESC, id DESC) AS rn
    `+usageErrorRows+`
)
SELECT `+usageErrorGroupColumns+`, ts_ms, path, query, category, mcp_tool, mcp_agent, client_id, username,
    message, kind
FROM r WHERE rn <= ? ORDER BY `+usageErrorGroupColumns+`, rn`, q.fromMS, usageErrorExampleLimit)
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

		if err = rows.Scan(&key.method, &key.route, &key.status, &key.code, &tsMS, &example.Path,
			&example.Query, &example.Category, &example.MCPTool, &example.MCPAgent, &example.ClientID,
			&example.Username, &example.Message, &example.ErrorKind); err != nil {
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
		if err = q.errorBreakdown(ctx, groups, breakdown.column, breakdown.field); err != nil {
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
