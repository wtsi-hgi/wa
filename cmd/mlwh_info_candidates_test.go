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
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

const infoChoicePromptForTest = "Choose a sample [1-4] (Enter to cancel): "

type infoCandidatesJSONForTest struct {
	Query    infoQueryJSONForTest             `json:"query"`
	Results  infoCandidatesResultsJSONForTest `json:"results"`
	Warnings []string                         `json:"warnings"`
}

type infoQueryJSONForTest struct {
	Identifier    string `json:"identifier"`
	Type          string `json:"type"`
	CacheSyncedAt string `json:"cache_synced_at"`
	WAVersion     string `json:"wa_version"`
}

type infoCandidatesResultsJSONForTest struct {
	Kind       string `json:"kind"`
	Candidates []struct {
		Identifier      string `json:"identifier"`
		Name            string `json:"name"`
		SangerSampleID  string `json:"sanger_sample_id"`
		SupplierName    string `json:"supplier_name"`
		AccessionNumber string `json:"accession_number"`
		HasData         bool   `json:"has_data"`
		Studies         []struct {
			IDStudyLims string `json:"id_study_lims"`
			Name        string `json:"name"`
		} `json:"studies"`
	} `json:"candidates"`
}

func TestMLWHInfoInteractiveSampleChoice(t *testing.T) {
	convey.Convey("Given a supplier name shared by four samples behind wa mlwh serve and a terminal", t, func() {
		clearMLWHClientEnvForTest(t)
		serverURL := startMLWHExportServerForCachePathForTest(t, newMultiSampleCachePathForTest(t))
		infoArgs := []string{"mlwh", "info", multiSampleSupplierForTest, "--server", serverURL}

		chosenInfo, _, err := executeInfoCommandForTest(t, []string{"mlwh", "info", "5534STDY0000102", "--server", serverURL}, strings.NewReader(""), false)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when the user picks the second sample, then it prints the numbered list, the prompt and exactly the info output for that sample", func() {
			stdout, stderr, err := executeInfoCommandForTest(t, infoArgs, strings.NewReader("2\n"), true)

			convey.So(err, convey.ShouldBeNil)
			convey.So(ExitCode(err), convey.ShouldEqual, 0)
			convey.So(stderr, convey.ShouldBeEmpty)
			list, info, found := strings.Cut(stdout, infoChoicePromptForTest)
			convey.So(found, convey.ShouldBeTrue)
			convey.So(info, convey.ShouldEqual, chosenInfo)
			convey.So(infoWordsForTest(list), convey.ShouldContainSubstring, "# name sanger_sample_id supplier_name data studies\n1 5534STDY0000101 5534STDY0000101 PD42487a no 5534 Study 5534\n")
			convey.So(infoWordsForTest(list), convey.ShouldContainSubstring, "\n2 5534STDY0000102 5534STDY0000102 PD42487a yes 5534 Study 5534\n")
			convey.So(infoWordsForTest(list), convey.ShouldContainSubstring, "\n4 5534STDY0000104 5534STDY0000104 PD42487a yes 5534 Study 5534; 7000 Study 7000\n")
			convey.So(stdout, convey.ShouldNotContainSubstring, "wa mlwh")
		})

		convey.Convey("when the user first types invalid choices, then it re-prompts and shows the chosen sample", func() {
			stdout, _, err := executeInfoCommandForTest(t, infoArgs, strings.NewReader("x\n9\n2\n"), true)

			convey.So(err, convey.ShouldBeNil)
			convey.So(strings.Count(stdout, infoChoicePromptForTest), convey.ShouldEqual, 3)
			convey.So(stdout, convey.ShouldContainSubstring, `"x" is not a number from 1 to 4`)
			convey.So(stdout, convey.ShouldContainSubstring, `"9" is not a number from 1 to 4`)
			convey.So(stdout, convey.ShouldEndWith, infoChoicePromptForTest+chosenInfo)
		})

		convey.Convey("when every allowed attempt is invalid, then it gives up with the ambiguous exit code", func() {
			stdout, stderr, err := executeInfoCommandForTest(t, infoArgs, strings.NewReader("0\n5\nfoo\n2\n"), true)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
			convey.So(strings.Count(stdout, infoChoicePromptForTest), convey.ShouldEqual, 3)
			convey.So(stdout, convey.ShouldNotContainSubstring, "Sample  5534STDY0000102")
			convey.So(stderr, convey.ShouldContainSubstring, "no sample chosen")
		})

		for _, answer := range []struct{ name, input string }{{"an empty line", "\n"}, {"end of input", ""}} {
			convey.Convey("when the user answers with "+answer.name+", then it cancels with the ambiguous exit code and a short message", func() {
				stdout, stderr, err := executeInfoCommandForTest(t, infoArgs, strings.NewReader(answer.input), true)

				convey.So(err, convey.ShouldNotBeNil)
				convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
				convey.So(strings.Count(stdout, infoChoicePromptForTest), convey.ShouldEqual, 1)
				convey.So(stderr, convey.ShouldEqual, "Error: \"PD42487a\" matches 4 samples; no sample chosen\n")
			})
		}

		convey.Convey("when the user interrupts while the prompt waits for input that never comes, then it ends promptly with the ambiguous exit code", func() {
			stdin := newBlockingStdinForTest(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				<-stdin.reading
				cancel()
			}()

			type outcome struct {
				stdout, stderr string
				err            error
			}
			done := make(chan outcome, 1)
			go func() {
				stdout, stderr, err := executeInfoCommandContextForTest(ctx, t, infoArgs, stdin, true)
				done <- outcome{stdout, stderr, err}
			}()

			var result outcome
			select {
			case result = <-done:
			case <-time.After(10 * time.Second):
			}

			convey.So(result.err, convey.ShouldNotBeNil)
			convey.So(ExitCode(result.err), convey.ShouldEqual, ExitCodeAmbiguous)
			convey.So(result.stdout, convey.ShouldEndWith, infoChoicePromptForTest+"\n")
			convey.So(result.stderr, convey.ShouldEqual, "Error: \"PD42487a\" matches 4 samples; no sample chosen\n")
		})

		convey.Convey("when --json is given, then it does not prompt and prints the candidates object", func() {
			stdout, _, err := executeInfoCommandForTest(t, append(infoArgs, "--json"), strings.NewReader("2\n"), true)

			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
			var report infoCandidatesJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.Results.Candidates, convey.ShouldHaveLength, 4)
		})

		convey.Convey("when the identifier names one sample, then it prints that sample without prompting", func() {
			stdout, stderr, err := executeInfoCommandForTest(t, []string{"mlwh", "info", "5534STDY0000102", "--server", serverURL}, strings.NewReader(""), true)

			convey.So(err, convey.ShouldBeNil)
			convey.So(stderr, convey.ShouldBeEmpty)
			convey.So(stdout, convey.ShouldEqual, chosenInfo)
		})
	})
}

// executeInfoCommandForTest runs the root command with stdin as its input and
// treats stdin and stdout as terminals when terminal is true.
func executeInfoCommandForTest(t *testing.T, args []string, stdin io.Reader, terminal bool) (string, string, error) {
	t.Helper()

	return executeInfoCommandContextForTest(context.Background(), t, args, stdin, terminal)
}

// executeInfoCommandContextForTest is executeInfoCommandForTest with ctx as the
// command context, as main.go's interrupt-aware context is.
func executeInfoCommandContextForTest(ctx context.Context, t *testing.T, args []string, stdin io.Reader, terminal bool) (string, string, error) {
	t.Helper()

	previous := infoPromptAllowed
	infoPromptAllowed = func(io.Reader, io.Writer) bool { return terminal }
	t.Cleanup(func() { infoPromptAllowed = previous })

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command := NewRootCommand()
	command.SetIn(stdin)
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs(args)

	err := command.ExecuteContext(ctx)

	return stdout.String(), stderr.String(), err
}

// infoWordsForTest collapses the column padding of text output to single
// spaces, so table rows can be asserted without fixing column widths.
func infoWordsForTest(text string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.Join(strings.Fields(line), " ")
	}

	return strings.Join(lines, "\n")
}

func newBlockingStdinForTest(t *testing.T) *blockingStdinForTest {
	t.Helper()

	stdin := &blockingStdinForTest{reading: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { close(stdin.release) })

	return stdin
}

func TestMLWHInfoNonInteractiveSampleList(t *testing.T) {
	convey.Convey("Given a supplier name shared by four samples behind wa mlwh serve and no terminal", t, func() {
		clearMLWHClientEnvForTest(t)
		cachePath := newMultiSampleCachePathForTest(t)
		addInfoCandidateSamplesForTest(t, cachePath)
		serverURL := startMLWHExportServerForCachePathForTest(t, cachePath)

		convey.Convey("when wa mlwh info runs, then stdout lists the candidates without a prompt or suggested commands, and stderr explains in one line", func() {
			stdout, stderr, err := executeInfoCommandForTest(t, []string{"mlwh", "info", multiSampleSupplierForTest, "--server", serverURL}, strings.NewReader("2\n"), false)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
			convey.So(stdout, convey.ShouldContainSubstring, `"PD42487a" matches 4 samples`)
			convey.So(infoWordsForTest(stdout), convey.ShouldContainSubstring, "\n1 5534STDY0000101 5534STDY0000101 PD42487a no 5534 Study 5534\n")
			convey.So(infoWordsForTest(stdout), convey.ShouldContainSubstring, "\n4 5534STDY0000104 5534STDY0000104 PD42487a yes 5534 Study 5534; 7000 Study 7000\n")
			convey.So(stdout, convey.ShouldNotContainSubstring, "5534STDY0000200")
			convey.So(stdout, convey.ShouldContainSubstring, "Cache synced")
			convey.So(stdout, convey.ShouldNotContainSubstring, "Choose a sample")
			convey.So(stdout, convey.ShouldNotContainSubstring, "wa mlwh")
			convey.So(stderr, convey.ShouldEqual, "Error: \"PD42487a\" matches 4 samples; give one of the identifiers listed\n")
		})

		convey.Convey("when wa mlwh info --json runs, then stdout is one candidates object without next commands", func() {
			stdout, _, err := executeInfoCommandForTest(t, []string{"mlwh", "info", multiSampleSupplierForTest, "--server", serverURL, "--json"}, strings.NewReader(""), false)

			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
			var raw map[string]json.RawMessage
			convey.So(json.Unmarshal([]byte(stdout), &raw), convey.ShouldBeNil)
			convey.So(raw, convey.ShouldNotContainKey, "next_commands")

			var report infoCandidatesJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.Results.Kind, convey.ShouldEqual, "ambiguous_sample")
			convey.So(report.Query, convey.ShouldResemble, infoQueryJSONForTest{
				Identifier:    multiSampleSupplierForTest,
				Type:          "auto",
				CacheSyncedAt: multiSampleSyncedAtForTest,
				WAVersion:     waBuildVersion(),
			})
			convey.So(raw, convey.ShouldHaveLength, 2)
			convey.So(raw, convey.ShouldContainKey, "query")
			var results map[string]json.RawMessage
			convey.So(json.Unmarshal(raw["results"], &results), convey.ShouldBeNil)
			convey.So(results, convey.ShouldHaveLength, 2)
			convey.So(results, convey.ShouldContainKey, "kind")
			convey.So(results, convey.ShouldContainKey, "candidates")
			convey.So(report.Results.Candidates, convey.ShouldHaveLength, 4)

			var identifiers, withData []string
			for _, candidate := range report.Results.Candidates {
				identifiers = append(identifiers, candidate.Identifier)
				if candidate.HasData {
					withData = append(withData, candidate.Name)
				}
			}
			convey.So(identifiers, convey.ShouldResemble, []string{"5534STDY0000101", "5534STDY0000102", "5534STDY0000103", "5534STDY0000104"})
			convey.So(withData, convey.ShouldResemble, []string{"5534STDY0000102", "5534STDY0000104"})
			convey.So(report.Results.Candidates[3].SupplierName, convey.ShouldEqual, multiSampleSupplierForTest)
			convey.So(report.Results.Candidates[3].AccessionNumber, convey.ShouldEqual, "EGAN0000104")
			convey.So(report.Results.Candidates[3].Studies, convey.ShouldHaveLength, 2)
		})

		convey.Convey("when the candidates' Sanger names are themselves ambiguous, then each candidate's identifier is another key that info resolves to that sample alone", func() {
			stdout, _, err := executeInfoCommandForTest(t, []string{"mlwh", "info", infoDuplicatedNameSupplierForTest, "--server", serverURL, "--json"}, strings.NewReader(""), false)

			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
			var report infoCandidatesJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.Results.Candidates, convey.ShouldHaveLength, 2)

			for _, candidate := range report.Results.Candidates {
				convey.So(candidate.Name, convey.ShouldEqual, infoDuplicatedNameForTest)
				convey.So(candidate.Identifier, convey.ShouldEqual, candidate.SangerSampleID)

				resolved, _, resolveErr := executeInfoCommandForTest(t, []string{"mlwh", "info", candidate.Identifier, "--server", serverURL, "--json"}, strings.NewReader(""), false)
				convey.So(resolveErr, convey.ShouldBeNil)
				var sample infoSampleJSONForTest
				convey.So(json.Unmarshal([]byte(resolved), &sample), convey.ShouldBeNil)
				convey.So(sample.Results.Kind, convey.ShouldEqual, "sanger_sample_id")
				convey.So(sample.Results.Sample.SangerSampleID, convey.ShouldEqual, candidate.SangerSampleID)
			}

			text, _, err := executeInfoCommandForTest(t, []string{"mlwh", "info", infoDuplicatedNameSupplierForTest, "--server", serverURL}, strings.NewReader(""), false)
			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
			convey.So(text, convey.ShouldContainSubstring, infoDuplicatedNameForTest+" (not unique; use 5534STDY0000601)")
			convey.So(text, convey.ShouldContainSubstring, infoDuplicatedNameForTest+" (not unique; use 5534STDY0000602)")
		})

		convey.Convey("when a candidate's Sanger name is unique among samples but info resolves it to a study, then its identifier is its Sanger sample id", func() {
			stdout, _, err := executeInfoCommandForTest(t, []string{"mlwh", "info", infoStudyNamedSupplierForTest, "--server", serverURL, "--json"}, strings.NewReader(""), false)

			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
			var report infoCandidatesJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			identifiers := map[string]string{}
			for _, candidate := range report.Results.Candidates {
				identifiers[candidate.Name] = candidate.Identifier
			}
			convey.So(identifiers, convey.ShouldResemble, map[string]string{"7000": "5534STDY0000701", "5534STDY0000702": "5534STDY0000702"})
		})

		convey.Convey("when an identifier is one sample's accession and others' supplier name, then all three are listed", func() {
			stdout, _, err := executeInfoCommandForTest(t, []string{"mlwh", "info", "SHARED-ACC", "--server", serverURL, "--json"}, strings.NewReader(""), false)

			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
			var report infoCandidatesJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			names := []string{}
			for _, candidate := range report.Results.Candidates {
				names = append(names, candidate.Identifier)
			}
			convey.So(names, convey.ShouldResemble, []string{"5534STDY0000300", "5534STDY0000301", "5534STDY0000302"})
		})

		convey.Convey("when an identifier is an accession two samples share, then both are listed", func() {
			stdout, _, err := executeInfoCommandForTest(t, []string{"mlwh", "info", "TWIN-ACC", "--server", serverURL, "--json"}, strings.NewReader(""), false)

			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeAmbiguous)
			var report infoCandidatesJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			names := []string{}
			for _, candidate := range report.Results.Candidates {
				names = append(names, candidate.Identifier)
			}
			convey.So(names, convey.ShouldResemble, []string{"5534STDY0000801", "5534STDY0000802"})
		})

		convey.Convey("when the identifier names one sample, then the sample report is printed and the command succeeds", func() {
			stdout, _, err := executeInfoCommandForTest(t, []string{"mlwh", "info", "5534STDY0000102", "--server", serverURL, "--json"}, strings.NewReader(""), false)

			convey.So(err, convey.ShouldBeNil)
			var sample infoSampleJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &sample), convey.ShouldBeNil)
			convey.So(sample.Results.Kind, convey.ShouldEqual, "sanger_sample_name")
			convey.So(sample.Results.Sample.Name, convey.ShouldEqual, "5534STDY0000102")
		})

		convey.Convey("when the identifier matches nothing, then it fails with the general error exit code", func() {
			_, _, err := executeInfoCommandForTest(t, []string{"mlwh", "info", "NO-SUCH-SAMPLE", "--server", serverURL}, strings.NewReader(""), false)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(ExitCode(err), convey.ShouldEqual, 1)
		})
	})
}

func TestMLWHInfoCandidateLookupTimeout(t *testing.T) {
	convey.Convey("Given candidates whose Sanger names are shared, and a server whose Sanger sample id lookups never answer", t, func() {
		clearMLWHClientEnvForTest(t)
		cachePath := newMultiSampleCachePathForTest(t)
		addInfoCandidateSamplesForTest(t, cachePath)
		serverURL := startMLWHExportServerForCachePathForTest(t, cachePath)

		remote, err := mlwh.NewRemoteClient(mlwhCLIRemoteConfig(serverURL))
		convey.So(err, convey.ShouldBeNil)
		client := slowSangerIDLookupClientForTest{RemoteClient: remote}

		previous := infoCandidatesTimeout
		infoCandidatesTimeout = 500 * time.Millisecond
		defer func() { infoCandidatesTimeout = previous }()

		convey.Convey("when info lists the candidates, then it gives up on the lookups within the timeout and warns that identifiers may be missing", func() {
			out := &bytes.Buffer{}
			done := make(chan error, 1)
			go func() {
				done <- runMLWHInfo(context.Background(), client, infoDuplicatedNameSupplierForTest, infoOptions{out: out, in: strings.NewReader(""), jsonOut: true})
			}()

			var runErr error
			select {
			case runErr = <-done:
			case <-time.After(10 * time.Second):
				runErr = errors.New("info did not finish")
			}

			convey.So(ExitCode(runErr), convey.ShouldEqual, ExitCodeAmbiguous)
			var report infoCandidatesJSONForTest
			convey.So(json.Unmarshal(out.Bytes(), &report), convey.ShouldBeNil)
			convey.So(report.Results.Candidates, convey.ShouldHaveLength, 2)
			convey.So(report.Results.Candidates[0].Identifier, convey.ShouldBeEmpty)
			convey.So(report.Warnings, convey.ShouldResemble, []string{infoCandidatesTimedOutWarning})
		})
	})
}

// blockingStdinForTest is a stdin whose Read never returns until the test
// ends, like a terminal nobody types into; reading is closed on the first Read.
type blockingStdinForTest struct {
	reading chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingStdinForTest) Read([]byte) (int, error) {
	s.once.Do(func() { close(s.reading) })
	<-s.release

	return 0, io.EOF
}

type infoSampleJSONForTest struct {
	Results struct {
		Kind   string `json:"kind"`
		Sample struct {
			Name           string `json:"name"`
			SangerSampleID string `json:"sanger_sample_id"`
		} `json:"sample"`
	} `json:"results"`
}

// slowSangerIDLookupClientForTest is a remote client whose Sanger sample id
// lookups of DUPNAME's samples wait until their context ends.
type slowSangerIDLookupClientForTest struct {
	*mlwh.RemoteClient
}

func (c slowSangerIDLookupClientForTest) FindSamplesBySangerID(ctx context.Context, value string, limit, offset int) ([]mlwh.Sample, error) {
	if strings.HasPrefix(value, "5534STDY00006") {
		<-ctx.Done()

		return nil, ctx.Err()
	}

	return c.RemoteClient.FindSamplesBySangerID(ctx, value, limit, offset)
}
