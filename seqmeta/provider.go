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

package seqmeta

import (
	"context"

	"github.com/wtsi-hgi/wa/saga"
)

// SAGAProvider is the mockable subset of saga.Client used by seqmeta.
type SAGAProvider interface {
	GetStudy(ctx context.Context, studyID string) (*saga.Study, error)
	AllStudies(ctx context.Context) ([]saga.Study, error)
	AllSamples(ctx context.Context) ([]saga.MLWHSample, error)
	AllSamplesForStudy(ctx context.Context, studyID string) ([]saga.MLWHSample, error)
	GetSampleFiles(ctx context.Context, sangerID string) ([]saga.IRODSFile, error)
	ListProjects(ctx context.Context) ([]saga.Project, error)
}
