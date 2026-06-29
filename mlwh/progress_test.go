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
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// f1SampleIDs names the samples seeded for the F1 baseline scenario (the F1
// portion of the HARD REQ 7 scenario seed) so the tests refer to them by intent
// rather than by bare surrogate key. Items 4.2-4.4 extend the same seed.
const (
	f1RegisteredOnly  = int64(301) // registered: library link only, no products, no iRODS
	f1SequencedNULLQC = int64(302) // sequenced: Illumina products with NULL qc, no iRODS
	f1DeliveredPass   = int64(303) // delivered: products qc=1 + iRODS on 2026-06-25 and 2026-06-26
	f1FailMixedQC     = int64(304) // sequenced: two Illumina products, qc=0 and qc=1 (any-fail -> fail)
	f1ONT             = int64(305) // ONT: oseq_flowcell only, no products/iRODS
	f1MultiPlatform   = int64(306) // delivered on Illumina, sequenced-only on PacBio (most-advanced delivered)
)

// f1DeliveredCreated names the two iRODS created timestamps the delivered sample
// carries, so delivered_at is asserted to be the earlier of the two.
var (
	f1DeliveredEarliest = time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC)
	f1DeliveredLatest   = time.Date(2026, time.June, 26, 9, 0, 0, 0, time.UTC)
)

// f2RunStatus names the Illumina NPG id_run the F2 run-status timeline scenario
// seeds (the run-status portion of the HARD REQ 7 scenario seed). It carries an
// ordered iseq_run_status timeline exercising every F2 property: ordering by
// date, recurrence, an on-hold event, an unknown dict description, and a source
// iscurrent=1 on an EARLIER date than the latest row (so current is proven
// DERIVED). Item 4.3 (F3) reuses this run for its embedded-run acceptance test.
const f2RunStatus = 52553

// f2RunStatusInvalid is an id_run absent from the synced cache, so RunStatus on
// it yields ErrNotFound (an unknown run, not a never-synced cache).
const f2RunStatusInvalid = "99999"

// f2StatusBase is the entered_at of the run's first status event; each later
// event is one hour after the previous, so deltas are a clean PT1H and the
// derived-current / ordering assertions read cleanly.
var f2StatusBase = time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC)

// f2 dict descriptions are the native NPG status pass-through strings the
// timeline reports verbatim as phases. "experimental new status" is deliberately
// NOT in any canonical/closed vocabulary, proving the open-dict pass-through.
const (
	f2PhasePending    = "pending"
	f2PhaseInProgress = "in progress"
	f2PhaseComplete   = "complete"
	f2PhaseAnalysis   = "analysis in progress"
	f2PhaseOnHold     = "on hold"
	f2PhaseUnknown    = "experimental new status"
	f2PhaseAnalysisOK = "analysis complete"
	f2PhaseQCReview   = "qc review pending"
)

// f3 names the milestone-shape and timestamp constants the F3 unified-progress
// scenario relies on, so the acceptance tests read by intent. The tracking
// mirror is keyed by id_sample_lims, which seedSampleMirrorRow sets to
// formatInt(id_sample_tmp+100); f3*SampleLims spell that out for the two tracked
// samples.
const (
	// f3DeliveredSampleLims is f1DeliveredPass's id_sample_lims (303 -> "403").
	// It is the tracked sample filled manifest_created..sequencing_run_start with
	// sequencing_qc_complete NULL (F3 test 1) and the sample whose embedded
	// Illumina run is run 52553 (F3 test 3).
	f3DeliveredSampleLims = "403"
	// f3LibraryCompleteSampleLims is f1SequencedNULLQC's id_sample_lims (302 ->
	// "402"). It is the tracked sample filled through library_complete with
	// sequencing_run_start NULL (F3 test 5).
	f3LibraryCompleteSampleLims = "402"
)

// f3MilestoneBase is the reached_at of the first milestone the tracked samples
// carry; each later milestone is one day after the previous, so a milestone's
// duration_to_next is a clean P1D and the open/current milestone (whose successor
// is NULL) has an empty duration_to_next.
var f3MilestoneBase = time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)

// f3PacBioStatusBase is the entered_at of the PacBio run's first dated lifecycle
// event; each later dated column is one hour after the previous, so the embedded
// PacBio timeline's deltas are a clean PT1H.
var f3PacBioStatusBase = time.Date(2026, time.June, 20, 8, 0, 0, 0, time.UTC)

// f3PacBio native (open-vocabulary) status strings the embedded PacBio timeline
// reports verbatim: the run-level dates (run_start/run_complete) carry the native
// run_status and the well-level dates (well_complete/qc_seq_date) carry the
// native well_status.
const (
	f3PacBioRunStatus  = "Complete"
	f3PacBioWellStatus = "qc complete"
)

// f3Elembio and f3Ultimagen name the Elembio and Ultimagen samples the F3
// scenario seeds (extending the HARD REQ 7 scenario to all platforms with a
// within-sequencing run-status source). Each carries a product-metrics row with
// an id_run and a matching run-status mirror row, so its per-run timeline is
// derived via id_run exactly like the Illumina (iseq_run_status) and PacBio
// (well metrics) paths.
const (
	f3Elembio   = int64(307) // Elembio: eseq product (id_run) + eseq_run_lane_metrics dates
	f3Ultimagen = int64(308) // Ultimagen: useq product (id_run) + useq_run_metrics status/dates
)

// f3ElembioIDRun and f3UltimagenIDRun are the NPG run ids joining each platform's
// product-metrics mirror to its run-status mirror (eseq_product_metrics_mirror.
// id_run -> eseq_run_lane_metrics_mirror.id_run; useq_product_metrics_mirror.
// id_run -> useq_run_metrics_mirror.id_run).
const (
	f3ElembioIDRun   = 60001
	f3UltimagenIDRun = 70001
)

// f3ElembioStatusBase and f3UltimagenStatusBase are the entered_at of each
// platform's first dated lifecycle event; each later dated column is one hour
// after the previous, so the embedded timelines have clean PT1H deltas.
var (
	f3ElembioStatusBase   = time.Date(2026, time.June, 22, 8, 0, 0, 0, time.UTC)
	f3UltimagenStatusBase = time.Date(2026, time.June, 23, 8, 0, 0, 0, time.UTC)
)

// f3UltimagenRunStatus is the Ultimagen native (open-vocabulary) run_status the
// embedded timeline reports verbatim for both run-level dated columns.
const f3UltimagenRunStatus = "Completed"

// theNineMilestonesInOrder is the canonical milestone order the F3 tests assert
// verbatim (the closed 9-name enum), independent of the production constant so a
// drift in either is caught.
var theNineMilestonesInOrder = []string{
	"manifest_created", "manifest_uploaded", "labware_received", "order_made",
	"working_dilution", "library_start", "library_complete",
	"sequencing_run_start", "sequencing_qc_complete",
}

// f4 names the samples seeded for the F4 status-breakdown scenario. It is the
// spec's "study S1" status-breakdown scenario realised as a self-contained study
// (LIMS id f4StudyLims) whose distinct partition is exactly the spec's {3,1,1}
// summing to 5, so the acceptance numbers hold; it cannot reuse the F1/F2/F3 S1
// seed (study 300), whose distinct partition is {2,4,2} summing to 8 (built for
// the baseline/run-status tests, not these counts). It still reuses every shared
// seed helper and models the same entities the spec calls out for F4: the
// multi-platform sample, the ONT sample, the PacBio product, and the
// tracking-mirror samples.
const (
	f4Delivered1      = int64(401) // delivered: Illumina product qc=1 + iRODS (with_data); in tracking
	f4Delivered2      = int64(402) // delivered: Illumina product qc=1 + iRODS (with_data)
	f4MultiPlatform   = int64(403) // delivered on Illumina, sequenced-only on PacBio (distinct with_data; per-platform Illumina with_data + PacBio sequenced_no_data)
	f4SequencedNoData = int64(404) // sequenced: Illumina product, no iRODS (sequenced_no_data); in tracking
	f4ONT             = int64(405) // ONT: oseq_flowcell only, no products/iRODS (registered)
	f4PacBioDelivered = int64(406) // delivered on PacBio only: PacBio product + matching study-scoped iRODS row (per-platform PacBio with_data)
)

// f4StudyLims is the LIMS study id of the F4 status-breakdown scenario study.
const f4StudyLims = "S4"

// f4StudyTmp is the surrogate key of the F4 scenario study (distinct from the
// 300-series F1/F2/F3 study so the two seeds never share a study row).
const f4StudyTmp = int64(400)

// f4DeliveredCreated is the iRODS created timestamp the delivered F4 samples
// carry; its exact value is irrelevant to the partition (only presence of a
// study-scoped iRODS row matters), so one constant suffices.
var f4DeliveredCreated = time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC)

// F1 acceptance test 1: a sample with a library link but NO products is
// registered, its QC is NOT pending (no products => not tracked, not pending),
// and it has no delivered_at.
func TestSampleBaselineRegisteredNoProducts(t *testing.T) {
	convey.Convey("Given the F1 baseline scenario with a registered-only sample (library link, no products)", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF1BaselineScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		baseline, err := client.deriveSampleBaseline(context.Background(), f1RegisteredOnly)

		convey.Convey("when its baseline is derived, then baseline_phase is registered, qc is not_tracked (NOT pending), and delivered_at is empty", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(baseline.BaselinePhase, convey.ShouldEqual, "registered")
			convey.So(baseline.QC, convey.ShouldNotEqual, "pending")
			convey.So(baseline.QC, convey.ShouldEqual, "not_tracked")
			convey.So(baseline.DeliveredAt, convey.ShouldEqual, "")
		})
	})
}

// F1 acceptance test 2: a sample with Illumina products whose qc is NULL and no
// iRODS is sequenced with qc pending (NULL preserved as pending, distinct from
// fail).
func TestSampleBaselineSequencedPendingQC(t *testing.T) {
	convey.Convey("Given the F1 baseline scenario with a sample carrying Illumina products (qc NULL) and no iRODS", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF1BaselineScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		baseline, err := client.deriveSampleBaseline(context.Background(), f1SequencedNULLQC)

		convey.Convey("when its baseline is derived, then baseline_phase is sequenced and qc is pending", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(baseline.BaselinePhase, convey.ShouldEqual, "sequenced")
			convey.So(baseline.QC, convey.ShouldEqual, "pending")
			convey.So(baseline.DeliveredAt, convey.ShouldEqual, "")
		})
	})
}

// F1 acceptance test 3: a sample with products (qc=1 on all) and iRODS rows
// created 2026-06-25 and 2026-06-26 is delivered with qc pass and delivered_at
// the earliest created.
func TestSampleBaselineDeliveredPassEarliestDeliveredAt(t *testing.T) {
	convey.Convey("Given the F1 baseline scenario with a sample whose products are qc=1 and that has iRODS rows on 2026-06-25 and 2026-06-26", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF1BaselineScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		baseline, err := client.deriveSampleBaseline(context.Background(), f1DeliveredPass)

		convey.Convey("when its baseline is derived, then baseline_phase is delivered, qc is pass, and delivered_at is the earliest created", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(baseline.BaselinePhase, convey.ShouldEqual, "delivered")
			convey.So(baseline.QC, convey.ShouldEqual, "pass")
			convey.So(baseline.DeliveredAt, convey.ShouldEqual, f1DeliveredEarliest.Format(utcRFC3339Layout))
		})
	})
}

// F1 acceptance test 4: a sample with two products, one qc=0 and one qc=1, rolls
// up to fail (any fail -> fail), beating the qc=1 product.
func TestSampleBaselineAnyFailRollsUpToFail(t *testing.T) {
	convey.Convey("Given the F1 baseline scenario with a sample carrying two products, one qc=0 and one qc=1", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF1BaselineScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		baseline, err := client.deriveSampleBaseline(context.Background(), f1FailMixedQC)

		convey.Convey("when its baseline is derived, then qc rolls up to fail", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(baseline.QC, convey.ShouldEqual, "fail")
		})
	})
}

// F1 acceptance test 5: an ONT sample (oseq_flowcell, no products/iRODS) is
// registered, its qc is not_tracked, and its platforms are ["ONT"] (never a
// false zero -- HARD REQ 11).
func TestSampleBaselineONTRegisteredNotTracked(t *testing.T) {
	convey.Convey("Given the F1 baseline scenario with an ONT sample (oseq_flowcell only, no products/iRODS)", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF1BaselineScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		baseline, err := client.deriveSampleBaseline(context.Background(), f1ONT)

		convey.Convey("when its baseline is derived, then baseline_phase is registered, qc is not_tracked, platforms is [\"ONT\"], and delivered_at is empty", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(baseline.BaselinePhase, convey.ShouldEqual, "registered")
			convey.So(baseline.QC, convey.ShouldEqual, "not_tracked")
			convey.So(baseline.Platforms, convey.ShouldResemble, []string{"ONT"})
			convey.So(baseline.DeliveredAt, convey.ShouldEqual, "")
		})
	})
}

// F1 acceptance test 6: a sample delivered on Illumina but only sequenced on
// PacBio takes the most-advanced phase (delivered), and its platforms carry
// BOTH Illumina and PacBio (canonical order).
func TestSampleBaselineMultiPlatformMostAdvanced(t *testing.T) {
	convey.Convey("Given the F1 baseline scenario with a sample delivered on Illumina but only sequenced on PacBio", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF1BaselineScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		baseline, err := client.deriveSampleBaseline(context.Background(), f1MultiPlatform)

		convey.Convey("when its baseline is derived, then baseline_phase is the most-advanced (delivered) and platforms has both Illumina and PacBio", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(baseline.BaselinePhase, convey.ShouldEqual, "delivered")
			convey.So(baseline.Platforms, convey.ShouldResemble, []string{"Illumina", "PacBio"})
		})
	})
}

// F2 acceptance test 1: an Illumina run with a sequence of iseq_run_status rows
// (pending -> in progress -> complete -> ... -> qc review pending, the latest)
// yields events ordered by date, each entered_at = its date, each non-last
// duration = the delta to the next event, the last event's duration empty, and
// current = the latest-date phase ("qc review pending").
func TestRunStatusOrderedEventsDeltasAndDerivedCurrent(t *testing.T) {
	convey.Convey("Given the F2 run-status scenario with run 52553's ordered iseq_run_status timeline ending at qc review pending", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF2RunStatusScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		timeline, err := client.RunStatus(context.Background(), formatInt(f2RunStatus))

		convey.Convey("when RunStatus is called, then events are ordered by date with delta durations, the last duration is empty, and current is the latest-date phase", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(timeline.IDRun, convey.ShouldEqual, f2RunStatus)
			convey.So(timeline.Platform, convey.ShouldEqual, "Illumina")
			convey.So(timeline.NotTracked, convey.ShouldEqual, "")

			phases := make([]string, len(timeline.Events))
			entered := make([]string, len(timeline.Events))
			for i, event := range timeline.Events {
				phases[i] = event.Phase
				entered[i] = event.EnteredAt
			}

			convey.So(phases, convey.ShouldResemble, []string{
				f2PhasePending, f2PhaseInProgress, f2PhaseComplete, f2PhaseAnalysis,
				f2PhaseOnHold, f2PhaseAnalysis, f2PhaseUnknown, f2PhaseAnalysisOK, f2PhaseQCReview,
			})

			convey.So(entered[0], convey.ShouldEqual, f2StatusBase.Format(utcRFC3339Layout))
			convey.So(entered[len(entered)-1], convey.ShouldEqual, f2StatusBase.Add(8*time.Hour).Format(utcRFC3339Layout))

			convey.So(timeline.Events[0].Duration, convey.ShouldEqual, "PT1H")
			convey.So(timeline.Events[len(timeline.Events)-2].Duration, convey.ShouldEqual, "PT1H")
			convey.So(timeline.Events[len(timeline.Events)-1].Duration, convey.ShouldEqual, "")

			convey.So(timeline.Current, convey.ShouldEqual, f2PhaseQCReview)
		})
	})
}

// F2 acceptance test 2: the run's source iscurrent=1 is on an EARLIER date than
// the latest row, so current is the latest-date phase, proving current is
// derived from the latest entered_at, not read from iscurrent.
func TestRunStatusCurrentIsDerivedNotIscurrent(t *testing.T) {
	convey.Convey("Given the F2 run-status scenario where source iscurrent=1 is on an earlier-dated row than the latest", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF2RunStatusScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		timeline, err := client.RunStatus(context.Background(), formatInt(f2RunStatus))

		convey.Convey("when RunStatus is called, then current is the latest-date phase, not the iscurrent=1 phase", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(timeline.Current, convey.ShouldEqual, f2PhaseQCReview)
			convey.So(timeline.Current, convey.ShouldNotEqual, f2PhaseOnHold)
		})
	})
}

// F2 acceptance test 3: a repeated phase ("analysis in progress" twice) and an
// "on hold" event both appear in date order, not deduplicated and not reordered
// (the timeline is faithful, never forced monotonic).
func TestRunStatusPreservesRecurrenceAndOnHoldInOrder(t *testing.T) {
	convey.Convey("Given the F2 run-status scenario with a recurring analysis phase and an on-hold event", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF2RunStatusScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		timeline, err := client.RunStatus(context.Background(), formatInt(f2RunStatus))

		convey.Convey("when RunStatus is called, then both analysis occurrences and the on-hold event appear in order", func() {
			convey.So(err, convey.ShouldBeNil)

			analysisPositions := []int{}
			onHoldPositions := []int{}
			for i, event := range timeline.Events {
				switch event.Phase {
				case f2PhaseAnalysis:
					analysisPositions = append(analysisPositions, i)
				case f2PhaseOnHold:
					onHoldPositions = append(onHoldPositions, i)
				}
			}

			convey.So(analysisPositions, convey.ShouldResemble, []int{3, 5})
			convey.So(onHoldPositions, convey.ShouldResemble, []int{4})
			convey.So(timeline.Events[3].EnteredAt, convey.ShouldEqual, f2StatusBase.Add(3*time.Hour).Format(utcRFC3339Layout))
			convey.So(timeline.Events[5].EnteredAt, convey.ShouldEqual, f2StatusBase.Add(5*time.Hour).Format(utcRFC3339Layout))
		})
	})
}

// F2 acceptance test 4: a new (unknown) iseq_run_status_dict description passes
// through verbatim as the phase value (open vocabulary, not rejected, not
// normalized).
func TestRunStatusUnknownDictDescriptionPassesThrough(t *testing.T) {
	convey.Convey("Given the F2 run-status scenario with an unknown iseq_run_status_dict description in the timeline", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF2RunStatusScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		timeline, err := client.RunStatus(context.Background(), formatInt(f2RunStatus))

		convey.Convey("when RunStatus is called, then the unknown description appears verbatim as a phase", func() {
			convey.So(err, convey.ShouldBeNil)

			phases := make([]string, len(timeline.Events))
			for i, event := range timeline.Events {
				phases[i] = event.Phase
			}

			convey.So(phases, convey.ShouldContain, f2PhaseUnknown)
		})
	})
}

// F2 acceptance test 5: an :id that is not a valid Illumina run yields
// ErrNotFound (the run is absent from the synced cache).
func TestRunStatusInvalidRunNotFound(t *testing.T) {
	convey.Convey("Given the F2 run-status scenario on a synced cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF2RunStatusScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, err := client.RunStatus(context.Background(), f2RunStatusInvalid)

		convey.Convey("when RunStatus is called for a run absent from the cache, then ErrNotFound", func() {
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})
}

// F3 acceptance test 1: a tracked sample filled manifest_created..
// sequencing_run_start with sequencing_qc_complete NULL has detailed_timeline,
// its milestones in canonical order, each non-final duration_to_next the delta to
// the next reached milestone, and current_milestone == sequencing_run_start (the
// latest reached, whose successor is NULL).
func TestSampleProgressTrackedMilestonesInCanonicalOrder(t *testing.T) {
	convey.Convey("Given the F3 progress scenario with a tracked sample filled manifest_created..sequencing_run_start (sequencing_qc_complete NULL)", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF3ProgressScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		progress, err := client.SampleProgress(context.Background(), "sample-"+formatInt(f1DeliveredPass))

		convey.Convey("when SampleProgress is called, then detailed_timeline is true, milestones are in canonical order with delta durations, and current_milestone is sequencing_run_start", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(progress.DetailedTimeline, convey.ShouldBeTrue)
			convey.So(progress.TimelineReason, convey.ShouldEqual, "")
			convey.So(progress.CurrentMilestone, convey.ShouldEqual, "sequencing_run_start")

			names := make([]string, len(progress.Milestones))
			for i, milestone := range progress.Milestones {
				names[i] = milestone.Name
			}
			convey.So(names, convey.ShouldResemble, theNineMilestonesInOrder[:8])

			convey.So(progress.Milestones[0].ReachedAt, convey.ShouldEqual, f3MilestoneBase.Format(utcRFC3339Layout))
			convey.So(progress.Milestones[0].DurationToNext, convey.ShouldEqual, "P1D")
			convey.So(progress.Milestones[len(progress.Milestones)-2].DurationToNext, convey.ShouldEqual, "P1D")
			convey.So(progress.Milestones[len(progress.Milestones)-1].DurationToNext, convey.ShouldEqual, "")
		})
	})
}

// F3 acceptance test 2: a sample ABSENT from the tracking mirror but with
// product-metrics and iRODS rows still returns the P0 baseline (delivered, its
// qc, its delivered_at) with detailed_timeline false and a non-empty
// timeline_reason -- less detail, not an error.
func TestSampleProgressAbsentFromTrackingStillReturnsBaseline(t *testing.T) {
	convey.Convey("Given the F3 progress scenario with a delivered sample absent from the tracking mirror", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF3ProgressScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		progress, err := client.SampleProgress(context.Background(), "sample-"+formatInt(f1MultiPlatform))

		convey.Convey("when SampleProgress is called, then detailed_timeline is false with a reason and the P0 baseline is still returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(progress.DetailedTimeline, convey.ShouldBeFalse)
			convey.So(progress.TimelineReason, convey.ShouldNotEqual, "")
			convey.So(progress.Milestones, convey.ShouldBeEmpty)
			convey.So(progress.CurrentMilestone, convey.ShouldEqual, "")
			convey.So(progress.BaselinePhase, convey.ShouldEqual, "delivered")
			convey.So(progress.QC, convey.ShouldEqual, "pass")
			convey.So(progress.DeliveredAt, convey.ShouldEqual, f1DeliveredLatest.Format(utcRFC3339Layout))
		})
	})
}

// F3 acceptance test 3: a tracked sample with one Illumina run carrying
// iseq_run_status rows embeds one RunStatusTimeline EQUAL to RunStatus(thatRun) --
// the same events and the same derived current -- proving the shared type does not
// drift between the standalone endpoint and the per-run embedding.
func TestSampleProgressEmbeddedIlluminaRunEqualsRunStatus(t *testing.T) {
	convey.Convey("Given the F3 progress scenario with a tracked sample on Illumina run 52553", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF3ProgressScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		progress, err := client.SampleProgress(context.Background(), "sample-"+formatInt(f1DeliveredPass))
		standalone, statusErr := client.RunStatus(context.Background(), formatInt(f2RunStatus))

		convey.Convey("when SampleProgress is called, then runs contains one RunStatusTimeline equal to RunStatus(thatRun)", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(statusErr, convey.ShouldBeNil)

			illuminaRuns := make([]RunStatusTimeline, 0, len(progress.Runs))
			for _, run := range progress.Runs {
				if run.IDRun == f2RunStatus {
					illuminaRuns = append(illuminaRuns, run)
				}
			}
			convey.So(illuminaRuns, convey.ShouldHaveLength, 1)
			convey.So(illuminaRuns[0], convey.ShouldResemble, standalone)
		})
	})
}

// F3 acceptance test 4: the ONT sample resolves identity + study, reports
// platforms == ["ONT"] and qc == not_tracked, has empty runs, and (being outside
// the tracking mirror) detailed_timeline false -- never a bare "no data".
func TestSampleProgressONTResolvesNotTracked(t *testing.T) {
	convey.Convey("Given the F3 progress scenario with the ONT sample (oseq_flowcell only, outside tracking)", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF3ProgressScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		progress, err := client.SampleProgress(context.Background(), "sample-"+formatInt(f1ONT))

		convey.Convey("when SampleProgress is called, then platforms is [\"ONT\"], qc is not_tracked, runs is empty, and detailed_timeline is false", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(progress.Sample.Name, convey.ShouldEqual, "sample-"+formatInt(f1ONT))
			convey.So(progress.Platforms, convey.ShouldResemble, []string{"ONT"})
			convey.So(progress.QC, convey.ShouldEqual, "not_tracked")
			convey.So(progress.Runs, convey.ShouldBeEmpty)
			convey.So(progress.DetailedTimeline, convey.ShouldBeFalse)
			convey.So(progress.TimelineReason, convey.ShouldNotEqual, "")
		})
	})
}

// F3 acceptance test 5: a tracked sample whose library_complete is set but
// sequencing_run_start NULL has current_milestone == library_complete and the
// library_start -> library_complete duration_to_next is the delta between them.
func TestSampleProgressCurrentMilestoneIsLibraryComplete(t *testing.T) {
	convey.Convey("Given the F3 progress scenario with a tracked sample filled through library_complete (sequencing_run_start NULL)", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF3ProgressScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		progress, err := client.SampleProgress(context.Background(), "sample-"+formatInt(f1SequencedNULLQC))

		convey.Convey("when SampleProgress is called, then current_milestone is library_complete and library_start's duration_to_next is the delta to library_complete", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(progress.DetailedTimeline, convey.ShouldBeTrue)
			convey.So(progress.CurrentMilestone, convey.ShouldEqual, "library_complete")

			var libraryStart *Milestone
			for i := range progress.Milestones {
				if progress.Milestones[i].Name == "library_start" {
					libraryStart = &progress.Milestones[i]
				}
			}
			convey.So(libraryStart, convey.ShouldNotBeNil)
			convey.So(libraryStart.DurationToNext, convey.ShouldEqual, "P1D")

			last := progress.Milestones[len(progress.Milestones)-1]
			convey.So(last.Name, convey.ShouldEqual, "library_complete")
			convey.So(last.DurationToNext, convey.ShouldEqual, "")
		})
	})
}

// F3 acceptance test 6: an unknown Sanger name on a synced cache yields
// ErrNotFound, and on a never-synced cache yields an error satisfying both
// ErrCacheNeverSynced and ErrNotFound (the resolver cascade).
func TestSampleProgressUnknownAndNeverSyncedCascade(t *testing.T) {
	convey.Convey("Given a synced F3 progress scenario and an unknown Sanger name", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF3ProgressScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, err := client.SampleProgress(context.Background(), "sample-does-not-exist")

		convey.Convey("when SampleProgress is called for an unknown name on a synced cache, then ErrNotFound (not never-synced)", func() {
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
		})
	})

	convey.Convey("Given a never-synced cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, err := client.SampleProgress(context.Background(), "sample-"+formatInt(f1DeliveredPass))

		convey.Convey("when SampleProgress is called, then the error satisfies both ErrCacheNeverSynced and ErrNotFound", func() {
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})
}

// F3 per-platform run test (avoids dead/untested code for the per-platform
// extraction built in step 3): a PacBio product-bearing sample's SampleProgress
// embeds a correct RunStatusTimeline for its PacBio run, built from the well
// metrics' status+dates via the shared normalizeRunStatusTimeline -- platform
// PacBio, IDRun 0 (non-Illumina), events in date order with the native
// run_status / well_status passed through verbatim, delta durations and a derived
// current.
func TestSampleProgressEmbedsPacBioRunFromWellMetrics(t *testing.T) {
	convey.Convey("Given the F3 progress scenario with a PacBio product-bearing sample whose well metrics carry status+dates", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF3ProgressScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		progress, err := client.SampleProgress(context.Background(), "sample-"+formatInt(f1MultiPlatform))

		convey.Convey("when SampleProgress is called, then runs contains the PacBio timeline built from the well metrics", func() {
			convey.So(err, convey.ShouldBeNil)

			pacbioRuns := make([]RunStatusTimeline, 0, len(progress.Runs))
			for _, run := range progress.Runs {
				if run.Platform == "PacBio" {
					pacbioRuns = append(pacbioRuns, run)
				}
			}
			convey.So(pacbioRuns, convey.ShouldHaveLength, 1)

			pacbio := pacbioRuns[0]
			convey.So(pacbio.IDRun, convey.ShouldEqual, 0)
			convey.So(pacbio.NotTracked, convey.ShouldEqual, "")

			phases := make([]string, len(pacbio.Events))
			entered := make([]string, len(pacbio.Events))
			for i, event := range pacbio.Events {
				phases[i] = event.Phase
				entered[i] = event.EnteredAt
			}
			convey.So(phases, convey.ShouldResemble, []string{
				f3PacBioRunStatus, f3PacBioRunStatus, f3PacBioWellStatus, f3PacBioWellStatus,
			})
			convey.So(entered[0], convey.ShouldEqual, f3PacBioStatusBase.Format(utcRFC3339Layout))
			convey.So(pacbio.Events[0].Duration, convey.ShouldEqual, "PT1H")
			convey.So(pacbio.Events[len(pacbio.Events)-1].Duration, convey.ShouldEqual, "")
			convey.So(pacbio.Current, convey.ShouldEqual, f3PacBioWellStatus)
		})
	})
}

// F3 per-platform run test (Elembio): an Elembio product-bearing sample's
// SampleProgress embeds a correct RunStatusTimeline for its Elembio run, built by
// joining eseq_product_metrics_mirror.id_run -> eseq_run_lane_metrics_mirror.id_run
// and turning the dated lane-lifecycle columns (run_started, run_complete) into
// date-ordered events via the shared normalizeRunStatusTimeline -- platform
// Elembio, IDRun 0 (non-Illumina), delta durations and a derived current. Elembio
// carries no native run_status string, so the phase is the lifecycle column's
// canonical name (an open-vocabulary, source-derived label).
func TestSampleProgressEmbedsElembioRunFromLaneMetrics(t *testing.T) {
	convey.Convey("Given the F3 progress scenario with an Elembio product-bearing sample whose lane metrics carry dated lifecycle columns", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF3ProgressScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		progress, err := client.SampleProgress(context.Background(), "sample-"+formatInt(f3Elembio))

		convey.Convey("when SampleProgress is called, then runs contains the Elembio timeline built from the lane metrics via id_run", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(progress.Platforms, convey.ShouldResemble, []string{"Elembio"})

			elembioRuns := make([]RunStatusTimeline, 0, len(progress.Runs))
			for _, run := range progress.Runs {
				if run.Platform == "Elembio" {
					elembioRuns = append(elembioRuns, run)
				}
			}
			convey.So(elembioRuns, convey.ShouldHaveLength, 1)

			elembio := elembioRuns[0]
			convey.So(elembio.IDRun, convey.ShouldEqual, 0)
			convey.So(elembio.NotTracked, convey.ShouldEqual, "")

			phases := make([]string, len(elembio.Events))
			entered := make([]string, len(elembio.Events))
			for i, event := range elembio.Events {
				phases[i] = event.Phase
				entered[i] = event.EnteredAt
			}
			convey.So(phases, convey.ShouldResemble, []string{elembioRunStartedPhase, elembioRunCompletePhase})
			convey.So(entered[0], convey.ShouldEqual, f3ElembioStatusBase.Format(utcRFC3339Layout))
			convey.So(elembio.Events[0].Duration, convey.ShouldEqual, "PT1H")
			convey.So(elembio.Events[len(elembio.Events)-1].Duration, convey.ShouldEqual, "")
			convey.So(elembio.Current, convey.ShouldEqual, elembioRunCompletePhase)
		})
	})
}

// F3 per-platform run test (Ultimagen): an Ultimagen product-bearing sample's
// SampleProgress embeds a correct RunStatusTimeline for its Ultimagen run, built
// by joining useq_product_metrics_mirror.id_run -> useq_run_metrics_mirror.id_run
// and turning the run-level dated columns (run_start, run_complete) into
// date-ordered events labelled with the native run_status (open vocabulary,
// verbatim) via the shared normalizeRunStatusTimeline -- platform Ultimagen, IDRun
// 0, delta durations and a derived current.
func TestSampleProgressEmbedsUltimagenRunFromRunMetrics(t *testing.T) {
	convey.Convey("Given the F3 progress scenario with an Ultimagen product-bearing sample whose run metrics carry status+dates", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF3ProgressScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		progress, err := client.SampleProgress(context.Background(), "sample-"+formatInt(f3Ultimagen))

		convey.Convey("when SampleProgress is called, then runs contains the Ultimagen timeline built from the run metrics via id_run", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(progress.Platforms, convey.ShouldResemble, []string{"Ultimagen"})

			ultimagenRuns := make([]RunStatusTimeline, 0, len(progress.Runs))
			for _, run := range progress.Runs {
				if run.Platform == "Ultimagen" {
					ultimagenRuns = append(ultimagenRuns, run)
				}
			}
			convey.So(ultimagenRuns, convey.ShouldHaveLength, 1)

			ultimagen := ultimagenRuns[0]
			convey.So(ultimagen.IDRun, convey.ShouldEqual, 0)
			convey.So(ultimagen.NotTracked, convey.ShouldEqual, "")

			phases := make([]string, len(ultimagen.Events))
			entered := make([]string, len(ultimagen.Events))
			for i, event := range ultimagen.Events {
				phases[i] = event.Phase
				entered[i] = event.EnteredAt
			}
			convey.So(phases, convey.ShouldResemble, []string{f3UltimagenRunStatus, f3UltimagenRunStatus})
			convey.So(entered[0], convey.ShouldEqual, f3UltimagenStatusBase.Format(utcRFC3339Layout))
			convey.So(ultimagen.Events[0].Duration, convey.ShouldEqual, "PT1H")
			convey.So(ultimagen.Events[len(ultimagen.Events)-1].Duration, convey.ShouldEqual, "")
			convey.So(ultimagen.Current, convey.ShouldEqual, f3UltimagenRunStatus)
		})
	})
}

// seedF3ProgressScenario extends the reusable HARD REQ 7 scenario seed (the F1
// baseline + F2 run-status portions) for the unified-progress endpoint: it adds
// seq_ops_tracking_per_sample_mirror rows (the delivered sample filled
// manifest_created..sequencing_run_start with sequencing_qc_complete NULL, and
// the sequenced sample filled through library_complete with sequencing_run_start
// NULL), deliberately leaves the ONT and multi-platform samples ABSENT from
// tracking (so they exercise the less-detail path), seeds a PacBio
// run-well-metrics source for the multi-platform sample's PacBio product (so its
// per-platform timeline is derivable), and marks the feeding sync tables synced
// so cache_synced_at and the resolver/never-synced cascade resolve.
func seedF3ProgressScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedF2RunStatusScenario(t, db)

	// Mark the remaining feeding tables synced (F2 already marked
	// iseq_product_metrics) so the sample resolver passes the synced cascade and
	// cache_synced_at has a populated feeding set.
	seedSyncState(t, db, syncTableSample, time.Date(2026, time.June, 27, 10, 0, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableSeqProductIRODSLocations, time.Date(2026, time.June, 27, 11, 0, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableSeqOpsTrackingPerSample, time.Date(2026, time.June, 27, 9, 0, 0, 0, time.UTC))

	// Tracked sample (F3 test 1 + test 3): filled manifest_created..
	// sequencing_run_start, sequencing_qc_complete NULL. Each milestone is one day
	// after the previous, so duration_to_next is P1D and the open milestone
	// (sequencing_run_start) has an empty duration_to_next.
	seedSeqOpsTrackingPerSampleMirrorRow(t, db, f3DeliveredSampleLims, "S1", map[string]time.Time{
		"manifest_created":     f3MilestoneBase,
		"manifest_uploaded":    f3MilestoneBase.AddDate(0, 0, 1),
		"labware_received":     f3MilestoneBase.AddDate(0, 0, 2),
		"order_made":           f3MilestoneBase.AddDate(0, 0, 3),
		"working_dilution":     f3MilestoneBase.AddDate(0, 0, 4),
		"library_start":        f3MilestoneBase.AddDate(0, 0, 5),
		"library_complete":     f3MilestoneBase.AddDate(0, 0, 6),
		"sequencing_run_start": f3MilestoneBase.AddDate(0, 0, 7),
	})

	// Tracked sample (F3 test 5): filled through library_complete with
	// sequencing_run_start NULL, so current_milestone is library_complete.
	seedSeqOpsTrackingPerSampleMirrorRow(t, db, f3LibraryCompleteSampleLims, "S1", map[string]time.Time{
		"manifest_created":  f3MilestoneBase,
		"manifest_uploaded": f3MilestoneBase.AddDate(0, 0, 1),
		"labware_received":  f3MilestoneBase.AddDate(0, 0, 2),
		"order_made":        f3MilestoneBase.AddDate(0, 0, 3),
		"working_dilution":  f3MilestoneBase.AddDate(0, 0, 4),
		"library_start":     f3MilestoneBase.AddDate(0, 0, 5),
		"library_complete":  f3MilestoneBase.AddDate(0, 0, 6),
	})

	// PacBio run-status source for the multi-platform sample's PacBio product
	// (id_pac_bio_rw_metrics_tmp == id_sample_tmp, matching
	// seedPacBioProductMetricsMirrorRow): four dated lifecycle columns one hour
	// apart, the run-level dates carrying run_status and the well-level dates
	// well_status, so the embedded PacBio timeline has clean PT1H deltas.
	seedPacBioRunWellMetricsMirrorRow(t, db, f1MultiPlatform, f3PacBioRunStatus, f3PacBioWellStatus, map[string]time.Time{
		"run_start":     f3PacBioStatusBase,
		"run_complete":  f3PacBioStatusBase.Add(time.Hour),
		"well_complete": f3PacBioStatusBase.Add(2 * time.Hour),
		"qc_seq_date":   f3PacBioStatusBase.Add(3 * time.Hour),
	})

	seedF3ElembioAndUltimagenRunStatus(t, db)
}

// seedF2RunStatusScenario extends the reusable HARD REQ 7 scenario seed (the F1
// baseline portion) with run 52553's ordered iseq_run_status timeline and the
// dict it references. It marks iseq_product_metrics as synced and seeds a
// product-metrics row for the run so ResolveRun resolves it (the Illumina id_run
// space) and an absent run yields ErrNotFound rather than a never-synced cascade.
// The timeline is ordered by date with a recurrence, an on-hold event, an
// unknown dict description, and a source iscurrent=1 on an earlier-dated row than
// the latest, so every F2 property is exercised. Item 4.3 reuses this run.
func seedF2RunStatusScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedF1BaselineScenario(t, db)
	seedSyncState(t, db, syncTableIseqProductMetrics, time.Date(2026, time.June, 27, 12, 0, 0, 0, time.UTC))

	// A product-metrics row keys the run into the Illumina id_run space so
	// ResolveRun finds it; its sample/study are part of the seeded study S1.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 55301, f1DeliveredPass, f2RunStatus, 1, 1, "S1", sql.NullInt64{Int64: 1, Valid: true})

	// The dict the timeline references; "experimental new status" is an unknown
	// (open-vocabulary) description that must pass through verbatim.
	dict := []struct {
		id          int64
		description string
	}{
		{1, f2PhasePending},
		{2, f2PhaseInProgress},
		{3, f2PhaseComplete},
		{4, f2PhaseAnalysis},
		{5, f2PhaseOnHold},
		{6, f2PhaseUnknown},
		{7, f2PhaseAnalysisOK},
		{8, f2PhaseQCReview},
	}
	for _, entry := range dict {
		seedIseqRunStatusDictMirrorRow(t, db, entry.id, entry.description)
	}

	// The ordered timeline (one hour apart). iscurrent=1 sits on the on-hold row
	// (hour 4), an EARLIER date than the latest (hour 8) qc-review row, so the
	// derived current must be qc review pending, not on hold.
	status := []struct {
		idRunStatus     int64
		hour            int
		idRunStatusDict int64
		iscurrent       int
	}{
		{5301, 0, 1, 0},
		{5302, 1, 2, 0},
		{5303, 2, 3, 0},
		{5304, 3, 4, 0},
		{5305, 4, 5, 1},
		{5306, 5, 4, 0},
		{5307, 6, 6, 0},
		{5308, 7, 7, 0},
		{5309, 8, 8, 0},
	}
	for _, row := range status {
		seedIseqRunStatusMirrorRow(t, db, row.idRunStatus, f2RunStatus, f2StatusBase.Add(time.Duration(row.hour)*time.Hour), row.idRunStatusDict, row.iscurrent)
	}
}

// seedF1BaselineScenario builds the F1 portion of the HARD REQ 7 scenario seed:
// one study "S1" linking six samples that exercise every baseline path --
// registered-only, sequenced with NULL qc (pending), delivered with qc=1 (pass)
// across two iRODS dates, a mixed qc=0/qc=1 fail roll-up, an ONT sample
// (oseq_flowcell only), and a multi-platform sample delivered on Illumina but
// sequenced-only on PacBio. It reuses the Phase 1/2 seeders and is reusable: a
// caller (Items 4.2-4.4) can extend it with tracking-mirror and run-status rows.
func seedF1BaselineScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 300, "S1")

	for _, id := range []int64{f1RegisteredOnly, f1SequencedNULLQC, f1DeliveredPass, f1FailMixedQC, f1ONT, f1MultiPlatform} {
		seedHierarchySample(t, db, id, "S1", "sample-"+formatInt(id))
		seedLibrarySample(t, db, "Standard", id, "S1")
	}

	// Registered-only sample: the library link above is its only row.

	// Sequenced sample with NULL qc and no iRODS -> sequenced / pending.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 30201, f1SequencedNULLQC, 52601, 1, 1, "S1", sql.NullInt64{})

	// Delivered sample: products qc=1, two iRODS rows on distinct dates ->
	// delivered / pass, delivered_at the earlier date.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 30301, f1DeliveredPass, 52601, 2, 1, "S1", sql.NullInt64{Int64: 1, Valid: true})
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "30301", "/seq/52601", "52601_2#1.cram", f1DeliveredPass, "S1", f1DeliveredLatest, "illumina")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "30302", "/seq/52601", "52601_2#2.cram", f1DeliveredPass, "S1", f1DeliveredEarliest, "illumina")

	// Mixed-qc sample: one product qc=0 and one qc=1 -> any-fail rolls up to fail.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 30401, f1FailMixedQC, 52601, 3, 1, "S1", sql.NullInt64{Int64: 0, Valid: true})
	seedIseqProductMetricsMirrorRowWithQC(t, db, 30402, f1FailMixedQC, 52601, 3, 2, "S1", sql.NullInt64{Int64: 1, Valid: true})

	// ONT sample: identity/study only, no products and no iRODS.
	seedOseqFlowcellMirrorRow(t, db, 30501, f1ONT, "S1")

	// Multi-platform sample: delivered on Illumina (product + iRODS), sequenced
	// only on PacBio (product, no iRODS) -> most-advanced is delivered, both
	// platforms reported.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 30601, f1MultiPlatform, 52601, 4, 1, "S1", sql.NullInt64{Int64: 1, Valid: true})
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "30601", "/seq/52601", "52601_4#1.cram", f1MultiPlatform, "S1", f1DeliveredLatest, "illumina")
	seedPacBioProductMetricsMirrorRow(t, db, "30602", f1MultiPlatform, "S1")
}

// F4 acceptance test 1: study S1 (3 delivered, 1 sequenced-no-data, 1 registered;
// 2 of them in the tracking mirror) yields distinct == {with_data:3,
// sequenced_no_data:1, registered:1} summing to samples_total (5), and
// with_detailed_timeline == 2.
func TestStatusBreakdownDistinctLadderSumsToTotal(t *testing.T) {
	convey.Convey("Given the F4 status-breakdown scenario: 3 delivered, 1 sequenced-no-data, 1 registered, 2 of them tracked", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF4StatusBreakdownScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		breakdown, err := client.StatusBreakdown(context.Background(), f4StudyLims)

		convey.Convey("when StatusBreakdown is called, then distinct is {3,1,1} summing to 5 and with_detailed_timeline is 2", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(breakdown.IDStudyLims, convey.ShouldEqual, f4StudyLims)
			convey.So(breakdown.Distinct, convey.ShouldResemble, PhaseLadder{WithData: 3, SequencedNoData: 1, Registered: 1})

			sum := breakdown.Distinct.WithData + breakdown.Distinct.SequencedNoData + breakdown.Distinct.Registered
			convey.So(sum, convey.ShouldEqual, 5)
			convey.So(breakdown.WithDetailedTimeline, convey.ShouldEqual, 2)
			convey.So(breakdown.CacheSyncedAt, convey.ShouldNotEqual, "")
		})
	})
}

// F4 acceptance test 2: a multi-platform sample in S1 delivered on Illumina but
// only sequenced on PacBio counts under Illumina with_data AND under PacBio
// sequenced_no_data in per_platform (so the grand total exceeds samples_total),
// while in distinct it counts once under with_data (its most-advanced phase).
func TestStatusBreakdownPerPlatformCountsMultiPlatformUnderBoth(t *testing.T) {
	convey.Convey("Given the F4 status-breakdown scenario with a sample delivered on Illumina but only sequenced on PacBio", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF4StatusBreakdownScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		breakdown, err := client.StatusBreakdown(context.Background(), f4StudyLims)

		convey.Convey("when StatusBreakdown is called, then per_platform counts it under Illumina with_data and PacBio sequenced_no_data, the grand total exceeds samples_total, and distinct counts it once under with_data", func() {
			convey.So(err, convey.ShouldBeNil)

			ladders := make(map[string]PhaseLadder, len(breakdown.PerPlatform))
			for _, entry := range breakdown.PerPlatform {
				ladders[entry.Platform] = entry.Ladder
			}

			convey.So(ladders["Illumina"], convey.ShouldResemble, PhaseLadder{WithData: 3, SequencedNoData: 1, Registered: 0})
			convey.So(ladders["PacBio"], convey.ShouldResemble, PhaseLadder{WithData: 0, SequencedNoData: 1, Registered: 0})

			grandTotal := 0
			for _, entry := range breakdown.PerPlatform {
				grandTotal += entry.Ladder.WithData + entry.Ladder.SequencedNoData + entry.Ladder.Registered
			}
			distinctTotal := breakdown.Distinct.WithData + breakdown.Distinct.SequencedNoData + breakdown.Distinct.Registered
			convey.So(grandTotal, convey.ShouldBeGreaterThan, distinctTotal)

			// In the distinct partition the multi-platform sample is counted once,
			// under with_data (most-advanced), so with_data stays 3 (not 4).
			convey.So(breakdown.Distinct.WithData, convey.ShouldEqual, 3)
		})
	})
}

// F4 per_platform with_data discrimination (non-Illumina): a PacBio-only sample
// genuinely DELIVERED on PacBio -- a PacBio product plus a study-scoped iRODS row
// linked to it by the shared product id -- must land in PacBio's with_data bucket.
// Without this the per_platform PacBio with_data branch passes only by absence
// (the existing scenario's PacBio sample is sequenced_no_data), so the non-Illumina
// delivery join (id_pac_bio_product = spi.id_iseq_product) is never positively
// exercised. The distinct partition still sums to samples_total (now 6) with the
// delivered PacBio sample counted once under with_data.
func TestStatusBreakdownPerPlatformNonIlluminaWithData(t *testing.T) {
	convey.Convey("Given the F4 status-breakdown scenario plus a PacBio-only sample delivered on PacBio", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF4StatusBreakdownScenario(t, cache.DB())
		seedF4PacBioDeliveredSample(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		breakdown, err := client.StatusBreakdown(context.Background(), f4StudyLims)

		convey.Convey("when StatusBreakdown is called, then per_platform counts the PacBio sample under PacBio with_data (positively, not by absence) and distinct still sums to samples_total", func() {
			convey.So(err, convey.ShouldBeNil)

			ladders := make(map[string]PhaseLadder, len(breakdown.PerPlatform))
			for _, entry := range breakdown.PerPlatform {
				ladders[entry.Platform] = entry.Ladder
			}

			// PacBio now has BOTH a delivered sample (the new PacBio-only one) and a
			// sequenced-only one (the multi-platform sample), so its with_data is a
			// positive 1, proving the non-Illumina delivery join, not true-by-absence.
			convey.So(ladders["PacBio"], convey.ShouldResemble, PhaseLadder{WithData: 1, SequencedNoData: 1, Registered: 0})
			// Illumina is unchanged by the PacBio-only addition.
			convey.So(ladders["Illumina"], convey.ShouldResemble, PhaseLadder{WithData: 3, SequencedNoData: 1, Registered: 0})

			// The delivered PacBio sample is its own distinct sample, so distinct
			// with_data rises to 4 and the three buckets still sum to samples_total (6).
			convey.So(breakdown.Distinct, convey.ShouldResemble, PhaseLadder{WithData: 4, SequencedNoData: 1, Registered: 1})
			sum := breakdown.Distinct.WithData + breakdown.Distinct.SequencedNoData + breakdown.Distinct.Registered
			convey.So(sum, convey.ShouldEqual, 6)
		})
	})
}

// F4 acceptance test 3: the ONT sample linked to S1 is counted in registered (not
// as a separate without-data negative), and the distinct buckets still sum to
// samples_total.
func TestStatusBreakdownONTCountedInRegistered(t *testing.T) {
	convey.Convey("Given the F4 status-breakdown scenario with an ONT sample linked to the study", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF4StatusBreakdownScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		breakdown, err := client.StatusBreakdown(context.Background(), f4StudyLims)

		convey.Convey("when StatusBreakdown is called, then the ONT sample is in registered and distinct still sums to samples_total", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(breakdown.Distinct.Registered, convey.ShouldEqual, 1)

			sum := breakdown.Distinct.WithData + breakdown.Distinct.SequencedNoData + breakdown.Distinct.Registered
			convey.So(sum, convey.ShouldEqual, 5)

			ladders := make(map[string]PhaseLadder, len(breakdown.PerPlatform))
			for _, entry := range breakdown.PerPlatform {
				ladders[entry.Platform] = entry.Ladder
			}
			convey.So(ladders["ONT"], convey.ShouldResemble, PhaseLadder{WithData: 0, SequencedNoData: 0, Registered: 1})
		})
	})
}

// F4 acceptance test 4: a never-synced cache yields an error satisfying both
// ErrCacheNeverSynced and ErrNotFound; an unknown study yields ErrNotFound; a
// synced but empty study yields all-zero ladders with cache_synced_at populated
// and no error (the CountSamplesForStudy cascade).
func TestStatusBreakdownNeverSyncedUnknownAndEmptyCascade(t *testing.T) {
	convey.Convey("Given a never-synced cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, err := client.StatusBreakdown(context.Background(), f4StudyLims)

		convey.Convey("when StatusBreakdown is called, then the error satisfies both ErrCacheNeverSynced and ErrNotFound", func() {
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given a synced F4 scenario and an unknown study id", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF4StatusBreakdownScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, err := client.StatusBreakdown(context.Background(), "no-such-study")

		convey.Convey("when StatusBreakdown is called, then ErrNotFound (not never-synced)", func() {
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
		})
	})

	convey.Convey("Given a synced cache with a study that has no linked samples", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// Seed the study row and mark the membership sync tables synced, but link
		// no samples, so the empty-study branch returns all-zero ladders.
		seedHierarchyStudy(t, cache.DB(), f4StudyTmp, f4StudyLims)
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.June, 27, 9, 0, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.June, 27, 10, 0, 0, 0, time.UTC))
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		breakdown, err := client.StatusBreakdown(context.Background(), f4StudyLims)

		convey.Convey("when StatusBreakdown is called, then it returns all-zero ladders with cache_synced_at populated and no error", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(breakdown.IDStudyLims, convey.ShouldEqual, f4StudyLims)
			convey.So(breakdown.Distinct, convey.ShouldResemble, PhaseLadder{})
			convey.So(breakdown.PerPlatform, convey.ShouldBeEmpty)
			convey.So(breakdown.WithDetailedTimeline, convey.ShouldEqual, 0)
			convey.So(breakdown.CacheSyncedAt, convey.ShouldNotEqual, "")
		})
	})
}

// F4 wiring (G1 acceptance test 2): the server.go handler for the new
// /study/:id/status-breakdown endpoint returns the SAME value as the Client
// method (the switch has a case for the new Method; no panic), proving the
// endpoint is fully wired through the four-step recipe.
func TestStatusBreakdownServerHandlerMatchesClient(t *testing.T) {
	convey.Convey("Given a server over a synced cache seeded with the F4 status-breakdown scenario", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedF4StatusBreakdownScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when GET /study/S4/status-breakdown is served, then it returns the same StatusBreakdown as the Client method", func() {
			expected, err := client.StatusBreakdown(context.Background(), f4StudyLims)
			convey.So(err, convey.ShouldBeNil)

			response := performMLWHRequestForTest(t, client, http.MethodGet, "/study/"+f4StudyLims+"/status-breakdown")
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var got StatusBreakdown
			decodeMLWHJSONResponseForTest(t, response, &got)
			convey.So(got, convey.ShouldResemble, expected)
		})
	})
}

// seedF4StatusBreakdownScenario builds the F4 status-breakdown study (LIMS id
// f4StudyLims): five library-linked samples whose distinct partition is exactly
// {with_data:3, sequenced_no_data:1, registered:1} summing to 5 -- three delivered
// (one of them multi-platform: Illumina-delivered + PacBio-sequenced-only), one
// sequenced-no-data, one ONT (registered) -- with two of them (a delivered and the
// sequenced sample) present in the tracking mirror (with_detailed_timeline == 2).
// It reuses the shared low-level seed helpers and marks every feeding sync table
// synced so cache_synced_at and the never-synced/unknown/empty cascade resolve.
func seedF4StatusBreakdownScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, f4StudyTmp, f4StudyLims)

	for _, id := range []int64{f4Delivered1, f4Delivered2, f4MultiPlatform, f4SequencedNoData, f4ONT} {
		seedHierarchySample(t, db, id, f4StudyLims, "sample-"+formatInt(id))
		seedLibrarySample(t, db, "Standard", id, f4StudyLims)
	}

	// Two plain Illumina-delivered samples: a product (qc=1) plus a study-scoped
	// iRODS row linked to it by the shared product id -> with_data.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 40101, f4Delivered1, 54401, 1, 1, f4StudyLims, sql.NullInt64{Int64: 1, Valid: true})
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "40101", "/seq/54401", "54401_1#1.cram", f4Delivered1, f4StudyLims, f4DeliveredCreated, "illumina")

	seedIseqProductMetricsMirrorRowWithQC(t, db, 40201, f4Delivered2, 54401, 2, 1, f4StudyLims, sql.NullInt64{Int64: 1, Valid: true})
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "40201", "/seq/54401", "54401_2#1.cram", f4Delivered2, f4StudyLims, f4DeliveredCreated, "illumina")

	// Multi-platform sample: delivered on Illumina (product 40301 + iRODS linked to
	// it), sequenced-only on PacBio (product 40302, NO iRODS) -> distinct with_data
	// (most-advanced), per-platform Illumina with_data + PacBio sequenced_no_data.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 40301, f4MultiPlatform, 54401, 3, 1, f4StudyLims, sql.NullInt64{Int64: 1, Valid: true})
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "40301", "/seq/54401", "54401_3#1.cram", f4MultiPlatform, f4StudyLims, f4DeliveredCreated, "illumina")
	seedPacBioProductMetricsMirrorRow(t, db, "40302", f4MultiPlatform, f4StudyLims)

	// Sequenced-no-data sample: an Illumina product, no iRODS row -> sequenced_no_data.
	seedIseqProductMetricsMirrorRowWithQC(t, db, 40401, f4SequencedNoData, 54401, 4, 1, f4StudyLims, sql.NullInt64{})

	// ONT sample: oseq_flowcell identity only, no products/iRODS -> registered.
	seedOseqFlowcellMirrorRow(t, db, 40501, f4ONT, f4StudyLims)

	// Two of the five samples in the tracking mirror (keyed by id_sample_lims =
	// id_sample_tmp+100) -> with_detailed_timeline == 2.
	seedSeqOpsTrackingPerSampleMirrorRow(t, db, formatInt(f4Delivered1+100), f4StudyLims, map[string]time.Time{
		"manifest_created": f3MilestoneBase,
		"library_complete": f3MilestoneBase.AddDate(0, 0, 1),
	})
	seedSeqOpsTrackingPerSampleMirrorRow(t, db, formatInt(f4SequencedNoData+100), f4StudyLims, map[string]time.Time{
		"manifest_created": f3MilestoneBase,
	})

	// Mark every feeding sync table synced so cache_synced_at is populated and the
	// never-synced/unknown/empty cascade resolves (study + sample identity, the
	// product-metrics mirrors, the iRODS locations mirror, and the tracking mirror).
	seedSyncState(t, db, syncTableStudy, time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableSample, time.Date(2026, time.June, 27, 9, 0, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableIseqFlowcell, time.Date(2026, time.June, 27, 9, 30, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableIseqProductMetrics, time.Date(2026, time.June, 27, 10, 0, 0, 0, time.UTC))
	seedSyncState(t, db, syncTablePacBioProductMetrics, time.Date(2026, time.June, 27, 10, 15, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableEseqProductMetrics, time.Date(2026, time.June, 27, 10, 30, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableUseqProductMetrics, time.Date(2026, time.June, 27, 10, 45, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableSeqProductIRODSLocations, time.Date(2026, time.June, 27, 11, 0, 0, 0, time.UTC))
	seedSyncState(t, db, syncTableSeqOpsTrackingPerSample, time.Date(2026, time.June, 27, 7, 0, 0, 0, time.UTC))
}

// seedF4PacBioDeliveredSample augments the F4 scenario with one PacBio-only sample
// genuinely DELIVERED on PacBio: a library link (so it joins the study's distinct
// partition), a PacBio product-metrics row, and a study-scoped iRODS row whose
// id_iseq_product equals that PacBio product's id_pac_bio_product. The shared
// product id is the per-platform with_data join (id_pac_bio_product =
// spi.id_iseq_product), so this sample lands in PacBio with_data -- positively
// exercising the non-Illumina delivery path rather than passing by absence. It adds
// one distinct with_data sample (samples_total becomes 6). It is deliberately NOT
// folded into seedF4StatusBreakdownScenario so the spec's pinned {3,1,1}-summing-to-5
// acceptance tests keep their exact counts.
func seedF4PacBioDeliveredSample(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchySample(t, db, f4PacBioDelivered, f4StudyLims, "sample-"+formatInt(f4PacBioDelivered))
	seedLibrarySample(t, db, "Standard", f4PacBioDelivered, f4StudyLims)

	seedPacBioProductMetricsMirrorRow(t, db, "40601", f4PacBioDelivered, f4StudyLims)
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "40601", "/seq/54601", "54601_1#1.cram", f4PacBioDelivered, f4StudyLims, f4DeliveredCreated, "pacbio")
}

// seedIseqProductMetricsMirrorRowWithQC inserts an Illumina product-metrics
// mirror row with an explicit (possibly NULL) overall qc, so the F1 QC roll-up
// can exercise pass / fail / pending / not_tracked distinctly. The existing
// seedIseqProductMetricsMirrorRow always sets qc=1; this variant lets a test
// seed NULL (pending) and 0 (fail) products.
func seedIseqProductMetricsMirrorRowWithQC(t *testing.T, db *sql.DB, idIseqProduct, idSampleTmp int64, idRun, position, tagIndex int, idStudyLims string, qc sql.NullInt64) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idIseqProduct,
		idSampleTmp,
		idRun,
		position,
		tagIndex,
		idSampleTmp,
		idStudyLims,
		qc,
		qc,
		qc,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedIseqProductMetricsMirrorRowWithQC(): %v", err)
	}
}

// seedSeqOpsTrackingPerSampleMirrorRow inserts one seq_ops_tracking_per_sample
// mirror row keyed by id_sample_lims, setting only the milestone datetime columns
// named in milestones (RFC3339 via formatSyncTime, the way the sync stores them)
// and leaving the rest SQL NULL, so a test can fill a sample to an arbitrary
// milestone shape. The non-milestone lookup/context columns are given seed
// defaults to satisfy the NOT NULL constraints.
func seedSeqOpsTrackingPerSampleMirrorRow(t *testing.T, db *sql.DB, idSampleLims, studyID string, milestones map[string]time.Time) {
	t.Helper()

	columns := []string{"id_sample_lims", "sanger_sample_id", "sanger_sample_name", "study_id", "programme", "faculty_sponsor", "library_type", "platform"}
	args := []any{idSampleLims, "ssid-" + idSampleLims, "ssname-" + idSampleLims, studyID, "programme", "faculty", "Standard", "Illumina"}

	for _, name := range theNineMilestonesInOrder {
		columns = append(columns, name)
		if reached, ok := milestones[name]; ok {
			args = append(args, formatSyncTime(reached))
		} else {
			args = append(args, nil)
		}
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",")
	query := `INSERT INTO seq_ops_tracking_per_sample_mirror(` + strings.Join(columns, ", ") + `) VALUES (` + placeholders + `)`
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("seedSeqOpsTrackingPerSampleMirrorRow(): %v", err)
	}
}

// seedIseqRunStatusDictMirrorRow inserts one iseq_run_status_dict_mirror row. The
// temporal_index is set from the id for determinism but is intentionally unused
// by the timeline (ordering is strictly by date), so an unknown description with
// any temporal_index still passes through.
func seedIseqRunStatusDictMirrorRow(t *testing.T, db *sql.DB, idRunStatusDict int64, description string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_run_status_dict_mirror(id_run_status_dict, description, temporal_index) VALUES (?, ?, ?)`,
		idRunStatusDict,
		description,
		idRunStatusDict,
	)
	if err != nil {
		t.Fatalf("seedIseqRunStatusDictMirrorRow(): %v", err)
	}
}

// seedIseqRunStatusMirrorRow inserts one iseq_run_status_mirror row, storing the
// date the way the sync does (formatSyncTime, UTC RFC3339Nano) so the read path
// round-trips it identically.
func seedIseqRunStatusMirrorRow(t *testing.T, db *sql.DB, idRunStatus, idRun int64, date time.Time, idRunStatusDict int64, iscurrent int) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_run_status_mirror(id_run_status, id_run, date, id_run_status_dict, iscurrent) VALUES (?, ?, ?, ?, ?)`,
		idRunStatus,
		idRun,
		formatSyncTime(date),
		idRunStatusDict,
		iscurrent,
	)
	if err != nil {
		t.Fatalf("seedIseqRunStatusMirrorRow(): %v", err)
	}
}

// seedPacBioRunWellMetricsMirrorRow inserts one pac_bio_run_well_metrics mirror
// row whose id_pac_bio_rw_metrics_tmp equals idPacBioRWMetrics (the same value
// seedPacBioProductMetricsMirrorRow uses for the product's
// id_pac_bio_rw_metrics_tmp, so a PacBio product links to it), carrying the native
// run_status / well_status and only the dated lifecycle columns named in dates
// (RFC3339 via formatSyncTime), the rest left SQL NULL. It is the per-platform
// status source the unified progress endpoint reads for a PacBio run.
func seedPacBioRunWellMetricsMirrorRow(t *testing.T, db *sql.DB, idPacBioRWMetrics int64, runStatus, wellStatus string, dates map[string]time.Time) {
	t.Helper()

	dated := func(name string) any {
		if value, ok := dates[name]; ok {
			return formatSyncTime(value)
		}

		return nil
	}

	_, err := db.Exec(
		`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, run_start, run_complete, well_complete, qc_seq_date, run_status, well_status, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idPacBioRWMetrics,
		"pb-run-"+formatInt(idPacBioRWMetrics),
		"A01",
		dated("run_start"),
		dated("run_complete"),
		dated("well_complete"),
		dated("qc_seq_date"),
		runStatus,
		wellStatus,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedPacBioRunWellMetricsMirrorRow(): %v", err)
	}
}

// seedF3ElembioAndUltimagenRunStatus extends the scenario with an Elembio sample
// and an Ultimagen sample, each linked to study S1 and carrying a product-metrics
// mirror row whose id_run joins to a matching run-status mirror row, so each
// sample's per-run within-sequencing timeline is derivable via id_run (the
// authoritative join the real ml_warehouse declares):
// eseq_product_metrics_mirror.id_run -> eseq_run_lane_metrics_mirror.id_run and
// useq_product_metrics_mirror.id_run -> useq_run_metrics_mirror.id_run. Both
// samples are left ABSENT from the tracking mirror (their per-platform timeline,
// not milestones, is the point). The Elembio lane metrics carry dated
// run_started/run_complete columns (Elembio has no native run_status string); the
// Ultimagen run metrics carry a native run_status plus run_start/run_complete.
func seedF3ElembioAndUltimagenRunStatus(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, id := range []int64{f3Elembio, f3Ultimagen} {
		seedHierarchySample(t, db, id, "S1", "sample-"+formatInt(id))
		seedLibrarySample(t, db, "Standard", id, "S1")
	}

	// Elembio: a product on run f3ElembioIDRun, and the lane metrics for that run
	// with run_started then run_complete one hour apart (clean PT1H delta).
	seedEseqProductMetricsMirrorRow(t, db, "eseq-"+formatInt(f3Elembio), f3ElembioIDRun, f3Elembio, "S1")
	seedEseqRunLaneMetricsMirrorRow(t, db, f3ElembioIDRun, map[string]time.Time{
		"run_started":  f3ElembioStatusBase,
		"run_complete": f3ElembioStatusBase.Add(time.Hour),
	})

	// Ultimagen: a product on run f3UltimagenIDRun, and the run metrics for that
	// run carrying the native run_status plus run_start then run_complete.
	seedUseqProductMetricsMirrorRow(t, db, "useq-"+formatInt(f3Ultimagen), f3UltimagenIDRun, f3Ultimagen, "S1")
	seedUseqRunMetricsMirrorRow(t, db, f3UltimagenIDRun, f3UltimagenRunStatus, map[string]time.Time{
		"run_start":    f3UltimagenStatusBase,
		"run_complete": f3UltimagenStatusBase.Add(time.Hour),
	})
}

// seedEseqProductMetricsMirrorRow inserts an Elembio product-metrics mirror row
// linking a sample to a study and carrying the id_run that joins to
// eseq_run_lane_metrics_mirror for the run-status timeline. qc is left NULL
// (pending) since these tests exercise the timeline, not the QC roll-up.
func seedEseqProductMetricsMirrorRow(t *testing.T, db *sql.DB, idEseqProduct string, idRun, idSampleTmp int64, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO eseq_product_metrics_mirror(id_eseq_product, id_eseq_flowcell_tmp, id_run, id_sample_tmp, id_study_lims, qc, qc_seq, qc_lib, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idEseqProduct,
		idSampleTmp,
		idRun,
		idSampleTmp,
		idStudyLims,
		nil,
		nil,
		nil,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedEseqProductMetricsMirrorRow(): %v", err)
	}
}

// seedEseqRunLaneMetricsMirrorRow inserts one eseq_run_lane_metrics mirror row
// keyed by id_run (the Elembio run-status join key), setting only the dated
// lifecycle columns named in dates (RFC3339 via formatSyncTime) and leaving the
// rest SQL NULL. Elembio carries no native run_status string, so the timeline's
// phase is derived from the lifecycle column name.
func seedEseqRunLaneMetricsMirrorRow(t *testing.T, db *sql.DB, idRun int64, dates map[string]time.Time) {
	t.Helper()

	dated := func(name string) any {
		if value, ok := dates[name]; ok {
			return formatSyncTime(value)
		}

		return nil
	}

	_, err := db.Exec(
		`INSERT INTO eseq_run_lane_metrics_mirror(id_run, lane, run_started, run_complete, last_updated) VALUES (?, ?, ?, ?, ?)`,
		idRun,
		int64(1),
		dated("run_started"),
		dated("run_complete"),
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedEseqRunLaneMetricsMirrorRow(): %v", err)
	}
}

// seedUseqProductMetricsMirrorRow inserts an Ultimagen product-metrics mirror row
// linking a sample to a study and carrying the id_run that joins to
// useq_run_metrics_mirror for the run-status timeline.
func seedUseqProductMetricsMirrorRow(t *testing.T, db *sql.DB, idUseqProduct string, idRun, idSampleTmp int64, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO useq_product_metrics_mirror(id_useq_product, id_useq_wafer_tmp, id_run, id_sample_tmp, id_study_lims, qc, qc_seq, qc_lib, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idUseqProduct,
		idSampleTmp,
		idRun,
		idSampleTmp,
		idStudyLims,
		nil,
		nil,
		nil,
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedUseqProductMetricsMirrorRow(): %v", err)
	}
}

// seedUseqRunMetricsMirrorRow inserts one useq_run_metrics mirror row keyed by
// id_run (the Ultimagen run-status join key), carrying the native run_status and
// only the dated lifecycle columns named in dates (RFC3339 via formatSyncTime),
// the rest left SQL NULL.
func seedUseqRunMetricsMirrorRow(t *testing.T, db *sql.DB, idRun int64, runStatus string, dates map[string]time.Time) {
	t.Helper()

	dated := func(name string) any {
		if value, ok := dates[name]; ok {
			return formatSyncTime(value)
		}

		return nil
	}

	_, err := db.Exec(
		`INSERT INTO useq_run_metrics_mirror(id_run, run_name, run_status, run_start, run_complete, last_updated) VALUES (?, ?, ?, ?, ?, ?)`,
		idRun,
		"useq-run-"+formatInt(idRun),
		runStatus,
		dated("run_start"),
		dated("run_complete"),
		formatSyncTime(time.Date(2026, time.May, 6, 12, 10, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedUseqRunMetricsMirrorRow(): %v", err)
	}
}
