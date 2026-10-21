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
	"runtime/debug"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

// setVersionForTest sets Version as -ldflags "-X .../cmd.Version=..." would,
// restoring it when the test ends.
func setVersionForTest(t *testing.T, version string) {
	t.Helper()

	original := Version
	Version = version

	t.Cleanup(func() { Version = original })
}

func TestVersionCommand(t *testing.T) {
	convey.Convey("Given Version set at build time, when wa version runs, then it prints exactly that version and a newline", t, func() {
		setVersionForTest(t, "v1.0.0-0-g803a7ae")

		stdout := &bytes.Buffer{}
		command := NewRootCommand()
		command.SetOut(stdout)
		command.SetErr(&bytes.Buffer{})
		command.SetArgs([]string{"version"})

		convey.So(command.Execute(), convey.ShouldBeNil)
		convey.So(stdout.String(), convey.ShouldEqual, "v1.0.0-0-g803a7ae\n")

		convey.Convey("Then the MLWH User-Agents carry the same version", func() {
			convey.So(mlwhCLIRemoteConfig("http://x").UserAgent, convey.ShouldEqual, "wa-cli/v1.0.0-0-g803a7ae")
			convey.So(mlwhServerRemoteConfig("http://x", mlwh.UserAgentProductResultsServer).UserAgent,
				convey.ShouldEqual, "wa-results-server/v1.0.0-0-g803a7ae")

			_, version, counted := mlwh.ParseUsageUserAgent(mlwhCLIRemoteConfig("http://x").UserAgent)
			convey.So(counted, convey.ShouldBeTrue)
			convey.So(version, convey.ShouldEqual, "v1.0.0-0-g803a7ae")
		})
	})

	convey.Convey("Given Version is not set, when wa version runs, then it prints the build info fallback", t, func() {
		setVersionForTest(t, "")

		output, err := executeRootCommandForTest(t, []string{"version"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldEqual, resolveWABuildVersion("", debug.ReadBuildInfo))
		convey.So(output, convey.ShouldNotBeEmpty)
	})

	convey.Convey("Given wa version with an argument, then it is a usage error", t, func() {
		output, err := executeRootCommandForTest(t, []string{"version", "extra"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Usage:\n  wa version")
	})
}

func TestResolveWABuildVersion(t *testing.T) {
	buildInfo := func(version string, ok bool) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			if !ok {
				return nil, false
			}

			return &debug.BuildInfo{Main: debug.Module{Path: "github.com/wtsi-hgi/wa", Version: version}}, true
		}
	}

	convey.Convey("Given a build-time Version, then it wins over the build info", t, func() {
		convey.So(resolveWABuildVersion("v1.0.0-3-gabc1234-dirty", buildInfo("v1.0.0", true)),
			convey.ShouldEqual, "v1.0.0-3-gabc1234-dirty")
	})

	convey.Convey("Given no build-time Version, then the build info module version is used", t, func() {
		convey.So(resolveWABuildVersion("", buildInfo("v1.0.0", true)), convey.ShouldEqual, "v1.0.0")
		convey.So(resolveWABuildVersion("", buildInfo("v1.0.1-0.20261021120000-803a7ae1b2c3+dirty", true)),
			convey.ShouldEqual, "v1.0.1-0.20261021120000-803a7ae1b2c3+dirty")
	})

	convey.Convey("Given no build-time Version and no usable build info, then it is devel", t, func() {
		convey.So(resolveWABuildVersion("", buildInfo("(devel)", true)), convey.ShouldEqual, waDevelBuildVersion)
		convey.So(resolveWABuildVersion("", buildInfo("", true)), convey.ShouldEqual, waDevelBuildVersion)
		convey.So(resolveWABuildVersion("", buildInfo("", false)), convey.ShouldEqual, waDevelBuildVersion)
	})
}
