// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	adminhttp "github.com/nguywnben/tanpopo/internal/admin/http"
	vkcluster "github.com/nguywnben/tanpopo/internal/cluster/valkey"
	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/builtin"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	lockfilepkg "github.com/nguywnben/tanpopo/internal/extension/lockfile"
	extport "github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
	"github.com/nguywnben/tanpopo/internal/extension/sidecar"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/ingress/a2a"
	"github.com/nguywnben/tanpopo/internal/ingress/cache"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/approval"
	mcphub "github.com/nguywnben/tanpopo/internal/ingress/mcp/hub"
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/ratelimit"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/oauth"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	telemetry "github.com/nguywnben/tanpopo/internal/telemetry/otel"
	"github.com/nguywnben/tanpopo/internal/telemetry/privacy"
	"github.com/nguywnben/tanpopo/internal/webhook"
	"github.com/nguywnben/tanpopo/internal/webui"
)

type serveRuntimeConfig struct {
	OfflineMode         bool
	MultiOrgDisabled    bool
	HasOfflineOverride  bool
	HasMultiOrgOverride bool
	MetricsListen       string
	HasMetricsListen    bool
	OTLPEndpoint        string
	OTLPAllowPrivateIP  bool
	HasOTLPAllowPrivate bool
	OnListen            func(mainAddr, metricsAddr string)
}

func newServeCommand(loc i18n.Locale) *cobra.Command {
	role := "all"
	listen := envOr("TANPOPO_LISTEN_ADDR", "127.0.0.1:8080")
	metricsListen := envOr("TANPOPO_METRICS_LISTEN", "127.0.0.1:9090")
	otlpEndpoint := envOr("TANPOPO_OTLP_ENDPOINT", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	otlpAllowPrivateIP := parseEnvBool("TANPOPO_OTLP_ALLOW_PRIVATE_IP", false)
	offlineFlag := false
	multiOrgFlag := true
	cmd := &cobra.Command{
		Use:   "serve",
		Short: i18n.T(loc, "cli.serve.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if role != "all" && role != "data-plane" && role != "control-plane" {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: i18n.T(loc, "cli.serve.err.invalid_role", "role", fmt.Sprintf("%q", role))}
			}
			if _, err := drainTimeoutFromEnv(os.LookupEnv); err != nil {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: i18n.T(loc, "cli.serve.err.invalid_drain"), Cause: err}
			}
			if _, err := preStopWaitFromEnv(os.LookupEnv); err != nil {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: i18n.T(loc, "cli.serve.err.invalid_drain"), Cause: err}
			}
			cfg := serveRuntimeConfig{
				OfflineMode:         offlineFlag,
				MultiOrgDisabled:    !multiOrgFlag,
				HasOfflineOverride:  cmd.Flags().Changed("offline"),
				HasMultiOrgOverride: cmd.Flags().Changed("multi-org"),
				MetricsListen:       metricsListen,
				HasMetricsListen:    cmd.Flags().Changed("metrics-listen"),
				OTLPEndpoint:        otlpEndpoint,
				OTLPAllowPrivateIP:  otlpAllowPrivateIP,
				HasOTLPAllowPrivate: cmd.Flags().Changed("otlp-allow-private-ip"),
			}
			if err := serveWithConfig(cmd.Context(), role, listen, cfg); err != nil {
				return &testharness.ExitError{Code: testharness.ExitInternal, Message: i18n.T(loc, "cli.serve.err.failed"), Cause: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", role, i18n.T(loc, "cli.serve.flag.role"))
	cmd.Flags().StringVar(&listen, "listen", listen, i18n.T(loc, "cli.serve.flag.listen"))
	cmd.Flags().StringVar(&metricsListen, "metrics-listen", metricsListen, i18n.T(loc, "cli.serve.flag.metrics_listen"))
	cmd.Flags().StringVar(&otlpEndpoint, "otlp-endpoint", otlpEndpoint, i18n.T(loc, "cli.serve.flag.otlp_endpoint"))
	cmd.Flags().BoolVar(&otlpAllowPrivateIP, "otlp-allow-private-ip", otlpAllowPrivateIP, i18n.T(loc, "cli.serve.flag.otlp_allow_private_ip"))
	cmd.Flags().BoolVar(&offlineFlag, "offline", false, i18n.T(loc, "cli.serve.flag.offline"))
	cmd.Flags().BoolVar(&multiOrgFlag, "multi-org", true, i18n.T(loc, "cli.serve.flag.multi_org"))
	return cmd
}

func serve(ctx context.Context, role, listen string) error {
	return serveWithConfig(ctx, role, listen, serveRuntimeConfig{})
}

func serveWithConfig(ctx context.Context, role, listen string, cfg serveRuntimeConfig) (serveErr error) {
	drainTimeout, err := drainTimeoutFromEnv(os.LookupEnv)
	if err != nil {
		return err
	}
	preStopWait, err := preStopWaitFromEnv(os.LookupEnv)
	if err != nil {
		return err
	}
	var draining atomic.Bool
	handler, metricsHandler, closeRuntime, err := buildRuntimeDetailed(ctx, role, listen, nil, &draining, cfg)
	if err != nil {
		return err
	}
	defer func() { serveErr = errors.Join(serveErr, closeRuntime()) }()
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listen, err)
	}
	metricsListen := strings.TrimSpace(cfg.MetricsListen)
	if !cfg.HasMetricsListen {
		metricsListen = envOr("TANPOPO_METRICS_LISTEN", "127.0.0.1:9090")
	}
	var (
		metricsListener net.Listener
		metricsServer   *http.Server
		metricsAddr     string
	)
	if metricsHandler != nil && metricsListen != "" && !strings.EqualFold(metricsListen, "disabled") && !strings.EqualFold(metricsListen, "off") && !strings.EqualFold(metricsListen, "none") && (metricsListen != listen || strings.HasSuffix(metricsListen, ":0")) {
		metricsListener, err = net.Listen("tcp", metricsListen)
		if err != nil {
			_ = listener.Close()
			return fmt.Errorf("listen internal metrics on %s: %w", metricsListen, err)
		}
		metricsAddr = metricsListener.Addr().String()
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", metricsHandler)
		metricsServer = &http.Server{
			Handler:           metricsMux,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       90 * time.Second,
		}
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	if cfg.OnListen != nil {
		cfg.OnListen(listener.Addr().String(), metricsAddr)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	if metricsServer != nil {
		go func() {
			if mErr := metricsServer.Serve(metricsListener); mErr != nil && !errors.Is(mErr, http.ErrServerClosed) {
				slog.Warn("internal metrics listener exited", "addr", metricsAddr, "error", mErr)
			}
		}()
	}
	select {
	case <-ctx.Done():
		// Mark /readyz as 503 immediately on shutdown signal while draining in-flight SSE streams (OPS-AC-06).
		draining.Store(true)
		if preStopWait > 0 {
			timer := time.NewTimer(preStopWait)
			<-timer.C
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
		defer cancel()
		var shutdownErr error
		if metricsServer != nil {
			if mErr := metricsServer.Shutdown(shutdownCtx); mErr != nil {
				_ = metricsServer.Close()
				shutdownErr = errors.Join(shutdownErr, mErr)
			}
		}
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return errors.Join(shutdownErr, err)
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return shutdownErr
		}
		return errors.Join(shutdownErr, err)
	case err := <-done:
		if metricsServer != nil {
			_ = metricsServer.Close()
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

const defaultDrainTimeout = 90 * time.Second
const maxDrainTimeout = 300 * time.Second
const maxPreStopWait = 30 * time.Second

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

func preStopWaitFromEnv(lookup func(string) (string, bool)) (time.Duration, error) {
	raw, ok := lookup("TANPOPO_PRESTOP_WAIT")
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	wait, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || wait < 0 || wait > maxPreStopWait {
		return 0, fmt.Errorf("TANPOPO_PRESTOP_WAIT must be between 0s and %s", maxPreStopWait)
	}
	return wait, nil
}

func buildRuntime(ctx context.Context, role, listen string) (http.Handler, func() error, error) {
	return buildRuntimeWithBeforeSQLiteClose(ctx, role, listen, nil)
}

// buildRuntimeWithBeforeSQLiteClose exposes the last quiescent SQLite state to
// shutdown tests. Production callers use buildRuntime and install no observer.
func buildRuntimeWithBeforeSQLiteClose(ctx context.Context, role, listen string, beforeSQLiteClose func(*sql.DB)) (http.Handler, func() error, error) {
	return buildRuntimeWithConfig(ctx, role, listen, beforeSQLiteClose, nil, serveRuntimeConfig{})
}

type runtimeStore interface {
	port.Store
	RawDB() *sql.DB
}

func buildRuntimeInternal(ctx context.Context, role, listen string, beforeSQLiteClose func(*sql.DB), draining *atomic.Bool) (http.Handler, func() error, error) {
	return buildRuntimeWithConfig(ctx, role, listen, beforeSQLiteClose, draining, serveRuntimeConfig{})
}

func buildRuntimeWithConfig(ctx context.Context, role, listen string, beforeSQLiteClose func(*sql.DB), draining *atomic.Bool, runtimeCfg serveRuntimeConfig) (http.Handler, func() error, error) {
	handler, _, closeRuntime, err := buildRuntimeDetailed(ctx, role, listen, beforeSQLiteClose, draining, runtimeCfg)
	return handler, closeRuntime, err
}

func buildRuntimeDetailed(ctx context.Context, role, listen string, beforeSQLiteClose func(*sql.DB), draining *atomic.Bool, runtimeCfg serveRuntimeConfig) (http.Handler, http.Handler, func() error, error) {
	if role != "all" && role != "data-plane" && role != "control-plane" {
		return nil, nil, nil, fmt.Errorf("invalid service role %q", role)
	}
	dataDir := envOr("TANPOPO_DATA_DIR", "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, nil, nil, fmt.Errorf("secure data directory: %w", err)
	}
	pluginsDir := envOr("TANPOPO_PLUGINS_DIR", filepath.Join(dataDir, "plugins"))
	if err := os.MkdirAll(pluginsDir, 0o700); err != nil {
		return nil, nil, nil, fmt.Errorf("create plugins directory: %w", err)
	}
	pgDSN := strings.TrimSpace(os.Getenv("TANPOPO_PG_DSN"))
	storageEngine := strings.ToLower(strings.TrimSpace(os.Getenv("TANPOPO_STORAGE_ENGINE")))
	switch storageEngine {
	case "", "sqlite", "postgres":
	default:
		return nil, nil, nil, fmt.Errorf("unsupported TANPOPO_STORAGE_ENGINE %q: expected sqlite or postgres", storageEngine)
	}
	var (
		store         runtimeStore
		isPostgres    bool
		dialect       = port.DialectSQLite
		sqliteDBPath  string
		postShredHook func(context.Context) error
	)
	if storageEngine == "postgres" || (storageEngine == "" && pgDSN != "") {
		if pgDSN == "" {
			return nil, nil, nil, errors.New("TANPOPO_PG_DSN is required when TANPOPO_STORAGE_ENGINE=postgres")
		}
		pgStore, openErr := postgres.Open(ctx, postgres.Options{DSN: pgDSN})
		if openErr != nil {
			return nil, nil, nil, fmt.Errorf("open PostgreSQL store: %w", openErr)
		}
		store = pgStore
		isPostgres = true
		dialect = port.DialectPostgres
		postShredHook = pgStore.Ping
	} else {
		sqliteDBPath = envOr("TANPOPO_DB_PATH", filepath.Join(dataDir, "tanpopo.db"))
		sqliteStore, openErr := sqlite.Open(ctx, sqlite.Config{DBPath: sqliteDBPath})
		if openErr != nil {
			return nil, nil, nil, fmt.Errorf("open local SQLite store: %w", openErr)
		}
		store = sqliteStore
		postShredHook = sqliteStore.CheckpointWAL
	}
	closeStore := func() { _ = store.Close() }
	db := store.RawDB()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		closeStore()
		return nil, nil, nil, fmt.Errorf("initialize audit logger: %w", err)
	}
	if sinkSetter, ok := store.(interface{ SetAuditSink(port.AuditSink) error }); ok {
		if setErr := sinkSetter.SetAuditSink(auditLog); setErr != nil {
			closeStore()
			return nil, nil, nil, fmt.Errorf("wire audit sink to store: %w", setErr)
		}
	}
	var ledgerService *ledger.Service
	activeKEKID := envOr("TANPOPO_MASTER_KEY_ID", "kek_v1")
	kek, err := vault.LoadStaticKEKFromEnv(activeKEKID, os.LookupEnv)
	if err != nil {
		closeStore()
		return nil, nil, nil, fmt.Errorf("load Master KEK: %w", err)
	}
	closeKeys := func() { kek.Close() }
	for idx, rawPrev := range parseEnvCSV("TANPOPO_MASTER_KEY_PREVIOUS") {
		prevKey, parseErr := vault.ParseMasterKeyString(rawPrev)
		if parseErr != nil {
			closeKeys()
			closeStore()
			return nil, nil, nil, fmt.Errorf("parse TANPOPO_MASTER_KEY_PREVIOUS[%d]: %w", idx, parseErr)
		}
		regErr := kek.RegisterKey(fmt.Sprintf("kek_prev_%d", idx), prevKey, false)
		clear(prevKey)
		if regErr != nil {
			closeKeys()
			closeStore()
			return nil, nil, nil, fmt.Errorf("register previous Master KEK[%d]: %w", idx, regErr)
		}
	}
	vaultStore, err := vault.NewStore(vault.Config{DB: db, KEK: kek, PostShredHook: postShredHook})
	if err != nil {
		closeKeys()
		closeStore()
		return nil, nil, nil, fmt.Errorf("initialize vault: %w", err)
	}
	builtinCatalog, err := builtin.NewCatalog()
	if err != nil {
		closeKeys()
		closeStore()
		return nil, nil, nil, fmt.Errorf("initialize built-in official catalog: %w", err)
	}
	releaseRootPubs, err := builtin.LoadReleaseRootPublicKeys(os.LookupEnv, dataDir)
	if err != nil {
		closeKeys()
		closeStore()
		return nil, nil, nil, fmt.Errorf("load extension release root public keys: %w", err)
	}
	if err := builtinCatalog.LoadOperatorReleaseSignatures(os.LookupEnv, dataDir); err != nil {
		closeKeys()
		closeStore()
		return nil, nil, nil, fmt.Errorf("load extension release signatures: %w", err)
	}
	trustedKeyring := trust.TrustedKeyring{RootPublicKeys: releaseRootPubs}
	var builtinSigner builtin.SignerFunc
	snaps, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db, Keyring: trustedKeyring})
	if err != nil {
		closeKeys()
		closeStore()
		return nil, nil, nil, fmt.Errorf("initialize configuration snapshot: %w", err)
	}

	var (
		vkClient       *vkcluster.Client
		vkRateLimiter  ratelimit.RateLimiterPort
		vkBudgetCoord  *vkcluster.BudgetCoordinator
		clusterSync    *snapshot.ClusterSync
		clusterDone    chan struct{}
		budgetLoopDone chan struct{}
	)
	if rawValkeyAddrs := strings.TrimSpace(os.Getenv("TANPOPO_VALKEY_ADDRS")); rawValkeyAddrs != "" {
		requireTLS := strings.EqualFold(strings.TrimSpace(os.Getenv("TANPOPO_VALKEY_REQUIRE_TLS")), "1") ||
			strings.EqualFold(strings.TrimSpace(os.Getenv("TANPOPO_VALKEY_REQUIRE_TLS")), "true")
		vkClient, err = vkcluster.Dial(vkcluster.Config{
			Addrs:      strings.Split(rawValkeyAddrs, ","),
			Username:   strings.TrimSpace(os.Getenv("TANPOPO_VALKEY_USERNAME")),
			Password:   os.Getenv("TANPOPO_VALKEY_PASSWORD"),
			RequireTLS: requireTLS,
		})
		if err != nil {
			vaultStore.Close()
			closeKeys()
			closeStore()
			return nil, nil, nil, fmt.Errorf("dial Valkey Cluster: %w", err)
		}
		replicas := int64(1)
		if rawReps := strings.TrimSpace(os.Getenv("TANPOPO_VALKEY_REPLICAS")); rawReps != "" {
			if parsed, perr := strconv.ParseInt(rawReps, 10, 64); perr == nil && parsed > 0 {
				replicas = parsed
			}
		}
		vkRateLimiter, err = vkcluster.NewRateLimiter(vkcluster.RateLimiterConfig{
			Client:                vkClient,
			EstimatedReplicaCount: replicas,
		})
		if err != nil {
			vkClient.Close()
			vaultStore.Close()
			closeKeys()
			closeStore()
			return nil, nil, nil, fmt.Errorf("initialize Valkey rate limiter: %w", err)
		}
	}

	// Decouple background runtimeCtx from listen ctx so SIGTERM drains active SSE streams cleanly
	// while /readyz immediately returns 503 via draining.Store(true).
	runtimeCtx, cancelRuntime := context.WithCancel(context.Background())
	watcherDone := make(chan struct{})
	var webhookDone chan struct{}
	var vkeyReaperDone chan struct{}
	var closeWebhookDispatcher func() error
	var webhookDispatcher *webhook.Dispatcher
	var approvalMaintenanceDone chan struct{}
	var marketplaceDone <-chan struct{}
	var clusterNodesDone chan struct{}
	nodeID := resolveRuntimeNodeID()
	if isPostgres || vkClient != nil {
		var pubsubBus snapshot.PubSubBus
		if vkClient != nil {
			pubsubBus = vkClient
		}
		disableValkeySub := strings.EqualFold(strings.TrimSpace(os.Getenv("TANPOPO_DISABLE_VALKEY_PUBSUB")), "1") ||
			strings.EqualFold(strings.TrimSpace(os.Getenv("TANPOPO_DISABLE_VALKEY_PUBSUB")), "true")
		clusterSync, err = snapshot.NewClusterSync(snapshot.ClusterSyncConfig{
			Manager:             snaps,
			DB:                  db,
			PGListenDSN:         pgDSN,
			Bus:                 pubsubBus,
			DisableValkeyPubSub: disableValkeySub,
		})
		if err != nil {
			if vkClient != nil {
				vkClient.Close()
			}
			vaultStore.Close()
			closeKeys()
			closeStore()
			cancelRuntime()
			return nil, nil, nil, fmt.Errorf("initialize cluster snapshot sync: %w", err)
		}
		clusterDone = make(chan struct{})
		go func() {
			defer close(clusterDone)
			clusterSync.Start(runtimeCtx)
		}()
	} else {
		go func() {
			defer close(watcherDone)
			snaps.StartWatcher(runtimeCtx, time.Second)
		}()
	}
	if clusterSync != nil {
		close(watcherDone)
	}
	publishChangeFn := func(pubCtx context.Context, ev snapshot.ChangeEvent) {
		if clusterSync != nil {
			_ = clusterSync.PublishChange(pubCtx, ev)
		}
	}
	var closeDataServer func() error
	var dataServerReady func(context.Context) error
	var telemetryPipeline *telemetry.Pipeline
	var cleanupOnce sync.Once
	var shutdownErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			lastActiveDBStage := ""
			lastActiveDBConnections := 0
			recordActiveDBConnections := func(stage string) {
				if inUse := db.Stats().InUse; inUse > 0 {
					lastActiveDBStage = stage
					lastActiveDBConnections = inUse
				}
			}
			recordActiveDBConnections("shutdown start")
			if closeDataServer != nil {
				shutdownErr = errors.Join(shutdownErr, closeDataServer())
				recordActiveDBConnections("after data server close")
			}
			if telemetryPipeline != nil {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				shutdownErr = errors.Join(shutdownErr, telemetryPipeline.Shutdown(shutdownCtx))
				cancel()
				recordActiveDBConnections("after telemetry shutdown")
			}
			if webhookDispatcher != nil {
				webhookDispatcher.StopClaiming()
			}
			cancelRuntime()
			if webhookDone != nil {
				<-webhookDone
			}
			if closeWebhookDispatcher != nil {
				shutdownErr = errors.Join(shutdownErr, closeWebhookDispatcher())
			}
			if approvalMaintenanceDone != nil {
				<-approvalMaintenanceDone
			}
			if vkeyReaperDone != nil {
				<-vkeyReaperDone
			}
			if budgetLoopDone != nil {
				<-budgetLoopDone
			}
			if clusterDone != nil {
				<-clusterDone
			}
			if clusterNodesDone != nil {
				<-clusterNodesDone
				delCtx, delCancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, _ = db.ExecContext(delCtx, port.Rebind(dialect, `DELETE FROM cluster_nodes WHERE node_id = ?`), nodeID)
				delCancel()
			}
			<-watcherDone
			recordActiveDBConnections("after snapshot watcher shutdown")
			if marketplaceDone != nil {
				<-marketplaceDone
				recordActiveDBConnections("after marketplace watcher shutdown")
			}
			if vkClient != nil {
				vkClient.Close()
			}
			vaultStore.Close()
			closeKeys()
			recordActiveDBConnections("before storage close")
			if beforeSQLiteClose != nil {
				beforeSQLiteClose(db)
			}
			if err := store.Close(); err != nil {
				if lastActiveDBStage != "" {
					shutdownErr = errors.Join(shutdownErr, fmt.Errorf("storage: last observed %d active connection(s) at %s", lastActiveDBConnections, lastActiveDBStage))
				}
				shutdownErr = errors.Join(shutdownErr, err)
			}
		})
		return shutdownErr
	}
	catalogRepo, err := catalog.NewRepository(catalog.Config{DB: db, Snapshots: snaps, Keyring: trustedKeyring, Audit: auditLog})
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("initialize provider catalog: %w", err)
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
	webhookDispatcher, err = webhook.New(webhook.Config{
		DB: db, Postgres: isPostgres, Resolver: egress.NewStdHostResolver(),
		Unseal: func(ctx context.Context, orgID, credentialID string) ([]byte, func(), error) {
			secret, unsealErr := vaultStore.UnsealWithCapability(ctx, brokerUnsealCap, orgID, credentialID)
			if unsealErr != nil {
				return nil, nil, unsealErr
			}
			if secret.SecretKind != vault.SecretKindWebhookHMAC {
				secret.Zeroize()
				return nil, nil, errors.New("webhook signing credential has an invalid kind")
			}
			return secret.Plaintext, secret.Zeroize, nil
		},
	})
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("initialize webhook dispatcher: %w", err)
	}
	closeWebhookDispatcher = webhookDispatcher.Close
	killSwitch, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{
		DB: db, Audit: auditLog, Streams: killswitch.NewStreamRegistry(),
		RecordWebhookTx: func(ctx context.Context, tx *sql.Tx, orgID, projectID, eventKey, eventType string, payload []byte) error {
			if orgID == "" {
				var body struct {
					TargetID string `json:"target_id"`
				}
				if err := json.Unmarshal(payload, &body); err != nil || body.TargetID == "" {
					return fmt.Errorf("resolve extension kill-switch target")
				}
				return recordExtensionWebhookOutbox(ctx, tx, dialect, webhookDispatcher, body.TargetID, "kill_switch:"+eventKey, eventType, payload)
			}
			return webhookDispatcher.RecordEventTx(ctx, tx, webhook.Event{
				ID: webhook.StableEventID("kill_switch:" + eventKey), OrgID: orgID,
				ProjectID: projectID, Type: eventType, Payload: payload,
			})
		},
	})
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("initialize kill switches: %w", err)
	}
	snaps.RegisterKillSwitchSync(killSwitch.RefreshFromDB)
	ledgerService, err = ledger.NewService(ledger.Config{
		DB: db, Audit: auditLog,
		AlertOutbox: func(ctx context.Context, tx *sql.Tx, ev ledger.AlertEvent) error {
			var scopeType, scopeID string
			var projectID string
			if err := tx.QueryRowContext(ctx, port.Rebind(dialect, `SELECT scope_type,scope_id FROM budget_ledgers WHERE org_id=? AND id=?`), ev.OrgID, ev.LedgerID).Scan(&scopeType, &scopeID); err != nil {
				return err
			}
			switch scopeType {
			case "PROJECT":
				projectID = scopeID
			case "ENV":
				if err := tx.QueryRowContext(ctx, port.Rebind(dialect, `SELECT project_id FROM environments WHERE org_id=? AND id=?`), ev.OrgID, scopeID).Scan(&projectID); err != nil {
					return err
				}
			case "VIRTUAL_KEY":
				if err := tx.QueryRowContext(ctx, port.Rebind(dialect, `SELECT project_id FROM virtual_keys WHERE org_id=? AND id=?`), ev.OrgID, scopeID).Scan(&projectID); err != nil {
					return err
				}
			}
			payload, marshalErr := json.Marshal(map[string]any{
				"code": ev.Code, "ledger_id": ev.LedgerID, "scope_type": ev.ScopeType,
				"scope_id": ev.ScopeID, "threshold_pct": ev.ThresholdPct,
				"utilization_pct": ev.UtilizationPct, "request_id": ev.RequestID,
				"occurred_at_ms": ev.OccurredAtMs,
			})
			if marshalErr != nil {
				return marshalErr
			}
			if scopeType != "ORG" && projectID == "" {
				return fmt.Errorf("resolve project for budget alert scope %s", scopeType)
			}
			return webhookDispatcher.RecordEventTx(ctx, tx, webhook.Event{ID: webhook.StableEventID("budget:" + ev.LedgerID + ":" + ev.Code), OrgID: ev.OrgID, ProjectID: projectID, Type: "budget.threshold", Payload: payload})
		},
	})
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("initialize usage ledger: %w", err)
	}
	if vkClient != nil && (role == "all" || role == "data-plane") {
		vkBudgetCoord, err = vkcluster.NewBudgetCoordinator(vkcluster.BudgetCoordinatorConfig{
			Client: vkClient,
			DB:     db,
			Ledger: ledgerService,
			NodeID: nodeID,
		})
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize Valkey budget coordinator: %w", err)
		}
		budgetLoopDone = make(chan struct{})
		go func() {
			defer close(budgetLoopDone)
			vkBudgetCoord.StartCheckpointLoop(runtimeCtx)
		}()
	}
	webhookDone = make(chan struct{})
	go func() {
		defer close(webhookDone)
		_ = webhookDispatcher.Run(runtimeCtx)
	}()
	var oauthService *oauth.Service
	if redirectURI != "" {
		oauthService, err = oauth.NewService(oauth.ServiceConfig{DB: db, Vault: vaultStore, UnsealCapability: brokerUnsealCap, Audit: auditLog, HostResolver: egress.NewStdHostResolver(), RedirectURI: redirectURI, AllowLoopbackHTTP: allowHermeticHTTP})
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize OAuth coordinator: %w", err)
		}
	}
	lockfile := envOr("TANPOPO_LOCK_FILE", filepath.Join(dataDir, "tanpopo.lock"))
	installerService, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB: db, Audit: auditLog, AuditEntryPoint: "admin_api", PluginsDir: pluginsDir, LockfilePath: lockfile,
		Keyring: trustedKeyring,
		RecordQuarantineEventTx: func(ctx context.Context, tx *sql.Tx, installationID, packageID, state string) error {
			payload, err := json.Marshal(map[string]any{"installation_id": installationID, "package_id": packageID, "state": state})
			if err != nil {
				return err
			}
			return recordExtensionWebhookOutbox(ctx, tx, dialect, webhookDispatcher, installationID, "extension.quarantined:"+installationID, "extension.quarantined", payload)
		},
	})
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("initialize extension installer: %w", err)
	}
	registryStore, err := registry.NewSQLStore(db, time.Now)
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("initialize marketplace registry store: %w", err)
	}
	marketplaceDone = startMarketplaceRuntime(runtimeCtx, registryStore, installerService, snaps, auditLog)
	brokerResolver := planeBrokerResolver{provider: catalogRepo, mcp: mcphub.BrokerResolver{DB: db}, a2a: a2a.BrokerResolver{DB: db}}
	blindBroker, err := broker.NewBlindBroker(broker.Config{
		Resolver: brokerResolver, PolicyResolver: brokerResolver, Vault: vaultStore,
		UnsealCap: brokerUnsealCap, Audit: auditLog, OAuthTokens: oauthService,
	})
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("initialize credential broker: %w", err)
	}
	a2aEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("TANPOPO_A2A_ENABLED")), "true")
	sidecarEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("TANPOPO_RUNTIME_SIDECAR_ENABLED")), "true")
	sidecarSocketDir := strings.TrimSpace(os.Getenv("TANPOPO_SIDECAR_SOCKET_DIR"))
	var sidecarFactory func(context.Context, *snapshot.ProviderAdapterView, installer.PermissionSet) (extport.ProviderRuntime, io.Closer, error)
	if sidecarEnabled {
		sidecarCA, caErr := sidecar.NewEphemeralCA(sidecar.DefaultCACertTTL, nil)
		if caErr != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize sidecar ephemeral CA: %w", caErr)
		}
		sidecarClientCert, certErr := sidecar.NewRotatingCertSource(sidecarCA, "spiffe://"+sidecar.SPIFFETenantDomain+"/core/gateway", false, sidecar.DefaultLeafCertTTL)
		if certErr != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize sidecar client certificate: %w", certErr)
		}
		sidecarFactory = newProductionSidecarFactory(sidecarSocketDir, sidecarCA, sidecarClientCert, auditLog)
	}
	mux := http.NewServeMux()
	telemetryPipeline, err = telemetry.NewPipeline(buildTelemetryPipelineConfig(runtimeCfg, version, db, vaultStore))
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("initialize telemetry pipeline: %w", err)
	}
	mux.Handle("/metrics", telemetryPipeline.Metrics)
	var a2aCoordinator *a2a.TaskCoordinator
	if role == "all" || role == "data-plane" {
		mcpServer, serverErr := mcphub.NewServer(mcphub.Config{
			DB: db, Snapshots: snaps, Broker: blindBroker, KillSwitch: killSwitch, Audit: auditLog,
			Upstream: &mcphub.BrokerClient{Broker: blindBroker},
			RecordApprovalPendingTx: func(ctx context.Context, tx *sql.Tx, requestID string, binding approval.Binding) error {
				payload, marshalErr := json.Marshal(map[string]string{"approval_request_id": requestID})
				if marshalErr != nil {
					return marshalErr
				}
				return webhookDispatcher.RecordEventTx(ctx, tx, webhook.Event{ID: webhook.StableEventID("mcp.approval.pending:" + requestID), OrgID: binding.OrgID, ProjectID: binding.ProjectID,
					Type: "mcp.approval.pending", Payload: payload})
			},
		})
		if serverErr != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize MCP Governance Hub: %w", serverErr)
		}
		approvalMaintenanceDone = make(chan struct{})
		go func() {
			defer close(approvalMaintenanceDone)
			mcpServer.RunApprovalMaintenance(runtimeCtx)
		}()
		mux.Handle("/mcp", mcpServer)

		var a2aErr error
		a2aCoordinator, a2aErr = a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
			Enabled:    a2aEnabled,
			DB:         db,
			Snapshots:  snaps,
			Broker:     blindBroker,
			Ledger:     ledgerService,
			Audit:      auditLog,
			KillSwitch: killSwitch,
		})
		if a2aErr != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize A2A Task Coordinator: %w", a2aErr)
		}
		a2aHandler := a2aCoordinator.Handler()
		if role != "data-plane" {
			consoleHandler := webui.Handler()
			mux.HandleFunc("/a2a/", func(w http.ResponseWriter, r *http.Request) {
				cleanPath := path.Clean("/" + r.URL.Path)
				if cleanPath == "/a2a/agents" && (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
					strings.TrimSpace(r.Header.Get("Authorization")) == "" &&
					strings.TrimSpace(r.Header.Get("X-Tanpopo-Virtual-Key")) == "" &&
					strings.TrimSpace(r.Header.Get("x-api-key")) == "" &&
					strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
					consoleHandler.ServeHTTP(w, r)
					return
				}
				a2aHandler.ServeHTTP(w, r)
			})
		} else {
			mux.Handle("/a2a/", a2aHandler)
		}
		mux.Handle("/.well-known/agent.json", a2aHandler)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		isDraining := draining != nil && draining.Load()
		if isDraining || ctx.Err() != nil || store.Ping(checkCtx) != nil || vaultStore.CheckServingReady(checkCtx) != nil || snaps.CheckServingReady() != nil || (clusterSync != nil && clusterSync.CheckServingReady() != nil) || (dataServerReady != nil && dataServerReady(checkCtx) != nil) {
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
		responseCache, err := cache.NewExactCache(cache.Config{DB: db, Vault: vaultStore})
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize response cache: %w", err)
		}
		var maxNodeStreams int64
		if rawMax := strings.TrimSpace(os.Getenv("TANPOPO_MAX_CONCURRENT_STREAMS")); rawMax != "" {
			parsedMax, parseErr := strconv.ParseInt(rawMax, 10, 64)
			if parseErr != nil || parsedMax < 0 {
				cleanup()
				return nil, nil, nil, fmt.Errorf("invalid TANPOPO_MAX_CONCURRENT_STREAMS %q", rawMax)
			}
			maxNodeStreams = parsedMax
		}
		dataServer, err = ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots: snaps, Broker: blindBroker, KillSwitch: killSwitch, PluginsDir: pluginsDir, Audit: auditLog, IdempotencyDB: db,
			RateLimiter:              vkRateLimiter,
			BudgetLedger:             ledgerService,
			Telemetry:                telemetryPipeline,
			ResponseCache:            responseCache,
			MaxNodeConcurrentStreams: maxNodeStreams,
			SidecarEnabled:           sidecarEnabled,
			SidecarSocketDir:         sidecarSocketDir,
			SidecarRuntimeFactory:    sidecarFactory,
		})
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize Data Plane: %w", err)
		}
		closeDataServer = func() error {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return dataServer.Close(shutdownCtx)
		}
		dataServerReady = dataServer.CheckServingReady
		mux.Handle("/v1/", dataServer)
		mux.Handle("/native", dataServer)
		mux.Handle("/native/", dataServer)
	}
	if telemetryPipeline != nil && telemetryPipeline.Metrics != nil {
		snapSource := "db_poll"
		if clusterSync != nil {
			if vkClient != nil && !parseEnvBool("TANPOPO_DISABLE_VALKEY_PUBSUB", false) {
				snapSource = "valkey_pubsub"
			} else if isPostgres {
				snapSource = "pg_notify"
			}
		}
		telemetryPipeline.Metrics.SetConfigSnapshotAgeProvider(func() (string, time.Duration, bool) {
			age, ok := snaps.SnapshotAge()
			return snapSource, age, ok
		})
	}
	if role == "all" || role == "control-plane" {
		bootstrapManager, err := bootstrap.NewManager(bootstrap.ManagerConfig{
			DB: db, TokenPath: envOr("TANPOPO_BOOTSTRAP_TOKEN_FILE", filepath.Join(dataDir, "bootstrap.token")), AuditSink: auditLog,
		})
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize bootstrap service: %w", err)
		}
		if _, err := bootstrapManager.EnsureToken(ctx); err != nil && !errors.Is(err, bootstrap.ErrBootstrapAlreadyCompleted) {
			cleanup()
			return nil, nil, nil, fmt.Errorf("ensure bootstrap token: %w", err)
		}
		authService, err := auth.NewLocalService(auth.ServiceConfig{DB: db, AuditSink: auditLog, AllowedOrigins: allowedOrigins(listen)})
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize local authentication: %w", err)
		}
		vkeys, err := vkey.NewService(vkey.ServiceConfig{
			DB: db, Snapshots: snaps, AuditSink: auditLog, OnChange: publishChangeFn,
		})
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize virtual keys: %w", err)
		}
		vkeyReapInterval := 2 * time.Second
		if rawInterval := strings.TrimSpace(os.Getenv("TANPOPO_VKEY_ROTATION_REAP_INTERVAL")); rawInterval != "" {
			if parsedInterval, pErr := time.ParseDuration(rawInterval); pErr == nil && parsedInterval > 0 {
				vkeyReapInterval = parsedInterval
			}
		}
		vkeyReaperDone = make(chan struct{})
		go func() {
			defer close(vkeyReaperDone)
			ticker := time.NewTicker(vkeyReapInterval)
			defer ticker.Stop()
			for {
				select {
				case <-runtimeCtx.Done():
					return
				case <-ticker.C:
					if _, rErr := vkeys.FinalizeExpiredRotations(runtimeCtx); rErr != nil && !errors.Is(rErr, context.Canceled) {
						slog.Warn("virtual key expired rotation sweep failed", "error", rErr)
					}
				}
			}
		}()
		var providerRuntimeFactory func(context.Context, string, string) (extport.ProviderRuntime, func(), error)
		if dataServer != nil {
			providerRuntimeFactory = dataServer.AcquireProviderRuntime
		}
		offlineMode := parseEnvBool("TANPOPO_OFFLINE_MODE", false)
		if runtimeCfg.HasOfflineOverride {
			offlineMode = runtimeCfg.OfflineMode
		}
		multiOrgDisabled := false
		if rawMulti := strings.TrimSpace(os.Getenv("TANPOPO_MULTI_ORG_ENABLED")); rawMulti != "" {
			multiOrgDisabled = !parseEnvBool("TANPOPO_MULTI_ORG_ENABLED", true)
		}
		if runtimeCfg.HasMultiOrgOverride {
			multiOrgDisabled = runtimeCfg.MultiOrgDisabled
		}
		archiveRoot := strings.TrimSpace(os.Getenv(installer.LocalMarketplaceArchiveRootEnv))
		if archiveRoot == "" && offlineMode {
			airgapPackages := filepath.Join(dataDir, "airgap", "packages")
			if fi, statErr := os.Stat(airgapPackages); statErr == nil && fi.IsDir() {
				archiveRoot = airgapPackages
			}
		}
		builtinPackages := builtinCatalog.Packages()
		marketplaceCatalog := make([]adminhttp.MarketplaceCatalogEntry, 0, len(builtinPackages))
		for _, pkg := range builtinPackages {
			prov := builtinCatalog.EvaluateProvenance(pkg, releaseRootPubs)
			marketplaceCatalog = append(marketplaceCatalog, adminhttp.MarketplaceCatalogEntry{
				PackageID:             pkg.PackageID,
				CanonicalName:         pkg.DisplayName,
				Version:               pkg.Version,
				ExecutionTier:         pkg.ExecutionTier,
				OriginTrustTier:       prov.OriginTrustTier,
				SignatureVerified:     prov.SignatureVerified,
				EnterpriseSigned:      false,
				SigningKeyFingerprint: prov.SigningKeyFingerprint,
				SourceURI:             pkg.SourceURI,
				ExpectedSHA256:        pkg.ArchiveSHA256,
				Summary:               pkg.Summary,
				Capabilities:          pkg.Capabilities,
			})
		}
		configFileEnv := strings.TrimSpace(os.Getenv("TANPOPO_CONFIG"))
		if configFileEnv == "" {
			configFileEnv = strings.TrimSpace(os.Getenv("TANPOPO_CONFIG_FILE"))
		}
		configPath := configFileEnv
		if configPath == "" {
			configPath = filepath.Join(dataDir, "tanpopo.yaml")
		}
		if !isPostgres {
			if configFileEnv == "" {
				if _, lerr := os.Lstat(configPath); os.IsNotExist(lerr) {
					if f, oerr := os.OpenFile(configPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); oerr == nil {
						_, _ = f.WriteString("{}\n")
						_ = f.Close()
					}
				}
			}
			if strings.TrimSpace(os.Getenv("TANPOPO_LOCK_FILE")) == "" {
				if _, lerr := os.Lstat(lockfile); os.IsNotExist(lerr) {
					_ = lockfilepkg.WriteAtomic(lockfile, lockfilepkg.NewEmpty(time.Now().UTC()))
				}
			}
		}
		adminServer, err := adminhttp.NewServer(adminhttp.ServerConfig{
			Store: store, DB: db, Bootstrap: bootstrapManager, Auth: authService, VKeys: vkeys, Ledgers: ledgerService,
			Vault: vaultStore, Snapshots: snaps, KillSwitch: killSwitch, Catalog: catalogRepo,
			Installer: installerService, MarketplaceCatalog: marketplaceCatalog,
			LockfilePath: lockfile, ConfigPath: configPath,
			ArchiveResolver: newRuntimeArchiveResolverWithBuiltins(archiveRoot, offlineMode, registryStore, vaultOCIRegistryCredentialResolver{db: db, vault: vaultStore, cap: brokerUnsealCap}, builtinCatalog, builtinSigner),
			WebhookEnqueue: func(ctx context.Context, orgID, projectID, eventType string, payload json.RawMessage) error {
				_, enqueueErr := webhookDispatcher.Enqueue(ctx, webhook.Event{OrgID: orgID, ProjectID: projectID, Type: eventType, Payload: payload})
				return enqueueErr
			},
			OnConfigChange: publishChangeFn,
			Broker:         blindBroker, OAuth: oauthService, A2ACoordinator: a2aCoordinator, ProviderRuntimeFactory: providerRuntimeFactory, Audit: auditLog,
			OfflineMode: offlineMode, MultiOrgDisabled: multiOrgDisabled,
			TrustedProxyCIDRs: parseEnvCSV("TANPOPO_TRUSTED_PROXY_CIDRS"),
			Version:           version,
		})
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("initialize Admin API: %w", err)
		}
		if recErr := adminServer.ResumePendingOrgDeletions(ctx); recErr != nil {
			fmt.Fprintf(os.Stderr, "tanpopo: warning: pending organization erasure recovery incomplete: %v\n", recErr)
		}
		mux.Handle("/admin/v1/", withTelemetryRequestLogger(adminServer, telemetryPipeline, "admin_api", "admin_request"))
		mux.Handle("/", webui.Handler())
	}
	clusterNodesDone = startClusterNodeHeartbeat(runtimeCtx, db, dialect, nodeID, role, listen, isPostgres, sqliteDBPath, snaps, clusterSync, vaultStore, dataServerReady)
	var metricsHandler http.Handler
	if telemetryPipeline != nil && telemetryPipeline.Metrics != nil {
		metricsHandler = telemetryPipeline.Metrics
	}
	return mux, metricsHandler, cleanup, nil
}

func recordExtensionWebhookOutbox(ctx context.Context, tx *sql.Tx, dialect port.Dialect, dispatcher *webhook.Dispatcher, installationID, eventKey, eventType string, payload []byte) error {
	rows, err := tx.QueryContext(ctx, port.Rebind(dialect, `SELECT org_id FROM org_extension_policies WHERE package_id=(SELECT package_id FROM extension_installations WHERE id=?) AND enabled=`+port.BoolExpr(dialect, true)+`
		UNION SELECT pc.org_id FROM provider_connections pc JOIN provider_adapters pa ON pa.id=pc.adapter_id WHERE pa.installation_id=? ORDER BY 1`), installationID, installationID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var orgIDs []string
	for rows.Next() {
		var orgID string
		if err := rows.Scan(&orgID); err != nil {
			return err
		}
		orgIDs = append(orgIDs, orgID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, orgID := range orgIDs {
		if err := dispatcher.RecordEventTx(ctx, tx, webhook.Event{ID: webhook.StableEventID(eventKey + ":" + orgID), OrgID: orgID, Type: eventType, Payload: payload}); err != nil {
			return err
		}
	}
	return nil
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

func newRuntimeArchiveResolver(root string, offlineMode bool, index registry.IndexStore, credentials registry.RegistryCredentialResolver) adminhttp.ExtensionArchiveResolver {
	return newRuntimeArchiveResolverWithBuiltins(root, offlineMode, index, credentials, nil, nil)
}

func newRuntimeArchiveResolverWithBuiltins(root string, offlineMode bool, index registry.IndexStore, credentials registry.RegistryCredentialResolver, builtinCatalog *builtin.Catalog, builtinSigner builtin.SignerFunc) adminhttp.ExtensionArchiveResolver {
	local := newServeArchiveResolver(root)
	oci := registry.NewOCIArchiveResolver(index, credentials, egress.NewStdHostResolver())
	return func(ctx context.Context, orgID, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error) {
		trimmedURI := strings.TrimSpace(sourceURI)
		if strings.HasPrefix(strings.ToLower(trimmedURI), builtin.SourceScheme) {
			if builtinCatalog == nil {
				return nil, nil, installer.ErrMarketplaceArchiveResolverUnavailable
			}
			return builtinCatalog.ResolveStageRequest(trimmedURI, expectedSHA256, builtinSigner)
		}
		if strings.HasPrefix(strings.ToLower(trimmedURI), "oci://") {
			if offlineMode {
				return nil, nil, installer.ErrMarketplaceArchiveResolverUnavailable
			}
			return oci.Resolve(ctx, orgID, trimmedURI, expectedSHA256)
		}
		if local == nil {
			return nil, nil, installer.ErrMarketplaceArchiveResolverUnavailable
		}
		return local(ctx, orgID, trimmedURI, expectedSHA256)
	}
}

func parseEnvBool(name string, defaultVal bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}

func parseEnvCSV(name string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if item := strings.TrimSpace(part); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func validateOriginEntry(raw string) (string, bool) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" || trimmed == "*" || strings.EqualFold(trimmed, "null") {
		return "", false
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	host := u.Hostname()
	if host == "" || host == "*" || strings.Contains(host, "*") {
		return "", false
	}
	port := u.Port()
	normHost := strings.ToLower(host)
	hostPort := normHost
	if port != "" {
		hostPort = net.JoinHostPort(normHost, port)
	} else if strings.Contains(normHost, ":") {
		hostPort = "[" + normHost + "]"
	}
	return scheme + "://" + hostPort, true
}

func allowedOrigins(addr string) []string {
	if raw := strings.TrimSpace(os.Getenv("TANPOPO_ALLOWED_ORIGINS")); raw != "" {
		out := make([]string, 0)
		for _, part := range strings.Split(raw, ",") {
			if norm, ok := validateOriginEntry(part); ok {
				out = append(out, norm)
			}
		}
		return out
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || strings.TrimSpace(host) == "" {
		return nil
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() {
			return nil
		}
		hostPort := net.JoinHostPort(host, port)
		if ip.IsLoopback() {
			return []string{"http://" + hostPort, "https://" + hostPort}
		}
		return []string{"https://" + hostPort}
	}
	hostPort := net.JoinHostPort(host, port)
	if strings.EqualFold(host, "localhost") {
		return []string{"http://" + hostPort, "https://" + hostPort}
	}
	return []string{"https://" + hostPort}
}

func newProductionSidecarFactory(
	socketDir string,
	ca *sidecar.EphemeralCA,
	clientCert *sidecar.RotatingCertSource,
	auditLog port.AuditSink,
) func(context.Context, *snapshot.ProviderAdapterView, installer.PermissionSet) (extport.ProviderRuntime, io.Closer, error) {
	return func(_ context.Context, adp *snapshot.ProviderAdapterView, _ installer.PermissionSet) (extport.ProviderRuntime, io.Closer, error) {
		if adp == nil {
			return nil, nil, fmt.Errorf("%w: adapter snapshot view missing", extport.ErrInvalidSpec)
		}
		var manifest installer.ExtensionManifest
		if err := json.Unmarshal([]byte(adp.ManifestJSON), &manifest); err != nil {
			return nil, nil, fmt.Errorf("%w: parse sidecar manifest_json: %v", extport.ErrInvalidSpec, err)
		}
		if manifest.Runtime == nil || strings.TrimSpace(manifest.Runtime.Entrypoint) == "" {
			return nil, nil, fmt.Errorf("%w: missing sidecar runtime.entrypoint", extport.ErrInvalidSpec)
		}
		ep := strings.TrimSpace(manifest.Runtime.Entrypoint)
		scCfg := sidecar.Config{
			Enabled:        true,
			InstallationID: adp.InstallationID,
			AuditSink:      auditLog,
		}
		switch {
		case strings.HasPrefix(ep, "unix://"):
			confined, cerr := sidecar.ConfineSocketPath(socketDir, strings.TrimPrefix(ep, "unix://"))
			if cerr != nil {
				return nil, nil, fmt.Errorf("%w: %v", extport.ErrInvalidSpec, cerr)
			}
			scCfg.UDSPath = confined
		case strings.HasPrefix(ep, "https://"):
			scCfg.EndpointURL = ep
			scCfg.CA = ca
			scCfg.ClientCertSource = clientCert
			scCfg.ExpectedServerSPIFFE = "spiffe://" + sidecar.SPIFFETenantDomain + "/ext/" + adp.InstallationID
		case !strings.ContainsAny(ep, `/\`) && !strings.Contains(ep, ".."):
			confined, cerr := sidecar.ConfineSocketPath(socketDir, filepath.Join(socketDir, ep+".sock"))
			if cerr != nil {
				return nil, nil, fmt.Errorf("%w: %v", extport.ErrInvalidSpec, cerr)
			}
			scCfg.UDSPath = confined
		default:
			return nil, nil, fmt.Errorf("%w: invalid sidecar endpoint %q", extport.ErrInvalidSpec, ep)
		}
		sc, serr := sidecar.NewClient(scCfg)
		if serr != nil {
			return nil, nil, serr
		}
		return sc, sc, nil
	}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func optionalBoolEnv(name string) *bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch raw {
	case "1", "true", "yes", "on":
		v := true
		return &v
	case "0", "false", "no", "off":
		v := false
		return &v
	default:
		return nil
	}
}

func resolveRuntimeNodeID() string {
	if nodeID := strings.TrimSpace(os.Getenv("TANPOPO_NODE_ID")); nodeID != "" {
		return nodeID
	}
	if hn, err := os.Hostname(); err == nil && strings.TrimSpace(hn) != "" {
		return fmt.Sprintf("%s-%d", strings.TrimSpace(hn), os.Getpid())
	}
	return fmt.Sprintf("tanpopo-%d", os.Getpid())
}

func buildTelemetryPipelineConfig(runtimeCfg serveRuntimeConfig, svcVersion string, db *sql.DB, vaultStore *vault.Store) telemetry.PipelineConfig {
	var logOut io.Writer = os.Stdout
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TANPOPO_LOG_OUT"))) {
	case "discard", "none", "off":
		logOut = io.Discard
	case "stderr":
		logOut = os.Stderr
	}
	otlpEndpoint := strings.TrimSpace(runtimeCfg.OTLPEndpoint)
	if otlpEndpoint == "" {
		otlpEndpoint = envOr("TANPOPO_OTLP_ENDPOINT", strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")))
	}
	otlpAllowPrivate := parseEnvBool("TANPOPO_OTLP_ALLOW_PRIVATE_IP", false)
	if runtimeCfg.HasOTLPAllowPrivate {
		otlpAllowPrivate = runtimeCfg.OTLPAllowPrivateIP
	}
	var metricsInterval time.Duration
	if rawInt := strings.TrimSpace(os.Getenv("TANPOPO_OTLP_METRICS_EXPORT_INTERVAL")); rawInt != "" {
		if d, err := time.ParseDuration(rawInt); err == nil && d > 0 {
			metricsInterval = d
		}
	}
	return telemetry.PipelineConfig{
		ServiceName:                "tanpopo",
		ServiceVersion:             svcVersion,
		IncludeTenantLabels:        optionalBoolEnv("TANPOPO_METRICS_INCLUDE_TENANT_LABELS"),
		MetricsScrapeToken:         strings.TrimSpace(os.Getenv("TANPOPO_METRICS_SCRAPE_TOKEN")),
		AllowUnauthenticatedScrape: parseEnvBool("TANPOPO_METRICS_ALLOW_UNAUTHENTICATED", false),
		OTLPEndpoint:               otlpEndpoint,
		OTLPTracesEndpoint:         envOr("TANPOPO_OTLP_TRACES_ENDPOINT", strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))),
		OTLPMetricsEndpoint:        envOr("TANPOPO_OTLP_METRICS_ENDPOINT", strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"))),
		OTLPLogsEndpoint:           envOr("TANPOPO_OTLP_LOGS_ENDPOINT", strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"))),
		OTLPAllowPrivateIP:         otlpAllowPrivate,
		OTLPAllowedCIDRs:           parseEnvCSV("TANPOPO_OTLP_ALLOWED_CIDRS"),
		MetricsExportInterval:      metricsInterval,
		CapturePayloads:            false,
		DB:                         db,
		Vault:                      vaultStore,
		LogOut:                     logOut,
	}
}

type statusResponseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusResponseRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusResponseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(p)
}

func (r *statusResponseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func withTelemetryRequestLogger(next http.Handler, pipeline *telemetry.Pipeline, entryPoint, event string) http.Handler {
	if next == nil || pipeline == nil || pipeline.Logger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusResponseRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		level := "INFO"
		if status >= 500 {
			level = "ERROR"
		} else if status >= 400 {
			level = "WARN"
		}
		reqID := strings.TrimSpace(rec.Header().Get("X-Tanpopo-Request-Id"))
		if reqID == "" {
			reqID = strings.TrimSpace(r.Header.Get("X-Tanpopo-Request-Id"))
		}
		pathOnly := ""
		if r.URL != nil {
			pathOnly = r.URL.Path
		}
		_ = pipeline.Logger.Log(privacy.LogRecord{
			Level:      level,
			Event:      event,
			EntryPoint: entryPoint,
			RequestID:  reqID,
			Fields: map[string]any{
				"method":            r.Method,
				"path":              pathOnly,
				"http_status":       status,
				"total_duration_ms": time.Since(start).Milliseconds(),
			},
		})
	})
}

func startClusterNodeHeartbeat(
	runtimeCtx context.Context,
	db *sql.DB,
	dialect port.Dialect,
	nodeID, role, listen string,
	isPostgres bool,
	sqliteDBPath string,
	snaps *snapshot.Manager,
	clusterSync *snapshot.ClusterSync,
	vaultStore *vault.Store,
	dataServerReady func(context.Context) error,
) chan struct{} {
	done := make(chan struct{})
	if db == nil {
		close(done)
		return done
	}
	startedAtMs := time.Now().UTC().UnixMilli()
	storageEngine := "sqlite_wal"
	if isPostgres {
		storageEngine = "postgres"
	}
	interval := 2 * time.Second
	if rawInt := strings.TrimSpace(os.Getenv("TANPOPO_NODE_HEARTBEAT_INTERVAL")); rawInt != "" {
		if d, err := time.ParseDuration(rawInt); err == nil && d > 0 {
			interval = d
		}
	}
	upsert := func(opCtx context.Context) {
		hbCtx, cancel := context.WithTimeout(opCtx, 2*time.Second)
		defer cancel()
		var configEpoch int64
		if snaps != nil {
			if cur := snaps.Current(); cur != nil {
				configEpoch = cur.Epoch
			}
		}
		var snapAgeMs int64
		if snaps != nil {
			if age, ok := snaps.SnapshotAge(); ok && age > 0 {
				snapAgeMs = age.Milliseconds()
			}
		}
		health := "ok"
		if snaps == nil || snaps.CheckServingReady() != nil {
			health = "unhealthy"
		} else if clusterSync != nil && clusterSync.CheckServingReady() != nil {
			health = "unhealthy"
		} else if vaultStore == nil || vaultStore.CheckServingReady(hbCtx) != nil {
			health = "unhealthy"
		} else if dataServerReady != nil && dataServerReady(hbCtx) != nil {
			health = "unhealthy"
		}
		var walBytes int64
		if !isPostgres && sqliteDBPath != "" {
			if fi, err := os.Stat(sqliteDBPath + "-wal"); err == nil && fi.Size() >= 0 {
				walBytes = fi.Size()
			}
		}
		kekIDsJSON := "[]"
		if vaultStore != nil {
			if ids := vaultStore.LoadedKeyIDs(hbCtx); len(ids) > 0 {
				if raw, merr := json.Marshal(ids); merr == nil {
					kekIDsJSON = string(raw)
				}
			}
		}
		nowMs := time.Now().UTC().UnixMilli()
		query := port.Rebind(dialect, `
			INSERT INTO cluster_nodes (
				node_id, role, listen_addr, config_epoch, snapshot_age_ms,
				health, storage_engine, wal_bytes, loaded_kek_ids_json,
				last_heartbeat_ms, started_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(node_id) DO UPDATE SET
				role = excluded.role,
				listen_addr = excluded.listen_addr,
				config_epoch = excluded.config_epoch,
				snapshot_age_ms = excluded.snapshot_age_ms,
				health = excluded.health,
				storage_engine = excluded.storage_engine,
				wal_bytes = excluded.wal_bytes,
				loaded_kek_ids_json = excluded.loaded_kek_ids_json,
				last_heartbeat_ms = excluded.last_heartbeat_ms
		`)
		if _, err := db.ExecContext(hbCtx, query,
			nodeID, role, listen, configEpoch, snapAgeMs,
			health, storageEngine, walBytes, kekIDsJSON,
			nowMs, startedAtMs,
		); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("cluster_nodes heartbeat upsert failed", "node_id", nodeID, "error", err)
		}
	}
	if runtimeCtx.Err() == nil {
		upsert(runtimeCtx)
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runtimeCtx.Done():
				return
			case <-ticker.C:
				upsert(runtimeCtx)
			}
		}
	}()
	return done
}
