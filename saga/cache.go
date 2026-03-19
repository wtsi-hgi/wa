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
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/wtsi-hgi/activecache"
)

const cacheKeySeparator = ":"

var cacheStates sync.Map

func makeCacheKey(method string, reqURL string) string {
	return method + cacheKeySeparator + reqURL
}

func splitCacheKey(key string) (string, string, error) {
	method, reqURL, ok := strings.Cut(key, cacheKeySeparator)
	if !ok || method == "" || reqURL == "" {
		return "", "", fmt.Errorf("saga: invalid cache key %q", key)
	}

	return method, reqURL, nil
}

func matchesAnyCacheTarget(cached *url.URL, targets []*url.URL) bool {
	for _, target := range targets {
		if cached.Scheme == target.Scheme && cached.Host == target.Host && cached.Path == target.Path {
			return true
		}
	}

	return false
}

func (c *Client) loadCachedResponse(key string) ([]byte, error) {
	method, reqURL, err := splitCacheKey(key)
	if err != nil {
		return nil, err
	}

	if method != http.MethodGet {
		return nil, fmt.Errorf("saga: unsupported cached method %q", method)
	}

	return c.doRequestURL(context.Background(), method, reqURL, nil)
}

type clientCacheState struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

func (c *Client) ensureCache() *clientCacheState {
	stateAny, _ := cacheStates.LoadOrStore(c, &clientCacheState{keys: make(map[string]struct{})})
	state := stateAny.(*clientCacheState)

	state.mu.Lock()
	defer state.mu.Unlock()

	if c.cache == nil {
		cacheDuration := c.cacheDuration
		if cacheDuration == 0 {
			cacheDuration = defaultCacheDuration
		}

		c.cache = activecache.New(cacheDuration, c.loadCachedResponse)
	}

	return state
}

func (c *Client) cachedGet(ctx context.Context, path string, query url.Values) ([]byte, error) {
	if c == nil {
		return c.doRequest(ctx, http.MethodGet, path, query, nil)
	}

	state := c.ensureCache()

	reqURL, err := c.requestURL(path, query)
	if err != nil {
		return nil, err
	}

	key := makeCacheKey(http.MethodGet, reqURL)
	body, err := c.cache.Get(key)
	if err != nil {
		return nil, err
	}

	state.mu.Lock()
	state.keys[key] = struct{}{}
	state.mu.Unlock()

	return body, nil
}

func (c *Client) invalidateRelatedCacheEntries(path string, includeParent bool) error {
	if c == nil {
		return nil
	}

	state := c.ensureCache()

	targets, err := c.cacheInvalidationTargets(path, includeParent)
	if err != nil {
		return err
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	for key := range state.keys {
		method, cachedURL, splitErr := splitCacheKey(key)
		if splitErr != nil {
			c.cache.Remove(key)
			delete(state.keys, key)

			continue
		}

		if method != http.MethodGet {
			continue
		}

		cached, parseErr := url.Parse(cachedURL)
		if parseErr != nil {
			c.cache.Remove(key)
			delete(state.keys, key)

			continue
		}

		if matchesAnyCacheTarget(cached, targets) {
			c.cache.Remove(key)
			delete(state.keys, key)
		}
	}

	return nil
}

func (c *Client) cacheInvalidationTargets(requestPath string, includeParent bool) ([]*url.URL, error) {
	targetURL, err := c.requestURL(requestPath, nil)
	if err != nil {
		return nil, err
	}

	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	targets := []*url.URL{target}
	if !includeParent {
		return targets, nil
	}

	parentPath := path.Dir(target.Path)
	if parentPath == "." || parentPath == "/" || parentPath == target.Path {
		return targets, nil
	}

	parent := *target
	parent.Path = parentPath
	parent.RawPath = parentPath
	targets = append(targets, &parent)

	return targets, nil
}
