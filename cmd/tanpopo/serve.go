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
	"time"

	"github.com/spf13/cobra"

	adminhttp "github.com/nguywnben/tanpopo/internal/admin/http"
	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
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

func serve(ctx context.Context, role, listen string) error {
	handler, closeRuntime, err := buildRuntime(ctx, role, listen)
	if err != nil {
		return err
	}
	defer closeRuntime()
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

func buildRuntime(ctx context.Context, role, listen string) (http.Handler, func(), error) {
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
	go func() {
		defer close(watcherDone)
		snaps.StartWatcher(runtimeCtx, time.Second)
	}()
	var closeDataServer func()
	var dataServerReady func(context.Context) error
	cleanup := func() {
		if closeDataServer != nil {
			closeDataServer()
		}
		cancelRuntime()
		<-watcherDone
		closeKeys()
		closeStore()
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
	lockfile := envOr("TANPOPO_LOCK_FILE", filepath.Join(dataDir, "tanpopo.lock"))
	installerService, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB: db, Audit: auditLog, AuditEntryPoint: "admin_api", PluginsDir: pluginsDir, LockfilePath: lockfile,
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize extension installer: %w", err)
	}
	blindBroker, err := broker.NewBlindBroker(broker.Config{
		Resolver: catalogRepo, PolicyResolver: catalogRepo, Vault: vaultStore,
		UnsealCap: vault.MintUnsealCapabilityForBroker(), Audit: auditLog,
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
		adminServer, err := adminhttp.NewServer(adminhttp.ServerConfig{
			Store: store, DB: db, Bootstrap: bootstrapManager, Auth: authService, VKeys: vkeys,
			Vault: vaultStore, Snapshots: snaps, KillSwitch: killSwitch, Catalog: catalogRepo,
			Installer: installerService, Broker: blindBroker, Audit: auditLog, Version: version,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize Admin API: %w", err)
		}
		mux.Handle("/admin/v1/", adminServer)
		mux.Handle("/", webui.Handler())
	}
	if role == "all" || role == "data-plane" {
		dataServer, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots: snaps, Broker: blindBroker, KillSwitch: killSwitch, PluginsDir: pluginsDir, Audit: auditLog, IdempotencyDB: db,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize Data Plane: %w", err)
		}
		closeDataServer = func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = dataServer.Close(shutdownCtx)
		}
		dataServerReady = dataServer.CheckServingReady
		mux.Handle("/v1/", dataServer)
		mux.Handle("/metrics", dataServer.MetricsHandler())
	}
	return mux, cleanup, nil
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
