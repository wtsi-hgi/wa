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

package seqmeta

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/wtsi-hgi/wa/mlwh"
)

type enrichClassifier struct {
	classify func(context.Context, Provider, string) (*EnrichmentResult, bool, []MissingHop, error)
}

var enrichClassifiers = []enrichClassifier{
	{classify: classifyStudyID},
	{classify: classifyStudyAccession},
	{classify: classifyRunID},
	{classify: classifySangerSampleID},
	{classify: classifySampleLimsID},
	{classify: classifySampleAccession},
	{classify: classifyLibraryType},
}

type enrichDetailProvider interface {
	StudyDetail(ctx context.Context, studyLimsID string) (*mlwh.StudyDetail, error)
	SampleDetail(ctx context.Context, sampleName string) (*mlwh.SampleDetail, error)
	RunDetail(ctx context.Context, runID string) (*mlwh.RunDetail, error)
}

func studyDetailFor(ctx context.Context, provider Provider, studyLimsID string) (*mlwh.StudyDetail, error) {
	if detailProvider, ok := provider.(enrichDetailProvider); ok {
		detail, err := detailProvider.StudyDetail(ctx, studyLimsID)
		if detail != nil || err != nil {
			return detail, err
		}
	}

	return buildStudyDetailFromProvider(ctx, provider, studyLimsID)
}

func buildStudyDetailFromProvider(ctx context.Context, provider Provider, studyLimsID string) (*mlwh.StudyDetail, error) {
	study, err := provider.GetStudy(ctx, studyLimsID)
	if err != nil {
		return nil, err
	}
	if study == nil {
		return nil, mlwh.ErrNotFound
	}

	samples, err := provider.AllSamplesForStudy(ctx, studyLimsID)
	if err != nil {
		return nil, err
	}

	detail := buildStudyDetail(*study, samples)

	return &detail, nil
}

func sampleDetailFor(ctx context.Context, provider Provider, sample mlwh.Sample) (*mlwh.SampleDetail, error) {
	if detailProvider, ok := provider.(enrichDetailProvider); ok {
		detail, err := detailProvider.SampleDetail(ctx, sampleLookupKey(sample))
		if detail != nil || err != nil {
			return detail, err
		}
	}

	return buildSampleDetailFromProvider(ctx, provider, sample)
}

func sampleLookupKey(sample mlwh.Sample) string {
	switch {
	case sample.SangerID != "":
		return sample.SangerID
	case sample.Name != "":
		return sample.Name
	case sample.IDSampleLims != "":
		return sample.IDSampleLims
	default:
		return sample.AccessionNumber
	}
}

func buildSampleDetailFromProvider(ctx context.Context, provider Provider, sample mlwh.Sample) (*mlwh.SampleDetail, error) {
	lanes, err := provider.LanesForSample(ctx, sample.Name, providerFetchLimit, 0)
	if err != nil && !errors.Is(err, mlwh.ErrNotFound) {
		return nil, err
	}
	if errors.Is(err, mlwh.ErrNotFound) {
		lanes = nil
	}

	paths, err := provider.IRODSPathsForSample(ctx, sample.Name, providerFetchLimit, 0)
	if err != nil && !errors.Is(err, mlwh.ErrNotFound) {
		return nil, err
	}
	if errors.Is(err, mlwh.ErrNotFound) {
		paths = nil
	}

	libraries := make([]mlwh.Library, 0, 1)
	if sample.LibraryType != "" {
		libraries = append(libraries, mlwh.Library{PipelineIDLims: sample.LibraryType, SampleCount: 1})
	}

	return &mlwh.SampleDetail{
		Sample:     sample,
		Lanes:      lanes,
		Libraries:  libraries,
		IRODSPaths: paths,
	}, nil
}

func runDetailFor(ctx context.Context, provider Provider, runID string, samples []mlwh.Sample) (*mlwh.RunDetail, error) {
	if detailProvider, ok := provider.(enrichDetailProvider); ok {
		detail, err := detailProvider.RunDetail(ctx, runID)
		if detail != nil || err != nil {
			return detail, err
		}
	}

	return buildRunDetailFromProvider(ctx, provider, runID, samples)
}

func buildRunDetailFromProvider(ctx context.Context, provider Provider, runID string, samples []mlwh.Sample) (*mlwh.RunDetail, error) {
	studies, err := studiesForSamples(ctx, provider, samples)
	if err != nil {
		return nil, err
	}

	parsedRunID, parseErr := strconv.Atoi(runID)
	if parseErr != nil {
		parsedRunID = 0
	}

	return &mlwh.RunDetail{
		Run:          mlwh.Run{IDRun: parsedRunID},
		Samples:      samples,
		Studies:      studies,
		StudyDetails: buildStudyDetails(studies, samples),
	}, nil
}

func studiesForSamples(ctx context.Context, provider Provider, samples []mlwh.Sample) ([]mlwh.Study, error) {
	studyIDs := make([]string, 0, len(samples))
	seen := make(map[string]struct{}, len(samples))

	for _, sample := range samples {
		if sample.IDStudyLims == "" {
			continue
		}

		if _, exists := seen[sample.IDStudyLims]; exists {
			continue
		}

		seen[sample.IDStudyLims] = struct{}{}
		studyIDs = append(studyIDs, sample.IDStudyLims)
	}

	studies := make([]mlwh.Study, 0, len(studyIDs))
	for _, studyID := range studyIDs {
		study, err := provider.GetStudy(ctx, studyID)
		if err != nil {
			return nil, err
		}
		if study == nil {
			continue
		}

		studies = append(studies, *study)
	}

	return studies, nil
}

type sampleClassifier func(context.Context, Provider, string) ([]mlwh.Sample, error)

func classifySampleIdentifier(
	ctx context.Context,
	provider Provider,
	identifier string,
	identifierType IdentifierType,
	lookup sampleClassifier,
) (*EnrichmentResult, bool, []MissingHop, error) {
	samples, err := lookup(ctx, provider, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	if len(samples) == 0 {
		return nil, false, nil, nil
	}

	sample := samples[0]
	detail, err := sampleDetailFor(ctx, provider, sample)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       identifierType,
		Graph: EnrichmentGraph{
			Sample:       &detail.Sample,
			Samples:      samples,
			SampleDetail: detail,
			Library:      libraryLinkForSample(detail.Sample),
		},
	}

	if detail.Study != nil {
		result.Graph.Study = detail.Study
	} else if err = enrichSampleStudy(ctx, provider, detail.Sample, result); err != nil {
		return nil, false, nil, err
	}

	return result, true, nil, nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func isClientError(err error) bool {
	return errors.Is(err, mlwh.ErrNotFound) || errors.Is(err, mlwh.ErrUnsupportedIdentifier)
}

func missingHop(hop string, err error) MissingHop {
	status := http.StatusBadGateway
	reason := ReasonUpstreamError

	if isClientError(err) {
		status = http.StatusNotFound
		reason = ReasonNotFound
	}

	return MissingHop{Hop: hop, Reason: reason, Status: status}
}

func libraryLinkForSample(sample mlwh.Sample) *Library {
	if sample.LibraryType == "" {
		return nil
	}

	return &Library{LibraryType: sample.LibraryType, IDStudyLims: sample.IDStudyLims}
}

func enrichSampleStudy(ctx context.Context, provider Provider, sample mlwh.Sample, result *EnrichmentResult) error {
	if result == nil {
		return nil
	}

	study, err := provider.StudyForSample(ctx, sample.Name)
	if err != nil {
		if isContextError(err) {
			return err
		}

		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopStudy, err))

		return nil
	}

	result.Graph.Study = study
	if result.Graph.SampleDetail != nil {
		result.Graph.SampleDetail.Study = study
	}

	return nil
}

// Enrich resolves an identifier into its enrichment graph.
func Enrich(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, error) {
	if identifier == "" {
		return nil, ErrUnknownIdentifier
	}
	if looksLikeLibraryType(identifier) {
		result, matched, missing, err := classifyLibraryType(ctx, provider, identifier)
		if err != nil {
			return nil, err
		}
		if matched {
			if len(missing) > 0 {
				result.Missing = append(missing, result.Missing...)
				result.Partial = true
			}

			return result, nil
		}

		if allClassificationHopsFailed(identifier, missing) {
			return nil, &enrichError{err: ErrAllHopsFailed, missing: missing}
		}

		return nil, ErrUnknownIdentifier
	}

	missing := make([]MissingHop, 0, len(enrichClassifiers))

	for _, classifier := range enrichClassifiers {
		result, matched, classifyMissing, err := classifier.classify(ctx, provider, identifier)
		if err != nil {
			return nil, err
		}

		missing = append(missing, classifyMissing...)

		if matched {
			if len(missing) > 0 {
				result.Missing = append(missing, result.Missing...)
				result.Partial = true
			}

			return result, nil
		}
	}

	if allClassificationHopsFailed(identifier, missing) {
		return nil, &enrichError{err: ErrAllHopsFailed, missing: missing}
	}

	return nil, ErrUnknownIdentifier
}

func allClassificationHopsFailed(identifier string, missing []MissingHop) bool {
	expectedFailures := len(enrichClassifiers)
	if _, err := strconv.Atoi(identifier); err != nil {
		expectedFailures--
	}

	if len(missing) != expectedFailures {
		return false
	}

	for _, hop := range missing {
		if hop.Hop != HopClassify || hop.Reason != ReasonUpstreamError {
			return false
		}
	}

	return true
}

func classifyStudyID(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	study, err := provider.GetStudy(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	if study == nil {
		return nil, false, nil, nil
	}

	result := &EnrichmentResult{Identifier: identifier, Type: IdentifierStudyID}
	if err = enrichStudy(ctx, provider, study, result); err != nil {
		return nil, false, nil, err
	}

	return result, true, nil, nil
}

func classifyStudyAccession(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	if looksLikeLibraryType(identifier) {
		return nil, false, nil, nil
	}

	studies, err := listAllStudies(ctx, provider)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	for _, candidate := range studies {
		if candidate.AccessionNumber != identifier {
			continue
		}

		study := candidate
		result := &EnrichmentResult{Identifier: identifier, Type: IdentifierStudyAccession}
		if err = enrichStudy(ctx, provider, &study, result); err != nil {
			return nil, false, nil, err
		}

		return result, true, nil, nil
	}

	return nil, false, nil, nil
}

func enrichStudy(ctx context.Context, provider Provider, study *mlwh.Study, result *EnrichmentResult) error {
	if study == nil || result == nil {
		return nil
	}

	detail, err := studyDetailFor(ctx, provider, study.IDStudyLims)
	if err != nil {
		if isContextError(err) {
			return err
		}

		result.Graph.Study = study
		result.Graph.Studies = []mlwh.Study{*study}
		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopSamples, err))

		return nil
	}

	result.Graph.Study = &detail.Study
	result.Graph.Studies = []mlwh.Study{detail.Study}
	result.Graph.Samples = flattenStudySamples(detail)
	result.Graph.Libraries = flatLibrariesFromStudyDetail(detail)
	result.Graph.StudyDetail = detail

	return nil
}

func classifyRunID(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	runID, err := strconv.Atoi(identifier)
	if err != nil {
		return nil, false, nil, nil
	}

	samples, err := provider.FindSamplesByRunID(ctx, runID)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	if len(samples) == 0 {
		return nil, false, nil, nil
	}

	runDetail, detailErr := runDetailFor(ctx, provider, identifier, samples)
	if detailErr != nil {
		if isContextError(detailErr) {
			return nil, false, nil, detailErr
		}

		runDetail = &mlwh.RunDetail{Run: mlwh.Run{IDRun: runID}, Samples: samples}
	}

	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       IdentifierRunID,
		Graph: EnrichmentGraph{
			Samples: samples,
		},
	}

	result.Graph.Samples = runDetail.Samples
	result.Graph.Studies = runDetail.Studies
	result.Graph.Libraries = distinctLibrariesForSamples(runDetail.Samples)
	result.Graph.StudyDetails = runDetail.StudyDetails

	if detailErr != nil {
		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopStudies, detailErr))
	}

	return result, true, nil, nil
}

func classifyLibraryType(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	if !looksLikeLibraryType(identifier) {
		return nil, false, nil, nil
	}

	samples, err := provider.FindSamplesByLibraryType(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	if len(samples) == 0 {
		return nil, false, nil, nil
	}

	studyDetails, missing, detailErr := libraryStudyDetails(ctx, provider, identifier, samples)
	if detailErr != nil {
		if isContextError(detailErr) {
			return nil, false, nil, detailErr
		}

		return nil, false, []MissingHop{missingHop(HopLibraries, detailErr)}, nil
	}

	flattened := flattenStudyDetailSamples(studyDetails)
	studies := make([]mlwh.Study, 0, len(studyDetails))
	for _, detail := range studyDetails {
		studies = append(studies, detail.Study)
	}

	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       IdentifierLibraryType,
		Graph: EnrichmentGraph{
			Samples:      flattened,
			Studies:      studies,
			Libraries:    distinctLibrariesForSamples(flattened),
			StudyDetails: studyDetails,
		},
		Missing: missing,
		Partial: len(missing) > 0,
	}

	return result, true, nil, nil
}

func looksLikeLibraryType(identifier string) bool {
	return strings.ContainsAny(identifier, " \t")
}

func cachedStudiesByID(ctx context.Context, provider Provider, studyIDs []string) (map[string]mlwh.Study, bool, error) {
	if len(studyIDs) == 0 {
		return map[string]mlwh.Study{}, true, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(studyIDs)), ",")
	args := make([]any, 0, len(studyIDs))
	for _, studyID := range studyIDs {
		args = append(args, studyID)
	}

	rows, err := provider.QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT id_study_lims, name, accession_number FROM study_mirror WHERE id_lims = 'SQSCP' AND id_study_lims IN (%s)`,
			placeholders,
		),
		args...,
	)
	if err != nil {
		return nil, false, err
	}
	if rows == nil {
		return map[string]mlwh.Study{}, false, nil
	}
	defer func() { _ = rows.Close() }()

	studies := make(map[string]mlwh.Study, len(studyIDs))
	for rows.Next() {
		var (
			studyID string
			name sql.NullString
			accession sql.NullString
		)

		if err = rows.Scan(&studyID, &name, &accession); err != nil {
			return nil, true, err
		}

		studies[studyID] = mlwh.Study{
			IDStudyLims:     studyID,
			IDLims:          "SQSCP",
			Name:            name.String,
			AccessionNumber: accession.String,
		}
	}

	if err = rows.Err(); err != nil {
		return nil, true, err
	}

	return studies, true, nil
}

func libraryStudyDetails(ctx context.Context, provider Provider, libraryType string, samples []mlwh.Sample) ([]mlwh.StudyDetail, []MissingHop, error) {
	studySamplesByID := make(map[string][]mlwh.Sample)
	orderedStudyIDs := make([]string, 0)
	for _, sample := range samples {
		if sample.IDStudyLims == "" {
			continue
		}

		if _, seen := studySamplesByID[sample.IDStudyLims]; !seen {
			orderedStudyIDs = append(orderedStudyIDs, sample.IDStudyLims)
		}

		studySamplesByID[sample.IDStudyLims] = append(
			studySamplesByID[sample.IDStudyLims],
			sample,
		)
	}

	cachedStudies, cacheAvailable, err := cachedStudiesByID(ctx, provider, orderedStudyIDs)
	if err != nil {
		return nil, nil, err
	}

	studyDetails := make([]mlwh.StudyDetail, 0, len(orderedStudyIDs))
	missing := make([]MissingHop, 0, 1)
	missingStudyMetadata := false
	truncated := false

	for _, studyID := range orderedStudyIDs {
		study, ok := cachedStudies[studyID]
		if !ok {
			if cacheAvailable {
				study = mlwh.Study{IDStudyLims: studyID, IDLims: "SQSCP"}
				missingStudyMetadata = true
			} else {
			resolvedStudy, getErr := provider.GetStudy(ctx, studyID)
			if getErr != nil {
				if isClientError(getErr) {
					continue
				}

				return nil, nil, getErr
			}
			if resolvedStudy == nil {
				continue
			}

			study = *resolvedStudy
			}
		}

		studySamples := studySamplesByID[studyID]
		if len(studySamples) == 0 {
			continue
		}

		if len(studySamples) > MaxLibrarySamples {
			truncated = true
			studySamples = studySamples[:MaxLibrarySamples]
		}

		studyDetails = append(studyDetails, buildStudyDetail(study, studySamples))
	}

	if truncated {
		missing = append(missing, MissingHop{Hop: HopLibraries, Reason: ReasonSamplesTruncated, Status: http.StatusOK})
	}
	if missingStudyMetadata {
		missing = append(missing, MissingHop{Hop: HopStudies, Reason: ReasonNotFound, Status: http.StatusOK})
	}

	return studyDetails, missing, nil
}

func flattenStudyDetailSamples(studyDetails []mlwh.StudyDetail) []mlwh.Sample {
	samples := make([]mlwh.Sample, 0)
	for i := range studyDetails {
		samples = append(samples, flattenStudySamples(&studyDetails[i])...)
	}

	return samples
}

func flattenStudySamples(detail *mlwh.StudyDetail) []mlwh.Sample {
	if detail == nil {
		return nil
	}

	samples := make([]mlwh.Sample, 0)
	for _, library := range detail.Libraries {
		samples = append(samples, library.Samples...)
	}

	return samples
}

func distinctLibrariesForSamples(samples []mlwh.Sample) []Library {
	seen := make(map[Library]struct{}, len(samples))
	libraries := make([]Library, 0, len(samples))

	for _, sample := range samples {
		if sample.LibraryType == "" {
			continue
		}

		library := Library{LibraryType: sample.LibraryType, IDStudyLims: sample.IDStudyLims}
		if _, exists := seen[library]; exists {
			continue
		}

		seen[library] = struct{}{}
		libraries = append(libraries, library)
	}

	return libraries
}

func buildStudyDetails(studies []mlwh.Study, samples []mlwh.Sample) []mlwh.StudyDetail {
	studyMap := make(map[string][]mlwh.Sample)
	for _, sample := range samples {
		studyMap[sample.IDStudyLims] = append(studyMap[sample.IDStudyLims], sample)
	}

	studyDetails := make([]mlwh.StudyDetail, 0, len(studies))
	for _, study := range studies {
		studySamples := studyMap[study.IDStudyLims]
		if len(studySamples) == 0 {
			continue
		}

		studyDetails = append(studyDetails, buildStudyDetail(study, studySamples))
	}

	return studyDetails
}

func buildStudyDetail(study mlwh.Study, samples []mlwh.Sample) mlwh.StudyDetail {
	grouped := make(map[string][]mlwh.Sample)
	ordered := make([]string, 0)

	for _, sample := range samples {
		if _, seen := grouped[sample.LibraryType]; !seen {
			ordered = append(ordered, sample.LibraryType)
		}

		grouped[sample.LibraryType] = append(grouped[sample.LibraryType], sample)
	}

	libraries := make([]mlwh.LibraryDetail, 0, len(grouped))
	for _, libraryType := range ordered {
		librarySamples := grouped[libraryType]
		libraries = append(libraries, mlwh.LibraryDetail{
			Library: mlwh.Library{PipelineIDLims: libraryType, SampleCount: len(librarySamples)},
			Samples: librarySamples,
		})
	}

	return mlwh.StudyDetail{Study: study, Libraries: libraries}
}

func flatLibrariesFromStudyDetail(detail *mlwh.StudyDetail) []Library {
	if detail == nil {
		return nil
	}

	libraries := make([]Library, 0, len(detail.Libraries))
	for _, library := range detail.Libraries {
		if library.Library.PipelineIDLims == "" {
			continue
		}

		libraries = append(libraries, Library{
			LibraryType: library.Library.PipelineIDLims,
			IDStudyLims: detail.Study.IDStudyLims,
		})
	}

	return libraries
}

func classifySangerSampleID(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	return classifySampleIdentifier(ctx, provider, identifier, IdentifierSangerSampleID,
		func(ctx context.Context, provider Provider, identifier string) ([]mlwh.Sample, error) {
			return provider.FindSamplesBySangerID(ctx, identifier)
		})
}

func classifySampleLimsID(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	return classifySampleIdentifier(ctx, provider, identifier, IdentifierSampleLimsID,
		func(ctx context.Context, provider Provider, identifier string) ([]mlwh.Sample, error) {
			return provider.FindSamplesByIDSampleLims(ctx, identifier)
		})
}

func classifySampleAccession(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	return classifySampleIdentifier(ctx, provider, identifier, IdentifierSampleAccession,
		func(ctx context.Context, provider Provider, identifier string) ([]mlwh.Sample, error) {
			return provider.FindSamplesByAccessionNumber(ctx, identifier)
		})
}
