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

package mlwhdiff

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/wtsi-hgi/wa/mlwh"
)

// Server serves the mlwhdiff REST API.
type Server struct {
	source  DiffSource
	store   *Store
	handler http.Handler
}

// NewServer creates a mlwhdiff HTTP server.
func NewServer(source DiffSource, store *Store) *Server {
	server := &Server{
		source: source,
		store:  store,
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.GET("/diff/study/:id", server.handleStudyDiff)
	router.GET("/diff/sample/:id", server.handleSampleDiff)
	server.handler = router

	return server
}

// Handler returns the configured HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) handleStudyDiff(c *gin.Context) {
	w := c.Writer
	queryID := c.Param("id")
	if queryID == "all" {
		err := s.store.WithLock(func() error {
			prepared, err := PrepareDiffStudies(c.Request.Context(), s.source, s.store)
			if err != nil {
				return err
			}

			return writePreparedDiff(w, "study", queryID, prepared)
		})
		if err != nil {
			s.writeDeferredDiffError(w, err)
		}

		return
	}

	samples, err := listStudySamples(c.Request.Context(), s.source, queryID)
	if err != nil {
		s.writeDiffError(w, err)

		return
	}

	err = s.store.WithLock(func() error {
		prepared, err := prepareDiffStudySamples(s.store, queryID, samples)
		if err != nil {
			return err
		}

		return writePreparedDiff(w, "study", queryID, prepared)
	})
	if err != nil {
		s.writeDeferredDiffError(w, err)
	}
}

func (s *Server) handleSampleDiff(c *gin.Context) {
	w := c.Writer
	queryID := c.Param("id")
	files, err := listSampleFiles(c.Request.Context(), s.source, queryID)
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

		return writePreparedDiff(w, "sample", queryID, prepared)
	})
	if err != nil {
		s.writeDeferredDiffError(w, err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) error {
	return writeJSON(w, status, map[string]string{"error": message})
}

func writePreparedDiff[T any](
	w http.ResponseWriter,
	kind string,
	queryID string,
	prepared *PreparedDiff[T],
) error {
	body, err := marshalJSON(prepared.Result)
	if err != nil {
		return err
	}

	if err := prepared.Commit(); err != nil {
		return err
	}

	if err := writeJSONBytes(w, http.StatusOK, body); err != nil {
		log.Printf("mlwhdiff: write failed for %s diff %q: %v", kind, queryID, err)
		if rollbackErr := prepared.Rollback(); rollbackErr != nil {
			log.Printf("mlwhdiff: rollback failed for %s diff %q: %v", kind, queryID, rollbackErr)

			return errors.Join(err, rollbackErr)
		}

		return err
	}

	return nil
}

func (s *Server) writeDeferredDiffError(w http.ResponseWriter, err error) {
	if w.Header().Get("Content-Type") != "" {
		return
	}

	s.writeDiffError(w, err)
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

func writeJSON(w http.ResponseWriter, status int, payload any) error {
	body, err := marshalJSON(payload)
	if err != nil {
		return err
	}

	return writeJSONBytes(w, status, body)
}

func marshalJSON(payload any) ([]byte, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, err
	}

	return body.Bytes(), nil
}

func writeJSONBytes(w http.ResponseWriter, status int, body []byte) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return err
	}

	return nil
}
