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

package mlwh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// submitFeedbackEndpoint lets SubmitFeedback reuse decodeRemoteError.
// It is deliberately not in Registry. NewResult must be set because
// decodeRemoteError calls it for a cache_never_synced envelope.
var submitFeedbackEndpoint = Endpoint{
	Method:    "SubmitFeedback",
	NewResult: func() any { return &FeedbackReceipt{} },
}

// submitFeedbackError reads the error body once so it can both pick the
// feedback sentinel from the envelope code and hand the same bytes to
// decodeRemoteErrorWithFallback, which keeps the message and the proxy hints.
// Its nil fallback keeps ErrUpstreamImpaired out: the feedback store is not
// the upstream MLWH database. It wraps the feedback sentinel only when the
// decoded error does not already match it, so the sentinel text appears once.
func submitFeedbackError(response *http.Response, proxyURL *url.URL) error {
	raw, err := io.ReadAll(io.LimitReader(response.Body, FeedbackMaxBodyBytes))
	if err != nil {
		return fmt.Errorf("mlwh: read SubmitFeedback %d response: %w", response.StatusCode, err)
	}

	var envelope httpErrorEnvelope

	decoded := json.NewDecoder(bytes.NewReader(raw)).Decode(&envelope) == nil

	response.Body = io.NopCloser(bytes.NewReader(raw))
	base := decodeRemoteErrorWithFallback(response, submitFeedbackEndpoint, proxyURL, nil)

	sentinel := submitFeedbackSentinel(response.StatusCode, decoded, envelope.Code)
	if sentinel == nil {
		return base
	}

	if !errors.Is(base, sentinel) {
		return fmt.Errorf("%w: %w", sentinel, base)
	}

	// The envelope code already gave the sentinel (503 feedback_disabled).
	// Drop the server message too when the sentinel text already says it.
	if envelope.Message != "" && strings.Contains(sentinel.Error(), envelope.Message) {
		return sentinel
	}

	return base
}

func submitFeedbackSentinel(status int, decoded bool, code string) error {
	switch status {
	case http.StatusBadRequest:
		if decoded && code == httpErrorCodeBadRequest {
			return ErrFeedbackInvalid
		}
	case http.StatusUnauthorized:
		return ErrFeedbackUnauthorized
	case http.StatusNotFound:
		return ErrFeedbackUnsupported
	case http.StatusRequestEntityTooLarge:
		return ErrFeedbackTooLarge
	case http.StatusServiceUnavailable:
		if decoded && code == httpErrorCodeFeedbackDisabled {
			return ErrFeedbackDisabled
		}
	}

	return nil
}

// SubmitFeedback POSTs submission to <BaseURL>/feedback and returns the
// receipt. It is not a Queryer method and has no Registry entry.
//
// Error responses keep the server's message and wrap ErrFeedbackInvalid (400
// with a bad_request envelope), ErrFeedbackUnauthorized (401),
// ErrFeedbackUnsupported (404), ErrFeedbackTooLarge (413) or
// ErrFeedbackDisabled (503 feedback_disabled). Other failures, including
// transport errors, other statuses and non-envelope bodies, match no feedback
// sentinel. No feedback error wraps ErrUpstreamImpaired unless the server's
// envelope code is upstream_impaired, which the feedback route never sends.
func (rc *RemoteClient) SubmitFeedback(ctx context.Context, submission FeedbackSubmission) (FeedbackReceipt, error) {
	if rc == nil || rc.httpClient == nil {
		return FeedbackReceipt{}, errors.New("mlwh: SubmitFeedback: nil remote client")
	}

	body, err := json.Marshal(submission)
	if err != nil {
		return FeedbackReceipt{}, fmt.Errorf("mlwh: encode SubmitFeedback request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, rc.baseURL+"/feedback", bytes.NewReader(body))
	if err != nil {
		return FeedbackReceipt{}, fmt.Errorf("mlwh: build SubmitFeedback request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")

	if rc.token != "" {
		request.Header.Set("Authorization", "Bearer "+rc.token)
	}

	proxyURL := selectedRemoteProxyURL(rc.httpClient, request)

	response, err := rc.httpClient.Do(request)
	if err != nil {
		return FeedbackReceipt{}, fmt.Errorf("mlwh: SubmitFeedback request failed: %w", err)
	}
	// Bind the original body now: submitFeedbackError replaces response.Body.
	defer func(body io.Closer) {
		_ = body.Close()
	}(response.Body)

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return FeedbackReceipt{}, submitFeedbackError(response, proxyURL)
	}

	var receipt FeedbackReceipt
	if err := json.NewDecoder(response.Body).Decode(&receipt); err != nil {
		return FeedbackReceipt{}, fmt.Errorf("mlwh: decode SubmitFeedback response: %w", err)
	}

	return receipt, nil
}
