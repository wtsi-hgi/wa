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
	"cmp"
	"context"
	"net/http"
)

// Identification header names a wa client may send. Every one is optional.
const (
	HeaderClientID     = "X-WA-Client-ID"
	HeaderClientUser   = "X-WA-Client-User"
	HeaderMCPTransport = "X-WA-MCP-Transport"
	HeaderMCPAgent     = "X-WA-MCP-Agent"
	HeaderMCPTool      = "X-WA-MCP-Tool"
)

// ClientIdentity is optional caller identification. Empty fields send no
// header.
type ClientIdentity struct {
	ClientID     string
	Username     string
	MCPTransport string
	MCPAgent     string
	MCPTool      string
}

// overriddenBy returns id with each non-empty field of other replacing its
// counterpart.
func (id ClientIdentity) overriddenBy(other ClientIdentity) ClientIdentity {
	return ClientIdentity{
		ClientID:     cmp.Or(other.ClientID, id.ClientID),
		Username:     cmp.Or(other.Username, id.Username),
		MCPTransport: cmp.Or(other.MCPTransport, id.MCPTransport),
		MCPAgent:     cmp.Or(other.MCPAgent, id.MCPAgent),
		MCPTool:      cmp.Or(other.MCPTool, id.MCPTool),
	}
}

// WithClientIdentity returns ctx carrying id. RemoteClient requests made
// with it send id's non-empty fields, each overriding the same field of
// RemoteConfig.Identity. A nested call replaces the outer identity.
func WithClientIdentity(ctx context.Context, id ClientIdentity) context.Context {
	return context.WithValue(ctx, clientIdentityContextKey{}, id)
}

// setIdentityHeaders adds the client's User-Agent and identity headers to
// req, letting the identity in req's context override the configured one
// field by field.
func (rc *RemoteClient) setIdentityHeaders(req *http.Request) {
	id := rc.identity
	if ctxID, ok := req.Context().Value(clientIdentityContextKey{}).(ClientIdentity); ok {
		id = id.overriddenBy(ctxID)
	}

	setIdentityHeader(req.Header, "User-Agent", rc.userAgent, UsageMaxUserAgentBytes)
	setIdentityHeader(req.Header, HeaderClientID, id.ClientID, UsageMaxClientIDBytes)
	setIdentityHeader(req.Header, HeaderClientUser, id.Username, UsageMaxUsernameBytes)
	setIdentityHeader(req.Header, HeaderMCPTransport, id.MCPTransport, UsageMaxMCPTransportBytes)
	setIdentityHeader(req.Header, HeaderMCPAgent, id.MCPAgent, UsageMaxMCPAgentBytes)
	setIdentityHeader(req.Header, HeaderMCPTool, id.MCPTool, UsageMaxMCPToolBytes)
}

// setIdentityHeader sets name to value after sanitising it and cutting it to
// limit bytes. An empty result sets nothing.
func setIdentityHeader(header http.Header, name, value string, limit int) {
	value = sanitiseIdentityValue(value)
	if len(value) > limit {
		value = value[:limit]
	}

	if value == "" {
		return
	}

	header.Set(name, value)
}

// sanitiseIdentityValue replaces every byte outside printable ASCII
// (0x20-0x7E) with '?', so net/http never rejects the header value.
func sanitiseIdentityValue(value string) string {
	out := []byte(value)

	for i, b := range out {
		if b < 0x20 || b > 0x7e {
			out[i] = '?'
		}
	}

	return string(out)
}

type clientIdentityContextKey struct{}
