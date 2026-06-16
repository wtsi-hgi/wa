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
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
	"github.com/wtsi-hgi/wa/mlwhdiff"
)

var listenFunc = net.Listen

const mlwhdiffProviderFetchLimit = 1_000_000

var openMLWHDiffMLWHCacheOnlyClient = mlwh.OpenCacheOnly

var openMLWHDiffClientFunc = openMLWHDiffClientWithConfig

type mlwhdiffMLWHConfig struct {
	ServerURL     string
	CachePath     string
	CachePassword string
}

func resolveMLWHDiffMLWHConfig(options *mlwhdiffOptions, cacheFlagChanged bool) (mlwhdiffMLWHConfig, error) {
	if trimmedServerURL := strings.TrimSpace(options.mlwhServerURL); trimmedServerURL != "" {
		return mlwhdiffMLWHConfig{ServerURL: trimmedServerURL}, nil
	}

	cachePath, hasCachePath, err := resolveMLWHDiffMLWHCachePath(options.mlwhCachePath, cacheFlagChanged)
	if err != nil {
		return mlwhdiffMLWHConfig{}, err
	}
	if !hasCachePath {
		return mlwhdiffMLWHConfig{}, errors.New("WA_MLWH_SERVER_URL or WA_MLWH_CACHE_PATH must be set; pass --mlwh-server-url or --mlwh-cache")
	}

	return mlwhdiffMLWHConfig{
		CachePath:     cachePath,
		CachePassword: firstEnv("WA_MLWH_CACHE_PASSWORD"),
	}, nil
}

type mlwhdiffMLWHHandle interface {
	mlwhdiff.DiffSource
	Close() error
}

func openMLWHDiffClientWithConfig(ctx context.Context, cfg mlwhdiffMLWHConfig) (mlwhdiffMLWHHandle, error) {
	if strings.TrimSpace(cfg.ServerURL) != "" {
		return mlwh.NewRemoteClient(mlwh.RemoteConfig{BaseURL: cfg.ServerURL})
	}

	client, err := openMLWHDiffMLWHCacheOnlyClient(ctx, mlwh.CacheConfig{
		Path:     cfg.CachePath,
		Password: cfg.CachePassword,
	})
	if err != nil {
		return nil, err
	}

	return client, nil
}

func openMLWHDiffClient(ctx context.Context, options *mlwhdiffOptions, cacheFlagChanged bool) (mlwhdiffMLWHHandle, error) {
	cfg, err := resolveMLWHDiffMLWHConfig(options, cacheFlagChanged)
	if err != nil {
		return nil, err
	}

	return openMLWHDiffClientFunc(ctx, cfg)
}

func newMLWHDiffDiffCommand(options *mlwhdiffOptions) *cobra.Command {
	var studyID string
	var sampleID string

	command := &cobra.Command{
		Use:   "diff",
		Short: "Diff study samples or sample files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (studyID == "" && sampleID == "") || (studyID != "" && sampleID != "") {
				return errors.New("usage: specify exactly one of --study or --sample")
			}

			provider, err := openMLWHDiffClient(commandContext(cmd), options, mlwhdiffFlagChanged(cmd, "mlwh-cache"))
			if err != nil {
				return err
			}
			defer func() { _ = provider.Close() }()

			store, err := mlwhdiff.OpenStore(options.dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			ctx := commandContext(cmd)

			if studyID != "" {
				return store.WithLock(func() error {
					prepared, err := mlwhdiff.PrepareDiffStudySamples(ctx, provider, store, studyID)
					if err != nil {
						return err
					}

					body, err := marshalCommandJSON(prepared.Result)
					if err != nil {
						return err
					}

					if err := prepared.Commit(); err != nil {
						return err
					}

					if err := writeCommandJSON(cmd.OutOrStdout(), body); err != nil {
						return rollbackPreparedDiff(prepared, err)
					}

					return nil
				})
			}

			return store.WithLock(func() error {
				prepared, err := mlwhdiff.PrepareDiffSampleFiles(ctx, provider, store, sampleID)
				if err != nil {
					return err
				}

				body, err := marshalCommandJSON(prepared.Result)
				if err != nil {
					return err
				}

				if err := prepared.Commit(); err != nil {
					return err
				}

				if err := writeCommandJSON(cmd.OutOrStdout(), body); err != nil {
					return rollbackPreparedDiff(prepared, err)
				}

				return nil
			})
		},
	}

	command.Flags().StringVar(&studyID, "study", "", "Study ID")
	command.Flags().StringVar(&sampleID, "sample", "", "Sanger sample ID")

	return command
}

func commandContext(cmd *cobra.Command) context.Context {
	if cmd == nil || cmd.Context() == nil {
		return context.Background()
	}

	return cmd.Context()
}

func mlwhdiffFlagChanged(cmd *cobra.Command, name string) bool {
	if cmd == nil {
		return false
	}

	flag := cmd.Flags().Lookup(name)

	return flag != nil && flag.Changed
}

func marshalCommandJSON(payload any) ([]byte, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, err
	}

	return body.Bytes(), nil
}

func writeCommandJSON(output io.Writer, body []byte) error {
	_, err := output.Write(body)

	return err
}

func rollbackPreparedDiff[T any](prepared *mlwhdiff.PreparedDiff[T], writeErr error) error {
	if rollbackErr := prepared.Rollback(); rollbackErr != nil {
		return errors.Join(writeErr, rollbackErr)
	}

	return writeErr
}

func newMLWHDiffServeCommand(options *mlwhdiffOptions) *cobra.Command {
	var port int

	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve the mlwhdiff HTTP API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			provider, err := openMLWHDiffClient(commandContext(cmd), options, mlwhdiffFlagChanged(cmd, "mlwh-cache"))
			if err != nil {
				return err
			}
			defer func() { _ = provider.Close() }()

			store, err := mlwhdiff.OpenStore(options.dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			listener, err := listenFunc("tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				return err
			}
			defer func() { _ = listener.Close() }()

			httpServer := &http.Server{Handler: mlwhdiff.NewServer(provider, store).Handler()}
			ctx := commandContext(cmd)

			go func() {
				<-ctx.Done()
				_ = httpServer.Shutdown(context.Background())
			}()

			err = httpServer.Serve(listener)
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}

			return err
		},
	}

	command.Flags().IntVar(&port, "port", 8080, "Port to bind")

	return command
}

type mlwhdiffCommandClient = mlwhdiffMLWHHandle

type mlwhdiffMLWHClientAdapter struct {
	client *mlwh.Client
}

func (a *mlwhdiffMLWHClientAdapter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if a == nil || a.client == nil || a.client.ReadDB() == nil {
		return nil, errors.New("mlwhdiff: mlwh client cache reader is not configured")
	}

	return a.client.ReadDB().QueryContext(ctx, query, args...)
}

func (a *mlwhdiffMLWHClientAdapter) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ClassifyIdentifier(ctx, raw)
}

func (a *mlwhdiffMLWHClientAdapter) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ResolveSample(ctx, raw)
}

func (a *mlwhdiffMLWHClientAdapter) ResolveSampleName(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ResolveSampleName(ctx, raw)
}

func (a *mlwhdiffMLWHClientAdapter) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ResolveStudy(ctx, raw)
}

func (a *mlwhdiffMLWHClientAdapter) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ResolveRun(ctx, raw)
}

func (a *mlwhdiffMLWHClientAdapter) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ResolveLibrary(ctx, raw)
}

func (a *mlwhdiffMLWHClientAdapter) AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error) {
	return a.client.AllStudies(ctx, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error) {
	match, err := a.client.ResolveStudy(ctx, identifier)
	if err != nil {
		return nil, err
	}

	return match.Study, nil
}

func (a *mlwhdiffMLWHClientAdapter) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForStudy(ctx, studyLimsID, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error) {
	return a.client.SamplesForStudy(ctx, studyLimsID, mlwhdiffProviderFetchLimit, 0)
}

func (a *mlwhdiffMLWHClientAdapter) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error) {
	return a.client.FindSamplesBySangerID(ctx, sangerID)
}

func (a *mlwhdiffMLWHClientAdapter) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error) {
	return a.client.FindSamplesByIDSampleLims(ctx, idSampleLims)
}

func (a *mlwhdiffMLWHClientAdapter) FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error) {
	return a.client.SamplesForRun(ctx, strconv.Itoa(idRun), mlwhdiffProviderFetchLimit, 0)
}

func (a *mlwhdiffMLWHClientAdapter) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]mlwh.Sample, error) {
	return a.client.FindSamplesByLibraryType(ctx, libraryType)
}

func (a *mlwhdiffMLWHClientAdapter) SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForLibraryType(ctx, pipelineIDLims, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) SamplesForLibraryID(ctx context.Context, libraryID string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForLibraryID(ctx, libraryID, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) SamplesForLibraryLimsID(ctx context.Context, idLibraryLims string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForLibraryLimsID(ctx, idLibraryLims, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error) {
	return a.client.FindSamplesByAccessionNumber(ctx, accessionNumber)
}

func (a *mlwhdiffMLWHClientAdapter) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForRun(ctx, idRun, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForLibrary(ctx, pipelineIDLims, studyLimsID, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error) {
	return a.client.LibrariesForStudy(ctx, studyLimsID, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) StudiesForSample(ctx context.Context, sangerName string) ([]mlwh.Study, error) {
	return a.client.StudiesForSample(ctx, sangerName)
}

func (a *mlwhdiffMLWHClientAdapter) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	return a.client.LanesForSample(ctx, sangerName, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	return a.client.IRODSPathsForSample(ctx, sangerName, limit, offset)
}

func (a *mlwhdiffMLWHClientAdapter) GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
	return a.client.IRODSPathsForSample(ctx, sangerName, mlwhdiffProviderFetchLimit, 0)
}

func (a *mlwhdiffMLWHClientAdapter) Sync(ctx context.Context) ([]mlwh.SyncReport, error) {
	return a.client.Sync(ctx)
}

func (a *mlwhdiffMLWHClientAdapter) Close() error {
	return a.client.Close()
}

type mlwhdiffOptions struct {
	dbPath        string
	mlwhCachePath string
	mlwhServerURL string
}

func newMLWHDiffCommand() *cobra.Command {
	options := &mlwhdiffOptions{}

	command := &cobra.Command{
		Use:   "mlwhdiff",
		Short: "Sequence metadata cache CLI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	command.PersistentFlags().StringVar(&options.dbPath, "db", "mlwhdiff.db", "SQLite database path")
	command.PersistentFlags().StringVar(&options.mlwhServerURL, "mlwh-server-url", firstEnv("WA_MLWH_SERVER_URL"), "Base URL for a remote MLWH server; defaults to WA_MLWH_SERVER_URL")
	command.PersistentFlags().StringVar(&options.mlwhCachePath, "mlwh-cache", "", "MLWH cache SQLite path or MySQL DSN without a password; defaults to WA_MLWH_CACHE_PATH when unset")

	command.AddCommand(newMLWHDiffDiffCommand(options))
	command.AddCommand(newMLWHDiffServeCommand(options))

	return command
}

func resolveMLWHDiffMLWHCachePath(flagValue string, flagChanged bool) (string, bool, error) {
	cachePath := strings.TrimSpace(flagValue)
	sourceName := "--mlwh-cache"
	if !flagChanged {
		if envValue := strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH")); envValue != "" {
			cachePath = envValue
			sourceName = "WA_MLWH_CACHE_PATH"
		}
	}

	if cachePath == "" {
		return "", false, nil
	}

	if !mlwhSyncCachePathLooksMySQL(cachePath) {
		return cachePath, true, nil
	}

	parsed, err := mysql.ParseDSN(cachePath)
	if err != nil {
		return "", false, fmt.Errorf("parse %s: %w", sourceName, err)
	}

	if parsed.Passwd != "" {
		return "", false, fmt.Errorf("%s: %w", sourceName, mlwh.ErrPasswordInDSN)
	}

	return cachePath, true, nil
}
