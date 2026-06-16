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
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestSampleDetailIncludesLanesFromWarmCache(t *testing.T) {
	convey.Convey("Given a sample with three lanes", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		const sangerName = "7607STDY14643771"
		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 21, "6568", sangerName)
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 3001, 21, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 3002, 21, 100, 2, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 3003, 21, 101, 1, 5, "6568")
		seedSyncState(t, client.cache.DB(), syncTableSeqProductIRODSLocations, time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC))

		detail, err := client.SampleDetail(context.Background(), sangerName)

		convey.So(err, convey.ShouldBeNil)
		convey.So(detail.Sample.Name, convey.ShouldEqual, sangerName)
		convey.So(detail.Lanes, convey.ShouldHaveLength, 3)
	})
}

func TestStudyDetailMissingStudyReturnsErrNotFound(t *testing.T) {
	convey.Convey("Given a synced cache without the requested study", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()
		seedSyncState(t, client.cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 19, 0, 0, 0, time.UTC))

		detail, err := client.StudyDetail(context.Background(), "9999")

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
		convey.So(detail, convey.ShouldResemble, StudyDetail{})
	})
}

func TestDetailMethodsNeverSyncedReturnErrCacheNeverSynced(t *testing.T) {
	convey.Convey("Given a never-synced cache", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		cases := []struct {
			name string
			call func(context.Context) error
		}{
			{name: "SampleDetail", call: func(ctx context.Context) error { _, err := client.SampleDetail(ctx, "S1"); return err }},
			{name: "StudyDetail", call: func(ctx context.Context) error { _, err := client.StudyDetail(ctx, "6568"); return err }},
			{name: "RunDetail", call: func(ctx context.Context) error { _, err := client.RunDetail(ctx, "100"); return err }},
			{name: "LibraryDetail", call: func(ctx context.Context) error { _, err := client.LibraryDetail(ctx, "Standard", "6568"); return err }},
		}

		for _, tc := range cases {
			err := tc.call(context.Background())
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		}
	})
}

func TestRunDetailIncludesSamplesAndStudiesFromWarmCache(t *testing.T) {
	convey.Convey("Given run 100 with samples on two studies", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchyStudy(t, client.cache.DB(), 2, "7777")
		seedHierarchySample(t, client.cache.DB(), 21, "6568", "S1")
		seedHierarchySample(t, client.cache.DB(), 22, "7777", "S2")
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "6568")
		seedLibrarySample(t, client.cache.DB(), "Bespoke", 22, "7777")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6101, 21, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 6102, 22, 100, 2, 0, "7777")

		detail, err := client.RunDetail(context.Background(), "100")

		convey.So(err, convey.ShouldBeNil)
		convey.So(detail.Run.IDRun, convey.ShouldEqual, 100)
		convey.So(len(detail.Samples), convey.ShouldBeGreaterThanOrEqualTo, 1)
		convey.So(len(detail.Studies), convey.ShouldBeGreaterThanOrEqualTo, 1)
		convey.So(detail.StudyDetails, convey.ShouldHaveLength, 2)
	})
}

func TestEnrichSamplePreservesSampleDetailAndOmitsLegacyGraphKeys(t *testing.T) {
	convey.Convey("Given sample 7607STDY14643771 with three lanes", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		const sangerName = "7607STDY14643771"
		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 21, "6568", sangerName)
		seedLibrarySample(t, client.cache.DB(), "Standard", 21, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 8001, 21, 100, 1, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 8002, 21, 100, 2, 0, "6568")
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 8003, 21, 101, 1, 5, "6568")
		seedSyncState(t, client.cache.DB(), syncTableSeqProductIRODSLocations, time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC))

		result, err := client.Enrich(context.Background(), sangerName)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Graph.SampleDetail, convey.ShouldNotBeNil)
		if result.Graph.SampleDetail == nil {
			return
		}
		convey.So(result.Graph.SampleDetail.Lanes, convey.ShouldHaveLength, 3)

		payload, marshalErr := json.Marshal(result)
		convey.So(marshalErr, convey.ShouldBeNil)

		var decoded map[string]any
		convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)
		graph := decoded["graph"].(map[string]any)
		_, hasProject := graph["project"]
		_, hasUsers := graph["users"]
		convey.So(hasProject, convey.ShouldBeFalse)
		convey.So(hasUsers, convey.ShouldBeFalse)
	})
}

func TestEnrichUnknownIdentifierReturnsErrNotFound(t *testing.T) {
	convey.Convey("Given a synced cache without the requested identifier", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()
		syncedAt := time.Date(2026, time.May, 14, 11, 0, 0, 0, time.UTC)
		seedSyncState(t, client.cache.DB(), syncTableStudy, syncedAt)
		seedSyncState(t, client.cache.DB(), syncTableSample, syncedAt)
		seedSyncState(t, client.cache.DB(), syncTableIseqFlowcell, syncedAt)
		seedSyncState(t, client.cache.DB(), syncTableIseqProductMetrics, syncedAt)

		result, err := client.Enrich(context.Background(), "unknown-identifier")

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(result, convey.ShouldResemble, EnrichmentResult{})
	})
}

func TestEnrichJSONGraphPreservesLibraryLinkContract(t *testing.T) {
	convey.Convey("Given a library enrichment result with library identifiers", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		seedHierarchySample(t, client.cache.DB(), 21, "6568", "library-contract-sample")
		_, seedErr := client.cache.DB().Exec(
			`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims, library_id, id_library_lims) VALUES (?, ?, ?, ?, ?)`,
			"Custom",
			21,
			"6568",
			"71046409",
			"SQPP-47463-G:B1",
		)
		convey.So(seedErr, convey.ShouldBeNil)

		result, err := client.Enrich(context.Background(), "Custom")

		convey.So(err, convey.ShouldBeNil)
		payload, marshalErr := json.Marshal(result)
		convey.So(marshalErr, convey.ShouldBeNil)

		var decoded map[string]any
		convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)
		graph := decoded["graph"].(map[string]any)
		libraries := graph["libraries"].([]any)
		library := libraries[0].(map[string]any)
		_, hasPipelineIDLims := library["pipeline_id_lims"]

		convey.So(library["library_type"], convey.ShouldEqual, "Custom")
		convey.So(library["id_study_lims"], convey.ShouldEqual, "6568")
		convey.So(library["library_id"], convey.ShouldEqual, "71046409")
		convey.So(library["id_library_lims"], convey.ShouldEqual, "SQPP-47463-G:B1")
		convey.So(hasPipelineIDLims, convey.ShouldBeFalse)
	})
}

func TestEnrichJSONGraphMatchesFrontendEnrichmentFixtures(t *testing.T) {
	convey.Convey("Given frontend fixture-shaped study, sample, run, and library identifiers", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		const (
			studyID     = "7607"
			libraryID   = "71046409"
			libraryLims = "SQPP-47463-G:B1"
			libraryType = "Custom"
			runID       = 48522
			sampleName  = "7607STDY14643771"
		)
		seedHierarchyStudy(t, client.cache.DB(), 1, studyID)
		seedHierarchySample(t, client.cache.DB(), 31, studyID, sampleName)
		seedHierarchySample(t, client.cache.DB(), 32, studyID, "7607STDY14643772")
		seedLibraryWithIdentifiers(t, client, libraryType, 31, studyID, libraryID, libraryLims)
		seedLibraryWithIdentifiers(t, client, libraryType, 32, studyID, libraryID, libraryLims)
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 9001, 31, runID, 1, 0, studyID)
		seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 9002, 32, runID, 1, 1, studyID)
		seedSyncState(t, client.cache.DB(), syncTableSeqProductIRODSLocations, time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC))

		ctx := context.Background()
		studyResult, studyErr := client.Enrich(ctx, studyID)
		sampleResult, sampleErr := client.Enrich(ctx, sampleName)
		runResult, runErr := client.Enrich(ctx, formatInt(runID))
		libraryResult, libraryErr := client.Enrich(ctx, libraryType)

		convey.So(studyErr, convey.ShouldBeNil)
		convey.So(sampleErr, convey.ShouldBeNil)
		convey.So(runErr, convey.ShouldBeNil)
		convey.So(libraryErr, convey.ShouldBeNil)

		studyGraph := enrichmentJSONGraph(t, studyResult)
		assertFrontendGraphKeys(studyGraph, "study", "studies", "samples", "libraries", "study_detail")
		studyDetail := jsonObjectField(studyGraph, "study_detail")
		studyLibraryDetails := jsonArrayField(studyDetail, "library_details")
		studyLibrary := jsonObjectField(studyLibraryDetails[0].(map[string]any), "library")
		convey.So(studyLibrary["pipeline_id_lims"], convey.ShouldEqual, libraryType)
		convey.So(studyLibrary["id_study_lims"], convey.ShouldEqual, studyID)
		convey.So(studyLibrary["library_id"], convey.ShouldEqual, libraryID)
		convey.So(studyLibrary["id_library_lims"], convey.ShouldEqual, libraryLims)

		sampleGraph := enrichmentJSONGraph(t, sampleResult)
		assertFrontendGraphKeys(sampleGraph, "study", "sample", "samples", "library", "sample_detail")
		sampleDetail := jsonObjectField(sampleGraph, "sample_detail")
		sampleDetailSample := jsonObjectField(sampleDetail, "sample")
		convey.So(sampleDetailSample["name"], convey.ShouldEqual, sampleName)
		convey.So(jsonArrayField(sampleDetail, "lanes"), convey.ShouldHaveLength, 1)
		convey.So(jsonArrayField(sampleDetail, "libraries"), convey.ShouldHaveLength, 1)

		runGraph := enrichmentJSONGraph(t, runResult)
		assertFrontendGraphKeys(runGraph, "studies", "samples", "libraries", "study_details")
		runStudyDetails := jsonArrayField(runGraph, "study_details")
		convey.So(runStudyDetails, convey.ShouldHaveLength, 1)
		convey.So(jsonArrayField(runStudyDetails[0].(map[string]any), "library_details"), convey.ShouldHaveLength, 1)

		libraryGraph := enrichmentJSONGraph(t, libraryResult)
		assertFrontendGraphKeys(libraryGraph, "studies", "samples", "libraries", "study_details")
		libraryLinks := jsonArrayField(libraryGraph, "libraries")
		libraryLink := libraryLinks[0].(map[string]any)
		convey.So(libraryLink["library_type"], convey.ShouldEqual, libraryType)
		convey.So(libraryLink["id_study_lims"], convey.ShouldEqual, studyID)
		convey.So(libraryLink["library_id"], convey.ShouldEqual, libraryID)
		convey.So(libraryLink["id_library_lims"], convey.ShouldEqual, libraryLims)
		convey.So(jsonArrayField(libraryGraph, "study_details"), convey.ShouldHaveLength, 1)
	})
}

func seedLibraryWithIdentifiers(
	t *testing.T,
	client *Client,
	pipelineIDLims string,
	idSampleTmp int64,
	idStudyLims string,
	libraryID string,
	idLibraryLims string,
) {
	t.Helper()

	_, err := client.cache.DB().Exec(
		`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims, library_id, id_library_lims) VALUES (?, ?, ?, ?, ?)`,
		pipelineIDLims,
		idSampleTmp,
		idStudyLims,
		libraryID,
		idLibraryLims,
	)
	convey.So(err, convey.ShouldBeNil)
}

func enrichmentJSONGraph(t *testing.T, result EnrichmentResult) map[string]any {
	t.Helper()

	payload, err := json.Marshal(result)
	convey.So(err, convey.ShouldBeNil)

	var decoded map[string]any
	convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)

	graph, ok := decoded["graph"].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return graph
}

func assertFrontendGraphKeys(graph map[string]any, expectedKeys ...string) {
	for _, removedKey := range []string{"project", "users"} {
		_, exists := graph[removedKey]
		convey.So(exists, convey.ShouldBeFalse)
	}

	for _, key := range expectedKeys {
		_, exists := graph[key]
		convey.So(exists, convey.ShouldBeTrue)
	}
	convey.So(graph, convey.ShouldHaveLength, len(expectedKeys))
}

func jsonObjectField(parent map[string]any, key string) map[string]any {
	value, ok := parent[key].(map[string]any)
	convey.So(ok, convey.ShouldBeTrue)

	return value
}

func jsonArrayField(parent map[string]any, key string) []any {
	value, ok := parent[key].([]any)
	convey.So(ok, convey.ShouldBeTrue)

	return value
}

func TestEnrichComposesPromotedDetailMethods(t *testing.T) {
	convey.Convey("Given the C2 Enrich composition contract", t, func() {
		calls, err := selectorCallsByFunction("enrich.go")

		convey.So(err, convey.ShouldBeNil)
		convey.So(calls["enrichStudy"]["StudyDetail"], convey.ShouldBeTrue)
		convey.So(calls["buildSampleEnrichment"]["SampleDetail"], convey.ShouldBeTrue)
		convey.So(calls["classifyRunID"]["RunDetail"], convey.ShouldBeTrue)
		convey.So(calls["libraryStudyDetails"]["LibraryDetail"], convey.ShouldBeTrue)
	})
}

func selectorCallsByFunction(path string) (map[string]map[string]bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil, err
	}

	calls := make(map[string]map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		fnCalls := make(map[string]bool)
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			fnCalls[selector.Sel.Name] = true

			return true
		})
		calls[fn.Name.Name] = fnCalls
	}

	return calls, nil
}

func TestStudyDetailGroupsLibrariesFromWarmCache(t *testing.T) {
	convey.Convey("Given a warm cache where study 6568 has Standard and Bespoke libraries", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		for sampleID := range 10 {
			resolvedID := int64(sampleID + 1)
			seedHierarchySample(t, client.cache.DB(), resolvedID, "6568", "sample-standard-"+formatInt(resolvedID))
			seedLibrarySample(t, client.cache.DB(), "Standard", resolvedID, "6568")
		}
		for sampleID := range 3 {
			resolvedID := int64(sampleID + 11)
			seedHierarchySample(t, client.cache.DB(), resolvedID, "6568", "sample-bespoke-"+formatInt(resolvedID))
			seedLibrarySample(t, client.cache.DB(), "Bespoke", resolvedID, "6568")
		}
		seedLibrarySample(t, client.cache.DB(), "Standard", 1, "6569")

		detail, err := client.StudyDetail(context.Background(), "6568")
		libraryDetail, libraryErr := client.LibraryDetail(context.Background(), "Standard", "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(detail.Study.IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(detail.Libraries, convey.ShouldHaveLength, 2)
		convey.So(totalStudyDetailSamples(detail), convey.ShouldEqual, 13)
		convey.So(libraryErr, convey.ShouldBeNil)
		convey.So(libraryDetail.Library, convey.ShouldResemble, Library{PipelineIDLims: "Standard", IDStudyLims: "6568"})
		convey.So(libraryDetail.Samples, convey.ShouldHaveLength, 10)
	})
}

func TestEnrichStudyPreservesStudyDetailContract(t *testing.T) {
	convey.Convey("Given study 6568 with two libraries, five samples, and two runs", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		for sampleID := range 3 {
			resolvedID := int64(sampleID + 1)
			seedHierarchySample(t, client.cache.DB(), resolvedID, "6568", "study-standard-"+formatInt(resolvedID))
			seedLibrarySample(t, client.cache.DB(), "Standard", resolvedID, "6568")
			seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 7000+resolvedID, resolvedID, 100, int(resolvedID), 0, "6568")
		}
		for sampleID := range 2 {
			resolvedID := int64(sampleID + 4)
			seedHierarchySample(t, client.cache.DB(), resolvedID, "6568", "study-bespoke-"+formatInt(resolvedID))
			seedLibrarySample(t, client.cache.DB(), "Bespoke", resolvedID, "6568")
			seedIseqProductMetricsMirrorRow(t, client.cache.DB(), 7000+resolvedID, resolvedID, 101, int(resolvedID), 0, "6568")
		}

		result, err := client.Enrich(context.Background(), "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Graph.StudyDetail, convey.ShouldNotBeNil)
		if result.Graph.StudyDetail == nil {
			return
		}
		convey.So(result.Graph.StudyDetail.Study.IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(result.Graph.StudyDetail.Libraries, convey.ShouldHaveLength, 2)
		convey.So(totalStudyDetailSamples(*result.Graph.StudyDetail), convey.ShouldEqual, 5)
		convey.So(result.Partial, convey.ShouldBeFalse)
	})
}

func totalStudyDetailSamples(detail StudyDetail) int {
	total := 0
	for _, library := range detail.Libraries {
		total += len(library.Samples)
	}

	return total
}

func TestEnrichLibraryTruncatesSamplesPerHop(t *testing.T) {
	convey.Convey("Given a library hop returning more than MaxSamplesPerHop rows", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		seedHierarchyStudy(t, client.cache.DB(), 1, "6568")
		for sampleID := range MaxSamplesPerHop + 500 {
			resolvedID := int64(sampleID + 1)
			seedHierarchySample(t, client.cache.DB(), resolvedID, "6568", "rna-polya-"+formatInt(resolvedID))
			seedLibrarySample(t, client.cache.DB(), "RNA PolyA", resolvedID, "6568")
		}

		result, err := client.Enrich(context.Background(), "RNA PolyA")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, MaxSamplesPerHop)
		convey.So(countMissingReason(result.Missing, ReasonSamplesTruncated), convey.ShouldEqual, 1)
	})
}

func countMissingReason(missing []MissingHop, reason string) int {
	count := 0
	for _, hop := range missing {
		if hop.Reason == reason {
			count++
		}
	}

	return count
}
