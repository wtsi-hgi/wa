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

import "errors"

// Exit statuses of the wa command. Any error not listed here exits with
// ExitCodeError.
const (
	// ExitCodeError is the exit status of a command that failed.
	ExitCodeError = 1

	// ExitCodeAmbiguous is the exit status of a command whose identifier names
	// several records when no single one was chosen, such as 'wa mlwh info' on
	// a supplier name shared by several samples.
	ExitCodeAmbiguous = 3
)

// exitCodeError is a command error that sets the process exit status.
type exitCodeError struct {
	err  error
	code int
}

func (e exitCodeError) Error() string {
	return e.err.Error()
}

func (e exitCodeError) Unwrap() error {
	return e.err
}

// ExitCode returns the process exit status for err, the error a command
// returned: 0 for nil, the status a command chose for its error, otherwise
// ExitCodeError.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}

	var coded exitCodeError
	if errors.As(err, &coded) {
		return coded.code
	}

	return ExitCodeError
}
