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
// is aggregatable by sample without a second query. IDRun is the Illumina NPG run
// id, derived by LEFT JOIN id_iseq_product -> iseq_product_metrics_mirror.id_run;
// it is 0 when not derivable (non-Illumina / unmatched), matching the existing
// RunOverview.IDRun / RunStatusTimeline.IDRun "0 for non-Illumina" convention.
// Platform is the iRODS row's mirrored platform string (the source
// seq_platform_name, e.g. "illumina"), so a 0 id_run reads as ONT / non-Illumina
// rather than ambiguous. Both fields are additive; existing fields unchanged.
type IRODSPath struct {
	IDProduct   string `json:"id_product" doc:"product identifier of the iRODS data object"`
	Collection  string `json:"collection" doc:"iRODS collection containing the data object"`
	DataObject  string `json:"data_object" doc:"iRODS data object name"`
	IRODSPath   string `json:"irods_path" doc:"full iRODS path of the data object"`
	IDSampleTmp int64  `json:"id_sample_tmp" doc:"internal MLWH surrogate key of the sample the data object belongs to"`
	Name        string `json:"name" doc:"Sanger sample name of the sample the data object belongs to; empty when the sample is not present in the sample mirror"`
	IDRun       int    `json:"id_run" doc:"Illumina NPG run id of the data object; 0 when not derivable (non-Illumina or unmatched)"`
	Platform    string `json:"platform" doc:"platform string the iRODS row was synced with (source seq_platform_name); disambiguates a 0 id_run as ONT/non-Illumina"`
}

// ManifestRow is one row of a study's data manifest: one sequencing product
// (run x position x tag) joined to its sample's identity, plus the study-level
// metadata carried once in the envelope (not per row). When the file-type / iRODS
// path is requested, IRODSPath is the data object for that product matching the
// suffix filter (empty string when the product has no matching iRODS object).
type ManifestRow struct {
	Name            string `json:"name" doc:"Sanger sample name"`
	SupplierName    string `json:"supplier_name" doc:"supplier-given sample name"`
	AccessionNumber string `json:"accession_number" doc:"sample public archive accession number"`
	SangerSampleID  string `json:"sanger_sample_id" doc:"Sanger sample id"`
	IDRun           int    `json:"id_run" doc:"Illumina NPG run id of the product"`
	Position        int    `json:"lane" doc:"lane position of the product"`
	TagIndex        int    `json:"tag_index" doc:"multiplexing tag index of the product"`
	IRODSPath       string `json:"irods_path,omitempty" doc:"iRODS path of the product's data object matching the file-type filter; present only when with_irods is set"`
}

// StudyManifest is the manifest envelope: the study-level metadata once, plus the
// page of product rows. The page is bounded/pageable; the study fields answer Q3's
// "study details" without repeating per row (D2/D5).
type StudyManifest struct {
	IDStudyLims     string        `json:"id_study_lims" doc:"LIMS study id"`
	Name            string        `json:"name" doc:"study name"`
	AccessionNumber string        `json:"accession_number" doc:"study accession number"`
	FacultySponsor  string        `json:"faculty_sponsor" doc:"study faculty sponsor"`
	DataAccessGroup string        `json:"data_access_group" doc:"study data access group"`
	Rows            []ManifestRow `json:"rows" doc:"page of per-product manifest rows"`
	CacheSyncedAt   string        `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
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
// Every figure is a single indexed aggregate over the cache mirrors. The
// mutually-exclusive distinct-sample partition is with_data / sequenced_no_data /
// registered: a sample counts in exactly one bucket by its most-advanced phase,
// precedence with_data > sequenced_no_data > registered, so registered =
// SamplesTotal - SamplesWithData - SamplesSequencedNoData. SamplesWithoutData is
// the derived superset of the two non-with-data buckets (= sequenced_no_data +
// registered = SamplesTotal - SamplesWithData); it overlaps
// SamplesSequencedNoData and so is not part of that partition.
type StudyOverview struct {
	IDStudyLims            string     `json:"id_study_lims" doc:"LIMS study id"`
	Name                   string     `json:"name" doc:"study name"`
	AccessionNumber        string     `json:"accession_number" doc:"study accession number"`
	FacultySponsor         string     `json:"faculty_sponsor" doc:"study faculty sponsor"`
	DataAccessGroup        string     `json:"data_access_group" doc:"study data access group governing data access"`
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

// PhaseLadder is the mutually-exclusive baseline-phase ladder (spec F4, closed
// enum) whose buckets sum per partition: with_data (samples with >=1 study-scoped
// iRODS row), sequenced_no_data (samples with product-metrics in this study but
// no iRODS rows), and registered (linked samples with no product-metrics,
// including ONT).
type PhaseLadder struct {
	WithData        int `json:"with_data" doc:"samples with >=1 study-scoped iRODS row"`
	SequencedNoData int `json:"sequenced_no_data" doc:"samples sequenced in this study but no iRODS rows"`
	Registered      int `json:"registered" doc:"linked samples with no product-metrics (incl. ONT)"`
}

// PlatformPhaseLadder is one platform's slice of the per-platform partition: the
// canonical platform name and the PhaseLadder whose buckets sum to that
// platform's distinct sample count in the study.
type PlatformPhaseLadder struct {
	Platform string      `json:"platform" doc:"platform name"`
	Ladder   PhaseLadder `json:"ladder" doc:"buckets summing to this platform's sample count"`
}

// StudyQCBreakdown is the QC split of a study's SEQUENCED (distinct) samples
// (D3): qc_pass/qc_fail/qc_pending partition the sequenced samples using the same
// per-sample roll-up progress.go applies (fail > pending > pass), so the three
// sum to "sequenced" (= samples_total - the registered bucket). not_tracked
// samples (no products, incl. ONT) are NOT sequenced and are excluded here.
type StudyQCBreakdown struct {
	QCPass    int `json:"qc_pass" doc:"distinct sequenced samples whose roll-up QC is pass"`
	QCFail    int `json:"qc_fail" doc:"distinct sequenced samples whose roll-up QC is fail"`
	QCPending int `json:"qc_pending" doc:"distinct sequenced samples whose roll-up QC is pending"`
}

// StatusBreakdown is the per-baseline-phase study rollup (spec F4, layer P3),
// answering "how many of my samples are at each phase" without a per-sample
// fan-out. It carries TWO denominators. Distinct is the distinct-sample
// partition over the study's library_samples-linked samples: a sample lands in
// exactly one ladder bucket by its most-advanced phase (precedence with_data >
// sequenced_no_data > registered), so the three buckets sum to samples_total.
// PerPlatform is the per-platform partition: a sample's true state shows under
// EACH of its platforms, so within a platform the buckets sum to that platform's
// sample count but the grand total across platforms may EXCEED samples_total (a
// multi-platform sample is counted under every platform it spans). Samples with
// no product-metrics, including ONT, are registered (never folded into a separate
// without-data negative). QC is the QC split of the SEQUENCED distinct samples
// (qc_pass/qc_fail/qc_pending), summing to samples_total - distinct.registered.
// WithDetailedTimeline is the count of the study's samples also present in the
// seq_ops_tracking_per_sample mirror. CacheSyncedAt is the oldest last_run across
// the feeding tables, distinct from any data timestamp (the freshness caveat).
type StatusBreakdown struct {
	IDStudyLims          string                `json:"id_study_lims" doc:"LIMS study id"`
	Distinct             PhaseLadder           `json:"distinct" doc:"distinct-sample partition, sums to samples_total"`
	PerPlatform          []PlatformPhaseLadder `json:"per_platform" doc:"per-platform partition; grand total may exceed samples_total"`
	QC                   StudyQCBreakdown      `json:"qc" doc:"QC split of the sequenced (distinct) samples; sums to samples_total - registered"`
	WithDetailedTimeline int                   `json:"with_detailed_timeline" doc:"samples also present in the tracking mirror"`
	CacheSyncedAt        string                `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
}

// RunStatusEvent is one within-sequencing status transition (spec F2). It uses
// entered_at (a run-lifecycle phase is ENTERED), deliberately distinct from a
// milestone's reached_at though the value semantics are identical. Phase is the
// native status description passed through verbatim (open vocabulary). Duration
// is the ISO8601-style delta to the NEXT event and is empty for the current/open
// (last) event.
type RunStatusEvent struct {
	Phase     string `json:"phase" doc:"native status description (open vocabulary)"`
	EnteredAt string `json:"entered_at" doc:"when the phase was entered (UTC RFC3339)"`
	Duration  string `json:"duration,omitempty" doc:"duration to the next event; empty for the current/open phase"`
}

// RunStatusTimeline is one run's normalized within-sequencing status timeline
// (spec F2/P5). It is the SINGLE shared type returned by GET /run/:id/status AND
// embedded per run in SampleProgress (spec F3), so the two surfaces never drift:
// both build it via the same normalizer. Events are ordered by entered_at and
// preserved faithfully (recurrences, on-hold, cancelled and stopped-early are
// kept, not deduplicated, reordered or forced monotonic); Current is DERIVED from
// the latest entered_at (never the source iscurrent). The phase vocabulary is an
// OPEN dict/source pass-through (NOT a frozen list): an unknown native status
// flows through verbatim. ONT has no within-sequencing status: an ONT-only sample
// has no within-sequencing runs, so SampleProgress (F3) emits no RunStatusTimeline
// for it (its runs is empty) rather than a false zero (HARD REQ 11), and RunStatus
// (F2) serves only Illumina runs.
type RunStatusTimeline struct {
	IDRun      int              `json:"id_run" doc:"Illumina NPG run id (0/empty for non-Illumina)"`
	Platform   string           `json:"platform" doc:"platform of the run"`
	Events     []RunStatusEvent `json:"events" doc:"ordered status events; empty for ONT"`
	Current    string           `json:"current" doc:"phase of the event with the latest entered_at (derived, not source iscurrent)"`
	NotTracked string           `json:"not_tracked,omitempty" doc:"reserved reason for a platform that reports an explicitly-untracked within-sequencing status; currently always empty/omitted (no supported platform sets it)"`
}

// Milestone is one wet-lab/sequencing milestone on the sample-progress timeline
// (spec F3). Name is one of the 9 canonical milestone names (a closed enum,
// reported verbatim). It uses reached_at -- a milestone is REACHED -- deliberately
// distinct in name from a run lifecycle event's entered_at though the value
// semantics are identical (RFC3339 + ISO8601 duration-to-next + open-phase
// handling). DurationToNext is the ISO8601-style span to the NEXT reached
// milestone and is empty for the open/current milestone (the latest reached one,
// whose successor is NULL).
type Milestone struct {
	Name           string `json:"name" doc:"one of the 9 milestone names"`
	ReachedAt      string `json:"reached_at" doc:"when the milestone was reached (UTC RFC3339)"`
	DurationToNext string `json:"duration_to_next,omitempty" doc:"ISO8601-style duration to the next reached milestone; empty for the open current phase"`
}

// SampleProgress is the unified sample-progress response (spec F3, layers
// P2/P4/P6). It is resolved by Sanger sample name and ALWAYS carries the
// always-derivable P0 baseline (Sample / Platforms / BaselinePhase / QC /
// DeliveredAt; see sampleBaseline), so it resolves for every sample on every
// platform with no "works for this sample, not that one" cliff. When the sample
// is present in seq_ops_tracking_per_sample_mirror it additionally carries the
// ordered Milestones (each reached_at + duration_to_next) and CurrentMilestone
// (the latest reached milestone whose successor is NULL) with
// DetailedTimeline=true; otherwise DetailedTimeline is false with a non-empty
// TimelineReason -- less detail, NEVER an error. Runs embeds one shared
// RunStatusTimeline per run of the sample (Illumina via the same builder as GET
// /run/:id/status so the embedded and standalone forms never drift; PacBio from
// its own well-metrics status/dates). ONT has no within-sequencing runs, so Runs
// is empty and QC is not_tracked from the baseline (HARD REQ 11), never a false
// zero. The overall QC is authoritative (the per-sample roll-up fail > pending >
// pass; not_tracked when the sample has no products). CacheSyncedAt is the oldest
// last_run across the feeding tables (tracking + iseq_run_status + product-metrics
// + iRODS), distinct from any data timestamp. The open/current milestone returns
// its reached_at timestamp for the caller to compute elapsed (no server "now"
// subtraction).
type SampleProgress struct {
	Sample           Sample              `json:"sample" doc:"the sample"`
	Platforms        []string            `json:"platforms" doc:"detected platforms; [\"ONT\"] for ONT, empty when registered only"`
	BaselinePhase    string              `json:"baseline_phase" doc:"registered|sequenced|delivered (most-advanced across platforms)"`
	QC               string              `json:"qc" doc:"pass|fail|pending|not_tracked (overall verdict, rolled up)"`
	DeliveredAt      string              `json:"delivered_at" doc:"earliest iRODS created (UTC RFC3339), empty if none"`
	DetailedTimeline bool                `json:"detailed_timeline" doc:"true when the sample is in the tracking mirror"`
	TimelineReason   string              `json:"timeline_reason,omitempty" doc:"why detailed_timeline is false (e.g. not in tracking window)"`
	Milestones       []Milestone         `json:"milestones,omitempty" doc:"ordered milestone timeline when detailed_timeline"`
	CurrentMilestone string              `json:"current_milestone,omitempty" doc:"latest reached milestone whose successor is NULL"`
	Runs             []RunStatusTimeline `json:"runs,omitempty" doc:"per-run within-sequencing status timeline"`
	CacheSyncedAt    string              `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
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
	Items      []T `json:"items" doc:"the page of rows"`
	Total      int `json:"total" doc:"total number of matching rows"`
	NextOffset int `json:"next_offset" doc:"offset of the next page, or -1 on the last page"`
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
