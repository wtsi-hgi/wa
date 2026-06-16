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
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
	"github.com/wtsi-hgi/wa/mlwhdiff"
	_ "modernc.org/sqlite"
)

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("broken pipe")
}

func TestDiffCommandWriteFailureDoesNotAdvanceWatermark(t *testing.T) {
	setMLWHDiffMLWHEnvForTest(t)

	originalOpen := openMLWHDiffClientFunc
	defer func() { openMLWHDiffClientFunc = originalOpen }()

	openMLWHDiffClientFunc = func(_ context.Context, _ mlwhdiffMLWHConfig) (mlwhdiffCommandClient, error) {
		return &mlwhdiffTestClient{provider: &mlwhdiffMockProvider{
			samplesForStudyFunc: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return []mlwh.Sample{{Name: "S1"}, {Name: "S2"}}, nil
			},
		}}, nil
	}

	convey.Convey("CLI diff does not advance the watermark when writing output fails", t, func() {
		dbPath := t.TempDir() + "/mlwhdiff.db"
		cmd := NewRootCommand()
		cmd.SetOut(failingWriter{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"mlwhdiff", "diff", "--study", "100", "--db", dbPath})

		err := cmd.Execute()
		convey.So(err, convey.ShouldNotBeNil)

		stdout, _, rerunErr := executeMLWHDiffCommand(t, []string{"mlwhdiff", "diff", "--study", "100", "--db", dbPath})
		convey.So(rerunErr, convey.ShouldBeNil)

		var result mlwhdiff.DiffResult[mlwh.Sample]
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)
	})
}

func setMLWHDiffMLWHEnvForTest(t *testing.T) {
	t.Helper()

	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/warehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_CACHE_PATH", filepath.Join(t.TempDir(), "mlwh.sqlite"))
}

func executeMLWHDiffCommand(t *testing.T, args []string) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return stdout, stderr, err
}

type mlwhdiffMockProvider struct {
	mlwh.Queryer

	queryContextFunc              func(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	classifyIdentifierFunc        func(ctx context.Context, raw string) (mlwh.Match, error)
	resolveSampleFunc             func(ctx context.Context, raw string) (mlwh.Match, error)
	resolveStudyFunc              func(ctx context.Context, raw string) (mlwh.Match, error)
	resolveRunFunc                func(ctx context.Context, raw string) (mlwh.Match, error)
	resolveLibraryFunc            func(ctx context.Context, raw string) (mlwh.Match, error)
	allStudiesFunc                func(ctx context.Context, limit, offset int) ([]mlwh.Study, error)
	getStudyFunc                  func(ctx context.Context, identifier string) (*mlwh.Study, error)
	samplesForStudyFunc           func(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
	allSamplesForStudyFunc        func(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error)
	findSamplesBySangerIDFunc     func(ctx context.Context, sangerID string) ([]mlwh.Sample, error)
	findSamplesByIDSampleLimsFunc func(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error)
	findSamplesByRunIDFunc        func(ctx context.Context, idRun int) ([]mlwh.Sample, error)
	findSamplesByLibraryTypeFunc  func(ctx context.Context, libraryType string) ([]mlwh.Sample, error)
	findSamplesByAccessionFunc    func(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error)
	samplesForRunFunc             func(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error)
	samplesForLibraryTypeFunc     func(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error)
	samplesForLibraryFunc         func(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
	librariesForStudyFunc         func(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error)
	studiesForSampleFunc          func(ctx context.Context, sangerName string) ([]mlwh.Study, error)
	lanesForSampleFunc            func(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error)
	irodsPathsForSampleFunc       func(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error)
	getSampleFilesFunc            func(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error)
}

func (m *mlwhdiffMockProvider) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if m != nil && m.queryContextFunc != nil {
		return m.queryContextFunc(ctx, query, args...)
	}

	return nil, nil
}

func (m *mlwhdiffMockProvider) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.classifyIdentifierFunc != nil {
		return m.classifyIdentifierFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *mlwhdiffMockProvider) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.resolveSampleFunc != nil {
		return m.resolveSampleFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *mlwhdiffMockProvider) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.resolveStudyFunc != nil {
		return m.resolveStudyFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *mlwhdiffMockProvider) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.resolveRunFunc != nil {
		return m.resolveRunFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *mlwhdiffMockProvider) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.resolveLibraryFunc != nil {
		return m.resolveLibraryFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *mlwhdiffMockProvider) AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error) {
	if m != nil && m.allStudiesFunc != nil {
		return m.allStudiesFunc(ctx, limit, offset)
	}

	return nil, nil
}

func (m *mlwhdiffMockProvider) GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error) {
	if m != nil && m.getStudyFunc != nil {
		return m.getStudyFunc(ctx, identifier)
	}

	return nil, nil
}

func (m *mlwhdiffMockProvider) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.samplesForStudyFunc != nil {
		return m.samplesForStudyFunc(ctx, studyLimsID, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error) {
	if m != nil && m.allSamplesForStudyFunc != nil {
		return m.allSamplesForStudyFunc(ctx, studyLimsID)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesBySangerIDFunc != nil {
		return m.findSamplesBySangerIDFunc(ctx, sangerID)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesByIDSampleLimsFunc != nil {
		return m.findSamplesByIDSampleLimsFunc(ctx, idSampleLims)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesByRunIDFunc != nil {
		return m.findSamplesByRunIDFunc(ctx, idRun)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesByLibraryTypeFunc != nil {
		return m.findSamplesByLibraryTypeFunc(ctx, libraryType)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesByAccessionFunc != nil {
		return m.findSamplesByAccessionFunc(ctx, accessionNumber)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.samplesForRunFunc != nil {
		return m.samplesForRunFunc(ctx, idRun, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.samplesForLibraryTypeFunc != nil {
		return m.samplesForLibraryTypeFunc(ctx, pipelineIDLims, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.samplesForLibraryFunc != nil {
		return m.samplesForLibraryFunc(ctx, pipelineIDLims, studyLimsID, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *mlwhdiffMockProvider) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error) {
	if m != nil && m.librariesForStudyFunc != nil {
		return m.librariesForStudyFunc(ctx, studyLimsID, limit, offset)
	}

	return nil, nil
}

func (m *mlwhdiffMockProvider) StudiesForSample(ctx context.Context, sangerName string) ([]mlwh.Study, error) {
	if m != nil && m.studiesForSampleFunc != nil {
		return m.studiesForSampleFunc(ctx, sangerName)
	}

	return nil, nil
}

func (m *mlwhdiffMockProvider) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	if m != nil && m.lanesForSampleFunc != nil {
		return m.lanesForSampleFunc(ctx, sangerName, limit, offset)
	}

	return []mlwh.Lane{}, nil
}

func (m *mlwhdiffMockProvider) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	if m != nil && m.irodsPathsForSampleFunc != nil {
		return m.irodsPathsForSampleFunc(ctx, sangerName, limit, offset)
	}

	return []mlwh.IRODSPath{}, nil
}

func (m *mlwhdiffMockProvider) GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
	if m != nil && m.getSampleFilesFunc != nil {
		return m.getSampleFilesFunc(ctx, sangerName)
	}

	return []mlwh.IRODSPath{}, nil
}

func TestDiffCommand(t *testing.T) {
	setMLWHDiffMLWHEnvForTest(t)

	originalOpen := openMLWHDiffClientFunc
	defer func() { openMLWHDiffClientFunc = originalOpen }()

	openMLWHDiffClientFunc = func(_ context.Context, _ mlwhdiffMLWHConfig) (mlwhdiffCommandClient, error) {
		return &mlwhdiffTestClient{provider: &mlwhdiffMockProvider{
			samplesForStudyFunc: func(_ context.Context, studyLimsID string, _, _ int) ([]mlwh.Sample, error) {
				if studyLimsID != "100" {
					return nil, errors.New("unexpected study id")
				}

				return []mlwh.Sample{{Name: "S1"}, {Name: "S2"}}, nil
			},
			irodsPathsForSampleFunc: func(_ context.Context, sangerName string, _, _ int) ([]mlwh.IRODSPath, error) {
				if sangerName != "ABC" {
					return nil, errors.New("unexpected sample id")
				}

				return []mlwh.IRODSPath{{Collection: "/abc", IRODSPath: "/abc/file.cram"}}, nil
			},
		}}, nil
	}

	convey.Convey("F1: diff subcommand prints JSON output", t, func() {
		var stderr *bytes.Buffer

		stdout, _, err := executeMLWHDiffCommand(t, []string{"mlwhdiff", "diff", "--study", "100", "--db", t.TempDir() + "/mlwhdiff.db"})
		convey.So(err, convey.ShouldBeNil)

		var result mlwhdiff.DiffResult[mlwh.Sample]
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)

		stdout, _, err = executeMLWHDiffCommand(t, []string{"mlwhdiff", "diff", "--sample", "ABC", "--db", t.TempDir() + "/mlwhdiff.db"})
		convey.So(err, convey.ShouldBeNil)

		var fileResult mlwhdiff.DiffResult[mlwh.IRODSPath]
		convey.So(json.Unmarshal(stdout.Bytes(), &fileResult), convey.ShouldBeNil)
		convey.So(fileResult.Added, convey.ShouldHaveLength, 1)

		_, stderr, err = executeMLWHDiffCommand(t, []string{"mlwhdiff", "diff"})
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "usage")

		_, _, err = executeMLWHDiffCommand(t, []string{"mlwhdiff", "diff", "--study", "100", "--sample", "ABC"})
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func TestServeCommand(t *testing.T) {
	setMLWHDiffMLWHEnvForTest(t)

	originalListen := listenFunc
	defer func() { listenFunc = originalListen }()
	originalOpen := openMLWHDiffClientFunc
	defer func() { openMLWHDiffClientFunc = originalOpen }()

	provider := &mlwhdiffMockProvider{
		allStudiesFunc: func(_ context.Context, _, _ int) ([]mlwh.Study, error) {
			return []mlwh.Study{{IDStudyLims: "6568", Name: "HCA"}}, nil
		},
		irodsPathsForSampleFunc: func(_ context.Context, sangerName string, _, _ int) ([]mlwh.IRODSPath, error) {
			if sangerName != "S1" {
				return nil, mlwh.ErrNotFound
			}

			return []mlwh.IRODSPath{{IDProduct: "P1"}}, nil
		},
	}
	openMLWHDiffClientFunc = func(_ context.Context, _ mlwhdiffMLWHConfig) (mlwhdiffCommandClient, error) {
		return &mlwhdiffTestClient{provider: provider}, nil
	}

	convey.Convey("F3: serve subcommand starts the HTTP API", t, func() {
		addrCh := make(chan string, 1)
		requestedAddr := ""
		listenFunc = func(network, addr string) (net.Listener, error) {
			requestedAddr = addr
			listener, err := net.Listen(network, "127.0.0.1:0")
			if err == nil {
				addrCh <- listener.Addr().String()
			}

			return listener, err
		}

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd := NewRootCommand()
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"mlwhdiff", "serve", "--port", "0", "--db", t.TempDir() + "/mlwhdiff.db"})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		addr := <-addrCh
		var response *http.Response
		var err error
		for attempt := 0; attempt < 20; attempt++ {
			response, err = http.Get("http://" + addr + "/diff/study/all")
			if err == nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = response.Body.Close() }()
		convey.So(response.StatusCode, convey.ShouldEqual, http.StatusOK)

		var result mlwhdiff.DiffResult[mlwh.Study]
		convey.So(json.NewDecoder(response.Body).Decode(&result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 1)

		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)

		cmd = NewRootCommand()
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"mlwhdiff", "serve", "--db", t.TempDir() + "/mlwhdiff.db"})
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
		errCh = make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()
		<-addrCh
		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)
		convey.So(requestedAddr, convey.ShouldEqual, ":8080")

		_, _, err = executeMLWHDiffCommand(t, []string{"mlwhdiff", "serve", "--port", "abc"})
		convey.So(err, convey.ShouldNotBeNil)
	})

	convey.Convey("E2.2: mlwhdiff serve boots with env-based MLWH cache passwords without exposing secrets on the CLI", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/warehouse")
		t.Setenv("WA_MLWH_PASSWORD", "secret")
		t.Setenv("WA_MLWH_CACHE_PASSWORD", "cache-secret")
		t.Setenv("WA_MLWH_CACHE_PATH", t.TempDir()+"/mlwh.sqlite")

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		openMLWHDiffClientFunc = originalOpen

		originalOpenMLWH := openMLWHDiffMLWHCacheOnlyClient
		defer func() { openMLWHDiffMLWHCacheOnlyClient = originalOpenMLWH }()

		captured := mlwh.CacheConfig{}
		openMLWHDiffMLWHCacheOnlyClient = func(_ context.Context, cfg mlwh.CacheConfig) (*mlwh.Client, error) {
			captured = cfg

			return &mlwh.Client{}, nil
		}

		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		cmd := NewRootCommand()
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"mlwhdiff", "serve", "--port", "0", "--db", t.TempDir() + "/mlwhdiff.db"})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		<-addrCh
		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)
		convey.So(captured.Path, convey.ShouldContainSubstring, "mlwh.sqlite")
		convey.So(captured.Password, convey.ShouldEqual, "cache-secret")
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, "secret")
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, "cache-secret")
		convey.So(stderr.String(), convey.ShouldNotContainSubstring, "secret")
		convey.So(stderr.String(), convey.ShouldNotContainSubstring, "cache-secret")
		convey.So(strings.Join(os.Args, " "), convey.ShouldNotContainSubstring, "secret")
		convey.So(strings.Join(os.Args, " "), convey.ShouldNotContainSubstring, "cache-secret")
	})

	convey.Convey("mlwhdiff serve opens the read-only MLWH provider from cache when a source DSN is present", t, func() {
		openMLWHDiffClientFunc = originalOpen
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(127.0.0.1:1)/warehouse")
		t.Setenv("WA_MLWH_CACHE_PATH", filepath.Join(t.TempDir(), "mlwh.sqlite"))

		err := executeServeCommandUntilListeningForTest(t, []string{"mlwhdiff", "serve", "--port", "0", "--db", t.TempDir() + "/mlwhdiff.db"})

		convey.So(err, convey.ShouldBeNil)
	})

	convey.Convey("E2.2a: mlwhdiff serve returns MLWH open errors", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/warehouse")
		t.Setenv("WA_MLWH_CACHE_PATH", t.TempDir()+"/mlwh.sqlite")

		originalOpenMLWH := openMLWHDiffMLWHCacheOnlyClient
		defer func() { openMLWHDiffMLWHCacheOnlyClient = originalOpenMLWH }()

		openMLWHDiffMLWHCacheOnlyClient = func(_ context.Context, _ mlwh.CacheConfig) (*mlwh.Client, error) {
			return nil, errors.New("boom")
		}

		_, _, err := executeMLWHDiffCommand(t, []string{"mlwhdiff", "serve", "--port", "0", "--db", t.TempDir() + "/mlwhdiff.db"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "boom")
	})

	convey.Convey("E2.3: mlwhdiff serve rejects password-bearing --mlwh-cache DSNs", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/warehouse")

		_, stderr, err := executeMLWHDiffCommand(t, []string{"mlwhdiff", "serve", "--mlwh-cache", "user:pass@tcp(localhost:3306)/wa_cache"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, mlwh.ErrPasswordInDSN), convey.ShouldBeTrue)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "--mlwh-cache")
	})

	convey.Convey("E2.4: mlwhdiff serve rejects the removed --mlwh-sync-interval flag", t, func() {
		setMLWHDiffMLWHEnvForTest(t)

		_, stderr, err := executeMLWHDiffCommand(t, []string{"mlwhdiff", "serve", "--mlwh-sync-interval", "5m"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "unknown flag: --mlwh-sync-interval")
	})
}

func TestMLWHDiffServeMLWHSelectionE3(t *testing.T) {
	convey.Convey("E3.1: Given WA_MLWH_SERVER_URL set, when mlwhdiff serve builds its source, then remote MLWH config is selected", t, func() {
		originalListen := listenFunc
		defer func() { listenFunc = originalListen }()
		originalOpen := openMLWHDiffClientFunc
		defer func() { openMLWHDiffClientFunc = originalOpen }()

		t.Setenv("WA_MLWH_SERVER_URL", "https://mlwh:9000")
		t.Setenv("WA_MLWH_CACHE_PATH", "")

		seenConfigCh := make(chan mlwhdiffMLWHConfig, 1)
		openMLWHDiffClientFunc = func(_ context.Context, cfg mlwhdiffMLWHConfig) (mlwhdiffMLWHHandle, error) {
			seenConfigCh <- cfg

			return &mlwhdiffTestClient{provider: &mlwhdiffMockProvider{}}, nil
		}

		err := executeServeCommandUntilListeningForTest(t, []string{"mlwhdiff", "serve", "--port", "0", "--db", filepath.Join(t.TempDir(), "mlwhdiff.db")})
		seenConfig := receiveMLWHDiffConfigForTest(t, seenConfigCh)

		convey.So(err, convey.ShouldBeNil)
		convey.So(seenConfig.ServerURL, convey.ShouldEqual, "https://mlwh:9000")
		convey.So(seenConfig.CachePath, convey.ShouldEqual, "")
	})

	convey.Convey("E3.2: Given only WA_MLWH_CACHE_PATH, when mlwhdiff serve builds its source, then local cache config is selected", t, func() {
		originalListen := listenFunc
		defer func() { listenFunc = originalListen }()
		originalOpen := openMLWHDiffClientFunc
		defer func() { openMLWHDiffClientFunc = originalOpen }()

		cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
		t.Setenv("WA_MLWH_SERVER_URL", "")
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)

		seenConfigCh := make(chan mlwhdiffMLWHConfig, 1)
		openMLWHDiffClientFunc = func(_ context.Context, cfg mlwhdiffMLWHConfig) (mlwhdiffMLWHHandle, error) {
			seenConfigCh <- cfg

			return &mlwhdiffTestClient{provider: &mlwhdiffMockProvider{}}, nil
		}

		err := executeServeCommandUntilListeningForTest(t, []string{"mlwhdiff", "serve", "--port", "0", "--db", filepath.Join(t.TempDir(), "mlwhdiff.db")})
		seenConfig := receiveMLWHDiffConfigForTest(t, seenConfigCh)

		convey.So(err, convey.ShouldBeNil)
		convey.So(seenConfig.ServerURL, convey.ShouldEqual, "")
		convey.So(seenConfig.CachePath, convey.ShouldEqual, cachePath)
	})

	convey.Convey("E3.1/E3.2: Given resolved configs, then the default opener constructs remote and local MLWH handles", t, func() {
		remote, err := openMLWHDiffClientWithConfig(context.Background(), mlwhdiffMLWHConfig{ServerURL: "https://mlwh:9000"})
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = remote.Close() }()
		_, isRemote := remote.(*mlwh.RemoteClient)
		convey.So(isRemote, convey.ShouldBeTrue)

		local, err := openMLWHDiffClientWithConfig(context.Background(), mlwhdiffMLWHConfig{CachePath: filepath.Join(t.TempDir(), "mlwh.sqlite")})
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = local.Close() }()
		_, isLocal := local.(*mlwh.Client)
		convey.So(isLocal, convey.ShouldBeTrue)
	})
}

func receiveMLWHDiffConfigForTest(t *testing.T, configs <-chan mlwhdiffMLWHConfig) mlwhdiffMLWHConfig {
	t.Helper()

	select {
	case cfg := <-configs:
		return cfg
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for mlwhdiff MLWH config")

		return mlwhdiffMLWHConfig{}
	}
}

func TestMLWHDiffServeDiffSourceE3(t *testing.T) {
	originalListen := listenFunc
	defer func() { listenFunc = originalListen }()
	originalOpen := openMLWHDiffClientFunc
	defer func() { openMLWHDiffClientFunc = originalOpen }()

	setMLWHDiffMLWHEnvForTest(t)
	provider := &mlwhdiffMockProvider{
		allStudiesFunc: func(_ context.Context, _, _ int) ([]mlwh.Study, error) {
			return []mlwh.Study{
				{IDStudyLims: "6568", Name: "HCA"},
				{IDStudyLims: "7777", Name: "WGS"},
			}, nil
		},
	}
	openMLWHDiffClientFunc = func(_ context.Context, _ mlwhdiffMLWHConfig) (mlwhdiffMLWHHandle, error) {
		return &mlwhdiffTestClient{provider: provider}, nil
	}

	convey.Convey("E3.3: Given a DiffSource returning two studies, when GET /diff/study/all is served, then added has length 2", t, func() {
		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		cmd := NewRootCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"mlwhdiff", "serve", "--port", "0", "--db", filepath.Join(t.TempDir(), "mlwhdiff.db")})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		addr := <-addrCh
		response, err := mlwhdiffGETForTest("http://" + addr + "/diff/study/all")
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = response.Body.Close() }()
		convey.So(response.StatusCode, convey.ShouldEqual, http.StatusOK)

		var result mlwhdiff.DiffResult[mlwh.Study]
		convey.So(json.NewDecoder(response.Body).Decode(&result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)

		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)
	})
}

func mlwhdiffGETForTest(rawURL string) (*http.Response, error) {
	var response *http.Response
	var err error
	for range 20 {
		response, err = http.Get(rawURL)
		if err == nil {
			return response, nil
		}
		time.Sleep(25 * time.Millisecond)
	}

	return response, err
}

type mlwhdiffTestClient struct {
	mlwh.Queryer

	provider  *mlwhdiffMockProvider
	syncFunc  func(context.Context) ([]mlwh.SyncReport, error)
	closeFunc func() error
}

func (c *mlwhdiffTestClient) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.provider.QueryContext(ctx, query, args...)
}

func (c *mlwhdiffTestClient) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ClassifyIdentifier(ctx, raw)
}

func (c *mlwhdiffTestClient) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ResolveSample(ctx, raw)
}

func (c *mlwhdiffTestClient) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ResolveStudy(ctx, raw)
}

func (c *mlwhdiffTestClient) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ResolveRun(ctx, raw)
}

func (c *mlwhdiffTestClient) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ResolveLibrary(ctx, raw)
}

func (c *mlwhdiffTestClient) AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error) {
	return c.provider.AllStudies(ctx, limit, offset)
}

func (c *mlwhdiffTestClient) GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error) {
	return c.provider.GetStudy(ctx, identifier)
}

func (c *mlwhdiffTestClient) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	return c.provider.SamplesForStudy(ctx, studyLimsID, limit, offset)
}

func (c *mlwhdiffTestClient) AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error) {
	return c.provider.AllSamplesForStudy(ctx, studyLimsID)
}

func (c *mlwhdiffTestClient) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesBySangerID(ctx, sangerID)
}

func (c *mlwhdiffTestClient) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesByIDSampleLims(ctx, idSampleLims)
}

func (c *mlwhdiffTestClient) FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesByRunID(ctx, idRun)
}

func (c *mlwhdiffTestClient) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesByLibraryType(ctx, libraryType)
}

func (c *mlwhdiffTestClient) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesByAccessionNumber(ctx, accessionNumber)
}

func (c *mlwhdiffTestClient) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error) {
	return c.provider.SamplesForRun(ctx, idRun, limit, offset)
}

func (c *mlwhdiffTestClient) SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error) {
	return c.provider.SamplesForLibraryType(ctx, pipelineIDLims, limit, offset)
}

func (c *mlwhdiffTestClient) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	return c.provider.SamplesForLibrary(ctx, pipelineIDLims, studyLimsID, limit, offset)
}

func (c *mlwhdiffTestClient) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error) {
	return c.provider.LibrariesForStudy(ctx, studyLimsID, limit, offset)
}

func (c *mlwhdiffTestClient) StudiesForSample(ctx context.Context, sangerName string) ([]mlwh.Study, error) {
	return c.provider.StudiesForSample(ctx, sangerName)
}

func (c *mlwhdiffTestClient) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	return c.provider.LanesForSample(ctx, sangerName, limit, offset)
}

func (c *mlwhdiffTestClient) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	return c.provider.IRODSPathsForSample(ctx, sangerName, limit, offset)
}

func (c *mlwhdiffTestClient) GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
	return c.provider.GetSampleFiles(ctx, sangerName)
}

func (c *mlwhdiffTestClient) Sync(ctx context.Context) ([]mlwh.SyncReport, error) {
	if c.syncFunc != nil {
		return c.syncFunc(ctx)
	}

	return nil, nil
}

func (c *mlwhdiffTestClient) Close() error {
	if c.closeFunc != nil {
		return c.closeFunc()
	}

	return nil
}

func TestMLWHDiffMLWHClientAdapterFindSamplesBySangerIDForwardsDirectly(t *testing.T) {
	adapter, db := openMLWHDiffAdapterForTest(t)
	seedMLWHDiffSyncState(t, db, "sample")
	seedMLWHDiffSample(t, db, mlwh.Sample{
		IDSampleTmp:     1,
		IDLims:          "SQSCP",
		IDSampleLims:    "777",
		UUIDSampleLims:  "00000000-0000-0000-0000-000000000001",
		Name:            "SANGER-1",
		SangerSampleID:  "12345",
		SupplierName:    "SUP-1",
		AccessionNumber: "ACC-1",
		DonorID:         "DONOR-1",
		TaxonID:         9606,
		CommonName:      "human",
		Description:     "sample",
	})

	convey.Convey("FindSamplesBySangerID forwards to the dedicated mlwh finder", t, func() {
		samples, err := adapter.FindSamplesBySangerID(context.Background(), "12345")
		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].Name, convey.ShouldEqual, "SANGER-1")
		convey.So(samples[0].SangerSampleID, convey.ShouldEqual, "12345")
	})
}

func seedMLWHDiffSyncState(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, NULL, 0)`,
		tableName,
		"2026-05-11T00:00:00Z",
		"2026-05-11T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert sync_state %s: %v", tableName, err)
	}
}

func seedMLWHDiffSample(t *testing.T, db *sql.DB, sample mlwh.Sample) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sample.IDSampleTmp,
		sample.IDLims,
		sample.IDSampleLims,
		sample.UUIDSampleLims,
		sample.Name,
		sample.SangerSampleID,
		sample.SupplierName,
		sample.AccessionNumber,
		sample.DonorID,
		sample.TaxonID,
		sample.CommonName,
		sample.Description,
		"2026-05-11T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert sample_mirror %d: %v", sample.IDSampleTmp, err)
	}
}

func TestMLWHDiffMLWHClientAdapterFindSamplesByIDSampleLimsForwardsDirectly(t *testing.T) {
	adapter, db := openMLWHDiffAdapterForTest(t)
	seedMLWHDiffSyncState(t, db, "sample")
	seedMLWHDiffSample(t, db, mlwh.Sample{
		IDSampleTmp:     2,
		IDLims:          "SQSCP",
		IDSampleLims:    "SAMPLE-LIMS-2",
		UUIDSampleLims:  "00000000-0000-0000-0000-000000000002",
		Name:            "SANGER-2",
		SangerSampleID:  "SS2",
		SupplierName:    "SUP-2",
		AccessionNumber: "ACC-2",
		DonorID:         "DONOR-2",
		TaxonID:         9606,
		CommonName:      "human",
		Description:     "sample",
	})

	convey.Convey("FindSamplesByIDSampleLims forwards to the dedicated mlwh finder", t, func() {
		samples, err := adapter.FindSamplesByIDSampleLims(context.Background(), "SAMPLE-LIMS-2")
		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].IDSampleLims, convey.ShouldEqual, "SAMPLE-LIMS-2")
	})
}

func TestMLWHDiffMLWHClientAdapterFindSamplesByAccessionNumberForwardsDirectly(t *testing.T) {
	adapter, db := openMLWHDiffAdapterForTest(t)
	seedMLWHDiffSyncState(t, db, "sample")
	seedMLWHDiffSample(t, db, mlwh.Sample{
		IDSampleTmp:     3,
		IDLims:          "SQSCP",
		IDSampleLims:    "888",
		UUIDSampleLims:  "00000000-0000-0000-0000-000000000003",
		Name:            "SANGER-3",
		SangerSampleID:  "SS3",
		SupplierName:    "SUP-3",
		AccessionNumber: "2001",
		DonorID:         "DONOR-3",
		TaxonID:         9606,
		CommonName:      "human",
		Description:     "sample",
	})

	convey.Convey("FindSamplesByAccessionNumber forwards to the dedicated mlwh finder", t, func() {
		samples, err := adapter.FindSamplesByAccessionNumber(context.Background(), "2001")
		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldHaveLength, 1)
		convey.So(samples[0].AccessionNumber, convey.ShouldEqual, "2001")
	})
}

func TestMLWHDiffMLWHClientAdapterFindSamplesByLibraryTypeForwardsDirectly(t *testing.T) {
	adapter, db := openMLWHDiffAdapterForTest(t)
	seedMLWHDiffSyncState(t, db, "iseq_flowcell")
	seedMLWHDiffSample(t, db, mlwh.Sample{
		IDSampleTmp:     4,
		IDLims:          "SQSCP",
		IDSampleLims:    "444",
		UUIDSampleLims:  "00000000-0000-0000-0000-000000000004",
		Name:            "SANGER-4",
		SangerSampleID:  "SS4",
		SupplierName:    "SUP-4",
		AccessionNumber: "ACC-4",
		DonorID:         "DONOR-4",
		TaxonID:         9606,
		CommonName:      "human",
		Description:     "sample",
	})
	seedMLWHDiffSample(t, db, mlwh.Sample{
		IDSampleTmp:     5,
		IDLims:          "SQSCP",
		IDSampleLims:    "555",
		UUIDSampleLims:  "00000000-0000-0000-0000-000000000005",
		Name:            "SANGER-5",
		SangerSampleID:  "SS5",
		SupplierName:    "SUP-5",
		AccessionNumber: "ACC-5",
		DonorID:         "DONOR-5",
		TaxonID:         9606,
		CommonName:      "human",
		Description:     "sample",
	})
	seedMLWHDiffLibrarySample(t, db, "LIBTYPE-1", 4, "6568")
	seedMLWHDiffLibrarySample(t, db, "LIBTYPE-1", 5, "7777")

	convey.Convey("FindSamplesByLibraryType forwards to the dedicated mlwh finder", t, func() {
		samples, err := adapter.FindSamplesByLibraryType(context.Background(), "LIBTYPE-1")
		convey.So(samples, convey.ShouldBeNil)
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, mlwh.ErrAmbiguous), convey.ShouldBeTrue)
	})
}

func seedMLWHDiffLibrarySample(t *testing.T, db *sql.DB, pipelineIDLims string, idSampleTmp int64, studyLimsID string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`,
		pipelineIDLims,
		idSampleTmp,
		studyLimsID,
	)
	if err != nil {
		t.Fatalf("insert library_samples %s/%d/%s: %v", pipelineIDLims, idSampleTmp, studyLimsID, err)
	}
}

func openMLWHDiffAdapterForTest(t *testing.T) (*mlwhdiffMLWHClientAdapter, *sql.DB) {
	t.Helper()

	cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
	client, err := mlwh.Open(context.Background(), mlwh.Config{
		Cache:  mlwh.CacheConfig{Path: cachePath},
		Source: mlwhdiffAdapterSourceStub{},
	})
	if err != nil {
		t.Fatalf("mlwh.Open(): %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	db, err := sql.Open("sqlite", cachePath)
	if err != nil {
		t.Fatalf("sql.Open(): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &mlwhdiffMLWHClientAdapter{client: client}, db
}

type mlwhdiffAdapterSourceStub struct{}

func (mlwhdiffAdapterSourceStub) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
	return nil, errors.New("unexpected source query")
}

func TestMLWHDiffServeHelpFlags(t *testing.T) {
	stdout, _, err := executeMLWHDiffCommand(t, []string{"mlwhdiff", "serve", "--help"})
	if err != nil {
		t.Fatalf("mlwhdiff serve --help: %v", err)
	}

	convey.Convey("E2.1: mlwhdiff serve help shows MLWH cache flags and hides removed or legacy flags", t, func() {
		convey.So(stdout.String(), convey.ShouldContainSubstring, "--mlwh-cache")
		convey.So(stdout.String(), convey.ShouldContainSubstring, "--mlwh-server-url")
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, "--mlwh-sync-interval")
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, "--token")
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, "--base-url")
	})
}
