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

package results

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrLocked reports that the caller is not allowed to access the requested result.
var ErrLocked = errors.New("results: locked")

// AccessState describes access for the current authenticated user.
type AccessState struct {
	CanView bool   `json:"can_view"`
	Locked  bool   `json:"locked"`
	Reason  string `json:"reason,omitempty"`
}

// AccessForResult evaluates whether user can view result.
func AccessForResult(result ResultSet, user *CurrentUser) (AccessState, error) {
	if user == nil {
		return lockedAccess("login_required"), nil
	}

	if result.OutputDirectoryGID == nil {
		return lockedAccess("forbidden"), nil
	}

	if user.Username == result.Requester || user.Username == result.Operator {
		return AccessState{CanView: true}, nil
	}

	if user.User == nil {
		return lockedAccess("forbidden"), nil
	}

	gids, err := user.User.GIDs()
	if err != nil {
		return lockedAccess("forbidden"), fmt.Errorf("lookup user groups: %w", err)
	}

	if hasResultGID(gids, *result.OutputDirectoryGID) {
		return AccessState{CanView: true}, nil
	}

	return lockedAccess("forbidden"), nil
}

func lockedAccess(reason string) AccessState {
	return AccessState{
		CanView: false,
		Locked:  true,
		Reason:  reason,
	}
}

// RequireResultAccess returns ErrLocked unless user can view result.
func RequireResultAccess(result ResultSet, user *CurrentUser) error {
	access, err := AccessForResult(result, user)
	if err != nil {
		return err
	}

	if !access.CanView {
		return ErrLocked
	}

	return nil
}

// AuthenticatedUser is the subset needed from go-authserver users.
type AuthenticatedUser interface {
	GIDs() ([]string, error)
}

// CurrentUser describes the authenticated caller and WA owner-session state.
type CurrentUser struct {
	Username string
	User     AuthenticatedUser
	IsOwner  bool
}

// RequireServerOwner returns ErrLocked unless user is an owner session.
func RequireServerOwner(user *CurrentUser) error {
	if user != nil && user.IsOwner {
		return nil
	}

	return ErrLocked
}

func hasResultGID(userGIDs []string, resultGID int64) bool {
	resultGIDString := strconv.FormatInt(resultGID, 10)

	for _, userGID := range userGIDs {
		if userGID == resultGIDString {
			return true
		}
	}

	return false
}
