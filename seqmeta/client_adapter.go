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

// ClientAdapter wraps saga.Client to satisfy SAGAProvider.
type ClientAdapter struct {
	client *saga.Client
}

var _ SAGAProvider = (*ClientAdapter)(nil)

// NewClientAdapter creates a SAGAProvider backed by a saga.Client.
func NewClientAdapter(client *saga.Client) *ClientAdapter {
	return &ClientAdapter{client: client}
}

// GetStudy delegates to the MLWH client.
func (a *ClientAdapter) GetStudy(ctx context.Context, studyID string) (*saga.Study, error) {
	return a.client.MLWH().GetStudy(ctx, studyID)
}

// AllStudies delegates to the MLWH client.
func (a *ClientAdapter) AllStudies(ctx context.Context) ([]saga.Study, error) {
	return a.client.MLWH().AllStudies(ctx)
}

// AllSamples delegates to the MLWH client.
func (a *ClientAdapter) AllSamples(ctx context.Context) ([]saga.MLWHSample, error) {
	return a.client.MLWH().AllSamples(ctx)
}

// AllSamplesForStudy delegates to the MLWH client.
func (a *ClientAdapter) AllSamplesForStudy(ctx context.Context, studyID string) ([]saga.MLWHSample, error) {
	return a.client.MLWH().AllSamplesForStudy(ctx, studyID)
}

// GetSampleFiles delegates to the iRODS client.
func (a *ClientAdapter) GetSampleFiles(ctx context.Context, sangerID string) ([]saga.IRODSFile, error) {
	return a.client.IRODS().GetSampleFiles(ctx, sangerID)
}

// ListProjects delegates to the projects client.
func (a *ClientAdapter) ListProjects(ctx context.Context) ([]saga.Project, error) {
	return a.client.Projects().List(ctx)
}
