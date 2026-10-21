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
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

const waDevelBuildVersion = "devel"

// Version is set at build time by the Makefile, run-dev.sh and the Dockerfile:
// go build -ldflags "-X github.com/wtsi-hgi/wa/cmd.Version=`git describe --tags --always --long --dirty`" .
var Version string

// newVersionCommand builds `wa version`, which prints waBuildVersion().
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print wa version",
		Long:  `Print wa version.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), waBuildVersion())

			return err
		},
	}
}

// waBuildVersion returns the version `wa version` prints and wa sends in its
// MLWH User-Agent: Version when set at build time, else the module version
// from the binary's build info.
func waBuildVersion() string {
	return resolveWABuildVersion(Version, debug.ReadBuildInfo)
}

// resolveWABuildVersion returns stamped if not empty, else readBuildInfo's
// Main.Version, or "devel" when build info is missing, empty, or "(devel)".
func resolveWABuildVersion(stamped string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if stamped != "" {
		return stamped
	}

	info, ok := readBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return waDevelBuildVersion
	}

	return info.Main.Version
}
