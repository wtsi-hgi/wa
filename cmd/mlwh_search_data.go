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
	"strings"
	"time"

	"github.com/wtsi-hgi/wa/mlwh"
)

const (
	// mlwhSearchWithDataTimeout bounds a `wa mlwh search --with-data`
	// invocation, which adds one export per returned sample to the search.
	mlwhSearchWithDataTimeout = 2 * time.Minute

	// mlwhSearchDataTimedOutMessage is shown when mlwhSearchWithDataTimeout
	// cuts off the per-sample data exports.
	mlwhSearchDataTimedOutMessage = "search timed out while listing sample data; use --limit to return fewer samples"
)

// searchDataColumns are the irods export columns each --with-data row holds.
var searchDataColumns = []string{"id_study_lims", "study_accession_number", "platform", "id_run", "lane", "tag_index", "manual_qc", "irods_path"}

// mlwhSampleDataExporter is the export capability `wa mlwh search --with-data`
// uses to list each sample's data. Both the local and remote clients provide it.
type mlwhSampleDataExporter interface {
	Export(ctx context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error)
}

// addSampleSearchData fills in each returned sample's Data with the irods
// sample export for its Sanger name, running at most mlwhLookupConcurrency
// exports at once. A sample whose export fails carries the error as DataError.
// When the deadline cuts the exports off, report is marked TimedOut and the
// samples it cut off say only that they timed out; when ctx is cancelled, the
// cancellation is returned.
func addSampleSearchData(ctx context.Context, client mlwhSearchClient, report *searchReport) error {
	exporter, ok := client.(mlwhSampleDataExporter)
	if !ok {
		return fmt.Errorf("--with-data: the MLWH client cannot export sample data")
	}

	results := report.Samples.Results
	runBounded(len(results), func(index int) {
		sample := &results[index]

		data, err := sampleSearchData(ctx, exporter, sample.Name)
		switch {
		case err == nil:
			sample.Data = &data
		case ctx.Err() != nil:
			sample.DataError = "timed out"
		default:
			sample.DataError = err.Error()
		}
	})

	switch err := ctx.Err(); {
	case errors.Is(err, context.DeadlineExceeded):
		report.TimedOut, report.dataTimedOut = true, true
	case err != nil:
		return fmt.Errorf("list sample data: %w", err)
	}

	return nil
}

func sampleSearchData(ctx context.Context, exporter mlwhSampleDataExporter, sangerName string) ([]searchSampleData, error) {
	result, err := exporter.Export(ctx, mlwh.ExportRelationship{Children: "irods", ParentKind: "sample"}, sangerName,
		mlwh.ExportOptions{Columns: searchDataColumns, FileType: "cram", All: true})
	if err != nil {
		return nil, err
	}

	column := make(map[string]int, len(result.Columns))
	for index, name := range result.Columns {
		column[name] = index
	}
	cell := func(row []string, name string) string {
		if index, ok := column[name]; ok && index < len(row) {
			return row[index]
		}

		return ""
	}

	data := []searchSampleData{}
	_, err = result.ForEachRow(ctx, func(row []string) error {
		data = append(data, searchSampleData{
			IDStudyLims:          cell(row, "id_study_lims"),
			StudyAccessionNumber: cell(row, "study_accession_number"),
			Platform:             cell(row, "platform"),
			IDRun:                cell(row, "id_run"),
			Lane:                 cell(row, "lane"),
			TagIndex:             cell(row, "tag_index"),
			ManualQC:             cell(row, "manual_qc"),
			IRODSPath:            cell(row, "irods_path"),
		})

		return nil
	})

	return data, err
}

// searchSampleData is one product of a sample with its iRODS path.
type searchSampleData struct {
	IDStudyLims          string `json:"id_study_lims"`
	StudyAccessionNumber string `json:"study_accession_number"`
	Platform             string `json:"platform"`
	IDRun                string `json:"id_run"`
	Lane                 string `json:"lane"`
	TagIndex             string `json:"tag_index"`
	ManualQC             string `json:"manual_qc"`
	IRODSPath            string `json:"irods_path"`
}

// searchSample is one sample search result. Data, present only with
// --with-data, lists the sample's products that have an iRODS object (the
// default irods sample export: deliverable CRAMs); DataError explains why it
// could not be listed.
type searchSample struct {
	mlwh.Sample

	Data      *[]searchSampleData `json:"data,omitempty"`
	DataError string              `json:"data_error,omitempty"`
}

// writeSearchSampleData prints a sample's data rows indented under it, one
// compact line each: study, platform, run_lane#tag, manual QC and iRODS path.
func writeSearchSampleData(out io.Writer, sample searchSample) {
	switch {
	case sample.DataError != "":
		_, _ = fmt.Fprintf(out, "    data unavailable: %s\n", sample.DataError)
	case sample.Data == nil:
	case len(*sample.Data) == 0:
		_, _ = fmt.Fprintln(out, "    no data")
	default:
		for _, row := range *sample.Data {
			product := row.IDRun + "_" + row.Lane
			if row.TagIndex != "" {
				product += "#" + row.TagIndex
			}

			fields := []string{row.IDStudyLims, row.Platform, product, row.ManualQC, row.IRODSPath}
			for index, field := range fields {
				if strings.TrimSpace(field) == "" {
					fields[index] = "-"
				}
			}
			_, _ = fmt.Fprintf(out, "    %s\n", strings.Join(fields, " "))
		}
	}
}
