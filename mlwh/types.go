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

// IRODSPath identifies a product path exported from MLWH joins.
type IRODSPath struct {
	IDProduct  string `json:"id_product" doc:"product identifier of the iRODS data object"`
	Collection string `json:"collection" doc:"iRODS collection containing the data object"`
	DataObject string `json:"data_object" doc:"iRODS data object name"`
	IRODSPath  string `json:"irods_path" doc:"full iRODS path of the data object"`
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

// StudyDetail groups a study with its library details.
type StudyDetail struct {
	Study     Study           `json:"study" doc:"the study these details describe"`
	Libraries []LibraryDetail `json:"library_details" doc:"per-library detail for the study"`
}

// RunDetail groups a run with its related studies and samples.
type RunDetail struct {
	Run          Run           `json:"run" doc:"the run these details describe"`
	Samples      []Sample      `json:"samples" doc:"samples sequenced on the run"`
	Studies      []Study       `json:"studies" doc:"studies the run's samples belong to"`
	StudyDetails []StudyDetail `json:"study_details,omitempty" doc:"per-study detail for the run"`
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
