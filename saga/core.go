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
	"encoding/json"
	"net/http"
)

// Ping checks whether the SAGA API root endpoint is reachable.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.doGet(ctx, "/", nil)

	return err
}

// VersionInfo describes the SAGA API version payload.
type VersionInfo struct {
	Rev *string `json:"rev"`
}

// Version returns the SAGA API version information.
func (c *Client) Version(ctx context.Context) (*VersionInfo, error) {
	body, err := c.doGet(ctx, "/version", nil)
	if err != nil {
		return nil, err
	}

	version := &VersionInfo{}
	if err := json.Unmarshal(body, version); err != nil {
		return nil, err
	}

	return version, nil
}

// UserInfo describes the authenticated SAGA user payload.
type UserInfo struct {
	Username string `json:"username"`
}

// AuthMe returns the currently authenticated SAGA user.
func (c *Client) AuthMe(ctx context.Context) (*UserInfo, error) {
	body, err := c.doGet(ctx, "/auth/me", nil)
	if err != nil {
		return nil, err
	}

	user := &UserInfo{}
	if err := json.Unmarshal(body, user); err != nil {
		return nil, err
	}

	return user, nil
}

// TokenResponse describes the generated SAGA API token payload.
type TokenResponse struct {
	Token string `json:"token"`
}

// GenerateToken creates a new SAGA API token for the current user.
func (c *Client) GenerateToken(ctx context.Context) (*TokenResponse, error) {
	body, err := c.doRequest(ctx, http.MethodPost, "/auth/token", nil, nil)
	if err != nil {
		return nil, err
	}

	response := &TokenResponse{}
	if err := json.Unmarshal(body, response); err != nil {
		return nil, err
	}

	return response, nil
}

// User describes a SAGA user payload returned by the users endpoint.
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

// ListUsers returns the SAGA users visible to the authenticated caller.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	body, err := c.doGet(ctx, "/users/", nil)
	if err != nil {
		return nil, err
	}

	users := []User{}
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, err
	}

	return users, nil
}
