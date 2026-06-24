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
	"bufio"
	"bytes"
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

	"github.com/gin-gonic/gin"
)

const DefaultMaxPreviewBytes int64 = 10 * 1024 * 1024

// MaxInlinePreviewLines is the hard cap on lines returned for an inline file
// browser preview. It is sized to comfortably fill the largest possible inline
// preview height so the height slider never needs to refetch.
const MaxInlinePreviewLines = 20

const previewTruncatedHeader = "X-Preview-Truncated"
const resolvedFilePathHeader = "X-WA-Resolved-File-Path"

// PreviewMode names the three distinct ways the file content endpoint can serve
// a registered file.
type PreviewMode int

const (
	// previewModeUnspecified preserves legacy behaviour: byte cap with optional
	// caller-supplied line_limit.
	previewModeUnspecified PreviewMode = iota
	// previewModeInline is the file-browser inline preview. Hard-capped at
	// MaxInlinePreviewLines AND the byte cap. Any caller-supplied line_limit is
	// reduced to MaxInlinePreviewLines.
	previewModeInline
	// previewModeEnlarged is the click-to-enlarge preview. Byte cap only; no
	// line cap (caller-supplied line_limit is ignored).
	previewModeEnlarged
	// previewModeDownload returns the entire file unchanged.
	previewModeDownload
)

func parsePreviewMode(raw string) (PreviewMode, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return previewModeUnspecified, true
	case "inline":
		return previewModeInline, true
	case "enlarged":
		return previewModeEnlarged, true
	case "download":
		return previewModeDownload, true
	default:
		return previewModeUnspecified, false
	}
}

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

func localPathForRegisteredFile(outputDirectory string, filePath string) (string, bool) {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return "", false
	}

	if filepath.IsAbs(trimmedPath) {
		return filepath.Clean(trimmedPath), true
	}

	cleanOutputDirectory := filepath.Clean(strings.TrimSpace(outputDirectory))
	if !filepath.IsAbs(cleanOutputDirectory) {
		return "", false
	}

	localPath := filepath.Clean(filepath.Join(cleanOutputDirectory, trimmedPath))
	if !pathWithinDirectory(cleanOutputDirectory, localPath) {
		return "", false
	}

	return localPath, true
}

func registeredFileLocalPath(outputDirectory string, files []FileEntry, requestedPath string) (string, bool) {
	cleanRequestedPath := filepath.Clean(requestedPath)

	for _, file := range files {
		if filepath.Clean(file.Path) == cleanRequestedPath {
			return localPathForRegisteredFile(outputDirectory, file.Path)
		}
	}

	return "", false
}

func isLineReadableContentType(contentType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))

	return strings.HasPrefix(normalized, "text/") || normalized == "application/json"
}

func previewContentTypeForPath(path string, download bool) string {
	if download {
		return detectContentType(path)
	}

	return detectPreviewContentType(path)
}

func readPreviewLinesWithinLimits(reader io.Reader, byteLimit int64, lineLimit int) ([]byte, bool, error) {
	bufferedReader := bufio.NewReader(reader)
	var preview bytes.Buffer
	remaining := byteLimit
	linesRead := 0

	for {
		if lineLimit > 0 && linesRead >= lineLimit {
			if _, err := bufferedReader.Peek(1); err != nil {
				if errors.Is(err, io.EOF) {
					return preview.Bytes(), false, nil
				}

				return nil, false, err
			}

			return preview.Bytes(), true, nil
		}

		line, err := bufferedReader.ReadBytes('\n')
		if len(line) > 0 {
			if int64(len(line)) > remaining {
				if preview.Len() == 0 && remaining > 0 {
					if _, writeErr := preview.Write(line[:int(remaining)]); writeErr != nil {
						return nil, false, writeErr
					}
				}

				return preview.Bytes(), true, nil
			}

			if _, writeErr := preview.Write(line); writeErr != nil {
				return nil, false, writeErr
			}

			remaining -= int64(len(line))
			linesRead++
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return preview.Bytes(), false, nil
			}

			return nil, false, err
		}

		if remaining == 0 {
			if _, err := bufferedReader.Peek(1); err != nil {
				if errors.Is(err, io.EOF) {
					return preview.Bytes(), false, nil
				}

				return nil, false, err
			}

			return preview.Bytes(), true, nil
		}
	}
}

func (s *Server) handleGetFile(c *gin.Context) {
	if s == nil || s.store == nil {
		writeServerError(c, http.StatusInternalServerError, "server store is not configured")

		return
	}

	r := c.Request
	w := c.Writer
	resultID := c.Param("id")
	result, err := s.store.Get(c.Request.Context(), resultID)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	if !s.requireResultAccess(c, *result) {
		return
	}

	requestedPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if requestedPath == "" {
		writeServerError(c, http.StatusBadRequest, "path query parameter is required")

		return
	}

	lineLimit := 0
	if lineLimitQuery := strings.TrimSpace(r.URL.Query().Get("line_limit")); lineLimitQuery != "" {
		parsedLineLimit, err := strconv.Atoi(lineLimitQuery)
		if err != nil || parsedLineLimit < 1 {
			writeServerError(c, http.StatusBadRequest, "line_limit query parameter must be a positive integer")

			return
		}

		lineLimit = parsedLineLimit
	}

	mode, ok := parsePreviewMode(r.URL.Query().Get("mode"))
	if !ok {
		writeServerError(c, http.StatusBadRequest, "mode query parameter must be one of: inline, enlarged, download")

		return
	}

	switch mode {
	case previewModeInline:
		if lineLimit <= 0 || lineLimit > MaxInlinePreviewLines {
			lineLimit = MaxInlinePreviewLines
		}
	case previewModeEnlarged:
		lineLimit = 0
	case previewModeDownload, previewModeUnspecified:
		// previewModeDownload is handled below alongside the legacy
		// download=true flag. previewModeUnspecified preserves legacy behaviour.
	}

	files, err := s.store.GetFiles(c.Request.Context(), resultID)
	if err != nil {
		writeDomainError(c, err)

		return
	}

	localPath, registered := registeredFileLocalPath(result.OutputDirectory, files, requestedPath)
	if !registered {
		writeServerError(c, http.StatusForbidden, "requested file is not registered")

		return
	}

	download := mode == previewModeDownload || r.URL.Query().Get("download") == "true"
	previewContentType := previewContentTypeForPath(localPath, download)
	allowReadablePreview := !download && isLineReadableContentType(previewContentType)
	fileInfo, err := os.Stat(localPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeDomainError(c, ErrFileGone)

			return
		}

		writeServerError(c, http.StatusInternalServerError, fmt.Sprintf("stat file: %v", err))

		return
	}

	if !download && !allowReadablePreview && fileInfo.Size() > s.maxPreviewBytes {
		w.Header().Set("X-File-Size", strconv.FormatInt(fileInfo.Size(), 10))
		writeDomainError(c, ErrFileTooLarge)

		return
	}

	reader, contentType, err := openFileForResponse(localPath, download)
	if err != nil {
		writeServerError(c, http.StatusInternalServerError, err.Error())

		return
	}
	defer func() {
		_ = reader.Close()
	}()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set(resolvedFilePathHeader, localPath)
	if download {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(localPath)))
	}
	if allowReadablePreview {
		preview, truncated, err := readPreviewLinesWithinLimits(reader, s.maxPreviewBytes, lineLimit)
		if err != nil {
			writeServerError(c, http.StatusInternalServerError, fmt.Sprintf("read preview lines: %v", err))

			return
		}

		if truncated {
			w.Header().Set(previewTruncatedHeader, "true")
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(preview)

		return
	}

	w.WriteHeader(http.StatusOK)

	if download {
		_, _ = io.Copy(w, reader)
	} else {
		_, _ = io.CopyN(w, reader, s.maxPreviewBytes)
	}
}
