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
)

const cacheKeySeparator = ":"

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
		if cached.Scheme == target.Scheme && cached.Host == target.Host && cachePathsMatch(cached.Path, target.Path) {
			return true
		}
	}

	return false
}

func cachePathsMatch(first string, second string) bool {
	if first == second {
		return true
	}

	return strings.TrimSuffix(first, "/") == strings.TrimSuffix(second, "/")
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

func (c *Client) cachedGet(ctx context.Context, path string, query url.Values) ([]byte, error) {
	if c == nil {
		return c.doRequest(ctx, http.MethodGet, path, query, nil)
	}

	reqURL, err := c.requestURL(path, query)
	if err != nil {
		return nil, err
	}

	c.cacheMu.RLock()
	cache := c.cache
	c.cacheMu.RUnlock()
	if cache == nil {
		return c.doRequest(ctx, http.MethodGet, path, query, nil)
	}

	key := makeCacheKey(http.MethodGet, reqURL)
	body, err := cache.Get(key)
	if err != nil {
		return nil, err
	}

	c.cacheMu.Lock()
	if c.cacheKeys != nil {
		c.cacheKeys[key] = struct{}{}
	}
	c.cacheMu.Unlock()

	return body, nil
}

func (c *Client) invalidateRelatedCacheEntries(path string, includeParent bool) error {
	if c == nil {
		return nil
	}

	targets, err := c.cacheInvalidationTargets(path, includeParent)
	if err != nil {
		return err
	}

	c.cacheMu.RLock()
	cache := c.cache
	if cache == nil || len(c.cacheKeys) == 0 {
		c.cacheMu.RUnlock()

		return nil
	}

	keys := make([]string, 0, len(c.cacheKeys))
	for key := range c.cacheKeys {
		keys = append(keys, key)
	}
	c.cacheMu.RUnlock()

	invalidated := make([]string, 0, len(keys))

	for _, key := range keys {
		method, cachedURL, splitErr := splitCacheKey(key)
		if splitErr != nil {
			cache.Remove(key)
			invalidated = append(invalidated, key)

			continue
		}

		if method != http.MethodGet {
			continue
		}

		cached, parseErr := url.Parse(cachedURL)
		if parseErr != nil {
			cache.Remove(key)
			invalidated = append(invalidated, key)

			continue
		}

		if matchesAnyCacheTarget(cached, targets) {
			cache.Remove(key)
			invalidated = append(invalidated, key)
		}
	}

	if len(invalidated) == 0 {
		return nil
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	if c.cacheKeys == nil {
		return nil
	}

	for _, key := range invalidated {
		delete(c.cacheKeys, key)
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
