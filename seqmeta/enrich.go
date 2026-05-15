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
	{classify: classifySangerSampleName},
	{classify: classifySangerSampleID},
	{classify: classifySampleLimsID},
	{classify: classifySampleAccession},
	{classify: classifyResolvedSample},
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

func buildStudyDetail(study mlwh.Study, samples []mlwh.Sample) mlwh.StudyDetail {
	grouped := make(map[string]mlwh.LibraryDetail)
	ordered := make([]string, 0)

	for _, sample := range samples {
		for _, library := range sample.Libraries {
			if library.PipelineIDLims == "" {
				continue
			}
			if library.IDStudyLims != "" && library.IDStudyLims != study.IDStudyLims {
				continue
			}

			key := studyLibraryGroupKey(library, study.IDStudyLims)
			if _, seen := grouped[key]; !seen {
				ordered = append(ordered, key)
				if library.IDStudyLims == "" {
					library.IDStudyLims = study.IDStudyLims
				}
				grouped[key] = mlwh.LibraryDetail{Library: library}
			}

			detail := grouped[key]
			detail.Samples = append(detail.Samples, sample)
			grouped[key] = detail
		}
	}

	libraries := make([]mlwh.LibraryDetail, 0, len(grouped))
	for _, key := range ordered {
		libraries = append(libraries, grouped[key])
	}

	return mlwh.StudyDetail{Study: study, Libraries: libraries}
}

func studyLibraryGroupKey(library mlwh.Library, studyID string) string {
	libraryStudyID := library.IDStudyLims
	if libraryStudyID == "" {
		libraryStudyID = studyID
	}

	return strings.Join(
		[]string{
			library.PipelineIDLims,
			libraryStudyID,
			library.LibraryID,
			library.IDLibraryLims,
		},
		"\x00",
	)
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
	case sample.Name != "":
		return sample.Name
	case sample.SangerSampleID != "":
		return sample.SangerSampleID
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

	libraries := append([]mlwh.Library(nil), sample.Libraries...)

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
		ids := sampleStudyIDs(sample)
		if len(ids) == 0 && sample.Name != "" {
			studies, err := provider.StudiesForSample(ctx, sample.Name)
			if err != nil {
				return nil, err
			}
			for _, study := range studies {
				ids = append(ids, study.IDStudyLims)
			}
		}

		for _, studyID := range ids {
			if _, exists := seen[studyID]; exists {
				continue
			}

			seen[studyID] = struct{}{}
			studyIDs = append(studyIDs, studyID)
		}
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

func sampleStudyIDs(sample mlwh.Sample) []string {
	ids := make([]string, 0, len(sample.Studies)+len(sample.Libraries))
	seen := make(map[string]struct{}, len(sample.Studies)+len(sample.Libraries))
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	for _, study := range sample.Studies {
		add(study.IDStudyLims)
	}
	for _, library := range sample.Libraries {
		add(library.IDStudyLims)
	}

	return ids
}

func buildStudyDetails(studies []mlwh.Study, samples []mlwh.Sample) []mlwh.StudyDetail {
	studyMap := make(map[string][]mlwh.Sample)
	for _, sample := range samples {
		for _, studyID := range sampleStudyIDs(sample) {
			studyMap[studyID] = append(studyMap[studyID], sample)
		}
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

	return buildSampleEnrichment(ctx, provider, identifier, identifierType, samples)
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

func buildSampleEnrichment(
	ctx context.Context,
	provider Provider,
	identifier string,
	identifierType IdentifierType,
	samples []mlwh.Sample,
) (*EnrichmentResult, bool, []MissingHop, error) {
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

func libraryLinkForSample(sample mlwh.Sample) *Library {
	library, ok := samplePrimaryLibrary(sample)
	if !ok {
		return nil
	}

	return &Library{LibraryType: library.PipelineIDLims, IDStudyLims: library.IDStudyLims}
}

func samplePrimaryLibrary(sample mlwh.Sample) (mlwh.Library, bool) {
	for _, library := range sample.Libraries {
		if library.PipelineIDLims == "" {
			continue
		}

		return library, true
	}

	return mlwh.Library{}, false
}

func enrichSampleStudy(ctx context.Context, provider Provider, sample mlwh.Sample, result *EnrichmentResult) error {
	if result == nil {
		return nil
	}

	studies, err := provider.StudiesForSample(ctx, sample.Name)
	if err != nil {
		if isContextError(err) {
			return err
		}

		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopStudy, err))

		return nil
	}
	if len(studies) == 0 {
		return nil
	}

	if result.Graph.Sample != nil {
		result.Graph.Sample.Studies = append([]mlwh.Study(nil), studies...)
	}
	if result.Graph.SampleDetail != nil {
		result.Graph.SampleDetail.Sample.Studies = append([]mlwh.Study(nil), studies...)
	}
	result.Graph.Studies = append([]mlwh.Study(nil), studies...)

	if len(studies) != 1 {
		return nil
	}

	study := studies[0]
	result.Graph.Study = &study
	if result.Graph.SampleDetail != nil {
		result.Graph.SampleDetail.Study = &study
	}

	return nil
}

type sampleNameResolver interface {
	ResolveSampleName(context.Context, string) (mlwh.Match, error)
}

func classifySangerSampleName(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	resolver, ok := provider.(sampleNameResolver)
	if !ok {
		return nil, false, nil, nil
	}

	match, err := resolver.ResolveSampleName(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}
		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}
	if match.Sample == nil {
		return nil, false, nil, nil
	}
	if match.Kind != mlwh.KindSangerSampleName {
		return nil, false, nil, nil
	}

	return buildSampleEnrichment(ctx, provider, identifier, IdentifierSangerSampleName, []mlwh.Sample{*match.Sample})
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

func looksLikeLibraryType(identifier string) bool {
	return strings.ContainsAny(identifier, " \t")
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

func classifyLibraryType(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	samples, err := samplesForLibraryType(ctx, provider, identifier)
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

	// If total samples across multiple studies exceed the reasonable limit for frontend,
	// truncate to prevent browser-freezing payloads. Single-study results are allowed
	// to use the per-study limit (MaxLibrarySamples).
	fetchTruncated := false
	if len(studyDetails) > 1 && len(flattened) > MaxLibraryTypeSamples {
		fetchTruncated = true
		flattened = flattened[:MaxLibraryTypeSamples]

		// Rebuild study details with truncated samples
		studyDetails = rebuildStudyDetailsFromSamples(studyDetails, flattened)
	}

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
		Partial: len(missing) > 0 || fetchTruncated,
	}

	if fetchTruncated {
		result.Missing = append(result.Missing, MissingHop{
			Hop:    HopClassify,
			Reason: ReasonSamplesTruncated,
			Status: http.StatusOK,
		})
	}

	return result, true, nil, nil
}

func samplesForLibraryType(ctx context.Context, provider Provider, identifier string) ([]mlwh.Sample, error) {
	samples, err := provider.SamplesForLibraryType(ctx, identifier, MaxLibrarySamples+1, 0)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		return samples, nil
	}

	return provider.FindSamplesByLibraryType(ctx, identifier)
}

func libraryStudyDetails(ctx context.Context, provider Provider, libraryType string, samples []mlwh.Sample) ([]mlwh.StudyDetail, []MissingHop, error) {
	studySamplesByID := make(map[string][]mlwh.Sample)
	orderedStudyIDs := make([]string, 0)
	for _, sample := range samples {
		for _, studyID := range sampleStudyIDs(sample) {
			if _, seen := studySamplesByID[studyID]; !seen {
				orderedStudyIDs = append(orderedStudyIDs, studyID)
			}

			studySamplesByID[studyID] = append(studySamplesByID[studyID], sample)
		}
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
			studyID   string
			name      sql.NullString
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

func rebuildStudyDetailsFromSamples(original []mlwh.StudyDetail, samples []mlwh.Sample) []mlwh.StudyDetail {
	sampleSet := make(map[string]struct{}, len(samples))
	for _, sample := range samples {
		sampleSet[sample.Name] = struct{}{}
	}

	result := make([]mlwh.StudyDetail, 0, len(original))
	for _, detail := range original {
		filteredLibraries := make([]mlwh.LibraryDetail, 0, len(detail.Libraries))
		hasAnySamples := false

		for _, lib := range detail.Libraries {
			filteredSamples := make([]mlwh.Sample, 0, len(lib.Samples))
			for _, sample := range lib.Samples {
				if _, keep := sampleSet[sample.Name]; keep {
					filteredSamples = append(filteredSamples, sample)
				}
			}

			if len(filteredSamples) > 0 {
				hasAnySamples = true
				filteredLibraries = append(filteredLibraries, mlwh.LibraryDetail{
					Library: lib.Library,
					Samples: filteredSamples,
				})
			}
		}

		if hasAnySamples {
			result = append(result, mlwh.StudyDetail{
				Study:     detail.Study,
				Libraries: filteredLibraries,
			})
		}
	}

	return result
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
			return classifyRunIDWithoutSamples(ctx, provider, identifier, err)
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	if len(samples) == 0 {
		return classifyRunIDWithoutSamples(ctx, provider, identifier, mlwh.ErrNotFound)
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

func classifyRunIDWithoutSamples(ctx context.Context, provider Provider, identifier string, samplesErr error) (*EnrichmentResult, bool, []MissingHop, error) {
	match, err := provider.ResolveRun(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}
		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}
	if match.Run == nil || match.Kind != mlwh.KindRunID {
		return nil, false, nil, nil
	}

	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       IdentifierRunID,
	}
	if samplesErr != nil {
		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopSamples, samplesErr))
	}

	return result, true, nil, nil
}

func distinctLibrariesForSamples(samples []mlwh.Sample) []Library {
	seen := make(map[Library]struct{}, len(samples))
	libraries := make([]Library, 0, len(samples))

	for _, sample := range samples {
		for _, sampleLibrary := range sample.Libraries {
			if sampleLibrary.PipelineIDLims == "" {
				continue
			}

			library := Library{LibraryType: sampleLibrary.PipelineIDLims, IDStudyLims: sampleLibrary.IDStudyLims}
			if _, exists := seen[library]; exists {
				continue
			}

			seen[library] = struct{}{}
			libraries = append(libraries, library)
		}
	}

	return libraries
}

func classifyResolvedSample(ctx context.Context, provider Provider, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	match, err := provider.ResolveSample(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}
		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}
	if match.Sample == nil {
		return nil, false, nil, nil
	}

	identifierType, ok := sampleIdentifierType(match.Kind)
	if !ok {
		return nil, false, nil, nil
	}

	return buildSampleEnrichment(ctx, provider, identifier, identifierType, []mlwh.Sample{*match.Sample})
}

func sampleIdentifierType(kind mlwh.IdentifierKind) (IdentifierType, bool) {
	switch kind {
	case mlwh.KindSampleUUID,
		mlwh.KindSampleLimsID,
		mlwh.KindSangerSampleName,
		mlwh.KindSangerSampleID,
		mlwh.KindSupplierName,
		mlwh.KindSampleAccession,
		mlwh.KindDonorID:
		return IdentifierType(kind), true
	default:
		return "", false
	}
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
