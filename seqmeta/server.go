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
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/wtsi-hgi/wa/mlwh"
)

const (
	defaultEnrichSuccessTTL  = 24 * time.Hour
	defaultEnrichNegativeTTL = 15 * time.Minute
)

// Server serves the seqmeta REST API.
type Server struct {
	provider    Provider
	store       *Store
	handler     http.Handler
	successTTL  time.Duration
	negativeTTL time.Duration
}

// NewServer creates a seqmeta HTTP server.
func NewServer(provider Provider, store *Store, opts ...ServerOption) *Server {
	server := &Server{
		provider:    provider,
		store:       store,
		successTTL:  defaultEnrichSuccessTTL,
		negativeTTL: defaultEnrichNegativeTTL,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}

	router := chi.NewRouter()
	router.Get("/studies", server.handleListStudies)
	router.Get("/study/{id}/samples", server.handleStudySamples)
	router.Get("/diff/study/{id}", server.handleStudyDiff)
	router.Get("/diff/sample/{id}", server.handleSampleDiff)
	router.Get("/enrich/*", server.handleEnrich)
	router.Delete("/enrich/*", server.handleDeleteEnrich)
	router.Get("/validate/*", server.handleValidate)
	server.handler = router

	return server
}

// Handler returns the configured HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) loadFreshEnrichCache(identifier string, now time.Time) (*enrichCacheEntry, error) {
	entry, err := s.store.LoadEnrichCache(identifier)
	if err != nil {
		return nil, err
	}

	if entry.FetchedAt.Add(entry.TTL).Before(now) {
		return nil, sql.ErrNoRows
	}

	return entry, nil
}

func (s *Server) handleListStudies(w http.ResponseWriter, r *http.Request) {
	studies, err := listAllStudies(r.Context(), s.provider)
	if err != nil {
		_ = writeError(w, http.StatusBadGateway, err.Error())

		return
	}

	_ = writeJSON(w, http.StatusOK, studies)
}

func (s *Server) handleStudySamples(w http.ResponseWriter, r *http.Request) {
	studyID := chi.URLParam(r, "id")
	samples, err := s.provider.AllSamplesForStudy(r.Context(), studyID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, mlwh.ErrNotFound) {
			status = http.StatusNotFound
		}

		_ = writeError(w, status, err.Error())

		return
	}

	if len(samples) == 0 && looksLikeStudyAccession(studyID) {
		samples, err = s.resolveByAccession(r.Context(), studyID)
		if err != nil {
			_ = writeError(w, http.StatusBadGateway, err.Error())

			return
		}
	}

	// Filter by library_type if query parameter is present
	libraryType := r.URL.Query().Get("library_type")
	if libraryType != "" {
		filtered := make([]mlwh.Sample, 0, len(samples))
		for _, sample := range samples {
			if sample.LibraryType == libraryType {
				filtered = append(filtered, sample)
			}
		}
		samples = filtered
	}

	_ = writeJSON(w, http.StatusOK, samples)
}

// looksLikeStudyAccession returns true if s is non-empty and contains at least
// one letter, indicating it is a study accession rather than a numeric study ID.
func looksLikeStudyAccession(s string) bool {
	for _, ch := range s {
		if unicode.IsLetter(ch) {
			return true
		}
	}

	return false
}

func (s *Server) handleStudyDiff(w http.ResponseWriter, r *http.Request) {
	queryID := chi.URLParam(r, "id")
	if queryID == "all" {
		err := s.store.WithLock(func() error {
			prepared, err := PrepareDiffStudies(r.Context(), s.provider, s.store)
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

		return
	}

	samples, err := listStudySamples(r.Context(), s.provider, queryID)
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

		if err := s.store.invalidateEnrichFor("study_samples", queryID); err != nil {
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
	files, err := listSampleFiles(r.Context(), s.provider, queryID)
	if err != nil {
		if errors.Is(err, mlwh.ErrNotFound) {
			_ = writeError(w, http.StatusNotFound, "identifier \""+queryID+"\": "+err.Error())

			return
		}

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

		if err := s.store.invalidateEnrichFor("sample_files", queryID); err != nil {
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
	identifier, err := decodeWildcardIdentifier(r, "/validate/")
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

func (s *Server) handleEnrich(w http.ResponseWriter, r *http.Request) {
	identifier, err := decodeWildcardIdentifier(r, "/enrich/")
	if err != nil {
		_ = writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	entry, err := s.loadFreshEnrichCache(identifier, time.Now())
	if err == nil {
		status := http.StatusOK
		if entry.Negative {
			status = http.StatusNotFound
		}

		_ = writeJSONBytes(w, status, entry.Body)

		return
	}

	if !errors.Is(err, sql.ErrNoRows) {
		_ = writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	result, err := Enrich(r.Context(), s.provider, identifier)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ErrUnknownIdentifier) {
			status = http.StatusNotFound
			body, marshalErr := marshalJSON(map[string]string{"error": err.Error()})
			if marshalErr != nil {
				_ = writeError(w, http.StatusInternalServerError, marshalErr.Error())

				return
			}

			if saveErr := s.store.SaveEnrichCache(enrichCacheEntry{
				Identifier: identifier,
				Body:       body,
				FetchedAt:  time.Now(),
				TTL:        s.negativeTTL,
				Negative:   true,
			}); saveErr != nil {
				_ = writeError(w, http.StatusInternalServerError, saveErr.Error())

				return
			}

			_ = writeJSONBytes(w, status, body)

			return
		} else if errors.Is(err, ErrAllHopsFailed) {
			var enrichErr *enrichError
			if errors.As(err, &enrichErr) {
				_ = writeJSON(w, status, map[string]any{"error": err.Error(), "missing": enrichErr.missing})

				return
			}
		}

		_ = writeError(w, status, err.Error())

		return
	}

	body, err := marshalJSON(result)
	if err != nil {
		_ = writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	ttl := s.successTTL
	if result.Partial {
		ttl = s.negativeTTL
	}

	if err := s.store.SaveEnrichCache(enrichCacheEntry{
		Identifier: identifier,
		Type:       result.Type,
		Body:       body,
		FetchedAt:  time.Now(),
		TTL:        ttl,
		Partial:    result.Partial,
	}); err != nil {
		_ = writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	_ = writeJSONBytes(w, http.StatusOK, body)
}

func decodeWildcardIdentifier(r *http.Request, prefix string) (string, error) {
	escaped := strings.TrimPrefix(r.URL.EscapedPath(), prefix)

	return url.PathUnescape(escaped)
}

func (s *Server) handleDeleteEnrich(w http.ResponseWriter, r *http.Request) {
	identifier, err := decodeWildcardIdentifier(r, "/enrich/")
	if err != nil {
		_ = writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	if err := s.store.DeleteEnrichCache(identifier); err != nil {
		_ = writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	_ = writeJSON(w, http.StatusOK, map[string]string{"identifier": identifier})
}

func (s *Server) writeDiffError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, mlwh.ErrNotFound) {
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

// resolveByAccession looks up studies to find one whose AccessionNumber matches
// the given accession, then returns all samples for that study's numeric ID.
func (s *Server) resolveByAccession(ctx context.Context, accession string) ([]mlwh.Sample, error) {
	studies, err := listAllStudies(ctx, s.provider)
	if err != nil {
		return nil, err
	}

	for _, study := range studies {
		if study.AccessionNumber == accession {
			return s.provider.AllSamplesForStudy(ctx, study.IDStudyLims)
		}
	}

	return []mlwh.Sample{}, nil
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithEnrichTTL configures enrich cache TTLs for successful and negative-or-partial responses.
func WithEnrichTTL(success, negativeOrPartial time.Duration) ServerOption {
	return func(server *Server) {
		server.successTTL = success
		server.negativeTTL = negativeOrPartial
	}
}
