// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	adminhttp "github.com/nguywnben/tanpopo/internal/admin/http"
	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	extport "github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/oauth"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	telemetry "github.com/nguywnben/tanpopo/internal/telemetry/otel"
	"github.com/nguywnben/tanpopo/internal/webui"
)

func newServeCommand() *cobra.Command {
	role := "all"
	listen := envOr("TANPOPO_LISTEN_ADDR", "127.0.0.1:8080")
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run Tanpopo services from the unified binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if role != "all" && role != "data-plane" && role != "control-plane" {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: fmt.Sprintf("invalid --role %q; expected all, data-plane, or control-plane", role)}
			}
			if _, err := drainTimeoutFromEnv(os.LookupEnv); err != nil {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: "invalid drain timeout configuration", Cause: err}
			}
			if err := serve(cmd.Context(), role, listen); err != nil {
				return &testharness.ExitError{Code: testharness.ExitInternal, Message: "serve failed", Cause: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", role, "Service role (all|data-plane|control-plane)")
	cmd.Flags().StringVar(&listen, "listen", listen, "HTTP listen address (default 127.0.0.1:8080)")
	return cmd
}

func serve(ctx context.Context, role, listen string) (serveErr error) {
	drainTimeout, err := drainTimeoutFromEnv(os.LookupEnv)
	if err != nil {
		return err
	}
	handler, closeRuntime, err := buildRuntime(ctx, role, listen)
	if err != nil {
		return err
	}
	defer func() { serveErr = errors.Join(serveErr, closeRuntime()) }()
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listen, err)
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

const defaultDrainTimeout = 90 * time.Second
const maxDrainTimeout = 300 * time.Second

func drainTimeoutFromEnv(lookup func(string) (string, bool)) (time.Duration, error) {
	raw, ok := lookup("TANPOPO_DRAIN_TIMEOUT")
	if !ok || strings.TrimSpace(raw) == "" {
		return defaultDrainTimeout, nil
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || timeout < 0 || timeout > maxDrainTimeout {
		return 0, fmt.Errorf("TANPOPO_DRAIN_TIMEOUT must be between 0s and %s", maxDrainTimeout)
	}
	return timeout, nil
}

func buildRuntime(ctx context.Context, role, listen string) (http.Handler, func() error, error) {
	if role != "all" && role != "data-plane" && role != "control-plane" {
		return nil, nil, fmt.Errorf("invalid service role %q", role)
	}
	dataDir := envOr("TANPOPO_DATA_DIR", "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("secure data directory: %w", err)
	}
	pluginsDir := envOr("TANPOPO_PLUGINS_DIR", filepath.Join(dataDir, "plugins"))
	if err := os.MkdirAll(pluginsDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create plugins directory: %w", err)
	}
	dbPath := envOr("TANPOPO_DB_PATH", filepath.Join(dataDir, "tanpopo.db"))
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		return nil, nil, fmt.Errorf("open local SQLite store: %w", err)
	}
	closeStore := func() { _ = store.Close() }
	db := store.RawDB()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		closeStore()
		return nil, nil, fmt.Errorf("initialize audit logger: %w", err)
	}
	ledgerService, err := ledger.NewService(ledger.Config{DB: db, Audit: auditLog})
	if err != nil {
		closeStore()
		return nil, nil, fmt.Errorf("initialize usage ledger: %w", err)
	}
	kek, err := vault.LoadStaticKEKFromEnv("kek_v1", os.LookupEnv)
	if err != nil {
		closeStore()
		return nil, nil, fmt.Errorf("load Master KEK: %w", err)
	}
	closeKeys := func() { kek.Close() }
	vaultStore, err := vault.NewStore(vault.Config{DB: db, KEK: kek, PostShredHook: store.CheckpointWAL})
	if err != nil {
		closeKeys()
		closeStore()
		return nil, nil, fmt.Errorf("initialize vault: %w", err)
	}
	snaps, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db})
	if err != nil {
		closeKeys()
		closeStore()
		return nil, nil, fmt.Errorf("initialize configuration snapshot: %w", err)
	}
	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	watcherDone := make(chan struct{})
	var marketplaceDone <-chan struct{}
	go func() {
		defer close(watcherDone)
		snaps.StartWatcher(runtimeCtx, time.Second)
	}()
	var closeDataServer func() error
	var dataServerReady func(context.Context) error
	var telemetryPipeline *telemetry.Pipeline
	var cleanupOnce sync.Once
	var shutdownErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			if closeDataServer != nil {
				shutdownErr = errors.Join(shutdownErr, closeDataServer())
			}
			if telemetryPipeline != nil {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				shutdownErr = errors.Join(shutdownErr, telemetryPipeline.Shutdown(shutdownCtx))
				cancel()
			}
			cancelRuntime()
			<-watcherDone
			if marketplaceDone != nil {
				<-marketplaceDone
			}
			vaultStore.Close()
			closeKeys()
			shutdownErr = errors.Join(shutdownErr, store.Close())
		})
		return shutdownErr
	}
	killSwitch, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{DB: db, Audit: auditLog, Streams: killswitch.NewStreamRegistry()})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize kill switches: %w", err)
	}
	snaps.RegisterKillSwitchSync(killSwitch.RefreshFromDB)
	catalogRepo, err := catalog.NewRepository(catalog.Config{DB: db, Snapshots: snaps, Audit: auditLog})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize provider catalog: %w", err)
	}
	redirectURI := strings.TrimSpace(os.Getenv("TANPOPO_OAUTH_REDIRECT_URI"))
	allowHermeticHTTP := strings.TrimSpace(os.Getenv("TANPOPO_TEST_HERMETIC")) == "1"
	if redirectURI == "" && allowHermeticHTTP {
		host, port, splitErr := net.SplitHostPort(listen)
		if splitErr == nil {
			host = strings.Trim(host, "[]")
			ip := net.ParseIP(host)
			if host == "localhost" || (ip != nil && ip.IsLoopback()) {
				redirectURI = "http://" + net.JoinHostPort(host, port) + "/admin/v1/oauth/callback"
			}
		}
	}
	brokerUnsealCap := vault.MintUnsealCapabilityForBroker()
	var oauthService *oauth.Service
	if redirectURI != "" {
		oauthService, err = oauth.NewService(oauth.ServiceConfig{DB: db, Vault: vaultStore, UnsealCapability: brokerUnsealCap, Audit: auditLog, HostResolver: egress.NewStdHostResolver(), RedirectURI: redirectURI, AllowLoopbackHTTP: allowHermeticHTTP})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize OAuth coordinator: %w", err)
		}
	}
	lockfile := envOr("TANPOPO_LOCK_FILE", filepath.Join(dataDir, "tanpopo.lock"))
	installerService, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB: db, Audit: auditLog, AuditEntryPoint: "admin_api", PluginsDir: pluginsDir, LockfilePath: lockfile,
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize extension installer: %w", err)
	}
	registryStore, err := registry.NewSQLStore(db, time.Now)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize marketplace registry store: %w", err)
	}
	marketplaceDone = startMarketplaceRuntime(runtimeCtx, registryStore, installerService, snaps, auditLog)
	blindBroker, err := broker.NewBlindBroker(broker.Config{
		Resolver: catalogRepo, PolicyResolver: catalogRepo, Vault: vaultStore,
		UnsealCap: brokerUnsealCap, Audit: auditLog, OAuthTokens: oauthService,
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize credential broker: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if ctx.Err() != nil || store.Ping(checkCtx) != nil || vaultStore.CheckServingReady(checkCtx) != nil || snaps.CheckServingReady() != nil || (dataServerReady != nil && dataServerReady(checkCtx) != nil) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	var dataServer *ingresshttp.Server
	if role == "all" || role == "data-plane" {
		telemetryPipeline, err = telemetry.NewPipeline(telemetry.PipelineConfig{
			ServiceName: "tanpopo", ServiceVersion: version, DB: db, Vault: vaultStore,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize telemetry pipeline: %w", err)
		}
		dataServer, err = ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots: snaps, Broker: blindBroker, KillSwitch: killSwitch, PluginsDir: pluginsDir, Audit: auditLog, IdempotencyDB: db,
			BudgetLedger: ledgerService,
			Telemetry:    telemetryPipeline,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize Data Plane: %w", err)
		}
		closeDataServer = func() error {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return dataServer.Close(shutdownCtx)
		}
		dataServerReady = dataServer.CheckServingReady
		mux.Handle("/v1/", dataServer)
		mux.Handle("/metrics", dataServer.MetricsHandler())
	}
	if role == "all" || role == "control-plane" {
		bootstrapManager, err := bootstrap.NewManager(bootstrap.ManagerConfig{
			DB: db, TokenPath: envOr("TANPOPO_BOOTSTRAP_TOKEN_FILE", filepath.Join(dataDir, "bootstrap.token")), AuditSink: auditLog,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize bootstrap service: %w", err)
		}
		if _, err := bootstrapManager.EnsureToken(ctx); err != nil && !errors.Is(err, bootstrap.ErrBootstrapAlreadyCompleted) {
			cleanup()
			return nil, nil, fmt.Errorf("ensure bootstrap token: %w", err)
		}
		authService, err := auth.NewLocalService(auth.ServiceConfig{DB: db, AuditSink: auditLog, AllowedOrigins: allowedOrigins(listen)})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize local authentication: %w", err)
		}
		vkeys, err := vkey.NewService(vkey.ServiceConfig{DB: db, Snapshots: snaps, AuditSink: auditLog})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize virtual keys: %w", err)
		}
		var providerRuntimeFactory func(context.Context, string, string) (extport.ProviderRuntime, func(), error)
		if dataServer != nil {
			providerRuntimeFactory = dataServer.AcquireProviderRuntime
		}
		adminServer, err := adminhttp.NewServer(adminhttp.ServerConfig{
			Store: store, DB: db, Bootstrap: bootstrapManager, Auth: authService, VKeys: vkeys, Ledgers: ledgerService,
			Vault: vaultStore, Snapshots: snaps, KillSwitch: killSwitch, Catalog: catalogRepo,
			Installer: installerService, ArchiveResolver: newRuntimeArchiveResolver(os.Getenv(installer.LocalMarketplaceArchiveRootEnv), registryStore, vaultOCIRegistryCredentialResolver{db: db, vault: vaultStore, cap: brokerUnsealCap}),
			Broker: blindBroker, OAuth: oauthService, ProviderRuntimeFactory: providerRuntimeFactory, Audit: auditLog, Version: version,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize Admin API: %w", err)
		}
		mux.Handle("/admin/v1/", adminServer)
		mux.Handle("/", webui.Handler())
	}
	return mux, cleanup, nil
}

func newServeArchiveResolver(root string) adminhttp.ExtensionArchiveResolver {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	resolver, err := installer.NewLocalMarketplaceArchiveResolver(root)
	if err != nil {
		return func(context.Context, string, string, string) (*installer.StageInstallRequest, func(), error) {
			return nil, nil, err
		}
	}
	return func(ctx context.Context, _ string, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error) {
		return resolver.Resolve(ctx, sourceURI, expectedSHA256)
	}
}

func newRuntimeArchiveResolver(root string, index registry.IndexStore, credentials registry.RegistryCredentialResolver) adminhttp.ExtensionArchiveResolver {
	local := newServeArchiveResolver(root)
	oci := registry.NewOCIArchiveResolver(index, credentials, egress.NewStdHostResolver())
	return func(ctx context.Context, orgID, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(sourceURI)), "oci://") {
			return oci.Resolve(ctx, orgID, sourceURI, expectedSHA256)
		}
		if local == nil {
			return nil, nil, installer.ErrMarketplaceArchiveResolverUnavailable
		}
		return local(ctx, orgID, sourceURI, expectedSHA256)
	}
}

func allowedOrigins(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || strings.TrimSpace(host) == "" {
		return nil
	}
	host = strings.Trim(host, "[]")
	return []string{"http://" + net.JoinHostPort(host, port)}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
