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

package saga

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wtsi-ssg/wr/backoff"
	backofftime "github.com/wtsi-ssg/wr/backoff/time"
	"github.com/wtsi-ssg/wr/retry"
)

const userAgent = "wtsi-hgi/wa"

const httpRetryStoppedNonRetryable retry.Reason = "non-retryable error"

var (
	// ErrUnauthorized is returned when the SAGA API rejects the supplied credentials.
	ErrUnauthorized = errors.New("saga: unauthorized (401)")
	// ErrNotFound is returned when the requested SAGA resource does not exist.
	ErrNotFound = errors.New("saga: not found (404)")
	// ErrServerError is returned when the SAGA API responds with a 5xx status.
	ErrServerError = errors.New("saga: server error (5xx)")
)

var httpRetryBackoff = func() *backoff.Backoff {
	return &backoff.Backoff{
		Min:     250 * time.Millisecond,
		Max:     3 * time.Second,
		Factor:  1.5,
		Sleeper: &backofftime.Sleeper{},
	}
}

// APIError represents an HTTP error returned by the SAGA API.
type APIError struct {
	StatusCode int
	Message    string
}

// Error returns a stable string form for SAGA HTTP errors.
func (e APIError) Error() string {
	return fmt.Sprintf("saga: HTTP %d: %s", e.StatusCode, e.Message)
}

// Unwrap returns the sentinel error for status codes with special handling.
func (e APIError) Unwrap() error {
	switch {
	case e.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case e.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case e.StatusCode >= http.StatusInternalServerError && e.StatusCode < 600:
		return ErrServerError
	default:
		return nil
	}
}

func (c *Client) doRequestOnce(
	ctx context.Context,
	method string,
	reqURL string,
	payload []byte,
) ([]byte, error) {

	var body io.Reader = http.NoBody
	if payload != nil {
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Authorization", authorizationHeaderValue(c.apiKey))
	req.Header.Set("User-Agent", userAgent)

	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    responseMessage(resp.StatusCode, bodyBytes),
		}
	}

	return bodyBytes, nil
}

func authorizationHeaderValue(apiKey string) string {
	if strings.HasPrefix(strings.ToLower(apiKey), "bearer ") {
		return apiKey
	}

	return "Bearer " + apiKey
}

func responseMessage(statusCode int, body []byte) string {
	message := strings.TrimSpace(string(body))
	if message != "" {
		return message
	}

	return http.StatusText(statusCode)
}

type retryHTTPUntil struct{}

func (u *retryHTTPUntil) ShouldStop(retries int, err error) retry.Reason {
	if err == nil {
		return retry.BecauseErrorNil
	}

	if !shouldRetryRequest(err) {
		return httpRetryStoppedNonRetryable
	}

	return ""
}

func shouldRetryRequest(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= http.StatusInternalServerError && apiErr.StatusCode < 600
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	return false
}

func (c *Client) doRequestURL(
	ctx context.Context,
	method string,
	reqURL string,
	payload []byte,
) ([]byte, error) {
	var responseBody []byte
	var err error

	status := retry.Do(
		ctx,
		func() error {
			responseBody, err = c.doRequestOnce(ctx, method, reqURL, payload)

			return err
		},
		retry.Untils{
			&retryHTTPUntil{},
			&retry.UntilLimit{Max: 3},
		},
		httpRetryBackoff(),
		fmt.Sprintf("SAGA %s %s", method, reqURL),
	)
	if status.Err != nil {
		return nil, status.Err
	}

	return responseBody, nil
}

func (c *Client) doRequest(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	payload []byte,
) ([]byte, error) {
	reqURL, err := c.requestURL(path, query)
	if err != nil {
		return nil, err
	}

	return c.doRequestURL(ctx, method, reqURL, payload)
}

func (c *Client) doGet(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.cachedGet(ctx, path, query)
}

func (c *Client) doPost(ctx context.Context, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	responseBody, err := c.doRequest(ctx, http.MethodPost, path, nil, payload)
	if err != nil {
		return nil, err
	}

	if err := c.invalidateRelatedCacheEntries(path, false); err != nil {
		return nil, err
	}

	return responseBody, nil
}

func (c *Client) doDelete(ctx context.Context, path string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil, nil)
	if err != nil {
		return err
	}

	err = c.invalidateRelatedCacheEntries(path, true)

	return err
}

func (c *Client) requestURL(path string, query url.Values) (string, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}

	baseURL.Path = strings.TrimSuffix(baseURL.Path, "/") + "/"

	relativeURL, err := url.Parse(strings.TrimPrefix(path, "/"))
	if err != nil {
		return "", err
	}

	resolvedURL := baseURL.ResolveReference(relativeURL)
	if query != nil {
		resolvedURL.RawQuery = query.Encode()
	}

	return resolvedURL.String(), nil
}
