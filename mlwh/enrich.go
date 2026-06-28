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

package mlwh

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const detailFetchLimit = 1_000_000

// allDetailRows is the default detail-collection limit: it returns every nested
// row, matching the exported StudyDetail/RunDetail behaviour.
const allDetailRows = detailFetchLimit

type enrichClassifier struct {
	classify func(context.Context, *Client, string) (*EnrichmentResult, bool, []MissingHop, error)
}

var enrichClassifiers = []enrichClassifier{
	{classify: classifyStudyID},
	{classify: classifyStudyAccession},
	{classify: classifyRunID},
	{classify: classifySangerSampleName},
	{classify: classifyResolvedSample},
	{classify: classifyLibraryType},
	{classify: classifySangerSampleID},
	{classify: classifySampleLimsID},
	{classify: classifySampleAccession},
}

type sampleClassifier func(context.Context, *Client, string) ([]Sample, error)

func classifySampleIdentifier(
	ctx context.Context,
	client *Client,
	identifier string,
	identifierType IdentifierKind,
	lookup sampleClassifier,
) (*EnrichmentResult, bool, []MissingHop, error) {
	samples, err := lookup(ctx, client, identifier)
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

	return client.buildSampleEnrichment(ctx, identifier, identifierType, samples)
}

func isContextError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func isClientError(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnsupportedIdentifier)
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

// detailOptions carries the optional pagination and lean controls a detail
// endpoint accepts. limit/offset page the nested collection (defaulting to
// every nested row when limit is the server fetch-all default), and lean drops
// the heavy nested objects in favour of the flat id lists. They are an HTTP-layer
// concern, so the exported StudyDetail/RunDetail methods (used by enrichment and
// the remote client) always return the full, all-rows, non-lean shape.
type detailOptions struct {
	limit  int
	offset int
	lean   bool
}

func defaultDetailOptions() detailOptions {
	return detailOptions{limit: allDetailRows, offset: 0}
}

// studyDetailWithOptions builds the study detail, paginates its nested
// library/sample collection by the given limit/offset, de-duplicates the
// referenced studies/libraries into the lookup tables, and (when lean) collapses
// it to the flat id lists. The returned total is the FULL nested sample count
// before pagination, which the handler reports via X-Total-Count.
func (c *Client) studyDetailWithOptions(ctx context.Context, studyLimsID string, opts detailOptions) (StudyDetail, int, error) {
	study, err := c.studyDetailStudy(ctx, studyLimsID)
	if err != nil {
		return StudyDetail{}, 0, err
	}

	libraries, err := c.LibrariesForStudy(ctx, studyLimsID, detailFetchLimit, 0)
	if err != nil {
		return StudyDetail{}, 0, err
	}

	samples, err := c.SamplesForStudy(ctx, studyLimsID, detailFetchLimit, 0)
	if err != nil {
		return StudyDetail{}, 0, err
	}

	total := len(samples)
	if opts.lean {
		return leanStudyDetail(study, samples, libraries), total, nil
	}

	pagedSamples := paginateSamples(samples, opts)
	detail := buildStudyDetail(study, pagedSamples)
	addMissingStudyLibraries(&detail, libraries)
	deduplicateStudyDetail(&detail)

	return detail, total, nil
}

// leanStudyDetail builds the lean study-detail shape: the top-level study plus
// the flat lists of its distinct sample and library ids, with the heavy
// library_details and lookup tables dropped, so the serialized response is
// strictly smaller than the non-lean one.
func leanStudyDetail(study Study, samples []Sample, libraries []Library) StudyDetail {
	return StudyDetail{
		Study:      study,
		SampleIDs:  distinctSampleIDs(samples),
		LibraryIDs: distinctLibraryIDs(samples, libraries),
		Lean:       true,
	}
}

// distinctSampleIDs returns the distinct sample ids of the given samples, in
// first-seen order, using the same key as the sample lookup table.
func distinctSampleIDs(samples []Sample) []string {
	ids := make([]string, 0, len(samples))
	seen := make(map[string]struct{}, len(samples))
	for _, sample := range samples {
		key := sampleLookupKey(sample)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}
		ids = append(ids, key)
	}

	return ids
}

// distinctLibraryIDs returns the distinct library ids referenced by the given
// samples and the study's standalone libraries, in first-seen order, using the
// same key as the library lookup table.
func distinctLibraryIDs(samples []Sample, libraries []Library) []string {
	ids := make([]string, 0, len(libraries))
	seen := make(map[string]struct{}, len(libraries))
	add := func(library Library) {
		if library.PipelineIDLims == "" {
			return
		}

		key := libraryLookupKey(library)
		if _, ok := seen[key]; ok {
			return
		}

		seen[key] = struct{}{}
		ids = append(ids, key)
	}

	for _, sample := range samples {
		for _, library := range sample.Libraries {
			add(library)
		}
	}
	for _, library := range libraries {
		add(library)
	}

	return ids
}

// libraryLookupKey is the stable per-library id used to key a library in a detail
// lookup table and in the lean flat id list. It is unique across the library's
// type, study, and identifiers, so two libraries that differ only by study do not
// collide.
func libraryLookupKey(library Library) string {
	return studyLibraryGroupKey(library, library.IDStudyLims)
}

// deduplicateStudyDetail moves the distinct studies and libraries referenced by
// the study detail's nested sample rows into the lookup tables and strips those
// sub-objects from the nested rows, so each entity is carried once (keyed by id)
// instead of being re-embedded under every sample.
func deduplicateStudyDetail(detail *StudyDetail) {
	lookups := newDetailLookups()
	lookups.addStudy(detail.Study)

	for libIndex := range detail.Libraries {
		library := &detail.Libraries[libIndex]
		lookups.addLibrary(library.Library)
		for sampleIndex := range library.Samples {
			lookups.addSampleReferences(library.Samples[sampleIndex])
			library.Samples[sampleIndex] = strippedSample(library.Samples[sampleIndex])
		}
	}

	detail.StudyLookup = lookups.studyMapOrNil()
	detail.LibraryLookup = lookups.libraryMapOrNil()
}

func newDetailLookups() *detailLookups {
	return &detailLookups{
		studies:   make(map[string]Study),
		libraries: make(map[string]Library),
	}
}

// strippedSample returns a copy of sample with its studies/libraries sub-objects
// cleared, so the de-duplicated nested rows reference those entities by id (via
// the lookup tables) instead of re-embedding them.
func strippedSample(sample Sample) Sample {
	sample.Studies = nil
	sample.Libraries = nil

	return sample
}

// runDetailWithOptions builds the run detail, paginates its nested sample
// collection by the given limit/offset (rebuilding the per-study detail from the
// page so it stays consistent), de-duplicates the referenced studies/libraries
// into the lookup tables, and (when lean) collapses it to the flat id lists. The
// returned total is the FULL nested sample count before pagination, which the
// handler reports via X-Total-Count.
func (c *Client) runDetailWithOptions(ctx context.Context, idRun string, opts detailOptions) (RunDetail, int, error) {
	runID, _ := strconv.Atoi(idRun)
	samples, err := c.SamplesForRun(ctx, idRun, detailFetchLimit, 0)
	if err != nil {
		return RunDetail{Run: Run{IDRun: runID}}, 0, err
	}

	studies, samples, err := c.studiesForSamples(ctx, samples)
	if err != nil {
		return RunDetail{Run: Run{IDRun: runID}, Samples: samples}, len(samples), err
	}

	total := len(samples)
	if opts.lean {
		return leanRunDetail(runID, samples, studies), total, nil
	}

	pagedSamples := paginateSamples(samples, opts)
	pagedStudies := studiesForRunSamples(studies, pagedSamples)
	detail := RunDetail{
		Run:          Run{IDRun: runID},
		Samples:      pagedSamples,
		Studies:      pagedStudies,
		StudyDetails: buildStudyDetails(pagedStudies, pagedSamples),
	}
	deduplicateRunDetail(&detail)

	return detail, total, nil
}

// leanRunDetail builds the lean run-detail shape: the run plus the flat lists of
// its distinct sample and study ids, with the heavy samples/studies/study_details
// and lookup tables dropped, so the serialized response is strictly smaller than
// the non-lean one.
func leanRunDetail(runID int, samples []Sample, studies []Study) RunDetail {
	studyIDs := make([]string, 0, len(studies))
	seen := make(map[string]struct{}, len(studies))
	for _, study := range studies {
		if study.IDStudyLims == "" {
			continue
		}
		if _, ok := seen[study.IDStudyLims]; ok {
			continue
		}

		seen[study.IDStudyLims] = struct{}{}
		studyIDs = append(studyIDs, study.IDStudyLims)
	}

	return RunDetail{
		Run:       Run{IDRun: runID},
		SampleIDs: distinctSampleIDs(samples),
		StudyIDs:  studyIDs,
		Lean:      true,
	}
}

// studiesForRunSamples narrows the run's studies to those actually referenced by
// the given page of samples, so a paginated run detail's studies/study_details
// stay consistent with the samples it carries.
func studiesForRunSamples(studies []Study, samples []Sample) []Study {
	referenced := make(map[string]struct{}, len(samples))
	for _, sample := range samples {
		for _, studyID := range sampleStudyIDs(sample) {
			referenced[studyID] = struct{}{}
		}
	}

	filtered := make([]Study, 0, len(studies))
	for _, study := range studies {
		if _, ok := referenced[study.IDStudyLims]; ok {
			filtered = append(filtered, study)
		}
	}

	return filtered
}

// deduplicateRunDetail moves the distinct studies and libraries referenced by the
// run detail's nested rows into the lookup tables and strips those sub-objects
// from every nested sample (both the top-level samples and the study_details), so
// each entity is carried once (keyed by id) instead of being re-embedded.
func deduplicateRunDetail(detail *RunDetail) {
	lookups := newDetailLookups()

	for index := range detail.Samples {
		lookups.addSampleReferences(detail.Samples[index])
		detail.Samples[index] = strippedSample(detail.Samples[index])
	}
	for _, study := range detail.Studies {
		lookups.addStudy(study)
	}
	for detailIndex := range detail.StudyDetails {
		studyDetail := &detail.StudyDetails[detailIndex]
		lookups.addStudy(studyDetail.Study)
		for libIndex := range studyDetail.Libraries {
			library := &studyDetail.Libraries[libIndex]
			lookups.addLibrary(library.Library)
			for sampleIndex := range library.Samples {
				library.Samples[sampleIndex] = strippedSample(library.Samples[sampleIndex])
			}
		}
	}

	detail.StudyLookup = lookups.studyMapOrNil()
	detail.LibraryLookup = lookups.libraryMapOrNil()
}

// paginateSamples applies a detail collection's limit/offset to its samples,
// returning the requested window. An offset past the end yields an empty page;
// the default fetch-all limit returns every sample.
func paginateSamples(samples []Sample, opts detailOptions) []Sample {
	if opts.offset >= len(samples) {
		return []Sample{}
	}

	end := len(samples)
	if opts.limit >= 0 && opts.offset+opts.limit < end {
		end = opts.offset + opts.limit
	}

	return samples[opts.offset:end]
}

// detailLookups accumulates the distinct studies and libraries referenced by the
// nested rows of a detail response, so each is carried once (keyed by id) instead
// of being re-embedded under every sample.
type detailLookups struct {
	studies   map[string]Study
	libraries map[string]Library
}

func (l *detailLookups) addSampleReferences(sample Sample) {
	for _, study := range sample.Studies {
		if study.IDStudyLims == "" {
			continue
		}
		if _, ok := l.studies[study.IDStudyLims]; !ok {
			l.studies[study.IDStudyLims] = study
		}
	}
	for _, library := range sample.Libraries {
		if library.PipelineIDLims == "" {
			continue
		}

		key := libraryLookupKey(library)
		if _, ok := l.libraries[key]; !ok {
			l.libraries[key] = library
		}
	}
}

func (l *detailLookups) addStudy(study Study) {
	if study.IDStudyLims == "" {
		return
	}
	if _, ok := l.studies[study.IDStudyLims]; !ok {
		l.studies[study.IDStudyLims] = study
	}
}

func (l *detailLookups) addLibrary(library Library) {
	if library.PipelineIDLims == "" {
		return
	}

	key := libraryLookupKey(library)
	if _, ok := l.libraries[key]; !ok {
		l.libraries[key] = library
	}
}

func (l *detailLookups) studyMapOrNil() map[string]Study {
	if len(l.studies) == 0 {
		return nil
	}

	return l.studies
}

func (l *detailLookups) libraryMapOrNil() map[string]Library {
	if len(l.libraries) == 0 {
		return nil
	}

	return l.libraries
}

func finishEnrichmentResult(result *EnrichmentResult, missing []MissingHop) EnrichmentResult {
	if result == nil {
		return EnrichmentResult{}
	}
	if len(missing) > 0 {
		result.Missing = append(append([]MissingHop(nil), missing...), result.Missing...)
		result.Partial = true
	}

	return *result
}

func classifyStudyID(ctx context.Context, client *Client, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	if _, err := strconv.Atoi(identifier); err != nil {
		return nil, false, nil, nil
	}

	match, err := client.ResolveStudy(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}
		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}
	if match.Study == nil || match.Kind != KindStudyLimsID {
		return nil, false, nil, nil
	}

	result := &EnrichmentResult{Identifier: identifier, Type: KindStudyLimsID}
	if err = client.enrichStudy(ctx, match.Study, result); err != nil {
		return nil, false, nil, err
	}

	return result, true, nil, nil
}

func classifyStudyAccession(ctx context.Context, client *Client, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	if looksLikeLibraryType(identifier) {
		return nil, false, nil, nil
	}

	match, err := client.ResolveStudy(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}
		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}
	if match.Study == nil || match.Kind != KindStudyAccession {
		return nil, false, nil, nil
	}

	result := &EnrichmentResult{Identifier: identifier, Type: KindStudyAccession}
	if err = client.enrichStudy(ctx, match.Study, result); err != nil {
		return nil, false, nil, err
	}

	return result, true, nil, nil
}

func looksLikeLibraryType(identifier string) bool {
	return strings.ContainsAny(identifier, " \t")
}

func classifyRunID(ctx context.Context, client *Client, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	if _, err := strconv.Atoi(identifier); err != nil {
		return nil, false, nil, nil
	}

	runDetail, err := client.RunDetail(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}
		if isClientError(err) {
			return classifyRunIDWithoutSamples(ctx, client, identifier, err)
		}
		if len(runDetail.Samples) == 0 {
			return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
		}

		result := runEnrichmentResult(identifier, runDetail)
		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopStudies, err))

		return result, true, nil, nil
	}
	if len(runDetail.Samples) == 0 {
		return classifyRunIDWithoutSamples(ctx, client, identifier, ErrNotFound)
	}

	return runEnrichmentResult(identifier, runDetail), true, nil, nil
}

func classifyRunIDWithoutSamples(
	ctx context.Context,
	client *Client,
	identifier string,
	samplesErr error,
) (*EnrichmentResult, bool, []MissingHop, error) {
	match, err := client.ResolveRun(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}
		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}
	if match.Run == nil || match.Kind != KindRunID {
		return nil, false, nil, nil
	}

	result := &EnrichmentResult{Identifier: identifier, Type: KindRunID}
	if samplesErr != nil {
		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopSamples, samplesErr))
	}

	return result, true, nil, nil
}

func runEnrichmentResult(identifier string, runDetail RunDetail) *EnrichmentResult {
	return &EnrichmentResult{
		Identifier: identifier,
		Type:       KindRunID,
		Graph: EnrichmentGraph{
			Samples:      runDetail.Samples,
			Studies:      runDetail.Studies,
			Libraries:    libraryLinksFromStudyDetails(runDetail.StudyDetails),
			StudyDetails: runDetail.StudyDetails,
		},
	}
}

// libraryLinksFromStudyDetails collects the distinct library links from a run's
// per-study detail, in build order. The detail is de-duplicated so each library
// is carried once under its study (rather than re-embedded under every sample),
// so the enrichment graph's library links are derived from it deterministically.
func libraryLinksFromStudyDetails(studyDetails []StudyDetail) []LibraryLink {
	seen := make(map[LibraryLink]struct{})
	links := make([]LibraryLink, 0)
	for index := range studyDetails {
		for _, link := range flatLibrariesFromStudyDetail(&studyDetails[index]) {
			if _, ok := seen[link]; ok {
				continue
			}

			seen[link] = struct{}{}
			links = append(links, link)
		}
	}
	if len(links) == 0 {
		return nil
	}

	return links
}

func classifyLibraryType(ctx context.Context, client *Client, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	libraryType, exactLibrary, resultType, err := client.resolvedLibraryType(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}

	samples, err := client.samplesForResolvedLibrary(ctx, libraryType, exactLibrary)
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

	if exactLibrary != nil {
		samples = filterSamplesForExactLibrary(samples, *exactLibrary)
		if len(samples) == 0 {
			return nil, false, nil, nil
		}
	}

	studyDetails, missing, detailErr := client.libraryStudyDetails(ctx, libraryType, exactLibrary, samples)
	if detailErr != nil {
		if isContextError(detailErr) {
			return nil, false, nil, detailErr
		}

		return nil, false, []MissingHop{missingHop(HopLibraries, detailErr)}, nil
	}

	flattened := flattenStudyDetailSamples(studyDetails)
	fetchTruncated := false
	if len(studyDetails) > 1 && len(flattened) > MaxSamplesPerHop {
		fetchTruncated = true
		flattened = flattened[:MaxSamplesPerHop]
		studyDetails = rebuildStudyDetailsFromSamples(studyDetails, flattened)
	}

	studies := make([]Study, 0, len(studyDetails))
	for _, detail := range studyDetails {
		studies = append(studies, detail.Study)
	}

	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       resultType,
		Graph: EnrichmentGraph{
			Library:      libraryLinkForExactMatch(exactLibrary),
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

func filterSamplesForExactLibrary(samples []Sample, exact Library) []Sample {
	filtered := make([]Sample, 0, len(samples))
	for _, sample := range samples {
		if sampleHasExactLibrary(sample, exact) {
			filtered = append(filtered, sample)
		}
	}

	return filtered
}

func sampleHasExactLibrary(sample Sample, exact Library) bool {
	for _, library := range sample.Libraries {
		if exact.LibraryID != "" && library.LibraryID == exact.LibraryID {
			return true
		}
		if exact.IDLibraryLims != "" && library.IDLibraryLims == exact.IDLibraryLims {
			return true
		}
	}

	return false
}

func flattenStudyDetailSamples(studyDetails []StudyDetail) []Sample {
	samples := make([]Sample, 0)
	for i := range studyDetails {
		samples = append(samples, flattenStudySamples(&studyDetails[i])...)
	}

	return samples
}

func flattenStudySamples(detail *StudyDetail) []Sample {
	if detail == nil {
		return nil
	}

	samples := make([]Sample, 0)
	for _, library := range detail.Libraries {
		samples = append(samples, library.Samples...)
	}

	return samples
}

func rebuildStudyDetailsFromSamples(original []StudyDetail, samples []Sample) []StudyDetail {
	sampleSet := make(map[string]struct{}, len(samples))
	for _, sample := range samples {
		sampleSet[sample.Name] = struct{}{}
	}

	result := make([]StudyDetail, 0, len(original))
	for _, detail := range original {
		filteredLibraries := make([]LibraryDetail, 0, len(detail.Libraries))
		hasAnySamples := false
		for _, library := range detail.Libraries {
			filteredSamples := make([]Sample, 0, len(library.Samples))
			for _, sample := range library.Samples {
				if _, keep := sampleSet[sample.Name]; keep {
					filteredSamples = append(filteredSamples, sample)
				}
			}
			if len(filteredSamples) == 0 {
				continue
			}

			hasAnySamples = true
			filteredLibraries = append(filteredLibraries, LibraryDetail{
				Library: library.Library,
				Samples: filteredSamples,
			})
		}
		if !hasAnySamples {
			continue
		}

		result = append(result, StudyDetail{
			Study:     detail.Study,
			Libraries: filteredLibraries,
		})
	}

	return result
}

func libraryLinkForExactMatch(exactLibrary *Library) *LibraryLink {
	if exactLibrary == nil {
		return nil
	}

	link := libraryLinkFromLibrary(*exactLibrary, exactLibrary.IDStudyLims)

	return &link
}

func distinctLibrariesForSamples(samples []Sample) []LibraryLink {
	seen := make(map[LibraryLink]struct{}, len(samples))
	libraries := make([]LibraryLink, 0, len(samples))
	for _, sample := range samples {
		for _, sampleLibrary := range sample.Libraries {
			if sampleLibrary.PipelineIDLims == "" {
				continue
			}

			library := libraryLinkFromLibrary(sampleLibrary, sampleLibrary.IDStudyLims)
			if _, exists := seen[library]; exists {
				continue
			}

			seen[library] = struct{}{}
			libraries = append(libraries, library)
		}
	}

	return libraries
}

func libraryLinkForSample(sample Sample) *LibraryLink {
	library, ok := samplePrimaryLibrary(sample)
	if !ok {
		return nil
	}

	link := libraryLinkFromLibrary(library, library.IDStudyLims)

	return &link
}

func samplePrimaryLibrary(sample Sample) (Library, bool) {
	for _, library := range sample.Libraries {
		if library.PipelineIDLims == "" {
			continue
		}

		return library, true
	}

	return Library{}, false
}

func flatLibrariesFromStudyDetail(detail *StudyDetail) []LibraryLink {
	if detail == nil {
		return nil
	}

	libraries := make([]LibraryLink, 0, len(detail.Libraries))
	for _, library := range detail.Libraries {
		if library.Library.PipelineIDLims == "" {
			continue
		}

		libraries = append(libraries, libraryLinkFromLibrary(library.Library, detail.Study.IDStudyLims))
	}

	return libraries
}

func libraryLinkFromLibrary(library Library, studyID string) LibraryLink {
	libraryStudyID := library.IDStudyLims
	if libraryStudyID == "" {
		libraryStudyID = studyID
	}

	return LibraryLink{
		LibraryType:   library.PipelineIDLims,
		IDStudyLims:   libraryStudyID,
		LibraryID:     library.LibraryID,
		IDLibraryLims: library.IDLibraryLims,
	}
}

func buildStudyDetails(studies []Study, samples []Sample) []StudyDetail {
	studyMap := make(map[string][]Sample)
	for _, sample := range samples {
		for _, studyID := range sampleStudyIDs(sample) {
			studyMap[studyID] = append(studyMap[studyID], sample)
		}
	}

	studyDetails := make([]StudyDetail, 0, len(studies))
	for _, study := range studies {
		studySamples := studyMap[study.IDStudyLims]
		if len(studySamples) == 0 {
			continue
		}

		studyDetails = append(studyDetails, buildStudyDetail(study, studySamples))
	}

	return studyDetails
}

func sampleStudyIDs(sample Sample) []string {
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

func buildStudyDetail(study Study, samples []Sample) StudyDetail {
	grouped := make(map[string]LibraryDetail)
	ordered := make([]string, 0)

	for _, sample := range samples {
		addStudyDetailSample(grouped, &ordered, study.IDStudyLims, sample)
	}

	libraries := make([]LibraryDetail, 0, len(grouped))
	for _, key := range ordered {
		libraries = append(libraries, grouped[key])
	}

	return StudyDetail{Study: study, Libraries: libraries}
}

func addStudyDetailSample(grouped map[string]LibraryDetail, ordered *[]string, studyLimsID string, sample Sample) {
	for _, library := range sample.Libraries {
		if library.PipelineIDLims == "" {
			continue
		}
		if library.IDStudyLims != "" && library.IDStudyLims != studyLimsID {
			continue
		}

		key := studyLibraryGroupKey(library, studyLimsID)
		if _, seen := grouped[key]; !seen {
			*ordered = append(*ordered, key)
			if library.IDStudyLims == "" {
				library.IDStudyLims = studyLimsID
			}
			grouped[key] = LibraryDetail{Library: library}
		}

		detail := grouped[key]
		detail.Samples = append(detail.Samples, sample)
		grouped[key] = detail
	}
}

func addMissingStudyLibraries(detail *StudyDetail, libraries []Library) {
	if detail == nil {
		return
	}

	seen := make(map[string]struct{}, len(detail.Libraries))
	for _, library := range detail.Libraries {
		seen[studyLibraryGroupKey(library.Library, detail.Study.IDStudyLims)] = struct{}{}
	}
	for _, library := range libraries {
		key := studyLibraryGroupKey(library, detail.Study.IDStudyLims)
		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}
		detail.Libraries = append(detail.Libraries, LibraryDetail{Library: library})
	}
}

func studyLibraryGroupKey(library Library, studyLimsID string) string {
	libraryStudyID := library.IDStudyLims
	if libraryStudyID == "" {
		libraryStudyID = studyLimsID
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

func libraryForExactStudy(exactLibrary Library, studyID string) Library {
	if exactLibrary.IDStudyLims == "" {
		exactLibrary.IDStudyLims = studyID
	}

	return exactLibrary
}

func classifySangerSampleName(ctx context.Context, client *Client, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	match, err := client.ResolveSampleName(ctx, identifier)
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}
		if isClientError(err) {
			return nil, false, nil, nil
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}
	if match.Sample == nil || match.Kind != KindSangerSampleName {
		return nil, false, nil, nil
	}

	return client.buildSampleEnrichment(ctx, identifier, KindSangerSampleName, []Sample{*match.Sample})
}

func classifyResolvedSample(ctx context.Context, client *Client, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	match, err := client.ResolveSample(ctx, identifier)
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

	return client.buildSampleEnrichment(ctx, identifier, identifierType, []Sample{*match.Sample})
}

func sampleIdentifierType(kind IdentifierKind) (IdentifierKind, bool) {
	switch kind {
	case KindSampleUUID,
		KindSampleLimsID,
		KindSangerSampleName,
		KindSangerSampleID,
		KindSupplierName,
		KindSampleAccession,
		KindDonorID:
		return kind, true
	default:
		return "", false
	}
}

func classifySangerSampleID(ctx context.Context, client *Client, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	return classifySampleIdentifier(ctx, client, identifier, KindSangerSampleID,
		func(ctx context.Context, client *Client, identifier string) ([]Sample, error) {
			return client.FindSamplesBySangerID(ctx, identifier)
		})
}

func classifySampleLimsID(ctx context.Context, client *Client, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	return classifySampleIdentifier(ctx, client, identifier, KindSampleLimsID,
		func(ctx context.Context, client *Client, identifier string) ([]Sample, error) {
			return client.FindSamplesByIDSampleLims(ctx, identifier)
		})
}

func classifySampleAccession(ctx context.Context, client *Client, identifier string) (*EnrichmentResult, bool, []MissingHop, error) {
	return classifySampleIdentifier(ctx, client, identifier, KindSampleAccession,
		func(ctx context.Context, client *Client, identifier string) ([]Sample, error) {
			return client.FindSamplesByAccessionNumber(ctx, identifier)
		})
}

func optionalDetailSlice[T any](values []T, err error) ([]T, error) {
	if err == nil {
		return values, nil
	}
	if errors.Is(err, ErrNotFound) && !errors.Is(err, ErrCacheNeverSynced) {
		return []T{}, nil
	}

	return nil, err
}

func singleStudyPointer(studies []Study) *Study {
	if len(studies) != 1 {
		return nil
	}

	study := studies[0]

	return &study
}

func sampleLookupKey(sample Sample) string {
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

func fillSampleDetailLibraryLinks(detail *SampleDetail) {
	if detail == nil || len(detail.Sample.Libraries) > 0 || len(detail.Libraries) == 0 {
		return
	}

	detail.Sample.Libraries = append([]Library(nil), detail.Libraries...)
}

func matchLibraryType(match Match) string {
	if match.Library != nil {
		return strings.TrimSpace(match.Library.PipelineIDLims)
	}
	if match.Kind == KindLibraryType {
		return strings.TrimSpace(match.Canonical)
	}

	return ""
}

func exactLibraryMatch(library *Library) *Library {
	if library == nil {
		return nil
	}
	if strings.TrimSpace(library.LibraryID) == "" && strings.TrimSpace(library.IDLibraryLims) == "" {
		return nil
	}

	return library
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

// Enrich resolves an identifier into its enrichment graph.
func (c *Client) Enrich(ctx context.Context, identifier string) (EnrichmentResult, error) {
	if strings.TrimSpace(identifier) == "" {
		return EnrichmentResult{}, ErrNotFound
	}
	if looksLikeLibraryType(identifier) {
		return c.enrichLibraryLikeIdentifier(ctx, identifier)
	}

	missing := make([]MissingHop, 0, len(enrichClassifiers))
	for _, classifier := range enrichClassifiers {
		result, matched, classifyMissing, err := classifier.classify(ctx, c, identifier)
		if err != nil {
			return EnrichmentResult{}, err
		}

		missing = append(missing, classifyMissing...)
		if matched {
			return finishEnrichmentResult(result, missing), nil
		}
	}

	if allClassificationHopsFailed(identifier, missing) {
		return EnrichmentResult{}, ErrUpstreamImpaired
	}

	return EnrichmentResult{}, ErrNotFound
}

func (c *Client) enrichLibraryLikeIdentifier(ctx context.Context, identifier string) (EnrichmentResult, error) {
	result, matched, missing, err := classifyLibraryType(ctx, c, identifier)
	if err != nil {
		return EnrichmentResult{}, err
	}
	if matched {
		return finishEnrichmentResult(result, missing), nil
	}
	if allClassificationHopsFailed(identifier, missing) {
		return EnrichmentResult{}, ErrUpstreamImpaired
	}

	return EnrichmentResult{}, ErrNotFound
}

func (c *Client) enrichStudy(ctx context.Context, study *Study, result *EnrichmentResult) error {
	if study == nil || result == nil {
		return nil
	}

	detail, err := c.StudyDetail(ctx, study.IDStudyLims)
	if err != nil {
		if isContextError(err) {
			return err
		}

		result.Graph.Study = study
		result.Graph.Studies = []Study{*study}
		result.Partial = true
		result.Missing = append(result.Missing, missingHop(HopSamples, err))

		return nil
	}

	result.Graph.Study = &detail.Study
	result.Graph.Studies = []Study{detail.Study}
	result.Graph.Samples = flattenStudySamples(&detail)
	result.Graph.Libraries = flatLibrariesFromStudyDetail(&detail)
	result.Graph.StudyDetail = &detail

	return nil
}

func (c *Client) buildSampleEnrichment(
	ctx context.Context,
	identifier string,
	identifierType IdentifierKind,
	samples []Sample,
) (*EnrichmentResult, bool, []MissingHop, error) {
	sample := samples[0]
	detail, err := c.SampleDetail(ctx, sampleLookupKey(sample))
	if err != nil {
		if isContextError(err) {
			return nil, false, nil, err
		}

		return nil, false, []MissingHop{missingHop(HopClassify, err)}, nil
	}
	fillSampleDetailLibraryLinks(&detail)

	result := &EnrichmentResult{
		Identifier: identifier,
		Type:       identifierType,
		Graph: EnrichmentGraph{
			Sample:       &detail.Sample,
			Samples:      samples,
			SampleDetail: &detail,
			Library:      libraryLinkForSample(detail.Sample),
		},
	}
	if detail.Study != nil {
		result.Graph.Study = detail.Study
	} else if err = c.enrichSampleStudy(ctx, detail.Sample, result); err != nil {
		return nil, false, nil, err
	}

	return result, true, nil, nil
}

func (c *Client) enrichSampleStudy(ctx context.Context, sample Sample, result *EnrichmentResult) error {
	if result == nil {
		return nil
	}

	studies, err := c.StudiesForSample(ctx, sample.Name)
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
		result.Graph.Sample.Studies = append([]Study(nil), studies...)
	}
	if result.Graph.SampleDetail != nil {
		result.Graph.SampleDetail.Sample.Studies = append([]Study(nil), studies...)
	}
	result.Graph.Studies = append([]Study(nil), studies...)

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

func (c *Client) resolvedLibraryType(ctx context.Context, identifier string) (string, *Library, IdentifierKind, error) {
	match, err := c.ResolveLibrary(ctx, identifier)
	if err == nil {
		libraryType := strings.TrimSpace(matchLibraryType(match))
		if libraryType != "" {
			resultType := match.Kind
			if resultType == "" {
				resultType = KindLibraryType
			}

			return libraryType, exactLibraryMatch(match.Library), resultType, nil
		}
	}
	if err != nil && !isClientError(err) {
		return "", nil, "", err
	}

	return identifier, nil, KindLibraryType, nil
}

func (c *Client) samplesForResolvedLibrary(ctx context.Context, libraryType string, exactLibrary *Library) ([]Sample, error) {
	if exactLibrary != nil {
		switch {
		case strings.TrimSpace(exactLibrary.LibraryID) != "":
			return c.SamplesForLibraryID(ctx, exactLibrary.LibraryID, MaxSamplesPerHop+1, 0)
		case strings.TrimSpace(exactLibrary.IDLibraryLims) != "":
			return c.SamplesForLibraryLimsID(ctx, exactLibrary.IDLibraryLims, MaxSamplesPerHop+1, 0)
		}
	}

	return c.samplesForLibraryType(ctx, libraryType)
}

func (c *Client) samplesForLibraryType(ctx context.Context, identifier string) ([]Sample, error) {
	samples, err := c.SamplesForLibraryType(ctx, identifier, MaxSamplesPerHop+1, 0)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		return samples, nil
	}

	return c.FindSamplesByLibraryType(ctx, identifier)
}

func (c *Client) libraryStudyDetails(
	ctx context.Context,
	libraryType string,
	exactLibrary *Library,
	samples []Sample,
) ([]StudyDetail, []MissingHop, error) {
	studySamplesByID := make(map[string][]Sample)
	orderedStudyIDs := make([]string, 0)
	for _, sample := range samples {
		for _, studyID := range sampleStudyIDs(sample) {
			if _, seen := studySamplesByID[studyID]; !seen {
				orderedStudyIDs = append(orderedStudyIDs, studyID)
			}

			studySamplesByID[studyID] = append(studySamplesByID[studyID], sample)
		}
	}

	studiesByID, missingStudyMetadata, err := c.studiesByIDForLibraryDetails(ctx, samples, orderedStudyIDs)
	if err != nil {
		return nil, nil, err
	}

	studyDetails := make([]StudyDetail, 0, len(orderedStudyIDs))
	missing := make([]MissingHop, 0, 1)
	truncated := false
	for _, studyID := range orderedStudyIDs {
		study, ok := studiesByID[studyID]
		if !ok {
			continue
		}

		studySamples := studySamplesByID[studyID]
		if len(studySamples) == 0 {
			continue
		}
		libraryDetail, err := c.LibraryDetail(ctx, libraryType, studyID)
		if err != nil {
			if !isClientError(err) {
				return nil, nil, err
			}
			libraryDetail = LibraryDetail{
				Library: Library{PipelineIDLims: libraryType, IDStudyLims: studyID},
				Samples: studySamples,
			}
		}

		studySamples = libraryDetail.Samples
		if exactLibrary != nil {
			studySamples = filterSamplesForExactLibrary(studySamples, *exactLibrary)
			if len(studySamples) == 0 {
				continue
			}
			libraryDetail.Library = libraryForExactStudy(*exactLibrary, studyID)
		}

		if len(studySamples) > MaxSamplesPerHop {
			truncated = true
			studySamples = studySamples[:MaxSamplesPerHop]
		}

		detail := buildStudyDetail(study, studySamples)
		if len(detail.Libraries) == 0 {
			libraryDetail.Samples = studySamples
			detail.Libraries = []LibraryDetail{libraryDetail}
		}

		studyDetails = append(studyDetails, detail)
	}
	if truncated {
		missing = append(missing, MissingHop{Hop: HopLibraries, Reason: ReasonSamplesTruncated, Status: http.StatusOK})
	}
	if missingStudyMetadata {
		missing = append(missing, MissingHop{Hop: HopStudies, Reason: ReasonNotFound, Status: http.StatusOK})
	}

	return studyDetails, missing, nil
}

func (c *Client) studiesByIDForLibraryDetails(
	ctx context.Context,
	samples []Sample,
	orderedStudyIDs []string,
) (map[string]Study, bool, error) {
	studiesByID := make(map[string]Study, len(orderedStudyIDs))
	for _, sample := range samples {
		for _, study := range sample.Studies {
			if study.IDStudyLims == "" {
				continue
			}
			if _, seen := studiesByID[study.IDStudyLims]; seen {
				continue
			}

			studiesByID[study.IDStudyLims] = study
		}
	}

	missingStudyMetadata := false
	for _, studyID := range orderedStudyIDs {
		if _, ok := studiesByID[studyID]; ok {
			continue
		}

		study, err := c.studyDetailStudy(ctx, studyID)
		if err != nil {
			if isClientError(err) {
				studiesByID[studyID] = Study{IDStudyLims: studyID, IDLims: sqscpIDLims}
				missingStudyMetadata = true

				continue
			}

			return nil, false, err
		}

		studiesByID[studyID] = study
	}

	return studiesByID, missingStudyMetadata, nil
}

// SampleDetail returns a sample and its related studies, lanes, libraries, and
// iRODS paths from the synced cache.
func (c *Client) SampleDetail(ctx context.Context, sangerName string) (SampleDetail, error) {
	match, err := c.ResolveSampleName(ctx, sangerName)
	if err != nil {
		return SampleDetail{}, err
	}
	if match.Sample == nil {
		return SampleDetail{}, ErrNotFound
	}

	sample := *match.Sample
	studies, err := c.optionalStudiesForSample(ctx, sample.Name)
	if err != nil {
		return SampleDetail{}, err
	}
	sample.Studies = append([]Study(nil), studies...)

	lanes, err := optionalDetailSlice(c.LanesForSample(ctx, sample.Name, detailFetchLimit, 0))
	if err != nil {
		return SampleDetail{}, err
	}
	paths, err := optionalDetailSlice(c.IRODSPathsForSample(ctx, sample.Name, detailFetchLimit, 0))
	if err != nil {
		return SampleDetail{}, err
	}

	return SampleDetail{
		Sample:     sample,
		Study:      singleStudyPointer(studies),
		Lanes:      lanes,
		Libraries:  append([]Library(nil), sample.Libraries...),
		IRODSPaths: paths,
	}, nil
}

// StudyDetail returns a study and grouped library/sample details from the synced
// cache. The result is de-duplicated: each distinct study and library is carried
// once in the lookup tables and the nested sample rows reference them by id (see
// StudyDetail). It returns every nested row in the default non-lean shape; the
// HTTP handler reaches studyDetailWithOptions for limit/offset/lean.
func (c *Client) StudyDetail(ctx context.Context, studyLimsID string) (StudyDetail, error) {
	detail, _, err := c.studyDetailWithOptions(ctx, studyLimsID, defaultDetailOptions())

	return detail, err
}

// RunDetail returns a run with its samples, studies, and grouped study details
// from the synced cache. The result is de-duplicated: each distinct study and
// library is carried once in the lookup tables and the nested rows reference them
// by id (see RunDetail). It returns every nested row in the default non-lean
// shape; the HTTP handler reaches runDetailWithOptions for limit/offset/lean.
func (c *Client) RunDetail(ctx context.Context, idRun string) (RunDetail, error) {
	detail, _, err := c.runDetailWithOptions(ctx, idRun, defaultDetailOptions())

	return detail, err
}

// LibraryDetail returns a study-scoped library and the samples it covers from
// the synced cache.
func (c *Client) LibraryDetail(ctx context.Context, pipelineIDLims, studyLimsID string) (LibraryDetail, error) {
	samples, err := c.SamplesForLibrary(ctx, pipelineIDLims, studyLimsID, detailFetchLimit, 0)
	if err != nil {
		return LibraryDetail{}, err
	}

	return LibraryDetail{
		Library: Library{PipelineIDLims: pipelineIDLims, IDStudyLims: studyLimsID},
		Samples: samples,
	}, nil
}

func (c *Client) studyDetailStudy(ctx context.Context, studyLimsID string) (Study, error) {
	study, err := c.resolveStudyFromCache(
		ctx,
		`SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
		studyLimsID,
	)
	if err == nil {
		return *study, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Study{}, err
	}
	if syncErr := c.requireAnySyncState(ctx, syncTableStudy); syncErr != nil {
		return Study{}, syncErr
	}

	return Study{}, ErrNotFound
}

func (c *Client) optionalStudiesForSample(ctx context.Context, sangerName string) ([]Study, error) {
	return optionalDetailSlice(c.StudiesForSample(ctx, sangerName))
}

func (c *Client) studiesForSamples(ctx context.Context, samples []Sample) ([]Study, []Sample, error) {
	studies := make([]Study, 0, len(samples))
	seen := make(map[string]struct{}, len(samples))

	for index := range samples {
		sampleStudies, err := c.sampleStudies(ctx, samples[index])
		if err != nil {
			return nil, nil, err
		}
		samples[index].Studies = append([]Study(nil), sampleStudies...)

		for _, study := range sampleStudies {
			if study.IDStudyLims == "" {
				continue
			}
			if _, ok := seen[study.IDStudyLims]; ok {
				continue
			}

			seen[study.IDStudyLims] = struct{}{}
			studies = append(studies, study)
		}
	}

	return studies, samples, nil
}

func (c *Client) sampleStudies(ctx context.Context, sample Sample) ([]Study, error) {
	if len(sample.Studies) > 0 {
		return sample.Studies, nil
	}
	if strings.TrimSpace(sample.Name) == "" {
		return nil, nil
	}

	return c.optionalStudiesForSample(ctx, sample.Name)
}
