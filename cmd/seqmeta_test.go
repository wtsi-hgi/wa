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
	"github.com/wtsi-hgi/wa/seqmeta"
	_ "modernc.org/sqlite"
)

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("broken pipe")
}

func setSeqmetaMLWHEnvForTest(t *testing.T) {
	t.Helper()

	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/warehouse")
	t.Setenv("WA_MLWH_CACHE_PATH", filepath.Join(t.TempDir(), "mlwh.sqlite"))
}

type seqmetaTestClient struct {
	provider  seqmeta.Provider
	syncFunc  func(context.Context) ([]mlwh.SyncReport, error)
	closeFunc func() error
}

func (c *seqmetaTestClient) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.provider.QueryContext(ctx, query, args...)
}

func (c *seqmetaTestClient) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ClassifyIdentifier(ctx, raw)
}

func (c *seqmetaTestClient) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ResolveSample(ctx, raw)
}

func (c *seqmetaTestClient) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ResolveStudy(ctx, raw)
}

func (c *seqmetaTestClient) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ResolveRun(ctx, raw)
}

func (c *seqmetaTestClient) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	return c.provider.ResolveLibrary(ctx, raw)
}

func (c *seqmetaTestClient) AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error) {
	return c.provider.AllStudies(ctx, limit, offset)
}

func (c *seqmetaTestClient) GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error) {
	return c.provider.GetStudy(ctx, identifier)
}

func (c *seqmetaTestClient) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	return c.provider.SamplesForStudy(ctx, studyLimsID, limit, offset)
}

func (c *seqmetaTestClient) AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error) {
	return c.provider.AllSamplesForStudy(ctx, studyLimsID)
}

func (c *seqmetaTestClient) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesBySangerID(ctx, sangerID)
}

func (c *seqmetaTestClient) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesByIDSampleLims(ctx, idSampleLims)
}

func (c *seqmetaTestClient) FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesByRunID(ctx, idRun)
}

func (c *seqmetaTestClient) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesByLibraryType(ctx, libraryType)
}

func (c *seqmetaTestClient) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error) {
	return c.provider.FindSamplesByAccessionNumber(ctx, accessionNumber)
}

func (c *seqmetaTestClient) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error) {
	return c.provider.SamplesForRun(ctx, idRun, limit, offset)
}

func (c *seqmetaTestClient) SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error) {
	return c.provider.SamplesForLibraryType(ctx, pipelineIDLims, limit, offset)
}

func (c *seqmetaTestClient) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	return c.provider.SamplesForLibrary(ctx, pipelineIDLims, studyLimsID, limit, offset)
}

func (c *seqmetaTestClient) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error) {
	return c.provider.LibrariesForStudy(ctx, studyLimsID, limit, offset)
}

func (c *seqmetaTestClient) StudiesForSample(ctx context.Context, sangerName string) ([]mlwh.Study, error) {
	return c.provider.StudiesForSample(ctx, sangerName)
}

func (c *seqmetaTestClient) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	return c.provider.LanesForSample(ctx, sangerName, limit, offset)
}

func (c *seqmetaTestClient) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	return c.provider.IRODSPathsForSample(ctx, sangerName, limit, offset)
}

func (c *seqmetaTestClient) GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
	return c.provider.GetSampleFiles(ctx, sangerName)
}

func (c *seqmetaTestClient) Sync(ctx context.Context) ([]mlwh.SyncReport, error) {
	if c.syncFunc != nil {
		return c.syncFunc(ctx)
	}

	return nil, nil
}

func (c *seqmetaTestClient) Close() error {
	if c.closeFunc != nil {
		return c.closeFunc()
	}

	return nil
}

func TestDiffCommand(t *testing.T) {
	setSeqmetaMLWHEnvForTest(t)

	originalOpen := openSeqmetaClientFunc
	defer func() { openSeqmetaClientFunc = originalOpen }()

	openSeqmetaClientFunc = func(_ context.Context, _ seqmetaMLWHConfig) (seqmetaCommandClient, error) {
		return &seqmetaTestClient{provider: &seqmetaMockProvider{
			allSamplesForStudyFunc: func(_ context.Context, studyLimsID string) ([]mlwh.Sample, error) {
				if studyLimsID != "100" {
					return nil, errors.New("unexpected study id")
				}

				return []mlwh.Sample{{Name: "S1"}, {Name: "S2"}}, nil
			},
			getSampleFilesFunc: func(_ context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
				if sangerName != "ABC" {
					return nil, errors.New("unexpected sample id")
				}

				return []mlwh.IRODSPath{{Collection: "/abc", IRODSPath: "/abc/file.cram"}}, nil
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

		stdout, _, err := executeSeqmetaCommand(t, []string{"seqmeta", "diff", "--study", "100", "--db", t.TempDir() + "/seqmeta.db"})
		convey.So(err, convey.ShouldBeNil)

		var result seqmeta.DiffResult[mlwh.Sample]
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)

		stdout, _, err = executeSeqmetaCommand(t, []string{"seqmeta", "diff", "--sample", "ABC", "--db", t.TempDir() + "/seqmeta.db"})
		convey.So(err, convey.ShouldBeNil)

		var fileResult seqmeta.DiffResult[mlwh.IRODSPath]
		convey.So(json.Unmarshal(stdout.Bytes(), &fileResult), convey.ShouldBeNil)
		convey.So(fileResult.Added, convey.ShouldHaveLength, 1)

		_, stderr, err = executeSeqmetaCommand(t, []string{"seqmeta", "diff"})
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "usage")

		_, _, err = executeSeqmetaCommand(t, []string{"seqmeta", "diff", "--study", "100", "--sample", "ABC"})
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func executeSeqmetaCommand(t *testing.T, args []string) (*bytes.Buffer, *bytes.Buffer, error) {
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

func TestValidateCommand(t *testing.T) {
	setSeqmetaMLWHEnvForTest(t)

	originalOpen := openSeqmetaClientFunc
	defer func() { openSeqmetaClientFunc = originalOpen }()

	openSeqmetaClientFunc = func(_ context.Context, _ seqmetaMLWHConfig) (seqmetaCommandClient, error) {
		return &seqmetaTestClient{provider: &seqmetaMockProvider{
			classifyIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				switch raw {
				case "6568":
					study := &mlwh.Study{IDStudyLims: "6568", Name: "HCA"}

					return mlwh.Match{Kind: mlwh.KindStudyLimsID, Canonical: "6568", Study: study}, nil
				case "unknown_id":
					return mlwh.Match{}, mlwh.ErrNotFound
				default:
					return mlwh.Match{}, nil
				}
			},
		}}, nil
	}

	convey.Convey("F2: validate subcommand prints JSON and errors on bad input", t, func() {
		stdout, _, err := executeSeqmetaCommand(t, []string{"seqmeta", "validate", "6568"})
		convey.So(err, convey.ShouldBeNil)

		var result seqmeta.IdentifierResult
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, seqmeta.IdentifierStudyID)
		convey.So(result.Object, convey.ShouldNotBeNil)

		_, stderr, err := executeSeqmetaCommand(t, []string{"seqmeta", "validate", "unknown_id"})
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "unknown identifier")

		_, stderr, err = executeSeqmetaCommand(t, []string{"seqmeta", "validate"})
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "usage")
	})
}

func TestDiffCommandWriteFailureDoesNotAdvanceWatermark(t *testing.T) {
	setSeqmetaMLWHEnvForTest(t)

	originalOpen := openSeqmetaClientFunc
	defer func() { openSeqmetaClientFunc = originalOpen }()

	openSeqmetaClientFunc = func(_ context.Context, _ seqmetaMLWHConfig) (seqmetaCommandClient, error) {
		return &seqmetaTestClient{provider: &seqmetaMockProvider{
			allSamplesForStudyFunc: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return []mlwh.Sample{{Name: "S1"}, {Name: "S2"}}, nil
			},
		}}, nil
	}

	convey.Convey("CLI diff does not advance the watermark when writing output fails", t, func() {
		dbPath := t.TempDir() + "/seqmeta.db"
		cmd := NewRootCommand()
		cmd.SetOut(failingWriter{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"seqmeta", "diff", "--study", "100", "--db", dbPath})

		err := cmd.Execute()
		convey.So(err, convey.ShouldNotBeNil)

		stdout, _, rerunErr := executeSeqmetaCommand(t, []string{"seqmeta", "diff", "--study", "100", "--db", dbPath})
		convey.So(rerunErr, convey.ShouldBeNil)

		var result seqmeta.DiffResult[mlwh.Sample]
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)
	})
}

func TestServeCommand(t *testing.T) {
	setSeqmetaMLWHEnvForTest(t)

	originalListen := listenFunc
	defer func() { listenFunc = originalListen }()
	originalOpen := openSeqmetaClientFunc
	defer func() { openSeqmetaClientFunc = originalOpen }()

	provider := &seqmetaMockProvider{
		classifyIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
			if raw != "6568" {
				return mlwh.Match{}, mlwh.ErrNotFound
			}

			study := &mlwh.Study{IDStudyLims: "6568", Name: "HCA"}

			return mlwh.Match{Kind: mlwh.KindStudyLimsID, Canonical: "6568", Study: study}, nil
		},
	}
	openSeqmetaClientFunc = func(_ context.Context, _ seqmetaMLWHConfig) (seqmetaCommandClient, error) {
		return &seqmetaTestClient{provider: provider}, nil
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
		cmd.SetArgs([]string{"seqmeta", "serve", "--port", "0", "--db", t.TempDir() + "/seqmeta.db"})

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
			response, err = http.Get("http://" + addr + "/validate/6568")
			if err == nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = response.Body.Close() }()
		convey.So(response.StatusCode, convey.ShouldEqual, http.StatusOK)

		var result seqmeta.IdentifierResult
		convey.So(json.NewDecoder(response.Body).Decode(&result), convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, seqmeta.IdentifierStudyID)
		convey.So(result.Object, convey.ShouldNotBeNil)

		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)

		cmd = NewRootCommand()
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"seqmeta", "serve", "--db", t.TempDir() + "/seqmeta.db"})
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

		_, _, err = executeSeqmetaCommand(t, []string{"seqmeta", "serve", "--port", "abc"})
		convey.So(err, convey.ShouldNotBeNil)
	})

	convey.Convey("E2.2: seqmeta serve boots with env-based MLWH passwords without exposing them on the CLI", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/warehouse")
		t.Setenv("WA_MLWH_PASSWORD", "secret")
		t.Setenv("WA_MLWH_CACHE_PATH", t.TempDir()+"/mlwh.sqlite")

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		openSeqmetaClientFunc = originalOpen

		originalOpenMLWH := openSeqmetaMLWHClient
		defer func() { openSeqmetaMLWHClient = originalOpenMLWH }()

		captured := mlwh.Config{}
		openSeqmetaMLWHClient = func(_ context.Context, cfg mlwh.Config) (*mlwh.Client, error) {
			captured = cfg

			return &mlwh.Client{}, nil
		}

		addrCh := make(chan string, 1)
		listenFunc = resultsServeListenFuncForTest(addrCh)

		cmd := NewRootCommand()
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"seqmeta", "serve", "--port", "0", "--db", t.TempDir() + "/seqmeta.db"})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- cmd.ExecuteContext(ctx)
		}()

		<-addrCh
		cancel()
		convey.So(<-errCh, convey.ShouldBeNil)
		convey.So(captured.DSN, convey.ShouldEqual, "mlwh_user@tcp(localhost:3306)/warehouse")
		convey.So(captured.Password, convey.ShouldEqual, "secret")
		convey.So(captured.Cache.Path, convey.ShouldContainSubstring, "mlwh.sqlite")
		convey.So(captured.Cache.Password, convey.ShouldEqual, "")
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, "secret")
		convey.So(stderr.String(), convey.ShouldNotContainSubstring, "secret")
		convey.So(strings.Join(os.Args, " "), convey.ShouldNotContainSubstring, "secret")
	})

	convey.Convey("E2.2a: seqmeta serve returns MLWH open errors", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/warehouse")
		t.Setenv("WA_MLWH_CACHE_PATH", t.TempDir()+"/mlwh.sqlite")

		originalOpenMLWH := openSeqmetaMLWHClient
		defer func() { openSeqmetaMLWHClient = originalOpenMLWH }()

		openSeqmetaMLWHClient = func(_ context.Context, _ mlwh.Config) (*mlwh.Client, error) {
			return nil, errors.New("boom")
		}

		_, _, err := executeSeqmetaCommand(t, []string{"seqmeta", "serve", "--port", "0", "--db", t.TempDir() + "/seqmeta.db"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "boom")
	})

	convey.Convey("E2.3: seqmeta serve rejects password-bearing --mlwh-cache DSNs", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/warehouse")

		_, stderr, err := executeSeqmetaCommand(t, []string{"seqmeta", "serve", "--mlwh-cache", "user:pass@tcp(localhost:3306)/wa_cache"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, mlwh.ErrPasswordInDSN), convey.ShouldBeTrue)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "--mlwh-cache")
	})

	convey.Convey("E2.4: seqmeta serve rejects the removed --mlwh-sync-interval flag", t, func() {
		setSeqmetaMLWHEnvForTest(t)

		_, stderr, err := executeSeqmetaCommand(t, []string{"seqmeta", "serve", "--mlwh-sync-interval", "5m"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "unknown flag: --mlwh-sync-interval")
	})
}

type seqmetaMockProvider struct {
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

func (m *seqmetaMockProvider) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if m != nil && m.queryContextFunc != nil {
		return m.queryContextFunc(ctx, query, args...)
	}

	return nil, nil
}

func (m *seqmetaMockProvider) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.classifyIdentifierFunc != nil {
		return m.classifyIdentifierFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *seqmetaMockProvider) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.resolveSampleFunc != nil {
		return m.resolveSampleFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *seqmetaMockProvider) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.resolveStudyFunc != nil {
		return m.resolveStudyFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *seqmetaMockProvider) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.resolveRunFunc != nil {
		return m.resolveRunFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *seqmetaMockProvider) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.resolveLibraryFunc != nil {
		return m.resolveLibraryFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *seqmetaMockProvider) AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error) {
	if m != nil && m.allStudiesFunc != nil {
		return m.allStudiesFunc(ctx, limit, offset)
	}

	return nil, nil
}

func (m *seqmetaMockProvider) GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error) {
	if m != nil && m.getStudyFunc != nil {
		return m.getStudyFunc(ctx, identifier)
	}

	return nil, nil
}

func (m *seqmetaMockProvider) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.samplesForStudyFunc != nil {
		return m.samplesForStudyFunc(ctx, studyLimsID, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error) {
	if m != nil && m.allSamplesForStudyFunc != nil {
		return m.allSamplesForStudyFunc(ctx, studyLimsID)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesBySangerIDFunc != nil {
		return m.findSamplesBySangerIDFunc(ctx, sangerID)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesByIDSampleLimsFunc != nil {
		return m.findSamplesByIDSampleLimsFunc(ctx, idSampleLims)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesByRunIDFunc != nil {
		return m.findSamplesByRunIDFunc(ctx, idRun)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesByLibraryTypeFunc != nil {
		return m.findSamplesByLibraryTypeFunc(ctx, libraryType)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error) {
	if m != nil && m.findSamplesByAccessionFunc != nil {
		return m.findSamplesByAccessionFunc(ctx, accessionNumber)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.samplesForRunFunc != nil {
		return m.samplesForRunFunc(ctx, idRun, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.samplesForLibraryTypeFunc != nil {
		return m.samplesForLibraryTypeFunc(ctx, pipelineIDLims, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.samplesForLibraryFunc != nil {
		return m.samplesForLibraryFunc(ctx, pipelineIDLims, studyLimsID, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *seqmetaMockProvider) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error) {
	if m != nil && m.librariesForStudyFunc != nil {
		return m.librariesForStudyFunc(ctx, studyLimsID, limit, offset)
	}

	return nil, nil
}

func (m *seqmetaMockProvider) StudiesForSample(ctx context.Context, sangerName string) ([]mlwh.Study, error) {
	if m != nil && m.studiesForSampleFunc != nil {
		return m.studiesForSampleFunc(ctx, sangerName)
	}

	return nil, nil
}

func (m *seqmetaMockProvider) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	if m != nil && m.lanesForSampleFunc != nil {
		return m.lanesForSampleFunc(ctx, sangerName, limit, offset)
	}

	return []mlwh.Lane{}, nil
}

func (m *seqmetaMockProvider) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	if m != nil && m.irodsPathsForSampleFunc != nil {
		return m.irodsPathsForSampleFunc(ctx, sangerName, limit, offset)
	}

	return []mlwh.IRODSPath{}, nil
}

func (m *seqmetaMockProvider) GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
	if m != nil && m.getSampleFilesFunc != nil {
		return m.getSampleFilesFunc(ctx, sangerName)
	}

	return []mlwh.IRODSPath{}, nil
}

func TestSeqmetaServeHelpFlags(t *testing.T) {
	stdout, _, err := executeSeqmetaCommand(t, []string{"seqmeta", "serve", "--help"})
	if err != nil {
		t.Fatalf("seqmeta serve --help: %v", err)
	}

	convey.Convey("E2.1: seqmeta serve help shows MLWH cache flags and hides removed or legacy flags", t, func() {
		convey.So(stdout.String(), convey.ShouldContainSubstring, "--mlwh-cache")
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, "--mlwh-sync-interval")
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, "--token")
		convey.So(stdout.String(), convey.ShouldNotContainSubstring, "--base-url")
	})
}

type seqmetaAdapterSourceStub struct{}

func (seqmetaAdapterSourceStub) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
	return nil, errors.New("unexpected source query")
}

func openSeqmetaAdapterForTest(t *testing.T) (*seqmetaMLWHClientAdapter, *sql.DB) {
	t.Helper()

	cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
	client, err := mlwh.Open(context.Background(), mlwh.Config{
		Cache:  mlwh.CacheConfig{Path: cachePath},
		Source: seqmetaAdapterSourceStub{},
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

	return &seqmetaMLWHClientAdapter{client: client}, db
}

func seedSeqmetaSyncState(t *testing.T, db *sql.DB, tableName string) {
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

func seedSeqmetaSample(t *testing.T, db *sql.DB, sample mlwh.Sample) {
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

func seedSeqmetaLibrarySample(t *testing.T, db *sql.DB, pipelineIDLims string, idSampleTmp int64, studyLimsID string) {
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

func TestSeqmetaMLWHClientAdapterFindSamplesBySangerIDForwardsDirectly(t *testing.T) {
	adapter, db := openSeqmetaAdapterForTest(t)
	seedSeqmetaSyncState(t, db, "sample")
	seedSeqmetaSample(t, db, mlwh.Sample{
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

func TestSeqmetaMLWHClientAdapterFindSamplesByIDSampleLimsForwardsDirectly(t *testing.T) {
	adapter, db := openSeqmetaAdapterForTest(t)
	seedSeqmetaSyncState(t, db, "sample")
	seedSeqmetaSample(t, db, mlwh.Sample{
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

func TestSeqmetaMLWHClientAdapterFindSamplesByAccessionNumberForwardsDirectly(t *testing.T) {
	adapter, db := openSeqmetaAdapterForTest(t)
	seedSeqmetaSyncState(t, db, "sample")
	seedSeqmetaSample(t, db, mlwh.Sample{
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

func TestSeqmetaMLWHClientAdapterFindSamplesByLibraryTypeForwardsDirectly(t *testing.T) {
	adapter, db := openSeqmetaAdapterForTest(t)
	seedSeqmetaSyncState(t, db, "iseq_flowcell")
	seedSeqmetaSample(t, db, mlwh.Sample{
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
	seedSeqmetaSample(t, db, mlwh.Sample{
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
	seedSeqmetaLibrarySample(t, db, "LIBTYPE-1", 4, "6568")
	seedSeqmetaLibrarySample(t, db, "LIBTYPE-1", 5, "7777")

	convey.Convey("FindSamplesByLibraryType forwards to the dedicated mlwh finder", t, func() {
		samples, err := adapter.FindSamplesByLibraryType(context.Background(), "LIBTYPE-1")
		convey.So(samples, convey.ShouldBeNil)
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, mlwh.ErrAmbiguous), convey.ShouldBeTrue)
	})
}
