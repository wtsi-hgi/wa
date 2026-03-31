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
	"strconv"

	"github.com/wtsi-hgi/wa/saga"
)

// Validate classifies a sequencing identifier by querying SAGA in priority order.
func Validate(ctx context.Context, provider SAGAProvider, identifier string) (*IdentifierResult, error) {
	if identifier == "" {
		return nil, ErrUnknownIdentifier
	}

	study, err := provider.GetStudy(ctx, identifier)
	if err == nil && study != nil {
		return &IdentifierResult{Identifier: identifier, Type: IdentifierStudyID, Object: study}, nil
	}
	if err != nil && !errors.Is(err, saga.ErrNotFound) {
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
