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
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/mlwh"
)

var openMLWHSyncClient = func(ctx context.Context, cfg mlwh.Config) (mlwhSyncClient, error) {
	return mlwh.Open(ctx, cfg)
}

var mlwhServeNewAuthServer = func(logWriter io.Writer) mlwhServeAuthServer {
	return &mlwhServeGasAuthServer{Server: gas.New(logWriter)}
}

func mlwhServeRejectPasswordAuth(_, _ string) (bool, string) {
	return false, ""
}

type mlwhSyncClient interface {
	Sync(context.Context) ([]mlwh.SyncReport, error)
	Close() error
}

type mlwhSyncReportingClient interface {
	mlwhSyncClient
	SetSyncReportWriter(io.Writer)
}

type mlwhServeAuthServer interface {
	Router() *gin.Engine
	AuthRouter() *gin.RouterGroup
	EnableAuthWithServerToken(certFile, keyFile, tokenBasename string, acb gas.AuthCallback) error
	StartHTTP(ctx context.Context, addr string) error
	Start(addr, certFile, keyFile string) error
	Stop()
}

func startMLWHServeAuthServer(ctx context.Context, authServer mlwhServeAuthServer, config mlwhServeConfig) error {
	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	defer authServer.Stop()

	if !config.secured {
		return authServer.StartHTTP(serveCtx, config.addr)
	}

	go func() {
		<-serveCtx.Done()
		authServer.Stop()
	}()

	return authServer.Start(config.addr, config.cert, config.key)
}

type mlwhServeGasAuthServer struct {
	*gas.Server
}

func (s *mlwhServeGasAuthServer) StartHTTP(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	httpServer := &http.Server{
		Handler:           s.Router(),
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

type mlwhServeConfig struct {
	addr        string
	cert        string
	key         string
	serverToken string
	secured     bool
}

func resolveMLWHServeConfig(rawURL string, port int, portChanged bool, cert string, key string, serverToken string) (mlwhServeConfig, error) {
	addr, err := resolveMLWHServeBindAddr(rawURL, port, portChanged)
	if err != nil {
		return mlwhServeConfig{}, err
	}

	config := mlwhServeConfig{
		addr:        addr,
		cert:        strings.TrimSpace(cert),
		key:         strings.TrimSpace(key),
		serverToken: strings.TrimSpace(serverToken),
	}
	config.secured = config.cert != "" || config.key != "" || config.serverToken != ""
	if !config.secured {
		return config, nil
	}

	if config.cert == "" || config.key == "" || config.serverToken == "" {
		return mlwhServeConfig{}, errors.New("--cert, --key, and --server-token are required together for secured mlwh serve")
	}

	if err = validateResultsServeServerToken(config.serverToken); err != nil {
		return mlwhServeConfig{}, err
	}

	return config, nil
}

func newMLWHServeCommand() *cobra.Command {
	var port int
	var bindURL string
	var cert string
	var key string
	var serverToken string
	var mlwhCache string

	command := &cobra.Command{
		Use:           "serve",
		Short:         "Serve the MLWH cache-backed HTTP API",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: strings.Join([]string{
			"Serve the local Sanger Multi-LIMS Warehouse (MLWH) metadata cache as",
			"the registry-backed read-only HTTP API used by other wa services.",
			"",
			"The server opens only the local cache and never contacts the upstream",
			"MLWH database or runs a sync; WA_MLWH_DSN is intentionally unused here.",
			"Populate the cache separately with 'wa mlwh sync' before serving.",
			"",
			"Configuration is read from the environment after the persistent --env",
			"flag has loaded matching .env files. Set WA_MLWH_CACHE_PATH or pass",
			"--mlwh-cache to choose the cache, and optionally set WA_MLWH_SERVER_CERT,",
			"WA_MLWH_SERVER_KEY, and WA_MLWH_SERVER_TOKEN together to secure the",
			"server. Without those TLS/token settings, mlwh serve is plain HTTP.",
			"Bind defaults come from the active WA_*_SEQMETA_HOST/PORT scenario",
			"variables, or WA_MLWH_SERVER_PORT when no scenario port is active.",
			"WA_MLWH_SERVER_URL is the public client URL used by wa mlwh info and",
			"mlwhdiff; it is not used as a bind address.",
			"",
			"Example:",
			"  WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite wa --env production mlwh serve",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cacheConfig, err := resolveMLWHServeCacheConfig(mlwhCache, cmd.Flags().Changed("mlwh-cache"))
			if err != nil {
				return err
			}

			serveConfig, err := resolveMLWHServeConfig(
				bindURL,
				port,
				cmd.Flags().Changed("port"),
				cert,
				key,
				serverToken,
			)
			if err != nil {
				return err
			}

			client, err := mlwh.OpenCacheOnly(commandContext(cmd), cacheConfig)
			if err != nil {
				return fmt.Errorf("open mlwh cache: %w", err)
			}
			defer func() { _ = client.Close() }()

			authServer := mlwhServeNewAuthServer(cmd.ErrOrStderr())
			server := mlwh.NewServer(client)
			if serveConfig.secured {
				if err = authServer.EnableAuthWithServerToken(
					serveConfig.cert,
					serveConfig.key,
					serveConfig.serverToken,
					mlwhServeRejectPasswordAuth,
				); err != nil {
					return err
				}

				configureMLWHServeRouter(authServer.Router())
				server.RegisterRoutes(authServer.Router(), authServer.AuthRouter())
			} else {
				server.RegisterRoutes(authServer.Router(), nil)
			}

			return startMLWHServeAuthServer(commandContext(cmd), authServer, serveConfig)
		},
	}

	command.Flags().StringVar(&bindURL, "url", "", "bind host:port or URL (defaults to active WA_*_SEQMETA_HOST/PORT or 127.0.0.1:<port>)")
	command.Flags().IntVar(&port, "port", 8080, "listen port used only when --url is unset")
	command.Flags().StringVar(&cert, "cert", firstEnv("WA_MLWH_SERVER_CERT"), "TLS certificate path")
	command.Flags().StringVarP(&key, "key", "k", firstEnv("WA_MLWH_SERVER_KEY"), "TLS private key path")
	command.Flags().StringVar(&serverToken, "server-token", firstEnv("WA_MLWH_SERVER_TOKEN"), "Server token basename or absolute path")
	command.Flags().StringVar(&mlwhCache, "mlwh-cache", "", "MLWH cache backend path or MySQL DSN without a password; defaults to WA_MLWH_CACHE_PATH when unset")

	return command
}

func resolveMLWHServeCacheConfig(flagValue string, flagChanged bool) (mlwh.CacheConfig, error) {
	cachePath := strings.TrimSpace(flagValue)
	sourceName := "--mlwh-cache"
	if !flagChanged {
		if envValue := strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH")); envValue != "" {
			cachePath = envValue
			sourceName = "WA_MLWH_CACHE_PATH"
		}
	}

	if cachePath == "" {
		return mlwh.CacheConfig{}, errors.New("WA_MLWH_CACHE_PATH must be set or --mlwh-cache provided")
	}

	if err := validateMLWHServeCachePath(cachePath, sourceName); err != nil {
		return mlwh.CacheConfig{}, err
	}

	if err := ensureMLWHSyncCacheDirectory(cachePath); err != nil {
		return mlwh.CacheConfig{}, err
	}

	return mlwh.CacheConfig{
		Path:     cachePath,
		Password: firstEnv("WA_MLWH_CACHE_PASSWORD"),
	}, nil
}

func validateMLWHServeCachePath(cachePath string, sourceName string) error {
	if !mlwhSyncCachePathLooksMySQL(cachePath) {
		return nil
	}

	parsed, err := mysql.ParseDSN(cachePath)
	if err != nil {
		return fmt.Errorf("parse %s: %w", sourceName, err)
	}

	if parsed.Passwd != "" {
		return fmt.Errorf("%s: %w", sourceName, mlwh.ErrPasswordInDSN)
	}

	return nil
}

func configureMLWHServeRouter(router *gin.Engine) {
	if router == nil {
		return
	}

	router.UseRawPath = true
	router.UnescapePathValues = false
}

func newMLWHCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "mlwh",
		Short: "Manage the local cache of Sanger MLWH metadata",
		Long: strings.Join([]string{
			"Manage the local cache of Sanger Multi-LIMS Warehouse (MLWH) metadata.",
			"",
			"wa keeps a mirrored local cache of five MLWH tables (study, sample,",
			"iseq_flowcell, iseq_product_metrics and",
			"seq_product_irods_locations) so commands such as 'wa results register' and 'wa",
			"mlwhdiff serve' can resolve sample, study, run and library lookups",
			"without re-querying the upstream MySQL warehouse on every call.",
			"Use these subcommands to populate and refresh that cache.",
			"",
			"Configuration is read from the environment. Use the persistent --env",
			"flag (or WA_ENV=development|test|production) to load matching",
			".env.<name> / .env.<name>.local files from the working directory",
			"before resolving these variables:",
			"",
			"  WA_MLWH_DSN             Required. Go MySQL DSN for the upstream",
			"                          MLWH, e.g.",
			"                          user@tcp(mlwh-db-ro:3435)/mlwarehouse.",
			"                          Embedded passwords are rejected; use",
			"                          WA_MLWH_PASSWORD instead.",
			"  WA_MLWH_PASSWORD        Optional. Password injected into the DSN",
			"                          at connect time.",
			"  WA_MLWH_CACHE_PATH      Optional. Path to the local SQLite cache",
			"                          file. Defaults to <user-cache-dir>/wa/",
			"                          mlwh.sqlite (created on first use).",
			"  WA_MLWH_CACHE_PASSWORD  Optional. SQLCipher key used to encrypt",
			"                          the local cache when set.",
			"",
			"Example:",
			"  WA_MLWH_DSN='mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse' \\",
			"  WA_MLWH_PASSWORD='secret' \\",
			"      wa --env development mlwh sync",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	command.AddCommand(newMLWHSyncCommand())
	command.AddCommand(newMLWHInfoCommand())
	command.AddCommand(newMLWHSearchCommand())
	command.AddCommand(newMLWHServeCommand())

	return command
}

func newMLWHSyncCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "sync",
		Short:         "Sync the five mirrored MLWH tables into the local cache",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: strings.Join([]string{
			"Sync rows from the upstream Sanger MLWH MySQL database into the",
			"local mirrored cache used by other wa subcommands.",
			"",
			"Run this command to (re)populate the cache before commands that",
			"resolve sample, study, run or library lookups, or on a schedule",
			"to keep the cache fresh. Each run incrementally pulls new and",
			"updated rows for study, sample, iseq_flowcell,",
			"iseq_product_metrics and seq_product_irods_locations, and",
			"prints an inserted/updated/high-water summary per table. The first",
			"run can be slow because it cold-loads the full table set.",
			"",
			"Configuration is read from the environment. Use the persistent",
			"--env flag (or WA_ENV=development|test|production) to load matching",
			".env.<name> / .env.<name>.local files from the working directory",
			"before resolving:",
			"",
			"  WA_MLWH_DSN             Required. Go MySQL DSN for the upstream",
			"                          MLWH, e.g.",
			"                          user@tcp(mlwh-db-ro:3435)/mlwarehouse.",
			"                          Embedded passwords are rejected; use",
			"                          WA_MLWH_PASSWORD instead.",
			"  WA_MLWH_PASSWORD        Optional. Password injected into the DSN",
			"                          at connect time.",
			"  WA_MLWH_CACHE_PATH      Optional. Path to the local SQLite cache",
			"                          file. Defaults to <user-cache-dir>/wa/",
			"                          mlwh.sqlite (created on first use).",
			"  WA_MLWH_CACHE_PASSWORD  Optional. SQLCipher key used to encrypt",
			"                          the local cache when set.",
			"",
			"Examples:",
			"  # Full incremental sync of all supported MLWH tables",
			"  WA_MLWH_DSN='mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse' \\",
			"  WA_MLWH_PASSWORD='secret' \\",
			"      wa --env development mlwh sync",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveMLWHSyncConfig()
			if err != nil {
				return reportMLWHSyncCommandError(cmd, err)
			}

			client, err := openMLWHSyncClient(cmd.Context(), cfg)
			if err != nil {
				if errors.Is(err, mlwh.ErrPasswordInDSN) {
					return reportMLWHSyncCommandError(cmd, fmt.Errorf("WA_MLWH_DSN: %w", err))
				}

				return reportMLWHSyncCommandError(cmd, fmt.Errorf("open mlwh client: %w", err))
			}
			defer func() { _ = client.Close() }()

			reportingClient, streamsReports := client.(mlwhSyncReportingClient)
			if streamsReports {
				reportingClient.SetSyncReportWriter(cmd.OutOrStdout())
			}

			reports, err := client.Sync(cmd.Context())
			if err != nil {
				return reportMLWHSyncCommandError(cmd, err)
			}

			if streamsReports {
				return nil
			}

			for _, report := range reports {
				_, _ = fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s inserted=%d updated=%d high_water=%s\n",
					report.Table,
					report.Inserted,
					report.Updated,
					report.HighWater.UTC().Format("2006-01-02T15:04:05Z"),
				)
			}

			return nil
		},
	}

	command.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return reportMLWHSyncCommandError(cmd, err)
	})

	return command
}

func reportMLWHSyncCommandError(cmd *cobra.Command, err error) error {
	if cmd != nil && err != nil {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err.Error())
	}

	return err
}

func resolveMLWHSyncConfig() (mlwh.Config, error) {
	dsn := firstEnv("WA_MLWH_DSN")
	if dsn == "" {
		return mlwh.Config{}, errors.New("WA_MLWH_DSN must be set")
	}

	cachePath, err := resolveMLWHSyncCachePath()
	if err != nil {
		return mlwh.Config{}, err
	}

	return mlwh.Config{
		DSN:      dsn,
		Password: firstEnv("WA_MLWH_PASSWORD"),
		Cache: mlwh.CacheConfig{
			Path:     cachePath,
			Password: firstEnv("WA_MLWH_CACHE_PASSWORD"),
		},
	}, nil
}

func resolveMLWHSyncCachePath() (string, error) {
	if cachePath := firstEnv("WA_MLWH_CACHE_PATH"); cachePath != "" {
		return cachePath, nil
	}

	baseDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("determine default WA_MLWH_CACHE_PATH: %w", err)
	}

	cachePath := filepath.Join(baseDir, "wa", "mlwh.sqlite")
	if err = ensureMLWHSyncCacheDirectory(cachePath); err != nil {
		return "", err
	}

	return cachePath, nil
}

func ensureMLWHSyncCacheDirectory(cachePath string) error {
	trimmedPath := strings.TrimSpace(cachePath)
	if trimmedPath == "" || trimmedPath == ":memory:" || strings.HasPrefix(trimmedPath, "file:") || mlwhSyncCachePathLooksMySQL(trimmedPath) {
		return nil
	}

	dirPath := filepath.Dir(trimmedPath)
	if dirPath == "." {
		return nil
	}

	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return fmt.Errorf("create mlwh cache directory: %w", err)
	}

	return nil
}

func mlwhSyncCachePathLooksMySQL(path string) bool {
	parsed, err := mysql.ParseDSN(path)
	if err != nil {
		return false
	}

	return parsed.DBName != "" && (parsed.User != "" || parsed.Net != "" || parsed.Addr != "" || strings.Contains(path, "@"))
}

func resolveMLWHServeBindAddr(rawURL string, port int, portChanged bool) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		bindPort, err := resolveMLWHServeBindPort(port, portChanged)
		if err != nil {
			return "", err
		}

		host := strings.TrimSpace(activeMLWHBindHost())
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
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", errors.New("mlwh serve URL must use http or https")
		}
		if parsed.User != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", errors.New("mlwh serve URL must be a host[:port] with no path")
		}

		trimmed = parsed.Host
	}

	if strings.ContainsAny(trimmed, "/?#") {
		return "", errors.New("mlwh serve bind address must be host:port")
	}

	if _, portValue, err := net.SplitHostPort(trimmed); err != nil {
		return "", fmt.Errorf("mlwh serve bind address must be host:port: %w", err)
	} else if portValue == "" {
		return "", errors.New("mlwh serve bind address must include a port")
	}

	return trimmed, nil
}

func resolveMLWHServeBindPort(flagValue int, flagChanged bool) (int, error) {
	port := flagValue
	source := "--port"
	if !flagChanged {
		envPort := strings.TrimSpace(activeMLWHPort())
		source = "active WA_*_SEQMETA_PORT"
		if envPort == "" {
			envPort = strings.TrimSpace(firstEnv("WA_MLWH_SERVER_PORT"))
			source = "WA_MLWH_SERVER_PORT"
		}
		if envPort != "" {
			parsedPort, err := strconv.Atoi(envPort)
			if err != nil {
				return 0, fmt.Errorf("invalid %s %q", source, envPort)
			}

			port = parsedPort
		}
	}

	if port < 0 || port > 65535 {
		return 0, fmt.Errorf("invalid %s %d", source, port)
	}

	return port, nil
}

func activeMLWHBindHost() string {
	switch firstEnv("WA_ENV") {
	case "development":
		return firstEnv("WA_DEV_SEQMETA_HOST")
	case "production":
		return firstEnv("WA_PROD_SEQMETA_HOST")
	default:
		return ""
	}
}
