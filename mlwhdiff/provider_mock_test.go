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

var _ DiffSource = (*MockProvider)(nil)

// MockProvider is a configurable DiffSource test helper.
type MockProvider struct {
	mlwh.Queryer

	AllStudiesFunc          func(ctx context.Context, limit, offset int) ([]mlwh.Study, error)
	SamplesForStudyFunc     func(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
	IRODSPathsForSampleFunc func(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error)
}

func (m *MockProvider) AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error) {
	if m != nil && m.AllStudiesFunc != nil {
		return m.AllStudiesFunc(ctx, limit, offset)
	}

	return nil, nil
}

func (m *MockProvider) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.SamplesForStudyFunc != nil {
		return m.SamplesForStudyFunc(ctx, studyLimsID, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	if m != nil && m.IRODSPathsForSampleFunc != nil {
		return m.IRODSPathsForSampleFunc(ctx, sangerName, limit, offset)
	}

	return []mlwh.IRODSPath{}, nil
}
