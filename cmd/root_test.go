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
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestNewRootCommand(t *testing.T) {
	convey.Convey("F1.2: Given seqmeta help, then help output lists diff, validate, and serve", t, func() {
		output, err := executeRootCommandForTest(t, []string{"seqmeta", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "diff")
		convey.So(output, convey.ShouldContainSubstring, "validate")
		convey.So(output, convey.ShouldContainSubstring, "serve")
	})

	convey.Convey("F1.3: Given results help, then help output lists the results subcommands", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "register")
		convey.So(output, convey.ShouldContainSubstring, "search")
		convey.So(output, convey.ShouldContainSubstring, "get")
		convey.So(output, convey.ShouldContainSubstring, "delete")
		convey.So(output, convey.ShouldContainSubstring, "rescan")
		convey.So(output, convey.ShouldContainSubstring, "serve")
	})

	convey.Convey("F1.4: Given results serve help, then help output lists the phase 5 serve flags", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "serve", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "--port")
		convey.So(output, convey.ShouldContainSubstring, "--db")
		convey.So(output, convey.ShouldContainSubstring, "--seqmeta-url")
		convey.So(output, convey.ShouldContainSubstring, "WA_SEQMETA_BACKEND_URL")
		convey.So(output, convey.ShouldContainSubstring, "--seqmeta-timeout")
	})

	convey.Convey("E4.4: Given wa with no subcommand, then help lists the surviving top-level subcommand trees", t, func() {
		output, err := executeRootCommandForTest(t, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "seqmeta")
		convey.So(output, convey.ShouldContainSubstring, "results")
		convey.So(output, convey.ShouldContainSubstring, "mlwh")
	})
}

func executeRootCommandForTest(t *testing.T, args []string) (string, error) {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command := NewRootCommand()
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs(args)

	err := command.Execute()

	return strings.TrimSpace(stdout.String() + stderr.String()), err
}
