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
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/wtsi-hgi/wa/mlwh"
)

var (
	ErrUnknownIdentifier = errors.New("seqmeta: unknown identifier")
	ErrAllHopsFailed     = errors.New("seqmeta: all enrichment hops failed")
	ErrStoreOpen         = errors.New("seqmeta: failed to open store")
	errStoreOperation    = errors.New("seqmeta: store operation failed")
)

const (
	HopClassify  = "classify"
	HopStudy     = "study"
	HopSamples   = "samples"
	HopLibraries = "libraries"
	HopStudies   = "studies"
)

const (
	ReasonUpstreamError    = "upstream_error"
	ReasonNotFound         = "not_found"
	ReasonSamplesTruncated = "samples_truncated"
)

const MaxLibrarySamples = mlwh.MaxSamplesPerHop

// MaxLibraryTypeSamples limits the total samples returned for library type
// enrichment across all studies to prevent browser-freezing payloads. This is
// lower than MaxLibrarySamples to account for library types spanning many studies.
const MaxLibraryTypeSamples = 200

// IdentifierType classifies a sequencing identifier.
type IdentifierType = mlwh.IdentifierKind

const (
	IdentifierStudyID IdentifierType = IdentifierStudyLimsID

	IdentifierStudyLimsID      IdentifierType = "study_lims_id"
	IdentifierStudyAccession   IdentifierType = "study_accession"
	IdentifierStudyUUID        IdentifierType = "study_uuid"
	IdentifierStudyName        IdentifierType = "study_name"
	IdentifierSangerSampleName IdentifierType = "sanger_sample_name"
	IdentifierSangerSampleID   IdentifierType = "sanger_sample_id"
	IdentifierSampleLimsID     IdentifierType = "sample_lims_id"
	IdentifierSampleUUID       IdentifierType = "sample_uuid"
	IdentifierSampleAccession  IdentifierType = "sample_accession"
	IdentifierSupplierName     IdentifierType = "supplier_name"
	IdentifierDonorID          IdentifierType = "donor_id"
	IdentifierRunID            IdentifierType = "run_id"
	IdentifierLibraryType      IdentifierType = "library_type"
)

// Store persists seqmeta watermark state in SQLite.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// StoredEntry is one row in the watermarks table.
type StoredEntry struct {
	EntryHash string
	Tombstone bool
	UpdatedAt time.Time
}

type enrichCacheEntry struct {
	Identifier string
	Type       IdentifierType
	Body       []byte
	FetchedAt  time.Time
	TTL        time.Duration
	Negative   bool
	Partial    bool
}

// DiffResult holds the result of a diff poll.
type DiffResult[T any] struct {
	Added    []T      `json:"added"`
	Modified []T      `json:"modified"`
	Removed  []string `json:"removed"`
}

// IdentifierResult is returned by Validate.
type IdentifierResult struct {
	Identifier string         `json:"identifier"`
	Type       IdentifierType `json:"type"`
	Object     any            `json:"object"`
}

// Library is a (library_type, id_study_lims) tuple scoped to a study.
type Library struct {
	LibraryType   string `json:"library_type"`
	IDStudyLims   string `json:"id_study_lims"`
	LibraryID     string `json:"library_id,omitempty"`
	IDLibraryLims string `json:"id_library_lims,omitempty"`
}

// EnrichmentGraph is the flat graph envelope returned under "graph".
type EnrichmentGraph struct {
	Study     *mlwh.Study   `json:"study,omitempty"`
	Studies   []mlwh.Study  `json:"studies,omitempty"`
	Sample    *mlwh.Sample  `json:"sample,omitempty"`
	Samples   []mlwh.Sample `json:"samples,omitempty"`
	Library   *Library      `json:"library,omitempty"`
	Libraries []Library     `json:"libraries,omitempty"`

	// Hierarchical structures
	StudyDetail  *mlwh.StudyDetail  `json:"study_detail,omitempty"`
	StudyDetails []mlwh.StudyDetail `json:"study_details,omitempty"`
	SampleDetail *mlwh.SampleDetail `json:"sample_detail,omitempty"`
}

// MissingHop records a hop that failed or was truncated.
type MissingHop struct {
	Hop    string `json:"hop"`
	Reason string `json:"reason"`
	Status int    `json:"status"`
}

// EnrichmentResult is the /enrich/{identifier} response body.
type EnrichmentResult struct {
	Identifier string          `json:"identifier"`
	Type       IdentifierType  `json:"type"`
	Graph      EnrichmentGraph `json:"graph"`
	Partial    bool            `json:"partial"`
	Missing    []MissingHop    `json:"missing,omitempty"`
}

type enrichError struct {
	err     error
	missing []MissingHop
}

func (e *enrichError) Error() string {
	return e.err.Error()
}

func (e *enrichError) Unwrap() error {
	return e.err
}
