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
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

// documentedCommandDocs are the user documents whose shell examples must
// parse against the real command tree. Paths are relative to the cmd package.
var documentedCommandDocs = []string{"../README.md", "../DEVELOPING.md"} //nolint:gochecknoglobals

// documentedVariableValue stands in for shell variables in documented
// commands. Their real values do not matter for flag parsing, and a number is
// accepted by both string and numeric flags.
func documentedVariableValue(string) string {
	return "1"
}

// documentedCommand is one wa invocation found in a shell code block.
type documentedCommand struct {
	location string
	args     []string
}

func TestDocumentedCommandsParse(t *testing.T) {
	convey.Convey("Given the wa command lines in README.md and DEVELOPING.md shell examples", t, func() {
		var commands []documentedCommand

		for _, path := range documentedCommandDocs {
			content, err := os.ReadFile(path)
			convey.So(err, convey.ShouldBeNil)

			commands = append(commands, documentedWACommands(path, string(content))...)
		}

		convey.So(len(commands), convey.ShouldBeGreaterThan, 0)

		convey.Convey("When each is parsed with --help, then every flag and subcommand is accepted", func() {
			failures := []string{}

			for _, command := range commands {
				if problem := documentedCommandProblem(t, command.args); problem != "" {
					failures = append(failures, command.location+": "+strings.Join(command.args, " ")+": "+problem)
				}
			}

			convey.So(failures, convey.ShouldBeEmpty)
		})

		convey.Convey("Then the documented results search examples are among them", func() {
			searches := 0

			for _, command := range commands {
				if strings.Contains(strings.Join(command.args, " "), "results search") {
					searches++
				}
			}

			convey.So(searches, convey.ShouldBeGreaterThan, 0)
		})
	})
}

// documentedCommandProblem returns why args do not parse against the real
// command tree, or "" if they do. Running with --help rejects unknown flags,
// but cobra treats a misspelled nested subcommand (`wa mlwh synk`) as a
// positional argument of its parent, so leftover positionals on a command
// that has subcommands are also reported.
func documentedCommandProblem(t *testing.T, args []string) string {
	t.Helper()

	if _, err := executeRootCommandForTest(t, append(args, "--help")); err != nil {
		return err.Error()
	}

	found, rest, err := NewRootCommand().Find(args)
	if err != nil {
		return err.Error()
	}

	if err = found.ParseFlags(rest); err != nil {
		return err.Error()
	}

	if positional := found.Flags().Args(); found.HasSubCommands() && len(positional) > 0 {
		return fmt.Sprintf("unknown subcommand %q of %q", positional[0], found.CommandPath())
	}

	return ""
}

// documentedWACommands returns the arguments of every `wa` or `./wa` command
// line inside bash/sh/shell fenced code blocks, joining backslash
// continuations. A command piped into another program keeps the rest of the
// pipeline as arguments, so a pipe followed by flags fails loudly as unknown
// flags rather than being silently accepted. Lines that start with an
// environment assignment (`FOO=1 wa ...`) are not recognised and are skipped.
func documentedWACommands(path, content string) []documentedCommand {
	var (
		commands []documentedCommand
		inShell  bool
		inFence  bool
		pending  string
		startAt  int
	)

	for index, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			language := strings.TrimPrefix(trimmed, "```")
			inShell = inFence && (language == "bash" || language == "sh" || language == "shell")
			pending = ""

			continue
		}

		if !inShell {
			continue
		}

		if pending == "" {
			startAt = index + 1
		}

		pending += strings.TrimSuffix(trimmed, "\\") + " "
		if strings.HasSuffix(trimmed, "\\") {
			continue
		}

		words := shellWords(os.Expand(pending, documentedVariableValue))
		pending = ""

		if len(words) > 1 && (words[0] == "wa" || words[0] == "./wa") {
			commands = append(commands, documentedCommand{
				location: path + ":" + strconv.Itoa(startAt),
				args:     words[1:],
			})
		}
	}

	return commands
}

// shellWords splits a documentation command line into words, honouring single
// and double quotes and dropping a trailing comment. It does not expand
// variables; that is unnecessary for flag parsing.
func shellWords(line string) []string {
	var (
		words   []string
		current strings.Builder
		quote   rune
		inWord  bool
	)

	for _, char := range line {
		switch {
		case quote != 0 && char == quote:
			quote = 0
		case quote != 0:
			current.WriteRune(char)
		case char == '\'' || char == '"':
			quote = char
			inWord = true
		case char == '#' && !inWord:
			return words
		case char == ' ' || char == '\t':
			if inWord {
				words = append(words, current.String())
				current.Reset()
				inWord = false
			}
		default:
			current.WriteRune(char)
			inWord = true
		}
	}

	if inWord {
		words = append(words, current.String())
	}

	return words
}
