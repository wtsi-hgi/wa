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
	// manifestNoProductsMessage is the neutral line shown when a synced study has
	// no products (an envelope with empty Rows). It is distinct from the
	// never-synced cache-unavailable message; both render cleanly and exit 0.
	manifestNoProductsMessage = "no products"

	// manifestEmptyIRODSPlaceholder renders a row whose iRODS object is absent
	// under --with-irods, so every row line keeps the same shape.
	manifestEmptyIRODSPlaceholder = "-"
)

var openMLWHManifestClient = func(ctx context.Context, cfg mlwh.Config) (mlwhManifestClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return mlwh.OpenCacheOnly(ctx, cfg.Cache)
	}

	return mlwh.Open(ctx, cfg)
}

var openMLWHManifestRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhManifestClient, error) {
	return mlwh.NewRemoteClient(cfg)
}

// mlwhManifestClient is the subset of the MLWH query surface used by `wa mlwh
// manifest`. Both *mlwh.RemoteClient and the local *mlwh.Client satisfy it.
type mlwhManifestClient interface {
	StudyManifest(ctx context.Context, studyLimsID, fileType string, withIRODS bool, limit, offset int) (mlwh.StudyManifest, error)
	Close() error
}

func openMLWHManifestConfiguredClient(ctx context.Context, serverURL string) (mlwhManifestClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHManifestRemoteClient(ctx, mlwh.RemoteConfig{BaseURL: trimmedServerURL})
	}

	cfg, err := resolveMLWHInfoLocalConfig()
	if err != nil {
		return nil, err
	}

	client, err := openMLWHManifestClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.DSN) != "" && errors.Is(err, mlwh.ErrPasswordInDSN) {
			return nil, fmt.Errorf("WA_MLWH_DSN: %w", err)
		}

		return nil, err
	}

	return client, nil
}

func newMLWHManifestCommand() *cobra.Command {
	var (
		serverURL string
		fileType  string
		withIRODS bool
		limit     int
		offset    int
		jsonOut   bool
	)

	command := &cobra.Command{
		Use:           "manifest <study>",
		Short:         "List a study's data manifest: one row per sequencing product",
		SilenceUsage:  true,
		SilenceErrors: false,
		Long: strings.Join([]string{
			"List a study's data manifest through a wa mlwh serve API: the study",
			"metadata once, then one row per sequencing product (run x lane x tag)",
			"joined to its sample's identity. The positional is the study LIMS id.",
			"",
			"Use this when you want the full per-product manifest for a study rather",
			"than \"info about one identifier\". Add --with-irods to attach each",
			"product's iRODS data object; --file-type restricts that object to a",
			"FILENAME-SUFFIX filter (a single leading dot is stripped): e.g.",
			"--file-type cram attaches the .cram object. The manifest stays",
			"product-grained regardless of --with-irods/--file-type: a product with",
			"no matching iRODS object still appears as a row (its irods_path renders",
			"as '-'). Use --limit/--offset to page and --json for a single JSON",
			"manifest object (the envelope, not a bare array) suitable for piping",
			"into jq.",
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
			"  # The manifest for a study via a development stack started by make dev",
			"  wa --env development mlwh manifest 5901",
			"",
			"  # The manifest with cram iRODS paths from a remote MLWH server as JSON",
			"  wa mlwh manifest 5901 --with-irods --file-type cram --server http://host:8091 --json",
			"",
			"  # A page of the manifest against a local operator cache",
			"  WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite wa mlwh manifest 5901 --limit 50 --offset 50",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			study, err := parseManifestStudyArg(args)
			if err != nil {
				return err
			}

			client, err := openMLWHManifestConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			return runMLWHManifest(cmd.Context(), client, cmd.OutOrStdout(), study, fileType, withIRODS, limit, offset, jsonOut)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().BoolVar(&withIRODS, "with-irods", false, "attach each product's iRODS data object path (renders '-' when a product has no matching object)")
	command.Flags().StringVar(&fileType, "file-type", "", "with --with-irods, restrict the attached object to this filename suffix (a leading dot is stripped), e.g. cram")
	command.Flags().IntVar(&limit, "limit", 50, "maximum number of product rows to return")
	command.Flags().IntVar(&offset, "offset", 0, "number of product rows to skip (for pagination)")
	command.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON manifest object (the envelope) instead of human-readable text")

	return command
}

// parseManifestStudyArg validates the single study positional and returns it
// trimmed. It rejects a missing/empty study with a clear usage error (non-zero
// exit).
func parseManifestStudyArg(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: wa mlwh manifest <study>")
	}

	study := strings.TrimSpace(args[0])
	if study == "" {
		return "", errors.New("usage: wa mlwh manifest <study>")
	}

	return study, nil
}

// runMLWHManifest fetches the study manifest and renders it. It applies the
// soft-failure policy: a never-synced cache (ErrCacheNeverSynced) is a neutral
// cache-unavailable result (exit 0), a synced study with no products (an envelope
// with empty Rows) prints the header plus the neutral no-products line (exit 0),
// and an invalid file type (ErrUnsupportedIdentifier, a bad-request-class input
// error) returns a clear error (non-zero exit).
func runMLWHManifest(ctx context.Context, client mlwhManifestClient, out io.Writer, study, fileType string, withIRODS bool, limit, offset int, jsonOut bool) error {
	manifest, err := client.StudyManifest(ctx, study, fileType, withIRODS, limit, offset)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			return writeManifestCacheUnavailable(out, jsonOut)
		}

		// A bad-request-class error here is the file-type filter being invalid (an
		// input error, not a degradation): name the rejected --file-type value so
		// the message is clear, and exit non-zero.
		if errors.Is(err, mlwh.ErrUnsupportedIdentifier) && fileType != "" {
			return fmt.Errorf("invalid --file-type %q: a filename suffix may not be empty or contain '%%', '_' or '/'", fileType)
		}

		return fmt.Errorf("study manifest for %q: %w", study, err)
	}

	if jsonOut {
		return writeManifestJSON(out, manifest)
	}

	writeManifestText(out, manifest, withIRODS)

	return nil
}

// writeManifestCacheUnavailable renders the never-synced degradation: a neutral
// cache-unavailable message with no sync hint (text), or the zero-value manifest
// envelope (--json), so the never-synced case is indistinguishable in shape from a
// synced empty result. Exit 0 either way.
func writeManifestCacheUnavailable(out io.Writer, jsonOut bool) error {
	if jsonOut {
		return writeManifestJSON(out, mlwh.StudyManifest{Rows: []mlwh.ManifestRow{}})
	}

	_, _ = fmt.Fprintf(out, "%s\n", mlwhCacheUnavailableMessage)

	return nil
}

func writeManifestJSON(out io.Writer, manifest mlwh.StudyManifest) error {
	if manifest.Rows == nil {
		manifest.Rows = []mlwh.ManifestRow{}
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(manifest); err != nil {
		return fmt.Errorf("encode study manifest: %w", err)
	}

	return nil
}

// writeManifestText renders the tabular text output: a header line with the study
// metadata printed ONCE, then one line per product row. With withIRODS each row
// line also carries irods_path (an empty path renders as the placeholder). A synced
// study with no products prints the header plus the neutral no-products line.
func writeManifestText(out io.Writer, manifest mlwh.StudyManifest, withIRODS bool) {
	writeManifestHeader(out, manifest)

	if len(manifest.Rows) == 0 {
		_, _ = fmt.Fprintf(out, "\n%s\n", manifestNoProductsMessage)

		return
	}

	_, _ = fmt.Fprintf(out, "\nProducts (%d):\n", len(manifest.Rows))
	for _, row := range manifest.Rows {
		writeManifestRow(out, row, withIRODS)
	}
}

// writeManifestHeader renders the study-level metadata carried once in the
// envelope (name / accession / faculty_sponsor / data_access_group), plus the
// study id and the cache freshness so the page is self-describing.
func writeManifestHeader(out io.Writer, manifest mlwh.StudyManifest) {
	_, _ = fmt.Fprintf(out, "Study manifest:\n")
	writeKV(out, "  id_study_lims", manifest.IDStudyLims)
	writeKV(out, "  name", manifest.Name)
	writeKV(out, "  accession_number", manifest.AccessionNumber)
	writeKV(out, "  faculty_sponsor", manifest.FacultySponsor)
	writeKV(out, "  data_access_group", manifest.DataAccessGroup)
	writeKV(out, "  cache_synced_at", manifest.CacheSyncedAt)
}

// writeManifestRow renders one product row's fields. With withIRODS it appends the
// row's irods_path, rendering an empty path as the placeholder so every row line
// keeps the same shape.
func writeManifestRow(out io.Writer, row mlwh.ManifestRow, withIRODS bool) {
	_, _ = fmt.Fprintf(out, "  name=%s supplier_name=%s accession_number=%s sanger_sample_id=%s id_run=%d lane=%d tag_index=%d",
		row.Name, row.SupplierName, row.AccessionNumber, row.SangerSampleID, row.IDRun, row.Position, row.TagIndex)

	if withIRODS {
		irodsPath := row.IRODSPath
		if strings.TrimSpace(irodsPath) == "" {
			irodsPath = manifestEmptyIRODSPlaceholder
		}

		_, _ = fmt.Fprintf(out, " irods_path=%s", irodsPath)
	}

	_, _ = fmt.Fprintln(out)
}
