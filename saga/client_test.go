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
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wtsi-hgi/activecache"
	. "github.com/smartystreets/goconvey/convey"
)

func TestNewClient(t *testing.T) {
	Convey("Given an API key", t, func() {
		client, err := NewClient("test-key")

		Convey("when NewClient is called, then defaults are applied", func() {
			So(err, ShouldBeNil)
			So(client, ShouldNotBeNil)
			So(func() { client.Close() }, ShouldNotPanic)
			So(client.baseURL, ShouldEqual, defaultBaseURL)
		})
	})

	Convey("Given an empty API key", t, func() {
		client, err := NewClient("")

		Convey("when NewClient is called, then ErrNoAPIKey is returned", func() {
			So(client, ShouldBeNil)
			So(errors.Is(err, ErrNoAPIKey), ShouldBeTrue)
		})
	})

	Convey("Given a custom base URL option", t, func() {
		client, err := NewClient("test-key", WithBaseURL("http://localhost:8080"))

		Convey("when NewClient is called, then the custom base URL is applied", func() {
			So(err, ShouldBeNil)
			So(client, ShouldNotBeNil)
			So(client.baseURL, ShouldEqual, "http://localhost:8080")
			So(client.cacheDuration, ShouldEqual, defaultCacheDuration)
		})
	})

	Convey("Given a custom cache duration option", t, func() {
		client, err := NewClient("test-key", WithCacheDuration(10*time.Minute))

		Convey("when NewClient is called, then the custom cache duration is applied", func() {
			So(err, ShouldBeNil)
			So(client, ShouldNotBeNil)
			So(client.baseURL, ShouldEqual, defaultBaseURL)
			So(client.cacheDuration, ShouldEqual, 10*time.Minute)
		})
	})

	Convey("Given custom base URL and cache duration options", t, func() {
		client, err := NewClient(
			"test-key",
			WithBaseURL("http://localhost:8080"),
			WithCacheDuration(10*time.Minute),
		)

		Convey("when NewClient is called, then both options are applied", func() {
			So(err, ShouldBeNil)
			So(client, ShouldNotBeNil)
			So(client.baseURL, ShouldEqual, "http://localhost:8080")
			So(client.cacheDuration, ShouldEqual, 10*time.Minute)
		})
	})
}

func TestClientClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		Convey("Given a client with an active cache", t, func() {
			var refreshes atomic.Uint64

			cache := activecache.New(10*time.Millisecond, func(string) ([]byte, error) {
				count := refreshes.Add(1)

				return []byte(strconv.FormatUint(count, 10)), nil
			})

			client := &Client{cache: cache}
			Reset(client.Close)

			_, err := client.cache.Get("key")

			So(err, ShouldBeNil)
			So(refreshes.Load(), ShouldEqual, 1)

			Convey("when Close is called, then cache refresh stops", func() {
				time.Sleep(11 * time.Millisecond)
				So(refreshes.Load(), ShouldEqual, 2)

				client.Close()

				time.Sleep(100 * time.Millisecond)
				So(refreshes.Load(), ShouldEqual, 2)
			})

			Convey("when Close is called twice, then it does not panic", func() {
				So(func() {
					client.Close()
					client.Close()
				}, ShouldNotPanic)
			})
		})
	})
}
