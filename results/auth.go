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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	gjwt "github.com/golang-jwt/jwt/v4"
	gas "github.com/wtsi-hgi/go-authserver"
)

// ErrLocked reports that the caller is not allowed to access the requested result.
var ErrLocked = errors.New("results: locked")

// OwnerSessionStore tracks JWTs that were minted by server-token login.
type OwnerSessionStore interface {
	MarkOwner(jwt string, expiresAt time.Time)
	IsOwner(jwt string) bool
	Delete(jwt string)
}

// WithOwnerSessionStore sets the owner-session store used by session and logout routes.
func WithOwnerSessionStore(store OwnerSessionStore) ServerOption {
	return func(server *Server) {
		if server != nil {
			server.ownerSessions = store
		}
	}
}

func isOwnerSessionJWTRequest(c *gin.Context, store OwnerSessionStore) bool {
	return c != nil &&
		c.Request != nil &&
		c.Request.URL != nil &&
		c.Request.URL.Path == gas.EndPointJWT &&
		store != nil
}

func markJWTResponseIfNeeded(c *gin.Context, store OwnerSessionStore, oldJWT string, shouldMark bool) {
	if !shouldMark {
		c.Next()

		return
	}

	writer := &ownerSessionResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	c.Next()

	if writer.Status() != http.StatusOK {
		return
	}

	token, ok := jwtStringResponse(writer.body.Bytes())
	if !ok {
		return
	}

	expiresAt, ok := jwtExpiresAt(token)
	if !ok {
		return
	}

	store.MarkOwner(token, expiresAt)
	if oldJWT != "" && oldJWT != token {
		store.Delete(oldJWT)
	}
}

func jwtStringResponse(body []byte) (string, bool) {
	var token string
	if err := json.Unmarshal(bytes.TrimSpace(body), &token); err != nil {
		return "", false
	}

	return token, token != ""
}

func jwtExpiresAt(token string) (time.Time, bool) {
	claims := gjwt.MapClaims{}
	if _, _, err := gjwt.NewParser().ParseUnverified(token, claims); err != nil {
		return time.Time{}, false
	}

	expiresAt, ok := claimUnixTime(claims["exp"])
	if !ok {
		return time.Time{}, false
	}

	return time.Unix(expiresAt, 0), true
}

func claimUnixTime(value any) (int64, bool) {
	switch exp := value.(type) {
	case float64:
		return int64(exp), exp > 0
	case json.Number:
		seconds, err := exp.Int64()

		return seconds, err == nil && seconds > 0
	case int64:
		return exp, exp > 0
	case int:
		return int64(exp), exp > 0
	default:
		return 0, false
	}
}

// OwnerSessionConfig configures WA owner-session tracking for go-authserver JWTs.
type OwnerSessionConfig struct {
	ServerUsername string
	ServerToken    []byte
	Store          OwnerSessionStore
}

// OwnerSessionMiddleware marks server-token login JWTs and carries that marker through refresh.
func OwnerSessionMiddleware(cfg OwnerSessionConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isOwnerSessionJWTRequest(c, cfg.Store) {
			c.Next()

			return
		}

		switch c.Request.Method {
		case http.MethodPost:
			markJWTResponseIfNeeded(c, cfg.Store, "", ownerLoginMatches(c.Request, cfg))
		case http.MethodGet:
			rawJWT := rawJWTFromRequest(c.Request)
			markJWTResponseIfNeeded(c, cfg.Store, rawJWT, cfg.Store.IsOwner(rawJWT))
		default:
			c.Next()
		}
	}
}

func rawJWTFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}

	if cookie, err := r.Cookie("jwt"); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}

	return strings.TrimSpace(parts[1])
}

func ownerLoginMatches(r *http.Request, cfg OwnerSessionConfig) bool {
	if cfg.ServerUsername == "" || len(cfg.ServerToken) == 0 {
		return false
	}

	username, password := loginCredentialsFromRequest(r)
	if username != cfg.ServerUsername {
		return false
	}

	return gas.TokenMatches([]byte(password), cfg.ServerToken)
}

func loginCredentialsFromRequest(r *http.Request) (string, string) {
	if r == nil {
		return "", ""
	}

	body := readRequestBodyPreserving(r)
	if len(body) > 0 {
		if username, password, ok := jsonLoginCredentials(body); ok {
			return username, password
		}

		if username, password, ok := formLoginCredentials(body); ok {
			return username, password
		}
	}

	values := r.URL.Query()

	return values.Get("username"), values.Get("password")
}

func readRequestBodyPreserving(r *http.Request) []byte {
	if r == nil || r.Body == nil {
		return nil
	}

	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))

	if err != nil {
		return nil
	}

	return body
}

func jsonLoginCredentials(body []byte) (string, string, bool) {
	var login struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.Unmarshal(body, &login); err != nil {
		return "", "", false
	}

	return login.Username, login.Password, login.Username != "" || login.Password != ""
}

func formLoginCredentials(body []byte) (string, string, bool) {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return "", "", false
	}

	username := values.Get("username")
	password := values.Get("password")

	return username, password, username != "" || password != ""
}

// InMemoryOwnerSessionStore stores owner JWT hashes in process memory.
type InMemoryOwnerSessionStore struct {
	mu         sync.Mutex
	ownerUntil map[string]time.Time
}

// NewOwnerSessionStore creates an empty in-memory owner-session store.
func NewOwnerSessionStore() *InMemoryOwnerSessionStore {
	return &InMemoryOwnerSessionStore{
		ownerUntil: map[string]time.Time{},
	}
}

// MarkOwner marks jwt as an owner session until expiresAt.
func (s *InMemoryOwnerSessionStore) MarkOwner(jwt string, expiresAt time.Time) {
	if s == nil || jwt == "" || expiresAt.IsZero() || !expiresAt.After(time.Now()) {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ownerUntil == nil {
		s.ownerUntil = map[string]time.Time{}
	}

	s.ownerUntil[jwtHash(jwt)] = expiresAt
}

func jwtHash(jwt string) string {
	hash := sha256.Sum256([]byte(jwt))

	return hex.EncodeToString(hash[:])
}

// IsOwner reports whether jwt is currently marked as an owner session.
func (s *InMemoryOwnerSessionStore) IsOwner(jwt string) bool {
	if s == nil || jwt == "" {
		return false
	}

	hash := jwtHash(jwt)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	expiresAt, ok := s.ownerUntil[hash]
	if !ok {
		return false
	}

	if !expiresAt.After(now) {
		delete(s.ownerUntil, hash)

		return false
	}

	return true
}

// Delete removes any owner marker for jwt.
func (s *InMemoryOwnerSessionStore) Delete(jwt string) {
	if s == nil || jwt == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.ownerUntil, jwtHash(jwt))
}

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

	if user.IsOwner {
		return AccessState{CanView: true}, nil
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

// CurrentUserFromContext builds the current user and overlays owner state from store.
func CurrentUserFromContext(c *gin.Context, store OwnerSessionStore) (*CurrentUser, error) {
	user := currentUserFromContextValue(c)
	if user == nil || user.Username == "" {
		return nil, nil
	}

	return &CurrentUser{
		Username: user.Username,
		User:     user.User,
		IsOwner:  store != nil && store.IsOwner(rawJWTFromRequest(c.Request)),
	}, nil
}

func currentUserFromContextValue(c *gin.Context) *CurrentUser {
	if c == nil {
		return nil
	}

	if value, ok := c.Get(currentUserGinContextKey); ok {
		if user := currentUserFromValue(value); user != nil {
			return user
		}
	}

	if value, ok := c.Get(goAuthserverUserGinContextKey); ok {
		return currentUserFromValue(value)
	}

	return nil
}

// RequireServerOwner returns ErrLocked unless user is an owner session.
func RequireServerOwner(user *CurrentUser) error {
	if user != nil && user.IsOwner {
		return nil
	}

	return ErrLocked
}

type ownerSessionResponseWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *ownerSessionResponseWriter) Write(data []byte) (int, error) {
	_, _ = w.body.Write(data)

	return w.ResponseWriter.Write(data)
}

func (w *ownerSessionResponseWriter) WriteString(data string) (int, error) {
	_, _ = w.body.WriteString(data)

	return w.ResponseWriter.WriteString(data)
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
