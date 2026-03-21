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

package saga

import (
	"context"
	"errors"
	"strconv"
)

// AnalysisType is a typed string for known iRODS analysis types.
type AnalysisType string

const (
	AnalysisCellrangerMulti  AnalysisType = "cellranger multi"
	AnalysisSpacerangerCount AnalysisType = "spaceranger count"
	AnalysisCellrangerCount  AnalysisType = "cellranger count"
	AnalysisCellrangerATAC   AnalysisType = "cellranger-atac count"
	AnalysisCellrangerARC    AnalysisType = "cellranger-arc count"
)

func irodsFileMatchesFilter(file IRODSFile, filter *FilterOptions) bool {
	if filter == nil {
		return true
	}

	if filter.AnalysisType != "" && !irodsFileMetadataMatchesAny(
		file,
		"analysis_type",
		[]string{string(filter.AnalysisType)},
	) {
		return false
	}

	for key, acceptableValues := range filter.Metadata {
		if !irodsFileMetadataMatchesAny(file, key, acceptableValues) {
			return false
		}
	}

	return true
}

func irodsFileMetadataMatchesAny(file IRODSFile, key string, acceptableValues []string) bool {
	if len(acceptableValues) == 0 {
		return false
	}

	acceptable := make(map[string]struct{}, len(acceptableValues))
	for _, value := range acceptableValues {
		acceptable[value] = struct{}{}
	}

	for _, metadata := range file.Metadata {
		if metadata.Name != key {
			continue
		}

		if _, ok := acceptable[metadata.Value]; ok {
			return true
		}
	}

	return false
}

func studyIRODSFileMatchesFilter(
	file IRODSFile,
	filter *FilterOptions,
	mlwhSample *MLWHSample,
) bool {
	if filter == nil {
		return true
	}

	if filter.AnalysisType != "" && !irodsFileMetadataMatchesAny(
		file,
		"analysis_type",
		[]string{string(filter.AnalysisType)},
	) {
		return false
	}

	for key, acceptableValues := range filter.Metadata {
		if irodsFileMetadataMatchesAny(file, key, acceptableValues) {
			continue
		}

		if mlwhSample == nil {
			return false
		}

		if !mlwhSampleMatchesFilter(*mlwhSample, key, acceptableValues) {
			return false
		}
	}

	return true
}

func mlwhSampleMatchesFilter(sample MLWHSample, key string, acceptableValues []string) bool {
	if len(acceptableValues) == 0 {
		return false
	}

	value, ok := mlwhSampleFilterValue(sample, key)
	if !ok {
		return false
	}

	for _, acceptableValue := range acceptableValues {
		if value == acceptableValue {
			return true
		}
	}

	return false
}

func mlwhSampleFilterValue(sample MLWHSample, key string) (string, bool) {
	switch key {
	case "id_study_lims":
		return sample.IDStudyLims, true
	case "id_sample_lims":
		return sample.IDSampleLims, true
	case "sanger_id":
		return sample.SangerID, true
	case "sample_name":
		return sample.SampleName, true
	case "taxon_id":
		return strconv.Itoa(sample.TaxonID), true
	case "common_name":
		return sample.CommonName, true
	case "library_type":
		return sample.LibraryType, true
	case "study_accession_number":
		return sample.StudyAccessionNumber, true
	case "accession_number":
		return sample.AccessionNumber, true
	default:
		return "", false
	}
}

// FilterOptions controls client-side filtering for key use cases.
type FilterOptions struct {
	AnalysisType AnalysisType
	Metadata     map[string][]string
}

// SampleFiles holds iRODS files associated with one sample.
type SampleFiles struct {
	SangerID string
	Files    []IRODSFile
}

// SampleIRODSFiles returns iRODS files for a sample after client-side filtering.
func (c *Client) SampleIRODSFiles(
	ctx context.Context,
	sangerID string,
	filter *FilterOptions,
) (*SampleFiles, error) {
	files, err := c.IRODS().GetSampleFiles(ctx, sangerID)
	if err != nil {
		return nil, err
	}

	return &SampleFiles{
		SangerID: sangerID,
		Files:    filterIRODSFiles(files, filter),
	}, nil
}

func filterIRODSFiles(files []IRODSFile, filter *FilterOptions) []IRODSFile {
	filtered := make([]IRODSFile, 0, len(files))

	for _, file := range files {
		if irodsFileMatchesFilter(file, filter) {
			filtered = append(filtered, file)
		}
	}

	return filtered
}

// StudyFiles holds iRODS files associated with one study.
type StudyFiles struct {
	StudyID string
	Files   []IRODSFile
}

// StudyIRODSFiles returns iRODS files for a study, falling back to MLWH study samples when needed.
func (c *Client) StudyIRODSFiles(
	ctx context.Context,
	studyID string,
	filter *FilterOptions,
) (*StudyFiles, error) {
	allIRODSSamples, err := c.IRODS().AllSamples(ctx)
	if err != nil {
		return nil, err
	}

	sangerIDs := irodsStudySampleIDs(allIRODSSamples, studyID)
	fallbackIDs, mlwhSamples, err := c.studyIRODSFallbackSamples(ctx, studyID, sangerIDs)
	if err != nil {
		return nil, err
	}

	files, err := c.studyIRODSFilesForSangerIDs(ctx, sangerIDs, filter, mlwhSamples)
	if err != nil {
		return nil, err
	}

	if len(fallbackIDs) > 0 {
		fallbackFiles, err := c.studyIRODSFilesForSangerIDs(ctx, fallbackIDs, filter, mlwhSamples)
		if err != nil {
			return nil, err
		}

		files = append(files, fallbackFiles...)
	}

	return &StudyFiles{
		StudyID: studyID,
		Files:   files,
	}, nil
}

func irodsStudySampleIDs(samples []IRODSSample, studyID string) []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0)

	for _, sample := range samples {
		if !stringsContain(irodsSampleStudyIDs(sample), studyID) {
			continue
		}

		for _, sangerID := range irodsSampleSangerIDs(sample) {
			if _, ok := seen[sangerID]; ok {
				continue
			}

			seen[sangerID] = struct{}{}
			ids = append(ids, sangerID)
		}
	}

	return ids
}

func stringsContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func irodsSampleStudyIDs(sample IRODSSample) []string {
	return irodsSampleDataStrings(sample, "avu:study_id")
}

func irodsSampleDataStrings(sample IRODSSample, key string) []string {
	if sample.Data == nil {
		return nil
	}

	value, ok := sample.Data[key]
	if !ok {
		return nil
	}

	return anyToStrings(value)
}

func anyToStrings(value any) []string {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil
		}

		return []string{typed}
	case []string:
		return append([]string(nil), typed...)
	case []any:
		values := make([]string, 0, len(typed))

		for _, item := range typed {
			stringValue, ok := item.(string)
			if !ok || stringValue == "" {
				continue
			}

			values = append(values, stringValue)
		}

		return values
	default:
		return nil
	}
}

func irodsSampleSangerIDs(sample IRODSSample) []string {
	sangerIDs := irodsSampleDataStrings(sample, "avu:sample")
	if len(sangerIDs) > 0 {
		return sangerIDs
	}

	if sample.SourceID == "" {
		return nil
	}

	return []string{sample.SourceID}
}

func (c *Client) studyIRODSFilesForSangerIDs(
	ctx context.Context,
	sangerIDs []string,
	filter *FilterOptions,
	mlwhSamples map[string]MLWHSample,
) ([]IRODSFile, error) {
	files := make([]IRODSFile, 0)

	for _, sangerID := range sangerIDs {
		sampleFiles, err := c.IRODS().GetSampleFiles(ctx, sangerID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}

			return nil, err
		}

		var mlwhSample *MLWHSample
		if sample, ok := mlwhSamples[sangerID]; ok {
			mlwhSample = &sample
		}

		files = append(files, filterStudyIRODSFiles(sampleFiles, filter, mlwhSample)...)
	}

	return files, nil
}

func filterStudyIRODSFiles(
	files []IRODSFile,
	filter *FilterOptions,
	mlwhSample *MLWHSample,
) []IRODSFile {
	filtered := make([]IRODSFile, 0, len(files))

	for _, file := range files {
		if studyIRODSFileMatchesFilter(file, filter, mlwhSample) {
			filtered = append(filtered, file)
		}
	}

	return filtered
}

func uniqueMLWHSamplesBySangerID(samples []MLWHSample) []MLWHSample {
	seen := make(map[string]struct{})
	unique := make([]MLWHSample, 0, len(samples))

	for _, sample := range samples {
		if sample.SangerID == "" {
			continue
		}

		if _, ok := seen[sample.SangerID]; ok {
			continue
		}

		seen[sample.SangerID] = struct{}{}
		unique = append(unique, sample)
	}

	return unique
}

func filterMLWHSamplesByStudyID(samples []MLWHSample, studyID string) []MLWHSample {
	filtered := make([]MLWHSample, 0)

	for _, sample := range samples {
		if sample.IDStudyLims == studyID {
			filtered = append(filtered, sample)
		}
	}

	return filtered
}

func filterMLWHSamplesBySangerID(samples []MLWHSample, sangerID string) []MLWHSample {
	filtered := make([]MLWHSample, 0)

	for _, sample := range samples {
		if sample.SangerID == sangerID {
			filtered = append(filtered, sample)
		}
	}

	return filtered
}

func collectIRODSAVUs(files []IRODSFile) map[string][]string {
	avus := make(map[string][]string)
	seen := make(map[string]map[string]struct{})

	for _, file := range files {
		for _, metadata := range file.Metadata {
			if _, ok := seen[metadata.Name]; !ok {
				seen[metadata.Name] = make(map[string]struct{})
			}

			if _, ok := seen[metadata.Name][metadata.Value]; ok {
				continue
			}

			seen[metadata.Name][metadata.Value] = struct{}{}
			avus[metadata.Name] = append(avus[metadata.Name], metadata.Value)
		}
	}

	return avus
}

func irodsFileStudyIDs(files []IRODSFile) []string {
	seen := make(map[string]struct{})
	studyIDs := make([]string, 0)

	for _, file := range files {
		for _, metadata := range file.Metadata {
			if metadata.Name != "study_id" && metadata.Name != "avu:study_id" {
				continue
			}

			if metadata.Value == "" {
				continue
			}

			if _, ok := seen[metadata.Value]; ok {
				continue
			}

			seen[metadata.Value] = struct{}{}
			studyIDs = append(studyIDs, metadata.Value)
		}
	}

	return studyIDs
}

func (c *Client) studyIRODSFallbackSamples(
	ctx context.Context,
	studyID string,
	directIDs []string,
) ([]string, map[string]MLWHSample, error) {
	studySamples, err := c.StudyAllSamples(ctx, studyID)
	if err != nil {
		return nil, nil, err
	}

	uniqueStudySamples := uniqueMLWHSamplesBySangerID(studySamples.Samples)
	if len(uniqueStudySamples) == 0 {
		return nil, nil, nil
	}

	seen := make(map[string]struct{}, len(directIDs))
	for _, sangerID := range directIDs {
		seen[sangerID] = struct{}{}
	}

	mlwhSamples := make(map[string]MLWHSample, len(uniqueStudySamples))
	fallbackIDs := make([]string, 0, len(uniqueStudySamples))

	for _, sample := range uniqueStudySamples {
		mlwhSamples[sample.SangerID] = sample

		if _, ok := seen[sample.SangerID]; ok {
			continue
		}

		fallbackIDs = append(fallbackIDs, sample.SangerID)
	}

	return fallbackIDs, mlwhSamples, nil
}

// SampleMetadata merges MLWH sample rows with iRODS file metadata for one sample.
type SampleMetadata struct {
	SangerID    string
	SampleName  string
	TaxonID     int
	CommonName  string
	LibraryType string
	StudyID     string
	MLWH        []MLWHSample
	IRODSFiles  []IRODSFile
	AVUs        map[string][]string
}

// SampleAllMetadata merges MLWH sample rows with iRODS file metadata for a Sanger sample ID.
func (c *Client) SampleAllMetadata(ctx context.Context, sangerID string) (*SampleMetadata, error) {
	irodsFiles, err := c.IRODS().GetSampleFiles(ctx, sangerID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}

		irodsFiles = []IRODSFile{}
	}

	mlwhSamples, err := c.sampleMetadataMLWHSamples(ctx, sangerID, irodsFiles)
	if err != nil {
		return nil, err
	}

	if len(mlwhSamples) == 0 && len(irodsFiles) == 0 {
		return nil, ErrNotFound
	}

	metadata := &SampleMetadata{
		SangerID:   sangerID,
		MLWH:       mlwhSamples,
		IRODSFiles: irodsFiles,
		AVUs:       collectIRODSAVUs(irodsFiles),
	}

	if len(mlwhSamples) > 0 {
		first := mlwhSamples[0]
		metadata.SampleName = first.SampleName
		metadata.TaxonID = first.TaxonID
		metadata.CommonName = first.CommonName
		metadata.LibraryType = first.LibraryType
		metadata.StudyID = first.IDStudyLims
	}

	return metadata, nil
}

func (c *Client) sampleMetadataMLWHSamples(
	ctx context.Context,
	sangerID string,
	irodsFiles []IRODSFile,
) ([]MLWHSample, error) {
	studyIDs := irodsFileStudyIDs(irodsFiles)
	if len(studyIDs) == 0 {
		allSamples, err := c.MLWH().AllSamples(ctx)
		if err != nil {
			return nil, err
		}

		return filterMLWHSamplesBySangerID(allSamples, sangerID), nil
	}

	mlwhSamples := make([]MLWHSample, 0)

	for _, studyID := range studyIDs {
		studySamples, err := c.StudyAllSamples(ctx, studyID)
		if err != nil {
			return nil, err
		}

		mlwhSamples = append(mlwhSamples, filterMLWHSamplesBySangerID(studySamples.Samples, sangerID)...)
	}

	return mlwhSamples, nil
}

// StudySamples holds all MLWH samples associated with one study.
type StudySamples struct {
	StudyID string
	Samples []MLWHSample
}

// StudyAllSamples returns all MLWH sample rows associated with a study.
func (c *Client) StudyAllSamples(ctx context.Context, studyID string) (*StudySamples, error) {
	allSamples, err := c.MLWH().AllSamplesForStudy(ctx, studyID)

	return &StudySamples{
		StudyID: studyID,
		Samples: filterMLWHSamplesByStudyID(allSamples, studyID),
	}, err
}
