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
	"regexp"
	"strconv"
)

var sampleSelectColumns = `id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description`

var sampleMirrorSelectColumns = `sample_mirror.id_sample_tmp, sample_mirror.id_lims, sample_mirror.id_sample_lims, sample_mirror.uuid_sample_lims, sample_mirror.name, sample_mirror.sanger_sample_id, sample_mirror.supplier_name, sample_mirror.accession_number, sample_mirror.donor_id, sample_mirror.taxon_id, sample_mirror.common_name, sample_mirror.description`

var studyMirrorSelectColumns = `id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing`

var uuidShapePattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUIDShape(raw string) bool {
	return uuidShapePattern.MatchString(raw)
}

// ClassifyIdentifier classifies an identifier by dispatching on shape and
// applying resolver priority within that shape.
func (c *Client) ClassifyIdentifier(ctx context.Context, raw string) (Match, error) {
	if isRejectedLIMSProviderConstant(raw) {
		return Match{}, fmt.Errorf("%w: %q looks like a LIMS provider constant", ErrUnsupportedIdentifier, raw)
	}

	if isUUIDShape(raw) {
		return c.classifyUUIDIdentifier(ctx, raw)
	}

	if _, err := strconv.Atoi(raw); err == nil {
		return c.classifyIntegerIdentifier(ctx, raw)
	}

	return c.classifyTextIdentifier(ctx, raw)
}

func (c *Client) readCacheDB() *sql.DB {
	if c == nil {
		return nil
	}

	if c.cacheReader != nil {
		return c.cacheReader
	}

	if c.cache != nil {
		return c.cache.DB()
	}

	return nil
}

func (c *Client) classifyUUIDIdentifier(ctx context.Context, raw string) (Match, error) {
	match, err := c.ResolveStudy(ctx, raw)
	if err == nil {
		return match, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Match{}, err
	}

	sample, err := c.resolveSampleByUUID(ctx, raw)
	if err != nil {
		return Match{}, err
	}

	return Match{Kind: KindSampleUUID, Canonical: sample.Name, Sample: sample}, nil
}

func (c *Client) classifyIntegerIdentifier(ctx context.Context, raw string) (Match, error) {
	match, err := c.ResolveStudy(ctx, raw)
	if err == nil {
		return match, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Match{}, err
	}

	sample, err := c.resolveSampleByLimsID(ctx, raw)
	if err == nil {
		return Match{Kind: KindSampleLimsID, Canonical: sample.Name, Sample: sample}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Match{}, err
	}

	return c.ResolveRun(ctx, raw)
}

func (c *Client) classifyTextIdentifier(ctx context.Context, raw string) (Match, error) {
	match, err := c.ResolveStudy(ctx, raw)
	if err == nil {
		return match, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Match{}, err
	}

	match, err = c.ResolveSample(ctx, raw)
	if err == nil {
		return match, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Match{}, err
	}

	return c.ResolveLibrary(ctx, raw)
}

// ResolveLibrary resolves an exact pipeline_id_lims match from the cache.
func (c *Client) ResolveLibrary(ctx context.Context, raw string) (Match, error) {
	if err := c.requireResolverSyncState(ctx, syncTableIseqFlowcell); err != nil {
		return Match{}, err
	}

	return c.resolveLibraryFromCache(ctx, raw)
}

func (c *Client) resolveLibraryFromCache(ctx context.Context, raw string) (Match, error) {

	db := c.readCacheDB()
	if db == nil {
		return Match{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	var pipelineIDLims string
	err := db.QueryRowContext(
		ctx,
		`SELECT pipeline_id_lims FROM library_samples WHERE pipeline_id_lims = ? LIMIT 1`,
		raw,
	).Scan(&pipelineIDLims)
	if errors.Is(err, sql.ErrNoRows) {
		return Match{}, ErrNotFound
	}
	if err != nil {
		return Match{}, fmt.Errorf("%w: query library cache: %w", ErrUpstreamImpaired, err)
	}

	library := &Library{PipelineIDLims: pipelineIDLims}

	return Match{Kind: KindLibraryType, Canonical: pipelineIDLims, Library: library}, nil
}

// ResolveRun resolves a numeric run identifier by checking for a matching
// iseq_product_metrics row in MLWH.
func (c *Client) ResolveRun(ctx context.Context, raw string) (Match, error) {
	runID, err := strconv.Atoi(raw)
	if err != nil {
		return Match{}, ErrUnsupportedIdentifier
	}

	if c == nil || c.syncSource == nil {
		return Match{}, fmt.Errorf("mlwh: sync source not configured")
	}

	rows, err := c.syncSource.QueryContext(
		ctx,
		`SELECT id_run FROM iseq_product_metrics WHERE id_run = ? LIMIT 1`,
		runID,
	)
	if err != nil {
		return Match{}, fmt.Errorf("%w: query run metrics: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return Match{}, fmt.Errorf("%w: query run metrics: %w", ErrUpstreamImpaired, err)
		}

		return Match{}, ErrNotFound
	}

	var resolvedRunID int
	if err = rows.Scan(&resolvedRunID); err != nil {
		return Match{}, fmt.Errorf("%w: scan run metrics: %w", ErrUpstreamImpaired, err)
	}

	run := &Run{IDRun: resolvedRunID}

	return Match{Kind: KindRunID, Canonical: strconv.Itoa(resolvedRunID), Run: run}, nil
}

// ResolveStudy resolves a study from cache-backed indexed lookups.
func (c *Client) ResolveStudy(ctx context.Context, raw string) (Match, error) {
	if isUUIDShape(raw) {
		study, err := c.resolveStudyFromCache(
			ctx,
			`SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE uuid_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
			raw,
		)
		if err != nil {
			return Match{}, err
		}

		return Match{Kind: KindStudyUUID, Canonical: study.IDStudyLims, Study: study}, nil
	}

	if _, err := strconv.Atoi(raw); err == nil {
		study, resolveErr := c.resolveStudyFromCache(
			ctx,
			`SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
			raw,
		)
		if resolveErr != nil {
			return Match{}, resolveErr
		}

		return Match{Kind: KindStudyLimsID, Canonical: study.IDStudyLims, Study: study}, nil
	}

	study, err := c.resolveStudyFromCache(
		ctx,
		`SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE accession_number = ? AND id_lims = 'SQSCP' LIMIT 1`,
		raw,
	)
	if err == nil {
		return Match{Kind: KindStudyAccession, Canonical: study.IDStudyLims, Study: study}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Match{}, err
	}

	study, err = c.resolveStudyByName(ctx, raw)
	if err != nil {
		return Match{}, err
	}

	return Match{Kind: KindStudyName, Canonical: study.IDStudyLims, Study: study}, nil
}

// ResolveSample resolves a sample from cache-backed indexed lookups.
func (c *Client) ResolveSample(ctx context.Context, raw string) (Match, error) {
	if isRejectedLIMSProviderConstant(raw) {
		return Match{}, fmt.Errorf("%w: %q looks like a LIMS provider constant", ErrUnsupportedIdentifier, raw)
	}

	sampleCacheWarm, err := c.sampleResolverCacheWarm(ctx)
	if err != nil {
		return Match{}, err
	}

	if sampleCacheWarm {
		match, resolveErr := c.resolveSampleDirectFromCache(ctx, raw)
		if resolveErr == nil {
			return match, nil
		}
		if !errors.Is(resolveErr, ErrNotFound) {
			return Match{}, resolveErr
		}
	}

	if !sampleCacheWarm && c != nil && c.syncSource != nil && isUUIDShape(raw) {
		sample, resolveErr := c.resolveSampleFromUpstream(ctx, `SELECT `+sampleSelectColumns+` FROM sample WHERE uuid_sample_lims = ? LIMIT 1`, raw)
		if resolveErr == nil {
			return Match{Kind: KindSampleUUID, Canonical: sample.Name, Sample: sample}, nil
		}
		if !errors.Is(resolveErr, ErrNotFound) {
			return Match{}, resolveErr
		}
	} else if !sampleCacheWarm && c != nil && c.syncSource != nil {
		if _, atoiErr := strconv.Atoi(raw); atoiErr == nil {
			sample, resolveErr := c.resolveSampleFromUpstream(ctx, `SELECT `+sampleSelectColumns+` FROM sample WHERE uuid_sample_lims = ? LIMIT 1`, raw)
			if resolveErr == nil {
				return Match{Kind: KindSampleUUID, Canonical: sample.Name, Sample: sample}, nil
			}
			if !errors.Is(resolveErr, ErrNotFound) {
				return Match{}, resolveErr
			}

			sample, resolveErr = c.resolveSampleFromUpstream(ctx, `SELECT `+sampleSelectColumns+` FROM sample WHERE id_sample_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, raw)
			if resolveErr == nil {
				return Match{Kind: KindSampleLimsID, Canonical: sample.Name, Sample: sample}, nil
			}
			if !errors.Is(resolveErr, ErrNotFound) {
				return Match{}, resolveErr
			}
		} else {
			steps := []struct {
				kind  IdentifierKind
				query string
			}{
				{kind: KindSangerSampleName, query: `SELECT ` + sampleSelectColumns + ` FROM sample WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`},
				{kind: KindSangerSampleID, query: `SELECT ` + sampleSelectColumns + ` FROM sample WHERE sanger_sample_id = ? AND id_lims = 'SQSCP' LIMIT 1`},
				{kind: KindSupplierName, query: `SELECT ` + sampleSelectColumns + ` FROM sample WHERE supplier_name = ? AND id_lims = 'SQSCP' LIMIT 1`},
				{kind: KindSampleAccession, query: `SELECT ` + sampleSelectColumns + ` FROM sample WHERE accession_number = ? AND id_lims = 'SQSCP' LIMIT 1`},
			}

			for _, step := range steps {
				sample, resolveErr := c.resolveSampleFromUpstream(ctx, step.query, raw)
				if resolveErr == nil {
					return Match{Kind: step.kind, Canonical: sample.Name, Sample: sample}, nil
				}
				if !errors.Is(resolveErr, ErrNotFound) {
					return Match{}, resolveErr
				}
			}
		}
	}

	if err := c.requireResolverSyncState(ctx, syncTableSample); err != nil {
		return Match{}, err
	}

	sample, err := c.resolveSampleFromCache(
		ctx,
		`SELECT `+sampleMirrorSelectColumns+` FROM donor_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = donor_samples.id_sample_tmp WHERE donor_samples.donor_id = ? ORDER BY sample_mirror.id_sample_tmp LIMIT 1`,
		raw,
	)
	if err != nil {
		return Match{}, err
	}

	return Match{Kind: KindDonorID, Canonical: sample.Name, Sample: sample}, nil
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

	steps := []struct {
		kind  IdentifierKind
		query string
	}{
		{kind: KindSangerSampleName, query: `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`},
		{kind: KindSangerSampleID, query: `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE sanger_sample_id = ? AND id_lims = 'SQSCP' LIMIT 1`},
		{kind: KindSupplierName, query: `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE supplier_name = ? AND id_lims = 'SQSCP' LIMIT 1`},
		{kind: KindSampleAccession, query: `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE accession_number = ? AND id_lims = 'SQSCP' LIMIT 1`},
	}

	for _, step := range steps {
		sample, err := c.resolveSampleFromCache(ctx, step.query, raw)
		if err == nil {
			return Match{Kind: step.kind, Canonical: sample.Name, Sample: sample}, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Match{}, err
		}
	}

	return Match{}, ErrNotFound
}

func (c *Client) resolveSampleByUUID(ctx context.Context, raw string) (*Sample, error) {
	warm, err := c.sampleResolverCacheWarm(ctx)
	if err != nil {
		return nil, err
	}
	if warm {
		match, resolveErr := c.resolveSampleDirectFromCache(ctx, raw)
		if resolveErr != nil {
			return nil, resolveErr
		}

		return match.Sample, nil
	}

	return c.resolveSampleFromUpstream(ctx, `SELECT `+sampleSelectColumns+` FROM sample WHERE uuid_sample_lims = ? LIMIT 1`, raw)
}

func (c *Client) resolveSampleByLimsID(ctx context.Context, raw string) (*Sample, error) {
	warm, err := c.sampleResolverCacheWarm(ctx)
	if err != nil {
		return nil, err
	}
	if warm {
		match, resolveErr := c.resolveSampleDirectFromCache(ctx, raw)
		if resolveErr != nil {
			return nil, resolveErr
		}

		return match.Sample, nil
	}

	return c.resolveSampleFromUpstream(ctx, `SELECT `+sampleSelectColumns+` FROM sample WHERE id_sample_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, raw)
}

func (c *Client) sampleResolverCacheWarm(ctx context.Context) (bool, error) {
	if c == nil || c.cache == nil {
		return false, nil
	}

	return c.hasResolverSyncState(ctx, syncTableSample)
}

func (c *Client) resolveSampleFromUpstream(ctx context.Context, query, raw string) (*Sample, error) {
	if c == nil || c.syncSource == nil {
		return nil, fmt.Errorf("mlwh: sync source not configured")
	}

	rows, err := c.syncSource.QueryContext(ctx, query, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample source: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return nil, fmt.Errorf("%w: query sample source: %w", ErrUpstreamImpaired, err)
		}

		return nil, ErrNotFound
	}

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
		return nil, fmt.Errorf("%w: scan sample source: %w", ErrUpstreamImpaired, err)
	}

	return sample, nil
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

func (c *Client) resolveStudyFromCache(ctx context.Context, query, raw string) (*Study, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	study := &Study{}
	targets, apply := studyScanTargets(study)
	err := db.QueryRowContext(ctx, query, raw).Scan(targets...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: query study cache: %w", ErrUpstreamImpaired, err)
	}
	apply()

	return study, nil
}

func (c *Client) resolveStudyByName(ctx context.Context, raw string) (*Study, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE name = ? AND id_lims = 'SQSCP' ORDER BY id_study_tmp LIMIT 2`, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: query study cache: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	studies := make([]*Study, 0, 2)
	for rows.Next() {
		study := &Study{}
		targets, apply := studyScanTargets(study)
		if err = rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("%w: scan study cache: %w", ErrUpstreamImpaired, err)
		}
		apply()

		studies = append(studies, study)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query study cache: %w", ErrUpstreamImpaired, err)
	}

	switch len(studies) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return studies[0], nil
	default:
		return nil, fmt.Errorf("%w: studies %s and %s", ErrAmbiguous, studies[0].IDStudyLims, studies[1].IDStudyLims)
	}
}
