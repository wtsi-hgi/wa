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

package mlwhdiff

import (
	"context"

	"github.com/wtsi-hgi/wa/mlwh"
)

const providerFetchLimit = 1_000_000

// DiffSource is the MLWH query surface used by mlwhdiff.
type DiffSource = mlwh.Queryer

func listAllStudies(ctx context.Context, source DiffSource) ([]mlwh.Study, error) {
	return source.AllStudies(ctx, providerFetchLimit, 0)
}

func listStudySamples(ctx context.Context, source DiffSource, studyID string) ([]mlwh.Sample, error) {
	return source.SamplesForStudy(ctx, studyID, providerFetchLimit, 0)
}

func listSampleFiles(ctx context.Context, source DiffSource, sangerName string) ([]mlwh.IRODSPath, error) {
	return source.IRODSPathsForSample(ctx, sangerName, providerFetchLimit, 0)
}
