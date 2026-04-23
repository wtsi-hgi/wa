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
	"errors"
	"net/http"
	"strconv"

	"github.com/wtsi-hgi/wa/saga"
)

// Validate classifies a sequencing identifier by querying SAGA in priority order.
func Validate(ctx context.Context, provider SAGAProvider, identifier string) (*IdentifierResult, error) {
	if identifier == "" {
		return nil, ErrUnknownIdentifier
	}

	// GetStudy is speculative: most identifiers are not study IDs, and some
	// SAGA deployments reject non-study-shaped IDs (e.g. "SANG205") with 4xx
	// statuses rather than 404. 4xx client errors (including 404) are treated
	// as "not a study" and we fall through to the broader cascade. 5xx,
	// transport, or other errors indicate a genuine SAGA problem and are
	// propagated immediately to avoid triggering the expensive AllStudies /
	// AllSamples / ListProjects cascade against an impaired backend.
	study, err := provider.GetStudy(ctx, identifier)
	if err == nil && study != nil {
		return &IdentifierResult{Identifier: identifier, Type: IdentifierStudyID, Object: study}, nil
	}
	if err != nil && !isClientError(err) {
		return nil, err
	}

	studies, err := provider.AllStudies(ctx)
	if err != nil {
		return nil, err
	}
	for _, candidate := range studies {
		if candidate.AccessionNumber == identifier {
			return &IdentifierResult{Identifier: identifier, Type: IdentifierStudyAccession, Object: candidate}, nil
		}
	}

	samples, err := provider.AllSamples(ctx)
	if err != nil {
		return nil, err
	}

	for _, candidate := range samples {
		if candidate.SangerID == identifier {
			return &IdentifierResult{Identifier: identifier, Type: IdentifierSangerSampleID, Object: candidate}, nil
		}
	}
	for _, candidate := range samples {
		if candidate.IDSampleLims == identifier {
			return &IdentifierResult{Identifier: identifier, Type: IdentifierSampleLimsID, Object: candidate}, nil
		}
	}
	for _, candidate := range samples {
		if candidate.AccessionNumber == identifier {
			return &IdentifierResult{Identifier: identifier, Type: IdentifierSampleAccession, Object: candidate}, nil
		}
	}

	if runID, convErr := strconv.Atoi(identifier); convErr == nil {
		for _, candidate := range samples {
			if candidate.IDRun == runID {
				return &IdentifierResult{Identifier: identifier, Type: IdentifierRunID, Object: candidate}, nil
			}
		}
	}

	for _, candidate := range samples {
		if candidate.LibraryType == identifier {
			return &IdentifierResult{Identifier: identifier, Type: IdentifierLibraryType, Object: candidate}, nil
		}
	}

	projects, err := provider.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for _, candidate := range projects {
		if candidate.Name == identifier {
			return &IdentifierResult{Identifier: identifier, Type: IdentifierProjectName, Object: candidate}, nil
		}
	}

	return nil, ErrUnknownIdentifier
}

// isClientError reports whether err is a SAGA API error with a 4xx status
// code (or the sentinel saga.ErrNotFound). Such errors indicate the
// identifier simply isn't a study on this backend, so the validation
// cascade may safely continue. All other errors (5xx, transport failures,
// context deadline) indicate a real SAGA problem and should be propagated.
func isClientError(err error) bool {
	if errors.Is(err, saga.ErrNotFound) {
		return true
	}

	var apiErr saga.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= http.StatusBadRequest && apiErr.StatusCode < http.StatusInternalServerError
	}

	apiErrPtr := &saga.APIError{}
	if errors.As(err, &apiErrPtr) {
		return apiErrPtr.StatusCode >= http.StatusBadRequest && apiErrPtr.StatusCode < http.StatusInternalServerError
	}

	return false
}
