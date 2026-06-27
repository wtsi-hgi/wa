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

import "errors"

var (
	ErrNotFound              = errors.New("mlwh: identifier not found")
	ErrCacheNeverSynced      = errors.New("mlwh: cache has never been synced; run \"wa mlwh sync\" first")
	ErrSyncAlreadyRunning    = errors.New("mlwh sync: another sync is already running against this cache")
	ErrAmbiguous             = errors.New("mlwh: identifier matches multiple records")
	ErrUnsupportedIdentifier = errors.New("mlwh: identifier form not supported")
)

// IdentifierKind classifies the canonical identifier returned by a resolver.
type IdentifierKind string

const (
	KindSampleUUID       IdentifierKind = "sample_uuid"
	KindSampleLimsID     IdentifierKind = "sample_lims_id"
	KindSangerSampleName IdentifierKind = "sanger_sample_name"
	KindSangerSampleID   IdentifierKind = "sanger_sample_id"
	KindSupplierName     IdentifierKind = "supplier_name"
	KindSampleAccession  IdentifierKind = "sample_accession"
	KindDonorID          IdentifierKind = "donor_id"
	KindStudyUUID        IdentifierKind = "study_uuid"
	KindStudyLimsID      IdentifierKind = "study_lims_id"
	KindStudyAccession   IdentifierKind = "study_accession"
	KindStudyName        IdentifierKind = "study_name"
	KindRunID            IdentifierKind = "run_id"
	KindLibraryType      IdentifierKind = "library_type"
	KindLibraryID        IdentifierKind = "library_id"
	KindLibraryLimsID    IdentifierKind = "id_library_lims"
	MaxSamplesPerHop     int            = 1000
)

// IdentifierKinds returns every IdentifierKind constant value in a stable
// declaration order. It is the single source of truth for the set of supported
// identifier kinds, consumed by the domain glossary's coverage test so a newly
// added kind that is left undocumented is caught.
func IdentifierKinds() []IdentifierKind {
	return []IdentifierKind{
		KindSampleUUID,
		KindSampleLimsID,
		KindSangerSampleName,
		KindSangerSampleID,
		KindSupplierName,
		KindSampleAccession,
		KindDonorID,
		KindStudyUUID,
		KindStudyLimsID,
		KindStudyAccession,
		KindStudyName,
		KindRunID,
		KindLibraryType,
		KindLibraryID,
		KindLibraryLimsID,
	}
}

// Library is the cache-backed library shape mirrored from MLWH.
type Library struct {
	PipelineIDLims string `json:"pipeline_id_lims" doc:"pipeline LIMS identifier of the library"`
	IDStudyLims    string `json:"id_study_lims" doc:"LIMS identifier of the study the library belongs to"`
	LibraryID      string `json:"library_id,omitempty" doc:"library identifier, when known"`
	IDLibraryLims  string `json:"id_library_lims,omitempty" doc:"LIMS library identifier, when known"`
}

// Run is the run identifier shape resolved from MLWH.
type Run struct {
	IDRun int `json:"id_run" doc:"sequencing run identifier"`
}

// Match is the canonical resolver result.
type Match struct {
	Kind      IdentifierKind `json:"kind" doc:"kind of the resolved identifier"`
	Canonical string         `json:"canonical" doc:"canonical form of the resolved identifier"`
	Sample    *Sample        `json:"sample,omitempty" doc:"matched sample, when the identifier resolves to one"`
	Study     *Study         `json:"study,omitempty" doc:"matched study, when the identifier resolves to one"`
	Run       *Run           `json:"run,omitempty" doc:"matched run, when the identifier resolves to one"`
	Library   *Library       `json:"library,omitempty" doc:"matched library, when the identifier resolves to one"`
}
