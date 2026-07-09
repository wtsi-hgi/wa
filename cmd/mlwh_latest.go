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

const (
	latestDefaultLimit = 10
	latestNoData       = "no data"
)

var openMLWHLatestClient = func(ctx context.Context, cfg mlwh.Config) (mlwhLatestClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return mlwh.OpenCacheOnly(ctx, cfg.Cache)
	}

	return mlwh.Open(ctx, cfg)
}

var openMLWHLatestRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhLatestClient, error) {
	return mlwh.NewRemoteClient(cfg)
}

type mlwhLatestClient interface {
	LatestDataForStudy(ctx context.Context, studyLimsID, fileType string, limit, offset int) ([]mlwh.RecentDataRow, error)
	LatestDataForFacultySponsor(ctx context.Context, name, fileType string, limit, offset int) ([]mlwh.RecentDataRow, error)
	Close() error
}

func openMLWHLatestConfiguredClient(ctx context.Context, serverURL string) (mlwhLatestClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHLatestRemoteClient(ctx, mlwh.RemoteConfig{BaseURL: trimmedServerURL})
	}

	cfg, err := resolveMLWHInfoLocalConfig()
	if err != nil {
		return nil, err
	}

	client, err := openMLWHLatestClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.DSN) != "" && errors.Is(err, mlwh.ErrPasswordInDSN) {
			return nil, fmt.Errorf("WA_MLWH_DSN: %w", err)
		}

		return nil, err
	}

	return client, nil
}

func newMLWHLatestCommand() *cobra.Command {
	var (
		serverURL      string
		facultySponsor string
		fileType       string
		limit          int
		offset         int
	)

	command := &cobra.Command{
		Use:           "latest <study-id>",
		Short:         "List the newest iRODS data rows for a study or faculty sponsor",
		SilenceUsage:  true,
		SilenceErrors: false,
		Long: strings.Join([]string{
			"List newest raw iRODS data rows from the local MLWH mirror, ordered",
			"by created time descending with stable id_run/id_product tie-breaks.",
			"Pass a study LIMS id positionally, or use --faculty-sponsor NAME to",
			"merge the newest bounded rows across all SQSCP studies whose named",
			"faculty_sponsor contains NAME. Add --file-type cram to restrict rows",
			"to paths whose file name ends with .cram.",
			"",
			mlwhQueryCommandConfigurationHelp,
			"",
			"Examples:",
			"  wa --env development mlwh latest 5901 --file-type cram",
			"  wa mlwh latest --faculty-sponsor Anderson --limit 25 --server http://host:8091",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			study, sponsor, err := parseLatestSelector(args, facultySponsor)
			if err != nil {
				return err
			}
			if err := validateLatestFileTypeFlag(fileType); err != nil {
				return err
			}
			if err := validateLatestPagination(limit, offset); err != nil {
				return err
			}

			client, err := openMLWHLatestConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			return runMLWHLatest(cmd.Context(), client, cmd.OutOrStdout(), latestSelector{study: study, sponsor: sponsor}, fileType, limit, offset)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().StringVar(&facultySponsor, "faculty-sponsor", "", "list latest data across studies whose faculty_sponsor contains this name")
	command.Flags().StringVar(&fileType, "file-type", "", "restrict rows to this filename suffix (a leading dot is stripped), e.g. cram")
	command.Flags().IntVar(&limit, "limit", latestDefaultLimit, "maximum number of newest rows to return")
	command.Flags().IntVar(&offset, "offset", 0, "number of newest rows to skip (for pagination)")

	return command
}

func parseLatestSelector(args []string, facultySponsor string) (string, string, error) {
	study := ""
	if len(args) > 0 {
		study = strings.TrimSpace(args[0])
	}
	sponsor := strings.TrimSpace(facultySponsor)

	if len(args) > 1 || (study == "" && sponsor == "") || (study != "" && sponsor != "") {
		return "", "", errors.New("usage: wa mlwh latest <study-id> or wa mlwh latest --faculty-sponsor NAME")
	}

	return study, sponsor, nil
}

func validateLatestFileTypeFlag(fileType string) error {
	if fileType == "" {
		return nil
	}

	normalised := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(fileType), "."))
	if normalised == "" || strings.ContainsAny(normalised, `%_/`) {
		return latestInvalidFileTypeError(fileType)
	}

	return nil
}

func latestInvalidFileTypeError(fileType string) error {
	return fmt.Errorf("invalid --file-type %q: a filename suffix may not be empty or contain '%%', '_' or '/'", fileType)
}

func validateLatestPagination(limit, offset int) error {
	if limit < 0 || offset < 0 {
		return errors.New("limit and offset must be non-negative")
	}

	return nil
}

func runMLWHLatest(ctx context.Context, client mlwhLatestClient, out io.Writer, selector latestSelector, fileType string, limit, offset int) error {
	rows, err := latestRows(ctx, client, selector, fileType, limit, offset)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			_, _ = fmt.Fprintf(out, "%s\n", mlwhCacheUnavailableMessage)

			return nil
		}
		if errors.Is(err, mlwh.ErrUnsupportedIdentifier) && fileType != "" {
			return latestInvalidFileTypeError(fileType)
		}

		return fmt.Errorf("latest data: %w", err)
	}

	writeLatestDataRows(out, rows)

	return nil
}

func writeLatestDataRows(out io.Writer, rows []mlwh.RecentDataRow) {
	_, _ = fmt.Fprintln(out, "Latest data:")
	if len(rows) == 0 {
		_, _ = fmt.Fprintf(out, "  %s\n", latestNoData)

		return
	}

	for _, row := range rows {
		_, _ = fmt.Fprintf(
			out,
			"  created=%s id_study_lims=%s study_name=%s name=%s supplier_name=%s id_run=%d lane=%d tag_index=%d platform=%s merged=%t irods_path=%s\n",
			row.Created,
			row.IDStudyLims,
			row.StudyName,
			row.Name,
			row.SupplierName,
			row.IDRun,
			row.Position,
			row.TagIndex,
			row.Platform,
			row.Merged,
			row.IRODSPath,
		)
	}
}

func latestRows(ctx context.Context, client mlwhLatestClient, selector latestSelector, fileType string, limit, offset int) ([]mlwh.RecentDataRow, error) {
	if selector.sponsor != "" {
		return client.LatestDataForFacultySponsor(ctx, selector.sponsor, fileType, limit, offset)
	}

	return client.LatestDataForStudy(ctx, selector.study, fileType, limit, offset)
}

type latestSelector struct {
	study   string
	sponsor string
}
