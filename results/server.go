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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Server serves the results REST API.
type Server struct {
	store     *Store
	validator *SeqmetaValidator
	handler   http.Handler
}

// NewServer constructs a results API server.
func NewServer(store *Store, validator *SeqmetaValidator) *Server {
	server := &Server{store: store, validator: validator}
	router := chi.NewRouter()
	router.Get("/results", server.handleGetResults)
	router.Post("/results", server.handlePostResults)
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

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeServerError(w, http.StatusBadRequest, err.Error())
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

	results, err := s.store.Search(r.Context(), searchParamsFromRequest(r))
	if err != nil {
		writeDomainError(w, err)

		return
	}

	writeJSON(w, http.StatusOK, results)
}

func searchParamsFromRequest(r *http.Request) SearchParams {
	query := r.URL.Query()
	params := SearchParams{
		Requester:          query.Get("user"),
		Operator:           query.Get("operator"),
		PipelineName:       query.Get("pipeline_name"),
		PipelineVersion:    query.Get("pipeline_version"),
		PipelineIdentifier: query.Get("pipeline_identifier"),
		RunKey:             query.Get("run_key"),
		OutputDirPrefix:    query.Get("output_dir_prefix"),
		Meta:               map[string]string{},
	}

	for key, values := range query {
		if len(values) == 0 || values[0] == "" {
			continue
		}

		switch {
		case strings.HasPrefix(key, "meta_"):
			if metaKey := strings.TrimPrefix(key, "meta_"); metaKey != "" {
				params.Meta[metaKey] = values[0]
			}
		case strings.HasPrefix(key, "seqmeta_"):
			params.Meta[key] = values[0]
		}
	}

	return params
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
