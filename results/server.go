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

	"github.com/gin-gonic/gin"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/mlwh"
)

var combinedStudyMetaKeys = []string{
	"study",
	"study_id",
	SeqmetaIDStudyLimsKey,
	LegacySeqmetaStudyIDKey,
	"seqmeta_study_accession",
}

var libraryTypeMetaKeys = []string{
	"library",
	SeqmetaPipelineIDLimsKey,
	LegacySeqmetaLibraryKey,
	LegacySeqmetaLibraryTypeKey,
}

var libraryIDMetaKeys = []string{SeqmetaLibraryIDKey, LegacySeqmetaLibraryIDKey}

var libraryLimsMetaKeys = []string{SeqmetaIDLibraryLimsKey, LegacySeqmetaLibraryLimsKey}

var combinedRunMetaKeys = []string{
	"run",
	"run_id",
	SeqmetaIDRunKey,
	LegacySeqmetaRunIDKey,
}

var combinedSampleMetaKeys = []string{
	"sample",
	"sample_id",
	"sample_name",
	"sample_accession_number",
	SeqmetaSampleNameKey,
	SeqmetaSampleNameURLKey,
	SeqmetaSangerSampleIDKey,
	SeqmetaIDSampleLimsKey,
	LegacySeqmetaSampleIDKey,
	LegacySeqmetaSampleLimsKey,
}

var candidateSampleNameMetaKeys = []string{
	"sample",
	"sample_id",
	"sample_name",
	SeqmetaSampleNameKey,
	SeqmetaSampleNameURLKey,
	LegacySeqmetaSampleIDKey,
}

var combinedLaneMetaKeys = []string{"seqmeta_lane"}

const defaultSeqmetaResolverCacheTTL = 5 * time.Minute

const (
	currentUserGinContextKey      = "wa_current_user"
	goAuthserverUserGinContextKey = "user"
)

type sampleMetadataSearchKey struct {
	key  string
	kind mlwh.IdentifierKind
}

var sampleMetadataSearchKeys = []sampleMetadataSearchKey{
	{key: SeqmetaSampleNameKey, kind: mlwh.KindSangerSampleName},
	{key: SeqmetaSampleNameURLKey, kind: mlwh.KindSangerSampleName},
	{key: LegacySeqmetaSampleIDKey, kind: mlwh.KindSangerSampleName},
	{key: "sample_name", kind: mlwh.KindSangerSampleName},
	{key: "sample_id", kind: mlwh.KindSangerSampleName},
	{key: SeqmetaSangerSampleIDKey, kind: mlwh.KindSangerSampleID},
	{key: SeqmetaIDSampleLimsKey, kind: mlwh.KindSampleLimsID},
	{key: LegacySeqmetaSampleLimsKey, kind: mlwh.KindSampleLimsID},
	{key: SeqmetaSupplierNameKey, kind: mlwh.KindSupplierName},
	{key: SeqmetaAccessionNumberKey, kind: mlwh.KindSampleAccession},
	{key: "sample_accession_number", kind: mlwh.KindSampleAccession},
}

type sampleSearchExpansion struct {
	kind   mlwh.IdentifierKind
	values []string
}

type seqmetaResolvedValues struct {
	samples []string
	runs    []string
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

type seqmetaSampleForSearch struct {
	SangerID string `json:"sanger_id"`
	IDRun    int    `json:"id_run"`
	Lane     int    `json:"lane"`
	TagIndex int    `json:"tag_index"`
}

type seqmetaEnrichmentForSearch struct {
	Graph struct {
		Sample       *seqmetaSampleForSearch       `json:"sample,omitempty"`
		Samples      []seqmetaSampleForSearch      `json:"samples,omitempty"`
		SampleDetail *seqmetaSampleDetailForSearch `json:"sample_detail,omitempty"`
	} `json:"graph"`
}

// SearchResolver expands search values into related sample, run, and lane values.
type SearchResolver interface {
	Expand(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]string, []string, []string, error)
}

type candidateSampleSearchResolver interface {
	ExpandCandidateSampleSearchValues(ctx context.Context, kind mlwh.IdentifierKind, canonical string, candidates []string) ([]string, error)
}

type studySearchCanonicalizer interface {
	CanonicalStudySearchValue(context.Context, string) (string, error)
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

	var samples []seqmetaSampleForSearch
	if err := json.NewDecoder(response.Body).Decode(&samples); err != nil {
		return nil, nil, fmt.Errorf("%w: decode response: %w", ErrSeqmetaFailed, err)
	}

	resolvedSamples := uniqueSangerIDs(samples)
	resolvedRuns := uniqueRunIDs(samples)
	resolvedLanes := uniqueLaneIDs(samples)
	r.cachePut(cacheKey, resolvedSamples, resolvedRuns, resolvedLanes)

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
	resolvedRuns := uniqueRunIDs(enrichment.Graph.Samples)
	resolvedLanes := uniqueLaneIDs(enrichment.Graph.Samples)
	r.cachePut(cacheKey, resolvedSamples, resolvedRuns, resolvedLanes)

	return resolvedSamples, resolvedLanes, nil
}

// Expand returns the related search identifiers for a canonical identifier.
func (r *SeqmetaSampleResolver) Expand(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]string, []string, []string, error) {
	trimmed := strings.TrimSpace(canonical)
	if trimmed == "" {
		return []string{}, []string{}, []string{}, nil
	}

	switch kind {
	case mlwh.KindStudyLimsID:
		samples, lanes, err := r.SamplesAndLanesForStudy(ctx, trimmed)
		if err != nil {
			return nil, nil, nil, err
		}

		return samples, runIDsFromLaneValues(lanes), lanes, nil
	case mlwh.KindLibraryType:
		samples, lanes, err := r.SamplesAndLanesForLibrary(ctx, trimmed)
		if err != nil {
			return nil, nil, nil, err
		}

		return samples, runIDsFromLaneValues(lanes), lanes, nil
	case mlwh.KindSangerSampleName:
		lanes, err := r.LanesForSample(ctx, trimmed)
		if err != nil {
			return nil, nil, nil, err
		}

		return []string{trimmed}, runIDsFromLaneValues(lanes), lanes, nil
	case mlwh.KindRunID:
		if r == nil || r.baseURL == "" {
			return nil, nil, nil, fmt.Errorf("%w: resolver is not configured", ErrSeqmetaFailed)
		}

		cacheKey := "run:" + trimmed
		if cached, ok := r.cacheGet(cacheKey); ok {
			return cached.samples, cached.runs, cached.lanes, nil
		}

		enrichment, err := r.fetchEnrichment(ctx, trimmed)
		if err != nil {
			return nil, nil, nil, err
		}

		samples := uniqueSangerIDs(enrichment.Graph.Samples)
		runs := uniqueRunIDs(enrichment.Graph.Samples)
		if len(runs) == 0 {
			runs = []string{trimmed}
		}
		lanes := uniqueLaneIDs(enrichment.Graph.Samples)
		r.cachePut(cacheKey, samples, runs, lanes)

		return samples, runs, lanes, nil
	default:
		return []string{}, []string{}, []string{}, nil
	}
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

	r.cachePut(cacheKey, nil, runIDsFromLaneValues(lanes), lanes)

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

func (r *SeqmetaSampleResolver) cachePut(key string, samples, runs, lanes []string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	r.cache[key] = seqmetaResolvedValues{
		samples:   samples,
		runs:      runs,
		lanes:     lanes,
		expiresAt: time.Now().Add(r.cacheTTL),
	}
}

func uniqueLaneIDs(samples []seqmetaSampleForSearch) []string {
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

func uniqueRunIDs(samples []seqmetaSampleForSearch) []string {
	runIDs := make([]string, 0, len(samples))
	seen := make(map[string]struct{}, len(samples))

	for _, sample := range samples {
		if sample.IDRun <= 0 {
			continue
		}

		runID := strconv.Itoa(sample.IDRun)
		if _, ok := seen[runID]; ok {
			continue
		}

		seen[runID] = struct{}{}
		runIDs = append(runIDs, runID)
	}

	return runIDs
}

func runIDsFromLaneValues(laneValues []string) []string {
	runIDs := make([]string, 0, len(laneValues))
	seen := make(map[string]struct{}, len(laneValues))

	for _, laneValue := range laneValues {
		runID, _, ok := strings.Cut(laneValue, "_")
		if !ok || runID == "" {
			continue
		}

		if _, ok := seen[runID]; ok {
			continue
		}

		seen[runID] = struct{}{}
		runIDs = append(runIDs, runID)
	}

	return runIDs
}

func buildLaneID(idRun, lane string, tagIndex int) string {
	run := strings.TrimSpace(idRun)
	laneValue := strings.TrimSpace(lane)
	if run == "" || laneValue == "" || tagIndex <= 0 {
		return ""
	}

	return run + "_" + laneValue + "#" + strconv.Itoa(tagIndex)
}

func uniqueSangerIDs(samples []seqmetaSampleForSearch) []string {
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

// SessionResponse reports the authenticated caller's current session state.
type SessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username"`
	IsOwner       bool   `json:"is_owner"`
}

// LockedResponse is returned when a caller cannot access an existing result.
type LockedResponse struct {
	Error    string `json:"error"`
	Locked   bool   `json:"locked"`
	ResultID string `json:"result_id,omitempty"`
	Message  string `json:"message"`
}

// Server serves the results REST API.
type Server struct {
	store           *Store
	validator       *SeqmetaValidator
	resolver        SearchResolver
	handler         http.Handler
	maxPreviewBytes int64
	ownerSessions   OwnerSessionStore
}

// NewServer constructs a results API server.
func NewServer(store *Store, validator *SeqmetaValidator, resolver SearchResolver, opts ...ServerOption) *Server {
	server := &Server{
		store:           store,
		validator:       validator,
		resolver:        resolver,
		maxPreviewBytes: DefaultMaxPreviewBytes,
		ownerSessions:   NewOwnerSessionStore(),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}

	return server
}

func (s *Server) newHandler() http.Handler {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	s.RegisterRoutes(router, nil)

	return router
}

// RegisterRoutes registers the results API routes on the provided Gin routers.
func (s *Server) RegisterRoutes(router *gin.Engine, auth *gin.RouterGroup) {
	if router != nil {
		router.GET(gas.EndPointREST+"/results", s.handleGetResults)
		router.GET(gas.EndPointREST+"/results/stats", s.handleGetStats)
		router.GET(gas.EndPointREST+"/results/meta-keys", s.handleGetMetaKeys)
		router.GET(gas.EndPointREST+"/results/:id/file", s.handleGetFile)
		router.GET(gas.EndPointREST+"/results/:id/files", s.handleGetResultFiles)
		router.GET(gas.EndPointREST+"/results/:id", s.handleGetResultByID)
	}

	if auth != nil {
		auth.GET("/session", s.handleGetSession)
		auth.POST("/logout", s.handlePostLogout)
		auth.GET("/results", s.handleGetResults)
		auth.GET("/results/stats", s.handleGetStats)
		auth.POST("/results", s.handlePostResults)
		auth.GET("/results/:id/file", s.handleGetFile)
		auth.GET("/results/:id/files", s.handleGetResultFiles)
		auth.PUT("/results/:id/files", s.handlePutResultFiles)
		auth.GET("/results/:id", s.handleGetResultByID)
		auth.DELETE("/results/:id", s.handleDeleteResultByID)
	}
}

// Handler returns the configured HTTP handler.
func (s *Server) Handler() http.Handler {
	if s == nil {
		return http.NotFoundHandler()
	}

	if s.handler == nil {
		s.handler = s.newHandler()
	}

	return s.handler
}

func (s *Server) handleGetSession(c *gin.Context) {
	user, err := CurrentUserFromContext(c, s.ownerSessions)
	if err != nil {
		writeServerError(c, http.StatusInternalServerError, err.Error())

		return
	}

	if user == nil || user.Username == "" {
		writeServerError(c, http.StatusUnauthorized, "authentication required")

		return
	}

	writeJSON(c, http.StatusOK, SessionResponse{
		Authenticated: true,
		Username:      user.Username,
		IsOwner:       user.IsOwner,
	})
}

func (s *Server) handlePostLogout(c *gin.Context) {
	if s != nil && s.ownerSessions != nil {
		s.ownerSessions.Delete(rawJWTFromRequest(c.Request))
	}

	c.Status(http.StatusNoContent)
}

func (s *Server) handlePostResults(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	registration, err := decodeRegistration(c.Request.Body)
	if err != nil {
		writeServerError(c, http.StatusBadRequest, "invalid JSON body")

		return
	}

	if err := s.applyRegistrationAuthPolicy(c, registration); err != nil {
		writeDomainError(c, err)

		return
	}

	outputDirectoryGID, err := OutputDirectoryGID(registration.OutputDirectory)
	if err != nil {
		writeDomainError(c, fmt.Errorf("%w: %v", ErrInvalidInput, err))

		return
	}

	registration.OutputDirectoryGID = outputDirectoryGID

	if err := s.validator.ValidateMetadataValues(c.Request.Context(), normalizedRegistrationMetadataValues(registration)); err != nil {
		writeDomainError(c, err)

		return
	}

	result, err := s.store.Upsert(c.Request.Context(), registration)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	status := http.StatusOK
	if result.CreatedAt.Equal(result.UpdatedAt) {
		status = http.StatusCreated
	}

	result.Access = AccessState{CanView: true}
	writeJSON(c, status, result)
}

func (s *Server) applyRegistrationAuthPolicy(c *gin.Context, registration *Registration) error {
	user, err := CurrentUserFromContext(c, s.ownerSessions)
	if err != nil || user == nil || user.IsOwner {
		return err
	}

	registration.Operator = user.Username

	return nil
}

func (s *Server) accessUserFromContext(c *gin.Context) (*CurrentUser, error) {
	if !isAuthResultsRoute(c) {
		return nil, nil
	}

	return CurrentUserFromContext(c, s.ownerSessions)
}

func isAuthResultsRoute(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}

	path := c.Request.URL.Path

	return path == gas.EndPointAuth+"/results" || strings.HasPrefix(path, gas.EndPointAuth+"/results/")
}

func (s *Server) requireResultAccess(c *gin.Context, result ResultSet) bool {
	_, ok := s.resultAccessForRequest(c, result)

	return ok
}

func (s *Server) resultAccessForRequest(c *gin.Context, result ResultSet) (AccessState, bool) {
	user, err := s.accessUserFromContext(c)
	if err != nil {
		writeDomainError(c, err)

		return AccessState{}, false
	}

	access, err := AccessForResult(result, user)
	if err != nil {
		writeDomainError(c, err)

		return AccessState{}, false
	}

	if !access.CanView {
		WriteLocked(c, result.ID)

		return access, false
	}

	return access, true
}

// WriteLocked writes the stable locked response for an existing inaccessible result.
func WriteLocked(c *gin.Context, resultID string) {
	writeJSON(c, http.StatusForbidden, LockedResponse{
		Error:    "locked",
		Locked:   true,
		ResultID: resultID,
		Message:  "You do not have access to this result set",
	})
}

func (s *Server) requireServerOwner(c *gin.Context, resultID string) bool {
	user, err := CurrentUserFromContext(c, s.ownerSessions)
	if err != nil {
		writeDomainError(c, err)

		return false
	}

	if err := RequireServerOwner(user); err != nil {
		if errors.Is(err, ErrLocked) {
			WriteLocked(c, resultID)

			return false
		}

		writeDomainError(c, err)

		return false
	}

	return true
}

func writeServerError(c *gin.Context, status int, message string) {
	writeJSON(c, status, map[string]string{"error": message})
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

func appendSampleSearchExpansion(existing []sampleSearchExpansion, kind mlwh.IdentifierKind, values []string) []sampleSearchExpansion {
	values = nonEmptySearchValues(values)
	if len(values) == 0 {
		return existing
	}

	for index := range existing {
		if existing[index].kind != kind {
			continue
		}

		existing[index].values = mergeSearchValues(existing[index].values, values)

		return existing
	}

	return append(existing, sampleSearchExpansion{kind: kind, values: values})
}

func canonicalStudySearchValues(ctx context.Context, resolver SearchResolver, values []string) ([]string, []string, error) {
	searchValues := append([]string{}, nonEmptySearchValues(values)...)
	expansionValues := append([]string{}, searchValues...)

	canonicalizer, ok := resolver.(studySearchCanonicalizer)
	if !ok {
		return searchValues, expansionValues, nil
	}

	expansionValues = []string{}
	for _, value := range searchValues {
		canonical, err := canonicalizer.CanonicalStudySearchValue(ctx, value)
		if err != nil {
			return nil, nil, err
		}
		if canonical == "" {
			canonical = value
		}

		searchValues = mergeSearchValues(searchValues, []string{canonical})
		expansionValues = mergeSearchValues(expansionValues, []string{canonical})
	}

	return searchValues, expansionValues, nil
}

func writeDomainError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeServerError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrFileGone):
		writeServerError(c, http.StatusGone, "file not found on disk")
	case errors.Is(err, ErrFileTooLarge):
		writeServerError(c, http.StatusRequestEntityTooLarge, "file exceeds preview limit")
	case errors.Is(err, ErrLocked):
		writeServerError(c, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrNotFound):
		writeServerError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSeqmetaRejected):
		writeServerError(c, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrSeqmetaFailed):
		writeServerError(c, http.StatusBadGateway, err.Error())
	default:
		writeServerError(c, http.StatusInternalServerError, err.Error())
	}
}

func expandSampleSearchValues(ctx context.Context, resolver SearchResolver, requests []sampleSearchExpansion) ([]string, []string, []string, error) {
	resolvedSamples := []string{}
	resolvedRuns := []string{}
	resolvedLanes := []string{}

	for _, request := range requests {
		for _, value := range request.values {
			samples, runs, lanes, err := resolver.Expand(ctx, request.kind, value)
			if err != nil {
				return nil, nil, nil, err
			}

			resolvedSamples = mergeSearchValues(resolvedSamples, samples)
			resolvedRuns = mergeSearchValues(resolvedRuns, runs)
			resolvedLanes = mergeSearchValues(resolvedLanes, lanes)
		}
	}

	return resolvedSamples, resolvedRuns, resolvedLanes, nil
}

func expandCandidateSampleSearchValues(ctx context.Context, resolver SearchResolver, candidates []string, requests []sampleSearchExpansion) ([]string, []string, []string, bool, error) {
	candidateResolver, ok := resolver.(candidateSampleSearchResolver)
	if !ok || len(candidates) == 0 {
		return nil, nil, nil, false, nil
	}

	resolvedSamples := []string{}
	remaining := []sampleSearchExpansion{}

	for _, request := range requests {
		if !directSampleSearchKind(request.kind) {
			remaining = append(remaining, request)

			continue
		}

		for _, value := range request.values {
			samples, err := candidateResolver.ExpandCandidateSampleSearchValues(ctx, request.kind, value, candidates)
			if err != nil {
				if errors.Is(err, mlwh.ErrUnsupportedIdentifier) {
					return nil, nil, nil, false, nil
				}

				return nil, nil, nil, false, err
			}

			resolvedSamples = mergeSearchValues(resolvedSamples, samples)
		}
	}

	if len(remaining) == len(requests) {
		return nil, nil, nil, false, nil
	}

	if len(remaining) == 0 {
		return resolvedSamples, []string{}, []string{}, true, nil
	}

	samples, runs, lanes, err := expandSampleSearchValues(ctx, resolver, remaining)
	if err != nil {
		return nil, nil, nil, false, err
	}

	return mergeSearchValues(resolvedSamples, samples), runs, lanes, true, nil
}

// AnnotateAccess returns results with per-row access calculated for user.
func AnnotateAccess(results []ResultSet, user *CurrentUser) ([]ResultSet, error) {
	annotated := make([]ResultSet, len(results))

	for i, result := range results {
		access, err := AccessForResult(result, user)
		if err != nil {
			return nil, err
		}

		result.Access = access
		annotated[i] = result
	}

	return annotated, nil
}

func writeJSON(c *gin.Context, status int, payload any) {
	c.Writer.Header().Set("Content-Type", "application/json")
	c.JSON(status, payload)
	_, _ = c.Writer.Write([]byte("\n"))
}

func (s *Server) handlePutResultFiles(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	resultID := c.Param("id")
	if !s.requireServerOwner(c, resultID) {
		return
	}

	files, err := decodeFileEntries(c.Request.Body)
	if err != nil {
		writeServerError(c, http.StatusBadRequest, "invalid JSON body")

		return
	}

	if err := s.store.ReplaceOutputFiles(c.Request.Context(), resultID, files); err != nil {
		writeDomainError(c, err)

		return
	}

	storedFiles, err := s.store.GetFiles(c.Request.Context(), resultID)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	writeJSON(c, http.StatusOK, storedFiles)
}

func decodeFileEntries(body io.ReadCloser) ([]FileEntry, error) {
	var files []FileEntry

	if err := decodeJSONBody(body, &files); err != nil {
		return nil, err
	}

	return files, nil
}

func (s *Server) handleGetResults(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	r := c.Request
	params := multiSearchParamsFromRequest(r)
	studyValues := combinedStudySearchValues(r)
	libraryTypeValues := combinedSearchValues(r, "library")
	libraryIDValues := []string{}
	libraryLimsValues := []string{}
	runValues := mergeSearchValues(combinedSearchValues(r, "run"), combinedSearchValues(r, "run_id"))
	sampleValues := combinedSearchValues(r, "sample")
	laneValues := combinedSearchValues(r, "seqmeta_lane")
	sampleExpansionRequests := appendSampleSearchExpansion(nil, mlwh.KindSangerSampleName, sampleValues)
	directRunValues := append([]string{}, runValues...)
	directSampleExactValues := map[string][]string{}

	libraryTypeValues = mergeSearchValues(libraryTypeValues, params.Meta["library"])
	libraryTypeValues = mergeSearchValues(libraryTypeValues, params.Meta[SeqmetaPipelineIDLimsKey])
	libraryTypeValues = mergeSearchValues(libraryTypeValues, params.Meta[LegacySeqmetaLibraryKey])
	libraryTypeValues = mergeSearchValues(libraryTypeValues, params.Meta[LegacySeqmetaLibraryTypeKey])
	libraryIDValues = mergeSearchValues(libraryIDValues, params.Meta[SeqmetaLibraryIDKey])
	libraryIDValues = mergeSearchValues(libraryIDValues, params.Meta[LegacySeqmetaLibraryIDKey])
	libraryLimsValues = mergeSearchValues(libraryLimsValues, params.Meta[SeqmetaIDLibraryLimsKey])
	libraryLimsValues = mergeSearchValues(libraryLimsValues, params.Meta[LegacySeqmetaLibraryLimsKey])
	runValues = mergeSearchValues(runValues, params.Meta[SeqmetaIDRunKey])
	runValues = mergeSearchValues(runValues, params.Meta[LegacySeqmetaRunIDKey])
	sampleValues = mergeSearchValues(sampleValues, params.Meta["sample"])
	sampleValues = mergeSearchValues(sampleValues, params.Meta[SeqmetaSampleNameURLKey])
	sampleExpansionRequests = appendSampleSearchExpansion(sampleExpansionRequests, mlwh.KindSangerSampleName, params.Meta["sample"])
	for _, searchKey := range sampleMetadataSearchKeys {
		values := params.Meta[searchKey.key]
		sampleExpansionRequests = appendSampleSearchExpansion(sampleExpansionRequests, searchKey.kind, values)
		if len(values) > 0 {
			directSampleExactValues[searchKey.key] = mergeSearchValues(directSampleExactValues[searchKey.key], values)
		}
	}
	laneValues = mergeSearchValues(laneValues, params.Meta["seqmeta_lane"])
	studyValues = mergeSearchValues(studyValues, params.Meta[SeqmetaIDStudyLimsKey])
	studyValues = mergeSearchValues(studyValues, params.Meta[LegacySeqmetaStudyIDKey])
	studyValues = mergeSearchValues(studyValues, params.Meta["seqmeta_study_accession"])
	delete(params.Meta, SeqmetaIDStudyLimsKey)
	delete(params.Meta, LegacySeqmetaStudyIDKey)
	delete(params.Meta, "seqmeta_study_accession")
	delete(params.Meta, "library")
	delete(params.Meta, SeqmetaPipelineIDLimsKey)
	delete(params.Meta, LegacySeqmetaLibraryKey)
	delete(params.Meta, SeqmetaLibraryIDKey)
	delete(params.Meta, LegacySeqmetaLibraryIDKey)
	delete(params.Meta, SeqmetaIDLibraryLimsKey)
	delete(params.Meta, LegacySeqmetaLibraryLimsKey)
	delete(params.Meta, LegacySeqmetaLibraryTypeKey)
	delete(params.Meta, SeqmetaIDRunKey)
	delete(params.Meta, LegacySeqmetaRunIDKey)
	delete(params.Meta, "seqmeta_lane")

	for _, searchKey := range sampleMetadataSearchKeys {
		delete(params.Meta, searchKey.key)
	}
	delete(params.Meta, "sample")

	legacyStudyIDUsed := len(combinedSearchValues(r, "study_id")) > 0
	hasLibraryValues := len(libraryTypeValues) > 0 || len(libraryIDValues) > 0 || len(libraryLimsValues) > 0

	resolvedSamples := []string{}
	resolvedRuns := []string{}
	resolvedLanes := []string{}
	if len(studyValues) > 0 {
		if s.resolver == nil {
			if legacyStudyIDUsed {
				writeServerError(c, http.StatusBadRequest, "seqmeta not configured")

				return
			}
		} else {
			studySearchValues, studyExpansionValues, err := canonicalStudySearchValues(c.Request.Context(), s.resolver, studyValues)
			if err != nil {
				writeDomainError(c, err)

				return
			}

			studyValues = studySearchValues
			for _, studyValue := range studyExpansionValues {
				samples, runs, lanes, err := s.resolver.Expand(c.Request.Context(), mlwh.KindStudyLimsID, studyValue)
				if err != nil {
					writeDomainError(c, err)

					return
				}

				resolvedSamples = mergeSearchValues(resolvedSamples, samples)
				resolvedRuns = mergeSearchValues(resolvedRuns, runs)
				resolvedLanes = mergeSearchValues(resolvedLanes, lanes)
			}

			sampleValues = mergeSearchValues(sampleValues, resolvedSamples)
			runValues = mergeSearchValues(runValues, resolvedRuns)
			laneValues = mergeSearchValues(laneValues, resolvedLanes)
		}
	}

	if len(libraryTypeValues) > 0 && s.resolver != nil {
		for _, libraryValue := range libraryTypeValues {
			samples, runs, lanes, err := s.resolver.Expand(c.Request.Context(), mlwh.KindLibraryType, libraryValue)
			if err != nil {
				writeDomainError(c, err)

				return
			}

			sampleValues = mergeSearchValues(sampleValues, samples)
			runValues = mergeSearchValues(runValues, runs)
			laneValues = mergeSearchValues(laneValues, lanes)
		}
	}

	if len(libraryIDValues) > 0 && s.resolver != nil {
		for _, libraryValue := range libraryIDValues {
			samples, runs, lanes, err := s.resolver.Expand(c.Request.Context(), mlwh.KindLibraryID, libraryValue)
			if err != nil {
				writeDomainError(c, err)

				return
			}

			sampleValues = mergeSearchValues(sampleValues, samples)
			runValues = mergeSearchValues(runValues, runs)
			laneValues = mergeSearchValues(laneValues, lanes)
		}
	}

	if len(libraryLimsValues) > 0 && s.resolver != nil {
		for _, libraryValue := range libraryLimsValues {
			samples, runs, lanes, err := s.resolver.Expand(c.Request.Context(), mlwh.KindLibraryLimsID, libraryValue)
			if err != nil {
				writeDomainError(c, err)

				return
			}

			sampleValues = mergeSearchValues(sampleValues, samples)
			runValues = mergeSearchValues(runValues, runs)
			laneValues = mergeSearchValues(laneValues, lanes)
		}
	}

	if len(directRunValues) > 0 && s.resolver != nil {
		for _, runValue := range directRunValues {
			samples, runs, lanes, err := s.resolver.Expand(c.Request.Context(), mlwh.KindRunID, runValue)
			if err != nil {
				writeDomainError(c, err)

				return
			}

			sampleValues = mergeSearchValues(sampleValues, samples)
			runValues = mergeSearchValues(runValues, runs)
			laneValues = mergeSearchValues(laneValues, lanes)
		}
	}

	if len(sampleExpansionRequests) > 0 &&
		len(studyValues) == 0 &&
		!hasLibraryValues &&
		len(directRunValues) == 0 &&
		s.resolver != nil {
		candidateSampleNames, err := s.store.DistinctMetadataValues(c.Request.Context(), candidateSampleNameMetaKeys)
		if err != nil {
			writeDomainError(c, err)

			return
		}

		samples, runs, lanes, expanded, err := expandCandidateSampleSearchValues(
			c.Request.Context(),
			s.resolver,
			candidateSampleNames,
			sampleExpansionRequests,
		)
		if err != nil {
			writeDomainError(c, err)

			return
		}
		if !expanded {
			samples, runs, lanes, err = expandSampleSearchValues(c.Request.Context(), s.resolver, sampleExpansionRequests)
		}
		if err != nil {
			writeDomainError(c, err)

			return
		}

		sampleValues = mergeSearchValues(sampleValues, samples)
		runValues = mergeSearchValues(runValues, runs)
		laneValues = mergeSearchValues(laneValues, lanes)
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

	for _, searchKey := range sampleMetadataSearchKeys {
		if values := directSampleExactValues[searchKey.key]; len(values) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{searchKey.key: values})
		}
	}

	for _, key := range libraryTypeMetaKeys {
		if len(libraryTypeValues) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{key: libraryTypeValues})
		}
	}

	for _, key := range libraryIDMetaKeys {
		if len(libraryIDValues) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{key: libraryIDValues})
		}
	}

	for _, key := range libraryLimsMetaKeys {
		if len(libraryLimsValues) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{key: libraryLimsValues})
		}
	}

	for _, key := range combinedRunMetaKeys {
		if len(runValues) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{key: runValues})
		}
	}

	for _, key := range combinedLaneMetaKeys {
		if len(laneValues) > 0 {
			params.OrMeta = append(params.OrMeta, map[string][]string{key: laneValues})
		}
	}

	results, err := s.store.SearchMulti(c.Request.Context(), params)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	accessUser, err := s.accessUserFromContext(c)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	results, err = AnnotateAccess(results, accessUser)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	if len(studyValues) > 0 && len(resolvedSamples) > 0 {
		writeJSON(c, http.StatusOK, wrapSearchResults(results, resolvedSamples))

		return
	}

	writeJSON(c, http.StatusOK, results)
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

		for _, sampleID := range resultSampleNames(result) {
			if _, ok := resolved[sampleID]; ok {
				wrappedResult.MatchedSamples = append(wrappedResult.MatchedSamples, sampleID)
			}
		}

		wrapped = append(wrapped, wrappedResult)
	}

	return wrapped
}

func (s *Server) handleGetStats(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	r := c.Request
	recent, err := nonNegativeIntQueryValue(r, "recent", 10)
	if err != nil {
		writeServerError(c, http.StatusBadRequest, err.Error())

		return
	}

	days, err := nonNegativeIntQueryValue(r, "days", 30)
	if err != nil {
		writeServerError(c, http.StatusBadRequest, err.Error())

		return
	}

	stats, err := s.store.Stats(c.Request.Context(), recent, days)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	accessUser, err := s.accessUserFromContext(c)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	stats.Recent, err = AnnotateAccess(stats.Recent, accessUser)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	writeJSON(c, http.StatusOK, stats)
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

func (s *Server) handleGetMetaKeys(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	keys, err := s.store.MetaKeys(c.Request.Context())
	if err != nil {
		writeDomainError(c, err)

		return
	}

	writeJSON(c, http.StatusOK, keys)
}

func (s *Server) handleGetResultByID(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	result, err := s.store.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDomainError(c, err)

		return
	}

	access, ok := s.resultAccessForRequest(c, *result)
	if !ok {
		return
	}

	result.Access = access
	writeJSON(c, http.StatusOK, result)
}

func (s *Server) handleGetResultFiles(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	resultID := c.Param("id")
	result, err := s.store.Get(c.Request.Context(), resultID)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	if !s.requireResultAccess(c, *result) {
		return
	}

	files, err := s.store.GetFiles(c.Request.Context(), resultID)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	writeJSON(c, http.StatusOK, files)
}

func (s *Server) handleDeleteResultByID(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	resultID := c.Param("id")
	if !s.requireServerOwner(c, resultID) {
		return
	}

	err := s.store.Delete(c.Request.Context(), resultID)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	c.Status(http.StatusNoContent)
}

func currentUserFromValue(value any) *CurrentUser {
	switch user := value.(type) {
	case *CurrentUser:
		return user
	case CurrentUser:
		return &user
	case *gas.User:
		return &CurrentUser{Username: user.Username, User: user}
	case gas.User:
		userCopy := user

		return &CurrentUser{Username: user.Username, User: &userCopy}
	default:
		return nil
	}
}

func resultSampleNames(result ResultSet) []string {
	sampleNames := []string{}

	for _, key := range []string{SeqmetaSampleNameKey, SeqmetaSampleNameURLKey, LegacySeqmetaSampleIDKey} {
		for _, value := range result.MetadataValues[key] {
			if sampleName := strings.TrimSpace(value); sampleName != "" {
				sampleNames = mergeSearchValues(sampleNames, []string{sampleName})
			}
		}
		if sampleName := strings.TrimSpace(result.Metadata[key]); sampleName != "" {
			sampleNames = mergeSearchValues(sampleNames, []string{sampleName})
		}
	}

	return sampleNames
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
