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

// Sample is the cache-backed sample shape mirrored from MLWH.
type Sample struct {
	IDSampleTmp     int64  `json:"id_sample_tmp"`
	IDLims          string `json:"id_lims"`
	IDSampleLims    string `json:"id_sample_lims"`
	UUIDSampleLims  string `json:"uuid_sample_lims"`
	IDStudyLims     string `json:"id_study_lims"`
	Name            string `json:"name"`
	SangerID        string `json:"sanger_id"`
	SangerSampleID  string `json:"sanger_sample_id"`
	SupplierName    string `json:"supplier_name"`
	AccessionNumber string `json:"accession_number"`
	DonorID         string `json:"donor_id"`
	LibraryType     string `json:"library_type"`
	TaxonID         int    `json:"taxon_id"`
	CommonName      string `json:"common_name"`
	Description     string `json:"description"`
}

// Study is the cache-backed study shape mirrored from MLWH.
type Study struct {
	IDStudyTmp               int64  `json:"id_study_tmp"`
	IDLims                   string `json:"id_lims"`
	IDStudyLims              string `json:"id_study_lims"`
	UUIDStudyLims            string `json:"uuid_study_lims"`
	Name                     string `json:"name"`
	AccessionNumber          string `json:"accession_number"`
	StudyTitle               string `json:"study_title"`
	FacultySponsor           string `json:"faculty_sponsor"`
	State                    string `json:"state"`
	Abstract                 string `json:"abstract"`
	Abbreviation             string `json:"abbreviation"`
	Description              string `json:"description"`
	DataReleaseStrategy      string `json:"data_release_strategy"`
	DataAccessGroup          string `json:"data_access_group"`
	HMDMCNumber              string `json:"hmdmc_number"`
	Programme                string `json:"programme"`
	Created                  string `json:"created"`
	ReferenceGenome          string `json:"reference_genome"`
	EthicallyApproved        bool   `json:"ethically_approved"`
	StudyType                string `json:"study_type"`
	ContainsHumanDNA         bool   `json:"contains_human_dna"`
	ContaminatedHumanDNA     bool   `json:"contaminated_human_dna"`
	StudyVisibility          string `json:"study_visibility"`
	EGADACAccessionNumber    string `json:"ega_dac_accession_number"`
	EGAPolicyAccessionNumber string `json:"ega_policy_accession_number"`
	DataReleaseTiming        string `json:"data_release_timing"`
}

// Lane identifies a run/lane/tag combination linked to a sample.
type Lane struct {
	IDRun    int `json:"id_run"`
	Position int `json:"lane"`
	TagIndex int `json:"tag_index"`
}

// IRODSPath identifies a product path exported from MLWH joins.
type IRODSPath struct {
	IDProduct  string `json:"id_product"`
	Collection string `json:"collection"`
	DataObject string `json:"data_object"`
	IRODSPath  string `json:"irods_path"`
}

// SampleDetail groups a sample with its enrichment graph neighbours.
type SampleDetail struct {
	Sample     Sample      `json:"sample"`
	Study      *Study      `json:"study,omitempty"`
	Lanes      []Lane      `json:"lanes"`
	Libraries  []Library   `json:"libraries,omitempty"`
	IRODSPaths []IRODSPath `json:"irods_paths,omitempty"`
}

// LibraryDetail groups a library with the samples it covers.
type LibraryDetail struct {
	Library Library  `json:"library,omitempty"`
	Samples []Sample `json:"samples"`
}

// StudyDetail groups a study with its library details.
type StudyDetail struct {
	Study     Study           `json:"study"`
	Libraries []LibraryDetail `json:"library_details"`
}

// RunDetail groups a run with its related studies and samples.
type RunDetail struct {
	Run          Run           `json:"run"`
	Samples      []Sample      `json:"samples"`
	Studies      []Study       `json:"studies"`
	StudyDetails []StudyDetail `json:"study_details,omitempty"`
}

// TaggedID identifies one canonical identifier dimension for results expansion.
type TaggedID struct {
	Kind      IdentifierKind
	Canonical string
}
