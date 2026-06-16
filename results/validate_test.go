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

package results

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"

	"github.com/wtsi-hgi/wa/mlwh"
)

func TestSeqmetaMetadataValueKeys(t *testing.T) {
	convey.Convey("seqmetaMetadataValueKeys returns sorted seqmeta metadata keys from the metadata map", t, func() {
		keys := seqmetaMetadataValueKeys(map[string][]string{
			"sample":         {"SAMPLE-1"},
			"seqmeta_runid":  {"48522"},
			"seqmeta_study":  {"6568"},
			"workflow_label": {"manual"},
		})

		convey.So(keys, convey.ShouldResemble, []string{"seqmeta_runid", "seqmeta_study"})
	})
}

func TestValidateRegistrationAcceptsFileSymlinkPathsUnderOutputDirectory(t *testing.T) {
	convey.Convey("ValidateRegistration accepts output file symlinks whose registered paths are under the output directory", t, func() {
		outputDir := t.TempDir()
		externalDir := t.TempDir()
		externalFile := filepath.Join(externalDir, "external.txt")
		linkPath := filepath.Join(outputDir, "cnmf", "k_10", "params.yaml")

		convey.So(os.WriteFile(externalFile, []byte("data"), 0o644), convey.ShouldBeNil)
		convey.So(os.MkdirAll(filepath.Dir(linkPath), 0o755), convey.ShouldBeNil)
		convey.So(os.Symlink(externalFile, linkPath), convey.ShouldBeNil)

		reg := &Registration{
			PipelineIdentifier: "pipe",
			RunKey:             "runid=48522",
			Requester:          "alice",
			PipelineName:       "nf-pipe",
			PipelineVersion:    "1.2.3",
			OutputDirectory:    outputDir,
			Files: []FileEntry{{
				Path:  linkPath,
				Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC),
				Size:  4,
				Kind:  "output",
			}},
		}

		err := ValidateRegistration(reg)

		convey.So(err, convey.ShouldBeNil)
	})
}

func TestValidateRegistrationRejectsDirectorySymlinkEscapes(t *testing.T) {
	convey.Convey("ValidateRegistration rejects output files reached via a directory symlink that exits the output directory", t, func() {
		outputDir := t.TempDir()
		externalDir := t.TempDir()
		externalFile := filepath.Join(externalDir, "external.txt")

		convey.So(os.WriteFile(externalFile, []byte("data"), 0o644), convey.ShouldBeNil)
		convey.So(os.Symlink(externalDir, filepath.Join(outputDir, "escape")), convey.ShouldBeNil)

		reg := &Registration{
			PipelineIdentifier: "pipe",
			RunKey:             "runid=48522",
			Requester:          "alice",
			PipelineName:       "nf-pipe",
			PipelineVersion:    "1.2.3",
			OutputDirectory:    outputDir,
			Files: []FileEntry{{
				Path:  filepath.Join(outputDir, "escape", "external.txt"),
				Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC),
				Size:  4,
				Kind:  "output",
			}},
		}

		err := ValidateRegistration(reg)

		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "outside output directory")
	})
}

type mlwhValidationResponseForTest struct {
	match mlwh.Match
	err   error
}

func TestMLWHValidatorValidateMetadataValues(t *testing.T) {
	convey.Convey("A1.1: ValidateMetadataValues accepts matching MLWH identifier types without an HTTP client field", t, func() {
		queryer := &mlwhValidationQueryerForTest{
			responses: map[string]mlwhValidationResponseForTest{
				"6568": {match: mlwh.Match{Kind: mlwh.KindStudyLimsID}},
			},
		}
		validator := NewMLWHValidator(queryer)

		err := validator.ValidateMetadataValues(context.Background(), map[string][]string{
			"seqmeta_id_study_lims": {"6568"},
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(queryer.calls, convey.ShouldResemble, []string{"6568"})
		convey.So(httpClientFieldCount(validator), convey.ShouldEqual, 0)
	})

	convey.Convey("A1.2: ValidateMetadataValues rejects mismatched MLWH identifier types", t, func() {
		queryer := &mlwhValidationQueryerForTest{
			responses: map[string]mlwhValidationResponseForTest{
				"X": {match: mlwh.Match{Kind: mlwh.KindSangerSampleName}},
			},
		}
		validator := NewMLWHValidator(queryer)

		err := validator.ValidateMetadataValues(context.Background(), map[string][]string{
			"seqmeta_id_study_lims": {"X"},
		})

		convey.So(errors.Is(err, ErrMLWHRejected), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, `expected "study_lims_id", got "sanger_sample_name"`)
	})

	convey.Convey("A1.3: ValidateMetadataValues treats MLWH not found as rejected", t, func() {
		queryer := &mlwhValidationQueryerForTest{
			responses: map[string]mlwhValidationResponseForTest{
				"6568": {err: mlwh.ErrNotFound},
			},
		}
		validator := NewMLWHValidator(queryer)

		err := validator.ValidateMetadataValues(context.Background(), map[string][]string{
			"seqmeta_id_study_lims": {"6568"},
		})

		convey.So(errors.Is(err, ErrMLWHRejected), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "identifier not found")
	})

	convey.Convey("A1.4: ValidateMetadataValues treats MLWH upstream and unsynced cache errors as failed", t, func() {
		for _, queryErr := range []error{mlwh.ErrUpstreamImpaired, mlwh.ErrCacheNeverSynced} {
			queryer := &mlwhValidationQueryerForTest{
				responses: map[string]mlwhValidationResponseForTest{
					"6568": {err: queryErr},
				},
			}
			validator := NewMLWHValidator(queryer)

			err := validator.ValidateMetadataValues(context.Background(), map[string][]string{
				"seqmeta_id_study_lims": {"6568"},
			})

			convey.So(errors.Is(err, ErrMLWHFailed), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrMLWHRejected), convey.ShouldBeFalse)
		}
	})

	convey.Convey("A1.5: ValidateMetadataValues rejects unknown seqmeta metadata suffixes before querying MLWH", t, func() {
		queryer := &mlwhValidationQueryerForTest{}
		validator := NewMLWHValidator(queryer)

		err := validator.ValidateMetadataValues(context.Background(), map[string][]string{
			"seqmeta_unknown": {"val"},
		})

		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, `unknown seqmeta field "seqmeta_unknown"`)
		convey.So(queryer.calls, convey.ShouldBeEmpty)
	})

	convey.Convey("A1.6: ValidateMetadataValues skips validation for a nil validator", t, func() {
		var validator *MLWHValidator

		err := validator.ValidateMetadataValues(context.Background(), map[string][]string{
			"seqmeta_id_study_lims": {"6568"},
		})

		convey.So(err, convey.ShouldBeNil)
	})
}

func httpClientFieldCount(validator *MLWHValidator) int {
	if validator == nil {
		return 0
	}

	validatorType := reflect.TypeOf(*validator)
	httpClientType := reflect.TypeOf((*http.Client)(nil))
	count := 0

	for i := range validatorType.NumField() {
		if validatorType.Field(i).Type == httpClientType {
			count++
		}
	}

	return count
}

type mlwhValidationQueryerForTest struct {
	mlwh.Queryer
	calls     []string
	responses map[string]mlwhValidationResponseForTest
}

func (q *mlwhValidationQueryerForTest) ClassifyIdentifier(
	_ context.Context,
	raw string,
) (mlwh.Match, error) {
	q.calls = append(q.calls, raw)

	response, ok := q.responses[raw]
	if !ok {
		return mlwh.Match{}, mlwh.ErrNotFound
	}

	return response.match, response.err
}
