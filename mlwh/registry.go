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

import "strings"

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

// QueryParam is a structured specification of one query-string parameter,
// consumed by the OpenAPI generator and the human reference to describe the
// limit/offset pagination controls (and any future filters).
type QueryParam struct {
	Name        string // e.g. "limit"
	Type        string // OpenAPI type, e.g. "integer"
	Required    bool
	Description string
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
		Method:      "RunsForSample",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/runs",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Run],
		Summary:     "List runs for a sample",
		Description: "Lists the distinct sequencing runs associated with the given sample. Defaults to returning all runs; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "MonthlyRunCounts",
		Verb:        registryVerbGet,
		Path:        "/runs/monthly",
		Query:       []string{"since", "until", "platform"},
		NewResult:   newSliceResult[MonthlyRunCount],
		Summary:     "List monthly grouped run counts",
		Description: "Returns monthly grouped run counts across platforms. Manufacturer is derived from platform as Illumina->Illumina, Elembio->Element Biosciences, Ultimagen->Ultima Genomics, PacBio->PacBio and ONT->Oxford Nanopore. Run grain is one run identifier, never wells or flowcells: Illumina, Elembio and Ultimagen count distinct id_run, PacBio counts distinct pac_bio_run_name, and ONT counts distinct experiment_name. Date basis is authoritative and each row states date_basis: Illumina and Elembio use run complete, Ultimagen uses run archived, PacBio uses run_complete, and ONT uses warehouse load time - not a true sequencing date. ONT is included under that labelled warehouse-load basis, never dropped. Optional since and until filter the normalised per-platform run date with since inclusive and until exclusive; platform may be supplied more than once to restrict platforms. Each row carries cache_synced_at; values are read from the cache mirrors and complete only up to their sync state (see /freshness).",
		QueryParams: monthlyRunQueryParams(),
	},
	{
		Method:      "RunListing",
		Verb:        registryVerbGet,
		Path:        "/runs",
		Query:       []string{"since", "until", "platform", "limit", "cursor"},
		NewResult:   newSliceResult[RunListingRow],
		Summary:     "List global runs",
		Description: "Returns a bounded global all-runs listing across platforms, one row per platform-native run identifier. The stable id is the composite <platform>:<native_id> (for example illumina:47409, pacbio:<run_name>, ont:<experiment_name>) and is the keyset cursor. Native_id is also returned separately. Manufacturer is derived from platform as Illumina->Illumina, Elembio->Element Biosciences, Ultimagen->Ultima Genomics, PacBio->PacBio and ONT->Oxford Nanopore. Run grain and date_basis match /runs/monthly: Illumina, Elembio and Ultimagen list distinct id_run, PacBio lists distinct pac_bio_run_name, and ONT lists distinct experiment_name; Illumina and Elembio use run complete, Ultimagen uses run archived, PacBio uses run_complete, and ONT uses warehouse load time - not a true sequencing date. Optional since and until filter the normalised per-platform run date with since inclusive and until exclusive; platform may be supplied more than once to restrict platforms. Use limit for a bounded page and cursor=<last id> to continue. Each row carries cache_synced_at; values are read from the cache mirrors and complete only up to their sync state (see /freshness).",
		QueryParams: runListingQueryParams(),
	},
	{
		Method:      "SequencingAggregate",
		Verb:        registryVerbGet,
		Path:        "/sequencing/aggregate",
		Query:       []string{"group_by", "unit", "since", "until", "platform"},
		NewResult:   newSliceResult[SequencingAggregateRow],
		Summary:     "List grouped sequencing aggregate",
		Description: "Returns a grouped sequencing aggregate in one call. group_by is required and may be supplied more than once or comma-separated, combining month, platform, manufacturer, programme and faculty_sponsor. unit is required: runs counts platform-native run identifiers using the per-platform date basis from /runs/monthly (Illumina and Elembio run complete, Ultimagen run archived, PacBio run_complete, ONT warehouse load time - not a true sequencing date); a run spanning multiple requested study groups counts once in each group it touches, with no server-side fan-out over studies. samples and products are data-grain aggregates over seq_product_irods_locations_mirror, windowed by iRODS created with since inclusive and until exclusive; each product is attributed through its single study-scoped iRODS row to exactly one study programme/faculty sponsor (one study programme attribution unit), and rows state date_basis=iRODS created. platform is optional and repeatable. Each row carries cache_synced_at; values are read from the cache mirrors and complete only up to their sync state (see /freshness).",
		QueryParams: sequencingAggregateQueryParams(),
	},
	{
		Method:      "StudyOverview",
		Verb:        registryVerbGet,
		Path:        "/study/:id/overview",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[StudyOverview],
		Summary:     "Get a study's sequencing overview",
		Description: "Returns one fixed-size aggregate answering \"what is in this study, how much sequencing data, and was anything added recently\", so callers avoid the large per-sample fan-outs. The overview also carries the study's own metadata read from study_mirror -- name, accession_number, faculty_sponsor and data_access_group (the group governing data access) -- so \"the data access group for study X\" (and its name / accession / sponsor) is this one small call rather than the much larger /study/:id/detail; these four fields are populated whenever the study exists, including a synced study with zero linked samples (counts 0, fields still set). samples_total is the distinct samples linked via library_samples. A sample has sequencing data available for this study iff it has at least one row in the iRODS locations mirror scoped by id_study_lims = the study (real data objects in iRODS); scoping is by the study the data is under, NOT data the sample has anywhere. samples_with_data, samples_sequenced_no_data and the implied registered bucket form the distinct-sample partition by most-advanced phase (precedence with_data > sequenced_no_data > registered), so each sample counts once and samples_without_data = samples_total - samples_with_data. samples_sequenced_no_data is the distinct samples with product-metrics in this study (scoped by the product-metrics id_study_lims) but no study-scoped iRODS rows; registered (= samples_total - samples_with_data - samples_sequenced_no_data) is the linked samples with no product-metrics, including ONT. data_objects is the study-scoped iRODS data-object count; runs, libraries and the sorted library_types come from the study's product-metrics and library tables. sequencing_date_range and newest_data_added are the earliest/latest iRODS creation timestamp (the created column, NEVER last_updated or last_run); added_last_7_days counts the distinct samples whose study-scoped iRODS data was added in the half-open window [now-7d, now) on that created column (created >= now-7d AND created < now). cache_synced_at is the oldest last_run across the feeding tables (study, sample, the product-metrics mirrors and the iRODS locations mirror), distinct from any data timestamp; every figure is read from the cache mirrors, so the overview is complete only up to that sync (see /freshness).",
	},
	{
		Method:      "RunOverview",
		Verb:        registryVerbGet,
		Path:        "/run/:id/overview",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[RunOverview],
		Summary:     "Get a run's sequencing overview",
		Description: "Returns one fixed-size aggregate answering \"what is on this run and how much sequencing data\", so callers need neither /run/:id/detail nor per-sample calls. :id is the Illumina NPG run id (the existing run/ResolveRun identifier space; no new resolver): a non-Illumina or otherwise invalid run yields the existing not-found / unsupported-identifier error, and a numeric run absent from the synced cache yields not_found. samples is the distinct samples on the run and studies the distinct studies on the run, both taken from the run's iseq_product_metrics rows (the same source as /run/:id/samples). data_objects is the iRODS data objects for the run: the run's iseq_product_metrics rows joined to the iRODS locations mirror by the shared id_iseq_product (the run's real data files in iRODS). sequencing_date_range is the earliest/latest iRODS creation timestamp for those data objects (the created column, NEVER last_updated or last_run); it is omitted when the run has no iRODS rows. This is a separate small aggregate, NOT folded into /run/:id/detail. cache_synced_at is the oldest last_run across the feeding tables (the iseq product-metrics mirror and the iRODS locations mirror), distinct from any data timestamp; every figure is read from the cache mirrors, so the overview is complete only up to that sync (see /freshness).",
	},
	{
		Method:      "RunStatus",
		Verb:        registryVerbGet,
		Path:        "/run/:id/status",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[RunStatusTimeline],
		Summary:     "Get a run's within-sequencing status timeline",
		Description: "Returns one run's within-sequencing status as a single normalized timeline of {phase, entered_at, duration} events, answering \"where is this run in the NPG sequencing lifecycle\". :id is the Illumina NPG run id (the existing run/ResolveRun identifier space; no new resolver): a non-Illumina or otherwise invalid run yields the existing not-found / unsupported-identifier error, and a numeric run absent from the synced cache yields not_found. Cross-platform sequencing status is reached via /sample/:id/progress, where the platform is known. The Illumina timeline comes from iseq_run_status joined to iseq_run_status_dict for each row's description; events are ordered by date with entered_at = that date (UTC RFC3339). The status vocabulary is an OPEN dict/source pass-through, NOT a frozen list: phase is the native iseq_run_status_dict description (or, for other platforms, their native run_status/well_status) reported verbatim, so a new or unknown status flows through unchanged rather than being rejected or normalized. Each event's duration is the ISO8601-style span to the NEXT event and is empty for the current/open (last) event; the same duration format is used by the milestone duration_to_next on /sample/:id/progress. current is DERIVED as the phase of the event with the latest date; it is NEVER read from the source iscurrent flag (a run whose iscurrent=1 sits on an earlier-dated row still reports the latest-dated phase as current). The timeline is faithful: recurrences, on-hold, cancelled and stopped-early statuses are preserved in date order and are NOT deduplicated, reordered or forced monotonic. entered_at is the run-lifecycle phase-ENTRY timestamp, deliberately distinct in name from the milestone reached_at on /sample/:id/progress (a lifecycle phase is entered; a milestone is reached) though the value semantics are identical. ONT has no within-sequencing status and no Illumina NPG run id, so it is not served here (an ONT or otherwise non-Illumina run yields the not-found / unsupported-identifier error noted above); ONT and cross-platform status is reached via /sample/:id/progress. This is the same RunStatusTimeline embedded per run in /sample/:id/progress, so the standalone and embedded forms never drift. Every value is read from the cache mirrors, so the timeline is complete only up to the iseq_run_status sync (see /freshness).",
	},
	{
		Method:      "SampleProgress",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/progress",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[SampleProgress],
		Summary:     "Get a sample's unified pipeline progress",
		Description: "Returns one response answering \"what is happening with this sample\": the always-available baseline, the milestone timeline (when the sample is tracked) and the per-run within-sequencing status, so callers need one call rather than several. :id is the Sanger sample name. It ALWAYS returns the P0 baseline, which resolves for every sample on every platform: baseline_phase is one of exactly registered (linked via library_samples, no product-metrics on any platform, including ONT), sequenced (has product-metrics, no iRODS rows yet) or delivered (has at least one study-scoped iRODS row), reported as the most-advanced phase across the sample's platforms; platforms lists the detected platforms (e.g. Illumina, PacBio, Elembio, Ultimagen), is [\"ONT\"] for an Oxford Nanopore sample and empty for a registered-only sample; delivered_at is the earliest iRODS creation timestamp (UTC RFC3339), empty when not delivered. qc is the overall, AUTHORITATIVE per-sample verdict, rolled up across the sample's products from each product's overall qc value (1 -> pass, 0 -> fail, NULL -> pending) by the rule fail > pending > pass (any product fails -> fail, else any pending -> pending, else pass); it is not_tracked when the sample has no products (including ONT), never a false zero. When the sample is present in the seq_ops_tracking_per_sample mirror, detailed_timeline is true and milestones lists the REACHED milestones in this exact canonical order -- manifest_created, manifest_uploaded, labware_received, order_made, working_dilution, library_start, library_complete, sequencing_run_start, sequencing_qc_complete (a closed 9-name set) -- each with its reached_at (UTC RFC3339) and duration_to_next (an ISO8601-style span to the next reached milestone, empty for the open/current one); current_milestone is the latest reached milestone whose successor is NULL. The open/current milestone returns its reached_at timestamp for the caller to compute elapsed time; the server does NOT subtract \"now\". When the sample is absent from the tracking mirror, detailed_timeline is false with a non-empty timeline_reason and no milestones -- this is LESS detail, NEVER an error (an ONT or untracked sample still returns its baseline). reached_at (a milestone is REACHED) is deliberately distinct in name from a run event's entered_at (a run lifecycle phase is ENTERED) though their value semantics are identical. runs embeds one RunStatusTimeline per run of the sample: Illumina runs are the SAME timeline returned by /run/:id/status (identical events and derived current, so the embedded and standalone forms never drift) and PacBio, Elembio and Ultimagen runs are built from their own run/well/lane status and dates through the same normalization; ONT has no within-sequencing runs, so runs is empty. cache_synced_at is the oldest last_run across the feeding tables (the tracking mirror, iseq_run_status, the product-metrics mirrors and the iRODS locations mirror), distinct from any data timestamp; every value is read from the cache mirrors, so the progress is complete only up to that sync (see /freshness). An unknown sample name on a synced cache yields not_found; a never-synced cache yields not_found together with a cache-never-synced signal.",
	},
	{
		Method:      "StatusBreakdown",
		Verb:        registryVerbGet,
		Path:        "/study/:id/status-breakdown",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[StatusBreakdown],
		Summary:     "Get a study's status breakdown by baseline phase",
		Description: "Returns one fixed-size rollup of a study's samples by baseline phase, answering \"how many of my samples are at each phase\" without a per-sample fan-out. :id is the LIMS study id. The ladder is the closed enum with_data > sequenced_no_data > registered: with_data is samples with at least one study-scoped iRODS row (real data objects in iRODS, scoped by id_study_lims = the study), sequenced_no_data is samples with product-metrics in this study but no study-scoped iRODS rows, and registered is the linked samples with no product-metrics at all. Samples with no product-metrics, INCLUDING ONT (Oxford Nanopore, which has no product-metrics/iRODS/QC), are counted in registered -- never folded into a separate without-data negative. The response carries TWO denominators. distinct is the distinct-sample partition over the study's library_samples-linked samples: each sample counts ONCE, in the single bucket of its most-advanced phase (precedence with_data > sequenced_no_data > registered), so the three distinct buckets SUM TO samples_total. per_platform is the per-platform partition: a sample's true state shows under EACH platform it spans (canonical platform names, e.g. Illumina, PacBio, Elembio, Ultimagen, and ONT), so within a platform the buckets sum to that platform's sample count but the GRAND TOTAL across platforms MAY EXCEED samples_total (a multi-platform sample is counted under every one of its platforms, e.g. with_data under Illumina and sequenced_no_data under PacBio, while in distinct it is counted once under with_data). with_detailed_timeline is the count of the study's samples also present in the seq_ops_tracking_per_sample mirror. Each partition is computed by one small grouped query, never a per-sample fan-out. In these terms, RECEIVED is samples_total (every linked sample), SEQUENCED is samples_total - registered (the distinct samples with product-metrics in this study, any platform), and NOT-SEQUENCED is registered (linked samples with no product-metrics, INCLUDING ONT). qc is the QC split of the SEQUENCED distinct samples into qc_pass, qc_fail and qc_pending using the SAME per-sample roll-up as /sample/:id/progress: each sample's products IN THIS STUDY are rolled up over iseq_product_metrics.qc (1 -> pass, 0 -> fail, NULL -> pending) by the rule fail > pending > pass, so a single-study sample's study verdict cannot disagree with its SampleProgress qc. The three qc counts SUM TO sequenced (= samples_total - registered); not_tracked and ONT samples have no product-metrics, are NOT sequenced and are EXCLUDED from the qc split (never a false qc_pending). cache_synced_at is the oldest last_run across the feeding tables (study, sample, the product-metrics mirrors, the iRODS locations mirror and the tracking mirror), distinct from any data timestamp; every figure (including the qc split) is read from the cache mirrors, so the breakdown is complete only up to that sync (see /freshness). An unknown study yields not_found; a never-synced cache yields not_found together with a cache-never-synced signal; a synced study with no samples yields all-zero ladders and qc {0,0,0}.",
	},
	{
		Method:      "SamplesWithData",
		Verb:        registryVerbGet,
		Path:        "/study/:id/samples-with-data",
		PathParams:  []string{"id"},
		Query:       []string{"since", "until"},
		Paginated:   true,
		NewResult:   newSliceResult[SampleWithData],
		Summary:     "List a study's samples that have sequencing data",
		Description: "Lists the distinct samples linked to the given study (via library_samples) that have sequencing data available for this study, each qualified by the platforms it has products on. A sample has data for this study iff it has at least one row in the iRODS locations mirror scoped by id_study_lims = the study (real data objects in iRODS), so scoping is by the study the data is under, NOT data the sample has anywhere. Together with /study/:id/samples-without-data this partitions the study's linked samples (with_data + without_data = samples_total). platforms lists the canonical platform names the sample has products on in this study (e.g. Illumina, PacBio, Elembio, Ultimagen); it is [\"ONT\"] for an ONT sample (Oxford Nanopore is not tracked for availability/QC, only identity and study) and empty for a registered-only sample with no products. The optional since and until RFC3339 query params restrict the list to samples whose study-scoped data was ADDED to iRODS in the half-open window [since, until): the filter is on the iRODS creation timestamp (the created column), NEVER on last_updated or last_run (last_updated conflates newly-added with later-modified rows, and last_run is only when wa synced), so it answers \"added since X\"; since is inclusive and until is exclusive (created >= since AND created < until), comparison is in normalised UTC, and until is optional (the window is open-ended when omitted). The in-window list and /study/:id/samples-with-data/count with the same since/until stay the exact count<->list cross-check. Without since the list is all-time. A malformed since or until, or an until supplied without a since (until is only the upper bound of a window, so it is meaningless alone), is rejected with a 400 bad_request before the query runs. Membership is read from the cache mirrors, so results are complete only up to the feeding tables' last sync (see /freshness). Defaults to returning all samples; use limit/offset to page.",
		QueryParams: fetchAllPaginationWithAddedWindowParams(),
	},
	{
		Method:      "SamplesWithoutData",
		Verb:        registryVerbGet,
		Path:        "/study/:id/samples-without-data",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[SampleWithData],
		Summary:     "List a study's samples that lack sequencing data",
		Description: "Lists the distinct samples linked to the given study (via library_samples) that have NO sequencing data available for this study: the complement of /study/:id/samples-with-data, so with_data + without_data = samples_total. A sample has data for this study iff it has at least one row in the iRODS locations mirror scoped by id_study_lims = the study; scoping is by the study the data is under, NOT data the sample has anywhere, so a sample with data only under another study appears here. This list includes samples sequenced in this study but not yet in iRODS, registered-only samples, and ONT samples. platforms qualifies each negative: the canonical platform names the sample has products on in this study (e.g. Illumina, PacBio, Elembio, Ultimagen), [\"ONT\"] for an ONT sample (Oxford Nanopore is not tracked for availability/QC, only identity and study), and empty for a registered-only sample with no products. Membership is read from the cache mirrors, so results are complete only up to the feeding tables' last sync (see /freshness). Defaults to returning all samples; use limit/offset to page.",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "LatestDataForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/latest-data",
		PathParams:  []string{"id"},
		Query:       []string{"file_type"},
		Paginated:   true,
		NewResult:   newSliceResult[RecentDataRow],
		Summary:     "List newest data objects for a study",
		Description: "Returns a bounded, pageable newest-first page of raw iRODS data-object rows for the given study. Rows are ordered by iRODS created DESC (created is data added, never last_changed), with ties by (id_run, id_product); this is a page, NOT an unbounded MAX(created) tie set. Membership is the raw seq_product_irods_locations_mirror data-object row set scoped by (id_study_lims, created), not a product-export row set, so the first row's created timestamp reconciles with StudyOverview.newest_data_added. Each row carries the full irods_path, study id/name, sample name and supplier_name, id_run, lane, tag_index, platform, and merged flag. Set file_type to restrict rows to data objects whose iRODS file name ends in `.<file_type>` using the filename-suffix rule, matched case-insensitively with one leading dot stripped. Defaults to 10 rows, maximum 1000; use limit/offset to page. The rows are read from cache mirrors; freshness is reported by cache_synced_at on related aggregates and by /freshness.",
		QueryParams: latestDataPaginationParams(),
	},
	{
		Method:      "LatestDataForFacultySponsor",
		Verb:        registryVerbGet,
		Path:        "/latest-data/faculty-sponsor/:name",
		PathParams:  []string{"name"},
		Query:       []string{"file_type"},
		Paginated:   true,
		NewResult:   newSliceResult[RecentDataRow],
		Summary:     "List newest data objects by faculty sponsor",
		Description: "Returns a bounded, pageable newest-first page of raw iRODS data-object rows across SQSCP studies whose study_mirror.faculty_sponsor contains the supplied name; faculty_sponsor is a Study field, NOT a study_users role. Each matching study contributes only its bounded top rows through the (id_study_lims, created) access path, and those per-study candidates are merged by iRODS created DESC (created is data added, never last_changed) with ties by (id_run, id_product), avoiding one request per study and avoiding an unbounded global filesort over every file. Membership is the raw seq_product_irods_locations_mirror row set, so results reconcile with each study's StudyOverview.newest_data_added. Set file_type to restrict rows to data objects whose iRODS file name ends in `.<file_type>` using the filename-suffix rule. Defaults to 10 rows, maximum 1000; use limit/offset to page. The rows are read from cache mirrors; freshness is reported by cache_synced_at on related aggregates and by /freshness.",
		QueryParams: latestDataPaginationParams(),
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
		Query:       []string{"file_type", "deliverables_only", "order_by", "since", "until"},
		Paginated:   true,
		NewResult:   newSliceResult[IRODSPath],
		Summary:     "List iRODS paths for a sample",
		Description: "Lists the iRODS data-object paths exported for the given sample (by Sanger sample name). Rows expose created (iRODS created time: data added, never last_changed), id_run, lane, tag_index, manual_qc (qc.go roll-up fail>pending>pass, empty when no product metrics), deliverable, merged, sample identity fields (id_sample_tmp, name, supplier_name, sanger_sample_id, accession_number) and study identity fields (id_study_lims, study_accession_number). deliverable uses iseq_flowcell.entity_type IN ('library','library_indexed') or Element/Ultima is_sequencing_control=0, approximates iRODS target=1, is NOT is_spiked, and is pass-through for PacBio/ONT. Defaults to returning all paths; use limit/offset to page. Set file_type to restrict the list to data objects whose iRODS file name ends in `.<file_type>`, matched case-insensitively with a single leading dot stripped (so `cram`, `.CRAM` and `CRAM` are equivalent); it is a filename-suffix filter, not a real file-type column, so a valid but unmatched suffix yields an empty list (not an error), and the matching /count honours the same filter. Set deliverables_only=true to apply the deliverable filter where a discriminator exists. An empty/whitespace file_type or one containing '%', '_' or '/' is rejected with a 400 bad_request. The list is read from the cache mirrors, so it is complete only up to their sync state (see /freshness).",
		QueryParams: fetchAllPaginationWithFileTypeParams("sample-scoped iRODS data objects added to iRODS"),
	},
	{
		Method:      "IRODSPathsForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/irods",
		PathParams:  []string{"id"},
		Query:       []string{"file_type", "deliverables_only", "order_by", "since", "until"},
		Paginated:   true,
		NewResult:   newSliceResult[IRODSPath],
		Summary:     "List iRODS paths for a study",
		Description: "Lists the iRODS data-object paths exported for the given study. Rows expose created (iRODS created time: data added, never last_changed), id_run, lane, tag_index, manual_qc (qc.go roll-up fail>pending>pass, empty when no product metrics), deliverable, merged, sample identity fields (id_sample_tmp, name, supplier_name, sanger_sample_id, accession_number) and study identity fields (id_study_lims, study_accession_number); merged=true marks merged multi-lane composite CRAM attribution and reports id_run=0, lane=0, tag_index=0. deliverable uses iseq_flowcell.entity_type IN ('library','library_indexed') or Element/Ultima is_sequencing_control=0, approximates iRODS target=1, is NOT is_spiked, and is pass-through for PacBio/ONT. Defaults to returning all paths; use limit/offset to page. Set file_type to restrict the list to data objects whose iRODS file name ends in `.<file_type>`, matched case-insensitively with a single leading dot stripped (so `cram`, `.CRAM` and `CRAM` are equivalent); it is a filename-suffix filter, not a real file-type column, so a valid but unmatched suffix yields an empty list (not an error), and the matching /count honours the same filter. Set deliverables_only=true to apply the deliverable filter where a discriminator exists. An empty/whitespace file_type or one containing '%', '_' or '/' is rejected with a 400 bad_request. The list is read from the cache mirrors, so it is complete only up to their sync state (see /freshness).",
		QueryParams: fetchAllPaginationWithFileTypeParams("study-scoped iRODS data objects added to iRODS"),
	},
	{
		Method:      "IRODSPathsForRun",
		Verb:        registryVerbGet,
		Path:        "/run/:id/irods",
		PathParams:  []string{"id"},
		Query:       []string{"file_type", "deliverables_only", "order_by", "since", "until"},
		Paginated:   true,
		NewResult:   newSliceResult[IRODSPath],
		Summary:     "List iRODS paths for a run",
		Description: "Lists the iRODS data objects on the given run by reading seq_product_irods_locations_mirror rows whose denormalized id_run equals the requested run, plus rows recovered through iseq_product_metrics_mirror for single-run merged composites when the mirror row's id_run is 0, one row per data object. Rows expose created (iRODS created time: data added, never last_changed), id_run, lane, tag_index, manual_qc (qc.go roll-up fail>pending>pass, empty when no product metrics), deliverable, merged, sample identity fields (id_sample_tmp, name, supplier_name, sanger_sample_id, accession_number) and study identity fields (id_study_lims, study_accession_number); merged=true marks merged multi-lane composite CRAM attribution and public merged rows report id_run=0, lane=0, tag_index=0 even when product-metrics recovery made them visible to the run. deliverable uses iseq_flowcell.entity_type IN ('library','library_indexed') or Element/Ultima is_sequencing_control=0, approximates iRODS target=1, is NOT is_spiked, and is pass-through for PacBio/ONT. :id is the Illumina NPG run id (the existing run/ResolveRun identifier space; no new resolver): a non-Illumina or otherwise invalid run yields the existing not-found / unsupported-identifier error, and a numeric run absent from the synced cache yields not_found. Defaults to returning all data objects; use limit/offset to page, and it is bounded and paginated like /study/:id/irods and /sample/:id/irods, setting the X-Total-Count and X-Next-Offset list-sizing headers from the matching /count (so X-Total-Count equals /run/:id/irods/count and the two cannot drift). Set file_type to restrict the list to data objects whose iRODS file name ends in `.<file_type>`, matched case-insensitively with a single leading dot stripped (so `cram`, `.CRAM` and `CRAM` are equivalent); it is a filename-suffix filter, not a real file-type column, so a valid but unmatched suffix yields an empty list (not an error), and the matching /count honours the same filter. Set deliverables_only=true to apply the deliverable filter where a discriminator exists. An empty/whitespace file_type or one containing '%', '_' or '/' is rejected with a 400 bad_request. The list is read from the cache mirrors, so it is complete only up to their sync state (see /freshness).",
		QueryParams: fetchAllPaginationWithFileTypeParams("run-scoped iRODS data objects added to iRODS"),
	},
	{
		Method:      "StudiesForSample",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/studies",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newSliceResult[Study],
		Summary:     "List studies for a sample",
		Description: "Lists the studies the given sample (by Sanger sample name) belongs to and sets X-Total-Count / X-Next-Offset from /sample/:id/studies/count.",
	},
	{
		Method:      "CountStudiesForSample",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/studies/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count studies for a sample",
		Description: "Returns the number of distinct studies the given sample belongs to, the count counterpart of /sample/:id/studies.",
	},
	{
		Method:      "StudiesForProgramme",
		Verb:        registryVerbGet,
		Path:        "/studies/programme/:term",
		PathParams:  []string{"term"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[Study],
		Summary:     "List studies by programme",
		Description: "Lists SQSCP studies whose programme exactly matches the supplied value, the programme grouping / attribution unit used by sequencing aggregates. Defaults to a page of 100, maximum 1000. The list is read from the cache mirror, so it is complete only up to the study table sync (see /freshness).",
		QueryParams: searchPaginationParams(),
	},
	{
		Method:      "CountStudiesForProgramme",
		Verb:        registryVerbGet,
		Path:        "/studies/programme/:term/count",
		PathParams:  []string{"term"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count studies by programme",
		Description: "Returns the number of SQSCP studies whose programme exactly matches the supplied value, the count counterpart of /studies/programme/:term and the same programme grouping / attribution unit used by sequencing aggregates. The count is read from the cache mirror, so it is complete only up to the study table sync (see /freshness).",
	},
	{
		Method:      "Programmes",
		Verb:        registryVerbGet,
		Path:        "/programmes",
		PathParams:  []string{},
		Query:       []string{},
		NewResult:   newSliceResult[Programme],
		Summary:     "List programmes",
		Description: "Lists the distinct non-empty SQSCP programme values with their study counts, so callers can discover the programme grouping / attribution vocabulary. Each sequencing product maps through exactly one study to that study's programme. The list is read from the cache mirror, so it is complete only up to the study table sync (see /freshness).",
	},
	{
		Method:      "StudyUsers",
		Verb:        registryVerbGet,
		Path:        "/study/:id/users",
		PathParams:  []string{"id"},
		Query:       []string{"role"},
		Paginated:   true,
		NewResult:   newSliceResult[StudyUser],
		Summary:     "List study users",
		Description: "Lists the study->users inverse: study_users role assignments for the given study from a single id_study_tmp lookup. DEFAULT no role filter returns ALL roles present (unlike /studies/user, whose default is owner, manager and data_access_contact). Set role to a comma-separated stored-role filter over owner, manager, data_access_contact, follower, slf_manager, lab_manager and administrator. The faculty_sponsor is a Study field, NOT a study_users role. Defaults to returning all rows; use limit/offset to page. The list is read from the cache mirror, so it is complete only up to the study_users table sync (see /freshness).",
		QueryParams: fetchAllPaginationWithStudyUsersRoleParams(),
	},
	{
		Method:      "CountStudyUsers",
		Verb:        registryVerbGet,
		Path:        "/study/:id/users/count",
		PathParams:  []string{"id"},
		Query:       []string{"role"},
		NewResult:   newResult[Count],
		Summary:     "Count study users",
		Description: "Returns the number of study->users role assignments for the given study, the count counterpart of /study/:id/users, honouring the same optional role filter. DEFAULT no role filter counts ALL roles present across owner, manager, data_access_contact, follower, slf_manager, lab_manager and administrator. The faculty_sponsor is a Study field, NOT a study_users role. The count is read from the cache mirror, so it is complete only up to the study_users table sync (see /freshness).",
		QueryParams: []QueryParam{studyUsersRoleQueryParam()},
	},
	{
		Method:      "SampleCRAMsForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/sample-crams",
		PathParams:  []string{"id"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[SampleCRAM],
		Summary:     "List sample CRAMs for a study",
		Description: "Lists one selected CRAM per sample for the given study, resolving each sample through the iRODS mirror's id_sample_tmp linkage and the cram filename suffix, and preferring merged composite objects when present. This is the merged-aware per-sample CRAM attribution surface. Defaults to returning all rows; use limit/offset to page. The list is read from the cache mirrors, so it is complete only up to their sync state (see /freshness).",
		QueryParams: fetchAllPaginationParams(),
	},
	{
		Method:      "CountSampleCRAMsForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/sample-crams/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count sample CRAMs for a study",
		Description: "Returns the number of selected per-sample CRAM rows for the given study, the count counterpart of /study/:id/sample-crams. It uses the same cram filename suffix and merged composite preference, so the count is one selected CRAM per sample. The count is read from the cache mirrors, so it is complete only up to their sync state (see /freshness).",
	},
	{
		Method:      "Export",
		Verb:        registryVerbGet,
		Path:        "/export/:children/:parent_kind/:parent_id",
		PathParams:  []string{"children", "parent_kind", "parent_id"},
		Query:       []string{"columns", "file_type", "deliverables_only", "role", "qc", "library_type", "organism", "sort", "order_by", "since", "until", "all", "cursor", "format"},
		NewResult:   newResult[ExportResult],
		Summary:     "Export relationship rows",
		Description: "Projects one supported MLWH export relationship into ordered string rows for the selected columns, matching the relationship grammar used by `wa mlwh export <children> <parent-kind> <parent-id>`. The response body carries Columns, Rows, Total, NextCursor, Complete and Format. HTTP query parameters support columns as an ordered comma-separated projection; file_type to restrict file exports by filename suffix; deliverables_only for file exports and product exports: file exports restrict to deliverable rows (the CRAM irods/files/sample-crams default excludes controls/sub-products), while products filter product rows using Illumina `iseq_flowcell.entity_type IN ('library','library_indexed')` and can change product rows/Total without depending on attached iRODS rows; qc, library_type and organism for shared export filters where supported; limit/offset for bounded pages; all for the complete matching set; cursor for iRODS/products keyset pagination; and format as tsv, csv or json metadata for callers that render the result. For products, products are product-grained, one row per distinct `(id_run, position, tag_index)`, including products with no iRODS object; products are keyset-cursor paginated; the default products response is a bounded page up to the internal 1000 default carrying Total, NextCursor and Complete; use cursor to continue products; use all=true for the complete products set; file_type only restricts the attached irods_path for products and does not drop product rows. A never-synced cache returns cache_never_synced so CLIs can degrade cleanly.",
		QueryParams: exportQueryParams(),
	},
	{
		Method:      "StudiesForFacultySponsor",
		Verb:        registryVerbGet,
		Path:        "/studies/faculty-sponsor/:name",
		PathParams:  []string{"name"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[PersonStudy],
		Summary:     "List studies by faculty sponsor",
		Description: "Lists the studies of a named PI/SPONSOR, matching study.faculty_sponsor (the named principal investigator / faculty sponsor, a free-text field) for the term as a case-insensitive substring, so \"carl\" matches \"Carl Anderson\". Each row carries the full study plus an empty role (the faculty sponsor is NOT a study_users role). This is DISTINCT from /studies/user/:person, which matches study_users ROLE MEMBERSHIP (owner/manager/...) across name, login and email and returns a different set; use this endpoint for the named sponsor and that one for role membership. Ordered by id_study_lims. Defaults to a page of 100, maximum 1000, and it sets the X-Total-Count and X-Next-Offset list-sizing headers from the matching /count (so X-Total-Count equals /studies/faculty-sponsor/:name/count and the two cannot drift). A whitespace-only name is rejected with a 400 bad_request. The match is read from the cache mirror, so it is complete only up to the study table's last sync (see /freshness); a never-synced cache returns an empty list together with a cache-never-synced signal and a synced cache with no match returns an empty list (not an error).",
		QueryParams: searchPaginationParams(),
	},
	{
		Method:      "CountStudiesForFacultySponsor",
		Verb:        registryVerbGet,
		Path:        "/studies/faculty-sponsor/:name/count",
		PathParams:  []string{"name"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count studies by faculty sponsor",
		Description: "Returns the number of studies of a named PI/SPONSOR, the count counterpart of /studies/faculty-sponsor/:name (count == the length of that list when all rows are fetched, so X-Total-Count on the list equals this count and the two cannot drift). It matches study.faculty_sponsor (the named principal investigator / faculty sponsor, a free-text field) for the term as a case-insensitive substring with no LIMIT, DISTINCT from /studies/user/:person/count (which counts study_users role membership). A whitespace-only name is rejected with a 400 bad_request. The count is read from the cache mirror, so it is complete only up to the study table's last sync (see /freshness); a never-synced cache returns a cache-never-synced signal and a synced cache with no match returns 0 (not an error).",
	},
	{
		Method:      "StudiesForUser",
		Verb:        registryVerbGet,
		Path:        "/studies/user/:person",
		PathParams:  []string{"person"},
		Query:       []string{"role"},
		Paginated:   true,
		NewResult:   newSliceResult[PersonStudy],
		Summary:     "List studies by user role membership",
		Description: "Lists the studies a person is a study_users ROLE MEMBER of, matching study_users (the per-study membership rows) where the term is a case-insensitive SUBSTRING of name, login OR email, so an email/login or a name both resolve and a caller given only an email is not falsely empty. This is DISTINCT from /studies/faculty-sponsor/:name, which matches the named PI/sponsor (study.faculty_sponsor, free-text) and returns a different set; use this endpoint for role membership and that one for the named sponsor. The DEFAULT role set is owner, manager and data_access_contact; set role to a comma-separated list to OVERRIDE (replace) that default set, matched exactly and case-insensitively (e.g. role=follower returns only follower rows, widening the result to followers; role=owner,manager returns owners and managers). Each row carries the full study plus the matched role; the same study may match under several roles, so rows are de-duplicated to one per (id_study_lims, role) and ordered by (id_study_lims, role). Defaults to a page of 100, maximum 1000, and it sets the X-Total-Count and X-Next-Offset list-sizing headers from the matching /count (so X-Total-Count equals /studies/user/:person/count and the two cannot drift). A whitespace-only person is rejected with a 400 bad_request. The match is read from the cache mirrors, so it is complete only up to the study and study_users tables' last syncs (see /freshness); a never-synced cache returns an empty list together with a cache-never-synced signal and a synced cache with no match returns an empty list (not an error).",
		QueryParams: searchPaginationWithRoleParams(),
	},
	{
		Method:      "CountStudiesForUser",
		Verb:        registryVerbGet,
		Path:        "/studies/user/:person/count",
		PathParams:  []string{"person"},
		Query:       []string{"role"},
		NewResult:   newResult[Count],
		Summary:     "Count studies by user role membership",
		Description: "Returns the number of studies a person is a study_users ROLE MEMBER of, the count counterpart of /studies/user/:person (count == the length of that list when all rows are fetched, so X-Total-Count on the list equals this count and the two cannot drift). It matches study_users where the term is a case-insensitive substring of name, login OR email and counts the distinct (id_study_lims, role) matches with no LIMIT, DISTINCT from /studies/faculty-sponsor/:name/count (which counts the named faculty_sponsor). The DEFAULT role set is owner, manager and data_access_contact; set role to a comma-separated list to OVERRIDE (replace) that default set, matched exactly and case-insensitively, so the count honours the same role filter as the list (e.g. role=follower counts only follower rows). A whitespace-only person is rejected with a 400 bad_request. The count is read from the cache mirrors, so it is complete only up to the study and study_users tables' last syncs (see /freshness); a never-synced cache returns a cache-never-synced signal and a synced cache with no match returns 0 (not an error).",
		QueryParams: []QueryParam{roleQueryParam()},
	},
	{
		Method:      "ResolvePerson",
		Verb:        registryVerbGet,
		Path:        "/resolve-person/:term",
		PathParams:  []string{"term"},
		Query:       []string{},
		Paginated:   true,
		NewResult:   newSliceResult[PersonCandidate],
		Summary:     "Resolve a partial person to candidate stored forms",
		Description: "Translates a partial/spoken person name (or a login/email fragment) into the DISTINCT candidate stored forms, so a caller can disambiguate among several people BEFORE running a studies query. It returns candidates from BOTH person sources, matching the term as a case-insensitive substring: from study_mirror, the distinct faculty_sponsor values containing the term (source=faculty_sponsor, name=the sponsor text, login/email/role empty) -- faculty_sponsor is a FREE-TEXT full name (the named PI/sponsor) -- each with study_count = the distinct SQSCP studies for that sponsor; and from study_users_mirror, the distinct (name, login, email, role) tuples where the term is a substring of name, login OR email (source=study_users) -- study_users identifies a person by name AND login (the Sanger username) AND email -- each with study_count = the distinct studies for that candidate's (login, role). The two study_count bases differ by design: candidates are grouped by (name, login, email, role) but the study_users study_count is per (login, role). The match is across name, login AND email, so a login or email fragment resolves to the stored name and vice versa. Ordered by (source, name, login, role) for determinism. Defaults to a page of 100, maximum 1000, and it sets the X-Total-Count and X-Next-Offset list-sizing headers from the matching /count (so X-Total-Count equals /resolve-person/:term/count and the two cannot drift). ROUTING GUIDANCE: if a narrow term yields nothing or is ambiguous, enumerate candidates here rather than dead-ending, then use /studies/faculty-sponsor or /studies/user with the chosen stored form. A whitespace-only term is rejected with a 400 bad_request. The candidates are read from the cache mirrors, so the result is complete only up to the study and study_users tables' last syncs (see /freshness); a never-synced cache returns an empty list together with a cache-never-synced signal and a synced cache with no match returns an empty list (not an error).",
		QueryParams: searchPaginationParams(),
	},
	{
		Method:      "CountResolvePerson",
		Verb:        registryVerbGet,
		Path:        "/resolve-person/:term/count",
		PathParams:  []string{"term"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count resolve-person candidates",
		Description: "Returns the number of DISTINCT candidate people matching the term across BOTH person sources, the count counterpart of /resolve-person/:term (count == the length of that list when all rows are fetched, so X-Total-Count on the list equals this count and the two cannot drift). It counts, with no LIMIT, the distinct study_mirror faculty_sponsor values containing the term (faculty_sponsor is a free-text full name) plus the distinct study_users_mirror (name, login, email, role) tuples where the term is a case-insensitive substring of name, login OR email (study_users identifies a person by name AND login AND email). A whitespace-only term is rejected with a 400 bad_request. The count is read from the cache mirrors, so it is complete only up to the study and study_users tables' last syncs (see /freshness); a never-synced cache returns a cache-never-synced signal and a synced cache with no match returns 0 (not an error).",
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
		Query:       []string{"lean"},
		NewResult:   newResult[StudyDetail],
		Summary:     "Get study detail",
		Description: "Returns the given study with the detail of each of its libraries and their samples. The response is de-duplicated to stay bounded: each distinct study and library is carried once in the study_lookup / library_lookup tables (keyed by id) and the nested sample rows under library_details reference them by id rather than re-embedding the same study and library objects under every sample. The optional limit/offset query params paginate the nested sample collection (defaulting to every sample), and X-Total-Count reports the full nested sample count while X-Next-Offset gives the offset of the next page (offset+returned, or -1 on the last page), exactly as the paginated list endpoints do. The optional lean query param (a boolean) drops the heavy library_details and lookup tables and returns only the top-level study plus the flat sample_ids and library_ids lists, so the response is strictly smaller.",
		QueryParams: detailQueryParams(),
	},
	{
		Method:      "RunDetail",
		Verb:        registryVerbGet,
		Path:        "/run/:id/detail",
		PathParams:  []string{"id"},
		Query:       []string{"lean"},
		NewResult:   newResult[RunDetail],
		Summary:     "Get run detail",
		Description: "Returns the given run with its related samples, studies, and per-study detail. The response is de-duplicated to stay bounded: each distinct study and library is carried once in the study_lookup / library_lookup tables (keyed by id) and the nested sample rows (both samples and study_details) reference them by id rather than re-embedding the same study and library objects under every sample. The optional limit/offset query params paginate the nested sample collection (defaulting to every sample, with studies and study_details rebuilt from the page), and X-Total-Count reports the full nested sample count while X-Next-Offset gives the offset of the next page (offset+returned, or -1 on the last page), exactly as the paginated list endpoints do. The optional lean query param (a boolean) drops the heavy samples, studies, study_details and lookup tables and returns only the run plus the flat sample_ids and study_ids lists, so the response is strictly smaller.",
		QueryParams: detailQueryParams(),
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
		Description: "Returns studies whose name, title, programme, or faculty sponsor contains the term (case-insensitive substring, minimum 3 characters). Each result is a full study row carrying id_study_lims, name and faculty_sponsor, so two studies sharing the same name are still distinguishable by their distinct id_study_lims and faculty_sponsor without a further call. Defaults to a page of 100, maximum 1000.",
		QueryParams: searchPaginationParams(),
	},
	{
		Method:      "SearchSamples",
		Verb:        registryVerbGet,
		Path:        "/search/sample/:term",
		PathParams:  []string{"term"},
		Query:       []string{"words", "organism", "library_type", "qc", "deliverables_only"},
		Paginated:   true,
		NewResult:   newSliceResult[Sample],
		Summary:     "Search samples by literal prefix",
		Description: "Returns samples whose name, supplier_name, common_name, donor_id fields have the term as a literal whole-value prefix by default (case-insensitive, LIKE 'term%' with escaping; minimum 3 characters for the free-text term). Set words=true for the opt-in separator-agnostic word-prefix mode over the same fields; it is NOT the default. Exact filters organism, library_type, qc and deliverables_only AND-combine with the term and are exempt from the 3-character minimum: organism resolves whole-word common_name membership, library_type matches library_samples.pipeline_id_lims exactly, qc uses the per-sample roll-up from qc.go (fail>pending>pass), and deliverables_only uses the deliverable discriminator entity_type / is_sequencing_control, NOT is_spiked, with pass-through for PacBio/ONT. QC grain differs from export: sample search is per-sample roll-up, while export is raw per-product qc. Defaults to a page of 100, maximum 1000.",
		QueryParams: sampleSearchPaginationParams(),
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
		Query:       []string{"words", "organism", "library_type", "qc", "deliverables_only"},
		NewResult:   newResult[Count],
		Summary:     "Count samples matching literal prefix",
		Description: "Returns the number of samples matching the same optioned sample search as /search/sample/:term, without transferring rows. The default is a literal whole-value prefix over name, supplier_name, common_name and donor_id; words=true switches to opt-in word-prefix mode, and organism, library_type, qc and deliverables_only are exact filters. qc uses the per-sample roll-up from qc.go (fail>pending>pass), while export QC is raw per-product qc; deliverables_only uses entity_type / is_sequencing_control, NOT is_spiked, with pass-through for PacBio/ONT. The count is exact up to a bound and reports that bound as a floor for very common terms.",
		QueryParams: sampleSearchOptionQueryParams(),
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
		Method:      "CountSamplesWithData",
		Verb:        registryVerbGet,
		Path:        "/study/:id/samples-with-data/count",
		PathParams:  []string{"id"},
		Query:       []string{"since", "until"},
		NewResult:   newResult[Count],
		Summary:     "Count a study's samples that have sequencing data",
		Description: "Returns the number of distinct samples linked to the given study (via library_samples) that have sequencing data available for this study, the count counterpart of /study/:id/samples-with-data (count == the length of that list when all rows are fetched). A sample has data for this study iff it has at least one row in the iRODS locations mirror scoped by id_study_lims = the study (real data objects in iRODS); scoping is by the study the data is under, NOT data the sample has anywhere. The figure counts distinct samples, never iRODS data objects, so a sample with many study-scoped iRODS rows is counted once. The optional since and until RFC3339 query params restrict the count to samples whose study-scoped data was ADDED to iRODS in the half-open window [since, until): the filter is on the iRODS creation timestamp (the created column), NEVER on last_updated or last_run (last_updated conflates newly-added with later-modified rows, and last_run is only when wa synced), so it answers \"added since X\"; since is inclusive and until is exclusive (created >= since AND created < until), comparison is in normalised UTC, and until is optional (the window is open-ended when omitted). Without since the count is all-time. A malformed since or until, or an until supplied without a since (until is only the upper bound of a window, so it is meaningless alone), is rejected with a 400 bad_request before the query runs. Membership is read from the cache mirrors, so the count is complete only up to the feeding tables' last sync (see /freshness).",
		QueryParams: addedWindowQueryParams("samples whose study-scoped data was added to iRODS"),
	},
	{
		Method:      "CountLatestDataForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/latest-data/count",
		PathParams:  []string{"id"},
		Query:       []string{"file_type"},
		NewResult:   newResult[Count],
		Summary:     "Count newest data rows for a study",
		Description: "Returns the number of raw seq_product_irods_locations_mirror rows for the given study, optionally restricted by file_type using the filename-suffix rule, so X-Total-Count on /study/:id/latest-data sizes the same raw iRODS-location membership used by the list. This is a raw data-object count over iRODS created (created is data added, never last_changed), not a product-export count. The count is read from cache mirrors; freshness is reported by cache_synced_at on related aggregates and by /freshness.",
		QueryParams: []QueryParam{fileTypeQueryParam()},
	},
	{
		Method:      "CountLatestDataForFacultySponsor",
		Verb:        registryVerbGet,
		Path:        "/latest-data/faculty-sponsor/:name/count",
		PathParams:  []string{"name"},
		Query:       []string{"file_type"},
		NewResult:   newResult[Count],
		Summary:     "Count newest data rows by faculty sponsor",
		Description: "Returns the number of raw seq_product_irods_locations_mirror rows under SQSCP studies whose study_mirror.faculty_sponsor contains the supplied name; faculty_sponsor is a Study field, NOT a study_users role. file_type uses the filename-suffix rule, so X-Total-Count on /latest-data/faculty-sponsor/:name sizes the same raw iRODS-location membership used by the list. This is a raw data-object count over iRODS created (created is data added, never last_changed). The count is read from cache mirrors; freshness is reported by cache_synced_at on related aggregates and by /freshness.",
		QueryParams: []QueryParam{fileTypeQueryParam()},
	},
	{
		Method:      "CountSamplesForRun",
		Verb:        registryVerbGet,
		Path:        "/run/:id/samples/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples on a run",
		Description: "Returns the number of distinct samples sequenced on the given run, the count counterpart of /run/:id/samples (count == the length of that list when all rows are fetched), via the same iseq_product_metrics_mirror join on id_run with no LIMIT. :id is the run id; a non-numeric id is rejected as an unsupported identifier, and a run absent from the synced cache yields not_found. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).",
	},
	{
		Method:      "CountSamplesForLibrary",
		Verb:        registryVerbGet,
		Path:        "/library/:pipeline/study/:study/samples/count",
		PathParams:  []string{"pipeline", "study"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples in a library",
		Description: "Returns the number of distinct samples in the library identified by its pipeline LIMS id within the given study, the count counterpart of /library/:pipeline/study/:study/samples (count == the length of that list when all rows are fetched), via the same library_samples join filtered by pipeline_id_lims and id_study_lims with no LIMIT. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).",
	},
	{
		Method:      "CountSamplesForLibraryID",
		Verb:        registryVerbGet,
		Path:        "/library-id/:id/samples/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples by library id",
		Description: "Returns the number of distinct samples in the library with the given library id, the count counterpart of /library-id/:id/samples (count == the length of that list when all rows are fetched), via the same library_samples filter on library_id with no LIMIT. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).",
	},
	{
		Method:      "CountSamplesForLibraryLimsID",
		Verb:        registryVerbGet,
		Path:        "/library-lims-id/:id/samples/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples by LIMS library id",
		Description: "Returns the number of distinct samples in the library with the given LIMS library id, the count counterpart of /library-lims-id/:id/samples (count == the length of that list when all rows are fetched), via the same library_samples filter on id_library_lims with no LIMIT. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).",
	},
	{
		Method:      "CountSamplesForLibraryType",
		Verb:        registryVerbGet,
		Path:        "/library-type/:id/samples/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples by library type",
		Description: "Returns the number of distinct samples in libraries of the given library type, the count counterpart of /library-type/:id/samples (count == the length of that list when all rows are fetched), via the same library_samples filter on pipeline_id_lims with no LIMIT. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).",
	},
	{
		Method:      "CountRunsForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/runs/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count runs for a study",
		Description: "Returns the number of distinct sequencing runs associated with the given study, the count counterpart of /study/:id/runs (count == the length of that list when all rows are fetched), via the same iseq_product_metrics_mirror filter on id_study_lims with no LIMIT. An unknown study yields not_found. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).",
	},
	{
		Method:      "CountRunsForSample",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/runs/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count runs for a sample",
		Description: "Returns the number of distinct sequencing runs associated with the given sample, the count counterpart of /sample/:id/runs.",
	},
	{
		Method:      "CountRunListing",
		Verb:        registryVerbGet,
		Path:        "/runs/count",
		Query:       []string{"since", "until", "platform"},
		NewResult:   newResult[Count],
		Summary:     "Count global runs",
		Description: "Returns the number of rows matching the same global all-runs listing filters as /runs, without transferring rows. The count uses the same run grain as the list: Illumina, Elembio and Ultimagen distinct id_run, PacBio distinct pac_bio_run_name, and ONT distinct experiment_name, with the same date_basis including ONT warehouse load time - not a true sequencing date. The count is read from cache mirrors; freshness is reported by cache_synced_at on related list rows and by /freshness.",
		QueryParams: monthlyRunQueryParams(),
	},
	{
		Method:      "CountLibrariesForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/libraries/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count libraries in a study",
		Description: "Returns the number of distinct libraries belonging to the given study, the count counterpart of /study/:id/libraries (count == the length of that list when all rows are fetched), counting the distinct (pipeline_id_lims, library_id, id_library_lims) library_samples groupings the list returns with no LIMIT. An unknown study yields not_found. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).",
	},
	{
		Method:      "CountLanesForSample",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/lanes/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count lanes for a sample",
		Description: "Returns the number of distinct run/lane/tag combinations on which the given sample (by Sanger sample name) was sequenced, the count counterpart of /sample/:id/lanes (count == the length of that list when all rows are fetched), counting the distinct (id_run, position, tag_index) rows the list returns with no LIMIT. An unknown sample yields not_found. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).",
	},
	{
		Method:      "CountIRODSPathsForSample",
		Verb:        registryVerbGet,
		Path:        "/sample/:id/irods/count",
		PathParams:  []string{"id"},
		Query:       []string{"file_type", "deliverables_only", "since", "until"},
		NewResult:   newResult[Count],
		Summary:     "Count iRODS paths for a sample",
		Description: "Returns the number of distinct iRODS data objects exported for the given sample (by Sanger sample name), the count counterpart of /sample/:id/irods (count == the length of that list when all rows are fetched), counting the distinct iRODS data objects the list returns with no LIMIT. Set file_type to count only data objects whose iRODS file name ends in `.<file_type>`, matched case-insensitively with a single leading dot stripped (the same filename-suffix filter as the list, so the count honours it and a valid but unmatched suffix yields 0, not an error); an empty/whitespace file_type or one containing '%', '_' or '/' is rejected with a 400 bad_request. Set deliverables_only=true to count only deliverable rows: the mirrored is_deliverable value is derived from Illumina iseq_flowcell.entity_type IN ('library','library_indexed') and Element/Ultima is_sequencing_control=0, approximates iRODS target=1, is NOT is_spiked, and passes through PacBio/ONT rows with no discriminator. The optional since and until query params restrict rows by iRODS created time in the half-open window [since, until): created >= since and created < until; until is optional but requires since, and without since the count is all-time. An unknown sample yields not_found. The count is read from the iRODS locations mirror, so it is complete only up to that table's last sync (see /freshness).",
		QueryParams: irodsCountQueryParams("sample-scoped iRODS data objects added to iRODS"),
	},
	{
		Method:      "CountIRODSPathsForStudy",
		Verb:        registryVerbGet,
		Path:        "/study/:id/irods/count",
		PathParams:  []string{"id"},
		Query:       []string{"file_type", "deliverables_only", "since", "until"},
		NewResult:   newResult[Count],
		Summary:     "Count iRODS paths for a study",
		Description: "Returns the number of distinct iRODS data objects exported for the given study, the count counterpart of /study/:id/irods (count == the length of that list when all rows are fetched), counting the distinct iRODS rows the list returns (scoped by id_study_lims) with no LIMIT. Set file_type to count only data objects whose iRODS file name ends in `.<file_type>`, matched case-insensitively with a single leading dot stripped (the same filename-suffix filter as the list, so the count honours it and a valid but unmatched suffix yields 0, not an error); an empty/whitespace file_type or one containing '%', '_' or '/' is rejected with a 400 bad_request. Set deliverables_only=true to count only deliverable rows: the mirrored is_deliverable value is derived from Illumina iseq_flowcell.entity_type IN ('library','library_indexed') and Element/Ultima is_sequencing_control=0, approximates iRODS target=1, is NOT is_spiked, and passes through PacBio/ONT rows with no discriminator. The optional since and until query params restrict rows by iRODS created time in the half-open window [since, until): created >= since and created < until; until is optional but requires since, and without since the count is all-time. An unknown study yields not_found. The count is read from the iRODS locations mirror, so it is complete only up to that table's last sync (see /freshness).",
		QueryParams: irodsCountQueryParams("study-scoped iRODS data objects added to iRODS"),
	},
	{
		Method:      "CountIRODSPathsForRun",
		Verb:        registryVerbGet,
		Path:        "/run/:id/irods/count",
		PathParams:  []string{"id"},
		Query:       []string{"file_type", "deliverables_only", "since", "until"},
		NewResult:   newResult[Count],
		Summary:     "Count iRODS paths for a run",
		Description: "Returns the number of iRODS data objects on the given run, the count counterpart of /run/:id/irods (count == the length of that list when all rows are fetched), counting the same run scope as the list with no LIMIT: seq_product_irods_locations_mirror rows whose denormalized id_run equals the requested run, plus rows recovered through iseq_product_metrics_mirror for single-run merged composites when the mirror row's id_run is 0. Public merged rows on the list report id_run=0, lane=0 and tag_index=0 even when product-metrics recovery made them visible to the run. :id is the Illumina NPG run id (the existing run/ResolveRun identifier space; no new resolver): a non-Illumina or otherwise invalid run yields the existing not-found / unsupported-identifier error, and a numeric run absent from the synced cache yields not_found. Set file_type to count only data objects whose iRODS file name ends in `.<file_type>`, matched case-insensitively with a single leading dot stripped (the same filename-suffix filter as the list, so the count honours it and a valid but unmatched suffix yields 0, not an error); an empty/whitespace file_type or one containing '%', '_' or '/' is rejected with a 400 bad_request. Set deliverables_only=true to count only deliverable rows: the mirrored is_deliverable value is derived from Illumina iseq_flowcell.entity_type IN ('library','library_indexed') and Element/Ultima is_sequencing_control=0, approximates iRODS target=1, is NOT is_spiked, and passes through PacBio/ONT rows with no discriminator. The optional since and until query params restrict rows by iRODS created time in the half-open window [since, until): created >= since and created < until; until is optional but requires since, and without since the count is all-time. The count is read from the iRODS locations mirror, so it is complete only up to that table's last sync (see /freshness).",
		QueryParams: irodsCountQueryParams("run-scoped iRODS data objects added to iRODS"),
	},
	{
		Method:      "CountFindSamplesBySangerID",
		Verb:        registryVerbGet,
		Path:        "/find/sample/sanger-id/:id/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples by Sanger sample id",
		Description: "Returns the number of samples whose Sanger sample id exactly matches the given value, the count counterpart of /find/sample/sanger-id/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).",
	},
	{
		Method:      "CountFindSamplesByIDSampleLims",
		Verb:        registryVerbGet,
		Path:        "/find/sample/lims-id/:id/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples by LIMS sample id",
		Description: "Returns the number of samples whose LIMS sample id exactly matches the given value, the count counterpart of /find/sample/lims-id/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).",
	},
	{
		Method:      "CountFindSamplesByAccessionNumber",
		Verb:        registryVerbGet,
		Path:        "/find/sample/accession/:id/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples by accession number",
		Description: "Returns the number of samples whose accession number exactly matches the given value, the count counterpart of /find/sample/accession/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).",
	},
	{
		Method:      "CountFindSamplesBySupplierName",
		Verb:        registryVerbGet,
		Path:        "/find/sample/supplier-name/:id/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples by supplier name",
		Description: "Returns the number of samples whose supplier name exactly matches the given value, the count counterpart of /find/sample/supplier-name/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).",
	},
	{
		Method:      "CountFindSamplesByLibraryType",
		Verb:        registryVerbGet,
		Path:        "/find/sample/library-type/:id/count",
		PathParams:  []string{"id"},
		Query:       []string{},
		NewResult:   newResult[Count],
		Summary:     "Count samples by library type",
		Description: "Returns the number of distinct samples whose library type exactly matches the given value, the count counterpart of /find/sample/library-type/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).",
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

// fileTypeQueryParam is the optional file_type filter QueryParam shared by the
// iRODS list and count endpoints: a case-insensitive FILENAME-SUFFIX match on
// irods_file_name (a leading dot is stripped), not a real file-type column. The
// wording is surface-neutral so it reads correctly on both the list endpoints and
// their /count counterparts. It is not a pagination control, so it is declared on
// the count entries directly and appended to the list entries' pagination params
// by fetchAllPaginationWithFileTypeParams.
func fileTypeQueryParam() QueryParam {
	return QueryParam{
		Name:        "file_type",
		Type:        "string",
		Required:    false,
		Description: "when set, restricts the result to data objects whose iRODS file name ends in `.<file_type>`, matched case-insensitively with a single leading dot stripped (e.g. `cram`, `.CRAM` and `CRAM` are equivalent); it is a filename-suffix filter, not a real file-type column, so a valid but unmatched suffix yields an empty result (not an error) and the matching /count honours the same filter; an empty/whitespace value or one containing '%', '_' or '/' is rejected with a 400 bad_request; omit to return all file types",
	}
}

func deliverablesOnlyQueryParam() QueryParam {
	return QueryParam{
		Name:        "deliverables_only",
		Type:        "boolean",
		Required:    false,
		Description: "when true, keeps only deliverable data objects using the platform discriminator: Illumina iseq_flowcell.entity_type IN ('library','library_indexed'), Element/Ultima is_sequencing_control=0; this approximates the iRODS target=1 AVU, is NOT is_spiked, and is pass-through for PacBio/ONT because they have no discriminator",
	}
}

// roleQueryParam is the optional role filter QueryParam shared by the
// /studies/user list and count endpoints: a comma-separated OVERRIDE of the
// default study_users role set, matched exactly and case-insensitively. The
// wording is surface-neutral so it reads correctly on both the list endpoint and
// its /count counterpart. It is not a pagination control, so it is declared on the
// count entry directly and appended to the list entry's pagination params by
// searchPaginationWithRoleParams.
func roleQueryParam() QueryParam {
	return QueryParam{
		Name:        "role",
		Type:        "string",
		Required:    false,
		Description: "when set, a comma-separated list of study_users roles that OVERRIDES (replaces) the default set (owner, manager, data_access_contact), each matched exactly and case-insensitively (e.g. role=follower returns only follower rows, widening the result to followers; role=owner,manager returns owners and managers); omit to use the default role set",
	}
}

func studyUsersRoleQueryParam() QueryParam {
	return QueryParam{
		Name:        "role",
		Type:        "string",
		Required:    false,
		Description: "when set, a comma-separated list of study_users roles to include, matched exactly and case-insensitively: owner, manager, data_access_contact, follower, slf_manager, lab_manager, administrator; omit to return ALL roles present for the study",
	}
}

func exportFileTypeQueryParam() QueryParam {
	return QueryParam{
		Name:        "file_type",
		Type:        "string",
		Required:    false,
		Description: "when set, for file exports, restricts exported file rows by filename suffix on data objects whose iRODS file name ends in `.<file_type>`, matched case-insensitively with a single leading dot stripped (e.g. `cram`, `.CRAM` and `CRAM` are equivalent); a valid but unmatched suffix yields no file export rows; when set for products, only restricts the attached `irods_path` value and does not filter product rows or change `Total`; it is a filename-suffix match, not a real file-type column; an empty/whitespace value or one containing '%', '_' or '/' is rejected with a 400 bad_request; when omitted, file exports use the relationship default, currently CRAM for irods/files/sample-crams exports; when omitted for products, any file type can attach when an iRODS path column is requested",
	}
}

// searchPaginationWithRoleParams are the QueryParams for the /studies/user list
// endpoint: the search-style limit/offset pagination controls (default 100,
// maximum 1000) plus the optional role override filter. The list shares its single
// endpoint with the role-filtered variant (parameterised by role), so it documents
// both the pagination and the filter controls.
func searchPaginationWithRoleParams() []QueryParam {
	return append(searchPaginationParams(), roleQueryParam())
}

func fetchAllPaginationWithStudyUsersRoleParams() []QueryParam {
	return append(fetchAllPaginationParams(), studyUsersRoleQueryParam())
}

// fetchAllPaginationWithAddedWindowParams are the QueryParams for the
// samples-with-data list endpoint: the fetch-all limit/offset pagination controls
// plus the optional since/until [since, until) added-window filter. The list
// shares its single endpoint with the windowed variant (parameterised by
// since/until), so it documents both the pagination and the window controls.
func fetchAllPaginationWithAddedWindowParams() []QueryParam {
	return append(fetchAllPaginationParams(), addedWindowQueryParams("samples whose study-scoped data was added to iRODS")...)
}

// detailQueryParams are the QueryParams for the de-duplicated detail endpoints
// (/study/:id/detail and /run/:id/detail): the limit/offset controls that
// paginate the nested sample collection (the same fetch-all defaults as the
// list endpoints, so the X-Total-Count / X-Next-Offset sizing headers behave the
// same) plus the boolean lean switch that drops the heavy nested objects in
// favour of the flat id lists. The detail methods are not themselves paginated
// Queryer methods (they take no trailing limit/offset), so these are declared as
// plain query params on a non-paginated entry, like the windowed-count window
// params.
func detailQueryParams() []QueryParam {
	return append(fetchAllPaginationParams(), QueryParam{
		Name:        "lean",
		Type:        "boolean",
		Required:    false,
		Description: "when true, drops the heavy nested objects (library_details / samples / studies / study_details and the lookup tables) and returns only the top-level entity plus flat id lists, so the response is strictly smaller; defaults to false",
	})
}

// fetchAllPaginationWithFileTypeParams are the QueryParams for the iRODS list
// endpoints: the fetch-all limit/offset pagination controls plus file_type,
// order_by, and the optional created-time window.
func fetchAllPaginationWithFileTypeParams(scope string) []QueryParam {
	params := append(fetchAllPaginationParams(), fileTypeQueryParam(), deliverablesOnlyQueryParam(), QueryParam{
		Name:        "order_by",
		Type:        "string",
		Required:    false,
		Description: "optional iRODS row order; supported value created_desc orders by iRODS created time newest-first; omit to keep the default stable listing order",
	})
	params = append(params, addedWindowQueryParams(scope)...)

	return params
}

func latestDataPaginationParams() []QueryParam {
	return []QueryParam{
		{
			Name:        "limit",
			Type:        "integer",
			Required:    false,
			Description: "maximum number of newest rows to return; defaults to 10 and must not exceed 1000",
		},
		{
			Name:        "offset",
			Type:        "integer",
			Required:    false,
			Description: "number of leading newest rows to skip before returning results; defaults to 0",
		},
		fileTypeQueryParam(),
	}
}

func irodsCountQueryParams(scope string) []QueryParam {
	params := []QueryParam{fileTypeQueryParam(), deliverablesOnlyQueryParam()}
	params = append(params, addedWindowQueryParams(scope)...)

	return params
}

func runListingQueryParams() []QueryParam {
	params := append([]QueryParam{}, monthlyRunQueryParams()...)
	params = append(params,
		QueryParam{Name: "limit", Type: "integer", Description: "maximum number of rows to return; defaults to 100, maximum 1000"},
		QueryParam{Name: "cursor", Type: "string", Description: "keyset cursor: the composite id from the last row of the previous page"},
	)

	return params
}

func monthlyRunQueryParams() []QueryParam {
	return []QueryParam{
		{Name: "since", Type: "string", Description: "optional YYYY-MM-DD or RFC3339 inclusive lower bound over the platform's normalised run date basis"},
		{Name: "until", Type: "string", Description: "optional YYYY-MM-DD or RFC3339 exclusive upper bound over the platform's normalised run date basis"},
		{Name: "platform", Type: "string", Description: "optional repeatable platform filter: Illumina, PacBio, Elembio, Ultimagen or ONT"},
	}
}

func sequencingAggregateQueryParams() []QueryParam {
	return []QueryParam{
		{Name: "group_by", Type: "string", Required: true, Description: "required repeatable or comma-separated grouping keys: month, platform, manufacturer, programme, faculty_sponsor"},
		{Name: "unit", Type: "string", Required: true, Description: "required counted unit: runs, samples, or products"},
		{Name: "since", Type: "string", Description: "optional inclusive lower bound; YYYY-MM-DD/RFC3339 over run date basis for unit=runs and over iRODS created for samples/products"},
		{Name: "until", Type: "string", Description: "optional exclusive upper bound; YYYY-MM-DD/RFC3339 over run date basis for unit=runs and over iRODS created for samples/products"},
		{Name: "platform", Type: "string", Description: "optional repeatable platform filter: Illumina, PacBio, Elembio, Ultimagen or ONT"},
	}
}

func exportQueryParams() []QueryParam {
	params := exportPaginationParams()
	params = append(params,
		QueryParam{Name: "columns", Type: "string", Description: exportColumnsQueryParamDescription()},
		exportFileTypeQueryParam(),
		QueryParam{Name: "deliverables_only", Type: "boolean", Description: "when true, for file exports, restricts exported file rows to deliverable rows; for products, filters product rows using Illumina `iseq_flowcell.entity_type IN ('library','library_indexed')` and changes product rows and `Total` without depending on attached iRODS rows; when false, includes controls/sub-products; omit to use the relationship default, which is true for CRAM irods/files/sample-crams exports"},
		QueryParam{Name: "role", Type: "string", Description: "optional comma-separated study_users role filter for study_users-backed exports; users-of-study omits to return all roles present, studies-of-user omits to use owner, manager and data_access_contact"},
		QueryParam{Name: "qc", Type: "string", Description: "optional QC filter for product-backed exports: pass, fail, or pending"},
		QueryParam{Name: "library_type", Type: "string", Description: "optional exact library type filter for sample-backed exports"},
		QueryParam{Name: "organism", Type: "string", Description: "optional organism/common-name filter for sample-backed exports"},
		QueryParam{Name: "sort", Type: "string", Description: "optional iRODS export sort mode; supported value created-desc orders by iRODS created time newest-first"},
		QueryParam{Name: "order_by", Type: "string", Description: "alias for sort on iRODS exports; supported value created_desc"},
		QueryParam{Name: "since", Type: "string", Description: "RFC3339 lower bound for iRODS created-date exports (created >= since); supported for iRODS exports"},
		QueryParam{Name: "until", Type: "string", Description: "RFC3339 upper bound for iRODS created-date exports (created < until); requires since"},
		QueryParam{Name: "all", Type: "boolean", Description: "when true, emits the complete matching set rather than a bounded page"},
		QueryParam{Name: "cursor", Type: "string", Description: "opaque keyset cursor returned by a previous iRODS or products export page"},
		QueryParam{Name: "format", Type: "string", Description: "output rendering format metadata: tsv, csv or json; defaults to tsv"},
	)

	return params
}

func exportColumnsQueryParamDescription() string {
	var builder strings.Builder
	builder.WriteString("ordered comma-separated export columns to emit; omit to use the relationship default projection. Choices by child: ")
	for index, vocab := range ExportColumnVocabularies() {
		if index > 0 {
			builder.WriteString("; ")
		}
		builder.WriteString(exportColumnVocabularyLabel(vocab))
		builder.WriteString(" default ")
		builder.WriteString(strings.Join(vocab.Default, ","))
		builder.WriteString("; available ")
		builder.WriteString(strings.Join(exportColumnDescriptionNames(vocab.Columns), ","))
	}

	return builder.String()
}

func exportColumnVocabularyLabel(vocab ExportColumnVocabulary) string {
	if len(vocab.Aliases) == 0 {
		return vocab.Children
	}

	return vocab.Children + "/" + strings.Join(vocab.Aliases, "/")
}

func exportColumnDescriptionNames(columns []ExportColumnDescription) []string {
	names := make([]string, len(columns))
	for index, column := range columns {
		names[index] = column.Name
		if len(column.Aliases) > 0 {
			names[index] += " (alias: " + strings.Join(column.Aliases, ", ") + ")"
		}
	}

	return names
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

// exportPaginationParams are the limit/offset QueryParams for the generic export
// endpoint, whose default response is a bounded internal page rather than the
// fetch-all behaviour used by many list endpoints.
func exportPaginationParams() []QueryParam {
	return []QueryParam{
		{
			Name:        "limit",
			Type:        "integer",
			Required:    false,
			Description: "maximum rows for a bounded export page; defaults to the internal 1000-row bounded page; use all=true for the complete matching set",
		},
		{
			Name:        "offset",
			Type:        "integer",
			Required:    false,
			Description: "number of leading rows to skip for offset-backed export relationships; defaults to 0; use cursor to continue keyset-backed iRODS/products pages",
		},
	}
}

func sampleSearchPaginationParams() []QueryParam {
	return append(searchPaginationParams(), sampleSearchOptionQueryParams()...)
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

// addedWindowQueryParams are the optional since/until RFC3339 QueryParams shared
// by created-time windowed endpoints: a half-open [since, until) filter on the
// iRODS creation timestamp (the created column, never last_updated/last_run).
// The caller supplies scope-specific wording for the row or sample set being
// filtered.
func addedWindowQueryParams(scope string) []QueryParam {
	return []QueryParam{
		{
			Name:        "since",
			Type:        "string",
			Required:    false,
			Description: "RFC3339 timestamp; when set, restricts the result to " + scope + " at or after this instant (the lower bound of the half-open [since, until) window; created >= since, inclusive), filtering on the iRODS creation timestamp (the data-added time) and never on last_updated/last_run; omit for an all-time result",
		},
		{
			Name:        "until",
			Type:        "string",
			Required:    false,
			Description: "RFC3339 timestamp; when set with since, the upper bound of the half-open window (created < until, exclusive); optional and open-ended when omitted; only meaningful with since, so an until supplied without a since is rejected with a 400 bad_request rather than silently ignored",
		},
	}
}

func sampleSearchOptionQueryParams() []QueryParam {
	return []QueryParam{
		{
			Name:        "words",
			Type:        "boolean",
			Required:    false,
			Description: "when true, uses the opt-in separator-agnostic word-prefix mode over name, supplier_name, common_name and donor_id; omit for the default literal whole-value prefix over those four fields",
		},
		{
			Name:        "organism",
			Type:        "string",
			Required:    false,
			Description: "exact sample filter over common_name after resolving whole-word organism membership; AND-combines with term and other filters and is exempt from the free-text 3-character minimum",
		},
		{
			Name:        "library_type",
			Type:        "string",
			Required:    false,
			Description: "exact sample filter over library_samples.pipeline_id_lims; AND-combines with term and other filters and is exempt from the free-text 3-character minimum",
		},
		{
			Name:        "qc",
			Type:        "string",
			Required:    false,
			Description: "sample-search QC filter: pass, fail or pending using the per-sample qc.go roll-up (fail>pending>pass); this differs from export QC, which filters raw per-product qc rows",
		},
		deliverablesOnlyQueryParam(),
	}
}

func newResult[T any]() any {
	return new(T)
}

func newSliceResult[T any]() any {
	result := []T{}

	return &result
}
