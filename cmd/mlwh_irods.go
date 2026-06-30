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

// irodsNoMatchMessage is the neutral line shown when a synced cache returns no
// matching iRODS paths (an empty/unmatched result). It is distinct from the
// never-synced cache-unavailable message; both render cleanly and exit 0.
const irodsNoMatchMessage = "no matching iRODS paths"

var openMLWHIRODSClient = func(ctx context.Context, cfg mlwh.Config) (mlwhIRODSClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return mlwh.OpenCacheOnly(ctx, cfg.Cache)
	}

	return mlwh.Open(ctx, cfg)
}

var openMLWHIRODSRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhIRODSClient, error) {
	return mlwh.NewRemoteClient(cfg)
}

// mlwhIRODSClient is the subset of the MLWH query surface used by `wa mlwh irods`.
// Both *mlwh.RemoteClient and the local *mlwh.Client satisfy it.
type mlwhIRODSClient interface {
	IRODSPathsForStudyByFileType(ctx context.Context, studyLimsID, fileType string, limit, offset int) ([]mlwh.IRODSPath, error)
	IRODSPathsForRun(ctx context.Context, idRun, fileType string, limit, offset int) ([]mlwh.IRODSPath, error)
	IRODSPathsForSampleByFileType(ctx context.Context, sangerName, fileType string, limit, offset int) ([]mlwh.IRODSPath, error)
	Close() error
}

func openMLWHIRODSConfiguredClient(ctx context.Context, serverURL string) (mlwhIRODSClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHIRODSRemoteClient(ctx, mlwh.RemoteConfig{BaseURL: trimmedServerURL})
	}

	cfg, err := resolveMLWHInfoLocalConfig()
	if err != nil {
		return nil, err
	}

	client, err := openMLWHIRODSClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.DSN) != "" && errors.Is(err, mlwh.ErrPasswordInDSN) {
			return nil, fmt.Errorf("WA_MLWH_DSN: %w", err)
		}

		return nil, err
	}

	return client, nil
}

func newMLWHIRODSCommand() *cobra.Command {
	var (
		serverURL string
		fileType  string
		limit     int
		offset    int
		jsonOut   bool
	)

	command := &cobra.Command{
		Use:           "irods <study|run|sample> <identifier>",
		Short:         "List iRODS paths for a study, run or sample, optionally filtered by file type",
		SilenceUsage:  true,
		SilenceErrors: false,
		Long: strings.Join([]string{
			"List the iRODS data object paths for a study, run or sample through a",
			"wa mlwh serve API, optionally restricted to a filename suffix with",
			"--file-type. The first positional selects the scope (study, run or",
			"sample) and the second is the identifier (a study LIMS id, an Illumina",
			"NPG run id, or a Sanger sample name).",
			"",
			"Use this when you want the data manifest's iRODS locations rather than",
			"\"info about one identifier\". --file-type is a FILENAME-SUFFIX filter (a",
			"single leading dot is stripped): e.g. --file-type cram matches data",
			"objects ending in .cram. A valid but unmatched suffix is not an error;",
			"it simply yields no matching iRODS paths. Use --limit/--offset to page",
			"and --json for a single JSON array of iRODS paths suitable for piping",
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
			"  # cram paths for a study via a development stack started by make dev",
			"  wa --env development mlwh irods study 5901 --file-type cram",
			"",
			"  # All iRODS paths for a run from a remote MLWH server as JSON",
			"  wa mlwh irods run 52553 --server http://host:8091 --json",
			"",
			"  # bam paths for a sample against a local operator cache",
			"  WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite wa mlwh irods sample DN1234 --file-type bam",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, identifier, err := parseIRODSScopeArgs(args)
			if err != nil {
				return err
			}

			normalisedFileType, err := normaliseIRODSFileTypeFlag(fileType, cmd.Flags().Changed("file-type"))
			if err != nil {
				return err
			}

			client, err := openMLWHIRODSConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			return runMLWHIRODS(cmd.Context(), client, cmd.OutOrStdout(), scope, identifier, normalisedFileType, limit, offset, jsonOut)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().StringVar(&fileType, "file-type", "", "restrict to data objects whose filename ends in this suffix (a leading dot is stripped), e.g. cram")
	command.Flags().IntVar(&limit, "limit", 50, "maximum number of iRODS paths to return")
	command.Flags().IntVar(&offset, "offset", 0, "number of iRODS paths to skip (for pagination)")
	command.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON array of iRODS paths instead of human-readable text")

	return command
}

// parseIRODSScopeArgs validates the two positionals and returns the lowercased
// scope keyword and the identifier. It rejects a missing scope/identifier and an
// unrecognised scope keyword with a clear usage/input error (non-zero exit).
func parseIRODSScopeArgs(args []string) (scope, identifier string, err error) {
	if len(args) != 2 {
		return "", "", errors.New("usage: wa mlwh irods <study|run|sample> <identifier>")
	}

	scope = strings.ToLower(strings.TrimSpace(args[0]))
	identifier = strings.TrimSpace(args[1])
	if scope == "" || identifier == "" {
		return "", "", errors.New("usage: wa mlwh irods <study|run|sample> <identifier>")
	}

	switch scope {
	case "study", "run", "sample":
		return scope, identifier, nil
	default:
		return "", "", fmt.Errorf("unknown scope %q (expected study, run or sample)", args[0])
	}
}

func normaliseIRODSFileTypeFlag(raw string, present bool) (string, error) {
	if !present {
		return "", nil
	}

	normalised := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "."))
	if normalised == "" || strings.ContainsAny(normalised, `%_/`) {
		return "", fmt.Errorf("invalid --file-type %q: a filename suffix may not be empty or contain '%%', '_' or '/'", raw)
	}

	return normalised, nil
}

// runMLWHIRODS dispatches to the scope's iRODS query and renders the result. It
// applies the soft-failure policy: a never-synced cache (ErrCacheNeverSynced) is a
// neutral cache-unavailable result (exit 0), and an empty/unmatched list prints
// the neutral no-match line (exit 0). The caller validates and normalises the
// optional file-type filter before dispatch, so query-level ErrUnsupportedIdentifier
// remains an identifier error.
func runMLWHIRODS(ctx context.Context, client mlwhIRODSClient, out io.Writer, scope, identifier, fileType string, limit, offset int, jsonOut bool) error {
	paths, err := dispatchIRODSScope(ctx, client, scope, identifier, fileType, limit, offset)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			return writeIRODSCacheUnavailable(out, jsonOut)
		}

		return fmt.Errorf("irods paths for %s %q: %w", scope, identifier, err)
	}

	if jsonOut {
		return writeIRODSPathsJSON(out, paths)
	}

	writeIRODSPathsText(out, paths)

	return nil
}

// writeIRODSCacheUnavailable renders the never-synced degradation: a neutral
// cache-unavailable message with no sync hint (text), or an empty JSON array
// (--json), so the never-synced case is indistinguishable in shape from a synced
// empty result. Exit 0 either way.
func writeIRODSCacheUnavailable(out io.Writer, jsonOut bool) error {
	if jsonOut {
		return writeIRODSPathsJSON(out, nil)
	}

	_, _ = fmt.Fprintf(out, "%s\n", mlwhCacheUnavailableMessage)

	return nil
}

func writeIRODSPathsJSON(out io.Writer, paths []mlwh.IRODSPath) error {
	if paths == nil {
		paths = []mlwh.IRODSPath{}
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(paths); err != nil {
		return fmt.Errorf("encode irods paths: %w", err)
	}

	return nil
}

// writeIRODSPathsText renders the tabular text output: one row per iRODS path with
// its path, id_run and platform columns. An empty/unmatched result prints the
// neutral no-match line (exit 0).
func writeIRODSPathsText(out io.Writer, paths []mlwh.IRODSPath) {
	if len(paths) == 0 {
		_, _ = fmt.Fprintf(out, "%s\n", irodsNoMatchMessage)

		return
	}

	_, _ = fmt.Fprintf(out, "iRODS paths (%d):\n", len(paths))
	for _, path := range paths {
		_, _ = fmt.Fprintf(out, "  %s id_run=%d platform=%s\n", path.IRODSPath, path.IDRun, path.Platform)
	}
}

func dispatchIRODSScope(ctx context.Context, client mlwhIRODSClient, scope, identifier, fileType string, limit, offset int) ([]mlwh.IRODSPath, error) {
	switch scope {
	case "study":
		return client.IRODSPathsForStudyByFileType(ctx, identifier, fileType, limit, offset)
	case "run":
		return client.IRODSPathsForRun(ctx, identifier, fileType, limit, offset)
	case "sample":
		return client.IRODSPathsForSampleByFileType(ctx, identifier, fileType, limit, offset)
	default:
		return nil, fmt.Errorf("unknown scope %q (expected study, run or sample)", scope)
	}
}
