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
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

const (
	// usageTopLimit caps the ranked actor, tool, agent, User-Agent, and
	// unmatched lists of a UsageSummary.
	usageTopLimit = 25
	// usageNoLimit is SQLite's LIMIT value for an uncapped list.
	usageNoLimit = -1

	usageDayLayout = "2006-01-02"
	usageMSPerDay  = int64(24 * time.Hour / time.Millisecond)

	usageOtherFilter     = "category = '" + string(UsageCategoryOther) + "'"
	usageUnmatchedFilter = "route = ''"
)

// usageUnmatchedKeys group unmatched rows by method and path.
var usageUnmatchedKeys = []string{"method", "path"}

// ErrUsageWindowInvalid is wrapped by Summary errors for an unknown window.
var ErrUsageWindowInvalid = errors.New("mlwh: invalid usage window")

const usageAggregateColumns = `COUNT(*), COALESCE(SUM(status >= 400), 0),
    COUNT(DISTINCT NULLIF(client_id, '')),
    COUNT(DISTINCT CASE WHEN client_id = '' THEN remote_addr END),
    COUNT(DISTINCT NULLIF(username, ''))`

// usageLatencyAtRankSQL selects one route's window latency at a 0-based
// rank. Walking the (method, route, latency_us, ts_ms) index in latency order
// finds the rank without sorting the route's rows. Its parameters are the
// method, route, window start, and rank.
const usageLatencyAtRankSQL = `SELECT latency_us FROM usage_requests
INDEXED BY usage_requests_route_latency
WHERE method = ? AND route = ? AND ts_ms > ? ORDER BY latency_us LIMIT 1 OFFSET ?`

// UsageTotals are the window's request, error, and distinct-caller counts.
type UsageTotals struct {
	Requests  int64   `json:"requests"`
	Errors    int64   `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
	Sessions  int64   `json:"sessions"`
	IPClients int64   `json:"ip_clients"`
	Users     int64   `json:"users"`
}

// UsageCategoryStats are the window's counts for one caller category.
type UsageCategoryStats struct {
	Category                 UsageCategory `json:"category"`
	Requests                 int64         `json:"requests"`
	Errors                   int64         `json:"errors"`
	ErrorRate                float64       `json:"error_rate"`
	Sessions                 int64         `json:"sessions"`
	IPClients                int64         `json:"ip_clients"`
	Users                    int64         `json:"users"`
	RequestsPerSessionMedian int64         `json:"requests_per_session_median"`
	RequestsPerSessionMax    int64         `json:"requests_per_session_max"`
}

// UsageEndpointStats are the window's counts and latencies for one matched
// route.
type UsageEndpointStats struct {
	Method     string           `json:"method"`
	Route      string           `json:"route"`
	Requests   int64            `json:"requests"`
	Errors     int64            `json:"errors"`
	ErrorRate  float64          `json:"error_rate"`
	P50Ms      float64          `json:"p50_ms"`
	P95Ms      float64          `json:"p95_ms"`
	ByCategory map[string]int64 `json:"by_category"`
}

// UsageCount is a request and error count for one value, such as an MCP
// tool.
type UsageCount struct {
	Value    string `json:"value"`
	Requests int64  `json:"requests"`
	Errors   int64  `json:"errors"`
}

// UsageErrorMessage is how often one error message was returned.
type UsageErrorMessage struct {
	Message string `json:"message"`
	Count   int64  `json:"count"`
}

// UsageErrorExample is one error request. Path, Query, Message, and
// ErrorKind are "" when not recorded.
type UsageErrorExample struct {
	Time      string        `json:"ts"` // RFC3339 UTC
	Path      string        `json:"path"`
	Query     string        `json:"query"`
	Category  UsageCategory `json:"category"`
	MCPTool   string        `json:"mcp_tool"`
	MCPAgent  string        `json:"mcp_agent"`
	ClientID  string        `json:"client_id"`
	Username  string        `json:"username"`
	Message   string        `json:"message"`
	ErrorKind string        `json:"error_kind"`
}

// UsageErrorGroup is the window's errors for one method, route, status, and
// error code. Route is "" for unmatched requests (code no_route). ErrorCode
// is nil for rows recorded before error details were stored.
type UsageErrorGroup struct {
	Method     string              `json:"method"`
	Route      string              `json:"route"`
	Status     int                 `json:"status"`
	ErrorCode  *string             `json:"error_code"`
	Count      int64               `json:"count"`
	FirstSeen  string              `json:"first_seen"` // RFC3339 UTC
	LastSeen   string              `json:"last_seen"`  // RFC3339 UTC
	ErrorKinds map[string]int64    `json:"error_kinds"`
	Messages   []UsageErrorMessage `json:"messages"` // top usageErrorMessageLimit
	Examples   []UsageErrorExample `json:"examples"` // latest usageErrorExampleLimit
	ByCategory map[string]int64    `json:"by_category"`
	ByTool     map[string]int64    `json:"by_tool"`
	ByAgent    map[string]int64    `json:"by_agent"`
}

// UsageUnmatched is the request count for one unmatched method and path.
type UsageUnmatched struct {
	Method     string           `json:"method"`
	Path       string           `json:"path"`
	Requests   int64            `json:"requests"`
	ByCategory map[string]int64 `json:"by_category"`
	ByTool     map[string]int64 `json:"by_tool"`
	ByAgent    map[string]int64 `json:"by_agent"`
}

// UsageActor is a user (Name = username, ByIP false) or a client (Name =
// client ID, or socket IP with ByIP true for rows without a client ID).
type UsageActor struct {
	Name       string           `json:"name"`
	ByIP       bool             `json:"by_ip"`
	Requests   int64            `json:"requests"`
	ByCategory map[string]int64 `json:"by_category"`
}

// UsageDay is the window's request count on one UTC date.
type UsageDay struct {
	Date       string           `json:"date"` // YYYY-MM-DD, UTC
	Requests   int64            `json:"requests"`
	ByCategory map[string]int64 `json:"by_category"`
}

// UsagePeriod is the request count of one window-length period.
type UsagePeriod struct {
	Start      string           `json:"start"` // exclusive, RFC3339 UTC
	End        string           `json:"end"`   // inclusive, RFC3339 UTC
	Requests   int64            `json:"requests"`
	ByCategory map[string]int64 `json:"by_category"`
	ByEndpoint map[string]int64 `json:"by_endpoint"` // "<METHOD> <route>"
}

// UsageSummary holds the aggregates of one usage window.
type UsageSummary struct {
	Window          UsageWindow          `json:"window"`
	GeneratedAt     string               `json:"generated_at"`    // RFC3339 UTC
	WindowStart     string               `json:"window_start"`    // "" for all
	EarliestRecord  string               `json:"earliest_record"` // "" when empty
	RetentionDays   int                  `json:"retention_days"`
	DroppedEvents   int64                `json:"dropped_events"`
	Totals          UsageTotals          `json:"totals"`
	Categories      []UsageCategoryStats `json:"categories"` // all six, fixed order
	Endpoints       []UsageEndpointStats `json:"endpoints"`
	MCPTools        []UsageCount         `json:"mcp_tools"`
	MCPAgents       []UsageCount         `json:"mcp_agents"`
	OtherUserAgents []UsageCount         `json:"other_user_agents"`
	Unmatched       []UsageUnmatched     `json:"unmatched"`
	Users           []UsageActor         `json:"users"`
	Clients         []UsageActor         `json:"clients"`
	Days            []UsageDay           `json:"days"`
	Series          []UsagePeriod        `json:"series"`
	Errors          []UsageErrorGroup    `json:"errors"`
}

// Summary flushes queued events, then aggregates window w at s.now().
// An invalid w returns an error wrapping ErrUsageWindowInvalid. After
// Close it returns a non-nil error.
func (s *UsageStore) Summary(ctx context.Context, w UsageWindow) (UsageSummary, error) {
	if !w.Valid() {
		return UsageSummary{}, fmt.Errorf("%w: %q", ErrUsageWindowInvalid, w)
	}

	if err := s.Flush(ctx); err != nil {
		return UsageSummary{}, fmt.Errorf("mlwh: flush usage: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UsageSummary{}, fmt.Errorf("mlwh: summarise usage: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	now := s.now().UTC()
	q := usageSummaryQuery{
		tx: tx, nowMS: now.UnixMilli(), windowMS: w.Duration().Milliseconds(), fromMS: math.MinInt64,
	}

	summary := UsageSummary{
		Window: w, GeneratedAt: now.Format(time.RFC3339),
		RetentionDays: s.retentionDays, DroppedEvents: s.Dropped(),
	}

	if q.windowMS > 0 {
		q.fromMS = q.nowMS - q.windowMS
		summary.WindowStart = usageFormatMS(q.fromMS)
	}

	if err = q.fill(ctx, &summary); err != nil {
		return UsageSummary{}, fmt.Errorf("mlwh: summarise usage: %w", err)
	}

	return summary, nil
}

func usageFormatMS(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// usageRankedGroup is one group of a ranked list: its key columns, request
// and error counts, and per-value counts of one breakdown column.
type usageRankedGroup struct {
	keys     []string
	requests int64
	errors   int64
	by       map[string]int64
}

func scanUsageRankedRow(rows *sql.Rows, nKeys int) (usageRankedGroup, sql.NullString, int64, error) {
	group := usageRankedGroup{keys: make([]string, nKeys), by: map[string]int64{}}

	var (
		value sql.NullString
		count int64
	)

	dest := make([]any, 0, nKeys+4)
	for i := range nKeys {
		dest = append(dest, &group.keys[i])
	}

	dest = append(dest, &group.requests, &group.errors, &value, &count)

	return group, value, count, rows.Scan(dest...)
}

// usageAggregate is a request, error, and distinct-caller count row.
type usageAggregate struct {
	requests, errors, sessions, ipClients, users int64
}

// usageSessionSpread is the nearest-rank median and the max of one
// category's per-session request counts.
type usageSessionSpread struct {
	median, max int64
}

// usageSummaryQuery runs the Summary queries for one window inside one read
// transaction, so every aggregate sees the same rows.
type usageSummaryQuery struct {
	tx       *sql.Tx
	nowMS    int64
	windowMS int64 // 0 for all
	fromMS   int64 // rows with ts_ms > fromMS are in the window
}

// fill sets every aggregate field of summary.
func (q usageSummaryQuery) fill(ctx context.Context, summary *UsageSummary) error {
	earliest, hasRows, err := q.earliest(ctx)
	if err != nil {
		return err
	}

	if hasRows {
		summary.EarliestRecord = usageFormatMS(earliest)
	}

	if summary.Totals, summary.Categories, err = q.categories(ctx); err != nil {
		return err
	}

	if summary.Endpoints, err = q.endpoints(ctx); err != nil {
		return err
	}

	if summary.MCPTools, err = q.counts(ctx, "mcp_tool", "mcp_tool <> ''"); err != nil {
		return err
	}

	if summary.MCPAgents, err = q.counts(ctx, "mcp_agent", "mcp_agent <> ''"); err != nil {
		return err
	}

	if summary.OtherUserAgents, err = q.counts(ctx, "user_agent", usageOtherFilter); err != nil {
		return err
	}

	if summary.Unmatched, err = q.unmatched(ctx); err != nil {
		return err
	}

	if summary.Users, summary.Clients, err = q.actors(ctx); err != nil {
		return err
	}

	if summary.Days, err = q.days(ctx, earliest, hasRows); err != nil {
		return err
	}

	if summary.Series, err = q.series(ctx, earliest, hasRows); err != nil {
		return err
	}

	summary.Errors, err = q.errorGroups(ctx)

	return err
}

// earliest returns the oldest ts_ms in the store, and false when it is empty.
func (q usageSummaryQuery) earliest(ctx context.Context) (int64, bool, error) {
	var earliest sql.NullInt64

	err := q.tx.QueryRowContext(ctx, "SELECT MIN(ts_ms) FROM usage_requests").Scan(&earliest)

	return earliest.Int64, earliest.Valid, err
}

// categories returns the window totals and the stats of all six categories.
func (q usageSummaryQuery) categories(ctx context.Context) (UsageTotals, []UsageCategoryStats, error) {
	var total usageAggregate

	err := q.tx.QueryRowContext(ctx, "SELECT "+usageAggregateColumns+
		" FROM usage_requests WHERE ts_ms > ?", q.fromMS).Scan(
		&total.requests, &total.errors, &total.sessions, &total.ipClients, &total.users)
	if err != nil {
		return UsageTotals{}, nil, err
	}

	byCategory, err := q.categoryAggregates(ctx)
	if err != nil {
		return UsageTotals{}, nil, err
	}

	spread, err := q.sessionSpread(ctx)
	if err != nil {
		return UsageTotals{}, nil, err
	}

	stats := make([]UsageCategoryStats, 0, len(UsageCategories()))

	for _, category := range UsageCategories() {
		agg := byCategory[string(category)]
		stats = append(stats, UsageCategoryStats{
			Category: category, Requests: agg.requests, Errors: agg.errors,
			ErrorRate: usageErrorRate(agg.errors, agg.requests), Sessions: agg.sessions,
			IPClients: agg.ipClients, Users: agg.users,
			RequestsPerSessionMedian: spread[string(category)].median,
			RequestsPerSessionMax:    spread[string(category)].max,
		})
	}

	return UsageTotals{
		Requests: total.requests, Errors: total.errors,
		ErrorRate: usageErrorRate(total.errors, total.requests), Sessions: total.sessions,
		IPClients: total.ipClients, Users: total.users,
	}, stats, nil
}

func usageErrorRate(errs, requests int64) float64 {
	if requests == 0 {
		return 0
	}

	return float64(errs) / float64(requests)
}

func (q usageSummaryQuery) categoryAggregates(ctx context.Context) (map[string]usageAggregate, error) {
	rows, err := q.tx.QueryContext(ctx, "SELECT category, "+usageAggregateColumns+
		" FROM usage_requests WHERE ts_ms > ? GROUP BY category", q.fromMS)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	aggregates := map[string]usageAggregate{}

	for rows.Next() {
		var (
			category string
			agg      usageAggregate
		)

		if err = rows.Scan(&category, &agg.requests, &agg.errors, &agg.sessions,
			&agg.ipClients, &agg.users); err != nil {
			return nil, err
		}

		aggregates[category] = agg
	}

	return aggregates, rows.Err()
}

// sessionSpread returns each category's usageSessionSpread.
func (q usageSummaryQuery) sessionSpread(ctx context.Context) (map[string]usageSessionSpread, error) {
	rows, err := q.tx.QueryContext(ctx, `
WITH s AS (
    SELECT category, COUNT(*) AS c FROM usage_requests
    WHERE ts_ms > ? AND client_id <> '' GROUP BY category, client_id
), r AS (
    SELECT category, c, ROW_NUMBER() OVER (PARTITION BY category ORDER BY c) AS rn,
        COUNT(*) OVER (PARTITION BY category) AS n FROM s
)
SELECT category, MAX(CASE WHEN rn = `+usageNearestRankSQL(50)+` THEN c END), MAX(c)
FROM r GROUP BY category`, q.fromMS)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	spread := map[string]usageSessionSpread{}

	for rows.Next() {
		var (
			category string
			stats    usageSessionSpread
		)

		if err = rows.Scan(&category, &stats.median, &stats.max); err != nil {
			return nil, err
		}

		spread[category] = stats
	}

	return spread, rows.Err()
}

// usageNearestRankSQL returns the SQL for the 1-based nearest rank
// ceil(p/100 * n) of percentile p, given a column n holding the count.
func usageNearestRankSQL(p int) string {
	return fmt.Sprintf("(%d * n + 99) / 100", p)
}

// endpoints returns every matched route in the window, ranked by requests.
func (q usageSummaryQuery) endpoints(ctx context.Context) ([]UsageEndpointStats, error) {
	groups, err := q.ranked(ctx, []string{"method", "route"}, "route <> ''", "category", usageNoLimit)
	if err != nil {
		return nil, err
	}

	latency, err := q.tx.PrepareContext(ctx, usageLatencyAtRankSQL)
	if err != nil {
		return nil, err
	}

	defer func() { _ = latency.Close() }()

	endpoints := make([]UsageEndpointStats, 0, len(groups))

	for _, group := range groups {
		stats := UsageEndpointStats{
			Method: group.keys[0], Route: group.keys[1], Requests: group.requests, Errors: group.errors,
			ErrorRate: usageErrorRate(group.errors, group.requests), ByCategory: group.by,
		}

		if stats.P50Ms, err = q.latencyMS(ctx, latency, stats, 50); err != nil {
			return nil, err
		}

		if stats.P95Ms, err = q.latencyMS(ctx, latency, stats, 95); err != nil {
			return nil, err
		}

		endpoints = append(endpoints, stats)
	}

	return endpoints, nil
}

// latencyMS returns the nearest-rank percentile p of the window latencies of
// the endpoint's route, in ms, using a prepared usageLatencyAtRankSQL.
func (q usageSummaryQuery) latencyMS(ctx context.Context, latency *sql.Stmt, endpoint UsageEndpointStats,
	p int64,
) (float64, error) {
	var us int64

	err := latency.QueryRowContext(ctx, endpoint.Method, endpoint.Route, q.fromMS,
		usageNearestRank(p, endpoint.Requests)-1).Scan(&us)

	return usageMS(us), err
}

// usageNearestRank returns the 1-based nearest rank ceil(p/100 * n) of
// percentile p among n values; usageNearestRankSQL is its SQL form.
func usageNearestRank(p, n int64) int64 {
	return (p*n + 99) / 100
}

func usageMS(us int64) float64 {
	return float64(us) / 1000
}

// counts returns the top usageTopLimit values of column among window rows
// matching filter.
func (q usageSummaryQuery) counts(ctx context.Context, column, filter string) ([]UsageCount, error) {
	groups, err := q.ranked(ctx, []string{column}, filter, "''", usageTopLimit)
	if err != nil {
		return nil, err
	}

	counts := make([]UsageCount, 0, len(groups))

	for _, group := range groups {
		counts = append(counts, UsageCount{Value: group.keys[0], Requests: group.requests, Errors: group.errors})
	}

	return counts, nil
}

// unmatched returns the top usageTopLimit unmatched method and path pairs,
// each with its per-tool and per-agent counts matched by method and path.
func (q usageSummaryQuery) unmatched(ctx context.Context) ([]UsageUnmatched, error) {
	groups, err := q.ranked(ctx, usageUnmatchedKeys, usageUnmatchedFilter, "category", usageTopLimit)
	if err != nil {
		return nil, err
	}

	byTool, err := q.unmatchedBy(ctx, "mcp_tool")
	if err != nil {
		return nil, err
	}

	byAgent, err := q.unmatchedBy(ctx, "mcp_agent")
	if err != nil {
		return nil, err
	}

	unmatched := make([]UsageUnmatched, 0, len(groups))

	for _, group := range groups {
		key := [2]string{group.keys[0], group.keys[1]}
		unmatched = append(unmatched, UsageUnmatched{
			Method: key[0], Path: key[1], Requests: group.requests, ByCategory: group.by,
			ByTool: usageNonNilCounts(byTool[key]), ByAgent: usageNonNilCounts(byAgent[key]),
		})
	}

	return unmatched, nil
}

// unmatchedBy returns the dimension counts of the top unmatched method and
// path pairs, keyed by method and path.
func (q usageSummaryQuery) unmatchedBy(ctx context.Context, dimension string) (map[[2]string]map[string]int64, error) {
	groups, err := q.ranked(ctx, usageUnmatchedKeys, usageUnmatchedFilter, dimension, usageTopLimit)
	if err != nil {
		return nil, err
	}

	by := make(map[[2]string]map[string]int64, len(groups))

	for _, group := range groups {
		by[[2]string{group.keys[0], group.keys[1]}] = group.by
	}

	return by, nil
}

// actors returns the top usageTopLimit users and clients.
func (q usageSummaryQuery) actors(ctx context.Context) ([]UsageActor, []UsageActor, error) {
	userGroups, err := q.ranked(ctx, []string{"username"}, "username <> ''", "category", usageTopLimit)
	if err != nil {
		return nil, nil, err
	}

	clientGroups, err := q.ranked(ctx,
		[]string{"CASE WHEN client_id <> '' THEN client_id ELSE remote_addr END", "client_id = ''"},
		"1", "category", usageTopLimit)
	if err != nil {
		return nil, nil, err
	}

	users := make([]UsageActor, 0, len(userGroups))

	for _, group := range userGroups {
		users = append(users, UsageActor{Name: group.keys[0], Requests: group.requests, ByCategory: group.by})
	}

	clients := make([]UsageActor, 0, len(clientGroups))

	for _, group := range clientGroups {
		clients = append(clients, UsageActor{
			Name: group.keys[0], ByIP: group.keys[1] == "1", Requests: group.requests, ByCategory: group.by,
		})
	}

	return users, clients, nil
}

// ranked groups window rows matching filter by the key expressions, ranks
// the groups by requests descending then keys ascending, keeps the first
// limit (usageNoLimit for all), and counts each group's rows per non-empty
// value of the dimension expression.
func (q usageSummaryQuery) ranked(ctx context.Context, keys []string, filter, dimension string,
	limit int,
) ([]usageRankedGroup, error) {
	rows, err := q.tx.QueryContext(ctx, usageRankedSQL(keys, filter, dimension), q.fromMS, limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var groups []usageRankedGroup

	for rows.Next() {
		group, value, count, err := scanUsageRankedRow(rows, len(keys))
		if err != nil {
			return nil, err
		}

		if n := len(groups); n == 0 || !slices.Equal(groups[n-1].keys, group.keys) {
			groups = append(groups, group)
		}

		if value.Valid {
			groups[len(groups)-1].by[value.String] = count
		}
	}

	return groups, rows.Err()
}

// usageRankedSQL builds the ranked query: one row per group and non-empty
// dimension value (or one row with a NULL value for a group without any),
// in rank order. Its parameters are the window start and the limit. Window
// rows are grouped by keys and dimension once, so ranking and the breakdown
// join read those groups rather than every row. INDEXED BY stops the planner
// walking usage_requests_route_ts for the endpoint grouping, which reads the
// table in route order and is slower than reading the window in ts order.
func usageRankedSQL(keys []string, filter, dimension string) string {
	selected := make([]string, len(keys))
	plain := make([]string, len(keys))
	top := make([]string, len(keys))
	joins := make([]string, len(keys))

	for i, key := range keys {
		selected[i] = fmt.Sprintf("%s AS k%d", key, i)
		plain[i] = fmt.Sprintf("k%d", i)
		top[i] = "top." + plain[i]
		joins[i] = fmt.Sprintf("g.k%d = top.k%d", i, i)
	}

	plainKeys, topKeys := strings.Join(plain, ", "), strings.Join(top, ", ")

	return fmt.Sprintf(`
WITH g AS (
    SELECT %s, %s AS d, COUNT(*) AS c, SUM(status >= 400) AS e
    FROM usage_requests INDEXED BY usage_requests_ts WHERE ts_ms > ? AND (%s) GROUP BY %s, d
), top AS (
    SELECT %s, SUM(c) AS n, SUM(e) AS e FROM g GROUP BY %s ORDER BY n DESC, %s LIMIT ?
)
SELECT %s, top.n, top.e, g.d, COALESCE(g.c, 0)
FROM top LEFT JOIN g ON %s AND g.d <> ''
ORDER BY top.n DESC, %s`,
		strings.Join(selected, ", "), dimension, filter, plainKeys,
		plainKeys, plainKeys, plainKeys,
		topKeys, strings.Join(joins, " AND "), topKeys)
}

// days returns every UTC date from the window start (or the earliest row for
// all) to now, with the window's rows counted per date. Rows after now count
// on their own date when it is listed.
func (q usageSummaryQuery) days(ctx context.Context, earliest int64, hasRows bool) ([]UsageDay, error) {
	firstMS := q.fromMS
	if q.windowMS == 0 {
		if !hasRows {
			return []UsageDay{}, nil
		}

		firstMS = earliest
	}

	counts, err := q.groupCounts(ctx, `SELECT ts_ms / ?, category, COUNT(*)
FROM usage_requests WHERE ts_ms > ? GROUP BY 1, 2`, usageMSPerDay, q.fromMS)
	if err != nil {
		return nil, err
	}

	days := []UsageDay{}

	for day := firstMS / usageMSPerDay; day <= q.nowMS/usageMSPerDay; day++ {
		byCategory := counts[day]
		if byCategory == nil {
			byCategory = map[string]int64{}
		}

		days = append(days, UsageDay{
			Date:       time.UnixMilli(day * usageMSPerDay).UTC().Format(usageDayLayout),
			Requests:   usageSumCounts(byCategory),
			ByCategory: byCategory,
		})
	}

	return days, nil
}

// series returns the consecutive window-length periods from the one holding
// the earliest row to period 0, oldest first, or [] for all or no rows.
func (q usageSummaryQuery) series(ctx context.Context, earliest int64, hasRows bool) ([]UsagePeriod, error) {
	if q.windowMS == 0 || !hasRows {
		return []UsagePeriod{}, nil
	}

	periodSQL := `SELECT CASE WHEN ts_ms > ? THEN 0 ELSE (? - ts_ms) / ? END AS k, %s, COUNT(*)
FROM usage_requests WHERE %s GROUP BY 1, 2`

	byCategory, err := q.groupCounts(ctx, fmt.Sprintf(periodSQL, "category", "1"),
		q.nowMS, q.nowMS, q.windowMS)
	if err != nil {
		return nil, err
	}

	byEndpoint, err := q.groupCounts(ctx, fmt.Sprintf(periodSQL, "method || ' ' || route", "route <> ''"),
		q.nowMS, q.nowMS, q.windowMS)
	if err != nil {
		return nil, err
	}

	periods := max(q.nowMS-earliest, 0)/q.windowMS + 1

	series := make([]UsagePeriod, 0, periods)

	for k := periods - 1; k >= 0; k-- {
		period := UsagePeriod{
			Start:      usageFormatMS(q.nowMS - (k+1)*q.windowMS),
			End:        usageFormatMS(q.nowMS - k*q.windowMS),
			ByCategory: usageNonNilCounts(byCategory[k]),
			ByEndpoint: usageNonNilCounts(byEndpoint[k]),
		}
		period.Requests = usageSumCounts(period.ByCategory)
		series = append(series, period)
	}

	return series, nil
}

func usageNonNilCounts(counts map[string]int64) map[string]int64 {
	if counts == nil {
		return map[string]int64{}
	}

	return counts
}

func usageSumCounts(counts map[string]int64) int64 {
	var total int64

	for _, n := range counts {
		total += n
	}

	return total
}

// groupCounts runs a query returning (bucket, value, count) rows and returns
// the counts keyed by bucket then value.
func (q usageSummaryQuery) groupCounts(ctx context.Context, query string,
	args ...any,
) (map[int64]map[string]int64, error) {
	rows, err := q.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	counts := map[int64]map[string]int64{}

	for rows.Next() {
		var (
			bucket int64
			value  string
			count  int64
		)

		if err = rows.Scan(&bucket, &value, &count); err != nil {
			return nil, err
		}

		if counts[bucket] == nil {
			counts[bucket] = map[string]int64{}
		}

		counts[bucket][value] = count
	}

	return counts, rows.Err()
}
