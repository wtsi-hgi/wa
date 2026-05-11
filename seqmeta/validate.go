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

	"github.com/wtsi-hgi/wa/mlwh"
)

// Validate classifies a sequencing identifier via the MLWH resolver surface.
func Validate(ctx context.Context, provider Provider, identifier string) (*IdentifierResult, error) {
	if identifier == "" {
		return nil, ErrUnknownIdentifier
	}

	match, err := provider.ClassifyIdentifier(ctx, identifier)
	if err != nil {
		switch {
		case errors.Is(err, mlwh.ErrCacheNeverSynced):
			return nil, errors.Join(fmtUnknownIdentifier(identifier), err)
		case errors.Is(err, mlwh.ErrNotFound):
			return nil, fmtUnknownIdentifier(identifier)
		case errors.Is(err, mlwh.ErrUnsupportedIdentifier):
			return nil, err
		default:
			return nil, err
		}
	}

	return &IdentifierResult{
		Identifier: match.Canonical,
		Type:       IdentifierType(match.Kind),
		Object:     matchObject(match),
	}, nil
}

func fmtUnknownIdentifier(identifier string) error {
	return errors.Join(ErrUnknownIdentifier, errors.New(identifier))
}

func matchObject(match mlwh.Match) any {
	switch {
	case match.Sample != nil:
		return match.Sample
	case match.Study != nil:
		return *match.Study
	case match.Run != nil:
		return match.Run
	case match.Library != nil:
		return match.Library
	default:
		return nil
	}
}
