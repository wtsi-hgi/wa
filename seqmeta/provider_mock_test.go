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

var _ SAGAProvider = (*MockProvider)(nil)

// MockProvider is a configurable SAGAProvider test helper.
type MockProvider struct {
	GetStudyFunc           func(ctx context.Context, studyID string) (*saga.Study, error)
	AllStudiesFunc         func(ctx context.Context) ([]saga.Study, error)
	AllSamplesFunc         func(ctx context.Context) ([]saga.MLWHSample, error)
	AllSamplesForStudyFunc func(ctx context.Context, studyID string) ([]saga.MLWHSample, error)
	GetSampleFilesFunc     func(ctx context.Context, sangerID string) ([]saga.IRODSFile, error)
	ListProjectsFunc       func(ctx context.Context) ([]saga.Project, error)
}

func (m *MockProvider) GetStudy(ctx context.Context, studyID string) (*saga.Study, error) {
	if m != nil && m.GetStudyFunc != nil {
		return m.GetStudyFunc(ctx, studyID)
	}

	return nil, nil
}

func (m *MockProvider) AllStudies(ctx context.Context) ([]saga.Study, error) {
	if m != nil && m.AllStudiesFunc != nil {
		return m.AllStudiesFunc(ctx)
	}

	return nil, nil
}

func (m *MockProvider) AllSamples(ctx context.Context) ([]saga.MLWHSample, error) {
	if m != nil && m.AllSamplesFunc != nil {
		return m.AllSamplesFunc(ctx)
	}

	return nil, nil
}

func (m *MockProvider) AllSamplesForStudy(ctx context.Context, studyID string) ([]saga.MLWHSample, error) {
	if m != nil && m.AllSamplesForStudyFunc != nil {
		return m.AllSamplesForStudyFunc(ctx, studyID)
	}

	return nil, nil
}

func (m *MockProvider) GetSampleFiles(ctx context.Context, sangerID string) ([]saga.IRODSFile, error) {
	if m != nil && m.GetSampleFilesFunc != nil {
		return m.GetSampleFilesFunc(ctx, sangerID)
	}

	return nil, nil
}

func (m *MockProvider) ListProjects(ctx context.Context) ([]saga.Project, error) {
	if m != nil && m.ListProjectsFunc != nil {
		return m.ListProjectsFunc(ctx)
	}

	return nil, nil
}
