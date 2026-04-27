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

package saga

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

type filterProbeReporter interface {
	Helper()
	Logf(format string, args ...any)
	Fatalf(format string, args ...any)
}

func assertSupportedFilterResultWithReporter(reporter filterProbeReporter, filterKey string, err error) {
	reporter.Helper()

	if err == nil {
		return
	}

	if errors.Is(err, ErrServerError) {
		reporter.Logf("MLWH filter %s appears unsupported upstream: %v", filterKey, err)
	}

	reporter.Fatalf("MLWH filter probe for %s failed: %v", filterKey, err)
}

type probeReporterStub struct {
	logs     []string
	failures []string
}

func (p *probeReporterStub) Helper() {}

func (p *probeReporterStub) Logf(format string, args ...any) {
	p.logs = append(p.logs, fmt.Sprintf(format, args...))
}

func (p *probeReporterStub) Fatalf(format string, args ...any) {
	p.failures = append(p.failures, fmt.Sprintf(format, args...))
}

func TestAssertSupportedFilterResult(t *testing.T) {
	Convey("Given the MLWH filter probe failure handler", t, func() {
		Convey("when the upstream probe fails with ErrServerError, then it logs the unsupported-filter hint and still fails the probe", func() {
			reporter := &probeReporterStub{}

			assertSupportedFilterResultWithReporter(reporter, "library_type", ErrServerError)

			So(reporter.logs, ShouldHaveLength, 1)
			So(reporter.logs[0], ShouldContainSubstring, "MLWH filter library_type appears unsupported upstream")
			So(reporter.failures, ShouldHaveLength, 1)
			So(reporter.failures[0], ShouldContainSubstring, "MLWH filter probe for library_type failed")
		})
	})
}

func mustHarvestSeedStudySample(t *testing.T, ctx context.Context, client *Client) MLWHSample {
	t.Helper()

	samples, err := client.MLWH().AllSamplesForStudy(ctx, "3361")
	assertSupportedFilterResult(t, "study_id", err)

	for _, sample := range samples {
		if sample.IDSampleLims != "" && sample.AccessionNumber != "" {
			return sample
		}
	}

	t.Fatalf("study 3361 did not yield a sample with id_sample_lims and accession_number populated")

	return MLWHSample{}
}

func assertSupportedFilterResult(t *testing.T, filterKey string, err error) {
	t.Helper()
	assertSupportedFilterResultWithReporter(t, filterKey, err)
	if err != nil {
		t.FailNow()
	}
}

func TestIntegration(t *testing.T) {
	token := os.Getenv("SAGA_TEST_API_TOKEN")
	if token == "" {
		t.Skip("SAGA_TEST_API_TOKEN not set")
	}

	Convey("Given a valid SAGA API token", t, func() {
		ctx := context.Background()

		Convey("when Ping is called, then no error is returned", func() {
			client := mustNewIntegrationClient(t, token)

			err := client.Ping(ctx)

			So(err, ShouldBeNil)
		})

		Convey("when Version is called, then a non-empty revision string is returned", func() {
			client := mustNewIntegrationClient(t, token)

			version, err := client.Version(ctx)

			So(err, ShouldBeNil)
			So(version, ShouldNotBeNil)
			So(version.Rev, ShouldNotBeNil)
			So(*version.Rev, ShouldNotBeBlank)
		})

		Convey("when MLWH GetStudy is called for study 6568, then the study name matches a known HCA embryo study", func() {
			client := mustNewIntegrationClient(t, token)

			study, err := client.MLWH().GetStudy(ctx, "6568")

			So(err, ShouldBeNil)
			So(study, ShouldNotBeNil)
			So(study.IDStudyLims, ShouldEqual, "6568")
			So(study.Name, ShouldNotBeBlank)
			So(studyNameLooksKnown(study.Name), ShouldBeTrue)
		})

		Convey("when IRODS GetSampleFiles is called for sample WTSI_wEMB10524782, then at least one file with a collection is returned", func() {
			client := mustNewIntegrationClient(t, token)

			files, err := client.IRODS().GetSampleFiles(ctx, "WTSI_wEMB10524782")

			So(err, ShouldBeNil)
			So(files, ShouldNotBeEmpty)
			So(files[0].Collection, ShouldNotBeBlank)
		})

		Convey("when SampleAllMetadata is called for sample WTSI_wEMB10524782, then AVUs are returned", func() {
			client := mustNewIntegrationClient(t, token)

			metadata, err := client.SampleAllMetadata(ctx, "WTSI_wEMB10524782")

			So(err, ShouldBeNil)
			So(metadata, ShouldNotBeNil)
			So(metadata.SangerID, ShouldEqual, "WTSI_wEMB10524782")
			So(metadata.AVUs, ShouldNotBeNil)
			So(len(metadata.AVUs), ShouldBeGreaterThan, 0)
		})

		Convey("when StudyAllSamples is called for study 3361, then at least one sample is returned", func() {
			client := mustNewIntegrationClient(t, token)

			studySamples, err := client.StudyAllSamples(ctx, "3361")

			So(err, ShouldBeNil)
			So(studySamples, ShouldNotBeNil)
			So(studySamples.StudyID, ShouldEqual, "3361")
			So(studySamples.Samples, ShouldNotBeEmpty)
		})

		Convey("when StudyIRODSFiles is called for study 6568, then at least one file is returned", func() {
			client := mustNewIntegrationClient(t, token)

			studyFiles, err := client.StudyIRODSFiles(ctx, "6568", nil)

			So(err, ShouldBeNil)
			So(studyFiles, ShouldNotBeNil)
			So(studyFiles.StudyID, ShouldEqual, "6568")
			So(studyFiles.Files, ShouldNotBeEmpty)
		})

		Convey("when SampleIRODSFiles is called for sample WTSI_wEMB10524782 without a filter, then at least one file is returned", func() {
			client := mustNewIntegrationClient(t, token)

			sampleFiles, err := client.SampleIRODSFiles(ctx, "WTSI_wEMB10524782", nil)

			So(err, ShouldBeNil)
			So(sampleFiles, ShouldNotBeNil)
			So(sampleFiles.SangerID, ShouldEqual, "WTSI_wEMB10524782")
			So(sampleFiles.Files, ShouldNotBeEmpty)
		})

		Convey("when Samples ListStudies is called, then at least one study is returned", func() {
			client := mustNewIntegrationClient(t, token)

			studies, err := client.Samples().ListStudies(ctx)

			So(err, ShouldBeNil)
			So(studies, ShouldNotBeEmpty)
		})
	})
}

func mustNewIntegrationClient(t *testing.T, token string) *Client {
	t.Helper()

	client, err := NewClient(token)
	if err != nil {
		t.Fatalf("create integration client: %v", err)
	}

	t.Cleanup(client.Close)

	return client
}

func studyNameLooksKnown(name string) bool {
	lowerName := strings.ToLower(name)

	return strings.Contains(lowerName, "hca") || strings.Contains(lowerName, "embryo")
}

func TestFilterProbes(t *testing.T) {
	token := os.Getenv("SAGA_TEST_API_TOKEN")
	if token == "" {
		t.Skip("SAGA_TEST_API_TOKEN not set")
	}

	Convey("Given a valid SAGA API token", t, func() {
		ctx := context.Background()
		client := mustNewIntegrationClient(t, token)

		Convey("when FindSamplesBySangerID is called for a known sample, then it returns MLWH samples", func() {
			samples, err := client.MLWH().FindSamplesBySangerID(ctx, "WTSI_wEMB10524782")

			assertSupportedFilterResult(t, "sanger_id", err)
			assertSampleResultShape(samples)
		})

		Convey("when FindSamplesByIDSampleLims is called for a sample harvested from study 3361, then it returns MLWH samples", func() {
			seed := mustHarvestSeedStudySample(t, ctx, client)

			samples, err := client.MLWH().FindSamplesByIDSampleLims(ctx, seed.IDSampleLims)

			assertSupportedFilterResult(t, "id_sample_lims", err)
			assertSampleResultShape(samples)
		})

		Convey("when FindSamplesByRunID is called for a known run, then it returns MLWH samples", func() {
			samples, err := client.MLWH().FindSamplesByRunID(ctx, 34134)

			assertSupportedFilterResult(t, "id_run", err)
			assertSampleResultShape(samples)
		})

		Convey("when FindSamplesByLibraryType is called for a known library type, then it returns MLWH samples", func() {
			samples, err := client.MLWH().FindSamplesByLibraryType(ctx, "RNA PolyA")

			assertSupportedFilterResult(t, "library_type", err)
			assertSampleResultShape(samples)
		})

		Convey("when FindSamplesByAccessionNumber is called for a sample harvested from study 3361, then it returns MLWH samples", func() {
			seed := mustHarvestSeedStudySample(t, ctx, client)

			samples, err := client.MLWH().FindSamplesByAccessionNumber(ctx, seed.AccessionNumber)

			assertSupportedFilterResult(t, "accession_number", err)
			assertSampleResultShape(samples)
		})
	})
}

func assertSampleResultShape(samples []MLWHSample) {
	So(samples, ShouldNotBeEmpty)
	So(samples[0].IDStudyLims, ShouldNotBeBlank)
	So(samples[0].IDSampleLims, ShouldNotBeBlank)
	So(samples[0].SangerID, ShouldNotBeBlank)
}
