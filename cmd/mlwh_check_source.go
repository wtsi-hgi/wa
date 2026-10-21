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
	"time"

	"github.com/spf13/cobra"

	"github.com/wtsi-hgi/wa/mlwh"
)

var (
	openMLWHCheckSource    = mlwh.OpenSource
	mlwhCheckSourceQueries = mlwh.AllSyncSourceQueries

	// mlwhCheckSourceConnectTimeout bounds connecting to and pinging the MLWH
	// source, so an unreachable server fails the check instead of hanging it.
	mlwhCheckSourceConnectTimeout = 10 * time.Second
)

// mlwhCheckSourceReport is the --json output of wa mlwh check-source.
type mlwhCheckSourceReport struct {
	OK      bool                    `json:"ok"`
	Queries []mlwh.SourceQueryCheck `json:"queries"`
}

func reportMLWHCheckSource(out io.Writer, checks []mlwh.SourceQueryCheck, jsonOutput bool) error {
	failed := 0
	for _, check := range checks {
		if !check.OK {
			failed++
		}
	}

	if jsonOutput {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(mlwhCheckSourceReport{OK: failed == 0, Queries: checks}); err != nil {
			return fmt.Errorf("write check-source report: %w", err)
		}
	} else {
		writeMLWHCheckSourceLines(out, checks, failed)
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d sync source queries failed to prepare against the MLWH source", failed, len(checks))
	}

	return nil
}

func writeMLWHCheckSourceLines(out io.Writer, checks []mlwh.SourceQueryCheck, failed int) {
	for _, check := range checks {
		if check.OK {
			_, _ = fmt.Fprintf(out, "ok    %s\n", check.Name)

			continue
		}

		_, _ = fmt.Fprintf(out, "FAIL  %s: %s\n", check.Name, check.Error)
	}

	if failed == 0 {
		_, _ = fmt.Fprintf(out, "all %d sync source queries prepared against the MLWH source\n", len(checks))
	}
}

func newMLWHCheckSourceCommand() *cobra.Command {
	var jsonOutput bool

	command := &cobra.Command{
		Use:   "check-source",
		Short: "Check that every sync query is valid against the upstream MLWH schema",
		Args:  cobra.NoArgs,
		Long: strings.Join([]string{
			"Check that the upstream MLWH schema still supports every query",
			"'wa mlwh sync' sends to it.",
			"",
			"Each sync source query is PREPAREd and closed without being",
			"executed, so the MLWH server validates every table and column the",
			"query names while no rows are read and nothing is changed. Run it",
			"before and after an upstream MLWH schema release: a missing table",
			"or column fails the matching query with the server's error.",
			"",
			"Each query is printed as 'ok' or 'FAIL' with its error. The exit",
			"code is non-zero when any query fails. --json prints a single",
			"report object instead.",
			"",
			"It connects with WA_MLWH_DSN and WA_MLWH_PASSWORD, as 'wa mlwh",
			"sync' does, and does not open the cache. For configuration,",
			"see `wa mlwh -h`.",
			"",
			"Examples:",
			"  wa --env production mlwh check-source",
			"  wa --env production mlwh check-source --json",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn := firstEnv("WA_MLWH_DSN")
			if dsn == "" {
				return errors.New("WA_MLWH_DSN must be set")
			}

			connectCtx, cancel := context.WithTimeout(cmd.Context(), mlwhCheckSourceConnectTimeout)
			source, err := openMLWHCheckSource(connectCtx, dsn, firstEnv("WA_MLWH_PASSWORD"))
			cancel()
			if err != nil {
				if errors.Is(err, mlwh.ErrPasswordInDSN) {
					return fmt.Errorf("WA_MLWH_DSN: %w", err)
				}

				return fmt.Errorf("open mlwh source: %w", err)
			}
			defer func() { _ = source.Close() }()

			checks := mlwh.PrepareSyncSourceQueries(cmd.Context(), source, mlwhCheckSourceQueries())

			return reportMLWHCheckSource(cmd.OutOrStdout(), checks, jsonOutput)
		},
	}

	command.Flags().BoolVar(&jsonOutput, "json", false, "print one JSON report instead of a line per query")

	return command
}
