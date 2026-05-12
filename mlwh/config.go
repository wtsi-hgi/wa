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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// Config describes the MLWH source and cache needed to build a Client.
type Config struct {
	DSN      string
	Password string
	Cache    CacheConfig
	Source   Querier
}

// Open constructs a cache-backed MLWH client.
func Open(ctx context.Context, cfg Config) (*Client, error) {
	cache, err := OpenCache(ctx, cfg.Cache)
	if err != nil {
		return nil, err
	}

	client := &Client{
		cache:       cache,
		cacheReader: readDBFromCache(cache),
		syncSource:  cfg.Source,
	}

	if client.syncSource != nil {
		return client, nil
	}

	resolvedDSN, err := ResolveDSN(cfg.DSN, cfg.Password)
	if err != nil {
		_ = cache.Close()

		return nil, err
	}

	sourceDB, err := sqlOpenFunc("mysql", resolvedDSN)
	if err != nil {
		_ = cache.Close()

		return nil, fmt.Errorf("mlwh: open source db: %w", err)
	}

	if err = sourceDB.PingContext(ctx); err != nil {
		_ = sourceDB.Close()
		_ = cache.Close()

		return nil, fmt.Errorf("mlwh: ping source db: %w", err)
	}

	client.syncSource = sourceDB
	client.sourceDB = sourceDB

	return client, nil
}

// OpenCacheOnly constructs a cache-backed MLWH client without opening an upstream source connection.
func OpenCacheOnly(ctx context.Context, cacheCfg CacheConfig) (*Client, error) {
	cache, err := OpenCache(ctx, cacheCfg)
	if err != nil {
		return nil, err
	}

	return &Client{cache: cache, cacheReader: readDBFromCache(cache)}, nil
}

func readDBFromCache(cache Cache) *sql.DB {
	if cache == nil {
		return nil
	}

	reader, ok := cache.(interface{ ReadDB() *sql.DB })
	if ok && reader.ReadDB() != nil {
		return reader.ReadDB()
	}

	return cache.DB()
}

// ResolveDSN returns a MySQL DSN with any separate password applied.
func ResolveDSN(dsn string, password string) (string, error) {
	trimmedDSN := strings.TrimSpace(dsn)
	if trimmedDSN == "" {
		return "", errors.New("mlwh: dsn must not be empty")
	}

	parsed, err := mysql.ParseDSN(trimmedDSN)
	if err != nil {
		return "", fmt.Errorf("mlwh: parse dsn: %w", err)
	}

	if parsed.Passwd != "" {
		return "", ErrPasswordInDSN
	}

	if strings.TrimSpace(password) != "" {
		parsed.Passwd = password
	}

	parsed.MultiStatements = false
	parsed.InterpolateParams = false

	resolved := parsed.FormatDSN()
	resolved = setMySQLDSNBoolParam(resolved, "multiStatements", false)
	resolved = setMySQLDSNBoolParam(resolved, "interpolateParams", false)

	return resolved, nil
}

func setMySQLDSNBoolParam(dsn string, key string, value bool) string {
	parts := strings.SplitN(dsn, "?", 2)
	params := url.Values{}
	if len(parts) == 2 {
		parsed, err := url.ParseQuery(parts[1])
		if err == nil {
			params = parsed
		}
	}

	params.Set(key, fmt.Sprintf("%t", value))

	return parts[0] + "?" + params.Encode()
}

// Close releases the cache and source database handles owned by the client.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	var closeErrs []error
	if c.sourceDB != nil {
		closeErrs = append(closeErrs, c.sourceDB.Close())
	}

	if c.cache != nil {
		closeErrs = append(closeErrs, c.cache.Close())
	}

	return errors.Join(closeErrs...)
}
