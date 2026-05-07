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
	"os"
	"path/filepath"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
)

var openMLWHSyncClient = func(ctx context.Context, cfg mlwh.Config) (mlwhSyncClient, error) {
	return mlwh.Open(ctx, cfg)
}

type mlwhSyncClient interface {
	Sync(context.Context, ...string) ([]mlwh.SyncReport, error)
	Close() error
}

func newMLWHCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "mlwh",
		Short: "MLWH cache and sync commands",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	command.AddCommand(newMLWHSyncCommand())

	return command
}

func newMLWHSyncCommand() *cobra.Command {
	var tables []string

	command := &cobra.Command{
		Use:   "sync",
		Short: "Sync MLWH tables into the local cache",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveMLWHSyncConfig()
			if err != nil {
				return err
			}

			client, err := openMLWHSyncClient(cmd.Context(), cfg)
			if err != nil {
				if errors.Is(err, mlwh.ErrPasswordInDSN) {
					return fmt.Errorf("WA_MLWH_DSN: %w", err)
				}

				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			reports, err := client.Sync(cmd.Context(), tables...)
			if err != nil {
				return err
			}

			for _, report := range reports {
				_, _ = fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s inserted=%d updated=%d high_water=%s\n",
					report.Table,
					report.Inserted,
					report.Updated,
					report.HighWater.UTC().Format("2006-01-02T15:04:05Z"),
				)
			}

			return nil
		},
	}

	command.Flags().StringSliceVar(&tables, "tables", nil, "limit the sync to specific tables (sample, study, iseq_flowcell)")

	return command
}

func resolveMLWHSyncConfig() (mlwh.Config, error) {
	dsn := firstEnv("WA_MLWH_DSN")
	if dsn == "" {
		return mlwh.Config{}, errors.New("WA_MLWH_DSN must be set")
	}

	cachePath, err := resolveMLWHSyncCachePath()
	if err != nil {
		return mlwh.Config{}, err
	}

	return mlwh.Config{
		DSN:      dsn,
		Password: firstEnv("WA_MLWH_PASSWORD"),
		Cache: mlwh.CacheConfig{
			Path:     cachePath,
			Password: firstEnv("WA_MLWH_CACHE_PASSWORD"),
		},
	}, nil
}

func resolveMLWHSyncCachePath() (string, error) {
	if cachePath := firstEnv("WA_MLWH_CACHE_PATH"); cachePath != "" {
		return cachePath, nil
	}

	baseDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("determine default WA_MLWH_CACHE_PATH: %w", err)
	}

	cachePath := filepath.Join(baseDir, "wa", "mlwh.sqlite")
	if err = ensureMLWHSyncCacheDirectory(cachePath); err != nil {
		return "", err
	}

	return cachePath, nil
}

func ensureMLWHSyncCacheDirectory(cachePath string) error {
	trimmedPath := strings.TrimSpace(cachePath)
	if trimmedPath == "" || trimmedPath == ":memory:" || strings.HasPrefix(trimmedPath, "file:") || mlwhSyncCachePathLooksMySQL(trimmedPath) {
		return nil
	}

	dirPath := filepath.Dir(trimmedPath)
	if dirPath == "." {
		return nil
	}

	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return fmt.Errorf("create mlwh cache directory: %w", err)
	}

	return nil
}

func mlwhSyncCachePathLooksMySQL(path string) bool {
	parsed, err := mysql.ParseDSN(path)
	if err != nil {
		return false
	}

	return parsed.DBName != "" && (parsed.User != "" || parsed.Net != "" || parsed.Addr != "" || strings.Contains(path, "@"))
}
