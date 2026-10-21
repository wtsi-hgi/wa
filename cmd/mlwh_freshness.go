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
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
)

// freshnessCheckErrorMaxRunes caps the error text in a --check problem line.
const freshnessCheckErrorMaxRunes = 200

// freshnessListErrorMaxRunes caps the error text in the default table.
const freshnessListErrorMaxRunes = 80

// errMLWHFreshnessStatusUnsupported reports a server older than the API
// version that added sync statuses to /freshness.
var errMLWHFreshnessStatusUnsupported = errors.New(
	"the MLWH server does not report sync status; it needs MLWH API 1.12.0 or later")

var openMLWHFreshnessRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhFreshnessCommandClient, error) {
	return mlwh.NewRemoteClient(cfg)
}

type mlwhFreshnessCommandClient interface {
	Freshness(ctx context.Context) (mlwh.Freshness, error)
	Close() error
}

// openMLWHFreshnessConfiguredClient opens the server at serverURL or, without
// one, the local cache at WA_MLWH_CACHE_PATH, which never needs the source.
func openMLWHFreshnessConfiguredClient(ctx context.Context, serverURL string) (mlwhFreshnessCommandClient, error) {
	if trimmed := strings.TrimSpace(serverURL); trimmed != "" {
		return openMLWHFreshnessRemoteClient(ctx, mlwhCLIRemoteConfig(trimmed))
	}

	if strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH")) == "" {
		return nil, errors.New("WA_MLWH_SERVER_URL or WA_MLWH_CACHE_PATH must be set; pass --server to use a remote wa mlwh serve instance")
	}

	staleAfter, err := resolveMLWHStaleAfter()
	if err != nil {
		return nil, err
	}

	cacheConfig, err := resolveMLWHServeCacheConfig("", false)
	if err != nil {
		return nil, err
	}

	client, err := mlwh.OpenCacheOnly(ctx, cacheConfig)
	if err != nil {
		return nil, err
	}

	client.SetStaleAfter(staleAfter)

	return client, nil
}

func newMLWHFreshnessCommand() *cobra.Command {
	var (
		serverURL string
		check     bool
		jsonOut   bool
	)

	command := &cobra.Command{
		Use:   "freshness",
		Args:  cobra.ExactArgs(0),
		Short: "Report MLWH cache sync status per table",
		Long: strings.Join([]string{
			"Report, for every mirrored MLWH table, its sync status and when its",
			"sync last succeeded and failed, from GET /freshness.",
			"",
			"A table is failing when its most recent sync failed or was skipped",
			"because a table it depends on failed, never_synced when it has",
			"neither succeeded nor synced any data, stale when its last success",
			"is older than the stale threshold, and ok otherwise. The overall",
			"status is the worst table status. The threshold is",
			"WA_MLWH_STALE_AFTER (a Go duration, default 2h) where the status is",
			"computed: on the server for --server, else locally.",
			"",
			"With --check, print nothing and exit 0 when the overall status is ok;",
			"otherwise print one line per table needing attention to stderr and",
			"exit 1, with tables sharing a status and error on one line. It also",
			"exits 1 when the server cannot be reached or is too old to report",
			"sync status. Run it from cron to mail on problems. A failed or",
			"interrupted sync reports its tables failing until their next",
			"successful sync.",
			"",
			"With no MLWH server configured, wa reads the local cache at",
			"WA_MLWH_CACHE_PATH.",
			"",
			mlwhQueryCommandConfigurationHelp,
			"",
			"Example:",
			"  */30 * * * * WA_MLWH_SERVER_URL=https://apps.hgi.sanger.ac.uk/mlwh-api /software/hgi/installs/wa/wa mlwh freshness --check",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := openMLWHFreshnessConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			freshness, err := client.Freshness(cmd.Context())
			if err != nil {
				return fmt.Errorf("read mlwh freshness: %w", err)
			}

			switch {
			case jsonOut:
				return writeMLWHFreshnessJSON(cmd.OutOrStdout(), freshness)
			case check:
				return checkMLWHFreshness(cmd.ErrOrStderr(), freshness)
			default:
				return writeMLWHFreshnessTable(cmd.OutOrStdout(), freshness)
			}
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().BoolVar(&check, "check", false, "exit 1, listing problem tables on stderr, unless the overall status is ok")
	command.Flags().BoolVar(&jsonOut, "json", false, "emit the freshness document as JSON")
	command.MarkFlagsMutuallyExclusive("check", "json")

	return command
}

func writeMLWHFreshnessJSON(stdout io.Writer, freshness mlwh.Freshness) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(freshness)
}

// checkMLWHFreshness writes the tables that are not ok to stderr and returns
// an error summarising them, or returns nil, writing nothing, when the overall
// status is ok. Tables sharing a status, blockers and error, such as every
// table after an unreachable source, share one line to keep cron mail short.
func checkMLWHFreshness(stderr io.Writer, freshness mlwh.Freshness) error {
	if freshness.Status == "" {
		return errMLWHFreshnessStatusUnsupported
	}

	if freshness.Status == mlwh.FreshnessStatusOK {
		return nil
	}

	groups := groupMLWHFreshnessProblems(freshness.Tables)

	problems := 0
	for _, group := range groups {
		problems += len(group)

		_, _ = fmt.Fprintln(stderr, mlwhFreshnessProblemLine(group))
	}

	return fmt.Errorf("mlwh sync status %s: %d of %d tables need attention",
		freshness.Status, problems, len(freshness.Tables))
}

// groupMLWHFreshnessProblems returns the tables that are not ok, grouped by
// status, blockers and error in order of each group's first table. Tables
// without an error each form their own group, since their last success times
// tell them apart.
func groupMLWHFreshnessProblems(tables []mlwh.TableFreshness) [][]mlwh.TableFreshness {
	var groups [][]mlwh.TableFreshness

	index := map[string]int{}

	for _, table := range tables {
		if table.Status == mlwh.FreshnessStatusOK {
			continue
		}

		if table.LastError == "" {
			groups = append(groups, []mlwh.TableFreshness{table})

			continue
		}

		key := strings.Join([]string{table.Status, strings.Join(table.BlockedBy, ","),
			firstLineCapped(table.LastError, freshnessCheckErrorMaxRunes)}, "\x00")

		if at, ok := index[key]; ok {
			groups[at] = append(groups[at], table)

			continue
		}

		index[key] = len(groups)
		groups = append(groups, []mlwh.TableFreshness{table})
	}

	return groups
}

// mlwhFreshnessProblemLine describes one group of problem tables: a single
// table with its last success, or several by name with their shared error.
func mlwhFreshnessProblemLine(group []mlwh.TableFreshness) string {
	table := group[0]

	var line string
	if len(group) == 1 {
		lastSuccess := table.LastSuccessAt
		if lastSuccess == "" {
			lastSuccess = "never"
		}

		line = fmt.Sprintf("%s: %s, last success %s", table.Table, table.Status, lastSuccess)
	} else {
		names := make([]string, len(group))
		for i, member := range group {
			names[i] = member.Table
		}

		line = fmt.Sprintf("%d tables %s (%s)", len(group), table.Status, strings.Join(names, ", "))
	}

	if len(table.BlockedBy) > 0 {
		line += ", blocked by " + strings.Join(table.BlockedBy, ", ")
	}

	if table.LastError != "" {
		line += ": " + firstLineCapped(table.LastError, freshnessCheckErrorMaxRunes)
	}

	return line
}

// firstLineCapped returns text's first line, cut to maxRunes runes.
func firstLineCapped(text string, maxRunes int) string {
	line, _, _ := strings.Cut(text, "\n")

	runes := []rune(line)
	if len(runes) <= maxRunes {
		return line
	}

	return string(runes[:maxRunes]) + "..."
}

func writeMLWHFreshnessTable(stdout io.Writer, freshness mlwh.Freshness) error {
	overall := freshness.Status
	if overall == "" {
		overall = "unknown (server needs MLWH API 1.12.0 or later)"
	}

	if _, err := fmt.Fprintf(stdout, "Overall: %s (stale after %s)\n", overall,
		time.Duration(freshness.StaleAfterSeconds)*time.Second); err != nil {
		return err
	}

	writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "TABLE\tSTATUS\tLAST SUCCESS\tLAST RUN\tLAST ERROR")

	for _, table := range freshness.Tables {
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", table.Table, dashIfEmpty(table.Status),
			dashIfEmpty(table.LastSuccessAt), dashIfEmpty(table.LastRun),
			dashIfEmpty(firstLineCapped(table.LastError, freshnessListErrorMaxRunes)))
	}

	return writer.Flush()
}

func dashIfEmpty(value string) string {
	if value == "" {
		return "-"
	}

	return value
}

// resolveMLWHStaleAfter parses WA_MLWH_STALE_AFTER as a positive Go duration,
// returning 0 (the mlwh default) when it is unset.
func resolveMLWHStaleAfter() (time.Duration, error) {
	raw := strings.TrimSpace(firstEnv("WA_MLWH_STALE_AFTER"))
	if raw == "" {
		return 0, nil
	}

	staleAfter, err := time.ParseDuration(raw)
	if err != nil || staleAfter <= 0 {
		return 0, fmt.Errorf("invalid WA_MLWH_STALE_AFTER %q: want a positive duration such as 2h or 90m", raw)
	}

	return staleAfter, nil
}
