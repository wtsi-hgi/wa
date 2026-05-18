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

package mlwh

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

type sampleLookupStep struct {
	kind  IdentifierKind
	query string
}

type sampleResolution struct {
	kind   IdentifierKind
	sample *Sample
}

var resolveSampleTextSteps = []sampleLookupStep{
	{kind: KindSangerSampleName, query: `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.id_sample_tmp LIMIT 2`},
	{kind: KindSangerSampleID, query: `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE sanger_sample_id = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.id_sample_tmp LIMIT 2`},
	{kind: KindSupplierName, query: `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE supplier_name = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.id_sample_tmp LIMIT 2`},
	{kind: KindSampleAccession, query: `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE accession_number = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.id_sample_tmp LIMIT 2`},
}

// ResolveSample resolves a sample from cache-backed indexed lookups.
func (c *Client) ResolveSample(ctx context.Context, raw string) (Match, error) {
	if isRejectedLIMSProviderConstant(raw) {
		return Match{}, fmt.Errorf("%w: %q looks like a LIMS provider constant", ErrUnsupportedIdentifier, raw)
	}

	match, err := c.resolveSampleDirectFromCache(ctx, raw)
	if err == nil {
		return match, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Match{}, err
	}

	if err := c.requireResolverSyncState(ctx, syncTableSample); err != nil {
		return Match{}, err
	}

	sample, err := c.resolveAmbiguousSampleFromCache(
		ctx,
		`SELECT `+sampleMirrorSelectColumns+` FROM donor_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = donor_samples.id_sample_tmp WHERE donor_samples.donor_id = ? ORDER BY sample_mirror.id_sample_tmp LIMIT 2`,
		raw,
	)
	if err != nil {
		return Match{}, err
	}

	return Match{Kind: KindDonorID, Canonical: sample.Name, Sample: sample}, nil
}

// ResolveSampleName resolves an exact canonical sample name from the cache.
func (c *Client) ResolveSampleName(ctx context.Context, raw string) (Match, error) {
	sample, err := c.resolveSampleFromCache(
		ctx,
		`SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`,
		raw,
	)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireResolverSyncState(ctx, syncTableSample); syncErr != nil {
				return Match{}, syncErr
			}
		}

		return Match{}, err
	}

	samples := []Sample{*sample}
	if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
		return Match{}, err
	}
	sample = &samples[0]

	return Match{Kind: KindSangerSampleName, Canonical: sample.Name, Sample: sample}, nil
}

func (c *Client) resolveSampleDirectFromCache(ctx context.Context, raw string) (Match, error) {
	if isUUIDShape(raw) {
		sample, err := c.resolveSampleFromCache(
			ctx,
			`SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE uuid_sample_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
			raw,
		)
		if err == nil {
			return Match{Kind: KindSampleUUID, Canonical: sample.Name, Sample: sample}, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Match{}, err
		}

		return Match{}, ErrNotFound
	}

	if _, atoiErr := strconv.Atoi(raw); atoiErr == nil {
		sample, err := c.resolveSampleFromCache(
			ctx,
			`SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE id_sample_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
			raw,
		)
		if err == nil {
			return Match{Kind: KindSampleLimsID, Canonical: sample.Name, Sample: sample}, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Match{}, err
		}

		return Match{}, ErrNotFound
	}

	return c.resolveSampleTextCascade(ctx, raw)
}

func (c *Client) resolveSampleTextCascade(ctx context.Context, raw string) (Match, error) {
	resolutions := make(map[int64]sampleResolution)
	orderedIDs := make([]int64, 0, 2)
	var first *sampleResolution

	for _, step := range resolveSampleTextSteps {
		sample, err := c.resolveAmbiguousSampleFromCache(ctx, step.query, raw)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}

			return Match{}, err
		}

		if _, seen := resolutions[sample.IDSampleTmp]; seen {
			continue
		}

		resolution := sampleResolution{kind: step.kind, sample: sample}
		resolutions[sample.IDSampleTmp] = resolution
		orderedIDs = append(orderedIDs, sample.IDSampleTmp)
		if first == nil {
			first = &resolution
		}
	}

	switch len(orderedIDs) {
	case 0:
		return Match{}, ErrNotFound
	case 1:
		return Match{Kind: first.kind, Canonical: first.sample.Name, Sample: first.sample}, nil
	default:
		return Match{}, fmt.Errorf("%w: %q matches samples %d and %d", ErrAmbiguous, raw, orderedIDs[0], orderedIDs[1])
	}
}

func (c *Client) resolveSampleByUUID(ctx context.Context, raw string) (*Sample, error) {
	return c.resolveSampleFromCache(
		ctx,
		`SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE uuid_sample_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
		raw,
	)
}

func (c *Client) resolveSampleByLimsID(ctx context.Context, raw string) (*Sample, error) {
	return c.resolveSampleFromCache(
		ctx,
		`SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE id_sample_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
		raw,
	)
}

func (c *Client) resolveSampleFromCache(ctx context.Context, query, raw string) (*Sample, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	sample := &Sample{}
	err := db.QueryRowContext(ctx, query, raw).Scan(
		&sample.IDSampleTmp,
		&sample.IDLims,
		&sample.IDSampleLims,
		&sample.UUIDSampleLims,
		&sample.Name,
		&sample.SangerSampleID,
		&sample.SupplierName,
		&sample.AccessionNumber,
		&sample.DonorID,
		&sample.TaxonID,
		&sample.CommonName,
		&sample.Description,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: query sample cache: %w", ErrUpstreamImpaired, err)
	}

	return sample, nil
}

func (c *Client) resolveAmbiguousSampleFromCache(ctx context.Context, query, raw string) (*Sample, error) {
	samples, err := c.querySamplesFromCache(ctx, query, raw)
	if err != nil {
		return nil, err
	}

	switch len(samples) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return samples[0], nil
	default:
		return nil, fmt.Errorf("%w: %q matches samples %d and %d", ErrAmbiguous, raw, samples[0].IDSampleTmp, samples[1].IDSampleTmp)
	}
}

func (c *Client) querySamplesFromCache(ctx context.Context, query, raw string) ([]*Sample, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, query, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample cache: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	samples := make([]*Sample, 0, 2)
	for rows.Next() {
		sample := &Sample{}
		if err = rows.Scan(
			&sample.IDSampleTmp,
			&sample.IDLims,
			&sample.IDSampleLims,
			&sample.UUIDSampleLims,
			&sample.Name,
			&sample.SangerSampleID,
			&sample.SupplierName,
			&sample.AccessionNumber,
			&sample.DonorID,
			&sample.TaxonID,
			&sample.CommonName,
			&sample.Description,
		); err != nil {
			return nil, fmt.Errorf("%w: scan sample cache: %w", ErrUpstreamImpaired, err)
		}

		samples = append(samples, sample)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query sample cache: %w", ErrUpstreamImpaired, err)
	}

	return samples, nil
}
