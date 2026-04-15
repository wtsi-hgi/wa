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

// DiffResult holds the result of a diff poll.
type DiffResult[T any] struct {
	Added    []T      `json:"added"`
	Modified []T      `json:"modified"`
	Removed  []string `json:"removed"`
}

// IdentifierType classifies a sequencing identifier.
type IdentifierType string

const (
	IdentifierStudyID         IdentifierType = "study_id"
	IdentifierStudyAccession  IdentifierType = "study_accession"
	IdentifierSangerSampleID  IdentifierType = "sanger_sample_id"
	IdentifierSampleLimsID    IdentifierType = "sample_lims_id"
	IdentifierSampleAccession IdentifierType = "sample_accession"
	IdentifierRunID           IdentifierType = "run_id"
	IdentifierLibraryType     IdentifierType = "library_type"
	IdentifierProjectName     IdentifierType = "project_name"
)

// IdentifierResult is returned by Validate.
type IdentifierResult struct {
	Identifier string         `json:"identifier"`
	Type       IdentifierType `json:"type"`
	Object     any            `json:"object"`
}

var (
	ErrUnknownIdentifier = errors.New("seqmeta: unknown identifier")
	ErrStoreOpen         = errors.New("seqmeta: failed to open store")
	errStoreOperation    = errors.New("seqmeta: store operation failed")
)
