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
	"errors"
	"fmt"
)

// countStudiesCacheSQL counts the SQSCP studies mirrored in the cache, matching
// the id_lims filter used by AllStudies.
const countStudiesCacheSQL = `SELECT COUNT(*) FROM study_mirror WHERE id_lims = 'SQSCP'`

// countSamplesForStudyCacheSQL counts the distinct samples linked to a study via
// library_samples. It uses the same join and filter as SamplesForStudy
// (samplesForStudyCacheSQL) without a LIMIT, so the count equals the number of
// rows SamplesForStudy returns when fetching all of them.
const countSamplesForStudyCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_study_lims = ?`

// Count is the bare count envelope returned by the /count endpoints. It
// serialises as {"count": N}.
type Count struct {
	Count int `json:"count" doc:"number of matching rows"`
}

// CountStudies counts the studies mirrored in the cache (study_mirror rows with
// id_lims = 'SQSCP'), the count counterpart of AllStudies. A never-synced cache
// returns Count{} with an error satisfying both ErrCacheNeverSynced and
// ErrNotFound.
func (c *Client) CountStudies(ctx context.Context) (Count, error) {
	count, err := c.queryCount(ctx, countStudiesCacheSQL, "count studies")
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// CountSamplesForStudy counts the distinct samples linked to a study, the count
// counterpart of SamplesForStudy (same library_samples join and filter, no
// LIMIT), so CountSamplesForStudy(study) equals len(SamplesForStudy(study, all))
// for any study. A synced study with no samples returns Count{Count: 0} and no
// error; a never-synced cache returns Count{} with an error satisfying both
// ErrCacheNeverSynced and ErrNotFound, mirroring SamplesForStudy.
func (c *Client) CountSamplesForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	count, err := c.queryCount(ctx, countSamplesForStudyCacheSQL, "count study samples", studyLimsID)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	return c.countSamplesForEmptyStudy(ctx, studyLimsID)
}

// countSamplesForEmptyStudy resolves the result when no samples were counted for
// a study, mirroring SamplesForStudy's never-synced cascade so the count and the
// list agree on a never-synced cache, an unknown study, and a synced empty study.
func (c *Client) countSamplesForEmptyStudy(ctx context.Context, studyLimsID string) (Count, error) {
	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return Count{}, err
	}
	if studyExists {
		summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
		if err != nil {
			return Count{}, err
		}
		if summary.allAbsent || !summary.allPresent {
			return Count{}, neverSyncedReadErr()
		}

		return Count{Count: 0}, nil
	}

	if err := c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		if errors.Is(err, ErrCacheNeverSynced) {
			return Count{}, err
		}

		return Count{}, err
	}

	return Count{}, ErrNotFound
}

// queryCount runs a single-row COUNT(...) query against the cache reader and
// returns the scalar result.
func (c *Client) queryCount(ctx context.Context, query, action string, args ...any) (int, error) {
	db := c.readCacheDB()
	if db == nil {
		return 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}

	return count, nil
}
