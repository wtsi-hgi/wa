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
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// Dialer is the LDAP connection subset needed to authenticate a user.
type Dialer interface {
	Bind(username, password string) error
	Close()
}

// Dial opens an LDAP connection for CheckPassword.
func Dial(address string) (Dialer, error) {
	conn, err := ldap.DialURL(address)
	if err != nil {
		return nil, err
	}

	return ldapConn{conn: conn}, nil
}

// DialFunc opens an LDAP connection to address.
type DialFunc func(address string) (Dialer, error)

// CheckPassword verifies username and password against LDAP and returns the UID.
func CheckPassword(
	dial DialFunc,
	lookup UIDLookup,
	ldapServer string,
	bindDN string,
	username string,
	password string,
) (bool, string) {
	if strings.TrimSpace(username) == "" || password == "" {
		return false, ""
	}

	uid, err := lookup(username)
	if err != nil {
		return false, ""
	}

	conn, err := dial(ldapAddress(ldapServer))
	if err != nil {
		return false, ""
	}
	defer conn.Close()

	if err := conn.Bind(fmt.Sprintf(bindDN, username), password); err != nil {
		return false, ""
	}

	return true, uid
}

func ldapAddress(ldapServer string) string {
	return fmt.Sprintf("ldaps://%s:636", ldapServer)
}

// UIDLookup returns the canonical Unix UID for username.
type UIDLookup func(username string) (string, error)

type ldapConn struct {
	conn *ldap.Conn
}

func (c ldapConn) Bind(username, password string) error {
	return c.conn.Bind(username, password)
}

func (c ldapConn) Close() {
	_ = c.conn.Close()
}
