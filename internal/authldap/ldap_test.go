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

package authldap

import (
	"errors"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

type fakeDialer struct {
	bindErr       error
	boundUsername string
	boundPassword string
	closed        bool
}

func (d *fakeDialer) Bind(username, password string) error {
	d.boundUsername = username
	d.boundPassword = password

	return d.bindErr
}

func (d *fakeDialer) Close() {
	d.closed = true
}

func TestCheckPassword(t *testing.T) {
	convey.Convey("A3.1: Given valid LDAP credentials, then CheckPassword binds with formatted DN and returns the UID", t, func() {
		dialer := &fakeDialer{}
		var dialedAddress string
		dial := func(address string) (Dialer, error) {
			dialedAddress = address

			return dialer, nil
		}
		lookup := func(username string) (string, error) {
			convey.So(username, convey.ShouldEqual, "alice")

			return "1001", nil
		}

		ok, uid := CheckPassword(
			dial,
			lookup,
			"ldap.example.org",
			"uid=%s,ou=people,dc=example,dc=org",
			"alice",
			"secret",
		)

		convey.So(ok, convey.ShouldBeTrue)
		convey.So(uid, convey.ShouldEqual, "1001")
		convey.So(dialedAddress, convey.ShouldEqual, "ldaps://ldap.example.org:636")
		convey.So(dialer.boundUsername, convey.ShouldEqual, "uid=alice,ou=people,dc=example,dc=org")
		convey.So(dialer.boundPassword, convey.ShouldEqual, "secret")
		convey.So(dialer.closed, convey.ShouldBeTrue)
	})

	convey.Convey("A3.2: Given UID lookup fails, then CheckPassword returns false and does not dial LDAP", t, func() {
		lookupErr := errors.New("uid lookup failed")
		dialCount := 0
		dial := func(address string) (Dialer, error) {
			dialCount++

			return &fakeDialer{}, nil
		}
		lookup := func(username string) (string, error) {
			convey.So(username, convey.ShouldEqual, "alice")

			return "", lookupErr
		}

		ok, uid := CheckPassword(
			dial,
			lookup,
			"ldap.example.org",
			"uid=%s,ou=people,dc=example,dc=org",
			"alice",
			"secret",
		)

		convey.So(ok, convey.ShouldBeFalse)
		convey.So(uid, convey.ShouldEqual, "")
		convey.So(dialCount, convey.ShouldEqual, 0)
	})

	convey.Convey("A3.3: Given LDAP bind fails, then CheckPassword returns false and an empty UID", t, func() {
		bindErr := errors.New("bind failed")
		dialer := &fakeDialer{bindErr: bindErr}
		dial := func(address string) (Dialer, error) {
			convey.So(address, convey.ShouldEqual, "ldaps://ldap.example.org:636")

			return dialer, nil
		}
		lookup := func(username string) (string, error) {
			convey.So(username, convey.ShouldEqual, "alice")

			return "1001", nil
		}

		ok, uid := CheckPassword(
			dial,
			lookup,
			"ldap.example.org",
			"uid=%s,ou=people,dc=example,dc=org",
			"alice",
			"secret",
		)

		convey.So(ok, convey.ShouldBeFalse)
		convey.So(uid, convey.ShouldEqual, "")
		convey.So(dialer.boundUsername, convey.ShouldEqual, "uid=alice,ou=people,dc=example,dc=org")
		convey.So(dialer.boundPassword, convey.ShouldEqual, "secret")
		convey.So(dialer.closed, convey.ShouldBeTrue)
	})
}
