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
	"errors"
	"strconv"

	"github.com/wtsi-hgi/wa/mlwh"
)

var _ Provider = (*ClientAdapter)(nil)

var errClientAdapterUnconfigured = errors.New("seqmeta: client adapter requires a seqmeta.Provider")

// ClientAdapter wraps a Provider implementation.
type ClientAdapter struct {
	provider Provider
}

// NewClientAdapter creates a Provider adapter when the given value already implements Provider.
func NewClientAdapter(client any) *ClientAdapter {
	provider, _ := client.(Provider)

	return &ClientAdapter{provider: provider}
}

func (a *ClientAdapter) delegate() (Provider, error) {
	if a == nil || a.provider == nil {
		return nil, errClientAdapterUnconfigured
	}

	return a.provider, nil
}

func (a *ClientAdapter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.QueryContext(ctx, query, args...)
}

func (a *ClientAdapter) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	provider, err := a.delegate()
	if err != nil {
		return mlwh.Match{}, err
	}

	return provider.ClassifyIdentifier(ctx, raw)
}

func (a *ClientAdapter) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	provider, err := a.delegate()
	if err != nil {
		return mlwh.Match{}, err
	}

	return provider.ResolveSample(ctx, raw)
}

func (a *ClientAdapter) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	provider, err := a.delegate()
	if err != nil {
		return mlwh.Match{}, err
	}

	return provider.ResolveStudy(ctx, raw)
}

func (a *ClientAdapter) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	provider, err := a.delegate()
	if err != nil {
		return mlwh.Match{}, err
	}

	return provider.ResolveRun(ctx, raw)
}

func (a *ClientAdapter) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	provider, err := a.delegate()
	if err != nil {
		return mlwh.Match{}, err
	}

	return provider.ResolveLibrary(ctx, raw)
}

func (a *ClientAdapter) AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.AllStudies(ctx, limit, offset)
}

func (a *ClientAdapter) GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return getStudy(ctx, provider, identifier)
}

func (a *ClientAdapter) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.SamplesForStudy(ctx, studyLimsID, limit, offset)
}

func (a *ClientAdapter) AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return listStudySamples(ctx, provider, studyLimsID)
}

func (a *ClientAdapter) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	match, err := provider.ResolveSample(ctx, sangerID)
	if err != nil {
		return nil, err
	}
	if match.Sample == nil {
		return []mlwh.Sample{}, nil
	}

	return []mlwh.Sample{*match.Sample}, nil
}

func (a *ClientAdapter) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error) {
	return a.FindSamplesBySangerID(ctx, idSampleLims)
}

func (a *ClientAdapter) FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.SamplesForRun(ctx, strconv.Itoa(idRun), providerFetchLimit, 0)
}

func (a *ClientAdapter) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]mlwh.Sample, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.SamplesForLibraryType(ctx, libraryType, providerFetchLimit, 0)
}

func (a *ClientAdapter) SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.SamplesForLibraryType(ctx, pipelineIDLims, limit, offset)
}

func (a *ClientAdapter) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error) {
	return a.FindSamplesBySangerID(ctx, accessionNumber)
}

func (a *ClientAdapter) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.SamplesForRun(ctx, idRun, limit, offset)
}

func (a *ClientAdapter) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.SamplesForLibrary(ctx, pipelineIDLims, studyLimsID, limit, offset)
}

func (a *ClientAdapter) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.LibrariesForStudy(ctx, studyLimsID, limit, offset)
}

func (a *ClientAdapter) StudyForSample(ctx context.Context, sangerName string) (*mlwh.Study, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.StudyForSample(ctx, sangerName)
}

func (a *ClientAdapter) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.LanesForSample(ctx, sangerName, limit, offset)
}

func (a *ClientAdapter) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return provider.IRODSPathsForSample(ctx, sangerName, limit, offset)
}

func (a *ClientAdapter) GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error) {
	provider, err := a.delegate()
	if err != nil {
		return nil, err
	}

	return listSampleFiles(ctx, provider, sangerName)
}
