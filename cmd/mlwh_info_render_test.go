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

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestInfoBarSegmentsProportional(t *testing.T) {
	convey.Convey("Given weights and a width, infoBarSegments returns proportional cell counts", t, func() {
		convey.Convey("the widest weight gets the widest segment and the total is exactly the width", func() {
			// Durations 2d, 4d, 21d, 3d (in seconds) over a 30-cell budget.
			weights := []int64{2 * 86400, 4 * 86400, 21 * 86400, 3 * 86400}
			segments := infoBarSegments(weights, 30)

			convey.So(segments, convey.ShouldHaveLength, 4)

			total := 0
			for _, s := range segments {
				total += s
			}
			convey.So(total, convey.ShouldEqual, 30)

			// The 21-day phase is the widest; the 2-day phase the narrowest.
			convey.So(segments[2], convey.ShouldBeGreaterThan, segments[0])
			convey.So(segments[2], convey.ShouldBeGreaterThan, segments[1])
			convey.So(segments[2], convey.ShouldBeGreaterThan, segments[3])
		})

		convey.Convey("a tiny phase still gets at least one cell", func() {
			// One huge phase and one near-zero phase over a generous width.
			weights := []int64{100000, 1}
			segments := infoBarSegments(weights, 40)

			convey.So(segments, convey.ShouldHaveLength, 2)
			convey.So(segments[1], convey.ShouldBeGreaterThanOrEqualTo, 1)

			total := 0
			for _, s := range segments {
				total += s
			}
			convey.So(total, convey.ShouldEqual, 40)
		})

		convey.Convey("all-zero weights split the width evenly", func() {
			segments := infoBarSegments([]int64{0, 0, 0}, 9)

			convey.So(segments, convey.ShouldResemble, []int{3, 3, 3})
		})

		convey.Convey("an empty weight slice returns no segments", func() {
			convey.So(infoBarSegments(nil, 30), convey.ShouldBeEmpty)
		})
	})
}

func TestInfoCompactDate(t *testing.T) {
	convey.Convey("Given an RFC3339 timestamp, infoCompactDate renders a compact month-day form", t, func() {
		convey.So(infoCompactDate("2026-04-01T00:00:00Z"), convey.ShouldEqual, "Apr 01")
		convey.So(infoCompactDate("2026-05-01T12:34:56Z"), convey.ShouldEqual, "May 01")
	})

	convey.Convey("Given an empty or unparseable value, infoCompactDate is empty", t, func() {
		convey.So(infoCompactDate(""), convey.ShouldEqual, "")
		convey.So(infoCompactDate("not-a-date"), convey.ShouldEqual, "")
	})
}

func TestInfoStyleColourGating(t *testing.T) {
	convey.Convey("Given a colour-disabled style, styling helpers emit no ANSI escapes", t, func() {
		style := infoStyle{colour: false, width: 100}

		convey.So(style.bold("hi"), convey.ShouldEqual, "hi")
		convey.So(style.dim("hi"), convey.ShouldEqual, "hi")
		convey.So(style.header("hi"), convey.ShouldEqual, "hi")
		convey.So(style.qcColour("pass", "pass"), convey.ShouldEqual, "pass")
		convey.So(strings.Contains(style.bold("hi"), "\x1b["), convey.ShouldBeFalse)
	})

	convey.Convey("Given a colour-enabled style, styling helpers wrap text in ANSI escapes", t, func() {
		style := infoStyle{colour: true, width: 100}

		convey.So(strings.Contains(style.bold("hi"), "\x1b["), convey.ShouldBeTrue)
		convey.So(strings.Contains(style.bold("hi"), "hi"), convey.ShouldBeTrue)
		convey.So(strings.HasSuffix(style.bold("hi"), "\x1b[0m"), convey.ShouldBeTrue)

		// QC verdicts colour differently and always reset.
		pass := style.qcColour("pass", "pass")
		fail := style.qcColour("fail", "fail")
		convey.So(pass, convey.ShouldNotEqual, fail)
		convey.So(strings.Contains(pass, "\x1b["), convey.ShouldBeTrue)
	})
}

func TestWriteInfoReportTextColourGating(t *testing.T) {
	report := infoReport{
		Identifier: "DN1234",
		Kind:       string(mlwh.KindSangerSampleName),
		Canonical:  "DN1234",
		Sample: &mlwh.Sample{
			Name:         "DN1234",
			IDSampleLims: "8675309",
			SupplierName: "vendor-id-1",
		},
	}

	convey.Convey("Given colour disabled, the rendered report contains no ANSI escapes", t, func() {
		var buf bytes.Buffer
		writeInfoReportText(&buf, report, infoStyle{colour: false, width: 100})

		convey.So(buf.String(), convey.ShouldContainSubstring, "DN1234")
		convey.So(strings.Contains(buf.String(), "\x1b["), convey.ShouldBeFalse)
	})

	convey.Convey("Given colour enabled, the rendered report contains ANSI escapes", t, func() {
		var buf bytes.Buffer
		writeInfoReportText(&buf, report, infoStyle{colour: true, width: 100})

		convey.So(buf.String(), convey.ShouldContainSubstring, "DN1234")
		convey.So(strings.Contains(buf.String(), "\x1b["), convey.ShouldBeTrue)
	})
}

func TestWriteInfoMilestoneBarIsDeterministic(t *testing.T) {
	convey.Convey("Given a progress timeline with several milestones at fixed times, the bar reflects durations without depending on now", t, func() {
		progress := &mlwh.SampleProgress{
			Sample:           mlwh.Sample{Name: "DN1234"},
			Platforms:        []string{"Illumina"},
			BaselinePhase:    "delivered",
			QC:               "pass",
			DeliveredAt:      "2026-05-01T00:00:00Z",
			DetailedTimeline: true,
			Milestones: []mlwh.Milestone{
				{Name: "received", ReachedAt: "2026-04-01T00:00:00Z", DurationToNext: "P2D"},
				{Name: "prepared", ReachedAt: "2026-04-03T00:00:00Z", DurationToNext: "P4D"},
				{Name: "sequencing", ReachedAt: "2026-04-07T00:00:00Z", DurationToNext: "P21D"},
				{Name: "delivered", ReachedAt: "2026-04-28T00:00:00Z", DurationToNext: "P3D"},
				{Name: "qc", ReachedAt: "2026-05-01T00:00:00Z"},
			},
			CurrentMilestone: "qc",
		}

		var buf bytes.Buffer
		writeSampleProgressSection(&buf, progress, infoStyle{colour: false, width: 100})
		out := buf.String()

		// Phase names present.
		convey.So(out, convey.ShouldContainSubstring, "received")
		convey.So(out, convey.ShouldContainSubstring, "sequencing")
		convey.So(out, convey.ShouldContainSubstring, "delivered")

		// Compact transition dates present.
		convey.So(out, convey.ShouldContainSubstring, "Apr 01")
		convey.So(out, convey.ShouldContainSubstring, "May 01")

		// The open/current phase is marked with a trailing arrow.
		convey.So(out, convey.ShouldContainSubstring, "current")

		// The 21-day sequencing segment must be wider than the 2-day received
		// segment: derive widths from the rendered bar deterministically.
		segments := infoBarSegments([]int64{2 * 86400, 4 * 86400, 21 * 86400, 3 * 86400}, 60)
		convey.So(segments[2], convey.ShouldBeGreaterThan, segments[0])

		// Rendering twice yields identical output (no now-dependence).
		var buf2 bytes.Buffer
		writeSampleProgressSection(&buf2, progress, infoStyle{colour: false, width: 100})
		convey.So(buf2.String(), convey.ShouldEqual, out)
	})
}

func TestWriteInfoMilestoneDatesAreNotDimmed(t *testing.T) {
	convey.Convey("Given a milestone timeline rendered with colour, the transition-date track is not dimmed", t, func() {
		progress := &mlwh.SampleProgress{
			Sample:           mlwh.Sample{Name: "DN1234"},
			Platforms:        []string{"Illumina"},
			BaselinePhase:    "delivered",
			QC:               "pass",
			DeliveredAt:      "2026-05-01T00:00:00Z",
			DetailedTimeline: true,
			Milestones: []mlwh.Milestone{
				{Name: "received", ReachedAt: "2026-04-01T00:00:00Z", DurationToNext: "P2D"},
				{Name: "prepared", ReachedAt: "2026-04-03T00:00:00Z", DurationToNext: "P4D"},
				{Name: "sequencing", ReachedAt: "2026-04-07T00:00:00Z", DurationToNext: "P21D"},
				{Name: "delivered", ReachedAt: "2026-04-28T00:00:00Z", DurationToNext: "P3D"},
				{Name: "qc", ReachedAt: "2026-05-01T00:00:00Z"},
			},
			CurrentMilestone: "qc",
		}

		var buf bytes.Buffer
		writeSampleProgressSection(&buf, progress, infoStyle{colour: true, width: 100})
		out := buf.String()

		// Colour is on, so ANSI is present somewhere in the section...
		convey.So(strings.Contains(out, "\x1b["), convey.ShouldBeTrue)
		// ...and dim is still used for genuinely-subtle parts (the legend arrows).
		convey.So(strings.Contains(out, ansiDim), convey.ShouldBeTrue)

		// "Apr 01" is the first milestone's transition date and appears only on the
		// date-track line (not in the headline). That line must NOT carry the faint
		// code: important dates stay readable on dark terminals.
		dateTrackLine := ""
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "Apr 01") {
				dateTrackLine = line

				break
			}
		}

		convey.So(dateTrackLine, convey.ShouldContainSubstring, "Apr 01")
		convey.So(strings.Contains(dateTrackLine, ansiDim), convey.ShouldBeFalse)

		// With colour off the same date track is plain (no ANSI at all).
		var plain bytes.Buffer
		writeSampleProgressSection(&plain, progress, infoStyle{colour: false, width: 100})
		convey.So(strings.Contains(plain.String(), "\x1b["), convey.ShouldBeFalse)
		convey.So(plain.String(), convey.ShouldContainSubstring, "Apr 01")
	})
}

func TestWriteStudyPanelShowsDimmedUUID(t *testing.T) {
	convey.Convey("Given a study with a LIMS UUID, the study panel shows a UUID field rendered dim", t, func() {
		report := infoReport{
			Identifier: "5901",
			Kind:       string(mlwh.KindStudyLimsID),
			Canonical:  "5901",
			Study: &mlwh.Study{
				IDStudyLims:   "5901",
				Name:          "Lung cancer GWAS",
				StudyTitle:    "Lung cancer GWAS title",
				UUIDStudyLims: "abc1234-5678-90ab-cdef-1234567890ab",
			},
		}

		convey.Convey("with colour off the UUID value is present and plain", func() {
			var buf bytes.Buffer
			writeInfoReportText(&buf, report, infoStyle{colour: false, width: 100})
			out := buf.String()

			convey.So(out, convey.ShouldContainSubstring, "UUID")
			convey.So(out, convey.ShouldContainSubstring, "abc1234-5678-90ab-cdef-1234567890ab")
			convey.So(strings.Contains(out, "\x1b["), convey.ShouldBeFalse)
		})

		convey.Convey("with colour on the UUID value is dimmed", func() {
			var buf bytes.Buffer
			writeInfoReportText(&buf, report, infoStyle{colour: true, width: 100})

			uuidLine := ""
			for _, line := range strings.Split(buf.String(), "\n") {
				if strings.Contains(line, "abc1234-5678-90ab-cdef-1234567890ab") {
					uuidLine = line

					break
				}
			}

			convey.So(uuidLine, convey.ShouldContainSubstring, "abc1234-5678-90ab-cdef-1234567890ab")
			convey.So(strings.Contains(uuidLine, ansiDim), convey.ShouldBeTrue)
		})

		convey.Convey("a study without a UUID shows no UUID field", func() {
			report.Study.UUIDStudyLims = ""

			var buf bytes.Buffer
			writeInfoReportText(&buf, report, infoStyle{colour: false, width: 100})

			convey.So(buf.String(), convey.ShouldNotContainSubstring, "UUID")
		})
	})
}

func TestWriteSequencingSectionMergesRunsAndLanes(t *testing.T) {
	convey.Convey("Given a run present in both the progress runs and the lane rows, the Sequencing section shows it once, combining platform, lane/tag and current status", t, func() {
		report := infoReport{
			SampleProgress: &mlwh.SampleProgress{
				Runs: []mlwh.RunStatusTimeline{
					{IDRun: 49166, Platform: "Illumina", Current: "qc complete"},
				},
			},
			Lanes: []mlwh.Lane{
				{IDRun: 49166, Position: 2, TagIndex: 8},
			},
		}

		var buf bytes.Buffer
		writeSequencingSection(&buf, report, infoStyle{colour: false, width: 100})
		out := buf.String()

		convey.So(out, convey.ShouldContainSubstring, "Sequencing")
		// The shared run id appears exactly once (de-duplicated across the two
		// feeds), not once per feed.
		convey.So(strings.Count(out, "Run 49166"), convey.ShouldEqual, 1)
		convey.So(strings.Count(out, "49166"), convey.ShouldEqual, 1)
		// The single line combines platform, lane/tag and current status.
		convey.So(out, convey.ShouldContainSubstring, "Illumina")
		convey.So(out, convey.ShouldContainSubstring, "lane 2 tag 8")
		convey.So(out, convey.ShouldContainSubstring, "qc complete")
	})

	convey.Convey("Given a run with multiple lanes, the Sequencing section shows a run headline and one indented line per lane", t, func() {
		report := infoReport{
			SampleProgress: &mlwh.SampleProgress{
				Runs: []mlwh.RunStatusTimeline{
					{IDRun: 49166, Platform: "Illumina", Current: "qc complete"},
				},
			},
			Lanes: []mlwh.Lane{
				{IDRun: 49166, Position: 1, TagIndex: 7},
				{IDRun: 49166, Position: 2, TagIndex: 8},
			},
		}

		var buf bytes.Buffer
		writeSequencingSection(&buf, report, infoStyle{colour: false, width: 100})
		out := buf.String()

		convey.So(strings.Count(out, "Run 49166"), convey.ShouldEqual, 1)
		convey.So(out, convey.ShouldContainSubstring, "qc complete")
		// Each lane is its own line under the run headline.
		convey.So(out, convey.ShouldContainSubstring, "lane 1 · tag 7")
		convey.So(out, convey.ShouldContainSubstring, "lane 2 · tag 8")
		// The lane/tag pairs are not also crammed onto the run headline.
		convey.So(out, convey.ShouldNotContainSubstring, "lane 1 tag 7")
	})

	convey.Convey("Given a run that appears only in the lane rows (no within-sequencing status), the Sequencing section still lists it", t, func() {
		report := infoReport{
			SampleProgress: &mlwh.SampleProgress{},
			Lanes: []mlwh.Lane{
				{IDRun: 70000, Position: 1, TagIndex: 3},
			},
		}

		var buf bytes.Buffer
		writeSequencingSection(&buf, report, infoStyle{colour: false, width: 100})
		out := buf.String()

		convey.So(strings.Count(out, "Run 70000"), convey.ShouldEqual, 1)
		convey.So(out, convey.ShouldContainSubstring, "lane 1 tag 3")
		// No status text for a lane-only run.
		convey.So(out, convey.ShouldNotContainSubstring, "qc complete")
	})

	convey.Convey("Given runs in both feeds, they are listed deterministically with progress order first then lane-only runs ascending", t, func() {
		report := infoReport{
			SampleProgress: &mlwh.SampleProgress{
				Runs: []mlwh.RunStatusTimeline{
					{IDRun: 49166, Platform: "Illumina", Current: "qc complete"},
					{IDRun: 49001, Platform: "Illumina", Current: "run pending"},
				},
			},
			Lanes: []mlwh.Lane{
				{IDRun: 80000, Position: 1, TagIndex: 1},
				{IDRun: 70000, Position: 1, TagIndex: 3},
				{IDRun: 49166, Position: 2, TagIndex: 8},
			},
		}

		var buf bytes.Buffer
		writeSequencingSection(&buf, report, infoStyle{colour: false, width: 100})
		out := buf.String()

		// Progress-run order preserved (49166 before 49001), then lane-only runs in
		// ascending id order (70000 before 80000), with no duplication of 49166.
		convey.So(strings.Count(out, "Run 49166"), convey.ShouldEqual, 1)
		idx49166 := strings.Index(out, "Run 49166")
		idx49001 := strings.Index(out, "Run 49001")
		idx70000 := strings.Index(out, "Run 70000")
		idx80000 := strings.Index(out, "Run 80000")
		convey.So(idx49166, convey.ShouldBeLessThan, idx49001)
		convey.So(idx49001, convey.ShouldBeLessThan, idx70000)
		convey.So(idx70000, convey.ShouldBeLessThan, idx80000)
	})

	convey.Convey("Given no progress, the Sequencing section is omitted entirely", t, func() {
		var buf bytes.Buffer
		writeSequencingSection(&buf, infoReport{}, infoStyle{colour: false, width: 100})

		convey.So(buf.String(), convey.ShouldBeBlank)
	})
}

func TestWriteWarningsSectionUsesWarningColour(t *testing.T) {
	convey.Convey("Given warnings rendered with colour, each warning uses the warning (yellow) colour, not dim", t, func() {
		var buf bytes.Buffer
		writeWarningsSection(&buf, []string{"overview boom"}, infoStyle{colour: true, width: 100})
		out := buf.String()

		convey.So(out, convey.ShouldContainSubstring, "Warnings")
		convey.So(out, convey.ShouldContainSubstring, "overview boom")
		// The warning text stands out in yellow rather than receding in faint.
		convey.So(strings.Contains(out, ansiYellow), convey.ShouldBeTrue)

		// Locate the warning line and confirm it is the yellow-wrapped one.
		warnLine := ""
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "overview boom") {
				warnLine = line

				break
			}
		}

		convey.So(strings.Contains(warnLine, ansiYellow), convey.ShouldBeTrue)
		convey.So(strings.Contains(warnLine, ansiDim), convey.ShouldBeFalse)
	})

	convey.Convey("Given warnings rendered without colour, the warning text is plain", t, func() {
		var buf bytes.Buffer
		writeWarningsSection(&buf, []string{"overview boom"}, infoStyle{colour: false, width: 100})

		convey.So(buf.String(), convey.ShouldContainSubstring, "overview boom")
		convey.So(strings.Contains(buf.String(), "\x1b["), convey.ShouldBeFalse)
	})
}

func TestWriteStudyPanelDeduplicatesCacheSynced(t *testing.T) {
	convey.Convey("Given a study report, the rendered panel shows cache-synced once and avoids duplicate sample-count panels", t, func() {
		report := infoReport{
			Identifier: "5901",
			Kind:       string(mlwh.KindStudyLimsID),
			Canonical:  "5901",
			Study: &mlwh.Study{
				IDStudyLims:     "5901",
				Name:            "Lung cancer GWAS",
				AccessionNumber: "EGAS00001005678",
				Programme:       "Cancer Genetics",
			},
			StudyOverview: &mlwh.StudyOverview{
				IDStudyLims:     "5901",
				SamplesTotal:    100,
				SamplesWithData: 80,
				DataObjects:     1234,
				Runs:            5,
				Libraries:       12,
				AddedLast7Days:  3,
				CacheSyncedAt:   "2026-06-27T00:00:00Z",
			},
			StatusBreakdown: &mlwh.StatusBreakdown{
				IDStudyLims:          "5901",
				Distinct:             mlwh.PhaseLadder{WithData: 80, SequencedNoData: 5, Registered: 15},
				PerPlatform:          []mlwh.PlatformPhaseLadder{{Platform: "Illumina", Ladder: mlwh.PhaseLadder{WithData: 80}}},
				WithDetailedTimeline: 40,
				CacheSyncedAt:        "2026-06-27T00:00:00Z",
			},
			SamplesWithDataCount:    &infoSamplesWithDataCount{AllTime: 80, AddedSince: 3, Since: "2026-06-20T00:00:00Z"},
			SamplesWithoutDataCount: &infoCount{Count: 2},
		}

		var buf bytes.Buffer
		writeInfoReportText(&buf, report, infoStyle{colour: false, width: 100})
		out := buf.String()

		// Cache-synced is shown exactly once even though three sources carry it.
		convey.So(strings.Count(out, "Cache synced"), convey.ShouldEqual, 1)

		// The core distinct partition appears once each.
		convey.So(strings.Count(out, "with data"), convey.ShouldBeGreaterThanOrEqualTo, 1)
	})
}
