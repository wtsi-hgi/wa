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
	"fmt"
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

type integrationSkipReporterStub struct {
	skipped bool
	message string
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

func (p *integrationSkipReporterStub) Helper() {}

func (p *integrationSkipReporterStub) Skip(args ...any) {
	p.skipped = true
	p.message = fmt.Sprint(args...)
}

func sampleLinkedMatrixCases() []enrichMatrixCase {
	cases := make([]enrichMatrixCase, 0, len(liveEnrichMatrixCases))

	for _, testCase := range liveEnrichMatrixCases {
		if testCase.expectedType == IdentifierSangerSampleID {
			cases = append(cases, testCase)
		}
	}

	return cases
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

func TestRequireIntegrationToken(t *testing.T) {
	convey.Convey("Given the live seqmeta integration token helper", t, func() {
		convey.Convey("when neither token variable is present, then the integration test is skipped cleanly", func() {
			t.Setenv("SAGA_API_TOKEN", "")
			t.Setenv("SAGA_TEST_API_TOKEN", "")
			reporter := &integrationSkipReporterStub{}

			token := requireIntegrationToken(reporter)

			convey.So(token, convey.ShouldBeBlank)
			convey.So(reporter.skipped, convey.ShouldBeTrue)
			convey.So(reporter.message, convey.ShouldEqual, "SAGA_API_TOKEN or SAGA_TEST_API_TOKEN not set")
		})

		convey.Convey("when SAGA_API_TOKEN is present, then it is preferred without skipping", func() {
			t.Setenv("SAGA_API_TOKEN", " primary-token ")
			t.Setenv("SAGA_TEST_API_TOKEN", "secondary-token")
			reporter := &integrationSkipReporterStub{}

			token := requireIntegrationToken(reporter)

			convey.So(token, convey.ShouldEqual, " primary-token ")
			convey.So(reporter.skipped, convey.ShouldBeFalse)
		})
	})
}

func TestIntegrationEnrichMatrix(t *testing.T) {
	token := requireIntegrationToken(t)

	client := mustNewSeqmetaIntegrationClient(t, token)
	provider := NewClientAdapter(client)
	caseMatrix := liveEnrichMatrixCases

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

func TestIntegrationEnrichSampleLinkedUnsupportedIdentifiersFailFast(t *testing.T) {
	token := requireIntegrationToken(t)

	client := mustNewSeqmetaIntegrationClient(t, token)
	provider := NewClientAdapter(client)
	sampleCases := sampleLinkedMatrixCases()

	convey.Convey("Given live sample identifier enrichments", t, func() {
		for _, testCase := range sampleCases {
			testCase := testCase

			convey.Convey(testCase.identifier, func() {
				status, body, baseResult := fetchLiveEnrichment(t, provider, testCase.identifier, liveEnrichMatrixTimeout)
				logUnexpectedEnrichmentStatus(t, testCase.identifier, status, body)

				convey.So(status, convey.ShouldEqual, http.StatusOK)
				assertLiveEnrichment(t, baseResult, testCase)
				convey.So(baseResult, convey.ShouldNotBeNil)
				if baseResult == nil {
					return
				}

				convey.So(baseResult.Graph.Sample, convey.ShouldNotBeNil)
				if baseResult.Graph.Sample == nil {
					return
				}

				sample := baseResult.Graph.Sample

				if sample.IDSampleLims != "" {
					convey.Convey("sample_lims_id", func() {
						status, body, alternateResult := fetchLiveEnrichment(t, provider, sample.IDSampleLims, 10*time.Second)
						logUnexpectedEnrichmentStatus(t, sample.IDSampleLims, status, body)

						convey.So(status, convey.ShouldEqual, http.StatusNotFound)
						convey.So(string(body), convey.ShouldContainSubstring, ErrUnknownIdentifier.Error())
						convey.So(alternateResult, convey.ShouldBeNil)
					})
				}

				if sample.AccessionNumber != "" {
					convey.Convey("sample_accession", func() {
						status, body, alternateResult := fetchLiveEnrichment(t, provider, sample.AccessionNumber, 10*time.Second)
						logUnexpectedEnrichmentStatus(t, sample.AccessionNumber, status, body)

						convey.So(status, convey.ShouldEqual, http.StatusNotFound)
						convey.So(string(body), convey.ShouldContainSubstring, ErrUnknownIdentifier.Error())
						convey.So(alternateResult, convey.ShouldBeNil)
					})
				}
			})
		}
	})
}

func integrationTokenForTest() string {
	if token := os.Getenv("SAGA_API_TOKEN"); token != "" {
		return token
	}

	return os.Getenv("SAGA_TEST_API_TOKEN")
}

type integrationSkipReporter interface {
	Helper()
	Skip(args ...any)
}

func requireIntegrationToken(reporter integrationSkipReporter) string {
	reporter.Helper()

	token := integrationTokenForTest()
	if token == "" {
		reporter.Skip("SAGA_API_TOKEN or SAGA_TEST_API_TOKEN not set")
	}

	return token
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
