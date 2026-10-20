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
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/wtsi-hgi/wa/mlwh"
)

const (
	infoKindAmbiguousSample = "ambiguous_sample"

	// infoCandidateLimit bounds the sample search behind the candidate list; an
	// identifier shared by more samples than this lists only the first page.
	infoCandidateLimit = 200
)

func needsShellQuoting(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	default:
		return !strings.ContainsRune("_@%+=:,./-", r)
	}
}

// mlwhInfoCandidateSearcher is the sample-search capability `wa mlwh info` uses
// to list the samples an ambiguous identifier names. Both the local and remote
// clients provide it, and it works against servers of any API version.
type mlwhInfoCandidateSearcher interface {
	SearchSamplesWithOptions(ctx context.Context, term string, opts mlwh.SampleSearchOptions, limit, offset int) ([]mlwh.Sample, error)
}

// buildInfoCandidatesReport lists the samples whose identifiers equal
// identifier. It reports false when fewer than two samples match, so the caller
// keeps the original ambiguity error (for example an ambiguous study).
func buildInfoCandidatesReport(ctx context.Context, client mlwhInfoClient, identifier string) (infoReport, bool) {
	searcher, ok := client.(mlwhInfoCandidateSearcher)
	if !ok {
		return infoReport{}, false
	}

	samples, err := searcher.SearchSamplesWithOptions(ctx, identifier, mlwh.SampleSearchOptions{}, infoCandidateLimit, 0)
	if err != nil {
		return infoReport{}, false
	}

	matches := make([]mlwh.Sample, 0, len(samples))
	for _, sample := range samples {
		if sampleHasIdentifier(sample, identifier) {
			matches = append(matches, sample)
		}
	}
	matches, uniqueIDClash := addUniqueIdentifierMatches(ctx, client, identifier, matches)
	if len(matches) < 2 {
		return infoReport{}, false
	}

	multiSample := !uniqueIDClash && mlwh.MultiSampleExportCovers(matches, identifier)
	report := infoReport{
		Identifier:        identifier,
		Kind:              infoKindAmbiguousSample,
		multiSampleExport: multiSample,
		NextCommands:      infoCandidateNextCommands(matches, identifier, multiSample),
	}

	withData, err := searcher.SearchSamplesWithOptions(ctx, identifier, mlwh.SampleSearchOptions{DeliverablesOnly: true}, infoCandidateLimit, 0)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("deliverable data lookup: %v", err))
	}

	report.Candidates = infoCandidatesFrom(matches, withData)

	return report, true
}

func sampleHasIdentifier(sample mlwh.Sample, identifier string) bool {
	for _, value := range []string{sample.Name, sample.SangerSampleID, sample.SupplierName, sample.AccessionNumber, sample.DonorID, sample.IDSampleLims} {
		if strings.EqualFold(value, identifier) {
			return true
		}
	}

	return false
}

// addUniqueIdentifierMatches adds the samples holding identifier as a Sanger
// sample id, LIMS id or accession, which sample search does not index. It also
// reports when one of those lookups is itself ambiguous, since such a clash
// rules out a multi-sample export even though the samples are not listed. The
// result is ordered by Sanger name.
func addUniqueIdentifierMatches(ctx context.Context, client mlwhInfoClient, identifier string, matches []mlwh.Sample) ([]mlwh.Sample, bool) {
	var clash bool
	for _, find := range []func(context.Context, string) ([]mlwh.Sample, error){
		client.FindSamplesBySangerID, client.FindSamplesByIDSampleLims, client.FindSamplesByAccessionNumber,
	} {
		found, err := find(ctx, identifier)
		if errors.Is(err, mlwh.ErrAmbiguous) {
			clash = true
		}
		for _, sample := range found {
			if !slices.ContainsFunc(matches, func(match mlwh.Sample) bool { return match.IDSampleTmp == sample.IDSampleTmp }) {
				matches = append(matches, sample)
			}
		}
	}

	slices.SortStableFunc(matches, func(a, b mlwh.Sample) int { return strings.Compare(a.Name, b.Name) })

	return matches, clash
}

// infoCandidateNextCommands suggests one export covering every candidate when
// a multi-sample export accepts identifier, otherwise one export per candidate
// by Sanger name, followed by the deliverable-data search.
func infoCandidateNextCommands(matches []mlwh.Sample, identifier string, multiSample bool) []string {
	quoted := shellQuote(identifier)
	search := "wa mlwh search --type sample " + quoted + " --deliverables-only"
	if multiSample {
		return []string{"wa mlwh export irods sample " + quoted, search}
	}

	commands := make([]string, 0, len(matches)+1)
	for _, sample := range matches {
		commands = append(commands, "wa mlwh export irods sample "+shellQuote(sample.Name))
	}

	return append(commands, search)
}

// shellQuote returns value as one POSIX shell word, single-quoting it unless it
// holds only characters the shell never interprets.
func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, needsShellQuoting) < 0 {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func infoCandidatesFrom(matches, withData []mlwh.Sample) []infoSampleCandidate {
	hasData := make(map[int64]bool, len(withData))
	for _, sample := range withData {
		hasData[sample.IDSampleTmp] = true
	}

	candidates := make([]infoSampleCandidate, len(matches))
	for index, sample := range matches {
		studies := make([]infoCandidateStudy, len(sample.Studies))
		for studyIndex, study := range sample.Studies {
			studies[studyIndex] = infoCandidateStudy{IDStudyLims: study.IDStudyLims, Name: study.Name}
		}

		candidates[index] = infoSampleCandidate{
			Name:               sample.Name,
			SangerSampleID:     sample.SangerSampleID,
			SupplierName:       sample.SupplierName,
			DonorID:            sample.DonorID,
			AccessionNumber:    sample.AccessionNumber,
			Studies:            studies,
			HasDeliverableData: hasData[sample.IDSampleTmp],
		}
	}

	return candidates
}

type infoCandidateStudy struct {
	IDStudyLims string `json:"id_study_lims"`
	Name        string `json:"name"`
}

func infoCandidateStudies(studies []infoCandidateStudy) string {
	if len(studies) == 0 {
		return "-"
	}

	labels := make([]string, len(studies))
	for index, study := range studies {
		labels[index] = strings.TrimSpace(study.IDStudyLims + " " + study.Name)
	}

	return strings.Join(labels, "; ")
}

// infoSampleCandidate is one sample an ambiguous identifier names.
// HasDeliverableData uses the sample search deliverables-only rule.
type infoSampleCandidate struct {
	Name               string               `json:"name"`
	SangerSampleID     string               `json:"sanger_sample_id"`
	SupplierName       string               `json:"supplier_name"`
	DonorID            string               `json:"donor_id"`
	AccessionNumber    string               `json:"accession_number"`
	Studies            []infoCandidateStudy `json:"studies"`
	HasDeliverableData bool                 `json:"has_deliverable_data"`
}

// writeInfoCandidates prints the candidate report and returns the error that
// makes the command exit non-zero: the identifier did not resolve to one record.
func writeInfoCandidates(out io.Writer, report infoReport, jsonOut bool) error {
	if jsonOut {
		if err := writeInfoReportJSON(out, report); err != nil {
			return err
		}
	} else {
		writeInfoCandidatesText(out, report, resolveInfoStyle(out))
	}

	if !report.multiSampleExport {
		return fmt.Errorf("%q matches %d samples; use one Sanger sample name", report.Identifier, len(report.Candidates))
	}

	return fmt.Errorf("%q matches %d samples; use one Sanger sample name, or the commands listed above to cover them all",
		report.Identifier, len(report.Candidates))
}

func writeInfoCandidatesText(out io.Writer, report infoReport, style infoStyle) {
	writeInfoTitle(out, style, "Samples", report)
	_, _ = fmt.Fprintf(out, "  %q matches %d samples:\n\n", report.Identifier, len(report.Candidates))

	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "  name\tsanger_sample_id\tdeliverable data\tstudies")
	for _, candidate := range report.Candidates {
		_, _ = fmt.Fprintf(table, "  %s\t%s\t%s\t%s\n",
			candidate.Name, candidate.SangerSampleID, infoYesNo(candidate.HasDeliverableData), infoCandidateStudies(candidate.Studies))
	}
	_ = table.Flush()

	exports, search := report.NextCommands[:len(report.NextCommands)-1], report.NextCommands[len(report.NextCommands)-1]
	exportHeading := "Export the files of one sample"
	if report.multiSampleExport {
		exportHeading = "Export the files of every matching sample"
	}
	_, _ = fmt.Fprintf(out, "\n  %s\n", style.section(exportHeading))
	for _, command := range exports {
		_, _ = fmt.Fprintf(out, "    %s\n", command)
	}
	_, _ = fmt.Fprintf(out, "  %s\n    %s\n", style.section("List only the samples with deliverable data"), search)

	writeCacheSyncedLine(out, style, report.CacheSyncedAt)
	writeWarningsSection(out, report.Warnings, style)
}

func infoYesNo(value bool) string {
	if value {
		return "yes"
	}

	return "no"
}
