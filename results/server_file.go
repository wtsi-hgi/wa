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
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const DefaultMaxPreviewBytes int64 = 10 * 1024 * 1024

// ServerOption configures Server behaviour.
type ServerOption func(*Server)

// WithMaxPreviewBytes sets the file preview size limit.
func WithMaxPreviewBytes(n int64) ServerOption {
	return func(server *Server) {
		if server != nil {
			server.maxPreviewBytes = n
		}
	}
}

type compoundReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (c *compoundReadCloser) Close() error {
	var closeErr error

	for _, closer := range c.closers {
		if err := closer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}

	return closeErr
}

func openFileForResponse(path string, download bool) (io.ReadCloser, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("open file: %w", err)
	}

	if download {
		return file, detectContentType(path), nil
	}

	if strings.EqualFold(filepath.Ext(path), ".gz") {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			_ = file.Close()

			return nil, "", fmt.Errorf("open gzip reader: %w", err)
		}

		return &compoundReadCloser{Reader: gzipReader, closers: []io.Closer{gzipReader, file}}, detectPreviewContentType(path), nil
	}

	return file, detectContentType(path), nil
}

func detectContentType(path string) string {
	return contentTypeFromName(filepath.Base(path))
}

func contentTypeFromName(name string) string {
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if filepath.Ext(name) == ".csv" {
		contentType = "text/csv"
	}

	if contentType == "" {
		return "application/octet-stream"
	}

	return contentType
}

func detectPreviewContentType(path string) string {
	baseName := filepath.Base(path)
	if strings.HasSuffix(strings.ToLower(baseName), ".gz") {
		baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	}

	return contentTypeFromName(baseName)
}

func hasRegisteredFile(files []FileEntry, requestedPath string) bool {
	cleanRequestedPath := filepath.Clean(requestedPath)

	for _, file := range files {
		if filepath.Clean(file.Path) == cleanRequestedPath {
			return true
		}
	}

	return false
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeServerError(w, http.StatusInternalServerError, "server store is not configured")

		return
	}

	requestedPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if requestedPath == "" {
		writeServerError(w, http.StatusBadRequest, "path query parameter is required")

		return
	}

	resultID := chi.URLParam(r, "id")
	if _, err := s.store.Get(r.Context(), resultID); err != nil {
		writeDomainError(w, err)

		return
	}

	files, err := s.store.GetFiles(r.Context(), resultID)
	if err != nil {
		writeDomainError(w, err)

		return
	}

	if !hasRegisteredFile(files, requestedPath) {
		writeServerError(w, http.StatusForbidden, "requested file is not registered")

		return
	}

	download := r.URL.Query().Get("download") == "true"
	fileInfo, err := os.Stat(requestedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeDomainError(w, ErrFileGone)

			return
		}

		writeServerError(w, http.StatusInternalServerError, fmt.Sprintf("stat file: %v", err))

		return
	}

	if !download && fileInfo.Size() > s.maxPreviewBytes {
		w.Header().Set("X-File-Size", strconv.FormatInt(fileInfo.Size(), 10))
		writeDomainError(w, ErrFileTooLarge)

		return
	}

	reader, contentType, err := openFileForResponse(requestedPath, download)
	if err != nil {
		writeServerError(w, http.StatusInternalServerError, err.Error())

		return
	}
	defer func() {
		_ = reader.Close()
	}()

	w.Header().Set("Content-Type", contentType)
	if download {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(requestedPath)))
	}

	w.WriteHeader(http.StatusOK)

	_, _ = io.Copy(w, reader)
}
