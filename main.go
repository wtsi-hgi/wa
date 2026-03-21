package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wtsi-hgi/wa/saga"
)

const (
	largeResultThreshold = 10
	candidateLimit       = 3
)

type studyCandidate struct {
	Study  saga.Study
	Reason string
	Score  int
}

type sampleCandidate struct {
	SangerID string
	Reason   string
	Score    int
}

type inspector struct {
	client   *saga.Client
	warnings []string
}

var digitsOnly = regexp.MustCompile(`^\d+$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		apiKey  = flag.String("token", firstEnv("SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN"), "SAGA API token")
		baseURL = flag.String("base-url", "", "override SAGA API base URL")
		timeout = flag.Duration("timeout", 2*time.Minute, "overall request timeout")
	)

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] <study-or-sample>\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "")
		fmt.Fprintln(flag.CommandLine.Output(), "The token defaults to SAGA_API_TOKEN, then SAGA_TEST_API_TOKEN.")
		fmt.Fprintln(flag.CommandLine.Output(), "The identifier can be a study ID/name/title or a sample Sanger ID/name/LIMS ID.")
		fmt.Fprintln(flag.CommandLine.Output(), "")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()

		return errors.New("expected exactly one study or sample identifier")
	}

	query := strings.TrimSpace(flag.Arg(0))
	if query == "" {
		return errors.New("identifier must not be empty")
	}

	options := make([]saga.Option, 0, 1)
	if trimmedBaseURL := strings.TrimSpace(*baseURL); trimmedBaseURL != "" {
		options = append(options, saga.WithBaseURL(trimmedBaseURL))
	}

	client, err := saga.NewClient(strings.TrimSpace(*apiKey), options...)
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	found, err := (&inspector{client: client}).inspect(ctx, query)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("no study or sample matches found for %q", query)
	}

	return nil
}

func (i *inspector) inspect(ctx context.Context, query string) (bool, error) {
	studies, err := i.resolveStudies(ctx, query)
	if err != nil {
		return false, err
	}

	samples, err := i.resolveSamples(ctx, query)
	if err != nil {
		return false, err
	}

	if len(studies) == 0 && len(samples) == 0 {
		return false, nil
	}

	fmt.Printf("Query: %s\n", query)
	for _, warning := range i.warnings {
		fmt.Printf("warning: %s\n", warning)
	}

	if len(studies) > 0 {
		fmt.Printf("\nStudy matches: %d\n", len(studies))
		for _, candidate := range studies {
			i.inspectStudy(ctx, candidate)
		}
	}

	if len(samples) > 0 {
		fmt.Printf("\nSample matches: %d\n", len(samples))
		for _, candidate := range samples {
			i.inspectSample(ctx, candidate)
		}
	}

	return true, nil
}

func (i *inspector) resolveStudies(ctx context.Context, query string) ([]studyCandidate, error) {
	if !looksLikeStudySearch(query) {
		return nil, nil
	}

	if looksLikeDirectStudyID(query) {
		directStudy, err := i.client.MLWH().GetStudy(ctx, query)
		if err == nil {
			return []studyCandidate{{
				Study:  *directStudy,
				Reason: "exact match on study ID",
				Score:  100,
			}}, nil
		}

		if !errors.Is(err, saga.ErrNotFound) {
			i.warnf("direct MLWH study lookup for %q failed: %v; falling back to study list search", query, err)
		}
	}

	studies, err := i.client.MLWH().AllStudies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MLWH studies: %w", err)
	}

	candidates := make(map[string]studyCandidate)
	for _, study := range studies {
		score, reason := studyScore(query, study)
		if score == 0 {
			continue
		}

		existing, ok := candidates[study.IDStudyLims]
		if ok && existing.Score >= score {
			continue
		}

		candidates[study.IDStudyLims] = studyCandidate{
			Study:  study,
			Reason: reason,
			Score:  score,
		}
	}

	return topStudies(candidates), nil
}

func (i *inspector) resolveSamples(ctx context.Context, query string) ([]sampleCandidate, error) {
	candidates := make(map[string]sampleCandidate)

	files, err := i.client.IRODS().GetSampleFiles(ctx, query)
	if err == nil && len(files) > 0 {
		candidates[query] = sampleCandidate{
			SangerID: query,
			Reason:   "exact match on Sanger sample ID",
			Score:    100,
		}
	}

	if err != nil && !errors.Is(err, saga.ErrNotFound) {
		i.warnf("direct iRODS sample lookup for %q failed: %v; falling back to iRODS catalogue search", query, err)
	}

	irodsSamples, err := i.client.IRODS().AllSamples(ctx)
	if err != nil {
		if len(candidates) == 0 {
			return nil, fmt.Errorf("list iRODS samples: %w", err)
		}

		i.warnf("list iRODS samples failed: %v", err)

		return topSamples(candidates), nil
	}

	for _, sample := range irodsSamples {
		score, reason := irodsSampleScore(query, sample)
		if score == 0 {
			continue
		}

		for _, sangerID := range irodsSampleCandidateIDs(sample, query) {
			existing, ok := candidates[sangerID]
			if ok && existing.Score >= score {
				continue
			}

			candidates[sangerID] = sampleCandidate{
				SangerID: sangerID,
				Reason:   reason,
				Score:    score,
			}
		}
	}

	return topSamples(candidates), nil
}

func (i *inspector) inspectStudy(ctx context.Context, candidate studyCandidate) {
	studyID := candidate.Study.IDStudyLims
	if studyID == "" {
		studyID = candidate.Study.IDLims
	}

	fmt.Printf("\n=== Study %s ===\n", studyID)
	fmt.Printf("Match: %s\n", candidate.Reason)
	printValue("MLWH study", candidate.Study)

	sagaSamples, err := i.client.Samples().GetStudySamples(ctx, "MLWH", studyID)
	printQueryResult("Saga study samples", sagaSamples, err)

	studySamples, err := i.client.StudyAllSamples(ctx, studyID)
	printQueryResult("Merged MLWH study samples", studySamples, err)

	studyFiles, err := i.client.StudyIRODSFiles(ctx, studyID, nil)
	printQueryResult("Merged study iRODS files", studyFiles, err)
}

func (i *inspector) inspectSample(ctx context.Context, candidate sampleCandidate) {
	fmt.Printf("\n=== Sample %s ===\n", candidate.SangerID)
	fmt.Printf("Match: %s\n", candidate.Reason)

	metadata, err := i.client.SampleAllMetadata(ctx, candidate.SangerID)
	printQueryResult("Merged sample metadata", metadata, err)

	sampleFiles, err := i.client.SampleIRODSFiles(ctx, candidate.SangerID, nil)
	printQueryResult("Sample iRODS files", sampleFiles, err)
}

func printQueryResult(title string, value any, err error) {
	if err != nil {
		fmt.Printf("\n%s\n", title)
		fmt.Printf("error: %v\n", err)

		return
	}

	printValue(title, value)
}

func printValue(title string, value any) {
	fmt.Printf("\n%s\n", title)
	printJSON(summariseValue(value))
}

func summariseValue(value any) any {
	switch typed := value.(type) {
	case *saga.SampleMetadata:
		if typed == nil {
			return nil
		}

		if len(typed.MLWH) >= largeResultThreshold || len(typed.IRODSFiles) >= largeResultThreshold {
			return struct {
				SangerID         string           `json:"sanger_id"`
				SampleName       string           `json:"sample_name"`
				StudyID          string           `json:"study_id"`
				TaxonID          int              `json:"taxon_id"`
				CommonName       string           `json:"common_name"`
				LibraryType      string           `json:"library_type"`
				MLWHRowCount     int              `json:"mlwh_row_count"`
				IRODSFileCount   int              `json:"irods_file_count"`
				AvailableAVUKeys []string         `json:"available_avu_keys"`
				ExampleMLWHRow   *saga.MLWHSample `json:"example_mlwh_row,omitempty"`
				ExampleIRODSFile *saga.IRODSFile  `json:"example_irods_file,omitempty"`
			}{
				SangerID:         typed.SangerID,
				SampleName:       typed.SampleName,
				StudyID:          typed.StudyID,
				TaxonID:          typed.TaxonID,
				CommonName:       typed.CommonName,
				LibraryType:      typed.LibraryType,
				MLWHRowCount:     len(typed.MLWH),
				IRODSFileCount:   len(typed.IRODSFiles),
				AvailableAVUKeys: sortedKeys(typed.AVUs),
				ExampleMLWHRow:   firstMLWHSample(typed.MLWH),
				ExampleIRODSFile: firstIRODSFile(typed.IRODSFiles),
			}
		}
	case *saga.StudySamples:
		if typed == nil {
			return nil
		}

		if len(typed.Samples) >= largeResultThreshold {
			return struct {
				StudyID      string           `json:"study_id"`
				SampleCount  int              `json:"sample_count"`
				ExampleEntry *saga.MLWHSample `json:"example_entry,omitempty"`
			}{
				StudyID:      typed.StudyID,
				SampleCount:  len(typed.Samples),
				ExampleEntry: firstMLWHSample(typed.Samples),
			}
		}
	case *saga.SampleFiles:
		if typed == nil {
			return nil
		}

		if len(typed.Files) >= largeResultThreshold {
			return struct {
				SangerID    string          `json:"sanger_id"`
				FileCount   int             `json:"file_count"`
				ExampleFile *saga.IRODSFile `json:"example_file,omitempty"`
			}{
				SangerID:    typed.SangerID,
				FileCount:   len(typed.Files),
				ExampleFile: firstIRODSFile(typed.Files),
			}
		}
	case *saga.StudyFiles:
		if typed == nil {
			return nil
		}

		if len(typed.Files) >= largeResultThreshold {
			return struct {
				StudyID     string          `json:"study_id"`
				FileCount   int             `json:"file_count"`
				ExampleFile *saga.IRODSFile `json:"example_file,omitempty"`
			}{
				StudyID:     typed.StudyID,
				FileCount:   len(typed.Files),
				ExampleFile: firstIRODSFile(typed.Files),
			}
		}
	case []saga.SagaSample:
		if len(typed) >= largeResultThreshold {
			return struct {
				Count   int              `json:"count"`
				Example *saga.SagaSample `json:"example,omitempty"`
			}{
				Count:   len(typed),
				Example: firstSagaSample(typed),
			}
		}
	}

	return value
}

func printJSON(value any) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Printf("<marshal error: %v>\n", err)

		return
	}

	fmt.Printf("%s\n", encoded)
}

func studyScore(query string, study saga.Study) (int, string) {
	return bestScore(query, map[string]string{
		"study ID":         study.IDStudyLims,
		"legacy study ID":  study.IDLims,
		"name":             study.Name,
		"title":            study.StudyTitle,
		"abbreviation":     study.Abbreviation,
		"accession number": study.AccessionNumber,
		"programme":        study.Programme,
		"faculty sponsor":  study.FacultySponsor,
	})
}

func mlwhSampleScore(query string, sample saga.MLWHSample) (int, string) {
	return bestScore(query, map[string]string{
		"Sanger ID":               sample.SangerID,
		"sample name":             sample.SampleName,
		"sample LIMS ID":          sample.IDSampleLims,
		"study ID":                sample.IDStudyLims,
		"sample accession number": sample.AccessionNumber,
	})
}

func irodsSampleScore(query string, sample saga.IRODSSample) (int, string) {
	fields := map[string]string{
		"source ID": sample.SourceID,
		"name":      sample.Name,
		"source":    sample.Source,
	}

	for index, value := range searchableValues(sample.Data) {
		fields[fmt.Sprintf("data[%d]", index)] = value
	}

	for index, value := range searchableValues(sample.Curated) {
		fields[fmt.Sprintf("curated[%d]", index)] = value
	}

	return bestScore(query, fields)
}

func bestScore(query string, fields map[string]string) (int, string) {
	normalizedQuery := normalize(query)
	if normalizedQuery == "" {
		return 0, ""
	}

	bestScore := 0
	bestReason := ""

	for name, value := range fields {
		normalizedValue := normalize(value)
		if normalizedValue == "" {
			continue
		}

		score := 0
		reason := ""

		switch {
		case normalizedValue == normalizedQuery:
			score = 100
			reason = fmt.Sprintf("exact match on %s", name)
		case strings.Contains(normalizedValue, normalizedQuery):
			score = 40
			reason = fmt.Sprintf("partial match on %s", name)
		}

		if score > bestScore {
			bestScore = score
			bestReason = reason
		}
	}

	return bestScore, bestReason
}

func searchableValues(values map[string]any) []string {
	flattened := make([]string, 0)
	for _, value := range values {
		flattenValue(value, &flattened)
	}

	return flattened
}

func flattenValue(value any, flattened *[]string) {
	switch typed := value.(type) {
	case nil:
		return
	case string:
		if typed != "" {
			*flattened = append(*flattened, typed)
		}
	case []string:
		for _, item := range typed {
			flattenValue(item, flattened)
		}
	case []any:
		for _, item := range typed {
			flattenValue(item, flattened)
		}
	case map[string]any:
		for _, item := range typed {
			flattenValue(item, flattened)
		}
	default:
		*flattened = append(*flattened, fmt.Sprint(typed))
	}
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func topStudies(candidates map[string]studyCandidate) []studyCandidate {
	list := make([]studyCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		list = append(list, candidate)
	}

	sort.Slice(list, func(i int, j int) bool {
		if list[i].Score != list[j].Score {
			return list[i].Score > list[j].Score
		}

		return list[i].Study.IDStudyLims < list[j].Study.IDStudyLims
	})

	if len(list) > candidateLimit {
		return list[:candidateLimit]
	}

	return list
}

func topSamples(candidates map[string]sampleCandidate) []sampleCandidate {
	list := make([]sampleCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		list = append(list, candidate)
	}

	sort.Slice(list, func(i int, j int) bool {
		if list[i].Score != list[j].Score {
			return list[i].Score > list[j].Score
		}

		return list[i].SangerID < list[j].SangerID
	})

	if len(list) > candidateLimit {
		return list[:candidateLimit]
	}

	return list
}

func sortedKeys(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func firstSagaSample(samples []saga.SagaSample) *saga.SagaSample {
	if len(samples) == 0 {
		return nil
	}

	return &samples[0]
}

func firstIRODSFile(files []saga.IRODSFile) *saga.IRODSFile {
	if len(files) == 0 {
		return nil
	}

	return &files[0]
}

func firstMLWHSample(samples []saga.MLWHSample) *saga.MLWHSample {
	if len(samples) == 0 {
		return nil
	}

	return &samples[0]
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}

	return ""
}

func looksLikeDirectStudyID(query string) bool {
	return digitsOnly.MatchString(strings.TrimSpace(query))
}

func looksLikeStudySearch(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false
	}

	if looksLikeDirectStudyID(trimmed) {
		return true
	}

	upper := strings.ToUpper(trimmed)
	lower := strings.ToLower(trimmed)

	if strings.HasPrefix(upper, "EGAS") || strings.HasPrefix(upper, "SRP") || strings.HasPrefix(upper, "ERP") || strings.HasPrefix(upper, "DRP") || strings.HasPrefix(upper, "PRJ") {
		return true
	}

	if strings.Contains(lower, "study") || strings.ContainsAny(trimmed, " \t/():,") {
		return true
	}

	return lettersOnly(trimmed)
}

func lettersOnly(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}

		return false
	}

	return value != ""
}

func (i *inspector) warnf(format string, args ...any) {
	i.warnings = append(i.warnings, fmt.Sprintf(format, args...))
}

func irodsSampleCandidateIDs(sample saga.IRODSSample, query string) []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0)

	for _, value := range stringsFromAny(sample.Curated["sanger_id"]) {
		ids = appendUniqueString(ids, seen, value)
	}

	for _, value := range stringsFromAny(sample.Data["avu:sample"]) {
		ids = appendUniqueString(ids, seen, value)
	}

	if sample.SourceID != "" && (normalize(sample.SourceID) == normalize(query) || !digitsOnly.MatchString(sample.SourceID)) {
		ids = appendUniqueString(ids, seen, sample.SourceID)
	}

	return ids
}

func appendUniqueString(values []string, seen map[string]struct{}, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return values
	}

	if _, ok := seen[trimmed]; ok {
		return values
	}

	seen[trimmed] = struct{}{}

	return append(values, trimmed)
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}

		return []string{typed}
	case []string:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if strings.TrimSpace(item) == "" {
				continue
			}

			values = append(values, item)
		}

		return values
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			stringValue, ok := item.(string)
			if !ok || strings.TrimSpace(stringValue) == "" {
				continue
			}

			values = append(values, stringValue)
		}

		return values
	default:
		return nil
	}
}
