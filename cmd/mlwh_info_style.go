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
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// infoFallbackWidth is the panel width used when output is not a terminal (tests
// and pipes). Keeping it fixed makes the rendered text fully deterministic.
const infoFallbackWidth = 100

// ANSI select-graphic-rendition codes used by the info renderer. They are only
// emitted when infoStyle.colour is true.
const (
	ansiReset     = "\x1b[0m"
	ansiBold      = "\x1b[1m"
	ansiDim       = "\x1b[2m"
	ansiCyan      = "\x1b[36m"
	ansiGreen     = "\x1b[32m"
	ansiRed       = "\x1b[31m"
	ansiYellow    = "\x1b[33m"
	ansiBoldCyan  = "\x1b[1;36m"
	ansiBoldWhite = "\x1b[1;37m"
)

// infoStyle holds the rendering configuration for `wa mlwh info` text output:
// whether to emit ANSI colour and the panel width to wrap/align within. It is
// computed once per invocation (resolveInfoStyle) and threaded through the
// renderer so the same layout is produced with or without colour.
type infoStyle struct {
	colour bool
	width  int
}

// resolveInfoStyle decides colour and width once, from the command's output
// writer and the environment. Colour is enabled only when out is a real
// terminal, with the usual overrides: NO_COLOR (any value) forces it off and
// CLICOLOR_FORCE (any value) forces it on even when not a tty. Width comes from
// the terminal size when out is a tty, else the fixed infoFallbackWidth so
// non-tty output (tests, pipes) is deterministic.
func resolveInfoStyle(out io.Writer) infoStyle {
	style := infoStyle{width: infoFallbackWidth}

	file, isFile := out.(*os.File)
	isTTY := isFile && term.IsTerminal(int(file.Fd()))

	if isTTY {
		if width, _, err := term.GetSize(int(file.Fd())); err == nil && width > 0 {
			style.width = width
		}
	}

	style.colour = isTTY
	if _, forced := os.LookupEnv("CLICOLOR_FORCE"); forced {
		style.colour = true
	}
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		style.colour = false
	}

	return style
}

// wrap returns text wrapped in the given ANSI code (with a reset) when colour is
// enabled, else the text unchanged.
func (s infoStyle) wrap(code, text string) string {
	if !s.colour || code == "" {
		return text
	}

	return code + text + ansiReset
}

// bold renders a strong primary value.
func (s infoStyle) bold(text string) string { return s.wrap(ansiBold, text) }

// dim renders secondary/auxiliary text.
func (s infoStyle) dim(text string) string { return s.wrap(ansiDim, text) }

// warn renders a warning that should stand out rather than recede.
func (s infoStyle) warn(text string) string { return s.wrap(ansiYellow, text) }

// header renders a panel title.
func (s infoStyle) header(text string) string { return s.wrap(ansiBoldCyan, text) }

// label renders a field label.
func (s infoStyle) label(text string) string { return s.wrap(ansiBoldWhite, text) }

// section renders a sub-section heading within a panel.
func (s infoStyle) section(text string) string { return s.wrap(ansiCyan, text) }

// qcColour colours a QC verdict by its meaning: pass green, fail red,
// pending/other yellow, not_tracked dim. text is what to display (so a longer
// label can be coloured by the bare verdict).
func (s infoStyle) qcColour(verdict, text string) string {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "pass":
		return s.wrap(ansiGreen, text)
	case "fail":
		return s.wrap(ansiRed, text)
	case "not_tracked", "":
		return s.wrap(ansiDim, text)
	default:
		return s.wrap(ansiYellow, text)
	}
}
