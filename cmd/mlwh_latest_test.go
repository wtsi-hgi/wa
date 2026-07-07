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
	"path/filepath"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

type stubMLWHLatestClient struct {
	latestStudy func(ctx context.Context, studyLimsID, fileType string, limit, offset int) ([]mlwh.RecentDataRow, error)
	closed      bool
}

func (s *stubMLWHLatestClient) LatestDataForStudy(ctx context.Context, studyLimsID, fileType string, limit, offset int) ([]mlwh.RecentDataRow, error) {
	if s.latestStudy != nil {
		return s.latestStudy(ctx, studyLimsID, fileType, limit, offset)
	}

	return []mlwh.RecentDataRow{}, nil
}

func (s *stubMLWHLatestClient) LatestDataForFacultySponsor(context.Context, string, string, int, int) ([]mlwh.RecentDataRow, error) {
	return []mlwh.RecentDataRow{}, nil
}

func (s *stubMLWHLatestClient) Close() error {
	s.closed = true

	return nil
}

// E2 acceptance test 3: wa mlwh latest <study> --file-type cram prints the newest
// CRAM rows and exits successfully.
func TestMLWHLatestPrintsNewestCRAMRowsE2(t *testing.T) {
	convey.Convey("Given a fake latest-data client returning newest CRAM rows for study 5901", t, func() {
		var capturedStudy, capturedFileType string
		var capturedLimit, capturedOffset int
		stub := &stubMLWHLatestClient{
			latestStudy: func(_ context.Context, studyLimsID, fileType string, limit, offset int) ([]mlwh.RecentDataRow, error) {
				capturedStudy = studyLimsID
				capturedFileType = fileType
				capturedLimit = limit
				capturedOffset = offset

				return []mlwh.RecentDataRow{
					{
						Created:      "2026-07-04T10:00:00Z",
						IRODSPath:    "/seq/61002/9102.cram",
						IDStudyLims:  "5901",
						StudyName:    "Study 5901",
						Name:         "anderson-sample-201",
						SupplierName: "supplier-201",
						IDRun:        61002,
						Position:     1,
						TagIndex:     2,
						Platform:     "illumina",
					},
					{
						Created:      "2026-07-03T10:00:00Z",
						IRODSPath:    "/seq/61001/9101.cram",
						IDStudyLims:  "5901",
						StudyName:    "Study 5901",
						Name:         "anderson-sample-202",
						SupplierName: "supplier-202",
						IDRun:        61001,
						Position:     1,
						TagIndex:     1,
						Platform:     "illumina",
					},
				}, nil
			},
		}
		withStubMLWHLatestClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "latest", "5901", "--file-type", "cram"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedStudy, convey.ShouldEqual, "5901")
		convey.So(capturedFileType, convey.ShouldEqual, "cram")
		convey.So(capturedLimit, convey.ShouldEqual, 10)
		convey.So(capturedOffset, convey.ShouldEqual, 0)
		convey.So(stub.closed, convey.ShouldBeTrue)
		convey.So(output, convey.ShouldContainSubstring, "Latest data:")
		convey.So(output, convey.ShouldContainSubstring, "anderson-sample-201")
		convey.So(output, convey.ShouldContainSubstring, "/seq/61002/9102.cram")
		convey.So(output, convey.ShouldContainSubstring, "created=2026-07-04T10:00:00Z")
		convey.So(strings.Index(output, "2026-07-04T10:00:00Z"), convey.ShouldBeLessThan, strings.Index(output, "2026-07-03T10:00:00Z"))
	})
}

func withStubMLWHLatestClient(t *testing.T, stub *stubMLWHLatestClient) {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHLatestClient
	t.Cleanup(func() { openMLWHLatestClient = original })

	openMLWHLatestClient = func(context.Context, mlwh.Config) (mlwhLatestClient, error) {
		return stub, nil
	}
}

func TestMLWHLatestInvalidFileTypeBeatsLocalNeverSyncedCacheK(t *testing.T) {
	convey.Convey("Given local cache-only mode with a never-synced cache, when latest gets an invalid --file-type, then it errors before opening the cache", t, func() {
		t.Setenv("WA_MLWH_DSN", "")
		t.Setenv("WA_MLWH_PASSWORD", "")
		t.Setenv("WA_MLWH_CACHE_PATH", filepath.Join(t.TempDir(), "mlwh-cache.sqlite"))
		t.Setenv("WA_MLWH_CACHE_PASSWORD", "")
		t.Setenv("WA_MLWH_SERVER_URL", "")
		t.Setenv("WA_MLWH_BACKEND_URL", "")
		t.Setenv("WA_ENV", "")
		t.Setenv("WA_TEST_SEQMETA_PORT", "")
		t.Setenv("WA_DEV_SEQMETA_PORT", "")
		t.Setenv("WA_PROD_SEQMETA_PORT", "")

		opened := false
		original := openMLWHLatestClient
		t.Cleanup(func() { openMLWHLatestClient = original })
		openMLWHLatestClient = func(context.Context, mlwh.Config) (mlwhLatestClient, error) {
			opened = true

			return &stubMLWHLatestClient{
				latestStudy: func(context.Context, string, string, int, int) ([]mlwh.RecentDataRow, error) {
					return nil, mlwh.ErrCacheNeverSynced
				},
			}, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "latest", "5901", "--file-type", "bad/type"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(opened, convey.ShouldBeFalse)
		convey.So(output, convey.ShouldContainSubstring, "invalid --file-type")
		convey.So(output, convey.ShouldContainSubstring, "bad/type")
		convey.So(output, convey.ShouldNotContainSubstring, mlwhCacheUnavailableMessage)
	})
}
