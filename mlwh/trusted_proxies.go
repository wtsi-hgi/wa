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
	"fmt"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
)

// ParseTrustedProxies parses a comma- or whitespace-separated list of IP
// addresses and CIDRs naming the reverse proxies whose X-Forwarded-For and
// X-Real-IP headers the server may believe. A bare address becomes a
// single-host prefix and a CIDR's host bits are masked off. A blank value
// returns no prefixes, meaning no proxy is trusted.
func ParseTrustedProxies(value string) ([]netip.Prefix, error) {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})

	var proxies []netip.Prefix

	for _, field := range fields {
		prefix, err := parseTrustedProxy(field)
		if err != nil {
			return nil, err
		}

		proxies = append(proxies, prefix)
	}

	return proxies, nil
}

func parseTrustedProxy(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid trusted proxy %q: %w", entry, err)
		}

		return prefix.Masked(), nil
	}

	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid trusted proxy %q: %w", entry, err)
	}

	if addr.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("invalid trusted proxy %q: zoned addresses are not supported", entry)
	}

	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// WithTrustedProxies sets the reverse proxies whose forwarding headers name
// the client address stored with feedback reports and usage rows. With none,
// the default, every forwarding header is ignored and the socket IP is used.
func WithTrustedProxies(proxies []netip.Prefix) ServerOption {
	return func(s *Server) {
		s.trustedProxies = proxies
	}
}

// applyTrustedProxies replaces gin's default of trusting every proxy with
// s.trustedProxies, so gin.Context.ClientIP() honours X-Forwarded-For and
// X-Real-IP only from those proxies. It walks X-Forwarded-For from the right
// and returns the first address that is not a trusted proxy.
func (s *Server) applyTrustedProxies(router *gin.Engine) {
	proxies := make([]string, 0, len(s.trustedProxies))
	for _, prefix := range s.trustedProxies {
		proxies = append(proxies, prefix.String())
	}

	// Valid prefixes always parse. Were one to fail, gin would trust no
	// proxy, which is the safe outcome.
	_ = router.SetTrustedProxies(proxies)
}

// clientHost returns the client address for feedback and usage records: the
// address gin resolves through the trusted proxies, or, when the socket
// address has no parsable IP, the host part of the raw remote address.
func clientHost(c *gin.Context) string {
	if ip := c.ClientIP(); ip != "" {
		return ip
	}

	return remoteHost(c.Request.RemoteAddr)
}
