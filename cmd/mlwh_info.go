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
	infoMaxRelated     = 50
	infoNotFoundExitFn = "mlwh: no matches"
)

var openMLWHInfoClient = func(ctx context.Context, cfg mlwh.Config) (mlwhInfoClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return mlwh.OpenCacheOnly(ctx, cfg.Cache)
	}

	return mlwh.Open(ctx, cfg)
}

var openMLWHInfoRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhInfoClient, error) {
	return mlwh.NewRemoteClient(cfg)
}

// mlwhInfoClient is the subset of *mlwh.Client used by `wa mlwh info`.
type mlwhInfoClient interface {
	ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveSample(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveRun(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error)
	FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error)
	FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error)
	FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error)

	StudiesForSample(ctx context.Context, sangerName string) ([]mlwh.Study, error)
	LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error)
	IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error)
	LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error)
	RunsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Run, error)
	SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
	SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error)
	SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)

	Close() error
}

func openMLWHInfoConfiguredClient(ctx context.Context, serverURL string) (mlwhInfoClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHInfoRemoteClient(ctx, mlwh.RemoteConfig{BaseURL: trimmedServerURL})
	}

	cfg, err := resolveMLWHInfoLocalConfig()
	if err != nil {
		return nil, err
	}

	client, err := openMLWHInfoClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.DSN) != "" && errors.Is(err, mlwh.ErrPasswordInDSN) {
			return nil, fmt.Errorf("WA_MLWH_DSN: %w", err)
		}

		return nil, err
	}

	return client, nil
}

func defaultMLWHInfoServerURL() string {
	if serverURL := strings.TrimSpace(firstEnv("WA_MLWH_SERVER_URL")); serverURL != "" {
		return serverURL
	}

	if backendURL := strings.TrimSpace(firstEnv("WA_MLWH_BACKEND_URL")); backendURL != "" {
		return backendURL
	}

	if port := strings.TrimSpace(activeMLWHPort()); port != "" {
		return "http://127.0.0.1:" + port
	}

	return ""
}

func activeMLWHPort() string {
	switch firstEnv("WA_ENV") {
	case "test":
		return firstEnv("WA_TEST_SEQMETA_PORT")
	case "development":
		return firstEnv("WA_DEV_SEQMETA_PORT")
	case "production":
		return firstEnv("WA_PROD_SEQMETA_PORT")
	default:
		return ""
	}
}

type mlwhInfoSampleNameResolver interface {
	ResolveSampleName(ctx context.Context, raw string) (mlwh.Match, error)
}

func runMLWHInfo(ctx context.Context, client mlwhInfoClient, out io.Writer, identifier, typeFlag string, jsonOut bool) error {
	match, err := classifyForInfo(ctx, client, identifier, typeFlag)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			return fmt.Errorf("resolve %q: %w", identifier, err)
		}

		if errors.Is(err, mlwh.ErrNotFound) {
			return fmt.Errorf("no match for identifier %q (run 'wa mlwh sync' if you think the cache is stale)", identifier)
		}

		return fmt.Errorf("resolve %q: %w (run 'wa mlwh sync' if your local cache is empty or stale)", identifier, err)
	}

	report := buildInfoReport(ctx, client, identifier, match)

	if jsonOut {
		return writeInfoReportJSON(out, report)
	}

	writeInfoReportText(out, report)

	return nil
}

func buildInfoReport(ctx context.Context, client mlwhInfoClient, identifier string, match mlwh.Match) infoReport {
	report := infoReport{
		Identifier: identifier,
		Kind:       string(match.Kind),
		Canonical:  match.Canonical,
		Sample:     match.Sample,
		Study:      match.Study,
		Run:        match.Run,
		Library:    match.Library,
	}

	switch {
	case match.Sample != nil:
		populateSampleInfo(ctx, client, &report, match.Sample)
	case match.Study != nil:
		populateStudyInfo(ctx, client, &report, match.Study.IDStudyLims)
	case match.Run != nil:
		if samples, err := client.SamplesForRun(ctx, fmt.Sprintf("%d", match.Run.IDRun), infoMaxRelated, 0); err == nil {
			report.Samples = samples
		} else {
			report.Warnings = append(report.Warnings, fmt.Sprintf("samples for run: %v", err))
		}
	case match.Library != nil:
		// Library Match has no parent study; samples can be listed once a
		// study LIMS id is known. Skip eager expansion to avoid surprising
		// upstream queries here; users can re-run with --type sample on a
		// specific sample to drill in.
	}

	return report
}

func writeInfoReportJSON(out io.Writer, report infoReport) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode info report: %w", err)
	}

	return nil
}

func writeInfoReportText(out io.Writer, report infoReport) {
	_, _ = fmt.Fprintf(out, "Identifier: %s\n", report.Identifier)
	_, _ = fmt.Fprintf(out, "Kind:       %s\n", report.Kind)

	if report.Canonical != "" {
		_, _ = fmt.Fprintf(out, "Canonical:  %s\n", report.Canonical)
	}

	if report.Sample != nil {
		writeSampleSection(out, report.Sample)
	}

	if report.Study != nil {
		writeStudySection(out, report.Study)
	}
	writeStudiesSection(out, report.Studies)

	if report.Run != nil {
		_, _ = fmt.Fprintf(out, "\nRun:\n  id_run: %d\n", report.Run.IDRun)
	}

	if report.Library != nil {
		_, _ = fmt.Fprintf(out, "\nLibrary:\n  pipeline_id_lims: %s\n",
			report.Library.PipelineIDLims)
		writeKV(out, "  id_study_lims", report.Library.IDStudyLims)
	}

	writeLanesSection(out, report.Lanes)
	writeLibrariesSection(out, report.Libraries)
	writeRunsSection(out, report.Runs)
	writeSamplesSection(out, report.Samples)
	writeIRODSPathsSection(out, report.IRODSPaths)
	writeWarningsSection(out, report.Warnings)
}

func writeSampleSection(out io.Writer, sample *mlwh.Sample) {
	_, _ = fmt.Fprintf(out, "\nSample:\n")
	writeKV(out, "  name", sample.Name)
	writeKV(out, "  id_sample_lims", sample.IDSampleLims)
	writeKV(out, "  uuid_sample_lims", sample.UUIDSampleLims)
	writeKV(out, "  sanger_sample_id", sample.SangerSampleID)
	writeKV(out, "  supplier_name", sample.SupplierName)
	writeKV(out, "  accession_number", sample.AccessionNumber)
	writeKV(out, "  donor_id", sample.DonorID)
	writeKV(out, "  common_name", sample.CommonName)
	for _, library := range sample.Libraries {
		if strings.TrimSpace(library.PipelineIDLims) == "" || strings.TrimSpace(library.IDStudyLims) == "" {
			continue
		}

		_, _ = fmt.Fprintf(out, "  library: %s / %s", library.PipelineIDLims, library.IDStudyLims)
		writeLibraryIdentifiers(out, library)
		_, _ = fmt.Fprintln(out)
	}
}

func writeLibraryIdentifiers(out io.Writer, library mlwh.Library) {
	if strings.TrimSpace(library.LibraryID) != "" {
		_, _ = fmt.Fprintf(out, " library_id=%s", library.LibraryID)
	}
	if strings.TrimSpace(library.IDLibraryLims) != "" {
		_, _ = fmt.Fprintf(out, " id_library_lims=%s", library.IDLibraryLims)
	}
}

func writeKV(out io.Writer, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}

	_, _ = fmt.Fprintf(out, "%s: %s\n", key, value)
}

func writeStudySection(out io.Writer, study *mlwh.Study) {
	_, _ = fmt.Fprintf(out, "\nStudy:\n")
	writeKV(out, "  id_study_lims", study.IDStudyLims)
	writeKV(out, "  uuid_study_lims", study.UUIDStudyLims)
	writeKV(out, "  name", study.Name)
	writeKV(out, "  accession_number", study.AccessionNumber)
	writeKV(out, "  study_title", study.StudyTitle)
	writeKV(out, "  faculty_sponsor", study.FacultySponsor)
	writeKV(out, "  programme", study.Programme)
}

func writeStudiesSection(out io.Writer, studies []mlwh.Study) {
	if len(studies) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\nStudies (%d):\n", len(studies))
	for _, study := range studies {
		_, _ = fmt.Fprintf(out, "  id_study_lims=%s", study.IDStudyLims)
		if strings.TrimSpace(study.Name) != "" {
			_, _ = fmt.Fprintf(out, " name=%s", study.Name)
		}
		if strings.TrimSpace(study.AccessionNumber) != "" {
			_, _ = fmt.Fprintf(out, " accession_number=%s", study.AccessionNumber)
		}
		_, _ = fmt.Fprintln(out)
	}
}

func writeLanesSection(out io.Writer, lanes []mlwh.Lane) {
	if len(lanes) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\nLanes (%d):\n", len(lanes))
	for _, lane := range lanes {
		_, _ = fmt.Fprintf(out, "  id_run=%d lane=%d tag_index=%d\n", lane.IDRun, lane.Position, lane.TagIndex)
	}
}

func writeLibrariesSection(out io.Writer, libraries []mlwh.Library) {
	if len(libraries) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\nLibraries (%d):\n", len(libraries))
	for _, lib := range libraries {
		_, _ = fmt.Fprintf(out, "  pipeline_id_lims=%s", lib.PipelineIDLims)
		if strings.TrimSpace(lib.IDStudyLims) != "" {
			_, _ = fmt.Fprintf(out, " id_study_lims=%s", lib.IDStudyLims)
		}
		writeLibraryIdentifiers(out, lib)
		_, _ = fmt.Fprintln(out)
	}
}

func writeRunsSection(out io.Writer, runs []mlwh.Run) {
	if len(runs) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\nRuns (%d):\n", len(runs))
	for _, run := range runs {
		_, _ = fmt.Fprintf(out, "  id_run=%d\n", run.IDRun)
	}
}

func writeSamplesSection(out io.Writer, samples []mlwh.Sample) {
	if len(samples) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\nSamples (%d):\n", len(samples))
	for _, sample := range samples {
		_, _ = fmt.Fprintf(out, "  name=%s id_sample_lims=%s sanger_sample_id=%s supplier_name=%s\n",
			sample.Name, sample.IDSampleLims, sample.SangerSampleID, sample.SupplierName)
	}
}

func writeIRODSPathsSection(out io.Writer, paths []mlwh.IRODSPath) {
	if len(paths) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\niRODS paths (%d):\n", len(paths))
	for _, path := range paths {
		_, _ = fmt.Fprintf(out, "  %s\n", path.IRODSPath)
	}
}

func writeWarningsSection(out io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\nWarnings:\n")
	for _, warning := range warnings {
		_, _ = fmt.Fprintf(out, "  %s\n", warning)
	}
}

func classifyForInfo(ctx context.Context, client mlwhInfoClient, identifier, typeFlag string) (mlwh.Match, error) {
	switch strings.ToLower(strings.TrimSpace(typeFlag)) {
	case "":
		if match, err := client.ResolveStudy(ctx, identifier); err == nil {
			return match, nil
		} else if !errors.Is(err, mlwh.ErrNotFound) {
			return mlwh.Match{}, err
		}

		if match, err := resolveSampleNameForInfo(ctx, client, identifier); err == nil {
			return match, nil
		} else if !errors.Is(err, mlwh.ErrNotFound) {
			return mlwh.Match{}, err
		}

		return client.ClassifyIdentifier(ctx, identifier)
	case "sample":
		if match, err := resolveSampleNameForInfo(ctx, client, identifier); err == nil {
			return match, nil
		} else if !errors.Is(err, mlwh.ErrNotFound) {
			return mlwh.Match{}, err
		}

		return client.ResolveSample(ctx, identifier)
	case "study":
		return client.ResolveStudy(ctx, identifier)
	case "run":
		return client.ResolveRun(ctx, identifier)
	case "library":
		return client.ResolveLibrary(ctx, identifier)
	default:
		return mlwh.Match{}, fmt.Errorf("unknown --type %q (expected sample, study, run or library)", typeFlag)
	}
}

func resolveSampleNameForInfo(ctx context.Context, client mlwhInfoClient, identifier string) (mlwh.Match, error) {
	nameResolver, ok := client.(mlwhInfoSampleNameResolver)
	if !ok {
		return mlwh.Match{}, mlwh.ErrNotFound
	}

	return nameResolver.ResolveSampleName(ctx, identifier)
}

func populateSampleInfo(ctx context.Context, client mlwhInfoClient, report *infoReport, sample *mlwh.Sample) {
	if err := refreshSampleFanOut(ctx, client, sample); err != nil && !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("sample fan-out: %v", err))
	}

	if report.Study == nil {
		studies := append([]mlwh.Study(nil), sample.Studies...)
		if len(studies) == 0 {
			var err error
			studies, err = client.StudiesForSample(ctx, sample.Name)
			if err != nil {
				if !errors.Is(err, mlwh.ErrNotFound) {
					report.Warnings = append(report.Warnings, fmt.Sprintf("studies for sample: %v", err))
				}

				studies = nil
			}
		}

		if len(studies) > 0 {
			switch len(studies) {
			case 1:
				report.Study = &studies[0]
			case 0:
				// Nothing to add.
			default:
				report.Studies = studies
			}
		}
	}

	if lanes, err := client.LanesForSample(ctx, sample.Name, infoMaxRelated, 0); err == nil {
		report.Lanes = lanes
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("lanes for sample: %v", err))
	}

	if paths, err := client.IRODSPathsForSample(ctx, sample.Name, infoMaxRelated, 0); err == nil {
		report.IRODSPaths = paths
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("irods paths for sample: %v", err))
	}
}

func refreshSampleFanOut(ctx context.Context, client mlwhInfoClient, sample *mlwh.Sample) error {
	if sample == nil {
		return nil
	}
	if len(sample.Libraries) > 0 && len(sample.Studies) > 0 {
		return nil
	}

	lookups := []func(context.Context) ([]mlwh.Sample, error){}
	if strings.TrimSpace(sample.SangerSampleID) != "" {
		lookups = append(lookups, func(ctx context.Context) ([]mlwh.Sample, error) {
			return client.FindSamplesBySangerID(ctx, sample.SangerSampleID)
		})
	}
	if strings.TrimSpace(sample.IDSampleLims) != "" {
		lookups = append(lookups, func(ctx context.Context) ([]mlwh.Sample, error) {
			return client.FindSamplesByIDSampleLims(ctx, sample.IDSampleLims)
		})
	}
	if strings.TrimSpace(sample.AccessionNumber) != "" {
		lookups = append(lookups, func(ctx context.Context) ([]mlwh.Sample, error) {
			return client.FindSamplesByAccessionNumber(ctx, sample.AccessionNumber)
		})
	}

	for _, lookup := range lookups {
		samples, err := lookup(ctx)
		if err != nil {
			if errors.Is(err, mlwh.ErrNotFound) {
				continue
			}

			return err
		}
		if len(samples) == 0 {
			continue
		}

		sample.Studies = append([]mlwh.Study(nil), samples[0].Studies...)
		sample.Libraries = append([]mlwh.Library(nil), samples[0].Libraries...)

		return nil
	}

	return mlwh.ErrNotFound
}

func populateStudyInfo(ctx context.Context, client mlwhInfoClient, report *infoReport, studyLimsID string) {
	if libs, err := client.LibrariesForStudy(ctx, studyLimsID, infoMaxRelated, 0); err == nil {
		report.Libraries = libs
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("libraries for study: %v", err))
	}

	if runs, err := client.RunsForStudy(ctx, studyLimsID, infoMaxRelated, 0); err == nil {
		report.Runs = runs
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("runs for study: %v", err))
	}

	if samples, err := client.SamplesForStudy(ctx, studyLimsID, infoMaxRelated, 0); err == nil {
		report.Samples = samples
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("samples for study: %v", err))
	}
}

// infoReport is the JSON-friendly shape of `wa mlwh info` results.
type infoReport struct {
	Identifier string           `json:"identifier"`
	Kind       string           `json:"kind"`
	Canonical  string           `json:"canonical,omitempty"`
	Sample     *mlwh.Sample     `json:"sample,omitempty"`
	Study      *mlwh.Study      `json:"study,omitempty"`
	Run        *mlwh.Run        `json:"run,omitempty"`
	Library    *mlwh.Library    `json:"library,omitempty"`
	Studies    []mlwh.Study     `json:"studies,omitempty"`
	Lanes      []mlwh.Lane      `json:"lanes,omitempty"`
	Libraries  []mlwh.Library   `json:"libraries,omitempty"`
	Runs       []mlwh.Run       `json:"runs,omitempty"`
	Samples    []mlwh.Sample    `json:"samples,omitempty"`
	IRODSPaths []mlwh.IRODSPath `json:"irods_paths,omitempty"`
	Warnings   []string         `json:"warnings,omitempty"`
}

func newMLWHInfoCommand() *cobra.Command {
	var (
		serverURL string
		typeFlag  string
		jsonOut   bool
	)

	command := &cobra.Command{
		Use:   "info <identifier>",
		Short: "Look up everything we know about an MLWH identifier",
		Long: strings.Join([]string{
			"Look up an MLWH identifier through a wa mlwh serve API and print",
			"every related record we have for it (sample fields, the parent",
			"study, library and run associations, lanes and iRODS paths).",
			"",
			"Use this when you have a sample name, sanger ID, supplier name,",
			"sample/study UUID, study LIMS ID or accession, library",
			"pipeline_id_lims, or numeric run id and want a quick human-readable",
			"or machine-readable view of what wa knows about it. By default",
			"the command auto-detects the identifier type; pass --type to force",
			"a specific resolver. Pass --json for a single JSON object suitable",
			"for piping into jq.",
			"",
			"Normal CLI users should point this command at the MLWH query",
			"server with --server or WA_MLWH_SERVER_URL; database and cache",
			"credentials stay with the server process. When WA_ENV selects a",
			"scenario and no server URL is set, the command defaults to the",
			"active local MLWH API port from WA_*_SEQMETA_PORT. Operators can",
			"still run against a local cache with WA_MLWH_CACHE_PATH, or use",
			"WA_MLWH_DSN for direct local operator mode.",
			"",
			"Configuration is read from the environment. Use the persistent",
			"--env flag (or WA_ENV=development|test|production) to load matching",
			".env.<name> / .env.<name>.local files from the working directory",
			"before resolving:",
			"",
			"  WA_MLWH_SERVER_URL      Preferred. Base URL for wa mlwh serve.",
			"  WA_MLWH_BACKEND_URL     Lower-precedence compatibility default.",
			"  WA_*_SEQMETA_PORT       Scenario-local default API port.",
			"  WA_MLWH_DSN             Optional direct operator mode only;",
			"                          required when running 'wa mlwh sync'.",
			"  WA_MLWH_PASSWORD        Optional. Password used with",
			"                          WA_MLWH_DSN when syncing from upstream.",
			"  WA_MLWH_CACHE_PATH      Optional local operator cache path or",
			"                          MySQL cache DSN without a password.",
			"  WA_MLWH_CACHE_PASSWORD  Optional. SQLCipher key used to encrypt",
			"                          the local cache when set.",
			"",
			"Examples:",
			"  # Query a development stack started by make dev",
			"  wa --env development mlwh info DN1234",
			"",
			"  # Query a remote MLWH server and emit JSON",
			"  wa mlwh info 5901 --server http://host:8091 --type study --json",
			"",
			"  # Local operator cache fallback",
			"  WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite wa mlwh info 49001 --type run",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("usage: wa mlwh info <identifier>")
			}

			identifier := strings.TrimSpace(args[0])
			if identifier == "" {
				return errors.New("usage: wa mlwh info <identifier>")
			}

			client, err := openMLWHInfoConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			return runMLWHInfo(cmd.Context(), client, cmd.OutOrStdout(), identifier, typeFlag, jsonOut)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().StringVar(&typeFlag, "type", "", "force identifier type (sample|study|run|library); default is auto-detect")
	command.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON object instead of human-readable text")

	return command
}

func resolveMLWHInfoLocalConfig() (mlwh.Config, error) {
	if strings.TrimSpace(firstEnv("WA_MLWH_DSN")) != "" {
		return resolveMLWHSyncConfig()
	}

	if strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH")) == "" {
		return mlwh.Config{}, errors.New("WA_MLWH_SERVER_URL or WA_MLWH_CACHE_PATH must be set; pass --server to use a remote wa mlwh serve instance")
	}

	cacheConfig, err := resolveMLWHServeCacheConfig("", false)
	if err != nil {
		return mlwh.Config{}, err
	}

	return mlwh.Config{Cache: cacheConfig}, nil
}
