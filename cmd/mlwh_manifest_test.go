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
	"encoding/json"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestMLWHManifestHelpRendersConfigurationDetails(t *testing.T) {
	convey.Convey("wa mlwh manifest --help renders documentation about env vars and an example", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "manifest", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_SERVER_URL")
		convey.So(output, convey.ShouldContainSubstring, "--with-irods")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh manifest")
	})
}

func TestMLWHManifestCommandRequiresStudy(t *testing.T) {
	convey.Convey("Given a missing study positional, when wa mlwh manifest runs, then it errors with usage and never opens a client", t, func() {
		original := openMLWHManifestClient
		t.Cleanup(func() { openMLWHManifestClient = original })
		openMLWHManifestClient = func(context.Context, mlwh.Config) (mlwhManifestClient, error) {
			t.Fatalf("client should not be opened when the study positional is missing")

			return nil, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "manifest"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "usage")
	})
}

// stubMLWHManifestClient is a fake mlwhManifestClient for the `wa mlwh manifest`
// tests. The single func field lets a test return a chosen StudyManifest envelope
// (or an error) and observe the file-type / with-irods / paging arguments the
// command dispatched with.
type stubMLWHManifestClient struct {
	manifest func(ctx context.Context, studyLimsID, fileType string, withIRODS bool, limit, offset int) (mlwh.StudyManifest, error)

	closed bool
}

func (s *stubMLWHManifestClient) StudyManifest(ctx context.Context, studyLimsID, fileType string, withIRODS bool, limit, offset int) (mlwh.StudyManifest, error) {
	if s.manifest != nil {
		return s.manifest(ctx, studyLimsID, fileType, withIRODS, limit, offset)
	}

	return mlwh.StudyManifest{}, nil
}

func (s *stubMLWHManifestClient) Close() error {
	s.closed = true

	return nil
}

// AT1: a fake client returning a StudyManifest with study metadata and 3 rows ->
// the study metadata prints ONCE and 3 row lines print with the per-row fields,
// exit 0.
func TestMLWHManifestPrintsMetadataOnceAndRows(t *testing.T) {
	convey.Convey("Given a fake client returning a manifest with study metadata + 3 rows, when wa mlwh manifest S1 runs, then the metadata prints once and 3 row lines print, exit 0 (H3 acceptance 1)", t, func() {
		var capturedID, capturedFileType string
		var capturedWithIRODS bool
		stub := &stubMLWHManifestClient{
			manifest: func(_ context.Context, studyLimsID, fileType string, withIRODS bool, _, _ int) (mlwh.StudyManifest, error) {
				capturedID = studyLimsID
				capturedFileType = fileType
				capturedWithIRODS = withIRODS

				return mlwh.StudyManifest{
					IDStudyLims:     "S1",
					Name:            "Study S1",
					AccessionNumber: "EGAS0000S1",
					FacultySponsor:  "Faculty sponsor 211",
					DataAccessGroup: "group-1",
					CacheSyncedAt:   "2026-06-27T06:00:00Z",
					Rows: []mlwh.ManifestRow{
						{Name: "S1-sample-alpha", SupplierName: "supplier-alpha", AccessionNumber: "EGAN-alpha", SangerSampleID: "sanger-alpha", IDRun: 52553, Position: 1, TagIndex: 1},
						{Name: "S1-sample-alpha", SupplierName: "supplier-alpha", AccessionNumber: "EGAN-alpha", SangerSampleID: "sanger-alpha", IDRun: 52553, Position: 1, TagIndex: 2},
						{Name: "S1-sample-beta", SupplierName: "supplier-beta", AccessionNumber: "EGAN-beta", SangerSampleID: "sanger-beta", IDRun: 52554, Position: 2, TagIndex: 3},
					},
				}, nil
			},
		}

		withStubMLWHManifestClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "manifest", "S1"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedID, convey.ShouldEqual, "S1")
		convey.So(capturedFileType, convey.ShouldEqual, "")
		convey.So(capturedWithIRODS, convey.ShouldBeFalse)
		convey.So(stub.closed, convey.ShouldBeTrue)

		// The study metadata header prints exactly once.
		convey.So(output, convey.ShouldContainSubstring, "Study S1")
		convey.So(output, convey.ShouldContainSubstring, "EGAS0000S1")
		convey.So(output, convey.ShouldContainSubstring, "Faculty sponsor 211")
		convey.So(output, convey.ShouldContainSubstring, "group-1")
		convey.So(strings.Count(output, "EGAS0000S1"), convey.ShouldEqual, 1)
		convey.So(strings.Count(output, "Faculty sponsor 211"), convey.ShouldEqual, 1)

		// One line per row carrying the per-row fields.
		convey.So(output, convey.ShouldContainSubstring, "S1-sample-alpha")
		convey.So(output, convey.ShouldContainSubstring, "supplier-alpha")
		convey.So(output, convey.ShouldContainSubstring, "sanger-alpha")
		convey.So(output, convey.ShouldContainSubstring, "S1-sample-beta")
		convey.So(output, convey.ShouldContainSubstring, "supplier-beta")
		convey.So(output, convey.ShouldContainSubstring, "sanger-beta")
		convey.So(output, convey.ShouldContainSubstring, "52554")

		// The header line must NOT carry an irods_path column when --with-irods is
		// not set, and no row line should either.
		convey.So(output, convey.ShouldNotContainSubstring, "irods_path")
	})
}

// AT2: --with-irods --file-type cram with rows carrying irods_path -> each row line
// includes its irods_path (an empty path renders as a placeholder like "-").
func TestMLWHManifestWithIRODSIncludesIRODSPath(t *testing.T) {
	convey.Convey("Given --with-irods --file-type cram and rows carrying irods_path, when wa mlwh manifest runs, then each row line includes its irods_path with empty rendered as '-' (H3 acceptance 2)", t, func() {
		var capturedFileType string
		var capturedWithIRODS bool
		stub := &stubMLWHManifestClient{
			manifest: func(_ context.Context, _, fileType string, withIRODS bool, _, _ int) (mlwh.StudyManifest, error) {
				capturedFileType = fileType
				capturedWithIRODS = withIRODS

				return mlwh.StudyManifest{
					IDStudyLims: "S1",
					Name:        "Study S1",
					Rows: []mlwh.ManifestRow{
						{Name: "S1-sample-alpha", SangerSampleID: "sanger-alpha", IDRun: 52553, Position: 1, TagIndex: 1, IRODSPath: "/seq/52553/52553_1#1.cram"},
						{Name: "S1-sample-alpha", SangerSampleID: "sanger-alpha", IDRun: 52553, Position: 1, TagIndex: 2, IRODSPath: "/seq/52553/52553_1#2.cram"},
						{Name: "S1-sample-beta", SangerSampleID: "sanger-beta", IDRun: 52554, Position: 2, TagIndex: 3, IRODSPath: ""},
					},
				}, nil
			},
		}

		withStubMLWHManifestClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "manifest", "S1", "--with-irods", "--file-type", "cram"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedWithIRODS, convey.ShouldBeTrue)
		convey.So(capturedFileType, convey.ShouldEqual, "cram")

		convey.So(output, convey.ShouldContainSubstring, "/seq/52553/52553_1#1.cram")
		convey.So(output, convey.ShouldContainSubstring, "/seq/52553/52553_1#2.cram")

		// The covered rows show their irods_path; the uncovered row renders the
		// placeholder.
		convey.So(output, convey.ShouldContainSubstring, "irods_path=/seq/52553/52553_1#1.cram")
		convey.So(output, convey.ShouldContainSubstring, "irods_path=-")
	})
}

// AT3: --json emits a single StudyManifest JSON OBJECT (the envelope), not a bare
// array: it decodes as an object exposing "rows".
func TestMLWHManifestJSONEmitsEnvelopeObject(t *testing.T) {
	convey.Convey("Given --json, when wa mlwh manifest runs, then stdout is a single StudyManifest object exposing rows, not a bare array (H3 acceptance 3)", t, func() {
		stub := &stubMLWHManifestClient{
			manifest: func(context.Context, string, string, bool, int, int) (mlwh.StudyManifest, error) {
				return mlwh.StudyManifest{
					IDStudyLims:     "S1",
					Name:            "Study S1",
					AccessionNumber: "EGAS0000S1",
					FacultySponsor:  "Faculty sponsor 211",
					DataAccessGroup: "group-1",
					CacheSyncedAt:   "2026-06-27T06:00:00Z",
					Rows: []mlwh.ManifestRow{
						{Name: "S1-sample-alpha", SangerSampleID: "sanger-alpha", IDRun: 52553, Position: 1, TagIndex: 1},
					},
				}, nil
			},
		}

		withStubMLWHManifestClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "manifest", "S1", "--json"})

		convey.So(err, convey.ShouldBeNil)

		// Decoding into a bare array must fail (it is an object, not an array).
		arr := []map[string]any{}
		convey.So(json.Unmarshal([]byte(output), &arr), convey.ShouldNotBeNil)

		// Decoding into an object must succeed and carry the envelope fields.
		decoded := map[string]any{}
		convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
		convey.So(decoded["id_study_lims"], convey.ShouldEqual, "S1")
		convey.So(decoded["name"], convey.ShouldEqual, "Study S1")
		convey.So(decoded["faculty_sponsor"], convey.ShouldEqual, "Faculty sponsor 211")
		convey.So(decoded["data_access_group"], convey.ShouldEqual, "group-1")

		rows, ok := decoded["rows"].([]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(rows, convey.ShouldHaveLength, 1)
	})
}

// AT4 (part 1): a synced study with NO products -> the header prints and a "no
// products" line prints, exit 0.
func TestMLWHManifestSyncedNoProductsPrintsNoProducts(t *testing.T) {
	convey.Convey("Given a synced study with no products (an envelope with empty Rows), when wa mlwh manifest runs, then the header prints with 'no products' and exit 0 (H3 acceptance 4)", t, func() {
		stub := &stubMLWHManifestClient{
			manifest: func(context.Context, string, string, bool, int, int) (mlwh.StudyManifest, error) {
				return mlwh.StudyManifest{
					IDStudyLims:     "S1",
					Name:            "Study S1",
					AccessionNumber: "EGAS0000S1",
					FacultySponsor:  "Faculty sponsor 211",
					DataAccessGroup: "group-1",
					CacheSyncedAt:   "2026-06-27T06:00:00Z",
					Rows:            []mlwh.ManifestRow{},
				}, nil
			},
		}

		withStubMLWHManifestClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "manifest", "S1"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Study S1")
		convey.So(output, convey.ShouldContainSubstring, "EGAS0000S1")
		convey.So(output, convey.ShouldContainSubstring, "no products")
	})
}

// AT4 (part 2): a never-synced cache via --server degrades gracefully: a neutral
// cache-unavailable message, no sync hint, exit 0 (locks the degradation policy).
func TestMLWHManifestServerModeNeverSyncedDegradesGracefully(t *testing.T) {
	convey.Convey("Given a never-synced cache reached via the MLWH server (no WA_MLWH_DSN), when wa mlwh manifest runs, then it degrades gracefully: a neutral cache-unavailable message, no sync hint, exit 0 (H3 acceptance 4)", t, func() {
		stub := &stubMLWHManifestClient{
			manifest: func(context.Context, string, string, bool, int, int) (mlwh.StudyManifest, error) {
				return mlwh.StudyManifest{}, mlwh.ErrCacheNeverSynced
			},
		}

		withServerModeMLWHManifestClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "manifest", "S1"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "not available")
	})
}

// withServerModeMLWHManifestClient wires `wa mlwh manifest` to talk to an MLWH
// server (the normal end-user path): WA_MLWH_SERVER_URL is set and WA_MLWH_DSN is
// empty, so the user cannot sync. The remote-client opener is stubbed to return
// stub.
func withServerModeMLWHManifestClient(t *testing.T, stub *stubMLWHManifestClient) {
	t.Helper()
	t.Setenv("WA_MLWH_SERVER_URL", "http://mlwh.example:8091")
	t.Setenv("WA_MLWH_DSN", "")
	t.Setenv("WA_MLWH_PASSWORD", "")
	t.Setenv("WA_MLWH_CACHE_PATH", "")
	t.Setenv("WA_MLWH_CACHE_PASSWORD", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHManifestRemoteClient
	t.Cleanup(func() { openMLWHManifestRemoteClient = original })

	openMLWHManifestRemoteClient = func(context.Context, mlwh.RemoteConfig) (mlwhManifestClient, error) {
		return stub, nil
	}
}

// An invalid --file-type yields a bad-request-class error (ErrUnsupportedIdentifier);
// the CLI prints a clear message naming the value and exits non-zero (an input
// error, not a degradation). Locks the exit-code policy alongside the soft cases.
func TestMLWHManifestInvalidFileTypeExitsNonZero(t *testing.T) {
	convey.Convey("Given --file-type a/b yields a bad-request-class error, when wa mlwh manifest runs, then it prints a clear message and exits non-zero", t, func() {
		stub := &stubMLWHManifestClient{
			manifest: func(context.Context, string, string, bool, int, int) (mlwh.StudyManifest, error) {
				return mlwh.StudyManifest{}, mlwh.ErrUnsupportedIdentifier
			},
		}

		withStubMLWHManifestClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "manifest", "S1", "--with-irods", "--file-type", "a/b"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "a/b")
		convey.So(output, convey.ShouldNotContainSubstring, "no products")
	})
}

// Paging: --limit/--offset are passed through to StudyManifest unchanged.
func TestMLWHManifestPassesPagingThrough(t *testing.T) {
	convey.Convey("Given --limit and --offset, when wa mlwh manifest runs, then they are passed to StudyManifest unchanged", t, func() {
		var capturedLimit, capturedOffset int
		stub := &stubMLWHManifestClient{
			manifest: func(_ context.Context, _, _ string, _ bool, limit, offset int) (mlwh.StudyManifest, error) {
				capturedLimit = limit
				capturedOffset = offset

				return mlwh.StudyManifest{IDStudyLims: "S1", Name: "Study S1", Rows: []mlwh.ManifestRow{}}, nil
			},
		}

		withStubMLWHManifestClient(t, stub)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "manifest", "S1", "--limit", "10", "--offset", "20"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedLimit, convey.ShouldEqual, 10)
		convey.So(capturedOffset, convey.ShouldEqual, 20)
	})
}

// withStubMLWHManifestClient wires `wa mlwh manifest` to a local cache that returns
// stub, mirroring withStubMLWHIRODSClient: WA_MLWH_DSN is set and WA_MLWH_SERVER_URL
// is cleared so openMLWHInfoConfiguredClient takes the local path.
func withStubMLWHManifestClient(t *testing.T, stub *stubMLWHManifestClient) {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHManifestClient
	t.Cleanup(func() { openMLWHManifestClient = original })

	openMLWHManifestClient = func(context.Context, mlwh.Config) (mlwhManifestClient, error) {
		return stub, nil
	}
}
