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
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
	"github.com/wtsi-hgi/wa/seqmeta"
)

var listenFunc = net.Listen

const seqmetaProviderFetchLimit = 1_000_000

var openSeqmetaMLWHClient = mlwh.Open

var openSeqmetaClientFunc = func(ctx context.Context, cfg seqmetaMLWHConfig) (seqmetaCommandClient, error) {
	client, err := openSeqmetaMLWHClient(ctx, mlwh.Config{
		DSN:      cfg.DSN,
		Password: cfg.Password,
		Cache: mlwh.CacheConfig{
			Path:     cfg.CachePath,
			Password: cfg.CachePassword,
		},
	})
	if err != nil {
		return nil, err
	}

	return &seqmetaMLWHClientAdapter{client: client}, nil
}

var newSeqmetaSyncTicker = func(interval time.Duration) seqmetaTicker {
	return &seqmetaRealTicker{ticker: time.NewTicker(interval)}
}

var seqmetaSyncTables = []string{"sample", "study", "iseq_flowcell"}

type seqmetaMLWHConfig struct {
	DSN           string
	Password      string
	CachePath     string
	CachePassword string
}

func resolveSeqmetaMLWHConfig(options *seqmetaOptions, cacheFlagChanged bool) (seqmetaMLWHConfig, error) {
	dsn := strings.TrimSpace(firstEnv("WA_MLWH_DSN"))
	if dsn == "" {
		return seqmetaMLWHConfig{}, errors.New("WA_MLWH_DSN must be set")
	}

	validatedDSN, err := resolveSeqmetaMLWHDSN(dsn)
	if err != nil {
		return seqmetaMLWHConfig{}, fmt.Errorf("WA_MLWH_DSN: %w", err)
	}

	cachePath, err := resolveSeqmetaMLWHCachePath(options.mlwhCachePath, cacheFlagChanged)
	if err != nil {
		return seqmetaMLWHConfig{}, err
	}

	return seqmetaMLWHConfig{
		DSN:           validatedDSN,
		Password:      firstEnv("WA_MLWH_PASSWORD"),
		CachePath:     cachePath,
		CachePassword: firstEnv("WA_MLWH_CACHE_PASSWORD"),
	}, nil
}

type seqmetaCommandClient interface {
	seqmeta.Provider
	Sync(context.Context, ...string) ([]mlwh.SyncReport, error)
	Close() error
}

func openSeqmetaClient(ctx context.Context, options *seqmetaOptions, cacheFlagChanged bool) (seqmetaCommandClient, error) {
	cfg, err := resolveSeqmetaMLWHConfig(options, cacheFlagChanged)
	if err != nil {
		return nil, err
	}

	return openSeqmetaClientFunc(ctx, cfg)
}

func commandContext(cmd *cobra.Command) context.Context {
	if cmd == nil || cmd.Context() == nil {
		return context.Background()
	}

	return cmd.Context()
}

func seqmetaFlagChanged(cmd *cobra.Command, name string) bool {
	if cmd == nil {
		return false
	}

	flag := cmd.Flags().Lookup(name)

	return flag != nil && flag.Changed
}

func startSeqmetaSyncLoop(ctx context.Context, client seqmetaCommandClient, interval time.Duration) {
	if interval <= 0 || client == nil {
		return
	}

	ticker := newSeqmetaSyncTicker(interval)
	go func() {
		defer ticker.Stop()

		_, _ = client.Sync(ctx, seqmetaSyncTables...)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C():
				_, _ = client.Sync(ctx, seqmetaSyncTables...)
			}
		}
	}()
}

type seqmetaTicker interface {
	C() <-chan time.Time
	Stop()
}

type seqmetaRealTicker struct {
	ticker *time.Ticker
}

func (t *seqmetaRealTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t *seqmetaRealTicker) Stop() {
	t.ticker.Stop()
}

type seqmetaMLWHClientAdapter struct {
	client *mlwh.Client
}

type seqmetaLibraryLookup interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error)
	SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
}

type seqmetaDirectLibraryLookup interface {
	seqmetaLibraryLookup
	SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error)
}

func (a *seqmetaMLWHClientAdapter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if a == nil || a.client == nil || a.client.ReadDB() == nil {
		return nil, errors.New("seqmeta: mlwh client cache reader is not configured")
	}

	return a.client.ReadDB().QueryContext(ctx, query, args...)
}

func (a *seqmetaMLWHClientAdapter) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ClassifyIdentifier(ctx, raw)
}

func (a *seqmetaMLWHClientAdapter) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ResolveSample(ctx, raw)
}

func (a *seqmetaMLWHClientAdapter) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ResolveStudy(ctx, raw)
}

func (a *seqmetaMLWHClientAdapter) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ResolveRun(ctx, raw)
}

func (a *seqmetaMLWHClientAdapter) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	return a.client.ResolveLibrary(ctx, raw)
}

func (a *seqmetaMLWHClientAdapter) AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error) {
	return a.client.AllStudies(ctx, limit, offset)
}

func (a *seqmetaMLWHClientAdapter) GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error) {
	match, err := a.client.ResolveStudy(ctx, identifier)
	if err != nil {
		return nil, err
	}

	return match.Study, nil
}

func (a *seqmetaMLWHClientAdapter) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForStudy(ctx, studyLimsID, limit, offset)
}

func (a *seqmetaMLWHClientAdapter) AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error) {
	return a.client.SamplesForStudy(ctx, studyLimsID, seqmetaProviderFetchLimit, 0)
}

func (a *seqmetaMLWHClientAdapter) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error) {
	return a.findSingleSample(ctx, sangerID)
}

func (a *seqmetaMLWHClientAdapter) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error) {
	return a.findSingleSample(ctx, idSampleLims)
}

func (a *seqmetaMLWHClientAdapter) FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error) {
	return a.client.SamplesForRun(ctx, strconv.Itoa(idRun), seqmetaProviderFetchLimit, 0)
}

func (a *seqmetaMLWHClientAdapter) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]mlwh.Sample, error) {
	return findSamplesByLibraryType(ctx, a, libraryType, seqmeta.MaxLibrarySamples)
}

func (a *seqmetaMLWHClientAdapter) SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForLibraryType(ctx, pipelineIDLims, limit, offset)
}

func (a *seqmetaMLWHClientAdapter) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error) {
	return a.findSingleSample(ctx, accessionNumber)
}

func (a *seqmetaMLWHClientAdapter) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForRun(ctx, idRun, limit, offset)
}

func (a *seqmetaMLWHClientAdapter) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	return a.client.SamplesForLibrary(ctx, pipelineIDLims, studyLimsID, limit, offset)
}

func (a *seqmetaMLWHClientAdapter) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error) {
	return a.client.LibrariesForStudy(ctx, studyLimsID, limit, offset)
}

func (a *seqmetaMLWHClientAdapter) StudyForSample(ctx context.Context, sangerName string) (*mlwh.Study, error) {
	return a.client.StudyForSample(ctx, sangerName)
}

func (a *seqmetaMLWHClientAdapter) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	return a.client.LanesForSample(ctx, sangerName, limit, offset)
}

func (a *seqmetaMLWHClientAdapter) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	return a.client.IRODSPathsForSample(ctx, sangerName, limit, offset)
}

func (a *seqmetaMLWHClientAdapter) GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
	return a.client.IRODSPathsForSample(ctx, sangerName, seqmetaProviderFetchLimit, 0)
}

func (a *seqmetaMLWHClientAdapter) Sync(ctx context.Context, tables ...string) ([]mlwh.SyncReport, error) {
	return a.client.Sync(ctx, tables...)
}

func (a *seqmetaMLWHClientAdapter) Close() error {
	return a.client.Close()
}

func (a *seqmetaMLWHClientAdapter) findSingleSample(ctx context.Context, raw string) ([]mlwh.Sample, error) {
	match, err := a.client.ResolveSample(ctx, raw)
	if err != nil {
		return nil, err
	}
	if match.Sample == nil {
		return []mlwh.Sample{}, nil
	}

	return []mlwh.Sample{*match.Sample}, nil
}

func findSamplesByLibraryType(ctx context.Context, lookup seqmetaLibraryLookup, libraryType string, limit int) ([]mlwh.Sample, error) {
	if lookup == nil {
		return nil, errors.New("seqmeta: library lookup requires an adapter")
	}

	match, err := lookup.ResolveLibrary(ctx, libraryType)
	if err != nil {
		if errors.Is(err, mlwh.ErrNotFound) {
			return []mlwh.Sample{}, nil
		}

		return nil, err
	}

	canonical := libraryType
	if match.Canonical != "" {
		canonical = match.Canonical
	}

	if directLookup, ok := lookup.(seqmetaDirectLibraryLookup); ok {
		samples, directErr := directLookup.SamplesForLibraryType(ctx, canonical, limit, 0)
		if directErr != nil {
			return nil, directErr
		}

		if len(samples) > 0 {
			return samples, nil
		}
	}

	rows, err := lookup.QueryContext(
		ctx,
		`SELECT DISTINCT id_study_lims FROM library_samples WHERE pipeline_id_lims = ? ORDER BY id_study_lims`,
		canonical,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	studyLimsIDs := make([]string, 0)
	for rows.Next() {
		var studyLimsID string
		if err = rows.Scan(&studyLimsID); err != nil {
			return nil, err
		}

		studyLimsIDs = append(studyLimsIDs, studyLimsID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	samples := make([]mlwh.Sample, 0)
	for _, studyLimsID := range studyLimsIDs {
		studySamples, studyErr := lookup.SamplesForLibrary(ctx, canonical, studyLimsID, limit, 0)
		if studyErr != nil {
			return nil, studyErr
		}

		samples = append(samples, studySamples...)
	}

	return samples, nil
}

type seqmetaOptions struct {
	dbPath           string
	mlwhCachePath    string
	mlwhSyncInterval time.Duration
}

func newSeqmetaCommand() *cobra.Command {
	options := &seqmetaOptions{}

	command := &cobra.Command{
		Use:   "seqmeta",
		Short: "Sequence metadata cache CLI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	command.PersistentFlags().StringVar(&options.dbPath, "db", "seqmeta.db", "SQLite database path")
	command.PersistentFlags().StringVar(&options.mlwhCachePath, "mlwh-cache", "", "MLWH cache SQLite path or MySQL DSN without a password; defaults to WA_MLWH_CACHE_PATH when unset")
	command.PersistentFlags().DurationVar(&options.mlwhSyncInterval, "mlwh-sync-interval", 0, "Periodic MLWH sync interval; zero disables background sync")

	command.AddCommand(newSeqmetaDiffCommand(options))
	command.AddCommand(newSeqmetaValidateCommand(options))
	command.AddCommand(newSeqmetaServeCommand(options))

	return command
}

func newSeqmetaDiffCommand(options *seqmetaOptions) *cobra.Command {
	var studyID string
	var sampleID string

	command := &cobra.Command{
		Use:   "diff",
		Short: "Diff study samples or sample files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (studyID == "" && sampleID == "") || (studyID != "" && sampleID != "") {
				return errors.New("usage: specify exactly one of --study or --sample")
			}

			provider, err := openSeqmetaClient(commandContext(cmd), options, seqmetaFlagChanged(cmd, "mlwh-cache"))
			if err != nil {
				return err
			}
			defer func() { _ = provider.Close() }()

			store, err := seqmeta.OpenStore(options.dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			ctx := commandContext(cmd)

			if studyID != "" {
				samples, err := provider.AllSamplesForStudy(ctx, studyID)
				if err != nil {
					return err
				}

				return store.WithLock(func() error {
					prepared, err := seqmeta.PrepareDiff(store, "study_samples:"+studyID, samples, func(sample mlwh.Sample) string {
						return sample.Name
					})
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
				prepared, err := seqmeta.PrepareDiffSampleFiles(ctx, provider, store, sampleID)
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

func rollbackPreparedDiff[T any](prepared *seqmeta.PreparedDiff[T], writeErr error) error {
	if rollbackErr := prepared.Rollback(); rollbackErr != nil {
		return errors.Join(writeErr, rollbackErr)
	}

	return writeErr
}

func newSeqmetaValidateCommand(options *seqmetaOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "validate <identifier>",
		Short: "Validate and classify one identifier",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("usage: validate <identifier>")
			}

			provider, err := openSeqmetaClient(commandContext(cmd), options, seqmetaFlagChanged(cmd, "mlwh-cache"))
			if err != nil {
				return err
			}
			defer func() { _ = provider.Close() }()

			result, err := seqmeta.Validate(commandContext(cmd), provider, args[0])
			if err != nil {
				return err
			}

			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
}

func newSeqmetaServeCommand(options *seqmetaOptions) *cobra.Command {
	var port int

	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve the seqmeta HTTP API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			provider, err := openSeqmetaClient(commandContext(cmd), options, seqmetaFlagChanged(cmd, "mlwh-cache"))
			if err != nil {
				return err
			}
			defer func() { _ = provider.Close() }()

			store, err := seqmeta.OpenStore(options.dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			listener, err := listenFunc("tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				return err
			}
			defer func() { _ = listener.Close() }()

			httpServer := &http.Server{Handler: seqmeta.NewServer(provider, store).Handler()}
			ctx := commandContext(cmd)
			startSeqmetaSyncLoop(ctx, provider, options.mlwhSyncInterval)

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

func resolveSeqmetaMLWHDSN(dsn string) (string, error) {
	trimmedDSN := strings.TrimSpace(dsn)
	if trimmedDSN == "" {
		return "", errors.New("mlwh: dsn is required")
	}

	parsed, err := mysql.ParseDSN(trimmedDSN)
	if err != nil {
		return "", fmt.Errorf("parse MLWH DSN: %w", err)
	}

	if parsed.Passwd != "" {
		return "", mlwh.ErrPasswordInDSN
	}

	return parsed.FormatDSN(), nil
}

func resolveSeqmetaMLWHCachePath(flagValue string, flagChanged bool) (string, error) {
	cachePath := strings.TrimSpace(flagValue)
	sourceName := "--mlwh-cache"
	if !flagChanged {
		if envValue := strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH")); envValue != "" {
			cachePath = envValue
			sourceName = "WA_MLWH_CACHE_PATH"
		}
	}

	if cachePath == "" {
		return "", errors.New("WA_MLWH_CACHE_PATH must be set or --mlwh-cache provided")
	}

	if !mlwhSyncCachePathLooksMySQL(cachePath) {
		return cachePath, nil
	}

	parsed, err := mysql.ParseDSN(cachePath)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", sourceName, err)
	}

	if parsed.Passwd != "" {
		return "", fmt.Errorf("%s: %w", sourceName, mlwh.ErrPasswordInDSN)
	}

	return cachePath, nil
}
