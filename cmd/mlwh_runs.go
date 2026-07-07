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
	"strings"

	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
)

const runsNoMonthlyRowsMessage = "no runs"

const runsNoListingRowsMessage = "no runs"

const runsNoAggregateRowsMessage = "no aggregate rows"

var openMLWHRunsClient = func(ctx context.Context, cfg mlwh.Config) (mlwhRunsClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return mlwh.OpenCacheOnly(ctx, cfg.Cache)
	}

	return mlwh.Open(ctx, cfg)
}

var openMLWHRunsRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhRunsClient, error) {
	return mlwh.NewRemoteClient(cfg)
}

// mlwhRunsClient is the subset of the MLWH query surface used by `wa mlwh runs`.
// Both *mlwh.RemoteClient and the local *mlwh.Client satisfy it.
type mlwhRunsClient interface {
	MonthlyRunCounts(ctx context.Context, opts mlwh.RunAggregationOptions) ([]mlwh.MonthlyRunCount, error)
	SequencingAggregate(ctx context.Context, opts mlwh.SequencingAggregateOptions) ([]mlwh.SequencingAggregateRow, error)
	RunListing(ctx context.Context, opts mlwh.RunAggregationOptions, limit int, cursor string) ([]mlwh.RunListingRow, error)
	CountRunListing(ctx context.Context, opts mlwh.RunAggregationOptions) (mlwh.Count, error)
	Close() error
}

func openMLWHRunsConfiguredClient(ctx context.Context, serverURL string) (mlwhRunsClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHRunsRemoteClient(ctx, mlwh.RemoteConfig{BaseURL: trimmedServerURL})
	}

	cfg, err := resolveMLWHInfoLocalConfig()
	if err != nil {
		return nil, err
	}

	client, err := openMLWHRunsClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.DSN) != "" && errors.Is(err, mlwh.ErrPasswordInDSN) {
			return nil, fmt.Errorf("WA_MLWH_DSN: %w", err)
		}

		return nil, err
	}

	return client, nil
}

func newMLWHRunsCommand() *cobra.Command {
	var (
		serverURL string
		monthly   bool
		since     string
		until     string
		platforms []string
		groupBy   []string
		unit      string
		limit     int
		cursor    string
		all       bool
	)

	command := &cobra.Command{
		Use:           "runs",
		Short:         "List global MLWH run aggregates",
		SilenceUsage:  true,
		SilenceErrors: false,
		Long: strings.Join([]string{
			"List global run aggregates through a wa mlwh serve API. In this",
			"phase, the default output is the flat global all-runs listing.",
			"Use --monthly to select monthly grouped run counts across platforms.",
			"Use --group-by to select the grouped sequencing aggregate; with",
			"--monthly, month is included automatically. The aggregate unit",
			"defaults to runs for CLI compatibility and can be set to samples or",
			"products with --unit.",
			"Rows are counted or listed at one run identifier: Illumina, Elembio",
			"and Ultimagen use distinct id_run, PacBio uses distinct",
			"pac_bio_run_name, and ONT uses distinct experiment_name. Flat run",
			"listing ids are stable composite keys such as illumina:52553,",
			"pacbio:TRACTION-RUN-1000, and ont:EXPERIMENT. ONT rows are included",
			"and labelled as warehouse load time - not a true sequencing date.",
			"",
			"Use --since and --until as YYYY-MM-DD or RFC3339 bounds over the",
			"platform-specific normalised run date; since is inclusive and until",
			"is exclusive. Repeat --platform to restrict runs to Illumina, PacBio,",
			"Elembio, Ultimagen or ONT. Without --all, the flat listing emits one",
			"bounded keyset page and reports total/next-cursor status. With --all,",
			"it follows each page's final composite id until the complete matching",
			"set has been emitted.",
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
			"Example:",
			"  wa --env development mlwh runs --since 2024-01-01 --platform PacBio --platform ONT",
			"  wa --env development mlwh runs --monthly --since 2024-01-01 --platform PacBio",
			"  wa --env development mlwh runs --monthly --group-by programme --platform PacBio --since 2024-01-01 --until 2025-01-01",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := openMLWHRunsConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			opts := mlwh.RunAggregationOptions{Since: since, Until: until, Platforms: platforms}
			if len(groupBy) > 0 || strings.TrimSpace(unit) != "" {
				if all || strings.TrimSpace(cursor) != "" {
					return errors.New("--all and --cursor are supported only for the flat run listing")
				}

				aggregateOpts := mlwh.SequencingAggregateOptions{
					GroupBy:   cliSequencingAggregateGroupBy(monthly, groupBy),
					Unit:      cliSequencingAggregateUnit(unit),
					Since:     since,
					Until:     until,
					Platforms: platforms,
				}

				return runMLWHRunsSequencingAggregate(cmd.Context(), client, cmd.OutOrStdout(), aggregateOpts)
			}
			if monthly {
				if all || strings.TrimSpace(cursor) != "" {
					return errors.New("--all and --cursor are supported only for the flat run listing")
				}

				return runMLWHRunsMonthly(cmd.Context(), client, cmd.OutOrStdout(), opts)
			}

			return runMLWHRunsListing(cmd.Context(), client, cmd.OutOrStdout(), opts, limit, cursor, all)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().BoolVar(&monthly, "monthly", false, "emit monthly grouped run counts")
	command.Flags().StringVar(&since, "since", "", "inclusive lower bound over the run date basis (YYYY-MM-DD or RFC3339)")
	command.Flags().StringVar(&until, "until", "", "exclusive upper bound over the run date basis (YYYY-MM-DD or RFC3339)")
	command.Flags().StringArrayVar(&platforms, "platform", nil, "restrict to a platform; repeat for multiple values")
	command.Flags().StringArrayVar(&groupBy, "group-by", nil, "aggregate grouping key; repeat or comma-separate month, platform, manufacturer, programme, faculty_sponsor")
	command.Flags().StringVar(&unit, "unit", "", "aggregate unit with --group-by: runs, samples, or products (default runs)")
	command.Flags().IntVar(&limit, "limit", mlwh.RunListingDefaultLimit, "maximum rows to return for a bounded page, or stream chunk size with --all")
	command.Flags().StringVar(&cursor, "cursor", "", "composite id from the previous page's last row")
	command.Flags().BoolVar(&all, "all", false, "emit the complete matching run listing instead of one bounded page")

	return command
}

func cliSequencingAggregateGroupBy(monthly bool, groupBy []string) []string {
	groups := make([]string, 0, len(groupBy)+1)
	if monthly {
		groups = append(groups, "month")
	}
	groups = append(groups, groupBy...)

	return groups
}

func cliSequencingAggregateUnit(unit string) string {
	if strings.TrimSpace(unit) == "" {
		return "runs"
	}

	return unit
}

func runMLWHRunsSequencingAggregate(ctx context.Context, client mlwhRunsClient, out io.Writer, opts mlwh.SequencingAggregateOptions) error {
	rows, err := client.SequencingAggregate(ctx, opts)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			_, _ = fmt.Fprintf(out, "%s\n", mlwhCacheUnavailableMessage)

			return nil
		}

		return fmt.Errorf("sequencing aggregate: %w", err)
	}

	writeSequencingAggregateRows(out, rows, opts.GroupBy)

	return nil
}

func writeSequencingAggregateRows(out io.Writer, rows []mlwh.SequencingAggregateRow, groupBy []string) {
	_, _ = fmt.Fprintln(out, "Sequencing aggregate:")
	if len(rows) == 0 {
		_, _ = fmt.Fprintf(out, "  %s\n", runsNoAggregateRowsMessage)

		return
	}

	for _, row := range rows {
		_, _ = fmt.Fprintf(out, "  %sunit=%s count=%d date_basis=%s cache_synced_at=%s\n",
			formatSequencingAggregateGroup(row.Group, groupBy),
			row.Unit,
			row.Count,
			row.DateBasis,
			row.CacheSyncedAt,
		)
	}
}

func formatSequencingAggregateGroup(group map[string]string, groupBy []string) string {
	var builder strings.Builder
	for _, key := range groupBy {
		if strings.TrimSpace(key) == "" {
			continue
		}
		normalized := strings.ToLower(strings.TrimSpace(key))
		_, _ = fmt.Fprintf(&builder, "%s=%s ", normalized, group[normalized])
	}

	return builder.String()
}

func runMLWHRunsMonthly(ctx context.Context, client mlwhRunsClient, out io.Writer, opts mlwh.RunAggregationOptions) error {
	rows, err := client.MonthlyRunCounts(ctx, opts)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			_, _ = fmt.Fprintf(out, "%s\n", mlwhCacheUnavailableMessage)

			return nil
		}
		if errors.Is(err, mlwh.ErrUnsupportedIdentifier) {
			return fmt.Errorf("monthly runs: %w", err)
		}

		return fmt.Errorf("monthly runs: %w", err)
	}

	writeMonthlyRunCounts(out, rows)

	return nil
}

func writeMonthlyRunCounts(out io.Writer, rows []mlwh.MonthlyRunCount) {
	_, _ = fmt.Fprintln(out, "Monthly runs:")
	if len(rows) == 0 {
		_, _ = fmt.Fprintf(out, "  %s\n", runsNoMonthlyRowsMessage)

		return
	}

	for _, row := range rows {
		_, _ = fmt.Fprintf(
			out,
			"  month=%s manufacturer=%s platform=%s count=%d date_basis=%s cache_synced_at=%s\n",
			row.Month,
			row.Manufacturer,
			row.Platform,
			row.Count,
			row.DateBasis,
			row.CacheSyncedAt,
		)
	}
}

func runMLWHRunsListing(ctx context.Context, client mlwhRunsClient, out io.Writer, opts mlwh.RunAggregationOptions, limit int, cursor string, all bool) error {
	if all && strings.TrimSpace(cursor) != "" {
		return errors.New("--cursor cannot be combined with --all")
	}
	if limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}
	if limit > mlwh.RunListingMaxLimit {
		return fmt.Errorf("--limit must not exceed %d", mlwh.RunListingMaxLimit)
	}

	total, err := client.CountRunListing(ctx, opts)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			_, _ = fmt.Fprintf(out, "%s\n", mlwhCacheUnavailableMessage)

			return nil
		}

		return fmt.Errorf("list runs: %w", err)
	}

	_, _ = fmt.Fprintln(out, "Runs:")
	if all {
		return runMLWHRunsListingAll(ctx, client, out, opts, limit, total.Count)
	}

	rows, err := client.RunListing(ctx, opts, limit, cursor)
	if err != nil {
		return fmt.Errorf("list runs: %w", err)
	}
	writeRunListingRows(out, rows)
	writeRunListingBoundedStatus(out, rows, total.Count)

	return nil
}

func writeRunListingRows(out io.Writer, rows []mlwh.RunListingRow) {
	for _, row := range rows {
		_, _ = fmt.Fprintf(
			out,
			"  id=%s platform=%s native_id=%s manufacturer=%s run_date=%s date_basis=%s cache_synced_at=%s\n",
			row.ID,
			row.Platform,
			row.NativeID,
			row.Manufacturer,
			row.RunDate,
			row.DateBasis,
			row.CacheSyncedAt,
		)
	}
}

func writeRunListingBoundedStatus(out io.Writer, rows []mlwh.RunListingRow, total int) {
	if len(rows) == 0 {
		_, _ = fmt.Fprintf(out, "  %s\n", runsNoListingRowsMessage)
		_, _ = fmt.Fprintf(out, "bounded page emitted: rows=0 total=%d next_cursor=<none>\n", total)

		return
	}

	next := "<none>"
	if len(rows) < total {
		next = rows[len(rows)-1].ID
	}
	_, _ = fmt.Fprintf(out, "bounded page emitted: rows=%d total=%d next_cursor=%s\n", len(rows), total, next)
}

func runMLWHRunsListingAll(ctx context.Context, client mlwhRunsClient, out io.Writer, opts mlwh.RunAggregationOptions, limit, total int) error {
	var (
		cursor  string
		emitted int
	)
	for {
		rows, err := client.RunListing(ctx, opts, limit, cursor)
		if err != nil {
			return fmt.Errorf("list runs: %w", err)
		}
		writeRunListingRows(out, rows)
		emitted += len(rows)
		if len(rows) < limit {
			break
		}
		cursor = rows[len(rows)-1].ID
	}
	if emitted == 0 {
		_, _ = fmt.Fprintf(out, "  %s\n", runsNoListingRowsMessage)
	}
	if emitted != total {
		return fmt.Errorf("list runs: count mismatch: count=%d rows=%d", total, emitted)
	}
	_, _ = fmt.Fprintf(out, "complete set emitted: rows=%d total=%d\n", emitted, total)

	return nil
}
