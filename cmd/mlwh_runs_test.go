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
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

type stubMLWHRunsClient struct {
	monthly func(context.Context, mlwh.RunAggregationOptions) ([]mlwh.MonthlyRunCount, error)
	agg     func(context.Context, mlwh.SequencingAggregateOptions) ([]mlwh.SequencingAggregateRow, error)
	list    func(context.Context, mlwh.RunAggregationOptions, int, string) ([]mlwh.RunListingRow, error)
	count   func(context.Context, mlwh.RunAggregationOptions) (mlwh.Count, error)
	closed  bool
}

func (s *stubMLWHRunsClient) MonthlyRunCounts(ctx context.Context, opts mlwh.RunAggregationOptions) ([]mlwh.MonthlyRunCount, error) {
	if s.monthly != nil {
		return s.monthly(ctx, opts)
	}

	return []mlwh.MonthlyRunCount{}, nil
}

func (s *stubMLWHRunsClient) SequencingAggregate(ctx context.Context, opts mlwh.SequencingAggregateOptions) ([]mlwh.SequencingAggregateRow, error) {
	if s.agg != nil {
		return s.agg(ctx, opts)
	}

	return []mlwh.SequencingAggregateRow{}, nil
}

func (s *stubMLWHRunsClient) RunListing(ctx context.Context, opts mlwh.RunAggregationOptions, limit int, cursor string) ([]mlwh.RunListingRow, error) {
	if s.list != nil {
		return s.list(ctx, opts, limit, cursor)
	}

	return []mlwh.RunListingRow{}, nil
}

func (s *stubMLWHRunsClient) CountRunListing(ctx context.Context, opts mlwh.RunAggregationOptions) (mlwh.Count, error) {
	if s.count != nil {
		return s.count(ctx, opts)
	}

	return mlwh.Count{}, nil
}

func (s *stubMLWHRunsClient) Close() error {
	s.closed = true

	return nil
}

func TestMLWHRunsMonthlyPrintsGroupedCountsF1(t *testing.T) {
	convey.Convey("Given a fake runs client returning monthly grouped run counts", t, func() {
		var captured mlwh.RunAggregationOptions
		stub := &stubMLWHRunsClient{
			monthly: func(_ context.Context, opts mlwh.RunAggregationOptions) ([]mlwh.MonthlyRunCount, error) {
				captured = opts

				return []mlwh.MonthlyRunCount{
					{
						Month:         "2023-12",
						Manufacturer:  "PacBio",
						Platform:      "PacBio",
						Count:         1,
						DateBasis:     "run_complete",
						CacheSyncedAt: "2026-07-01T08:00:00Z",
					},
					{
						Month:         "2024-01",
						Manufacturer:  "Oxford Nanopore",
						Platform:      "ONT",
						Count:         2,
						DateBasis:     "warehouse load time - not a true sequencing date",
						CacheSyncedAt: "2026-07-01T08:00:00Z",
					},
				}, nil
			},
		}
		withStubMLWHRunsClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{
			"mlwh", "runs", "--monthly",
			"--since", "2023-12-01",
			"--until", "2024-02-01",
			"--platform", "PacBio",
			"--platform", "ONT",
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(captured.Since, convey.ShouldEqual, "2023-12-01")
		convey.So(captured.Until, convey.ShouldEqual, "2024-02-01")
		convey.So(captured.Platforms, convey.ShouldResemble, []string{"PacBio", "ONT"})
		convey.So(stub.closed, convey.ShouldBeTrue)
		convey.So(output, convey.ShouldContainSubstring, "Monthly runs:")
		convey.So(output, convey.ShouldContainSubstring, "month=2023-12 manufacturer=PacBio platform=PacBio count=1 date_basis=run_complete cache_synced_at=2026-07-01T08:00:00Z")
		convey.So(output, convey.ShouldContainSubstring, "month=2024-01 manufacturer=Oxford Nanopore platform=ONT count=2 date_basis=warehouse load time - not a true sequencing date cache_synced_at=2026-07-01T08:00:00Z")
		convey.So(strings.Index(output, "month=2023-12"), convey.ShouldBeLessThan, strings.Index(output, "month=2024-01"))
	})
}

func TestMLWHRunsMonthlyGroupByProgrammeUsesSequencingAggregateG2(t *testing.T) {
	convey.Convey("Given a fake runs client returning a programme-grouped sequencing aggregate", t, func() {
		var captured mlwh.SequencingAggregateOptions
		stub := &stubMLWHRunsClient{
			agg: func(_ context.Context, opts mlwh.SequencingAggregateOptions) ([]mlwh.SequencingAggregateRow, error) {
				captured = opts

				return []mlwh.SequencingAggregateRow{{
					Group:         map[string]string{"month": "2026-02", "programme": "Cancer"},
					Unit:          "runs",
					Count:         2,
					DateBasis:     "run_complete",
					CacheSyncedAt: "2026-07-02T08:00:00Z",
				}}, nil
			},
		}
		withStubMLWHRunsClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{
			"mlwh", "runs", "--monthly",
			"--group-by", "programme",
			"--since", "2026-02-01",
			"--until", "2026-03-01",
			"--platform", "PacBio",
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(captured.GroupBy, convey.ShouldResemble, []string{"month", "programme"})
		convey.So(captured.Unit, convey.ShouldEqual, "runs")
		convey.So(captured.Since, convey.ShouldEqual, "2026-02-01")
		convey.So(captured.Until, convey.ShouldEqual, "2026-03-01")
		convey.So(captured.Platforms, convey.ShouldResemble, []string{"PacBio"})
		convey.So(stub.closed, convey.ShouldBeTrue)
		convey.So(output, convey.ShouldContainSubstring, "Sequencing aggregate:")
		convey.So(output, convey.ShouldContainSubstring, "month=2026-02 programme=Cancer unit=runs count=2 date_basis=run_complete cache_synced_at=2026-07-02T08:00:00Z")
	})
}

func TestMLWHRunsFlatListingPagesAllByCompositeIDF2(t *testing.T) {
	convey.Convey("Given a fake runs client with three global run listing rows split over two keyset pages", t, func() {
		var (
			captured mlwh.RunAggregationOptions
			calls    []string
		)
		stub := &stubMLWHRunsClient{
			list: func(_ context.Context, opts mlwh.RunAggregationOptions, limit int, cursor string) ([]mlwh.RunListingRow, error) {
				captured = opts
				calls = append(calls, cursor)
				convey.So(limit, convey.ShouldEqual, 2)
				switch cursor {
				case "":
					return []mlwh.RunListingRow{
						{
							ID:            "illumina:52553",
							Platform:      "Illumina",
							NativeID:      "52553",
							Manufacturer:  "Illumina",
							RunDate:       "2023-12-15",
							DateBasis:     "run complete",
							CacheSyncedAt: "2026-07-01T08:00:00Z",
						},
						{
							ID:            "ont:ONTRUN-11",
							Platform:      "ONT",
							NativeID:      "ONTRUN-11",
							Manufacturer:  "Oxford Nanopore",
							RunDate:       "2024-01-11",
							DateBasis:     "warehouse load time - not a true sequencing date",
							CacheSyncedAt: "2026-07-01T08:00:00Z",
						},
					}, nil
				case "ont:ONTRUN-11":
					return []mlwh.RunListingRow{{
						ID:            "pacbio:TRACTION-RUN-1000",
						Platform:      "PacBio",
						NativeID:      "TRACTION-RUN-1000",
						Manufacturer:  "PacBio",
						RunDate:       "2023-12-20",
						DateBasis:     "run_complete",
						CacheSyncedAt: "2026-07-01T08:00:00Z",
					}}, nil
				default:
					return []mlwh.RunListingRow{}, nil
				}
			},
			count: func(_ context.Context, opts mlwh.RunAggregationOptions) (mlwh.Count, error) {
				captured = opts

				return mlwh.Count{Count: 3}, nil
			},
		}
		withStubMLWHRunsClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{
			"mlwh", "runs", "--all", "--limit", "2",
			"--since", "2023-12-01",
			"--until", "2024-02-01",
			"--platform", "Illumina",
			"--platform", "ONT",
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(captured.Since, convey.ShouldEqual, "2023-12-01")
		convey.So(captured.Until, convey.ShouldEqual, "2024-02-01")
		convey.So(captured.Platforms, convey.ShouldResemble, []string{"Illumina", "ONT"})
		convey.So(calls, convey.ShouldResemble, []string{"", "ont:ONTRUN-11"})
		convey.So(stub.closed, convey.ShouldBeTrue)
		convey.So(output, convey.ShouldContainSubstring, "Runs:")
		convey.So(output, convey.ShouldContainSubstring, "id=illumina:52553 platform=Illumina native_id=52553 manufacturer=Illumina run_date=2023-12-15 date_basis=run complete cache_synced_at=2026-07-01T08:00:00Z")
		convey.So(output, convey.ShouldContainSubstring, "id=ont:ONTRUN-11 platform=ONT native_id=ONTRUN-11 manufacturer=Oxford Nanopore run_date=2024-01-11 date_basis=warehouse load time - not a true sequencing date cache_synced_at=2026-07-01T08:00:00Z")
		convey.So(output, convey.ShouldContainSubstring, "id=pacbio:TRACTION-RUN-1000 platform=PacBio native_id=TRACTION-RUN-1000 manufacturer=PacBio run_date=2023-12-20 date_basis=run_complete cache_synced_at=2026-07-01T08:00:00Z")
		convey.So(output, convey.ShouldContainSubstring, "complete set emitted: rows=3 total=3")
		convey.So(strings.Index(output, "id=illumina:52553"), convey.ShouldBeLessThan, strings.Index(output, "id=pacbio:TRACTION-RUN-1000"))
	})
}

func TestMLWHRunsFlatListingCursorStatusUsesRowsAfterPage(t *testing.T) {
	convey.Convey("Given a cursor page whose total includes rows before the cursor", t, func() {
		stub := &stubMLWHRunsClient{
			list: func(_ context.Context, _ mlwh.RunAggregationOptions, limit int, cursor string) ([]mlwh.RunListingRow, error) {
				if limit == 2 && cursor == "illumina:52553" {
					return []mlwh.RunListingRow{
						runListingTestRow("ont:ONTRUN-11", "ONT", "ONTRUN-11"),
						runListingTestRow("pacbio:TRACTION-RUN-1000", "PacBio", "TRACTION-RUN-1000"),
					}, nil
				}

				return []mlwh.RunListingRow{}, nil
			},
			count: func(_ context.Context, _ mlwh.RunAggregationOptions) (mlwh.Count, error) {
				return mlwh.Count{Count: 3}, nil
			},
		}
		withStubMLWHRunsClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{
			"mlwh", "runs", "--limit", "2", "--cursor", "illumina:52553",
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "bounded page emitted: rows=2 total=3 next_cursor=<none>")
		convey.So(output, convey.ShouldNotContainSubstring, "next_cursor=pacbio:TRACTION-RUN-1000")
	})

	convey.Convey("Given a non-final bounded page", t, func() {
		stub := &stubMLWHRunsClient{
			list: func(_ context.Context, _ mlwh.RunAggregationOptions, limit int, cursor string) ([]mlwh.RunListingRow, error) {
				if limit == 2 && cursor == "" {
					return []mlwh.RunListingRow{
						runListingTestRow("illumina:52553", "Illumina", "52553"),
						runListingTestRow("ont:ONTRUN-11", "ONT", "ONTRUN-11"),
					}, nil
				}
				if limit == 1 && cursor == "ont:ONTRUN-11" {
					return []mlwh.RunListingRow{
						runListingTestRow("pacbio:TRACTION-RUN-1000", "PacBio", "TRACTION-RUN-1000"),
					}, nil
				}

				return []mlwh.RunListingRow{}, nil
			},
			count: func(_ context.Context, _ mlwh.RunAggregationOptions) (mlwh.Count, error) {
				return mlwh.Count{Count: 3}, nil
			},
		}
		withStubMLWHRunsClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{
			"mlwh", "runs", "--limit", "2",
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "bounded page emitted: rows=2 total=3 next_cursor=ont:ONTRUN-11")
	})
}

func runListingTestRow(id, platform, nativeID string) mlwh.RunListingRow {
	return mlwh.RunListingRow{
		ID:            id,
		Platform:      platform,
		NativeID:      nativeID,
		Manufacturer:  platform,
		RunDate:       "2024-01-11",
		DateBasis:     "run complete",
		CacheSyncedAt: "2026-07-01T08:00:00Z",
	}
}

func withStubMLWHRunsClient(t *testing.T, stub *stubMLWHRunsClient) {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHRunsClient
	t.Cleanup(func() { openMLWHRunsClient = original })

	openMLWHRunsClient = func(context.Context, mlwh.Config) (mlwhRunsClient, error) {
		return stub, nil
	}
}
