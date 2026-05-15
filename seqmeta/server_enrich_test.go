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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestServerEnrichEndpoint(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	study := &mlwh.Study{IDStudyLims: "6568", Name: "Study 6568"}
	samples := []mlwh.Sample{
		serverEnrichSample("6568", "S1", "Sample 1", "RNA PolyA"),
		serverEnrichSample("6568", "S2", "Sample 2", "RNA PolyA"),
		serverEnrichSample("6568", "S3", "Sample 3", "PCR free"),
	}
	received := ""

	provider := &MockProvider{
		GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
			received = identifier
			if identifier == "6568" || identifier == "foo/bar" {
				return &mlwh.Study{IDStudyLims: identifier, Name: "Study " + identifier}, nil
			}
			return nil, mlwh.ErrNotFound
		},
		AllSamplesForStudyFunc: func(_ context.Context, studyID string) ([]mlwh.Sample, error) {
			if studyID == "6568" {
				return samples, nil
			}
			return []mlwh.Sample{}, nil
		},
	}
	server := NewServer(provider, store)

	convey.Convey("enrich endpoint returns a study graph envelope", t, func() {
		provider.GetStudyFunc = func(_ context.Context, identifier string) (*mlwh.Study, error) {
			received = identifier
			if identifier == "6568" {
				return study, nil
			}
			if identifier == "foo/bar" {
				return &mlwh.Study{IDStudyLims: identifier}, nil
			}
			return nil, mlwh.ErrNotFound
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(result.Graph.Study, convey.ShouldResemble, study)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 3)
		convey.So(result.Graph.Libraries, convey.ShouldHaveLength, 2)
		convey.So(result.Partial, convey.ShouldBeFalse)
	})

	convey.Convey("enrich endpoint passes URL-decoded identifiers to Enrich", t, func() {
		request := httptest.NewRequest(http.MethodGet, "/enrich/foo%2Fbar", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(received, convey.ShouldEqual, "foo/bar")
	})

	convey.Convey("enrich endpoint returns partial results when a downstream hop fails", t, func() {
		convey.So(store.DeleteEnrichCache("6568"), convey.ShouldBeNil)

		provider.GetStudyFunc = func(_ context.Context, _ string) (*mlwh.Study, error) {
			return study, nil
		}
		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]mlwh.Sample, error) {
			return nil, context.DeadlineExceeded
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/6568", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var body map[string]string
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &body), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusBadGateway)
		convey.So(body["error"], convey.ShouldContainSubstring, context.DeadlineExceeded.Error())
	})

	convey.Convey("enrich endpoint ignores stale negative cache entries for library-type identifiers", t, func() {
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "RNA PolyA",
			Body:       []byte(`{"error":"seqmeta: unknown identifier"}`),
			FetchedAt:  time.Now(),
			TTL:        time.Hour,
			Negative:   true,
		}), convey.ShouldBeNil)

		provider.FindSamplesByLibraryTypeFn = func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
			convey.So(libraryType, convey.ShouldEqual, "RNA PolyA")

			return []mlwh.Sample{
				serverEnrichSample("6568", "S1", "Sample 1", libraryType),
			}, nil
		}
		provider.GetStudyFunc = func(_ context.Context, identifier string) (*mlwh.Study, error) {
			if identifier != "6568" {
				return nil, mlwh.ErrNotFound
			}

			return study, nil
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/RNA%20PolyA", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Graph.Studies, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 1)
	})

	convey.Convey("enrich endpoint ignores stale negative cache entries for one-word library identifiers", t, func() {
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "Custom",
			Body:       []byte(`{"error":"seqmeta: unknown identifier"}`),
			FetchedAt:  time.Now(),
			TTL:        time.Hour,
			Negative:   true,
		}), convey.ShouldBeNil)

		provider.FindSamplesByLibraryTypeFn = func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
			convey.So(libraryType, convey.ShouldEqual, "Custom")

			return []mlwh.Sample{
				serverEnrichSample("7607", "SANGER-ALT", "7607STDY14643771", libraryType),
			}, nil
		}
		provider.GetStudyFunc = func(_ context.Context, identifier string) (*mlwh.Study, error) {
			if identifier != "7607" {
				return nil, mlwh.ErrNotFound
			}

			return &mlwh.Study{IDStudyLims: "7607", Name: "Study 7607"}, nil
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/Custom", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 1)
	})

	convey.Convey("enrich endpoint ignores stale negative cache entries for run IDs", t, func() {
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "48522",
			Body:       []byte(`{"error":"seqmeta: unknown identifier"}`),
			FetchedAt:  time.Now(),
			TTL:        time.Hour,
			Negative:   true,
		}), convey.ShouldBeNil)

		provider.FindSamplesByRunIDFn = func(_ context.Context, idRun int) ([]mlwh.Sample, error) {
			convey.So(idRun, convey.ShouldEqual, 48522)

			return nil, mlwh.ErrNotFound
		}
		provider.ResolveRunFunc = func(_ context.Context, raw string) (mlwh.Match, error) {
			convey.So(raw, convey.ShouldEqual, "48522")

			return mlwh.Match{
				Kind:      mlwh.KindRunID,
				Canonical: "48522",
				Run:       &mlwh.Run{IDRun: 48522},
			}, nil
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/48522", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(result.Partial, convey.ShouldBeTrue)
	})

	convey.Convey("enrich endpoint ignores stale negative cache entries for canonical sample names", t, func() {
		sample := serverEnrichSample("7607", "SANGER-ALT", "7607STDY14643771", "Custom")
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "7607STDY14643771",
			Body:       []byte(`{"error":"seqmeta: unknown identifier"}`),
			FetchedAt:  time.Now(),
			TTL:        time.Hour,
			Negative:   true,
		}), convey.ShouldBeNil)

		provider.FindSamplesBySangerIDFn = func(_ context.Context, sangerID string) ([]mlwh.Sample, error) {
			convey.So(sangerID, convey.ShouldEqual, "7607STDY14643771")

			return nil, mlwh.ErrNotFound
		}
		provider.ResolveSampleFunc = func(_ context.Context, raw string) (mlwh.Match, error) {
			convey.So(raw, convey.ShouldEqual, "7607STDY14643771")

			return mlwh.Match{
				Kind:      mlwh.KindSangerSampleName,
				Canonical: "7607STDY14643771",
				Sample:    &sample,
			}, nil
		}
		provider.SampleDetailFunc = func(_ context.Context, sampleName string) (*mlwh.SampleDetail, error) {
			convey.So(sampleName, convey.ShouldEqual, "7607STDY14643771")

			return &mlwh.SampleDetail{Sample: sample}, nil
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/7607STDY14643771", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleName)
		convey.So(result.Graph.SampleDetail, convey.ShouldNotBeNil)
	})

	convey.Convey("enrich endpoint refreshes legacy study cache entries that only grouped library types", t, func() {
		convey.So(insertLegacyEnrichCacheForTest(store, enrichCacheEntry{
			Identifier: "7607",
			Type:       IdentifierStudyID,
			Body: []byte(`{
				"identifier":"7607",
				"type":"study_lims_id",
				"graph":{
					"study":{"id_study_tmp":0,"id_lims":"SQSCP","id_study_lims":"7607","name":"Study 7607","faculty_sponsor":"","state":"","accession_number":"","data_release_strategy":"","study_title":"","data_access_group":"","programme":"","reference_genome":"","ethically_approved":false,"study_type":"","contains_human_dna":false,"contaminated_human_dna":false,"study_visibility":"","ega_dac_accession_number":"","ega_policy_accession_number":"","data_release_timing":""},
					"libraries":[{"library_type":"Custom","id_study_lims":"7607"}]
				},
				"partial":false
			}`),
			FetchedAt: time.Now(),
			TTL:       time.Hour,
		}), convey.ShouldBeNil)

		study7607 := &mlwh.Study{IDStudyLims: "7607", Name: "Study 7607"}
		provider.GetStudyFunc = func(_ context.Context, identifier string) (*mlwh.Study, error) {
			convey.So(identifier, convey.ShouldEqual, "7607")

			return study7607, nil
		}
		provider.AllSamplesForStudyFunc = func(_ context.Context, studyID string) ([]mlwh.Sample, error) {
			convey.So(studyID, convey.ShouldEqual, "7607")

			return []mlwh.Sample{
				serverEnrichSampleWithLibrary("7607", "S1", "7607STDY14643771", "Custom", "71046409", "SQPP-47463-G:B1"),
				serverEnrichSampleWithLibrary("7607", "S2", "7607STDY14643772", "Custom", "71046410", "SQPP-47464-G:C1"),
			}, nil
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/7607", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Graph.Libraries, convey.ShouldResemble, []Library{
			{
				LibraryType:   "Custom",
				IDStudyLims:   "7607",
				LibraryID:     "71046409",
				IDLibraryLims: "SQPP-47463-G:B1",
			},
			{
				LibraryType:   "Custom",
				IDStudyLims:   "7607",
				LibraryID:     "71046410",
				IDLibraryLims: "SQPP-47464-G:C1",
			},
		})
	})

	convey.Convey("enrich endpoint refreshes legacy sample cache entries that only exposed library type", t, func() {
		convey.So(insertLegacyEnrichCacheForTest(store, enrichCacheEntry{
			Identifier: "7607STDY14643771",
			Type:       IdentifierSangerSampleName,
			Body: []byte(`{
				"identifier":"7607STDY14643771",
				"type":"sanger_sample_name",
				"graph":{
					"sample":{"id_study_lims":"7607","id_sample_lims":"SMP001","sanger_id":"7607STDY14643771","sample_name":"7607STDY14643771","taxon_id":0,"common_name":"","library_type":"Custom","accession_number":""},
					"library":{"library_type":"Custom","id_study_lims":"7607"}
				},
				"partial":false
			}`),
			FetchedAt: time.Now(),
			TTL:       time.Hour,
		}), convey.ShouldBeNil)

		sample := serverEnrichSampleWithLibrary("7607", "7607STDY14643771", "7607STDY14643771", "Custom", "71046409", "SQPP-47463-G:B1")
		provider.GetStudyFunc = func(_ context.Context, _ string) (*mlwh.Study, error) {
			return nil, mlwh.ErrNotFound
		}
		provider.AllSamplesForStudyFunc = nil
		provider.ResolveSampleNameFunc = func(_ context.Context, raw string) (mlwh.Match, error) {
			convey.So(raw, convey.ShouldEqual, "7607STDY14643771")

			return mlwh.Match{
				Kind:      mlwh.KindSangerSampleName,
				Canonical: "7607STDY14643771",
				Sample:    &sample,
			}, nil
		}
		provider.SampleDetailFunc = func(_ context.Context, sampleName string) (*mlwh.SampleDetail, error) {
			convey.So(sampleName, convey.ShouldEqual, "7607STDY14643771")

			return &mlwh.SampleDetail{
				Sample:    sample,
				Libraries: sample.Libraries,
			}, nil
		}

		request := httptest.NewRequest(http.MethodGet, "/enrich/7607STDY14643771", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		var result EnrichmentResult
		convey.So(json.Unmarshal(recorder.Body.Bytes(), &result), convey.ShouldBeNil)
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(result.Graph.Library, convey.ShouldResemble, &Library{
			LibraryType:   "Custom",
			IDStudyLims:   "7607",
			LibraryID:     "71046409",
			IDLibraryLims: "SQPP-47463-G:B1",
		})
	})

	convey.Convey("enrich endpoint does not store negative cache entries for unmatched one-word identifiers", t, func() {
		convey.So(store.DeleteEnrichCache("UnmatchedIdentifier"), convey.ShouldBeNil)
		provider.GetStudyFunc = nil
		provider.AllSamplesForStudyFunc = nil
		provider.FindSamplesBySangerIDFn = nil
		provider.FindSamplesByRunIDFn = nil
		provider.ResolveSampleNameFunc = nil
		provider.ResolveRunFunc = nil
		provider.ResolveSampleFunc = nil
		provider.SampleDetailFunc = nil
		provider.FindSamplesByLibraryTypeFn = nil

		request := httptest.NewRequest(http.MethodGet, "/enrich/UnmatchedIdentifier", nil)
		recorder := httptest.NewRecorder()

		server.Handler().ServeHTTP(recorder, request)

		entry, loadErr := store.LoadEnrichCache("UnmatchedIdentifier")
		convey.So(recorder.Code, convey.ShouldEqual, http.StatusNotFound)
		convey.So(loadErr, convey.ShouldEqual, sql.ErrNoRows)
		convey.So(entry, convey.ShouldBeNil)
	})
}

func serverEnrichSample(studyID, sangerSampleID, name, libraryType string) mlwh.Sample {
	return serverEnrichSampleWithLibrary(studyID, sangerSampleID, name, libraryType, "", "")
}

func insertLegacyEnrichCacheForTest(store *Store, entry enrichCacheEntry) error {
	_, err := store.db.Exec(`
		INSERT OR REPLACE INTO enrich_cache(identifier, type, body, fetched_at, ttl_seconds, negative, partial)
		VALUES(?, ?, ?, ?, ?, ?, ?)
	`, entry.Identifier, entry.Type, entry.Body, entry.FetchedAt.UTC().Format(time.RFC3339Nano), int64(entry.TTL), boolInt(entry.Negative), boolInt(entry.Partial))

	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func serverEnrichSampleWithLibrary(studyID, sangerSampleID, name, libraryType, libraryID, idLibraryLims string) mlwh.Sample {
	return mlwh.Sample{
		Name:           name,
		SangerSampleID: sangerSampleID,
		Studies:        []mlwh.Study{{IDStudyLims: studyID}},
		Libraries: []mlwh.Library{{
			PipelineIDLims: libraryType,
			IDStudyLims:    studyID,
			LibraryID:      libraryID,
			IDLibraryLims:  idLibraryLims,
		}},
	}
}
