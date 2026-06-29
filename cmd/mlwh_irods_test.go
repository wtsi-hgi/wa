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
	"errors"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestMLWHIRODSHelpRendersConfigurationDetails(t *testing.T) {
	convey.Convey("wa mlwh irods --help renders documentation about env vars and an example", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "irods", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_SERVER_URL")
		convey.So(output, convey.ShouldContainSubstring, "--file-type")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh irods")
	})
}

// stubMLWHIRODSClient is a fake mlwhIRODSClient for the `wa mlwh irods` tests. Each
// scope dispatches to its own func field so a test can assert the command routed to
// the right method and observe the file-type/paging arguments.
type stubMLWHIRODSClient struct {
	study  func(ctx context.Context, id, fileType string, limit, offset int) ([]mlwh.IRODSPath, error)
	run    func(ctx context.Context, idRun, fileType string, limit, offset int) ([]mlwh.IRODSPath, error)
	sample func(ctx context.Context, name, fileType string, limit, offset int) ([]mlwh.IRODSPath, error)

	closed bool
}

func (s *stubMLWHIRODSClient) IRODSPathsForStudyByFileType(ctx context.Context, id, fileType string, limit, offset int) ([]mlwh.IRODSPath, error) {
	if s.study != nil {
		return s.study(ctx, id, fileType, limit, offset)
	}

	return nil, errors.New("study not stubbed")
}

func (s *stubMLWHIRODSClient) IRODSPathsForRun(ctx context.Context, idRun, fileType string, limit, offset int) ([]mlwh.IRODSPath, error) {
	if s.run != nil {
		return s.run(ctx, idRun, fileType, limit, offset)
	}

	return nil, errors.New("run not stubbed")
}

func (s *stubMLWHIRODSClient) IRODSPathsForSampleByFileType(ctx context.Context, name, fileType string, limit, offset int) ([]mlwh.IRODSPath, error) {
	if s.sample != nil {
		return s.sample(ctx, name, fileType, limit, offset)
	}

	return nil, errors.New("sample not stubbed")
}

func (s *stubMLWHIRODSClient) Close() error {
	s.closed = true

	return nil
}

func TestMLWHIRODSCommandRequiresScopeAndIdentifier(t *testing.T) {
	convey.Convey("Given missing positionals, when wa mlwh irods runs, then it errors with usage and never opens a client", t, func() {
		original := openMLWHIRODSClient
		t.Cleanup(func() { openMLWHIRODSClient = original })
		openMLWHIRODSClient = func(context.Context, mlwh.Config) (mlwhIRODSClient, error) {
			t.Fatalf("client should not be opened when positionals are missing")

			return nil, nil
		}

		for _, args := range [][]string{
			{"mlwh", "irods"},
			{"mlwh", "irods", "study"},
		} {
			output, err := executeRootCommandForTest(t, args)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "usage")
		}
	})

	convey.Convey("Given an unknown scope keyword, when wa mlwh irods runs, then it errors clearly", t, func() {
		stub := &stubMLWHIRODSClient{}
		withStubMLWHIRODSClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "irods", "banana", "S1"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "scope")
	})
}

// AT1: a fake client returning 2 .cram paths for study S1 prints both paths with
// their id_run/platform and exits 0.
func TestMLWHIRODSStudyPrintsPaths(t *testing.T) {
	convey.Convey("Given a fake client returning 2 cram paths for study S1, when wa mlwh irods study S1 --file-type cram runs, then both paths print with id_run/platform, exit 0 (H2 acceptance 1)", t, func() {
		var capturedFileType string
		stub := &stubMLWHIRODSClient{
			study: func(_ context.Context, id, fileType string, _, _ int) ([]mlwh.IRODSPath, error) {
				convey.So(id, convey.ShouldEqual, "S1")
				capturedFileType = fileType

				return []mlwh.IRODSPath{
					{IRODSPath: "/seq/illumina/runs/52/52553/plex1/52553#1.cram", IDRun: 52553, Platform: "illumina"},
					{IRODSPath: "/seq/illumina/runs/52/52553/plex2/52553#2.cram", IDRun: 52553, Platform: "illumina"},
				}, nil
			},
			run: func(context.Context, string, string, int, int) ([]mlwh.IRODSPath, error) {
				t.Fatalf("run dispatch must not be used for the study scope")

				return nil, nil
			},
			sample: func(context.Context, string, string, int, int) ([]mlwh.IRODSPath, error) {
				t.Fatalf("sample dispatch must not be used for the study scope")

				return nil, nil
			},
		}

		withStubMLWHIRODSClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "irods", "study", "S1", "--file-type", "cram"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedFileType, convey.ShouldEqual, "cram")
		convey.So(output, convey.ShouldContainSubstring, "/seq/illumina/runs/52/52553/plex1/52553#1.cram")
		convey.So(output, convey.ShouldContainSubstring, "/seq/illumina/runs/52/52553/plex2/52553#2.cram")
		convey.So(output, convey.ShouldContainSubstring, "52553")
		convey.So(output, convey.ShouldContainSubstring, "illumina")
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

// AT2: a run scope with an empty list prints "no matching iRODS paths" and exits 0.
func TestMLWHIRODSRunEmptyPrintsNoMatch(t *testing.T) {
	convey.Convey("Given wa mlwh irods run 52553 --file-type bam returns an empty list, when run, then it prints \"no matching iRODS paths\" and exits 0 (H2 acceptance 2)", t, func() {
		var capturedID, capturedFileType string
		stub := &stubMLWHIRODSClient{
			run: func(_ context.Context, idRun, fileType string, _, _ int) ([]mlwh.IRODSPath, error) {
				capturedID = idRun
				capturedFileType = fileType

				return []mlwh.IRODSPath{}, nil
			},
		}

		withStubMLWHIRODSClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "irods", "run", "52553", "--file-type", "bam"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedID, convey.ShouldEqual, "52553")
		convey.So(capturedFileType, convey.ShouldEqual, "bam")
		convey.So(output, convey.ShouldContainSubstring, "no matching iRODS paths")
	})
}

// AT3: --json emits a single JSON array of IRODSPath.
func TestMLWHIRODSJSONArrayOutput(t *testing.T) {
	convey.Convey("Given --json, when wa mlwh irods runs, then stdout is a single JSON array of IRODSPath (H2 acceptance 3)", t, func() {
		stub := &stubMLWHIRODSClient{
			sample: func(_ context.Context, name, _ string, _, _ int) ([]mlwh.IRODSPath, error) {
				convey.So(name, convey.ShouldEqual, "DN1234")

				return []mlwh.IRODSPath{{
					IRODSPath: "/seq/illumina/runs/48/48522/plex1/48522#1.cram",
					IDRun:     48522,
					Platform:  "illumina",
				}}, nil
			},
		}

		withStubMLWHIRODSClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "irods", "sample", "DN1234", "--json"})

		convey.So(err, convey.ShouldBeNil)

		decoded := []map[string]any{}
		convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
		convey.So(decoded, convey.ShouldHaveLength, 1)
		convey.So(decoded[0]["irods_path"], convey.ShouldEqual, "/seq/illumina/runs/48/48522/plex1/48522#1.cram")
		convey.So(decoded[0]["id_run"], convey.ShouldEqual, 48522)
		convey.So(decoded[0]["platform"], convey.ShouldEqual, "illumina")
	})
}

func TestMLWHIRODSEmptyJSONIsArray(t *testing.T) {
	convey.Convey("Given --json with an empty result, when wa mlwh irods runs, then stdout is an empty JSON array (not null)", t, func() {
		stub := &stubMLWHIRODSClient{
			study: func(context.Context, string, string, int, int) ([]mlwh.IRODSPath, error) {
				return []mlwh.IRODSPath{}, nil
			},
		}

		withStubMLWHIRODSClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "irods", "study", "S1", "--json"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(strings.TrimSpace(output), convey.ShouldEqual, "[]")
	})
}

// AT4: an invalid file type yields a bad-request-class error; the CLI prints a clear
// message and exits non-zero (input error, not a degradation).
func TestMLWHIRODSInvalidFileTypeExitsNonZero(t *testing.T) {
	convey.Convey("Given --file-type a/b yields a bad-request-class error, when wa mlwh irods runs, then it prints a clear message and exits non-zero (H2 acceptance 4)", t, func() {
		stub := &stubMLWHIRODSClient{
			study: func(context.Context, string, string, int, int) ([]mlwh.IRODSPath, error) {
				return nil, mlwh.ErrUnsupportedIdentifier
			},
		}

		withStubMLWHIRODSClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "irods", "study", "S1", "--file-type", "a/b"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "a/b")
		convey.So(output, convey.ShouldNotContainSubstring, "no matching iRODS paths")
	})
}

// withStubMLWHIRODSClient wires `wa mlwh irods` to a local cache that returns stub,
// mirroring withStubMLWHInfoClient: WA_MLWH_DSN is set and WA_MLWH_SERVER_URL is
// cleared so openMLWHInfoConfiguredClient takes the local path.
func withStubMLWHIRODSClient(t *testing.T, stub *stubMLWHIRODSClient) {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHIRODSClient
	t.Cleanup(func() { openMLWHIRODSClient = original })

	openMLWHIRODSClient = func(context.Context, mlwh.Config) (mlwhIRODSClient, error) {
		return stub, nil
	}
}

// Never-synced cache via --server degrades gracefully: a neutral cache-unavailable
// message, no sync hint, exit 0 (locks the degradation/exit-code policy).
func TestMLWHIRODSServerModeNeverSyncedDegradesGracefully(t *testing.T) {
	convey.Convey("Given a never-synced cache reached via the MLWH server (no WA_MLWH_DSN), when wa mlwh irods runs, then it degrades gracefully: a neutral cache-unavailable message, no sync hint, exit 0", t, func() {
		stub := &stubMLWHIRODSClient{
			study: func(context.Context, string, string, int, int) ([]mlwh.IRODSPath, error) {
				return []mlwh.IRODSPath{}, mlwh.ErrCacheNeverSynced
			},
		}

		withServerModeMLWHIRODSClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "irods", "study", "S1"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "not available")
	})
}

// withServerModeMLWHIRODSClient wires `wa mlwh irods` to talk to an MLWH server (the
// normal end-user path): WA_MLWH_SERVER_URL is set and WA_MLWH_DSN is empty, so the
// user cannot sync. The remote-client opener is stubbed to return stub.
func withServerModeMLWHIRODSClient(t *testing.T, stub *stubMLWHIRODSClient) {
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

	original := openMLWHIRODSRemoteClient
	t.Cleanup(func() { openMLWHIRODSRemoteClient = original })

	openMLWHIRODSRemoteClient = func(context.Context, mlwh.RemoteConfig) (mlwhIRODSClient, error) {
		return stub, nil
	}
}
