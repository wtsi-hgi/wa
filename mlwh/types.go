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

const (
	// HopClassify records an identifier-classification enrichment hop.
	HopClassify = "classify"
	// HopStudy records a study metadata enrichment hop.
	HopStudy = "study"
	// HopSamples records a sample expansion enrichment hop.
	HopSamples = "samples"
	// HopLibraries records a library expansion enrichment hop.
	HopLibraries = "libraries"
	// HopStudies records a study expansion enrichment hop.
	HopStudies = "studies"
)

const (
	// ReasonUpstreamError records an upstream cache or source failure.
	ReasonUpstreamError = "upstream_error"
	// ReasonNotFound records a missing optional enrichment hop.
	ReasonNotFound = "not_found"
	// ReasonSamplesTruncated records a bounded sample expansion.
	ReasonSamplesTruncated = "samples_truncated"
)

// Sample is the cache-backed sample shape mirrored from MLWH.
type Sample struct {
	IDSampleTmp     int64     `json:"id_sample_tmp" doc:"internal MLWH surrogate key for the sample"`
	IDLims          string    `json:"id_lims" doc:"LIMS system that owns the sample"`
	IDSampleLims    string    `json:"id_sample_lims" doc:"LIMS identifier of the sample"`
	UUIDSampleLims  string    `json:"uuid_sample_lims" doc:"LIMS UUID of the sample"`
	Name            string    `json:"name" doc:"sample name"`
	SangerSampleID  string    `json:"sanger_sample_id" doc:"Sanger sample identifier"`
	SupplierName    string    `json:"supplier_name" doc:"name the sample supplier gave the sample"`
	AccessionNumber string    `json:"accession_number" doc:"public archive accession number for the sample"`
	DonorID         string    `json:"donor_id" doc:"donor identifier the sample came from"`
	TaxonID         int       `json:"taxon_id" doc:"NCBI taxonomy id of the sample organism"`
	CommonName      string    `json:"common_name" doc:"common name of the sample organism"`
	Description     string    `json:"description" doc:"free-text sample description"`
	Studies         []Study   `json:"studies,omitempty" doc:"studies the sample belongs to, when expanded"`
	Libraries       []Library `json:"libraries,omitempty" doc:"libraries prepared from the sample, when expanded"`
}

// Study is the cache-backed study shape mirrored from MLWH.
type Study struct {
	IDStudyTmp               int64  `json:"id_study_tmp" doc:"internal MLWH surrogate key for the study"`
	IDLims                   string `json:"id_lims" doc:"LIMS system that owns the study"`
	IDStudyLims              string `json:"id_study_lims" doc:"LIMS identifier of the study"`
	UUIDStudyLims            string `json:"uuid_study_lims" doc:"LIMS UUID of the study"`
	Name                     string `json:"name" doc:"study name"`
	AccessionNumber          string `json:"accession_number" doc:"public archive accession number for the study"`
	StudyTitle               string `json:"study_title" doc:"full study title"`
	FacultySponsor           string `json:"faculty_sponsor" doc:"faculty sponsor of the study"`
	State                    string `json:"state" doc:"lifecycle state of the study"`
	DataReleaseStrategy      string `json:"data_release_strategy" doc:"data release strategy (e.g. open or managed)"`
	DataAccessGroup          string `json:"data_access_group" doc:"data access group governing access to the study data"`
	Programme                string `json:"programme" doc:"programme the study belongs to"`
	ReferenceGenome          string `json:"reference_genome" doc:"reference genome used for the study"`
	EthicallyApproved        bool   `json:"ethically_approved" doc:"whether the study has ethical approval"`
	StudyType                string `json:"study_type" doc:"type of study"`
	ContainsHumanDNA         bool   `json:"contains_human_dna" doc:"whether the study samples contain human DNA"`
	ContaminatedHumanDNA     bool   `json:"contaminated_human_dna" doc:"whether the study samples may be contaminated with human DNA"`
	StudyVisibility          string `json:"study_visibility" doc:"visibility of the study"`
	EGADACAccessionNumber    string `json:"ega_dac_accession_number" doc:"EGA Data Access Committee accession number"`
	EGAPolicyAccessionNumber string `json:"ega_policy_accession_number" doc:"EGA policy accession number"`
	DataReleaseTiming        string `json:"data_release_timing" doc:"timing of data release for the study"`
}

// Lane identifies a run/lane/tag combination linked to a sample.
type Lane struct {
	IDRun    int `json:"id_run" doc:"sequencing run identifier"`
	Position int `json:"lane" doc:"lane position on the run"`
	TagIndex int `json:"tag_index" doc:"index of the multiplexing tag within the lane"`
}

// IRODSPath identifies a product path exported from MLWH joins. IDSampleTmp and
// Name identify the sample the data object belongs to, so a study iRODS listing
// is aggregatable by sample without a second query.
type IRODSPath struct {
	IDProduct   string `json:"id_product" doc:"product identifier of the iRODS data object"`
	Collection  string `json:"collection" doc:"iRODS collection containing the data object"`
	DataObject  string `json:"data_object" doc:"iRODS data object name"`
	IRODSPath   string `json:"irods_path" doc:"full iRODS path of the data object"`
	IDSampleTmp int64  `json:"id_sample_tmp" doc:"internal MLWH surrogate key of the sample the data object belongs to"`
	Name        string `json:"name" doc:"Sanger sample name of the sample the data object belongs to"`
}

// SampleWithData is the enriched list row for the samples-with-data and
// samples-without-data partitions. It carries the platforms the sample has
// products on so every entry is platform-qualified rather than a bare "no data":
// empty for a registered-only sample (no products), ["ONT"] for an ONT sample
// (present in oseq_flowcell, with no products/iRODS/QC), and the canonical
// platform names (e.g. "Illumina", "PacBio", "Elembio", "Ultimagen") for a
// sample with product metrics. It is a NEW type, not the shared Sample struct.
type SampleWithData struct {
	Sample    Sample   `json:"sample" doc:"the sample"`
	Platforms []string `json:"platforms" doc:"platforms the sample has products on; empty for registered-only, [\"ONT\"] for ONT"`
}

// DateRange is an earliest/latest RFC3339 pair (empty strings when absent).
type DateRange struct {
	Earliest string `json:"earliest" doc:"earliest timestamp (UTC RFC3339)"`
	Latest   string `json:"latest" doc:"latest timestamp (UTC RFC3339)"`
}

// StudyOverview is the fixed-size study aggregate answering "what is in study X,
// how much sequencing data, and was anything added recently" in one response.
// Every figure is a single indexed aggregate over the cache mirrors;
// SamplesWithData / SamplesWithoutData / SamplesSequencedNoData form the
// distinct-sample partition (a sample counts in exactly one bucket by its
// most-advanced phase, precedence with_data > sequenced_no_data > registered, so
// registered = SamplesTotal - SamplesWithData - SamplesSequencedNoData).
type StudyOverview struct {
	IDStudyLims            string     `json:"id_study_lims" doc:"LIMS study id"`
	SamplesTotal           int        `json:"samples_total" doc:"distinct samples linked via library_samples"`
	SamplesWithData        int        `json:"samples_with_data" doc:"distinct samples with >=1 study-scoped iRODS row"`
	SamplesWithoutData     int        `json:"samples_without_data" doc:"samples_total minus samples_with_data"`
	SamplesSequencedNoData int        `json:"samples_sequenced_no_data" doc:"distinct samples with product-metrics in this study but no iRODS rows (distinct-sample partition)"`
	DataObjects            int        `json:"data_objects" doc:"study-scoped iRODS data objects"`
	Runs                   int        `json:"runs" doc:"distinct runs for the study"`
	Libraries              int        `json:"libraries" doc:"distinct libraries for the study"`
	LibraryTypes           []string   `json:"library_types" doc:"distinct library types present"`
	SequencingDateRange    *DateRange `json:"sequencing_date_range,omitempty" doc:"earliest/latest iRODS created for the study"`
	NewestDataAdded        string     `json:"newest_data_added" doc:"latest study-scoped iRODS created (UTC RFC3339), empty if none"`
	AddedLast7Days         int        `json:"added_last_7_days" doc:"distinct samples whose data was added in [now-7d, now)"`
	CacheSyncedAt          string     `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
}

// RunOverview is the fixed-size run aggregate answering "what is on this run and
// how much sequencing data" in one response, so callers need neither
// /run/:id/detail nor per-sample calls. id_run is the Illumina NPG run id (the
// existing Run/ResolveRun space); every figure is a single indexed aggregate over
// the cache mirrors. It is a separate small aggregate, NOT folded into RunDetail
// and NOT added to the bare Run struct.
type RunOverview struct {
	IDRun               int        `json:"id_run" doc:"Illumina NPG run id"`
	Samples             int        `json:"samples" doc:"distinct samples on the run"`
	Studies             int        `json:"studies" doc:"distinct studies on the run"`
	DataObjects         int        `json:"data_objects" doc:"iRODS data objects for the run"`
	SequencingDateRange *DateRange `json:"sequencing_date_range,omitempty" doc:"earliest/latest iRODS created for the run"`
	CacheSyncedAt       string     `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
}

// SampleDetail groups a sample with its enrichment graph neighbours.
type SampleDetail struct {
	Sample     Sample      `json:"sample" doc:"the sample these details describe"`
	Study      *Study      `json:"study,omitempty" doc:"primary study of the sample, when known"`
	Lanes      []Lane      `json:"lanes" doc:"lanes on which the sample was sequenced"`
	Libraries  []Library   `json:"libraries,omitempty" doc:"libraries prepared from the sample"`
	IRODSPaths []IRODSPath `json:"irods_paths,omitempty" doc:"iRODS data-object paths for the sample"`
}

// LibraryDetail groups a library with the samples it covers.
type LibraryDetail struct {
	Library Library  `json:"library,omitempty" doc:"the library these details describe"`
	Samples []Sample `json:"samples" doc:"samples covered by the library"`
}

// StudyDetail groups a study with its library details. To keep the response
// bounded it is de-duplicated: each distinct study and library is carried once
// in StudyLookup / LibraryLookup (keyed by id), and the nested sample rows under
// library_details reference those by id rather than re-embedding the same study
// and library objects under every sample. When the lean query param is set the
// heavy library_details / lookup tables are dropped and only the flat SampleIDs
// and LibraryIDs lists are returned, so the serialized response is strictly
// smaller.
type StudyDetail struct {
	Study         Study              `json:"study" doc:"the study these details describe"`
	Libraries     []LibraryDetail    `json:"library_details,omitempty" doc:"per-library detail for the study (omitted when lean); nested sample rows reference the study/library lookups by id rather than re-embedding them"`
	StudyLookup   map[string]Study   `json:"study_lookup,omitempty" doc:"distinct studies referenced by the nested rows, keyed by LIMS study id (omitted when lean)"`
	LibraryLookup map[string]Library `json:"library_lookup,omitempty" doc:"distinct libraries referenced by the nested rows, keyed by library id (omitted when lean)"`
	SampleIDs     []string           `json:"sample_ids,omitempty" doc:"flat list of the study's distinct sample ids (lean responses only)"`
	LibraryIDs    []string           `json:"library_ids,omitempty" doc:"flat list of the study's distinct library ids (lean responses only)"`
	Lean          bool               `json:"lean,omitempty" doc:"true when the heavy nested objects were dropped in favour of the flat id lists"`
}

// RunDetail groups a run with its related studies and samples. Like StudyDetail
// it is de-duplicated: each distinct study and library referenced by the run's
// per-study detail is carried once in StudyLookup / LibraryLookup (keyed by id)
// and the nested sample rows reference them by id rather than re-embedding them.
// When the lean query param is set the heavy samples / studies / study_details
// and the lookup tables are dropped and only the flat SampleIDs / StudyIDs lists
// are returned, so the serialized response is strictly smaller.
type RunDetail struct {
	Run           Run                `json:"run" doc:"the run these details describe"`
	Samples       []Sample           `json:"samples,omitempty" doc:"samples sequenced on the run (omitted when lean); each carries no studies/libraries sub-object, those being in the lookups"`
	Studies       []Study            `json:"studies,omitempty" doc:"studies the run's samples belong to (omitted when lean)"`
	StudyDetails  []StudyDetail      `json:"study_details,omitempty" doc:"per-study detail for the run (omitted when lean)"`
	StudyLookup   map[string]Study   `json:"study_lookup,omitempty" doc:"distinct studies referenced by the nested rows, keyed by LIMS study id (omitted when lean)"`
	LibraryLookup map[string]Library `json:"library_lookup,omitempty" doc:"distinct libraries referenced by the nested rows, keyed by library id (omitted when lean)"`
	SampleIDs     []string           `json:"sample_ids,omitempty" doc:"flat list of the run's distinct sample ids (lean responses only)"`
	StudyIDs      []string           `json:"study_ids,omitempty" doc:"flat list of the run's distinct study ids (lean responses only)"`
	Lean          bool               `json:"lean,omitempty" doc:"true when the heavy nested objects were dropped in favour of the flat id lists"`
}

// LibraryLink is a compact library tuple used by the enrichment graph contract.
type LibraryLink struct {
	LibraryType   string `json:"library_type" doc:"library type"`
	IDStudyLims   string `json:"id_study_lims" doc:"LIMS identifier of the study the library belongs to"`
	LibraryID     string `json:"library_id,omitempty" doc:"library identifier, when known"`
	IDLibraryLims string `json:"id_library_lims,omitempty" doc:"LIMS library identifier, when known"`
}

// EnrichmentGraph is the flat graph envelope returned under "graph".
type EnrichmentGraph struct {
	Study     *Study        `json:"study,omitempty" doc:"single related study, when the identifier resolves to one"`
	Studies   []Study       `json:"studies,omitempty" doc:"related studies, when the identifier expands to many"`
	Sample    *Sample       `json:"sample,omitempty" doc:"single related sample, when the identifier resolves to one"`
	Samples   []Sample      `json:"samples,omitempty" doc:"related samples, when the identifier expands to many"`
	Library   *LibraryLink  `json:"library,omitempty" doc:"single related library, when the identifier resolves to one"`
	Libraries []LibraryLink `json:"libraries,omitempty" doc:"related libraries, when the identifier expands to many"`

	StudyDetail  *StudyDetail  `json:"study_detail,omitempty" doc:"detailed view of the single related study, when present"`
	StudyDetails []StudyDetail `json:"study_details,omitempty" doc:"detailed views of the related studies, when present"`
	SampleDetail *SampleDetail `json:"sample_detail,omitempty" doc:"detailed view of the single related sample, when present"`
}

// MissingHop records a hop that failed or was truncated.
type MissingHop struct {
	Hop    string `json:"hop" doc:"name of the enrichment hop that failed or was truncated"`
	Reason string `json:"reason" doc:"reason the hop is missing (e.g. upstream_error, not_found, samples_truncated)"`
	Status int    `json:"status" doc:"HTTP-style status associated with the missing hop"`
}

// EnrichmentResult is the enrichment response body.
type EnrichmentResult struct {
	Identifier string          `json:"identifier" doc:"canonical form of the enriched identifier"`
	Type       IdentifierKind  `json:"type" doc:"kind of the enriched identifier"`
	Graph      EnrichmentGraph `json:"graph" doc:"graph of related studies, samples, and libraries"`
	Partial    bool            `json:"partial" doc:"true when one or more hops were missing or truncated"`
	Missing    []MissingHop    `json:"missing,omitempty" doc:"hops that failed or were truncated, when partial"`
}

// Page is the typed paged variant exposing the list-sizing metadata that the
// paginated list endpoints additionally report via the X-Total-Count and
// X-Next-Offset response headers (the bodies stay bare JSON arrays). Items is
// the page of rows (identical to the bare-slice method's result for the same
// args), Total is the total number of matching rows (the X-Total-Count value,
// equal to the corresponding /count endpoint), and NextOffset is the offset of
// the next page (the X-Next-Offset value: offset+len(Items) when more rows
// remain, else -1 for the last page).
type Page[T any] struct {
	Items      []T
	Total      int
	NextOffset int
}

// TaggedID identifies one canonical identifier dimension for results expansion.
type TaggedID struct {
	Kind      IdentifierKind `json:"kind" doc:"kind of the identifier"`
	Canonical string         `json:"canonical" doc:"canonical form of the identifier"`
}

// SearchValues groups expanded values used for results searches.
type SearchValues struct {
	Samples []string `json:"samples" doc:"sample values to search downstream results by"`
	Runs    []string `json:"runs" doc:"run values to search downstream results by"`
	Lanes   []string `json:"lanes" doc:"lane values to search downstream results by"`
}
