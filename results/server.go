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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wtsi-hgi/wa/saga"
)

var combinedStudyMetaKeys = []string{
	"study",
	"study_id",
	"seqmeta_studyid",
	"seqmeta_study_accession",
}

var combinedLibraryMetaKeys = []string{
	"library",
	"seqmeta_library",
}

var combinedSampleMetaKeys = []string{
	"sample",
	"sample_id",
	"sample_name",
	"sample_accession_number",
	"seqmeta_sampleid",
	"seqmeta_sample_lims",
}

var combinedLaneMetaKeys = []string{"seqmeta_lane"}

const defaultSeqmetaResolverCacheTTL = 5 * time.Minute

type seqmetaResolvedValues struct {
	samples []string
	lanes   []string

	expiresAt time.Time
}

type seqmetaLaneForSearch struct {
	IDRun    string `json:"id_run"`
	Lane     string `json:"lane"`
	TagIndex int    `json:"tag_index"`
}

type seqmetaSampleDetailForSearch struct {
	Lanes []seqmetaLaneForSearch `json:"lanes"`
}

type seqmetaEnrichmentForSearch struct {
	Graph struct {
		Sample       *saga.MLWHSample              `json:"sample,omitempty"`
		Samples      []saga.MLWHSample             `json:"samples,omitempty"`
		SampleDetail *seqmetaSampleDetailForSearch `json:"sample_detail,omitempty"`
	} `json:"graph"`
}

// SeqmetaSampleResolver resolves study IDs to seqmeta sample IDs.
type SeqmetaSampleResolver struct {
	baseURL string
	client  *http.Client

	cacheTTL time.Duration
	cacheMu  sync.Mutex
	cache    map[string]seqmetaResolvedValues
}

// NewSeqmetaSampleResolver constructs a study sample resolver for the given seqmeta service URL.
func NewSeqmetaSampleResolver(baseURL string, timeout time.Duration) *SeqmetaSampleResolver {
	return &SeqmetaSampleResolver{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client: &http.Client{
			Timeout: timeout,
		},
		cacheTTL: defaultSeqmetaResolverCacheTTL,
		cache:    map[string]seqmetaResolvedValues{},
	}
}

// SamplesForStudy returns the unique Sanger sample IDs associated with a study.
func (r *SeqmetaSampleResolver) SamplesForStudy(ctx context.Context, studyID string) ([]string, error) {
	samples, _, err := r.SamplesAndLanesForStudy(ctx, studyID)

	return samples, err
}

// SamplesAndLanesForStudy returns unique sample IDs and lane IDs associated with a study.
func (r *SeqmetaSampleResolver) SamplesAndLanesForStudy(ctx context.Context, studyID string) ([]string, []string, error) {
	if r == nil || r.baseURL == "" {
		return nil, nil, fmt.Errorf("%w: resolver is not configured", ErrSeqmetaFailed)
	}

	cacheKey := "study:" + strings.TrimSpace(studyID)
	if cached, ok := r.cacheGet(cacheKey); ok {
		return cached.samples, cached.lanes, nil
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		r.baseURL+"/study/"+url.PathEscape(studyID)+"/samples",
		nil,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: build request: %w", ErrSeqmetaFailed, err)
	}

	response, err := r.client.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: request seqmeta samples: %w", ErrSeqmetaFailed, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%w: unexpected status %d", ErrSeqmetaFailed, response.StatusCode)
	}

	var samples []saga.MLWHSample
	if err := json.NewDecoder(response.Body).Decode(&samples); err != nil {
		return nil, nil, fmt.Errorf("%w: decode response: %w", ErrSeqmetaFailed, err)
	}

	resolvedSamples := uniqueSangerIDs(samples)
	resolvedLanes := uniqueLaneIDs(samples)
	r.cachePut(cacheKey, resolvedSamples, resolvedLanes)

	return resolvedSamples, resolvedLanes, nil
}

// SamplesAndLanesForLibrary returns sample and lane identifiers for a library type.
func (r *SeqmetaSampleResolver) SamplesAndLanesForLibrary(ctx context.Context, libraryType string) ([]string, []string, error) {
	if r == nil || r.baseURL == "" {
		return nil, nil, fmt.Errorf("%w: resolver is not configured", ErrSeqmetaFailed)
	}

	trimmed := strings.TrimSpace(libraryType)
	if trimmed == "" {
		return []string{}, []string{}, nil
	}

	cacheKey := "library:" + trimmed
	if cached, ok := r.cacheGet(cacheKey); ok {
		return cached.samples, cached.lanes, nil
	}

	enrichment, err := r.fetchEnrichment(ctx, trimmed)
	if err != nil {
		return nil, nil, err
	}

	resolvedSamples := uniqueSangerIDs(enrichment.Graph.Samples)
	resolvedLanes := uniqueLaneIDs(enrichment.Graph.Samples)
	r.cachePut(cacheKey, resolvedSamples, resolvedLanes)

	return resolvedSamples, resolvedLanes, nil
}

// LanesForSample returns lane identifiers related to a sample identifier.
func (r *SeqmetaSampleResolver) LanesForSample(ctx context.Context, sampleID string) ([]string, error) {
	if r == nil || r.baseURL == "" {
		return nil, fmt.Errorf("%w: resolver is not configured", ErrSeqmetaFailed)
	}

	trimmed := strings.TrimSpace(sampleID)
	if trimmed == "" {
		return []string{}, nil
	}

	cacheKey := "sample:" + trimmed
	if cached, ok := r.cacheGet(cacheKey); ok {
		return cached.lanes, nil
	}

	enrichment, err := r.fetchEnrichment(ctx, trimmed)
	if err != nil {
		return nil, err
	}

	lanes := make([]string, 0)
	seen := map[string]struct{}{}
	if enrichment.Graph.SampleDetail != nil {
		for _, lane := range enrichment.Graph.SampleDetail.Lanes {
			laneID := buildLaneID(lane.IDRun, lane.Lane, lane.TagIndex)
			if laneID == "" {
				continue
			}
			if _, ok := seen[laneID]; ok {
				continue
			}
			seen[laneID] = struct{}{}
			lanes = append(lanes, laneID)
		}
	}

	if enrichment.Graph.Sample != nil {
		laneID := buildLaneID(
			strconv.Itoa(enrichment.Graph.Sample.IDRun),
			strconv.Itoa(enrichment.Graph.Sample.Lane),
			enrichment.Graph.Sample.TagIndex,
		)
		if laneID != "" {
			if _, ok := seen[laneID]; !ok {
				lanes = append(lanes, laneID)
			}
		}
	}

	r.cachePut(cacheKey, nil, lanes)

	return lanes, nil
}

func (r *SeqmetaSampleResolver) fetchEnrichment(ctx context.Context, identifier string) (*seqmetaEnrichmentForSearch, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		r.baseURL+"/enrich/"+url.PathEscape(identifier),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %w", ErrSeqmetaFailed, err)
	}

	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: request seqmeta enrich: %w", ErrSeqmetaFailed, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: unexpected status %d", ErrSeqmetaFailed, response.StatusCode)
	}

	var enrichment seqmetaEnrichmentForSearch
	if err := json.NewDecoder(response.Body).Decode(&enrichment); err != nil {
		return nil, fmt.Errorf("%w: decode enrichment: %w", ErrSeqmetaFailed, err)
	}

	return &enrichment, nil
}

func (r *SeqmetaSampleResolver) cacheGet(key string) (seqmetaResolvedValues, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	entry, ok := r.cache[key]
	if !ok {
		return seqmetaResolvedValues{}, false
	}

	if time.Now().After(entry.expiresAt) {
		delete(r.cache, key)

		return seqmetaResolvedValues{}, false
	}

	return entry, true
}

func (r *SeqmetaSampleResolver) cachePut(key string, samples, lanes []string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	r.cache[key] = seqmetaResolvedValues{
		samples:   samples,
		lanes:     lanes,
		expiresAt: time.Now().Add(r.cacheTTL),
	}
}

func uniqueLaneIDs(samples []saga.MLWHSample) []string {
	laneIDs := make([]string, 0, len(samples))
	seen := make(map[string]struct{}, len(samples))

	for _, sample := range samples {
		laneID := buildLaneID(
			strconv.Itoa(sample.IDRun),
			strconv.Itoa(sample.Lane),
			sample.TagIndex,
		)
		if laneID == "" {
			continue
		}

		if _, ok := seen[laneID]; ok {
			continue
		}

		seen[laneID] = struct{}{}
		laneIDs = append(laneIDs, laneID)
	}

	return laneIDs
}

func buildLaneID(idRun, lane string, tagIndex int) string {
	run := strings.TrimSpace(idRun)
	laneValue := strings.TrimSpace(lane)
	if run == "" || laneValue == "" || tagIndex <= 0 {
		return ""
	}

	return run + "_" + laneValue + "#" + strconv.Itoa(tagIndex)
}

func uniqueSangerIDs(samples []saga.MLWHSample) []string {
	ids := make([]string, 0, len(samples))
	seen := make(map[string]struct{}, len(samples))

	for _, sample := range samples {
		if sample.SangerID == "" {
			continue
		}

		if _, ok := seen[sample.SangerID]; ok {
			continue
		}

		seen[sample.SangerID] = struct{}{}
		ids = append(ids, sample.SangerID)
	}

	return ids
}

// Server serves the results REST API.
type Server struct {
	store           *Store
	validator       *SeqmetaValidator
	resolver        *SeqmetaSampleResolver
	handler         http.Handler
	maxPreviewBytes int64
}

// NewServer constructs a results API server.
func NewServer(store *Store, validator *SeqmetaValidator, resolver *SeqmetaSampleResolver, opts ...ServerOption) *Server {
	server := &Server{
		store:           store,
		validator:       validator,
		resolver:        resolver,
		maxPreviewBytes: DefaultMaxPreviewBytes,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}

	router := chi.NewRouter()
	router.Get("/results", server.handleGetResults)
	router.Post("/results", server.handlePostResults)
	router.Get("/results/stats", server.handleGetStats)
	router.Get("/results/meta-keys", server.handleGetMetaKeys)
	router.Get("/results/{id}/file", server.handleGetFile)
	router.Get("/results/{id}/files", server.handleGetResultFiles)
	router.Put("/results/{id}/files", server.handlePutResultFiles)
	router.Get("/results/{id}", server.handleGetResultByID)
	router.Delete("/results/{id}", server.handleDeleteResultByID)
	server.handler = router

	return server
}

// Handler returns the configured HTTP handler.
func (s *Server) Handler() http.Handler {
	if s == nil || s.handler == nil {
		return http.NotFoundHandler()
	}

	return s.handler
}

func (s *Server) handlePostResults(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeServerError(w, http.StatusInternalServerError, "server store is not configured")

		return
	}

	registration, err := decodeRegistration(r.Body)
	if err != nil {
		writeServerError(w, http.StatusBadRequest, "invalid JSON body")

		return
	}

	if err := s.validator.ValidateMetadata(r.Context(), registration.Metadata); err != nil {
		writeDomainError(w, err)

		return
	}

	result, err := s.store.Upsert(r.Context(), registration)
	if err != nil {
		writeDomainError(w, err)

		return
	}

	status := http.StatusOK
	if result.CreatedAt.Equal(result.UpdatedAt) {
		status = http.StatusCreated
	}

	writeJSON(w, status, result)
}

func writeServerError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeRegistration(body io.ReadCloser) (*Registration, error) {
	var registration Registration

	if err := decodeJSONBody(body, &registration); err != nil {
		return nil, err
	}

	return &registration, nil
}

func multiSearchParamsFromRequest(r *http.Request) MultiSearchParams {
	query := r.URL.Query()
	params := MultiSearchParams{
		Requester:          nonEmptySearchValues(query["user"]),
		Operator:           nonEmptySearchValues(query["operator"]),
		PipelineName:       nonEmptySearchValues(query["pipeline_name"]),
		PipelineVersion:    nonEmptySearchValues(query["pipeline_version"]),
		PipelineIdentifier: nonEmptySearchValues(query["pipeline_identifier"]),
		RunKey:             nonEmptySearchValues(query["run_key"]),
		OutputDirPrefix:    nonEmptySearchValues(query["output_dir_prefix"]),
		Meta:               map[string][]string{},
	}

	for key, values := range query {
		values = nonEmptySearchValues(values)
		if len(values) == 0 {
			continue
		}

		switch {
		case strings.HasPrefix(key, "meta_"):
			if metaKey := strings.TrimPrefix(key, "meta_"); metaKey != "" {
				params.Meta[metaKey] = values
			}
		case strings.HasPrefix(key, "seqmeta_"):
			params.Meta[key] = values
		}
	}

	return params
}

func combinedStudySearchValues(r *http.Request) []string {
	return mergeSearchValues(
		combinedSearchValues(r, "study"),
		combinedSearchValues(r, "study_id"),
	)
}

func combinedSearchValues(r *http.Request, key string) []string {
	return nonEmptySearchValues(r.URL.Query()[key])
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeServerError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrFileGone):
		writeServerError(w, http.StatusGone, "file not found on disk")
	case errors.Is(err, ErrFileTooLarge):
		writeServerError(w, http.StatusRequestEntityTooLarge, "file exceeds preview limit")
	case errors.Is(err, ErrNotFound):
		writeServerError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSeqmetaRejected):
		writeServerError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrSeqmetaFailed):
		writeServerError(w, http.StatusBadGateway, err.Error())
	default:
		writeServerError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handlePutResultFiles(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeServerError(w, http.StatusInternalServerError, "server store is not configured")

		return
	}

	files, err := decodeFileEntries(r.Body)
	if err != nil {
		writeServerError(w, http.StatusBadRequest, "invalid JSON body")

		return
	}

	resultID := chi.URLParam(r, "id")

	if err := s.store.ReplaceOutputFiles(r.Context(), resultID, files); err != nil {
		writeDomainError(w, err)

		return
	}

	storedFiles, err := s.store.GetFiles(r.Context(), resultID)
	if err != nil {
		writeDomainError(w, err)

		return
	}

	writeJSON(w, http.StatusOK, storedFiles)
}

func decodeFileEntries(body io.ReadCloser) ([]FileEntry, error) {
	var files []FileEntry

	if err := decodeJSONBody(body, &files); err != nil {
		return nil, err
	}

	return files, nil
}

func (s *Server) handleGetResults(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeServerError(w, http.StatusInternalServerError, "server store is not configured")

		return
	}

	params := multiSearchParamsFromRequest(r)
	studyValues := combinedStudySearchValues(r)
	libraryValues := combinedSearchValues(r, "library")
	sampleValues := combinedSearchValues(r, "sample")
	laneValues := combinedSearchValues(r, "seqmeta_lane")
	directSampleValues := append([]string{}, sampleValues...)

	libraryValues = mergeSearchValues(libraryValues, params.Meta["library"])
	libraryValues = mergeSearchValues(libraryValues, params.Meta["seqmeta_library"])
	sampleValues = mergeSearchValues(sampleValues, params.Meta["seqmeta_sampleid"])
	sampleValues = mergeSearchValues(sampleValues, params.Meta["seqmeta_sample_lims"])
	sampleValues = mergeSearchValues(sampleValues, params.Meta["sample_name"])
	sampleValues = mergeSearchValues(sampleValues, params.Meta["sample_id"])
	sampleValues = mergeSearchValues(sampleValues, params.Meta["sample_accession_number"])
	sampleValues = mergeSearchValues(sampleValues, params.Meta["sample"])
	directSampleValues = mergeSearchValues(directSampleValues, sampleValues)
	laneValues = mergeSearchValues(laneValues, params.Meta["seqmeta_lane"])
	studyValues = mergeSearchValues(studyValues, params.Meta["seqmeta_studyid"])
	studyValues = mergeSearchValues(studyValues, params.Meta["seqmeta_study_accession"])
	delete(params.Meta, "seqmeta_studyid")
	delete(params.Meta, "seqmeta_study_accession")
	delete(params.Meta, "library")
	delete(params.Meta, "seqmeta_library")
	delete(params.Meta, "seqmeta_lane")

	delete(params.Meta, "seqmeta_sampleid")
	delete(params.Meta, "seqmeta_sample_lims")
	delete(params.Meta, "sample_name")
	delete(params.Meta, "sample_id")
	delete(params.Meta, "sample_accession_number")
	delete(params.Meta, "sample")

	legacyStudyIDUsed := len(combinedSearchValues(r, "study_id")) > 0

	resolvedSamples := []string{}
	resolvedLanes := []string{}
	if len(studyValues) > 0 {
		if s.resolver == nil || strings.TrimSpace(s.resolver.baseURL) == "" {
			if legacyStudyIDUsed {
				writeServerError(w, http.StatusBadRequest, "seqmeta not configured")

				return
			}
		} else {
			for _, studyValue := range studyValues {
				samples, lanes, err := s.resolver.SamplesAndLanesForStudy(r.Context(), studyValue)
				if err != nil {
					writeDomainError(w, err)

					return
				}

				resolvedSamples = mergeSearchValues(resolvedSamples, samples)
				resolvedLanes = mergeSearchValues(resolvedLanes, lanes)
			}

			sampleValues = mergeSearchValues(sampleValues, resolvedSamples)
			laneValues = mergeSearchValues(laneValues, resolvedLanes)
		}
	}

	if len(libraryValues) > 0 && s.resolver != nil && strings.TrimSpace(s.resolver.baseURL) != "" {
		for _, libraryValue := range libraryValues {
			samples, lanes, err := s.resolver.SamplesAndLanesForLibrary(r.Context(), libraryValue)
			if err != nil {
				writeDomainError(w, err)

				return
			}

			sampleValues = mergeSearchValues(sampleValues, samples)
			laneValues = mergeSearchValues(laneValues, lanes)
		}
	}

	if len(directSampleValues) > 0 &&
		len(studyValues) == 0 &&
		len(libraryValues) == 0 &&
		s.resolver != nil &&
		strings.TrimSpace(s.resolver.baseURL) != "" {
		for _, sampleValue := range directSampleValues {
			lanes, err := s.resolver.LanesForSample(r.Context(), sampleValue)
			if err != nil {
				writeDomainError(w, err)

				return
			}

			laneValues = mergeSearchValues(laneValues, lanes)
		}
	}

	for _, key := range combinedStudyMetaKeys {
		if len(studyValues) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{key: studyValues})
		}
	}

	for _, key := range combinedSampleMetaKeys {
		if len(sampleValues) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{key: sampleValues})
		}
	}

	for _, key := range combinedLibraryMetaKeys {
		if len(libraryValues) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{key: libraryValues})
		}
	}

	for _, key := range combinedLaneMetaKeys {
		if len(laneValues) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{key: laneValues})
		}
	}

	results, err := s.store.SearchMulti(r.Context(), params)
	if err != nil {
		writeDomainError(w, err)

		return
	}

	if len(studyValues) > 0 && len(resolvedSamples) > 0 {
		writeJSON(w, http.StatusOK, wrapSearchResults(results, resolvedSamples))

		return
	}

	writeJSON(w, http.StatusOK, results)
}

func mergeSearchValues(existing []string, incoming []string) []string {
	merged := make([]string, 0, len(existing)+len(incoming))
	seen := make(map[string]struct{}, len(existing)+len(incoming))

	for _, value := range append(existing, incoming...) {
		if value == "" {
			continue
		}

		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}
		merged = append(merged, value)
	}

	return merged
}

func wrapSearchResults(results []ResultSet, resolvedSamples []string) []SearchResult {
	wrapped := make([]SearchResult, 0, len(results))
	resolved := make(map[string]struct{}, len(resolvedSamples))

	for _, sampleID := range resolvedSamples {
		resolved[sampleID] = struct{}{}
	}

	for _, result := range results {
		wrappedResult := SearchResult{ResultSet: result}

		if sampleID := strings.TrimSpace(result.Metadata["seqmeta_sampleid"]); sampleID != "" {
			if _, ok := resolved[sampleID]; ok {
				wrappedResult.MatchedSamples = []string{sampleID}
			}
		}

		wrapped = append(wrapped, wrappedResult)
	}

	return wrapped
}

func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeServerError(w, http.StatusInternalServerError, "server store is not configured")

		return
	}

	recent, err := nonNegativeIntQueryValue(r, "recent", 10)
	if err != nil {
		writeServerError(w, http.StatusBadRequest, err.Error())

		return
	}

	days, err := nonNegativeIntQueryValue(r, "days", 30)
	if err != nil {
		writeServerError(w, http.StatusBadRequest, err.Error())

		return
	}

	stats, err := s.store.Stats(r.Context(), recent, days)
	if err != nil {
		writeDomainError(w, err)

		return
	}

	writeJSON(w, http.StatusOK, stats)
}

func nonNegativeIntQueryValue(r *http.Request, key string, defaultValue int) (int, error) {
	rawValue := strings.TrimSpace(r.URL.Query().Get(key))
	if rawValue == "" {
		return defaultValue, nil
	}

	value, err := strconv.Atoi(rawValue)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid %s query parameter", key)
	}

	return value, nil
}

func (s *Server) handleGetMetaKeys(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeServerError(w, http.StatusInternalServerError, "server store is not configured")

		return
	}

	keys, err := s.store.MetaKeys(r.Context())
	if err != nil {
		writeDomainError(w, err)

		return
	}

	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) handleGetResultByID(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeServerError(w, http.StatusInternalServerError, "server store is not configured")

		return
	}

	result, err := s.store.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeDomainError(w, err)

		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetResultFiles(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeServerError(w, http.StatusInternalServerError, "server store is not configured")

		return
	}

	resultID := chi.URLParam(r, "id")
	files, err := s.store.GetFiles(r.Context(), resultID)
	if err != nil {
		writeDomainError(w, err)

		return
	}

	if len(files) == 0 {
		if _, err := s.store.Get(r.Context(), resultID); err != nil {
			writeDomainError(w, err)

			return
		}
	}

	writeJSON(w, http.StatusOK, files)
}

func (s *Server) handleDeleteResultByID(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeServerError(w, http.StatusInternalServerError, "server store is not configured")

		return
	}

	err := s.store.Delete(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeDomainError(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func decodeJSONBody(body io.ReadCloser, target any) error {
	defer func() {
		_ = body.Close()
	}()

	decoder := json.NewDecoder(body)

	if err := decoder.Decode(target); err != nil {
		return err
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON")
		}

		return err
	}

	return nil
}
