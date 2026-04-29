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
	"errors"
	"net/http"
	"strconv"

	"github.com/wtsi-hgi/wa/saga"
)

const (
	mlwhFilterSangerID        = "sanger_id"
	mlwhFilterIDSampleLims    = "id_sample_lims"
	mlwhFilterIDRun           = "id_run"
	mlwhFilterLibraryType     = "library_type"
	mlwhFilterAccessionNumber = "accession_number"
)

type enrichClassifier struct {
	classify func(context.Context, SAGAProvider, string) (*EnrichmentResult, bool, []MissingHop, error)
}

var enrichClassifiers = []enrichClassifier{
	{classify: classifyStudyID},
	{classify: classifyStudyAccession},
	{classify: classifySangerSampleID},
	{classify: classifySampleLimsID},
	{classify: classifySampleAccession},
	{classify: classifyRunID},
	{classify: classifyLibraryType},
	{classify: classifyProjectName},
}

type enrichFilterSupportProvider interface {
	EnrichFilterSupported(filterKey string) (bool, bool)
}

func isUnsupportedFilterError(provider SAGAProvider, filterKey string, err error) bool {
	if filterKey == "" || !errors.Is(err, saga.ErrServerError) {
		return false
	}

	filterSupport, ok := provider.(enrichFilterSupportProvider)
	if !ok {
		return false
	}

	supported, known := filterSupport.EnrichFilterSupported(filterKey)

	return known && !supported
}

type sampleClassifier func(context.Context, SAGAProvider, string) ([]saga.MLWHSample, error)

func classifySampleIdentifier(
	ctx context.Context,
	provider SAGAProvider,
	identifier string,
	identifierType IdentifierType,
	filterKey string,
	lookup sampleClassifier,
) (*EnrichmentResult, bool, []MissingHop, error) {
	samples, err := lookup(ctx, provider, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		if isUnsupportedFilterError(provider, filterKey, err) {
			return nil, false, unsupportedFilterMissing(), nil
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
	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       identifierType,
		Graph: EnrichmentGraph{
			Sample:  &sample,
			Samples: samples,
		},
	}

	if err := enrichSampleStudy(ctx, provider, sample, result); err != nil {
		return nil, false, nil, err
	}

	return result, true, nil, nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func unsupportedFilterMissing() []MissingHop {
	return []MissingHop{{
		Hop:    HopClassify,
		Reason: ReasonFilterUnsupported,
		Status: http.StatusBadGateway,
	}}
}

func missingHop(hop string, err error) MissingHop {
	status := http.StatusBadGateway
	reason := ReasonUpstreamError

	if isClientError(err) {
		status = http.StatusNotFound
		reason = ReasonNotFound

		var apiErr saga.APIError
		if errors.As(err, &apiErr) {
			status = apiErr.StatusCode
		}

		apiErrPtr := &saga.APIError{}
		if errors.As(err, &apiErrPtr) {
			status = apiErrPtr.StatusCode
		}
	}

	return MissingHop{Hop: hop, Reason: reason, Status: status}
}

func enrichSampleStudy(ctx context.Context, provider SAGAProvider, sample saga.MLWHSample, result *EnrichmentResult) error {
	if result == nil {
		return nil
	}

	result.Graph.Library = &Library{LibraryType: sample.LibraryType, IDStudyLims: sample.IDStudyLims}

	study, err := provider.StudyForSample(ctx, sample)
	if err != nil {
		if isContextError(err) {
			return err
		}

		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopStudy, err))

		return nil
	}

	result.Graph.Study = study

	return nil
}

// Enrich resolves an identifier into its enrichment graph.
func Enrich(ctx context.Context, provider SAGAProvider, identifier string) (*EnrichmentResult, error) {
	if identifier == "" {
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

func classifyStudyID(ctx context.Context, provider SAGAProvider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
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

	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       IdentifierStudyID,
		Graph: EnrichmentGraph{
			Study: study,
		},
	}

	if err := enrichStudy(ctx, provider, study, result); err != nil {
		return nil, false, nil, err
	}

	return result, true, nil, nil
}

func classifyStudyAccession(ctx context.Context, provider SAGAProvider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	studies, err := provider.AllStudies(ctx)
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
		result := &EnrichmentResult{
			Identifier: identifier,
			Type:       IdentifierStudyAccession,
			Graph: EnrichmentGraph{
				Study: &study,
			},
		}

		if err := enrichStudy(ctx, provider, &study, result); err != nil {
			return nil, false, nil, err
		}

		return result, true, nil, nil
	}

	return nil, false, nil, nil
}

func enrichStudy(ctx context.Context, provider SAGAProvider, study *saga.Study, result *EnrichmentResult) error {
	if study == nil || result == nil {
		return nil
	}

	result.Graph.Studies = []saga.Study{*study}

	samples, err := provider.AllSamplesForStudy(ctx, study.IDStudyLims)
	if err != nil {
		if isContextError(err) {
			return err
		}

		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopSamples, err))

		return nil
	}

	result.Graph.Samples = samples
	result.Graph.Libraries = distinctLibraries(study.IDStudyLims, samples)
	if len(result.Missing) > 0 {
		result.Partial = true
	}

	return nil
}

func distinctLibraries(studyID string, samples []saga.MLWHSample) []Library {
	seen := make(map[Library]struct{}, len(samples))
	libraries := make([]Library, 0, len(samples))

	for _, sample := range samples {
		library := Library{LibraryType: sample.LibraryType, IDStudyLims: studyID}
		if _, exists := seen[library]; exists {
			continue
		}

		seen[library] = struct{}{}
		libraries = append(libraries, library)
	}

	return libraries
}

func classifyRunID(ctx context.Context, provider SAGAProvider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	runID, err := strconv.Atoi(identifier)
	if err != nil {
		return nil, false, nil, nil
	}

	samples, err := provider.FindSamplesByRunID(ctx, runID)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		if isUnsupportedFilterError(provider, mlwhFilterIDRun, err) {
			return nil, false, unsupportedFilterMissing(), nil
		}

		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	if len(samples) == 0 {
		return nil, false, nil, nil
	}

	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       IdentifierRunID,
		Graph: EnrichmentGraph{
			Samples: samples,
		},
	}

	if err := enrichStudiesForSamples(ctx, provider, samples, result); err != nil {
		return nil, false, nil, err
	}

	return result, true, nil, nil
}

func classifyLibraryType(ctx context.Context, provider SAGAProvider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	samples, err := provider.FindSamplesByLibraryType(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		if isUnsupportedFilterError(provider, mlwhFilterLibraryType, err) {
			return nil, false, unsupportedFilterMissing(), nil
		}

		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	if len(samples) == 0 {
		return nil, false, nil, nil
	}

	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       IdentifierLibraryType,
		Graph: EnrichmentGraph{
			Samples: samples,
		},
	}

	if len(samples) > MaxLibrarySamples {
		result.Graph.Samples = samples[:MaxLibrarySamples]
		result.Partial = true
		result.Missing = append(result.Missing, MissingHop{
			Hop:    HopSamples,
			Reason: ReasonSamplesTruncated,
			Status: http.StatusOK,
		})
	}

	if err := enrichStudiesForSamples(ctx, provider, samples, result); err != nil {
		return nil, false, nil, err
	}

	return result, true, nil, nil
}

func enrichStudiesForSamples(ctx context.Context, provider SAGAProvider, samples []saga.MLWHSample, result *EnrichmentResult) error {
	if result == nil {
		return nil
	}

	result.Graph.Libraries = distinctLibrariesForSamples(samples)

	studyIDs := make([]string, 0, len(samples))
	seen := make(map[string]struct{}, len(samples))

	for _, sample := range samples {
		if _, exists := seen[sample.IDStudyLims]; exists {
			continue
		}

		seen[sample.IDStudyLims] = struct{}{}
		studyIDs = append(studyIDs, sample.IDStudyLims)
	}

	studies := make([]saga.Study, 0, len(studyIDs))

	for _, studyID := range studyIDs {
		study, err := provider.GetStudy(ctx, studyID)
		if err != nil {
			if isContextError(err) {
				return err
			}

			result.Partial = true
			result.Missing = append(result.Missing, missingHop(HopStudies, err))

			continue
		}

		if study == nil {
			continue
		}

		studies = append(studies, *study)
	}

	result.Graph.Studies = studies

	return nil
}

func classifyProjectName(ctx context.Context, provider SAGAProvider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	projects, err := provider.ListProjects(ctx)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	for _, candidate := range projects {
		if candidate.Name != identifier {
			continue
		}

		project := candidate
		result := &EnrichmentResult{
			Identifier: identifier,
			Type:       IdentifierProjectName,
			Graph: EnrichmentGraph{
				Project: &project,
			},
		}

		if err := enrichProject(ctx, provider, &project, result); err != nil {
			return nil, false, nil, err
		}

		return result, true, nil, nil
	}

	return nil, false, nil, nil
}

func enrichProject(ctx context.Context, provider SAGAProvider, project *saga.Project, result *EnrichmentResult) error {
	if project == nil || result == nil {
		return nil
	}

	projectStudies, err := provider.ListProjectStudies(ctx, project.ID)
	if err != nil {
		if isContextError(err) {
			return err
		}

		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopStudies, err))
	} else {
		studies := make([]saga.Study, 0, len(projectStudies))

		for _, projectStudy := range projectStudies {
			study, studyErr := provider.GetStudy(ctx, projectStudy.IDStudyLims)
			if studyErr != nil {
				if isContextError(studyErr) {
					return studyErr
				}

				result.Partial = true
				result.Missing = append(result.Missing, missingHop(HopStudies, studyErr))

				continue
			}

			if study == nil {
				continue
			}

			studies = append(studies, *study)
		}

		result.Graph.Studies = studies
	}

	projectSamples, err := provider.ListProjectSamples(ctx, project.ID)
	if err != nil {
		if isContextError(err) {
			return err
		}

		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopSamples, err))
	} else {
		samples := make([]saga.MLWHSample, 0, len(projectSamples))

		for _, projectSample := range projectSamples {
			resolvedSamples, sampleErr := provider.FindSamplesBySangerID(ctx, projectSample.SangerID)
			if sampleErr != nil {
				if isContextError(sampleErr) {
					return sampleErr
				}

				result.Partial = true
				result.Missing = append(result.Missing, missingHop(HopSamples, sampleErr))

				continue
			}

			samples = append(samples, resolvedSamples...)
		}

		result.Graph.Samples = samples
		result.Graph.Libraries = distinctLibrariesForSamples(samples)
	}

	users, err := provider.ListProjectUsers(ctx, project.ID)
	if err != nil {
		if isContextError(err) {
			return err
		}

		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopUsers, err))

		return nil
	}

	result.Graph.Users = users

	return nil
}

func distinctLibrariesForSamples(samples []saga.MLWHSample) []Library {
	seen := make(map[Library]struct{}, len(samples))
	libraries := make([]Library, 0, len(samples))

	for _, sample := range samples {
		library := Library{LibraryType: sample.LibraryType, IDStudyLims: sample.IDStudyLims}
		if _, exists := seen[library]; exists {
			continue
		}

		seen[library] = struct{}{}
		libraries = append(libraries, library)
	}

	return libraries
}

func classifySangerSampleID(ctx context.Context, provider SAGAProvider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	return classifySampleIdentifier(ctx, provider, identifier, IdentifierSangerSampleID,
		mlwhFilterSangerID,
		func(ctx context.Context, provider SAGAProvider, identifier string) ([]saga.MLWHSample, error) {
			return provider.FindSamplesBySangerID(ctx, identifier)
		})
}

func classifySampleLimsID(ctx context.Context, provider SAGAProvider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	return classifySampleIdentifier(ctx, provider, identifier, IdentifierSampleLimsID,
		mlwhFilterIDSampleLims,
		func(ctx context.Context, provider SAGAProvider, identifier string) ([]saga.MLWHSample, error) {
			return provider.FindSamplesByIDSampleLims(ctx, identifier)
		})
}

func classifySampleAccession(ctx context.Context, provider SAGAProvider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	return classifySampleIdentifier(ctx, provider, identifier, IdentifierSampleAccession,
		mlwhFilterAccessionNumber,
		func(ctx context.Context, provider SAGAProvider, identifier string) ([]saga.MLWHSample, error) {
			return provider.FindSamplesByAccessionNumber(ctx, identifier)
		})
}
