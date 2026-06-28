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

package mlwh

import "database/sql"

// Canonical QC verdict strings for a single product's mirrored overall qc
// value, per the QC string rule (1 -> pass, 0 -> fail, NULL -> pending).
const (
	// qcPass is the QC verdict for a product whose overall qc is 1.
	qcPass = "pass"
	// qcFail is the QC verdict for a product whose overall qc is 0.
	qcFail = "fail"
	// qcPending is the QC verdict for a product whose overall qc is SQL NULL
	// (not yet decided), distinct from a fail.
	qcPending = "pending"
)

// qcString maps a single product's mirrored overall qc value to its canonical
// verdict string per the QC string rule: 1 -> pass, 0 -> fail, NULL -> pending.
// NULL must reach here as an invalid sql.NullInt64 (never coerced to 0), so a
// pending product stays distinct from a fail.
func qcString(qc sql.NullInt64) string {
	if !qc.Valid {
		return qcPending
	}

	if qc.Int64 == 1 {
		return qcPass
	}

	return qcFail
}
