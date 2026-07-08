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
	mlwhExportDefaultLimit = 50

	mlwhExportFormatTSV  = "tsv"
	mlwhExportFormatCSV  = "csv"
	mlwhExportFormatJSON = "json"
)

var openMLWHExportClient = func(ctx context.Context, cfg mlwh.Config) (mlwhExportClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return mlwh.OpenCacheOnly(ctx, cfg.Cache)
	}

	return mlwh.Open(ctx, cfg)
}

var openMLWHExportRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhExportClient, error) {
	remote, err := mlwh.NewRemoteClient(cfg)
	if err != nil {
		return nil, err
	}

	return &mlwhExportRemoteClient{remote: remote}, nil
}

type mlwhExportClient interface {
	Export(ctx context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error)
	Close() error
}

func openMLWHExportConfiguredClient(ctx context.Context, serverURL string) (mlwhExportClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHExportRemoteClient(ctx, mlwh.RemoteConfig{BaseURL: trimmedServerURL})
	}

	cfg, err := resolveMLWHInfoLocalConfig()
	if err != nil {
		return nil, err
	}

	client, err := openMLWHExportClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.DSN) != "" && errors.Is(err, mlwh.ErrPasswordInDSN) {
			return nil, fmt.Errorf("WA_MLWH_DSN: %w", err)
		}

		return nil, err
	}

	return client, nil
}

func newMLWHExportCommand() *cobra.Command {
	flags := mlwhExportFlags{limit: mlwhExportDefaultLimit, format: mlwhExportFormatTSV}

	command := &cobra.Command{
		Use:           "export <children> <parent-kind> <parent-id>",
		Short:         "Export MLWH relationship rows with selectable columns",
		SilenceUsage:  true,
		SilenceErrors: false,
		Long: strings.Join([]string{
			"Export children of an MLWH parent through a local cache or wa mlwh",
			"serve API. The grammar is 'wa mlwh export <children> <parent-kind>",
			"<parent-id>'. Relationships include files of a study, sample or run;",
			"samples of a study, run or library; runs of a study or sample;",
			"libraries of a study; lanes of a sample; studies of a sample,",
			"faculty-sponsor, user or programme; users of a study; and",
			"sample-crams of a study.",
			"",
			"Use --columns for an ordered comma-separated projection and --format",
			"tsv, csv or json. --json is shorthand for --format json. CRAM",
			"exports (irods/files/sample-crams) default to deliverables-only;",
			"--include-controls disables that default. sample-crams still selects",
			"one merged-aware CRAM per sample. Without --all the command emits one",
			"bounded page and prints",
			"total/next-cursor status to stderr. With --all it streams the complete",
			"set and states that explicitly, so truncation is never silent.",
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
			"  wa --env development mlwh export irods study 5901 --file-type cram",
			"  wa mlwh export runs sample DN1234 --columns id_run,platform,run_date",
			"  wa mlwh export irods study 5901 --all --server http://host:8091 --json",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			rel, parentID, err := parseMLWHExportArgs(args)
			if err != nil {
				return err
			}

			opts, err := flags.options(rel, cmd)
			if err != nil {
				return err
			}

			client, err := openMLWHExportConfiguredClient(cmd.Context(), flags.serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			return runMLWHExport(cmd.Context(), client, cmd.OutOrStdout(), cmd.ErrOrStderr(), rel, parentID, opts)
		},
	}

	command.Flags().StringVar(&flags.serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().StringVar(&flags.columns, "columns", "", "ordered comma-separated columns to emit")
	command.Flags().StringVar(&flags.format, "format", mlwhExportFormatTSV, "output format: tsv, csv or json")
	command.Flags().BoolVar(&flags.jsonOut, "json", false, "emit JSON output (shorthand for --format json)")
	command.Flags().StringVar(&flags.fileType, "file-type", "", "restrict file exports to data objects whose filename ends in this suffix; file exports default to cram")
	command.Flags().BoolVar(&flags.deliverablesOnly, "deliverables-only", false, "restrict cram file exports to deliverable rows")
	command.Flags().BoolVar(&flags.includeControls, "include-controls", false, "include controls/sub-products in cram file exports")
	command.Flags().StringVar(&flags.role, "role", "", "restrict study_users-backed exports to comma-separated roles")
	command.Flags().StringVar(&flags.qc, "qc", "", "restrict product-backed exports by QC: pass, fail or pending")
	command.Flags().StringVar(&flags.libraryType, "library-type", "", "restrict sample-backed exports by library type")
	command.Flags().StringVar(&flags.organism, "organism", "", "restrict sample-backed exports by organism/common name")
	command.Flags().StringVar(&flags.sort, "sort", "", "export sort order (supported value: created-desc)")
	command.Flags().StringVar(&flags.since, "since", "", "inclusive RFC3339 lower bound for created-date exports")
	command.Flags().StringVar(&flags.until, "until", "", "exclusive RFC3339 upper bound for created-date exports")
	command.Flags().StringVar(&flags.cursor, "cursor", "", "opaque next_cursor from a previous canonical-order iRODS export page")
	command.Flags().IntVar(&flags.limit, "limit", mlwhExportDefaultLimit, "maximum rows to return for a bounded page, or stream chunk size with --all")
	command.Flags().IntVar(&flags.offset, "offset", 0, "number of rows to skip for bounded limit/offset paging")
	command.Flags().BoolVar(&flags.all, "all", false, "emit the complete matching set instead of a bounded page")

	return command
}

func parseMLWHExportArgs(args []string) (mlwh.ExportRelationship, string, error) {
	if len(args) != 3 {
		return mlwh.ExportRelationship{}, "", errors.New("usage: wa mlwh export <children> <parent-kind> <parent-id>")
	}

	children := strings.ToLower(strings.TrimSpace(args[0]))
	parentKind := strings.ToLower(strings.TrimSpace(args[1]))
	parentID := strings.TrimSpace(args[2])
	if children == "" || parentKind == "" || parentID == "" {
		return mlwh.ExportRelationship{}, "", errors.New("usage: wa mlwh export <children> <parent-kind> <parent-id>")
	}

	return mlwh.ExportRelationship{Children: children, ParentKind: parentKind}, parentID, nil
}

func runMLWHExport(
	ctx context.Context,
	client mlwhExportClient,
	dataOut io.Writer,
	statusOut io.Writer,
	rel mlwh.ExportRelationship,
	parentID string,
	opts mlwh.ExportOptions,
) error {
	result, err := client.Export(ctx, rel, parentID, opts)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			return writeMLWHExportCacheUnavailable(dataOut, statusOut, opts.Format)
		}
		if errors.Is(err, mlwh.ErrNotFound) {
			if mlwhClientNeverSynced(ctx, client) {
				return writeMLWHExportCacheUnavailable(dataOut, statusOut, opts.Format)
			}

			writeMLWHExportNotFound(statusOut, rel, parentID)

			return nil
		}

		return fmt.Errorf("export %s of %s %q: %w", rel.Children, rel.ParentKind, parentID, err)
	}

	rows, err := result.RenderAsTo(ctx, dataOut, opts.Format)
	if err != nil {
		return fmt.Errorf("render export: %w", err)
	}

	writeMLWHExportStatus(statusOut, rows, result, opts.All)

	return nil
}

func writeMLWHExportCacheUnavailable(dataOut io.Writer, statusOut io.Writer, format string) error {
	if format == mlwhExportFormatJSON {
		if _, err := io.WriteString(dataOut, "[]\n"); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintf(statusOut, "%s\n", mlwhCacheUnavailableMessage)

	return nil
}

func writeMLWHExportNotFound(out io.Writer, rel mlwh.ExportRelationship, parentID string) {
	_, _ = fmt.Fprintf(out, "not found: %s %q for export %s\n", rel.ParentKind, parentID, rel.Children)
}

func writeMLWHExportStatus(out io.Writer, rows int, result mlwh.ExportResult, all bool) {
	if all {
		_, _ = fmt.Fprintf(out, "complete set emitted: rows=%d\n", rows)

		return
	}

	next := result.NextCursor
	if next == "" {
		next = "<none>"
	}

	_, _ = fmt.Fprintf(out, "bounded page emitted: rows=%d total=%d next_cursor=%s\n", rows, result.Total, next)
}

type mlwhExportRemoteClient struct {
	remote *mlwh.RemoteClient
}

func (c *mlwhExportRemoteClient) Export(ctx context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
	return c.remote.Export(ctx, rel, parentID, opts)
}

func (c *mlwhExportRemoteClient) Freshness(ctx context.Context) (mlwh.Freshness, error) {
	return c.remote.Freshness(ctx)
}

func (c *mlwhExportRemoteClient) Close() error {
	if c == nil || c.remote == nil {
		return nil
	}

	return c.remote.Close()
}

type mlwhExportFlags struct {
	serverURL        string
	columns          string
	format           string
	jsonOut          bool
	fileType         string
	deliverablesOnly bool
	includeControls  bool
	role             string
	qc               string
	libraryType      string
	organism         string
	sort             string
	since            string
	until            string
	cursor           string
	limit            int
	offset           int
	all              bool
}

func (f mlwhExportFlags) options(rel mlwh.ExportRelationship, cmd *cobra.Command) (mlwh.ExportOptions, error) {
	columns, err := splitMLWHExportColumns(f.columns)
	if err != nil {
		return mlwh.ExportOptions{}, err
	}

	deliverablesOnly, err := mlwhExportDeliverablesFlag(cmd, f.deliverablesOnly, f.includeControls)
	if err != nil {
		return mlwh.ExportOptions{}, err
	}

	format := strings.ToLower(strings.TrimSpace(f.format))
	if f.jsonOut {
		format = mlwhExportFormatJSON
	}

	return mlwh.ExportOptions{
		Columns:          columns,
		FileType:         mlwhExportFileTypeDefault(rel, f.fileType, cmd.Flags().Changed("file-type")),
		DeliverablesOnly: deliverablesOnly,
		Role:             f.role,
		QC:               f.qc,
		LibraryType:      f.libraryType,
		Organism:         f.organism,
		Sort:             f.sort,
		Since:            f.since,
		Until:            f.until,
		Limit:            f.limit,
		Offset:           f.offset,
		All:              f.all,
		Cursor:           f.cursor,
		Format:           format,
	}, nil
}

func splitMLWHExportColumns(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	parts := strings.Split(trimmed, ",")
	columns := make([]string, 0, len(parts))
	for _, part := range parts {
		column := strings.TrimSpace(part)
		if column == "" {
			return nil, errors.New("--columns contains an empty column name")
		}
		columns = append(columns, column)
	}

	return columns, nil
}

func mlwhExportDeliverablesFlag(cmd *cobra.Command, deliverablesOnly, includeControls bool) (*bool, error) {
	deliverablesChanged := cmd.Flags().Changed("deliverables-only")
	includeChanged := cmd.Flags().Changed("include-controls")
	if deliverablesChanged && includeChanged {
		return nil, errors.New("--deliverables-only and --include-controls cannot be used together")
	}
	if includeChanged {
		value := false

		return &value, nil
	}
	if deliverablesChanged {
		value := deliverablesOnly

		return &value, nil
	}

	return nil, nil
}

func mlwhExportFileTypeDefault(rel mlwh.ExportRelationship, fileType string, changed bool) string {
	if changed {
		return fileType
	}
	if mlwhExportUsesFileType(rel.Children) {
		return "cram"
	}

	return ""
}

func mlwhExportUsesFileType(children string) bool {
	switch strings.ToLower(strings.TrimSpace(children)) {
	case "irods", "files", "sample-crams":
		return true
	default:
		return false
	}
}
