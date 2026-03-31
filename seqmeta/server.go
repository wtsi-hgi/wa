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
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/wtsi-hgi/wa/saga"
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
	queryID := chi.URLParam(r, "id")
	samples, err := s.provider.AllSamplesForStudy(r.Context(), queryID)
	if err != nil {
		s.writeDiffError(w, err)

		return
	}

	err = s.store.WithLock(func() error {
		prepared, err := prepareDiffStudySamples(s.store, queryID, samples)
		if err != nil {
			return err
		}

		body, err := marshalJSON(prepared.Result)
		if err != nil {
			return err
		}

		if err := prepared.Commit(); err != nil {
			return err
		}

		if err := writeJSONBytes(w, http.StatusOK, body); err != nil {
			log.Printf("seqmeta: write failed for study diff %q: %v", queryID, err)
			if rollbackErr := prepared.Rollback(); rollbackErr != nil {
				log.Printf("seqmeta: rollback failed for study diff %q: %v", queryID, rollbackErr)

				return errors.Join(err, rollbackErr)
			}

			return err
		}

		return nil
	})
	if err != nil {
		if w.Header().Get("Content-Type") != "" {
			return
		}

		s.writeDiffError(w, err)
	}
}

func (s *Server) handleSampleDiff(w http.ResponseWriter, r *http.Request) {
	queryID := chi.URLParam(r, "id")
	files, err := s.provider.GetSampleFiles(r.Context(), queryID)
	if err != nil {
		s.writeDiffError(w, err)

		return
	}

	err = s.store.WithLock(func() error {
		prepared, err := prepareDiffSampleFiles(s.store, queryID, files)
		if err != nil {
			return err
		}

		body, err := marshalJSON(prepared.Result)
		if err != nil {
			return err
		}

		if err := prepared.Commit(); err != nil {
			return err
		}

		if err := writeJSONBytes(w, http.StatusOK, body); err != nil {
			log.Printf("seqmeta: write failed for sample diff %q: %v", queryID, err)
			if rollbackErr := prepared.Rollback(); rollbackErr != nil {
				log.Printf("seqmeta: rollback failed for sample diff %q: %v", queryID, rollbackErr)

				return errors.Join(err, rollbackErr)
			}

			return err
		}

		return nil
	})
	if err != nil {
		if w.Header().Get("Content-Type") != "" {
			return
		}

		s.writeDiffError(w, err)
	}
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	escaped := strings.TrimPrefix(r.URL.EscapedPath(), "/validate/")
	identifier, err := url.PathUnescape(escaped)
	if err != nil {
		_ = writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	result, err := Validate(r.Context(), s.provider, identifier)
	if err != nil {
		if errors.Is(err, ErrUnknownIdentifier) {
			_ = writeError(w, http.StatusNotFound, err.Error())

			return
		}

		_ = writeError(w, http.StatusBadGateway, err.Error())

		return
	}

	_ = writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeDiffError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, saga.ErrNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, errStoreOperation) {
		status = http.StatusInternalServerError
	}

	_ = writeError(w, status, err.Error())
}

func marshalJSON(payload any) ([]byte, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, err
	}

	return body.Bytes(), nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) error {
	body, err := marshalJSON(payload)
	if err != nil {
		return err
	}

	return writeJSONBytes(w, status, body)
}

func writeJSONBytes(w http.ResponseWriter, status int, body []byte) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return err
	}

	return nil
}

func writeError(w http.ResponseWriter, status int, message string) error {
	return writeJSON(w, status, map[string]string{"error": message})
}
