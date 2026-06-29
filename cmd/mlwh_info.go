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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
)

const (
	infoMaxRelated     = 50
	infoNotFoundExitFn = "mlwh: no matches"
)

var openMLWHInfoClient = func(ctx context.Context, cfg mlwh.Config) (mlwhInfoClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return mlwh.OpenCacheOnly(ctx, cfg.Cache)
	}

	return mlwh.Open(ctx, cfg)
}

var openMLWHInfoRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhInfoClient, error) {
	return mlwh.NewRemoteClient(cfg)
}

// mlwhInfoClient is the subset of *mlwh.Client used by `wa mlwh info`.
type mlwhInfoClient interface {
	ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveSample(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveRun(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error)
	FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error)
	FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error)
	FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error)

	StudiesForSample(ctx context.Context, sangerName string) ([]mlwh.Study, error)
	LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error)
	IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error)
	LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error)
	RunsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Run, error)
	SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
	SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error)
	SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)

	StudyOverview(ctx context.Context, studyLimsID string) (mlwh.StudyOverview, error)
	StatusBreakdown(ctx context.Context, studyLimsID string) (mlwh.StatusBreakdown, error)
	CountSamplesWithData(ctx context.Context, studyLimsID string) (mlwh.Count, error)
	CountSamplesWithDataSince(ctx context.Context, studyLimsID, since, until string) (mlwh.Count, error)
	SamplesWithoutData(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.SampleWithData, error)
	SampleProgress(ctx context.Context, sangerName string) (mlwh.SampleProgress, error)
	RunOverview(ctx context.Context, idRun string) (mlwh.RunOverview, error)
	RunStatus(ctx context.Context, idRun string) (mlwh.RunStatusTimeline, error)

	Close() error
}

func openMLWHInfoConfiguredClient(ctx context.Context, serverURL string) (mlwhInfoClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHInfoRemoteClient(ctx, mlwh.RemoteConfig{BaseURL: trimmedServerURL})
	}

	cfg, err := resolveMLWHInfoLocalConfig()
	if err != nil {
		return nil, err
	}

	client, err := openMLWHInfoClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.DSN) != "" && errors.Is(err, mlwh.ErrPasswordInDSN) {
			return nil, fmt.Errorf("WA_MLWH_DSN: %w", err)
		}

		return nil, err
	}

	return client, nil
}

func defaultMLWHInfoServerURL() string {
	if serverURL := strings.TrimSpace(firstEnv("WA_MLWH_SERVER_URL")); serverURL != "" {
		return serverURL
	}

	if backendURL := strings.TrimSpace(firstEnv("WA_MLWH_BACKEND_URL")); backendURL != "" {
		return backendURL
	}

	if port := strings.TrimSpace(activeMLWHPort()); port != "" {
		return "http://127.0.0.1:" + port
	}

	return ""
}

func activeMLWHPort() string {
	switch firstEnv("WA_ENV") {
	case "test":
		return firstEnv("WA_TEST_SEQMETA_PORT")
	case "development":
		return firstEnv("WA_DEV_SEQMETA_PORT")
	case "production":
		return firstEnv("WA_PROD_SEQMETA_PORT")
	default:
		return ""
	}
}

// infoQueryError renders a failed-query error for `wa mlwh info`. When canSync is
// false (server or cache-only mode) it omits any 'wa mlwh sync' hint: the
// never-synced case becomes a neutral cache-unavailable message that does not
// embed mlwh.ErrCacheNeverSynced.Error() (whose text contains the sync hint), and
// other errors drop the sync suffix. When canSync is true (local operator
// upstream-DSN mode) the actionable sync hints are kept.
func infoQueryError(identifier string, err error, canSync bool) error {
	if errors.Is(err, mlwh.ErrCacheNeverSynced) {
		if canSync {
			return fmt.Errorf("resolve %q: %w", identifier, err)
		}

		return fmt.Errorf("resolve %q: %s", identifier, mlwhCacheUnavailableMessage)
	}

	if errors.Is(err, mlwh.ErrNotFound) {
		if canSync {
			return fmt.Errorf("no match for identifier %q (run 'wa mlwh sync' if you think the cache is stale)", identifier)
		}

		return fmt.Errorf("no match for identifier %q", identifier)
	}

	if canSync {
		return fmt.Errorf("resolve %q: %w (run 'wa mlwh sync' if your local cache is empty or stale)", identifier, err)
	}

	return fmt.Errorf("resolve %q: %w", identifier, err)
}

func writeSamplePanel(out io.Writer, report infoReport, style infoStyle) {
	writeInfoTitle(out, style, "Sample", report)

	sample := report.Sample
	writeSampleIdentityFields(out, style, sample)
	writeSampleStudyLine(out, report, style)
	writeSampleLibraries(out, style, sample)
	writeSampleProgressSection(out, report.SampleProgress, style)
	writeSequencingSection(out, report, style)
	writeSampleIRODS(out, report, style)
	writeCacheSyncedLine(out, style, sampleCacheSyncedAt(report))
}

// writeInfoTitle renders the panel title line (entity word, identifier and
// kind, with the canonical form when it differs) followed by a rule.
func writeInfoTitle(out io.Writer, style infoStyle, entity string, report infoReport) {
	title := style.header(entity) + "  " + style.bold(report.Identifier)
	if report.Kind != "" {
		title += "   " + style.dim("("+report.Kind+")")
	}
	if report.Canonical != "" && report.Canonical != report.Identifier {
		title += " " + style.dim("→ "+report.Canonical)
	}

	_, _ = fmt.Fprintln(out, title)
	_, _ = fmt.Fprintln(out, infoRule(style))
}

// writeSampleIdentityFields prints the sample's core identity in aligned
// label/value fields, surfacing supplier_name and the other always-shown
// identifiers.
func writeSampleIdentityFields(out io.Writer, style infoStyle, sample *mlwh.Sample) {
	const labelWidth = 12

	organism := strings.TrimSpace(sample.CommonName)
	if sample.TaxonID > 0 {
		organism = infoJoinNonEmpty(" ", organism, fmt.Sprintf("(%d)", sample.TaxonID))
	}

	// sanger_sample_id is shown only when it differs from the sample name (they
	// are usually identical), to avoid a redundant near-duplicate row.
	sangerID := ""
	if strings.TrimSpace(sample.SangerSampleID) != "" && sample.SangerSampleID != sample.Name {
		sangerID = sample.SangerSampleID
	}

	fields := []struct{ label, value string }{
		{"Sanger ID", sample.Name},
		{"LIMS ID", sample.IDSampleLims},
		{"Sanger SID", sangerID},
		{"Supplier", sample.SupplierName},
		{"Accession", sample.AccessionNumber},
		{"Donor", sample.DonorID},
		{"Organism", organism},
		{"UUID", sample.UUIDSampleLims},
	}

	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			continue
		}

		value := field.value
		if field.label == "UUID" {
			value = style.dim(value)
		}

		_, _ = fmt.Fprintf(out, "  %s\n", infoField(style, field.label, value, labelWidth))
	}
}

// writeSampleStudyLine prints the single resolved study, or each study when the
// sample spans several (so a two-pairing sample still shows both study ids).
func writeSampleStudyLine(out io.Writer, report infoReport, style infoStyle) {
	studies := report.Studies
	if len(studies) == 0 && report.Study != nil {
		studies = []mlwh.Study{*report.Study}
	}
	if len(studies) == 0 {
		return
	}

	_, _ = fmt.Fprintln(out)
	for _, study := range studies {
		_, _ = fmt.Fprintf(out, "  %s\n", infoField(style, "Study", infoStudyHeadline(style, study), 12))
	}
}

func infoStudyHeadline(style infoStyle, study mlwh.Study) string {
	headline := style.bold(study.IDStudyLims)
	headline = infoJoinNonEmpty("  ", headline, study.Name)
	if strings.TrimSpace(study.AccessionNumber) != "" {
		headline = infoJoinNonEmpty("  ", headline, style.dim("("+study.AccessionNumber+")"))
	}

	return headline
}

// writeSampleLibraries lists each library/study pairing (with its library_id
// and id_library_lims when present), so both pairings of a two-pairing sample
// are shown with their study ids.
func writeSampleLibraries(out io.Writer, style infoStyle, sample *mlwh.Sample) {
	pairs := make([]mlwh.Library, 0, len(sample.Libraries))
	for _, library := range sample.Libraries {
		if strings.TrimSpace(library.PipelineIDLims) == "" || strings.TrimSpace(library.IDStudyLims) == "" {
			continue
		}

		pairs = append(pairs, library)
	}

	if len(pairs) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\n  %s\n", style.section("Libraries"))
	for _, library := range pairs {
		line := library.PipelineIDLims + " / " + library.IDStudyLims
		if ids := infoLibraryIdentifiers(style, library); ids != "" {
			line += "  " + ids
		}

		_, _ = fmt.Fprintf(out, "    %s\n", line)
	}
}

func infoLibraryIdentifiers(style infoStyle, library mlwh.Library) string {
	var parts []string
	if strings.TrimSpace(library.LibraryID) != "" {
		parts = append(parts, style.dim("library_id=")+library.LibraryID)
	}
	if strings.TrimSpace(library.IDLibraryLims) != "" {
		parts = append(parts, style.dim("id_library_lims=")+library.IDLibraryLims)
	}

	return strings.Join(parts, " ")
}

// infoProgressHeadline folds the platforms, baseline phase, QC verdict and
// delivery date into a single compact line.
func infoProgressHeadline(style infoStyle, progress *mlwh.SampleProgress) string {
	parts := []string{}
	if len(progress.Platforms) > 0 {
		parts = append(parts, strings.Join(progress.Platforms, "+"))
	}
	if phase := strings.TrimSpace(progress.BaselinePhase); phase != "" {
		parts = append(parts, phase)
	}
	if qc := strings.TrimSpace(progress.QC); qc != "" {
		parts = append(parts, style.qcColour(qc, "QC "+qc))
	}
	if delivered := infoCompactDate(progress.DeliveredAt); delivered != "" {
		// The delivery date is important timeline content: keep it readable (the
		// surrounding " · " separators stay dim via the join below).
		parts = append(parts, "delivered "+delivered)
	}

	return strings.Join(parts, style.dim(" · "))
}

func writeMilestoneTimeline(out io.Writer, style infoStyle, milestones []mlwh.Milestone) {
	if len(milestones) == 0 {
		return
	}

	items := make([]infoTimelineItem, len(milestones))
	for i, milestone := range milestones {
		items[i] = infoTimelineItem{
			label: milestone.Name,
			at:    milestone.ReachedAt,
			open:  strings.TrimSpace(milestone.DurationToNext) == "" && i == len(milestones)-1,
		}
	}

	writeInfoTimeline(out, style, items, "    ")
}

// writeSequencingSection renders one Sequencing section for the sample, merging
// the within-sequencing run status (progress.Runs) and the physical lane/tag
// rows (report.Lanes) keyed by id_run so a run that appears in both feeds is
// shown exactly once. Runs from progress.Runs come first in their given order;
// runs that appear only in the lane rows follow in ascending id_run order. The
// ordering is fully deterministic and never depends on time.Now. With sample
// progress present but no runs and no lanes it shows an explicit "none"; it is
// omitted entirely only when there is neither progress nor any sequencing data
// (so lane rows still render even if the progress feed degraded).
func writeSequencingSection(out io.Writer, report infoReport, style infoStyle) {
	var statuses []mlwh.RunStatusTimeline
	if report.SampleProgress != nil {
		statuses = report.SampleProgress.Runs
	}

	order, runs, lanes := infoMergeSequencing(statuses, report.Lanes)

	if report.SampleProgress == nil && len(order) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\n  %s\n", style.section("Sequencing"))

	if len(order) == 0 {
		_, _ = fmt.Fprintf(out, "    %s\n", style.dim("none"))

		return
	}

	for _, id := range order {
		writeSequencingRun(out, style, id, runs[id], lanes[id])
	}
}

// infoMergeSequencing groups the run-status feed and the lane-row feed by id_run.
// It returns the deterministic id order (progress runs first in their given
// order, then lane-only runs ascending), plus lookups from id_run to its status
// (when present) and to its lane rows (in their given order).
func infoMergeSequencing(
	statuses []mlwh.RunStatusTimeline, allLanes []mlwh.Lane,
) ([]int, map[int]mlwh.RunStatusTimeline, map[int][]mlwh.Lane) {
	runs := make(map[int]mlwh.RunStatusTimeline, len(statuses))
	lanes := make(map[int][]mlwh.Lane)

	var order []int
	seen := make(map[int]bool)

	for _, status := range statuses {
		if !seen[status.IDRun] {
			seen[status.IDRun] = true
			order = append(order, status.IDRun)
		}
		runs[status.IDRun] = status
	}

	var laneOnly []int
	for _, lane := range allLanes {
		lanes[lane.IDRun] = append(lanes[lane.IDRun], lane)
		if !seen[lane.IDRun] {
			seen[lane.IDRun] = true
			laneOnly = append(laneOnly, lane.IDRun)
		}
	}

	sort.Ints(laneOnly)

	return append(order, laneOnly...), runs, lanes
}

// writeSequencingRun renders one run of the Sequencing section. With a single
// lane (or none) it is a one-liner: "Run <id>  <platform> · <lane X tag Y> ·
// <current>". With several lanes the run headline carries platform/status and
// each lane is an indented "lane X · tag Y" line beneath it. The platform,
// lane/tag and current-status text are readable; only the " · " separators dim.
func writeSequencingRun(out io.Writer, style infoStyle, id int, run mlwh.RunStatusTimeline, lanes []mlwh.Lane) {
	platform := strings.TrimSpace(run.Platform)
	current := strings.TrimSpace(run.Current)

	if len(lanes) <= 1 {
		lanePart := ""
		if len(lanes) == 1 {
			lanePart = fmt.Sprintf("lane %d tag %d", lanes[0].Position, lanes[0].TagIndex)
		}

		headline := infoJoinNonEmpty(style.dim(" · "), platform, lanePart, current)
		_, _ = fmt.Fprintf(out, "    %s %d  %s\n", style.label("Run"), id, headline)

		return
	}

	headline := infoJoinNonEmpty(style.dim(" · "), platform, current)
	_, _ = fmt.Fprintf(out, "    %s %d  %s\n", style.label("Run"), id, headline)
	for _, lane := range lanes {
		_, _ = fmt.Fprintf(out, "      %s\n",
			infoJoinNonEmpty(style.dim(" · "), fmt.Sprintf("lane %d", lane.Position), fmt.Sprintf("tag %d", lane.TagIndex)))
	}
}

// writeSampleIRODS lists every iRODS data-object path for the sample, each as a
// single compact line. The lane/tag links are shown by writeSequencingSection
// (merged with the run status), so this lists only the data-object paths.
func writeSampleIRODS(out io.Writer, report infoReport, style infoStyle) {
	if len(report.IRODSPaths) == 0 {
		return
	}

	_, _ = fmt.Fprintln(out)
	label := fmt.Sprintf("iRODS (%d)", len(report.IRODSPaths))
	for i, path := range report.IRODSPaths {
		writeListField(out, style, label, path.IRODSPath, i, 12)
	}
}

// writeListField prints the first row of a list with its label and blank-pads
// the label column on subsequent rows so the values align.
func writeListField(out io.Writer, style infoStyle, label, value string, index, labelWidth int) {
	shown := label
	if index > 0 {
		shown = ""
	}

	_, _ = fmt.Fprintf(out, "  %s\n", infoField(style, shown, value, labelWidth))
}

func writeCacheSyncedLine(out io.Writer, style infoStyle, synced string) {
	date := infoCompactDate(synced)
	if date == "" {
		return
	}

	_, _ = fmt.Fprintf(out, "\n  %s\n", style.dim("Cache synced "+date))
}

// sampleCacheSyncedAt picks the cache-synced timestamp to show once for a sample
// panel, preferring the progress feed (the broadest set of feeding tables).
func sampleCacheSyncedAt(report infoReport) string {
	if report.SampleProgress != nil && strings.TrimSpace(report.SampleProgress.CacheSyncedAt) != "" {
		return report.SampleProgress.CacheSyncedAt
	}

	return ""
}

func writeStudyPanel(out io.Writer, report infoReport, style infoStyle) {
	writeInfoTitle(out, style, "Study", report)

	study := report.Study
	const labelWidth = 12

	writeStudyMetaFields(out, style, study, labelWidth)
	writeStudySamplesBlock(out, report, style, labelWidth)
	writeStudyDataBlock(out, report, style, labelWidth)
	writeStudyListColumns(out, report, style)
	writeCacheSyncedLine(out, style, studyCacheSyncedAt(report))
}

func writeStudyMetaFields(out io.Writer, style infoStyle, study *mlwh.Study, labelWidth int) {
	for _, field := range []struct{ label, value string }{
		{"Accession", study.AccessionNumber},
		{"Programme", study.Programme},
		{"Sponsor", study.FacultySponsor},
		{"Title", study.StudyTitle},
		{"UUID", study.UUIDStudyLims},
	} {
		if strings.TrimSpace(field.value) == "" {
			continue
		}

		// The UUID is an auxiliary identifier: render it dim (matching the sample
		// panel) and never truncate it.
		value := infoTruncate(field.value, max(20, style.width-18))
		if field.label == "UUID" {
			value = style.dim(field.value)
		}

		_, _ = fmt.Fprintf(out, "  %s\n", infoField(style, field.label, value, labelWidth))
	}
}

// writeStudySamplesBlock renders the sample partition once: the total, the
// recency note, the distinct with_data / sequenced_no_data / registered buckets
// as proportional bars, the per-platform summary and the detailed-timeline
// count. Sources are reconciled (StatusBreakdown.Distinct sums to samples_total,
// the StudyOverview supplies the total and recency).
func writeStudySamplesBlock(out io.Writer, report infoReport, style infoStyle, labelWidth int) {
	overview := report.StudyOverview
	breakdown := report.StatusBreakdown
	if overview == nil && breakdown == nil {
		return
	}

	total, withData, withoutData, recency := infoStudySampleTotals(report)

	summary := fmt.Sprintf("%s total", infoInt(total))
	summary = infoJoinNonEmpty(style.dim(" · "), summary, fmt.Sprintf("%s with data", infoInt(withData)),
		fmt.Sprintf("%s without data", infoInt(withoutData)))
	if recency > 0 {
		summary += "  " + style.dim(fmt.Sprintf("(+%s added in last 7 days)", infoInt(recency)))
	}

	_, _ = fmt.Fprintf(out, "\n  %s\n", infoField(style, "Samples", summary, labelWidth))

	if breakdown != nil {
		writeStudyDistinctBars(out, breakdown.Distinct, total)
		writeStudyPlatformSummary(out, style, breakdown.PerPlatform)
		if breakdown.WithDetailedTimeline > 0 {
			_, _ = fmt.Fprintf(out, "    %s\n", style.dim(fmt.Sprintf("detailed timeline for %s samples", infoInt(breakdown.WithDetailedTimeline))))
		}
	}
}

// infoStudySampleTotals reconciles the authoritative figures: the distinct
// partition from StatusBreakdown sums to samples_total and supplies the with-data
// count (so the summary line agrees with the distinct bars drawn from the same
// ladder); the StudyOverview gives the total, the recency count, and the with-data
// count only as a fallback when there is no StatusBreakdown.
func infoStudySampleTotals(report infoReport) (total, withData, withoutData, recency int) {
	if report.StatusBreakdown != nil {
		ladder := report.StatusBreakdown.Distinct
		withData = ladder.WithData
		total = ladder.WithData + ladder.SequencedNoData + ladder.Registered
	}

	if report.StudyOverview != nil {
		overview := report.StudyOverview
		if overview.SamplesTotal > 0 || total == 0 {
			total = overview.SamplesTotal
		}
		// Only take with-data from the overview when no StatusBreakdown is present;
		// otherwise the breakdown's with-data figure wins so the summary matches the
		// rendered with-data bar even if the two endpoints disagree.
		if report.StatusBreakdown == nil {
			withData = overview.SamplesWithData
		}
		recency = overview.AddedLast7Days
	}

	withoutData = total - withData
	if withoutData < 0 {
		withoutData = 0
	}

	return total, withData, withoutData, recency
}

// infoInt renders an integer with thousands separators for readability (text
// only; the JSON contract is untouched).
func infoInt(value int) string {
	sign := ""
	digits := fmt.Sprintf("%d", value)
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}

	if len(digits) <= 3 {
		return sign + digits
	}

	var b strings.Builder
	lead := len(digits) % 3
	if lead > 0 {
		b.WriteString(digits[:lead])
	}
	for i := lead; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(digits[i : i+3])
	}

	return sign + b.String()
}

func writeStudyDistinctBars(out io.Writer, ladder mlwh.PhaseLadder, total int) {
	rows := []struct {
		label string
		count int
	}{
		{"with data", ladder.WithData},
		{"sequenced, no data", ladder.SequencedNoData},
		{"registered only", ladder.Registered},
	}

	const labelWidth = 19
	for _, row := range rows {
		bar := infoProportionBar(row.count, total, 20)
		_, _ = fmt.Fprintf(out, "    %s %s  %s  %s\n",
			infoPadTo(row.label, labelWidth), infoRight(infoInt(row.count), 4), bar, infoPercent(row.count, total))
	}
}

// infoProportionBar renders a 20-ish cell bar with `count/total` filled.
func infoProportionBar(count, total, width int) string {
	if width <= 0 {
		width = 20
	}

	filled := 0
	if total > 0 && count > 0 {
		filled = (count*width + total/2) / total
		if filled == 0 {
			filled = 1
		}
		if filled > width {
			filled = width
		}
	}

	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func infoRight(text string, n int) string {
	if len(text) >= n {
		return text
	}

	return strings.Repeat(" ", n-len(text)) + text
}

func infoPercent(count, total int) string {
	if total <= 0 {
		return "0%"
	}

	return fmt.Sprintf("%d%%", (count*100+total/2)/total)
}

func writeStudyPlatformSummary(out io.Writer, style infoStyle, platforms []mlwh.PlatformPhaseLadder) {
	if len(platforms) == 0 {
		return
	}

	var parts []string
	for _, platform := range platforms {
		if platform.Ladder.WithData > 0 {
			parts = append(parts, fmt.Sprintf("%s %s with data", platform.Platform, infoInt(platform.Ladder.WithData)))
		} else {
			parts = append(parts, platform.Platform)
		}
	}

	_, _ = fmt.Fprintf(out, "    %s\n", style.dim("by platform: "+strings.Join(parts, ", ")))
}

// writeStudyDataBlock summarises the study's sequencing data once: object, run
// and library counts, library types and the sequencing date range.
func writeStudyDataBlock(out io.Writer, report infoReport, style infoStyle, labelWidth int) {
	overview := report.StudyOverview
	if overview == nil {
		return
	}

	summary := infoJoinNonEmpty(style.dim(" · "),
		fmt.Sprintf("%s objects", infoInt(overview.DataObjects)),
		fmt.Sprintf("%s runs", infoInt(overview.Runs)),
		fmt.Sprintf("%s libraries", infoInt(overview.Libraries)))

	_, _ = fmt.Fprintf(out, "\n  %s\n", infoField(style, "Data", summary, labelWidth))

	if len(overview.LibraryTypes) > 0 {
		_, _ = fmt.Fprintf(out, "    %s\n", style.dim("library types: "+strings.Join(overview.LibraryTypes, ", ")))
	}

	if rng := infoDateRange(overview.SequencingDateRange); rng != "" {
		// Sequencing dates are important timeline content: keep them readable.
		_, _ = fmt.Fprintf(out, "    sequencing %s\n", rng)
	}
}

// infoDateRange renders an "earliest → latest" compact range, or "" when
// absent. The year is appended when the two ends fall in different years so a
// cross-year range reads unambiguously, while staying compact within one year.
func infoDateRange(rng *mlwh.DateRange) string {
	if rng == nil {
		return ""
	}

	start, okStart := parseInfoTime(rng.Earliest)
	end, okEnd := parseInfoTime(rng.Latest)

	if okStart && okEnd && start.Year() != end.Year() {
		return start.Format("Jan 02 2006") + " → " + end.Format("Jan 02 2006")
	}

	return infoJoinNonEmpty(" → ", infoCompactDate(rng.Earliest), infoCompactDate(rng.Latest))
}

// writeStudyListColumns lists the study's libraries, runs and samples, each with
// the true total in its header and a "(N of M)" header when truncated at the
// fetch cap.
func writeStudyListColumns(out io.Writer, report infoReport, style infoStyle) {
	total := 0
	if report.StudyOverview != nil {
		total = report.StudyOverview.Libraries
	}
	writeLibrariesList(out, style, report.Libraries, total)

	total = 0
	if report.StudyOverview != nil {
		total = report.StudyOverview.Runs
	}
	writeRunsList(out, style, report.Runs, total)

	total = 0
	if report.StudyOverview != nil {
		total = report.StudyOverview.SamplesTotal
	}
	writeSamplesList(out, style, report.Samples, total)
}

func writeLibrariesList(out io.Writer, style infoStyle, libraries []mlwh.Library, total int) {
	if len(libraries) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\n  %s\n", style.section(infoListHeading("Libraries", len(libraries), total)))
	for _, library := range libraries {
		line := library.PipelineIDLims
		if strings.TrimSpace(library.IDStudyLims) != "" {
			line += " / " + library.IDStudyLims
		}
		if ids := infoLibraryIdentifiers(style, library); ids != "" {
			line += "  " + ids
		}

		_, _ = fmt.Fprintf(out, "    %s\n", line)
	}
}

// infoListHeading renders a list heading like "Runs (5)" or, when the shown
// rows were truncated below the true total, "Samples (50 of 100)".
func infoListHeading(label string, shown, total int) string {
	if total > shown {
		return fmt.Sprintf("%s (%d of %d)", label, shown, total)
	}

	return fmt.Sprintf("%s (%d)", label, shown)
}

func writeRunsList(out io.Writer, style infoStyle, runs []mlwh.Run, total int) {
	if len(runs) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\n  %s\n", style.section(infoListHeading("Runs", len(runs), total)))
	for _, run := range runs {
		_, _ = fmt.Fprintf(out, "    %d\n", run.IDRun)
	}
}

func writeSamplesList(out io.Writer, style infoStyle, samples []mlwh.Sample, total int) {
	if len(samples) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\n  %s\n", style.section(infoListHeading("Samples", len(samples), total)))
	for _, sample := range samples {
		line := sample.Name
		if supplier := strings.TrimSpace(sample.SupplierName); supplier != "" {
			line = infoJoinNonEmpty("  ", line, style.dim(supplier))
		}

		_, _ = fmt.Fprintf(out, "    %s\n", line)
	}
}

func studyCacheSyncedAt(report infoReport) string {
	if report.StudyOverview != nil && strings.TrimSpace(report.StudyOverview.CacheSyncedAt) != "" {
		return report.StudyOverview.CacheSyncedAt
	}
	if report.StatusBreakdown != nil {
		return report.StatusBreakdown.CacheSyncedAt
	}

	return ""
}

func writeRunPanel(out io.Writer, report infoReport, style infoStyle) {
	writeInfoTitle(out, style, "Run", report)

	writeRunContents(out, report, style)
	writeRunStatusSection(out, report.RunStatus, style)

	total := 0
	if report.RunOverview != nil {
		total = report.RunOverview.Samples
	}
	writeSamplesList(out, style, report.Samples, total)
	writeCacheSyncedLine(out, style, runCacheSyncedAt(report))
}

func writeRunContents(out io.Writer, report infoReport, style infoStyle) {
	overview := report.RunOverview
	if overview == nil {
		return
	}

	summary := infoJoinNonEmpty(style.dim(" · "),
		fmt.Sprintf("%s samples", infoInt(overview.Samples)),
		fmt.Sprintf("%s studies", infoInt(overview.Studies)),
		fmt.Sprintf("%s data objects", infoInt(overview.DataObjects)))

	_, _ = fmt.Fprintf(out, "  %s\n", infoField(style, "Contents", summary, 12))

	if rng := infoDateRange(overview.SequencingDateRange); rng != "" {
		// Sequencing dates are important timeline content: keep them readable.
		_, _ = fmt.Fprintf(out, "    sequencing %s\n", rng)
	}
}

func infoCurrentPhase(status *mlwh.RunStatusTimeline) string {
	if reason := strings.TrimSpace(status.NotTracked); reason != "" {
		return "not tracked"
	}
	if current := strings.TrimSpace(status.Current); current != "" {
		return "current " + current
	}

	return ""
}

func runCacheSyncedAt(report infoReport) string {
	if report.RunOverview != nil && strings.TrimSpace(report.RunOverview.CacheSyncedAt) != "" {
		return report.RunOverview.CacheSyncedAt
	}

	return ""
}

func writeLibraryPanel(out io.Writer, report infoReport, style infoStyle) {
	writeInfoTitle(out, style, "Library", report)

	library := report.Library
	_, _ = fmt.Fprintf(out, "  %s\n", infoField(style, "pipeline_id_lims", library.PipelineIDLims, 16))
	if strings.TrimSpace(library.IDStudyLims) != "" {
		_, _ = fmt.Fprintf(out, "  %s\n", infoField(style, "id_study_lims", library.IDStudyLims, 16))
	}
	if ids := infoLibraryIdentifiers(style, *library); ids != "" {
		_, _ = fmt.Fprintf(out, "  %s\n", ids)
	}
}

type mlwhInfoSampleNameResolver interface {
	ResolveSampleName(ctx context.Context, raw string) (mlwh.Match, error)
}

// runMLWHInfo resolves identifier and prints its report. canSync reports whether
// the caller is in local operator upstream-DSN mode (the only mode where
// 'wa mlwh sync' works); when false the failed-query messages omit any sync hint,
// because an end-user going via the server or a cache-only path cannot sync.
func runMLWHInfo(ctx context.Context, client mlwhInfoClient, out io.Writer, identifier, typeFlag, since string, jsonOut, canSync bool) error {
	match, err := classifyForInfo(ctx, client, identifier, typeFlag)
	if err != nil {
		return infoQueryError(identifier, err, canSync)
	}

	report := buildInfoReport(ctx, client, identifier, since, match)

	if jsonOut {
		return writeInfoReportJSON(out, report)
	}

	writeInfoReportText(out, report, resolveInfoStyle(out))

	return nil
}

func buildInfoReport(ctx context.Context, client mlwhInfoClient, identifier, since string, match mlwh.Match) infoReport {
	report := infoReport{
		Identifier: identifier,
		Kind:       string(match.Kind),
		Canonical:  match.Canonical,
		Sample:     match.Sample,
		Study:      match.Study,
		Run:        match.Run,
		Library:    match.Library,
	}

	switch {
	case match.Sample != nil:
		populateSampleInfo(ctx, client, &report, match.Sample)
		populateSampleProgress(ctx, client, &report, match.Sample.Name)
	case match.Study != nil:
		populateStudyInfo(ctx, client, &report, match.Study.IDStudyLims)
		populateStudyFeatures(ctx, client, &report, match.Study.IDStudyLims, since)
	case match.Run != nil:
		runID := fmt.Sprintf("%d", match.Run.IDRun)
		if samples, err := client.SamplesForRun(ctx, runID, infoMaxRelated, 0); err == nil {
			report.Samples = samples
		} else {
			report.Warnings = append(report.Warnings, fmt.Sprintf("samples for run: %v", err))
		}
		populateRunFeatures(ctx, client, &report, runID)
	case match.Library != nil:
		// Library Match has no parent study; samples can be listed once a
		// study LIMS id is known. Skip eager expansion to avoid surprising
		// upstream queries here; users can re-run with --type sample on a
		// specific sample to drill in.
	}

	return report
}

func writeInfoReportJSON(out io.Writer, report infoReport) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode info report: %w", err)
	}

	return nil
}

func writeInfoReportText(out io.Writer, report infoReport, style infoStyle) {
	switch {
	case report.Sample != nil:
		writeSamplePanel(out, report, style)
	case report.Study != nil:
		writeStudyPanel(out, report, style)
	case report.Run != nil:
		writeRunPanel(out, report, style)
	case report.Library != nil:
		writeLibraryPanel(out, report, style)
	default:
		writeInfoTitle(out, style, "Result", report)
	}

	writeWarningsSection(out, report.Warnings, style)
}

// writeSampleProgressSection renders the headline progress line and the
// milestone timeline bar (when a detailed timeline is present). The per-run
// within-sequencing status is shown by writeSequencingSection instead, so
// Progress stays headline + timeline only. It degrades cleanly when progress is
// nil or not tracked.
func writeSampleProgressSection(out io.Writer, progress *mlwh.SampleProgress, style infoStyle) {
	if progress == nil {
		return
	}

	_, _ = fmt.Fprintf(out, "\n  %s  %s\n", style.section("Progress"), infoProgressHeadline(style, progress))

	if reason := strings.TrimSpace(progress.TimelineReason); reason != "" && !progress.DetailedTimeline {
		_, _ = fmt.Fprintf(out, "    %s\n", style.dim(reason))
	}

	writeMilestoneTimeline(out, style, progress.Milestones)
}

// writeRunStatusSection renders the within-sequencing status: the platform and
// current phase headline plus a proportional event timeline (the same renderer
// as the milestone bar). When there are no events it shows the not-tracked reason
// when one is given, else "none".
func writeRunStatusSection(out io.Writer, status *mlwh.RunStatusTimeline, style infoStyle) {
	if status == nil {
		return
	}

	headline := infoJoinNonEmpty(style.dim(" · "), status.Platform, infoCurrentPhase(status))
	if strings.TrimSpace(headline) == "" {
		headline = style.dim("not tracked")
	}

	_, _ = fmt.Fprintf(out, "\n  %s  %s\n", style.section("Status"), headline)

	if len(status.Events) == 0 {
		empty := "none"
		if reason := strings.TrimSpace(status.NotTracked); reason != "" {
			empty = reason
		}

		_, _ = fmt.Fprintf(out, "    %s\n", style.dim(empty))

		return
	}

	items := make([]infoTimelineItem, len(status.Events))
	for i, event := range status.Events {
		items[i] = infoTimelineItem{
			label: event.Phase,
			at:    event.EnteredAt,
			open:  strings.TrimSpace(event.Duration) == "" && i == len(status.Events)-1,
		}
	}

	writeInfoTimeline(out, style, items, "    ")
}

func writeWarningsSection(out io.Writer, warnings []string, style infoStyle) {
	if len(warnings) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "\n  %s\n", style.section("Warnings"))
	for _, warning := range warnings {
		// Warnings should stand out rather than recede, so colour them yellow.
		_, _ = fmt.Fprintf(out, "    %s\n", style.warn(warning))
	}
}

func classifyForInfo(ctx context.Context, client mlwhInfoClient, identifier, typeFlag string) (mlwh.Match, error) {
	switch strings.ToLower(strings.TrimSpace(typeFlag)) {
	case "":
		if match, err := client.ResolveStudy(ctx, identifier); err == nil {
			return match, nil
		} else if !errors.Is(err, mlwh.ErrNotFound) {
			return mlwh.Match{}, err
		}

		if match, err := resolveSampleNameForInfo(ctx, client, identifier); err == nil {
			return match, nil
		} else if !errors.Is(err, mlwh.ErrNotFound) {
			return mlwh.Match{}, err
		}

		return client.ClassifyIdentifier(ctx, identifier)
	case "sample":
		if match, err := resolveSampleNameForInfo(ctx, client, identifier); err == nil {
			return match, nil
		} else if !errors.Is(err, mlwh.ErrNotFound) {
			return mlwh.Match{}, err
		}

		return client.ResolveSample(ctx, identifier)
	case "study":
		return client.ResolveStudy(ctx, identifier)
	case "run":
		return client.ResolveRun(ctx, identifier)
	case "library":
		return client.ResolveLibrary(ctx, identifier)
	default:
		return mlwh.Match{}, fmt.Errorf("unknown --type %q (expected sample, study, run or library)", typeFlag)
	}
}

func resolveSampleNameForInfo(ctx context.Context, client mlwhInfoClient, identifier string) (mlwh.Match, error) {
	nameResolver, ok := client.(mlwhInfoSampleNameResolver)
	if !ok {
		return mlwh.Match{}, mlwh.ErrNotFound
	}

	return nameResolver.ResolveSampleName(ctx, identifier)
}

func populateSampleInfo(ctx context.Context, client mlwhInfoClient, report *infoReport, sample *mlwh.Sample) {
	if err := refreshSampleFanOut(ctx, client, sample); err != nil && !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("sample fan-out: %v", err))
	}

	if report.Study == nil {
		studies := append([]mlwh.Study(nil), sample.Studies...)
		if len(studies) == 0 {
			var err error
			studies, err = client.StudiesForSample(ctx, sample.Name)
			if err != nil {
				if !errors.Is(err, mlwh.ErrNotFound) {
					report.Warnings = append(report.Warnings, fmt.Sprintf("studies for sample: %v", err))
				}

				studies = nil
			}
		}

		if len(studies) > 0 {
			switch len(studies) {
			case 1:
				report.Study = &studies[0]
			case 0:
				// Nothing to add.
			default:
				report.Studies = studies
			}
		}
	}

	if lanes, err := client.LanesForSample(ctx, sample.Name, infoMaxRelated, 0); err == nil {
		report.Lanes = lanes
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("lanes for sample: %v", err))
	}

	if paths, err := client.IRODSPathsForSample(ctx, sample.Name, infoMaxRelated, 0); err == nil {
		report.IRODSPaths = paths
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("irods paths for sample: %v", err))
	}
}

func refreshSampleFanOut(ctx context.Context, client mlwhInfoClient, sample *mlwh.Sample) error {
	if sample == nil {
		return nil
	}
	if len(sample.Libraries) > 0 && len(sample.Studies) > 0 {
		return nil
	}

	lookups := []func(context.Context) ([]mlwh.Sample, error){}
	if strings.TrimSpace(sample.SangerSampleID) != "" {
		lookups = append(lookups, func(ctx context.Context) ([]mlwh.Sample, error) {
			return client.FindSamplesBySangerID(ctx, sample.SangerSampleID)
		})
	}
	if strings.TrimSpace(sample.IDSampleLims) != "" {
		lookups = append(lookups, func(ctx context.Context) ([]mlwh.Sample, error) {
			return client.FindSamplesByIDSampleLims(ctx, sample.IDSampleLims)
		})
	}
	if strings.TrimSpace(sample.AccessionNumber) != "" {
		lookups = append(lookups, func(ctx context.Context) ([]mlwh.Sample, error) {
			return client.FindSamplesByAccessionNumber(ctx, sample.AccessionNumber)
		})
	}

	for _, lookup := range lookups {
		samples, err := lookup(ctx)
		if err != nil {
			if errors.Is(err, mlwh.ErrNotFound) {
				continue
			}

			return err
		}
		if len(samples) == 0 {
			continue
		}

		sample.Studies = append([]mlwh.Study(nil), samples[0].Studies...)
		sample.Libraries = append([]mlwh.Library(nil), samples[0].Libraries...)

		return nil
	}

	return mlwh.ErrNotFound
}

func populateStudyInfo(ctx context.Context, client mlwhInfoClient, report *infoReport, studyLimsID string) {
	if libs, err := client.LibrariesForStudy(ctx, studyLimsID, infoMaxRelated, 0); err == nil {
		report.Libraries = libs
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("libraries for study: %v", err))
	}

	if runs, err := client.RunsForStudy(ctx, studyLimsID, infoMaxRelated, 0); err == nil {
		report.Runs = runs
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("runs for study: %v", err))
	}

	if samples, err := client.SamplesForStudy(ctx, studyLimsID, infoMaxRelated, 0); err == nil {
		report.Samples = samples
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("samples for study: %v", err))
	}
}

// resolveInfoSince turns the raw --since flag into the RFC3339 timestamp the API
// expects. An empty value defaults to now-7d (matching the overview's
// added_last_7_days window). A value is accepted either as an RFC3339 timestamp
// (passed through verbatim) or as a Go duration (e.g. 168h), interpreted as
// now-duration. The duration must be positive: --since denotes a window start
// like now-7d, so a negative or zero duration (which would yield the present or
// a future timestamp) is rejected.
func resolveInfoSince(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Now().Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339), nil
	}

	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed.Format(time.RFC3339), nil
	}

	if duration, err := time.ParseDuration(trimmed); err == nil {
		if duration <= 0 {
			return "", fmt.Errorf("invalid --since %q: duration must be positive "+
				"(it is the window start, e.g. 168h means now-168h)", raw)
		}

		return time.Now().Add(-duration).UTC().Format(time.RFC3339), nil
	}

	return "", fmt.Errorf("invalid --since %q: expected an RFC3339 timestamp or a Go duration (e.g. 168h)", raw)
}

// populateStudyFeatures fetches and records the new study feature endpoints
// (overview, status breakdown, samples-with-data all-time + since counts, and
// samples-without-data count). Each sub-endpoint degrades gracefully: a failure
// is captured as a per-section warning and does not abort the others.
func populateStudyFeatures(ctx context.Context, client mlwhInfoClient, report *infoReport, studyLimsID, since string) {
	if overview, err := client.StudyOverview(ctx, studyLimsID); err == nil {
		report.StudyOverview = &overview
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("study overview: %v", err))
	}

	if breakdown, err := client.StatusBreakdown(ctx, studyLimsID); err == nil {
		report.StatusBreakdown = &breakdown
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("status breakdown: %v", err))
	}

	report.SamplesWithDataCount = studySamplesWithDataCount(ctx, client, report, studyLimsID, since)

	if rows, err := client.SamplesWithoutData(ctx, studyLimsID, infoMaxRelated, 0); err == nil {
		report.SamplesWithoutDataCount = &infoCount{Count: len(rows)}
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("samples without data: %v", err))
	}
}

func studySamplesWithDataCount(ctx context.Context, client mlwhInfoClient, report *infoReport, studyLimsID, since string) *infoSamplesWithDataCount {
	count := &infoSamplesWithDataCount{Since: since}

	if allTime, err := client.CountSamplesWithData(ctx, studyLimsID); err == nil {
		count.AllTime = allTime.Count
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("samples with data count: %v", err))
	}

	if recent, err := client.CountSamplesWithDataSince(ctx, studyLimsID, since, ""); err == nil {
		count.AddedSince = recent.Count
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("samples with data since: %v", err))
	}

	return count
}

// populateSampleProgress fetches GET /sample/:id/progress for the resolved
// sample, degrading gracefully when the endpoint is absent.
func populateSampleProgress(ctx context.Context, client mlwhInfoClient, report *infoReport, sangerName string) {
	if progress, err := client.SampleProgress(ctx, sangerName); err == nil {
		report.SampleProgress = &progress
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("sample progress: %v", err))
	}
}

// populateRunFeatures fetches GET /run/:id/overview and GET /run/:id/status for
// the resolved run, degrading gracefully when a sub-endpoint is absent (e.g. a
// non-Illumina run has no NPG run-status).
func populateRunFeatures(ctx context.Context, client mlwhInfoClient, report *infoReport, idRun string) {
	if overview, err := client.RunOverview(ctx, idRun); err == nil {
		report.RunOverview = &overview
	} else if !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("run overview: %v", err))
	}

	// A run with no within-sequencing status (e.g. a non-Illumina run with no
	// NPG run-status) reports not-found; render the section cleanly with an
	// empty timeline ("none") rather than omitting it, mirroring the API's own
	// not-tracked semantics.
	status, err := client.RunStatus(ctx, idRun)
	if err != nil && !errors.Is(err, mlwh.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("run status: %v", err))

		return
	}

	report.RunStatus = &status
}

// infoReport is the JSON-friendly shape of `wa mlwh info` results.
type infoReport struct {
	Identifier string           `json:"identifier"`
	Kind       string           `json:"kind"`
	Canonical  string           `json:"canonical,omitempty"`
	Sample     *mlwh.Sample     `json:"sample,omitempty"`
	Study      *mlwh.Study      `json:"study,omitempty"`
	Run        *mlwh.Run        `json:"run,omitempty"`
	Library    *mlwh.Library    `json:"library,omitempty"`
	Studies    []mlwh.Study     `json:"studies,omitempty"`
	Lanes      []mlwh.Lane      `json:"lanes,omitempty"`
	Libraries  []mlwh.Library   `json:"libraries,omitempty"`
	Runs       []mlwh.Run       `json:"runs,omitempty"`
	Samples    []mlwh.Sample    `json:"samples,omitempty"`
	IRODSPaths []mlwh.IRODSPath `json:"irods_paths,omitempty"`

	StudyOverview           *mlwh.StudyOverview       `json:"study_overview,omitempty"`
	StatusBreakdown         *mlwh.StatusBreakdown     `json:"status_breakdown,omitempty"`
	SamplesWithDataCount    *infoSamplesWithDataCount `json:"samples_with_data_count,omitempty"`
	SamplesWithoutDataCount *infoCount                `json:"samples_without_data_count,omitempty"`
	SampleProgress          *mlwh.SampleProgress      `json:"sample_progress,omitempty"`
	RunOverview             *mlwh.RunOverview         `json:"run_overview,omitempty"`
	RunStatus               *mlwh.RunStatusTimeline   `json:"run_status,omitempty"`

	Warnings []string `json:"warnings,omitempty"`
}

// infoSamplesWithDataCount carries a study's all-time samples-with-data count
// alongside the recency count (samples whose data was added since the resolved
// --since timestamp).
type infoSamplesWithDataCount struct {
	AllTime    int    `json:"all_time"`
	AddedSince int    `json:"added_since"`
	Since      string `json:"since"`
}

// infoCount is a simple count envelope used where only a single figure is shown.
type infoCount struct {
	Count int `json:"count"`
}

func newMLWHInfoCommand() *cobra.Command {
	var (
		serverURL string
		typeFlag  string
		sinceFlag string
		jsonOut   bool
	)

	command := &cobra.Command{
		Use:   "info <identifier>",
		Short: "Look up everything we know about an MLWH identifier",
		Long: strings.Join([]string{
			"Look up an MLWH identifier through a wa mlwh serve API and print",
			"every related record we have for it (sample fields, the parent",
			"study, library and run associations, lanes and iRODS paths).",
			"",
			"Use this when you have a sample name, sanger ID, supplier name,",
			"sample/study UUID, study LIMS ID or accession, library",
			"pipeline_id_lims, or numeric run id and want a quick human-readable",
			"or machine-readable view of what wa knows about it. By default",
			"the command auto-detects the identifier type; pass --type to force",
			"a specific resolver. Pass --json for a single JSON object suitable",
			"for piping into jq.",
			"",
			"For a study, the report also includes the overview, status",
			"breakdown, samples-with-data counts and samples-without-data count.",
			"Use --since to set the start of the samples-with-data recency window",
			"(an RFC3339 timestamp or a Go duration such as 168h); it defaults to",
			"now-7d, matching the overview's added_last_7_days. A sample report",
			"adds its progress timeline and a run report adds the run overview",
			"and within-sequencing status.",
			"",
			"Normal CLI users should point this command at the MLWH query",
			"server with --server or WA_MLWH_SERVER_URL; database and cache",
			"credentials stay with the server process. When WA_ENV selects a",
			"scenario and no server URL is set, the command defaults to the",
			"active local MLWH API port from WA_*_SEQMETA_PORT. Operators can",
			"still run against a local cache with WA_MLWH_CACHE_PATH, or use",
			"WA_MLWH_DSN for direct local operator mode.",
			"",
			"Configuration is read from the environment. Use the persistent",
			"--env flag (or WA_ENV=development|test|production) to load matching",
			".env.<name> / .env.<name>.local files from the working directory",
			"before resolving:",
			"",
			"  WA_MLWH_SERVER_URL      Preferred. Base URL for wa mlwh serve.",
			"  WA_MLWH_BACKEND_URL     Lower-precedence compatibility default.",
			"  WA_*_SEQMETA_PORT       Scenario-local default API port.",
			"  WA_MLWH_DSN             Optional direct operator mode only;",
			"                          required when running 'wa mlwh sync'.",
			"  WA_MLWH_PASSWORD        Optional. Password used with",
			"                          WA_MLWH_DSN when syncing from upstream.",
			"  WA_MLWH_CACHE_PATH      Optional local operator cache path or",
			"                          MySQL cache DSN without a password.",
			"  WA_MLWH_CACHE_PASSWORD  Optional. SQLCipher key used to encrypt",
			"                          the local cache when set.",
			"",
			"Examples:",
			"  # Query a development stack started by make dev",
			"  wa --env development mlwh info DN1234",
			"",
			"  # Query a remote MLWH server and emit JSON",
			"  wa mlwh info 5901 --server http://host:8091 --type study --json",
			"",
			"  # Local operator cache fallback",
			"  WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite wa mlwh info 49001 --type run",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("usage: wa mlwh info <identifier>")
			}

			identifier := strings.TrimSpace(args[0])
			if identifier == "" {
				return errors.New("usage: wa mlwh info <identifier>")
			}

			since, err := resolveInfoSince(sinceFlag)
			if err != nil {
				return err
			}

			client, err := openMLWHInfoConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			// The user can sync only in local operator upstream-DSN mode (no
			// server URL and WA_MLWH_DSN set); that is the only mode where
			// 'wa mlwh sync' works. Otherwise failed-query errors must not
			// mention sync.
			canSync := strings.TrimSpace(serverURL) == "" && strings.TrimSpace(firstEnv("WA_MLWH_DSN")) != ""

			return runMLWHInfo(cmd.Context(), client, cmd.OutOrStdout(), identifier, typeFlag, since, jsonOut, canSync)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().StringVar(&typeFlag, "type", "", "force identifier type (sample|study|run|library); default is auto-detect")
	command.Flags().StringVar(&sinceFlag, "since", "", "for a study, the start of the samples-with-data recency window: an RFC3339 timestamp or a Go duration (e.g. 168h); defaults to now-7d")
	command.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON object instead of human-readable text")

	return command
}

func resolveMLWHInfoLocalConfig() (mlwh.Config, error) {
	if strings.TrimSpace(firstEnv("WA_MLWH_DSN")) != "" {
		return resolveMLWHSyncConfig()
	}

	if strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH")) == "" {
		return mlwh.Config{}, errors.New("WA_MLWH_SERVER_URL or WA_MLWH_CACHE_PATH must be set; pass --server to use a remote wa mlwh serve instance")
	}

	cacheConfig, err := resolveMLWHServeCacheConfig("", false)
	if err != nil {
		return mlwh.Config{}, err
	}

	return mlwh.Config{Cache: cacheConfig}, nil
}
