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
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/mlwh"
)

var combinedStudyMetaKeys = []string{
	"study",
	"study_id",
	SeqmetaIDStudyLimsKey,
	LegacySeqmetaStudyIDKey,
	SeqmetaStudyAccessionKey,
	SeqmetaStudyUUIDKey,
	SeqmetaStudyNameKey,
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
	SeqmetaSupplierNameKey,
	SeqmetaIDSampleLimsKey,
	SeqmetaAccessionNumberKey,
	SeqmetaSampleUUIDKey,
	SeqmetaDonorIDKey,
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

const (
	currentUserGinContextKey      = "wa_current_user"
	goAuthserverUserGinContextKey = "user"
)

var combinedSampleSearchKinds = []mlwh.IdentifierKind{
	mlwh.KindSangerSampleName,
	mlwh.KindSupplierName,
	mlwh.KindSampleLimsID,
	mlwh.KindSangerSampleID,
	mlwh.KindSampleAccession,
	mlwh.KindSampleUUID,
	mlwh.KindDonorID,
}

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

// SearchResolver expands search values into related sample, run, and lane values.
type SearchResolver interface {
	Expand(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]string, []string, []string, error)
}

type searchSuggestionClassifier interface {
	ClassifyIdentifier(context.Context, string) (mlwh.Match, error)
}

type candidateSampleSearchResolver interface {
	ExpandCandidateSampleSearchValues(ctx context.Context, kind mlwh.IdentifierKind, canonical string, candidates []string) ([]string, error)
}

type studySearchCanonicalizer interface {
	CanonicalStudySearchValue(context.Context, string) (string, error)
}

type mlwhSearchSuggestionTarget struct {
	fieldKey       string
	directMetaKeys []string
	directValues   []string
	expansionKind  mlwh.IdentifierKind
	expansionValue string
}

func mlwhSampleSearchSuggestionTarget(match mlwh.Match, raw string) mlwhSearchSuggestionTarget {
	values := []string{raw, match.Canonical}
	if match.Sample != nil {
		values = append(values,
			match.Sample.Name,
			match.Sample.IDSampleLims,
			match.Sample.SangerSampleID,
			match.Sample.SupplierName,
			match.Sample.AccessionNumber,
			match.Sample.UUIDSampleLims,
			match.Sample.DonorID,
		)
	}

	expansionKind := match.Kind
	expansionValue := raw
	if match.Kind == mlwh.KindSangerSampleName {
		expansionValue = firstNonEmptySearchValue(match.Canonical, raw)
	}

	return mlwhSearchSuggestionTarget{
		fieldKey:       "sample",
		directMetaKeys: combinedSampleMetaKeys,
		directValues:   exactSearchValues(values...),
		expansionKind:  expansionKind,
		expansionValue: expansionValue,
	}
}

func mlwhStudySearchSuggestionTarget(match mlwh.Match, raw string) mlwhSearchSuggestionTarget {
	values := []string{raw, match.Canonical}
	if match.Study != nil {
		values = append(values,
			match.Study.IDStudyLims,
			match.Study.UUIDStudyLims,
			match.Study.Name,
			match.Study.AccessionNumber,
		)
	}

	return mlwhSearchSuggestionTarget{
		fieldKey:       "study",
		directMetaKeys: combinedStudyMetaKeys,
		directValues:   exactSearchValues(values...),
		expansionKind:  mlwh.KindStudyLimsID,
		expansionValue: firstNonEmptySearchValue(match.Canonical, studyLimsIDFromMatch(match), raw),
	}
}

func mlwhRunSearchSuggestionTarget(match mlwh.Match, raw string) mlwhSearchSuggestionTarget {
	values := []string{raw, match.Canonical}
	if match.Run != nil {
		values = append(values, strconv.Itoa(match.Run.IDRun))
	}

	return mlwhSearchSuggestionTarget{
		fieldKey:       "run",
		directMetaKeys: combinedRunMetaKeys,
		directValues:   exactSearchValues(values...),
		expansionKind:  mlwh.KindRunID,
		expansionValue: firstNonEmptySearchValue(match.Canonical, raw),
	}
}

func mlwhLibrarySearchSuggestionTarget(match mlwh.Match, raw string) mlwhSearchSuggestionTarget {
	values := []string{raw, match.Canonical}
	if match.Library != nil {
		values = append(values,
			match.Library.PipelineIDLims,
			match.Library.LibraryID,
			match.Library.IDLibraryLims,
		)
	}

	return mlwhSearchSuggestionTarget{
		fieldKey:       mlwhLibrarySuggestionFieldKey(match.Kind),
		directMetaKeys: mlwhLibrarySuggestionMetaKeys(match.Kind),
		directValues:   exactSearchValues(values...),
		expansionKind:  match.Kind,
		expansionValue: firstNonEmptySearchValue(match.Canonical, raw),
	}
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
	validator       *MLWHValidator
	resolver        SearchResolver
	handler         http.Handler
	maxPreviewBytes int64
	ownerSessions   OwnerSessionStore
}

// NewServer constructs a results API server.
func NewServer(store *Store, validator *MLWHValidator, resolver SearchResolver, opts ...ServerOption) *Server {
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
		router.GET(gas.EndPointREST+"/results/search-suggestions", s.handleGetSearchSuggestions)
		router.GET(gas.EndPointREST+"/results/:id/file", s.handleGetFile)
		router.GET(gas.EndPointREST+"/results/:id/files", s.handleGetResultFiles)
		router.GET(gas.EndPointREST+"/results/:id", s.handleGetResultByID)
	}

	if auth != nil {
		auth.GET("/session", s.handleGetSession)
		auth.POST("/logout", s.handlePostLogout)
		auth.GET("/results", s.handleGetResults)
		auth.GET("/results/stats", s.handleGetStats)
		auth.GET("/results/search-suggestions", s.handleGetSearchSuggestions)
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

	registrationResolver, _ := s.resolver.(RegistrationResolver)
	if err := ApplyRegistrationLookups(c.Request.Context(), registration, registrationResolver); err != nil {
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

func appendCombinedSampleSearchExpansions(existing []sampleSearchExpansion, values []string) []sampleSearchExpansion {
	for _, kind := range combinedSampleSearchKinds {
		existing = appendSampleSearchExpansion(existing, kind, values)
	}

	return existing
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
	case errors.Is(err, ErrMLWHRejected):
		writeServerError(c, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrMLWHFailed), errors.Is(err, ErrSeqmetaFailed):
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
	handledDirectRequest := false

	for _, request := range requests {
		if !directSampleSearchKind(request.kind) {
			remaining = append(remaining, request)

			continue
		}

		handledDirectRequest = true
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

	if !handledDirectRequest {
		return nil, nil, nil, false, nil
	}

	if len(remaining) == 0 || len(resolvedSamples) > 0 {
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
	sampleExpansionRequests := appendCombinedSampleSearchExpansions(nil, sampleValues)
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
	sampleExpansionRequests = appendCombinedSampleSearchExpansions(sampleExpansionRequests, params.Meta["sample"])
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
	studyValues = mergeSearchValues(studyValues, params.Meta[SeqmetaStudyAccessionKey])
	studyValues = mergeSearchValues(studyValues, params.Meta[SeqmetaStudyUUIDKey])
	studyValues = mergeSearchValues(studyValues, params.Meta[SeqmetaStudyNameKey])
	delete(params.Meta, SeqmetaIDStudyLimsKey)
	delete(params.Meta, LegacySeqmetaStudyIDKey)
	delete(params.Meta, SeqmetaStudyAccessionKey)
	delete(params.Meta, SeqmetaStudyUUIDKey)
	delete(params.Meta, SeqmetaStudyNameKey)
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
				writeServerError(c, http.StatusBadRequest, "MLWH resolver not configured")

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

func (s *Server) handleGetSearchSuggestions(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	limit, err := nonNegativeIntQueryValue(c.Request, "limit", 20)
	if err != nil {
		writeServerError(c, http.StatusBadRequest, err.Error())

		return
	}
	if limit > 50 {
		limit = 50
	}

	suggestions, err := s.store.SearchSuggestions(c.Request.Context(), c.Query("q"), limit)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	mlwhSuggestions, err := s.mlwhSearchSuggestions(c.Request.Context(), c.Query("q"), limit)
	if err != nil {
		writeDomainError(c, err)

		return
	}
	suggestions = appendUniqueSearchSuggestions(mlwhSuggestions, suggestions, limit)

	writeJSON(c, http.StatusOK, suggestions)
}

func appendUniqueSearchSuggestions(existing []SearchSuggestion, incoming []SearchSuggestion, limit int) []SearchSuggestion {
	if limit <= 0 {
		return []SearchSuggestion{}
	}

	merged := make([]SearchSuggestion, 0, min(limit, len(existing)+len(incoming)))
	seen := make(map[SearchSuggestion]struct{}, len(existing)+len(incoming))
	for _, suggestion := range append(existing, incoming...) {
		if suggestion.FieldKey == "" || suggestion.Value == "" {
			continue
		}
		if _, ok := seen[suggestion]; ok {
			continue
		}

		seen[suggestion] = struct{}{}
		merged = append(merged, suggestion)
		if len(merged) >= limit {
			break
		}
	}

	return merged
}

func (s *Server) mlwhSearchSuggestions(ctx context.Context, query string, limit int) ([]SearchSuggestion, error) {
	term := strings.TrimSpace(query)
	if term == "" || limit <= 0 || s == nil || s.resolver == nil {
		return []SearchSuggestion{}, nil
	}

	classifier, ok := s.resolver.(searchSuggestionClassifier)
	if !ok {
		return []SearchSuggestion{}, nil
	}

	match, err := classifier.ClassifyIdentifier(ctx, term)
	if err != nil {
		switch {
		case errors.Is(err, mlwh.ErrNotFound), errors.Is(err, mlwh.ErrUnsupportedIdentifier):
			return []SearchSuggestion{}, nil
		default:
			return nil, fmt.Errorf("%w: classify search suggestion: %w", ErrMLWHFailed, err)
		}
	}

	suggestions := []SearchSuggestion{}
	for _, target := range mlwhSearchSuggestionTargets(match, term) {
		registered, err := s.hasRegisteredMLWHSearchSuggestionTarget(ctx, target)
		if err != nil {
			return nil, err
		}
		if !registered {
			continue
		}

		suggestions = append(suggestions, SearchSuggestion{FieldKey: target.fieldKey, Value: term})
		if len(suggestions) >= limit {
			break
		}
	}

	return suggestions, nil
}

func mlwhSearchSuggestionTargets(match mlwh.Match, raw string) []mlwhSearchSuggestionTarget {
	switch match.Kind {
	case mlwh.KindSampleUUID, mlwh.KindSampleLimsID, mlwh.KindSangerSampleName,
		mlwh.KindSangerSampleID, mlwh.KindSupplierName, mlwh.KindSampleAccession, mlwh.KindDonorID:
		return []mlwhSearchSuggestionTarget{mlwhSampleSearchSuggestionTarget(match, raw)}
	case mlwh.KindStudyUUID, mlwh.KindStudyLimsID, mlwh.KindStudyAccession, mlwh.KindStudyName:
		return []mlwhSearchSuggestionTarget{mlwhStudySearchSuggestionTarget(match, raw)}
	case mlwh.KindRunID:
		return []mlwhSearchSuggestionTarget{mlwhRunSearchSuggestionTarget(match, raw)}
	case mlwh.KindLibraryType, mlwh.KindLibraryID, mlwh.KindLibraryLimsID:
		return []mlwhSearchSuggestionTarget{mlwhLibrarySearchSuggestionTarget(match, raw)}
	default:
		return []mlwhSearchSuggestionTarget{}
	}
}

func (s *Server) hasRegisteredMLWHSearchSuggestionTarget(ctx context.Context, target mlwhSearchSuggestionTarget) (bool, error) {
	registered, err := s.store.hasExactMetadataValue(ctx, target.directMetaKeys, target.directValues)
	if err != nil || registered {
		return registered, err
	}

	if target.expansionKind == "" || target.expansionValue == "" {
		return false, nil
	}

	samples, runs, lanes, err := s.resolver.Expand(ctx, target.expansionKind, target.expansionValue)
	if err != nil {
		if errors.Is(err, mlwh.ErrNotFound) || errors.Is(err, mlwh.ErrUnsupportedIdentifier) {
			return false, nil
		}

		return false, err
	}

	if registered, err = s.store.hasExactMetadataValue(ctx, combinedSampleMetaKeys, samples); err != nil || registered {
		return registered, err
	}
	if registered, err = s.store.hasExactMetadataValue(ctx, combinedRunMetaKeys, runs); err != nil || registered {
		return registered, err
	}

	return s.store.hasExactMetadataValue(ctx, combinedLaneMetaKeys, lanes)
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

func mlwhLibrarySuggestionFieldKey(kind mlwh.IdentifierKind) string {
	switch kind {
	case mlwh.KindLibraryID:
		return SeqmetaLibraryIDKey
	case mlwh.KindLibraryLimsID:
		return SeqmetaIDLibraryLimsKey
	default:
		return "library"
	}
}

func mlwhLibrarySuggestionMetaKeys(kind mlwh.IdentifierKind) []string {
	switch kind {
	case mlwh.KindLibraryID:
		return libraryIDMetaKeys
	case mlwh.KindLibraryLimsID:
		return libraryLimsMetaKeys
	default:
		return libraryTypeMetaKeys
	}
}

func studyLimsIDFromMatch(match mlwh.Match) string {
	if match.Study == nil {
		return ""
	}

	return match.Study.IDStudyLims
}

func firstNonEmptySearchValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func exactSearchValues(values ...string) []string {
	filtered := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}

		seen[trimmed] = struct{}{}
		filtered = append(filtered, trimmed)
	}

	return filtered
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
