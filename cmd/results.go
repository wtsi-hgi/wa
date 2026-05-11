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
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
	"github.com/wtsi-hgi/wa/results"

	_ "modernc.org/sqlite"
)

var resultsHTTPClient = &http.Client{Timeout: 30 * time.Second}

var resultsServeOpenMLWHClient = openResultsServeMLWHClientWithConfig

var resultsRegisterResolverOpener = openResultsRegisterResolver

var resultsServeNewTicker = func(interval time.Duration) resultsServeTicker {
	return &resultsServeRealTicker{ticker: time.NewTicker(interval)}
}

var resultsRegisterSeqmetaFlagMetaKeys = map[string]string{
	"run":     "seqmeta_runid",
	"study":   "seqmeta_studyid",
	"sample":  "seqmeta_sampleid",
	"library": "seqmeta_librarytype",
}

type resultsServeMLWHConfig struct {
	DSN       string
	CachePath string
}

func resolveResultsServeMLWHConfig(flagValue string, flagChanged bool) (resultsServeMLWHConfig, bool, error) {
	cachePath, hasCachePath, err := resolveResultsServeMLWHCachePath(flagValue, flagChanged)
	if err != nil {
		return resultsServeMLWHConfig{}, false, err
	}

	dsn := strings.TrimSpace(firstEnv("WA_MLWH_DSN"))
	if dsn == "" && !hasCachePath {
		return resultsServeMLWHConfig{}, false, nil
	}

	if dsn == "" {
		return resultsServeMLWHConfig{}, false, errors.New("WA_MLWH_DSN must be set")
	}

	if !hasCachePath {
		return resultsServeMLWHConfig{}, false, errors.New("WA_MLWH_CACHE_PATH or --mlwh-cache must be set")
	}

	resolvedDSN, err := mlwh.ResolveDSN(dsn, firstEnv("WA_MLWH_PASSWORD"))
	if err != nil {
		return resultsServeMLWHConfig{}, false, fmt.Errorf("WA_MLWH_DSN: %w", err)
	}

	return resultsServeMLWHConfig{
		DSN:       resolvedDSN,
		CachePath: cachePath,
	}, true, nil
}

func startResultsServeMLWHSync(ctx context.Context, client resultsServeSyncClient, interval time.Duration) func() {
	if interval <= 0 || client == nil {
		return func() {}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		ticker := resultsServeNewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				_, _ = client.Sync(ctx)
			}
		}
	}()

	return func() {
		<-done
	}
}

type resultsRegisterResolver interface {
	ResolveSample(context.Context, string) (mlwh.Match, error)
	ResolveStudy(context.Context, string) (mlwh.Match, error)
	ResolveRun(context.Context, string) (mlwh.Match, error)
	ResolveLibrary(context.Context, string) (mlwh.Match, error)
	Close() error
}

func openResultsRegisterResolver(ctx context.Context) (resultsRegisterResolver, error) {
	dsn := strings.TrimSpace(firstEnv("WA_MLWH_DSN"))
	if dsn == "" {
		return nil, errors.New("WA_MLWH_DSN is required to resolve --run/--study/--sample/--library")
	}

	cachePath := strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH"))
	if cachePath == "" {
		return nil, errors.New("WA_MLWH_CACHE_PATH is required to resolve --run/--study/--sample/--library")
	}

	client, err := mlwh.Open(ctx, mlwh.Config{
		DSN:      dsn,
		Password: firstEnv("WA_MLWH_PASSWORD"),
		Cache: mlwh.CacheConfig{
			Path:     cachePath,
			Password: firstEnv("WA_MLWH_CACHE_PASSWORD"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open mlwh resolver client: %w", err)
	}

	return client, nil
}

func resultsRegisterResolvedCanonical(flagName, value, canonical string) (string, error) {
	trimmed := strings.TrimSpace(canonical)
	if trimmed == "" {
		return "", fmt.Errorf("resolve %s %q: %w", flagName, value, mlwh.ErrNotFound)
	}

	return trimmed, nil
}

type resultsServeSyncClient interface {
	Sync(context.Context) ([]mlwh.SyncReport, error)
	ExpandIdentifier(context.Context, mlwh.IdentifierKind, string) ([]mlwh.TaggedID, error)
	LanesForSample(context.Context, string, int, int) ([]mlwh.Lane, error)
	Close() error
}

func openResultsServeMLWHClientWithConfig(ctx context.Context, cfg resultsServeMLWHConfig) (resultsServeSyncClient, error) {
	sourceDB, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open mlwh source db: %w", err)
	}

	if err = sourceDB.PingContext(ctx); err != nil {
		_ = sourceDB.Close()

		return nil, fmt.Errorf("ping mlwh source db: %w", err)
	}

	client, err := mlwh.Open(ctx, mlwh.Config{
		Cache: mlwh.CacheConfig{
			Path:     cfg.CachePath,
			Password: firstEnv("WA_MLWH_CACHE_PASSWORD"),
		},
		Source: sourceDB,
	})
	if err != nil {
		_ = sourceDB.Close()

		return nil, err
	}

	return &resultsServeMLWHRuntime{client: client, sourceDB: sourceDB}, nil
}

type resultsServeTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type resultsServeRealTicker struct {
	ticker *time.Ticker
}

func (t *resultsServeRealTicker) Chan() <-chan time.Time {
	return t.ticker.C
}

func (t *resultsServeRealTicker) Stop() {
	t.ticker.Stop()
}

type resultsServeMLWHRuntime struct {
	client   *mlwh.Client
	sourceDB *sql.DB
}

func (r *resultsServeMLWHRuntime) Sync(ctx context.Context) ([]mlwh.SyncReport, error) {
	return r.client.Sync(ctx)
}

func (r *resultsServeMLWHRuntime) ExpandIdentifier(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]mlwh.TaggedID, error) {
	return r.client.ExpandIdentifier(ctx, kind, canonical)
}

func (r *resultsServeMLWHRuntime) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	return r.client.LanesForSample(ctx, sangerName, limit, offset)
}

func (r *resultsServeMLWHRuntime) Close() error {
	var closeErrs []error
	if r.client != nil {
		closeErrs = append(closeErrs, r.client.Close())
	}
	if r.sourceDB != nil {
		closeErrs = append(closeErrs, r.sourceDB.Close())
	}

	return errors.Join(closeErrs...)
}

type resultsRegisterLookupValues struct {
	run     string
	study   string
	sample  string
	library string
}

func resolveResultsRegisterLookupMetadata(ctx context.Context, values resultsRegisterLookupValues) (map[string]string, error) {
	if strings.TrimSpace(values.run) == "" && strings.TrimSpace(values.study) == "" && strings.TrimSpace(values.sample) == "" && strings.TrimSpace(values.library) == "" {
		return nil, nil
	}

	client, err := resultsRegisterResolverOpener(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	metadata := make(map[string]string, 4)

	if trimmedRun := strings.TrimSpace(values.run); trimmedRun != "" {
		resolvedRunID, err := resolveResultsRegisterRunID(ctx, client, trimmedRun)
		if err != nil {
			return nil, err
		}

		metadata["seqmeta_runid"] = resolvedRunID
	}

	if trimmedStudy := strings.TrimSpace(values.study); trimmedStudy != "" {
		resolvedStudyID, err := resolveResultsRegisterStudyID(ctx, client, trimmedStudy)
		if err != nil {
			return nil, err
		}

		metadata["seqmeta_studyid"] = resolvedStudyID
	}

	if trimmedSample := strings.TrimSpace(values.sample); trimmedSample != "" {
		resolvedSampleID, err := resolveResultsRegisterSampleID(ctx, client, trimmedSample)
		if err != nil {
			return nil, err
		}

		metadata["seqmeta_sampleid"] = resolvedSampleID
	}

	if trimmedLibrary := strings.TrimSpace(values.library); trimmedLibrary != "" {
		resolvedLibraryType, err := resolveResultsRegisterLibraryType(ctx, client, trimmedLibrary)
		if err != nil {
			return nil, err
		}

		metadata["seqmeta_librarytype"] = resolvedLibraryType
	}

	return metadata, nil
}

func resolveResultsRegisterRunID(ctx context.Context, client resultsRegisterResolver, value string) (string, error) {
	match, err := client.ResolveRun(ctx, value)
	if err != nil {
		return "", fmt.Errorf("resolve --run %q: %w", value, err)
	}

	return resultsRegisterResolvedCanonical("--run", value, match.Canonical)
}

func resolveResultsRegisterStudyID(ctx context.Context, client resultsRegisterResolver, value string) (string, error) {
	match, err := client.ResolveStudy(ctx, value)
	if err != nil {
		return "", fmt.Errorf("resolve --study %q: %w", value, err)
	}

	return resultsRegisterResolvedCanonical("--study", value, match.Canonical)
}

func resolveResultsRegisterSampleID(ctx context.Context, client resultsRegisterResolver, value string) (string, error) {
	match, err := client.ResolveSample(ctx, value)
	if err != nil {
		return "", fmt.Errorf("resolve --sample %q: %w", value, err)
	}

	return resultsRegisterResolvedCanonical("--sample", value, match.Canonical)
}

func resolveResultsRegisterLibraryType(ctx context.Context, client resultsRegisterResolver, value string) (string, error) {
	match, err := client.ResolveLibrary(ctx, value)
	if err != nil {
		return "", fmt.Errorf("resolve --library %q: %w", value, err)
	}

	return resultsRegisterResolvedCanonical("--library", value, match.Canonical)
}

type resultSetWithFiles struct {
	results.ResultSet
	Files []results.FileEntry `json:"files"`
}

type resultsCommandOptions struct {
	serverURL string
}

func getResult(ctx context.Context, serverURL, resultID string, includeFiles bool) ([]byte, error) {
	resultPath := "/results/" + url.PathEscape(resultID)
	resultBody, err := getResultsResource(ctx, serverURL, resultPath, http.StatusOK, "get result")
	if err != nil {
		return nil, err
	}

	if !includeFiles {
		return resultBody, nil
	}

	var result results.ResultSet
	if err := json.Unmarshal(resultBody, &result); err != nil {
		return nil, fmt.Errorf("decode result response: %w", err)
	}

	filesBody, err := getResultsResource(ctx, serverURL, resultPath+"/files", http.StatusOK, "get result files")
	if err != nil {
		return nil, err
	}

	var files []results.FileEntry
	if err := json.Unmarshal(filesBody, &files); err != nil {
		return nil, fmt.Errorf("decode result files response: %w", err)
	}

	return marshalCommandJSON(resultSetWithFiles{ResultSet: result, Files: files})
}

func getResultsResource(ctx context.Context, serverURL, resourcePath string, successStatus int, operation string) ([]byte, error) {
	endpoint, err := resultsEndpointURL(serverURL, resourcePath)
	if err != nil {
		return nil, fmt.Errorf("parse --server URL: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create %s request: %w", operation, err)
	}

	response, err := resultsHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", operation, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", operation, err)
	}

	if response.StatusCode != successStatus {
		return nil, decodeResultsCommandError(response.StatusCode, body)
	}

	if !json.Valid(body) {
		return nil, fmt.Errorf("%s response was not valid JSON", operation)
	}

	return body, nil
}

func decodeResultsCommandError(statusCode int, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}

	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return fmt.Errorf("results server returned %d: %s", statusCode, payload.Error)
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return fmt.Errorf("results server returned %d", statusCode)
	}

	return fmt.Errorf("results server returned %d: %s", statusCode, trimmed)
}

func newResultsCommand() *cobra.Command {
	options := &resultsCommandOptions{}

	command := &cobra.Command{
		Use:   "results",
		Short: "Results REST API commands",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	command.PersistentFlags().StringVar(&options.serverURL, "server", defaultResultsServerURL(), "Results server base URL (defaults to the active WA_*_RESULTS_PORT)")

	command.AddCommand(newResultsRegisterCommand(options))
	command.AddCommand(newResultsSearchCommand(options))
	command.AddCommand(newResultsGetCommand(options))
	command.AddCommand(newResultsDeleteCommand(options))
	command.AddCommand(newResultsRescanCommand(options))
	command.AddCommand(newResultsServeCommand())

	return command
}

func activeResultsPort() string {
	switch firstEnv("WA_ENV") {
	case "test":
		return firstEnv("WA_TEST_RESULTS_PORT")
	case "development":
		return firstEnv("WA_DEV_RESULTS_PORT")
	case "production":
		return firstEnv("WA_PROD_RESULTS_PORT")
	default:
		return ""
	}
}

func resolveResultsServeDBDSN(flagValue string, flagChanged bool) (string, error) {
	dsn := strings.TrimSpace(flagValue)
	if !flagChanged {
		if envValue := firstEnv("WA_RESULTS_DB_PATH"); envValue != "" {
			dsn = envValue
		}
	}

	if dsn == "" {
		return "", errors.New("results database path or DSN must not be empty")
	}

	password := firstEnv("WA_RESULTS_DB_PASSWORD")

	return resolveResultsMySQLPassword(dsn, password, flagChanged)
}

func resolveResultsMySQLPassword(dsn string, password string, rejectEmbeddedPassword bool) (string, error) {
	trimmedDSN := strings.TrimSpace(dsn)
	if resultsDBDriverName(trimmedDSN) != "mysql" {
		return trimmedDSN, nil
	}

	config, err := mysql.ParseDSN(trimmedDSN)
	if err != nil {
		return "", fmt.Errorf("parse MySQL DSN: %w", err)
	}

	if config.Passwd != "" {
		if rejectEmbeddedPassword {
			return "", errors.New("MySQL database passwords must not be supplied on the command line; use WA_RESULTS_DB_PATH or WA_RESULTS_DB_PASSWORD instead")
		}

		return trimmedDSN, nil
	}

	if strings.TrimSpace(password) == "" {
		return trimmedDSN, nil
	}

	config.Passwd = password

	return config.FormatDSN(), nil
}

func validateResultsSQLiteDBPath(dsn string) error {
	trimmedDSN := strings.TrimSpace(dsn)
	if trimmedDSN == "" || trimmedDSN == ":memory:" || strings.HasPrefix(trimmedDSN, "file:") {
		return nil
	}

	dirPath := filepath.Dir(trimmedDSN)
	if dirPath == "." {
		return nil
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("results database directory does not exist: %s", dirPath)
		}

		return fmt.Errorf("results database directory cannot be used: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("results database directory cannot be used: %s is not a directory", dirPath)
	}

	return nil
}

func newResultsRegisterCommand(options *resultsCommandOptions) *cobra.Command {
	var requester string
	var operator string
	var commandLine string
	var workflowPath string
	var runID string
	var additionalUnique string
	var inputFiles []string
	var metaValues []string
	var lookupValues resultsRegisterLookupValues
	var includeHidden bool
	var useJSON bool

	command := &cobra.Command{
		Use:   "register [output-dir]",
		Short: "Register a result set",
		Long: strings.Join([]string{
			"Register a result set.",
			"",
			"The --run, --study, --sample and --library shorthands resolve through MLWH and store canonical seqmeta metadata keys.",
			"--sample accepts Sanger name, supplier name, id_sample_lims, sample UUID, or donor ID.",
			"--study accepts LIMS ID, accession, UUID, or name; --run accepts numeric run IDs.",
			"--library accepts exact pipeline_id_lims values, and the first call may block on a cold-cache sync, so pre-warm with wa mlwh sync.",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			registration, err := buildResultsRegistrationForCommand(
				ctx,
				cmd,
				args,
				requester,
				operator,
				commandLine,
				workflowPath,
				runID,
				additionalUnique,
				inputFiles,
				metaValues,
				lookupValues,
				includeHidden,
				useJSON,
			)
			if err != nil {
				return err
			}

			responseBody, err := registerResults(ctx, options.serverURL, registration)
			if err != nil {
				return err
			}

			return writeCommandJSON(cmd.OutOrStdout(), responseBody)
		},
	}

	command.Flags().StringVar(&requester, "user", "", "Requester name")
	command.Flags().StringVar(&operator, "operator", "", "Operator name")
	command.Flags().StringVar(&commandLine, "command", "", "Pipeline command line")
	command.Flags().StringVar(&workflowPath, "nextflow-workflow", "", "Path to the Nextflow workflow used for the run")
	command.Flags().StringVar(&runID, "runid", "", "Run identifier")
	command.Flags().StringVar(&additionalUnique, "additional-unique", "", "Additional value used to disambiguate the run key")
	command.Flags().StringArrayVar(&inputFiles, "input-file", nil, "Input file to track; may be supplied multiple times")
	command.Flags().StringArrayVar(&metaValues, "meta", nil, "Metadata value in key=value form; may be supplied multiple times")
	command.Flags().StringVar(&lookupValues.run, "run", "", "Resolve a numeric run ID through MLWH and store it as seqmeta_runid")
	command.Flags().StringVar(&lookupValues.study, "study", "", "Resolve a study LIMS ID, accession, UUID, or name through MLWH and store it as seqmeta_studyid")
	command.Flags().StringVar(&lookupValues.sample, "sample", "", "Resolve a sample Sanger name, supplier name, id_sample_lims, sample UUID, or donor ID through MLWH and store it as seqmeta_sampleid")
	command.Flags().StringVar(&lookupValues.library, "library", "", "Resolve an exact pipeline_id_lims through MLWH and store it as seqmeta_librarytype; the first call may block on a cold-cache sync, so pre-warm with wa mlwh sync")
	command.Flags().BoolVar(&includeHidden, "include-hidden", false, "Include hidden files and directories in the output scan")
	command.Flags().BoolVar(&useJSON, "json", false, "Read a registration JSON payload from stdin instead of scanning a directory")

	return command
}

func buildResultsRegistrationForCommand(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	requester string,
	operator string,
	commandLine string,
	workflowPath string,
	runID string,
	additionalUnique string,
	inputFiles []string,
	metaValues []string,
	lookupValues resultsRegisterLookupValues,
	includeHidden bool,
	useJSON bool,
) (*results.Registration, error) {
	if useJSON {
		if len(args) != 0 {
			return nil, errors.New("usage: register --json")
		}

		registration, err := decodeResultsRegistration(cmd.InOrStdin())
		if err != nil {
			return nil, err
		}

		if err := results.ValidateRegistration(registration); err != nil {
			return nil, err
		}

		return registration, nil
	}

	if len(args) != 1 {
		return nil, errors.New("usage: register [output-dir]")
	}

	if strings.TrimSpace(requester) == "" {
		return nil, errors.New("--user is required")
	}

	if strings.TrimSpace(workflowPath) == "" {
		return nil, errors.New("--nextflow-workflow is required")
	}

	runKey := results.BuildRunKey(strings.TrimSpace(runID), strings.TrimSpace(additionalUnique))
	if runKey == "" {
		return nil, errors.New("--runid or --additional-unique is required")
	}

	seqmetaMetadata, err := resolveResultsRegisterLookupMetadata(ctx, lookupValues)
	if err != nil {
		return nil, err
	}

	metadata, err := parseResultsRegisterMetadata(metaValues, seqmetaMetadata)
	if err != nil {
		return nil, err
	}

	outputDir, err := filepath.Abs(args[0])
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}

	if err := validateResultsScanRoot(outputDir, includeHidden); err != nil {
		return nil, err
	}

	outputFiles, scanWarnings, err := results.ScanDirectory(outputDir, includeHidden)
	if err != nil {
		return nil, fmt.Errorf("scan output directory: %w", err)
	}

	writeResultsScanWarnings(cmd.ErrOrStderr(), scanWarnings)

	pipelineIdentifier, pipelineName, pipelineVersion, err := results.DetectPipeline(workflowPath)
	if err != nil {
		return nil, fmt.Errorf("detect pipeline: %w", err)
	}

	trackedInputs, err := resultsRegisterInputFiles(inputFiles)
	if err != nil {
		return nil, err
	}

	pipelineFile, err := resultsRegisterPipelineFile(workflowPath)
	if err != nil {
		return nil, err
	}

	return &results.Registration{
		PipelineIdentifier: pipelineIdentifier,
		RunKey:             runKey,
		Requester:          strings.TrimSpace(requester),
		Operator:           strings.TrimSpace(operator),
		Command:            strings.TrimSpace(commandLine),
		PipelineName:       pipelineName,
		PipelineVersion:    pipelineVersion,
		OutputDirectory:    outputDir,
		Files:              deduplicateResultsTrackedFiles(outputFiles, trackedInputs, pipelineFile),
		Metadata:           metadata,
	}, nil
}

func decodeResultsRegistration(input io.Reader) (*results.Registration, error) {
	var registration results.Registration
	decoder := json.NewDecoder(input)
	if err := decoder.Decode(&registration); err != nil {
		return nil, fmt.Errorf("decode registration JSON: %w", err)
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("decode registration JSON: unexpected trailing JSON")
		}

		return nil, fmt.Errorf("decode registration JSON: %w", err)
	}

	return &registration, nil
}

func parseResultsRegisterMetadata(metaValues []string, seqmetaMetadata map[string]string) (map[string]string, error) {
	metadata, err := parseResultsMetadataFilters(metaValues)
	if err != nil {
		return nil, err
	}

	for key, value := range seqmetaMetadata {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			continue
		}

		if _, exists := metadata[key]; exists {
			return nil, fmt.Errorf("metadata key %q was supplied via both --meta and --%s", key, resultsRegisterSeqmetaFlagName(key))
		}

		metadata[key] = trimmedValue
	}

	return metadata, nil
}

func resultsRegisterSeqmetaFlagName(metaKey string) string {
	for flagName, key := range resultsRegisterSeqmetaFlagMetaKeys {
		if key == metaKey {
			return flagName
		}
	}

	return metaKey
}

func registerResults(ctx context.Context, serverURL string, registration *results.Registration) ([]byte, error) {
	body, err := marshalCommandJSON(registration)
	if err != nil {
		return nil, fmt.Errorf("marshal registration request: %w", err)
	}

	endpoint, err := resultsEndpointURL(serverURL, "/results")
	if err != nil {
		return nil, fmt.Errorf("parse --server URL: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create register request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := resultsHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request register result: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read register response: %w", err)
	}

	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return nil, decodeResultsCommandError(response.StatusCode, responseBody)
	}

	if !json.Valid(responseBody) {
		return nil, errors.New("results register response was not valid JSON")
	}

	return responseBody, nil
}

func newResultsSearchCommand(options *resultsCommandOptions) *cobra.Command {
	var requester string
	var operator string
	var pipelineName string
	var pipelineVersion string
	var pipelineIdentifier string
	var runKey string
	var outputDirPrefix string
	var metaValues []string

	command := &cobra.Command{
		Use:   "search",
		Short: "Search result sets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			query, err := buildResultsSearchQuery(requester, operator, pipelineName, pipelineVersion, pipelineIdentifier, runKey, outputDirPrefix, metaValues)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			responseBody, err := searchResults(ctx, options.serverURL, query)
			if err != nil {
				return err
			}

			_, err = cmd.OutOrStdout().Write(responseBody)

			return err
		},
	}

	command.Flags().StringVar(&requester, "user", "", "Requester filter")
	command.Flags().StringVar(&operator, "operator", "", "Operator filter")
	command.Flags().StringVar(&pipelineName, "pipeline-name", "", "Pipeline name filter")
	command.Flags().StringVar(&pipelineVersion, "pipeline-version", "", "Pipeline version filter")
	command.Flags().StringVar(&pipelineIdentifier, "pipeline-identifier", "", "Pipeline identifier filter")
	command.Flags().StringVar(&runKey, "run-key", "", "Run key filter")
	command.Flags().StringArrayVar(&metaValues, "meta", nil, "Metadata filter in key=value form")
	command.Flags().StringVar(&outputDirPrefix, "output-dir-prefix", "", "Output directory prefix filter")

	return command
}

func buildResultsSearchQuery(requester, operator, pipelineName, pipelineVersion, pipelineIdentifier, runKey, outputDirPrefix string, metaValues []string) (url.Values, error) {
	query := url.Values{}
	if requester != "" {
		query.Set("user", requester)
	}

	if operator != "" {
		query.Set("operator", operator)
	}

	if pipelineName != "" {
		query.Set("pipeline_name", pipelineName)
	}

	if pipelineVersion != "" {
		query.Set("pipeline_version", pipelineVersion)
	}

	if pipelineIdentifier != "" {
		query.Set("pipeline_identifier", pipelineIdentifier)
	}

	if runKey != "" {
		query.Set("run_key", runKey)
	}

	if outputDirPrefix != "" {
		query.Set("output_dir_prefix", outputDirPrefix)
	}

	metadata, err := parseResultsMetadataFilters(metaValues)
	if err != nil {
		return nil, err
	}

	for key, value := range metadata {
		query.Set("meta_"+key, value)
	}

	return query, nil
}

func parseResultsMetadataFilters(metaValues []string) (map[string]string, error) {
	metadata := make(map[string]string, len(metaValues))

	for _, metaValue := range metaValues {
		key, value, found := strings.Cut(metaValue, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !found || key == "" || value == "" {
			return nil, fmt.Errorf("invalid --meta value %q: expected key=value", metaValue)
		}

		metadata[key] = value
	}

	return metadata, nil
}

func searchResults(ctx context.Context, serverURL string, query url.Values) ([]byte, error) {
	endpoint, err := resultsEndpointURL(serverURL, "/results")
	if err != nil {
		return nil, fmt.Errorf("parse --server URL: %w", err)
	}
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create search request: %w", err)
	}

	response, err := resultsHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request search results: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read search response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return nil, decodeResultsCommandError(response.StatusCode, body)
	}

	if !json.Valid(body) {
		return nil, errors.New("results search response was not valid JSON")
	}

	return body, nil
}

func newResultsGetCommand(options *resultsCommandOptions) *cobra.Command {
	var includeFiles bool

	command := &cobra.Command{
		Use:   "get <id>",
		Short: "Get one result set",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("usage: get <id>")
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			responseBody, err := getResult(ctx, options.serverURL, args[0], includeFiles)
			if err != nil {
				return err
			}

			return writeCommandJSON(cmd.OutOrStdout(), responseBody)
		},
	}

	command.Flags().BoolVar(&includeFiles, "files", false, "Include the tracked file list in the response")

	return command
}

func newResultsDeleteCommand(options *resultsCommandOptions) *cobra.Command {

	command := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete one result set",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("usage: delete <id>")
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			return deleteResult(ctx, options.serverURL, args[0])
		},
	}

	return command
}

func deleteResult(ctx context.Context, serverURL, resultID string) error {
	endpoint, err := resultsEndpointURL(serverURL, "/results/"+url.PathEscape(resultID))
	if err != nil {
		return fmt.Errorf("parse --server URL: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}

	response, err := resultsHTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("request delete: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read delete response: %w", err)
	}

	if response.StatusCode != http.StatusNoContent {
		return decodeResultsCommandError(response.StatusCode, body)
	}

	return nil
}

func newResultsRescanCommand(options *resultsCommandOptions) *cobra.Command {
	var includeHidden bool

	command := &cobra.Command{
		Use:   "rescan <id> <dir>",
		Short: "Rescan registered output files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New("usage: rescan <id> <dir>")
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if err := validateResultsRescanDirectory(ctx, options.serverURL, args[0], args[1]); err != nil {
				return err
			}

			if err := validateResultsScanRoot(args[1], includeHidden); err != nil {
				return err
			}

			files, scanWarnings, err := results.ScanDirectory(args[1], includeHidden)
			if err != nil {
				return fmt.Errorf("scan output directory: %w", err)
			}

			writeResultsScanWarnings(cmd.ErrOrStderr(), scanWarnings)

			responseBody, err := rescanResults(ctx, options.serverURL, args[0], files)
			if err != nil {
				return err
			}

			if len(bytes.TrimSpace(responseBody)) == 0 {
				return nil
			}

			return writeCommandJSON(cmd.OutOrStdout(), responseBody)
		},
	}

	command.Flags().BoolVar(&includeHidden, "include-hidden", false, "Include hidden files and directories in the scan")

	return command
}

func rescanResults(ctx context.Context, serverURL, resultID string, files []results.FileEntry) ([]byte, error) {
	body, err := marshalCommandJSON(files)
	if err != nil {
		return nil, fmt.Errorf("marshal rescan request: %w", err)
	}

	endpoint, err := resultsEndpointURL(serverURL, "/results/"+url.PathEscape(resultID)+"/files")
	if err != nil {
		return nil, fmt.Errorf("parse --server URL: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rescan request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := resultsHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request rescan: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read rescan response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return nil, decodeResultsCommandError(response.StatusCode, responseBody)
	}

	return responseBody, nil
}

func defaultResultsServerURL() string {
	if port := activeResultsPort(); port != "" {
		return "http://127.0.0.1:" + port
	}

	return "http://localhost:8080"
}

func resultsEndpointURL(serverURL, resourcePath string) (*url.URL, error) {
	endpoint, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}

	endpoint.Path = path.Join(endpoint.Path, resourcePath)

	return endpoint, nil
}

func newResultsServeCommand() *cobra.Command {
	var port int
	var dbPath string
	var mlwhCache string
	var mlwhSyncInterval time.Duration
	var seqmetaURL string
	var seqmetaTimeout time.Duration

	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve the results HTTP API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := commandContext(cmd)

			dsn, err := resolveResultsServeDBDSN(dbPath, cmd.Flags().Changed("db"))
			if err != nil {
				return err
			}

			mlwhConfig, enableMLWH, err := resolveResultsServeMLWHConfig(mlwhCache, cmd.Flags().Changed("mlwh-cache"))
			if err != nil {
				return err
			}
			if mlwhSyncInterval > 0 && !enableMLWH {
				return errors.New("--mlwh-sync-interval requires MLWH configuration via WA_MLWH_DSN and WA_MLWH_CACHE_PATH or --mlwh-cache")
			}

			db, err := openResultsDB(dsn)
			if err != nil {
				return err
			}

			store, err := results.NewStore(db)
			if err != nil {
				_ = db.Close()

				return err
			}
			defer func() { _ = store.Close() }()

			var mlwhClient resultsServeSyncClient
			if enableMLWH {
				mlwhClient, err = resultsServeOpenMLWHClient(ctx, mlwhConfig)
				if err != nil {
					return fmt.Errorf("open mlwh client: %w", err)
				}
				defer func() { _ = mlwhClient.Close() }()
			}

			var validator *results.SeqmetaValidator
			var resolver results.SearchResolver
			if strings.TrimSpace(seqmetaURL) != "" {
				validator = results.NewSeqmetaValidator(seqmetaURL, seqmetaTimeout)
			}
			if mlwhClient != nil {
				resolver = results.NewMLWHSearchResolver(mlwhClient)
			}

			listener, err := listenFunc("tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				return err
			}
			defer func() { _ = listener.Close() }()

			httpServer := &http.Server{Handler: results.NewServer(store, validator, resolver).Handler()}
			serveCtx, cancelServe := context.WithCancel(ctx)
			waitForSync := startResultsServeMLWHSync(serveCtx, mlwhClient, mlwhSyncInterval)

			go func() {
				<-serveCtx.Done()
				_ = httpServer.Shutdown(context.Background())
			}()

			err = httpServer.Serve(listener)
			cancelServe()
			waitForSync()
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}

			return err
		},
	}

	command.Flags().IntVar(&port, "port", 8080, "Port to bind")
	command.Flags().StringVar(&dbPath, "db", "results.db", "SQLite database path or MySQL DSN without a password; defaults to WA_RESULTS_DB_PATH when unset")
	command.Flags().StringVar(&mlwhCache, "mlwh-cache", "", "MLWH cache backend path or MySQL DSN without a password; defaults to WA_MLWH_CACHE_PATH when unset")
	command.Flags().DurationVar(&mlwhSyncInterval, "mlwh-sync-interval", 0, "Opt-in background MLWH sync interval; zero disables the sync loop")
	command.Flags().StringVar(&seqmetaURL, "seqmeta-url", firstEnv("WA_SEQMETA_BACKEND_URL"), "Base URL for seqmeta validation (defaults to WA_SEQMETA_BACKEND_URL)")
	command.Flags().DurationVar(&seqmetaTimeout, "seqmeta-timeout", 30*time.Second, "Timeout for seqmeta validation requests")

	return command
}

func openResultsDB(dsn string) (*sql.DB, error) {
	driverName := resultsDBDriverName(dsn)
	if driverName == "sqlite" {
		if err := validateResultsSQLiteDBPath(dsn); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}

	if driverName == "sqlite" && dsn == ":memory:" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}

	return db, nil
}

func resultsDBDriverName(dsn string) string {
	trimmedDSN := strings.TrimSpace(dsn)
	if strings.Contains(trimmedDSN, "@tcp(") || strings.Contains(trimmedDSN, "@unix(") {
		return "mysql"
	}

	return "sqlite"
}

func resultsRegisterInputFiles(paths []string) ([]results.FileEntry, error) {
	entries := make([]results.FileEntry, 0, len(paths))

	for _, filePath := range paths {
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return nil, fmt.Errorf("resolve input file %q: %w", filePath, err)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("stat input file %q: %w", filePath, err)
		}

		if info.IsDir() {
			return nil, fmt.Errorf("input file %q: is a directory", filePath)
		}

		entries = append(entries, results.FileEntry{
			Path:  absPath,
			Mtime: info.ModTime(),
			Size:  info.Size(),
			Kind:  "input",
		})
	}

	return entries, nil
}

func writeResultsScanWarnings(output io.Writer, warnings int) {
	if warnings <= 0 || output == nil {
		return
	}

	_, _ = fmt.Fprintf(output, "warning: skipped %d path(s) while scanning output files\n", warnings)
}

func deduplicateResultsTrackedFiles(outputFiles, inputFiles []results.FileEntry, pipelineFile results.FileEntry) []results.FileEntry {
	files := append(append(append(make([]results.FileEntry, 0, len(outputFiles)+len(inputFiles)+1), outputFiles...), inputFiles...), pipelineFile)
	keepIndexByPath := make(map[string]int, len(files))
	for index, file := range files {
		keepIndexByPath[file.Path] = index
	}

	uniqueFiles := make([]results.FileEntry, 0, len(keepIndexByPath))
	for index, file := range files {
		if keepIndexByPath[file.Path] != index {
			continue
		}

		uniqueFiles = append(uniqueFiles, file)
	}

	return uniqueFiles
}

func validateResultsScanRoot(rootDir string, includeHidden bool) error {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil
	}

	visitedDirs := map[string]struct{}{resolvedRoot: {}}

	return validateResultsScanTree(absRoot, absRoot, resolvedRoot, includeHidden, visitedDirs)
}

func validateResultsRescanDirectory(ctx context.Context, serverURL, resultID, dir string) error {
	resultBody, err := getResult(ctx, serverURL, resultID, false)
	if err != nil {
		return err
	}

	var resultSet results.ResultSet
	if err := json.Unmarshal(resultBody, &resultSet); err != nil {
		return fmt.Errorf("decode result set: %w", err)
	}

	if !resultsSameCanonicalDirectory(dir, resultSet.OutputDirectory) {
		return fmt.Errorf("rescan directory %q does not match registered output directory %q", dir, resultSet.OutputDirectory)
	}

	return nil
}

func resultsSameCanonicalDirectory(firstPath, secondPath string) bool {
	firstAbs, firstErr := filepath.Abs(firstPath)
	secondAbs, secondErr := filepath.Abs(secondPath)
	if firstErr != nil || secondErr != nil {
		return false
	}

	firstResolved, firstResolvedOK := resolveResultsPath(firstAbs)
	secondResolved, secondResolvedOK := resolveResultsPath(secondAbs)
	if firstResolvedOK && secondResolvedOK {
		return firstResolved == secondResolved
	}

	return filepath.Clean(firstAbs) == filepath.Clean(secondAbs)
}

func resolveResultsPath(path string) (string, bool) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}

	return resolvedPath, true
}

func validateResultsScanTree(rootDir, dir, resolvedRoot string, includeHidden bool, visitedDirs map[string]struct{}) error {
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, child := range children {
		name := child.Name()
		if !includeHidden && strings.HasPrefix(name, ".") {
			continue
		}

		childPath := filepath.Join(dir, name)
		info, err := os.Stat(childPath)
		if err != nil || !info.IsDir() {
			continue
		}

		resolvedPath, err := filepath.EvalSymlinks(childPath)
		if err != nil {
			continue
		}

		if !resultsPathWithinDirectory(resolvedRoot, resolvedPath) {
			return fmt.Errorf("scan output directory: directory symlink %q resolves outside %q", childPath, rootDir)
		}

		if _, seen := visitedDirs[resolvedPath]; seen {
			continue
		}

		visitedDirs[resolvedPath] = struct{}{}
		if err := validateResultsScanTree(rootDir, childPath, resolvedRoot, includeHidden, visitedDirs); err != nil {
			return err
		}
	}

	return nil
}

func resultsPathWithinDirectory(rootPath, candidatePath string) bool {
	relPath, err := filepath.Rel(rootPath, candidatePath)
	if err != nil {
		return false
	}

	return relPath == "." || (relPath != ".." && !strings.HasPrefix(relPath, ".."+string(os.PathSeparator)))
}

func resultsRegisterPipelineFile(workflowPath string) (results.FileEntry, error) {
	absPath, err := filepath.Abs(workflowPath)
	if err != nil {
		return results.FileEntry{}, fmt.Errorf("resolve workflow file %q: %w", workflowPath, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return results.FileEntry{}, fmt.Errorf("stat workflow file %q: %w", workflowPath, err)
	}

	if info.IsDir() {
		return results.FileEntry{}, fmt.Errorf("workflow file %q: is a directory", workflowPath)
	}

	return results.FileEntry{
		Path:  absPath,
		Mtime: info.ModTime(),
		Size:  info.Size(),
		Kind:  "pipeline",
	}, nil
}

func resolveResultsServeMLWHCachePath(flagValue string, flagChanged bool) (string, bool, error) {
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
