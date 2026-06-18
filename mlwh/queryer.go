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

import "context"

// Queryer describes the complete MLWH read/query surface.
type Queryer interface {
	// Classification and resolution.
	ClassifyIdentifier(ctx context.Context, raw string) (Match, error)
	ResolveSample(ctx context.Context, raw string) (Match, error)
	ResolveSampleName(ctx context.Context, raw string) (Match, error)
	ResolveStudy(ctx context.Context, raw string) (Match, error)
	ResolveRun(ctx context.Context, raw string) (Match, error)
	ResolveLibrary(ctx context.Context, raw string) (Match, error)
	ResolveLibraryIdentifier(ctx context.Context, raw string) (Match, error)

	// Enumeration and hierarchy.
	AllStudies(ctx context.Context, limit, offset int) ([]Study, error)
	SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Sample, error)
	SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]Sample, error)
	SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]Sample, error)
	SamplesForLibraryID(ctx context.Context, libraryID string, limit, offset int) ([]Sample, error)
	SamplesForLibraryLimsID(ctx context.Context, idLibraryLims string, limit, offset int) ([]Sample, error)
	SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]Sample, error)
	LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Library, error)
	RunsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Run, error)
	LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]Lane, error)
	IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]IRODSPath, error)
	IRODSPathsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]IRODSPath, error)
	StudiesForSample(ctx context.Context, sangerName string) ([]Study, error)

	// Sample finders.
	FindSamplesBySangerID(ctx context.Context, sangerID string) ([]Sample, error)
	FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]Sample, error)
	FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]Sample, error)
	FindSamplesBySupplierName(ctx context.Context, supplierName string) ([]Sample, error)
	FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]Sample, error)

	// Expansion.
	ExpandIdentifier(ctx context.Context, kind IdentifierKind, canonical string) ([]TaggedID, error)
	ExpandSearchValues(ctx context.Context, kind IdentifierKind, canonical string) (SearchValues, error)
	ExpandSampleSearchValues(ctx context.Context, kind IdentifierKind, canonical string) ([]string, error)

	// Enrichment graph and detail aggregates.
	Enrich(ctx context.Context, identifier string) (EnrichmentResult, error)
	SampleDetail(ctx context.Context, sangerName string) (SampleDetail, error)
	StudyDetail(ctx context.Context, studyLimsID string) (StudyDetail, error)
	RunDetail(ctx context.Context, idRun string) (RunDetail, error)
	LibraryDetail(ctx context.Context, pipelineIDLims, studyLimsID string) (LibraryDetail, error)
}

var _ Queryer = (*Client)(nil)
