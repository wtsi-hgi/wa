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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wtsi-hgi/wa/mlwh"
)

const (
	infoKindAmbiguousSample = "ambiguous_sample"

	// infoCandidateLimit bounds the sample search behind the candidate list; an
	// identifier shared by more samples than this lists only the first page.
	infoCandidateLimit = 200

	// infoChoiceAttempts bounds how many answers the sample prompt reads before
	// giving up.
	infoChoiceAttempts = 3

	// infoCandidatesTimedOutWarning is the candidates report warning when
	// infoCandidatesTimeout cut the candidate lookups off.
	infoCandidatesTimedOutWarning = "candidate lookups timed out; some identifiers or data flags may be missing"
)

// infoCandidatesTimeout bounds the lookups behind the candidate list of an
// identifier that names several samples. Tests replace it.
var infoCandidatesTimeout = time.Minute

// infoPromptAllowed reports whether `wa mlwh info` may ask the user to choose
// among the samples an identifier names: both its input and its output must be
// terminals. Tests replace it.
var infoPromptAllowed = func(in io.Reader, out io.Writer) bool {
	return isTerminalStream(in) && isTerminalStream(out)
}

// mlwhInfoCandidateSearcher is the sample-search capability `wa mlwh info` uses
// to list the samples an ambiguous identifier names. Both the local and remote
// clients provide it, and it works against servers of any API version.
type mlwhInfoCandidateSearcher interface {
	SearchSamplesWithOptions(ctx context.Context, term string, opts mlwh.SampleSearchOptions, limit, offset int) ([]mlwh.Sample, error)
}

// infoUniqueIdentifier returns the first of sample's Sanger name, Sanger sample
// id, LIMS id, UUID and accession that no other sample holds and that
// `wa mlwh info` resolves to sample, or "" when none qualifies. Some resolvers
// pick the first of several matching samples, so resolving to sample alone
// does not prove the value is unique. Its requests are probes.
func infoUniqueIdentifier(ctx context.Context, client mlwhInfoClient, searcher mlwhInfoCandidateSearcher, sample mlwh.Sample) string {
	ctx = mlwh.WithLookupProbe(ctx)

	samplesNamed := func(ctx context.Context, name string) ([]mlwh.Sample, error) {
		found, err := searcher.SearchSamplesWithOptions(ctx, name, mlwh.SampleSearchOptions{}, infoCandidateLimit, 0)

		return slices.DeleteFunc(found, func(other mlwh.Sample) bool { return !strings.EqualFold(other.Name, name) }), err
	}

	for _, key := range []struct {
		value   string
		holders infoSampleHolders
	}{
		{sample.Name, samplesNamed},
		{sample.SangerSampleID, client.FindSamplesBySangerID},
		{sample.IDSampleLims, client.FindSamplesByIDSampleLims},
		{sample.UUIDSampleLims, nil}, // UUIDs are unique by construction
		{sample.AccessionNumber, client.FindSamplesByAccessionNumber},
	} {
		if strings.TrimSpace(key.value) == "" || !infoOnlyHolder(ctx, key.holders, key.value, sample) {
			continue
		}

		match, err := classifyForInfo(ctx, client, key.value, "")
		if err == nil && match.Sample != nil && match.Sample.IDSampleTmp == sample.IDSampleTmp {
			return key.value
		}
	}

	return ""
}

// infoOnlyHolder reports whether sample is the only sample holders finds for
// value; a nil holders trusts the value to be unique.
func infoOnlyHolder(ctx context.Context, holders infoSampleHolders, value string, sample mlwh.Sample) bool {
	if holders == nil {
		return true
	}

	found, err := holders(ctx, value)

	return err == nil && len(found) == 1 && found[0].IDSampleTmp == sample.IDSampleTmp
}

// infoSampleCandidate is one sample an ambiguous identifier names. Identifier
// is a value `wa mlwh info` resolves to this sample alone, empty when none of
// the sample's identifiers does. HasData uses the sample search
// deliverables-only rule.
type infoSampleCandidate struct {
	Identifier      string               `json:"identifier"`
	Name            string               `json:"name"`
	SangerSampleID  string               `json:"sanger_sample_id"`
	SupplierName    string               `json:"supplier_name"`
	AccessionNumber string               `json:"accession_number"`
	Studies         []infoCandidateStudy `json:"studies"`
	HasData         bool                 `json:"has_data"`
}

type infoCandidateStudy struct {
	IDStudyLims string `json:"id_study_lims"`
	Name        string `json:"name"`
}

// writeInfoCandidateRows prints the candidates as a numbered, aligned table.
func writeInfoCandidateRows(out io.Writer, candidates []infoSampleCandidate) {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "  #\tname\tsanger_sample_id\tsupplier_name\tdata\tstudies")
	for index, candidate := range candidates {
		_, _ = fmt.Fprintf(table, "  %d\t%s\t%s\t%s\t%s\t%s\n", index+1, infoCandidateName(candidate),
			candidate.SangerSampleID, candidate.SupplierName, infoYesNo(candidate.HasData), infoCandidateStudies(candidate.Studies))
	}
	_ = table.Flush()
}

// infoCandidateName shows the candidate's Sanger name, and the identifier to
// use instead when that name does not name this sample alone.
func infoCandidateName(candidate infoSampleCandidate) string {
	if candidate.Identifier == "" || candidate.Identifier == candidate.Name {
		return candidate.Name
	}

	return fmt.Sprintf("%s (not unique; use %s)", candidate.Name, candidate.Identifier)
}

// infoCandidatesReport lists the samples identifier names; as JSON it is the
// "results" object of `wa mlwh info --json`, while Identifier and
// CacheSyncedAt go in "query" and Warnings beside it.
type infoCandidatesReport struct {
	Kind          string                `json:"kind"`
	Identifier    string                `json:"-"`
	Candidates    []infoSampleCandidate `json:"candidates"`
	CacheSyncedAt string                `json:"-"`
	Warnings      []string              `json:"-"`
}

// buildInfoCandidatesReport lists the samples whose identifiers equal
// identifier. It reports false when fewer than two samples match, so the caller
// keeps the original ambiguity error (for example an ambiguous study). When
// ctx's deadline cuts the per-candidate lookups off, the report says so in
// Warnings.
func buildInfoCandidatesReport(ctx context.Context, client mlwhInfoClient, identifier string) (infoCandidatesReport, bool) {
	searcher, ok := client.(mlwhInfoCandidateSearcher)
	if !ok {
		return infoCandidatesReport{}, false
	}

	samples, err := searcher.SearchSamplesWithOptions(ctx, identifier, mlwh.SampleSearchOptions{}, infoCandidateLimit, 0)
	if err != nil {
		return infoCandidatesReport{}, false
	}

	matches := make([]mlwh.Sample, 0, len(samples))
	for _, sample := range samples {
		if sampleHasIdentifier(sample, identifier) {
			matches = append(matches, sample)
		}
	}
	matches = addUniqueIdentifierMatches(ctx, client, identifier, matches)
	if len(matches) < 2 {
		return infoCandidatesReport{}, false
	}

	report := infoCandidatesReport{Kind: infoKindAmbiguousSample, Identifier: identifier}

	withData, err := searcher.SearchSamplesWithOptions(ctx, identifier, mlwh.SampleSearchOptions{DeliverablesOnly: true}, infoCandidateLimit, 0)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("deliverable data lookup: %v", err))
	}

	report.Candidates = infoCandidatesFrom(ctx, client, searcher, matches, withData)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		report.Warnings = append(report.Warnings, infoCandidatesTimedOutWarning)
	}

	return report, true
}

// writeInfoCandidatesJSON prints the candidates report and returns the error
// that exits with ExitCodeAmbiguous.
func writeInfoCandidatesJSON(out io.Writer, report infoCandidatesReport, typeFlag string) error {
	err := writeMLWHJSONDocument(out, "info report", mlwhJSONDocument[infoQuery, infoCandidatesReport]{
		Query:    newInfoQuery(report.Identifier, typeFlag, report.CacheSyncedAt),
		Results:  report,
		Warnings: report.Warnings,
	})
	if err != nil {
		return err
	}

	return infoAmbiguousError(report, "give one of the candidates' identifiers")
}

// writeInfoCandidatesList prints the numbered candidate list for a reader who
// cannot be asked to choose, and returns the error that exits with
// ExitCodeAmbiguous.
func writeInfoCandidatesList(out io.Writer, report infoCandidatesReport) error {
	writeInfoCandidatesText(out, report, resolveInfoStyle(out))

	return infoAmbiguousError(report, "give one of the identifiers listed")
}

// chooseInfoCandidate prints the candidate list and asks the user to pick one
// by number, re-asking after an invalid answer up to infoChoiceAttempts times.
// It returns the chosen candidate's identifier, or the ExitCodeAmbiguous error
// when the user cancels with an empty answer, end of input or an interrupt
// (ctx done), or gives up.
func chooseInfoCandidate(ctx context.Context, in io.Reader, out io.Writer, report infoCandidatesReport) (string, error) {
	writeInfoCandidatesText(out, report, resolveInfoStyle(out))
	_, _ = fmt.Fprintln(out)

	reader := bufio.NewReader(in)
	count := len(report.Candidates)
	for range infoChoiceAttempts {
		_, _ = fmt.Fprintf(out, "Choose a sample [1-%d] (Enter to cancel): ", count)

		line, readErr := readInfoChoice(ctx, reader)
		answer := strings.TrimSpace(line)
		if answer == "" {
			if readErr != nil {
				_, _ = fmt.Fprintln(out)
			}

			break
		}

		if choice, err := strconv.Atoi(answer); err == nil && choice >= 1 && choice <= count {
			if identifier := report.Candidates[choice-1].Identifier; identifier != "" {
				return identifier, nil
			}

			_, _ = fmt.Fprintf(out, "sample %d has no identifier that names it alone\n", choice)
		} else {
			_, _ = fmt.Fprintf(out, "%q is not a number from 1 to %d\n", answer, count)
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
	}

	return "", infoAmbiguousError(report, "no sample chosen")
}

// readInfoChoice reads one answer line, or returns ctx's error when ctx is done
// first. A terminal read cannot be cancelled, so an interrupted read's
// goroutine ends only when its read returns, as the process exits.
func readInfoChoice(ctx context.Context, reader *bufio.Reader) (string, error) {
	type answer struct {
		line string
		err  error
	}

	answers := make(chan answer, 1)
	go func() {
		line, err := reader.ReadString('\n')
		answers <- answer{line, err}
	}()

	select {
	case got := <-answers:
		return got.line, got.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func infoAmbiguousError(report infoCandidatesReport, advice string) error {
	return exitCodeError{
		code: ExitCodeAmbiguous,
		err:  fmt.Errorf("%q matches %d samples; %s", report.Identifier, len(report.Candidates), advice),
	}
}

// infoSampleHolders returns the samples holding one kind of identifier.
type infoSampleHolders func(ctx context.Context, value string) ([]mlwh.Sample, error)

func sampleHasIdentifier(sample mlwh.Sample, identifier string) bool {
	for _, value := range []string{sample.Name, sample.SangerSampleID, sample.SupplierName, sample.AccessionNumber, sample.DonorID, sample.IDSampleLims} {
		if strings.EqualFold(value, identifier) {
			return true
		}
	}

	return false
}

// addUniqueIdentifierMatches adds the samples holding identifier as a Sanger
// sample id, LIMS id or accession, which sample search does not index. The
// result is ordered by Sanger name. Its lookups are probes.
func addUniqueIdentifierMatches(ctx context.Context, client mlwhInfoClient, identifier string, matches []mlwh.Sample) []mlwh.Sample {
	ctx = mlwh.WithLookupProbe(ctx)

	for _, find := range []func(context.Context, string) ([]mlwh.Sample, error){
		client.FindSamplesBySangerID, client.FindSamplesByIDSampleLims, client.FindSamplesByAccessionNumber,
	} {
		found, _ := find(ctx, identifier)
		for _, sample := range found {
			if !slices.ContainsFunc(matches, func(match mlwh.Sample) bool { return match.IDSampleTmp == sample.IDSampleTmp }) {
				matches = append(matches, sample)
			}
		}
	}

	slices.SortStableFunc(matches, func(a, b mlwh.Sample) int { return strings.Compare(a.Name, b.Name) })

	return matches
}

func infoCandidatesFrom(ctx context.Context, client mlwhInfoClient, searcher mlwhInfoCandidateSearcher, matches, withData []mlwh.Sample) []infoSampleCandidate {
	hasData := make(map[int64]bool, len(withData))
	for _, sample := range withData {
		hasData[sample.IDSampleTmp] = true
	}

	identifiers := make([]string, len(matches))
	runBounded(len(matches), func(index int) {
		identifiers[index] = infoUniqueIdentifier(ctx, client, searcher, matches[index])
	})

	candidates := make([]infoSampleCandidate, len(matches))
	for index, sample := range matches {
		studies := make([]infoCandidateStudy, len(sample.Studies))
		for studyIndex, study := range sample.Studies {
			studies[studyIndex] = infoCandidateStudy{IDStudyLims: study.IDStudyLims, Name: study.Name}
		}

		candidates[index] = infoSampleCandidate{
			Identifier:      identifiers[index],
			Name:            sample.Name,
			SangerSampleID:  sample.SangerSampleID,
			SupplierName:    sample.SupplierName,
			AccessionNumber: sample.AccessionNumber,
			Studies:         studies,
			HasData:         hasData[sample.IDSampleTmp],
		}
	}

	return candidates
}

func writeInfoCandidatesText(out io.Writer, report infoCandidatesReport, style infoStyle) {
	writeInfoTitle(out, style, "Samples", infoReport{Identifier: report.Identifier, Kind: report.Kind})
	_, _ = fmt.Fprintf(out, "  %q matches %d samples:\n\n", report.Identifier, len(report.Candidates))

	writeInfoCandidateRows(out, report.Candidates)

	writeCacheSyncedLine(out, style, report.CacheSyncedAt)
	writeWarningsSection(out, report.Warnings, style)
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

func infoYesNo(value bool) string {
	if value {
		return "yes"
	}

	return "no"
}
