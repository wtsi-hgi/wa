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
	mlwhExportFormatTSV  = "tsv"
	mlwhExportFormatCSV  = "csv"
	mlwhExportFormatJSON = "json"
)

const mlwhExportIntroHelp = `Export one kind of MLWH child row for one MLWH parent through a local
cache or wa mlwh serve API.

Grammar:
  wa mlwh export <children> <parent-kind> <parent-id>

The first two words choose the relationship. The third word is the parent
identifier or text value interpreted according to parent-kind. Output is TSV by
default, or CSV/JSON with --format. Use --columns for an ordered comma-separated
set of columns. The command writes only export data to stdout, so it is safe to
redirect to a file.`

const mlwhExportParentIDHelp = `Parent kinds and parent-id values:
  study: a study LIMS id, study UUID, accession number or study name
  sample: a sample UUID, LIMS id, Sanger sample name/id, supplier name, accession or donor id
  run: an Illumina NPG id_run
  library: a pipeline_id_lims, library_id, id_library_lims or library type
  faculty-sponsor: a case-insensitive faculty_sponsor substring
  user: a study_users name, login or email substring
  programme: an exact programme value`

const mlwhExportOptionsHelp = `File exports:
  iRODS/files and sample-crams use --file-type as a filename suffix filter. A
  single leading dot is stripped, so cram, .cram and CRAM are equivalent. If no
  --file-type is supplied, file exports default to cram.

Deliverables:
  CRAM file exports default to deliverables-only. In MLWH terms, deliverable uses
  iseq_flowcell.entity_type IN ('library','library_indexed') for Illumina and
  Element/Ultima control flags where available, approximates iRODS target=1, and
  is not is_spiked. PacBio/ONT rows without that discriminator pass through.
  Use --include-controls to disable the default, or --deliverables-only to make
  the filter explicit. sample-crams still selects one merged-aware CRAM per
  sample.

Filters:
  --qc pass|fail|pending, --library-type and --organism apply to sample-backed
  iRODS/files, samples and sample-crams exports. --role narrows study_users-backed
  studies/users exports.

Sorting and date windows:
  --sort created-desc is the only supported explicit sort because the default
  iRODS order is run/lane/tag/file order. Use created-desc when you want newest
  data first or a created-date window. --since is inclusive and --until is
  exclusive; both use an RFC3339 timestamp such as
  2026-07-01T00:00:00Z.

Completeness:
  Exports include every matching row. There are no paging flags and no success
  message is appended after the data.`

const mlwhExportExamplesHelp = `Examples:
  wa mlwh export sample-crams study 5901
  wa --env development mlwh export irods study 5901 --file-type cram
  wa mlwh export runs sample DN1234 --columns id_run,platform,run_date
  wa mlwh export irods study 5901 --sort created-desc --since 2026-07-01T00:00:00Z
  wa mlwh export irods study 5901 --server http://host:8091 --json`

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
	flags := mlwhExportFlags{format: mlwhExportFormatTSV}

	command := &cobra.Command{
		Use:           "export <children> <parent-kind> <parent-id>",
		Short:         "Export MLWH relationship rows with selectable columns",
		SilenceUsage:  true,
		SilenceErrors: false,
		Long: strings.Join([]string{
			mlwhExportIntroHelp,
			mlwhExportChildrenHelp(),
			mlwhExportParentIDHelp,
			mlwhExportColumnsHelp(),
			mlwhExportOptionsHelp,
			mlwhQueryCommandConfigurationHelp,
			mlwhExportExamplesHelp,
		}, "\n\n"),
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
	command.Flags().StringVar(&flags.sort, "sort", "", "explicit iRODS sort order; only created-desc is supported, for newest-created exports")
	command.Flags().StringVar(&flags.since, "since", "", "inclusive RFC3339 lower bound for created-date iRODS exports, e.g. 2026-07-01T00:00:00Z")
	command.Flags().StringVar(&flags.until, "until", "", "exclusive RFC3339 upper bound for created-date iRODS exports, e.g. 2026-08-01T00:00:00Z")

	return command
}

func mlwhExportChildrenHelp() string {
	var builder strings.Builder
	builder.WriteString("Children:\n")
	for _, desc := range mlwh.ExportRelationshipDescriptions() {
		builder.WriteString("  ")
		builder.WriteString(mlwhExportChildrenLabel(desc.Children, desc.Aliases))
		builder.WriteString(": ")
		builder.WriteString(desc.Description)
		builder.WriteString("; parent-kind: ")
		builder.WriteString(strings.Join(desc.ParentKinds, ", "))
		builder.WriteByte('\n')
	}

	return strings.TrimRight(builder.String(), "\n")
}

func mlwhExportChildrenLabel(children string, aliases []string) string {
	if len(aliases) == 0 {
		return children
	}

	return fmt.Sprintf("%s (alias: %s)", children, strings.Join(aliases, ", "))
}

func mlwhExportColumnsHelp() string {
	var builder strings.Builder
	builder.WriteString("Columns:\n")
	for _, vocab := range mlwh.ExportColumnVocabularies() {
		builder.WriteString("  ")
		builder.WriteString(mlwhExportColumnLabel(vocab.Children, vocab.Aliases))
		builder.WriteString(":\n    default: ")
		builder.WriteString(strings.Join(vocab.Default, ", "))
		builder.WriteString("\n    available: ")
		builder.WriteString(strings.Join(mlwhExportColumnNames(vocab.Columns), ", "))
		builder.WriteByte('\n')
	}

	return strings.TrimRight(builder.String(), "\n")
}

func mlwhExportColumnLabel(children string, aliases []string) string {
	if len(aliases) == 0 {
		return children
	}

	return children + "/" + strings.Join(aliases, "/")
}

func mlwhExportColumnNames(columns []mlwh.ExportColumnDescription) []string {
	names := make([]string, len(columns))
	for index, column := range columns {
		names[index] = column.Name
		if len(column.Aliases) > 0 {
			names[index] += " (alias: " + strings.Join(column.Aliases, ", ") + ")"
		}
	}

	return names
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

	_, err = result.RenderAsTo(ctx, dataOut, opts.Format)
	if err != nil {
		return fmt.Errorf("render export: %w", err)
	}

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
		All:              true,
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
