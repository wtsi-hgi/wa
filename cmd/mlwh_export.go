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
  sample: a sample UUID, LIMS id, Sanger sample name/id, supplier name, accession or donor id;
    a value naming several samples fails as ambiguous with exit status 3,
    listing the samples on stderr and writing nothing to stdout. With
    --all-samples, a supplier name or donor id shared by several samples
    exports the rows of every matching sample (samples with no matching rows
    add none), and lanes and irods rows carry sanger_sample_id and
    supplier_name to tell them apart; a value that is also some sample's
    name, Sanger sample id, LIMS id or accession still fails as ambiguous.
    --all-samples needs a server at MLWH API 1.11.0 or later; against an
    older server it fails with exit status 1
  run: an Illumina NPG id_run
  library: a pipeline_id_lims, library_id, id_library_lims or library type
  faculty-sponsor: a case-insensitive faculty_sponsor substring
  user: a study_users name, login or email substring
  programme: an exact programme value`

const mlwhExportOptionsHelp = `File exports:
  iRODS/files and sample-crams use --file-type as a filename suffix filter. A
  single leading dot is stripped, so cram, .cram and CRAM are equivalent. If no
  --file-type is supplied, these file exports default to cram.

Product exports:
  Products are product-grained: one row per distinct id_run/lane/tag product,
  including products with no iRODS object. For products, --file-type only
  restricts the attached irods_path; there is no cram default and no forced
  deliverables view. A blank irods_path is expected when no matching object is
  attached; use irods_unmatched and reason for known gaps.

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
  iRODS/files, samples, sample-crams and products exports. --role narrows
  study_users-backed studies/users exports.

Sorting and date windows:
  --sort created-desc is the only supported explicit sort because the default
  iRODS order is run/lane/tag/file order. Use created-desc when you want newest
  data first or a created-date window. --since is inclusive and --until is
  exclusive; both use an RFC3339 timestamp such as
  2026-07-01T00:00:00Z.

Completeness:
  By default, exports include every matching row and append no success message
  after the data. Use --limit to emit one bounded page and --cursor to continue
  from a previous bounded iRODS/products page. Omit --limit to export
  everything.`

const mlwhExportExamplesHelp = `Examples:
  wa mlwh export sample-crams study 5901
  wa --env development mlwh export irods study 5901 --file-type cram
  wa mlwh export products study 7568 --file-type cram --columns name,supplier_name,accession_number,sanger_sample_id,id_run,lane,tag_index,manual_qc,irods_path,irods_unmatched,reason
  wa mlwh export runs sample DN1234 --columns id_run,platform,run_date
  wa mlwh export lanes sample PD42478a --all-samples
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

func needsShellQuoting(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	default:
		return !strings.ContainsRune("_@%+=:,./-", r)
	}
}

type mlwhExportClient interface {
	Export(ctx context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error)
	Close() error
}

func openMLWHExportConfiguredClient(ctx context.Context, serverURL string) (mlwhExportClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHExportRemoteClient(ctx, mlwhCLIRemoteConfig(trimmedServerURL))
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
		Use:   "export <children> <parent-kind> <parent-id>",
		Short: "Export MLWH relationship rows with selectable columns",
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
	command.Flags().IntVar(&flags.limit, "limit", 0, "emit one bounded page with at most this many rows; omit to export everything")
	command.Flags().StringVar(&flags.cursor, "cursor", "", "resume a bounded iRODS/products export from this cursor")
	command.Flags().StringVar(&flags.fileType, "file-type", "", "restrict file exports or products irods_path attachments to data objects whose filename ends in this suffix")
	command.Flags().BoolVar(&flags.deliverablesOnly, "deliverables-only", false, "restrict cram file exports to deliverable rows")
	command.Flags().BoolVar(&flags.includeControls, "include-controls", false, "include controls/sub-products in cram file exports")
	command.Flags().StringVar(&flags.role, "role", "", "restrict study_users-backed exports to comma-separated roles")
	command.Flags().StringVar(&flags.qc, "qc", "", "restrict product-backed exports by QC: pass, fail or pending")
	command.Flags().StringVar(&flags.libraryType, "library-type", "", "restrict sample-backed exports by library type")
	command.Flags().StringVar(&flags.organism, "organism", "", "restrict sample-backed exports by organism/common name")
	command.Flags().StringVar(&flags.sort, "sort", "", "explicit iRODS sort order; only created-desc is supported, for newest-created exports")
	command.Flags().StringVar(&flags.since, "since", "", "inclusive RFC3339 lower bound for created-date iRODS exports, e.g. 2026-07-01T00:00:00Z")
	command.Flags().BoolVar(&flags.allSamples, "all-samples", false, "for a sample parent, span every sample sharing the supplier name or donor id; rows carry sanger_sample_id and supplier_name")
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
		return mlwh.ExportRelationship{}, "", usageError("usage: wa mlwh export <children> <parent-kind> <parent-id>")
	}

	children := strings.ToLower(strings.TrimSpace(args[0]))
	parentKind := strings.ToLower(strings.TrimSpace(args[1]))
	parentID := strings.TrimSpace(args[2])
	if children == "" || parentKind == "" || parentID == "" {
		return mlwh.ExportRelationship{}, "", usageError("usage: wa mlwh export <children> <parent-kind> <parent-id>")
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
		if handled, handleErr := handleMLWHExportCleanError(ctx, client, dataOut, statusOut, rel, parentID, opts.Format, err); handled {
			return handleErr
		}

		var unsupported *mlwh.MultiSampleUnsupportedError
		if errors.As(err, &unsupported) {
			return mlwhExportMultiSampleUnsupportedError(rel, parentID, unsupported)
		}

		if rel.ParentKind == "sample" && errors.Is(err, mlwh.ErrAmbiguous) {
			return mlwhExportAmbiguousSampleError(ctx, client, rel, parentID, opts.MultiSample)
		}

		return fmt.Errorf("export %s of %s %q: %w", rel.Children, rel.ParentKind, parentID, err)
	}

	emitted, err := result.RenderAsTo(ctx, dataOut, opts.Format)
	if err != nil {
		if emitted == 0 {
			if handled, handleErr := handleMLWHExportCleanError(ctx, client, dataOut, statusOut, rel, parentID, opts.Format, err); handled {
				return handleErr
			}
		}

		return fmt.Errorf("render export: %w", err)
	}
	if opts.Limit > 0 {
		if err = writeMLWHExportBoundedPageStatus(statusOut, result); err != nil {
			return fmt.Errorf("write bounded export status: %w", err)
		}
	}

	return nil
}

// mlwhExportMultiSampleUnsupportedError explains that the server is too old to
// export several samples at once, that a newer one could only when the
// identifier is a shared supplier name or donor id, and how to export the
// samples one by one instead.
func mlwhExportMultiSampleUnsupportedError(rel mlwh.ExportRelationship, parentID string, err *mlwh.MultiSampleUnsupportedError) error {
	return fmt.Errorf(
		"export %s of %s %q: %q names several samples on the MLWH server at %s, which runs API %s, too old to export several samples at once; "+
			"if it is a supplier name or donor id the samples share, a server with API %s or later can export them together; "+
			"otherwise, or with this server, export each sample by its Sanger sample name, listing them with:\n  wa mlwh search --type sample %s",
		rel.Children, rel.ParentKind, parentID, parentID, err.ServerURL, err.ServerAPIVersion, mlwh.MultiSampleExportAPIVersion, shellQuote(parentID))
}

// shellQuote returns value as one POSIX shell word, single-quoting it unless it
// holds only characters the shell never interprets.
func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, needsShellQuoting) < 0 {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func handleMLWHExportCleanError(
	ctx context.Context,
	client mlwhExportClient,
	dataOut io.Writer,
	statusOut io.Writer,
	rel mlwh.ExportRelationship,
	parentID string,
	format string,
	err error,
) (bool, error) {
	if errors.Is(err, mlwh.ErrCacheNeverSynced) {
		return true, writeMLWHExportCacheUnavailable(dataOut, statusOut, format)
	}
	if errors.Is(err, mlwh.ErrNotFound) {
		if mlwhClientNeverSynced(ctx, client) {
			return true, writeMLWHExportCacheUnavailable(dataOut, statusOut, format)
		}

		writeMLWHExportNotFound(statusOut, rel, parentID)

		return true, nil
	}

	return false, nil
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

func writeMLWHExportBoundedPageStatus(out io.Writer, result mlwh.ExportResult) error {
	parts := []string{"bounded page emitted", "omit --limit to export everything"}
	if result.NextCursor != "" {
		parts = append(parts, "continue with --cursor "+result.NextCursor)
	}
	if result.Complete {
		parts = append(parts, "final page")
	}

	_, err := fmt.Fprintln(out, strings.Join(parts, "; "))

	return err
}

// mlwhExportAmbiguousSampleError reports that parentID names several samples,
// listing them when the client can, and exits with ExitCodeAmbiguous. Without
// --all-samples it points to that flag; with it, the identifier is one that
// must be unique, so the samples cannot be exported together.
func mlwhExportAmbiguousSampleError(ctx context.Context, client mlwhExportClient, rel mlwh.ExportRelationship, parentID string, allSamples bool) error {
	advice := "pass --all-samples to export every sample when it is a supplier name or donor id they share, or give one sample's Sanger name"
	if allSamples {
		advice = "it is also some sample's name, Sanger sample id, LIMS id or accession, so --all-samples cannot span it; give one sample's Sanger name"
	}

	var listing strings.Builder
	if infoClient, ok := mlwhExportCandidateClient(client); ok {
		if report, found := buildInfoCandidatesReport(ctx, infoClient, parentID); found {
			_, _ = fmt.Fprintf(&listing, " matches %d samples:\n\n", len(report.Candidates))
			writeInfoCandidateRows(&listing, report.Candidates)
			listing.WriteString("\n")
		}
	}
	if listing.Len() == 0 {
		listing.WriteString(" names several samples; ")
	}

	return exitCodeError{
		code: ExitCodeAmbiguous,
		err:  fmt.Errorf("export %s of sample %q: %q%s%s", rel.Children, parentID, parentID, listing.String(), advice),
	}
}

// mlwhExportCandidateClient returns the client to list an ambiguous sample's
// candidates with, when the export client offers one.
func mlwhExportCandidateClient(client mlwhExportClient) (mlwhInfoClient, bool) {
	if remote, ok := client.(*mlwhExportRemoteClient); ok {
		return remote.remote, true
	}

	infoClient, ok := client.(mlwhInfoClient)

	return infoClient, ok
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
	allSamples       bool
	limit            int
	cursor           string
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

	limitChanged := cmd.Flags().Changed("limit")
	cursorChanged := cmd.Flags().Changed("cursor")
	if f.limit < 0 {
		return mlwh.ExportOptions{}, errors.New("--limit must be non-negative")
	}
	if cursorChanged && (!limitChanged || f.limit <= 0) {
		return mlwh.ExportOptions{}, usageError("--cursor requires a positive explicit --limit to continue a bounded export")
	}

	if f.allSamples && rel.ParentKind != "sample" {
		return mlwh.ExportOptions{}, usageError("--all-samples applies only to sample exports")
	}

	opts := mlwh.ExportOptions{
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
		Format:           format,
		MultiSample:      f.allSamples,
	}
	if f.limit > 0 {
		opts.Limit = f.limit
		opts.Cursor = f.cursor
	} else {
		opts.All = true
	}

	return opts, nil
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
		return nil, usageError("--deliverables-only and --include-controls cannot be used together")
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
