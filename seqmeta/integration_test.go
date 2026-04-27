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
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
)

type enrichMatrixExpectation string

const (
	enrichExpectationEndToEnd enrichMatrixExpectation = "end-to-end"
	enrichExpectationPartial  enrichMatrixExpectation = "partial"
	liveEnrichMatrixTimeout                           = 60 * time.Second
)

type enrichMatrixCase struct {
	identifier   string
	expectedType IdentifierType
	expectation  enrichMatrixExpectation
}

var liveEnrichMatrixCases = []enrichMatrixCase{
	{identifier: "5993", expectedType: IdentifierStudyID, expectation: enrichExpectationEndToEnd},
	{identifier: "5994", expectedType: IdentifierStudyID, expectation: enrichExpectationEndToEnd},
	{identifier: "6591", expectedType: IdentifierStudyID, expectation: enrichExpectationEndToEnd},
	{identifier: "5835STDY8046554", expectedType: IdentifierSangerSampleID, expectation: enrichExpectationEndToEnd},
	{identifier: "WTSI_wEMB10524782", expectedType: IdentifierSangerSampleID, expectation: enrichExpectationEndToEnd},
	{identifier: "6591STDY10735392", expectedType: IdentifierSangerSampleID, expectation: enrichExpectationEndToEnd},
	{identifier: "RNA PolyA", expectedType: IdentifierLibraryType, expectation: enrichExpectationEndToEnd},
	{identifier: "Agilent Pulldown", expectedType: IdentifierLibraryType, expectation: enrichExpectationEndToEnd},
	{identifier: "34134", expectedType: IdentifierRunID, expectation: enrichExpectationEndToEnd},
	{identifier: "40121", expectedType: IdentifierRunID, expectation: enrichExpectationEndToEnd},
}

func liveEnrichCasesForSupport(cases []enrichMatrixCase, support map[string]bool) []enrichMatrixCase {
	updated := make([]enrichMatrixCase, 0, len(cases))

	for _, testCase := range cases {
		updatedCase := testCase

		switch testCase.expectedType {
		case IdentifierSangerSampleID:
			if knownUnsupported(support, mlwhFilterSangerID) {
				updatedCase.expectation = enrichExpectationPartial
			}
		case IdentifierRunID:
			if knownUnsupported(support, mlwhFilterIDRun) {
				updatedCase.expectation = enrichExpectationPartial
			}
		case IdentifierLibraryType:
			if knownUnsupported(support, mlwhFilterLibraryType) {
				updatedCase.expectation = enrichExpectationPartial
			}
		}

		updated = append(updated, updatedCase)
	}

	return updated
}

func knownUnsupported(support map[string]bool, filterKey string) bool {
	supported, ok := support[filterKey]

	return ok && !supported
}

func assertLiveEnrichment(t *testing.T, result *EnrichmentResult, testCase enrichMatrixCase) {
	t.Helper()

	convey.So(result, convey.ShouldNotBeNil)
	if result == nil {
		return
	}

	convey.So(result.Identifier, convey.ShouldEqual, testCase.identifier)
	convey.So(result.Type, convey.ShouldEqual, testCase.expectedType)

	switch testCase.expectation {
	case enrichExpectationEndToEnd:
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeEmpty)
	case enrichExpectationPartial:
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldNotBeEmpty)
		convey.So(hasMissingReason(result.Missing, ReasonFilterUnsupported), convey.ShouldBeTrue)
	}

	if testCase.expectation == enrichExpectationPartial {
		return
	}

	switch testCase.expectedType {
	case IdentifierStudyID:
		convey.So(result.Graph.Study, convey.ShouldNotBeNil)
		convey.So(result.Graph.Samples, convey.ShouldNotBeEmpty)
		convey.So(result.Graph.Libraries, convey.ShouldNotBeEmpty)
	case IdentifierSangerSampleID:
		convey.So(result.Graph.Sample, convey.ShouldNotBeNil)
		convey.So(result.Graph.Study, convey.ShouldNotBeNil)
		convey.So(result.Graph.Library, convey.ShouldNotBeNil)
	case IdentifierLibraryType, IdentifierRunID:
		convey.So(result.Graph.Studies, convey.ShouldNotBeEmpty)
		convey.So(result.Graph.Samples, convey.ShouldNotBeEmpty)
		convey.So(result.Graph.Libraries, convey.ShouldNotBeEmpty)
	}
}

func hasMissingReason(missing []MissingHop, reason string) bool {
	for _, hop := range missing {
		if hop.Reason == reason {
			return true
		}
	}

	return false
}

func TestIntegrationEnrichMatrix(t *testing.T) {
	token := os.Getenv("SAGA_TEST_API_TOKEN")
	if token == "" {
		t.Skip("SAGA_TEST_API_TOKEN not set")
	}

	client := mustNewSeqmetaIntegrationClient(t, token)
	support := probeLiveEnrichFilterSupport(t, client)
	provider := NewClientAdapter(client, WithSupportedEnrichFilters(support))
	caseMatrix := liveEnrichCasesForSupport(liveEnrichMatrixCases, support)

	convey.Convey("Given a valid SAGA API token", t, func() {
		convey.Convey("when each G1 matrix row is fetched through GET /enrich/{identifier}, then the assertion for its tag holds", func() {
			for _, testCase := range caseMatrix {
				testCase := testCase

				convey.Convey(testCase.identifier, func() {
					status, body, result := fetchLiveEnrichment(t, provider, testCase.identifier, liveEnrichMatrixTimeout)
					logUnexpectedEnrichmentStatus(t, testCase.identifier, status, body)

					convey.So(status, convey.ShouldEqual, http.StatusOK)
					convey.So(body, convey.ShouldNotBeEmpty)
					assertLiveEnrichment(t, result, testCase)
				})
			}
		})

		convey.Convey("when each G1 matrix row uses a 20 second request timeout, then it either completes or surfaces context deadline exceeded", func() {
			for _, testCase := range caseMatrix {
				testCase := testCase

				convey.Convey(testCase.identifier, func() {
					status, body, result := fetchLiveEnrichment(t, provider, testCase.identifier, 20*time.Second)
					logUnexpectedEnrichmentStatus(t, testCase.identifier, status, body)

					if status == http.StatusOK {
						assertLiveEnrichment(t, result, testCase)

						return
					}

					convey.So(status, convey.ShouldEqual, http.StatusBadGateway)
					convey.So(string(body), convey.ShouldContainSubstring, context.DeadlineExceeded.Error())
				})
			}
		})
	})
}

func mustNewSeqmetaIntegrationClient(t *testing.T, token string) *saga.Client {
	t.Helper()

	client, err := saga.NewClient(token)
	if err != nil {
		t.Fatalf("create integration client: %v", err)
	}

	t.Cleanup(client.Close)

	return client
}

func probeLiveEnrichFilterSupport(t *testing.T, client *saga.Client) map[string]bool {
	t.Helper()

	ctx := context.Background()
	support := map[string]bool{
		mlwhFilterSangerID:    true,
		mlwhFilterIDRun:       true,
		mlwhFilterLibraryType: true,
	}

	if _, err := client.MLWH().FindSamplesBySangerID(ctx, "WTSI_wEMB10524782"); err != nil {
		support[mlwhFilterSangerID] = mustInterpretFilterProbe(t, mlwhFilterSangerID, err)
	}

	if _, err := client.MLWH().FindSamplesByRunID(ctx, 34134); err != nil {
		support[mlwhFilterIDRun] = mustInterpretFilterProbe(t, mlwhFilterIDRun, err)
	}

	if _, err := client.MLWH().FindSamplesByLibraryType(ctx, "RNA PolyA"); err != nil {
		support[mlwhFilterLibraryType] = mustInterpretFilterProbe(t, mlwhFilterLibraryType, err)
	}

	return support
}

func mustInterpretFilterProbe(t *testing.T, filterKey string, err error) bool {
	t.Helper()

	if errors.Is(err, saga.ErrServerError) {
		t.Logf("MLWH filter %s appears unsupported upstream: %v", filterKey, err)

		return false
	}

	t.Fatalf("MLWH filter probe for %s failed: %v", filterKey, err)

	return false
}

func fetchLiveEnrichment(t *testing.T, provider SAGAProvider, identifier string, timeout time.Duration) (int, []byte, *EnrichmentResult) {
	t.Helper()

	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("open integration store: %v", err)
	}
	defer func() { _ = store.Close() }()

	server := NewServer(provider, store)
	ctx := context.Background()

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	request := httptest.NewRequest(http.MethodGet, "/enrich/"+url.PathEscape(identifier), nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	body := recorder.Body.Bytes()
	if recorder.Code != http.StatusOK {
		return recorder.Code, body, nil
	}

	var result EnrichmentResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode enrichment response for %q: %v", identifier, err)
	}

	return recorder.Code, body, &result
}

func logUnexpectedEnrichmentStatus(t *testing.T, identifier string, status int, body []byte) {
	t.Helper()

	if status == http.StatusOK {
		return
	}

	t.Logf("GET /enrich/%s returned %d: %s", identifier, status, string(body))
}
