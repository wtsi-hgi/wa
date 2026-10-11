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
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/user"
	"runtime/debug"
	"sync"

	"github.com/wtsi-hgi/wa/mlwh"
)

const (
	waDevelBuildVersion = "devel"
	mlwhCLIRunIDBytes   = 16
)

// mlwhCLIRunID is this process's CLI run ID: 16 random bytes as lowercase
// hex, made on first use.
var mlwhCLIRunID = sync.OnceValue(func() string {
	runID := make([]byte, mlwhCLIRunIDBytes)
	_, _ = rand.Read(runID) // never fails; Go crashes the process instead

	return hex.EncodeToString(runID)
})

// mlwhCLIRemoteConfig returns the RemoteConfig the wa CLI uses to reach an
// MLWH server at baseURL: the wa-cli User-Agent, this process's run ID, and
// the local username.
func mlwhCLIRemoteConfig(baseURL string) mlwh.RemoteConfig {
	return mlwh.RemoteConfig{
		BaseURL:   baseURL,
		UserAgent: mlwh.UserAgentProductCLI + "/" + waBuildVersion(),
		Identity: mlwh.ClientIdentity{
			ClientID: mlwhCLIRunID(),
			Username: mlwhCLIUsername(),
		},
	}
}

// mlwhServerRemoteConfig returns the RemoteConfig a wa server uses to reach an
// MLWH server at baseURL: product's User-Agent and no client identity, since
// the originating end user is not carried.
func mlwhServerRemoteConfig(baseURL, product string) mlwh.RemoteConfig {
	return mlwh.RemoteConfig{
		BaseURL:   baseURL,
		UserAgent: product + "/" + waBuildVersion(),
	}
}

// waBuildVersion returns debug.ReadBuildInfo().Main.Version, or "devel"
// when build info is missing, empty, or "(devel)".
func waBuildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return waDevelBuildVersion
	}

	return info.Main.Version
}

// mlwhCLIUsername returns os/user.Current().Username, else $USER, else "".
func mlwhCLIUsername() string {
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}

	return os.Getenv("USER")
}
