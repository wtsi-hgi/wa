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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

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
	// "wa mlwh sync" hint because end-users going via the server cannot sync.
	mlwhCacheUnavailableMessage = "the MLWH cache is not available yet"
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
	SearchStudies(ctx context.Context, term string, limit, offset int) ([]mlwh.Study, error)
	SearchSamples(ctx context.Context, term string, limit, offset int) ([]mlwh.Sample, error)
	CountStudySearch(ctx context.Context, term string) (mlwh.Count, error)
	CountSampleSearch(ctx context.Context, term string) (mlwh.Count, error)
	Close() error
}

func openMLWHSearchConfiguredClient(ctx context.Context, serverURL string) (mlwhSearchClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHSearchRemoteClient(ctx, mlwh.RemoteConfig{BaseURL: trimmedServerURL})
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
		serverURL string
		typeFlag  string
		limit     int
		offset    int
		jsonOut   bool
	)

	command := &cobra.Command{
		Use:   "search <term>",
		Short: "Search MLWH studies and samples by a free-text term",
		Long: strings.Join([]string{
			"Search the Sanger Multi-LIMS Warehouse (MLWH) for studies and samples",
			"that match a free-text term, through a wa mlwh serve API. Study search",
			"is a case-insensitive substring over study name, title, programme and",
			"faculty sponsor; sample search is a case-insensitive word-prefix over",
			"sample name, supplier name, common name and donor id. The term must be",
			"at least 3 characters. This is a read-only query tool.",
			"",
			"Use this when you have a partial study title, programme, organism or",
			"supplier name and want to discover the matching studies or samples.",
			"By default both studies and samples are searched; pass --type to",
			"restrict to one. Use --limit/--offset to page results and --json for a",
			"single JSON object suitable for piping into jq. Each section reports a",
			"total match count; a very common sample term reports its count as a",
			"floor (e.g. 10000+).",
			"",
			"Normal CLI users should point this command at the MLWH query server",
			"with --server or WA_MLWH_SERVER_URL; database and cache credentials",
			"stay with the server process. When WA_ENV selects a scenario and no",
			"server URL is set, the command defaults to the active local MLWH API",
			"port from WA_*_SEQMETA_PORT. Operators can still run against a local",
			"cache with WA_MLWH_CACHE_PATH, or use WA_MLWH_DSN for direct local",
			"operator mode.",
			"",
			"Configuration is read from the environment. Use the persistent --env",
			"flag (or WA_ENV=development|test|production) to load matching",
			".env.<name> / .env.<name>.local files from the working directory",
			"before resolving:",
			"",
			"  WA_MLWH_SERVER_URL      Preferred. Base URL for wa mlwh serve.",
			"  WA_MLWH_BACKEND_URL     Lower-precedence compatibility default.",
			"  WA_*_SEQMETA_PORT       Scenario-local default API port.",
			"  WA_MLWH_DSN             Optional direct operator mode only.",
			"  WA_MLWH_PASSWORD        Optional. Password used with WA_MLWH_DSN.",
			"  WA_MLWH_CACHE_PATH      Optional local operator cache path or",
			"                          MySQL cache DSN without a password.",
			"  WA_MLWH_CACHE_PASSWORD  Optional. SQLCipher key used to encrypt",
			"                          the local cache when set.",
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
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("usage: wa mlwh search <term>")
			}

			term := strings.TrimSpace(args[0])
			if term == "" {
				return errors.New("usage: wa mlwh search <term>")
			}

			client, err := openMLWHSearchConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			return runMLWHSearch(cmd.Context(), client, cmd.OutOrStdout(), term, typeFlag, limit, offset, jsonOut)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().StringVar(&typeFlag, "type", "", "restrict the search to one kind (study|sample); default is both")
	command.Flags().IntVar(&limit, "limit", 50, "maximum number of results to return per kind")
	command.Flags().IntVar(&offset, "offset", 0, "number of results to skip per kind (for pagination)")
	command.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON object instead of human-readable text")

	return command
}

func runMLWHSearch(ctx context.Context, client mlwhSearchClient, out io.Writer, term, typeFlag string, limit, offset int, jsonOut bool) error {
	if len(strings.TrimSpace(term)) < searchMinTermLength {
		_, _ = fmt.Fprintf(out, "search term must be at least %d characters\n", searchMinTermLength)

		return nil
	}

	wantStudies, wantSamples, err := searchTypeSelection(typeFlag)
	if err != nil {
		return err
	}

	report := searchReport{Term: term}

	if wantStudies {
		section, emptyCache, sectionErr := buildStudySearchSection(ctx, client, term, limit, offset)
		if sectionErr != nil {
			return sectionErr
		}

		report.Studies = section
		report.cacheUnavailable = report.cacheUnavailable || emptyCache
	}

	if wantSamples {
		section, emptyCache, sectionErr := buildSampleSearchSection(ctx, client, term, limit, offset)
		if sectionErr != nil {
			return sectionErr
		}

		report.Samples = section
		report.cacheUnavailable = report.cacheUnavailable || emptyCache
	}

	if jsonOut {
		return writeSearchReportJSON(out, report)
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

func buildStudySearchSection(ctx context.Context, client mlwhSearchClient, term string, limit, offset int) (*studySearchSection, bool, error) {
	emptyCache := false

	studies, err := client.SearchStudies(ctx, term, limit, offset)
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

	return &studySearchSection{Count: count.Count, Results: studies}, emptyCache, nil
}

// isEmptyCacheSearchError reports whether err is the never-synced/empty-cache
// signal that the mlwh layer joins onto search and count results. It is treated
// as an empty result (not a hard failure) so the command can render a neutral
// "cache not available" message without any sync hint.
func isEmptyCacheSearchError(err error) bool {
	return errors.Is(err, mlwh.ErrCacheNeverSynced)
}

func buildSampleSearchSection(ctx context.Context, client mlwhSearchClient, term string, limit, offset int) (*sampleSearchSection, bool, error) {
	emptyCache := false

	samples, err := client.SearchSamples(ctx, term, limit, offset)
	if err != nil {
		if !isEmptyCacheSearchError(err) {
			return nil, false, fmt.Errorf("search samples for %q: %w", term, err)
		}

		emptyCache = true
	}

	count, err := client.CountSampleSearch(ctx, term)
	if err != nil {
		if !isEmptyCacheSearchError(err) {
			return nil, false, fmt.Errorf("count samples for %q: %w", term, err)
		}

		emptyCache = true
	}

	return &sampleSearchSection{Count: count.Count, Results: samples}, emptyCache, nil
}

func writeSearchReportJSON(out io.Writer, report searchReport) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode search report: %w", err)
	}

	return nil
}

func writeSearchReportText(out io.Writer, report searchReport) {
	_, _ = fmt.Fprintf(out, "Term: %s\n", report.Term)

	if report.Studies != nil {
		writeStudySearchSection(out, report.Studies)
	}

	if report.Samples != nil {
		writeSampleSearchSection(out, report.Samples)
	}

	if report.cacheUnavailable {
		_, _ = fmt.Fprintf(out, "\n%s\n", mlwhCacheUnavailableMessage)
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
	}
}

type studySearchSection struct {
	Count   int          `json:"count"`
	Results []mlwh.Study `json:"results"`
}

type sampleSearchSection struct {
	Count   int           `json:"count"`
	Results []mlwh.Sample `json:"results"`
}

// searchReport is the JSON-friendly shape of `wa mlwh search` results. A section
// is nil (and omitted) when --type does not request it. cacheUnavailable is set
// (text-only, never serialised) when a never-synced/empty cache was observed, so
// the text renderer can show a neutral note without any sync hint.
type searchReport struct {
	Term             string               `json:"term"`
	Studies          *studySearchSection  `json:"studies,omitempty"`
	Samples          *sampleSearchSection `json:"samples,omitempty"`
	cacheUnavailable bool
}
