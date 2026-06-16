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

package results

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/wtsi-hgi/wa/mlwh"
)

var (
	ErrNotFound = errors.New("results: not found")

	ErrInvalidInput = errors.New("results: invalid input")

	ErrFileGone = errors.New("results: file not found on disk")

	ErrFileTooLarge = errors.New("results: file exceeds preview limit")

	ErrSeqmetaFailed = errors.New("results: seqmeta unavailable")

	ErrMLWHFailed = errors.New("results: mlwh unavailable")

	ErrMLWHRejected = errors.New("results: mlwh validation failed")
)

const (
	// SeqmetaIDRunKey stores an MLWH id_run value.
	SeqmetaIDRunKey = "seqmeta_id_run"
	// SeqmetaIDStudyLimsKey stores an MLWH id_study_lims value.
	SeqmetaIDStudyLimsKey = "seqmeta_id_study_lims"
	// SeqmetaStudyAccessionKey stores an MLWH study accession_number value.
	SeqmetaStudyAccessionKey = "seqmeta_study_accession"
	// SeqmetaStudyUUIDKey stores an MLWH uuid_study_lims value.
	SeqmetaStudyUUIDKey = "seqmeta_uuid_study_lims"
	// SeqmetaStudyNameKey stores an MLWH study name value.
	SeqmetaStudyNameKey = "seqmeta_study_name"
	// SeqmetaSampleNameKey stores the MLWH sample name value used as the
	// canonical sample identity for sample-scoped result metadata.
	SeqmetaSampleNameKey = "seqmeta_name"
	// SeqmetaSampleNameURLKey is the precise frontend query/display alias for
	// sample-name metadata. Stored result metadata may still use
	// SeqmetaSampleNameKey.
	SeqmetaSampleNameURLKey = "seqmeta_sample_name"
	// SeqmetaIDSampleLimsKey stores an MLWH id_sample_lims value.
	SeqmetaIDSampleLimsKey = "seqmeta_id_sample_lims"
	// SeqmetaSangerSampleIDKey stores an MLWH sanger_sample_id value.
	SeqmetaSangerSampleIDKey = "seqmeta_sanger_sample_id"
	// SeqmetaSupplierNameKey stores an MLWH supplier_name value.
	SeqmetaSupplierNameKey = "seqmeta_supplier_name"
	// SeqmetaAccessionNumberKey stores an MLWH sample accession_number value.
	SeqmetaAccessionNumberKey = "seqmeta_accession_number"
	// SeqmetaSampleUUIDKey stores an MLWH uuid_sample_lims value.
	SeqmetaSampleUUIDKey = "seqmeta_uuid_sample_lims"
	// SeqmetaDonorIDKey stores an MLWH donor_id value.
	SeqmetaDonorIDKey = "seqmeta_donor_id"
	// SeqmetaPipelineIDLimsKey stores an MLWH pipeline_id_lims value.
	SeqmetaPipelineIDLimsKey = "seqmeta_pipeline_id_lims"
	// SeqmetaLibraryIDKey stores an MLWH library_id value.
	SeqmetaLibraryIDKey = "seqmeta_library_id"
	// SeqmetaIDLibraryLimsKey stores an MLWH id_library_lims value.
	SeqmetaIDLibraryLimsKey = "seqmeta_id_library_lims"

	// Legacy seqmeta keys remain supported for existing result databases and
	// URLs, but new registrations use the MLWH-named keys above.
	LegacySeqmetaRunIDKey       = "seqmeta_runid"
	LegacySeqmetaStudyIDKey     = "seqmeta_studyid"
	LegacySeqmetaSampleIDKey    = "seqmeta_sampleid"
	LegacySeqmetaSampleLimsKey  = "seqmeta_sample_lims"
	LegacySeqmetaLibraryKey     = "seqmeta_library"
	LegacySeqmetaLibraryIDKey   = "seqmeta_libraryid"
	LegacySeqmetaLibraryLimsKey = "seqmeta_library_lims"
	LegacySeqmetaLibraryTypeKey = "seqmeta_librarytype"
)

// SeqmetaFieldTypes maps metadata key suffixes to expected seqmeta identifier types.
var SeqmetaFieldTypes = map[string]string{
	"id_run":           "run_id",
	"runid":            "run_id",
	"id_study_lims":    "study_lims_id",
	"studyid":          "study_lims_id",
	"study_accession":  "study_accession",
	"uuid_study_lims":  "study_uuid",
	"study_name":       "study_name",
	"name":             "sanger_sample_name",
	"sampleid":         "sanger_sample_name",
	"id_sample_lims":   "sample_lims_id",
	"sample_lims":      "sample_lims_id",
	"sanger_sample_id": "sanger_sample_id",
	"supplier_name":    "supplier_name",
	"accession_number": "sample_accession",
	"uuid_sample_lims": "sample_uuid",
	"donor_id":         "donor_id",
	"library":          "library_type",
	"library_id":       "library_id",
	"libraryid":        "library_id",
	"id_library_lims":  "id_library_lims",
	"library_lims":     "id_library_lims",
	"librarytype":      "library_type",
	"pipeline_id_lims": "library_type",
}

// ResultSet is the core domain object returned by queries.
type ResultSet struct {
	ID                 string              `json:"id"`
	PipelineIdentifier string              `json:"pipeline_identifier"`
	RunKey             string              `json:"run_key"`
	Requester          string              `json:"requester"`
	Operator           string              `json:"operator"`
	Command            string              `json:"command"`
	PipelineName       string              `json:"pipeline_name"`
	PipelineVersion    string              `json:"pipeline_version"`
	OutputDirectory    string              `json:"output_directory"`
	OutputDirectoryGID *int64              `json:"output_directory_gid"`
	Metadata           map[string]string   `json:"metadata"`
	MetadataValues     map[string][]string `json:"metadata_values,omitempty"`
	Access             AccessState         `json:"access"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

// FileEntry represents one tracked file.
type FileEntry struct {
	Path  string    `json:"path"`
	Mtime time.Time `json:"mtime"`
	Size  int64     `json:"size"`
	Kind  string    `json:"kind"`
}

// RegistrationLookupValues carries raw MLWH lookup flag values for server-side
// resolution during results registration.
type RegistrationLookupValues struct {
	Run     []string `json:"run,omitempty"`
	Study   []string `json:"study,omitempty"`
	Sample  []string `json:"sample,omitempty"`
	Library []string `json:"library,omitempty"`
}

// Registration is the POST /results request body.
type Registration struct {
	PipelineIdentifier string                    `json:"pipeline_identifier"`
	RunKey             string                    `json:"run_key"`
	Requester          string                    `json:"requester"`
	Operator           string                    `json:"operator"`
	Command            string                    `json:"command"`
	PipelineName       string                    `json:"pipeline_name"`
	PipelineVersion    string                    `json:"pipeline_version"`
	OutputDirectory    string                    `json:"output_directory"`
	OutputDirectoryGID *int64                    `json:"-"`
	Files              []FileEntry               `json:"files"`
	Metadata           map[string]string         `json:"metadata"`
	MetadataValues     map[string][]string       `json:"metadata_values,omitempty"`
	LookupValues       *RegistrationLookupValues `json:"lookup_values,omitempty"`
}

// SearchParams holds parsed query parameters for filtering.
type SearchParams struct {
	Requester          string
	Operator           string
	PipelineName       string
	PipelineVersion    string
	PipelineIdentifier string
	RunKey             string
	OutputDirPrefix    string
	Meta               map[string]string
}

// MultiSearchParams holds parsed query parameters for multi-value filtering.
type MultiSearchParams struct {
	Requester          []string
	Operator           []string
	PipelineName       []string
	PipelineVersion    []string
	PipelineIdentifier []string
	RunKey             []string
	OutputDirPrefix    []string
	Meta               map[string][]string
	// OrMeta, if non-empty, is a list of single-key meta conditions ORed together.
	// A result must match at least one condition. Each element maps one meta key to
	// one or more values.
	OrMeta []map[string][]string
}

// DailyCount is registrations per day.
type DailyCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// PipelineCount is result sets per pipeline.
type PipelineCount struct {
	PipelineName string `json:"pipeline_name"`
	Count        int    `json:"count"`
}

// StatsResult is returned by GET /results/stats.
type StatsResult struct {
	Total     int             `json:"total"`
	Recent    []ResultSet     `json:"recent"`
	Daily     []DailyCount    `json:"daily"`
	Pipelines []PipelineCount `json:"pipelines"`
}

// SearchResult wraps a ResultSet with optional matched sample IDs.
type SearchResult struct {
	ResultSet      ResultSet `json:"result_set"`
	MatchedSamples []string  `json:"matched_samples,omitempty"`
}

// Store persists result sets in SQL.
type Store struct{ db *sql.DB }

// MLWHValidator validates seqmeta_* metadata fields against MLWH.
type MLWHValidator struct {
	q mlwh.Queryer
}

// CompositeKeyID returns the deterministic ID for a pipeline identifier and run key.
func CompositeKeyID(pipelineIdentifier, runKey string) string {
	var key []byte
	if strings.ContainsRune(pipelineIdentifier, '\x00') || strings.ContainsRune(runKey, '\x00') {
		key = make([]byte, 0, len(pipelineIdentifier)+len(runKey)+16)
		key = appendLengthPrefixedKeyPart(key, pipelineIdentifier)
		key = appendLengthPrefixedKeyPart(key, runKey)
	} else {
		key = append([]byte(pipelineIdentifier), '\x00')
		key = append(key, runKey...)
	}

	hash := sha256.Sum256(key)

	return hex.EncodeToString(hash[:])
}

func appendLengthPrefixedKeyPart(key []byte, value string) []byte {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	key = append(key, length[:]...)

	return append(key, value...)
}

// BuildRunKey returns a canonical query-encoded run key from non-empty parts.
//
// The primary value is now user-facing as "unique", but the stored query key
// remains "runid" so reruns of existing registrations keep the same ID.
func BuildRunKey(unique, additionalUnique string) string {
	values := url.Values{}

	if unique != "" {
		values.Set("runid", unique)
	}

	if additionalUnique != "" {
		values.Set("unique", additionalUnique)
	}

	return values.Encode()
}

// DisplayRunKeyUnique returns the user-facing unique label for a stored run key.
func DisplayRunKeyUnique(runKey string) string {
	trimmed := strings.TrimSpace(runKey)
	if trimmed == "" || !strings.Contains(trimmed, "=") {
		return trimmed
	}

	values, err := url.ParseQuery(trimmed)
	if err != nil {
		return trimmed
	}

	primary := firstQueryValue(values, "runid")
	additional := firstQueryValue(values, "unique")

	switch {
	case primary != "" && additional != "":
		return primary + " / " + additional
	case primary != "":
		return primary
	case additional != "":
		return additional
	default:
		return trimmed
	}
}

func firstQueryValue(values url.Values, key string) string {
	for _, value := range values[key] {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}
