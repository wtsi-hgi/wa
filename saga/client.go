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
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/wtsi-hgi/activecache"
)

const (
	defaultBaseURL       = "https://saga.cellgeni.sanger.ac.uk/api"
	defaultCacheDuration = 5 * time.Minute
	defaultHTTPTimeout   = 30 * time.Second
)

// ErrNoAPIKey is returned when a client is created without an API key.
var ErrNoAPIKey = errors.New("saga: API key required")

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the default SAGA API base URL.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// WithCacheDuration overrides the default cache duration.
func WithCacheDuration(duration time.Duration) Option {
	return func(c *Client) {
		c.cacheDuration = duration
	}
}

// Client is the top-level SAGA API client.
type Client struct {
	apiKey        string
	baseURL       string
	cacheDuration time.Duration
	http          *http.Client
	cache         *activecache.Cache[string, []byte]
	cacheKeys     map[string]struct{}
	cacheMu       sync.RWMutex
	closeOnce     sync.Once
}

// NewClient creates a SAGA API client using sensible defaults.
func NewClient(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, ErrNoAPIKey
	}

	client := &Client{
		apiKey:        apiKey,
		baseURL:       defaultBaseURL,
		cacheDuration: defaultCacheDuration,
		cacheKeys:     make(map[string]struct{}),
		http: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		opt(client)
	}

	client.cache = activecache.New(client.cacheDuration, client.loadCachedResponse)

	return client, nil
}

// Close releases client resources.
func (c *Client) Close() {
	if c == nil {
		return
	}

	c.closeOnce.Do(func() {
		c.cacheMu.Lock()
		defer c.cacheMu.Unlock()

		if c.cache != nil {
			c.cache.Stop()
		}

		c.cache = nil
		c.cacheKeys = nil
	})
}
