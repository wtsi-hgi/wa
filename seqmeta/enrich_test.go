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
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

type cacheBackedEnrichProvider struct {
	*mlwh.Client
}

func (p *cacheBackedEnrichProvider) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return p.ReadDB().QueryContext(ctx, query, args...)
}

func (p *cacheBackedEnrichProvider) GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error) {
	match, err := p.ResolveStudy(ctx, identifier)
	if err != nil {
		return nil, err
	}

	return match.Study, nil
}

func (p *cacheBackedEnrichProvider) AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error) {
	return p.SamplesForStudy(ctx, studyLimsID, providerFetchLimit, 0)
}

func (p *cacheBackedEnrichProvider) FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error) {
	return p.SamplesForRun(ctx, strconv.Itoa(idRun), providerFetchLimit, 0)
}

func (p *cacheBackedEnrichProvider) GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
	return p.IRODSPathsForSample(ctx, sangerName, providerFetchLimit, 0)
}

func TestEnrichUsesMLWHDetailGraphs(t *testing.T) {
	ctx := context.Background()

	convey.Convey("C2.2: buildSampleDetailFromProvider preserves one library entry per sample pairing", t, func() {
		sample := mlwh.Sample{
			Name:           "Sample 1",
			SangerSampleID: "S1",
			Libraries: []mlwh.Library{
				{PipelineIDLims: "Standard", IDStudyLims: "6568"},
				{PipelineIDLims: "Chromium", IDStudyLims: "6569"},
			},
		}

		provider := &MockProvider{
			LanesForSampleFunc: func(_ context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
				convey.So(sangerName, convey.ShouldEqual, "Sample 1")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return nil, mlwh.ErrNotFound
			},
			IRODSPathsForSampleFunc: func(_ context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
				convey.So(sangerName, convey.ShouldEqual, "Sample 1")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return nil, mlwh.ErrNotFound
			},
		}

		detail, err := buildSampleDetailFromProvider(ctx, provider, sample)

		convey.So(err, convey.ShouldBeNil)
		convey.So(detail, convey.ShouldNotBeNil)
		if detail == nil {
			return
		}

		convey.So(detail.Libraries, convey.ShouldResemble, []mlwh.Library{
			{PipelineIDLims: "Standard", IDStudyLims: "6568"},
			{PipelineIDLims: "Chromium", IDStudyLims: "6569"},
		})
	})

	convey.Convey("D3.1: study enrichment emits library_details from mlwh.StudyDetail", t, func() {
		studyDetail := &mlwh.StudyDetail{
			Study: mlwh.Study{IDStudyLims: "6568", Name: "Study 6568"},
			Libraries: []mlwh.LibraryDetail{
				{
					Library: mlwh.Library{PipelineIDLims: "Standard", IDStudyLims: "6568"},
					Samples: []mlwh.Sample{
						detailGraphSample("6568", "S1", "Sample 1", "Standard"),
						detailGraphSample("6568", "S2", "Sample 2", "Standard"),
						detailGraphSample("6568", "S3", "Sample 3", "Standard"),
					},
				},
				{
					Library: mlwh.Library{PipelineIDLims: "Bespoke", IDStudyLims: "6568"},
					Samples: []mlwh.Sample{
						detailGraphSample("6568", "S4", "Sample 4", "Bespoke"),
						detailGraphSample("6568", "S5", "Sample 5", "Bespoke"),
					},
				},
			},
		}

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				convey.So(identifier, convey.ShouldEqual, "6568")
				return &studyDetail.Study, nil
			},
			StudyDetailFunc: func(_ context.Context, studyLimsID string) (*mlwh.StudyDetail, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "6568")
				return studyDetail, nil
			},
		}

		result, err := Enrich(ctx, provider, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		payload, marshalErr := json.Marshal(result)
		convey.So(marshalErr, convey.ShouldBeNil)

		var decoded map[string]any
		convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)

		graph := decoded["graph"].(map[string]any)
		studyNode := graph["study_detail"].(map[string]any)
		libraryDetails := studyNode["library_details"].([]any)
		totalSamples := 0

		for _, detail := range libraryDetails {
			totalSamples += len(detail.(map[string]any)["samples"].([]any))
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(len(libraryDetails), convey.ShouldEqual, 2)
		convey.So(totalSamples, convey.ShouldEqual, 5)
		convey.So(studyNode["study"].(map[string]any)["id_study_lims"], convey.ShouldEqual, "6568")
	})

	convey.Convey("D3.2: sample enrichment emits lanes from mlwh.SampleDetail", t, func() {
		sampleDetail := &mlwh.SampleDetail{
			Sample: detailGraphSample("6568", "7607STDY14643771", "Sample 7607STDY14643771", "Standard"),
			Lanes:  []mlwh.Lane{{IDRun: 101, Position: 1, TagIndex: 10}, {IDRun: 101, Position: 2, TagIndex: 11}, {IDRun: 102, Position: 1, TagIndex: 12}},
		}

		provider := &MockProvider{
			FindSamplesBySangerIDFn: func(_ context.Context, sangerID string) ([]mlwh.Sample, error) {
				convey.So(sangerID, convey.ShouldEqual, "7607STDY14643771")
				return []mlwh.Sample{sampleDetail.Sample}, nil
			},
			SampleDetailFunc: func(_ context.Context, sangerName string) (*mlwh.SampleDetail, error) {
				convey.So(sangerName, convey.ShouldEqual, "Sample 7607STDY14643771")
				return sampleDetail, nil
			},
		}

		result, err := Enrich(ctx, provider, "7607STDY14643771")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		payload, marshalErr := json.Marshal(result)
		convey.So(marshalErr, convey.ShouldBeNil)

		var decoded map[string]any
		convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)

		sampleNode := decoded["graph"].(map[string]any)["sample_detail"].(map[string]any)
		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)
		convey.So(len(sampleNode["lanes"].([]any)), convey.ShouldEqual, 3)
		convey.So(sampleNode["sample"].(map[string]any)["sanger_sample_id"], convey.ShouldEqual, "7607STDY14643771")
	})

	convey.Convey("Bug 3: sample enrichment accepts the canonical sample name stored by results register", t, func() {
		sampleDetail := &mlwh.SampleDetail{
			Sample: detailGraphSample("7607", "SANGER-ALT", "7607STDY14643771", "Custom"),
			Lanes:  []mlwh.Lane{{IDRun: 48522, Position: 1, TagIndex: 1}},
		}
		provider := &MockProvider{
			ResolveSampleFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "7607STDY14643771")

				return mlwh.Match{
					Kind:      mlwh.KindSangerSampleName,
					Canonical: "7607STDY14643771",
					Sample:    &sampleDetail.Sample,
				}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return nil, mlwh.ErrNotFound
			},
			SampleDetailFunc: func(_ context.Context, sangerName string) (*mlwh.SampleDetail, error) {
				convey.So(sangerName, convey.ShouldEqual, "7607STDY14643771")

				return sampleDetail, nil
			},
		}

		result, err := Enrich(ctx, provider, "7607STDY14643771")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleName)
		convey.So(result.Graph.SampleDetail, convey.ShouldNotBeNil)
		convey.So(result.Graph.SampleDetail.Lanes, convey.ShouldHaveLength, 1)
	})

	convey.Convey("Bug 4: sample enrichment tries the fast canonical sample-name resolver before broad sample scans", t, func() {
		sampleDetail := &mlwh.SampleDetail{
			Sample: detailGraphSample("6568", "WTSI_wEMB10524782", "WTSI_wEMB10524782", "Chromium single cell ATAC"),
			Lanes:  []mlwh.Lane{{IDRun: 40121, Position: 1, TagIndex: 5}},
		}
		provider := &MockProvider{
			ResolveSampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "WTSI_wEMB10524782")

				return mlwh.Match{
					Kind:      mlwh.KindSangerSampleName,
					Canonical: "WTSI_wEMB10524782",
					Sample:    &sampleDetail.Sample,
				}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return nil, errors.New("unexpected broad sanger_sample_id scan")
			},
			ResolveSampleFunc: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, errors.New("unexpected broad ResolveSample cascade")
			},
			SampleDetailFunc: func(_ context.Context, sangerName string) (*mlwh.SampleDetail, error) {
				convey.So(sangerName, convey.ShouldEqual, "WTSI_wEMB10524782")

				return sampleDetail, nil
			},
		}

		result, err := Enrich(ctx, provider, "WTSI_wEMB10524782")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleName)
		convey.So(result.Graph.SampleDetail, convey.ShouldNotBeNil)
		convey.So(result.Graph.SampleDetail.Lanes, convey.ShouldHaveLength, 1)
	})

	convey.Convey("Bug 3: run enrichment resolves a registered run id even when no sample expansion matched", t, func() {
		provider := &MockProvider{
			FindSamplesByRunIDFn: func(_ context.Context, idRun int) ([]mlwh.Sample, error) {
				convey.So(idRun, convey.ShouldEqual, 48522)

				return nil, mlwh.ErrNotFound
			},
			ResolveRunFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "48522")

				return mlwh.Match{
					Kind:      mlwh.KindRunID,
					Canonical: "48522",
					Run:       &mlwh.Run{IDRun: 48522},
				}, nil
			},
		}

		result, err := Enrich(ctx, provider, "48522")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(result.Identifier, convey.ShouldEqual, "48522")
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldContain, MissingHop{Hop: HopSamples, Reason: ReasonNotFound, Status: http.StatusNotFound})
	})

	convey.Convey("D3.3: enrichment JSON omits legacy project and users graph keys", t, func() {
		studyDetail := &mlwh.StudyDetail{Study: mlwh.Study{IDStudyLims: "6568"}}
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*mlwh.Study, error) {
				return &studyDetail.Study, nil
			},
			StudyDetailFunc: func(_ context.Context, _ string) (*mlwh.StudyDetail, error) {
				return studyDetail, nil
			},
		}

		result, err := Enrich(ctx, provider, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		payload, marshalErr := json.Marshal(result)
		convey.So(marshalErr, convey.ShouldBeNil)

		var decoded map[string]any
		convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)

		graph := decoded["graph"].(map[string]any)
		_, hasRemovedStudyOwner := graph["project"]
		_, hasUsers := graph["users"]

		convey.So(hasRemovedStudyOwner, convey.ShouldBeFalse)
		convey.So(hasUsers, convey.ShouldBeFalse)
	})

	convey.Convey("D3.4: library enrichment truncates the per-study library hop at MaxSamplesPerHop", t, func() {
		studies := []mlwh.Study{{IDStudyLims: "6568", Name: "Study 6568"}}
		librarySamples := make([]mlwh.Sample, 0, 1500)

		for sampleNumber := range 1500 {
			librarySamples = append(librarySamples, detailGraphSample("6568", "S"+string(rune('A'+(sampleNumber%26))), "Library Sample", "RNA PolyA"))
		}

		provider := &MockProvider{
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "RNA PolyA")
				return librarySamples, nil
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				if identifier == "RNA PolyA" {
					return nil, mlwh.ErrNotFound
				}

				convey.So(identifier, convey.ShouldEqual, "6568")
				return &studies[0], nil
			},
		}

		result, err := Enrich(ctx, provider, "RNA PolyA")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldContain, MissingHop{Hop: HopLibraries, Reason: ReasonSamplesTruncated, Status: 200})
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, MaxLibrarySamples)
	})

	convey.Convey("D3.5: library enrichment builds study details from matched samples without rescanning every study", t, func() {
		provider := &MockProvider{
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "Chromium single cell 3 prime v3")

				return []mlwh.Sample{
					detailGraphSample("6568", "S1", "Sample 1", libraryType),
					detailGraphSample("6568", "S2", "Sample 2", libraryType),
					detailGraphSample("7777", "S3", "Sample 3", libraryType),
				}, nil
			},
			AllStudiesFunc: func(_ context.Context, _, _ int) ([]mlwh.Study, error) {
				return nil, errors.New("unexpected AllStudies call")
			},
			SamplesForLibraryFunc: func(_ context.Context, _, _ string, _, _ int) ([]mlwh.Sample, error) {
				return nil, errors.New("unexpected SamplesForLibrary call")
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				switch identifier {
				case "6568":
					return &mlwh.Study{IDStudyLims: "6568", Name: "Study 6568"}, nil
				case "7777":
					return &mlwh.Study{IDStudyLims: "7777", Name: "Study 7777"}, nil
				default:
					return nil, mlwh.ErrNotFound
				}
			},
		}

		result, err := Enrich(ctx, provider, "Chromium single cell 3 prime v3")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 3)
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.StudyDetails[0].Libraries, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.StudyDetails[0].Libraries[0].Library.PipelineIDLims, convey.ShouldEqual, "Chromium single cell 3 prime v3")
		convey.So(result.Graph.StudyDetails[0].Libraries[0].Samples, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.StudyDetails[1].Libraries[0].Samples, convey.ShouldHaveLength, 1)
	})

	convey.Convey("D3.6: library enrichment bulk-loads cached studies before falling back to per-study lookup", t, func() {
		db, err := sql.Open("sqlite", ":memory:")
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		_, err = db.Exec(`CREATE TABLE study_mirror (id_study_lims TEXT PRIMARY KEY, id_lims TEXT, name TEXT, accession_number TEXT)`)
		convey.So(err, convey.ShouldBeNil)
		_, err = db.Exec(`INSERT INTO study_mirror(id_study_lims, id_lims, name, accession_number) VALUES ('6568', 'SQSCP', 'Study 6568', 'EGAS00001006568'), ('7777', 'SQSCP', 'Study 7777', 'EGAS00001007777')`)
		convey.So(err, convey.ShouldBeNil)

		provider := &MockProvider{
			QueryContextFunc: db.QueryContext,
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "Chromium single cell 3 prime v3")

				return []mlwh.Sample{
					detailGraphSample("6568", "S1", "Sample 1", libraryType),
					detailGraphSample("7777", "S2", "Sample 2", libraryType),
				}, nil
			},
			GetStudyFunc: func(_ context.Context, _ string) (*mlwh.Study, error) {
				return nil, errors.New("unexpected GetStudy call")
			},
		}

		result, err := Enrich(ctx, provider, "Chromium single cell 3 prime v3")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.StudyDetails[0].Study.Name, convey.ShouldEqual, "Study 6568")
		convey.So(result.Graph.StudyDetails[1].Study.AccessionNumber, convey.ShouldEqual, "EGAS00001007777")
	})

	convey.Convey("D3.7: library enrichment returns partial stub studies for cache misses instead of calling upstream per study", t, func() {
		db, err := sql.Open("sqlite", ":memory:")
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		_, err = db.Exec(`CREATE TABLE study_mirror (id_study_lims TEXT PRIMARY KEY, id_lims TEXT, name TEXT, accession_number TEXT)`)
		convey.So(err, convey.ShouldBeNil)
		_, err = db.Exec(`INSERT INTO study_mirror(id_study_lims, id_lims, name, accession_number) VALUES ('6568', 'SQSCP', 'Study 6568', 'EGAS00001006568')`)
		convey.So(err, convey.ShouldBeNil)

		provider := &MockProvider{
			QueryContextFunc: db.QueryContext,
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
				return []mlwh.Sample{
					detailGraphSample("6568", "S1", "Sample 1", libraryType),
					detailGraphSample("7777", "S2", "Sample 2", libraryType),
				}, nil
			},
			GetStudyFunc: func(_ context.Context, _ string) (*mlwh.Study, error) {
				return nil, errors.New("unexpected GetStudy call")
			},
		}

		result, err := Enrich(ctx, provider, "Chromium single cell 3 prime v3")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldContain, MissingHop{Hop: HopStudies, Reason: ReasonNotFound, Status: http.StatusOK})
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.StudyDetails[1].Study.IDStudyLims, convey.ShouldEqual, "7777")
		convey.So(result.Graph.StudyDetails[1].Study.Name, convey.ShouldEqual, "")
	})

	convey.Convey("D3.8: library-like identifiers skip earlier study and sample classifiers", t, func() {
		db, err := sql.Open("sqlite", ":memory:")
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = db.Close() }()

		_, err = db.Exec(`CREATE TABLE study_mirror (id_study_lims TEXT PRIMARY KEY, id_lims TEXT, name TEXT, accession_number TEXT)`)
		convey.So(err, convey.ShouldBeNil)
		_, err = db.Exec(`INSERT INTO study_mirror(id_study_lims, id_lims, name, accession_number) VALUES ('6568', 'SQSCP', 'Study 6568', 'EGAS00001006568')`)
		convey.So(err, convey.ShouldBeNil)

		provider := &MockProvider{
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
				return []mlwh.Sample{detailGraphSample("6568", "S1", "Sample 1", libraryType)}, nil
			},
			QueryContextFunc: db.QueryContext,
			GetStudyFunc: func(_ context.Context, _ string) (*mlwh.Study, error) {
				return nil, errors.New("unexpected GetStudy call")
			},
		}

		result, err := Enrich(ctx, provider, "Chromium single cell 3 prime v3")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 1)
	})

	convey.Convey("Bug 4: library enrichment uses a bounded paged lookup for multi-sample library types", t, func() {
		provider := &MockProvider{
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return nil, errors.New("unexpected unique library-type lookup")
			},
			SamplesForLibraryTypeFunc: func(_ context.Context, libraryType string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "Chromium single cell 3 prime v3")
				convey.So(limit, convey.ShouldEqual, MaxLibrarySamples+1)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.Sample{
					detailGraphSample("6568", "S1", "Sample 1", libraryType),
					detailGraphSample("6568", "S2", "Sample 2", libraryType),
				}, nil
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				convey.So(identifier, convey.ShouldEqual, "6568")

				return &mlwh.Study{IDStudyLims: "6568", Name: "Study 6568"}, nil
			},
		}

		result, err := Enrich(ctx, provider, "Chromium single cell 3 prime v3")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 2)
		convey.So(result.Partial, convey.ShouldBeFalse)
	})

	convey.Convey("Bug 2: library enrichment resolves library_id values as library IDs and filters to the exact library", t, func() {
		provider := &MockProvider{
			ResolveLibraryFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "71046409")

				return mlwh.Match{
					Kind:      mlwh.KindLibraryID,
					Canonical: "71046409",
					Library: &mlwh.Library{
						PipelineIDLims: "Custom",
						IDStudyLims:    "7607",
						LibraryID:      "71046409",
						IDLibraryLims:  "SQPP-47463-G:B1",
					},
				}, nil
			},
			SamplesForLibraryIDFunc: func(_ context.Context, libraryID string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(libraryID, convey.ShouldEqual, "71046409")
				convey.So(limit, convey.ShouldEqual, MaxLibrarySamples+1)
				convey.So(offset, convey.ShouldEqual, 0)

				matching := detailGraphSample("7607", "SANGER-MATCH", "Sample Match", "Custom")
				matching.Libraries[0].LibraryID = "71046409"
				matching.Libraries[0].IDLibraryLims = "SQPP-47463-G:B1"

				return []mlwh.Sample{matching}, nil
			},
			SamplesForLibraryTypeFunc: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return nil, errors.New("unexpected broad library-type page for exact library id")
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return nil, errors.New("unexpected unique library-type lookup")
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				if identifier != "7607" {
					return nil, mlwh.ErrNotFound
				}

				return &mlwh.Study{IDStudyLims: "7607", Name: "Study 7607"}, nil
			},
			ResolveRunFunc: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
			ResolveSampleFunc: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
		}

		result, err := Enrich(ctx, provider, "71046409")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryID)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.Samples[0].Name, convey.ShouldEqual, "Sample Match")
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.StudyDetails[0].Libraries, convey.ShouldHaveLength, 1)
	})

	convey.Convey("Bug 3: library enrichment accepts one-word pipeline_id_lims values stored by results register", t, func() {
		provider := &MockProvider{
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "Custom")

				return []mlwh.Sample{
					detailGraphSample("7607", "SANGER-ALT", "7607STDY14643771", libraryType),
				}, nil
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				if identifier == "7607" {
					return &mlwh.Study{IDStudyLims: "7607", Name: "Study 7607"}, nil
				}

				return nil, mlwh.ErrNotFound
			},
		}

		result, err := Enrich(ctx, provider, "Custom")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 1)
	})

	convey.Convey("Bug 4: run metadata enrichment does not scan every study before resolving a run id", t, func() {
		allStudiesCalls := 0
		provider := &MockProvider{
			AllStudiesFunc: func(_ context.Context, _, _ int) ([]mlwh.Study, error) {
				allStudiesCalls++

				return nil, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, idRun int) ([]mlwh.Sample, error) {
				convey.So(idRun, convey.ShouldEqual, 48522)

				return []mlwh.Sample{detailGraphSample("7607", "SANGER-RUN", "Run Sample", "Custom")}, nil
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				if identifier == "7607" {
					return &mlwh.Study{IDStudyLims: "7607", Name: "Study 7607"}, nil
				}

				return nil, mlwh.ErrNotFound
			},
		}

		result, err := Enrich(ctx, provider, "48522")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(allStudiesCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("Bug 4: sample metadata enrichment does not scan every study before resolving a canonical sample name", t, func() {
		allStudiesCalls := 0
		sample := detailGraphSample("7607", "SANGER-SAMPLE", "7607STDY14643771", "Custom")
		provider := &MockProvider{
			AllStudiesFunc: func(_ context.Context, _, _ int) ([]mlwh.Study, error) {
				allStudiesCalls++

				return nil, nil
			},
			GetStudyFunc: func(_ context.Context, _ string) (*mlwh.Study, error) {
				return nil, mlwh.ErrNotFound
			},
			ResolveSampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "7607STDY14643771")

				return mlwh.Match{Kind: mlwh.KindSangerSampleName, Canonical: raw, Sample: &sample}, nil
			},
			SampleDetailFunc: func(_ context.Context, sampleName string) (*mlwh.SampleDetail, error) {
				convey.So(sampleName, convey.ShouldEqual, "7607STDY14643771")

				return &mlwh.SampleDetail{Sample: sample}, nil
			},
		}

		result, err := Enrich(ctx, provider, "7607STDY14643771")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(allStudiesCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("Bug 4: one-word library metadata enrichment does not scan every study before resolving the library type", t, func() {
		allStudiesCalls := 0
		provider := &MockProvider{
			AllStudiesFunc: func(_ context.Context, _, _ int) ([]mlwh.Study, error) {
				allStudiesCalls++

				return nil, nil
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				if identifier == "7607" {
					return &mlwh.Study{IDStudyLims: "7607", Name: "Study 7607"}, nil
				}

				return nil, mlwh.ErrNotFound
			},
			SamplesForLibraryTypeFunc: func(_ context.Context, libraryType string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "Custom")
				convey.So(limit, convey.ShouldEqual, MaxLibrarySamples+1)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.Sample{detailGraphSample("7607", "SANGER-LIB", "Library Sample", libraryType)}, nil
			},
		}

		result, err := Enrich(ctx, provider, "Custom")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(allStudiesCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("Bug 4: one-word library metadata enrichment does not run broad sample scans before the library lookup", t, func() {
		broadSampleCalls := 0
		provider := &MockProvider{
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				broadSampleCalls++

				return nil, errors.New("unexpected broad sanger sample scan")
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				broadSampleCalls++

				return nil, errors.New("unexpected broad sample lims scan")
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				broadSampleCalls++

				return nil, errors.New("unexpected broad sample accession scan")
			},
			ResolveSampleFunc: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
			SamplesForLibraryTypeFunc: func(_ context.Context, libraryType string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "Custom")
				convey.So(limit, convey.ShouldEqual, MaxLibrarySamples+1)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.Sample{detailGraphSample("7607", "SANGER-LIB", "Library Sample", libraryType)}, nil
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				if identifier == "7607" {
					return &mlwh.Study{IDStudyLims: "7607", Name: "Study 7607"}, nil
				}

				return nil, mlwh.ErrNotFound
			},
		}

		result, err := Enrich(ctx, provider, "Custom")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(broadSampleCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("Bug 4: study accession enrichment uses indexed ResolveStudy instead of scanning every study", t, func() {
		allStudiesCalls := 0
		provider := &MockProvider{
			AllStudiesFunc: func(_ context.Context, _, _ int) ([]mlwh.Study, error) {
				allStudiesCalls++

				return nil, nil
			},
			ResolveStudyFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "EGAS00001007607")

				return mlwh.Match{
					Kind:      mlwh.KindStudyAccession,
					Canonical: "7607",
					Study: &mlwh.Study{
						IDStudyLims:     "7607",
						AccessionNumber: "EGAS00001007607",
					},
				}, nil
			},
			StudyDetailFunc: func(_ context.Context, studyLimsID string) (*mlwh.StudyDetail, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "7607")

				return &mlwh.StudyDetail{Study: mlwh.Study{
					IDStudyLims:     "7607",
					AccessionNumber: "EGAS00001007607",
				}}, nil
			},
			GetStudyFunc: func(_ context.Context, _ string) (*mlwh.Study, error) {
				return nil, mlwh.ErrNotFound
			},
		}

		result, err := Enrich(ctx, provider, "EGAS00001007607")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyAccession)
		convey.So(result.Graph.Study.AccessionNumber, convey.ShouldEqual, "EGAS00001007607")
		convey.So(allStudiesCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("Bug 4: combined result metadata enrichment stays under one second on a cache-backed provider", t, func() {
		cachePath := filepath.Join(t.TempDir(), "mlwh-cache.sqlite")
		seedResultMetadataMLWHCache(t, cachePath)

		client, err := mlwh.OpenCacheOnly(ctx, mlwh.CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		defer func() { convey.So(client.Close(), convey.ShouldBeNil) }()

		provider := &cacheBackedEnrichProvider{Client: client}
		identifiers := []string{"Custom", "71046409", "48522", "7607STDY14643771", "7607"}

		started := time.Now()
		results := make([]*EnrichmentResult, 0, len(identifiers))
		for _, identifier := range identifiers {
			result, enrichErr := Enrich(ctx, provider, identifier)
			convey.So(enrichErr, convey.ShouldBeNil)
			convey.So(result, convey.ShouldNotBeNil)
			results = append(results, result)
		}

		elapsed := time.Since(started)
		t.Logf("combined cache-backed result metadata enrichment took %s", elapsed)
		convey.So(elapsed, convey.ShouldBeLessThan, time.Second)
		convey.So(results[0].Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(results[1].Type, convey.ShouldEqual, IdentifierLibraryID)
		convey.So(results[1].Graph.Samples, convey.ShouldHaveLength, 1)
		convey.So(results[2].Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(results[3].Type, convey.ShouldEqual, IdentifierSangerSampleName)
		convey.So(results[4].Type, convey.ShouldEqual, IdentifierStudyID)
	})

	convey.Convey("Bug 4: combined result metadata enrichment avoids the slow broad fallback paths", t, func() {
		study := mlwh.Study{IDStudyLims: "7607", Name: "Study 7607"}
		libraryTypeSample := detailGraphSample("7607", "SANGER-LIB", "Library Sample", "Custom")
		exactLibrarySample := detailGraphSample("7607", "SANGER-EXACT", "Exact Library Sample", "Exact Custom")
		exactLibrarySample.Libraries[0].LibraryID = "71046409"
		exactLibrarySample.Libraries[0].IDLibraryLims = "SQPP-47463-G:B1"
		runSample := detailGraphSample("7607", "SANGER-RUN", "Run Sample", "Run Custom")
		sample := detailGraphSample("7607", "SANGER-SAMPLE", "7607STDY14643771", "Sample Custom")

		broadSampleCalls := 0
		broadExactLibraryTypeCalls := 0
		exactLibraryIDCalls := 0
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				if identifier == "7607" {
					return &study, nil
				}

				return nil, mlwh.ErrNotFound
			},
			ResolveStudyFunc: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
			ResolveRunFunc: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
			ResolveSampleNameFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw == "7607STDY14643771" {
					return mlwh.Match{Kind: mlwh.KindSangerSampleName, Canonical: raw, Sample: &sample}, nil
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			ResolveLibraryFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw == "71046409" {
					return mlwh.Match{
						Kind:      mlwh.KindLibraryID,
						Canonical: "71046409",
						Library: &mlwh.Library{
							PipelineIDLims: "Exact Custom",
							IDStudyLims:    "7607",
							LibraryID:      "71046409",
							IDLibraryLims:  "SQPP-47463-G:B1",
						},
					}, nil
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			FindSamplesByRunIDFn: func(_ context.Context, idRun int) ([]mlwh.Sample, error) {
				if idRun == 48522 {
					return []mlwh.Sample{runSample}, nil
				}

				return nil, mlwh.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				broadSampleCalls++

				return nil, mlwh.ErrNotFound
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				broadSampleCalls++

				return nil, mlwh.ErrNotFound
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				broadSampleCalls++

				return nil, mlwh.ErrNotFound
			},
			ResolveSampleFunc: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
			SamplesForLibraryIDFunc: func(_ context.Context, libraryID string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(libraryID, convey.ShouldEqual, "71046409")
				convey.So(limit, convey.ShouldEqual, MaxLibrarySamples+1)
				convey.So(offset, convey.ShouldEqual, 0)
				exactLibraryIDCalls++

				return []mlwh.Sample{exactLibrarySample}, nil
			},
			SamplesForLibraryTypeFunc: func(_ context.Context, libraryType string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(limit, convey.ShouldEqual, MaxLibrarySamples+1)
				convey.So(offset, convey.ShouldEqual, 0)
				if libraryType == "Exact Custom" {
					broadExactLibraryTypeCalls++

					return []mlwh.Sample{exactLibrarySample}, nil
				}
				if libraryType == "Custom" {
					return []mlwh.Sample{libraryTypeSample}, nil
				}

				return nil, mlwh.ErrNotFound
			},
			SampleDetailFunc: func(_ context.Context, sampleName string) (*mlwh.SampleDetail, error) {
				if sampleName == "7607STDY14643771" {
					return &mlwh.SampleDetail{Sample: sample, Study: &study}, nil
				}

				return nil, mlwh.ErrNotFound
			},
			StudyDetailFunc: func(_ context.Context, studyLimsID string) (*mlwh.StudyDetail, error) {
				if studyLimsID == "7607" {
					return &mlwh.StudyDetail{Study: study}, nil
				}

				return nil, mlwh.ErrNotFound
			},
		}

		identifiers := []string{"Custom", "71046409", "48522", "7607STDY14643771", "7607"}
		started := time.Now()
		results := make([]*EnrichmentResult, 0, len(identifiers))
		for _, identifier := range identifiers {
			result, err := Enrich(ctx, provider, identifier)
			convey.So(err, convey.ShouldBeNil)
			convey.So(result, convey.ShouldNotBeNil)
			results = append(results, result)
		}

		elapsed := time.Since(started)
		t.Logf("combined fake-provider result metadata enrichment took %s", elapsed)
		convey.So(elapsed, convey.ShouldBeLessThan, time.Second)
		convey.So(results[0].Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(results[1].Type, convey.ShouldEqual, IdentifierLibraryID)
		convey.So(results[2].Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(results[3].Type, convey.ShouldEqual, IdentifierSangerSampleName)
		convey.So(results[4].Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(broadSampleCalls, convey.ShouldEqual, 0)
		convey.So(broadExactLibraryTypeCalls, convey.ShouldEqual, 0)
		convey.So(exactLibraryIDCalls, convey.ShouldEqual, 1)
	})
}

func detailGraphSample(studyID, sangerSampleID, name, libraryType string) mlwh.Sample {
	return mlwh.Sample{
		Name:           name,
		SangerSampleID: sangerSampleID,
		Studies:        []mlwh.Study{{IDStudyLims: studyID}},
		Libraries:      []mlwh.Library{{PipelineIDLims: libraryType, IDStudyLims: studyID}},
	}
}

func seedResultMetadataMLWHCache(t *testing.T, cachePath string) {
	t.Helper()

	ctx := context.Background()
	cache, err := mlwh.OpenCache(ctx, mlwh.CacheConfig{Path: cachePath})
	convey.So(err, convey.ShouldBeNil)
	defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

	db := cache.DB()
	seedResultMetadataSyncState(t, db)
	seedResultMetadataStudy(t, db)
	seedResultMetadataSample(t, db)
	seedResultMetadataLibrary(t, db)
	seedResultMetadataRun(t, db)
}

func seedResultMetadataSyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, tableName := range []string{"sample", "study", "iseq_flowcell", "iseq_product_metrics", "seq_product_irods_locations"} {
		_, err := db.Exec(
			`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, ?, ?)`,
			tableName,
			"2026-05-15T12:00:00Z",
			"2026-05-15T12:01:00Z",
			nil,
			0,
		)
		convey.So(err, convey.ShouldBeNil)
	}
}

func seedResultMetadataStudy(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		7607,
		"SQSCP",
		"7607",
		"study-uuid-7607",
		"Study 7607",
		"EGAS00001007607",
		"Study 7607 title",
		"Sponsor",
		"active",
		"standard",
		"group",
		"programme",
		"GRCh38",
		1,
		"genomic sequencing",
		1,
		0,
		"public",
		"EGAC00001000001",
		"EGAP00001000001",
		"standard",
		"2026-05-15T12:00:00Z",
	)
	convey.So(err, convey.ShouldBeNil)
}

func seedResultMetadataSample(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		31,
		"SQSCP",
		"9575305",
		"sample-uuid-31",
		"7607STDY14643771",
		"SANGER-7607",
		"supplier-31",
		"SAMEA7607",
		"donor-31",
		9606,
		"human",
		"sample 31",
		"2026-05-15T12:00:00Z",
	)
	convey.So(err, convey.ShouldBeNil)
}

func seedResultMetadataLibrary(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims, library_id, id_library_lims) VALUES (?, ?, ?, ?, ?)`,
		"Custom",
		31,
		"7607",
		"71046409",
		"SQPP-47463-G:B1",
	)
	convey.So(err, convey.ShouldBeNil)
}

func seedResultMetadataRun(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"product-31",
		31,
		48522,
		1,
		1,
		31,
		"7607",
		1,
		1,
		1,
		"2026-05-15T12:00:00Z",
	)
	convey.So(err, convey.ShouldBeNil)
}
