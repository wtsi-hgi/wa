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

// Package mlwh exposes cache-backed MLWH queries.
//
// Add a new MLWH query by completing four steps: add any required schema
// column and index in both cache dialects, add one Client method, add one
// Queryer member, and add one Registry entry.
package mlwh

const registryVerbGet = "GET"

// Endpoint describes one Queryer method's REST endpoint.
type Endpoint struct {
	Method     string
	Verb       string
	Path       string
	PathParams []string
	Query      []string
	Paginated  bool
	NewResult  func() any
}

// Registry is the single source from which the handler and RemoteClient derive.
// Adding a Queryer method requires adding a Registry entry so local and remote
// query surfaces stay aligned.
var Registry = []Endpoint{
	{
		Method:     "ClassifyIdentifier",
		Verb:       registryVerbGet,
		Path:       "/classify/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[Match],
	},
	{
		Method:     "ResolveSample",
		Verb:       registryVerbGet,
		Path:       "/resolve/sample/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[Match],
	},
	{
		Method:     "ResolveSampleName",
		Verb:       registryVerbGet,
		Path:       "/resolve/sample-name/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[Match],
	},
	{
		Method:     "ResolveStudy",
		Verb:       registryVerbGet,
		Path:       "/resolve/study/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[Match],
	},
	{
		Method:     "ResolveRun",
		Verb:       registryVerbGet,
		Path:       "/resolve/run/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[Match],
	},
	{
		Method:     "ResolveLibrary",
		Verb:       registryVerbGet,
		Path:       "/resolve/library/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[Match],
	},
	{
		Method:     "ResolveLibraryIdentifier",
		Verb:       registryVerbGet,
		Path:       "/resolve/library-identifier/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[Match],
	},
	{
		Method:    "AllStudies",
		Verb:      registryVerbGet,
		Path:      "/studies",
		Query:     []string{},
		Paginated: true,
		NewResult: newSliceResult[Study],
	},
	{
		Method:     "SamplesForStudy",
		Verb:       registryVerbGet,
		Path:       "/study/:id/samples",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "SamplesForRun",
		Verb:       registryVerbGet,
		Path:       "/run/:id/samples",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "SamplesForLibrary",
		Verb:       registryVerbGet,
		Path:       "/library/:pipeline/study/:study/samples",
		PathParams: []string{"pipeline", "study"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "SamplesForLibraryID",
		Verb:       registryVerbGet,
		Path:       "/library-id/:id/samples",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "SamplesForLibraryLimsID",
		Verb:       registryVerbGet,
		Path:       "/library-lims-id/:id/samples",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "SamplesForLibraryType",
		Verb:       registryVerbGet,
		Path:       "/library-type/:id/samples",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "LibrariesForStudy",
		Verb:       registryVerbGet,
		Path:       "/study/:id/libraries",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[Library],
	},
	{
		Method:     "RunsForStudy",
		Verb:       registryVerbGet,
		Path:       "/study/:id/runs",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[Run],
	},
	{
		Method:     "LanesForSample",
		Verb:       registryVerbGet,
		Path:       "/sample/:id/lanes",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[Lane],
	},
	{
		Method:     "IRODSPathsForSample",
		Verb:       registryVerbGet,
		Path:       "/sample/:id/irods",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[IRODSPath],
	},
	{
		Method:     "IRODSPathsForStudy",
		Verb:       registryVerbGet,
		Path:       "/study/:id/irods",
		PathParams: []string{"id"},
		Query:      []string{},
		Paginated:  true,
		NewResult:  newSliceResult[IRODSPath],
	},
	{
		Method:     "StudiesForSample",
		Verb:       registryVerbGet,
		Path:       "/sample/:id/studies",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newSliceResult[Study],
	},
	{
		Method:     "FindSamplesBySangerID",
		Verb:       registryVerbGet,
		Path:       "/find/sample/sanger-id/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "FindSamplesByIDSampleLims",
		Verb:       registryVerbGet,
		Path:       "/find/sample/lims-id/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "FindSamplesByAccessionNumber",
		Verb:       registryVerbGet,
		Path:       "/find/sample/accession/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "FindSamplesBySupplierName",
		Verb:       registryVerbGet,
		Path:       "/find/sample/supplier-name/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "FindSamplesByLibraryType",
		Verb:       registryVerbGet,
		Path:       "/find/sample/library-type/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newSliceResult[Sample],
	},
	{
		Method:     "ExpandIdentifier",
		Verb:       registryVerbGet,
		Path:       "/expand/:kind/:id",
		PathParams: []string{"kind", "id"},
		Query:      []string{},
		NewResult:  newSliceResult[TaggedID],
	},
	{
		Method:     "ExpandSearchValues",
		Verb:       registryVerbGet,
		Path:       "/expand-search/:kind/:id",
		PathParams: []string{"kind", "id"},
		Query:      []string{},
		NewResult:  newResult[SearchValues],
	},
	{
		Method:     "ExpandSampleSearchValues",
		Verb:       registryVerbGet,
		Path:       "/expand-sample-search/:kind/:id",
		PathParams: []string{"kind", "id"},
		Query:      []string{},
		NewResult:  newSliceResult[string],
	},
	{
		Method:     "Enrich",
		Verb:       registryVerbGet,
		Path:       "/enrich/:id",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[EnrichmentResult],
	},
	{
		Method:     "SampleDetail",
		Verb:       registryVerbGet,
		Path:       "/sample/:id/detail",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[SampleDetail],
	},
	{
		Method:     "StudyDetail",
		Verb:       registryVerbGet,
		Path:       "/study/:id/detail",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[StudyDetail],
	},
	{
		Method:     "RunDetail",
		Verb:       registryVerbGet,
		Path:       "/run/:id/detail",
		PathParams: []string{"id"},
		Query:      []string{},
		NewResult:  newResult[RunDetail],
	},
	{
		Method:     "LibraryDetail",
		Verb:       registryVerbGet,
		Path:       "/library/:pipeline/study/:study/detail",
		PathParams: []string{"pipeline", "study"},
		Query:      []string{},
		NewResult:  newResult[LibraryDetail],
	},
}

func newResult[T any]() any {
	return new(T)
}

func newSliceResult[T any]() any {
	result := []T{}

	return &result
}
