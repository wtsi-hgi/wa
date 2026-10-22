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

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
)

const (
	// searchMinTermLength mirrors the mlwh layer's minimum effective term length;
	// shorter terms are short-circuited there to an empty result, so the CLI
	// reports the requirement instead of silently returning nothing.
	searchMinTermLength = 3

	// searchSampleCountCap is the sample-count floor reported by the mlwh layer:
	// CountSampleSearch is exact up to this cap, and a count that reaches it is a
	// floor (rendered "10000+") rather than an exact total.
	searchSampleCountCap = 10000

	// mlwhCacheUnavailableMessage is the neutral message shown when the MLWH cache
	// has never been synced (or is otherwise empty). It deliberately omits any
	// "wa mlwh sync" hint because end-users going via the server cannot sync. It is
	// shared with the never-synced rendering of `wa mlwh info` (see mlwh_info.go).
	mlwhCacheUnavailableMessage = "the MLWH cache is not available yet"

	// mlwhSearchTimeout bounds a single `wa mlwh search` invocation so a slow term
	// (two unrelated common words can match millions of token rows and take ~30s;
	// see 260627-9) cannot hang the CLI. It is generous, well above any normal
	// query (~0.1s) and below the RemoteClient's own 30s server-mode cap, and it
	// also bounds local-cache mode, which has no such cap of its own.
	mlwhSearchTimeout = 15 * time.Second

	// mlwhSearchTimedOutMessage is shown when a search is cut off by
	// mlwhSearchTimeout (or otherwise reports a deadline). It explains the likely
	// cause and the action to take without printing a raw error or any sync hint.
	mlwhSearchTimedOutMessage = "search timed out: a term of two common words can match a very large number of samples; try a more specific term"
)

var openMLWHSearchClient = func(ctx context.Context, cfg mlwh.Config) (mlwhSearchClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return mlwh.OpenCacheOnly(ctx, cfg.Cache)
	}

	return mlwh.Open(ctx, cfg)
}

var openMLWHSearchRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhSearchClient, error) {
	return mlwh.NewRemoteClient(cfg)
}

// mlwhSearchClient is the subset of the MLWH query surface used by
// `wa mlwh search`. Both *mlwh.RemoteClient and the local *mlwh.Client satisfy
// it.
type mlwhSearchClient interface {
	CountStudySearch(ctx context.Context, term string) (mlwh.Count, error)
	CountSampleSearchWithOptions(ctx context.Context, term string, opts mlwh.SampleSearchOptions) (mlwh.Count, error)
	SearchStudiesCursor(ctx context.Context, term string, page mlwh.SearchPageOptions) (mlwh.SearchCursorPage[mlwh.Study], error)
	SearchSamplesCursor(ctx context.Context, term string, opts mlwh.SampleSearchOptions, page mlwh.SearchPageOptions) (mlwh.SearchCursorPage[mlwh.Sample], error)
	Close() error
}

func openMLWHSearchConfiguredClient(ctx context.Context, serverURL string) (mlwhSearchClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHSearchRemoteClient(ctx, mlwhCLIRemoteConfig(trimmedServerURL))
	}

	cfg, err := resolveMLWHInfoLocalConfig()
	if err != nil {
		return nil, err
	}

	client, err := openMLWHSearchClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.DSN) != "" && errors.Is(err, mlwh.ErrPasswordInDSN) {
			return nil, fmt.Errorf("WA_MLWH_DSN: %w", err)
		}

		return nil, err
	}

	return client, nil
}

func newMLWHSearchCommand() *cobra.Command {
	var (
		serverURL        string
		typeFlag         string
		words            bool
		organism         string
		libraryType      string
		qc               string
		deliverablesOnly bool
		limit            int
		offset           int
		cursor           string
		jsonOut          bool
		withData         bool
	)

	command := &cobra.Command{
		Use:   "search <term>",
		Short: "Search MLWH studies and samples by a free-text term",
		Long: strings.Join([]string{
			"Search the Sanger Multi-LIMS Warehouse (MLWH) for studies and samples",
			"that match a free-text term, through a wa mlwh serve API. Study search",
			"is a case-insensitive substring over study name, title, programme and",
			"faculty sponsor; sample search defaults to a literal whole-value prefix",
			"over sample name, supplier name, common name and donor id. Add --words",
			"for the legacy separator-agnostic word-prefix search. Free-text terms",
			"must be at least 3 characters unless an exact sample filter is supplied",
			"for a sample search. This is a read-only query tool.",
			"",
			"Use this when you have a partial study title, programme, organism or",
			"supplier name and want to discover the matching studies or samples.",
			"By default both studies and samples are searched; pass --type to",
			"restrict to one. Sample searches can be narrowed with --organism,",
			"--library-type, --qc pass|fail|pending and --deliverables-only. Use",
			"--limit to size a page and --json for a single JSON object suitable",
			"for piping into jq. Each section reports a total match count; a very",
			"common sample term reports its count as a floor (e.g. 10000+).",
			"",
			"To page, pass the next cursor a page reports to --cursor, with the",
			"same term, --type study or sample, and options. A cursor continues",
			"just after the last row of the page that gave it (like Elasticsearch's",
			"search_after), so later pages do not re-scan earlier ones, and rows",
			"are never repeated or skipped when the cache syncs between pages. If",
			"it has synced since the first page, the page carries a warning: rows",
			"that newly match before the cursor are missed until a new search.",
			"--offset still skips rows instead, but cannot be combined with",
			"--cursor.",
			"",
			"The --json object has two parts:",
			`  {"query": {"term", "type", "words", "organism", "library_type", "qc",`,
			`             "deliverables_only", "with_data", "limit", "offset",`,
			`             "cursor", "next": {"studies", "samples"},`,
			`             "cache_synced_at", "wa_version"},`,
			`   "results": {"studies": {"count", "results": [...]},`,
			`               "samples": {"count", "results": [...]}},`,
			`   "timed_out": true, "warnings": [...]}`,
			"query echoes the term and the options that shaped the results (type",
			"is study, sample or both; organism, library_type, qc and cursor",
			"appear only when given), next (per requested type, the cursor for",
			"its next page, or null when no rows remain), cache_synced_at (the",
			"oldest sync time across the cache's tables) and wa_version (this",
			"client's version). results has a section per requested type.",
			"timed_out appears only when the search ran out of time, so results",
			"are incomplete; warnings only when there is one, such as a cache sync",
			"since the first page.",
			"",
			"Add --with-data to list each returned sample's sequencing data under",
			"it: one row per product that has an iRODS object (the sample's",
			"deliverable CRAMs, as 'wa mlwh export irods sample <name>' lists them),",
			"so products without an iRODS object do not appear. Text output shows",
			"'<study> <platform> <run>_<lane>#<tag> <manual_qc> <irods_path>' per",
			"row, or 'no data'; --json adds a data array to each sample of",
			"{id_study_lims, study_accession_number, platform, id_run, lane,",
			"tag_index, manual_qc, irods_path}. A sample whose data could not be",
			"listed shows 'data unavailable: <reason>' in text, and has no data",
			"array but a data_error string in --json. It costs one export per",
			"returned sample, so --limit bounds it; at most 4 run at once. If the",
			"exports run out of time, the samples cut off show 'timed out' as",
			"their reason and the output is marked timed out (timed_out in --json).",
			"",
			mlwhQueryCommandConfigurationHelp,
			"",
			"Examples:",
			"  # Query a development stack started by make dev",
			"  wa --env development mlwh search malaria",
			"",
			"  # Query a remote MLWH server for studies only and emit JSON",
			"  wa mlwh search \"lung cancer\" --server http://host:8091 --type study --json",
			"",
			"  # Page sample matches against a local operator cache",
			"  WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite wa mlwh search musculus --type sample --limit 100",
			"",
			"  # Loop over every sample match, page by page",
			"  cursor=",
			"  while page=$(wa mlwh search musculus --type sample --json ${cursor:+--cursor \"$cursor\"}); do",
			"    echo \"$page\" | jq -c '.results.samples.results[]'",
			"    cursor=$(echo \"$page\" | jq -r '.query.next.samples // empty')",
			"    [ -n \"$cursor\" ] || break",
			"  done",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError("usage: wa mlwh search <term>")
			}

			term := strings.TrimSpace(args[0])
			if term == "" {
				return usageError("usage: wa mlwh search <term>")
			}

			cursor = strings.TrimSpace(cursor)
			if err := validateSearchCursorFlags(cursor, typeFlag, offset); err != nil {
				return err
			}

			// Bound the whole invocation so a slow term cannot hang the CLI; the
			// deadline propagates to the search (and, in server mode, the remote
			// query). runMLWHSearch turns a resulting deadline into a clear message.
			timeout := mlwhSearchTimeout
			if withData {
				wantStudies, wantSamples, err := searchTypeSelection(typeFlag)
				if err == nil && wantStudies && !wantSamples {
					return usageError("--with-data needs a sample search")
				}

				timeout = mlwhSearchWithDataTimeout
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			client, err := openMLWHSearchConfiguredClient(ctx, serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			opts := mlwh.SampleSearchOptions{
				Words:            words,
				Organism:         organism,
				LibraryType:      libraryType,
				QC:               qc,
				DeliverablesOnly: deliverablesOnly,
			}

			return runMLWHSearch(ctx, client, cmd.OutOrStdout(), term, typeFlag, opts, searchPage{limit: limit, offset: offset, cursor: cursor, withData: withData}, jsonOut)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().StringVar(&typeFlag, "type", "", "restrict the search to one kind (study|sample); default is both")
	command.Flags().BoolVar(&words, "words", false, "use separator-agnostic word-prefix sample search instead of the default literal whole-value prefix")
	command.Flags().StringVar(&organism, "organism", "", "restrict sample matches to this organism/common name")
	command.Flags().StringVar(&libraryType, "library-type", "", "restrict sample matches to this library type")
	command.Flags().StringVar(&qc, "qc", "", "restrict sample matches by product QC: pass, fail or pending")
	command.Flags().BoolVar(&deliverablesOnly, "deliverables-only", false, "restrict sample matches to deliverable data objects")
	command.Flags().IntVar(&limit, "limit", 50, "maximum number of results to return per kind")
	command.Flags().IntVar(&offset, "offset", 0, "number of results to skip per kind (for pagination)")
	command.Flags().StringVar(&cursor, "cursor", "", "continue a --type study or --type sample search after the page whose next cursor this is")
	command.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON object instead of human-readable text")
	command.Flags().BoolVar(&withData, "with-data", false, "list each returned sample's products that have an iRODS object (study, platform, run, lane, tag, manual QC, iRODS path)")

	return command
}

// validateSearchCursorFlags rejects a --cursor without a single --type (each
// kind pages with its own cursor) or combined with --offset.
func validateSearchCursorFlags(cursor, typeFlag string, offset int) error {
	if cursor == "" {
		return nil
	}

	wantStudies, wantSamples, err := searchTypeSelection(typeFlag)
	if err == nil && wantStudies && wantSamples {
		return usageError("--cursor needs --type study or --type sample: each kind pages with its own cursor")
	}
	if offset != 0 {
		return usageError("--cursor and --offset cannot be combined: continue with the cursor alone")
	}

	return nil
}

func runMLWHSearch(ctx context.Context, client mlwhSearchClient, out io.Writer, term, typeFlag string, sampleOpts mlwh.SampleSearchOptions, page searchPage, jsonOut bool) error {
	wantStudies, wantSamples, err := searchTypeSelection(typeFlag)
	if err != nil {
		return err
	}

	if len(strings.TrimSpace(term)) < searchMinTermLength && (!wantSamples || !sampleSearchHasExactFilter(sampleOpts)) {
		_, _ = fmt.Fprintf(out, "search term must be at least %d characters\n", searchMinTermLength)

		return nil
	}

	report := searchReport{Term: term}

	// A section timeout sets report.TimedOut and leaves that section nil; the
	// shared ctx deadline has then expired, so a later section is skipped (it
	// would only time out again) while any already-collected section is kept. A
	// non-timeout error still surfaces. Rendering then stays valid in both modes.
	if wantStudies && !report.TimedOut {
		if err := addStudySearchSection(ctx, client, &report, term, page); err != nil {
			return err
		}
	}

	if wantSamples && !report.TimedOut {
		if err := addSampleSearchSection(ctx, client, &report, term, sampleOpts, page); err != nil {
			return err
		}
	}

	if page.withData && report.Samples != nil {
		if err := addSampleSearchData(ctx, client, &report); err != nil {
			return err
		}
	}

	if jsonOut {
		query := newSearchQuery(term, typeFlag, sampleOpts, page)
		query.Next = report.nextCursors()
		query.CacheSyncedAt = mlwhCacheSyncedAt(ctx, client)

		return writeMLWHJSONDocument(out, "search report", mlwhJSONDocument[searchQuery, searchReport]{
			Query:    query,
			Results:  report,
			TimedOut: report.TimedOut,
			Warnings: report.warnings,
		})
	}

	writeSearchReportText(out, report)

	return nil
}

func searchTypeSelection(typeFlag string) (wantStudies, wantSamples bool, err error) {
	switch strings.ToLower(strings.TrimSpace(typeFlag)) {
	case "":
		return true, true, nil
	case "study":
		return true, false, nil
	case "sample":
		return false, true, nil
	default:
		return false, false, fmt.Errorf("unknown --type %q (expected study or sample)", typeFlag)
	}
}

func sampleSearchHasExactFilter(opts mlwh.SampleSearchOptions) bool {
	return strings.TrimSpace(opts.Organism) != "" ||
		strings.TrimSpace(opts.LibraryType) != "" ||
		strings.TrimSpace(opts.QC) != "" ||
		opts.DeliverablesOnly
}

// addStudySearchSection runs the study search and folds the result into report.
// A deadline error is recorded as report.TimedOut (section left nil, partial
// results preserved); any other error is returned unchanged.
func addStudySearchSection(ctx context.Context, client mlwhSearchClient, report *searchReport, term string, page searchPage) error {
	section, emptyCache, err := buildStudySearchSection(ctx, client, term, page)
	if err != nil {
		if isSearchTimeoutError(err) {
			report.TimedOut = true

			return nil
		}

		return err
	}

	report.Studies = section
	report.cacheUnavailable = report.cacheUnavailable || emptyCache
	report.addPageWarnings(section.stale)

	return nil
}

func buildStudySearchSection(ctx context.Context, client mlwhSearchClient, term string, page searchPage) (*studySearchSection, bool, error) {
	emptyCache := false

	result, err := client.SearchStudiesCursor(ctx, term, page.options())
	studies := result.Items
	if err != nil {
		if !isEmptyCacheSearchError(err) {
			return nil, false, fmt.Errorf("search studies for %q: %w", term, err)
		}

		emptyCache = true
	}

	count, err := client.CountStudySearch(ctx, term)
	if err != nil {
		if !isEmptyCacheSearchError(err) {
			return nil, false, fmt.Errorf("count studies for %q: %w", term, err)
		}

		emptyCache = true
	}

	if studies == nil {
		studies = []mlwh.Study{}
	}

	return &studySearchSection{Count: count.Count, Results: studies, next: result.NextCursor, stale: result.Stale}, emptyCache, nil
}

// isEmptyCacheSearchError reports whether err is the never-synced/empty-cache
// signal that the mlwh layer joins onto search and count results. It is treated
// as an empty result (not a hard failure) so the command can render a neutral
// "cache not available" message without any sync hint.
func isEmptyCacheSearchError(err error) bool {
	return errors.Is(err, mlwh.ErrCacheNeverSynced)
}

// isSearchTimeoutError reports whether err is a search cut off by the CLI
// deadline (or a query the server cancelled at its own deadline). The
// RemoteClient wraps the context error inside ErrUpstreamImpaired, but the
// %w chain still satisfies errors.Is for context.DeadlineExceeded, so this
// matches without string-matching the message.
func isSearchTimeoutError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// addSampleSearchSection runs the sample search and folds the result into report,
// mirroring addStudySearchSection's timeout/partial-results handling.
func addSampleSearchSection(ctx context.Context, client mlwhSearchClient, report *searchReport, term string, opts mlwh.SampleSearchOptions, page searchPage) error {
	section, emptyCache, err := buildSampleSearchSection(ctx, client, term, opts, page)
	if err != nil {
		if isSearchTimeoutError(err) {
			report.TimedOut = true

			return nil
		}

		return err
	}

	report.Samples = section
	report.cacheUnavailable = report.cacheUnavailable || emptyCache
	report.addPageWarnings(section.stale)

	return nil
}

func buildSampleSearchSection(ctx context.Context, client mlwhSearchClient, term string, opts mlwh.SampleSearchOptions, page searchPage) (*sampleSearchSection, bool, error) {
	emptyCache := false

	result, err := client.SearchSamplesCursor(ctx, term, opts, page.options())
	samples := result.Items
	if err != nil {
		if !isEmptyCacheSearchError(err) {
			return nil, false, fmt.Errorf("search samples for %q: %w", term, err)
		}

		emptyCache = true
	}

	count, err := client.CountSampleSearchWithOptions(ctx, term, opts)
	if err != nil {
		if !isEmptyCacheSearchError(err) {
			return nil, false, fmt.Errorf("count samples for %q: %w", term, err)
		}

		emptyCache = true
	}

	results := make([]searchSample, len(samples))
	for index, sample := range samples {
		results[index] = searchSample{Sample: sample}
	}

	return &sampleSearchSection{Count: count.Count, Results: results, next: result.NextCursor, stale: result.Stale}, emptyCache, nil
}

func writeSearchReportText(out io.Writer, report searchReport) {
	_, _ = fmt.Fprintf(out, "Term: %s\n", report.Term)

	if report.Studies != nil {
		writeStudySearchSection(out, report.Studies)
		writeSearchNextPageHint(out, "studies", "study", report.Studies.next)
	}

	if report.Samples != nil {
		writeSampleSearchSection(out, report.Samples)
		writeSearchNextPageHint(out, "samples", "sample", report.Samples.next)
	}

	for _, warning := range report.warnings {
		_, _ = fmt.Fprintf(out, "\nwarning: %s\n", warning)
	}

	if report.cacheUnavailable {
		_, _ = fmt.Fprintf(out, "\n%s\n", mlwhCacheUnavailableMessage)
	}

	switch {
	case report.dataTimedOut:
		_, _ = fmt.Fprintf(out, "\n%s\n", mlwhSearchDataTimedOutMessage)
	case report.TimedOut:
		_, _ = fmt.Fprintf(out, "\n%s\n", mlwhSearchTimedOutMessage)
	}
}

func writeStudySearchSection(out io.Writer, section *studySearchSection) {
	_, _ = fmt.Fprintf(out, "\nStudies (%s):\n", formatSearchCount(section.Count, false))
	for _, study := range section.Results {
		_, _ = fmt.Fprintf(out, "  id_study_lims=%s", study.IDStudyLims)
		writeSearchField(out, "name", study.Name)
		writeSearchField(out, "study_title", study.StudyTitle)
		writeSearchField(out, "accession_number", study.AccessionNumber)
		_, _ = fmt.Fprintln(out)
	}
}

// writeSearchNextPageHint tells the reader how to get a section's next page.
func writeSearchNextPageHint(out io.Writer, section, searchType, next string) {
	if next == "" {
		return
	}

	_, _ = fmt.Fprintf(out, "  more %s: add --type %s --cursor %s\n", section, searchType, next)
}

// formatSearchCount renders a count header value, appending "+" when a sample
// count reaches the exact-count cap and is therefore a floor.
func formatSearchCount(count int, isSample bool) string {
	if isSample && count >= searchSampleCountCap {
		return fmt.Sprintf("%d+", count)
	}

	return fmt.Sprintf("%d", count)
}

// writeSearchField appends " key=value" for a non-blank value, mirroring the
// field-printing style of mlwh_info.go (blank fields are omitted).
func writeSearchField(out io.Writer, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}

	_, _ = fmt.Fprintf(out, " %s=%s", key, value)
}

func writeSampleSearchSection(out io.Writer, section *sampleSearchSection) {
	_, _ = fmt.Fprintf(out, "\nSamples (%s):\n", formatSearchCount(section.Count, true))
	for _, sample := range section.Results {
		_, _ = fmt.Fprintf(out, "  name=%s", sample.Name)
		writeSearchField(out, "supplier_name", sample.SupplierName)
		writeSearchField(out, "common_name", sample.CommonName)
		writeSearchField(out, "donor_id", sample.DonorID)
		writeSearchField(out, "accession_number", sample.AccessionNumber)
		_, _ = fmt.Fprintln(out)
		writeSearchSampleData(out, sample)
	}
}

// searchPage selects which results `wa mlwh search` returns per kind, and
// whether each sample's data is added.
type searchPage struct {
	limit    int
	offset   int
	cursor   string
	withData bool
}

func (page searchPage) options() mlwh.SearchPageOptions {
	return mlwh.SearchPageOptions{Limit: page.limit, Offset: page.offset, Cursor: page.cursor}
}

// studySearchSection is the JSON shape of the studies block. Results is always a
// present array (no omitempty): the build path normalizes a nil slice to an
// empty slice so an empty/never-synced cache serialises "results": [], matching
// the synced no-match success path, never "results": null.
type studySearchSection struct {
	Count   int          `json:"count"`
	Results []mlwh.Study `json:"results"`
	next    string
	stale   bool
}

// sampleSearchSection is the JSON shape of the samples block; see
// studySearchSection for the always-present Results array contract.
type sampleSearchSection struct {
	Count   int            `json:"count"`
	Results []searchSample `json:"results"`
	next    string
	stale   bool
}

// searchQuery is the --json "query" object of `wa mlwh search`: the term, the
// effective options that shaped the results, the cache sync time and the wa
// client version. Type is "study", "sample" or "both"; the string filters are
// omitted when not given.
type searchQuery struct {
	Term             string `json:"term"`
	Type             string `json:"type"`
	Words            bool   `json:"words"`
	Organism         string `json:"organism,omitempty"`
	LibraryType      string `json:"library_type,omitempty"`
	QC               string `json:"qc,omitempty"`
	DeliverablesOnly bool   `json:"deliverables_only"`
	WithData         bool   `json:"with_data"`
	Limit            int    `json:"limit"`
	Offset           int    `json:"offset"`
	Cursor           string `json:"cursor,omitempty"`
	// Next maps each searched kind ("studies", "samples") to the cursor for
	// its next page, or null when no rows remain.
	Next map[string]*string `json:"next"`
	// CacheSyncedAt is the oldest last_run across the cache's synced tables.
	CacheSyncedAt string `json:"cache_synced_at,omitempty"`
	WAVersion     string `json:"wa_version"`
}

func newSearchQuery(term, typeFlag string, opts mlwh.SampleSearchOptions, page searchPage) searchQuery {
	searchType := strings.ToLower(strings.TrimSpace(typeFlag))
	if searchType == "" {
		searchType = "both"
	}

	return searchQuery{
		Term:             term,
		Type:             searchType,
		Words:            opts.Words,
		Organism:         strings.TrimSpace(opts.Organism),
		LibraryType:      strings.TrimSpace(opts.LibraryType),
		QC:               strings.TrimSpace(opts.QC),
		DeliverablesOnly: opts.DeliverablesOnly,
		WithData:         page.withData,
		Limit:            page.limit,
		Offset:           page.offset,
		Cursor:           page.cursor,
		Next:             map[string]*string{},
		WAVersion:        waBuildVersion(),
	}
}

// searchReport holds `wa mlwh search` results; as JSON it is the "results"
// object of the --json document. A section is nil (and omitted) when --type
// does not request it. TimedOut is set (and serialised beside "results" as
// "timed_out": true) when a section search hit the deadline; the affected
// section stays nil while any already-collected section is preserved, so both
// --json and text renderings stay valid and keep partial results.
// cacheUnavailable is set (text-only, never serialised) when a never-synced/empty
// cache was observed, so the text renderer can show a neutral note without any
// sync hint.
// dataTimedOut (text-only) records that the timeout cut off --with-data's
// per-sample exports rather than a section search.
type searchReport struct {
	Term             string               `json:"-"`
	Studies          *studySearchSection  `json:"studies,omitempty"`
	Samples          *sampleSearchSection `json:"samples,omitempty"`
	TimedOut         bool                 `json:"-"`
	cacheUnavailable bool
	dataTimedOut     bool
	warnings         []string
}

// addPageWarnings records the stale-cursor warning once when a section's page
// continued a cursor across a cache sync.
func (report *searchReport) addPageWarnings(stale bool) {
	if stale && !slices.Contains(report.warnings, mlwh.SearchCursorStaleWarning) {
		report.warnings = append(report.warnings, mlwh.SearchCursorStaleWarning)
	}
}

// nextCursors maps each searched section to its next-page cursor, or nil
// when it has no more rows.
func (report searchReport) nextCursors() map[string]*string {
	next := map[string]*string{}
	if report.Studies != nil {
		next["studies"] = optionalCursor(report.Studies.next)
	}
	if report.Samples != nil {
		next["samples"] = optionalCursor(report.Samples.next)
	}

	return next
}

func optionalCursor(cursor string) *string {
	if cursor == "" {
		return nil
	}

	return &cursor
}
