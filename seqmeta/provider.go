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

	"github.com/wtsi-hgi/wa/mlwh"
)

// Provider is the mockable MLWH-backed query surface used by seqmeta.
type Provider interface {
	mlwh.Querier
	ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveSample(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveRun(ctx context.Context, raw string) (mlwh.Match, error)
	ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error)
	AllStudies(ctx context.Context, limit, offset int) ([]mlwh.Study, error)
	GetStudy(ctx context.Context, identifier string) (*mlwh.Study, error)
	SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
	AllSamplesForStudy(ctx context.Context, studyLimsID string) ([]mlwh.Sample, error)
	FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error)
	FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error)
	FindSamplesByRunID(ctx context.Context, idRun int) ([]mlwh.Sample, error)
	FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]mlwh.Sample, error)
	FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]mlwh.Sample, error)
	SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]mlwh.Sample, error)
	SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]mlwh.Sample, error)
	SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)
	LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]mlwh.Library, error)
	StudiesForSample(ctx context.Context, sangerName string) ([]mlwh.Study, error)
	LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error)
	IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error)
	GetSampleFiles(ctx context.Context, sangerName string) ([]mlwh.IRODSPath, error)
}

const providerFetchLimit = 1_000_000

func getStudy(ctx context.Context, provider Provider, identifier string) (*mlwh.Study, error) {
	match, err := provider.ResolveStudy(ctx, identifier)
	if err != nil {
		return nil, err
	}

	return match.Study, nil
}

func listAllStudies(ctx context.Context, provider Provider) ([]mlwh.Study, error) {
	return provider.AllStudies(ctx, providerFetchLimit, 0)
}

func listStudySamples(ctx context.Context, provider Provider, studyID string) ([]mlwh.Sample, error) {
	return provider.SamplesForStudy(ctx, studyID, providerFetchLimit, 0)
}

func listSampleFiles(ctx context.Context, provider Provider, sangerName string) ([]mlwh.IRODSPath, error) {
	return provider.IRODSPathsForSample(ctx, sangerName, providerFetchLimit, 0)
}
