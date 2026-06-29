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
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const (
	// infoBarMinSegment is the minimum cell width any timeline segment gets from
	// the pure infoBarSegments helper so a tiny phase stays visible.
	infoBarMinSegment = 1
	// infoDateCellWidth is the minimum width the rendered bar gives each closed
	// segment so its compact transition date (e.g. "Apr 01") fits beneath it.
	infoDateCellWidth = 8
	// infoBarMaxWidth caps the proportional bar so it stays within a panel even
	// on very wide terminals (unless a date-fitting minimum needs more).
	infoBarMaxWidth = 56
	// infoEllipsis truncates over-long values.
	infoEllipsis = "…"
)

// infoRule returns a horizontal rule of the panel width (capped) used under a
// panel title.
func infoRule(style infoStyle) string {
	width := style.width
	if width > infoBarMaxWidth+24 {
		width = infoBarMaxWidth + 24
	}
	if width < 10 {
		width = 10
	}

	return style.dim(strings.Repeat("─", width))
}

// infoField formats a "Label  value" pair with the label coloured and padded to
// labelWidth so a column of fields aligns.
func infoField(style infoStyle, label, value string, labelWidth int) string {
	padded := label
	if len(label) < labelWidth {
		padded = label + strings.Repeat(" ", labelWidth-len(label))
	}

	return style.label(padded) + "  " + value
}

// infoJoinNonEmpty joins the non-empty parts with sep.
func infoJoinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}

	return strings.Join(kept, sep)
}

// infoTimelineItem is one node on a proportional timeline (a milestone or a
// run-status event). At is its RFC3339 timestamp; open marks the trailing
// current/open node (the last one, whose duration-to-next is empty).
type infoTimelineItem struct {
	label string
	at    string
	open  bool
}

// writeInfoTimeline renders a proportional timeline for the ordered items as
// three lines: a phase legend (full names in order, so none is cropped), a
// proportional bar whose closed segments are sized by the fixed inter-node time
// deltas and carry a compact duration label, and a transition-date track aligned
// to the bar. The open/current node is drawn as a trailing "▸ current" marker on
// the bar and named "(current)" in the legend. Widths come only from the
// parsed timestamps, so the output is deterministic and never depends on
// time.Now(). With a single (open-only) item it draws just the marker line.
func writeInfoTimeline(out io.Writer, style infoStyle, items []infoTimelineItem, indent string) {
	if len(items) == 0 {
		return
	}

	widths := infoBarSegmentWidths(items, style.width-len(indent)-12)

	legend, barLine, dateLine := infoTimelineLines(style, items, widths)

	_, _ = fmt.Fprintf(out, "%s%s\n", indent, legend)
	_, _ = fmt.Fprintf(out, "%s%s\n", indent, strings.TrimRight(barLine, " "))
	if strings.TrimSpace(dateLine) != "" {
		// The transition-date track is important timeline content, so render it in
		// the default foreground (not dimmed) for legibility on dark terminals.
		_, _ = fmt.Fprintf(out, "%s%s\n", indent, strings.TrimRight(dateLine, " "))
	}
}

// infoBarSegmentWidths sizes the len(items)-1 inter-node segments proportionally
// to each node-to-next time delta, giving each segment at least enough cells
// (infoDateCellWidth) to fit its compact transition date. avail is the cells
// available for the bar; the budget is the proportional cap (infoBarMaxWidth)
// but never less than the date-fitting minimum and never more than avail. With a
// single item there are no segments.
func infoBarSegmentWidths(items []infoTimelineItem, avail int) []int {
	segCount := len(items) - 1
	if segCount <= 0 {
		return nil
	}

	minSeg := infoDateCellWidth
	minTotal := minSeg * segCount

	budget := infoBarMaxWidth
	if budget < minTotal {
		budget = minTotal
	}
	if avail > 0 && budget > avail {
		budget = avail
	}
	if budget < segCount {
		budget = segCount
		minSeg = 1
	}

	weights := make([]int64, segCount)
	for i := range weights {
		weights[i] = infoSegmentSeconds(items, i)
	}

	return infoBarSegmentsMin(weights, budget, minSeg)
}

// infoBarSegmentsMin is infoBarSegments with a configurable per-segment minimum
// (used by the bar renderer so each segment can fit a compact date label).
func infoBarSegmentsMin(weights []int64, width, minSeg int) []int {
	if len(weights) == 0 {
		return nil
	}
	if minSeg < 1 {
		minSeg = 1
	}

	segments := make([]int, len(weights))

	minTotal := minSeg * len(weights)
	if width < minTotal {
		width = minTotal
	}

	var totalWeight int64
	for _, w := range weights {
		if w > 0 {
			totalWeight += w
		}
	}

	if totalWeight == 0 {
		return infoBarSegmentsEven(len(weights), width)
	}

	spare := width - minTotal
	remainders := make([]float64, len(weights))

	allocated := 0
	for i, w := range weights {
		exact := float64(spare) * float64(maxInt64(w, 0)) / float64(totalWeight)
		whole := int(exact)
		segments[i] = minSeg + whole
		remainders[i] = exact - float64(whole)
		allocated += whole
	}

	infoBarDistributeLeftover(segments, remainders, spare-allocated)

	return segments
}

// infoBarSegmentsEven splits width across n segments as evenly as possible,
// front-loading any remainder so the result is deterministic.
func infoBarSegmentsEven(n, width int) []int {
	segments := make([]int, n)
	base := width / n
	extra := width % n

	for i := range segments {
		segments[i] = base
		if i < extra {
			segments[i]++
		}
	}

	return segments
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}

	return b
}

// infoBarDistributeLeftover hands `leftover` extra cells to the segments with
// the largest fractional remainders (ties broken by lower index), so the
// segment widths sum exactly to the budget.
func infoBarDistributeLeftover(segments []int, remainders []float64, leftover int) {
	if leftover <= 0 {
		return
	}

	order := make([]int, len(segments))
	for i := range order {
		order[i] = i
	}

	sort.SliceStable(order, func(a, b int) bool {
		return remainders[order[a]] > remainders[order[b]]
	})

	for i := range leftover {
		segments[order[i%len(order)]]++
	}
}

// infoTimelineLines builds the legend, bar and date-track lines for N nodes and
// their N-1 inter-node segments. The legend lists every phase name in order
// joined by arrows (full names, so none is cropped); the bar is the proportional
// run of segments plus a trailing marker for the final node ("▸ current" when it
// is the open node); the date track places each transition date at its node's
// start column, truncating only on collision.
func infoTimelineLines(style infoStyle, items []infoTimelineItem, widths []int) (string, string, string) {
	var barLine strings.Builder

	names := make([]string, len(items))
	dates := make([]string, len(items))
	offsets := make([]int, len(items))

	offset := 0
	for i, item := range items {
		names[i] = item.label
		dates[i] = infoCompactDate(item.at)
		offsets[i] = offset

		if i < len(widths) {
			seconds := infoSegmentSeconds(items, i)
			barLine.WriteString(infoBarCell(infoSegmentDuration(time.Duration(seconds)*time.Second), widths[i]))
			offset += widths[i]
		}
	}

	last := len(items) - 1
	if items[last].open {
		names[last] += " (current)"
		barLine.WriteString(style.bold("▸ ") + "current")
	}

	return infoTimelineLegend(style, names), barLine.String(), infoTrackLine(dates, offsets)
}

// infoCompactDate renders an RFC3339 timestamp as a compact "Mon DD" (e.g.
// "Apr 01"), or "" when empty/unparseable. Used for transition dates so the
// timeline stays narrow.
func infoCompactDate(rfc3339 string) string {
	parsed, ok := parseInfoTime(rfc3339)
	if !ok {
		return ""
	}

	return parsed.Format("Jan 02")
}

func parseInfoTime(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}

	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, false
	}

	return parsed, true
}

// infoBarCell renders one proportional segment "├──2d──┤" of total width cells,
// centring the duration label between bracket/dash fills.
func infoBarCell(label string, width int) string {
	if width <= 0 {
		width = 1
	}

	if width <= 2 {
		return strings.Repeat("├", 1) + strings.Repeat("┤", width-1)
	}

	inner := width - 2
	if len(label) > inner {
		label = ""
	}

	pad := inner - len(label)
	left := pad / 2
	right := pad - left

	return "├" + strings.Repeat("─", left) + label + strings.Repeat("─", right) + "┤"
}

// infoSegmentDuration renders a compact human duration for a closed segment
// label, e.g. "2d", "5h", "30m". Zero or negative spans render as "0d".
func infoSegmentDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d > 0:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	default:
		return "0d"
	}
}

// infoTimelineLegend joins the phase names with dim arrows so every name is
// shown in full above the bar.
func infoTimelineLegend(style infoStyle, names []string) string {
	return strings.Join(names, style.dim(" → "))
}

// infoTrackLine places each label at its column offset on one line, padding with
// spaces between labels. A label wider than the gap to the next label is
// truncated so labels never overlap; the final label may run to its natural
// width.
func infoTrackLine(labels []string, offsets []int) string {
	var b strings.Builder

	for i, label := range labels {
		if b.Len() < offsets[i] {
			b.WriteString(strings.Repeat(" ", offsets[i]-b.Len()))
		}

		if i+1 < len(labels) {
			gap := offsets[i+1] - offsets[i] - 1
			if gap < 1 {
				gap = 1
			}
			label = infoTruncate(label, gap)
		}

		b.WriteString(label)
		b.WriteString(" ")
	}

	return b.String()
}

// infoTruncate shortens text to at most n runes, appending an ellipsis when it
// had to cut.
func infoTruncate(text string, n int) string {
	if n <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= n {
		return text
	}

	if n == 1 {
		return infoEllipsis
	}

	return string(runes[:n-1]) + infoEllipsis
}

// infoSegmentSeconds is the delta in seconds from node i to node i+1, floored at
// zero (an out-of-order or unparseable pair contributes nothing).
func infoSegmentSeconds(items []infoTimelineItem, i int) int64 {
	if i+1 >= len(items) {
		return 0
	}

	start, okStart := parseInfoTime(items[i].at)
	end, okEnd := parseInfoTime(items[i+1].at)
	if !okStart || !okEnd {
		return 0
	}

	if delta := end.Sub(start); delta > 0 {
		return int64(delta.Seconds())
	}

	return 0
}

// infoBarSegments distributes width cells across the given non-negative weights
// proportionally, giving every segment at least infoBarMinSegment cell so tiny
// phases stay visible, and returns counts summing to exactly width (when width
// is at least the number of segments). It is a pure deterministic function: the
// timeline bar derives its weights from fixed inter-node time deltas, never from
// the current time. With all-zero weights the width is split as evenly as
// possible. Any leftover from rounding is handed to the segments with the
// largest fractional remainders, keeping the result stable.
func infoBarSegments(weights []int64, width int) []int {
	return infoBarSegmentsMin(weights, width, infoBarMinSegment)
}

// infoPadTo left-justifies text into a field of n cells (truncating with an
// ellipsis when too long).
func infoPadTo(text string, n int) string {
	if n <= 0 {
		return ""
	}

	text = infoTruncate(text, n)
	if len(text) >= n {
		return text
	}

	return text + strings.Repeat(" ", n-len(text))
}
