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

package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	osuser "os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/internal/authldap"
	"github.com/wtsi-hgi/wa/mlwh"
	"github.com/wtsi-hgi/wa/results"

	_ "modernc.org/sqlite"
)

const (
	resultsServerTokenBasename = ".wa-results-server.token"
	resultsJWTBasename         = ".wa-results.jwt"
)

var resultsHTTPClient = &http.Client{Timeout: 30 * time.Second}

var resultsServeOpenMLWHClient = openResultsServeMLWHClientWithConfig

var resultsServeNewAuthServer = func(logWriter io.Writer) resultsServeAuthServer {
	return gas.New(logWriter)
}

var resultsNewClientCLI = func(
	jwtBasename string,
	serverTokenBasename string,
	addr string,
	cert string,
	oktaMode bool,
	username ...string,
) (resultsAuthClient, error) {
	return gas.NewClientCLI(jwtBasename, serverTokenBasename, addr, cert, oktaMode, username...)
}

var resultsNewAuthClient = newResultsAuthClient

//nolint:unused // Overridden by results serve tests to avoid wall-clock waits.
var resultsServeNewTicker = func(interval time.Duration) resultsServeTicker {
	return &resultsServeRealTicker{ticker: time.NewTicker(interval)}
}

type resultsServeMode int

const (
	resultsServeModeTLS resultsServeMode = iota + 1
	resultsServeModeACME
)

func resolveResultsServeMode(cert, key, acme, cache string) (resultsServeMode, error) {
	hasCert := strings.TrimSpace(cert) != ""
	hasKey := strings.TrimSpace(key) != ""
	hasACME := strings.TrimSpace(acme) != ""
	hasCache := strings.TrimSpace(cache) != ""

	if hasCert != hasKey || hasACME != hasCache {
		return 0, errors.New("you must supply --cert and --key, or --acme and --cache")
	}

	if hasCert && hasACME {
		return 0, errors.New("you must supply either --cert and --key, or --acme and --cache, not both")
	}

	if hasACME && hasCache {
		return resultsServeModeACME, nil
	}

	if hasCert && hasKey {
		return resultsServeModeTLS, nil
	}

	return 0, errors.New("you must supply --cert and --key, or --acme and --cache")
}

type resultsServeMLWHConfig struct {
	DSN       string
	CachePath string
}

func resolveResultsServeMLWHConfig(flagValue string, flagChanged bool) (resultsServeMLWHConfig, bool, error) {
	cachePath, hasCachePath, err := resolveResultsServeMLWHCachePath(flagValue, flagChanged)
	if err != nil {
		return resultsServeMLWHConfig{}, false, err
	}

	dsn := strings.TrimSpace(firstEnv("WA_MLWH_DSN"))
	if dsn == "" && !hasCachePath {
		return resultsServeMLWHConfig{}, false, nil
	}

	if !hasCachePath {
		return resultsServeMLWHConfig{}, false, errors.New("WA_MLWH_CACHE_PATH or --mlwh-cache must be set")
	}
	if dsn == "" {
		return resultsServeMLWHConfig{CachePath: cachePath}, true, nil
	}

	resolvedDSN, err := mlwh.ResolveDSN(dsn, firstEnv("WA_MLWH_PASSWORD"))
	if err != nil {
		return resultsServeMLWHConfig{}, false, fmt.Errorf("WA_MLWH_DSN: %w", err)
	}

	return resultsServeMLWHConfig{
		DSN:       resolvedDSN,
		CachePath: cachePath,
	}, true, nil
}

func resolveResultsServeSecurityConfig(
	rawURL string,
	port int,
	portChanged bool,
	cert string,
	key string,
	acme string,
	cache string,
	ldapServer string,
	ldapDN string,
	serverToken string,
) (resultsServeSecurityConfig, error) {
	addr, err := resolveResultsServeBindAddr(rawURL, port, portChanged)
	if err != nil {
		return resultsServeSecurityConfig{}, err
	}

	mode, err := resolveResultsServeMode(cert, key, acme, cache)
	if err != nil {
		return resultsServeSecurityConfig{}, err
	}

	config := resultsServeSecurityConfig{
		addr:        addr,
		cert:        strings.TrimSpace(cert),
		key:         strings.TrimSpace(key),
		acme:        strings.TrimSpace(acme),
		cache:       strings.TrimSpace(cache),
		ldapServer:  strings.TrimSpace(ldapServer),
		ldapDN:      strings.TrimSpace(ldapDN),
		serverToken: strings.TrimSpace(serverToken),
		mode:        mode,
	}

	if err := validateResultsServeLDAP(config.ldapServer, config.ldapDN); err != nil {
		return resultsServeSecurityConfig{}, err
	}

	if err := validateResultsServeServerToken(config.serverToken); err != nil {
		return resultsServeSecurityConfig{}, err
	}

	if mode == resultsServeModeACME {
		if err := validateResultsServeACMECache(config.cache); err != nil {
			return resultsServeSecurityConfig{}, err
		}
	}

	return config, nil
}

func resolveResultsServeBindAddr(rawURL string, port int, portChanged bool) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		bindPort, err := resolveResultsServeBindPort(port, portChanged)
		if err != nil {
			return "", err
		}

		host := strings.TrimSpace(activeResultsBindHost())
		if host == "" {
			host = "127.0.0.1"
		}

		return net.JoinHostPort(host, strconv.Itoa(bindPort)), nil
	}

	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", fmt.Errorf("invalid --url: %w", err)
		}
		if parsed.Scheme != "https" {
			return "", errors.New("results serve URL must use https")
		}
		if parsed.User != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", errors.New("results serve URL must be a host[:port] with no path")
		}

		trimmed = parsed.Host
	}

	if strings.ContainsAny(trimmed, "/?#") {
		return "", errors.New("results serve bind address must be host:port")
	}

	if _, portValue, err := net.SplitHostPort(trimmed); err != nil {
		return "", fmt.Errorf("results serve bind address must be host:port: %w", err)
	} else if portValue == "" {
		return "", errors.New("results serve bind address must include a port")
	}

	return trimmed, nil
}

func resolveResultsServeBindPort(flagValue int, flagChanged bool) (int, error) {
	port := flagValue
	source := "--port"

	if !flagChanged {
		envPort := strings.TrimSpace(activeResultsPort())
		if envPort != "" {
			parsedPort, err := strconv.Atoi(envPort)
			if err != nil {
				return 0, fmt.Errorf("invalid active WA_*_RESULTS_PORT %q", envPort)
			}

			port = parsedPort
			source = "active WA_*_RESULTS_PORT"
		}
	}

	if port < 0 || port > 65535 {
		return 0, fmt.Errorf("invalid %s %d", source, port)
	}

	return port, nil
}

func activeResultsBindHost() string {
	switch firstEnv("WA_ENV") {
	case "development":
		return firstEnv("WA_DEV_RESULTS_HOST")
	case "production":
		return firstEnv("WA_PROD_RESULTS_HOST")
	default:
		return ""
	}
}

func validateResultsServeLDAP(ldapServer, ldapDN string) error {
	if ldapServer == "" || ldapDN == "" {
		return errors.New("--ldap_server and --ldap_dn are required")
	}

	if !strings.Contains(ldapDN, "%s") {
		return errors.New("--ldap_dn must contain %s")
	}

	return nil
}

func validateResultsServeServerToken(serverToken string) error {
	if serverToken == "" {
		return errors.New("--server-token is required")
	}

	if !filepath.IsAbs(serverToken) && filepath.Base(serverToken) != serverToken {
		return errors.New("--server-token must be a basename or absolute path")
	}

	return nil
}

func validateResultsServeACMECache(cacheDir string) error {
	stat, err := os.Stat(cacheDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("cert cache directory does not exist")
		}

		return fmt.Errorf("stat cert cache directory: %w", err)
	}

	if !stat.IsDir() {
		return errors.New("cert cache path must be a directory")
	}

	if stat.Mode().Perm() != 0o700 {
		return errors.New("cert cache directory must only be readable by the server user")
	}

	return nil
}

func resultsServeOwnerSessionConfig(tokenBasename string, store results.OwnerSessionStore) (results.OwnerSessionConfig, error) {
	currentUser, err := osuser.Current()
	if err != nil {
		return results.OwnerSessionConfig{}, err
	}

	tokenPath, err := resultsServeServerTokenPath(tokenBasename)
	if err != nil {
		return results.OwnerSessionConfig{}, err
	}

	serverToken, err := resultsServeServerToken(tokenPath)
	if err != nil {
		return results.OwnerSessionConfig{}, err
	}

	return results.OwnerSessionConfig{
		ServerUsername: currentUser.Username,
		ServerToken:    serverToken,
		Store:          store,
	}, nil
}

func resultsServeServerToken(tokenPath string) ([]byte, error) {
	if token, err := gas.GetStoredToken(tokenPath); err == nil && token != nil {
		return token, nil
	}

	token, err := gas.GenerateToken()
	if err != nil {
		return nil, err
	}

	if err := writeResultsServeServerToken(tokenPath, token); err != nil {
		return nil, err
	}

	return token, nil
}

func writeResultsServeServerToken(tokenPath string, token []byte) error {
	file, err := os.OpenFile(tokenPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}

	defer func() {
		_ = file.Close()
	}()

	if err := file.Chmod(0o600); err != nil {
		return err
	}

	if n, err := file.Write(token); err != nil {
		return err
	} else if n != len(token) {
		return io.ErrShortWrite
	}

	return nil
}

func resultsServeServerTokenPath(tokenBasename string) (string, error) {
	if filepath.IsAbs(tokenBasename) {
		return tokenBasename, nil
	}

	tokenDir, err := gas.TokenDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(tokenDir, tokenBasename), nil
}

func startResultsServeAuthServer(ctx context.Context, authServer resultsServeAuthServer, config resultsServeSecurityConfig) error {
	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	defer authServer.Stop()

	go func() {
		<-serveCtx.Done()
		authServer.Stop()
	}()

	switch config.mode {
	case resultsServeModeTLS:
		return authServer.Start(config.addr, config.cert, config.key)
	case resultsServeModeACME:
		if resultsServeUseACMETLSOnly(config.addr) {
			return authServer.StartACMETLSOnly(config.addr, config.acme, config.cache)
		}

		return authServer.StartACME(config.addr, config.acme, config.cache)
	default:
		return errors.New("results serve mode is not configured")
	}
}

func resultsServeUseACMETLSOnly(addr string) bool {
	_, port, err := net.SplitHostPort(addr)

	return err == nil && (port == "443" || port == "https")
}

type resultsServeAuthServer interface {
	Router() *gin.Engine
	AuthRouter() *gin.RouterGroup
	EnableAuthWithServerToken(certFile, keyFile, tokenBasename string, acb gas.AuthCallback) error
	Start(addr, certFile, keyFile string) error
	StartACME(addr string, acmeURL, cacheDir string) error
	StartACMETLSOnly(addr string, acmeURL, cacheDir string) error
	Stop()
}

type resultsServeSecurityConfig struct {
	addr        string
	cert        string
	key         string
	acme        string
	cache       string
	ldapServer  string
	ldapDN      string
	serverToken string
	mode        resultsServeMode
}

func (c resultsServeSecurityConfig) authCertFile() string {
	return c.cert
}

func (c resultsServeSecurityConfig) authKeyFile() string {
	if c.key != "" {
		return c.key
	}

	if c.mode == resultsServeModeACME {
		return filepath.Join(c.cache, "wa-results-jwt.key")
	}

	return ""
}

func (c resultsServeSecurityConfig) authCallback() gas.AuthCallback {
	return func(username, password string) (bool, string) {
		return authldap.CheckPassword(
			authldap.Dial,
			gas.UserNameToUID,
			c.ldapServer,
			c.ldapDN,
			username,
			password,
		)
	}
}

type resultsServeSyncClient interface {
	Sync(context.Context) ([]mlwh.SyncReport, error)
	ExpandIdentifier(context.Context, mlwh.IdentifierKind, string) ([]mlwh.TaggedID, error)
	ExpandSearchValues(context.Context, mlwh.IdentifierKind, string) ([]string, []string, []string, error)
	ExpandSampleSearchValues(context.Context, mlwh.IdentifierKind, string) ([]string, error)
	LanesForSample(context.Context, string, int, int) ([]mlwh.Lane, error)
	ResolveRun(context.Context, string) (mlwh.Match, error)
	ResolveStudy(context.Context, string) (mlwh.Match, error)
	ResolveSample(context.Context, string) (mlwh.Match, error)
	ResolveSampleName(context.Context, string) (mlwh.Match, error)
	ResolveLibrary(context.Context, string) (mlwh.Match, error)
	ResolveLibraryIdentifier(context.Context, string) (mlwh.Match, error)
	Close() error
}

func openResultsServeMLWHClientWithConfig(ctx context.Context, cfg resultsServeMLWHConfig) (resultsServeSyncClient, error) {
	client, err := mlwh.OpenCacheOnly(ctx, mlwh.CacheConfig{
		Path:     cfg.CachePath,
		Password: firstEnv("WA_MLWH_CACHE_PASSWORD"),
	})
	if err != nil {
		return nil, err
	}

	return &resultsServeMLWHRuntime{client: client}, nil
}

//nolint:unused // Kept with resultsServeNewTicker for the results serve test hook.
type resultsServeTicker interface {
	Chan() <-chan time.Time
	Stop()
}

//nolint:unused // Kept with resultsServeNewTicker for the results serve test hook.
type resultsServeRealTicker struct {
	ticker *time.Ticker
}

//nolint:unused // Kept with resultsServeNewTicker for the results serve test hook.
func (t *resultsServeRealTicker) Chan() <-chan time.Time {
	return t.ticker.C
}

//nolint:unused // Kept with resultsServeNewTicker for the results serve test hook.
func (t *resultsServeRealTicker) Stop() {
	t.ticker.Stop()
}

type resultsServeMLWHRuntime struct {
	client   *mlwh.Client
	sourceDB *sql.DB
}

func (r *resultsServeMLWHRuntime) Sync(ctx context.Context) ([]mlwh.SyncReport, error) {
	return r.client.Sync(ctx)
}

func (r *resultsServeMLWHRuntime) ExpandIdentifier(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]mlwh.TaggedID, error) {
	return r.client.ExpandIdentifier(ctx, kind, canonical)
}

func (r *resultsServeMLWHRuntime) ExpandSearchValues(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]string, []string, []string, error) {
	return r.client.ExpandSearchValues(ctx, kind, canonical)
}

func (r *resultsServeMLWHRuntime) ExpandSampleSearchValues(ctx context.Context, kind mlwh.IdentifierKind, canonical string) ([]string, error) {
	return r.client.ExpandSampleSearchValues(ctx, kind, canonical)
}

func (r *resultsServeMLWHRuntime) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	return r.client.ResolveStudy(ctx, raw)
}

func (r *resultsServeMLWHRuntime) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	return r.client.ResolveRun(ctx, raw)
}

func (r *resultsServeMLWHRuntime) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	return r.client.ResolveSample(ctx, raw)
}

func (r *resultsServeMLWHRuntime) ResolveSampleName(ctx context.Context, raw string) (mlwh.Match, error) {
	return r.client.ResolveSampleName(ctx, raw)
}

func (r *resultsServeMLWHRuntime) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	return r.client.ResolveLibrary(ctx, raw)
}

func (r *resultsServeMLWHRuntime) ResolveLibraryIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	return r.client.ResolveLibraryIdentifier(ctx, raw)
}

func (r *resultsServeMLWHRuntime) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]mlwh.Lane, error) {
	return r.client.LanesForSample(ctx, sangerName, limit, offset)
}

func (r *resultsServeMLWHRuntime) Close() error {
	var closeErrs []error
	if r.client != nil {
		closeErrs = append(closeErrs, r.client.Close())
	}
	if r.sourceDB != nil {
		closeErrs = append(closeErrs, r.sourceDB.Close())
	}

	return errors.Join(closeErrs...)
}

func newResultsAuthClientWithServerToken(
	serverURL string,
	certPath string,
	serverTokenBasename string,
	username ...string,
) (resultsAuthClient, error) {
	addr, err := resultsAuthAddr(serverURL)
	if err != nil {
		return nil, err
	}

	client, err := resultsNewClientCLI(
		resultsJWTBasename,
		serverTokenBasename,
		addr,
		effectiveResultsCertPath(certPath),
		false,
		username...,
	)
	if err != nil {
		return nil, err
	}

	return &permissionCheckingResultsAuthClient{
		client:              client,
		jwtBasename:         resultsJWTBasename,
		serverTokenBasename: serverTokenBasename,
	}, nil
}

func resultsRegisterPasswordAuthenticatedRequest(serverURL, certPath string) (*resty.Request, error) {
	serverTokenPath, err := unavailableResultsServerTokenPath()
	if err != nil {
		return nil, err
	}

	authClient, err := newResultsAuthClientWithServerToken(serverURL, certPath, serverTokenPath)
	if err != nil {
		return nil, err
	}

	return authClient.AuthenticatedRequest()
}

func unavailableResultsServerTokenPath() (string, error) {
	tokenDir, err := gas.TokenDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(tokenDir, fmt.Sprintf("%s.disabled.%d", resultsServerTokenBasename, time.Now().UnixNano())), nil
}

func resultsRegisterAuthenticatedRequest(serverURL, certPath string) (*resty.Request, error) {
	authClient, err := resultsNewAuthClient(serverURL, certPath)
	if err != nil {
		return nil, err
	}

	if authClient.CanReadServerToken() {
		if ownerClient, ok := authClient.(resultsOwnerAuthClient); ok {
			request, err := ownerClient.OwnerAuthenticatedRequest()
			if err == nil || !errors.Is(err, gas.ErrNoAuth) {
				return request, err
			}

			return resultsRegisterPasswordAuthenticatedRequest(serverURL, certPath)
		}

		return authClient.AuthenticatedRequest()
	}

	return authClient.AuthenticatedRequest()
}

func defaultResultsBackendServerURL() string {
	backendURL := strings.TrimSpace(firstEnv("WA_RESULTS_BACKEND_URL"))
	if backendURL == "" {
		return ""
	}

	// WA_RESULTS_BACKEND_URL is for the frontend and may include a path prefix,
	// but CLI auth defaults must be an origin accepted by go-authserver.
	return resultsServerURLOrigin(backendURL)
}

func resultsServerURLOrigin(rawURL string) string {
	endpoint, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return ""
	}

	origin := (&url.URL{Scheme: endpoint.Scheme, Host: endpoint.Host}).String()
	if _, err := resultsAuthAddr(origin); err != nil {
		return ""
	}

	return origin
}

func resultsRegisterLookupPayload(values results.RegistrationLookupValues) *results.RegistrationLookupValues {
	payload := results.RegistrationLookupValues{
		Run:     nonEmptyRegisterLookupValues(values.Run),
		Study:   nonEmptyRegisterLookupValues(values.Study),
		Sample:  nonEmptyRegisterLookupValues(values.Sample),
		Library: nonEmptyRegisterLookupValues(values.Library),
	}
	if !payload.HasValues() {
		return nil
	}

	return &payload
}

func nonEmptyRegisterLookupValues(values []string) []string {
	nonEmpty := make([]string, 0, len(values))

	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}

	return nonEmpty
}

func appendResultsRegisterMetadataValue(metadata map[string][]string, key string, value string) {
	for _, existingValue := range metadata[key] {
		if existingValue == value {
			return
		}
	}

	metadata[key] = append(metadata[key], value)
}

func resultsPublicHTTPClient(certPath string) (*http.Client, error) {
	trimmedCertPath := strings.TrimSpace(certPath)
	if trimmedCertPath == "" {
		if resultsHTTPClient != nil {
			return resultsHTTPClient, nil
		}

		return &http.Client{Timeout: 30 * time.Second}, nil
	}

	certPEM, err := os.ReadFile(trimmedCertPath)
	if err != nil {
		return nil, fmt.Errorf("read results server cert: %w", err)
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(certPEM) {
		return nil, errors.New("results server cert did not contain a PEM certificate")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	timeout := 30 * time.Second
	if resultsHTTPClient != nil {
		if resultsHTTPClient.Timeout != 0 {
			timeout = resultsHTTPClient.Timeout
		}
		if baseTransport, ok := resultsHTTPClient.Transport.(*http.Transport); ok && baseTransport != nil {
			transport = baseTransport.Clone()
		}
	}

	tlsConfig := &tls.Config{RootCAs: rootCAs}
	if transport.TLSClientConfig != nil {
		tlsConfig = transport.TLSClientConfig.Clone()
		tlsConfig.RootCAs = rootCAs
	}
	transport.TLSClientConfig = tlsConfig

	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func resultsRegisterOperatorName(operator string) (string, error) {
	operatorName := strings.TrimSpace(operator)
	if operatorName != "" {
		return operatorName, nil
	}

	currentUser, err := osuser.Current()
	if err != nil {
		return "", fmt.Errorf("get current user: %w", err)
	}

	return strings.TrimSpace(currentUser.Username), nil
}

func parseResultsMetadataValueFilters(metaValues []string) (map[string][]string, error) {
	metadata := make(map[string][]string, len(metaValues))

	for _, metaValue := range metaValues {
		key, value, err := parseResultsMetadataValue(metaValue)
		if err != nil {
			return nil, err
		}

		appendResultsRegisterMetadataValue(metadata, key, value)
	}

	return metadata, nil
}

func parseResultsMetadataValue(metaValue string) (string, string, error) {
	key, value, found := strings.Cut(metaValue, "=")
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if !found || key == "" || value == "" {
		return "", "", fmt.Errorf("invalid --meta value %q: expected key=value", metaValue)
	}

	return key, value, nil
}

func singleResultsRegisterMetadata(metadata map[string][]string) map[string]string {
	single := make(map[string]string, len(metadata))

	for key, values := range metadata {
		if len(values) == 0 {
			continue
		}

		single[key] = values[0]
	}

	return single
}

func resultsRegisterWorkflowFiles(identity results.WorkflowIdentity) ([]results.FileEntry, error) {
	if strings.TrimSpace(identity.LocalPath) == "" {
		return nil, nil
	}

	absPath, err := filepath.Abs(identity.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow file %q: %w", identity.LocalPath, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat workflow file %q: %w", identity.LocalPath, err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("workflow file %q: is a directory", identity.LocalPath)
	}

	return []results.FileEntry{{
		Path:  absPath,
		Mtime: info.ModTime(),
		Size:  info.Size(),
		Kind:  "pipeline",
	}}, nil
}

func effectiveResultsCertPath(certPath string) string {
	if trimmedCertPath := strings.TrimSpace(certPath); trimmedCertPath != "" {
		return trimmedCertPath
	}

	return strings.TrimSpace(firstEnv("WA_RESULTS_SERVER_CERT"))
}

func resultsAuthenticatedRequest(serverURL, certPath string) (*resty.Request, error) {
	return resultsAuthenticatedRequestWithOwnerLogin(serverURL, certPath, false)
}

func resultsOwnerAuthenticatedRequest(serverURL, certPath string) (*resty.Request, error) {
	return resultsAuthenticatedRequestWithOwnerLogin(serverURL, certPath, true)
}

func resultsAuthenticatedRequestWithOwnerLogin(serverURL, certPath string, ownerLogin bool) (*resty.Request, error) {
	authClient, err := resultsNewAuthClient(serverURL, certPath)
	if err != nil {
		return nil, err
	}

	if ownerLogin {
		if ownerClient, ok := authClient.(resultsOwnerAuthClient); ok {
			return ownerClient.OwnerAuthenticatedRequest()
		}
	}

	request, err := authClient.AuthenticatedRequest()
	if err != nil {
		return nil, err
	}

	return request, nil
}

func resultsRegisterUniqueValue(unique, legacyRunID string) (string, error) {
	trimmedUnique := strings.TrimSpace(unique)
	trimmedLegacyRunID := strings.TrimSpace(legacyRunID)

	if trimmedUnique != "" && trimmedLegacyRunID != "" && trimmedUnique != trimmedLegacyRunID {
		return "", errors.New("--unique and deprecated --runid cannot both be set to different values")
	}

	if trimmedUnique != "" {
		return trimmedUnique, nil
	}

	return trimmedLegacyRunID, nil
}

type resultSetWithFiles struct {
	results.ResultSet
	Files []results.FileEntry `json:"files"`
}

func getResultFromPath(ctx context.Context, serverURL, certPath, resultPath string, includeFiles bool) ([]byte, error) {
	resultBody, err := getAuthenticatedResultsResource(ctx, serverURL, certPath, resultPath, http.StatusOK, "get result")
	if err != nil {
		return nil, err
	}

	if !includeFiles {
		return resultBody, nil
	}

	var result results.ResultSet
	if err := json.Unmarshal(resultBody, &result); err != nil {
		return nil, fmt.Errorf("decode result response: %w", err)
	}

	filesBody, err := getAuthenticatedResultsResource(ctx, serverURL, certPath, resultPath+"/files", http.StatusOK, "get result files")
	if err != nil {
		return nil, err
	}

	var files []results.FileEntry
	if err := json.Unmarshal(filesBody, &files); err != nil {
		return nil, fmt.Errorf("decode result files response: %w", err)
	}

	return marshalCommandJSON(resultSetWithFiles{ResultSet: result, Files: files})
}

type resultsRequestFactory func(serverURL, certPath string) (*resty.Request, error)

func getAuthenticatedResultsResource(ctx context.Context, serverURL, certPath, resourcePath string, successStatus int, operation string) ([]byte, error) {
	return getResultsResourceWithAuth(ctx, serverURL, certPath, resourcePath, successStatus, operation, resultsAuthenticatedRequest)
}

func getResultsResourceWithAuth(
	ctx context.Context,
	serverURL,
	certPath,
	resourcePath string,
	successStatus int,
	operation string,
	requestFactory resultsRequestFactory,
) ([]byte, error) {
	request, err := requestFactory(serverURL, certPath)
	if err != nil {
		return nil, err
	}

	response, err := request.SetContext(ctx).Get(resourcePath)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", operation, err)
	}

	body := response.Body()
	if response.StatusCode() != successStatus {
		return nil, decodeResultsCommandError(response.StatusCode(), body)
	}

	if !json.Valid(body) {
		return nil, fmt.Errorf("%s response was not valid JSON", operation)
	}

	return body, nil
}

type resultsAuthClient interface {
	AuthenticatedRequest() (*resty.Request, error)
	CanReadServerToken() bool
}

type resultsAuthLoginClient interface {
	Login(usernameAndPassword ...string) error
}

type resultsOwnerAuthClient interface {
	OwnerAuthenticatedRequest() (*resty.Request, error)
}

func newResultsAuthClient(serverURL string, certPath string, username ...string) (resultsAuthClient, error) {
	return newResultsAuthClientWithServerToken(serverURL, certPath, resultsServerTokenBasename, username...)
}

type permissionCheckingResultsAuthClient struct {
	client              resultsAuthClient
	jwtBasename         string
	serverTokenBasename string
}

func (c *permissionCheckingResultsAuthClient) AuthenticatedRequest() (*resty.Request, error) {
	return c.authenticatedRequest(false)
}

func (c *permissionCheckingResultsAuthClient) OwnerAuthenticatedRequest() (*resty.Request, error) {
	return c.authenticatedRequest(true)
}

func (c *permissionCheckingResultsAuthClient) authenticatedRequest(ownerLogin bool) (*resty.Request, error) {
	if err := resultsTokenPermissionError(c.jwtBasename); err != nil {
		return nil, err
	}

	if err := resultsTokenPermissionError(c.serverTokenBasename); err != nil {
		return nil, err
	}

	if ownerLogin && c.client.CanReadServerToken() {
		if loginClient, ok := c.client.(resultsAuthLoginClient); ok {
			if err := loginClient.Login(); err != nil {
				return nil, err
			}
		}
	}

	return c.client.AuthenticatedRequest()
}

func resultsTokenPermissionError(tokenBasename string) error {
	tokenPath, err := resultsTokenPath(tokenBasename)
	if err != nil {
		return err
	}

	if _, err := gas.GetStoredToken(tokenPath); err != nil {
		var permissionsErr gas.JWTPermissionsError
		if errors.As(err, &permissionsErr) {
			return err
		}
	}

	return nil
}

func (c *permissionCheckingResultsAuthClient) CanReadServerToken() bool {
	return c.client.CanReadServerToken()
}

type resultsCommandOptions struct {
	serverURL string
	certPath  string
}

func getResult(ctx context.Context, serverURL, certPath, resultID string, includeFiles bool) ([]byte, error) {
	return getResultFromPath(ctx, serverURL, certPath, gas.EndPointAuth+"/results/"+url.PathEscape(resultID), includeFiles)
}

func decodeResultsCommandError(statusCode int, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}

	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return fmt.Errorf("results server returned %d: %s", statusCode, payload.Error)
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return fmt.Errorf("results server returned %d", statusCode)
	}

	return fmt.Errorf("results server returned %d: %s", statusCode, trimmed)
}

func newResultsCommand() *cobra.Command {
	options := &resultsCommandOptions{}

	command := &cobra.Command{
		Use:   "results",
		Short: "Results REST API commands",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	command.PersistentFlags().StringVar(&options.serverURL, "server", defaultResultsServerURL(), "Results server base URL (defaults to WA_RESULTS_SERVER_URL, WA_RESULTS_BACKEND_URL, or the active WA_*_RESULTS_PORT)")
	command.PersistentFlags().StringVar(&options.certPath, "cert", firstEnv("WA_RESULTS_SERVER_CERT"), "CA/cert path to trust for the results server")

	command.AddCommand(newResultsRegisterCommand(options))
	command.AddCommand(newResultsSearchCommand(options))
	command.AddCommand(newResultsGetCommand(options))
	command.AddCommand(newResultsDeleteCommand(options))
	command.AddCommand(newResultsRescanCommand(options))
	command.AddCommand(newResultsServeCommand())

	return command
}

func activeResultsPort() string {
	switch firstEnv("WA_ENV") {
	case "test":
		return firstEnv("WA_TEST_RESULTS_PORT")
	case "development":
		return firstEnv("WA_DEV_RESULTS_PORT")
	case "production":
		return firstEnv("WA_PROD_RESULTS_PORT")
	default:
		return ""
	}
}

func resolveResultsServeDBDSN(flagValue string, flagChanged bool) (string, error) {
	dsn := strings.TrimSpace(flagValue)
	if !flagChanged {
		if envValue := firstEnv("WA_RESULTS_DB_PATH"); envValue != "" {
			dsn = envValue
		}
	}

	if dsn == "" {
		return "", errors.New("results database path or DSN must not be empty")
	}

	password := firstEnv("WA_RESULTS_DB_PASSWORD")

	return resolveResultsMySQLPassword(dsn, password, flagChanged)
}

func resolveResultsMySQLPassword(dsn string, password string, rejectEmbeddedPassword bool) (string, error) {
	trimmedDSN := strings.TrimSpace(dsn)
	if resultsDBDriverName(trimmedDSN) != "mysql" {
		return trimmedDSN, nil
	}

	config, err := mysql.ParseDSN(trimmedDSN)
	if err != nil {
		return "", fmt.Errorf("parse MySQL DSN: %w", err)
	}

	if config.Passwd != "" {
		if rejectEmbeddedPassword {
			return "", errors.New("MySQL database passwords must not be supplied on the command line; use WA_RESULTS_DB_PATH or WA_RESULTS_DB_PASSWORD instead")
		}

		return trimmedDSN, nil
	}

	if strings.TrimSpace(password) == "" {
		return trimmedDSN, nil
	}

	config.Passwd = password

	return config.FormatDSN(), nil
}

func validateResultsSQLiteDBPath(dsn string) error {
	trimmedDSN := strings.TrimSpace(dsn)
	if trimmedDSN == "" || trimmedDSN == ":memory:" || strings.HasPrefix(trimmedDSN, "file:") {
		return nil
	}

	dirPath := filepath.Dir(trimmedDSN)
	if dirPath == "." {
		return nil
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("results database directory does not exist: %s", dirPath)
		}

		return fmt.Errorf("results database directory cannot be used: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("results database directory cannot be used: %s is not a directory", dirPath)
	}

	return nil
}

func newResultsRegisterCommand(options *resultsCommandOptions) *cobra.Command {
	var requester string
	var operator string
	var commandLine string
	var workflowReference string
	var unique string
	var legacyRunID string
	var additionalUnique string
	var inputFiles []string
	var matchPatterns []string
	var metaValues []string
	var lookupValues results.RegistrationLookupValues
	var includeHidden bool
	var useJSON bool

	command := &cobra.Command{
		Use:   "register [output-dir]",
		Short: "Register result files",
		Long: `Register result files from an output directory.

Identity:
  output-dir (required) is the directory to scan for output files.
  --user (required) records the requester or owner.
  --operator (optional) records who performed the registration.
  --command (optional) records the command line that produced the results.
  --workflow (required) is the workflow identity: a raw string, local Nextflow
    file, GitHub URL, or owner/repo shorthand.
  --unique (required) is the stable unique run key. Use the same value when
    rerunning the same logical result so the stored registration, files and
    metadata are refreshed.

The server replaces an existing result set instead of adding a new one when a
registration has the same workflow identity and unique key.
Use a single stable, human-readable label for --unique that describes the
output, such as a run, cohort, panel or parameter-set label, and reuse it for
future replacements.
Avoid a timestamp, random value, or output path unless every registration should
create a new result set.

Files:
  --input-file tracks input files separately from scanned outputs.
  --match limits scanned outputs with output-relative globs; repeat it to match
    any glob.
  --include-hidden includes hidden files and directories in the scan.
  --json reads a complete registration JSON payload from stdin instead of
    scanning output-dir.

Metadata:
  --run, --study, --sample and --library are sent to the results server, which
    resolves them through its configured MLWH cache and stores canonical seqmeta
    metadata keys. Normal CLI users do not need WA_MLWH_CACHE_PATH.
  --run accepts numeric run IDs.
  --study accepts LIMS ID, accession, UUID, or name.
  --sample accepts Sanger name, supplier name, id_sample_lims, sample UUID, or
    donor ID.
  --library accepts exact pipeline_id_lims, library_id, or id_library_lims
    values.
  --meta adds literal key=value metadata; repeat it to keep multiple values.

Server:
  --server selects the results server, and --cert sets the CA/cert path used to
  trust it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			registration, err := buildResultsRegistrationForCommand(
				ctx,
				cmd,
				args,
				requester,
				operator,
				commandLine,
				workflowReference,
				unique,
				legacyRunID,
				additionalUnique,
				inputFiles,
				matchPatterns,
				metaValues,
				lookupValues,
				includeHidden,
				useJSON,
			)
			if err != nil {
				return err
			}

			responseBody, err := registerResults(ctx, options.serverURL, options.certPath, registration)
			if err != nil {
				return err
			}

			return writeCommandJSON(cmd.OutOrStdout(), responseBody)
		},
	}

	command.Flags().StringVar(&requester, "user", "", "Requester or owner name (required)")
	command.Flags().StringVar(&operator, "operator", "", "Operator who performed the registration")
	command.Flags().StringVar(&commandLine, "command", "", "Command line that produced the results")
	command.Flags().StringVar(&workflowReference, "workflow", "", "Workflow identity (required): raw string, local Nextflow file, GitHub URL, or owner/repo")
	command.Flags().StringVar(&unique, "unique", "", "Stable unique run key for replacement (required)")
	command.Flags().StringArrayVar(&inputFiles, "input-file", nil, "Input file to track separately from outputs; repeat as needed")
	command.Flags().StringArrayVar(&matchPatterns, "match", nil, "Output-relative glob of files to register; repeat to match any glob")
	command.Flags().BoolVar(&includeHidden, "include-hidden", false, "Scan hidden files and directories")
	command.Flags().StringArrayVar(&lookupValues.Run, "run", nil, "MLWH id_run lookup; repeat as needed")
	command.Flags().StringArrayVar(&lookupValues.Study, "study", nil, "MLWH study lookup (LIMS ID, accession, UUID, or name); repeat as needed")
	command.Flags().StringArrayVar(&lookupValues.Sample, "sample", nil, "MLWH sample lookup (Sanger name, supplier name, id_sample_lims, sample UUID, or donor ID); repeat as needed")
	command.Flags().StringArrayVar(&lookupValues.Library, "library", nil, "MLWH library lookup (pipeline_id_lims, library_id, or id_library_lims); repeat as needed")
	command.Flags().StringArrayVar(&metaValues, "meta", nil, "Literal metadata as key=value; repeat to keep multiple values")
	command.Flags().BoolVar(&useJSON, "json", false, "Read complete registration JSON from stdin instead of scanning output-dir")
	command.Flags().StringVar(&workflowReference, "nextflow-workflow", "", "Deprecated alias for --workflow")
	command.Flags().StringVar(&legacyRunID, "runid", "", "Deprecated alias for --unique")
	command.Flags().StringVar(&additionalUnique, "additional-unique", "", "Deprecated extra unique label kept for old commands")
	_ = command.Flags().MarkHidden("runid")
	_ = command.Flags().MarkHidden("additional-unique")
	_ = command.Flags().MarkHidden("nextflow-workflow")
	command.Flags().SortFlags = false

	return command
}

func buildResultsRegistrationForCommand(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	requester string,
	operator string,
	commandLine string,
	workflowReference string,
	unique string,
	legacyRunID string,
	additionalUnique string,
	inputFiles []string,
	matchPatterns []string,
	metaValues []string,
	lookupValues results.RegistrationLookupValues,
	includeHidden bool,
	useJSON bool,
) (*results.Registration, error) {
	if useJSON {
		if len(args) != 0 {
			return nil, errors.New("usage: register --json")
		}

		registration, err := decodeResultsRegistration(cmd.InOrStdin())
		if err != nil {
			return nil, err
		}

		if err := results.ValidateRegistration(registration); err != nil {
			return nil, err
		}

		return registration, nil
	}

	if len(args) != 1 {
		return nil, errors.New("usage: register [output-dir]")
	}

	if strings.TrimSpace(requester) == "" {
		return nil, errors.New("--user is required")
	}

	operatorName, err := resultsRegisterOperatorName(operator)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(workflowReference) == "" {
		return nil, errors.New("--workflow is required")
	}

	uniqueValue, err := resultsRegisterUniqueValue(unique, legacyRunID)
	if err != nil {
		return nil, err
	}

	runKey := results.BuildRunKey(uniqueValue, strings.TrimSpace(additionalUnique))
	if runKey == "" {
		return nil, errors.New("--unique is required")
	}

	outputDir, err := filepath.Abs(args[0])
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}

	if err := validateResultsScanRoot(outputDir, includeHidden); err != nil {
		return nil, err
	}

	outputFiles, scanWarnings, err := results.ScanDirectory(outputDir, includeHidden, matchPatterns...)
	if err != nil {
		return nil, fmt.Errorf("scan output directory: %w", err)
	}

	writeResultsScanWarnings(cmd.ErrOrStderr(), scanWarnings)
	if len(outputFiles) == 0 {
		return nil, errors.New("no output files discovered in output directory")
	}

	metadata, metadataValues, err := parseResultsRegisterMetadata(metaValues)
	if err != nil {
		return nil, err
	}

	workflowIdentity, err := results.ResolveWorkflowIdentity(workflowReference)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow identity: %w", err)
	}

	trackedInputs, err := resultsRegisterInputFiles(inputFiles)
	if err != nil {
		return nil, err
	}

	workflowFiles, err := resultsRegisterWorkflowFiles(workflowIdentity)
	if err != nil {
		return nil, err
	}

	return &results.Registration{
		PipelineIdentifier: workflowIdentity.Identifier,
		RunKey:             runKey,
		Requester:          strings.TrimSpace(requester),
		Operator:           operatorName,
		Command:            strings.TrimSpace(commandLine),
		PipelineName:       workflowIdentity.Name,
		PipelineVersion:    workflowIdentity.Version,
		OutputDirectory:    outputDir,
		Files:              deduplicateResultsTrackedFiles(outputFiles, trackedInputs, workflowFiles...),
		Metadata:           metadata,
		MetadataValues:     metadataValues,
		LookupValues:       resultsRegisterLookupPayload(lookupValues),
	}, nil
}

func decodeResultsRegistration(input io.Reader) (*results.Registration, error) {
	var registration results.Registration
	decoder := json.NewDecoder(input)
	if err := decoder.Decode(&registration); err != nil {
		return nil, fmt.Errorf("decode registration JSON: %w", err)
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("decode registration JSON: unexpected trailing JSON")
		}

		return nil, fmt.Errorf("decode registration JSON: %w", err)
	}

	return &registration, nil
}

func parseResultsRegisterMetadata(metaValues []string) (map[string]string, map[string][]string, error) {
	metadataValues, err := parseResultsMetadataValueFilters(metaValues)
	if err != nil {
		return nil, nil, err
	}

	return singleResultsRegisterMetadata(metadataValues), metadataValues, nil
}

func registerResults(ctx context.Context, serverURL string, certPath string, registration *results.Registration) ([]byte, error) {
	body, err := marshalCommandJSON(registration)
	if err != nil {
		return nil, fmt.Errorf("marshal registration request: %w", err)
	}

	request, err := resultsRegisterAuthenticatedRequest(serverURL, certPath)
	if err != nil {
		return nil, err
	}

	response, err := request.
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Post(gas.EndPointAuth + "/results")
	if err != nil {
		return nil, fmt.Errorf("request register result: %w", err)
	}

	responseBody := response.Body()

	if response.StatusCode() != http.StatusCreated && response.StatusCode() != http.StatusOK {
		return nil, decodeResultsCommandError(response.StatusCode(), responseBody)
	}

	if !json.Valid(responseBody) {
		return nil, errors.New("results register response was not valid JSON")
	}

	return responseBody, nil
}

func newResultsSearchCommand(options *resultsCommandOptions) *cobra.Command {
	var requester string
	var operator string
	var pipelineName string
	var pipelineVersion string
	var pipelineIdentifier string
	var unique string
	var legacyRunKey string
	var outputDirPrefix string
	var metaValues []string

	command := &cobra.Command{
		Use:   "search",
		Short: "Search result sets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			uniqueValue, err := resultsSearchUniqueValue(unique, legacyRunKey)
			if err != nil {
				return err
			}

			query, err := buildResultsSearchQuery(requester, operator, pipelineName, pipelineVersion, pipelineIdentifier, uniqueValue, outputDirPrefix, metaValues)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			responseBody, err := searchResults(ctx, options.serverURL, options.certPath, query)
			if err != nil {
				return err
			}

			_, err = cmd.OutOrStdout().Write(responseBody)

			return err
		},
	}

	command.Flags().StringVar(&requester, "user", "", "Requester filter")
	command.Flags().StringVar(&operator, "operator", "", "Operator filter")
	command.Flags().StringVar(&pipelineName, "pipeline-name", "", "Pipeline name filter")
	command.Flags().StringVar(&pipelineVersion, "pipeline-version", "", "Pipeline version filter")
	command.Flags().StringVar(&pipelineIdentifier, "pipeline-identifier", "", "Pipeline identifier filter")
	command.Flags().StringVar(&unique, "unique", "", "Unique key filter")
	command.Flags().StringVar(&legacyRunKey, "run-key", "", "Deprecated alias for --unique")
	command.Flags().StringArrayVar(&metaValues, "meta", nil, "Metadata filter in key=value form")
	command.Flags().StringVar(&outputDirPrefix, "output-dir-prefix", "", "Output directory prefix filter")
	_ = command.Flags().MarkHidden("run-key")

	return command
}

func resultsSearchUniqueValue(unique, legacyRunKey string) (string, error) {
	trimmedUnique := strings.TrimSpace(unique)
	trimmedLegacyRunKey := strings.TrimSpace(legacyRunKey)

	if trimmedUnique != "" && trimmedLegacyRunKey != "" && trimmedUnique != trimmedLegacyRunKey {
		return "", errors.New("--unique and deprecated --run-key cannot both be set to different values")
	}

	if trimmedUnique != "" {
		return trimmedUnique, nil
	}

	return trimmedLegacyRunKey, nil
}

func buildResultsSearchQuery(requester, operator, pipelineName, pipelineVersion, pipelineIdentifier, runKey, outputDirPrefix string, metaValues []string) (url.Values, error) {
	query := url.Values{}
	if requester != "" {
		query.Set("user", requester)
	}

	if operator != "" {
		query.Set("operator", operator)
	}

	if pipelineName != "" {
		query.Set("pipeline_name", pipelineName)
	}

	if pipelineVersion != "" {
		query.Set("pipeline_version", pipelineVersion)
	}

	if pipelineIdentifier != "" {
		query.Set("pipeline_identifier", pipelineIdentifier)
	}

	if runKey != "" {
		query.Set("run_key", runKey)
	}

	if outputDirPrefix != "" {
		query.Set("output_dir_prefix", outputDirPrefix)
	}

	metadata, err := parseResultsMetadataFilters(metaValues)
	if err != nil {
		return nil, err
	}

	for key, value := range metadata {
		query.Set("meta_"+key, value)
	}

	return query, nil
}

func parseResultsMetadataFilters(metaValues []string) (map[string]string, error) {
	metadata := make(map[string]string, len(metaValues))

	for _, metaValue := range metaValues {
		key, value, err := parseResultsMetadataValue(metaValue)
		if err != nil {
			return nil, err
		}

		metadata[key] = value
	}

	return metadata, nil
}

func searchResults(ctx context.Context, serverURL string, certPath string, query url.Values) ([]byte, error) {
	endpoint, err := resultsEndpointURL(serverURL, gas.EndPointREST+"/results")
	if err != nil {
		return nil, fmt.Errorf("parse --server URL: %w", err)
	}
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create search request: %w", err)
	}

	effectiveCertPath := certPath
	if endpoint.Scheme == "http" {
		effectiveCertPath = ""
	}

	client, err := resultsPublicHTTPClient(effectiveCertPath)
	if err != nil {
		return nil, err
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request search results: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read search response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return nil, decodeResultsCommandError(response.StatusCode, body)
	}

	if !json.Valid(body) {
		return nil, errors.New("results search response was not valid JSON")
	}

	return body, nil
}

func newResultsGetCommand(options *resultsCommandOptions) *cobra.Command {
	var includeFiles bool

	command := &cobra.Command{
		Use:   "get <id>",
		Short: "Get one result set",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("usage: get <id>")
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			responseBody, err := getResult(ctx, options.serverURL, options.certPath, args[0], includeFiles)
			if err != nil {
				return err
			}

			return writeCommandJSON(cmd.OutOrStdout(), responseBody)
		},
	}

	command.Flags().BoolVar(&includeFiles, "files", false, "Include the tracked file list in the response")

	return command
}

func newResultsDeleteCommand(options *resultsCommandOptions) *cobra.Command {

	command := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete one result set",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("usage: delete <id>")
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			return deleteResult(ctx, options.serverURL, options.certPath, args[0])
		},
	}

	return command
}

func deleteResult(ctx context.Context, serverURL, certPath, resultID string) error {
	request, err := resultsOwnerAuthenticatedRequest(serverURL, certPath)
	if err != nil {
		return err
	}

	response, err := request.
		SetContext(ctx).
		Delete(gas.EndPointAuth + "/results/" + url.PathEscape(resultID))
	if err != nil {
		return fmt.Errorf("request delete: %w", err)
	}

	if response.StatusCode() != http.StatusNoContent {
		return decodeResultsCommandError(response.StatusCode(), response.Body())
	}

	return nil
}

func newResultsRescanCommand(options *resultsCommandOptions) *cobra.Command {
	var includeHidden bool

	command := &cobra.Command{
		Use:   "rescan <id> <dir>",
		Short: "Rescan registered output files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New("usage: rescan <id> <dir>")
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if err := validateResultsRescanDirectory(ctx, options.serverURL, options.certPath, args[0], args[1]); err != nil {
				return err
			}

			if err := validateResultsScanRoot(args[1], includeHidden); err != nil {
				return err
			}

			files, scanWarnings, err := results.ScanDirectory(args[1], includeHidden)
			if err != nil {
				return fmt.Errorf("scan output directory: %w", err)
			}

			writeResultsScanWarnings(cmd.ErrOrStderr(), scanWarnings)

			responseBody, err := rescanResults(ctx, options.serverURL, options.certPath, args[0], files)
			if err != nil {
				return err
			}

			if len(bytes.TrimSpace(responseBody)) == 0 {
				return nil
			}

			return writeCommandJSON(cmd.OutOrStdout(), responseBody)
		},
	}

	command.Flags().BoolVar(&includeHidden, "include-hidden", false, "Include hidden files and directories in the scan")

	return command
}

func rescanResults(ctx context.Context, serverURL, certPath, resultID string, files []results.FileEntry) ([]byte, error) {
	body, err := marshalCommandJSON(files)
	if err != nil {
		return nil, fmt.Errorf("marshal rescan request: %w", err)
	}

	request, err := resultsOwnerAuthenticatedRequest(serverURL, certPath)
	if err != nil {
		return nil, err
	}

	response, err := request.
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Put(gas.EndPointAuth + "/results/" + url.PathEscape(resultID) + "/files")
	if err != nil {
		return nil, fmt.Errorf("request rescan: %w", err)
	}

	responseBody := response.Body()
	if response.StatusCode() != http.StatusOK {
		return nil, decodeResultsCommandError(response.StatusCode(), responseBody)
	}

	return responseBody, nil
}

func defaultResultsServerURL() string {
	if serverURL := strings.TrimSpace(firstEnv("WA_RESULTS_SERVER_URL")); serverURL != "" {
		return serverURL
	}

	if backendURL := defaultResultsBackendServerURL(); backendURL != "" {
		return backendURL
	}

	port := strings.TrimSpace(activeResultsPort())
	if port != "" {
		return "https://127.0.0.1:" + port
	}

	return "https://localhost:8080"
}

func resultsEndpointURL(serverURL, resourcePath string) (*url.URL, error) {
	endpoint, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}

	endpoint.Path = path.Join(endpoint.Path, resourcePath)

	return endpoint, nil
}

func newResultsServeCommand() *cobra.Command {
	var port int
	var bindURL string
	var cert string
	var key string
	var acme string
	var cache string
	var ldapServer string
	var ldapDN string
	var serverToken string
	var dbPath string
	var mlwhCache string
	var seqmetaURL string
	var seqmetaTimeout time.Duration

	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve the results HTTP API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := commandContext(cmd)

			securityConfig, err := resolveResultsServeSecurityConfig(
				bindURL,
				port,
				cmd.Flags().Changed("port"),
				cert,
				key,
				acme,
				cache,
				ldapServer,
				ldapDN,
				serverToken,
			)
			if err != nil {
				return err
			}

			dsn, err := resolveResultsServeDBDSN(dbPath, cmd.Flags().Changed("db"))
			if err != nil {
				return err
			}

			mlwhConfig, enableMLWH, err := resolveResultsServeMLWHConfig(mlwhCache, cmd.Flags().Changed("mlwh-cache"))
			if err != nil {
				return err
			}

			db, err := openResultsDB(dsn)
			if err != nil {
				return err
			}

			store, err := results.NewStore(db)
			if err != nil {
				_ = db.Close()

				return err
			}
			defer func() { _ = store.Close() }()

			var mlwhClient resultsServeSyncClient
			if enableMLWH {
				mlwhClient, err = resultsServeOpenMLWHClient(ctx, mlwhConfig)
				if err != nil {
					return fmt.Errorf("open mlwh client: %w", err)
				}
				defer func() { _ = mlwhClient.Close() }()
			}

			var validator *results.SeqmetaValidator
			var resolver results.SearchResolver
			if strings.TrimSpace(seqmetaURL) != "" {
				validator = results.NewSeqmetaValidator(seqmetaURL, seqmetaTimeout)
			}
			if mlwhClient != nil {
				resolver = results.NewMLWHSearchResolver(mlwhClient)
			}

			authServer := resultsServeNewAuthServer(cmd.ErrOrStderr())
			ownerStore := results.NewOwnerSessionStore()

			ownerConfig, err := resultsServeOwnerSessionConfig(securityConfig.serverToken, ownerStore)
			if err != nil {
				return err
			}

			authServer.Router().Use(results.OwnerSessionMiddleware(ownerConfig))
			if err := authServer.EnableAuthWithServerToken(
				securityConfig.authCertFile(),
				securityConfig.authKeyFile(),
				securityConfig.serverToken,
				securityConfig.authCallback(),
			); err != nil {
				return err
			}

			results.NewServer(
				store,
				validator,
				resolver,
				results.WithOwnerSessionStore(ownerStore),
			).RegisterRoutes(authServer.Router(), authServer.AuthRouter())

			return startResultsServeAuthServer(ctx, authServer, securityConfig)
		},
	}

	command.Flags().StringVar(&bindURL, "url", "", "HTTPS bind address (defaults to active WA_*_RESULTS_HOST/PORT or 127.0.0.1:<port>)")
	command.Flags().IntVar(&port, "port", 8080, "Deprecated HTTPS port alias used only when --url is unset")
	command.Flags().StringVar(&cert, "cert", firstEnv("WA_RESULTS_SERVER_CERT"), "TLS certificate path")
	command.Flags().StringVarP(&key, "key", "k", firstEnv("WA_RESULTS_SERVER_KEY"), "TLS private key path")
	command.Flags().StringVarP(&acme, "acme", "a", firstEnv("WA_RESULTS_SERVER_ACME"), "ACME directory URL")
	command.Flags().StringVarP(&cache, "cache", "c", firstEnv("WA_RESULTS_SERVER_CACHE"), "ACME certificate cache directory")
	command.Flags().StringVarP(&ldapServer, "ldap_server", "s", firstEnv("WA_RESULTS_LDAP_SERVER"), "LDAP server FQDN")
	command.Flags().StringVarP(&ldapDN, "ldap_dn", "l", firstEnv("WA_RESULTS_LDAP_DN"), "LDAP bind DN template containing %s")
	command.Flags().StringVar(&serverToken, "server-token", resultsServerTokenBasename, "Server token basename or absolute path")
	command.Flags().StringVar(&dbPath, "db", "results.db", "SQLite database path or MySQL DSN without a password; defaults to WA_RESULTS_DB_PATH when unset")
	command.Flags().StringVar(&mlwhCache, "mlwh-cache", "", "MLWH cache backend path or MySQL DSN without a password; defaults to WA_MLWH_CACHE_PATH when unset")
	command.Flags().StringVar(&seqmetaURL, "seqmeta-url", firstEnv("WA_SEQMETA_BACKEND_URL"), "Base URL for seqmeta validation (defaults to WA_SEQMETA_BACKEND_URL)")
	command.Flags().DurationVar(&seqmetaTimeout, "seqmeta-timeout", 30*time.Second, "Timeout for seqmeta validation requests")

	return command
}

func openResultsDB(dsn string) (*sql.DB, error) {
	driverName := resultsDBDriverName(dsn)
	if driverName == "sqlite" {
		if err := validateResultsSQLiteDBPath(dsn); err != nil {
			return nil, err
		}

		dsn = resultsSQLiteDSN(dsn)
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}

	if driverName == "sqlite" && dsn == ":memory:" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}

	return db, nil
}

func resultsSQLiteDSN(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == ":memory:" || strings.HasPrefix(trimmed, "file:") {
		return trimmed
	}

	return fmt.Sprintf("file:%s?mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", filepath.ToSlash(trimmed))
}

func resultsDBDriverName(dsn string) string {
	trimmedDSN := strings.TrimSpace(dsn)
	if strings.Contains(trimmedDSN, "@tcp(") || strings.Contains(trimmedDSN, "@unix(") {
		return "mysql"
	}

	return "sqlite"
}

func resultsRegisterInputFiles(paths []string) ([]results.FileEntry, error) {
	entries := make([]results.FileEntry, 0, len(paths))

	for _, filePath := range paths {
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return nil, fmt.Errorf("resolve input file %q: %w", filePath, err)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("stat input file %q: %w", filePath, err)
		}

		if info.IsDir() {
			return nil, fmt.Errorf("input file %q: is a directory", filePath)
		}

		entries = append(entries, results.FileEntry{
			Path:  absPath,
			Mtime: info.ModTime(),
			Size:  info.Size(),
			Kind:  "input",
		})
	}

	return entries, nil
}

func writeResultsScanWarnings(output io.Writer, warnings int) {
	if warnings <= 0 || output == nil {
		return
	}

	_, _ = fmt.Fprintf(output, "warning: skipped %d path(s) while scanning output files\n", warnings)
}

func deduplicateResultsTrackedFiles(outputFiles, inputFiles []results.FileEntry, pipelineFiles ...results.FileEntry) []results.FileEntry {
	files := append(append(append(make([]results.FileEntry, 0, len(outputFiles)+len(inputFiles)+len(pipelineFiles)), outputFiles...), inputFiles...), pipelineFiles...)
	keepIndexByPath := make(map[string]int, len(files))
	for index, file := range files {
		keepIndexByPath[file.Path] = index
	}

	uniqueFiles := make([]results.FileEntry, 0, len(keepIndexByPath))
	for index, file := range files {
		if keepIndexByPath[file.Path] != index {
			continue
		}

		uniqueFiles = append(uniqueFiles, file)
	}

	return uniqueFiles
}

func validateResultsScanRoot(rootDir string, includeHidden bool) error {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil
	}

	visitedDirs := map[string]struct{}{resolvedRoot: {}}

	return validateResultsScanTree(absRoot, absRoot, resolvedRoot, includeHidden, visitedDirs)
}

func validateResultsRescanDirectory(ctx context.Context, serverURL, certPath, resultID, dir string) error {
	resultBody, err := getResultsResourceWithAuth(
		ctx,
		serverURL,
		certPath,
		gas.EndPointAuth+"/results/"+url.PathEscape(resultID),
		http.StatusOK,
		"get result",
		resultsOwnerAuthenticatedRequest,
	)
	if err != nil {
		return err
	}

	var resultSet results.ResultSet
	if err := json.Unmarshal(resultBody, &resultSet); err != nil {
		return fmt.Errorf("decode result set: %w", err)
	}

	if !resultsSameCanonicalDirectory(dir, resultSet.OutputDirectory) {
		return fmt.Errorf("rescan directory %q does not match registered output directory %q", dir, resultSet.OutputDirectory)
	}

	return nil
}

func resultsSameCanonicalDirectory(firstPath, secondPath string) bool {
	firstAbs, firstErr := filepath.Abs(firstPath)
	secondAbs, secondErr := filepath.Abs(secondPath)
	if firstErr != nil || secondErr != nil {
		return false
	}

	firstResolved, firstResolvedOK := resolveResultsPath(firstAbs)
	secondResolved, secondResolvedOK := resolveResultsPath(secondAbs)
	if firstResolvedOK && secondResolvedOK {
		return firstResolved == secondResolved
	}

	return filepath.Clean(firstAbs) == filepath.Clean(secondAbs)
}

func resolveResultsPath(path string) (string, bool) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}

	return resolvedPath, true
}

func validateResultsScanTree(rootDir, dir, resolvedRoot string, includeHidden bool, visitedDirs map[string]struct{}) error {
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, child := range children {
		name := child.Name()
		if !includeHidden && strings.HasPrefix(name, ".") {
			continue
		}

		childPath := filepath.Join(dir, name)
		info, err := os.Stat(childPath)
		if err != nil || !info.IsDir() {
			continue
		}

		resolvedPath, err := filepath.EvalSymlinks(childPath)
		if err != nil {
			continue
		}

		if !resultsPathWithinDirectory(resolvedRoot, resolvedPath) {
			return fmt.Errorf("scan output directory: directory symlink %q resolves outside %q", childPath, rootDir)
		}

		if _, seen := visitedDirs[resolvedPath]; seen {
			continue
		}

		visitedDirs[resolvedPath] = struct{}{}
		if err := validateResultsScanTree(rootDir, childPath, resolvedRoot, includeHidden, visitedDirs); err != nil {
			return err
		}
	}

	return nil
}

func resultsPathWithinDirectory(rootPath, candidatePath string) bool {
	relPath, err := filepath.Rel(rootPath, candidatePath)
	if err != nil {
		return false
	}

	return relPath == "." || (relPath != ".." && !strings.HasPrefix(relPath, ".."+string(os.PathSeparator)))
}

func resultsAuthAddr(serverURL string) (string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return "", err
	}

	if endpoint.Scheme != "https" {
		return "", errors.New("results server URL must use https")
	}

	if endpoint.Host == "" {
		return "", errors.New("results server URL must include a host")
	}

	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return "", errors.New("go-authserver CLI auth endpoints require an origin URL with no path")
	}

	return endpoint.Host, nil
}

func resultsTokenPath(tokenBasename string) (string, error) {
	if filepath.IsAbs(tokenBasename) {
		return tokenBasename, nil
	}

	tokenDir, err := gas.TokenDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(tokenDir, tokenBasename), nil
}

func resolveResultsServeMLWHCachePath(flagValue string, flagChanged bool) (string, bool, error) {
	cachePath := strings.TrimSpace(flagValue)
	sourceName := "--mlwh-cache"
	if !flagChanged {
		if envValue := strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH")); envValue != "" {
			cachePath = envValue
			sourceName = "WA_MLWH_CACHE_PATH"
		}
	}

	if cachePath == "" {
		return "", false, nil
	}

	if !mlwhSyncCachePathLooksMySQL(cachePath) {
		return cachePath, true, nil
	}

	parsed, err := mysql.ParseDSN(cachePath)
	if err != nil {
		return "", false, fmt.Errorf("parse %s: %w", sourceName, err)
	}

	if parsed.Passwd != "" {
		return "", false, fmt.Errorf("%s: %w", sourceName, mlwh.ErrPasswordInDSN)
	}

	return cachePath, true, nil
}
