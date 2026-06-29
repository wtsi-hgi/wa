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
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Canonical baseline phases (spec F1, closed enum, asserted verbatim). The
// ladder is monotonic and most-advanced-wins: registered (linked, no products)
// -> sequenced (has product-metrics) -> delivered (has iRODS rows).
const (
	// baselineRegistered is the baseline phase for a sample that is linked via
	// library_samples but has no product-metrics on any platform (includes ONT,
	// whose oseq_flowcell carries no products/iRODS).
	baselineRegistered = "registered"
	// baselineSequenced is the baseline phase for a sample that has
	// product-metrics on at least one platform but no iRODS rows yet.
	baselineSequenced = "sequenced"
	// baselineDelivered is the baseline phase for a sample that has at least one
	// row in seq_product_irods_locations_mirror (its data is in iRODS).
	baselineDelivered = "delivered"
)

// qcNotTracked is the per-sample QC roll-up verdict for a sample with NO
// product-metrics on any platform: QC is untracked rather than pending (a
// pending verdict requires a product whose qc is undecided). ONT (no products)
// and a registered-only sample are both not_tracked, never a false zero
// (HARD REQ 11). It is distinct from qcPass/qcFail/qcPending (qc.go), which map
// a single product's overall qc value.
const qcNotTracked = "not_tracked"

// sampleQCRollupSQL aggregates the overall qc column across every platform's
// product-metrics mirror for one sample (sample-wide, not study-scoped), so the
// per-sample QC roll-up is one indexed pass rather than N per-product reads. The
// inner UNION ALL gathers the sample's overall qc value from each mirror over
// each mirror's id_sample_tmp index; the outer aggregate reports the product
// count (whether the sample is sequenced at all), the pending count (products
// whose qc is SQL NULL) and MIN(qc) (0 when any product fails, since SQL MIN
// ignores NULLs). The caller maps these to qcFail > qcPending > qcPass, or
// qcNotTracked when the product count is zero.
var sampleQCRollupSQL = `SELECT COUNT(*), SUM(CASE WHEN qc IS NULL THEN 1 ELSE 0 END), MIN(qc) FROM (` +
	sampleProductMetricsQCUnion() + `) AS sample_products`

// sampleIRODSDeliverySQL is the single sample-scoped iRODS aggregate that yields
// the data-object count (whether the sample is delivered) and the earliest
// created (delivered_at) in one indexed pass over the id_sample_tmp index.
// MIN(created) is NULL when the sample has no iRODS rows, which the caller maps
// to an empty delivered_at and a not-delivered phase.
const sampleIRODSDeliverySQL = `SELECT COUNT(*), MIN(created) FROM seq_product_irods_locations_mirror WHERE id_sample_tmp = ?`

// illuminaRunStatusTimelineSQL reads one Illumina run's within-sequencing status
// timeline: every iseq_run_status_mirror row for the run joined to
// iseq_run_status_dict_mirror for its description (the phase), ordered strictly
// by date then id_run_status (the latter only to make equal-dated rows
// deterministic). The dict description is passed through verbatim as the open
// status vocabulary, and rows are returned faithfully -- recurrences, on-hold,
// cancelled and stopped-early are NOT deduplicated, reordered or forced
// monotonic. iscurrent is intentionally NOT selected: current is derived from
// the latest date, never read from the source flag.
const illuminaRunStatusTimelineSQL = `SELECT s.date, d.description FROM iseq_run_status_mirror AS s ` +
	`JOIN iseq_run_status_dict_mirror AS d ON d.id_run_status_dict = s.id_run_status_dict ` +
	`WHERE s.id_run = ? ORDER BY s.date, s.id_run_status`

// milestoneColumns is the canonical, ordered set of the 9 wet-lab/sequencing
// milestone datetime columns in seq_ops_tracking_per_sample_mirror (spec F3, a
// closed enum reported verbatim and in this exact order). The progress endpoint
// emits only the REACHED (non-NULL) ones in this order, and current_milestone is
// the last reached one (its successor in this order being NULL).
var milestoneColumns = []string{
	"manifest_created",
	"manifest_uploaded",
	"labware_received",
	"order_made",
	"working_dilution",
	"library_start",
	"library_complete",
	"sequencing_run_start",
	"sequencing_qc_complete",
}

// sampleTrackingMilestonesSQL reads one sample's 9 milestone datetime columns
// from seq_ops_tracking_per_sample_mirror by its id_sample_lims (the tracking
// mirror's sample key, indexed). The columns are selected in canonical
// milestoneColumns order; a missing row means the sample is outside the tracking
// window (detailed_timeline=false), distinct from a present row whose milestone
// columns are all NULL.
var sampleTrackingMilestonesSQL = `SELECT ` + strings.Join(milestoneColumns, ", ") +
	` FROM seq_ops_tracking_per_sample_mirror WHERE id_sample_lims = ? LIMIT 1`

// sampleIlluminaRunsSQL lists the distinct Illumina NPG run ids the sample
// appears on, from iseq_product_metrics_mirror keyed on id_sample_tmp (the same
// run source as LanesForSample), ordered for a deterministic runs slice.
const sampleIlluminaRunsSQL = `SELECT DISTINCT id_run FROM iseq_product_metrics_mirror WHERE id_sample_tmp = ? ORDER BY id_run`

// samplePacBioWellMetricsSQL reads the PacBio run/well status timeline source for
// one sample: every pac_bio_run_well_metrics_mirror row reachable from the
// sample's pac_bio_product_metrics_mirror rows through the shared
// id_pac_bio_rw_metrics_tmp (the only per-run linkage the mirrors carry for
// PacBio). It returns the native run_status / well_status (open vocabulary,
// verbatim) and the four dated lifecycle columns; the caller turns each non-NULL
// date into a runStatusRawEvent and feeds them through normalizeRunStatusTimeline.
// DISTINCT collapses the (sample, well) fan-out so each well contributes one
// timeline source. Ordered by run start then well for a deterministic runs slice.
const samplePacBioWellMetricsSQL = `SELECT DISTINCT w.run_status, w.well_status, w.run_start, w.run_complete, w.well_complete, w.qc_seq_date ` +
	`FROM pac_bio_run_well_metrics_mirror AS w ` +
	`JOIN pac_bio_product_metrics_mirror AS p ON p.id_pac_bio_rw_metrics_tmp = w.id_pac_bio_rw_metrics_tmp ` +
	`WHERE p.id_sample_tmp = ? ORDER BY w.run_start, w.well_label`

// sampleEseqLaneMetricsSQL reads the Elembio within-sequencing status timeline
// source for one sample: every eseq_run_lane_metrics_mirror row reachable from the
// sample's eseq_product_metrics_mirror rows through the shared id_run -- the
// authoritative join the real ml_warehouse declares (eseq_product_metrics.id_run
// -> eseq_run_lane_metrics.id_run), exactly mirroring the PacBio path's
// id_pac_bio_rw_metrics_tmp join but keyed on id_run. Elembio carries NO native
// run_status string (the lane table expresses status as dated lifecycle columns),
// so the caller labels each non-NULL dated column with its canonical lifecycle
// phase name and feeds them through the shared normalizeRunStatusTimeline. DISTINCT
// collapses the (sample, lane) fan-out so each id_run contributes one timeline
// source; ordered by id_run then run_started for a deterministic runs slice.
const sampleEseqLaneMetricsSQL = `SELECT DISTINCT l.run_started, l.run_complete ` +
	`FROM eseq_run_lane_metrics_mirror AS l ` +
	`JOIN eseq_product_metrics_mirror AS p ON p.id_run = l.id_run ` +
	`WHERE p.id_sample_tmp = ? ORDER BY l.id_run, l.run_started`

// sampleUseqRunMetricsSQL reads the Ultimagen within-sequencing status timeline
// source for one sample: every useq_run_metrics_mirror row reachable from the
// sample's useq_product_metrics_mirror rows through the shared id_run
// (useq_product_metrics.id_run -> useq_run_metrics.id_run, where id_run is the
// run-metrics primary key), the same id_run-keyed model as the Elembio path.
// Ultimagen carries a native run_status string, passed through verbatim (open
// vocabulary) for both run-level dated columns. DISTINCT collapses the fan-out so
// each id_run contributes one timeline source; ordered by id_run then run_start.
const sampleUseqRunMetricsSQL = `SELECT DISTINCT r.run_status, r.run_start, r.run_complete ` +
	`FROM useq_run_metrics_mirror AS r ` +
	`JOIN useq_product_metrics_mirror AS p ON p.id_run = r.id_run ` +
	`WHERE p.id_sample_tmp = ? ORDER BY r.id_run, r.run_start`

// Elembio lane-lifecycle phase labels (spec F2 open vocabulary). Elembio's
// eseq_run_lane_metrics has no native run_status column -- its within-sequencing
// status is expressed as dated lifecycle columns -- so each dated column's
// canonical name is its phase, derived from the source column the same way PacBio
// labels its run-level vs well-level dates. They are passed through
// normalizeRunStatusTimeline verbatim (never normalized against a closed list).
const (
	elembioRunStartedPhase  = "run started"
	elembioRunCompletePhase = "run complete"
)

// sampleProgressFeedingTables are the sync tables whose oldest last_run defines a
// SampleProgress's cache_synced_at (spec F3): the tracking mirror (milestones),
// iseq_run_status (the run-status timeline), every platform's product-metrics
// mirror (the baseline phase / per-run linkage) and the iRODS locations mirror
// (delivery). cache_synced_at is the OLDEST last_run among those that have ever
// synced, distinct from any data timestamp (the freshness caveat).
var sampleProgressFeedingTables = []string{
	syncTableSeqOpsTrackingPerSample,
	syncTableIseqRunStatus,
	syncTableIseqProductMetrics,
	syncTablePacBioProductMetrics,
	syncTableEseqProductMetrics,
	syncTableUseqProductMetrics,
	syncTableSeqProductIRODSLocations,
}

// trackingTimelineReason is the non-empty timeline_reason reported when a sample
// is absent from seq_ops_tracking_per_sample_mirror: the detailed milestone
// timeline is simply unavailable (less detail), never an error.
const trackingTimelineReason = "not in tracking window"

// statusBreakdownDistinctCacheSQL is the ONE grouped query for the distinct-sample
// partition of the study status breakdown (spec F4): a single pass over the shared
// Phase 2 membership join (studyDataMembershipJoin) that buckets each linked sample
// by its most-advanced phase via conditional aggregation. with_data counts the
// distinct samples with >=1 study-scoped iRODS row (studyScopedIRODSExists, the same
// predicate StudyOverview / SamplesWithData use); sequenced_no_data counts those
// with NO study-scoped iRODS row but >=1 study-scoped product-metrics row
// (studyScopedProductMetricsExists); registered counts the rest (no products,
// including ONT). Each distinct sample satisfies exactly one CASE, so the three
// COUNT(DISTINCT ...) buckets sum to samples_total -- it is the SAME partition
// StudyOverview computes (precedence with_data > sequenced_no_data > registered),
// expressed as one query rather than three counts. No id_lims filter, matching the
// canonical study membership (CountSamplesForStudy), so the buckets stay a partition
// of the study's linked samples.
var statusBreakdownDistinctCacheSQL = `SELECT ` +
	`COUNT(DISTINCT CASE WHEN EXISTS (` + studyScopedIRODSExists("") + `) THEN sample_mirror.id_sample_tmp END), ` +
	`COUNT(DISTINCT CASE WHEN NOT EXISTS (` + studyScopedIRODSExists("") + `) AND EXISTS (` + studyScopedProductMetricsExists() + `) THEN sample_mirror.id_sample_tmp END), ` +
	`COUNT(DISTINCT CASE WHEN NOT EXISTS (` + studyScopedIRODSExists("") + `) AND NOT EXISTS (` + studyScopedProductMetricsExists() + `) THEN sample_mirror.id_sample_tmp END) ` +
	`FROM ` + studyDataMembershipJoin + ` WHERE library_samples.id_study_lims = ?`

// countSamplesWithDetailedTimelineCacheSQL counts the study's linked samples that
// are also present in the seq_ops_tracking_per_sample mirror (spec F4
// with_detailed_timeline). It reuses the shared membership join and joins the
// tracking mirror by its sample key (sample_mirror.id_sample_lims =
// seq_ops_tracking_per_sample_mirror.id_sample_lims) via a correlated EXISTS, so it
// counts distinct SAMPLES present in tracking, never tracking rows.
var countSamplesWithDetailedTimelineCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM ` + studyDataMembershipJoin +
	` WHERE library_samples.id_study_lims = ? AND EXISTS (` +
	`SELECT 1 FROM seq_ops_tracking_per_sample_mirror t WHERE t.id_sample_lims = sample_mirror.id_sample_lims)`

// statusBreakdownFeedingTables are the sync tables whose oldest last_run defines a
// StatusBreakdown's cache_synced_at (spec F4): the study + sample identity tables,
// every platform's product-metrics mirror and the iRODS locations mirror (the two
// partitions) plus the tracking mirror (with_detailed_timeline). cache_synced_at is
// the OLDEST last_run among those that have ever synced, distinct from any data
// timestamp (the freshness caveat).
var statusBreakdownFeedingTables = []string{
	syncTableStudy,
	syncTableSample,
	syncTableIseqProductMetrics,
	syncTablePacBioProductMetrics,
	syncTableEseqProductMetrics,
	syncTableUseqProductMetrics,
	syncTableSeqProductIRODSLocations,
	syncTableSeqOpsTrackingPerSample,
}

// statusBreakdownPlatformArm pairs one platform's product-metrics mirror with its
// product-id column, so the per-platform partition can both establish the
// platform's membership and test per-platform delivery (the shared product id links
// an iRODS row to that platform's products).
type statusBreakdownPlatformArm struct {
	platform  string
	table     string
	productID string
}

// statusBreakdownProductPlatformArms are the four product-bearing platforms of the
// per-platform partition, in platformCanonicalOrder. Each platform's sample set is
// its product-metrics rows in the study, and a (sample, platform) is delivered when
// the sample has a study-scoped iRODS row whose id_iseq_product matches one of that
// sample's products on that platform (the A2 linkage: iseq via id_iseq_product,
// pacbio via id_pac_bio_product, eseq via id_eseq_product, useq via id_useq_product).
var statusBreakdownProductPlatformArms = []statusBreakdownPlatformArm{
	{platformIllumina, "iseq_product_metrics_mirror", "id_iseq_product"},
	{platformPacBio, "pac_bio_product_metrics_mirror", "id_pac_bio_product"},
	{platformElembio, "eseq_product_metrics_mirror", "id_eseq_product"},
	{platformUltimagen, "useq_product_metrics_mirror", "id_useq_product"},
}

// sampleProductMetricsQCUnion is the UNION ALL of the overall qc column across
// every platform's product-metrics mirror for one sample, keyed on
// id_sample_tmp (sample-wide). Each arm binds the same id_sample_tmp once, so
// the roll-up spans all platforms a sample was sequenced on. ONT (oseq_flowcell)
// carries no products and is intentionally absent, so an ONT-only sample yields
// zero rows here (qcNotTracked).
func sampleProductMetricsQCUnion() string {
	arm := func(table string) string {
		return `SELECT qc FROM ` + table + ` WHERE id_sample_tmp = ?`
	}

	return strings.Join([]string{
		arm("iseq_product_metrics_mirror"),
		arm("pac_bio_product_metrics_mirror"),
		arm("eseq_product_metrics_mirror"),
		arm("useq_product_metrics_mirror"),
	}, " UNION ALL ")
}

// statusBreakdownPerPlatformSQL is the ONE grouped query for the per-platform
// partition of the study status breakdown (spec F4). Its inner SELECT fans a sample
// out to one row per platform it spans, tagged with that platform's bucket, and the
// outer GROUP BY platform sums those rows into each platform's ladder. Because a
// multi-platform sample contributes a row under EACH of its platforms, the grand
// total across platforms may exceed samples_total (the two-denominator decision).
// Within a platform the buckets sum to that platform's distinct sample count.
//
// Each product-platform arm selects the distinct samples with that platform's
// product-metrics in the study, tagging the bucket as with_data when the sample has
// a study-scoped iRODS row joined to ANY of its products on that platform (so the
// bucket is per (sample, platform), correct even when a sample has both a delivered
// and an undelivered product on the platform), else sequenced_no_data. The ONT arm
// selects the study's oseq_flowcell samples, always tagged registered (ONT has no
// product-metrics or iRODS). The platform name is a canonical literal per arm (never
// the iRODS seq_platform_name string), matching platformsForStudySamplesSQL.
func statusBreakdownPerPlatformSQL() string {
	productArm := func(arm statusBreakdownPlatformArm) string {
		delivered := `EXISTS (SELECT 1 FROM seq_product_irods_locations_mirror spi ` +
			`INNER JOIN ` + arm.table + ` pi ON pi.` + arm.productID + ` = spi.id_iseq_product ` +
			`WHERE spi.id_study_lims = pm.id_study_lims AND pi.id_sample_tmp = pm.id_sample_tmp)`

		return `SELECT DISTINCT pm.id_sample_tmp, '` + arm.platform + `' AS platform, ` +
			`CASE WHEN ` + delivered + ` THEN 'with_data' ELSE 'sequenced_no_data' END AS bucket ` +
			`FROM ` + arm.table + ` pm WHERE pm.id_study_lims = ?`
	}

	arms := make([]string, 0, len(statusBreakdownProductPlatformArms)+1)
	for _, arm := range statusBreakdownProductPlatformArms {
		arms = append(arms, productArm(arm))
	}
	arms = append(arms, `SELECT DISTINCT o.id_sample_tmp, '`+platformONT+`' AS platform, 'registered' AS bucket `+
		`FROM oseq_flowcell_mirror o WHERE o.id_study_lims = ?`)

	return `SELECT platform, ` +
		`SUM(CASE WHEN bucket = 'with_data' THEN 1 ELSE 0 END), ` +
		`SUM(CASE WHEN bucket = 'sequenced_no_data' THEN 1 ELSE 0 END), ` +
		`SUM(CASE WHEN bucket = 'registered' THEN 1 ELSE 0 END) ` +
		`FROM (` + strings.Join(arms, " UNION ALL ") + `) AS per_platform_samples GROUP BY platform`
}

// runStatusRawEvent is one un-normalized status transition (a phase and the
// instant it was entered) feeding normalizeRunStatusTimeline. It is the
// platform-agnostic intermediate every platform's reader produces, so the
// normalizer (durations, derived current, faithful ordering) lives in one place.
type runStatusRawEvent struct {
	// Phase is the native status description, passed through verbatim (open
	// vocabulary; never normalized against a closed list).
	Phase string
	// EnteredAt is when the phase was entered, in source order (already sorted by
	// the reader; the normalizer does not re-sort).
	EnteredAt time.Time
}

// illuminaRunStatusEvents reads one Illumina run's status rows joined to the dict,
// ordered by date, as the platform-agnostic raw events the normalizer consumes.
// The dict description is the phase (open vocabulary, verbatim); the stored date
// is re-parsed and the events stay in date order (faithful, not deduplicated).
func (c *Client) illuminaRunStatusEvents(ctx context.Context, idRun int) ([]runStatusRawEvent, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, illuminaRunStatusTimelineSQL, idRun)
	if err != nil {
		return nil, fmt.Errorf("%w: query illumina run status timeline: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	events := make([]runStatusRawEvent, 0)
	for rows.Next() {
		var (
			dateRaw any
			phase   string
		)
		if err = rows.Scan(&dateRaw, &phase); err != nil {
			return nil, fmt.Errorf("%w: scan illumina run status timeline: %w", ErrUpstreamImpaired, err)
		}

		entered, parseErr := parseSyncTimeValue(dateRaw)
		if parseErr != nil {
			return nil, fmt.Errorf("mlwh: parse illumina run status date: %w", parseErr)
		}

		events = append(events, runStatusRawEvent{Phase: phase, EnteredAt: entered})
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query illumina run status timeline: %w", ErrUpstreamImpaired, err)
	}

	return events, nil
}

// pacBioWellEvents turns one PacBio well's status+dates into date-ordered raw
// status events the normalizer consumes: one event per non-NULL dated lifecycle
// column, labelled with the native run_status (the run-level run_start /
// run_complete dates) or well_status (the well-level well_complete / qc_seq_date
// dates), passed through verbatim. The events are sorted by their date so the
// normalizer's durations and derived-current are correct regardless of column
// order; NULL dates are skipped.
func pacBioWellEvents(runStatus, wellStatus string, runStart, runComplete, wellComplete, qcSeqDate any) ([]runStatusRawEvent, error) {
	dated := []struct {
		phase string
		raw   any
	}{
		{runStatus, runStart},
		{runStatus, runComplete},
		{wellStatus, wellComplete},
		{wellStatus, qcSeqDate},
	}

	events := make([]runStatusRawEvent, 0, len(dated))
	for _, entry := range dated {
		if entry.raw == nil {
			continue
		}

		entered, err := parseSyncTimeValue(entry.raw)
		if err != nil {
			return nil, fmt.Errorf("mlwh: parse pacbio well metrics date: %w", err)
		}

		events = append(events, runStatusRawEvent{Phase: entry.phase, EnteredAt: entered})
	}

	slices.SortStableFunc(events, func(a, b runStatusRawEvent) int {
		return a.EnteredAt.Compare(b.EnteredAt)
	})

	return events, nil
}

// eseqLaneEvents turns one Elembio lane's dated lifecycle columns into date-ordered
// raw status events the normalizer consumes: one event per non-NULL dated column,
// labelled with that column's canonical lifecycle phase (Elembio has no native
// run_status string, so the column semantics ARE the phase, passed through
// verbatim). It is the Elembio analogue of pacBioWellEvents -- the events are
// sorted by their date so the normalizer's durations and derived-current are
// correct regardless of column order, and NULL dates are skipped.
func eseqLaneEvents(runStarted, runComplete any) ([]runStatusRawEvent, error) {
	dated := []struct {
		phase string
		raw   any
	}{
		{elembioRunStartedPhase, runStarted},
		{elembioRunCompletePhase, runComplete},
	}

	events := make([]runStatusRawEvent, 0, len(dated))
	for _, entry := range dated {
		if entry.raw == nil {
			continue
		}

		entered, err := parseSyncTimeValue(entry.raw)
		if err != nil {
			return nil, fmt.Errorf("mlwh: parse eseq lane metrics date: %w", err)
		}

		events = append(events, runStatusRawEvent{Phase: entry.phase, EnteredAt: entered})
	}

	slices.SortStableFunc(events, func(a, b runStatusRawEvent) int {
		return a.EnteredAt.Compare(b.EnteredAt)
	})

	return events, nil
}

// useqRunEvents turns one Ultimagen run's dated lifecycle columns into date-ordered
// raw status events the normalizer consumes: one event per non-NULL dated column
// (run_start, run_complete), each labelled with the run's native run_status string
// passed through verbatim (open vocabulary). It is the Ultimagen analogue of
// pacBioWellEvents -- events are sorted by their date and NULL dates are skipped.
func useqRunEvents(runStatus string, runStart, runComplete any) ([]runStatusRawEvent, error) {
	dated := []struct {
		raw any
	}{
		{runStart},
		{runComplete},
	}

	events := make([]runStatusRawEvent, 0, len(dated))
	for _, entry := range dated {
		if entry.raw == nil {
			continue
		}

		entered, err := parseSyncTimeValue(entry.raw)
		if err != nil {
			return nil, fmt.Errorf("mlwh: parse useq run metrics date: %w", err)
		}

		events = append(events, runStatusRawEvent{Phase: runStatus, EnteredAt: entered})
	}

	slices.SortStableFunc(events, func(a, b runStatusRawEvent) int {
		return a.EnteredAt.Compare(b.EnteredAt)
	})

	return events, nil
}

// normalizeRunStatusTimeline turns a platform's date-ordered raw status events
// into the single shared RunStatusTimeline (spec F2): each event's entered_at is
// its source date as UTC RFC3339, each event's duration is the ISO8601-style
// delta to the NEXT event (empty for the last/open event), and current is the
// phase of the LAST (latest-date) event -- DERIVED, never the source iscurrent.
// Events are emitted in the given order with no deduplication, reordering or
// monotonic forcing, so recurrences, on-hold, cancelled and stopped-early are
// preserved faithfully; the phase is passed through verbatim, so an unknown/new
// status is accepted unchanged (open vocabulary). It is platform-agnostic so
// every platform (and both F2 and F3) share one normalization, and the duration
// uses the same iso8601Duration helper as F3's Milestone.duration_to_next. An
// empty event slice yields an empty timeline with no current (e.g. an Illumina
// run with no status rows yet).
func normalizeRunStatusTimeline(idRun int, platform string, raw []runStatusRawEvent) RunStatusTimeline {
	events := make([]RunStatusEvent, len(raw))
	for i, event := range raw {
		duration := ""
		if i+1 < len(raw) {
			duration = iso8601Duration(event.EnteredAt, raw[i+1].EnteredAt)
		}

		events[i] = RunStatusEvent{
			Phase:     event.Phase,
			EnteredAt: event.EnteredAt.UTC().Format(utcRFC3339Layout),
			Duration:  duration,
		}
	}

	current := ""
	if len(raw) > 0 {
		current = raw[len(raw)-1].Phase
	}

	return RunStatusTimeline{
		IDRun:    idRun,
		Platform: platform,
		Events:   events,
		Current:  current,
	}
}

// iso8601Duration formats the non-negative span from earlier to later as an
// ISO8601 duration (PnDTnHnMnS), the shared duration format for a run-status
// event's duration-to-next (spec F2) and a milestone's duration_to_next (spec
// F3), so the two never drift. Whole days are emitted as the date part (P1D) and
// the remainder as the time part (T...); a sub-day span has no date part (PT1H30M)
// and a zero or negative span (out-of-order or equal timestamps) renders PT0S, so
// a faithfully-preserved non-monotonic timeline still produces a stable string.
func iso8601Duration(earlier, later time.Time) string {
	span := later.Sub(earlier)
	if span <= 0 {
		return "PT0S"
	}

	days := int64(span / (24 * time.Hour))
	rest := span - time.Duration(days)*24*time.Hour
	hours := int64(rest / time.Hour)
	rest -= time.Duration(hours) * time.Hour
	minutes := int64(rest / time.Minute)
	rest -= time.Duration(minutes) * time.Minute
	seconds := int64(rest / time.Second)

	var b strings.Builder
	b.WriteByte('P')
	if days > 0 {
		b.WriteString(strconv.FormatInt(days, 10))
		b.WriteByte('D')
	}
	if hours > 0 || minutes > 0 || seconds > 0 {
		b.WriteByte('T')
		writeISO8601TimePart(&b, hours, 'H')
		writeISO8601TimePart(&b, minutes, 'M')
		writeISO8601TimePart(&b, seconds, 'S')
	}

	return b.String()
}

// writeISO8601TimePart appends one non-zero ISO8601 time component (e.g. 1H) to
// b, skipping zero components so the duration stays compact (PT1H, not PT1H0M0S).
func writeISO8601TimePart(b *strings.Builder, value int64, unit byte) {
	if value == 0 {
		return
	}

	b.WriteString(strconv.FormatInt(value, 10))
	b.WriteByte(unit)
}

// reachedMilestone is one reached milestone: its canonical name and the instant
// it was reached, in canonical milestoneColumns order.
type reachedMilestone struct {
	name      string
	reachedAt time.Time
}

// sampleTrackingMilestones reads the sample's tracking row by id_sample_lims and
// returns the reached (non-NULL) milestones in canonical order plus whether a row
// existed at all. A present row with every milestone NULL returns an empty slice
// with present=true (detailed_timeline=true, no milestones), distinct from an
// absent row (present=false).
func (c *Client) sampleTrackingMilestones(ctx context.Context, idSampleLims string) ([]reachedMilestone, bool, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, false, fmt.Errorf("mlwh: cache reader not configured")
	}

	values := make([]any, len(milestoneColumns))
	scanTargets := make([]any, len(milestoneColumns))
	for i := range values {
		scanTargets[i] = &values[i]
	}

	err := db.QueryRowContext(ctx, sampleTrackingMilestonesSQL, idSampleLims).Scan(scanTargets...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%w: query sample tracking milestones: %w", ErrUpstreamImpaired, err)
	}

	reached := make([]reachedMilestone, 0, len(milestoneColumns))
	for i, raw := range values {
		if raw == nil {
			continue
		}

		reachedAt, parseErr := parseSyncTimeValue(raw)
		if parseErr != nil {
			return nil, false, fmt.Errorf("mlwh: parse milestone %s: %w", milestoneColumns[i], parseErr)
		}

		reached = append(reached, reachedMilestone{name: milestoneColumns[i], reachedAt: reachedAt})
	}

	return reached, true, nil
}

// buildMilestones turns the reached milestones (already in canonical order) into
// the ordered Milestone slice: each reached_at is the milestone's instant as UTC
// RFC3339, and each duration_to_next is the iso8601Duration to the NEXT reached
// milestone (the shared duration helper, so it never drifts from a run event's
// duration), empty for the last (open/current) milestone. The open milestone's
// timestamp is returned unsubtracted for the caller to compute elapsed (no server
// "now" subtraction).
func buildMilestones(reached []reachedMilestone) []Milestone {
	milestones := make([]Milestone, len(reached))
	for i, milestone := range reached {
		duration := ""
		if i+1 < len(reached) {
			duration = iso8601Duration(milestone.reachedAt, reached[i+1].reachedAt)
		}

		milestones[i] = Milestone{
			Name:           milestone.name,
			ReachedAt:      milestone.reachedAt.UTC().Format(utcRFC3339Layout),
			DurationToNext: duration,
		}
	}

	return milestones
}

// sampleProductMetricsPlatformsSQL returns the distinct platforms one sample
// spans, sample-wide (not study-scoped): one (platform) row per platform the
// sample has products on, plus ONT from oseq_flowcell membership. The canonical
// platform name is a literal per arm (never the iRODS seq_platform_name string),
// matching platformsForStudySamplesSQL. Each arm binds the same id_sample_tmp
// once.
func sampleProductMetricsPlatformsSQL() string {
	arm := func(table, platform string) string {
		return `SELECT DISTINCT '` + platform + `' AS platform FROM ` + table + ` WHERE id_sample_tmp = ?`
	}

	return strings.Join([]string{
		arm("iseq_product_metrics_mirror", platformIllumina),
		arm("pac_bio_product_metrics_mirror", platformPacBio),
		arm("eseq_product_metrics_mirror", platformElembio),
		arm("useq_product_metrics_mirror", platformUltimagen),
		arm("oseq_flowcell_mirror", platformONT),
	}, " UNION ALL ")
}

// baselinePhaseFor resolves the most-advanced baseline phase from the two
// coverage signals: any iRODS row makes the sample delivered (the most-advanced
// phase), else any product-metrics row makes it sequenced, else it is registered
// (linked only, incl. ONT). A delivered sample necessarily has products too, so
// delivered takes precedence over sequenced.
func baselinePhaseFor(productCount int, delivered bool) string {
	switch {
	case delivered:
		return baselineDelivered
	case productCount > 0:
		return baselineSequenced
	default:
		return baselineRegistered
	}
}

// rollUpSampleQC maps the QC aggregate (product count, pending count, MIN(qc))
// to the per-sample verdict: qcNotTracked when there are no products, else
// qcFail when any product fails (MIN(qc) == 0, since SQL MIN ignores NULLs),
// else qcPending when any product's qc is NULL, else qcPass.
func rollUpSampleQC(productCount int, pending, minQC sql.NullInt64) string {
	switch {
	case productCount == 0:
		return qcNotTracked
	case minQC.Valid && minQC.Int64 == 0:
		return qcFail
	case pending.Valid && pending.Int64 > 0:
		return qcPending
	default:
		return qcPass
	}
}

// sampleBaseline is the always-derivable P0 baseline for one sample (spec F1):
// the coarse phase, the per-sample QC roll-up, the delivery timestamp, and the
// platforms the sample spans. It is the reusable derivation the unified progress
// endpoint (F3) embeds in SampleProgress, so its fields mirror the SampleProgress
// baseline fields (BaselinePhase / QC / DeliveredAt / Platforms). The derivation
// is sample-centric and sample-wide (keyed on id_sample_tmp, not study-scoped):
// a multi-platform sample's phase is the most-advanced across all its platforms
// and its Platforms slice lists all of them in canonical order.
type sampleBaseline struct {
	// BaselinePhase is one of baselineRegistered/baselineSequenced/baselineDelivered.
	BaselinePhase string
	// QC is the per-sample roll-up: qcFail if any product fails, else qcPending
	// if any is pending, else qcPass; qcNotTracked when the sample has no
	// products (incl. ONT).
	QC string
	// DeliveredAt is the earliest iRODS created (UTC RFC3339), empty unless the
	// sample is delivered.
	DeliveredAt string
	// Platforms lists the platforms the sample spans, in platformCanonicalOrder;
	// ["ONT"] for an ONT-only sample, empty for a registered-only sample.
	Platforms []string
}

// deriveSampleBaseline computes the always-available P0 baseline (spec F1) for
// one sample, identified by its internal surrogate key (id_sample_tmp). It is
// the reusable derivation the unified progress endpoint (F3) embeds: it resolves
// the most-advanced phase across the sample's platforms via the platform-coverage
// union (product-metrics + iRODS mirrors for Illumina/PacBio/Elembio/Ultimagen)
// plus oseq_flowcell for ONT, rolls up QC over the overall qc column (fail >
// pending > pass; not_tracked when the sample has no products, incl. ONT), and
// reports delivered_at as the earliest iRODS created when delivered. It uses
// single indexed aggregates (no N+1 per product). ONT and registered-only
// samples report not_tracked QC and no delivered_at rather than a false zero
// (HARD REQ 11).
func (c *Client) deriveSampleBaseline(ctx context.Context, idSampleTmp int64) (sampleBaseline, error) {
	platforms, err := c.sampleBaselinePlatforms(ctx, idSampleTmp)
	if err != nil {
		return sampleBaseline{}, err
	}

	qc, productCount, err := c.sampleBaselineQC(ctx, idSampleTmp)
	if err != nil {
		return sampleBaseline{}, err
	}

	deliveredAt, delivered, err := c.sampleBaselineDelivery(ctx, idSampleTmp)
	if err != nil {
		return sampleBaseline{}, err
	}

	return sampleBaseline{
		BaselinePhase: baselinePhaseFor(productCount, delivered),
		QC:            qc,
		DeliveredAt:   deliveredAt,
		Platforms:     platforms,
	}, nil
}

// sampleBaselinePlatforms returns the platforms the sample spans, in
// platformCanonicalOrder, as a non-nil slice (empty for a registered-only
// sample). It runs the sample-wide platform union once.
func (c *Client) sampleBaselinePlatforms(ctx context.Context, idSampleTmp int64) ([]string, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	args := make([]any, len(platformCanonicalOrder))
	for i := range platformCanonicalOrder {
		args[i] = idSampleTmp
	}

	rows, err := db.QueryContext(ctx, sampleProductMetricsPlatformsSQL(), args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample baseline platforms: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	present := make(map[string]struct{}, len(platformCanonicalOrder))
	for rows.Next() {
		var platform string
		if err = rows.Scan(&platform); err != nil {
			return nil, fmt.Errorf("%w: scan sample baseline platforms: %w", ErrUpstreamImpaired, err)
		}

		present[platform] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query sample baseline platforms: %w", ErrUpstreamImpaired, err)
	}

	ordered := make([]string, 0, len(present))
	for _, platform := range platformCanonicalOrder {
		if _, ok := present[platform]; ok {
			ordered = append(ordered, platform)
		}
	}

	return ordered, nil
}

// sampleBaselineQC rolls up the sample's overall qc across all platforms and
// returns the verdict and the product count (so the caller can tell sequenced
// from registered). The roll-up is fail > pending > pass; a sample with no
// products is qcNotTracked (never pending), so ONT and registered-only samples
// do not present a false pending.
func (c *Client) sampleBaselineQC(ctx context.Context, idSampleTmp int64) (string, int, error) {
	db := c.readCacheDB()
	if db == nil {
		return "", 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	args := []any{idSampleTmp, idSampleTmp, idSampleTmp, idSampleTmp}

	var (
		productCount int
		pending      sql.NullInt64
		minQC        sql.NullInt64
	)
	if err := db.QueryRowContext(ctx, sampleQCRollupSQL, args...).Scan(&productCount, &pending, &minQC); err != nil {
		return "", 0, fmt.Errorf("%w: aggregate sample qc roll-up: %w", ErrUpstreamImpaired, err)
	}

	return rollUpSampleQC(productCount, pending, minQC), productCount, nil
}

// sampleBaselineDelivery returns the sample's delivered_at (earliest iRODS
// created, UTC RFC3339) and whether it is delivered (has any iRODS row), from
// one sample-scoped iRODS aggregate. delivered_at is empty when the sample has
// no iRODS rows.
func (c *Client) sampleBaselineDelivery(ctx context.Context, idSampleTmp int64) (string, bool, error) {
	db := c.readCacheDB()
	if db == nil {
		return "", false, fmt.Errorf("mlwh: cache reader not configured")
	}

	var (
		dataObjects int
		minCreated  any
	)
	if err := db.QueryRowContext(ctx, sampleIRODSDeliverySQL, idSampleTmp).Scan(&dataObjects, &minCreated); err != nil {
		return "", false, fmt.Errorf("%w: aggregate sample irods delivery: %w", ErrUpstreamImpaired, err)
	}
	if dataObjects == 0 {
		return "", false, nil
	}

	deliveredAt, err := formatFreshnessTime(minCreated)
	if err != nil {
		return "", false, fmt.Errorf("mlwh: parse sample irods created for baseline: %w", err)
	}

	return deliveredAt, true, nil
}

// RunStatus returns the within-sequencing run-status timeline (spec F2/P5) for an
// Illumina run as one normalized RunStatusTimeline of {phase, entered_at,
// duration} events. idRun is the Illumina NPG id_run (the existing Run/ResolveRun
// space; NO new resolver), so a non-Illumina or otherwise invalid run yields the
// existing not-found / unsupported-identifier error and a numeric run absent from
// the synced cache yields ErrNotFound (the never-synced cascade applies via
// ResolveRun). The phase vocabulary is an OPEN dict/source pass-through (NOT a
// frozen list): an unknown iseq_run_status_dict description flows through
// verbatim. Events are ordered by date and preserved faithfully (recurrences,
// on-hold, cancelled and stopped-early kept, not deduplicated, reordered or
// forced monotonic); current is DERIVED from the latest date, never the source
// iscurrent. It delegates to runStatusTimelineForIlluminaRun, the same reusable
// per-run builder the unified progress endpoint (F3) embeds, so the standalone
// and embedded timelines never drift.
func (c *Client) RunStatus(ctx context.Context, idRun string) (RunStatusTimeline, error) {
	match, err := c.ResolveRun(ctx, idRun)
	if err != nil {
		return RunStatusTimeline{}, err
	}

	return c.runStatusTimelineForIlluminaRun(ctx, match.Run.IDRun)
}

// runStatusTimelineForIlluminaRun builds the normalized RunStatusTimeline for one
// resolved Illumina run from iseq_run_status_mirror joined to the dict. It is the
// reusable per-run Illumina builder shared by RunStatus (F2) and the unified
// progress endpoint (F3) so the standalone and embedded timelines are identical
// by construction. F3 obtains timelines for a sample's runs through this builder
// (Illumina) and, for the other platforms, by feeding their own status/dates
// through the shared platform-agnostic normalizeRunStatusTimeline; ONT has no
// within-sequencing runs, so F3 emits no RunStatusTimeline for an ONT sample (its
// runs is empty) rather than a false zero (HARD REQ 11).
func (c *Client) runStatusTimelineForIlluminaRun(ctx context.Context, idRun int) (RunStatusTimeline, error) {
	events, err := c.illuminaRunStatusEvents(ctx, idRun)
	if err != nil {
		return RunStatusTimeline{}, err
	}

	return normalizeRunStatusTimeline(idRun, platformIllumina, events), nil
}

// SampleProgress returns the unified sample-progress response (spec F3, layers
// P2/P4/P6) for the sample with the given Sanger sample name. It ALWAYS returns
// the always-derivable P0 baseline (deriveSampleBaseline: Platforms /
// BaselinePhase / QC / DeliveredAt), so it resolves for every sample on every
// platform. When the sample is present in seq_ops_tracking_per_sample_mirror it
// additionally returns the ordered milestones (each reached_at + duration_to_next
// to the next reached milestone, empty for the open one) and current_milestone
// (the latest reached milestone, whose successor is NULL) with
// detailed_timeline=true; otherwise detailed_timeline=false with a non-empty
// timeline_reason -- less detail, NEVER an error. For each of the sample's runs it
// embeds the shared RunStatusTimeline: Illumina runs through the same builder as
// GET /run/:id/status (so embedded and standalone never drift) and PacBio runs
// from their own well-metrics status/dates via normalizeRunStatusTimeline; ONT has
// no within-sequencing runs (empty runs) and its QC is not_tracked from the
// baseline (HARD REQ 11). cache_synced_at is the oldest last_run across the
// feeding tables. The never-synced / unknown-name cascade matches ResolveSampleName
// (and SampleDetail / LanesForSample): an unknown name on a synced cache yields
// ErrNotFound, and a never-synced cache yields an error satisfying both
// ErrCacheNeverSynced and ErrNotFound. An ONT or absent-from-tracking sample is
// not an error.
func (c *Client) SampleProgress(ctx context.Context, sangerName string) (SampleProgress, error) {
	match, err := c.ResolveSampleName(ctx, sangerName)
	if err != nil {
		return SampleProgress{}, err
	}
	if match.Sample == nil {
		return SampleProgress{}, ErrNotFound
	}

	sample := *match.Sample
	baseline, err := c.deriveSampleBaseline(ctx, sample.IDSampleTmp)
	if err != nil {
		return SampleProgress{}, err
	}

	progress := SampleProgress{
		Sample:        sample,
		Platforms:     baseline.Platforms,
		BaselinePhase: baseline.BaselinePhase,
		QC:            baseline.QC,
		DeliveredAt:   baseline.DeliveredAt,
	}

	if err = c.fillSampleProgressMilestones(ctx, sample.IDSampleLims, &progress); err != nil {
		return SampleProgress{}, err
	}

	runs, err := c.sampleRunTimelines(ctx, sample.IDSampleTmp)
	if err != nil {
		return SampleProgress{}, err
	}
	progress.Runs = runs

	syncedAt, err := c.oldestFeedingLastRun(ctx, sampleProgressFeedingTables)
	if err != nil {
		return SampleProgress{}, err
	}
	progress.CacheSyncedAt = syncedAt

	return progress, nil
}

// fillSampleProgressMilestones fills the milestone timeline from the tracking
// mirror. When the sample has a tracking row it sets detailed_timeline=true and
// emits the reached (non-NULL) milestones in canonical order, each with its
// reached_at (UTC RFC3339) and duration_to_next (the iso8601Duration to the next
// reached milestone, empty for the last/open one), and sets current_milestone to
// the last reached milestone (whose successor is NULL). When the sample is absent
// from the mirror it sets detailed_timeline=false with a non-empty timeline_reason
// and no milestones -- less detail, never an error.
func (c *Client) fillSampleProgressMilestones(ctx context.Context, idSampleLims string, progress *SampleProgress) error {
	reached, present, err := c.sampleTrackingMilestones(ctx, idSampleLims)
	if err != nil {
		return err
	}
	if !present {
		progress.TimelineReason = trackingTimelineReason

		return nil
	}

	progress.DetailedTimeline = true
	progress.Milestones = buildMilestones(reached)
	if len(progress.Milestones) > 0 {
		progress.CurrentMilestone = progress.Milestones[len(progress.Milestones)-1].Name
	}

	return nil
}

// sampleRunTimelines embeds one shared RunStatusTimeline per run of the sample,
// across every platform that has within-sequencing run status: the Illumina runs
// (via the same builder as GET /run/:id/status, so embedded and standalone never
// drift), the PacBio runs (built from their own well-metrics status/dates), the
// Elembio runs and the Ultimagen runs (each built from its own status/dates), all
// fed through the single shared normalizeRunStatusTimeline so there is no drift.
// Every platform's per-run timeline is reached through the run id its
// product-metrics carries: Illumina via iseq_product_metrics_mirror.id_run, PacBio
// via id_pac_bio_rw_metrics_tmp, Elembio via eseq_product_metrics_mirror.id_run ->
// eseq_run_lane_metrics_mirror.id_run, and Ultimagen via
// useq_product_metrics_mirror.id_run -> useq_run_metrics_mirror.id_run (the
// authoritative id_run joins the real ml_warehouse declares). ONT has no
// within-sequencing run status (no products/runs), so an ONT-only sample yields an
// empty (non-nil) slice rather than a false zero (HARD REQ 11).
func (c *Client) sampleRunTimelines(ctx context.Context, idSampleTmp int64) ([]RunStatusTimeline, error) {
	illumina, err := c.sampleIlluminaRunTimelines(ctx, idSampleTmp)
	if err != nil {
		return nil, err
	}

	pacbio, err := c.samplePacBioRunTimelines(ctx, idSampleTmp)
	if err != nil {
		return nil, err
	}

	elembio, err := c.sampleEseqRunTimelines(ctx, idSampleTmp)
	if err != nil {
		return nil, err
	}

	ultimagen, err := c.sampleUseqRunTimelines(ctx, idSampleTmp)
	if err != nil {
		return nil, err
	}

	runs := make([]RunStatusTimeline, 0, len(illumina)+len(pacbio)+len(elembio)+len(ultimagen))
	runs = append(runs, illumina...)
	runs = append(runs, pacbio...)
	runs = append(runs, elembio...)
	runs = append(runs, ultimagen...)

	return runs, nil
}

// sampleIlluminaRunTimelines builds the RunStatusTimeline for each distinct
// Illumina run the sample appears on, reusing runStatusTimelineForIlluminaRun so
// each embedded run EQUALS RunStatus(thatRun).
func (c *Client) sampleIlluminaRunTimelines(ctx context.Context, idSampleTmp int64) ([]RunStatusTimeline, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, sampleIlluminaRunsSQL, idSampleTmp)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample illumina runs: %w", ErrUpstreamImpaired, err)
	}

	runIDs := make([]int, 0)
	for rows.Next() {
		var idRun int
		if err = rows.Scan(&idRun); err != nil {
			_ = rows.Close()

			return nil, fmt.Errorf("%w: scan sample illumina runs: %w", ErrUpstreamImpaired, err)
		}

		runIDs = append(runIDs, idRun)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()

		return nil, fmt.Errorf("%w: query sample illumina runs: %w", ErrUpstreamImpaired, err)
	}
	_ = rows.Close()

	timelines := make([]RunStatusTimeline, 0, len(runIDs))
	for _, idRun := range runIDs {
		timeline, timelineErr := c.runStatusTimelineForIlluminaRun(ctx, idRun)
		if timelineErr != nil {
			return nil, timelineErr
		}

		timelines = append(timelines, timeline)
	}

	return timelines, nil
}

// samplePacBioRunTimelines builds the RunStatusTimeline for each PacBio well the
// sample has products on, from the well metrics' native run_status / well_status
// and dated lifecycle columns fed through the shared normalizeRunStatusTimeline
// (platform PacBio, IDRun 0 since PacBio has no Illumina NPG run id). Each well's
// non-NULL dated columns become date-ordered events: the run-level dates
// (run_start, run_complete) carry the native run_status and the well-level dates
// (well_complete, qc_seq_date) carry the native well_status, each passed through
// verbatim (open vocabulary). A well with no dated columns yields no timeline.
func (c *Client) samplePacBioRunTimelines(ctx context.Context, idSampleTmp int64) ([]RunStatusTimeline, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, samplePacBioWellMetricsSQL, idSampleTmp)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample pacbio well metrics: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	timelines := make([]RunStatusTimeline, 0)
	for rows.Next() {
		var (
			runStatus, wellStatus                          sql.NullString
			runStart, runComplete, wellComplete, qcSeqDate any
		)
		if err = rows.Scan(&runStatus, &wellStatus, &runStart, &runComplete, &wellComplete, &qcSeqDate); err != nil {
			return nil, fmt.Errorf("%w: scan sample pacbio well metrics: %w", ErrUpstreamImpaired, err)
		}

		events, eventsErr := pacBioWellEvents(runStatus.String, wellStatus.String, runStart, runComplete, wellComplete, qcSeqDate)
		if eventsErr != nil {
			return nil, eventsErr
		}
		if len(events) == 0 {
			continue
		}

		timelines = append(timelines, normalizeRunStatusTimeline(0, platformPacBio, events))
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query sample pacbio well metrics: %w", ErrUpstreamImpaired, err)
	}

	return timelines, nil
}

// sampleEseqRunTimelines builds the RunStatusTimeline for each Elembio run the
// sample's products link to, joining eseq_product_metrics_mirror.id_run ->
// eseq_run_lane_metrics_mirror.id_run and feeding the lane's dated lifecycle
// columns (run_started, run_complete) through the shared normalizeRunStatusTimeline
// (platform Elembio, IDRun 0 since the displayed IDRun is reserved for Illumina,
// matching the PacBio path). Elembio carries no native run_status string, so each
// dated column becomes an event labelled with its canonical lifecycle phase
// (eseqLaneEvents); a lane with no dated columns yields no timeline.
func (c *Client) sampleEseqRunTimelines(ctx context.Context, idSampleTmp int64) ([]RunStatusTimeline, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, sampleEseqLaneMetricsSQL, idSampleTmp)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample eseq lane metrics: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	timelines := make([]RunStatusTimeline, 0)
	for rows.Next() {
		var runStarted, runComplete any
		if err = rows.Scan(&runStarted, &runComplete); err != nil {
			return nil, fmt.Errorf("%w: scan sample eseq lane metrics: %w", ErrUpstreamImpaired, err)
		}

		events, eventsErr := eseqLaneEvents(runStarted, runComplete)
		if eventsErr != nil {
			return nil, eventsErr
		}
		if len(events) == 0 {
			continue
		}

		timelines = append(timelines, normalizeRunStatusTimeline(0, platformElembio, events))
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query sample eseq lane metrics: %w", ErrUpstreamImpaired, err)
	}

	return timelines, nil
}

// sampleUseqRunTimelines builds the RunStatusTimeline for each Ultimagen run the
// sample's products link to, joining useq_product_metrics_mirror.id_run ->
// useq_run_metrics_mirror.id_run and feeding the run's dated lifecycle columns
// (run_start, run_complete), each labelled with the native run_status (open
// vocabulary, verbatim), through the shared normalizeRunStatusTimeline (platform
// Ultimagen, IDRun 0). A run with no dated columns yields no timeline.
func (c *Client) sampleUseqRunTimelines(ctx context.Context, idSampleTmp int64) ([]RunStatusTimeline, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, sampleUseqRunMetricsSQL, idSampleTmp)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample useq run metrics: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	timelines := make([]RunStatusTimeline, 0)
	for rows.Next() {
		var (
			runStatus             sql.NullString
			runStart, runComplete any
		)
		if err = rows.Scan(&runStatus, &runStart, &runComplete); err != nil {
			return nil, fmt.Errorf("%w: scan sample useq run metrics: %w", ErrUpstreamImpaired, err)
		}

		events, eventsErr := useqRunEvents(runStatus.String, runStart, runComplete)
		if eventsErr != nil {
			return nil, eventsErr
		}
		if len(events) == 0 {
			continue
		}

		timelines = append(timelines, normalizeRunStatusTimeline(0, platformUltimagen, events))
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query sample useq run metrics: %w", ErrUpstreamImpaired, err)
	}

	return timelines, nil
}

// StatusBreakdown returns the per-baseline-phase study rollup (spec F4, layer P3):
// the counts of the study's samples by baseline phase, so a study owner sees study
// progress without a per-sample fan-out. It carries TWO denominators. distinct is
// the distinct-sample partition over the study's library_samples-linked samples,
// computed by ONE grouped query (statusBreakdownDistinctCacheSQL) that reuses the
// same Phase 2 membership join and study-scoped iRODS / product-metrics predicates
// as StudyOverview: each sample lands in exactly one of with_data >
// sequenced_no_data > registered by its most-advanced phase, so the three buckets
// sum to samples_total. per_platform is the per-platform partition, computed by ONE
// grouped query (statusBreakdownPerPlatformSQL): a sample's true state shows under
// EACH platform it spans, so within a platform the buckets sum to that platform's
// sample count but the grand total across platforms may EXCEED samples_total.
// Samples with no product-metrics (including ONT) are registered, never folded into
// a separate without-data negative. with_detailed_timeline is the count of the
// study's samples also present in the tracking mirror. cache_synced_at is the oldest
// last_run across the feeding tables, distinct from any data timestamp. The
// never-synced / unknown-study / synced-empty cascade matches CountSamplesForStudy:
// a never-synced cache returns the zero value with an error satisfying both
// ErrCacheNeverSynced and ErrNotFound, an unknown study returns ErrNotFound, and a
// synced study with no samples returns all-zero ladders with cache_synced_at
// populated.
func (c *Client) StatusBreakdown(ctx context.Context, studyLimsID string) (StatusBreakdown, error) {
	total, err := c.queryCount(ctx, countSamplesForStudyCacheSQL, "count study samples for status breakdown", studyLimsID)
	if err != nil {
		return StatusBreakdown{}, err
	}
	if total == 0 {
		return c.statusBreakdownForEmptyStudy(ctx, studyLimsID)
	}

	breakdown := StatusBreakdown{IDStudyLims: studyLimsID}

	distinct, err := c.statusBreakdownDistinct(ctx, studyLimsID)
	if err != nil {
		return StatusBreakdown{}, err
	}
	breakdown.Distinct = distinct

	perPlatform, err := c.statusBreakdownPerPlatform(ctx, studyLimsID)
	if err != nil {
		return StatusBreakdown{}, err
	}
	breakdown.PerPlatform = perPlatform

	withTimeline, err := c.queryCount(ctx, countSamplesWithDetailedTimelineCacheSQL, "count study samples with detailed timeline", studyLimsID)
	if err != nil {
		return StatusBreakdown{}, err
	}
	breakdown.WithDetailedTimeline = withTimeline

	syncedAt, err := c.oldestFeedingLastRun(ctx, statusBreakdownFeedingTables)
	if err != nil {
		return StatusBreakdown{}, err
	}
	breakdown.CacheSyncedAt = syncedAt

	return breakdown, nil
}

// statusBreakdownDistinct runs the single grouped distinct-partition query and
// returns the most-advanced-phase ladder summing to samples_total.
func (c *Client) statusBreakdownDistinct(ctx context.Context, studyLimsID string) (PhaseLadder, error) {
	db := c.readCacheDB()
	if db == nil {
		return PhaseLadder{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	var ladder PhaseLadder
	if err := db.QueryRowContext(ctx, statusBreakdownDistinctCacheSQL, studyLimsID).
		Scan(&ladder.WithData, &ladder.SequencedNoData, &ladder.Registered); err != nil {
		return PhaseLadder{}, fmt.Errorf("%w: aggregate study status breakdown distinct partition: %w", ErrUpstreamImpaired, err)
	}

	return ladder, nil
}

// statusBreakdownPerPlatform runs the single grouped per-platform-partition query
// and returns one PlatformPhaseLadder per platform present in the study, in
// platformCanonicalOrder. Each platform's buckets sum to that platform's distinct
// sample count; the grand total may exceed samples_total because a multi-platform
// sample is counted under every platform it spans.
func (c *Client) statusBreakdownPerPlatform(ctx context.Context, studyLimsID string) ([]PlatformPhaseLadder, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	args := make([]any, len(statusBreakdownProductPlatformArms)+1)
	for i := range args {
		args[i] = studyLimsID
	}

	rows, err := db.QueryContext(ctx, statusBreakdownPerPlatformSQL(), args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query study status breakdown per-platform partition: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	ladders := make(map[string]PhaseLadder, len(platformCanonicalOrder))
	for rows.Next() {
		var (
			platform string
			ladder   PhaseLadder
		)
		if err = rows.Scan(&platform, &ladder.WithData, &ladder.SequencedNoData, &ladder.Registered); err != nil {
			return nil, fmt.Errorf("%w: scan study status breakdown per-platform partition: %w", ErrUpstreamImpaired, err)
		}

		ladders[platform] = ladder
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query study status breakdown per-platform partition: %w", ErrUpstreamImpaired, err)
	}

	ordered := make([]PlatformPhaseLadder, 0, len(ladders))
	for _, platform := range platformCanonicalOrder {
		if ladder, ok := ladders[platform]; ok {
			ordered = append(ordered, PlatformPhaseLadder{Platform: platform, Ladder: ladder})
		}
	}

	return ordered, nil
}

// statusBreakdownForEmptyStudy resolves a StatusBreakdown when no samples are linked
// to the study, mirroring CountSamplesForStudy's cascade: a never-synced cache
// returns the joined sentinel, an unknown study returns ErrNotFound, and a synced
// study with no samples returns all-zero ladders carrying cache_synced_at.
func (c *Client) statusBreakdownForEmptyStudy(ctx context.Context, studyLimsID string) (StatusBreakdown, error) {
	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return StatusBreakdown{}, err
	}
	if studyExists {
		summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
		if err != nil {
			return StatusBreakdown{}, err
		}
		if summary.allAbsent || !summary.allPresent {
			return StatusBreakdown{}, neverSyncedReadErr()
		}

		syncedAt, err := c.oldestFeedingLastRun(ctx, statusBreakdownFeedingTables)
		if err != nil {
			return StatusBreakdown{}, err
		}

		return StatusBreakdown{IDStudyLims: studyLimsID, CacheSyncedAt: syncedAt}, nil
	}

	if err := c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return StatusBreakdown{}, err
	}

	return StatusBreakdown{}, ErrNotFound
}
