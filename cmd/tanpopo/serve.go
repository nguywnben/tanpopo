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
	"net"
	"net/http"
	"os"
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
	"github.com/nguywnben/tanpopo/internal/extension/installer"
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
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	telemetry "github.com/nguywnben/tanpopo/internal/telemetry/otel"
	"github.com/nguywnben/tanpopo/internal/webhook"
	"github.com/nguywnben/tanpopo/internal/webui"
)

func newServeCommand(loc i18n.Locale) *cobra.Command {
	role := "all"
	listen := envOr("TANPOPO_LISTEN_ADDR", "127.0.0.1:8080")
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
			if err := serve(cmd.Context(), role, listen); err != nil {
				return &testharness.ExitError{Code: testharness.ExitInternal, Message: i18n.T(loc, "cli.serve.err.failed"), Cause: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", role, i18n.T(loc, "cli.serve.flag.role"))
	cmd.Flags().StringVar(&listen, "listen", listen, i18n.T(loc, "cli.serve.flag.listen"))
	return cmd
}

func serve(ctx context.Context, role, listen string) (serveErr error) {
	drainTimeout, err := drainTimeoutFromEnv(os.LookupEnv)
	if err != nil {
		return err
	}
	var draining atomic.Bool
	handler, closeRuntime, err := buildRuntimeInternal(ctx, role, listen, nil, &draining)
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
		// Mark /readyz as 503 immediately on shutdown signal while draining in-flight SSE streams (OPS-AC-06).
		draining.Store(true)
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
	return buildRuntimeWithBeforeSQLiteClose(ctx, role, listen, nil)
}

// buildRuntimeWithBeforeSQLiteClose exposes the last quiescent SQLite state to
// shutdown tests. Production callers use buildRuntime and install no observer.
func buildRuntimeWithBeforeSQLiteClose(ctx context.Context, role, listen string, beforeSQLiteClose func(*sql.DB)) (http.Handler, func() error, error) {
	return buildRuntimeInternal(ctx, role, listen, beforeSQLiteClose, nil)
}

type runtimeStore interface {
	port.Store
	RawDB() *sql.DB
}

func buildRuntimeInternal(ctx context.Context, role, listen string, beforeSQLiteClose func(*sql.DB), draining *atomic.Bool) (http.Handler, func() error, error) {
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
	pgDSN := strings.TrimSpace(os.Getenv("TANPOPO_PG_DSN"))
	storageEngine := strings.ToLower(strings.TrimSpace(os.Getenv("TANPOPO_STORAGE_ENGINE")))
	switch storageEngine {
	case "", "sqlite", "postgres":
	default:
		return nil, nil, fmt.Errorf("unsupported TANPOPO_STORAGE_ENGINE %q: expected sqlite or postgres", storageEngine)
	}
	var (
		store         runtimeStore
		isPostgres    bool
		dialect       = port.DialectSQLite
		postShredHook func(context.Context) error
	)
	if storageEngine == "postgres" || (storageEngine == "" && pgDSN != "") {
		if pgDSN == "" {
			return nil, nil, errors.New("TANPOPO_PG_DSN is required when TANPOPO_STORAGE_ENGINE=postgres")
		}
		pgStore, openErr := postgres.Open(ctx, postgres.Options{DSN: pgDSN})
		if openErr != nil {
			return nil, nil, fmt.Errorf("open PostgreSQL store: %w", openErr)
		}
		store = pgStore
		isPostgres = true
		dialect = port.DialectPostgres
		postShredHook = pgStore.Ping
	} else {
		dbPath := envOr("TANPOPO_DB_PATH", filepath.Join(dataDir, "tanpopo.db"))
		sqliteStore, openErr := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
		if openErr != nil {
			return nil, nil, fmt.Errorf("open local SQLite store: %w", openErr)
		}
		store = sqliteStore
		postShredHook = sqliteStore.CheckpointWAL
	}
	closeStore := func() { _ = store.Close() }
	db := store.RawDB()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		closeStore()
		return nil, nil, fmt.Errorf("initialize audit logger: %w", err)
	}
	var ledgerService *ledger.Service
	kek, err := vault.LoadStaticKEKFromEnv("kek_v1", os.LookupEnv)
	if err != nil {
		closeStore()
		return nil, nil, fmt.Errorf("load Master KEK: %w", err)
	}
	closeKeys := func() { kek.Close() }
	vaultStore, err := vault.NewStore(vault.Config{DB: db, KEK: kek, PostShredHook: postShredHook})
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
			return nil, nil, fmt.Errorf("dial Valkey Cluster: %w", err)
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
			return nil, nil, fmt.Errorf("initialize Valkey rate limiter: %w", err)
		}
	}

	// Decouple background runtimeCtx from listen ctx so SIGTERM drains active SSE streams cleanly
	// while /readyz immediately returns 503 via draining.Store(true).
	runtimeCtx, cancelRuntime := context.WithCancel(context.Background())
	watcherDone := make(chan struct{})
	var webhookDone chan struct{}
	var closeWebhookDispatcher func() error
	var webhookDispatcher *webhook.Dispatcher
	var approvalMaintenanceDone chan struct{}
	var marketplaceDone <-chan struct{}
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
			return nil, nil, fmt.Errorf("initialize cluster snapshot sync: %w", err)
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
			if budgetLoopDone != nil {
				<-budgetLoopDone
			}
			if clusterDone != nil {
				<-clusterDone
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
		return nil, nil, fmt.Errorf("initialize webhook dispatcher: %w", err)
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
		return nil, nil, fmt.Errorf("initialize kill switches: %w", err)
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
		return nil, nil, fmt.Errorf("initialize usage ledger: %w", err)
	}
	if vkClient != nil && (role == "all" || role == "data-plane") {
		nodeID := strings.TrimSpace(os.Getenv("TANPOPO_NODE_ID"))
		if nodeID == "" {
			if hn, herr := os.Hostname(); herr == nil && strings.TrimSpace(hn) != "" {
				nodeID = strings.TrimSpace(hn)
			}
		}
		vkBudgetCoord, err = vkcluster.NewBudgetCoordinator(vkcluster.BudgetCoordinatorConfig{
			Client: vkClient,
			DB:     db,
			Ledger: ledgerService,
			NodeID: nodeID,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize Valkey budget coordinator: %w", err)
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
			return nil, nil, fmt.Errorf("initialize OAuth coordinator: %w", err)
		}
	}
	lockfile := envOr("TANPOPO_LOCK_FILE", filepath.Join(dataDir, "tanpopo.lock"))
	installerService, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB: db, Audit: auditLog, AuditEntryPoint: "admin_api", PluginsDir: pluginsDir, LockfilePath: lockfile,
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
		return nil, nil, fmt.Errorf("initialize extension installer: %w", err)
	}
	registryStore, err := registry.NewSQLStore(db, time.Now)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize marketplace registry store: %w", err)
	}
	marketplaceDone = startMarketplaceRuntime(runtimeCtx, registryStore, installerService, snaps, auditLog)
	brokerResolver := planeBrokerResolver{provider: catalogRepo, mcp: mcphub.BrokerResolver{DB: db}, a2a: a2a.BrokerResolver{DB: db}}
	blindBroker, err := broker.NewBlindBroker(broker.Config{
		Resolver: brokerResolver, PolicyResolver: brokerResolver, Vault: vaultStore,
		UnsealCap: brokerUnsealCap, Audit: auditLog, OAuthTokens: oauthService,
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize credential broker: %w", err)
	}
	a2aEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("TANPOPO_A2A_ENABLED")), "true")
	sidecarEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("TANPOPO_RUNTIME_SIDECAR_ENABLED")), "true")
	sidecarSocketDir := strings.TrimSpace(os.Getenv("TANPOPO_SIDECAR_SOCKET_DIR"))
	var sidecarFactory func(context.Context, *snapshot.ProviderAdapterView, installer.PermissionSet) (extport.ProviderRuntime, io.Closer, error)
	if sidecarEnabled {
		sidecarCA, caErr := sidecar.NewEphemeralCA(sidecar.DefaultCACertTTL, nil)
		if caErr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize sidecar ephemeral CA: %w", caErr)
		}
		sidecarClientCert, certErr := sidecar.NewRotatingCertSource(sidecarCA, "spiffe://"+sidecar.SPIFFETenantDomain+"/core/gateway", false, sidecar.DefaultLeafCertTTL)
		if certErr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize sidecar client certificate: %w", certErr)
		}
		sidecarFactory = newProductionSidecarFactory(sidecarSocketDir, sidecarCA, sidecarClientCert, auditLog)
	}
	mux := http.NewServeMux()
	if role == "all" || role == "data-plane" {
		mcpServer, serverErr := mcphub.NewServer(mcphub.Config{
			DB: db, Snapshots: snaps, Broker: blindBroker, Audit: auditLog,
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
			return nil, nil, fmt.Errorf("initialize MCP Governance Hub: %w", serverErr)
		}
		approvalMaintenanceDone = make(chan struct{})
		go func() {
			defer close(approvalMaintenanceDone)
			mcpServer.RunApprovalMaintenance(runtimeCtx)
		}()
		mux.Handle("/mcp", mcpServer)

		a2aCoordinator, a2aErr := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
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
			return nil, nil, fmt.Errorf("initialize A2A Task Coordinator: %w", a2aErr)
		}
		a2aHandler := a2aCoordinator.Handler()
		mux.Handle("/a2a/", a2aHandler)
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
		telemetryPipeline, err = telemetry.NewPipeline(telemetry.PipelineConfig{
			ServiceName: "tanpopo", ServiceVersion: version, DB: db, Vault: vaultStore,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize telemetry pipeline: %w", err)
		}
		responseCache, err := cache.NewExactCache(cache.Config{DB: db, Vault: vaultStore})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize response cache: %w", err)
		}
		dataServer, err = ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots: snaps, Broker: blindBroker, KillSwitch: killSwitch, PluginsDir: pluginsDir, Audit: auditLog, IdempotencyDB: db,
			RateLimiter:           vkRateLimiter,
			BudgetLedger:          ledgerService,
			Telemetry:             telemetryPipeline,
			ResponseCache:         responseCache,
			SidecarEnabled:        sidecarEnabled,
			SidecarSocketDir:      sidecarSocketDir,
			SidecarRuntimeFactory: sidecarFactory,
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
		vkeys, err := vkey.NewService(vkey.ServiceConfig{
			DB: db, Snapshots: snaps, AuditSink: auditLog, OnChange: publishChangeFn,
		})
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
			WebhookEnqueue: func(ctx context.Context, orgID, projectID, eventType string, payload json.RawMessage) error {
				_, enqueueErr := webhookDispatcher.Enqueue(ctx, webhook.Event{OrgID: orgID, ProjectID: projectID, Type: eventType, Payload: payload})
				return enqueueErr
			},
			OnConfigChange: publishChangeFn,
			Broker:         blindBroker, OAuth: oauthService, ProviderRuntimeFactory: providerRuntimeFactory, Audit: auditLog, Version: version,
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
