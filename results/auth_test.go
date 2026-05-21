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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
	gas "github.com/wtsi-hgi/go-authserver"
)

func TestRequireServerOwner(t *testing.T) {
	convey.Convey("B2.7: Given owner and non-owner users, then only owner sessions are allowed", t, func() {
		convey.So(RequireServerOwner(&CurrentUser{IsOwner: true}), convey.ShouldBeNil)
		convey.So(errors.Is(RequireServerOwner(&CurrentUser{IsOwner: false}), ErrLocked), convey.ShouldBeTrue)
		convey.So(errors.Is(RequireServerOwner(nil), ErrLocked), convey.ShouldBeTrue)
	})
}

func TestCurrentUserFromContextOwnerSessionsA4(t *testing.T) {
	convey.Convey("A4.6: Given an unknown JWT with Username svc, when CurrentUserFromContext builds the user, then IsOwner is false", t, func() {
		gin.SetMode(gin.TestMode)

		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodGet, gas.EndPointAuth+"/session", nil)
		context.Request.Header.Set("Authorization", "Bearer unknown-jwt")
		context.Set(goAuthserverUserGinContextKey, &gas.User{Username: "svc", UID: "1234"})

		user, err := CurrentUserFromContext(context, NewOwnerSessionStore())

		convey.So(err, convey.ShouldBeNil)
		convey.So(user, convey.ShouldNotBeNil)
		convey.So(user.Username, convey.ShouldEqual, "svc")
		convey.So(user.IsOwner, convey.ShouldBeFalse)
	})
}

type authUserForTest struct {
	gids []string
	err  error
}

func (u authUserForTest) GIDs() ([]string, error) {
	if u.err != nil {
		return nil, u.err
	}

	return u.gids, nil
}

func TestAccessForResult(t *testing.T) {
	convey.Convey("B2.1: Given result GID 200, requester alice, and user alice with no groups, then access is allowed", t, func() {
		result := authResultForTest(gidForTest(200))
		result.Requester = "alice"
		user := &CurrentUser{Username: "alice", User: authUserForTest{}}

		access, err := AccessForResult(result, user)

		convey.So(err, convey.ShouldBeNil)
		convey.So(access, convey.ShouldResemble, AccessState{CanView: true})
		convey.So(RequireResultAccess(result, user), convey.ShouldBeNil)
	})

	convey.Convey("B2.2: Given result GID 200, operator alice, and user alice, then access is allowed", t, func() {
		result := authResultForTest(gidForTest(200))
		result.Operator = "alice"
		user := &CurrentUser{Username: "alice", User: authUserForTest{}}

		access, err := AccessForResult(result, user)

		convey.So(err, convey.ShouldBeNil)
		convey.So(access.CanView, convey.ShouldBeTrue)
		convey.So(access.Locked, convey.ShouldBeFalse)
	})

	convey.Convey("B2.3: Given result GID 200 and user bob with Unix group 200, then access is allowed", t, func() {
		result := authResultForTest(gidForTest(200))
		user := &CurrentUser{Username: "bob", User: authUserForTest{gids: []string{"100", "200"}}}

		access, err := AccessForResult(result, user)

		convey.So(err, convey.ShouldBeNil)
		convey.So(access.CanView, convey.ShouldBeTrue)
		convey.So(access.Locked, convey.ShouldBeFalse)
	})

	convey.Convey("B2.4: Given result GID 200 and user bob without the group, then access is forbidden", t, func() {
		result := authResultForTest(gidForTest(200))
		user := &CurrentUser{Username: "bob", User: authUserForTest{gids: []string{"100"}}}

		access, err := AccessForResult(result, user)

		convey.So(err, convey.ShouldBeNil)
		convey.So(access, convey.ShouldResemble, AccessState{
			CanView: false,
			Locked:  true,
			Reason:  "forbidden",
		})
		convey.So(errors.Is(RequireResultAccess(result, user), ErrLocked), convey.ShouldBeTrue)
	})

	convey.Convey("B2.5: Given nil user, then login is required", t, func() {
		result := authResultForTest(gidForTest(200))

		access, err := AccessForResult(result, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(access, convey.ShouldResemble, AccessState{
			CanView: false,
			Locked:  true,
			Reason:  "login_required",
		})
		convey.So(errors.Is(RequireResultAccess(result, nil), ErrLocked), convey.ShouldBeTrue)
	})

	convey.Convey("B2.6: Given group lookup fails, then access returns the error and is not accessible", t, func() {
		lookupErr := errors.New("lookup failed")
		result := authResultForTest(gidForTest(200))
		user := &CurrentUser{Username: "bob", User: authUserForTest{err: lookupErr}}

		access, err := AccessForResult(result, user)

		convey.So(errors.Is(err, lookupErr), convey.ShouldBeTrue)
		convey.So(access.CanView, convey.ShouldBeFalse)
		convey.So(access.Locked, convey.ShouldBeTrue)
	})

	convey.Convey("Given an owner session, then access is allowed without requester or group membership", t, func() {
		result := authResultForTest(nil)
		user := &CurrentUser{Username: "svc", IsOwner: true}

		access, err := AccessForResult(result, user)

		convey.So(err, convey.ShouldBeNil)
		convey.So(access, convey.ShouldResemble, AccessState{CanView: true})
		convey.So(RequireResultAccess(result, user), convey.ShouldBeNil)
	})

	convey.Convey("Given legacy NULL output_directory_gid, then normal users cannot access the result", t, func() {
		result := authResultForTest(nil)
		result.Requester = "alice"
		result.Operator = "alice"
		user := &CurrentUser{Username: "alice", User: authUserForTest{gids: []string{"200"}}}

		access, err := AccessForResult(result, user)

		convey.So(err, convey.ShouldBeNil)
		convey.So(access, convey.ShouldResemble, AccessState{
			CanView: false,
			Locked:  true,
			Reason:  "forbidden",
		})
		convey.So(errors.Is(RequireResultAccess(result, user), ErrLocked), convey.ShouldBeTrue)
	})
}

func authResultForTest(gid *int64) ResultSet {
	return ResultSet{
		ID:                 "result-1",
		PipelineIdentifier: "pipe",
		RunKey:             "run",
		Requester:          "requester",
		Operator:           "operator",
		OutputDirectoryGID: gid,
	}
}

func gidForTest(gid int64) *int64 {
	return &gid
}
