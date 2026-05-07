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
	"database/sql"

	"github.com/wtsi-hgi/wa/mlwh"
)

var _ Provider = (*MockProvider)(nil)

// MockProvider is a configurable Provider test helper.
type MockProvider struct {
	QueryContextFunc        func(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ClassifyIdentifierFunc  func(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveSampleFunc       func(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveStudyFunc        func(ctx context.Context, raw string, options ...mlwh.ResolveStudyOption) (mlwh.Match, error)
	ResolveRunFunc          func(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveLibraryFunc      func(ctx context.Context, raw string) (mlwh.Match, error)
	StudyDetailFunc         func(ctx context.Context, studyLimsID string) (*mlwh.StudyDetail, error)
	SampleDetailFunc        func(ctx context.Context, sampleName string) (*mlwh.SampleDetail, error)
	RunDetailFunc           func(ctx context.Context, runID string) (*mlwh.RunDetail, error)
	AllStudiesFunc          func(ctx context.Context, limit, offset int) ([]mlwh.Study, error)
	GetStudyFunc            func(ctx context.Context, identifier string) (*mlwh.Study, error)
	SamplesForStudyFunc     func(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
	AllSamplesForStudyFunc  func(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error)
	FindSamplesBySangerIDFn func(ctx context.Context, sangerID string) ([]mlwh.Sample, error)
	FindSamplesByIDSampleLimsFn func(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error)
	FindSamplesByRunIDFn        func(ctx context.Context, idRun int) ([]mlwh.Sample, error)
	FindSamplesByLibraryTypeFn  func(ctx context.Context, libraryType string) ([]mlwh.Sample, error)
	FindSamplesByAccessionNumberFn func(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error)
	SamplesForRunFunc       func(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error)
	SamplesForLibraryFunc   func(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
	LibrariesForStudyFunc   func(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error)
	StudyForSampleFunc      func(ctx context.Context, sangerName string) (*mlwh.Study, error)
	LanesForSampleFunc      func(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error)
	IRODSPathsForSampleFunc func(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error)
	GetSampleFilesFunc      func(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error)
}

func (m *MockProvider) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if m != nil && m.QueryContextFunc != nil {
		return m.QueryContextFunc(ctx, query, args...)
	}

	return nil, nil
}

func (m *MockProvider) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.ClassifyIdentifierFunc != nil {
		return m.ClassifyIdentifierFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *MockProvider) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.ResolveSampleFunc != nil {
		return m.ResolveSampleFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *MockProvider) ResolveStudy(ctx context.Context, raw string, options ...mlwh.ResolveStudyOption) (mlwh.Match, error) {
	if m != nil && m.ResolveStudyFunc != nil {
		return m.ResolveStudyFunc(ctx, raw, options...)
	}

	return mlwh.Match{}, nil
}

func (m *MockProvider) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.ResolveRunFunc != nil {
		return m.ResolveRunFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *MockProvider) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	if m != nil && m.ResolveLibraryFunc != nil {
		return m.ResolveLibraryFunc(ctx, raw)
	}

	return mlwh.Match{}, nil
}

func (m *MockProvider) StudyDetail(ctx context.Context, studyLimsID string) (*mlwh.StudyDetail, error) {
	if m != nil && m.StudyDetailFunc != nil {
		return m.StudyDetailFunc(ctx, studyLimsID)
	}

	return nil, nil
}

func (m *MockProvider) SampleDetail(ctx context.Context, sampleName string) (*mlwh.SampleDetail, error) {
	if m != nil && m.SampleDetailFunc != nil {
		return m.SampleDetailFunc(ctx, sampleName)
	}

	return nil, nil
}

func (m *MockProvider) RunDetail(ctx context.Context, runID string) (*mlwh.RunDetail, error) {
	if m != nil && m.RunDetailFunc != nil {
		return m.RunDetailFunc(ctx, runID)
	}

	return nil, nil
}

func (m *MockProvider) AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error) {
	if m != nil && m.AllStudiesFunc != nil {
		return m.AllStudiesFunc(ctx, limit, offset)
	}

	return nil, nil
}

func (m *MockProvider) GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error) {
	if m != nil && m.GetStudyFunc != nil {
		return m.GetStudyFunc(ctx, identifier)
	}

	return nil, nil
}

func (m *MockProvider) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.SamplesForStudyFunc != nil {
		return m.SamplesForStudyFunc(ctx, studyLimsID, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error) {
	if m != nil && m.AllSamplesForStudyFunc != nil {
		return m.AllSamplesForStudyFunc(ctx, studyLimsID)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error) {
	if m != nil && m.FindSamplesBySangerIDFn != nil {
		return m.FindSamplesBySangerIDFn(ctx, sangerID)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error) {
	if m != nil && m.FindSamplesByIDSampleLimsFn != nil {
		return m.FindSamplesByIDSampleLimsFn(ctx, idSampleLims)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error) {
	if m != nil && m.FindSamplesByRunIDFn != nil {
		return m.FindSamplesByRunIDFn(ctx, idRun)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]mlwh.Sample, error) {
	if m != nil && m.FindSamplesByLibraryTypeFn != nil {
		return m.FindSamplesByLibraryTypeFn(ctx, libraryType)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error) {
	if m != nil && m.FindSamplesByAccessionNumberFn != nil {
		return m.FindSamplesByAccessionNumberFn(ctx, accessionNumber)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.SamplesForRunFunc != nil {
		return m.SamplesForRunFunc(ctx, idRun, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	if m != nil && m.SamplesForLibraryFunc != nil {
		return m.SamplesForLibraryFunc(ctx, pipelineIDLims, studyLimsID, limit, offset)
	}

	return []mlwh.Sample{}, nil
}

func (m *MockProvider) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error) {
	if m != nil && m.LibrariesForStudyFunc != nil {
		return m.LibrariesForStudyFunc(ctx, studyLimsID, limit, offset)
	}

	return nil, nil
}

func (m *MockProvider) StudyForSample(ctx context.Context, sangerName string) (*mlwh.Study, error) {
	if m != nil && m.StudyForSampleFunc != nil {
		return m.StudyForSampleFunc(ctx, sangerName)
	}

	return nil, nil
}

func (m *MockProvider) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	if m != nil && m.LanesForSampleFunc != nil {
		return m.LanesForSampleFunc(ctx, sangerName, limit, offset)
	}

	return []mlwh.Lane{}, nil
}

func (m *MockProvider) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	if m != nil && m.IRODSPathsForSampleFunc != nil {
		return m.IRODSPathsForSampleFunc(ctx, sangerName, limit, offset)
	}

	return []mlwh.IRODSPath{}, nil
}

func (m *MockProvider) GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
	if m != nil && m.GetSampleFilesFunc != nil {
		return m.GetSampleFilesFunc(ctx, sangerName)
	}

	return []mlwh.IRODSPath{}, nil
}
