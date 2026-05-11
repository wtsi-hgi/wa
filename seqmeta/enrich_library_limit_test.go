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

package seqmeta

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func limitedLibrarySample(studyID, sangerSampleID, name, libraryType string) mlwh.Sample {
	return mlwh.Sample{
		Name:           name,
		SangerSampleID: sangerSampleID,
		Studies:        []mlwh.Study{{IDStudyLims: studyID}},
		Libraries:      []mlwh.Library{{PipelineIDLims: libraryType, IDStudyLims: studyID}},
	}
}

func TestLibraryTypeEnrichmentLimitsPreventsLargePayload(t *testing.T) {
	convey.Convey("Library type enrichment limits total samples to prevent browser-freezing payloads", t, func() {
		ctx := context.Background()

		// Simulate "Chromium single cell 3 prime v3" which returns 1000 samples across 53 studies
		// This is too much data for the frontend to handle efficiently
		samples := make([]mlwh.Sample, 0, 1000)
		sampleIndex := 0
		for studyNum := range 53 {
			for sampleNum := range 19 {
				studyID := "study_" + string(rune('0'+studyNum))
				// Use unique sample names across all studies for proper filtering
				sampleName := studyID + "_sample_" + string(rune('A'+sampleNum))
				samples = append(samples, limitedLibrarySample(studyID, sampleName+"_id", sampleName, "Chromium single cell 3 prime v3"))
				sampleIndex++
			}
		}

		limitCalled := false
		provider := &MockProvider{
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "Chromium single cell 3 prime v3")
				limitCalled = true
				return samples, nil
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				return &mlwh.Study{IDStudyLims: identifier, Name: "Study " + identifier}, nil
			},
		}

		result, err := Enrich(ctx, provider, "Chromium single cell 3 prime v3")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(limitCalled, convey.ShouldBeTrue)

		if result == nil {
			return
		}

		// The key assertion: total samples should be limited to MaxLibraryTypeSamples
		// to prevent massive payloads that freeze the browser when spanning multiple studies
		convey.So(len(result.Graph.Samples), convey.ShouldBeLessThanOrEqualTo, MaxLibraryTypeSamples)

		// Critical: verify nested StudyDetails also respect the limit, proving the serialized
		// JSON payload cannot balloon via deeply nested structures
		totalNestedSamples := 0
		for _, studyDetail := range result.Graph.StudyDetails {
			for _, library := range studyDetail.Libraries {
				totalNestedSamples += len(library.Samples)
			}
		}
		convey.So(totalNestedSamples, convey.ShouldBeLessThanOrEqualTo, MaxLibraryTypeSamples)

		// If we hit the limit, result should be marked as partial
		if len(samples) > MaxLibraryTypeSamples {
			convey.So(result.Partial, convey.ShouldBeTrue)
			convey.So(result.Missing, convey.ShouldNotBeEmpty)

			foundTruncation := false
			for _, missing := range result.Missing {
				if missing.Reason == ReasonSamplesTruncated {
					foundTruncation = true
					break
				}
			}
			convey.So(foundTruncation, convey.ShouldBeTrue)
		}
	})
}

func TestStudyEnrichmentIncludesLibraryMetadata(t *testing.T) {
	convey.Convey("Study enrichment returns study_detail.libraries with samples grouped by library type", t, func() {
		ctx := context.Background()

		// Simulate study 6568 with multiple samples across different library types
		studySamples := []mlwh.Sample{
			limitedLibrarySample("6568", "S1", "Sample1", "Chromium single cell 3 prime v3"),
			limitedLibrarySample("6568", "S2", "Sample2", "Chromium single cell 3 prime v3"),
			limitedLibrarySample("6568", "S3", "Sample3", "Standard"),
			limitedLibrarySample("6568", "S4", "Sample4", "Standard"),
			limitedLibrarySample("6568", "S5", "Sample5", "Standard"),
		}

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				convey.So(identifier, convey.ShouldEqual, "6568")
				return &mlwh.Study{IDStudyLims: "6568", Name: "Study 6568", AccessionNumber: "EGAS00001006568"}, nil
			},
			AllSamplesForStudyFunc: func(_ context.Context, studyLimsID string) ([]mlwh.Sample, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "6568")
				return studySamples, nil
			},
		}

		result, err := Enrich(ctx, provider, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)

		if result == nil {
			return
		}

		// Verify study_detail exists and has libraries
		convey.So(result.Graph.StudyDetail, convey.ShouldNotBeNil)
		convey.So(result.Graph.StudyDetail.Libraries, convey.ShouldNotBeEmpty)

		// Verify libraries are grouped correctly by type
		convey.So(len(result.Graph.StudyDetail.Libraries), convey.ShouldEqual, 2)

		// Find each library type and verify sample counts
		chromiumLib := findLibrary(result.Graph.StudyDetail.Libraries, "Chromium single cell 3 prime v3")
		convey.So(chromiumLib, convey.ShouldNotBeNil)
		convey.So(len(chromiumLib.Samples), convey.ShouldEqual, 2)

		standardLib := findLibrary(result.Graph.StudyDetail.Libraries, "Standard")
		convey.So(standardLib, convey.ShouldNotBeNil)
		convey.So(len(standardLib.Samples), convey.ShouldEqual, 3)

		// Verify the serialized JSON contains library_details
		payload, marshalErr := json.Marshal(result)
		convey.So(marshalErr, convey.ShouldBeNil)

		var decoded map[string]any
		convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)

		graph := decoded["graph"].(map[string]any)
		studyDetail := graph["study_detail"].(map[string]any)
		libraryDetails := studyDetail["library_details"].([]any)

		convey.So(len(libraryDetails), convey.ShouldEqual, 2)

		// Verify total sample count in nested structures matches
		totalNestedSamples := 0
		for _, detail := range libraryDetails {
			samples := detail.(map[string]any)["samples"].([]any)
			totalNestedSamples += len(samples)
		}
		convey.So(totalNestedSamples, convey.ShouldEqual, 5)
	})
}

func findLibrary(libraries []mlwh.LibraryDetail, libraryType string) *mlwh.LibraryDetail {
	for i := range libraries {
		if libraries[i].Library.PipelineIDLims == libraryType {
			return &libraries[i]
		}
	}
	return nil
}
