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

// Endpoint describes one Queryer method's REST endpoint. Summary, Description,
// and QueryParams are the enriched metadata the OpenAPI document and the human
// endpoint reference derive from; every entry carries a non-empty Summary and
// Description, and every Paginated entry declares limit/offset QueryParams.
type Endpoint struct {
	Method      string
	Verb        string
	Path        string
	PathParams  []string
	Query       []string
	Paginated   bool
	NewResult   func() any
	Summary     string       // short, human-readable (required, non-empty)
	Description string       // longer description (required, non-empty)
	QueryParams []QueryParam // structured specs for limit/offset and any filters
}

// Registry is the single source from which the handler and RemoteClient derive.
// Adding a Queryer method requires adding a Registry entry so local and remote
// query surfaces stay aligned.
var Registry = []Endpoint{
	{
		Method:      "ClassifyIdentifier",
		Verb:        registryVerbGet,
		Path:        "/classify/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Match],
		Summary:     "Classify an identifier",
		Description: "Detects the kind of the given raw identifier and returns its canonical form with any directly matching study, sample, run, or library.",
	},
	{
		Method:      "ResolveSample",
		Verb:        registryVerbGet,
		Path:        "/resolve/sample/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Match],
		Summary:     "Resolve a sample identifier",
		Description: "Resolves any supported sample identifier (UUID, LIMS id, Sanger sample name or id, supplier name, accession, or donor id) to its canonical sample Match.",
	},
	{
		Method:      "ResolveSampleName",
		Verb:        registryVerbGet,
		Path:        "/resolve/sample-name/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Match],
		Summary:     "Resolve a Sanger sample name",
		Description: "Resolves a Sanger sample name to its canonical sample Match, disambiguating it from other sample identifier forms.",
	},
	{
		Method:      "ResolveStudy",
		Verb:        registryVerbGet,
		Path:        "/resolve/study/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Match],
		Summary:     "Resolve a study identifier",
		Description: "Resolves any supported study identifier (UUID, LIMS id, accession, or name) to its canonical study Match.",
	},
	{
		Method:      "ResolveRun",
		Verb:        registryVerbGet,
		Path:        "/resolve/run/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Match],
		Summary:     "Resolve a run identifier",
		Description: "Resolves a sequencing run identifier to its canonical run Match.",
	},
	{
		Method:      "ResolveLibrary",
		Verb:        registryVerbGet,
		Path:        "/resolve/library/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Match],
		Summary:     "Resolve a library by type",
		Description: "Resolves a library identified by its library type to its canonical library Match.",
	},
	{
		Method:      "ResolveLibraryIdentifier",
		Verb:        registryVerbGet,
		Path:        "/resolve/library-identifier/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Match],
		Summary:     "Resolve a library identifier",
		Description: "Resolves a library identifier (library id or LIMS library id) to its canonical library Match.",
	},
	{
		Method:      "AllStudies",
		Verb:        registryVerbGet,
		Path:        "/studies",
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Study],
		Summary:     "List all studies",
		Description: "Lists every study mirrored in the cache, ordered by LIMS study id. Defaults to returning all studies; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "SamplesForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/samples",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Sample],
		Summary:     "List samples in a study",
		Description: "Lists the distinct samples linked to the given study via its libraries. Defaults to returning all samples; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "SamplesForRun",
		Verb:        registryVerbGet,
		Path:        "/run/:id/samples",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Sample],
		Summary:     "List samples on a run",
		Description: "Lists the samples sequenced on the given run. Defaults to returning all samples; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "SamplesForLibrary",
		Verb:        registryVerbGet,
		Path:        "/library/:pipeline/study/:study/samples",
		PathParams:  []string{"pipeline", "study"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Sample],
		Summary:     "List samples in a library",
		Description: "Lists the samples in the library identified by its pipeline LIMS id within the given study. Defaults to returning all samples; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "SamplesForLibraryID",
		Verb:        registryVerbGet,
		Path:        "/library-id/:id/samples",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Sample],
		Summary:     "List samples by library id",
		Description: "Lists the samples in the library with the given library id. Defaults to returning all samples; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "SamplesForLibraryLimsID",
		Verb:        registryVerbGet,
		Path:        "/library-lims-id/:id/samples",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Sample],
		Summary:     "List samples by LIMS library id",
		Description: "Lists the samples in the library with the given LIMS library id. Defaults to returning all samples; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "SamplesForLibraryType",
		Verb:        registryVerbGet,
		Path:        "/library-type/:id/samples",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Sample],
		Summary:     "List samples by library type",
		Description: "Lists the samples in libraries of the given library type. Defaults to returning all samples; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "LibrariesForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/libraries",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Library],
		Summary:     "List libraries in a study",
		Description: "Lists the libraries belonging to the given study. Defaults to returning all libraries; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "RunsForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/runs",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Run],
		Summary:     "List runs for a study",
		Description: "Lists the sequencing runs associated with the given study. Defaults to returning all runs; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "LanesForSample",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/lanes",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Lane],
		Summary:     "List lanes for a sample",
		Description: "Lists the run/lane/tag combinations on which the given sample (by Sanger sample name) was sequenced. Defaults to returning all lanes; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "IRODSPathsForSample",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/irods",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[IRODSPath],
		Summary:     "List iRODS paths for a sample",
		Description: "Lists the iRODS data-object paths exported for the given sample (by Sanger sample name). Defaults to returning all paths; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "IRODSPathsForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/irods",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[IRODSPath],
		Summary:     "List iRODS paths for a study",
		Description: "Lists the iRODS data-object paths exported for the given study. Defaults to returning all paths; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "StudiesForSample",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/studies",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newSliceResult[Study],
		Summary:     "List studies for a sample",
		Description: "Lists the studies the given sample (by Sanger sample name) belongs to.",
	},
	{
		Method:      "FindSamplesBySangerID",
		Verb:        registryVerbGet,
		Path:        "/find/sample/sanger-id/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newSliceResult[Sample],
		Summary:     "Find samples by Sanger sample id",
		Description: "Returns the samples whose Sanger sample id exactly matches the given value.",
	},
	{
		Method:      "FindSamplesByIDSampleLims",
		Verb:        registryVerbGet,
		Path:        "/find/sample/lims-id/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newSliceResult[Sample],
		Summary:     "Find samples by LIMS sample id",
		Description: "Returns the samples whose LIMS sample id exactly matches the given value.",
	},
	{
		Method:      "FindSamplesByAccessionNumber",
		Verb:        registryVerbGet,
		Path:        "/find/sample/accession/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newSliceResult[Sample],
		Summary:     "Find samples by accession number",
		Description: "Returns the samples whose accession number exactly matches the given value.",
	},
	{
		Method:      "FindSamplesBySupplierName",
		Verb:        registryVerbGet,
		Path:        "/find/sample/supplier-name/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newSliceResult[Sample],
		Summary:     "Find samples by supplier name",
		Description: "Returns the samples whose supplier name exactly matches the given value.",
	},
	{
		Method:      "FindSamplesByLibraryType",
		Verb:        registryVerbGet,
		Path:        "/find/sample/library-type/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newSliceResult[Sample],
		Summary:     "Find samples by library type",
		Description: "Returns the samples whose library type exactly matches the given value.",
	},
	{
		Method:      "ExpandIdentifier",
		Verb:        registryVerbGet,
		Path:        "/expand/:kind/:id",
		PathParams:  []string{"kind", "id"},
		Query:       []string{},
		NewResult:   newSliceResult[TaggedID],
		Summary:     "Expand an identifier to related identifiers",
		Description: "Expands the given identifier of the named kind into the set of related canonical identifiers (kind and canonical value) reachable from it.",
	},
	{
		Method:      "ExpandSearchValues",
		Verb:        registryVerbGet,
		Path:        "/expand-search/:kind/:id",
		PathParams:  []string{"kind", "id"},
		Query:       []string{},
		NewResult:   newResult[SearchValues],
		Summary:     "Expand an identifier to result-search values",
		Description: "Expands the given identifier into the sample, run, and lane values used to search downstream results.",
	},
	{
		Method:      "ExpandSampleSearchValues",
		Verb:        registryVerbGet,
		Path:        "/expand-sample-search/:kind/:id",
		PathParams:  []string{"kind", "id"},
		Query:       []string{},
		NewResult:   newSliceResult[string],
		Summary:     "Expand an identifier to sample search values",
		Description: "Expands the given identifier into the list of sample values used to search downstream results.",
	},
	{
		Method:      "Enrich",
		Verb:        registryVerbGet,
		Path:        "/enrich/:id",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[EnrichmentResult],
		Summary:     "Enrich an identifier",
		Description: "Classifies the given identifier and walks the MLWH graph to assemble its related studies, samples, and libraries, reporting any missing or truncated hops.",
	},
	{
		Method:      "SampleDetail",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/detail",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[SampleDetail],
		Summary:     "Get sample detail",
		Description: "Returns the given sample (by Sanger sample name) with its study, lanes, libraries, and iRODS paths.",
	},
	{
		Method:      "StudyDetail",
		Verb:        registryVerbGet,
		Path:        "/study/:id/detail",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[StudyDetail],
		Summary:     "Get study detail",
		Description: "Returns the given study with the detail of each of its libraries and their samples.",
	},
	{
		Method:      "RunDetail",
		Verb:        registryVerbGet,
		Path:        "/run/:id/detail",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[RunDetail],
		Summary:     "Get run detail",
		Description: "Returns the given run with its related samples, studies, and per-study detail.",
	},
	{
		Method:      "LibraryDetail",
		Verb:        registryVerbGet,
		Path:        "/library/:pipeline/study/:study/detail",
		PathParams:  []string{"pipeline", "study"},
		Query:       []string{},
		NewResult:   newResult[LibraryDetail],
		Summary:     "Get library detail",
		Description: "Returns the library identified by its pipeline LIMS id within the given study, together with the samples it covers.",
	},
	{
		Method:      "SearchStudies",
		Verb:        registryVerbGet,
		Path:        "/search/study/:term",
		PathParams:  []string{"term"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Study],
		Summary:     "Search studies by substring",
		Description: "Returns studies whose name, title, programme, or faculty sponsor contains the term (case-insensitive substring, minimum 3 characters). Defaults to a page of 100, maximum 1000.",
		QueryParams: searchPaginationParams(),
	},
	{
		Method:      "SearchSamples",
		Verb:        registryVerbGet,
		Path:        "/search/sample/:term",
		PathParams:  []string{"term"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Sample],
		Summary:     "Search samples by word prefix",
		Description: "Returns samples having a word in name, supplier name, common name, or donor id that starts with the term (case-insensitive word-prefix match, minimum 3 characters), backed by a word-token prefix index for the large sample table. So \"musculus\" and \"mus\" both match \"Mus Musculus\"; a substring inside a word does not. Defaults to a page of 100, maximum 1000.",
		QueryParams: searchPaginationParams(),
	},
	{
		Method:      "CountStudySearch",
		Verb:        registryVerbGet,
		Path:        "/search/study/:term/count",
		PathParams:  []string{"term"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count studies matching a substring",
		Description: "Returns the number of studies matching the same substring search as /search/study/:term, without transferring rows.",
	},
	{
		Method:      "CountSampleSearch",
		Verb:        registryVerbGet,
		Path:        "/search/sample/:term/count",
		PathParams:  []string{"term"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples matching a word prefix",
		Description: "Returns the number of samples matching the same word-prefix search as /search/sample/:term, without transferring rows. The count is exact up to a bound and reports that bound as a floor for very common terms.",
	},
	{
		Method:      "CountStudies",
		Verb:        registryVerbGet,
		Path:        "/studies/count",
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count all studies",
		Description: "Returns the total number of studies mirrored in the cache, the count counterpart of /studies.",
	},
	{
		Method:      "CountSamplesForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/samples/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples in a study",
		Description: "Returns the number of distinct samples linked to the given study, the count counterpart of /study/:id/samples.",
	},
	{
		Method:      "Freshness",
		Verb:        registryVerbGet,
		Path:        "/freshness",
		Query:       []string{},
		NewResult:   newResult[Freshness],
		Summary:     "Report cache freshness",
		Description: "Reports, per mirrored sync table, its high-water mark and last sync run time (UTC RFC3339) and whether it has ever synced. Succeeds even on a never-synced cache so callers can degrade gracefully.",
	},
}

// QueryParam is a structured specification of one query-string parameter,
// consumed by the OpenAPI generator and the human reference to describe the
// limit/offset pagination controls (and any future filters).
type QueryParam struct {
	Name        string // e.g. "limit"
	Type        string // OpenAPI type, e.g. "integer"
	Required    bool
	Description string
}

// fetchAllPaginationParams are the limit/offset QueryParams for the fetch-all
// paginated endpoints, whose limit defaults to the fetch-all page size so
// callers receive every row unless they page deliberately.
func fetchAllPaginationParams() []QueryParam {
	return []QueryParam{
		{
			Name:        "limit",
			Type:        "integer",
			Required:    false,
			Description: "maximum number of rows to return; defaults to a fetch-all page that returns every matching row",
		},
		{
			Name:        "offset",
			Type:        "integer",
			Required:    false,
			Description: "number of leading rows to skip before returning results; defaults to 0",
		},
	}
}

// searchPaginationParams are the limit/offset QueryParams for the substring
// search endpoints, whose limit defaults to 100 and is capped at 1000 (a larger
// limit is rejected, not clamped).
func searchPaginationParams() []QueryParam {
	return []QueryParam{
		{
			Name:        "limit",
			Type:        "integer",
			Required:    false,
			Description: "maximum number of rows to return; defaults to 100, maximum 1000 (a larger limit is rejected)",
		},
		{
			Name:        "offset",
			Type:        "integer",
			Required:    false,
			Description: "number of leading rows to skip before returning results; defaults to 0",
		},
	}
}

func newResult[T any]() any {
	return new(T)
}

func newSliceResult[T any]() any {
	result := []T{}

	return &result
}
