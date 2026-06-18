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
	"errors"
	"net/http"
)

const (
	httpErrorCodeNotFound              = "not_found"
	httpErrorCodeAmbiguous             = "ambiguous"
	httpErrorCodeUnsupportedIdentifier = "unsupported_identifier"
	httpErrorCodeCacheNeverSynced      = "cache_never_synced"
	httpErrorCodeUpstreamImpaired      = "upstream_impaired"
)

func httpStatusAndErrorCode(err error) (int, string) {
	switch {
	case errors.Is(err, ErrCacheNeverSynced):
		return http.StatusServiceUnavailable, httpErrorCodeCacheNeverSynced
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, httpErrorCodeNotFound
	case errors.Is(err, ErrAmbiguous):
		return http.StatusConflict, httpErrorCodeAmbiguous
	case errors.Is(err, ErrUnsupportedIdentifier):
		return http.StatusUnprocessableEntity, httpErrorCodeUnsupportedIdentifier
	case errors.Is(err, ErrUpstreamImpaired):
		return http.StatusBadGateway, httpErrorCodeUpstreamImpaired
	default:
		return http.StatusBadGateway, httpErrorCodeUpstreamImpaired
	}
}

func sentinelForHTTPErrorCode(code string) error {
	switch code {
	case httpErrorCodeNotFound:
		return ErrNotFound
	case httpErrorCodeAmbiguous:
		return ErrAmbiguous
	case httpErrorCodeUnsupportedIdentifier:
		return ErrUnsupportedIdentifier
	case httpErrorCodeCacheNeverSynced:
		return ErrCacheNeverSynced
	case httpErrorCodeUpstreamImpaired:
		return ErrUpstreamImpaired
	default:
		return nil
	}
}

type httpErrorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
