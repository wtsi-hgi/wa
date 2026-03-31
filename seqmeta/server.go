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
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Server serves the seqmeta REST API.
type Server struct {
	provider SAGAProvider
	store    *Store
	handler  http.Handler
}

// NewServer creates a seqmeta HTTP server.
func NewServer(provider SAGAProvider, store *Store) *Server {
	server := &Server{provider: provider, store: store}

	router := chi.NewRouter()
	router.Get("/diff/study/{id}", server.handleStudyDiff)
	router.Get("/diff/sample/{id}", server.handleSampleDiff)
	router.Get("/validate/*", server.handleValidate)
	server.handler = router

	return server
}

// Handler returns the configured HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) handleStudyDiff(w http.ResponseWriter, r *http.Request) {
	result, err := DiffStudySamples(r.Context(), s.provider, s.store, chi.URLParam(r, "id"))
	if err != nil {
		s.writeDiffError(w, err)

		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSampleDiff(w http.ResponseWriter, r *http.Request) {
	result, err := DiffSampleFiles(r.Context(), s.provider, s.store, chi.URLParam(r, "id"))
	if err != nil {
		s.writeDiffError(w, err)

		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	escaped := strings.TrimPrefix(r.URL.EscapedPath(), "/validate/")
	identifier, err := url.PathUnescape(escaped)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	result, err := Validate(r.Context(), s.provider, identifier)
	if err != nil {
		if errors.Is(err, ErrUnknownIdentifier) {
			writeError(w, http.StatusNotFound, err.Error())

			return
		}

		writeError(w, http.StatusBadGateway, err.Error())

		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeDiffError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, errStoreOperation) {
		status = http.StatusInternalServerError
	}

	writeError(w, status, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
