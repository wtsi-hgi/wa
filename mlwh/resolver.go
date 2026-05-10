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
	"time"
)

const sampleNegativeCacheTTLSeconds = 15 * 60

// sampleStudyLimsSubquery resolves a sample's study_lims_id via iseq_flowcell
// joined through study.id_study_lims because the upstream MLWH `sample` table
// has no `id_study_lims` column and current iseq_flowcell rows link to study
// by id_study_tmp. COALESCE squashes a NULL (sample with no flowcell entry,
// e.g. PacBio-only) to an empty string for safe Scan.
const sampleStudyLimsSubquery = `COALESCE((SELECT study.id_study_lims FROM iseq_flowcell INNER JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp WHERE iseq_flowcell.id_sample_tmp = sample.id_sample_tmp AND study.id_lims = 'SQSCP' LIMIT 1), '') AS id_study_lims`

var sampleSelectColumns = `id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, ` + sampleStudyLimsSubquery + `, name, name AS sanger_id, sanger_sample_id, supplier_name, accession_number, donor_id, '' AS library_type, taxon_id, common_name, description`

var sampleMirrorSelectColumns = `sample_mirror.id_sample_tmp, sample_mirror.id_lims, sample_mirror.id_sample_lims, sample_mirror.uuid_sample_lims, sample_mirror.id_study_lims, sample_mirror.name, sample_mirror.sanger_id, sample_mirror.sanger_sample_id, sample_mirror.supplier_name, sample_mirror.accession_number, sample_mirror.donor_id, sample_mirror.library_type, sample_mirror.taxon_id, sample_mirror.common_name, sample_mirror.description`

var negativeCacheColumns = []string{"raw", "reason", "fetched_at", "ttl_seconds"}

var studyMirrorSelectColumns = `id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, egadac_accession_number, ega_policy_accession_number, data_release_timing`

var uuidShapePattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ResolveStudyOption customizes ResolveStudy name matching behavior.
type ResolveStudyOption func(*resolveStudyOpts)

// WithCaseInsensitiveStudyName enables case-insensitive matching for the name step.
func WithCaseInsensitiveStudyName() ResolveStudyOption {
	return func(opts *resolveStudyOpts) {
		opts.caseInsensitiveName = true
	}
}

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
// On the first call against a cold cache, it blocks on a full wa mlwh sync of
// iseq_flowcell before reading so operators can pre-warm with wa mlwh sync.
func (c *Client) ResolveLibrary(ctx context.Context, raw string) (Match, error) {
	if err := c.ensureResolverTableSynced(ctx, syncTableIseqFlowcell); err != nil {
		return Match{}, err
	}

	match, err := c.resolveLibraryFromCache(ctx, raw)
	if errors.Is(err, ErrNotFound) {
		if _, syncErr := c.Sync(ctx, syncTableIseqFlowcell); syncErr != nil {
			return Match{}, syncErr
		}

		match, err = c.resolveLibraryFromCache(ctx, raw)
	}

	return match, err
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

type resolveStudyOpts struct {
	caseInsensitiveName bool
}

// ResolveStudy resolves a study from cache-backed indexed lookups.
func (c *Client) ResolveStudy(ctx context.Context, raw string, opts ...ResolveStudyOption) (Match, error) {
	config := resolveStudyOpts{}
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}

	if isUUIDShape(raw) {
		study, err := c.resolveStudyFromCacheWithWarmup(
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
		study, resolveErr := c.resolveStudyFromCacheWithWarmup(
			ctx,
			`SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
			raw,
		)
		if resolveErr != nil {
			return Match{}, resolveErr
		}

		return Match{Kind: KindStudyLimsID, Canonical: study.IDStudyLims, Study: study}, nil
	}

	study, err := c.resolveStudyFromCacheWithWarmup(
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

	study, err = c.resolveStudyByNameWithWarmup(ctx, raw, config.caseInsensitiveName)
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

	negativeCached, err := c.isSampleNegativeCached(ctx, raw)
	if err != nil {
		return Match{}, err
	}
	if negativeCached {
		return Match{}, ErrNotFound
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

	if err := c.ensureResolverTableSynced(ctx, syncTableSample); err != nil {
		return Match{}, err
	}

	sample, err := c.resolveSampleFromCache(
		ctx,
		`SELECT `+sampleMirrorSelectColumns+` FROM donor_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = donor_samples.id_sample_tmp WHERE donor_samples.donor_id = ? ORDER BY sample_mirror.id_sample_tmp LIMIT 1`,
		raw,
	)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if cacheErr := c.cacheSampleNegativeLookup(ctx, raw); cacheErr != nil {
				return Match{}, cacheErr
			}
		}

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

func (c *Client) resolveStudyFromCacheWithWarmup(ctx context.Context, query, raw string) (*Study, error) {
	study, err := c.resolveStudyFromCache(ctx, query, raw)
	if err == nil || !errors.Is(err, ErrNotFound) || c == nil || c.syncSource == nil {
		return study, err
	}

	warm, warmErr := c.hasResolverSyncState(ctx, syncTableStudy)
	if warmErr != nil {
		return nil, warmErr
	}
	if warm {
		return nil, err
	}

	if syncErr := c.ensureResolverTableSynced(ctx, syncTableStudy); syncErr != nil {
		return nil, syncErr
	}

	return c.resolveStudyFromCache(ctx, query, raw)
}

func (c *Client) resolveStudyByNameWithWarmup(ctx context.Context, raw string, caseInsensitive bool) (*Study, error) {
	study, err := c.resolveStudyByName(ctx, raw, caseInsensitive)
	if err == nil || !errors.Is(err, ErrNotFound) || c == nil || c.syncSource == nil {
		return study, err
	}

	warm, warmErr := c.hasResolverSyncState(ctx, syncTableStudy)
	if warmErr != nil {
		return nil, warmErr
	}
	if warm {
		return nil, err
	}

	if syncErr := c.ensureResolverTableSynced(ctx, syncTableStudy); syncErr != nil {
		return nil, syncErr
	}

	return c.resolveStudyByName(ctx, raw, caseInsensitive)
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
		&sample.IDStudyLims,
		&sample.Name,
		&sample.SangerID,
		&sample.SangerSampleID,
		&sample.SupplierName,
		&sample.AccessionNumber,
		&sample.DonorID,
		&sample.LibraryType,
		&sample.TaxonID,
		&sample.CommonName,
		&sample.Description,
	); err != nil {
		return nil, fmt.Errorf("%w: scan sample source: %w", ErrUpstreamImpaired, err)
	}

	return sample, nil
}

func (c *Client) isSampleNegativeCached(ctx context.Context, raw string) (bool, error) {
	db := c.readCacheDB()
	if db == nil {
		return false, nil
	}

	var fetchedAt string
	var ttlSeconds int
	err := db.QueryRowContext(ctx, `SELECT fetched_at, ttl_seconds FROM negative_cache WHERE raw = ? LIMIT 1`, raw).Scan(&fetchedAt, &ttlSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: query negative cache: %w", ErrUpstreamImpaired, err)
	}

	fetchedTime, err := time.Parse(time.RFC3339Nano, fetchedAt)
	if err != nil {
		return false, fmt.Errorf("%w: parse negative cache timestamp: %w", ErrUpstreamImpaired, err)
	}

	if time.Since(fetchedTime) <= time.Duration(ttlSeconds)*time.Second {
		return true, nil
	}

	if _, err = c.cache.DB().ExecContext(ctx, `DELETE FROM negative_cache WHERE raw = ?`, raw); err != nil {
		return false, fmt.Errorf("%w: clear expired negative cache: %w", ErrUpstreamImpaired, err)
	}

	return false, nil
}

func (c *Client) cacheSampleNegativeLookup(ctx context.Context, raw string) error {
	if c == nil || c.cache == nil {
		return nil
	}

	stmt := buildUpsertStatement(c.cache.Dialect(), "negative_cache", negativeCacheColumns, []string{"raw"})

	_, err := c.cache.DB().ExecContext(
		ctx,
		stmt,
		raw,
		"not_found",
		time.Now().UTC().Format(time.RFC3339Nano),
		sampleNegativeCacheTTLSeconds,
	)
	if err != nil {
		return fmt.Errorf("%w: write negative cache: %w", ErrUpstreamImpaired, err)
	}

	return nil
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
		&sample.IDStudyLims,
		&sample.Name,
		&sample.SangerID,
		&sample.SangerSampleID,
		&sample.SupplierName,
		&sample.AccessionNumber,
		&sample.DonorID,
		&sample.LibraryType,
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
	err := db.QueryRowContext(ctx, query, raw).Scan(
		&study.IDStudyTmp,
		&study.IDLims,
		&study.IDStudyLims,
		&study.UUIDStudyLims,
		&study.Name,
		&study.AccessionNumber,
		&study.StudyTitle,
		&study.FacultySponsor,
		&study.State,
		&study.Abstract,
		&study.Abbreviation,
		&study.Description,
		&study.DataReleaseStrategy,
		&study.DataAccessGroup,
		&study.HMDMCNumber,
		&study.Programme,
		&study.Created,
		&study.ReferenceGenome,
		&study.EthicallyApproved,
		&study.StudyType,
		&study.ContainsHumanDNA,
		&study.ContaminatedHumanDNA,
		&study.StudyVisibility,
		&study.EGADACAccessionNumber,
		&study.EGAPolicyAccessionNumber,
		&study.DataReleaseTiming,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: query study cache: %w", ErrUpstreamImpaired, err)
	}

	return study, nil
}

func (c *Client) resolveStudyByName(ctx context.Context, raw string, caseInsensitive bool) (*Study, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	query := `SELECT ` + studyMirrorSelectColumns + ` FROM study_mirror WHERE name = ? AND id_lims = 'SQSCP' ORDER BY id_study_tmp LIMIT 2`
	if caseInsensitive {
		query = `SELECT ` + studyMirrorSelectColumns + ` FROM study_mirror WHERE LOWER(name) = LOWER(?) AND id_lims = 'SQSCP' ORDER BY id_study_tmp LIMIT 2`
	}

	rows, err := db.QueryContext(ctx, query, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: query study cache: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	studies := make([]*Study, 0, 2)
	for rows.Next() {
		study := &Study{}
		if err = rows.Scan(
			&study.IDStudyTmp,
			&study.IDLims,
			&study.IDStudyLims,
			&study.UUIDStudyLims,
			&study.Name,
			&study.AccessionNumber,
			&study.StudyTitle,
			&study.FacultySponsor,
			&study.State,
			&study.Abstract,
			&study.Abbreviation,
			&study.Description,
			&study.DataReleaseStrategy,
			&study.DataAccessGroup,
			&study.HMDMCNumber,
			&study.Programme,
			&study.Created,
			&study.ReferenceGenome,
			&study.EthicallyApproved,
			&study.StudyType,
			&study.ContainsHumanDNA,
			&study.ContaminatedHumanDNA,
			&study.StudyVisibility,
			&study.EGADACAccessionNumber,
			&study.EGAPolicyAccessionNumber,
			&study.DataReleaseTiming,
		); err != nil {
			return nil, fmt.Errorf("%w: scan study cache: %w", ErrUpstreamImpaired, err)
		}

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
